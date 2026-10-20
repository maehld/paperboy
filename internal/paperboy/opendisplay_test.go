package paperboy

import (
	"testing"
	"time"

	"github.com/maehld/paperboy/internal/opendisplay"
)

func TestOpenDisplayConfig(t *testing.T) {
	cfg := &Config{UploadToken: testToken, Profiles: map[string]Profile{"p": {Width: 8, Height: 8}},
		Devices: []Device{{Type: TypeOpenDisplay, Name: "Flur", Address: "10.0.0.5", Profile: "p", MAC: "aa:bb:cc:dd:ee:ff"}}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	d := cfg.Devices[0]
	if d.Address != "10.0.0.5:2446" || d.MAC != "" || d.key() != "opendisplay:Flur" {
		t.Fatalf("wrong normalization: %+v", d)
	}
	if cfg.device("") != nil || cfg.device("AA:BB:CC:DD:EE:FF") != nil {
		t.Fatal("OpenDisplay devices must not use the TRMNL API")
	}
	cfg.Devices[0].Address = ""
	if cfg.validate() == nil {
		t.Fatal("missing address must be rejected")
	}
}

func TestButtonTracker(t *testing.T) {
	var tr buttonTracker
	status := func(b byte) opendisplay.Status {
		var s opendisplay.Status
		s.Dynamic[5] = b
		return s
	}
	if ids := tr.presses(status(0x12), []byte{5}); len(ids) != 0 {
		t.Fatal("first status is only a baseline")
	}
	if ids := tr.presses(status(0x92), []byte{5}); len(ids) != 0 {
		t.Fatal("only the pressed bit changed: no new press")
	}
	if ids := tr.presses(status(0x22), []byte{5}); len(ids) != 1 || ids[0] != 2 {
		t.Fatalf("new counter must yield a press of button 2: %v", ids)
	}
}

func TestPusherPaging(t *testing.T) {
	s, _ := testServer(t)
	upload(t, s, "0", 255)
	upload(t, s, "1", 0)
	upload(t, s, "2", 0)
	p := &pusher{s: s, d: &s.cfg.Devices[0]}
	now := time.Now()
	p.press(buttonPrev, now)
	if p.page != 2 {
		t.Fatalf("back from overview to last page: %d", p.page)
	}
	p.press(buttonNext, now)
	p.press(buttonNext, now)
	if p.page != 1 {
		t.Fatalf("next: %d", p.page)
	}
	p.press(buttonHome, now)
	if p.page != 0 {
		t.Fatal("button 0 = overview")
	}
}
