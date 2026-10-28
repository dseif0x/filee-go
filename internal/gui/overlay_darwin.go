//go:build darwin

package gui

import (
	"math"
	"time"
	"unsafe"

	"github.com/ebitengine/purego/objc"

	"github.com/dseif0x/filee-go/internal/donut"
	"github.com/dseif0x/filee-go/internal/gui/gesture"
	"github.com/dseif0x/filee-go/internal/macos/ns"
	"github.com/dseif0x/filee-go/internal/macos/pbpaths"
	"github.com/dseif0x/filee-go/internal/settings"
	"github.com/dseif0x/filee-go/internal/web"
)

// The donut's window: a borderless, transparent, non-activating panel above
// every window, on every Space (full-screen ones too). Its view draws the
// donut as a layer whose contents are the pure-Go renderer's image, and is
// the drop target (NSDraggingDestination) for files dragged out of Finder.

const (
	nsBorderless          = 0
	nsNonactivatingPanel  = 1 << 7
	nsBackingBuffered     = 2
	nsPopUpMenuLevel      = 101
	nsCanJoinAllSpaces    = 1 << 0
	nsStationary          = 1 << 4
	nsIgnoresCycle        = 1 << 6
	nsFullScreenAuxiliary = 1 << 8
	nsDragOperationNone   = 0
	nsDragOperationCopy   = 1
)

type overlay struct {
	m        *macApp
	panel    ns.ID
	view     ns.ID
	layer    ns.ID
	renderer *donut.Renderer
	pal      donut.Palette
	shown    bool

	running   bool
	lastFrame time.Time
	dragLast  *donut.Point
	offers    bool
	tooltip   string
	// testMenu is drawn instead of the controller's (the self-test).
	testMenu *donut.Menu
}

var theOverlay *overlay

var (
	panelClass, viewClass, overlayDelegateClass objc.Class
)

func registerOverlayClasses() {
	if panelClass != 0 {
		return
	}
	var err error
	panelClass, err = objc.RegisterClass("FileeDonutPanel", objc.GetClass("NSPanel"), nil, nil, []objc.MethodDef{
		// Borderless windows don't take the keyboard by themselves: click mode needs it (arrows, Enter, Esc, Tab).
		{Cmd: ns.Sel("canBecomeKeyWindow"), Fn: func(objc.ID, objc.SEL) bool { return true }},
		{Cmd: ns.Sel("canBecomeMainWindow"), Fn: func(objc.ID, objc.SEL) bool { return false }},
	})
	if err != nil {
		panic(err)
	}
	ev := func(f func(o *overlay, event ns.ID)) func(objc.ID, objc.SEL, objc.ID) {
		return func(_ objc.ID, _ objc.SEL, event objc.ID) {
			if theOverlay != nil && theOverlay.m.radial != nil {
				f(theOverlay, event)
			}
		}
	}
	viewClass, err = objc.RegisterClass("FileeDonutView", objc.GetClass("NSView"), nil, nil, []objc.MethodDef{
		{Cmd: ns.Sel("isFlipped"), Fn: func(objc.ID, objc.SEL) bool { return true }},
		{Cmd: ns.Sel("isOpaque"), Fn: func(objc.ID, objc.SEL) bool { return false }},
		{Cmd: ns.Sel("acceptsFirstResponder"), Fn: func(objc.ID, objc.SEL) bool { return true }},
		{Cmd: ns.Sel("acceptsFirstMouse:"), Fn: func(objc.ID, objc.SEL, objc.ID) bool { return true }},
		{Cmd: ns.Sel("mouseMoved:"), Fn: ev((*overlay).mouseMoved)},
		{Cmd: ns.Sel("mouseDragged:"), Fn: ev((*overlay).mouseMoved)},
		{Cmd: ns.Sel("mouseDown:"), Fn: ev(func(o *overlay, e ns.ID) { o.menu().PointerPressed(o.at(e, "locationInWindow"), false) })},
		{Cmd: ns.Sel("rightMouseDown:"), Fn: ev(func(o *overlay, e ns.ID) { o.menu().PointerPressed(o.at(e, "locationInWindow"), true) })},
		{Cmd: ns.Sel("mouseUp:"), Fn: ev(func(o *overlay, e ns.ID) { o.menu().PointerReleased(o.at(e, "locationInWindow")) })},
		{Cmd: ns.Sel("mouseExited:"), Fn: ev(func(o *overlay, e ns.ID) { o.menu().PointerExited(o.at(e, "locationInWindow")) })},
		{Cmd: ns.Sel("scrollWheel:"), Fn: ev((*overlay).scrollWheel)},
		{Cmd: ns.Sel("keyDown:"), Fn: ev((*overlay).keyDown)},
		{Cmd: ns.Sel("draggingEntered:"), Fn: func(_ objc.ID, _ objc.SEL, sender objc.ID) uint {
			if theOverlay == nil || theOverlay.m.radial == nil {
				return nsDragOperationNone
			}
			return theOverlay.draggingEntered(sender)
		}},
		{Cmd: ns.Sel("draggingUpdated:"), Fn: func(_ objc.ID, _ objc.SEL, sender objc.ID) uint {
			if theOverlay == nil || theOverlay.m.radial == nil {
				return nsDragOperationNone
			}
			return theOverlay.draggingUpdated(sender)
		}},
		{Cmd: ns.Sel("draggingExited:"), Fn: ev(func(o *overlay, _ ns.ID) { o.m.radial.DragExited(o.dragLast) })},
		{Cmd: ns.Sel("wantsPeriodicDraggingUpdates"), Fn: func(objc.ID, objc.SEL) bool { return false }},
		{Cmd: ns.Sel("prepareForDragOperation:"), Fn: func(objc.ID, objc.SEL, objc.ID) bool { return true }},
		{Cmd: ns.Sel("performDragOperation:"), Fn: func(_ objc.ID, _ objc.SEL, sender objc.ID) bool {
			if theOverlay == nil || theOverlay.m.radial == nil {
				return false
			}
			return theOverlay.performDrop(sender)
		}},
	})
	if err != nil {
		panic(err)
	}
	overlayDelegateClass, err = objc.RegisterClass("FileeOverlayDelegate", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
		{Cmd: ns.Sel("windowDidResignKey:"), Fn: func(objc.ID, objc.SEL, objc.ID) {
			if theOverlay != nil && theOverlay.m.radial != nil {
				theOverlay.m.radial.Deactivated()
			}
		}},
	})
	if err != nil {
		panic(err)
	}
}

func newOverlay(m *macApp) *overlay {
	registerOverlayClasses()
	o := &overlay{m: m, renderer: donut.NewRenderer(nil, 2)}
	theOverlay = o
	rect := ns.Rect{Size: ns.Size{Width: 400, Height: 400}}
	o.panel = ns.Send(ns.ID(objc.ID(panelClass).Send(ns.Sel("alloc"))), "initWithContentRect:styleMask:backing:defer:",
		rect, uint(nsBorderless|nsNonactivatingPanel), uint(nsBackingBuffered), false)
	p := o.panel
	ns.Send(p, "setOpaque:", false)
	ns.Send(p, "setBackgroundColor:", ns.Send(ns.Class("NSColor"), "clearColor"))
	ns.Send(p, "setHasShadow:", false)
	ns.Send(p, "setLevel:", nsPopUpMenuLevel)
	ns.Send(p, "setCollectionBehavior:", uint(nsCanJoinAllSpaces|nsStationary|nsIgnoresCycle|nsFullScreenAuxiliary))
	ns.Send(p, "setHidesOnDeactivate:", false)
	ns.Send(p, "setReleasedWhenClosed:", false)
	ns.Send(p, "setAcceptsMouseMovedEvents:", true)
	ns.Send(p, "setFloatingPanel:", true)
	ns.Send(p, "setWorksWhenModal:", true)
	ns.Send(p, "setDelegate:", ns.Send(ns.ID(overlayDelegateClass), "new"))

	o.view = ns.Send(ns.ID(objc.ID(viewClass).Send(ns.Sel("alloc"))), "initWithFrame:", rect)
	// Layer-hosting: the view's layer is ours, its contents the rendered donut.
	o.layer = ns.Retain(ns.Send(ns.Class("CALayer"), "layer"))
	ns.Send(o.view, "setLayer:", o.layer)
	ns.Send(o.view, "setWantsLayer:", true)
	ns.Send(o.layer, "setContentsGravity:", ns.Constant("kCAGravityResize"))
	types := append([]string{"public.file-url", "NSFilenamesPboardType"}, pbpaths.DragTypes...)
	ns.Send(o.view, "registerForDraggedTypes:", ns.StringArray(types))
	const tracking = 0x01 | 0x02 | 0x80 | 0x200 // entered/exited, moved, always, in visible rect
	area := ns.Send(ns.Alloc("NSTrackingArea"), "initWithRect:options:owner:userInfo:", ns.Rect{}, uint(tracking), o.view, objc.ID(0))
	ns.Send(o.view, "addTrackingArea:", area)
	ns.Release(area)
	ns.Send(p, "setContentView:", o.view)
	o.applyTheme()
	return o
}

func (o *overlay) menu() *donut.Menu {
	if o.testMenu != nil {
		return o.testMenu
	}
	if o.m.radial == nil {
		o.testMenu = donut.NewMenu()
		return o.testMenu
	}
	return o.m.radial.Menu()
}

func newOverlayForTest(m *macApp) *overlay { return newOverlay(m) }

// at is an event's (or a drag's) location in the view: top-left points.
func (o *overlay) at(event ns.ID, sel string) donut.Point {
	p := ns.GetPoint(event, sel)
	q := ns.GetPoint(o.view, "convertPoint:fromView:", p, objc.ID(0))
	return donut.Point{X: q.X, Y: q.Y}
}

func (o *overlay) mouseMoved(event ns.ID) {
	p := o.at(event, "locationInWindow")
	m := o.menu()
	m.PointerMoved(p)
	if tip := m.Tooltip(p); tip != o.tooltip {
		o.tooltip = tip
		if tip == "" {
			ns.Send(o.view, "setToolTip:", objc.ID(0))
		} else {
			ns.Send(o.view, "setToolTip:", ns.String(tip))
		}
	}
}

func (o *overlay) scrollWheel(event ns.ID) {
	if ns.Float(event, "scrollingDeltaY") != 0 {
		o.menu().Wheel(time.Now().UnixMilli())
	}
}

func (o *overlay) keyDown(event ns.ID) {
	code := objc.Send[uint16](event, ns.Sel("keyCode"))
	name := gesture.MacKeyName(int(code))
	if name == "" {
		return
	}
	if !o.menu().HandleKey(name) && name == "Escape" {
		o.m.radial.Escape()
	}
}

func (o *overlay) draggingEntered(sender ns.ID) uint {
	files, content := readDrag(ns.Send(sender, "draggingPasteboard"))
	o.dragLast = nil
	o.offers = o.m.radial.DragEntered(files, content)
	if !o.offers {
		o.m.log.Debug("a drag without files Filee reads entered the donut", "types", ns.Strings(ns.Send(ns.Send(sender, "draggingPasteboard"), "types")))
		return nsDragOperationNone
	}
	return o.draggingUpdated(sender)
}

func (o *overlay) draggingUpdated(sender ns.ID) uint {
	p := o.at(sender, "draggingLocation")
	o.dragLast = &p
	// Always copy: a move would make Finder delete the files after the drop.
	if o.m.radial.DragOver(p) && o.offers {
		return nsDragOperationCopy
	}
	return nsDragOperationNone
}

func (o *overlay) performDrop(sender ns.ID) bool {
	p := o.at(sender, "draggingLocation")
	// The pasteboard is read now (it goes with the drag); converting waits
	// until the drag session ended, so a dialog ("ask where to save") doesn't hold Finder's drag.
	files, content := readDrag(ns.Send(sender, "draggingPasteboard"))
	converts := o.menu().Converts(o.menu().HitTest(p))
	ns.OnMain(func() { o.m.radial.Drop(p, files, content) })
	return converts
}

// readDrag reads a drag's files (Finder's file URLs, older apps' path list),
// or, when it has none, what a web browser offers for its files.
func readDrag(pb ns.ID) ([]string, *web.DragContent) {
	files := pbpaths.FirstExisting([]func() []string{
		func() []string { return fileURLs(pb) },
		func() []string {
			return ns.Strings(ns.Send(pb, "propertyListForType:", ns.String(pbpaths.FilenamesType)))
		},
		func() []string {
			var out []string
			for _, s := range itemStrings(pb, pbpaths.FileURLType) {
				if p := pbpaths.FromFileURL(s); p != "" {
					out = append(out, p)
				}
			}
			return out
		},
	}, nil)
	if len(files) > 0 {
		return files, nil
	}
	c := &web.DragContent{Texts: map[string]string{}}
	for _, t := range []string{web.MacURL, web.MacURLName, web.MacHTML} {
		if s := ns.GoString(ns.Send(pb, "stringForType:", ns.String(t))); s != "" {
			c.Texts[t] = s
		}
	}
	for _, pic := range []struct{ uti, name, mime string }{
		{"public.png", "image.png", "image/png"}, {"public.jpeg", "image.jpg", "image/jpeg"},
		{"org.webmproject.webp", "image.webp", "image/webp"}, {"com.compuserve.gif", "image.gif", "image/gif"},
		{"public.tiff", "image.tiff", "image/tiff"},
	} {
		data := nsDataBytes(ns.Send(pb, "dataForType:", ns.String(pic.uti)))
		if len(data) > 0 {
			name, mime := pic.name, pic.mime
			c.Picture = func() web.DropItem { return web.FromBytes(name, data, mime) }
			break
		}
	}
	if len(c.Texts) == 0 && c.Picture == nil {
		return nil, nil
	}
	return nil, c
}

// fileURLs are the file URLs on a pasteboard as NSURLs (AppKit resolves file reference URLs).
func fileURLs(pb ns.ID) []string {
	opts := ns.Send(ns.Class("NSDictionary"), "dictionaryWithObject:forKey:",
		ns.Send(ns.Class("NSNumber"), "numberWithBool:", true), ns.Constant("NSPasteboardURLReadingFileURLsOnlyKey"))
	urls := ns.Send(pb, "readObjectsForClasses:options:", ns.NSArray(ns.Class("NSURL")), opts)
	var out []string
	for _, u := range ns.Array(urls) {
		if ns.Bool(u, "isFileURL") {
			if p := ns.GoString(ns.Send(u, "path")); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// itemStrings is the value of a type as text on each pasteboard item that has it.
func itemStrings(pb ns.ID, t string) []string {
	var out []string
	name := ns.String(t)
	for _, it := range ns.Array(ns.Send(pb, "pasteboardItems")) {
		if s := ns.GoString(ns.Send(it, "stringForType:", name)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func nsDataBytes(data ns.ID) []byte {
	if data == 0 {
		return nil
	}
	n := ns.Uint(data, "length")
	if n == 0 {
		return nil
	}
	p := objc.Send[*byte](data, ns.Sel("bytes"))
	if p == nil {
		return nil
	}
	return append([]byte(nil), unsafe.Slice(p, n)...)
}

// screenAt is the screen holding the Quartz point: its frame and visible
// frame in Cocoa coordinates (bottom-left), its scale, and the primary
// screen's height (to convert).
func screenAt(x, yTop float64) (frame, visible ns.Rect, scale, primaryH float64) {
	screens := ns.Array(ns.Send(ns.Class("NSScreen"), "screens"))
	if len(screens) == 0 {
		return ns.Rect{Size: ns.Size{Width: 1440, Height: 900}}, ns.Rect{Size: ns.Size{Width: 1440, Height: 900}}, 2, 900
	}
	primaryH = ns.GetRect(screens[0], "frame").Size.Height
	cy := primaryH - yTop
	chosen := screens[0]
	for _, s := range screens {
		f := ns.GetRect(s, "frame")
		if x >= f.Origin.X && x < f.Origin.X+f.Size.Width && cy >= f.Origin.Y && cy < f.Origin.Y+f.Size.Height {
			chosen = s
			break
		}
	}
	return ns.GetRect(chosen, "frame"), ns.GetRect(chosen, "visibleFrame"), ns.Float(chosen, "backingScaleFactor"), primaryH
}

// show places the panel centred on the Quartz point, inside the screen's
// visible area, and shows it without activating Filee (unless asked).
func (o *overlay) show(x, y, side float64, activate bool) {
	_, vis, scale, primaryH := screenAt(x, y)
	if scale <= 0 {
		scale = 2
	}
	o.renderer.Scale = scale
	cy := primaryH - y
	ox := math.Max(vis.Origin.X, math.Min(x-side/2, math.Max(vis.Origin.X, vis.Origin.X+vis.Size.Width-side)))
	oy := math.Max(vis.Origin.Y, math.Min(cy-side/2, math.Max(vis.Origin.Y, vis.Origin.Y+vis.Size.Height-side)))
	o.applyTheme()
	ns.Send(o.panel, "setFrame:display:", ns.Rect{Origin: ns.Point{X: ox, Y: oy}, Size: ns.Size{Width: side, Height: side}}, false)
	o.draw()
	ns.Send(o.panel, "setLevel:", nsPopUpMenuLevel)
	ns.Send(o.panel, "orderFrontRegardless")
	o.shown = true
	if activate {
		ns.Send(o.m.nsapp, "activateIgnoringOtherApps:", true)
		ns.Send(o.panel, "makeKeyAndOrderFront:", objc.ID(0))
		ns.Send(o.panel, "makeFirstResponder:", o.view)
	}
	o.redraw()
}

func (o *overlay) hide() {
	o.shown = false
	o.tooltip = ""
	ns.Send(o.view, "setToolTip:", objc.ID(0))
	ns.Send(o.panel, "orderOut:", objc.ID(0))
}

func (o *overlay) setHitBackdrop(on bool) { o.renderer.HitBackdrop = on }

// applyTheme takes the colours from the theme settings and the system's light/dark mode.
func (o *overlay) applyTheme() {
	s := o.m.app.Settings()
	dark := false
	switch s.Theme.Mode {
	case settings.ThemeDark:
		dark = true
	case settings.ThemeSystem:
		dark = systemDark()
	}
	var custom map[string]string
	if t := settings.LoadCustomTheme(o.m.app.DataDir(), o.m.log); t != nil {
		custom = t.Light
		if dark {
			custom = t.Dark
		}
	}
	o.pal = donut.NewPalette(dark, s.Theme.Accent, custom)
	reduce := o.m.mac.PrefersReducedMotion()
	if s.Theme.ReduceMotion != nil {
		reduce = *s.Theme.ReduceMotion
	}
	o.menu().Motion = !reduce
}

// systemDark: the app's effective appearance is Dark Aqua.
func systemDark() bool {
	app := ns.Send(ns.Class("NSApplication"), "sharedApplication")
	appearance := ns.Send(app, "effectiveAppearance")
	names := ns.NSArray(ns.Constant("NSAppearanceNameAqua"), ns.Constant("NSAppearanceNameDarkAqua"))
	best := ns.GoString(ns.Send(appearance, "bestMatchFromAppearancesWithNames:", names))
	return best == "NSAppearanceNameDarkAqua"
}

// redraw runs frames until the donut's animations are over.
func (o *overlay) redraw() {
	if o.running {
		return
	}
	o.running = true
	o.lastFrame = time.Now()
	ns.OnMain(o.frame)
}

func (o *overlay) frame() {
	now := time.Now()
	dt := now.Sub(o.lastFrame).Seconds()
	o.lastFrame = now
	m := o.menu()
	active := m.Advance(dt)
	o.draw()
	if active || m.NeedsFrames() {
		time.AfterFunc(16*time.Millisecond, func() { ns.OnMain(o.frame) })
		return
	}
	o.running = false
}

// draw renders the donut and hands the picture to the layer.
func (o *overlay) draw() {
	if o.panel == 0 {
		return
	}
	img := o.renderer.Render(o.menu(), o.pal)
	cg := ns.CGImage(img)
	if cg == 0 {
		return
	}
	ca := ns.Class("CATransaction")
	ns.Send(ca, "begin")
	ns.Send(ca, "setDisableActions:", true)
	ns.Send(o.layer, "setContents:", objc.ID(cg))
	ns.Send(o.layer, "setContentsScale:", o.renderer.Scale)
	ns.Send(ca, "commit")
	ns.CGImageRelease(cg)
}
