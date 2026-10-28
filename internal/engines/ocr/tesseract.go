package ocr

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/install"
	"github.com/dseif0x/filee-go/internal/tools"
)

// TesseractSpec finds Tesseract: FILEE_TESSERACT, Filee's engines folder,
// PATH, Homebrew on macOS, the UB Mannheim installer's folder on Windows.
var TesseractSpec = tools.Spec{
	Names: []string{"tesseract"},
	Env:   "FILEE_TESSERACT",
	Dirs: map[string][]string{
		"darwin":  {"tesseract", "tesseract/bin", "/opt/homebrew/opt/tesseract/bin"},
		"linux":   {"tesseract", "tesseract/bin"},
		"windows": {"tesseract", "C:/Program Files/Tesseract-OCR", "C:/Program Files (x86)/Tesseract-OCR"},
	},
}

const tesseractTimeout = 10 * time.Minute

// tesseractRuns: a few pages at a time keep a desktop busy without using all
// memory on a long scan.
func tesseractRuns() int { return min(max(runtime.NumCPU(), 1), 4) }

// Tesseract turns scans and pictures into searchable PDFs.
type Tesseract struct{}

func NewTesseract() *Tesseract { return &Tesseract{} }

func (t *Tesseract) ID() string            { return "tesseract" }
func (t *Tesseract) Name() string          { return "Tesseract OCR" }
func (t *Tesseract) MaxParallelism() int   { return 1 } // its pages run in parallel
func (t *Tesseract) Edges() []convert.Edge { return ocrEdges() }
func locateTesseract() string              { return tools.Find(TesseractSpec) }
func (t *Tesseract) available() bool       { return locateTesseract() != "" }

func (t *Tesseract) Status() convert.Status {
	exe := locateTesseract()
	if exe == "" {
		return convert.Unavailable("engine.reason.not_installed", "tesseract")
	}
	version := ""
	if strings.HasPrefix(exe, tools.EnginesDir()) {
		version = install.Version("tesseract")
	}
	return convert.Available(exe, version)
}

func (t *Tesseract) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	exe := locateTesseract()
	if exe == "" {
		return nil, errors.New("Tesseract is not installed.")
	}
	lang, ok := FindLanguage(step.Preset.Pdf.OcrLanguage)
	if !ok {
		lang, _ = FindLanguage(DefaultLanguage)
	}
	data, err := EnsureLanguage(ctx, lang.Code)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, convert.Refuse("error.ocr_language_download", lang.NativeName()+": "+err.Error())
	}
	scratch, err := newScratch(step.WorkDir, "ocr-")
	if err != nil {
		return nil, err
	}
	tessdata := filepath.Join(scratch, "tessdata")
	if err := os.MkdirAll(tessdata, 0o755); err != nil {
		return nil, err
	}
	if err := copyFile(data, filepath.Join(tessdata, filepath.Base(data))); err != nil {
		return nil, err
	}
	r := &tesseractRun{exe, scratch, lang.Code}
	progress.Report(0.05)
	output := step.Output.Allocate("pdf", "")
	if output == "" {
		return nil, nil
	}
	if step.From == "pdf" {
		err = recognizePdf(ctx, step, scratch, output, tesseractRuns(), func(ctx context.Context, picture, base string) error {
			return r.toPdf(ctx, picture, base, Dpi)
		}, progress)
	} else {
		// A copy with a plain name: the picture's own folder and name may not be readable for Tesseract.
		picture := "picture" + strings.ToLower(filepath.Ext(step.InputPath))
		if err = copyFile(step.InputPath, filepath.Join(scratch, picture)); err == nil {
			dpi := Dpi
			if HasResolution(step.InputPath) {
				dpi = 0
			}
			if err = r.toPdf(ctx, picture, "page", dpi); err == nil {
				err = moveFile(filepath.Join(scratch, "page.pdf"), output)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{output}, nil
}

// tesseractRun is one language, one Tesseract, working in folder (with the
// language in its tessdata). Tesseract only ever sees plain relative names
// there: some Windows builds open files through the ANSI code page.
type tesseractRun struct {
	exe, folder, language string
}

// TesseractArgs read input and write base.pdf; dpi 0 trusts the picture's own resolution.
func TesseractArgs(input, base, language string, dpi int) []string {
	// tessedit_create_pdf instead of the "pdf" config file, which lives in Tesseract's own tessdata.
	a := []string{input, base, "--tessdata-dir", "tessdata", "-l", language, "-c", "tessedit_create_pdf=1"}
	if dpi > 0 {
		a = append(a, "--dpi", fmt.Sprint(dpi))
	}
	return a
}

func (r *tesseractRun) toPdf(ctx context.Context, input, base string, dpi int) error {
	res, err := tools.Run(ctx, tools.Cmd{
		Path: r.exe, Args: TesseractArgs(input, base, r.language, dpi), Dir: r.folder,
		// One thread per Tesseract: several pages run at a time. The system's TESSDATA_PREFIX is not used.
		Env:     []string{"OMP_THREAD_LIMIT=1", "TESSDATA_PREFIX="},
		Timeout: tesseractTimeout, LowPriority: true,
	})
	out := filepath.Join(r.folder, base+".pdf")
	if err != nil || !exists(out) {
		code := -1
		stderr := ""
		if res != nil {
			code, stderr = res.ExitCode, strings.TrimSpace(string(res.Stderr))
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var missing *tools.RuntimeMissingError
		if errors.As(err, &missing) {
			return convert.Refuse(tools.RuntimeMissingKey, missing.Error())
		}
		return fmt.Errorf("Tesseract failed (exit %d). %s", code, stderr)
	}
	return nil
}

var _ convert.Converter = (*Tesseract)(nil)
