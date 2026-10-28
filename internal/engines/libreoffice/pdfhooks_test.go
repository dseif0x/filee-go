package libreoffice

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/pdf"
	"github.com/dseif0x/filee-go/internal/testutil"
	"github.com/dseif0x/filee-go/internal/tools"
)

// writePDF writes a PDF with one page per content stream.
func writePDF(t *testing.T, path string, contents ...string) {
	t.Helper()
	objs := []string{"", "", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"}
	var kids bytes.Buffer
	for _, c := range contents {
		objs = append(objs, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(c)+1, c))
		objs = append(objs, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", len(objs)))
		fmt.Fprintf(&kids, "%d 0 R ", len(objs))
	}
	objs[0] = "<< /Type /Catalog /Pages 2 0 R >>"
	objs[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids.String(), len(contents))
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The PDF engine's tools around LibreOffice: one page for PDF → SVG, empty
// pages of a Calc PDF removed (a stand-in soffice copies its input).
func TestPdfToolsAroundLibreOffice(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "soffice")
	os.WriteFile(script, []byte(`#!/bin/sh
while [ $# -gt 0 ]; do
  case "$1" in
    --convert-to) ext="${2%%:*}"; shift 2;;
    --outdir) out="$2"; shift 2;;
    -*) shift;;
    *) in="$1"; shift;;
  esac
done
name=$(basename "$in"); name="${name%.*}"
cp "$in" "$out/$name.$ext"
`), 0o755)
	t.Setenv("FILEE_LIBREOFFICE", script)
	tools.Forget()
	t.Cleanup(tools.Forget)
	c := New()
	c.profiles = filepath.Join(dir, "profiles")
	if !c.Status().Available {
		t.Fatal("fake soffice not found")
	}
	text := "BT /F1 12 Tf 20 100 Td (page) Tj ET"

	src := filepath.Join(dir, "three.pdf")
	writePDF(t, src, text, text, text)
	step := testutil.Step(t, src, "pdf", "svg")
	step.Preset.Pdf.PageRange = "2-3"
	out, err := c.Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := pdf.PageCount(out[0]); err != nil || n != 1 {
		t.Fatalf("SVG from %d pages: %v", n, err)
	}

	sheet := filepath.Join(dir, "sheet.xlsx") // the stand-in copies it: a "Calc PDF" with an empty page
	writePDF(t, sheet, text, "", text)
	step = testutil.Step(t, sheet, "xlsx", "pdf")
	var notices []string
	step.Notice = func(k string) { notices = append(notices, k) }
	out, err = c.Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := pdf.PageCount(out[0]); err != nil || n != 2 || len(notices) != 1 {
		t.Fatalf("%d pages, %v, notices %v", n, err, notices)
	}
}
