package ocr

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/klippa-app/go-pdfium/requests"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/presets"
)

func testPng(t *testing.T, dir, name string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{250, 250, uint8(200 + x%50), 255})
		}
	}
	p := filepath.Join(dir, name)
	f, _ := os.Create(p)
	png.Encode(f, img)
	f.Close()
	return p
}

func pageText(t *testing.T, path string, page int) string {
	t.Helper()
	d, err := openPdf(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	tp, err := d.inst.FPDFText_LoadPage(&requests.FPDFText_LoadPage{Page: d.page(page)})
	if err != nil {
		t.Fatal(err)
	}
	defer d.inst.FPDFText_ClosePage(&requests.FPDFText_ClosePage{TextPage: tp.TextPage})
	r, err := d.inst.GetPageText(&requests.GetPageText{Page: d.page(page)})
	if err != nil {
		t.Fatal(err)
	}
	return r.Text
}

func TestTextLayerStretchesWordsOverThePicture(t *testing.T) {
	l := EstimateLine("Hello  wide world", 0.1, 0.5, 0.8, 0.05)
	if len(l.Words) != 3 || l.Words[0].Text != "Hello" || l.Words[1].Text != "wide" {
		t.Fatal(l.Words)
	}
	if r := WordRanges("a bb  ccc"); !slices.Equal(r, [][2]int{{0, 1}, {2, 2}, {6, 3}}) {
		t.Error(r)
	}
	layer := TextLayer([]Line{l}, 600, 800)
	if !strings.HasPrefix(layer, "q\nBT\n3 Tr\n") || !strings.HasSuffix(layer, "ET\nQ\n") {
		t.Error(layer)
	}
	if !strings.Contains(layer, "/FOcr 40 Tf") || !strings.Contains(layer, "<00480065006C006C006F0020> Tj") {
		t.Error(layer)
	}
	if TextLayer(nil, 1, 1) != "" || TextLayer([]Line{{Height: 0, Words: []Word{{"x", 0, 1}}}}, 1, 1) != "" {
		t.Error("empty layers")
	}
	// Outside the BMP: two UTF-16 units, two codes.
	if !strings.Contains(TextLayer([]Line{{0, 0, 1, 0.1, []Word{{"😀", 0, 0.5}}}}, 100, 100), "<D83DDE00>") {
		t.Error("surrogates")
	}
}

func TestSearchablePdfRoundTrip(t *testing.T) {
	dir := t.TempDir()
	pic := testPng(t, dir, "scan.png", 300, 400)
	var w PdfWriter
	if err := w.AddPage(pic, 72, 96, []Line{EstimateLine("Already searchable", 0.1, 0.8, 0.8, 0.05)}); err != nil {
		t.Fatal(err)
	}
	if err := w.AddPage(pic, 72, 96, nil); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.pdf")
	if err := w.Save(out); err != nil {
		t.Fatal(err)
	}
	has, err := pagesWithText(out)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(has, []bool{true, false}) {
		t.Fatal(has)
	}
	if got := strings.TrimSpace(pageText(t, out, 0)); got != "Already searchable" {
		t.Errorf("%q", got)
	}
	if b, _ := os.ReadFile(out); strings.Count(string(b), "/FontFile2") != 1 {
		t.Error("the font is written once")
	}
	if HasResolution(pic) {
		t.Error("a PNG without pHYs has no resolution")
	}
}

type fakeRecognizer struct {
	langs []string
	calls [][]string
	fail  bool
}

func (f *fakeRecognizer) Available() bool     { return true }
func (f *fakeRecognizer) Languages() []string { return f.langs }
func (f *fakeRecognizer) Recognize(ctx context.Context, picture string, langs []string) ([]Line, error) {
	f.calls = append(f.calls, langs)
	if f.fail && len(langs) > 1 {
		return nil, errors.New("two languages")
	}
	return []Line{EstimateLine("Recognized words", 0.1, 0.5, 0.8, 0.1)}, nil
}

type stubConverter struct {
	convert.Converter
	available bool
	called    bool
}

func (s *stubConverter) Status() convert.Status { return convert.Status{Available: s.available} }
func (s *stubConverter) Convert(context.Context, *convert.Step, convert.Progress) ([]string, error) {
	s.called = true
	return []string{"tesseract"}, nil
}

func stepFor(dir, input, from string, preset *presets.Preset, out *string) *convert.Step {
	return &convert.Step{InputPath: input, From: from, To: "pdf", Preset: preset, WorkDir: dir,
		Output: convert.AllocatorFunc(func(ext, suffix string) string {
			*out = filepath.Join(dir, "result"+suffix+"."+ext)
			return *out
		})}
}

func TestVisionRecognizesOnlyThePagesWithoutText(t *testing.T) {
	dir := t.TempDir()
	pic := testPng(t, dir, "scan.png", 200, 260)
	var w PdfWriter
	w.AddPage(pic, 72, 94, []Line{EstimateLine("Already searchable", 0.1, 0.8, 0.8, 0.05)})
	w.AddPage(pic, 72, 94, nil)
	w.AddPage(pic, 72, 94, nil)
	src := filepath.Join(dir, "src.pdf")
	if err := w.Save(src); err != nil {
		t.Fatal(err)
	}
	r := &fakeRecognizer{langs: []string{"en-US", "de-DE"}, fail: true}
	v := NewVisionWith(r, &stubConverter{})
	p := presets.New("ocr", "pdf")
	p.Pdf.Ocr, p.Pdf.OcrLanguage = true, "deu"
	var out string
	got, err := v.Convert(context.Background(), stepFor(dir, src, "pdf", p, &out), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != out {
		t.Fatal(got)
	}
	has, err := pagesWithText(out)
	if err != nil || !slices.Equal(has, []bool{true, true, true}) {
		t.Fatal(has, err)
	}
	eq := func(want, got string) {
		if strings.TrimSpace(got) != want {
			t.Errorf("want %q, got %q", want, got)
		}
	}
	eq("Already searchable", pageText(t, out, 0))
	eq("Recognized words", pageText(t, out, 1))
	// German first, then English; read again with German alone after a failure.
	if len(r.calls) != 4 || !slices.Equal(r.calls[0], []string{"de-DE", "en-US"}) || !slices.Equal(r.calls[1], []string{"de-DE"}) {
		t.Error(r.calls)
	}

	// Every page with text: copied, with a note.
	note := ""
	s := stepFor(dir, out, "pdf", p, &out)
	s.Output = convert.AllocatorFunc(func(ext, suffix string) string { return filepath.Join(dir, "again.pdf") })
	s.Notice = func(k string) { note = k }
	if _, err := v.Convert(context.Background(), s, nil); err != nil || note != "notice.ocr_has_text" {
		t.Error(note, err)
	}

	// A page range: only those pages.
	p.Pdf.PageRange = "2"
	var ranged string
	if _, err := v.Convert(context.Background(), stepFor(dir, src, "pdf", p, &ranged), nil); err != nil {
		t.Fatal(err)
	}
	if has, _ := pagesWithText(ranged); !slices.Equal(has, []bool{true}) {
		t.Error(has)
	}
}

func TestVisionPicturesAndLanguages(t *testing.T) {
	dir := t.TempDir()
	pic := testPng(t, dir, "photo.png", 120, 90)
	r := &fakeRecognizer{langs: []string{"en-US", "ja-JP"}}
	v := NewVisionWith(r, &stubConverter{})
	p := presets.New("ocr", "pdf")
	p.Pdf.Ocr, p.Pdf.OcrLanguage = true, "jpn"
	var out string
	if _, err := v.Convert(context.Background(), stepFor(dir, pic, "png", p, &out), nil); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(pageText(t, out, 0)) != "Recognized words" {
		t.Error("no text")
	}
	// A language Vision doesn't read goes to Tesseract, or is refused without it.
	p.Pdf.OcrLanguage = "hin"
	tess := &stubConverter{available: true}
	if got, err := NewVisionWith(r, tess).Convert(context.Background(), stepFor(dir, pic, "png", p, &out), nil); err != nil || !tess.called || got[0] != "tesseract" {
		t.Error(got, err)
	}
	_, err := NewVisionWith(r, &stubConverter{}).Convert(context.Background(), stepFor(dir, pic, "png", p, &out), nil)
	var refused *convert.RefusedError
	if !errors.As(err, &refused) || refused.Key != "error.ocr_needs_tesseract" || refused.Detail != "हिन्दी" {
		t.Error(err)
	}
	if !slices.Equal(VisionLanguagesFor("chi_sim", []string{"zh-Hans", "en-US"}), []string{"zh-Hans", "en-US"}) ||
		!slices.Equal(VisionLanguagesFor("eng", []string{"en-US"}), []string{"en-US"}) ||
		VisionLanguagesFor("nor", []string{"en-US"}) != nil ||
		!slices.Equal(VisionLanguagesFor("nor", []string{"no-NO"}), []string{"no-NO"}) {
		t.Error("language mapping")
	}
	off := NewVisionWith(nil, nil)
	if off.Status().ReasonKey != "engine.reason.unsupported_platform" || off.Reads("eng") {
		t.Error("off")
	}
}

func TestLanguageList(t *testing.T) {
	all := Languages()
	if len(all) != 69 || all[0].Code != "eng" || all[1].Code != "deu" {
		t.Fatal(len(all), all[:2])
	}
	de, _ := FindLanguage("deu")
	if de.NativeName() != "Deutsch" {
		t.Error(de.NativeName())
	}
	ko, _ := FindLanguage("kor")
	if ko.NativeName() != "한국어" {
		t.Error(ko.NativeName())
	}
	c := LanguageComponents()["eng"]
	if c.URL != "https://raw.githubusercontent.com/tesseract-ocr/tessdata_fast/4.1.0/eng.traineddata" || c.Kind != "traineddata" || len(c.Sha256) != 64 {
		t.Error(c)
	}
	if _, err := EnsureLanguage(context.Background(), "klingon"); err == nil {
		t.Error("unknown language")
	}
	t.Setenv("FILEE_ENGINES_DIR", t.TempDir())
	if IsLanguageInstalled("eng") {
		t.Error("installed in an empty folder")
	}
}

func TestTesseract(t *testing.T) {
	a := TesseractArgs("p3.png", "p3", "kor", 300)
	if strings.Join(a, " ") != "p3.png p3 --tessdata-dir tessdata -l kor -c tessedit_create_pdf=1 --dpi 300" {
		t.Error(a)
	}
	if slices.Contains(TesseractArgs("x.png", "page", "eng", 0), "--dpi") {
		t.Error("dpi of a picture that has one")
	}
	e := NewTesseract().Edges()
	if len(e) != 7 || !e[0].Ocr || e[6].From != "pdf" {
		t.Error(e)
	}
	if locateTesseract() == "" {
		if NewTesseract().Status().Available {
			t.Error("available without the program")
		}
		t.Skip("Tesseract is not installed")
	}
}

func TestResolutions(t *testing.T) {
	dir := t.TempDir()
	// A PNG with pHYs at 11811 px/m (300 dpi).
	p := testPng(t, dir, "a.png", 4, 4)
	data, _ := os.ReadFile(p)
	phys := []byte{0, 0, 0, 9, 'p', 'H', 'Y', 's', 0, 0, 0x2e, 0x23, 0, 0, 0x2e, 0x23, 1, 0, 0, 0, 0}
	withDpi := append(append(append([]byte{}, data[:33]...), phys...), data[33:]...)
	q := filepath.Join(dir, "dpi.png")
	os.WriteFile(q, withDpi, 0o644)
	if r := resolution(q); r < 299 || r > 301 {
		t.Error(r)
	}
	if orient(image.NewRGBA(image.Rect(0, 0, 3, 2)), 8).Bounds().Dx() != 2 {
		t.Error("orientation")
	}
}
