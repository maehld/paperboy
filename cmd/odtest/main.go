// Command odtest checks an OpenDisplay ePaper display over Wi-Fi or Bluetooth:
// it prints firmware and panel configuration and optionally sends a test pattern.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/maehld/paperboy/internal/opendisplay"
	"github.com/maehld/paperboy/internal/opendisplay/odble"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run() error {
	host := flag.String("host", "", "IP address or hostname of the display (e.g. 192.168.1.50 or OD1234abcd.local)")
	port := flag.Int("port", opendisplay.DefaultPort, "TCP port of the display")
	bleTarget := flag.String("ble", "", "Connect via Bluetooth instead of Wi-Fi: device name (e.g. OD1234abcd) or BLE address")
	scan := flag.Bool("scan", false, "Scan for OpenDisplay devices via Bluetooth and list them")
	scanTime := flag.Duration("scan-time", 10*time.Second, "Scan duration for -scan or -ble")
	noResponse := flag.Bool("ble-noresponse", false, "BLE: write without response (faster, experimental)")
	send := flag.Bool("send", false, "Send a test pattern (without: only read information)")
	fast := flag.Bool("fast", false, "Fast refresh instead of full refresh")
	chunk := flag.Int("chunk", 1000, "Bytes per data chunk (Wi-Fi 1–4092; limited to the maximum automatically on BLE)")
	pngPath := flag.String("png", "", "Also save the test pattern as a PNG file")
	timeout := flag.Duration("timeout", 10*time.Second, "Timeout for connecting and short commands")
	buttons := flag.Bool("buttons", false, "Continuously poll the status block (buttons/touch) and print changes; stop with Ctrl+C")
	poll := flag.Duration("poll", 500*time.Millisecond, "Poll interval for -buttons")
	flag.Parse()
	if *scan {
		return scanBLE(*scanTime)
	}
	var client *opendisplay.Client
	switch {
	case *bleTarget != "":
		fmt.Println("Searching for", *bleTarget, "via Bluetooth …")
		d, err := odble.Find(*bleTarget, *scanTime)
		if err != nil {
			return err
		}
		fmt.Printf("Found:          %s (%s, %d dBm), connecting …\n", d.Name, d.Address, d.RSSI)
		t, err := odble.Connect(d, odble.Options{WriteWithoutResponse: *noResponse})
		if err != nil {
			return err
		}
		client = opendisplay.NewClient(t, *timeout)
		fmt.Printf("Connected:      at most %d bytes per command\n", t.MaxPayload())
	case *host != "":
		address := net.JoinHostPort(*host, strconv.Itoa(*port))
		fmt.Println("Connecting to", address, "…")
		var err error
		client, err = opendisplay.Dial(address, *timeout)
		if err != nil {
			return fmt.Errorf("connection failed: %w (Wi-Fi enabled on the display? port %d?)", err, *port)
		}
	default:
		return fmt.Errorf("specify -host, -ble or -scan, e.g.: odtest -host 192.168.1.50 [-send] or odtest -scan")
	}
	defer client.Close()
	if *chunk > client.MaxChunk() {
		*chunk = client.MaxChunk()
	}

	if v, err := client.FirmwareVersion(); err != nil {
		fmt.Println("Firmware:       not readable:", err)
	} else {
		fmt.Printf("Firmware:       %d.%d.%d (%s)\n", v.Major, v.Minor, v.Patch, v.SHA)
	}
	cfg, err := client.ReadConfig()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	fmt.Printf("Configuration:  version %d, BLE %v, Wi-Fi %v, encryption %v\n",
		cfg.Version, cfg.CommunicationModes&1 != 0, cfg.CommunicationModes&4 != 0, cfg.EncryptionEnabled)
	if cfg.UnknownPacket >= 0 {
		fmt.Printf("Note:           unknown configuration packet 0x%02x, rest not parsed\n", cfg.UnknownPacket)
	}
	for i, d := range cfg.Displays {
		fmt.Printf("Display %d:      %d × %d pixels (%d × %d mm), %s\n", i, d.Width, d.Height, d.WidthMM, d.HeightMM, d.ColorScheme)
		fmt.Printf("                panel IC 0x%04x, rotation %d, partial update %d, transmission modes %08b\n",
			d.PanelIC, d.Rotation, d.PartialUpdate, d.TransmissionModes)
	}
	if cfg.Model != "" || cfg.Name != "" {
		fmt.Printf("Device:         %s %s\n", cfg.Model, cfg.Name)
	}
	for _, b := range cfg.Buttons {
		where := fmt.Sprintf("status byte %d", b.ByteIndex)
		if b.ByteIndex == opendisplay.ButtonNotPublished || b.ByteIndex > 10 {
			where = "not published"
		}
		fmt.Printf("Button %d:       type %d, %s\n", b.Instance, b.InputType, where)
	}
	for _, t := range cfg.Touches {
		fmt.Printf("Touch %d:        status bytes %d–%d, flags %08b\n", t.Instance, t.StartByte, t.StartByte+4, t.Flags)
	}
	if *buttons {
		return watchStatus(client, cfg, *poll)
	}

	display := cfg.Displays[0]
	img := testPattern(display.Width, display.Height, display.ColorScheme == opendisplay.SchemeGray4)
	if *pngPath != "" {
		if err := savePNG(*pngPath, img); err != nil {
			return err
		}
		fmt.Println("Test pattern saved:", *pngPath)
	}
	if !*send {
		fmt.Println("\nRead only. Use -send to transfer a test pattern.")
		return nil
	}
	pixels, err := opendisplay.Encode(img, display)
	if err != nil {
		return err
	}
	mode := opendisplay.RefreshFull
	if *fast {
		mode = opendisplay.RefreshFast
	}
	fmt.Printf("\nSending %d bytes in chunks of %d bytes …\n", len(pixels), *chunk)
	start := time.Now()
	lastPercent := -1
	err = client.Upload(pixels, *chunk, mode, func(sent, total int) {
		if p := sent * 100 / total; p/10 != lastPercent/10 {
			lastPercent = p
			fmt.Printf("  %3d %%\n", p)
		}
	})
	if err != nil {
		return err
	}
	fmt.Printf("Done after %s: the display should now show the test pattern.\n", time.Since(start).Round(100*time.Millisecond))
	fmt.Println("Expected: border, large square top left, small one top right, bar black → white, checkerboard at the bottom.")
	return nil
}

// watchStatus polls the status block and prints every change, decoded where the
// configuration says which bytes belong to buttons or touch.
func watchStatus(client *opendisplay.Client, cfg opendisplay.Config, interval time.Duration) error {
	fmt.Printf("\nPolling the status block every %s. Press buttons or touch the display now; stop with Ctrl+C.\n", interval)
	fmt.Println("Bytes 0–10 = dynamic area; button = bit 7 pressed, bits 3–6 counter, bits 0–2 ID.")
	var last *opendisplay.Status
	for {
		s, err := client.ReadStatus()
		if err != nil {
			return fmt.Errorf("status query: %w", err)
		}
		if last == nil || s.Raw != last.Raw {
			line := fmt.Sprintf("%s  dyn % x  %.1f °C  %d mV", time.Now().Format("15:04:05.0"), s.Dynamic[:], s.TemperatureC, s.BatteryMV)
			for _, b := range cfg.Buttons {
				if b.ByteIndex > 10 {
					continue
				}
				st := s.Button(b.ByteIndex)
				changed := last == nil || last.Dynamic[b.ByteIndex] != s.Dynamic[b.ByteIndex]
				if changed {
					line += fmt.Sprintf("  | byte %d: button %d %s, counter %d", b.ByteIndex, st.ID, map[bool]string{true: "PRESSED", false: "released"}[st.Pressed], st.Count)
				}
			}
			for _, t := range cfg.Touches {
				if ts, ok := s.Touch(t.StartByte); ok && ts.Contacts != 0 {
					line += fmt.Sprintf("  | touch %d: %d contacts at %d,%d", t.Instance, ts.Contacts, ts.X, ts.Y)
				}
			}
			fmt.Println(line)
			last = &s
		}
		time.Sleep(interval)
	}
}

// scanBLE lists nearby OpenDisplay devices.
func scanBLE(duration time.Duration) error {
	fmt.Printf("Scanning %s for OpenDisplay devices via Bluetooth …\n", duration)
	devices, err := odble.Scan(duration, nil)
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		fmt.Println("No devices found (Bluetooth enabled on the display? Not connected elsewhere?).")
		return nil
	}
	for _, d := range devices {
		fmt.Printf("  %-16s %s  %d dBm\n", d.Name, d.Address, d.RSSI)
	}
	fmt.Println("\nConnect with: odtest -ble <name>")
	return nil
}

// testPattern draws orientation markers, a gray ramp and a fine checkerboard
// in the panel's native orientation.
func testPattern(w, h int, gray bool) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))
	fill := func(r image.Rectangle, level uint8) {
		draw.Draw(img, r, &image.Uniform{color.Gray{level}}, image.Point{}, draw.Src)
	}
	fill(img.Bounds(), 255)
	border := max(2, min(w, h)/120)
	fill(image.Rect(0, 0, w, border), 0)
	fill(image.Rect(0, h-border, w, h), 0)
	fill(image.Rect(0, 0, border, h), 0)
	fill(image.Rect(w-border, 0, w, h), 0)
	unit := min(w, h) / 8
	fill(image.Rect(border*3, border*3, border*3+unit, border*3+unit), 0)         // top left: large
	fill(image.Rect(w-border*3-unit/2, border*3, w-border*3, border*3+unit/2), 0) // top right: small
	levels := []uint8{0, 85, 170, 255}
	if !gray {
		levels = []uint8{0, 255, 0, 255}
	}
	barTop, barBottom := h/2-unit, h/2+unit/2
	barWidth := (w - 2*unit) / len(levels)
	for i, level := range levels {
		x := unit + i*barWidth
		fill(image.Rect(x, barTop, x+barWidth, barBottom), level)
	}
	fill(image.Rect(unit, barTop, unit+barWidth*len(levels), barTop+border), 0)
	fill(image.Rect(unit, barBottom-border, unit+barWidth*len(levels), barBottom), 0)
	cell := 8
	for y := h - unit; y < h-border*3; y += cell {
		for x := unit; x < w-unit; x += cell {
			if (x/cell+y/cell)%2 == 0 {
				fill(image.Rect(x, y, x+cell, y+cell), 0)
			}
		}
	}
	return img
}

func savePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
