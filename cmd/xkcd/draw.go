package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"

	xdraw "golang.org/x/image/draw"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

func (x *xkcd) Render(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	if w.Widget != "comic" {
		return fmt.Errorf("%w %q", plugin.ErrUnknownWidget, w.Widget)
	}
	levels := render.Levels(img.Palette)
	b := img.Rect.Inset(w.Style.Padding)
	if b.Empty() {
		return nil
	}
	base := req.TextPx(w)
	gap := max(int(req.PtPx(2)), 2)
	args := w.Arg()

	// Title above, alt text below; the comic gets the rest.
	if args.Bool("title", false) {
		size := base
		text := fmt.Sprintf("#%d  %s", x.cur.Num, x.cur.Title)
		render.Text(img, text, b.Min.X, b.Min.Y+int(size*0.9), render.Style{Size: size, Bold: true, Shrink: true}, b.Dx(), color.Black, render.Left)
		b.Min.Y += int(size*1.25) + gap
	}
	if args.Bool("alt", false) && !b.Empty() {
		size := max(base*0.7, req.PtPx(6), render.MinSize)
		step := int(size * 1.2)
		maxLines := min(max(b.Dy()/3/max(step, 1), 1), 8)
		lines := wrapFunc(x.cur.Alt, func(s string) bool { return render.Measure(s, size, false) <= b.Dx() }, 200)
		if len(lines) > maxLines {
			lines = lines[:maxLines]
			lines[maxLines-1] = render.Ellipsize(lines[maxLines-1]+"…", size, false, b.Dx())
		}
		top := b.Max.Y - len(lines)*step
		for i, l := range lines {
			render.Text(img, l, b.Min.X, top+i*step+int(size), render.Style{Size: size, Shrink: true}, b.Dx(), color.Black, render.Left)
		}
		b.Max.Y = top - gap
	}
	if b.Dx() < 4 || b.Dy() < 4 {
		return nil
	}
	fit := fitImage(x.cur.img, b.Dx(), b.Dy())
	r := image.Rectangle{Max: fit.Bounds().Size()}.Add(image.Pt(b.Min.X+(b.Dx()-fit.Bounds().Dx())/2, b.Min.Y+(b.Dy()-fit.Bounds().Dy())/2))
	draw.Draw(img, r, render.Quantize(fit, levels), image.Point{}, draw.Src)
	return nil
}

// fitImage scales src to the largest size within w x h that keeps its aspect
// ratio. Transparent areas become white. The scaling is done on gray values
// with a Catmull-Rom kernel, which averages over the covered source pixels
// when shrinking, so thin lines turn into gray instead of vanishing.
func fitImage(src image.Image, w, h int) *image.Gray {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	flat := image.NewGray(image.Rect(0, 0, sw, sh))
	draw.Draw(flat, flat.Rect, image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(flat, flat.Rect, src, sb.Min, draw.Over)

	dw, dh := w, (sh*w+sw/2)/sw
	if dh > h {
		dw, dh = (sw*h+sh/2)/sh, h
	}
	dw, dh = min(max(dw, 1), w), min(max(dh, 1), h)
	dst := image.NewGray(image.Rect(0, 0, dw, dh))
	xdraw.CatmullRom.Scale(dst, dst.Rect, flat, flat.Rect, xdraw.Src, nil)
	return dst
}
