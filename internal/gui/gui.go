// Package gui is the desktop app. macOS has a native one (purego, no cgo);
// every other OS builds this stub. cmd/filee calls Run for "filee app".
package gui

import (
	"errors"

	"github.com/dseif0x/filee-go/internal/app"
	"github.com/dseif0x/filee-go/internal/platform"
)

// ErrNotBuilt: this build has no GUI for its OS.
var ErrNotBuilt = errors.New("the Filee app is not built for this OS")

// Options are the command line of "filee app".
type Options struct {
	// Background: started at sign-in, no window.
	Background bool
	// SelfTest exercises the native bindings without user interaction and
	// prints what worked (to paste into a bug report).
	SelfTest bool
	// Files open the donut for these files (Finder's "Open With", a later launch).
	Files []string
}

// Run starts the GUI on a and returns when it quits.
func Run(a *app.App) error { return RunWith(a, Options{}) }

// RunWith is Run with options.
func RunWith(a *app.App, o Options) error {
	if Runner != nil {
		return Runner(a, o)
	}
	return ErrNotBuilt
}

// Runner is the GUI of this build, nil when there is none (set by an OS file's init).
var Runner func(*app.App, Options) error

// Platform is this OS's services for app.New (nil for platform.Null).
func Platform() platform.Services {
	if PlatformFactory != nil {
		return PlatformFactory()
	}
	return nil
}

// PlatformFactory makes the OS services, nil when there are none.
var PlatformFactory func() platform.Services
