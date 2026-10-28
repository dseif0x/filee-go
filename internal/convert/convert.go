// Package convert is the contract every conversion engine implements, plus
// the catalog of engines and the route planner over their edges.
//
// Adding an engine means one type implementing Converter, listed in
// internal/engines/all.
package convert

import (
	"context"
	"fmt"

	"github.com/dseif0x/filee-go/internal/presets"
)

// Edge is one direct conversion an engine offers.
type Edge struct {
	From, To string
	// Cost weighs the edge in route planning (default 10 when 0; Free for a
	// real cost of 0).
	Cost int
	// ImageSourcesOnly edges are used only when the file itself is a raster
	// image (tracing); NotForImageSources the reverse.
	ImageSourcesOnly   bool
	NotForImageSources bool
	// DirectOnly edges are used only as the whole route, never as a hop.
	DirectOnly bool
	// FrameSequence, MotionPhoto, LivePhoto and Ocr edges are special routes
	// a preset asks for explicitly; the planner never uses them otherwise.
	FrameSequence bool
	MotionPhoto   bool
	LivePhoto     bool
	Ocr           bool
	// Unshaped edges cannot lay out complex scripts (Arabic, Indic, …) and are
	// left out when the text needs shaping.
	Unshaped bool
}

// E is a plain edge with the default cost.
func E(from, to string) Edge { return Edge{From: from, To: to, Cost: 10} }

// EC is a plain edge with a cost.
func EC(from, to string, cost int) Edge { return Edge{From: from, To: to, Cost: cost} }

// Fan returns plain edges from every source to every target (same cost).
func Fan(from, to []string, cost int) []Edge {
	out := make([]Edge, 0, len(from)*len(to))
	for _, f := range from {
		for _, t := range to {
			if f != t {
				out = append(out, Edge{From: f, To: t, Cost: cost})
			}
		}
	}
	return out
}

// Free is the Cost of an edge that costs nothing (0 means the default 10).
const Free = -1

func (e Edge) cost() int {
	switch {
	case e.Cost == 0:
		return 10
	case e.Cost < 0:
		return 0
	}
	return e.Cost
}

// EffectiveCost is the cost the planner uses for the edge.
func (e Edge) EffectiveCost() int { return e.cost() }

// Status tells whether an engine can run here.
type Status struct {
	Available bool
	// ReasonKey is a localization key saying why not ("engine.reason.not_installed").
	ReasonKey string
	Detail    string
	Version   string
}

func Available(detail, version string) Status {
	return Status{Available: true, Detail: detail, Version: version}
}
func Unavailable(reasonKey, detail string) Status {
	return Status{ReasonKey: reasonKey, Detail: detail}
}

// OutputAllocator hands out output paths. Allocate returns "" when the file
// is to be skipped (it exists and the preset says skip). suffix is appended
// to the file name ("-page1").
type OutputAllocator interface {
	Allocate(ext, suffix string) string
}

// AllocatorFunc adapts a function.
type AllocatorFunc func(ext, suffix string) string

func (f AllocatorFunc) Allocate(ext, suffix string) string { return f(ext, suffix) }

// Step is one conversion of a route.
type Step struct {
	InputPath string
	From, To  string
	Preset    *presets.Preset
	Output    OutputAllocator
	// WorkDir is a private scratch directory, removed after the job.
	WorkDir       string
	FrameSequence bool
	MotionPhoto   bool
	LivePhoto     bool
	// Notice reports a localization key the UI shows with the result.
	Notice func(key string)
	// MissingFonts reports fonts a document asked for and did not get.
	MissingFonts func(names []string)
	// OriginalSourcePath is the dropped file when InputPath is an intermediate.
	OriginalSourcePath string
}

// Progress receives 0–1; nil is allowed.
type Progress func(fraction float64)

// Report calls p when set.
func (p Progress) Report(f float64) {
	if p != nil {
		p(f)
	}
}

// Converter is one engine.
type Converter interface {
	ID() string
	Name() string
	Edges() []Edge
	// MaxParallelism is how many files it converts at once (0 = CPU count).
	MaxParallelism() int
	Status() Status
	// Convert writes the outputs (allocated through step.Output) and returns them.
	Convert(ctx context.Context, step *Step, progress Progress) ([]string, error)
}

// RefusedError is a conversion the engine refuses for a reason the user can
// understand (a localization key), not a crash.
type RefusedError struct {
	Key    string
	Detail string
}

func (e *RefusedError) Error() string {
	if e.Detail == "" {
		return e.Key
	}
	return fmt.Sprintf("%s (%s)", e.Key, e.Detail)
}

// Refuse returns a *RefusedError.
func Refuse(key, detail string) error { return &RefusedError{Key: key, Detail: detail} }

// ---- optional interfaces ----

// ImageTransformer engines apply the picture edits (resize, rotate, …) on the
// given edge; the job applies the edits only on the first such step.
type ImageTransformer interface {
	Transforms(from, to string) bool
}

// TextScripts names scripts found in a document.
type TextScripts struct {
	// NeedsShaping is a script that needs complex shaping, "" if none.
	NeedsShaping string
	// WithoutWidths is a script the fonts have no widths for, "" if none.
	WithoutWidths string
}

// TextShapingCheck engines can scan a document for complex scripts.
type TextShapingCheck interface {
	ScanScripts(sourcePath, sourceFormat string) TextScripts
}

type PdfMerger interface {
	MergePdfs(ctx context.Context, inputs []string, output string) error
}

type PdfFinisher interface {
	FinishPdf(ctx context.Context, path string, options *presets.PdfOptions) error
}

type TiffMerger interface {
	MergeTiffs(ctx context.Context, inputs []string, output string, options *presets.ImageOptions) error
}

type MediaJoiner interface {
	JoinMedia(ctx context.Context, inputs []string, output, format string, preset *presets.Preset, workDir string, progress Progress) error
}

// ImageInput is one picture to join.
type ImageInput struct{ Path, Format string }

type ImageJoiner interface {
	CanRead(format string) bool
	CanWrite(format string) bool
	JoinImages(ctx context.Context, inputs []ImageInput, output, format string, options *presets.ImageOptions, progress Progress) error
}

// FileCombiner engines combine several files into one of format (e.g. sheets into one workbook).
type FileCombiner interface {
	CanCombineInto(format string) bool
	Combine(ctx context.Context, inputs []string, output, format string, preset *presets.Preset, workDir string, progress Progress) error
}
