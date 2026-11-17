package widget

import (
	"errors"
	"os/exec"
	"time"
)

// exitInfo describes how a plugin process ended (issue #306). A process the
// operator stopped or restarted also ends here, so the page names the code, the
// signal or nothing at all.
func exitInfo(err error, at time.Time) ExitInfo {
	info := ExitInfo{At: at}
	if err == nil {
		return info
	}
	info.Error = err.Error()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		info.Code = ee.ExitCode()
		if name := exitSignal(ee); name != "" {
			info.Signal = name
		}
	}
	return info
}
