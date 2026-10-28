package ebook

import (
	"archive/zip"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/docxwriter"
)

// The e-book readers feed the DOCX writer through the document reader registry.
func TestEbooksBecomeDocxThroughTheDocumentReaders(t *testing.T) {
	dir := t.TempDir()
	for _, c := range [][2]string{{sampleEpub(t, dir), "epub"}, {sampleFb2(t, dir), "fb2"}} {
		s := step(t, c[0], c[1], "docx", nil)
		out, err := docxwriter.New().Convert(bg, s, nil)
		if err != nil {
			t.Fatalf("%s: %v", c[1], err)
		}
		z, err := zip.OpenReader(out[0])
		if err != nil {
			t.Fatal(err)
		}
		var body string
		media := 0
		for _, f := range z.File {
			if f.Name == "word/document.xml" {
				body = string(readAllZip(t, f))
			}
			if strings.HasPrefix(f.Name, "word/media/") {
				media++
			}
		}
		z.Close()
		want := map[string]string{"epub": "밤하늘에", "fb2": "Глава первая"}[c[1]]
		if !strings.Contains(body, want) {
			t.Errorf("%s: DOCX lacks %q", c[1], want)
		}
		if c[1] == "epub" && media != 1 {
			t.Errorf("epub: %d pictures", media)
		}
	}
}
