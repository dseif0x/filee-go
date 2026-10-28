package watch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/jobs"
	"github.com/dseif0x/filee-go/internal/platform"
)

var windows = runtime.GOOS == "windows"

// ConvertFunc converts a batch of complete files and returns the finished job
// (nil when it could not start).
type ConvertFunc func(ctx context.Context, files []string) *jobs.Job

// Options of a watcher; zero values are the defaults.
type Options struct {
	Log *slog.Logger
	// Settle is how long a file must stay unchanged (DefaultSettleTime when 0).
	Settle time.Duration
	// Now is the clock (tests use a fake one).
	Now func() time.Time
	// Converted is called after each converted batch (on a background goroutine).
	Converted func(*jobs.Job)
}

var temporaryExtensions = []string{".tmp", ".temp", ".part", ".partial", ".crdownload", ".download", ".opdownload", ".!ut", ".filepart"}
var systemFiles = []string{"desktop.ini", "thumbs.db"}

// Watcher converts the files that land in one watched folder. A file is
// converted only once it is complete: it must keep the same size and time
// for the settle time and open without sharing (Windows; elsewhere only the
// wait and Finder's copy-in-progress date count). Temporary files, the output
// folder and the originals folder are never picked up, so converted files
// can't loop back in.
type Watcher struct {
	rule    *Rule
	convert ConvertFunc
	o       Options
	log     *slog.Logger

	mu      sync.Mutex
	pending map[string]candidate
	handled map[string]bool
	results *generated

	busy   chan struct{}
	ctx    context.Context
	cancel context.CancelFunc
	fs     *fsnotify.Watcher
	wg     sync.WaitGroup
	err    string
}

type candidate struct {
	path        string
	exists      bool
	length      int64
	modified    time.Time
	stableSince time.Time
}

func candidateOf(path string, now time.Time) candidate {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return candidate{path: path, stableSince: now}
	}
	return candidate{path: path, exists: true, length: fi.Size(), modified: fi.ModTime(), stableSince: now}
}

func (c candidate) refresh(now time.Time) candidate {
	f := candidateOf(c.path, now)
	if f.exists && f.length == c.length && f.modified.Equal(c.modified) {
		return c
	}
	return f
}

func (c candidate) key() string {
	return fmt.Sprintf("%s|%d|%d", platform.PathKey(c.path), c.length, c.modified.UnixNano())
}

// New makes a watcher for a copy of rule.
func New(rule *Rule, convert ConvertFunc, o Options) *Watcher {
	if o.Log == nil {
		o.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if o.Settle <= 0 {
		o.Settle = DefaultSettleTime
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &Watcher{rule: rule.Clone(), convert: convert, o: o, log: o.Log, pending: map[string]candidate{}, handled: map[string]bool{},
		busy: make(chan struct{}, 1), ctx: ctx, cancel: cancel}
	w.results = &generated{rule: w.rule, entries: map[string]generatedEntry{}}
	if err := w.results.load(); err != nil {
		w.log.Warn("could not load the generated results of a watch folder", "folder", w.rule.Folder, "error", err)
	}
	return w
}

// Rule is the watcher's copy of its rule.
func (w *Watcher) Rule() *Rule { return w.rule }

// Error says why the folder is not being watched, or "".
func (w *Watcher) Error() string { return w.err }

// Start starts watching. With MoveToOriginals the files already in the
// folder are converted too (they arrived while nobody was watching); with
// Keep only new files are, because the old ones were converted before.
func (w *Watcher) Start() error {
	if fi, err := os.Stat(w.rule.Folder); err != nil || !fi.IsDir() {
		w.err = "Folder not found: " + w.rule.Folder
		return errors.New(w.err)
	}
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		w.err = err.Error()
		return err
	}
	w.fs = fs
	if err := w.addFolder(w.rule.Folder); err != nil {
		_ = fs.Close()
		w.err = err.Error()
		return err
	}
	w.wg.Add(2)
	go w.events()
	go w.tick()
	if w.rule.Originals == MoveToOriginals {
		w.scanExisting()
	}
	w.err = ""
	return nil
}

// addFolder watches folder and, with subfolders, every folder below it
// (fsnotify watches one level).
func (w *Watcher) addFolder(folder string) error {
	if err := w.fs.Add(folder); err != nil {
		return err
	}
	if !w.rule.IncludeSubfolders {
		return nil
	}
	return filepath.WalkDir(folder, func(p string, d os.DirEntry, err error) error {
		if err != nil || p == folder || !d.IsDir() {
			return nil
		}
		// The output and originals folders are never looked at.
		if w.excludedFolder(p) {
			return filepath.SkipDir
		}
		_ = w.fs.Add(p)
		return nil
	})
}

func (w *Watcher) excludedFolder(p string) bool {
	out := w.rule.ResolvedOutputFolder()
	return (!sameFolder(out, w.rule.Folder) && platform.SamePath(p, out)) || platform.SamePath(p, w.rule.OriginalsFolder())
}

func (w *Watcher) events() {
	defer w.wg.Done()
	for {
		select {
		case <-w.ctx.Done():
			return
		case ev, ok := <-w.fs.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename|fsnotify.Chmod) == 0 {
				continue
			}
			if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
				// A folder copied in: watch it and take what is already in it.
				if w.rule.IncludeSubfolders && ev.Op&fsnotify.Create != 0 && !w.excludedFolder(ev.Name) {
					_ = w.addFolder(ev.Name)
					w.scan(ev.Name)
				}
				continue
			}
			w.Notice(ev.Name)
		case err, ok := <-w.fs.Errors:
			if !ok {
				return
			}
			// The change buffer overflowed (thousands of files at once): look at the folder itself.
			w.log.Warn("watching missed changes; rescanning", "folder", w.rule.Folder, "error", err)
			w.scanExisting()
		}
	}
}

func (w *Watcher) tick() {
	defer w.wg.Done()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-t.C:
			w.ProcessPending()
		}
	}
}

// Notice registers a file that appeared or changed; it is converted once it has settled.
func (w *Watcher) Notice(path string) {
	if ShouldIgnore(path, w.rule) {
		return
	}
	now := w.o.Now()
	w.mu.Lock()
	defer w.mu.Unlock()
	k := platform.PathKey(path)
	if old, ok := w.pending[k]; ok {
		w.pending[k] = old.refresh(now)
	} else {
		w.pending[k] = candidateOf(path, now)
	}
}

// ProcessPending converts the pending files that are complete; the timer
// calls it every second, tests directly. It returns how many files were
// handed to the conversion.
func (w *Watcher) ProcessPending() int {
	select {
	case w.busy <- struct{}{}:
	default:
		return 0 // the previous batch is still converting; new files wait for the next tick
	}
	defer func() { <-w.busy }()

	now := w.o.Now()
	var ready []string
	w.mu.Lock()
	for k, c := range w.pending {
		cur := c.refresh(now)
		if !cur.exists || w.results.contains(cur.path) {
			delete(w.pending, k)
			continue
		}
		w.pending[k] = cur
		if now.Sub(cur.stableSince) < w.o.Settle || !canOpenExclusively(cur.path) || beingCopiedByFinder(cur.path) {
			continue
		}
		delete(w.pending, k)
		// A file that was converted before (same size and time) is not converted again.
		if key := cur.key(); !w.handled[key] {
			w.handled[key] = true
			ready = append(ready, cur.path)
		}
	}
	w.mu.Unlock()
	if len(ready) == 0 || w.ctx.Err() != nil {
		return 0
	}
	slices.Sort(ready)
	w.log.Info("watch folder: converting", "folder", w.rule.Folder, "count", len(ready))
	j := w.safeConvert(ready)
	if j == nil {
		return len(ready)
	}
	// A shared input/output folder can't be excluded wholesale: remember
	// exactly what this batch wrote before the next batch runs.
	changed := false
	shared := sameFolder(w.rule.ResolvedOutputFolder(), w.rule.Folder) || !platform.IsInside(w.rule.ResolvedOutputFolder(), w.rule.Folder)
	for _, out := range uniquePaths(j.Outputs()) {
		if shared {
			changed = w.results.remember(out) || changed
		}
		if fi, err := os.Stat(out); err == nil && !fi.IsDir() {
			w.mu.Lock()
			w.handled[candidateOf(out, now).key()] = true
			w.mu.Unlock()
		}
	}
	if changed {
		if err := w.results.save(); err != nil {
			w.log.Warn("could not save the generated results of a watch folder", "folder", w.rule.Folder, "error", err)
		}
	}
	if w.rule.Originals == MoveToOriginals {
		for _, f := range j.Files() {
			if f.State == jobs.FileDone {
				MoveOriginal(w.rule, f.SourcePath, w.log)
			}
		}
	}
	if w.o.Converted != nil {
		w.o.Converted(j)
	}
	return len(ready)
}

func (w *Watcher) safeConvert(files []string) (j *jobs.Job) {
	defer func() {
		if r := recover(); r != nil {
			w.log.Error("watch folder failed", "folder", w.rule.Folder, "panic", r)
			j = nil
		}
	}()
	return w.convert(w.ctx, files)
}

func uniquePaths(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range in {
		if k := platform.PathKey(p); !seen[k] {
			seen[k] = true
			out = append(out, p)
		}
	}
	return out
}

func (w *Watcher) scanExisting() { w.scan(w.rule.Folder) }

func (w *Watcher) scan(folder string) {
	err := filepath.WalkDir(folder, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != folder && (!w.rule.IncludeSubfolders || w.excludedFolder(p)) {
				return filepath.SkipDir
			}
			return nil
		}
		w.Notice(p)
		return nil
	})
	if err != nil {
		w.log.Warn("could not list a watch folder", "folder", folder, "error", err)
	}
}

// Close stops watching and lets a running batch finish moving its originals.
func (w *Watcher) Close() {
	w.cancel()
	if w.fs != nil {
		_ = w.fs.Close()
	}
	w.wg.Wait()
	w.busy <- struct{}{}
	<-w.busy
}

// ShouldIgnore is true for files a watch folder never converts: temporary,
// hidden or system files, unknown formats, anything inside the output or
// originals folder, and names the rule's Include leaves out.
func ShouldIgnore(path string, r *Rule) bool {
	name := filepath.Base(path)
	lower := strings.ToLower(name)
	if name == "" || strings.HasPrefix(name, "~$") || strings.HasPrefix(name, ".") || slices.Contains(systemFiles, lower) ||
		slices.Contains(temporaryExtensions, strings.ToLower(filepath.Ext(name))) {
		return true
	}
	// Exclude a dedicated output subtree, but never the whole watch root when
	// output is the same folder or one of its parents (issue #234).
	out := r.ResolvedOutputFolder()
	if !sameFolder(out, r.Folder) && platform.IsInside(out, r.Folder) && platform.IsInside(path, out) || platform.IsInside(path, r.OriginalsFolder()) {
		return true
	}
	if !r.IncludeSubfolders && !sameFolder(filepath.Dir(path), r.Folder) {
		return true
	}
	if formats.Detect(path) == nil || !FilterOf(r.Include).Matches(name) {
		return true
	}
	fi, err := os.Stat(path)
	if err != nil {
		return true // gone already, or not ours to read
	}
	return fi.IsDir() || isHiddenOrSystem(path, fi)
}

// MoveOriginal moves a converted source into the rule's originals folder
// (keeping its subfolder), numbering it when the name is taken. A file
// outside the watched folder stays where it is.
func MoveOriginal(r *Rule, source string, log *slog.Logger) {
	if !platform.IsInside(source, r.Folder) || platform.IsInside(source, r.OriginalsFolder()) {
		return
	}
	rel, err := filepath.Rel(r.Folder, source)
	if err != nil {
		return
	}
	target := filepath.Join(r.OriginalsFolder(), rel)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		log.Warn("could not move a file to the originals folder", "file", source, "error", err)
		return
	}
	ext := filepath.Ext(target)
	stem := strings.TrimSuffix(target, ext)
	for n := 2; exists(target); n++ {
		target = fmt.Sprintf("%s (%d)%s", stem, n, ext)
	}
	if err := os.Rename(source, target); err != nil {
		log.Warn("could not move a file to the originals folder", "file", source, "error", err)
	}
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func sameFolder(a, b string) bool {
	if a == "" {
		return false
	}
	return platform.SamePath(strings.TrimRight(a, `/\`), strings.TrimRight(b, `/\`))
}

// ---- results written back into the watched tree ----

// generated remembers results when a rule writes back into its input tree:
// without it, restarting an inbox watcher would convert yesterday's outputs
// again. Entries identify a file version, not just a name: replacing an
// output with a new incoming file makes it eligible again.
type generated struct {
	rule    *Rule
	entries map[string]generatedEntry
}

type generatedEntry struct {
	Path           string `json:"Path"`
	Length         int64  `json:"Length"`
	LastWriteTicks int64  `json:"LastWriteTicks"`
	IsDirectory    bool   `json:"IsDirectory,omitempty"`
}

func (g *generated) stateFile() string {
	sum := sha256.Sum256([]byte(g.rule.ID))
	return filepath.Join(g.rule.Folder, ".filee-watch-"+hex.EncodeToString(sum[:])[:16]+".json")
}

func (g *generated) load() error {
	b, err := os.ReadFile(g.stateFile())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var list []*generatedEntry
	if err := json.Unmarshal(b, &list); err != nil {
		return err
	}
	for _, e := range list {
		if e != nil && g.current(*e) {
			g.entries[platform.PathKey(e.Path)] = *e
		}
	}
	return nil
}

func (g *generated) contains(path string) bool {
	if e, ok := g.entries[platform.PathKey(path)]; ok && g.current(e) {
		return true
	}
	for _, e := range g.entries {
		if e.IsDirectory && platform.IsInside(path, e.Path) && g.current(e) {
			return true
		}
	}
	return false
}

func (g *generated) remember(path string) bool {
	if !platform.IsInside(path, g.rule.Folder) {
		return false
	}
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	if fi.IsDir() {
		g.entries[platform.PathKey(path)] = generatedEntry{Path: path, IsDirectory: true}
	} else {
		g.entries[platform.PathKey(path)] = generatedEntry{Path: path, Length: fi.Size(), LastWriteTicks: fi.ModTime().UnixNano()}
	}
	return true
}

func (g *generated) save() error {
	var list []generatedEntry
	for k, e := range g.entries {
		if !g.current(e) {
			delete(g.entries, k)
			continue
		}
		list = append(list, e)
	}
	slices.SortFunc(list, func(a, b generatedEntry) int { return strings.Compare(a.Path, b.Path) })
	b, err := json.Marshal(list)
	if err != nil {
		return err
	}
	tmp := g.stateFile() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, g.stateFile()); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	hide(g.stateFile())
	return nil
}

func (g *generated) current(e generatedEntry) bool {
	// A copied or edited state file can't exclude the root itself or files outside this rule.
	if strings.TrimSpace(e.Path) == "" || !platform.IsInside(e.Path, g.rule.Folder) {
		return false
	}
	fi, err := os.Stat(e.Path)
	if err != nil {
		return false
	}
	if e.IsDirectory {
		return fi.IsDir()
	}
	return !fi.IsDir() && fi.Size() == e.Length && fi.ModTime().UnixNano() == e.LastWriteTicks
}
