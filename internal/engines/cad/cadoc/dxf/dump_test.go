package dxf

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// TestDumpSamples writes sample files for external validators when
// DXF_DUMP_DIR is set.
func TestDumpSamples(t *testing.T) {
	dir := os.Getenv("DXF_DUMP_DIR")
	if dir == "" {
		t.Skip("DXF_DUMP_DIR not set")
	}
	for _, v := range writeVersions {
		for _, bin := range []bool{false, true} {
			doc := basicDoc(v)
			var buf bytes.Buffer
			if err := Write(&buf, doc, WriteConfig{Binary: bin}, nil); err != nil {
				t.Fatal(err)
			}
			name := "basic_" + v.String()
			if bin {
				name += "_bin"
			}
			if err := os.WriteFile(filepath.Join(dir, name+".dxf"), buf.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	empty := cadoc.NewDocument()
	var buf bytes.Buffer
	if err := Write(&buf, empty, WriteConfig{}, nil); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "empty_AC1032.dxf"), buf.Bytes(), 0o644)
}

// TestConvertSamples reads the DXF files named in DXF_SAMPLES (a glob) and
// writes them back to DXF_DUMP_DIR as AC1032 and AC1018.
func TestConvertSamples(t *testing.T) {
	pattern, dir := os.Getenv("DXF_SAMPLES"), os.Getenv("DXF_DUMP_DIR")
	if pattern == "" || dir == "" {
		t.Skip("DXF_SAMPLES / DXF_DUMP_DIR not set")
	}
	files, _ := filepath.Glob(pattern)
	for _, f := range files {
		counts := map[Level]int{}
		var first []string
		doc, err := ReadFile(f, ReadConfig{Failsafe: true, CreateDefaults: true}, func(l Level, m string) {
			counts[l]++
			if l >= LevelWarning && len(first) < 5 {
				first = append(first, m)
			}
		})
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		n := 0
		for _, r := range doc.BlockRecords.Entries() {
			n += len(r.Entities)
		}
		t.Logf("%s: %s ms=%d all=%d notes=%v %q", filepath.Base(f), doc.Header.Version, len(doc.ModelSpace().Entities), n, counts, first)
		for _, v := range []cadoc.Version{cadoc.AC1032, cadoc.AC1018} {
			doc.Header.SetVersion(v)
			var buf bytes.Buffer
			if err := Write(&buf, doc, WriteConfig{}, nil); err != nil {
				t.Errorf("%s: write %s: %v", f, v, err)
				continue
			}
			if err := structureErrors(buf.Bytes()); err != nil {
				t.Errorf("%s: %s: %v", f, v, err)
			}
			out := filepath.Join(dir, "conv_"+v.String()+"_"+filepath.Base(f))
			_ = os.WriteFile(out, buf.Bytes(), 0o644)
		}
	}
}
