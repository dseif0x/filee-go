package dwg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// TestWriterRewriteDWGSamples reads the AutoCAD sample DWGs of
// FILEE_DWG_SAMPLES, writes them in every version and reads them back.
func TestWriterRewriteDWGSamples(t *testing.T) {
	dir := os.Getenv("FILEE_DWG_SAMPLES")
	if dir == "" {
		t.Skip("FILEE_DWG_SAMPLES not set")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "sample_AC1*.dwg"))
	for _, p := range files {
		for _, v := range writerTestVersions {
			t.Run(filepath.Base(p)+"/"+v.String(), func(t *testing.T) {
				doc, err := ReadFile(p, ReadConfig{}, nil)
				if err != nil {
					t.Skipf("read: %v", err)
				}
				data, notes := writerTestWrite(t, doc, v, WriteConfig{})
				wtParse(t, data)
				w := &writer{doc: doc, version: v}
				want := writerTestSummarize(doc, func(e cadoc.Entity) bool { ok, _ := w.entitySupported(e); return ok })
				back, err := ReadBytes(data, ReadConfig{}, nil)
				if err != nil {
					t.Fatalf("read back: %v", err)
				}
				writerTestCompare(t, want, writerTestSummarize(back, nil))
				t.Logf("%d bytes, %d notes", len(data), len(notes))
			})
		}
	}
}
