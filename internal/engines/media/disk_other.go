//go:build !(linux || darwin || freebsd || netbsd || openbsd || windows)

package media

func freeSpace(string) (int64, bool) { return 0, false }
func hideFolder(string)              {}
