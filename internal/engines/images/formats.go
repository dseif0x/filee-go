package images

import (
	"math"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/presets"
)

// Readable are the image formats this engine reads (see Decode). Camera RAW is
// developed by dcraw (WebAssembly) or, failing that, its embedded preview is
// used; XCF, HEIC, DICOM, Photo CD, OpenEXR, Radiance HDR and FITS are read only.
var Readable = []string{
	"png", "jpg", "webp", "tiff", "bmp", "gif", "ico", "avif", "heic",
	"jxl", "jp2", "psd", "psb", "tga", "ppm", "xcf", "raw",
	"apng", "mng", "qoi", "dds", "pcx", "cur", "xpm", "xbm", "wbmp", "pam", "pfm", "sgi", "ras", "miff",
	"dcm", "pcd", "exr", "hdr", "fits",
}

// Writable are the image formats this engine writes. HEIC is missing: there is
// no HEVC encoder in pure Go, so HEIC is written with FFmpeg (package heic).
var Writable = []string{
	"png", "jpg", "webp", "tiff", "bmp", "gif", "ico", "avif",
	"jxl", "jp2", "psd", "psb", "tga", "ppm",
	"apng", "mng", "qoi", "dds", "pcx", "cur", "xpm", "xbm", "wbmp", "pam", "pfm", "sgi", "ras", "miff",
}

// Packages are files holding a picture and more, of which the picture is read (an iPhone LIVP).
var Packages = []string{"livp"}

// Sources are every format the engine takes as input. The Windows metafiles
// (EMF/WMF), which ImageMagick renders through GDI+, are not among them: there
// is no pure-Go renderer for them.
func Sources() []string { return append(slices.Clone(Readable), Packages...) }

var (
	// multiFrame formats store several frames/pages in one file.
	multiFrame = []string{"tiff", "gif", "apng", "mng"}
	// animated formats' frames are an animation: between these, all frames are kept with their timing.
	animated = []string{"gif", "webp", "apng", "mng"}
	// noAlpha formats are written without alpha: transparency is flattened onto the background.
	noAlpha = []string{"jpg", "ppm", "heic", "xbm", "wbmp", "pfm", "pcx", "ras"}
	// sizeVariants hold one picture in several sizes (icons) or mipmaps: the largest is converted.
	sizeVariants = []string{"ico", "cur", "dds"}
)

// CanRead reports whether the engine reads format.
func CanRead(format string) bool { return slices.Contains(Sources(), format) }

// CanWrite reports whether the engine writes format.
func CanWrite(format string) bool { return slices.Contains(Writable, format) }

// Edge is the edge an image engine offers from source to target. GIF holds 256
// colours, so a still picture costs more there (it reaches video through a
// full-colour PNG instead), while animations and multi-page files still go
// through GIF. Still picture → APNG costs as much. FITS → TIFF is the data for
// further processing: it costs a little more, so FITS reaches other formats
// through a stretched PNG or JPG instead.
func Edge(source, target string) convert.Edge {
	cost := 10
	switch {
	case (target == "gif" || target == "apng") && !slices.Contains(multiFrame, source) && !slices.Contains(animated, source):
		cost = 12
	case source == "fits" && target == "tiff":
		cost = 11
	}
	return convert.EC(source, target, cost)
}

// KeepsAnimation: frames of source are written as one animation in target.
func KeepsAnimation(source, target string) bool {
	return slices.Contains(animated, source) && slices.Contains(animated, target)
}

// HoldsAlpha is false for formats written without transparency.
func HoldsAlpha(format string) bool { return !slices.Contains(noAlpha, format) }

// MaxSide is the longest side a format stores: JPEG 65,535 px (65,500 keeps
// clear of encoder rounding), WebP 16,383 px, AVIF 16,384 px.
func MaxSide(target string) float64 {
	switch target {
	case "jpg", "jpeg":
		return 65_500
	case "webp":
		return 16_383
	case "avif":
		return 16_384
	}
	return math.Inf(1)
}

// OutputExtension is the extension written for target. "ppm" covers the Netpbm
// family: a grey result, or a .pgm/.pbm source kept as such, is written as
// PGM/PBM with its own extension.
func OutputExtension(target string, o *presets.ImageOptions, sourcePath string) string {
	if target != "ppm" {
		return target
	}
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(sourcePath), ".")) {
	case "pbm":
		return "pbm"
	case "pgm":
		return "pgm"
	}
	if o != nil && o.Grayscale {
		return "pgm"
	}
	return "ppm"
}

// SelectFrames chooses the frames of a (possibly multi-frame) source that are
// converted. ICO, CUR, DDS: the largest. Animations: every frame for an
// animated or multi-frame target, else the first. PSD/PSB: the composite.
// Otherwise all frames/pages. Animation frames are already whole pictures
// (coalesced when read).
func SelectFrames(p *Picture, source, target string) []*Frame {
	frames := p.Frames
	if len(frames) <= 1 {
		return slices.Clone(frames)
	}
	switch {
	case source == "psd" || source == "psb":
		return frames[:1]
	case slices.Contains(sizeVariants, source):
		best := frames[0]
		for _, f := range frames[1:] {
			if f.Width()*f.Height() > best.Width()*best.Height() {
				best = f
			}
		}
		return []*Frame{best}
	case slices.Contains(animated, source) && !slices.Contains(multiFrame, target) && !KeepsAnimation(source, target):
		return frames[:1]
	}
	return slices.Clone(frames)
}
