package jobs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/platform"
	"github.com/dseif0x/filee-go/internal/presets"
)

// Event tells subscribers that a job changed. Finished is set once, when the
// job reached its final state. Call Job.Snapshot for its values.
type Event struct {
	Job      *Job
	Finished bool
}

// Options configure a queue. Every field may be left zero.
type Options struct {
	// MoveToTrash moves a file to the Recycle Bin / Trash for "Replace the
	// original": nil when it worked, else why not. Without it originals are
	// always kept.
	MoveToTrash func(path string) *platform.TrashRefusal
	Log         *slog.Logger
	// WorkRoot holds every job's work folder (DefaultWorkRoot when "").
	WorkRoot string
	// The helpers for merged, combined and joined results. When nil, the
	// first available converter of the catalog implementing the interface is used.
	PdfMerger   convert.PdfMerger
	TiffMerger  convert.TiffMerger
	MediaJoiner convert.MediaJoiner
	Combiner    convert.FileCombiner
	PdfFinisher convert.PdfFinisher
	ImageJoiner convert.ImageJoiner
}

// Queue runs jobs one after another on a background goroutine.
type Queue struct {
	catalog *convert.Catalog
	opt     Options
	log     *slog.Logger

	gatesMu sync.Mutex
	gates   map[string]chan struct{}

	// outMu guards everything about output paths: the reservations and the
	// folders this queue made.
	outMu    sync.Mutex
	reserved map[string]*reservation
	made     map[string]string

	subMu  sync.RWMutex
	subs   map[int]func(Event)
	nextID int

	pendMu  sync.Mutex
	pending []*Job
	wake    chan struct{}
	closed  bool
	done    chan struct{}

	busy atomic.Int64
}

// reservation is how many conversions hold an output path, and whether more
// than one ever did: then neither may delete it when it fails.
type reservation struct {
	path    string
	holders int
	shared  bool
}

// DefaultWorkRoot is where every job has its work folder, named by its id.
func DefaultWorkRoot() string { return filepath.Join(os.TempDir(), "Filee") }

// NewQueue starts a queue over the catalog's converters.
func NewQueue(catalog *convert.Catalog, o Options) *Queue {
	q := &Queue{
		catalog: catalog, opt: o, log: o.Log,
		gates: map[string]chan struct{}{}, reserved: map[string]*reservation{}, made: map[string]string{},
		subs: map[int]func(Event){}, wake: make(chan struct{}, 1), done: make(chan struct{}),
	}
	if q.log == nil {
		q.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if q.opt.WorkRoot == "" {
		q.opt.WorkRoot = DefaultWorkRoot()
	}
	go q.loop()
	return q
}

// WorkRoot is the folder of the jobs' work folders.
func (q *Queue) WorkRoot() string { return q.opt.WorkRoot }

// Subscribe registers fn for every job event and returns the function that
// removes it. fn runs on the queue's goroutines while a job runs: it must be
// quick and must not block (hand the event to the UI thread).
func (q *Queue) Subscribe(fn func(Event)) (unsubscribe func()) {
	q.subMu.Lock()
	id := q.nextID
	q.nextID++
	q.subs[id] = fn
	q.subMu.Unlock()
	return func() {
		q.subMu.Lock()
		delete(q.subs, id)
		q.subMu.Unlock()
	}
}

func (q *Queue) emit(e Event) {
	q.subMu.RLock()
	fns := make([]func(Event), 0, len(q.subs))
	for _, f := range q.subs {
		fns = append(fns, f)
	}
	q.subMu.RUnlock()
	for _, f := range fns {
		func() {
			defer func() {
				if r := recover(); r != nil {
					q.log.Warn("job event handler panicked", "panic", r)
				}
			}()
			f(e)
		}()
	}
}

func (q *Queue) notify(j *Job) { q.emit(Event{Job: j}) }

// Busy reports whether a job waits in the queue or runs.
func (q *Queue) Busy() bool { return q.busy.Load() > 0 }

// Enqueue adds a job to the queue and returns it at once.
func (q *Queue) Enqueue(sources []string, preset *presets.Preset, presetName string, fetcher SourceFetcher) *Job {
	j := NewJob(sources, preset, presetName, fetcher)
	q.Submit(j)
	return j
}

// Submit adds an existing job to the queue.
func (q *Queue) Submit(j *Job) {
	q.busy.Add(1) // until the worker is done with it
	q.pendMu.Lock()
	closed := q.closed
	if !closed {
		q.pending = append(q.pending, j)
	}
	q.pendMu.Unlock()
	if closed {
		q.busy.Add(-1)
		j.Cancel()
		j.setState(JobCancelled)
		q.release(j)
		q.emit(Event{Job: j, Finished: true})
		return
	}
	q.notify(j)
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// Close lets the jobs already queued finish, then stops the worker.
func (q *Queue) Close() {
	q.pendMu.Lock()
	if q.closed {
		q.pendMu.Unlock()
		<-q.done
		return
	}
	q.closed = true
	q.pendMu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
	<-q.done
}

func (q *Queue) loop() {
	defer close(q.done)
	for {
		q.pendMu.Lock()
		if len(q.pending) == 0 {
			closed := q.closed
			q.pendMu.Unlock()
			if closed {
				return
			}
			<-q.wake
			continue
		}
		j := q.pending[0]
		q.pending = q.pending[1:]
		q.pendMu.Unlock()
		if j.Cancelled() {
			j.setState(JobCancelled)
			q.release(j)
			q.notify(j)
			q.emit(Event{Job: j, Finished: true})
		} else {
			q.Run(j)
		}
		q.busy.Add(-1)
	}
}

// Run runs a job to completion on the calling goroutine (the command line
// and tests use it; Enqueue's worker too).
func (q *Queue) Run(j *Job) {
	q.busy.Add(1)
	defer q.busy.Add(-1)
	q.runJob(j)
}

func (q *Queue) runJob(j *Job) {
	j.setState(JobRunning)
	q.notify(j)
	workDir := filepath.Join(q.opt.WorkRoot, j.id)
	_ = os.MkdirAll(workDir, 0o755)
	func() {
		defer func() {
			if r := recover(); r != nil {
				q.log.Error("job failed", "job", j.id, "panic", r, "stack", string(debug.Stack()))
				j.setState(JobFailed)
			}
		}()
		q.runJobBody(j, workDir)
	}()

	q.outMu.Lock()
	for _, o := range j.Outputs() {
		q.unreserve(o)
	}
	// Folders no running job writes into any more are the user's from now on.
	for k, folder := range q.made {
		if !q.hasReservationIn(folder) {
			delete(q.made, k)
		}
	}
	q.outMu.Unlock()
	_ = os.RemoveAll(workDir)
	q.release(j)
	q.notify(j)
	q.emit(Event{Job: j, Finished: true})
}

func (q *Queue) runJobBody(j *Job, workDir string) {
	if j.fetcher != nil {
		q.fetchSources(j)
	}
	planner := q.catalog.Planner()
	p := j.preset
	merger := q.pdfMerger()
	merge := p.Pdf.MergeIntoSingle && p.TargetFormat == "pdf" && len(j.files) > 1 && merger != nil
	// Any files, one of them or many, archives or not: they go into the archive as they are.
	combiner := q.combiner(p.TargetFormat)
	combine := p.PacksIntoOneArchive() && combiner != nil
	// One file is enough: a 10-page PDF becomes one 10-page TIFF.
	tiff := q.tiffMerger()
	multiPageTiff := p.Image.MultiPageTiff && p.TargetFormat == "tiff" && tiff != nil
	// Clips back to back, or pictures one after the other, in the order they were dropped.
	joiner := q.mediaJoiner()
	join := p.JoinsIntoOneMedia() && joiner != nil
	var imageJoiner convert.ImageJoiner
	if p.JoinsImages() {
		imageJoiner = q.imageJoiner()
	}

	switch {
	case join:
		q.runJoinedMedia(j, planner, workDir, joiner)
	case imageJoiner != nil:
		q.runMergedImages(j, planner, workDir, imageJoiner)
	case merge:
		q.runMerged(j, planner, workDir, merger)
	case multiPageTiff:
		q.runMultiPageTiff(j, planner, workDir, tiff)
	case combine:
		q.runCombined(j, workDir, combiner)
	default:
		parallel(len(j.files), runtime.NumCPU(), func(i int) {
			q.runFile(j, j.files[i], i+1, planner, workDir, true, fileOptions{})
		})
	}

	j.edit(func() {
		allDone, anyDone := true, false
		for _, f := range j.files {
			if f.State != FileDone && f.State != FileSkipped {
				allDone = false
			}
			if f.State == FileDone {
				anyDone = true
			}
		}
		switch {
		case j.Cancelled():
			j.state = JobCancelled
		case allDone:
			j.state = JobCompleted
		case anyDone:
			j.state = JobCompletedWithErrors
		default:
			j.state = JobFailed
		}
	})
}

// parallel runs fn for 0…n-1 on at most workers goroutines and waits.
func parallel(n, workers int, fn func(i int)) {
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			fn(i)
		}()
	}
	wg.Wait()
}

func (q *Queue) release(j *Job) {
	if j.fetcher == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			q.log.Warn("cleaning up after job failed", "job", j.id, "panic", r)
		}
	}()
	j.fetcher.Release(j)
}

// ---- the helpers a job may need, from the options or the catalog ----

func firstAvailable[T any](q *Queue) T {
	var zero T
	if q.catalog == nil {
		return zero
	}
	for _, c := range q.catalog.All() {
		if t, ok := c.(T); ok && q.catalog.StatusOf(c).Available {
			return t
		}
	}
	return zero
}

func (q *Queue) pdfMerger() convert.PdfMerger {
	if q.opt.PdfMerger != nil {
		return q.opt.PdfMerger
	}
	return firstAvailable[convert.PdfMerger](q)
}

func (q *Queue) tiffMerger() convert.TiffMerger {
	if q.opt.TiffMerger != nil {
		return q.opt.TiffMerger
	}
	return firstAvailable[convert.TiffMerger](q)
}

func (q *Queue) mediaJoiner() convert.MediaJoiner {
	if q.opt.MediaJoiner != nil {
		return q.opt.MediaJoiner
	}
	return firstAvailable[convert.MediaJoiner](q)
}

func (q *Queue) pdfFinisher() convert.PdfFinisher {
	if q.opt.PdfFinisher != nil {
		return q.opt.PdfFinisher
	}
	return firstAvailable[convert.PdfFinisher](q)
}

func (q *Queue) imageJoiner() convert.ImageJoiner {
	if q.opt.ImageJoiner != nil {
		return q.opt.ImageJoiner
	}
	return firstAvailable[convert.ImageJoiner](q)
}

func (q *Queue) combiner(format string) convert.FileCombiner {
	if q.opt.Combiner != nil {
		return q.opt.Combiner
	}
	if q.catalog == nil {
		return nil
	}
	for _, c := range q.catalog.All() {
		if t, ok := c.(convert.FileCombiner); ok && q.catalog.StatusOf(c).Available && t.CanCombineInto(format) {
			return t
		}
	}
	return nil
}

// ---- engine gates ----

func (q *Queue) gateFor(c convert.Converter) chan struct{} {
	q.gatesMu.Lock()
	defer q.gatesMu.Unlock()
	g, ok := q.gates[c.ID()]
	if !ok {
		n := c.MaxParallelism()
		if n <= 0 {
			n = runtime.NumCPU()
		}
		g = make(chan struct{}, n)
		q.gates[c.ID()] = g
	}
	return g
}

// ---- fetching ----

// fetchShare is the part of a fetched file's progress that saving it takes.
const fetchShare = 0.5

// fetchSources saves the sources the job's fetcher brings before anything is
// converted. A file that can't be saved fails with its reason; the others still convert.
func (q *Queue) fetchSources(j *Job) {
	var files []*FileResult
	for _, f := range j.files {
		if f.NeedsFetch {
			files = append(files, f)
		}
	}
	parallel(len(files), 4, func(i int) {
		f := files[i]
		var source string
		j.edit(func() {
			f.State = FileRunning
			f.Transfer = &TransferProgress{}
			source = f.SourcePath
		})
		q.notify(j)
		progress := func(p TransferProgress) {
			changed := false
			j.edit(func() {
				if f.State != FileRunning || f.Saved {
					return
				}
				t := p
				f.Transfer = &t
				fr, _ := p.Fraction()
				if v := fr * fetchShare; v > f.Progress {
					f.Progress = v
				}
				changed = true
			})
			if changed {
				q.notify(j)
			}
		}
		path, err := safeFetch(j, source, progress)
		var refused *convert.RefusedError
		j.edit(func() {
			switch {
			case err == nil:
				f.SourcePath = path
				f.Saved = true
				f.State = FilePending
				f.Progress = fetchShare
			case j.Cancelled() || errors.Is(err, context.Canceled):
				f.State = FileCancelled
			case errors.As(err, &refused):
				q.log.Info("saving a file was refused", "file", source, "reason", err)
				failLocked(f, refused.Key, refused.Detail)
			default:
				q.log.Warn("saving a file failed", "file", source, "error", err)
				failLocked(f, "error.web_download_failed", err.Error())
			}
		})
		q.notify(j)
	})
}

func safeFetch(j *Job, source string, progress func(TransferProgress)) (path string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	return j.fetcher.Fetch(j.ctx, source, progress)
}

func failLocked(f *FileResult, key, detail string) {
	f.State = FileFailed
	f.ErrorKey = key
	f.ErrorDetail = detail
	f.Progress = 1
}

func (q *Queue) failFile(j *Job, f *FileResult, key, detail string) {
	j.edit(func() { failLocked(f, key, detail) })
	q.notify(j)
}

// failUnturned: a file would be used as it is although the preset turns,
// mirrors or covers parts of it.
func (q *Queue) failUnturned(j *Job, f *FileResult, detail string) {
	key := "error.cannot_turn"
	if j.preset.Image.HidesAreas() {
		key = "error.cannot_hide"
	}
	q.failFile(j, f, key, detail)
}

// isEmptyFile: a file (not a folder) with nothing in it.
func isEmptyFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular() && fi.Size() == 0
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func detectID(path string) string {
	if f := formats.Detect(path); f != nil {
		return f.ID
	}
	return ""
}

// classify turns an engine error into the file's state: cancelled, refused
// with a reason, or failed.
func classify(j *Job, err error) (cancelled bool, key, detail string) {
	var refused *convert.RefusedError
	switch {
	case j.Cancelled() || errors.Is(err, context.Canceled):
		return true, "", ""
	case errors.As(err, &refused):
		return false, refused.Key, refused.Detail
	}
	return false, "error.conversion_failed", err.Error()
}

// finishPdfs applies the preset's PDF finishing to the PDFs among outputs.
func (q *Queue) finishPdfs(ctx context.Context, p *presets.Preset, outputs []string) error {
	// A retry from history whose preset (and so its password) is gone: no PDF
	// without the password it was meant to have.
	if p.Pdf.PasswordLeftOut && p.Pdf.Password == "" {
		return convert.Refuse("error.pdf_password_gone", "")
	}
	if !p.Pdf.FinishesPdf() {
		return nil
	}
	finisher := q.pdfFinisher()
	if finisher == nil {
		return nil
	}
	for _, o := range outputs {
		if detectID(o) == "pdf" {
			if err := finisher.FinishPdf(ctx, o, &p.Pdf); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkSizeLimit refuses when a result is over its limit (#553): the engines
// fit pictures and videos where they can; this catches the routes they don't.
func checkSizeLimit(p *presets.Preset, outputs []string) error {
	if _, detail := presets.OverSizeLimit(p, outputs); detail != "" {
		return convert.Refuse("error.over_size_limit", detail)
	}
	return nil
}

// safe runs f, turning a panic into an error.
func safe(f func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	return f()
}

func uniqueStrings(in []string) []string {
	var out []string
	for _, s := range in {
		if !slices.ContainsFunc(out, func(o string) bool { return platform.SamePath(o, s) }) {
			out = append(out, s)
		}
	}
	return out
}
