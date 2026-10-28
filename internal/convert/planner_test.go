package convert

import (
	"context"
	"testing"
)

type fake struct {
	id    string
	edges []Edge
}

func (f *fake) ID() string                                                 { return f.id }
func (f *fake) Name() string                                               { return f.id }
func (f *fake) Edges() []Edge                                              { return f.edges }
func (f *fake) MaxParallelism() int                                        { return 0 }
func (f *fake) Status() Status                                             { return Available("", "") }
func (f *fake) Convert(context.Context, *Step, Progress) ([]string, error) { return nil, nil }

func TestPlan(t *testing.T) {
	rhwp := &fake{"rhwp", []Edge{E("hwpx", "pdf")}}
	pdfium := &fake{"pdfium", []Edge{E("pdf", "png"), EC("pdf", "jpg", 12)}}
	magick := &fake{"magick", []Edge{E("png", "jpg"), E("jpg", "png"), E("png", "png"), {From: "png", To: "svg", ImageSourcesOnly: true}}}
	tess := &fake{"tesseract", []Edge{{From: "png", To: "pdf", Ocr: true}}}
	p := NewPlanner([]Converter{rhwp, pdfium, magick, tess}, nil)

	r := p.Plan("HWPX", "png", Options{})
	if r == nil || len(r.Steps) != 2 || r.Steps[0].Converter != rhwp || r.Steps[1].Converter != pdfium || r.Cost != 20 {
		t.Fatalf("hwpx→png = %+v", r)
	}
	if r := p.Plan("hwpx", "jpg", Options{}); r == nil || r.Cost != 22 || len(r.Steps) != 2 {
		t.Fatalf("hwpx→jpg = %+v", r)
	}
	if p.Plan("png", "png", Options{}) == nil || p.Plan("jpg", "jpg", Options{}) != nil {
		t.Fatal("self edges")
	}
	// Tracing only for raster sources.
	if p.Plan("pdf", "svg", Options{}) != nil || p.Plan("jpg", "svg", Options{}) == nil {
		t.Fatal("ImageSourcesOnly")
	}
	// OCR: jpg → png → (ocr) pdf → png.
	r = p.Plan("jpg", "png", Options{Ocr: true})
	if r == nil || len(r.Steps) != 3 || r.Steps[1].Converter != tess {
		t.Fatalf("ocr = %+v", r)
	}
	// Priority penalty flips a tie.
	a := &fake{"a", []Edge{E("x", "y")}}
	b := &fake{"b", []Edge{E("x", "y")}}
	p = NewPlanner([]Converter{a, b}, func(c Converter) int {
		if c == a {
			return 1
		}
		return 0
	})
	if r := p.Plan("x", "y", Options{}); r.Steps[0].Converter != b {
		t.Fatal("penalty")
	}
}
