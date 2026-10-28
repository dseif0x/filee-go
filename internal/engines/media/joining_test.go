package media

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/presets"
)

func TestJoinCopiesOnlyMatchingClips(t *testing.T) {
	v := &VideoStream{Width: 1920, Height: 1080, SampleAspect: 1, FrameRate: 30, Codec: "h264", PixelFormat: "yuv420p", HeaderHash: "md5:a"}
	a := AudioStream{2, 48000, "aac"}
	c1, c2 := infoOf(5, v, a), infoOf(7, v, a)
	if !CanCopyVideos([]*MediaInfo{c1, c2}, opts(nil)) {
		t.Error("same clips not copied")
	}
	other := *v
	other.HeaderHash = "md5:b"
	if CanCopyVideos([]*MediaInfo{c1, infoOf(5, &other, a)}, opts(nil)) {
		t.Error("other parameter sets copied")
	}
	if CanCopyVideos([]*MediaInfo{c1, infoOf(5, v)}, opts(nil)) {
		t.Error("clip without sound copied")
	}
	eq(t, "file '/a/it'\\''s.mp4'\n", ConcatList([]string{"/a/it's.mp4"}))
	cp := CopyVideos("list.txt", "out.mp4", c1)
	eq(t, "concat", after(cp, "-f"))
	eq(t, "copy", after(cp, "-c"))
}

func TestEncodeVideosAndAudio(t *testing.T) {
	v := &VideoStream{Width: 1920, Height: 1080, SampleAspect: 1, FrameRate: 30, Codec: "h264", PixelFormat: "yuv420p"}
	portrait := &VideoStream{Width: 1080, Height: 1920, SampleAspect: 1, FrameRate: 25}
	clips := []Clip{{"a.mp4", infoOf(5, v, AudioStream{2, 44100, "aac"})}, {"b.mov", infoOf(3, portrait)}}
	a, err := EncodeVideos(clips, "out.mp4", opts(func(o *presets.MediaOptions) { o.MaxHeight = 720 }), "libx264", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g := after(a, "-filter_complex")
	contains(t, g, "[0:V:0]scale=1280:720:force_original_aspect_ratio=decrease:force_divisible_by=2,pad=1280:720:(ow-iw)/2:(oh-ih)/2:black,setsar=1,fps=30,format=yuv420p[v0]")
	contains(t, g, "aevalsrc=0:c=stereo:s=48000:d=3,")
	contains(t, g, "[v0][a0][v1][a1]concat=n=2:v=1:a=1[v][a]")
	eq(t, "192k", after(a, "-b:a"))
	m, err := EncodeAudio([]Clip{{"a.mp3", infoOf(5, nil, AudioStream{1, 44100, "mp3"})}, {"b.wav", infoOf(5, nil, AudioStream{1, 8000, "pcm"})}}, "out.m4a", opts(nil), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	contains(t, after(m, "-filter_complex"), "channel_layouts=mono")
	eq(t, "96k", after(m, "-b:a"))
	eq(t, "ipod", after(m, "-f"))
	alac, _ := EncodeAudio(clips[:1], "out.m4a", opts(func(o *presets.MediaOptions) { o.AudioCodec = presets.AudioAlac }), t.TempDir())
	eq(t, "alac", after(alac, "-c:a"))
	if _, err := EncodeAudio([]Clip{{"x", infoOf(0, nil)}}, "o", opts(nil), t.TempDir()); err == nil {
		t.Error("a silent clip of unknown length")
	}
}

func TestSlideshowCrossfades(t *testing.T) {
	o := opts(nil) // 3 s slides, 1 s fades
	a, err := Slideshow([]string{"1.jpg", "2.jpg", "3.jpg"}, "out.mp4", o, "libx264", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g := after(a, "-filter_complex")
	contains(t, g, "[0:v]scale=out_color_matrix=bt709:out_range=tv,format=yuv420p,setsar=1,fps=30,tpad=stop_mode=clone:stop_duration=3,trim=duration=3,setpts=PTS-STARTPTS[s0]")
	contains(t, g, "[s0][s1]xfade=transition=fade:duration=1:offset=2[x1]")
	contains(t, g, "[x1][s2]xfade=transition=fade:duration=1:offset=4[out]")
	eq(t, "18", after(a, "-crf")) // one step above Balanced
	eq(t, 7.0, SlideshowDuration(3, o).Seconds())
	o.Crossfade = false
	cut, _ := Slideshow([]string{"1.jpg", "2.jpg"}, "out.mp4", o, "libx264", t.TempDir())
	contains(t, after(cut, "-filter_complex"), "[s0][s1]concat=n=2:v=1:a=0[out]")
	s := PrepareSlide("in.png", "s.jpg", 1920, 1080)
	contains(t, after(s, "-vf"), "premultiply=inplace=1,scale=1920:1080:force_original_aspect_ratio=decrease:force_divisible_by=2:flags=lanczos,pad=")
}

func TestLongGraphsGoIntoAFile(t *testing.T) {
	dir := t.TempDir()
	short, _ := GraphArgs("null", dir)
	eq(t, "-filter_complex", short[0])
	long, err := GraphArgs(strings.Repeat("x", MaxInlineGraph+1), dir)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "-/filter_complex", long[0])
	if b, _ := os.ReadFile(long[1]); len(b) != MaxInlineGraph+1 || filepath.Dir(long[1]) != dir {
		t.Error("graph file")
	}
}
