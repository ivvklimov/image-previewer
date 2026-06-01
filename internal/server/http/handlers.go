package internalhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ivvklimov/image-previewer/internal/cache"
	"github.com/ivvklimov/image-previewer/internal/config"
	"github.com/ivvklimov/image-previewer/internal/logger"
	"github.com/ivvklimov/image-previewer/internal/proxy"
	"github.com/ivvklimov/image-previewer/internal/resizer"
)

// Обрабатывает HTTP-запросы к image-previewer.
type Handler struct {
	logger  *logger.Logger
	cfg     *config.Config
	cache   *cache.DiskCache
	proxy   *proxy.Fetcher
	resizer *resizer.Resizer
}

// Создаёт новый обработчик с внедрёнными зависимостями.
func NewHandler(log *logger.Logger, cfg *config.Config, c *cache.DiskCache, p *proxy.Fetcher, r *resizer.Resizer) *Handler {
	return &Handler{logger: log, cfg: cfg, cache: c, proxy: p, resizer: r}
}

// Обрабатывает GET /fill/{width}/{height}/{url...}.
func (h *Handler) HandlePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. Парсинг пути: /fill/{width}/{height}/{remote_path_without_scheme}
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

	remotePath := pathParts[3]
	if pathParts[4] != "" {
		remotePath += "/" + pathParts[4]
	}
	remoteURLStr := "https://" + remotePath

	remoteURL, err := url.Parse(remoteURLStr)
	if err != nil || remoteURL.Scheme == "" || remoteURL.Host == "" {
		http.Error(w, "invalid remote URL", http.StatusBadRequest)
		return
	}

	// 2. Формируем ключ кэша
	cacheKey := cache.KeyOf(remoteURL.String(), width, height)

	// Контекст с таймаутом для операций ввода-вывода
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// 3. Проверяем кэш
	cachedData, err := h.cache.Get(ctx, cacheKey)
	if err == nil {
		h.logger.Info(fmt.Sprintf("cache hit: %dx%d %s", width, height, remoteURL.String()))
		w.Header().Set("Content-Type", http.DetectContentType(cachedData))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(cachedData)
		return
	}

	if !errors.Is(err, cache.ErrNotFound) {
		h.logger.Warn(fmt.Sprintf("cache read error (proceeding to fetch): %v", err))
	}

	// 4. Cache miss: загружаем через прокси, передавая исходные заголовки запроса (r.Header)
	h.logger.Info(fmt.Sprintf("cache miss, fetching: %dx%d %s", width, height, remoteURL.String()))

	data, err := h.proxy.Fetch(ctx, remoteURL.String(), r.Header)
	if err != nil {
		h.handleProxyError(w, err, remoteURL.String())
		return
	}

	// 5. Ресайз
	resizedData, err := h.resizer.Fill(data, width, height)
	if err != nil {
		h.logger.Warn(fmt.Sprintf("resize error: %v", err))
		http.Error(w, "failed to process image", http.StatusInternalServerError)
		return
	}

	// 6. Сохраняем в кэш (ошибки записи логируем, но не прерываем успешный ответ клиенту)
	if err := h.cache.Set(ctx, cacheKey, resizedData); err != nil {
		h.logger.Warn(fmt.Sprintf("cache write error: %v", err))
	}

	// 7. Возвращаем результат клиенту
	w.Header().Set("Content-Type", http.DetectContentType(resizedData))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resizedData)
}

// Маппит доменные ошибки прокси в HTTP-статусы.
func (h *Handler) handleProxyError(w http.ResponseWriter, err error, url string) {
	switch {
	case errors.Is(err, proxy.ErrNotFound):
		http.Error(w, "image not found", http.StatusNotFound)
	case errors.Is(err, proxy.ErrInvalidMimeType):
		http.Error(w, "unsupported image format", http.StatusUnsupportedMediaType)
	case errors.Is(err, proxy.ErrTooLarge):
		http.Error(w, "image too large", http.StatusRequestEntityTooLarge)
	case errors.Is(err, proxy.ErrBadGateway):
		h.logger.Warn(fmt.Sprintf("proxy bad gateway: %v, url: %s", err, url))
		http.Error(w, "bad gateway", http.StatusBadGateway)
	default:
		h.logger.Error(fmt.Sprintf("proxy unknown error: %v, url: %s", err, url))
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}
