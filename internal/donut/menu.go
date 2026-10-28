package donut

import (
	"math"
	"strings"
)

// Item is one slice of the donut.
type Item struct {
	ID    string
	Label string
	// Caption is the small second line (the target format); hidden when equal to Label.
	Caption        string
	Enabled        bool
	DisabledReason string
	// Group: a slice that converts nothing itself but fans out Children.
	Group    bool
	Children []Item
}

// CanExpand: a group with something to fan out.
func (i *Item) CanExpand() bool { return i.Group && len(i.Children) > 0 }

const (
	// GapPixels is the gap between slices (keeps the donut "full" while separating them).
	GapPixels   = 3
	PopOut      = 7
	openSeconds = 0.42
	openStagger = 0.028
	// CriticalDamping settles a spring of stiffness 320 without overshooting.
	criticalDamping = 36
)

// spring is an underdamped spring: the soft "boing" when a slice pops out.
type spring struct{ v, vel float64 }

// maxStep is the longest time one integration step covers (late frames take several).
const maxStep = 1.0 / 120

func (s *spring) step(target, dt, stiffness, damping float64) bool {
	steps := max(1, int(math.Ceil(dt/maxStep)))
	h := dt / float64(steps)
	for range steps {
		force := stiffness*(target-s.v) - damping*s.vel
		s.vel += force * h
		s.v += s.vel * h
	}
	if math.Abs(target-s.v) < 0.001 && math.Abs(s.vel) < 0.01 {
		s.v, s.vel = target, 0
		return false
	}
	return true
}

func (s *spring) stepDefault(target, dt float64) bool { return s.step(target, dt, 320, 20) }

// Menu is the live donut toolbar's state: its slices and what is lit up,
// open or animating. The host feeds it pointer, drag and key events in
// logical coordinates of its square (Side × Side), calls Advance on every
// frame while NeedsFrames, and draws it with Render. Not safe for
// concurrent use: the host calls it from its UI thread.
type Menu struct {
	items []Item

	OuterRadius  float64
	HoleRatio    float64
	SliceOpacity float64
	// CornerRadius is the theme's softness (0–28); the slices round by half of it.
	CornerRadius   float64
	CenterTitle    string
	CenterSubtitle string
	// CenterHint is the small line at the bottom of the hub ("⟳ Tab back"); CenterHintWheel draws a mouse before it.
	CenterHint      string
	CenterHintWheel bool
	CanFlip         bool
	// FlipKey flips the donut in click mode ("Tab"), "" for none.
	FlipKey       string
	FlipWithWheel bool
	// FlipKeyFromHook: drag mode, the global hook flips; the window's own key press doesn't.
	FlipKeyFromHook bool
	// ReserveSubRing keeps room for a group's ring before the slices are known.
	ReserveSubRing bool
	// Opacity is the whole donut's (0 while a drag waits for files).
	Opacity float64
	// AllowContextEdit: click mode, a right-click on a slice asks to edit its preset.
	AllowContextEdit bool
	// Motion is false when the user (or the system) wants reduced motion: every value jumps to its end.
	Motion bool

	// Events (all called on the host's thread, from the event or Advance that caused them).
	OnActiveIndexChanged func(index int)
	OnFaceSwapping       func()
	OnSegmentInvoked     func(index int)
	OnSubSegmentInvoked  func(group, sub int)
	OnCenterInvoked      func()
	OnContextRequested   func(index, sub int)
	OnProgressCancel     func()
	OnProgressClicked    func()
	// OnAnimate asks the host for frames (call Advance until it returns false).
	OnAnimate func()

	hover        []spring
	open         []float64
	centerPulse  spring
	openClock    float64
	closing      bool
	closeDone    func()
	hoverIndex   int
	extHighlight int
	extCenterHot bool
	press        *pressState
	clock        float64
	needsFrames  bool

	groupState
	progressState
	flipState
}

type pressState struct {
	index int
	at    Point
}

// NewMenu returns an empty donut with the default appearance.
func NewMenu() *Menu {
	m := &Menu{OuterRadius: 150, HoleRatio: 0.42, SliceOpacity: 0.96, CornerRadius: 16, Opacity: 1, Motion: true,
		openClock: math.MaxFloat64, hoverIndex: -1, extHighlight: -1}
	m.groupState.reset()
	m.flipState.lastWheel = -1 << 62
	m.resetProgress()
	return m
}

// Items are the slices in order.
func (m *Menu) Items() []Item { return m.items }

// Count is the number of slices.
func (m *Menu) Count() int { return len(m.items) }

// SetItems replaces the slices (new files, or the other face of a flip).
func (m *Menu) SetItems(items []Item) {
	m.items = items
	n := len(items)
	m.hover = make([]spring, n)
	m.open = make([]float64, n)
	for i := range m.open {
		m.open[i] = 1
	}
	m.hoverIndex, m.extHighlight = -1, -1
	m.expanded, m.shownExpanded, m.subHover = -1, -1, -1
	m.clearPending()
	m.expand = spring{}
	// New files (or none): a flip of the old ones is over. A flip's own swap keeps going.
	if !m.swapping {
		m.cancelFlip()
	}
	m.lastExternalAt = nil
	m.keyboardHold, m.ringFromKeys = false, false
	m.keyAnchor, m.lastPointer = nil, nil
	m.animate()
}

// Side is the width and height of the donut's square in points.
func (m *Menu) Side() float64 {
	groups := false
	for i := range m.items {
		groups = groups || m.items[i].Group
	}
	return SideFor(m.OuterRadius, m.ReserveSubRing || groups)
}

// CenterPoint is the middle of the square.
func (m *Menu) CenterPoint() Point { s := m.Side(); return Point{s / 2, s / 2} }

func (m *Menu) innerRadius() float64 { return m.OuterRadius * clamp(m.HoleRatio, 0.2, 0.75) }

// IsCenterHot: a drag from outside hovers the hole (a drop cancels).
func (m *Menu) IsCenterHot() bool { return m.extCenterHot }

// ActiveIndex is the emphasised slice (pointer or drag), or -1.
func (m *Menu) ActiveIndex() int {
	if m.extHighlight >= 0 {
		return m.extHighlight
	}
	return m.hoverIndex
}

// ActiveSubIndex is the hovered slice of a group's ring, or -1.
func (m *Menu) ActiveSubIndex() int { return m.subHover }

func (m *Menu) activeChanged() {
	if m.OnActiveIndexChanged != nil {
		m.OnActiveIndexChanged(m.ActiveIndex())
	}
}

// PlayOpenAnimation pops the slices out of the centre.
func (m *Menu) PlayOpenAnimation() {
	m.resetProgress()
	m.closing, m.closeDone = false, nil
	if !m.Motion {
		for i := range m.open {
			m.open[i] = 1
		}
		m.animate()
		return
	}
	for i := range m.open {
		m.open[i] = 0
	}
	m.openClock = 0
	m.animate()
}

// PlayCloseAnimation shrinks the donut into the centre, then calls done.
func (m *Menu) PlayCloseAnimation(done func()) {
	if m.isFlipping() {
		m.finishFlip()
	}
	if !m.Motion || m.Count() == 0 {
		done()
		return
	}
	m.closing, m.closeDone = true, done
	m.openClock = 0
	m.animate()
}

// PulseCenter gives the hub a short "boing".
func (m *Menu) PulseCenter() {
	if m.Motion {
		m.centerPulse.vel = 6
		m.animate()
	}
}

// SetExternalHighlight lights up a slice from outside (drag over). -1 clears.
func (m *Menu) SetExternalHighlight(index int, centerHot bool) {
	if m.extHighlight == index && m.extCenterHot == centerHot {
		return
	}
	m.extHighlight, m.extCenterHot = index, centerHot
	m.activeChanged()
	m.animate()
}

// LitAmount is how far slice index is drawn lit up (0–1), for the drag log.
func (m *Menu) LitAmount(index int) float64 {
	if index >= 0 && index < len(m.hover) {
		return m.hover[index].v
	}
	return 0
}

// HitTest says what is under p.
func (m *Menu) HitTest(p Point) Hit {
	if m.progressMode {
		return m.hitTestProgress(p)
	}
	c := m.CenterPoint()
	h := HitTest(c, p, m.innerRadius(), m.OuterRadius, m.Count(), PopOut)
	if children := m.expandedChildren(); children != nil {
		inner, outer := m.subRadii()
		sub := SubIndexAt(c, p, inner, outer, MidAngle(m.expanded, m.Count()), len(children), m.subSpanOf(len(children)))
		if sub >= 0 {
			return Hit{Kind: SubSegment, Index: m.expanded, Sub: sub}
		}
		// On the way out to the ring the pointer crosses the gap before it: there it still belongs to the group.
		if h.Kind == Outside && distance(p, c) < inner && m.inFan(p, m.expanded) {
			return Hit{Kind: Segment, Index: m.expanded, Sub: -1}
		}
	}
	return h
}

// PointerMoved: the mouse moved over the donut (click mode).
func (m *Menu) PointerMoved(p Point) {
	if m.progressMode {
		m.progressPointerMoved(p)
		return
	}
	if m.keyboardHold {
		if m.keyAnchor == nil {
			a := p
			m.keyAnchor = &a
		}
		if distance(p, *m.keyAnchor) < keyboardJitter {
			return
		}
		m.keyboardHold = false
	}
	lp := p
	m.lastPointer = &lp
	h := m.HitTest(p)
	if m.isFlipping() {
		m.pointAtAngle(AngleOf(m.CenterPoint(), p), h.Kind == Segment || h.Kind == SubSegment)
	}
	m.trackGroups(h, &lp)
	switch h.Kind {
	case Segment, SubSegment:
		m.setHover(h.Index)
	default:
		m.setHover(-1)
	}
}

// Tooltip is the reason the slice under p is disabled, or "".
func (m *Menu) Tooltip(p Point) string {
	h := m.HitTest(p)
	switch h.Kind {
	case SubSegment:
		if c := m.items[h.Index].Children[h.Sub]; !c.Enabled {
			return c.DisabledReason
		}
	case Segment:
		if it := m.items[h.Index]; !it.Enabled {
			return it.DisabledReason
		}
	}
	return ""
}

// PointerExited: the mouse left the window at p.
func (m *Menu) PointerExited(p Point) {
	if m.progressMode {
		m.progressPointerMoved(Point{math.MaxFloat64 / 4, 0})
		return
	}
	if m.press == nil && !m.LeftThroughSeam(p) {
		m.CollapseGroup()
		m.setHover(-1)
	}
}

// PointerPressed: a mouse button went down at p.
func (m *Menu) PointerPressed(p Point, right bool) {
	h := m.HitTest(p)
	if m.progressMode {
		if !right {
			m.press = &pressState{index: -1, at: p}
		}
		return
	}
	if right {
		if h.Kind == Segment && m.AllowContextEdit && m.OnContextRequested != nil {
			m.OnContextRequested(h.Index, -1)
		} else if h.Kind == SubSegment && m.AllowContextEdit && m.OnContextRequested != nil {
			m.OnContextRequested(h.Index, h.Sub)
		}
		return
	}
	idx := -1
	if h.Kind == Segment {
		idx = h.Index
	}
	m.press = &pressState{index: idx, at: p}
}

// PointerReleased: the left button went up at p.
func (m *Menu) PointerReleased(p Point) {
	if m.press == nil {
		return
	}
	m.press = nil
	if m.progressMode {
		m.progressPointerReleased(p)
		return
	}
	h := m.HitTest(p)
	switch h.Kind {
	case Segment:
		it := &m.items[h.Index]
		if it.Enabled && !it.Group {
			if m.OnSegmentInvoked != nil {
				m.OnSegmentInvoked(h.Index)
			}
		} else if it.CanExpand() {
			m.expandGroup(h.Index) // no waiting when clicked
		}
	case Outside:
		if m.expanded >= 0 {
			m.CollapseGroup()
		}
	case SubSegment:
		if m.items[h.Index].Children[h.Sub].Enabled && m.OnSubSegmentInvoked != nil {
			m.OnSubSegmentInvoked(h.Index, h.Sub)
		}
	case Center:
		if m.OnCenterInvoked != nil {
			m.OnCenterInvoked()
		}
	}
}

func (m *Menu) setHover(index int) {
	if m.hoverIndex == index {
		return
	}
	m.hoverIndex = index
	m.activeChanged()
	m.animate()
}

func (m *Menu) animate() {
	m.needsFrames = true
	if m.OnAnimate != nil {
		m.OnAnimate()
	}
}

// FinishAnimations sets everything to where it ends (the frame loop stalled).
func (m *Menu) FinishAnimations() {
	for i := range m.hover {
		v := 0.0
		if i == m.ActiveIndex() {
			v = 1
		}
		m.hover[i] = spring{v: v}
	}
	m.centerPulse = spring{}
	if m.expanded >= 0 {
		m.expand = spring{v: 1}
	} else {
		m.expand = spring{}
		m.shownExpanded = -1
	}
	m.finishFlip()
	if m.openClock < math.MaxFloat64 {
		m.openClock = math.MaxFloat64
		v := 1.0
		if m.closing {
			v = 0
		}
		for i := range m.open {
			m.open[i] = v
		}
		if m.closing {
			m.finishClose()
		}
	}
}

// NeedsFrames: an animation, a timer or a progress ring is running.
func (m *Menu) NeedsFrames() bool {
	return m.needsFrames
}

// Advance moves every animation dt seconds on and reports whether more frames are needed.
func (m *Menu) Advance(dt float64) bool {
	dt = clamp(dt, 0, 0.05)
	m.clock += dt
	active := false
	if m.pendingGroup >= 0 {
		if m.clock >= m.pendingAt {
			m.ExpandPendingGroup()
		} else {
			active = true
		}
	}
	if !m.Motion {
		for i := range m.hover {
			v := 0.0
			if i == m.ActiveIndex() {
				v = 1
			}
			m.hover[i] = spring{v: v}
		}
		v := 1.0
		if m.closing {
			v = 0
		}
		for i := range m.open {
			m.open[i] = v
		}
		m.openClock = math.MaxFloat64
		m.centerPulse = spring{}
		if m.expanded >= 0 {
			m.expand = spring{v: 1}
		} else {
			m.expand = spring{}
			m.shownExpanded = -1
		}
		m.finishFlip()
		if m.closing {
			m.finishClose()
		}
		active = m.stepProgress(dt) || active
	} else {
		active2 := false
		for i := range m.hover {
			if i == m.ActiveIndex() {
				active2 = m.hover[i].stepDefault(1, dt) || active2
			} else {
				active2 = m.hover[i].step(0, dt, 320, criticalDamping) || active2
			}
		}
		active2 = m.centerPulse.step(0, dt, 260, 12) || active2
		if m.expanded >= 0 {
			active2 = m.expand.step(1, dt, 320, 24) || active2
		} else {
			active2 = m.expand.step(0, dt, 320, criticalDamping) || active2
		}
		if m.expanded < 0 && m.expand.v <= 0.001 {
			m.shownExpanded = -1
		}
		active2 = m.stepProgress(dt) || active2
		active2 = m.stepFlip(dt) || active2
		if m.openClock < math.MaxFloat64 {
			m.openClock += dt
			done := true
			for i := range m.open {
				stagger, dur := openStagger, openSeconds
				if m.closing {
					stagger, dur = 0.01, 0.16
				}
				t := clamp((m.openClock-float64(i)*stagger)/dur, 0, 1)
				if m.closing {
					m.open[i] = 1 - easeInCubic(t)
				} else {
					m.open[i] = easeOutBack(t)
				}
				done = done && t >= 1
			}
			if done {
				m.openClock = math.MaxFloat64
				if m.closing {
					m.finishClose()
				}
			} else {
				active2 = true
			}
		}
		active = active || active2
	}
	m.needsFrames = active
	return active
}

func (m *Menu) finishClose() {
	m.closing = false
	done := m.closeDone
	m.closeDone = nil
	if done != nil {
		done()
	}
}

func easeOutBack(t float64) float64 {
	const c1 = 1.55
	const c3 = c1 + 1
	return 1 + c3*math.Pow(t-1, 3) + c1*math.Pow(t-1, 2)
}

func easeInCubic(t float64) float64 { return t * t * t }

func (m *Menu) averageOpen() float64 {
	if len(m.open) == 0 {
		return 1
	}
	s := 0.0
	for _, v := range m.open {
		s += v
	}
	return clamp(s/float64(len(m.open)), 0, 1)
}

// sliceCornerRadius is half the theme's softness (0–28).
func (m *Menu) sliceCornerRadius() float64 { return clamp(m.CornerRadius, 0, 28) * 0.5 }

// Converts: dropping or clicking at h converts (an enabled slice that isn't a
// group, or an enabled slice of a group's ring).
func (m *Menu) Converts(h Hit) bool {
	switch h.Kind {
	case Segment:
		return h.Index >= 0 && h.Index < m.Count() && !m.items[h.Index].Group && m.items[h.Index].Enabled
	case SubSegment:
		if h.Index < 0 || h.Index >= m.Count() {
			return false
		}
		ch := m.items[h.Index].Children
		return h.Sub >= 0 && h.Sub < len(ch) && ch[h.Sub].Enabled
	}
	return false
}

// HandleKey handles a key (click mode) by its name as the settings write it
// ("Tab", "Escape", "Enter", "Space", "Left", …). It returns false for keys
// left to the window (Esc on the donut closes it).
func (m *Menu) HandleKey(key string) bool {
	if !m.progressMode && m.FlipKey != "" && strings.EqualFold(key, m.FlipKey) {
		if !m.FlipKeyFromHook {
			m.Flip()
		}
		return true
	}
	if !m.handleKeyCore(key) {
		return false
	}
	m.keyboardHold = true
	m.keyAnchor = m.lastPointer
	return true
}

func (m *Menu) handleKeyCore(key string) bool {
	if m.progressMode || m.Count() == 0 {
		return false
	}
	step := 0
	switch key {
	case "Right", "Down":
		step = 1
	case "Left", "Up":
		step = -1
	}
	if m.expandedChildren() != nil && (key == "Escape" || key == "Backspace") {
		group := m.expanded
		m.CollapseGroup()
		m.setHover(group)
		return true
	}
	if children := m.expandedChildren(); children != nil && m.subHover >= 0 {
		if key == "Enter" || key == "Space" {
			if children[m.subHover].Enabled && m.OnSubSegmentInvoked != nil {
				m.OnSubSegmentInvoked(m.expanded, m.subHover)
			}
			return true
		}
		if step == 0 {
			return false
		}
		m.setSub((m.subHover + step + len(children)) % len(children))
		return true
	}
	current := m.ActiveIndex()
	if (key == "Enter" || key == "Space") && current >= 0 {
		if m.items[current].CanExpand() {
			m.expandGroup(current)
			m.setSub(0)
			m.ringFromKeys = true
		} else if m.items[current].Enabled && m.OnSegmentInvoked != nil {
			m.OnSegmentInvoked(current)
		}
		return true
	}
	if step == 0 {
		return false
	}
	m.CollapseGroup()
	if current < 0 {
		if step > 0 {
			m.setHover(0)
		} else {
			m.setHover(m.Count() - 1)
		}
	} else {
		m.setHover((current + step + m.Count()) % m.Count())
	}
	m.pointAtSelection()
	return true
}
