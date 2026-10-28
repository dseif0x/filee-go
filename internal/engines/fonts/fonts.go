// Package fonts converts fonts between TTF, OTF, WOFF, WOFF2 and EOT entirely
// in Go (zlib from the standard library, Brotli from andybalholm/brotli). CFF
// (PostScript) outlines are converted to TrueType when the target needs them
// (TTF, EOT). Windows bitmap fonts (FON) are read and traced into outlines.
package fonts

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"

	"github.com/dseif0x/filee-go/internal/convert"
)

// Formats are the format ids this engine reads and writes.
var Formats = []string{"ttf", "otf", "woff", "woff2", "eot"}

// Converter is the font engine.
type Converter struct{}

func New() *Converter { return &Converter{} }

func (*Converter) ID() string          { return "font" }
func (*Converter) Name() string        { return "Fonts (built-in)" }
func (*Converter) MaxParallelism() int { return 0 }

// Edges are every format to every other one, directly, plus read-only Windows
// bitmap FON to each of them.
func (*Converter) Edges() []convert.Edge {
	e := convert.Fan(Formats, Formats, 10)
	for _, to := range Formats {
		e = append(e, convert.E("fon", to))
	}
	return e
}

func (*Converter) Status() convert.Status {
	return convert.Available("TTF, OTF, WOFF, WOFF2, EOT; FON (Windows bitmap fonts) read", "built-in")
}

func (*Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) (outs []string, err error) {
	defer recoverData(&err)
	data, err := os.ReadFile(step.InputPath)
	if err != nil {
		return nil, err
	}
	if step.From == "fon" {
		return convertFon(ctx, step, data, progress), nil
	}
	font := loadFont(ctx, data)
	progress.Report(0.2)
	out := saveFont(ctx, font, step.To, func(f float64) { progress.Report(0.2 + 0.6*f) })
	progress.Report(0.9)
	path := step.Output.Allocate(step.To, "")
	if path == "" {
		return nil, nil
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{path}, nil
}

// convertFon writes one output per RT_FONT resource. Every resource is parsed
// and every output is built in memory before the first path is allocated, so a
// failure or cancellation before writing leaves no new file and no existing
// file changed.
func convertFon(ctx context.Context, step *convert.Step, data []byte, progress convert.Progress) []string {
	strikes := readFon(ctx, data)
	checkCtx(ctx)
	progress.Report(0.1)

	// Same-height, same-style ordinals and the resource count are fixed now, so skipped outputs never rename the others.
	type styleKey struct {
		height, weight int
		italic         bool
	}
	ordinals := make([]int, len(strikes))
	counts := map[styleKey]int{}
	for i, s := range strikes {
		checkCtx(ctx)
		k := styleKey{s.pixelHeight, s.weight, s.italic}
		counts[k]++
		ordinals[i] = counts[k]
	}
	results := make([][]byte, len(strikes))
	for i := range strikes {
		font := buildFon(ctx, &strikes[i], len(strikes), ordinals[i])
		results[i] = saveFont(ctx, font, step.To, nil)
		if i == 0 {
			progress.Report(0.85)
		} else {
			progress.Report(0.85 + 0.05*float64(i)/float64(len(strikes)))
		}
		checkCtx(ctx)
	}
	checkCtx(ctx)
	var outputs []string
	for i, s := range strikes {
		var style string
		switch {
		case s.weight == 400 && !s.italic:
		case s.weight == 700 && !s.italic:
			style = "-Bold"
		case s.weight == 400 && s.italic:
			style = "-Italic"
		case s.weight == 700 && s.italic:
			style = "-BoldItalic"
		case !s.italic:
			style = fmt.Sprintf("-Weight%d", s.weight)
		default:
			style = fmt.Sprintf("-Weight%dItalic", s.weight)
		}
		suffix := ""
		if len(strikes) > 1 {
			suffix = fmt.Sprintf("-%dpx%s", s.pixelHeight, style)
			if ordinals[i] > 1 {
				suffix += fmt.Sprintf("-%d", ordinals[i])
			}
		}
		path := step.Output.Allocate(step.To, suffix)
		if path == "" {
			continue
		}
		if err := os.WriteFile(path, results[i], 0o644); err != nil {
			panic(dataError(err.Error()))
		}
		outputs = append(outputs, path)
	}
	progress.Report(1)
	return outputs
}

// loadFont decodes a font file, whatever its container (recognised by content, not extension).
func loadFont(ctx context.Context, data []byte) *sfnt {
	if len(data) >= 4 {
		switch binary.BigEndian.Uint32(data) {
		case woffSignature:
			return decodeWoff(data)
		case woff2Signature:
			return decodeWoff2(ctx, data)
		}
	}
	if isSfnt(data) {
		return readSfnt(data, 0)
	}
	if isEot(data) {
		return decodeEot(data)
	}
	fail("This file is not a font Filee can read (TTF, OTF, WOFF, WOFF2 or EOT).")
	return nil
}

// saveFont encodes the font as format. TTF and EOT need TrueType outlines, so
// CFF outlines are converted first; OTF keeps whatever outlines the font has
// (TrueType-flavoured OpenType is valid OpenType).
func saveFont(ctx context.Context, font *sfnt, format string, progress func(float64)) []byte {
	switch format {
	case "ttf":
		return trueTypeOutlines(ctx, font, progress).write()
	case "otf":
		return font.write()
	case "woff":
		return encodeWoff(ctx, font)
	case "woff2":
		return encodeWoff2(ctx, font, progress)
	case "eot":
		// Internet Explorer, the only EOT consumer, renders EOT fonts with TrueType outlines only.
		return encodeEot(trueTypeOutlines(ctx, font, progress))
	}
	fail(fmt.Sprintf("Unknown font format '%s'.", format))
	return nil
}

// trueTypeOutlines is the font itself when it has TrueType outlines, else a converted copy.
func trueTypeOutlines(ctx context.Context, font *sfnt, progress func(float64)) *sfnt {
	if !font.has("glyf") && (font.has("CFF ") || font.has("CFF2")) {
		return cffToTrueType(ctx, font, progress)
	}
	return font
}

// Load decodes font data of any supported container.
func Load(data []byte) (f *Font, err error) {
	defer recoverData(&err)
	return &Font{loadFont(context.Background(), data)}, nil
}

// Font is a decoded font, for other engines that embed fonts.
type Font struct{ f *sfnt }

// Save encodes the font as format ("ttf", "otf", "woff", "woff2" or "eot").
func (f *Font) Save(ctx context.Context, format string) (out []byte, err error) {
	defer recoverData(&err)
	if f == nil || f.f == nil {
		return nil, errors.New("no font")
	}
	return saveFont(ctx, f.f, format, nil), nil
}
