//go:build !windows

package archive

import (
	"os"

	"golang.org/x/sys/unix"
)

func freeSpace(dir string) (int64, bool) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, false
	}
	return int64(st.Bavail) * int64(st.Bsize), true
}

func isReparsePoint(os.FileInfo) bool { return false }
