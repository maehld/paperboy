package page

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Icons are PNG pictograms from the instance's icons directory (ADR 0038). A
// loaded file becomes a mask: opaque dark pixels are ink, everything else is
// background, so the same asset renders on 1-bit and 4-gray panels.

const (
	defaultIconSize  = 0.85 // fraction of the region height
	defaultIconLabel = 0.6  // fraction of the label height
	iconLabelHeight  = 0.22 // fraction of the region height reserved for the caption
)

func (i *Icon) validate() error {
	if strings.TrimSpace(i.Source) == "" {
		return errors.New("source missing")
	}
	switch i.Align {
	case "", "left", "center", "right":
	default:
		return fmt.Errorf("align %q: expected left, center or right", i.Align)
	}
	if i.Size < 0 || i.Size > 2 {
		return fmt.Errorf("size %g: expected a fraction of the region height, e.g. 0.85", i.Size)
	}
	if i.LabelSize < 0 || i.LabelSize > 2 {
		return fmt.Errorf("label_size %g: expected a fraction, e.g. 0.6", i.LabelSize)
	}
	switch i.LabelWeight {
	case "", "regular", "bold":
	default:
		return fmt.Errorf("label_weight %q: expected regular or bold", i.LabelWeight)
	}
	return nil
}

// mask is a decoded icon: one bit per pixel, true = ink.
type mask struct {
	w, h int
	ink  []bool
}

func (m *mask) at(x, y int) bool {
	if x < 0 || y < 0 || x >= m.w || y >= m.h {
		return false
	}
	return m.ink[y*m.w+x]
}

// maskOf converts an image to ink. A pixel counts as ink when it is opaque and
// dark, which covers pictures with a transparent background as well as
// black-on-white ones.
func maskOf(img image.Image) *mask {
	b := img.Bounds()
	m := &mask{w: b.Dx(), h: b.Dy(), ink: make([]bool, b.Dx()*b.Dy())}
	for y := 0; y < m.h; y++ {
		for x := 0; x < m.w; x++ {
			n := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			if n.A < 128 {
				continue // transparent
			}
			lum := (299*int(n.R) + 587*int(n.G) + 114*int(n.B)) / 1000
			m.ink[y*m.w+x] = lum < 128
		}
	}
	return m
}

// trim crops the mask to its ink so that transparent or white margins of an
// asset do not shrink the drawn pictogram.
func (m *mask) trim() *mask {
	minX, minY, maxX, maxY := m.w, m.h, -1, -1
	for y := 0; y < m.h; y++ {
		for x := 0; x < m.w; x++ {
			if !m.ink[y*m.w+x] {
				continue
			}
			minX, minY = min(minX, x), min(minY, y)
			maxX, maxY = max(maxX, x), max(maxY, y)
		}
	}
	if maxX < 0 {
		return nil
	}
	w, h := maxX-minX+1, maxY-minY+1
	if w == m.w && h == m.h {
		return m
	}
	out := &mask{w: w, h: h, ink: make([]bool, w*h)}
	for y := 0; y < h; y++ {
		copy(out.ink[y*w:(y+1)*w], m.ink[(minY+y)*m.w+minX:])
	}
	return out
}

// iconKey identifies a decoded icon; the modification time replaces an asset
// after it was edited.
type iconKey struct {
	path string
	mod  int64
}

var (
	iconMu    sync.Mutex
	iconCache = map[iconKey]*mask{}
)

// loadIcon decodes the PNG at path, reusing the mask of an unchanged file.
func loadIcon(path string) (*mask, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	key := iconKey{path: path, mod: info.ModTime().UnixNano()}
	iconMu.Lock()
	cached := iconCache[key]
	iconMu.Unlock()
	if cached != nil {
		return cached, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	m := maskOf(img).trim()
	if m == nil {
		return nil, fmt.Errorf("%s: the picture contains no ink", path)
	}
	iconMu.Lock()
	if len(iconCache) >= 128 { // an instance has a handful of icons; keep the map bounded
		iconCache = map[iconKey]*mask{}
	}
	iconCache[key] = m
	iconMu.Unlock()
	return m, nil
}

// iconPath resolves a source template result inside the icons directory. Paths
// that leave the directory are rejected.
func iconPath(dir, source string) (string, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", errors.New("empty source")
	}
	if filepath.IsAbs(source) || filepath.VolumeName(source) != "" ||
		strings.HasPrefix(source, "/") || strings.HasPrefix(source, `\`) {
		return "", fmt.Errorf("%q: absolute paths are not allowed", source)
	}
	clean := filepath.Clean(source)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%q: paths outside the icons directory are not allowed", source)
	}
	return filepath.Join(dir, clean), nil
}

// fit scales the mask to the target height, preserving the aspect ratio.
func (m *mask) fit(height int) (int, int) {
	if height < 1 {
		height = 1
	}
	if m.h < 1 {
		return height, height
	}
	width := int(math.Round(float64(m.w) * float64(height) / float64(m.h)))
	return max(width, 1), height
}

// drawIcon paints the mask scaled to height with its ink colour, aligned inside
// dst. Downscaling uses the majority of the covered source pixels, so thin
// strokes of a large pictogram survive on a small panel.
func drawIcon(dst *image.Paletted, m *mask, dstRect image.Rectangle, height int, align int) {
	w, h := m.fit(height)
	if w > dstRect.Dx() { // a wide pictogram is limited by the width as well
		w = dstRect.Dx()
		h = max(int(math.Round(float64(m.h)*float64(w)/float64(m.w))), 1)
	}
	x0 := dstRect.Min.X
	switch align {
	case alignCenter:
		x0 = dstRect.Min.X + (dstRect.Dx()-w)/2
	case alignRight:
		x0 = dstRect.Max.X - w
	}
	y0 := dstRect.Min.Y + (dstRect.Dy()-h)/2
	if w < 1 || h < 1 {
		return
	}
	for y := 0; y < h; y++ {
		sy0, sy1 := y*m.h/h, (y+1)*m.h/h
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		for x := 0; x < w; x++ {
			sx0, sx1 := x*m.w/w, (x+1)*m.w/w
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			if !m.majority(sx0, sy0, sx1, sy1) {
				continue
			}
			px, py := x0+x, y0+y
			if !image.Pt(px, py).In(dst.Rect) {
				continue
			}
			dst.SetColorIndex(px, py, 0)
		}
	}
}

// majority reports whether at least half of a source rectangle is ink.
func (m *mask) majority(x0, y0, x1, y1 int) bool {
	ink, total := 0, 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			total++
			if m.at(x, y) {
				ink++
			}
		}
	}
	return total > 0 && ink*2 >= total
}

// icon draws the icon block: the picture uses the region minus its padding and
// an optional caption at the bottom (ADR 0038).
func (r *renderer) icon(img *image.Paletted, rc image.Rectangle, b Block) {
	ic := b.Icon
	pad := r.padding(&b, ic.Padding)
	area := image.Rect(rc.Min.X+pad, rc.Min.Y+pad, rc.Max.X-pad, rc.Max.Y-pad)
	if area.Dx() < 1 || area.Dy() < 1 {
		return
	}
	label := r.expand(ic.Label)
	labelHeight := 0
	if label != "" {
		labelHeight = max(int(float64(area.Dy())*iconLabelHeight), 1)
		area.Max.Y -= labelHeight
		if area.Dy() < 1 {
			return
		}
	}

	source := strings.TrimSpace(r.expand(ic.Source))
	path, err := iconPath(r.t.iconsDir(), source)
	if err != nil {
		r.fail(fmt.Errorf("icon: %w", err))
		return
	}
	m, err := loadIcon(path)
	if err != nil {
		r.fail(fmt.Errorf("icon: %w", err))
		return
	}
	size := ic.Size
	if size <= 0 {
		size = defaultIconSize
	}
	drawIcon(img, m, area, max(int(float64(area.Dy())*size), 1), parseAlign(ic.Align, alignCenter))

	if label == "" {
		return
	}
	labelSize := ic.LabelSize
	if labelSize <= 0 {
		labelSize = defaultIconLabel
	}
	st := style(labelSize, float64(labelHeight), defaultIconLabel, ic.LabelWeight, false, nil)
	baseline := rc.Max.Y - pad
	drawStyled(img, label, rc.Min.X+rc.Dx()/2, baseline, st, rc.Dx()-2*pad,
		color.Black, parseAlign(ic.Align, alignCenter))
}
