package tools

import (
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// StallWatch tells a program that is still working from one that hangs: every
// line it writes is a sign of life, and one that writes nothing for longer
// than Limit counts as stuck. Engines that report progress all the time
// (FFmpeg, calibre) may then run as long as a big file needs.
type StallWatch struct {
	Limit time.Duration
	last  atomic.Int64
}

// NewStallWatch starts a watch with the given limit.
func NewStallWatch(limit time.Duration) *StallWatch {
	w := &StallWatch{Limit: limit}
	w.Touch()
	return w
}

// Touch records a sign of life.
func (w *StallWatch) Touch() { w.last.Store(time.Now().UnixNano()) }

// Stalled is true once nothing came for longer than Limit.
func (w *StallWatch) Stalled() bool {
	return time.Since(time.Unix(0, w.last.Load())) > w.Limit
}

// ProgressEstimate reports a rising estimate for engines that say nothing
// while they work (LibreOffice, Pandoc, Ghostscript): quickly at first and
// ever slower towards an upper bound, so the progress never sits at 0 %.
type ProgressEstimate struct {
	stop chan struct{}
	once sync.Once
}

// StartEstimate reports to report every 150 ms until Stop. typical is the
// usual duration (after it about 63 % of the way from..to is covered). A nil
// report does nothing.
func StartEstimate(report func(float64), typical time.Duration, from, to float64) *ProgressEstimate {
	e := &ProgressEstimate{stop: make(chan struct{})}
	if report == nil {
		return e
	}
	seconds := max(0.1, typical.Seconds())
	started := time.Now()
	go func() {
		t := time.NewTicker(150 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-e.stop:
				return
			case <-t.C:
				select {
				case <-e.stop:
					return
				default:
				}
				report(EstimateValue(time.Since(started).Seconds(), seconds, from, to))
			}
		}
	}()
	return e
}

// Stop ends the reports (safe to call more than once, and on nil).
func (e *ProgressEstimate) Stop() {
	if e != nil {
		e.once.Do(func() { close(e.stop) })
	}
}

// EstimateValue is the estimate after elapsed seconds.
func EstimateValue(elapsed, typical, from, to float64) float64 {
	return from + (to-from)*(1-math.Exp(-max(0, elapsed)/typical))
}

// TransferRate is the speed of a transfer from "bytes so far" samples over a
// sliding window, so download speeds and times left don't jump with every
// network hiccup.
type TransferRate struct {
	window, settle time.Duration
	samples        []rateSample
	bytesPerSecond float64
}

type rateSample struct {
	at    time.Duration
	bytes int64
}

// NewTransferRate averages over window (5 s when 0); a speed is given once
// the measured span is at least settle long (0.5 s when 0).
func NewTransferRate(window, settle time.Duration) *TransferRate {
	if window <= 0 {
		window = 5 * time.Second
	}
	if settle <= 0 {
		settle = 500 * time.Millisecond
	}
	return &TransferRate{window: window, settle: settle}
}

// BytesPerSecond over the window; 0 until enough time has passed to tell.
func (r *TransferRate) BytesPerSecond() float64 { return r.bytesPerSecond }

// Add records that bytes were transferred in total after elapsed.
func (r *TransferRate) Add(elapsed time.Duration, bytes int64) {
	r.samples = append(r.samples, rateSample{elapsed, bytes})
	// One sample older than the window stays as the start of the measured span.
	for len(r.samples) > 2 && elapsed-r.samples[1].at >= r.window {
		r.samples = r.samples[1:]
	}
	start := r.samples[0]
	seconds := (elapsed - start.at).Seconds()
	if seconds >= r.settle.Seconds() && seconds > 0 {
		r.bytesPerSecond = float64(max(0, bytes-start.bytes)) / seconds
	}
}

// Remaining is the time until total bytes are reached; ok is false while the speed is unknown.
func (r *TransferRate) Remaining(done, total int64) (time.Duration, bool) {
	if r.bytesPerSecond <= 0 {
		return 0, false
	}
	return time.Duration(float64(max(0, total-done)) / r.bytesPerSecond * float64(time.Second)), true
}
