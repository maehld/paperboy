package opendisplay

import (
	"encoding/binary"
	"fmt"
)

const cmdReadStatus = 0x0044

// Status is the 16-byte manufacturer-specific data block, which BLE devices
// advertise and which can be read on demand over LAN.
type Status struct {
	Raw                 [16]byte
	Dynamic             [11]byte // shared by buttons, touch and sensors per configuration
	TemperatureC        float64
	BatteryMV           int
	Reboot, WantConnect bool
	Loop                byte
}

// ButtonState is one dynamic byte decoded as button report.
type ButtonState struct {
	ID, Count byte // Count is a 4-bit press counter that wraps around
	Pressed   bool
}

// TouchState is a 5-byte touch block.
type TouchState struct {
	Contacts, TrackID byte // Contacts 1–5 touching, 6 released
	X, Y              int
}

func (c *Client) ReadStatus() (Status, error) {
	if err := c.send(cmdReadStatus, nil); err != nil {
		return Status{}, err
	}
	body, err := c.expect(cmdReadStatus, c.Timeout)
	if err != nil {
		return Status{}, err
	}
	return ParseStatus(body)
}

// ParseStatus decodes [company:2][dynamic:11][temp:1][battery:1][flags:1].
func ParseStatus(b []byte) (Status, error) {
	if len(b) != 16 {
		return Status{}, fmt.Errorf("status block has %d instead of 16 bytes", len(b))
	}
	s := Status{}
	copy(s.Raw[:], b)
	copy(s.Dynamic[:], b[2:13])
	s.TemperatureC = float64(b[13])/2 - 40
	s.BatteryMV = (int(b[14]) | int(b[15]&1)<<8) * 10
	s.Reboot = b[15]&2 != 0
	s.WantConnect = b[15]&4 != 0
	s.Loop = b[15] >> 4
	return s, nil
}

func (s Status) Button(index byte) ButtonState {
	v := s.Dynamic[index%11]
	return ButtonState{ID: v & 7, Count: (v >> 3) & 15, Pressed: v&0x80 != 0}
}

func (s Status) Touch(start byte) (TouchState, bool) {
	if start > 6 {
		return TouchState{}, false
	}
	d := s.Dynamic[start:]
	return TouchState{Contacts: d[0] & 15, TrackID: d[0] >> 4,
		X: int(binary.LittleEndian.Uint16(d[1:])), Y: int(binary.LittleEndian.Uint16(d[3:]))}, true
}
