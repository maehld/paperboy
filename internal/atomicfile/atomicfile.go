// Package atomicfile replaces a file in one step.
package atomicfile

import (
	"io/fs"
	"os"
	"path/filepath"
)

// WriteFile replaces path with data. It writes a unique temporary file in the
// target directory, gives it perm, and renames it over path. A reader sees the
// old file or the new one, never a mix, and two writers cannot interleave. The
// caller creates the directory; the name starts with a dot, so a temporary file
// does not show up as content.
func WriteFile(path string, data []byte, perm fs.FileMode) error {
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	f, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	cleanup := true
	defer func() {
		if cleanup {
			os.Remove(tmp)
		}
	}()
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}
