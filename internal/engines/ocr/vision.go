package ocr

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/dseif0x/filee-go/internal/convert"
)

// TextRecognizer reads the text of a picture: what Vision needs from Apple Vision.
type TextRecognizer interface {
	// Available is true when the recognizer can run on this computer.
	Available() bool
	// Languages it reads, as Vision identifiers (en-US, zh-Hans).
	Languages() []string
	// Recognize returns the lines of text in picture, in languages (most likely first).
	Recognize(ctx context.Context, picture string, languages []string) ([]Line, error)
}

// Vision is text recognition with Apple Vision on macOS: the same step as
// Tesseract with macOS's own recognizer, no download. Registered before
// Tesseract, so on a Mac it is the one the planner takes; a language Vision
// doesn't read goes to Tesseract when that is installed.
type Vision struct {
	recognizer TextRecognizer
	tesseract  convert.Converter
}

// NewVision returns the engine with the system's recognizer (nil off macOS).
func NewVision() *Vision { return &Vision{recognizer: systemRecognizer(), tesseract: NewTesseract()} }

// NewVisionWith returns the engine with recognizer and a Tesseract fallback (tests).
func NewVisionWith(r TextRecognizer, tesseract convert.Converter) *Vision {
	return &Vision{recognizer: r, tesseract: tesseract}
}

// visionRuns: Vision uses the Neural Engine or the GPU; a couple of pages at a time keep it busy.
func visionRuns() int { return min(max(runtime.NumCPU(), 1), 2) }

func (v *Vision) ID() string            { return "vision" }
func (v *Vision) Name() string          { return "Apple Vision" }
func (v *Vision) MaxParallelism() int   { return 1 }
func (v *Vision) Edges() []convert.Edge { return ocrEdges() }

func (v *Vision) Status() convert.Status {
	if v.recognizer == nil {
		return convert.Unavailable("engine.reason.unsupported_platform", "")
	}
	if !v.recognizer.Available() {
		return convert.Unavailable("engine.reason.not_installed", "Vision.framework")
	}
	return convert.Available(strings.Join(v.recognizer.Languages(), ", "), "macOS "+osVersion())
}

// Reads is true when Vision reads code (a Tesseract code) here, so it needs no download.
func (v *Vision) Reads(code string) bool {
	return v.recognizer != nil && v.recognizer.Available() && VisionLanguagesFor(code, v.recognizer.Languages()) != nil
}

func (v *Vision) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	if v.recognizer == nil || !v.recognizer.Available() {
		return nil, errors.New("Apple Vision text recognition runs on macOS only.")
	}
	lang, ok := FindLanguage(step.Preset.Pdf.OcrLanguage)
	if !ok {
		lang, _ = FindLanguage(DefaultLanguage)
	}
	langs := VisionLanguagesFor(lang.Code, v.recognizer.Languages())
	if langs == nil {
		if v.tesseract != nil && v.tesseract.Status().Available {
			return v.tesseract.Convert(ctx, step, progress)
		}
		return nil, convert.Refuse("error.ocr_needs_tesseract", lang.NativeName())
	}
	scratch, err := newScratch(step.WorkDir, "vision-")
	if err != nil {
		return nil, err
	}
	progress.Report(0.05)
	output := step.Output.Allocate("pdf", "")
	if output == "" {
		return nil, nil
	}
	if step.From == "pdf" {
		err = recognizePdf(ctx, step, scratch, output, visionRuns(), func(ctx context.Context, picture, base string) error {
			path := filepath.Join(scratch, picture)
			w, h, err := pngSize(path)
			if err != nil {
				return err
			}
			lines, err := v.recognize(ctx, path, langs)
			if err != nil {
				return err
			}
			var pw PdfWriter
			if err := pw.AddPage(path, points(w, Dpi), points(h, Dpi), lines); err != nil {
				return err
			}
			return pw.Save(filepath.Join(scratch, base+".pdf"))
		}, progress)
	} else {
		var pages []visionPicture
		if pages, err = picturePages(step.InputPath, step.From, scratch); err == nil {
			var pw PdfWriter
			for _, p := range pages {
				if err = ctx.Err(); err != nil {
					break
				}
				var lines []Line
				if lines, err = v.recognize(ctx, p.path, langs); err != nil {
					break
				}
				if err = pw.AddPage(p.path, points(p.width, p.dpi), points(p.height, p.dpi), lines); err != nil {
					break
				}
			}
			if err == nil {
				err = pw.Save(output)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{output}, nil
}

// recognize reads one picture; when Vision fails with English next to the
// language asked for, it reads again with that one alone.
func (v *Vision) recognize(ctx context.Context, picture string, langs []string) ([]Line, error) {
	lines, err := v.recognizer.Recognize(ctx, picture, langs)
	if err != nil && len(langs) > 1 && ctx.Err() == nil {
		return v.recognizer.Recognize(ctx, picture, langs[:1])
	}
	return lines, err
}

func points(pixels int, dpi float64) float64 { return float64(pixels) * 72 / dpi }

func pngSize(path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	c, err := png.DecodeConfig(f)
	if err != nil {
		return 0, 0, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return c.Width, c.Height, nil
}

// visionByCode maps Tesseract codes to the Vision identifiers they may go by, the usual one first.
var visionByCode = map[string][]string{
	"eng": {"en-US"}, "deu": {"de-DE"}, "fra": {"fr-FR"}, "spa": {"es-ES"}, "por": {"pt-BR"}, "ita": {"it-IT"},
	"nld": {"nl-NL"}, "pol": {"pl-PL"}, "ces": {"cs-CZ"}, "ron": {"ro-RO"}, "swe": {"sv-SE"}, "dan": {"da-DK"},
	"nor": {"nb-NO", "no-NO"}, "tur": {"tr-TR"}, "ind": {"id-ID"}, "msa": {"ms-MY"}, "rus": {"ru-RU"}, "ukr": {"uk-UA"},
	"ara": {"ar-SA"}, "tha": {"th-TH"}, "vie": {"vi-VT", "vi-VN"}, "kor": {"ko-KR"}, "jpn": {"ja-JP"},
	"chi_sim": {"zh-Hans"}, "chi_tra": {"zh-Hant"},
}

// VisionFallbackLanguages are what Vision reads on every macOS Filee runs on, when it can't be asked.
var VisionFallbackLanguages = []string{"en-US", "fr-FR", "it-IT", "de-DE", "es-ES", "pt-BR", "zh-Hans", "zh-Hant", "ko-KR", "ja-JP", "ru-RU", "uk-UA"}

// VisionLanguagesFor are the languages to ask Vision for when the text is in
// code: that language, then English (scans often have English words in
// them); nil when Vision doesn't read it.
func VisionLanguagesFor(code string, supported []string) []string {
	match := ""
	for _, c := range visionByCode[code] {
		for _, s := range supported {
			if strings.EqualFold(s, c) && match == "" {
				match = s
			}
		}
	}
	if match == "" {
		return nil
	}
	for _, s := range supported {
		if strings.EqualFold(s, "en-US") && s != match {
			return []string{match, s}
		}
	}
	return []string{match}
}

var _ convert.Converter = (*Vision)(nil)
