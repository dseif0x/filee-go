package presets

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
)

// The clip window's editing, without the window: the stretch of the video
// between the start and end handles, cut into pieces with the blade. A piece
// can be cut out (leaving a pause that can be closed, or closing up at once),
// put back, trimmed by its edges, made louder or quieter, muted, faster or
// slower; silent stretches can be taken out at once; music can go under the
// clip. Every change can be undone. ToEdit gives what the conversion writes.

// ClipPieceState is what happens to a piece of the clip.
type ClipPieceState int

const (
	// PieceKept plays.
	PieceKept ClipPieceState = iota
	// PieceRemoved is cut out, and what follows closed up.
	PieceRemoved
	// PieceBlank is cut out, its time left as a pause (until the gap is closed).
	PieceBlank
)

// ClipPiece is a piece of the clip between two cuts, in seconds of the video.
type ClipPiece struct {
	Start, End float64
	State      ClipPieceState
	Volume     float64
	Speed      float64
	Muted      bool
}

// NewClipPiece is a kept piece at volume and speed 1.
func NewClipPiece(start, end float64) ClipPiece {
	return ClipPiece{Start: start, End: end, Volume: 1, Speed: 1}
}

func (p ClipPiece) IsKept() bool { return p.State == PieceKept }

// Length is how long it plays in the clip (nothing when removed).
func (p ClipPiece) Length() float64 {
	if p.State == PieceRemoved {
		return 0
	}
	return max(0, p.End-p.Start) / p.Speed
}

const (
	clipUndoDepth = 100
	// MinSilence: a silent stretch shorter than this is a pause in speech, not a gap.
	MinSilence = 0.4
	// SilencePadding is kept of a silence on either side next to sound, so words aren't clipped.
	SilencePadding = 0.12
)

type clipState struct {
	pieces          []ClipPiece
	fadeIn, fadeOut float64
	music           *ClipMusic
}

// ClipCutter cuts a stretch of a video into pieces and changes them.
type ClipCutter struct {
	duration        float64
	pieces          []ClipPiece
	fadeIn, fadeOut float64
	music           *ClipMusic
	undo, redo      []clipState
	joining         string
	batching        int

	// Changed is called after every change (also undo and redo).
	Changed func()
}

// NewClipCutter starts with r kept inside a video of duration seconds.
func NewClipCutter(duration float64, r ClipRange) *ClipCutter {
	d := max(MinClipLength, duration)
	clip := ClampClipRange(r.Start, r.End, d)
	return &ClipCutter{duration: d, pieces: []ClipPiece{NewClipPiece(clip.Start, clip.End)}}
}

func (c *ClipCutter) Duration() float64 { return c.duration }

// Pieces from the start to the end handle, one after the other without gaps (a copy).
func (c *ClipCutter) Pieces() []ClipPiece { return slices.Clone(c.pieces) }

func (c *ClipCutter) Start() float64   { return c.pieces[0].Start }
func (c *ClipCutter) End() float64     { return c.pieces[len(c.pieces)-1].End }
func (c *ClipCutter) FadeIn() float64  { return c.fadeIn }
func (c *ClipCutter) FadeOut() float64 { return c.fadeOut }
func (c *ClipCutter) CanUndo() bool    { return len(c.undo) > 0 }
func (c *ClipCutter) CanRedo() bool    { return len(c.redo) > 0 }

// Music under the clip (a copy), or nil.
func (c *ClipCutter) Music() *ClipMusic {
	if c.music == nil {
		return nil
	}
	m := *c.music
	return &m
}

func (c *ClipCutter) SetFadeIn(v float64) {
	f := ClipFade(v)
	c.change(func() { c.fadeIn = f }, c.fadeIn != f, "")
}

func (c *ClipCutter) SetFadeOut(v float64) {
	f := ClipFade(v)
	c.change(func() { c.fadeOut = f }, c.fadeOut != f, "")
}

// Length is how long the clip plays, in seconds.
func (c *ClipCutter) Length() float64 {
	var l float64
	for _, p := range c.pieces {
		l += p.Length()
	}
	return l
}

// HasGaps: pieces were cut out leaving a pause.
func (c *ClipCutter) HasGaps() bool {
	return slices.ContainsFunc(c.pieces, func(p ClipPiece) bool { return p.State == PieceBlank })
}

// IndexAt is the piece at seconds of the video, or -1 outside the clip.
func (c *ClipCutter) IndexAt(seconds float64) int {
	if seconds < c.Start() || seconds > c.End() {
		return -1
	}
	for i, p := range c.pieces {
		if seconds < p.End || i == len(c.pieces)-1 {
			return i
		}
	}
	return -1
}

// ---- the ends ----

// MoveStart moves the start; past the end it takes the clip along, cuts and all.
func (c *ClipCutter) MoveStart(seconds float64) {
	if !finite(seconds) {
		return
	}
	start := clamp(seconds, 0, c.duration-MinClipLength)
	if start == c.Start() {
		return
	}
	c.change(func() {
		if start > c.End()-MinClipLength {
			c.shift(start - c.Start())
			c.bound(c.Start(), min(c.End(), c.duration))
		} else {
			c.bound(start, c.End())
		}
	}, true, "")
}

// MoveEnd moves the end; before the start it takes the clip along.
func (c *ClipCutter) MoveEnd(seconds float64) {
	if !finite(seconds) {
		return
	}
	end := clamp(seconds, MinClipLength, c.duration)
	if end == c.End() {
		return
	}
	c.change(func() {
		if end < c.Start()+MinClipLength {
			c.shift(end - c.End())
			c.bound(max(0, c.Start()), c.End())
		} else {
			c.bound(c.Start(), end)
		}
	}, true, "")
}

// Move moves the whole clip, cuts and all, to start at seconds.
func (c *ClipCutter) Move(seconds float64) {
	if !finite(seconds) {
		return
	}
	start := clamp(seconds, 0, max(0, c.duration-(c.End()-c.Start())))
	if start != c.Start() {
		c.change(func() { c.shift(start - c.Start()) }, true, "")
	}
}

// SetLength puts the end seconds after the start; when the video ends sooner, its last seconds.
func (c *ClipCutter) SetLength(seconds float64) {
	if !finite(seconds) || seconds <= 0 {
		return
	}
	length := clamp(seconds, MinClipLength, c.duration)
	start := min(c.Start(), c.duration-length)
	c.change(func() { c.bound(start, start+length) }, true, "")
}

// ---- cuts ----

// Split cuts the piece at seconds in two; false outside the clip or too close to a cut.
func (c *ClipCutter) Split(seconds float64) bool {
	i := c.IndexAt(seconds)
	if i < 0 {
		return false
	}
	p := c.pieces[i]
	if seconds-p.Start < MinClipLength || p.End-seconds < MinClipLength {
		return false
	}
	c.change(func() {
		left, right := p, p
		left.End, right.Start = seconds, seconds
		c.pieces[i] = left
		c.pieces = slices.Insert(c.pieces, i+1, right)
	}, true, "")
	return true
}

// Remove cuts a piece out: closeUp closes what follows up (a ripple delete),
// otherwise its time stays as a pause. False for the last piece that plays.
func (c *ClipCutter) Remove(index int, closeUp bool) bool {
	if index < 0 || index >= len(c.pieces) {
		return false
	}
	state := PieceBlank
	if closeUp {
		state = PieceRemoved
	}
	p := c.pieces[index]
	if p.State == state || p.IsKept() && c.kept() == 1 {
		return false
	}
	c.change(func() { c.pieces[index].State = state }, true, "")
	return true
}

func (c *ClipCutter) kept() int {
	n := 0
	for _, p := range c.pieces {
		if p.IsKept() {
			n++
		}
	}
	return n
}

// Restore puts a piece that was cut out back.
func (c *ClipCutter) Restore(index int) bool {
	if index < 0 || index >= len(c.pieces) || c.pieces[index].IsKept() {
		return false
	}
	c.change(func() { c.pieces[index].State = PieceKept }, true, "")
	return true
}

// CloseGap closes the pause piece index left: what follows closes up.
func (c *ClipCutter) CloseGap(index int) bool {
	if index < 0 || index >= len(c.pieces) || c.pieces[index].State != PieceBlank {
		return false
	}
	c.change(func() { c.pieces[index].State = PieceRemoved }, true, "")
	return true
}

// CloseGaps closes every pause and returns how many.
func (c *ClipCutter) CloseGaps() int {
	gaps := 0
	for _, p := range c.pieces {
		if p.State == PieceBlank {
			gaps++
		}
	}
	if gaps > 0 {
		c.change(func() {
			next := slices.Clone(c.pieces)
			for i := range next {
				if next[i].State == PieceBlank {
					next[i].State = PieceRemoved
				}
			}
			c.pieces = next
		}, true, "")
	}
	return gaps
}

// TrimEdge drags an edge of piece index (its end with end, else its start) to
// seconds of the video, as a trim on the timeline: outwards it takes back what
// was cut out next to it or, at the clip's ends, more of the video; inwards it
// shortens the piece and what it gives up is cut out. A drag is one undo step.
// Returns where the edge ended up (NaN when nothing was done).
func (c *ClipCutter) TrimEdge(index int, end bool, seconds float64) float64 {
	if index < 0 || index >= len(c.pieces) || !finite(seconds) {
		return math.NaN()
	}
	join := fmt.Sprintf("trim %d %v", index, end)
	if end {
		if index == len(c.pieces)-1 {
			to := clamp(seconds, c.pieces[index].Start+MinClipLength, c.duration)
			if to != c.End() {
				c.change(func() { c.bound(c.Start(), to) }, true, join)
			}
			return c.End()
		}
		c.change(func() { c.moveCut(index, seconds, true) }, true, join)
		return c.pieces[min(index, len(c.pieces)-1)].End
	}
	if index == 0 {
		from := clamp(seconds, 0, c.pieces[0].End-MinClipLength)
		if from != c.Start() {
			c.change(func() { c.bound(from, c.End()) }, true, join)
		}
		return c.Start()
	}
	c.change(func() { c.moveCut(index-1, seconds, false) }, true, join)
	return c.pieces[min(index, len(c.pieces)-1)].Start
}

// SetVolume sets the volume of a piece (1 as recorded, 0 silent).
func (c *ClipCutter) SetVolume(index int, volume float64) {
	if index < 0 || index >= len(c.pieces) || !finite(volume) {
		return
	}
	v := clamp(math.Round(volume*100)/100, 0, MaxClipVolume)
	if c.pieces[index].Volume != v {
		c.change(func() { c.pieces[index].Volume = v }, true, fmt.Sprintf("volume %d", index))
	}
}

// SetMuted mutes or unmutes the sound of a piece; its picture plays on.
func (c *ClipCutter) SetMuted(index int, muted bool) {
	if index >= 0 && index < len(c.pieces) && c.pieces[index].Muted != muted {
		c.change(func() { c.pieces[index].Muted = muted }, true, "")
	}
}

// SetSpeed sets the speed of a piece (1 as recorded).
func (c *ClipCutter) SetSpeed(index int, speed float64) {
	if index < 0 || index >= len(c.pieces) || !finite(speed) || speed <= 0 {
		return
	}
	v := clamp(speed, MinClipSpeed, MaxClipSpeed)
	if c.pieces[index].Speed != v {
		c.change(func() { c.pieces[index].Speed = v }, true, "")
	}
}

// ---- music ----

// SetMusic lays music under the clip (nil takes it away).
func (c *ClipCutter) SetMusic(m *ClipMusic) {
	c.change(func() {
		if m == nil {
			c.music = nil
		} else {
			cp := *m
			c.music = &cp
		}
	}, true, "")
}

// MoveMusic moves the music to start offset seconds into the clip (a drag is one undo step).
func (c *ClipCutter) MoveMusic(offset float64) {
	if c.music == nil || !finite(offset) {
		return
	}
	v := clamp(offset, 0, max(0, c.Length()-MinClipLength))
	if v != c.music.Offset {
		c.change(func() { c.music = c.withMusic(func(m *ClipMusic) { m.Offset = v }) }, true, "music offset")
	}
}

// SetMusicVolume sets the music's volume (0 to 2; a drag is one undo step).
func (c *ClipCutter) SetMusicVolume(volume float64) {
	if c.music == nil || !finite(volume) {
		return
	}
	v := clamp(math.Round(volume*100)/100, 0, 2)
	if v != c.music.Volume {
		c.change(func() { c.music = c.withMusic(func(m *ClipMusic) { m.Volume = v }) }, true, "music volume")
	}
}

func (c *ClipCutter) withMusic(f func(*ClipMusic)) *ClipMusic {
	cp := *c.music
	f(&cp)
	return &cp
}

// RemoveSilences takes the silent stretches (from FFmpeg's silencedetect) out
// of the clip: each at least MinSilence long, less SilencePadding next to
// sound, is cut out and the rest closes up. Something always stays. Returns
// how many stretches were taken out; one undo puts them all back.
func (c *ClipCutter) RemoveSilences(silences []ClipRange) int {
	sorted := slices.Clone(silences)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })
	type stretch struct{ start, end float64 }
	var stretches []stretch
	for _, s := range sorted {
		start := s.Start + SilencePadding
		if s.Start <= c.Start()+1e-6 {
			start = c.Start()
		}
		end := s.End - SilencePadding
		if s.End >= c.End()-1e-6 {
			end = c.End()
		}
		start, end = max(start, c.Start()), min(end, c.End())
		if end-start >= max(MinSilence-2*SilencePadding, MinClipLength) {
			stretches = append(stretches, stretch{start, end})
		}
	}
	if len(stretches) == 0 {
		return 0
	}
	before := c.snapshot()
	pieces := c.pieces
	removed := 0
	for _, s := range stretches {
		pieces = cutAt(pieces, s.start)
		pieces = cutAt(pieces, s.end)
		next := slices.Clone(pieces)
		anyKept := false
		for i := range next {
			if next[i].IsKept() && next[i].Start >= s.start-1e-6 && next[i].End <= s.end+1e-6 {
				next[i].State = PieceRemoved
			}
			anyKept = anyKept || next[i].IsKept()
		}
		if anyKept {
			removed++
			pieces = next
		}
	}
	if removed == 0 {
		return 0
	}
	c.remember(before)
	c.joining = ""
	c.pieces = pieces
	c.changed()
	return removed
}

// Batch makes several changes one: one undo step and one Changed. Batches in
// a row with the same non-empty join are one undo step.
func (c *ClipCutter) Batch(changes func(), join string) {
	before := c.snapshot()
	c.batching++
	func() {
		defer func() { c.batching-- }()
		changes()
	}()
	if c.batching > 0 || c.same(before) {
		return
	}
	if join == "" || join != c.joining || len(c.undo) == 0 {
		c.remember(before)
	} else {
		c.redo = nil
	}
	c.joining = join
	c.changed()
}

func (c *ClipCutter) same(s clipState) bool {
	if !slices.Equal(s.pieces, c.pieces) || s.fadeIn != c.fadeIn || s.fadeOut != c.fadeOut {
		return false
	}
	if s.music == nil || c.music == nil {
		return s.music == nil && c.music == nil
	}
	return s.music.Path == c.music.Path && s.music.Offset == c.music.Offset && s.music.Volume == c.music.Volume && s.music.From == c.music.From
}

// Undo takes back the last change.
func (c *ClipCutter) Undo() bool { return c.step(&c.undo, &c.redo) }

// Redo does the last undone change again.
func (c *ClipCutter) Redo() bool { return c.step(&c.redo, &c.undo) }

// ErrAllCut: every piece of the clip is cut out.
var ErrAllCut = errors.New("every piece of the clip is cut out")

// ToEdit is what the conversion writes: the pieces that play (neighbours
// alike joined), the pauses, the music and the fades.
func (c *ClipCutter) ToEdit() (*ClipEdit, error) {
	edit := &ClipEdit{FadeIn: c.fadeIn, FadeOut: c.fadeOut, Music: c.Music(), Parts: []ClipPart{}}
	for _, p := range c.pieces {
		if p.State == PieceRemoved {
			continue
		}
		blank := p.State == PieceBlank
		if n := len(edit.Parts); n > 0 {
			last := &edit.Parts[n-1]
			if !blank && !last.Blank && math.Abs(last.End-p.Start) < 1e-9 && last.Volume == p.Volume && last.Speed == p.Speed && last.Muted == p.Muted {
				last.End = p.End
				continue
			}
		}
		edit.Parts = append(edit.Parts, ClipPart{Start: p.Start, End: p.End, Blank: blank, Volume: p.Volume, Speed: p.Speed, Muted: p.Muted})
	}
	for _, p := range edit.Parts {
		if !p.Blank {
			return edit, nil
		}
	}
	return nil, ErrAllCut
}

// ---- inside ----

func (c *ClipCutter) changed() {
	if c.Changed != nil {
		c.Changed()
	}
}

func (c *ClipCutter) snapshot() clipState {
	return clipState{slices.Clone(c.pieces), c.fadeIn, c.fadeOut, c.Music()}
}

func (c *ClipCutter) remember(s clipState) {
	c.undo = append(c.undo, s)
	c.redo = nil
	if len(c.undo) > clipUndoDepth {
		c.undo = c.undo[len(c.undo)-clipUndoDepth:]
	}
}

func (c *ClipCutter) change(f func(), changes bool, join string) {
	if !changes {
		return
	}
	if c.batching > 0 {
		f()
		return
	}
	before := c.snapshot()
	f()
	if join == "" || join != c.joining || len(c.undo) == 0 {
		c.remember(before)
	} else {
		c.redo = nil
	}
	c.joining = join
	c.changed()
}

func (c *ClipCutter) step(from, to *[]clipState) bool {
	if len(*from) == 0 {
		return false
	}
	*to = append(*to, c.snapshot())
	s := (*from)[len(*from)-1]
	*from = (*from)[:len(*from)-1]
	c.pieces, c.fadeIn, c.fadeOut, c.music = s.pieces, s.fadeIn, s.fadeOut, s.music
	c.joining = ""
	c.changed()
	return true
}

func (c *ClipCutter) shift(delta float64) {
	next := slices.Clone(c.pieces)
	for i := range next {
		next[i].Start += delta
		next[i].End += delta
	}
	c.pieces = next
}

// moveCut moves the cut after piece left to seconds. The piece being dragged
// (the left one when growing, else the right one) grows over pieces cut out
// next to it, using them up; a piece that plays next to it gives way down to
// MinClipLength. Dragged inwards, what it gives up becomes a removed piece,
// unless its neighbour plays: then the cut just moves.
func (c *ClipCutter) moveCut(left int, seconds float64, growing bool) {
	pieces := slices.Clone(c.pieces)
	defer func() { c.pieces = pieces }()
	if growing {
		piece := pieces[left]
		to := max(seconds, piece.Start+MinClipLength)
		for left+1 < len(pieces) && !pieces[left+1].IsKept() && to >= pieces[left+1].End-MinClipLength {
			pieces = slices.Delete(pieces, left+1, left+2)
		}
		if left+1 >= len(pieces) {
			c.pieces = pieces
			c.bound(c.Start(), clamp(to, piece.Start+MinClipLength, c.duration))
			pieces = c.pieces
			return
		}
		next := pieces[left+1]
		to = min(to, next.End-MinClipLength)
		switch {
		case to < piece.End && piece.IsKept() && !next.IsKept():
			// Shortened next to a piece cut out: what it gives up joins that one.
			pieces[left].End = to
			pieces[left+1].Start = to
		case to < piece.End && piece.IsKept() && next.IsKept():
			// Shortened next to a piece that plays: the given-up part is cut out, nothing else moves.
			pieces[left].End = to
			gap := ClipPiece{Start: to, End: next.Start, State: PieceRemoved, Volume: piece.Volume, Speed: piece.Speed, Muted: piece.Muted}
			if gap.End-gap.Start >= 1e-9 {
				pieces = slices.Insert(pieces, left+1, gap)
			}
		default:
			pieces[left].End = to
			pieces[left+1].Start = to
		}
		return
	}
	right := left + 1
	piece := pieces[right]
	from := min(seconds, piece.End-MinClipLength)
	for right-1 >= 0 && !pieces[right-1].IsKept() && from <= pieces[right-1].Start+MinClipLength {
		pieces = slices.Delete(pieces, right-1, right)
		right--
	}
	if right-1 < 0 {
		c.pieces = pieces
		c.bound(clamp(from, 0, piece.End-MinClipLength), c.End())
		pieces = c.pieces
		return
	}
	previous := pieces[right-1]
	from = max(from, previous.Start+MinClipLength)
	if from > piece.Start && piece.IsKept() && previous.IsKept() {
		// Shortened next to a piece that plays: the given-up part is cut out.
		pieces[right].Start = from
		pieces = slices.Insert(pieces, right, ClipPiece{Start: previous.End, End: from, State: PieceRemoved, Volume: piece.Volume, Speed: piece.Speed, Muted: piece.Muted})
		return
	}
	pieces[right].Start = from
	pieces[right-1].End = from
}

// bound keeps the pieces between start and end: those outside dropped, the
// first and last trimmed or stretched to the ends, slivers shorter than
// MinClipLength joined to their neighbour. Something always plays.
func (c *ClipCutter) bound(start, end float64) {
	var inside []ClipPiece
	for _, p := range c.pieces {
		p.Start, p.End = max(p.Start, start), min(p.End, end)
		if p.End-p.Start > 1e-9 {
			inside = append(inside, p)
		}
	}
	if len(inside) == 0 {
		p := c.pieces[0]
		if start >= c.End() {
			p = c.pieces[len(c.pieces)-1]
		}
		p.Start, p.End = start, end
		inside = []ClipPiece{p}
	}
	inside[0].Start = start
	inside[len(inside)-1].End = end
	for len(inside) > 1 && inside[0].End-inside[0].Start < MinClipLength {
		inside = inside[1:]
		inside[0].Start = start
	}
	for len(inside) > 1 && inside[len(inside)-1].End-inside[len(inside)-1].Start < MinClipLength {
		inside = inside[:len(inside)-1]
		inside[len(inside)-1].End = end
	}
	if !slices.ContainsFunc(inside, ClipPiece.IsKept) {
		inside[0].State = PieceKept
	}
	c.pieces = inside
}

// cutAt is pieces cut at seconds, unless it is (nearly) a cut already.
func cutAt(pieces []ClipPiece, seconds float64) []ClipPiece {
	i := slices.IndexFunc(pieces, func(p ClipPiece) bool { return seconds > p.Start+1e-6 && seconds < p.End-1e-6 })
	if i < 0 {
		return pieces
	}
	out := slices.Clone(pieces)
	left, right := out[i], out[i]
	left.End, right.Start = seconds, seconds
	out[i] = left
	return slices.Insert(out, i+1, right)
}
