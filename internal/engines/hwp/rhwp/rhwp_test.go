package rhwp

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/hwp/hwpx"
	"github.com/dseif0x/filee-go/internal/engines/hwp/xmltree"
	"github.com/dseif0x/filee-go/internal/presets"
)

func eq[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", what, got, want)
	}
}

func writeZip(t *testing.T, path string, entries ...[2]string) string {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, e := range entries {
		w, _ := z.Create(e[0])
		w.Write([]byte(e[1]))
	}
	z.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func docxWith(t *testing.T, dir, name, text, styles string) string {
	ns := `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`
	entries := [][2]string{{"word/document.xml", `<w:document ` + ns + `><w:body><w:p><w:r><w:t xml:space="preserve">` + hwpx.Escape(text) + `</w:t></w:r></w:p></w:body></w:document>`}}
	if styles != "" {
		entries = append(entries, [2]string{"word/styles.xml", styles})
	}
	return writeZip(t, filepath.Join(dir, name), entries...)
}

func textFile(t *testing.T, dir, name, text string) string {
	p := filepath.Join(dir, name)
	os.WriteFile(p, []byte(text), 0o644)
	return p
}

// ───────────────────────── Scripts ─────────────────────────

func TestScriptsThatNeedShapingAreFoundInDocuments(t *testing.T) {
	dir := t.TempDir()
	shaping := func(p, format string) string { s, _ := FindBothInFile(p, format); return s }
	eq(t, "gujarati", shaping(docxWith(t, dir, "gujarati.docx", "સૌરાષ્ટ્ર ઘરખેડ", ""), "docx"), "Gujarati")
	eq(t, "arabic", shaping(docxWith(t, dir, "arabic.docx", "Report: مرحبا بالعالم", ""), "docx"), "Arabic")
	eq(t, "plain", shaping(docxWith(t, dir, "plain.docx", "English, 한국어, 中文, Ελληνικά, Русский", ""), "docx"), "")
	eq(t, "styles", shaping(docxWith(t, dir, "styles.docx", "English only", `<w:styles><w:style w:styleId="Title"><w:name w:val="عنوان"/></w:style></w:styles>`), "docx"), "")
	eq(t, "hindi", shaping(textFile(t, dir, "hindi.txt", "Title\nहिन्दी भाषा"), "txt"), "Devanagari")
	eq(t, "hebrew", shaping(textFile(t, dir, "hebrew.html", "<p>&#x5E9;&#x5DC;&#x5D5;&#x5DD;</p>"), "html"), "Hebrew")
	eq(t, "thai", shaping(textFile(t, dir, "thai.rtf", "{\\rtf1 \\u3585?\\u3634?}"), "rtf"), "Thai")
	eq(t, "md", shaping(textFile(t, dir, "plain.md", "# Title\n\nC:\\users\\me and &#169; 2026"), "md"), "")
	eq(t, "hwp", shaping(textFile(t, dir, "binary.hwp", "ignored"), "hwp"), "")
	const mhtml = "MIME-Version: 1.0\r\nContent-Type: multipart/related; boundary=\"b\"\r\n\r\n--b\r\n" +
		"Content-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n" +
		"<p>=E0=AA=B8=E0=AB=8C=E0=AA=B0=E0=AA=BE</p>\r\n--b--\r\n"
	eq(t, "mhtml", shaping(textFile(t, dir, "page.mhtml", mhtml), "mhtml"), "Gujarati")
	eq(t, "plain mhtml", shaping(textFile(t, dir, "plain.mhtml", strings.Replace(mhtml, "=E0=AA=B8=E0=AB=8C=E0=AA=B0=E0=AA=BE", "Hello", 1)), "mhtml"), "")
	ofd := func(name, text string) string {
		return writeZip(t, filepath.Join(dir, name),
			[2]string{"Doc_0/Pages/Page_0/Content.xml", `<ofd:Page xmlns:ofd="http://www.ofdspec.org/2016"><ofd:TextCode X="0" Y="4">` + text + `</ofd:TextCode></ofd:Page>`},
			[2]string{"Doc_0/PublicRes.xml", `<Res><Font ID="1" FontName="UKIJ Tuz مرحبا"/></Res>`})
	}
	eq(t, "uyghur", shaping(ofd("uyghur.ofd", "ئۇيغۇرچە ھۆججەت"), "ofd"), "Arabic")
	eq(t, "chinese", shaping(ofd("chinese.ofd", "电子发票（普通发票）"), "ofd"), "")
}

func TestScriptsWithoutWidthsAreFoundInDocuments(t *testing.T) {
	dir := t.TempDir()
	unmeasured := func(p, format string) string { _, u := FindBothInFile(p, format); return u }
	eq(t, "polish", unmeasured(docxWith(t, dir, "polish.docx", "Zażółć gęślą jaźń", ""), "docx"), "Latin Extended")
	eq(t, "vietnamese", unmeasured(textFile(t, dir, "vietnamese.md", "Tiếng Việt"), "md"), "Latin Extended")
	eq(t, "greek", unmeasured(docxWith(t, dir, "greek.docx", "Αναφορά Report", ""), "docx"), "Greek")
	eq(t, "russian", unmeasured(docxWith(t, dir, "russian.docx", "Отчёт за месяц", ""), "docx"), "Cyrillic")
	eq(t, "russian txt", unmeasured(textFile(t, dir, "russian.txt", "Заголовок\nтекст"), "txt"), "Cyrillic")
	eq(t, "armenian", unmeasured(textFile(t, dir, "armenian.txt", "Հայերեն տեքստ"), "txt"), "Armenian")
	eq(t, "georgian", unmeasured(textFile(t, dir, "georgian.txt", "ქართული ტექსტი"), "txt"), "Georgian")
	eq(t, "decomposed", unmeasured(textFile(t, dir, "decomposed.md", "café"), "md"), "combining marks")
	for name, text := range map[string]string{"latin1": "Ça été über Straße niño", "korean": "한국어 문서", "japanese": "日本語のテキスト",
		"chinese": "中文文档", "fullwidth": "ＡＢＣ", "arabic": "مرحبا بالعالم"} {
		eq(t, name, unmeasured(docxWith(t, dir, name+".docx", text, ""), "docx"), "")
	}
	eq(t, "lone symbols", unmeasured(textFile(t, dir, "symbols.md", "각도 α = 30°, 저항 10 Ω, 두께 5 μm"), "md"), "")
	eq(t, "greek word", unmeasured(textFile(t, dir, "word.md", "결과: Αναφορά"), "md"), "Greek")
	eq(t, "short", unmeasured(textFile(t, dir, "short.md", "Да"), "md"), "Cyrillic")
	// Variation selectors ride on a letter rhwp measures.
	eq(t, "emoji", unmeasured(docxWith(t, dir, "emoji.docx", "Déjà vu ❤️ ✔️", ""), "docx"), "")
	eq(t, "ivs", unmeasured(textFile(t, dir, "ivs.txt", "葛\U000E0100の本文"), "txt"), "")
	// Both answers from one scan; references outside Unicode are skipped.
	s, u := FindBothInFile(textFile(t, dir, "mixed.txt", "Отчёт مرحبا"), "txt")
	eq(t, "mixed", s+"/"+u, "Arabic/Cyrillic")
	s, _ = FindBothInFile(textFile(t, dir, "bad-ref.html", "<p>&#1114112; &#x5E9;&#x5DC;</p>"), "html")
	eq(t, "bad html ref", s, "Hebrew")
	s, _ = FindBothInFile(textFile(t, dir, "bad-ref.rtf", "{\\rtf1 \\u-99999? \u1488\\u1513?\\u1500?}"), "rtf")
	eq(t, "bad rtf ref", s, "Hebrew")
}

func TestAnHwpxStaysWithRhwpForUnmeasuredLetters(t *testing.T) {
	path := writeZip(t, filepath.Join(t.TempDir(), "report.hwpx"),
		[2]string{"Contents/section0.xml", `<hs:sec xmlns:hs="http://www.hancom.co.kr/hwpml/2011/section"><hp:t xmlns:hp="x">보고서 Отчёт</hp:t></hs:sec>`})
	_, u := FindBothInFile(path, "hwpx")
	eq(t, "unmeasured", u, "Cyrillic")
	eq(t, "scan", New().ScanScripts(path, "hwpx").WithoutWidths, "")
	for _, e := range New().Edges() {
		if e.To == "pdf" && !e.Unshaped {
			t.Error("rhwp's PDF edges must be unshaped")
		}
	}
}

// ───────────────────────── Fonts ─────────────────────────

const (
	latinChars    = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789.,"
	cyrillicChars = "АБВГДЕЖЗИКЛМНОПРСТУФХЦЧШЩЭЮЯабвгдежзиклмнопрстуфхцчшщэюя"
	hangulChars   = "한글문서대한민국섞인장입니다"
	hanChars      = "改造我们的学习这是一个测试文档说话国家和会时间漢字"
)

// computer is a fake computer: font name → (embeddable, characters it has).
var computer = map[string]struct {
	usable bool
	chars  string
}{
	"맑은 고딕":           {true, latinChars + hangulChars + "漢字國"},
	"함초롬바탕":           {true, latinChars + hangulChars + "漢字國"},
	"Microsoft YaHei": {true, latinChars + cyrillicChars + hanChars},
	"SimSun":          {true, latinChars + hanChars},
	"Yu Gothic":       {true, latinChars + hanChars + "日本語のテキスト"},
	"Segoe UI":        {true, latinChars + cyrillicChars},
	"Times New Roman": {true, latinChars + cyrillicChars},
	"思源宋体 VF Medium":  {false, latinChars + hanChars},
}

func fakeLookup(name string) *InstalledFont {
	f, ok := computer[name]
	if !ok {
		return nil
	}
	return &InstalledFont{Usable: f.usable, Has: func(r rune) bool { return strings.ContainsRune(f.chars, r) }}
}

func plan(text string, slots ...slotFace) map[slotFace]string { return Plan(slots, text, fakeLookup) }

func TestFontPlans(t *testing.T) {
	// Issue #64: Markdown → PDF in Chinese mixed Malgun Gothic and a fallback font character by character.
	p := plan("改造我们的学习 这是一个测试文档。", slotFace{"HANJA", "맑은 고딕"}, slotFace{"LATIN", "맑은 고딕"}, slotFace{"HANJA", "함초롬바탕"})
	eq(t, "hanja sans", p[slotFace{"HANJA", "맑은 고딕"}], "Microsoft YaHei")
	eq(t, "latin sans", p[slotFace{"LATIN", "맑은 고딕"}], "Microsoft YaHei")
	eq(t, "hanja serif", p[slotFace{"HANJA", "함초롬바탕"}], "SimSun")
	eq(t, "korean keeps", len(plan("대한민국 漢字 섞인 문장입니다. English 123.", slotFace{"HANGUL", "맑은 고딕"}, slotFace{"HANJA", "맑은 고딕"}, slotFace{"LATIN", "맑은 고딕"})), 0)
	eq(t, "cyrillic", plan("Привет мир. Hello.", slotFace{"HANGUL", "맑은 고딕"}, slotFace{"LATIN", "맑은 고딕"})[slotFace{"HANGUL", "맑은 고딕"}], "Segoe UI")
	eq(t, "with hangul", len(plan("한글 Привет", slotFace{"HANGUL", "맑은 고딕"})), 0)
	eq(t, "cff2", plan("这是一个测试文档", slotFace{"HANJA", "思源宋体 VF Medium"})[slotFace{"HANJA", "思源宋体 VF Medium"}], "SimSun")
	eq(t, "not installed", len(plan("这是一个测试文档", slotFace{"HANJA", "Not Installed"})), 0)
}

func TestLanguageAndScripts(t *testing.T) {
	for text, want := range map[string]string{"这是我们的国家": "zh-Hans", "這是我們的國家": "zh-Hant", "日本語のテキスト": "ja",
		"这是一个测试文档，我们说的话和时间。书名：日本語のテキスト": "zh-Hans", "한국어 漢字": "ko", "Привет": ""} {
		eq(t, text, Language(text), want)
	}
	for face, want := range map[string]struct {
		script string
		serif  bool
	}{"思源宋体 VF Medium": {"zh-Hans", true}, "Source Han Sans SC VF": {"zh-Hans", false}, "Noto Serif TC": {"zh-Hant", true},
		"Source Han Serif K VF": {"ko", true}, "함초롬돋움": {"ko", false}, "Malgun Gothic": {"ko", false}, "Source Han Sans JP": {"ja", false}, "Inter": {"", false}} {
		eq(t, face+" script", Script(face), want.script)
		eq(t, face+" serif", IsSerif(face), want.serif)
	}
}

// koreanHwpx is a small HWPX of Filee's writer: Korean text with a few Han characters, the default fonts.
func koreanHwpx(t *testing.T, dir string) string {
	s := doc.NewSection()
	s.Blocks = []doc.Block{&doc.Paragraph{Inlines: []doc.Inline{&doc.Text{Text: "한글 문서입니다. 漢字 몇 개."}}}}
	p := filepath.Join(dir, "korean.hwpx")
	if err := hwpx.Write(&doc.Document{Sections: []*doc.Section{s}}, p, nil); err != nil {
		t.Fatal(err)
	}
	return p
}

func entryOf(t *testing.T, path, name string) string {
	t.Helper()
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name == name {
			r, _ := f.Open()
			b, _ := io.ReadAll(r)
			r.Close()
			return string(b)
		}
	}
	return ""
}

func faces(t *testing.T, path string) []string {
	d, err := xmltree.ParseString(entryOf(t, path, "Contents/header.xml"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var out []string
	for _, f := range d.Root.DescendantsNamed("font") {
		if face := f.A("face"); face != "" && !seen[face] {
			seen[face] = true
			out = append(out, face)
		}
	}
	return out
}

func TestACopyOfTheHwpxNamesTheReplacement(t *testing.T) {
	dir := t.TempDir()
	path := koreanHwpx(t, dir)
	before := faces(t, path)
	broken := before[0]
	copy := Prepare(path, dir, func(face string) *InstalledFont {
		switch face {
		case broken:
			return &InstalledFont{Usable: false, Has: func(rune) bool { return true }}
		case "Malgun Gothic":
			return &InstalledFont{Usable: true, Has: func(rune) bool { return true }}
		}
		return nil
	})
	if copy == "" {
		t.Fatal("no copy")
	}
	after := strings.Join(faces(t, copy), "|")
	if strings.Contains("|"+after+"|", "|"+broken+"|") || !strings.Contains(after, "Malgun Gothic") {
		t.Errorf("faces of the copy: %s", after)
	}
	eq(t, "original untouched", strings.Join(faces(t, path), "|"), strings.Join(before, "|"))
	z, _ := zip.OpenReader(copy)
	eq(t, "first entry", z.File[0].Name, "mimetype")
	z.Close()
	all := func(string) *InstalledFont { return &InstalledFont{Usable: true, Has: func(rune) bool { return true }} }
	eq(t, "nothing to replace", Prepare(path, dir, all), "")
}

func TestPdfOptions(t *testing.T) {
	font := func() *InstalledFont { return &InstalledFont{Usable: true, Has: func(rune) bool { return true }} }
	eq(t, "cambria", strings.Join(PdfOptions(func(n string) *InstalledFont {
		if n == "Cambria Math" {
			return font()
		}
		return nil
	}), " "), "--equation-font Cambria Math")
	eq(t, "symbols installed", len(PdfOptions(func(n string) *InstalledFont {
		if n == "Cambria Math" || n == "Arial Unicode MS" {
			return font()
		}
		return nil
	})), 0)
	eq(t, "nothing", len(PdfOptions(func(string) *InstalledFont { return nil })), 0)
	eq(t, "fallbacks", strings.Join(PdfOptions(func(n string) *InstalledFont {
		if n == "Noto Serif" || n == "Noto Sans" || n == "Noto Sans Mono" {
			return font()
		}
		return nil
	}), " "), "--fallback-serif Noto Serif --fallback-sans Noto Sans --fallback-mono Noto Sans Mono")
}

// ───────────────────────── Input fixes ─────────────────────────

func TestEquationScriptsAreNormalised(t *testing.T) {
	for script, want := range map[string]string{
		"f ^{2} = f CIRCf": "f ^{2} = f CIRC f", "n(A SMALLINTERB)": "n(A SMALLINTER B)", "A SUBSETB": "A SUBSET B",
		"1 lek le28": "1 le k le 28", "rootx + rootxy": "root x + root xy", "P CUP Q!=U": "P CUP Q NEQ U", "4 +- root 6": "4  PLUSMINUS  root 6",
	} {
		eq(t, script, NormaliseEquation(script), want)
	}
	for _, script := range []string{"left ( x right ) leq 3", "index line inside lemma", `"CIRCf != x" + y`, "x SUBSETEQ y"} {
		eq(t, script, NormaliseEquation(script), script)
	}
	// Filee's own scripts are left alone.
	for _, script := range []string{"{{a + b} over {2 c}}", "sqrt {b ^{2} - 4 ac} + root {3} of {x}", "lim _{x rarrow 0} {{sin x} over {x}}", "f ( x ) = cases {x , & x GEQ 0 # - x , & x < 0}"} {
		eq(t, script, NormaliseEquation(script), script)
	}
}

func TestShapesSizedRelativeToTheColumnGetTheirLaidOutSize(t *testing.T) {
	d, _ := xmltree.ParseString(`<hp:sec xmlns:hp="http://www.hancom.co.kr/hwpml/2011/paragraph">` +
		`<hp:rect><hp:sz width="9920" widthRelTo="COLUMN" height="3206" heightRelTo="ABSOLUTE"/><hp:curSz width="23053" height="24841"/></hp:rect>` +
		`<hp:pic><hp:sz width="5000" widthRelTo="ABSOLUTE" height="4000" heightRelTo="ABSOLUTE"/><hp:curSz width="6000" height="4800"/></hp:pic></hp:sec>`)
	if !FixShapeSizes(d.Root) {
		t.Fatal("nothing fixed")
	}
	sizes := d.Root.DescendantsNamed("sz")
	eq(t, "rect", fmt.Sprintln(sizes[0].A("width"), sizes[0].A("height"), sizes[0].A("widthRelTo"), sizes[0].A("heightRelTo")), "23053 24841 ABSOLUTE ABSOLUTE\n")
	eq(t, "pic", sizes[1].A("width"), "5000")
	if FixShapeSizes(d.Root) {
		t.Error("fixed twice")
	}
}

// The form of #49: in a table cell taller than its text, "Title", a paragraph holding only a table that floats 500
// below the paragraph's top (three rows of 2000, outer margins 141) and "After". Paragraphs are 2800 apart.
const (
	step, offset, rowHeight, rows, margin, outerHeight = 2800, 500, 2000, 3, 141, 30_000
	anchorTop                                          = step
	underTable                                         = anchorTop + offset + margin + rows*rowHeight + margin
)

func pXML(line int, content ...string) string {
	return `<hp:p id="0" paraPrIDRef="0" styleIDRef="0" pageBreak="0" columnBreak="0" merged="0"><hp:run charPrIDRef="0">` + strings.Join(content, "") + `</hp:run>` + lines(line, 1000) + `</hp:p>`
}

func lines(vertpos, height int) string {
	return fmt.Sprintf(`<hp:linesegarray><hp:lineseg textpos="0" vertpos="%d" vertsize="%d" textheight="%d" baseline="%d" spacing="800" horzpos="0" horzsize="45130" flags="393216"/></hp:linesegarray>`, vertpos, height, height, height*85/100)
}

func tXML(text string) string { return "<hp:t>" + text + "</hp:t>" }

func tableXML(width, rh int, treatAsChar bool, vertOffset int, rowsOf [][]string) string {
	tac := 0
	if treatAsChar {
		tac = 1
	}
	m := func(name string) string {
		return fmt.Sprintf(`<hp:%s left="%d" right="%d" top="%d" bottom="%d"/>`, name, margin, margin, margin, margin)
	}
	s := fmt.Sprintf(`<hp:tbl id="7" zOrder="0" numberingType="TABLE" textWrap="TOP_AND_BOTTOM" textFlow="BOTH_SIDES" lock="0" dropcapstyle="None" pageBreak="CELL" repeatHeader="1" rowCnt="%d" colCnt="1" cellSpacing="0" borderFillIDRef="2" noAdjust="0">`, len(rowsOf)) +
		fmt.Sprintf(`<hp:sz width="%d" widthRelTo="ABSOLUTE" height="%d" heightRelTo="ABSOLUTE" protect="0"/>`, width, len(rowsOf)*rh) +
		fmt.Sprintf(`<hp:pos treatAsChar="%d" affectLSpacing="0" flowWithText="1" allowOverlap="0" holdAnchorAndSO="0" vertRelTo="PARA" horzRelTo="COLUMN" vertAlign="TOP" horzAlign="LEFT" vertOffset="%d" horzOffset="0"/>`, tac, vertOffset) +
		m("outMargin") + m("inMargin")
	for r, paragraphs := range rowsOf {
		s += `<hp:tr><hp:tc name="" header="0" hasMargin="0" protect="0" editable="0" dirty="0" borderFillIDRef="2"><hp:subList id="" textDirection="HORIZONTAL" lineWrap="BREAK" vertAlign="TOP" linkListIDRef="0" linkListNextIDRef="0" textWidth="0" textHeight="0" hasTextRef="0" hasNumRef="0">` +
			strings.Join(paragraphs, "") + "</hp:subList>" +
			fmt.Sprintf(`<hp:cellAddr colAddr="0" rowAddr="%d"/><hp:cellSpan colSpan="1" rowSpan="1"/><hp:cellSz width="%d" height="%d"/>`, r, width, rh) + m("cellMargin") + "</hp:tc></hp:tr>"
	}
	return s + "</hp:tbl>"
}

func anchorXML(text string, treatAsChar bool, line int) string {
	inner := [][]string{{pXML(0, tXML("InnerA"))}, {pXML(0, tXML("InnerB"))}, {pXML(0, tXML("InnerC"))}}
	content := []string{tableXML(30_000, rowHeight, treatAsChar, offset, inner)}
	if text != "" {
		content = append(content, tXML(text))
	}
	return pXML(line, content...)
}

func outerTable(anchor string) string {
	return tableXML(40_000, outerHeight, true, 0, [][]string{{pXML(0, tXML("Title")), anchor, pXML(underTable+step, tXML("After"))}})
}

const secOpen = `<hs:sec xmlns:hs="http://www.hancom.co.kr/hwpml/2011/section" xmlns:hp="http://www.hancom.co.kr/hwpml/2011/paragraph">`

func sectionOf(t *testing.T, body string) *xmltree.Element {
	d, err := xmltree.ParseString(secOpen + body + "</hs:sec>")
	if err != nil {
		t.Fatal(err)
	}
	return d.Root
}

func lineOf(root *xmltree.Element, text string, anchor bool) int {
	var paragraph *xmltree.Element
	for _, p := range root.DescendantsNamed("p") {
		for _, r := range p.Children("run") {
			for _, tt := range r.Children("t") {
				if tt.Value() == text {
					paragraph = p
				}
			}
		}
	}
	if anchor {
		for _, a := range paragraph.Ancestors() {
			if a.Local == "p" {
				paragraph = a
				break
			}
		}
	}
	v, _ := number(paragraph.Child("linesegarray").Child("lineseg"), "vertpos")
	return v
}

func TestATableFloatingInACellIsDrawnFromTheTopOfItsParagraph(t *testing.T) {
	root := sectionOf(t, pXML(0, tXML("Before"), outerTable(anchorXML("", false, underTable))))
	if !AnchorTablesInCells(root) {
		t.Fatal("nothing moved")
	}
	var cellLines []int
	for _, p := range root.FirstDescendant("subList").Children("p") {
		v, _ := number(p.Child("linesegarray").Child("lineseg"), "vertpos")
		cellLines = append(cellLines, v)
	}
	eq(t, "cell lines", fmt.Sprint(cellLines), fmt.Sprint([]int{0, anchorTop, underTable + step}))
	eq(t, "before", lineOf(root, "Before", false), 0)
	if AnchorTablesInCells(root) {
		t.Error("moved twice")
	}
	for name, body := range map[string]string{
		"with text": pXML(0, outerTable(anchorXML("next to the table", false, underTable))),
		"in line":   pXML(0, outerTable(anchorXML("", true, underTable))),
		"not under": pXML(0, outerTable(anchorXML("", false, anchorTop+offset))),
		"no cell":   pXML(0, tXML("Title")) + anchorXML("", false, underTable) + pXML(underTable+step, tXML("After")),
	} {
		if AnchorTablesInCells(sectionOf(t, body)) {
			t.Errorf("%s: moved", name)
		}
	}
}

func TestOnlyAnHwpxConvertedFromHwpGetsItsTablesMoved(t *testing.T) {
	dir := t.TempDir()
	makeForm := func(fromHwp bool) string {
		folder := filepath.Join(dir, map[bool]string{true: "from-hwp", false: "saved"}[fromHwp])
		os.MkdirAll(folder, 0o755)
		base := koreanHwpx(t, folder)
		section := entryOf(t, base, "Contents/section0.xml")
		d, _ := xmltree.ParseString(section)
		var setup *xmltree.Element
		for _, r := range d.Root.Child("p").Children("run") {
			if r.Child("secPr") != nil {
				setup = r
			}
		}
		attrs := `id="0" paraPrIDRef="0" styleIDRef="0" pageBreak="0" columnBreak="0" merged="0"`
		root := d.Root
		root.Kids = nil
		body := `<hp:p ` + attrs + `>` + setup.String() + `<hp:run charPrIDRef="0">` + tXML("Before") + `</hp:run>` + lines(0, 1000) + `</hp:p>` +
			`<hp:p ` + attrs + `><hp:run charPrIDRef="0">` + outerTable(anchorXML("", false, underTable)) + `</hp:run>` + lines(step, outerHeight+2*margin) + `</hp:p>`
		rootOpen := root.String()
		rootOpen = rootOpen[:len(rootOpen)-2] + ">"
		newSection := `<?xml version="1.0" encoding="UTF-8" standalone="yes" ?>` + rootOpen + body + "</hs:sec>"
		// Rewrite the package with the new section (and the marker).
		z, _ := zip.OpenReader(base)
		var entries [][2]string
		for _, f := range z.File {
			r, _ := f.Open()
			b, _ := io.ReadAll(r)
			r.Close()
			if f.Name == "Contents/section0.xml" {
				b = []byte(newSection)
			}
			entries = append(entries, [2]string{f.Name, string(b)})
		}
		z.Close()
		if fromHwp {
			entries = append(entries, [2]string{Hwp5OriginMarker, "2:paragraph-utf16"})
		}
		return writeZip(t, filepath.Join(folder, "form.hwpx"), entries...)
	}
	all := func(string) *InstalledFont { return &InstalledFont{Usable: true, Has: func(rune) bool { return true }} }
	fromHwp, saved := makeForm(true), makeForm(false)
	eq(t, "saved by 한글", Prepare(saved, dir, all), "")
	copy := Prepare(fromHwp, dir, all)
	if copy == "" {
		t.Fatal("no copy")
	}
	section := func(p string) *xmltree.Element {
		d, err := xmltree.ParseString(entryOf(t, p, "Contents/section0.xml"))
		if err != nil {
			t.Fatal(err)
		}
		return d.Root
	}
	eq(t, "moved", lineOf(section(copy), "InnerA", true), anchorTop)
	eq(t, "original", lineOf(section(fromHwp), "InnerA", true), underTable)
	if entryOf(t, copy, Hwp5OriginMarker) == "" {
		t.Error("marker lost")
	}
}

func TestRoomBelowEquationsThatReachUnderTheirLine(t *testing.T) {
	dir := t.TempDir()
	matrix := &hwpx.MathMatrix{Command: "pmatrix", Rows: [][]hwpx.MathNode{{&hwpx.MathNumber{Digits: "1"}, &hwpx.MathNumber{Digits: "2"}}, {&hwpx.MathNumber{Digits: "3"}, &hwpx.MathNumber{Digits: "4"}}}}
	zero := 0
	s := doc.NewSection()
	s.Blocks = []doc.Block{
		&doc.Paragraph{Format: doc.ParaFormat{After: &zero}, Inlines: []doc.Inline{&doc.Text{Text: "Inline "}, hwpx.NewEquation(&hwpx.MathScripts{Base: &hwpx.MathText{Text: "c"}, Sup: &hwpx.MathNumber{Digits: "2"}}, doc.CharFormat{})}},
		&doc.Paragraph{Format: doc.ParaFormat{After: &zero}, Inlines: []doc.Inline{hwpx.NewEquation(matrix, doc.CharFormat{})}},
		&doc.Paragraph{Format: doc.ParaFormat{After: &zero}, Inlines: []doc.Inline{&doc.Text{Text: "Text after"}}},
	}
	path := filepath.Join(dir, "eq.hwpx")
	if err := hwpx.Write(&doc.Document{Sections: []*doc.Section{s}}, path, nil); err != nil {
		t.Fatal(err)
	}
	copy := Prepare(path, dir, func(string) *InstalledFont { return &InstalledFont{Usable: true, Has: func(rune) bool { return true }} })
	if copy == "" {
		t.Fatal("no copy")
	}
	section, _ := xmltree.ParseString(entryOf(t, copy, "Contents/section0.xml"))
	head, _ := xmltree.ParseString(entryOf(t, copy, "Contents/header.xml"))
	after := func(p *xmltree.Element) int {
		for _, pp := range head.Root.DescendantsNamed("paraPr") {
			if pp.A("id") == p.A("paraPrIDRef") {
				v, _ := number(pp.FirstDescendant("next"), "value")
				return v
			}
		}
		return -1
	}
	var matrixEq *xmltree.Element
	for _, e := range section.Root.DescendantsNamed("equation") {
		if strings.Contains(e.Child("script").Value(), "pmatrix") {
			matrixEq = e
		}
	}
	height, _ := number(matrixEq.Child("sz"), "height")
	eq(t, "baseline", matrixEq.A("baseLine"), "50")
	paragraphs := section.Root.Children("p")
	eq(t, "matrix room", after(paragraphs[1]), int(math.Ceil(float64(height)*0.35)))
	eq(t, "text after", after(paragraphs[2]), 0)
	eq(t, "inline", after(paragraphs[0]), 0)
}

// ───────────────────────── Engine ─────────────────────────

func TestTimeLimitsAndEstimates(t *testing.T) {
	eq(t, "small", TimeLimit(0), baseTimeLimit)
	eq(t, "huge", TimeLimit(1<<40), maxTimeLimit)
	n := int64(10_000)
	if Typical(&n, 0, true) >= Typical(&n, 0, false) {
		t.Error("the direct backend is faster")
	}
	eq(t, "minimum", Typical(nil, 0, false), typicalDuration)
	path := koreanHwpx(t, t.TempDir())
	if c := Characters(path); c == nil || *c != int64(len([]rune("한글문서입니다.漢字몇개."))) {
		t.Errorf("characters %v", c)
	}
	if TextSize(path) <= 0 {
		t.Error("text size")
	}
	if !UsesDirectBackend("table.xlsx") || UsesDirectBackend("report.docx") {
		t.Error("direct backend choice")
	}
}

type fixedOutput string

func (f fixedOutput) Allocate(ext, suffix string) string { return string(f) }

func TestRhwpConverts(t *testing.T) {
	if Locate() == "" {
		t.Skip("rhwp not installed")
	}
	dir := t.TempDir()
	path := koreanHwpx(t, dir)
	out := filepath.Join(dir, "out.pdf")
	step := &convert.Step{InputPath: path, From: "hwpx", To: "pdf", Preset: presets.New("pdf", "pdf"), Output: fixedOutput(out), WorkDir: dir}
	if _, err := New().Convert(context.Background(), step, nil); err != nil {
		t.Fatal(err)
	}
	if !written(out) {
		t.Error("no PDF")
	}
}
