package unhwp

// Controls of a paragraph: the record of an extended control character (CTRL_HEADER) and its children.

import (
	"encoding/binary"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
)

func (h *hwpFile) control(c *record, b *builder, f doc.CharFormat, state *sectionState) {
	if len(c.data) < 4 {
		return
	}
	id := binary.LittleEndian.Uint32(c.data)
	r := &reader{b: c.data, pos: 4}
	switch id {
	case ctrlSection:
		if state == nil {
			return
		}
		r.u32()               // attr
		r.skip(2 + 2 + 2 + 4) // column gap, grids, default tab
		state.outlineID = r.u16()
		if start := r.u16(); start > 0 {
			state.section.StartPageNumber = ptr(start)
		}
		for _, child := range c.children {
			if child.tag == tagPageDef {
				state.section.Page = pageDef(child.data)
			}
		}
	case ctrlColumn:
		attr := r.u16()
		gap := r.i16()
		count := max(1, attr>>2&0xFF)
		columns := doc.SingleColumn
		if count > 1 {
			if attr&(1<<12) == 0 {
				r.skip(2 * count)
			}
			r.u16()
			separator := r.u8()
			columns = doc.Columns{Count: count, Gap: gap, Separator: separator != 0}
		}
		if state == nil {
			return
		}
		if state.columnsSet {
			cc := columns
			b.current.ColumnsChange = &cc
		} else {
			state.section.Columns = columns
			state.columnsSet = true
		}
	case ctrlTable:
		if t := h.table(c); t != nil {
			b.table(t)
		}
	case ctrlShape:
		for _, in := range h.shape(c, anchorOf(c.data)) {
			b.add(in)
		}
	case ctrlEquation:
		for _, child := range c.children {
			if child.tag == tagEqEdit {
				er := &reader{b: child.data}
				er.u32()
				if script := strings.TrimSpace(er.str()); script != "" {
					b.addText(script, f)
				}
			}
		}
	case ctrlHeader, ctrlFooter:
		if state == nil {
			return
		}
		pages := doc.BothPages
		switch r.u32() & 3 {
		case 1:
			pages = doc.EvenPages
		case 2:
			pages = doc.OddPages
		}
		target := &state.section.Footers
		if id == ctrlHeader {
			target = &state.section.Headers
		}
		for _, existing := range *target {
			if existing.Pages == pages {
				return // a later one replaces it from its page on; the model keeps the first
			}
		}
		*target = append(*target, &doc.HeaderFooter{Pages: pages, Blocks: h.list(c.children)})
	case ctrlFootnote, ctrlEndnote:
		b.add(&doc.Note{Endnote: id == ctrlEndnote, Blocks: h.list(c.children)})
	case ctrlAutoNumber:
		attr := r.u32()
		number := r.u16()
		switch attr & 0xF {
		case 0:
			b.add(&doc.Field{Kind: doc.PageNumberField, Format: f})
		case 1, 2: // the note's own number, written again by every target format
		default:
			b.addText(itoa(number), f)
		}
	case ctrlNewNumber:
		attr := r.u32()
		if number := r.u16(); state != nil && attr&0xF == 0 && state.section.StartPageNumber == nil {
			state.section.StartPageNumber = ptr(max(1, number))
		}
	case ctrlPageNumber:
		if state == nil {
			return
		}
		attr := r.u32()
		r.skip(6) // user symbol, prefix, suffix
		side := r.u16()
		pn := &pageNumber{position: int(attr >> 8 & 0xF)}
		if side != 0 {
			pn.side = string(rune(side))
		}
		state.pageNumber = pn
	case ctrlBookmark:
		if name := bookmarkName(c); name != "" {
			b.add(&doc.Bookmark{Name: name})
		}
	case ctrlDutmal:
		// Ruby text (덧말): the main text.
		b.addText(r.str(), f)
	case ctrlCompose:
		// Overlapping characters (글자 겹치기): the characters one after the other.
		b.addText(r.str(), f)
	default:
		if id>>24 == '%' { // a field: hyperlinks open a link until the field end
			target := ""
			if id == ctrlHyperlink {
				r.u32() // attr
				r.u8()  // extra attr
				target = hyperlinkTarget(r.str())
			}
			// Other fields (click-here, date, ...) keep their result text, which follows until the field end.
			b.fields = append(b.fields, target != "")
			if target != "" {
				b.openLink(target)
			}
		}
	}
}

// hyperlinkTarget reads the command of a hyperlink field: the target with ':', '?', ';' and '#' escaped by a
// backslash, then ";kind;..."; an unescaped '?' starts a bookmark name.
func hyperlinkTarget(command string) string {
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
		if c == '?' && !strings.Contains(target.String(), "://") {
			c = '#'
		}
		target.WriteRune(c)
	}
	if t := target.String(); t != "#" {
		return t
	}
	return ""
}

// bookmarkName is the first string of the bookmark's parameter set (CTRL_DATA).
func bookmarkName(c *record) string {
	for _, child := range c.children {
		if child.tag != tagCtrlData {
			continue
		}
		r := &reader{b: child.data}
		r.u16() // set id
		count := r.i16()
		r.u16()
		for i := 0; i < count && !r.bad; i++ {
			r.u16() // item id
			if kind := r.u16(); kind == 1 {
				return r.str()
			}
			return ""
		}
	}
	return ""
}

func pageDef(b []byte) *doc.Page {
	r := &reader{b: b}
	width, height := int(r.u32()), int(r.u32())
	left, right, top, bottom := int(r.u32()), int(r.u32()), int(r.u32()), int(r.u32())
	header, footer, gutter := int(r.u32()), int(r.u32()), int(r.u32())
	attr := r.u32()
	if r.bad || width <= 0 || height <= 0 {
		return nil
	}
	// Width and height describe the portrait sheet; bit 0 turns it (landscape).
	if attr&1 != 0 {
		width, height = max(width, height), min(width, height)
	}
	return &doc.Page{Width: width, Height: height, Left: left, Right: right, Top: top, Bottom: bottom, Header: header, Footer: footer, Gutter: gutter}
}

// list is the paragraphs of a list (LIST_HEADER followed by its paragraphs) among the children of a control.
func (h *hwpFile) list(children []*record) []doc.Block {
	var blocks []doc.Block
	for _, c := range children {
		if c.tag == tagParaHeader {
			blocks = h.paragraph(c, blocks, nil)
		}
	}
	return blocks
}

// ───────────────────────── Objects ─────────────────────────

// anchorOf is the position of an object from its common properties; nil when it is laid out like a character.
func anchorOf(data []byte) *doc.Anchor {
	r := &reader{b: data, pos: 4}
	attr := r.u32()
	vertical, horizontal := r.i32(), r.i32()
	if r.bad || attr&1 != 0 {
		return nil
	}
	a := &doc.Anchor{VerticalOffset: vertical, HorizontalOffset: horizontal, AllowOverlap: attr&(1<<14) != 0}
	a.VerticalRelativeTo = [...]string{"PAPER", "PAGE", "PARA", "PARA"}[attr>>3&3]
	a.VerticalAlign = [...]string{"TOP", "CENTER", "BOTTOM", "INSIDE", "OUTSIDE", "TOP", "TOP", "TOP"}[attr>>5&7]
	a.HorizontalRelativeTo = [...]string{"PAPER", "PAGE", "COLUMN", "PARA"}[attr>>8&3]
	a.HorizontalAlign = [...]string{"LEFT", "CENTER", "RIGHT", "INSIDE", "OUTSIDE", "LEFT", "LEFT", "LEFT"}[attr>>10&7]
	a.Wrap = [...]string{"SQUARE", "SQUARE", "SQUARE", "TOP_AND_BOTTOM", "BEHIND_TEXT", "IN_FRONT_OF_TEXT", "TOP_AND_BOTTOM", "TOP_AND_BOTTOM"}[attr>>21&7]
	return a
}

func objectSize(data []byte) (int, int) {
	r := &reader{b: data, pos: 16}
	return int(r.u32()), int(r.u32())
}

// shape reads a drawing object ('gso '): pictures, text boxes, rectangles, ellipses, lines and groups.
func (h *hwpFile) shape(c *record, anchor *doc.Anchor) []doc.Inline {
	width, height := objectSize(c.data)
	for _, child := range c.children {
		if child.tag == tagShapeComponent {
			return h.component(child, width, height, anchor)
		}
	}
	return nil
}

func (h *hwpFile) component(sc *record, width, height int, anchor *doc.Anchor) []doc.Inline {
	var out []doc.Inline
	var kind int
	var listChildren []*record
	hasList := false
	for _, child := range sc.children {
		switch child.tag {
		case tagListHeader:
			hasList = true
		case tagParaHeader:
			listChildren = append(listChildren, child)
		case tagShapePicture:
			r := &reader{b: child.data, pos: 71}
			if p := h.picture(r.u16()); p != "" && !r.bad {
				img := &doc.Image{Path: p, Anchor: anchor}
				if width > 0 {
					img.Width = ptr(width)
				}
				if height > 0 {
					img.Height = ptr(height)
				}
				out = append(out, img)
			}
		case tagShapeRectangle:
			kind = tagShapeRectangle
		case tagShapeEllipse:
			kind = tagShapeEllipse
		case tagShapeLine:
			kind = tagShapeLine
		case tagShapeComponent:
			// A member of a group: its size and offset inside the group.
			r := &reader{b: child.data, pos: 4}
			if len(child.data) >= 8 && binary.LittleEndian.Uint32(child.data) == binary.LittleEndian.Uint32(child.data[4:]) {
				r.pos = 8
			}
			x, y := r.i32(), r.i32()
			r.skip(4) // group level, version
			r.skip(8) // original size
			w, hgt := int(r.u32()), int(r.u32())
			a := anchor
			if a == nil {
				a = &doc.Anchor{HorizontalRelativeTo: "COLUMN", HorizontalAlign: "LEFT", VerticalRelativeTo: "PARA", VerticalAlign: "TOP", Wrap: "IN_FRONT_OF_TEXT", AllowOverlap: true}
			}
			member := *a
			member.HorizontalOffset += x
			member.VerticalOffset += y
			member.AllowOverlap = true
			out = append(out, h.component(child, w, hgt, &member)...)
		}
	}
	if hasList {
		box := doc.NewTextBox()
		box.Width, box.Height, box.Anchor = max(width, 1), max(height, 1), anchor
		box.Blocks = h.list(listChildren)
		if kind == tagShapeEllipse {
			box.Shape = doc.ShapeEllipse
		}
		return append(out, box)
	}
	switch kind {
	case tagShapeRectangle:
		out = append(out, &doc.Shape{Kind: doc.ShapeRectangle, Width: width, Height: height, Anchor: anchor, Line: doc.ThinBorder})
	case tagShapeEllipse:
		out = append(out, &doc.Shape{Kind: doc.ShapeEllipse, Width: width, Height: height, Anchor: anchor, Line: doc.ThinBorder})
	case tagShapeLine:
		out = append(out, &doc.Shape{Kind: doc.ShapeLine, Width: width, Height: height, Anchor: anchor, Line: doc.ThinBorder})
	}
	return out
}

// table reads a table control: the TABLE record, then one LIST_HEADER per cell followed by its paragraphs. A list
// before the TABLE record is the caption.
func (h *hwpFile) table(c *record) *doc.Table {
	t := &doc.Table{}
	var cell *doc.Cell
	var caption []*record
	seenTable := false
	inCaption := false
	finishCell := func(paragraphs []*record) {
		if cell != nil {
			for _, p := range paragraphs {
				cell.Blocks = h.paragraph(p, cell.Blocks, nil)
			}
		}
	}
	var pending []*record
	rowsByAddress := map[int]*doc.Row{}
	maxRow := -1
	for _, child := range c.children {
		switch child.tag {
		case tagTable:
			seenTable = true
			r := &reader{b: child.data}
			r.u32()
			rows, cols := r.u16(), r.u16()
			r.skip(2)
			l, rt, tp, bt := r.u16(), r.u16(), r.u16(), r.u16()
			t.CellMargin = &doc.Insets{Left: l, Right: rt, Top: tp, Bottom: bt}
			r.skip(2 * rows)
			if bf := h.border(r.u16()); bf != nil {
				borders := bf.borders
				t.Borders = &borders
			}
			t.ColumnCount = cols
		case tagListHeader:
			if !seenTable {
				inCaption = true
				continue
			}
			inCaption = false
			finishCell(pending)
			pending = nil
			r := &reader{b: child.data}
			r.u16()
			r.u16()
			flags := r.u32()
			col, row, colSpan, rowSpan := r.u16(), r.u16(), r.u16(), r.u16()
			width, height := int(r.u32()), int(r.u32())
			l, rt, tp, bt := r.u16(), r.u16(), r.u16(), r.u16()
			borderID := r.u16()
			_ = width
			cell = &doc.Cell{Column: col, ColSpan: max(1, colSpan), RowSpan: max(1, rowSpan)}
			switch flags >> 5 & 3 {
			case 1:
				cell.VerticalAlign = doc.VAlignCenter
			case 2:
				cell.VerticalAlign = doc.VAlignBottom
			}
			if bf := h.border(borderID); bf != nil {
				borders := bf.borders
				cell.Borders, cell.Fill = &borders, bf.fill
			}
			if t.CellMargin == nil || *t.CellMargin != (doc.Insets{Left: l, Right: rt, Top: tp, Bottom: bt}) {
				cell.CellMargin = &doc.Insets{Left: l, Right: rt, Top: tp, Bottom: bt}
			}
			rr := rowsByAddress[row]
			if rr == nil {
				rr = &doc.Row{}
				rowsByAddress[row] = rr
				maxRow = max(maxRow, row)
			}
			if rowSpan == 1 && height > 0 && (rr.Height == nil || height < *rr.Height) {
				rr.Height = ptr(height)
			}
			rr.Cells = append(rr.Cells, cell)
		case tagParaHeader:
			if inCaption {
				caption = append(caption, child)
			} else {
				pending = append(pending, child)
			}
		}
	}
	finishCell(pending)
	for i := 0; i <= maxRow; i++ {
		if row := rowsByAddress[i]; row != nil {
			t.Rows = append(t.Rows, row)
		}
	}
	for _, p := range caption {
		t.Caption = h.paragraph(p, t.Caption, nil)
	}
	if len(t.Rows) == 0 {
		return nil
	}
	return t
}

func (h *hwpFile) border(id int) *borderFill {
	if id >= 1 && id <= len(h.info.borders) {
		return &h.info.borders[id-1]
	}
	return nil
}
