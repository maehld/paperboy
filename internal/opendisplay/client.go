// Package opendisplay implements the sender side of the OpenDisplay protocol.
// The Wi-Fi LAN transport (TCP, length-prefixed frames) is built in; other
// transports such as BLE plug in via Transport. Encryption is not supported;
// it must be disabled on the device.
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
var ErrAuthRequired = errors.New("device requires authentication; disable encryption on the display")

// Transport carries command payloads to the device and returns its responses
// one payload at a time (a TCP frame or a BLE notification).
type Transport interface {
	WriteFrame(payload []byte) error
	ReadFrame(timeout time.Duration) ([]byte, error)
	// MaxPayload is the largest payload a single WriteFrame accepts.
	MaxPayload() int
	Close() error
}

// Client is a single plaintext session. The device serves one client at a time.
type Client struct {
	t       Transport
	Timeout time.Duration // per-response timeout for quick commands
}

func NewClient(t Transport, timeout time.Duration) *Client {
	return &Client{t: t, Timeout: timeout}
}

// Dial connects over the Wi-Fi LAN transport.
func Dial(address string, timeout time.Duration) (*Client, error) {
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return nil, err
	}
	return NewClient(&tcpTransport{conn: conn, writeTimeout: timeout}, timeout), nil
}

func (c *Client) Close() error { return c.t.Close() }

// MaxChunk is the largest pixel chunk Upload can send in one data command.
func (c *Client) MaxChunk() int { return c.t.MaxPayload() - 2 }

func (c *Client) writeFrame(payload []byte) error { return c.t.WriteFrame(payload) }

func (c *Client) readFrame(timeout time.Duration) ([]byte, error) { return c.t.ReadFrame(timeout) }

// tcpTransport frames payloads as [length:2 LE][payload].
type tcpTransport struct {
	conn         net.Conn
	writeTimeout time.Duration
}

func (t *tcpTransport) MaxPayload() int { return maxPayload }

func (t *tcpTransport) Close() error { return t.conn.Close() }

func (t *tcpTransport) WriteFrame(payload []byte) error {
	if len(payload) == 0 || len(payload) > maxPayload {
		return fmt.Errorf("invalid frame length %d", len(payload))
	}
	frame := make([]byte, 2+len(payload))
	binary.LittleEndian.PutUint16(frame, uint16(len(payload)))
	copy(frame[2:], payload)
	t.conn.SetWriteDeadline(time.Now().Add(t.writeTimeout))
	_, err := t.conn.Write(frame)
	return err
}

func (t *tcpTransport) ReadFrame(timeout time.Duration) ([]byte, error) {
	t.conn.SetReadDeadline(time.Now().Add(timeout))
	var header [2]byte
	if _, err := io.ReadFull(t.conn, header[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint16(header[:])
	if n == 0 || n > maxPayload {
		t.conn.Close()
		return nil, fmt.Errorf("invalid frame length %d from device", n)
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(t.conn, payload); err != nil {
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
		return 0, false, nil, fmt.Errorf("response too short: % x", frame)
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
			return nil, fmt.Errorf("device reports error for command 0x%04x: % x", cmd, frame)
		}
		return body, nil
	}
	return nil, fmt.Errorf("no matching response to command 0x%04x", cmd)
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
		return FirmwareVersion{}, fmt.Errorf("incomplete firmware response: % x", body)
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
		return Config{}, fmt.Errorf("configuration: first chunk too short")
	}
	total := int(binary.LittleEndian.Uint16(body[2:4]))
	data := append([]byte(nil), body[4:]...)
	for len(data) < total {
		body, err := c.expect(cmdReadConfig, 2*time.Second)
		if err != nil {
			return Config{}, fmt.Errorf("configuration aborted at %d/%d bytes: %w", len(data), total, err)
		}
		if len(body) <= 2 {
			return Config{}, fmt.Errorf("configuration: empty chunk at %d/%d bytes", len(data), total)
		}
		data = append(data, body[2:]...)
	}
	return ParseConfig(data)
}

// Upload streams an encoded frame (see Encode) via uncompressed direct write
// and waits until the panel has refreshed.
func (c *Client) Upload(pixels []byte, chunk int, mode RefreshMode, progress func(sent, total int)) error {
	if chunk < 1 || chunk > c.MaxChunk() {
		return fmt.Errorf("invalid chunk size %d", chunk)
	}
	if err := c.send(cmdWriteStart, nil); err != nil {
		return err
	}
	if _, err := c.expect(cmdWriteStart, 10*time.Second); err != nil {
		return fmt.Errorf("start: %w", err)
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
			return fmt.Errorf("data at %d bytes: %w", sent, err)
		}
		code, nack, _, err := response(frame)
		if err != nil {
			return err
		}
		if nack {
			return fmt.Errorf("device rejects data at %d bytes: % x", sent, frame)
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
			return fmt.Errorf("unexpected response during transfer: % x", frame)
		}
	}
	if !autoCompleted {
		if err := c.send(cmdWriteEnd, []byte{byte(mode)}); err != nil {
			return err
		}
		if _, err := c.expect(cmdWriteEnd, 90*time.Second); err != nil {
			return fmt.Errorf("end: %w", err)
		}
	}
	frame, err := c.readFrame(90 * time.Second)
	if err != nil {
		return fmt.Errorf("waiting for display: %w", err)
	}
	switch code, _, _, _ := response(frame); code {
	case cmdRefreshDone:
		return nil
	case cmdRefreshTimeout:
		return errors.New("panel reports timeout while refreshing")
	default:
		return fmt.Errorf("unexpected response after sending: % x", frame)
	}
}
