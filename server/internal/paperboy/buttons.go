package paperboy

import "time"

// buttonHoldLong is the press duration from which a button counts as held
// (ADR 0067). The firmware reports the measured time and leaves the threshold
// to the server.
const buttonHoldLong = 700 * time.Millisecond

// pageRoles translates the buttons of a wake into the roles that page. A home
// press only syncs, so it never appears here. A long next or previous jumps to
// the first page of the plan, whatever the device's actions setting is
// (ADR 0161).
func pageRoles(buttons []string, holds []int) []string {
	out := make([]string, 0, len(buttons))
	for i, b := range buttons {
		if held(hold(holds, i)) {
			switch b {
			case buttonNext, buttonPrevious:
				out = append(out, buttonHome)
			}
			continue
		}
		if b != buttonHome {
			out = append(out, b)
		}
	}
	return out
}

// hold reads the hold_ms of the button at i.
func hold(holds []int, i int) int {
	if i < len(holds) {
		return holds[i]
	}
	return 0
}

// held reports whether a press lasted long enough to count as held.
func held(ms int) bool {
	return time.Duration(ms)*time.Millisecond >= buttonHoldLong
}
