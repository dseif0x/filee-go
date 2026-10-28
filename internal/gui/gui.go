// Package gui is the desktop app. The GUI agents fill it per OS (macOS first,
// with purego); every other OS builds this stub. cmd/filee calls Run for
// "filee app".
package gui

import (
	"errors"

	"github.com/dseif0x/filee-go/internal/app"
)

// ErrNotBuilt: this build has no GUI for its OS.
var ErrNotBuilt = errors.New("the Filee app is not built for this OS")

// Run starts the GUI on a and returns when it quits. Replace the stub by
// setting Runner from an OS-specific file (an init function).
func Run(a *app.App) error {
	if Runner != nil {
		return Runner(a)
	}
	return ErrNotBuilt
}

// Runner is the GUI of this build, nil when there is none.
var Runner func(*app.App) error
