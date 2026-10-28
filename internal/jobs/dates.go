package jobs

import (
	"os"
	"time"
)

// fileDates are a file's creation and modification times. Created is zero
// where the system doesn't say (or Filee can't set it): only Windows sets it.
type fileDates struct {
	Created, Modified time.Time
}

// datesOf returns the dates of source, or nil when they can't be read.
func datesOf(source string) *fileDates {
	fi, err := os.Stat(source)
	if err != nil {
		return nil
	}
	return &fileDates{Created: creationTime(fi), Modified: fi.ModTime()}
}

func setDates(path string, d fileDates) error {
	if err := os.Chtimes(path, d.Modified, d.Modified); err != nil {
		return err
	}
	if !d.Created.IsZero() {
		return setCreationTime(path, d.Created)
	}
	return nil
}
