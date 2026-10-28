package donut

import "math"

// After a drop the slices are sucked into the centre, the hole becomes a
// small progress ring with the percentage, and on completion a check mark
// (or "!") pops up, holds, and fades:
//
//	BeginProgress → slices collapse (≈0.3 s) → ring springs in
//	SetProgress   → arc follows smoothly
//	CompleteProgress → arc fills → badge → hold → fade → done
//	HandOffProgress  → ring shrinks away quickly → done

// Outcome is how a conversion shown by the ring ended.
type Outcome int

const (
	NoOutcome Outcome = iota
	Succeeded
	Failed
	Cancelled
)

const (
	// RingRadius is the progress ring's radius (centre of the stroke).
	RingRadius        = 36
	ringStroke        = 6
	collapseSeconds   = 0.28
	collapseStagger   = 0.02
	minRingSeconds    = 0.6
	holdSuccess       = 0.9
	holdFailure       = 1.4
	fadeSeconds       = 0.25
	handOffSeconds    = 0.22
	plateRadius       = RingRadius + ringStroke + 2
	cancelButtonSize  = 10
	progressHoverEdge = 14
)

type progressState struct {
	progressMode   bool
	collapseClock  float64
	ringClock      float64
	ring           spring
	shownProgress  float64
	targetProgress float64
	pendingOutcome Outcome
	outcome        Outcome
	badge          spring
	holdClock      float64
	fadeClock      float64
	handOffClock   float64
	progressDone   func()
	ringHover      bool
}

// IsInProgressMode: the donut shows a conversion's progress instead of presets.
func (m *Menu) IsInProgressMode() bool { return m.progressMode }

// IsFinishingProgress: the finished ring still holds its badge or fades out.
func (m *Menu) IsFinishingProgress() bool { return m.progressMode && m.progressDone != nil }

// ShownOutcome is the badge on show (NoOutcome while running).
func (m *Menu) ShownOutcome() Outcome { return m.outcome }

// LeaveProgressMode drops the ring (and its pending callback) for a new gesture.
func (m *Menu) LeaveProgressMode() { m.resetProgress() }

func (m *Menu) resetProgress() {
	m.progressState = progressState{ringClock: -1, holdClock: -1, fadeClock: -1, handOffClock: -1}
}

func (m *Menu) collapseEnd() float64 {
	return collapseSeconds + collapseStagger*float64(max(0, m.Count()-1))
}

// BeginProgress collapses the slices into the centre and shows the ring.
func (m *Menu) BeginProgress() {
	m.resetProgress()
	m.progressMode = true
	m.closing, m.closeDone = false, nil
	m.openClock = math.MaxFloat64
	if !m.Motion {
		m.collapseClock = m.collapseEnd()
		m.ring.v = 1
		m.ringClock = 0
	}
	m.animate()
}

// SetProgress sets the progress (0–1); the arc moves towards it.
func (m *Menu) SetProgress(v float64) {
	m.targetProgress = clamp(v, 0, 1)
	m.animate()
}

// CompleteProgress shows the outcome, holds, fades and then calls done.
func (m *Menu) CompleteProgress(o Outcome, done func()) {
	if !m.progressMode {
		done()
		return
	}
	if o == Succeeded {
		m.targetProgress = 1
	}
	m.pendingOutcome = o
	m.progressDone = done
	m.animate()
}

// HandOffProgress shrinks the ring away quickly, then calls done.
func (m *Menu) HandOffProgress(done func()) {
	if !m.progressMode {
		done()
		return
	}
	m.progressDone = done
	m.pendingOutcome = NoOutcome
	if !m.Motion {
		m.finishProgress()
		return
	}
	m.handOffClock = 0
	m.animate()
}

func (m *Menu) finishProgress() {
	done := m.progressDone
	m.progressDone = nil
	if done != nil {
		done()
	}
}

func (m *Menu) stepProgress(dt float64) bool {
	if !m.progressMode {
		return false
	}
	animate := m.Motion
	active := false
	if m.collapseClock < m.collapseEnd() {
		if animate {
			m.collapseClock += dt
		} else {
			m.collapseClock = m.collapseEnd()
		}
		active = true
	} else if m.ringClock < 0 {
		m.ringClock = 0
		if animate {
			m.ring.vel = 2
		} else {
			m.ring.v = 1
		}
	}
	if m.ringClock >= 0 {
		m.ringClock += dt
		if animate {
			active = m.ring.step(1, dt, 300, 16) || active
		}
	}
	gap := m.targetProgress - m.shownProgress
	if animate {
		m.shownProgress += gap * math.Min(1, dt*9)
	} else {
		m.shownProgress = m.targetProgress
	}
	if math.Abs(m.targetProgress-m.shownProgress) < 0.002 {
		m.shownProgress = m.targetProgress
	} else {
		active = true
	}
	if p := m.pendingOutcome; p != NoOutcome && m.outcome == NoOutcome && m.ringClock >= minRingSeconds &&
		(p != Succeeded || m.shownProgress >= 0.999) {
		m.outcome = p
		m.holdClock = 0
		if animate {
			m.badge.vel = 9
		} else {
			m.badge.v = 1
		}
	}
	if m.pendingOutcome != NoOutcome {
		active = true
	}
	if m.outcome != NoOutcome {
		if animate {
			active = m.badge.step(1, dt, 420, 14) || active
		}
		if m.fadeClock < 0 {
			m.holdClock += dt
			hold := holdSuccess
			switch m.outcome {
			case Failed:
				hold = holdFailure
			case Cancelled:
				hold = 0.2
			}
			if m.holdClock >= hold {
				m.fadeClock = 0
			}
			active = true
		} else {
			if animate {
				m.fadeClock += dt
			} else {
				m.fadeClock = fadeSeconds
			}
			if m.fadeClock >= fadeSeconds {
				m.finishProgress()
				return false
			}
			active = true
		}
	}
	if m.handOffClock >= 0 {
		m.handOffClock += dt
		if m.handOffClock >= handOffSeconds {
			m.finishProgress()
			return false
		}
		active = true
	}
	return active
}

func (m *Menu) cancelButtonCenter() Point {
	return PointAt(m.CenterPoint(), RingRadius+ringStroke, math.Pi/4)
}

func (m *Menu) hitTestProgress(p Point) Hit {
	if m.ringHover && m.outcome == NoOutcome && distance(p, m.cancelButtonCenter()) <= cancelButtonSize+1 {
		return hit(ProgressCancel)
	}
	if distance(p, m.CenterPoint()) <= plateRadius {
		return hit(ProgressRing)
	}
	return hit(Outside)
}

func (m *Menu) progressPointerMoved(p Point) {
	over := distance(p, m.CenterPoint()) <= RingRadius+ringStroke+progressHoverEdge
	if over != m.ringHover {
		m.ringHover = over
		m.animate()
	}
}

func (m *Menu) progressPointerReleased(p Point) {
	switch m.hitTestProgress(p).Kind {
	case ProgressCancel:
		if m.OnProgressCancel != nil {
			m.OnProgressCancel()
		}
	case ProgressRing:
		if m.OnProgressClicked != nil {
			m.OnProgressClicked()
		}
	}
}
