package media

// Real conversions with FFmpeg; skipped when it is not installed (set
// FILEE_FFMPEG or FILEE_ENGINES_DIR to a copy to run them).

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/presets"
)

func ffmpegOrSkip(t *testing.T) (string, string) {
	t.Helper()
	f, p := Locate()
	if f == "" {
		t.Skip("FFmpeg is not installed")
	}
	return f, p
}

// testVideo is a 2 s 160×120 test picture with a sine tone (or silent).
func testVideo(t *testing.T, dir, name string, audio bool, extra ...string) string {
	t.Helper()
	ffmpeg, _ := ffmpegOrSkip(t)
	out := filepath.Join(dir, name)
	args := append(common(), "-f", "lavfi", "-i", "testsrc2=size=160x120:rate=30")
	if audio {
		args = append(args, "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000")
	}
	args = append(args, "-t", "2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac")
	args = append(append(args, extra...), out)
	if err := runChecked(context.Background(), ffmpeg, args, nil, 0); err != nil {
		t.Fatal(err)
	}
	return out
}

type run struct {
	t       *testing.T
	dir     string
	outputs []string
	notices []string
}

func (r *run) step(input, from, to string, p *presets.Preset) *convert.Step {
	return &convert.Step{InputPath: input, From: from, To: to, Preset: p, WorkDir: r.dir,
		Notice: func(k string) { r.notices = append(r.notices, k) },
		Output: convert.AllocatorFunc(func(ext, suffix string) string {
			name := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input)) + suffix
			if ext != "" {
				name += "." + ext
			}
			p := filepath.Join(r.dir, "out", name)
			os.MkdirAll(filepath.Dir(p), 0o755)
			return p
		})}
}

func (r *run) convert(input, from, to string, p *presets.Preset) []string {
	r.t.Helper()
	got, err := New().Convert(context.Background(), r.step(input, from, to, p), func(float64) {})
	if err != nil {
		r.t.Fatalf("%s → %s: %v", from, to, err)
	}
	if len(got) == 0 {
		r.t.Fatalf("%s → %s: nothing written", from, to)
	}
	return got
}

func probeOf(t *testing.T, path string) *MediaInfo {
	t.Helper()
	_, ffprobe := Locate()
	info, err := Probe(context.Background(), ffprobe, path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func TestRealVideoAndAudioConversions(t *testing.T) {
	dir := t.TempDir()
	src := testVideo(t, dir, "clip ü.mp4", true)
	r := &run{t: t, dir: dir}
	p := presets.New("p", "mkv")

	mkv := r.convert(src, "mp4", "mkv", p)[0]
	if i := probeOf(t, mkv); i.Video.Codec != "h264" || i.Audio == nil || i.Seconds() < 1.9 {
		t.Errorf("mkv: %+v", i.Video)
	}
	webm := r.convert(src, "mp4", "webm", p)[0]
	if i := probeOf(t, webm); i.Video.Codec != "vp9" || i.Audio.Codec != "opus" {
		t.Error("webm codecs", i.Video.Codec)
	}
	hevc := presets.New("h", "mp4")
	hevc.Media.VideoCodec = presets.VideoHevc
	if i := probeOf(t, r.convert(webm, "webm", "mp4", hevc)[0]); i.Video.Codec != "hevc" {
		t.Error("hevc", i.Video.Codec)
	}
	for _, to := range []string{"mp3", "m4a", "wav", "flac", "ogg", "opus"} {
		out := r.convert(src, "mp4", to, p)[0]
		if i := probeOf(t, out); i.Audio == nil || i.Video != nil {
			t.Error(to)
		}
	}
	gif := r.convert(src, "mp4", "gif", p)[0]
	if b, _ := os.ReadFile(gif); !bytes.HasPrefix(b, []byte("GIF89a")) {
		t.Error("gif")
	}
	still := r.convert(src, "mp4", "png", p)[0]
	f, _ := os.Open(still)
	cfg, err := png.DecodeConfig(f)
	f.Close()
	if err != nil || cfg.Width != 160 {
		t.Error("poster frame", err)
	}
	// The still picture back to a video of five seconds.
	if i := probeOf(t, r.convert(still, "png", "mov", p)[0]); i.Seconds() < 4.9 || i.Video.Codec != "h264" {
		t.Error("still video", i.Seconds())
	}
	// Without metadata: same format, streams copied.
	tagged := testVideo(t, dir, "tagged.mp4", true, "-metadata", "title=Secret")
	clean := presets.New("c", "mp4")
	clean.Media.RemoveMetadata = true
	out := r.convert(tagged, "mp4", "mp4", clean)[0]
	if b, _ := os.ReadFile(out); bytes.Contains(b, []byte("Secret")) {
		t.Error("title kept")
	}
	// No subtitles: a clear error, nothing left behind.
	_, err = New().Convert(context.Background(), r.step(src, "mp4", "srt", p), nil)
	if err == nil || !strings.Contains(err.Error(), "no subtitle track") {
		t.Error(err)
	}
	// Size limit far too small: refused before encoding.
	small := presets.New("s", "mp4")
	small.Media.MaxSizeMb = 0.001
	_, err = New().Convert(context.Background(), r.step(src, "mp4", "mp4", small), nil)
	if re, ok := err.(*convert.RefusedError); !ok || re.Key != "error.over_size_limit" {
		t.Error(err)
	}
}

func TestRealFramesClipsAndJoins(t *testing.T) {
	dir := t.TempDir()
	src := testVideo(t, dir, "clip.mp4", true)
	r := &run{t: t, dir: dir}
	frames := presets.New("f", "png")
	frames.Media.SaveEveryFrame = true
	frames.Media.FrameInterval = 10
	st := r.step(src, "mp4", "png", frames)
	st.FrameSequence = true
	got, err := New().Convert(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	if e, _ := os.ReadDir(got[0]); len(e) != 6 || !strings.HasSuffix(got[0], "_frames") {
		t.Errorf("%s: %d frames", got[0], len(e))
	}

	clip := presets.New("c", "mp4")
	clip.Media.Clip = &presets.ClipEdit{Parts: []presets.ClipPart{
		{Start: 0, End: 0.5, Volume: 1, Speed: 1}, {Start: 0.5, End: 1, Blank: true, Volume: 1, Speed: 1}, {Start: 1, End: 2, Volume: 0.5, Speed: 2}},
		FadeIn: 0.2}
	cut := r.convert(src, "mp4", "mp4", clip)[0]
	if i := probeOf(t, cut); i.Seconds() < 1.4 || i.Seconds() > 1.7 || i.Audio == nil {
		t.Error("clip length", i.Seconds())
	}
	plain := presets.New("c", "mkv")
	plain.Media.Clip = presets.ClipOf(0.5, 1.5)
	if i := probeOf(t, r.convert(src, "mp4", "mkv", plain)[0]); i.Seconds() > 2.1 {
		t.Error("copied clip", i.Seconds())
	}

	// Joins: the same clips are copied, different ones encoded.
	other := testVideo(t, dir, "other.mp4", false, "-s", "320x200")
	join := presets.New("j", "mp4")
	join.Media.JoinIntoOne = true
	for _, inputs := range [][]string{{src, src}, {src, other}} {
		out := filepath.Join(dir, fmt.Sprintf("joined-%d.mp4", len(r.outputs)))
		r.outputs = append(r.outputs, out)
		if err := New().JoinMedia(context.Background(), inputs, out, "mp4", join, dir, nil); err != nil {
			t.Fatal(err)
		}
		if i := probeOf(t, out); i.Seconds() < 3.9 || i.Video.Width != 160 {
			t.Error("join", i.Seconds(), i.Video.Width)
		}
	}
	m4a := filepath.Join(dir, "joined.m4a")
	if err := New().JoinMedia(context.Background(), []string{src, other}, m4a, "m4a", join, dir, nil); err != nil {
		t.Fatal(err)
	}
	if i := probeOf(t, m4a); i.Audio == nil || i.Seconds() < 3.9 {
		t.Error("audio join")
	}
	// A slideshow of two pictures with a crossfade: 3 + 3 − 1 s.
	show := presets.New("s", "mp4")
	show.Media.Slideshow = true
	a := writeTestPicture(t, dir, "a.png", 400, 300)
	b := writeTestPicture(t, dir, "b.png", 300, 400)
	out := filepath.Join(dir, "show.mp4")
	if err := New().JoinMedia(context.Background(), []string{a, b}, out, "mp4", show, dir, nil); err != nil {
		t.Fatal(err)
	}
	if i := probeOf(t, out); i.Video.Width != 1920 || i.Seconds() < 4.9 || i.Seconds() > 5.2 {
		t.Error("slideshow", i.Video.Width, i.Seconds())
	}
}

func writeTestPicture(t *testing.T, dir, name string, w, h int) string {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 90, 255})
		}
	}
	p := filepath.Join(dir, name)
	f, _ := os.Create(p)
	if strings.HasSuffix(name, ".jpg") {
		jpeg.Encode(f, img, nil)
	} else {
		png.Encode(f, img)
	}
	f.Close()
	return p
}

func TestRealHeicAndLivePhotos(t *testing.T) {
	ffmpegOrSkip(t)
	dir := t.TempDir()
	r := &run{t: t, dir: dir}
	p := presets.New("h", "heic")
	// One coded picture, and a grid of tiles.
	for _, size := range [][2]int{{300, 200}, {1100, 600}} {
		pic := writeTestPicture(t, dir, fmt.Sprintf("p%d.jpg", size[0]), size[0], size[1])
		out := r.convert(pic, "jpg", "heic", p)[0]
		data, _ := os.ReadFile(out)
		if string(data[4:12]) != "ftypheic" {
			t.Fatal("not a heic")
		}
		// FFmpeg reads it back at its size (the grid crops the padding).
		if i := probeOf(t, out); i.Video == nil {
			t.Error("unreadable heic")
		}
	}

	src := testVideo(t, dir, "IMG_0042.mp4", true)
	apple := presets.New("l", "jpg")
	apple.Media.LivePhoto = presets.LivePhotoApple
	st := r.step(src, "mp4", "jpg", apple)
	st.LivePhoto = true
	pair, err := New().Convert(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pair) != 2 || !strings.HasSuffix(pair[1], ".mov") {
		t.Fatal(pair)
	}
	id := ImageIdentifier(pair[0])
	movID, err := MovieIdentifier(pair[1])
	if id == "" || err != nil || movID != id {
		t.Errorf("identifiers %q %q %v", id, movID, err)
	}
	if i := probeOf(t, pair[1]); i.Video == nil || i.Seconds() < 1.9 {
		t.Error("live movie")
	}
	// And back: the photo gives its movie (the pair's MOV is never overwritten).
	back := presets.New("b", "mp4")
	back.Media.ExtractMotionPhoto = true
	st = r.step(pair[0], "jpg", "mp4", back)
	st.MotionPhoto = true
	v, err := New().Convert(context.Background(), st, nil)
	if err != nil || len(v) != 1 {
		t.Fatal(v, err)
	}

	android := presets.New("a", "jpg")
	android.Media.LivePhoto = presets.LivePhotoAndroid
	android.Media.LivePhotoLength = 1
	st = r.step(src, "mp4", "jpg", android)
	st.LivePhoto = true
	mp, err := New().Convert(context.Background(), st, nil)
	if err != nil || !strings.HasSuffix(mp[0], ".MP.jpg") {
		t.Fatal(mp, err)
	}
	data, _ := os.ReadFile(mp[0])
	off, l, ok, err := FindMotionVideo(bytes.NewReader(data))
	if !ok || err != nil {
		t.Fatal(ok, err)
	}
	video := filepath.Join(dir, "embedded.mp4")
	os.WriteFile(video, data[off:off+l], 0o644)
	if i := probeOf(t, video); i.Seconds() < 0.9 || i.Seconds() > 1.2 {
		t.Error("motion clip length", i.Seconds())
	}
	heicLive := presets.New("l", "heic")
	heicLive.Media.LivePhoto = presets.LivePhotoApple
	st = r.step(src, "mp4", "heic", heicLive)
	st.LivePhoto = true
	hp, err := New().Convert(context.Background(), st, nil)
	if err != nil || ImageIdentifier(hp[0]) == "" {
		t.Error("heic live photo", hp, err)
	}
}

func TestRealPreviewHelpers(t *testing.T) {
	dir := t.TempDir()
	src := testVideo(t, dir, "v.mp4", true)
	s, err := ProbeVideo(context.Background(), src)
	if err != nil || s.Width != 160 || s.Height != 120 || s.Duration < 1.9 {
		t.Fatal(s, err)
	}
	frame := filepath.Join(dir, "f.jpg")
	if ok, err := WriteFrame(context.Background(), src, 1, frame, 60); !ok || err != nil {
		t.Error(ok, err)
	}
	loud, err := ReadLoudness(context.Background(), src, 0, 0, 10, dir)
	if err != nil || len(loud) != 10 {
		t.Error(loud, err)
	}
	n := 0
	err = PlayClip(context.Background(), src, presets.ClipOf(0, 0.5), 60, func(b []byte, w, h int, at float64) { n++ })
	if err != nil || n < 10 {
		t.Error("played", n, err)
	}
	if l, err := ReadSoundLength(context.Background(), src); err != nil || l < 1.9 {
		t.Error(l, err)
	}
	if _, err := FindSilences(context.Background(), src, 0, 2); err != nil {
		t.Error(err)
	}
}
