package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

//go:embed icons/*.png
var iconFiles embed.FS

var (
	iconOnce  sync.Once
	iconMasks map[string]*render.Mask
)

// icon returns the mask of an embedded pictogram by name.
func icon(name string) *render.Mask {
	iconOnce.Do(func() {
		iconMasks = map[string]*render.Mask{}
		entries, _ := iconFiles.ReadDir("icons")
		for _, e := range entries {
			f, err := iconFiles.Open("icons/" + e.Name())
			if err != nil {
				continue
			}
			img, err := png.Decode(f)
			f.Close()
			if err == nil {
				iconMasks[strings.TrimSuffix(e.Name(), ".png")] = render.NewMask(img)
			}
		}
	})
	return iconMasks[name]
}

var configKeys = map[string]plugin.Arg{
	"lat":     {Type: "float", Required: true, Description: "latitude, e.g. 52.52"},
	"lon":     {Type: "float", Required: true, Description: "longitude, e.g. 13.40"},
	"base":    {Type: "string", Default: defaultBase, Description: "Open-Meteo base URL"},
	"refresh": {Type: "duration", Default: "15m", Description: "how often the forecast is fetched"},
}

// weather is the plugin. The forecast is fetched with the configured
// language, so the condition texts follow the global language.
type weather struct {
	lat, lon float64
	base     string
	refresh  time.Duration
	language string

	mu      sync.Mutex
	data    *branch
	loadErr error
	ready   chan struct{}

	cur  *branch // snapshot for the current request
	req  *plugin.Request
	loc  *time.Location
	lang string
}

func (w *weather) Describe() plugin.Description {
	both := []string{plugin.KindImage, plugin.KindText}
	return plugin.Description{
		Title:  "Weather (Open-Meteo)",
		Config: configKeys,
		Widgets: []plugin.WidgetInfo{
			{Widget: "current", Title: "Current weather", Kinds: both, MinWidth: 160, MinHeight: 80,
				Sizes: []plugin.Span{{Columns: 2, Rows: 1}, {Columns: 3, Rows: 2}},
				Args: map[string]plugin.Arg{
					"clothing": {Type: "bool", Default: false, Description: "T-shirt or pullover by apparent temperature and wind"},
				}},
			{Widget: "days", Title: "Daily forecast", Kinds: both, MinWidth: 200, MinHeight: 80,
				Sizes: []plugin.Span{{Columns: 3, Rows: 1}, {Columns: 6, Rows: 2}},
				Args: map[string]plugin.Arg{
					"days": {Type: "int", Default: 5, Description: "number of days, 1 to 5"},
				}},
			{Widget: "hours", Title: "Temperature today", Kinds: []string{plugin.KindImage}, MinWidth: 200, MinHeight: 80,
				Sizes: []plugin.Span{{Columns: 3, Rows: 1}, {Columns: 6, Rows: 2}}},
		},
	}
}

func (w *weather) Configure(cfg plugin.Values, req *plugin.Request) error {
	if err := cfg.Check(configKeys); err != nil {
		return err
	}
	w.lat, w.lon = cfg.Float("lat", 0), cfg.Float("lon", 0)
	if w.lat < -90 || w.lat > 90 || w.lon < -180 || w.lon > 180 {
		return errors.New("lat must be within -90..90 and lon within -180..180")
	}
	w.base = cfg.String("base", defaultBase)
	refresh, err := cfg.Duration("refresh", 15*time.Minute)
	if err != nil {
		return err
	}
	if refresh < time.Minute {
		return errors.New("refresh must be at least 1m")
	}
	w.refresh, w.language = refresh, req.Lang()
	return nil
}

// Start fetches the forecast now and then every refresh and reports a change
// when it differs.
func (w *weather) Start(ctx context.Context, notify func(widgets ...string)) {
	w.ready = make(chan struct{})
	go func() {
		first := true
		var last string
		for {
			b, err := w.load(ctx)
			w.mu.Lock()
			if err != nil {
				w.loadErr = err
				fmt.Fprintln(os.Stderr, "weather:", err)
			} else {
				w.data, w.loadErr = &b, nil
			}
			w.mu.Unlock()
			if err == nil {
				sum, _ := json.Marshal(b)
				if !first && string(sum) != last {
					notify()
				}
				last = string(sum)
			}
			if first {
				close(w.ready)
				first = false
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(w.refresh):
			}
		}
	}()
}

func (w *weather) load(ctx context.Context) (branch, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	f, err := fetch(ctx, http.DefaultClient, w.base, w.lat, w.lon)
	if err != nil {
		return branch{}, err
	}
	return branchOf(f, w.language)
}

func (w *weather) Prepare(req *plugin.Request) error {
	select {
	case <-w.ready:
	case <-time.After(10 * time.Second):
		return errors.New("forecast is still loading")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.data == nil {
		return w.loadErr
	}
	w.cur, w.req, w.loc, w.lang = w.data, req, req.Location(), req.Lang()
	return nil
}

func (w *weather) MaxAge(*plugin.Request) time.Duration { return 15 * time.Minute }

func (w *weather) t(de, en string) string { return w.req.T(de, en) }

func (w *weather) num(v float64, decimals int) string {
	s := strconv.FormatFloat(v, 'f', decimals, 64)
	if w.lang == "de" {
		s = strings.Replace(s, ".", ",", 1)
	}
	return s
}

func (w *weather) temp(v float64) string { return w.num(math.Round(v)+0, 0) + "°" }

func (w *weather) Render(req *plugin.Request, wr plugin.WidgetRequest, img *image.Paletted) error {
	switch wr.Widget {
	case "current":
		w.current(wr, img)
	case "days":
		w.days(wr, img)
	case "hours":
		w.hours(wr, img)
	default:
		return fmt.Errorf("%w %q", plugin.ErrUnknownWidget, wr.Widget)
	}
	return nil
}

// clothing advises a pullover below 15 °C apparent temperature or above wind
// force 3, else a T-shirt.
func clothing(c current) string {
	if c.Apparent < 15 || c.Beaufort > 3 {
		return "pullover"
	}
	return "tshirt"
}

func (w *weather) current(wr plugin.WidgetRequest, img *image.Paletted) {
	c := w.cur.Current
	b := img.Rect.Inset(wr.Style.Padding)
	if b.Empty() {
		return
	}
	iconW := min(b.Dy(), b.Dx()/3)
	icon(c.Symbol).Draw(img, image.Rect(b.Min.X, b.Min.Y, b.Min.X+iconW, b.Max.Y), iconW*85/100, render.Center)
	right := b.Max.X
	if wr.Arg().Bool("clothing", false) {
		cw := min(b.Dy()*2/3, b.Dx()/5)
		icon(clothing(c)).Draw(img, image.Rect(b.Max.X-cw, b.Min.Y, b.Max.X, b.Max.Y), cw*85/100, render.Center)
		right -= cw + wr.Style.Padding
	}
	x := b.Min.X + iconW + wr.Style.Padding
	width := right - x
	h := b.Dy()
	base := w.req.TextPx(wr)
	big := min(float64(h)*0.42, base*2.6)
	small := min(float64(h)*0.17, base*0.9)
	top := b.Min.Y + (h-int(big+2.6*small))/2
	render.Text(img, w.num(c.Temp, 1)+" °C", x, top+int(big*0.9), render.Style{Size: big, Bold: true, Shrink: true}, width, color.Black, render.Left)
	render.Text(img, c.Text, x, top+int(big+small*1.1), render.Style{Size: small, Shrink: true}, width, color.Black, render.Left)
	detail := fmt.Sprintf("%s %s · %s %d · %s %s/%s", w.t("gefühlt", "feels"), w.temp(c.Apparent),
		w.t("Wind", "wind"), c.Beaufort, w.t("heute", "today"), w.temp(w.cur.Today.Max), w.temp(w.cur.Today.Min))
	render.Text(img, detail, x, top+int(big+small*2.5), render.Style{Size: small * 0.85, Shrink: true}, width, color.Black, render.Left)
}

func (w *weather) dayList(n int) []day {
	n = min(max(n, 1), 5)
	if len(w.cur.Days) < n {
		n = len(w.cur.Days)
	}
	return w.cur.Days[:n]
}

func (w *weather) days(wr plugin.WidgetRequest, img *image.Paletted) {
	list := w.dayList(wr.Arg().Int("days", 5))
	if len(list) == 0 {
		return
	}
	b := img.Rect.Inset(wr.Style.Padding)
	cw := b.Dx() / len(list)
	h := b.Dy()
	text := min(float64(h)*0.17, w.req.TextPx(wr)*0.9)
	for i, d := range list {
		col := image.Rect(b.Min.X+i*cw, b.Min.Y, b.Min.X+(i+1)*cw, b.Max.Y)
		cx := col.Min.X + cw/2
		label := d.Label
		if i == 0 {
			label = w.t("Heute", "Today")
		}
		render.Text(img, label, cx, col.Min.Y+h*18/100, render.Style{Size: text, Bold: true, Shrink: true}, cw-4, color.Black, render.Center)
		iconH := h * 45 / 100
		icon(symbol(d.Code)).Draw(img, image.Rect(col.Min.X, col.Min.Y+h*24/100, col.Max.X, col.Min.Y+h*24/100+iconH), iconH*85/100, render.Center)
		render.Text(img, w.temp(d.Max)+" / "+w.temp(d.Min), cx, col.Max.Y-h*4/100, render.Style{Size: text, Shrink: true}, cw-4, color.Black, render.Center)
		if i > 0 && wr.Style.Rule > 0 {
			render.VLine(img, col.Min.X, col.Min.Y+h/10, col.Max.Y-h/10, wr.Style.Rule)
		}
	}
}

// hours draws today's temperature as a line over a time axis with the
// precipitation probability as shaded bars.
func (w *weather) hours(wr plugin.WidgetRequest, img *image.Paletted) {
	pts := w.cur.Hours
	if len(pts) < 2 {
		return
	}
	b := img.Rect.Inset(wr.Style.Padding)
	label := max(min(w.req.TextPx(wr)*0.6, float64(b.Dy())*0.15), 10)
	left := b.Min.X + render.Measure("-10°", label, false) + 6
	right := b.Max.X - render.Measure("00", label, false)/2
	top := b.Min.Y + int(label*1.4)
	bottom := b.Max.Y - int(label*1.4)
	if right-left < 20 || bottom-top < 20 {
		return
	}
	render.Text(img, w.t("Temperatur heute", "Temperature today"), b.Min.X, b.Min.Y+int(label), render.Style{Size: label, Bold: true, Shrink: true}, b.Dx(), color.Black, render.Left)
	lo, hi := 1e9, -1e9
	type sample struct {
		t    time.Time
		temp float64
		prob float64
	}
	var ss []sample
	for _, p := range pts {
		t, err := time.Parse(time.RFC3339, p.Time)
		if err != nil {
			continue
		}
		v := p.Values["temp"]
		lo, hi = min(lo, v), max(hi, v)
		ss = append(ss, sample{t.In(w.loc), v, p.Values["precip_prob"]})
	}
	if len(ss) < 2 {
		return
	}
	lo, hi = float64(int(lo)-1), float64(int(hi)+1)
	day := time.Date(ss[0].t.Year(), ss[0].t.Month(), ss[0].t.Day(), 0, 0, 0, 0, w.loc)
	x := func(t time.Time) int { return left + int(t.Sub(day).Hours()/24*float64(right-left)) }
	y := func(v float64) int { return bottom - int((v-lo)/(hi-lo)*float64(bottom-top)) }
	for _, s := range ss {
		if s.prob <= 0 {
			continue
		}
		x0 := x(s.t)
		bar := int(s.prob / 100 * float64(bottom-top))
		render.Fill(img, image.Rect(x0+1, bottom-bar, x0+max((right-left)/24-1, 2), bottom), "shade")
	}
	for i := 1; i < len(ss); i++ {
		render.Segment(img, x(ss[i-1].t), y(ss[i-1].temp), x(ss[i].t), y(ss[i].temp), 2, false)
	}
	render.HLine(img, left, bottom, right, 1)
	for _, v := range []float64{lo, (lo + hi) / 2, hi} {
		render.Text(img, w.temp(v), left-4, y(v)+int(label*0.35), render.Style{Size: label}, left-b.Min.X, color.Black, render.Right)
	}
	for h := 0; h <= 24; h += 6 {
		t := day.Add(time.Duration(h) * time.Hour)
		render.VLine(img, x(t), bottom, bottom+3, 1)
		render.Text(img, fmt.Sprintf("%02d", h%24), x(t), b.Max.Y, render.Style{Size: label}, 40, color.Black, render.Center)
	}
	if now := w.req.Now.In(w.loc); now.After(day) && now.Before(day.Add(24*time.Hour)) {
		render.Segment(img, x(now), top, x(now), bottom, 1, true)
	}
}

// Text fills the widgets of a character display.
func (w *weather) Text(req *plugin.Request, wr plugin.WidgetRequest) ([]string, error) {
	switch wr.Widget {
	case "current":
		c := w.cur.Current
		lines := []string{plugin.Columns(c.Text, w.num(c.Temp, 1)+"°C", wr.Width)}
		lines = append(lines, fmt.Sprintf("%s %s Bft %d", w.t("gef.", "feels"), w.temp(c.Apparent), c.Beaufort))
		return lines, nil
	case "days":
		var out []string
		for _, d := range w.dayList(wr.Height) {
			out = append(out, plugin.Columns(d.Label+" "+d.Text, w.temp(d.Max)+"/"+w.temp(d.Min), wr.Width))
		}
		return out, nil
	}
	return nil, fmt.Errorf("%w %q for text displays", plugin.ErrUnknownWidget, wr.Widget)
}
