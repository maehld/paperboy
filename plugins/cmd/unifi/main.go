// Command unifi is a Paperboy plugin (ADR 0049) that shows the state of a
// UniFi home network: how many devices and clients the controller knows, how
// many devices are offline and what the internet says. It reads the documented
// UniFi integration API for the sites, the devices and the clients, and the
// controller's private stat/health endpoint for the short WAN summary
// (issue #494).
//
//	plugins:
//	  unifi:
//	    config:
//	      url: https://192.168.1.1
//	      api_key: ${UNIFI_API_KEY}
//
//	pages/netzwerk.yaml:
//	  cells:
//	    - { at: [0, 0], span: [2, 2], widget: unifi/status }
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
)

const (
	// defaultRefresh is the poll interval.
	defaultRefresh = 5 * time.Minute
	// minRefresh keeps the plugin from asking faster than the controller likes.
	minRefresh = time.Minute
	// requestTimeout bounds one HTTP call.
	requestTimeout = 15 * time.Second
	userAgent      = "paperboy-unifi (+https://github.com/maehld/paperboy)"
)

func main() {
	plugin.Serve(newPlugin())
}

func newPlugin() *unifi {
	return &unifi{}
}

var configKeys = map[string]plugin.Arg{
	"url": {
		Type: "string", Required: true,
		Description: "https address of the UniFi controller, e.g. https://192.168.1.1",
	},
	"api_key": {
		Type: "string", Required: true,
		Description: "API key of the UniFi integration, sent as X-API-KEY",
	},
	"site": {
		Type:        "string",
		Description: "site to read, by name or id; without it the first site is used",
	},
	"refresh": {
		Type: "duration", Default: "5m",
		Description: "how often the controller is read, at least 1m",
	},
	"insecure": {
		Type: "bool", Default: false,
		Description: "accept the controller's own TLS certificate",
	},
	"wan": {
		Type: "bool", Default: true,
		Description: "read the WAN summary and count its state; off skips the private call and hides the WAN line",
	},
}

// unifi is the plugin.
type unifi struct {
	plugin.Demand

	client *http.Client
	api    *integrationAPI
	priv   *privateAPI

	// configuration, set once by Configure
	site       string
	wanEnabled bool
	refresh    time.Duration

	mu      sync.Mutex
	snap    snapshot
	lastErr error
}

// stderr receives the log lines; tests replace it to count them.
var stderr io.Writer = os.Stderr

var (
	_ plugin.Handler = (*unifi)(nil)
	_ plugin.Valuer  = (*unifi)(nil)
	_ plugin.Starter = (*unifi)(nil)
	_ plugin.Ager    = (*unifi)(nil)
)

func (u *unifi) Describe() plugin.Description {
	image := []string{plugin.KindImage}
	return plugin.Description{
		Title:   "UniFi network",
		Version: version(),
		Config:  configKeys,
		Values: []plugin.ValueSpec{
			{Name: "state", Title: "Network state", Type: plugin.ValueEnum, Enum: stateKeys},
			{Name: "clients", Title: "Clients", Type: plugin.ValueNumber},
			{Name: "devices", Title: "Devices", Type: plugin.ValueNumber},
			{Name: "offline", Title: "Offline devices", Type: plugin.ValueNumber},
			{Name: "worst", Title: "Most important fact", Type: plugin.ValueText},
			{Name: "latency", Title: "Internet latency", Type: plugin.ValueNumber, Unit: "ms"},
			{Name: "download", Title: "Download speed", Type: plugin.ValueNumber, Unit: "Mbit/s", Precision: 1},
			{Name: "upload", Title: "Upload speed", Type: plugin.ValueNumber, Unit: "Mbit/s", Precision: 1},
		},
		Widgets: []plugin.WidgetInfo{
			{
				Widget: "status", Title: "Network status", Kinds: image,
				Sizes:    []plugin.Span{{Columns: 2, Rows: 2}, {Columns: 3, Rows: 2}, {Columns: 6, Rows: 1}},
				MinWidth: 120, MinHeight: 60,
				Args: map[string]plugin.Arg{
					"focus": {
						Type: "string", Default: "clients", Enum: []any{"clients", "devices"},
						EnumLabels:  map[string]string{"clients": "Clients", "devices": "Devices"},
						Description: "which count is the large value",
					},
					"detail": {
						Type: "bool", Default: true,
						Description: "show the status line under the value",
					},
				},
			},
			{
				Widget: "wan", Title: "Internet status", Kinds: image,
				Sizes:    []plugin.Span{{Columns: 2, Rows: 2}, {Columns: 3, Rows: 2}, {Columns: 6, Rows: 1}},
				MinWidth: 120, MinHeight: 60,
				Args: map[string]plugin.Arg{
					"traffic": {
						Type: "bool", Default: false,
						Description: "show the upload and download rate of the last fetch",
					},
					"ip": {
						Type: "bool", Default: false,
						Description: "show the public address of the connection",
					},
					"speed": {
						Type: "bool", Default: true,
						Description: "show the result of the last speed test",
					},
				},
			},
		},
	}
}

func (u *unifi) Configure(cfg plugin.Values, req *plugin.Request) error {
	if req.Version != 0 && req.Version < 9 {
		return fmt.Errorf("unifi needs Paperboy protocol 9 or newer; this Paperboy reports %d", req.Version)
	}
	if err := cfg.Check(configKeys); err != nil {
		return err
	}
	raw := strings.TrimRight(strings.TrimSpace(cfg.String("url", "")), "/")
	base, err := url.Parse(raw)
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return fmt.Errorf("url: expected an https address such as https://192.168.1.1, got %q", raw)
	}
	key := strings.TrimSpace(cfg.String("api_key", ""))
	if key == "" {
		return errors.New("api_key: empty")
	}
	refresh, err := cfg.Duration("refresh", defaultRefresh)
	if err != nil {
		return err
	}
	if refresh < minRefresh {
		return fmt.Errorf("refresh: %s is below the minimum of %s", refresh, minRefresh)
	}

	client := u.client
	if client == nil {
		// The controller serves its own certificate; insecure opts into it.
		insecure := cfg.Bool("insecure", false)
		client = &http.Client{
			Timeout: requestTimeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
			},
		}
	}
	u.client = client
	u.api = &integrationAPI{controller{base: raw, key: key, http: client}}
	u.priv = &privateAPI{controller{base: raw, key: key, http: client}}
	u.site = strings.TrimSpace(cfg.String("site", ""))
	u.wanEnabled = cfg.Bool("wan", true)
	u.refresh = refresh
	return nil
}

// MaxAge lets Paperboy reuse the answers for one refresh interval.
func (u *unifi) MaxAge(*plugin.Request) time.Duration {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.refresh
}

// Start reads the controller now and then when Paperboy needs the data
// (ADR 0126), never more often than refresh, and reports a change when what
// the values show moves.
func (u *unifi) Start(ctx context.Context, notify func(widgets ...string)) {
	go func() {
		if u.fetch(ctx) {
			notify()
		}
		for {
			if !u.Wait(ctx, u.refresh) {
				return
			}
			if u.fetch(ctx) {
				notify()
			}
		}
	}()
}

// fetch reads the controller once and returns whether the shown answer
// changed. A failed request keeps the last data. The private health call is
// best effort: its error is logged once and never discards the fresh devices
// and clients.
func (u *unifi) fetch(ctx context.Context) bool {
	u.mu.Lock()
	prev, wan := u.snap, u.wanEnabled
	u.mu.Unlock()

	next := prev
	if next.site.ID == "" {
		sites, err := u.api.sites(ctx)
		if err != nil {
			u.logErr(err)
			return false
		}
		site, err := pickSite(sites, u.site)
		if err != nil {
			u.logErr(err)
			return false
		}
		next.site = site
	}

	devices, err := u.api.devices(ctx, next.site.ID)
	if err != nil {
		u.logErr(err)
		return false
	}
	next.devices = devices

	clients, err := u.api.clients(ctx, next.site.ID)
	if err != nil {
		u.logErr(err)
		return false
	}
	next.clients = clients

	healthOK := true
	if wan {
		healthOK = false
		if wan, www, err := u.priv.health(ctx, next.site.InternalReference); err != nil {
			// The private call fails quietly: one log line, and the previous
			// health stays. The fresh devices and clients stay either way.
			u.logErr(err)
		} else {
			next.wan, next.www = wan, www
			healthOK = true
		}
	} else {
		// wan:false drops the private call completely. Clear an earlier
		// answer so the values and the signature do not keep it.
		next.wan, next.www = healthSubsystem{}, healthSubsystem{}
	}
	next.loaded = true

	u.mu.Lock()
	if healthOK {
		u.lastErr = nil
	}
	changed := prev.signature() != next.signature()
	u.snap = next
	u.mu.Unlock()
	return changed
}

// logErr writes err to the log once: the same message is not repeated on
// every refresh. A fully successful fetch clears the mark, so an error that
// returns logs again.
func (u *unifi) logErr(err error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.lastErr == nil || err.Error() != u.lastErr.Error() {
		fmt.Fprintln(stderr, "unifi:", err)
		u.lastErr = err
	}
}

// Values answers the declared values of a text page (ADR 0111). A value the
// plugin cannot read yet answers a dash; the state answers an empty enum, so
// no plan condition matches.
func (u *unifi) Values(req *plugin.Request) (map[string]plugin.Value, error) {
	u.mu.Lock()
	snap, wan := u.snap, u.wanEnabled
	u.mu.Unlock()

	dash := plugin.TextValue("–")
	t := func(de, en string) string { return req.T(de, en) }
	out := make(map[string]plugin.Value, len(req.Values))
	for _, name := range req.Values {
		switch name {
		case "state":
			if key := snap.stateKey(wan); key != "" {
				out[name] = plugin.EnumValue(key)
			} else {
				out[name] = plugin.Value{}
			}
		case "clients":
			if !snap.loaded {
				out[name] = dash
			} else {
				out[name] = plugin.NumberValue(float64(snap.clients))
			}
		case "devices":
			if !snap.loaded {
				out[name] = dash
			} else {
				out[name] = plugin.NumberValue(float64(len(snap.devices)))
			}
		case "offline":
			if !snap.loaded {
				out[name] = dash
			} else {
				out[name] = plugin.NumberValue(float64(snap.offline()))
			}
		case "download":
			if v := snap.speed(true); v == nil {
				out[name] = dash
			} else {
				out[name] = plugin.NumberValue(*v)
			}
		case "upload":
			if v := snap.speed(false); v == nil {
				out[name] = dash
			} else {
				out[name] = plugin.NumberValue(*v)
			}
		case "worst":
			if !snap.loaded {
				out[name] = dash
			} else {
				out[name] = plugin.TextValue(snap.statusLine(wan, t))
			}
		case "latency":
			if !wan || !snap.loaded || snap.www.Latency == nil {
				out[name] = dash
			} else {
				out[name] = plugin.NumberValue(float64(*snap.www.Latency))
			}
		default:
			return nil, fmt.Errorf("%w %q", plugin.ErrUnknownValue, name)
		}
	}
	return out, nil
}
