package webui

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/xuri/excelize/v2"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/presets"
)

// The spreadsheet print studio: page setup per sheet (or region) of an XLSX,
// an exact preview rendered by LibreOffice from a temporary copy, and one PDF
// of every included region. The user's workbook is never written.

// SheetPaper is a paper size both SpreadsheetML and LibreOffice know.
type SheetPaper struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Code     int     `json:"code"`
	WidthMm  float64 `json:"widthMm"`
	HeightMm float64 `json:"heightMm"`
}

// SheetPapers in the order the studio lists them; "source" keeps the workbook's.
var SheetPapers = []SheetPaper{
	{"source", "From workbook", 0, 0, 0}, {"a5", "A5", 11, 148, 210}, {"a4", "A4", 9, 210, 297}, {"a3", "A3", 8, 297, 420},
	{"letter", "Letter", 1, 215.9, 279.4}, {"legal", "Legal", 5, 215.9, 355.6}, {"tabloid", "Tabloid", 3, 279.4, 431.8},
}

func paperByCode(code int) string {
	for _, p := range SheetPapers {
		if p.Code == code && code != 0 {
			return p.ID
		}
	}
	return "source"
}

func paperByID(id string) SheetPaper {
	for _, p := range SheetPapers {
		if p.ID == id {
			return p
		}
	}
	return SheetPapers[0]
}

// SheetPageSetup is a sheet's own page settings.
type SheetPageSetup struct {
	Paper        string  `json:"paper"`
	Orientation  string  `json:"orientation"` // source, portrait, landscape
	ScalePercent int     `json:"scalePercent,omitempty"`
	FitWidth     int     `json:"fitWidth,omitempty"`
	FitHeight    int     `json:"fitHeight,omitempty"`
	Order        string  `json:"order"` // source, downThenOver, overThenDown
	MarginLeft   float64 `json:"marginLeftMm"`
	MarginRight  float64 `json:"marginRightMm"`
	MarginTop    float64 `json:"marginTopMm"`
	MarginBottom float64 `json:"marginBottomMm"`
}

// SheetInfo is one worksheet as the studio sees it.
type SheetInfo struct {
	Index         int            `json:"index"`
	Name          string         `json:"name"`
	Part          string         `json:"part"`
	Hidden        bool           `json:"hidden"`
	UsedRange     string         `json:"usedRange"`
	PrintArea     string         `json:"printArea,omitempty"`
	RepeatRows    string         `json:"repeatRows,omitempty"`
	UsedRows      int            `json:"usedRows"`
	UsedColumns   int            `json:"usedColumns"`
	HiddenRows    int            `json:"hiddenRows"`
	HiddenColumns int            `json:"hiddenColumns"`
	PageSetup     SheetPageSetup `json:"pageSetup"`
	// SuspiciousScale: the workbook asks for 25 % or less; WideUnbounded: over 60 columns without a print area.
	SuspiciousScale bool `json:"suspiciousScale"`
	WideUnbounded   bool `json:"wideUnbounded"`
}

// SheetRegion is the settings of one rendered region.
type SheetRegion struct {
	Sheet          int     `json:"sheet"`
	Range          string  `json:"range"`
	Paper          string  `json:"paper"`
	Orientation    string  `json:"orientation"`
	ScaleMode      string  `json:"scaleMode"` // source, percent, fit
	ScalePercent   int     `json:"scalePercent"`
	FitWidth       int     `json:"fitWidth"`
	FitHeight      int     `json:"fitHeight"`
	Order          string  `json:"order"`
	MarginLeft     float64 `json:"marginLeftMm"`
	MarginRight    float64 `json:"marginRightMm"`
	MarginTop      float64 `json:"marginTopMm"`
	MarginBottom   float64 `json:"marginBottomMm"`
	IncludeHidden  bool    `json:"includeHidden"`
	KeepBreaks     bool    `json:"keepBreaks"`
	WritePrintArea bool    `json:"writePrintArea"`
}

// RegionFromSource is a region as the sheet itself prints.
func RegionFromSource(s SheetInfo) SheetRegion {
	r := s.UsedRange
	if s.PrintArea != "" {
		r = s.PrintArea
	}
	ps := s.PageSetup
	scale := ps.ScalePercent
	if scale == 0 {
		scale = 100
	}
	return SheetRegion{
		Sheet: s.Index, Range: r, Paper: ps.Paper, Orientation: ps.Orientation, ScaleMode: "source", ScalePercent: scale,
		FitWidth: max(1, ps.FitWidth), FitHeight: max(0, ps.FitHeight), Order: ps.Order,
		MarginLeft: ps.MarginLeft, MarginRight: ps.MarginRight, MarginTop: ps.MarginTop, MarginBottom: ps.MarginBottom,
		KeepBreaks: true, WritePrintArea: s.PrintArea != "",
	}
}

// ---- reading ----

var (
	cellRef  = regexp.MustCompile(`^([A-Za-z]+)(\d+)$`)
	rowRange = regexp.MustCompile(`^\d+:\d+$`)
	colRef   = regexp.MustCompile(`^[A-Za-z]{1,3}$`)
	rowRef   = regexp.MustCompile(`^\d{1,7}$`)
)

func parseCell(ref string) (row, col int, ok bool) {
	m := cellRef.FindStringSubmatch(strings.ReplaceAll(ref, "$", ""))
	if m == nil {
		return -1, -1, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil || n < 1 || n > 1_048_576 {
		return -1, -1, false
	}
	c := 0
	for _, ch := range strings.ToUpper(m[1]) {
		c = c*26 + int(ch-'A'+1)
		if c > 16384 {
			return -1, -1, false
		}
	}
	return n - 1, c - 1, true
}

func cellName(col, row int) string {
	letters := ""
	for v := col + 1; v > 0; v = (v - 1) / 26 {
		letters = string(rune('A'+(v-1)%26)) + letters
	}
	return letters + strconv.Itoa(row+1)
}

// splitAreas splits "A1:B2,'x,y'!C3" at commas outside quotes.
func splitAreas(v string) []string {
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

// ValidSheetRange: a comma list of A1 ranges a print area can be.
func ValidSheetRange(r string) bool {
	if strings.TrimSpace(r) == "" {
		return false
	}
	for _, part := range splitAreas(r) {
		v := strings.ReplaceAll(strings.TrimSpace(part), "$", "")
		cells := strings.Split(v, ":")
		ok := false
		if len(cells) == 1 || len(cells) == 2 {
			ok = true
			for _, c := range cells {
				if _, _, good := parseCell(c); !good {
					ok = false
				}
			}
		}
		if !ok && len(cells) == 2 && colRef.MatchString(cells[0]) && colRef.MatchString(cells[1]) {
			ok = true
		}
		if !ok && len(cells) == 2 && rowRef.MatchString(cells[0]) && rowRef.MatchString(cells[1]) {
			a, _ := strconv.Atoi(cells[0])
			b, _ := strconv.Atoi(cells[1])
			ok = a >= 1 && a <= 1_048_576 && b >= 1 && b <= 1_048_576
		}
		if !ok {
			return false
		}
	}
	return true
}

type xmlAttrs []xml.Attr

func (a xmlAttrs) get(name string) string {
	for _, x := range a {
		if x.Name.Local == name && (x.Name.Space == "" || !strings.Contains(x.Name.Space, "relationships") || name == "id") {
			return x.Value
		}
	}
	return ""
}

func (a xmlAttrs) rel(name string) string {
	for _, x := range a {
		if x.Name.Local == name && strings.Contains(x.Name.Space, "relationships") {
			return x.Value
		}
	}
	return ""
}

// InspectWorkbook reads sheet names, content bounds and print settings
// without LibreOffice.
func InspectWorkbook(file string) ([]SheetInfo, error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return nil, fmt.Errorf("the file is not an Excel workbook: %w", err)
	}
	defer zr.Close()
	entries := map[string]*zip.File{}
	for _, f := range zr.File {
		entries[strings.ToLower(f.Name)] = f
	}
	wbPath := "xl/workbook.xml"
	wb := entries[wbPath]
	if wb == nil {
		return nil, errors.New("the file is not an Excel workbook (no xl/workbook.xml)")
	}
	rels := readRels(entries, wbPath)
	type sheetRef struct {
		name, rid, state string
	}
	var sheets []sheetRef
	type defined struct{ name, local, value string }
	var names []defined
	if err := walkXML(wb, func(d *xml.Decoder, se xml.StartElement) {
		a := xmlAttrs(se.Attr)
		switch se.Name.Local {
		case "sheet":
			sheets = append(sheets, sheetRef{a.get("name"), a.rel("id"), a.get("state")})
		case "definedName":
			var v string
			_ = d.DecodeElement(&v, &se)
			names = append(names, defined{a.get("name"), a.get("localSheetId"), v})
		}
	}); err != nil {
		return nil, err
	}
	definedRange := func(index int, kind string, rowsOnly bool) string {
		for _, n := range names {
			if n.name != kind || n.local != strconv.Itoa(index) || strings.TrimSpace(n.value) == "" {
				continue
			}
			var parts []string
			for _, p := range splitAreas(n.value) {
				p = strings.TrimSpace(strings.ReplaceAll(p[strings.LastIndex(p, "!")+1:], "$", ""))
				if !rowsOnly || rowRange.MatchString(p) {
					parts = append(parts, p)
				}
			}
			return strings.Join(parts, ",")
		}
		return ""
	}
	var out []SheetInfo
	for index, s := range sheets {
		target := rels[s.rid]
		entry := entries[strings.ToLower(target)]
		if entry == nil {
			continue
		}
		info, err := readWorksheet(entry)
		if err != nil {
			return nil, err
		}
		name := s.name
		if name == "" {
			name = fmt.Sprintf("Sheet %d", index+1)
		}
		info.Index, info.Name, info.Part = index, name, target
		info.Hidden = s.state == "hidden" || s.state == "veryHidden"
		info.PrintArea = definedRange(index, "_xlnm.Print_Area", false)
		info.RepeatRows = definedRange(index, "_xlnm.Print_Titles", true)
		info.SuspiciousScale = info.PageSetup.ScalePercent > 0 && info.PageSetup.ScalePercent <= 25
		info.WideUnbounded = info.PrintArea == "" && info.UsedColumns > 60
		out = append(out, info)
	}
	return out, nil
}

func readRels(entries map[string]*zip.File, part string) map[string]string {
	dir, base := path.Split(part)
	f := entries[strings.ToLower(dir+"_rels/"+base+".rels")]
	out := map[string]string{}
	if f == nil {
		return out
	}
	_ = walkXML(f, func(d *xml.Decoder, se xml.StartElement) {
		if se.Name.Local != "Relationship" {
			return
		}
		a := xmlAttrs(se.Attr)
		t := a.get("Target")
		if strings.HasPrefix(t, "/") {
			t = strings.TrimPrefix(t, "/")
		} else {
			t = path.Clean(dir + t)
		}
		out[a.get("Id")] = t
	})
	return out
}

func walkXML(f *zip.File, fn func(*xml.Decoder, xml.StartElement)) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	d := xml.NewDecoder(rc)
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if se, ok := tok.(xml.StartElement); ok {
			fn(d, se)
		}
	}
}

func readWorksheet(f *zip.File) (SheetInfo, error) {
	top, left, bottom, right := 1<<30, 1<<30, -1, -1
	info := SheetInfo{PageSetup: SheetPageSetup{Paper: "source", Orientation: "source", Order: "source",
		MarginLeft: 0.7 * 25.4, MarginRight: 0.7 * 25.4, MarginTop: 0.75 * 25.4, MarginBottom: 0.75 * 25.4}}
	var merges []string
	on := func(v string) bool { return v == "1" || v == "true" }
	num := func(v string) (float64, bool) {
		x, err := strconv.ParseFloat(v, 64)
		return x, err == nil
	}
	atoi := func(v string) int { n, _ := strconv.Atoi(v); return n }
	err := walkXML(f, func(d *xml.Decoder, se xml.StartElement) {
		a := xmlAttrs(se.Attr)
		switch se.Name.Local {
		case "row":
			if on(a.get("hidden")) {
				info.HiddenRows++
			}
		case "col":
			if on(a.get("hidden")) {
				lo := max(1, atoi(a.get("min")))
				hi := atoi(a.get("max"))
				if hi == 0 {
					hi = lo
				}
				info.HiddenColumns += max(0, min(16384, hi)-lo+1)
			}
		case "c":
			var cell struct {
				V  *string   `xml:"v"`
				F  *string   `xml:"f"`
				IS *struct{} `xml:"is"`
			}
			_ = d.DecodeElement(&cell, &se)
			// A styled empty cell can push Excel's dimension to XFD1048576.
			if cell.V != nil || cell.F != nil || cell.IS != nil {
				if r, c, ok := parseCell(a.get("r")); ok {
					top, left, bottom, right = min(top, r), min(left, c), max(bottom, r), max(right, c)
				}
			}
		case "mergeCell":
			if v := a.get("ref"); v != "" {
				merges = append(merges, v)
			}
		case "pageSetup":
			ps := &info.PageSetup
			ps.Paper = paperByCode(atoi(a.get("paperSize")))
			ps.ScalePercent = atoi(a.get("scale"))
			ps.FitWidth, ps.FitHeight = atoi(a.get("fitToWidth")), atoi(a.get("fitToHeight"))
			switch a.get("orientation") {
			case "portrait", "landscape":
				ps.Orientation = a.get("orientation")
			}
			switch a.get("pageOrder") {
			case "downThenOver", "overThenDown":
				ps.Order = a.get("pageOrder")
			}
		case "pageMargins":
			ps := &info.PageSetup
			if v, ok := num(a.get("left")); ok {
				ps.MarginLeft = v * 25.4
			}
			if v, ok := num(a.get("right")); ok {
				ps.MarginRight = v * 25.4
			}
			if v, ok := num(a.get("top")); ok {
				ps.MarginTop = v * 25.4
			}
			if v, ok := num(a.get("bottom")); ok {
				ps.MarginBottom = v * 25.4
			}
		}
	})
	if err != nil {
		return info, err
	}
	for _, m := range merges {
		parts := strings.Split(m, ":")
		if len(parts) != 2 {
			continue
		}
		r1, c1, ok1 := parseCell(parts[0])
		r2, c2, ok2 := parseCell(parts[1])
		if ok1 && ok2 && bottom >= 0 && r1 >= top && c1 >= left && r1 <= bottom && c1 <= right {
			bottom, right = max(bottom, r2), max(right, c2)
		}
	}
	if bottom < 0 {
		info.UsedRange = "A1"
		return info, nil
	}
	info.UsedRange = cellName(left, top) + ":" + cellName(right, bottom)
	info.UsedRows, info.UsedColumns = bottom-top+1, right-left+1
	return info, nil
}

// ---- writing the copy LibreOffice prints ----

func ptr[T any](v T) *T { return &v }

// writePrintCopy writes a copy of the workbook with only the region's sheet
// visible and its page setup applied.
func writePrintCopy(source, dest string, sheets []SheetInfo, r SheetRegion) error {
	if !ValidSheetRange(r.Range) {
		return errors.New("the print area is not a valid A1 cell range")
	}
	var sheet *SheetInfo
	for i := range sheets {
		if sheets[i].Index == r.Sheet {
			sheet = &sheets[i]
		}
	}
	if sheet == nil {
		return errors.New("the selected worksheet is no longer in the workbook")
	}
	f, err := excelize.OpenFile(source)
	if err != nil {
		return err
	}
	defer f.Close()
	list := f.GetSheetList()
	if r.Sheet < 0 || r.Sheet >= len(list) {
		return errors.New("the selected worksheet is no longer in the workbook")
	}
	name := list[r.Sheet]
	if err := f.SetSheetVisible(name, true); err != nil {
		return err
	}
	f.SetActiveSheet(r.Sheet)
	for i, other := range list {
		if i != r.Sheet {
			_ = f.SetSheetVisible(other, false)
		}
	}
	// The print area: the region's, else none (LibreOffice prints the content).
	_ = f.DeleteDefinedName(&excelize.DefinedName{Name: "_xlnm.Print_Area", Scope: name})
	if r.WritePrintArea {
		escaped := strings.ReplaceAll(name, "'", "''")
		var parts []string
		for _, p := range splitAreas(r.Range) {
			parts = append(parts, "'"+escaped+"'!"+absoluteRange(p))
		}
		if err := f.SetDefinedName(&excelize.DefinedName{Name: "_xlnm.Print_Area", RefersTo: strings.Join(parts, ","), Scope: name}); err != nil {
			return err
		}
	}
	if r.IncludeHidden {
		rows, err := f.Rows(name)
		if err == nil {
			n := 0
			for rows.Next() {
				n++
			}
			_ = rows.Close()
			for row := 1; row <= n; row++ {
				if vis, err := f.GetRowVisible(name, row); err == nil && !vis {
					_ = f.SetRowVisible(name, row, true)
				}
			}
		}
		for c := 1; c <= max(sheet.UsedColumns+64, 64) && c <= 16384; c++ {
			col := strings.TrimRight(cellName(c-1, 0), "1")
			if vis, err := f.GetColVisible(name, col); err == nil && !vis {
				_ = f.SetColVisible(name, col, true)
			}
		}
	}
	if !r.KeepBreaks {
		removeBreaks(f, name)
	}
	in := func(mm float64) *float64 { return ptr(mm / 25.4) }
	if err := f.SetPageMargins(name, &excelize.PageLayoutMarginsOptions{
		Left: in(r.MarginLeft), Right: in(r.MarginRight), Top: in(r.MarginTop), Bottom: in(r.MarginBottom),
	}); err != nil {
		return err
	}
	layout := &excelize.PageLayoutOptions{}
	if p := paperByID(r.Paper); p.Code != 0 {
		layout.Size = ptr(p.Code)
	}
	if r.Orientation == "portrait" || r.Orientation == "landscape" {
		layout.Orientation = ptr(r.Orientation)
	}
	if r.Order == "downThenOver" || r.Order == "overThenDown" {
		layout.PageOrder = ptr(r.Order)
	}
	switch r.ScaleMode {
	case "percent":
		layout.AdjustTo = ptr(uint(clampInt(r.ScalePercent, 10, 400)))
		_ = f.SetSheetProps(name, &excelize.SheetPropsOptions{FitToPage: ptr(false)})
	case "fit":
		layout.FitToWidth = ptr(clampInt(r.FitWidth, 1, 100))
		layout.FitToHeight = ptr(clampInt(r.FitHeight, 0, 100))
		_ = f.SetSheetProps(name, &excelize.SheetPropsOptions{FitToPage: ptr(true)})
	}
	if err := f.SetPageLayout(name, layout); err != nil {
		return err
	}
	return f.SaveAs(dest)
}

func absoluteRange(part string) string {
	cells := strings.Split(strings.ReplaceAll(strings.TrimSpace(part), "$", ""), ":")
	for i, c := range cells {
		switch {
		case cellRef.MatchString(c):
			m := cellRef.FindStringSubmatch(c)
			cells[i] = "$" + strings.ToUpper(m[1]) + "$" + m[2]
		default:
			cells[i] = "$" + strings.ToUpper(c)
		}
	}
	return strings.Join(cells, ":")
}

// removeBreaks drops the sheet's manual page breaks.
func removeBreaks(f *excelize.File, sheet string) {
	// excelize removes a break at a cell; ask for every row and column break in the used part.
	rows, err := f.GetRows(sheet)
	if err != nil {
		return
	}
	for r := 1; r <= len(rows)+1; r++ {
		_ = f.RemovePageBreak(sheet, cellName(0, r-1))
	}
	cols := 0
	for _, row := range rows {
		cols = max(cols, len(row))
	}
	for c := 1; c <= cols+1; c++ {
		_ = f.RemovePageBreak(sheet, cellName(c-1, 0))
	}
}

// ---- the session ----

type sheetSession struct {
	mu     sync.Mutex
	sheets []SheetInfo
	work   string
	// rendered: region hash → PDF of it.
	rendered map[string]string
	current  string
}

func (ss *sheetSession) release() {
	if ss.work != "" {
		_ = os.RemoveAll(ss.work)
	}
}

func (s *server) sheetSession(t *ToolJob) (*sheetSession, error) {
	t.mu.Lock()
	ss := t.sheets
	path := t.Files[0].Path
	t.mu.Unlock()
	if ss != nil {
		return ss, nil
	}
	sheets, err := InspectWorkbook(path)
	if err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp("", "filee-sheets")
	if err != nil {
		return nil, err
	}
	ss = &sheetSession{sheets: sheets, work: work, rendered: map[string]string{}}
	t.mu.Lock()
	if t.sheets == nil {
		t.sheets = ss
	} else {
		_ = os.RemoveAll(work)
		ss = t.sheets
	}
	t.mu.Unlock()
	return ss, nil
}

func (s *server) printInspect(w http.ResponseWriter, r *http.Request, t *ToolJob) {
	ss, err := s.sheetSession(t)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	var regions []SheetRegion
	for _, sh := range ss.sheets {
		regions = append(regions, RegionFromSource(sh))
	}
	status := s.libreOffice()
	writeJSON(w, map[string]any{
		"file": t.Files[0].Name, "sheets": ss.sheets, "regions": regions, "papers": SheetPapers,
		"available": status.Available, "reason": s.a.Text(orDefault(status.ReasonKey, "engine.reason.not_installed")),
	})
}

// libreOffice is the LibreOffice engine's status.
func (s *server) libreOffice() convert.Status {
	c := s.a.Catalog().Find("libreoffice")
	if c == nil {
		return convert.Unavailable("engine.reason.not_installed", "")
	}
	return s.a.Catalog().StatusOf(c)
}

// renderRegion makes the PDF of one region (cached by its settings).
func (s *server) renderRegion(ctx context.Context, t *ToolJob, ss *sheetSession, reg SheetRegion) (string, error) {
	b, _ := json.Marshal(reg)
	sum := sha256.Sum256(b)
	key := hex.EncodeToString(sum[:8])
	ss.mu.Lock()
	if p, ok := ss.rendered[key]; ok {
		ss.mu.Unlock()
		return p, nil
	}
	ss.mu.Unlock()
	c := s.a.Catalog().Find("libreoffice")
	if st := s.libreOffice(); c == nil || !st.Available {
		return "", errors.New(s.a.Text(orDefault(st.ReasonKey, "engine.reason.not_installed")))
	}
	dir := filepath.Join(ss.work, key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	input := filepath.Join(dir, "sheet.xlsx")
	output := filepath.Join(dir, "sheet.pdf")
	if err := writePrintCopy(t.Files[0].Path, input, ss.sheets, reg); err != nil {
		return "", err
	}
	step := &convert.Step{InputPath: input, From: "xlsx", To: "pdf", Preset: presets.New("", "pdf"), WorkDir: dir,
		Output: convert.AllocatorFunc(func(string, string) string { return output }), OriginalSourcePath: t.Files[0].Path}
	if _, err := c.Convert(ctx, step, nil); err != nil {
		return "", err
	}
	if _, err := os.Stat(output); err != nil {
		return "", errors.New("LibreOffice did not create the requested PDF")
	}
	ss.mu.Lock()
	ss.rendered[key] = output
	ss.mu.Unlock()
	return output, nil
}

func (s *server) printPreview(w http.ResponseWriter, r *http.Request, t *ToolJob) {
	var reg SheetRegion
	if !readJSON(w, r, &reg) {
		return
	}
	if !ValidSheetRange(reg.Range) {
		writeError(w, http.StatusBadRequest, errors.New(s.a.Text("sheet_studio.invalid_range")))
		return
	}
	ss, err := s.sheetSession(t)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	pdf, err := s.renderRegion(ctx, t, ss, reg)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	n, err := api.PageCountFile(ctx, pdf)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	ss.mu.Lock()
	ss.current = pdf
	ss.mu.Unlock()
	writeJSON(w, map[string]any{"pages": n, "token": filepath.Base(filepath.Dir(pdf)), "status": s.a.Text("sheet_studio.page_count", n)})
}

// printPage is a page of a rendered region as PNG (token from printPreview).
func (s *server) printPage(w http.ResponseWriter, r *http.Request, t *ToolJob) {
	ss, err := s.sheetSession(t)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	n, _ := strconv.Atoi(r.PathValue("n"))
	token := filepath.Base(r.URL.Query().Get("token"))
	pdf := filepath.Join(ss.work, token, "sheet.pdf")
	if _, err := os.Stat(pdf); token == "" || err != nil {
		writeError(w, http.StatusNotFound, errors.New("no preview"))
		return
	}
	maxDim, _ := strconv.Atoi(r.URL.Query().Get("max"))
	p, err := s.conv.preview(r.Context(), pdf, n, maxDim)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(p.PNG)
}

func (s *server) printExport(w http.ResponseWriter, r *http.Request, t *ToolJob) {
	var in struct {
		Regions []SheetRegion `json:"regions"`
		Folder  string        `json:"folder"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if len(in.Regions) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("nothing to export"))
		return
	}
	for _, reg := range in.Regions {
		if !ValidSheetRange(reg.Range) {
			writeError(w, http.StatusBadRequest, errors.New(s.a.Text("sheet_studio.invalid_range")))
			return
		}
	}
	ss, err := s.sheetSession(t)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	t.mu.Lock()
	p := t.Preset.Clone()
	source := t.Files[0].Path
	t.mu.Unlock()
	applied := s.a.Settings().WithDefaultOutput(p)
	if in.Folder != "" {
		applied = presets.ForChosenFolder(applied, in.Folder)
	}
	out := presets.Resolve(&applied.Output, presets.Tokens{SourcePath: source, PresetName: t.Name, Index: 1, Now: time.Now()}, "pdf", nil, "")
	if out == "" {
		writeJSON(w, map[string]any{"skipped": true, "message": s.a.Text("toast.skipped")})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Minute)
	defer cancel()
	var parts []string
	for _, reg := range in.Regions {
		pdf, err := s.renderRegion(ctx, t, ss, reg)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err)
			return
		}
		parts = append(parts, pdf)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	tmp := out + ".filee-tmp"
	defer os.Remove(tmp)
	if len(parts) == 1 {
		b, err := os.ReadFile(parts[0])
		if err == nil {
			err = os.WriteFile(tmp, b, 0o644)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	} else if err := api.MergeCreateFile(ctx, parts, tmp, false, nil); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := os.Rename(tmp, out); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.record(p, t.Name, []string{source}, []string{out})
	writeJSON(w, map[string]any{"path": out, "message": s.a.Text("sheet_studio.exported", filepath.Base(out))})
}
