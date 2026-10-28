package donut

import "math"

// Group slices fan out a small ring of presets outside the donut, where they
// are clicked or dropped on like ordinary slices. Resting on a group's slice
// (ExpandDelay) or moving outward on it opens its ring; the ring stays open
// on the way out to it and folds on another slice, the hole, or going round
// the outside away from it.

const (
	subGap = 6
	// subBand is the thickness of the ring a group fans out.
	subBand = 46
	// SubRingSpace is the room around the donut for a group's ring and its labels.
	SubRingSpace = subGap + subBand + 10
	// ExpandDelay is how long the pointer rests on a group slice before it opens (seconds).
	ExpandDelay    = 0.16
	outwardPixels  = 10
	keyboardJitter = 6
	backdropMargin = 8
	leaveEdge      = 12
)

// SideFor is the width and height of a donut of outerRadius, with room for group rings when asked.
func SideFor(outerRadius float64, subRing bool) float64 {
	extra := 0.0
	if subRing {
		extra = SubRingSpace
	}
	return 2 * (outerRadius + PopOut + 14 + extra)
}

type groupState struct {
	expanded, shownExpanded, subHover int
	expand                            spring
	pendingGroup                      int
	pendingAt                         float64
	pendingDistance                   float64
	keyboardHold, ringFromKeys        bool
	keyAnchor, lastPointer            *Point
}

func (g *groupState) reset() {
	g.expanded, g.shownExpanded, g.subHover, g.pendingGroup = -1, -1, -1, -1
}

func (m *Menu) expandedChildren() []Item {
	if m.expanded >= 0 && m.expanded < m.Count() && m.items[m.expanded].Group {
		return m.items[m.expanded].Children
	}
	return nil
}

func (m *Menu) subRadii() (float64, float64) {
	in := m.OuterRadius + PopOut + subGap
	return in, in + subBand
}

func (m *Menu) subSpanOf(count int) float64 {
	in, out := m.subRadii()
	return SubSpan(Span(m.Count()), count, (in+out)/2)
}

// inFan: p is in the direction of group's ring, at any distance.
func (m *Menu) inFan(p Point, group int) bool {
	if group < 0 || group >= m.Count() {
		return false
	}
	count := len(m.items[group].Children)
	return count > 0 && SubIndexAt(m.CenterPoint(), p, 0, math.MaxFloat64, MidAngle(group, m.Count()), count, m.subSpanOf(count)) >= 0
}

func (m *Menu) onWayToRing(p Point) bool {
	return m.expanded >= 0 && distance(p, m.CenterPoint()) > (m.innerRadius()+m.OuterRadius)/2 && m.inFan(p, m.expanded)
}

// SubSliceCenter is the middle of slice sub of group's ring.
func (m *Menu) SubSliceCenter(group, sub int) Point {
	count := len(m.items[group].Children)
	mid := SubMidAngle(MidAngle(group, m.Count()), sub, count, m.subSpanOf(count))
	in, out := m.subRadii()
	return PointAt(m.CenterPoint(), (in+out)/2, mid)
}

// SliceCenter is the middle of slice i.
func (m *Menu) SliceCenter(i int) Point {
	return PointAt(m.CenterPoint(), (m.innerRadius()+m.OuterRadius)/2, MidAngle(i, m.Count()))
}

func (m *Menu) trackGroups(h Hit, at *Point) {
	before := [2]int{m.expanded, m.pendingGroup}
	sub := -1
	c := m.CenterPoint()
	switch {
	case h.Kind == Segment && h.Index == m.expanded:
		m.ringFromKeys = false
	case h.Kind == Segment && at != nil && m.onWayToRing(*at):
		m.clearPending()
	case h.Kind == Segment && h.Index >= 0 && h.Index < m.Count() && m.items[h.Index].CanExpand():
		m.expanded = -1
		d := 0.0
		if at != nil {
			d = distance(*at, c)
		}
		switch {
		case m.pendingGroup != h.Index:
			m.clearPending()
			m.pendingGroup = h.Index
			m.pendingDistance = d
			m.pendingAt = m.clock + ExpandDelay
		case at != nil && d-m.pendingDistance >= outwardPixels:
			m.ExpandPendingGroup()
		case at != nil:
			m.pendingDistance = math.Min(m.pendingDistance, d)
		}
	case h.Kind == SubSegment:
		m.ringFromKeys = false
		sub = h.Sub
	case h.Kind == Outside && m.pendingGroup >= 0 && at != nil && m.inFan(*at, m.pendingGroup) && distance(*at, c) <= m.subOuter()+backdropMargin:
		m.ExpandPendingGroup()
	case h.Kind == Outside:
		m.clearPending()
		if m.expanded >= 0 && at != nil && !m.inFan(*at, m.expanded) {
			m.expanded = -1
		}
	case h.Kind == Center && m.ringFromKeys:
		sub = m.subHover
	default:
		m.expanded = -1
		m.ringFromKeys = false
		m.clearPending()
	}
	m.setSub(sub)
	if before != [2]int{m.expanded, m.pendingGroup} {
		m.animate()
	}
}

func (m *Menu) subOuter() float64 { _, o := m.subRadii(); return o }

func (m *Menu) clearPending() { m.pendingGroup = -1 }

// ExpandPendingGroup opens the group the pointer rests on.
func (m *Menu) ExpandPendingGroup() {
	if m.pendingGroup < 0 || m.pendingGroup >= m.Count() {
		return
	}
	m.expandGroup(m.pendingGroup)
}

func (m *Menu) expandGroup(group int) {
	m.ringFromKeys = false
	if m.shownExpanded != group {
		m.expand = spring{}
	}
	m.expanded, m.shownExpanded = group, group
	m.clearPending()
	m.activeChanged()
	m.animate()
}

// ExpandedGroup is the group fanned out now, or -1.
func (m *Menu) ExpandedGroup() int { return m.expanded }

func (m *Menu) setSub(sub int) {
	if m.subHover == sub {
		return
	}
	m.subHover = sub
	m.activeChanged()
	m.animate()
}

// CollapseGroup folds an open group away.
func (m *Menu) CollapseGroup() {
	m.expanded = -1
	m.ringFromKeys = false
	m.clearPending()
	m.setSub(-1)
	m.animate()
}

// ExternalDragLeft: a drag from outside left the window, last seen at lastAt
// (nil when unknown). Unless it was well inside the donut or an open fan
// (macOS reports a drag leaving over see-through pixels), the highlights go.
func (m *Menu) ExternalDragLeft(lastAt *Point) {
	if lastAt != nil && m.LeftThroughSeam(*lastAt) {
		return
	}
	m.lastExternalAt = nil
	m.CollapseGroup()
	m.SetExternalHighlight(-1, false)
}

// SetExternalHit lights up what a drag from outside is over.
func (m *Menu) SetExternalHit(h Hit, at *Point) {
	if at != nil {
		a := *at
		m.lastExternalAt = &a
		if m.isFlipping() {
			m.pointAtAngle(AngleOf(m.CenterPoint(), a), h.Kind == Segment || h.Kind == SubSegment)
		}
	}
	m.trackGroups(h, at)
	idx := -1
	if h.Kind == Segment || h.Kind == SubSegment {
		idx = h.Index
	}
	m.SetExternalHighlight(idx, h.Kind == Center)
}

// LeftThroughSeam: the pointer "left" at p well inside the donut or the
// backdrop of an open (or opening) group: a see-through pixel let it through.
func (m *Menu) LeftThroughSeam(p Point) bool {
	group := m.expanded
	if group < 0 {
		group = m.pendingGroup
	}
	if group < 0 || group >= m.Count() {
		return false
	}
	c := m.CenterPoint()
	d := distance(p, c)
	if d <= m.OuterRadius+PopOut-leaveEdge {
		return true
	}
	count := len(m.items[group].Children)
	reach := m.subOuter() + backdropMargin
	if count == 0 || d > reach-leaveEdge {
		return false
	}
	halfSpan := math.Min(2*math.Pi, m.subSpanOf(count)*float64(count)+2*backdropMargin/reach) / 2
	inside := halfSpan - math.Abs(remainder(AngleOf(c, p)-MidAngle(group, m.Count()), 2*math.Pi))
	return inside > 0 && (inside >= math.Pi/2 || d*math.Sin(inside) >= leaveEdge)
}
