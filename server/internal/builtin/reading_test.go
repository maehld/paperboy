package builtin

import (
	"errors"
	"image"
	"testing"
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
	"github.com/maehld/paperboy/server/internal/reading"
)

// readingRequest returns a request whose device reported the given readings.
func readingRequest(lang string, taken time.Time, readings map[string]float64) *plugin.Request {
	dev := &plugin.DeviceStatus{ID: "kueche", Status: "ok"}
	if len(readings) > 0 {
		dev.Readings = map[string]plugin.Reading{}
	}
	for name, v := range readings {
		dev.Readings[name] = plugin.Reading{Value: v, Taken: taken}
	}
	return &plugin.Request{
		Language:     lang,
		Timezone:     "UTC",
		Now:          testStatusTime,
		ServerStatus: &plugin.ServerStatus{CurrentDeviceStatus: dev},
	}
}

func renderReading(t *testing.T, req *plugin.Request, w, h int, args map[string]any) (*image.Paletted, image.Rectangle) {
	t.Helper()
	style := plugin.Style{Padding: 4, Rule: 1, TextPx: 16}
	img := render.NewCanvas(w, h, 2)
	wr := plugin.WidgetRequest{Widget: "reading", Width: w, Height: h, Args: args, Style: style}
	if err := (Plugin{}).Render(req, wr, img); err != nil {
		t.Fatalf("reading %dx%d: %v", w, h, err)
	}
	return img, image.Rect(0, 0, w, h).Inset(style.Padding)
}

func TestReadingHeroFollowsLanguage(t *testing.T) {
	taken := testStatusTime.Add(-2 * time.Hour)
	spec, ok := reading.Lookup(reading.Temperature)
	if !ok {
		t.Fatal("temperature is not in the catalog")
	}
	r := plugin.Reading{Value: 21.5, Taken: taken}
	for _, c := range []struct {
		lang, value, unit, label, caption string
		args                              map[string]any
	}{
		{"de", "21,5", "°C", "", "vor 2h", nil},
		{"en", "21.5", "°C", "", "2h ago", nil},
		{"de", "21,5", "", "", "", map[string]any{"unit": false, "age": false}},
		{"de", "21,5", "°C", "Temperatur", "", map[string]any{"label": true, "age": false}},
		{"en", "21.5", "°C", "Temperature", "", map[string]any{"label": true, "age": false}},
	} {
		req := readingRequest(c.lang, taken, nil)
		hero := readingHero(req, plugin.Values(c.args), spec, r)
		if hero.Value != c.value || hero.Unit != c.unit || hero.Label != c.label || hero.Caption != c.caption {
			t.Errorf("%s %v: %+v", c.lang, c.args, hero)
		}
	}
	// A fresh value carries no age, so a cycle does not print "vor 0h".
	req := readingRequest("de", testStatusTime.Add(-10*time.Minute), nil)
	if caption := readingHero(req, plugin.Values(nil), spec, plugin.Reading{Value: 21.5, Taken: req.Now.Add(-10 * time.Minute)}).Caption; caption != "" {
		t.Errorf("fresh value shows %q", caption)
	}
}

func TestReadingAge(t *testing.T) {
	req := &plugin.Request{Language: "de"}
	at := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		d    time.Duration
		want string
	}{
		{10 * time.Minute, ""},
		{2 * time.Hour, "vor 2h"},
		{25 * time.Hour, "vor 1d"},
	} {
		if got := readingAge(req, at, at.Add(c.d)); got != c.want {
			t.Errorf("%v: got %q, want %q", c.d, got, c.want)
		}
	}
	if got := readingAge(req, time.Time{}, at.Add(48*time.Hour)); got != "" {
		t.Errorf("a value without a time shows %q", got)
	}
}

func TestReadingWidgetDraws(t *testing.T) {
	for _, c := range []struct {
		name  string
		w, h  int
		args  map[string]any
		value float64
	}{
		{"2x1", 200, 60, nil, 21.5},
		{"1x1", 103, 30, nil, 21.5},
		{"3x2-label", 312, 175, map[string]any{"label": true}, 21.5},
		{"humidity", 200, 60, map[string]any{"name": "humidity"}, 45},
		{"no-unit", 200, 60, map[string]any{"unit": false}, 21.5},
	} {
		req := readingRequest("de", testStatusTime.Add(-2*time.Hour), map[string]float64{"temperature": c.value, "humidity": c.value})
		img, cell := renderReading(t, req, c.w, c.h, c.args)
		box, ok := inkBox(img, img.Rect)
		if !ok {
			t.Fatalf("%s: nothing drawn", c.name)
		}
		if !box.In(cell) {
			t.Errorf("%s: ink %v leaves the padded cell %v", c.name, box, cell)
		}
	}
}

// A device without the reading shows a notice in its cell instead of leaving it
// empty or failing the page.
func TestReadingWithoutValue(t *testing.T) {
	req := readingRequest("de", testStatusTime, map[string]float64{"humidity": 45})
	img, cell := renderReading(t, req, 200, 60, map[string]any{"name": "temperature"})
	if inkCount(img, cell) == 0 {
		t.Error("a missing reading left the cell empty")
	}
	// An unknown name is a page error, not a notice: the catalog has no unit
	// and no precision for it.
	w := plugin.WidgetRequest{Widget: "reading", Width: 200, Height: 60,
		Args: map[string]any{"name": "pressure"}, Style: plugin.Style{Padding: 4, Rule: 1, TextPx: 16}}
	if err := (Plugin{}).Render(req, w, render.NewCanvas(200, 60, 2)); err == nil {
		t.Error("an unknown reading name drew something")
	}
}

func TestReadingValueNeedsADevice(t *testing.T) {
	req := readingRequest("de", testStatusTime, map[string]float64{"temperature": 21.5})
	got, err := Plugin{}.Values(&plugin.Request{Language: "de", Timezone: "UTC", Now: testStatusTime,
		ServerStatus: req.ServerStatus, Values: []string{"temperature"}})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := got["temperature"]; !ok || v.Number == nil || *v.Number != 21.5 {
		t.Errorf("temperature value: %+v", got)
	}
	// A reading the device did not report fails that value, so the cell shows
	// its notice rather than a zero.
	if _, err := (Plugin{}).Values(&plugin.Request{Language: "de", Timezone: "UTC", Now: testStatusTime,
		ServerStatus: req.ServerStatus, Values: []string{"humidity"}}); err == nil {
		t.Error("a missing reading answered a value")
	}
	if _, err := (Plugin{}).Values(&plugin.Request{Language: "de", Timezone: "UTC", Now: testStatusTime,
		Values: []string{"temperature"}}); err == nil {
		t.Error("a request without a device answered a value")
	}
}

func TestReadingDeclared(t *testing.T) {
	d := Plugin{}.Describe()
	values := map[string]plugin.ValueSpec{}
	for _, v := range d.Values {
		values[v.Name] = v
	}
	for _, name := range []string{reading.Temperature, reading.Humidity} {
		spec, ok := reading.Lookup(name)
		if !ok {
			t.Fatalf("%s is not in the catalog", name)
		}
		v, ok := values[name]
		if !ok {
			t.Fatalf("value %q is not declared", name)
		}
		if v.Type != plugin.ValueNumber || v.Unit != spec.Unit || v.Precision != spec.Precision {
			t.Errorf("value %q: %+v", name, v)
		}
	}
	for _, w := range d.Widgets {
		if w.Widget != "reading" {
			continue
		}
		name, ok := w.Args["name"]
		if !ok || name.Default != reading.Temperature {
			t.Fatalf("reading name argument: %+v", w.Args)
		}
		if len(name.Enum) != len(reading.Catalog) {
			t.Errorf("name enum %v does not list the catalog", name.Enum)
		}
		if _, ok := w.Args["device"]; !ok {
			t.Error("the reading widget has no device argument")
		}
		return
	}
	t.Fatal("the reading widget is not described")
}

func TestReadingTooSmall(t *testing.T) {
	req := readingRequest("de", testStatusTime, map[string]float64{"temperature": 21.5})
	w := plugin.WidgetRequest{Widget: "reading", Width: 10, Height: 10,
		Style: plugin.Style{Padding: 8, Rule: 1, TextPx: 16}}
	if err := (Plugin{}).Render(req, w, render.NewCanvas(10, 10, 2)); !errors.Is(err, plugin.ErrTooSmall) {
		t.Errorf("a 10x10 cell: error = %v, want ErrTooSmall", err)
	}
}
