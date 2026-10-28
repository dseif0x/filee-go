package images

import (
	"image"
	"image/color"
	"image/gif"
	"math"
)

// Playback speed of animations (ImageOptions.AnimationSpeed): rewrites the
// frame delays of an animation written as GIF, WebP, APNG or MNG, and drops
// frames where a delay would get shorter than players show it.

const (
	// MinDelayMs is the shortest delay written: browsers show a frame asking
	// for 10 ms or less for 100 ms, so a shorter delay would play slower.
	MinDelayMs        = 20
	shortDelayShownMs = 100
	maxHundredths     = 65535
)

// TicksPerSecond delays are written in: hundredths for GIF and MNG, milliseconds for WebP and APNG.
func TicksPerSecond(target string) int {
	if target == "webp" || target == "apng" {
		return 1000
	}
	return 100
}

// ChangeSpeed returns the frames to write, playing speed times as fast: every
// delay divided by speed in target's units, rounded on the running total so
// the animation keeps its new length. A frame that would start less than
// MinDelayMs after the one before is dropped (its time goes to the frame
// before it). Delays of 10 ms or less count as the 100 ms players show.
func ChangeSpeed(frames []*Frame, speed float64, target string) []*Frame {
	if len(frames) < 2 || math.IsNaN(speed) || math.IsInf(speed, 0) || speed <= 0 || speed == 1 {
		return frames
	}
	ticks := TicksPerSecond(target)
	minTicks := int64(MinDelayMs * ticks / 1000)
	starts := make([]float64, len(frames)+1)
	for i, f := range frames {
		starts[i+1] = starts[i] + shownDelayMs(f)*float64(ticks)/1000/speed
	}
	type kept struct {
		index int
		start int64
	}
	var keep []kept
	for i := range frames {
		start := int64(math.Round(starts[i]))
		if len(keep) == 0 || start-keep[len(keep)-1].start >= minTicks {
			keep = append(keep, kept{i, start})
		}
	}
	end := int64(math.Round(starts[len(frames)]))
	if len(keep) > 1 && end-keep[len(keep)-1].start < minTicks {
		keep = keep[:len(keep)-1]
	}
	out := make([]*Frame, 0, len(keep))
	for k, kp := range keep {
		f := frames[kp.index]
		next := end
		if k+1 < len(keep) {
			next = keep[k+1].start
		}
		delay := max(minTicks, next-kp.start)
		if ticks == 100 {
			delay = min(delay, maxHundredths)
		} else {
			delay = min(delay, math.MaxInt32)
		}
		f.Ticks, f.Delay = ticks, int(delay)
		out = append(out, f)
	}
	return out
}

func shownDelayMs(f *Frame) float64 {
	ms := f.DelayMs()
	if ms <= 10 {
		return shortDelayShownMs
	}
	return ms
}

// coalesceGIF composes the frames of a GIF onto its canvas as a player shows
// them (disposal applied), so every frame is a whole picture.
func coalesceGIF(g *gif.GIF) *Picture {
	w, h := g.Config.Width, g.Config.Height
	if w == 0 || h == 0 {
		for _, f := range g.Image {
			w, h = max(w, f.Rect.Max.X), max(h, f.Rect.Max.Y)
		}
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, w, h))
	p := &Picture{Loops: g.LoopCount}
	if p.Loops < 0 {
		p.Loops = 1 // GIF: -1 plays once
	}
	alpha := false
	for i, frame := range g.Image {
		var disposal byte
		if i < len(g.Disposal) {
			disposal = g.Disposal[i]
		}
		var previous *image.NRGBA
		if disposal == gif.DisposalPrevious {
			previous = image.NewNRGBA(canvas.Rect)
			copy(previous.Pix, canvas.Pix)
		}
		r := frame.Rect.Intersect(canvas.Rect)
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				idx := frame.Pix[frame.PixOffset(x, y)]
				if int(idx) >= len(frame.Palette) {
					continue
				}
				c := color.NRGBAModel.Convert(frame.Palette[idx]).(color.NRGBA)
				if c.A == 0 {
					continue
				}
				o := canvas.PixOffset(x, y)
				canvas.Pix[o], canvas.Pix[o+1], canvas.Pix[o+2], canvas.Pix[o+3] = c.R, c.G, c.B, c.A
			}
		}
		out := image.NewNRGBA(canvas.Rect)
		copy(out.Pix, canvas.Pix)
		if !alpha && !opaque(out) {
			alpha = true
		}
		f := &Frame{Img: out, Ticks: 100}
		if i < len(g.Delay) {
			f.Delay = g.Delay[i]
		}
		p.Frames = append(p.Frames, f)
		switch disposal {
		case gif.DisposalBackground:
			for y := r.Min.Y; y < r.Max.Y; y++ {
				clear(canvas.Pix[canvas.PixOffset(r.Min.X, y):canvas.PixOffset(r.Max.X, y)])
			}
		case gif.DisposalPrevious:
			canvas = previous
		}
	}
	for _, f := range p.Frames {
		f.Alpha = alpha
	}
	return p
}

// encodeGIF writes frames as a GIF (an animation when there are several).
func encodeGIF(frames []*Frame, loops int) *gif.GIF {
	w, h := 0, 0
	for _, f := range frames {
		w, h = max(w, f.Width()), max(h, f.Height())
	}
	g := &gif.GIF{Config: image.Config{Width: w, Height: h}}
	if len(frames) > 1 {
		g.LoopCount = loops
	} else {
		g.LoopCount = -1
	}
	for _, f := range frames {
		pal := quantize(f.Img, 256)
		g.Image = append(g.Image, pal)
		delay := f.Delay
		if f.Ticks > 0 && f.Ticks != 100 {
			delay = int(math.Round(float64(f.Delay) * 100 / float64(f.Ticks)))
		}
		g.Delay = append(g.Delay, min(delay, 65535))
		// A frame with transparency replaces the one before instead of being drawn over it.
		if pal.Palette[0] == (color.NRGBA{}) {
			g.Disposal = append(g.Disposal, gif.DisposalBackground)
		} else {
			g.Disposal = append(g.Disposal, gif.DisposalNone)
		}
	}
	return g
}
