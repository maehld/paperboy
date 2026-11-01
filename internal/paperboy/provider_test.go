package paperboy

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestProviderHelperProcess is not a real test: it acts as the external
// provider program that StartProviders runs, driven by environment variables.
func TestProviderHelperProcess(t *testing.T) {
	if os.Getenv("PAPERBOY_TEST_PROVIDER") != "1" {
		return
	}
	if os.Getenv("PAPERBOY_TEST_MODE") == "fail" {
		fmt.Fprintln(os.Stderr, "provider is broken")
		os.Exit(2)
	}
	fmt.Printf(`{"language": %q, "timezone": %q, "temp": 18.5, "text": "Sonnig"}`,
		os.Getenv("PAPERBOY_LANGUAGE"), os.Getenv("PAPERBOY_TIMEZONE"))
	os.Exit(0)
}

// TestProviderWritesIntoDocument runs a provider through the server and checks
// that its output lands in the data document together with the global language
// and timezone (ADR 0036).
func TestProviderWritesIntoDocument(t *testing.T) {
	s, _ := testServer(t)
	s.config().Language = "en"
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	s.config().ProvidersDir = t.TempDir()
	s.config().Providers = []Provider{{
		Name:     "weather",
		Command:  []string{exe, "-test.run=TestProviderHelperProcess"},
		Path:     "weather",
		Interval: Duration(time.Hour),
		Timeout:  Duration(30 * time.Second),
		Env:      map[string]string{"PAPERBOY_TEST_PROVIDER": "1"},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.StartProviders(ctx)

	deadline := time.Now().Add(10 * time.Second)
	for {
		v, ok := s.doc.Get("weather")
		if ok {
			m, _ := v.(map[string]any)
			if m["temp"] != 18.5 || m["language"] != "en" || m["timezone"] == "" {
				t.Fatalf("provider output: %#v", v)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("provider never wrote its branch")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
