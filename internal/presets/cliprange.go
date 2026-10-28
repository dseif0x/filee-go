package presets

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
)

// MinClipLength is the shortest clip in seconds: about three frames, so a clip is never empty.
const MinClipLength = 0.1

// ClipRange is a stretch of a video in seconds from its start, Start before End.
type ClipRange struct {
	Start, End float64
}

// Length of the range in seconds.
func (r ClipRange) Length() float64 { return r.End - r.Start }

// knownDuration returns the duration when it is a usable length.
func knownDuration(duration float64) (float64, bool) {
	return duration, duration > 0 && finite(duration)
}

// ClampClipRange is the clip from start to end inside a video of duration
// seconds (0 when unknown: only the start is kept at 0 or later). Ends given
// the wrong way round are swapped, and a clip shorter than MinClipLength grows
// to it, towards the end while there is room. Values that are no numbers count
// as 0 (start) and the whole video (end).
func ClampClipRange(start, end, duration float64) ClipRange {
	total, known := knownDuration(duration)
	if !finite(start) {
		start = 0
	}
	if !finite(end) {
		if known {
			end = total
		} else {
			end = start + MinClipLength
		}
	}
	if end < start {
		start, end = end, start
	}
	start, end = max(0, start), max(0, end)
	if known {
		start, end = min(start, total), min(end, total)
	}
	if end-start < MinClipLength {
		end = start + MinClipLength
		if known && end > total {
			end = total
			start = max(0, end-MinClipLength)
		}
	}
	return ClipRange{start, end}
}

// ClipDisplay is a time as the clip window shows it, to a tenth of a second:
// 0:07.5, 4:05.0, 1:02:05.3 (hours only when there are any). Tenths are
// rounded half away from zero; a negative or unknown time is 0.
func ClipDisplay(seconds float64) string {
	var tenths int64
	if finite(seconds) && seconds > 0 {
		tenths = int64(math.Round(seconds * 10))
	}
	hours := tenths / 36_000
	minutes := tenths / 600 % 60
	secs := tenths / 10 % 60
	tenth := tenths % 10
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d.%d", hours, minutes, secs, tenth)
	}
	return fmt.Sprintf("%d:%02d.%d", minutes, secs, tenth)
}

// ClipFfmpeg is a time as FFmpeg's -ss and -to read it: seconds with three decimals (65.300).
func ClipFfmpeg(seconds float64) string {
	if !finite(seconds) {
		seconds = 0
	}
	return strconv.FormatFloat(max(0, seconds), 'f', 3, 64)
}

// ---- the rest of ClipEdit ----

// ErrClipEmpty: nothing of the video is left to play.
var ErrClipEmpty = errors.New("the clip has no part of the video left")

// ClipEditOf is one stretch of the video, as recorded.
func ClipEditOf(r ClipRange) *ClipEdit { return ClipOf(r.Start, r.End) }

// Clamped is the clip as it can be written from a video of duration seconds
// (0 when unknown): every part inside the video and at least MinClipLength
// long, in order and not overlapping (a part left empty is dropped), speeds
// and volumes within their limits, and fades no longer than half the clip.
func (c *ClipEdit) Clamped(duration float64) (*ClipEdit, error) {
	total, known := knownDuration(duration)
	var parts []ClipPart
	last := 0.0
	for _, part := range c.Parts {
		if !finite(part.Start) || !finite(part.End) {
			continue
		}
		start := part.Start
		if !part.Blank {
			start = max(part.Start, last)
		}
		end := part.End
		if known {
			start, end = min(start, total), min(end, total)
		}
		start = max(0, start)
		if end-start < MinClipLength {
			continue
		}
		volume := part.PlayVolume()
		if part.Muted {
			volume = part.Volume
		}
		parts = append(parts, ClipPart{Start: start, End: end, Blank: part.Blank, Volume: volume, Muted: part.Muted, Speed: part.PlaySpeed()})
		if !part.Blank {
			last = end
		}
	}
	plays := false
	for _, p := range parts {
		plays = plays || !p.Blank
	}
	if !plays {
		return nil, ErrClipEmpty
	}
	// Pauses only between parts that play: at either end they would only be black.
	for parts[0].Blank {
		parts = parts[1:]
	}
	for parts[len(parts)-1].Blank {
		parts = parts[:len(parts)-1]
	}
	clip := &ClipEdit{Parts: parts}
	half := clip.Length() / 2
	clip.FadeIn = min(ClipFade(c.FadeIn), half)
	clip.FadeOut = min(ClipFade(c.FadeOut), half)
	// Music only where it plays: from 0 into the clip, from a place in its file, for some time.
	if m := c.Music; m != nil && m.Path != "" && finite(m.Offset) && finite(m.From) && finite(m.Length) {
		kept := *m
		kept.Offset = max(0, kept.Offset)
		kept.From = clamp(kept.From, 0, max(0, kept.Length))
		kept.Volume = kept.PlayVolume()
		if kept.PlayLength(clip.Length()) >= MinClipLength {
			clip.Music = &kept
		}
	}
	return clip, nil
}

// From is the rest of the clip from seconds into it on, for playing it from
// there: the part playing then starts where it is, the parts before it are
// left out, and the fade in stays only where it is still running.
func (c *ClipEdit) From(seconds float64) *ClipEdit {
	rest := &ClipEdit{FadeOut: c.FadeOut, Parts: []ClipPart{}}
	if c.Music != nil {
		// The music keeps its place against the pictures: what has played of it is left out.
		m := *c.Music
		m.From += max(0, seconds-m.Offset)
		m.Offset = max(0, m.Offset-max(0, seconds))
		rest.Music = &m
	}
	at := 0.0
	skip := max(0, seconds)
	for _, part := range c.Parts {
		length := part.Length()
		if at+length <= skip {
			at += length
			continue
		}
		cp := part
		if at < skip {
			cp.Start += (skip - at) * part.PlaySpeed()
		}
		rest.Parts = append(rest.Parts, cp)
		at += length
	}
	rest.FadeIn = max(0, ClipFade(c.FadeIn)-skip)
	return rest
}

// SourceTimeAt is where in the video the clip is at seconds into it (for the
// playhead); in a pause, where the pause stands.
func (c *ClipEdit) SourceTimeAt(seconds float64) float64 {
	at := 0.0
	for _, part := range c.Parts {
		length := part.Length()
		if seconds <= at+length {
			return part.Start + max(0, seconds-at)*part.PlaySpeed()
		}
		at += length
	}
	if len(c.Parts) > 0 {
		return c.Parts[len(c.Parts)-1].End
	}
	return 0
}

// ClipTimeAt is how far into the clip the video at sourceTime plays (the next
// part's start when it is cut out).
func (c *ClipEdit) ClipTimeAt(sourceTime float64) float64 {
	at := 0.0
	for _, part := range c.Parts {
		if sourceTime < part.Start {
			return at
		}
		if sourceTime <= part.End {
			return at + (sourceTime-part.Start)/part.PlaySpeed()
		}
		at += part.Length()
	}
	return at
}

// UnmarshalJSON reads a part over its defaults (volume and speed 1).
func (p *ClipPart) UnmarshalJSON(b []byte) error {
	type plain ClipPart
	d := plain{Volume: 1, Speed: 1}
	if err := json.Unmarshal(b, &d); err != nil {
		return err
	}
	*p = ClipPart(d)
	return nil
}

// UnmarshalJSON reads music over its defaults (usual volume, fading).
func (m *ClipMusic) UnmarshalJSON(b []byte) error {
	type plain ClipMusic
	d := plain{Volume: DefaultMusicVolume, Fade: true}
	if err := json.Unmarshal(b, &d); err != nil {
		return err
	}
	*m = ClipMusic(d)
	return nil
}
