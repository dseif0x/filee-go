package markdown

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/testutil"
)

const sample = `---
title: "The Title"
---
# Heading *one*

Some **bold**, _italic_, ~~gone~~ and ` + "`code`" + ` &amp; \*stars\*.
Line two[^1].

1. first
2. second
   - nested

- [x] done

| A | B |
|---|--:|
| 1 | 2 |

> quoted

![pic](p.png){width=2cm}

[^1]: The note.
`

func TestReader(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "p.png"), []byte("x"), 0o644)
	d := Read(sample, dir, "")
	if d.Title != "The Title" {
		t.Fatalf("title %q", d.Title)
	}
	blocks := d.Sections[0].Blocks
	h := blocks[0].(*doc.Paragraph)
	if h.HeadingLevel != 1 || doc.PlainText(h.Inlines) != "Heading one" {
		t.Fatalf("heading %+v %q", h, doc.PlainText(h.Inlines))
	}
	if _, ok := h.Inlines[0].(*doc.Bookmark); !ok {
		t.Error("no heading bookmark")
	}
	p := blocks[1].(*doc.Paragraph)
	if got := doc.PlainText(p.Inlines); got != "Some bold, italic, gone and code & *stars*. Line two." {
		t.Errorf("text %q", got)
	}
	var note, bold bool
	for _, in := range p.Inlines {
		switch in := in.(type) {
		case *doc.Note:
			note = len(in.Blocks) == 1
		case *doc.Text:
			if in.Text == "bold" && doc.Val(in.Format.Bold, false) {
				bold = true
			}
		}
	}
	if !note || !bold {
		t.Errorf("note %v bold %v", note, bold)
	}
	var lists, tables, images int
	for _, b := range blocks {
		switch b := b.(type) {
		case *doc.Paragraph:
			if b.List != nil {
				lists++
			}
			for _, in := range b.Inlines {
				if im, ok := in.(*doc.Image); ok {
					images++
					if im.Width == nil || *im.Width != doc.FromMm(20) {
						t.Errorf("image width %v", im.Width)
					}
				}
			}
		case *doc.Table:
			tables++
			if len(b.Rows) != 2 || !b.Rows[0].Header {
				t.Errorf("table %+v", b)
			}
		}
	}
	if lists != 4 || tables != 1 || images != 1 {
		t.Errorf("lists %d tables %d images %d", lists, tables, images)
	}
}

func TestConvert(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "doc.md")
	os.WriteFile(in, []byte(sample), 0o644)
	step := testutil.Step(t, in, "md", "html")
	out, err := New().Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(out[0])
	html := string(b)
	if !strings.Contains(html, "<title>Heading one</title>") || !strings.Contains(html, "<strong>bold</strong>") || !strings.Contains(html, "<table>") {
		t.Errorf("html %s", html)
	}
	step = testutil.Step(t, in, "md", "txt")
	out, err = New().Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(out[0])
	txt := string(b)
	if !strings.HasPrefix(txt, "\xEF\xBB\xBFHeading one\n") || strings.Contains(txt, "**") || !strings.Contains(txt, "A\tB") {
		t.Errorf("txt %q", txt)
	}
}
