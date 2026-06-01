package internalhttp

import (
	"bytes"
	"crypto/tls"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ivvklimov/image-previewer/internal/cache"
	"github.com/ivvklimov/image-previewer/internal/config"
	"github.com/ivvklimov/image-previewer/internal/logger"
	"github.com/ivvklimov/image-previewer/internal/proxy"
	"github.com/ivvklimov/image-previewer/internal/resizer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeTestJPEG создаёт тестовое JPEG-изображение.
func makeTestJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	buf := &bytes.Buffer{}
	require.NoError(t, jpeg.Encode(buf, img, &jpeg.Options{Quality: 75}))
	return buf.Bytes()
}

func TestHandler_HandlePreview_Integration(t *testing.T) {
	// 1. Поднимаем HTTPS тестовый сервер (так как хендлер жёстко добавляет "https://")
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(makeTestJPEG(t, 600, 300))
	}))
	defer upstream.Close()

	// Извлекаем host без схемы: upstream.URL = "https://127.0.0.1:xxxxx", нам нужно "127.0.0.1:xxxxx"
	testHost := upstream.URL[len("https://"):]

	// 2. Настраиваем прокси-клиент, который игнорирует ошибки самоподписанных сертификатов тестового сервера
	insecureClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 10 * time.Second,
	}

	// 3. Инициализируем реальные зависимости для теста
	tmpDir := t.TempDir()
	diskCache := cache.NewDiskCache(tmpDir)

	// Передаём наш кастомный клиент в прокси
	proxyFetcher := proxy.NewFetcher(proxy.WithHTTPClient(insecureClient))

	imageResizer := resizer.NewResizer()
	log := logger.NewWithOutput("info", "test", io.Discard)
	cfg := &config.Config{}

	h := NewHandler(log, cfg, diskCache, proxyFetcher, imageResizer)

	tests := []struct {
		name     string
		method   string
		path     string
		wantCode int
	}{
		{
			name:     "cache miss -> fetch -> resize -> cache set",
			method:   http.MethodGet,
			path:     "/fill/200/200/" + testHost + "/image.jpg",
			wantCode: http.StatusOK,
		},
		{
			name:     "cache hit (same key as above)",
			method:   http.MethodGet,
			path:     "/fill/200/200/" + testHost + "/image.jpg",
			wantCode: http.StatusOK,
		},
		{
			name:     "invalid dimensions",
			method:   http.MethodGet,
			path:     "/fill/-10/200/" + testHost + "/image.jpg",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "missing url",
			method:   http.MethodGet,
			path:     "/fill/200/200/",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "wrong method",
			method:   http.MethodPost,
			path:     "/fill/200/200/" + testHost + "/image.jpg",
			wantCode: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			h.HandlePreview(w, req)

			require.Equal(t, tt.wantCode, w.Code)
			if tt.wantCode == http.StatusOK {
				assert.NotEmpty(t, w.Body.Bytes())
				// Проверяем, что результат - действительно валидный JPEG (magic bytes)
				assert.True(t, bytes.HasPrefix(w.Body.Bytes(), []byte{0xFF, 0xD8, 0xFF}), "response should be valid JPEG")
			}
		})
	}
}
