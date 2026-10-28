package radial

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/dseif0x/filee-go/internal/app"
	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/donut"
	"github.com/dseif0x/filee-go/internal/jobs"
	"github.com/dseif0x/filee-go/internal/presets"
)

type fakeEngine struct{}

func (fakeEngine) ID() string   { return "magick" }
func (fakeEngine) Name() string { return "ImageMagick" }
func (fakeEngine) Edges() []convert.Edge {
	return []convert.Edge{convert.E("jpg", "png"), convert.E("png", "jpg"), convert.E("jpg", "webp"), convert.E("jpg", "jpg")}
}
func (fakeEngine) MaxParallelism() int    { return 0 }
func (fakeEngine) Status() convert.Status { return convert.Available("", "7") }
func (fakeEngine) Convert(_ context.Context, st *convert.Step, _ convert.Progress) ([]string, error) {
	p := st.Output.Allocate(st.To, "")
	if p == "" {
		return nil, nil
	}
	return []string{p}, os.WriteFile(p, []byte(st.To), 0o644)
}

type timer struct {
	at time.Duration
	f  func()
}

type fakeHost struct {
	shown, active, backdrop, watching bool
	side                              float64
	timers                            []*timer
	notified                          []jobs.Snapshot
	tools                             []string
}

func (h *fakeHost) ShowOverlay(x, y, side float64, activate bool) {
	h.shown, h.side, h.active = true, side, activate
}
func (h *fakeHost) HideOverlay()           { h.shown = false }
func (h *fakeHost) Redraw()                {}
func (h *fakeHost) SetHitBackdrop(on bool) { h.backdrop = on }
func (h *fakeHost) After(d time.Duration, f func()) func() {
	t := &timer{d, f}
	h.timers = append(h.timers, t)
	return func() { t.f = nil }
}
func (h *fakeHost) WatchFlipKey(on bool)   { h.watching = on }
func (h *fakeHost) Notify(s jobs.Snapshot) { h.notified = append(h.notified, s) }
func (h *fakeHost) ShowTool(tool string, _ *presets.Preset, files []string) {
	h.tools = append(h.tools, tool)
}
func (h *fakeHost) OpenSettings(string)                {}
func (h *fakeHost) ChooseFolder(string) (string, bool) { return "", false }
func (h *fakeHost) Reveal(string)                      {}
func (h *fakeHost) Log() *slog.Logger                  { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// fire runs the timers due within d.
func (h *fakeHost) fire(d time.Duration) {
	due := slices.Clone(h.timers)
	h.timers = nil
	for _, t := range due {
		if t.at <= d {
			if t.f != nil {
				t.f()
			}
		} else {
			h.timers = append(h.timers, t)
		}
	}
}

func newApp(t *testing.T) *app.App {
	a, err := app.New(app.Options{DataDir: t.TempDir(), Catalog: convert.NewCatalog(fakeEngine{}), WorkRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a
}

func indexOf(m *donut.Menu, id string) int {
	return slices.IndexFunc(m.Items(), func(i donut.Item) bool { return i.ID == id })
}

func TestDragDropConvertsAndShowsTheRing(t *testing.T) {
	a := newApp(t)
	h := &fakeHost{}
	c := New(a, h)
	dir := t.TempDir()
	src := filepath.Join(dir, "photo.jpg")
	_ = os.WriteFile(src, []byte("jpg"), 0o644)

	c.ShowForDrag(300, 300)
	if !h.shown || !h.backdrop || c.Menu().Opacity != 0 {
		t.Fatal("the drag overlay should wait invisibly")
	}
	if !c.DragEntered([]string{src}, nil) || !c.HasFiles() || c.Menu().Count() == 0 {
		t.Fatal("files didn't fill the donut")
	}
	if h.backdrop || c.Menu().Opacity != 1 {
		t.Error("the donut stayed hidden")
	}
	m := c.Menu()
	gray := indexOf(m, "half-size")
	avif := indexOf(m, "to-pdf")
	if gray < 0 || avif < 0 {
		t.Fatalf("image donut lacks its slices: %v", m.Items())
	}
	if m.Items()[avif].Enabled || m.Items()[avif].DisabledReason == "" {
		t.Error("PDF has no engine here but isn't greyed out")
	}
	if c.DragOver(m.SliceCenter(avif)) {
		t.Error("a drop on a greyed-out slice would convert")
	}
	if !m.Items()[gray].Enabled || !c.DragOver(m.SliceCenter(gray)) {
		t.Fatalf("grayscale: %+v", m.Items()[gray])
	}
	if !c.Drop(m.SliceCenter(gray), []string{src}, nil) {
		t.Fatal("drop refused")
	}
	if !m.IsInProgressMode() {
		t.Fatal("no progress ring")
	}
	deadline := time.Now().Add(10 * time.Second)
	var job *jobs.Job
	for _, s := range a.Jobs() {
		job = a.Job(s.ID)
	}
	for !job.State().Final() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	c.JobChanged(job.Snapshot())
	for i := 0; i < 400 && m.Advance(1.0/60); i++ {
	}
	if job.State() != jobs.JobCompleted || h.shown {
		t.Errorf("job %v, overlay still shown %v", job.State(), h.shown)
	}
	if len(h.notified) != 0 {
		t.Error("a quick success shouldn't notify")
	}
}

func TestEmptyDragClosesAndClickMode(t *testing.T) {
	a := newApp(t)
	h := &fakeHost{}
	c := New(a, h)
	c.ShowForDrag(10, 10)
	c.OnDragGestureEnded()
	if h.shown || c.Visible() {
		t.Fatal("a drag without files kept the overlay")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "a.jpg")
	_ = os.WriteFile(src, []byte("x"), 0o644)
	c.ShowForFiles(50, 50, []string{src}, false)
	if !h.active || !c.ClickMode() {
		t.Fatal("click mode didn't activate")
	}
	m := c.Menu()
	crop := indexOf(m, "tool-hide")
	if crop < 0 {
		t.Fatal("no hide tool")
	}
	m.PointerPressed(m.SliceCenter(crop), false)
	m.PointerReleased(m.SliceCenter(crop))
	if len(h.tools) != 1 || h.tools[0] != "hide" || c.Visible() {
		t.Errorf("tool %v visible %v", h.tools, c.Visible())
	}
	c.ShowForFiles(50, 50, []string{src}, false)
	c.Escape()
	for i := 0; i < 100 && m.Advance(1.0/60); i++ {
	}
	if h.shown {
		t.Error("Esc didn't close")
	}
}
