package main

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

// fuelArg selects the fuel of a widget or a value.
var fuelArg = plugin.Arg{
	Type:        "string",
	Default:     fuelDiesel,
	Enum:        []any{fuelDiesel, fuelE5, fuelE10},
	EnumLabels:  map[string]string{fuelDiesel: "Diesel", fuelE5: "Super E5", fuelE10: "Super E10"},
	Description: "fuel type to show",
}

// stationsArg narrows a list to its first rows.
var stationsArg = plugin.Arg{
	Type:        "int",
	Default:     5,
	Description: "number of stations, 1 to 10",
}

// spanArg is the window of the history chart.
var spanArg = plugin.Arg{
	Type:        "duration",
	Default:     "24h",
	Description: "window of the chart, 1h to 168h",
}

var fuelValueArgs = map[string]plugin.Arg{"fuel": fuelArg}

// valueArgs are the arguments of every declared value. A values request
// carries one set, so the check sees the union.
var valueArgs = map[string]plugin.Arg{"fuel": fuelArg, "stations": stationsArg}

func (f *fuel) Describe() plugin.Description {
	image := []string{plugin.KindImage}
	return plugin.Description{
		Title:   "Fuel prices (Tankerkönig)",
		Version: version(),
		Config:  configKeys,
		Values: []plugin.ValueSpec{
			{Name: "price", Title: "Cheapest price", Type: plugin.ValueNumber, Unit: "€/l", Precision: 3, Args: fuelValueArgs},
			{Name: "station", Title: "Cheapest station", Type: plugin.ValueText, Args: fuelValueArgs},
			{Name: "distance", Title: "Distance to the cheapest station", Type: plugin.ValueNumber, Unit: "km", Precision: 1, Args: fuelValueArgs},
			{Name: "open", Title: "Open state of the cheapest station", Type: plugin.ValueText, Args: fuelValueArgs},
			{Name: "list", Title: "Nearest stations", Type: plugin.ValueText,
				Args: map[string]plugin.Arg{"fuel": fuelArg, "stations": stationsArg}},
			{Name: "updated", Title: "Time of the last fetch", Type: plugin.ValueTimestamp},
		},
		Widgets: []plugin.WidgetInfo{
			{Widget: "prices", Title: "Nearest stations", Kinds: image, MinWidth: 200, MinHeight: 80,
				Sizes: []plugin.Span{{Columns: 3, Rows: 4}, {Columns: 6, Rows: 3}},
				Args:  map[string]plugin.Arg{"fuel": fuelArg, "stations": stationsArg}},
			{Widget: "cheapest", Title: "Cheapest station", Kinds: image, MinWidth: 160, MinHeight: 70,
				Sizes: []plugin.Span{{Columns: 3, Rows: 2}, {Columns: 6, Rows: 2}},
				Args:  map[string]plugin.Arg{"fuel": fuelArg}},
			{Widget: "history", Title: "Price history", Kinds: image, MinWidth: 220, MinHeight: 90,
				Sizes: []plugin.Span{{Columns: 6, Rows: 2}, {Columns: 3, Rows: 3}},
				Args:  map[string]plugin.Arg{"fuel": fuelArg, "span": spanArg}},
		},
	}
}

func (f *fuel) Render(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	switch w.Widget {
	case "prices":
		return f.prices(w, img)
	case "cheapest":
		return f.cheapest(w, img)
	case "history":
		return f.historyWidget(w, img)
	}
	return fmt.Errorf("%w %q", plugin.ErrUnknownWidget, w.Widget)
}

// Values answers the declared values of a text page from the snapshot of the
// request (ADR 0111).
func (f *fuel) Values(req *plugin.Request) (map[string]plugin.Value, error) {
	if err := req.ValueArgs.Check(valueArgs); err != nil {
		return nil, err
	}
	fuelName, err := fuelFrom(req.ValueArgs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]plugin.Value, len(req.Values))
	for _, name := range req.Values {
		switch name {
		case "price", "station", "distance", "open":
			s, ok := cheapestStation(f.cur, fuelName)
			if !ok {
				return nil, fmt.Errorf("no station offers %s", fuelLabel(fuelName))
			}
			v, _ := s.fuel(fuelName)
			switch name {
			case "price":
				out[name] = plugin.NumberValue(v)
			case "station":
				out[name] = plugin.TextValue(s.label())
			case "distance":
				out[name] = plugin.NumberValue(s.Dist)
			case "open":
				out[name] = plugin.TextValue(f.openText(s.Open))
			}
		case "list":
			out[name] = plugin.TextValue(f.listLines(fuelName, req.ValueArgs.Int("stations", 5)))
		case "updated":
			out[name] = plugin.TimestampValue(f.curAt)
		default:
			return nil, fmt.Errorf("%w %q", plugin.ErrUnknownValue, name)
		}
	}
	return out, nil
}

// listLines is one line per station: name, price and distance.
func (f *fuel) listLines(fuelName string, n int) string {
	list := stationsWithFuel(f.cur, fuelName)
	n = min(max(n, 1), maxStations)
	if len(list) > n {
		list = list[:n]
	}
	var lines []string
	for _, s := range list {
		v, _ := s.fuel(fuelName)
		line := fmt.Sprintf("%s %s %s", s.label(), f.money(v), f.km(s.Dist))
		if !s.Open {
			line += " " + f.t("zu", "closed")
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// fuelFrom reads the fuel argument.
func fuelFrom(v plugin.Values) (string, error) {
	name := v.String("fuel", fuelDiesel)
	switch name {
	case fuelDiesel, fuelE5, fuelE10:
		return name, nil
	}
	return "", fmt.Errorf("fuel %q: expected diesel, e5 or e10", name)
}

// fuelLabel is the name of a fuel in both languages.
func fuelLabel(name string) string {
	switch name {
	case fuelE5:
		return "Super E5"
	case fuelE10:
		return "Super E10"
	}
	return "Diesel"
}

// stationsWithFuel keeps the stations that offer the fuel, nearest first.
func stationsWithFuel(stations []station, name string) []station {
	out := make([]station, 0, len(stations))
	for _, s := range stations {
		if _, ok := s.fuel(name); ok {
			out = append(out, s)
		}
	}
	return out
}

// cheapestStation returns the station with the lowest price. An open station
// wins over a closed one, and the nearer station wins a tie.
func cheapestStation(stations []station, name string) (station, bool) {
	var open, closed station
	var openPrice, closedPrice float64
	hasOpen, hasClosed := false, false
	for _, s := range stations {
		v, ok := s.fuel(name)
		if !ok {
			continue
		}
		switch {
		case s.Open && (!hasOpen || lower(v, s.Dist, openPrice, open.Dist)):
			open, openPrice, hasOpen = s, v, true
		case !s.Open && (!hasClosed || lower(v, s.Dist, closedPrice, closed.Dist)):
			closed, closedPrice, hasClosed = s, v, true
		}
	}
	if hasOpen {
		return open, true
	}
	return closed, hasClosed
}

// lower reports whether a price beats another; a tie goes to the nearer
// station.
func lower(price, dist, otherPrice, otherDist float64) bool {
	if price != otherPrice {
		return price < otherPrice
	}
	return dist < otherDist
}

func (f *fuel) openText(open bool) string {
	if open {
		return f.t("offen", "open")
	}
	return f.t("zu", "closed")
}

// t picks the German or the English text of the request's language.
func (f *fuel) t(de, en string) string { return f.req.T(de, en) }

// num formats a number with a decimal comma for German.
func (f *fuel) num(v float64, decimals int) string {
	s := fmt.Sprintf("%.*f", decimals, v)
	if f.lang == "de" {
		s = strings.Replace(s, ".", ",", 1)
	}
	return s
}

// money is a price per litre with three decimals.
func (f *fuel) money(v float64) string { return f.num(v, 3) }

// km is a distance with one decimal.
func (f *fuel) km(v float64) string { return f.num(v, 1) + " km" }

// message draws a short text in the middle of the cell.
func (f *fuel) message(w plugin.WidgetRequest, img *image.Paletted, text string) error {
	b := img.Rect.Inset(w.Style.Padding)
	if b.Empty() {
		return plugin.TooSmall(w)
	}
	size := min(f.req.TextPx(w), float64(b.Dy())*0.5)
	size = max(size, render.MinSize)
	render.Text(img, text, b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2+int(size*0.35),
		render.Style{Size: size, Shrink: true}, b.Dx(), color.Black, render.Center)
	return nil
}
