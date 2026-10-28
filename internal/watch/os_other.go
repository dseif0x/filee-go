//go:build !windows && !darwin

package watch

import "os"

// Linux doesn't lock files being written: only the wait tells.
func canOpenExclusively(string) bool { return true }

func beingCopiedByFinder(string) bool { return false }

func isHiddenOrSystem(string, os.FileInfo) bool { return false }

func hide(string) {}
