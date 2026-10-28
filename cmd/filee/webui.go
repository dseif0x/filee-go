package main

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"

	"github.com/dseif0x/filee-go/internal/app"
	"github.com/dseif0x/filee-go/internal/webui"
)

// webCmd serves the settings and tools as a local web page and opens it in
// the browser; the watch folders run until Ctrl+C.
func webCmd(ctx context.Context, o *options, a *app.App, stdout, stderr io.Writer) int {
	base, err := webui.Start(ctx, a)
	if err != nil {
		fmt.Fprintf(stderr, "filee: %v\n", err)
		return exitUsage
	}
	a.StartWatchers()
	page := "home"
	if len(o.inputs) > 0 {
		page = strings.Trim(strings.ToLower(o.inputs[0]), "/#")
	}
	url := base + "/#/" + page
	fmt.Fprintf(stdout, "Filee settings: %s\nPress Ctrl+C to stop.\n", url)
	if !o.noBrowser {
		if err := openBrowser(url); err != nil {
			fmt.Fprintf(stderr, "filee: could not open the browser (%v); open the address above.\n", err)
		}
	}
	<-ctx.Done()
	return exitSuccess
}

// openBrowser is a var so tests don't start one.
var openBrowser = func(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
