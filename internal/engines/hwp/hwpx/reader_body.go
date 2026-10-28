package hwpx

// Paragraphs, runs and their content: text with tabs and line breaks, fields (hyperlinks), bookmarks, notes, page
// numbers, section controls and tables. 한글 keeps tables inside runs like characters; in the model a table is a
// block of its own, so the paragraph around it is split.

import (
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/hwp/xmltree"
)

type openField struct {
	id     string
	target string // "" = not a hyperlink
}

type bodyState struct {
	// fields open in the current paragraph list (fieldBegin … fieldEnd); hyperlinks may span paragraphs.
	fields []openField
	// markpen is the highlighter colour between hp:markpenBegin and hp:markpenEnd ("" = none).
	markpen string
	// deleted is inside tracked deleted text (hp:deleteBegin … hp:deleteEnd), which the document does not show.
	deleted bool
	// outlineNumberingID numbers outline paragraphs (개요) in the current section (secPr outlineShapeIDRef).
	outlineNumberingID string
}

// readSubList reads the paragraphs and tables of a paragraph list (cell, header, footer, note, text box, caption).
func (r *hwpxReader) readSubList(subList *xmltree.Element) []doc.Block {
	var blocks []doc.Block
	if subList == nil {
		return blocks
	}
	// Fields, highlighting and tracked deletions do not reach into or out of a nested paragraph list.
	saved := r.body
	r.body.fields, r.body.markpen, r.body.deleted = nil, "", false
	for _, e := range elements(subList) {
		if e.Local == "p" {
			blocks = r.readParagraph(e, blocks, nil)
		} else {
			r.skip(e.Local)
		}
	}
	r.body.fields, r.body.markpen, r.body.deleted = saved.fields, saved.markpen, saved.deleted
	return blocks
}

// ───────────────────────── Paragraphs ─────────────────────────

// paragraphBuilder builds the block(s) of one hp:p: a table inside it ends the paragraph so far, becomes a block, and
// the text after it continues in a new paragraph with the same format.
type paragraphBuilder struct {
	output     []doc.Block
	reader     *hwpxReader
	sinks      []*doc.Link
	format     doc.ParaFormat
	heading    int
	list       *doc.ListRef
	afterTable bool
	current    *doc.Paragraph
	// markFormat is the format of the last run, used for the paragraph mark (the height of an empty paragraph).
	markFormat doc.CharFormat
}

func (b *paragraphBuilder) add(in doc.Inline) {
	if n := len(b.sinks); n > 0 {
		b.sinks[n-1].Content = append(b.sinks[n-1].Content, in)
	} else {
		b.current.Inlines = append(b.current.Inlines, in)
	}
}

func (b *paragraphBuilder) sink() []doc.Inline {
	if n := len(b.sinks); n > 0 {
		return b.sinks[n-1].Content
	}
	return b.current.Inlines
}

func (b *paragraphBuilder) setLast(in doc.Inline) {
	if n := len(b.sinks); n > 0 {
		b.sinks[n-1].Content[len(b.sinks[n-1].Content)-1] = in
	} else {
		b.current.Inlines[len(b.current.Inlines)-1] = in
	}
}

// addText adds text, joined to the previous text when it has the same format.
func (b *paragraphBuilder) addText(text string, format doc.CharFormat) {
	if text == "" {
		return
	}
	if s := b.sink(); len(s) > 0 {
		if last, ok := s[len(s)-1].(*doc.Text); ok && last.Format.Equal(format) {
			b.setLast(&doc.Text{Text: last.Text + text, Format: format})
			return
		}
	}
	b.add(&doc.Text{Text: text, Format: format})
}

func (b *paragraphBuilder) openLink(target string) {
	link := &doc.Link{Target: target}
	b.add(link)
	b.sinks = append(b.sinks, link)
}

func (b *paragraphBuilder) closeLink() {
	if n := len(b.sinks); n > 0 {
		b.sinks = b.sinks[:n-1]
	}
}

func (b *paragraphBuilder) table(t *doc.Table) {
	// Breaks and column changes have no place on a table, so they keep an empty paragraph in front of it.
	if b.hasContent() || b.current.PageBreakBefore || b.current.ColumnBreakBefore || b.current.ColumnsChange != nil {
		b.emit()
	}
	b.output = append(b.output, t)
	// The rest of a list item is not numbered again.
	var list *doc.ListRef
	if b.list != nil {
		l := *b.list
		l.Numbered = false
		list = &l
	}
	b.newParagraph(list)
	b.afterTable = true
}

func (b *paragraphBuilder) finish() {
	if !b.afterTable || b.hasContent() {
		b.emit()
	}
}

func (b *paragraphBuilder) hasContent() bool {
	for _, in := range b.current.Inlines {
		if l, ok := in.(*doc.Link); ok && len(l.Content) == 0 {
			continue
		}
		return true
	}
	return false
}

func (b *paragraphBuilder) emit() {
	b.current.MarkFormat = b.markFormat
	b.output = append(b.output, b.current)
}

func (b *paragraphBuilder) newParagraph(list *doc.ListRef) {
	b.current = &doc.Paragraph{Format: b.format, HeadingLevel: b.heading, List: list}
	b.sinks = nil
	// A hyperlink that started in an earlier paragraph (or before a table) continues here.
	for _, f := range b.reader.body.fields {
		if f.target != "" {
			b.openLink(f.target)
		}
	}
}

func (r *hwpxReader) readParagraph(p *xmltree.Element, output []doc.Block, section *sectionState) []doc.Block {
	shape := r.paraShapeOf(p.A("paraPrIDRef"))
	heading, list := r.headingAndList(shape, p.A("styleIDRef"))
	b := &paragraphBuilder{output: output, reader: r, format: shape.format, heading: heading, list: list, markFormat: r.charFormat("")}
	b.newParagraph(list)
	b.current.PageBreakBefore = flag(p, "pageBreak", false) || shape.pageBreakBefore
	b.current.ColumnBreakBefore = flag(p, "columnBreak", false)
	for _, c := range elements(p) {
		switch c.Local {
		case "run":
			r.run(c, b, section)
		case "linesegarray":
			// layout cache, computed again by every reader
		default:
			r.skip(c.Local)
		}
	}
	b.finish()
	return b.output
}

// headingAndList are the heading level and list of a paragraph shape. Outline paragraphs (개요) are headings; when
// the section's outline numbering shows a number for their level (1., 가., ...), they are numbered too.
func (r *hwpxReader) headingAndList(shape *paraShape, styleID string) (int, *doc.ListRef) {
	heading := 0
	var list *doc.ListRef
	switch shape.headingType {
	case "OUTLINE":
		heading = min(shape.headingLevel+1, 9)
		id := shape.headingID
		if id == "" || id == "0" {
			id = r.body.outlineNumberingID
		}
		if n := r.numbering(id); n != nil && shape.headingLevel < len(n.Levels) && hasCounter(n.Levels[shape.headingLevel].Text) {
			list = &doc.ListRef{Numbering: n, Level: shape.headingLevel, Numbered: true}
		}
	case "NUMBER", "NUMBERING", "BULLET":
		var n *doc.Numbering
		if shape.headingType == "BULLET" {
			n = r.bullet(shape.headingID)
		} else {
			n = r.numbering(shape.headingID)
		}
		if n != nil && len(n.Levels) > 0 {
			list = &doc.ListRef{Numbering: n, Level: min(shape.headingLevel, len(n.Levels)-1), Numbered: true}
		}
	}
	if heading > 0 {
		return heading, list
	}
	return r.styleHeadingLevel(styleID), list
}

// ───────────────────────── Runs ─────────────────────────

func (r *hwpxReader) run(run *xmltree.Element, b *paragraphBuilder, section *sectionState) {
	format := r.charFormat(run.A("charPrIDRef"))
	b.markFormat = format
	for _, item := range elements(run) {
		switch item.Local {
		case "t":
			r.text(item, b, format)
		case "secPr":
			// read by readTopParagraph
		case "ctrl":
			for _, control := range elements(item) {
				r.control(control, b, format, section)
			}
		case "tbl":
			b.table(r.table(item))
		case "colPr", "header", "footer", "footNote", "endNote", "fieldBegin", "fieldEnd", "bookmark", "autoNum", "newNum", "pageNum":
			r.control(item, b, format, section) // controls outside hp:ctrl (other producers)
		default:
			r.drawingObject(item, b, format)
		}
	}
}

func (r *hwpxReader) highlighted(format doc.CharFormat) doc.CharFormat {
	if r.body.markpen == "" {
		return format
	}
	format.Shade = ptr(r.body.markpen)
	return format
}

// text reads hp:t: text mixed with tabs, line breaks, special spaces, highlighter and change-tracking marks.
func (r *hwpxReader) text(t *xmltree.Element, b *paragraphBuilder, format doc.CharFormat) {
	for _, node := range t.Kids {
		if txt, ok := node.(*xmltree.Text); ok {
			value := txt.Data
			// 한글 never stores a line break as a character (hp:lineBreak does that), so line ends are only the
			// indentation of pretty-printed files.
			if strings.ContainsAny(value, "\r\n") {
				if strings.TrimSpace(value) == "" {
					continue
				}
				value = strings.ReplaceAll(strings.ReplaceAll(value, "\r", ""), "\n", "")
			}
			if !r.body.deleted {
				b.addText(value, r.highlighted(format))
			}
			continue
		}
		element, ok := node.(*xmltree.Element)
		if !ok {
			continue
		}
		switch element.Local {
		case "tab":
			if !r.body.deleted {
				b.add(&doc.Tab{Format: r.highlighted(format)})
			}
		case "lineBreak", "columnBreak":
			if !r.body.deleted {
				b.add(&doc.LineBreak{Format: format})
			}
		case "nbSpace":
			if !r.body.deleted {
				b.addText("\u00A0", r.highlighted(format))
			}
		case "fwSpace":
			if !r.body.deleted {
				b.addText("\u2007", r.highlighted(format)) // fixed-width space: as wide as a digit
			}
		case "hyphen":
			if !r.body.deleted {
				b.addText("\u00AD", r.highlighted(format)) // soft hyphen, shown only at a line end
			}
		case "markpenBegin":
			r.body.markpen = hwpColor(element.A("color"))
			if r.body.markpen == "" {
				r.body.markpen = "#FFFF00"
			}
		case "markpenEnd":
			r.body.markpen = ""
		case "deleteBegin":
			r.body.deleted = true
		case "deleteEnd":
			r.body.deleted = false
		case "insertBegin", "insertEnd", "titleMark":
			// inserted text is shown as it is; title marks only feed a table of contents
		default:
			r.skip(element.Local)
		}
	}
}

// ───────────────────────── Controls ─────────────────────────

func (r *hwpxReader) control(control *xmltree.Element, b *paragraphBuilder, format doc.CharFormat, section *sectionState) {
	switch control.Local {
	case "colPr":
		if section == nil {
			r.skip(control.Local)
			return
		}
		if section.columnsSet {
			c := columnsOf(control)
			b.current.ColumnsChange = &c
		} else {
			section.current().Columns = columnsOf(control)
			section.columnsSet = true
		}
	case "header", "footer":
		if section == nil {
			r.skip(control.Local)
			return
		}
		r.headerFooter(control, section.current())
	case "footNote", "endNote":
		b.add(r.note(control, control.Local == "endNote"))
	case "fieldBegin":
		r.fieldBegin(control, b)
	case "fieldEnd":
		r.fieldEnd(control, b)
	case "bookmark":
		if name := control.A("name"); name != "" {
			b.add(&doc.Bookmark{Name: name})
		}
	case "autoNum":
		switch control.A("numType") {
		case "PAGE":
			b.add(&doc.Field{Kind: doc.PageNumberField, Format: format})
		case "TOTAL_PAGE":
			b.add(&doc.Field{Kind: doc.TotalPagesField, Format: format})
		case "FOOTNOTE", "ENDNOTE":
			// the note's own number, written again by every target format
		default:
			// Picture, table and equation numbers: keep the number shown when the file was saved.
			b.addText(control.A("num"), format)
		}
	case "newNum":
		if section == nil {
			r.skip(control.Local)
			return
		}
		if control.A("numType") == "PAGE" && !section.startNumberSet {
			section.current().StartPageNumber = ptr(max(1, intAttr(control, "num", 1)))
			section.startNumberSet = true
		}
	case "pageNum":
		if section == nil {
			r.skip(control.Local)
			return
		}
		section.pageNumber = control
	default:
		r.skip(control.Local) // hidden comments, index marks, page hiding, page number control, ...
	}
}

// headerFooter: 한글 lets a later header replace it from its page on, the model keeps the first.
func (r *hwpxReader) headerFooter(control *xmltree.Element, section *doc.Section) {
	pages := doc.BothPages
	switch control.A("applyPageType") {
	case "EVEN":
		pages = doc.EvenPages
	case "ODD":
		pages = doc.OddPages
	}
	target := &section.Footers
	if control.Local == "header" {
		target = &section.Headers
	}
	for _, h := range *target {
		if h.Pages == pages {
			r.skip(control.Local)
			return
		}
	}
	*target = append(*target, &doc.HeaderFooter{Pages: pages, Blocks: r.readSubList(child(control, "subList"))})
}

func (r *hwpxReader) note(control *xmltree.Element, endnote bool) *doc.Note {
	note := &doc.Note{Endnote: endnote, Blocks: r.readSubList(child(control, "subList"))}
	// The note starts with its number (an autoNum, skipped) and a space.
	if len(note.Blocks) > 0 {
		if first, ok := note.Blocks[0].(*doc.Paragraph); ok && len(first.Inlines) > 0 {
			if lead, ok := first.Inlines[0].(*doc.Text); ok && strings.HasPrefix(lead.Text, " ") {
				if len(lead.Text) == 1 {
					first.Inlines = first.Inlines[1:]
				} else {
					first.Inlines[0] = &doc.Text{Text: lead.Text[1:], Format: lead.Format}
				}
			}
		}
	}
	return note
}

// ───────────────────────── Fields ─────────────────────────

func (r *hwpxReader) fieldBegin(control *xmltree.Element, b *paragraphBuilder) {
	target := ""
	switch control.A("type") {
	case "HYPERLINK":
		target = HyperlinkTarget(control)
	case "BOOKMARK":
		if name := control.A("name"); name != "" {
			b.add(&doc.Bookmark{Name: name})
		}
	}
	// Other fields (click-here, date, file name, ...) keep their result text, which follows until fieldEnd.
	r.body.fields = append(r.body.fields, openField{control.A("id"), target})
	b.reader = r
	if target != "" {
		b.openLink(target)
	}
}

func (r *hwpxReader) fieldEnd(control *xmltree.Element, b *paragraphBuilder) {
	id, hasID := control.Attr("beginIDRef")
	index := -1
	for i := len(r.body.fields) - 1; i >= 0; i-- {
		if !hasID || r.body.fields[i].id == id {
			index = i
			break
		}
	}
	if index < 0 {
		index = len(r.body.fields) - 1
	}
	if index < 0 {
		return
	}
	field := r.body.fields[index]
	r.body.fields = append(r.body.fields[:index:index], r.body.fields[index+1:]...)
	if field.target != "" {
		b.closeLink()
	}
}

// HyperlinkTarget is the target of a HYPERLINK field, from its "Command" parameter: the target with ':', '?', ';' and
// '#' escaped by a backslash, then ";kind;..." ("url;1;0;0;"). An unescaped '?' starts a bookmark name ("?name" = a
// bookmark in this document, "file.hwp?name" one in another file).
func HyperlinkTarget(fieldBegin *xmltree.Element) string {
	parameter := func(name string) (string, bool) {
		for _, p := range descendants(fieldBegin, "stringParam") {
			if p.A("name") == name {
				return p.Value(), true
			}
		}
		return "", false
	}
	if command, _ := parameter("Command"); command != "" {
		var target strings.Builder
		rs := []rune(command)
		for i := 0; i < len(rs); i++ {
			c := rs[i]
			if c == '\\' && i+1 < len(rs) {
				i++
				target.WriteRune(rs[i])
				continue
			}
			if c == ';' {
				break
			}
			// Producers that do not escape keep a URL's query string ('?') as it is.
			if c == '?' && !strings.Contains(target.String(), "://") {
				c = '#'
			}
			target.WriteRune(c)
		}
		if t := target.String(); t != "" && t != "#" {
			return t
		}
	}
	path, _ := parameter("Path")
	return path
}

// ───────────────────────── Tables ─────────────────────────

type cellWidth struct{ column, span, width int }

func (r *hwpxReader) table(tbl *xmltree.Element) *doc.Table {
	table := &doc.Table{ColumnCount: max(0, intAttr(tbl, "colCnt", 0))}
	if pos := child(tbl, "pos"); pos != nil && !flag(pos, "treatAsChar", false) {
		switch pos.A("horzAlign") {
		case "CENTER":
			table.Align = doc.AlignCenter
		case "RIGHT":
			table.Align = doc.AlignRight
		}
		if rel, ok := pos.Attr("horzRelTo"); table.Align == doc.AlignLeft && (!ok || rel == "COLUMN" || rel == "PARA") {
			table.Indent = max(0, intAttr(pos, "horzOffset", 0))
		}
	}
	if m := child(tbl, "inMargin"); m != nil {
		table.CellMargin = &doc.Insets{Left: intAttr(m, "left", 0), Right: intAttr(m, "right", 0), Top: intAttr(m, "top", 0), Bottom: intAttr(m, "bottom", 0)}
	}
	if bf := r.borderFillOf(tbl.A("borderFillIDRef")); bf != nil {
		borders := bf.borders
		table.Borders = &borders
	}

	var widths []cellWidth
	type insets struct {
		set bool
		v   doc.Insets
	}
	cellMargins := map[insets]bool{}
	headerRows := true
	for _, tr := range children(tbl, "tr") {
		row := &doc.Row{}
		var height *int
		allHeader := true
		for _, tc := range children(tr, "tc") {
			// Some producers wrap the cell properties in hp:tcPr.
			part := func(name string) *xmltree.Element {
				if c := child(tc, name); c != nil {
					return c
				}
				return child(child(tc, "tcPr"), name)
			}
			address := part("cellAddr")
			span := part("cellSpan")
			size := part("cellSz")
			subList := child(tc, "subList")
			cell := &doc.Cell{Column: -1, ColSpan: max(1, intAttr(span, "colSpan", 1)), RowSpan: max(1, intAttr(span, "rowSpan", 1)),
				VerticalAlign: verticalAlignOf(subList.A("vertAlign"))}
			if address != nil {
				cell.Column = max(0, intAttr(address, "colAddr", 0))
			}
			if bf := r.borderFillOf(tc.A("borderFillIDRef")); bf != nil {
				borders := bf.borders
				cell.Borders, cell.Fill = &borders, bf.fill
			}
			cell.Blocks = r.readSubList(subList)
			row.Cells = append(row.Cells, cell)

			if h := intAttr(size, "height", 0); cell.RowSpan == 1 && h > 0 {
				if height == nil || h < *height {
					height = ptr(h)
				}
			}
			if cell.Column >= 0 {
				widths = append(widths, cellWidth{cell.Column, cell.ColSpan, intAttr(size, "width", 0)})
			}
			allHeader = allHeader && flag(tc, "header", false)
			if m := part("cellMargin"); flag(tc, "hasMargin", false) && m != nil {
				cellMargins[insets{true, doc.Insets{Left: intAttr(m, "left", 0), Right: intAttr(m, "right", 0), Top: intAttr(m, "top", 0), Bottom: intAttr(m, "bottom", 0)}}] = true
			} else {
				cellMargins[insets{}] = true
			}
		}
		if len(row.Cells) == 0 {
			continue
		}
		row.Height = height
		// Header rows (제목 줄) are the leading rows whose cells are all marked as header cells.
		headerRows = headerRows && allHeader
		row.Header = headerRows
		table.Rows = append(table.Rows, row)
	}

	// Cells with their own margins (hasMargin) set the table's padding when they all agree.
	if len(cellMargins) == 1 {
		for m := range cellMargins {
			if m.set {
				v := m.v
				table.CellMargin = &v
			}
		}
	}
	most := 1
	if len(widths) > 0 {
		most = 0
		for _, w := range widths {
			most = max(most, w.column+w.span)
		}
	}
	table.ColumnCount = max(table.ColumnCount, most)
	table.ColumnWidths = columnWidthsOf(widths, table.ColumnCount)
	if c := child(child(tbl, "caption"), "subList"); c != nil {
		table.Caption = r.readSubList(c)
	}
	return table
}

// columnWidthsOf derives column widths from the cell widths: single cells first, then what spanning cells leave over.
func columnWidthsOf(cells []cellWidth, columns int) []int {
	if columns <= 0 {
		return nil
	}
	widths := make([]*int, columns)
	for _, c := range cells {
		if c.span == 1 && c.column < columns && c.width > 0 && widths[c.column] == nil {
			widths[c.column] = ptr(c.width)
		}
	}
	for changed := true; changed; {
		changed = false
		for _, c := range cells {
			if c.span <= 1 || c.width <= 0 {
				continue
			}
			var unknown []int
			rest := c.width
			for i := c.column; i < c.column+min(c.span, columns-c.column); i++ {
				if widths[i] == nil {
					unknown = append(unknown, i)
				} else {
					rest -= *widths[i]
				}
			}
			if len(unknown) == 0 {
				continue
			}
			for _, i := range unknown {
				widths[i] = ptr(max(1, rest/len(unknown)))
			}
			changed = true
		}
	}
	total, known := 0, 0
	for _, w := range widths {
		if w != nil {
			total += *w
			known++
		}
	}
	if known == 0 {
		return nil
	}
	fallback := total / known
	out := make([]int, columns)
	for i, w := range widths {
		out[i] = val(w, fallback)
	}
	return out
}

func verticalAlignOf(value string) doc.VerticalAlign {
	switch value {
	case "CENTER":
		return doc.VAlignCenter
	case "BOTTOM":
		return doc.VAlignBottom
	}
	return doc.VAlignTop
}
