package hwpx

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/hwp/xmltree"
)

// DOCX → HWPX with the built-in reader and writer: layout that Pandoc-based conversion loses (page setup,
// headers/footers, columns, sections, text boxes, shapes, floating pictures, formatting, table borders) must arrive
// in the OWPML structure 한글 uses.

func TestDocxPageSetupHeadersFootersAndPageNumbers(t *testing.T) {
	b := newDocx()
	header := b.header(wP(wR("머리말 제목"), `<w:jc w:val="right"/>`))
	footer := b.footer(wP(wR("- ")+wField("PAGE", "1")+wR(" / ")+wField("NUMPAGES", "3")+wR(" -"), `<w:jc w:val="center"/>`))
	b.add(wP(wR("가로 방향 본문")))
	b.sectPr = fmt.Sprintf(`<w:headerReference w:type="default" r:id="%s"/><w:footerReference w:type="default" r:id="%s"/>`, header, footer) +
		`<w:pgSz w:w="16838" w:h="11906" w:orient="landscape"/>` +
		`<w:pgMar w:top="1985" w:right="1134" w:bottom="1418" w:left="1134" w:header="851" w:footer="851" w:gutter="0"/><w:titlePg/>`
	hwpx := docxToHwpx(t, b, "page-setup")
	section := section0(t, hwpx)

	pagePr := section.FirstDescendant("pagePr")
	eq(t, "landscape", pagePr.A("landscape"), "NARROWLY")
	eq(t, "width", atoi(t, pagePr, "width"), 11906*5)
	eq(t, "height", atoi(t, pagePr, "height"), 16838*5)
	margin := pagePr.Child("margin")
	eq(t, "top", atoi(t, margin, "top"), 851*5)
	eq(t, "header", atoi(t, margin, "header"), (1985-851)*5)
	eq(t, "bottom", atoi(t, margin, "bottom"), 851*5)
	eq(t, "footer", atoi(t, margin, "footer"), (1418-851)*5)
	eq(t, "left", atoi(t, margin, "left"), 1134*5)

	headerControl := section.FirstDescendant("header")
	eq(t, "applyPageType", headerControl.A("applyPageType"), "BOTH")
	if !strings.Contains(headerControl.Value(), "머리말 제목") {
		t.Error("header text missing")
	}
	footerControl := section.FirstDescendant("footer")
	var numbers []string
	for _, a := range footerControl.DescendantsNamed("autoNum") {
		numbers = append(numbers, a.A("numType"))
	}
	eq(t, "page numbers", strings.Join(numbers, ","), "PAGE,TOTAL_PAGE")
	var texts strings.Builder
	for _, tt := range footerControl.DescendantsNamed("t") {
		texts.WriteString(tt.Value())
	}
	if strings.Contains(texts.String(), "3") {
		t.Error("cached field results are kept")
	}
	visibility := section.FirstDescendant("visibility")
	eq(t, "hideFirstHeader", visibility.A("hideFirstHeader"), "1")
	eq(t, "hideFirstFooter", visibility.A("hideFirstFooter"), "1")
}

func TestDocxColumnsChangeWithinASectionAndNewPaperStartsANewSection(t *testing.T) {
	b := newDocx().
		add(wP(wR("한 단 본문"))).
		add(wP(wR("첫 구역 끝"), "<w:sectPr>"+defaultDocxPage+"</w:sectPr>")).
		add(wP(wR("두 단 왼쪽"))).
		add(wP(wR("두 단 끝"), `<w:sectPr><w:type w:val="continuous"/>`+defaultDocxPage+`<w:cols w:num="2" w:space="720" w:sep="1"/></w:sectPr>`)).
		add(wP(wR("레터 용지")))
	b.sectPr = `<w:type w:val="nextPage"/><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" w:gutter="0"/>`
	hwpx := docxToHwpx(t, b, "sections")
	first := section0(t, hwpx)
	change := paragraphWith(t, first, "두 단 왼쪽").FirstDescendant("colPr")
	eq(t, "colCount", atoi(t, change, "colCount"), 2)
	eq(t, "sameGap", atoi(t, change, "sameGap"), 3600)
	if change.Child("colLine") == nil {
		t.Error("no separator line")
	}
	eq(t, "first colPr", atoi(t, first.FirstDescendant("colPr"), "colCount"), 1)

	second := hwpxPart(t, hwpx, "Contents/section1.xml")
	pagePr := second.FirstDescendant("pagePr")
	eq(t, "landscape", pagePr.A("landscape"), "WIDELY")
	eq(t, "width", atoi(t, pagePr, "width"), 61200)
	eq(t, "height", atoi(t, pagePr, "height"), 79200)
	if !strings.Contains(second.Value(), "레터 용지") {
		t.Error("second section text missing")
	}
}

func TestDocxParagraphAndCharacterFormattingListsAndDocumentGrid(t *testing.T) {
	b := newDocx()
	b.numbering = `<w:abstractNum w:abstractNumId="1">
  <w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/><w:pPr><w:ind w:left="400" w:hanging="400"/></w:pPr></w:lvl>
  <w:lvl w:ilvl="1"><w:start w:val="1"/><w:numFmt w:val="ganada"/><w:lvlText w:val="%2)"/><w:pPr><w:ind w:left="800" w:hanging="400"/></w:pPr></w:lvl>
</w:abstractNum>
<w:abstractNum w:abstractNumId="2"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:lvlText w:val="&#xF0B7;"/><w:rPr><w:rFonts w:ascii="Symbol" w:hAnsi="Symbol"/></w:rPr></w:lvl></w:abstractNum>
<w:num w:numId="1"><w:abstractNumId w:val="1"/></w:num>
<w:num w:numId="2"><w:abstractNumId w:val="2"/></w:num>`
	b.sectPr = defaultDocxPage + `<w:docGrid w:type="lines" w:linePitch="360"/>`
	b.add(wP(wR("가운데 제목"), `<w:pStyle w:val="Heading1"/><w:jc w:val="center"/>`)).
		add(wP(wR("굵은 빨강", `<w:b/><w:color w:val="FF0000"/><w:sz w:val="28"/><w:rFonts w:ascii="Arial" w:hAnsi="Arial" w:eastAsia="굴림"/>`)+
			wR(" 형광펜", `<w:highlight w:val="yellow"/>`)+wR(" 무늬", `<w:shd w:val="pct25" w:color="auto" w:fill="auto"/>`),
			`<w:ind w:left="720" w:hanging="360"/><w:spacing w:before="240" w:after="120"/>`)).
		add(wP(wR("첫 항목"), `<w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr>`)).
		add(wP(wR("하위 항목"), `<w:numPr><w:ilvl w:val="1"/><w:numId w:val="1"/></w:numPr>`)).
		add(wP(wR("글머리 항목"), `<w:numPr><w:ilvl w:val="0"/><w:numId w:val="2"/></w:numPr>`)).
		add(wP(wR("2") + wR("016-06-15")))
	hwpx := docxToHwpx(t, b, "formatting")
	section := section0(t, hwpx)
	head := hwpxPart(t, hwpx, "Contents/header.xml")
	byID := func(kind, id string) *xmltree.Element {
		for _, e := range head.DescendantsNamed(kind) {
			if e.A("id") == id {
				return e
			}
		}
		t.Fatalf("no %s %s", kind, id)
		return nil
	}
	paraPr := func(p *xmltree.Element) *xmltree.Element { return byID("paraPr", p.A("paraPrIDRef")) }
	charPr := func(r *xmltree.Element) *xmltree.Element { return byID("charPr", r.A("charPrIDRef")) }
	marginOf := func(pp *xmltree.Element, branch, name string) int {
		return atoi(t, pp.FirstDescendant(branch).FirstDescendant(name), "value")
	}

	title := paragraphWith(t, section, "가운데 제목")
	var headingStyle *xmltree.Element
	for _, s := range head.DescendantsNamed("style") {
		if s.A("engName") == "Heading 1" {
			headingStyle = s
		}
	}
	eq(t, "heading style", title.A("styleIDRef"), headingStyle.A("id"))
	eq(t, "align", paraPr(title).Child("align").A("horizontal"), "CENTER")

	red := charPr(runWith(t, section, "굵은 빨강"))
	eq(t, "height", red.A("height"), "1400")
	eq(t, "colour", red.A("textColor"), "#FF0000")
	if red.Child("bold") == nil {
		t.Error("not bold")
	}
	font := func(language string) string {
		for _, ff := range head.DescendantsNamed("fontface") {
			if ff.A("lang") == language {
				for _, f := range ff.Children("font") {
					if f.A("id") == red.Child("fontRef").A(strings.ToLower(language)) {
						return f.A("face")
					}
				}
			}
		}
		return ""
	}
	eq(t, "latin font", font("LATIN"), "Arial")
	eq(t, "hangul font", font("HANGUL"), "굴림")
	eq(t, "highlight", charPr(runWith(t, section, "형광펜")).A("shadeColor"), "#FFFF00")
	eq(t, "pattern", charPr(runWith(t, section, "무늬")).A("shadeColor"), "#BFBFBF")

	// Hanging indent (Word: text at 0.5", first line at 0.25"): 한글 keeps the first line at "left".
	indented := paraPr(paragraphWith(t, section, "굵은 빨강"))
	eq(t, "left", marginOf(indented, "case", "left"), 1800)
	eq(t, "intent", marginOf(indented, "case", "intent"), -1800)
	eq(t, "prev", marginOf(indented, "case", "prev"), 1200)
	eq(t, "next", marginOf(indented, "case", "next"), 600)
	eq(t, "default left", marginOf(indented, "default", "left"), 3600)
	eq(t, "default intent", marginOf(indented, "default", "intent"), -3600)

	// Word's 18 pt document grid: 10 pt lines become 18 pt, 14 pt lines need two grid lines.
	gridLine := paraPr(paragraphWith(t, section, "첫 항목")).FirstDescendant("case").Child("lineSpacing")
	eq(t, "grid type", gridLine.A("type"), "AT_LEAST")
	eq(t, "grid value", atoi(t, gridLine, "value"), 1800)
	eq(t, "big line", atoi(t, indented.FirstDescendant("case").Child("lineSpacing"), "value"), 3600)

	item := paraPr(paragraphWith(t, section, "첫 항목")).Child("heading")
	sub := paraPr(paragraphWith(t, section, "하위 항목")).Child("heading")
	eq(t, "type", item.A("type"), "NUMBER")
	eq(t, "level", atoi(t, item, "level"), 0)
	eq(t, "sub level", atoi(t, sub, "level"), 1)
	numbering := byID("numbering", item.A("idRef"))
	heads := numbering.Children("paraHead")
	eq(t, "level 1", heads[0].A("numFormat")+" "+heads[0].Value(), "DIGIT ^1.")
	eq(t, "level 2", heads[1].A("numFormat")+" "+heads[1].Value(), "HANGUL_SYLLABLE ^2)")
	bullet := paraPr(paragraphWith(t, section, "글머리 항목")).Child("heading")
	eq(t, "bullet", byID("numbering", bullet.A("idRef")).Children("paraHead")[0].Value(), "•")

	found := false
	for _, tt := range section.DescendantsNamed("t") {
		found = found || tt.Value() == "2016-06-15"
	}
	if !found {
		t.Error("runs formatted alike are not one text")
	}
}

func TestDocxTablesKeepMergedCellsBordersAndShading(t *testing.T) {
	cell := func(content, tcPr string) string {
		return "<w:tc><w:tcPr>" + tcPr + "</w:tcPr>" + wP(wR(content)) + "</w:tc>"
	}
	b := newDocx().add(`<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/><w:jc w:val="center"/></w:tblPr>` +
		`<w:tblGrid><w:gridCol w:w="2000"/><w:gridCol w:w="3000"/><w:gridCol w:w="2000"/></w:tblGrid>` +
		"<w:tr><w:trPr><w:tblHeader/></w:trPr>" + cell("가로 병합", `<w:gridSpan w:val="2"/><w:shd w:val="pct10" w:color="auto" w:fill="auto"/>`) +
		cell("세로 병합", `<w:vMerge w:val="restart"/><w:vAlign w:val="center"/>`) + "</w:tr>" +
		"<w:tr>" + cell("A", "") + cell("B", "") + cell("", "<w:vMerge/>") + "</w:tr>" +
		"<w:tr>" + cell("C", `<w:tcBorders><w:bottom w:val="double" w:sz="12" w:color="FF0000"/></w:tcBorders>`) + cell("D", `<w:shd w:val="clear" w:color="auto" w:fill="FFF2CC"/>`) + cell("E", "") + "</w:tr>" +
		"</w:tbl>" + wP(""))
	hwpx := docxToHwpx(t, b, "table")
	table := section0(t, hwpx).FirstDescendant("tbl")
	eq(t, "rows", atoi(t, table, "rowCnt"), 3)
	eq(t, "cols", atoi(t, table, "colCnt"), 3)
	eq(t, "repeatHeader", table.A("repeatHeader"), "1")
	eq(t, "align", table.Child("pos").A("horzAlign"), "CENTER")

	type cellInfo struct {
		text                              string
		row, col, rowSpan, colSpan, width int
		borderFill                        *xmltree.Element
		valign                            string
	}
	var cells []cellInfo
	covered := map[[2]int]int{}
	for _, tc := range table.DescendantsNamed("tc") {
		c := cellInfo{text: tc.Child("subList").Value(), row: atoi(t, tc.Child("cellAddr"), "rowAddr"), col: atoi(t, tc.Child("cellAddr"), "colAddr"),
			rowSpan: atoi(t, tc.Child("cellSpan"), "rowSpan"), colSpan: atoi(t, tc.Child("cellSpan"), "colSpan"), width: atoi(t, tc.Child("cellSz"), "width"),
			borderFill: headItem(t, hwpx, "borderFill", tc.A("borderFillIDRef")), valign: tc.Child("subList").A("vertAlign")}
		cells = append(cells, c)
		for r := c.row; r < c.row+c.rowSpan; r++ {
			for col := c.col; col < c.col+c.colSpan; col++ {
				covered[[2]int{r, col}]++
			}
		}
	}
	if len(covered) != 9 {
		t.Errorf("covered %d grid positions", len(covered))
	}
	for k, n := range covered {
		if n != 1 {
			t.Errorf("%v covered %d times", k, n)
		}
	}
	find := func(text string) cellInfo {
		for _, c := range cells {
			if c.text == text {
				return c
			}
		}
		t.Fatalf("no cell %q", text)
		return cellInfo{}
	}
	wide := find("가로 병합")
	eq(t, "colspan", wide.colSpan, 2)
	eq(t, "width", wide.width, 25000)
	eq(t, "pct10", wide.borderFill.FirstDescendant("winBrush").A("faceColor"), "#E6E6E6")
	tall := find("세로 병합")
	eq(t, "rowspan", tall.rowSpan, 2)
	eq(t, "valign", tall.valign, "CENTER")
	bottom := find("C").borderFill.Child("bottomBorder")
	eq(t, "double", bottom.A("type"), "DOUBLE_SLIM")
	eq(t, "red", bottom.A("color"), "#FF0000")
	eq(t, "grid", find("A").borderFill.Child("leftBorder").A("type"), "SOLID")
	eq(t, "fill", find("D").borderFill.FirstDescendant("winBrush").A("faceColor"), "#FFF2CC")
}

func TestDocxTextBoxesShapesAndFloatingPictures(t *testing.T) {
	b := newDocx()
	picture := b.image(pngBytes(120, 60))
	const column = `<wp:positionH relativeFrom="column"><wp:posOffset>0</wp:posOffset></wp:positionH>`
	const paragraph = `<wp:positionV relativeFrom="paragraph"><wp:posOffset>0</wp:posOffset></wp:positionV>`
	shape := func(geometry, fill, line string, cx, cy int64, text string) string {
		s := fmt.Sprintf(`<wps:wsp><wps:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="%s"/>%s%s</wps:spPr>`, cx, cy, geometry, fill, line)
		if text != "" {
			s += "<wps:txbx><w:txbxContent>" + wP(wR(text)) + "</w:txbxContent></wps:txbx>"
		}
		return s + `<wps:bodyPr anchor="ctr"/></wps:wsp>`
	}
	b.add(wP(wAnchor(pictureURI, wPicture(picture, 914400, 457200), 914400, 457200,
		`<wp:positionH relativeFrom="page"><wp:posOffset>914400</wp:posOffset></wp:positionH>`, paragraph, "") + wR("그림 옆에 흐르는 글"))).
		add(wP(wInlinePicture(picture, 457200, 457200))).
		add(wP(wAnchor(pictureURI, wPicture(picture, 457200, 457200), 457200, 457200,
			`<wp:positionH relativeFrom="margin"><wp:posOffset>-635000</wp:posOffset></wp:positionH>`,
			`<wp:positionV relativeFrom="paragraph"><wp:posOffset>-127000</wp:posOffset></wp:positionV>`, "") + wR("여백으로 나간 그림"))).
		add(wP(wAnchor(shapeURI,
			shape("rect", `<a:solidFill><a:srgbClr val="FFF2CC"/></a:solidFill>`, `<a:ln w="12700"><a:solidFill><a:srgbClr val="C00000"/></a:solidFill></a:ln>`, 1828800, 914400, "글상자 안의 글"),
			1828800, 914400, `<wp:positionH relativeFrom="margin"><wp:align>center</wp:align></wp:positionH>`,
			`<wp:positionV relativeFrom="page"><wp:posOffset>3600000</wp:posOffset></wp:positionV>`, "<wp:wrapTopAndBottom/>"))).
		add(wP(wAnchor(shapeURI, shape("line", "", `<a:ln w="25400"><a:solidFill><a:srgbClr val="4F81BD"/></a:solidFill></a:ln>`, 3000000, 0, ""),
			3000000, 0, column, paragraph, "<wp:wrapNone/>"))).
		add(wP(wAnchor(shapeURI,
			shape("rect", `<a:gradFill><a:gsLst><a:gs pos="0"><a:srgbClr val="99CCFF"/></a:gs><a:gs pos="100000"><a:srgbClr val="FFFFFF"/></a:gs></a:gsLst><a:lin ang="0"/></a:gradFill>`, "<a:ln><a:noFill/></a:ln>", 2000000, 180000, ""),
			2000000, 180000, column, paragraph, "<wp:wrapNone/>")))
	hwpx := docxToHwpx(t, b, "objects")
	if strings.Count(zipEntry(t, hwpx, "Contents/content.hpf"), "BinData/") != 1 {
		t.Error("one picture used three times is not stored once")
	}
	section := section0(t, hwpx)
	pictures := section.DescendantsNamed("pic")
	inline, floatingFound, outsideFound := 0, false, false
	for _, p := range pictures {
		pos := p.Child("pos")
		switch {
		case pos.A("treatAsChar") == "1":
			inline++
		case atoi(t, pos, "horzOffset") == 7200:
			floatingFound = true
			eq(t, "wrap", p.A("textWrap"), "SQUARE")
			eq(t, "relTo", pos.A("horzRelTo"), "PAPER")
		case pos.A("horzRelTo") == "PAPER":
			outsideFound = true
			eq(t, "re-anchored offset", atoi(t, pos, "horzOffset"), 1440*5-5000)
			eq(t, "vertOffset", atoi(t, pos, "vertOffset"), 0)
		}
	}
	if inline != 1 || !floatingFound || !outsideFound {
		t.Errorf("pictures: inline %d floating %v outside %v", inline, floatingFound, outsideFound)
	}
	for _, p := range section.DescendantsNamed("pos") {
		if atoi(t, p, "horzOffset") < 0 || atoi(t, p, "vertOffset") < 0 {
			t.Error("negative offset")
		}
	}
	var textBox, gradientRect *xmltree.Element
	for _, r := range section.DescendantsNamed("rect") {
		if r.Child("drawText") != nil {
			textBox = r
		} else {
			gradientRect = r
		}
	}
	if !strings.Contains(textBox.Child("drawText").Value(), "글상자 안의 글") {
		t.Error("text box text")
	}
	eq(t, "valign", textBox.FirstDescendant("subList").A("vertAlign"), "CENTER")
	eq(t, "fill", textBox.FirstDescendant("winBrush").A("faceColor"), "#FFF2CC")
	eq(t, "line colour", textBox.Child("lineShape").A("color"), "#C00000")
	eq(t, "line width", atoi(t, textBox.Child("lineShape"), "width"), 100)
	position := textBox.Child("pos")
	eq(t, "horzRelTo", position.A("horzRelTo"), "PAGE")
	eq(t, "horzAlign", position.A("horzAlign"), "CENTER")
	eq(t, "vertRelTo", position.A("vertRelTo"), "PAPER")
	eq(t, "vertOffset", atoi(t, position, "vertOffset"), 28346)
	eq(t, "textWrap", textBox.A("textWrap"), "TOP_AND_BOTTOM")
	line := section.FirstDescendant("line")
	eq(t, "line end", atoi(t, line.Child("endPt"), "x"), 23622)
	eq(t, "line colour", line.Child("lineShape").A("color"), "#4F81BD")
	var colors []string
	for _, c := range gradientRect.FirstDescendant("gradation").Children("color") {
		colors = append(colors, c.A("value"))
	}
	eq(t, "gradient", strings.Join(colors, ","), "#99CCFF,#FFFFFF")
}

func TestDocxNotesLinksBookmarksAndPageBreaks(t *testing.T) {
	b := newDocx()
	b.footnotes(`<w:footnote w:id="1"><w:p><w:r><w:footnoteRef/></w:r>` + wR(" 각주 설명") + "</w:p></w:footnote>")
	url := b.hyperlink("https://example.com/a?b=1")
	b.add(wP(`<w:bookmarkStart w:id="0" w:name="intro"/>`+wR("소개")+`<w:bookmarkEnd w:id="0"/>`, `<w:pStyle w:val="Heading1"/>`)).
		add(wP(wR("본문") + `<w:r><w:footnoteReference w:id="1"/></w:r>` +
			`<w:hyperlink r:id="` + url + `">` + wR("웹 링크", `<w:rStyle w:val="Hyperlink"/>`) + "</w:hyperlink>" +
			`<w:hyperlink w:anchor="intro">` + wR("소개로 이동") + "</w:hyperlink>")).
		add(wP(wR("앞 쪽") + `<w:r><w:br w:type="page"/></w:r>` + wR("다음 쪽")))
	hwpx := docxToHwpx(t, b, "notes-links")
	section := section0(t, hwpx)

	note := section.FirstDescendant("footNote")
	firstRun := note.FirstDescendant("p").Children("run")[0]
	eq(t, "note number", firstRun.FirstDescendant("autoNum").A("numType"), "FOOTNOTE")
	if !strings.Contains(note.Value(), "각주 설명") {
		t.Error("note text")
	}
	parameter := func(field *xmltree.Element, name string) string {
		for _, p := range field.Descendants() {
			if p.A("name") == name {
				return p.Value()
			}
		}
		return ""
	}
	var web, bookmark *xmltree.Element
	for _, l := range section.DescendantsNamed("fieldBegin") {
		switch parameter(l, "Category") {
		case "HWPHYPERLINK_TYPE_URL":
			web = l
		case "HWPHYPERLINK_TYPE_HWP":
			bookmark = l
		}
	}
	eq(t, "command", parameter(web, "Command"), `https\://example.com/a\?b=1;1;0;0;`)
	eq(t, "path", parameter(web, "Path"), "https://example.com/a?b=1")
	eq(t, "bookmark command", parameter(bookmark, "Command"), "?intro;0;0;0;")
	eq(t, "bookmark", section.FirstDescendant("bookmark").A("name"), "intro")
	eq(t, "field ends", len(section.DescendantsNamed("fieldEnd")), 2)
	eq(t, "page break", paragraphWith(t, section, "다음 쪽").A("pageBreak"), "1")
	eq(t, "no page break", paragraphWith(t, section, "앞 쪽").A("pageBreak"), "0")
}
