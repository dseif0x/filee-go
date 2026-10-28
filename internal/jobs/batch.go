package jobs

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/presets"
)

// picturesReadyShare is the part of a slideshow's (or a merge of pictures')
// progress that preparing its pictures takes; encoding (or merging) fills the rest.
const picturesReadyShare = 0.3

// markDone marks every file of the job that didn't fail as done.
func (q *Queue) markDone(j *Job, first *FileResult, output string) {
	j.edit(func() {
		first.Outputs = append(first.Outputs, output)
		for _, f := range j.files {
			if f.State != FileFailed {
				f.State = FileDone
				f.Progress = 1
			}
		}
	})
	q.notify(j)
}

func (q *Queue) skipAll(j *Job, files []*FileResult) {
	j.edit(func() {
		for _, f := range files {
			f.State = FileSkipped
			f.Progress = 1
		}
	})
	q.notify(j)
}

// runMerged converts every file to PDF in the work folder, then merges them into one output.
func (q *Queue) runMerged(j *Job, planner *convert.Planner, workDir string, merger convert.PdfMerger) {
	parts := make([]string, len(j.files))
	parallel(len(j.files), runtime.NumCPU(), func(i int) {
		f := j.files[i]
		if f.stopped() {
			return
		}
		// An empty PDF goes the long way, where it fails as empty instead of
		// breaking the whole merge; so does one whose text is recognized first.
		if detectID(f.SourcePath) == "pdf" && !isEmptyFile(f.SourcePath) && !j.preset.RecognizesTextOf("pdf") {
			// Merged as it is: its pages can't be turned or covered.
			if j.preset.Image.ReworksPixels() {
				q.failUnturned(j, f, "pdf → pdf")
			} else {
				parts[i] = f.SourcePath
			}
			return
		}
		if produced := q.runFile(j, f, i+1, planner, workDir, false, fileOptions{}); len(produced) > 0 {
			parts[i] = produced[0]
		}
	})
	var inputs []string
	for _, p := range parts {
		if p != "" {
			inputs = append(inputs, p)
		}
	}
	if len(inputs) == 0 || j.Cancelled() {
		return
	}
	first := j.files[0]
	a := q.newFinalAllocator(j, first.SourcePath, 1, true)
	output := a.Allocate("pdf", "")
	if output == "" {
		q.skipAll(j, j.files)
		return
	}
	err := safe(func() error {
		if err := merger.MergePdfs(j.ctx, inputs, output); err != nil {
			return err
		}
		// Once for the merged PDF, so its pages are numbered through.
		return q.finishPdfs(j.ctx, j.preset, []string{output})
	})
	if err != nil {
		a.discard()
		q.failGroup(j, j.files, err, len(inputs), output)
		return
	}
	q.markDone(j, first, output)
}

// failGroup marks every file of a merge that didn't fail before as failed (or
// cancelled) with err.
func (q *Queue) failGroup(j *Job, files []*FileResult, err error, count int, output string) {
	cancelled, key, detail := classify(j, err)
	if !cancelled {
		q.log.Warn("writing one file of several failed", "count", count, "output", output, "error", err)
	}
	j.edit(func() {
		for _, f := range files {
			if f.State == FileFailed {
				continue
			}
			if cancelled {
				f.State = FileCancelled
			} else {
				failLocked(f, key, detail)
			}
		}
	})
	q.notify(j)
}

// runMultiPageTiff converts every file to TIFF pages in the work folder (a
// TIFF is taken as it is), then writes them all as one multi-page TIFF named after the first file.
func (q *Queue) runMultiPageTiff(j *Job, planner *convert.Planner, workDir string, merger convert.TiffMerger) {
	parts := make([][]string, len(j.files))
	parallel(len(j.files), runtime.NumCPU(), func(i int) {
		f := j.files[i]
		if f.stopped() {
			return
		}
		// A TIFF joins as it is, unless it is to be turned or covered.
		if detectID(f.SourcePath) == "tiff" && !j.preset.Image.ReworksPixels() && !isEmptyFile(f.SourcePath) {
			parts[i] = []string{f.SourcePath}
			return
		}
		parts[i] = q.runFile(j, f, i+1, planner, workDir, false, fileOptions{intermediate: "tiff"})
	})
	var inputs []string
	for _, p := range parts {
		inputs = append(inputs, p...)
	}
	if len(inputs) == 0 || j.Cancelled() {
		return
	}
	first := j.files[0]
	a := q.newFinalAllocator(j, first.SourcePath, 1, true)
	output := a.Allocate("tiff", "")
	if output == "" {
		q.skipAll(j, j.files)
		return
	}
	if err := safe(func() error { return merger.MergeTiffs(j.ctx, inputs, output, &j.preset.Image) }); err != nil {
		a.discard()
		q.failGroup(j, j.files, err, len(inputs), output)
		return
	}
	q.markDone(j, first, output)
}

// runJoinedMedia: "Merge videos" / "Merge audio" put the files into one as
// they are; "Video from images" makes every picture a PNG in the work folder
// first (a PNG is taken as it is), then the pictures become one video. The
// output is named after the first file.
func (q *Queue) runJoinedMedia(j *Job, planner *convert.Planner, workDir string, joiner convert.MediaJoiner) {
	slideshow := j.preset.Media.Slideshow
	target := formats.ByID(j.preset.TargetFormat)
	// Before any picture is converted: too many fail at once. Files whose download failed don't count.
	pictures := 0
	for _, f := range j.files {
		if !f.stopped() {
			pictures++
		}
	}
	if slideshow && pictures > presets.MaxSlides {
		for _, f := range j.files {
			if !f.stopped() {
				q.failFile(j, f, "error.too_many_pictures", fmt.Sprintf("%d > %d", pictures, presets.MaxSlides))
			}
		}
		return
	}
	parts := make([][]string, len(j.files))
	parallel(len(j.files), runtime.NumCPU(), func(i int) {
		f := j.files[i]
		if f.stopped() {
			return
		}
		source := formats.Detect(f.SourcePath)
		switch {
		case source == nil:
			q.failFile(j, f, "error.unsupported_source", "."+formats.ExtensionOf(f.SourcePath))
			return
		case isEmptyFile(f.SourcePath):
			q.failFile(j, f, "error.empty_file", "")
			return
		}
		if slideshow {
			if source.ID == "png" && !j.preset.Image.ReworksPixels() {
				parts[i] = []string{f.SourcePath}
			} else {
				parts[i] = q.runFile(j, f, i+1, planner, workDir, false, fileOptions{intermediate: "png", ceiling: picturesReadyShare})
			}
			return
		}
		// A video's sound can join an audio file; a picture or a document joins nothing.
		if source.Category != formats.Video && !(source.Category == formats.Audio && target.Category == formats.Audio) {
			q.failFile(j, f, "error.no_route", source.ID+" → "+target.ID)
			return
		}
		parts[i] = []string{f.SourcePath}
	})
	var joined []*FileResult
	var inputs []string
	for i, f := range j.files {
		if len(parts[i]) > 0 && f.State != FileFailed {
			joined = append(joined, f)
		}
		inputs = append(inputs, parts[i]...)
	}
	if len(inputs) == 0 || len(joined) == 0 || j.Cancelled() {
		q.notify(j)
		return
	}
	q.writeJoined(j, joined, len(inputs), target.PrimaryExtension(), func(output string, progress convert.Progress) error {
		return joiner.JoinMedia(j.ctx, inputs, output, target.ID, j.preset, workDir, progress)
	})
}

// runMergedImages: "Merge images" puts the pictures, in order, into one image
// named after the first, in the first one's format for "same as source" (PNG
// when that can't be written). A picture the joiner doesn't read becomes a
// plain PNG first; the preset's options apply once, as the pictures merge.
func (q *Queue) runMergedImages(j *Job, planner *convert.Planner, workDir string, joiner convert.ImageJoiner) {
	plain := presets.New("", "png")
	plain.Pdf.RenderDpi = j.preset.Pdf.RenderDpi
	parts := make([]*convert.ImageInput, len(j.files))
	parallel(len(j.files), runtime.NumCPU(), func(i int) {
		f := j.files[i]
		if f.stopped() {
			return
		}
		source := formats.Detect(f.SourcePath)
		switch {
		case source == nil:
			q.failFile(j, f, "error.unsupported_source", "."+formats.ExtensionOf(f.SourcePath))
		case isEmptyFile(f.SourcePath):
			q.failFile(j, f, "error.empty_file", "")
		case source.Category != formats.Image && source.Category != formats.Vector:
			q.failFile(j, f, "error.images_only", "")
		case joiner.CanRead(source.ID):
			parts[i] = &convert.ImageInput{Path: f.SourcePath, Format: source.ID}
		default:
			if png := q.runFile(j, f, i+1, planner, workDir, false, fileOptions{intermediate: "png", ceiling: picturesReadyShare, preset: plain}); len(png) > 0 {
				parts[i] = &convert.ImageInput{Path: png[0], Format: "png"}
			}
		}
	})
	var joined []*FileResult
	var inputs []convert.ImageInput
	for i, f := range j.files {
		if parts[i] != nil && f.State != FileFailed {
			joined = append(joined, f)
		}
		if parts[i] != nil {
			inputs = append(inputs, *parts[i])
		}
	}
	if len(joined) == 0 || j.Cancelled() {
		q.notify(j)
		return
	}
	target := j.preset.TargetFormat
	if target == presets.SameAsSource {
		target = "png"
		if first := detectID(joined[0].SourcePath); first != "" && joiner.CanWrite(first) {
			target = first
		}
	} else if !joiner.CanWrite(target) {
		for _, f := range joined {
			q.failFile(j, f, "error.no_route", detectID(f.SourcePath)+" → "+target)
		}
		return
	}
	q.writeJoined(j, joined, len(inputs), formats.ByID(target).PrimaryExtension(), func(output string, progress convert.Progress) error {
		return joiner.JoinImages(j.ctx, inputs, output, target, &j.preset.Image, progress)
	})
}

// writeJoined writes the one file several sources become, named after the
// first of joined. Their progress moves together; a write that fails or is
// cancelled is undone.
func (q *Queue) writeJoined(j *Job, joined []*FileResult, count int, ext string, write func(output string, progress convert.Progress) error) {
	first := joined[0]
	a := q.newFinalAllocator(j, first.SourcePath, 1, true)
	output := a.Allocate(ext, "")
	if output == "" {
		q.skipAll(j, joined)
		return
	}
	floor := 1.0
	j.edit(func() {
		for _, f := range joined {
			f.State = FileRunning
			floor = min(floor, f.Progress)
		}
	})
	q.notify(j)
	progress := func(p float64) {
		v := floor + (1-floor)*min(1, max(0, p))
		moved := false
		j.edit(func() {
			// Progress only moves forward.
			if v > first.Progress {
				for _, f := range joined {
					f.Progress = v
				}
				moved = true
			}
		})
		if moved {
			q.notify(j)
		}
	}
	err := safe(func() error {
		if err := write(output, progress); err != nil {
			return err
		}
		// Joined media aren't fitted: one over the limit is refused (#553).
		return checkSizeLimit(j.preset, []string{output})
	})
	if err != nil {
		a.discard()
		q.failGroup(j, joined, err, count, output)
		return
	}
	j.edit(func() {
		first.Outputs = append(first.Outputs, output)
		for _, f := range joined {
			f.State = FileDone
			f.Progress = 1
		}
	})
	q.notify(j)
}

// runCombined packs every source file, unchanged, into one archive named after the first file.
func (q *Queue) runCombined(j *Job, workDir string, combiner convert.FileCombiner) {
	format := formats.ByID(j.preset.TargetFormat)
	var inputs []*FileResult
	for _, f := range j.files {
		if f.stopped() {
			continue
		}
		// Folders are packed with their content.
		if exists(f.SourcePath) {
			inputs = append(inputs, f)
		} else {
			q.failFile(j, f, "error.conversion_failed", "The file no longer exists.")
		}
	}
	if len(inputs) == 0 {
		q.notify(j)
		return
	}
	a := q.newFinalAllocator(j, inputs[0].SourcePath, 1, true)
	output := a.Allocate(format.PrimaryExtension(), "")
	if output == "" {
		q.skipAll(j, inputs)
		return
	}
	j.edit(func() {
		for _, f := range inputs {
			f.State = FileRunning
		}
	})
	q.notify(j)
	progress := func(p float64) {
		v := min(1, max(0, p))
		moved := false
		j.edit(func() {
			if v > inputs[0].Progress {
				for _, f := range inputs {
					f.Progress = v
				}
				moved = true
			}
		})
		if moved {
			q.notify(j)
		}
	}
	// A Shapefile goes into the archive with its companion files: the .shp alone is no map.
	var files []string
	seen := map[string]bool{}
	for _, f := range inputs {
		for _, file := range append([]string{f.SourcePath}, formats.Companions(f.SourcePath)...) {
			abs, _ := filepath.Abs(file)
			if k := reserveKey(abs); !seen[k] {
				seen[k] = true
				files = append(files, file)
			}
		}
	}
	err := safe(func() error {
		return combiner.Combine(j.ctx, files, output, format.ID, j.preset, workDir, progress)
	})
	if err != nil {
		a.discard()
		cancelled, _, _ := classify(j, err)
		if !cancelled {
			q.log.Warn("packing files failed", "count", len(inputs), "output", output, "error", err)
		}
		j.edit(func() {
			for _, f := range inputs {
				if cancelled {
					f.State = FileCancelled
				} else {
					failLocked(f, "error.conversion_failed", err.Error())
				}
			}
		})
		q.notify(j)
		return
	}
	j.edit(func() {
		inputs[0].Outputs = append(inputs[0].Outputs, output)
		for _, f := range inputs {
			f.State = FileDone
			f.Progress = 1
		}
	})
	q.notify(j)
}

// DeleteStaleWorkFolders deletes the work folders of jobs a crash or a power
// cut kept from removing them: intermediate PDFs, TIFF pages and video frames
// can take gigabytes. Only a job's folder nothing was written in for age goes,
// so a job still running in another Filee (the command line) keeps its own.
func DeleteStaleWorkFolders(workRoot string, age time.Duration) {
	// A temp folder everyone can write in may hold a link to somewhere else: never follow one.
	fi, err := os.Lstat(workRoot)
	if err != nil || !fi.IsDir() {
		return
	}
	entries, err := os.ReadDir(workRoot)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !isJobID(e.Name()) || e.Type()&os.ModeSymlink != 0 || !e.IsDir() {
			continue
		}
		folder := filepath.Join(workRoot, e.Name())
		last, err := lastWrite(folder)
		if err != nil {
			continue // something in it can't be read: it stays
		}
		if time.Since(last) > age {
			_ = os.RemoveAll(folder)
		}
	}
}

func isJobID(name string) bool {
	if len(name) != 10 {
		return false
	}
	for _, c := range name {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// lastWrite is the newest time of anything in folder: a folder's own time
// changes only when an entry right inside it does. Links are not followed.
func lastWrite(folder string) (time.Time, error) {
	var last time.Time
	err := filepath.WalkDir(folder, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(last) {
			last = info.ModTime()
		}
		return nil
	})
	return last, err
}
