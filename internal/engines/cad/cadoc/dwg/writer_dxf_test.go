package dwg

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc/dxf"
)

// writerTestDXFFiles lists the DXF samples: the writer_* files of testdata
// and, with FILEE_DWG_SAMPLES set, the sample_*_ascii.dxf files there.
func writerTestDXFFiles(t *testing.T) []string {
	files, _ := filepath.Glob(filepath.Join("testdata", "writer_*.dxf*"))
	if dir := os.Getenv("FILEE_DWG_SAMPLES"); dir != "" {
		more, _ := filepath.Glob(filepath.Join(dir, "sample_AC1*_ascii.dxf"))
		files = append(files, more...)
	}
	return files
}

func writerTestReadDXF(t *testing.T, path string) *cadoc.Document {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(path, ".gz") {
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if data, err = io.ReadAll(zr); err != nil {
			t.Fatal(err)
		}
	}
	doc, err := dxf.ReadBytes(data, dxf.ReadConfig{}, nil)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return doc
}

func TestWriterDXFSamples(t *testing.T) {
	for _, path := range writerTestDXFFiles(t) {
		for _, v := range writerTestVersions {
			t.Run(filepath.Base(path)+"/"+v.String(), func(t *testing.T) {
				doc := writerTestReadDXF(t, path)
				data, notes := writerTestWrite(t, doc, v, WriteConfig{})
				f := wtParse(t, data)
				missing := 0
				for h, name := range writerTestExpected(t, doc, v) {
					if _, ok := f.handles[h]; !ok {
						missing++
						if missing < 10 {
							t.Errorf("%s %X not in the handle map", name, h)
						}
					}
				}
				var models int
				for _, e := range doc.Entities() {
					if _, ok := f.handles[e.Base().Handle]; ok {
						models++
					} else {
						t.Logf("not written: %s %X", e.ObjectName(), e.Base().Handle)
					}
				}
				t.Logf("%d bytes, %d objects, %d of %d model space entities, %d notes", len(data), len(f.handles),
					models, len(doc.Entities()), len(notes))
				for i, n := range notes {
					if i < 5 {
						t.Logf("note: %s", n)
					}
				}
			})
		}
	}
}
