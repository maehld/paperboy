//go:build linux

package stats

import (
	"os"
	"strconv"
	"strings"
)

// userHZ is Linux' USER_HZ, the unit of the utime and stime fields of
// /proc/<pid>/stat. The kernel keeps it at 100 on every architecture Paperboy
// runs on.
const userHZ = 100

// process reads /proc/<pid>/statm and /proc/<pid>/stat: the resident pages and
// the CPU ticks the process has used.
func process(pid int) (Proc, bool) {
	var p Proc
	ok := false
	if data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/statm"); err == nil {
		// size resident shared text lib data dt, in pages
		if fields := strings.Fields(string(data)); len(fields) > 1 {
			if pages, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				p.RSS = pages * uint64(os.Getpagesize())
				ok = true
			}
		}
	}
	if data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil {
		// The command name may contain spaces and parentheses, so the fields
		// start after the last ')': utime is field 14 of the line, stime field
		// 15, which is index 11 and 12 of the rest.
		if i := strings.LastIndexByte(string(data), ')'); i >= 0 {
			if fields := strings.Fields(string(data)[i+1:]); len(fields) > 12 {
				utime, err1 := strconv.ParseUint(fields[11], 10, 64)
				stime, err2 := strconv.ParseUint(fields[12], 10, 64)
				if err1 == nil && err2 == nil {
					p.CPUSeconds = float64(utime+stime) / userHZ
					ok = true
				}
			}
		}
	}
	return p, ok
}
