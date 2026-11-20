package paperboy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

	"github.com/maehld/paperboy/pkg/render"
)

// overlayRect is the area the menu covers: the top half, where the buttons
// are on both current devices.
func overlayRect(p Profile) image.Rectangle {
	return image.Rect(0, 0, p.Width, max(1, p.Height/2))
}

// bannerRect is the strip that shows a running action or its result. It is
// not a menu screen: it needs no selection and no close box, and it leaves
// almost the whole page visible.
func bannerRect(p Profile) image.Rectangle {
	return image.Rect(0, 0, p.Width, max(28, p.Height/8))
}

// menuCells splits the top half into equal cells. header reserves a strip for
// the confirmation or result text.
// rowCells splits area into equal columns (landscape) or rows (portrait).
func rowCells(area image.Rectangle, portrait bool, kinds []string) []menuCell {
	cells := make([]menuCell, 0, len(kinds))
	n := max(len(kinds), 1)
	for i, kind := range kinds {
		var r image.Rectangle
		if portrait {
			h := area.Dy() / n
			r = image.Rect(area.Min.X, area.Min.Y+i*h, area.Max.X, area.Min.Y+(i+1)*h)
			if i == n-1 {
				r.Max.Y = area.Max.Y
			}
		} else {
			w := area.Dx() / n
			r = image.Rect(area.Min.X+i*w, area.Min.Y, area.Min.X+(i+1)*w, area.Max.Y)
			if i == n-1 {
				r.Max.X = area.Max.X
			}
		}
		cells = append(cells, menuCell{kind: kind, index: i, rect: r})
	}
	return cells
}

// actionKinds returns the cells of the action row for one phase.
func actionKinds(phase string) []string {
	switch phase {
	case menuConfirmPhase:
		// Yes and no only: a close box would be the same as "no".
		return []string{cellYes, cellNo}
	case menuRunningPhase, menuResultPhase, menuEmptyPhase, menuOffPhase:
		return nil // drawn as a banner, no cells
	}
	kinds := make([]string, menuPerPage)
	for i := range kinds {
		kinds[i] = cellAction
	}
	return kinds
}

// controlKinds returns the buttons of the strip under the action row. It only
// exists for devices whose digitiser is turned on: the strip is meant to be
// tapped, and a dead button is worse than none (ADR 0082).
func controlKinds(count int, touch bool) []string {
	if !touch {
		return nil
	}
	kinds := make([]string, 0, 3)
	if count > menuPerPage {
		kinds = append(kinds, cellPrev, cellNext)
	}
	return append(kinds, cellClose)
}

// stripCells lays the control strip out as one compact group in the middle:
// equal boxes with a gap between them, the close box last (ADR 0082).
func stripCells(strip image.Rectangle, kinds []string) []menuCell {
	cells := make([]menuCell, 0, len(kinds))
	if strip.Empty() || len(kinds) == 0 {
		return cells
	}
	h := max(14, strip.Dy()-6)
	w := min(strip.Dx()/6, max(26, h*2))
	gap := max(4, w/3)
	total := len(kinds)*w + (len(kinds)-1)*gap
	x := strip.Min.X + max(3, (strip.Dx()-total)/2)
	y := strip.Min.Y + 3
	for i, kind := range kinds {
		if i > 0 {
			x += gap
		}
		cells = append(cells, menuCell{kind: kind, index: -1, rect: image.Rect(x, y, x+w, y+h)})
		x += w
	}
	return cells
}

// confirmRect is the message box of the confirmation: centred over the page,
// so it reads as a dialog instead of a full-width band (ADR 0081).
func confirmRect(p Profile) image.Rectangle {
	w := min(p.Width-8, max(320, p.Width*2/3))
	h := min(p.Height-8, max(170, p.Height*9/20))
	x := (p.Width - w) / 2
	y := (p.Height - h) / 2
	return image.Rect(x, y, x+w, y+h)
}

// menuLayout computes the cells of one phase. The list covers the top half,
// with the control strip of a touch device beneath its actions; the
// confirmation is a centred box with its two choices (ADR 0081).
func menuLayout(p Profile, phase string, count int, touch bool) (actions, controls []menuCell, overlay image.Rectangle) {
	if phase == menuConfirmPhase {
		overlay = confirmRect(p)
		inner := overlay.Inset(6)
		titleH := max(24, inner.Dy()/3)
		row := image.Rect(inner.Min.X, inner.Min.Y+titleH, inner.Max.X, inner.Max.Y)
		actions = rowCells(row, p.Height > p.Width, []string{cellYes, cellNo})
		return actions, nil, overlay
	}
	overlay = overlayRect(p)
	area := overlay
	// The strip belongs to the list: on the confirmation screen it would
	// offer a second "no" and page arrows that cannot page (ADR 0057, 0082).
	if phase == menuListPhase {
		if strip := controlKinds(count, touch); len(strip) > 0 {
			sh := max(22, overlay.Dy()/4)
			rect := image.Rect(area.Min.X, area.Max.Y-sh, area.Max.X, area.Max.Y)
			controls = stripCells(rect, strip)
			area = image.Rect(area.Min.X, area.Min.Y, area.Max.X, area.Max.Y-sh)
		}
	}
	actions = rowCells(area, p.Height > p.Width, actionKinds(phase))
	return actions, controls, overlay
}

// menuView is what the overlay shows in one request.
type menuView struct {
	phase   string
	actions []menuAction // the full list, for the visible page and the header
	offset  int
	sel     string
	close   bool // the close box of the control strip has the selection
	touch   bool // draw the control strip at all
	title   string
	text    string
}

// drawMenu paints the menu over the page image (ADR 0057): the list covers the
// top half, the confirmation is a message box of its own (ADR 0081).
func drawMenu(img *image.Paletted, p Profile, v menuView, t texts) {
	if v.phase == menuRunningPhase || v.phase == menuResultPhase || v.phase == menuEmptyPhase || v.phase == menuOffPhase {
		drawBanner(img, p, v)
		return
	}
	actions, controls, overlay := menuLayout(p, v.phase, len(v.actions), v.touch)
	if overlay.Intersect(img.Rect).Empty() {
		return
	}
	if v.phase == menuConfirmPhase {
		drawConfirm(img, p, v, actions, overlay, t)
		return
	}
	bg := image.Rect(overlay.Min.X, overlay.Min.Y, min(overlay.Max.X, img.Rect.Max.X), min(overlay.Max.Y, img.Rect.Max.Y))
	render.FillColor(img, bg, render.White)
	size := max(render.MinSize, float64(overlay.Dy())/5)
	if len(actions) > 0 {
		c := actions[0].rect
		size = max(render.MinSize, min(float64(c.Dx())/7.5, float64(c.Dy())/4.5))
	}
	ai := v.offset
	for i := range actions {
		c := &actions[i]
		var label string
		var action *menuAction
		switch c.kind {
		case cellAction:
			c.index = ai
			if ai < len(v.actions) {
				action = &v.actions[ai]
				label = action.Title
			}
			ai++
		}
		selected := action != nil && action.Key == v.sel && !v.close
		drawCell(img, *c, label, size, selected, action != nil && action.Disabled != "", false)
	}
	for i := range controls {
		c := &controls[i]
		var label string
		switch c.kind {
		case cellClose:
			label = t.menuClose()
		case cellPrev:
			label = "<"
		case cellNext:
			label = ">"
		}
		// The strip is a control, not an action: thinner, smaller, grey, and
		// selected with a heavier frame and a larger glyph instead of an
		// inverted fill (ADR 0082).
		selected := c.kind == cellClose && v.close
		small := max(render.MinSize, size*0.6)
		if selected {
			small = max(render.MinSize, size*0.8)
		}
		drawCell(img, *c, label, small, selected, false, true)
	}
	// A rule under the overlay separates it from the page.
	thickness := max(1, int(p.PPI/72*0.6))
	render.HLine(img, overlay.Min.X, overlay.Max.Y-1, overlay.Max.X, thickness)
}

func drawCell(img *image.Paletted, c menuCell, label string, size float64, selected, disabled, control bool) {
	if c.rect.Empty() {
		return
	}
	if control {
		// A tap target of the control strip, not an action: a thin frame
		// around a small glyph, and the selection inverts the box so it is
		// unmistakable (ADR 0082).
		if selected {
			render.FillColor(img, c.rect, render.Black)
		} else {
			frameCell(img, c.rect, 1)
		}
		ink := render.DarkGray
		if selected {
			ink = render.White
		}
		inner := c.rect.Inset(3)
		cx := inner.Min.X + inner.Dx()/2
		render.Text(img, label, cx, inner.Min.Y+inner.Dy()/2+int(size*0.36),
			render.Style{Size: size, Bold: true, Shrink: true}, inner.Dx(), ink, render.Center)
		return
	}
	if disabled {
		// Unavailable actions are hatched whether selected or not; the
		// selection gets a heavier frame, because inverting would fight the
		// hatch.
		render.Fill(img, c.rect, "hatch")
	} else if selected {
		render.Fill(img, c.rect, "solid")
	}
	ink := render.Black
	if selected && !disabled {
		ink = render.White
	} else if disabled {
		ink = render.DarkGray
	}
	// Every cell gets a box of its own, so neighbours show two lines and each
	// action reads as a button, like the control strip below (issue #136).
	frameCell(img, c.rect, 1)
	inner := c.rect.Inset(1)
	lines := wrapMenu(label, inner.Dx()-6, size)
	if len(lines) == 0 && label != "" {
		lines = []string{render.Ellipsize(label, size, false, inner.Dx()-6)}
	}
	// Center the block of lines vertically in the cell.
	lineH := int(size * 1.2)
	top := inner.Min.Y + (inner.Dy()-len(lines)*lineH)/2 + int(size)
	cx := inner.Min.X + inner.Dx()/2
	if disabled && len(lines) > 0 {
		// The label gets a white plate, so it stays readable over the hatch.
		widest := 0
		for _, line := range lines {
			widest = max(widest, render.Measure(line, size, false))
		}
		plate := image.Rect(cx-widest/2-3, top-lineH+2, cx+widest/2+3, top+lineH*(len(lines)-1)+4)
		render.FillColor(img, plate.Intersect(inner), render.White)
	}
	for _, line := range lines {
		if top > inner.Max.Y-int(size)/2 {
			break
		}
		render.Text(img, line, cx, top, render.Style{Size: size, Bold: selected, Shrink: true}, inner.Dx()-6, ink, render.Center)
		top += lineH
	}
	if disabled && selected {
		frameCell(img, c.rect, 2)
	}
}

// drawBanner paints the running action and its result as a strip at the top:
// the outcome is an announcement, not a dialog, so it has no cells and no
// close box. A running action is outlined, a result is filled.
func drawBanner(img *image.Paletted, p Profile, v menuView) {
	r := bannerRect(p)
	r = r.Intersect(img.Rect)
	if r.Empty() {
		return
	}
	text := v.title
	if v.text != "" {
		if text == "" {
			text = v.text
		} else {
			text += ": " + v.text
		}
	}
	ink := render.White
	if v.phase == menuRunningPhase {
		render.FillColor(img, r, render.White)
		frameCell(img, r, 2)
		ink = render.Black
	} else {
		render.FillColor(img, r, render.Black)
	}
	size := max(render.MinSize, float64(r.Dy())*0.52)
	render.Text(img, text, r.Min.X+12, r.Min.Y+r.Dy()/2+int(size*0.36),
		render.Style{Size: size, Bold: true, Shrink: true}, r.Dx()-24, ink, render.Left)
}

// frameCell draws a border of the given thickness inside r.
func frameCell(img *image.Paletted, r image.Rectangle, t int) {
	render.FillColor(img, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+t), render.Black)
	render.FillColor(img, image.Rect(r.Min.X, r.Max.Y-t, r.Max.X, r.Max.Y), render.Black)
	render.FillColor(img, image.Rect(r.Min.X, r.Min.Y, r.Min.X+t, r.Max.Y), render.Black)
	render.FillColor(img, image.Rect(r.Max.X-t, r.Min.Y, r.Max.X, r.Max.Y), render.Black)
}

// drawConfirm paints the confirmation as a message box: the action's title on
// top, then the two choices with the key that triggers each written beneath
// them (ADR 0081).
func drawConfirm(img *image.Paletted, p Profile, v menuView, cells []menuCell, box image.Rectangle, t texts) {
	render.FillColor(img, box, render.White)
	frameCell(img, box, 2)
	inner := box.Inset(6)
	titleH := max(24, inner.Dy()/3)
	titleRect := image.Rect(inner.Min.X, inner.Min.Y, inner.Max.X, inner.Min.Y+titleH)
	titleSize := max(render.MinSize, min(float64(titleRect.Dy())*0.58, float64(box.Dx())/16))
	lines := wrapMenu(v.title, titleRect.Dx(), titleSize)
	if v.text != "" && len(lines) > 1 {
		lines = lines[:1]
	}
	lineH := int(titleSize * 1.2)
	top := titleRect.Min.Y + (titleRect.Dy()-len(lines)*lineH)/2 + int(titleSize)
	for _, line := range lines {
		render.Text(img, line, titleRect.Min.X+titleRect.Dx()/2, top,
			render.Style{Size: titleSize, Bold: true, Shrink: true}, titleRect.Dx(), render.Black, render.Center)
		top += lineH
	}
	if v.text != "" {
		// A plugin's own explanation sits under the title, small and grey.
		hintSize := max(render.MinSize, titleSize*0.55)
		render.Text(img, v.text, titleRect.Min.X+titleRect.Dx()/2, top,
			render.Style{Size: hintSize, Shrink: true}, titleRect.Dx(), render.DarkGray, render.Center)
	}
	for _, c := range cells {
		label, key := t.menuYes(), t.menuYesKey()
		if c.kind == cellNo {
			label, key = t.menuNo(), t.menuNoKey()
		}
		frameCell(img, c.rect, 1)
		in := c.rect.Inset(4)
		word := max(render.MinSize, min(float64(in.Dy())*0.42, float64(in.Dx())/5))
		keySize := max(render.MinSize, word*0.5)
		block := int(word*1.2) + int(keySize*1.6)
		top := in.Min.Y + (in.Dy()-block)/2 + int(word)
		cx := in.Min.X + in.Dx()/2
		render.Text(img, label, cx, top,
			render.Style{Size: word, Bold: true, Shrink: true}, in.Dx(), render.Black, render.Center)
		render.Text(img, key, cx, top+int(keySize*1.7),
			render.Style{Size: keySize, Shrink: true}, in.Dx(), render.DarkGray, render.Center)
	}
}

// wrapMenu breaks a title into at most two lines that fit width pixels.
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

// menuOverlay decodes a rendered page, draws the menu over it and registers
// the result as a notice page, so the page cache is not overwritten.
func (s *Server) menuOverlay(page servedPage, profile string, p Profile, v menuView, t texts) (out servedPage, err error) {
	// Drawing is the last place without a recover: a font or encoder panic
	// must leave the served page intact instead of failing the whole sync.
	defer func() {
		if r := recover(); r != nil {
			slog.Error("menu drawing panicked", "profile", profile, "panic", r, "stack", string(debug.Stack()))
			out, err = page, fmt.Errorf("menu drawing panicked: %v", r)
		}
	}()
	s.mu.Lock()
	data := s.hints[page.hash].data
	if len(data) != 0 {
		hp := s.hints[page.hash]
		hp.used = time.Now()
		s.hints[page.hash] = hp
	}
	if len(data) == 0 {
		if rp := s.renderedPageLocked(profile, page.name, page.hash); rp != nil {
			data = rp.data
		}
	}
	s.mu.Unlock()
	if len(data) == 0 {
		return page, fmt.Errorf("page %s is not available", page.hash)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return page, err
	}
	pal, ok := img.(*image.Paletted)
	if !ok {
		return page, errors.New("page is not paletted")
	}
	if pal.Rect.Dx() != p.Width || pal.Rect.Dy() != p.Height {
		return page, fmt.Errorf("page is %dx%d, display is %dx%d", pal.Rect.Dx(), pal.Rect.Dy(), p.Width, p.Height)
	}
	drawMenu(pal, p, v, t)
	out = s.notice(profile, pal)
	out.name, out.page, out.pages = page.name, page.page, page.pages
	out.refresh, out.interval = page.refresh, page.interval
	return out, nil
}

// overlayMenu draws the open menu over the served page. It resolves the
// plugin state of the actions when the menu opened and records the hash of
// the confirmation screen, which the next sync compares with the device's
// shown hash (ADR 0057).
func (s *Server) overlayMenu(cfg *Config, d *Device, served servedPage, m *menuState, touch bool) servedPage {
	prof := cfg.Profiles[d.Profile]
	if !m.Resolved {
		ctx, cancel := context.WithTimeout(context.Background(), actionBudget)
		m.Actions = s.resolveActions(ctx, cfg, d, m.Actions, s.now())
		cancel()
		m.Resolved = true
		if m.Selected == "" && len(m.Actions) > 0 {
			m.Selected = m.Actions[0].Key
		}
		// Nothing to choose: say so once instead of showing a list in which
		// every cell is hatched (ADR 0082). The reasons only arrive with the
		// plugin answers, so this cannot happen when the menu opens.
		if m.Phase == menuListPhase && allDisabled(m.Actions) {
			m.Phase, m.Reason = menuOffPhase, m.Actions[0].Disabled
		}
	}
	t := cfg.texts()
	v := menuView{phase: m.Phase, actions: m.Actions, offset: m.Offset, sel: m.Selected,
		close: m.CloseSel, touch: touch}
	switch m.Phase {
	case menuEmptyPhase:
		v.title = t.menuEmpty()
	case menuOffPhase:
		v.title = m.Reason
	case menuConfirmPhase:
		if a := m.selected(); a != nil {
			v.title = a.Title
			// The plugins' own explanation, if any; the keys are written
			// under Ja and Nein by the dialog itself (ADR 0081).
			v.text = a.ConfirmText
		}
	case menuRunningPhase:
		if a := m.selected(); a != nil {
			v.title = a.Title
		}
		_, v.text = t.menuRunning("")
	case menuResultPhase:
		if m.Simulated {
			v.title, v.text = t.menuSimulated(m.Result)
		} else {
			v.title, v.text = t.menuResult(m.ResultOK, m.Result)
		}
	}
	out, err := s.menuOverlay(served, d.Profile, prof, v, t)
	if err != nil {
		slog.Warn("menu overlay failed", "device", d.ID, "error", err)
		return served
	}
	if m.Phase == menuConfirmPhase {
		m.ConfirmHash = out.hash
	}
	// The device reports this hash as "shown" until the menu is repainted. A
	// press that arrives with it belongs to the menu even when the deadline has
	// passed in the meantime (ADR 0078).
	m.Screen = out.hash
	return out
}
