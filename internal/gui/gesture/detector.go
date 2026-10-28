// Package gesture turns low-level mouse and keyboard events into the gestures
// that open the donut. It knows nothing of the operating system, so it is
// tested anywhere; the macOS event tap feeds it.
//
//	Drag:  button down with the exact modifiers → (armed) → moved past the
//	       threshold → DragStarted → button up → DragEnded
//	Hold:  button down with the modifiers → (armed) → HoldElapsed without
//	       moving → HoldTriggered
//	Drag and Hold on the same button and modifiers arm together: moving past
//	the threshold drags, holding still holds.
//	Key chord: key down with the exact modifiers → KeyChordTriggered at the
//	last known cursor position.
package gesture

import (
	"strings"
	"sync"
	"time"

	"github.com/dseif0x/filee-go/internal/settings"
)

// Detector detects the configured gestures. Safe for concurrent use; the
// event callbacks are called outside its lock, on the caller's goroutine
// (the hold timer's goroutine for HoldTriggered).
type Detector struct {
	mu          sync.Mutex
	gestures    []*settings.TriggerGesture
	armed       *armed
	dragActive  bool
	lateRefused bool
	lastX       int
	lastY       int
	token       int64

	// ScopeFilter decides whether a gesture may start at a point (scope,
	// excluded apps, Filee's own windows). Called with the lock held: it must be fast.
	ScopeFilter func(g *settings.TriggerGesture, x, y int) bool
	// SystemDragThreshold is used when a gesture's threshold is 0.
	SystemDragThreshold int
	// Paused turns everything off (the menu's "Pause").
	Paused bool

	DragStarted       func(g *settings.TriggerGesture, x, y int)
	DragEnded         func(x, y int)
	HoldTriggered     func(g *settings.TriggerGesture, x, y int)
	KeyChordTriggered func(g *settings.TriggerGesture, x, y int)
	// AfterFunc schedules the hold timer (time.AfterFunc when nil; tests replace it).
	AfterFunc func(d time.Duration, f func())
}

type armed struct {
	drag, hold *settings.TriggerGesture
	x, y       int
	token      int64
}

// New returns a detector with the system threshold of 4 points.
func New() *Detector { return &Detector{SystemDragThreshold: 4} }

// Configure sets the gestures (only enabled ones count).
func (d *Detector) Configure(gestures []*settings.TriggerGesture) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.gestures = nil
	for _, g := range gestures {
		if g.Enabled {
			d.gestures = append(d.gestures, g.Clone())
		}
	}
	d.armed = nil
}

// SetPaused pauses or resumes detection.
func (d *Detector) SetPaused(p bool) {
	d.mu.Lock()
	d.Paused = p
	d.mu.Unlock()
}

func (d *Detector) inScope(g *settings.TriggerGesture, x, y int) bool {
	return d.ScopeFilter == nil || d.ScopeFilter(g, x, y)
}

func (d *Detector) firstInScope(kind settings.TriggerKind, b settings.MouseButton, mods settings.ModifierKeys, x, y int) *settings.TriggerGesture {
	for _, g := range d.gestures {
		if g.Kind == kind && g.Button == b && g.Modifiers == mods && d.inScope(g, x, y) {
			return g
		}
	}
	return nil
}

// MouseDown: a button went down at (x, y) with the modifiers held.
func (d *Detector) MouseDown(b settings.MouseButton, x, y int, mods settings.ModifierKeys) {
	d.mu.Lock()
	d.lastX, d.lastY = x, y
	d.armed = nil
	d.lateRefused = false
	if d.Paused {
		d.mu.Unlock()
		return
	}
	drag := d.firstInScope(settings.Drag, b, mods, x, y)
	hold := d.firstInScope(settings.Hold, b, mods, x, y)
	if drag == nil && hold == nil {
		d.mu.Unlock()
		return
	}
	d.token++
	a := &armed{drag: drag, hold: hold, x: x, y: y, token: d.token}
	d.armed = a
	d.mu.Unlock()
	if hold != nil {
		after := d.AfterFunc
		if after == nil {
			after = func(t time.Duration, f func()) { time.AfterFunc(t, f) }
		}
		token := a.token
		after(time.Duration(hold.HoldMilliseconds)*time.Millisecond, func() { d.HoldElapsed(token) })
	}
}

// MouseMove: the pointer moved; held is the button held now (ButtonNone for none).
func (d *Detector) MouseMove(x, y int, mods settings.ModifierKeys, held settings.MouseButton) {
	d.mu.Lock()
	d.lastX, d.lastY = x, y
	var fire *settings.TriggerGesture
	if d.armed == nil {
		fire = d.lateModifier(x, y, mods, held)
	} else {
		fire = d.pastThreshold(x, y)
	}
	if fire != nil {
		d.dragActive = true
	}
	d.mu.Unlock()
	if fire != nil && d.DragStarted != nil {
		d.DragStarted(fire, x, y)
	}
}

func (d *Detector) moved(g *settings.TriggerGesture, a *armed, x, y int) bool {
	t := g.DragThreshold
	if t <= 0 {
		t = d.SystemDragThreshold
	}
	return abs(x-a.x) > t || abs(y-a.y) > t
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (d *Detector) pastThreshold(x, y int) *settings.TriggerGesture {
	a := *d.armed
	if a.hold != nil && d.moved(a.hold, &a, x, y) {
		a.hold = nil
	}
	if a.drag != nil && d.moved(a.drag, &a, x, y) {
		d.armed = nil
		return a.drag
	}
	if a.drag == nil && a.hold == nil {
		d.armed = nil
	} else {
		d.armed = &a
	}
	return nil
}

// lateModifier: the modifier was pressed after the drag had started (start
// dragging, then press the key): a drag gesture that just passed its threshold.
func (d *Detector) lateModifier(x, y int, mods settings.ModifierKeys, held settings.MouseButton) *settings.TriggerGesture {
	if d.dragActive || d.lateRefused || d.Paused || held == settings.ButtonNone || held == "" || mods == settings.ModNone {
		return nil
	}
	for _, g := range d.gestures {
		if g.Kind == settings.Drag && g.Button == held && g.Modifiers == mods {
			if d.inScope(g, x, y) {
				return g
			}
			d.lateRefused = true
			return nil
		}
	}
	return nil
}

// MouseUp: a button went up.
func (d *Detector) MouseUp(b settings.MouseButton, x, y int) {
	d.mu.Lock()
	d.lastX, d.lastY = x, y
	d.armed = nil
	ended := d.dragActive
	d.dragActive = false
	d.lateRefused = false
	d.mu.Unlock()
	if ended && d.DragEnded != nil {
		d.DragEnded(x, y)
	}
}

// HoldElapsed is the hold timer of the press token.
func (d *Detector) HoldElapsed(token int64) {
	d.mu.Lock()
	a := d.armed
	if a == nil || a.hold == nil || a.token != token {
		d.mu.Unlock()
		return
	}
	g, x, y := a.hold, a.x, a.y
	d.armed = nil
	d.lateRefused = true // moving on after the hold doesn't start the drag that shares its modifiers
	d.mu.Unlock()
	if d.HoldTriggered != nil {
		d.HoldTriggered(g, x, y)
	}
}

// KeyDown: a key (by its settings name, "Space") went down with the modifiers.
func (d *Detector) KeyDown(key string, mods settings.ModifierKeys) {
	d.mu.Lock()
	if d.Paused {
		d.mu.Unlock()
		return
	}
	x, y := d.lastX, d.lastY
	var fire *settings.TriggerGesture
	for _, g := range d.gestures {
		if g.Kind == settings.KeyChord && strings.EqualFold(g.Key, key) && g.Modifiers == mods && d.inScope(g, x, y) {
			fire = g
			break
		}
	}
	d.mu.Unlock()
	if fire != nil && d.KeyChordTriggered != nil {
		d.KeyChordTriggered(fire, x, y)
	}
}

// LastCursor is the last position seen.
func (d *Detector) LastCursor() (int, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastX, d.lastY
}
