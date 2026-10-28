// Package subtitles converts text subtitles in managed code: SRT, ASS/SSA,
// WebVTT, SAMI, LRC, TTML, MicroDVD and SCC are read into one cue model and
// written as SRT, ASS, WebVTT, SAMI, LRC, TTML or MicroDVD, always as UTF-8.
// Korean CP949 / EUC-KR files (most SAMI files) are recognised. Subtitles
// inside videos are taken out by FFmpeg (video → SRT / ASS / WebVTT).
package subtitles

import (
	"context"
	"os"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/formats"
)

// Converter is the subtitle engine.
type Converter struct{}

func New() *Converter { return &Converter{} }

func (*Converter) ID() string          { return "subtitle" }
func (*Converter) Name() string        { return "Subtitles (built-in)" }
func (*Converter) MaxParallelism() int { return 0 }

// Edges are every subtitle format to every one Filee writes, also to itself:
// an SRT or SAMI file in CP949 comes out as UTF-8.
func (*Converter) Edges() []convert.Edge {
	var e []convert.Edge
	for _, from := range ReadFormats {
		for _, to := range WriteFormats {
			e = append(e, convert.E(from, to))
		}
	}
	return e
}

func (*Converter) Status() convert.Status {
	return convert.Available("SRT, ASS, WebVTT, SAMI, LRC, TTML, MicroDVD, SCC", "built-in")
}

func (*Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	b, err := os.ReadFile(step.InputPath)
	if err != nil {
		return nil, err
	}
	d, err := Read(step.From, b)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	progress.Report(0.5)
	data, err := Write(d, step.To)
	if err != nil {
		return nil, err
	}
	ext := step.To
	if f := formats.ByID(step.To); f != nil {
		ext = f.PrimaryExtension()
	}
	out := step.Output.Allocate(ext, "")
	if out == "" {
		return nil, nil
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{out}, nil
}
