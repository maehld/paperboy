package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/sdk/design"
	"github.com/maehld/paperboy/sdk/format"
	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

//go:embed icons/weathericons.ttf
var iconFont []byte

// iconPx is the size the glyphs are drawn at before render.NewMask trims them.
const iconPx = 128

// iconRunes maps the pictogram names of the widgets to their code points in the
// Weather Icons font. The names and the points follow css/weather-icons.css in
// https://github.com/erikflowers/weather-icons.
var iconRunes = map[string]rune{
	"day-sunny":               0xf00d,
	"night-clear":             0xf02e,
	"day-cloudy":              0xf002,
	"night-alt-partly-cloudy": 0xf081,
	"cloudy":                  0xf013,
	"fog":                     0xf014,
	"sprinkle":                0xf01c,
	"sleet":                   0xf0b5,
	"rain":                    0xf019,
	"snow":                    0xf01b,
	"thunderstorm":            0xf01e,
	"sunrise":                 0xf051,
	"sunset":                  0xf052,
}

var (
	iconOnce  sync.Once
	iconMasks map[string]*render.Mask
)

// icon returns the mask of a glyph of the embedded Weather Icons font. Each
// glyph is drawn once at a large size, so a display scales the stored shape to
// its cell.
func icon(name string) *render.Mask {
	iconOnce.Do(loadIcons)
	return iconMasks[name]
}

// loadIcons parses the font once and caches the trimmed mask of every glyph the
// widgets use.
func loadIcons() {
	iconMasks = map[string]*render.Mask{}
	f, err := opentype.Parse(iconFont)
	if err != nil {
		fmt.Fprintln(os.Stderr, "weather: parse icons:", err)
		return
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: iconPx, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		fmt.Fprintln(os.Stderr, "weather: open icons:", err)
		return
	}
	defer face.Close()
	for name, r := range iconRunes {
		if m := glyphMask(face, r); m != nil {
			iconMasks[name] = m
		}
	}
	for i := 0; i < moonGlyphs; i++ {
		if m := glyphMask(face, moonGlyphBase+rune(i)); m != nil {
			iconMasks[moonGlyphName(i)] = m
		}
	}
}

// glyphMask draws one rune on a canvas with room around its ink and returns the
// trimmed mask. A generous canvas makes the glyph size inside the face
// irrelevant.
func glyphMask(face font.Face, r rune) *render.Mask {
	b, _ := font.BoundString(face, string(r))
	const pad = 8
	w, h := (b.Max.X-b.Min.X).Ceil()+2*pad, (b.Max.Y-b.Min.Y).Ceil()+2*pad
	if w <= 2*pad || h <= 2*pad {
		return nil
	}
	img := image.NewGray(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.White, image.Point{}, draw.Src)
	d := font.Drawer{
		Dst:  img,
		Src:  image.Black,
		Face: face,
		Dot:  fixed.Point26_6{X: fixed.I(pad) - b.Min.X, Y: fixed.I(pad) - b.Min.Y},
	}
	d.DrawString(string(r))
	return render.NewMask(img)
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
	plugin.Demand

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

// forecastArgs are the arguments the forecast value reads (ADR 0120).
var forecastArgs = map[string]plugin.Arg{
	"days": {Type: "int", Default: 5, Description: "number of days, 1 to 5"},
}

// markArg selects the extrema the chart labels.
var markArg = plugin.Arg{
	Type: "string", Default: "none", Enum: []any{"none", "min", "max", "both"},
	EnumLabels: map[string]string{
		"none": "None",
		"min":  "Lowest",
		"max":  "Highest",
		"both": "Both",
	},
	Description: "mark the lowest and the highest value",
}

// legendArg hides the fill legend of the hours widget.
var legendArg = plugin.Arg{
	Type: "bool", Default: true,
	Description: "show which fill means rain, snow or both",
}

func (w *weather) Describe() plugin.Description {
	image := []string{plugin.KindImage}
	return plugin.Description{
		Title:   "Weather (Open-Meteo)",
		Version: version(),
		Config:  configKeys,
		// Values give a text page the forecast without a widget (ADR 0111).
		Values: []plugin.ValueSpec{
			{Name: "temperature", Title: "Temperature", Type: plugin.ValueNumber, Unit: "°C", Precision: 1},
			{Name: "apparent", Title: "Feels like", Type: plugin.ValueNumber, Unit: "°C", Precision: 1},
			{Name: "humidity", Title: "Humidity", Type: plugin.ValueNumber, Unit: "%", Precision: 0},
			{Name: "precipitation", Title: "Precipitation", Type: plugin.ValueNumber, Unit: "mm", Precision: 1},
			{Name: "precipitation-probability", Title: "Chance of rain", Type: plugin.ValueNumber, Unit: "%", Precision: 0},
			{Name: "precipitation-today", Title: "Precipitation today", Type: plugin.ValueNumber, Unit: "mm", Precision: 1},
			{Name: "precipitation-next", Title: "Next precipitation", Type: plugin.ValueText},
			{Name: "wind", Title: "Wind", Type: plugin.ValueNumber, Unit: "km/h", Precision: 1},
			{Name: "beaufort", Title: "Wind force", Type: plugin.ValueNumber, Unit: "Bft", Precision: 0},
			{Name: "condition", Title: "Condition", Type: plugin.ValueText},
			{Name: "min-today", Title: "Lowest today", Type: plugin.ValueNumber, Unit: "°C", Precision: 1},
			{Name: "max-today", Title: "Highest today", Type: plugin.ValueNumber, Unit: "°C", Precision: 1},
			{Name: "sunrise", Title: "Sunrise", Type: plugin.ValueTimestamp},
			{Name: "sunset", Title: "Sunset", Type: plugin.ValueTimestamp},
			{Name: "daylight", Title: "Daylight", Type: plugin.ValueText},
			{Name: "moon-phase", Title: "Moon phase", Type: plugin.ValueText},
			{Name: "moon-illumination", Title: "Moon illumination", Type: plugin.ValueNumber, Unit: "%", Precision: 0},
			{Name: "updated", Title: "Forecast time", Type: plugin.ValueTimestamp},
			{Name: "forecast", Title: "Daily forecast", Type: plugin.ValueText, Args: forecastArgs},
		},
		Widgets: []plugin.WidgetInfo{
			{Widget: "current", Title: "Current weather", Kinds: image, MinWidth: 160, MinHeight: 80,
				Sizes: []plugin.Span{{Columns: 3, Rows: 3}, {Columns: 6, Rows: 3}}},
			{Widget: "days", Title: "Daily forecast", Kinds: image, MinWidth: 200, MinHeight: 80,
				Sizes: []plugin.Span{{Columns: 3, Rows: 4}, {Columns: 6, Rows: 2}},
				Args:  forecastArgs},
			{Widget: "hours", Title: "Temperature today", Kinds: image, MinWidth: 200, MinHeight: 80,
				Sizes: []plugin.Span{{Columns: 6, Rows: 2}, {Columns: 6, Rows: 3}},
				Args:  map[string]plugin.Arg{"mark": markArg, "legend": legendArg}},
			{Widget: "sun", Title: "Sunrise and sunset", Kinds: image, MinWidth: 200, MinHeight: 80,
				Sizes: []plugin.Span{{Columns: 6, Rows: 2}, {Columns: 3, Rows: 2}}},
			{Widget: "moon", Title: "Moon phase", Kinds: image, MinWidth: 160, MinHeight: 80,
				Sizes: []plugin.Span{{Columns: 3, Rows: 2}, {Columns: 6, Rows: 2}}},
		},
	}
}

func (w *weather) Configure(cfg plugin.Values, req *plugin.Request) error {
	if req.Version != 0 && req.Version < 9 {
		return fmt.Errorf("weather needs Paperboy protocol 9 or newer; this Paperboy reports %d", req.Version)
	}
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
		var next time.Time
		for {
			if !next.IsZero() {
				if !w.Wait(ctx, 0) {
					return
				}
				if wait := time.Until(next); wait > 0 {
					select {
					case <-ctx.Done():
						return
					case <-time.After(wait):
					}
					continue
				}
			}
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
			next = time.Now().Add(w.refresh)
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
	return format.Number(v, decimals, w.lang)
}

func (w *weather) temp(v float64) string { return w.num(math.Round(v)+0, 0) + "°" }

func (w *weather) Render(req *plugin.Request, wr plugin.WidgetRequest, img *image.Paletted) error {
	switch wr.Widget {
	case "current":
		return w.current(wr, img)
	case "days":
		return w.days(wr, img)
	case "hours":
		return w.hours(wr, img)
	case "sun":
		return w.sun(wr, img)
	case "moon":
		return w.moon(wr, img)
	}
	return fmt.Errorf("%w %q", plugin.ErrUnknownWidget, wr.Widget)
}

func (w *weather) current(wr plugin.WidgetRequest, img *image.Paletted) error {
	c := w.cur.Current
	b := img.Rect.Inset(wr.Style.Padding)
	if b.Empty() {
		return plugin.TooSmall(wr)
	}
	text := b
	detail := fmt.Sprintf("%s %s · %s %d · %s/%s", w.t("gefühlt", "feels"), w.temp(c.Apparent),
		w.t("Wind", "wind"), c.Beaufort, w.temp(w.cur.Today.Max), w.temp(w.cur.Today.Min))
	hero := design.Hero{
		Value:   w.num(c.Temp, 1),
		Unit:    "°C",
		Caption: c.Text,
		Details: []string{detail},
		Note:    w.eventText(),
		Icon:    icon(c.Symbol),
	}
	if !design.DrawHero(img, text, hero, design.NewScale(w.req.TextPx(wr), b.Dy())) {
		return plugin.TooSmall(wr)
	}
	return nil
}

// nextEvent is the first event of today that is not over yet; the forecast may
// be older than the request, so the widget decides at render time.
func (w *weather) nextEvent() *precipEvent {
	for i := range w.cur.Events {
		e := &w.cur.Events[i]
		end, err := time.Parse(time.RFC3339, e.End)
		if err != nil || !end.After(w.req.Now) {
			continue
		}
		return e
	}
	return nil
}

// eventText names the next precipitation: when it starts and how long it
// lasts, or when the running one ends.
func (w *weather) eventText() string {
	e := w.nextEvent()
	if e == nil {
		return w.t("Kein Regen heute", "No rain today")
	}
	start, errS := time.Parse(time.RFC3339, e.Start)
	end, errE := time.Parse(time.RFC3339, e.End)
	if errS != nil || errE != nil {
		return w.t("Kein Regen heute", "No rain today")
	}
	kind := w.t("Regen", "Rain")
	switch e.Type {
	case precipSnow:
		kind = w.t("Schnee", "Snow")
	case precipMixed:
		kind = w.t("Regen+Schnee", "Rain+snow")
	}
	if w.req.Now.Before(start) {
		return fmt.Sprintf("%s %s %s, %s %d h", kind, w.t("ab", "from"),
			start.In(w.loc).Format("15:04"), w.t("ca.", "about"), int(end.Sub(start).Hours()))
	}
	return fmt.Sprintf("%s %s %s", kind, w.t("bis", "until"), end.In(w.loc).Format("15:04"))
}

func (w *weather) dayList(n int) []day {
	n = min(max(n, 1), 5)
	if len(w.cur.Days) < n {
		n = len(w.cur.Days)
	}
	return w.cur.Days[:n]
}

func (w *weather) days(wr plugin.WidgetRequest, img *image.Paletted) error {
	b := img.Rect.Inset(wr.Style.Padding)
	if b.Empty() {
		return plugin.TooSmall(wr)
	}
	list := w.dayList(wr.Arg().Int("days", 5))
	if len(list) == 0 {
		return nil
	}
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
		icon(symbol(d.Code, true)).Draw(img, image.Rect(col.Min.X, col.Min.Y+h*24/100, col.Max.X, col.Min.Y+h*24/100+iconH), iconH*85/100, render.Center)
		render.Text(img, w.temp(d.Max)+" / "+w.temp(d.Min), cx, col.Max.Y-h*4/100, render.Style{Size: text, Shrink: true}, cw-4, color.Black, render.Center)
		if i > 0 && wr.Style.Rule > 0 {
			render.VLine(img, col.Min.X, col.Min.Y+h/10, col.Max.Y-h/10, wr.Style.Rule)
		}
	}
	return nil
}

// hours draws today's temperature as a line over a time axis with the
// precipitation probability as bars. The fill of a bar names the type: rain is
// shaded, snow hatched and a mixed hour solid.
func (w *weather) hours(wr plugin.WidgetRequest, img *image.Paletted) error {
	b := img.Rect.Inset(wr.Style.Padding)
	if b.Empty() {
		return plugin.TooSmall(wr)
	}
	pts := w.cur.Hours
	if len(pts) < 2 {
		return nil
	}
	lo, hi := 1e9, -1e9
	type sample struct {
		t    time.Time
		temp float64
		prob float64
		fill string
	}
	var ss []sample
	for _, p := range pts {
		t, err := time.Parse(time.RFC3339, p.Time)
		if err != nil {
			continue
		}
		v := p.Values["temp"]
		lo, hi = min(lo, v), max(hi, v)
		fill := "shade"
		switch p.Type {
		case precipSnow:
			fill = "hatch"
		case precipMixed:
			fill = "solid"
		}
		ss = append(ss, sample{t.In(w.loc), v, p.Values["precip_prob"], fill})
	}
	if len(ss) < 2 {
		return nil
	}
	lo, hi = float64(int(lo)-1), float64(int(hi)+1)
	day := time.Date(ss[0].t.Year(), ss[0].t.Month(), ss[0].t.Day(), 0, 0, 0, 0, w.loc)
	temps := make([]design.Point, len(ss))
	rain := make([]design.Point, len(ss))
	for i, s := range ss {
		temps[i] = design.Point{At: s.t, Value: s.temp}
		rain[i] = design.Point{At: s.t, Value: s.prob, Fill: s.fill}
	}
	ticks := make([]time.Time, 0, 5)
	for h := 0; h <= 24; h += 6 {
		ticks = append(ticks, day.Add(time.Duration(h)*time.Hour))
	}
	var legend []design.LegendItem
	if wr.Arg().Bool("legend", true) {
		legend = []design.LegendItem{
			{Label: w.t("Regen %", "rain %"), Fill: "shade"},
			{Label: w.t("Schnee %", "snow %"), Fill: "hatch"},
			{Label: w.t("beides %", "mixed %"), Fill: "solid"},
		}
	}
	if !design.DrawTrend(img, b, design.Trend{
		From: day,
		To:   day.Add(24 * time.Hour),
		Series: []design.Series{
			{Points: rain, Style: design.Bars, Scale: &design.Range{Min: 0, Max: 100}, Fill: "shade"},
			{Points: temps, Style: design.Line, Width: 2, Halo: true},
		},
		Range: &design.Range{Min: lo, Max: hi},
		Axis:  true,
		Ticks: ticks,
		Marks: design.ExtremaMarks(wr.Arg().String("mark", "none"), temps, w.temp),
		TickFormat: func(t time.Time) string {
			return fmt.Sprintf("%02d", t.Hour())
		},
		Now:    w.req.Now.In(w.loc),
		Label:  w.t("Temperatur (°C)", "Temperature (°C)"),
		Legend: legend,
		Format: w.temp,
	}, design.NewScale(w.req.TextPx(wr), b.Dy())) {
		return plugin.TooSmall(wr)
	}
	return nil
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Values answers the declared values of a text page from the forecast the
// plugin loaded (ADR 0111). forecast carries one line per day.
func (w *weather) Values(req *plugin.Request) (map[string]plugin.Value, error) {
	if err := req.ValueArgs.Check(forecastArgs); err != nil {
		return nil, err
	}
	out := make(map[string]plugin.Value, len(req.Values))
	c, t := w.cur.Current, w.cur.Today
	at := func(s string) (plugin.Value, error) {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return plugin.Value{}, fmt.Errorf("time %q: %w", s, err)
		}
		return plugin.TimestampValue(v), nil
	}
	for _, name := range req.Values {
		switch name {
		case "temperature":
			out[name] = plugin.NumberValue(c.Temp)
		case "apparent":
			out[name] = plugin.NumberValue(c.Apparent)
		case "humidity":
			out[name] = plugin.NumberValue(c.Humidity)
		case "precipitation":
			out[name] = plugin.NumberValue(c.Precip)
		case "precipitation-probability":
			prob := 0.0
			if c.PrecipProb != nil {
				prob = *c.PrecipProb
			}
			out[name] = plugin.NumberValue(prob)
		case "precipitation-today":
			out[name] = plugin.NumberValue(t.Precip)
		case "precipitation-next":
			out[name] = plugin.TextValue(w.eventText())
		case "wind":
			out[name] = plugin.NumberValue(c.Wind)
		case "beaufort":
			out[name] = plugin.NumberValue(float64(c.Beaufort))
		case "condition":
			out[name] = plugin.TextValue(c.Text)
		case "min-today":
			out[name] = plugin.NumberValue(t.Min)
		case "max-today":
			out[name] = plugin.NumberValue(t.Max)
		case "sunrise", "sunset":
			s := t.Sunrise
			if name == "sunset" {
				s = t.Sunset
			}
			v, err := at(s)
			if err != nil {
				return nil, err
			}
			out[name] = v
		case "daylight":
			out[name] = plugin.TextValue(w.daylightText(w.daylight()))
		case "moon-phase":
			phase, _ := moonPhase(w.req.Now)
			out[name] = plugin.TextValue(w.moonName(phase))
		case "moon-illumination":
			_, illumination := moonPhase(w.req.Now)
			out[name] = plugin.NumberValue(illumination * 100)
		case "updated":
			v, err := at(w.cur.Updated)
			if err != nil {
				return nil, err
			}
			out[name] = v
		case "forecast":
			out[name] = plugin.TextValue(w.forecastLines(req.ValueArgs.Int("days", 5)))
		default:
			return nil, fmt.Errorf("%w %q", plugin.ErrUnknownValue, name)
		}
	}
	return out, nil
}

// forecastLines is the daily forecast as one line per day: label, condition
// and the range.
func (w *weather) forecastLines(days int) string {
	var lines []string
	for i, d := range w.dayList(days) {
		label := d.Label
		if i == 0 {
			label = w.t("Heute", "Today")
		}
		lines = append(lines, fmt.Sprintf("%s %s %s/%s", label, d.Text, w.temp(d.Max), w.temp(d.Min)))
	}
	return strings.Join(lines, "\n")
}
