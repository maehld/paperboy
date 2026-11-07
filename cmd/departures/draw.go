package main

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

func (p *departures) Render(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	if w.Widget != "departures" {
		return fmt.Errorf("%w %q", plugin.ErrUnknownWidget, w.Widget)
	}
	mode, err := timeMode(w)
	if err != nil {
		return err
	}
	b := img.Bounds()
	pad := w.Style.Padding
	rule := max(w.Style.Rule, 1)
	size := max(req.TextPx(w), render.MinSize)
	small := max(size*0.8, req.PtPx(5.8), render.MinSize)
	rowH := int(size*1.4) + rule
	top := b.Min.Y + pad
	left, right := b.Min.X+pad, b.Max.X-pad

	if w.Arg().Bool("header", false) && p.curName != "" {
		render.Text(img, p.curName, left, top+int(size), render.Style{Size: size, Bold: true, Shrink: true}, right-left, color.Black, render.Left)
		top += rowH
		render.HLine(img, left, top-rule-int(size*0.2), right, rule)
	}
	rows := (b.Max.Y - pad - top) / rowH
	if rows < 1 {
		return fmt.Errorf("cell too small for a departure (%d px high)", b.Dy())
	}
	deps := p.cur
	if len(deps) > rows {
		deps = deps[:rows]
	}
	if len(deps) == 0 {
		render.Text(img, p.t("Keine Abfahrten", "No departures"), left, top+int(size), render.Style{Size: size, Shrink: true}, right-left, color.Black, render.Left)
		return nil
	}

	// Column widths follow the content so that destinations get the rest.
	lineW, timeW, delayW := 0, 0, 0
	times := make([]string, len(deps))
	for i, d := range deps {
		times[i] = p.timeText(d, req.Now, mode)
		lineW = max(lineW, render.Measure(d.Line, size, true))
		timeW = max(timeW, render.Measure(times[i], size, true))
		if dl := delayText(d); dl != "" {
			delayW = max(delayW, render.Measure(dl, small, true))
		}
	}
	gap := int(size * 0.5)
	lineW = min(lineW, (right-left)/3)
	timeW = min(timeW, (right-left)/2)
	destX := left + lineW + gap
	timeX := right
	delayX := timeX - timeW
	destEnd := delayX
	if delayW > 0 {
		destEnd = delayX - gap/2 - delayW
	}
	for i, d := range deps {
		y := top + i*rowH
		base := y + int(size)
		render.Text(img, d.Line, left, base, render.Style{Size: size, Bold: true, Shrink: true}, lineW, color.Black, render.Left)
		destW := destEnd - gap/2 - destX
		dest := d.Destination
		if render.Measure(dest, size, false) > destW {
			dest = stripLocality(dest) // "Beispielstadt, Depot" -> "Depot"
		}
		render.Text(img, dest, destX, base, render.Style{Size: size, Shrink: true}, destW, color.Black, render.Left)
		if d.Cancelled {
			tw := min(render.Measure(dest, size, false), destW)
			render.HLine(img, destX, base-int(size*0.3), destX+tw, rule)
		}
		if dl := delayText(d); dl != "" && !d.Cancelled {
			render.Text(img, dl, delayX-gap/2, base, render.Style{Size: small, Bold: true}, delayW, color.Black, render.Right)
		}
		render.Text(img, times[i], timeX, base, render.Style{Size: size, Bold: true, Shrink: true}, timeW, color.Black, render.Right)
		if i < len(deps)-1 {
			render.HLine(img, left, y+rowH-rule, right, rule)
		}
	}
	return nil
}

func timeMode(w plugin.WidgetRequest) (string, error) {
	m := w.Arg().String("time", "auto")
	switch m {
	case "auto", "minutes", "clock":
		return m, nil
	}
	return "", fmt.Errorf("time: %q is not auto, minutes or clock", m)
}

func (p *departures) t(de, en string) string { return p.req.T(de, en) }

// delayText is the real-time delay in minutes with a sign, empty when on time.
func delayText(d departure) string {
	if n := d.delay(); n != 0 {
		return fmt.Sprintf("%+d", n)
	}
	return ""
}

// timeText is the minutes until departure, or the clock time from one hour on.
// A cancelled departure shows that instead.
func (p *departures) timeText(d departure, now time.Time, mode string) string {
	return p.timeIn(d, now, mode, false)
}

func (p *departures) timeIn(d departure, now time.Time, mode string, compact bool) string {
	if d.Cancelled {
		if compact {
			return p.t("Ausf.", "canc.")
		}
		return p.t("Ausfall", "cancelled")
	}
	mins := int(d.when().Sub(now) / time.Minute)
	if mode == "clock" || (mode == "auto" && mins >= 60) {
		return d.when().In(p.location).Format("15:04")
	}
	if mins <= 0 {
		return p.t("jetzt", "now")
	}
	if compact {
		return fmt.Sprintf("%d", mins)
	}
	return fmt.Sprintf("%d min", mins)
}

// Text draws one departure per line: line and destination left, delay and
// time right. A header line with the stop name is optional.
func (p *departures) Text(req *plugin.Request, w plugin.WidgetRequest) ([]string, error) {
	mode, err := timeMode(w)
	if err != nil {
		return nil, err
	}
	var lines []string
	if w.Arg().Bool("header", false) && p.curName != "" {
		lines = append(lines, plugin.Align(p.curName, w.Width, "left"))
	}
	if len(p.cur) == 0 {
		lines = append(lines, p.t("Keine Abfahrten", "No departures"))
	}
	for _, d := range p.cur {
		right := p.timeIn(d, req.Now, mode, true)
		if dl := delayText(d); dl != "" && !d.Cancelled {
			right = dl + " " + right
		}
		left := d.Line
		if d.Destination != "" {
			left += " " + stripLocality(d.Destination)
		}
		lines = append(lines, plugin.Columns(left, right, w.Width))
	}
	if len(lines) > w.Height {
		lines = lines[:w.Height]
	}
	return lines, nil
}

// stripLocality shortens "Beispielstadt, Depot" to "Depot" on narrow
// character displays.
func stripLocality(name string) string {
	if _, after, ok := strings.Cut(name, ", "); ok && after != "" {
		return after
	}
	return name
}
