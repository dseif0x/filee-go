// Command filee converts files from the command line (convert, watch,
// formats, presets, engines) and starts the desktop app (filee app). Results
// are written for people, one line per file, or with --json as one JSON
// document. Exit codes: 0 everything converted, 1 some files failed, 2 wrong
// arguments or a missing input, 3 nothing to convert.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dseif0x/filee-go/internal/app"
	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/gui"
	"github.com/dseif0x/filee-go/internal/i18n"
	"github.com/dseif0x/filee-go/internal/jobs"
	"github.com/dseif0x/filee-go/internal/platform"
	"github.com/dseif0x/filee-go/internal/presets"
	"github.com/dseif0x/filee-go/internal/watch"
)

const (
	exitSuccess    = 0
	exitSomeFailed = 1
	exitUsage      = 2
	exitNothing    = 3
)

// run is the whole command line; newApp makes the app (tests pass one with a
// temporary data folder and fake engines).
func run(ctx context.Context, args []string, stdout, stderr io.Writer, newApp func() (*app.App, error)) int {
	o := parseOptions(args)
	if o.err != "" {
		fmt.Fprintf(stderr, "filee: %s\nRun 'filee help' for usage.\n", o.err)
		return exitUsage
	}
	switch o.command {
	case "help":
		cmd := ""
		if len(o.inputs) > 0 {
			cmd = o.inputs[0]
		}
		fmt.Fprint(stdout, helpText(cmd))
		return exitSuccess
	case "version":
		fmt.Fprintf(stdout, "filee %s\n", app.Version)
		return exitSuccess
	}
	a, err := newApp()
	if err != nil {
		fmt.Fprintf(stderr, "filee: %v\n", err)
		return exitUsage
	}
	defer a.Close()
	switch o.command {
	case "convert":
		return convertCmd(o, a, stdout, stderr)
	case "watch":
		return watchCmd(ctx, o, a, stdout, stderr)
	case "formats":
		return formatsCmd(o, a, stdout, stderr)
	case "engines":
		return enginesCmd(a, stdout)
	case "settings":
		return webCmd(ctx, o, a, stdout, stderr)
	case "app":
		if o.webOnly {
			return webCmd(ctx, o, a, stdout, stderr)
		}
		if err := gui.RunWith(a, gui.Options{Background: o.background, SelfTest: o.selftest, Files: o.inputs}); errors.Is(err, gui.ErrNotBuilt) {
			// No desktop app for this OS: the settings open in the browser instead.
			return webCmd(ctx, o, a, stdout, stderr)
		} else if err != nil {
			fmt.Fprintf(stderr, "filee: %v\n", err)
			return exitUsage
		}
		return exitSuccess
	}
	return presetsCmd(a, stdout)
}

func convertCmd(o *options, a *app.App, stdout, stderr io.Writer) int {
	p, name := resolvePreset(o, a, stderr)
	if p == nil {
		return exitUsage
	}
	if o.output != "" {
		useFolder(p, o.output)
	}
	var missing []string
	files := expandInputs(o.inputs, o.recursive, &missing)
	for _, m := range missing {
		fmt.Fprintf(stderr, "filee: not found: %s\n", m)
	}
	if len(missing) > 0 {
		return exitUsage
	}
	if len(files) == 0 {
		fmt.Fprintln(stderr, "filee: no files to convert.")
		return exitNothing
	}
	var out io.Writer = stdout
	if o.json || o.quiet {
		out = nil
	}
	j := runJob(a, files, p, name, out)
	s := j.Snapshot()
	done := s.Count(jobs.FileDone)
	failed := s.Count(jobs.FileFailed, jobs.FileCancelled)
	switch {
	case o.json:
		writeJSON(s, a.Texts(), stdout)
	case !o.quiet && failed == 0:
		fmt.Fprintf(stdout, "Converted %d of %d file(s).\n", done, len(s.Files))
	case !o.quiet:
		fmt.Fprintf(stdout, "Converted %d of %d file(s), %d failed.\n", done, len(s.Files), failed)
	}
	if failed > 0 {
		return exitSomeFailed
	}
	return exitSuccess
}

func watchCmd(ctx context.Context, o *options, a *app.App, stdout, stderr io.Writer) int {
	p, name := resolvePreset(o, a, stderr)
	if p == nil {
		return exitUsage
	}
	folder, _ := filepath.Abs(o.inputs[0])
	// Unlike saved app rules, a CLI rule is made on every launch: a stable id
	// keeps an inbox writing into itself from converting its outputs again.
	rule := watch.NewRule(folder, "")
	rule.ID = "cli"
	rule.IncludeSubfolders = o.recursive
	if o.output != "" {
		rule.OutputFolder, _ = filepath.Abs(o.output)
	}
	if o.moveOriginals {
		rule.Originals = watch.MoveToOriginals
	}
	useFolder(p, rule.ResolvedOutputFolder())
	// As the app's watch folders: subfolders stay subfolders.
	p.Output.KeepFoldersFrom = rule.Folder
	var out io.Writer = stdout
	if o.quiet {
		out = nil
	}
	wo := watch.Options{}
	if o.settle >= 0 {
		wo.Settle = time.Duration(o.settle * float64(time.Second))
		if wo.Settle == 0 {
			wo.Settle = time.Nanosecond
		}
	}
	w := watch.New(rule, func(ctx context.Context, files []string) *jobs.Job {
		j := jobs.NewJob(files, p, name, nil)
		stop := context.AfterFunc(ctx, j.Cancel)
		defer stop()
		return runPrepared(a, j, out)
	}, wo)
	if err := w.Start(); err != nil {
		fmt.Fprintf(stderr, "filee: %v\n", err)
		return exitUsage
	}
	defer w.Close()
	if !o.quiet {
		fmt.Fprintf(stdout, "Watching %s → %s, saving to %s. Press Ctrl+C to stop.\n", rule.Folder, name, rule.ResolvedOutputFolder())
	}
	<-ctx.Done()
	return exitSuccess
}

// runJob runs one job and prints each file's result as soon as it is final (when out is set).
func runJob(a *app.App, files []string, p *presets.Preset, name string, out io.Writer) *jobs.Job {
	return runPrepared(a, jobs.NewJob(files, p, name, nil), out)
}

var cancelRunning = struct {
	sync.Mutex
	jobs map[*jobs.Job]bool
}{jobs: map[*jobs.Job]bool{}}

func runPrepared(a *app.App, j *jobs.Job, out io.Writer) *jobs.Job {
	var mu sync.Mutex
	printed := map[int]bool{}
	texts := a.Texts()
	print := func() {
		if out == nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		for i, f := range j.Files() {
			if f.State != jobs.FilePending && f.State != jobs.FileRunning && !printed[i] {
				printed[i] = true
				fmt.Fprintln(out, describe(f, texts))
			}
		}
	}
	unsub := a.Queue().Subscribe(func(e jobs.Event) {
		if e.Job == j {
			print()
		}
	})
	cancelRunning.Lock()
	cancelRunning.jobs[j] = true
	cancelRunning.Unlock()
	a.Queue().Run(j)
	cancelRunning.Lock()
	delete(cancelRunning.jobs, j)
	cancelRunning.Unlock()
	unsub()
	print()
	return j
}

// cancelAll cancels the conversions running now (Ctrl+C).
func cancelAll() {
	cancelRunning.Lock()
	defer cancelRunning.Unlock()
	for j := range cancelRunning.jobs {
		j.Cancel()
	}
}

func describe(f jobs.FileResult, texts *i18n.Localizer) string {
	src := shorten(f.SourcePath)
	switch f.State {
	case jobs.FileDone:
		var outs []string
		for _, o := range f.Outputs {
			outs = append(outs, shorten(o))
		}
		s := fmt.Sprintf("ok    %s -> %s", src, strings.Join(outs, ", "))
		if f.NoticeKey != "" {
			s += " (" + texts.Notice(f.NoticeKey, f.NoticeReasonKey, f.NoticeDetail) + ")"
		}
		if len(f.MissingFonts) > 0 {
			s += " (missing fonts: " + strings.Join(f.MissingFonts, ", ") + ")"
		}
		return s
	case jobs.FileSkipped:
		return fmt.Sprintf("skip  %s (the output file already exists)", src)
	case jobs.FileCancelled:
		return fmt.Sprintf("stop  %s (cancelled)", src)
	}
	return fmt.Sprintf("fail  %s: %s", src, errorText(f, texts))
}

func errorText(f jobs.FileResult, texts *i18n.Localizer) string {
	msg := "conversion failed"
	if f.ErrorKey != "" {
		msg = texts.Text(f.ErrorKey)
	}
	if strings.TrimSpace(f.ErrorDetail) == "" {
		return msg
	}
	return msg + " (" + f.ErrorDetail + ")"
}

// shorten is relative to the current folder when the file is inside it, else the full path.
func shorten(path string) string {
	wd, err := os.Getwd()
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(wd, path)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return path
	}
	return rel
}

type jsonFile struct {
	Source       string   `json:"source"`
	State        string   `json:"state"`
	Outputs      []string `json:"outputs"`
	Error        string   `json:"error,omitempty"`
	Notice       string   `json:"notice,omitempty"`
	MissingFonts []string `json:"missingFonts,omitempty"`
}

func writeJSON(s jobs.Snapshot, texts *i18n.Localizer, out io.Writer) {
	doc := struct {
		Preset    string     `json:"preset"`
		Target    string     `json:"target"`
		Converted int        `json:"converted"`
		Skipped   int        `json:"skipped"`
		Failed    int        `json:"failed"`
		Files     []jsonFile `json:"files"`
	}{s.PresetName, s.Target, s.Count(jobs.FileDone), s.Count(jobs.FileSkipped), s.Count(jobs.FileFailed, jobs.FileCancelled), []jsonFile{}}
	for _, f := range s.Files {
		jf := jsonFile{Source: f.SourcePath, State: strings.ToLower(string(f.State)), Outputs: f.Outputs, MissingFonts: f.MissingFonts}
		if jf.Outputs == nil {
			jf.Outputs = []string{}
		}
		if f.State == jobs.FileFailed || f.State == jobs.FileCancelled {
			jf.Error = errorText(f, texts)
		} else if f.NoticeKey != "" {
			jf.Notice = texts.Notice(f.NoticeKey, f.NoticeReasonKey, f.NoticeDetail)
		}
		doc.Files = append(doc.Files, jf)
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	fmt.Fprintln(out, string(b))
}

// resolvePreset is the preset of --to or --preset with --quality,
// --overwrite/--skip, --keep-dates and --zip applied (a copy), and its name.
func resolvePreset(o *options, a *app.App, stderr io.Writer) (*presets.Preset, string) {
	var p *presets.Preset
	if o.to != "" {
		f := app.FindFormat(o.to)
		if f == nil {
			fmt.Fprintf(stderr, "filee: unknown format '%s'. Run 'filee formats' for the list.\n", o.to)
			return nil, ""
		}
		if !f.Writable() {
			fmt.Fprintf(stderr, "filee: Filee reads %s but can't write it.\n", f.Name)
			return nil, ""
		}
		p = presets.New("", f.ID)
	} else {
		found := a.FindPreset(o.preset)
		switch {
		case found == nil:
			fmt.Fprintf(stderr, "filee: no preset '%s'. Run 'filee presets' for the list.\n", o.preset)
			return nil, ""
		case found.IsGroup():
			// A donut group converts nothing; name what it holds so the right one is easy to pick.
			var members []string
			for _, id := range found.SubPresetIDs {
				if m := a.Preset(id); m != nil && !m.IsGroup() && !m.IsTool() {
					members = append(members, m.ID)
				}
			}
			if len(members) > 0 {
				fmt.Fprintf(stderr, "filee: '%s' is a group on the donut, not a conversion. Use one of: %s\n", o.preset, strings.Join(members, ", "))
			} else {
				fmt.Fprintf(stderr, "filee: '%s' is a group on the donut, not a conversion, and has no presets left. Run 'filee presets' for the list.\n", o.preset)
			}
			return nil, ""
		case found.IsTool():
			fmt.Fprintf(stderr, "filee: '%s' is an interactive tool, not a conversion.\n", o.preset)
			return nil, ""
		}
		p = found
	}
	name := a.Texts().PresetName(p)
	p = a.Settings().WithDefaultOutput(p).Clone()
	if o.quality > 0 {
		p.Image.Quality = o.quality
	}
	if o.conflict != "" {
		p.Output.Conflict = o.conflict
	}
	if o.keepDates {
		p.Output.KeepDates = true
	}
	if o.zip {
		p.Output.ZipResult = true
	}
	return p, name
}

func useFolder(p *presets.Preset, folder string) {
	p.Output.Location = presets.LocationCustomFolder
	p.Output.CustomFolder, _ = filepath.Abs(folder)
}

// expandInputs are the files of the arguments: files as given, wildcards
// (which cmd.exe and PowerShell pass on unexpanded) and folders (their files
// in formats Filee knows; with recursive also in sub folders).
func expandInputs(inputs []string, recursive bool, missing *[]string) []string {
	var files []string
	inFolder := func(dir, pattern string, onlyKnown bool) []string {
		var out []string
		_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if p != dir && !recursive {
					return filepath.SkipDir
				}
				return nil
			}
			if ok, _ := filepath.Match(pattern, d.Name()); ok && (!onlyKnown || formats.Detect(p) != nil) {
				out = append(out, p)
			}
			return nil
		})
		return out
	}
	for _, in := range inputs {
		if strings.ContainsAny(in, "*?") {
			dir := filepath.Dir(in)
			matches := inFolder(dir, filepath.Base(in), false)
			if len(matches) == 0 {
				*missing = append(*missing, in)
			}
			files = append(files, matches...)
			continue
		}
		fi, err := os.Stat(in)
		switch {
		case err != nil:
			*missing = append(*missing, in)
		case fi.IsDir():
			files = append(files, inFolder(in, "*", true)...)
		default:
			files = append(files, in)
		}
	}
	var out []string
	for _, f := range files {
		abs, err := filepath.Abs(f)
		if err != nil {
			continue
		}
		if !slices.ContainsFunc(out, func(o string) bool { return platform.SamePath(o, abs) }) {
			out = append(out, abs)
		}
	}
	return out
}

func formatsCmd(o *options, a *app.App, stdout, stderr io.Writer) int {
	if len(o.inputs) > 0 {
		from := app.FindFormat(o.inputs[0])
		if from == nil {
			fmt.Fprintf(stderr, "filee: unknown format '%s'.\n", o.inputs[0])
			return exitUsage
		}
		installed := a.Catalog().Planner()
		// Engines a download would add, not those this platform lacks.
		var assume []string
		for _, c := range a.Catalog().All() {
			if a.Catalog().StatusOf(c).ReasonKey != "engine.reason.unsupported_platform" {
				assume = append(assume, c.ID())
			}
		}
		everything := a.Catalog().Planner(assume...)
		var now, later []string
		for _, t := range formats.Known {
			if !t.Writable() || t.ID == from.ID || t.ID == formats.Folder {
				continue
			}
			switch {
			case installed.Steps(from.ID, t.ID) > 0:
				now = append(now, t.ID)
			case everything.Steps(from.ID, t.ID) > 0:
				later = append(later, t.ID)
			}
		}
		fmt.Fprintf(stdout, "%s (%s) converts to:\n", from.Name, strings.Join(from.Extensions, ", "))
		if len(now) > 0 {
			fmt.Fprintf(stdout, "  %s\n", strings.Join(now, ", "))
		} else {
			fmt.Fprintln(stdout, "  (nothing with the engines installed now)")
		}
		if len(later) > 0 {
			fmt.Fprintf(stdout, "With optional engines (install them in Filee, Settings → Engines):\n  %s\n", strings.Join(later, ", "))
		}
		return exitSuccess
	}
	for _, cat := range formats.Categories {
		first := true
		for _, f := range formats.Known {
			if f.Category != cat || f.ID == formats.Folder {
				continue
			}
			if first {
				fmt.Fprintf(stdout, "%s:\n", cat)
				first = false
			}
			ro := ""
			if !f.Writable() {
				ro = " (read only)"
			}
			fmt.Fprintf(stdout, "  %-10s %s%s  .%s\n", f.ID, f.Name, ro, strings.Join(f.Extensions, " ."))
		}
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Run 'filee formats <format>' to see what a format converts to.")
	return exitSuccess
}

func presetsCmd(a *app.App, stdout io.Writer) int {
	fmt.Fprintf(stdout, "%-24s %-28s %s\n", "ID", "NAME", "TARGET")
	texts := a.Texts()
	for _, p := range a.Presets() {
		if !p.IsGroup() && !p.IsTool() {
			fmt.Fprintf(stdout, "%-24s %-28s %s\n", p.ID, texts.PresetName(p), p.TargetFormat)
		}
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Use one with: filee convert <files> --preset <id or name>")
	return exitSuccess
}

func enginesCmd(a *app.App, stdout io.Writer) int {
	engines := a.Engines()
	if len(engines) == 0 {
		fmt.Fprintln(stdout, "No engines are built into this filee.")
		return exitSuccess
	}
	fmt.Fprintf(stdout, "%-16s %-28s %-6s %s\n", "ID", "NAME", "EDGES", "STATUS")
	for _, e := range engines {
		status := "available"
		if e.Version != "" {
			status += " (" + e.Version + ")"
		}
		if !e.Available {
			status = a.Text(e.ReasonKey)
			if e.Detail != "" {
				status += " (" + e.Detail + ")"
			}
		}
		fmt.Fprintf(stdout, "%-16s %-28s %-6d %s\n", e.ID, e.Name, e.Edges, status)
	}
	return exitSuccess
}
