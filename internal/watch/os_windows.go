//go:build windows

package watch

import (
	"os"
	"syscall"
)

// canOpenExclusively: a writer still holding the file (download, copy) makes an open without sharing fail.
func canOpenExclusively(path string) bool {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return false
	}
	_ = syscall.CloseHandle(h)
	return true
}

func beingCopiedByFinder(string) bool { return false }

func isHiddenOrSystem(path string, _ os.FileInfo) bool {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return true
	}
	a, err := syscall.GetFileAttributes(p)
	if err != nil {
		return true
	}
	return a&(syscall.FILE_ATTRIBUTE_HIDDEN|syscall.FILE_ATTRIBUTE_SYSTEM) != 0
}

func hide(path string) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	if a, err := syscall.GetFileAttributes(p); err == nil {
		_ = syscall.SetFileAttributes(p, a|syscall.FILE_ATTRIBUTE_HIDDEN)
	}
}
