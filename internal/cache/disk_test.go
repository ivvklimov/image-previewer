package cache

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiskCache_BasicSetGet(t *testing.T) {
	dir := t.TempDir()
	c := NewDiskCache(dir, 1024)
	ctx := context.Background()

	data := []byte("hello world")
	require.NoError(t, c.Set(ctx, "k1", data))

	got, err := c.Get(ctx, "k1")
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

func TestDiskCache_GetMiss(t *testing.T) {
	c := NewDiskCache(t.TempDir(), 1024)
	_, err := c.Get(context.Background(), "missing")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestDiskCache_LRUEviction_ByBytes(t *testing.T) {
	dir := t.TempDir()
	// Устанавливаем жесткий лимит: 100 байт
	c := NewDiskCache(dir, 100)
	ctx := context.Background()

	data40 := bytes.Repeat([]byte("a"), 40)

	// 1. Добавляем первый файл (40 байт)
	require.NoError(t, c.Set(ctx, "k1", data40))
	items, size, maxSize := c.Stats()
	t.Logf("После k1: файлов=%d, размер=%d байт (лимит=%d)", items, size, maxSize)

	// 2. Добавляем второй файл (еще 40 байт, итого 80)
	require.NoError(t, c.Set(ctx, "k2", data40))
	items, size, maxSize = c.Stats()
	t.Logf("После k2: файлов=%d, размер=%d байт (лимит=%d)", items, size, maxSize)

	// 3. Добавляем третий файл (еще 40 байт, итого 120 -> Превышение!)
	// В этот момент должен сработать evict() и удалить k1
	require.NoError(t, c.Set(ctx, "k3", data40))
	items, size, maxSize = c.Stats()
	t.Logf("После k3 (должен вытеснить k1): файлов=%d, размер=%d байт (лимит=%d)", items, size, maxSize)

	// --- Проверки ---

	// k1 должен быть вытеснен (ошибка ErrNotFound)
	_, err := c.Get(ctx, "k1")
	require.ErrorIs(t, err, ErrNotFound, "k1 должен быть вытеснен из памяти")

	// k2 и k3 должны остаться
	_, err = c.Get(ctx, "k2")
	require.NoError(t, err)
	_, err = c.Get(ctx, "k3")
	require.NoError(t, err)

	// Самое главное: проверяем, что файл k1 физически удален с диска
	_, err = os.Stat(filepath.Join(dir, "k1"))
	require.True(t, os.IsNotExist(err), "файл k1 должен быть физически удален с диска при eviction")
}

func TestDiskCache_LRU_OrderAfterGet(t *testing.T) {
	dir := t.TempDir()
	c := NewDiskCache(dir, 100)
	ctx := context.Background()

	data30 := bytes.Repeat([]byte("b"), 30)

	require.NoError(t, c.Set(ctx, "k1", data30)) // [k1]
	require.NoError(t, c.Set(ctx, "k2", data30)) // [k2, k1]
	require.NoError(t, c.Set(ctx, "k3", data30)) // [k3, k2, k1]

	// Обращаемся к k1 — он становится самым свежим
	_, err := c.Get(ctx, "k1")
	require.NoError(t, err)
	// Порядок: [k1, k3, k2] -> k2 самый старый

	// Добавляем k4 — должен вытолкнуть k2
	require.NoError(t, c.Set(ctx, "k4", data30))

	_, err = c.Get(ctx, "k2")
	require.ErrorIs(t, err, ErrNotFound, "k2 должен быть вытеснен")
}

func TestDiskCache_ConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	c := NewDiskCache(dir, 10<<20) // 10 MB, чтобы не триггерить eviction
	ctx := context.Background()

	wg := &sync.WaitGroup{}
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 10_000; i++ {
			key := "key_" + strconv.Itoa(i)
			data := bytes.Repeat([]byte("a"), 100)
			_ = c.Set(ctx, key, data)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 10_000; i++ {
			key := "key_" + strconv.Itoa(i%1000)
			_, _ = c.Get(ctx, key)
		}
	}()

	wg.Wait()

	items, size, maxSize := c.Stats()
	assert.Greater(t, items, 0)
	assert.LessOrEqual(t, size, maxSize)
}

// Проверяет, что при холодном старте существующие файлы на диске
// корректно восстанавливаются в LRU-очередь и учитываются в лимите.
func TestDiskCache_RestoreFromDisk_Basic(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// === ФАЗА 1: "Первый запуск сервера" ===
	// Создаём кэш с лимитом 100 байт и добавляем 2 файла по 40 байт
	c1 := NewDiskCache(dir, 100)

	data40 := bytes.Repeat([]byte("a"), 40)
	require.NoError(t, c1.Set(ctx, "a", data40))

	// Небольшая задержка, чтобы гарантировать разницу в ModTime
	// (на случай, если файловая система имеет низкую точность времени)
	time.Sleep(10 * time.Millisecond)

	require.NoError(t, c1.Set(ctx, "b", data40))

	items, size, _ := c1.Stats()
	require.Equal(t, 2, items)
	require.Equal(t, int64(80), size)

	// Файлы физически на диске
	_, err := os.Stat(filepath.Join(dir, "a"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, "b"))
	require.NoError(t, err)

	// === ФАЗА 2: "Перезапуск сервера" (холодный старт) ===
	// Создаём НОВЫЙ экземпляр кэша на той же директории
	c2 := NewDiskCache(dir, 100)

	// Проверяем, что restoreFromDisk восстановил состояние
	items, size, _ = c2.Stats()
	require.Equal(t, 2, items, "после рестарта должно быть 2 файла")
	require.Equal(t, int64(80), size, "суммарный размер должен восстановиться")

	// Get должен работать для восстановленных ключей
	got, err := c2.Get(ctx, "a")
	require.NoError(t, err)
	require.Equal(t, data40, got)

	// === ФАЗА 3: Проверяем LRU-порядок ===
	// restoreFromDisk сортирует файлы по ModTime (от старых к новым)
	// и добавляет их через PushBack.
	// Файл "a" создан раньше "b", поэтому его ModTime меньше.
	// После PushBack: Front → a -> b -> Back.
	// Самый старый (Back) - это "b".
	// Добавим новый файл "c" - он должен вытеснить "b" (самый старый)
	data30 := bytes.Repeat([]byte("b"), 30)
	require.NoError(t, c2.Set(ctx, "c", data30)) // 80 + 30 = 110 > 100

	// "b" должен быть вытеснен (он самый старый из восстановленных)
	_, err = c2.Get(ctx, "b")
	require.ErrorIs(t, err, ErrNotFound, "b должен быть вытеснен как самый старый")

	// "a" и "c" должны остаться
	_, err = c2.Get(ctx, "a")
	require.NoError(t, err)
	_, err = c2.Get(ctx, "c")
	require.NoError(t, err)

	// Файл "b" физически удалён с диска
	_, err = os.Stat(filepath.Join(dir, "b"))
	require.True(t, os.IsNotExist(err), "файл b должен быть удалён с диска")
}

// Проверяет, что если после рестарта суммарный размер файлов на диске уже превышает лимит
// (например, лимит уменьшили в конфиге), то restoreFromDisk сразу подчищает лишнее.
func TestDiskCache_RestoreFromDisk_OverLimit(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// === ФАЗА 1: Записываем файлы с большим лимитом ===
	c1 := NewDiskCache(dir, 1000) // лимит 1000 байт

	data50 := bytes.Repeat([]byte("x"), 50)
	require.NoError(t, c1.Set(ctx, "k1", data50))
	require.NoError(t, c1.Set(ctx, "k2", data50))
	require.NoError(t, c1.Set(ctx, "k3", data50))

	items, size, _ := c1.Stats()
	require.Equal(t, 3, items)
	require.Equal(t, int64(150), size)

	// === ФАЗА 2: "Перезапуск" с уменьшенным лимитом ===
	// На диске 150 байт, а новый лимит - всего 100 байт
	c2 := NewDiskCache(dir, 100)

	// restoreFromDisk должен сразу вызвать evict и удалить старые файлы
	items, size, _ = c2.Stats()
	require.LessOrEqual(t, size, int64(100), "размер после восстановления не должен превышать лимит")
	require.LessOrEqual(t, items, 2, "количество файлов должно уменьшиться")

	// Физически на диске размер тоже должен быть <= 100 байт
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var totalOnDisk int64
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		info, err := e.Info()
		require.NoError(t, err)
		totalOnDisk += info.Size()
	}
	require.LessOrEqual(t, totalOnDisk, int64(100), "файлы на диске должны быть удалены")
}
