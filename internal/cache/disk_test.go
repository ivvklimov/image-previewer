package cache

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKeyOf(t *testing.T) {
	k1 := KeyOf("https://example.com/a.jpg", 300, 200)
	k2 := KeyOf("https://example.com/a.jpg", 300, 200)
	k3 := KeyOf("https://example.com/a.jpg", 300, 201)

	assert.Equal(t, k1, k2, "same input -> same key")
	assert.NotEqual(t, k1, k3, "different input -> different key")
	assert.Len(t, k1, 64, "SHA256 hex = 64 chars")
}

func TestDiskCache_GetMiss(t *testing.T) {
	c := NewDiskCache(t.TempDir())
	_, err := c.Get(context.Background(), "missing")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestDiskCache_HasMiss(t *testing.T) {
	c := NewDiskCache(t.TempDir())
	ok, err := c.Has(context.Background(), "missing")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestDiskCache_SetThenGet(t *testing.T) {
	c := NewDiskCache(t.TempDir())
	ctx := context.Background()
	data := []byte{0xFF, 0xD8, 0xFF, 0xE0} // JPEG magic

	require.NoError(t, c.Set(ctx, "k1", data))

	got, err := c.Get(ctx, "k1")
	require.NoError(t, err)
	assert.Equal(t, data, got)

	ok, err := c.Has(ctx, "k1")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestDiskCache_SetEmptyReturnsError(t *testing.T) {
	c := NewDiskCache(t.TempDir())
	err := c.Set(context.Background(), "empty", nil)
	require.Error(t, err)

	// .tmp не должен остаться
	files, _ := filepath.Glob(filepath.Join(c.dir, "*.tmp"))
	assert.Empty(t, files)
}

func TestDiskCache_ConcurrentSet(t *testing.T) {
	c := NewDiskCache(t.TempDir())
	ctx := context.Background()

	done := make(chan struct{}, 10)
	for i := 0; i < 10; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			_ = c.Set(ctx, "shared", []byte{byte(i)})
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}

	ok, err := c.Has(ctx, "shared")
	require.NoError(t, err)
	assert.True(t, ok, "атомарный rename гарантирует существование файла")

	files, _ := filepath.Glob(filepath.Join(c.dir, "*.tmp"))
	assert.Empty(t, files, ".tmp не должны оставаться после конкурентной записи")
}

func TestDiskCache_OverwriteExisting(t *testing.T) {
	c := NewDiskCache(t.TempDir())
	ctx := context.Background()

	require.NoError(t, c.Set(ctx, "k", []byte("v1")))
	require.NoError(t, c.Set(ctx, "k", []byte("v2")))

	got, err := c.Get(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, []byte("v2"), got)
}

func TestDiskCache_DirNotExists_ReturnsError(t *testing.T) {
	c := NewDiskCache("/nonexistent/path/for/test")
	_, err := c.Get(context.Background(), "k")
	require.Error(t, err)
}

func TestDiskCache_FileExistsButEmpty(t *testing.T) {
	dir := t.TempDir()
	c := NewDiskCache(dir)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "k"), nil, 0644))

	got, err := c.Get(context.Background(), "k")
	require.NoError(t, err)
	assert.Empty(t, got)
}
