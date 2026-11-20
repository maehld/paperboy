package imagelimit

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

// pngHeader returns a PNG that declares the given size in its header and has no
// image data. A decoder reads the dimensions from it before it allocates.
func pngHeader(t *testing.T, width, height uint32) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], width)
	binary.BigEndian.PutUint32(ihdr[4:], height)
	ihdr[8] = 8 // bit depth
	ihdr[9] = 2 // color type: truecolour
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], 13)
	buf.Write(length[:])
	buf.WriteString("IHDR")
	buf.Write(ihdr)
	var sum [4]byte
	binary.BigEndian.PutUint32(sum[:], crc32.ChecksumIEEE(append([]byte("IHDR"), ihdr...)))
	buf.Write(sum[:])
	return buf.Bytes()
}

func TestDecodeRejectsAHugeHeader(t *testing.T) {
	data := pngHeader(t, 100000, 100000)
	if _, err := Decode(data); err == nil || !strings.Contains(err.Error(), "pixels") {
		t.Fatalf("huge image: %v", err)
	}
}

func TestDecodeAcceptsASmallImage(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	img.Set(1, 1, color.NRGBA{R: 0x10, G: 0x20, B: 0x30, A: 0xff})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	got, err := Decode(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if b := got.Bounds(); b.Dx() != 3 || b.Dy() != 2 {
		t.Fatalf("image is %dx%d", b.Dx(), b.Dy())
	}
}
