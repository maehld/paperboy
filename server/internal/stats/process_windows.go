//go:build windows

package stats

import (
	"syscall"
	"unsafe"
)

// The Windows API calls come straight from the system libraries, so Paperboy
// needs no dependency for them (ADR 0104).
var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	psapi    = syscall.NewLazyDLL("psapi.dll")

	procOpenProcess          = kernel32.NewProc("OpenProcess")
	procCloseHandle          = kernel32.NewProc("CloseHandle")
	procGetProcessTimes      = kernel32.NewProc("GetProcessTimes")
	procGetProcessMemoryInfo = psapi.NewProc("GetProcessMemoryInfo")
)

const (
	processVMRead                  = 0x0010
	processQueryInformation        = 0x0400
	processQueryLimitedInformation = 0x1000
)

// memoryCounters is PROCESS_MEMORY_COUNTERS.
type memoryCounters struct {
	cb                         uint32
	pageFaultCount             uint32
	peakWorkingSetSize         uintptr
	workingSetSize             uintptr
	quotaPeakPagedPoolUsage    uintptr
	quotaPagedPoolUsage        uintptr
	quotaPeakNonPagedPoolUsage uintptr
	quotaNonPagedPoolUsage     uintptr
	pagefileUsage              uintptr
	peakPagefileUsage          uintptr
}

// filetime is FILETIME: 100 ns intervals since 1601, in two halves.
type filetime struct {
	low  uint32
	high uint32
}

func (f filetime) seconds() float64 {
	return float64(uint64(f.high)<<32|uint64(f.low)) / 1e7
}

// process reads the working set and the CPU time of one process.
func process(pid int) (Proc, bool) {
	var p Proc
	access := uintptr(processQueryLimitedInformation | processQueryInformation | processVMRead)
	h, _, _ := procOpenProcess.Call(access, 0, uintptr(pid))
	if h == 0 {
		return p, false
	}
	defer procCloseHandle.Call(h)
	handle := syscall.Handle(h)
	ok := false
	mem := memoryCounters{cb: uint32(unsafe.Sizeof(memoryCounters{}))}
	if r1, _, _ := procGetProcessMemoryInfo.Call(uintptr(handle), uintptr(unsafe.Pointer(&mem)),
		uintptr(mem.cb)); r1 != 0 {
		p.RSS = uint64(mem.workingSetSize)
		ok = true
	}
	// GetProcessTimes wants a creation, an exit, a kernel and a user time; the
	// last two are what the process used.
	var created, exited, kernel, user filetime
	if r1, _, _ := procGetProcessTimes.Call(uintptr(handle), uintptr(unsafe.Pointer(&created)),
		uintptr(unsafe.Pointer(&exited)), uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user))); r1 != 0 {
		p.CPUSeconds = kernel.seconds() + user.seconds()
		ok = true
	}
	return p, ok
}
