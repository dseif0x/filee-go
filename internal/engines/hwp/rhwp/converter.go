// Package rhwp is the "rhwp" engine: HWP / HWPX → PDF, HWP → HWPX and HWPX → HWP with the rhwp command line tool
// (MIT, https://github.com/edwardkim/rhwp). Works without Hancom Office.
//
// rhwp draws text one character after the other, left to right, without OpenType shaping: Arabic letters don't join
// and run the wrong way, Indic vowel signs and conjuncts come out as separate glyphs with dotted circles (issues
// #185, #202). Its PDF edges are marked Unshaped, so a document with such text (found by ScanScripts) goes to
// LibreOffice, or is refused with that reason when LibreOffice is missing. rhwp's width tables cover only ASCII and
// Latin-1, general punctuation, math, the CJK forms and Hangul, so a letter of any other script overlaps at a fixed
// half-em (rhwp#8143): such a document takes a route without rhwp when one exists, and is never refused for it.
package rhwp

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/hwp/xmltree"
	"github.com/dseif0x/filee-go/internal/engines/libreoffice"
	"github.com/dseif0x/filee-go/internal/engines/spreadsheet"
	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/tools"
)

// rhwp renders text slowly: about 1 s per KB of section XML (some 4 s per page of a novel) with rhwp 0.8.7, so a
// 640-page book takes some 48 minutes; export-hwpx and convert take seconds for the same book. The time limit grows
// with the document's text, and still stops a rhwp that hangs.
//
// Nearly all of that time goes into the PDF backend, not the layout: the default one draws each page as SVG with one
// text element per character and converts that to PDF. Its "direct" backend paints the same layout straight into the
// PDF 30 to 100 times faster, but refuses pages with what it can't paint (gradients, patterns, shadows, connectors)
// and takes neither the generic fallback families nor the equation font of PdfOptions. Documents Filee writes from a
// spreadsheet (text in table cells, borders, fills) use it, with the SVG backend when it fails.
const (
	baseTimeLimit        = 2 * time.Minute
	timeLimitPerMegabyte = 30 * time.Minute
	maxTimeLimit         = 60 * time.Minute
	// typicalPerMegabyte is the usual rendering time per MB of section XML, for an HWP (whose characters aren't counted).
	typicalPerMegabyte = 15 * time.Minute
	// Usual rendering time per drawn character with the SVG and the direct backend (drives the progress estimate).
	typicalPerCharacter       = 3500 * time.Microsecond
	typicalPerCharacterDirect = 200 * time.Microsecond
	typicalDuration           = 1500 * time.Millisecond
)

// Spec finds rhwp: FILEE_RHWP, Filee's engine folder (engines/rhwp), the PATH.
var Spec = tools.Spec{Names: []string{"rhwp"}, Env: "FILEE_RHWP", Package: "rhwp", Dirs: map[string][]string{
	"darwin": {"rhwp", "rhwp/bin"}, "linux": {"rhwp", "rhwp/bin"}, "windows": {"rhwp", "rhwp/bin"},
}}

// Locate is the rhwp program, or "".
func Locate() string { return tools.Find(Spec) }

func locateCalibre() string {
	return tools.Find(tools.Spec{Names: []string{"ebook-convert"}, Env: "FILEE_CALIBRE", Package: "calibre", Dirs: map[string][]string{
		"darwin": {"calibre", "/Applications/calibre.app/Contents/MacOS"}, "linux": {"calibre", "/opt/calibre"},
		"windows": {"calibre", `C:\Program Files\Calibre2`},
	}})
}

// Converter is the "rhwp" engine.
type Converter struct{}

func New() *Converter { return &Converter{} }

func (*Converter) ID() string          { return "rhwp" }
func (*Converter) Name() string        { return "rhwp" }
func (*Converter) MaxParallelism() int { return 2 }

func (*Converter) Edges() []convert.Edge {
	return []convert.Edge{
		{From: "hwp", To: "pdf", Cost: 10, Unshaped: true},
		{From: "hwpx", To: "pdf", Cost: 10, Unshaped: true},
		// Lossless within the 한글 document model; anything → HWP goes through the HWPX writer first.
		convert.E("hwp", "hwpx"),
		convert.E("hwpx", "hwp"),
	}
}

func (*Converter) Status() convert.Status {
	exe := Locate()
	if exe == "" {
		return convert.Unavailable("engine.reason.not_installed", "")
	}
	return convert.Available(exe, "")
}

// ScanScripts names the scripts in the source's text that rhwp can't draw: one that needs shaping, and one it draws
// but has no widths for. Every format that reaches rhwp through the HWPX writer is scanned; a binary HWP is not.
func (*Converter) ScanScripts(sourcePath, sourceFormat string) convert.TextScripts {
	shaping, unmeasured := FindBothInFile(sourcePath, sourceFormat)
	// An HWPX stays with rhwp, the most faithful renderer for it; only shaping sends it elsewhere.
	if sourceFormat == "hwpx" {
		unmeasured = ""
	}
	return convert.TextScripts{NeedsShaping: shaping, WithoutWidths: unmeasured}
}

func (c *Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	exe := Locate()
	if exe == "" {
		return nil, errors.New("rhwp was not found")
	}
	temp := filepath.Join(step.WorkDir, fmt.Sprintf("rhwp-%s.%s", randomName(), step.To))
	original := step.OriginalSourcePath
	if original == "" {
		original = step.InputPath
	}
	// Intermediate HWPX files include template fonts the user's original document never requested.
	if step.To == "pdf" && step.MissingFonts != nil {
		if missing := libreoffice.MissingFonts(original); len(missing) > 0 {
			step.MissingFonts(missing)
		}
	}
	input := step.InputPath
	if step.To == "pdf" {
		input = prepare(ctx, exe, step)
	}
	text := TextSize(input)
	var characters *int64
	if step.To == "pdf" {
		characters = Characters(input)
	}
	progress.Report(0.1)

	run := func(args []string, typical time.Duration) (*tools.Result, error) {
		estimate := tools.StartEstimate(progress, typical, 0.1, 0.9)
		defer estimate.Stop()
		res, err := tools.Run(ctx, tools.Cmd{Path: exe, Args: args, Timeout: TimeLimit(text)})
		var exit *tools.ExitError
		if errors.As(err, &exit) {
			return res, nil // the output decides, as rhwp may still have written it
		}
		return res, err
	}
	refuse := func(err error) error {
		var timeout *tools.TimeoutError
		if errors.As(err, &timeout) {
			// E-books reach rhwp through the HWPX writer; calibre makes their PDFs much faster when ranked above it.
			key := "error.rhwp_timeout"
			if locateCalibre() != "" {
				key = "error.rhwp_timeout_calibre"
			}
			return convert.Refuse(key, err.Error())
		}
		return err
	}

	if step.To == "pdf" && UsesDirectBackend(original) {
		res, err := run([]string{"export-pdf", input, "-o", temp, "--backend", "direct"}, Typical(characters, text, true))
		if err != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err == nil && res != nil && res.ExitCode == 0 && written(temp) {
			return finish(step, temp, progress)
		}
		os.Remove(temp) // the SVG backend draws what the direct one refused
	}

	var args []string
	typical := typicalDuration
	switch step.To {
	case "pdf":
		args = append([]string{"export-pdf", input, "-o", temp}, PdfOptions(nil)...)
		typical = Typical(characters, text, false)
	case "hwpx":
		args = []string{"export-hwpx", step.InputPath, temp}
	case "hwp":
		args = []string{"convert", step.InputPath, temp}
	default:
		return nil, fmt.Errorf("rhwp cannot write %s", step.To)
	}
	res, err := run(args, typical)
	if err != nil {
		return nil, refuse(err)
	}
	if !written(temp) {
		stderr := ""
		code := 0
		if res != nil {
			stderr, code = strings.TrimSpace(string(res.Stderr)), res.ExitCode
		}
		return nil, fmt.Errorf("rhwp failed (exit %d). %s", code, stderr)
	}
	return finish(step, temp, progress)
}

func finish(step *convert.Step, temp string, progress convert.Progress) ([]string, error) {
	target := step.Output.Allocate(step.To, "")
	if target == "" {
		return nil, nil
	}
	if err := moveFile(temp, target); err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{target}, nil
}

func moveFile(from, to string) error {
	os.Remove(to)
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	if err := os.WriteFile(to, data, 0o644); err != nil {
		return err
	}
	return os.Remove(from)
}

func written(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Size() > 0
}

// UsesDirectBackend tells whether the PDF of a document converted from sourcePath is drawn with rhwp's direct
// backend: one Filee wrote from a spreadsheet.
func UsesDirectBackend(sourcePath string) bool {
	f := formats.Detect(sourcePath)
	if f == nil {
		return false
	}
	for _, s := range spreadsheet.SheetFormats {
		if s == f.ID {
			return true
		}
	}
	return false
}

// prepare is the document rhwp renders: a copy with the fixes of Prepare (fonts, shape sizes, equation scripts,
// tables floating in cells) when it needs any, else the original. An HWP is read through an HWPX copy for that, and
// used as it is when nothing needs fixing or the HWPX copy fails.
func prepare(ctx context.Context, exe string, step *convert.Step) string {
	hwpx := step.InputPath
	if step.From == "hwp" {
		hwpx = filepath.Join(step.WorkDir, fmt.Sprintf("rhwp-%s.hwpx", randomName()))
		tools.Run(ctx, tools.Cmd{Path: exe, Args: []string{"export-hwpx", step.InputPath, hwpx}, Timeout: TimeLimit(TextSize(step.InputPath))})
		if !written(hwpx) {
			return step.InputPath
		}
	}
	if p := Prepare(hwpx, step.WorkDir, nil); p != "" {
		return p
	}
	return step.InputPath
}

// TimeLimit is how long one rhwp run on a document with textBytes of text may take.
func TimeLimit(textBytes int64) time.Duration {
	limit := baseTimeLimit + time.Duration(float64(timeLimitPerMegabyte)*(float64(textBytes)/1048576.0))
	return min(limit, maxTimeLimit)
}

// Typical is the usual time of an export-pdf run: by its characters, else (an HWP) by its text size.
func Typical(characters *int64, textBytes int64, direct bool) time.Duration {
	var typical time.Duration
	if characters != nil {
		per := typicalPerCharacter
		if direct {
			per = typicalPerCharacterDirect
		}
		typical = per * time.Duration(*characters)
	} else {
		typical = time.Duration(float64(typicalPerMegabyte) * (float64(textBytes) / 1048576.0))
	}
	return max(typical, typicalDuration)
}

func isSectionEntry(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "contents/section") && strings.HasSuffix(lower, ".xml")
}

// Characters is the characters rhwp draws in an HWPX (the text of its sections without spaces: one SVG element
// each), or nil for a file that isn't an HWPX package.
func Characters(path string) *int64 {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil // not a ZIP (an HWP): the text size stands in
	}
	defer z.Close()
	var count int64
	sections := 0
	for _, f := range z.File {
		if !isSectionEntry(f.Name) {
			continue
		}
		sections++
		rc, err := f.Open()
		if err != nil {
			return nil
		}
		d, err := xmltree.Parse(rc)
		rc.Close()
		if err != nil {
			return nil
		}
		d.Root.Walk(func(e *xmltree.Element) bool {
			if e.Local != "t" {
				return true
			}
			for _, c := range e.Value() {
				if !unicode.IsSpace(c) {
					count++
				}
			}
			return false
		})
	}
	if sections == 0 {
		return nil
	}
	return &count
}

// TextSize is the size of a document's text, which rendering time follows: the unpacked section XML of an HWPX
// (pictures left out), else three times the file (an HWP deflates its body text to about a third).
func TextSize(path string) int64 {
	if z, err := zip.OpenReader(path); err == nil {
		var total int64
		for _, f := range z.File {
			if isSectionEntry(f.Name) {
				total += int64(f.UncompressedSize64)
			}
		}
		z.Close()
		if total > 0 {
			return total
		}
	}
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size() * 3
}
