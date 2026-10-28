package jobs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/presets"
)

// fileOptions change how runFile converts a file that isn't the job's final output.
type fileOptions struct {
	// intermediate is the format a non-final file becomes ("pdf" when "").
	intermediate string
	// ceiling is the file's progress once the route is done (1 when 0).
	ceiling float64
	// preset is the preset the route runs with (the job's when nil).
	preset *presets.Preset
}

// runFile converts one source file along its planned route. With final
// false, results stay in the work folder as intermediate files (PDF for
// merging, TIFF pages for a multi-page TIFF). It returns the files written.
func (q *Queue) runFile(j *Job, f *FileResult, index int, planner *convert.Planner, workDir string, final bool, o fileOptions) []string {
	ctx := j.ctx
	runWith := o.preset
	if runWith == nil {
		runWith = j.preset
	}
	if o.intermediate == "" {
		o.intermediate = "pdf"
	}
	ceiling := o.ceiling
	if ceiling == 0 {
		ceiling = 1
	}
	if f.stopped() {
		return nil
	}
	fail := func(key, detail string) []string {
		q.failFile(j, f, key, detail)
		return nil
	}
	source := formats.Detect(f.SourcePath)
	if source == nil {
		return fail("error.unsupported_source", "."+formats.ExtensionOf(f.SourcePath))
	}
	// A file just made with "New" in the file manager stays 0 bytes until its
	// app saves it: no engine can read it, and their errors don't say why.
	if isEmptyFile(f.SourcePath) {
		return fail("error.empty_file", "")
	}

	target := runWith.TargetFormat
	switch {
	case !final:
		target = o.intermediate
	case target == presets.SameAsSource:
		target = source.ID
	}

	// "Save every frame", "Extract Live/Motion Photo video" and "Live / Motion
	// Photo" are steps of their own: never for an intermediate file, and never
	// on the way to another format.
	frames := final && runWith.SavesFramesOf(source.ID)
	motion := final && runWith.ExtractsMotionOf(source.ID)
	live := final && runWith.MakesLivePhotoOf(source.ID)
	// "Recognize text": Tesseract writes a searchable PDF first, read on from
	// there. Also for the PDFs that are merged into one.
	ocr := (final || target == "pdf") && runWith.RecognizesTextOf(source.ID)
	route := planner.Plan(source.ID, target, convert.Options{Frames: frames, MotionPhoto: motion, LivePhoto: live, Ocr: ocr})
	if route == nil {
		return fail("error.no_route", source.ID+" → "+target)
	}

	// A step that draws text without shaping would break Arabic, Hebrew, Indic
	// or Thai text: such a file takes a route without it, or is refused with
	// the reason rather than written broken.
	var scripts *convert.TextScripts
	for _, s := range route.Steps {
		if s.Unshaped {
			if check, ok := s.Converter.(convert.TextShapingCheck); ok {
				scripts = q.scanScripts(check, f.SourcePath, source.ID)
			}
			break
		}
	}
	if scripts != nil && scripts.NeedsShaping != "" {
		q.log.Info("text needs shaping: planning a route without the unshaped engine", "file", f.SourcePath, "script", scripts.NeedsShaping)
		route = planner.Plan(source.ID, target, convert.Options{Frames: frames, Shaping: true, Ocr: ocr})
		if route == nil {
			return fail("error.needs_shaping", scripts.NeedsShaping)
		}
	} else if scripts != nil && scripts.WithoutWidths != "" {
		// Letters the engine has no widths for overlap but stay readable: the
		// file is never refused, it only takes a route without the unshaped step when there is one.
		q.log.Info("text can't be measured: planning a route without the unshaped engine", "file", f.SourcePath, "script", scripts.WithoutWidths)
		if r := planner.Plan(source.ID, target, convert.Options{Frames: frames, Shaping: true}); r != nil {
			route = r
		}
	}

	// Rotate / flip / hidden parts: only an engine that turns images does it,
	// and only once (EPS → PNG → JPG would otherwise turn twice).
	turnAt := -1
	reworks := runWith.Image.ReworksPixels()
	if reworks {
		turnAt = route.FirstTransformStep()
		if turnAt < 0 {
			key := "error.cannot_turn"
			if runWith.Image.HidesAreas() {
				key = "error.cannot_hide"
			}
			return fail(key, source.ID+" → "+target)
		}
	}
	unturned := runWith
	if turnAt >= 0 {
		unturned = runWith.Clone()
		unturned.Image.Rotation = presets.RotateNone
		unturned.Image.Flip = presets.FlipNone
		unturned.Image.HiddenAreas = nil
	}

	var floor float64
	j.edit(func() {
		f.State = FileRunning
		// A file saved from a browser first has part of its progress already.
		floor = f.Progress
	})
	q.notify(j)
	ceiling = max(ceiling, floor)

	// The last step's allocator: how many paths it handed out tells a single
	// output from several, and a failed conversion is undone through it.
	var last *finalAllocator
	zipResult := final && runWith.Output.ZipResult

	outputs, err := func() ([]string, error) {
		inputs := []string{f.SourcePath}
		n := len(route.Steps)
		for s, step := range route.Steps {
			isLast := s == n-1
			var alloc convert.OutputAllocator
			if isLast && final && !zipResult {
				last = q.newFinalAllocator(j, f.SourcePath, index, false)
				alloc = last
			} else {
				alloc = &tempAllocator{workDir: workDir, base: formats.NameWithoutExtension(f.SourcePath)}
			}
			var outs []string
			for _, input := range inputs {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				stepIndex := s
				progress := func(p float64) {
					v := floor + (ceiling-floor)*(float64(stepIndex)+min(1, max(0, p)))/float64(n)
					moved := false
					j.edit(func() {
						if v > f.Progress {
							f.Progress = v
							moved = true
						}
					})
					if moved {
						q.notify(j)
					}
				}
				// Turned (and covered) once, in step turnAt.
				preset := unturned
				if s == turnAt {
					preset = runWith
				}
				// Playback speed changes once, in the step that writes the output.
				if preset.Image.ChangesSpeed() && !(isLast && final) {
					preset = preset.Clone()
					preset.Image.AnimationSpeed = 1
				}
				// Likewise the size limit: only the result has to fit.
				if (preset.Image.MaxSizeMb > 0 || preset.Media.MaxSizeMb > 0) && !(isLast && final) {
					preset = preset.Clone()
					preset.Image.MaxSizeMb = 0
					preset.Media.MaxSizeMb = 0
				}
				st := &convert.Step{
					InputPath: input, From: step.From, To: step.To, Preset: preset, Output: alloc, WorkDir: workDir,
					FrameSequence: step.FrameSequence, MotionPhoto: step.MotionPhoto, LivePhoto: step.LivePhoto,
					Notice: func(key string) { j.edit(func() { f.NoticeKey = key }) },
					MissingFonts: func(names []string) {
						j.edit(func() { f.MissingFonts = appendFonts(f.MissingFonts, names) })
						q.notify(j)
					},
					OriginalSourcePath: f.SourcePath,
				}
				written, err := q.convertStep(ctx, step.Converter, st, progress)
				if err != nil {
					return nil, err
				}
				outs = append(outs, written...)
			}
			inputs = outs
		}

		// Under "Save as ZIP" the output is still in the work folder. Never
		// with hidden parts: the source copied over the result would show them again.
		copiedSource := false
		if final && runWith.Output.OnlyIfSmaller && !runWith.Image.HidesAreas() && len(inputs) == 1 {
			var err error
			if copiedSource, err = copyIfNotSmaller(f.SourcePath, inputs[0]); err != nil {
				return nil, err
			}
		}
		if copiedSource {
			j.edit(func() { f.NoticeKey = "notice.pdf_not_smaller" })
		}
		// Watermark, page numbers, password: on every PDF this file became.
		if final && runWith.TargetFormat == "pdf" {
			if err := q.finishPdfs(ctx, runWith, inputs); err != nil {
				return nil, err
			}
		}
		// The size limit (#553): a result the engine couldn't fit isn't handed over.
		if final {
			if err := checkSizeLimit(runWith, inputs); err != nil {
				return nil, err
			}
		}

		switch {
		case zipResult && len(inputs) > 0:
			zipped, err := q.packZip(j, f, index, inputs)
			if err != nil {
				return nil, err
			}
			inputs = zipped
		case final && len(inputs) == 1 && last != nil && last.handed() == 1:
			// One output, and nothing else handed out for it (a Markdown file's picture folder goes with it).
			inputs = []string{q.nextToSource(j, f, index, inputs[0], last)}
		}

		// The source's dates are read now: "Replace the original" may send it to the Trash below.
		var dates *fileDates
		if final && runWith.Output.KeepDates {
			dates = datesOf(f.SourcePath)
		}
		if final && !zipResult && len(inputs) == 1 {
			inputs = []string{q.replaceOriginal(j, f, source.ID, inputs[0], copiedSource)}
		}
		// A name the engine took but wrote nothing to is free again.
		if last != nil {
			last.release(inputs)
		}
		if final && dates != nil {
			q.copyDates(*dates, inputs)
		}
		return inputs, nil
	}()

	if err != nil {
		if last != nil {
			last.discard()
		}
		cancelled, key, detail := classify(j, err)
		if cancelled {
			j.edit(func() { f.State = FileCancelled })
			q.notify(j)
			return nil
		}
		if key == "error.conversion_failed" {
			q.log.Warn("converting a file failed", "file", f.SourcePath, "error", err)
		} else {
			q.log.Info("converting a file was refused", "file", f.SourcePath, "reason", err)
		}
		return fail(key, detail)
	}

	j.edit(func() {
		if final {
			f.Outputs = append(f.Outputs, outputs...)
		}
		f.State = FileDone
		if len(outputs) == 0 && final {
			f.State = FileSkipped
		}
		f.Progress = ceiling
	})
	q.notify(j)
	return outputs
}

// convertStep runs one engine step inside its gate; a panic is an error.
func (q *Queue) convertStep(ctx context.Context, c convert.Converter, st *convert.Step, progress convert.Progress) (out []string, err error) {
	gate := q.gateFor(c)
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-gate }()
	err = safe(func() error {
		var e error
		out, e = c.Convert(ctx, st, progress)
		return e
	})
	if err == nil {
		err = ctx.Err()
	}
	return out, err
}

// scanScripts runs the engine's scan; one that fails finds nothing, and the
// conversion reports the file.
func (q *Queue) scanScripts(check convert.TextShapingCheck, path, format string) (out *convert.TextScripts) {
	defer func() {
		if r := recover(); r != nil {
			q.log.Warn("could not check the scripts of a file", "file", path, "panic", r)
			out = nil
		}
	}()
	s := check.ScanScripts(path, format)
	return &s
}

// copyIfNotSmaller puts the source in place of output when both have the
// same format and the output is not smaller ("Only if smaller"). True when it did.
func copyIfNotSmaller(source, output string) (bool, error) {
	if detectID(source) != detectID(output) {
		return false, nil
	}
	of, err := os.Stat(output)
	if err != nil || of.IsDir() {
		return false, nil
	}
	sf, err := os.Stat(source)
	if err != nil || of.Size() < sf.Size() {
		return false, nil
	}
	return true, copyFileStream(source, output)
}

// tempAllocator hands out unique paths inside the job's work folder for intermediate files.
type tempAllocator struct {
	workDir, base string
}

func (t *tempAllocator) Allocate(ext, suffix string) string {
	dir := filepath.Join(t.workDir, newJobID()[:8])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	ext = trimDot(ext)
	if ext == "" {
		return filepath.Join(dir, t.base+suffix)
	}
	return filepath.Join(dir, fmt.Sprintf("%s%s.%s", t.base, suffix, ext))
}

func trimDot(ext string) string {
	for len(ext) > 0 && ext[0] == '.' {
		ext = ext[1:]
	}
	return ext
}
