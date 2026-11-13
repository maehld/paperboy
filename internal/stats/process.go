package stats

// Proc is what the operating system reports about one process. A field the
// platform cannot tell stays zero, so the page shows nothing instead of a
// wrong number.
type Proc struct {
	RSS        uint64  // resident set size in bytes
	CPUSeconds float64 // total CPU time since the process started
}

// Process reports the resources of one process; ok is false when the platform
// cannot tell or the process is gone.
func Process(pid int) (Proc, bool) {
	return process(pid)
}
