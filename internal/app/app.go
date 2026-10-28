// Package app is the facade the user interfaces (the macOS GUI, the web
// settings UI) are built on. It owns the user's data (internal/settings), the
// engine catalog and route planner, the job queue, the watch folders and the
// history, and tells its subscribers about every change as an Event.
//
// Everything is UI-agnostic and safe to call from any goroutine. Getters
// return copies: change them and hand them back to a Save method. Events are
// delivered on background goroutines; a subscriber must return quickly (hand
// the event to its UI thread).
//
// Typical use:
//
//	a, err := app.New(app.Options{})       // loads settings, presets, profiles, history
//	defer a.Close()
//	a.StartWatchers()                       // the enabled watch folders
//	stop := a.Subscribe(func(e app.Event) { ... })
//	job, err := a.Convert(paths, "to-png")  // or ConvertTo(paths, "png")
//	snap := job.Snapshot()                  // progress, per-file state, outputs
package app

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/all"
	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/history"
	"github.com/dseif0x/filee-go/internal/i18n"
	"github.com/dseif0x/filee-go/internal/jobs"
	"github.com/dseif0x/filee-go/internal/platform"
	"github.com/dseif0x/filee-go/internal/presets"
	"github.com/dseif0x/filee-go/internal/profiles"
	"github.com/dseif0x/filee-go/internal/settings"
	"github.com/dseif0x/filee-go/internal/watch"
	"github.com/dseif0x/filee-go/internal/web"
)

// Version is the app version (set by the build with -ldflags "-X ...app.Version=").
var Version = "0.1.0-dev"

// Options configure New; every field may be left zero.
type Options struct {
	// DataDir holds settings.json & co. ("" for settings.DefaultDir()).
	DataDir string
	// Catalog is the engines (nil for internal/engines/all).
	Catalog *convert.Catalog
	// Platform is the OS services (nil for platform.Null).
	Platform platform.Services
	Log      *slog.Logger
	// WorkRoot holds the jobs' work folders ("" for jobs.DefaultWorkRoot()).
	WorkRoot string
	// KeepJobs is how many finished jobs Jobs() still lists (default 50).
	KeepJobs int
}

// EventKind says what changed.
type EventKind string

const (
	// JobUpdated: a job's state or progress changed (Job is set).
	JobUpdated EventKind = "job.updated"
	// JobFinished: a job reached its final state, once (Job is set).
	JobFinished     EventKind = "job.finished"
	SettingsChanged EventKind = "settings.changed"
	LibraryChanged  EventKind = "library.changed"
	HistoryChanged  EventKind = "history.changed"
	// WatchChanged: a watch folder started, stopped or failed (WatchRuleID, Error).
	WatchChanged EventKind = "watch.changed"
	// EnginesChanged: the engines' availability was checked again.
	EnginesChanged EventKind = "engines.changed"
)

// Event is one change. For job events Job is a consistent snapshot.
type Event struct {
	Kind        EventKind      `json:"kind"`
	Job         *jobs.Snapshot `json:"job,omitempty"`
	WatchRuleID string         `json:"watchRuleId,omitempty"`
	Error       string         `json:"error,omitempty"`
}

// App is the running Filee without its windows.
type App struct {
	log      *slog.Logger
	platform platform.Services
	catalog  *convert.Catalog
	queue    *jobs.Queue
	dl       *web.Downloader
	keepJobs int

	mu       sync.Mutex // guards store, watchers, jobs
	store    *settings.Store
	watchers map[string]*watch.Watcher
	watchErr map[string]string
	jobList  []*jobs.Job
	jobRules map[string]string // job id → watch rule id

	subMu sync.RWMutex
	subs  map[int]func(Event)
	subID int

	unsubQueue func()
	closeOnce  sync.Once
}

// New loads the user's data and starts the job queue.
func New(o Options) (*App, error) {
	if o.Log == nil {
		o.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if o.Platform == nil {
		o.Platform = platform.Null{}
	}
	if o.Catalog == nil {
		o.Catalog = all.Catalog()
	}
	if o.KeepJobs <= 0 {
		o.KeepJobs = 50
	}
	store := settings.NewStore(o.DataDir, o.Log)
	if err := store.Load(); err != nil {
		return nil, fmt.Errorf("loading settings: %w", err)
	}
	a := &App{
		log: o.Log, platform: o.Platform, catalog: o.Catalog, store: store, keepJobs: o.KeepJobs,
		watchers: map[string]*watch.Watcher{}, watchErr: map[string]string{}, jobRules: map[string]string{},
		subs: map[int]func(Event){}, dl: web.NewDownloader(nil, "Filee/"+Version),
	}
	a.catalog.SetPriority(store.Settings.EnginePriority)
	i18n.SetLanguage(store.Settings.Language)
	a.queue = jobs.NewQueue(a.catalog, jobs.Options{MoveToTrash: o.Platform.MoveToTrash, Log: o.Log, WorkRoot: o.WorkRoot})
	a.unsubQueue = a.queue.Subscribe(a.onJob)
	// Work folders a crash left behind (a day old) go.
	go jobs.DeleteStaleWorkFolders(a.queue.WorkRoot(), 24*time.Hour)
	go web.DeleteStale(web.DefaultDropRoot(), 24*time.Hour)
	return a, nil
}

// Close stops the watch folders and lets queued jobs finish.
func (a *App) Close() {
	a.closeOnce.Do(func() {
		a.StopWatchers()
		a.queue.Close()
		a.unsubQueue()
	})
}

// Subscribe registers fn for every event and returns the function that removes it.
func (a *App) Subscribe(fn func(Event)) (unsubscribe func()) {
	a.subMu.Lock()
	id := a.subID
	a.subID++
	a.subs[id] = fn
	a.subMu.Unlock()
	return func() {
		a.subMu.Lock()
		delete(a.subs, id)
		a.subMu.Unlock()
	}
}

// Events is Subscribe as a channel (buffered; events are dropped when it is
// full rather than blocking the conversions). Call the returned func to stop.
func (a *App) Events(buffer int) (<-chan Event, func()) {
	ch := make(chan Event, max(1, buffer))
	var once sync.Once
	var mu sync.Mutex
	closed := false
	stop := a.Subscribe(func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		if closed {
			return
		}
		select {
		case ch <- e:
		default:
		}
	})
	return ch, func() {
		once.Do(func() {
			stop()
			mu.Lock()
			closed = true
			close(ch)
			mu.Unlock()
		})
	}
}

func (a *App) emit(e Event) {
	a.subMu.RLock()
	fns := make([]func(Event), 0, len(a.subs))
	for _, f := range a.subs {
		fns = append(fns, f)
	}
	a.subMu.RUnlock()
	for _, f := range fns {
		func() {
			defer func() {
				if r := recover(); r != nil {
					a.log.Warn("event subscriber panicked", "panic", r)
				}
			}()
			f(e)
		}()
	}
}

func (a *App) onJob(e jobs.Event) {
	s := e.Job.Snapshot()
	if !e.Finished {
		a.emit(Event{Kind: JobUpdated, Job: &s})
		return
	}
	a.mu.Lock()
	entry := history.From(e.Job)
	entry.WatchRuleID = a.jobRules[s.ID]
	delete(a.jobRules, s.ID)
	if s.State == jobs.JobCompleted {
		a.store.Settings.SuccessfulConversions++
	}
	recorded := a.store.Settings.KeepHistory
	err := a.store.AddHistory(entry)
	a.mu.Unlock()
	if err != nil {
		a.log.Warn("could not save the history", "error", err)
	}
	a.emit(Event{Kind: JobFinished, Job: &s})
	if recorded {
		a.emit(Event{Kind: HistoryChanged})
	}
}

// Platform is the OS services the app was made with.
func (a *App) Platform() platform.Services { return a.platform }

// Catalog is the engine catalog.
func (a *App) Catalog() *convert.Catalog { return a.catalog }

// Queue is the job queue (for callers that run jobs of their own).
func (a *App) Queue() *jobs.Queue { return a.queue }

// DataDir is the folder of the user's data.
func (a *App) DataDir() string { return a.store.Dir() }

// Text is the user-facing text for key in the chosen language.
func (a *App) Text(key string, args ...any) string { return i18n.Text(key, args...) }

// Texts is the localizer in use.
func (a *App) Texts() *i18n.Localizer { return i18n.Default() }

// ---- settings ----

// Settings returns a copy of the settings.
func (a *App) Settings() *settings.AppSettings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.store.Settings.Clone()
}

// SaveSettings replaces and saves the settings, then applies them: the
// language, the engine order and the watch folders.
func (a *App) SaveSettings(s *settings.AppSettings) error {
	if s == nil {
		return errors.New("no settings")
	}
	a.mu.Lock()
	old := a.store.Settings
	a.store.Settings = s.Clone()
	err := a.store.SaveSettings()
	if err != nil {
		a.store.Settings = old
	}
	a.mu.Unlock()
	if err != nil {
		return err
	}
	a.catalog.SetPriority(s.EnginePriority)
	i18n.SetLanguage(s.Language)
	a.restartWatchersIfRunning()
	a.emit(Event{Kind: SettingsChanged})
	return nil
}

// ---- presets ----

// Presets returns copies of every preset, in library order.
func (a *App) Presets() []*presets.Preset {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]*presets.Preset, len(a.store.Presets))
	for i, p := range a.store.Presets {
		out[i] = p.Clone()
	}
	return out
}

// Preset returns a copy of the preset id, or nil.
func (a *App) Preset(id string) *presets.Preset {
	a.mu.Lock()
	defer a.mu.Unlock()
	if p := a.store.FindPreset(id); p != nil {
		return p.Clone()
	}
	return nil
}

// FindPreset finds a conversion preset by id or shown name (conversions
// before groups and tools of the same name).
func (a *App) FindPreset(idOrName string) *presets.Preset {
	ps := a.Presets()
	texts := a.Texts()
	match := func(p *presets.Preset) bool {
		return strings.EqualFold(p.ID, idOrName) || strings.EqualFold(texts.PresetName(p), idOrName)
	}
	for _, conversionsOnly := range []bool{true, false} {
		for _, p := range ps {
			if strings.EqualFold(p.ID, idOrName) && (!conversionsOnly || !p.IsGroup() && !p.IsTool()) {
				return p
			}
		}
		for _, p := range ps {
			if match(p) && (!conversionsOnly || !p.IsGroup() && !p.IsTool()) {
				return p
			}
		}
	}
	return nil
}

// SavePreset adds the preset, or replaces the one with its id.
func (a *App) SavePreset(p *presets.Preset) error {
	if p == nil {
		return errors.New("no preset")
	}
	if p.ID == "" {
		p.ID = presets.NewID()
	}
	return a.library(func(s *settings.Store) error {
		c := p.Clone()
		if i := slices.IndexFunc(s.Presets, func(x *presets.Preset) bool { return x.ID == p.ID }); i >= 0 {
			s.Presets[i] = c
		} else {
			s.Presets = append(s.Presets, c)
		}
		return nil
	})
}

// DeletePreset removes a preset (and its slices on the donuts).
func (a *App) DeletePreset(id string) error {
	return a.library(func(s *settings.Store) error {
		n := len(s.Presets)
		s.Presets = slices.DeleteFunc(s.Presets, func(x *presets.Preset) bool { return x.ID == id })
		if len(s.Presets) == n {
			return fmt.Errorf("no preset %q", id)
		}
		return nil
	})
}

// ReorderPresets puts the library in the order of ids (missing ones keep
// their place after them).
func (a *App) ReorderPresets(ids []string) error {
	return a.library(func(s *settings.Store) error {
		var out []*presets.Preset
		for _, id := range ids {
			if p := s.FindPreset(id); p != nil && !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
		for _, p := range s.Presets {
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
		s.Presets = out
		return nil
	})
}

// BuiltInPresets are fresh copies of the factory presets, and PlacePresets
// those offered by "Add" (email, Discord, …).
func (a *App) BuiltInPresets() []*presets.Preset { return presets.BuiltIn() }
func (a *App) PlacePresets() []*presets.Preset   { return presets.PlacePresets() }

// ResetLibrary replaces presets and profiles with the factory defaults.
func (a *App) ResetLibrary() error {
	return a.library(func(s *settings.Store) error {
		s.Presets, s.Profiles = presets.BuiltIn(), profiles.BuiltIn()
		return nil
	})
}

// ExportLibrary writes presets and profiles to a bundle (without PDF passwords).
func (a *App) ExportLibrary(path string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.store.Export(path)
}

// ImportLibrary reads a bundle; it returns how many presets and profiles it took.
func (a *App) ImportLibrary(path string) (int, error) {
	a.mu.Lock()
	n, err := a.store.Import(path)
	a.mu.Unlock()
	if err == nil {
		a.emit(Event{Kind: LibraryChanged})
	}
	return n, err
}

// library changes presets/profiles under the lock and saves them.
func (a *App) library(change func(*settings.Store) error) error {
	a.mu.Lock()
	err := change(a.store)
	if err == nil {
		err = a.store.SaveLibrary()
	}
	a.mu.Unlock()
	if err == nil {
		a.emit(Event{Kind: LibraryChanged})
	}
	return err
}

// ---- profiles ----

// Profiles returns copies of the donut profiles, in order.
func (a *App) Profiles() []*profiles.Profile {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]*profiles.Profile, len(a.store.Profiles))
	for i, p := range a.store.Profiles {
		out[i] = p.Clone()
	}
	return out
}

// ProfileFor is the donut that opens for the files (a copy), or nil.
func (a *App) ProfileFor(paths []string) *profiles.Profile {
	if p := profiles.Select(a.Profiles(), paths); p != nil {
		return p
	}
	return nil
}

// SaveProfile adds the profile, or replaces the one with its id.
func (a *App) SaveProfile(p *profiles.Profile) error {
	if p == nil {
		return errors.New("no profile")
	}
	if p.ID == "" {
		p.ID = profiles.New("").ID
	}
	return a.library(func(s *settings.Store) error {
		c := p.Clone()
		if i := slices.IndexFunc(s.Profiles, func(x *profiles.Profile) bool { return x.ID == p.ID }); i >= 0 {
			s.Profiles[i] = c
		} else {
			s.Profiles = append(s.Profiles, c)
		}
		return nil
	})
}

// DeleteProfile removes a profile (a mixed one comes back when none is left).
func (a *App) DeleteProfile(id string) error {
	return a.library(func(s *settings.Store) error {
		s.Profiles = slices.DeleteFunc(s.Profiles, func(x *profiles.Profile) bool { return x.ID == id })
		return nil
	})
}

// MoveProfile moves a profile past its neighbour of the same kind (direction -1 up, +1 down).
func (a *App) MoveProfile(id string, direction int) (bool, error) {
	moved := false
	err := a.library(func(s *settings.Store) error {
		p := s.FindProfile(id)
		if p == nil {
			return fmt.Errorf("no profile %q", id)
		}
		s.Profiles, _, _, moved = profiles.Move(s.Profiles, p, direction, nil)
		return nil
	})
	return moved, err
}

// CanResetProfile tells whether the profile is a built-in donut that can be put back.
func (a *App) CanResetProfile(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.store.FindProfile(id)
	return p != nil && a.store.CanResetProfile(p)
}

// ResetProfile puts a built-in donut back as a new library has it.
func (a *App) ResetProfile(id string) (bool, error) {
	a.mu.Lock()
	p := a.store.FindProfile(id)
	if p == nil {
		a.mu.Unlock()
		return false, fmt.Errorf("no profile %q", id)
	}
	ok, err := a.store.ResetProfile(p)
	a.mu.Unlock()
	if ok && err == nil {
		a.emit(Event{Kind: LibraryChanged})
	}
	return ok, err
}

// ---- engines and routes ----

// Engine is one conversion engine as the UI lists it.
type Engine struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
	ReasonKey string `json:"reasonKey,omitempty"`
	Detail    string `json:"detail,omitempty"`
	Version   string `json:"version,omitempty"`
	Edges     int    `json:"edges"`
}

// Engines lists the engines in priority order with their status.
func (a *App) Engines() []Engine {
	var out []Engine
	for _, c := range a.catalog.Ordered() {
		s := a.catalog.StatusOf(c)
		out = append(out, Engine{ID: c.ID(), Name: c.Name(), Available: s.Available, ReasonKey: s.ReasonKey, Detail: s.Detail, Version: s.Version, Edges: len(c.Edges())})
	}
	return out
}

// RefreshEngines checks every engine again (after installing one).
func (a *App) RefreshEngines() {
	a.catalog.Refresh()
	a.emit(Event{Kind: EnginesChanged})
}

// Planner plans over the available engines.
func (a *App) Planner() *convert.Planner { return a.catalog.Planner() }

// TargetsFor lists the formats every one of the files converts to with the
// engines installed now (the formats of a mixed drop all must reach).
func (a *App) TargetsFor(paths []string) []string {
	planner := a.Planner()
	var out []string
	first := true
	seen := map[string]bool{}
	for _, p := range paths {
		f := formats.Detect(p)
		if f == nil || seen[f.ID] {
			continue
		}
		seen[f.ID] = true
		t := planner.Targets(f.ID)
		if first {
			out, first = t, false
		} else {
			out = slices.DeleteFunc(out, func(x string) bool { return !slices.Contains(t, x) })
		}
	}
	return out
}

// Slice is a preset on the donut for the dropped files.
type Slice struct {
	Preset *presets.Preset `json:"preset"`
	// Name is the name on the donut.
	Name string `json:"name"`
	// Usable: at least one of the files converts with it (groups and tools: always).
	Usable bool `json:"usable"`
}

// Donut is the donut for the files: their profile with both faces.
type Donut struct {
	Profile *profiles.Profile `json:"profile"`
	Front   []Slice           `json:"front"`
	Back    []Slice           `json:"back"`
}

// DonutFor is the donut the files open, with each slice's availability.
func (a *App) DonutFor(paths []string) *Donut {
	prof := a.ProfileFor(paths)
	if prof == nil {
		return nil
	}
	planner := a.Planner()
	texts := a.Texts()
	slices := func(ids []string) []Slice {
		var out []Slice
		for _, id := range ids {
			p := a.Preset(id)
			if p == nil {
				continue
			}
			out = append(out, Slice{Preset: p, Name: texts.DonutName(p), Usable: a.usable(planner, p, paths)})
		}
		return out
	}
	return &Donut{Profile: prof, Front: slices(prof.PresetIDs), Back: slices(prof.BackPresetIDs)}
}

func (a *App) usable(planner *convert.Planner, p *presets.Preset, paths []string) bool {
	if p.IsGroup() || p.IsTool() || p.PacksIntoOneArchive() {
		return true
	}
	for _, path := range paths {
		f := formats.Detect(path)
		if f == nil {
			continue
		}
		target := p.TargetFormat
		if target == presets.SameAsSource {
			target = f.ID
		}
		o := convert.Options{Frames: p.SavesFramesOf(f.ID), MotionPhoto: p.ExtractsMotionOf(f.ID), LivePhoto: p.MakesLivePhotoOf(f.ID), Ocr: p.RecognizesTextOf(f.ID)}
		if planner.Plan(f.ID, target, o) != nil {
			return true
		}
		// Joined media, merged pictures and PDFs take other routes; offer them for their own kind.
		if p.JoinsIntoOneMedia() && (formats.CategoryOf(f.ID) == formats.Video || formats.CategoryOf(f.ID) == formats.Audio) {
			return true
		}
	}
	return false
}

// ---- converting ----

// Convert converts the files with the conversion preset presetID (by id or
// shown name). The app-wide output settings apply; the job runs in the background.
func (a *App) Convert(paths []string, presetID string) (*jobs.Job, error) {
	p := a.FindPreset(presetID)
	if p == nil {
		return nil, fmt.Errorf("no preset %q", presetID)
	}
	if p.IsGroup() || p.IsTool() {
		return nil, fmt.Errorf("%q is not a conversion", presetID)
	}
	return a.ConvertWith(paths, p, a.Texts().PresetName(p), nil), nil
}

// ConvertTo converts the files to a format (id or extension) with default options.
func (a *App) ConvertTo(paths []string, format string) (*jobs.Job, error) {
	f := FindFormat(format)
	if f == nil {
		return nil, fmt.Errorf("unknown format %q", format)
	}
	if !f.Writable() {
		return nil, fmt.Errorf("Filee reads %s but can't write it", f.Name)
	}
	p := presets.New("", f.ID)
	return a.ConvertWith(paths, p, a.Texts().PresetName(p), nil), nil
}

// ConvertWith converts the files with any preset (a tool's conversion, a
// preset changed for one run); fetcher may save sources first (web drops).
func (a *App) ConvertWith(paths []string, p *presets.Preset, name string, fetcher jobs.SourceFetcher) *jobs.Job {
	a.mu.Lock()
	p = a.store.Settings.WithDefaultOutput(p)
	a.mu.Unlock()
	if f, ok := fetcher.(*web.Fetcher); ok {
		p = f.Apply(p)
	}
	j := jobs.NewJob(platform.ResolveShortcuts(a.platform, paths), p, name, fetcher)
	a.track(j, "")
	a.queue.Submit(j)
	return j
}

// BrowserDrop turns a browser drag into a drop whose placeholders stand for
// the files until their job saves them; pass the fetcher to ConvertWith.
func (a *App) BrowserDrop(content *web.DragContent) (*web.Drop, *web.Fetcher) {
	items := web.DragFiles(content)
	if len(items) == 0 {
		return nil, nil
	}
	drop := web.NewDrop(items, "")
	s := a.Settings()
	folder := s.BrowserDrops.Folder
	if strings.TrimSpace(folder) == "" {
		folder = a.platform.DownloadsFolder()
	}
	return drop, drop.Fetcher(web.DropOptions{Folder: folder, SaveOnlyConverted: s.BrowserDrops.SaveOnlyConverted}, a.dl)
}

func (a *App) track(j *jobs.Job, ruleID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if ruleID != "" {
		a.jobRules[j.ID()] = ruleID
	}
	a.jobList = append(a.jobList, j)
	// Forget the oldest finished jobs beyond the limit.
	finished := 0
	for _, x := range a.jobList {
		if x.State().Final() {
			finished++
		}
	}
	for i := 0; finished > a.keepJobs && i < len(a.jobList); {
		if a.jobList[i].State().Final() {
			a.jobList = slices.Delete(a.jobList, i, i+1)
			finished--
			continue
		}
		i++
	}
}

// Jobs returns snapshots of the running and recent jobs, oldest first.
func (a *App) Jobs() []jobs.Snapshot {
	a.mu.Lock()
	list := slices.Clone(a.jobList)
	a.mu.Unlock()
	out := make([]jobs.Snapshot, len(list))
	for i, j := range list {
		out[i] = j.Snapshot()
	}
	return out
}

// Job returns the job id, or nil.
func (a *App) Job(id string) *jobs.Job {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, j := range a.jobList {
		if j.ID() == id {
			return j
		}
	}
	return nil
}

// Cancel cancels the job id; false when there is none.
func (a *App) Cancel(id string) bool {
	if j := a.Job(id); j != nil {
		j.Cancel()
		return true
	}
	return false
}

// Busy reports whether a job waits or runs.
func (a *App) Busy() bool { return a.queue.Busy() }

// FindFormat finds a format by id ("jpg") or extension ("jpeg", ".jpg").
func FindFormat(text string) *formats.Format {
	key := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(text), "."))
	if f := formats.ByID(key); f != nil {
		return f
	}
	return formats.ByExtension(key)
}

// ---- history ----

// History returns the finished conversions, newest first (copies).
func (a *App) History() []history.Entry {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]history.Entry, len(a.store.History))
	for i, e := range a.store.History {
		out[i] = *e
	}
	return out
}

// ClearHistory empties the history.
func (a *App) ClearHistory() error {
	a.mu.Lock()
	err := a.store.ClearHistory()
	a.mu.Unlock()
	a.emit(Event{Kind: HistoryChanged})
	return err
}

// Undo moves the results of the history entry of jobID to the Trash; the
// refusal says why it stopped (nil when it worked).
func (a *App) Undo(jobID string) (*platform.TrashRefusal, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, e := range a.store.History {
		if e.JobID != jobID {
			continue
		}
		if !e.CanUndo() {
			return &platform.TrashRefusal{ReasonKey: "trash.changed"}, nil
		}
		r := e.Undo(a.platform.MoveToTrash)
		err := a.store.SaveHistory()
		go a.emit(Event{Kind: HistoryChanged})
		return r, err
	}
	return nil, fmt.Errorf("no history entry %q", jobID)
}

// Retry converts the files that failed in the history entry of jobID again,
// with the preset as that job ran it.
func (a *App) Retry(jobID string) (*jobs.Job, error) {
	a.mu.Lock()
	var entry *history.Entry
	for _, e := range a.store.History {
		if e.JobID == jobID {
			entry = e
		}
	}
	a.mu.Unlock()
	if entry == nil || len(entry.Failures) == 0 {
		return nil, fmt.Errorf("nothing to retry in %q", jobID)
	}
	p := entry.Preset
	if p == nil {
		if p = a.Preset(entry.PresetID); p == nil {
			return nil, fmt.Errorf("the preset of %q is gone", jobID)
		}
	} else if live := a.Preset(entry.PresetID); live != nil && live.Pdf.Password != "" && p.Pdf.PasswordLeftOut {
		// The password isn't kept in the history: take it from the preset again.
		p = p.Clone()
		p.Pdf.Password, p.Pdf.PasswordLeftOut = live.Pdf.Password, false
	}
	var files []string
	for _, f := range entry.Failures {
		files = append(files, f.Source)
	}
	j := jobs.NewJob(files, p, entry.PresetName, nil)
	a.track(j, entry.WatchRuleID)
	a.queue.Submit(j)
	return j, nil
}

// RecordHistory adds a result made outside the job queue (the crop tool, the
// PDF page editor) to the history.
func (a *App) RecordHistory(e *history.Entry) error {
	if e == nil {
		return errors.New("no entry")
	}
	a.mu.Lock()
	recorded := a.store.Settings.KeepHistory
	a.store.Settings.SuccessfulConversions++
	err := a.store.AddHistory(e)
	a.mu.Unlock()
	if recorded {
		a.emit(Event{Kind: HistoryChanged})
	}
	return err
}
