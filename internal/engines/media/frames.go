package media

// Video → every frame as a picture ("Save every frame"): numbered PNG or JPG
// files in one folder. The pictures go into a hidden folder next to the
// output, moved into place when FFmpeg is done, so a cancelled or failed run
// leaves nothing behind.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/presets"
)

// FramesFolderSuffix names the output folder: "clip.mp4" → "clip_frames".
const FramesFolderSuffix = "_frames"

func writeFrames(ctx context.Context, ffmpeg string, step *convert.Step, input string, info *MediaInfo, progress convert.Progress) ([]string, error) {
	v := info.Video
	if v == nil {
		return nil, ErrNoVideo
	}
	ext := formats.ByID(step.To).PrimaryExtension()
	interval := max(1, step.Preset.Media.FrameInterval)
	count, known := EstimatedFrames(info, interval)
	if known && count > presets.MaxFrames {
		return nil, convert.Refuse("error.too_many_frames", fmt.Sprintf("%s > %s", thousands(count), thousands(presets.MaxFrames)))
	}
	output := step.Output.Allocate("", FramesFolderSuffix)
	if output == "" {
		return nil, nil
	}
	if known {
		if free, ok := freeSpace(filepath.Dir(output)); ok {
			if need := EstimatedBytes(v, ext, count); need > free {
				return nil, convert.Refuse("error.not_enough_space", fmt.Sprintf("%.1f GB > %.1f GB", float64(need)/1e9, float64(free)/1e9))
			}
		}
	}
	// Same folder as the output (same drive, so the move is instant).
	staging := filepath.Join(filepath.Dir(output), "."+filepath.Base(output)+".filee-"+randomName()[:8])
	frames := filepath.Join(staging, "frames")
	if err := os.MkdirAll(frames, 0o755); err != nil {
		return nil, err
	}
	hideFolder(staging)
	defer os.RemoveAll(staging)
	// FFmpeg reads % in the whole path as part of the number pattern.
	pattern := filepath.Join(strings.ReplaceAll(frames, "%", "%%"), "frame_%06d."+ext)
	args := FrameSequence(input, pattern, ext, v, step.Preset.Image.Quality, interval)
	if err := runPass(ctx, ffmpeg, Pass{args, 1}, info.Seconds(), progress, 0); err != nil {
		return nil, err
	}
	if entries, _ := os.ReadDir(frames); len(entries) == 0 {
		return nil, errors.New("FFmpeg did not write any frames.")
	}
	if exists(output) {
		if err := os.RemoveAll(output); err != nil {
			return nil, err
		}
	}
	if err := os.Rename(frames, output); err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{output}, nil
}

// EstimatedFrames is duration × frame rate ÷ interval; known is false when the file doesn't say.
func EstimatedFrames(info *MediaInfo, interval int) (int64, bool) {
	if info.Seconds() <= 0 || info.Video == nil || info.Video.FrameRate <= 0 {
		return 0, false
	}
	return int64(math.Ceil(info.Seconds() * info.Video.FrameRate / float64(max(1, interval)))), true
}

// EstimatedBytes is a generous guess of the pictures' size: PNG about half of
// raw RGB (twice that for 10-bit), JPG about a quarter byte per pixel.
func EstimatedBytes(v *VideoStream, ext string, frames int64) int64 {
	pixels := max(1, float64(v.Width)*v.SampleAspect) * float64(max(1, v.Height))
	per := pixels * 0.25
	if ext == "png" {
		per = pixels * 1.5
		if v.IsHighBitDepth() {
			per *= 2
		}
	}
	return int64(min(per*float64(frames), math.MaxInt64/2))
}

// thousands writes 100000 as "100,000" (N0).
func thousands(n int64) string {
	s := fmt.Sprint(n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}
