//go:build linux || darwin || freebsd || netbsd || openbsd

package media

import "syscall"

// freeSpace is the free bytes on the drive of path; ok is false where that can't be read.
func freeSpace(path string) (int64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, false
	}
	return int64(uint64(st.Bavail) * uint64(st.Bsize)), true
}

func hideFolder(string) {} // the leading dot hides it
