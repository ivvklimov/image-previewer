package cache

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Ошибки кэша.
var (
	ErrNotFound = errors.New("cache key not found")
)

// LRU-кэш на диске с ограничением в байтах.
type DiskCache struct {
	dir         string
	maxSize     int64             // максимальный размер в байтах
	currentSize int64             // текущий занятый размер
	items       map[string]*entry // ключ -> метаданные
	queue       List              // LRU-очередь
	mu          sync.RWMutex      // RWMutex для конкурентного доступа
}

// Метаданные одного закэшированного файла.
type entry struct {
	key      string
	size     int64
	listItem *ListItem
}

// Генерирует ключ кэша: SHA256(url + ":" + width + "x" + height) в hex.
func KeyOf(url string, width, height int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%dx%d", url, width, height)))
	return fmt.Sprintf("%x", h)
}

// Создаёт новый дисковый LRU-кэш и восстанавливает его состояние с диска.
func NewDiskCache(dir string, maxSize int64) *DiskCache {
	if maxSize <= 0 {
		maxSize = 100 << 20 // 100 MB по умолчанию
	}

	c := &DiskCache{
		dir:     dir,
		maxSize: maxSize,
		items:   make(map[string]*entry),
		queue:   NewList(),
	}

	// Восстанавливаем состояние из существующих файлов на диске
	c.restoreFromDisk()

	return c
}

// Сканирует директорию кэша при старте и добавляет существующие
// файлы в LRU-очередь, сортируя их по времени модификации (от старых к новым).
func (c *DiskCache) restoreFromDisk() {
	c.mu.Lock()
	defer c.mu.Unlock()

	files, err := os.ReadDir(c.dir)
	if err != nil {
		return // Если директории нет или нет прав, просто возвращаем пустой кэш
	}

	type fileInfo struct {
		name string
		info os.FileInfo
	}
	var validFiles []fileInfo

	for _, f := range files {
		if f.IsDir() || strings.HasPrefix(f.Name(), ".") || strings.HasSuffix(f.Name(), ".tmp") {
			continue
		}
		info, err := f.Info()
		if err != nil {
			continue
		}
		validFiles = append(validFiles, fileInfo{name: f.Name(), info: info})
	}

	// Честно сортируем по времени модификации (от старых к новым)
	sort.Slice(validFiles, func(i, j int) bool {
		return validFiles[i].info.ModTime().Before(validFiles[j].info.ModTime())
	})

	// Добавляем в конец очереди (PushBack) в хронологическом порядке
	for _, f := range validFiles {
		size := f.info.Size()
		c.currentSize += size

		e := &entry{
			key:  f.name,
			size: size,
		}
		e.listItem = c.queue.PushBack(e)
		c.items[f.name] = e
	}

	// Если после восстановления превысили лимит, подчищаем
	c.evict()
}

// Возвращает данные из кэша и обновляет позицию в LRU.
func (c *DiskCache) Get(_ context.Context, key string) ([]byte, error) {
	c.mu.Lock() // Используем Lock, так как мы модифицируем очередь (MoveToFront)
	defer c.mu.Unlock()

	e, exists := c.items[key]
	if !exists {
		return nil, ErrNotFound
	}

	// Элемент используется, перемещаем его в начало (самый свежий)
	c.queue.MoveToFront(e.listItem)

	data, err := os.ReadFile(c.path(key))
	if err != nil {
		if os.IsNotExist(err) {
			// Файл пропал с диска вручную — чистим метаданные
			c.removeEntry(e)
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read cache file: %w", err)
	}

	return data, nil
}

// Сохраняет данные в кэш. При превышении лимита вытесняет старые элементы.
func (c *DiskCache) Set(_ context.Context, key string, data []byte) error {
	if len(data) == 0 {
		return errors.New("cannot cache empty data")
	}

	newSize := int64(len(data))
	if newSize > c.maxSize {
		return fmt.Errorf("item size %d exceeds max cache size %d", newSize, c.maxSize)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Если ключ уже есть — обновляем размер и позицию
	if e, exists := c.items[key]; exists {
		oldSize := e.size
		e.size = newSize
		c.queue.MoveToFront(e.listItem)
		c.currentSize += newSize - oldSize

		if err := c.atomicWrite(key, data); err != nil {
			return err
		}
		c.evict()
		return nil
	}

	// Новый элемент: сначала атомарно пишем на диск
	if err := c.atomicWrite(key, data); err != nil {
		return err
	}

	// Добавляем в LRU
	e := &entry{key: key, size: newSize}
	e.listItem = c.queue.PushFront(e)
	c.items[key] = e
	c.currentSize += newSize

	// Вытесняем старые, если превысили лимит
	c.evict()
	return nil
}

// Проверяет наличие ключа в кэше (быстрая проверка без чтения с диска).
func (c *DiskCache) Has(_ context.Context, key string) (bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, exists := c.items[key]
	return exists, nil
}

// Возвращает текущую статистику (для тестов или метрик).
func (c *DiskCache) Stats() (items int, sizeBytes int64, maxSize int64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items), c.currentSize, c.maxSize
}

// Вытесняет наименее недавно использованные элементы.
// Важно: вызывается только под c.mu.Lock()!
func (c *DiskCache) evict() {
	for c.currentSize > c.maxSize {
		back := c.queue.Back()
		if back == nil {
			break
		}
		e, ok := back.Value.(*entry)
		if !ok {
			break
		}
		c.removeEntry(e)
	}
}

// Удаляет элемент из памяти и с диска.
// Важно: вызывается только под c.mu.Lock()!
func (c *DiskCache) removeEntry(e *entry) {
	_ = os.Remove(c.path(e.key)) // Игнорируем ошибки удаления
	c.queue.Remove(e.listItem)
	delete(c.items, e.key)
	c.currentSize -= e.size
	if c.currentSize < 0 {
		c.currentSize = 0
	}
}

// Атомарно записывает данные: .tmp -> rename.
func (c *DiskCache) atomicWrite(key string, data []byte) error {
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

func (c *DiskCache) path(key string) string {
	return filepath.Join(c.dir, key)
}
