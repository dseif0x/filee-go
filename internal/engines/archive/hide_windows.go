package archive

import "golang.org/x/sys/windows"

// hide sets the hidden attribute, which Explorer goes by.
func hide(p string) {
	if ptr, err := windows.UTF16PtrFromString(p); err == nil {
		if attrs, err := windows.GetFileAttributes(ptr); err == nil {
			windows.SetFileAttributes(ptr, attrs|windows.FILE_ATTRIBUTE_HIDDEN)
		}
	}
}
