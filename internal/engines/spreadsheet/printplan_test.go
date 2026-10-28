package spreadsheet

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/testutil"
)

func TestPrintPlan(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "b.xlsx")
	book := sampleBook()
	if err := WriteXLSX(book, p); err != nil {
		t.Fatal(err)
	}
	info, err := InspectPrint(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Sheets) != 2 || info.Sheets[0].UsedRange != "A1:C4" || info.Sheets[0].UsedColumns != 3 || info.Sheets[0].HiddenColumns != 1 {
		t.Fatalf("%+v", info.Sheets[0])
	}
	region := RegionFromSource(info.Sheets[0])
	region.Range, region.WritePrintArea, region.Paper, region.Orientation, region.ScaleMode, region.FitWidth = "A1:B3", true, PaperA3, OrientationLandscape, ScaleFitToPages, 1
	out := filepath.Join(dir, "print.xlsx")
	if err := CreatePrintPackage(p, out, info, region); err != nil {
		t.Fatal(err)
	}
	z := testutil.ReadZip(t, out)
	if wb := z["xl/workbook.xml"]; !strings.Contains(wb, `'Data 2026'!$A$1:$B$3`) || !strings.Contains(wb, `state="hidden"`) {
		t.Errorf("workbook %s", wb)
	}
	if s := z["xl/worksheets/sheet1.xml"]; !strings.Contains(s, `paperSize="8"`) || !strings.Contains(s, `orientation="landscape"`) || !strings.Contains(s, `fitToPage="1"`) || !strings.Contains(s, "<sheetPr>") {
		t.Errorf("sheet %s", s)
	}
	back, err := ReadXLSX(out)
	if err != nil || len(back.Sheets) != 1 {
		t.Fatalf("reread %v %v", err, back)
	}
	if !IsValidPrintRange("A1:C3,E:F,2:4") || IsValidPrintRange("A1:ZZZZ9") || IsValidPrintRange("") {
		t.Error("ranges")
	}
}
