package config

import "time"

// Корневая структура конфигурации.
type Config struct {
	Logger LoggerConfig `yaml:"logger"`
	Server ServerConfig `yaml:"server"`
	Cache  CacheConfig  `yaml:"cache"`
}

// Настройки логгера.
type LoggerConfig struct {
	Level  string `yaml:"level"` // "debug", "info", "warn", "error"
	Format string `yaml:"format"`
}

// Настройки HTTP-сервера.
type ServerConfig struct {
	Port         int           `yaml:"port"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	IdleTimeout  time.Duration `yaml:"idle_timeout"`
}

// Настройки кэша.
type CacheConfig struct {
	Dir        string `yaml:"dir"`
	LimitBytes int64  `yaml:"limit_bytes"`
}
