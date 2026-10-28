// Package jobs runs conversions: a Job is one "drop" (a batch of files
// converted with one preset) and the Queue runs jobs in the background. It
// plans each file's route, runs the steps with intermediate files in a work
// folder, limits how many files each engine converts at once, hands out
// output paths and reports progress. UI code only enqueues and listens.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/presets"
)

// JobState is the state of a whole job.
type JobState string

const (
	JobQueued              JobState = "Queued"
	JobRunning             JobState = "Running"
	JobCompleted           JobState = "Completed"
	JobCompletedWithErrors JobState = "CompletedWithErrors"
	JobFailed              JobState = "Failed"
	JobCancelled           JobState = "Cancelled"
)

// Final reports whether the job is over.
func (s JobState) Final() bool { return s != JobQueued && s != JobRunning }

// FileState is the state of one source file of a job.
type FileState string

const (
	FilePending   FileState = "Pending"
	FileRunning   FileState = "Running"
	FileDone      FileState = "Done"
	FileSkipped   FileState = "Skipped"
	FileFailed    FileState = "Failed"
	FileCancelled FileState = "Cancelled"
)

// TransferProgress is how far saving one source has come: bytes so far, and
// the size when the source says it (Total <= 0 when unknown).
type TransferProgress struct {
	Bytes int64 `json:"bytes"`
	Total int64 `json:"total,omitempty"`
}

// Fraction is 0–1, or false when the size isn't known.
func (t TransferProgress) Fraction() (float64, bool) {
	if t.Total <= 0 {
		return 0, false
	}
	return min(1, max(0, float64(t.Bytes)/float64(t.Total))), true
}

// JobTransfer is what a job is saving now (see Job.Transfer).
type JobTransfer struct {
	// Bytes saved so far by the files being saved.
	Bytes int64 `json:"bytes"`
	// Total is their size together, or -1 when one of them doesn't say.
	Total int64 `json:"total"`
	// Saved files of the job, and Count of those saved first.
	Saved int `json:"saved"`
	Count int `json:"count"`
	// Received is every byte saved so far, finished files included (for the speed).
	Received int64 `json:"received"`
}

// FileResult is the result for one source file. The values of a running job
// change on the queue's goroutines: read them through Job.Files or
// Job.Snapshot, which copy them under the job's lock.
type FileResult struct {
	// SourcePath is the file converted; for a source the job fetches first a
	// placeholder until it has been saved, then where it was saved.
	SourcePath string    `json:"sourcePath"`
	State      FileState `json:"state"`
	Progress   float64   `json:"progress"`
	Outputs    []string  `json:"outputs"`
	// ErrorKey is the localization key of the error (error.no_route), ErrorDetail its raw detail.
	ErrorKey    string `json:"errorKey,omitempty"`
	ErrorDetail string `json:"errorDetail,omitempty"`
	// NeedsFetch: the job's fetcher saves this source first; Saved once it has.
	NeedsFetch bool              `json:"needsFetch,omitempty"`
	Saved      bool              `json:"saved,omitempty"`
	Transfer   *TransferProgress `json:"transfer,omitempty"`
	// NoticeKey is a note on a file that converted (notice.pdf_not_smaller),
	// NoticeReasonKey why (a localization key), NoticeDetail the system's words.
	NoticeKey       string `json:"noticeKey,omitempty"`
	NoticeReasonKey string `json:"noticeReasonKey,omitempty"`
	NoticeDetail    string `json:"noticeDetail,omitempty"`
	// OriginalInTrash: "Replace the original" sent the source to the Trash.
	OriginalInTrash bool `json:"originalInTrash,omitempty"`
	// MissingFonts are document fonts that weren't installed (the renderer substituted them).
	MissingFonts []string `json:"missingFonts,omitempty"`
}

func (f *FileResult) clone() FileResult {
	c := *f
	c.Outputs = slices.Clone(f.Outputs)
	c.MissingFonts = slices.Clone(f.MissingFonts)
	if f.Transfer != nil {
		t := *f.Transfer
		c.Transfer = &t
	}
	return c
}

// stopped: a file that didn't get as far as being converted (saving it failed or was cancelled).
func (f *FileResult) stopped() bool { return f.State == FileFailed || f.State == FileCancelled }

// SourceFetcher brings sources that aren't files yet onto disk when their job
// runs: files dragged out of a web browser are saved (or downloaded) then, so
// the download shows its progress and errors like the conversion.
type SourceFetcher interface {
	// Fetches reports whether the fetcher saves source (the others are files already).
	Fetches(source string) bool
	// Fetch saves the file source stands for and returns its path. A
	// *convert.RefusedError is a reason the user can act on.
	Fetch(ctx context.Context, source string, progress func(TransferProgress)) (string, error)
	// Release is called once when the job is over (finished, failed or
	// cancelled before it ran), before the finished event.
	Release(job *Job)
}

// Job is a batch of files converted with one preset.
type Job struct {
	id         string
	created    time.Time
	preset     *presets.Preset
	presetName string
	fetcher    SourceFetcher
	ctx        context.Context
	cancel     context.CancelFunc

	mu    sync.Mutex
	files []*FileResult
	state JobState
}

// NewJob makes a queued job. The preset is copied: later edits don't affect it.
func NewJob(sources []string, preset *presets.Preset, presetName string, fetcher SourceFetcher) *Job {
	// A Shapefile's .dbf, .shx, .prj, ... dropped with its .shp are part of it.
	sources = formats.WithoutCompanions(sources)
	ctx, cancel := context.WithCancel(context.Background())
	j := &Job{
		id: newJobID(), created: time.Now(), preset: preset.Clone(), presetName: presetName, fetcher: fetcher,
		ctx: ctx, cancel: cancel, state: JobQueued,
	}
	for _, s := range sources {
		j.files = append(j.files, &FileResult{SourcePath: s, State: FilePending, NeedsFetch: fetcher != nil && fetcher.Fetches(s)})
	}
	return j
}

func newJobID() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (j *Job) ID() string           { return j.id }
func (j *Job) CreatedAt() time.Time { return j.created }

// Preset is the job's own copy of the preset; don't change it.
func (j *Job) Preset() *presets.Preset { return j.preset }
func (j *Job) PresetName() string      { return j.presetName }
func (j *Job) Fetcher() SourceFetcher  { return j.fetcher }

// Context is cancelled when the job is.
func (j *Job) Context() context.Context { return j.ctx }

// Cancel requests cancellation; running engines stop as soon as they can.
func (j *Job) Cancel() { j.cancel() }

// Cancelled reports whether cancellation was requested.
func (j *Job) Cancelled() bool { return j.ctx.Err() != nil }

func (j *Job) State() JobState {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.state
}

func (j *Job) setState(s JobState) {
	j.mu.Lock()
	j.state = s
	j.mu.Unlock()
}

// edit changes the job's values under its lock.
func (j *Job) edit(f func()) {
	j.mu.Lock()
	defer j.mu.Unlock()
	f()
}

// Files returns copies of the file results.
func (j *Job) Files() []FileResult {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]FileResult, len(j.files))
	for i, f := range j.files {
		out[i] = f.clone()
	}
	return out
}

// Sources are the source files as their results currently name them.
func (j *Job) Sources() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]string, len(j.files))
	for i, f := range j.files {
		out[i] = f.SourcePath
	}
	return out
}

// Outputs are every file written by the job.
func (j *Job) Outputs() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.outputsLocked()
}

func (j *Job) outputsLocked() []string {
	var out []string
	for _, f := range j.files {
		out = append(out, f.Outputs...)
	}
	return out
}

// Progress is the overall progress 0–1.
func (j *Job) Progress() float64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.progressLocked()
}

func (j *Job) progressLocked() float64 {
	if len(j.files) == 0 {
		return 1
	}
	var sum float64
	for _, f := range j.files {
		sum += f.Progress
	}
	return sum / float64(len(j.files))
}

// Transfer is what the job's fetcher is saving now, summed over the files
// being saved; nil when none is. The total is known only when every one of
// them says its size.
func (j *Job) Transfer() *JobTransfer {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.transferLocked()
}

func (j *Job) transferLocked() *JobTransfer {
	t := JobTransfer{}
	active := 0
	known := true
	for _, f := range j.files {
		if !f.NeedsFetch {
			continue
		}
		t.Count++
		if f.Saved {
			t.Saved++
		}
		if f.Transfer != nil {
			t.Received += f.Transfer.Bytes
		}
		if f.State == FileRunning && !f.Saved && f.Transfer != nil {
			active++
			t.Bytes += f.Transfer.Bytes
			if f.Transfer.Total > 0 {
				t.Total += f.Transfer.Total
			} else {
				known = false
			}
		}
	}
	if active == 0 {
		return nil
	}
	if !known {
		t.Total = -1
	}
	return &t
}

// Snapshot is a consistent copy of a job for the UI (and JSON).
type Snapshot struct {
	ID         string       `json:"id"`
	CreatedAt  time.Time    `json:"createdAt"`
	PresetID   string       `json:"presetId"`
	PresetName string       `json:"presetName"`
	Target     string       `json:"target"`
	State      JobState     `json:"state"`
	Progress   float64      `json:"progress"`
	Files      []FileResult `json:"files"`
	Transfer   *JobTransfer `json:"transfer,omitempty"`
}

// Snapshot copies the job's current values.
func (j *Job) Snapshot() Snapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	s := Snapshot{
		ID: j.id, CreatedAt: j.created, PresetID: j.preset.ID, PresetName: j.presetName, Target: j.preset.TargetFormat,
		State: j.state, Progress: j.progressLocked(), Transfer: j.transferLocked(), Files: make([]FileResult, len(j.files)),
	}
	for i, f := range j.files {
		s.Files[i] = f.clone()
	}
	return s
}

// Count returns how many files are in each of the given states.
func (s Snapshot) Count(states ...FileState) int {
	n := 0
	for _, f := range s.Files {
		if slices.Contains(states, f.State) {
			n++
		}
	}
	return n
}

// Outputs of the snapshot's files.
func (s Snapshot) Outputs() []string {
	var out []string
	for _, f := range s.Files {
		out = append(out, f.Outputs...)
	}
	return out
}

func appendFonts(have, more []string) []string {
	out := slices.Clone(have)
	for _, m := range more {
		if !slices.ContainsFunc(out, func(h string) bool { return strings.EqualFold(h, m) }) {
			out = append(out, m)
		}
	}
	return out
}
