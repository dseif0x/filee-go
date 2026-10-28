package spreadsheet

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dseif0x/filee-go/internal/engines/office/opc"
	"github.com/dseif0x/filee-go/internal/xdom"
)

// XLSX (SpreadsheetML) → Workbook, read directly: cell values formatted the
// way Excel shows them, fonts, fills, borders, alignment, merged cells,
// column widths, custom row heights, hidden rows / columns / sheets, frozen
// header rows, print titles and row breaks. Charts, pictures and conditional
// formatting are not read.

const (
	sNs = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	rNs = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	// Strict OOXML uses other namespaces for the same elements.
	sStrictNs = "http://purl.oclc.org/ooxml/spreadsheetml/main"
	rStrictNs = "http://purl.oclc.org/ooxml/officeDocument/relationships"
	// date1904Offset: 1 January 1904 is serial 1462 in the 1900 system.
	date1904Offset = 1462
)

type xlsxReader struct {
	pkg         *opc.Package
	ns, rns     string
	shared      []string
	cellFormats []xf
	formatter   *NumberFormatter
	date1904    bool
}

type xf struct {
	style  CellStyle
	format string
}

// ReadXLSX reads every visible sheet of an XLSX / XLSM / XLTX file.
func ReadXLSX(path string) (*Workbook, error) {
	pkg, err := opc.Open(path)
	if err != nil {
		return nil, errors.New("the file is not an Excel workbook: " + err.Error())
	}
	defer pkg.Close()
	r := &xlsxReader{pkg: pkg, ns: sNs, rns: rNs}
	return r.read()
}

func (r *xlsxReader) read() (*Workbook, error) {
	main := r.pkg.MainPartPath()
	if main == "" {
		main = "xl/workbook.xml"
	}
	wb := r.pkg.Part(main)
	if wb == nil {
		return nil, errors.New("the file is not an Excel workbook (no xl/workbook.xml)")
	}
	if wb.Name.Space == sStrictNs {
		r.ns, r.rns = sStrictNs, rStrictNs
	}
	pr := wb.Child(r.ns, "workbookPr").A("date1904")
	r.date1904 = pr == "1" || pr == "true"
	r.formatter = NewFormatter(r.date1904)

	var theme *xdom.Node
	if t := r.pkg.TargetOfType(main, "/theme"); t != "" {
		theme = r.pkg.Part(t)
	}
	colors := opc.FromTheme(theme)
	if t := r.pkg.TargetOfType(main, "/sharedStrings"); t != "" {
		for _, si := range r.pkg.Part(t).All(r.ns, "si") {
			r.shared = append(r.shared, r.richText(si))
		}
	}
	if t := r.pkg.TargetOfType(main, "/styles"); t != "" {
		r.readStyles(r.pkg.Part(t), colors)
	}

	// Print titles are a defined name per sheet, by the sheet's position (hidden sheets count).
	titles := map[int][2]int{}
	for _, name := range wb.Child(r.ns, "definedNames").All(r.ns, "definedName") {
		if name.A("name") != "_xlnm.Print_Titles" {
			continue
		}
		id, err := strconv.Atoi(name.A("localSheetId"))
		if err != nil {
			continue
		}
		if f, l, ok := PrintTitleRows(name.Value()); ok {
			titles[id] = [2]int{f, l}
		}
	}

	book := &Workbook{}
	for index, sheet := range wb.Child(r.ns, "sheets").All(r.ns, "sheet") {
		if s := sheet.A("state"); s == "hidden" || s == "veryHidden" {
			continue
		}
		target := r.pkg.Target(main, sheet.AN(r.rns, "id"))
		if target == "" {
			continue
		}
		xml := r.pkg.Part(target)
		if xml == nil || !xml.Is(r.ns, "worksheet") {
			continue // chart sheets and dialog sheets have no cells
		}
		name := sheet.A("name")
		if name == "" {
			name = "Sheet"
		}
		read := r.readSheet(name, xml)
		if t, ok := titles[index]; ok {
			read.TitleRows = &t
		}
		book.Sheets = append(book.Sheets, read)
	}
	return book, nil
}

var printTitleRows = regexp.MustCompile(`^\$?(\d+):\$?(\d+)$`)

// PrintTitleRows returns the 0-based rows of a print titles reference:
// 'Bill of quantities'!$3:$4, also next to repeated columns.
func PrintTitleRows(ref string) (int, int, bool) {
	for _, part := range strings.Split(ref, ",") {
		part = strings.TrimSpace(part[strings.LastIndexByte(part, '!')+1:])
		m := printTitleRows.FindStringSubmatch(part)
		if m == nil {
			continue
		}
		f, e1 := strconv.Atoi(m[1])
		l, e2 := strconv.Atoi(m[2])
		if e1 == nil && e2 == nil && f >= 1 && l >= f {
			return f - 1, l - 1, true
		}
	}
	return 0, 0, false
}

// richText is the text of a shared or inline string; phonetic hints skipped.
func (r *xlsxReader) richText(item *xdom.Node) string {
	if t := item.Child(r.ns, "t"); t != nil {
		return t.Value()
	}
	var sb strings.Builder
	for _, run := range item.All(r.ns, "r") {
		sb.WriteString(run.Child(r.ns, "t").Value())
	}
	return sb.String()
}

func on(n *xdom.Node) bool {
	if n == nil {
		return false
	}
	v, ok := n.Attr("", "val")
	return !ok || v == "1" || v == "true"
}

func onAttr(v string) bool { return v == "1" || v == "true" }

func index(n *xdom.Node, attr string) int {
	v, err := strconv.Atoi(n.A(attr))
	if err != nil || v < 0 {
		return 0
	}
	return v
}

func parseFloat(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v, err == nil
}

func (r *xlsxReader) readStyles(root *xdom.Node, colors *opc.Colors) {
	if root == nil {
		return
	}
	formats := map[int]string{}
	for _, f := range root.Child(r.ns, "numFmts").All(r.ns, "numFmt") {
		if id, err := strconv.Atoi(f.A("numFmtId")); err == nil {
			code, ok := f.Attr("", "formatCode")
			if !ok {
				code = "General"
			}
			formats[id] = code
		}
	}
	type font struct {
		bold, italic, underline, strike bool
		size                            float64
		color                           string
	}
	var fonts []font
	for _, f := range root.Child(r.ns, "fonts").All(r.ns, "font") {
		u := f.Child(r.ns, "u")
		size, _ := parseFloat(f.Child(r.ns, "sz").A("val"))
		fonts = append(fonts, font{
			bold: on(f.Child(r.ns, "b")), italic: on(f.Child(r.ns, "i")), strike: on(f.Child(r.ns, "strike")),
			underline: u != nil && u.A("val") != "none", size: size, color: colors.Spreadsheet(f.Child(r.ns, "color")),
		})
	}
	var fills []string
	for _, f := range root.Child(r.ns, "fills").All(r.ns, "fill") {
		p := f.Child(r.ns, "patternFill")
		t := p.A("patternType")
		if p == nil || t == "" || t == "none" || t == "gray125" {
			fills = append(fills, "")
			continue
		}
		c := colors.Spreadsheet(p.Child(r.ns, "fgColor"))
		if c == "" {
			c = colors.Spreadsheet(p.Child(r.ns, "bgColor"))
		}
		fills = append(fills, c)
	}
	side := func(n *xdom.Node) CellBorder {
		if n == nil {
			return NoBorder
		}
		s, ok := n.Attr("", "style")
		if !ok {
			return NoBorder
		}
		return CellBorder{Style: s, Color: colors.Spreadsheet(n.Child(r.ns, "color"))}
	}
	first := func(n *xdom.Node, a, b string) *xdom.Node {
		if c := n.Child(r.ns, a); c != nil {
			return c
		}
		return n.Child(r.ns, b)
	}
	var borders [][4]CellBorder
	for _, b := range root.Child(r.ns, "borders").All(r.ns, "border") {
		borders = append(borders, [4]CellBorder{side(first(b, "left", "start")), side(first(b, "right", "end")), side(b.Child(r.ns, "top")), side(b.Child(r.ns, "bottom"))})
	}
	for _, x := range root.Child(r.ns, "cellXfs").All(r.ns, "xf") {
		var fo font
		if i := index(x, "fontId"); i < len(fonts) {
			fo = fonts[i]
		}
		fill := ""
		if i := index(x, "fillId"); i < len(fills) {
			fill = fills[i]
		}
		border := [4]CellBorder{NoBorder, NoBorder, NoBorder, NoBorder}
		if i := index(x, "borderId"); i < len(borders) {
			border = borders[i]
		}
		al := x.Child(r.ns, "alignment")
		style := CellStyle{
			Bold: fo.bold, Italic: fo.italic, Underline: fo.underline, Strike: fo.strike, FontSize: fo.size, Color: fo.color, Fill: fill,
			Wrap: onAttr(al.A("wrapText")), Left: border[0], Right: border[1], Top: border[2], Bottom: border[3],
		}
		switch al.A("horizontal") {
		case "left":
			style.Align = AlignLeft
		case "center", "centerContinuous":
			style.Align = AlignCenter
		case "right":
			style.Align = AlignRight
		}
		switch al.A("vertical") {
		case "top":
			style.VerticalAlign = VAlignTop
		case "center":
			style.VerticalAlign = VAlignCenter
		}
		id := index(x, "numFmtId")
		code, ok := formats[id]
		if !ok {
			code, ok = BuiltInFormats[id]
			if !ok {
				code = "General"
			}
		}
		r.cellFormats = append(r.cellFormats, xf{style.normalized(), code})
	}
}

func (r *xlsxReader) readSheet(name string, xml *xdom.Node) *Worksheet {
	sheet := NewWorksheet(name)
	if w, ok := parseFloat(xml.Child(r.ns, "sheetFormatPr").A("defaultColWidth")); ok {
		sheet.DefaultColumnWidth = w
	}
	var pane *xdom.Node
	if views := xml.Child(r.ns, "sheetViews").All(r.ns, "sheetView"); len(views) > 0 {
		pane = views[0].Child(r.ns, "pane")
	}
	if s := pane.A("state"); s == "frozen" || s == "frozenSplit" {
		if y, ok := parseFloat(pane.A("ySplit")); ok {
			sheet.FrozenRows = int(y)
		}
	}
	for _, col := range xml.Child(r.ns, "cols").All(r.ns, "col") {
		lo := index(col, "min")
		hi := min(index(col, "max"), lo+16384)
		w, hasWidth := parseFloat(col.A("width"))
		for c := lo; c <= hi && c > 0; c++ {
			if hasWidth {
				sheet.ColumnWidths[c-1] = w
			}
			if onAttr(col.A("hidden")) || hasWidth && w == 0 {
				sheet.HiddenColumns[c-1] = true
			}
		}
	}
	nextRow := 0
	for _, row := range xml.Child(r.ns, "sheetData").All(r.ns, "row") {
		ri := nextRow
		if n, err := strconv.Atoi(row.A("r")); err == nil {
			ri = n - 1
		}
		nextRow = ri + 1
		if onAttr(row.A("hidden")) {
			sheet.HiddenRows[ri] = true
		}
		if onAttr(row.A("customHeight")) {
			if h, ok := parseFloat(row.A("ht")); ok {
				sheet.RowHeights[ri] = h
			}
		}
		nextColumn := 0
		for _, c := range row.All(r.ns, "c") {
			ci := nextColumn
			if _, col, ok := ParseCell(c.A("r")); ok {
				ci = col
			}
			nextColumn = ci + 1
			f := xf{DefaultStyle, "General"}
			if s := index(c, "s"); s < len(r.cellFormats) {
				f = r.cellFormats[s]
			}
			if v := r.value(c, f.style, f.format); v.HasContent() {
				sheet.Set(ri, ci, v)
			}
		}
	}
	for _, m := range xml.Child(r.ns, "mergeCells").All(r.ns, "mergeCell") {
		if rg, ok := ParseRange(m.A("ref")); ok && (rg.Bottom > rg.Top || rg.Right > rg.Left) {
			sheet.Merges = append(sheet.Merges, rg)
		}
	}
	// <brk id="20"/>: the break is above 0-based row 20, which starts the next page.
	for _, b := range xml.Child(r.ns, "rowBreaks").All(r.ns, "brk") {
		if id, err := strconv.Atoi(b.A("id")); err == nil && id > 0 {
			sheet.RowBreaks[id] = true
		}
	}
	return sheet
}

// value is the displayed text of a cell and, for numbers and booleans, the
// value behind it. Dates of a 1904-based workbook move to the 1900 system.
func (r *xlsxReader) value(c *xdom.Node, style CellStyle, format string) *Cell {
	t := c.A("t")
	if t == "" {
		t = "n"
	}
	raw := ""
	hasRaw := false
	if v := c.Child(r.ns, "v"); v != nil {
		raw, hasRaw = v.Value(), true
	}
	switch t {
	case "s":
		i, err := strconv.Atoi(strings.TrimSpace(raw))
		if err == nil && i >= 0 && i < len(r.shared) {
			return TextCell(r.shared[i], style)
		}
		return TextCell("", style)
	case "inlineStr":
		if is := c.Child(r.ns, "is"); is != nil {
			return TextCell(r.richText(is), style)
		}
		return TextCell(raw, style)
	case "str", "e":
		return TextCell(raw, style)
	case "b":
		v := 0.0
		text := "FALSE"
		if raw == "1" || raw == "true" {
			v, text = 1, "TRUE"
		}
		return &Cell{Text: text, Style: style, Value: &v, IsBoolean: true}
	case "d":
		if d, ok := parseISODate(raw); ok {
			off := 0.0
			if r.date1904 {
				off = date1904Offset
			}
			return r.number(ToSerial(d)-off, style, format)
		}
		return TextCell(raw, style)
	default:
		if v, ok := parseFloat(raw); ok && hasRaw {
			return r.number(v, style, format)
		}
		return TextCell(raw, style)
	}
}

func parseISODate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02", "15:04:05", "15:04"} {
		if d, err := time.Parse(layout, s); err == nil {
			if layout == "15:04:05" || layout == "15:04" {
				d = time.Date(1899, 12, 30, d.Hour(), d.Minute(), d.Second(), 0, time.UTC)
			}
			return d, true
		}
	}
	return time.Time{}, false
}

func (r *xlsxReader) number(v float64, style CellStyle, format string) *Cell {
	value := v
	if r.date1904 && r.formatter.IsDate(format) {
		value = v + date1904Offset
	}
	nf := format
	if IsGeneral(nf) {
		nf = ""
	}
	return &Cell{Text: r.formatter.Format(v, format), Style: style, IsNumber: true, Value: &value, NumberFormat: nf}
}
