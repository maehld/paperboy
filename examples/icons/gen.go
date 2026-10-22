//go:build ignore

// Command gen draws the sample icons of examples/icons as monochrome PNGs. It
// is not part of the build; run it after changing a shape:
//
//	go run examples/icons/gen.go -out examples/icons
//
// The pictures are drawn at four times the output size and box-filtered down,
// so the mask threshold in Paperboy (ADR 0038) keeps clean edges. The ink is
// black with alpha, the background is transparent.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

// size is the edge length of an output icon and ss the supersampling factor.
const (
	size = 256
	ss   = 4
)

// canvas is a monochrome drawing surface in normalized coordinates: -1..1 on
// both axes, y pointing down.
type canvas struct {
	w, h int
	pix  []bool
}

func newCanvas() *canvas {
	return &canvas{w: size * ss, h: size * ss, pix: make([]bool, size*ss*size*ss)}
}

// px maps a normalized coordinate to a pixel.
func (c *canvas) px(x, y float64) (int, int) {
	return int((x + 1) / 2 * float64(c.w)), int((y + 1) / 2 * float64(c.h))
}

func (c *canvas) set(x, y int) {
	if x >= 0 && y >= 0 && x < c.w && y < c.h {
		c.pix[y*c.w+x] = true
	}
}

// disk fills a circle.
func (c *canvas) disk(cx, cy, r float64) {
	x0, y0 := c.px(cx-r, cy-r)
	x1, y1 := c.px(cx+r, cy+r)
	cx0, cy0 := c.px(cx, cy)
	rx := float64(x1-x0) / 2
	ry := float64(y1-y0) / 2
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			dx := (float64(x) - float64(cx0)) / rx
			dy := (float64(y) - float64(cy0)) / ry
			if dx*dx+dy*dy <= 1 {
				c.set(x, y)
			}
		}
	}
}

// rect fills an axis-aligned rectangle.
func (c *canvas) rect(x0, y0, x1, y1 float64) {
	px0, py0 := c.px(x0, y0)
	px1, py1 := c.px(x1, y1)
	for y := py0; y <= py1; y++ {
		for x := px0; x <= px1; x++ {
			c.set(x, y)
		}
	}
}

// poly fills a polygon with the even-odd rule.
func (c *canvas) poly(pts [][2]float64) {
	minY, maxY := math.Inf(1), math.Inf(-1)
	for _, p := range pts {
		minY = math.Min(minY, p[1])
		maxY = math.Max(maxY, p[1])
	}
	y0, _ := c.px(0, minY)
	_, yEnd := c.px(0, maxY)
	for y := y0; y <= yEnd; y++ {
		cy := (float64(y)+0.5)/float64(c.h)*2 - 1
		var xs []float64
		for i := range pts {
			a, b := pts[i], pts[(i+1)%len(pts)]
			if (a[1] <= cy) == (b[1] <= cy) {
				continue
			}
			xs = append(xs, a[0]+(cy-a[1])*(b[0]-a[0])/(b[1]-a[1]))
		}
		for i := 0; i+1 < len(xs); i += 2 {
			x0, _ := c.px(xs[i], 0)
			x1, _ := c.px(xs[i+1], 0)
			for x := x0; x <= x1; x++ {
				c.set(x, y)
			}
		}
	}
}

// thick draws a line of the given thickness.
func (c *canvas) thick(x0, y0, x1, y1, t float64) {
	length := math.Hypot(x1-x0, y1-y0)
	steps := int(length / 0.004)
	if steps < 1 {
		steps = 1
	}
	for i := 0; i <= steps; i++ {
		f := float64(i) / float64(steps)
		c.disk(x0+(x1-x0)*f, y0+(y1-y0)*f, t/2)
	}
}

// png converts the canvas to a black-on-transparent picture, cropped to the ink
// with a small margin so that the icon fills the region it is drawn into.
func (c *canvas) png() *image.NRGBA {
	minX, minY, maxX, maxY := c.w, c.h, -1, -1
	for y := 0; y < c.h; y++ {
		for x := 0; x < c.w; x++ {
			if !c.pix[y*c.w+x] {
				continue
			}
			minX, minY = min(minX, x), min(minY, y)
			maxX, maxY = max(maxX, x), max(maxY, y)
		}
	}
	if maxX < 0 {
		panic("empty icon")
	}
	margin := 2 * ss
	minX, minY = max(minX-margin, 0), max(minY-margin, 0)
	maxX, maxY = min(maxX+margin, c.w-1), min(maxY+margin, c.h-1)
	w, h := (maxX-minX+1)/ss, (maxY-minY+1)/ss
	if w < 1 || h < 1 {
		panic("icon too small")
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			ink := 0
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					if c.pix[(minY+y*ss+sy)*c.w+minX+x*ss+sx] {
						ink++
					}
				}
			}
			a := uint8(ink * 255 / (ss * ss))
			img.SetNRGBA(x, y, color.NRGBA{0, 0, 0, a})
		}
	}
	return img
}

// ---- motifs ----

// sun draws a disk with eight rays.
func sun() *canvas {
	c := newCanvas()
	c.disk(0, 0, 0.30)
	for i := 0; i < 8; i++ {
		a := float64(i) * math.Pi / 4
		c.thick(0.42*math.Cos(a), 0.42*math.Sin(a), 0.58*math.Cos(a), 0.58*math.Sin(a), 0.07)
	}
	return c
}

// cloudAt draws a cloud whose bottom edge sits at y.
func cloudAt(c *canvas, y float64) {
	c.disk(-0.22, y-0.14, 0.18)
	c.disk(0.02, y-0.24, 0.24)
	c.disk(0.26, y-0.12, 0.17)
	c.rect(-0.30, y-0.20, 0.34, y)
	c.disk(-0.30, y-0.06, 0.07)
	c.disk(0.34, y-0.06, 0.07)
}

func cloud() *canvas {
	c := newCanvas()
	cloudAt(c, 0.14)
	return c
}

func rain() *canvas {
	c := newCanvas()
	cloudAt(c, -0.10)
	for _, x := range []float64{-0.22, 0.0, 0.22} {
		c.thick(x+0.04, 0.10, x-0.06, 0.42, 0.055)
	}
	return c
}

func snow() *canvas {
	c := newCanvas()
	cloudAt(c, -0.10)
	for _, p := range [][2]float64{{-0.24, 0.28}, {0.0, 0.20}, {0.24, 0.28}} {
		for i := 0; i < 3; i++ {
			a := float64(i) * math.Pi / 3
			c.thick(p[0]-0.08*math.Cos(a), p[1]-0.08*math.Sin(a),
				p[0]+0.08*math.Cos(a), p[1]+0.08*math.Sin(a), 0.035)
		}
	}
	return c
}

func thunder() *canvas {
	c := newCanvas()
	cloudAt(c, -0.12)
	c.poly([][2]float64{
		{0.06, -0.08}, {-0.10, 0.18}, {-0.01, 0.18}, {-0.08, 0.44},
		{0.16, 0.10}, {0.05, 0.10}, {0.13, -0.08},
	})
	return c
}

// shirt draws a T-shirt body with short sleeves.
func shirt() *canvas {
	c := newCanvas()
	c.poly([][2]float64{
		{-0.13, -0.45}, {-0.31, -0.38}, {-0.50, -0.20}, {-0.44, -0.06},
		{-0.27, -0.14}, {-0.27, 0.45}, {0.27, 0.45}, {0.27, -0.14},
		{0.44, -0.06}, {0.50, -0.20}, {0.31, -0.38}, {0.13, -0.45},
	})
	return c
}

// pullover draws a long-sleeved sweater with a ribbed hem.
func pullover() *canvas {
	c := newCanvas()
	c.poly([][2]float64{
		{-0.13, -0.45}, {-0.31, -0.38}, {-0.52, -0.18}, {-0.50, 0.16},
		{-0.37, 0.20}, {-0.28, 0.00}, {-0.28, 0.36}, {0.28, 0.36},
		{0.28, 0.00}, {0.37, 0.20}, {0.50, 0.16}, {0.52, -0.18},
		{0.31, -0.38}, {0.13, -0.45},
	})
	c.rect(-0.30, 0.36, 0.30, 0.47)  // ribbed hem
	c.rect(-0.52, 0.10, -0.42, 0.21) // cuffs
	c.rect(0.42, 0.10, 0.52, 0.21)
	return c
}

func main() {
	out := flag.String("out", ".", "directory for the generated PNGs")
	flag.Parse()
	motifs := map[string]*canvas{
		"sun": sun(), "cloud": cloud(), "rain": rain(), "snow": snow(),
		"thunder": thunder(), "tshirt": shirt(), "pullover": pullover(),
	}
	names := []string{"sun", "cloud", "rain", "snow", "thunder", "tshirt", "pullover"}
	for _, name := range names {
		path := filepath.Join(*out, name+".png")
		f, err := os.Create(path)
		if err != nil {
			fail(err)
		}
		enc := png.Encoder{CompressionLevel: png.BestCompression}
		if err := enc.Encode(f, motifs[name].png()); err != nil {
			f.Close()
			fail(err)
		}
		if err := f.Close(); err != nil {
			fail(err)
		}
		fmt.Println("wrote", path)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen:", err)
	os.Exit(1)
}
