package main

import (
	"bytes"
	"errors"
	"image/png"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

func berlin(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func at(y int, m time.Month, d, h, min int, loc *time.Location) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, loc)
}

func request(t *testing.T, now time.Time, args map[string]any) (*plugin.Request, plugin.WidgetRequest) {
	t.Helper()
	loc := berlin(t)
	return &plugin.Request{
			Language: "de", Timezone: "Europe/Berlin", Now: now.In(loc),
			Page: plugin.PageInfo{PPI: 125, Width: 800, Height: 480},
		}, plugin.WidgetRequest{
			Widget: "countdown", Width: 400, Height: 120, Args: args,
			Style: plugin.Style{Padding: 8, Rule: 1, TextPx: 28},
		}
}

func TestParseDate(t *testing.T) {
	loc := berlin(t)
	now := at(2026, 11, 20, 12, 0, loc)
	cases := []struct {
		in     string
		want   time.Time
		yearly bool
		timed  bool
	}{
		{"2026-12-24", at(2026, 12, 24, 0, 0, loc), false, false},
		{"2026-12-24T18:30", at(2026, 12, 24, 18, 30, loc), false, true},
		{"12-24", at(2026, 12, 24, 0, 0, loc), true, false},
		{"01-01", at(2027, 1, 1, 0, 0, loc), true, false},
		{"11-20", at(2026, 11, 20, 0, 0, loc), true, false},
	}
	for _, c := range cases {
		got, yearly, timed, err := parseDate(c.in, now, loc)
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if !got.Equal(c.want) || yearly != c.yearly || timed != c.timed {
			t.Errorf("%s = %v yearly=%v timed=%v, want %v %v %v", c.in, got, yearly, timed, c.want, c.yearly, c.timed)
		}
	}
	for _, bad := range []string{"", "24.12.2026", "2026-02-30", "12-24 18:00"} {
		if _, _, _, err := parseDate(bad, now, loc); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	// A yearly date on the day itself stays in this year.
	if got, _, _, _ := parseDate("12-24", at(2026, 12, 24, 15, 0, loc), loc); got.Year() != 2026 {
		t.Errorf("yearly date on its day became %v", got)
	}
}

func TestParseSince(t *testing.T) {
	loc := berlin(t)
	target := at(2026, 7, 26, 0, 0, loc)
	cases := map[string]time.Time{
		"2026-07-12":       at(2026, 7, 12, 0, 0, loc),
		"2026-07-12T18:00": at(2026, 7, 12, 18, 0, loc),
		"07-12":            at(2026, 7, 12, 0, 0, loc),
		"08-01":            at(2025, 8, 1, 0, 0, loc),
	}
	for in, want := range cases {
		got, err := parseSince(in, target, loc)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if !got.Equal(want) {
			t.Errorf("%s = %v, want %v", in, got, want)
		}
	}
	for _, bad := range []string{"", "12.07.2026", "2026-13-01"} {
		if _, err := parseSince(bad, target, loc); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestEvaluate(t *testing.T) {
	loc := berlin(t)
	eval := func(now, target time.Time, timed bool, since time.Time, bar bool) state {
		return (&args{now: now, target: target, timed: timed, since: since, bar: bar, loc: loc}).evaluate()
	}
	if st := eval(at(2026, 11, 20, 12, 0, loc), at(2026, 12, 24, 0, 0, loc), false, time.Time{}, false); st.days != 34 || st.passed || st.today {
		t.Errorf("christmas: %+v", st)
	}
	if st := eval(at(2026, 12, 24, 12, 0, loc), at(2026, 12, 24, 0, 0, loc), false, time.Time{}, false); !st.today {
		t.Errorf("christmas day: %+v", st)
	}
	if st := eval(at(2026, 12, 25, 0, 1, loc), at(2026, 12, 24, 0, 0, loc), false, time.Time{}, false); !st.passed {
		t.Errorf("after christmas: %+v", st)
	}
	// A DST change does not shift a calendar count.
	if st := eval(at(2026, 3, 28, 12, 0, loc), at(2026, 3, 29, 0, 0, loc), false, time.Time{}, false); st.days != 1 {
		t.Errorf("DST day count %d, want 1", st.days)
	}
	// A timed target counts hours in its last day, and days before that.
	if st := eval(at(2026, 12, 24, 12, 0, loc), at(2026, 12, 24, 18, 0, loc), true, time.Time{}, false); !st.timed || st.remain != 6*time.Hour {
		t.Errorf("timed last day: %+v", st)
	}
	if st := eval(at(2026, 12, 23, 18, 1, loc), at(2026, 12, 24, 18, 0, loc), true, time.Time{}, false); !st.timed {
		t.Errorf("23h59 left is not timed: %+v", st)
	}
	if st := eval(at(2026, 12, 23, 18, 0, loc), at(2026, 12, 24, 18, 0, loc), true, time.Time{}, false); st.timed || st.days != 1 {
		t.Errorf("24h left: %+v", st)
	}
	if st := eval(at(2026, 12, 24, 18, 0, loc), at(2026, 12, 24, 18, 0, loc), true, time.Time{}, false); !st.passed {
		t.Errorf("timed target reached is not passed: %+v", st)
	}
	// The bar covers the span from since to target.
	st := (&args{
		now: at(2026, 12, 16, 0, 0, loc), target: at(2026, 12, 31, 0, 0, loc),
		since: at(2026, 12, 1, 0, 0, loc), bar: true, loc: loc,
	}).evaluate()
	if math.Abs(st.fraction-0.5) > 0.001 {
		t.Errorf("fraction %v, want 0.5", st.fraction)
	}
	for _, before := range []time.Time{at(2026, 11, 1, 0, 0, loc), at(2027, 1, 5, 0, 0, loc)} {
		st := (&args{now: before, target: at(2026, 12, 31, 0, 0, loc), since: at(2026, 12, 1, 0, 0, loc), bar: true, loc: loc}).evaluate()
		if st.fraction < 0 || st.fraction > 1 {
			t.Errorf("fraction %v outside 0..1", st.fraction)
		}
	}
}

func TestValue(t *testing.T) {
	loc := berlin(t)
	a := &args{loc: loc}
	cases := []struct {
		st   state
		lang string
		want string
	}{
		{state{days: 34}, "de", "34 Tage"},
		{state{days: 1}, "de", "1 Tag"},
		{state{days: 1}, "en", "1 day"},
		{state{passed: true}, "de", "vorbei"},
		{state{passed: true}, "en", "passed"},
		{state{today: true}, "de", "heute"},
		{state{today: true}, "en", "today"},
		{state{timed: true, remain: 5*time.Hour + 20*time.Minute}, "de", "5 Std 20 Min"},
		{state{timed: true, remain: 5*time.Hour + 20*time.Minute}, "en", "5 h 20 min"},
		{state{timed: true, remain: 5 * time.Hour}, "de", "5 Std"},
		{state{timed: true, remain: 20 * time.Minute}, "de", "20 Min"},
		{state{timed: true, remain: 10 * time.Second}, "de", "1 Min"},
	}
	for _, c := range cases {
		req := &plugin.Request{Language: c.lang}
		// value reads the request language through req.T.
		if got := a.value(req, c.st); got != c.want {
			t.Errorf("%+v %s = %q, want %q", c.st, c.lang, got, c.want)
		}
	}
}

func TestText(t *testing.T) {
	loc := berlin(t)
	req, w := request(t, at(2026, 11, 20, 12, 0, loc), map[string]any{"label": "Weihnachten", "date": "12-24"})
	w.Width, w.Height = 20, 2
	lines, err := countdown{}.Text(req, w)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0] != "Weihnachten  34 Tage" || !strings.HasPrefix(lines[1], "24.12.") {
		t.Fatalf("lines %q", lines)
	}
	for _, l := range lines {
		if len([]rune(l)) > 20 {
			t.Errorf("line %q is wider than 20 columns", l)
		}
	}

	// With a reference the bar takes the second line.
	req, w = request(t, at(2026, 11, 20, 12, 0, loc), map[string]any{"label": "Weihnachten", "date": "12-24", "since": "01-01"})
	w.Width, w.Height = 20, 3
	lines, _ = countdown{}.Text(req, w)
	if len(lines) != 3 || !strings.HasPrefix(lines[1], "[") || !strings.HasSuffix(lines[1], "]") {
		t.Fatalf("lines %q", lines)
	}

	// Without a label the value is centered.
	req, w = request(t, at(2026, 11, 20, 12, 0, loc), map[string]any{"date": "12-24"})
	w.Width, w.Height = 20, 1
	lines, _ = countdown{}.Text(req, w)
	if len(lines) != 1 || lines[0] != "      34 Tage" {
		t.Errorf("centered line %q", lines[0])
	}

	// Passed and short values stay inside the width.
	req, w = request(t, at(2026, 12, 25, 0, 1, loc), map[string]any{"date": "2026-12-24"})
	w.Width, w.Height = 8, 1
	lines, _ = countdown{}.Text(req, w)
	if len(lines) != 1 || strings.TrimSpace(lines[0]) != "vorbei" {
		t.Errorf("passed line %q", lines[0])
	}
	req, w = request(t, at(2026, 11, 20, 12, 0, loc), map[string]any{"date": "12-24"})
	w.Width, w.Height = 6, 1
	lines, _ = countdown{}.Text(req, w)
	if len(lines) != 1 || strings.TrimSpace(lines[0]) != "34 d" {
		t.Errorf("short line %q", lines[0])
	}
}

func TestBarLine(t *testing.T) {
	cases := map[float64]string{0: "[----]", 0.5: "[##--]", 1: "[####]"}
	for f, want := range cases {
		if got := barLine(f, 6); got != want {
			t.Errorf("barLine(%v) = %q, want %q", f, got, want)
		}
	}
	if barLine(0.5, 3) != "" {
		t.Error("a bar narrower than four columns is drawn")
	}
}

func TestRenderViaProtocol(t *testing.T) {
	loc := berlin(t)
	now := at(2026, 11, 20, 12, 0, loc)
	req, w := request(t, now, map[string]any{"label": "Weihnachten", "date": "12-24", "since": "01-01"})
	req.Action, req.Levels = plugin.ActionRender, 4
	second := w
	second.Width, second.Args = 800, map[string]any{"label": "Urlaub", "date": "2026-12-24T18:00", "since": "2026-12-01"}
	req.Widgets = []plugin.WidgetRequest{w, second}
	m := plugin.Handle(countdown{}, req)
	if m.Error != "" || len(m.Widgets) != 2 {
		t.Fatalf("%+v", m)
	}
	for i, res := range m.Widgets {
		if res.Error != "" || len(res.PNG) == 0 {
			t.Fatalf("widget %d: %+v", i, res)
		}
		img, err := png.Decode(bytes.NewReader(res.PNG))
		if err != nil {
			t.Fatal(err)
		}
		ink := 0
		for y := 0; y < img.Bounds().Dy(); y++ {
			for x := 0; x < img.Bounds().Dx(); x++ {
				if r, g, b, _ := img.At(x, y).RGBA(); r+g+b < 3*0x8000 {
					ink++
				}
			}
		}
		if ink == 0 {
			t.Errorf("widget %d drew nothing", i)
		}
	}
	// The day count changes at local midnight.
	if want := int(at(2026, 11, 21, 0, 0, loc).Sub(now) / time.Second); m.MaxAge != want {
		t.Errorf("max_age %d, want %d", m.MaxAge, want)
	}

	// A countdown in its last day counts minutes and refreshes sooner.
	req, w = request(t, now, map[string]any{"date": "2026-11-20T18:00"})
	req.Action, req.Levels = plugin.ActionRender, 2
	req.Widgets = []plugin.WidgetRequest{w}
	if m := plugin.Handle(countdown{}, req); m.MaxAge != 30 {
		t.Errorf("max_age %d, want 30", m.MaxAge)
	}
}

func TestRenderFails(t *testing.T) {
	loc := berlin(t)
	req, w := request(t, at(2026, 11, 20, 12, 0, loc), map[string]any{})
	req.Action, req.Levels = plugin.ActionRender, 2
	req.Widgets = []plugin.WidgetRequest{w}
	m := plugin.Handle(countdown{}, req)
	if len(m.Widgets) != 1 || !strings.Contains(m.Widgets[0].Error, "date is required") {
		t.Fatalf("%+v", m)
	}
	req.Widgets[0].Args = map[string]any{"date": "24.12.2026"}
	if m := plugin.Handle(countdown{}, req); len(m.Widgets) != 1 || !strings.Contains(m.Widgets[0].Error, "date:") {
		t.Fatalf("%+v", m)
	}
	req.Widgets[0].Args = map[string]any{"date": "2026-12-24", "since": "2026-12-31"}
	if m := plugin.Handle(countdown{}, req); len(m.Widgets) != 1 || !strings.Contains(m.Widgets[0].Error, "since must be before date") {
		t.Fatalf("%+v", m)
	}
	if _, err := (countdown{}).Text(req, plugin.WidgetRequest{Widget: "nope", Width: 20, Height: 2}); err == nil {
		t.Error("unknown widget accepted")
	}
}

func TestRenderSizes(t *testing.T) {
	loc := berlin(t)
	sizes := [][2]int{{800, 60}, {400, 120}, {400, 240}, {120, 40}, {20, 20}}
	for _, levels := range []int{2, 4} {
		for _, size := range sizes {
			req, w := request(t, at(2026, 11, 20, 12, 0, loc), map[string]any{"label": "Weihnachten", "date": "12-24", "since": "01-01"})
			req.Levels, w.Width, w.Height = levels, size[0], size[1]
			if err := (countdown{}).Render(req, w, render.NewCanvas(size[0], size[1], levels)); err != nil {
				t.Fatalf("%v levels %d: %v", size, levels, err)
			}
		}
	}
}

// A cell without room is reported, so Paperboy draws its notice instead of an
// empty cell.
func TestTooSmallCell(t *testing.T) {
	req, w := request(t, at(2026, 11, 20, 12, 0, berlin(t)), map[string]any{"date": "2026-12-24"})
	req.Levels, w.Width, w.Height = 2, 10, 10
	if err := (countdown{}).Render(req, w, render.NewCanvas(10, 10, 2)); !errors.Is(err, plugin.ErrTooSmall) {
		t.Errorf("countdown in a 10x10 cell: error = %v, want ErrTooSmall", err)
	}
}
