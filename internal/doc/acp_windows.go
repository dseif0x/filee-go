package doc

import "syscall"

var getACP = syscall.NewLazyDLL("kernel32.dll").NewProc("GetACP")

func ansiCodePage() int {
	if getACP.Find() != nil {
		return 0
	}
	r, _, _ := getACP.Call()
	return int(r)
}
