// Package reading is the catalog of the measured values a device reports in its
// sync (ADR 0134). A device sends a bare number per reading; the catalog fixes
// its unit, precision and plausible range, so Paperboy owns the presentation and
// the firmware cannot label a value falsely.
package reading

import (
	"math"
	"strconv"
)

// Spec describes one reading a device may report.
type Spec struct {
	Name      string  // key in the sync body and in the device state
	Title     string  // operator-facing name
	TitleDE   string  // German name for a German page
	Unit      string  // unit the device sends the value in
	Precision int     // decimals of the unit
	Min, Max  float64 // plausible range; a value outside is discarded
}

// Names of the readings, so a key is spelled once.
const (
	Temperature = "temperature"
	Humidity    = "humidity"
)

// Catalog lists the readings Paperboy accepts. Its order is the display order
// of the dashboard. A name that is missing here is ignored: a newer firmware
// may report more than this server knows.
var Catalog = []Spec{
	{Name: Temperature, Title: "Temperature", TitleDE: "Temperatur", Unit: "°C", Precision: 1, Min: -40, Max: 125},
	{Name: Humidity, Title: "Humidity", TitleDE: "Luftfeuchtigkeit", Unit: "%", Precision: 0, Min: 0, Max: 100},
}

// TitleIn returns the operator-facing name in a language, for a widget that
// labels its value.
func (s Spec) TitleIn(lang string) string {
	if lang == "de" && s.TitleDE != "" {
		return s.TitleDE
	}
	return s.Title
}

// Lookup finds the spec of a reading by name.
func Lookup(name string) (Spec, bool) {
	for _, s := range Catalog {
		if s.Name == name {
			return s, true
		}
	}
	return Spec{}, false
}

// Valid reports whether a value is plausible for this reading. A failed or
// implausible measurement must not fail the sync, so the caller discards the
// value instead (ADR 0134).
func (s Spec) Valid(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= s.Min && v <= s.Max
}

// Format writes a reading with its unit in the style of the operator UI, e.g.
// "21.5 °C" or "45 %". A page formats its own language; the dashboard stays
// English (ADR 0031).
func (s Spec) Format(v float64) string {
	return strconv.FormatFloat(v, 'f', s.Precision, 64) + " " + s.Unit
}
