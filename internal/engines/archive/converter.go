// Package archive is the "archive" engine: unpack (to a folder, the
// "Extract" preset), convert between archive formats (ZIP, 7Z, TAR, TAR.GZ,
// TAR.BZ2, TAR.XZ and the single-file GZ / BZ2 / XZ), and "Compress into one
// archive" for any files (convert.FileCombiner).
//
// The C# original reads through its bundled 7-Zip. Here Filee's own pure-Go
// readers and writers do nearly everything; 7-Zip (7zz / 7z, when installed)
// writes 7Z and reads the formats without a Go reader (DMG, IMG, LHA, ARJ,
// LZX and Quantum cabinets), and reads ISO images (UDF) when installed.
package archive

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/presets"
	"github.com/dseif0x/filee-go/internal/tools"
)

// archiveTargets are the archive formats Filee writes (plus formats.Folder).
var archiveTargets = []string{"zip", "7z", "tar", "tgz", "tbz2", "txz"}

// singleFileSources are compressed single files (no names or folders inside); they convert among each other.
var singleFileSources = []string{"gz", "bz2", "xz", "lzma", "z", "lz"}

// Converter is the archive engine.
type Converter struct {
	mu       sync.Mutex
	sevenZip string
	located  bool
}

func New() *Converter { return &Converter{} }

func (*Converter) ID() string   { return "archive" }
func (*Converter) Name() string { return "Archives (7-Zip)" }

// MaxParallelism: two jobs at once keep the disk busy without thrashing it.
func (*Converter) MaxParallelism() int { return 2 }

func (*Converter) Edges() []convert.Edge {
	var edges []convert.Edge
	for _, from := range readableFormats {
		for _, to := range append(slices.Clone(archiveTargets), formats.Folder) {
			edges = append(edges, convert.E(from, to))
		}
	}
	for _, from := range singleFileSources {
		for _, to := range singleFileFormats {
			edges = append(edges, convert.E(from, to))
		}
	}
	return edges
}

func (c *Converter) tool() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.located {
		c.sevenZip = locateSevenZip()
		c.located = true
	}
	return c.sevenZip
}

func (c *Converter) Status() convert.Status {
	c.mu.Lock()
	c.sevenZip = locateSevenZip()
	c.located = true
	seven := c.sevenZip
	c.mu.Unlock()
	if seven == "" {
		return convert.Available("built-in; 7-Zip not found (7Z output, DMG, IMG, LHA, ARJ and LZX cabinets need it)", "built-in")
	}
	return convert.Available(seven, "built-in · 7-Zip")
}

func (c *Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	seven := c.tool()
	x := &extractor{sevenZip: seven}
	if step.To == formats.Folder {
		return extractToFolder(ctx, step, x, progress)
	}
	if step.To == "7z" && seven == "" {
		return nil, errNo7Zip
	}

	// Archive → archive: unpack into the job's work folder, then pack everything that came out.
	scratch := filepath.Join(step.WorkDir, "archive-"+randomSuffix())
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		return nil, err
	}
	defer tryDeleteFolder(scratch)
	est := startEstimate(scaled(progress, 0, 1), typicalDuration(step.InputPath), 0.02, 0.45)
	unpacked, err := x.extract(ctx, step.InputPath, step.From, scratch)
	est()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(unpacked)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	items := make([]packItem, 0, len(names))
	for _, n := range names {
		items = append(items, packItem{filepath.Join(unpacked, n), n})
	}
	output := step.Output.Allocate(formats.ByID(step.To).PrimaryExtension(), "")
	if output == "" {
		return nil, nil
	}
	level := presets.ArchiveNormal
	if step.Preset != nil {
		level = step.Preset.Archive.Level
	}
	if err := (&packer{seven}).pack(ctx, items, output, step.To, level, scratch, scaled(progress, 0.5, 1)); err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{output}, nil
}

// extractToFolder unpacks into the folder the output rule names. The archive
// is unpacked into a hidden folder next to it (same drive, so the final move
// is instant) and only moved into place when complete; one top folder is not
// nested twice.
func extractToFolder(ctx context.Context, step *convert.Step, x *extractor, progress convert.Progress) ([]string, error) {
	output := step.Output.Allocate("", "")
	if output == "" {
		return nil, nil
	}
	staging := filepath.Join(filepath.Dir(output), "."+filepath.Base(output)+".filee-"+randomSuffix())
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return nil, err
	}
	hide(staging)
	defer tryDeleteFolder(staging)
	est := startEstimate(progress, typicalDuration(step.InputPath), 0.02, 0.95)
	unpacked, err := x.extract(ctx, step.InputPath, step.From, staging)
	est()
	if err != nil {
		return nil, err
	}
	if err := moveFolder(contentRoot(unpacked), output); err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{output}, nil
}

func (c *Converter) CanCombineInto(format string) bool {
	if builtInWriters[format] {
		return true
	}
	return slices.Contains(archiveTargets, format) && c.tool() != ""
}

func (c *Converter) Combine(ctx context.Context, inputs []string, output, format string, preset *presets.Preset, workDir string, progress convert.Progress) error {
	level := presets.ArchiveNormal
	if preset != nil {
		level = preset.Archive.Level
	}
	if err := (&packer{c.tool()}).pack(ctx, uniqueEntries(inputs), output, format, level, workDir, progress); err != nil {
		return err
	}
	progress.Report(1)
	return nil
}

// uniqueEntries stores each file (or folder) under its own name; later ones
// with a name already used become "name (2).ext".
func uniqueEntries(paths []string) []packItem {
	used := map[string]bool{}
	items := make([]packItem, 0, len(paths))
	for _, p := range paths {
		name := filepath.Base(strings.TrimRight(p, `/\`))
		ext := filepath.Ext(name)
		stem := strings.TrimSuffix(name, ext)
		for n := 2; used[strings.ToLower(name)]; n++ {
			name = fmt.Sprintf("%s (%d)%s", stem, n, ext)
		}
		used[strings.ToLower(name)] = true
		items = append(items, packItem{p, name})
	}
	return items
}

// typicalDuration of unpacking for the progress estimate (~60 MB/s).
func typicalDuration(p string) time.Duration {
	var size int64
	if st, err := os.Stat(p); err == nil {
		size = st.Size()
	}
	return time.Duration(max(0.5, float64(size)/60e6) * float64(time.Second))
}

func scaled(p convert.Progress, from, to float64) convert.Progress {
	if p == nil {
		return nil
	}
	return func(f float64) { p(from + (to-from)*f) }
}

// startEstimate reports estimated progress until the returned stop is called.
func startEstimate(p convert.Progress, typical time.Duration, from, to float64) func() {
	if p == nil {
		return func() {}
	}
	e := tools.StartEstimate(p, typical, from, to)
	return e.Stop
}
