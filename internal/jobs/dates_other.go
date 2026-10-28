//go:build !windows

package jobs

import (
	"os"
	"time"
)

// creationTime: macOS and Linux have no portable way to set a file's birth
// time, so only the modification time is copied there.
func creationTime(os.FileInfo) time.Time { return time.Time{} }

func setCreationTime(string, time.Time) error { return nil }
