package tools

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps console programs from flashing a window.
func hideWindow(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
