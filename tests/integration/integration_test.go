//go:build integration

package integration

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// Адрес нашего приложения (запущенного локально)
	baseURL = "http://localhost:8090"
	// Адрес тестового upstream (nginx в Docker, host network)
	upstreamHost = "upstream"
)

// Формирует URL запроса к приложению.
func buildURL(width, height int, imageURL string) string {
	return fmt.Sprintf("%s/fill/%d/%d/%s", baseURL, width, height, imageURL)
}

func TestMain(m *testing.M) {
	// Ждём, пока приложение станет доступно
	fmt.Println("Waiting for application to be ready...")
	waitForService(baseURL+"/health", 30*time.Second)
	fmt.Println("Application is ready!")

	os.Exit(m.Run())
}

func waitForService(url string, timeout time.Duration) {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	panic(fmt.Sprintf("service at %s did not become ready within %v", url, timeout))
}

// === СЦЕНАРИИ ИЗ ТЗ ===

// 1. Удаленный сервер вернул изображение (успешный запрос, cache miss).
func TestIntegration_UpstreamReturnsImage(t *testing.T) {
	imageURL := upstreamHost + "/gopher_original_1024x504.jpg"
	url := buildURL(200, 200, imageURL)

	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "image/")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Greater(t, len(body), 0, "response body should not be empty")
}

// 2. Картинка найдена в кэше (cache hit).
func TestIntegration_CacheHit(t *testing.T) {
	imageURL := upstreamHost + "/gopher_original_1024x504.jpg"
	url := buildURL(150, 150, imageURL)

	// Первый запрос (cache miss)
	resp1, err := http.Get(url)
	require.NoError(t, err)
	body1, _ := io.ReadAll(resp1.Body)
	_ = resp1.Body.Close()
	require.Equal(t, http.StatusOK, resp1.StatusCode)

	// Второй запрос (cache hit)
	start := time.Now()
	resp2, err := http.Get(url)
	require.NoError(t, err)
	body2, _ := io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()
	duration := time.Since(start)

	require.Equal(t, http.StatusOK, resp2.StatusCode)
	assert.Equal(t, body1, body2, "cached response should be identical")
	t.Logf("Cache hit took %v", duration)
}

// 3. Удаленный сервер не существует.
func TestIntegration_UpstreamDoesNotExist(t *testing.T) {
	// Несуществующий порт
	imageURL := "http://localhost:9999/nonexistent.jpg"
	url := buildURL(200, 200, imageURL)

	resp, err := http.Get(url)
	require.NoError(t, err) // HTTP-запрос прошёл, но сервер вернул ошибку
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadGateway, resp.StatusCode, "should return 502 when upstream is unreachable")
}

// 4. Удаленный сервер существует, но изображение не найдено (404).
func TestIntegration_ImageNotFound(t *testing.T) {
	imageURL := upstreamHost + "/nonexistent_image.jpg"
	url := buildURL(200, 200, imageURL)

	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "should return 404 when image not found")
}

// 5. Удаленный сервер существует, но файл не изображение.
func TestIntegration_NotAnImage(t *testing.T) {
	imageURL := upstreamHost + "/file.txt" // <-- было /fake.exe
	url := buildURL(200, 200, imageURL)

	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Ожидаем ошибку - приложение должно отвергнуть не изображение
	assert.NotEqual(t, http.StatusOK, resp.StatusCode, "should reject non-image files")
}

// 6. Удаленный сервер вернул ошибку (5xx).
func TestIntegration_UpstreamServerError(t *testing.T) {
	imageURL := upstreamHost + "/error"
	url := buildURL(200, 200, imageURL)

	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadGateway, resp.StatusCode, "should return 502 when upstream returns 5xx")
}

// 7. Изображение меньше нужного размера (upscale).
func TestIntegration_ImageSmallerThanRequested(t *testing.T) {
	imageURL := upstreamHost + "/tiny.jpg"
	// Запрашиваем размер больше оригинала (оригинал 10x10, просим 200x200)
	url := buildURL(200, 200, imageURL)

	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "should handle upscale")
}

// === ДОПОЛНИТЕЛЬНЫЕ СЦЕНАРИИ ===

// Некорректные параметры запроса.
func TestIntegration_InvalidParameters(t *testing.T) {
	tests := []struct {
		name string
		path string
		want int
	}{
		{"zero width", "/fill/0/200/" + upstreamHost + "/gopher_original_1024x504.jpg", http.StatusBadRequest},
		{"zero height", "/fill/200/0/" + upstreamHost + "/gopher_original_1024x504.jpg", http.StatusBadRequest},
		{"negative width", "/fill/-1/200/" + upstreamHost + "/gopher_original_1024x504.jpg", http.StatusBadRequest},
		{"missing url", "/fill/200/200/", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(baseURL + tt.path)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, tt.want, resp.StatusCode)
		})
	}
}

// Конкурентные запросы (проверка потокобезопасности).
func TestIntegration_ConcurrentRequests(t *testing.T) {
	imageURL := upstreamHost + "/ingress-lens.png"
	url := buildURL(100, 100, imageURL)

	const numRequests = 20
	var wg sync.WaitGroup
	wg.Add(numRequests)

	errCh := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			defer wg.Done()
			resp, err := http.Get(url)
			if err != nil {
				errCh <- err
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errCh <- fmt.Errorf("unexpected status: %d", resp.StatusCode)
				return
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent request failed: %v", err)
	}
}

// Проверка, что Content-Type соответствует формату изображения.
func TestIntegration_ContentTypeMatchesFormat(t *testing.T) {
	tests := []struct {
		file     string
		wantType string
	}{
		{"gopher_original_1024x504.jpg", "image/jpeg"},
		{"ingress-lens.png", "image/png"},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			imageURL := upstreamHost + "/" + tt.file
			url := buildURL(100, 100, imageURL)

			resp, err := http.Get(url)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusOK, resp.StatusCode)
			ct := resp.Header.Get("Content-Type")
			assert.True(t, strings.HasPrefix(ct, tt.wantType),
				"Content-Type should be %s, got %s", tt.wantType, ct)
		})
	}
}
