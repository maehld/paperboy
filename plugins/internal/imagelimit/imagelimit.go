// Package imagelimit decodes an image from an untrusted source only when its
// declared size stays within a pixel budget. A header can name a huge canvas
// while the download stays small, so the dimensions are checked before the
// pixels are allocated.
package imagelimit

import (
	"bytes"
	"fmt"
	"image"
)

// MaxPixels is the pixel budget of a downloaded image. A display draws at most
// a few megapixels, and artwork above this is not worth the memory.
const MaxPixels = 16 << 20

// Decode reads data as an image and refuses one above MaxPixels. The caller
// imports the format decoders it expects.
func Decode(data []byte) (image.Image, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return nil, fmt.Errorf("image is %dx%d, more than %d pixels", cfg.Width, cfg.Height, MaxPixels)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}
