package hwpx

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/hwp/xmltree"
)

// docxBuilder builds small .docx files for tests, so DOCX features can be tested without sample documents.
type docxBuilder struct {
	body       strings.Builder
	sectPr     string
	styles     string
	numbering  string
	settings   string
	rels       [][4]string // id, type, target, external
	parts      map[string]string
	partOrder  []string
	media      map[string][]byte
}

const docxNamespaces = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
	`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
	`xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" ` +
	`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
	`xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture" ` +
	`xmlns:wps="http://schemas.microsoft.com/office/word/2010/wordprocessingShape" ` +
	`xmlns:wpg="http://schemas.microsoft.com/office/word/2010/wordprocessingGroup" ` +
	`xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" ` +
	`xmlns:v="urn:schemas-microsoft-com:vml" ` +
	`xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math" ` +
	`xmlns:w10="urn:schemas-microsoft-com:office:word"`

const defaultDocxPage = `<w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" w:gutter="0"/>`

const defaultDocxStyles = `<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="맑은 고딕" w:eastAsia="맑은 고딕" w:hAnsi="맑은 고딕"/><w:sz w:val="20"/></w:rPr></w:rPrDefault>
<w:pPrDefault><w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>
<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>
<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:pPr><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/><w:sz w:val="32"/></w:rPr></w:style>
<w:style w:type="character" w:styleId="Hyperlink"><w:name w:val="Hyperlink"/><w:rPr><w:color w:val="0563C1"/><w:u w:val="single"/></w:rPr></w:style>
<w:style w:type="table" w:styleId="TableGrid"><w:name w:val="Table Grid"/><w:tblPr><w:tblBorders>
<w:top w:val="single" w:sz="4" w:color="000000"/><w:left w:val="single" w:sz="4" w:color="000000"/><w:bottom w:val="single" w:sz="4" w:color="000000"/>
<w:right w:val="single" w:sz="4" w:color="000000"/><w:insideH w:val="single" w:sz="4" w:color="000000"/><w:insideV w:val="single" w:sz="4" w:color="000000"/>
</w:tblBorders></w:tblPr></w:style>`

func newDocx() *docxBuilder {
	return &docxBuilder{sectPr: defaultDocxPage, styles: defaultDocxStyles, parts: map[string]string{}, media: map[string][]byte{}}
}

func (b *docxBuilder) add(xml string) *docxBuilder {
	b.body.WriteString(xml)
	return b
}

func (b *docxBuilder) relationship(kind, target string, external bool) string {
	id := fmt.Sprintf("rId%d", len(b.rels)+10)
	ext := ""
	if external {
		ext = "1"
	}
	b.rels = append(b.rels, [4]string{id, kind, target, ext})
	return id
}

func (b *docxBuilder) image(png []byte) string {
	name := fmt.Sprintf("media/image%d.png", len(b.media)+1)
	b.media["word/"+name] = png
	return b.relationship("http://schemas.openxmlformats.org/officeDocument/2006/relationships/image", name, false)
}

func (b *docxBuilder) hyperlink(url string) string {
	return b.relationship("http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink", url, true)
}

func (b *docxBuilder) part(kind, xml string) string {
	name := kind + ".xml"
	if kind == "header" || kind == "footer" {
		n := 0
		for k := range b.parts {
			if strings.HasPrefix(k, kind) {
				n++
			}
		}
		name = fmt.Sprintf("%s%d.xml", kind, n+1)
	}
	if _, ok := b.parts[name]; !ok {
		b.partOrder = append(b.partOrder, name)
	}
	b.parts[name] = xml
	return b.relationship("http://schemas.openxmlformats.org/officeDocument/2006/relationships/"+kind, name, false)
}

func (b *docxBuilder) header(paragraphs string) string {
	return b.part("header", "<w:hdr "+docxNamespaces+">"+paragraphs+"</w:hdr>")
}

func (b *docxBuilder) footer(paragraphs string) string {
	return b.part("footer", "<w:ftr "+docxNamespaces+">"+paragraphs+"</w:ftr>")
}

func (b *docxBuilder) footnotes(notes string) {
	b.part("footnotes", "<w:footnotes "+docxNamespaces+`><w:footnote w:type="separator" w:id="-1"><w:p><w:r><w:separator/></w:r></w:p></w:footnote>`+notes+"</w:footnotes>")
}

func (b *docxBuilder) save(t *testing.T, path string) string {
	t.Helper()
	if b.numbering != "" {
		b.part("numbering", "<w:numbering "+docxNamespaces+">"+b.numbering+"</w:numbering>")
	}
	if b.settings != "" {
		b.part("settings", "<w:settings "+docxNamespaces+">"+b.settings+"</w:settings>")
	}
	b.part("styles", "<w:styles "+docxNamespaces+">"+b.styles+"</w:styles>")
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	write := func(name, text string) {
		w, _ := z.Create(name)
		w.Write([]byte(text))
	}
	const decl = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`
	write("[Content_Types].xml", decl+`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Default Extension="png" ContentType="image/png"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`)
	write("_rels/.rels", decl+`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`)
	var rels strings.Builder
	for _, r := range b.rels {
		ext := ""
		if r[3] != "" {
			ext = ` TargetMode="External"`
		}
		fmt.Fprintf(&rels, `<Relationship Id="%s" Type="%s" Target="%s"%s/>`, r[0], r[1], Escape(r[2]), ext)
	}
	write("word/_rels/document.xml.rels", decl+`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+rels.String()+`</Relationships>`)
	write("word/document.xml", decl+"<w:document "+docxNamespaces+"><w:body>"+b.body.String()+"<w:sectPr>"+b.sectPr+"</w:sectPr></w:body></w:document>")
	for _, name := range b.partOrder {
		write("word/"+name, decl+b.parts[name])
	}
	for name, data := range b.media {
		w, _ := z.Create(name)
		w.Write(data)
	}
	z.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func wP(runs string, pPr ...string) string {
	return "<w:p><w:pPr>" + strings.Join(pPr, "") + "</w:pPr>" + runs + "</w:p>"
}

func wR(text string, rPr ...string) string {
	return "<w:r><w:rPr>" + strings.Join(rPr, "") + `</w:rPr><w:t xml:space="preserve">` + Escape(text) + "</w:t></w:r>"
}

func wField(instruction, result string) string {
	return `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> ` + instruction + ` </w:instrText></w:r>` +
		`<w:r><w:fldChar w:fldCharType="separate"/></w:r>` + wR(result) + `<w:r><w:fldChar w:fldCharType="end"/></w:r>`
}

const (
	pictureURI = "http://schemas.openxmlformats.org/drawingml/2006/picture"
	shapeURI   = "http://schemas.microsoft.com/office/word/2010/wordprocessingShape"
)

func wAnchor(uri, graphic string, cx, cy int64, horizontal, vertical, wrap string) string {
	if wrap == "" {
		wrap = `<wp:wrapSquare wrapText="bothSides"/>`
	}
	return fmt.Sprintf(`<w:r><w:drawing><wp:anchor distT="0" distB="0" distL="0" distR="0" simplePos="0" relativeHeight="1" behindDoc="0" locked="0" layoutInCell="1" allowOverlap="1">`+
		`<wp:simplePos x="0" y="0"/>%s%s<wp:extent cx="%d" cy="%d"/>%s<wp:docPr id="1" name="obj"/>`+
		`<a:graphic><a:graphicData uri="%s">%s</a:graphicData></a:graphic></wp:anchor></w:drawing></w:r>`, horizontal, vertical, cx, cy, wrap, uri, graphic)
}

func wPicture(id string, cx, cy int64) string {
	return fmt.Sprintf(`<pic:pic><pic:nvPicPr><pic:cNvPr id="1" name="p"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="%s"/></pic:blipFill>`+
		`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"/></pic:spPr></pic:pic>`, id, cx, cy)
}

func wInlinePicture(id string, cx, cy int64) string {
	return fmt.Sprintf(`<w:r><w:drawing><wp:inline><wp:extent cx="%d" cy="%d"/><wp:docPr id="2" name="i"/><a:graphic><a:graphicData uri="%s">`, cx, cy, pictureURI) +
		wPicture(id, cx, cy) + "</a:graphicData></a:graphic></wp:inline></w:drawing></w:r>"
}

func pngBytes(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{147, 112, 219, 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

// docxToHwpx converts a built DOCX with the reader and the writer and returns the HWPX path.
func docxToHwpx(t *testing.T, b *docxBuilder, name string) string {
	t.Helper()
	dir := t.TempDir()
	path := b.save(t, filepath.Join(dir, name+".docx"))
	d, err := ReadDocx(path, filepath.Join(dir, "media"))
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, name+".hwpx")
	if err := Write(d, out, nil); err != nil {
		t.Fatal(err)
	}
	return out
}

// hwpxPart parses one part of an HWPX package.
func hwpxPart(t *testing.T, path, name string) *xmltree.Element {
	t.Helper()
	d, err := xmltree.ParseString(zipEntry(t, path, name))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return d.Root
}

func section0(t *testing.T, path string) *xmltree.Element { return hwpxPart(t, path, "Contents/section0.xml") }

// paragraphWith is the innermost hp:p whose own runs hold text.
func paragraphWith(t *testing.T, root *xmltree.Element, text string) *xmltree.Element {
	t.Helper()
	var found *xmltree.Element
	for _, p := range root.DescendantsNamed("p") {
		var sb strings.Builder
		for _, r := range p.Children("run") {
			for _, tt := range r.Children("t") {
				sb.WriteString(tt.Value())
			}
		}
		if strings.Contains(sb.String(), text) {
			found = p
		}
	}
	if found == nil {
		t.Fatalf("no paragraph with %q", text)
	}
	return found
}

func runWith(t *testing.T, root *xmltree.Element, text string) *xmltree.Element {
	t.Helper()
	for _, r := range root.DescendantsNamed("run") {
		for _, tt := range r.Children("t") {
			if strings.Contains(tt.Value(), text) {
				return r
			}
		}
	}
	t.Fatalf("no run with %q", text)
	return nil
}

// headItem is the header.xml element of a kind (charPr, paraPr, borderFill, ...) with an id.
func headItem(t *testing.T, path, kind, id string) *xmltree.Element {
	t.Helper()
	for _, e := range hwpxPart(t, path, "Contents/header.xml").DescendantsNamed(kind) {
		if e.A("id") == id {
			return e
		}
	}
	t.Fatalf("no %s %s", kind, id)
	return nil
}

func atoi(t *testing.T, e *xmltree.Element, name string) int {
	t.Helper()
	v, err := strconv.Atoi(e.A(name))
	if err != nil {
		t.Fatalf("<%s> %s=%q", e.Local, name, e.A(name))
	}
	return v
}

func eq[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", what, got, want)
	}
}
