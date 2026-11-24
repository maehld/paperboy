package builtin

import (
	"fmt"
	"image"
	"time"

	"github.com/maehld/paperboy/sdk/design"
	"github.com/maehld/paperboy/sdk/format"
	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
	"github.com/maehld/paperboy/server/internal/reading"
)

// readingNames lists the catalog keys, so the name argument offers every
// reading Paperboy knows.
func readingNames() []any {
	out := make([]any, 0, len(reading.Catalog))
	for _, s := range reading.Catalog {
		out = append(out, s.Name)
	}
	return out
}

// readingValue answers one reading of the device a text page is rendered for.
// A device without that reading fails the value, so the cell shows its notice
// instead of a zero (ADR 0163).
func readingValue(req *plugin.Request, name string) (plugin.Value, error) {
	var dev *plugin.DeviceStatus
	if req.ServerStatus != nil {
		dev = req.ServerStatus.CurrentDeviceStatus
	}
	if r, ok := deviceReading(dev, name); ok {
		return plugin.NumberValue(r.Value), nil
	}
	return plugin.Value{}, fmt.Errorf("no reading %q for this device", name)
}

// deviceReading finds one reading of a device.
func deviceReading(dev *plugin.DeviceStatus, name string) (plugin.Reading, bool) {
	if dev == nil {
		return plugin.Reading{}, false
	}
	r, ok := dev.Readings[name]
	return r, ok
}

// readingAge names the time since a measurement in the page's language. A
// value younger than an hour counts as fresh and shows no age.
func readingAge(req *plugin.Request, taken, now time.Time) string {
	if taken.IsZero() {
		return ""
	}
	d := now.Sub(taken)
	if d < time.Hour {
		return ""
	}
	age := fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	if d < 24*time.Hour {
		age = fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return req.T("vor "+age, age+" ago")
}

// readingWidget draws one reading of a device as a hero: the value large with
// its unit, the catalog title above it and the age below (ADR 0163).
func (Plugin) readingWidget(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	a := w.Arg()
	name := a.String("name", reading.Temperature)
	spec, ok := reading.Lookup(name)
	if !ok {
		return fmt.Errorf("unknown reading %q", name)
	}
	b := img.Rect.Inset(w.Style.Padding)
	if b.Empty() {
		return plugin.TooSmall(w)
	}
	r, ok := deviceReading(statusDevice(req, a.String("device", "")), name)
	if !ok {
		cellNotice(req, w, b, img, req.T("keine Messung", "no reading"))
		return nil
	}
	hero := readingHero(req, a, spec, r)
	s := design.NewScale(req.TextPx(w), b.Dy())
	if design.DrawHero(img, b, hero, s) {
		return nil
	}
	// A short cell drops the label before it drops the value, so a 1x1 cell
	// still shows the reading.
	hero.Label = ""
	if design.DrawHero(img, b, hero, s) {
		return nil
	}
	return plugin.TooSmall(w)
}

// readingHero builds the hero of the widget from its arguments: the number in
// the page's language, the unit, the catalog title and the age.
func readingHero(req *plugin.Request, a plugin.Values, spec reading.Spec, r plugin.Reading) design.Hero {
	hero := design.Hero{
		Value: format.Number(r.Value, spec.Precision, req.Lang()),
		Align: align(a.String("align", "center"), render.Center),
	}
	if a.Bool("unit", true) {
		hero.Unit = spec.Unit
	}
	if a.Bool("label", false) {
		hero.Label = spec.TitleIn(req.Lang())
	}
	if a.Bool("age", true) {
		hero.Caption = readingAge(req, r.Taken, req.Now)
	}
	return hero
}
