package proxy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetcher_Fetch_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0})
	}))
	defer srv.Close()

	f := NewFetcher()
	data, err := f.Fetch(context.Background(), srv.URL+"/img.jpg", nil)
	require.NoError(t, err)
	assert.Len(t, data, 4)
}

func TestFetcher_Fetch_ProxiesHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Проверяем, что заголовки клиента дошли до upstream
		assert.Equal(t, "my-custom-agent", r.Header.Get("User-Agent"))
		assert.Equal(t, "max-age=0", r.Header.Get("Cache-Control"))
		// Проверяем, что hop-by-hop заголовок Host был проигнорирован (остался адресом тестового сервера)
		assert.NotEqual(t, "should-be-ignored.com", r.Header.Get("Host"))

		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0})
	}))
	defer srv.Close()

	f := NewFetcher()

	// Имитируем заголовки от клиента
	headers := http.Header{}
	headers.Set("User-Agent", "my-custom-agent")
	headers.Set("Cache-Control", "max-age=0")
	headers.Set("Host", "should-be-ignored.com") // Должен быть проигнорирован

	data, err := f.Fetch(context.Background(), srv.URL+"/img.jpg", headers)
	require.NoError(t, err)
	assert.Len(t, data, 4)
}

func TestFetcher_Fetch_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := NewFetcher()
	_, err := f.Fetch(context.Background(), srv.URL+"/missing.jpg", nil)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestFetcher_Fetch_ServerError(t *testing.T) {
	tests := []struct {
		name string
		code int
	}{
		{"500", http.StatusInternalServerError},
		{"502", http.StatusBadGateway},
		{"503", http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.code)
			}))
			defer srv.Close()

			f := NewFetcher()
			_, err := f.Fetch(context.Background(), srv.URL+"/err", nil)
			require.ErrorIs(t, err, ErrBadGateway)
		})
	}
}

func TestFetcher_Fetch_InvalidMimeType(t *testing.T) {
	tests := []struct {
		name string
		ct   string
	}{
		{"exe", "application/octet-stream"},
		{"html", "text/html"},
		{"empty", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.ct != "" {
					w.Header().Set("Content-Type", tt.ct)
				}
				_, _ = w.Write([]byte("not image"))
			}))
			defer srv.Close()

			f := NewFetcher()
			_, err := f.Fetch(context.Background(), srv.URL+"/file", nil)
			require.ErrorIs(t, err, ErrInvalidMimeType)
		})
	}
}

func TestFetcher_Fetch_TooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(make([]byte, 2048))
	}))
	defer srv.Close()

	f := NewFetcher(WithMaxBodySize(1024))
	_, err := f.Fetch(context.Background(), srv.URL+"/big.jpg", nil)
	require.ErrorIs(t, err, ErrTooLarge)
}

func TestFetcher_Fetch_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := NewFetcher(WithTimeout(50 * time.Millisecond))
	_, err := f.Fetch(context.Background(), srv.URL+"/slow.jpg", nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrBadGateway) || errors.Is(err, context.DeadlineExceeded),
		"unexpected error: %v", err)
}

func TestFetcher_Fetch_EmptyURL(t *testing.T) {
	f := NewFetcher()
	_, err := f.Fetch(context.Background(), "", nil)
	require.Error(t, err)
}

func TestFetcher_CustomUserAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "my-bot/2.0", r.Header.Get("User-Agent"))
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 0x50, 0x4E, 0x47})
	}))
	defer srv.Close()

	f := NewFetcher(WithUserAgent("my-bot/2.0"))
	_, err := f.Fetch(context.Background(), srv.URL+"/img.png", nil)
	require.NoError(t, err)
}

func TestIsSupportedImage(t *testing.T) {
	tests := []struct {
		ct   string
		want bool
	}{
		{"image/jpeg", true},
		{"IMAGE/JPEG", true},
		{"image/jpeg; charset=binary", true},
		{"image/png", true},
		{"image/webp", false},
		{"text/html", false},
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, isSupportedImage(tt.ct), "ct=%q", tt.ct)
	}
}
