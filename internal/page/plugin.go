package page

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strings"

	pb "github.com/maehld/paperboy/pkg/render"
)

// PluginBlock places one image fragment of a plugin into a region (spike:
// plugin fragments). Paperboy asks the plugin for exactly the pixel size of
// the region minus padding, so the fragment is never scaled. In YAML the block
// also accepts a scalar: plugin: calendar/month.
type PluginBlock struct {
	Name    string         `yaml:"name"`  // plugin name from paperboy.yaml
	Image   string         `yaml:"image"` // image the plugin offers
	Args    map[string]any `yaml:"args"`
	Padding *int           `yaml:"padding"` // default 0: the plugin owns its margins
}

// FragmentRequest asks for one fragment in an exact size.
type FragmentRequest struct {
	Plugin string
	Image  string
	Args   map[string]any
	Width  int
	Height int
}

// Fragment is the answer for one FragmentRequest.
type Fragment struct {
	Image image.Image // nil when Err is set
	Err   error
	Stale bool // an older fragment, because the plugin failed
}

// FragmentSource renders the fragments of one page; the result has one entry
// per request in the same order. It runs before the page is drawn and outside
// the render lock, because a plugin is an external process.
type FragmentSource func(reqs []FragmentRequest, gray bool) []Fragment

func (p *PluginBlock) validate() error {
	if !ValidName(p.Name) {
		return fmt.Errorf("name %q: only a-z, 0-9 and -", p.Name)
	}
	if !ValidName(p.Image) {
		return fmt.Errorf("image %q: only a-z, 0-9 and -", p.Image)
	}
	if p.Padding != nil && *p.Padding < 0 {
		return fmt.Errorf("padding must not be negative")
	}
	return nil
}

// splitPluginRef turns "calendar/month" into name and image.
func splitPluginRef(s string) (string, string) {
	name, img, _ := strings.Cut(strings.TrimSpace(s), "/")
	return name, img
}

// pluginRect is the area a plugin block draws into.
func (r *renderer) pluginRect(rc image.Rectangle, b *Block) image.Rectangle {
	pad := 0
	if b.Padding != nil {
		pad = *b.Padding
	}
	if b.Plugin.Padding != nil {
		pad = *b.Plugin.Padding
	}
	return rc.Inset(max(pad, 0))
}

// collectFragments walks the layout like the renderer and lists every plugin
// block with its exact pixel area.
func (t *Template) collectFragments(d Display) ([]FragmentRequest, []image.Rectangle) {
	var reqs []FragmentRequest
	var rects []image.Rectangle
	r := &renderer{}
	var visit func(rc image.Rectangle, b *Block)
	visit = func(rc image.Rectangle, b *Block) {
		if rc.Dx() <= 0 || rc.Dy() <= 0 {
			return
		}
		switch {
		case b.Plugin != nil:
			area := r.pluginRect(rc, b)
			if area.Dx() > 0 && area.Dy() > 0 {
				reqs = append(reqs, FragmentRequest{Plugin: b.Plugin.Name, Image: b.Plugin.Image,
					Args: b.Plugin.Args, Width: area.Dx(), Height: area.Dy()})
				rects = append(rects, area)
			}
		case b.Columns != nil:
			for i, rect := range columnRects(rc, b.Columns) {
				visit(rect, &b.Columns.Items[i].Block)
			}
		}
	}
	bands := t.bands(d)
	for i := range t.Blocks {
		if i >= len(bands) {
			break
		}
		visit(bands[i], &t.Blocks[i])
	}
	return reqs, rects
}

// columnRects splits a region into the weighted columns of a columns block.
func columnRects(rc image.Rectangle, c *Columns) []image.Rectangle {
	total := 0
	for _, col := range c.Items {
		total += col.Weight
	}
	if total < 1 {
		return nil
	}
	out := make([]image.Rectangle, len(c.Items))
	x := rc.Min.X
	for i, col := range c.Items {
		w := rc.Dx() * col.Weight / total
		if i == len(c.Items)-1 {
			w = rc.Max.X - x
		}
		out[i] = image.Rect(x, rc.Min.Y, x+w, rc.Max.Y)
		x += w
	}
	return out
}

// plugin pastes the fragment that was fetched for this region. A missing
// fragment becomes a short notice inside the region, so one failing plugin
// does not take the whole page down.
func (r *renderer) plugin(img *image.Paletted, rc image.Rectangle, b Block) {
	area := r.pluginRect(rc, &b)
	if area.Dx() <= 0 || area.Dy() <= 0 {
		return
	}
	f, ok := r.fragments[area]
	label := b.Plugin.Name + "/" + b.Plugin.Image
	switch {
	case !ok:
		r.fragmentNotice(img, area, label, text(r.language, "kein Plugin-Aufruf", "plugin not called"))
		return
	case f.Image == nil:
		msg := text(r.language, "Plugin-Fehler", "plugin error")
		if f.Err != nil {
			msg += ": " + f.Err.Error()
		}
		r.fragmentNotice(img, area, label, msg)
		return
	}
	b2 := f.Image.Bounds()
	if b2.Dx() != area.Dx() || b2.Dy() != area.Dy() {
		r.fragmentNotice(img, area, label, fmt.Sprintf(text(r.language, "falsche Größe %dx%d statt %dx%d", "wrong size %dx%d instead of %dx%d"),
			b2.Dx(), b2.Dy(), area.Dx(), area.Dy()))
		return
	}
	q := pb.Quantize(f.Image, pb.Levels(img.Palette))
	draw.Draw(img, area, q, image.Point{}, draw.Src)
}

func (r *renderer) fragmentNotice(img *image.Paletted, area image.Rectangle, label, msg string) {
	pb.Fill(img, area, "hatch")
	inner := area.Inset(max(area.Dy()/6, 2))
	pb.FillColor(img, inner, color.White)
	size := float64(min(inner.Dy()/3, 24))
	cx := inner.Min.X + inner.Dx()/2
	cy := inner.Min.Y + inner.Dy()/2
	drawStyled(img, label, cx, cy-int(size*0.2), textStyle{Size: size, Bold: true, Shrink: true}, inner.Dx()-8, color.Black, alignCenter)
	drawStyled(img, pb.Ellipsize(msg, size*0.75, false, inner.Dx()-8), cx, cy+int(size*0.9), textStyle{Size: size * 0.75, Shrink: true}, inner.Dx()-8, color.Black, alignCenter)
}
