//go:build windows

package widget

import "os/exec"

// exitSignal cannot name a signal on Windows: a process ends with an exit code
// (issue #306).
func exitSignal(*exec.ExitError) string { return "" }
