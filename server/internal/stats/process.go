package stats

// Proc is what the operating system reports about one process. A field the
// platform cannot tell stays zero, so the page shows nothing instead of a
// wrong number.
type Proc struct {
	RSS        uint64  // resident set size in bytes
	CPUSeconds float64 // total CPU time since the process started

	// The observation of stage 0 of issue #306: what the process opened and
	// where it talks to. Empty where the platform cannot tell.
	Files      []string // targets of the process's descriptors, without sockets
	Conns      []string // sockets: "tcp → 1.2.3.4:443", "unix /run/dbus/…"
	ReadBytes  uint64   // bytes read from block devices and files
	WriteBytes uint64   // bytes written to block devices and files
}

// Process reports the resources of one process; ok is false when the platform
// cannot tell or the process is gone.
func Process(pid int) (Proc, bool) {
	return process(pid)
}
