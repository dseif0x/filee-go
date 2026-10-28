package jobs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/presets"
)

// fake "converts" by writing the input name and target format into the output file.
type fake struct {
	id    string
	edges []convert.Edge
	off   atomic.Bool

	outputsPerInput int
	err             error
	fonts           []string
	// pictureFolder also writes a "_files" folder beside the output (Markdown);
	// reserveFolder takes it from the allocator rather than just writing it.
	pictureFolder, reserveFolder bool
	// before runs at the start of every step.
	before func(*convert.Step)
	// after runs once the outputs are written.
	after func()

	mu    sync.Mutex
	steps []fakeStep
}

type fakeStep struct {
	from, to, input, original string
	frames                    bool
	preset                    *presets.Preset
}

func newFake(id string, edges ...convert.Edge) *fake {
	return &fake{id: id, edges: edges, outputsPerInput: 1}
}

func (f *fake) ID() string            { return f.id }
func (f *fake) Name() string          { return f.id }
func (f *fake) Edges() []convert.Edge { return f.edges }
func (f *fake) MaxParallelism() int   { return 0 }
func (f *fake) Status() convert.Status {
	if f.off.Load() {
		return convert.Unavailable("engine.reason.not_installed", "")
	}
	return convert.Available("", "")
}

func (f *fake) Steps() []fakeStep {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.steps)
}

func (f *fake) Convert(ctx context.Context, st *convert.Step, progress convert.Progress) ([]string, error) {
	f.mu.Lock()
	f.steps = append(f.steps, fakeStep{st.From, st.To, st.InputPath, st.OriginalSourcePath, st.FrameSequence, st.Preset.Clone()})
	f.mu.Unlock()
	if f.before != nil {
		f.before(st)
	}
	if f.err != nil {
		return nil, f.err
	}
	if len(f.fonts) > 0 && st.MissingFonts != nil {
		st.MissingFonts(f.fonts)
	}
	var written []string
	for i := 0; i < f.outputsPerInput; i++ {
		suffix := ""
		if f.outputsPerInput > 1 {
			suffix = "_p" + string(rune('1'+i))
		}
		path := st.Output.Allocate(st.To, suffix)
		if path == "" {
			continue
		}
		if err := os.WriteFile(path, []byte(filepath.Base(st.InputPath)+"→"+st.To+" by "+f.id), 0o644); err != nil {
			return nil, err
		}
		written = append(written, path)
		if f.pictureFolder {
			stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			folder := filepath.Join(filepath.Dir(path), stem+"_files")
			if f.reserveFolder {
				folder = st.Output.Allocate("", "_files")
			}
			_ = os.MkdirAll(folder, 0o755)
			_ = os.WriteFile(filepath.Join(folder, "img0001.png"), []byte("picture"), 0o644)
		}
	}
	progress.Report(1)
	if f.after != nil {
		f.after()
	}
	return written, ctx.Err()
}

// turner is a fake that turns images.
type turner struct{ *fake }

func (turner) Transforms(from, to string) bool { return true }

// shaper is a fake whose unshaped edges check the scripts of a file.
type shaper struct {
	*fake
	script, widths func(path string) string
	mu             sync.Mutex
	checked        []string
}

func (s *shaper) ScanScripts(path, format string) convert.TextScripts {
	s.mu.Lock()
	s.checked = append(s.checked, filepath.Base(path))
	s.mu.Unlock()
	t := convert.TextScripts{}
	if s.script != nil {
		t.NeedsShaping = s.script(path)
	}
	if s.widths != nil {
		t.WithoutWidths = s.widths(path)
	}
	return t
}

// recorder writes the names of what it was given.
type recorder struct {
	mu     sync.Mutex
	inputs []string
	err    error
}

func (r *recorder) record(inputs []string, output string) error {
	if r.err != nil {
		return r.err
	}
	r.mu.Lock()
	var names []string
	for _, i := range inputs {
		r.inputs = append(r.inputs, i)
		names = append(names, filepath.Base(i))
	}
	r.mu.Unlock()
	return os.WriteFile(output, []byte(strings.Join(names, "|")), 0o644)
}

type pdfMerger struct{ recorder }

func (m *pdfMerger) MergePdfs(_ context.Context, inputs []string, output string) error {
	return m.record(inputs, output)
}

type tiffMerger struct{ recorder }

func (m *tiffMerger) MergeTiffs(_ context.Context, inputs []string, output string, _ *presets.ImageOptions) error {
	return m.record(inputs, output)
}

type combiner struct{ recorder }

func (c *combiner) CanCombineInto(string) bool { return true }
func (c *combiner) Combine(_ context.Context, inputs []string, output, _ string, _ *presets.Preset, _ string, p convert.Progress) error {
	p.Report(0.5)
	return c.record(inputs, output)
}

type mediaJoiner struct {
	recorder
	joining func()
}

func (m *mediaJoiner) JoinMedia(_ context.Context, inputs []string, output, _ string, _ *presets.Preset, _ string, p convert.Progress) error {
	if m.joining != nil {
		m.joining()
	}
	return m.record(inputs, output)
}

type finisher struct {
	mu       sync.Mutex
	finished []string
}

func (f *finisher) FinishPdf(_ context.Context, path string, _ *presets.PdfOptions) error {
	f.mu.Lock()
	f.finished = append(f.finished, filepath.Base(path))
	f.mu.Unlock()
	return nil
}

// imageJoiner is a fake converter that also merges pictures.
type imageJoiner struct {
	*fake
	reads, writes []string
	recorder
	formats []string
	tooBig  bool
}

func (i *imageJoiner) CanRead(f string) bool  { return slices.Contains(i.reads, f) }
func (i *imageJoiner) CanWrite(f string) bool { return slices.Contains(i.writes, f) }
func (i *imageJoiner) JoinImages(_ context.Context, inputs []convert.ImageInput, output, format string, _ *presets.ImageOptions, _ convert.Progress) error {
	if i.tooBig {
		_ = os.WriteFile(output, []byte("partial"), 0o644)
		return convert.Refuse("error.merged_image_too_large", "")
	}
	var paths []string
	for _, in := range inputs {
		paths = append(paths, in.Path)
		i.formats = append(i.formats, in.Format)
	}
	return i.record(paths, output)
}

// ---- helpers ----

type env struct {
	t   *testing.T
	dir string
}

func newEnv(t *testing.T) *env { return &env{t: t, dir: t.TempDir()} }

func (e *env) file(name, content string) string {
	p := filepath.Join(e.dir, name)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
	return p
}

func (e *env) path(name string) string { return filepath.Join(e.dir, name) }

func (e *env) queue(o Options, converters ...convert.Converter) *Queue {
	if o.WorkRoot == "" {
		o.WorkRoot = filepath.Join(e.dir, ".work")
	}
	q := NewQueue(convert.NewCatalog(converters...), o)
	e.t.Cleanup(q.Close)
	return q
}

func run(q *Queue, files []string, p *presets.Preset) *Job {
	j := NewJob(files, p, "Test", nil)
	q.Run(j)
	return j
}

func target(t string) *presets.Preset { return presets.New("", t) }

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

var errBoom = errors.New("boom")
