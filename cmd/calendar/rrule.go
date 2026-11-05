package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// rrule is the supported subset of an iCalendar recurrence rule (RFC 5545
// 3.3.10): FREQ DAILY to YEARLY with INTERVAL, COUNT, UNTIL, BYDAY (with
// ordinals in monthly and yearly rules), BYMONTHDAY, BYMONTH, BYSETPOS and
// WKST. That covers what calendar apps create; other parts are an error, so
// such an event is reported and skipped instead of being shown wrongly.
type rrule struct {
	Freq       string
	Interval   int
	Count      int
	Until      *dateValue
	ByDay      []weekdayNum
	ByMonthDay []int
	ByMonth    []int
	BySetPos   []int
	WeekStart  time.Weekday
}

type weekdayNum struct {
	N   int // 0 = every such weekday in the period, otherwise 1-based, negative from the end
	Day time.Weekday
}

var weekdayCodes = map[string]time.Weekday{
	"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday,
	"TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday,
}

// maxPeriods bounds the expansion of one rule, so a daily rule from decades ago
// or a rule that never matches cannot loop for long.
const maxPeriods = 100000

func parseRRule(value string, z *zones, startLoc *time.Location) (rrule, error) {
	r := rrule{Interval: 1, WeekStart: time.Monday}
	for _, part := range strings.Split(value, ";") {
		if part == "" {
			continue
		}
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			return r, fmt.Errorf("invalid rule part %q", part)
		}
		key = strings.ToUpper(key)
		switch key {
		case "FREQ":
			r.Freq = strings.ToUpper(val)
		case "INTERVAL":
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 {
				return r, fmt.Errorf("invalid INTERVAL %q", val)
			}
			r.Interval = n
		case "COUNT":
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 {
				return r, fmt.Errorf("invalid COUNT %q", val)
			}
			r.Count = n
		case "UNTIL":
			params := map[string]string{}
			if len(val) == 8 {
				params["VALUE"] = "DATE"
			}
			d, err := z.parseDate(val, params)
			if err != nil {
				return r, fmt.Errorf("UNTIL: %w", err)
			}
			if !d.IsDate && !strings.HasSuffix(val, "Z") {
				d.Time = time.Date(d.Time.Year(), d.Time.Month(), d.Time.Day(),
					d.Time.Hour(), d.Time.Minute(), d.Time.Second(), 0, startLoc)
			}
			r.Until = &d
		case "BYDAY":
			for _, item := range strings.Split(val, ",") {
				item = strings.ToUpper(strings.TrimSpace(item))
				if len(item) < 2 {
					return r, fmt.Errorf("invalid BYDAY %q", val)
				}
				day, ok := weekdayCodes[item[len(item)-2:]]
				if !ok {
					return r, fmt.Errorf("invalid BYDAY %q", val)
				}
				n := 0
				if prefix := item[:len(item)-2]; prefix != "" {
					var err error
					if n, err = strconv.Atoi(prefix); err != nil || n == 0 || n < -53 || n > 53 {
						return r, fmt.Errorf("invalid BYDAY %q", val)
					}
				}
				r.ByDay = append(r.ByDay, weekdayNum{N: n, Day: day})
			}
		case "BYMONTHDAY":
			list, err := intList(val, -31, 31)
			if err != nil {
				return r, fmt.Errorf("BYMONTHDAY: %w", err)
			}
			r.ByMonthDay = list
		case "BYMONTH":
			list, err := intList(val, 1, 12)
			if err != nil {
				return r, fmt.Errorf("BYMONTH: %w", err)
			}
			r.ByMonth = list
		case "BYSETPOS":
			list, err := intList(val, -366, 366)
			if err != nil {
				return r, fmt.Errorf("BYSETPOS: %w", err)
			}
			r.BySetPos = list
		case "WKST":
			day, ok := weekdayCodes[strings.ToUpper(val)]
			if !ok {
				return r, fmt.Errorf("invalid WKST %q", val)
			}
			r.WeekStart = day
		default:
			return r, fmt.Errorf("unsupported rule part %s", key)
		}
	}
	switch r.Freq {
	case "DAILY", "WEEKLY", "MONTHLY", "YEARLY":
	case "":
		return r, fmt.Errorf("FREQ missing")
	default:
		return r, fmt.Errorf("unsupported FREQ %s", r.Freq)
	}
	if r.Count > 0 && r.Until != nil {
		return r, fmt.Errorf("COUNT and UNTIL are exclusive")
	}
	return r, nil
}

func intList(s string, lo, hi int) ([]int, error) {
	var out []int
	for _, item := range strings.Split(s, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(item))
		if err != nil || n == 0 || n < lo || n > hi {
			return nil, fmt.Errorf("invalid value %q", item)
		}
		out = append(out, n)
	}
	return out, nil
}

// expand calls emit for every start of the series in order, beginning with
// DTSTART, until a start lies after end, the rule ends or emit returns false.
func (r rrule) expand(start time.Time, end time.Time, emit func(time.Time) bool) {
	loc := start.Location()
	count := 0
	next := func(t time.Time) bool {
		if r.Until != nil {
			if r.Until.IsDate {
				if dateKey(t.In(loc)) > dateKey(r.Until.Time) {
					return false
				}
			} else if t.After(r.Until.Time) {
				return false
			}
		}
		if t.After(end) {
			return false
		}
		count++
		if !emit(t) {
			return false
		}
		return r.Count == 0 || count < r.Count
	}
	if !next(start) {
		return
	}
	at := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, start.Hour(), start.Minute(), start.Second(), 0, loc)
	}
	for i := 0; i < maxPeriods; i++ {
		days := r.period(start, i)
		if len(days) == 0 {
			if r.periodStart(start, i).After(end) {
				return
			}
			continue
		}
		for _, d := range days {
			t := at(d.Year(), d.Month(), d.Day())
			if !t.After(start) {
				continue
			}
			if !next(t) {
				return
			}
		}
	}
}

// periodStart is the first day of period i, used to stop rules that produce
// empty periods.
func (r rrule) periodStart(start time.Time, i int) time.Time {
	y, m, d := start.Date()
	loc := start.Location()
	n := i * r.Interval
	switch r.Freq {
	case "DAILY":
		return time.Date(y, m, d+n, 0, 0, 0, 0, loc)
	case "WEEKLY":
		return time.Date(y, m, d+7*n, 0, 0, 0, 0, loc)
	case "MONTHLY":
		return time.Date(y, m+time.Month(n), 1, 0, 0, 0, 0, loc)
	default:
		return time.Date(y+n, 1, 1, 0, 0, 0, 0, loc)
	}
}

// period returns the sorted candidate days of period i (date only, at noon UTC
// to keep the arithmetic free of DST effects).
func (r rrule) period(start time.Time, i int) []time.Time {
	sy, sm, sd := start.Date()
	n := i * r.Interval
	var days []time.Time
	switch r.Freq {
	case "DAILY":
		d := civil(sy, sm, sd+n)
		if r.monthOK(d.Month()) && r.monthDayOK(d) && r.weekdayOK(d.Weekday()) {
			days = append(days, d)
		}
	case "WEEKLY":
		first := civil(sy, sm, sd)
		offset := (int(first.Weekday()) - int(r.WeekStart) + 7) % 7
		weekStart := first.AddDate(0, 0, -offset+7*n)
		wanted := r.ByDay
		if len(wanted) == 0 {
			wanted = []weekdayNum{{Day: start.Weekday()}}
		}
		for k := 0; k < 7; k++ {
			d := weekStart.AddDate(0, 0, k)
			if !r.monthOK(d.Month()) {
				continue
			}
			for _, w := range wanted {
				if w.Day == d.Weekday() {
					days = append(days, d)
					break
				}
			}
		}
	case "MONTHLY":
		m := civil(sy, sm+time.Month(n), 1)
		if r.monthOK(m.Month()) {
			days = r.inMonth(m.Year(), m.Month(), sd)
		}
	case "YEARLY":
		y := sy + n
		switch {
		case len(r.ByMonth) > 0:
			for _, month := range sortedInts(r.ByMonth) {
				days = append(days, r.inMonth(y, time.Month(month), sd)...)
			}
		case len(r.ByDay) > 0:
			all := spanDays(civil(y, 1, 1), civil(y+1, 1, 1))
			days = byWeekday(all, r.ByDay)
			if len(r.ByMonthDay) > 0 {
				days = filter(days, r.monthDayOK)
			}
		case len(r.ByMonthDay) > 0:
			days = r.inMonth(y, sm, sd)
		default:
			if d := civil(y, sm, sd); d.Day() == sd {
				days = append(days, d)
			}
		}
	}
	sort.Slice(days, func(a, b int) bool { return days[a].Before(days[b]) })
	return r.setPos(days)
}

// inMonth expands BYMONTHDAY and BYDAY within one month; without either the
// day of DTSTART counts and months without that day are skipped.
func (r rrule) inMonth(y int, m time.Month, startDay int) []time.Time {
	first := civil(y, m, 1)
	next := civil(y, m+1, 1)
	all := spanDays(first, next)
	switch {
	case len(r.ByDay) > 0:
		days := byWeekday(all, r.ByDay)
		if len(r.ByMonthDay) > 0 {
			days = filter(days, r.monthDayOK)
		}
		return days
	case len(r.ByMonthDay) > 0:
		return filter(all, r.monthDayOK)
	default:
		if startDay <= len(all) {
			return []time.Time{all[startDay-1]}
		}
		return nil
	}
}

func (r rrule) setPos(days []time.Time) []time.Time {
	if len(r.BySetPos) == 0 || len(days) == 0 {
		return days
	}
	var out []time.Time
	for _, p := range r.BySetPos {
		idx := p - 1
		if p < 0 {
			idx = len(days) + p
		}
		if idx >= 0 && idx < len(days) {
			out = append(out, days[idx])
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Before(out[b]) })
	return out
}

func (r rrule) monthOK(m time.Month) bool {
	if len(r.ByMonth) == 0 {
		return true
	}
	for _, v := range r.ByMonth {
		if time.Month(v) == m {
			return true
		}
	}
	return false
}

func (r rrule) monthDayOK(d time.Time) bool {
	if len(r.ByMonthDay) == 0 {
		return true
	}
	length := civil(d.Year(), d.Month()+1, 0).Day()
	for _, v := range r.ByMonthDay {
		if v == d.Day() || (v < 0 && length+v+1 == d.Day()) {
			return true
		}
	}
	return false
}

func (r rrule) weekdayOK(w time.Weekday) bool {
	if len(r.ByDay) == 0 {
		return true
	}
	for _, v := range r.ByDay {
		if v.Day == w {
			return true
		}
	}
	return false
}

// byWeekday selects the days of a period that match BYDAY, honouring ordinals
// relative to the period ("2MO" = second Monday, "-1FR" = last Friday).
func byWeekday(all []time.Time, wanted []weekdayNum) []time.Time {
	var out []time.Time
	for _, w := range wanted {
		var matching []time.Time
		for _, d := range all {
			if d.Weekday() == w.Day {
				matching = append(matching, d)
			}
		}
		switch {
		case w.N == 0:
			out = append(out, matching...)
		case w.N > 0 && w.N <= len(matching):
			out = append(out, matching[w.N-1])
		case w.N < 0 && -w.N <= len(matching):
			out = append(out, matching[len(matching)+w.N])
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Before(out[b]) })
	return dedupe(out)
}

func filter(days []time.Time, keep func(time.Time) bool) []time.Time {
	var out []time.Time
	for _, d := range days {
		if keep(d) {
			out = append(out, d)
		}
	}
	return out
}

func dedupe(days []time.Time) []time.Time {
	var out []time.Time
	for i, d := range days {
		if i == 0 || !d.Equal(days[i-1]) {
			out = append(out, d)
		}
	}
	return out
}

func spanDays(from, to time.Time) []time.Time {
	var out []time.Time
	for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
		out = append(out, d)
	}
	return out
}

func sortedInts(in []int) []int {
	out := append([]int(nil), in...)
	sort.Ints(out)
	return out
}

// civil returns a normalized calendar date.
func civil(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 12, 0, 0, 0, time.UTC)
}

func dateKey(t time.Time) string {
	return t.Format("20060102")
}
