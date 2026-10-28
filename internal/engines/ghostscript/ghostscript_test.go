package ghostscript

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/presets"
)

func TestArguments(t *testing.T) {
	a := strings.Join(Args([]string{"-sDEVICE=pdfwrite"}, "/w/out.pdf", "/in/x.eps"), " ")
	if a != "-dSAFER -dBATCH -dNOPAUSE -dQUIET -sDEVICE=pdfwrite -sOutputFile=/w/out.pdf -f /in/x.eps" {
		t.Error(a)
	}
	if strings.Join(PngArgs(9999, true), " ") != "-sDEVICE=pngalpha -r1200 -dTextAlphaBits=4 -dGraphicsAlphaBits=4 -dEPSCrop" {
		t.Error(PngArgs(9999, true))
	}
	if PngArgs(10, false)[1] != "-r36" {
		t.Error("lowest dpi")
	}
	for l, want := range map[presets.PdfCompression]string{presets.PdfCompressScreen: "/screen", presets.PdfCompressEbook: "/ebook", presets.PdfCompressPrint: "/printer", presets.PdfCompressNone: "/ebook"} {
		if CompressSettings(l) != want {
			t.Error(l)
		}
	}
	if pageList([]int{0, 2, 3}) != "-sPageList=1,3,4" {
		t.Error(pageList([]int{0, 2, 3}))
	}
	if len(New().Edges()) != 6 || !New().Transforms("eps", "png") || New().Transforms("eps", "pdf") {
		t.Error("edges")
	}
	dir := t.TempDir()
	for _, n := range []string{"page-10.png", "page-2.png", "page-1.png", "other.png"} {
		os.WriteFile(filepath.Join(dir, n), nil, 0o644)
	}
	f := numbered(dir, "page-", ".png")
	if len(f) != 3 || f[0].index != 1 || f[2].index != 10 {
		t.Error(f)
	}
	if lastLines("a\n\nb\nc\nd\ne\nf\ng\n") != "b c d e f g" {
		t.Error(lastLines("a\nb"))
	}
}

const eps = `%!PS-Adobe-3.0 EPSF-3.0
%%BoundingBox: 0 0 100 50
newpath 10 10 moveto 90 40 lineto 0 0 1 setrgbcolor 4 setlinewidth stroke
showpage
`

func TestWithGhostscript(t *testing.T) {
	gs := Locate()
	if gs == "" {
		t.Skip("Ghostscript is not installed")
	}
	dir := t.TempDir()
	in := filepath.Join(dir, "line.eps")
	os.WriteFile(in, []byte(eps), 0o644)
	p := presets.New("p", "pdf")
	for _, to := range []string{"pdf", "png"} {
		var out string
		step := &convert.Step{InputPath: in, From: "eps", To: to, Preset: p, WorkDir: dir,
			Output: convert.AllocatorFunc(func(ext, suffix string) string { out = filepath.Join(dir, "out"+suffix+"."+ext); return out })}
		got, err := New().Convert(context.Background(), step, nil)
		if err != nil || len(got) != 1 {
			t.Fatal(to, got, err)
		}
		if st, err := os.Stat(out); err != nil || st.Size() == 0 {
			t.Error(to, "empty")
		}
	}
}
