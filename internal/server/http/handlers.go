package internalhttp

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/ivvklimov/image-previewer/internal/config"
	"github.com/ivvklimov/image-previewer/internal/logger"
)

// Обрабатывает HTTP-запросы к image-previewer.
type Handler struct {
	logger *logger.Logger
	cfg    *config.Config
}

// Создаёт новый обработчик.
func NewHandler(log *logger.Logger, cfg *config.Config) *Handler {
	return &Handler{logger: log, cfg: cfg}
}

// Обрабатывает GET /fill/{width}/{height}/{url...}.
func (h *Handler) HandlePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Парсинг пути: /fill/{width}/{height}/{remote_path_without_scheme}
	pathParts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 5)
	if len(pathParts) < 5 || pathParts[0] != "fill" {
		http.Error(w, `invalid path format. expected: /fill/{width}/{height}/{remote_url}`, http.StatusBadRequest)
		return
	}

	width, errW := strconv.Atoi(pathParts[1])
	height, errH := strconv.Atoi(pathParts[2])
	if errW != nil || errH != nil || width <= 0 || height <= 0 {
		http.Error(w, "width and height must be positive integers", http.StatusBadRequest)
		return
	}

	// Собираем remote_path: host + путь
	remotePath := pathParts[3]
	if pathParts[4] != "" {
		remotePath += "/" + pathParts[4]
	}

	// Автодобавляем https:// (безопасный дефолт)
	remoteURLStr := "https://" + remotePath

	// Валидация итогового URL
	remoteURL, err := url.Parse(remoteURLStr)
	if err != nil || remoteURL.Scheme == "" || remoteURL.Host == "" {
		http.Error(w, "invalid remote URL", http.StatusBadRequest)
		return
	}

	// Бизнес-логирование (статус и клиент логирует middleware)
	h.logger.Info(fmt.Sprintf("preview requested: %dx%d %s", width, height, remoteURL.String()))

	// TODO: Proxy -> Resize -> Cache (будет в следующей ветке)
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Endpoint ready. Dimensions: %sx%s\nURL: %s\n", pathParts[1], pathParts[2], remoteURL.String())
}
