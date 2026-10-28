// Package calibre is the "calibre" engine: e-book formats Filee does not
// read or write itself, with calibre's ebook-convert (GPL-3.0, a separate
// program): LIT, LRF, CHM, PDB, PML, RB, SNB, TCR and OEB → EPUB, and EPUB →
// MOBI, AZW3, LIT, LRF, PDB, PML, RB, SNB and TCR. EPUB is the hub: the
// built-in engine handles every other step, and calibre's edges cost more so
// built-in routes win where they exist.
//
// calibre also makes PDFs of e-books, much faster than the built-in route
// (HWPX writer → rhwp) for long books. That route stays the default;
// calibre's PDF edges win once the user ranks calibre above the HWPX writer.
//
// Each run gets its own calibre configuration, cache and temp folders inside
// the job's work directory, so Filee never reads or changes the settings of
// a calibre the user may have installed.
package calibre

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/tools"
)

// StallTimeout: ebook-convert prints its progress as it works, so a book
// takes as long as it needs; only this long without a single line means
// calibre is stuck.
const StallTimeout = 10 * time.Minute

// Inputs are the formats only calibre reads.
var Inputs = []string{"lit", "lrf", "chm", "pdb", "pml", "rb", "snb", "tcr", "oeb"}

// Outputs are the formats only calibre writes.
var Outputs = []string{"mobi", "azw3", "lit", "lrf", "pdb", "pml", "rb", "snb", "tcr"}

// PdfInputs are the books calibre makes PDFs of: the ones the built-in engines read too, and Inputs.
var PdfInputs = append([]string{"epub", "mobi", "azw3", "azw", "prc", "fb2", "htmlz", "txtz"}, Inputs...)

// PdfCost of e-book → PDF. The built-in route costs 31 in the default engine
// order (HWPX writer 10 + 8, rhwp 10 + 3) and calibre ranks last (+ 9), so 23
// loses by one; ranked above the HWPX writer, calibre wins.
const PdfCost = 23

// Spec finds ebook-convert: FILEE_CALIBRE, Filee's engine folder, the PATH and the usual install places.
var Spec = tools.Spec{
	Names:   []string{"ebook-convert"},
	Env:     "FILEE_CALIBRE",
	Package: "calibre",
	Dirs: map[string][]string{
		"darwin":  {"calibre/calibre.app/Contents/MacOS", "calibre/Contents/MacOS", "/Applications/calibre.app/Contents/MacOS", "~/Applications/calibre.app/Contents/MacOS"},
		"linux":   {"calibre", "/opt/calibre"},
		"windows": {"calibre", "calibre/Calibre2", `C:\Program Files\Calibre2`, `C:\Program Files (x86)\Calibre2`},
	},
}

// Converter is the calibre engine.
type Converter struct {
	mu  sync.Mutex
	exe string
}

func New() *Converter { return &Converter{} }

func (*Converter) ID() string   { return "calibre" }
func (*Converter) Name() string { return "Calibre" }

// MaxParallelism: ebook-convert is a large Python process; two at a time keep the PC responsive.
func (*Converter) MaxParallelism() int { return 2 }

func (*Converter) Edges() []convert.Edge {
	var e []convert.Edge
	for _, from := range Inputs {
		e = append(e, convert.EC(from, "epub", 20))
	}
	for _, to := range Outputs {
		e = append(e, convert.EC("epub", to, 20))
	}
	for _, from := range PdfInputs {
		e = append(e, convert.EC(from, "pdf", PdfCost))
	}
	return e
}

// Locate returns ebook-convert, or "".
func Locate() string { return tools.Find(Spec) }

func (c *Converter) Status() convert.Status {
	exe := Locate()
	c.mu.Lock()
	c.exe = exe
	c.mu.Unlock()
	if exe == "" {
		return convert.Unavailable("engine.reason.not_installed", "")
	}
	return convert.Available(exe, "")
}

func (c *Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	c.mu.Lock()
	exe := c.exe
	c.mu.Unlock()
	if exe == "" {
		exe = Locate()
	}
	if exe == "" {
		return nil, fmt.Errorf("calibre is not installed")
	}
	work := filepath.Join(step.WorkDir, "calibre-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	if err := os.MkdirAll(work, 0o755); err != nil {
		return nil, err
	}
	// ebook-convert picks formats by file extension: give it names it knows. An .opf (OEB) stays where
	// it is, next to the files it lists; a zipped .oeb is read by calibre's ZIP input.
	input := step.InputPath
	if !strings.EqualFold(filepath.Ext(input), ".opf") {
		ext := step.From
		if ext == "oeb" {
			ext = "zip"
		}
		input = filepath.Join(work, "input."+ext)
		if err := copyFile(step.InputPath, input); err != nil {
			return nil, err
		}
	}
	temp := filepath.Join(work, "output."+step.To)
	var env []string
	for _, d := range []struct{ name, dir string }{{"CALIBRE_CONFIG_DIRECTORY", "config"}, {"CALIBRE_CACHE_DIRECTORY", "cache"}, {"CALIBRE_TEMP_DIR", "temp"}} {
		p := filepath.Join(work, d.dir)
		if err := os.MkdirAll(p, 0o755); err != nil {
			return nil, err
		}
		env = append(env, d.name+"="+p)
	}
	env = append(env,
		"CALIBRE_OVERRIDE_LANG=en", // English messages in the error details, whatever the system language
		"PYTHONIOENCODING=utf-8",
		"PYTHONUNBUFFERED=1", // each progress line as it is printed: the stall watch counts on them
	)
	progress.Report(0.02)
	res, err := tools.Run(ctx, tools.Cmd{
		Path: exe, Args: append([]string{input, temp}, OutputOptions(step.To)...), Dir: work, Env: env,
		OnStdoutLine: func(l string) { reportProgress(l, progress) },
		OnStderrLine: func(l string) { reportProgress(l, progress) },
		StallTimeout: StallTimeout,
	})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if _, ok := err.(*tools.StalledError); ok {
		return nil, convert.Refuse(tools.StalledErrorKey, err.Error())
	}
	if _, ok := err.(*tools.RuntimeMissingError); ok {
		return nil, convert.Refuse(tools.RuntimeMissingKey, err.Error())
	}
	if err != nil || !isFile(temp) {
		out := ""
		if res != nil {
			out = string(res.Stderr)
			if strings.TrimSpace(out) == "" {
				out = string(res.Stdout)
			}
		}
		code := -1
		if res != nil {
			code = res.ExitCode
		}
		return nil, fmt.Errorf("calibre failed (exit %d). %s", code, lastLines(out))
	}
	output := step.Output.Allocate(formats.ByID(step.To).PrimaryExtension(), "")
	if output == "" {
		return nil, nil
	}
	if err := move(temp, output); err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{output}, nil
}

// OutputOptions are ebook-convert's options for a target. PDF: A4 like the
// built-in route (calibre's default is Letter) and page numbers.
func OutputOptions(to string) []string {
	if to == "pdf" {
		return []string{"--paper-size", "a4", "--pdf-page-numbers"}
	}
	return nil
}

var percent = regexp.MustCompile(`^\s*(\d{1,3})%\s`)

// reportProgress reads "34% Running transforms on e-book..." lines.
func reportProgress(line string, progress convert.Progress) {
	if m := percent.FindStringSubmatch(line); m != nil {
		if p, err := strconv.Atoi(m[1]); err == nil {
			progress.Report(float64(max(0, min(100, p))) / 100 * 0.95)
		}
	}
}

// lastLines is the end of calibre's output, where its error message (or Python traceback) is.
func lastLines(out string) string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines[max(0, len(lines)-3):], " ")
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func move(from, to string) error {
	os.Remove(to)
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	if err := copyFile(from, to); err != nil {
		return err
	}
	return os.Remove(from)
}
