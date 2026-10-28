//go:build darwin

package gui

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/url"

	"github.com/ebitengine/purego/objc"

	"github.com/dseif0x/filee-go/internal/macos/ns"
	"github.com/dseif0x/filee-go/internal/presets"
)

// Windows of the local web UI (internal/webui): the settings and the tool
// editors, each an NSWindow holding a WKWebView.

const (
	styleTitled         = 1 << 0
	styleClosable       = 1 << 1
	styleMiniaturizable = 1 << 2
	styleResizable      = 1 << 3
	autoresizeWidth     = 1 << 1
	autoresizeHeight    = 1 << 4
)

type webWindows struct {
	m        *macApp
	settings ns.ID // the settings window, kept for reuse
	webview  ns.ID
}

func newWebWindows(m *macApp) *webWindows { return &webWindows{m: m} }

// newWebWindow makes a titled window of w×h holding a web view.
func newWebWindow(title string, w, h float64) (window, webview ns.ID) {
	rect := ns.Rect{Size: ns.Size{Width: w, Height: h}}
	window = ns.Send(ns.Alloc("NSWindow"), "initWithContentRect:styleMask:backing:defer:",
		rect, uint(styleTitled|styleClosable|styleMiniaturizable|styleResizable), uint(nsBackingBuffered), false)
	ns.Send(window, "setReleasedWhenClosed:", false)
	ns.Send(window, "setTitle:", ns.String(title))
	ns.Send(window, "setMinSize:", ns.Size{Width: 640, Height: 480})
	if ns.Class("WKWebView") != 0 {
		config := ns.Send(ns.New("WKWebViewConfiguration"), "autorelease")
		webview = ns.Send(ns.Alloc("WKWebView"), "initWithFrame:configuration:", rect, config)
		ns.Send(webview, "setAutoresizingMask:", uint(autoresizeWidth|autoresizeHeight))
		ns.Send(window, "setContentView:", webview)
		ns.Release(webview)
	}
	ns.Send(window, "center")
	return window, webview
}

func load(webview ns.ID, address string) {
	u := ns.URL(address)
	if webview == 0 || u == 0 {
		return
	}
	ns.Send(webview, "loadRequest:", ns.Send(ns.Class("NSURLRequest"), "requestWithURL:", u))
}

func (w *webWindows) front(window ns.ID) {
	ns.Send(w.m.nsapp, "activateIgnoringOtherApps:", true)
	ns.Send(window, "makeKeyAndOrderFront:", objc.ID(0))
}

// showSettings shows the settings window ("" for its first page).
func (w *webWindows) showSettings(page string) {
	base, err := w.m.webBase()
	if err != nil {
		w.m.alert(w.m.app.Text("app.name") + ": " + err.Error())
		return
	}
	if w.settings == 0 {
		w.settings, w.webview = newWebWindow("Filee", 1040, 720)
	}
	address := base + "/"
	if page != "" {
		address = base + "/#/" + page
	}
	load(w.webview, address)
	w.front(w.settings)
}

// ShowTool opens a tool editor of the web UI for the files. The job token,
// the preset and the files go in the address (the web UI's contract names
// the token; preset and files are what the editor needs to open them).
func (m *macApp) ShowTool(tool string, p *presets.Preset, files []string) {
	base, err := m.webBase()
	if err != nil {
		m.alert(err.Error())
		return
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	list, _ := json.Marshal(files)
	q := url.Values{"job": {hex.EncodeToString(b)}, "preset": {p.ID}, "files": {string(list)}}
	window, webview := newWebWindow(m.app.Texts().PresetName(p), 1100, 760)
	load(webview, base+"/#/tool/"+tool+"?"+q.Encode())
	m.windows.front(window)
}

// ChooseFolder asks for a folder to save into (a preset set to "Ask every time").
func (m *macApp) ChooseFolder(title string) (string, bool) {
	panel := ns.Send(ns.Class("NSOpenPanel"), "openPanel")
	ns.Send(panel, "setCanChooseDirectories:", true)
	ns.Send(panel, "setCanChooseFiles:", false)
	ns.Send(panel, "setCanCreateDirectories:", true)
	ns.Send(panel, "setAllowsMultipleSelection:", false)
	ns.Send(panel, "setMessage:", ns.String(title))
	ns.Send(m.nsapp, "activateIgnoringOtherApps:", true)
	if ns.Int(panel, "runModal") != 1 { // NSModalResponseOK
		return "", false
	}
	u := ns.Send(panel, "URL")
	if u == 0 {
		return "", false
	}
	return ns.GoString(ns.Send(u, "path")), true
}

// alert shows a message in a small window of its own.
func (m *macApp) alert(text string) {
	a := ns.Send(ns.New("NSAlert"), "autorelease")
	ns.Send(a, "setMessageText:", ns.String("Filee"))
	ns.Send(a, "setInformativeText:", ns.String(text))
	ns.Send(m.nsapp, "activateIgnoringOtherApps:", true)
	ns.Send(a, "runModal")
}
