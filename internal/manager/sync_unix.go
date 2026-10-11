//go:build !windows

package manager

import "os"

// syncDir flushes directory metadata on POSIX systems.
func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
