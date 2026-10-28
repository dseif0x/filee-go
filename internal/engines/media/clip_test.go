package media

import (
	"slices"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/presets"
)

var movie = infoOf(600, hd, AudioStream{2, 48000, "aac"})

func parts() *presets.ClipEdit {
	return &presets.ClipEdit{
		Parts: []presets.ClipPart{
			{Start: 10, End: 20, Volume: 0.5, Speed: 1},
			{Start: 20, End: 22, Blank: true, Volume: 1, Speed: 1},
			{Start: 30, End: 40, Speed: 2, Volume: 1},
		},
		FadeIn: 1, FadeOut: 2,
	}
}

func contains(t *testing.T, s, part string) {
	t.Helper()
	if !strings.Contains(s, part) {
		t.Errorf("%q not in\n%s", part, s)
	}
}

func TestClipCopyArgs(t *testing.T) {
	a, err := ClipCopy("in.mp4", "out.mp4", "mp4", ClipRange{65.3, 95.3}, opts(nil), movie)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "65.300", after(a, "-ss"))
	eq(t, "95.300", after(a, "-to"))
	eq(t, "copy", after(a, "-c"))
	eq(t, "make_zero", after(a, "-avoid_negative_ts"))
	eq(t, "+faststart", after(a, "-movflags"))
	eq(t, "out.mp4", a[len(a)-1])
	if slices.Index(a, "-ss") > slices.Index(a, "-i") {
		t.Error("-ss after -i")
	}
	exact, _ := Plan("in.mp4", "out.mp4", "mp4", opts(nil), movie, "w", PlanOptions{Clip: &ClipRange{12.5, 42.5}})
	e := exact[0].Args
	eq(t, "12.500", after(e, "-ss"))
	eq(t, "libx264", after(e, "-c:v"))
	eq(t, "aac", after(e, "-c:a"))
	whole := args(t, "mp4", nil, infoOf(600, &VideoStream{Width: 1920, Height: 1080, SampleAspect: 1, FrameRate: 30, Codec: "h264", PixelFormat: "yuv420p"}, AudioStream{2, 48000, "aac"}))
	eq(t, "copy", after(whole, "-c:v"))
	f := FrameAt("in.mp4", 61.25, "frame.jpg", 180, nil)
	eq(t, "61.250", after(f, "-ss"))
	eq(t, "1", after(f, "-frames:v"))
	eq(t, "frame.jpg", f[len(f)-1])
}

func TestClipGraphReadsTheVideoOnce(t *testing.T) {
	g, err := ClipGraphOf("in.mp4", parts(), 1920, 1080, "30", ClipGraphOptions{Audio: true})
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "-ss 10.000 -to 40.000 -i in.mp4", strings.Join(g.Inputs, " "))
	eq(t, 17.0, g.Length)
	f := g.Filter
	contains(t, f, "[0:V:0]split=2[vs0][vs1];")
	contains(t, f, "[0:a:0]asplit=2[as0][as1];")
	contains(t, f, "[vs0]trim=start=0:end=10,setpts=PTS-STARTPTS,fps=30,scale=1920:1080,setsar=1,format=yuv420p[v0]")
	contains(t, f, "[vs1]trim=start=20:end=30,setpts=(PTS-STARTPTS)/2,")
	contains(t, f, "[as0]atrim=start=0:end=10,asetpts=PTS-STARTPTS,volume=0.5,")
	contains(t, f, "[as1]atrim=start=20:end=30,asetpts=PTS-STARTPTS,atempo=2,")
	contains(t, f, "color=c=black:s=1920x1080:r=30:d=2,setsar=1,format=yuv420p[v1]")
	contains(t, f, "anullsrc=r=48000:cl=stereo,atrim=duration=2[a1]")
	contains(t, f, "[v0][a0][v1][a1][v2][a2]concat=n=3:v=1:a=1[vc][ac]")
	contains(t, f, "[vc]fade=t=in:st=0:d=1,fade=t=out:st=15:d=2[v]")

	one := &presets.ClipEdit{Parts: []presets.ClipPart{{Start: 0, End: 4, Speed: 0.5, Volume: 1}}}
	s, _ := ClipGraphOf("in.mp4", one, 640, 360, "25", ClipGraphOptions{})
	if s.HasAudio || strings.Contains(s.Filter, "[0:a:0]") || !strings.HasSuffix(s.Filter, "concat=n=1:v=1:a=0[vc];[vc]null[v]") {
		t.Error(s.Filter)
	}
	eq(t, 8.0, s.Length)
}

func TestTempoSteps(t *testing.T) {
	for _, c := range []struct {
		speed float64
		want  []float64
	}{{1, nil}, {2, []float64{2}}, {0.75, []float64{0.75}}, {3, []float64{2, 1.5}}, {4, []float64{2, 2}}, {0.25, []float64{0.5, 0.5}}} {
		if got := Tempo(c.speed); !slices.Equal(got, c.want) {
			t.Errorf("%v: %v", c.speed, got)
		}
	}
}

func TestClipEditArgs(t *testing.T) {
	a, err := ClipEditArgs("in.mp4", "out.mp4", "mp4", parts(), opts(func(o *presets.MediaOptions) { o.Quality = presets.MediaSmall; o.MaxHeight = 720 }), movie, nil)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "[v]", after(a, "-map"))
	if !slices.Contains(a, "[a]") {
		t.Error("no [a]")
	}
	contains(t, after(a, "-filter_complex"), "scale=1280:720")
	eq(t, "28", after(a, "-crf"))
	eq(t, "aac", after(a, "-c:a"))
	silent, _ := ClipEditArgs("in.mp4", "out.webm", "webm", parts(), opts(func(o *presets.MediaOptions) { o.RemoveAudio = true }), movie, nil)
	if slices.Contains(silent, "[a]") || !slices.Contains(silent, "-an") || after(silent, "-c:v") != "libvpx-vp9" {
		t.Error(silent)
	}
	r := *hd
	r.Rotation = 90
	w, h := ClipSize(&r, 0)
	eq(t, [2]int{1080, 1920}, [2]int{w, h})
	r.Rotation = -90
	w, h = ClipSize(&r, 720)
	eq(t, [2]int{404, 720}, [2]int{w, h})
	vob, _ := ClipEditArgs("in.mp4", "out.vob", "vob", parts(), opts(nil), movie, nil)
	contains(t, after(vob, "-filter_complex"), "[vd]scale=720:")
}

func TestPreviewArgs(t *testing.T) {
	p, w, h, err := ClipPreviewVideo("in.mp4", parts(), hd, 360, 25)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, [2]int{640, 360}, [2]int{w, h})
	eq(t, "rawvideo", after(p, "-f"))
	contains(t, after(p, "-filter_complex"), "format=bgra")
	eq(t, "pipe:1", p[len(p)-1])
	s, _ := ClipPreviewAudio("in.mp4", parts(), hd, true)
	eq(t, "[a]", after(s, "-map"))
	eq(t, "wav", after(s, "-f"))
}

func TestMusicIsMixedUnderTheClip(t *testing.T) {
	e := parts()
	e.Parts[0].Muted = true
	e.Music = &presets.ClipMusic{Path: "song.mp3", Offset: 2.5, From: 30, Length: 100, Volume: 0.5, Fade: true}
	g, err := ClipGraphOf("in.mp4", e, 1920, 1080, "30", ClipGraphOptions{Audio: true})
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "-ss 10.000 -to 40.000 -i in.mp4 -ss 30.000 -i song.mp3", strings.Join(g.Inputs, " "))
	contains(t, g.Filter, "[as0]atrim=start=0:end=10,asetpts=PTS-STARTPTS,volume=0,")
	contains(t, g.Filter, "[1:a:0]atrim=duration=14.5,asetpts=PTS-STARTPTS,volume=0.5,afade=t=in:st=0:d=1,afade=t=out:st=13.5:d=1,adelay=2500:all=1,")
	contains(t, g.Filter, "[ac][m]amix=inputs=2:duration=first:dropout_transition=0:normalize=0[am]")
	sv, _ := ClipGraphOf("in.mp4", e, 1920, 1080, "30", ClipGraphOptions{})
	contains(t, sv.Filter, "anullsrc=r=48000:cl=stereo,atrim=duration=17[ac]")
	a, _ := ClipPreviewAudio("in.mp4", e, hd, false)
	if !slices.Contains(a, "song.mp3") {
		t.Error("no music in the preview")
	}
}

func TestClampClip(t *testing.T) {
	e := &presets.ClipEdit{Parts: []presets.ClipPart{
		{Start: 1, End: 1.5, Blank: true, Volume: 1, Speed: 1},
		{Start: 5, End: 30, Volume: 9, Speed: 10},
		{Start: 20, End: 25, Volume: 1, Speed: 1}, // overlaps: starts at 30, beyond the end → dropped
		{Start: 40, End: 40.05, Volume: 1, Speed: 1},
	}, FadeIn: 100}
	c, err := ClampClip(e, 28)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Parts) != 1 || c.Parts[0].Start != 5 || c.Parts[0].End != 28 || c.Parts[0].Speed != 4 || c.Parts[0].Volume != 2 {
		t.Fatalf("%+v", c.Parts)
	}
	eq(t, 23.0/4/2, c.FadeIn)
	if _, err := ClampClip(&presets.ClipEdit{Parts: []presets.ClipPart{{Start: 50, End: 60}}}, 10); err == nil {
		t.Error("nothing left accepted")
	}
	if !(&presets.ClipEdit{Parts: []presets.ClipPart{{Start: 1, End: 2, Volume: 1, Speed: 1}}}).IsPlainCut() {
		t.Error("plain cut")
	}
}

func TestSilencesAndLoudness(t *testing.T) {
	log := `[silencedetect @ 0000] silence_start: 0
[silencedetect @ 0000] silence_end: 1.5 | silence_duration: 1.5
[silencedetect @ 0000] silence_start: 4.25
[silencedetect @ 0000] silence_end: 6 | silence_duration: 1.75
[silencedetect @ 0000] silence_start: 9`
	s := ParseSilences(log, 60, 72)
	if !slices.Equal(s, []ClipRange{{60, 61.5}, {64.25, 66}, {69, 72}}) {
		t.Error(s)
	}
	a := SilenceArgs("in.mp4", 60, 72)
	eq(t, "60.000", after(a, "-ss"))
	eq(t, "silencedetect=noise=-35dB:d=0.4", after(a, "-af"))
	l := Loudness([]float32{0, 0, 0.5, 0.5, 1, 1}, 2, 3, 3)
	if !slices.Equal(l, []float32{0, 0.5, 1}) {
		t.Error(l)
	}
	if Loudness(nil, 2, 3, 3) != nil {
		t.Error("no samples")
	}
}
