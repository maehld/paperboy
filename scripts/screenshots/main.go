// Command screenshots renders the documentation pictures: the example pages
// and every widget of the shipped plugins. It builds Paperboy and the plugins,
// starts a mock of every upstream service and writes the PNGs that the plugin
// documents embed.
//
// Run it from the repository root. The tool belongs to no module, so Go starts
// it from its files:
//
//	go run ./scripts/screenshots/main.go ./scripts/screenshots/mock.go
//	go run ./scripts/screenshots/main.go ./scripts/screenshots/mock.go -only weather
//	go run ./scripts/screenshots/main.go ./scripts/screenshots/mock.go -serve
//
// The -serve flag starts the instance and prints its address on stdout, so
// scripts/ui-shots.mjs can take pictures of the operator pages. The instance
// stops when stdin closes or the process gets a signal.
//
// The mock keeps the pictures stable and needs no credentials. xkcd and the
// Pokémon card show placeholder art, because the repository ships neither the
// comic nor the sprites.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// pageShots maps a plugin to the example pages its documentation shows. A page
// that ends in .lcd2004 is a text page for the 20x4 display.
var pageShots = map[string][]string{
	"weather":    {"wetter"},
	"evcc":       {"energie", "energie-woche", "ertrag"},
	"calendar":   {"kalender", "woche", "geburtstage", "geburtstage.lcd2004"},
	"departures": {"abfahrten", "abfahrten.lcd2004"},
	"pokedex":    {"pokemon"},
	"xkcd":       {"comic"},
	"myskoda":    {"auto", "auto.lcd2004"},
	"countdown":  {"countdown", "countdown.lcd2004"},
	"rss":        {"nachrichten"},
	"dexcom":     {"glukose", "glukose.lcd2004"},
	"fuel":       {"spritpreise"},
	"paperboy":   {"status", "hinweise"},
	"prusalink":  {"drucker", "drucker.lcd2004"},
}

// widgetArgs overrides the arguments of a widget preview where the plugin
// needs one to draw something useful.
var widgetArgs = map[string]map[string]string{
	"countdown/countdown": {"label": "Weihnachten", "date": "12-24", "since": "01-01"},
	"fuel/history":        {"span": "24h"},
	"paperboy/header":     {"title": "Übersicht"},
}

// plugins are the shipped plugins: the directories under plugins/cmd/ with a
// VERSION file. The built-in plugin needs no program.
var plugins = []string{
	"calendar", "countdown", "departures", "dexcom", "evcc", "fuel",
	"myskoda", "pokedex", "prusalink", "rss", "weather", "xkcd",
}

func main() {
	out := flag.String("out", "docs/images/plugins", "output directory for the PNGs")
	only := flag.String("only", "", "render one plugin only")
	keep := flag.Bool("keep", false, "keep the temporary instance")
	bin := flag.String("bin", "", "use built binaries from this directory instead of building")
	serve := flag.Bool("serve", false, "start the instance and wait instead of rendering (for a browser tool)")
	flag.Parse()

	if err := run(*out, *only, *bin, *keep, *serve); err != nil {
		fmt.Fprintln(os.Stderr, "screenshots:", err)
		os.Exit(1)
	}
}

func run(out, only, binDir string, keep, serve bool) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "paperboy-screenshots-")
	if err != nil {
		return err
	}
	if keep {
		fmt.Println("instance:", tmp)
	} else {
		defer os.RemoveAll(tmp)
	}

	instance := filepath.Join(tmp, "instance")
	if err := prepareInstance(root, instance, binDir); err != nil {
		return err
	}
	paperboy, err := build(root, tmp, instance, binDir)
	if err != nil {
		return err
	}

	m := newMock(root)
	mux := http.NewServeMux()
	mux.Handle("/", m)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	upstream := &http.Server{Handler: mux}
	go upstream.Serve(ln)
	defer upstream.Close()
	m.base = "http://" + ln.Addr().String()
	if err := writeConfig(instance, m.base); err != nil {
		return err
	}
	if err := seedState(instance, m.base); err != nil {
		return err
	}

	logFile, err := os.Create(filepath.Join(tmp, "paperboy.log"))
	if err != nil {
		return err
	}
	defer logFile.Close()
	addr := freeAddr()
	cmd := exec.Command(paperboy, "-config", instance, "-listen", addr)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()
	base := "http://" + addr
	if err := waitReady(base, logFile); err != nil {
		return fmt.Errorf("%w (log: %s)", err, filepath.Join(tmp, "paperboy.log"))
	}

	if serve {
		// A browser tool (scripts/ui-shots.mjs) takes the pictures. Print the
		// address, then keep the instance alive until stdin closes or the tool
		// gets a signal.
		fmt.Println("paperboy=" + base)
		if keep {
			fmt.Println("instance=" + instance)
		}
		waitForStop()
		return nil
	}

	if err := render(out, root, base, only); err != nil {
		return fmt.Errorf("%w (log: %s)", err, filepath.Join(tmp, "paperboy.log"))
	}
	return nil
}

// repoRoot walks up until it finds go.work.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.work not found")
		}
		dir = parent
	}
}

// build builds Paperboy and the plugins. With binDir the programs are taken
// from there.
func build(root, tmp, instance, binDir string) (string, error) {
	paperboy := filepath.Join(binDir, "paperboy")
	pluginsDir := filepath.Join(instance, "plugins")
	if binDir == "" {
		paperboy = filepath.Join(tmp, "paperboy")
		if err := goBuild(root, paperboy, "./server/cmd/paperboy"); err != nil {
			return "", err
		}
		for _, name := range plugins {
			if err := goBuild(root, filepath.Join(pluginsDir, name), "./plugins/cmd/"+name); err != nil {
				return "", err
			}
		}
		return paperboy, nil
	}
	abs, err := filepath.Abs(binDir)
	if err != nil {
		return "", err
	}
	for _, name := range plugins {
		if err := copyFile(filepath.Join(abs, name), filepath.Join(pluginsDir, name)); err != nil {
			return "", err
		}
	}
	return filepath.Join(abs, "paperboy"), nil
}

func goBuild(root, target, pkg string) error {
	fmt.Fprintln(os.Stderr, "building", pkg)
	cmd := exec.Command("go", "build", "-o", target, pkg)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go build %s: %v\n%s", pkg, err, out)
	}
	return nil
}

// prepareInstance copies the examples and creates the directories Paperboy
// expects (ADR 0037).
func prepareInstance(root, instance, binDir string) error {
	for _, dir := range []string{"displays", "pages", "plans", "devices"} {
		if err := copyTree(filepath.Join(root, "examples", dir), filepath.Join(instance, dir)); err != nil {
			return err
		}
	}
	return os.MkdirAll(filepath.Join(instance, "plugins"), 0o755)
}

// writeConfig writes paperboy.yaml. Every plugin that reaches a remote service
// points at the mock.
func writeConfig(instance, base string) error {
	config := fmt.Sprintf(`language: de
timezone: Europe/Berlin
plugins:
  weather:
    config: {lat: 52.52, lon: 13.405, base: %[1]q, refresh: 1h}
  evcc:
    config: {url: %[1]q}
  fuel:
    config: {apikey: demo, lat: 52.52, lon: 13.405, radius: 5, base: %[1]q}
  pokedex:
    config: {pokemon: pikachu, base: %[2]q}
  xkcd:
    config: {comic: 927, base: %[1]q}
  countdown: {}
  dexcom:
    config:
      account_name: demo@example.com
      password: demo
      app_id: d89443d2-327c-4a6f-89e5-496bbb0317db
      region: eu
      base: %[1]q
      unit: mg/dL
      refresh: 5m
  departures:
    config:
      base: %[3]q
      stops:
        - {name: Marktplatz, stop: "Demo, Marktplatz"}
        - {name: Hauptbahnhof, stop: "Demo, Hauptbahnhof"}
      walk_minutes: 4
  myskoda:
    config: {api_key: demo, vin: TMBJB9NY5RF999999, base: %[1]q, refresh: 1h}
  prusalink:
    config: {url: %[1]q, api_key: demo, refresh: 1m}
  rss:
    config:
      feeds:
        - {name: Tagesschau, url: %[6]q}
        - {name: Heise, url: %[7]q}
  calendar:
    config:
      caldav: %[4]q
      carddav: %[5]q
      user: demo@example.com
      password: demo
      calendars: [Privat, Arbeit]
defaults:
  refresh: 5m
`, base,
		base+"/api/v2", base+"/efa", base+"/cal", base+"/card",
		base+"/feeds/tagesschau.xml", base+"/feeds/heise.xml")
	return os.WriteFile(filepath.Join(instance, "paperboy.yaml"), []byte(config), 0o644)
}

// seedState fills the fuel history, which grows one point per refresh. The
// picture of the history widget shows a day of changes instead of one point.
func seedState(instance, base string) error {
	dir := filepath.Join(instance, "state", "plugins", "fuel")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	nowUnix := time.Now().Unix()
	series := map[string][]map[string]any{}
	for i, name := range []string{"diesel", "e5", "e10"} {
		var points []map[string]any
		price := 1.789 + float64(i)*0.15
		for h := 48; h >= 0; h-- {
			if h%3 != 0 {
				continue
			}
			price += float64((h*7+i*3)%5-2) * 0.004
			points = append(points, map[string]any{"at": nowUnix - int64(h)*3600, "price": round3(price)})
		}
		series[name] = points
	}
	state := map[string]any{"version": 1, "key": fuelKey(), "series": series}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0o644); err != nil {
		return err
	}

	// Device state makes the built-in device widgets show rows instead of
	// "?" (ADR 0115). LastSeen also keeps the plan demand alive.
	devices := filepath.Join(instance, "state", "devices")
	if err := os.MkdirAll(devices, 0o755); err != nil {
		return err
	}
	now := time.Now()
	for mac, extra := range map[string]map[string]any{
		"AABBCCDDEE01": {"hardware": "e1001", "firmware": "v1.4.2", "battery_voltage": "3.89",
			"battery_percent": 76.0, "battery_mah": 3050.0, "battery_ma": -38.0, "rssi": "-67"},
		"AABBCCDDEE02": {"hardware": "lcd2004", "firmware": "v1.4.2", "rssi": "-54",
			"reported_power": "mains"},
	} {
		st := map[string]any{
			"api_key": "demo", "first_seen": now.AddDate(0, -2, 0),
			"last_seen": now.Add(-90 * time.Second), "last_refresh": 600,
			"rule": "tag", "page": 0,
		}
		for k, v := range extra {
			st[k] = v
		}
		data, err := json.Marshal(st)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(devices, mac+".json"), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// fuelKey is the fingerprint of the demo location and key, as the plugin
// computes it.
func fuelKey() string {
	sum := sha256Sum(fmt.Sprintf("%s|%.5f|%.5f|%.2f", "demo", 52.52, 13.405, 5.0))
	return sum
}

// render fetches every page and widget that the docs show.
func render(out, root, base, only string) error {
	client := &http.Client{Timeout: 2 * time.Minute}
	for plugin, pages := range pageShots {
		if only != "" && plugin != only {
			continue
		}
		dir := filepath.Join(out, plugin)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		for _, page := range pages {
			display, device := "e1001", "wohnzimmer"
			file := page
			text := strings.HasSuffix(page, ".lcd2004") || page == "hinweise"
			if strings.HasSuffix(page, ".lcd2004") {
				display, device = "lcd2004", "lcd"
				file = strings.TrimSuffix(page, ".lcd2004")
			}
			if page == "hinweise" {
				display, device = "lcd2004", "lcd"
			}
			name := "page-" + file
			if text {
				name += "-lcd2004"
			}
			u := fmt.Sprintf("%s/api/preview/%s/%s?device=%s", base, display, file, device)
			data, err := getBody(client, u)
			if err != nil {
				return fmt.Errorf("%s: %w", page, err)
			}
			if text {
				// A text display answers its lines, not a PNG. The docs show
				// them as a code block that keeps the characters exact.
				if err := os.WriteFile(filepath.Join(dir, name+".txt"), data, 0o644); err != nil {
					return err
				}
				fmt.Println("text", plugin+"/"+name)
				continue
			}
			if !bytes.HasPrefix(data, []byte("\x89PNG")) {
				return fmt.Errorf("%s: not a PNG", page)
			}
			if err := os.WriteFile(filepath.Join(dir, name+".png"), data, 0o644); err != nil {
				return err
			}
			fmt.Println("page", plugin+"/"+name)
		}
	}
	widgets, err := describe(client, base)
	if err != nil {
		return err
	}
	for _, p := range widgets {
		if only != "" && p.Name != only {
			continue
		}
		dir := filepath.Join(out, p.Name)
		for _, w := range p.Description.Widgets {
			span := "3x2"
			best := 0
			for _, s := range w.Sizes {
				if area := s.Columns * s.Rows; area > best {
					best = area
					span = fmt.Sprintf("%dx%d", s.Columns, s.Rows)
				}
			}
			q := url.Values{"display": {"e1001"}, "span": {span}, "device": {"wohnzimmer"}}
			for k, v := range widgetArgs[p.Name+"/"+w.Widget] {
				q.Set("arg."+k, v)
			}
			u := fmt.Sprintf("%s/api/widget/%s/%s?%s", base, p.Name, w.Widget, q.Encode())
			data, err := getBody(client, u)
			if err != nil {
				return fmt.Errorf("%s/%s: %w", p.Name, w.Widget, err)
			}
			if err := os.WriteFile(filepath.Join(dir, w.Widget+".png"), data, 0o644); err != nil {
				return err
			}
			fmt.Println("widget", p.Name+"/"+w.Widget)
		}
	}
	return nil
}

type pluginList struct {
	Plugins []pluginInfo `json:"plugins"`
}

type pluginInfo struct {
	Name        string `json:"name"`
	Error       string `json:"error"`
	Description *struct {
		Widgets []widgetInfo `json:"widgets"`
	} `json:"description"`
}

type widgetInfo struct {
	Widget string `json:"widget"`
	Sizes  []struct {
		Columns int `json:"columns"`
		Rows    int `json:"rows"`
	} `json:"sizes"`
}

// describe reads GET /api/plugins, which also starts every plugin once.
func describe(client *http.Client, base string) ([]pluginInfo, error) {
	var body pluginList
	resp, err := client.Get(base + "/api/plugins")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /api/plugins: %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	var out []pluginInfo
	for _, p := range body.Plugins {
		if p.Error != "" || p.Description == nil {
			fmt.Fprintf(os.Stderr, "plugin %s: %s\n", p.Name, p.Error)
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// getBody fetches a preview and reports a widget error header.
func getBody(client *http.Client, u string) ([]byte, error) {
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s: %s", u, resp.Status, strings.TrimSpace(string(data)))
	}
	if msg := resp.Header.Get("X-Widget-Error"); msg != "" {
		fmt.Fprintf(os.Stderr, "warning: %s: %s\n", u, msg)
	}
	return data, nil
}

// waitForStop blocks until stdin closes or the process gets a signal, so a
// caller can start the instance, take its pictures and stop it again.
func waitForStop() {
	done := make(chan struct{})
	go func() {
		io.Copy(io.Discard, os.Stdin)
		close(done)
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	select {
	case <-done:
	case <-signals:
	}
}

// waitReady waits until the server answers the plugin list. That request also
// starts every plugin, so the first page render has its data.
func waitReady(base string, log *os.File) error {
	client := &http.Client{Timeout: 3 * time.Minute}
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(base + "/api/plugins")
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	log.Seek(0, io.SeekStart)
	sc := bufio.NewScanner(io.LimitReader(log, 4000))
	var tail []string
	for sc.Scan() {
		tail = append(tail, sc.Text())
	}
	return fmt.Errorf("paperboy did not answer in time: %s", strings.Join(tail, " | "))
}

// freeAddr takes a free loopback port.
func freeAddr() string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "127.0.0.1:8090"
	}
	defer ln.Close()
	return ln.Addr().String()
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}
