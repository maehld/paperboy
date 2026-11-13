// Command dexcom is a Paperboy plugin (ADR 0049) that shows the glucose
// readings of a Dexcom G7 sensor. It reads them from the Dexcom Share service,
// the same cloud the G7 app uploads to and the path xDrip+, Nightscout and
// pydexcom use; the sensor itself speaks a proprietary protocol and is not
// read directly.
//
// The service keeps one day of values, so the widgets cover a day and an hour.
//
//	plugins:
//	  dexcom:
//	    config:
//	      account_name: ${DEXCOM_ACCOUNT}
//	      password: ${DEXCOM_PASSWORD}
//	      app_id: 00000000-0000-0000-0000-000000000000
//	      region: eu
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

const (
	// dayWindow is how far back the service answers; it keeps one day.
	dayWindow = 1440
	// maxReadings is one reading every five minutes for a day.
	maxReadings = 288
	// requestTimeout bounds one HTTP call.
	requestTimeout = 30 * time.Second
	// defaultRefresh is the poll interval; readings arrive every five minutes.
	defaultRefresh = 5 * time.Minute
	// minRefresh keeps the plugin from asking faster than the sensor reports.
	minRefresh = time.Minute
	// staleAfter is the age at which the reading no longer counts as current.
	staleAfter = 15 * time.Minute
	// stateVersion guards the stored state; a different version is ignored.
	stateVersion = 1

	unitMGDL = "mg/dL"
	unitMMOL = "mmol/L"
)

func main() {
	plugin.Serve(&dexcom{})
}

var configKeys = map[string]plugin.Arg{
	"account_name": {Type: "string", Description: "Dexcom account name (e-mail address); exchanged for the account ID once"},
	"account_id":   {Type: "string", Description: "account ID (a GUID), if you have it instead of the account name"},
	"password":     {Type: "string", Required: true, Description: "account password, preferably via ${VAR} from the environment"},
	"app_id": {Type: "string", Required: true,
		Description: "application ID (a GUID) of the app that created the share"},
	"region": {Type: "string", Required: true, Enum: []any{"eu", "us"},
		Description: "host of the account: eu or us; the account may live on another host than its country"},
	"refresh": {Type: "duration", Default: "5m", Description: "how often the readings are read, at least 1m"},
	"unit": {Type: "string", Default: "auto", Enum: []any{"auto", "mg/dL", "mmol/L"},
		Description: "unit of the thresholds and the widgets; auto takes what the account reports"},
	"urgent": {Type: "float", Description: "urgent low threshold; 0 disables the state; default " + "55 mg/dL or 3.1 mmol/L"},
	"low":    {Type: "float", Description: "low threshold, the lower edge of the target band; default 80 mg/dL or 4.4 mmol/L"},
	"high":   {Type: "float", Description: "high threshold, the upper edge of the target band; default 280 mg/dL or 15.6 mmol/L"},
	"min":    {Type: "float", Description: "bottom of the chart scale; default 40 mg/dL or 2.2 mmol/L"},
	"max":    {Type: "float", Description: "top of the chart scale; default 300 mg/dL or 15.6 mmol/L"},
}

// dexcom holds the configuration and what the service answered last.
type dexcom struct {
	mu  sync.Mutex
	api *shareClient

	// configuration, set once by Configure
	accountName string
	accountID   string
	password    string
	region      string
	refresh     time.Duration
	unit        string // mg/dL, mmol/L, or empty for detection
	set         map[string]float64
	lang        string

	// state
	session  string
	resolved string // account ID the service answered for account_name
	fetched  time.Time
	readings []reading
	loadErr  error
	ready    chan struct{}
}

var (
	_ plugin.Handler     = (*dexcom)(nil)
	_ plugin.Texter      = (*dexcom)(nil)
	_ plugin.Starter     = (*dexcom)(nil)
	_ plugin.Ager        = (*dexcom)(nil)
	_ plugin.Stater      = (*dexcom)(nil)
	_ plugin.Conditioner = (*dexcom)(nil)
)

func (d *dexcom) Describe() plugin.Description {
	both := []string{plugin.KindImage, plugin.KindText}
	return plugin.Description{
		Title:   "Dexcom",
		Version: version(),
		Config:  configKeys,
		Conditions: []plugin.ConditionSpec{
			{Name: "urgent", Title: "Urgent low", Description: "the reading is below the urgent threshold; inactive when that threshold is 0"},
			{Name: "low", Title: "Low", Description: "the reading is below the low threshold, including the urgent range"},
			{Name: "high", Title: "High", Description: "the reading is above the high threshold"},
		},
		Widgets: []plugin.WidgetInfo{
			{
				Widget: "glucose", Title: "Glucose reading", Kinds: both,
				Sizes:    []plugin.Span{{Columns: 2, Rows: 2}, {Columns: 3, Rows: 2}},
				MinWidth: 120, MinHeight: 80,
				Args: map[string]plugin.Arg{},
			},
			{
				Widget: "graph", Title: "Glucose history", Kinds: []string{plugin.KindImage},
				Sizes:    []plugin.Span{{Columns: 3, Rows: 4}, {Columns: 6, Rows: 4}, {Columns: 6, Rows: 3}},
				MinWidth: 200, MinHeight: 120,
				Args: map[string]plugin.Arg{
					"period": {Type: "string", Default: "day", Enum: []any{"day", "hour"},
						EnumLabels:  map[string]string{"day": "Today", "hour": "Last hour"},
						Description: "which part of the history the chart shows"},
					"header": {Type: "bool", Default: true,
						Description: "show the title and the unit above the chart"},
				},
			},
			{
				Widget: "values", Title: "Glucose values", Kinds: both,
				Sizes:    []plugin.Span{{Columns: 2, Rows: 2}, {Columns: 3, Rows: 2}},
				MinWidth: 120, MinHeight: 60,
				Args: map[string]plugin.Arg{
					"values": {Type: "list", Enum: valueKeys(),
						Description: "the values to show, one row each"},
					"columns": {Type: "int", Default: 1,
						Description: "how many columns of rows, 1 to 2"},
				},
			},
		},
	}
}

func (d *dexcom) Configure(cfg plugin.Values, req *plugin.Request) error {
	if err := cfg.Check(configKeys); err != nil {
		return err
	}
	name := strings.TrimSpace(cfg.String("account_name", ""))
	id := strings.TrimSpace(cfg.String("account_id", ""))
	switch {
	case name == "" && id == "":
		return errors.New("account_name: required unless account_id is given")
	case name != "" && id != "":
		return errors.New("account_name and account_id: give only one of them")
	}
	if id != "" && !looksLikeGUID(id) {
		return errors.New("account_id: expected a GUID")
	}
	password := cfg.String("password", "")
	if password == "" {
		return errors.New("password: empty")
	}
	appID := strings.TrimSpace(cfg.String("app_id", ""))
	if !looksLikeGUID(appID) {
		return errors.New("app_id: expected the GUID of the app that created the share")
	}
	region := strings.ToLower(strings.TrimSpace(cfg.String("region", "")))
	base, ok := shareRegions[region]
	if !ok {
		return fmt.Errorf("region: expected eu or us, got %q", region)
	}
	refresh, err := cfg.Duration("refresh", defaultRefresh)
	if err != nil {
		return err
	}
	if refresh < minRefresh {
		return fmt.Errorf("refresh: %s is below the minimum of %s", refresh, minRefresh)
	}
	unit := strings.TrimSpace(cfg.String("unit", "auto"))
	switch unit {
	case "auto", "":
		unit = ""
	case unitMGDL, unitMMOL:
	default:
		return fmt.Errorf("unit: expected auto, %s or %s, got %q", unitMGDL, unitMMOL, unit)
	}

	set := map[string]float64{}
	for _, key := range []string{"urgent", "low", "high", "min", "max"} {
		if _, ok := cfg[key]; !ok {
			continue
		}
		v := cfg.Float(key, 0)
		if v < 0 {
			return fmt.Errorf("%s: %g is negative", key, v)
		}
		set[key] = v
	}
	// The order holds in both units, so it can be checked here.
	full := defaults(unitMGDL)
	for k, v := range set {
		switch k {
		case "urgent":
			full.urgent = v
		case "low":
			full.low = v
		case "high":
			full.high = v
		case "min":
			full.min = v
		case "max":
			full.max = v
		}
	}
	if err := full.check(); err != nil {
		return err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.api = &shareClient{base: base, appID: appID, http: &http.Client{Timeout: requestTimeout}}
	d.accountName, d.accountID = name, id
	d.password, d.region, d.refresh = password, region, refresh
	d.unit, d.set, d.lang = unit, set, req.Lang()
	return nil
}

func (d *dexcom) MaxAge(*plugin.Request) time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.refresh
}

// Conditions reports the bands a plan can depend on (ADR 0103). They are all
// inactive while the newest reading is older than staleAfter, so a stale value
// never switches a page; a failed load is an error, which Paperboy also treats
// as inactive.
func (d *dexcom) Conditions(req *plugin.Request) (map[string]bool, error) {
	l, err := d.limits()
	if err != nil {
		return nil, err
	}
	rs, loadErr, _ := d.snapshot()
	if loadErr != nil {
		return nil, loadErr
	}
	active := map[string]bool{"urgent": false, "low": false, "high": false}
	last, ok := current(rs, req.Now)
	if !ok || req.Now.Sub(last.at) > staleAfter {
		return active, nil
	}
	switch level(last.value, l) {
	case "urgent":
		// The urgent range is below the low threshold as well.
		active["urgent"], active["low"] = true, true
	case "low":
		active["low"] = true
	case "high":
		active["high"] = true
	}
	return active, nil
}

// Start polls the service in the background.
func (d *dexcom) Start(ctx context.Context, notify func(widgets ...string)) {
	d.mu.Lock()
	d.ready = make(chan struct{})
	// A restart within the interval costs no request.
	wait := time.Duration(0)
	if !d.fetched.IsZero() {
		if rest := d.refresh - time.Since(d.fetched); rest > 0 {
			wait = rest
		}
	}
	last := d.fingerprint()
	d.mu.Unlock()

	go func() {
		first := true
		for {
			if wait > 0 && !d.wait(ctx, wait) {
				return
			}
			wait = d.refresh
			err := d.update(ctx)
			d.mu.Lock()
			if err != nil {
				if d.loadErr == nil || err.Error() != d.loadErr.Error() {
					fmt.Fprintln(os.Stderr, "dexcom:", err)
				}
				d.loadErr = err
			} else {
				d.loadErr = nil
			}
			now := d.fingerprint()
			d.mu.Unlock()
			if err == nil && now != last {
				last = now
				if !first {
					notify()
				}
			}
			if first {
				close(d.ready)
				first = false
			}
		}
	}()
}

// wait sleeps for d and reports whether the context is still alive.
func (d *dexcom) wait(ctx context.Context, dur time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(dur):
		return true
	}
}

// update reads the readings, logging in first when there is no session.
func (d *dexcom) update(ctx context.Context) error {
	for attempt := 0; ; attempt++ {
		session, err := d.sessionID()
		if err != nil {
			return err
		}
		minutes, count := d.window()
		fresh, err := d.api.readings(session, minutes, count)
		if err != nil {
			if isSessionGone(err) && attempt == 0 {
				// The service forgot the session; one new login.
				d.mu.Lock()
				d.session = ""
				d.mu.Unlock()
				continue
			}
			return err
		}
		now := time.Now()
		d.mu.Lock()
		d.readings = merge(d.readings, fresh, now)
		d.fetched, d.loadErr = now, nil
		d.mu.Unlock()
		return nil
	}
}

// window says what the next request has to cover: without stored readings the
// whole day, otherwise only the ones missed since the last fetch plus one
// reading of context, so a poll moves a handful of values instead of a day.
func (d *dexcom) window() (minutes, count int) {
	d.mu.Lock()
	var newest time.Time
	if n := len(d.readings); n > 0 {
		newest = d.readings[n-1].at
	}
	d.mu.Unlock()
	if newest.IsZero() {
		return dayWindow, maxReadings
	}
	gap := time.Since(newest)
	if gap < 0 {
		gap = 0
	}
	count = int(gap/(5*time.Minute)) + 2
	count = min(count, maxReadings)
	minutes = int(gap/time.Minute) + 30
	minutes = min(max(minutes, 30), dayWindow)
	return minutes, count
}

// merge adds the fresh readings to the stored ones, drops duplicates and
// everything older than a day.
func merge(old, fresh []reading, now time.Time) []reading {
	seen := make(map[int64]bool, len(old)+len(fresh))
	out := make([]reading, 0, len(old)+len(fresh))
	for _, list := range [][]reading{old, fresh} {
		for _, r := range list {
			// The service repeats a reading with the same stamp; a minute is
			// the finest step of the sensor.
			key := r.at.Unix() / 60
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	cut := now.Add(-24 * time.Hour)
	i := 0
	for i < len(out) && out[i].at.Before(cut) {
		i++
	}
	return out[i:]
}

// sessionID returns a session ID, logging in when there is none.
func (d *dexcom) sessionID() (string, error) {
	d.mu.Lock()
	session, resolved := d.session, d.resolved
	name, id, password := d.accountName, d.accountID, d.password
	d.mu.Unlock()
	if session != "" {
		return session, nil
	}
	accountID := id
	if accountID == "" {
		accountID = resolved
	}
	if accountID == "" {
		// Exchange the account name for the account ID once and keep it.
		got, err := d.api.authenticate(name, password)
		if err != nil {
			return "", fmt.Errorf("account: %w", err)
		}
		accountID = got
	}
	got, err := d.api.login(accountID, password)
	if err != nil {
		return "", fmt.Errorf("login: %w", err)
	}
	d.mu.Lock()
	d.session, d.resolved = got, accountID
	d.mu.Unlock()
	return got, nil
}

// fingerprint covers what the widgets show of the newest reading, so a value
// that did not change does not redraw the panels.
func (d *dexcom) fingerprint() string {
	if len(d.readings) == 0 {
		return ""
	}
	last := d.readings[len(d.readings)-1]
	return fmt.Sprintf("%d %s %d", int(last.value*10), last.trend, len(d.readings))
}

// ---- state (ADR 0081) ----

type storedState struct {
	Version   int             `json:"version"`
	Region    string          `json:"region"`
	Account   string          `json:"account,omitempty"`
	AccountID string          `json:"account_id,omitempty"`
	Session   string          `json:"session,omitempty"`
	SessionAt time.Time       `json:"session_at,omitempty"`
	FetchedAt time.Time       `json:"fetched_at,omitempty"`
	Readings  []storedReading `json:"readings,omitempty"`
}

// LoadState takes over the session, the account ID and the last day of
// readings, so a restart costs no request and the first page has data. A state
// of another account or region is ignored.
func (d *dexcom) LoadState(state json.RawMessage, blobs map[string][]byte) error {
	if len(state) == 0 {
		return nil
	}
	var s storedState
	if err := json.Unmarshal(state, &s); err != nil {
		fmt.Fprintln(os.Stderr, "dexcom: ignoring the stored state:", err)
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	account := d.accountName
	if account == "" {
		account = d.accountID
	}
	switch {
	case s.Version != stateVersion:
		fmt.Fprintf(os.Stderr, "dexcom: ignoring a stored state of version %d\n", s.Version)
		return nil
	case s.Region != d.region:
		fmt.Fprintln(os.Stderr, "dexcom: ignoring the stored state of another region")
		return nil
	case s.Account != account:
		fmt.Fprintln(os.Stderr, "dexcom: ignoring the stored state of another account")
		return nil
	}
	d.resolved, d.session, d.fetched = s.AccountID, s.Session, s.FetchedAt
	d.readings = fromStored(s.Readings)
	return nil
}

// State returns the session, the account ID and the readings of the last day.
func (d *dexcom) State() json.RawMessage {
	d.mu.Lock()
	defer d.mu.Unlock()
	account := d.accountName
	if account == "" {
		account = d.accountID
	}
	s := storedState{
		Version:   stateVersion,
		Region:    d.region,
		Account:   account,
		AccountID: d.resolved,
		Session:   d.session,
		FetchedAt: d.fetched,
		Readings:  toStored(d.readings),
	}
	out, err := json.Marshal(s)
	if err != nil {
		return nil
	}
	return out
}

func toStored(rs []reading) []storedReading {
	cut := time.Now().Add(-24 * time.Hour)
	out := make([]storedReading, 0, len(rs))
	for _, r := range rs {
		if r.at.Before(cut) {
			continue
		}
		stamp := "/Date(" + strconv.FormatInt(r.at.UnixMilli(), 10) + ")/"
		out = append(out, storedReading{WT: stamp, ST: stamp, Value: r.value, Trend: r.trend})
	}
	return out
}

func fromStored(list []storedReading) []reading {
	out := make([]reading, 0, len(list))
	for _, r := range list {
		at, ok := parseDexDate(r.WT)
		if !ok {
			continue
		}
		out = append(out, reading{at: at, value: r.Value, trend: r.Trend})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	return out
}
