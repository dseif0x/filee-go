package presets

import "github.com/dseif0x/filee-go/internal/formats"

// Factory defaults: the presets a new library has, and those offered by
// Settings → Presets → Add. The donut layouts are profiles.BuiltIn.

// p builds a built-in convert preset; set changes its options.
func p(id, name, target string, set ...func(*Preset)) *Preset {
	x := New(name, target)
	x.ID = id
	for _, f := range set {
		f(x)
	}
	return x
}

// k builds a built-in preset named by a localization key.
func k(id, nameKey, target string, set ...func(*Preset)) *Preset {
	x := p(id, "", target, set...)
	x.NameKey = nameKey
	return x
}

func pattern(s string) func(*Preset) { return func(x *Preset) { x.Output.FileNamePattern = s } }
func quality(q int) func(*Preset)    { return func(x *Preset) { x.Image.Quality = q } }
func tool(name string) func(*Preset) { return func(x *Preset) { x.Kind = KindTool; x.Tool = name } }
func group(ids ...string) func(*Preset) {
	return func(x *Preset) { x.Kind = KindGroup; x.TargetFormat = GroupTarget; x.SubPresetIDs = ids }
}

// BuiltIn returns the default preset list (fresh copies).
func BuiltIn() []*Preset {
	var out []*Preset
	add := func(ps ...*Preset) { out = append(out, ps...) }
	add(
		p("to-png", "PNG", "png"),
		p("to-jpg", "JPG", "jpg", quality(90)),
		p("jpg-70", "JPG 70%", "jpg", quality(70), pattern("{name}_70")),
		p("to-webp", "WEBP", "webp", quality(85)),
		p("to-tiff", "TIFF", "tiff"),
		p("to-bmp", "BMP", "bmp"),
		p("to-ico", "ICO", "ico"),
		p("to-gif", "GIF", "gif"),
		p("to-avif", "AVIF", "avif", quality(60)),
		p("to-heic", "HEIC", "heic", quality(80)),
		k("half-size", "preset.half_size", SameAsSource, func(x *Preset) {
			x.Image.Resize = ResizePercent
			x.Image.ResizePercent = 50
		}, pattern("{name}_50")),
		k("grayscale", "preset.grayscale", SameAsSource, func(x *Preset) { x.Image.Grayscale = true }, pattern("{name}_gray")),
		k("remove-background", "preset.remove_background", "png", func(x *Preset) { x.Image.RemoveBackground = true }, pattern("{name}_transparent")),
	)
	add(TrimPresets()...)
	add(k("tool-crop", "preset.crop", SameAsSource, tool(""), pattern("{name}_cropped")))
	add(ImageEditPresets()...)
	add(HidePresets()...)
	add(
		k("tool-sheet-studio", "preset.sheet_studio", "pdf", tool(""), pattern("{name}_prepared")),
		k("tool-pdf-crop", "preset.pdf_crop", SameAsSource, tool(""), pattern("{name}_cropped")),
		k("tool-pdf-pages", "preset.pdf_pages", SameAsSource, tool(""), pattern("{name}_edited")),
		k("tool-pdf-add", "preset.pdf_add", SameAsSource, tool(""), pattern("{name}_merged")),
		k("pdf-rotate-left", "preset.rotate_left", "pdf", func(x *Preset) { x.Pdf.Rotation = PdfLeft90 }, pattern("{name}_left")),
		k("pdf-rotate-right", "preset.rotate_right", "pdf", func(x *Preset) { x.Pdf.Rotation = PdfRight90 }, pattern("{name}_right")),
		k("pdf-edit", "preset.group_pdf_edit", GroupTarget,
			group("tool-pdf-crop", "pdf-rotate-left", "pdf-rotate-right", "tool-pdf-pages", "pdf-split", "tool-pdf-add")),
	)
	add(ImageToolPresets()...)
	add(
		p("tiff-300", "TIFF 300dpi", "tiff", func(x *Preset) {
			dpi := 300
			x.Image.Dpi = &dpi
			x.Pdf.RenderDpi = 300
		}, pattern("{name}_300dpi")),
		p("to-pdf", "PDF", "pdf"),
		k("merge-pdf", "preset.merge_pdf", "pdf", func(x *Preset) { x.Pdf.MergeIntoSingle = true }, pattern("{name}_merged")),
		k("pdf-long-image", "preset.long_image", "png", func(x *Preset) { x.Pdf.CombinePages = true }, pattern("{name}_long")),
		k("pdf-split", "preset.split_pdf", "pdf", func(x *Preset) {
			x.Pdf.SplitPages = true
			x.Output.Location = LocationSubfolder
			x.Output.SubfolderName = "{name}"
		}),
	)
	add(PdfCompressionPresets()...)
	add(OcrPresets()...)
	add(PdfSlidesPresets()...)
	add(FramePresets()...)
	add(MotionPhotoPresets()...)
	add(LivePhotoPresets()...)
	add(ClipPresets()...)
	add(MediaToolPresets()...)
	add(MergeImagesPresets()...)
	add(p("to-docx", "DOCX", "docx"), p("to-hwpx", "HWPX", "hwpx"))
	add(HwpPresets()...)
	add(
		p("to-odt", "ODT", "odt"),
		p("to-rtf", "RTF", "rtf"),
		p("to-txt", "TXT", "txt"),
		p("to-md", "Markdown", "md"),
		p("to-html", "HTML", "html"),
	)
	add(SpreadsheetPresets()...)
	add(CatalogPresets()...)
	add(DataPresets()...)
	add(CodecAndSubtitlePresets()...)
	add(ModelPresets()...)
	add(GeoPresets()...)
	add(TracePresets()...)
	return out
}

// FindBuiltIn returns a fresh copy of the built-in preset id, or nil.
func FindBuiltIn(id string) *Preset {
	for _, x := range BuiltIn() {
		if x.ID == id {
			return x
		}
	}
	return nil
}

// PlacePresets are ready-made presets for where a file is sent (#553),
// offered only by Settings → Presets → Add. Email: 18 MB, since an
// attachment grows by a third on the way and most providers stop at 25 MB.
func PlacePresets() []*Preset {
	return []*Preset{
		k("for-email", "preset.for_email", "mp4", func(x *Preset) { x.Media.MaxSizeMb = 18 }, pattern("{name}_email")),
		k("for-discord", "preset.for_discord", "mp4", func(x *Preset) { x.Media.MaxSizeMb = 10 }, pattern("{name}_discord")),
		k("for-whatsapp", "preset.for_whatsapp", "mp4", func(x *Preset) {
			x.Media.MaxHeight = 720
			x.Media.MaxSizeMb = 16
		}, pattern("{name}_whatsapp")),
		k("for-instagram", "preset.for_instagram", "jpg", func(x *Preset) {
			x.Image.Resize = ResizeWidth
			x.Image.Width = 1080
		}, pattern("{name}_instagram")),
	}
}

// MotionPhotoPresets: the video of a Live / Motion Photo as MP4.
func MotionPhotoPresets() []*Preset {
	return []*Preset{k("extract-motion", "preset.extract_motion", "mp4", func(x *Preset) { x.Media.ExtractMotionPhoto = true })}
}

// LivePhotoPresets: a clip of a video as an iPhone Live Photo or an Android Motion Photo.
func LivePhotoPresets() []*Preset {
	return []*Preset{
		k("live-photo-iphone", "preset.live_photo_iphone", "jpg", func(x *Preset) { x.Media.LivePhoto = LivePhotoApple }),
		k("motion-photo-android", "preset.motion_photo_android", "jpg", func(x *Preset) { x.Media.LivePhoto = LivePhotoAndroid }),
	}
}

// ClipPresets: "Cut a clip", a part of a video chosen on a timeline.
func ClipPresets() []*Preset {
	return []*Preset{k("tool-clip", "preset.clip", SameAsSource, tool(ClipTool), pattern("{name}_clip"))}
}

// ImageEditPresets: "Edit pictures" (#584), choices made in a window each time.
func ImageEditPresets() []*Preset {
	return []*Preset{k("tool-image-edit", "preset.image_edit", SameAsSource, tool(ImageEditTool), pattern("{name}_edited"))}
}

// HidePresets: "Pixelate / blur" parts of a picture before it is shared.
func HidePresets() []*Preset {
	return []*Preset{k("tool-hide", "preset.hide", SameAsSource, tool(HideTool), pattern("{name}_hidden"))}
}

// TrimPresets: "Trim transparent borders" (#332).
func TrimPresets() []*Preset {
	return []*Preset{k("trim-transparent", "preset.trim_transparent", SameAsSource, func(x *Preset) { x.Image.TrimTransparent = true }, pattern("{name}_trimmed"))}
}

// MediaToolPresets: smaller copies, a video of photos, a video's sound,
// joined videos or audio, a copy without metadata.
func MediaToolPresets() []*Preset {
	return []*Preset{
		// Quality 75 like most photo apps' "smaller"; a JPEG saved below it stays at its own quality.
		k("compress-image", "preset.compress_image", SameAsSource, func(x *Preset) {
			x.Image.Quality = 75
			x.Image.MaxCompression = true
			x.Image.TiffCompression = TiffZip
			x.Output.OnlyIfSmaller = true
		}, pattern("{name}_compressed")),
		// H.264 at the Small quality, at most 1080p.
		k("compress-video", "preset.compress_video", "mp4", func(x *Preset) {
			x.Media.Quality = MediaSmall
			x.Media.MaxHeight = 1080
			x.Output.OnlyIfSmaller = true
		}, pattern("{name}_compressed")),
		k("video-from-images", "preset.video_from_images", "mp4", func(x *Preset) { x.Media.Slideshow = true }, pattern("{name}_video")),
		k("extract-audio", "preset.extract_audio", "m4a", func(x *Preset) { x.Media.CopyAudioWhenPossible = true }),
		k("merge-videos", "preset.merge_videos", "mp4", func(x *Preset) { x.Media.JoinIntoOne = true }, pattern("{name}_merged")),
		// AAC at the Small quality: 128 kbit/s for stereo.
		k("compress-audio", "preset.compress_audio", "m4a", func(x *Preset) {
			x.Media.Quality = MediaSmall
			x.Output.OnlyIfSmaller = true
		}, pattern("{name}_compressed")),
		k("merge-audio", "preset.merge_audio", "m4a", func(x *Preset) { x.Media.JoinIntoOne = true }, pattern("{name}_merged")),
		// Everything goes, the colour profile too. File dates are new ones.
		k("remove-metadata", "preset.remove_metadata", SameAsSource, func(x *Preset) {
			x.Image.Quality = 90
			x.Image.KeepMetadata = false
			x.Image.KeepColorProfile = false
			x.Image.KeepSourceQuality = true
			x.Pdf.RemoveMetadata = true
			x.Media.RemoveMetadata = true
		}, pattern("{name}_clean")),
	}
}

// MergeImagesPresets: "Merge images" (#645) into one picture in the first one's format.
func MergeImagesPresets() []*Preset {
	return []*Preset{k("merge-images", "preset.merge_images", SameAsSource, func(x *Preset) {
		x.Image.JoinIntoOne = true
		x.Image.Quality = 90
	}, pattern("{name}_merged"))}
}

// PdfCompressionPresets: "Compress PDF", pictures at 150 dpi.
func PdfCompressionPresets() []*Preset {
	return []*Preset{k("compress-pdf", "preset.compress_pdf", "pdf", func(x *Preset) { x.Pdf.Compression = PdfCompressEbook }, pattern("{name}_compressed"))}
}

// OcrPresets: "Searchable PDF", text recognized by Tesseract.
func OcrPresets() []*Preset {
	return []*Preset{k("searchable-pdf", "preset.searchable_pdf", "pdf", func(x *Preset) { x.Pdf.Ocr = true }, pattern("{name}_searchable"))}
}

// PdfSlidesPresets: PDF → PowerPoint with a picture of each page on its own slide.
func PdfSlidesPresets() []*Preset {
	return []*Preset{k("pdf-slides", "preset.pdf_slides", "pptx", quality(90))}
}

// FramePresets: every frame of a video as pictures in a folder.
func FramePresets() []*Preset {
	every := func(x *Preset) { x.Media.SaveEveryFrame = true }
	return []*Preset{
		k("extract-frames-png", "preset.extract_frames_png", "png", every),
		k("extract-frames-jpg", "preset.extract_frames_jpg", "jpg", every),
	}
}

// TracePresets: raster → SVG tracing, in colour and as black line art.
func TracePresets() []*Preset {
	return []*Preset{
		k("trace-svg", "preset.trace_svg", "svg"),
		k("trace-line-art", "preset.trace_line_art", "svg", func(x *Preset) { x.Trace.Mode = TraceBlackAndWhite }),
	}
}

// CodecAndSubtitlePresets: HEVC / AV1 video, ALAC and the subtitle targets.
func CodecAndSubtitlePresets() []*Preset {
	return []*Preset{
		p("mp4-hevc", "MP4 HEVC", "mp4", func(x *Preset) { x.Media.VideoCodec = VideoHevc }, pattern("{name}_hevc")),
		p("mp4-av1", "MP4 AV1", "mp4", func(x *Preset) { x.Media.VideoCodec = VideoAv1 }, pattern("{name}_av1")),
		p("to-alac", "ALAC", "m4a", func(x *Preset) { x.Media.AudioCodec = AudioAlac }),
		// On the video donut, SRT takes the first text subtitle track out of the video.
		k("extract-subtitles", "preset.extract_subtitles", "srt"),
		p("to-srt", "SRT", "srt"),
		p("to-vtt", "WebVTT", "vtt"),
		p("to-ass", "ASS", "ass"),
		p("to-smi", "SAMI", "smi"),
		p("to-lrc", "LRC", "lrc"),
		p("to-ttml", "TTML", "ttml"),
	}
}

// HwpPresets: HWP as a target, through HWPX.
func HwpPresets() []*Preset { return []*Preset{p("to-hwp", "HWP", "hwp")} }

// SpreadsheetPresets: spreadsheet targets.
func SpreadsheetPresets() []*Preset {
	return []*Preset{p("to-xlsx", "XLSX", "xlsx"), p("to-csv", "CSV", "csv")}
}

// ModelPresets: 3D model targets.
func ModelPresets() []*Preset {
	return []*Preset{
		p("to-glb", "GLB", "glb"), p("to-stl", "STL", "stl"), p("to-obj", "OBJ", "obj"),
		p("to-3mf", "3MF", "3mf"), p("to-ply", "PLY", "ply"), p("to-gltf", "glTF", "gltf"),
	}
}

// GeoPresets: geospatial vector targets.
func GeoPresets() []*Preset {
	return []*Preset{
		p("to-geojson", "GeoJSON", "geojson"), p("to-shp", "Shapefile", "shp"), p("to-kml", "KML", "kml"),
		p("to-gpx", "GPX", "gpx"), p("to-kmz", "KMZ", "kmz"),
	}
}

// CatalogPresets: images, vector, office, e-books, video, audio, archives, CAD and fonts.
func CatalogPresets() []*Preset {
	return []*Preset{
		p("to-jxl", "JPEG XL", "jxl", quality(85)),
		p("to-psd", "PSD", "psd"),
		p("to-tga", "TGA", "tga"),
		p("to-icns", "ICNS", "icns"),
		p("to-svg", "SVG", "svg"),
		p("to-eps", "EPS", "eps"),

		p("to-ods", "ODS", "ods"),
		p("to-pptx", "PPTX", "pptx"),
		p("to-odp", "ODP", "odp"),

		p("to-epub", "EPUB", "epub"),
		p("to-mobi", "MOBI", "mobi"),
		p("to-azw3", "AZW3", "azw3"),
		p("to-kepub", "KEPUB", "kepub"),
		p("to-fb2", "FB2", "fb2"),
		p("to-cbz", "CBZ", "cbz"),

		p("to-mp4", "MP4", "mp4"),
		p("mp4-720", "MP4 720p", "mp4", func(x *Preset) {
			x.Media.MaxHeight = 720
			x.Media.Quality = MediaSmall
		}, pattern("{name}_720p")),
		p("to-webm", "WEBM", "webm"),
		p("to-mov", "MOV", "mov"),
		p("to-mkv", "MKV", "mkv"),
		p("to-avi", "AVI", "avi"),

		p("to-mp3", "MP3", "mp3"),
		p("mp3-128", "MP3 128k", "mp3", func(x *Preset) { x.Media.AudioBitrateKbps = 128 }, pattern("{name}_128k")),
		p("to-m4a", "M4A", "m4a"),
		p("to-wav", "WAV", "wav"),
		p("to-flac", "FLAC", "flac"),
		p("to-ogg", "OGG", "ogg"),
		p("to-opus", "OPUS", "opus"),
		p("to-aac", "AAC", "aac"),

		p("to-zip", "ZIP", "zip"),
		p("to-7z", "7Z", "7z"),
		p("to-tgz", "TAR.GZ", "tgz"),
		p("to-tar", "TAR", "tar"),
		k("extract", "preset.extract", formats.Folder),
		k("zip-all", "preset.zip_all", "zip", func(x *Preset) { x.Archive.CombineIntoOne = true }, pattern("{name}_files")),

		p("to-dxf", "DXF", "dxf"),
		p("to-dwg", "DWG", "dwg"),

		p("to-ttf", "TTF", "ttf"),
		p("to-otf", "OTF", "otf"),
		p("to-woff", "WOFF", "woff"),
		p("to-woff2", "WOFF2", "woff2"),
		p("to-eot", "EOT", "eot"),
	}
}

// ImageToolPresets: rotate and flip presets, and the two groups that fan them out.
func ImageToolPresets() []*Preset {
	// A JPG is encoded again when it turns: at 95, so turning a good photo doesn't visibly cost it quality.
	turn := func(id, key, pat string, set func(*ImageOptions)) *Preset {
		return k(id, key, SameAsSource, quality(95), pattern(pat), func(x *Preset) { set(&x.Image) })
	}
	return []*Preset{
		turn("rotate-left", "preset.rotate_left", "{name}_left", func(i *ImageOptions) { i.Rotation = Left90 }),
		turn("rotate-right", "preset.rotate_right", "{name}_right", func(i *ImageOptions) { i.Rotation = Right90 }),
		turn("rotate-180", "preset.rotate_180", "{name}_180", func(i *ImageOptions) { i.Rotation = Rotate180 }),
		turn("flip-horizontal", "preset.flip_horizontal", "{name}_mirrored", func(i *ImageOptions) { i.Flip = FlipHorizontal }),
		turn("flip-vertical", "preset.flip_vertical", "{name}_flipped", func(i *ImageOptions) { i.Flip = FlipVertical }),
		k("rotate", "preset.group_rotate", GroupTarget, group("rotate-left", "rotate-right", "rotate-180")),
		k("flip", "preset.group_flip", GroupTarget, group("flip-horizontal", "flip-vertical")),
	}
}

// DataPresets: structured data and contacts targets.
func DataPresets() []*Preset {
	return []*Preset{
		p("to-json", "JSON", "json"), p("to-yaml", "YAML", "yaml"), p("to-toml", "TOML", "toml"),
		p("to-xml", "XML", "xml"), p("to-eml", "EML", "eml"), p("to-vcf", "vCard", "vcf"),
		p("to-ics", "iCalendar", "ics"),
	}
}
