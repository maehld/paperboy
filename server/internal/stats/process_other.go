//go:build !linux && !windows

package stats

// process cannot tell anything about a process on this platform: no resident
// set size and no CPU time (ADR 0104). The page leaves those fields empty.
func process(pid int) (Proc, bool) {
	return Proc{}, false
}
