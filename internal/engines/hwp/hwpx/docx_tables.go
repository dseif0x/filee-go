package hwpx

// DOCX tables: grid column widths, merged cells (gridSpan / vMerge), borders from the table style, the table and each
// cell, cell shading, cell margins, vertical alignment, row heights and repeated header rows.

import (
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/hwp/xmltree"
)

func (r *docxReader) readTable(tbl *xmltree.Element) *doc.Table {
	tblPr := tbl.ChildNS(nsW, "tblPr")
	styleID := wval(tblPr.ChildNS(nsW, "tblStyle"))
	if styleID == "" {
		styleID = r.defaultTableStyle
	}
	chain := r.styleChain(r.styles[styleID])

	var grid []int
	allPositive := true
	for _, c := range tbl.ChildNS(nsW, "tblGrid").ChildrenNS(nsW, "gridCol") {
		w := doc.FromTwips(float64(val(twips(c, "w"), 0)))
		grid = append(grid, w)
		if w <= 0 {
			allPositive = false
		}
	}
	table := &doc.Table{ColumnCount: len(grid), Indent: doc.FromTwips(float64(val(twips(tblPr.ChildNS(nsW, "tblInd"), "w"), 0)))}
	if len(grid) > 0 && allPositive {
		table.ColumnWidths = grid
	}
	switch wval(tblPr.ChildNS(nsW, "jc")) {
	case "center":
		table.Align = doc.AlignCenter
	case "right", "end":
		table.Align = doc.AlignRight
	}

	// Borders and cell margins: table style chain, then the table's own properties.
	borders := map[string]doc.Border{}
	var margins *doc.Insets
	props := []*xmltree.Element{}
	for _, s := range chain {
		props = append(props, s.tblPr)
	}
	props = append(props, tblPr)
	for _, p := range props {
		applyBorders(p.ChildNS(nsW, "tblBorders"), borders)
		if cellMar := p.ChildNS(nsW, "tblCellMar"); cellMar != nil {
			m := cellMargins(cellMar, margins)
			margins = &m
		}
	}
	table.CellMargin = margins
	empty := doc.EmptyBorders
	table.Borders = &empty

	// Header row look from the table style ("firstRow" conditional formatting), when the table enables it.
	look := tblPr.ChildNS(nsW, "tblLook")
	firstRowLook := false
	if v, ok := wattrOK(look, "firstRow"); ok {
		firstRowLook = v == "1" || v == "true"
	} else if mask, err := strconv.ParseUint(wval(look), 16, 32); err == nil {
		firstRowLook = mask&0x20 != 0
	}
	var firstRowStyles []*xmltree.Element
	if firstRowLook {
		for _, s := range chain {
			for _, c := range s.conditions {
				if wattr(c, "type") == "firstRow" {
					firstRowStyles = append(firstRowStyles, c)
				}
			}
		}
	}
	var firstRowStyle *xmltree.Element
	if n := len(firstRowStyles); n > 0 {
		firstRowStyle = firstRowStyles[n-1]
	}

	var rows []*xmltree.Element
	for _, e := range tbl.Elements() {
		if e.Is(nsW, "tr") {
			rows = append(rows, e)
		} else {
			rows = append(rows, flattenRows(e, "tr")...)
		}
	}
	// vMerge: the cell that restarts a vertical merge, per grid column.
	mergeOrigin := map[int]*doc.Cell{}
	for ri, tr := range rows {
		trPr := tr.ChildNS(nsW, "trPr")
		row := &doc.Row{}
		if h := trPr.ChildNS(nsW, "tblHeader"); h != nil && wval(h) != "0" && wval(h) != "false" {
			row.Header = true
		}
		if h := twips(trPr.ChildNS(nsW, "trHeight"), "val"); h != nil && *h > 0 {
			row.Height = ptr(doc.FromTwips(float64(*h)))
		}
		column := val(twips(trPr.ChildNS(nsW, "gridBefore"), "val"), 0)
		var cells []*xmltree.Element
		for _, e := range tr.Elements() {
			if e.Is(nsW, "tc") {
				cells = append(cells, e)
			} else {
				cells = append(cells, flattenRows(e, "tc")...)
			}
		}
		for _, tc := range cells {
			tcPr := tc.ChildNS(nsW, "tcPr")
			span := max(1, val(twips(tcPr.ChildNS(nsW, "gridSpan"), "val"), 1))
			vMerge := tcPr.ChildNS(nsW, "vMerge")
			if origin, ok := mergeOrigin[column]; vMerge != nil && wval(vMerge) != "restart" && ok {
				origin.RowSpan++
				column += span
				continue
			}
			cell := &doc.Cell{Column: column, ColSpan: span, RowSpan: 1}
			cell.Blocks = r.readBlocks(tc.Elements(), nil)
			switch wval(tcPr.ChildNS(nsW, "vAlign")) {
			case "center":
				cell.VerticalAlign = doc.VAlignCenter
			case "bottom":
				cell.VerticalAlign = doc.VAlignBottom
			}
			// Cell edges: outer table borders on the outside, inside borders between cells, cell overrides.
			pick := func(outer bool, outerName, inner string) doc.Border {
				name := inner
				if outer {
					name = outerName
				}
				if b, ok := borders[name]; ok {
					return b
				}
				return doc.NoBorder
			}
			cellBorders := map[string]doc.Border{
				"left":   pick(column == 0, "left", "insideV"),
				"right":  pick(column+span >= len(grid), "right", "insideV"),
				"top":    pick(ri == 0, "top", "insideH"),
				"bottom": pick(ri == len(rows)-1, "bottom", "insideH"),
			}
			fill := ""
			layers := []*xmltree.Element{}
			for _, s := range chain {
				layers = append(layers, s.tcPr)
			}
			if ri == 0 {
				for _, s := range firstRowStyles {
					layers = append(layers, s.ChildNS(nsW, "tcPr"))
				}
			}
			layers = append(layers, tcPr)
			for _, p := range layers {
				// Sides a cell leaves out keep the padding its neighbours get: the table's, else the HWPX default.
				if cellMar := p.ChildNS(nsW, "tcMar"); cellMar != nil {
					current := defaultCellMargin
					if cell.CellMargin != nil {
						current = *cell.CellMargin
					} else if table.CellMargin != nil {
						current = *table.CellMargin
					}
					m := cellMargins(cellMar, &current)
					cell.CellMargin = &m
				}
				applyBorders(p.ChildNS(nsW, "tcBorders"), cellBorders)
				if shd := p.ChildNS(nsW, "shd"); shd != nil {
					if c := shadingColor(shd); c != "" {
						fill = c
					}
				}
			}
			cell.Borders = &doc.Borders{Left: cellBorders["left"], Right: cellBorders["right"], Top: cellBorders["top"], Bottom: cellBorders["bottom"]}
			cell.Fill = fill
			if ri == 0 && firstRowStyle.ChildNS(nsW, "rPr").ChildNS(nsW, "b") != nil {
				embolden(cell.Blocks)
			}
			if vMerge != nil {
				mergeOrigin[column] = cell
			} else {
				delete(mergeOrigin, column)
			}
			row.Cells = append(row.Cells, cell)
			column += span
		}
		table.Rows = append(table.Rows, row)
		table.ColumnCount = max(table.ColumnCount, column)
	}
	return table
}

func (r *docxReader) styleChain(style *docxStyle) []*docxStyle {
	var chain []*docxStyle
	for current := style; current != nil && len(chain) < 20; {
		chain = append([]*docxStyle{current}, chain...)
		if current.basedOn == "" {
			break
		}
		current = r.styles[current.basedOn]
	}
	return chain
}

// flattenRows looks through w:sdt and w:customXml for rows (or cells).
func flattenRows(e *xmltree.Element, name string) []*xmltree.Element {
	var inner []*xmltree.Element
	switch {
	case e.Is(nsW, "sdt"):
		inner = e.ChildNS(nsW, "sdtContent").Elements()
	case e.Is(nsW, "customXml"):
		inner = e.Elements()
	default:
		return nil
	}
	var out []*xmltree.Element
	for _, c := range inner {
		if c.Is(nsW, name) {
			out = append(out, c)
		} else {
			out = append(out, flattenRows(c, name)...)
		}
	}
	return out
}

func applyBorders(borders *xmltree.Element, target map[string]doc.Border) {
	for _, edge := range borders.Elements() {
		name := edge.Local
		switch name {
		case "start":
			name = "left"
		case "end":
			name = "right"
		}
		target[name] = wordBorder(edge)
	}
}

// wordBorder is a Word border (w:val style, w:sz in eighths of a point, w:color).
func wordBorder(edge *xmltree.Element) doc.Border {
	style := doc.BorderSolid
	v, ok := wattrOK(edge, "val")
	switch {
	case !ok || v == "nil" || v == "none":
		style = doc.BorderNone
	case v == "dotted":
		style = doc.BorderDot
	case v == "dashed" || v == "dashSmallGap":
		style = doc.BorderDash
	case v == "dotDash" || v == "dotDotDash":
		style = doc.BorderDashDot
	case v == "double" || v == "triple" || v == "thinThickSmallGap" || v == "thickThinSmallGap":
		style = doc.BorderDouble
	}
	eighths := val(twips(edge, "sz"), 4)
	color := "#000000"
	if c := wattr(edge, "color"); len(c) == 6 {
		color = "#" + strings.ToUpper(c)
	}
	return doc.Border{Style: style, WidthMm: float64(eighths) / 8.0 * 25.4 / 72, Color: color}
}

func cellMargins(cellMar *xmltree.Element, current *doc.Insets) doc.Insets {
	existing := doc.Insets{Left: 540, Right: 540}
	if current != nil {
		existing = *current
	}
	get := func(name, alternative string, fallback int) int {
		side := cellMar.ChildNS(nsW, name)
		if side == nil {
			side = cellMar.ChildNS(nsW, alternative)
		}
		if wattr(side, "type") == "nil" {
			return 0
		}
		if t := twips(side, "w"); t != nil {
			return doc.FromTwips(float64(*t))
		}
		return fallback
	}
	return doc.Insets{Left: get("left", "start", existing.Left), Right: get("right", "end", existing.Right), Top: get("top", "top", existing.Top), Bottom: get("bottom", "bottom", existing.Bottom)}
}

func embolden(blocks []doc.Block) {
	for _, b := range blocks {
		p, ok := b.(*doc.Paragraph)
		if !ok {
			continue
		}
		for i, in := range p.Inlines {
			if t, ok := in.(*doc.Text); ok {
				f := t.Format
				f.Bold = ptr(true)
				p.Inlines[i] = &doc.Text{Text: t.Text, Format: f}
			}
		}
	}
}
