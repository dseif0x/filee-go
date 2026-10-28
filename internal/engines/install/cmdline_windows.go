package install

import (
	"os/exec"
	"syscall"
)

func setRawCommandLine(c *exec.Cmd, line string) {
	c.SysProcAttr = &syscall.SysProcAttr{CmdLine: line, HideWindow: true}
}
