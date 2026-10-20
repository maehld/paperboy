package opendisplay

import (
	"encoding/binary"
	"fmt"
)

// Fixed payload sizes of Flex config packets (from the reference sender py-opendisplay).
var packetSizes = map[byte]int{
	0x01: 22, 0x02: 22, 0x04: 30, 0x20: 46, 0x21: 22, 0x23: 30, 0x24: 30, 0x25: 30,
	0x26: 160, 0x27: 64, 0x28: 32, 0x29: 32, 0x2a: 32, 0x2b: 32, 0x2c: 288,
}

const (
	packetSystem   = 0x01
	packetDisplay  = 0x20
	packetButtons  = 0x25
	packetWifi     = 0x26
	packetSecurity = 0x27
	packetTouch    = 0x28
	packetNames    = 0x2c
	wifiLegacySize = 65
	// ButtonNotPublished marks an input that reports into no status byte.
	ButtonNotPublished = 0xff
)

// Button describes a binary input; ByteIndex is its byte in the 11-byte dynamic status block.
type Button struct {
	Instance, InputType, ByteIndex byte
}

// Touch describes a touch controller reporting 5 bytes from StartByte of the dynamic block.
type Touch struct {
	Instance, StartByte, Flags byte
}

type ColorScheme byte

const (
	SchemeMono  ColorScheme = 0
	SchemeBWR   ColorScheme = 1
	SchemeBWY   ColorScheme = 2
	SchemeBWRY  ColorScheme = 3
	Scheme6     ColorScheme = 4
	SchemeGray4 ColorScheme = 5
)

func (s ColorScheme) String() string {
	switch s {
	case SchemeMono:
		return "black/white (1 bpp)"
	case SchemeBWR:
		return "black/white/red"
	case SchemeBWY:
		return "black/white/yellow"
	case SchemeBWRY:
		return "black/white/red/yellow"
	case Scheme6:
		return "6 colors (Spectra 6)"
	case SchemeGray4:
		return "4 gray levels"
	}
	return fmt.Sprintf("unknown (%d)", s)
}

type Display struct {
	PanelIC           uint16
	Width, Height     int
	WidthMM, HeightMM int
	Rotation          byte
	PartialUpdate     byte // 0 none, 1 regions, 2 full-frame partial only
	ColorScheme       ColorScheme
	TransmissionModes byte
}

type Config struct {
	Version            byte
	CommunicationModes byte // bit 0 BLE, bit 1 OEPL, bit 2 Wi-Fi
	Displays           []Display
	Buttons            []Button
	Touches            []Touch
	Model, Name        string
	EncryptionEnabled  bool
	UnknownPacket      int // first unknown packet ID, or -1
}

// ParseConfig parses [length:2 LE][version:1][packets…][crc:2].
func ParseConfig(data []byte) (Config, error) {
	if len(data) < 5 {
		return Config{}, fmt.Errorf("configuration too short: %d bytes", len(data))
	}
	cfg := Config{Version: data[2], UnknownPacket: -1}
	packets := data[3 : len(data)-2]
	for offset := 0; offset+2 <= len(packets); {
		id := packets[offset+1]
		offset += 2
		size, known := packetSizes[id]
		if !known {
			cfg.UnknownPacket = int(id)
			break
		}
		if offset+size > len(packets) {
			if id == packetWifi && len(packets)-offset == wifiLegacySize {
				size = wifiLegacySize
			} else {
				return cfg, fmt.Errorf("packet 0x%02x truncated", id)
			}
		}
		p := packets[offset : offset+size]
		offset += size
		switch id {
		case packetSystem:
			cfg.CommunicationModes = p[2]
		case packetDisplay:
			cfg.Displays = append(cfg.Displays, Display{
				PanelIC:           binary.LittleEndian.Uint16(p[2:]),
				Width:             int(binary.LittleEndian.Uint16(p[4:])),
				Height:            int(binary.LittleEndian.Uint16(p[6:])),
				WidthMM:           int(binary.LittleEndian.Uint16(p[8:])),
				HeightMM:          int(binary.LittleEndian.Uint16(p[10:])),
				Rotation:          p[14],
				PartialUpdate:     p[20],
				ColorScheme:       ColorScheme(p[21]),
				TransmissionModes: p[22],
			})
		case packetButtons:
			cfg.Buttons = append(cfg.Buttons, Button{Instance: p[0], InputType: p[1], ByteIndex: p[15]})
		case packetTouch:
			cfg.Touches = append(cfg.Touches, Touch{Instance: p[0], Flags: p[8], StartByte: p[10]})
		case packetNames:
			cfg.Model, cfg.Name = cString(p[32:64]), cString(p[96:128])
		case packetSecurity:
			// [enabled:1][key:16]…; an all-zero key means encryption is off.
			zero := true
			for _, b := range p[1:17] {
				zero = zero && b == 0
			}
			cfg.EncryptionEnabled = p[0] != 0 && !zero
		}
	}
	if len(cfg.Displays) == 0 {
		return cfg, fmt.Errorf("configuration contains no display packet")
	}
	return cfg, nil
}

func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
