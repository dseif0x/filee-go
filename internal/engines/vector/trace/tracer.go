package trace

import (
	"bufio"
	"context"
	"fmt"
	"image"
	"image/draw"
	"io"
	"math"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	xdraw "golang.org/x/image/draw"

	"github.com/dseif0x/filee-go/internal/presets"
)

// Settings are the numbers of one tracing run, derived from the preset's
// TracingOptions.
type Settings struct {
	Mode            presets.TracingMode
	Colors          int
	SpeckleArea     float64
	MinRegion       int
	MergeEdgeBands  bool
	CurveTolerance  float64
	CornerAngle     float64
	CornerRun       int
	SmoothingPasses int
}

// SettingsFrom turns mode, colours and detail into tracing parameters;
// longEdge is the traced picture's longer side in pixels.
func SettingsFrom(o presets.TracingOptions, longEdge int) *Settings {
	f := float64(max(0, min(100, o.Detail))) / 100
	coarse := (1 - f) * (1 - f)
	// Specks are counted in pixels, so a larger picture of the same artwork has larger specks.
	size := math.Max(1, math.Min(4, float64(longEdge)/1000))
	passes := 0
	switch {
	case f < 0.34:
		passes = 3
	case f < 0.67:
		passes = 2
	case f < 0.9:
		passes = 1
	}
	run := 3
	if f > 0.8 {
		run = 2
	}
	mode := o.Mode
	if mode != presets.TraceBlackAndWhite {
		mode = presets.TraceColor
	}
	return &Settings{
		Mode:            mode,
		Colors:          max(presets.MinTraceColors, min(presets.MaxTraceColors, o.Colors)),
		SpeckleArea:     math.Round(24 * coarse * size),
		MinRegion:       int(math.Round(1 + 64*coarse*size)),
		MergeEdgeBands:  f < 0.9,
		CurveTolerance:  0.25 + 0.9*(1-f),
		CornerAngle:     70 - 30*f,
		CornerRun:       run,
		SmoothingPasses: passes,
	}
}

// Layer is one colour of a traced picture.
type Layer struct {
	Color int
	Paths []Path
}

// Result is a traced picture: its size in pixels and its shapes, bottom first.
type Result struct {
	Width, Height int
	Layers        []Layer
}

const (
	// MaxPixels: larger pictures are reduced before tracing.
	MaxPixels = 6_000_000
	// MaxSide is the longest side traced.
	MaxSide = 4096
)

// TracedSize is the size a width × height picture is traced at.
func TracedSize(width, height int) (int, int) {
	scale := math.Min(1, math.Min(MaxSide/float64(max(width, height)), math.Sqrt(MaxPixels/(float64(width)*float64(height)))))
	if scale >= 1 {
		return width, height
	}
	return max(1, int(math.Round(float64(width)*scale))), max(1, int(math.Round(float64(height)*scale)))
}

// Picture traces an image (reduced to TracedSize first) and returns the
// result and the straight RGBA size traced.
func Picture(ctx context.Context, img image.Image, o presets.TracingOptions, progress func(float64)) (*Result, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("the image has no pictures to trace")
	}
	tw, th := TracedSize(w, h)
	nrgba := image.NewNRGBA(image.Rect(0, 0, tw, th))
	if tw != w || th != h {
		xdraw.CatmullRom.Scale(nrgba, nrgba.Bounds(), img, b, xdraw.Src, nil)
	} else {
		draw.Draw(nrgba, nrgba.Bounds(), img, b.Min, draw.Src)
	}
	if progress != nil {
		progress(0.1)
	}
	s := SettingsFrom(o, max(tw, th))
	return Trace(ctx, nrgba.Pix, tw, th, s, func(p float64) {
		if progress != nil {
			progress(0.1 + 0.85*p)
		}
	})
}

// Trace traces straight-alpha RGBA pixels.
func Trace(ctx context.Context, rgba []byte, width, height int, s *Settings, progress func(float64)) (*Result, error) {
	report := func(p float64) {
		if progress != nil {
			progress(p)
		}
	}
	var layers []Layer
	var err error
	if s.Mode == presets.TraceBlackAndWhite {
		layers, err = traceBlackAndWhite(ctx, rgba, width, height, s, report)
	} else {
		layers, err = traceColors(ctx, rgba, width, height, s, report)
	}
	if err != nil {
		return nil, err
	}
	return &Result{Width: width, Height: height, Layers: layers}, nil
}

func traceBlackAndWhite(ctx context.Context, rgba []byte, width, height int, s *Settings, report func(float64)) ([]Layer, error) {
	mask := InkMask(rgba, width, height)
	report(0.2)
	outlines, err := TraceOutlines(ctx, mask, width, height, s.SpeckleArea)
	if err != nil {
		return nil, err
	}
	report(0.5)
	paths := fitAll(ctx, outlines, s)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if len(paths) == 0 {
		return nil, nil
	}
	return []Layer{{Color: 0, Paths: paths}}, nil
}

// InkMask is 1 for ink: on an opaque picture the pixels darker than Otsu's
// threshold (transparent ones count as white paper); on a transparent
// background the opaque pixels, or only their darker ones when the opaque part
// has light and dark areas (a black logo on a white plate).
func InkMask(rgba []byte, width, height int) []byte {
	count := width * height
	lum := make([]byte, count)
	var hist, opaqueHist [256]int64
	var transparent int64
	for p := 0; p < count; p++ {
		o := p * 4
		a := int(rgba[o+3])
		y := (int(rgba[o])*299 + int(rgba[o+1])*587 + int(rgba[o+2])*114) / 1000
		onWhite := byte(255 - (255-y)*a/255)
		lum[p] = onWhite
		hist[onWhite]++
		if a >= OpaqueAlpha {
			opaqueHist[y]++
		} else {
			transparent++
		}
	}
	mask := make([]byte, count)
	if transparent >= int64(count/100+1) {
		spread := percentile(opaqueHist[:], 0.95) - percentile(opaqueHist[:], 0.05)
		threshold := 256
		if spread >= 64 {
			threshold = Otsu(opaqueHist[:])
		}
		for p := 0; p < count; p++ {
			o := p * 4
			if rgba[o+3] < OpaqueAlpha {
				continue
			}
			y := (int(rgba[o])*299 + int(rgba[o+1])*587 + int(rgba[o+2])*114) / 1000
			if y < threshold {
				mask[p] = 1
			}
		}
		return mask
	}
	cut := Otsu(hist[:])
	if percentile(hist[:], 0.99)-percentile(hist[:], 0.01) < 32 {
		cut = 128
	}
	for p := 0; p < count; p++ {
		if int(lum[p]) < cut {
			mask[p] = 1
		}
	}
	return mask
}

// Otsu's threshold: values below it are one class, the rest the other.
func Otsu(hist []int64) int {
	var total int64
	sum := 0.0
	for i := 0; i < 256; i++ {
		total += hist[i]
		sum += float64(i) * float64(hist[i])
	}
	if total == 0 {
		return 128
	}
	bgSum, best := 0.0, -1.0
	var bg int64
	threshold := 128
	for t := 0; t < 255; t++ {
		bg += hist[t]
		if bg == 0 {
			continue
		}
		fg := total - bg
		if fg == 0 {
			break
		}
		bgSum += float64(t) * float64(hist[t])
		mb := bgSum / float64(bg)
		mf := (sum - bgSum) / float64(fg)
		between := float64(bg) * float64(fg) * (mb - mf) * (mb - mf)
		if between > best {
			best = between
			threshold = t + 1
		}
	}
	return threshold
}

func percentile(hist []int64, fraction float64) int {
	var total int64
	for _, h := range hist {
		total += h
	}
	if total == 0 {
		return 0
	}
	target := int64(float64(total) * fraction)
	var running int64
	for i := 0; i < 256; i++ {
		running += hist[i]
		if running > target {
			return i
		}
	}
	return 255
}

func traceColors(ctx context.Context, rgba []byte, width, height int, s *Settings, report func(float64)) ([]Layer, error) {
	palette, labels, err := Quantize(ctx, rgba, width, height, s.Colors)
	if err != nil {
		return nil, err
	}
	if len(palette) == 0 {
		return nil, nil
	}
	report(0.15)
	if s.MergeEdgeBands {
		MergeEdgeBands(labels, rgba, palette, width, height)
	}
	if err := MergeSmallRegions(ctx, labels, palette, width, height, s.MinRegion); err != nil {
		return nil, err
	}
	report(0.3)
	// Bottom to top by area: the background first, small details last.
	areas := make([]int64, len(palette))
	for _, l := range labels {
		if l != Transparent {
			areas[l]++
		}
	}
	var order []int
	for c := range palette {
		if areas[c] > 0 {
			order = append(order, c)
		}
	}
	sort.SliceStable(order, func(a, b int) bool {
		if areas[order[a]] != areas[order[b]] {
			return areas[order[a]] > areas[order[b]]
		}
		return order[a] < order[b]
	})
	var rank [256]int
	for i := range rank {
		rank[i] = -1
	}
	for i, c := range order {
		rank[c] = i
	}
	results := make([]Layer, len(order))
	var done atomic.Int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, max(1, runtime.GOMAXPROCS(0)))
	var firstErr error
	var mu sync.Mutex
	for layer := range order {
		wg.Add(1)
		sem <- struct{}{}
		go func(layer int) {
			defer wg.Done()
			defer func() { <-sem }()
			mask := make([]byte, len(labels))
			for p, l := range labels {
				if rank[l] >= layer {
					mask[p] = 1
				}
			}
			outlines, err := TraceOutlines(ctx, mask, width, height, s.SpeckleArea)
			if err != nil {
				mu.Lock()
				firstErr = err
				mu.Unlock()
				return
			}
			paths := make([]Path, 0, len(outlines))
			for i := range outlines {
				paths = append(paths, Fit(&outlines[i], s))
			}
			results[layer] = Layer{Color: palette[order[layer]], Paths: paths}
			report(0.3 + 0.65*float64(done.Add(1))/float64(len(order)))
		}(layer)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	out := results[:0]
	for _, l := range results {
		if len(l.Paths) > 0 {
			out = append(out, l)
		}
	}
	return out, nil
}

func fitAll(ctx context.Context, outlines []PixelOutline, s *Settings) []Path {
	paths := make([]Path, len(outlines))
	workers := max(1, runtime.GOMAXPROCS(0))
	var wg sync.WaitGroup
	chunk := (len(outlines) + workers - 1) / workers
	for w := 0; w < workers; w++ {
		lo, hi := w*chunk, min(len(outlines), (w+1)*chunk)
		if lo >= hi {
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := lo; i < hi && ctx.Err() == nil; i++ {
				paths[i] = Fit(&outlines[i], s)
			}
		}()
	}
	wg.Wait()
	return paths
}

// WriteSVG writes the traced picture as outputWidth × outputHeight pixels (the
// source image's size; traced pixels are scaled up when the picture was reduced).
func WriteSVG(r *Result, w io.Writer, outputWidth, outputHeight int, comment string) error {
	sx := float64(outputWidth) / float64(r.Width)
	sy := float64(outputHeight) / float64(r.Height)
	bw := bufio.NewWriterSize(w, 1<<16)
	bw.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	fmt.Fprintf(bw, "<svg xmlns=\"http://www.w3.org/2000/svg\" version=\"1.1\" width=\"%d\" height=\"%d\" viewBox=\"0 0 %d %d\">\n", outputWidth, outputHeight, outputWidth, outputHeight)
	if comment != "" {
		fmt.Fprintf(bw, "<!-- %s -->\n", strings.ReplaceAll(comment, "--", "- -"))
	}
	var data strings.Builder
	for _, l := range r.Layers {
		data.Reset()
		for i := range l.Paths {
			AppendPath(&data, &l.Paths[i], sx, sy)
		}
		fmt.Fprintf(bw, "<path fill=\"#%06x\" d=\"", l.Color)
		bw.WriteString(data.String())
		bw.WriteString("\"/>\n")
	}
	bw.WriteString("</svg>\n")
	return bw.Flush()
}

// AppendPath appends a closed path's data.
func AppendPath(b *strings.Builder, p *Path, sx, sy float64) {
	b.WriteByte('M')
	point(b, p.Start, sx, sy)
	for _, s := range p.Segments {
		if s.IsLine {
			b.WriteByte('L')
		} else {
			b.WriteByte('C')
			point(b, s.Control1, sx, sy)
			b.WriteByte(' ')
			point(b, s.Control2, sx, sy)
			b.WriteByte(' ')
		}
		point(b, s.End, sx, sy)
	}
	b.WriteByte('Z')
}

func point(b *strings.Builder, v Vec, sx, sy float64) {
	number(b, v.X*sx)
	b.WriteByte(' ')
	number(b, v.Y*sy)
}

func number(b *strings.Builder, v float64) {
	r := math.Round(v*100) / 100
	if r == 0 {
		r = 0
	}
	b.WriteString(strconv.FormatFloat(r, 'f', -1, 64))
}
