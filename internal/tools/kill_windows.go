package tools

import (
	"os/exec"
	"strconv"
)

// killTree kills the program with everything it started (taskkill /T).
func killTree(c *exec.Cmd) {
	c.Cancel = func() error {
		if c.Process == nil {
			return nil
		}
		k := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(c.Process.Pid))
		hideWindow(k)
		if err := k.Run(); err != nil {
			return c.Process.Kill()
		}
		return nil
	}
}
