package archive

import (
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

func freeSpace(dir string) (int64, bool) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, false
	}
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, nil, nil); err != nil {
		return 0, false
	}
	return int64(free), true
}

// isReparsePoint: junctions are no symlinks to os.Lstat, but lead elsewhere all the same.
func isReparsePoint(info os.FileInfo) bool {
	d, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && d.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
