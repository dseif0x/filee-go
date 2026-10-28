//go:build linux || darwin || freebsd || netbsd || openbsd

package tools

import (
	"os/exec"
	"syscall"
)

// killTree makes the program a process group leader and kills the whole group
// when the run ends early, so children (a shell's commands, LibreOffice's
// helper) don't keep the output pipes open.
func killTree(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.Setpgid = true
	c.Cancel = func() error {
		if c.Process == nil {
			return nil
		}
		return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
}
