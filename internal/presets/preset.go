// Package presets holds a preset (one slice of the donut toolbar: "convert to
// TargetFormat with these options"), its option groups and the output rule.
//
// Enumerations are strings in JSON, matched case-insensitively when read;
// an unknown value falls back to the default (see enum.go).
package presets

import (
	"crypto/rand"
	"encoding/hex"
	"math"
	"slices"

	"github.com/dseif0x/filee-go/internal/formats"
)

// Kind tells a convert slice from a group of slices or a tool.
type Kind string

const (
	KindConvert Kind = "Convert"
	KindGroup   Kind = "Group"
	KindTool    Kind = "Tool"
)

// Tools a Tool preset opens.
const (
	CropTool      = "crop"
	ClipTool      = "clip"
	ImageEditTool = "image-edit"
	HideTool      = "hide"
)

const (
	// MaxSubPresets is the most slices a group holds.
	MaxSubPresets = 6
	// GroupTarget is the TargetFormat of a group.
	GroupTarget = "group"
	// SameAsSource as TargetFormat keeps each file's own format (image tools).
	SameAsSource = "same"
)

// Preset is "convert to TargetFormat with these options".
type Preset struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	NameKey      string          `json:"nameKey,omitempty"`
	TargetFormat string          `json:"targetFormat"`
	Image        ImageOptions    `json:"image"`
	Pdf          PdfOptions      `json:"pdf"`
	Document     DocumentOptions `json:"document"`
	Media        MediaOptions    `json:"media"`
	Archive      ArchiveOptions  `json:"archive"`
	Trace        TracingOptions  `json:"trace"`
	Cad          CadOptions      `json:"cad"`
	Output       OutputRule      `json:"output"`
	Kind         Kind            `json:"kind"`
	SubPresetIDs []string        `json:"subPresetIds,omitempty"`
	// Tool names the tool of a Tool preset ("" is the crop tool).
	Tool string `json:"tool,omitempty"`
}

// New returns a convert preset to target with default options.
func New(name, target string) *Preset {
	return &Preset{
		ID: NewID(), Name: name, TargetFormat: target, Kind: KindConvert,
		Image: DefaultImageOptions(), Pdf: DefaultPdfOptions(), Media: DefaultMediaOptions(),
		Archive: ArchiveOptions{Level: ArchiveNormal}, Trace: TracingOptions{Mode: TraceColor, Colors: 16, Detail: 50},
		Output: DefaultOutputRule(),
	}
}

// NewID returns a fresh 12-character preset id.
func NewID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (p *Preset) IsGroup() bool         { return p.Kind == KindGroup }
func (p *Preset) IsTool() bool          { return p.Kind == KindTool }
func (p *Preset) IsCropTool() bool      { return p.IsTool() && (p.Tool == "" || p.Tool == CropTool) }
func (p *Preset) IsClipTool() bool      { return p.IsTool() && p.Tool == ClipTool }
func (p *Preset) IsImageEditTool() bool { return p.IsTool() && p.Tool == ImageEditTool }
func (p *Preset) IsHideTool() bool      { return p.IsTool() && p.Tool == HideTool }

// PacksIntoOneArchive: all dropped files go into one archive.
func (p *Preset) PacksIntoOneArchive() bool {
	f := formats.ByID(p.TargetFormat)
	return p.Archive.CombineIntoOne && f != nil && f.Category == formats.Archive && f.ID != formats.Folder
}

// SavesFramesOf: a video becomes one picture per frame.
func (p *Preset) SavesFramesOf(source string) bool {
	return p.Media.SaveEveryFrame && !p.MakesLivePhotoOf(source) && slices.Contains(FrameTargets, p.TargetFormat) &&
		formats.CategoryOf(source) == formats.Video
}

// ExtractsMotionOf: the video inside a motion photo is saved.
func (p *Preset) ExtractsMotionOf(source string) bool {
	return slices.Contains(MotionPhotoTargets, p.TargetFormat) && slices.Contains(MotionPhotoSources, source) &&
		(p.Media.ExtractMotionPhoto || source == "livp")
}

// MakesLivePhotoOf: a video becomes a live / motion photo.
func (p *Preset) MakesLivePhotoOf(source string) bool {
	return slices.Contains(LivePhotoTargets(p.Media.LivePhoto), p.TargetFormat) && formats.CategoryOf(source) == formats.Video
}

// RecognizesTextOf: the route goes through OCR.
func (p *Preset) RecognizesTextOf(source string) bool {
	return p.Pdf.Ocr && slices.Contains(OcrTargets, p.TargetFormat) &&
		(source == "pdf" || formats.CategoryOf(source) == formats.Image)
}

// JoinsIntoOneMedia: the dropped clips become one file.
func (p *Preset) JoinsIntoOneMedia() bool {
	return (p.Media.JoinIntoOne || p.Media.Slideshow) && slices.Contains(JoinTargets, p.TargetFormat)
}

// JoinsImages: the dropped pictures become one picture.
func (p *Preset) JoinsImages() bool {
	return p.Image.JoinIntoOne && (p.TargetFormat == SameAsSource || formats.CategoryOf(p.TargetFormat) == formats.Image)
}

// CutsClipOf: the clip editor's cut applies to a video source.
func (p *Preset) CutsClipOf(source string) bool {
	return p.Media.Clip != nil && len(p.Media.Clip.Parts) > 0 && formats.CategoryOf(source) == formats.Video
}

// WithoutPassword returns the preset with the PDF password left out (for
// saving to disk); mark records that one was set.
func (p *Preset) WithoutPassword(mark bool) *Preset {
	if p.Pdf.Password == "" {
		return p
	}
	c := p.Clone()
	c.Pdf.Password = ""
	c.Pdf.PasswordLeftOut = mark
	return c
}

// Clone returns a deep copy.
func (p *Preset) Clone() *Preset {
	c := *p
	c.SubPresetIDs = slices.Clone(p.SubPresetIDs)
	c.Image.IcoSizes = slices.Clone(p.Image.IcoSizes)
	if p.Image.HiddenAreas != nil {
		c.Image.HiddenAreas = slices.Clone(p.Image.HiddenAreas)
	}
	if p.Image.CustomSize != nil {
		s := *p.Image.CustomSize
		c.Image.CustomSize = &s
	}
	if p.Image.MinShortEdge != nil {
		v := *p.Image.MinShortEdge
		c.Image.MinShortEdge = &v
	}
	if p.Image.Dpi != nil {
		v := *p.Image.Dpi
		c.Image.Dpi = &v
	}
	c.Media.Clip = p.Media.Clip.Clone()
	c.Output.Renames = slices.Clone(p.Output.Renames)
	return &c
}

func clamp[T int | float64](v, lo, hi T) T { return max(lo, min(v, hi)) }

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// ---- images ----

type ResizeMode string

const (
	ResizeNone         ResizeMode = "None"
	ResizePercent      ResizeMode = "Percent"
	ResizeLongEdge     ResizeMode = "LongEdge"
	ResizeWidth        ResizeMode = "Width"
	ResizeCustom       ResizeMode = "Custom"
	ResizeMinShortEdge ResizeMode = "MinShortEdge"
)

type ImageRotation string

const (
	RotateNone ImageRotation = "None"
	Right90    ImageRotation = "Right90"
	Left90     ImageRotation = "Left90"
	Rotate180  ImageRotation = "Rotate180"
)

type ImageFlip string

const (
	FlipNone       ImageFlip = "None"
	FlipHorizontal ImageFlip = "Horizontal"
	FlipVertical   ImageFlip = "Vertical"
)

type ImageJoinDirection string

const (
	JoinVertical   ImageJoinDirection = "Vertical"
	JoinHorizontal ImageJoinDirection = "Horizontal"
)

type ImageJoinFit string

const (
	JoinSameSize ImageJoinFit = "SameSize"
	JoinOriginal ImageJoinFit = "Original"
)

type TiffCompression string

const (
	TiffNone TiffCompression = "None"
	TiffLzw  TiffCompression = "Lzw"
	TiffZip  TiffCompression = "Zip"
)

type FitsTiffMode string

const (
	FitsLinear    FitsTiffMode = "Linear"
	FitsStretched FitsTiffMode = "Stretched"
)

type HideStyle string

const (
	HidePixelate HideStyle = "Pixelate"
	HideBlur     HideStyle = "Blur"
)

// HiddenArea is a rectangle to pixelate or blur, in fractions of the picture (0–1).
type HiddenArea struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

const (
	MaxJoinGap          = 1000
	TransparentJoin     = "transparent"
	DefaultHideStrength = 50
)

// AnimationSpeeds are the speeds the UI offers for animated pictures.
var AnimationSpeeds = []float64{0.25, 0.5, 0.75, 1, 1.5, 2, 4}

type ImageOptions struct {
	Quality                int                `json:"quality"`
	MaxSizeMb              float64            `json:"maxSizeMb,omitempty"`
	Resize                 ResizeMode         `json:"resize"`
	Rotation               ImageRotation      `json:"rotation,omitempty"`
	Flip                   ImageFlip          `json:"flip,omitempty"`
	ResizePercent          int                `json:"resizePercent"`
	LongEdge               int                `json:"longEdge"`
	Width                  int                `json:"width"`
	CustomSize             *CustomImageSize   `json:"customSize,omitempty"`
	MinShortEdge           *int               `json:"minShortEdge,omitempty"`
	Dpi                    *int               `json:"dpi,omitempty"`
	KeepMetadata           bool               `json:"keepMetadata"`
	KeepColorProfile       bool               `json:"keepColorProfile"`
	MaxCompression         bool               `json:"maxCompression,omitempty"`
	KeepSourceQuality      bool               `json:"keepSourceQuality,omitempty"`
	Grayscale              bool               `json:"grayscale,omitempty"`
	TrimTransparent        bool               `json:"trimTransparent,omitempty"`
	RemoveBackground       bool               `json:"removeBackground,omitempty"`
	BackgroundTolerance    int                `json:"backgroundTolerance"`
	KeepEnclosedBackground bool               `json:"keepEnclosedBackground,omitempty"`
	Background             string             `json:"background"`
	TiffCompression        TiffCompression    `json:"tiffCompression"`
	MultiPageTiff          bool               `json:"multiPageTiff,omitempty"`
	WebpLossless           bool               `json:"webpLossless,omitempty"`
	JoinIntoOne            bool               `json:"joinIntoOne,omitempty"`
	JoinDirection          ImageJoinDirection `json:"joinDirection,omitempty"`
	JoinFit                ImageJoinFit       `json:"joinFit,omitempty"`
	JoinGap                int                `json:"joinGap,omitempty"`
	JoinBackground         string             `json:"joinBackground"`
	FitsTiff               FitsTiffMode       `json:"fitsTiff,omitempty"`
	FitsDebayer            bool               `json:"fitsDebayer"`
	IcoSizes               []int              `json:"icoSizes"`
	HideStyle              HideStyle          `json:"hideStyle,omitempty"`
	HideStrength           int                `json:"hideStrength"`
	HiddenAreas            []HiddenArea       `json:"hiddenAreas,omitempty"`
	AnimationSpeed         float64            `json:"animationSpeed"`
}

// DefaultImageOptions are the options of a new preset.
func DefaultImageOptions() ImageOptions {
	return ImageOptions{
		Quality: 90, Resize: ResizeNone, Rotation: RotateNone, Flip: FlipNone, ResizePercent: 50, LongEdge: 1920, Width: 1200,
		KeepMetadata: true, KeepColorProfile: true, BackgroundTolerance: 25, Background: "#FFFFFF",
		TiffCompression: TiffLzw, JoinDirection: JoinVertical, JoinFit: JoinSameSize, JoinBackground: "#FFFFFF",
		FitsTiff: FitsLinear, FitsDebayer: true, IcoSizes: []int{256, 128, 64, 48, 32, 16},
		HideStyle: HidePixelate, HideStrength: DefaultHideStrength, AnimationSpeed: 1,
	}
}

func (o *ImageOptions) TurnsOrMirrors() bool {
	return (o.Rotation != "" && o.Rotation != RotateNone) || (o.Flip != "" && o.Flip != FlipNone)
}
func (o *ImageOptions) HidesAreas() bool    { return len(o.HiddenAreas) > 0 }
func (o *ImageOptions) ReworksPixels() bool { return o.TurnsOrMirrors() || o.HidesAreas() }
func (o *ImageOptions) EditsPicture() bool {
	return (o.Resize != "" && o.Resize != ResizeNone) || o.Grayscale || o.TrimTransparent || o.RemoveBackground ||
		o.MaxCompression || o.ChangesSpeed() || o.CustomSize != nil || o.MinShortEdge != nil
}
func (o *ImageOptions) JoinSpacing() int { return clamp(o.JoinGap, 0, MaxJoinGap) }

// ClampedMinShortEdge is MinShortEdge within the allowed sizes, or 0 when unset.
func (o *ImageOptions) ClampedMinShortEdge() int {
	if o.MinShortEdge == nil || *o.MinShortEdge <= 0 {
		return 0
	}
	return clamp(*o.MinShortEdge, MinSide, MaxSide)
}

// PlaybackSpeed is AnimationSpeed made safe (0.1–10, 1 when unset).
func (o *ImageOptions) PlaybackSpeed() float64 {
	if !finite(o.AnimationSpeed) || o.AnimationSpeed <= 0 {
		return 1
	}
	return clamp(o.AnimationSpeed, 0.1, 10)
}
func (o *ImageOptions) ChangesSpeed() bool { return o.PlaybackSpeed() != 1 }

const (
	MinSide = 16
	MaxSide = 16384
)

type CustomImageSize struct {
	Width      int  `json:"width"`
	Height     int  `json:"height"`
	LockAspect bool `json:"lockAspect,omitempty"`
}

func (s *CustomImageSize) ClampedWidth() int  { return clamp(s.Width, MinSide, MaxSide) }
func (s *CustomImageSize) ClampedHeight() int { return clamp(s.Height, MinSide, MaxSide) }

// ---- PDF ----

type PdfPageSize string

const (
	PageFitImage PdfPageSize = "FitImage"
	PageA4       PdfPageSize = "A4"
	PageLetter   PdfPageSize = "Letter"
)

type PdfCompression string

const (
	PdfCompressNone   PdfCompression = "None"
	PdfCompressScreen PdfCompression = "Screen"
	PdfCompressEbook  PdfCompression = "Ebook"
	PdfCompressPrint  PdfCompression = "Print"
)

type PdfRotation string

const (
	PdfRotateNone PdfRotation = "None"
	PdfLeft90     PdfRotation = "Left90"
	PdfRight90    PdfRotation = "Right90"
	PdfRotate180  PdfRotation = "Rotate180"
)

// OcrTargets are the targets text recognition can produce.
var OcrTargets = []string{"pdf", "docx", "txt", "md", "hwpx"}

type PdfOptions struct {
	MergeIntoSingle         bool           `json:"mergeIntoSingle,omitempty"`
	PageSize                PdfPageSize    `json:"pageSize"`
	MarginMm                float64        `json:"marginMm,omitempty"`
	RenderDpi               int            `json:"renderDpi"`
	PageRange               string         `json:"pageRange,omitempty"`
	SplitPages              bool           `json:"splitPages,omitempty"`
	Rotation                PdfRotation    `json:"rotation,omitempty"`
	CombinePages            bool           `json:"combinePages,omitempty"`
	TrimPageMargins         bool           `json:"trimPageMargins,omitempty"`
	SplitTallImage          bool           `json:"splitTallImage,omitempty"`
	Compression             PdfCompression `json:"compression,omitempty"`
	RemoveMetadata          bool           `json:"removeMetadata,omitempty"`
	CompressWithGhostscript bool           `json:"compressWithGhostscript,omitempty"`
	Watermark               string         `json:"watermark,omitempty"`
	PageNumbers             bool           `json:"pageNumbers,omitempty"`
	Password                string         `json:"password,omitempty"`
	PasswordLeftOut         bool           `json:"passwordLeftOut,omitempty"`
	Ocr                     bool           `json:"ocr,omitempty"`
	OcrLanguage             string         `json:"ocrLanguage"`
}

func DefaultPdfOptions() PdfOptions {
	return PdfOptions{PageSize: PageFitImage, RenderDpi: 150, Rotation: PdfRotateNone, Compression: PdfCompressNone, OcrLanguage: "eng"}
}

// FinishesPdf: watermark, page numbers or a password are added after writing.
func (o *PdfOptions) FinishesPdf() bool {
	return trimmed(o.Watermark) != "" || o.PageNumbers || o.Password != "" || o.PasswordLeftOut
}

type DocumentOptions struct {
	PdfA             bool `json:"pdfA,omitempty"`
	LeaveOutPictures bool `json:"leaveOutPictures,omitempty"`
}

// ---- media ----

type MediaQuality string

const (
	MediaHigh     MediaQuality = "High"
	MediaBalanced MediaQuality = "Balanced"
	MediaSmall    MediaQuality = "Small"
)

type LivePhotoKind string

const (
	LivePhotoNone    LivePhotoKind = "None"
	LivePhotoApple   LivePhotoKind = "Apple"
	LivePhotoAndroid LivePhotoKind = "Android"
)

type VideoCodec string

const (
	VideoDefault VideoCodec = "Default"
	VideoHevc    VideoCodec = "Hevc"
	VideoAv1     VideoCodec = "Av1"
)

type AudioCodec string

const (
	AudioDefault AudioCodec = "Default"
	AudioAlac    AudioCodec = "Alac"
)

type WavSampleFormat string

const (
	Pcm16   WavSampleFormat = "Pcm16"
	Pcm8    WavSampleFormat = "Pcm8"
	Pcm24   WavSampleFormat = "Pcm24"
	Pcm32   WavSampleFormat = "Pcm32"
	Float32 WavSampleFormat = "Float32"
)

var (
	FrameTargets       = []string{"png", "jpg"}
	MotionPhotoTargets = []string{"mp4", "mov"}
	MotionPhotoSources = []string{"jpg", "heic", "avif", "livp"}
	JoinTargets        = []string{"mp4", "m4a"}
)

// LivePhotoTargets are the photo formats a live photo of kind is written as.
func LivePhotoTargets(kind LivePhotoKind) []string {
	switch kind {
	case LivePhotoApple:
		return []string{"jpg", "heic"}
	case LivePhotoAndroid:
		return []string{"jpg"}
	}
	return nil
}

const (
	MinLivePhotoLength = 0.5
	MaxLivePhotoLength = 5
	MaxFrames          = 100_000
	MaxSlides          = 300
	MinSlideSeconds    = 0.5
	MaxSlideSeconds    = 60
	CrossfadeSeconds   = 1
)

type MediaOptions struct {
	Quality               MediaQuality    `json:"quality"`
	MaxHeight             int             `json:"maxHeight,omitempty"`
	MaxSizeMb             float64         `json:"maxSizeMb,omitempty"`
	AudioBitrateKbps      int             `json:"audioBitrateKbps,omitempty"`
	RemoveAudio           bool            `json:"removeAudio,omitempty"`
	AudioChannels         int             `json:"audioChannels,omitempty"`
	AudioSampleRate       int             `json:"audioSampleRate,omitempty"`
	WavFormat             WavSampleFormat `json:"wavFormat"`
	VideoCodec            VideoCodec      `json:"videoCodec"`
	AudioCodec            AudioCodec      `json:"audioCodec"`
	HardwareEncoding      bool            `json:"hardwareEncoding,omitempty"`
	SaveEveryFrame        bool            `json:"saveEveryFrame,omitempty"`
	ExtractMotionPhoto    bool            `json:"extractMotionPhoto,omitempty"`
	LivePhoto             LivePhotoKind   `json:"livePhoto,omitempty"`
	LivePhotoStart        float64         `json:"livePhotoStart,omitempty"`
	LivePhotoLength       float64         `json:"livePhotoLength"`
	LivePhotoCover        float64         `json:"livePhotoCover"`
	FrameInterval         int             `json:"frameInterval"`
	ExactCut              bool            `json:"exactCut,omitempty"`
	Clip                  *ClipEdit       `json:"clip,omitempty"`
	JoinIntoOne           bool            `json:"joinIntoOne,omitempty"`
	Slideshow             bool            `json:"slideshow,omitempty"`
	SlideSeconds          float64         `json:"slideSeconds"`
	Crossfade             bool            `json:"crossfade"`
	CopyAudioWhenPossible bool            `json:"copyAudioWhenPossible,omitempty"`
	RemoveMetadata        bool            `json:"removeMetadata,omitempty"`
}

func DefaultMediaOptions() MediaOptions {
	return MediaOptions{
		Quality: MediaBalanced, WavFormat: Pcm16, VideoCodec: VideoDefault, AudioCodec: AudioDefault, LivePhoto: LivePhotoNone,
		LivePhotoLength: 3, LivePhotoCover: 1.5, FrameInterval: 1, SlideSeconds: 3, Crossfade: true,
	}
}

// SlideDuration is SlideSeconds made safe.
func (o *MediaOptions) SlideDuration() float64 {
	if !finite(o.SlideSeconds) {
		return 3
	}
	return clamp(o.SlideSeconds, MinSlideSeconds, MaxSlideSeconds)
}

// FadeDuration is the crossfade between slides (0 without).
func (o *MediaOptions) FadeDuration() float64 {
	if !o.Crossfade {
		return 0
	}
	return min(CrossfadeSeconds, o.SlideDuration()/2)
}

// VideoCodecsFor lists the codecs the UI offers for a video target.
func VideoCodecsFor(target string) []VideoCodec {
	switch target {
	case "mp4", "m4v", "mkv":
		return []VideoCodec{VideoDefault, VideoHevc, VideoAv1}
	case "mov":
		return []VideoCodec{VideoDefault, VideoHevc}
	case "webm":
		return []VideoCodec{VideoDefault, VideoAv1}
	}
	return []VideoCodec{VideoDefault}
}

// AudioCodecsFor lists the codecs the UI offers for an audio target.
func AudioCodecsFor(target string) []AudioCodec {
	if target == "m4a" || target == "m4b" {
		return []AudioCodec{AudioDefault, AudioAlac}
	}
	return []AudioCodec{AudioDefault}
}

// EffectiveVideoCodec is codec when target takes it, else the default.
func EffectiveVideoCodec(target string, codec VideoCodec) VideoCodec {
	if slices.Contains(VideoCodecsFor(target), codec) {
		return codec
	}
	return VideoDefault
}

// EffectiveAudioCodec is codec when target takes it, else the default.
func EffectiveAudioCodec(target string, codec AudioCodec) AudioCodec {
	if slices.Contains(AudioCodecsFor(target), codec) {
		return codec
	}
	return AudioDefault
}

// H264Targets are the containers the default codec writes as H.264.
var H264Targets = []string{"mp4", "m4v", "mov", "mkv", "flv", "ts", "m2ts", "3gp", "3g2"}

// TakesHardwareEncoding: a hardware encoder can write target with codec.
func TakesHardwareEncoding(target string, codec VideoCodec) bool {
	switch EffectiveVideoCodec(target, codec) {
	case VideoHevc, VideoAv1:
		return true
	}
	return slices.Contains(H264Targets, target)
}

// ---- archives, tracing, CAD ----

type ArchiveLevel string

const (
	ArchiveStore   ArchiveLevel = "Store"
	ArchiveFast    ArchiveLevel = "Fast"
	ArchiveNormal  ArchiveLevel = "Normal"
	ArchiveMaximum ArchiveLevel = "Maximum"
)

type ArchiveOptions struct {
	CombineIntoOne bool         `json:"combineIntoOne,omitempty"`
	Level          ArchiveLevel `json:"level"`
}

type TracingMode string

const (
	TraceColor         TracingMode = "Color"
	TraceBlackAndWhite TracingMode = "BlackAndWhite"
)

const (
	MinTraceColors = 2
	MaxTraceColors = 64
)

type TracingOptions struct {
	Mode   TracingMode `json:"mode"`
	Colors int         `json:"colors"`
	// Detail is 0–100.
	Detail int `json:"detail"`
}

type CadVersion string

const (
	AutoCad2018         CadVersion = "AutoCad2018"
	AutoCad2013         CadVersion = "AutoCad2013"
	AutoCad2010         CadVersion = "AutoCad2010"
	AutoCad2007         CadVersion = "AutoCad2007"
	AutoCad2004         CadVersion = "AutoCad2004"
	AutoCad2000         CadVersion = "AutoCad2000"
	SameAsSourceVersion CadVersion = "SameAsSource"
)

type CadColors string

const (
	CadAsDrawn       CadColors = "AsDrawn"
	CadBlackAndWhite CadColors = "BlackAndWhite"
	CadGrayscale     CadColors = "Grayscale"
)

type CadOptions struct {
	Version CadVersion `json:"version,omitempty"`
	Colors  CadColors  `json:"colors,omitempty"`
}
