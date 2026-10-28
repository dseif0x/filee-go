//go:build !(linux || darwin || freebsd || netbsd || openbsd || windows)

package tools

import "os/exec"

func lowerAtStart(*exec.Cmd)    {}
func lowerAfterStart(*exec.Cmd) {}
