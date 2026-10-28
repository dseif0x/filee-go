// Package media is the FFmpeg engine (id "ffmpeg"): video ↔ video (H.264,
// HEVC, AV1, …), video → animated GIF / APNG, a still frame or every frame,
// audio and subtitle extraction, audio ↔ audio, GIF, APNG and pictures →
// video, images → HEIC (x265 + Filee's own container writer), Live / Motion
// Photos → their video and a clip of a video → a Live / Motion Photo, clips
// or pictures joined into one file, copies without metadata and "Cut a clip".
// FFmpeg runs as an external program found through internal/tools.
package media

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/presets"
	"github.com/dseif0x/filee-go/internal/tools"
)

// FfmpegSpec finds FFmpeg: FILEE_FFMPEG, Filee's engines folder, PATH,
// Homebrew / MacPorts on macOS, the usual Windows folders.
var FfmpegSpec = tools.Spec{
	Names: []string{"ffmpeg"},
	Env:   "FILEE_FFMPEG",
	Dirs: map[string][]string{
		"darwin":  {"ffmpeg", "ffmpeg/bin", "/opt/homebrew/opt/ffmpeg/bin", "/Applications/FFmpeg", "~/Applications/FFmpeg"},
		"linux":   {"ffmpeg", "ffmpeg/bin"},
		"windows": {"ffmpeg/bin", "ffmpeg", "C:/ffmpeg/bin", "C:/Program Files/ffmpeg/bin"},
	},
}

// Locate returns ffmpeg and ffprobe of the same installation, or "" "".
func Locate() (ffmpeg, ffprobe string) {
	f := tools.Find(FfmpegSpec)
	if f == "" {
		return "", ""
	}
	p := tools.Program(filepath.Dir(f), "ffprobe")
	if p == "" {
		return "", ""
	}
	return f, p
}

// Converter is the FFmpeg engine.
type Converter struct {
	// Log receives which hardware encoders work and why a hardware encode was redone in software.
	Log *slog.Logger
	// SelectHardware replaces the hardware probe (tests).
	SelectHardware func(ctx context.Context, ffmpeg, codec string, tenBit bool, encoders map[string]bool) *HardwareEncoder

	edges []convert.Edge
}

// New returns the engine.
func New() *Converter { return &Converter{} }

func (c *Converter) log() *slog.Logger {
	if c.Log != nil {
		return c.Log
	}
	return slog.Default()
}

func (c *Converter) ID() string   { return "ffmpeg" }
func (c *Converter) Name() string { return "FFmpeg" }

// MaxParallelism: FFmpeg's encoders use every core; two runs keep the CPU busy between files.
func (c *Converter) MaxParallelism() int { return 2 }

// Transforms: only HEIC, which is read and edited here before x265 encodes it; never video.
func (c *Converter) Transforms(from, to string) bool {
	return to == "heic" && formats.CategoryOf(from) != formats.Video
}

func (c *Converter) Status() convert.Status {
	f, _ := Locate()
	if f == "" {
		return convert.Unavailable("engine.reason.not_installed", "ffmpeg")
	}
	v, err := VersionOf(f)
	if err != nil {
		// Present but broken.
		return convert.Unavailable("engine.reason.error", err.Error())
	}
	return convert.Available(f, "FFmpeg "+v)
}

func idsOf(cat formats.Category) []string {
	var out []string
	for _, f := range formats.Known {
		if f.Category == cat {
			out = append(out, f.ID)
		}
	}
	return out
}

func sortedKeys[T any](m map[string]T) []string {
	k := make([]string, 0, len(m))
	for x := range m {
		k = append(k, x)
	}
	slices.Sort(k)
	return k
}

// Edges: every video format → every video target, a still PNG frame, every
// audio and subtitle target, GIF and APNG (costlier: video → JPG goes through
// the still frame); every frame as pictures (FrameSequence); audio ↔ audio;
// GIF, APNG and PNG → MP4, WEBM, MOV; pictures → HEIC; Live / Motion Photos.
func (c *Converter) Edges() []convert.Edge {
	if c.edges != nil {
		return c.edges
	}
	videos := idsOf(formats.Video)
	audios := idsOf(formats.Audio)
	videoTargets := append(sortedKeys(VideoTargets), "png")
	audioTargets := sortedKeys(AudioTargets)
	var e []convert.Edge
	for _, s := range videos {
		for _, t := range videoTargets {
			e = append(e, convert.Edge{From: s, To: t, Cost: 10})
		}
	}
	for _, s := range videos {
		// A picture turned into a video has no sound: picture → video → audio would make a silent file.
		for _, t := range audioTargets {
			e = append(e, convert.Edge{From: s, To: t, Cost: 10, NotForImageSources: true})
		}
	}
	for _, s := range videos {
		for _, t := range presets.FrameTargets {
			e = append(e, convert.Edge{From: s, To: t, Cost: 10, FrameSequence: true})
		}
	}
	for _, s := range videos {
		// SRT is the hub to the other subtitle formats, so ASS and WebVTT cost a little more.
		for _, t := range sortedKeys(SubtitleTargets) {
			cost := 11
			if t == "srt" {
				cost = 10
			}
			e = append(e, convert.Edge{From: s, To: t, Cost: cost, NotForImageSources: true})
		}
	}
	for _, s := range videos {
		e = append(e, convert.EC(s, "gif", 15), convert.EC(s, "apng", 15))
	}
	for _, s := range audios {
		for _, t := range audioTargets {
			e = append(e, convert.Edge{From: s, To: t, Cost: 10})
		}
	}
	for _, t := range PictureToVideoTargets {
		e = append(e, convert.EC("gif", t, 10), convert.EC("apng", t, 11), convert.EC("png", t, 11))
	}
	for _, s := range HeicSources {
		e = append(e, convert.EC(s, "heic", 10))
	}
	for _, s := range presets.MotionPhotoSources {
		for _, t := range presets.MotionPhotoTargets {
			e = append(e, convert.Edge{From: s, To: t, Cost: 10, MotionPhoto: true})
		}
	}
	live := presets.LivePhotoTargets(presets.LivePhotoApple)
	for _, t := range presets.LivePhotoTargets(presets.LivePhotoAndroid) {
		if !slices.Contains(live, t) {
			live = append(live, t)
		}
	}
	for _, s := range videos {
		for _, t := range live {
			e = append(e, convert.Edge{From: s, To: t, Cost: 10, LivePhoto: true})
		}
	}
	c.edges = e
	return e
}

func (c *Converter) tools() (string, string, error) {
	f, p := Locate()
	if f == "" {
		return "", "", errNotFound
	}
	return f, p, nil
}

func (c *Converter) selectHardware(ctx context.Context, ffmpeg, codec string, tenBit bool, encoders map[string]bool) *HardwareEncoder {
	if c.SelectHardware != nil {
		return c.SelectHardware(ctx, ffmpeg, codec, tenBit, encoders)
	}
	return SelectHardware(ctx, ffmpeg, codec, tenBit, encoders, c.log())
}

func sizeLimitBytes(mb float64) int64 { return int64(mb * 1_000_000) }

func describeSize(size, limit int64) string {
	return fmt.Sprintf("%s MB > %s MB", num2(float64(size)/1e6), num2(float64(limit)/1e6))
}

// num2 is a number with up to two decimals ("0.##").
func num2(v float64) string { return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64) }

// Convert converts one file.
func (c *Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	ffmpeg, ffprobe, err := c.tools()
	if err != nil {
		return nil, err
	}
	if step.MotionPhoto {
		return c.extractMotion(ctx, step, progress)
	}
	if step.LivePhoto {
		return c.livePhoto(ctx, ffmpeg, ffprobe, step, progress)
	}
	if step.To == "heic" {
		return c.toHeic(ctx, ffmpeg, step, progress)
	}
	input, _ := filepath.Abs(step.InputPath)
	info, err := Probe(ctx, ffprobe, input)
	if err != nil {
		return nil, err
	}
	progress.Report(0.02)
	if step.FrameSequence {
		return writeFrames(ctx, ffmpeg, step, input, info, progress)
	}
	if _, video := VideoTargets[step.To]; video && step.Preset.CutsClipOf(step.From) {
		return c.cutClip(ctx, ffmpeg, step, input, info, progress)
	}
	f := formats.ByID(step.To)
	if f == nil {
		return nil, fmt.Errorf("FFmpeg cannot write '%s'.", step.To)
	}
	output := step.Output.Allocate(f.PrimaryExtension(), "")
	if output == "" {
		return nil, nil
	}
	// A PNG is a still picture (from the image engines), shown for a few seconds.
	still := step.From == "png"
	_, toVideo := VideoTargets[step.To]
	_, toAudio := AudioTargets[step.To]

	var cover *CoverExtras
	if !still && info.CoverStream >= 0 && toAudio {
		cover = PrepareCover(ctx, ffmpeg, input, info.CoverStream, step.To, step.WorkDir)
	}
	media := step.Preset.Media
	// "Remove metadata": the streams as they are, in the same format, without tags.
	if media.RemoveMetadata && step.From == step.To && !still && (toVideo || toAudio) {
		args, err := WithoutMetadata(input, output, step.To, info)
		if err == nil {
			err = runPasses(ctx, ffmpeg, []Pass{{args, 1}}, output, info.Seconds(), progress)
		}
		if err != nil {
			tryDelete(output)
			return nil, err
		}
		progress.Report(1)
		return []string{output}, nil
	}

	// The size limit: refused when not even a low bitrate fits, else one encode
	// with the bitrate capped at what fits, and another only if it missed.
	var limit int64
	kbps := 0
	if !still && media.MaxSizeMb > 0 && toVideo {
		limit = sizeLimitBytes(media.MaxSizeMb)
		media, kbps = ForSizeLimit(media, info, limit)
		if kbps == 0 && info.Seconds() > 0 {
			return nil, convert.Refuse("error.over_size_limit", fmt.Sprintf("%.0f s in %s MB", info.Seconds(), num2(media.MaxSizeMb)))
		}
	}
	var encoders map[string]bool
	if !still {
		encoders = EncodersOf(ffmpeg)
	}
	var hw *HardwareEncoder
	if !still {
		if codec := HardwareCodec(step.To, &media, info, false); codec != "" {
			hw = c.selectHardware(ctx, ffmpeg, codec, TenBit(codec, info.Video), encoders)
		}
	}
	// Video → animated GIF / APNG can play faster or slower.
	speed := 1.0
	if step.To == "gif" || step.To == "apng" {
		speed = step.Preset.Image.PlaybackSpeed()
	}
	plan := func(e *HardwareEncoder) ([]Pass, error) {
		if still {
			return PlanStill(input, output, step.To, &media, info)
		}
		return Plan(input, output, step.To, &media, info, step.WorkDir, PlanOptions{Cover: cover, Encoders: encoders, Hardware: e, AnimationSpeed: speed})
	}
	// Fails for a missing stream before anything is written.
	passes, err := plan(hw)
	if err != nil {
		tryDelete(output)
		return nil, err
	}
	if kbps > 0 {
		passes = WithMaxRate(passes, kbps)
		inner := plan
		plan = func(e *HardwareEncoder) ([]Pass, error) {
			p, err := inner(e)
			if err != nil {
				return nil, err
			}
			return WithMaxRate(p, kbps), nil
		}
	}
	// FFmpeg reports the position in the output, which a speed change makes longer or shorter.
	duration := info.Seconds() / speed
	if still {
		duration = StillSeconds
	}
	encoding := progress
	if limit > 0 && progress != nil {
		encoding = func(p float64) { progress(p * 0.9) }
	}
	err = c.runWithFallback(ctx, passes, hw, plan, func(p []Pass) error {
		return runPasses(ctx, ffmpeg, p, output, duration, encoding)
	}, output)
	if err == nil && limit > 0 {
		err = fitAgain(ctx, ffmpeg, step, input, output, &media, info, encoders, cover, limit, kbps, progress)
	}
	if err != nil {
		// Never leave a half-written file where the user expects a finished one.
		tryDelete(output)
		return nil, err
	}
	progress.Report(1)
	return []string{output}, nil
}

// fitAgain: a bitrate is an average, so when the capped encode missed the
// limit the video is encoded once more at a bitrate as much lower as it
// missed by; refused when that still doesn't fit.
func fitAgain(ctx context.Context, ffmpeg string, step *convert.Step, input, output string, fitting *presets.MediaOptions, info *MediaInfo,
	encoders map[string]bool, cover *CoverExtras, limit int64, kbps int, progress convert.Progress) error {
	size := fileSize(output)
	if size > limit && kbps > 0 {
		kbps = int(float64(kbps) * float64(limit) / float64(size) * 0.95)
		var again convert.Progress
		if progress != nil {
			again = func(p float64) { progress(0.9 + p*0.1) }
		}
		passes, err := Plan(input, output, step.To, fitting, info, step.WorkDir, PlanOptions{Cover: cover, Encoders: encoders})
		if err != nil {
			return err
		}
		if err := runPasses(ctx, ffmpeg, AtBitrate(passes, kbps), output, info.Seconds(), again); err != nil {
			return err
		}
		size = fileSize(output)
	}
	if size > limit {
		return convert.Refuse("error.over_size_limit", describeSize(size, limit))
	}
	return nil
}

// cutClip: one stretch as recorded is copied as it is unless the preset asks
// for an exact cut; a copy the target can't take is encoded instead. Parts,
// speeds, volumes, pauses or fades go through one filter graph.
func (c *Converter) cutClip(ctx context.Context, ffmpeg string, step *convert.Step, input string, info *MediaInfo, progress convert.Progress) ([]string, error) {
	if info.Video == nil {
		return nil, ErrNoVideo
	}
	media := &step.Preset.Media
	edit, err := ClampClip(media.Clip, info.Seconds())
	if err != nil {
		return nil, err
	}
	output := step.Output.Allocate(formats.ByID(step.To).PrimaryExtension(), "")
	if output == "" {
		return nil, nil
	}
	duration := edit.Length()
	fail := func(err error) ([]string, error) {
		tryDelete(output)
		return nil, err
	}
	if !edit.IsPlainCut() {
		args, err := ClipEditArgs(input, output, step.To, edit, media, info, EncodersOf(ffmpeg))
		if err != nil {
			return fail(err)
		}
		if err := runPasses(ctx, ffmpeg, []Pass{{args, 1}}, output, duration, progress); err != nil {
			return fail(err)
		}
		progress.Report(1)
		return []string{output}, nil
	}
	clip := ClipRange{edit.Parts[0].Start, edit.Parts[0].End}
	if !media.ExactCut {
		args, err := ClipCopy(input, output, step.To, clip, media, info)
		if err == nil {
			err = runPasses(ctx, ffmpeg, []Pass{{args, 1}}, output, duration, progress)
		}
		if err == nil {
			progress.Report(1)
			return []string{output}, nil
		}
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		c.log().Warn("copying the clip failed; encoding it instead", "file", input, "reason", err)
		tryDelete(output)
	}
	encoders := EncodersOf(ffmpeg)
	var hw *HardwareEncoder
	if codec := HardwareCodec(step.To, media, info, true); codec != "" {
		hw = c.selectHardware(ctx, ffmpeg, codec, TenBit(codec, info.Video), encoders)
	}
	plan := func(e *HardwareEncoder) ([]Pass, error) {
		return Plan(input, output, step.To, media, info, step.WorkDir, PlanOptions{Encoders: encoders, Hardware: e, Clip: &clip})
	}
	passes, err := plan(hw)
	if err != nil {
		return fail(err)
	}
	if err := c.runWithFallback(ctx, passes, hw, plan, func(p []Pass) error {
		return runPasses(ctx, ffmpeg, p, output, duration, progress)
	}, output); err != nil {
		return fail(err)
	}
	progress.Report(1)
	return []string{output}, nil
}

// extractMotion: the video of a Live / Motion Photo is copied into the work
// directory, converted there like any video and moved into place once done.
func (c *Converter) extractMotion(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	video, companion, err := ExtractMotion(step.InputPath, step.From, step.WorkDir, c.log())
	if err != nil {
		return nil, err
	}
	staged := &stagedOutput{final: step.Output, workDir: step.WorkDir, companion: companion, paths: map[string]string{}}
	inner := *step
	inner.InputPath, inner.From, inner.MotionPhoto, inner.Output = video, "mov", false, staged
	written, err := c.Convert(ctx, &inner, progress)
	if err != nil {
		return nil, err
	}
	return staged.commit(written)
}

// stagedOutput hands out work-directory paths for outputs whose real path
// comes from final. That one is never the companion: with "overwrite",
// IMG_0001.HEIC → MOV would otherwise be written over the Live Photo's own MOV.
type stagedOutput struct {
	final     convert.OutputAllocator
	workDir   string
	companion string
	paths     map[string]string
}

func (s *stagedOutput) Allocate(ext, suffix string) string {
	p := s.final.Allocate(ext, suffix)
	for n := 2; p != "" && s.companion != "" && samePath(p, s.companion); n++ {
		p = s.final.Allocate(ext, fmt.Sprintf("%s (%d)", suffix, n))
	}
	if p == "" {
		return ""
	}
	t := filepath.Join(s.workDir, randomName()+"."+trimDot(ext))
	s.paths[t] = p
	return t
}

func trimDot(s string) string {
	for len(s) > 0 && s[0] == '.' {
		s = s[1:]
	}
	return s
}

func (s *stagedOutput) commit(written []string) ([]string, error) {
	var out []string
	for _, t := range written {
		final := s.paths[t]
		os.Remove(final)
		if err := moveFile(t, final); err != nil {
			return out, err
		}
		out = append(out, final)
	}
	return out, nil
}

// runWithFallback runs passes planned with hw. When a hardware encode fails
// (a driver error, a picture larger than the chip encodes) the file is
// encoded again in software: the user asked for a file, not for the GPU.
// Cancellation is never retried.
func (c *Converter) runWithFallback(ctx context.Context, passes []Pass, hw *HardwareEncoder, plan func(*HardwareEncoder) ([]Pass, error),
	run func([]Pass) error, output string) error {
	err := run(passes)
	if err == nil || hw == nil || ctx.Err() != nil {
		return err
	}
	c.log().Warn("hardware encoder failed; encoding it again with the software encoder", "file", output, "encoder", hw.Name, "reason", err)
	tryDelete(output)
	// An FFmpeg without x265 or an AV1 encoder says so here.
	p, err := plan(nil)
	if err != nil {
		return err
	}
	return run(p)
}

// runPasses runs every pass, then checks that FFmpeg wrote something.
func runPasses(ctx context.Context, ffmpeg string, passes []Pass, output string, seconds float64, progress convert.Progress) error {
	done := 0.0
	for _, p := range passes {
		if err := runPass(ctx, ffmpeg, p, seconds, progress, done); err != nil {
			return err
		}
		done += p.Weight
	}
	if fileSize(output) == 0 {
		return errors.New("FFmpeg did not write any output.")
	}
	return nil
}

// runPass runs one pass and maps FFmpeg's position onto its share of the progress.
func runPass(ctx context.Context, ffmpeg string, p Pass, seconds float64, progress convert.Progress, done float64) error {
	var onPosition func(float64)
	var est *tools.ProgressEstimate
	if progress != nil && seconds > 0 {
		onPosition = func(s float64) { progress(0.02 + 0.97*(done+p.Weight*min(max(s/seconds, 0), 1))) }
	} else if progress != nil {
		// Without a duration (raw streams) there is nothing to measure against: an estimate instead.
		est = tools.StartEstimate(progress, 20*time.Second, 0.02+0.97*done, 0.02+0.97*(done+p.Weight))
	}
	err := runChecked(ctx, ffmpeg, p.Args, onPosition, 0)
	est.Stop()
	return err
}

// runJoinPass maps FFmpeg's position onto from … to of the progress.
func runJoinPass(ctx context.Context, ffmpeg string, args []string, seconds float64, progress convert.Progress, from, to float64) error {
	var onPosition func(float64)
	if progress != nil && seconds > 0 {
		onPosition = func(s float64) { progress(from + (to-from)*min(max(s/seconds, 0), 1)) }
	}
	return runChecked(ctx, ffmpeg, args, onPosition, 0)
}

// JoinMedia joins clips or audio files into one file, or pictures into a
// slideshow. Everything that can fail before FFmpeg writes happens first, and
// nothing here deletes output: with "Overwrite" it may be the user's own file.
func (c *Converter) JoinMedia(ctx context.Context, inputs []string, output, format string, preset *presets.Preset, workDir string, progress convert.Progress) error {
	ffmpeg, ffprobe, err := c.tools()
	if err != nil {
		return err
	}
	scratch := filepath.Join(workDir, "join-"+randomName()[:8])
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		return err
	}
	m := &preset.Media
	var args []string
	var seconds, start float64
	if m.Slideshow {
		slides, err := prepareSlides(ctx, ffmpeg, inputs, scratch, progress)
		if err != nil {
			return err
		}
		enc, err := VideoEncoder("mp4", "libx264", m.VideoCodec, EncodersOf(ffmpeg))
		if err != nil {
			return err
		}
		if args, err = Slideshow(slides, output, m, enc, scratch); err != nil {
			return err
		}
		seconds = SlideshowDuration(len(slides), m).Seconds()
		start = 0.2
	} else {
		var clips []Clip
		for i, p := range inputs {
			abs, _ := filepath.Abs(p)
			info, err := Probe(ctx, ffprobe, abs)
			if err != nil {
				return err
			}
			clips = append(clips, Clip{abs, info})
			seconds += info.Seconds()
			progress.Report(0.05 * float64(i+1) / float64(len(inputs)))
		}
		start = 0.05
		infos := make([]*MediaInfo, len(clips))
		for i := range clips {
			infos[i] = clips[i].Info
		}
		switch {
		case format == "m4a":
			for _, c := range clips {
				if c.Info.Audio == nil && c.Info.Video == nil {
					return fmt.Errorf("%s has no sound.", filepath.Base(c.Path))
				}
			}
			args, err = EncodeAudio(clips, output, m, scratch)
		case CanCopyVideos(infos, m):
			list := filepath.Join(scratch, "clips.txt")
			paths := make([]string, len(clips))
			for i := range clips {
				paths[i] = clips[i].Path
			}
			if err = os.WriteFile(list, []byte(ConcatList(paths)), 0o644); err == nil {
				args = CopyVideos(list, output, clips[0].Info)
			}
		default:
			var enc string
			if enc, err = VideoEncoder("mp4", "libx264", m.VideoCodec, EncodersOf(ffmpeg)); err == nil {
				args, err = EncodeVideos(clips, output, m, enc, scratch)
			}
		}
		if err != nil {
			return err
		}
	}
	if err := runJoinPass(ctx, ffmpeg, args, seconds, progress, start, 1); err != nil {
		return err
	}
	if fileSize(output) == 0 {
		return errors.New("FFmpeg did not write any output.")
	}
	progress.Report(1)
	return nil
}

// prepareSlides fits every picture into the slideshow frame as a small JPEG (a fifth of the progress).
func prepareSlides(ctx context.Context, ffmpeg string, pictures []string, scratch string, progress convert.Progress) ([]string, error) {
	if len(pictures) > presets.MaxSlides {
		return nil, convert.Refuse("error.too_many_pictures", fmt.Sprintf("%d > %d", len(pictures), presets.MaxSlides))
	}
	var slides []string
	for i, p := range pictures {
		slide := filepath.Join(scratch, fmt.Sprintf("slide-%04d.jpg", i+1))
		abs, _ := filepath.Abs(p)
		if err := runJoinPass(ctx, ffmpeg, PrepareSlide(abs, slide, SlideshowWidth, SlideshowHeight), 0, nil, 0, 0); err != nil {
			return nil, err
		}
		slides = append(slides, slide)
		progress.Report(0.2 * float64(i+1) / float64(len(pictures)))
	}
	return slides, nil
}

var _ convert.Converter = (*Converter)(nil)
var _ convert.MediaJoiner = (*Converter)(nil)
var _ convert.ImageTransformer = (*Converter)(nil)
