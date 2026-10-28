package pdf

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// testPage is one page of a hand-built test PDF: its size and content stream
// (Helvetica is /F1, Helvetica-Bold /F2).
type testPage struct {
	W, H    float64
	Content string
	Rotate  int
	// Extra is added to the page dictionary (annotations).
	Extra string
}

// buildPDF writes a minimal valid PDF with the pages.
func buildPDF(t testing.TB, pages ...testPage) string {
	t.Helper()
	var objs []string
	add := func(s string) int { objs = append(objs, s); return len(objs) }
	catalog := add("") // filled later
	pagesObj := add("")
	f1 := add("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	f2 := add("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>")
	var kids []int
	for _, p := range pages {
		c := add(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(p.Content)+1, p.Content))
		rot := ""
		if p.Rotate != 0 {
			rot = fmt.Sprintf(" /Rotate %d", p.Rotate)
		}
		kids = append(kids, add(fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %g %g]%s /Resources << /Font << /F1 %d 0 R /F2 %d 0 R >> >> /Contents %d 0 R %s>>",
			pagesObj, p.W, p.H, rot, f1, f2, c, p.Extra)))
	}
	objs[catalog-1] = fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesObj)
	var k bytes.Buffer
	for _, id := range kids {
		fmt.Fprintf(&k, "%d 0 R ", id)
	}
	objs[pagesObj-1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", k.String(), len(kids))
	info := add("<< /Title (Test title) /Author (Someone) /Producer (filee test) >>")
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
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
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root %d 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, catalog, info, xref)
	path := filepath.Join(t.TempDir(), fmt.Sprintf("test%d.pdf", len(pages)))
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// textLines is a content stream of lines of Helvetica text from the top.
func textLines(size float64, x, top float64, lines ...string) string {
	var b bytes.Buffer
	for i, l := range lines {
		fmt.Fprintf(&b, "BT /F1 %g Tf %g %g Td (%s) Tj ET\n", size, x, top-float64(i)*size*1.2, l)
	}
	return b.String()
}

// text is one piece of text at x, y (baseline) in font F1 or F2 (bold).
func text(font string, size, x, y float64, s string) string {
	return fmt.Sprintf("BT /%s %g Tf %g %g Td (%s) Tj ET\n", font, size, x, y, s)
}

// linkAnnot is a link annotation to uri over the box.
func linkAnnot(x1, y1, x2, y2 float64, uri string) string {
	return fmt.Sprintf("<< /Type /Annot /Subtype /Link /Rect [%g %g %g %g] /Border [0 0 0] /A << /S /URI /URI (%s) >> >>", x1, y1, x2, y2, uri)
}
