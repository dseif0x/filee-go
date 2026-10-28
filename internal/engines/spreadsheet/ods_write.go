package spreadsheet

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/dseif0x/filee-go/internal/doc"
)

// Workbook → ODS: one table per sheet with typed cells (numbers,
// percentages, currencies, dates and times keep their value and number
// format as a data style; the shown text is the cached text:p), booleans,
// text with line breaks and repeated spaces, cell styles, merged cells,
// column widths, row heights, hidden rows and columns, frozen rows.

const (
	odsMime     = "application/vnd.oasis.opendocument.spreadsheet"
	odfVersion  = "1.3"
	declaration = `<?xml version="1.0" encoding="UTF-8"?>`
	odfNs       = `xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" ` +
		`xmlns:style="urn:oasis:names:tc:opendocument:xmlns:style:1.0" ` +
		`xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0" ` +
		`xmlns:table="urn:oasis:names:tc:opendocument:xmlns:table:1.0" ` +
		`xmlns:fo="urn:oasis:names:tc:opendocument:xmlns:xsl-fo-compatible:1.0" ` +
		`xmlns:number="urn:oasis:names:tc:opendocument:xmlns:datastyle:1.0"`
	// sheetColumns: LibreOffice before 7.4 stops at 1024 columns and warns about anything beyond.
	sheetColumns = 1024
	// Excel's default font size, so a workbook from Excel keeps its proportions.
	odsStyles = declaration + `<office:document-styles ` + odfNs + ` office:version="` + odfVersion + `"><office:styles>` +
		`<style:default-style style:family="table-cell"><style:text-properties fo:font-size="11pt" style:font-size-asian="11pt" style:font-size-complex="11pt"/></style:default-style>` +
		`<style:style style:name="Default" style:family="table-cell"/>` +
		`</office:styles></office:document-styles>`
)

// WriteODS writes book as an ODS file.
func WriteODS(book *Workbook, path string) error {
	sheets := book.Sheets
	if len(sheets) == 0 {
		sheets = []*Worksheet{NewWorksheet("Sheet1")}
	}
	names := make([]string, len(sheets))
	for i, s := range sheets {
		names[i] = s.Name
	}
	names = UniqueNames(names)
	styles := newAutoStyles()
	var body strings.Builder
	type frozen struct {
		name string
		rows int
	}
	var frozenRows []frozen
	for i, s := range sheets {
		odsTable(&body, s, names[i], styles)
		if s.FrozenRows > 0 {
			frozenRows = append(frozenRows, frozen{names[i], s.FrozenRows})
		}
	}
	content := declaration + `<office:document-content ` + odfNs + ` office:version="` + odfVersion + `"><office:automatic-styles>` + styles.xml() +
		`</office:automatic-styles><office:body><office:spreadsheet>` + body.String() + `</office:spreadsheet></office:body></office:document-content>`
	parts := [][2]string{{"mimetype", odsMime}, {"content.xml", content}, {"styles.xml", odsStyles}}
	manifest := declaration + `<manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0" manifest:version="` + odfVersion + `">` +
		`<manifest:file-entry manifest:full-path="/" manifest:version="` + odfVersion + `" manifest:media-type="` + odsMime + `"/>` +
		`<manifest:file-entry manifest:full-path="content.xml" manifest:media-type="text/xml"/>` +
		`<manifest:file-entry manifest:full-path="styles.xml" manifest:media-type="text/xml"/>`
	if len(frozenRows) > 0 {
		var sb strings.Builder
		sb.WriteString(declaration + `<office:document-settings xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:config="urn:oasis:names:tc:opendocument:xmlns:config:1.0" office:version="` + odfVersion + `">`)
		sb.WriteString(`<office:settings><config:config-item-set config:name="ooo:view-settings"><config:config-item-map-indexed config:name="Views"><config:config-item-map-entry>`)
		sb.WriteString(`<config:config-item config:name="ViewId" config:type="string">view1</config:config-item><config:config-item-map-named config:name="Tables">`)
		for _, f := range frozenRows {
			fmt.Fprintf(&sb, `<config:config-item-map-entry config:name="%s">`, doc.EscapeXML(f.name))
			item := func(name, typ string, v int) {
				fmt.Fprintf(&sb, `<config:config-item config:name="%s" config:type="%s">%d</config:config-item>`, name, typ, v)
			}
			item("HorizontalSplitMode", "short", 0)
			item("VerticalSplitMode", "short", 2)
			item("HorizontalSplitPosition", "int", 0)
			item("VerticalSplitPosition", "int", f.rows)
			item("ActiveSplitRange", "short", 2)
			item("PositionLeft", "int", 0)
			item("PositionRight", "int", 0)
			item("PositionTop", "int", 0)
			item("PositionBottom", "int", f.rows)
			sb.WriteString("</config:config-item-map-entry>")
		}
		sb.WriteString(`</config:config-item-map-named></config:config-item-map-entry></config:config-item-map-indexed></config:config-item-set></office:settings></office:document-settings>`)
		parts = append(parts, [2]string{"settings.xml", sb.String()})
		manifest += `<manifest:file-entry manifest:full-path="settings.xml" manifest:media-type="text/xml"/>`
	}
	parts = append(parts, [2]string{"META-INF/manifest.xml", manifest + "</manifest:manifest>"})
	return WriteZip(path, parts)
}

func repeated(what string, n int) string {
	if n > 1 {
		return fmt.Sprintf(` table:number-%s-repeated="%d"`, what, n)
	}
	return ""
}

type rc struct{ r, c int }

func odsTable(sb *strings.Builder, sheet *Worksheet, name string, styles *autoStyles) {
	used, ok := sheet.UsedRange()
	right, bottom := 0, 0
	if ok {
		right, bottom = used.Right, used.Bottom
	}
	fmt.Fprintf(sb, `<table:table table:name="%s" table:style-name="ta1">`, doc.EscapeXML(name))
	for c := 0; c <= right; {
		w := sheet.ColumnWidth(c)
		hidden := sheet.HiddenColumns[c]
		n := 1
		for c+n <= right && sheet.ColumnWidth(c+n) == w && sheet.HiddenColumns[c+n] == hidden {
			n++
		}
		vis := ""
		if hidden {
			vis = ` table:visibility="collapse"`
		}
		fmt.Fprintf(sb, `<table:table-column table:style-name="%s"%s%s/>`, styles.column(w), repeated("columns", n), vis)
		c += n
	}
	// The rest of the sheet gets the workbook's default width.
	if right+1 < sheetColumns {
		fmt.Fprintf(sb, `<table:table-column table:style-name="%s"%s/>`, styles.column(sheet.DefaultColumnWidth), repeated("columns", sheetColumns-right-1))
	}
	// Merged areas: the top-left cell spans, the others are covered cells.
	anchors := map[rc]CellRange{}
	covered := map[rc]bool{}
	for _, m := range sheet.Merges {
		k := rc{m.Top, m.Left}
		if _, dup := anchors[k]; dup || covered[k] {
			continue // overlapping merges are invalid; keep the first
		}
		anchors[k] = m
		for r := m.Top; r <= m.Bottom; r++ {
			for c := m.Left; c <= m.Right; c++ {
				if r != m.Top || c != m.Left {
					covered[rc{r, c}] = true
				}
			}
		}
	}
	coveredRows := map[int]bool{}
	for k := range covered {
		if k.c <= right {
			coveredRows[k.r] = true
		}
	}
	empty := func(r int) bool { return len(sheet.Rows[r]) == 0 && !coveredRows[r] }
	heightOf := func(r int) *float64 {
		if h, ok := sheet.RowHeights[r]; ok {
			return &h
		}
		return nil
	}
	same := func(a, b *float64) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
	for r := 0; r <= bottom; {
		height := heightOf(r)
		hidden := sheet.HiddenRows[r]
		vis := ""
		if hidden {
			vis = ` table:visibility="collapse"`
		}
		if empty(r) {
			n := 1
			for r+n <= bottom && empty(r+n) && sheet.HiddenRows[r+n] == hidden && same(heightOf(r+n), height) {
				n++
			}
			fmt.Fprintf(sb, `<table:table-row table:style-name="%s"%s%s><table:table-cell%s/></table:table-row>`, styles.row(height), repeated("rows", n), vis, repeated("columns", right+1))
			r += n
			continue
		}
		fmt.Fprintf(sb, `<table:table-row table:style-name="%s"%s>`, styles.row(height), vis)
		for c := 0; c <= right; {
			cell := sheet.Get(r, c)
			isCovered := covered[rc{r, c}]
			_, anchor := anchors[rc{r, c}]
			if isCovered || cell == nil && !anchor {
				n := 1
				for c+n <= right && covered[rc{r, c + n}] == isCovered {
					if !isCovered {
						_, a := anchors[rc{r, c + n}]
						if sheet.Get(r, c+n) != nil || a {
							break
						}
					}
					n++
				}
				sb.WriteString(pick(isCovered, "<table:covered-table-cell", "<table:table-cell") + repeated("columns", n) + "/>")
				c += n
				continue
			}
			var m *CellRange
			if a, ok := anchors[rc{r, c}]; ok {
				m = &a
			}
			odsCell(sb, cell, m, styles)
			c++
		}
		sb.WriteString("</table:table-row>")
		r++
	}
	sb.WriteString("</table:table>")
}

func odsCell(sb *strings.Builder, cell *Cell, m *CellRange, styles *autoStyles) {
	sb.WriteString("<table:table-cell")
	if m != nil {
		if m.Right > m.Left {
			fmt.Fprintf(sb, ` table:number-columns-spanned="%d"`, m.Right-m.Left+1)
		}
		if m.Bottom > m.Top {
			fmt.Fprintf(sb, ` table:number-rows-spanned="%d"`, m.Bottom-m.Top+1)
		}
	}
	if cell == nil {
		sb.WriteString("/>")
		return
	}
	number := cellNumber(cell)
	data := ""
	switch {
	case cell.IsBoolean && number != nil:
		fmt.Fprintf(sb, ` office:value-type="boolean" office:boolean-value="%s"`, pick(*number != 0, "true", "false"))
	case number != nil && !math.IsNaN(*number) && !math.IsInf(*number, 0):
		name, valueType := styles.dataStyle(cell.NumberFormat)
		data = name
		sb.WriteString(odsValue(*number, valueType))
	case cell.Text != "":
		sb.WriteString(` office:value-type="string"`)
	}
	if name := styles.cell(cell.Style, data); name != "" {
		fmt.Fprintf(sb, ` table:style-name="%s"`, name)
	}
	if cell.Text == "" {
		sb.WriteString("/>")
		return
	}
	sb.WriteByte('>')
	odsParagraphs(sb, cell.Text)
	sb.WriteString("</table:table-cell>")
}

// odsValue: dates as ISO dates, times as durations, the rest as numbers.
func odsValue(v float64, valueType string) string {
	n := strconv.FormatFloat(v, 'g', -1, 64)
	switch {
	case valueType == "date" && v > -657000 && v < 2958000:
		d := FromSerial(v)
		iso := d.Format("2006-01-02")
		if d.Hour() != 0 || d.Minute() != 0 || d.Second() != 0 || d.Nanosecond() != 0 {
			if d.Nanosecond() == 0 {
				iso = d.Format("2006-01-02T15:04:05")
			} else {
				iso = d.Format("2006-01-02T15:04:05.000")
			}
		}
		return ` office:value-type="date" office:date-value="` + iso + `"`
	case valueType == "time" && math.Abs(v) < 1e7:
		ms := math.Round(math.Abs(v) * 86400000)
		dur := time.Duration(ms) * time.Millisecond
		hours := int64(dur / time.Hour)
		minutes := int64(dur/time.Minute) % 60
		secs := float64(dur%time.Minute) / float64(time.Second)
		s := strconv.FormatFloat(secs, 'f', 3, 64)
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
		if secs < 10 {
			s = "0" + s
		}
		sign := pick(v < 0, "-", "")
		return fmt.Sprintf(` office:value-type="time" office:time-value="%sPT%dH%02dM%sS"`, sign, hours, minutes, s)
	case valueType == "percentage" || valueType == "currency":
		return fmt.Sprintf(` office:value-type="%s" office:value="%s"`, valueType, n)
	}
	return ` office:value-type="float" office:value="` + n + `"`
}

// odsParagraphs: one text:p per line; runs of spaces and tabs as text:s and text:tab.
func odsParagraphs(sb *strings.Builder, text string) {
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		sb.WriteString("<text:p>")
		rs := []rune(line)
		for i := 0; i < len(rs); i++ {
			ch := rs[i]
			switch {
			case ch == ' ':
				run := 1
				for i+run < len(rs) && rs[i+run] == ' ' {
					run++
				}
				// The first space of a run stays text unless the line starts with it.
				extra := run - 1
				if i == 0 {
					extra = run
				} else {
					sb.WriteByte(' ')
				}
				if extra == 1 {
					sb.WriteString("<text:s/>")
				} else if extra > 1 {
					fmt.Fprintf(sb, `<text:s text:c="%d"/>`, extra)
				}
				i += run - 1
			case ch == '\t':
				sb.WriteString("<text:tab/>")
			case ch >= ' ':
				sb.WriteString(doc.EscapeXML(string(ch)))
			}
		}
		sb.WriteString("</text:p>")
	}
}

type autoStyles struct {
	columns, rows, data, cells strings.Builder
	columnNames                map[float64]string
	rowNames                   map[float64]string
	dataNames                  map[string][2]string
	cellNames                  map[styleKey]string
}

func newAutoStyles() *autoStyles {
	return &autoStyles{columnNames: map[float64]string{}, rowNames: map[float64]string{}, dataNames: map[string][2]string{}, cellNames: map[styleKey]string{}}
}

func (a *autoStyles) column(w float64) string {
	if n, ok := a.columnNames[w]; ok {
		return n
	}
	n := "co" + strconv.Itoa(len(a.columnNames)+1)
	a.columnNames[w] = n
	fmt.Fprintf(&a.columns, `<style:style style:name="%s" style:family="table-column"><style:table-column-properties fo:break-before="auto" style:column-width="%spt"/></style:style>`, n, fmtG(w*PointsPerCharacter, 3))
	return n
}

// row is a row style: a custom height, or (nil) the height that fits the content.
func (a *autoStyles) row(h *float64) string {
	key := -1.0
	if h != nil {
		key = *h
	}
	if n, ok := a.rowNames[key]; ok {
		return n
	}
	n := "ro" + strconv.Itoa(len(a.rowNames)+1)
	a.rowNames[key] = n
	height := 15.0
	if h != nil {
		height = *h
	}
	fmt.Fprintf(&a.rows, `<style:style style:name="%s" style:family="table-row"><style:table-row-properties style:row-height="%spt" fo:break-before="auto" style:use-optimal-row-height="%s"/></style:style>`, n, fmtG(height, 2), pick(h == nil, "true", "false"))
	return n
}

// dataStyle is the data style of an Excel code ("" = General) and the value type of its cells.
func (a *autoStyles) dataStyle(format string) (string, string) {
	if format == "" {
		return "", "float"
	}
	if r, ok := a.dataNames[format]; ok {
		return r[0], r[1]
	}
	name := "N" + strconv.Itoa(len(a.dataNames)+1)
	r := [2]string{"", "float"}
	if xml, vt, ok := ExcelToOdf(format, name); ok {
		a.data.WriteString(xml)
		r = [2]string{name, vt}
	}
	a.dataNames[format] = r
	return r[0], r[1]
}

func (a *autoStyles) cell(style CellStyle, data string) string {
	style = style.normalized()
	if style == DefaultStyle && data == "" {
		return ""
	}
	key := styleKey{style, data}
	if n, ok := a.cellNames[key]; ok {
		return n
	}
	n := "ce" + strconv.Itoa(len(a.cellNames)+1)
	a.cellNames[key] = n
	b := &a.cells
	fmt.Fprintf(b, `<style:style style:name="%s" style:family="table-cell" style:parent-style-name="Default"`, n)
	if data != "" {
		fmt.Fprintf(b, ` style:data-style-name="%s"`, data)
	}
	b.WriteString("><style:table-cell-properties")
	if style.Fill != "" {
		fmt.Fprintf(b, ` fo:background-color="%s"`, style.Fill)
	}
	if style.Left == style.Right && style.Left == style.Top && style.Left == style.Bottom {
		if style.Left.IsVisible() {
			fmt.Fprintf(b, ` fo:border="%s"`, odfBorderOf(style.Left))
		}
	} else {
		for _, s := range []struct {
			name string
			b    CellBorder
		}{{"left", style.Left}, {"right", style.Right}, {"top", style.Top}, {"bottom", style.Bottom}} {
			v := "none"
			if s.b.IsVisible() {
				v = odfBorderOf(s.b)
			}
			fmt.Fprintf(b, ` fo:border-%s="%s"`, s.name, v)
		}
	}
	if style.VerticalAlign != VAlignBottom {
		fmt.Fprintf(b, ` style:vertical-align="%s"`, pick(style.VerticalAlign == VAlignTop, "top", "middle"))
	}
	if style.Wrap {
		b.WriteString(` fo:wrap-option="wrap"`)
	}
	if style.Align != AlignGeneral {
		b.WriteString(` style:text-align-source="fix" style:repeat-content="false"`)
	}
	b.WriteString("/>")
	if style.Align != AlignGeneral {
		al := "end"
		switch style.Align {
		case AlignLeft:
			al = "start"
		case AlignCenter:
			al = "center"
		}
		fmt.Fprintf(b, `<style:paragraph-properties fo:text-align="%s"/>`, al)
	}
	var t strings.Builder
	if style.Bold {
		t.WriteString(` fo:font-weight="bold" style:font-weight-asian="bold" style:font-weight-complex="bold"`)
	}
	if style.Italic {
		t.WriteString(` fo:font-style="italic" style:font-style-asian="italic" style:font-style-complex="italic"`)
	}
	if style.Underline {
		t.WriteString(` style:text-underline-style="solid" style:text-underline-width="auto" style:text-underline-color="font-color"`)
	}
	if style.Strike {
		t.WriteString(` style:text-line-through-style="solid" style:text-line-through-type="single"`)
	}
	if style.FontSize != 0 {
		s := fmtG(style.FontSize, 2)
		fmt.Fprintf(&t, ` fo:font-size="%spt" style:font-size-asian="%spt" style:font-size-complex="%spt"`, s, s, s)
	}
	if style.Color != "" {
		fmt.Fprintf(&t, ` fo:color="%s"`, style.Color)
	}
	if t.Len() > 0 {
		b.WriteString("<style:text-properties" + t.String() + "/>")
	}
	b.WriteString("</style:style>")
	return n
}

func (a *autoStyles) xml() string {
	return a.columns.String() + a.rows.String() +
		`<style:style style:name="ta1" style:family="table"><style:table-properties table:display="true" style:writing-mode="lr-tb"/></style:style>` +
		a.data.String() + a.cells.String()
}

// odfBorderOf: an Excel border as LibreOffice writes it when importing the same style.
func odfBorderOf(b CellBorder) string {
	width, pattern := 0.75, "solid"
	switch b.Style {
	case "hair":
		width = 0.1
	case "medium":
		width = 1.75
	case "thick":
		width = 2.5
	case "double":
		width, pattern = 2.25, "double"
	case "dotted":
		pattern = "dotted"
	case "dashed":
		pattern = "dashed"
	case "mediumDashed":
		width, pattern = 1.75, "dashed"
	case "dashDot":
		pattern = "dot-dash"
	case "mediumDashDot", "slantDashDot":
		width, pattern = 1.75, "dot-dash"
	case "dashDotDot":
		pattern = "dot-dot-dash"
	case "mediumDashDotDot":
		width, pattern = 1.75, "dot-dot-dash"
	}
	c := b.Color
	if c == "" {
		c = "#000000"
	}
	return strconv.FormatFloat(width, 'f', -1, 64) + "pt " + pattern + " " + c
}
