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
		fmt.Fprintln(os.Stderr, "Fehler:", err)
		os.Exit(1)
	}
}

func run() error {
	host := flag.String("host", "", "IP-Adresse oder Hostname des Displays (z. B. 192.168.1.50 oder OD1234abcd.local)")
	port := flag.Int("port", opendisplay.DefaultPort, "TCP-Port des Displays")
	bleTarget := flag.String("ble", "", "Statt WLAN per Bluetooth verbinden: Gerätename (z. B. OD1234abcd) oder BLE-Adresse")
	scan := flag.Bool("scan", false, "Nach OpenDisplay-Geräten per Bluetooth suchen und auflisten")
	scanTime := flag.Duration("scan-time", 10*time.Second, "Suchdauer für -scan bzw. -ble")
	noResponse := flag.Bool("ble-noresponse", false, "BLE: Schreiben ohne Bestätigung (schneller, experimentell)")
	send := flag.Bool("send", false, "Testbild senden (ohne: nur Informationen lesen)")
	fast := flag.Bool("fast", false, "Schnelle Aktualisierung statt Vollrefresh")
	chunk := flag.Int("chunk", 1000, "Bytes pro Datenblock (WLAN 1–4092; bei BLE automatisch auf das Maximum begrenzt)")
	pngPath := flag.String("png", "", "Testbild zusätzlich als PNG-Datei speichern")
	timeout := flag.Duration("timeout", 10*time.Second, "Timeout für Verbindung und kurze Befehle")
	buttons := flag.Bool("buttons", false, "Statusblock (Tasten/Touch) fortlaufend abfragen und Änderungen ausgeben; Ende mit Strg+C")
	poll := flag.Duration("poll", 500*time.Millisecond, "Abfrageabstand für -buttons")
	flag.Parse()
	if *scan {
		return scanBLE(*scanTime)
	}
	var client *opendisplay.Client
	switch {
	case *bleTarget != "":
		fmt.Println("Suche", *bleTarget, "per Bluetooth …")
		d, err := odble.Find(*bleTarget, *scanTime)
		if err != nil {
			return err
		}
		fmt.Printf("Gefunden:       %s (%s, %d dBm), verbinde …\n", d.Name, d.Address, d.RSSI)
		t, err := odble.Connect(d, odble.Options{WriteWithoutResponse: *noResponse})
		if err != nil {
			return err
		}
		client = opendisplay.NewClient(t, *timeout)
		fmt.Printf("Verbunden:      höchstens %d Byte pro Befehl\n", t.MaxPayload())
	case *host != "":
		address := net.JoinHostPort(*host, strconv.Itoa(*port))
		fmt.Println("Verbinde mit", address, "…")
		var err error
		client, err = opendisplay.Dial(address, *timeout)
		if err != nil {
			return fmt.Errorf("Verbindung fehlgeschlagen: %w (WLAN am Display aktiv? Port %d?)", err, *port)
		}
	default:
		return fmt.Errorf("bitte -host, -ble oder -scan angeben, z. B.: odtest -host 192.168.1.50 [-send] oder odtest -scan")
	}
	defer client.Close()
	if *chunk > client.MaxChunk() {
		*chunk = client.MaxChunk()
	}

	if v, err := client.FirmwareVersion(); err != nil {
		fmt.Println("Firmware:       nicht lesbar:", err)
	} else {
		fmt.Printf("Firmware:       %d.%d.%d (%s)\n", v.Major, v.Minor, v.Patch, v.SHA)
	}
	cfg, err := client.ReadConfig()
	if err != nil {
		return fmt.Errorf("Konfiguration: %w", err)
	}
	fmt.Printf("Konfiguration:  Version %d, BLE %v, WLAN %v, Verschlüsselung %v\n",
		cfg.Version, cfg.CommunicationModes&1 != 0, cfg.CommunicationModes&4 != 0, cfg.EncryptionEnabled)
	if cfg.UnknownPacket >= 0 {
		fmt.Printf("Hinweis:        unbekanntes Konfigurationspaket 0x%02x, Rest nicht ausgewertet\n", cfg.UnknownPacket)
	}
	for i, d := range cfg.Displays {
		fmt.Printf("Display %d:      %d × %d Pixel (%d × %d mm), %s\n", i, d.Width, d.Height, d.WidthMM, d.HeightMM, d.ColorScheme)
		fmt.Printf("                Panel-IC 0x%04x, Rotation %d, Teilaktualisierung %d, Übertragungsmodi %08b\n",
			d.PanelIC, d.Rotation, d.PartialUpdate, d.TransmissionModes)
	}
	if cfg.Model != "" || cfg.Name != "" {
		fmt.Printf("Gerät:          %s %s\n", cfg.Model, cfg.Name)
	}
	for _, b := range cfg.Buttons {
		where := fmt.Sprintf("Statusbyte %d", b.ByteIndex)
		if b.ByteIndex == opendisplay.ButtonNotPublished || b.ByteIndex > 10 {
			where = "nicht veröffentlicht"
		}
		fmt.Printf("Tasten %d:       Typ %d, %s\n", b.Instance, b.InputType, where)
	}
	for _, t := range cfg.Touches {
		fmt.Printf("Touch %d:        Statusbytes %d–%d, Flags %08b\n", t.Instance, t.StartByte, t.StartByte+4, t.Flags)
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
		fmt.Println("Testbild gespeichert:", *pngPath)
	}
	if !*send {
		fmt.Println("\nNur gelesen. Mit -send wird ein Testbild übertragen.")
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
	fmt.Printf("\nSende %d Byte in Blöcken zu %d Byte …\n", len(pixels), *chunk)
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
	fmt.Printf("Fertig nach %s: Das Display sollte jetzt das Testbild zeigen.\n", time.Since(start).Round(100*time.Millisecond))
	fmt.Println("Erwartung: Rahmen, großes Quadrat oben links, kleines oben rechts, Balken Schwarz → Weiß, Schachbrett unten.")
	return nil
}

// watchStatus polls the status block and prints every change, decoded where the
// configuration says which bytes belong to buttons or touch.
func watchStatus(client *opendisplay.Client, cfg opendisplay.Config, interval time.Duration) error {
	fmt.Printf("\nFrage Statusblock alle %s ab. Jetzt Tasten drücken bzw. Display berühren; Ende mit Strg+C.\n", interval)
	fmt.Println("Bytes 0–10 = dynamischer Bereich; Taste = Bit 7 gedrückt, Bits 3–6 Zähler, Bits 0–2 ID.")
	var last *opendisplay.Status
	for {
		s, err := client.ReadStatus()
		if err != nil {
			return fmt.Errorf("Statusabfrage: %w", err)
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
					line += fmt.Sprintf("  | Byte %d: Taste %d %s, Zähler %d", b.ByteIndex, st.ID, map[bool]string{true: "GEDRÜCKT", false: "los"}[st.Pressed], st.Count)
				}
			}
			for _, t := range cfg.Touches {
				if ts, ok := s.Touch(t.StartByte); ok && ts.Contacts != 0 {
					line += fmt.Sprintf("  | Touch %d: Kontakte %d bei %d,%d", t.Instance, ts.Contacts, ts.X, ts.Y)
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
	fmt.Printf("Suche %s nach OpenDisplay-Geräten per Bluetooth …\n", duration)
	devices, err := odble.Scan(duration, nil)
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		fmt.Println("Keine Geräte gefunden (Bluetooth am Display aktiv? Nicht anderweitig verbunden?).")
		return nil
	}
	for _, d := range devices {
		fmt.Printf("  %-16s %s  %d dBm\n", d.Name, d.Address, d.RSSI)
	}
	fmt.Println("\nVerbinden mit: odtest -ble <Name>")
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
