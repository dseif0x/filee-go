// Package history keeps finished conversions as the home page shows them
// ("Recent conversions"), with what Undo and Retry need, and what a watch
// folder converted lately.
package history

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dseif0x/filee-go/internal/jobs"
	"github.com/dseif0x/filee-go/internal/platform"
	"github.com/dseif0x/filee-go/internal/presets"
)

// MaxUndoable: more results than this ("Save every frame", a long PDF split
// into pages) get no Undo, so history.json stays small and an Undo is quick.
const MaxUndoable = 500

// Entry is the persisted summary of a finished job.
type Entry struct {
	JobID      string        `json:"jobId"`
	FinishedAt time.Time     `json:"finishedAt"`
	PresetID   string        `json:"presetId"`
	PresetName string        `json:"presetName"`
	State      jobs.JobState `json:"state"`
	Sources    []string      `json:"sources"`
	Outputs    []string      `json:"outputs"`
	// WatchRuleID is the watch folder the job came from, or "".
	WatchRuleID string    `json:"watchRuleId,omitempty"`
	Failures    []Failure `json:"failures"`
	// Preset is the preset as the job ran it, kept when files failed so they
	// can be converted again the same way. Its PDF password is left out.
	Preset *presets.Preset `json:"preset,omitempty"`
	// Undoable are the results Undo sends to the Trash, as they were when the
	// job finished: Undo is offered only while each is still there unchanged.
	Undoable []Output `json:"undoable"`
	// OriginalsInTrash: "Replace the original" sent a source to the Trash.
	OriginalsInTrash bool `json:"originalsInTrash,omitempty"`
	// Undone: Undo moved the results to the Trash.
	Undone bool `json:"undone,omitempty"`
}

// Output is a result as the job left it.
type Output struct {
	Path       string    `json:"path"`
	Length     int64     `json:"length"`
	WrittenUtc time.Time `json:"writtenUtc"`
	// Trashed: Undo moved it to the Trash.
	Trashed bool `json:"trashed,omitempty"`
}

// Unchanged reports whether the result is still there, the same size and
// last written at the same time.
func (o Output) Unchanged() bool {
	fi, err := os.Stat(o.Path)
	return err == nil && !fi.IsDir() && fi.Size() == o.Length && fi.ModTime().UTC().Equal(o.WrittenUtc.UTC())
}

// Failure is a file a job couldn't convert.
type Failure struct {
	Source      string `json:"source"`
	ErrorKey    string `json:"errorKey,omitempty"`
	ErrorDetail string `json:"errorDetail,omitempty"`
}

// From summarizes a finished job.
func From(j *jobs.Job) *Entry {
	s := j.Snapshot()
	e := &Entry{
		JobID: s.ID, FinishedAt: time.Now(), PresetID: s.PresetID, PresetName: s.PresetName, State: s.State,
		Sources: []string{}, Outputs: []string{}, Failures: []Failure{},
	}
	for _, f := range s.Files {
		e.Sources = append(e.Sources, f.SourcePath)
		e.Outputs = append(e.Outputs, f.Outputs...)
		if f.State == jobs.FileFailed {
			e.Failures = append(e.Failures, Failure{Source: f.SourcePath, ErrorKey: f.ErrorKey, ErrorDetail: f.ErrorDetail})
		}
		if f.OriginalInTrash {
			e.OriginalsInTrash = true
		}
	}
	// history.json isn't the place for a PDF password; and no Retry for
	// hidden parts of a picture (a Filee before them would write it as it is).
	if len(e.Failures) > 0 && !j.Preset().Image.HidesAreas() {
		e.Preset = j.Preset().Clone().WithoutPassword(true)
	}
	e.Undoable = undoable(s)
	return e
}

func undoable(s jobs.Snapshot) []Output {
	out := []Output{}
	seen := map[string]bool{}
	for _, f := range s.Files {
		for _, o := range f.Outputs {
			// A source left as it was is the user's file, not a result.
			if !f.OriginalInTrash && platform.SamePath(o, f.SourcePath) {
				continue
			}
			fi, err := os.Stat(o)
			// An output folder (Extract), or a document whose pictures went
			// into "<name>_files" beside it: Undo only moves files, so the
			// whole job gets none rather than half of it.
			if err != nil || fi.IsDir() {
				return []Output{}
			}
			stem := strings.TrimSuffix(filepath.Base(o), filepath.Ext(o))
			if pi, err := os.Stat(filepath.Join(filepath.Dir(o), stem+"_files")); err == nil && pi.IsDir() {
				return []Output{}
			}
			k := platform.PathKey(o)
			if seen[k] {
				continue
			}
			seen[k] = true
			if len(out) == MaxUndoable {
				return []Output{}
			}
			out = append(out, Output{Path: o, Length: fi.Size(), WrittenUtc: fi.ModTime().UTC()})
		}
	}
	return out
}

// CanUndo: every result is still there unchanged, and nothing was undone yet.
func (e *Entry) CanUndo() bool {
	if e.Undone || len(e.Undoable) == 0 {
		return false
	}
	for _, o := range e.Undoable {
		if !o.Trashed && !o.Unchanged() {
			return false
		}
	}
	return true
}

// Undo moves the results to the Trash with moveToTrash. It stops at the first
// refusal, marking what went; Undone is set once all of them went.
func (e *Entry) Undo(moveToTrash func(string) *platform.TrashRefusal) *platform.TrashRefusal {
	for i := range e.Undoable {
		o := &e.Undoable[i]
		if o.Trashed {
			continue
		}
		if !o.Unchanged() {
			return &platform.TrashRefusal{ReasonKey: "trash.changed", Detail: o.Path}
		}
		if r := moveToTrash(o.Path); r != nil {
			return r
		}
		o.Trashed = true
	}
	e.Undone = true
	return nil
}

// WatchFailure is a file of a watch folder whose last conversion failed, with
// the preset that job ran with.
type WatchFailure struct {
	Failure Failure
	Preset  *presets.Preset
}

// WatchResults are the recent results of one watch folder.
type WatchResults struct {
	Converted int
	Failures  []WatchFailure
}

// IsEmpty: nothing was converted and nothing failed.
func (w WatchResults) IsEmpty() bool { return w.Converted == 0 && len(w.Failures) == 0 }

// WatchResultsOf is what the watch folder ruleID converted lately, from the
// history (newest first): a file that failed and then converted counts as converted.
func WatchResultsOf(history []*Entry, ruleID string) WatchResults {
	seen := map[string]bool{}
	var r WatchResults
	for _, e := range history {
		if e.WatchRuleID != ruleID {
			continue
		}
		for _, src := range e.Sources {
			k := platform.PathKey(src)
			if seen[k] {
				continue // a newer job already decided this file
			}
			seen[k] = true
			var failure *Failure
			for i := range e.Failures {
				if platform.PathKey(e.Failures[i].Source) == k {
					failure = &e.Failures[i]
					break
				}
			}
			if failure == nil {
				r.Converted++
			} else {
				r.Failures = append(r.Failures, WatchFailure{Failure: *failure, Preset: e.Preset})
			}
		}
	}
	return r
}
