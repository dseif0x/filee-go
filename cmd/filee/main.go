package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/dseif0x/filee-go/internal/app"
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
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, func() (*app.App, error) { return app.New(app.Options{}) })
	stop()
	os.Exit(code)
}
