package convert

import (
	"container/heap"
	"sort"
	"strings"

	"github.com/dseif0x/filee-go/internal/formats"
)

// MaxSteps is the longest route the planner builds.
const MaxSteps = 3

// RouteStep is one hop of a route.
type RouteStep struct {
	Converter     Converter
	From, To      string
	FrameSequence bool
	Unshaped      bool
	MotionPhoto   bool
	LivePhoto     bool
}

// Route is the cheapest chain of converters from a source to a target format.
//
// Engines only know direct conversions (edges); many useful conversions need
// two hops, e.g. HWPX → PDF → PNG. Formats are nodes and edges weighted arcs,
// so a new engine unlocks new routes without touching other code.
type Route struct {
	Steps []RouteStep
	Cost  int
}

// FirstTransformStep is the index of the first step that applies picture edits, or -1.
func (r *Route) FirstTransformStep() int {
	for i, s := range r.Steps {
		if t, ok := s.Converter.(ImageTransformer); ok && t.Transforms(s.From, s.To) {
			return i
		}
	}
	return -1
}

type planEdge struct {
	conv Converter
	edge Edge
	cost int
}

// Planner plans routes over a fixed set of converters.
type Planner struct {
	from map[string][]planEdge
}

// NewPlanner builds a planner; penalty (may be nil) adds to the cost of every
// edge of a converter, so the user's engine order breaks ties.
func NewPlanner(converters []Converter, penalty func(Converter) int) *Planner {
	p := &Planner{from: map[string][]planEdge{}}
	for _, c := range converters {
		pen := 0
		if penalty != nil {
			pen = penalty(c)
		}
		for _, e := range c.Edges() {
			e.From, e.To = strings.ToLower(e.From), strings.ToLower(e.To)
			p.from[e.From] = append(p.from[e.From], planEdge{c, e, e.cost() + pen})
		}
	}
	// Stable order: cheapest first, then registration order.
	for k := range p.from {
		sort.SliceStable(p.from[k], func(i, j int) bool { return p.from[k][i].cost < p.from[k][j].cost })
	}
	return p
}

// Options asks for a special route.
type Options struct {
	Frames      bool
	Shaping     bool
	MotionPhoto bool
	LivePhoto   bool
	Ocr         bool
}

// Plan returns the cheapest route, or nil.
func (p *Planner) Plan(from, to string, o Options) *Route {
	from, to = strings.ToLower(from), strings.ToLower(to)
	if o.Ocr {
		return p.planWithOcr(from, to, o.Shaping)
	}
	if o.Frames || o.MotionPhoto || o.LivePhoto {
		// One of them: frames, then the video of a photo, then a photo of a video.
		motion := o.MotionPhoto && !o.Frames
		live := o.LivePhoto && !o.Frames && !motion
		for _, e := range p.from[from] {
			if e.edge.To == to && (o.Frames && e.edge.FrameSequence || motion && e.edge.MotionPhoto || live && e.edge.LivePhoto) {
				return &Route{Steps: []RouteStep{{Converter: e.conv, From: from, To: to, FrameSequence: o.Frames, MotionPhoto: motion, LivePhoto: live}}, Cost: e.cost}
			}
		}
		return nil
	}
	// Same-format "conversions" (re-encode, split, compress) need an explicit self edge.
	if from == to {
		for _, e := range p.from[from] {
			if e.edge.To == to && !e.edge.FrameSequence && !e.edge.MotionPhoto && !e.edge.LivePhoto && !e.edge.Ocr {
				return &Route{Steps: []RouteStep{{Converter: e.conv, From: from, To: to}}, Cost: e.cost}
			}
		}
		return nil
	}

	imageSource := formats.CategoryOf(from) == formats.Image
	dist := map[string]int{from: 0}
	hops := map[string]int{from: 0}
	type back struct {
		node string
		step RouteStep
	}
	prev := map[string]back{}
	q := &queue{}
	heap.Push(q, item{from, 0})
	for q.Len() > 0 {
		it := heap.Pop(q).(item)
		if it.cost > dist[it.node] {
			continue // stale entry
		}
		if it.node == to {
			break
		}
		if hops[it.node] >= MaxSteps {
			continue
		}
		for _, e := range p.from[it.node] {
			ed := e.edge
			if ed.To == ed.From || ed.FrameSequence || ed.MotionPhoto || ed.LivePhoto || ed.Ocr ||
				(ed.ImageSourcesOnly && !imageSource) || (ed.NotForImageSources && imageSource) ||
				(ed.Unshaped && o.Shaping) || (ed.DirectOnly && (it.node != from || ed.To != to)) {
				continue
			}
			next := it.cost + e.cost
			if known, ok := dist[ed.To]; ok && known <= next {
				continue
			}
			dist[ed.To] = next
			hops[ed.To] = hops[it.node] + 1
			prev[ed.To] = back{it.node, RouteStep{Converter: e.conv, From: ed.From, To: ed.To, Unshaped: ed.Unshaped}}
			heap.Push(q, item{ed.To, next})
		}
	}
	if _, ok := prev[to]; !ok {
		return nil
	}
	var steps []RouteStep
	for n := to; n != from; n = prev[n].node {
		steps = append(steps, prev[n].step)
	}
	for i, j := 0, len(steps)-1; i < j; i, j = i+1, j-1 {
		steps[i], steps[j] = steps[j], steps[i]
	}
	return &Route{Steps: steps, Cost: dist[to]}
}

// CanConvert reports whether any route exists.
func (p *Planner) CanConvert(from, to string, o Options) bool { return p.Plan(from, to, o) != nil }

// Targets lists every format reachable from from (plain routes), sorted.
func (p *Planner) Targets(from string) []string {
	var out []string
	for _, f := range formats.Known {
		if f.Writable() && f.ID != from && p.Plan(from, f.ID, Options{}) != nil {
			out = append(out, f.ID)
		}
	}
	return out
}

func (p *Planner) planWithOcr(from, to string, shaping bool) *Route {
	r := &Route{}
	source := from
	if p.ocrEdge(from) == nil {
		// A picture the OCR engine can't open (HEIC, PSD, …) becomes a PNG first.
		if formats.CategoryOf(from) != formats.Image {
			return nil
		}
		toPng := p.Plan(from, "png", Options{Shaping: shaping})
		if toPng == nil {
			return nil
		}
		r.Steps = append(r.Steps, toPng.Steps...)
		r.Cost += toPng.Cost
		source = "png"
	}
	ocr := p.ocrEdge(source)
	if ocr == nil {
		return nil
	}
	r.Steps = append(r.Steps, RouteStep{Converter: ocr.conv, From: source, To: "pdf"})
	r.Cost += ocr.cost
	if to != "pdf" {
		rest := p.Plan("pdf", to, Options{Shaping: shaping})
		if rest == nil {
			return nil
		}
		r.Steps = append(r.Steps, rest.Steps...)
		r.Cost += rest.Cost
	}
	return r
}

func (p *Planner) ocrEdge(from string) *planEdge {
	for i, e := range p.from[from] {
		if e.edge.Ocr && e.edge.To == "pdf" {
			return &p.from[from][i]
		}
	}
	return nil
}

type item struct {
	node string
	cost int
}

type queue []item

func (q queue) Len() int           { return len(q) }
func (q queue) Less(i, j int) bool { return q[i].cost < q[j].cost }
func (q queue) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *queue) Push(x any)        { *q = append(*q, x.(item)) }
func (q *queue) Pop() any          { o := *q; it := o[len(o)-1]; *q = o[:len(o)-1]; return it }

// Steps is how many steps the cheapest plain route takes (0 = none); it makes
// the planner a presets.Router for the tools.
func (p *Planner) Steps(from, to string) int {
	if r := p.Plan(from, to, Options{}); r != nil {
		return len(r.Steps)
	}
	return 0
}
