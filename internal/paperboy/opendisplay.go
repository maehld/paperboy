package paperboy

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maehld/paperboy/internal/opendisplay"
)

// Run pushes images to OpenDisplay devices until ctx ends. TRMNL devices need
// no background work; they are served by the HTTP handlers.
func (s *Server) Run(ctx context.Context) {
	done := make(chan struct{})
	count := 0
	for i := range s.cfg.Devices {
		if d := &s.cfg.Devices[i]; d.Type == TypeOpenDisplay {
			count++
			p := &pusher{s: s, d: d, timeout: 10 * time.Second}
			go func() { p.run(ctx); done <- struct{}{} }()
		}
	}
	for ; count > 0; count-- {
		<-done
	}
}

// Button IDs reported by reTerminal Sticky and E1001 under OpenDisplay.
const (
	buttonHome = 0
	buttonPrev = 1
	buttonNext = 2
)

// fullRefreshEvery forces a full refresh after this many fast refreshes on
// monochrome panels to clear ghosting; 4-gray panels always refresh fully.
const fullRefreshEvery = 11

// pusher keeps one OpenDisplay device up to date. With buttons it holds a
// persistent connection and polls the status block every 500 ms; otherwise it
// connects once per update so the device stays available for other tools.
type pusher struct {
	s       *Server
	d       *Device
	timeout time.Duration

	client    *opendisplay.Client
	display   *opendisplay.Display
	buttons   []byte
	tracker   buttonTracker
	page      int
	lastInput time.Time
	lastFrame []byte
	fastCount int
}

func (p *pusher) run(ctx context.Context) {
	retry := 30 * time.Second
	var nextRender time.Time
	defer p.disconnect()
	for ctx.Err() == nil {
		now := p.s.now()
		if p.client == nil {
			if err := p.connect(); err != nil {
				p.record(now, err)
				slog.Warn("OpenDisplay unreachable", "device", p.d.Name, "error", err, "retry", retry)
				if !sleep(ctx, retry) {
					return
				}
				retry = min(2*retry, max(p.d.refresh(now), 30*time.Second))
				continue
			}
			retry = 30 * time.Second
			nextRender = now
		}
		if len(p.buttons) > 0 {
			status, err := p.client.ReadStatus()
			if err != nil {
				p.fail(now, "Tastenabfrage", err)
				if !sleep(ctx, retry) {
					return
				}
				continue
			}
			for _, id := range p.tracker.presses(status, p.buttons) {
				p.press(id, now)
				nextRender = now
			}
		}
		if p.page != 0 && now.Sub(p.lastInput) >= time.Duration(p.s.cfg.PageTimeout) {
			p.page = 0
			nextRender = now
		}
		if !now.Before(nextRender) {
			if err := p.update(now); err != nil {
				p.fail(now, "Aktualisierung", err)
				if !sleep(ctx, retry) {
					return
				}
				continue
			}
			nextRender = now.Add(p.d.refresh(now))
		}
		wait := 500 * time.Millisecond
		if len(p.buttons) == 0 {
			p.disconnect() // idle connections would be dropped by the device
			wait = time.Until(nextRender)
		}
		if !sleep(ctx, wait) {
			return
		}
	}
}

func (p *pusher) press(id byte, now time.Time) {
	p.lastInput = now
	_, pages, _ := p.s.pageImage(p.d.Profile, 0, now)
	pages = max(pages, 1)
	switch id {
	case buttonHome:
		p.page = 0
	case buttonPrev:
		p.page = (p.page + pages - 1) % pages
	case buttonNext:
		p.page = (p.page + 1) % pages
	}
	slog.Info("OpenDisplay button", "device", p.d.Name, "button", id, "page", p.page)
}

func (p *pusher) connect() error {
	client, err := opendisplay.Dial(p.d.Address, p.timeout)
	if err != nil {
		return err
	}
	if p.display == nil {
		cfg, err := client.ReadConfig()
		if err != nil {
			client.Close()
			return err
		}
		if len(cfg.Displays) == 0 {
			client.Close()
			return fmt.Errorf("Gerät meldet kein Display")
		}
		p.display = &cfg.Displays[0]
		p.buttons = nil
		for _, b := range cfg.Buttons {
			if p.d.Buttons && b.ByteIndex <= 10 {
				p.buttons = append(p.buttons, b.ByteIndex)
			}
		}
		profile := p.s.cfg.Profiles[p.d.Profile]
		if profile.Width != p.display.Width || profile.Height != p.display.Height {
			slog.Warn("panel size does not match profile", "device", p.d.Name, "panel", fmt.Sprintf("%dx%d", p.display.Width, p.display.Height),
				"profile", fmt.Sprintf("%dx%d", profile.Width, profile.Height))
		}
		slog.Info("OpenDisplay connected", "device", p.d.Name, "model", cfg.Model, "width", p.display.Width, "height", p.display.Height,
			"scheme", p.display.ColorScheme.String(), "buttons", len(p.buttons) > 0)
	}
	p.client = client
	p.tracker = buttonTracker{} // the first status read after connecting is only a baseline
	return nil
}

func (p *pusher) disconnect() {
	if p.client != nil {
		p.client.Close()
		p.client = nil
	}
}

func (p *pusher) fail(now time.Time, stage string, err error) {
	slog.Warn("OpenDisplay error", "device", p.d.Name, "stage", stage, "error", err)
	p.record(now, fmt.Errorf("%s: %w", stage, err))
	p.disconnect()
	p.display = nil // re-read the configuration on the next connect
}

// record stores the outcome of a push attempt for GET /api/devices.
func (p *pusher) record(now time.Time, err error) {
	p.s.mu.Lock()
	defer p.s.mu.Unlock()
	st := p.s.stateLocked(p.d.key())
	st.Page = p.page
	if err != nil {
		st.LastError = err.Error()
		return
	}
	st.LastSeen, st.LastError = now, ""
}

func (p *pusher) update(now time.Time) error {
	img, pages, ok := p.s.pageImage(p.d.Profile, p.page, now)
	if !ok {
		img = placeholder(p.s.cfg.Profiles[p.d.Profile], "Paperboy", "Warte auf Bilder …")
	}
	if p.page >= pages {
		p.page = 0
	}
	if img.Rect.Dx() != p.display.Width || img.Rect.Dy() != p.display.Height {
		return fmt.Errorf("Bild %dx%d passt nicht zum Panel %dx%d", img.Rect.Dx(), img.Rect.Dy(), p.display.Width, p.display.Height)
	}
	frame, err := opendisplay.Encode(img, *p.display)
	if err != nil {
		return err
	}
	if bytes.Equal(frame, p.lastFrame) {
		p.record(now, nil)
		return nil
	}
	mode := p.refreshMode()
	start := time.Now()
	if err := p.client.Upload(frame, 1000, mode, nil); err != nil {
		return err
	}
	slog.Info("OpenDisplay updated", "device", p.d.Name, "page", p.page, "fast", mode == opendisplay.RefreshFast,
		"duration", time.Since(start).Round(100*time.Millisecond))
	p.lastFrame = frame
	if mode == opendisplay.RefreshFast {
		p.fastCount++
	} else {
		p.fastCount = 0
	}
	p.record(now, nil)
	return nil
}

func (p *pusher) refreshMode() opendisplay.RefreshMode {
	if p.display.ColorScheme != opendisplay.SchemeMono || p.lastFrame == nil || p.fastCount >= fullRefreshEvery {
		return opendisplay.RefreshFull
	}
	return opendisplay.RefreshFast
}

// buttonTracker turns status snapshots into presses. Each button has its own
// 4-bit counter and a status byte shows the last pressed button, so a press is
// any change of the (ID, counter) pair. Short presses often show only there.
type buttonTracker struct {
	last map[byte]byte // status byte index → last raw value
}

func (t *buttonTracker) presses(status opendisplay.Status, indices []byte) []byte {
	baseline := t.last == nil
	if baseline {
		t.last = map[byte]byte{}
	}
	var ids []byte
	for _, index := range indices {
		raw := status.Dynamic[index]
		previous, seen := t.last[index]
		t.last[index] = raw
		if baseline || !seen || raw&0x7f == previous&0x7f {
			continue // only the pressed bit changed: same press
		}
		ids = append(ids, raw&7)
	}
	return ids
}

func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
