package calibre

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/presets"
)

func TestEdges(t *testing.T) {
	c := New()
	p := convert.NewPlanner([]convert.Converter{c}, nil)
	for _, e := range c.Edges() {
		if formats.ByID(e.From) == nil || formats.ByID(e.To) == nil {
			t.Errorf("unknown format in %s → %s", e.From, e.To)
		}
	}
	if r := p.Plan("epub", "azw3", convert.Options{}); r == nil || r.Cost != 20 {
		t.Fatalf("%+v", r)
	}
	if r := p.Plan("lit", "pdf", convert.Options{}); r == nil || len(r.Steps) != 1 || r.Cost != PdfCost {
		t.Fatalf("%+v", r)
	}
	if got := OutputOptions("pdf"); len(got) != 3 || got[1] != "a4" {
		t.Fatal(got)
	}
	var got []float64
	reportProgress("  34% Running transforms on e-book...", func(f float64) { got = append(got, f) })
	reportProgress("no percent here", func(f float64) { got = append(got, f) })
	if len(got) != 1 || got[0] < 0.32 || got[0] > 0.33 {
		t.Fatal(got)
	}
	if lastLines("a\n\nb\nc\nd\n") != "b c d" {
		t.Fatal(lastLines("a\nb\nc\nd"))
	}
}

func TestConvertWithCalibre(t *testing.T) {
	if Locate() == "" {
		t.Skip("calibre (ebook-convert) is not installed")
	}
	dir := t.TempDir()
	txt := filepath.Join(dir, "book.txt")
	os.WriteFile(txt, []byte("Hello calibre"), 0o644)
	out := filepath.Join(dir, "book.epub")
	// Calibre reads TXT too; the engine only needs an input it names by extension.
	s := &convert.Step{InputPath: txt, From: "txt", To: "epub", Preset: presets.New("x", "epub"), WorkDir: t.TempDir(),
		Output: convert.AllocatorFunc(func(string, string) string { return out })}
	if _, err := New().Convert(context.Background(), s, nil); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(out); err != nil || st.Size() == 0 {
		t.Fatal(err)
	}
}
