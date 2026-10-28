package media

// The clip window's preview: plays a clip as it will be written (parts,
// speeds, volumes, pauses and fades), the pictures as raw frames from FFmpeg,
// the sound through ffplay (without it the preview plays silent). Also the
// sound's loudness over the video for the timeline, and where it is silent.

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/dseif0x/filee-go/internal/presets"
	"github.com/dseif0x/filee-go/internal/tools"
)

// PreviewFps is the frame rate of the preview.
const PreviewFps = 25

// peakRate is how many samples a second the loudness is read at.
const peakRate = 2000

// SilenceThreshold: quieter than this (dB) is silence.
const SilenceThreshold = -35

// soundStart is how long the sound takes to start once ffplay has it: the pictures wait as long.
const soundStart = 180 * time.Millisecond

var errNotFound = errors.New("FFmpeg was not found.")

// PreviewHasSound is true when ffplay is next to FFmpeg.
func PreviewHasSound() bool { return player() != "" }

func player() string {
	ffmpeg, _ := Locate()
	if ffmpeg == "" {
		return ""
	}
	return tools.Program(filepath.Dir(ffmpeg), "ffplay")
}

// PlayerVolume (0–100) instead of full when ≥ 0; tests play silently with 0.
var PlayerVolume = -1

// PlayClip plays edit of the video at path from its start: each picture,
// height pixels high in BGRA, goes to onFrame (pixels, width, height, seconds
// into the clip) when it is due; the sound plays along. It ends when the clip
// has played or ctx is cancelled (then without an error). The buffer may be
// kept only until onFrame returns.
func PlayClip(ctx context.Context, path string, edit *presets.ClipEdit, height int, onFrame func([]byte, int, int, float64)) error {
	ffmpeg, ffprobe := Locate()
	if ffmpeg == "" {
		return errNotFound
	}
	input, _ := filepath.Abs(path)
	info, err := Probe(ctx, ffprobe, input)
	if err != nil {
		return err
	}
	v := info.Video
	if v == nil {
		return ErrNoVideo
	}
	clip, err := ClampClip(edit, info.Seconds())
	if err != nil {
		return nil // nothing left to play
	}
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	args, w, h, err := ClipPreviewVideo(input, clip, v, height, PreviewFps)
	if err != nil {
		return err
	}
	pictures := exec.CommandContext(ctx, ffmpeg, args...)
	pictures.Dir = filepath.Dir(ffmpeg)
	frames, err := pictures.StdoutPipe()
	if err != nil {
		return err
	}
	if err := pictures.Start(); err != nil {
		return err
	}
	defer pictures.Wait()

	var playing *exec.Cmd
	if ffplay := player(); ffplay != "" && (info.Audio != nil || clip.Music != nil) {
		a, err := ClipPreviewAudio(input, clip, v, info.Audio != nil)
		if err == nil {
			decoder := exec.CommandContext(ctx, ffmpeg, a...)
			pa := []string{"-hide_banner", "-loglevel", "error", "-nodisp", "-autoexit"}
			if PlayerVolume >= 0 {
				pa = append(pa, "-volume", strconv.Itoa(PlayerVolume))
			}
			playing = exec.CommandContext(ctx, ffplay, append(pa, "-i", "-")...)
			if pipe, err := decoder.StdoutPipe(); err == nil {
				playing.Stdin = pipe
				if decoder.Start() == nil {
					defer decoder.Wait()
					if playing.Start() != nil {
						playing = nil
					}
				} else {
					playing = nil
				}
			} else {
				playing = nil
			}
		}
	}

	buf := make([]byte, w*h*4)
	var clock time.Time
	delay := time.Duration(0)
	if playing != nil {
		delay = soundStart
	}
	for n := 0; ctx.Err() == nil; n++ {
		if _, err := io.ReadFull(frames, buf); err != nil {
			break
		}
		if clock.IsZero() {
			// The clock starts with the first picture: FFmpeg takes a moment to seek and start.
			clock = time.Now()
		}
		due := delay + time.Duration(float64(n)/PreviewFps*float64(time.Second))
		wait := due - time.Since(clock)
		if wait > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(wait):
			}
		} else if wait < -200*time.Millisecond {
			continue // behind: skip a picture rather than run late
		}
		if ctx.Err() == nil {
			onFrame(buf, w, h, float64(n)/PreviewFps)
		}
	}
	if ctx.Err() == nil && playing != nil {
		// The sound may still be playing its last moment.
		done := make(chan struct{})
		go func() { playing.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(time.Second):
		case <-ctx.Done():
		}
	}
	stop()
	if playing != nil {
		playing.Wait()
	}
	return nil
}

// ReadLoudness is how loud the sound of path is from start to end seconds
// (end ≤ start: to its end), in count steps from 0 to 1 (its loudest step);
// nil without sound.
func ReadLoudness(ctx context.Context, path string, start, end float64, count int, workDir string) ([]float32, error) {
	ffmpeg, _ := Locate()
	if ffmpeg == "" {
		return nil, errNotFound
	}
	samples := filepath.Join(workDir, randomName()+".f32")
	defer os.Remove(samples)
	args := common()
	if start > 0 {
		args = append(args, "-ss", ffTime(start))
	}
	if end > start {
		args = append(args, "-to", ffTime(end))
	}
	abs, _ := filepath.Abs(path)
	args = append(args, "-i", abs, "-map", "0:a:0", "-ac", "1", "-ar", itoa(peakRate), "-c:a", "pcm_f32le", "-f", "f32le", samples)
	r, err := RunFfmpeg(ctx, ffmpeg, args, nil, 0)
	if err != nil {
		return nil, err
	}
	if r.ExitCode != 0 || !exists(samples) {
		return nil, nil // no sound
	}
	data, err := os.ReadFile(samples)
	if err != nil {
		return nil, err
	}
	f := make([]float32, len(data)/4)
	for i := range f {
		f[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	d := 0.0
	if end > start {
		d = end - start
	}
	return Loudness(f, peakRate, d, count), nil
}

// ReadSoundLength is how long the sound (or video) at path plays; an error when it has no sound.
func ReadSoundLength(ctx context.Context, path string) (float64, error) {
	_, ffprobe := Locate()
	if ffprobe == "" {
		return 0, errNotFound
	}
	abs, _ := filepath.Abs(path)
	info, err := Probe(ctx, ffprobe, abs)
	if err != nil {
		return 0, err
	}
	if info.Audio == nil {
		return 0, errors.New("The file has no sound.")
	}
	if info.Seconds() <= 0 {
		return 0, errors.New("The file doesn't say how long it is.")
	}
	return info.Seconds(), nil
}

// FindSilences returns the silent stretches of path between start and end.
func FindSilences(ctx context.Context, path string, start, end float64) ([]ClipRange, error) {
	ffmpeg, _ := Locate()
	if ffmpeg == "" {
		return nil, errNotFound
	}
	abs, _ := filepath.Abs(path)
	res, err := tools.Run(ctx, tools.Cmd{Path: ffmpeg, Args: SilenceArgs(abs, start, end), Timeout: 10 * time.Minute, LowPriority: true})
	var exit *tools.ExitError
	if errors.As(err, &exit) {
		return nil, fmt.Errorf("FFmpeg failed: %s", Summarize(string(res.Stderr)))
	}
	if err != nil {
		return nil, err
	}
	return ParseSilences(string(res.Stderr), start, end), nil
}

// SilenceArgs run silencedetect over start..end, reported on stderr.
func SilenceArgs(input string, start, end float64) []string {
	return []string{"-hide_banner", "-nostdin", "-nostats", "-v", "info", "-ss", ffTime(start), "-to", ffTime(end), "-i", input,
		"-map", "0:a:0", "-af", fmt.Sprintf("silencedetect=noise=%sdB:d=%s", strconv.Itoa(SilenceThreshold), strconv.FormatFloat(MinSilence, 'f', -1, 64)),
		"-f", "null", "-"}
}

var silenceLine = regexp.MustCompile(`silence_(start|end):\s*(-?[0-9.]+(?:e-?[0-9]+)?)`)

// ParseSilences reads silencedetect's report in seconds of the video (its
// times count from offset); one still silent at the end lasts until end.
func ParseSilences(log string, offset, end float64) []ClipRange {
	var out []ClipRange
	open, isOpen := 0.0, false
	for _, m := range silenceLine.FindAllStringSubmatch(log, -1) {
		s, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			continue
		}
		at := max(offset, offset+s)
		if m[1] == "start" {
			open, isOpen = at, true
		} else if isOpen {
			if min(at, end) > open {
				out = append(out, ClipRange{open, min(at, end)})
			}
			isOpen = false
		}
	}
	if isOpen && end > open {
		out = append(out, ClipRange{open, end})
	}
	return out
}

// Loudness is the root mean square of samples (mono, rate a second) in count
// equal steps of duration seconds, the loudest step 1.
func Loudness(samples []float32, rate int, duration float64, count int) []float32 {
	if count <= 0 || len(samples) == 0 {
		return nil
	}
	steps := make([]float32, count)
	total := float64(len(samples))
	if duration > 0 {
		total = duration * float64(rate)
	}
	n := float64(len(samples))
	loudest := float32(0)
	for i := range steps {
		from := int(min(n, math.Floor(total*float64(i)/float64(count))))
		to := int(min(n, math.Floor(total*float64(i+1)/float64(count))))
		if to <= from {
			continue
		}
		sum := 0.0
		for _, s := range samples[from:to] {
			sum += float64(s) * float64(s)
		}
		steps[i] = float32(math.Sqrt(sum / float64(to-from)))
		loudest = max(loudest, steps[i])
	}
	if loudest > 0 {
		for i := range steps {
			steps[i] /= loudest
		}
	}
	return steps
}
