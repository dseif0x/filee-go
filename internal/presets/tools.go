package presets

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/dseif0x/filee-go/internal/formats"
)

// Router is what the tools ask of the route planner: how many steps the
// cheapest plain route from one format to another takes (0 = none).
// *convert.Planner implements it.
type Router interface {
	Steps(from, to string) int
}

// toConversion turns a tool preset into a plain conversion to target.
func toConversion(tool *Preset, target string) *Preset {
	p := tool.Clone()
	p.Kind = KindConvert
	p.Tool = ""
	p.SubPresetIDs = nil
	p.TargetFormat = target
	return p
}

// ---- "Cut a clip" ----

// ClipFallbackTarget is where a clip goes when the video's own format can't be written.
const ClipFallbackTarget = "mp4"

// ClipTargetFor is the format a clip of a sourceFormat video is written in:
// its own when one engine writes it straight from the video, else MP4.
func ClipTargetFor(sourceFormat string, r Router) string {
	if r.Steps(sourceFormat, sourceFormat) == 1 {
		return sourceFormat
	}
	return ClipFallbackTarget
}

// CanCutClip: a sourceFormat file is a video a clip can be written of.
func CanCutClip(sourceFormat string, r Router) bool {
	return formats.CategoryOf(sourceFormat) == formats.Video && r.Steps(sourceFormat, ClipTargetFor(sourceFormat, r)) == 1
}

// ClipConversionOf is the conversion that writes clip of a video as target,
// with the tool's name, output rule and media options. The tool is left alone.
func ClipConversionOf(tool *Preset, target string, clip *ClipEdit) *Preset {
	p := toConversion(tool, target)
	// Only the clip: none of the other things a video preset can make of a video.
	p.Media.SaveEveryFrame = false
	p.Media.ExtractMotionPhoto = false
	p.Media.LivePhoto = LivePhotoNone
	p.Media.Clip = clip.Clone()
	return p
}

// ---- "Pixelate / blur" ----

const (
	// HideFallbackTarget is where a picture goes when its own format can't be written.
	HideFallbackTarget = "png"
	MinHideStrength    = 1
	MaxHideStrength    = 10
	// MinEffectPixels is the smallest square (or blur) in pixels.
	MinEffectPixels = 8
	// MinAreaSide: parts smaller than this (in parts of the side) are a click, not a drag.
	MinAreaSide = 0.004
)

// CanHide: a sourceFormat file is a picture the tool can open.
func CanHide(sourceFormat string) bool { return formats.CategoryOf(sourceFormat) == formats.Image }

// writableFromItself: the format is written straight from itself.
func writableFromItself(source string, r Router) bool {
	f := formats.ByID(source)
	return f != nil && f.Writable() && r.Steps(source, source) > 0
}

// HideTargetFor is SameAsSource when the picture can be written from itself, else PNG.
func HideTargetFor(sourceFormat string, r Router) string {
	if writableFromItself(sourceFormat, r) {
		return SameAsSource
	}
	return HideFallbackTarget
}

// HideConversionOf is the conversion that writes a picture with areas covered
// as the tool says. The tool itself is left as it is.
func HideConversionOf(tool *Preset, target string, areas []HiddenArea) *Preset {
	p := toConversion(tool, target)
	p.Image.HiddenAreas = []HiddenArea{}
	for _, a := range areas {
		if c, ok := ClampArea(a); ok {
			p.Image.HiddenAreas = append(p.Image.HiddenAreas, c)
		}
	}
	p.Image.HideStrength = ClampHideStrength(p.Image.HideStrength)
	// "Only if smaller" would keep the original, every part of it in view.
	p.Output.OnlyIfSmaller = false
	return p
}

// RememberHide keeps the window's choice in the tool for the next time.
func RememberHide(tool *Preset, style HideStyle, strength int) {
	tool.Image.HideStyle = style
	tool.Image.HideStrength = ClampHideStrength(strength)
}

func ClampHideStrength(s int) int { return clamp(s, MinHideStrength, MaxHideStrength) }

// HideEffectSize is the side of a square (pixelate) or the blur's sigma in
// pixels of a width × height picture: 2 % of its shorter side at the lowest
// strength to 6.5 % at the highest, never under MinEffectPixels. Tied to the
// picture, not the part: squares much smaller than the letters leave text guessable.
func HideEffectSize(strength, width, height int) float64 {
	return max(MinEffectPixels, float64(min(max(1, width), max(1, height)))*(0.015+0.005*float64(ClampHideStrength(strength))))
}

// ClampArea is the area within the picture; false when nothing of it is left.
func ClampArea(a HiddenArea) (HiddenArea, bool) {
	if !finite(a.X) || !finite(a.Y) || !finite(a.Width) || !finite(a.Height) {
		return HiddenArea{}, false
	}
	left := clamp(min(a.X, a.X+a.Width), 0, 1)
	right := clamp(max(a.X, a.X+a.Width), 0, 1)
	top := clamp(min(a.Y, a.Y+a.Height), 0, 1)
	bottom := clamp(max(a.Y, a.Y+a.Height), 0, 1)
	if right > left && bottom > top {
		return HiddenArea{X: left, Y: top, Width: right - left, Height: bottom - top}, true
	}
	return HiddenArea{}, false
}

// PixelBounds is the area in whole pixels of a width × height picture,
// rounded outwards, at least one pixel; false when nothing of it is on the picture.
func PixelBounds(a HiddenArea, width, height int) (x, y, w, h int, ok bool) {
	in, ok := ClampArea(a)
	if width <= 0 || height <= 0 || !ok {
		return 0, 0, 0, 0, false
	}
	left := clamp(int(math.Floor(in.X*float64(width)+1e-9)), 0, width-1)
	top := clamp(int(math.Floor(in.Y*float64(height)+1e-9)), 0, height-1)
	right := clamp(int(math.Ceil((in.X+in.Width)*float64(width)-1e-9)), left+1, width)
	bottom := clamp(int(math.Ceil((in.Y+in.Height)*float64(height)-1e-9)), top+1, height)
	return left, top, right - left, bottom - top, true
}

// CellOf is the square a pixel column (or row) falls in, by the pixel's centre.
func CellOf(pixel int, cell float64) int { return int(math.Floor((float64(pixel) + 0.5) / cell)) }

// CellStart is the first pixel column (or row) of square index.
func CellStart(index int, cell float64) int { return int(math.Ceil(float64(index)*cell - 0.5)) }

// ---- "Edit pictures" ----

// ImageEditFallbackTarget is where a picture goes when "same format" can't be written.
const ImageEditFallbackTarget = "png"

var (
	// ImageEditTargets are the formats the window offers after "Same format".
	ImageEditTargets = []string{"png", "jpg", "webp", "avif", "heic", "gif", "bmp", "tiff", "ico"}
	// ImageEditLossyTargets show the quality slider.
	ImageEditLossyTargets = []string{"jpg", "webp", "avif", "heic"}
	// ImageEditResizeModes are the resizes the window offers.
	ImageEditResizeModes = []ResizeMode{ResizeNone, ResizePercent, ResizeLongEdge, ResizeWidth}
)

// CanEditImage: a sourceFormat file is a picture the tool can open.
func CanEditImage(sourceFormat string) bool { return formats.CategoryOf(sourceFormat) == formats.Image }

// ImageEditTargetsFrom are the targets at least one source can be written in.
func ImageEditTargetsFrom(sourceFormats []string, r Router) []string {
	var out []string
	for _, t := range ImageEditTargets {
		for _, s := range sourceFormats {
			if r.Steps(s, t) > 0 {
				out = append(out, t)
				break
			}
		}
	}
	return out
}

// ImageEditTargetFor is chosen, but "same format" that can't be written straight from itself becomes PNG.
func ImageEditTargetFor(sourceFormat, chosen string, r Router) string {
	if chosen != SameAsSource {
		return chosen
	}
	if writableFromItself(sourceFormat, r) {
		return SameAsSource
	}
	return ImageEditFallbackTarget
}

// ImageEditConversionOf is the conversion with image's options and the tool's name and output rule.
func ImageEditConversionOf(tool *Preset, target string, image ImageOptions) *Preset {
	p := toConversion(tool, target)
	donor := &Preset{Image: image}
	p.Image = donor.Clone().Image
	return p
}

// RememberImageEdit keeps the window's choices in the tool for the next time.
func RememberImageEdit(tool *Preset, target string, image ImageOptions) {
	tool.TargetFormat = target
	k := &tool.Image
	k.Resize, k.ResizePercent, k.LongEdge, k.Width = image.Resize, image.ResizePercent, image.LongEdge, image.Width
	k.Rotation, k.Flip, k.Grayscale, k.Quality = image.Rotation, image.Flip, image.Grayscale, image.Quality
}

// ---- "Ask every time" ----

// MakesOneResult: the batch makes one file, so the save dialog asks for its
// name; otherwise it asks for a folder.
func MakesOneResult(p *Preset, sources int) bool {
	return sources == 1 || p.TargetFormat == "pdf" && p.Pdf.MergeIntoSingle || p.Image.MultiPageTiff && p.TargetFormat == "tiff" ||
		p.PacksIntoOneArchive() || p.JoinsIntoOneMedia() || p.JoinsImages()
}

// SuggestedName is the name the save dialog suggests for source.
func SuggestedName(p *Preset, presetName, source string) string {
	name := ExpandPattern(p.Output.FileNamePattern, Tokens{SourcePath: source, PresetName: presetName, Index: 1, Now: time.Now()})
	for _, r := range p.Output.Renames {
		name = r.Apply(name)
	}
	name = Sanitize(name, formats.NameWithoutExtension(source))
	if ext := ResultExtension(p, source); ext != "" {
		return name + "." + ext
	}
	return name
}

// ResultExtension is the extension the result gets, without the dot: "zip"
// when saved as a ZIP, the source's own for "same as source", "" for a folder.
func ResultExtension(p *Preset, source string) string {
	switch {
	case p.Output.ZipResult:
		return "zip"
	case p.TargetFormat == SameAsSource:
		return formats.ExtensionOf(source)
	}
	if f := formats.ByID(p.TargetFormat); f != nil && len(f.Extensions) > 0 {
		return f.Extensions[0]
	}
	return ""
}

// ForChosenFile is the preset saving its one result as chosenPath: that
// folder and name, without the pattern or renames. Only that exact path may
// replace a file there.
func ForChosenFile(p *Preset, chosenPath string) *Preset {
	full, _ := filepath.Abs(chosenPath)
	c := inFolder(p, filepath.Dir(full))
	base := filepath.Base(full)
	c.Output.FileNamePattern = base[:len(base)-len(filepath.Ext(base))]
	c.Output.Renames = nil
	c.Output.ConfirmedPath = full
	return c
}

// ForChosenFolder saves several results in folder under the preset's own names.
func ForChosenFolder(p *Preset, folder string) *Preset { return inFolder(p, folder) }

func inFolder(p *Preset, folder string) *Preset {
	c := p.Clone()
	c.Output.AskEveryTime = false
	c.Output.Location = LocationCustomFolder
	c.Output.CustomFolder = folder
	c.Output.KeepFoldersFrom = ""
	c.Output.SubfolderOnlyForSeveralFiles = false
	// The place the user picked is where the result goes, not back over the original.
	c.Output.ReplaceOriginal = false
	return c
}

// ---- size limit (#553) ----

// SizeLimitBytes is megabytes of 1,000,000 bytes, as mail and chat services count them.
func SizeLimitBytes(megabytes float64) int64 { return int64(megabytes * 1_000_000) }

// DescribeSize is "12.3 MB > 10 MB".
func DescribeSize(size, limit int64) string {
	f := func(b int64) string { return strconv.FormatFloat(math.Round(float64(b)/1e4)/100, 'f', -1, 64) }
	return fmt.Sprintf("%s MB > %s MB", f(size), f(limit))
}

// SizeLimitFor is the limit for a result of path's kind: pictures and videos have their own; 0 = none.
func SizeLimitFor(p *Preset, path string) int64 {
	switch formats.CategoryOf(idOf(path)) {
	case formats.Image:
		if p.Image.MaxSizeMb > 0 {
			return SizeLimitBytes(p.Image.MaxSizeMb)
		}
	case formats.Video:
		if p.Media.MaxSizeMb > 0 {
			return SizeLimitBytes(p.Media.MaxSizeMb)
		}
	}
	return 0
}

// OverSizeLimit returns the first output over its limit with the description, or "".
func OverSizeLimit(p *Preset, outputs []string) (path, detail string) {
	for _, o := range outputs {
		limit := SizeLimitFor(p, o)
		if limit <= 0 {
			continue
		}
		if fi, err := os.Stat(o); err == nil && !fi.IsDir() && fi.Size() > limit {
			return o, DescribeSize(fi.Size(), limit)
		}
	}
	return "", ""
}

func idOf(path string) string {
	if f := formats.Detect(path); f != nil {
		return f.ID
	}
	return ""
}
