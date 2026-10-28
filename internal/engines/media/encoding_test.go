package media

import (
	"math"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dseif0x/filee-go/internal/presets"
)

var hd = &VideoStream{Width: 1920, Height: 1080, SampleAspect: 1, FrameRate: 29.97}

func opts(f func(*presets.MediaOptions)) *presets.MediaOptions {
	o := presets.DefaultMediaOptions()
	if f != nil {
		f(&o)
	}
	return &o
}

func infoOf(seconds float64, v *VideoStream, a ...AudioStream) *MediaInfo {
	i := &MediaInfo{Duration: time.Duration(seconds * float64(time.Second)), Video: v, CoverStream: -1, AudioTracks: a}
	if len(a) > 0 {
		i.Audio = &a[0]
	}
	return i
}

func args(t *testing.T, target string, o *presets.MediaOptions, info *MediaInfo) []string {
	t.Helper()
	if o == nil {
		o = opts(nil)
	}
	if info == nil {
		info = infoOf(10, hd, AudioStream{Channels: 6, SampleRate: 44100})
	}
	p, err := Plan("in.mkv", "out."+target, target, o, info, "work", PlanOptions{})
	if err != nil {
		t.Fatalf("%s: %v", target, err)
	}
	if len(p) != 1 {
		t.Fatalf("%s: %d passes", target, len(p))
	}
	return p[0].Args
}

func after(a []string, option string) string {
	i := slices.Index(a, option)
	if i < 0 || i+1 >= len(a) {
		return "<missing " + option + ">"
	}
	return a[i+1]
}

func eq[T comparable](t *testing.T, want, got T) {
	t.Helper()
	if want != got {
		t.Errorf("want %v, got %v", want, got)
	}
}

func TestQualityBitrateAndSizeReachTheEncoders(t *testing.T) {
	mp4 := args(t, "mp4", opts(func(o *presets.MediaOptions) { o.Quality = presets.MediaHigh; o.MaxHeight = 720 }), nil)
	eq(t, "libx264", after(mp4, "-c:v"))
	eq(t, "18", after(mp4, "-crf"))
	eq(t, "scale=w=-2:h='trunc(min(ih,720)/2)*2'", after(mp4, "-vf"))
	eq(t, "+faststart", after(mp4, "-movflags"))
	eq(t, "768k", after(mp4, "-b:a")) // 256k per stereo pair × 3 (5.1)
	eq(t, "mp4", after(mp4, "-f"))

	webm := args(t, "webm", opts(func(o *presets.MediaOptions) { o.Quality = presets.MediaSmall; o.RemoveAudio = true }), nil)
	i := slices.Index(webm, "-b:v")
	eq(t, "-b:v 0 -crf 38", strings.Join(webm[i:i+4], " "))
	if !slices.Contains(webm, "-an") || slices.Contains(webm, "0:a:0") {
		t.Error("webm without audio")
	}
	mp3 := args(t, "mp3", opts(func(o *presets.MediaOptions) { o.AudioBitrateKbps = 130 }), nil)
	eq(t, "128k", after(mp3, "-b:a"))
	if slices.Contains(mp3, "-q:a") {
		t.Error("mp3 with bitrate has -q:a")
	}
	eq(t, "2", after(args(t, "mp3", nil, nil), "-q:a"))
	eq(t, "2", after(args(t, "wma", nil, nil), "-ac"))
	eq(t, "8000", after(args(t, "amr", nil, nil), "-ar"))
	eq(t, "30000/1001", after(args(t, "mpg", nil, nil), "-r"))
	eq(t, "448k", after(args(t, "vob", nil, nil), "-b:a"))
}

func TestChannelsSampleRateAndBitDepth(t *testing.T) {
	opus := args(t, "opus", opts(func(o *presets.MediaOptions) { o.AudioBitrateKbps = 32; o.AudioChannels = 1 }), nil)
	eq(t, "32k", after(opus, "-b:a"))
	eq(t, "1", after(opus, "-ac"))
	if slices.Contains(args(t, "opus", nil, nil), "-ac") {
		t.Error("-ac without a channel option")
	}
	wav := args(t, "wav", opts(func(o *presets.MediaOptions) { o.AudioChannels = 1; o.AudioSampleRate = 16000; o.WavFormat = presets.Pcm24 }), nil)
	eq(t, "pcm_s24le", after(wav, "-c:a"))
	eq(t, "1", after(wav, "-ac"))
	eq(t, "16000", after(wav, "-ar"))
	eq(t, "pcm_s16le", after(args(t, "wav", nil, nil), "-c:a"))
	eq(t, "pcm_u8", after(args(t, "wav", opts(func(o *presets.MediaOptions) { o.WavFormat = presets.Pcm8 }), nil), "-c:a"))
	if slices.Contains(args(t, "flac", opts(func(o *presets.MediaOptions) { o.AudioSampleRate = 16000 }), nil), "-ar") {
		t.Error("flac got a sample rate")
	}
	amr := args(t, "amr", opts(func(o *presets.MediaOptions) { o.AudioChannels = 2 }), nil)
	for i := 1; i < len(amr); i++ {
		if amr[i-1] == "-ac" && amr[i] != "1" {
			t.Error("amr is mono")
		}
	}
}

func TestStreamsTheTargetTakesAreCopied(t *testing.T) {
	phone := infoOf(10, &VideoStream{Width: 1920, Height: 1080, SampleAspect: 1, Rotation: -90, FrameRate: 30, Codec: "hevc", PixelFormat: "yuv420p10le"},
		AudioStream{2, 44100, "aac"})
	mp4 := args(t, "mp4", nil, phone)
	eq(t, "copy", after(mp4, "-c:v"))
	eq(t, "hvc1", after(mp4, "-tag:v"))
	eq(t, "copy", after(mp4, "-c:a"))
	if slices.Contains(mp4, "-vf") {
		t.Error("copy with -vf")
	}
	webm := args(t, "webm", nil, phone)
	eq(t, "libvpx-vp9 libopus", after(webm, "-c:v")+" "+after(webm, "-c:a"))
	eq(t, "libx264", after(args(t, "mp4", opts(func(o *presets.MediaOptions) { o.MaxHeight = 1080 }), phone), "-c:v"))
	eq(t, "copy", after(args(t, "mp4", opts(func(o *presets.MediaOptions) { o.MaxHeight = 2160 }), phone), "-c:v"))
	eq(t, "libx264", after(args(t, "mp4", opts(func(o *presets.MediaOptions) { o.Quality = presets.MediaSmall }), phone), "-c:v"))
	v444 := *phone.Video
	v444.Codec, v444.PixelFormat = "h264", "yuv444p"
	eq(t, "libx264", after(args(t, "mp4", nil, infoOf(10, &v444, AudioStream{2, 44100, "aac"})), "-c:v"))
	pcm := infoOf(10, phone.Video, AudioStream{2, 48000, "pcm_s16le"})
	eq(t, "aac", after(args(t, "mp4", nil, pcm), "-c:a"))
	eq(t, "aac", after(args(t, "mov", opts(func(o *presets.MediaOptions) { o.AudioBitrateKbps = 128 }), phone), "-c:a"))
	eq(t, "aac", after(args(t, "mov", opts(func(o *presets.MediaOptions) { o.AudioChannels = 1 }), phone), "-c:a"))
	eq(t, "copy", after(args(t, "mov", opts(func(o *presets.MediaOptions) { o.AudioChannels = 2 }), phone), "-c:a"))
}

func TestEveryAudioTrackIsKept(t *testing.T) {
	info := infoOf(10, hd, AudioStream{2, 48000, "opus"}, AudioStream{6, 48000, "dts"})
	a := args(t, "mp4", nil, info)
	if !slices.Contains(a, "0:a:1") {
		t.Fatal("second track not mapped")
	}
	eq(t, "aac", after(a, "-c:a:0"))
	eq(t, "576k", after(a, "-b:a:1"))
	flv := args(t, "flv", nil, info)
	if slices.Contains(flv, "0:a:1") {
		t.Error("flv holds one track")
	}
}

func TestHevcAv1AndMissingEncoders(t *testing.T) {
	hevc := args(t, "mp4", opts(func(o *presets.MediaOptions) { o.VideoCodec = presets.VideoHevc }), nil)
	eq(t, "libx265", after(hevc, "-c:v"))
	eq(t, "26", after(hevc, "-crf"))
	eq(t, "hvc1", after(hevc, "-tag:v"))
	av1 := args(t, "mkv", opts(func(o *presets.MediaOptions) { o.VideoCodec = presets.VideoAv1 }), nil)
	eq(t, "libsvtav1", after(av1, "-c:v"))
	enc, err := VideoEncoder("mp4", "libx264", presets.VideoAv1, setOf("libaom-av1"))
	eq(t, "libaom-av1", enc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VideoEncoder("mp4", "libx264", presets.VideoHevc, setOf("libx264")); err == nil {
		t.Error("missing x265 not reported")
	}
	// A codec the target doesn't take falls back to the target's usual one.
	eq(t, "mpeg4", after(args(t, "avi", opts(func(o *presets.MediaOptions) { o.VideoCodec = presets.VideoHevc }), nil), "-c:v"))
}

func TestTenBitAndHdrStayTenBit(t *testing.T) {
	hdr := &VideoStream{Width: 3840, Height: 2160, SampleAspect: 1, FrameRate: 30, Codec: "h264", PixelFormat: "yuv420p10le",
		Color: &VideoColor{Primaries: "bt2020", Transfer: "smpte2084", Matrix: "bt2020nc"}}
	a := args(t, "mkv", opts(func(o *presets.MediaOptions) { o.VideoCodec = presets.VideoHevc }), infoOf(5, hdr))
	eq(t, "yuv420p10le", after(a, "-pix_fmt"))
	eq(t, "smpte2084", after(a, "-color_trc"))
	eq(t, "bt2020", after(a, "-color_primaries"))
	if !hdr.IsHdr() || !hdr.IsHighBitDepth() {
		t.Error("hdr not detected")
	}
}

func TestDvdAndDvAreLetterboxed(t *testing.T) {
	eq(t, "scale=720:480,pad=720:480:(ow-iw)/2:(oh-ih)/2:black,setdar=16/9", DvdOrDvFilter(hd, false))
	eq(t, "scale=720:428,pad=720:576:(ow-iw)/2:(oh-ih)/2:black,setdar=16/9", DvdOrDvFilter(&VideoStream{Width: 2048, Height: 858, SampleAspect: 1}, true))
	eq(t, "scale=720:576,pad=720:576:(ow-iw)/2:(oh-ih)/2:black,setdar=4/3", DvdOrDvFilter(&VideoStream{Width: 640, Height: 480, SampleAspect: 1}, true))
	if !IsPal(25) || !IsPal(50) || IsPal(29.97) {
		t.Error("IsPal")
	}
}

func TestFrameRates(t *testing.T) {
	for _, c := range []struct {
		fps               float64
		nearest, broadcst string
	}{{29.97003, "30000/1001", "30000/1001"}, {25, "25", "25"}, {29.87, "30", "30000/1001"}, {15, "15", "24000/1001"}, {0, "", "25"}} {
		eq(t, c.nearest, NearestRate(c.fps))
		eq(t, c.broadcst, BroadcastRate(c.fps))
	}
}

func TestGifPalettePass(t *testing.T) {
	p, err := Plan("in.mp4", "out.gif", "gif", opts(func(o *presets.MediaOptions) { o.MaxHeight = 200 }), infoOf(3, hd), "work", PlanOptions{})
	if err != nil || len(p) != 2 {
		t.Fatal(err, len(p))
	}
	eq(t, 1.0, p[0].Weight+p[1].Weight)
	palette := after(p[0].Args, "image2")
	if !strings.HasPrefix(palette, filepath.Join("work", "palette-")) {
		t.Error(palette)
	}
	if !strings.Contains(after(p[0].Args, "-filter_complex"), "palettegen") {
		t.Error("no palettegen")
	}
	second := p[1].Args
	eq(t, palette, second[slices.Index(second, palette)])
	fc := after(second, "-filter_complex")
	if !strings.Contains(fc, "fps=12,scale=w='min(iw,480)':h='min(ih,200)'") || !strings.Contains(fc, "paletteuse") {
		t.Error(fc)
	}
	fast, _ := Plan("in.mp4", "out.gif", "gif", opts(nil), infoOf(3, hd), "work", PlanOptions{AnimationSpeed: 2})
	if !strings.Contains(after(fast[1].Args, "-filter_complex"), "setpts=PTS/2,fps=12") {
		t.Error("speed not applied")
	}
}

func TestApng(t *testing.T) {
	a := args(t, "apng", opts(func(o *presets.MediaOptions) { o.Quality = presets.MediaHigh }), infoOf(3, hd, AudioStream{2, 44100, ""}))
	eq(t, "fps=15,scale=w='min(iw,640)':h=ih:force_original_aspect_ratio=decrease:flags=lanczos", after(a, "-vf"))
	eq(t, "rgb24", after(a, "-pix_fmt"))
	eq(t, "0", after(a, "-plays"))
	eq(t, "apng", after(a, "-f"))
}

func TestStillPicture(t *testing.T) {
	photo := infoOf(0, &VideoStream{Width: 4033, Height: 3025, SampleAspect: 1, FrameRate: 25})
	p, err := PlanStill("in.png", "out.mov", "mov", opts(nil), photo)
	if err != nil {
		t.Fatal(err)
	}
	mov := p[0].Args
	i := slices.Index(mov, "-loop")
	eq(t, "-loop 1 -framerate 30 -i in.png", strings.Join(mov[i:i+6], " "))
	eq(t, "5", after(mov, "-t"))
	eq(t, "premultiply=inplace=1,scale=4032:3024:flags=lanczos:out_color_matrix=bt709:out_range=tv,format=yuv420p", after(mov, "-vf"))
	eq(t, "libx264 18 stillimage bt709", after(mov, "-c:v")+" "+after(mov, "-crf")+" "+after(mov, "-tune")+" "+after(mov, "-colorspace"))
	w, _ := PlanStill("in.png", "out.webm", "webm", opts(func(o *presets.MediaOptions) { o.Quality = presets.MediaSmall }), photo)
	eq(t, "32", after(w[0].Args, "-crf"))
	if _, err := PlanStill("in.png", "out.avi", "avi", opts(nil), photo); err == nil {
		t.Error("avi accepted")
	}
	for _, c := range []struct {
		w, h, max int
		want      string
	}{{4032, 3024, 0, "scale=4032:3024"}, {1001, 751, 0, "scale=1000:750"}, {4032, 3024, 720, "scale=960:720"},
		{8064, 6048, 0, "scale=6894:5170"}, {30000, 2000, 0, "scale=16384:1092"}, {0, 0, 0, "scale=w='trunc(iw/2)*2':h='trunc(ih/2)*2'"}} {
		eq(t, c.want+":flags=lanczos", StillScale(&VideoStream{Width: c.w, Height: c.h, SampleAspect: 1, FrameRate: 25}, c.max))
	}
}

func TestMissingStreams(t *testing.T) {
	silent := infoOf(1, hd)
	noVideo := infoOf(1, nil, AudioStream{2, 48000, ""})
	if _, err := Plan("in", "out.mp3", "mp3", opts(nil), silent, "w", PlanOptions{}); err == nil || err.Error() != "The file has no audio track." {
		t.Error(err)
	}
	if _, err := Plan("in", "out.mp4", "mp4", opts(nil), noVideo, "w", PlanOptions{}); err == nil || err.Error() != "The file has no video stream." {
		t.Error(err)
	}
	if !slices.Contains(args(t, "mkv", nil, silent), "-an") {
		t.Error("no -an")
	}
	if _, err := Plan("in", "out.srt", "srt", opts(nil), silent, "w", PlanOptions{}); err == nil {
		t.Error("subtitles without a track")
	}
	withPgs := infoOf(1, hd)
	withPgs.Subtitles = []SubtitleStream{{2, "hdmv_pgs_subtitle", ""}, {3, "subrip", "kor"}}
	s := args(t, "srt", nil, withPgs)
	eq(t, "0:3", after(s, "-map"))
	eq(t, "copy", after(s, "-c:s"))
	eq(t, "webvtt", after(args(t, "vtt", nil, withPgs), "-c:s"))
}

func TestProbeIsParsed(t *testing.T) {
	info, err := ParseProbe([]byte(`{
  "streams": [
    { "index": 0, "codec_name": "h264", "codec_type": "video", "width": 1920, "height": 1080,
      "sample_aspect_ratio": "1:1", "r_frame_rate": "30/1", "avg_frame_rate": "30000/1001",
      "disposition": { "attached_pic": 0 }, "side_data_list": [ { "side_data_type": "Display Matrix", "rotation": -90 } ] },
    { "index": 1, "codec_name": "aac", "codec_type": "audio", "channels": 2, "sample_rate": "48000", "duration": "12.5" },
    { "index": 2, "codec_name": "mjpeg", "codec_type": "video", "width": 300, "height": 300, "disposition": { "attached_pic": 1 } },
    { "index": 3, "codec_name": "subrip", "codec_type": "subtitle", "tags": { "language": "kor" } }
  ],
  "format": { "duration": "12.480000" }
}`))
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(info.Seconds()-12.48) > 0.001 {
		t.Error(info.Seconds())
	}
	v := info.Video
	if v.Width != 1920 || v.Rotation != -90 || math.Abs(v.FrameRate-30000/1001.0) > 1e-9 || v.Codec != "h264" {
		t.Errorf("%+v", v)
	}
	if math.Abs(v.DisplayAspect()-9/16.0) > 0.001 || v.DisplayHeight() != 1920 {
		t.Error("display")
	}
	eq(t, AudioStream{2, 48000, "aac"}, *info.Audio)
	eq(t, 2, info.CoverStream)
	eq(t, SubtitleStream{3, "subrip", "kor"}, info.Subtitles[0])
	raw, _ := ParseProbe([]byte(`{ "streams": [ { "index": 0, "codec_type": "audio", "channels": 1, "sample_rate": "8000" } ], "format": {} }`))
	if raw.Duration != 0 || raw.Video != nil {
		t.Error("raw")
	}
}

func TestProgressAndErrorsAreRead(t *testing.T) {
	if s, ok := ParsePosition("out_time_us=2500000"); !ok || s != 2.5 {
		t.Error(s)
	}
	if _, ok := ParsePosition("out_time_us=N/A"); ok {
		t.Error("N/A")
	}
	if s, ok := ParsePosition("frame_time=1.25"); !ok || s != 1.25 {
		t.Error(s)
	}
	if _, ok := ParsePosition("progress=continue"); ok {
		t.Error("progress")
	}
	eq(t, "too many channels: got 6, need 2 or fewer Error while opening encoder",
		Summarize("[wmav2 @ 000001e0] too many channels: got 6, need 2 or fewer\r\n[aost#0:1/wmav2 @ 000001e0] [enc:wmav2 @ 000001e0] Error while opening encoder\r\n"))
	enc := ParseEncoders(" V....D libx264              libx264 H.264\n A....D aac   AAC\n ------\n")
	if !enc["libx264"] || !enc["aac"] || len(enc) != 2 {
		t.Error(enc)
	}
	fl := ParseFilters(" .S zscale  V->V  Apply resizing\n TSC scale V->V x\n ... abuffer |->A buffer\n")
	if !fl["zscale"] || !fl["scale"] || !fl["abuffer"] {
		t.Error(fl)
	}
}

func TestSizeLimit(t *testing.T) {
	eq(t, 0, FitBitrateKbps(1_000_000, 600, 96))
	o, kbps := ForSizeLimit(presets.MediaOptions{HardwareEncoding: true, Quality: presets.MediaBalanced}, infoOf(60, hd, AudioStream{2, 48000, "aac"}), 10_000_000)
	if o.HardwareEncoding || o.AudioBitrateKbps != 96 || kbps != 1266-96 {
		t.Error(o.HardwareEncoding, o.AudioBitrateKbps, kbps)
	}
	capped := WithMaxRate([]Pass{{[]string{"-c:v", "libx264", "-crf", "23"}, 1}}, 1000)[0].Args
	eq(t, "-c:v libx264 -crf 23 -maxrate 1000k -bufsize 2000k", strings.Join(capped, " "))
	vp9 := WithMaxRate([]Pass{{[]string{"-b:v", "0", "-crf", "32"}, 1}}, 500)[0].Args
	eq(t, "-b:v 500k -crf 32 -maxrate 500k -bufsize 1000k", strings.Join(vp9, " "))
	at := AtBitrate([]Pass{{[]string{"-b:v", "0", "-crf", "32", "-x"}, 1}}, 800)[0].Args
	eq(t, "-b:v 800k -maxrate 1200k -bufsize 1600k -x", strings.Join(at, " "))
}

func TestWithoutMetadata(t *testing.T) {
	info := infoOf(5, &VideoStream{Width: 10, Height: 10, SampleAspect: 1, Codec: "hevc"}, AudioStream{2, 48000, "aac"})
	info.HasAttachments = true
	a, err := WithoutMetadata("in.mkv", "out.mkv", "mkv", info)
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Join(a, " ")
	for _, want := range []string{"-map 0:V?", "-map_metadata -1", "-map_chapters -1", "-map 0:t", "-f matroska"} {
		if !strings.Contains(s, want) {
			t.Error(want)
		}
	}
}

func TestFrameSequenceArgs(t *testing.T) {
	a := FrameSequence("in.mp4", "out/frame_%06d.jpg", "jpg", hd, 90, 1)
	eq(t, "passthrough", after(a, "-fps_mode"))
	eq(t, "mjpeg", after(a, "-c:v"))
	eq(t, "5", after(a, "-q:v"))
	b := FrameSequence("in.mp4", "out/frame_%06d.png", "png", &VideoStream{Width: 720, Height: 480, SampleAspect: 8.0 / 9}, 90, 5)
	eq(t, `scale=w='trunc(iw*sar/2)*2':h=ih,setsar=1,select='not(mod(n\,5))'`, after(b, "-vf"))
	eq(t, "vfr", after(b, "-fps_mode"))
	n, ok := EstimatedFrames(infoOf(10, hd), 1)
	if !ok || n != 300 {
		t.Error(n)
	}
	eq(t, int64(1920*1080*1.5*300), EstimatedBytes(hd, "png", 300))
	eq(t, "100,000", thousands(100000))
	eq(t, 2, QualityToQv(100))
	eq(t, 31, QualityToQv(1))
}
