package paperboy

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

	"github.com/maehld/paperboy/sdk/render"
	"github.com/maehld/paperboy/server/internal/page"
)

// overlayActions draws the action view over the served page and registers it
// as a notice page, so the page cache stays untouched (ADR 0162). A failure
// leaves the page as it is and closes the mode.
func (s *Server) overlayActions(cfg *Config, d *Device, served servedPage, a *actionState, now time.Time, selection bool) (out servedPage) {
	out = served
	defer func() {
		if r := recover(); r != nil {
			slog.Error("action view panicked", "device", d.ID, "panic", r, "stack", string(debug.Stack()))
			a.close()
			out = served
		}
	}()
	if !a.Open || served.name == "" {
		a.close()
		return served
	}
	tmpl, err := s.pages.get(cfg.Pages, served.name, d.Profile)
	if err != nil || tmpl.Actionable() == 0 {
		a.close()
		return served
	}
	if !a.Resolved || a.Page != served.name {
		ctx, cancel := context.WithTimeout(context.Background(), actionBudget)
		opts := s.resolveWidgetActions(ctx, cfg, d, tmpl, now)
		cancel()
		if len(opts) == 0 {
			a.close()
			return served
		}
		a.Options, a.Resolved, a.Page = opts, true, served.name
	}
	a.choose()
	s.mu.Lock()
	rp := s.renderedPageLocked(d.Profile, served.name, served.hash)
	s.mu.Unlock()
	if rp == nil || len(rp.data) == 0 {
		a.close()
		return served
	}
	img, err := png.Decode(bytes.NewReader(rp.data))
	if err != nil {
		a.close()
		return served
	}
	pal, ok := img.(*image.Paletted)
	if !ok || pal.Rect.Dx() != cfg.Profiles[d.Profile].Width || pal.Rect.Dy() != cfg.Profiles[d.Profile].Height {
		a.close()
		return served
	}
	drawActionView(pal, tmpl, cfg.Profiles[d.Profile], cfg.texts(), a, selection)
	out = s.notice(d.Profile, pal)
	out.name, out.page, out.pages = served.name, served.page, served.pages
	out.refresh, out.interval = served.refresh, served.interval
	a.Screen = out.hash
	return out
}

// drawActionView replaces every actionable cell with its option tiles and
// records the tile of each option, so the next tap hits it. selection draws
// the selected option as an inverted tile on a device without a digitiser
// (ADR 0163).
func drawActionView(img *image.Paletted, tmpl *page.Template, p Profile, t texts, a *actionState, selection bool) {
	type tile struct {
		rect     image.Rectangle
		label    string
		disabled bool
		option   int // index into a.Options; -1 for the small-cell notice
	}
	rects := tmpl.Rects(p.Width, p.Height)
	byCell := map[int][]int{}
	for i := range a.Options {
		byCell[a.Options[i].Cell] = append(byCell[a.Options[i].Cell], i)
	}
	var tiles []tile
	for cell, indexes := range byCell {
		if cell < 0 || cell >= len(rects) {
			continue
		}
		area := rects[cell]
		if area.Empty() {
			continue
		}
		render.FillColor(img, area, render.White)
		frameCell(img, area, 2)
		inner := area.Inset(5)
		if inner.Empty() {
			// The cell is too small for a frame and a tile. It stays a
			// disabled tile so a tap cannot run an option blind.
			for _, i := range indexes {
				a.Options[i].Disabled = t.actionTooSmall()
				a.Options[i].Rect = rectOf(area)
			}
			continue
		}
		cols := 2
		if inner.Dx() < inner.Dy()*6/5 {
			cols = 1
		}
		rows := (len(indexes) + cols - 1) / cols
		tw, th := inner.Dx()/cols, inner.Dy()/rows
		if tw < 20 || th < 14 {
			// The cell cannot hold its options. It says so instead of drawing
			// unreadable tiles, and a tap hits only disabled tiles.
			for _, i := range indexes {
				a.Options[i].Disabled = t.actionTooSmall()
				a.Options[i].Rect = rectOf(inner)
			}
			tiles = append(tiles, tile{rect: inner, label: t.actionTooSmall(), disabled: true, option: -1})
			continue
		}
		for n, i := range indexes {
			r, c := n/cols, n%cols
			rect := image.Rect(
				inner.Min.X+c*inner.Dx()/cols+2, inner.Min.Y+r*inner.Dy()/rows+2,
				inner.Min.X+(c+1)*inner.Dx()/cols-2, inner.Min.Y+(r+1)*inner.Dy()/rows-2)
			a.Options[i].Rect = rectOf(rect)
			tiles = append(tiles, tile{rect: rect, label: a.Options[i].Title,
				disabled: a.Options[i].Disabled != "", option: i})
		}
	}
	// The cell that cannot hold its options disabled them above; choose after
	// that pass, so the selection never points at such a tile.
	a.choose()
	for _, t := range tiles {
		drawTile(img, t.rect, t.label, t.disabled, selection && t.option == a.Selected)
	}
}

// drawTile paints one option as a button: a frame, a wrapped label and a
// hatch with a grey label when the plugin cannot run it. The selected tile is
// inverted: a black tile with a white label (ADR 0163).
func drawTile(img *image.Paletted, r image.Rectangle, label string, disabled, selected bool) {
	if r.Empty() {
		return
	}
	switch {
	case selected:
		render.FillColor(img, r, render.Black)
	case disabled:
		render.Fill(img, r, "hatch")
	}
	frameCell(img, r, 1)
	inner := r.Inset(2)
	size := max(render.MinSize, min(float64(inner.Dy())*0.45, float64(inner.Dx())/6.5))
	if inner.Dy() < 18 {
		size = max(render.MinSize, float64(inner.Dy())*0.8)
	}
	lines := wrapMenu(label, inner.Dx()-4, size)
	if len(lines) == 0 && label != "" {
		lines = []string{render.Ellipsize(label, size, false, inner.Dx()-4)}
	}
	lineH := int(size * 1.2)
	top := inner.Min.Y + (inner.Dy()-len(lines)*lineH)/2 + int(size)
	cx := inner.Min.X + inner.Dx()/2
	ink := render.Black
	switch {
	case selected:
		ink = render.White
	case disabled:
		ink = render.DarkGray
		if len(lines) > 0 {
			widest := 0
			for _, line := range lines {
				widest = max(widest, render.Measure(line, size, false))
			}
			plate := image.Rect(cx-widest/2-3, top-lineH+2, cx+widest/2+3, top+lineH*(len(lines)-1)+4)
			render.FillColor(img, plate.Intersect(inner), render.White)
		}
	}
	for _, line := range lines {
		if top > inner.Max.Y-int(size)/2 {
			break
		}
		render.Text(img, line, cx, top, render.Style{Size: size, Shrink: true}, inner.Dx()-4, ink, render.Center)
		top += lineH
	}
}

// rectOf converts an image rectangle to the persisted form.
func rectOf(r image.Rectangle) [4]int { return [4]int{r.Min.X, r.Min.Y, r.Max.X, r.Max.Y} }

// frameCell draws a border of the given thickness inside r.
func frameCell(img *image.Paletted, r image.Rectangle, t int) {
	render.FillColor(img, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+t), render.Black)
	render.FillColor(img, image.Rect(r.Min.X, r.Max.Y-t, r.Max.X, r.Max.Y), render.Black)
	render.FillColor(img, image.Rect(r.Min.X, r.Min.Y, r.Min.X+t, r.Max.Y), render.Black)
	render.FillColor(img, image.Rect(r.Max.X-t, r.Min.Y, r.Max.X, r.Max.Y), render.Black)
}

// wrapMenu breaks a label into at most two lines that fit width pixels.
func wrapMenu(s string, width int, size float64) []string {
	if s == "" {
		return nil
	}
	if render.Measure(s, size, false) <= width {
		return []string{s}
	}
	words := strings.Fields(s)
	var lines []string
	cur := ""
	for _, w := range words {
		try := w
		if cur != "" {
			try = cur + " " + w
		}
		if render.Measure(try, size, false) <= width || cur == "" {
			cur = try
			continue
		}
		lines = append(lines, cur)
		cur = w
		if len(lines) == 2 {
			break
		}
	}
	if len(lines) < 2 && cur != "" {
		lines = append(lines, cur)
	}
	if len(lines) == 2 {
		lines[1] = render.Ellipsize(lines[1], size, false, width)
	}
	return lines
}

// actionWake names the one-shot wake that closes an open action mode.
func actionWake(a actionState, now time.Time, critical bool) *syncWake {
	if !a.Open || critical {
		return nil
	}
	left := a.Deadline.Sub(now)
	if left < time.Second {
		left = time.Second
	}
	return &syncWake{Reason: "action", AfterS: int(left.Seconds())}
}

// fitWakeToListen shortens a listen window that would swallow a one-shot wake:
// the firmware listens before it sleeps, so a wake after after_s seconds would
// arrive listen_s too late. The time it moves out of the window goes into the
// wake, which then fires when it was meant to (ADR 0078, 0162).
func fitWakeToListen(listen time.Duration, wake *syncWake) (time.Duration, *syncWake) {
	if wake == nil || wake.AfterS < 1 {
		return listen, wake
	}
	room := int(listen.Seconds())
	if room > wake.AfterS-1 {
		room = wake.AfterS - 1
	}
	if room < 0 {
		room = 0
	}
	wake.AfterS -= room
	return time.Duration(room) * time.Second, wake
}

// actionFailure draws the failure banner over the served page and registers it
// as a notice page, so the page cache stays untouched (ADR 0162). The next
// sync serves the plain page again.
func (s *Server) actionFailure(cfg *Config, d *Device, served servedPage, message string) (out servedPage) {
	out = served
	defer func() {
		if r := recover(); r != nil {
			slog.Error("action banner panicked", "device", d.ID, "panic", r, "stack", string(debug.Stack()))
			out = served
		}
	}()
	if served.name == "" {
		return served
	}
	s.mu.Lock()
	rp := s.renderedPageLocked(d.Profile, served.name, served.hash)
	s.mu.Unlock()
	if rp == nil || len(rp.data) == 0 {
		return served
	}
	img, err := png.Decode(bytes.NewReader(rp.data))
	if err != nil {
		return served
	}
	pal, ok := img.(*image.Paletted)
	p := cfg.Profiles[d.Profile]
	if !ok || pal.Rect.Dx() != p.Width || pal.Rect.Dy() != p.Height {
		return served
	}
	drawFailureBanner(pal, p, cfg.texts().actionFailed(shorten(message)))
	out = s.notice(d.Profile, pal)
	out.name, out.page, out.pages = served.name, served.page, served.pages
	out.refresh, out.interval = served.refresh, served.interval
	return out
}

// drawFailureBanner paints a black strip at the top with the failure reason,
// in the same style as the old action result banner.
func drawFailureBanner(img *image.Paletted, p Profile, text string) {
	r := image.Rect(0, 0, p.Width, max(28, p.Height/8)).Intersect(img.Rect)
	if r.Empty() {
		return
	}
	render.FillColor(img, r, render.Black)
	size := max(render.MinSize, float64(r.Dy())*0.52)
	render.Text(img, text, r.Min.X+12, r.Min.Y+r.Dy()/2+int(size*0.36),
		render.Style{Size: size, Bold: true, Shrink: true}, r.Dx()-24, render.White, render.Left)
}
