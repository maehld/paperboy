// Command myskoda is a Paperboy plugin (ADR 0049) for the MyŠkoda Public API
// (https://public.api.connect.skoda-auto.cz/docs). It reads the vehicle state
// in the background and draws:
//
//   - "battery": state of charge, range and charging progress,
//   - "charging": charging state, plug, power, target and the current profile,
//   - "profiles": the saved charging profiles with their targets and timers,
//   - "location": parking position and address,
//   - "climate": air conditioning, window heating, auxiliary heating,
//   - "card": the vehicle render image with a compact state line,
//   - "car": a compact overview for small cells.
//
// All widgets except "card" have text variants for character displays.
//
// The API is rate-limited to 20 requests per hour per VIN and needs an API key
// created in the MyŠkoda app. It is meant for personal use only; the plugin is
// read-only, remote commands are not part of it (see the issue).
//
//	plugins:
//	  myskoda:
//	    config:
//	      api_key: ${SKODA_API_KEY}
//	      vin: TMBJB9NY5RF999999
//	      refresh: 1h
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

const (
	defaultBase = "https://public.api.connect.skoda-auto.cz"
	// includes are the parts the widgets draw; everything else stays out of
	// the answer, so unsupported parts are absent instead of reported.
	includes = "info,status,odometer,parkingPosition,airConditioning,auxiliaryHeating,activeVentilation,charging,chargingProfiles,operations"
	// requestTimeout bounds one HTTP call.
	requestTimeout = 30 * time.Second
	// maxImageBytes caps the render image download.
	maxImageBytes = 16 << 20
	// minRefresh is the shortest configured poll interval. The API allows 20
	// requests per hour and VIN, so 10m leaves room for commands later.
	minRefresh = 10 * time.Minute
	// keyWarnBefore is the remaining key lifetime that triggers a log line.
	keyWarnBefore = 7 * 24 * time.Hour

	problemExpired      = "api-key-expired"
	problemNotAllowed   = "api-key-not-authorized"
	problemNotSupported = "operation-not-supported"
	problemDisabled     = "operation-disabled"
	problemRateLimit    = "rate-limit-exceeded"
	problemVehicleBusy  = "vehicle-not-accepting-requests"
)

func main() {
	plugin.Serve(&myskoda{client: &http.Client{Timeout: requestTimeout}})
}

var configKeys = map[string]plugin.Arg{
	"api_key": {Type: "string", Required: true, Description: "API key from the MyŠkoda app, bound to the vehicle"},
	"vin":     {Type: "string", Required: true, Description: "vehicle identification number, 17 characters"},
	"refresh": {Type: "duration", Default: "1h", Description: "how often the vehicle state is read, at least 10m"},
	"base":    {Type: "string", Default: defaultBase, Description: "API base URL"},
}

// ---- API answer ----

// vehicleResponse is GET /api/v1/vehicles/{vin}.
type vehicleResponse struct {
	Vehicle vehicle        `json:"vehicle"`
	Errors  []vehicleError `json:"errors"`
}

// vehicleError names a part that is unsupported, disabled or unavailable. It
// is informational: the rest of the answer is still valid.
type vehicleError struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type vehicle struct {
	VIN               string            `json:"vin"`
	Name              string            `json:"name"`
	LicensePlate      string            `json:"licensePlate"`
	RenderURL         string            `json:"renderUrl"`
	Status            *status           `json:"status"`
	Odometer          *odometer         `json:"odometer"`
	ParkingPosition   *parkingPosition  `json:"parkingPosition"`
	AirConditioning   *climate          `json:"airConditioning"`
	AuxiliaryHeating  *auxHeating       `json:"auxiliaryHeating"`
	ActiveVentilation *ventilation      `json:"activeVentilation"`
	Charging          *charging         `json:"charging"`
	ChargingProfiles  *chargingProfiles `json:"chargingProfiles"`
	Operations        []operation       `json:"operations"`
}

type status struct {
	Overall struct {
		DoorsLocked *string `json:"doorsLocked"`
		Locked      *string `json:"locked"`
		Doors       *string `json:"doors"`
		Windows     *string `json:"windows"`
		Lights      *string `json:"lights"`
	} `json:"overall"`
	Detail struct {
		Sunroof *string `json:"sunroof"`
		Trunk   *string `json:"trunk"`
		Bonnet  *string `json:"bonnet"`
	} `json:"detail"`
	Captured string `json:"carCapturedTimestamp"`
}

type charging struct {
	Status          *chargingStatus   `json:"status"`
	Settings        *chargingSettings `json:"settings"`
	InSavedLocation *bool             `json:"isVehicleInSavedLocation"`
	Captured        string            `json:"carCapturedTimestamp"`
}

type chargingStatus struct {
	State          *string  `json:"state"`
	ChargeType     *string  `json:"chargeType"`
	PowerKW        *float64 `json:"chargePowerInKw"`
	RateKMH        *float64 `json:"chargingRateInKilometersPerHour"`
	RemainingMin   *int     `json:"remainingTimeToFullyChargedInMinutes"`
	FullyChargedAt string   `json:"fullyChargedAt"`
	PlugState      *string  `json:"plugConnectionState"`
	PlugLock       *string  `json:"plugLockState"`
	Battery        struct {
		SoC   *int `json:"stateOfChargeInPercent"`
		Range *int `json:"remainingCruisingRangeInMeters"`
	} `json:"battery"`
}

type chargingSettings struct {
	TargetSoC          *int     `json:"targetStateOfChargeInPercent"`
	PreferredMode      *string  `json:"preferredChargeMode"`
	AvailableModes     []string `json:"availableChargeModes"`
	CareMode           *string  `json:"chargingCareMode"`
	BatteryCareTarget  *int     `json:"batteryCareModeTargetValueInPercent"`
	MaxCurrentAC       *string  `json:"maxChargeCurrentAc"`
	MaxCurrentACAmpere *int     `json:"maxChargeCurrentAcAmpere"`
	AutoUnlock         *string  `json:"autoUnlockPlugWhenCharged"`
}

type chargingProfiles struct {
	Profiles []chargingProfile `json:"profiles"`
	Current  *currentProfile   `json:"currentVehiclePositionProfile"`
}

type chargingProfile struct {
	ID                     int64           `json:"id"`
	Name                   string          `json:"name"`
	Settings               profileSettings `json:"settings"`
	PreferredChargingTimes []chargingTime  `json:"preferredChargingTimes"`
	Timers                 []timer         `json:"timers"`
}

type profileSettings struct {
	MaxChargingCurrent *string `json:"maxChargingCurrent"`
	TargetSoC          *int    `json:"targetStateOfChargeInPercent"`
	AutoUnlock         *string `json:"autoUnlockPlugWhenCharged"`
}

type chargingTime struct {
	Enabled bool   `json:"enabled"`
	Start   string `json:"startTime"`
	End     string `json:"endTime"`
}

type timer struct {
	Enabled     bool     `json:"enabled"`
	Time        string   `json:"time"`
	Type        string   `json:"type"`
	OneOffDay   string   `json:"oneOffDay"`
	RecurringOn []string `json:"recurringOn"`
}

type currentProfile struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	TargetSoC        *int   `json:"targetStateOfChargeInPercent"`
	NextChargingTime string `json:"nextChargingTime"`
}

type parkingPosition struct {
	State string `json:"state"`
	GPS   *struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	} `json:"gpsCoordinates"`
	Address  string `json:"formattedAddress"`
	Captured string `json:"carCapturedTimestamp"`
}

type temperature struct {
	Value *float64 `json:"value"`
	Unit  *string  `json:"unit"`
}

type climate struct {
	State         *string     `json:"state"`
	Target        temperature `json:"targetTemperature"`
	ReachAt       string      `json:"estimatedReachOfTargetTemperatureAt"`
	WithoutPower  *bool       `json:"airConditioningWithoutExternalPower"`
	AtUnlock      *bool       `json:"airConditioningAtUnlock"`
	WindowHeating struct {
		Enabled *bool   `json:"enabled"`
		Front   *string `json:"front"`
		Rear    *string `json:"rear"`
	} `json:"windowHeating"`
	Captured string `json:"carCapturedTimestamp"`
}

type auxHeating struct {
	State     *string     `json:"state"`
	StartMode *string     `json:"startMode"`
	Duration  *int        `json:"durationInSeconds"`
	Target    temperature `json:"targetTemperature"`
	ReachAt   string      `json:"estimatedReachOfTargetTemperatureAt"`
	Captured  string      `json:"carCapturedTimestamp"`
}

type ventilation struct {
	State    *string `json:"state"`
	Duration *int    `json:"durationInSeconds"`
	Captured string  `json:"carCapturedTimestamp"`
}

type odometer struct {
	KM       *int64 `json:"mileageInKm"`
	Captured string `json:"carCapturedTimestamp"`
}

type operation struct {
	Name string `json:"name"`
}

// supplement is the answer of the render image download.
type supplement struct {
	img image.Image
	url string
	err error
}

// stateVersion is the format of state.json; a file of another version is
// discarded instead of guessed (ADR 0065).
const stateVersion = 1

// storedState is what the plugin keeps between restarts (ADR 0065). It holds
// no secret: the API key appears as a fingerprint only, so a changed key
// throws the file away instead of trusting data fetched with another one.
type storedState struct {
	Version     int       `json:"version"`
	VIN         string    `json:"vin"`
	Key         string    `json:"key"`
	FetchedAt   time.Time `json:"fetched_at"`
	NextAttempt time.Time `json:"next_attempt,omitempty"`
	KeyExpires  time.Time `json:"key_expires_at,omitempty"`
	RenderURL   string    `json:"render_url,omitempty"`
	Vehicle     *vehicle  `json:"vehicle,omitempty"`
	// LastError is the message the widgets showed after the last attempt, so
	// a restart during a failed load keeps the notice instead of claiming
	// that there is no data yet.
	LastError string `json:"last_error,omitempty"`
}

// myskoda is the plugin.
type myskoda struct {
	client  *http.Client
	apiKey  string
	vin     string
	base    string
	refresh time.Duration

	statePath string // state/plugins/myskoda/state.json; empty without a state directory
	imagePath string // state/plugins/myskoda/render.png
	readyOnce sync.Once

	mu      sync.Mutex
	data    *vehicle
	image   *supplement
	loadErr error
	ready   chan struct{}
	fetched time.Time // when the API answered last
	next    time.Time // no request before this: the refresh interval or a wait the API asked for
	expires time.Time // X-API-Key-Expires-At

	cur  *vehicle
	img  *supplement
	req  *plugin.Request
	loc  *time.Location
	lang string
}

func (m *myskoda) Describe() plugin.Description {
	both := []string{plugin.KindImage, plugin.KindText}
	return plugin.Description{
		Title:   "MyŠkoda",
		Version: version(),
		Config:  configKeys,
		Widgets: []plugin.WidgetInfo{
			{Widget: "battery", Title: "Battery and range", Kinds: both, MinWidth: 160, MinHeight: 80,
				Sizes: []plugin.Span{{Columns: 3, Rows: 3}, {Columns: 6, Rows: 2}, {Columns: 2, Rows: 2}},
				Args: map[string]plugin.Arg{
					"range": {Type: "bool", Default: true, Description: "show the remaining range"},
				}},
			{Widget: "charging", Title: "Charging", Kinds: both, MinWidth: 200, MinHeight: 80,
				Sizes: []plugin.Span{{Columns: 3, Rows: 3}, {Columns: 6, Rows: 2}},
				Args: map[string]plugin.Arg{
					"profile": {Type: "bool", Default: true, Description: "show the current charging profile"},
				}},
			{Widget: "profiles", Title: "Charging profiles", Kinds: both, MinWidth: 200, MinHeight: 100,
				Sizes: []plugin.Span{{Columns: 3, Rows: 4}, {Columns: 6, Rows: 3}},
				Args: map[string]plugin.Arg{
					"max": {Type: "int", Default: 2, Description: "profiles to show, 1 to 5"},
				}},
			{Widget: "location", Title: "Parking position", Kinds: both, MinWidth: 160, MinHeight: 80,
				Sizes: []plugin.Span{{Columns: 3, Rows: 3}, {Columns: 6, Rows: 2}},
				Args: map[string]plugin.Arg{
					"coords": {Type: "bool", Default: true, Description: "show the coordinates"},
				}},
			{Widget: "climate", Title: "Air conditioning", Kinds: both, MinWidth: 160, MinHeight: 80,
				Sizes: []plugin.Span{{Columns: 3, Rows: 3}, {Columns: 3, Rows: 2}}},
			{Widget: "card", Title: "Vehicle image", Kinds: []string{plugin.KindImage}, MinWidth: 200, MinHeight: 150,
				Sizes: []plugin.Span{{Columns: 6, Rows: 4}, {Columns: 6, Rows: 8}, {Columns: 3, Rows: 4}},
				Args: map[string]plugin.Arg{
					"name":  {Type: "bool", Default: true, Description: "show the vehicle name"},
					"state": {Type: "bool", Default: true, Description: "show state of charge, range and charging state"},
				}},
			{Widget: "car", Title: "Vehicle overview", Kinds: both, MinWidth: 160, MinHeight: 80,
				Sizes: []plugin.Span{{Columns: 3, Rows: 3}, {Columns: 2, Rows: 2}, {Columns: 6, Rows: 2}},
				Args: map[string]plugin.Arg{
					"plate": {Type: "bool", Default: false, Description: "show the license plate"},
				}},
		},
	}
}

func (m *myskoda) Configure(cfg plugin.Values, req *plugin.Request) error {
	if err := cfg.Check(configKeys); err != nil {
		return err
	}
	m.apiKey = strings.TrimSpace(cfg.String("api_key", ""))
	if m.apiKey == "" {
		return errors.New("api_key is empty")
	}
	m.vin = strings.ToUpper(strings.TrimSpace(cfg.String("vin", "")))
	if len(m.vin) != 17 {
		return fmt.Errorf("vin: expected 17 characters, got %d", len(m.vin))
	}
	base, err := url.Parse(strings.TrimRight(cfg.String("base", defaultBase), "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return errors.New("base: expected http(s)://host")
	}
	refresh, err := cfg.Duration("refresh", time.Hour)
	if err != nil {
		return err
	}
	if refresh < minRefresh {
		fmt.Fprintf(os.Stderr, "myskoda: refresh %s is below the minimum, using %s\n", refresh, minRefresh)
		refresh = minRefresh
	}
	m.base, m.refresh = base.String(), refresh
	m.setupState()
	return nil
}

// setupState picks the plugin's state directory (ADR 0065) and takes over what
// the last run left there, so the first page after a restart has data and a
// restart within the refresh interval costs no request.
func (m *myskoda) setupState() {
	dir := plugin.StateDir()
	if dir == "" {
		return
	}
	m.statePath = filepath.Join(dir, "state.json")
	m.imagePath = filepath.Join(dir, "render.png")
	m.loadState()
}

// loadState reads state.json; a file of another vehicle, another key, another
// format, or one that cannot be read is ignored and replaced by the next run.
func (m *myskoda) loadState() {
	data, err := os.ReadFile(m.statePath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintln(os.Stderr, "myskoda: reading the stored state:", err)
		}
		return
	}
	var s storedState
	if err := json.Unmarshal(data, &s); err != nil {
		fmt.Fprintln(os.Stderr, "myskoda: ignoring the stored state:", err)
		return
	}
	switch {
	case s.Version != stateVersion:
		fmt.Fprintf(os.Stderr, "myskoda: ignoring a stored state of version %d\n", s.Version)
		return
	case !strings.EqualFold(s.VIN, m.vin):
		fmt.Fprintln(os.Stderr, "myskoda: ignoring the stored state of another vehicle")
		return
	case s.Key != m.fingerprint():
		fmt.Fprintln(os.Stderr, "myskoda: ignoring the stored state of another API key")
		return
	}
	// The timing counts even without data: a wait the API asked for outlasts a
	// restart, and a failed first load stays failed.
	m.mu.Lock()
	m.data = s.Vehicle
	m.fetched, m.next, m.expires = s.FetchedAt, s.NextAttempt, s.KeyExpires
	if s.Vehicle == nil && s.LastError != "" {
		m.loadErr = errors.New(s.LastError)
	}
	m.mu.Unlock()
	if s.RenderURL != "" {
		if img, err := readImage(m.imagePath); err == nil {
			m.image = &supplement{img: img, url: s.RenderURL}
		}
	}
	if !m.expires.IsZero() && time.Until(m.expires) < keyWarnBefore {
		fmt.Fprintf(os.Stderr, "myskoda: API key expires at %s; create a new one in the MyŠkoda app\n",
			m.expires.Format(time.RFC3339))
	}
}

// fingerprint identifies the API key in the stored state without keeping it.
func (m *myskoda) fingerprint() string {
	sum := sha256.Sum256([]byte(m.apiKey))
	return hex.EncodeToString(sum[:8])
}

// snapshot is the state to store; callers hold m.mu.
func (m *myskoda) snapshot() storedState {
	s := storedState{Version: stateVersion, VIN: m.vin, Key: m.fingerprint(),
		FetchedAt: m.fetched, NextAttempt: m.next, KeyExpires: m.expires, Vehicle: m.data}
	if m.loadErr != nil {
		s.LastError = m.loadErr.Error()
	}
	if m.image != nil {
		s.RenderURL = m.image.url
	}
	return s
}

// persist writes the state; failures only cost the next restart a request.
func (m *myskoda) persist(s storedState) {
	if m.statePath == "" {
		return
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err == nil {
		err = writeFile(m.statePath, data)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "myskoda: saving the state:", err)
	}
}

// persistImage keeps the render image for the next start; the API key is not
// part of it.
func (m *myskoda) persistImage(img image.Image) {
	if m.imagePath == "" || img == nil {
		return
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return
	}
	if err := writeFile(m.imagePath, buf.Bytes()); err != nil {
		fmt.Fprintln(os.Stderr, "myskoda: saving the render image:", err)
	}
}

// writeFile replaces path atomically, so a crash never leaves half a file.
func writeFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// readImage decodes a PNG the last run stored.
func readImage(path string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	img, _, err := image.Decode(io.LimitReader(file, maxImageBytes+1))
	return img, err
}

// Start loads the vehicle state when it is due and then every refresh, and
// reports a change when the displayed data moved. A state left by the last run
// decides the first attempt, so a restart within the interval or during a wait
// the API asked for costs no request (ADR 0065).
func (m *myskoda) Start(ctx context.Context, notify func(widgets ...string)) {
	m.ready = make(chan struct{})
	go func() {
		var last string
		var have bool
		var warned time.Time
		for {
			if wait := time.Until(m.due()); wait > 0 {
				m.signalReady()
				select {
				case <-ctx.Done():
					return
				case <-time.After(wait):
				}
				continue
			}
			ans, err := m.fetch(ctx)
			wait := m.refresh
			var needImage string
			m.mu.Lock()
			if err != nil {
				if m.loadErr == nil || err.Error() != m.loadErr.Error() {
					fmt.Fprintln(os.Stderr, "myskoda:", err)
				}
				m.loadErr = err
			} else {
				m.data, m.loadErr = ans.vehicle, nil
				m.fetched = time.Now()
				if !ans.expires.IsZero() {
					m.expires = ans.expires
				}
				sum, _ := json.Marshal(ans.vehicle)
				if have && string(sum) != last {
					notify()
				}
				last, have = string(sum), true
				if ans.vehicle.RenderURL != "" && (m.image == nil || m.image.url != ans.vehicle.RenderURL || m.image.img == nil) {
					// The download happens outside the lock; a failed one is
					// retried at the next refresh.
					needImage = ans.vehicle.RenderURL
				}
			}
			// A 429 or an exhausted quota outlasts the refresh interval.
			if ans.wait > wait {
				wait = ans.wait
			}
			m.next = time.Now().Add(wait)
			m.mu.Unlock()

			if needImage != "" {
				img, err := m.downloadImage(ctx, needImage)
				m.mu.Lock()
				m.image = &supplement{img: img, url: needImage, err: err}
				m.mu.Unlock()
				if err != nil {
					fmt.Fprintln(os.Stderr, "myskoda:", err)
				}
			}

			if !ans.expires.IsZero() && !ans.expires.Equal(warned) && time.Until(ans.expires) < keyWarnBefore {
				fmt.Fprintf(os.Stderr, "myskoda: API key expires at %s; create a new one in the MyŠkoda app\n",
					ans.expires.Format(time.RFC3339))
				warned = ans.expires
			}

			m.mu.Lock()
			s, image := m.snapshot(), m.image
			m.mu.Unlock()
			m.persist(s)
			if needImage != "" && image != nil {
				m.persistImage(image.img)
			}
			m.signalReady()
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
	}()
}

// due is the moment the next request is allowed: one refresh after the last
// answer, later if the API asked to wait.
func (m *myskoda) due() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.fetched.Add(m.refresh)
	if m.next.After(d) {
		d = m.next
	}
	return d
}

// signalReady tells Prepare that the state is there, once.
func (m *myskoda) signalReady() { m.readyOnce.Do(func() { close(m.ready) }) }

// downloadImage reads the vehicle render. It sends no API key: the URL is a
// plain image address, and the key belongs to the API host only.
func (m *myskoda) downloadImage(ctx context.Context, u string) (image.Image, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("render image: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxImageBytes {
		return nil, fmt.Errorf("render image larger than %d bytes", maxImageBytes)
	}
	img, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("render image: %w", err)
	}
	return img, nil
}

// answer is one successful or failed request with what the loop needs to plan
// the next one.
type answer struct {
	vehicle *vehicle
	expires time.Time     // X-API-Key-Expires-At
	wait    time.Duration // server-requested wait: 429 or an exhausted quota
}

// apiError is a non-200 answer with the problem type of the API.
type apiError struct {
	status  int
	kind    string
	detail  string
	vehicle string
}

func (e *apiError) Error() string {
	switch {
	case e.kind == problemExpired:
		return "API key expired: create a new key in the MyŠkoda app and set api_key again"
	case e.kind == problemNotAllowed:
		return "API key is not authorized for this vehicle: check the key's vehicles in the MyŠkoda app"
	case e.kind == problemRateLimit:
		return "rate limit exceeded: the API allows 20 requests per hour and vehicle"
	case e.kind == problemVehicleBusy:
		return "the vehicle is not accepting requests right now, retrying later"
	case e.kind == problemNotSupported:
		return "the vehicle does not support this operation"
	case e.kind == problemDisabled:
		return "the operation is currently disabled for this vehicle"
	}
	msg := fmt.Sprintf("HTTP %d", e.status)
	if e.detail != "" {
		msg += ": " + e.detail
	}
	if e.vehicle != "" {
		msg += " (" + e.vehicle + ")"
	}
	return msg
}

// fetch reads the vehicle state. A 429 answer carries the wait from
// Retry-After; an exhausted quota carries RateLimit-Reset.
func (m *myskoda) fetch(ctx context.Context) (answer, error) {
	var ans answer
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	u := m.base + "/api/v1/vehicles/" + url.PathEscape(m.vin)
	q := url.Values{"include": {includes}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u+"?"+q.Encode(), nil)
	if err != nil {
		return ans, err
	}
	req.Header.Set("X-API-Key", m.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return ans, err
	}
	defer resp.Body.Close()

	if t, ok := parseExpiry(resp.Header.Get("X-API-Key-Expires-At")); ok {
		ans.expires = t
	}
	ans.wait = waitFromHeaders(resp.Header)

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return ans, err
	}
	if resp.StatusCode != http.StatusOK {
		return ans, problem(resp.StatusCode, body, m.vin)
	}
	var vr vehicleResponse
	if err := json.Unmarshal(body, &vr); err != nil {
		return ans, fmt.Errorf("invalid answer: %w", err)
	}
	if vr.Vehicle.VIN != "" && !strings.EqualFold(vr.Vehicle.VIN, m.vin) {
		return ans, fmt.Errorf("the API answered for %s instead of %s", vr.Vehicle.VIN, m.vin)
	}
	ans.vehicle = &vr.Vehicle
	return ans, nil
}

// problem turns an error answer into an apiError, preferring the RFC 9457
// problem type over the status code.
func problem(status int, body []byte, vin string) error {
	var p struct {
		Type   string `json:"type"`
		Detail string `json:"detail"`
	}
	_ = json.Unmarshal(body, &p)
	kind := p.Type
	if i := strings.LastIndex(kind, "/"); i >= 0 {
		kind = kind[i+1:]
	}
	return &apiError{status: status, kind: kind, detail: p.Detail, vehicle: vin}
}

// parseExpiry reads the key expiry header, which is an HTTP date or RFC 3339.
func parseExpiry(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	if t, err := http.ParseTime(s); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// waitFromHeaders returns how long to wait before the next request: Retry-After
// after a 429, or the window reset once the quota is used up.
func waitFromHeaders(h http.Header) time.Duration {
	if s := h.Get("Retry-After"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
		if t, err := http.ParseTime(s); err == nil {
			if d := time.Until(t); d > 0 {
				return d
			}
		}
	}
	if h.Get("RateLimit-Remaining") == "0" {
		if n, err := strconv.Atoi(h.Get("RateLimit-Reset")); err == nil && n > 0 {
			return time.Duration(n+30) * time.Second
		}
	}
	return 0
}

func (m *myskoda) Prepare(req *plugin.Request) error {
	select {
	case <-m.ready:
	case <-time.After(15 * time.Second):
		return errors.New("vehicle state is still loading")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data == nil {
		if m.loadErr != nil {
			return m.loadErr
		}
		return errors.New("vehicle state is not available")
	}
	m.cur, m.img, m.req, m.loc, m.lang = m.data, m.image, req, req.Location(), req.Lang()
	return nil
}

// MaxAge: the data is a snapshot, so the refresh interval is the sensible
// reuse time as well.
func (m *myskoda) MaxAge(*plugin.Request) time.Duration {
	return max(m.refresh, minRefresh)
}

// ---- labels and formatting ----

func (m *myskoda) t(de, en string) string { return m.req.T(de, en) }

func (m *myskoda) num(v float64, decimals int) string {
	s := strconv.FormatFloat(v, 'f', decimals, 64)
	if m.lang == "de" {
		s = strings.Replace(s, ".", ",", 1)
	}
	return s
}

func (m *myskoda) soc() (int, bool) {
	if c := m.cur.Charging; c != nil && c.Status != nil && c.Status.Battery.SoC != nil {
		return *c.Status.Battery.SoC, true
	}
	return 0, false
}

// rangeKM returns the remaining range, rounded to whole kilometres.
func (m *myskoda) rangeKM() (int, bool) {
	if c := m.cur.Charging; c != nil && c.Status != nil && c.Status.Battery.Range != nil {
		return int(math.Round(float64(*c.Status.Battery.Range) / 1000)), true
	}
	return 0, false
}

func (m *myskoda) targetSoC() (int, bool) {
	if c := m.cur.Charging; c != nil && c.Settings != nil && c.Settings.TargetSoC != nil {
		return *c.Settings.TargetSoC, true
	}
	return 0, false
}

// chargeState names charging.status.state; an unknown value is shown as it is
// rather than guessed. The labels stay short: they share a line with the power
// and, on text displays, with the range.
func (m *myskoda) chargeState() string {
	if m.cur.Charging == nil || m.cur.Charging.Status == nil || m.cur.Charging.Status.State == nil {
		return m.t("Unbekannt", "Unknown")
	}
	switch s := *m.cur.Charging.Status.State; s {
	case "CONNECT_CABLE":
		return m.t("Getrennt", "Unplugged")
	case "CHARGING":
		return m.t("Lädt", "Charging")
	case "CONSERVING":
		return m.t("Erhaltung", "Holding")
	case "READY_FOR_CHARGING":
		return m.t("Bereit", "Ready")
	case "DISCHARGING":
		return m.t("Entlädt", "Discharging")
	case "CHARGING_INTERRUPTED":
		return m.t("Unterbrochen", "Interrupted")
	default:
		return s
	}
}

// climateState names airConditioning.state.
func (m *myskoda) climateState() string {
	if m.cur.AirConditioning == nil {
		return m.t("Unbekannt", "Unknown")
	}
	if m.cur.AirConditioning.State == nil {
		return m.t("Unbekannt", "Unknown")
	}
	switch s := *m.cur.AirConditioning.State; s {
	case "OFF":
		return m.t("Aus", "Off")
	case "COOLING":
		return m.t("Kühlt", "Cooling")
	case "HEATING":
		return m.t("Heizt", "Heating")
	case "HEATING_AUXILIARY":
		return m.t("Heizt", "Heating")
	case "VENTILATION":
		return m.t("Lüftet", "Ventilating")
	case "COMPLETED":
		return m.t("Fertig", "Done")
	default:
		return s
	}
}

// lockState names status.overall.doorsLocked.
func (m *myskoda) lockState() string {
	if m.cur.Status == nil || m.cur.Status.Overall.DoorsLocked == nil {
		return m.t("Unbekannt", "Unknown")
	}
	switch s := *m.cur.Status.Overall.DoorsLocked; s {
	case "YES":
		return m.t("Verriegelt", "Locked")
	case "NO":
		return m.t("Offen", "Unlocked")
	case "OPENED":
		return m.t("Offen", "Open")
	case "TRUNK_OPENED":
		return m.t("Kofferraum offen", "Trunk open")
	default:
		return s
	}
}

// power returns the charge power in kW, if the car reports one.
func (m *myskoda) power() (float64, bool) {
	if m.cur.Charging != nil && m.cur.Charging.Status != nil && m.cur.Charging.Status.PowerKW != nil {
		return *m.cur.Charging.Status.PowerKW, true
	}
	return 0, false
}

// remaining returns the time until the battery is full as a short text.
func (m *myskoda) remaining() string {
	c := m.cur.Charging
	if c == nil || c.Status == nil {
		return ""
	}
	if c.Status.RemainingMin != nil && *c.Status.RemainingMin > 0 {
		min := *c.Status.RemainingMin
		if min < 60 {
			return fmt.Sprintf("%d min", min)
		}
		return fmt.Sprintf("%d h %02d", min/60, min%60)
	}
	if t, ok := m.time(c.Status.FullyChargedAt); ok {
		return m.t("voll ", "full ") + t.Format("15:04")
	}
	return ""
}

// time parses an API timestamp and returns it in the request's timezone.
func (m *myskoda) time(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t.In(m.loc), true
}

// captured formats when the vehicle sent the data: "Stand 14:20", with the
// date when it was not today.
func (m *myskoda) captured(s string) string {
	t, ok := m.time(s)
	if !ok {
		return ""
	}
	if t.Year() == m.req.Now.In(m.loc).Year() && t.YearDay() == m.req.Now.In(m.loc).YearDay() {
		return m.t("Stand ", "as of ") + t.Format("15:04")
	}
	return m.t("Stand ", "as of ") + t.Format("02.01. 15:04")
}

// temperature formats the target temperature with its unit.
func (m *myskoda) temperature(t temperature) string {
	if t.Value == nil {
		return ""
	}
	unit := "°C"
	if t.Unit != nil && *t.Unit == "FAHRENHEIT" {
		unit = "°F"
	}
	return m.num(*t.Value, 1) + " " + unit
}

// profileName returns the profile the car is currently in.
func (m *myskoda) currentProfile() *currentProfile {
	if m.cur.ChargingProfiles == nil {
		return nil
	}
	return m.cur.ChargingProfiles.Current
}
