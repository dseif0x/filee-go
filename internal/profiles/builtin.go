package profiles

import (
	"slices"

	"github.com/dseif0x/filee-go/internal/formats"
)

// Each built-in donut has a back face (Tab or the mouse wheel flips the open
// donut to it) with presets for its files that aren't on its front.

// PdfDonut is the front of the PDF donut.
var PdfDonut = []string{"to-png", "to-jpg", "pdf-long-image", "to-docx", "pdf-slides", "to-txt", "to-md", "to-epub", "to-hwpx", "to-hwp", "pdf-split", "merge-pdf"}

func b(id, key string, exts []string, front, back []string) *Profile {
	return &Profile{ID: id, NameKey: key, Extensions: exts, PresetIDs: slices.Clone(front), BackPresetIDs: slices.Clone(back)}
}

// BuiltIn returns the default donut profiles (fresh copies).
func BuiltIn() []*Profile {
	out := []*Profile{
		b("images", "profile.images", ExtensionsOf(formats.Image),
			[]string{"to-png", "to-jpg", "to-webp", "to-pdf", "trace-svg", "trace-line-art", "to-tiff", "to-bmp", "to-ico", "half-size", "tool-image-edit", "tool-hide"},
			// Full: "Extract Live Photo video" stays in the library, on no donut, and "GIF" since "Merge images" took its slot.
			[]string{"tool-crop", "rotate", "flip", "remove-background", "trim-transparent", "grayscale", "to-avif", "merge-images", "merge-pdf", "compress-image", "remove-metadata", "zip-all"}),
		b("pdf", "profile.pdf", []string{"pdf"}, PdfDonut,
			[]string{"pdf-edit", "compress-pdf", "to-webp", "to-tiff", "to-html", "to-odt", "to-rtf", "to-azw3", "to-cbz", "remove-metadata", "zip-all"}),
		b("office", "profile.office", ExtensionsOf(formats.Document),
			[]string{"to-pdf", "to-hwpx", "to-hwp", "to-docx", "to-odt", "to-txt", "to-html", "to-epub", "to-png", "merge-pdf"},
			[]string{"to-md", "to-rtf", "to-jpg", "pdf-long-image", "to-azw3", "to-kepub", "zip-all"}),
		b("spreadsheets", "profile.spreadsheets", ExtensionsOf(formats.Spreadsheet),
			[]string{"to-pdf", "to-xlsx", "to-csv", "to-json", "to-ods", "to-hwpx", "to-hwp", "to-html", "merge-pdf"},
			[]string{"tool-sheet-studio", "to-docx", "to-md", "to-txt", "to-yaml", "to-xml", "to-vcf", "to-ics", "to-png", "zip-all"}),
		b("presentations", "profile.presentations", ExtensionsOf(formats.Presentation),
			[]string{"to-pdf", "to-png", "to-jpg", "to-pptx", "to-hwpx", "to-hwp", "merge-pdf"},
			[]string{"to-odp", "to-webp", "pdf-long-image", "to-docx", "to-txt", "to-md", "to-html", "zip-all"}),
		b("hwp", "profile.hwp", []string{"hwp", "hwpx"},
			[]string{"to-pdf", "to-docx", "to-hwpx", "to-hwp", "to-png", "to-jpg", "to-txt", "to-md", "to-epub", "merge-pdf"},
			[]string{"to-html", "to-odt", "to-rtf", "pdf-long-image", "to-azw3", "zip-all"}),
		b("text", "profile.text", ExtensionsOf(formats.Text),
			[]string{"to-pdf", "to-hwpx", "to-hwp", "to-docx", "to-html", "to-txt", "to-epub"},
			[]string{"to-md", "to-odt", "to-rtf", "to-kepub", "merge-pdf", "zip-all"}),
		b("ebooks", "profile.ebooks", ExtensionsOf(formats.Ebook),
			[]string{"to-pdf", "to-epub", "to-docx", "to-txt", "to-azw3", "to-mobi", "to-kepub", "to-hwpx", "to-hwp", "to-html"},
			[]string{"to-fb2", "to-md", "to-rtf", "to-odt", "merge-pdf", "zip-all"}),
		b("video", "profile.video", ExtensionsOf(formats.Video),
			[]string{"to-mp4", "mp4-720", "mp4-hevc", "to-webm", "to-mov", "to-gif", "to-mp3", "extract-subtitles", "to-mkv", "to-avi"},
			// Every frame as pictures is on no front (a long video gives thousands of files).
			[]string{"tool-clip", "mp4-av1", "to-webp", "extract-frames-png", "extract-frames-jpg", "live-photo-iphone", "motion-photo-android",
				"extract-audio", "to-wav", "compress-video", "merge-videos", "zip-all"}),
		b("audio", "profile.audio", ExtensionsOf(formats.Audio),
			[]string{"to-mp3", "mp3-128", "to-m4a", "to-wav", "to-flac", "to-ogg", "to-opus", "to-aac"},
			[]string{"to-alac", "compress-audio", "merge-audio", "remove-metadata", "zip-all"}),
		b("subtitles", "profile.subtitles", ExtensionsOf(formats.Subtitle),
			[]string{"to-srt", "to-vtt", "to-ass", "to-smi", "to-lrc", "to-ttml"}, []string{"zip-all"}),
		b("vector", "profile.vector", ExtensionsOf(formats.Vector),
			[]string{"to-png", "to-pdf", "to-svg", "to-jpg", "to-webp", "to-eps"},
			[]string{"to-tiff", "to-ico", "to-icns", "to-avif", "to-dxf", "merge-pdf", "zip-all"}),
		// Nothing else converts archives: no back face.
		b("archives", "profile.archives", ExtensionsOf(formats.Archive),
			[]string{"extract", "zip-all", "to-zip", "to-7z", "to-tgz", "to-tar"}, []string{}),
		b("cad", "profile.cad", ExtensionsOf(formats.Cad),
			[]string{"to-pdf", "to-png", "to-svg", "to-dxf", "to-dwg"},
			[]string{"to-jpg", "to-webp", "to-tiff", "merge-pdf", "zip-all"}),
		b("fonts", "profile.fonts", ExtensionsOf(formats.Font),
			[]string{"to-woff2", "to-woff", "to-ttf", "to-otf", "to-eot"}, []string{"zip-all"}),
		b("data", "profile.data", ExtensionsOf(formats.Data),
			[]string{"to-json", "to-yaml", "to-toml", "to-xml", "to-csv", "to-xlsx"},
			[]string{"to-ods", "to-html", "to-md", "zip-all"}),
		b("models", "profile.models", ExtensionsOf(formats.Model3D),
			[]string{"to-glb", "to-stl", "to-obj", "to-3mf", "to-ply", "to-png"},
			[]string{"to-gltf", "to-jpg", "to-webp", "zip-all"}),
		// A shapefile is several files: one ZIP keeps them together.
		b("geo", "profile.geo", ExtensionsOf(formats.Geo),
			[]string{"to-geojson", "to-shp", "to-kml", "to-csv", "to-gpx", "to-kmz"}, []string{"zip-all"}),
	}
	mixed := b("mixed", "profile.mixed", []string{},
		[]string{"to-pdf", "merge-pdf", "zip-all", "to-png", "to-txt"},
		[]string{"to-jpg", "to-webp", "to-docx", "to-md", "to-html", "compress-pdf"})
	mixed.IsFallback = true
	return append(out, mixed)
}

// FindBuiltIn returns a fresh copy of the built-in profile id, or nil.
func FindBuiltIn(id string) *Profile {
	for _, p := range BuiltIn() {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// ExtensionsOf lists every file extension of the formats in category.
func ExtensionsOf(category formats.Category) []string {
	out := []string{}
	for _, f := range formats.Known {
		if f.Category != category {
			continue
		}
		for _, e := range f.Extensions {
			if e != "" {
				out = append(out, e)
			}
		}
	}
	return out
}
