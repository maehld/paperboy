// Package odble provides the Bluetooth Low Energy transport for OpenDisplay:
// every command is one write to the GATT characteristic 0x2446, every response
// one notification on it. It lives in its own package so that only programs
// using BLE pull in the Bluetooth dependency.
package odble

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"tinygo.org/x/bluetooth"

	"solarai/internal/opendisplay"
)

var uuid = bluetooth.New16BitUUID(0x2446)

// Device is a display found while scanning.
type Device struct {
	Name    string
	Address bluetooth.Address
	RSSI    int16
}

var (
	enableOnce sync.Once
	enableErr  error
)

func adapter() (*bluetooth.Adapter, error) {
	enableOnce.Do(func() { enableErr = bluetooth.DefaultAdapter.Enable() })
	if enableErr != nil {
		return nil, fmt.Errorf("Bluetooth-Adapter nicht verfügbar: %w", enableErr)
	}
	return bluetooth.DefaultAdapter, nil
}

// Scan lists OpenDisplay devices (name prefix "OD" or service 0x2446) seen
// within duration. If match is non-nil, scanning stops at the first device for
// which it returns true.
func Scan(duration time.Duration, match func(Device) bool) ([]Device, error) {
	a, err := adapter()
	if err != nil {
		return nil, err
	}
	var (
		mu    sync.Mutex
		found []Device
		seen  = map[string]bool{}
	)
	timer := time.AfterFunc(duration, func() { a.StopScan() })
	defer timer.Stop()
	err = a.Scan(func(a *bluetooth.Adapter, r bluetooth.ScanResult) {
		name := r.LocalName()
		if !strings.HasPrefix(name, "OD") && !r.HasServiceUUID(uuid) {
			return
		}
		d := Device{Name: name, Address: r.Address, RSSI: r.RSSI}
		mu.Lock()
		defer mu.Unlock()
		if seen[d.Address.String()] {
			return
		}
		seen[d.Address.String()] = true
		found = append(found, d)
		if match != nil && match(d) {
			a.StopScan()
		}
	})
	mu.Lock()
	defer mu.Unlock()
	return found, err
}

// Find scans until a device whose name or address equals target (case
// insensitive) appears.
func Find(target string, timeout time.Duration) (Device, error) {
	var hit *Device
	_, err := Scan(timeout, func(d Device) bool {
		if strings.EqualFold(d.Name, target) || strings.EqualFold(d.Address.String(), target) {
			hit = &d
			return true
		}
		return false
	})
	if err != nil {
		return Device{}, err
	}
	if hit == nil {
		return Device{}, fmt.Errorf("Display %q nicht innerhalb von %s gefunden", target, timeout)
	}
	return *hit, nil
}

// Transport is a connected GATT session implementing opendisplay.Transport.
type Transport struct {
	dev        bluetooth.Device
	char       bluetooth.DeviceCharacteristic
	maxPayload int
	noResponse bool
	rx         chan []byte
}

// Options tune the GATT session.
type Options struct {
	// WriteWithoutResponse speeds up uploads but gives no link-layer
	// acknowledgement; the protocol ACK per command still applies.
	WriteWithoutResponse bool
}

// Connect opens a GATT session and enables notifications.
func Connect(d Device, opt Options) (*Transport, error) {
	a, err := adapter()
	if err != nil {
		return nil, err
	}
	dev, err := a.Connect(d.Address, bluetooth.ConnectionParams{})
	if err != nil {
		return nil, fmt.Errorf("Verbindung: %w", err)
	}
	t := &Transport{dev: dev, noResponse: opt.WriteWithoutResponse, rx: make(chan []byte, 64)}
	if err := t.setup(); err != nil {
		dev.Disconnect()
		return nil, err
	}
	return t, nil
}

func (t *Transport) setup() error {
	services, err := t.dev.DiscoverServices([]bluetooth.UUID{uuid})
	if err != nil || len(services) == 0 {
		return fmt.Errorf("OpenDisplay-Dienst 0x2446 nicht gefunden: %v", err)
	}
	chars, err := services[0].DiscoverCharacteristics([]bluetooth.UUID{uuid})
	if err != nil || len(chars) == 0 {
		return fmt.Errorf("OpenDisplay-Characteristic 0x2446 nicht gefunden: %v", err)
	}
	t.char = chars[0]
	// ATT payload = MTU - 3; without a negotiated MTU fall back to the minimum.
	t.maxPayload = 20
	if mtu, err := t.char.GetMTU(); err == nil && mtu > 23 {
		t.maxPayload = min(int(mtu)-3, 512)
	}
	err = t.char.EnableNotifications(func(buf []byte) {
		frame := append([]byte(nil), buf...)
		select {
		case t.rx <- frame:
		default: // reader is gone or far behind; dropping beats blocking the BLE stack
		}
	})
	if err != nil {
		return fmt.Errorf("Benachrichtigungen aktivieren: %w", err)
	}
	return nil
}

func (t *Transport) MaxPayload() int { return t.maxPayload }

func (t *Transport) WriteFrame(payload []byte) error {
	if len(payload) == 0 || len(payload) > t.maxPayload {
		return fmt.Errorf("ungültige Rahmenlänge %d (BLE höchstens %d)", len(payload), t.maxPayload)
	}
	var err error
	if t.noResponse {
		_, err = t.char.WriteWithoutResponse(payload)
	} else {
		_, err = t.char.Write(payload)
	}
	return err
}

var errTimeout = errors.New("Zeitüberschreitung beim Warten auf BLE-Antwort")

func (t *Transport) ReadFrame(timeout time.Duration) ([]byte, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case frame := <-t.rx:
		return frame, nil
	case <-timer.C:
		return nil, errTimeout
	}
}

func (t *Transport) Close() error { return t.dev.Disconnect() }

var _ opendisplay.Transport = (*Transport)(nil)
