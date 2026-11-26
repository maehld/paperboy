package paperboy

import "time"

// buttonHoldLong is the press duration from which a button counts as held
// (ADR 0067). The firmware reports the measured time and leaves the threshold
// to the server.
const buttonHoldLong = 700 * time.Millisecond

// roleFirst and roleLast are the page roles of a long press. They never reach
// the wire, which knows only the button roles of ADR 0042.
const (
	roleFirst = "first"
	roleLast  = "last"
)

// pageRoles translates the buttons of a wake into the roles that page. A home
// press only syncs, so it never appears here. A long previous jumps to the
// first page of the list and a long next to the last, whatever the device's
// actions setting is (ADR 0161, 0170).
func pageRoles(buttons []string, holds []int) []string {
	out := make([]string, 0, len(buttons))
	for i, b := range buttons {
		if held(hold(holds, i)) {
			switch b {
			case buttonPrevious:
				out = append(out, roleFirst)
			case buttonNext:
				out = append(out, roleLast)
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

// homePress reports a home press and whether it was held.
func homePress(buttons []string, holds []int) (pressed, long bool) {
	for i, b := range buttons {
		if b != buttonHome {
			continue
		}
		pressed = true
		long = long || held(hold(holds, i))
	}
	return pressed, long
}

// pagePressed reports whether a wake carried next or previous. Either leaves
// the touch action mode and pages (ADR 0162).
func pagePressed(buttons []string) bool {
	for _, b := range buttons {
		if b == buttonNext || b == buttonPrevious {
			return true
		}
	}
	return false
}

// selectionStep is the direction the key selection moves: next is +1,
// previous is -1. A wake with both takes the last press in order (ADR 0163).
func selectionStep(buttons []string) int {
	step := 0
	for _, b := range buttons {
		switch b {
		case buttonNext:
			step = 1
		case buttonPrevious:
			step = -1
		}
	}
	return step
}
