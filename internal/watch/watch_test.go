package watch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/jobs"
	"github.com/dseif0x/filee-go/internal/presets"
)

type pngFake struct{}

func (pngFake) ID() string             { return "magick" }
func (pngFake) Name() string           { return "magick" }
func (pngFake) Edges() []convert.Edge  { return []convert.Edge{convert.E("jpg", "png")} }
func (pngFake) MaxParallelism() int    { return 0 }
func (pngFake) Status() convert.Status { return convert.Available("", "") }
func (pngFake) Convert(_ context.Context, st *convert.Step, _ convert.Progress) ([]string, error) {
	p := st.Output.Allocate(st.To, "")
	return []string{p}, os.WriteFile(p, []byte("png"), 0o644)
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time    { return c.now }
func (c *clock) advance(s float64) { c.now = c.now.Add(time.Duration(s * float64(time.Second))) }
func write(t *testing.T, p, s string) string {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

type setup struct {
	w    *Watcher
	c    *clock
	jobs []*jobs.Job
}

func create(t *testing.T, rule *Rule) *setup {
	q := jobs.NewQueue(convert.NewCatalog(pngFake{}), jobs.Options{WorkRoot: t.TempDir()})
	t.Cleanup(q.Close)
	s := &setup{c: &clock{now: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)}}
	s.w = New(rule, func(_ context.Context, files []string) *jobs.Job {
		p := presets.New("", "png")
		p.Output.Location = presets.LocationCustomFolder
		p.Output.CustomFolder = rule.ResolvedOutputFolder()
		j := jobs.NewJob(files, p, "Test", nil)
		q.Run(j)
		s.jobs = append(s.jobs, j)
		return j
	}, Options{Settle: 2 * time.Second, Now: s.c.Now})
	t.Cleanup(s.w.Close)
	return s
}

func TestOutputCanShareTheWatchedFolderWithoutConvertingItsOwnResults(t *testing.T) {
	for _, originals := range []AfterConversion{Keep, MoveToOriginals} {
		for _, sep := range []string{"", string(filepath.Separator)} {
			dir := t.TempDir()
			rule := NewRule(dir, "x")
			rule.OutputFolder = dir + sep
			rule.Originals = originals
			s := create(t, rule)
			src := write(t, filepath.Join(dir, "photo.jpg"), "x")
			s.w.Notice(src)
			s.c.advance(3)
			if n := s.w.ProcessPending(); n != 1 {
				t.Fatalf("%s: %d", originals, n)
			}
			out := s.jobs[0].Outputs()
			if len(out) != 1 || out[0] != filepath.Join(dir, "photo.png") {
				t.Fatal(out)
			}
			_, err := os.Stat(src)
			if (err == nil) != (originals == Keep) {
				t.Fatalf("%s: source there %v", originals, err == nil)
			}
			s.w.Notice(out[0])
			s.c.advance(3)
			if n := s.w.ProcessPending(); n != 0 {
				t.Fatal("converted its own result")
			}
			s.w.Notice(write(t, filepath.Join(dir, "next.jpg"), "x"))
			s.c.advance(3)
			if n := s.w.ProcessPending(); n != 1 || len(s.jobs) != 2 {
				t.Fatal("new file not converted")
			}
		}
	}
}

func TestSharedFolderResultsSurviveARestartButAReplacedFileIsNew(t *testing.T) {
	dir := t.TempDir()
	rule := NewRule(dir, "x")
	rule.OutputFolder = dir
	rule.Originals = MoveToOriginals
	first := create(t, rule)
	first.w.Notice(write(t, filepath.Join(dir, "photo.jpg"), "x"))
	first.c.advance(3)
	if first.w.ProcessPending() != 1 {
		t.Fatal("first batch")
	}
	first.w.Close()

	again := create(t, rule)
	if err := again.w.Start(); err != nil {
		t.Fatal(err)
	}
	again.c.advance(3)
	if again.w.ProcessPending() != 0 || len(again.jobs) != 0 {
		t.Fatal("converted yesterday's result again")
	}
	replaced := filepath.Join(dir, "photo.png")
	write(t, replaced, "a different incoming file")
	again.w.Notice(replaced)
	again.c.advance(3)
	if again.w.ProcessPending() != 1 {
		t.Fatal("a replaced file was not new input")
	}
}

func TestAFileIsConvertedOnceItHasSettledAndAGrowingOneWaits(t *testing.T) {
	dir := t.TempDir()
	s := create(t, NewRule(dir, "x"))
	src := write(t, filepath.Join(dir, "a.jpg"), "x")
	s.w.Notice(src)
	s.c.advance(1)
	if s.w.ProcessPending() != 0 {
		t.Fatal("too early")
	}
	// It grows: the wait starts again.
	write(t, src, "xx")
	s.c.advance(1.5)
	if s.w.ProcessPending() != 0 {
		t.Fatal("converted while growing")
	}
	s.c.advance(2.5)
	if s.w.ProcessPending() != 1 {
		t.Fatal("never converted")
	}
	// The same file, unchanged, is not converted again.
	s.w.Notice(src)
	s.c.advance(3)
	if s.w.ProcessPending() != 0 {
		t.Fatal("converted twice")
	}
}

func TestIgnoredFiles(t *testing.T) {
	dir := t.TempDir()
	rule := NewRule(dir, "x")
	for _, rel := range []string{"a.tmp", "b.jpg.crdownload", "~$doc.docx", ".hidden.jpg", "thumbs.db", "c.unknownext",
		filepath.Join("converted", "d.jpg"), filepath.Join("originals", "e.jpg"), filepath.Join("sub", "f.jpg")} {
		p := write(t, filepath.Join(dir, rel), "x")
		if !ShouldIgnore(p, rule) {
			t.Error("not ignored:", rel)
		}
	}
	if ShouldIgnore(write(t, filepath.Join(dir, "g.jpg"), "x"), rule) {
		t.Error("a plain file is ignored")
	}
	rule.IncludeSubfolders = true
	if ShouldIgnore(filepath.Join(dir, "sub", "f.jpg"), rule) {
		t.Error("subfolders count with the rule")
	}
	// An output folder above the watch root does not exclude the inputs.
	up := NewRule(dir, "x")
	up.OutputFolder = filepath.Dir(dir)
	if ShouldIgnore(filepath.Join(dir, "g.jpg"), up) {
		t.Error("output above the root excluded the inputs")
	}
	rule.Include = []string{"*.heic"}
	if !ShouldIgnore(filepath.Join(dir, "g.jpg"), rule) {
		t.Error("the include patterns let it through")
	}
}

func TestMoveToOriginalsConvertsWhatIsThereAndMissingFoldersAreReported(t *testing.T) {
	dir := t.TempDir()
	rule := NewRule(dir, "x")
	rule.Originals = MoveToOriginals
	write(t, filepath.Join(dir, "a.jpg"), "x")
	write(t, filepath.Join(dir, "originals", "a.jpg"), "older")
	s := create(t, rule)
	if err := s.w.Start(); err != nil {
		t.Fatal(err)
	}
	s.c.advance(3)
	if s.w.ProcessPending() != 1 {
		t.Fatal("existing file not converted")
	}
	if _, err := os.Stat(filepath.Join(dir, "originals", "a (2).jpg")); err != nil {
		t.Fatal("not moved with a number", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "converted", "a.png")); err != nil {
		t.Fatal(err)
	}
	missing := create(t, NewRule(filepath.Join(dir, "nope"), "x"))
	if err := missing.w.Start(); err == nil || missing.w.Error() == "" {
		t.Fatal("a missing folder was watched")
	}
}

func TestTheWatcherSeesNewFilesByItself(t *testing.T) {
	dir := t.TempDir()
	rule := NewRule(dir, "x")
	rule.IncludeSubfolders = true
	done := make(chan *jobs.Job, 4)
	q := jobs.NewQueue(convert.NewCatalog(pngFake{}), jobs.Options{WorkRoot: t.TempDir()})
	defer q.Close()
	w := New(rule, func(_ context.Context, files []string) *jobs.Job {
		j := jobs.NewJob(files, rule.Apply(presets.New("", "png")), "Test", nil)
		q.Run(j)
		done <- j
		return j
	}, Options{Settle: 100 * time.Millisecond})
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	_ = os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	time.Sleep(50 * time.Millisecond) // let the new folder be watched
	write(t, filepath.Join(dir, "sub", "new.jpg"), "x")
	select {
	case j := <-done:
		if out := j.Outputs(); len(out) != 1 || out[0] != filepath.Join(dir, "converted", "sub", "new.png") {
			t.Fatal(out)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the new file was never converted")
	}
}

func TestPatterns(t *testing.T) {
	cases := []struct {
		pattern, name string
		match         bool
	}{
		{"*.heic", "IMG_0001.HEIC", true}, {"*.heic", "IMG_0001.jpg", false},
		{"scan_??.png", "scan_12.png", true}, {"scan_??.png", "scan_123.png", false},
		{`/^IMG_\d+\.jpg$/`, "img_20261011.jpg", true}, {`/^IMG_\d+\.jpg$/`, "IMG_cat.jpg", false},
		{`/^report-\d{2,4}/`, "report-2026 final.docx", true},
	}
	for _, c := range cases {
		if ValidatePattern(c.pattern) != "" || FilterOf([]string{c.pattern}).Matches(c.name) != c.match {
			t.Errorf("%s %s", c.pattern, c.name)
		}
	}
	for typed, want := range map[string]string{"heic": "*.heic", ".HEIC": "*.heic", "scan": "*scan*", "scan_*": "scan_*", `/^IMG_\d+/`: `/^IMG_\d+/`, "  ": ""} {
		if got := NormalizePattern(typed); got != want {
			t.Errorf("%q → %q", typed, got)
		}
	}
	if ValidatePattern("/IMG_(/") == "" || !FilterOf([]string{"/IMG_(/"}).Matches("anything.jpg") {
		t.Error("an invalid regular expression")
	}
}

func TestApplyKeepsSubfoldersAndTheFolderNameRules(t *testing.T) {
	dir := t.TempDir()
	rule := NewRule(dir, "x")
	rule.FileNamePattern = " {name}_w "
	rule.Renames = []presets.RenameStep{{Kind: presets.RenameUppercase}, {Kind: presets.RenameReplace}}
	p := rule.Apply(presets.New("", "png"))
	got := presets.Resolve(&p.Output, presets.Tokens{SourcePath: filepath.Join(dir, "sub", "a.jpg")}, "png", func(string) bool { return false }, "")
	if want := filepath.Join(dir, "converted", "sub", "A_W.png"); got != want {
		t.Fatalf("%s != %s", got, want)
	}
	if p.Output.ReplaceOriginal || len(p.Output.Renames) != 1 {
		t.Fatal(p.Output)
	}
}
