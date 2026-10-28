// Package webui is the local web UI of the settings and the tool editors
// (built in parallel by another agent). This file only fixes the contract the
// macOS app relies on, and goes away when the real package lands:
//
//	baseURL + "/#/<page>"                    a settings page
//	baseURL + "/#/tool/<tool>?job=<token>…"  a tool editor (crop, clip, hide,
//	                                         image-edit, pdf-pages, print)
package webui

import (
	"context"
	"errors"

	"github.com/dseif0x/filee-go/internal/app"
)

// Start serves the web UI for a on localhost and returns its address.
func Start(ctx context.Context, a *app.App) (baseURL string, err error) {
	return "", errors.New("the web UI is not part of this build yet")
}
