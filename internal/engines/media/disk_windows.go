package media

import (
	"syscall"
	"unsafe"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpace = kernel32.NewProc("GetDiskFreeSpaceExW")
	setAttributes    = kernel32.NewProc("SetFileAttributesW")
)

func freeSpace(path string) (int64, bool) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, false
	}
	var free uint64
	r, _, _ := getDiskFreeSpace.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&free)), 0, 0)
	if r == 0 {
		return 0, false
	}
	return int64(free), true
}

func hideFolder(path string) {
	if p, err := syscall.UTF16PtrFromString(path); err == nil {
		setAttributes.Call(uintptr(unsafe.Pointer(p)), 0x2) // FILE_ATTRIBUTE_HIDDEN
	}
}
