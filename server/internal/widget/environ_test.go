package widget

import (
	"strings"
	"testing"
)

// TestEnvironHidesPaperboySecrets guards ADR 0134: a plugin inherits an
// allowlist, not everything Paperboy received.
func TestEnvironHidesPaperboySecrets(t *testing.T) {
	t.Setenv("PAPERBOY_UI_PASSWORD", "hunter2")
	t.Setenv("PAPERBOY_DOWNLOAD_TOKEN", "ghp_secret")
	t.Setenv("GITHUB_TOKEN", "ghp_secret")

	env := environ(map[string]string{"PAPERBOY_LANGUAGE": "de"})
	for _, entry := range env {
		for _, secret := range []string{"PAPERBOY_UI_PASSWORD", "PAPERBOY_DOWNLOAD_TOKEN", "GITHUB_TOKEN"} {
			if strings.HasPrefix(entry, secret+"=") {
				t.Errorf("plugin environment leaked %s: %q", secret, entry)
			}
		}
	}
	if !hasEnv(env, "PAPERBOY_LANGUAGE=de") {
		t.Errorf("configured value missing: %q", env)
	}
}

func TestEnvironKeepsAllowlistedVariables(t *testing.T) {
	t.Setenv("LANG", "de_DE.UTF-8")
	t.Setenv("HTTPS_PROXY", "http://proxy.example:3128")

	env := environ(nil)
	if !hasEnv(env, "LANG=de_DE.UTF-8") {
		t.Errorf("LANG missing: %q", env)
	}
	if !hasEnv(env, "HTTPS_PROXY=http://proxy.example:3128") {
		t.Errorf("HTTPS_PROXY missing: %q", env)
	}
}

// TestEnvironReplacesInheritedKeys keeps one value per key: an appended
// duplicate would let the inherited value win.
func TestEnvironReplacesInheritedKeys(t *testing.T) {
	t.Setenv("PATH", "/host/bin")
	t.Setenv("LANG", "en_US.UTF-8")

	env := environ(map[string]string{"PATH": "/plugin/bin", "LANG": "de_DE.UTF-8"})
	paths := 0
	for _, entry := range env {
		if strings.HasPrefix(entry, "PATH=") {
			paths++
			if entry != "PATH=/plugin/bin" {
				t.Errorf("PATH = %q, want the configured value", entry)
			}
		}
		if strings.HasPrefix(entry, "LANG=") && entry != "LANG=de_DE.UTF-8" {
			t.Errorf("LANG = %q, want the configured value", entry)
		}
	}
	if paths != 1 {
		t.Errorf("PATH appears %d times, want once", paths)
	}
}

func TestEnvironIsSorted(t *testing.T) {
	env := environ(map[string]string{"ZZZ": "1", "PAPERBOY_LANGUAGE": "en"})
	for i := 1; i < len(env); i++ {
		if env[i-1] > env[i] {
			t.Fatalf("environment is not sorted: %q before %q", env[i-1], env[i])
		}
	}
}

func hasEnv(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}
