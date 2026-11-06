// Command solardemo is a Paperboy plugin with synthetic solar data (ADR 0049).
// It stands in for a SolarAI plugin until that exists: "tiles" shows the
// current values and "chart" today's power curve.
//
//	echo '{"id":"1","action":"describe"}' | go run ./cmd/solardemo
package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

func main() {
	plugin.Serve(&solar{})
}

type solar struct {
	req  *plugin.Request
	loc  *time.Location
	now  time.Time
	lang string
}

func (s *solar) Describe() plugin.Description {
	return plugin.Description{
		Title: "Solar (demo)",
		Widgets: []plugin.WidgetInfo{
			{Widget: "tiles", Title: "Current values", Kinds: []string{plugin.KindImage, plugin.KindText}, MinWidth: 200, MinHeight: 100,
				Sizes: []plugin.Span{{Columns: 3, Rows: 1}, {Columns: 6, Rows: 1}, {Columns: 3, Rows: 2}},
				Args: map[string]plugin.Arg{
					"columns": {Type: "int", Default: 2, Description: "tiles per row: 2 or 4"},
				}},
			{Widget: "chart", Title: "Power today", Kinds: []string{plugin.KindImage}, MinWidth: 200, MinHeight: 100,
				Sizes: []plugin.Span{{Columns: 6, Rows: 1}, {Columns: 6, Rows: 2}}},
		},
	}
}

func (s *solar) Configure(cfg plugin.Values, _ *plugin.Request) error {
	return cfg.Check(nil)
}

func (s *solar) Prepare(req *plugin.Request) error {
	s.req, s.loc, s.now, s.lang = req, req.Location(), req.Now.In(req.Location()), req.Lang()
	return nil
}

func (s *solar) Render(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	switch w.Widget {
	case "tiles":
		s.tiles(w, img)
	case "chart":
		s.chart(w, img)
	default:
		return fmt.Errorf("%w %q", plugin.ErrUnknownWidget, w.Widget)
	}
	return nil
}

// Text shows the values on a character display, one per line.
func (s *solar) Text(req *plugin.Request, w plugin.WidgetRequest) ([]string, error) {
	if w.Widget != "tiles" {
		return nil, fmt.Errorf("%w %q for text displays", plugin.ErrUnknownWidget, w.Widget)
	}
	var out []string
	for _, it := range s.items() {
		out = append(out, plugin.Columns(it[0], it[1], w.Width))
	}
	return out, nil
}

// MaxAge: the demo values change every minute.
func (s *solar) MaxAge(*plugin.Request) time.Duration { return time.Minute }

func (s *solar) t(de, en string) string {
	if s.lang == "de" {
		return de
	}
	return en
}

// pv and load are synthetic curves in watts for an hour of the day.
func pv(h float64) float64 {
	if h < 7 || h > 18 {
		return 0
	}
	return 5200 * math.Pow(math.Sin((h-7)/11*math.Pi), 1.6)
}

func load(h float64) float64 {
	return 350 + 900*math.Exp(-math.Pow(h-7.5, 2)/1.5) + 1400*math.Exp(-math.Pow(h-19, 2)/3)
}

func (s *solar) hour() float64 {
	return float64(s.now.Hour()) + float64(s.now.Minute())/60
}

func (s *solar) num(v float64, dec int) string {
	out := strconv.FormatFloat(v, 'f', dec, 64)
	if s.lang == "de" {
		out = strings.Replace(out, ".", ",", 1)
	}
	return out
}

func (s *solar) power(w float64) string {
	if math.Abs(w) >= 1000 {
		return s.num(math.Abs(w)/1000, 2) + " kW"
	}
	return s.num(math.Abs(w), 0) + " W"
}

func (s *solar) items() [][3]string {
	h := s.hour()
	p, l := pv(h), load(h)
	grid := l - p
	gridWord := s.t("Bezug", "import")
	if grid < 0 {
		gridWord = s.t("Einspeisung", "export")
	}
	return [][3]string{
		{s.t("PV", "PV"), s.power(p), ""},
		{s.t("Verbrauch", "Load"), s.power(l), ""},
		{s.t("Netz", "Grid"), s.power(grid), gridWord},
		{s.t("Akku", "Battery"), "81 %", s.t("lädt", "charging")},
	}
}

func (s *solar) tiles(w plugin.WidgetRequest, img *image.Paletted) {
	items := s.items()
	cols := 2
	if w.Arg().Int("columns", 2) == 4 {
		cols = 4
	}
	rows := (len(items) + cols - 1) / cols
	b := img.Rect
	cw, ch := b.Dx()/cols, b.Dy()/rows
	pad := w.Style.Padding
	base := s.req.TextPx(w)
	for i, it := range items {
		cell := image.Rect(b.Min.X+(i%cols)*cw, b.Min.Y+(i/cols)*ch, b.Min.X+(i%cols+1)*cw, b.Min.Y+(i/cols+1)*ch)
		x := cell.Min.X + pad
		labelSize := min(float64(ch)*0.2, base*0.8)
		render.Text(img, it[0], x, cell.Min.Y+pad+int(labelSize), render.Style{Size: labelSize, Shrink: true}, cw-2*pad, color.Black, render.Left)
		render.Text(img, it[1], x, cell.Min.Y+ch*66/100, render.Style{Size: min(float64(ch)*0.34, base*1.6), Bold: true, Shrink: true}, cw-2*pad, color.Black, render.Left)
		if it[2] != "" {
			render.Text(img, it[2], x, cell.Max.Y-pad, render.Style{Size: min(float64(ch)*0.17, base*0.7), Shrink: true}, cw-2*pad, color.Black, render.Left)
		}
	}
	for c := 1; c < cols; c++ {
		render.VLine(img, b.Min.X+c*cw, b.Min.Y+pad, b.Max.Y-pad, 1)
	}
	for r := 1; r < rows; r++ {
		render.HLine(img, b.Min.X+pad, b.Min.Y+r*ch, b.Max.X-pad, 1)
	}
}

// chart draws today's PV curve as an area and the load as a line, with a time
// axis and a dashed "now" marker.
func (s *solar) chart(w plugin.WidgetRequest, img *image.Paletted) {
	b := img.Rect
	labelSize := max(min(s.req.TextPx(w)*0.6, float64(b.Dy())*0.1), 10)
	left := b.Min.X + render.Measure("6 kW", labelSize, false) + 12
	right := b.Max.X - render.Measure("00:00", labelSize, false)/2 - 4
	top := b.Min.Y + int(labelSize*1.8)
	bottom := b.Max.Y - int(labelSize*1.6)
	if right-left < 20 || bottom-top < 20 {
		return
	}
	render.Text(img, s.t("Leistung heute", "Power today"), b.Min.X+8, b.Min.Y+int(labelSize*1.3),
		render.Style{Size: labelSize * 1.1, Bold: true, Shrink: true}, b.Dx()/2, color.Black, render.Left)
	yMax := 6000.0
	x := func(h float64) int { return left + int(h/24*float64(right-left)) }
	y := func(w float64) int { return bottom - int(w/yMax*float64(bottom-top)) }

	nowH := s.hour()
	for px := left; px < right; px++ {
		h := float64(px-left) / float64(right-left) * 24
		if h > nowH {
			break
		}
		if v := pv(h); v > 0 {
			render.Fill(img, image.Rect(px, y(v), px+1, bottom), "shade")
		}
	}
	prevX, prevY := -1, 0
	for px := left; px < right; px++ {
		h := float64(px-left) / float64(right-left) * 24
		if h > nowH {
			break
		}
		cy := y(load(h))
		if prevX >= 0 {
			render.Segment(img, prevX, prevY, px, cy, 2, false)
		}
		prevX, prevY = px, cy
	}
	render.HLine(img, left, bottom, right, 1)
	render.VLine(img, left, top, bottom, 1)
	for _, kw := range []float64{2000, 4000, 6000} {
		render.Text(img, s.num(kw/1000, 0)+" kW", left-4, y(kw)+int(labelSize*0.35), render.Style{Size: labelSize}, left-b.Min.X, color.Black, render.Right)
		for px := left; px < right; px += 4 {
			img.SetColorIndex(px, y(kw), 0)
		}
	}
	for h := 0; h <= 24; h += 6 {
		render.VLine(img, x(float64(h)), bottom, bottom+4, 1)
		render.Text(img, fmt.Sprintf("%02d:00", h%24), x(float64(h)), b.Max.Y-3, render.Style{Size: labelSize},
			60, color.Black, render.Center)
	}
	render.Segment(img, x(nowH), top, x(nowH), bottom, 1, true)
	lx := right - render.Measure(s.t("Verbrauch", "Load"), labelSize, false)
	render.Text(img, s.t("Verbrauch", "Load"), lx, top-4, render.Style{Size: labelSize}, right-lx, color.Black, render.Left)
	render.Segment(img, lx-26, top-int(labelSize*0.4), lx-6, top-int(labelSize*0.4), 2, false)
	px := lx - 40 - render.Measure("PV", labelSize, false)
	render.Fill(img, image.Rect(px-18, top-int(labelSize*0.8), px-4, top-2), "shade")
	render.Text(img, "PV", px, top-4, render.Style{Size: labelSize}, 40, color.Black, render.Left)
}
