package radial

import (
	"slices"
	"strings"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/i18n"
	"github.com/dseif0x/filee-go/internal/presets"
)

// singleFrameRaster are the picture formats the crop overlay writes back.
var singleFrameRaster = []string{"png", "jpg", "bmp", "tiff", "webp", "avif", "heic", "jxl", "qoi", "tga", "ico", "jp2"}

// unavailable is why a preset can't convert files of these formats with the
// engines installed now, "" when it can (PresetAvailability.Check, without
// the engine downloads the Go version doesn't have).
func unavailable(planner *convert.Planner, texts *i18n.Localizer, p *presets.Preset, fs []*formats.Format) string {
	reason := func(key string) string { return texts.Text(key) }
	switch {
	case p.IsClipTool():
		for _, f := range fs {
			if presets.CanCutClip(f.ID, planner) {
				return ""
			}
		}
		if !slices.ContainsFunc(fs, func(f *formats.Format) bool { return f.Category == formats.Video }) {
			return reason("error.unsupported_source")
		}
		return reason("error.no_route")
	case p.IsImageEditTool():
		if slices.ContainsFunc(fs, func(f *formats.Format) bool { return presets.CanEditImage(f.ID) }) {
			return ""
		}
		return reason("error.images_only")
	case p.IsHideTool():
		if slices.ContainsFunc(fs, func(f *formats.Format) bool { return presets.CanHide(f.ID) }) {
			return ""
		}
		return reason("error.images_only")
	case p.IsTool():
		all := func(ok func(*formats.Format) bool) bool {
			return len(fs) > 0 && !slices.ContainsFunc(fs, func(f *formats.Format) bool { return !ok(f) })
		}
		switch {
		case p.ID == "tool-sheet-studio":
			if all(func(f *formats.Format) bool { return f.ID == "xlsx" }) {
				if planner.Steps("xlsx", "pdf") > 0 {
					return ""
				}
				return reason("error.no_route")
			}
			return reason("error.unsupported_source")
		case strings.HasPrefix(p.ID, "tool-pdf-"):
			if all(func(f *formats.Format) bool { return f.ID == "pdf" }) {
				return ""
			}
			return reason("error.unsupported_source")
		case p.IsCropTool() && all(func(f *formats.Format) bool { return slices.Contains(singleFrameRaster, f.ID) }):
			return ""
		}
		return reason("error.unsupported_source")
	case p.PacksIntoOneArchive():
		// Packs the files as they are, of any type.
		return ""
	}
	if len(fs) == 0 {
		return reason("error.unsupported_source")
	}
	if p.TargetFormat == presets.SameAsSource && p.Image.EditsPicture() {
		fs = slices.DeleteFunc(slices.Clone(fs), func(f *formats.Format) bool { return f.Category != formats.Image })
		if len(fs) == 0 {
			return reason("error.images_only")
		}
	}
	if p.JoinsImages() {
		for _, f := range fs {
			if (f.Category == formats.Image || f.Category == formats.Vector) && (f.ID == "png" || planner.Steps(f.ID, "png") > 0) {
				return ""
			}
		}
		return reason("error.images_only")
	}
	if p.JoinsIntoOneMedia() {
		target := p.TargetFormat
		for _, f := range fs {
			if p.Media.Slideshow {
				if (f.Category == formats.Image || f.Category == formats.Vector) && (f.ID == "png" || planner.Steps(f.ID, "png") > 0) && planner.Steps("png", target) > 0 {
					return ""
				}
			} else if (f.Category == formats.Video || f.Category == formats.Audio && target == "m4a") && planner.Steps(f.ID, target) > 0 {
				return ""
			}
		}
		return reason("error.no_route")
	}
	merge := p.Pdf.MergeIntoSingle && p.TargetFormat == "pdf"
	turns := p.Image.TurnsOrMirrors()
	copiesUnturned := false
	for _, f := range fs {
		target := p.TargetFormat
		if target == presets.SameAsSource {
			target = f.ID
		}
		if merge && f.ID == "pdf" && !turns && !p.RecognizesTextOf(f.ID) {
			return ""
		}
		o := convert.Options{Frames: p.SavesFramesOf(f.ID), MotionPhoto: p.ExtractsMotionOf(f.ID), LivePhoto: p.MakesLivePhotoOf(f.ID), Ocr: p.RecognizesTextOf(f.ID)}
		route := planner.Plan(f.ID, target, o)
		if route == nil {
			continue
		}
		if !turns || route.FirstTransformStep() >= 0 {
			return ""
		}
		copiesUnturned = true
	}
	if copiesUnturned {
		return reason("error.cannot_turn")
	}
	return reason("error.no_route")
}
