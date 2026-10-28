package main

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dseif0x/filee-go/internal/app"
	"github.com/dseif0x/filee-go/internal/gui"
)

// Ctrl+C cancels the running conversion or stops watching; a second Ctrl+C
// ends the process at once.
func main() {
	ctx, stop := context.WithCancel(context.Background())
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt)
	go func() {
		<-signals
		cancelAll()
		stop()
		<-signals
		os.Exit(130)
	}()
	code := run(ctx, appArgs(os.Args[1:]), os.Stdout, os.Stderr, func() (*app.App, error) {
		return app.New(app.Options{Platform: gui.Platform()})
	})
	stop()
	os.Exit(code)
}

// appArgs are the arguments as the command line reads them. Started as
// Filee.app (Finder, the Dock, a login item) the executable gets no command,
// or only the -psn_ argument old macOS versions add: that starts the app.
func appArgs(args []string) []string {
	var kept []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-psn_") {
			kept = append(kept, a)
		}
	}
	if len(kept) == 0 && insideAppBundle() {
		return []string{"app"}
	}
	if len(kept) > 0 && strings.HasPrefix(kept[0], "-") && insideAppBundle() && !slices.Contains([]string{"-h", "--help", "-v", "--version"}, kept[0]) {
		// "Filee.app/Contents/MacOS/Filee --background" (the launch agent of older installs).
		return append([]string{"app"}, kept...)
	}
	return kept
}

// insideAppBundle: the executable is Something.app/Contents/MacOS/x.
func insideAppBundle() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	dir := filepath.Dir(exe)
	return filepath.Base(dir) == "MacOS" && filepath.Base(filepath.Dir(dir)) == "Contents" &&
		strings.HasSuffix(filepath.Dir(filepath.Dir(dir)), ".app")
}
