// Package ghostscript is PostScript and EPS with Ghostscript (id
// "ghostscript", an external program): EPS/PS → PDF with pdfwrite, PDF →
// EPS/PS with eps2write/ps2write, EPS/PS → PNG with pngalpha (a transparent
// background, which a route through PDF would lose). "Compress PDF" may also
// run a PDF through pdfwrite (CompressPdf; no PDF → PDF edge, so routes
// don't change).
package ghostscript

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/install"
	"github.com/dseif0x/filee-go/internal/engines/media"
	"github.com/dseif0x/filee-go/internal/presets"
	"github.com/dseif0x/filee-go/internal/tools"
)

// Spec finds Ghostscript: FILEE_GHOSTSCRIPT, Filee's engines folder, PATH,
// Homebrew on macOS, C:\Program Files\gs\gs*\bin (newest first) on Windows.
var Spec = tools.Spec{
	Names: []string{"gs", "gswin64c"},
	Env:   "FILEE_GHOSTSCRIPT",
	Dirs: map[string][]string{
		"darwin":  {"ghostscript", "ghostscript/bin", "/opt/homebrew/opt/ghostscript/bin"},
		"linux":   {"ghostscript", "ghostscript/bin"},
		"windows": {"ghostscript", "C:/Program Files/gs/gs*/bin", "C:/Program Files (x86)/gs/gs*/bin"},
	},
}

const timeout = 5 * time.Minute

// cleanEnv: variables Ghostscript would otherwise read from the user's system
// (extra options, resource and font paths), so it behaves the same everywhere.
var cleanEnv = []string{"GS_OPTIONS=", "GS_LIB=", "GS_FONTPATH="}

// Locate returns the Ghostscript program, or "".
func Locate() string { return tools.Find(Spec) }

// Converter is the Ghostscript engine.
type Converter struct{}

func New() *Converter { return &Converter{} }

func (c *Converter) ID() string          { return "ghostscript" }
func (c *Converter) Name() string        { return "Ghostscript" }
func (c *Converter) MaxParallelism() int { return 0 }

// Transforms: only when rendering EPS / PostScript to PNG.
func (c *Converter) Transforms(from, to string) bool { return to == "png" }

func (c *Converter) Edges() []convert.Edge {
	return []convert.Edge{
		convert.E("eps", "pdf"), convert.E("ps", "pdf"), convert.E("pdf", "eps"), convert.E("pdf", "ps"),
		convert.E("eps", "png"), convert.E("ps", "png"),
	}
}

func (c *Converter) Status() convert.Status {
	gs := Locate()
	if gs == "" {
		return convert.Unavailable("engine.reason.not_installed", "gs")
	}
	version := ""
	if strings.HasPrefix(gs, tools.EnginesDir()) {
		version = install.Version("ghostscript")
	}
	return convert.Available(gs, version)
}

func (c *Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	gs := Locate()
	if gs == "" {
		return nil, errors.New("Ghostscript is not installed.")
	}
	progress.Report(0.05)
	var out []string
	var err error
	switch step.To {
	case "pdf":
		out, err = toPdf(ctx, gs, step, progress)
	case "ps":
		out, err = toPostScript(ctx, gs, step, progress)
	case "eps":
		out, err = toEps(ctx, gs, step, progress)
	case "png":
		out, err = toPng(ctx, gs, step, progress)
	default:
		err = fmt.Errorf("Ghostscript cannot write '%s'.", step.To)
	}
	if err != nil {
		return nil, err
	}
	progress.Report(1)
	return out, nil
}

// ConvertToPdf turns PostScript (PS, EPS, PostScript-based Illustrator) into
// PDF; EPS pages are cropped to the bounding box when crop.
func ConvertToPdf(ctx context.Context, gs, input, output string, crop bool, workDir string, progress convert.Progress) error {
	scratch, err := newScratch(workDir)
	if err != nil {
		return err
	}
	target := filepath.Join(scratch, "out.pdf")
	args := []string{"-sDEVICE=pdfwrite", "-dAutoRotatePages=/None"}
	if crop {
		args = append(args, "-dEPSCrop")
	}
	if err := run(ctx, gs, args, target, input, progress); err != nil {
		return err
	}
	return move(target, output)
}

// CompressSettings is pdfwrite's -dPDFSETTINGS for a level: pictures at 72, 150 or 300 dpi.
func CompressSettings(level presets.PdfCompression) string {
	switch level {
	case presets.PdfCompressScreen:
		return "/screen"
	case presets.PdfCompressPrint:
		return "/printer"
	}
	return "/ebook"
}

// CompressPdf rewrites a PDF smaller with pdfwrite's settings for level
// ("Compress PDF" with Ghostscript); the caller keeps it only when smaller.
func CompressPdf(ctx context.Context, gs, input, output string, level presets.PdfCompression, workDir string) error {
	scratch, err := newScratch(workDir)
	if err != nil {
		return err
	}
	target := filepath.Join(scratch, "out.pdf")
	args := []string{"-sDEVICE=pdfwrite", "-dPDFSETTINGS=" + CompressSettings(level), "-dCompatibilityLevel=1.6", "-dAutoRotatePages=/None"}
	if err := run(ctx, gs, args, target, input, nil); err != nil {
		return err
	}
	return move(target, output)
}

func toPdf(ctx context.Context, gs string, step *convert.Step, progress convert.Progress) ([]string, error) {
	out := step.Output.Allocate("pdf", "")
	if out == "" {
		return nil, nil
	}
	if err := ConvertToPdf(ctx, gs, step.InputPath, out, step.From == "eps", step.WorkDir, progress); err != nil {
		return nil, err
	}
	return []string{out}, nil
}

func pageList(pages []int) string {
	s := make([]string, len(pages))
	for i, p := range pages {
		s[i] = strconv.Itoa(p + 1)
	}
	return "-sPageList=" + strings.Join(s, ",")
}

func toPostScript(ctx context.Context, gs string, step *convert.Step, progress convert.Progress) ([]string, error) {
	out := step.Output.Allocate("ps", "")
	if out == "" {
		return nil, nil
	}
	scratch, err := newScratch(step.WorkDir)
	if err != nil {
		return nil, err
	}
	target := filepath.Join(scratch, "out.ps")
	args := []string{"-sDEVICE=ps2write"}
	if pages := selectedPages(step); pages != nil {
		args = append(args, pageList(pages))
	}
	if err := run(ctx, gs, args, target, step.InputPath, progress); err != nil {
		return nil, err
	}
	return []string{out}, move(target, out)
}

// toEps: EPS holds one page, so a multi-page PDF becomes one EPS per page (_p{n}).
func toEps(ctx context.Context, gs string, step *convert.Step, progress convert.Progress) ([]string, error) {
	scratch, err := newScratch(step.WorkDir)
	if err != nil {
		return nil, err
	}
	pages := selectedPages(step)
	args := []string{"-sDEVICE=eps2write"}
	if pages != nil {
		args = append(args, pageList(pages))
	}
	// %d makes Ghostscript write one numbered file per page.
	if err := run(ctx, gs, args, filepath.Join(scratch, "page-%d.eps"), step.InputPath, progress); err != nil {
		return nil, err
	}
	files := numbered(scratch, "page-", ".eps")
	if len(files) == 0 {
		return nil, errors.New("Ghostscript wrote no EPS file.")
	}
	var written []string
	for _, f := range files {
		number := f.index
		if pages != nil && f.index-1 < len(pages) {
			number = pages[f.index-1] + 1
		}
		suffix := ""
		if len(files) > 1 {
			suffix = fmt.Sprintf("_p%d", number)
		}
		p := step.Output.Allocate("eps", suffix)
		if p == "" {
			continue
		}
		if err := move(f.path, p); err != nil {
			return nil, err
		}
		written = append(written, p)
	}
	return written, nil
}

// PngArgs are the device options of EPS/PS → PNG: transparent where the
// drawing leaves the page empty, at the preset's DPI, EPS cropped to its bounding box.
func PngArgs(dpi int, eps bool) []string {
	a := []string{"-sDEVICE=pngalpha", fmt.Sprintf("-r%d", min(max(dpi, 36), 1200)), "-dTextAlphaBits=4", "-dGraphicsAlphaBits=4"}
	if eps {
		a = append(a, "-dEPSCrop")
	}
	return a
}

// toPng renders every page (_p{n} for several) and applies the preset's picture edits.
func toPng(ctx context.Context, gs string, step *convert.Step, progress convert.Progress) ([]string, error) {
	scratch, err := newScratch(step.WorkDir)
	if err != nil {
		return nil, err
	}
	if err := run(ctx, gs, PngArgs(step.Preset.Pdf.RenderDpi, step.From == "eps"), filepath.Join(scratch, "page-%d.png"), step.InputPath, progress); err != nil {
		return nil, err
	}
	files := numbered(scratch, "page-", ".png")
	if len(files) == 0 {
		return nil, errors.New("Ghostscript wrote no PNG file.")
	}
	var written []string
	for i, f := range files {
		suffix := ""
		if len(files) > 1 {
			suffix = fmt.Sprintf("_p%d", i+1)
		}
		p := step.Output.Allocate("png", suffix)
		if p == "" {
			continue
		}
		if err := editPng(f.path, p, &step.Preset.Image); err != nil {
			return nil, err
		}
		written = append(written, p)
	}
	return written, nil
}

func editPng(from, to string, o *presets.ImageOptions) error {
	if !o.EditsPicture() && !o.TurnsOrMirrors() {
		return move(from, to)
	}
	f, err := os.Open(from)
	if err != nil {
		return err
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		return err
	}
	var edited image.Image = media.EditPicture(img, o)
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if err := png.Encode(out, edited); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

type numberedFile struct {
	path  string
	index int
}

func numbered(dir, prefix, ext string) []numberedFile {
	entries, _ := os.ReadDir(dir)
	var out []numberedFile
	for _, e := range entries {
		n := e.Name()
		if !strings.HasPrefix(n, prefix) || !strings.HasSuffix(n, ext) {
			continue
		}
		if i, err := strconv.Atoi(n[len(prefix) : len(n)-len(ext)]); err == nil {
			out = append(out, numberedFile{filepath.Join(dir, n), i})
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].index < out[b].index })
	return out
}

// selectedPages is the preset's page range as zero-based pages, or nil for
// every page (also when the PDF can't be read: all pages rather than fail).
func selectedPages(step *convert.Step) []int {
	if strings.TrimSpace(step.Preset.Pdf.PageRange) == "" || step.From != "pdf" {
		return nil
	}
	n, err := api.PageCountFile(context.Background(), step.InputPath)
	if err != nil {
		return nil
	}
	return convert.ParsePageRange(step.Preset.Pdf.PageRange, n)
}

// Args is the whole command line: safe mode, the device, the output, the input.
func Args(device []string, outputFile, input string) []string {
	abs, err := filepath.Abs(input)
	if err != nil {
		abs = input
	}
	a := append([]string{"-dSAFER", "-dBATCH", "-dNOPAUSE", "-dQUIET"}, device...)
	return append(a, "-sOutputFile="+outputFile, "-f", abs)
}

// run runs Ghostscript in safe mode. Outputs go to an ASCII-named scratch
// folder first: in -sOutputFile a '%' is a page-number placeholder, so user
// paths can't be passed there.
func run(ctx context.Context, gs string, device []string, outputFile, input string, progress convert.Progress) error {
	est := tools.StartEstimate(progress, 2*time.Second, 0.1, 0.9)
	res, err := tools.Run(ctx, tools.Cmd{Path: gs, Args: Args(device, outputFile, input), Env: cleanEnv, Timeout: timeout, LowPriority: true})
	est.Stop()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	produced := false
	if strings.Contains(filepath.Base(outputFile), "%") {
		entries, _ := os.ReadDir(filepath.Dir(outputFile))
		produced = len(entries) > 0
	} else if st, e := os.Stat(outputFile); e == nil && st.Size() > 0 {
		produced = true
	}
	if err != nil || !produced {
		code := -1
		detail := ""
		if res != nil {
			code = res.ExitCode
			detail = lastLines(string(res.Stderr) + "\n" + string(res.Stdout))
		}
		var t *tools.TimeoutError
		if errors.As(err, &t) {
			return err
		}
		var missing *tools.RuntimeMissingError
		if errors.As(err, &missing) {
			return convert.Refuse(tools.RuntimeMissingKey, missing.Error())
		}
		return fmt.Errorf("%s", strings.TrimSpace(fmt.Sprintf("Ghostscript failed (exit %d). %s", code, detail)))
	}
	return nil
}

// lastLines is the end of Ghostscript's messages: the actual error is at the bottom.
func lastLines(text string) string {
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	return strings.Join(lines, " ")
}

func newScratch(workDir string) (string, error) {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	s := filepath.Join(workDir, "gs-"+hex.EncodeToString(b))
	return s, os.MkdirAll(s, 0o755)
}

func move(from, to string) error {
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

var (
	_ convert.Converter        = (*Converter)(nil)
	_ convert.ImageTransformer = (*Converter)(nil)
)
