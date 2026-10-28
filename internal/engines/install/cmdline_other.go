//go:build !windows

package install

import "os/exec"

func setRawCommandLine(*exec.Cmd, string) {}
