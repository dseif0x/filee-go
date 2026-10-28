package media

// What the clip window needs of a video before anything is cut: its length
// and pictures of single frames for its timeline and large preview.

import (
	"context"
	"math"
	"path/filepath"
	"time"
)

// VideoSummary is a video as the clip window shows it.
type VideoSummary struct {
	// Duration in seconds, 0 when the file doesn't say.
	Duration float64
	// Width and Height as shown (pixel aspect and rotation applied).
	Width, Height int
	FrameRate     float64
}

// FrameLength is one frame's length in seconds (30 fps when unknown).
func (s VideoSummary) FrameLength() float64 {
	if s.FrameRate > 1 && s.FrameRate < 1000 {
		return 1 / s.FrameRate
	}
	return 1 / 30.0
}

// LastFrameTime is a frame before the end: FFmpeg finds nothing at the end itself.
func (s VideoSummary) LastFrameTime() float64 { return max(0, s.Duration-s.FrameLength()) }

// frameStall: one frame may take this long (FFmpeg seeks to the key frame before it).
const frameStall = 30 * time.Second

// VideoFramesAvailable is true when FFmpeg is installed.
func VideoFramesAvailable() bool { f, _ := Locate(); return f != "" }

// ProbeVideo reads the length and size of the video at path.
func ProbeVideo(ctx context.Context, path string) (VideoSummary, error) {
	_, ffprobe := Locate()
	if ffprobe == "" {
		return VideoSummary{}, errNotFound
	}
	abs, _ := filepath.Abs(path)
	info, err := Probe(ctx, ffprobe, abs)
	if err != nil {
		return VideoSummary{}, err
	}
	v := info.Video
	if v == nil {
		return VideoSummary{}, ErrNoVideo
	}
	h := v.Height
	if v.IsRotated() {
		h = v.Width
	}
	return VideoSummary{info.Seconds(), int(math.RoundToEven(float64(h) * v.DisplayAspect())), h, v.FrameRate}, nil
}

// WriteFrame writes the frame at seconds as a JPEG at most maxHeight high to
// output; false when FFmpeg found no frame there (past the end) or failed.
func WriteFrame(ctx context.Context, path string, seconds float64, output string, maxHeight int) (bool, error) {
	ffmpeg, _ := Locate()
	if ffmpeg == "" {
		return false, errNotFound
	}
	abs, _ := filepath.Abs(path)
	r, err := RunFfmpeg(ctx, ffmpeg, FrameAt(abs, max(0, seconds), output, maxHeight, nil), nil, frameStall)
	if err != nil {
		return false, err
	}
	return r.ExitCode == 0 && fileSize(output) > 0, nil
}

// ThumbnailTime is the time of the timeline's picture index of count: the middle of its part.
func ThumbnailTime(index, count int, duration float64) float64 {
	if count <= 0 || duration <= 0 {
		return 0
	}
	return duration * (float64(index) + 0.5) / float64(count)
}
