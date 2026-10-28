package spreadsheet

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/office/opc"
	"github.com/dseif0x/filee-go/internal/xdom"
)

// Spreadsheet print metadata and an export plan for the interactive
// XLSX → PDF studio. The source workbook is never edited: CreatePrintPackage
// applies one plan to a temporary copy for LibreOffice to render.

type PageOrientation int

const (
	OrientationSource PageOrientation = iota
	OrientationPortrait
	OrientationLandscape
)

type ScaleMode int

const (
	ScaleSource ScaleMode = iota
	ScalePercent
	ScaleFitToPages
)

type PageOrder int

const (
	OrderSource PageOrder = iota
	OrderDownThenOver
	OrderOverThenDown
)

// Paper is a standard paper size understood by both SpreadsheetML and LibreOffice.
type Paper struct {
	ID, Name          string
	OoxmlCode         int
	WidthMm, HeightMm float64
}

var (
	PaperSource  = Paper{"source", "From workbook", 0, 0, 0}
	PaperA5      = Paper{"a5", "A5", 11, 148, 210}
	PaperA4      = Paper{"a4", "A4", 9, 210, 297}
	PaperA3      = Paper{"a3", "A3", 8, 297, 420}
	PaperLetter  = Paper{"letter", "Letter", 1, 215.9, 279.4}
	PaperLegal   = Paper{"legal", "Legal", 5, 215.9, 355.6}
	PaperTabloid = Paper{"tabloid", "Tabloid", 3, 279.4, 431.8}
	Papers       = []Paper{PaperSource, PaperA5, PaperA4, PaperA3, PaperLetter, PaperLegal, PaperTabloid}
)

// PaperFromCode is the paper of an OOXML paperSize code (Source when unknown).
func PaperFromCode(code *int) Paper {
	if code != nil {
		for _, p := range Papers {
			if p.OoxmlCode == *code {
				return p
			}
		}
	}
	return PaperSource
}

// SourcePageSetup is the page settings found in the source sheet.
type SourcePageSetup struct {
	Paper                                                    Paper
	Orientation                                              PageOrientation
	ScalePercent, FitWidth, FitHeight                        *int
	Order                                                    PageOrder
	MarginLeftMm, MarginRightMm, MarginTopMm, MarginBottomMm float64
}

// SheetInfo is one worksheet as the studio sees it.
type SheetInfo struct {
	Index                                            int
	Name, PartPath                                   string
	IsHidden                                         bool
	UsedRange                                        string
	PrintArea, RepeatRows                            string // "" = none
	UsedRows, UsedColumns, HiddenRows, HiddenColumns int
	PageSetup                                        SourcePageSetup
}

func (s *SheetInfo) InitialRange() string {
	if s.PrintArea != "" {
		return s.PrintArea
	}
	return s.UsedRange
}

func (s *SheetInfo) HasSuspiciousScale() bool {
	return s.PageSetup.ScalePercent != nil && *s.PageSetup.ScalePercent > 0 && *s.PageSetup.ScalePercent <= 25
}

func (s *SheetInfo) HasWideUnboundedRange() bool { return s.PrintArea == "" && s.UsedColumns > 60 }

// WorkbookInfo is the workbook metadata the print studio needs.
type WorkbookInfo struct {
	Path, WorkbookPartPath string
	Sheets                 []SheetInfo
}

// PrintRegion is the settings for one independently rendered worksheet region.
type PrintRegion struct {
	Sheet                                                    SheetInfo
	Range                                                    string
	Paper                                                    Paper
	Orientation                                              PageOrientation
	ScaleMode                                                ScaleMode
	ScalePercent, FitWidth, FitHeight                        int
	Order                                                    PageOrder
	MarginLeftMm, MarginRightMm, MarginTopMm, MarginBottomMm float64
	IncludeHidden, KeepManualBreaks, WritePrintArea          bool
}

func intOr(p *int, d int) int {
	if p == nil {
		return d
	}
	return *p
}

// RegionFromSource is the region the sheet's own settings describe.
func RegionFromSource(s SheetInfo) PrintRegion {
	ps := s.PageSetup
	return PrintRegion{Sheet: s, Range: s.InitialRange(), Paper: ps.Paper, Orientation: ps.Orientation, ScaleMode: ScaleSource,
		ScalePercent: intOr(ps.ScalePercent, 100), FitWidth: max(1, intOr(ps.FitWidth, 1)), FitHeight: max(0, intOr(ps.FitHeight, 0)),
		Order: ps.Order, MarginLeftMm: ps.MarginLeftMm, MarginRightMm: ps.MarginRightMm, MarginTopMm: ps.MarginTopMm, MarginBottomMm: ps.MarginBottomMm,
		KeepManualBreaks: true, WritePrintArea: s.PrintArea != ""}
}

var (
	cellPattern     = regexp.MustCompile(`^([A-Za-z]+)(\d+)$`)
	rowRangePattern = regexp.MustCompile(`^\d+:\d+$`)
	columnPattern   = regexp.MustCompile(`^[A-Za-z]{1,3}$`)
	rowPattern      = regexp.MustCompile(`^\d{1,7}$`)
)

// InspectPrint reads worksheet names, content bounds and print settings.
func InspectPrint(path string) (*WorkbookInfo, error) {
	pkg, err := opc.Open(path)
	if err != nil {
		return nil, err
	}
	defer pkg.Close()
	wbPath := pkg.MainPartPath()
	if wbPath == "" {
		wbPath = "xl/workbook.xml"
	}
	wb := pkg.Part(wbPath)
	if wb == nil {
		return nil, errors.New("the file is not an Excel workbook (no xl/workbook.xml)")
	}
	names := wb.Child(sNs, "definedNames").All(sNs, "definedName")
	info := &WorkbookInfo{Path: path, WorkbookPartPath: wbPath}
	for index, sheet := range wb.Child(sNs, "sheets").All(sNs, "sheet") {
		target := pkg.Target(wbPath, sheet.AN(rNs, "id"))
		entry := pkg.Entry(target)
		if target == "" || entry == nil {
			continue
		}
		ws := readWorksheetSummary(entry)
		name := sheet.A("name")
		if name == "" {
			name = fmt.Sprintf("Sheet %d", index+1)
		}
		o := OrientationSource
		switch ws.orientation {
		case "portrait":
			o = OrientationPortrait
		case "landscape":
			o = OrientationLandscape
		}
		order := OrderSource
		switch ws.pageOrder {
		case "downThenOver":
			order = OrderDownThenOver
		case "overThenDown":
			order = OrderOverThenDown
		}
		st := sheet.A("state")
		info.Sheets = append(info.Sheets, SheetInfo{
			Index: index, Name: name, PartPath: target, IsHidden: st == "hidden" || st == "veryHidden", UsedRange: ws.reference,
			PrintArea: definedRange(names, index, "_xlnm.Print_Area", false), RepeatRows: definedRange(names, index, "_xlnm.Print_Titles", true),
			UsedRows: ws.rows, UsedColumns: ws.columns, HiddenRows: ws.hiddenRows, HiddenColumns: ws.hiddenColumns,
			PageSetup: SourcePageSetup{PaperFromCode(ws.paperSize), o, ws.scale, ws.fitWidth, ws.fitHeight, order,
				ws.marginLeft * 25.4, ws.marginRight * 25.4, ws.marginTop * 25.4, ws.marginBottom * 25.4},
		})
	}
	return info, nil
}

// IsValidPrintRange reports whether a comma-separated A1 range can safely become an Excel print area.
func IsValidPrintRange(r string) bool {
	if strings.TrimSpace(r) == "" {
		return false
	}
	for _, part := range SplitAreas(r) {
		v := strings.ReplaceAll(strings.TrimSpace(part), "$", "")
		cells := strings.Split(v, ":")
		ok := true
		if len(cells) == 1 || len(cells) == 2 {
			for _, c := range cells {
				if _, _, good := tryCell(c); !good {
					ok = false
				}
			}
		} else {
			ok = false
		}
		if !ok && len(cells) == 2 {
			ok = tryColumn(cells[0]) && tryColumn(cells[1])
			if !ok {
				ok = true
				for _, c := range cells {
					n, err := strconv.Atoi(c)
					if !rowPattern.MatchString(c) || err != nil || n < 1 || n > 1_048_576 {
						ok = false
					}
				}
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func definedRange(names []*xdom.Node, sheet int, kind string, rowsOnly bool) string {
	value := ""
	for _, n := range names {
		if n.A("name") == kind && n.A("localSheetId") == strconv.Itoa(sheet) {
			value = n.Value()
			break
		}
	}
	if strings.TrimSpace(value) == "" {
		return ""
	}
	var ranges []string
	for _, part := range SplitAreas(value) {
		r := strings.TrimSpace(strings.ReplaceAll(part[strings.LastIndexByte(part, '!')+1:], "$", ""))
		if rowsOnly && !rowRangePattern.MatchString(r) {
			continue
		}
		ranges = append(ranges, r)
	}
	return strings.Join(ranges, ",")
}

type worksheetSummary struct {
	reference                                        string
	rows, columns, hiddenRows, hiddenColumns         int
	paperSize, scale, fitWidth, fitHeight            *int
	orientation, pageOrder                           string
	marginLeft, marginRight, marginTop, marginBottom float64
}

func optInt(v string) *int {
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil
	}
	return &n
}

func readWorksheetSummary(entry *zip.File) worksheetSummary {
	s := worksheetSummary{marginLeft: 0.7, marginRight: 0.7, marginTop: 0.75, marginBottom: 0.75}
	top, left, bottom, right := 1<<30, 1<<30, -1, -1
	var merges []string
	rc, err := entry.Open()
	if err != nil {
		s.reference = "A1"
		return s
	}
	defer rc.Close()
	d := xml.NewDecoder(rc)
	attrOf := func(t xml.StartElement, n string) string {
		for _, a := range t.Attr {
			if a.Name.Local == n && a.Name.Space == "" {
				return a.Value
			}
		}
		return ""
	}
	inCell, cellHas, cellRef := false, false, ""
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != sNs {
				continue
			}
			switch t.Name.Local {
			case "row":
				if onAttr(attrOf(t, "hidden")) {
					s.hiddenRows++
				}
			case "col":
				if onAttr(attrOf(t, "hidden")) {
					lo := max(1, intOr(optInt(attrOf(t, "min")), 1))
					hi := min(16384, intOr(optInt(attrOf(t, "max")), lo))
					s.hiddenColumns += max(0, hi-lo+1)
				}
			case "c":
				inCell, cellHas, cellRef = true, false, attrOf(t, "r")
			case "v", "is", "f":
				if inCell {
					cellHas = true
				}
			case "mergeCell":
				if r := attrOf(t, "ref"); r != "" {
					merges = append(merges, r)
				}
			case "pageSetup":
				s.paperSize, s.scale = optInt(attrOf(t, "paperSize")), optInt(attrOf(t, "scale"))
				s.fitWidth, s.fitHeight = optInt(attrOf(t, "fitToWidth")), optInt(attrOf(t, "fitToHeight"))
				s.orientation, s.pageOrder = attrOf(t, "orientation"), attrOf(t, "pageOrder")
			case "pageMargins":
				for _, m := range []struct {
					n string
					p *float64
				}{{"left", &s.marginLeft}, {"right", &s.marginRight}, {"top", &s.marginTop}, {"bottom", &s.marginBottom}} {
					if v, ok := parseFloat(attrOf(t, m.n)); ok {
						*m.p = v
					}
				}
			}
		case xml.EndElement:
			if t.Name.Space == sNs && t.Name.Local == "c" {
				// A styled empty cell can push Excel's dimension to XFD1048576.
				if row, col, ok := tryCell(cellRef); cellHas && ok {
					top, left, bottom, right = min(top, row), min(left, col), max(bottom, row), max(right, col)
				}
				inCell = false
			}
		case xml.Directive:
			s.reference = "A1"
			return s
		}
	}
	for _, m := range merges {
		parts := strings.Split(m, ":")
		if len(parts) != 2 {
			continue
		}
		r1, c1, ok1 := tryCell(parts[0])
		r2, c2, ok2 := tryCell(parts[1])
		if ok1 && ok2 && bottom >= 0 && r1 >= top && c1 >= left && r1 <= bottom && c1 <= right {
			bottom, right = max(bottom, r2), max(right, c2)
		}
	}
	if bottom < 0 {
		s.reference = "A1"
		return s
	}
	s.reference = ColumnName(left) + strconv.Itoa(top+1) + ":" + ColumnName(right) + strconv.Itoa(bottom+1)
	s.rows, s.columns = bottom-top+1, right-left+1
	return s
}

// SplitAreas splits at commas outside quoted sheet names.
func SplitAreas(v string) []string {
	var out []string
	start, quoted := 0, false
	for i := 0; i < len(v); i++ {
		switch {
		case v[i] == '\'' && i+1 < len(v) && v[i+1] == '\'':
			i++
		case v[i] == '\'':
			quoted = !quoted
		case v[i] == ',' && !quoted:
			out = append(out, v[start:i])
			start = i + 1
		}
	}
	return append(out, v[start:])
}

func tryCell(ref string) (int, int, bool) {
	m := cellPattern.FindStringSubmatch(strings.ReplaceAll(strings.TrimSpace(ref), "$", ""))
	if m == nil {
		return -1, -1, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil || n < 1 || n > 1_048_576 {
		return -1, -1, false
	}
	col := 0
	for _, ch := range strings.ToUpper(m[1]) {
		col = col*26 + int(ch-'A'+1)
		if col > 16_384 {
			return -1, -1, false
		}
	}
	return n - 1, col - 1, true
}

func tryColumn(v string) bool {
	if !columnPattern.MatchString(v) {
		return false
	}
	col := 0
	for _, ch := range strings.ToUpper(v) {
		col = col*26 + int(ch-'A'+1)
	}
	return col <= 16_384
}

// CreatePrintPackage writes a temporary XLSX with one printable sheet and
// one region, preserving every other package part.
func CreatePrintPackage(source, destination string, wb *WorkbookInfo, region PrintRegion) error {
	if !IsValidPrintRange(region.Range) {
		return errors.New("the print area is not a valid A1 cell range")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	in, err := zip.OpenReader(source)
	if err != nil {
		return err
	}
	defer in.Close()
	f, err := os.Create(destination)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	fail := func(err error) error {
		zw.Close()
		f.Close()
		os.Remove(destination)
		return err
	}
	isSheet := func(name string) bool {
		for _, s := range wb.Sheets {
			if strings.EqualFold(name, s.PartPath) {
				return true
			}
		}
		return false
	}
	for _, e := range in.File {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.Name, Method: zip.Deflate, Modified: e.Modified})
		if err != nil {
			return fail(err)
		}
		switch {
		case strings.EqualFold(e.Name, wb.WorkbookPartPath):
			err = rewritePart(e, w, func(root *xdom.Node) error { return rewriteWorkbook(root, region) })
		case strings.EqualFold(e.Name, region.Sheet.PartPath):
			err = rewritePart(e, w, func(root *xdom.Node) error { rewriteSheet(root, region); return nil })
		case isSheet(e.Name):
			err = rewritePart(e, w, func(root *xdom.Node) error { setSheetSelected(root, false); return nil })
		default:
			var rc io.ReadCloser
			if rc, err = e.Open(); err == nil {
				_, err = io.Copy(w, rc)
				rc.Close()
			}
		}
		if err != nil {
			return fail(err)
		}
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func rewritePart(e *zip.File, w io.Writer, change func(*xdom.Node) error) error {
	rc, err := e.Open()
	if err != nil {
		return err
	}
	root, err := xdom.Parse(rc, opc.MaxPartBytes)
	rc.Close()
	if err != nil {
		return err
	}
	if err := change(root); err != nil {
		return err
	}
	var b bytes.Buffer
	if err := xdom.Write(&b, root, map[string]string{sNs: ""}); err != nil {
		return err
	}
	_, err = w.Write(b.Bytes())
	return err
}

func rewriteWorkbook(root *xdom.Node, region PrintRegion) error {
	sheetsEl := root.Child(sNs, "sheets")
	sheets := sheetsEl.All(sNs, "sheet")
	if region.Sheet.Index < 0 || region.Sheet.Index >= len(sheets) {
		return errors.New("the selected worksheet is no longer in the workbook")
	}
	selected := sheets[region.Sheet.Index]
	selected.RemoveAttr("", "state")
	for _, s := range sheets {
		if s != selected {
			s.SetAttr("", "state", "hidden")
		}
	}
	views := root.Child(sNs, "bookViews")
	if views == nil {
		views = xdom.NewElement(sNs, "bookViews")
		if sheetsEl != nil {
			sheetsEl.AddBefore(views)
		} else {
			root.Add(views)
		}
	}
	view := views.Child(sNs, "workbookView")
	if view == nil {
		view = xdom.NewElement(sNs, "workbookView")
		views.Add(view)
	}
	idx := strconv.Itoa(region.Sheet.Index)
	view.SetAttr("", "activeTab", idx)
	view.SetAttr("", "firstSheet", idx)
	defined := root.Child(sNs, "definedNames")
	for _, n := range defined.All(sNs, "definedName") {
		if n.A("name") == "_xlnm.Print_Area" && n.A("localSheetId") == idx {
			n.Remove()
		}
	}
	if region.WritePrintArea {
		if defined == nil {
			defined = xdom.NewElement(sNs, "definedNames")
			if sheetsEl != nil {
				sheetsEl.AddAfter(defined)
			} else {
				root.Add(defined)
			}
		}
		escaped := strings.ReplaceAll(region.Sheet.Name, "'", "''")
		var parts []string
		for _, p := range SplitAreas(region.Range) {
			parts = append(parts, "'"+escaped+"'!"+absoluteRange(p))
		}
		n := xdom.NewElement(sNs, "definedName")
		n.SetAttr("", "name", "_xlnm.Print_Area")
		n.SetAttr("", "localSheetId", idx)
		n.SetText(strings.Join(parts, ","))
		defined.Add(n)
	}
	return nil
}

func setSheetSelected(root *xdom.Node, selected bool) {
	for _, v := range root.Child(sNs, "sheetViews").All(sNs, "sheetView") {
		if selected {
			v.SetAttr("", "tabSelected", "1")
		} else {
			v.RemoveAttr("", "tabSelected")
		}
	}
}

func rewriteSheet(root *xdom.Node, region PrintRegion) {
	setSheetSelected(root, true)
	if region.IncludeHidden {
		for _, r := range root.Child(sNs, "sheetData").All(sNs, "row") {
			r.RemoveAttr("", "hidden")
		}
		for _, c := range root.Child(sNs, "cols").All(sNs, "col") {
			c.RemoveAttr("", "hidden")
		}
	}
	if !region.KeepManualBreaks {
		root.Child(sNs, "rowBreaks").Remove()
		root.Child(sNs, "colBreaks").Remove()
	}
	margins := ensure(root, "pageMargins")
	setNumber := func(n *xdom.Node, name string, v float64) { n.SetAttr("", name, fmtG(v, 4)) }
	setNumber(margins, "left", region.MarginLeftMm/25.4)
	setNumber(margins, "right", region.MarginRightMm/25.4)
	setNumber(margins, "top", region.MarginTopMm/25.4)
	setNumber(margins, "bottom", region.MarginBottomMm/25.4)
	if _, ok := margins.Attr("", "header"); !ok {
		margins.SetAttr("", "header", "0.3")
	}
	if _, ok := margins.Attr("", "footer"); !ok {
		margins.SetAttr("", "footer", "0.3")
	}
	setup := ensure(root, "pageSetup")
	// A cached printerSettings relationship can carry the old paper and orientation into LibreOffice.
	if region.Paper != PaperSource || region.Orientation != OrientationSource || region.ScaleMode != ScaleSource || region.Order != OrderSource {
		var kept []xml.Attr
		for _, a := range setup.Attrs {
			if a.Name.Local != "id" {
				kept = append(kept, a)
			}
		}
		setup.Attrs = kept
	}
	if region.Paper != PaperSource {
		setup.SetAttr("", "paperSize", strconv.Itoa(region.Paper.OoxmlCode))
	}
	switch region.Orientation {
	case OrientationPortrait:
		setup.SetAttr("", "orientation", "portrait")
	case OrientationLandscape:
		setup.SetAttr("", "orientation", "landscape")
	}
	switch region.Order {
	case OrderDownThenOver:
		setup.SetAttr("", "pageOrder", "downThenOver")
	case OrderOverThenDown:
		setup.SetAttr("", "pageOrder", "overThenDown")
	}
	props := root.Child(sNs, "sheetPr")
	if props == nil {
		props = xdom.NewElement(sNs, "sheetPr")
		root.Insert(0, props)
	}
	pageProps := props.Child(sNs, "pageSetUpPr")
	if pageProps == nil {
		pageProps = xdom.NewElement(sNs, "pageSetUpPr")
		props.Add(pageProps)
	}
	switch region.ScaleMode {
	case ScalePercent:
		setup.SetAttr("", "scale", strconv.Itoa(max(10, min(400, region.ScalePercent))))
		setup.RemoveAttr("", "fitToWidth")
		setup.RemoveAttr("", "fitToHeight")
		pageProps.SetAttr("", "fitToPage", "0")
	case ScaleFitToPages:
		setup.RemoveAttr("", "scale")
		setup.SetAttr("", "fitToWidth", strconv.Itoa(max(1, min(100, region.FitWidth))))
		setup.SetAttr("", "fitToHeight", strconv.Itoa(max(0, min(100, region.FitHeight))))
		pageProps.SetAttr("", "fitToPage", "1")
	}
}

// ensure finds or adds pageMargins / pageSetup in schema order.
func ensure(root *xdom.Node, local string) *xdom.Node {
	if e := root.Child(sNs, local); e != nil {
		return e
	}
	el := xdom.NewElement(sNs, local)
	if local == "pageMargins" {
		if setup := root.Child(sNs, "pageSetup"); setup != nil {
			setup.AddBefore(el)
			return el
		}
	}
	after := "pageMargins"
	if local == "pageMargins" {
		after = "printOptions"
	}
	if prev := root.Child(sNs, after); prev != nil {
		prev.AddAfter(el)
	} else {
		root.Add(el)
	}
	return el
}

func absoluteRange(r string) string {
	parts := strings.Split(strings.TrimSpace(strings.ReplaceAll(r, "$", "")), ":")
	for i, v := range parts {
		switch {
		case columnPattern.MatchString(v):
			parts[i] = "$" + strings.ToUpper(v)
		case rowPattern.MatchString(v):
			parts[i] = "$" + v
		default:
			if s := strings.IndexAny(v, "0123456789"); s > 0 {
				parts[i] = "$" + strings.ToUpper(v[:s]) + "$" + v[s:]
			}
		}
	}
	return strings.Join(parts, ":")
}
