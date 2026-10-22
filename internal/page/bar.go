package page

import (
	"image"
	"image/color"
	"image/draw"
	"math"
)

// bar draws a value as a filled bar or a battery symbol (ADR 0034, section 6).
func (r *renderer) bar(img *image.Paletted, rc image.Rectangle, b *Bar, padding *int) {
	pad := 4
	if padding != nil {
		pad = *padding
	}
	pad = max(pad, 0)
	inner := rc.Inset(pad).Intersect(img.Rect)
	if inner.Empty() {
		return
	}

	ratio := 0.0
	if v, ok := lookupValue(r.root, b.Value); ok {
		if n, ok := toFloat(v); ok && b.Max > b.Min {
			ratio = (n - b.Min) / (b.Max - b.Min)
			ratio = math.Min(math.Max(ratio, 0), 1)
		}
	}

	label := r.expand(b.Label)
	labelSize := fractionSize(b.LabelSize, float64(rc.Dy()), 0.13)
	band := 0
	if label != "" && inner.Dy() >= 2 {
		band = int(math.Ceil(labelSize * 1.3))
		band = min(max(band, 1), inner.Dy()/2)
	}

	body := inner
	if band > 0 {
		body.Max.Y -= band
	}
	if body.Empty() {
		return
	}

	if b.Orientation == "horizontal" {
		r.horizontalBar(img, body, b, ratio)
	} else {
		r.verticalBar(img, body, b, ratio)
	}

	if band > 0 {
		labelBand := image.Rect(inner.Min.X, body.Max.Y, inner.Max.X, inner.Max.Y)
		align := parseAlign(b.Align, alignCenter)
		x := labelBand.Min.X + labelBand.Dx()/2
		switch align {
		case alignLeft:
			x = labelBand.Min.X
		case alignRight:
			x = labelBand.Max.X
		}
		st := textStyle{Size: labelSize, Bold: b.LabelWeight == "bold", Shrink: true}
		baseline := labelBand.Max.Y - int(math.Round(labelSize*0.15))
		drawStyled(img, label, x, baseline, st, labelBand.Dx(), color.Black, align)
	}
}

// verticalBar draws a battery symbol: an optional cap above a bordered body
// that fills upwards.
func (r *renderer) verticalBar(img *image.Paletted, body image.Rectangle, b *Bar, ratio float64) {
	if b.Cap {
		capH := max(2, b.Border)
		if body.Dy()/30 > capH {
			capH = body.Dy() / 30
		}
		capH = min(capH, body.Dy()/4)
		if capH > 0 {
			capW := max(body.Dx()/3, 2)
			x := body.Min.X + (body.Dx()-capW)/2
			fillSolid(img, image.Rect(x, body.Min.Y, x+capW, body.Min.Y+capH))
			body.Min.Y += capH
		}
	}
	if body.Empty() {
		return
	}

	inner := body
	if b.Border > 0 {
		fillSolid(img, body)
		inner = body.Inset(b.Border)
		if inner.Empty() {
			return
		}
		draw.Draw(img, inner, image.NewUniform(color.White), image.Point{}, draw.Src)
	}

	fillH := int(math.Round(float64(inner.Dy()) * ratio))
	if fillH <= 0 || b.Fill == "none" {
		return
	}
	fillH = min(fillH, inner.Dy())
	fillRect(img, image.Rect(inner.Min.X, inner.Max.Y-fillH, inner.Max.X, inner.Max.Y), b.Fill)
}

// horizontalBar draws a simple progress bar that fills from the left.
func (r *renderer) horizontalBar(img *image.Paletted, body image.Rectangle, b *Bar, ratio float64) {
	inner := body
	if b.Border > 0 {
		fillSolid(img, body)
		inner = body.Inset(b.Border)
		if inner.Empty() {
			return
		}
		draw.Draw(img, inner, image.NewUniform(color.White), image.Point{}, draw.Src)
	}

	fillW := int(math.Round(float64(inner.Dx()) * ratio))
	if fillW <= 0 || b.Fill == "none" {
		return
	}
	fillW = min(fillW, inner.Dx())
	fillRect(img, image.Rect(inner.Min.X, inner.Min.Y, inner.Min.X+fillW, inner.Max.Y), b.Fill)
}
