package data

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/formats"
)

func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// run converts path to target with the data engine and returns the output path or the error.
func run(t *testing.T, path, target string) (string, error) {
	t.Helper()
	from := formats.Detect(path).ID
	step := &convert.Step{
		InputPath: path, From: from, To: target, WorkDir: t.TempDir(),
		Output: convert.AllocatorFunc(func(ext, suffix string) string {
			return filepath.Join(filepath.Dir(path), "out-"+strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))+suffix+"."+ext)
		}),
	}
	outs, err := New().Convert(context.Background(), step, nil)
	if err != nil {
		return "", err
	}
	if len(outs) != 1 {
		t.Fatalf("outputs %v", outs)
	}
	return outs[0], nil
}

func convertText(t *testing.T, name, text, target string) string {
	t.Helper()
	out, err := run(t, writeFile(t, name, []byte(text)), target)
	if err != nil {
		t.Fatalf("%s → %s: %v", name, target, err)
	}
	b, _ := os.ReadFile(out)
	if target != "csv" && strings.HasPrefix(string(b), "\xEF\xBB\xBF") {
		t.Fatal("UTF-8 without BOM expected")
	}
	return strings.TrimPrefix(string(b), "\xEF\xBB\xBF")
}

func fail(t *testing.T, name, text, target string) string {
	t.Helper()
	out, err := run(t, writeFile(t, name, []byte(text)), target)
	if err == nil {
		t.Fatalf("%s → %s succeeded: %s", name, target, out)
	}
	return err.Error()
}

func eq(t *testing.T, want, got string) {
	t.Helper()
	if want != got {
		t.Fatalf("want:\n%q\ngot:\n%q", want, got)
	}
}

func has(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Fatalf("%q\ndoes not contain\n%q", s, sub)
	}
}

func hasNot(t *testing.T, s, sub string) {
	t.Helper()
	if strings.Contains(s, sub) {
		t.Fatalf("%q\ncontains\n%q", s, sub)
	}
}

// lines builds text from lines (a raw string literal stand-in for C#'s """).
func lines(l ...string) string { return strings.Join(l, "\n") }
