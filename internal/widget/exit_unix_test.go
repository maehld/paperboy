//go:build !windows

package widget

import (
	"os/exec"
	"testing"
	"time"
)

// TestExitInfoNamesASignal checks that a plugin killed by SIGSYS reports the
// signal name, so the runtime page can say why it died (issue #306).
func TestExitInfoNamesASignal(t *testing.T) {
	cmd := exec.Command("sh", "-c", "kill -SYS $$")
	err := cmd.Run()
	info := exitInfo(err, time.Now())
	if info.Signal != "SIGSYS" {
		t.Fatalf("signal = %q (error %v)", info.Signal, info.Error)
	}
	if info.Code != -1 {
		t.Errorf("exit code = %d, want -1 for a signal", info.Code)
	}

	cmd = exec.Command("sh", "-c", "exit 7")
	info = exitInfo(cmd.Run(), time.Now())
	if info.Code != 7 || info.Signal != "" {
		t.Fatalf("exit code = %d, signal = %q", info.Code, info.Signal)
	}
	if info = exitInfo(nil, time.Now()); info.Code != 0 || info.Error != "" {
		t.Fatalf("clean exit: %+v", info)
	}
}
