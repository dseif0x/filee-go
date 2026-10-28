//go:build darwin

package gui

import (
	"math"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ebitengine/purego"

	"github.com/dseif0x/filee-go/internal/gui/gesture"
	"github.com/dseif0x/filee-go/internal/macos/ns"
	"github.com/dseif0x/filee-go/internal/settings"
	"github.com/dseif0x/filee-go/internal/web"
)

// Global gestures: a listen-only Quartz event tap (it sees every mouse and
// key event of the session, holds none back) on a thread of its own feeds
// the gesture detector. It needs Accessibility (or Input Monitoring): until
// Filee has it, the tap waits and starts by itself once it is given.

// CGEventType values.
const (
	evLeftMouseDown          = 1
	evLeftMouseUp            = 2
	evRightMouseDown         = 3
	evRightMouseUp           = 4
	evMouseMoved             = 5
	evLeftMouseDragged       = 6
	evRightMouseDragged      = 7
	evKeyDown                = 10
	evKeyUp                  = 11
	evScrollWheel            = 22
	evOtherMouseDown         = 25
	evOtherMouseUp           = 26
	evOtherMouseDragged      = 27
	evTapDisabledByTimeout   = 0xFFFFFFFE
	evTapDisabledByUserInput = 0xFFFFFFFF

	fieldMouseButtonNumber  = 3
	fieldKeyboardAutorepeat = 8
	fieldKeyboardKeycode    = 9
	fieldScrollDeltaAxis1   = 11
	fieldScrollPointDelta1  = 96

	flagShift   = 0x00020000
	flagControl = 0x00040000
	flagOption  = 0x00080000
	flagCommand = 0x00100000
)

type triggers struct {
	m   *macApp
	det *gesture.Detector

	watchFlip atomic.Bool
	flipKey   atomic.Value // string
	flipWheel atomic.Bool
	flipDown  bool // tap thread only

	mu       sync.Mutex
	excluded map[string]bool
	ownName  string

	tap       uintptr
	loop      uintptr
	running   atomic.Bool
	waiting   bool // for the permission
	waitTimer *time.Ticker
}

var (
	theTriggers *triggers
	tapCallback uintptr
	tapOnce     sync.Once
)

func newTriggers(m *macApp) *triggers {
	t := &triggers{m: m, det: gesture.New(), excluded: map[string]bool{}, ownName: ns.ProcessName(int32(os.Getpid()))}
	t.flipKey.Store("")
	theTriggers = t
	t.det.SystemDragThreshold = m.mac.SystemDragThreshold()
	t.det.ScopeFilter = t.inScope
	// Detector callbacks come on the tap thread (holds on a timer goroutine): the UI runs on the main thread.
	t.det.DragStarted = func(_ *settings.TriggerGesture, x, y int) {
		ns.OnMain(func() { m.radial.ShowForDrag(float64(x), float64(y)) })
	}
	t.det.DragEnded = func(int, int) { ns.OnMain(m.radial.OnDragGestureEnded) }
	selection := func(_ *settings.TriggerGesture, x, y int) { go t.selectionGesture(x, y) }
	t.det.HoldTriggered = selection
	t.det.KeyChordTriggered = selection
	return t
}

var selectionPending atomic.Bool

// selectionGesture opens the donut for Finder's selection (a hold or a key chord).
func (t *triggers) selectionGesture(x, y int) {
	if !selectionPending.CompareAndSwap(false, true) {
		return
	}
	defer selectionPending.Store(false)
	files, err := t.m.mac.Selection()
	if err != nil {
		t.m.log.Warn("could not read Finder's selection", "error", err)
		ns.OnMain(func() { t.m.alert(err.Error()) })
		return
	}
	if len(files) == 0 {
		t.m.log.Info("the selection shortcut found no selected files", "front", t.m.mac.DescribeForegroundWindow())
		return
	}
	ns.OnMain(func() {
		if !t.m.app.Settings().Paused {
			t.m.radial.ShowForFiles(float64(x), float64(y), files, false)
		}
	})
}

// apply takes the gestures, the pause and the flip key from the settings.
func (t *triggers) apply(s *settings.AppSettings) {
	t.det.Configure(s.Triggers)
	t.det.SetPaused(s.Paused)
	t.flipKey.Store(s.Donut.EffectiveFlipKey(s.Triggers))
	t.flipWheel.Store(s.Donut.FlipWithWheel)
	ex := map[string]bool{}
	for _, p := range s.ExcludedProcesses {
		ex[strings.ToLower(strings.TrimSuffix(strings.TrimSpace(p), ".exe"))] = true
	}
	t.mu.Lock()
	t.excluded = ex
	t.mu.Unlock()
}

// inScope decides whether a gesture may start at the point (tap thread).
func (t *triggers) inScope(g *settings.TriggerGesture, x, y int) bool {
	var process string
	fm := false
	known := false
	if g.Kind == settings.KeyChord {
		// A keyboard shortcut goes to the app in front, wherever the pointer is.
		if f := t.m.mac.Foreground(); f != nil {
			process, fm, known = f.ProcessName, f.IsFileManager, true
		}
	}
	if !known {
		process = t.m.mac.ProcessNameAt(x, y)
		fm = strings.EqualFold(process, "Finder")
	}
	t.mu.Lock()
	excluded := t.excluded[strings.ToLower(process)]
	t.mu.Unlock()
	if process != "" && (strings.EqualFold(process, t.ownName) || excluded) {
		return false
	}
	// A picture or link dragged out of a web page opens the donut like a file.
	return g.Scope == settings.ScopeAnywhere || fm || g.Kind == settings.Drag && web.IsBrowser(process)
}

// start installs the event tap, or waits for the permission.
func (t *triggers) start() {
	if t.running.Load() {
		return
	}
	if !t.m.mac.HasAccessibility() {
		t.waitForPermission()
		return
	}
	tapOnce.Do(func() { tapCallback = purego.NewCallback(tapEvent) })
	started := make(chan bool, 1)
	go func() {
		// The tap's run loop: a thread of its own, so a busy main thread never delays input.
		runtime.LockOSThread()
		const mask = 1<<evLeftMouseDown | 1<<evLeftMouseUp | 1<<evRightMouseDown | 1<<evRightMouseUp | 1<<evMouseMoved |
			1<<evLeftMouseDragged | 1<<evRightMouseDragged | 1<<evKeyDown | 1<<evKeyUp | 1<<evScrollWheel |
			1<<evOtherMouseDown | 1<<evOtherMouseUp | 1<<evOtherMouseDragged
		// kCGSessionEventTap, kCGHeadInsertEventTap, kCGEventTapOptionListenOnly.
		tap := ns.CGEventTapCreate(1, 0, 1, mask, tapCallback, 0)
		if tap == 0 {
			started <- false
			return
		}
		src := ns.CFMachPortCreateRunLoopSrc(0, tap, 0)
		loop := ns.CFRunLoopGetCurrent()
		ns.CFRunLoopAddSource(loop, src, uintptr(ns.Constant("kCFRunLoopCommonModes")))
		ns.CFRelease(src)
		ns.CGEventTapEnable(tap, true)
		t.mu.Lock()
		t.tap, t.loop = tap, loop
		t.mu.Unlock()
		t.running.Store(true)
		started <- true
		ns.CFRunLoopRun()
		t.running.Store(false)
	}()
	if <-started {
		t.m.log.Info("global gestures started")
	} else {
		t.m.log.Warn("could not install the event tap; gestures wait for the permission")
		t.waitForPermission()
	}
}

// waitForPermission checks every two seconds whether Filee was allowed, then starts.
func (t *triggers) waitForPermission() {
	if t.waiting {
		return
	}
	t.waiting = true
	t.m.log.Info("gestures wait for the Accessibility permission")
	tick := time.NewTicker(2 * time.Second)
	go func() {
		for range tick.C {
			if t.m.mac.HasAccessibility() {
				tick.Stop()
				ns.OnMain(func() {
					t.waiting = false
					t.m.log.Info("Accessibility permission granted")
					t.start()
				})
				return
			}
		}
	}()
}

// stop removes the tap.
func (t *triggers) stop() {
	t.mu.Lock()
	tap, loop := t.tap, t.loop
	t.tap, t.loop = 0, 0
	t.mu.Unlock()
	if tap != 0 {
		ns.CGEventTapEnable(tap, false)
		ns.CFMachPortInvalidate(tap)
		ns.CFRunLoopStop(loop)
		ns.CFRelease(tap)
	}
}

func modifiers(flags uint64) settings.ModifierKeys {
	var m settings.ModifierKeys
	if flags&flagControl != 0 {
		m |= settings.ModCtrl
	}
	if flags&flagOption != 0 {
		m |= settings.ModAlt
	}
	if flags&flagShift != 0 {
		m |= settings.ModShift
	}
	if flags&flagCommand != 0 {
		m |= settings.ModMeta
	}
	return m
}

func buttonNumber(n int64) settings.MouseButton {
	switch n {
	case 0:
		return settings.ButtonLeft
	case 1:
		return settings.ButtonRight
	case 2:
		return settings.ButtonMiddle
	case 3:
		return settings.ButtonX1
	case 4:
		return settings.ButtonX2
	}
	return settings.ButtonNone
}

// tapEvent is the event tap's callback (tap thread). It must return quickly
// and never panic into Core Graphics.
func tapEvent(proxy uintptr, typ uint32, event uintptr, refcon uintptr) uintptr {
	t := theTriggers
	if t == nil {
		return event
	}
	defer func() {
		if r := recover(); r != nil {
			t.m.log.Error("panic in the event tap", "panic", r)
		}
	}()
	switch typ {
	case evTapDisabledByTimeout, evTapDisabledByUserInput:
		t.mu.Lock()
		tap := t.tap
		t.mu.Unlock()
		if tap != 0 {
			ns.CGEventTapEnable(tap, true)
		}
		return event
	}
	loc := ns.CGEventGetLocation(event)
	x, y := int(math.Round(loc.X)), int(math.Round(loc.Y))
	mods := modifiers(ns.CGEventGetFlags(event))
	d := t.det
	switch typ {
	case evLeftMouseDown:
		d.MouseDown(settings.ButtonLeft, x, y, mods)
	case evRightMouseDown:
		d.MouseDown(settings.ButtonRight, x, y, mods)
	case evOtherMouseDown:
		d.MouseDown(buttonNumber(ns.CGEventGetIntegerValueField(event, fieldMouseButtonNumber)), x, y, mods)
	case evLeftMouseUp:
		d.MouseUp(settings.ButtonLeft, x, y)
	case evRightMouseUp:
		d.MouseUp(settings.ButtonRight, x, y)
	case evOtherMouseUp:
		d.MouseUp(buttonNumber(ns.CGEventGetIntegerValueField(event, fieldMouseButtonNumber)), x, y)
	case evMouseMoved:
		d.MouseMove(x, y, mods, settings.ButtonNone)
	case evLeftMouseDragged:
		d.MouseMove(x, y, mods, settings.ButtonLeft)
	case evRightMouseDragged:
		d.MouseMove(x, y, mods, settings.ButtonRight)
	case evOtherMouseDragged:
		d.MouseMove(x, y, mods, buttonNumber(ns.CGEventGetIntegerValueField(event, fieldMouseButtonNumber)))
	case evKeyDown, evKeyUp:
		name := gesture.MacKeyName(int(ns.CGEventGetIntegerValueField(event, fieldKeyboardKeycode)))
		down := typ == evKeyDown
		if flip, _ := t.flipKey.Load().(string); flip != "" && strings.EqualFold(name, flip) {
			repeat := down && t.flipDown
			t.flipDown = down
			// Once per press, not for the repeats of a held key; only while a drag-mode donut shows files.
			if down && !repeat && t.watchFlip.Load() {
				ns.OnMain(t.m.radial.FlipKeyPressed)
			}
		}
		if down && name != "" && ns.CGEventGetIntegerValueField(event, fieldKeyboardAutorepeat) == 0 {
			d.KeyDown(name, mods)
		}
	case evScrollWheel:
		if t.watchFlip.Load() && t.flipWheel.Load() && (ns.CGEventGetIntegerValueField(event, fieldScrollDeltaAxis1) != 0 || ns.CGEventGetIntegerValueField(event, fieldScrollPointDelta1) != 0) {
			ns.OnMain(t.m.radial.WheelTurned)
		}
	}
	return event
}
