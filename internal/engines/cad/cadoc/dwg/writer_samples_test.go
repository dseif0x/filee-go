package dwg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// TestWriterCheckerOnSamples runs the structural checks of the writer tests
// on DWG files written by AutoCAD, to make sure the checks themselves read
// real files. Set FILEE_DWG_SAMPLES to a directory with sample_AC*.dwg.
func TestWriterCheckerOnSamples(t *testing.T) {
	dir := os.Getenv("FILEE_DWG_SAMPLES")
	if dir == "" {
		t.Skip("FILEE_DWG_SAMPLES not set")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "sample_AC1*.dwg"))
	for _, p := range files {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(filepath.Base(p), func(t *testing.T) {
			f := &wtFile{sections: map[string][]byte{}}
			f.version = writerTestParseVersion(data)
			if f.version == 0 {
				t.Skip("unsupported")
			}
			if f.version >= 25 {
				wtParseAC18(t, f, data)
			} else {
				wtParseAC15(t, f, data)
			}
			f.handles = wtParseHandles(t, f.sections[writerSecHandles])
			wtCheckObjects(t, f)
			t.Logf("%d sections, %d objects", len(f.sections), len(f.objects))
		})
	}
}

func writerTestParseVersion(data []byte) (v cadoc.Version) {
	switch string(data[:6]) {
	case "AC1014":
		return 21
	case "AC1015":
		return 23
	case "AC1018":
		return 25
	case "AC1024":
		return 29
	case "AC1027":
		return 31
	case "AC1032":
		return 33
	}
	return 0
}
