//go:build darwin

package gui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/dseif0x/filee-go/internal/app"
	"github.com/dseif0x/filee-go/internal/donut"
	"github.com/dseif0x/filee-go/internal/macos/macplatform"
	"github.com/dseif0x/filee-go/internal/macos/ns"
)

// selfTest exercises the native parts without user interaction (filee app
// --selftest) and prints one line per check, to paste into a bug report.
// It shows the donut for a moment and leaves no state behind.
func selfTest(a *app.App, w io.Writer) error {
	failed := 0
	check := func(name string, f func() (string, error)) {
		var detail string
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("panic: %v", r)
				}
			}()
			ns.WithPool(func() { detail, err = f() })
		}()
		if err != nil {
			failed++
			fmt.Fprintf(w, "FAIL %-22s %v\n", name, err)
			return
		}
		fmt.Fprintf(w, "ok   %-22s %s\n", name, detail)
	}
	fmt.Fprintf(w, "Filee %s self-test on %s/%s, %s\n", app.Version, runtime.GOOS, runtime.GOARCH, osVersion())

	check("main thread", func() (string, error) {
		if !ns.IsMainThread() {
			return "", fmt.Errorf("not on the main thread (runtime.LockOSThread in init)")
		}
		return "yes", nil
	})
	check("classes", func() (string, error) {
		var missing, optional []string
		for _, c := range []string{"NSApplication", "NSPanel", "NSView", "NSStatusBar", "NSPasteboard", "NSWorkspace", "NSFileManager", "CALayer", "NSOpenPanel", "NSAlert"} {
			if ns.Class(c) == 0 {
				missing = append(missing, c)
			}
		}
		for _, c := range []string{"WKWebView", "UNUserNotificationCenter", "SMAppService"} {
			if ns.Class(c) == 0 {
				optional = append(optional, c)
			}
		}
		if len(missing) > 0 {
			return "", fmt.Errorf("missing %v", missing)
		}
		if len(optional) > 0 {
			return "missing optional " + strings.Join(optional, ", "), nil
		}
		return "all present", nil
	})
	var nsapp ns.ID
	check("NSApplication", func() (string, error) {
		nsapp = ns.Send(ns.Class("NSApplication"), "sharedApplication")
		if nsapp == 0 {
			return "", fmt.Errorf("no shared application")
		}
		ok := ns.Bool(nsapp, "setActivationPolicy:", 1)
		return fmt.Sprintf("accessory policy accepted: %v, policy now %d", ok, ns.Int(nsapp, "activationPolicy")), nil
	})
	check("struct returns", func() (string, error) {
		scr := ns.Send(ns.Class("NSScreen"), "mainScreen")
		f := ns.GetRect(scr, "frame")
		v := ns.GetRect(scr, "visibleFrame")
		mouse := ns.GetPoint(ns.Class("NSEvent"), "mouseLocation")
		if f.Size.Width <= 0 || f.Size.Height <= 0 {
			return "", fmt.Errorf("main screen frame %+v", f)
		}
		return fmt.Sprintf("screen %.0fx%.0f @%.0fx, visible %.0fx%.0f at %.0f,%.0f, mouse %.0f,%.0f", f.Size.Width, f.Size.Height,
			ns.Float(scr, "backingScaleFactor"), v.Size.Width, v.Size.Height, v.Origin.X, v.Origin.Y, mouse.X, mouse.Y), nil
	})
	check("strings and arrays", func() (string, error) {
		s := "Filee 변환 ✓"
		if got := ns.GoString(ns.String(s)); got != s {
			return "", fmt.Errorf("round trip gave %q", got)
		}
		arr := ns.StringArray([]string{"a", "b"})
		if got := ns.Strings(arr); len(got) != 2 || got[1] != "b" {
			return "", fmt.Errorf("array %v", got)
		}
		return "UTF-8 round trip", nil
	})
	check("main-thread dispatch", func() (string, error) {
		done := false
		go ns.OnMain(func() { done = true })
		deadline := time.Now().Add(2 * time.Second)
		for !done && time.Now().Before(deadline) {
			ns.CFRunLoopRunInMode(uintptr(ns.Constant("kCFRunLoopDefaultMode")), 0.05, true)
		}
		if !done {
			return "", fmt.Errorf("a function posted to the main queue never ran")
		}
		return "dispatch_async_f round trip", nil
	})
	m := &macApp{app: a, log: logger(), nsapp: nsapp}
	if s, err := macplatform.New(); err == nil {
		m.mac = s
	}
	check("platform", func() (string, error) {
		x, y := cursor()
		fg := m.mac.Foreground()
		fgName := "?"
		if fg != nil {
			fgName = fg.ProcessName
		}
		apps := m.mac.RunningAppNames()
		return fmt.Sprintf("under the mouse (%.0f,%.0f): %q, front: %q, %d apps with windows, language %q, reduce motion %v",
			x, y, m.mac.ProcessNameAt(int(x), int(y)), fgName, len(apps), macplatform.PreferredLanguage(), m.mac.PrefersReducedMotion()), nil
	})
	check("permissions", func() (string, error) {
		return fmt.Sprintf("accessibility %v, input monitoring %v, in a bundle %v, login item status %d",
			ns.AXIsProcessTrusted(), ns.CGPreflightListenEventAccess(), macplatform.InBundle(), macplatform.LoginItemStatus()), nil
	})
	check("trash", func() (string, error) {
		dir, err := os.MkdirTemp("", "filee-selftest")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(dir)
		p := filepath.Join(dir, "selftest.txt")
		if err := os.WriteFile(p, []byte("Filee self-test, safe to delete"), 0o644); err != nil {
			return "", err
		}
		if r := m.mac.MoveToTrash(p); r != nil {
			return "", fmt.Errorf("%s: %s", r.ReasonKey, r.Detail)
		}
		if _, err := os.Stat(p); err == nil {
			return "", fmt.Errorf("the file is still there")
		}
		if t := m.mac.ShortcutTarget("/etc/hosts"); t != "" {
			return "", fmt.Errorf("/etc/hosts read as an alias of %q", t)
		}
		return "a temporary file went to the Trash (selftest.txt, delete it there)", nil
	})
	check("pasteboard", func() (string, error) {
		pb := ns.Send(ns.Class("NSPasteboard"), "pasteboardWithUniqueName")
		defer ns.Send(pb, "releaseGlobally")
		ns.Send(pb, "clearContents")
		exe, _ := os.Executable()
		ns.Send(pb, "writeObjects:", ns.NSArray(ns.FileURL(exe)))
		files, content := readDrag(pb)
		if len(files) != 1 || files[0] != exe || content != nil {
			return "", fmt.Errorf("read back %v", files)
		}
		return "file URL written and read back", nil
	})
	check("donut render", func() (string, error) {
		menu := donut.NewMenu()
		menu.SetItems([]donut.Item{{ID: "a", Label: "PNG", Caption: "PNG", Enabled: true}, {ID: "b", Label: "변환", Enabled: true}})
		r := donut.NewRenderer(nil, 2)
		start := time.Now()
		img := r.Render(menu, donut.NewPalette(systemDark(), donut.DefaultAccent, nil))
		cg := ns.CGImage(img)
		if cg == 0 {
			return "", fmt.Errorf("no CGImage")
		}
		ns.CGImageRelease(cg)
		return fmt.Sprintf("%dx%d in %v, dark mode %v, %d fallback fonts", img.Rect.Dx(), img.Rect.Dy(), time.Since(start).Round(time.Millisecond), systemDark(), loadFallbackFonts()), nil
	})
	check("overlay panel", func() (string, error) {
		m.radial = nil
		o := newOverlayForTest(m)
		menu := donut.NewMenu()
		menu.SetItems([]donut.Item{{ID: "a", Label: "Self-test", Enabled: true}, {ID: "b", Label: "Filee", Enabled: true}})
		menu.CenterTitle = "Filee"
		menu.CenterSubtitle = "self-test"
		o.testMenu = menu
		x, y := cursor()
		o.show(x, y, menu.Side(), false)
		deadline := time.Now().Add(700 * time.Millisecond)
		for time.Now().Before(deadline) {
			ns.CFRunLoopRunInMode(uintptr(ns.Constant("kCFRunLoopDefaultMode")), 0.02, false)
		}
		visible := ns.Bool(o.panel, "isVisible")
		contents := ns.Send(o.layer, "contents")
		key := ns.Bool(o.panel, "canBecomeKeyWindow")
		types := len(ns.Array(ns.Send(o.view, "registeredDraggedTypes")))
		frame := ns.GetRect(o.panel, "frame")
		o.hide()
		if !visible || contents == 0 {
			return "", fmt.Errorf("visible %v, layer contents %v", visible, contents != 0)
		}
		return fmt.Sprintf("shown at %.0f,%.0f %.0fx%.0f, can become key %v, %d drag types", frame.Origin.X, frame.Origin.Y, frame.Size.Width, frame.Size.Height, key, types), nil
	})
	check("status item", func() (string, error) {
		bar := ns.Send(ns.Class("NSStatusBar"), "systemStatusBar")
		item := ns.Send(bar, "statusItemWithLength:", float64(-1))
		if item == 0 || ns.Send(item, "button") == 0 {
			return "", fmt.Errorf("no status item")
		}
		ns.Send(bar, "removeStatusItem:", item)
		return "created and removed", nil
	})
	check("event tap", func() (string, error) {
		if !ns.AXIsProcessTrusted() && !ns.CGPreflightListenEventAccess() {
			return "skipped: no Accessibility / Input Monitoring permission yet", nil
		}
		cb := purego.NewCallback(func(proxy uintptr, typ uint32, event uintptr, refcon uintptr) uintptr { return event })
		tap := ns.CGEventTapCreate(1, 0, 1, 1<<evMouseMoved, cb, 0)
		if tap == 0 {
			return "", fmt.Errorf("CGEventTapCreate failed although the permission is there")
		}
		ns.CGEventTapEnable(tap, true)
		enabled := ns.CGEventTapIsEnabled(tap)
		ns.CGEventTapEnable(tap, false)
		ns.CFMachPortInvalidate(tap)
		ns.CFRelease(tap)
		return fmt.Sprintf("listen-only tap created, enabled %v", enabled), nil
	})
	check("web view", func() (string, error) {
		if ns.Class("WKWebView") == 0 {
			return "", fmt.Errorf("WebKit didn't load")
		}
		win, wv := newWebWindow("Filee self-test", 640, 480)
		load(wv, "about:blank")
		ns.Send(win, "close")
		ns.Release(win)
		return "window with a WKWebView made and closed", nil
	})
	check("notifications", func() (string, error) {
		if !macplatform.InBundle() {
			return "skipped: not running as Filee.app (osascript is used instead)", nil
		}
		c := ns.Send(ns.Class("UNUserNotificationCenter"), "currentNotificationCenter")
		if c == 0 {
			return "", fmt.Errorf("no notification center")
		}
		return "notification center available", nil
	})
	check("services provider", func() (string, error) {
		registerServices(func([]string) {}, nil)
		if servicesProvider == 0 {
			return "", fmt.Errorf("not registered")
		}
		if !ns.Bool(servicesProvider, "respondsToSelector:", ns.Sel("convertFiles:userData:error:")) {
			return "", fmt.Errorf("provider doesn't answer convertFiles:userData:error:")
		}
		return "FileeServicesProvider answers convertFiles:userData:error:", nil
	})
	check("menu bar only", func() (string, error) {
		delegate := newAppDelegate()
		for _, sel := range []string{"applicationShouldTerminate:", "applicationShouldHandleReopen:hasVisibleWindows:"} {
			if !ns.Bool(delegate, "respondsToSelector:", ns.Sel(sel)) {
				return "", fmt.Errorf("app delegate doesn't answer %s", sel)
			}
		}
		if !ns.Bool(windowDelegate(), "respondsToSelector:", ns.Sel("windowWillClose:")) {
			return "", fmt.Errorf("window delegate doesn't answer windowWillClose:")
		}
		return fmt.Sprintf("quit and window-close handlers present, system quit now %v", systemQuit()), nil
	})
	_ = objc.ID(0)
	if failed > 0 {
		fmt.Fprintf(w, "%d check(s) failed\n", failed)
		return fmt.Errorf("%d self-test check(s) failed", failed)
	}
	fmt.Fprintln(w, "all checks passed")
	return nil
}

func osVersion() string {
	info := ns.Send(ns.Class("NSProcessInfo"), "processInfo")
	return ns.GoString(ns.Send(info, "operatingSystemVersionString"))
}
