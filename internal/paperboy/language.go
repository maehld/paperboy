package paperboy

import (
	"os"
	"strings"
)

// detectLanguage returns the system language if Paperboy supports it (ADR
// 0033). Explicit environment variables win over the operating system; Windows
// uses its UI language, other systems rely on the environment alone.
func detectLanguage() string {
	for _, name := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		if lang := languageFromEnv(os.Getenv(name)); lang != "" {
			return lang
		}
	}
	return systemLanguage()
}

// languageFromEnv reads values such as "de_DE.UTF-8" or "en_US:de".
func languageFromEnv(v string) string {
	for _, part := range strings.Split(v, ":") {
		part = strings.ToLower(strings.TrimSpace(part))
		if i := strings.IndexAny(part, "._@-"); i > 0 {
			part = part[:i]
		}
		switch part {
		case "de", "en":
			return part
		}
	}
	return ""
}
