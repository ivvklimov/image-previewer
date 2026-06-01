package internalhttp

import (
	"context"
	"net/http"
	"strconv"

	"github.com/ivvklimov/image-previewer/internal/cache"
	"github.com/ivvklimov/image-previewer/internal/config"
	"github.com/ivvklimov/image-previewer/internal/logger"
	"github.com/ivvklimov/image-previewer/internal/proxy"
	"github.com/ivvklimov/image-previewer/internal/resizer"
)

// HTTP-сервер с поддержкой graceful shutdown.
type Server struct {
	httpServer *http.Server
	logger     *logger.Logger
}

// Создаёт новый сервер.
func NewServer(host string, port int, log *logger.Logger, cfg *config.Config) *Server {
	mux := http.NewServeMux()

	// 1. Инициализация зависимостей
	diskCache := cache.NewDiskCache(cfg.Cache.Dir)
	proxyFetcher := proxy.NewFetcher(
		proxy.WithTimeout(cfg.Server.ReadTimeout), // Используем таймаут из конфига
	)
	imageResizer := resizer.NewResizer()

	// 2. Создаём хэндлер с внедрёнными зависимостями
	h := NewHandler(log, cfg, diskCache, proxyFetcher, imageResizer)

	// 3. Роуты
	mux.HandleFunc("GET /fill/{width}/{height}/{url...}", h.HandlePreview)

	// 4. Применяем middleware логирования
	handler := LoggingMiddleware(log, mux)

	return &Server{
		httpServer: &http.Server{
			Addr:         host + ":" + strconv.Itoa(port),
			Handler:      handler,
			ReadTimeout:  cfg.Server.ReadTimeout,
			WriteTimeout: cfg.Server.WriteTimeout,
			IdleTimeout:  cfg.Server.IdleTimeout,
		},
		logger: log,
	}
}

// Запускает сервер в блокирующем режиме.
func (s *Server) Start(ctx context.Context) error {
	s.logger.Info("starting http server addr=" + s.httpServer.Addr)
	errChan := make(chan error, 1)
	go func() {
		errChan <- s.httpServer.ListenAndServe()
	}()

	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return s.Stop(context.Background())
	}
}

// Выполняет graceful shutdown.
func (s *Server) Stop(ctx context.Context) error {
	s.logger.Info("stopping http server")
	return s.httpServer.Shutdown(ctx)
}
