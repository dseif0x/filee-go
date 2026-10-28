//go:build windows

package jobs

import (
	"os"
	"syscall"
	"time"
)

func creationTime(fi os.FileInfo) time.Time {
	if d, ok := fi.Sys().(*syscall.Win32FileAttributeData); ok {
		return time.Unix(0, d.CreationTime.Nanoseconds())
	}
	return time.Time{}
}

func setCreationTime(path string, t time.Time) error {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	// FILE_FLAG_BACKUP_SEMANTICS opens folders too.
	h, err := syscall.CreateFile(p, syscall.FILE_WRITE_ATTRIBUTES, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(h)
	ft := syscall.NsecToFiletime(t.UnixNano())
	return syscall.SetFileTime(h, &ft, nil, nil)
}
