package opendisplay

import (
	"encoding/binary"
	"image"
	"image/color"
	"io"
	"net"
	"testing"
	"time"
)

// fakeDevice answers frames on the server side of a pipe using handle.
func fakeDevice(t *testing.T, handle func(cmd uint16, payload []byte, reply func(...byte))) *Client {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() { client.Close(); server.Close() })
	reply := func(b ...byte) {
		frame := binary.LittleEndian.AppendUint16(nil, uint16(len(b)))
		server.Write(append(frame, b...))
	}
	go func() {
		for {
			var header [2]byte
			if _, err := io.ReadFull(server, header[:]); err != nil {
				return
			}
			payload := make([]byte, binary.LittleEndian.Uint16(header[:]))
			if _, err := io.ReadFull(server, payload); err != nil {
				return
			}
			handle(binary.BigEndian.Uint16(payload), payload[2:], reply)
		}
	}()
	return NewClient(&tcpTransport{conn: client, writeTimeout: time.Second}, time.Second)
}

func testConfig(scheme ColorScheme) []byte {
	packets := []byte{0, packetSystem}
	system := make([]byte, 22)
	system[2] = 0b101 // BLE + Wi-Fi
	packets = append(packets, system...)
	display := make([]byte, 46)
	binary.LittleEndian.PutUint16(display[2:], 0x0015)
	binary.LittleEndian.PutUint16(display[4:], 800)
	binary.LittleEndian.PutUint16(display[6:], 480)
	display[14], display[20], display[21] = 1, 1, byte(scheme)
	packets = append(packets, 1, packetDisplay)
	packets = append(packets, display...)
	data := binary.LittleEndian.AppendUint16(nil, uint16(len(packets)+5))
	data = append(data, 1)
	data = append(data, packets...)
	return append(data, 0, 0) // CRC not checked
}

func TestReadConfigInChunks(t *testing.T) {
	config := testConfig(SchemeGray4)
	c := fakeDevice(t, func(cmd uint16, _ []byte, reply func(...byte)) {
		if cmd != cmdReadConfig {
			return
		}
		first := append([]byte{0x00, 0x40, 0, 0}, byte(len(config)), byte(len(config)>>8))
		reply(append(first, config[:40]...)...)
		reply(append([]byte{0x00, 0x40, 1, 0}, config[40:]...)...)
	})
	cfg, err := c.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	d := cfg.Displays[0]
	if d.Width != 800 || d.Height != 480 || d.ColorScheme != SchemeGray4 || d.PanelIC != 0x15 || d.Rotation != 1 || cfg.CommunicationModes&4 == 0 {
		t.Fatalf("parsed incorrectly: %+v %+v", cfg, d)
	}
}

func TestUploadWithAckFlagAndExplicitEnd(t *testing.T) {
	var received []byte
	var endMode []byte
	c := fakeDevice(t, func(cmd uint16, payload []byte, reply func(...byte)) {
		switch cmd {
		case cmdWriteStart:
			reply(0x80, 0x70) // ACK with high bit
		case cmdWriteData:
			received = append(received, payload...)
			reply(0x00, 0x71)
		case cmdWriteEnd:
			endMode = payload
			reply(0x00, 0x72)
			reply(0x00, 0x73)
		}
	})
	data := make([]byte, 2500)
	for i := range data {
		data[i] = byte(i)
	}
	if err := c.Upload(data, 1000, RefreshFast, nil); err != nil {
		t.Fatal(err)
	}
	if string(received) != string(data) || len(endMode) != 1 || endMode[0] != 1 {
		t.Fatalf("wrong data or end mode: %d bytes, end % x", len(received), endMode)
	}
}

func TestUploadAutoCompleteAndNack(t *testing.T) {
	c := fakeDevice(t, func(cmd uint16, _ []byte, reply func(...byte)) {
		switch cmd {
		case cmdWriteStart:
			reply(0x00, 0x70)
		case cmdWriteData:
			reply(0x00, 0x72) // device refreshes on its own
			reply(0x00, 0x73)
		case cmdWriteEnd:
			t.Error("END must not be sent after automatic completion")
		}
	})
	if err := c.Upload([]byte{1, 2, 3}, 1000, RefreshFull, nil); err != nil {
		t.Fatal(err)
	}
	c = fakeDevice(t, func(cmd uint16, _ []byte, reply func(...byte)) { reply(0xff, 0x70) })
	if err := c.Upload([]byte{1}, 1000, RefreshFull, nil); err == nil {
		t.Fatal("NACK on start must return an error")
	}
}

func TestEncodeMonoBitOrder(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 8, 1))
	for x, white := range []bool{true, false, true, true, false, false, true, false} {
		if white {
			img.SetGray(x, 0, color.Gray{255})
		}
	}
	out, err := Encode(img, Display{Width: 8, Height: 1, ColorScheme: SchemeMono})
	if err != nil || len(out) != 1 || out[0] != 0xb2 {
		t.Fatalf("got % x, %v; want b2", out, err)
	}
}

func TestEncodeGray4Planes(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 4, 1))
	for x, level := range []uint8{0, 85, 170, 255} { // black, dark, light, white
		img.SetGray(x, 0, color.Gray{level})
	}
	out, err := Encode(img, Display{Width: 4, Height: 1, ColorScheme: SchemeGray4, PanelIC: 0x15})
	// Codes 3,1,2,0 → plane0 (bit 0) 1,1,0,0 = 0xC0; plane1 (bit 1) 1,0,1,0 = 0xA0.
	if err != nil || len(out) != 2 || out[0] != 0xc0 || out[1] != 0xa0 {
		t.Fatalf("got % x, %v", out, err)
	}
}
