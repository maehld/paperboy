package paperboy

import (
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

// A manual alert asks one device to signal at its next sync (ADR 0142). The
// device drives every request and cannot confirm a signal, so the state keeps
// the request with its expiry, not an outcome. This is the shape of the pairing
// claim (ADR 0109): a wish that waits for the device's next request.

// alertLifetime is how long a manual request waits for the device: two regular
// refreshes, so a slow plan still collects it. At least one hour protects a
// device whose interval is unknown, and one day keeps a display that stayed
// away from signalling for an old click.
func alertLifetime(d *Device, st *deviceState) time.Duration {
	interval := d.refresh()
	if st.LastInterval > 0 {
		interval = time.Duration(st.LastInterval) * time.Second
	}
	life := 2 * interval
	if life < time.Hour {
		life = time.Hour
	}
	if life > 24*time.Hour {
		life = 24 * time.Hour
	}
	return life
}

// Refusal codes for a manual alert: the dashboard shows the text before the
// click, and the redirect carries the code, not a message.
const (
	refuseUnknown      = "unknown"      // no device file with that id
	refuseMethods      = "methods"      // the device file names no alert method
	refuseProblem      = "problem"      // the device cannot sync (ADR 0079)
	refuseUnregistered = "unregistered" // the device has no key yet (ADR 0042)
)

// alertRefusalText turns a refusal code into the line on the card.
func alertRefusalText(code string) string {
	switch code {
	case refuseMethods:
		return "the device file names no alert method: enable beep or blink"
	case refuseProblem:
		return "the device cannot sync; its card names the reason"
	case refuseUnregistered:
		return "the device has not registered yet"
	case refuseUnknown:
		return "no device file uses this id"
	}
	return ""
}

// alertOffer answers why the dashboard offers no signal for a device, or an
// empty string when the request would work.
func alertOffer(d *Device, info deviceInfo) string {
	switch {
	case d == nil:
		return alertRefusalText(refuseUnknown)
	case info.Problem != "":
		return alertRefusalText(refuseProblem)
	}
	if beep, blink := d.alertMethods(); !beep && !blink {
		return alertRefusalText(refuseMethods)
	}
	if !info.Registered {
		return alertRefusalText(refuseUnregistered)
	}
	return ""
}

// alertQueuedText names a request that waits for the next sync, for the card's
// details. An expired request is not shown: the next sync drops it.
func alertQueuedText(a *pendingAlert, now time.Time, loc *time.Location) string {
	if a == nil || now.After(a.Until) {
		return ""
	}
	return "queued " + agoText(a.At, now) + " · expires " + a.Until.In(loc).Format("15:04")
}

// takeManualAlert consumes the request of this device for one sync. An expired
// request or one during the battery warning is dropped: the warning page
// carries no alert either (ADR 0131). Callers hold s.mu and take the snapshot
// afterwards, so the consumed request reaches the state file of this sync.
func takeManualAlert(d *Device, st *deviceState, now time.Time, critical bool) bool {
	req := st.PendingAlert
	if req == nil {
		return false
	}
	st.PendingAlert = nil
	switch {
	case now.After(req.Until):
		slog.Info("manual alert expired", "device", d.ID, "requested", req.At)
		return false
	case critical:
		slog.Info("manual alert dropped", "device", d.ID, "reason", "critical battery")
		return false
	}
	return true
}

// handleDeviceAlert queues a manual alert for the next sync of one device. The
// dashboard posts a form, so the answer is a redirect to the cards; a refusal
// travels as a code in the query, and a lost page does not answer with JSON.
func (s *Server) handleDeviceAlert(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	id := r.PathValue("id")
	var d *Device
	for i := range cfg.Devices {
		if cfg.Devices[i].ID == id {
			d = &cfg.Devices[i]
			break
		}
	}
	refused := refuseUnknown
	if d != nil {
		refused = s.queueAlert(d)
	}
	target := "/ui?alert=" + url.QueryEscape(id)
	if refused != "" {
		target += "&refused=" + refused
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// queueAlert writes the request into the device state and answers an empty
// string on success, or the refusal code. It writes the state outside the lock,
// like every other state change.
func (s *Server) queueAlert(d *Device) string {
	if d.Problem != "" {
		return refuseProblem
	}
	if beep, blink := d.alertMethods(); !beep && !blink {
		return refuseMethods
	}
	key := d.stateKey()
	s.lock("alert.lock")
	st := s.state[key]
	if st == nil || st.APIKey == "" {
		s.mu.Unlock()
		return refuseUnregistered
	}
	now := s.now()
	life := alertLifetime(d, st)
	st.PendingAlert = &pendingAlert{At: now, Until: now.Add(life)}
	data := s.stateSnapshotLocked(st)
	s.mu.Unlock()
	s.saveState(key, data)
	slog.Info("manual alert queued", "device", d.ID, "until", now.Add(life).Format(time.RFC3339))
	return ""
}
