package donut

import (
	"math"
	"testing"
)

func TestGeometry(t *testing.T) {
	c := Point{100, 100}
	if i := IndexAt(0, 4); i != 0 {
		t.Errorf("top is slice %d", i)
	}
	if i := IndexAt(2*math.Pi-0.1, 4); i != 0 {
		t.Errorf("just left of the top is slice %d", i)
	}
	if i := IndexAt(math.Pi/2, 4); i != 1 {
		t.Errorf("3 o'clock is slice %d", i)
	}
	if a := AngleOf(c, Point{200, 100}); math.Abs(a-math.Pi/2) > 1e-9 {
		t.Errorf("angle %v", a)
	}
	if h := HitTest(c, Point{100, 95}, 20, 80, 4, 0); h.Kind != Center {
		t.Errorf("hole: %v", h)
	}
	if h := HitTest(c, Point{100, 40}, 20, 80, 4, 0); h.Kind != Segment || h.Index != 0 {
		t.Errorf("top slice: %+v", h)
	}
	if h := HitTest(c, Point{100, 15}, 20, 80, 4, 0); h.Kind != Outside {
		t.Errorf("outside: %+v", h)
	}
	if h := HitTest(c, Point{100, 15}, 20, 80, 4, 7); h.Kind != Segment {
		t.Errorf("popped-out slack: %+v", h)
	}
	// A fan across 12 o'clock.
	span := SubSpan(Span(4), 3, 0)
	if s := SubIndexAt(c, PointAt(c, 90, SubMidAngle(0, 0, 3, span)), 85, 100, 0, 3, span); s != 0 {
		t.Errorf("first of the fan: %d", s)
	}
	if s := SubIndexAt(c, PointAt(c, 90, SubMidAngle(0, 2, 3, span)), 85, 100, 0, 3, span); s != 2 {
		t.Errorf("last of the fan: %d", s)
	}
	// A rounded slice stays inside its sharp sector.
	o := Wedge(c, 30, 80, 0, Span(5), 3, 8)
	if o.CornerRadius <= 0 {
		t.Fatal("no rounding")
	}
	var p Path
	p.AddOutline(o)
	for _, sub := range p.subpaths() {
		for _, q := range sub {
			d := distance(c, q)
			if d < 30-1e-6 || d > 80+1e-6 || math.Abs(remainder(AngleOf(c, q), 2*math.Pi)) > Span(5)/2 {
				t.Fatalf("point %v outside the slice (r %v)", q, d)
			}
		}
	}
}

func TestGroupOpensAfterRestingOnIt(t *testing.T) {
	m := sampleMenu()
	settle(m)
	at := m.SliceCenter(2)
	m.SetExternalHit(m.HitTest(at), &at)
	if m.ExpandedGroup() != -1 {
		t.Fatal("opened at once")
	}
	for range 12 {
		m.Advance(1.0 / 60)
	}
	if m.ExpandedGroup() != 2 {
		t.Fatalf("group not open after the delay: %d", m.ExpandedGroup())
	}
	sub := m.SubSliceCenter(2, 1)
	h := m.HitTest(sub)
	if h.Kind != SubSegment || h.Sub != 1 || !m.Converts(h) {
		t.Fatalf("ring slice hit %+v", h)
	}
	if h := m.HitTest(m.SubSliceCenter(2, 2)); m.Converts(h) {
		t.Error("a disabled ring slice converts")
	}
	if m.Converts(m.HitTest(m.SliceCenter(2))) {
		t.Error("the group slice converts")
	}
	m.SetExternalHit(m.HitTest(m.SliceCenter(0)), ptr(m.SliceCenter(0)))
	if m.ExpandedGroup() != -1 {
		t.Error("another slice didn't fold the group")
	}
}

func TestClicksAndKeys(t *testing.T) {
	m := sampleMenu()
	settle(m)
	got := -1
	m.OnSegmentInvoked = func(i int) { got = i }
	center := false
	m.OnCenterInvoked = func() { center = true }
	m.PointerMoved(m.SliceCenter(1))
	m.PointerPressed(m.SliceCenter(1), false)
	m.PointerReleased(m.SliceCenter(1))
	if got != 1 {
		t.Errorf("click converted with %d", got)
	}
	m.PointerPressed(m.CenterPoint(), false)
	m.PointerReleased(m.CenterPoint())
	if !center {
		t.Error("the hole didn't close")
	}
	got = -1
	m.PointerMoved(Point{1, 1})
	if !m.HandleKey("Right") || m.ActiveIndex() != 0 {
		t.Fatalf("right arrow: %d", m.ActiveIndex())
	}
	m.HandleKey("Left")
	m.HandleKey("Enter")
	if got != 4 {
		t.Errorf("Enter converted with %d", got)
	}
	if m.HandleKey("Escape") {
		t.Error("Esc was taken by the donut without a group open")
	}
}

func TestFlipSwapsFaces(t *testing.T) {
	m := sampleMenu()
	settle(m)
	swaps := 0
	m.OnFaceSwapping = func() {
		swaps++
		m.SetItems([]Item{{ID: "x", Label: "Back", Enabled: true}})
	}
	m.FlipKey = "Tab"
	if !m.HandleKey("Tab") || swaps != 1 || m.Count() != 1 {
		t.Fatalf("Tab: %d swaps, %d items", swaps, m.Count())
	}
	if !m.IsFlipping() {
		t.Error("no wave")
	}
	r := NewRenderer(nil, 1)
	for m.Advance(1.0 / 60) {
		r.Render(m, NewPalette(false, "", nil))
	}
	if m.IsFlipping() {
		t.Error("the wave never ended")
	}
	if m.FlipFromWheel(1000) && m.FlipFromWheel(1100) && swaps != 2 {
		t.Errorf("one wheel turn flipped %d times", swaps-1)
	}
}

func TestProgressHandOff(t *testing.T) {
	m := sampleMenu()
	m.Motion = false
	m.BeginProgress()
	m.SetProgress(0.5)
	m.Advance(0.01)
	done := false
	m.HandOffProgress(func() { done = true })
	if !done {
		t.Fatal("hand-off without motion should finish at once")
	}
	m.BeginProgress()
	done = false
	m.CompleteProgress(Failed, func() { done = true })
	for i := 0; i < 200 && !done; i++ {
		m.Advance(0.05)
	}
	if !done || m.ShownOutcome() != Failed {
		t.Errorf("failed ring: done %v outcome %v", done, m.ShownOutcome())
	}
}
