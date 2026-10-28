package hwpx

import (
	"archive/zip"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/hwp/xmltree"
	"github.com/dseif0x/filee-go/internal/engines/markdown"
	"github.com/dseif0x/filee-go/internal/engines/spreadsheet"
)

// HWPX → document model, the inverse of the writer: documents from the other readers (DOCX, Markdown, XLSX, PPTX)
// are written as HWPX and read back, then written and read again, which must give the same model.

// ───────────────────────── Canonical dump ─────────────────────────

type dumper struct {
	sb         strings.Builder
	layout     bool
	numberings map[*doc.Numbering]int
}

func dumpDocument(d *doc.Document, layout bool) string {
	w := &dumper{layout: layout, numberings: map[*doc.Numbering]int{}}
	w.line(0, "document title=%s", d.Title)
	for _, s := range d.Sections {
		w.line(0, "section page=%v columns=%v hide=%v/%v start=%v", deref(s.Page), s.Columns, s.HideFirstHeader, s.HideFirstFooter, deref(s.StartPageNumber))
		for _, h := range s.Headers {
			w.line(1, "header %d", h.Pages)
			w.blocks(2, h.Blocks)
		}
		for _, f := range s.Footers {
			w.line(1, "footer %d", f.Pages)
			w.blocks(2, f.Blocks)
		}
		w.blocks(1, s.Blocks)
	}
	return w.sb.String()
}

func deref[T any](p *T) any {
	if p == nil {
		return "-"
	}
	return *p
}

func (w *dumper) line(depth int, format string, args ...any) {
	w.sb.WriteString(strings.Repeat("  ", depth))
	fmt.Fprintf(&w.sb, format, args...)
	w.sb.WriteByte('\n')
}

func charString(f doc.CharFormat) string {
	return fmt.Sprintf("{%v %v %v %v %v %v %v %v %v %v %v %v}", deref(f.Bold), deref(f.Italic), deref(f.Underline), deref(f.Strike), deref(f.Superscript), deref(f.Subscript),
		deref(f.Size), deref(f.Font), deref(f.EastAsianFont), deref(f.Color), deref(f.Shade), deref(f.Spacing))
}

func paraString(f doc.ParaFormat) string {
	return fmt.Sprintf("[%v %v/%v/%v %v/%v %v keep=%v/%v/%v tabs=%v]", deref(f.Align), deref(f.Left), deref(f.Right), deref(f.FirstLine), deref(f.Before), deref(f.After),
		deref(f.LineSpacing), deref(f.KeepWithNext), deref(f.KeepLines), deref(f.WidowOrphan), f.Tabs)
}

func (w *dumper) list(l *doc.ListRef) string {
	if l == nil {
		return "none"
	}
	id, ok := w.numberings[l.Numbering]
	if !ok {
		id = len(w.numberings) + 1
		w.numberings[l.Numbering] = id
		return fmt.Sprintf("#%d%v L%d %v", id, l.Numbering.Levels, l.Level, l.Numbered)
	}
	return fmt.Sprintf("#%d L%d %v", id, l.Level, l.Numbered)
}

func hashOf(path string) string {
	b, _ := os.ReadFile(path)
	return fmt.Sprintf("%x", sha256.Sum256(b))[:8]
}

func (w *dumper) blocks(depth int, blocks []doc.Block) {
	for _, b := range blocks {
		switch v := b.(type) {
		case *doc.Paragraph:
			w.line(depth, "P %s mark=%s heading=%d list=%s break=%v/%v columns=%v", paraString(v.Format), charString(v.MarkFormat), v.HeadingLevel, w.list(v.List), v.PageBreakBefore, v.ColumnBreakBefore, deref(v.ColumnsChange))
			w.inlines(depth+1, v.Inlines)
		case *doc.Table:
			w.line(depth, "TABLE count=%d widths=%v relative=%v align=%v indent=%d margin=%v borders=%v", v.ColumnCount, v.ColumnWidths, v.RelativeWidths, v.Align, v.Indent, deref(v.CellMargin), deref(v.Borders))
			for _, r := range v.Rows {
				height := any("-")
				if w.layout {
					height = deref(r.Height)
				}
				w.line(depth+1, "ROW height=%v header=%v", height, r.Header)
				for _, c := range r.Cells {
					w.line(depth+2, "CELL col=%d span=%dx%d valign=%v fill=%s borders=%v", c.Column, c.RowSpan, c.ColSpan, c.VerticalAlign, c.Fill, deref(c.Borders))
					w.blocks(depth+3, c.Blocks)
				}
			}
			if len(v.Caption) > 0 {
				w.line(depth+1, "CAPTION")
				w.blocks(depth+2, v.Caption)
			}
		}
	}
}

func (w *dumper) inlines(depth int, inlines []doc.Inline) {
	for _, in := range inlines {
		switch v := in.(type) {
		case *doc.Text:
			w.line(depth, "T %q %s", v.Text, charString(v.Format))
		case *doc.Equation:
			w.line(depth, "EQ %q %s", v.Text.Text, charString(v.Text.Format))
		case *doc.Tab:
			w.line(depth, "TAB %s", charString(v.Format))
		case *doc.LineBreak:
			w.line(depth, "BR %s", charString(v.Format))
		case *doc.Link:
			w.line(depth, "LINK %s", v.Target)
			w.inlines(depth+1, v.Content)
		case *doc.Bookmark:
			w.line(depth, "BOOKMARK %s", v.Name)
		case *doc.Note:
			w.line(depth, "NOTE endnote=%v", v.Endnote)
			w.blocks(depth+1, v.Blocks)
		case *doc.Field:
			w.line(depth, "FIELD %d %s", v.Kind, charString(v.Format))
		case *doc.Image:
			w.line(depth, "IMAGE %s %vx%v anchor=%v", hashOf(v.Path), deref(v.Width), deref(v.Height), deref(v.Anchor))
		case *doc.TextBox:
			w.line(depth, "TEXTBOX %dx%d anchor=%v fill=%s gradient=%v line=%v shape=%v ratio=%d padding=%v valign=%v", v.Width, v.Height, deref(v.Anchor), v.Fill, deref(v.Gradient), v.Line, v.Shape, v.CornerRatio, v.Padding, v.VerticalAlign)
			w.blocks(depth+1, v.Blocks)
		case *doc.Shape:
			w.line(depth, "SHAPE %v %dx%d anchor=%v fill=%s gradient=%v line=%v ratio=%d flip=%v/%v", v.Kind, v.Width, v.Height, deref(v.Anchor), v.Fill, deref(v.Gradient), v.Line, v.CornerRatio, v.FlipHorizontal, v.FlipVertical)
		}
	}
}

// outline is the section parts as a sequence of elements and text, without run boundaries, idRefs and table heights.
func outline(t *testing.T, hwpx string) string {
	t.Helper()
	z, err := zip.OpenReader(hwpx)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	var names []string
	for _, f := range z.File {
		if strings.HasPrefix(f.Name, "Contents/section") {
			names = append(names, f.Name)
		}
	}
	sort.Strings(names)
	var sb strings.Builder
	var walk func(e *xmltree.Element)
	walk = func(e *xmltree.Element) {
		if e.Local != "run" && e.Local != "t" {
			tableHeight := e.Local == "cellSz" || e.Local == "sz" && e.Parent != nil && e.Parent.Local == "tbl"
			sb.WriteString("<" + e.Local)
			for _, a := range e.Attrs {
				if a.Prefix == "xmlns" || a.Local == "xmlns" || strings.HasSuffix(a.Local, "IDRef") || tableHeight && a.Local == "height" {
					continue
				}
				sb.WriteString(" " + a.Local + "=" + a.Value)
			}
			sb.WriteString(">")
		}
		for _, k := range e.Kids {
			switch n := k.(type) {
			case *xmltree.Text:
				sb.WriteString(n.Data)
			case *xmltree.Element:
				walk(n)
			}
		}
	}
	for _, n := range names {
		walk(hwpxPart(t, hwpx, n))
	}
	return sb.String()
}

// roundTrip writes source, reads it back, writes and reads again: the two models and the two files must agree.
func roundTrip(t *testing.T, source *doc.Document, name string) (*doc.Document, string) {
	t.Helper()
	dir := t.TempDir()
	first := filepath.Join(dir, name+".hwpx")
	if err := Write(source, first, nil); err != nil {
		t.Fatal(err)
	}
	skipped := map[string]int{}
	read, err := ReadHwpx(first, filepath.Join(dir, "media-1"), skipped)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) > 0 {
		t.Errorf("skipped elements: %v", skipped) // everything Filee writes has a place in the model
	}
	second := filepath.Join(dir, name+"-again.hwpx")
	if err := Write(read, second, nil); err != nil {
		t.Fatal(err)
	}
	again, err := ReadHwpx(second, filepath.Join(dir, "media-2"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if a, b := dumpDocument(read, false), dumpDocument(again, false); a != b {
		t.Errorf("second round trip differs:\n%s", firstDifference(a, b))
	}
	if a, b := outline(t, first), outline(t, second); a != b {
		t.Errorf("second file differs:\n%s", firstDifference(a, b))
	}
	return read, first
}

func firstDifference(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) && i < len(bl); i++ {
		if al[i] != bl[i] {
			return fmt.Sprintf("line %d:\n  %s\n  %s", i+1, al[i], bl[i])
		}
	}
	if len(a) > 400 {
		return fmt.Sprintf("lengths %d / %d", len(a), len(b))
	}
	return a + "\n----\n" + b
}

func allText(blocks []doc.Block) string {
	var sb strings.Builder
	for _, in := range doc.Inlines(blocks) {
		if s, _, ok := textOf(in); ok {
			sb.WriteString(s)
		}
	}
	return sb.String()
}

// richDocx: two sections (odd/even headers and a hidden first header, then landscape with two columns), formatting,
// lists, tab stops, line spacing kinds, a table with spans, borders, shading and a nested table, pictures inline and
// floating, a text box, shapes, footnote, endnote, links, bookmark and breaks.
func richDocx(t *testing.T, dir string) string {
	b := newDocx()
	b.numbering = `<w:abstractNum w:abstractNumId="1">
  <w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/><w:pPr><w:ind w:left="400" w:hanging="400"/></w:pPr></w:lvl>
  <w:lvl w:ilvl="1"><w:start w:val="3"/><w:numFmt w:val="ganada"/><w:lvlText w:val="%2)"/><w:pPr><w:ind w:left="800" w:hanging="400"/></w:pPr></w:lvl>
</w:abstractNum>
<w:abstractNum w:abstractNumId="2"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:lvlText w:val="&#xF0B7;"/></w:lvl></w:abstractNum>
<w:num w:numId="1"><w:abstractNumId w:val="1"/></w:num>
<w:num w:numId="2"><w:abstractNumId w:val="2"/></w:num>`
	b.settings = "<w:evenAndOddHeaders/>"
	picture := b.image(pngBytes(120, 60))
	header := b.header(wP(wR("홀수 쪽 머리말"), `<w:jc w:val="right"/>`))
	evenHeader := b.header(wP(wR("짝수 쪽 머리말")))
	footer := b.footer(wP(wR("- ")+wField("PAGE", "1")+wR(" / ")+wField("NUMPAGES", "3")+wR(" -"), `<w:jc w:val="center"/>`))
	b.footnotes(`<w:footnote w:id="1"><w:p><w:r><w:footnoteRef/></w:r>` + wR(" 각주 설명") + "</w:p></w:footnote>")
	b.part("endnotes", "<w:endnotes "+docxNamespaces+`><w:endnote w:type="separator" w:id="-1"><w:p><w:r><w:separator/></w:r></w:p></w:endnote><w:endnote w:id="1"><w:p><w:r><w:endnoteRef/></w:r>`+wR(" 미주 설명")+"</w:p></w:endnote></w:endnotes>")
	url := b.hyperlink("https://example.com/a?b=1")
	cell := func(content, tcPr string) string { return "<w:tc><w:tcPr>" + tcPr + "</w:tcPr>" + content + "</w:tc>" }
	const column = `<wp:positionH relativeFrom="column"><wp:posOffset>0</wp:posOffset></wp:positionH>`
	const paragraph = `<wp:positionV relativeFrom="paragraph"><wp:posOffset>0</wp:posOffset></wp:positionV>`
	shape := func(geometry, fill, line string, cx, cy int64, text string) string {
		s := fmt.Sprintf(`<wps:wsp><wps:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="%s"/>%s%s</wps:spPr>`, cx, cy, geometry, fill, line)
		if text != "" {
			s += "<wps:txbx><w:txbxContent>" + wP(wR(text)) + "</w:txbxContent></wps:txbx>"
		}
		return s + `<wps:bodyPr anchor="ctr"/></wps:wsp>`
	}
	nested := `<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/></w:tblPr><w:tblGrid><w:gridCol w:w="1000"/><w:gridCol w:w="1000"/></w:tblGrid>` +
		"<w:tr>" + cell(wP(wR("안1")), "") + cell(wP(wR("안2")), "") + "</w:tr></w:tbl>" + wP("")

	b.add(wP(`<w:bookmarkStart w:id="0" w:name="intro"/>`+wR("가운데 제목")+`<w:bookmarkEnd w:id="0"/>`, `<w:pStyle w:val="Heading1"/><w:jc w:val="center"/>`)).
		add(wP(wR("굵은 빨강", `<w:b/><w:color w:val="FF0000"/><w:sz w:val="28"/><w:rFonts w:ascii="Arial" w:hAnsi="Arial" w:eastAsia="굴림"/>`)+
			wR(" 형광펜", `<w:highlight w:val="yellow"/>`)+wR(" 기울임", `<w:i/><w:u w:val="single"/><w:strike/>`)+
			wR("위첨자", `<w:vertAlign w:val="superscript"/>`)+wR("아래첨자", `<w:vertAlign w:val="subscript"/>`)+
			wR("자간", `<w:spacing w:val="20"/>`)+"<w:r><w:tab/></w:r>"+wR("탭 뒤"),
			`<w:ind w:left="720" w:hanging="360"/><w:spacing w:before="240" w:after="120"/><w:tabs><w:tab w:val="right" w:leader="dot" w:pos="8000"/></w:tabs><w:keepNext/><w:keepLines/>`)).
		add(wP(wR("고정 줄 간격"), `<w:spacing w:line="360" w:lineRule="exact"/><w:ind w:right="400" w:firstLine="300"/><w:jc w:val="both"/>`)).
		add(wP(wR("최소 줄 간격"), `<w:spacing w:line="300" w:lineRule="atLeast"/><w:jc w:val="distribute"/>`)).
		add(wP(wR("첫 항목"), `<w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr>`)).
		add(wP(wR("하위 항목"), `<w:numPr><w:ilvl w:val="1"/><w:numId w:val="1"/></w:numPr>`)).
		add(wP(wR("글머리 항목"), `<w:numPr><w:ilvl w:val="0"/><w:numId w:val="2"/></w:numPr>`)).
		add(wP(wR("본문") + `<w:r><w:footnoteReference w:id="1"/></w:r>` + wR("미주") + `<w:r><w:endnoteReference w:id="1"/></w:r>` +
			`<w:hyperlink r:id="` + url + `">` + wR("웹 링크", `<w:rStyle w:val="Hyperlink"/>`) + "</w:hyperlink>" +
			`<w:hyperlink w:anchor="intro">` + wR("소개로 이동") + "</w:hyperlink>" + wR("줄") + "<w:r><w:br/></w:r>" + wR("바꿈"))).
		add(`<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/><w:jc w:val="center"/></w:tblPr>` +
			`<w:tblGrid><w:gridCol w:w="2000"/><w:gridCol w:w="3000"/><w:gridCol w:w="2000"/></w:tblGrid>` +
			"<w:tr><w:trPr><w:tblHeader/></w:trPr>" + cell(wP(wR("가로 병합")), `<w:gridSpan w:val="2"/><w:shd w:val="pct10" w:color="auto" w:fill="auto"/>`) +
			cell(wP(wR("세로 병합")), `<w:vMerge w:val="restart"/><w:vAlign w:val="center"/>`) + "</w:tr>" +
			"<w:tr>" + cell(nested, "") + cell(wP(wR("B")), `<w:vAlign w:val="bottom"/>`) + cell(wP(""), "<w:vMerge/>") + "</w:tr>" +
			"<w:tr>" + cell(wP(wR("C")), `<w:tcBorders><w:bottom w:val="double" w:sz="12" w:color="FF0000"/></w:tcBorders>`) +
			cell(wP(wR("D")), `<w:shd w:val="clear" w:color="auto" w:fill="FFF2CC"/>`) + cell(wP(wR("E")), `<w:tcBorders><w:left w:val="dashed" w:sz="8" w:color="0000FF"/></w:tcBorders>`) + "</w:tr>" +
			"</w:tbl>").
		add(wP(wAnchor(pictureURI, wPicture(picture, 914400, 457200), 914400, 457200,
			`<wp:positionH relativeFrom="page"><wp:posOffset>914400</wp:posOffset></wp:positionH>`, paragraph, "") + wR("그림 옆에 흐르는 글"))).
		add(wP(wInlinePicture(picture, 457200, 457200))).
		add(wP(wAnchor(shapeURI,
			shape("roundRect", `<a:solidFill><a:srgbClr val="FFF2CC"/></a:solidFill>`, `<a:ln w="12700"><a:solidFill><a:srgbClr val="C00000"/></a:solidFill></a:ln>`, 1828800, 914400, "글상자 안의 글"),
			1828800, 914400, `<wp:positionH relativeFrom="margin"><wp:align>center</wp:align></wp:positionH>`,
			`<wp:positionV relativeFrom="page"><wp:posOffset>3600000</wp:posOffset></wp:positionV>`, "<wp:wrapTopAndBottom/>"))).
		add(wP(wAnchor(shapeURI, shape("line", "", `<a:ln w="25400"><a:solidFill><a:srgbClr val="4F81BD"/></a:solidFill><a:prstDash val="dash"/></a:ln>`, 3000000, 0, ""),
			3000000, 0, column, paragraph, "<wp:wrapNone/>"))).
		add(wP(wAnchor(shapeURI,
			shape("ellipse", `<a:gradFill><a:gsLst><a:gs pos="0"><a:srgbClr val="99CCFF"/></a:gs><a:gs pos="100000"><a:srgbClr val="FFFFFF"/></a:gs></a:gsLst><a:lin ang="5400000"/></a:gradFill>`, "<a:ln><a:noFill/></a:ln>", 1000000, 500000, ""),
			1000000, 500000, column, paragraph, "<wp:wrapNone/>"))).
		add(wP(wR("앞 쪽") + `<w:r><w:br w:type="page"/></w:r>` + wR("다음 쪽"))).
		add(wP(wR("첫 구역 끝"),
			fmt.Sprintf(`<w:sectPr><w:headerReference w:type="default" r:id="%s"/><w:headerReference w:type="even" r:id="%s"/><w:footerReference w:type="default" r:id="%s"/>`, header, evenHeader, footer)+
				defaultDocxPage+`<w:titlePg/><w:pgNumType w:start="3"/></w:sectPr>`)).
		add(wP(wR("가로 두 단"))).
		add(wP(wR("다음 단")) + wP(`<w:r><w:br w:type="column"/></w:r>`+wR("둘째 단")))
	b.sectPr = `<w:pgSz w:w="16838" w:h="11906" w:orient="landscape"/>` +
		`<w:pgMar w:top="1985" w:right="1134" w:bottom="1418" w:left="1134" w:header="851" w:footer="851" w:gutter="0"/><w:cols w:num="2" w:space="720" w:sep="1"/>`
	return b.save(t, filepath.Join(dir, "rich.docx"))
}

func TestDocxDocumentsSurviveTheWayThroughHwpx(t *testing.T) {
	dir := t.TempDir()
	source, err := ReadDocx(richDocx(t, dir), filepath.Join(dir, "docx-media"))
	if err != nil {
		t.Fatal(err)
	}
	read, _ := roundTrip(t, source, "docx")
	if len(read.Sections) != 2 {
		t.Fatalf("%d sections", len(read.Sections))
	}
	first := read.Sections[0]
	if len(first.Headers) != 2 || first.Headers[0].Pages != doc.OddPages || first.Headers[1].Pages != doc.EvenPages {
		t.Errorf("headers %v", first.Headers)
	}
	eq(t, "odd header", allText(first.Headers[0].Blocks), "홀수 쪽 머리말")
	if !first.HideFirstHeader || val(first.StartPageNumber, 0) != 3 {
		t.Errorf("hide %v start %v", first.HideFirstHeader, deref(first.StartPageNumber))
	}
	var fields []doc.FieldKind
	for _, in := range doc.Inlines(first.Footers[0].Blocks) {
		if f, ok := in.(*doc.Field); ok {
			fields = append(fields, f.Kind)
		}
	}
	eq(t, "fields", fmt.Sprint(fields), fmt.Sprint([]doc.FieldKind{doc.PageNumberField, doc.TotalPagesField}))
	landscape := read.Sections[1]
	if !landscape.Page.Landscape() || landscape.Page.Width != 16838*5 || landscape.Page.Height != 11906*5 {
		t.Errorf("landscape page %+v", landscape.Page)
	}
	eq(t, "columns", landscape.Columns, doc.Columns{Count: 2, Gap: 3600, Separator: true})

	var indented *doc.Paragraph
	for _, b := range first.Blocks {
		if p, ok := b.(*doc.Paragraph); ok && strings.HasPrefix(allText([]doc.Block{p}), "굵은 빨강") {
			indented = p
		}
	}
	eq(t, "left", val(indented.Format.Left, 0), 3600)
	eq(t, "first line", val(indented.Format.FirstLine, 0), -1800)
	red := indented.Inlines[0].(*doc.Text)
	eq(t, "red", fmt.Sprintf("%v %v %v %v %v", isTrue(red.Format.Bold), val(red.Format.Size, 0), val(red.Format.Font, ""), val(red.Format.EastAsianFont, ""), val(red.Format.Color, "")), "true 1400 Arial 굴림 #FF0000")

	var notes, links []string
	images, ellipses := 0, 0
	boxFound, pageBreak, columnBreak := false, false, false
	var table *doc.Table
	for _, in := range doc.Inlines(first.Blocks) {
		switch v := in.(type) {
		case *doc.Note:
			notes = append(notes, fmt.Sprintf("%s/%v", allText(v.Blocks), v.Endnote))
		case *doc.Link:
			links = append(links, v.Target)
		case *doc.Image:
			images++
		case *doc.TextBox:
			boxFound = boxFound || v.Fill == "#FFF2CC" && allText(v.Blocks) == "글상자 안의 글"
		case *doc.Shape:
			if v.Kind == doc.ShapeEllipse && v.Gradient != nil && len(v.Gradient.Colors) == 2 {
				ellipses++
			}
		}
	}
	eq(t, "notes", strings.Join(notes, ","), "각주 설명/false,미주 설명/true")
	eq(t, "links", strings.Join(links, ","), "https://example.com/a?b=1,#intro")
	eq(t, "images", images, 2)
	eq(t, "ellipse", ellipses, 1)
	if !boxFound {
		t.Error("no text box")
	}
	for _, b := range first.Blocks {
		switch v := b.(type) {
		case *doc.Table:
			table = v
		case *doc.Paragraph:
			pageBreak = pageBreak || v.PageBreakBefore
		}
	}
	for _, b := range landscape.Blocks {
		if p, ok := b.(*doc.Paragraph); ok {
			columnBreak = columnBreak || p.ColumnBreakBefore
		}
	}
	if !pageBreak || !columnBreak {
		t.Errorf("page break %v column break %v", pageBreak, columnBreak)
	}
	if !table.Rows[0].Header || table.Rows[0].Cells[0].ColSpan != 2 || table.Rows[0].Cells[1].RowSpan != 2 {
		t.Errorf("table header/spans")
	}
	if nested, ok := table.Rows[1].Cells[0].Blocks[0].(*doc.Table); !ok || allText([]doc.Block{nested}) != "안1안2" {
		t.Errorf("nested table")
	}
}

func TestMarkdownDocumentsSurviveTheWayThroughHwpx(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "logo.png"), pngBytes(240, 120), 0o644)
	const md = "---\ntitle: 마크다운 제목\n---\n\n# 첫 번째 제목\n\n일반 문단에 **굵게**, *기울임*, ~~취소선~~, `코드` 그리고 [링크](https://example.com) 가 있습니다.[^1]\n\n> 인용문\n\n3. 셋째부터 시작\n4. 다음 항목\n   - 중첩 글머리\n\n| 이름 | 값 |\n|------|---:|\n| 표 셀 | 42 |\n\n![로고](logo.png)\n\n```\n코드 블록\n```\n\n[^1]: 각주 내용입니다.\n"
	source := markdown.Read(md, dir, "")
	read, _ := roundTrip(t, source, "markdown")
	eq(t, "title", read.Title, "마크다운 제목")
	blocks := read.Sections[0].Blocks
	images, start := 0, 0
	note := ""
	for _, in := range doc.Inlines(blocks) {
		switch v := in.(type) {
		case *doc.Image:
			images++
		case *doc.Note:
			note = allText(v.Blocks)
		}
	}
	for _, b := range blocks {
		if p, ok := b.(*doc.Paragraph); ok && allText([]doc.Block{p}) == "셋째부터 시작" {
			start = p.List.Numbering.Levels[0].Start
		}
	}
	eq(t, "images", images, 1)
	eq(t, "list start", start, 3)
	if !strings.Contains(note, "각주 내용입니다") {
		t.Errorf("note %q", note)
	}
}

func TestSpreadsheetsSurviveTheWayThroughHwpx(t *testing.T) {
	header := spreadsheet.CellStyle{Bold: true, Fill: "#1F4E79", Color: "#FFFFFF"}
	sales := spreadsheet.NewWorksheet("매출")
	sales.Set(0, 0, spreadsheet.TextCell("지점별 매출", header))
	sales.Set(1, 0, spreadsheet.TextCell("지점", header))
	sales.Set(1, 1, spreadsheet.TextCell("매출", header))
	sales.Set(2, 0, spreadsheet.TextCell("서울", spreadsheet.CellStyle{}))
	sales.Set(2, 1, &spreadsheet.Cell{Text: "₩1,350,000", IsNumber: true})
	sales.Merges = []spreadsheet.CellRange{{Top: 0, Left: 0, Bottom: 0, Right: 1}}
	sales.FrozenRows = 2
	wide := spreadsheet.NewWorksheet("넓은 표")
	for c := 0; c < 12; c++ {
		wide.Set(0, c, spreadsheet.TextCell(fmt.Sprintf("항목 %d", c+1), spreadsheet.CellStyle{}))
		wide.ColumnWidths[c] = 12
	}
	source := spreadsheet.Document(&spreadsheet.Workbook{Sheets: []*spreadsheet.Worksheet{sales, wide}})
	read, _ := roundTrip(t, source, "xlsx")
	var table *doc.Table
	for _, b := range read.Sections[0].Blocks {
		if tb, ok := b.(*doc.Table); ok {
			table = tb
		}
	}
	eq(t, "merge", fmt.Sprint(table.Rows[0].Cells[0].RowSpan, table.Rows[0].Cells[0].ColSpan), "1 2")
	eq(t, "fill", table.Rows[0].Cells[0].Fill, "#1F4E79")
	var headers []bool
	for _, r := range table.Rows {
		headers = append(headers, r.Header)
	}
	eq(t, "headers", fmt.Sprint(headers), "[true true false]")
	if !read.Sections[1].Page.Landscape() {
		t.Error("wide sheet not landscape")
	}
}

func TestExcelPageBreaksBecomePageBreaks(t *testing.T) {
	sheet := spreadsheet.NewWorksheet("목록")
	sheet.Set(0, 0, spreadsheet.TextCell("번호", spreadsheet.CellStyle{Bold: true}))
	for r := 1; r <= 8; r++ {
		sheet.Set(r, 0, &spreadsheet.Cell{Text: fmt.Sprint(r), IsNumber: true})
	}
	sheet.HiddenRows[6] = true
	sheet.FrozenRows = 1
	sheet.RowBreaks[4], sheet.RowBreaks[6] = true, true
	source := spreadsheet.Document(&spreadsheet.Workbook{Sheets: []*spreadsheet.Worksheet{sheet}})
	out := filepath.Join(t.TempDir(), "목록.hwpx")
	if err := Write(source, out, nil); err != nil {
		t.Fatal(err)
	}
	eq(t, "page breaks", len(regexp.MustCompile(`pageBreak="1"`).FindAllString(zipEntry(t, out, "Contents/section0.xml"), -1)), 2)
}
