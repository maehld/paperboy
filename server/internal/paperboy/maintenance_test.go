package paperboy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeDeviceState puts one device state into the server and its file, the way
// a device that is not configured would leave it.
func writeDeviceState(t *testing.T, s *Server, key string, st *deviceState) string {
	t.Helper()
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	path := deviceStateFile(s.devicesDir, key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.state[key] = st
	s.mu.Unlock()
	return path
}

// TestMaintenanceRemovesStaleDeviceState keeps the state of a device that may
// still be claimed, and drops the state of hardware that has been gone for a
// month (ADR 0154).
func TestMaintenanceRemovesStaleDeviceState(t *testing.T) {
	s, now := planServer(t, testPlan)
	old := writeDeviceState(t, s, "AABBCCDDEE02", &deviceState{
		FirstSeen: now.Add(-60 * 24 * time.Hour), LastSeen: now.Add(-45 * 24 * time.Hour), Hardware: "sticky"})
	// A device that registered an hour ago waits for a claim: its state stays
	// until the operator decides (ADR 0080).
	recent := writeDeviceState(t, s, "AABBCCDDEE03", &deviceState{
		FirstSeen: now.Add(-time.Hour), LastSeen: now.Add(-time.Minute)})

	body := htmlRequest(t, s, "/ui/system/maintenance?preview=devices").Body.String()
	if !strings.Contains(body, "AA:BB:CC:DD:EE:02") {
		t.Fatalf("the preview misses the stale state: %s", body)
	}
	if strings.Contains(body, "AA:BB:CC:DD:EE:03") {
		t.Fatalf("the preview lists a state that is not stale: %s", body)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ui/system/maintenance/delete", strings.NewReader("kind=devices"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete: %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/ui/system/maintenance?done=devices&removed=1&failed=0" {
		t.Fatalf("redirect: %q", loc)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("the stale state file must be gone")
	}
	if _, err := os.Stat(recent); err != nil {
		t.Fatalf("the recent state file must stay: %v", err)
	}
	s.mu.Lock()
	_, kept := s.state["AABBCCDDEE03"]
	_, dropped := s.state["AABBCCDDEE02"]
	s.mu.Unlock()
	if !kept || dropped {
		t.Fatalf("memory keeps %v and drops %v", kept, dropped)
	}
	// The page names what it removed (ADR 0106).
	page := htmlRequest(t, s, "/ui/system/maintenance?done=devices&removed=1&failed=0").Body.String()
	if !strings.Contains(page, "Removed 1 device states.") {
		t.Fatalf("no confirmation: %s", page)
	}
}

// TestMaintenanceRemovesOrphanedBackups removes the editor copies of deleted
// objects and keeps the ones whose object still exists (ADR 0066, 0154).
func TestMaintenanceRemovesOrphanedBackups(t *testing.T) {
	s, _ := planServer(t, testPlan)
	cfg := s.config()
	root := filepath.Join(cfg.Dir, stateDirName, "editor")
	writePageFile(t, filepath.Join(root, "pages", "eins", "1.yaml"), "old eins")
	writePageFile(t, filepath.Join(root, "pages", "gone", "1.yaml"), "old gone")
	writePageFile(t, filepath.Join(root, "pages", "gone", "2.yaml"), "older gone")
	writePageFile(t, filepath.Join(root, "plans", "zimmer", "1.yaml"), "old plan")

	body := htmlRequest(t, s, "/ui/system/maintenance?preview=backups").Body.String()
	if !strings.Contains(body, "gone") {
		t.Fatalf("the preview misses the orphaned backups: %s", body)
	}
	if strings.Contains(body, ">eins<") || strings.Contains(body, ">zimmer<") {
		t.Fatalf("the preview lists a backup of an object that exists: %s", body)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ui/system/maintenance/delete", strings.NewReader("kind=backups"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete: %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(root, "pages", "gone")); !os.IsNotExist(err) {
		t.Fatal("the orphaned backups must be gone")
	}
	for _, stay := range []string{
		filepath.Join(root, "pages", "eins", "1.yaml"),
		filepath.Join(root, "plans", "zimmer", "1.yaml"),
	} {
		if _, err := os.Stat(stay); err != nil {
			t.Fatalf("%s must stay: %v", stay, err)
		}
	}
}
