package resizer

import (
	"bytes"
	"errors"
	"fmt"
	"image/jpeg"
	"image/png"

	"github.com/disintegration/imaging"
)

const (
	DefaultQuality = 85
	MaxDimension   = 10000
)

var (
	ErrInvalidDimensions = errors.New("invalid target dimensions")
	ErrUnsupportedFormat = errors.New("unsupported or invalid image format")
	ErrTooLarge          = errors.New("source image dimensions exceed maximum")
	ErrDecode            = errors.New("failed to decode image")
)

type Resizer struct {
	quality int
}

type Option func(*Resizer)

func WithQuality(q int) Option {
	return func(r *Resizer) {
		if q < 1 {
			q = 1
		}
		if q > 100 {
			q = 100
		}
		r.quality = q
	}
}

func NewResizer(opts ...Option) *Resizer {
	r := &Resizer{quality: DefaultQuality}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Применяет алгоритм fill и сохраняет формат (только JPEG или PNG).
func (r *Resizer) Fill(src []byte, width, height int) ([]byte, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("%w: %dx%d", ErrInvalidDimensions, width, height)
	}
	if width > MaxDimension || height > MaxDimension {
		return nil, fmt.Errorf("%w: %dx%d > %d", ErrTooLarge, width, height, MaxDimension)
	}

	format, err := detectFormat(src)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedFormat, err)
	}

	img, err := imaging.Decode(bytes.NewReader(src), imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecode, err)
	}

	bounds := img.Bounds()
	if bounds.Dx() > MaxDimension || bounds.Dy() > MaxDimension {
		return nil, fmt.Errorf("%w: source %dx%d > %d", ErrTooLarge, bounds.Dx(), bounds.Dy(), MaxDimension)
	}

	resized := imaging.Fill(img, width, height, imaging.Center, imaging.Lanczos)

	buf := &bytes.Buffer{}
	switch format {
	case "image/jpeg":
		err = jpeg.Encode(buf, resized, &jpeg.Options{Quality: r.quality})
	case "image/png":
		err = png.Encode(buf, resized)
	default:
		return nil, ErrUnsupportedFormat
	}

	if err != nil {
		return nil, fmt.Errorf("encode image: %w", err)
	}

	return buf.Bytes(), nil
}

// Проверяет magic bytes (только JPEG и PNG).
func detectFormat(data []byte) (string, error) {
	if len(data) < 4 {
		return "", errors.New("file too small")
	}

	// JPEG: FF D8 FF
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg", nil
	}
	// PNG: 89 50 4E 47
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return "image/png", nil
	}

	return "", errors.New("unrecognized magic bytes")
}
