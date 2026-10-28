package libreoffice

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/testutil"
)

func TestEdges(t *testing.T) {
	edges := BuildEdges(false, true, false, false, false)
	cost := func(from, to string) int {
		for _, e := range edges {
			if e.From == from && e.To == to {
				return e.Cost
			}
		}
		return -1
	}
	if cost("doc", "docx") != 25 || cost("pptx", "pdf") != 1 || cost("docx", "pdf") != 25 || cost("txt", "pdf") != 28 || cost("hwp", "pdf") != -1 {
		t.Fatal("costs")
	}
	if c := BuildEdges(true, false, true, true, true); !slices.ContainsFunc(c, func(e convert.Edge) bool { return e.From == "hwpx" && e.To == "pdf" && e.Cost == 1 }) {
		t.Fatal("hwp edges")
	}
	for _, e := range edges {
		if e.To == "svg" && !e.NotForImageSources {
			t.Fatal("svg edges must skip image sources")
		}
	}
}

func TestFilters(t *testing.T) {
	cases := map[[2]string]string{
		{"xlsx", "pdf"}: "pdf:calc_pdf_Export", {"docx", "pdf"}: "pdf:writer_pdf_Export", {"pptx", "svg"}: "svg:impress_svg_Export",
		{"cdr", "png"}: "png:draw_png_Export", {"cgm", "odg"}: "odg:impress8_draw", {"xls", "html"}: "html:HTML (StarCalc)",
		{"xlsx", "csv"}: "csv:Text - txt - csv (StarCalc):44,34,76",
	}
	for k, want := range cases {
		if got := FilterFor(k[0], k[1], false, k[0]); got != want {
			t.Errorf("%v: %s", k, got)
		}
	}
	if FamilyOf("sda", "sda") != "draw" || FamilyOf("sda", "sdd") != "impress" {
		t.Error("sda")
	}
	if !strings.Contains(FilterFor("odt", "pdf", true, "odt"), `"SelectPdfVersion"`) {
		t.Error("pdf/a")
	}
}

func TestRepack(t *testing.T) {
	dir := t.TempDir()
	in := testutil.Zip(t, filepath.Join(dir, "a.odt"), "content.xml", "<x/>", "mimetype", "application/vnd.oasis.opendocument.text", "content.xml", "<dup/>")
	if !IsPackage(in) {
		t.Fatal("package")
	}
	out := filepath.Join(dir, "b.odt")
	if err := Repack(in, out); err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	if len(z.File) != 2 || z.File[0].Name != "mimetype" || z.File[0].Method != zip.Store || z.File[0].Flags&0x8 != 0 || z.File[1].Flags&0x8 != 0 {
		t.Fatalf("entries %+v", z.File)
	}
	if got := testutil.ReadZip(t, out)["content.xml"]; got != "<x/>" {
		t.Fatal(got)
	}
}

func TestMissingFonts(t *testing.T) {
	p := testutil.Zip(t, filepath.Join(t.TempDir(), "f.docx"),
		"word/document.xml", `<w:document xmlns:w="w"><w:r><w:rPr><w:rFonts w:ascii="Calibri" w:eastAsia="+mn-ea" w:hAnsi="Fancy Font"/></w:rPr></w:r></w:document>`,
		"word/fontTable.xml", `<w:fonts xmlns:w="w"><w:font w:name="Embedded One"><w:embedRegular/></w:font></w:fonts>`,
		"word/styles.xml", `<w:styles xmlns:w="w"><w:rFonts w:ascii="Embedded One" w:cs="Symbol"/></w:styles>`)
	got := missingFonts(p, func(f string) bool { return f == "Carlito" })
	if !slices.Equal(got, []string{"Fancy Font"}) {
		t.Fatalf("%v", got)
	}
}

// A stand-in soffice that "converts" by copying the input under the target extension.
func TestConvertWithFakeSoffice(t *testing.T) {
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
	c := New()
	c.profiles = filepath.Join(dir, "profiles")
	if !c.Status().Available {
		t.Fatal("fake soffice not found")
	}
	in := filepath.Join(dir, "x.doc")
	os.WriteFile(in, []byte("doc"), 0o644)
	out, err := c.Convert(context.Background(), testutil.Step(t, in, "doc", "odt"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(out[0]); string(b) != "doc" || filepath.Ext(out[0]) != ".odt" {
		t.Fatalf("%v", out)
	}
}

func TestRealLibreOffice(t *testing.T) {
	if Locate() == "" {
		t.Skip("LibreOffice is not installed")
	}
}
