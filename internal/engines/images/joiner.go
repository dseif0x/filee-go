package images

import (
	"context"
	"fmt"
	"image"
	"math"
	"os"
	"strings"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/presets"
)

// "Merge images": pictures one under another or side by side in one image,
// with a gap and a background colour between them. The layout is worked out
// from the pictures' sizes first, so only some pictures (and the merged image)
// are in memory at once.

// MaxJoinedPixels is the largest merged image (the size of the longest "Long image").
const MaxJoinedPixels = 200_000_000

// keptPixels: pictures read to measure them stay in memory up to this many pixels in all.
const keptPixels = 50_000_000

type placement struct{ X, Y, W, H int }

type joinLayout struct {
	W, H     int
	Pictures []placement
}

// planJoin lays out pictures of sizes as o says; refuses above MaxJoinedPixels.
func planJoin(sizes [][2]int, o *presets.ImageOptions) (*joinLayout, error) {
	sideBySide := o.JoinDirection == presets.JoinHorizontal
	gap := float64(o.JoinSpacing())
	type sz struct{ across, along float64 }
	fitted := make([]sz, len(sizes))
	across := 0.0
	for i, s := range sizes {
		if sideBySide {
			fitted[i] = sz{float64(s[1]), float64(s[0])}
		} else {
			fitted[i] = sz{float64(s[0]), float64(s[1])}
		}
		across = math.Max(across, fitted[i].across)
	}
	along := 0.0
	placed := make([]sz, len(fitted))
	for i, s := range fitted {
		if o.JoinFit == presets.JoinSameSize {
			placed[i] = sz{across, math.Max(1, math.Round(s.along*across/s.across))}
		} else {
			placed[i] = s
		}
		along += placed[i].along
	}
	along += gap * float64(len(placed)-1)
	if across*along > MaxJoinedPixels {
		w, h := across, along
		if sideBySide {
			w, h = along, across
		}
		return nil, convert.Refuse("error.merged_image_too_large", fmt.Sprintf("%.0f × %.0f px", w, h))
	}
	l := &joinLayout{}
	at := 0.0
	for _, p := range placed {
		offset := int(math.Floor((across - p.across) / 2))
		if sideBySide {
			l.Pictures = append(l.Pictures, placement{int(at), offset, int(p.along), int(p.across)})
		} else {
			l.Pictures = append(l.Pictures, placement{offset, int(at), int(p.across), int(p.along)})
		}
		at += p.along + gap
	}
	if sideBySide {
		l.W, l.H = int(along), int(across)
	} else {
		l.W, l.H = int(across), int(along)
	}
	return l, nil
}

func joinImages(ctx context.Context, inputs []convert.ImageInput, output, format string, o *presets.ImageOptions, progress convert.Progress) error {
	if len(inputs) == 0 {
		return fmt.Errorf("no pictures to merge")
	}
	reading := &presets.Preset{Image: *o}
	kept := make([]*Frame, len(inputs))
	sizes := make([][2]int, len(inputs))
	var total int64
	var dpiX, dpiY float64
	for i, in := range inputs {
		if err := ctx.Err(); err != nil {
			return err
		}
		f, err := readJoined(in, reading, o)
		if err != nil {
			return err
		}
		sizes[i] = [2]int{f.Width(), f.Height()}
		if dpiX == 0 {
			dpiX, dpiY = f.Meta.DpiX, f.Meta.DpiY
		}
		if px := int64(f.Width()) * int64(f.Height()); total+px <= keptPixels {
			kept[i] = f
			total += px
		}
		progress.Report(0.4 * float64(i+1) / float64(len(inputs)))
	}
	l, err := planJoin(sizes, o)
	if err != nil {
		return err
	}
	bg := backgroundColor(o.JoinBackground)
	transparent := strings.EqualFold(strings.TrimSpace(o.JoinBackground), presets.TransparentJoin)
	if transparent {
		bg = colorTransparent
	}
	merged := &Frame{Img: image.NewNRGBA(image.Rect(0, 0, l.W, l.H)), Alpha: transparent}
	fill(merged.Img, bg)
	for i, in := range inputs {
		if err := ctx.Err(); err != nil {
			return err
		}
		f := kept[i]
		kept[i] = nil
		if f == nil {
			if f, err = readJoined(in, reading, o); err != nil {
				return err
			}
		}
		p := l.Pictures[i]
		img := f.Img
		if f.Width() != p.W || f.Height() != p.H {
			img = Resize(img, p.W, p.H)
		}
		over(merged.Img, img, p.X, p.Y)
		progress.Report(0.4 + 0.4*float64(i+1)/float64(len(inputs)))
	}
	merged.Meta.DpiX, merged.Meta.DpiY = dpiX, dpiY
	// The pictures are turned already: the rest of the options apply to the merged image.
	rest := *o
	rest.Rotation, rest.Flip = presets.RotateNone, presets.FlipNone
	ApplyOptions(merged, &rest, format)
	if err := ctx.Err(); err != nil {
		return err
	}
	var data []byte
	if format == "apng" {
		data, err = encodeAPNG([]*Frame{merged}, 0)
	} else {
		data, err = encodeFrame(merged, format, OutputExtension(format, &rest, ""))
	}
	if err != nil {
		return err
	}
	if o.MaxSizeMb > 0 {
		if data, err = FitToSize(merged, data, format, format, sizeLimit(o.MaxSizeMb)); err != nil {
			return err
		}
	}
	if err := os.WriteFile(output, data, 0o644); err != nil {
		return err
	}
	progress.Report(1)
	return nil
}

// readJoined is one picture as it looks: the first page or frame (an icon's
// largest size), upright and turned by the preset.
func readJoined(in convert.ImageInput, reading *presets.Preset, o *presets.ImageOptions) (*Frame, error) {
	pic, err := Decode(in.Path, in.Format, reading)
	if err != nil {
		return nil, err
	}
	if len(pic.Frames) == 0 {
		return nil, fmt.Errorf("the picture has no image in it")
	}
	f := SelectFrames(pic, in.Format, "png")[0]
	f.AutoOrient()
	Turn(f, o)
	return f, nil
}
