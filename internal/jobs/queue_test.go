package jobs

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/platform"
	"github.com/dseif0x/filee-go/internal/presets"
)

func TestConvertsEveryFile(t *testing.T) {
	e := newEnv(t)
	a, b := e.file("a.jpg", "x"), e.file("b.jpg", "x")
	q := e.queue(Options{}, newFake("magick", convert.E("jpg", "png")))
	j := run(q, []string{a, b}, target("png"))
	if j.State() != JobCompleted {
		t.Fatalf("state %s", j.State())
	}
	want := []string{e.path("a.png"), e.path("b.png")}
	if got := j.Outputs(); !slices.Equal(got, want) {
		t.Fatalf("outputs %v", got)
	}
	if s := j.Snapshot(); s.Progress != 1 || s.Count(FileDone) != 2 {
		t.Fatalf("snapshot %+v", s)
	}
}

func TestMissingFontsOfEveryStepAreCombined(t *testing.T) {
	e := newEnv(t)
	doc := e.file("a.docx", "x")
	first := newFake("w", convert.E("docx", "hwpx"))
	first.fonts = []string{"Batang", "Gulim"}
	second := newFake("r", convert.E("hwpx", "pdf"))
	second.fonts = []string{"gulim", "Dotum"}
	j := run(e.queue(Options{}, first, second), []string{doc}, target("pdf"))
	f := j.Files()[0]
	if f.State != FileDone || !slices.Equal(f.MissingFonts, []string{"Batang", "Gulim", "Dotum"}) {
		t.Fatalf("%+v", f)
	}
}

func TestEnqueueRunsInTheBackgroundAndFinishesOnce(t *testing.T) {
	e := newEnv(t)
	a := e.file("a.jpg", "x")
	release := make(chan struct{})
	slow := newFake("magick", convert.E("jpg", "png"))
	slow.before = func(*convert.Step) { <-release }
	q := e.queue(Options{}, slow)
	finished := make(chan *Job, 2)
	q.Subscribe(func(ev Event) {
		if ev.Finished {
			finished <- ev.Job
		}
	})
	j := q.Enqueue([]string{a}, target("png"), "PNG", nil)
	if !q.Busy() {
		t.Fatal("not busy after enqueueing")
	}
	close(release)
	select {
	case got := <-finished:
		if got != j || got.State() != JobCompleted {
			t.Fatalf("finished %v %s", got == j, got.State())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("never finished")
	}
	for i := 0; q.Busy() && i < 1000; i++ {
		time.Sleep(time.Millisecond)
	}
	if q.Busy() {
		t.Fatal("still busy")
	}
	select {
	case <-finished:
		t.Fatal("finished twice")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestAJobCancelledBeforeItRunsIsCancelled(t *testing.T) {
	e := newEnv(t)
	release := make(chan struct{})
	slow := newFake("magick", convert.E("jpg", "png"))
	slow.before = func(*convert.Step) { <-release }
	q := e.queue(Options{}, slow)
	first := q.Enqueue([]string{e.file("a.jpg", "x")}, target("png"), "PNG", nil)
	second := q.Enqueue([]string{e.file("b.jpg", "x")}, target("png"), "PNG", nil)
	second.Cancel()
	done := make(chan struct{})
	q.Subscribe(func(ev Event) {
		if ev.Finished && ev.Job == second {
			close(done)
		}
	})
	close(release)
	<-done
	if first.State() != JobCompleted || second.State() != JobCancelled || fileExists(e.path("b.png")) {
		t.Fatalf("%s %s", first.State(), second.State())
	}
}

func TestTextThatNeedsShapingAvoidsAnUnshapedEngineOrIsRefused(t *testing.T) {
	e := newEnv(t)
	writer := newFake("hwpx-writer", convert.E("docx", "hwpx"))
	rhwp := &shaper{fake: newFake("rhwp", convert.Edge{From: "hwpx", To: "pdf", Cost: 10, Unshaped: true}),
		script: func(p string) string {
			if strings.HasPrefix(filepath.Base(p), "arabic") {
				return "Arabic"
			}
			return ""
		}}
	office := newFake("libreoffice", convert.EC("docx", "pdf", 25))
	q := e.queue(Options{}, writer, rhwp, office)
	j := run(q, []string{e.file("arabic.docx", "x"), e.file("english.docx", "x")}, target("pdf"))
	if j.State() != JobCompleted || len(rhwp.Steps()) != 1 || len(office.Steps()) != 1 {
		t.Fatalf("%s rhwp %d office %d", j.State(), len(rhwp.Steps()), len(office.Steps()))
	}

	office.off.Store(true)
	q2 := e.queue(Options{}, writer, rhwp, office)
	j = run(q2, []string{e.file("arabic-2.docx", "x")}, target("pdf"))
	f := j.Files()[0]
	if f.State != FileFailed || f.ErrorKey != "error.needs_shaping" || f.ErrorDetail != "Arabic" || fileExists(e.path("arabic-2.pdf")) {
		t.Fatalf("%+v", f)
	}
}

func TestTextTheEngineCannotMeasureTakesAnotherRouteButIsNeverRefused(t *testing.T) {
	e := newEnv(t)
	writer := newFake("hwpx-writer", convert.E("docx", "hwpx"))
	rhwp := &shaper{fake: newFake("rhwp", convert.Edge{From: "hwpx", To: "pdf", Cost: 10, Unshaped: true}),
		widths: func(string) string { return "Cyrillic" }}
	office := newFake("libreoffice", convert.EC("docx", "pdf", 25))
	j := run(e.queue(Options{}, writer, rhwp, office), []string{e.file("ru.docx", "x")}, target("pdf"))
	if j.State() != JobCompleted || len(office.Steps()) != 1 || len(rhwp.Steps()) != 0 {
		t.Fatal("did not go around rhwp")
	}
	office.off.Store(true)
	j = run(e.queue(Options{}, writer, rhwp, office), []string{e.file("ru2.docx", "x")}, target("pdf"))
	if j.State() != JobCompleted || len(rhwp.Steps()) != 1 {
		t.Fatalf("refused: %+v", j.Files()[0])
	}
}

func TestRotateNeedsAnEngineThatTurnsAndTurnsOnce(t *testing.T) {
	e := newEnv(t)
	p := target("jpg")
	p.Image.Rotation = presets.Right90
	// Without a turning engine the file is refused rather than written unturned.
	plain := newFake("plain", convert.E("pdf", "jpg"))
	j := run(e.queue(Options{}, plain), []string{e.file("a.pdf", "x")}, p)
	if f := j.Files()[0]; f.ErrorKey != "error.cannot_turn" {
		t.Fatalf("%+v", f)
	}
	// EPS → PNG → JPG with a turner on both steps turns only on the first.
	t1 := turner{newFake("ghost", convert.E("eps", "png"))}
	t2 := turner{newFake("magick", convert.E("png", "jpg"))}
	j = run(e.queue(Options{}, t1, t2), []string{e.file("b.eps", "x")}, p)
	if j.State() != JobCompleted {
		t.Fatalf("%+v", j.Files()[0])
	}
	if r := t1.Steps()[0].preset.Image.Rotation; r != presets.Right90 {
		t.Fatalf("first step %s", r)
	}
	if r := t2.Steps()[0].preset.Image.Rotation; r != presets.RotateNone {
		t.Fatalf("second step %s", r)
	}
	// A step before the turning one gets the preset without rotation.
	before := newFake("before", convert.E("cdr", "png"))
	turn := turner{newFake("turn", convert.E("png", "webp"))}
	run(e.queue(Options{}, before, turn), []string{e.file("c.cdr", "x")}, func() *presets.Preset {
		x := p.Clone()
		x.TargetFormat = "webp"
		return x
	}())
	if before.Steps()[0].preset.Image.Rotation != presets.RotateNone || turn.Steps()[0].preset.Image.Rotation != presets.Right90 {
		t.Fatal("rotation on the wrong step")
	}
}

func TestSpeedAndSizeLimitApplyOnlyToTheLastStep(t *testing.T) {
	e := newEnv(t)
	a := newFake("a", convert.E("webp", "png"))
	b := newFake("b", convert.E("png", "gif"))
	p := target("gif")
	p.Image.AnimationSpeed = 2
	p.Image.MaxSizeMb = 5
	run(e.queue(Options{}, a, b), []string{e.file("x.webp", "x")}, p)
	first, last := a.Steps()[0].preset.Image, b.Steps()[0].preset.Image
	if first.AnimationSpeed != 1 || first.MaxSizeMb != 0 || last.AnimationSpeed != 2 || last.MaxSizeMb != 5 {
		t.Fatalf("first %v/%v last %v/%v", first.AnimationSpeed, first.MaxSizeMb, last.AnimationSpeed, last.MaxSizeMb)
	}
}

func TestAResultOverTheSizeLimitIsRefusedAndRemoved(t *testing.T) {
	e := newEnv(t)
	p := target("png")
	p.Image.MaxSizeMb = 0.000001 // 1 byte
	j := run(e.queue(Options{}, newFake("m", convert.E("jpg", "png"))), []string{e.file("a.jpg", "x")}, p)
	f := j.Files()[0]
	if f.ErrorKey != "error.over_size_limit" || !strings.Contains(f.ErrorDetail, "MB >") || fileExists(e.path("a.png")) {
		t.Fatalf("%+v", f)
	}
}

func TestKeepDatesGivesOutputsTheDatesOfTheirSource(t *testing.T) {
	e := newEnv(t)
	src := e.file("a.jpg", "x")
	old := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	_ = os.Chtimes(src, old, old)
	p := target("png")
	p.Output.KeepDates = true
	j := run(e.queue(Options{}, newFake("m", convert.E("jpg", "png"))), []string{src}, p)
	fi, err := os.Stat(j.Outputs()[0])
	if err != nil || !fi.ModTime().Equal(old) {
		t.Fatalf("%v %v", err, fi.ModTime())
	}
}

func TestEveryFrameIsSavedOnlyForVideosAndFrameTargets(t *testing.T) {
	e := newEnv(t)
	frames := newFake("ffmpeg", convert.Edge{From: "mp4", To: "png", Cost: 10, FrameSequence: true}, convert.E("mp4", "png"), convert.E("gif", "png"))
	p := target("png")
	p.Media.SaveEveryFrame = true
	run(e.queue(Options{}, frames), []string{e.file("v.mp4", "x"), e.file("g.gif", "x")}, p)
	steps := frames.Steps()
	slices.SortFunc(steps, func(a, b fakeStep) int { return strings.Compare(a.from, b.from) })
	if len(steps) != 2 || steps[0].from != "gif" || steps[0].frames || steps[1].from != "mp4" || !steps[1].frames {
		t.Fatalf("%+v", steps)
	}
}

func TestARefusedConversionSaysWhy(t *testing.T) {
	e := newEnv(t)
	f := newFake("m", convert.E("mp4", "png"))
	f.err = convert.Refuse("error.too_many_frames", "100001")
	j := run(e.queue(Options{}, f), []string{e.file("v.mp4", "x")}, target("png"))
	if r := j.Files()[0]; r.ErrorKey != "error.too_many_frames" || r.ErrorDetail != "100001" || j.State() != JobFailed {
		t.Fatalf("%+v", r)
	}
}

func TestIntermediatesStayOutOfTheOutputFolder(t *testing.T) {
	e := newEnv(t)
	a := newFake("a", convert.E("hwp", "hwpx"))
	b := newFake("b", convert.E("hwpx", "pdf"))
	j := run(e.queue(Options{}, a, b), []string{e.file("doc.hwp", "x")}, target("pdf"))
	if !slices.Equal(j.Outputs(), []string{e.path("doc.pdf")}) || fileExists(e.path("doc.hwpx")) {
		t.Fatal(j.Outputs())
	}
	// The second step read the first step's file from the work folder.
	if in := b.Steps()[0].input; !strings.HasPrefix(in, filepath.Join(e.dir, ".work")) || b.Steps()[0].original != e.path("doc.hwp") {
		t.Fatal(in)
	}
	if entries, _ := os.ReadDir(filepath.Join(e.dir, ".work")); len(entries) != 0 {
		t.Fatal("work folder left behind")
	}
}

func TestFailuresAreIsolatedPerFileAndEmptyOrUnknownFilesSayWhy(t *testing.T) {
	e := newEnv(t)
	q := e.queue(Options{}, newFake("m", convert.E("jpg", "png")))
	j := run(q, []string{e.file("good.jpg", "x"), e.file("empty.jpg", ""), e.file("what.zzz", "x"), e.file("doc.docx", "x")}, target("png"))
	fs := j.Files()
	if j.State() != JobCompletedWithErrors || fs[0].State != FileDone || fs[1].ErrorKey != "error.empty_file" ||
		fs[2].ErrorKey != "error.unsupported_source" || fs[2].ErrorDetail != ".zzz" || fs[3].ErrorKey != "error.no_route" || fs[3].ErrorDetail != "docx → png" {
		t.Fatalf("%+v", fs)
	}
}

func TestEngineErrorsAndPanicsMarkTheFileFailed(t *testing.T) {
	e := newEnv(t)
	f := newFake("m", convert.E("jpg", "png"))
	f.err = errBoom
	j := run(e.queue(Options{}, f), []string{e.file("a.jpg", "x")}, target("png"))
	if r := j.Files()[0]; r.ErrorKey != "error.conversion_failed" || r.ErrorDetail != "boom" {
		t.Fatalf("%+v", r)
	}
	p := newFake("p", convert.E("jpg", "png"))
	p.before = func(*convert.Step) { panic("kaputt") }
	j = run(e.queue(Options{}, p), []string{e.file("b.jpg", "x")}, target("png"))
	if r := j.Files()[0]; r.ErrorKey != "error.conversion_failed" || r.ErrorDetail != "kaputt" {
		t.Fatalf("%+v", r)
	}
}

func TestSameAsSourceReencodesInItsOwnFormat(t *testing.T) {
	e := newEnv(t)
	f := newFake("m", convert.E("jpg", "jpg"))
	p := target(presets.SameAsSource)
	p.Output.FileNamePattern = "{name}_small"
	j := run(e.queue(Options{}, f), []string{e.file("a.jpg", "x")}, p)
	if !slices.Equal(j.Outputs(), []string{e.path("a_small.jpg")}) {
		t.Fatal(j.Outputs())
	}
}

func TestMergeConvertsToPdfThenMergesInOrder(t *testing.T) {
	e := newEnv(t)
	m := &pdfMerger{}
	fin := &finisher{}
	q := e.queue(Options{PdfMerger: m, PdfFinisher: fin}, newFake("m", convert.E("jpg", "pdf")))
	p := target("pdf")
	p.Pdf.MergeIntoSingle = true
	p.Pdf.PageNumbers = true
	p.Output.FileNamePattern = "{name}_merged"
	j := run(q, []string{e.file("b.jpg", "x"), e.file("a.pdf", "%PDF"), e.file("c.jpg", "x")}, p)
	if j.State() != JobCompleted || !slices.Equal(j.Outputs(), []string{e.path("b_merged.pdf")}) {
		t.Fatalf("%s %v", j.State(), j.Outputs())
	}
	if got := read(t, e.path("b_merged.pdf")); got != "b.pdf|a.pdf|c.pdf" {
		t.Fatal(got)
	}
	// Finished once, so the pages are numbered through.
	if !slices.Equal(fin.finished, []string{"b_merged.pdf"}) {
		t.Fatal(fin.finished)
	}
}

func TestAnEmptyPdfFailsAsEmptyInsteadOfBreakingTheMerge(t *testing.T) {
	e := newEnv(t)
	m := &pdfMerger{}
	q := e.queue(Options{PdfMerger: m}, newFake("m", convert.E("jpg", "pdf")))
	p := target("pdf")
	p.Pdf.MergeIntoSingle = true
	j := run(q, []string{e.file("a.pdf", "%PDF"), e.file("empty.pdf", "")}, p)
	if j.State() != JobCompletedWithErrors || j.Files()[1].ErrorKey != "error.empty_file" || len(m.inputs) != 1 {
		t.Fatalf("%s %+v", j.State(), j.Files())
	}
}

func TestMergingWithARotationNeverPassesAFileThroughUnturned(t *testing.T) {
	e := newEnv(t)
	m := &pdfMerger{}
	q := e.queue(Options{PdfMerger: m}, turner{newFake("m", convert.E("jpg", "pdf"))})
	p := target("pdf")
	p.Pdf.MergeIntoSingle = true
	p.Image.Rotation = presets.Left90
	j := run(q, []string{e.file("a.jpg", "x"), e.file("b.pdf", "%PDF")}, p)
	fs := j.Files()
	if fs[0].State != FileDone || fs[1].ErrorKey != "error.cannot_turn" || len(m.inputs) != 1 {
		t.Fatalf("%+v", fs)
	}
}

func TestMultiPageTiff(t *testing.T) {
	e := newEnv(t)
	m := &tiffMerger{}
	pages := newFake("pdfium", convert.E("pdf", "tiff"))
	pages.outputsPerInput = 2
	q := e.queue(Options{TiffMerger: m}, pages)
	p := target("tiff")
	p.Image.MultiPageTiff = true
	j := run(q, []string{e.file("doc.pdf", "%PDF"), e.file("scan.tiff", "II*")}, p)
	if j.State() != JobCompleted || !slices.Equal(j.Outputs(), []string{e.path("doc.tiff")}) {
		t.Fatalf("%s %v", j.State(), j.Outputs())
	}
	if got := read(t, e.path("doc.tiff")); got != "doc_p1.tiff|doc_p2.tiff|scan.tiff" {
		t.Fatal(got)
	}
}

func TestCombinePacksEveryFileAsItIsWithShapefileCompanions(t *testing.T) {
	e := newEnv(t)
	c := &combiner{}
	q := e.queue(Options{Combiner: c})
	p := target("zip")
	p.Archive.CombineIntoOne = true
	p.Output.FileNamePattern = "{name}_files"
	shp := e.file("map.shp", "x")
	e.file("map.dbf", "x")
	e.file("map.shx", "x")
	folder := e.path("folder")
	_ = os.MkdirAll(folder, 0o755)
	j := run(q, []string{shp, e.file("empty.bin", ""), folder, e.file("what.zzz", "x")}, p)
	if j.State() != JobCompleted || !slices.Equal(j.Outputs(), []string{e.path("map_files.zip")}) {
		t.Fatalf("%s %v", j.State(), j.Outputs())
	}
	names := []string{}
	for _, i := range c.inputs {
		names = append(names, filepath.Base(i))
	}
	if !slices.Equal(names, []string{"map.shp", "map.shx", "map.dbf", "empty.bin", "folder", "what.zzz"}) {
		t.Fatal(names)
	}
	// A combiner that fails fails every file and leaves nothing.
	bad := &combiner{}
	bad.err = errBoom
	j = run(e.queue(Options{Combiner: bad}), []string{e.file("x.txt", "x"), e.file("y.txt", "x")}, p)
	if j.State() != JobFailed || fileExists(e.path("x_files.zip")) || j.Files()[1].ErrorDetail != "boom" {
		t.Fatalf("%s %+v", j.State(), j.Files())
	}
}

func TestWithoutCombineArchivesConvertFileByFile(t *testing.T) {
	e := newEnv(t)
	c := &combiner{}
	q := e.queue(Options{Combiner: c}, newFake("archive", convert.E("rar", "zip")))
	j := run(q, []string{e.file("a.rar", "x"), e.file("b.rar", "x")}, target("zip"))
	if j.State() != JobCompleted || len(j.Outputs()) != 2 || len(c.inputs) != 0 {
		t.Fatal(j.Outputs())
	}
}

func TestParallelFilesWithTheSameNameDoNotCollide(t *testing.T) {
	e := newEnv(t)
	q := e.queue(Options{}, newFake("m", convert.E("jpg", "webp"), convert.E("png", "webp")))
	j := run(q, []string{e.file("same.jpg", "x"), e.file("same.png", "x")}, target("webp"))
	out := j.Outputs()
	if len(out) != 2 || out[0] == out[1] {
		t.Fatal(out)
	}
}

// halfWriter takes a name, writes half a file and fails (or is cancelled).
type halfWriter struct {
	*fake
	writes bool
	during func()
}

func newHalf(writes bool, during func()) *halfWriter {
	h := &halfWriter{fake: newFake("half", convert.E("jpg", "png")), writes: writes, during: during}
	return h
}

func (h *halfWriter) Convert(ctx context.Context, st *convert.Step, _ convert.Progress) ([]string, error) {
	path := st.Output.Allocate(st.To, "")
	if h.writes {
		_ = os.WriteFile(path, []byte("half"), 0o644)
	}
	if h.during != nil {
		h.during()
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return nil, errBoom
}

func TestAFailedConversionLeavesNoHalfWrittenFileAndFreesItsName(t *testing.T) {
	e := newEnv(t)
	photo := e.file("photo.jpg", "x")
	j := run(e.queue(Options{}, newHalf(true, nil)), []string{photo}, target("png"))
	if j.State() != JobFailed || fileExists(e.path("photo.png")) {
		t.Fatal("half-written file left")
	}
}

func TestACancelledConversionLeavesNothingAndARetryGetsTheSameName(t *testing.T) {
	e := newEnv(t)
	photo := e.file("photo.jpg", "x")
	var running *Job
	half := newHalf(true, func() { running.Cancel() })
	good := newFake("magick", convert.E("jpg", "png"))
	good.off.Store(true)
	cat := convert.NewCatalog(half, good)
	q := NewQueue(cat, Options{WorkRoot: e.path(".work")})
	defer q.Close()
	running = NewJob([]string{photo}, target("png"), "Test", nil)
	q.Run(running)
	if running.State() != JobCancelled || fileExists(e.path("photo.png")) {
		t.Fatal(running.State())
	}
	half.off.Store(true)
	good.off.Store(false)
	cat.Refresh()
	retry := run(q, []string{photo}, target("png"))
	if !slices.Equal(retry.Outputs(), []string{e.path("photo.png")}) {
		t.Fatal(retry.Outputs())
	}
}

func TestAFailedConversionNeverDeletesAFileItWasToOverwrite(t *testing.T) {
	e := newEnv(t)
	photo := e.file("photo.jpg", "x")
	existing := e.file("photo.png", "precious")
	p := target("png")
	p.Output.Conflict = presets.ConflictOverwrite
	run(e.queue(Options{}, newHalf(false, nil)), []string{photo}, p)
	if read(t, existing) != "precious" {
		t.Fatal("the file to overwrite was deleted")
	}
}

func TestAFailedConversionNeverDeletesTheFileAnotherOneSharesTheNameOf(t *testing.T) {
	e := newEnv(t)
	// Two files that become the same name under Overwrite: one fails, the other's result stays.
	good := newFake("good", convert.E("bmp", "png"))
	done := make(chan struct{})
	half := newHalf(false, func() {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	})
	good.after = func() { close(done) }
	p := target("png")
	p.Output.Conflict = presets.ConflictOverwrite
	p.Output.FileNamePattern = "same"
	j := run(e.queue(Options{}, half, good), []string{e.file("a.jpg", "x"), e.file("b.bmp", "x")}, p)
	if !fileExists(e.path("same.png")) || j.State() != JobCompletedWithErrors {
		t.Fatalf("%s %+v", j.State(), j.Files())
	}
}

func TestAFailedConversionRemovesTheSubfolderItMadeButNotOneThatWasThere(t *testing.T) {
	e := newEnv(t)
	p := target("png")
	p.Output.Location = presets.LocationSubfolder
	run(e.queue(Options{}, newHalf(true, nil)), []string{e.file("a.jpg", "x")}, p)
	if fileExists(e.path("converted")) {
		t.Fatal("subfolder left")
	}
	_ = os.MkdirAll(e.path("converted"), 0o755)
	run(e.queue(Options{}, newHalf(true, nil)), []string{e.file("b.jpg", "x")}, p)
	if !fileExists(e.path("converted")) {
		t.Fatal("the user's folder was removed")
	}
}

func TestSubfolderOnlyForSeveralFiles(t *testing.T) {
	e := newEnv(t)
	p := target("png")
	p.Output.Location = presets.LocationSubfolder
	p.Output.SubfolderOnlyForSeveralFiles = true
	q := e.queue(Options{}, newFake("m", convert.E("jpg", "png")))
	// A single output is saved next to its source, and the subfolder goes.
	j := run(q, []string{e.file("one.jpg", "x")}, p)
	if !slices.Equal(j.Outputs(), []string{e.path("one.png")}) || fileExists(e.path("converted")) {
		t.Fatal(j.Outputs())
	}
	// Several outputs of one file stay together in the subfolder.
	pages := newFake("pages", convert.E("pdf", "png"))
	pages.outputsPerInput = 2
	j = run(e.queue(Options{}, pages), []string{e.file("doc.pdf", "x")}, p)
	if out := j.Outputs(); len(out) != 2 || filepath.Dir(out[0]) != e.path("converted") {
		t.Fatal(out)
	}
	// A Markdown file keeps its picture folder: it stays in the subfolder.
	md := newFake("pandoc", convert.E("docx", "md"))
	md.pictureFolder = true
	pm := p.Clone()
	pm.TargetFormat = "md"
	j = run(e.queue(Options{}, md), []string{e.file("text.docx", "x")}, pm)
	if out := j.Outputs(); len(out) != 1 || filepath.Dir(out[0]) != e.path("converted") {
		t.Fatal(out)
	}
	// Skip meets an existing file next to the source: the result stays in the subfolder.
	e.file("taken.png", "old")
	ps := p.Clone()
	ps.Output.Conflict = presets.ConflictSkip
	j = run(q, []string{e.file("taken.jpg", "x")}, ps)
	if out := j.Outputs(); len(out) != 1 || out[0] != filepath.Join(e.path("converted"), "taken.png") || read(t, e.path("taken.png")) != "old" {
		t.Fatal(out)
	}
	// A merged PDF is one file: next to the first source.
	m := &pdfMerger{}
	pdf := p.Clone()
	pdf.TargetFormat = "pdf"
	pdf.Pdf.MergeIntoSingle = true
	j = run(e.queue(Options{PdfMerger: m}), []string{e.file("x.pdf", "%"), e.file("y.pdf", "%")}, pdf)
	if !slices.Equal(j.Outputs(), []string{e.path("x.pdf")[:len(e.path("x.pdf"))-4] + " (2).pdf"}) {
		t.Fatal(j.Outputs())
	}
}

func TestZipResultPacksTheOutputsOfEachFile(t *testing.T) {
	e := newEnv(t)
	pages := newFake("pages", convert.E("pdf", "png"))
	pages.outputsPerInput = 2
	p := target("png")
	p.Output.ZipResult = true
	j := run(e.queue(Options{}, pages), []string{e.file("doc.pdf", "x")}, p)
	if !slices.Equal(j.Outputs(), []string{e.path("doc.zip")}) {
		t.Fatal(j.Outputs())
	}
	zr, err := zip.OpenReader(e.path("doc.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if !slices.Equal(names, []string{"doc_p1.png", "doc_p2.png"}) {
		t.Fatal(names)
	}
}

func TestOnlyIfSmallerKeepsTheOriginalWhenTheCopyIsNotSmaller(t *testing.T) {
	e := newEnv(t)
	p := target(presets.SameAsSource)
	p.Output.OnlyIfSmaller = true
	p.Output.FileNamePattern = "{name}_compressed"
	src := e.file("big.jpg", "short")
	j := run(e.queue(Options{}, newFake("m", convert.E("jpg", "jpg"))), []string{src}, p)
	f := j.Files()[0]
	if f.NoticeKey != "notice.pdf_not_smaller" || read(t, e.path("big_compressed.jpg")) != "short" {
		t.Fatalf("%+v", f)
	}
	// Smaller: the copy stays.
	src = e.file("small.jpg", strings.Repeat("x", 500))
	j = run(e.queue(Options{}, newFake("m", convert.E("jpg", "jpg"))), []string{src}, p)
	if f := j.Files()[0]; f.NoticeKey != "" || read(t, e.path("small_compressed.jpg")) == strings.Repeat("x", 500) {
		t.Fatalf("%+v", f)
	}
}

func TestReplaceOriginal(t *testing.T) {
	e := newEnv(t)
	trash := e.path("trash")
	_ = os.MkdirAll(trash, 0o755)
	var trashed []string
	moveToTrash := func(path string) *platform.TrashRefusal {
		trashed = append(trashed, path)
		if err := os.Rename(path, filepath.Join(trash, filepath.Base(path))); err != nil {
			return &platform.TrashRefusal{ReasonKey: "trash.failed", Detail: err.Error()}
		}
		return nil
	}
	p := target(presets.SameAsSource)
	p.Output.ReplaceOriginal = true
	p.Output.FileNamePattern = "{name}_small"
	src := e.file("photo.jpg", strings.Repeat("x", 100))
	j := run(e.queue(Options{MoveToTrash: moveToTrash}, newFake("m", convert.E("jpg", "jpg"))), []string{src}, p)
	f := j.Files()[0]
	if !slices.Equal(f.Outputs, []string{src}) || !f.OriginalInTrash || !fileExists(filepath.Join(trash, "photo.jpg")) ||
		!strings.Contains(read(t, src), "by m") || fileExists(e.path("photo_small.jpg")) {
		t.Fatalf("%+v", f)
	}
	// A refusing trash keeps both, with the reason.
	src2 := e.file("other.jpg", strings.Repeat("x", 100))
	refuse := func(string) *platform.TrashRefusal {
		return &platform.TrashRefusal{ReasonKey: "trash.in_use", Detail: "locked"}
	}
	j = run(e.queue(Options{MoveToTrash: refuse}, newFake("m", convert.E("jpg", "jpg"))), []string{src2}, p)
	f = j.Files()[0]
	if f.NoticeKey != "notice.original_kept" || f.NoticeReasonKey != "trash.in_use" || !slices.Equal(f.Outputs, []string{e.path("other_small.jpg")}) ||
		read(t, src2) != strings.Repeat("x", 100) {
		t.Fatalf("%+v", f)
	}
	// Without a trash the original is kept.
	src3 := e.file("third.jpg", strings.Repeat("x", 100))
	j = run(e.queue(Options{}, newFake("m", convert.E("jpg", "jpg"))), []string{src3}, p)
	if f := j.Files()[0]; f.NoticeReasonKey != "trash.no_recycle_bin" {
		t.Fatalf("%+v", f)
	}
	// Another format is saved beside the original; another kind of TXT is not replaced; .jpeg is a spelling of .jpg.
	j = run(e.queue(Options{MoveToTrash: moveToTrash}, newFake("m", convert.E("jpg", "png"))), []string{e.file("pic.jpg", "x")}, func() *presets.Preset {
		x := p.Clone()
		x.TargetFormat = "png"
		return x
	}())
	if f := j.Files()[0]; f.OriginalInTrash || !fileExists(e.path("pic.jpg")) {
		t.Fatalf("%+v", f)
	}
	if !sameKindOfFile(".jpeg", ".JPG") || sameKindOfFile(".bat", ".txt") {
		t.Fatal("spellings")
	}
}

func TestJoinedMedia(t *testing.T) {
	e := newEnv(t)
	joiner := &mediaJoiner{}
	pngs := newFake("magick", convert.E("jpg", "png"))
	q := e.queue(Options{MediaJoiner: joiner}, pngs)
	p := target("mp4")
	p.Media.JoinIntoOne = true
	p.Output.FileNamePattern = "{name}_merged"
	j := run(q, []string{e.file("b.mp4", "x"), e.file("doc.pdf", "x"), e.file("a.mov", "x")}, p)
	fs := j.Files()
	if j.State() != JobCompletedWithErrors || fs[1].ErrorKey != "error.no_route" || !slices.Equal(j.Outputs(), []string{e.path("b_merged.mp4")}) {
		t.Fatalf("%s %+v", j.State(), fs)
	}
	if got := read(t, e.path("b_merged.mp4")); got != "b.mp4|a.mov" {
		t.Fatal(got)
	}
	// A slideshow makes PNGs first and takes a PNG as it is; it isn't done while the video is made.
	slides := target("mp4")
	slides.Media.Slideshow = true
	var during []FileResult
	joiner2 := &mediaJoiner{}
	var j2 *Job
	joiner2.joining = func() { during = j2.Files() }
	q2 := e.queue(Options{MediaJoiner: joiner2}, pngs)
	j2 = NewJob([]string{e.file("p1.jpg", "x"), e.file("p2.png", "x")}, slides, "Test", nil)
	q2.Run(j2)
	if j2.State() != JobCompleted || len(joiner2.inputs) != 2 || filepath.Base(joiner2.inputs[1]) != "p2.png" || filepath.Base(joiner2.inputs[0]) != "p1.png" {
		t.Fatalf("%s %v", j2.State(), joiner2.inputs)
	}
	for _, f := range during {
		if f.State != FileRunning || f.Progress >= 1 {
			t.Fatalf("done while joining: %+v", f)
		}
	}
	// Too many pictures fail before any is converted.
	var many []string
	for i := 0; i <= presets.MaxSlides; i++ {
		many = append(many, e.file(filepath.Join("many", "p"+strconv.Itoa(i)+".png"), "x"))
	}
	j = run(q2, many, slides)
	if f := j.Files()[0]; f.ErrorKey != "error.too_many_pictures" || len(joiner2.inputs) != 2 {
		t.Fatalf("%+v", f)
	}
	// A failed join fails every file, frees its name and leaves no output.
	bad := &mediaJoiner{}
	bad.err = errBoom
	sub := p.Clone()
	sub.Output.Location = presets.LocationSubfolder
	j = run(e.queue(Options{MediaJoiner: bad}), []string{e.file("x.mp4", "x"), e.file("y.mp4", "x")}, sub)
	if j.State() != JobFailed || fileExists(e.path("converted")) {
		t.Fatalf("%s", j.State())
	}
}

func TestMergedImages(t *testing.T) {
	e := newEnv(t)
	joiner := &imageJoiner{fake: newFake("magick", convert.E("icns", "png")), reads: []string{"png", "jpg"}, writes: []string{"png", "jpg"}}
	q := e.queue(Options{}, joiner)
	p := target(presets.SameAsSource)
	p.Image.JoinIntoOne = true
	p.Output.FileNamePattern = "{name}_merged"
	j := run(q, []string{e.file("b.jpg", "x"), e.file("icon.icns", "x"), e.file("doc.pdf", "x"), e.file("a.png", "x")}, p)
	fs := j.Files()
	if fs[2].ErrorKey != "error.images_only" || !slices.Equal(j.Outputs(), []string{e.path("b_merged.jpg")}) {
		t.Fatalf("%+v", fs)
	}
	if !slices.Equal(joiner.formats, []string{"jpg", "png", "png"}) {
		t.Fatal(joiner.formats)
	}
	// A target the joiner can't write fails every picture.
	p2 := p.Clone()
	p2.TargetFormat = "webp"
	j = run(q, []string{e.file("c.png", "x")}, p2)
	if f := j.Files()[0]; f.ErrorKey != "error.no_route" {
		t.Fatalf("%+v", f)
	}
	// Refused as too large: says why and leaves no file.
	big := &imageJoiner{fake: newFake("magick"), reads: []string{"png"}, writes: []string{"png"}, tooBig: true}
	j = run(e.queue(Options{}, big), []string{e.file("d.png", "x"), e.file("e.png", "x")}, p)
	if f := j.Files()[0]; f.ErrorKey != "error.merged_image_too_large" || fileExists(e.path("d_merged.png")) {
		t.Fatalf("%+v", f)
	}
}

func TestPdfFinishingAndAPasswordThatIsGone(t *testing.T) {
	e := newEnv(t)
	fin := &finisher{}
	p := target("pdf")
	p.Pdf.Watermark = "DRAFT"
	q := e.queue(Options{PdfFinisher: fin}, newFake("a", convert.E("docx", "pdf")), newFake("b", convert.E("jpg", "pdf")))
	run(q, []string{e.file("a.docx", "x"), e.file("b.jpg", "x")}, p)
	slices.Sort(fin.finished)
	if !slices.Equal(fin.finished, []string{"a.pdf", "b.pdf"}) {
		t.Fatal(fin.finished)
	}
	gone := p.Clone()
	gone.Pdf.PasswordLeftOut = true
	j := run(q, []string{e.file("c.docx", "x")}, gone)
	if f := j.Files()[0]; f.ErrorKey != "error.pdf_password_gone" || fileExists(e.path("c.pdf")) {
		t.Fatalf("%+v", f)
	}
}

func TestOcrRunsBeforeTheDocumentWriter(t *testing.T) {
	e := newEnv(t)
	tess := newFake("tesseract", convert.Edge{From: "png", To: "pdf", Cost: 10, Ocr: true}, convert.Edge{From: "pdf", To: "pdf", Cost: 10, Ocr: true})
	text := newFake("pdf-text", convert.E("pdf", "docx"))
	plain := newFake("magick", convert.E("png", "pdf"))
	p := target("docx")
	p.Pdf.Ocr = true
	j := run(e.queue(Options{}, tess, text, plain), []string{e.file("scan.png", "x")}, p)
	if j.State() != JobCompleted || len(tess.Steps()) != 1 || len(plain.Steps()) != 0 || len(text.Steps()) != 1 {
		t.Fatalf("%s tess %d plain %d", j.State(), len(tess.Steps()), len(plain.Steps()))
	}
}

// fetcher saves "web:" sources by writing them into a folder.
type fetcher struct {
	dir      string
	released int
	mu       sync.Mutex
}

func (f *fetcher) Fetches(s string) bool { return strings.HasPrefix(s, "web:") }
func (f *fetcher) Fetch(_ context.Context, s string, progress func(TransferProgress)) (string, error) {
	name := strings.TrimPrefix(s, "web:")
	if name == "page.jpg" {
		return "", convert.Refuse("error.web_page", "")
	}
	progress(TransferProgress{Bytes: 5, Total: 10})
	p := filepath.Join(f.dir, name)
	return p, os.WriteFile(p, []byte("downloaded"), 0o644)
}
func (f *fetcher) Release(*Job) {
	f.mu.Lock()
	f.released++
	f.mu.Unlock()
}

func TestFetchedSourcesAreSavedFirstAndFailuresSayWhy(t *testing.T) {
	e := newEnv(t)
	fe := &fetcher{dir: e.dir}
	q := e.queue(Options{}, newFake("m", convert.E("jpg", "png")))
	j := NewJob([]string{"web:a.jpg", "web:page.jpg", e.file("local.jpg", "x")}, target("png"), "Test", fe)
	q.Run(j)
	fs := j.Files()
	if fs[0].SourcePath != e.path("a.jpg") || !fs[0].Saved || fs[0].State != FileDone || fs[1].ErrorKey != "error.web_page" ||
		fs[2].State != FileDone || fe.released != 1 || j.State() != JobCompletedWithErrors {
		t.Fatalf("%s %+v", j.State(), fs)
	}
	if j.Transfer() != nil {
		t.Fatal("transfer after the job")
	}
}

func TestJobTransferSumsTheDownloads(t *testing.T) {
	j := NewJob([]string{"web:a", "web:b"}, target("png"), "T", &fetcher{})
	j.files[0].State, j.files[0].Transfer = FileRunning, &TransferProgress{Bytes: 3, Total: 10}
	j.files[1].State, j.files[1].Transfer = FileRunning, &TransferProgress{Bytes: 4, Total: 20}
	if tr := j.Transfer(); tr == nil || tr.Bytes != 7 || tr.Total != 30 || tr.Count != 2 {
		t.Fatalf("%+v", tr)
	}
	j.files[1].Transfer.Total = 0
	if tr := j.Transfer(); tr.Total != -1 {
		t.Fatalf("%+v", tr)
	}
}

func TestStaleWorkFoldersAreDeletedAndRunningOnesKept(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "0123456789")
	fresh := filepath.Join(root, "abcdef0123")
	other := filepath.Join(root, "keep-me")
	for _, d := range []string{old, fresh, other} {
		_ = os.MkdirAll(filepath.Join(d, "sub"), 0o755)
		_ = os.WriteFile(filepath.Join(d, "sub", "f"), []byte("x"), 0o644)
	}
	past := time.Now().Add(-48 * time.Hour)
	for _, p := range []string{old, filepath.Join(old, "sub"), filepath.Join(old, "sub", "f"), other} {
		_ = os.Chtimes(p, past, past)
	}
	// A running job writes deeper down: its folder's own time is old, the file new.
	_ = os.Chtimes(fresh, past, past)
	DeleteStaleWorkFolders(root, 24*time.Hour)
	if fileExists(old) || !fileExists(fresh) || !fileExists(other) {
		t.Fatal(fileExists(old), fileExists(fresh), fileExists(other))
	}
}

func TestStaleSweepNeverFollowsALink(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	victim := filepath.Join(target, "precious")
	_ = os.WriteFile(victim, []byte("x"), 0o644)
	past := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(victim, past, past)
	if err := os.Symlink(target, filepath.Join(root, "0123456789")); err != nil {
		t.Skip("no symlinks here")
	}
	DeleteStaleWorkFolders(root, time.Hour)
	if !fileExists(victim) {
		t.Fatal("followed a link")
	}
}

func TestEventsCarryProgressThatOnlyMovesForward(t *testing.T) {
	e := newEnv(t)
	q := e.queue(Options{}, newFake("a", convert.E("hwp", "hwpx")), newFake("b", convert.E("hwpx", "pdf")))
	var mu sync.Mutex
	var seen []float64
	q.Subscribe(func(ev Event) {
		mu.Lock()
		seen = append(seen, ev.Job.Progress())
		mu.Unlock()
	})
	run(q, []string{e.file("a.hwp", "x")}, target("pdf"))
	for i := 1; i < len(seen); i++ {
		if seen[i] < seen[i-1] {
			t.Fatal(seen)
		}
	}
	if seen[len(seen)-1] != 1 {
		t.Fatal(seen)
	}
}
