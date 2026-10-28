//go:build !(linux || darwin || freebsd || netbsd || openbsd || windows)

package tools

import "os/exec"

func killTree(*exec.Cmd) {}
