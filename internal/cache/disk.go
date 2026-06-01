package cache

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Возвращается, когда ключ отсутствует в кэше.
var ErrNotFound = errors.New("cache key not found")

// Дисковый кэш изображений.
// Атомарная запись: .tmp -> rename, защита от частично записанных файлов.
type DiskCache struct {
	dir string
}

// Создаёт дисковый кэш в указанной директории.
// Директория должна существовать (создаётся в main.go через os.MkdirAll).
func NewDiskCache(dir string) *DiskCache {
	return &DiskCache{dir: dir}
}

// Генерирует ключ кэша: SHA256(url + ":" + width + "x" + height) в hex.
func KeyOf(url string, width, height int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%dx%d", url, width, height)))
	return fmt.Sprintf("%x", h)
}

// Возвращает данные из кэша или ErrNotFound.
func (c *DiskCache) Get(_ context.Context, key string) ([]byte, error) {
	data, err := os.ReadFile(c.path(key))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read cache: %w", err)
	}
	return data, nil
}

// Атомарно сохраняет данные в кэш: сначала в .tmp, затем rename.
func (c *DiskCache) Set(_ context.Context, key string, data []byte) error {
	if len(data) == 0 {
		return errors.New("cannot cache empty data")
	}

	target := c.path(key)
	tmp := target + ".tmp"

	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write tmp: %w", err)
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close tmp: %w", err)
	}

	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// Проверяет наличие ключа в кэше.
func (c *DiskCache) Has(_ context.Context, key string) (bool, error) {
	_, err := os.Stat(c.path(key))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (c *DiskCache) path(key string) string {
	return filepath.Join(c.dir, key)
}
