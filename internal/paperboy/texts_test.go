package paperboy

import "testing"

func TestLanguageFromEnv(t *testing.T) {
	cases := map[string]string{
		"de_DE.UTF-8": "de",
		"en_US.UTF-8": "en",
		"en_US:de":    "en",
		"fr_FR.UTF-8": "",
		"C":           "",
		"POSIX":       "",
		"":            "",
	}
	for value, want := range cases {
		if got := languageFromEnv(value); got != want {
			t.Fatalf("languageFromEnv(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestDetectLanguagePrefersEnvironment(t *testing.T) {
	t.Setenv("LANGUAGE", "en_US.UTF-8")
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")
	if got := detectLanguage(); got != "en" {
		t.Fatalf("detectLanguage() = %q, want en", got)
	}
}

func TestTexts(t *testing.T) {
	deCfg, enCfg := &Config{Language: "de"}, &Config{Language: "en"}
	de, en := deCfg.texts(), enCfg.texts()
	if got := de.lowBattery("34"); got != "Akku schwach – bitte laden (ca. 3,4 V)" {
		t.Fatalf("german low battery: %q", got)
	}
	if got := en.lowBattery("34"); got != "Battery low – please charge (approx. 3.4 V)" {
		t.Fatalf("english low battery: %q", got)
	}
	if title, line := de.criticalBattery("30"); title != "Akku fast leer" || line != "Bitte jetzt laden (ca. 3,0 V)" {
		t.Fatalf("german critical battery: %q %q", title, line)
	}
	if title, line := en.criticalBattery("30"); title != "Battery almost empty" || line != "Please charge now (approx. 3.0 V)" {
		t.Fatalf("english critical battery: %q %q", title, line)
	}
	if title, text := de.noPlan(); title != "Kein Plan konfiguriert" || text == "" {
		t.Fatalf("german no plan: %q %q", title, text)
	}
	if title, text := en.noPlan(); title != "No plan configured" || text == "" {
		t.Fatalf("english no plan: %q %q", title, text)
	}
}
