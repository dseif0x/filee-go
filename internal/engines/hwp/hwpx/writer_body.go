package hwpx

// Section parts: page setup, columns, headers/footers, paragraphs, runs and tables.
//
// 한글 keeps page setup (secPr) and the column definition (colPr) in the first run of a section's first
// paragraph; header and footer controls follow in the same run. Page and column breaks are paragraph attributes.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
)

func attrInt(e interface{ A(string) string }, name string, fallback int) int {
	if v, err := strconv.Atoi(e.A(name)); err == nil {
		return v
	}
	return fallback
}

func (w *writer) section(section *doc.Section) (string, error) {
	template := w.templateSection.FirstDescendantNS(nsHP, "secPr")
	if template == nil {
		return "", errors.New("the template section has no secPr")
	}
	secPr := template.Copy()
	pagePr := secPr.ChildNS(nsHP, "pagePr")
	margin := pagePr.ChildNS(nsHP, "margin")
	if page := section.Page; page != nil && pagePr != nil && margin != nil {
		// Width/height always describe the portrait sheet; "NARROWLY" turns it (landscape), "WIDELY" does not.
		pagePr.SetAttr("landscape", map[bool]string{true: "NARROWLY", false: "WIDELY"}[page.Landscape()])
		pagePr.SetAttr("width", itoa(min(page.Width, page.Height)))
		pagePr.SetAttr("height", itoa(max(page.Width, page.Height)))
		margin.SetAttr("left", itoa(page.Left))
		margin.SetAttr("right", itoa(page.Right))
		margin.SetAttr("top", itoa(page.Top))
		margin.SetAttr("bottom", itoa(page.Bottom))
		margin.SetAttr("header", itoa(page.Header))
		margin.SetAttr("footer", itoa(page.Footer))
		margin.SetAttr("gutter", itoa(page.Gutter))
	}
	if visibility := secPr.ChildNS(nsHP, "visibility"); visibility != nil {
		visibility.SetAttr("hideFirstHeader", b01(section.HideFirstHeader))
		visibility.SetAttr("hideFirstFooter", b01(section.HideFirstFooter))
	}
	if startNum := secPr.ChildNS(nsHP, "startNum"); startNum != nil {
		startNum.SetAttr("page", itoa(val(section.StartPageNumber, 0)))
	}

	m := func(name string, fallback int) int {
		if margin == nil {
			return fallback
		}
		return attrInt(margin, name, fallback)
	}
	paperWidth, paperHeight, landscape := 59530, 84190, false
	if pagePr != nil {
		paperWidth = attrInt(pagePr, "width", 59530)
		paperHeight = attrInt(pagePr, "height", 84190)
		landscape = pagePr.A("landscape") == "NARROWLY"
	}
	sheet := paperWidth
	if landscape {
		sheet = paperHeight
	}
	textWidth := max(2000, sheet-m("left", 7200)-m("right", 7200)-m("gutter", 0))
	w.pageLeft = m("left", 7200) + m("gutter", 0)
	w.pageTop = m("top", 4255) + m("header", 4250)

	var setup strings.Builder
	setup.WriteString(secPr.String())
	setup.WriteString("<hp:ctrl>" + columnDefinition(section.Columns) + "</hp:ctrl>")

	w.base = w.normal
	w.width = textWidth
	controlID := 1
	for _, header := range section.Headers {
		setup.WriteString(w.headerFooter("header", header, controlID, textWidth, m("header", 4250), "TOP"))
		controlID++
	}
	for _, footer := range section.Footers {
		setup.WriteString(w.headerFooter("footer", footer, controlID, textWidth, m("footer", 2240), "BOTTOM"))
		controlID++
	}

	w.width = columnWidth(textWidth, section.Columns)
	var body strings.Builder
	body.WriteString(w.sectionOpenTag())
	blocks := section.Blocks
	if len(blocks) == 0 {
		blocks = []doc.Block{&doc.Paragraph{}}
	} else if _, ok := blocks[0].(*doc.Paragraph); !ok {
		blocks = append([]doc.Block{&doc.Paragraph{}}, blocks...)
	}
	for i, block := range blocks {
		if p, ok := block.(*doc.Paragraph); ok && p.ColumnsChange != nil {
			w.width = columnWidth(textWidth, *p.ColumnsChange)
		}
		lead := ""
		if i == 0 {
			lead = `<hp:run charPrIDRef="` + w.normal.charPr + `">` + setup.String() + "</hp:run>"
		}
		body.WriteString(w.block(block, lead))
	}
	body.WriteString("</hs:sec>")
	return body.String(), nil
}

func b01(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func columnWidth(textWidth int, columns doc.Columns) int {
	if columns.Count <= 1 {
		return textWidth
	}
	return max(2000, (textWidth-columns.Gap*(columns.Count-1))/columns.Count)
}

func columnDefinition(columns doc.Columns) string {
	line := ""
	if columns.Separator {
		line = `<hp:colLine type="SOLID" width="0.12 mm" color="#000000"/>`
	}
	gap := 0
	if columns.Count > 1 {
		gap = columns.Gap
	}
	return fmt.Sprintf(`<hp:colPr id="" type="NEWSPAPER" layout="LEFT" colCount="%d" sameSz="1" sameGap="%d">%s</hp:colPr>`, max(1, columns.Count), gap, line)
}

func (w *writer) headerFooter(kind string, item *doc.HeaderFooter, id, textWidth, textHeight int, verticalAlign string) string {
	pages := "BOTH"
	switch item.Pages {
	case doc.EvenPages:
		pages = "EVEN"
	case doc.OddPages:
		pages = "ODD"
	}
	return fmt.Sprintf(`<hp:ctrl><hp:%s id="%d" applyPageType="%s">`, kind, id, pages) +
		w.subList(item.Blocks, verticalAlign, textWidth, textHeight, "") +
		"</hp:" + kind + "></hp:ctrl>"
}

// subList is the paragraph list of a header, footer, cell, text box or note.
func (w *writer) subList(blocks []doc.Block, verticalAlign string, textWidth, textHeight int, leadingRun string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, `<hp:subList id="" textDirection="HORIZONTAL" lineWrap="BREAK" vertAlign="%s" linkListIDRef="0" linkListNextIDRef="0" textWidth="%d" textHeight="%d" hasTextRef="0" hasNumRef="0">`, verticalAlign, textWidth, textHeight)
	list := blocks
	if len(blocks) == 0 {
		list = []doc.Block{&doc.Paragraph{}}
	} else if _, ok := blocks[0].(*doc.Paragraph); !ok && leadingRun != "" {
		list = append([]doc.Block{&doc.Paragraph{}}, blocks...)
	}
	for i, b := range list {
		lead := ""
		if i == 0 {
			lead = leadingRun
		}
		sb.WriteString(w.block(b, lead))
	}
	sb.WriteString("</hp:subList>")
	return sb.String()
}

// ───────────────────────── Blocks ─────────────────────────

// block writes a paragraph or table; leadingRun is a raw run placed first in the paragraph (section setup, note number).
func (w *writer) block(block doc.Block, leadingRun string) string {
	switch b := block.(type) {
	case *doc.Paragraph:
		return w.paragraph(b, leadingRun)
	case *doc.Table:
		return w.table(b, leadingRun)
	}
	return ""
}

func (w *writer) styleOf(p *doc.Paragraph) styleIDs {
	if p.HeadingLevel > 0 {
		if h, ok := w.headings[p.HeadingLevel]; ok {
			return h
		}
	}
	return w.base
}

func (w *writer) paragraph(p *doc.Paragraph, leadingRun string) string {
	w.plain.WriteString("\n")
	style := w.styleOf(p)
	paraPrID := w.paraPr(style.paraPr, p.Format, p.List)

	runs := &runBuilder{}
	if p.ColumnsChange != nil {
		runs.control(w.normal.charPr, "<hp:ctrl>"+columnDefinition(*p.ColumnsChange)+"</hp:ctrl>", false)
	}
	size := largestSize(p.Inlines)
	if size == 0 {
		size = val(p.MarkFormat.Size, w.baseSize(style.charPr))
	}
	tabs := tabElements(p, size)
	w.inlines(p.Inlines, runs, style.charPr, false, &tabs)

	var sb strings.Builder
	fmt.Fprintf(&sb, `<hp:p id="0" paraPrIDRef="%s" styleIDRef="%s" pageBreak="%s" columnBreak="%s" merged="0">`, paraPrID, style.style, b01(p.PageBreakBefore), b01(p.ColumnBreakBefore))
	sb.WriteString(leadingRun)
	sb.WriteString(runs.finish(w.charPr(style.charPr, p.MarkFormat, false), leadingRun != ""))
	sb.WriteString("</hp:p>")
	return sb.String()
}

// runBuilder collects runs: consecutive content with the same character shape shares one hp:run and one hp:t, and
// controls (fields, bookmarks) and objects (pictures, tables, text boxes) go between the text parts.
type runBuilder struct {
	output        strings.Builder
	items         strings.Builder
	charPr        string
	open          bool // a charPr is current
	textOpen      bool
	endsWithObject bool
}

func (r *runBuilder) text(charPr, xml string) {
	r.switchTo(charPr)
	if !r.textOpen {
		r.items.WriteString("<hp:t>")
		r.textOpen = true
	}
	r.items.WriteString(xml)
	r.endsWithObject = false
}

func (r *runBuilder) control(charPr, xml string, isObject bool) {
	r.switchTo(charPr)
	r.closeText()
	r.items.WriteString(xml)
	r.endsWithObject = isObject
}

// finish returns the runs; an empty paragraph still needs one run for 한글 to show it.
func (r *runBuilder) finish(emptyCharPr string, hasLeadingRun bool) string {
	r.flush()
	if r.output.Len() == 0 && !hasLeadingRun {
		return `<hp:run charPrIDRef="` + emptyCharPr + `"><hp:t/></hp:run>`
	}
	return r.output.String()
}

func (r *runBuilder) switchTo(charPr string) {
	if r.open && r.charPr == charPr {
		return
	}
	r.flush()
	r.charPr = charPr
	r.open = true
}

func (r *runBuilder) closeText() {
	if r.textOpen {
		r.items.WriteString("</hp:t>")
	}
	r.textOpen = false
}

func (r *runBuilder) flush() {
	if !r.open {
		return
	}
	r.closeText()
	if r.endsWithObject {
		r.items.WriteString("<hp:t/>") // as 한글 writes it after an object
	}
	r.output.WriteString(`<hp:run charPrIDRef="` + r.charPr + `">`)
	r.output.WriteString(r.items.String())
	r.output.WriteString("</hp:run>")
	r.items.Reset()
	r.open = false
	r.endsWithObject = false
}

// inlines writes inlines; tabs are the pre-computed inline tab elements of the paragraph, in order.
func (w *writer) inlines(inlines []doc.Inline, runs *runBuilder, charPrBase string, link bool, tabs *[]string) {
	for _, inline := range inlines {
		switch v := inline.(type) {
		case *doc.Equation:
			if m, ok := v.Math.(MathNode); ok {
				runs.control(w.charPr(charPrBase, v.Text.Format, link), w.equation(m, v.Text.Format, v.Text.Text, charPrBase), true)
			} else {
				runs.text(w.charPr(charPrBase, v.Text.Format, link), escape(v.Text.Text))
			}
			w.plain.WriteString(v.Text.Text)
		case *doc.Text:
			runs.text(w.charPr(charPrBase, v.Format, link), escape(v.Text))
			w.plain.WriteString(v.Text)
		case *doc.LineBreak:
			runs.text(w.charPr(charPrBase, v.Format, link), "<hp:lineBreak/>")
			w.plain.WriteString("\n")
		case *doc.Tab:
			tab := `<hp:tab width="4000" leader="0" type="1"/>`
			if len(*tabs) > 0 {
				tab = (*tabs)[0]
				*tabs = (*tabs)[1:]
			}
			runs.text(w.charPr(charPrBase, v.Format, link), tab)
			w.plain.WriteString("\t")
		case *doc.Link:
			id := w.nextID()
			var format doc.CharFormat
			for _, c := range v.Content {
				if _, f, ok := textOf(c); ok {
					format = f
					break
				}
			}
			charPr := w.charPr(charPrBase, format, true)
			runs.control(charPr, fieldBegin(id, v.Target), false)
			w.inlines(v.Content, runs, charPrBase, true, tabs)
			runs.control(charPr, fmt.Sprintf(`<hp:ctrl><hp:fieldEnd beginIDRef="%d" fieldid="%s"/></hp:ctrl>`, id, hyperlinkFieldID), false)
		case *doc.Bookmark:
			runs.control(charPrBase, `<hp:ctrl><hp:bookmark name="`+escape(v.Name)+`"/></hp:ctrl>`, false)
		case *doc.Field:
			kind := "TOTAL_PAGE"
			if v.Kind == doc.PageNumberField {
				kind = "PAGE"
			}
			runs.control(w.charPr(charPrBase, v.Format, false), autoNumber(kind, 1, ""), false)
		case *doc.Note:
			runs.control(charPrBase, w.note(v), false)
		case *doc.Image:
			if picture := w.picture(v); picture != "" {
				runs.control(charPrBase, picture, true)
			}
		case *doc.TextBox:
			runs.control(charPrBase, w.textBox(v), true)
		case *doc.Shape:
			runs.control(charPrBase, w.shape(v), true)
		}
	}
}

// ───────────────────────── Tables ─────────────────────────

var defaultCellMargin = doc.Insets{Left: 510, Right: 510, Top: 141, Bottom: 141}

func (w *writer) table(table *doc.Table, leadingRun string) string {
	if len(table.Rows) == 0 {
		if leadingRun == "" {
			return ""
		}
		return w.paragraph(&doc.Paragraph{}, leadingRun)
	}
	placed, columnCount := doc.PlaceCells(table)
	available := max(4000, w.width-table.Indent)
	widths := doc.ColumnWidths(table, columnCount, available)
	totalWidth := sum(widths)
	rowHeights := w.estimateRowHeights(table, placed, widths)
	margin := defaultCellMargin
	if table.CellMargin != nil {
		margin = *table.CellMargin
	}
	tableBorders := doc.GridBorders
	if table.Borders != nil {
		tableBorders = *table.Borders
	}
	headerRows := 0
	for _, r := range table.Rows {
		if !r.Header {
			break
		}
		headerRows++
	}

	var sb strings.Builder
	id := w.nextID()
	fmt.Fprintf(&sb, `<hp:tbl id="%d" zOrder="0" numberingType="TABLE" textWrap="TOP_AND_BOTTOM" textFlow="BOTH_SIDES" lock="0" dropcapstyle="None" pageBreak="CELL" repeatHeader="%s" rowCnt="%d" colCnt="%d" cellSpacing="0" borderFillIDRef="%s" noAdjust="0">`,
		id, b01(headerRows > 0), len(table.Rows), columnCount, w.borderFillID(tableBorders, ""))
	fmt.Fprintf(&sb, `<hp:sz width="%d" widthRelTo="ABSOLUTE" height="%d" heightRelTo="ABSOLUTE" protect="0"/>`, totalWidth, sum(rowHeights))
	horizontalAlign := "LEFT"
	switch table.Align {
	case doc.AlignCenter:
		horizontalAlign = "CENTER"
	case doc.AlignRight:
		horizontalAlign = "RIGHT"
	}
	offset := 0
	if table.Align == doc.AlignLeft {
		offset = table.Indent
	}
	fmt.Fprintf(&sb, `<hp:pos treatAsChar="0" affectLSpacing="0" flowWithText="1" allowOverlap="0" holdAnchorAndSO="0" vertRelTo="PARA" horzRelTo="COLUMN" vertAlign="TOP" horzAlign="%s" vertOffset="0" horzOffset="%d"/>`, horizontalAlign, offset)
	bottom := 1417
	if table.Tight {
		bottom = 0
	}
	fmt.Fprintf(&sb, `<hp:outMargin left="0" right="0" top="0" bottom="%d"/>`, bottom)
	fmt.Fprintf(&sb, `<hp:inMargin left="%d" right="%d" top="%d" bottom="%d"/>`, margin.Left, margin.Right, margin.Top, margin.Bottom)

	outerWidth := w.width
	for r, row := range placed {
		sb.WriteString("<hp:tr>")
		for _, pc := range row {
			cell, column := pc.Cell, pc.Column
			cellMargin := margin
			if cell.CellMargin != nil {
				cellMargin = *cell.CellMargin
			}
			width := 0
			for i := column; i < column+cell.ColSpan && i < len(widths); i++ {
				width += widths[i]
			}
			height := 0
			for i := r; i < r+cell.RowSpan && i < len(rowHeights); i++ {
				height += rowHeights[i]
			}
			w.width = max(1000, width-cellMargin.Left-cellMargin.Right)
			verticalAlign := "TOP"
			switch cell.VerticalAlign {
			case doc.VAlignCenter:
				verticalAlign = "CENTER"
			case doc.VAlignBottom:
				verticalAlign = "BOTTOM"
			}
			borders := tableBorders
			if cell.Borders != nil {
				borders = *cell.Borders
			}
			fmt.Fprintf(&sb, `<hp:tc name="" header="%s" hasMargin="%s" protect="0" editable="0" dirty="0" borderFillIDRef="%s">`, b01(r < headerRows), b01(cell.CellMargin != nil), w.borderFillID(borders, cell.Fill))
			sb.WriteString(w.subList(cell.Blocks, verticalAlign, 0, 0, ""))
			fmt.Fprintf(&sb, `<hp:cellAddr colAddr="%d" rowAddr="%d"/>`, column, r)
			fmt.Fprintf(&sb, `<hp:cellSpan colSpan="%d" rowSpan="%d"/>`, cell.ColSpan, cell.RowSpan)
			// Like 한글 itself: a merged cell is as tall as all the rows it spans.
			fmt.Fprintf(&sb, `<hp:cellSz width="%d" height="%d"/>`, width, height)
			fmt.Fprintf(&sb, `<hp:cellMargin left="%d" right="%d" top="%d" bottom="%d"/>`, cellMargin.Left, cellMargin.Right, cellMargin.Top, cellMargin.Bottom)
			sb.WriteString("</hp:tc>")
		}
		sb.WriteString("</hp:tr>")
	}
	w.width = outerWidth
	sb.WriteString("</hp:tbl>")

	// The paragraph holding the table has a text line under it; a tight table's is 1 pt with no spacing.
	paraPrID, charPrID := w.base.paraPr, w.base.charPr
	if table.Tight {
		paraPrID = w.paraPr(w.base.paraPr, doc.ParaFormat{Before: ptr(0), After: ptr(0), LineSpacing: &doc.LineSpacing{Kind: doc.SpacingPercent, Value: 100}}, nil)
		charPrID = w.charPr(w.base.charPr, doc.CharFormat{Size: ptr(100)}, false)
	}
	runs := &runBuilder{}
	runs.control(charPrID, sb.String(), true)
	paragraph := fmt.Sprintf(`<hp:p id="0" paraPrIDRef="%s" styleIDRef="%s" pageBreak="%s" columnBreak="0" merged="0">%s%s</hp:p>`,
		paraPrID, w.base.style, b01(table.PageBreakBefore), leadingRun, runs.finish(charPrID, leadingRun != ""))
	for _, b := range table.Caption {
		paragraph += w.block(b, "")
	}
	return paragraph
}

func sum(values []int) int {
	t := 0
	for _, v := range values {
		t += v
	}
	return t
}
