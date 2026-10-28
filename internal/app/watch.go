package app

import (
	"context"
	"fmt"

	"github.com/dseif0x/filee-go/internal/history"
	"github.com/dseif0x/filee-go/internal/jobs"
	"github.com/dseif0x/filee-go/internal/watch"
)

// WatchStatus is a watch folder as the UI shows it.
type WatchStatus struct {
	Rule    *watch.Rule          `json:"rule"`
	Running bool                 `json:"running"`
	Error   string               `json:"error,omitempty"`
	Recent  history.WatchResults `json:"recent"`
}

// StartWatchers starts every enabled watch folder of the settings (and stops
// those no longer there). It is safe to call again after settings change.
func (a *App) StartWatchers() {
	a.mu.Lock()
	rules := a.store.Settings.WatchFolders
	paused := a.store.Settings.Paused
	wanted := map[string]*watch.Rule{}
	for _, r := range rules {
		if r.Enabled && !paused {
			wanted[r.ID] = r.Clone()
		}
	}
	var stop []*watch.Watcher
	for id, w := range a.watchers {
		if r, ok := wanted[id]; !ok || !sameRule(r, w.Rule()) {
			stop = append(stop, w)
			delete(a.watchers, id)
		}
	}
	var start []*watch.Rule
	for id, r := range wanted {
		if _, ok := a.watchers[id]; !ok {
			start = append(start, r)
		}
	}
	a.mu.Unlock()

	for _, w := range stop {
		w.Close()
		a.emit(Event{Kind: WatchChanged, WatchRuleID: w.Rule().ID})
	}
	for _, r := range start {
		w := watch.New(r, a.watchConvert(r), watch.Options{Log: a.log})
		err := w.Start()
		a.mu.Lock()
		if err != nil {
			a.watchErr[r.ID] = err.Error()
		} else {
			delete(a.watchErr, r.ID)
			a.watchers[r.ID] = w
		}
		a.mu.Unlock()
		ev := Event{Kind: WatchChanged, WatchRuleID: r.ID}
		if err != nil {
			ev.Error = err.Error()
		}
		a.emit(ev)
	}
}

func sameRule(a, b *watch.Rule) bool {
	return fmt.Sprintf("%+v", *a) == fmt.Sprintf("%+v", *b)
}

// StopWatchers stops every watch folder.
func (a *App) StopWatchers() {
	a.mu.Lock()
	ws := a.watchers
	a.watchers = map[string]*watch.Watcher{}
	a.mu.Unlock()
	for _, w := range ws {
		w.Close()
	}
}

func (a *App) restartWatchersIfRunning() {
	a.mu.Lock()
	running := len(a.watchers) > 0 || len(a.watchErr) > 0
	a.mu.Unlock()
	if running {
		a.StartWatchers()
	}
}

// watchConvert runs a watch folder's batch on the queue with its preset as
// the folder applies it, and waits for it.
func (a *App) watchConvert(r *watch.Rule) watch.ConvertFunc {
	return func(ctx context.Context, files []string) *jobs.Job {
		p := a.Preset(r.PresetID)
		if p == nil || p.IsGroup() || p.IsTool() {
			a.log.Warn("watch folder preset is gone", "folder", r.Folder, "preset", r.PresetID)
			return nil
		}
		a.mu.Lock()
		p = a.store.Settings.WithDefaultOutput(p)
		a.mu.Unlock()
		j := jobs.NewJob(files, r.Apply(p), a.Texts().PresetName(p), nil)
		a.track(j, r.ID)
		stop := context.AfterFunc(ctx, j.Cancel)
		defer stop()
		done := make(chan struct{})
		unsub := a.queue.Subscribe(func(e jobs.Event) {
			if e.Finished && e.Job == j {
				close(done)
			}
		})
		defer unsub()
		a.queue.Submit(j)
		<-done
		return j
	}
}

// WatchFolders lists the watch folders of the settings with their state and
// what each converted lately.
func (a *App) WatchFolders() []WatchStatus {
	s := a.Settings()
	entries := a.History()
	hist := make([]*history.Entry, len(entries))
	for i := range entries {
		hist[i] = &entries[i]
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []WatchStatus
	for _, r := range s.WatchFolders {
		_, running := a.watchers[r.ID]
		st := WatchStatus{Rule: r, Running: running, Error: a.watchErr[r.ID]}
		st.Recent = history.WatchResultsOf(hist, r.ID)
		out = append(out, st)
	}
	return out
}
