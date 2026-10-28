package presets

import (
	"encoding/json"
	"math"
	"slices"
	"testing"
)

func cutter() *ClipCutter { return NewClipCutter(312.4, ClipRange{60, 90}) }

type se struct{ s, e float64 }

func spans(c *ClipCutter) []se {
	var out []se
	for _, p := range c.Pieces() {
		out = append(out, se{round(p.Start), round(p.End)})
	}
	return out
}

func round(v float64) float64 { return math.Round(v*1e6) / 1e6 }

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func mustEdit(t *testing.T, c *ClipCutter) *ClipEdit {
	t.Helper()
	e, err := c.ToEdit()
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestANewClipIsOnePieceAsRecorded(t *testing.T) {
	c := cutter()
	if !slices.Equal(c.Pieces(), []ClipPiece{NewClipPiece(60, 90)}) || c.Length() != 30 || c.CanUndo() {
		t.Fatal(c.Pieces())
	}
	e := mustEdit(t, c)
	if !e.IsPlainCut() || e.Start() != 60 || e.End() != 90 {
		t.Fatal(e)
	}
}

func TestTheBladeCutsAPieceInTwoButNotNextToACut(t *testing.T) {
	c := cutter()
	if !c.Split(70) || !slices.Equal(c.Pieces(), []ClipPiece{NewClipPiece(60, 70), NewClipPiece(70, 90)}) {
		t.Fatal(c.Pieces())
	}
	if c.Split(70.05) || c.Split(95) || c.Split(60) {
		t.Fatal("split where it shouldn't")
	}
	if c.IndexAt(65) != 0 || c.IndexAt(70) != 1 || c.IndexAt(50) != -1 {
		t.Fatal("IndexAt")
	}
	if !mustEdit(t, c).IsPlainCut() {
		t.Fatal("two pieces as recorded are one stretch")
	}
}

func TestWithTheMagnetARemovedPieceClosesUpWithoutItAPauseStays(t *testing.T) {
	c := cutter()
	c.Split(70)
	c.Split(80)
	if !c.Remove(1, true) || c.Length() != 20 {
		t.Fatal(c.Length())
	}
	e := mustEdit(t, c)
	if len(e.Parts) != 2 || e.Parts[0].End != 70 || e.Parts[1].Start != 80 || e.IsPlainCut() {
		t.Fatal(e.Parts)
	}
	if !c.Remove(1, false) || !c.HasGaps() || c.Length() != 30 {
		t.Fatal("pause")
	}
	if blanks := mustEdit(t, c).Parts; !blanks[1].Blank || blanks[0].Blank || blanks[2].Blank {
		t.Fatal(blanks)
	}
	if c.CloseGaps() != 1 || c.HasGaps() || c.Length() != 20 {
		t.Fatal("close gaps")
	}
	if !c.Restore(1) || c.Length() != 30 {
		t.Fatal("restore")
	}
}

func TestTheLastPieceThatPlaysCantBeRemoved(t *testing.T) {
	c := cutter()
	if c.Remove(0, true) {
		t.Fatal("removed the only piece")
	}
	c.Split(75)
	if !c.Remove(0, true) || c.Remove(1, true) || c.Remove(1, false) || len(mustEdit(t, c).Parts) != 1 {
		t.Fatal("last piece")
	}
}

func TestVolumeAndSpeedBelongToAPiece(t *testing.T) {
	c := cutter()
	c.Split(70)
	c.SetSpeed(1, 2)
	c.SetVolume(0, 0.5)
	if c.Length() != 20 {
		t.Fatal(c.Length())
	}
	c.SetSpeed(1, 99)
	c.SetVolume(0, -1)
	if c.Pieces()[1].Speed != MaxClipSpeed || c.Pieces()[0].Volume != 0 {
		t.Fatal(c.Pieces())
	}
	c.SetVolume(0, 9)
	e := mustEdit(t, c)
	if e.Parts[0].Volume != MaxClipVolume || e.Parts[1].Speed != MaxClipSpeed || !near(e.Length(), 10+20/MaxClipSpeed) {
		t.Fatal(e.Parts)
	}
}

func TestMovingTheEndsKeepsTheCutsInside(t *testing.T) {
	c := cutter()
	c.Split(70)
	c.Split(80)
	c.SetVolume(2, 2)
	c.MoveStart(72)
	if !slices.Equal(spans(c), []se{{72, 80}, {80, 90}}) {
		t.Fatal(spans(c))
	}
	c.MoveEnd(100)
	if p := c.Pieces()[1]; p.Start != 80 || p.End != 100 || p.Volume != 2 {
		t.Fatal(p)
	}
	c.MoveEnd(80.02)
	if !slices.Equal(spans(c), []se{{72, 80.02}}) {
		t.Fatal(spans(c))
	}
	m := cutter()
	m.Split(70)
	m.MoveStart(300)
	if m.Start() != 300 || m.End() != 312.4 || len(m.Pieces()) != 2 || !near(m.Pieces()[0].End, 310) {
		t.Fatal(spans(m))
	}
	m.MoveEnd(3)
	if m.Start() != 0 || m.End() != 3 {
		t.Fatal(spans(m))
	}
}

func TestMovingTheClipAndQuickLengthsStayInTheVideo(t *testing.T) {
	c := cutter()
	c.Split(70)
	c.Move(300)
	if !near(c.Start(), 312.4-30) || c.End() != 312.4 || !near(c.Pieces()[0].End, c.Start()+10) {
		t.Fatal(spans(c))
	}
	c.Move(-4)
	if c.Start() != 0 || !near(c.End(), 30) {
		t.Fatal(spans(c))
	}
	c.SetLength(60)
	if c.Start() != 0 || c.End() != 60 {
		t.Fatal(spans(c))
	}
	c.Move(290)
	c.SetLength(60)
	if !near(c.Start(), 312.4-60) || !near(c.End(), 312.4) {
		t.Fatal(spans(c))
	}
}

func TestSilencesAreTakenOutKeepingALittleNextToSound(t *testing.T) {
	c := cutter()
	if n := c.RemoveSilences([]ClipRange{{55, 62}, {70, 73}, {80, 80.3}, {88, 95}}); n != 3 {
		t.Fatal(n)
	}
	e := mustEdit(t, c)
	p := SilencePadding
	if len(e.Parts) != 2 || !near(e.Parts[0].Start, 62-p) || !near(e.Parts[0].End, 70+p) || !near(e.Parts[1].Start, 73-p) || !near(e.Parts[1].End, 88+p) {
		t.Fatal(e.Parts)
	}
	if !near(c.Length(), 30-(2-p)-(3-2*p)-(2-p)) {
		t.Fatal(c.Length())
	}
	if !c.Undo() || c.Length() != 30 || !c.Redo() || !near(c.Length(), e.Length()) {
		t.Fatal("undo / redo")
	}
	q := cutter()
	if q.RemoveSilences([]ClipRange{{0, 312.4}}) != 0 || q.Length() != 30 {
		t.Fatal("all silent")
	}
}

func TestEveryChangeCanBeUndoneAndRedone(t *testing.T) {
	c := cutter()
	changes := 0
	c.Changed = func() { changes++ }
	c.Split(70)
	c.Remove(0, true)
	c.SetFadeIn(1)
	c.SetFadeOut(99)
	if c.FadeOut() != MaxClipFade || changes != 4 {
		t.Fatal(c.FadeOut(), changes)
	}
	if !c.Undo() || c.FadeOut() != 0 || !c.Undo() || !c.Undo() || c.Length() != 30 || !c.Undo() || len(c.Pieces()) != 1 || c.Undo() {
		t.Fatal("undo chain")
	}
	if !c.Redo() || len(c.Pieces()) != 2 {
		t.Fatal("redo")
	}
	c.SetVolume(1, 0.9)
	c.SetVolume(1, 0.8)
	c.SetVolume(1, 0.7)
	if !c.Undo() || c.Pieces()[1].Volume != 1 {
		t.Fatal("a slider drag is one step")
	}
	c.SetVolume(0, 2)
	if c.CanRedo() {
		t.Fatal("redo kept after a new change")
	}
	changes = 0
	c.MoveStart(c.Start())
	c.SetVolume(0, 2)
	if changes != 0 {
		t.Fatal("no-op changes told")
	}
}

func TestMuteGapsAndEdges(t *testing.T) {
	c := cutter()
	c.Split(70)
	c.SetMuted(1, true)
	e := mustEdit(t, c)
	if !e.Parts[1].Muted || e.Parts[1].PlayVolume() != 0 || e.Parts[1].Volume != 1 || e.IsPlainCut() || c.Length() != 30 {
		t.Fatal(e.Parts)
	}
	c.SetMuted(1, false)
	if !mustEdit(t, c).IsPlainCut() {
		t.Fatal("unmuted")
	}

	g := cutter()
	g.Split(70)
	g.Split(80)
	g.Remove(0, false)
	g.Remove(1, false)
	if !g.CloseGap(1) || g.CloseGap(2) || g.Length() != 20 {
		t.Fatal("close gap")
	}
	if st := g.Pieces(); st[0].State != PieceBlank || st[1].State != PieceRemoved || st[2].State != PieceKept {
		t.Fatal(st)
	}
}

func TestDraggingAnEdgeOutTakesBackWhatWasCut(t *testing.T) {
	c := cutter()
	c.Split(70)
	c.Split(80)
	c.Remove(1, true)
	c.TrimEdge(0, true, 74)
	if !slices.Equal(spans(c), []se{{60, 74}, {74, 80}, {80, 90}}) || c.Pieces()[1].IsKept() || c.Length() != 24 {
		t.Fatal(spans(c))
	}
	c.TrimEdge(0, true, 80)
	if !slices.Equal(spans(c), []se{{60, 80}, {80, 90}}) || c.Length() != 30 {
		t.Fatal(spans(c))
	}
	if !c.Undo() || c.Length() != 20 {
		t.Fatal("one drag one step", c.Length())
	}
	c.TrimEdge(2, false, 76)
	if !slices.Equal(spans(c), []se{{60, 70}, {70, 76}, {76, 90}}) || c.Pieces()[1].IsKept() || !c.Pieces()[2].IsKept() {
		t.Fatal(spans(c))
	}
}

func TestDraggingAnEdgeInCutsThatMuchOut(t *testing.T) {
	c := cutter()
	c.Split(70)
	c.TrimEdge(0, true, 65)
	if !slices.Equal(spans(c), []se{{60, 65}, {65, 70}, {70, 90}}) || c.Pieces()[1].IsKept() || c.Length() != 25 {
		t.Fatal(spans(c))
	}
	o := cutter()
	o.Split(70)
	o.TrimEdge(1, false, 75)
	if !slices.Equal(spans(o), []se{{60, 70}, {70, 75}, {75, 90}}) || o.Pieces()[1].IsKept() {
		t.Fatal(spans(o))
	}
	o.TrimEdge(2, false, 95)
	if p := o.Pieces(); !near(p[len(p)-1].Start, 90-MinClipLength) {
		t.Fatal(spans(o))
	}
	r := cutter()
	r.Split(70)
	if r.TrimEdge(1, true, 100) != 100 || r.TrimEdge(0, false, 50) != 50 || r.TrimEdge(1, true, 999) != 312.4 {
		t.Fatal(spans(r))
	}
}

func TestMusicUnderTheClip(t *testing.T) {
	c := cutter()
	c.SetMusic(&ClipMusic{Path: "song.mp3", Length: 200, Volume: DefaultMusicVolume, Fade: true})
	c.MoveMusic(5)
	c.MoveMusic(8)
	c.SetMusicVolume(0.3)
	c.SetMusicVolume(0.25)
	c.MoveMusic(999)
	if !near(c.Music().Offset, 30-MinClipLength) {
		t.Fatal(c.Music())
	}
	if e := mustEdit(t, c); e.Music.Path != "song.mp3" || e.Music.Volume != 0.25 || e.IsPlainCut() {
		t.Fatal(e.Music)
	}
	if !c.Undo() || c.Music().Offset != 8 || !c.Undo() || c.Music().Volume != DefaultMusicVolume || !c.Undo() || c.Music().Offset != 0 || !c.Undo() || c.Music() != nil {
		t.Fatal("music undo")
	}

	m := &ClipMusic{Path: "song.mp3", Offset: 4, From: 10, Length: 20, Volume: DefaultMusicVolume}
	if m.PlayLength(30) != 10 || m.PlayLength(10) != 6 {
		t.Fatal("play length")
	}
	e := ClipEditOf(ClipRange{0, 30})
	e.Music = m
	if l := e.From(7).Music; l.Offset != 0 || l.From != 13 {
		t.Fatal(l)
	}
	if s := e.From(1).Music; s.Offset != 3 || s.From != 10 {
		t.Fatal(s)
	}
	e.Music = &ClipMusic{Path: "song.mp3", Offset: 40, Length: 20, Volume: 1}
	if cl, _ := e.Clamped(100); cl.Music != nil {
		t.Fatal("music past the end")
	}
	e.Music = &ClipMusic{Path: "song.mp3", Offset: -3, Volume: 9, Length: 20}
	if cl, _ := e.Clamped(100); cl.Music.Offset != 0 || cl.Music.Volume != 2 {
		t.Fatal(cl.Music)
	}
}

func TestSeveralChangesInABatchAreOneUndoStep(t *testing.T) {
	c := cutter()
	c.Split(70)
	c.Split(80)
	told := 0
	c.Changed = func() { told++ }
	c.Batch(func() {
		c.Remove(0, false)
		c.Remove(2, false)
		c.SetVolume(1, 1.5)
	}, "")
	if p := c.Pieces(); told != 1 || p[0].State != PieceBlank || p[1].State != PieceKept || p[2].State != PieceBlank {
		t.Fatal(told, p)
	}
	if !c.Undo() {
		t.Fatal("undo")
	}
	for _, p := range c.Pieces() {
		if !p.IsKept() || p.Volume != 1 {
			t.Fatal(p)
		}
	}
	steps := 0
	for c.Undo() {
		steps++
	}
	c.Batch(func() { c.SetVolume(0, 1) }, "")
	if c.CanUndo() || steps != 2 {
		t.Fatal(steps)
	}
}

func edit() *ClipEdit {
	return &ClipEdit{Parts: []ClipPart{{Start: 10, End: 20, Volume: 1, Speed: 1}, {Start: 20, End: 24, Blank: true, Volume: 1, Speed: 1}, {Start: 30, End: 40, Volume: 1, Speed: 2}}, FadeIn: 1, FadeOut: 2}
}

func TestAClipPlaysItsPartsPausesAndSpeeds(t *testing.T) {
	e := edit()
	if e.Length() != 19 || e.Start() != 10 || e.End() != 40 || e.IsPlainCut() {
		t.Fatal(e.Length())
	}
	for in, want := range map[float64]float64{5: 15, 12: 22, 16: 34, 99: 40} {
		if got := e.SourceTimeAt(in); got != want {
			t.Errorf("SourceTimeAt(%v) = %v", in, got)
		}
	}
	for in, want := range map[float64]float64{15: 5, 34: 16, 27: 14} {
		if got := e.ClipTimeAt(in); got != want {
			t.Errorf("ClipTimeAt(%v) = %v", in, got)
		}
	}
	rest := e.From(16)
	if len(rest.Parts) != 1 || rest.Parts[0].Start != 34 || rest.Length() != 3 || rest.FadeIn != 0 || rest.FadeOut != 2 {
		t.Fatal(rest)
	}
	if edit().From(0.5).FadeIn != 0.5 || edit().From(0).Length() != e.Length() {
		t.Fatal("from")
	}
}

func TestClampingKeepsPartsInTheVideo(t *testing.T) {
	e, err := (&ClipEdit{Parts: []ClipPart{
		{Start: 0, End: 2, Blank: true, Volume: 1, Speed: 1},
		{Start: 5, End: 8, Speed: 99, Volume: math.NaN()},
		{Start: 7, End: 12, Volume: 1, Speed: 1},
		{Start: 12, End: 12.05, Volume: 1, Speed: 1},
		{Start: 290, End: 400, Volume: 1, Speed: 1},
		{Start: 400, End: 410, Blank: true, Volume: 1, Speed: 1},
	}, FadeIn: 99, FadeOut: -3}).Clamped(300)
	if err != nil || len(e.Parts) != 3 || e.Parts[1].Start != 8 || e.Parts[2].End != 300 || e.Parts[0].Speed != MaxClipSpeed || e.Parts[0].Volume != 1 {
		t.Fatal(e, err)
	}
	if e.FadeIn != min(MaxClipFade, e.Length()/2) || e.FadeOut != 0 {
		t.Fatal(e.FadeIn, e.FadeOut)
	}
	s := ClipEditOf(ClipRange{0, 4})
	s.FadeIn = 5
	if c, _ := s.Clamped(10); c.FadeIn != 2 {
		t.Fatal(c.FadeIn)
	}
	if _, err := ClipEditOf(ClipRange{400, 410}).Clamped(300); err == nil {
		t.Fatal("nothing left is an error")
	}
}

func TestAClipSurvivesSavingWithItsConversion(t *testing.T) {
	conv := ClipConversionOf(FindBuiltIn("tool-clip"), "mp4", edit())
	if conv.IsTool() || conv.Media.Clip == nil || conv.TargetFormat != "mp4" {
		t.Fatal(conv)
	}
	b, _ := json.Marshal([]*Preset{conv})
	var read []*Preset
	if err := json.Unmarshal(b, &read); err != nil {
		t.Fatal(err)
	}
	if got := read[0].Media.Clip; got.Parts[2].Speed != 2 || !got.Parts[1].Blank || got.FadeIn != 1 || got.FadeOut != 2 || got.Parts[0].Volume != 1 {
		t.Fatal(got)
	}
	cp := conv.Clone()
	cp.Media.Clip.Parts[0].Start = 0
	if conv.Media.Clip.Parts[0].Start != 10 {
		t.Fatal("a copy shares its clip")
	}
}

func TestClipRangeAndTimes(t *testing.T) {
	if r := ClampClipRange(50, 10, 30); r != (ClipRange{10, 30}) {
		t.Fatal(r)
	}
	if r := ClampClipRange(29.98, 29.99, 30); !near(r.Start, 29.9) || r.End != 30 {
		t.Fatal(r)
	}
	if r := ClampClipRange(math.NaN(), math.Inf(1), 0); r.Start != 0 || !near(r.End, MinClipLength) {
		t.Fatal(r)
	}
	for s, want := range map[float64]string{7.5: "0:07.5", 245: "4:05.0", 3725.3: "1:02:05.3", 59.96: "1:00.0", -1: "0:00.0"} {
		if got := ClipDisplay(s); got != want {
			t.Errorf("%v → %s", s, got)
		}
	}
	if ClipFfmpeg(65.3) != "65.300" || ClipFfmpeg(math.NaN()) != "0.000" {
		t.Fatal("ffmpeg time")
	}
}
