package spreadsheet

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dseif0x/filee-go/internal/xdom"
)

// ODS (OpenDocument spreadsheet) → Workbook, read from content.xml,
// styles.xml and settings.xml: sheets, repeated rows and columns (the huge
// empty repeats that end every sheet are skipped), merged cells, values with
// the text Calc shows, number formats as Excel codes, cell styles, column
// widths, row heights, hidden rows / columns / sheets and frozen rows.
// Charts, images, comments and conditional formats are not read.

const (
	officeNs   = "urn:oasis:names:tc:opendocument:xmlns:office:1.0"
	tableNs    = "urn:oasis:names:tc:opendocument:xmlns:table:1.0"
	textNs     = "urn:oasis:names:tc:opendocument:xmlns:text:1.0"
	configNs   = "urn:oasis:names:tc:opendocument:xmlns:config:1.0"
	manifestNs = "urn:oasis:names:tc:opendocument:xmlns:manifest:1.0"

	maxRows    = 1_048_576
	maxColumns = 16_384
	// maxStyledRepeat: empty formatted cells repeated more often than this are the formatted rest of a sheet.
	maxStyledRepeat = 256
	// PointsPerCharacter of column width (7 px of Calibri 11 at 96 dpi).
	PointsPerCharacter = 5.25
)

type familyName struct{ family, name string }

type cellStyleInfo struct {
	style     CellStyle
	dataStyle string
}

type odsReader struct {
	styles       map[familyName]*xdom.Node
	dataStyles   map[string]*xdom.Node
	cellStyles   map[string]cellStyleInfo
	formats      map[string]string
	frozen       map[string]int
	formatter    *NumberFormatter
	defaultStyle *xdom.Node
}

// ReadODS reads every visible sheet of an ODS / OTS file.
func ReadODS(path string) (*Workbook, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, errors.New("the file is not an OpenDocument spreadsheet: " + err.Error())
	}
	defer z.Close()
	load := func(name string) *xdom.Node {
		for _, f := range z.File {
			if f.Name == name {
				rc, err := f.Open()
				if err != nil {
					return nil
				}
				defer rc.Close()
				n, _ := xdom.Parse(rc, 512<<20)
				return n
			}
		}
		return nil
	}
	if m := load("META-INF/manifest.xml"); m != nil && len(m.Descendants(manifestNs, "encryption-data")) > 0 {
		return nil, errors.New("the spreadsheet is protected with a password (encrypted); remove the password in LibreOffice and convert it again")
	}
	content := load("content.xml")
	if content == nil {
		return nil, errors.New("the file is not an OpenDocument spreadsheet (no content.xml)")
	}
	return readODSContent(content, load("styles.xml"), load("settings.xml"))
}

// ReadFODS reads a flat OpenDocument spreadsheet (one XML file).
func ReadFODS(r io.Reader) (*Workbook, error) {
	root, err := xdom.Parse(r, 512<<20)
	if err != nil {
		return nil, err
	}
	return readODSContent(root, root, root)
}

func readODSContent(content, styles, settings *xdom.Node) (*Workbook, error) {
	spreadsheet := content.Child(officeNs, "body").Child(officeNs, "spreadsheet")
	if spreadsheet == nil {
		return nil, errors.New("the file is not an OpenDocument spreadsheet (no office:spreadsheet)")
	}
	r := &odsReader{styles: map[familyName]*xdom.Node{}, dataStyles: map[string]*xdom.Node{}, cellStyles: map[string]cellStyleInfo{},
		formats: map[string]string{}, frozen: map[string]int{}, formatter: NewFormatter(false)}
	r.readStyles(styles, content)
	r.readSettings(settings)
	book := &Workbook{}
	for _, table := range spreadsheet.All(tableNs, "table") {
		ts := r.find("table", table.AN(tableNs, "style-name"))
		if ts.Child(styleNs, "table-properties").AN(tableNs, "display") != "false" {
			book.Sheets = append(book.Sheets, r.readTable(table))
		}
	}
	return book, nil
}

func (r *odsReader) readStyles(styles, content *xdom.Node) {
	var containers []*xdom.Node
	if styles != nil {
		containers = append(containers, styles.Child(officeNs, "styles"), styles.Child(officeNs, "automatic-styles"))
	}
	if content != styles {
		containers = append(containers, content.Child(officeNs, "automatic-styles"))
	}
	for _, c := range containers {
		for _, el := range c.Elements() {
			name, hasName := el.Attr(styleNs, "name")
			switch {
			case el.Is(styleNs, "default-style") && el.AN(styleNs, "family") == "table-cell":
				r.defaultStyle = el
			case el.Is(styleNs, "style") && hasName:
				r.styles[familyName{el.AN(styleNs, "family"), name}] = el
			case el.Name.Space == numberNs && hasName:
				r.dataStyles[name] = el
			}
		}
	}
}

func (r *odsReader) find(family, name string) *xdom.Node {
	if name == "" {
		return nil
	}
	return r.styles[familyName{family, name}]
}

func (r *odsReader) cellStyleOf(name string) cellStyleInfo {
	if name == "" {
		name = "Default"
	}
	if c, ok := r.cellStyles[name]; ok {
		return c
	}
	var chain []*xdom.Node
	for s := r.find("table-cell", name); s != nil && len(chain) < 20; s = r.find("table-cell", s.AN(styleNs, "parent-style-name")) {
		chain = append([]*xdom.Node{s}, chain...)
	}
	if r.defaultStyle != nil {
		chain = append([]*xdom.Node{r.defaultStyle}, chain...)
	}
	var p styleProps
	data := ""
	for _, s := range chain {
		p.apply(s)
		if d, ok := s.Attr(styleNs, "data-style-name"); ok {
			data = d
		}
	}
	c := cellStyleInfo{p.cellStyle(), data}
	r.cellStyles[name] = c
	return c
}

func (r *odsReader) formatOf(dataStyle string) string {
	if dataStyle == "" {
		return ""
	}
	f, ok := r.formats[dataStyle]
	if !ok {
		if s := r.dataStyles[dataStyle]; s != nil {
			f = OdfToExcel(s, func(n string) *xdom.Node { return r.dataStyles[n] })
		}
		r.formats[dataStyle] = f
	}
	return f
}

type styleProps struct {
	bold, italic, underline, strike, wrap *bool
	size                                  *float64
	color, fill                           *string
	textAlign, alignSource, verticalAlign *string
	left, right, top, bottom              *CellBorder
}

func bp(v bool) *bool                                       { return &v }
func sp(v string) *string                                   { return &v }
func attr(n *xdom.Node, space, local string) (string, bool) { return n.Attr(space, local) }

func (p *styleProps) apply(style *xdom.Node) {
	if text := style.Child(styleNs, "text-properties"); text != nil {
		w, ok := attr(text, foNs, "font-weight")
		if !ok {
			w, ok = attr(text, styleNs, "font-weight-asian")
		}
		if ok {
			n, err := strconv.Atoi(w)
			p.bold = bp(w == "bold" || err == nil && n >= 600)
		}
		it, ok := attr(text, foNs, "font-style")
		if !ok {
			it, ok = attr(text, styleNs, "font-style-asian")
		}
		if ok {
			p.italic = bp(it == "italic" || it == "oblique")
		}
		if u, ok := attr(text, styleNs, "text-underline-style"); ok {
			p.underline = bp(u != "none")
		}
		if s, ok := attr(text, styleNs, "text-line-through-style"); ok {
			p.strike = bp(s != "none")
		}
		if v, ok := OdfPoints(text.AN(foNs, "font-size")); ok {
			p.size = &v
		}
		if c := odfColor(text.AN(foNs, "color")); c != "" {
			p.color = sp(c)
		}
	}
	if cell := style.Child(styleNs, "table-cell-properties"); cell != nil {
		if f, ok := attr(cell, foNs, "background-color"); ok {
			if f == "transparent" {
				p.fill = nil
			} else {
				p.fill = sp(odfColor(f))
			}
		}
		if all, ok := attr(cell, foNs, "border"); ok {
			b := odfBorder(all)
			p.left, p.right, p.top, p.bottom = &b, &b, &b, &b
		}
		for _, s := range []struct {
			name string
			dst  **CellBorder
		}{{"border-left", &p.left}, {"border-right", &p.right}, {"border-top", &p.top}, {"border-bottom", &p.bottom}} {
			if v, ok := attr(cell, foNs, s.name); ok {
				b := odfBorder(v)
				*s.dst = &b
			}
		}
		if v, ok := attr(cell, styleNs, "vertical-align"); ok {
			p.verticalAlign = sp(v)
		}
		if v, ok := attr(cell, foNs, "wrap-option"); ok {
			p.wrap = bp(v == "wrap")
		}
		if v, ok := attr(cell, styleNs, "text-align-source"); ok {
			p.alignSource = sp(v)
		}
	}
	if v, ok := attr(style.Child(styleNs, "paragraph-properties"), foNs, "text-align"); ok {
		p.textAlign = sp(v)
	}
}

func deref[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

func (p *styleProps) cellStyle() CellStyle {
	s := CellStyle{
		Bold: deref(p.bold, false), Italic: deref(p.italic, false), Underline: deref(p.underline, false), Strike: deref(p.strike, false),
		FontSize: deref(p.size, 0), Color: deref(p.color, ""), Fill: deref(p.fill, ""), Wrap: deref(p.wrap, false),
		Left: deref(p.left, NoBorder), Right: deref(p.right, NoBorder), Top: deref(p.top, NoBorder), Bottom: deref(p.bottom, NoBorder),
	}
	// "value-type" is Calc's automatic alignment (numbers right, text left), like Excel's General.
	if deref(p.alignSource, "") != "value-type" {
		switch deref(p.textAlign, "") {
		case "start", "left", "justify":
			s.Align = AlignLeft
		case "center":
			s.Align = AlignCenter
		case "end", "right":
			s.Align = AlignRight
		}
	}
	switch deref(p.verticalAlign, "") {
	case "top":
		s.VerticalAlign = VAlignTop
	case "middle":
		s.VerticalAlign = VAlignCenter
	}
	return s.normalized()
}

// odfBorder: "0.75pt solid #000000" → the Excel border style of that weight and pattern.
func odfBorder(v string) CellBorder {
	parts := strings.Fields(v)
	pattern := ""
	for _, p := range parts {
		switch p {
		case "solid", "dotted", "dashed", "dot-dash", "dot-dot-dash", "double", "groove", "ridge", "inset", "outset", "fine-dashed", "dash-dot", "dash-dot-dot", "none", "hidden":
			pattern = p
		}
		if pattern != "" {
			break
		}
	}
	if pattern == "" || pattern == "none" || pattern == "hidden" {
		return NoBorder
	}
	width := 0.75
	for _, p := range parts {
		if w, ok := OdfPoints(p); ok {
			width = w
			break
		}
	}
	color := ""
	for _, p := range parts {
		if c := odfColor(p); c != "" {
			color = c
			break
		}
	}
	var style string
	switch pattern {
	case "double":
		style = "double"
	case "dotted":
		style = "dotted"
	case "dashed", "fine-dashed":
		style = pick(width > 1.1, "mediumDashed", "dashed")
	case "dot-dash", "dash-dot":
		style = pick(width > 1.1, "mediumDashDot", "dashDot")
	case "dot-dot-dash", "dash-dot-dot":
		style = pick(width > 1.1, "mediumDashDotDot", "dashDotDot")
	default:
		switch {
		case width < 0.3:
			style = "hair"
		case width <= 1.1:
			style = "thin"
		case width <= 2.1:
			style = "medium"
		default:
			style = "thick"
		}
	}
	return CellBorder{style, color}
}

var hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func odfColor(v string) string {
	if hexColor.MatchString(v) {
		return strings.ToUpper(v)
	}
	return ""
}

// OdfPoints converts an ODF length ("2.258cm", "0.889in", "15pt") to points.
func OdfPoints(length string) (float64, bool) {
	if length == "" {
		return 0, false
	}
	i := 0
	for i < len(length) && strings.IndexByte("-+.0123456789", length[i]) >= 0 {
		i++
	}
	v, err := strconv.ParseFloat(length[:i], 64)
	if err != nil {
		return 0, false
	}
	switch length[i:] {
	case "pt":
		return v, true
	case "in":
		return v * 72, true
	case "cm":
		return v * 72 / 2.54, true
	case "mm":
		return v * 72 / 25.4, true
	case "pc":
		return v * 12, true
	case "px":
		return v * 0.75, true
	}
	return 0, false
}

func (r *odsReader) readSettings(settings *xdom.Node) {
	if settings == nil {
		return
	}
	for _, m := range settings.Descendants(configNs, "config-item-map-named") {
		if m.AN(configNs, "name") != "Tables" {
			continue
		}
		for _, entry := range m.All(configNs, "config-item-map-entry") {
			item := func(name string) string {
				for _, i := range entry.All(configNs, "config-item") {
					if i.AN(configNs, "name") == name {
						return i.Value()
					}
				}
				return ""
			}
			rows, err := strconv.Atoi(item("VerticalSplitPosition"))
			if item("VerticalSplitMode") == "2" && err == nil && rows > 0 {
				if sheet, ok := entry.Attr(configNs, "name"); ok {
					r.frozen[sheet] = rows
				}
			}
		}
		break
	}
}

type span struct {
	start, count int
	size         *float64
	hidden       bool
	defaultStyle string
}

type cellRun struct {
	column, count    int
	cell             *Cell
	colSpan, rowSpan int
}

func (r *odsReader) readTable(table *xdom.Node) *Worksheet {
	name := table.AN(tableNs, "name")
	if name == "" {
		name = "Sheet"
	}
	sheet := NewWorksheet(name)
	var columns []span
	c := 0
	for _, col := range children(table, "table-column", "table-header-columns", "table-column-group", "table-columns") {
		if c >= maxColumns {
			break
		}
		count := min(repeat(col.n, tableNs, "number-columns-repeated"), maxColumns-c)
		var width *float64
		if w, ok := OdfPoints(r.find("table-column", col.n.AN(tableNs, "style-name")).Child(styleNs, "table-column-properties").AN(styleNs, "column-width")); ok {
			w /= PointsPerCharacter
			width = &w
		}
		columns = append(columns, span{c, count, width, collapsed(col.n), col.n.AN(tableNs, "default-cell-style-name")})
		c += count
	}
	var rows []span
	row, headerRows := 0, 0
	for _, rw := range children(table, "table-row", "table-header-rows", "table-row-group", "table-rows") {
		if row >= maxRows {
			break
		}
		count := min(repeat(rw.n, tableNs, "number-rows-repeated"), maxRows-row)
		props := r.find("table-row", rw.n.AN(tableNs, "style-name")).Child(styleNs, "table-row-properties")
		var height *float64
		// Calc saves every row's height; only rows not sized to their content have a custom one.
		if props.AN(styleNs, "use-optimal-row-height") != "true" {
			if h, ok := OdfPoints(props.AN(styleNs, "row-height")); ok {
				height = &h
			}
		}
		rows = append(rows, span{row, count, height, collapsed(rw.n), ""})
		if rw.header && headerRows == row {
			headerRows += count
		}
		cells := r.cells(rw.n, columns)
		hasText, any := false, false
		for _, run := range cells {
			if run.cell != nil && run.cell.Text != "" || run.colSpan > 1 || run.rowSpan > 1 {
				hasText = true
			}
			if run.cell != nil {
				any = true
			}
		}
		copies := 0
		if hasText {
			copies = count
		} else if any && count <= maxStyledRepeat {
			copies = count
		}
		for k := 0; k < copies; k++ {
			for _, run := range cells {
				for i := 0; i < run.count; i++ {
					if run.cell != nil {
						sheet.Set(row+k, run.column+i, run.cell)
					}
					if run.colSpan > 1 || run.rowSpan > 1 {
						sheet.Merges = append(sheet.Merges, CellRange{row + k, run.column + i, row + k + run.rowSpan - 1, run.column + i + run.colSpan - 1})
					}
				}
			}
		}
		row += count
	}
	// Widths, heights and hidden flags matter only where the sheet has something.
	right, bottom := -1, -1
	if used, ok := sheet.UsedRange(); ok {
		right, bottom = used.Right, used.Bottom
	}
	for _, m := range sheet.Merges {
		right, bottom = max(right, m.Right), max(bottom, m.Bottom)
	}
	for _, s := range columns {
		for i := s.start; i < s.start+s.count && i <= right; i++ {
			if s.size != nil {
				sheet.ColumnWidths[i] = math.Round(*s.size*10000) / 10000
			}
			if s.hidden {
				sheet.HiddenColumns[i] = true
			}
		}
		if s.size != nil && s.start <= right+1 && right+1 < s.start+s.count {
			sheet.DefaultColumnWidth = math.Round(*s.size*10000) / 10000
		}
	}
	for _, s := range rows {
		for i := s.start; i < s.start+s.count && i <= bottom; i++ {
			if s.size != nil {
				sheet.RowHeights[i] = math.Round(*s.size*100) / 100
			}
			if s.hidden {
				sheet.HiddenRows[i] = true
			}
		}
	}
	if f, ok := r.frozen[name]; ok {
		sheet.FrozenRows = f
	} else {
		sheet.FrozenRows = headerRows
	}
	return sheet
}

type child struct {
	n      *xdom.Node
	header bool
}

// children lists rows (or columns) in order, also inside groups; header marks header rows.
func children(parent *xdom.Node, item, header string, groups ...string) []child {
	var out []child
	for _, el := range parent.Elements() {
		switch {
		case el.Is(tableNs, item):
			out = append(out, child{el, false})
		case el.Is(tableNs, header):
			for _, c := range children(el, item, header, groups...) {
				out = append(out, child{c.n, true})
			}
		default:
			for _, g := range groups {
				if el.Is(tableNs, g) {
					out = append(out, children(el, item, header, groups...)...)
					break
				}
			}
		}
	}
	return out
}

func repeat(n *xdom.Node, space, local string) int {
	v, err := strconv.Atoi(n.AN(space, local))
	if err != nil || v <= 0 {
		return 1
	}
	return v
}

func collapsed(n *xdom.Node) bool {
	v := n.AN(tableNs, "visibility")
	return v == "collapse" || v == "filter"
}

func (r *odsReader) cells(row *xdom.Node, columns []span) []cellRun {
	var runs []cellRun
	rowDefault := row.AN(tableNs, "default-cell-style-name")
	c := 0
	for _, el := range row.Elements() {
		covered := el.Is(tableNs, "covered-table-cell")
		if !covered && !el.Is(tableNs, "table-cell") {
			continue
		}
		if c >= maxColumns {
			break
		}
		count := min(repeat(el, tableNs, "number-columns-repeated"), maxColumns-c)
		if !covered {
			styleName, ok := el.Attr(tableNs, "style-name")
			if !ok {
				styleName = rowDefault
				if styleName == "" {
					for _, s := range columns {
						if s.start <= c && c < s.start+s.count {
							styleName = s.defaultStyle
							break
						}
					}
				}
			}
			cell := r.cell(el, styleName)
			if !cell.HasContent() || cell.Text == "" && count > maxStyledRepeat {
				cell = nil
			}
			cs := repeat(el, tableNs, "number-columns-spanned")
			rs := repeat(el, tableNs, "number-rows-spanned")
			if cell != nil || cs > 1 || rs > 1 {
				runs = append(runs, cellRun{c, count, cell, cs, rs})
			}
		}
		c += count
	}
	return runs
}

func (r *odsReader) cell(el *xdom.Node, styleName string) *Cell {
	info := r.cellStyleOf(styleName)
	typ := el.AN(officeNs, "value-type")
	var value *float64
	switch typ {
	case "float", "percentage", "currency":
		if v, ok := parseFloat(el.AN(officeNs, "value")); ok {
			value = &v
		}
	case "date":
		if d, ok := parseODFDate(el.AN(officeNs, "date-value")); ok {
			v := ToSerial(d)
			value = &v
		}
	case "time":
		if d, ok := parseDuration(el.AN(officeNs, "time-value")); ok {
			value = &d
		}
	case "boolean":
		b := el.AN(officeNs, "boolean-value")
		v := 0.0
		if b == "true" || b == "1" {
			v = 1
		}
		value = &v
	}
	text, hasText := paragraphs(el)
	if typ == "boolean" {
		if !hasText {
			text = pick(*value == 1, "TRUE", "FALSE")
		}
		return &Cell{Text: text, Style: info.style, Value: value, IsBoolean: true}
	}
	if value == nil {
		if !hasText {
			text = el.AN(officeNs, "string-value")
		}
		return TextCell(text, info.style)
	}
	v := *value
	format := r.formatOf(info.dataStyle)
	if format == "" {
		switch typ {
		case "date":
			format = pick(v == math.Floor(v), "yyyy-mm-dd", "yyyy-mm-dd hh:mm:ss")
		case "time":
			format = "hh:mm:ss"
		case "percentage":
			format = "0%"
		}
	}
	if IsGeneral(format) {
		format = ""
	}
	// text:p is what Calc showed when it saved; files from other programs may leave it out.
	if !hasText {
		text = r.formatter.Format(v, format)
	}
	return &Cell{Text: text, Style: info.style, IsNumber: true, Value: value, NumberFormat: format}
}

func parseODFDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, l := range []string{"2006-01-02T15:04:05.999999999Z07:00", "2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.Parse(l, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

var isoDuration = regexp.MustCompile(`^(-)?P(?:(\d+(?:\.\d+)?)D)?(?:T(?:(\d+(?:\.\d+)?)H)?(?:(\d+(?:\.\d+)?)M)?(?:(\d+(?:\.\d+)?)S)?)?$`)

// parseDuration reads an xs:duration ("PT12H30M00S") as days.
func parseDuration(s string) (float64, bool) {
	m := isoDuration.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, false
	}
	f := func(x string) float64 { v, _ := strconv.ParseFloat(x, 64); return v }
	days := f(m[2]) + f(m[3])/24 + f(m[4])/1440 + f(m[5])/86400
	if m[1] == "-" {
		days = -days
	}
	return days, true
}

// paragraphs is the text of a cell, paragraphs joined by line breaks.
func paragraphs(cell *xdom.Node) (string, bool) {
	var ps []*xdom.Node
	for _, e := range cell.Elements() {
		if e.Is(textNs, "p") || e.Is(textNs, "h") {
			ps = append(ps, e)
		}
	}
	if len(ps) == 0 {
		return "", false
	}
	var sb bytes.Buffer
	for i, p := range ps {
		if i > 0 {
			sb.WriteByte('\n')
		}
		inline(p, &sb)
	}
	return sb.String(), true
}

func inline(el *xdom.Node, sb *bytes.Buffer) {
	for _, n := range el.Children {
		if n.IsText() {
			// ODF collapses white space; extra spaces are text:s elements.
			for _, ch := range n.Text {
				if ch == '\t' || ch == '\r' || ch == '\n' {
					ch = ' '
				}
				b := sb.Bytes()
				if ch != ' ' || len(b) == 0 || b[len(b)-1] != ' ' {
					sb.WriteRune(ch)
				}
			}
			continue
		}
		switch {
		case n.Is(textNs, "s"):
			sb.WriteString(strings.Repeat(" ", repeat(n, textNs, "c")))
		case n.Is(textNs, "tab"):
			sb.WriteByte('\t')
		case n.Is(textNs, "line-break"):
			sb.WriteByte('\n')
		case n.Is(officeNs, "annotation"), n.Is(textNs, "note"):
		default:
			inline(n, sb)
		}
	}
}
