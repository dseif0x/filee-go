// Package cad is the built-in CAD engine: DWG ↔ DXF (any version), and DWG /
// DXF → PDF, SVG and images. Drawings are read and written with a Go port of
// ACadSharp (packages cadoc, cadoc/dxf, cadoc/dwg) and rendered with the gfx
// drawing layer.
package cad

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/vector/imgout"
	"github.com/dseif0x/filee-go/internal/formats"
)

var sources = []string{"dwg", "dxf"}

// Converter is the CAD engine. Rendering draws model space from the top (the
// active layout when model space is empty), fitted to the page: PDF on an A3
// sheet (A4 / Letter when the preset says so), SVG and images with the
// drawing's aspect ratio on white. A PDF of a drawing with paper-space
// layouts has one page per layout, with model space through each layout's
// viewports. Every rendered output takes the preset's colours.
type Converter struct{}

func New() *Converter { return &Converter{} }

func (*Converter) ID() string   { return "cad" }
func (*Converter) Name() string { return "CAD (built-in)" }

// MaxParallelism: a loaded drawing takes several times its file size in
// memory, so only a few are converted at once.
func (*Converter) MaxParallelism() int { return max(1, min(4, runtime.NumCPU()/2)) }

// Transforms: only when rendering to an image (not PDF, SVG or between DWG and DXF).
func (*Converter) Transforms(from, to string) bool { return slices.Contains(imgout.Writable, to) }

func (*Converter) Edges() []convert.Edge {
	e := []convert.Edge{
		convert.E("dwg", "dxf"),
		convert.E("dxf", "dwg"),
		// Into another version of the same format (#489).
		convert.E("dwg", "dwg"),
		convert.E("dxf", "dxf"),
	}
	targets := append([]string{"pdf", "svg"}, imgout.Writable...)
	for _, s := range sources {
		for _, t := range targets {
			e = append(e, imgout.Edge(s, t))
		}
	}
	return e
}

func (*Converter) Status() convert.Status {
	return convert.Available("DWG R13–2018+, DXF", "built-in")
}

func (c *Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	progress.Report(0.05)
	log := func(msg string) { slog.Debug("cad", "file", step.InputPath, "msg", msg) }
	doc, err := ReadDocument(step.InputPath, step.From, log)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	progress.Report(0.4)
	if step.To == "dxf" || step.To == "dwg" {
		path := step.Output.Allocate(step.To, "")
		if path == "" {
			return nil, nil
		}
		leftOut, err := WriteDocument(doc, path, step.To, step.Preset.Cad.Version, log)
		if err != nil {
			return nil, err
		}
		if leftOut > 0 && step.Notice != nil {
			step.Notice("notice.cad_newer_left_out")
		}
		progress.Report(1)
		return []string{path}, nil
	}
	scene, err := BuildScene(ctx, doc)
	if err != nil {
		return nil, err
	}
	report(step.InputPath, scene)
	// A PDF follows the layouts like a plot of every sheet; images and SVG stay one picture of model space.
	var sheets []*LayoutSheet
	if step.To == "pdf" {
		if sheets, err = BuildLayouts(ctx, doc); err != nil {
			return nil, err
		}
	}
	if len(scene.Items) == 0 && len(sheets) == 0 {
		if len(scene.Skipped) > 0 {
			keys := make([]string, 0, len(scene.Skipped))
			for k := range scene.Skipped {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			return nil, fmt.Errorf("the drawing only contains objects Filee cannot draw (%s)", strings.Join(keys, ", "))
		}
		return nil, errors.New("the drawing has nothing visible to draw: model space and the layout are empty, or every entity is on a layer that is off, frozen or not plotted")
	}
	progress.Report(0.7)
	palette := Palette{Colors: step.Preset.Cad.Colors}
	title := formats.NameWithoutExtension(filepath.Base(step.InputPath))
	path := step.Output.Allocate(step.To, "")
	if path == "" {
		return nil, nil
	}
	var b bytes.Buffer
	switch step.To {
	case "pdf":
		if len(sheets) > 0 {
			// With empty model space the scene is the active layout's paper space,
			// which must not show through the viewports.
			model := scene
			if scene.Space != "model" {
				model = NewScene()
			}
			err = WritePdfSheets(sheets, model, &step.Preset.Pdf, palette, title, &b)
		} else {
			err = WritePdf(scene, &step.Preset.Pdf, palette, title, &b)
		}
	case "svg":
		err = WriteSvg(scene, palette, &b)
	default:
		// Rendered at the preset's render resolution (150 dpi by default: an A3
		// page is about 2500 px on the long edge), then the image options apply.
		dpi := float64(max(36, min(1200, step.Preset.Pdf.RenderDpi)))
		r, used := RenderBitmap(scene, dpi, palette)
		out, err := imgout.Write(r.Straight(), math.Round(used), step.To, singlePath(path), &step.Preset.Image, true)
		if err != nil {
			os.Remove(path)
			return nil, err
		}
		progress.Report(1)
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{path}, nil
}

// singlePath hands out the already allocated output path (the image writer
// may change the extension for Netpbm sub-formats).
func singlePath(path string) convert.OutputAllocator {
	return convert.AllocatorFunc(func(ext, suffix string) string {
		if ext == strings.TrimPrefix(filepath.Ext(path), ".") && suffix == "" {
			return path
		}
		return strings.TrimSuffix(path, filepath.Ext(path)) + suffix + "." + ext
	})
}

func report(input string, s *Scene) {
	if s.Space != "model" {
		slog.Info("cad: model space is empty, drew a layout", "file", input, "layout", s.Space)
	}
	if len(s.Skipped) > 0 {
		slog.Info("cad: skipped unsupported entities", "file", input, "types", s.SkippedSummary())
	}
	if s.Truncated {
		slog.Warn("cad: the drawing exceeds the rendering limits and was cut short", "file", input)
	}
}
