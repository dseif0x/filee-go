// Package icns makes macOS icons (.icns) from any raster picture and reads
// them back, without macOS. The container is Filee's own (file.go); the image
// engine (package images) reads, resizes and encodes the PNG and JPEG 2000
// entries.
package icns

import (
	"bytes"
	"context"
	"errors"
	"image"
	"os"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/images"
)

// Converter is raster image → ICNS and ICNS → raster image.
type Converter struct{}

func New() *Converter { return &Converter{} }

func (*Converter) ID() string                      { return "icns" }
func (*Converter) Name() string                    { return "ICNS (built-in)" }
func (*Converter) MaxParallelism() int             { return 0 }
func (*Converter) Transforms(from, to string) bool { return true }
func (*Converter) Status() convert.Status          { return convert.Available("", "built-in") }

func (*Converter) Edges() []convert.Edge {
	var edges []convert.Edge
	for _, s := range images.Sources() {
		edges = append(edges, convert.E(s, "icns"))
	}
	for _, t := range images.Writable {
		edges = append(edges, images.Edge("icns", t))
	}
	return edges
}

func (*Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	if step.From == "icns" {
		return readIcon(ctx, step, progress)
	}
	return writeIcon(ctx, step, progress)
}

// writeIcon makes an icon with every size iconutil produces (16 to 1024 px,
// PNG). The picture is centred on a transparent square first, so nothing is
// stretched or cut off.
func writeIcon(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	pic, err := images.Decode(step.InputPath, step.From, step.Preset)
	if err != nil {
		return nil, err
	}
	// An icon holds one picture: the first page/frame, or the largest icon of an .ico.
	master := images.SelectFrames(pic, step.From, "icns")[0]
	images.ApplyOptions(master, &step.Preset.Image, "icns")
	square := makeSquare(master.Img)
	progress.Report(0.2)
	encoded := map[int][]byte{}
	var entries []Entry
	for i, wt := range WrittenTypes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		png, ok := encoded[wt.Size]
		if !ok {
			// EXIF/XMP would be repeated in all ten entries: none.
			png = images.EncodePNG32(images.Resize(square, wt.Size, wt.Size))
			encoded[wt.Size] = png
		}
		entries = append(entries, Entry{wt.Type, png})
		progress.Report(0.2 + 0.7*float64(i+1)/float64(len(WrittenTypes)))
	}
	path := step.Output.Allocate("icns", "")
	if path == "" {
		return nil, nil
	}
	if err := os.WriteFile(path, Write(entries), 0o644); err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{path}, nil
}

// makeSquare pads the shorter side with transparency so the picture becomes a centred square.
func makeSquare(img *image.NRGBA) *image.NRGBA {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	if w == h {
		return img
	}
	side := max(w, h)
	out := image.NewNRGBA(image.Rect(0, 0, side, side))
	x, y := (side-w)/2, (side-h)/2
	for row := 0; row < h; row++ {
		copy(out.Pix[(y+row)*out.Stride+x*4:], img.Pix[row*img.Stride:row*img.Stride+w*4])
	}
	return out
}

// readIcon converts the largest picture of an .icns file to step.To.
func readIcon(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	data, err := os.ReadFile(step.InputPath)
	if err != nil {
		return nil, err
	}
	entries, err := Parse(data)
	if err != nil {
		return nil, err
	}
	candidates := Candidates(entries)
	if len(candidates) == 0 {
		return nil, errors.New("the ICNS file contains no image Filee can read (only 1-bit or 8-bit palette icons)")
	}
	progress.Report(0.2)
	f, err := decodeLargest(ctx, candidates)
	if err != nil {
		return nil, err
	}
	images.ApplyOptions(f, &step.Preset.Image, step.To)
	progress.Report(0.7)
	written, err := images.Write([]*images.Frame{f}, step.To, step.Output, &step.Preset.Image, step.InputPath, false, 0)
	if err != nil {
		return written, err
	}
	progress.Report(1)
	return written, nil
}

// decodeLargest decodes the largest picture; a broken entry falls back to the next smaller one.
func decodeLargest(ctx context.Context, candidates []Candidate) (*images.Frame, error) {
	var first error
	for _, c := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if c.Encoded != nil {
			format := "png"
			if !bytes.HasPrefix(c.Encoded, pngSignature) {
				format = "jp2"
			}
			pic, err := images.DecodeBytes(c.Encoded, format)
			if err == nil && len(pic.Frames) > 0 {
				f := pic.Frames[0]
				f.Alpha = true
				return f, nil
			}
			if first == nil {
				first = err
			}
			continue
		}
		bm, err := c.Decode()
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		img := &image.NRGBA{Pix: bm.RGBA, Stride: bm.Width * 4, Rect: image.Rect(0, 0, bm.Width, bm.Height)}
		return &images.Frame{Img: img, Alpha: true}, nil
	}
	if first == nil {
		first = errors.New("no picture")
	}
	return nil, errors.Join(errors.New("no image in the ICNS file could be decoded"), first)
}

var (
	_ convert.Converter        = (*Converter)(nil)
	_ convert.ImageTransformer = (*Converter)(nil)
)
