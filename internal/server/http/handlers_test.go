package internalhttp

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ivvklimov/image-previewer/internal/config"
	"github.com/ivvklimov/image-previewer/internal/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testServiceName = "image-previewer"
	testImagePath   = "raw.githubusercontent.com/OtusGolang/final_project/master/examples/image-previewer/_gopher_original_1024x504.jpg"
)

// === ХЕЛПЕРЫ ====

// Создаёт *http.Request с нужным методом и путём.
func newTestRequest(method, path string) *http.Request {
	return httptest.NewRequest(method, path, nil)
}

// === ТЕСТЫ ===

func TestHandler_HandlePreview(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		path     string
		wantCode int
		wantBody string
		wantLog  string
	}{
		{
			name:     "valid path",
			method:   http.MethodGet,
			path:     "/fill/300/200/" + testImagePath,
			wantCode: http.StatusOK,
			wantBody: "Dimensions: 300x200",
			wantLog:  "preview requested",
		},
		{
			name:     "missing url",
			method:   http.MethodGet,
			path:     "/fill/300/200/",
			wantCode: http.StatusBadRequest,
			wantBody: "invalid path format",
			wantLog:  "", // ошибка логируется отдельно, не в этом тесте
		},
		{
			name:     "invalid dimensions",
			method:   http.MethodGet,
			path:     "/fill/-10/200/" + testImagePath,
			wantCode: http.StatusBadRequest,
			wantBody: "positive integers",
			wantLog:  "",
		},
		{
			name:     "wrong method",
			method:   http.MethodPost,
			path:     "/fill/300/200/" + testImagePath,
			wantCode: http.StatusMethodNotAllowed,
			wantBody: "method not allowed",
			wantLog:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Изолируем логи в buffer для проверки
			var buf bytes.Buffer
			log := logger.NewWithOutput("info", testServiceName, &buf)
			cfg := &config.Config{}

			h := NewHandler(log, cfg)

			req := newTestRequest(tt.method, tt.path)
			w := httptest.NewRecorder()

			h.HandlePreview(w, req)

			require.Equal(t, tt.wantCode, w.Code)
			if tt.wantBody != "" {
				assert.Contains(t, w.Body.String(), tt.wantBody)
			}
			if tt.wantLog != "" {
				assert.Contains(t, buf.String(), tt.wantLog)
			}
		})
	}
}
