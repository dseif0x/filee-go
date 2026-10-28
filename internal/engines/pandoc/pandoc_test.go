package pandoc

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/testutil"
)

func TestEdges(t *testing.T) {
	p := convert.NewPlanner([]convert.Converter{New()}, nil)
	r := p.Plan("html", "rtf", convert.Options{})
	if r == nil || len(r.Steps) != 2 || r.Steps[0].To != "md" {
		t.Fatalf("html → rtf should go through Markdown: %+v", r)
	}
	if r := p.Plan("md", "pptx", convert.Options{}); r == nil || r.Cost != slidesCost {
		t.Fatal("slides")
	}
	for _, e := range New().Edges() {
		if _, ok := readers[e.From]; !ok {
			t.Errorf("no reader for %s", e.From)
		}
		if _, ok := writers[e.To]; !ok {
			t.Errorf("no writer for %s", e.To)
		}
	}
}

func TestArgumentsWithFakePandoc(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "pandoc")
	log := filepath.Join(dir, "args")
	os.WriteFile(script, []byte("#!/bin/sh\necho \"$@\" > "+log+"\nwhile [ $# -gt 0 ]; do if [ \"$1\" = -o ]; then echo out > \"$2\"; fi; shift; done\n"), 0o755)
	t.Setenv("FILEE_PANDOC", script)
	in := filepath.Join(dir, "a.docx")
	os.WriteFile(in, []byte("x"), 0o644)
	c := New()
	if !c.Status().Available {
		t.Fatal("not found")
	}
	out, err := c.Convert(context.Background(), testutil.Step(t, in, "docx", "md"), nil)
	if err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(log)
	for _, want := range []string{"-f docx", "-t gfm", "--lua-filter=", "--extract-media=a_files"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("args %q lack %q", args, want)
		}
	}
	if filepath.Ext(out[0]) != ".md" {
		t.Error(out)
	}
}

func TestRealPandoc(t *testing.T) {
	if Locate() == "" {
		t.Skip("Pandoc is not installed")
	}
}
