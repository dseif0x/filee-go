// Document → DOCX (WordprocessingML, written by hand): sections with their
// page size and margins, paragraphs with alignment, indents, tab stops, line
// spacing and heading styles, formatted runs, links, pictures (a scanned page
// behind the text) and tables with merged cells, borders and shading. Kept in
// this package so PDF → DOCX needs nothing else; the office engines have the
// full writer.

package pdf

import (
	"archive/zip"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	wNamespaces = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
		`xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" ` +
		`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
		`xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"`
	xmlDeclaration = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`
	relBase        = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
)

func xmlEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '"':
			b.WriteString("&quot;")
		case r < 0x20 && r != '\t' && r != '\n' && r != '\r', r == 0xFFFE, r == 0xFFFF:
			// not allowed in XML
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// twips from HWPUNIT (1/100 pt → 1/20 pt).
func twips(v int) int { return v / 5 }

type docxWriter struct {
	rels   []string // relationship XML
	media  map[string]string
	files  map[string][]byte
	nextID int
	pics   int
}

func (w *docxWriter) rel(typ, target string, external bool) string {
	w.nextID++
	id := fmt.Sprintf("rId%d", w.nextID+10)
	mode := ""
	if external {
		mode = ` TargetMode="External"`
	}
	w.rels = append(w.rels, fmt.Sprintf(`<Relationship Id="%s" Type="%s%s" Target="%s"%s/>`, id, relBase, typ, xmlEscape(target), mode))
	return id
}

// WriteDocx writes doc as a DOCX file.
func WriteDocx(doc *Document, path string) error {
	w := &docxWriter{media: map[string]string{}, files: map[string][]byte{}}
	var body strings.Builder
	for si, s := range doc.Sections {
		for bi, blk := range s.Blocks {
			sect := ""
			if si < len(doc.Sections)-1 && bi == len(s.Blocks)-1 {
				sect = sectPr(s.Page)
			}
			switch v := blk.(type) {
			case *Paragraph:
				w.paragraph(&body, v, sect)
			case *Table:
				w.table(&body, v)
				if sect != "" {
					body.WriteString("<w:p><w:pPr>" + sect + "</w:pPr></w:p>")
				}
			}
		}
	}
	last := PageSetup{}
	if n := len(doc.Sections); n > 0 {
		last = doc.Sections[n-1].Page
	}
	document := xmlDeclaration + `<w:document ` + wNamespaces + `><w:body>` + body.String() + sectPr(last) + `</w:body></w:document>`

	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	z := zip.NewWriter(f)
	put := func(name, content string) {
		if err != nil {
			return
		}
		var fw interface{ Write([]byte) (int, error) }
		if fw, err = z.Create(name); err == nil {
			_, err = fw.Write([]byte(content))
		}
	}
	var types strings.Builder
	types.WriteString(xmlDeclaration + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>` +
		`<Default Extension="png" ContentType="image/png"/><Default Extension="jpeg" ContentType="image/jpeg"/><Default Extension="jpg" ContentType="image/jpeg"/>` +
		`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
		`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
		`<Override PartName="/word/settings.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"/>` +
		`<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>` +
		`<Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/></Types>`)
	put("[Content_Types].xml", types.String())
	put("_rels/.rels", xmlDeclaration+`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
		`<Relationship Id="rId1" Type="`+relBase+`officeDocument" Target="word/document.xml"/>`+
		`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>`+
		`<Relationship Id="rId3" Type="`+relBase+`extended-properties" Target="docProps/app.xml"/></Relationships>`)
	put("word/document.xml", document)
	put("word/_rels/document.xml.rels", xmlDeclaration+`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
		`<Relationship Id="rId1" Type="`+relBase+`styles" Target="styles.xml"/>`+
		`<Relationship Id="rId2" Type="`+relBase+`settings" Target="settings.xml"/>`+strings.Join(w.rels, "")+`</Relationships>`)
	put("word/styles.xml", docxStyles())
	put("word/settings.xml", xmlDeclaration+`<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:defaultTabStop w:val="720"/><w:compat><w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/></w:compat></w:settings>`)
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	title := ""
	if doc.Title != "" {
		title = "<dc:title>" + xmlEscape(doc.Title) + "</dc:title>"
	}
	put("docProps/core.xml", xmlDeclaration+`<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">`+
		title+`<dcterms:created xsi:type="dcterms:W3CDTF">`+now+`</dcterms:created><dcterms:modified xsi:type="dcterms:W3CDTF">`+now+`</dcterms:modified></cp:coreProperties>`)
	put("docProps/app.xml", xmlDeclaration+`<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Application>Filee</Application></Properties>`)
	for name, data := range w.files {
		if err != nil {
			break
		}
		var fw interface{ Write([]byte) (int, error) }
		if fw, err = z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store}); err == nil {
			_, err = fw.Write(data)
		}
	}
	if cerr := z.Close(); err == nil {
		err = cerr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func sectPr(p PageSetup) string {
	if p.Width <= 0 || p.Height <= 0 {
		p = PageSetup{59528, 84189, 7200, 7200, 7200, 7200}
	}
	orient := ""
	if p.Width > p.Height {
		orient = ` w:orient="landscape"`
	}
	return fmt.Sprintf(`<w:sectPr><w:pgSz w:w="%d" w:h="%d"%s/><w:pgMar w:top="%d" w:right="%d" w:bottom="%d" w:left="%d" w:header="0" w:footer="0" w:gutter="0"/></w:sectPr>`,
		twips(p.Width), twips(p.Height), orient, twips(p.Top), twips(p.Right), twips(p.Bottom), twips(p.Left))
}

func docxStyles() string {
	var b strings.Builder
	b.WriteString(xmlDeclaration + `<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`)
	b.WriteString(`<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="Calibri" w:cs="Calibri"/><w:sz w:val="22"/></w:rPr></w:rPrDefault><w:pPrDefault><w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>`)
	b.WriteString(`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>`)
	for i := 1; i <= 6; i++ {
		fmt.Fprintf(&b, `<w:style w:type="paragraph" w:styleId="Heading%d"><w:name w:val="heading %d"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:outlineLvl w:val="%d"/></w:pPr><w:rPr><w:b/></w:rPr></w:style>`, i, i, i-1)
	}
	b.WriteString(`<w:style w:type="character" w:styleId="Hyperlink"><w:name w:val="Hyperlink"/><w:rPr><w:color w:val="0563C1"/><w:u w:val="single"/></w:rPr></w:style>`)
	b.WriteString(`<w:style w:type="table" w:default="1" w:styleId="TableNormal"><w:name w:val="Normal Table"/><w:tblPr><w:tblCellMar><w:left w:w="108" w:type="dxa"/><w:right w:w="108" w:type="dxa"/></w:tblCellMar></w:tblPr></w:style>`)
	b.WriteString(`</w:styles>`)
	return b.String()
}

func (w *docxWriter) paragraph(b *strings.Builder, p *Paragraph, sect string) {
	b.WriteString("<w:p><w:pPr>")
	if p.HeadingLevel > 0 {
		fmt.Fprintf(b, `<w:pStyle w:val="Heading%d"/>`, min(6, p.HeadingLevel))
	}
	if p.PageBreakBefore {
		b.WriteString("<w:pageBreakBefore/>")
	}
	f := p.Format
	if len(f.Tabs) > 0 {
		b.WriteString("<w:tabs>")
		for _, t := range f.Tabs {
			fmt.Fprintf(b, `<w:tab w:val="left" w:pos="%d"/>`, twips(t.Position))
		}
		b.WriteString("</w:tabs>")
	}
	line := ""
	switch {
	case f.LineSpacing.Kind == SpacingAtLeast && f.LineSpacing.Value > 0:
		line = fmt.Sprintf(` w:line="%d" w:lineRule="atLeast"`, twips(f.LineSpacing.Value))
	case f.LineSpacing.Kind == SpacingPercent && f.LineSpacing.Value > 0:
		// 130 % of the model is Word's single line spacing.
		line = fmt.Sprintf(` w:line="%d" w:lineRule="auto"`, f.LineSpacing.Value*240/130)
	}
	fmt.Fprintf(b, `<w:spacing w:before="%d" w:after="%d"%s/>`, twips(f.Before), twips(f.After), line)
	if f.Left != nil || f.FirstLine != nil {
		b.WriteString("<w:ind")
		if f.Left != nil {
			fmt.Fprintf(b, ` w:start="%d"`, twips(*f.Left))
		}
		if f.FirstLine != nil {
			if *f.FirstLine >= 0 {
				fmt.Fprintf(b, ` w:firstLine="%d"`, twips(*f.FirstLine))
			} else {
				fmt.Fprintf(b, ` w:hanging="%d"`, twips(-*f.FirstLine))
			}
		}
		b.WriteString("/>")
	}
	if f.RightToLeft {
		b.WriteString("<w:bidi/>")
	}
	switch f.Align {
	case AlignCenter:
		b.WriteString(`<w:jc w:val="center"/>`)
	case AlignRight:
		b.WriteString(`<w:jc w:val="end"/>`)
	case AlignJustify:
		b.WriteString(`<w:jc w:val="both"/>`)
	}
	if p.MarkFormat.Size > 0 {
		fmt.Fprintf(b, `<w:rPr><w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr>`, p.MarkFormat.Size/50, p.MarkFormat.Size/50)
	}
	b.WriteString(sect)
	b.WriteString("</w:pPr>")
	w.inlines(b, p.Inlines, p.Format.RightToLeft, false)
	b.WriteString("</w:p>")
}

func runProps(f CharFormat, rtl, link bool) string {
	var b strings.Builder
	b.WriteString("<w:rPr>")
	if link {
		b.WriteString(`<w:rStyle w:val="Hyperlink"/>`)
	}
	if f.Font != "" {
		n := xmlEscape(f.Font)
		fmt.Fprintf(&b, `<w:rFonts w:ascii="%s" w:hAnsi="%s" w:eastAsia="%s" w:cs="%s"/>`, n, n, n, n)
	}
	if f.Bold {
		b.WriteString("<w:b/><w:bCs/>")
	}
	if f.Italic {
		b.WriteString("<w:i/><w:iCs/>")
	}
	if f.Color != "" && !link {
		fmt.Fprintf(&b, `<w:color w:val="%s"/>`, strings.TrimPrefix(f.Color, "#"))
	}
	if f.Size > 0 {
		fmt.Fprintf(&b, `<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, f.Size/50, f.Size/50)
	}
	if rtl {
		b.WriteString("<w:rtl/>")
	}
	b.WriteString("</w:rPr>")
	return b.String()
}

func (w *docxWriter) inlines(b *strings.Builder, inlines []Inline, rtl, link bool) {
	for _, in := range inlines {
		switch v := in.(type) {
		case *Text:
			fmt.Fprintf(b, `<w:r>%s<w:t xml:space="preserve">%s</w:t></w:r>`, runProps(v.Format, rtl && direction(v.Text) < 0, link), xmlEscape(v.Text))
		case *Tab:
			fmt.Fprintf(b, `<w:r>%s<w:tab/></w:r>`, runProps(v.Format, false, link))
		case *LineBreak:
			fmt.Fprintf(b, `<w:r>%s<w:br/></w:r>`, runProps(v.Format, false, link))
		case *Link:
			id := w.rel("hyperlink", v.Target, true)
			fmt.Fprintf(b, `<w:hyperlink r:id="%s">`, id)
			w.inlines(b, v.Content, rtl, true)
			b.WriteString("</w:hyperlink>")
		case *Image:
			w.image(b, v)
		}
	}
}

func (w *docxWriter) image(b *strings.Builder, img *Image) {
	id, ok := w.media[img.Path]
	if !ok {
		data, err := os.ReadFile(img.Path)
		if err != nil {
			return
		}
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(img.Path), "."))
		if ext != "png" && ext != "jpg" && ext != "jpeg" {
			return
		}
		w.pics++
		name := fmt.Sprintf("media/image%d.%s", w.pics, ext)
		w.files["word/"+name] = data
		id = w.rel("image", name, false)
		w.media[img.Path] = id
	}
	cx, cy := int64(img.Width)*127, int64(img.Height)*127
	if cx <= 0 || cy <= 0 {
		if f, err := os.Open(img.Path); err == nil {
			if c, _, err := image.DecodeConfig(f); err == nil {
				cx, cy = int64(c.Width)*9525, int64(c.Height)*9525
			}
			f.Close()
		}
	}
	w.nextID++
	n := w.nextID
	graphic := fmt.Sprintf(`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic><pic:nvPicPr><pic:cNvPr id="%d" name="Picture %d"/><pic:cNvPicPr/></pic:nvPicPr>`+
		`<pic:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>`+
		`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic></a:graphicData></a:graphic>`,
		n, n, id, cx, cy)
	if img.Behind {
		// The scanned page behind the text, at the paper's top left corner.
		fmt.Fprintf(b, `<w:r><w:drawing><wp:anchor distT="0" distB="0" distL="0" distR="0" simplePos="0" relativeHeight="0" behindDoc="1" locked="0" layoutInCell="1" allowOverlap="1">`+
			`<wp:simplePos x="0" y="0"/><wp:positionH relativeFrom="page"><wp:posOffset>0</wp:posOffset></wp:positionH><wp:positionV relativeFrom="page"><wp:posOffset>0</wp:posOffset></wp:positionV>`+
			`<wp:extent cx="%d" cy="%d"/><wp:effectExtent l="0" t="0" r="0" b="0"/><wp:wrapNone/><wp:docPr id="%d" name="Page %d"/><wp:cNvGraphicFramePr/>%s</wp:anchor></w:drawing></w:r>`,
			cx, cy, n, n, graphic)
		return
	}
	fmt.Fprintf(b, `<w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0"><wp:extent cx="%d" cy="%d"/><wp:docPr id="%d" name="Picture %d"/><wp:cNvGraphicFramePr><a:graphicFrameLocks noChangeAspect="1"/></wp:cNvGraphicFramePr>%s</wp:inline></w:drawing></w:r>`,
		cx, cy, n, n, graphic)
}

func borderXML(name string, br Border) string {
	if br.Style == BorderNone {
		return fmt.Sprintf(`<w:%s w:val="nil"/>`, name)
	}
	val := "single"
	if br.Style == BorderDash {
		val = "dashed"
	}
	// Eighths of a point.
	sz := max(2, min(96, int(br.WidthMm/25.4*72*8+0.5)))
	color := strings.TrimPrefix(br.Color, "#")
	if color == "" {
		color = "000000"
	}
	return fmt.Sprintf(`<w:%s w:val="%s" w:sz="%d" w:space="0" w:color="%s"/>`, name, val, sz, color)
}

func (w *docxWriter) table(b *strings.Builder, t *Table) {
	columns := t.ColumnCount
	b.WriteString("<w:tbl><w:tblPr>")
	switch {
	case t.Align == AlignCenter:
		b.WriteString(`<w:jc w:val="center"/>`)
	case t.Indent > 0:
		fmt.Fprintf(b, `<w:tblInd w:w="%d" w:type="dxa"/>`, twips(t.Indent))
	}
	fmt.Fprintf(b, `<w:tblLayout w:type="fixed"/><w:tblCellMar><w:top w:w="%d" w:type="dxa"/><w:left w:w="%d" w:type="dxa"/><w:bottom w:w="%d" w:type="dxa"/><w:right w:w="%d" w:type="dxa"/></w:tblCellMar>`,
		twips(t.CellMargin.Top), twips(t.CellMargin.Left), twips(t.CellMargin.Bottom), twips(t.CellMargin.Right))
	b.WriteString("</w:tblPr><w:tblGrid>")
	for _, cw := range t.ColumnWidths {
		fmt.Fprintf(b, `<w:gridCol w:w="%d"/>`, twips(cw))
	}
	b.WriteString("</w:tblGrid>")
	// Cells by their place; vertically merged cells continue in the rows below.
	type placed struct {
		cell  *Cell
		start bool
	}
	grid := make([][]*placed, len(t.Rows))
	for r := range grid {
		grid[r] = make([]*placed, columns)
	}
	for r, row := range t.Rows {
		col := 0
		for _, c := range row.Cells {
			if c.Column >= 0 {
				col = c.Column
			}
			for col < columns && grid[r][col] != nil {
				col++
			}
			if col >= columns {
				break
			}
			for rr := r; rr < min(len(t.Rows), r+max(1, c.RowSpan)); rr++ {
				grid[rr][col] = &placed{c, rr == r}
				for cc := col + 1; cc < min(columns, col+max(1, c.ColSpan)); cc++ {
					grid[rr][cc] = &placed{} // covered by the span
				}
			}
			col += max(1, c.ColSpan)
		}
	}
	for r, row := range t.Rows {
		b.WriteString("<w:tr>")
		if row.Height > 0 {
			fmt.Fprintf(b, `<w:trPr><w:trHeight w:val="%d" w:hRule="atLeast"/></w:trPr>`, twips(row.Height))
		}
		for col := 0; col < columns; col++ {
			p := grid[r][col]
			if p != nil && p.cell == nil {
				continue // inside a horizontal span
			}
			var c *Cell
			span := 1
			if p != nil {
				c, span = p.cell, max(1, p.cell.ColSpan)
			}
			width := 0
			for k := col; k < min(columns, col+span) && k < len(t.ColumnWidths); k++ {
				width += t.ColumnWidths[k]
			}
			fmt.Fprintf(b, `<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/>`, twips(width))
			if span > 1 {
				fmt.Fprintf(b, `<w:gridSpan w:val="%d"/>`, span)
			}
			if c != nil && c.RowSpan > 1 {
				if p.start {
					b.WriteString(`<w:vMerge w:val="restart"/>`)
				} else {
					b.WriteString(`<w:vMerge/>`)
				}
			}
			if c != nil {
				b.WriteString("<w:tcBorders>" + borderXML("top", c.Borders.Top) + borderXML("start", c.Borders.Left) +
					borderXML("bottom", c.Borders.Bottom) + borderXML("end", c.Borders.Right) + "</w:tcBorders>")
				if c.Fill != "" {
					fmt.Fprintf(b, `<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, strings.TrimPrefix(c.Fill, "#"))
				}
				switch c.VerticalAlign {
				case VAlignCenter:
					b.WriteString(`<w:vAlign w:val="center"/>`)
				case VAlignBottom:
					b.WriteString(`<w:vAlign w:val="bottom"/>`)
				}
			}
			b.WriteString("</w:tcPr>")
			wrote := false
			if c != nil && p.start {
				for _, blk := range c.Blocks {
					switch v := blk.(type) {
					case *Paragraph:
						w.paragraph(b, v, "")
						wrote = true
					case *Table:
						w.table(b, v)
					}
				}
			}
			if !wrote {
				b.WriteString("<w:p/>")
			}
			b.WriteString("</w:tc>")
			col += span - 1
		}
		b.WriteString("</w:tr>")
	}
	b.WriteString("</w:tbl>")
}
