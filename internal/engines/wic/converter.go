// Package wic reads JPEG XR (.jxr, .wdp, .hdp — HDR screenshots of Windows
// among them) with the Windows Imaging Component, part of Windows. There is
// no JPEG XR decoder in pure Go (nor in ImageMagick), so this engine is
// Windows only: WIC is called through COM with golang.org/x/sys/windows, no
// cgo. The picture becomes an ordinary 8-bit frame; the image engine does the
// rest (preset options, every format it writes).
package wic

import (
	"context"
	"runtime"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/images"
)

// Converter is JPEG XR → images.
type Converter struct{}

func New() *Converter { return &Converter{} }

func (*Converter) ID() string                      { return "wic" }
func (*Converter) Name() string                    { return "Windows Imaging Component" }
func (*Converter) MaxParallelism() int             { return 0 }
func (*Converter) Transforms(from, to string) bool { return true }

func (*Converter) Edges() []convert.Edge {
	var edges []convert.Edge
	for _, t := range images.Writable {
		edges = append(edges, images.Edge("jxr", t))
	}
	return edges
}

func (*Converter) Status() convert.Status {
	if runtime.GOOS != "windows" {
		return convert.Unavailable("engine.reason.unsupported_platform", "")
	}
	if err := codecAvailable(); err != nil {
		return convert.Unavailable("engine.reason.not_installed", "JPEG XR codec: "+err.Error())
	}
	return convert.Available("JPEG XR (JXR, WDP, HDP) → images", windowsVersion())
}

func (*Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	f, err := read(step.InputPath)
	if err != nil {
		return nil, err
	}
	return images.WriteDecoded(ctx, &images.Picture{Frames: []*images.Frame{f}}, step.From, step, progress)
}

var (
	_ convert.Converter        = (*Converter)(nil)
	_ convert.ImageTransformer = (*Converter)(nil)
)
