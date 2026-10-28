//go:build darwin

package watch

import (
	"os"
	"syscall"
	"time"
)

// macOS doesn't lock files being written: only the wait and Finder's date tell.
func canOpenExclusively(string) bool { return true }

// beingCopiedByFinder: Finder gives a file it is still copying the creation date 24 January 1984.
func beingCopiedByFinder(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return true
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	born := time.Unix(st.Birthtimespec.Sec, st.Birthtimespec.Nsec).Local()
	y, m, d := born.Date()
	return y == 1984 && m == time.January && d == 24
}

// isHiddenOrSystem: the UF_HIDDEN flag Finder uses (dot files are left out by name already).
func isHiddenOrSystem(_ string, fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Flags&0x8000 != 0
}

func hide(string) {}
