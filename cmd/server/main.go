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

	// 2. Загружаем базовый конфиг из файла
	cfg, err := config.Load[config.Config](configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// 3. Применяем ENV-переменные (приоритет над файлом)
	config.ApplyAPIEnvOverrides(cfg)

	// 4. Валидация конфига (fail-fast)
	if err := config.ValidateAPI(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Config validation failed: %v\n", err)
		os.Exit(1)
	}

	// 5. Инициализация логгера
	log := logger.New(cfg.Logger.Level, "image-previewer")
	log.Info(fmt.Sprintf("Starting image-previewer (version: %s)", version.String()))

	// 6. Инициализация директории кэша
	if err := os.MkdirAll(cfg.Cache.Dir, 0755); err != nil {
		log.Error(fmt.Sprintf("failed to create cache directory %s: %v", cfg.Cache.Dir, err))
		os.Exit(1)
	}

	// 7. Создание HTTP-сервера
	server := internalhttp.NewServer("", cfg.Server.Port, log, cfg)

	// 8. Контекст с graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()

	log.Info("image-previewer is running...")

	// 9. Запуск сервера (блокирующий)
	if err := server.Start(ctx); err != nil {
		log.Error("failed to start http server: " + err.Error())
		cancel()
		os.Exit(1)
	}
}
