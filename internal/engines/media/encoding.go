package media

// Which container and codecs Filee writes for every video and audio format,
// and the FFmpeg command lines for them. Pure (no processes), so the choices
// are unit tested without FFmpeg.

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/presets"
)

// Target is how one format is written: the FFmpeg muxer (always given: some
// extensions are ambiguous), video encoder ("" for audio) and audio encoder.
type Target struct {
	Muxer, VideoCodec, AudioCodec string
}

// Pass is one run of ffmpeg; the weights of a conversion's passes add up to 1.
type Pass struct {
	Args   []string
	Weight float64
}

// VideoTargets are the video formats Filee writes: H.264 + AAC wherever the
// container allows it, the codecs their players expect elsewhere.
var VideoTargets = map[string]Target{
	"mp4": {"mp4", "libx264", "aac"},
	// Apple's M4V is MP4 (FFmpeg's "m4v" muxer would write a raw MPEG-4 part 2 stream).
	"m4v":  {"mp4", "libx264", "aac"},
	"mov":  {"mov", "libx264", "aac"},
	"mkv":  {"matroska", "libx264", "aac"},
	"flv":  {"flv", "libx264", "aac"},
	"ts":   {"mpegts", "libx264", "aac"},
	"m2ts": {"mpegts", "libx264", "aac"},
	"3gp":  {"3gp", "libx264", "aac"},
	"3g2":  {"3g2", "libx264", "aac"},
	// The WebM specification allows only VP8/VP9/AV1 with Vorbis/Opus.
	"webm": {"webm", "libvpx-vp9", "libopus"},
	// MPEG-4 part 2 tagged XVID + CBR MP3: what DVD players and old TVs read in AVI.
	"avi": {"avi", "mpeg4", "libmp3lame"},
	"wmv": {"asf", "wmv2", "wmav2"},
	// MPEG-2 program stream ("vob" is FFmpeg's plain MPEG-2 PS muxer).
	"mpg": {"vob", "mpeg2video", "mp2"},
	// DVD-Video compliant: 720×480 / 720×576, AC-3, DVD mux rate.
	"vob": {"dvd", "mpeg2video", "ac3"},
	"ogv": {"ogg", "libtheora", "libvorbis"},
	"dv":  {"dv", "dvvideo", "pcm_s16le"},
	// MXF OP1a with long-GOP MPEG-2 (XDCAM style) and 48 kHz PCM.
	"mxf": {"mxf", "mpeg2video", "pcm_s16le"},
}

// AudioTargets are the audio formats Filee writes (also the targets of audio extraction).
var AudioTargets = map[string]Target{
	"mp3": {"mp3", "", "libmp3lame"},
	// The iPod muxer writes the M4A/M4B brands iTunes and phones expect.
	"m4a":  {"ipod", "", "aac"},
	"m4b":  {"ipod", "", "aac"},
	"aac":  {"adts", "", "aac"},
	"wav":  {"wav", "", "pcm_s16le"},
	"flac": {"flac", "", "flac"},
	"ogg":  {"ogg", "", "libvorbis"},
	"opus": {"opus", "", "libopus"},
	"wma":  {"asf", "", "wmav2"},
	"aiff": {"aiff", "", "pcm_s16be"},
	"ac3":  {"ac3", "", "ac3"},
	"amr":  {"amr", "", "libopencore_amrnb"},
	"au":   {"au", "", "pcm_s16be"},
	"caf":  {"caf", "", "pcm_s16be"},
	"weba": {"webm", "", "libopus"},
	"mka":  {"matroska", "", "libopus"},
	"mp2":  {"mp2", "", "mp2"},
	"voc":  {"voc", "", "pcm_s16le"},
	"wv":   {"wv", "", "wavpack"},
	"tta":  {"tta", "", "tta"},
	"w64":  {"w64", "", "pcm_s16le"},
	"eac3": {"eac3", "", "eac3"},
}

// SubtitleTargets are the formats a video's first text subtitle track can be
// taken out as: muxer and encoder.
var SubtitleTargets = map[string][2]string{
	"srt": {"srt", "srt"},
	"ass": {"ass", "ass"},
	"vtt": {"webvtt", "webvtt"},
}

// Subtitle codecs that are text; Blu-ray PGS, DVD VobSub and DVB subtitles are pictures.
var textSubtitleCodecs = setOf("subrip", "srt", "ass", "ssa", "webvtt", "mov_text", "text", "microdvd", "sami", "realtext",
	"subviewer", "subviewer1", "jacosub", "mpl2", "pjs", "vplayer", "stl", "eia_608", "ttml")

// PictureToVideoTargets are the formats an animated GIF or a still picture can become.
var PictureToVideoTargets = []string{"mp4", "webm", "mov"}

// StillSeconds is how long a video made of one still picture shows it.
const StillSeconds = 5

// The largest picture H.264 and VP9 at level 6.2 allow (about 35.6 MP); larger
// photos are scaled down to it, or many players refuse the video.
const maxVideoPixels = 8192 * 4352

// The longest side VP9 level 6.2 allows, for panoramas within the pixel limit.
const maxVideoSide = 16384

// Audio targets that can carry a cover picture copied from the source.
var coverTargets = setOf("mp3", "m4a", "m4b", "flac")

// Frame rates MPEG-2 (and MXF) signal exactly.
var broadcastRates = [][2]int{{24000, 1001}, {24, 1}, {25, 1}, {30000, 1001}, {30, 1}, {50, 1}, {60000, 1001}, {60, 1}}

// Container metadata that describes the source file, not the content: not copied.
var sourceOnlyTags = []string{"major_brand", "minor_version", "compatible_brands"}

// Common are the global arguments of every run: no banner, no keyboard input,
// only errors on stderr, overwrite.
var Common = []string{"-hide_banner", "-nostdin", "-nostats", "-v", "error", "-y"}

func common() []string { return slices.Clone(Common) }

// ErrNoVideo is a file without a video stream where one is needed.
var ErrNoVideo = errors.New("The file has no video stream.")

// ClipRange is a stretch of a video in source seconds.
type ClipRange struct{ Start, End float64 }

// ffTime is a time as -ss and -to read it: seconds with three decimals.
func ffTime(seconds float64) string {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		seconds = 0
	}
	return strconv.FormatFloat(max(0, seconds), 'f', 3, 64)
}

// num is a number with up to three decimals ("0.###").
func num(v float64) string {
	r := math.RoundToEven(v*1000) / 1000
	if r == 0 {
		r = 0 // no "-0"
	}
	return strconv.FormatFloat(r, 'f', -1, 64)
}

// secs is a non-negative number for FFmpeg's filters.
func secs(v float64) string { return num(max(0, v)) }

func itoa(v int) string { return strconv.Itoa(v) }

func setOf(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, i := range items {
		m[i] = true
	}
	return m
}

func choose[T any](q presets.MediaQuality, high, balanced, small T) T {
	switch q {
	case presets.MediaHigh:
		return high
	case presets.MediaSmall:
		return small
	}
	return balanced
}

// PlanOptions are the optional inputs of Plan.
type PlanOptions struct {
	// Cover is the source's cover for targets FFmpeg can't put a picture stream into.
	Cover *CoverExtras
	// Encoders the FFmpeg in use has; nil assumes the usual build's.
	Encoders map[string]bool
	// Hardware is the encoder that passed its probe; nil encodes in software.
	Hardware *HardwareEncoder
	// AnimationSpeed of a GIF or APNG (0 or 1 keeps the video's).
	AnimationSpeed float64
	// Clip: a video target with only this part, cut exactly (always encoded).
	Clip *ClipRange
}

// Plan returns the ffmpeg runs that convert input into output as target.
func Plan(input, output, target string, o *presets.MediaOptions, info *MediaInfo, workDir string, p PlanOptions) ([]Pass, error) {
	speed := p.AnimationSpeed
	if speed == 0 {
		speed = 1
	}
	if p.Clip != nil {
		if spec, ok := VideoTargets[target]; ok {
			args, err := videoArgs(input, output, target, spec, o, info, p.Encoders, p.Hardware, p.Clip)
			return one(args, err)
		}
	}
	switch target {
	case "gif":
		return animatedGif(input, output, o, info, workDir, speed)
	case "apng":
		return one(animatedPng(input, output, o, info, speed))
	case "png":
		return one(posterFrame(input, output, info))
	}
	if spec, ok := VideoTargets[target]; ok {
		return one(videoArgs(input, output, target, spec, o, info, p.Encoders, p.Hardware, nil))
	}
	if spec, ok := AudioTargets[target]; ok {
		return one(audioArgs(input, output, target, spec, o, info, p.Cover))
	}
	if _, ok := SubtitleTargets[target]; ok {
		return one(subtitleArgs(input, output, target, info))
	}
	return nil, fmt.Errorf("FFmpeg cannot write '%s'.", target)
}

func one(args []string, err error) ([]Pass, error) {
	if err != nil {
		return nil, err
	}
	return []Pass{{args, 1}}, nil
}

// QualityToQv maps image quality (1..100) to mjpeg's -q:v (1..31, 1 best).
func QualityToQv(quality int) int {
	q := min(max(quality, 1), 100)
	return min(max(int(math.RoundToEven(2.0+float64(100-q)*29.0/99.0)), 1), 31)
}

// FrameSequence writes every frame (or every intervalth) of the first video
// stream as numbered pictures in pattern (an image2 pattern). Frames keep
// their own timestamps, so a variable frame rate video gets no duplicates.
func FrameSequence(input, pattern, ext string, video *VideoStream, quality, interval int) []string {
	sel := "null"
	if interval > 1 {
		sel = fmt.Sprintf(`select='not(mod(n\,%d))'`, interval)
	}
	sq := ""
	if video != nil {
		sq = SquarePixels(video)
	}
	mode := "passthrough"
	if interval > 1 {
		mode = "vfr"
	}
	args := append(common(), "-i", input, "-map", "0:V:0", "-vf", sq+sel, "-fps_mode", mode)
	if ext == "jpg" || ext == "jpeg" {
		args = append(args, "-c:v", "mjpeg", "-q:v", itoa(QualityToQv(quality)))
	} else {
		args = append(args, "-c:v", "png")
	}
	return append(args, "-f", "image2", "-progress", "pipe:1", pattern)
}

// ───────────────────────── Video ─────────────────────────

// HardwareCodec is the codec a hardware encoder would write for this file
// ("h264", "hevc", "av1"), or "" when the preset doesn't ask for hardware
// encoding, the target uses another codec, or the video is copied anyway.
// encode: the video is encoded even where it could be copied (an exact clip).
func HardwareCodec(target string, o *presets.MediaOptions, info *MediaInfo, encode bool) string {
	if !o.HardwareEncoding || info.Video == nil {
		return ""
	}
	if _, ok := VideoTargets[target]; !ok || !presets.TakesHardwareEncoding(target, o.VideoCodec) {
		return ""
	}
	if !encode && !info.RawStream && CanCopyVideo(target, info.Video, o) {
		return ""
	}
	switch presets.EffectiveVideoCodec(target, o.VideoCodec) {
	case presets.VideoHevc:
		return "hevc"
	case presets.VideoAv1:
		return "av1"
	}
	return "h264"
}

// Containers that hold a single audio track: the first one is kept.
var singleAudioTargets = setOf("flv", "dv")

func appleHevc(codec, target string) bool {
	return codec == "hevc" && (target == "mp4" || target == "m4v" || target == "mov")
}

func videoArgs(input, output, target string, spec Target, o *presets.MediaOptions, info *MediaInfo, encoders map[string]bool,
	hw *HardwareEncoder, clip *ClipRange) ([]string, error) {
	video := info.Video
	if video == nil {
		return nil, ErrNoVideo
	}
	// Every track is kept (languages, commentary), except in containers that
	// hold one and in a clip, which keeps the first one.
	tracks := info.AudioTracks
	switch {
	case o.RemoveAudio:
		tracks = nil
	case clip != nil || singleAudioTargets[target]:
		tracks = tracks[:min(1, len(tracks))]
	}

	args := common()
	if clip != nil {
		args = append(args, ClipInput(*clip)...)
	}
	args = append(args, "-i", input, "-map", "0:V:0")
	for i := range tracks {
		args = append(args, "-map", fmt.Sprintf("0:a:%d", i))
	}

	switch {
	case clip == nil && !info.RawStream && CanCopyVideo(target, video, o):
		args = append(args, "-c:v", "copy")
		// Apple plays HEVC in MP4/MOV only under the "hvc1" tag.
		if appleHevc(video.Codec, target) {
			args = append(args, "-tag:v", "hvc1")
		}
	case hw != nil && HardwareCodec(target, o, info, clip != nil) == hw.Codec():
		// VA-API's device is a global option, so it goes before the input.
		args = slices.Insert(args, len(Common), HardwareInputArgs(hw)...)
		args = append(args, "-vf", ScaleFilter(o.MaxHeight)+HardwareFilterSuffix(hw, TenBit(hw.Codec(), video)))
		args = append(args, HardwareCodecArgs(hw, target, o.Quality, video, o.MaxHeight)...)
	default:
		pal := IsPal(video.FrameRate)
		if target == "vob" || target == "dv" {
			args = append(args, "-vf", DvdOrDvFilter(video, pal))
		} else {
			args = append(args, "-vf", ScaleFilter(o.MaxHeight))
		}
		enc, err := VideoEncoder(target, spec.VideoCodec, o.VideoCodec, encoders)
		if err != nil {
			return nil, err
		}
		vc, err := VideoCodecArgs(target, enc, o.Quality, video, pal)
		if err != nil {
			return nil, err
		}
		args = append(args, vc...)
	}

	if len(tracks) == 0 {
		args = append(args, "-an")
	}
	for i := range tracks {
		var ta []string
		if clip == nil && CanCopyAudio(target, &tracks[i], o) {
			ta = []string{"-c:a", "copy"}
		} else {
			ta = audioCodecArgs(target, spec.AudioCodec, o, &tracks[i], true)
		}
		if len(tracks) > 1 {
			ta = ForAudioTrack(ta, i)
		}
		args = append(args, ta...)
	}
	args = append(args, MetadataArgs(0)...)
	args = append(args, ContainerArgs(target)...)
	// Sparse or late-starting streams otherwise fail with "too many packets buffered".
	return append(args, "-max_muxing_queue_size", "4096", "-f", spec.Muxer, "-progress", "pipe:1", output), nil
}

// ForAudioTrack addresses audio options to one output track (-b:a → -b:a:2, -ac → -ac:a:2).
func ForAudioTrack(args []string, track int) []string {
	out := make([]string, len(args))
	for i, a := range args {
		switch {
		case i > 0 && strings.HasPrefix(args[i-1], "-") && !strings.HasPrefix(a, "-"):
			out[i] = a // values are never options
		case a == "-c:a" || a == "-b:a" || a == "-q:a":
			out[i] = fmt.Sprintf("%s:%d", a, track)
		case a == "-ac" || a == "-ar":
			out[i] = fmt.Sprintf("%s:a:%d", a, track)
		default:
			out[i] = a
		}
	}
	return out
}

// ClipCopy copies a part of a video as it is (-c copy): done in a moment and
// lossless, but starting at the key frame before the start. The first video
// and audio streams are kept.
func ClipCopy(input, output, target string, clip ClipRange, o *presets.MediaOptions, info *MediaInfo) ([]string, error) {
	video := info.Video
	if video == nil {
		return nil, ErrNoVideo
	}
	spec, ok := VideoTargets[target]
	if !ok {
		return nil, fmt.Errorf("FFmpeg cannot write '%s'.", target)
	}
	audio := !o.RemoveAudio && info.Audio != nil
	args := append(common(), ClipInput(clip)...)
	args = append(args, "-i", input, "-map", "0:V:0")
	if audio {
		args = append(args, "-map", "0:a:0")
	}
	args = append(args, "-c", "copy")
	if appleHevc(video.Codec, target) {
		args = append(args, "-tag:v", "hvc1")
	}
	if !audio {
		args = append(args, "-an")
	}
	// The copied packets keep their timestamps from the key frame on: shifted to start at 0.
	args = append(args, "-avoid_negative_ts", "make_zero")
	args = append(args, MetadataArgs(0)...)
	args = append(args, ContainerArgs(target)...)
	return append(args, "-max_muxing_queue_size", "4096", "-f", spec.Muxer, "-progress", "pipe:1", output), nil
}

// ClipInput reads only clip of the file: -ss and -to before -i.
func ClipInput(c ClipRange) []string { return []string{"-ss", ffTime(c.Start), "-to", ffTime(c.End)} }

// FrameAt writes one frame at seconds as a JPEG at most maxHeight high
// (square pixels, upright): the clip window's timeline and preview.
func FrameAt(input string, seconds float64, output string, maxHeight int, video *VideoStream) []string {
	sq := ""
	if video != nil {
		sq = SquarePixels(video)
	}
	return append(common(), "-ss", ffTime(seconds), "-i", input, "-map", "0:V:0",
		"-vf", sq+fmt.Sprintf("scale=w=-2:h='trunc(min(ih,%d)/2)*2'", max(16, maxHeight)),
		"-frames:v", "1", "-update", "1", "-c:v", "mjpeg", "-q:v", "3", "-f", "image2", output)
}

type codecSets struct{ video, audio map[string]bool }

// Remuxable are the codecs a container takes as they are: a video whose
// streams are listed is only rewrapped (no quality loss, HDR kept).
var Remuxable = map[string]codecSets{
	"mp4":  {setOf("h264", "hevc"), setOf("aac", "alac", "ac3", "eac3", "mp3")},
	"m4v":  {setOf("h264", "hevc"), setOf("aac", "alac", "ac3", "eac3", "mp3")},
	"mov":  {setOf("h264", "hevc", "prores"), setOf("aac", "alac", "ac3", "eac3", "mp3", "pcm_s16le", "pcm_s24le")},
	"mkv":  {setOf("h264", "hevc", "vp9", "av1"), setOf("aac", "alac", "ac3", "eac3", "mp3", "opus", "vorbis", "flac")},
	"webm": {setOf("vp8", "vp9", "av1"), setOf("opus", "vorbis")},
	"ts":   {setOf("h264", "hevc"), setOf("aac", "ac3", "eac3", "mp3")},
	"m2ts": {setOf("h264", "hevc"), setOf("aac", "ac3", "eac3")},
}

// Pixel formats players decode in hardware.
var playablePixelFormats = setOf("yuv420p", "yuvj420p", "yuv420p10le")

// CanCopyVideo: the target takes the video stream's codec in a playable pixel
// format, it needs no scaling, and the preset doesn't ask for a small file
// (or a size limit). A preset asking for HEVC or AV1 copies only that codec.
func CanCopyVideo(target string, v *VideoStream, o *presets.MediaOptions) bool {
	if o.Quality == presets.MediaSmall || o.MaxSizeMb > 0 || (o.MaxHeight > 0 && v.DisplayHeight() > max(16, o.MaxHeight)) || v.Codec == "" {
		return false
	}
	codecs, ok := Remuxable[target]
	if !ok {
		return false
	}
	var takes bool
	switch presets.EffectiveVideoCodec(target, o.VideoCodec) {
	case presets.VideoHevc:
		takes = v.Codec == "hevc"
	case presets.VideoAv1:
		takes = v.Codec == "av1"
	default:
		takes = codecs.video[v.Codec]
	}
	return takes && (v.Codec == "prores" || playablePixelFormats[v.PixelFormat])
}

// VideoEncoder is the encoder for a video that has to be encoded: the
// preset's codec where the target takes it (x265 for HEVC; SVT-AV1, else
// libaom for AV1), else the target's usual one.
func VideoEncoder(target, def string, codec presets.VideoCodec, encoders map[string]bool) (string, error) {
	has := func(e string) bool { return encoders == nil || encoders[e] }
	switch presets.EffectiveVideoCodec(target, codec) {
	case presets.VideoHevc:
		if has("libx265") {
			return "libx265", nil
		}
		return "", errors.New("This FFmpeg has no HEVC encoder (libx265). Filee's FFmpeg download has one.")
	case presets.VideoAv1:
		if has("libsvtav1") {
			return "libsvtav1", nil
		}
		if has("libaom-av1") {
			return "libaom-av1", nil
		}
		return "", errors.New("This FFmpeg has no AV1 encoder (libsvtav1 or libaom-av1). Filee's FFmpeg download has one.")
	}
	return def, nil
}

// CanCopyAudio: the container takes the audio stream, no bitrate is set and
// the channels stay as they are.
func CanCopyAudio(target string, a *AudioStream, o *presets.MediaOptions) bool {
	if o.Quality == presets.MediaSmall || o.AudioBitrateKbps > 0 {
		return false
	}
	if (o.AudioChannels == 1 || o.AudioChannels == 2) && o.AudioChannels != a.Channels {
		return false
	}
	codecs, ok := Remuxable[target]
	return ok && a.Codec != "" && codecs.audio[a.Codec]
}

// ScaleFilter scales down to maxHeight (never up) keeping the aspect ratio,
// with even sides, which 4:2:0 encoders require.
func ScaleFilter(maxHeight int) string {
	if maxHeight > 0 {
		return fmt.Sprintf("scale=w=-2:h='trunc(min(ih,%d)/2)*2'", max(16, maxHeight))
	}
	return "scale=w='trunc(iw/2)*2':h='trunc(ih/2)*2'"
}

// DvdOrDvFilter fits the picture into 720×480 (NTSC) or 720×576 (PAL) at 4:3
// or 16:9, whichever is closer to its shape, with black bars.
func DvdOrDvFilter(v *VideoStream, pal bool) string {
	height := 480
	if pal {
		height = 576
	}
	aspect := v.DisplayAspect()
	wide := aspect >= 1.55
	frame := 4 / 3.0
	dar := "4/3"
	if wide {
		frame, dar = 16/9.0, "16/9"
	}
	var w, h int
	if aspect > frame {
		w, h = 720, Even(float64(height)*frame/aspect)
	} else {
		w, h = Even(720*aspect/frame), height
	}
	return fmt.Sprintf("scale=%d:%d,pad=720:%d:(ow-iw)/2:(oh-ih)/2:black,setdar=%s", w, h, height, dar)
}

// IsPal: PAL (25 fps) for 25/50 fps sources, NTSC for everything else.
func IsPal(fps float64) bool {
	return math.Abs(fps-25) < 0.5 || math.Abs(fps-50) < 1 || math.Abs(fps-12.5) < 0.3
}

// FitBitrateKbps is the video bitrate that fits limitBytes over seconds, less
// the sound and a margin; 0 when not even 100 kbit/s fits.
func FitBitrateKbps(limitBytes int64, seconds float64, audioKbps int) int {
	v := int(float64(limitBytes)*8*0.95/1000/seconds) - audioKbps
	if v >= 100 {
		return v
	}
	return 0
}

// ForSizeLimit returns the options a video with a size limit is encoded with,
// and the video bitrate that fits (0 when the length is unknown or nothing
// fits): never copied, sound at most 96 kbit/s a track, no hardware encoder.
func ForSizeLimit(m presets.MediaOptions, info *MediaInfo, limitBytes int64) (presets.MediaOptions, int) {
	f := m
	f.Clip = m.Clip.Clone()
	f.HardwareEncoding = false
	if !(f.AudioBitrateKbps > 0 && f.AudioBitrateKbps < 96) {
		f.AudioBitrateKbps = 96
	}
	audio := 0
	if !f.RemoveAudio {
		audio = f.AudioBitrateKbps * len(info.AudioTracks)
	}
	kbps := 0
	if s := info.Seconds(); s > 0 {
		kbps = FitBitrateKbps(limitBytes, s, audio)
	}
	return f, kbps
}

// WithMaxRate caps the video's peaks at kbps next to its constant quality;
// VP9 and libaom's "-b:v 0" becomes the cap.
func WithMaxRate(passes []Pass, kbps int) []Pass {
	out := make([]Pass, len(passes))
	for p, pass := range passes {
		args := pass.Args
		var r []string
		for i := 0; i < len(args); i++ {
			if args[i] == "-b:v" && i+1 < len(args) && args[i+1] == "0" {
				r = append(r, "-b:v", itoa(kbps)+"k")
				i++
				continue
			}
			r = append(r, args[i])
			if (args[i] == "-crf" || args[i] == "-q:v") && i+1 < len(args) {
				i++
				r = append(r, args[i], "-maxrate", itoa(kbps)+"k", "-bufsize", itoa(kbps*2)+"k")
			}
		}
		out[p] = Pass{r, pass.Weight}
	}
	return out
}

// AtBitrate encodes the video at kbps instead of a constant quality.
func AtBitrate(passes []Pass, kbps int) []Pass {
	out := make([]Pass, len(passes))
	for p, pass := range passes {
		args := pass.Args
		var r []string
		for i := 0; i < len(args); i++ {
			switch {
			case (args[i] == "-crf" || args[i] == "-q:v") && i+1 < len(args):
				r = append(r, "-b:v", itoa(kbps)+"k", "-maxrate", itoa(kbps*3/2)+"k", "-bufsize", itoa(kbps*2)+"k")
				i++
			case args[i] == "-b:v" && i+1 < len(args) && args[i+1] == "0":
				i++ // VP9 / libaom's "constant quality" switch
			default:
				r = append(r, args[i])
			}
		}
		out[p] = Pass{r, pass.Weight}
	}
	return out
}

// VideoCodecArgs are the encoder options of one video codec.
func VideoCodecArgs(target, codec string, q presets.MediaQuality, v *VideoStream, pal bool) ([]string, error) {
	// Constant quantizer for the MPEG-style codecs: 2 best useful, 4 good, 7 visibly compressed.
	quantizer := itoa(choose(q, 2, 4, 7))
	rate := func(r string) []string {
		if r == "" {
			return nil
		}
		return []string{"-r", r}
	}
	switch codec {
	case "libx264":
		return []string{"-c:v", "libx264", "-preset", "medium", "-crf", itoa(choose(q, 18, 23, 28)), "-pix_fmt", "yuv420p"}, nil
	case "libx265":
		a := []string{"-c:v", "libx265", "-preset", "medium", "-crf", itoa(choose(q, 22, 26, 30))}
		a = append(a, HighBitDepthArgs(v)...)
		a = append(a, "-x265-params", "log-level=error")
		if target == "mp4" || target == "m4v" || target == "mov" {
			a = append(a, "-tag:v", "hvc1")
		}
		return a, nil
	case "libsvtav1":
		return append([]string{"-c:v", "libsvtav1", "-preset", "8", "-crf", itoa(choose(q, 26, 32, 40))}, HighBitDepthArgs(v)...), nil
	case "libaom-av1":
		return append([]string{"-c:v", "libaom-av1", "-b:v", "0", "-crf", itoa(choose(q, 26, 32, 40)), "-cpu-used", "6", "-row-mt", "1",
			"-tiles", "2x2"}, HighBitDepthArgs(v)...), nil
	case "libvpx-vp9":
		return []string{"-c:v", "libvpx-vp9", "-b:v", "0", "-crf", itoa(choose(q, 24, 32, 38)), "-row-mt", "1",
			"-deadline", "good", "-cpu-used", "4", "-pix_fmt", "yuv420p"}, nil
	case "mpeg4":
		return append([]string{"-c:v", "mpeg4", "-tag:v", "XVID", "-q:v", quantizer, "-g", "250", "-bf", "2", "-pix_fmt", "yuv420p"},
			rate(NearestRate(v.FrameRate))...), nil
	case "wmv2":
		return append([]string{"-c:v", "wmv2", "-q:v", quantizer, "-g", "250", "-pix_fmt", "yuv420p"}, rate(NearestRate(v.FrameRate))...), nil
	case "libtheora":
		return append([]string{"-c:v", "libtheora", "-q:v", itoa(choose(q, 8, 6, 4)), "-pix_fmt", "yuv420p"}, rate(NearestRate(v.FrameRate))...), nil
	case "mpeg2video":
		if target == "vob" {
			// DVD-Video limits: peak 9.8 Mbit/s, 1.8 Mbit VBV buffer, GOP 15 (PAL) / 18 (NTSC).
			g, r := "18", "30000/1001"
			if pal {
				g, r = "15", "25"
			}
			return []string{"-c:v", "mpeg2video", "-b:v", itoa(choose(q, 8000, 6000, 4000)) + "k", "-maxrate", "9000k",
				"-minrate", "0", "-bufsize", "1835k", "-g", g, "-bf", "2", "-pix_fmt", "yuv420p", "-r", r}, nil
		}
		return []string{"-c:v", "mpeg2video", "-q:v", quantizer, "-g", "15", "-bf", "2", "-pix_fmt", "yuv420p",
			"-r", BroadcastRate(v.FrameRate)}, nil
	case "dvvideo":
		// DV is fixed at 25 Mbit/s: NTSC samples colour 4:1:1, PAL 4:2:0.
		if pal {
			return []string{"-c:v", "dvvideo", "-pix_fmt", "yuv420p", "-r", "25"}, nil
		}
		return []string{"-c:v", "dvvideo", "-pix_fmt", "yuv411p", "-r", "30000/1001"}, nil
	}
	return nil, fmt.Errorf("unsupported video codec %s", codec)
}

// HighBitDepthArgs keeps 10 bits (with the HDR colour tags) for 10-bit and
// HDR sources in HEVC and AV1, 8-bit 4:2:0 for everything else.
func HighBitDepthArgs(v *VideoStream) []string {
	if v.IsHighBitDepth() || v.IsHdr() {
		return append([]string{"-pix_fmt", "yuv420p10le"}, ColorArgs(v)...)
	}
	return []string{"-pix_fmt", "yuv420p"}
}

// ColorArgs are the source's colour tags (primaries, transfer, matrix).
func ColorArgs(v *VideoStream) []string {
	var a []string
	if c := v.Color; c != nil {
		if c.Primaries != "" {
			a = append(a, "-color_primaries", c.Primaries)
		}
		if c.Transfer != "" {
			a = append(a, "-color_trc", c.Transfer)
		}
		if c.Matrix != "" {
			a = append(a, "-colorspace", c.Matrix)
		}
	}
	return a
}

// NearestRate keeps exact NTSC/film/PAL rates and rounds anything else to
// whole frames per second; "" when unknown.
func NearestRate(fps float64) string {
	if fps <= 0 {
		return ""
	}
	for _, r := range broadcastRates {
		if math.Abs(fps-float64(r[0])/float64(r[1])) < 0.01 {
			return rateText(r)
		}
	}
	return itoa(min(max(int(math.RoundToEven(fps)), 1), 120))
}

// BroadcastRate is the broadcast rate closest to fps (25 when unknown).
func BroadcastRate(fps float64) string {
	if fps <= 0 {
		return "25"
	}
	best := broadcastRates[0]
	for _, r := range broadcastRates[1:] {
		if math.Abs(fps-float64(r[0])/float64(r[1])) < math.Abs(fps-float64(best[0])/float64(best[1])) {
			best = r
		}
	}
	return rateText(best)
}

func rateText(r [2]int) string {
	if r[1] == 1 {
		return itoa(r[0])
	}
	return fmt.Sprintf("%d/%d", r[0], r[1])
}

// ContainerArgs are the muxer options of a target.
func ContainerArgs(target string) []string {
	switch target {
	case "mp4", "m4v", "mov", "3gp", "3g2", "m4a", "m4b":
		// The index at the front: playback can start before the file is loaded.
		return []string{"-movflags", "+faststart"}
	case "m2ts":
		return []string{"-mpegts_m2ts_mode", "1"}
	case "vob":
		return []string{"-muxrate", "10080000", "-packetsize", "2048"}
	case "mp3":
		// ID3v2.3 is what Windows Explorer and most car stereos read.
		return []string{"-id3v2_version", "3"}
	case "wav":
		return []string{"-rf64", "auto"}
	}
	return nil
}

// MetadataArgs keep title, artist, dates, … from input from, without the tags
// that describe the source file.
func MetadataArgs(from int) []string {
	a := []string{"-map_metadata", itoa(from)}
	for _, t := range sourceOnlyTags {
		a = append(a, "-metadata", t+"=")
	}
	return a
}

// ───────────────────────── Audio ─────────────────────────

func audioArgs(input, output, target string, spec Target, o *presets.MediaOptions, info *MediaInfo, extras *CoverExtras) ([]string, error) {
	audio := info.Audio
	if audio == nil {
		return nil, errors.New("The file has no audio track.")
	}
	args := append(common(), "-i", input)
	// Ogg: the tags, and the picture as a comment, come from a metadata file (input 1).
	if extras != nil && extras.MetadataFile != "" {
		args = append(args, "-i", extras.MetadataFile)
	}
	args = append(args, "-map", "0:a:0")
	if info.CoverStream >= 0 && coverTargets[target] {
		args = append(args, "-map", fmt.Sprintf("0:%d", info.CoverStream), "-c:v", "copy", "-disposition:v:0", "attached_pic")
	}
	if extras != nil && extras.AttachmentFile != "" && extras.AttachmentMime != "" {
		args = append(args, "-attach", extras.AttachmentFile, "-metadata:s:t", "mimetype="+extras.AttachmentMime,
			"-metadata:s:t", "filename="+filepath.Base(extras.AttachmentFile))
	}
	if CanCopyIntoAudio(target, audio, o) {
		args = append(args, "-c:a", "copy")
	} else {
		codec := AudioEncoder(target, spec.AudioCodec, o)
		args = append(args, audioCodecArgs(target, codec, o, audio, false)...)
		args = append(args, ChannelAndRateArgs(target, codec, o, audio.SampleRate)...)
	}
	from := 0
	if extras != nil && extras.MetadataFile != "" {
		from = 1
	}
	args = append(args, MetadataArgs(from)...)
	args = append(args, ContainerArgs(target)...)
	return append(args, "-f", spec.Muxer, "-progress", "pipe:1", output), nil
}

// Audio codecs an audio target takes as they are (CopyAudioWhenPossible).
var copyableAudio = map[string]map[string]bool{
	"m4a": setOf("aac", "alac"), "m4b": setOf("aac", "alac"), "aac": setOf("aac"), "mp3": setOf("mp3"),
	"flac": setOf("flac"), "opus": setOf("opus"), "ogg": setOf("vorbis"),
}

// CanCopyIntoAudio: the preset copies sound it can, the target takes this
// codec (only ALAC when asked for Apple Lossless), and nothing asks for an encode.
func CanCopyIntoAudio(target string, a *AudioStream, o *presets.MediaOptions) bool {
	if !o.CopyAudioWhenPossible || o.Quality == presets.MediaSmall || o.AudioBitrateKbps > 0 ||
		((o.AudioChannels == 1 || o.AudioChannels == 2) && o.AudioChannels != a.Channels) || a.Codec == "" || !copyableAudio[target][a.Codec] {
		return false
	}
	return presets.EffectiveAudioCodec(target, o.AudioCodec) != presets.AudioAlac || a.Codec == "alac"
}

// WithoutMetadata copies the file's video, audio and subtitle streams into the
// same format with no tags, per-stream tags or chapters ("Remove metadata").
// Data tracks are left out; album art and MKV font attachments stay.
func WithoutMetadata(input, output, target string, info *MediaInfo) ([]string, error) {
	spec, ok := VideoTargets[target]
	if !ok {
		if spec, ok = AudioTargets[target]; !ok {
			return nil, fmt.Errorf("FFmpeg cannot write '%s'.", target)
		}
	}
	pictures := "0:v?"
	if info.Video != nil {
		pictures = "0:V?"
	}
	args := append(common(), "-i", input, "-map", pictures, "-map", "0:a?", "-map", "0:s?", "-c", "copy")
	if info.Video != nil && appleHevc(info.Video.Codec, target) {
		args = append(args, "-tag:v", "hvc1")
	}
	args = append(args, "-map_metadata", "-1", "-map_metadata:s", "-1", "-map_chapters", "-1", "-fflags", "+bitexact")
	if info.HasAttachments {
		args = append(args, "-map", "0:t", "-map_metadata:s:t", "0:s:t")
	}
	args = append(args, ContainerArgs(target)...)
	return append(args, "-max_muxing_queue_size", "4096", "-f", spec.Muxer, "-progress", "pipe:1", output), nil
}

// AudioEncoder is the PCM format the preset picks for WAV and W64, ALAC when
// asked for Apple Lossless, else the target's usual one.
func AudioEncoder(target, def string, o *presets.MediaOptions) string {
	if target == "wav" || target == "w64" {
		return WavCodec(o.WavFormat)
	}
	if presets.EffectiveAudioCodec(target, o.AudioCodec) == presets.AudioAlac {
		return "alac"
	}
	return def
}

// WavCodec is the PCM codec of a WAV sample format.
func WavCodec(f presets.WavSampleFormat) string {
	switch f {
	case presets.Pcm8:
		return "pcm_u8"
	case presets.Pcm24:
		return "pcm_s24le"
	case presets.Pcm32:
		return "pcm_s32le"
	case presets.Float32:
		return "pcm_f32le"
	}
	return "pcm_s16le"
}

var losslessEncoders = setOf("flac", "alac", "wavpack", "tta")

// ChannelAndRateArgs: mono or stereo for any target, the sample rate for WAV
// and W64; DSD (above 192 kHz) becomes 88.2 kHz for lossless and PCM, 44.1
// kHz for Vorbis and WMA.
func ChannelAndRateArgs(target, codec string, o *presets.MediaOptions, sourceRate int) []string {
	var a []string
	if (o.AudioChannels == 1 || o.AudioChannels == 2) && codec != "libopencore_amrnb" {
		a = append(a, "-ac", itoa(o.AudioChannels))
	}
	switch {
	case (target == "wav" || target == "w64") && o.AudioSampleRate >= 8000 && o.AudioSampleRate <= 192000:
		a = append(a, "-ar", itoa(o.AudioSampleRate))
	case sourceRate > 192000 && (losslessEncoders[codec] || strings.HasPrefix(codec, "pcm_")):
		a = append(a, "-ar", "88200")
	case sourceRate > 192000 && (codec == "libvorbis" || codec == "wmav2"):
		a = append(a, "-ar", "44100")
	}
	return a
}

var (
	mp3Bitrates = []int{32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}
	mp2Bitrates = []int{32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384}
	ac3Bitrates = []int{32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384, 448, 512, 576, 640}
)

// audioCodecArgs are the encoder options of one audio codec: the preset's
// bitrate wins (clamped), else the quality picks a bitrate or VBR level.
func audioCodecArgs(target, codec string, o *presets.MediaOptions, a *AudioStream, inVideo bool) []string {
	q := o.Quality
	req := o.AudioBitrateKbps
	ch := a.Channels
	if ch <= 0 {
		ch = 2
	}
	pick := func(def int) int {
		if req > 0 {
			return req
		}
		return def
	}
	args := []string{"-c:a", codec}
	switch codec {
	case "aac":
		args = append(args, bitrate(pick(scaled(choose(q, 256, 192, 128), ch)), 16, 256*ch)...)
	case "libopus":
		args = append(args, bitrate(pick(scaled(choose(q, 160, 128, 64), ch)), 6, 256*ch)...)
	case "libmp3lame":
		if req > 0 || inVideo {
			// Constant bitrate: variable-bitrate MP3 confuses AVI players' sync.
			args = append(args, bitrate(Snap(pick(choose(q, 256, 192, 128)), mp3Bitrates), 32, 320)...)
		} else {
			args = append(args, "-q:a", itoa(choose(q, 0, 2, 5)))
		}
	case "libvorbis":
		if req > 0 {
			args = append(args, bitrate(req, 32, 500)...)
		} else {
			args = append(args, "-q:a", itoa(choose(q, 6, 4, 2)))
		}
	case "wmav2":
		args = append(args, bitrate(pick(choose(q, 192, 128, 96)), 32, 320)...)
		if ch > 2 {
			args = append(args, "-ac", "2")
		}
	case "mp2":
		args = append(args, bitrate(Snap(pick(choose(q, 320, 224, 160)), mp2Bitrates), 32, 384)...)
		if a.SampleRate > 0 && a.SampleRate < 32000 {
			args = append(args, "-ar", "32000")
		}
	case "ac3":
		if target == "vob" {
			def := 192
			if ch > 2 {
				def = 448
			}
			args = append(args, bitrate(Snap(pick(def), ac3Bitrates), 64, 448)...)
			args = append(args, "-ar", "48000")
		} else if req > 0 {
			args = append(args, bitrate(Snap(req, ac3Bitrates), 32, 640)...)
		}
	case "eac3":
		args = append(args, bitrate(pick(scaled(choose(q, 256, 192, 128), ch)), 32, 6144)...)
	case "libopencore_amrnb":
		args = append(args, "-ar", "8000", "-ac", "1", "-b:a", "12.2k")
	case "pcm_s16le":
		switch {
		case target == "dv":
			args = append(args, "-ar", "48000", "-ac", "2")
		case target == "mxf":
			args = append(args, "-ar", "48000")
		case target == "voc" && ch > 2:
			args = append(args, "-ac", "2")
		}
	}
	return args
}

// scaled is a stereo-pair bitrate scaled to the channel count.
func scaled(stereo, channels int) int { return stereo * min(max(channels, 1), 8) / 2 }

func bitrate(kbps, lo, hi int) []string {
	return []string{"-b:a", itoa(min(max(kbps, lo), max(lo, hi))) + "k"}
}

// Snap is the largest allowed bitrate not above kbps (the smallest if all are above).
func Snap(kbps int, allowed []int) int {
	best := -1
	for _, a := range allowed {
		if a <= kbps && a > best {
			best = a
		}
	}
	if best < 0 {
		return allowed[0]
	}
	return best
}

// ───────────────────────── Subtitles ─────────────────────────

// subtitleArgs take the first text subtitle track out as SRT, ASS or WebVTT;
// a track already in the target's format is copied.
func subtitleArgs(input, output, target string, info *MediaInfo) ([]string, error) {
	var track *SubtitleStream
	for i := range info.Subtitles {
		if textSubtitleCodecs[info.Subtitles[i].Codec] {
			track = &info.Subtitles[i]
			break
		}
	}
	if track == nil {
		if len(info.Subtitles) == 0 {
			return nil, errors.New("The video has no subtitle track.")
		}
		return nil, errors.New("The video's subtitles are pictures (Blu-ray PGS, DVD or DVB subtitles), not text. Picture subtitles can't be turned into text without OCR.")
	}
	spec := SubtitleTargets[target]
	same := (target == "srt" && (track.Codec == "subrip" || track.Codec == "srt")) || (target == "ass" && track.Codec == "ass") ||
		(target == "vtt" && track.Codec == "webvtt")
	enc := spec[1]
	if same {
		enc = "copy"
	}
	return append(common(), "-i", input, "-map", fmt.Sprintf("0:%d", track.Index), "-c:s", enc, "-f", spec[0], "-progress", "pipe:1", output), nil
}

// ───────────────────────── Animated GIF / APNG ─────────────────────────

// animatedGif is two passes: the first finds the 256 colours that suit the
// whole clip (palettegen), the second maps every frame to them (paletteuse).
func animatedGif(input, output string, o *presets.MediaOptions, info *MediaInfo, workDir string, speed float64) ([]Pass, error) {
	v := info.Video
	if v == nil {
		return nil, ErrNoVideo
	}
	q := o.Quality
	chain := gifFrames(v, o.MaxHeight, choose(q, 15, 12, 10), choose(q, 640, 480, 320), speed)
	palette := filepath.Join(workDir, "palette-"+randomName()+".png")
	colors := choose(q, 256, 256, 128)
	// Error diffusion looks best; ordered dithering compresses much better.
	dither := choose(q, "sierra2_4a", "bayer:bayer_scale=4", "bayer:bayer_scale=5")
	// Pass 1 reports no output time until the palette is written at the end, so
	// a discarded copy of the frames goes to a null output whose per-frame stats
	// serve as progress.
	first := append(common(), "-i", input,
		"-filter_complex", fmt.Sprintf("[0:V:0]%s,split[a][b];[a]palettegen=max_colors=%d[palette]", chain, colors),
		"-map", "[palette]", "-frames:v", "1", "-update", "1", "-f", "image2", palette,
		"-map", "[b]", "-stats_enc_pre", "pipe:1", "-stats_enc_pre_fmt", frameTimeKey+"={t}", "-f", "null", "-")
	second := append(common(), "-i", input, "-i", palette,
		"-filter_complex", fmt.Sprintf("[0:V:0]%s[frames];[frames][1:v]paletteuse=dither=%s:diff_mode=rectangle[gif]", chain, dither),
		"-map", "[gif]", "-loop", "0", "-f", "gif", "-progress", "pipe:1", output)
	return []Pass{{first, 0.4}, {second, 0.6}}, nil
}

// animatedPng: full colour, looping, with GIF's frame rate and width limits.
func animatedPng(input, output string, o *presets.MediaOptions, info *MediaInfo, speed float64) ([]string, error) {
	v := info.Video
	if v == nil {
		return nil, ErrNoVideo
	}
	q := o.Quality
	chain := gifFrames(v, o.MaxHeight, choose(q, 15, 12, 10), choose(q, 640, 480, 320), speed)
	return append(common(), "-i", input, "-map", "0:V:0", "-vf", chain, "-an",
		"-c:v", "apng", "-pix_fmt", "rgb24", "-pred", "mixed", "-plays", "0", "-f", "apng", "-progress", "pipe:1", output), nil
}

// gifFrames: speed, frame rate, square pixels and size of animation frames
// (never upscaled); the speed changes timestamps before the frame rate.
func gifFrames(v *VideoStream, maxHeight, fps, maxWidth int, speed float64) string {
	height := "ih"
	if maxHeight > 0 {
		height = fmt.Sprintf("'min(ih,%d)'", max(16, maxHeight))
	}
	timing := ""
	if !math.IsNaN(speed) && !math.IsInf(speed, 0) && speed > 0 && speed != 1 {
		timing = "setpts=PTS/" + num(speed) + ","
	}
	return fmt.Sprintf("%sfps=%d,%sscale=w='min(iw,%d)':h=%s:force_original_aspect_ratio=decrease:flags=lanczos",
		timing, fps, SquarePixels(v), maxWidth, height)
}

// SquarePixels stretches anamorphic video (DV, DVD) to square pixels: images have no pixel aspect.
func SquarePixels(v *VideoStream) string {
	if math.Abs(v.SampleAspect-1) > 0.01 {
		return "scale=w='trunc(iw*sar/2)*2':h=ih,setsar=1,"
	}
	return ""
}

// posterFrame is one full-size PNG frame from a tenth into the video (the
// first frames are often black): the start for video → image conversions.
func posterFrame(input, output string, info *MediaInfo) ([]string, error) {
	v := info.Video
	if v == nil {
		return nil, ErrNoVideo
	}
	return append(common(), "-ss", num(info.Seconds()/10), "-i", input, "-map", "0:V:0",
		"-vf", SquarePixels(v)+"null", "-frames:v", "1", "-update", "1", "-c:v", "png", "-f", "image2",
		"-progress", "pipe:1", output), nil
}

// PlanStill is a video showing the PNG input for StillSeconds: BT.709 TV
// range, 30 fps, one quality step above the preset's (identical frames cost
// the encoder almost nothing).
func PlanStill(input, output, target string, o *presets.MediaOptions, info *MediaInfo) ([]Pass, error) {
	pic := info.Video
	if pic == nil {
		return nil, errors.New("The file has no picture.")
	}
	if !slices.Contains(PictureToVideoTargets, target) {
		return nil, fmt.Errorf("FFmpeg does not turn pictures into '%s'.", target)
	}
	spec := VideoTargets[target]
	q := presets.MediaHigh
	if o.Quality == presets.MediaSmall {
		q = presets.MediaBalanced
	}
	args := append(common(), "-flags2", "+icc_profiles", "-loop", "1", "-framerate", "30", "-i", input, "-map", "0:V:0",
		"-t", itoa(StillSeconds),
		"-vf", fmt.Sprintf("premultiply=inplace=1,%s:out_color_matrix=bt709:out_range=tv,format=yuv420p", StillScale(pic, o.MaxHeight)))
	vc, err := VideoCodecArgs(target, spec.VideoCodec, q, pic, false)
	if err != nil {
		return nil, err
	}
	args = append(args, vc...)
	if spec.VideoCodec == "libx264" {
		args = append(args, "-tune", "stillimage")
	}
	args = append(args, "-colorspace", "bt709", "-an")
	args = append(args, ContainerArgs(target)...)
	args = append(args, "-f", spec.Muxer, "-progress", "pipe:1", output)
	return []Pass{{args, 1}}, nil
}

// StillScale is the picture's own size, even, scaled down only to maxHeight
// or to what video players decode.
func StillScale(pic *VideoStream, maxHeight int) string {
	w, h := float64(pic.Width), float64(pic.Height)
	if w <= 0 || h <= 0 {
		return ScaleFilter(maxHeight) + ":flags=lanczos"
	}
	f := min(1, min(math.Sqrt(maxVideoPixels/(w*h)), maxVideoSide/max(w, h)))
	if maxHeight > 0 {
		f = min(f, float64(max(16, maxHeight))/h)
	}
	return fmt.Sprintf("scale=%d:%d:flags=lanczos", evenDown(w*f), evenDown(h*f))
}

// Even rounds to the nearest even number (at least 2).
func Even(v float64) int { return max(2, int(math.RoundToEven(v/2))*2) }

// evenDown is the even number at or below v (a tiny margin keeps 1080.0000001 at 1080).
func evenDown(v float64) int { return max(2, int(math.Floor(v/2+1e-6))*2) }
