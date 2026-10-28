package donut

import "math"

// Flipping the donut to its other face: each slice folds about its spoke,
// starting under the pointer and spreading both ways. The owner swaps the
// items when OnFaceSwapping is called; the old slices are kept for drawing only.

const (
	// WheelQuietMs: a wheel event this soon after the previous one is the same turn (one flip per turn).
	WheelQuietMs = 300
	sliceTurn    = 0.12
	flipStagger  = 0.08
	flipDuration = sliceTurn + flipStagger
)

type oldSlice struct {
	item            Item
	mid, span, open float64
	hover, emphasis float64
	hot             bool
}

type oldGroup struct {
	index         int
	children      []Item
	expansion     float64
	subHover      int
	expanded      bool
	opening, span float64
}

type flipState struct {
	lastWheel      int64
	flipClock      float64
	flipQueued     bool
	swapping       bool
	lastExternalAt *Point
	oldFace        []oldSlice
	oldGroup       *oldGroup
	oldDelay       []float64
	newDelay       []float64
	oldOriginIndex int
	flipOrigin     *float64
	pointingAngle  *float64
	pointingAtSlc  bool
	flipping       bool
}

func (m *Menu) isFlipping() bool { return m.flipping }

// IsFlipping reports whether a flip runs.
func (m *Menu) IsFlipping() bool { return m.flipping }

// FlipFromWheel flips on the first wheel event of a turn (nowMs on any clock).
func (m *Menu) FlipFromWheel(nowMs int64) bool {
	same := nowMs-m.lastWheel < WheelQuietMs
	m.lastWheel = nowMs
	return !same && m.Flip()
}

// Wheel is a wheel turn over the click-mode donut.
func (m *Menu) Wheel(nowMs int64) bool {
	if !m.FlipWithWheel || m.progressMode {
		return false
	}
	m.FlipFromWheel(nowMs)
	return true
}

func (m *Menu) progressOf(old bool, i int) float64 {
	d := m.newDelay
	if old {
		d = m.oldDelay
	}
	return clamp((m.flipClock-d[i])/sliceTurn, 0, 1)
}

func (m *Menu) sliceFlipWidth(old bool, i int) float64 {
	if old && (m.oldFace == nil || i < 0 || i >= len(m.oldFace)) {
		return 0
	}
	if !old && (i < 0 || i >= m.Count()) {
		return 0
	}
	if !m.flipping {
		if old {
			return 0
		}
		return 1
	}
	u := m.progressOf(old, i)
	if old && u >= 0.5 || !old && u < 0.5 {
		return 0
	}
	return math.Max(math.Abs(math.Cos(math.Pi*u)), 0.01)
}

func (m *Menu) sliceFlipTransform(old bool, i int) Matrix {
	if !m.flipping {
		return Identity
	}
	w := m.sliceFlipWidth(old, i)
	mid := MidAngle(i, m.Count())
	if old {
		mid = m.oldFace[i].mid
	}
	// Squeeze across the spoke: rotate the spoke onto the y axis, scale x, rotate back.
	return about(m.CenterPoint(), Rotate(-mid).Then(Scale(math.Max(w, 0.01), 1)).Then(Rotate(mid)))
}

func (m *Menu) shadowCoverage() float64 {
	if !m.flipping {
		return 1
	}
	cov := 0.0
	for i := range m.Count() {
		cov += m.sliceFlipWidth(false, i) * Span(m.Count()) / (2 * math.Pi)
	}
	for i, o := range m.oldFace {
		cov += m.sliceFlipWidth(true, i) * o.span / (2 * math.Pi)
	}
	return math.Min(1, cov)
}

func (m *Menu) delay(i, count int) float64 {
	if count == 1 || i == IndexAt(*m.flipOrigin, count) {
		return 0
	}
	return flipStagger * math.Abs(remainder(MidAngle(i, count)-*m.flipOrigin, 2*math.Pi)) / math.Pi
}

func (m *Menu) beginWave(incomingOnly bool) {
	c := m.CenterPoint()
	origin := 0.0
	switch {
	case incomingOnly:
	case m.lastExternalAt != nil:
		if *m.lastExternalAt != c {
			origin = AngleOf(c, *m.lastExternalAt)
		}
	case !m.keyboardHold && m.lastPointer != nil:
		if *m.lastPointer != c {
			origin = AngleOf(c, *m.lastPointer)
		}
	case m.ActiveIndex() >= 0:
		origin = MidAngle(m.ActiveIndex(), m.Count())
	}
	m.flipOrigin = &origin
	m.oldDelay = make([]float64, len(m.oldFace))
	for i := range m.oldFace {
		m.oldDelay[i] = m.delay(i, len(m.oldFace))
	}
	m.oldOriginIndex = -1
	if len(m.oldFace) > 0 {
		m.oldOriginIndex = IndexAt(origin, len(m.oldFace))
	}
	m.pointingAngle, m.pointingAtSlc = nil, false
}

func (m *Menu) prepareIncoming() {
	m.newDelay = make([]float64, m.Count())
	for j := range m.newDelay {
		d := m.delay(j, m.Count())
		for i, o := range m.oldFace {
			if math.Abs(remainder(o.mid-MidAngle(j, m.Count()), 2*math.Pi)) < (o.span+Span(m.Count()))/2 {
				d = math.Max(d, m.oldDelay[i])
			}
		}
		m.newDelay[j] = d
	}
}

func (m *Menu) captureOldFace() {
	n := m.Count()
	m.oldFace = make([]oldSlice, n)
	for i := range n {
		hv := m.hover[i].v
		m.oldFace[i] = oldSlice{item: m.items[i], mid: MidAngle(i, n), span: Span(n), open: m.open[i], hover: hv,
			emphasis: hv, hot: i == m.ActiveIndex()}
	}
	m.oldGroup = nil
	if m.shownExpanded >= 0 && m.shownExpanded < n && m.expand.v > 0.01 && len(m.items[m.shownExpanded].Children) > 0 {
		ch := m.items[m.shownExpanded].Children
		m.oldGroup = &oldGroup{index: m.shownExpanded, children: ch, expansion: m.expand.v, subHover: m.subHover,
			expanded: m.expanded == m.shownExpanded, opening: m.averageOpen(), span: m.subSpanOf(len(ch))}
	}
}

func (m *Menu) pointAtAngle(angle float64, pointed bool) {
	if !m.flipping || len(m.oldFace) == 0 {
		return
	}
	if m.pointingAngle != nil && m.pointingAtSlc == pointed && math.Abs(remainder(*m.pointingAngle-angle, 2*math.Pi)) < 1e-6 {
		return
	}
	a := angle
	m.pointingAngle, m.pointingAtSlc = &a, pointed
	oldIndex := IndexAt(angle, len(m.oldFace))
	if oldIndex != m.oldOriginIndex && m.oldOriginIndex >= 0 {
		o := &m.oldFace[m.oldOriginIndex]
		o.hover, o.emphasis, o.hot = 0, 0, false
		if m.oldGroup != nil && m.oldGroup.index == m.oldOriginIndex {
			m.oldGroup.subHover = -1
		}
	}
	if !pointed {
		return
	}
	if m.flipClock < m.oldDelay[oldIndex] {
		m.oldDelay[oldIndex] = m.flipClock
	}
	if ni := IndexAt(angle, m.Count()); ni >= 0 && m.progressOf(false, ni) < 0.5 {
		m.newDelay[ni] = m.flipClock - sliceTurn/2
	}
}

func (m *Menu) pointAtSelection() {
	if m.flipping && m.ActiveIndex() >= 0 {
		m.pointAtAngle(MidAngle(m.ActiveIndex(), m.Count()), true)
	}
}

// Flip switches to the other face and folds the old slices away in a wave.
// It returns false when there is nothing to flip to.
func (m *Menu) Flip() bool {
	if m.progressMode || m.closing {
		return false
	}
	if m.flipping {
		m.flipQueued = !m.flipQueued
		return true
	}
	if !m.CanFlip {
		return false
	}
	if !m.Motion {
		m.CollapseGroup()
		m.swapFaces()
		return true
	}
	m.captureOldFace()
	m.beginWave(false)
	var pointed *Point
	if m.lastExternalAt != nil && m.isSliceHit(*m.lastExternalAt) {
		pointed = m.lastExternalAt
	} else if !m.keyboardHold && m.lastPointer != nil && m.isSliceHit(*m.lastPointer) {
		pointed = m.lastPointer
	}
	var at Point
	if pointed != nil {
		at = *pointed
	}
	m.CollapseGroup()
	m.swapFaces()
	m.prepareIncoming()
	m.flipClock = 0
	m.flipping = true
	if pointed != nil && m.Count() > 0 {
		angle := AngleOf(m.CenterPoint(), at)
		m.pointingAngle, m.pointingAtSlc = &angle, true
		i := IndexAt(angle, m.Count())
		m.newDelay[i] = m.delay(i, m.Count())
	}
	m.animate()
	return true
}

func (m *Menu) isSliceHit(p Point) bool {
	k := m.HitTest(p).Kind
	return k == Segment || k == SubSegment
}

func (m *Menu) stepFlip(dt float64) bool {
	if !m.flipping {
		return false
	}
	m.flipClock += dt
	if m.flipClock < flipDuration {
		return true
	}
	m.endFlip()
	return m.flipping
}

func (m *Menu) finishFlip() {
	for m.flipping {
		m.endFlip()
	}
}

func (m *Menu) endFlip() {
	m.flipping = false
	m.flipClock = -1
	m.oldFace, m.oldGroup, m.flipOrigin = nil, nil, nil
	if !m.flipQueued {
		return
	}
	m.flipQueued = false
	m.Flip()
}

func (m *Menu) cancelFlip() {
	m.flipping = false
	m.flipClock = -1
	m.flipQueued = false
	m.oldFace, m.oldGroup, m.flipOrigin = nil, nil, nil
}

func (m *Menu) swapFaces() {
	dragAt := m.lastExternalAt
	pointer := m.lastPointer
	if m.keyboardHold {
		pointer = nil
	}
	m.swapping = true
	func() {
		defer func() { m.swapping = false }()
		if m.OnFaceSwapping != nil {
			m.OnFaceSwapping()
		}
	}()
	if dragAt != nil {
		at := *dragAt
		m.SetExternalHit(m.HitTest(at), &at)
	} else if pointer != nil {
		p := *pointer
		m.lastPointer = &p
		h := m.HitTest(p)
		m.trackGroups(h, &p)
		if h.Kind == Segment || h.Kind == SubSegment {
			m.setHover(h.Index)
		} else {
			m.setHover(-1)
		}
	}
	m.activeChanged()
	m.animate()
}
