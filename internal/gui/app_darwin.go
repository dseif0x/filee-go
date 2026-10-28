//go:build darwin

package gui

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ebitengine/purego/objc"

	"github.com/dseif0x/filee-go/internal/app"
	"github.com/dseif0x/filee-go/internal/gui/radial"
	"github.com/dseif0x/filee-go/internal/i18n"
	"github.com/dseif0x/filee-go/internal/macos/macplatform"
	"github.com/dseif0x/filee-go/internal/macos/ns"
	"github.com/dseif0x/filee-go/internal/platform"
	"github.com/dseif0x/filee-go/internal/settings"
	"github.com/dseif0x/filee-go/internal/webui"
)

// AppKit runs on the main thread: the main goroutine stays on it from here on.
func init() {
	runtime.LockOSThread()
	Runner = runMac
	PlatformFactory = func() platform.Services {
		s, err := macplatform.New()
		if err != nil {
			return nil
		}
		return s
	}
}

// macApp is the running app. Its fields are used on the main thread only,
// unless noted.
type macApp struct {
	app      *app.App
	opts     Options
	log      *slog.Logger
	mac      *macplatform.Services
	nsapp    ns.ID
	radial   *radial.Controller
	overlay  *overlay
	status   *statusItem
	triggers *triggers
	windows  *webWindows
	notifier *notifier

	webMu  sync.Mutex
	webURL string // the settings UI, "" until (or unless) it started
	webErr error

	applied struct {
		startup, menu *bool
		dock          *bool
	}
	pendingFiles []string
	pendingTimer func()

	// quitting is set by the menu bar's Quit: only then does Filee exit
	// (and when the system logs out or shuts down).
	quitting bool
	// inMenuBarOnly: the Dock icon was closed; Filee lives on in the menu bar.
	inMenuBarOnly bool
}

var current *macApp

func runMac(a *app.App, o Options) error {
	if err := ns.Load(); err != nil {
		return err
	}
	if o.SelfTest {
		return selfTest(a, os.Stdout)
	}
	mac, ok := a.Platform().(*macplatform.Services)
	if !ok {
		var err error
		if mac, err = macplatform.New(); err != nil {
			return err
		}
	}
	m := &macApp{app: a, opts: o, mac: mac, log: logger()}
	current = m
	ns.PanicHandler = func(r any) { m.log.Error("panic on the main thread", "panic", r, "stack", stack()) }
	// An app started from Finder has no LANG: take the language from System Settings.
	if os.Getenv("LANG") == "" && os.Getenv("LC_ALL") == "" {
		if lang := macplatform.PreferredLanguage(); lang != "" {
			_ = os.Setenv("LANG", strings.ReplaceAll(lang, "-", "_"))
			i18n.SetLanguage(a.Settings().Language)
		}
	}
	go loadFallbackFonts()
	m.start()
	ns.Send(m.nsapp, "run")
	return nil
}

func stack() string {
	b := make([]byte, 8192)
	return string(b[:runtime.Stack(b, false)])
}

// logger writes to ~/Library/Logs/Filee/filee.log (and stderr when FILEE_LOG_STDERR is set).
func logger() *slog.Logger {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library", "Logs", "Filee")
	var w io.Writer = io.Discard
	if err := os.MkdirAll(dir, 0o755); err == nil {
		path := filepath.Join(dir, "filee.log")
		if fi, err := os.Stat(path); err == nil && fi.Size() > 4<<20 {
			_ = os.Rename(path, path+".1")
		}
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			w = f
		}
	}
	if os.Getenv("FILEE_LOG_STDERR") != "" {
		w = io.MultiWriter(w, os.Stderr)
	}
	return slog.New(slog.NewTextHandler(w, nil))
}

func (m *macApp) start() {
	m.nsapp = ns.Send(ns.Class("NSApplication"), "sharedApplication")
	// An agent until the settings ask for the Dock icon (Info.plist says LSUIElement too).
	ns.Send(m.nsapp, "setActivationPolicy:", 1)
	ns.Send(m.nsapp, "setDelegate:", newAppDelegate())
	m.buildMainMenu()
	// "Convert with Filee" in Finder: registered first, so a request made while Filee wasn't running arrives.
	registerServices(func(files []string) { m.openFiles(files) }, func(types []string) {
		m.log.Warn("Convert with Filee was picked, but the request named no existing file", "types", types)
	})
	m.windows = newWebWindows(m)
	m.notifier = newNotifier(m)
	m.radial = radial.New(m.app, m)
	m.overlay = newOverlay(m)
	m.status = newStatusItem(m)
	m.triggers = newTriggers(m)
	m.applySettings()
	m.triggers.start()
	m.app.Subscribe(func(e app.Event) {
		switch e.Kind {
		case app.JobUpdated, app.JobFinished:
			if e.Job != nil {
				s := *e.Job
				ns.OnMain(func() { m.radial.JobChanged(s) })
			}
		case app.SettingsChanged:
			ns.OnMain(m.applySettings)
		}
	})
	m.app.StartWatchers()
	go m.startWebUI()
	m.log.Info("Filee started", "version", app.Version, "bundle", macplatform.InBundle())
	if len(m.opts.Files) > 0 {
		m.openFiles(m.opts.Files)
	} else if !m.opts.Background && !m.app.Settings().FirstRunCompleted {
		ns.OnMain(func() { m.openSettings("home") })
	}
	if !m.opts.Background {
		m.askForPermissionOnce()
	}
}

// askForPermissionOnce shows macOS's Accessibility alert when a drag gesture
// is on and Filee hasn't asked before (once ever: after an update of the
// ad-hoc signed app the list can show Filee on while macOS still denies it).
func (m *macApp) askForPermissionOnce() {
	s := m.app.Settings()
	drag := slices.ContainsFunc(s.Triggers, func(t *settings.TriggerGesture) bool { return t.Enabled && t.Kind == settings.Drag })
	if m.mac.HasAccessibility() || s.AccessibilityPromptShown || !drag {
		return
	}
	m.after(1500*time.Millisecond, func() {
		s := m.app.Settings()
		s.AccessibilityPromptShown = true
		if err := m.app.SaveSettings(s); err != nil {
			m.log.Warn("could not save settings", "error", err)
		}
		m.mac.RequestAccessibility()
	})
}

func (m *macApp) startWebUI() {
	url, err := webui.Start(context.Background(), m.app)
	m.webMu.Lock()
	m.webURL, m.webErr = strings.TrimRight(url, "/"), err
	m.webMu.Unlock()
	if err != nil {
		m.log.Warn("the settings UI did not start", "error", err)
	}
}

// webBase is the settings UI's address, waiting a moment for it to start.
func (m *macApp) webBase() (string, error) {
	for range 50 {
		m.webMu.Lock()
		url, err := m.webURL, m.webErr
		m.webMu.Unlock()
		if url != "" || err != nil {
			return url, err
		}
		time.Sleep(20 * time.Millisecond)
	}
	return "", fmt.Errorf("the settings UI is still starting")
}

// applySettings applies what the settings drive (at start and on every save).
func (m *macApp) applySettings() {
	s := m.app.Settings()
	m.triggers.apply(s)
	m.status.refresh(s)
	m.overlay.applyTheme()
	if m.applied.dock == nil || *m.applied.dock != s.ShowInDock {
		show := s.ShowInDock
		// Starting at sign-in stays out of the Dock until a window opens.
		if !(m.applied.dock == nil && m.opts.Background) || !show {
			if m.mac.SetShowInDock(show) || !show {
				m.applied.dock = &show
			}
		}
	}
	// Login item and Finder service: only Filee.app registers them, and not a copy with its own data folder.
	if !macplatform.InBundle() || os.Getenv("FILEE_DATA_DIR") != "" {
		return
	}
	exe, _ := os.Executable()
	if m.applied.startup == nil || *m.applied.startup != s.StartWithSystem {
		v := s.StartWithSystem
		go func() {
			if err := m.mac.SetStartWithSystem(v, exe); err != nil {
				m.log.Warn("could not change start at login", "error", err)
			}
		}()
		m.applied.startup = &v
	}
	if m.applied.menu == nil || *m.applied.menu != s.ContextMenuEnabled {
		v := s.ContextMenuEnabled
		go func() {
			if err := m.mac.SetContextMenu(v, exe, m.app.Text("general.context_menu_label")); err != nil {
				m.log.Warn("could not change the Finder service", "error", err)
			}
		}()
		m.applied.menu = &v
	}
}

// openFiles opens the donut for files from another process (Services, Open
// With): a burst is collected for a moment, then the donut opens at the cursor.
func (m *macApp) openFiles(files []string) {
	m.pendingFiles = append(m.pendingFiles, files...)
	if m.pendingTimer != nil {
		m.pendingTimer()
	}
	m.pendingTimer = m.after(350*time.Millisecond, func() {
		files := slices.Compact(m.pendingFiles)
		m.pendingFiles, m.pendingTimer = nil, nil
		x, y := cursor()
		m.radial.ShowForFiles(x, y, files, true)
	})
}

// openSettings shows the settings window on a page.
func (m *macApp) openSettings(page string) {
	if m.applied.dock != nil && *m.applied.dock {
		m.mac.SetShowInDock(true)
	}
	if m.inMenuBarOnly {
		m.inMenuBarOnly = false
		m.status.refresh(m.app.Settings())
	}
	m.windows.showSettings(page)
}

// toMenuBar closes Filee's windows and its Dock icon; it keeps running in
// the menu bar. Quit in the Dock or the app menu (⌘Q) does this, and so
// does closing the last window.
func (m *macApp) toMenuBar() {
	for _, w := range ns.Array(ns.Send(m.nsapp, "windows")) {
		if titledWindow(w) && ns.Bool(w, "isVisible") {
			ns.Send(w, "orderOut:", objc.ID(0))
		}
	}
	m.inMenuBarOnly = true
	// The menu bar icon is the way back in, even when the settings hide it.
	m.status.refresh(m.app.Settings())
	m.mac.SetShowInDock(false)
}

// titledWindow tells Filee's own windows (settings, tools) from the donut
// panel and the menu bar's windows.
func titledWindow(w ns.ID) bool { return ns.Uint(w, "styleMask")&styleTitled != 0 }

// windowClosed drops the Dock icon when the last of Filee's windows closed.
func (m *macApp) windowClosed(closing ns.ID) {
	for _, w := range ns.Array(ns.Send(m.nsapp, "windows")) {
		if w != closing && titledWindow(w) && ns.Bool(w, "isVisible") {
			return
		}
	}
	// After the close finished, so the window doesn't flash back with the policy change.
	m.after(0, m.toMenuBar)
}

// systemQuit reports whether the quit request comes from logging out,
// restarting or shutting down (its Apple event names a reason), which Filee
// must not refuse.
func systemQuit() bool {
	event := ns.Send(ns.Send(ns.Class("NSAppleEventManager"), "sharedAppleEventManager"), "currentAppleEvent")
	if event == 0 {
		return false
	}
	const keyQuitReason = 0x7768793F // 'why?'
	return ns.Send(event, "attributeDescriptorForKeyword:", uint32(keyQuitReason)) != 0
}

func (m *macApp) quit() {
	m.log.Info("quitting")
	m.quitting = true
	m.triggers.stop()
	m.app.Close()
	ns.Send(m.nsapp, "terminate:", objc.ID(0))
}

// after runs f on the main thread after d; the returned func cancels it.
func (m *macApp) after(d time.Duration, f func()) func() {
	var mu sync.Mutex
	cancelled := false
	t := time.AfterFunc(d, func() {
		ns.OnMain(func() {
			mu.Lock()
			c := cancelled
			mu.Unlock()
			if !c {
				f()
			}
		})
	})
	return func() {
		mu.Lock()
		cancelled = true
		mu.Unlock()
		t.Stop()
	}
}

// cursor is the mouse position in top-left global points (Quartz).
func cursor() (float64, float64) {
	ev := ns.CGEventCreate(0)
	if ev == 0 {
		return 200, 200
	}
	defer ns.CFRelease(ev)
	p := ns.CGEventGetLocation(ev)
	return p.X, p.Y
}

// buildMainMenu: an agent shows no menu bar, but key equivalents (⌘C, ⌘V,
// ⌘W, ⌘Q in the settings window's web view) go through the main menu.
func (m *macApp) buildMainMenu() {
	menu := ns.Send(ns.New("NSMenu"), "autorelease")
	item := func(parent ns.ID, title, action, key string) {
		var sel objc.SEL
		if action != "" {
			sel = ns.Sel(action)
		}
		it := ns.Send(ns.Alloc("NSMenuItem"), "initWithTitle:action:keyEquivalent:", ns.String(title), sel, ns.String(key))
		ns.Send(parent, "addItem:", it)
		ns.Release(it)
	}
	sub := func(title string) ns.ID {
		top := ns.Send(ns.Alloc("NSMenuItem"), "initWithTitle:action:keyEquivalent:", ns.String(title), objc.SEL(0), ns.String(""))
		submenu := ns.Send(ns.Alloc("NSMenu"), "initWithTitle:", ns.String(title))
		ns.Send(top, "setSubmenu:", submenu)
		ns.Send(menu, "addItem:", top)
		ns.Release(top)
		ns.Release(submenu)
		return submenu
	}
	appMenu := sub("Filee")
	// terminate: is answered by applicationShouldTerminate:, which closes to the menu bar.
	item(appMenu, m.app.Text("app.close_to_menu_bar"), "terminate:", "q")
	edit := sub("Edit")
	item(edit, "Undo", "undo:", "z")
	item(edit, "Redo", "redo:", "Z")
	item(edit, "Cut", "cut:", "x")
	item(edit, "Copy", "copy:", "c")
	item(edit, "Paste", "paste:", "v")
	item(edit, "Select All", "selectAll:", "a")
	win := sub("Window")
	item(win, "Close", "performClose:", "w")
	item(win, "Minimize", "performMiniaturize:", "m")
	ns.Send(m.nsapp, "setMainMenu:", menu)
}

// ---- the app delegate ----

var appDelegateClass objc.Class

func newAppDelegate() ns.ID {
	if appDelegateClass == 0 {
		var err error
		appDelegateClass, err = objc.RegisterClass("FileeAppDelegate", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
			{Cmd: ns.Sel("applicationShouldHandleReopen:hasVisibleWindows:"), Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID, _ bool) bool {
				// Clicking the Dock icon or opening Filee again in Finder brings the settings back.
				if current != nil {
					current.openSettings("")
				}
				return true
			}},
			{Cmd: ns.Sel("applicationShouldTerminateAfterLastWindowClosed:"), Fn: func(objc.ID, objc.SEL, objc.ID) bool { return false }},
			{Cmd: ns.Sel("applicationShouldTerminate:"), Fn: func(objc.ID, objc.SEL, objc.ID) uint {
				const terminateCancel, terminateNow = 0, 1
				if current == nil || current.quitting || systemQuit() {
					return terminateNow
				}
				// Quit in the Dock or ⌘Q: out of the Dock, still in the menu bar.
				current.log.Info("closed to the menu bar")
				current.toMenuBar()
				return terminateCancel
			}},
			{Cmd: ns.Sel("application:openURLs:"), Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID, urls objc.ID) {
				var files []string
				for _, u := range ns.Array(urls) {
					if ns.Bool(u, "isFileURL") {
						files = append(files, ns.GoString(ns.Send(u, "path")))
					}
				}
				if current != nil && len(files) > 0 {
					current.openFiles(files)
				}
			}},
			{Cmd: ns.Sel("applicationWillTerminate:"), Fn: func(objc.ID, objc.SEL, objc.ID) {
				if current != nil {
					current.triggers.stop()
					current.app.Close()
				}
			}},
		})
		if err != nil {
			panic(err)
		}
	}
	return ns.Send(ns.ID(appDelegateClass), "new")
}

// ---- radial.Host ----

func (m *macApp) ShowOverlay(x, y, side float64, activate bool) { m.overlay.show(x, y, side, activate) }
func (m *macApp) HideOverlay()                                  { m.overlay.hide() }
func (m *macApp) Redraw()                                       { m.overlay.redraw() }
func (m *macApp) SetHitBackdrop(on bool)                        { m.overlay.setHitBackdrop(on) }
func (m *macApp) After(d time.Duration, f func()) func()        { return m.after(d, f) }
func (m *macApp) WatchFlipKey(on bool)                          { m.triggers.watchFlip.Store(on) }
func (m *macApp) Log() *slog.Logger                             { return m.log }
func (m *macApp) Reveal(path string)                            { m.mac.RevealInFileManager(path) }
func (m *macApp) OpenSettings(page string)                      { m.openSettings(page) }
