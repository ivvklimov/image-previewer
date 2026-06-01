package resizer

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// === HELPERS ===

func makeJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	buf := &bytes.Buffer{}
	require.NoError(t, jpeg.Encode(buf, img, &jpeg.Options{Quality: 75}))
	return buf.Bytes()
}

func makePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	buf := &bytes.Buffer{}
	require.NoError(t, png.Encode(buf, img))
	return buf.Bytes()
}

func makeFakeExe(t *testing.T) []byte {
	t.Helper()
	return []byte{0x4D, 0x5A, 0x90, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
}

// === TESTS ===

func TestResizer_Fill_PreservesFormat(t *testing.T) {
	r := NewResizer()

	t.Run("JPEG to JPEG", func(t *testing.T) {
		src := makeJPEG(t, 600, 300)
		dst, err := r.Fill(src, 200, 200)
		require.NoError(t, err)
		assert.True(t, bytes.HasPrefix(dst, []byte{0xFF, 0xD8, 0xFF}), "должен начинаться с JPEG magic bytes")
	})

	t.Run("PNG to PNG", func(t *testing.T) {
		src := makePNG(t, 600, 300)
		dst, err := r.Fill(src, 200, 200)
		require.NoError(t, err)
		assert.True(t, bytes.HasPrefix(dst, []byte{0x89, 0x50, 0x4E, 0x47}), "должен начинаться с PNG magic bytes")
	})
}

func TestResizer_Fill_UnsupportedFormat_Rejected(t *testing.T) {
	r := NewResizer()
	exeData := makeFakeExe(t)
	_, err := r.Fill(exeData, 100, 100)

	require.Error(t, err)
	require.ErrorIs(t, err, ErrUnsupportedFormat)
}

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantFmt string
		wantErr bool
	}{
		// Только JPEG и PNG
		{"valid jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, "image/jpeg", false},
		{"valid png", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x00}, "image/png", false},
		{"fake exe", []byte{0x4D, 0x5A, 0x90, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, "", true},
		{"too short", []byte{0xFF, 0xD8}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fmt, err := detectFormat(tt.data)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantFmt, fmt)
			}
		})
	}
}

func TestResizer_Fill_Success_HorizontalToSquare(t *testing.T) {
	r := NewResizer()
	src := makeJPEG(t, 600, 300)

	dst, err := r.Fill(src, 200, 200)
	require.NoError(t, err)
	require.NotEmpty(t, dst)

	img, err := jpeg.Decode(bytes.NewReader(dst))
	require.NoError(t, err)
	b := img.Bounds()
	assert.Equal(t, 200, b.Dx())
	assert.Equal(t, 200, b.Dy())
}

func TestResizer_Fill_InvalidDimensions(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		height int
	}{
		{"zero width", 0, 100},
		{"zero height", 100, 0},
		{"negative width", -10, 100},
	}
	r := NewResizer()
	src := makeJPEG(t, 200, 200)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := r.Fill(src, tt.width, tt.height)
			require.ErrorIs(t, err, ErrInvalidDimensions)
		})
	}
}
