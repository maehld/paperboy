package opendisplay

import (
	"encoding/binary"
	"testing"
)

func TestReadStatusDecodesButtonsAndTouch(t *testing.T) {
	msd := []byte{0x46, 0x24,
		0x80 | 5<<3 | 2,              // byte 0: button 2 pressed, counter 5
		0x02, 0x40, 0x01, 0x20, 0x00, // bytes 1–5: touch, 2 contacts at 320,32
		0, 0, 0, 0, 0,
		(25 + 40) * 2, 0x2c, 0x01 | 0x04 | 3<<4} // 25 °C, battery 0x12c = 300 × 10 mV, connect requested
	c := fakeDevice(t, func(cmd uint16, _ []byte, reply func(...byte)) {
		if cmd == cmdReadStatus {
			reply(append([]byte{0x00, 0x44}, msd...)...)
		}
	})
	s, err := c.ReadStatus()
	if err != nil {
		t.Fatal(err)
	}
	if b := s.Button(0); !b.Pressed || b.ID != 2 || b.Count != 5 {
		t.Fatalf("Taste falsch dekodiert: %+v", b)
	}
	if tc, ok := s.Touch(1); !ok || tc.Contacts != 2 || tc.X != 320 || tc.Y != 32 {
		t.Fatalf("Touch falsch dekodiert: %+v", tc)
	}
	if s.TemperatureC != 25 || s.BatteryMV != 3000 || !s.WantConnect || s.Reboot || s.Loop != 3 {
		t.Fatalf("Status falsch: %+v", s)
	}
}

func TestParseConfigWithButtonsTouchAndNames(t *testing.T) {
	base := testConfig(SchemeMono)
	packets := base[3 : len(base)-2]
	buttons := make([]byte, 30)
	buttons[0], buttons[1], buttons[15] = 1, 3, 4
	touch := make([]byte, 32)
	touch[10] = 5
	names := make([]byte, 288)
	copy(names[32:], "reTerminal Sticky")
	copy(names[96:], "Flur")
	packets = append(append(packets, 2, packetButtons), buttons...)
	packets = append(append(packets, 3, packetTouch), touch...)
	packets = append(append(packets, 4, packetNames), names...)
	data := binary.LittleEndian.AppendUint16(nil, uint16(len(packets)+5))
	data = append(append(append(data, 1), packets...), 0, 0)
	cfg, err := ParseConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UnknownPacket != -1 || len(cfg.Buttons) != 1 || cfg.Buttons[0].ByteIndex != 4 || cfg.Buttons[0].InputType != 3 ||
		len(cfg.Touches) != 1 || cfg.Touches[0].StartByte != 5 || cfg.Model != "reTerminal Sticky" || cfg.Name != "Flur" {
		t.Fatalf("falsch geparst: %+v", cfg)
	}
}
