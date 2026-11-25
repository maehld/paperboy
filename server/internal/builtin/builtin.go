// Package builtin is Paperboy's own plugin "paperboy" (ADR 0049). It runs in
// the Paperboy process and offers the widgets that need no data source: free
// text, a clock, the Paperboy logo, readings, the device status of ADR 0115
// and the fixed header band of ADR 0168.
package builtin

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"time"

	"github.com/maehld/paperboy/sdk/design"
	"github.com/maehld/paperboy/sdk/format"
	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
	"github.com/maehld/paperboy/server/internal/reading"
)

// Name is the plugin name pages use, e.g. paperboy/text.
const Name = "paperboy"

// Plugin implements plugin.Handler and plugin.Valuer.
type Plugin struct{}

var (
	_ plugin.Handler = Plugin{}
	_ plugin.Valuer  = Plugin{}
)

func (Plugin) Describe() plugin.Description {
	image := []string{plugin.KindImage}
	return plugin.Description{
		Title: "Paperboy",
		// Values need no data source: a text page can show its title and the
		// time without another plugin (ADR 0111).
		Values: []plugin.ValueSpec{
			{Name: "title", Title: "Page title", Type: plugin.ValueText},
			{Name: "time", Title: "Current time", Type: plugin.ValueTimestamp},
			{Name: "date", Title: "Current date", Type: plugin.ValueTimestamp},
			{Name: "now", Title: "Current date and time", Type: plugin.ValueTimestamp},
			{Name: "long", Title: "Long line", Type: plugin.ValueText},
			{Name: "block", Title: "Text block", Type: plugin.ValueText},
			// The room the device hangs in, not the weather outside: the
			// plugin prefix keeps it apart from weather/temperature.
			{Name: reading.Temperature, Title: "Room temperature", Type: plugin.ValueNumber, Unit: "°C", Precision: 1},
			{Name: reading.Humidity, Title: "Room humidity", Type: plugin.ValueNumber, Unit: "%", Precision: 0},
		},
		Widgets: []plugin.WidgetInfo{
			{Widget: "battery", Title: "Battery", Kinds: image, Sizes: []plugin.Span{{Columns: 1, Rows: 1}, {Columns: 2, Rows: 1}, {Columns: 3, Rows: 1}, {Columns: 2, Rows: 2}},
				Args: map[string]plugin.Arg{
					"device": {Type: "string", Description: "device id; default the device that shows the page"},
					"label":  {Type: "string", Default: "auto", Description: "auto, percent, volts or none", Enum: []any{"auto", "percent", "volts", "none"}},
					"align":  {Type: "string", Default: "center", Description: "left, center or right", Enum: []any{"left", "center", "right"}},
				}},
			{Widget: "device-status", Title: "Device status", Kinds: image, Sizes: []plugin.Span{{Columns: 2, Rows: 2}, {Columns: 3, Rows: 2}, {Columns: 6, Rows: 1}, {Columns: 6, Rows: 2}, {Columns: 6, Rows: 3}},
				Args: map[string]plugin.Arg{
					"devices": {Type: "list", Description: "device ids; default the device that shows the page"},
					"battery": {Type: "bool", Default: true, Description: "battery charge"},
					"runtime": {Type: "bool", Default: true, Description: "estimated battery runtime"},
					"seen":    {Type: "bool", Default: true, Description: "time since the last contact"},
				}},
			{Widget: "reading", Title: "Reading", Kinds: image, Sizes: []plugin.Span{{Columns: 1, Rows: 1}, {Columns: 2, Rows: 1}, {Columns: 3, Rows: 1}, {Columns: 3, Rows: 2}},
				Args: map[string]plugin.Arg{
					"name":   {Type: "string", Default: reading.Temperature, Description: "reading of the catalog", Enum: readingNames()},
					"device": {Type: "string", Description: "device id; default the device that shows the page"},
					"label":  {Type: "bool", Default: false, Description: "the catalog title above the value"},
					"unit":   {Type: "bool", Default: true, Description: "unit after the value"},
					"age":    {Type: "bool", Default: true, Description: "age in the caption once the value is older than an hour"},
					"align":  {Type: "string", Default: "center", Description: "left, center or right", Enum: []any{"left", "center", "right"}},
				}},
			{Widget: "text", Title: "Text", Kinds: image, Sizes: []plugin.Span{{Columns: 2, Rows: 1}, {Columns: 6, Rows: 1}},
				Args: map[string]plugin.Arg{
					"text":  {Type: "string", Required: true, Description: "lines separated by \\n"},
					"align": {Type: "string", Default: "center", Enum: []any{"left", "center", "right"}},
					"size":  {Type: "float", Default: 0.5, Description: "text height as a fraction of the line height"},
					"bold":  {Type: "bool", Default: false},
				}},
			{Widget: "clock", Title: "Clock", Kinds: image, Sizes: []plugin.Span{{Columns: 2, Rows: 1}, {Columns: 3, Rows: 2}, {Columns: 6, Rows: 2}},
				Args: map[string]plugin.Arg{
					"format": {Type: "string", Default: "time", Description: "time, date, datetime or auto: the longest date form that fits plus the time", Enum: []any{"time", "date", "datetime", "auto"}},
					"align":  {Type: "string", Default: "center", Description: "left, center or right", Enum: []any{"left", "center", "right"}},
				}},
			{Widget: "logo", Title: "Paperboy logo", Kinds: []string{plugin.KindImage}, Sizes: []plugin.Span{{Columns: 2, Rows: 1}, {Columns: 3, Rows: 2}, {Columns: 4, Rows: 3}, {Columns: 6, Rows: 3}},
				Args: map[string]plugin.Arg{
					"wordmark": {Type: "bool", Default: false, Description: "with the wordmark, as in the lockup"},
				}},
		},
	}
}

func (Plugin) Configure(plugin.Values, *plugin.Request) error { return nil }

// Values answers the declared values of a text page: the page title and the
// page's moment. date is midnight, so Paperboy shows its date; time and now
// are the current moment and show their time (ADR 0111).
func (Plugin) Values(req *plugin.Request) (map[string]plugin.Value, error) {
	out := make(map[string]plugin.Value, len(req.Values))
	now := req.Now.In(req.Location())
	for _, name := range req.Values {
		switch name {
		case "title":
			out[name] = plugin.TextValue(req.Page.Title)
		case "time", "now":
			out[name] = plugin.TimestampValue(now)
		case "date":
			out[name] = plugin.TimestampValue(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()))
		case "long":
			out[name] = plugin.TextValue("Diese Zeile ist absichtlich zu lang für das Display.")
		case "block":
			out[name] = plugin.TextValue("Zeile eins mit einem sehrlangenWort\nZeile zwei\nZeile drei\nZeile vier")
		case reading.Temperature, reading.Humidity:
			v, err := readingValue(req, name)
			if err != nil {
				return nil, err
			}
			out[name] = v
		default:
			return nil, fmt.Errorf("%w %q", plugin.ErrUnknownValue, name)
		}
	}
	return out, nil
}

func align(s string, def render.Align) render.Align {
	switch s {
	case "left":
		return render.Left
	case "center":
		return render.Center
	case "right":
		return render.Right
	}
	return def
}

func (p Plugin) Render(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	switch w.Widget {
	case "band":
		return p.band(req, w, img)
	case "text":
		return p.text(w, img)
	case "clock":
		return p.clock(req, w, img)
	case "logo":
		return p.logo(w, img)
	case "battery":
		return p.battery(req, w, img)
	case "device-status":
		return p.deviceStatus(req, w, img)
	case "reading":
		return p.readingWidget(req, w, img)
	}
	return fmt.Errorf("%w %q", plugin.ErrUnknownWidget, w.Widget)
}

// band draws the fixed header band of a display type: the page title and its
// page indicator on the left, then temperature, humidity, clock and battery
// right-aligned (ADR 0168). All text uses one size; the title is bold and the
// battery gauge is a little smaller.
func (Plugin) band(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	a := w.Arg()
	b := img.Rect
	pad := w.Style.Padding
	if b.Dy()-2*pad < 4 || b.Dx()-2*pad < 4 {
		return plugin.TooSmall(w)
	}
	title := req.Page.Title
	if a.Bool("page_indicator", true) && req.Page.Count > 1 {
		title += fmt.Sprintf(" %d/%d", req.Page.Number, req.Page.Count)
	}
	// The right group, from left to right. Two spaces separate the items.
	var leftToRight []string
	if a.Bool("temperature", false) {
		leftToRight = append(leftToRight, bandReading(req, reading.Temperature))
	}
	if a.Bool("humidity", false) {
		leftToRight = append(leftToRight, bandReading(req, reading.Humidity))
	}
	if clock := a.String("clock", "time"); clock != "none" {
		if s := clockText(req, clock); s != "" {
			leftToRight = append(leftToRight, s)
		}
	}
	items := leftToRight
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	var battery *plugin.BatteryStatus
	if a.Bool("battery", false) {
		if dev := statusDevice(req, ""); dev != nil {
			battery = dev.Battery
		}
	}
	left := b.Min.X + pad
	right := b.Max.X - pad
	available := right - left
	size := float64(b.Dy()) * 0.5
	// All text shares one size. A crowded band steps the size down, so the
	// title and the right group stay equal and readable.
	for {
		gap := max(2, render.Measure("  ", size, false))
		parts := len(items)
		if battery != nil {
			parts++
		}
		total := render.Measure(title, size, true) + parts*gap
		for _, s := range items {
			total += render.Measure(s, size, false)
		}
		if battery != nil {
			total += int(capHeight(size)) * 2
		}
		if parts == 0 || total <= available || size <= render.MinSize {
			break
		}
		size--
	}
	gap := max(2, render.Measure("  ", size, false))
	baseline := b.Min.Y + b.Dy()/2 + int(size*0.36)
	if battery != nil {
		iconH := min(capHeight(size), float64(b.Dy()-2*pad))
		iconW := int(iconH * 2)
		if iconW >= 4 && int(iconH) >= 3 && right-iconW-gap > left {
			r := image.Rect(right-iconW, baseline-int(iconH), right, baseline)
			design.DrawBattery(img, r, batteryGauge(battery), 0)
			right -= iconW + gap
		}
	}
	for _, s := range items {
		bandText(img, s, size, baseline, &right, gap, left)
	}
	render.Text(img, title, left, baseline, render.Style{Size: size, Bold: true, Shrink: true},
		max(right-left, 1), color.Black, render.Left)
	return nil
}

// bandText draws one right-aligned item of the band and moves the right edge
// left for the next item.
func bandText(img *image.Paletted, s string, size float64, baseline int, right *int, gap, minX int) {
	if s == "" {
		return
	}
	w := render.Measure(s, size, false)
	if w < 1 || *right-w < minX {
		return
	}
	render.Text(img, s, *right, baseline, render.Style{Size: size, Shrink: true}, w+2, color.Black, render.Right)
	*right -= w + gap
}

// bandReading is one reading of the device as a single band text, e.g.
// "21,5 °C". A missing reading says so in the page language.
func bandReading(req *plugin.Request, name string) string {
	spec, ok := reading.Lookup(name)
	if !ok {
		return ""
	}
	r, ok := deviceReading(statusDevice(req, ""), name)
	if !ok {
		return req.T("keine Messung", "no reading")
	}
	text := format.Number(r.Value, spec.Precision, req.Lang())
	if spec.Unit != "" {
		text += " " + spec.Unit
	}
	return text
}

// textLines splits the text argument into lines. A literal \n and a real line
// break are the same, and one trailing line break adds no empty line.
func textLines(s string) []string {
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

func (Plugin) text(w plugin.WidgetRequest, img *image.Paletted) error {
	a := w.Arg()
	lines := textLines(a.String("text", ""))
	pad := w.Style.Padding
	b := img.Rect.Inset(pad)
	if b.Empty() {
		return plugin.TooSmall(w)
	}
	lineH := b.Dy() / len(lines)
	al := align(a.String("align", "center"), render.Center)
	x := b.Min.X + b.Dx()/2
	switch al {
	case render.Left:
		x = b.Min.X
	case render.Right:
		x = b.Max.X
	}
	for i, l := range lines {
		base := b.Min.Y + i*lineH + lineH*3/5
		render.Text(img, l, x, base, render.Style{Size: float64(lineH) * a.Float("size", 0.5), Bold: a.Bool("bold", false), Shrink: true},
			b.Dx(), color.Black, al)
	}
	return nil
}

var (
	daysDE   = [7]string{"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"}
	monthsDE = [12]string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"}
)

// dateForms returns the date of now from the longest to the shortest form.
func dateForms(req *plugin.Request) []string {
	now := req.Now.In(req.Location())
	if req.Lang() == "en" {
		return []string{
			now.Format("Monday, January 2, 2006"),
			now.Format("Mon, Jan 2, 2006"),
			now.Format("Mon, Jan 2"),
			now.Format("Jan 2"),
		}
	}
	day := daysDE[now.Weekday()]
	return []string{
		fmt.Sprintf("%s, %d. %s %d", day, now.Day(), monthsDE[now.Month()-1], now.Year()),
		fmt.Sprintf("%s, %d.%d.%d", day[:2], now.Day(), int(now.Month()), now.Year()),
		fmt.Sprintf("%s, %d.%d.", day[:2], now.Day(), int(now.Month())),
		fmt.Sprintf("%d.%d.", now.Day(), int(now.Month())),
	}
}

// headerStatus builds the right side of the header: the date (arg date, in
// the longest form that fits) and the time (arg clock). When even the shortest
// date does not fit, the date is left out.
func headerStatus(req *plugin.Request, a plugin.Values, fits func(string) bool) string {
	clock := ""
	if a.Bool("clock", true) {
		clock = req.Now.In(req.Location()).Format("15:04")
	}
	if !a.Bool("date", false) {
		return clock
	}
	for _, d := range dateForms(req) {
		s := d
		if clock != "" {
			s += "  " + clock
		}
		if fits(s) {
			return s
		}
	}
	return clock
}

func clockText(req *plugin.Request, format string) string {
	now := req.Now.In(req.Location())
	date := now.Format("2.1.2006")
	day := [7]string{"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"}[now.Weekday()]
	if req.Lang() == "en" {
		date = now.Format("Jan 2, 2006")
		day = now.Weekday().String()[:3]
	}
	switch format {
	case "date":
		return day + ", " + date
	case "datetime":
		return day + ", " + date + " " + now.Format("15:04")
	}
	return now.Format("15:04")
}

func (Plugin) clock(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	a := w.Arg()
	b := img.Rect.Inset(w.Style.Padding)
	if b.Empty() {
		return plugin.TooSmall(w)
	}
	size := float64(b.Dy()) * 0.7
	s := clockText(req, a.String("format", "time"))
	if a.String("format", "time") == "auto" {
		// The header uses the longest date form that fits and keeps the time
		// when even the shortest date does not fit.
		s = headerStatus(req, plugin.Values{"date": true, "clock": true}, func(candidate string) bool {
			return render.Measure(candidate, size, false) <= b.Dx()
		})
	}
	if s == "" {
		return nil
	}
	al := align(a.String("align", "center"), render.Center)
	x := b.Min.X + b.Dx()/2
	switch al {
	case render.Left:
		x = b.Min.X
	case render.Right:
		x = b.Max.X
	}
	render.Text(img, s, x, b.Min.Y+b.Dy()/2+int(size*0.36), render.Style{Size: size, Bold: true, Shrink: true},
		b.Dx(), color.Black, al)
	return nil
}
