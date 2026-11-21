package atomicfile

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestWriteFileIsAtomic runs many writers on one path: every read must see a
// complete document of one writer, never a mix and never an empty file.
func TestWriteFileIsAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	writers := 16
	rounds := 50
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			payload, err := json.Marshal(map[string]string{"writer": string(rune('a' + i))})
			if err != nil {
				t.Error(err)
				return
			}
			for range rounds {
				if err := WriteFile(path, payload, 0o600); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	for {
		select {
		case <-done:
			return
		default:
		}
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]string
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("partial file: %q (%v)", data, err)
		}
		if doc["writer"] == "" {
			t.Fatalf("unexpected document: %q", data)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestWriteFileCreatesNoLeftovers checks the mode and the directory content: a
// completed write leaves the target only.
func TestWriteFileCreatesNoLeftovers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode: %v %v", fi, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		t.Fatalf("directory after the write: %v", entries)
	}
}

// TestWriteFileReplacesContent proves the rename replaces an existing file.
func TestWriteFileReplacesContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, []byte("new")) {
		t.Fatalf("content: %q (%v)", data, err)
	}
}
