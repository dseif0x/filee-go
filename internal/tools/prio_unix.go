//go:build linux || darwin || freebsd || netbsd || openbsd

package tools

import (
	"os/exec"
	"syscall"
)

func lowerAtStart(*exec.Cmd) {}

// lowerAfterStart sets a nice value of 10; programs it starts inherit it.
func lowerAfterStart(c *exec.Cmd) {
	if c.Process != nil {
		_ = syscall.Setpriority(syscall.PRIO_PROCESS, c.Process.Pid, 10)
	}
}
