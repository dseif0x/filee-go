package unhwp

// Body text: sections, paragraphs with their text and character shapes, and the controls of a paragraph (section and
// column definitions, tables, drawing objects, headers and footers, notes, fields, numbers, bookmarks).

import (
	"encoding/binary"
	"strings"
	"unicode/utf16"

	"github.com/dseif0x/filee-go/internal/doc"
)

func ptr[T any](v T) *T { return &v }

// sectionState is the section being read: a 'secd' control starts it; the first 'cold' sets its columns.
type sectionState struct {
	section    *doc.Section
	columnsSet bool
	pageNumber *pageNumber
	outlineID  int
}

type pageNumber struct {
	position int
	side     string
}

func (h *hwpFile) readSection(records []*record, d *doc.Document) {
	state := &sectionState{section: doc.NewSection()}
	d.Sections = append(d.Sections, state.section)
	for _, r := range records {
		if r.tag == tagParaHeader {
			state.section.Blocks = h.paragraph(r, state.section.Blocks, state)
		}
	}
	h.finishSection(state)
}

// finishSection turns an automatic page number ('pgnp', "쪽 번호 매기기") into a header or footer paragraph.
func (h *hwpFile) finishSection(state *sectionState) {
	pn := state.pageNumber
	if pn == nil || pn.position == 0 {
		return
	}
	align := doc.AlignCenter
	switch pn.position {
	case 1, 4, 9, 10:
		align = doc.AlignLeft
	case 3, 6, 7, 8:
		align = doc.AlignRight
	}
	p := &doc.Paragraph{Format: doc.ParaFormat{Align: ptr(align)}}
	if pn.side != "" {
		p.Inlines = append(p.Inlines, &doc.Text{Text: pn.side + " "})
	}
	p.Inlines = append(p.Inlines, &doc.Field{Kind: doc.PageNumberField})
	if pn.side != "" {
		p.Inlines = append(p.Inlines, &doc.Text{Text: " " + pn.side})
	}
	target := &state.section.Footers
	if pn.position >= 1 && pn.position <= 3 || pn.position == 7 || pn.position == 9 {
		target = &state.section.Headers
	}
	if len(*target) == 0 {
		*target = append(*target, &doc.HeaderFooter{Pages: doc.BothPages})
	}
	for _, item := range *target {
		item.Blocks = append(item.Blocks, p)
	}
}

// ───────────────────────── Paragraphs ─────────────────────────

// builder builds the blocks of one paragraph: a table ends the paragraph so far and the rest continues in a new one.
type builder struct {
	output     []doc.Block
	format     doc.ParaFormat
	heading    int
	list       *doc.ListRef
	current    *doc.Paragraph
	links      []*doc.Link
	afterTable bool
	mark       doc.CharFormat
	// fields are the fields open in the paragraph; true for a hyperlink.
	fields []bool
}

func (b *builder) add(in doc.Inline) {
	if n := len(b.links); n > 0 {
		b.links[n-1].Content = append(b.links[n-1].Content, in)
	} else {
		b.current.Inlines = append(b.current.Inlines, in)
	}
}

func (b *builder) addText(text string, f doc.CharFormat) {
	if text == "" {
		return
	}
	sink := &b.current.Inlines
	if n := len(b.links); n > 0 {
		sink = &b.links[n-1].Content
	}
	if n := len(*sink); n > 0 {
		if last, ok := (*sink)[n-1].(*doc.Text); ok && last.Format.Equal(f) {
			(*sink)[n-1] = &doc.Text{Text: last.Text + text, Format: f}
			return
		}
	}
	*sink = append(*sink, &doc.Text{Text: text, Format: f})
}

func (b *builder) newParagraph(list *doc.ListRef) {
	b.current = &doc.Paragraph{Format: b.format, HeadingLevel: b.heading, List: list}
	links := b.links
	b.links = nil
	for _, l := range links {
		b.openLink(l.Target)
	}
}

func (b *builder) openLink(target string) {
	l := &doc.Link{Target: target}
	b.add(l)
	b.links = append(b.links, l)
}

func (b *builder) closeLink() {
	if n := len(b.links); n > 0 {
		b.links = b.links[:n-1]
	}
}

func (b *builder) hasContent() bool {
	for _, in := range b.current.Inlines {
		if l, ok := in.(*doc.Link); ok && len(l.Content) == 0 {
			continue
		}
		return true
	}
	return false
}

func (b *builder) table(t *doc.Table) {
	if b.hasContent() || b.current.PageBreakBefore || b.current.ColumnBreakBefore || b.current.ColumnsChange != nil {
		b.emit()
	}
	b.output = append(b.output, t)
	var list *doc.ListRef
	if b.list != nil {
		l := *b.list
		l.Numbered = false
		list = &l
	}
	b.newParagraph(list)
	b.afterTable = true
}

func (b *builder) emit() {
	b.current.MarkFormat = b.mark
	b.output = append(b.output, b.current)
}

func (b *builder) finish() []doc.Block {
	if !b.afterTable || b.hasContent() {
		b.emit()
	}
	return b.output
}

// paragraph reads one PARA_HEADER with its text, character shapes and controls.
func (h *hwpFile) paragraph(p *record, output []doc.Block, state *sectionState) []doc.Block {
	r := &reader{b: p.data}
	r.u32() // characters
	r.u32() // control mask
	paraShapeID := r.u16()
	styleID := r.u8()
	breaks := r.u8()

	var text []uint16
	var shapes [][2]int
	var controls []*record
	for _, c := range p.children {
		switch c.tag {
		case tagParaText:
			text = make([]uint16, len(c.data)/2)
			for i := range text {
				text[i] = binary.LittleEndian.Uint16(c.data[2*i:])
			}
		case tagParaCharShape:
			for i := 0; i+8 <= len(c.data); i += 8 {
				shapes = append(shapes, [2]int{int(binary.LittleEndian.Uint32(c.data[i:])), int(binary.LittleEndian.Uint32(c.data[i+4:]))})
			}
		case tagCtrlHeader:
			controls = append(controls, c)
		}
	}

	format, heading, list := h.paraFormat(paraShapeID, styleID, state)
	b := &builder{output: output, format: format, heading: heading, list: list, mark: h.charFormat(firstShape(shapes))}
	b.newParagraph(list)
	b.current.PageBreakBefore = breaks&0x04 != 0 || h.pageBreakBefore(paraShapeID)
	b.current.ColumnBreakBefore = breaks&0x08 != 0

	// Controls without a character in the text (some producers) are read after the text.
	next := 0
	shapeAt := func(i int) doc.CharFormat {
		id := 0
		for _, s := range shapes {
			if s[0] <= i {
				id = s[1]
			}
		}
		return h.charFormat(id)
	}
	var run []uint16
	runFormat := doc.CharFormat{}
	flushRun := func() {
		if len(run) > 0 {
			b.addText(string(utf16.Decode(run)), runFormat)
			run = run[:0]
		}
	}
	for i := 0; i < len(text); {
		c := text[i]
		f := shapeAt(i)
		if c >= 32 {
			if len(run) > 0 && !runFormat.Equal(f) {
				flushRun()
			}
			runFormat = f
			run = append(run, c)
			i++
			continue
		}
		flushRun()
		switch c {
		case 10:
			b.add(&doc.LineBreak{Format: f})
			i++
		case 13:
			i++
		case 24:
			b.addText("­", f)
			i++
		case 30:
			b.addText(" ", f)
			i++
		case 31:
			b.addText(" ", f)
			i++
		case 0, 25, 26, 27, 28, 29:
			i++
		case 9:
			b.add(&doc.Tab{Format: f})
			i += 8
		case 4: // field end
			if n := len(b.fields); n > 0 {
				if b.fields[n-1] {
					b.closeLink()
				}
				b.fields = b.fields[:n-1]
			}
			i += 8
		case 5, 6, 7, 8, 19, 20:
			i += 8
		default: // extended controls: the next control of the paragraph
			if next < len(controls) {
				h.control(controls[next], b, f, state)
				next++
			}
			i += 8
		}
	}
	flushRun()
	for ; next < len(controls); next++ {
		h.control(controls[next], b, h.charFormat(0), state)
	}
	return b.finish()
}

func firstShape(shapes [][2]int) int {
	if len(shapes) > 0 {
		return shapes[0][1]
	}
	return 0
}

// ───────────────────────── Formats ─────────────────────────

func (h *hwpFile) charFormat(id int) doc.CharFormat {
	if id < 0 || id >= len(h.info.charShapes) {
		return doc.CharFormat{}
	}
	c := h.info.charShapes[id]
	f := doc.CharFormat{
		Bold:        ptr(c.attr&2 != 0),
		Italic:      ptr(c.attr&1 != 0),
		Underline:   ptr(c.attr>>2&3 == 1),
		Strike:      ptr(c.attr>>18&7 != 0),
		Superscript: ptr(c.attr&(1<<15) != 0),
		Subscript:   ptr(c.attr&(1<<16) != 0),
		Size:        ptr(c.size),
		Color:       ptr(orDefault(c.textColor, "#000000")),
		Spacing:     ptr(c.spacing),
	}
	if face := h.face(1, c.faces[1]); face != "" {
		f.Font = ptr(face)
	}
	if face := h.face(0, c.faces[0]); face != "" {
		f.EastAsianFont = ptr(face)
	}
	if c.shadeColor != "" && c.shadeColor != "#FFFFFF" {
		f.Shade = ptr(c.shadeColor)
	}
	return f
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func (h *hwpFile) face(language, id int) string {
	if id >= 0 && id < len(h.info.faces[language]) {
		return h.info.faces[language][id]
	}
	return ""
}

func (h *hwpFile) pageBreakBefore(id int) bool {
	return id >= 0 && id < len(h.info.paraShapes) && h.info.paraShapes[id].attr1&(1<<19) != 0
}

// paraFormat is the format, heading level and list of a paragraph shape. Lengths are stored doubled, like the
// hp:default branch of HWPX.
func (h *hwpFile) paraFormat(id, styleID int, state *sectionState) (doc.ParaFormat, int, *doc.ListRef) {
	var f doc.ParaFormat
	heading := h.styleHeading(styleID)
	if id < 0 || id >= len(h.info.paraShapes) {
		return f, heading, nil
	}
	p := h.info.paraShapes[id]
	align := doc.AlignJustify
	switch p.attr1 >> 2 & 7 {
	case 1:
		align = doc.AlignLeft
	case 2:
		align = doc.AlignRight
	case 3:
		align = doc.AlignCenter
	case 4, 5:
		align = doc.AlignDistribute
	}
	half := func(v int) int { return int(float64(v)/2 + 0.5*sign(v)) }
	intent, left := half(p.indent), half(p.left)
	modelLeft := left
	if intent < 0 {
		modelLeft = left - intent
	}
	kind, value := int(p.attr1&3), p.lineSpacingOld
	if p.hasNewSpacing {
		kind, value = int(p.attr3&0x1F), p.lineSpacing
	}
	var spacing doc.LineSpacing
	switch kind {
	case 1:
		spacing = doc.LineSpacing{Kind: doc.SpacingFixed, Value: half(value)}
	case 2:
		spacing = doc.LineSpacing{Kind: doc.SpacingAtLeast, Value: 1000 + half(value)}
	case 3:
		spacing = doc.LineSpacing{Kind: doc.SpacingAtLeast, Value: half(value)}
	default:
		spacing = doc.LineSpacing{Kind: doc.SpacingPercent, Value: value}
	}
	f = doc.ParaFormat{
		Align: ptr(align), Left: ptr(modelLeft), Right: ptr(half(p.right)), FirstLine: ptr(intent),
		Before: ptr(half(p.prev)), After: ptr(half(p.next)), LineSpacing: &spacing,
		KeepWithNext: ptr(p.attr1&(1<<17) != 0), KeepLines: ptr(p.attr1&(1<<18) != 0), WidowOrphan: ptr(p.attr1&(1<<16) != 0),
	}
	if p.tabDef >= 0 && p.tabDef < len(h.info.tabDefs) && len(h.info.tabDefs[p.tabDef]) > 0 {
		f.Tabs = h.info.tabDefs[p.tabDef]
	}
	level := int(p.attr1 >> 25 & 7)
	var list *doc.ListRef
	switch p.attr1 >> 23 & 3 {
	case 1: // outline (개요)
		heading = min(level+1, 9)
		numberingID := state.outlineID
		if n := h.numbering(numberingID); n != nil && level < len(n.Levels) && hasCounter(n.Levels[level].Text) {
			list = &doc.ListRef{Numbering: n, Level: level, Numbered: true}
		}
	case 2:
		if n := h.numbering(p.numbering); n != nil && len(n.Levels) > 0 {
			list = &doc.ListRef{Numbering: n, Level: min(level, len(n.Levels)-1), Numbered: true}
		}
	case 3:
		if n := h.bullet(p.numbering); n != nil {
			list = &doc.ListRef{Numbering: n, Level: min(level, len(n.Levels)-1), Numbered: true}
		}
	}
	return f, heading, list
}

func sign(v int) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

func (h *hwpFile) styleHeading(id int) int {
	if id < 0 || id >= len(h.info.styles) {
		return 0
	}
	name := h.info.styles[id].engName
	if len(name) > 8 && strings.EqualFold(name[:8], "Heading ") {
		if level := atoiSafe(name[8:]); level >= 1 && level <= 9 {
			return level
		}
	}
	return 0
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

var numberingCache = map[*hwpFile]map[int]*doc.Numbering{}

// numbering is the list definition of a 1-based numbering id (shared by all its paragraphs).
func (h *hwpFile) numbering(id int) *doc.Numbering {
	if id < 1 || id > len(h.info.numberings) {
		return nil
	}
	return h.cachedList(id, func() *doc.Numbering {
		return &doc.Numbering{Levels: append([]doc.NumberingLevel(nil), h.info.numberings[id-1].levels...)}
	})
}

// bullet is the list definition of a 1-based bullet id: the same symbol on every level.
func (h *hwpFile) bullet(id int) *doc.Numbering {
	if id < 1 || id > len(h.info.bullets) {
		return nil
	}
	return h.cachedList(-id, func() *doc.Numbering {
		n := &doc.Numbering{}
		for i := 0; i < 10; i++ {
			n.Levels = append(n.Levels, doc.NumberingLevel{Format: "DIGIT", Text: h.info.bullets[id-1], Start: 1, Bullet: true})
		}
		return n
	})
}

func (h *hwpFile) cachedList(key int, make func() *doc.Numbering) *doc.Numbering {
	m := numberingCache[h]
	if m == nil {
		m = map[int]*doc.Numbering{}
		numberingCache[h] = m
	}
	if n, ok := m[key]; ok {
		return n
	}
	n := make()
	m[key] = n
	return n
}
