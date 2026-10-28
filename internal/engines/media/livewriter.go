package media

// Video → a Live / Motion Photo of a short clip: an iPhone pair (a JPG or HEIC
// and a MOV with the same name and content identifier) or an Android Motion
// Photo (one JPG with the clip appended). Everything is written in the work
// directory and moved into place at the end, the pair together.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/heic"
	"github.com/dseif0x/filee-go/internal/presets"
)

// LiveFps is the frame rate of the clip: what phones record Live and Motion Photos at.
const LiveFps = 30

// LiveShortSide is the clip's shorter side unless the preset asks for less (an iPhone's is 1440 × 1920).
const LiveShortSide = 1440

// liveToneMap turns HDR (PQ or HLG) into SDR BT.709: phones show the video
// next to an SDR photo. Needs FFmpeg's zscale filter.
const liveToneMap = "zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=tv,format=yuv420p,"

// LiveClip is a clip of a video: start and length in seconds, and the frame (at 30 fps) that becomes the photo.
type LiveClip struct {
	Start, Length float64
	CoverFrame    int
}

// LiveClipOf is the clip the preset asks for in a video of duration seconds
// (0 when unknown): a length of 0.5–5 s (shorter when the video ends sooner)
// and the photo's frame within it.
func LiveClipOf(o *presets.MediaOptions, duration float64) (LiveClip, error) {
	fin := func(v, def float64) float64 {
		if finite(v) {
			return v
		}
		return def
	}
	start := max(0, fin(o.LivePhotoStart, 0))
	length := min(max(fin(o.LivePhotoLength, 3), presets.MinLivePhotoLength), presets.MaxLivePhotoLength)
	if duration > 0 {
		// At least two frames, or there is nothing to move.
		if start > 0 && duration-start < 2.0/LiveFps {
			return LiveClip{}, convert.Refuse("error.live_photo_start", "")
		}
		length = min(length, duration-start)
	}
	last := max(0, int(math.Ceil(length*LiveFps-1e-6))-1)
	cover := max(0, fin(o.LivePhotoCover, length/2))
	return LiveClip{start, length, min(int(math.Floor(cover*LiveFps+1e-6)), last)}, nil
}

// LiveClipSize is the picture as shown, scaled down so its shorter side is at most maxShortSide, even sides.
func LiveClipSize(v *VideoStream, maxShortSide int) (int, int) {
	sar := v.SampleAspect
	if sar <= 0 {
		sar = 1
	}
	w, h := float64(v.Width)*sar, float64(v.Height)
	if v.IsRotated() {
		w, h = h, w
	}
	scale := min(1, float64(maxShortSide)/max(1, min(w, h)))
	return Even(w * scale), Even(h * scale)
}

func liveSeconds(s float64) string {
	return strconv.FormatFloat(math.RoundToEven(s*1e6)/1e6, 'f', -1, 64)
}

// LiveClipArgs write the clip as a QuickTime movie (iPhone) or MP4 (Android):
// 30 fps, 8-bit 4:2:0 H.264 (HEVC when asked, hvc1 for Apple), AAC. The movie
// header stays at the end, where TagMovie rewrites it.
func LiveClipArgs(input, output, muxer string, c LiveClip, v *VideoStream, a *AudioStream, o *presets.MediaOptions, encoder, toneMap string) []string {
	side := LiveShortSide
	if o.MaxHeight > 0 {
		side = min(o.MaxHeight, LiveShortSide)
	}
	w, h := LiveClipSize(v, side)
	args := append(common(), "-ss", liveSeconds(c.Start), "-i", input, "-map", "0:V:0")
	if a != nil {
		args = append(args, "-map", "0:a:0")
	}
	args = append(args, "-t", liveSeconds(c.Length), "-vf", fmt.Sprintf("%sfps=%d,scale=%d:%d,setsar=1,format=yuv420p", toneMap, LiveFps, w, h))
	if encoder == "libx265" {
		args = append(args, "-c:v", "libx265", "-preset", "medium", "-crf", itoa(choose(o.Quality, 22, 26, 30)), "-x265-params", "log-level=error", "-tag:v", "hvc1")
	} else {
		args = append(args, "-c:v", "libx264", "-preset", "medium", "-crf", itoa(choose(o.Quality, 18, 23, 28)), "-profile:v", "high")
	}
	args = append(args, "-pix_fmt", "yuv420p")
	if toneMap != "" {
		args = append(args, "-color_primaries", "bt709", "-color_trc", "bt709", "-colorspace", "bt709")
	}
	if a == nil {
		args = append(args, "-an")
	} else {
		br := 160
		if o.AudioBitrateKbps > 0 {
			br = o.AudioBitrateKbps
		} else if o.Quality == presets.MediaSmall {
			br = 96
		}
		ch := "2"
		if a.Channels == 1 {
			ch = "1"
		}
		rate := "48000"
		if a.SampleRate == 44100 || a.SampleRate == 48000 {
			rate = itoa(a.SampleRate)
		}
		args = append(args, "-c:a", "aac", "-b:a", itoa(br)+"k", "-ac", ch, "-ar", rate)
	}
	return append(args, "-sn", "-dn", "-map_chapters", "-1", "-map_metadata", "0", "-metadata", "major_brand=", "-metadata", "minor_version=",
		"-metadata", "compatible_brands=", "-max_muxing_queue_size", "4096", "-f", muxer, "-progress", "pipe:1", output)
}

// LiveCoverArgs take frame of the clip at full size by the same 30 fps timing
// as the clip, so it is exactly the frame the still image time points at.
func LiveCoverArgs(input, output string, start float64, frame int, v *VideoStream, toneMap string) []string {
	square := ""
	if math.Abs(v.SampleAspect-1) > 0.01 && v.SampleAspect > 0 {
		square = "scale=w='trunc(iw*sar/2)*2':h=ih,setsar=1,"
	}
	return append(common(), "-ss", liveSeconds(start), "-i", input, "-map", "0:V:0",
		"-vf", fmt.Sprintf(`%sfps=%d,select=eq(n\,%d),%sformat=rgb24`, toneMap, LiveFps, frame, square),
		"-frames:v", "1", "-fps_mode", "passthrough", "-update", "1", "-c:v", "png", "-f", "image2", "-progress", "pipe:1", output)
}

// AllocatePair returns the paths of an iPhone pair: the photo where the output
// rules put it and the MOV next to it with the same name. When that MOV is
// taken both get the next number. nil when the rules skip the photo.
func AllocatePair(out convert.OutputAllocator, photoExt, input string) ([]string, error) {
	for attempt := 1; attempt <= 100; attempt++ {
		suffix := ""
		if attempt > 1 {
			suffix = fmt.Sprintf(" (%d)", attempt)
		}
		photo := out.Allocate(photoExt, suffix)
		if photo == "" {
			return nil, nil
		}
		movie := strings.TrimSuffix(photo, filepath.Ext(photo)) + ".mov"
		if samePath(movie, input) || isDir(movie) {
			continue
		}
		if exists(photo) || !exists(movie) {
			return []string{photo, movie}, nil
		}
	}
	return nil, errors.New("No free name for the Live Photo's photo and video.")
}

// CommitFiles moves finished files into place: first next to their targets
// under a hidden name (a move to another drive copies, which can fail without
// touching the user's files), then each renamed over its target.
func CommitFiles(files [][2]string) error {
	var staged [][2]string
	defer func() {
		for _, s := range staged {
			os.Remove(s[0])
		}
	}()
	for _, f := range files {
		stage := filepath.Join(filepath.Dir(f[1]), ".filee-"+randomName()+filepath.Ext(f[1]))
		staged = append(staged, [2]string{stage, f[1]})
		if err := moveFile(f[0], stage); err != nil {
			return err
		}
	}
	for _, s := range staged {
		if err := os.Rename(s[0], s[1]); err != nil {
			return err
		}
	}
	return nil
}

func (c *Converter) livePhoto(ctx context.Context, ffmpeg, ffprobe string, step *convert.Step, progress convert.Progress) ([]string, error) {
	o := &step.Preset.Media
	apple := o.LivePhoto == presets.LivePhotoApple
	input, _ := filepath.Abs(step.InputPath)
	info, err := Probe(ctx, ffprobe, input)
	if err != nil {
		return nil, err
	}
	v := info.Video
	if v == nil {
		return nil, ErrNoVideo
	}
	clip, err := LiveClipOf(o, info.Seconds())
	if err != nil {
		return nil, err
	}
	toneMap := ""
	if v.IsHdr() {
		if !HasFilter(ffmpeg, "zscale") {
			return nil, convert.Refuse("error.live_photo_hdr", "")
		}
		toneMap = liveToneMap
	}
	progress.Report(0.02)

	// The names first: a file the output rules skip costs no encoding.
	photoExt := "jpg"
	if step.To == "heic" {
		photoExt = "heic"
	}
	var targets []string
	if apple {
		if targets, err = AllocatePair(step.Output, photoExt, input); err != nil {
			return nil, err
		}
	} else if p := step.Output.Allocate("jpg", ".MP"); p != "" {
		targets = []string{p}
	}
	if targets == nil {
		return nil, nil
	}
	token := randomName()
	ext, muxer := ".mp4", "mp4"
	if apple {
		ext, muxer = ".mov", "mov"
	}
	movie := filepath.Join(step.WorkDir, token+ext)
	pngPath := filepath.Join(step.WorkDir, token+".png")
	photo := filepath.Join(step.WorkDir, token+"."+photoExt)

	enc, err := VideoEncoder("mov", "libx264", o.VideoCodec, EncodersOf(ffmpeg))
	if err != nil {
		return nil, err
	}
	audio := info.Audio
	if o.RemoveAudio {
		audio = nil
	}
	if err := runPass(ctx, ffmpeg, Pass{LiveClipArgs(input, movie, muxer, clip, v, audio, o, enc, toneMap), 0.8}, clip.Length, progress, 0); err != nil {
		return nil, err
	}
	// The photo's frame, within the frames the clip really has.
	frames, err := movieFrames(movie)
	if err != nil {
		return nil, err
	}
	if frames == 0 {
		return nil, errors.New("FFmpeg wrote a clip without frames.")
	}
	cover := min(clip.CoverFrame, int(frames)-1)
	if err := runPass(ctx, ffmpeg, Pass{LiveCoverArgs(input, pngPath, clip.Start, cover, v, toneMap), 0.1}, 0, progress, 0.8); err != nil {
		return nil, err
	}
	if !exists(pngPath) {
		return nil, errors.New("FFmpeg did not write the photo's frame.")
	}
	quality := min(max(step.Preset.Image.Quality, 1), 100)
	coverSeconds := float64(cover) / LiveFps
	if apple {
		id := strings.ToUpper(uuid.NewString())
		exif := MakerNoteExif(MakerNote(id))
		var data []byte
		if photoExt == "heic" {
			w, h, err := pngSize(pngPath)
			if err != nil {
				return nil, err
			}
			data, err = heic.Encode(ctx, ffmpeg, pngPath, w, h, nil, exif, quality, step.WorkDir)
			if err != nil {
				return nil, err
			}
		} else if data, err = pngToJpeg(pngPath, quality, exif); err != nil {
			return nil, err
		}
		if err := os.WriteFile(photo, data, 0o644); err != nil {
			return nil, err
		}
		if err := TagMovie(movie, id, coverSeconds); err != nil {
			return nil, err
		}
		if err := CommitFiles([][2]string{{movie, targets[1]}, {photo, targets[0]}}); err != nil {
			return nil, err
		}
		if step.Notice != nil {
			step.Notice("notice.live_photo_pair")
		}
	} else {
		data, err := pngToJpeg(pngPath, quality, nil)
		if err != nil {
			return nil, err
		}
		if err := WriteMotionPhoto(data, movie, photo, int64(math.RoundToEven(coverSeconds*1e6))); err != nil {
			return nil, err
		}
		if err := CommitFiles([][2]string{{photo, targets[0]}}); err != nil {
			return nil, err
		}
	}
	progress.Report(1)
	return targets, nil
}

func movieFrames(path string) (uint32, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	_, moov, err := ReadMovieHeader(f, 0)
	if err != nil {
		return 0, err
	}
	return VideoSampleCount(moov)
}

func pngSize(path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	c, err := png.DecodeConfig(f)
	return c.Width, c.Height, err
}

// pngToJpeg encodes a PNG as a JPEG at quality with exif (TIFF data) when given.
func pngToJpeg(path string, quality int, exif []byte) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		return nil, err
	}
	return encodeJpeg(img, quality, exif)
}

func encodeJpeg(img image.Image, quality int, exif []byte) ([]byte, error) {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	if exif == nil {
		return b.Bytes(), nil
	}
	return WithExif(b.Bytes(), exif)
}
