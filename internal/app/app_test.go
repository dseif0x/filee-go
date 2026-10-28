package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/jobs"
	"github.com/dseif0x/filee-go/internal/presets"
	"github.com/dseif0x/filee-go/internal/watch"
)

type fake struct{}

func (fake) ID() string   { return "magick" }
func (fake) Name() string { return "ImageMagick" }
func (fake) Edges() []convert.Edge {
	return []convert.Edge{convert.E("jpg", "png"), convert.E("png", "webp")}
}
func (fake) MaxParallelism() int    { return 0 }
func (fake) Status() convert.Status { return convert.Available("", "7") }
func (fake) Convert(_ context.Context, st *convert.Step, _ convert.Progress) ([]string, error) {
	p := st.Output.Allocate(st.To, "")
	return []string{p}, os.WriteFile(p, []byte(st.To), 0o644)
}

func newApp(t *testing.T) *App {
	a, err := New(Options{DataDir: t.TempDir(), Catalog: convert.NewCatalog(fake{}), WorkRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a
}

func wait(t *testing.T, ch <-chan Event, kind EventKind) Event {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case e := <-ch:
			if e.Kind == kind {
				return e
			}
		case <-timeout:
			t.Fatal("no", kind)
		}
	}
}

func TestConvertReportsEventsAndRecordsHistory(t *testing.T) {
	a := newApp(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "a.jpg")
	_ = os.WriteFile(src, []byte("x"), 0o644)
	events, stop := a.Events(100)
	defer stop()
	j, err := a.Convert([]string{src}, "PNG")
	if err != nil {
		t.Fatal(err)
	}
	e := wait(t, events, JobFinished)
	if e.Job.ID != j.ID() || e.Job.State != jobs.JobCompleted || e.Job.Files[0].Outputs[0] != filepath.Join(dir, "a.png") {
		t.Fatalf("%+v", e.Job)
	}
	wait(t, events, HistoryChanged)
	if h := a.History(); len(h) != 1 || h[0].JobID != j.ID() {
		t.Fatal(h)
	}
	if len(a.Jobs()) != 1 || a.Settings().SuccessfulConversions != 1 {
		t.Fatal("jobs / counter")
	}
	if _, err := a.Convert([]string{src}, "rotate"); err == nil {
		t.Fatal("a group converted")
	}
	if _, err := a.ConvertTo([]string{src}, "raw"); err == nil {
		t.Fatal("a read-only format was a target")
	}
}

func TestTargetsDonutAndLibrary(t *testing.T) {
	a := newApp(t)
	if got := a.TargetsFor([]string{"/x/a.jpg"}); len(got) != 2 {
		t.Fatal(got)
	}
	if got := a.TargetsFor([]string{"/x/a.jpg", "/x/b.png"}); len(got) != 1 || got[0] != "webp" {
		t.Fatal(got)
	}
	d := a.DonutFor([]string{"/x/a.jpg"})
	if d.Profile.ID != "images" || len(d.Front) == 0 || !d.Front[0].Usable || d.Front[0].Name == "" {
		t.Fatalf("%+v", d.Front)
	}
	p := presets.New("Mine", "webp")
	if err := a.SavePreset(p); err != nil || a.Preset(p.ID) == nil {
		t.Fatal(err)
	}
	p.Name = "Renamed"
	_ = a.SavePreset(p)
	if a.Preset(p.ID).Name != "Renamed" || a.FindPreset("renamed") == nil {
		t.Fatal("replace")
	}
	if err := a.DeletePreset(p.ID); err != nil || a.Preset(p.ID) != nil {
		t.Fatal(err)
	}
	if moved, err := a.MoveProfile("pdf", -1); !moved || err != nil || a.Profiles()[0].ID != "pdf" {
		t.Fatal(moved, err)
	}
	if ok, err := a.ResetProfile("pdf"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if eng := a.Engines(); len(eng) != 1 || !eng[0].Available || eng[0].Version != "7" {
		t.Fatal(eng)
	}
}

func TestSettingsSaveAndWatchFolders(t *testing.T) {
	a := newApp(t)
	in := t.TempDir()
	s := a.Settings()
	rule := watch.NewRule(in, "to-png")
	s.WatchFolders = append(s.WatchFolders, rule)
	s.EnginePriority = []string{"magick"}
	if err := a.SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	events, stop := a.Events(100)
	defer stop()
	a.StartWatchers()
	st := a.WatchFolders()
	if len(st) != 1 || !st[0].Running {
		t.Fatalf("%+v", st)
	}
	_ = os.WriteFile(filepath.Join(in, "w.jpg"), []byte("x"), 0o644)
	_ = watch.DefaultSettleTime
	// The settle time is seconds: wait for the job, generously.
	timeout := time.After(30 * time.Second)
	for {
		select {
		case e := <-events:
			if e.Kind == JobFinished {
				if out := e.Job.Files[0].Outputs; len(out) != 1 || out[0] != filepath.Join(in, "converted", "w.png") {
					t.Fatal(out)
				}
				for i := 0; i < 100 && len(a.WatchFolders()[0].Recent.Failures)+a.WatchFolders()[0].Recent.Converted == 0; i++ {
					time.Sleep(10 * time.Millisecond)
				}
				if a.WatchFolders()[0].Recent.Converted != 1 {
					t.Fatal(a.WatchFolders()[0].Recent)
				}
				return
			}
		case <-timeout:
			t.Fatal("the watch folder never converted")
		}
	}
}
