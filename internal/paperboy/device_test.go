package paperboy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestClientIP reads the source address of a request off the connection, with
// and without a port and for IPv4 and IPv6 (ADR 0083).
func TestClientIP(t *testing.T) {
	for _, tc := range []struct{ remote, want string }{
		{"192.0.2.1:1234", "192.0.2.1"},
		{"[2001:db8::1]:443", "2001:db8::1"},
		{"192.0.2.1", "192.0.2.1"},
		{"[2001:db8::1]", "2001:db8::1"},
		{"", ""},
	} {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = tc.remote
		if got := clientIP(r); got != tc.want {
			t.Errorf("clientIP(%q) = %q, want %q", tc.remote, got, tc.want)
		}
	}
}

// registerFrom registers a device from a given address.
func registerFrom(t *testing.T, s *Server, remote, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/device/register", strings.NewReader(body))
	req.RemoteAddr = remote
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// TestDeviceAddressIsShown stores the address of the last contact per MAC and
// shows it on the dashboard, in the devices tab and for unclaimed hardware
// (ADR 0083).
func TestDeviceAddressIsShown(t *testing.T) {
	s, _ := planServer(t, testPlan)
	dev := planDevice(t, s)
	key, err := deviceStateKey(planDeviceMAC)
	if err != nil {
		t.Fatal(err)
	}
	if st := s.state[key]; st == nil || st.ClientIP != "192.0.2.1" {
		t.Fatalf("registration does not record the address: %+v", s.state[key])
	}
	if info := s.deviceInfos(); len(info) != 1 || info[0].ClientIP != "192.0.2.1" {
		t.Fatalf("the devices API lacks the address: %+v", info)
	}
	if body := htmlRequest(t, s, "/ui").Body.String(); !strings.Contains(body, "192.0.2.1") {
		t.Fatalf("the dashboard lacks the address:\n%s", body)
	}
	writeDeviceFile(t, s.config().Dir, "test", "display_type: small\nmac: \""+planDeviceMAC+"\"\nplan: zimmer\n")
	if body := htmlRequest(t, s, "/ui/editor?tab=devices").Body.String(); !strings.Contains(body, "192.0.2.1") {
		t.Fatalf("the devices tab lacks the address:\n%s", body)
	}

	// A sync from another address moves the address with the last contact.
	req := httptest.NewRequest(http.MethodPost, "/api/device/sync", strings.NewReader(`{"reason":"timer"}`))
	req.RemoteAddr = "10.0.0.7:4321"
	req.Header.Set("Authorization", "Bearer "+dev.key)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sync: %d %s", rec.Code, rec.Body)
	}
	if st := s.state[key]; st == nil || st.ClientIP != "10.0.0.7" {
		t.Fatalf("sync does not update the address: %+v", s.state[key])
	}

	// Hardware without a device file keeps its address for the claim.
	if rec := registerFrom(t, s, "10.9.8.7:5555", `{"mac":"aa:bb:cc:dd:ee:99","hardware":"sticky"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("unknown device: %d %s", rec.Code, rec.Body)
	}
	body := htmlRequest(t, s, "/ui/editor?tab=devices").Body.String()
	if !strings.Contains(body, "AA:BB:CC:DD:EE:99") || !strings.Contains(body, "10.9.8.7") {
		t.Fatalf("the unclaimed hardware lacks the address:\n%s", body)
	}
}

// TestDeviceAddressSurvivesRestart reads the address back from the state file,
// so a restart keeps what the editor showed (ADR 0080, 0082).
func TestDeviceAddressSurvivesRestart(t *testing.T) {
	s, _ := planServer(t, testPlan)
	planDevice(t, s)
	key, err := deviceStateKey(planDeviceMAC)
	if err != nil {
		t.Fatal(err)
	}
	states, err := loadDeviceStates(s.devicesDir)
	if err != nil {
		t.Fatal(err)
	}
	if st := states[key]; st == nil || st.ClientIP != "192.0.2.1" {
		t.Fatalf("the state file drops the address: %+v", states[key])
	}
}
