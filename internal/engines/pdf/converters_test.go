package pdf

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/presets"
)

// zipParts reads every part of a ZIP package and checks that XML parts are well-formed.
func zipParts(t *testing.T, path string) map[string]string {
	t.Helper()
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	parts := map[string]string{}
	for _, f := range z.File {
		r, _ := f.Open()
		data, _ := io.ReadAll(r)
		r.Close()
		parts[f.Name] = string(data)
		if strings.HasSuffix(f.Name, ".xml") || strings.HasSuffix(f.Name, ".rels") {
			d := xml.NewDecoder(bytes.NewReader(data))
			for {
				if _, err := d.Token(); err == io.EOF {
					break
				} else if err != nil {
					t.Fatalf("%s: %v", f.Name, err)
				}
			}
		}
	}
	return parts
}

func tablePDF(t *testing.T, pages int) string {
	var list []testPage
	for p := 0; p < pages; p++ {
		var c strings.Builder
		xs := []float64{72, 200, 330}
		ys := []float64{700, 680, 660, 640}
		for _, x := range xs {
			fmt.Fprintf(&c, "0.5 w %g %g m %g %g l S\n", x, ys[0], x, ys[len(ys)-1])
		}
		for _, y := range ys {
			fmt.Fprintf(&c, "0.5 w %g %g m %g %g l S\n", xs[0], y, xs[len(xs)-1], y)
		}
		cells := [][]string{{"Item", "Amount"}, {fmt.Sprintf("Apples %d", p), "1,234.50"}, {"Pears", "2026-10-23"}}
		for r, row := range cells {
			for col, s := range row {
				c.WriteString(text("F1", 10, xs[col]+4, ys[r]-14, s))
			}
		}
		list = append(list, testPage{W: 595, H: 842, Content: c.String()})
	}
	return buildPDF(t, list...)
}

func TestPdfTextOutputs(t *testing.T) {
	src := buildPDF(t, testPage{W: 595, H: 842, Content: text("F2", 20, 72, 760, "Heading") +
		body(72, 720, "First paragraph of the text.") + body(72, 690, "Second paragraph.") +
		"q 100 0 0 80 300 500 cm 0 0 1 rg 0 0 1 1 re f Q"})
	step := newStep(t, src, "pdf", "txt", nil)
	out, err := PdfText{}.Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out[0])
	if string(data) != "\uFEFFHeading\r\n\r\nFirst paragraph of the text.\r\n\r\nSecond paragraph.\r\n" {
		t.Fatalf("%q", data)
	}

	step = newStep(t, src, "pdf", "md", nil)
	out, err = PdfText{}.Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(out[0])
	if !strings.HasPrefix(string(data), "# Heading\n\nFirst paragraph of the text.\n\nSecond paragraph.\n") {
		t.Fatalf("%q", data)
	}

	step = newStep(t, src, "pdf", "docx", nil)
	out, err = PdfText{}.Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	parts := zipParts(t, out[0])
	doc := parts["word/document.xml"]
	if !strings.Contains(doc, "First paragraph of the text.") || !strings.Contains(doc, `<w:pStyle w:val="Heading1"/>`) || !strings.Contains(doc, `w:w="11900"`) {
		t.Fatalf("document.xml: %s", doc)
	}

	scan := buildPDF(t, testPage{W: 300, H: 300, Content: "0 0 1 rg 50 50 200 200 re f"})
	if _, err := (PdfText{}).Convert(context.Background(), newStep(t, scan, "pdf", "txt", nil), nil); err != ErrNoText {
		t.Fatalf("expected no text, got %v", err)
	}
	step = newStep(t, scan, "pdf", "md", nil)
	out, err = PdfText{}.Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(out[0])
	if !strings.Contains(string(data), "![](<test1_files/img0001.jpg>)") {
		t.Fatalf("%q", data)
	}
	if _, err := os.Stat(strings.TrimSuffix(out[0], ".md") + "_files/img0001.jpg"); err != nil {
		t.Fatal(err)
	}
	step = newStep(t, scan, "pdf", "docx", nil)
	out, err = PdfText{}.Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	if parts := zipParts(t, out[0]); !strings.Contains(parts["word/document.xml"], `behindDoc="1"`) || parts["word/media/image1.jpg"] == "" {
		t.Fatal("the scan should lie behind the page")
	}
}

func TestPdfTables(t *testing.T) {
	src := tablePDF(t, 2)
	step := newStep(t, src, "pdf", "xlsx", nil)
	out, err := PdfTables{}.Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenFile(out[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if sheets := f.GetSheetList(); len(sheets) != 1 || sheets[0] != "Table 1" {
		t.Fatalf("sheets %v", sheets)
	}
	rows, _ := f.GetRows("Table 1", excelize.Options{RawCellValue: true})
	// The table goes on over the second page; its repeated header is left out.
	if len(rows) != 5 || rows[1][1] != "1234.5" || rows[3][0] != "Apples 1" {
		t.Fatalf("rows %q", rows)
	}
	if v, _ := f.GetCellValue("Table 1", "B3"); v != "2026-10-23" {
		t.Fatalf("date shown as %q", v)
	}

	step = newStep(t, src, "pdf", "csv", nil)
	out, err = PdfTables{}.Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out[0])
	if !strings.Contains(string(data), "Apples 0,\"1,234.50\"\r\n") {
		t.Fatalf("%q", data)
	}

	step = newStep(t, src, "pdf", "ods", nil)
	out, err = PdfTables{}.Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	parts := zipParts(t, out[0])
	if !strings.Contains(parts["content.xml"], `office:value="1234.5"`) || !strings.Contains(parts["content.xml"], `office:date-value="2026-10-23"`) {
		t.Fatal(parts["content.xml"])
	}

	_, err = PdfTables{}.Convert(context.Background(), newStep(t, threePages(t), "pdf", "xlsx", nil), nil)
	if r, ok := err.(*convert.RefusedError); !ok || r.Key != "error.pdf_no_tables" {
		t.Fatalf("expected a refusal, got %v", err)
	}
}

func TestNumbersAndDates(t *testing.T) {
	for in, want := range map[string]string{"1,234.50": "1234.5 #,##0.00", "(120)": "-120 0;(0)", "$9.99": "9.99 \"$\"0.00", "12.5%": "0.125 0.0%", "-7": "-7 0"} {
		v, f, ok := parseNumber(in)
		if got := fmt.Sprintf("%v %s", v, f); !ok || got != want {
			t.Errorf("%s: %q", in, got)
		}
	}
	for _, in := range []string{"007", "1.234,5", "1234567890123456", "(12"} {
		if _, _, ok := parseNumber(in); ok {
			t.Errorf("%s should stay text", in)
		}
	}
	if v, f, ok := parseDate("2026. 10. 23."); !ok || v != 46318 || f != "yyyy. mm. dd." {
		t.Errorf("%v %q", v, f)
	}
	if _, _, ok := parseDate("2026-02-30"); ok {
		t.Error("no such date")
	}
}

func TestPdfSlides(t *testing.T) {
	src := threePages(t)
	step := newStep(t, src, "pdf", "pptx", func(p *presets.Preset) { p.Pdf.PageRange = "1,3" })
	out, err := PdfSlides{}.Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	parts := zipParts(t, out[0])
	if parts["ppt/slides/slide2.xml"] == "" || parts["ppt/slides/slide3.xml"] != "" {
		t.Fatal("two slides expected")
	}
	if !strings.Contains(parts["ppt/notesSlides/notesSlide2.xml"], "Page three") {
		t.Fatal(parts["ppt/notesSlides/notesSlide2.xml"])
	}
	// A4 portrait slides.
	if !strings.Contains(parts["ppt/presentation.xml"], `<p:sldSz cx="7556500" cy="10693400"/>`) {
		t.Fatal(parts["ppt/presentation.xml"])
	}
}
