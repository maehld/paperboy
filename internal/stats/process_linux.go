//go:build linux

package stats

import (
	"encoding/hex"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
)

// userHZ is Linux' USER_HZ, the unit of the utime and stime fields of
// /proc/<pid>/stat. The kernel keeps it at 100 on every architecture Paperboy
// runs on.
const userHZ = 100

// Limits keep one runaway plugin from filling the page: a process that opens
// thousands of files shows the first fifty.
const (
	maxFiles = 50
	maxConns = 50
)

// process reads the numbers of one process: /proc/<pid>/statm and stat for
// memory and CPU, fd for the files and sockets, io for the disk traffic.
func process(pid int) (Proc, bool) {
	var p Proc
	dir := "/proc/" + strconv.Itoa(pid)
	ok := false
	if data, err := os.ReadFile(dir + "/statm"); err == nil {
		// size resident shared text lib data dt, in pages
		if fields := strings.Fields(string(data)); len(fields) > 1 {
			if pages, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				p.RSS = pages * uint64(os.Getpagesize())
				ok = true
			}
		}
	}
	if data, err := os.ReadFile(dir + "/stat"); err == nil {
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
	if files, conns, found := observe(dir); found {
		p.Files, p.Conns = files, conns
		ok = true
	}
	if data, err := os.ReadFile(dir + "/io"); err == nil {
		p.ReadBytes = ioBytes(string(data), "read_bytes:")
		p.WriteBytes = ioBytes(string(data), "write_bytes:")
	}
	return p, ok
}

// observe reads the descriptors of the process and matches the sockets among
// them against the kernel's socket tables.
func observe(dir string) (files, conns []string, ok bool) {
	entries, err := os.ReadDir(dir + "/fd")
	if err != nil {
		return nil, nil, false
	}
	seen := map[string]bool{}
	sockets := map[string]bool{}
	for _, e := range entries {
		target, err := os.Readlink(dir + "/fd/" + e.Name())
		if err != nil || target == "" {
			continue
		}
		if inode, isSocket := strings.CutPrefix(target, "socket:["); isSocket {
			sockets[strings.TrimSuffix(inode, "]")] = true
			continue
		}
		if strings.HasPrefix(target, "pipe:[") {
			// The protocol pipes of Paperboy are plumbing, not a file the
			// operator wants to see.
			continue
		}
		if !seen[target] {
			seen[target] = true
			files = append(files, target)
		}
	}
	sort.Strings(files)
	if len(files) > maxFiles {
		files = files[:maxFiles]
	}
	return files, socketNames(sockets), true
}

// socketNames names the sockets the process holds: the remote peer of a TCP or
// UDP socket, the listening address, or the path of a Unix socket.
func socketNames(inodes map[string]bool) []string {
	if len(inodes) == 0 {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, table := range []struct{ path, proto string }{
		{"/proc/net/tcp", "tcp"}, {"/proc/net/tcp6", "tcp6"},
		{"/proc/net/udp", "udp"}, {"/proc/net/udp6", "udp6"},
	} {
		data, err := os.ReadFile(table.path)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for _, line := range lines[1:] {
			// sl local rem st tx:rx tr:when retrnsmt uid timeout inode
			f := strings.Fields(line)
			if len(f) < 10 || !inodes[f[9]] {
				continue
			}
			local, _ := socketAddr(f[1])
			remote, remotePort := socketAddr(f[2])
			if f[3] == "0A" && strings.HasPrefix(table.proto, "tcp") {
				add(table.proto + " listen " + local)
				continue
			}
			if remotePort > 0 {
				add(table.proto + " → " + remote)
			} else {
				add(table.proto + " " + local)
			}
		}
	}
	if data, err := os.ReadFile("/proc/net/unix"); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines[1:] {
			// Num RefCount Protocol Flags Type St Inode [Path]
			f := strings.Fields(line)
			if len(f) < 7 || !inodes[f[6]] {
				continue
			}
			if len(f) > 7 {
				add("unix " + f[7])
			} else {
				add("unix (unnamed)")
			}
		}
	}
	sort.Strings(out)
	if len(out) > maxConns {
		out = out[:maxConns]
	}
	return out
}

// socketAddr turns one address of /proc/net into a printable peer and its
// port. The kernel writes the bytes of an address in host order: reversed for
// IPv4, four little-endian words for IPv6.
func socketAddr(s string) (string, int) {
	host, portHex, ok := strings.Cut(s, ":")
	if !ok {
		return s, 0
	}
	port, _ := strconv.ParseUint(portHex, 16, 16)
	raw, err := hex.DecodeString(host)
	if err != nil {
		return s, int(port)
	}
	switch len(raw) {
	case 4:
		return net.JoinHostPort(net.IPv4(raw[3], raw[2], raw[1], raw[0]).String(), strconv.Itoa(int(port))), int(port)
	case 16:
		var b [16]byte
		for i := 0; i < 4; i++ {
			b[i*4+0] = raw[i*4+3]
			b[i*4+1] = raw[i*4+2]
			b[i*4+2] = raw[i*4+1]
			b[i*4+3] = raw[i*4+0]
		}
		return net.JoinHostPort(net.IP(b[:]).String(), strconv.Itoa(int(port))), int(port)
	}
	return s, int(port)
}

// ioBytes reads one counter of /proc/<pid>/io.
func ioBytes(data, key string) uint64 {
	for _, line := range strings.Split(data, "\n") {
		name, value, ok := strings.Cut(line, ":")
		if ok && name+":" == key {
			n, _ := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
			return n
		}
	}
	return 0
}
