package paperboy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeviceStateKey(t *testing.T) {
	for _, tc := range []struct{ mac, key string }{
		{"aa:bb:cc:dd:ee:01", "AABBCCDDEE01"},
		{"AA-BB-CC-DD-EE-01", "AABBCCDDEE01"},
		{"aa:bb:cc:dd:ee:01:02:03", "AABBCCDDEE010203"},
	} {
		key, err := deviceStateKey(tc.mac)
		if err != nil || key != tc.key {
			t.Fatalf("%q: %q, %v", tc.mac, key, err)
		}
	}
	if _, err := deviceStateKey("kueche"); err == nil {
		t.Fatal("a device id is no MAC")
	}
	if got := formatStateKey("AABBCCDDEE01"); got != "AA:BB:CC:DD:EE:01" {
		t.Fatalf("formatted MAC: %q", got)
	}
}

func TestLoadDeviceStatesSkipsUnusableFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("AABBCCDDEE01.json", `{"api_key": "key-1", "page": 2}`)
	write("aabbccddee02.json", `{"page": 3}`)
	write("broken.json", `{`)
	write("notes.txt", `hello`)
	if err := os.MkdirAll(filepath.Join(dir, "AABBCCDDEE03.json"), 0o755); err != nil {
		t.Fatal(err)
	}

	state, err := loadDeviceStates(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(state) != 2 {
		t.Fatalf("loaded %d entries: %v", len(state), state)
	}
	if st := state["AABBCCDDEE01"]; st == nil || st.APIKey != "key-1" || st.Page != 2 {
		t.Fatalf("first device: %+v", st)
	}
	// A lower case file name is the same key on a case-insensitive file system.
	if st := state["AABBCCDDEE02"]; st == nil || st.Page != 3 {
		t.Fatalf("second device: %+v", st)
	}
}

func TestLoadDeviceStatesWithoutDirectory(t *testing.T) {
	state, err := loadDeviceStates(filepath.Join(t.TempDir(), "state", "devices"))
	if err != nil || len(state) != 0 {
		t.Fatalf("missing directory: %v %v", state, err)
	}
}

func TestValidStateKey(t *testing.T) {
	for _, key := range []string{"AABBCCDDEE01", "aabbccddee01", "AABBCCDDEE010203"} {
		if !validStateKey(key) {
			t.Fatalf("%q is a key", key)
		}
	}
	for _, key := range []string{"", "AABBCCDDEE0", "AABBCCDDEE0G", "kueche", "AABBCCDDEE01.json"} {
		if validStateKey(key) {
			t.Fatalf("%q is no key", key)
		}
	}
}
