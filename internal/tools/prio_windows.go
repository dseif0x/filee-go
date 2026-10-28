package tools

import (
	"os/exec"
	"syscall"
)

// lowerAtStart starts the program below normal priority.
func lowerAtStart(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.CreationFlags |= 0x00004000 // BELOW_NORMAL_PRIORITY_CLASS
}

func lowerAfterStart(*exec.Cmd) {}
