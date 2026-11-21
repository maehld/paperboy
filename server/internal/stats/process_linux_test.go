//go:build linux

package stats

import (
	"os"
	"strings"
	"testing"
)

// TestProcessReportsObservation checks that the platform reader names the
// files of a live process and parses the kernel's address format (issue #306).
func TestProcessReportsObservation(t *testing.T) {
	p, ok := Process(os.Getpid())
	if !ok {
		t.Fatal("the test process must be readable")
	}
	if p.RSS == 0 {
		t.Error("resident set size is zero")
	}
	if len(p.Files) == 0 {
		t.Error("the test process has no open files")
	}
	found := false
	for _, f := range p.Files {
		if strings.HasPrefix(f, "socket:") {
			t.Errorf("%q is a socket and belongs in Conns", f)
		}
		if strings.HasSuffix(f, "/proc/self/fd") || strings.HasPrefix(f, "/dev/") || strings.HasPrefix(f, "/sys/") || strings.HasPrefix(f, "/usr/") {
			found = true
		}
	}
	if !found {
		t.Errorf("no path in %v looks like a real file", p.Files)
	}

	if got, port := socketAddr("0100007F:1F90"); got != "127.0.0.1:8080" || port != 8080 {
		t.Errorf("IPv4 address: %q %d", got, port)
	}
	if got, _ := socketAddr("00000000000000000000000001000000:0050"); got != "[::1]:80" {
		t.Errorf("IPv6 address: %q", got)
	}
	if got, _ := socketAddr("garbage"); got != "garbage" {
		t.Errorf("unknown address: %q", got)
	}
	if got := ioBytes("rchar: 10\nread_bytes: 2048\nwrite_bytes: 4096\n", "read_bytes:"); got != 2048 {
		t.Errorf("read_bytes: %d", got)
	}
	if got := ioBytes("read_bytes: 2048\n", "write_bytes:"); got != 0 {
		t.Errorf("missing counter: %d", got)
	}
}
