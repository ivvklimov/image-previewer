package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Константы по умолчанию.
const (
	DefaultTimeout     = 10 * time.Second
	DefaultMaxBodySize = 10 << 20 // 10 MB
)

// Типовые ошибки прокси.
var (
	ErrNotFound        = errors.New("upstream: image not found")
	ErrBadGateway      = errors.New("upstream: server error")
	ErrInvalidMimeType = errors.New("upstream: unsupported MIME type, only image/jpeg and image/png are allowed")
	ErrTooLarge        = errors.New("upstream: image exceeds max size")
)

// HTTP-клиент для загрузки изображений.
type Fetcher struct {
	client    *http.Client
	maxBody   int64
	userAgent string
}

// Функциональная опция для Fetcher.
type Option func(*Fetcher)

// Устанавливает таймаут HTTP-клиента.
func WithTimeout(d time.Duration) Option {
	return func(f *Fetcher) { f.client.Timeout = d }
}

// Устанавливает лимит размера тела ответа.
func WithMaxBodySize(n int64) Option {
	return func(f *Fetcher) { f.maxBody = n }
}

// Устанавливает User-Agent по умолчанию.
func WithUserAgent(ua string) Option {
	return func(f *Fetcher) { f.userAgent = ua }
}

// Позволяет заменить HTTP-клиент (полезно для тестов с самоподписанными сертификатами).
func WithHTTPClient(client *http.Client) Option {
	return func(f *Fetcher) {
		f.client = client
	}
}

// Создаёт прокси-клиент с дефолтными параметрами.
func NewFetcher(opts ...Option) *Fetcher {
	f := &Fetcher{
		client:    &http.Client{Timeout: DefaultTimeout},
		maxBody:   DefaultMaxBodySize,
		userAgent: "image-previewer/1.0",
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// Загружает изображение по URL, проксируя исходные заголовки запроса (RFC 7230).
func (f *Fetcher) Fetch(ctx context.Context, url string, originalHeaders http.Header) ([]byte, error) {
	if url == "" {
		return nil, errors.New("empty url")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	// 1. Проксирование заголовков (RFC 7230: пропускаем hop-by-hop заголовки)
	if originalHeaders != nil {
		hopByHop := map[string]bool{
			"connection":        true,
			"host":              true,
			"transfer-encoding": true,
			"upgrade":           true,
			"proxy-connection":  true,
			"keep-alive":        true,
		}
		for key, values := range originalHeaders {
			if hopByHop[strings.ToLower(key)] {
				continue
			}
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
	}

	// 2. Дефолтные значения, если они не были переданы клиентом
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", f.userAgent)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "image/jpeg, image/png")
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadGateway, err)
	}
	defer resp.Body.Close()

	// 3. Маппинг HTTP-кодов в доменные ошибки
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("%w: status %d", ErrBadGateway, resp.StatusCode)
	case resp.StatusCode >= 400:
		return nil, fmt.Errorf("upstream client error: status %d", resp.StatusCode)
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	// 4. Валидация Content-Type
	ct := resp.Header.Get("Content-Type")
	if !isSupportedImage(ct) {
		return nil, fmt.Errorf("%w: got %q", ErrInvalidMimeType, ct)
	}

	// 5. Ограничение размера: читаем maxBody+1 байт, если больше — ошибка
	limited := io.LimitReader(resp.Body, f.maxBody+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if int64(len(data)) > f.maxBody {
		return nil, ErrTooLarge
	}

	return data, nil
}

// Проверяет, что Content-Type является JPEG или PNG.
func isSupportedImage(ct string) bool {
	mime := strings.TrimSpace(strings.SplitN(ct, ";", 2)[0])
	mime = strings.ToLower(mime)
	return mime == "image/jpeg" || mime == "image/png"
}
