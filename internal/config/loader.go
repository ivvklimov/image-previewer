package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Загружает конфигурацию из YAML-файла в любую структуру.
// Универсальная функция для всех сервисов (как в календаре).
func Load[T any](path string) (*T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %s: %w", path, err)
	}

	var cfg T
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return &cfg, nil
}

// Применяет переменные окружения к конфигурации image-previewer.
// Вызывайте после config.Load[Config]() в cmd/server/main.go.
// ENV имеет приоритет над значениями из YAML-файла (12-Factor).
func ApplyAPIEnvOverrides(cfg *Config) {
	// Logger
	if envLevel := os.Getenv("LOG_LEVEL"); envLevel != "" {
		cfg.Logger.Level = envLevel
	}

	// Server
	if envPort := os.Getenv("APP_PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil {
			cfg.Server.Port = p
		}
	}
	if env := os.Getenv("READ_TIMEOUT"); env != "" {
		if d, err := time.ParseDuration(env); err == nil {
			cfg.Server.ReadTimeout = d
		}
	}
	if env := os.Getenv("WRITE_TIMEOUT"); env != "" {
		if d, err := time.ParseDuration(env); err == nil {
			cfg.Server.WriteTimeout = d
		}
	}
	if env := os.Getenv("IDLE_TIMEOUT"); env != "" {
		if d, err := time.ParseDuration(env); err == nil {
			cfg.Server.IdleTimeout = d
		}
	}

	// Cache
	if env := os.Getenv("CACHE_DIR"); env != "" {
		cfg.Cache.Dir = env
	}
	if env := os.Getenv("CACHE_LIMIT_BYTES"); env != "" {
		if i, err := strconv.ParseInt(env, 10, 64); err == nil {
			cfg.Cache.LimitBytes = i
		}
	}
}

// Проверяет корректность конфигурации image-previewer (fail-fast).
func ValidateAPI(cfg *Config) error {
	// Server
	if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", cfg.Server.Port)
	}
	if cfg.Server.ReadTimeout <= 0 {
		cfg.Server.ReadTimeout = 10 * time.Second
	}
	if cfg.Server.WriteTimeout <= 0 {
		cfg.Server.WriteTimeout = 30 * time.Second
	}
	if cfg.Server.IdleTimeout <= 0 {
		cfg.Server.IdleTimeout = 60 * time.Second
	}

	// Logger
	if cfg.Logger.Level == "" {
		cfg.Logger.Level = "info"
	}

	// Cache
	if cfg.Cache.Dir == "" {
		cfg.Cache.Dir = "./cache"
	}
	if cfg.Cache.LimitBytes <= 0 {
		return fmt.Errorf("cache limit must be > 0, got: %d", cfg.Cache.LimitBytes)
	}

	return nil
}
