package jobs

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dseif0x/filee-go/internal/platform"
	"github.com/dseif0x/filee-go/internal/presets"
)

// ---- reservations (all under q.outMu) ----

func reserveKey(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	// Case-insensitively everywhere: a mount may be case-insensitive.
	return strings.ToLower(abs)
}

// reserve takes path; true if nobody held it.
func (q *Queue) reserve(path string) bool {
	k := reserveKey(path)
	if r, ok := q.reserved[k]; ok {
		r.holders++
		r.shared = true
		return false
	}
	q.reserved[k] = &reservation{path: path, holders: 1}
	return true
}

// unreserve lets go of path: free once nobody holds it.
func (q *Queue) unreserve(path string) {
	k := reserveKey(path)
	if r, ok := q.reserved[k]; ok {
		r.holders--
		if r.holders <= 0 {
			delete(q.reserved, k)
		}
	}
}

func (q *Queue) isReserved(path string) bool {
	_, ok := q.reserved[reserveKey(path)]
	return ok
}

func absDir(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	return filepath.Dir(abs)
}

func (q *Queue) hasReservationIn(folder string) bool {
	for _, r := range q.reserved {
		if platform.SamePath(absDir(r.path), folder) {
			return true
		}
	}
	return false
}

// removeIfEmpty removes a folder this queue made for outputs once nothing is
// in it and no running conversion is to write into it. Never a folder that
// was there before, and never one with anything inside.
func (q *Queue) removeIfEmpty(folder string) {
	folder, _ = filepath.Abs(folder)
	k := platform.PathKey(folder)
	if _, ok := q.made[k]; !ok || q.hasReservationIn(folder) {
		return
	}
	entries, err := os.ReadDir(folder)
	if err == nil && len(entries) == 0 {
		if os.Remove(folder) != nil {
			return
		}
	} else if err == nil {
		return
	}
	delete(q.made, k)
}

// ---- the final allocator ----

type allocation struct {
	path string
	// own: nothing was there before and no other conversion had it.
	own bool
}

// finalAllocator hands out final output paths by the preset's output rule.
type finalAllocator struct {
	q         *Queue
	rule      *presets.OutputRule
	tokens    presets.Tokens
	allocated []allocation
}

func (q *Queue) newFinalAllocator(j *Job, source string, index int, oneFile bool) *finalAllocator {
	rule := &j.preset.Output
	if oneFile {
		rule, _ = forOneFile(rule)
	}
	return &finalAllocator{q: q, rule: rule, tokens: presets.Tokens{SourcePath: source, PresetName: j.presetName, Index: index, Now: j.created}}
}

// forOneFile is the rule for a single output file: with
// SubfolderOnlyForSeveralFiles it is saved next to the source instead.
func forOneFile(rule *presets.OutputRule) (*presets.OutputRule, bool) {
	if !rule.SubfolderOnlyForSeveralFiles || rule.Location != presets.LocationSubfolder {
		return rule, false
	}
	single := *rule
	single.Renames = slices.Clone(rule.Renames)
	single.Location = presets.LocationSameFolder
	return &single, true
}

func (a *finalAllocator) Allocate(ext, suffix string) string {
	q := a.q
	// Reserved under the lock so two parallel files never pick the same name.
	q.outMu.Lock()
	defer q.outMu.Unlock()
	path := presets.Resolve(a.rule, a.tokens, ext, func(p string) bool { return exists(p) || q.isReserved(p) }, suffix)
	if path == "" {
		return ""
	}
	nothingThere := !exists(path)
	a.allocated = append(a.allocated, allocation{path, q.reserve(path) && nothingThere})
	folder := absDir(path)
	if !exists(folder) {
		if err := os.MkdirAll(folder, 0o755); err == nil {
			q.made[platform.PathKey(folder)] = folder
		}
	}
	// An empty extension asks for a folder (archive extraction): the converter creates it.
	return path
}

// handed is how many paths were handed out: an output and the picture folder that goes with it are two.
func (a *finalAllocator) handed() int {
	a.q.outMu.Lock()
	defer a.q.outMu.Unlock()
	return len(a.allocated)
}

// moved: the one output was moved to to (next to its source); its name goes with it.
func (a *finalAllocator) moved(from, to string) {
	q := a.q
	i := slices.IndexFunc(a.allocated, func(x allocation) bool { return strings.EqualFold(x.path, from) })
	if i < 0 {
		return
	}
	// Finished and in its place: nothing to undo there any more.
	a.allocated[i] = allocation{to, false}
	q.unreserve(from)
	q.reserve(to)
}

// release frees the names handed out but not in except (the outputs, which
// stay taken until the job ends).
func (a *finalAllocator) release(except []string) {
	q := a.q
	q.outMu.Lock()
	defer q.outMu.Unlock()
	for _, x := range a.allocated {
		if !slices.ContainsFunc(except, func(e string) bool { return strings.EqualFold(e, x.path) }) {
			q.unreserve(x.path)
		}
	}
	a.allocated = nil
}

// discard undoes a conversion that failed or was cancelled: deletes what it
// started writing at paths that were its alone (never a file it was to
// overwrite, nor one another conversion had too), frees every name so a retry
// gets the same one, and removes a folder it made if that left it empty.
func (a *finalAllocator) discard() {
	q := a.q
	q.outMu.Lock()
	defer q.outMu.Unlock()
	var folders []string
	for _, x := range a.allocated {
		if r, ok := q.reserved[reserveKey(x.path)]; x.own && ok && !r.shared {
			if err := os.RemoveAll(x.path); err != nil {
				q.log.Warn("could not delete the unfinished output", "output", x.path, "error", err)
			}
		}
		q.unreserve(x.path)
		if d := absDir(x.path); !slices.ContainsFunc(folders, func(f string) bool { return platform.SamePath(f, d) }) {
			folders = append(folders, d)
		}
	}
	for _, f := range folders {
		q.removeIfEmpty(f)
	}
	a.allocated = nil
}

// ---- next to the source ----

// nextToSource: with SubfolderOnlyForSeveralFiles, the one file a source
// became moves out of the subfolder next to the source, keeping its name (the
// conflict policy applies there), and the subfolder goes if that left it
// empty. A file that can't be moved stays in the subfolder.
func (q *Queue) nextToSource(j *Job, f *FileResult, index int, output string, a *finalAllocator) string {
	rule, changed := forOneFile(&j.preset.Output)
	if !changed {
		return output
	}
	if fi, err := os.Stat(output); err != nil || fi.IsDir() {
		return output
	}
	// Pandoc writes a document's pictures into "<name>_files" beside it: they stay together.
	stem := strings.TrimSuffix(filepath.Base(output), filepath.Ext(output))
	if fi, err := os.Stat(filepath.Join(filepath.Dir(output), stem+"_files")); err == nil && fi.IsDir() {
		return output
	}
	// The name was already made from the pattern and renames: it is kept as it is.
	rule.FileNamePattern = stem
	rule.Renames = nil

	q.outMu.Lock()
	defer q.outMu.Unlock()
	tokens := presets.Tokens{SourcePath: f.SourcePath, PresetName: j.presetName, Index: index, Now: j.created}
	target := presets.Resolve(rule, tokens, filepath.Ext(output), func(p string) bool {
		return !platform.SamePath(p, output) && (exists(p) || q.isReserved(p))
	}, "")
	if target == "" || platform.SamePath(target, output) {
		return output
	}
	// Over an existing file only when the policy says so: one that appeared since is never replaced.
	if err := moveFile(output, target, rule.Conflict == presets.ConflictOverwrite); err != nil {
		q.log.Warn("could not move an output next to its source", "output", output, "error", err)
		return output
	}
	a.moved(output, target)
	q.removeIfEmpty(filepath.Dir(output))
	return target
}

// moveFile renames from to to; across drives it copies and deletes. Without
// overwrite an existing target is an error.
func moveFile(from, to string, overwrite bool) error {
	if !overwrite && exists(to) {
		return fmt.Errorf("%s: %w", to, os.ErrExist)
	}
	if err := os.Rename(from, to); err == nil {
		return nil
	} else if _, statErr := os.Stat(from); statErr != nil {
		return err
	}
	// Another drive (or a rename the system refuses): copy, then delete.
	if err := copyFileStream(from, to); err != nil {
		_ = os.Remove(to)
		return err
	}
	return os.Remove(from)
}

func copyFileStream(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fi.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// ---- "Replace the original" ----

// spellings are extensions that only spell the same kind of file differently.
var spellings = [][]string{
	{".jpg", ".jpeg", ".jpe", ".jfif"}, {".tiff", ".tif"}, {".html", ".htm"}, {".md", ".markdown"}, {".yaml", ".yml"},
	{".mpg", ".mpeg"}, {".wav", ".wave"}, {".aiff", ".aif"}, {".ogg", ".oga"}, {".3gp", ".3gpp"}, {".m2ts", ".mts"},
	{".mov", ".qt"}, {".tex", ".latex"},
}

func sameKindOfFile(a, b string) bool {
	if strings.EqualFold(a, b) {
		return true
	}
	has := func(set []string, x string) bool {
		return slices.ContainsFunc(set, func(s string) bool { return strings.EqualFold(s, x) })
	}
	for _, s := range spellings {
		if has(s, a) && has(s, b) {
			return true
		}
	}
	return false
}

// replaceOriginal: the finished output takes the source's place and the
// source goes to the Recycle Bin / Trash; the path of the result is returned.
// The result is first moved next to the source under a temporary name, so a
// failure there leaves the source alone; then the source goes to the Trash;
// then the result is renamed. Anything that stops it keeps the source, with a
// note saying why. copiedSource: "Only if smaller" copied the source as the
// result, so the source stays where it is and the copy goes.
func (q *Queue) replaceOriginal(j *Job, f *FileResult, sourceFormat, output string, copiedSource bool) string {
	source := f.SourcePath
	if !j.preset.Output.ReplaceOriginal || j.fetcher != nil || detectID(output) != sourceFormat || platform.SamePath(output, source) {
		return output
	}
	if fi, err := os.Stat(output); err != nil || fi.IsDir() {
		return output
	}
	// One format covers different kinds of file (TXT: .bat, .py, .log), and
	// the result takes the original's name: only the same extension, or
	// another spelling of it, replaces.
	if !sameKindOfFile(filepath.Ext(output), filepath.Ext(source)) {
		return output
	}
	if copiedSource {
		if os.Remove(output) != nil {
			return output
		}
		j.edit(func() { f.NoticeKey = "notice.original_unchanged" })
		return source
	}
	kept := func(why platform.TrashRefusal, result string) string {
		q.log.Info("kept the original", "source", source, "reason", why.ReasonKey, "detail", why.Detail)
		j.edit(func() {
			f.NoticeKey = "notice.original_kept"
			f.NoticeReasonKey = why.ReasonKey
			f.NoticeDetail = why.Detail
		})
		return result
	}
	if q.opt.MoveToTrash == nil {
		return kept(platform.TrashRefusal{ReasonKey: "trash.no_recycle_bin"}, output)
	}

	abs, _ := filepath.Abs(source)
	staged := filepath.Join(filepath.Dir(abs), fmt.Sprintf(".%s.%s.filee-tmp", filepath.Base(source), newJobID()[:8]))
	if err := moveFile(output, staged, false); err != nil {
		q.log.Warn("could not move the result next to the original", "output", output, "error", err)
		return kept(platform.TrashRefusal{ReasonKey: "trash.cannot_place", Detail: err.Error()}, output)
	}

	refusal := q.opt.MoveToTrash(source)
	if refusal != nil {
		// The result goes back to where it was saved.
		if err := moveFile(staged, output, false); err != nil {
			q.log.Warn("could not move the result back", "staged", staged, "error", err)
			return kept(*refusal, staged)
		}
		return kept(*refusal, output)
	}
	j.edit(func() { f.OriginalInTrash = true })
	if err := moveFile(staged, source, false); err != nil {
		// The original is in the Trash already: the result goes back to the name it was saved under.
		q.log.Warn("could not rename the result to the original's name", "staged", staged, "error", err)
		j.edit(func() {
			f.NoticeKey = "notice.original_in_trash"
			f.NoticeReasonKey = "trash.cannot_place"
			f.NoticeDetail = err.Error()
		})
		if err := moveFile(staged, output, false); err != nil {
			return staged
		}
		return output
	}
	return source
}

// ---- "Save as ZIP" ----

// packZip packs the files one source became into one ZIP (named by the
// preset's output rule, so the conflict policy applies to it); empty when
// the rule skips an existing ZIP. A clash inside gets "name (2).ext", and a
// ZIP that can't be completed is deleted.
func (q *Queue) packZip(j *Job, f *FileResult, index int, files []string) ([]string, error) {
	a := q.newFinalAllocator(j, f.SourcePath, index, true)
	zipPath := a.Allocate("zip", "")
	if zipPath == "" {
		return nil, nil
	}
	err := func() error {
		out, err := os.Create(zipPath)
		if err != nil {
			return err
		}
		zw := zip.NewWriter(out)
		used := map[string]bool{}
		for _, path := range files {
			if err := j.ctx.Err(); err != nil {
				zw.Close()
				out.Close()
				return err
			}
			name := filepath.Base(path)
			ext := filepath.Ext(name)
			stem := strings.TrimSuffix(name, ext)
			for n := 2; used[strings.ToLower(name)]; n++ {
				name = fmt.Sprintf("%s (%d)%s", stem, n, ext)
			}
			used[strings.ToLower(name)] = true
			if err := addToZip(zw, path, name); err != nil {
				zw.Close()
				out.Close()
				return err
			}
		}
		if err := zw.Close(); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	}()
	if err != nil {
		_ = os.Remove(zipPath)
		a.discard()
		return nil, err
	}
	return []string{zipPath}, nil
}

func addToZip(zw *zip.Writer, path, name string) error {
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	h, err := zip.FileInfoHeader(fi)
	if err != nil {
		return err
	}
	h.Name = name
	h.Method = zip.Deflate
	w, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, in)
	return err
}

// ---- dates ----

// copyDates gives the outputs the creation and modification dates of the
// source ("Keep dates"). A failure keeps the files; only the dates are new.
func (q *Queue) copyDates(d fileDates, outputs []string) {
	for _, o := range outputs {
		if !exists(o) {
			continue
		}
		if err := setDates(o, d); err != nil {
			q.log.Warn("could not copy the source's dates", "output", o, "error", err)
		}
	}
}
