// Package opendisplay implements the sender side of the OpenDisplay protocol
// over the Wi-Fi LAN transport (TCP, length-prefixed frames). Encryption is not
// supported; it must be disabled on the device.
package opendisplay

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

const (
	DefaultPort = 2446
	maxPayload  = 4094 // 4096-byte wire frame minus the 2-byte length prefix
	ackFlag     = 0x8000

	cmdReadConfig      = 0x0040
	cmdFirmwareVersion = 0x0043
	cmdWriteStart      = 0x0070
	cmdWriteData       = 0x0071
	cmdWriteEnd        = 0x0072
	cmdRefreshDone     = 0x0073
	cmdRefreshTimeout  = 0x0074
)

// RefreshMode selects the panel waveform after an upload.
type RefreshMode byte

const (
	RefreshFull RefreshMode = 0
	RefreshFast RefreshMode = 1
)

// ErrAuthRequired is returned when the device has encryption enabled.
var ErrAuthRequired = errors.New("Gerät verlangt Authentifizierung; Verschlüsselung am Display deaktivieren")

// Client is a single plaintext session. The device serves one client at a time.
type Client struct {
	conn    net.Conn
	Timeout time.Duration // per-response timeout for quick commands
}

func Dial(address string, timeout time.Duration) (*Client, error) {
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, Timeout: timeout}, nil
}

func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) writeFrame(payload []byte) error {
	if len(payload) == 0 || len(payload) > maxPayload {
		return fmt.Errorf("ungültige Rahmenlänge %d", len(payload))
	}
	frame := make([]byte, 2+len(payload))
	binary.LittleEndian.PutUint16(frame, uint16(len(payload)))
	copy(frame[2:], payload)
	c.conn.SetWriteDeadline(time.Now().Add(c.Timeout))
	_, err := c.conn.Write(frame)
	return err
}

func (c *Client) readFrame(timeout time.Duration) ([]byte, error) {
	c.conn.SetReadDeadline(time.Now().Add(timeout))
	var header [2]byte
	if _, err := io.ReadFull(c.conn, header[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint16(header[:])
	if n == 0 || n > maxPayload {
		c.conn.Close()
		return nil, fmt.Errorf("ungültige Rahmenlänge %d vom Gerät", n)
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(c.conn, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (c *Client) send(cmd uint16, payload []byte) error {
	frame := make([]byte, 2, 2+len(payload))
	binary.BigEndian.PutUint16(frame, cmd)
	return c.writeFrame(append(frame, payload...))
}

// response classifies a device frame: the echoed command (ACK flag removed),
// whether it is an error frame, and the bytes after the 2-byte code.
func response(frame []byte) (code uint16, nack bool, body []byte, err error) {
	if len(frame) < 2 {
		return 0, false, nil, fmt.Errorf("Antwort zu kurz: % x", frame)
	}
	if frame[0] == 0xff {
		return uint16(frame[1]), true, frame[2:], nil
	}
	code = binary.BigEndian.Uint16(frame) &^ ackFlag
	body = frame[2:]
	// With encryption enabled, rejected commands answer with status 0xFE.
	if len(body) == 1 && body[0] == 0xfe {
		return code, true, body, ErrAuthRequired
	}
	return code, false, body, nil
}

// expect reads frames until one answers cmd, skipping a few stray frames.
func (c *Client) expect(cmd uint16, timeout time.Duration) ([]byte, error) {
	for range 4 {
		frame, err := c.readFrame(timeout)
		if err != nil {
			return nil, err
		}
		code, nack, body, err := response(frame)
		if err != nil {
			return nil, err
		}
		if code != cmd&0xff && code != cmd {
			continue
		}
		if nack {
			return nil, fmt.Errorf("Gerät meldet Fehler für Befehl 0x%04x: % x", cmd, frame)
		}
		return body, nil
	}
	return nil, fmt.Errorf("keine passende Antwort auf Befehl 0x%04x", cmd)
}

type FirmwareVersion struct {
	Major, Minor, Patch byte
	SHA                 string
}

func (c *Client) FirmwareVersion() (FirmwareVersion, error) {
	if err := c.send(cmdFirmwareVersion, nil); err != nil {
		return FirmwareVersion{}, err
	}
	body, err := c.expect(cmdFirmwareVersion, c.Timeout)
	if err != nil {
		return FirmwareVersion{}, err
	}
	if len(body) < 3 || len(body) < 3+int(body[2]) {
		return FirmwareVersion{}, fmt.Errorf("Firmware-Antwort unvollständig: % x", body)
	}
	v := FirmwareVersion{Major: body[0], Minor: body[1], SHA: string(body[3 : 3+int(body[2])])}
	if len(body) > 3+int(body[2]) {
		v.Patch = body[3+int(body[2])]
	}
	return v, nil
}

// ReadConfig reads and parses the stored Flex configuration.
func (c *Client) ReadConfig() (Config, error) {
	if err := c.send(cmdReadConfig, nil); err != nil {
		return Config{}, err
	}
	// First chunk: [chunk:2][total:2][data]; later chunks: [chunk:2][data].
	body, err := c.expect(cmdReadConfig, 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	if len(body) < 4 {
		return Config{}, fmt.Errorf("Konfiguration: erster Block zu kurz")
	}
	total := int(binary.LittleEndian.Uint16(body[2:4]))
	data := append([]byte(nil), body[4:]...)
	for len(data) < total {
		body, err := c.expect(cmdReadConfig, 2*time.Second)
		if err != nil {
			return Config{}, fmt.Errorf("Konfiguration abgebrochen bei %d/%d Byte: %w", len(data), total, err)
		}
		if len(body) <= 2 {
			return Config{}, fmt.Errorf("Konfiguration: leerer Block bei %d/%d Byte", len(data), total)
		}
		data = append(data, body[2:]...)
	}
	return ParseConfig(data)
}

// Upload streams an encoded frame (see Encode) via uncompressed direct write
// and waits until the panel has refreshed.
func (c *Client) Upload(pixels []byte, chunk int, mode RefreshMode, progress func(sent, total int)) error {
	if chunk < 1 || chunk > maxPayload-2 {
		return fmt.Errorf("ungültige Blockgröße %d", chunk)
	}
	if err := c.send(cmdWriteStart, nil); err != nil {
		return err
	}
	if _, err := c.expect(cmdWriteStart, 10*time.Second); err != nil {
		return fmt.Errorf("Start: %w", err)
	}
	autoCompleted := false
	for sent := 0; sent < len(pixels); {
		end := min(sent+chunk, len(pixels))
		if err := c.send(cmdWriteData, pixels[sent:end]); err != nil {
			return err
		}
		sent = end
		frame, err := c.readFrame(90 * time.Second)
		if err != nil {
			return fmt.Errorf("Daten bei %d Byte: %w", sent, err)
		}
		code, nack, _, err := response(frame)
		if err != nil {
			return err
		}
		if nack {
			return fmt.Errorf("Gerät lehnt Daten bei %d Byte ab: % x", sent, frame)
		}
		if progress != nil {
			progress(sent, len(pixels))
		}
		// Firmware may refresh on its own once the expected byte count arrived.
		if code == cmdWriteEnd {
			autoCompleted = true
			break
		}
		if code != cmdWriteData {
			return fmt.Errorf("unerwartete Antwort während der Übertragung: % x", frame)
		}
	}
	if !autoCompleted {
		if err := c.send(cmdWriteEnd, []byte{byte(mode)}); err != nil {
			return err
		}
		if _, err := c.expect(cmdWriteEnd, 90*time.Second); err != nil {
			return fmt.Errorf("Ende: %w", err)
		}
	}
	frame, err := c.readFrame(90 * time.Second)
	if err != nil {
		return fmt.Errorf("warte auf Anzeige: %w", err)
	}
	switch code, _, _, _ := response(frame); code {
	case cmdRefreshDone:
		return nil
	case cmdRefreshTimeout:
		return errors.New("Panel meldet Zeitüberschreitung beim Aktualisieren")
	default:
		return fmt.Errorf("unerwartete Antwort nach dem Senden: % x", frame)
	}
}
