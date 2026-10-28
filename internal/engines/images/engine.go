package images

import (
	"context"
	"os"
	"runtime/debug"
	"strings"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/presets"
)

// Converter is the raster image engine. Its id stays "magick" (the C#
// original's ImageMagick engine), so saved engine priorities still match.
type Converter struct{}

// New returns the engine.
func New() *Converter { return &Converter{} }

func (*Converter) ID() string          { return "magick" }
func (*Converter) Name() string        { return "Images (built-in)" }
func (*Converter) MaxParallelism() int { return 0 }

// Transforms: the picture edits apply on every edge.
func (*Converter) Transforms(from, to string) bool { return true }

func (*Converter) Edges() []convert.Edge {
	var edges []convert.Edge
	for _, s := range Sources() {
		for _, t := range Writable {
			edges = append(edges, Edge(s, t))
		}
	}
	return edges
}

func (*Converter) Status() convert.Status {
	return convert.Available("Pure-Go codecs (WebP, AVIF, HEIC, JPEG XL as WebAssembly)", moduleVersion("github.com/gen2brain/webp"))
}

// moduleVersion is the version of a dependency built into the program ("" when unknown).
func moduleVersion(path string) string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, d := range info.Deps {
			if d.Path == path {
				return strings.TrimPrefix(d.Version, "v")
			}
		}
	}
	return ""
}

func (*Converter) CanRead(format string) bool  { return CanRead(format) }
func (*Converter) CanWrite(format string) bool { return CanWrite(format) }

func (c *Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	// FITS → TIFF keeps the file's depth (16 or 32 bits): written by its own writer.
	if step.From == "fits" && step.To == "tiff" {
		return writeFitsTiff(ctx, step, progress)
	}
	from := step.From
	// Animated PNGs are often saved as .png: read them as APNG, so they stay animated.
	if from == "png" && isAnimatedPNGFile(step.InputPath) {
		from = "apng"
	}
	pic, err := Decode(step.InputPath, from, step.Preset)
	if err != nil {
		return nil, err
	}
	return WriteDecoded(ctx, pic, from, step, progress)
}

// WriteDecoded is the rest of a conversion once the picture is read (also for
// pictures another engine decoded): the frames kept for the target, the
// preset's options, the files. from is the format the frames were read from
// (animations and icons keep different frames).
func WriteDecoded(ctx context.Context, pic *Picture, from string, step *convert.Step, progress convert.Progress) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	progress.Report(0.3)
	o := &step.Preset.Image
	// A JPG has no transparent border to trim: re-encoding it would only cost a generation.
	if o.TrimTransparent && !HoldsAlpha(step.To) {
		return nil, convert.Refuse("error.no_transparency", strings.ToUpper(step.To))
	}
	animation := KeepsAnimation(from, step.To)
	frames := SelectFrames(pic, from, step.To)
	if animation && o.ChangesSpeed() {
		frames = ChangeSpeed(frames, o.PlaybackSpeed(), step.To)
	}
	for _, f := range frames {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ApplyOptions(f, o, step.To)
		// Animations aren't trimmed: each frame trimmed alone would shift.
		if o.TrimTransparent && !(animation && len(frames) > 1) {
			TrimTransparentBorders(f)
		}
	}
	progress.Report(0.7)
	written, err := Write(frames, step.To, step.Output, o, step.InputPath, animation, pic.Loops)
	if err != nil {
		return written, err
	}
	progress.Report(1)
	return written, nil
}

// MergeTiffs writes the pages into one multi-page TIFF. Each page is flattened
// onto the preset's background at 8 bits and marked as a page of a document
// (SubfileType 2), so fax software and old viewers show it page by page.
func (*Converter) MergeTiffs(ctx context.Context, inputs []string, output string, o *presets.ImageOptions) error {
	bg := backgroundColor(o.Background)
	var pages []*Frame
	for _, path := range inputs {
		if err := ctx.Err(); err != nil {
			return err
		}
		pic, err := Decode(path, "", nil)
		if err != nil {
			return err
		}
		for _, f := range pic.Frames {
			f.Img = flatten(f.Img, bg)
			f.Alpha = false
			f.enc.tiff = string(o.TiffCompression)
			f.enc.subfile2 = true
			pages = append(pages, f)
		}
	}
	data, err := encodeTIFF(pages)
	if err != nil {
		return err
	}
	return os.WriteFile(output, data, 0o644)
}

// JoinImages puts the pictures one under another or side by side in one image.
func (*Converter) JoinImages(ctx context.Context, inputs []convert.ImageInput, output, format string, o *presets.ImageOptions, progress convert.Progress) error {
	return joinImages(ctx, inputs, output, format, o, progress)
}

var (
	_ convert.Converter        = (*Converter)(nil)
	_ convert.ImageTransformer = (*Converter)(nil)
	_ convert.ImageJoiner      = (*Converter)(nil)
	_ convert.TiffMerger       = (*Converter)(nil)
)
