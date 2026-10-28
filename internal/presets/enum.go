package presets

import (
	"encoding/json"
	"strings"
)

func trimmed(s string) string { return strings.TrimSpace(s) }

// lenient sets *v to the value of all matching it case-insensitively, or to
// def when it matches none (a misspelled or newer value).
func lenient[T ~string](v *T, def T, all ...T) {
	for _, a := range all {
		if strings.EqualFold(string(*v), string(a)) {
			*v = a
			return
		}
	}
	*v = def
}

// UnmarshalJSON reads a preset over the defaults, so missing fields keep
// their default values, and normalizes enumerations.
func (p *Preset) UnmarshalJSON(b []byte) error {
	type plain Preset
	d := plain(*New("", "png"))
	d.ID = ""
	if err := json.Unmarshal(b, &d); err != nil {
		return err
	}
	*p = Preset(d)
	if p.ID == "" {
		p.ID = NewID()
	}
	p.Normalize()
	return nil
}

// Normalize replaces unknown enumeration values with their defaults.
func (p *Preset) Normalize() {
	// An unknown kind is shown as a group: it never converts by surprise.
	lenient(&p.Kind, KindGroup, KindConvert, KindGroup, KindTool)
	i := &p.Image
	lenient(&i.Resize, ResizeNone, ResizeNone, ResizePercent, ResizeLongEdge, ResizeWidth, ResizeCustom, ResizeMinShortEdge)
	lenient(&i.Rotation, RotateNone, RotateNone, Right90, Left90, Rotate180)
	lenient(&i.Flip, FlipNone, FlipNone, FlipHorizontal, FlipVertical)
	lenient(&i.JoinDirection, JoinVertical, JoinVertical, JoinHorizontal)
	lenient(&i.JoinFit, JoinSameSize, JoinSameSize, JoinOriginal)
	lenient(&i.TiffCompression, TiffLzw, TiffNone, TiffLzw, TiffZip)
	lenient(&i.FitsTiff, FitsLinear, FitsLinear, FitsStretched)
	lenient(&i.HideStyle, HidePixelate, HidePixelate, HideBlur)
	if i.IcoSizes == nil {
		i.IcoSizes = []int{256, 128, 64, 48, 32, 16}
	}
	d := &p.Pdf
	lenient(&d.PageSize, PageFitImage, PageFitImage, PageA4, PageLetter)
	lenient(&d.Compression, PdfCompressNone, PdfCompressNone, PdfCompressScreen, PdfCompressEbook, PdfCompressPrint)
	lenient(&d.Rotation, PdfRotateNone, PdfRotateNone, PdfLeft90, PdfRight90, PdfRotate180)
	m := &p.Media
	lenient(&m.Quality, MediaBalanced, MediaHigh, MediaBalanced, MediaSmall)
	lenient(&m.WavFormat, Pcm16, Pcm16, Pcm8, Pcm24, Pcm32, Float32)
	lenient(&m.VideoCodec, VideoDefault, VideoDefault, VideoHevc, VideoAv1)
	lenient(&m.AudioCodec, AudioDefault, AudioDefault, AudioAlac)
	lenient(&m.LivePhoto, LivePhotoNone, LivePhotoNone, LivePhotoApple, LivePhotoAndroid)
	lenient(&p.Archive.Level, ArchiveNormal, ArchiveStore, ArchiveFast, ArchiveNormal, ArchiveMaximum)
	lenient(&p.Trace.Mode, TraceColor, TraceColor, TraceBlackAndWhite)
	lenient(&p.Cad.Version, AutoCad2018, AutoCad2018, AutoCad2013, AutoCad2010, AutoCad2007, AutoCad2004, AutoCad2000, SameAsSourceVersion)
	lenient(&p.Cad.Colors, CadAsDrawn, CadAsDrawn, CadBlackAndWhite, CadGrayscale)
	o := &p.Output
	lenient(&o.Location, LocationDefault, LocationDefault, LocationSameFolder, LocationSubfolder, LocationCustomFolder)
	lenient(&o.Conflict, ConflictRename, ConflictRename, ConflictOverwrite, ConflictSkip)
	for k := range o.Renames {
		r := &o.Renames[k]
		lenient(&r.Kind, RenameReplace, RenameReplace, RenameRemove, RenamePrefix, RenameSuffix, RenameSpacesToUnderscores,
			RenameRemoveCopyNumber, RenameRemoveBrackets, RenameRemoveLeadingNumber, RenameRemoveSymbols, RenameLowercase,
			RenameUppercase, RenameRegex)
	}
}
