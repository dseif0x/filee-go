// Package testutil has helpers for engine tests: zip fixtures and steps.
package testutil

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/presets"
)

// Zip writes a zip file with the entries in order (name, content pairs).
func Zip(t testing.TB, path string, entries ...string) string {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for i := 0; i+1 < len(entries); i += 2 {
		method := zip.Deflate
		if entries[i] == "mimetype" {
			method = zip.Store
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: entries[i], Method: method})
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, entries[i+1])
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return path
}

// ReadZip returns the entries of a zip file by name.
func ReadZip(t testing.TB, path string) map[string]string {
	t.Helper()
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	out := map[string]string{}
	for _, f := range z.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = string(b)
	}
	return out
}

// Step returns a step converting input (from → to) with outputs in dir,
// named after the input ("<name><suffix>.<ext>").
func Step(t testing.TB, input, from, to string) *convert.Step {
	t.Helper()
	dir := t.TempDir()
	base := filepath.Base(input)
	if i := strings.IndexByte(base, '.'); i > 0 {
		base = base[:i]
	}
	return &convert.Step{
		InputPath: input, From: from, To: to, Preset: presets.New("test", to), WorkDir: t.TempDir(),
		Output: convert.AllocatorFunc(func(ext, suffix string) string {
			if ext == "" {
				return filepath.Join(dir, base+suffix)
			}
			return filepath.Join(dir, base+suffix+"."+ext)
		}),
	}
}
