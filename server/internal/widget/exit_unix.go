//go:build !windows

package widget

import (
	"os/exec"
	"syscall"
)

// exitSignal names the signal that killed a plugin process. SIGSYS is the
// interesting one: on Linux the kernel sends it when a sandbox or seccomp
// filter denies a system call, which is why a plugin died (issue #306).
func exitSignal(err *exec.ExitError) string {
	status, ok := err.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return ""
	}
	switch sig := status.Signal(); sig {
	case syscall.SIGSYS:
		return "SIGSYS"
	case syscall.SIGKILL:
		return "SIGKILL"
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGSEGV:
		return "SIGSEGV"
	case syscall.SIGABRT:
		return "SIGABRT"
	}
	return status.Signal().String()
}
