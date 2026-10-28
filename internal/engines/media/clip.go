package media

// "Cut a clip" with parts: one FFmpeg filter graph plays the parts of the
// video one after the other, each at its speed and volume, with black and
// silence for the pauses and the fades at the ends. The same graph writes the
// clip and plays it in the clip window.

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/dseif0x/filee-go/internal/presets"
)

// MinClipLength is the shortest clip in seconds: about three frames.
const MinClipLength = 0.1

// MinSilence is the shortest silence "Remove silence" looks for.
const MinSilence = 0.4

// ClipGraph is the inputs and filter graph of a clip: its picture comes out of
// [v], its sound of [a].
type ClipGraph struct {
	Inputs   []string
	Filter   string
	HasAudio bool
	HasVideo bool
	// Length is how long the clip plays, in seconds.
	Length float64
}

// Sample rate and layout every part's sound is brought to, so the parts join.
const clipAudioFormat = "aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo"

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// ClampClip returns the clip as it can be written from a video of duration
// seconds (0 when unknown): every part inside the video and at least
// MinClipLength long, in order and not overlapping, speeds and volumes within
// their limits, fades no longer than half the clip, music only where it plays.
func ClampClip(c *presets.ClipEdit, duration float64) (*presets.ClipEdit, error) {
	total := 0.0
	if duration > 0 && finite(duration) {
		total = duration
	}
	var parts []presets.ClipPart
	last := 0.0
	for _, p := range c.Parts {
		if !finite(p.Start) || !finite(p.End) {
			continue
		}
		start := p.Start
		if !p.Blank {
			start = max(p.Start, last)
		}
		end := p.End
		if total > 0 {
			start = min(start, total)
			end = min(end, total)
		}
		start = max(0, start)
		if end-start < MinClipLength {
			continue
		}
		vol := p.PlayVolume()
		if p.Muted {
			vol = p.Volume
		}
		parts = append(parts, presets.ClipPart{Start: start, End: end, Blank: p.Blank, Volume: vol, Muted: p.Muted, Speed: p.PlaySpeed()})
		if !p.Blank {
			last = end
		}
	}
	hasSource := false
	for _, p := range parts {
		if !p.Blank {
			hasSource = true
		}
	}
	if !hasSource {
		return nil, errors.New("The clip has no part of the video left.")
	}
	// Pauses only between parts that play: at either end they would only be black.
	for parts[0].Blank {
		parts = parts[1:]
	}
	for parts[len(parts)-1].Blank {
		parts = parts[:len(parts)-1]
	}
	out := &presets.ClipEdit{Parts: parts}
	half := out.Length() / 2
	out.FadeIn = min(presets.ClipFade(c.FadeIn), half)
	out.FadeOut = min(presets.ClipFade(c.FadeOut), half)
	if m := c.Music; m != nil && m.Path != "" && finite(m.Offset) && finite(m.From) && finite(m.Length) {
		k := *m
		k.Offset = max(0, k.Offset)
		k.From = min(max(k.From, 0), max(0, k.Length))
		k.Volume = k.PlayVolume()
		if k.PlayLength(out.Length()) >= MinClipLength {
			out.Music = &k
		}
	}
	return out, nil
}

// ClipGraphOptions are the choices of ClipGraphOf.
type ClipGraphOptions struct {
	// Audio uses the video's own sound.
	Audio bool
	// NoVideo makes a graph of the sound alone (the preview's).
	NoVideo bool
	// NoMusic leaves the clip's music out.
	NoMusic bool
	// PixelFormat of the pictures, the same for every part ("" = yuv420p).
	PixelFormat string
}

// ClipGraphOf builds the graph that plays edit (already clamped) of input:
// its pictures width × height at fps, its sound 48 kHz stereo with the music
// mixed under it. The video is read once, from the first part to the last.
func ClipGraphOf(input string, edit *presets.ClipEdit, width, height int, fps string, o ClipGraphOptions) (*ClipGraph, error) {
	video := !o.NoVideo
	audio := o.Audio
	pixfmt := o.PixelFormat
	if pixfmt == "" {
		pixfmt = "yuv420p"
	}
	song := edit.Music
	if o.NoMusic {
		song = nil
	}
	sound := audio || song != nil
	if !video && !sound {
		return nil, errors.New("A clip's graph needs pictures or sound.")
	}
	var src []presets.ClipPart
	for _, p := range edit.Parts {
		if !p.Blank {
			src = append(src, p)
		}
	}
	if len(src) == 0 {
		return nil, errors.New("The clip has no part of the video.")
	}
	from, to := src[0].Start, src[len(src)-1].End
	inputs := []string{"-ss", ffTime(from), "-to", ffTime(to), "-i", input}
	if song != nil {
		inputs = append(inputs, "-ss", ffTime(song.From), "-i", song.Path)
	}

	var g strings.Builder
	n := len(src)
	if video {
		g.WriteString("[0:V:0]")
		if n > 1 {
			fmt.Fprintf(&g, "split=%d", n)
		} else {
			g.WriteString("null")
		}
		for k := 0; k < n; k++ {
			fmt.Fprintf(&g, "[vs%d]", k)
		}
		g.WriteByte(';')
	}
	if audio {
		g.WriteString("[0:a:0]")
		if n > 1 {
			fmt.Fprintf(&g, "asplit=%d", n)
		} else {
			g.WriteString("anull")
		}
		for k := 0; k < n; k++ {
			fmt.Fprintf(&g, "[as%d]", k)
		}
		g.WriteByte(';')
	}

	picture := fmt.Sprintf("fps=%s,scale=%d:%d,setsar=1,format=%s", fps, width, height, pixfmt)
	source := 0
	for i, p := range edit.Parts {
		length := secs(p.Length())
		if p.Blank {
			if video {
				fmt.Fprintf(&g, "color=c=black:s=%dx%d:r=%s:d=%s,setsar=1,format=%s[v%d];", width, height, fps, length, pixfmt, i)
			}
			if audio {
				fmt.Fprintf(&g, "anullsrc=r=48000:cl=stereo,atrim=duration=%s[a%d];", length, i)
			}
			continue
		}
		start, end := secs(p.Start-from), secs(p.End-from)
		speed := p.PlaySpeed()
		setpts := "setpts=PTS-STARTPTS"
		if speed != 1 {
			setpts = "setpts=(PTS-STARTPTS)/" + secs(speed)
		}
		if video {
			fmt.Fprintf(&g, "[vs%d]trim=start=%s:end=%s,%s,%s[v%d];", source, start, end, setpts, picture, i)
		}
		if audio {
			fmt.Fprintf(&g, "[as%d]atrim=start=%s:end=%s,asetpts=PTS-STARTPTS", source, start, end)
			for _, t := range Tempo(speed) {
				g.WriteString(",atempo=" + secs(t))
			}
			if v := p.PlayVolume(); v != 1 {
				g.WriteString(",volume=" + secs(v))
			}
			fmt.Fprintf(&g, ",%s[a%d];", clipAudioFormat, i)
		}
		source++
	}
	for i := range edit.Parts {
		if video {
			fmt.Fprintf(&g, "[v%d]", i)
		}
		if audio {
			fmt.Fprintf(&g, "[a%d]", i)
		}
	}
	fmt.Fprintf(&g, "concat=n=%d:v=%d:a=%d", len(edit.Parts), b2i(video), b2i(audio))
	if video {
		g.WriteString("[vc]")
	}
	if audio {
		g.WriteString("[ac]")
	}
	g.WriteByte(';')

	total := edit.Length()
	fadeIn := min(presets.ClipFade(edit.FadeIn), total/2)
	fadeOut := min(presets.ClipFade(edit.FadeOut), total/2)
	var outputs []string
	if video {
		outputs = append(outputs, "[vc]"+fades("fade", fadeIn, fadeOut, total)+"[v]")
	}
	switch {
	case song != nil:
		// The music: as long as it plays, at its volume, faded at its ends, delayed to its place, under the sound.
		if !audio {
			outputs = append(outputs, fmt.Sprintf("anullsrc=r=48000:cl=stereo,atrim=duration=%s[ac]", secs(total)))
		}
		length := song.PlayLength(total)
		fade := 0.0
		if song.Fade {
			fade = min(presets.MusicFadeSeconds, length/2)
		}
		delay := int64(math.RoundToEven(song.Offset * 1000))
		chain := fmt.Sprintf("[1:a:0]atrim=duration=%s,asetpts=PTS-STARTPTS", secs(length))
		if v := song.PlayVolume(); v != 1 {
			chain += ",volume=" + secs(v)
		}
		if fade > 0 {
			chain += "," + fades("afade", fade, fade, length)
		}
		if delay > 0 {
			chain += fmt.Sprintf(",adelay=%d:all=1", delay)
		}
		outputs = append(outputs, chain+","+clipAudioFormat+"[m]",
			"[ac][m]amix=inputs=2:duration=first:dropout_transition=0:normalize=0[am]",
			"[am]"+fades("afade", fadeIn, fadeOut, total)+"[a]")
	case audio:
		outputs = append(outputs, "[ac]"+fades("afade", fadeIn, fadeOut, total)+"[a]")
	}
	g.WriteString(strings.Join(outputs, ";"))
	return &ClipGraph{Inputs: inputs, Filter: g.String(), HasAudio: sound, HasVideo: video, Length: total}, nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ClipEditArgs write edit of input as target, encoded with the preset's
// quality, largest height and codec, at the video's size and frame rate.
func ClipEditArgs(input, output, target string, edit *presets.ClipEdit, o *presets.MediaOptions, info *MediaInfo, encoders map[string]bool) ([]string, error) {
	v := info.Video
	if v == nil {
		return nil, ErrNoVideo
	}
	spec, ok := VideoTargets[target]
	if !ok {
		return nil, fmt.Errorf("FFmpeg cannot write '%s'.", target)
	}
	w, h := ClipSize(v, o.MaxHeight)
	fps := NearestRate(v.FrameRate)
	if fps == "" {
		fps = "30"
	}
	enc, err := VideoEncoder(target, spec.VideoCodec, o.VideoCodec, encoders)
	if err != nil {
		return nil, err
	}
	tenBit := v.IsHighBitDepth() && (enc == "libx265" || enc == "libsvtav1" || enc == "libaom-av1")
	pixfmt := "yuv420p"
	if tenBit {
		pixfmt = "yuv420p10le"
	}
	// "Remove audio" leaves the clip silent, music and all.
	graph, err := ClipGraphOf(input, edit, w, h, fps, ClipGraphOptions{
		Audio: !o.RemoveAudio && info.Audio != nil, NoMusic: o.RemoveAudio, PixelFormat: pixfmt,
	})
	if err != nil {
		return nil, err
	}
	pal := IsPal(v.FrameRate)
	filter := graph.Filter
	if target == "vob" || target == "dv" {
		filter = strings.Replace(filter, "[v]", "[vd]", -1) + ";[vd]" + DvdOrDvFilter(v, pal) + "[v]"
	}
	args := append(common(), graph.Inputs...)
	args = append(args, "-filter_complex", filter, "-map", "[v]")
	vc, err := VideoCodecArgs(target, enc, o.Quality, v, pal)
	if err != nil {
		return nil, err
	}
	args = append(args, vc...)
	if graph.HasAudio {
		args = append(args, "-map", "[a]")
		args = append(args, audioCodecArgs(target, spec.AudioCodec, o, &AudioStream{Channels: 2, SampleRate: 48000}, true)...)
	} else {
		args = append(args, "-an")
	}
	args = append(args, MetadataArgs(0)...)
	args = append(args, ContainerArgs(target)...)
	return append(args, "-max_muxing_queue_size", "4096", "-f", spec.Muxer, "-progress", "pipe:1", output), nil
}

// ClipPreviewVideo plays edit in the clip window: pictures height pixels high
// as raw BGRA frames at fps on stdout.
func ClipPreviewVideo(input string, edit *presets.ClipEdit, v *VideoStream, height, fps int) (args []string, width, frameHeight int, err error) {
	width, frameHeight = ClipSize(v, height)
	g, err := ClipGraphOf(input, edit, width, frameHeight, itoa(fps), ClipGraphOptions{PixelFormat: "bgra"})
	if err != nil {
		return nil, 0, 0, err
	}
	args = append(common(), g.Inputs...)
	return append(args, "-filter_complex", g.Filter, "-map", "[v]", "-f", "rawvideo", "-pix_fmt", "bgra", "pipe:1"), width, frameHeight, nil
}

// ClipPreviewAudio plays the sound of edit: a WAV stream on stdout (for ffplay).
func ClipPreviewAudio(input string, edit *presets.ClipEdit, v *VideoStream, hasAudio bool) ([]string, error) {
	w, h := ClipSize(v, 90)
	g, err := ClipGraphOf(input, edit, w, h, "10", ClipGraphOptions{Audio: hasAudio, NoVideo: true})
	if err != nil {
		return nil, err
	}
	args := append(common(), g.Inputs...)
	return append(args, "-filter_complex", g.Filter, "-map", "[a]", "-c:a", "pcm_s16le", "-f", "wav", "pipe:1"), nil
}

// ClipSize is the size of a clip's pictures: as shown (pixel aspect and
// rotation applied), scaled down to maxHeight (0: kept), even sides.
func ClipSize(v *VideoStream, maxHeight int) (int, int) {
	sar := v.SampleAspect
	if sar <= 0 {
		sar = 1
	}
	w, h := float64(v.Width)*sar, float64(v.Height)
	if v.IsRotated() {
		w, h = h, w
	}
	scale := 1.0
	if maxHeight > 0 && h > float64(maxHeight) {
		scale = float64(maxHeight) / h
	}
	return Even(max(16, w*scale)), Even(max(16, h*scale))
}

// Tempo is the atempo steps for speed: each between 0.5 and 2; none for 1.
func Tempo(speed float64) []float64 {
	var steps []float64
	rest := speed
	for rest > 2+1e-9 {
		steps = append(steps, 2)
		rest /= 2
	}
	for rest < 0.5-1e-9 {
		steps = append(steps, 0.5)
		rest /= 0.5
	}
	if math.Abs(rest-1) > 1e-9 {
		steps = append(steps, rest)
	}
	return steps
}

// fades is the fade in and out of filter (fade or afade), or a pass-through.
func fades(filter string, in, out, total float64) string {
	var s []string
	if in > 0 {
		s = append(s, fmt.Sprintf("%s=t=in:st=0:d=%s", filter, secs(in)))
	}
	if out > 0 {
		s = append(s, fmt.Sprintf("%s=t=out:st=%s:d=%s", filter, secs(max(0, total-out)), secs(out)))
	}
	if len(s) > 0 {
		return strings.Join(s, ",")
	}
	if filter == "fade" {
		return "null"
	}
	return "anull"
}
