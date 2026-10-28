package office

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/testutil"
)

const rels = `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="%s"/></Relationships>`

func TestDocmToDocxDropsMacros(t *testing.T) {
	dir := t.TempDir()
	in := testutil.Zip(t, filepath.Join(dir, "a.docm"),
		"[Content_Types].xml", `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="bin" ContentType="application/vnd.ms-office.vbaProject"/><Override PartName="/word/document.xml" ContentType="application/vnd.ms-word.document.macroEnabled.main+xml"/><Override PartName="/word/vbaData.xml" ContentType="application/vnd.ms-word.vbaData+xml"/></Types>`,
		"_rels/.rels", strings.Replace(rels, "%s", "word/document.xml", 1),
		"word/document.xml", `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Hi</w:t></w:r></w:p></w:body></w:document>`,
		"word/_rels/document.xml.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.microsoft.com/office/2006/relationships/vbaProject" Target="vbaProject.bin"/><Relationship Id="rId2" Type="x/styles" Target="styles.xml"/></Relationships>`,
		"word/vbaProject.bin", "VBA",
		"word/_rels/vbaProject.bin.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="x" Target="vbaData.xml"/></Relationships>`,
		"word/vbaData.xml", `<x/>`,
	)
	step := testutil.Step(t, in, "docm", "docx")
	out, err := New().Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	z := testutil.ReadZip(t, out[0])
	for _, gone := range []string{"word/vbaProject.bin", "word/_rels/vbaProject.bin.rels", "word/vbaData.xml"} {
		if _, ok := z[gone]; ok {
			t.Errorf("%s kept", gone)
		}
	}
	ct := z["[Content_Types].xml"]
	if !strings.Contains(ct, Variants["docx"].ContentType) || strings.Contains(ct, "vbaProject") || strings.Contains(ct, "vbaData") {
		t.Errorf("content types: %s", ct)
	}
	if r := z["word/_rels/document.xml.rels"]; strings.Contains(r, "vbaProject") || !strings.Contains(r, "styles.xml") {
		t.Errorf("rels: %s", r)
	}
	if !strings.Contains(z["word/document.xml"], "Hi") {
		t.Error("document lost")
	}
}

func TestXlsmToXlsxRemovesMacroSheet(t *testing.T) {
	dir := t.TempDir()
	in := testutil.Zip(t, filepath.Join(dir, "b.xlsm"),
		"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/xl/workbook.xml" ContentType="application/vnd.ms-excel.sheet.macroEnabled.main+xml"/><Override PartName="/xl/macrosheets/sheet1.xml" ContentType="application/vnd.ms-excel.macrosheet+xml"/></Types>`,
		"_rels/.rels", strings.Replace(rels, "%s", "/xl/workbook.xml", 1),
		"xl/workbook.xml", `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><bookViews><workbookView activeTab="1"/></bookViews><sheets><sheet name="Macro1" sheetId="1" r:id="rId1"/><sheet name="Data" sheetId="2" r:id="rId2"/></sheets><definedNames><definedName name="a" localSheetId="0">x</definedName><definedName name="b" localSheetId="1">y</definedName></definedNames></workbook>`,
		"xl/_rels/workbook.xml.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="x/macrosheet" Target="macrosheets/sheet1.xml"/><Relationship Id="rId2" Type="x/worksheet" Target="worksheets/sheet2.xml"/></Relationships>`,
		"xl/macrosheets/sheet1.xml", `<x/>`,
		"xl/worksheets/sheet2.xml", `<worksheet/>`,
	)
	out := filepath.Join(dir, "b.xlsx")
	if err := Rewrite(in, out, "xlsx"); err != nil {
		t.Fatal(err)
	}
	wb := testutil.ReadZip(t, out)["xl/workbook.xml"]
	if strings.Contains(wb, "Macro1") || !strings.Contains(wb, `name="Data"`) || strings.Contains(wb, `name="a"`) ||
		!strings.Contains(wb, `localSheetId="0">y`) || strings.Contains(wb, "activeTab") {
		t.Errorf("workbook: %s", wb)
	}
}

func TestEdgesStayInFamily(t *testing.T) {
	p := convert.NewPlanner([]convert.Converter{New()}, nil)
	if r := p.Plan("docm", "docx", convert.Options{}); r == nil || len(r.Steps) != 1 {
		t.Fatal("no docm → docx")
	}
	if p.Plan("docx", "xlsx", convert.Options{}) != nil {
		t.Fatal("cross-family route")
	}
	if n := len(New().Edges()); n != 4*3+3*2+5*4 {
		t.Fatalf("%d edges", n)
	}
}
