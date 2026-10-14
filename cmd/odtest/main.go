// Command odtest checks an OpenDisplay ePaper display over Wi-Fi: it prints
// firmware and panel configuration and optionally sends a test pattern.
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

	"solarai/internal/opendisplay"
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
	send := flag.Bool("send", false, "Testbild senden (ohne: nur Informationen lesen)")
	fast := flag.Bool("fast", false, "Schnelle Aktualisierung statt Vollrefresh")
	chunk := flag.Int("chunk", 1000, "Bytes pro Datenblock (1–4092)")
	pngPath := flag.String("png", "", "Testbild zusätzlich als PNG-Datei speichern")
	timeout := flag.Duration("timeout", 10*time.Second, "Timeout für Verbindung und kurze Befehle")
	flag.Parse()
	if *host == "" {
		return fmt.Errorf("bitte -host angeben, z. B.: odtest -host 192.168.1.50 [-send]")
	}
	address := net.JoinHostPort(*host, strconv.Itoa(*port))
	fmt.Println("Verbinde mit", address, "…")
	client, err := opendisplay.Dial(address, *timeout)
	if err != nil {
		return fmt.Errorf("Verbindung fehlgeschlagen: %w (WLAN am Display aktiv? Port %d?)", err, *port)
	}
	defer client.Close()

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
