package paperboy

import (
	"fmt"
	"strconv"
)

// texts holds the built-in image texts for one language (ADR 0033).
type texts struct {
	language string
}

// variant picks the German or English text.
func (t texts) variant(de, en string) string {
	if t.language == "en" {
		return en
	}
	return de
}

func (t texts) noPlan() (string, string) {
	return t.variant(
			"Kein Plan konfiguriert",
			"No plan configured",
		), t.variant(
			"Für dieses Gerät ist in der Konfiguration kein Plan hinterlegt.",
			"No plan is configured for this device.",
		)
}

func (t texts) planMissing() string {
	return t.variant("Plan fehlt", "Plan missing")
}

func (t texts) noSelection() (string, string) {
	return t.variant(
			"Keine Seite festgelegt",
			"No page selected",
		), t.variant(
			"Für diese Situation passt keine Regel und kein fallback.",
			"No rule and no fallback match the current situation.",
		)
}

func (t texts) noFittingPage(rule, profile string) (string, string) {
	return t.variant("Keine passende Seite", "No matching page"),
		t.variant("Regel "+rule+" hat keine Seite für "+profile+".", "Rule "+rule+" has no page for "+profile+".")
}

func (t texts) pageError(name string) string {
	return t.variant("Seite ", "Page ") + name
}

// voltage renders a tenth-of-a-volt key such as "34" as "3,4 V" in German and
// "3.4 V" in English.
func (t texts) voltage(key string) string {
	v, err := strconv.Atoi(key)
	if err != nil || v <= 0 {
		return ""
	}
	if t.language == "en" {
		return fmt.Sprintf(" (approx. %d.%d V)", v/10, v%10)
	}
	return fmt.Sprintf(" (ca. %d,%d V)", v/10, v%10)
}

func (t texts) lowBattery(key string) string {
	return t.variant("Akku schwach – bitte laden", "Battery low – please charge") + t.voltage(key)
}

func (t texts) criticalBattery(key string) (string, string) {
	return t.variant("Akku fast leer", "Battery almost empty"),
		t.variant("Bitte jetzt laden", "Please charge now") + t.voltage(key)
}

// The device menu (ADR 0057). Its labels are short: they sit in cells of a
// small panel.
func (t texts) menuClose() string { return t.variant("X", "X") }

func (t texts) menuMore() string { return t.variant("…", "…") }

func (t texts) menuEmpty() string {
	return t.variant("Keine Aktionen", "No actions")
}

func (t texts) menuConfirm(title string) (string, string) {
	return title, t.variant("Home = ja, andere Taste = nein", "Home = yes, any other = no")
}

func (t texts) menuYes() string { return t.variant("Ja", "Yes") }

func (t texts) menuNo() string { return t.variant("Nein", "No") }

func (t texts) menuRunning(title string) (string, string) {
	return title, t.variant("Wird ausgeführt …", "Running …")
}

func (t texts) menuResult(ok bool, message string) (string, string) {
	title := t.variant("Erledigt", "Done")
	if !ok {
		title = t.variant("Fehlgeschlagen", "Failed")
	}
	return title, message
}

func (t texts) menuExpired() string {
	return t.variant("Menü geschlossen", "Menu closed")
}

func (t texts) menuChanged() string {
	return t.variant("Aktion nicht mehr im Plan", "Action no longer in the plan")
}

func (t texts) menuDenied() string {
	return t.variant("Aktionen sind für dieses Gerät gesperrt", "Actions are disabled for this device")
}

func (t texts) menuDisabled(reason string) string {
	return t.variant("Nicht verfügbar: ", "Not available: ") + reason
}
