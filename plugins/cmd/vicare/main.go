// Command vicare is a Paperboy plugin (ADR 0049) for Viessmann heating
// systems. It reads the features of one device from the official ViCare cloud
// API (https://developer.viessmann-climatesolutions.com) in the background and
// draws:
//
//   - "heating": flow target, outside and room temperature, program, burner,
//   - "temperatures": outside, flow, return, boiler, DHW and room,
//   - "dhw": domestic hot water with its target and charging state,
//   - "burner": burner or compressor with hours, starts and modulation,
//   - "status": model, data age, programs and the first error,
//   - "overview": a compact overview for small cells,
//   - "curve": the heating curve from slope and shift with the current target.
//
// All widgets are for image displays; the values below give a text page the
// same data. The API is the ViCare cloud, so the heating system must be online
// through its Vitoconnect or a built-in gateway.
//
// The API asks for OAuth2 with PKCE: create a client in the developer portal,
// run the program once with -login, and put the printed refresh token,
// installation, gateway and device into paperboy.yaml. The plugin renews the
// access token itself and keeps both tokens in its state, never in the
// configuration.
//
//	plugins:
//	  vicare:
//	    config:
//	      client_id: ${VICARE_CLIENT_ID}
//	      refresh_token: ${VICARE_REFRESH_TOKEN}
//	      installation: "1234567"
//	      gateway: "7XXXXXXXXXX"
//	      device: "0"
//	      refresh: 10m
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/sdk/format"
	"github.com/maehld/paperboy/sdk/plugin"
)

// minRefresh is the shortest configured interval. The free API plan allows
// about 1450 calls a day; one gateway-wide features call per 10 minutes uses a
// tenth of it and leaves room for a later action.
const minRefresh = 5 * time.Minute

func main() {
	login := flag.Bool("login", false, "log in to the ViCare account and print the refresh token and the IDs")
	client := flag.String("client", "", "client ID from the Viessmann developer portal, for -login")
	flag.Parse()

	v := &vicare{
		client: &http.Client{Timeout: requestTimeout},
		base:   defaultBase,
		iam:    defaultIAM,
	}
	if *login {
		if err := v.login(*client); err != nil {
			fmt.Fprintln(os.Stderr, "vicare:", err)
			os.Exit(1)
		}
		return
	}
	plugin.Serve(v)
}

var configKeys = map[string]plugin.Arg{
	"client_id":     {Type: "string", Required: true, Description: "client ID of the personal API client in the Viessmann developer portal"},
	"refresh_token": {Type: "string", Required: true, Description: "refresh token printed by the plugin's -login"},
	"installation":  {Type: "string", Description: "installation ID; discovered from the account when empty"},
	"gateway":       {Type: "string", Description: "gateway serial; discovered when empty"},
	"device":        {Type: "string", Description: "device ID on the gateway, default the heating device"},
	"refresh":       {Type: "duration", Default: "10m", Description: "how often the device is read, at least 5m"},
	"base":          {Type: "string", Default: defaultBase, Description: "API base URL"},
}

// stateVersion is the format of the plugin state; one of another version is
// discarded instead of guessed (ADR 0081).
const stateVersion = 1

// storedState is what the plugin keeps between restarts (ADR 0081). It holds
// the rotated tokens: the refresh token in the configuration is only the seed,
// because Viessmann replaces it on every renewal.
type storedState struct {
	Version      int       `json:"version"`
	Key          string    `json:"key"`
	Installation string    `json:"installation,omitempty"`
	Gateway      string    `json:"gateway,omitempty"`
	Device       string    `json:"device,omitempty"`
	Model        string    `json:"model,omitempty"`
	Roles        []string  `json:"roles,omitempty"`
	Identified   bool      `json:"identified,omitempty"`
	Tokens       *tokens   `json:"tokens,omitempty"`
	FetchedAt    time.Time `json:"fetched_at,omitempty"`
	NextAttempt  time.Time `json:"next_attempt,omitempty"`
	Snapshot     *snapshot `json:"snapshot,omitempty"`
	// LastError is the message the widgets showed after the last attempt, so
	// a restart during a failed load keeps the notice.
	LastError string `json:"last_error,omitempty"`
}

// vicare is the plugin.
type vicare struct {
	plugin.Demand

	client *http.Client
	base   string
	iam    string

	clientID    string
	seedRefresh string
	refresh     time.Duration

	readyOnce sync.Once
	ready     chan struct{}

	mu sync.Mutex
	// pending is the state to send with the next answer, nil when unchanged.
	pending      *storedState
	installation string
	gateway      string
	device       string
	model        string
	roles        []string
	identified   bool
	tokens       *tokens
	snap         *snapshot
	fetched      time.Time
	next         time.Time
	loadErr      error

	cur  *snapshot
	req  *plugin.Request
	loc  *time.Location
	lang string
}

var (
	_ plugin.Eventer  = (*vicare)(nil)
	_ plugin.Starter  = (*vicare)(nil)
	_ plugin.Preparer = (*vicare)(nil)
	_ plugin.Ager     = (*vicare)(nil)
	_ plugin.Valuer   = (*vicare)(nil)
	_ plugin.Stater   = (*vicare)(nil)
)

func (v *vicare) Describe() plugin.Description {
	image := []string{plugin.KindImage}
	temperature := "°C"
	return plugin.Description{
		Title:   "ViCare",
		Version: version(),
		Config:  configKeys,
		Values: []plugin.ValueSpec{
			{Name: "outside", Title: "Outside temperature", Type: plugin.ValueNumber, Unit: temperature, Precision: 1},
			{Name: "room", Title: "Room temperature", Type: plugin.ValueNumber, Unit: temperature, Precision: 1},
			{Name: "flow", Title: "Flow temperature", Type: plugin.ValueNumber, Unit: temperature, Precision: 1},
			{Name: "flow_target", Title: "Flow target", Type: plugin.ValueNumber, Unit: temperature, Precision: 1},
			{Name: "dhw", Title: "DHW temperature", Type: plugin.ValueNumber, Unit: temperature, Precision: 1},
			{Name: "dhw_target", Title: "DHW target", Type: plugin.ValueNumber, Unit: temperature, Precision: 1},
			{Name: "burner", Title: "Burner", Type: plugin.ValueText},
			{Name: "program", Title: "Heating program", Type: plugin.ValueText},
			{Name: "state", Title: "State", Type: plugin.ValueEnum, Enum: []string{"ok", "error"}},
			{Name: "curve_slope", Title: "Heating curve slope", Type: plugin.ValueNumber, Precision: 1},
			{Name: "curve_shift", Title: "Heating curve shift", Type: plugin.ValueNumber, Precision: 0},
			{Name: "model", Title: "Device model", Type: plugin.ValueText},
			{Name: "updated", Title: "Data from", Type: plugin.ValueTimestamp},
		},
		Widgets: []plugin.WidgetInfo{
			{Widget: "heating", Title: "Heating", Kinds: image, MinWidth: 160, MinHeight: 80,
				Sizes: []plugin.Span{{Columns: 3, Rows: 2}, {Columns: 2, Rows: 2}, {Columns: 6, Rows: 2}},
				Args: map[string]plugin.Arg{
					"circuit": {Type: "int", Default: 0, Description: "heating circuit, 0 for the first"},
					"room":    {Type: "bool", Default: true, Description: "show the room temperature"},
				}},
			{Widget: "temperatures", Title: "Temperatures", Kinds: image, MinWidth: 160, MinHeight: 100,
				Sizes: []plugin.Span{{Columns: 2, Rows: 3}, {Columns: 3, Rows: 3}, {Columns: 6, Rows: 3}},
				Args: map[string]plugin.Arg{
					"circuit": {Type: "int", Default: 0, Description: "heating circuit, 0 for the first"},
					"max":     {Type: "int", Default: 6, Description: "rows to show, 2 to 8"},
				}},
			{Widget: "dhw", Title: "Domestic hot water", Kinds: image, MinWidth: 160, MinHeight: 80,
				Sizes: []plugin.Span{{Columns: 2, Rows: 2}, {Columns: 3, Rows: 2}, {Columns: 3, Rows: 3}},
				Args: map[string]plugin.Arg{
					"meter": {Type: "bool", Default: true, Description: "show the bar to the target temperature"},
				}},
			{Widget: "burner", Title: "Burner", Kinds: image, MinWidth: 160, MinHeight: 80,
				Sizes: []plugin.Span{{Columns: 2, Rows: 2}, {Columns: 3, Rows: 2}},
				Args: map[string]plugin.Arg{
					"stats": {Type: "bool", Default: true, Description: "show hours and starts"},
				}},
			{Widget: "status", Title: "Status", Kinds: image, MinWidth: 160, MinHeight: 80,
				Sizes: []plugin.Span{{Columns: 3, Rows: 2}, {Columns: 6, Rows: 2}},
				Args: map[string]plugin.Arg{
					"errors": {Type: "bool", Default: true, Description: "show the first error or notice"},
				}},
			{Widget: "overview", Title: "Overview", Kinds: image, MinWidth: 160, MinHeight: 80,
				Sizes: []plugin.Span{{Columns: 2, Rows: 2}, {Columns: 3, Rows: 2}, {Columns: 6, Rows: 2}},
				Args: map[string]plugin.Arg{
					"circuit": {Type: "int", Default: 0, Description: "heating circuit, 0 for the first"},
				}},
			{Widget: "curve", Title: "Heating curve", Kinds: image, MinWidth: 240, MinHeight: 120,
				Sizes: []plugin.Span{{Columns: 3, Rows: 2}, {Columns: 6, Rows: 3}, {Columns: 3, Rows: 3}},
				Args: map[string]plugin.Arg{
					"circuit":     {Type: "int", Default: 0, Description: "heating circuit, 0 for the first"},
					"outside_min": {Type: "int", Default: -20, Description: "left end of the outside axis, °C"},
					"outside_max": {Type: "int", Default: 20, Description: "right end of the outside axis, °C"},
					"formula": {Type: "string", Default: "auto", Enum: []any{"auto", "boiler", "heatpump"},
						Description: "shape of the curve; auto follows the device roles"},
				}},
		},
	}
}

func (v *vicare) Configure(cfg plugin.Values, req *plugin.Request) error {
	if req.Version != 0 && req.Version < 9 {
		return fmt.Errorf("vicare needs Paperboy protocol 9 or newer; this Paperboy reports %d", req.Version)
	}
	if err := cfg.Check(configKeys); err != nil {
		return err
	}
	v.clientID = strings.TrimSpace(cfg.String("client_id", ""))
	if v.clientID == "" {
		return errors.New("client_id is empty")
	}
	v.seedRefresh = strings.TrimSpace(cfg.String("refresh_token", ""))
	if v.seedRefresh == "" {
		return errors.New("refresh_token is empty: run the plugin with -login")
	}
	v.installation = strings.TrimSpace(cfg.String("installation", ""))
	v.gateway = strings.TrimSpace(cfg.String("gateway", ""))
	v.device = strings.TrimSpace(cfg.String("device", ""))

	base := strings.TrimRight(strings.TrimSpace(cfg.String("base", defaultBase)), "/")
	if base == "" {
		base = defaultBase
	}
	v.base = base
	v.iam = defaultIAM

	refresh, err := cfg.Duration("refresh", 10*time.Minute)
	if err != nil {
		return err
	}
	if refresh < minRefresh {
		fmt.Fprintf(os.Stderr, "vicare: refresh %s is below the minimum, using %s\n", refresh, minRefresh)
		refresh = minRefresh
	}
	v.refresh = refresh
	return nil
}

// fingerprint identifies the configuration in the stored state without
// keeping the seed token in it.
func (v *vicare) fingerprint() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		v.clientID, v.installation, v.gateway, v.device, v.seedRefresh,
	}, "\x00")))
	return hex.EncodeToString(sum[:8])
}

// LoadState takes over the state Paperboy stored last time (ADR 0081). A
// state of another format or another configuration is ignored.
func (v *vicare) LoadState(state json.RawMessage, _ map[string][]byte) error {
	if len(state) == 0 {
		return nil
	}
	var s storedState
	if err := json.Unmarshal(state, &s); err != nil {
		fmt.Fprintln(os.Stderr, "vicare: ignoring the stored state:", err)
		return nil
	}
	switch {
	case s.Version != stateVersion:
		fmt.Fprintf(os.Stderr, "vicare: ignoring a stored state of version %d\n", s.Version)
		return nil
	case s.Key != v.fingerprint():
		fmt.Fprintln(os.Stderr, "vicare: ignoring the stored state of another configuration")
		return nil
	}
	v.mu.Lock()
	v.installation, v.gateway, v.device = s.Installation, s.Gateway, s.Device
	v.model, v.roles, v.identified, v.tokens = s.Model, s.Roles, s.Identified, s.Tokens
	v.snap, v.fetched, v.next = s.Snapshot, s.FetchedAt, s.NextAttempt
	if s.Snapshot == nil && s.LastError != "" {
		v.loadErr = errors.New(s.LastError)
	}
	v.mu.Unlock()
	return nil
}

// storedLocked is the state to keep; callers hold v.mu.
func (v *vicare) storedLocked() storedState {
	s := storedState{
		Version: stateVersion, Key: v.fingerprint(),
		Installation: v.installation, Gateway: v.gateway, Device: v.device,
		Model: v.model, Roles: v.roles, Identified: v.identified, Tokens: v.tokens,
		FetchedAt: v.fetched, NextAttempt: v.next, Snapshot: v.snap,
	}
	if v.loadErr != nil {
		s.LastError = v.loadErr.Error()
	}
	return s
}

// queueLocked hands the current state to the next answer; callers hold v.mu.
func (v *vicare) queueLocked() {
	s := v.storedLocked()
	v.pending = &s
}

// State returns the state to store; nil when nothing changed (ADR 0081).
func (v *vicare) State() json.RawMessage {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.pending == nil {
		return nil
	}
	data, err := json.Marshal(v.pending)
	v.pending = nil
	if err != nil {
		return nil
	}
	return data
}

// Start reads the device when Paperboy needs it, never more often than
// refresh, and reports a change when a shown value moved (ADR 0065, 0126).
func (v *vicare) Start(ctx context.Context, notify func(widgets ...string)) {
	v.ready = make(chan struct{})
	go func() {
		var last string
		var have bool
		fetch := func() {
			snap, wait, err := v.fetch(ctx)
			changed := false
			v.mu.Lock()
			if err != nil {
				if v.loadErr == nil || v.loadErr.Error() != err.Error() {
					fmt.Fprintln(os.Stderr, "vicare:", err)
				}
				v.loadErr = err
			} else {
				v.snap, v.loadErr = snap, nil
				v.fetched = time.Now()
				sum, _ := json.Marshal(snap)
				changed = have && string(sum) != last
				last, have = string(sum), true
			}
			if wait < v.refresh {
				wait = v.refresh
			}
			v.next = time.Now().Add(wait)
			v.queueLocked()
			v.mu.Unlock()
			// The callback must not run under v.mu: ServeIO answers it with
			// State, which takes the same lock.
			if changed {
				notify()
			}
		}

		// A due state is the first fetch; a restart within the interval or
		// during a wait the API asked for costs no request.
		if !time.Now().Before(v.due()) {
			fetch()
		}
		v.signalReady()

		for {
			if !v.Wait(ctx, 0) {
				return
			}
			if wait := time.Until(v.due()); wait > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(wait):
				}
				continue
			}
			fetch()
		}
	}()
}

// due is the moment the next request is allowed.
func (v *vicare) due() time.Time {
	v.mu.Lock()
	defer v.mu.Unlock()
	d := v.fetched.Add(v.refresh)
	if v.next.After(d) {
		d = v.next
	}
	return d
}

func (v *vicare) signalReady() { v.readyOnce.Do(func() { close(v.ready) }) }

// fetch reads the features of the configured device. It discovers the IDs
// once when the configuration leaves them out, and returns how long the API
// asked to wait.
func (v *vicare) fetch(ctx context.Context) (*snapshot, time.Duration, error) {
	v.mu.Lock()
	inst, gw, dev, identified := v.installation, v.gateway, v.device, v.identified
	v.mu.Unlock()

	if !identified {
		i, g, d, err := v.resolve(ctx)
		switch {
		case err != nil && (inst == "" || gw == "" || dev == ""):
			return nil, 0, err
		case err != nil:
			// The IDs are configured, so the system is readable without the
			// account list; the model and the roles stay unknown.
			fmt.Fprintln(os.Stderr, "vicare: cannot read the installations, using the configured IDs:", err)
		default:
			inst, gw, dev = i.ID, g.Serial, d.ID
			v.mu.Lock()
			v.installation, v.gateway, v.device = inst, gw, dev
			v.model, v.roles, v.identified = d.ModelID, d.Roles, true
			v.queueLocked()
			v.mu.Unlock()
		}
	}

	path := "/iot/v2/features/installations/" + url.PathEscape(inst) +
		"/gateways/" + url.PathEscape(gw) + "/features?includeDevicesFeatures=true"
	body, status, header, err := v.apiGet(ctx, path)
	if err != nil {
		return nil, 0, err
	}
	if status != http.StatusOK {
		return nil, rateWait(status, header), problem(status, body)
	}
	var answer struct {
		Data []feature `json:"data"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return nil, 0, fmt.Errorf("invalid features answer: %w", err)
	}
	if len(answer.Data) == 0 {
		return nil, 0, errors.New("the API returned no features; is the device online?")
	}
	return snapshotFrom(answer.Data, dev), 0, nil
}

// Prepare makes the snapshot of the moment available to every widget and
// value of one request.
func (v *vicare) Prepare(req *plugin.Request) error {
	select {
	case <-v.ready:
	case <-time.After(15 * time.Second):
		return errors.New("heating system state is still loading")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.snap == nil {
		if v.loadErr != nil {
			return v.loadErr
		}
		return errors.New("heating system state is not available")
	}
	v.cur, v.req, v.loc, v.lang = v.snap, req, req.Location(), req.Lang()
	return nil
}

// MaxAge: the data is a snapshot, so the refresh interval is the reuse time.
func (v *vicare) MaxAge(*plugin.Request) time.Duration {
	return max(v.refresh, minRefresh)
}

// ---- labels and formatting ----

func (v *vicare) t(de, en string) string { return v.req.T(de, en) }

func (v *vicare) num(f float64, decimals int) string {
	return format.Number(f, decimals, v.lang)
}

func (v *vicare) temp(f float64) string {
	return v.num(f, 1) + " " + v.t("°C", "°C")
}

// circuit returns the configured circuit of a widget, nil when the device
// does not report it.
func (v *vicare) circuit(index int) *circuit {
	for i := range v.cur.Circuits {
		if v.cur.Circuits[i].Index == index {
			return &v.cur.Circuits[i]
		}
	}
	if len(v.cur.Circuits) > 0 {
		return &v.cur.Circuits[0]
	}
	return nil
}

// programName names the active heating program.
func (v *vicare) programName(program string) string {
	switch program {
	case "normal":
		return v.t("Normal", "Normal")
	case "reduced":
		return v.t("Absenken", "Reduced")
	case "comfort":
		return v.t("Komfort", "Comfort")
	case "eco":
		return v.t("Eco", "Eco")
	case "holiday":
		return v.t("Urlaub", "Holiday")
	case "frostprotection":
		return v.t("Frostschutz", "Frost protection")
	case "standby":
		return v.t("Standby", "Standby")
	case "external":
		return v.t("Extern", "External")
	case "manual":
		return v.t("Manuell", "Manual")
	case "fixed":
		return v.t("Fest", "Fixed")
	case "dhwPrecedence":
		return v.t("Warmwasser", "DHW")
	default:
		return program
	}
}

// modeName names the active operating mode of a circuit.
func (v *vicare) modeName(mode string) string {
	switch mode {
	case "heating":
		return v.t("Heizen", "Heating")
	case "dhwAndHeating":
		return v.t("Heizen + Warmwasser", "Heating + DHW")
	case "cooling":
		return v.t("Kühlen", "Cooling")
	case "heatingCooling":
		return v.t("Heizen/Kühlen", "Heating/cooling")
	case "dhwAndHeatingCooling":
		return v.t("Heizen/Kühlen", "Heating/cooling")
	case "standby":
		return v.t("Standby", "Standby")
	case "off":
		return v.t("Aus", "Off")
	default:
		return mode
	}
}

// dhwMode names the operating mode of the domestic hot water.
func (v *vicare) dhwMode(mode string) string {
	switch mode {
	case "off":
		return v.t("Aus", "Off")
	case "balanced":
		return v.t("Normal", "Balanced")
	case "comfort":
		return v.t("Komfort", "Comfort")
	case "eco":
		return v.t("Eco", "Eco")
	default:
		return mode
	}
}

// burnerState names the burner or the compressor: "An" or "Aus", empty when
// the device has neither.
func (v *vicare) burnerState() string {
	switch {
	case v.cur.Burner != nil:
		if v.cur.Burner.Active {
			return v.t("Brenner an", "Burner on")
		}
		return v.t("Brenner aus", "Burner off")
	case v.cur.Compressor != nil:
		if v.cur.Compressor.Active {
			return v.t("Kompressor an", "Compressor on")
		}
		return v.t("Kompressor aus", "Compressor off")
	}
	return ""
}

// stateKey is the value for plan conditions: an active error is "error".
func (v *vicare) stateKey() string {
	if len(v.cur.Errors) > 0 {
		return "error"
	}
	return "ok"
}

// dataAge formats when the device sent its newest value.
func (v *vicare) dataAge() string {
	t := v.cur.Updated
	if t.IsZero() {
		return ""
	}
	t = t.In(v.loc)
	now := v.req.Now.In(v.loc)
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return v.t("Stand ", "as of ") + t.Format("15:04")
	}
	return v.t("Stand ", "as of ") + t.Format("02.01. 15:04")
}
