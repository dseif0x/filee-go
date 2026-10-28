package history

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/jobs"
	"github.com/dseif0x/filee-go/internal/platform"
	"github.com/dseif0x/filee-go/internal/presets"
)

type conv struct{}

func (conv) ID() string             { return "c" }
func (conv) Name() string           { return "c" }
func (conv) Edges() []convert.Edge  { return []convert.Edge{convert.E("jpg", "pdf")} }
func (conv) MaxParallelism() int    { return 0 }
func (conv) Status() convert.Status { return convert.Available("", "") }
func (conv) Convert(_ context.Context, st *convert.Step, _ convert.Progress) ([]string, error) {
	p := st.Output.Allocate(st.To, "")
	return []string{p}, os.WriteFile(p, []byte("pdf"), 0o644)
}

func TestAnEntryKeepsWhatRetryAndUndoNeedButNoPassword(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "a.jpg")
	_ = os.WriteFile(good, []byte("x"), 0o644)
	q := jobs.NewQueue(convert.NewCatalog(conv{}), jobs.Options{WorkRoot: t.TempDir()})
	defer q.Close()
	p := presets.New("", "pdf")
	p.Pdf.Password = "secret"
	j := jobs.NewJob([]string{good, filepath.Join(dir, "b.docx")}, p, "PDF", nil)
	q.Run(j)
	e := From(j)
	if e.State != jobs.JobCompletedWithErrors || len(e.Failures) != 1 || e.Failures[0].ErrorKey != "error.no_route" {
		t.Fatalf("%+v", e)
	}
	if e.Preset == nil || e.Preset.Pdf.Password != "" || !e.Preset.Pdf.PasswordLeftOut {
		t.Fatal("password kept")
	}
	if len(e.Undoable) != 1 || !e.CanUndo() {
		t.Fatal(e.Undoable)
	}
	trash := t.TempDir()
	if r := e.Undo(func(p string) *platform.TrashRefusal {
		_ = os.Rename(p, filepath.Join(trash, filepath.Base(p)))
		return nil
	}); r != nil || !e.Undone || e.CanUndo() {
		t.Fatal(r)
	}
	res := WatchResultsOf([]*Entry{{WatchRuleID: "w", Sources: []string{good}}, {WatchRuleID: "w", Sources: []string{good, "x"}, Failures: []Failure{{Source: good}, {Source: "x"}}}}, "w")
	if res.Converted != 1 || len(res.Failures) != 1 || res.Failures[0].Failure.Source != "x" {
		t.Fatalf("%+v", res)
	}
}
