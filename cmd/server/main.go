package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ivvklimov/image-previewer/internal/config"
	"github.com/ivvklimov/image-previewer/internal/logger"
	internalhttp "github.com/ivvklimov/image-previewer/internal/server/http"
	"github.com/ivvklimov/image-previewer/internal/version"
)

// Глобальные переменные для флагов.
var (
	configFile  string
	showVersion bool
)

// Регистрирует флаги (выполняется до main).
func init() {
	flag.StringVar(&configFile, "config", "configs/config.yaml", "Path to configuration file")
	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
}

func main() {
	flag.Parse()

	// 1. Проверка версии - до загрузки конфига
	if showVersion || flag.Arg(0) == "version" {
		version.Print()
		return
	}

	// 2. Запускаем основную логику
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Fatal error: %v\n", err)
		os.Exit(1)
	}
}

// Содержит всю логику инициализации и запуска.
// Возвращает ошибку, если что-то пошло не так.
func run() error {
	// 1. Загружаем базовый конфиг из файла
	cfg, err := config.Load[config.Config](configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// 2. Применяем ENV-переменные (приоритет над файлом)
	config.ApplyAPIEnvOverrides(cfg)

	// 3. Валидация конфига (fail-fast)
	if err := config.ValidateAPI(cfg); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	// 4. Инициализация логгера
	log := logger.New(cfg.Logger.Level, "image-previewer")
	log.Info(fmt.Sprintf("Starting image-previewer (version: %s)", version.String()))

	// 5. Инициализация директории кэша
	if err := os.MkdirAll(cfg.Cache.Dir, 0o755); err != nil {
		return fmt.Errorf("failed to create cache directory %s: %w", cfg.Cache.Dir, err)
	}

	// 6. Создание HTTP-сервера
	server := internalhttp.NewServer("", cfg.Server.Port, log, cfg)

	// 7. Контекст с graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()

	log.Info("image-previewer is running...")

	// 8. Запуск сервера (блокирующий).
	// Возвращает ошибку, если не удалось запуститься, или nil при штатной остановке.
	return server.Start(ctx)
}
