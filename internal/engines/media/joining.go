package media

// Several files into one: clips or audio files back to back ("Merge videos",
// "Merge audio") and pictures one after the other with crossfades ("Video
// from images").

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dseif0x/filee-go/internal/presets"
)

// SlideshowFps: smooth fades, and what every player plays.
const SlideshowFps = 30

// The rate every clip's sound is brought to before joining.
const joinSampleRate = 48000

// MaxInlineGraph: a longer filter graph goes into a file (-/filter_complex):
// Windows limits a command line to 32,767 characters.
const MaxInlineGraph = 8000

// SlideshowWidth × SlideshowHeight is the frame every picture is fitted into.
const SlideshowWidth, SlideshowHeight = 1920, 1080

// Clip is one input of a join.
type Clip struct {
	Path string
	Info *MediaInfo
}

// CanCopyVideos: the clips join without an encode: every one a video MP4
// takes as it is, all with the same codec setup, size, pixel format, frame
// rate, turn and pixel shape, and the same kind of sound (or none).
func CanCopyVideos(clips []*MediaInfo, o *presets.MediaOptions) bool {
	if len(clips) == 0 || clips[0].Video == nil {
		return false
	}
	f := clips[0].Video
	for _, c := range clips {
		v := c.Video
		if v == nil || !CanCopyVideo("mp4", v, o) || v.Codec != f.Codec || v.Width != f.Width || v.Height != f.Height ||
			v.PixelFormat != f.PixelFormat || v.Rotation != f.Rotation || math.Abs(v.SampleAspect-f.SampleAspect) >= 0.01 ||
			math.Abs(v.FrameRate-f.FrameRate) >= 0.01 || v.HeaderHash == "" || v.HeaderHash != f.HeaderHash {
			return false
		}
	}
	s := clips[0].Audio
	for _, c := range clips {
		a := c.Audio
		if s == nil {
			if a != nil {
				return false
			}
			continue
		}
		if a == nil || !CanCopyAudio("mp4", a, o) || a.Codec != s.Codec || a.SampleRate != s.SampleRate || a.Channels != s.Channels {
			return false
		}
	}
	return true
}

// ConcatList is the concat demuxer's list: one file '…' line per clip.
func ConcatList(paths []string) string {
	var b strings.Builder
	for _, p := range paths {
		if a, err := filepath.Abs(p); err == nil {
			p = a
		}
		b.WriteString("file '" + strings.ReplaceAll(p, "'", `'\''`) + "'\n")
	}
	return b.String()
}

// CopyVideos copies clips that share codecs and size into one MP4.
func CopyVideos(listFile, output string, first *MediaInfo) []string {
	a := append(common(), "-f", "concat", "-safe", "0", "-i", listFile, "-map", "0:V:0")
	if first.Audio != nil {
		a = append(a, "-map", "0:a:0")
	}
	a = append(a, "-c", "copy")
	if first.Video != nil && first.Video.Codec == "hevc" {
		a = append(a, "-tag:v", "hvc1")
	}
	a = append(a, MetadataArgs(0)...)
	a = append(a, ContainerArgs("mp4")...)
	return append(a, "-max_muxing_queue_size", "4096", "-f", "mp4", "-progress", "pipe:1", output)
}

// JoinFrame is the frame every clip is fitted into: the first clip's picture
// as shown, even, no taller than maxHeight.
func JoinFrame(first *VideoStream, maxHeight int) (int, int) {
	h := float64(max(2, first.DisplayHeight()))
	if maxHeight > 0 {
		h = min(h, float64(max(16, maxHeight)))
	}
	return Even(h * first.DisplayAspect()), Even(h)
}

// EncodeVideos encodes clips of any codec and size into one MP4, each fitted
// into the first clip's frame; a clip without sound is silent for its length.
func EncodeVideos(clips []Clip, output string, o *presets.MediaOptions, encoder, workDir string) ([]string, error) {
	first := clips[0].Info.Video
	if first == nil {
		return nil, fmt.Errorf("%s has no video stream.", filepath.Base(clips[0].Path))
	}
	w, h := JoinFrame(first, o.MaxHeight)
	fps := NearestRate(first.FrameRate)
	if fps == "" {
		fps = "30"
	}
	withSound := false
	for _, c := range clips {
		if c.Info.Audio != nil {
			withSound = true
		}
	}
	var graph []string
	segments := ""
	for i, c := range clips {
		v := c.Info.Video
		if v == nil {
			return nil, fmt.Errorf("%s has no video stream.", filepath.Base(c.Path))
		}
		graph = append(graph, fmt.Sprintf("[%d:V:0]%s%s,fps=%s,format=yuv420p[v%d]", i, SquarePixels(v), fit(w, h, ""), fps, i))
		segments += fmt.Sprintf("[v%d]", i)
		if !withSound {
			continue
		}
		if c.Info.Audio != nil {
			graph = append(graph, fmt.Sprintf("[%d:a:0]%s[a%d]", i, resample("stereo"), i))
		} else {
			s, err := silence(c, "stereo")
			if err != nil {
				return nil, err
			}
			graph = append(graph, fmt.Sprintf("%s[a%d]", s, i))
		}
		segments += fmt.Sprintf("[a%d]", i)
	}
	tail := "[v]"
	if withSound {
		tail += "[a]"
	}
	graph = append(graph, fmt.Sprintf("%sconcat=n=%d:v=1:a=%d%s", segments, len(clips), b2i(withSound), tail))

	a := common()
	for _, c := range clips {
		a = append(a, "-i", c.Path)
	}
	ga, err := GraphArgs(strings.Join(graph, ";"), workDir)
	if err != nil {
		return nil, err
	}
	a = append(a, ga...)
	a = append(a, "-map", "[v]")
	vc, err := VideoCodecArgs("mp4", encoder, o.Quality, first, false)
	if err != nil {
		return nil, err
	}
	a = append(a, vc...)
	if withSound {
		a = append(a, "-map", "[a]", "-c:a", "aac", "-b:a", itoa(aacKbps(o, 2))+"k")
	} else {
		a = append(a, "-an")
	}
	a = append(a, MetadataArgs(0)...)
	a = append(a, ContainerArgs("mp4")...)
	return append(a, "-max_muxing_queue_size", "4096", "-f", "mp4", "-progress", "pipe:1", output), nil
}

// EncodeAudio decodes the files and joins them sample after sample into one
// M4A (no gaps or encoder padding): mono when every clip is, else stereo.
func EncodeAudio(clips []Clip, output string, o *presets.MediaOptions, workDir string) ([]string, error) {
	mono := true
	for _, c := range clips {
		if c.Info.Audio == nil || c.Info.Audio.Channels != 1 {
			mono = false
		}
	}
	layout := "stereo"
	if mono {
		layout = "mono"
	}
	var graph []string
	segments := ""
	for i, c := range clips {
		if c.Info.Audio != nil {
			graph = append(graph, fmt.Sprintf("[%d:a:0]%s[a%d]", i, resample(layout), i))
		} else {
			s, err := silence(c, layout)
			if err != nil {
				return nil, err
			}
			graph = append(graph, fmt.Sprintf("%s[a%d]", s, i))
		}
		segments += fmt.Sprintf("[a%d]", i)
	}
	graph = append(graph, fmt.Sprintf("%sconcat=n=%d:v=0:a=1[a]", segments, len(clips)))
	a := common()
	for _, c := range clips {
		a = append(a, "-i", c.Path)
	}
	ga, err := GraphArgs(strings.Join(graph, ";"), workDir)
	if err != nil {
		return nil, err
	}
	a = append(a, ga...)
	a = append(a, "-map", "[a]", "-vn")
	if presets.EffectiveAudioCodec("m4a", o.AudioCodec) == presets.AudioAlac {
		a = append(a, "-c:a", "alac")
	} else {
		ch := 2
		if mono {
			ch = 1
		}
		a = append(a, "-c:a", "aac", "-b:a", itoa(aacKbps(o, ch))+"k")
	}
	a = append(a, MetadataArgs(0)...)
	a = append(a, ContainerArgs("m4a")...)
	return append(a, "-f", "ipod", "-progress", "pipe:1", output), nil
}

func aacKbps(o *presets.MediaOptions, channels int) int {
	if o.AudioBitrateKbps > 0 {
		return min(max(o.AudioBitrateKbps, 16), 512)
	}
	return choose(o.Quality, 256, 192, 128) * channels / 2
}

func resample(layout string) string {
	return fmt.Sprintf("aresample=%d,aformat=sample_fmts=fltp:sample_rates=%d:channel_layouts=%s", joinSampleRate, joinSampleRate, layout)
}

// silence as long as a clip without sound keeps the later clips' sound in step.
func silence(c Clip, layout string) (string, error) {
	s := c.Info.Seconds()
	if s <= 0 {
		return "", fmt.Errorf("%s has no sound, and FFmpeg can't tell how long it is.", filepath.Base(c.Path))
	}
	return fmt.Sprintf("aevalsrc=0:c=%s:s=%d:d=%s,%s", layout, joinSampleRate, num(s), resample(layout)), nil
}

// SlideshowDuration is how long count pictures play: the fades overlap them.
func SlideshowDuration(count int, o *presets.MediaOptions) time.Duration {
	if count <= 0 {
		return 0
	}
	s := float64(count)*o.SlideDuration() - float64(count-1)*o.FadeDuration()
	return time.Duration(s * float64(time.Second))
}

// PrepareSlide fits one picture into the slideshow frame (black bars,
// transparency on black) as a JPEG at FFmpeg's best quality.
func PrepareSlide(picture, output string, width, height int) []string {
	return append(common(), "-i", picture, "-map", "0:V:0",
		"-vf", "premultiply=inplace=1,"+fit(width, height, ":flags=lanczos"),
		"-frames:v", "1", "-update", "1", "-c:v", "mjpeg", "-q:v", "2", "-f", "image2", output)
}

// Slideshow is the prepared slides as one MP4: each shown for SlideDuration,
// fading into the next over FadeDuration; BT.709 TV range, 30 fps, no sound.
func Slideshow(slides []string, output string, o *presets.MediaOptions, encoder, workDir string) ([]string, error) {
	show := num(o.SlideDuration())
	fade := o.FadeDuration()
	var graph []string
	for i := range slides {
		// One decoded frame per picture, repeated for its time.
		graph = append(graph, fmt.Sprintf("[%d:v]scale=out_color_matrix=bt709:out_range=tv,format=yuv420p,setsar=1,fps=%d,"+
			"tpad=stop_mode=clone:stop_duration=%s,trim=duration=%s,setpts=PTS-STARTPTS[s%d]", i, SlideshowFps, show, show, i))
	}
	switch {
	case len(slides) == 1:
		graph = append(graph, "[s0]null[out]")
	case fade > 0:
		prev := "s0"
		for k := 1; k < len(slides); k++ {
			label := fmt.Sprintf("x%d", k)
			if k == len(slides)-1 {
				label = "out"
			}
			graph = append(graph, fmt.Sprintf("[%s][s%d]xfade=transition=fade:duration=%s:offset=%s[%s]", prev, k, num(fade),
				num(float64(k)*(o.SlideDuration()-fade)), label))
			prev = label
		}
	default:
		var s strings.Builder
		for i := range slides {
			fmt.Fprintf(&s, "[s%d]", i)
		}
		graph = append(graph, fmt.Sprintf("%sconcat=n=%d:v=1:a=0[out]", s.String(), len(slides)))
	}
	a := common()
	for _, s := range slides {
		a = append(a, "-i", s)
	}
	ga, err := GraphArgs(strings.Join(graph, ";"), workDir)
	if err != nil {
		return nil, err
	}
	a = append(a, ga...)
	a = append(a, "-map", "[out]")
	q := presets.MediaHigh
	if o.Quality == presets.MediaSmall {
		q = presets.MediaBalanced
	}
	frame := &VideoStream{Width: SlideshowWidth, Height: SlideshowHeight, SampleAspect: 1, FrameRate: SlideshowFps, PixelFormat: "yuv420p"}
	vc, err := VideoCodecArgs("mp4", encoder, q, frame, false)
	if err != nil {
		return nil, err
	}
	a = append(a, vc...)
	a = append(a, "-r", itoa(SlideshowFps), "-colorspace", "bt709", "-color_primaries", "bt709", "-color_trc", "bt709", "-an")
	a = append(a, ContainerArgs("mp4")...)
	return append(a, "-f", "mp4", "-progress", "pipe:1", output), nil
}

// fit fits a picture into width × height, centred, with black bars.
func fit(w, h int, scaleOptions string) string {
	return fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease:force_divisible_by=2%s,pad=%d:%d:(ow-iw)/2:(oh-ih)/2:black,setsar=1",
		w, h, scaleOptions, w, h)
}

// GraphArgs is -filter_complex with the graph, or -/filter_complex with a
// file holding it when it is long.
func GraphArgs(graph, workDir string) ([]string, error) {
	if len(graph) <= MaxInlineGraph {
		return []string{"-filter_complex", graph}, nil
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, err
	}
	file := filepath.Join(workDir, "graph-"+randomName()+".txt")
	if err := os.WriteFile(file, []byte(graph), 0o644); err != nil {
		return nil, err
	}
	return []string{"-/filter_complex", file}, nil
}
