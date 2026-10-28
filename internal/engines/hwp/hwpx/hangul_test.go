package hwpx

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/doc"
)

// A two-section package with the features and variants of files saved by 한글: hp:switch branches (exact and doubled
// values, a 2016 heading), bullets, outline numbering, automatic page numbers, fields, highlighter, tracked changes,
// special spaces, a caption, nested and merged cells, pictures (one without a manifest entry, one missing), a group,
// equation, compose, ruby text, notes, a chart with an OLE fallback, and in the second section other prefixes.
func hangulStylePackage(t *testing.T, folder string) string {
	const ns = `xmlns:ha="http://www.hancom.co.kr/hwpml/2011/app" xmlns:hp="http://www.hancom.co.kr/hwpml/2011/paragraph" ` +
		`xmlns:hp10="http://www.hancom.co.kr/hwpml/2016/paragraph" xmlns:hs="http://www.hancom.co.kr/hwpml/2011/section" ` +
		`xmlns:hc="http://www.hancom.co.kr/hwpml/2011/core" xmlns:hh="http://www.hancom.co.kr/hwpml/2011/head"`
	const caseOpen = `<hp:case hp:required-namespace="http://www.hancom.co.kr/hwpml/2016/HwpUnitChar">`
	border := func(kind, width, color, fill string) string {
		return `<hh:slash type="NONE" Crooked="0" isCounter="0"/><hh:backSlash type="NONE" Crooked="0" isCounter="0"/>` + kind + width + color + fill
	}
	charPr := func(id, attributes, fontRef, spacing, flags string) string {
		return fmt.Sprintf(`<hh:charPr id="%s" %s useFontSpace="0" useKerning="0" symMark="NONE" borderFillIDRef="1">`, id, attributes) +
			fmt.Sprintf(`<hh:fontRef hangul="%s" latin="%s" hanja="0" japanese="0" other="0" symbol="0" user="0"/>`, fontRef, fontRef) +
			`<hh:ratio hangul="100" latin="100" hanja="100" japanese="100" other="100" symbol="100" user="100"/>` +
			fmt.Sprintf(`<hh:spacing hangul="%s" latin="%s" hanja="0" japanese="0" other="0" symbol="0" user="0"/>`, spacing, spacing) +
			`<hh:relSz hangul="100" latin="100" hanja="100" japanese="100" other="100" symbol="100" user="100"/>` +
			`<hh:offset hangul="0" latin="0" hanja="0" japanese="0" other="0" symbol="0" user="0"/>` + flags + "</hh:charPr>"
	}
	margins := func(intent, left, right, prev, next int) string {
		return fmt.Sprintf(`<hh:margin><hc:intent value="%d" unit="HWPUNIT"/><hc:left value="%d" unit="HWPUNIT"/><hc:right value="%d" unit="HWPUNIT"/><hc:prev value="%d" unit="HWPUNIT"/><hc:next value="%d" unit="HWPUNIT"/></hh:margin>`, intent, left, right, prev, next)
	}
	paraPr := func(id, tabPr, align, heading, breaks, spacing string) string {
		return fmt.Sprintf(`<hh:paraPr id="%s" tabPrIDRef="%s" condense="0" fontLineHeight="0" snapToGrid="1" suppressLineNumbers="0" checked="0">`, id, tabPr) +
			fmt.Sprintf(`<hh:align horizontal="%s" vertical="BASELINE"/>%s`, align, heading) +
			fmt.Sprintf(`<hh:breakSetting breakLatinWord="KEEP_WORD" breakNonLatinWord="KEEP_WORD" %s lineWrap="BREAK"/>`, breaks) +
			`<hh:autoSpacing eAsianEng="0" eAsianNum="0"/>` + spacing + `<hh:border borderFillIDRef="1" offsetLeft="0" offsetRight="0" offsetTop="0" offsetBottom="0" connect="0" ignoreMargin="0"/></hh:paraPr>`
	}
	sw := func(exact, doubled string) string {
		return "<hp:switch>" + caseOpen + exact + "</hp:case><hp:default>" + doubled + "</hp:default></hp:switch>"
	}
	const noBreaks = `widowOrphan="0" keepWithNext="0" keepLines="0" pageBreakBefore="0"`
	const percent160 = `<hh:lineSpacing type="PERCENT" value="160" unit="HWPUNIT"/>`
	heading := func(kind string, id, level int) string {
		return fmt.Sprintf(`<hh:heading type="%s" idRef="%d" level="%d"/>`, kind, id, level)
	}
	paraHead := func(level int, format, text string, start int) string {
		return fmt.Sprintf(`<hh:paraHead start="%d" level="%d" align="LEFT" useInstWidth="1" autoIndent="1" widthAdjust="0" textOffsetType="PERCENT" textOffset="50" numFormat="%s" charPrIDRef="4294967295" checkable="0">%s</hh:paraHead>`, start, level, format, text)
	}

	header := "<hh:head " + ns + ` version="1.4" secCnt="2"><hh:beginNum page="1" footnote="1" endnote="1" pic="1" tbl="1" equation="1"/><hh:refList>` +
		`<hh:fontfaces itemCnt="2">` +
		`<hh:fontface lang="HANGUL" fontCnt="2"><hh:font id="0" face="함초롬바탕" type="TTF" isEmbedded="0"><hh:typeInfo familyType="FCAT_GOTHIC" weight="6" proportion="4" contrast="0" strokeVariation="1" armStyle="1" letterform="1" midline="1" xHeight="1"/></hh:font><hh:font id="1" face="맑은 고딕" type="TTF" isEmbedded="0"/></hh:fontface>` +
		`<hh:fontface lang="LATIN" fontCnt="2"><hh:font id="0" face="함초롬바탕" type="TTF" isEmbedded="0"/><hh:font id="1" face="Times New Roman" type="TTF" isEmbedded="0"/></hh:fontface>` +
		`</hh:fontfaces><hh:borderFills itemCnt="2">` +
		`<hh:borderFill id="1" threeD="0" shadow="0" centerLine="NONE" breakCellSeparateLine="0">` +
		border(`<hh:leftBorder type="NONE" width="0.1 mm" color="#000000"/><hh:rightBorder type="NONE" width="0.1 mm" color="#000000"/>`,
			`<hh:topBorder type="NONE" width="0.1 mm" color="#000000"/><hh:bottomBorder type="NONE" width="0.1 mm" color="#000000"/>`,
			`<hh:diagonal type="SOLID" width="0.1 mm" color="#000000"/>`, "") + "</hh:borderFill>" +
		`<hh:borderFill id="2" threeD="0" shadow="0" centerLine="NONE" breakCellSeparateLine="0">` +
		border(`<hh:leftBorder type="SOLID" width="0.12 mm" color="#000000"/><hh:rightBorder type="DOUBLE_SLIM" width="0.5 mm" color="#FF0000"/>`,
			`<hh:topBorder type="DASH" width="0.2 mm" color="#0000FF"/><hh:bottomBorder type="DOT" width="0.12mm" color="#000000"/>`,
			`<hh:diagonal type="SOLID" width="0.1 mm" color="#000000"/>`,
			`<hc:fillBrush><hc:winBrush faceColor="#FFFF00" hatchColor="#000000" alpha="0"/></hc:fillBrush>`) + "</hh:borderFill>" +
		`</hh:borderFills><hh:charProperties itemCnt="2">` +
		charPr("0", `height="1000" textColor="#000000" shadeColor="none"`, "0", "0",
			`<hh:underline type="NONE" shape="SOLID" color="#000000"/><hh:strikeout shape="NONE" color="#000000"/><hh:outline type="NONE"/><hh:shadow type="NONE" color="#B2B2B2" offsetX="10" offsetY="10"/>`) +
		charPr("1", `height="1400" textColor="#0000ff" shadeColor="#FFFF00"`, "1", "-5",
			`<hh:bold/><hh:italic/><hh:underline type="BOTTOM" shape="SOLID" color="#0000FF"/><hh:strikeout shape="SOLID" color="#0000FF"/><hh:outline type="NONE"/><hh:shadow type="NONE" color="#B2B2B2" offsetX="10" offsetY="10"/><hh:supscript/>`) +
		`</hh:charProperties><hh:tabProperties itemCnt="2"><hh:tabPr id="0" autoTabLeft="0" autoTabRight="0"/><hh:tabPr id="1" autoTabLeft="0" autoTabRight="0">` +
		sw(`<hh:tabItem pos="20000" type="CENTER" leader="DASH" unit="HWPUNIT"/>`, `<hh:tabItem pos="40000" type="CENTER" leader="DASH"/>`) +
		`</hh:tabPr></hh:tabProperties><hh:numberings itemCnt="2">` +
		`<hh:numbering id="1" start="0">` + paraHead(1, "DIGIT", "^1.", 1) + paraHead(2, "HANGUL_SYLLABLE", "^2.", 1) + "</hh:numbering>" +
		`<hh:numbering id="2" start="1">` + paraHead(1, "DIGIT", "^1)", 1) + paraHead(2, "ROMAN_SMALL", "(^2)", 3) + "</hh:numbering>" +
		"</hh:numberings><hh:bullets itemCnt=\"1\"><hh:bullet id=\"1\" char=\"\" useImage=\"0\">" +
		`<hh:paraHead level="0" align="LEFT" useInstWidth="0" autoIndent="1" widthAdjust="0" textOffsetType="PERCENT" textOffset="50" numFormat="DIGIT" charPrIDRef="4294967295" checkable="0"/></hh:bullet></hh:bullets>` +
		`<hh:paraProperties itemCnt="6">` +
		paraPr("0", "0", "JUSTIFY", heading("NONE", 0, 0), noBreaks, sw(margins(0, 0, 0, 0, 0)+percent160, margins(0, 0, 0, 0, 0)+percent160)) +
		paraPr("1", "1", "LEFT", heading("NONE", 0, 0), `widowOrphan="1" keepWithNext="1" keepLines="1" pageBreakBefore="0"`,
			sw(margins(-2000, 1000, 500, 400, 600)+`<hh:lineSpacing type="FIXED" value="1800" unit="HWPUNIT"/>`,
				margins(-4000, 2000, 1000, 800, 1200)+`<hh:lineSpacing type="FIXED" value="3600" unit="HWPUNIT"/>`)) +
		// An older paragraph shape without a switch: lengths in the doubled scale.
		paraPr("2", "0", "LEFT", heading("OUTLINE", 0, 0), noBreaks, margins(0, 2000, 0, 0, 0)+`<hh:lineSpacing type="PERCENT" value="150"/>`) +
		paraPr("3", "0", "LEFT", heading("BULLET", 1, 0), noBreaks, sw(margins(0, 0, 0, 0, 0)+percent160, margins(0, 0, 0, 0, 0)+percent160)) +
		paraPr("4", "0", "LEFT",
			`<hp:switch><hp:case hp:required-namespace="http://www.hancom.co.kr/hwpml/2016/paragraph">`+heading("NUMBER", 2, 1)+"</hp:case><hp:default>"+heading("NONE", 0, 0)+"</hp:default></hp:switch>",
			`widowOrphan="0" keepWithNext="0" keepLines="0" pageBreakBefore="1"`, sw(margins(0, 0, 0, 0, 0)+percent160, margins(0, 0, 0, 0, 0)+percent160)) +
		paraPr("5", "0", "LEFT", heading("NONE", 0, 0), noBreaks,
			sw(margins(0, 0, 0, 0, 0)+`<hh:lineSpacing type="BETWEEN_LINES" value="500" unit="HWPUNIT"/>`, margins(0, 0, 0, 0, 0)+`<hh:lineSpacing type="BETWEEN_LINES" value="1000" unit="HWPUNIT"/>`)) +
		`</hh:paraProperties><hh:styles itemCnt="2">` +
		`<hh:style id="0" type="PARA" name="바탕글" engName="Normal" paraPrIDRef="0" charPrIDRef="0" nextStyleIDRef="0" langID="1042" lockForm="0"/>` +
		`<hh:style id="1" type="PARA" name="개요 1" engName="Outline 1" paraPrIDRef="2" charPrIDRef="0" nextStyleIDRef="1" langID="1042" lockForm="0"/>` +
		`</hh:styles></hh:refList><hh:compatibleDocument targetProgram="HWP201X"><hh:layoutCompatibility/></hh:compatibleDocument></hh:head>`

	p := func(paraPr, runs string, columnBreak bool) string {
		return fmt.Sprintf(`<hp:p id="0" paraPrIDRef="%s" styleIDRef="0" pageBreak="0" columnBreak="%s" merged="0">%s`, paraPr, b01(columnBreak), runs) +
			`<hp:linesegarray><hp:lineseg textpos="0" vertpos="0" vertsize="1000" textheight="1000" baseline="850" spacing="600" horzpos="0" horzsize="42520" flags="393216"/></hp:linesegarray></hp:p>`
	}
	r := func(content, charPr string) string { return fmt.Sprintf(`<hp:run charPrIDRef="%s">%s</hp:run>`, charPr, content) }
	tt := func(text string) string { return "<hp:t>" + text + "</hp:t>" }
	subList := func(paragraphs, vertAlign string) string {
		return fmt.Sprintf(`<hp:subList id="" textDirection="HORIZONTAL" lineWrap="BREAK" vertAlign="%s" linkListIDRef="0" linkListNextIDRef="0" textWidth="0" textHeight="0" hasTextRef="0" hasNumRef="0">%s</hp:subList>`, vertAlign, paragraphs)
	}
	cell := func(content string, column, row, colSpan, rowSpan, width, height int, borderFill string, header bool, vertAlign string) string {
		return fmt.Sprintf(`<hp:tc name="" header="%s" hasMargin="0" protect="0" editable="0" dirty="0" borderFillIDRef="%s">%s`, b01(header), borderFill, subList(content, vertAlign)) +
			fmt.Sprintf(`<hp:cellAddr colAddr="%d" rowAddr="%d"/><hp:cellSpan colSpan="%d" rowSpan="%d"/><hp:cellSz width="%d" height="%d"/>`, column, row, colSpan, rowSpan, width, height) +
			`<hp:cellMargin left="141" right="141" top="141" bottom="141"/></hp:tc>`
	}
	pos := func(inline bool, x int, y string, horzAlign string) string {
		return fmt.Sprintf(`<hp:pos treatAsChar="%s" affectLSpacing="0" flowWithText="1" allowOverlap="0" holdAnchorAndSO="0" vertRelTo="PARA" horzRelTo="COLUMN" vertAlign="TOP" horzAlign="%s" vertOffset="%s" horzOffset="%d"/>`, b01(inline), horzAlign, y, x)
	}
	picture := func(item string, width, height int, pos string) string {
		return `<hp:pic id="5" zOrder="0" numberingType="PICTURE" textWrap="SQUARE" textFlow="BOTH_SIDES" lock="0" dropcapstyle="None" href="" groupLevel="0" instid="6" reverse="0">` +
			fmt.Sprintf(`<hp:offset x="0" y="0"/><hp:orgSz width="%d" height="%d"/><hp:curSz width="%d" height="%d"/><hp:flip horizontal="0" vertical="0"/>`, width, height, width, height) +
			fmt.Sprintf(`<hp:imgRect><hc:pt0 x="0" y="0"/><hc:pt1 x="%d" y="0"/><hc:pt2 x="%d" y="%d"/><hc:pt3 x="0" y="%d"/></hp:imgRect>`, width, width, height, height) +
			`<hp:imgClip left="0" right="0" top="0" bottom="0"/><hp:inMargin left="0" right="0" top="0" bottom="0"/><hp:imgDim dimwidth="0" dimheight="0"/>` +
			fmt.Sprintf(`<hc:img binaryItemIDRef="%s" bright="0" contrast="0" effect="REAL_PIC" alpha="0"/><hp:effects/>`, item) +
			fmt.Sprintf(`<hp:sz width="%d" widthRelTo="ABSOLUTE" height="%d" heightRelTo="ABSOLUTE" protect="0"/>%s<hp:outMargin left="0" right="0" top="0" bottom="0"/></hp:pic>`, width, height, pos)
	}
	note := func(kind, text string) string {
		numType := "ENDNOTE"
		if kind == "footNote" {
			numType = "FOOTNOTE"
		}
		return fmt.Sprintf(`<hp:ctrl><hp:%s number="1" suffixChar="41" instId="77">`, kind) +
			subList(p("0", r(fmt.Sprintf(`<hp:ctrl><hp:autoNum num="1" numType="%s"><hp:autoNumFormat type="DIGIT" userChar="" prefixChar="" suffixChar=")" supscript="0"/></hp:autoNum></hp:ctrl>`, numType)+tt(" "+text), "0"), false), "TOP") +
			fmt.Sprintf(`</hp:%s></hp:ctrl>`, kind)
	}
	hyperlink := func(id, command string) string {
		return fmt.Sprintf(`<hp:ctrl><hp:fieldBegin id="%s" type="HYPERLINK" name="" editable="0" dirty="0" zorder="-1" fieldid="627600491"><hp:parameters cnt="2" name="">`, id) +
			fmt.Sprintf(`<hp:integerParam name="Prop">0</hp:integerParam><hp:stringParam name="Command">%s</hp:stringParam></hp:parameters></hp:fieldBegin></hp:ctrl>`, command)
	}
	fieldEnd := func(id string) string { return fmt.Sprintf(`<hp:ctrl><hp:fieldEnd beginIDRef="%s" fieldid="627600491"/></hp:ctrl>`, id) }

	secPr := `<hp:secPr id="" textDirection="HORIZONTAL" spaceColumns="1134" tabStop="8000" tabStopVal="4000" tabStopUnit="HWPUNIT" outlineShapeIDRef="1" memoShapeIDRef="0" textVerticalWidthHead="0" masterPageCnt="0">` +
		`<hp:grid lineGrid="0" charGrid="0" wonggojiFormat="0"/><hp:startNum pageStartsOn="BOTH" page="5" pic="0" tbl="0" equation="0"/>` +
		`<hp:visibility hideFirstHeader="1" hideFirstFooter="0" hideFirstMasterPage="0" border="SHOW_ALL" fill="SHOW_ALL" hideFirstPageNum="0" hideFirstEmptyLine="0" showLineNumber="0"/>` +
		`<hp:lineNumberShape restartType="0" countBy="0" distance="0" startNumber="0"/>` +
		`<hp:pagePr landscape="NARROWLY" width="59528" height="84188" gutterType="LEFT_ONLY"><hp:margin header="4252" footer="4252" gutter="0" left="8504" right="8504" top="5668" bottom="4252"/></hp:pagePr>` +
		`<hp:footNotePr><hp:autoNumFormat type="DIGIT" userChar="" prefixChar="" suffixChar=")" supscript="0"/><hp:noteLine length="-1" type="SOLID" width="0.12 mm" color="#000000"/></hp:footNotePr>` +
		`</hp:secPr>`
	setup := secPr + `<hp:ctrl><hp:colPr id="" type="NEWSPAPER" layout="LEFT" colCount="2" sameSz="0" sameGap="0"><hp:colLine type="SOLID" width="0.12 mm" color="#000000"/><hp:colSz width="20000" gap="850"/><hp:colSz width="21000" gap="0"/></hp:colPr></hp:ctrl>`
	headerControls := `<hp:ctrl><hp:header id="1" applyPageType="ODD">` + subList(p("0", r(tt("홀수 쪽 머리말"), "0"), false), "TOP") + "</hp:header></hp:ctrl>" +
		`<hp:ctrl><hp:pageNum pos="BOTTOM_CENTER" formatType="DIGIT" sideChar="-"/></hp:ctrl>`
	nestedTable := `<hp:tbl id="21" zOrder="1" numberingType="TABLE" textWrap="TOP_AND_BOTTOM" textFlow="BOTH_SIDES" lock="0" dropcapstyle="None" pageBreak="CELL" repeatHeader="0" rowCnt="1" colCnt="1" cellSpacing="0" borderFillIDRef="1" noAdjust="0">` +
		`<hp:sz width="6000" widthRelTo="ABSOLUTE" height="1000" heightRelTo="ABSOLUTE" protect="0"/>` + pos(true, 0, "0", "LEFT") + `<hp:outMargin left="0" right="0" top="0" bottom="0"/>` +
		"<hp:tr>" + cell(p("0", r(tt("안쪽"), "0"), false), 0, 0, 1, 1, 6000, 1000, "1", false, "TOP") + "</hp:tr></hp:tbl>"
	table := `<hp:tbl id="20" zOrder="0" numberingType="TABLE" textWrap="TOP_AND_BOTTOM" textFlow="BOTH_SIDES" lock="0" dropcapstyle="None" pageBreak="CELL" repeatHeader="1" rowCnt="2" colCnt="3" cellSpacing="0" borderFillIDRef="1" noAdjust="0">` +
		`<hp:sz width="30000" widthRelTo="ABSOLUTE" height="3000" heightRelTo="ABSOLUTE" protect="0"/>` + pos(false, 0, "0", "RIGHT") +
		`<hp:outMargin left="283" right="283" top="283" bottom="283"/><hp:inMargin left="141" right="141" top="141" bottom="141"/>` +
		`<hp:caption side="BOTTOM" fullSz="0" width="8504" gap="850" lastWidth="30000">` +
		subList(p("0", r(tt("표 ")+`<hp:ctrl><hp:autoNum num="1" numType="TABLE"><hp:autoNumFormat type="DIGIT" userChar="" prefixChar="" suffixChar="" supscript="0"/></hp:autoNum></hp:ctrl>`+tt(" 캡션"), "0"), false), "TOP") + "</hp:caption>" +
		"<hp:tr>" + cell(p("0", r(tt("병합"), "0"), false), 0, 0, 2, 1, 20000, 1500, "2", true, "CENTER") +
		cell(p("0", r(tt("세로"), "0"), false), 2, 0, 1, 2, 10000, 3000, "1", true, "BOTTOM") + "</hp:tr>" +
		"<hp:tr>" + cell(p("0", r(nestedTable+"<hp:t/>", "0"), false), 0, 1, 1, 1, 8000, 1500, "1", false, "TOP") + cell(p("0", r(tt("B"), "0"), false), 1, 1, 1, 1, 12000, 1500, "1", false, "TOP") + "</hp:tr></hp:tbl>"
	group := `<hp:container id="40" zOrder="3" numberingType="PICTURE" textWrap="SQUARE" textFlow="BOTH_SIDES" lock="0" dropcapstyle="None" href="" groupLevel="0" instid="41">` +
		`<hp:offset x="0" y="0"/><hp:orgSz width="20000" height="10000"/><hp:curSz width="20000" height="10000"/><hp:flip horizontal="0" vertical="0"/>` +
		`<hp:rect id="42" zOrder="0" numberingType="NONE" textWrap="SQUARE" textFlow="BOTH_SIDES" lock="0" dropcapstyle="None" href="" groupLevel="1" instid="43" ratio="20">` +
		`<hp:offset x="1000" y="2000"/><hp:orgSz width="8000" height="4000"/><hp:curSz width="0" height="0"/><hp:flip horizontal="0" vertical="0"/>` +
		`<hp:renderingInfo><hc:transMatrix e1="1" e2="0" e3="1000" e4="0" e5="1" e6="2000"/><hc:scaMatrix e1="1.5" e2="0" e3="0" e4="0" e5="0.5" e6="0"/><hc:rotMatrix e1="1" e2="0" e3="0" e4="0" e5="1" e6="0"/></hp:renderingInfo>` +
		`<hp:lineShape color="#FF0000" width="283" style="DASH" endCap="FLAT" headStyle="NORMAL" tailStyle="NORMAL" headfill="1" tailfill="1" headSz="MEDIUM_MEDIUM" tailSz="MEDIUM_MEDIUM" outlineStyle="NORMAL" alpha="0"/>` +
		`<hc:fillBrush><hc:gradation type="LINEAR" angle="90" centerX="50" centerY="50" step="255" colorNum="2" stepCenter="50" alpha="0"><hc:color value="#FF0000"/><hc:color value="#0000FF"/></hc:gradation></hc:fillBrush>` +
		`<hp:shadow type="NONE" color="#B2B2B2" offsetX="0" offsetY="0" alpha="0"/>` +
		`<hp:drawText lastWidth="8000" name="" editable="0">` + subList(p("0", r(tt("묶음 글상자"), "0"), false), "CENTER") + `<hp:textMargin left="100" right="100" top="50" bottom="50"/></hp:drawText>` +
		`<hc:pt0 x="0" y="0"/><hc:pt1 x="8000" y="0"/><hc:pt2 x="8000" y="4000"/><hc:pt3 x="0" y="4000"/></hp:rect>` +
		`<hp:ellipse id="44" zOrder="1" numberingType="NONE" textWrap="SQUARE" textFlow="BOTH_SIDES" lock="0" dropcapstyle="None" href="" groupLevel="1" instid="45" intervalDirty="0" hasArcPr="0" arcType="NORMAL">` +
		`<hp:offset x="12000" y="0"/><hp:orgSz width="6000" height="6000"/><hp:curSz width="6000" height="6000"/>` +
		`<hp:lineShape color="#000000" width="0" style="SOLID"/><hc:fillBrush><hc:winBrush faceColor="#00FF00" hatchColor="#000000" alpha="0"/></hc:fillBrush></hp:ellipse>` +
		`<hp:sz width="20000" widthRelTo="ABSOLUTE" height="10000" heightRelTo="ABSOLUTE" protect="0"/>` + pos(false, 1000, "500", "LEFT") +
		`<hp:outMargin left="0" right="0" top="0" bottom="0"/></hp:container>`

	section0 := `<?xml version="1.0" encoding="UTF-8" standalone="yes" ?><hs:sec ` + ns + ">" +
		`<hp:p id="0" paraPrIDRef="2" styleIDRef="1" pageBreak="0" columnBreak="0" merged="0">` + r(setup+headerControls, "0") + r(tt("제1장 개요"), "0") + "</hp:p>" +
		p("1", r(tt("굵게"), "1")+r(tt(`보통<hp:markpenBegin color="#00FF00"/>형광<hp:markpenEnd/> 묶음<hp:nbSpace/>빈칸<hp:fwSpace/>고정<hp:tab width="4000" leader="0" type="1"/>탭<hp:lineBreak/>둘째 줄`+
			`<hp:deleteBegin Id="1" TcId="1"/>지운 글<hp:deleteEnd Id="1" TcId="1"/><hp:insertBegin Id="2" TcId="2"/>넣은 글<hp:insertEnd Id="2" TcId="2"/>하이<hp:hyphen/>픈`), "0"), false) +
		p("0", r(`<hp:ctrl><hp:bookmark name="처음"/></hp:ctrl>`+hyperlink("10", `http\://www.hancom.com\?a=1;1;0;0;`)+tt("한컴"), "0")+
			r(tt("링크")+fieldEnd("10")+tt(" 뒤"), "1")+
			r(hyperlink("11", "?처음;0;0;0;")+tt("처음으로")+fieldEnd("11")+
				`<hp:ctrl><hp:fieldBegin id="12" type="CLICK_HERE" name="이름" editable="1" dirty="0"><hp:parameters cnt="0" name=""/></hp:fieldBegin></hp:ctrl>`+tt("누름틀")+fieldEnd("12"), "0"), false) +
		p("3", r(tt("글머리 항목"), "0"), false) +
		p("4", r(tt("번호 항목"), "0"), false) +
		p("5", r(tt("줄 간격"), "0"), false) +
		p("0", r(tt("표 앞")+table+tt("표 뒤"), "0"), false) +
		p("0", r(picture("image1", 5000, 2500, pos(false, 1000, "4294967196", "LEFT"))+
			picture("image2", 3000, 3000, pos(true, 0, "0", "LEFT"))+picture("image9", 3000, 3000, pos(true, 0, "0", "LEFT"))+"<hp:t/>", "0"), false) +
		p("0", r(group+"<hp:t/>", "0"), false) +
		p("0", r(tt("식: ")+
			`<hp:equation id="30" zOrder="4" numberingType="EQUATION" textWrap="TOP_AND_BOTTOM" textFlow="BOTH_SIDES" lock="0" dropcapstyle="None" version="Equation Version 60" baseLine="86" textColor="#000000" baseUnit="1000" lineMode="CHAR" font="HYhwpEQ">`+
			`<hp:sz width="2000" widthRelTo="ABSOLUTE" height="1500" heightRelTo="ABSOLUTE" protect="0"/>`+pos(true, 0, "0", "LEFT")+`<hp:outMargin left="56" right="56" top="0" bottom="0"/><hp:shapeComment>수식입니다.</hp:shapeComment><hp:script>x over y</hp:script></hp:equation>`+
			tt(" ")+`<hp:compose circleType="SHAPE_CIRCLE" charSz="-3" composeType="SPREAD" charPrCnt="10" composeText="12"/>`+
			`<hp:dutmal posType="TOP" szRatio="0" option="4" styleIDRef="0" align="CENTER"><hp:mainText>본말</hp:mainText><hp:subText>덧말</hp:subText></hp:dutmal>`, "0"), false) +
		p("0", r(tt("각주 참조")+note("footNote", "각주 내용")+note("endNote", "미주 내용"), "0"), false) +
		p("0", r(`<hp:switch><hp:case hp:required-namespace="http://www.hancom.co.kr/hwpml/2016/ooxmlchart"><hp:chart id="50" chartIDRef="Chart/chart1.xml"/></hp:case>`+
			`<hp:default><hp:ole id="51" objectType="EMBEDDED" binaryItemIDRef="ole1"/></hp:default></hp:switch>`+tt("차트 뒤"), "0"), false) +
		p("0", r(tt("다음 단"), "0"), true) +
		"</hs:sec>"

	// Other prefixes: a default namespace for the section and "p:" for paragraphs.
	section1 := `<?xml version="1.0" encoding="UTF-8"?><sec xmlns="http://www.hancom.co.kr/hwpml/2011/section" xmlns:p="http://www.hancom.co.kr/hwpml/2011/paragraph">` +
		`<p:p id="0" paraPrIDRef="0" styleIDRef="0" pageBreak="0" columnBreak="0" merged="0"><p:run charPrIDRef="0">` +
		`<p:secPr id="" outlineShapeIDRef="1"><p:startNum pageStartsOn="BOTH" page="0"/><p:pagePr landscape="WIDELY" width="59528" height="84188" gutterType="LEFT_ONLY">` +
		`<p:margin header="4252" footer="4252" gutter="0" left="8504" right="8504" top="5668" bottom="4252"/></p:pagePr></p:secPr>` +
		`<p:ctrl><p:colPr id="" type="NEWSPAPER" layout="LEFT" colCount="1" sameSz="1" sameGap="0"/></p:ctrl><p:ctrl><p:newNum num="3" numType="PAGE"/></p:ctrl></p:run>` +
		`<p:run charPrIDRef="1"><p:t>둘째 구역</p:t></p:run></p:p></sec>`

	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	write := func(name, text string, method uint16) {
		w, _ := z.CreateHeader(&zip.FileHeader{Name: name, Method: method})
		w.Write([]byte(text))
	}
	write("mimetype", "application/hwp+zip", zip.Store)
	write("version.xml", `<?xml version="1.0" encoding="UTF-8"?><hv:HCFVersion xmlns:hv="http://www.hancom.co.kr/hwpml/2011/version" tagetApplication="WORDPROCESSOR" major="5" minor="1" micro="1" buildNumber="0" os="1" xmlVersion="1.4" application="Hancom Office Hangul" appVersion="12, 0, 0, 1234 WIN32LEWindows_10"/>`, zip.Deflate)
	write("META-INF/container.xml", `<?xml version="1.0" encoding="UTF-8"?><ocf:container xmlns:ocf="urn:oasis:names:tc:opendocument:xmlns:container"><ocf:rootfiles>`+
		`<ocf:rootfile full-path="Contents/content.hpf" media-type="application/hwpml-package+xml"/><ocf:rootfile full-path="Preview/PrvText.txt" media-type="text/plain"/></ocf:rootfiles></ocf:container>`, zip.Deflate)
	write("META-INF/manifest.xml", `<?xml version="1.0" encoding="UTF-8"?><odf:manifest xmlns:odf="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0"/>`, zip.Deflate)
	write("Contents/content.hpf", `<?xml version="1.0" encoding="UTF-8"?><opf:package xmlns:opf="http://www.idpf.org/2007/opf/" version="" unique-identifier="" id="">`+
		`<opf:metadata><opf:title>한글 문서 제목</opf:title><opf:language>ko</opf:language></opf:metadata><opf:manifest>`+
		`<opf:item id="header" href="Contents/header.xml" media-type="application/xml"/><opf:item id="image1" href="BinData/image1.png" media-type="image/png" isEmbeded="1"/>`+
		`<opf:item id="section0" href="Contents/section0.xml" media-type="application/xml"/><opf:item id="section1" href="Contents/section1.xml" media-type="application/xml"/>`+
		`<opf:item id="settings" href="settings.xml" media-type="application/xml"/></opf:manifest>`+
		`<opf:spine><opf:itemref idref="header" linear="yes"/><opf:itemref idref="section0" linear="yes"/><opf:itemref idref="section1" linear="yes"/></opf:spine></opf:package>`, zip.Deflate)
	write("Contents/header.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes" ?>`+header, zip.Deflate)
	write("Contents/section0.xml", section0, zip.Deflate)
	write("Contents/section1.xml", section1, zip.Deflate)
	for _, name := range []string{"image1", "image2"} {
		w, _ := z.Create("BinData/" + name + ".png")
		w.Write(pngBytes(40, 20))
	}
	z.Close()
	path := filepath.Join(folder, "hangul-style.hwpx")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func inlineSummary(inlines []doc.Inline) string {
	var parts []string
	for _, in := range inlines {
		switch v := in.(type) {
		case *doc.Text:
			parts = append(parts, v.Text)
		case *doc.Tab:
			parts = append(parts, "<tab>")
		case *doc.LineBreak:
			parts = append(parts, "<br>")
		case *doc.Field:
			parts = append(parts, fmt.Sprintf("<field %d>", v.Kind))
		default:
			parts = append(parts, "?")
		}
	}
	return strings.Join(parts, "|")
}

func TestPackagesInTheStyleOfHangulAreRead(t *testing.T) {
	dir := t.TempDir()
	path := hangulStylePackage(t, dir)
	skipped := map[string]int{}
	d, err := ReadHwpx(path, filepath.Join(dir, "media"), skipped)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "title", d.Title, "한글 문서 제목")
	if len(d.Sections) != 2 {
		t.Fatalf("%d sections", len(d.Sections))
	}
	s := d.Sections[0]
	eq(t, "page", *s.Page, doc.Page{Width: 84188, Height: 59528, Left: 8504, Right: 8504, Top: 5668, Bottom: 4252, Header: 4252, Footer: 4252})
	eq(t, "columns", s.Columns, doc.Columns{Count: 2, Gap: 850, Separator: true})
	if !s.HideFirstHeader || val(s.StartPageNumber, 0) != 5 {
		t.Errorf("hide %v start %v", s.HideFirstHeader, deref(s.StartPageNumber))
	}
	if len(s.Headers) != 1 || s.Headers[0].Pages != doc.OddPages || allText(s.Headers[0].Blocks) != "홀수 쪽 머리말" {
		t.Errorf("headers")
	}
	pageNumber := s.Footers[0].Blocks[0].(*doc.Paragraph)
	eq(t, "page number align", *pageNumber.Format.Align, doc.AlignCenter)
	eq(t, "page number", inlineSummary(pageNumber.Inlines), "- |<field 0>| -")

	blocks := s.Blocks
	heading := blocks[0].(*doc.Paragraph)
	eq(t, "heading", fmt.Sprintf("%d %s", heading.HeadingLevel, allText(blocks[:1])), "1 제1장 개요")
	eq(t, "outline numbering", heading.List.Numbering.Levels[0].Text, "^1.")
	eq(t, "halved margin", *heading.Format.Left, 1000)
	eq(t, "heading spacing", *heading.Format.LineSpacing, doc.LineSpacing{Kind: doc.SpacingPercent, Value: 150})

	formatted := blocks[1].(*doc.Paragraph)
	eq(t, "format", paraString(formatted.Format), paraString(doc.ParaFormat{Align: ptr(doc.AlignLeft), Left: ptr(3000), Right: ptr(500), FirstLine: ptr(-2000), Before: ptr(400), After: ptr(600),
		LineSpacing: &doc.LineSpacing{Kind: doc.SpacingFixed, Value: 1800}, KeepWithNext: ptr(true), KeepLines: ptr(true), WidowOrphan: ptr(true),
		Tabs: []doc.TabStop{{Position: 20000, Kind: doc.TabCenter, Leader: "DASH"}}}))
	eq(t, "texts", inlineSummary(formatted.Inlines), "굵게|보통|형광| 묶음\u00a0빈칸\u2007고정|<tab>|탭|<br>|둘째 줄넣은 글하이\u00ad픈")
	bold := formatted.Inlines[0].(*doc.Text).Format
	eq(t, "bold format", charString(bold), charString(doc.CharFormat{Bold: ptr(true), Italic: ptr(true), Underline: ptr(true), Strike: ptr(true), Superscript: ptr(true), Subscript: ptr(false),
		Size: ptr(1400), Font: ptr("Times New Roman"), EastAsianFont: ptr("맑은 고딕"), Color: ptr("#0000FF"), Shade: ptr("#FFFF00"), Spacing: ptr(-5)}))
	eq(t, "markpen", val(formatted.Inlines[2].(*doc.Text).Format.Shade, ""), "#00FF00")
	eq(t, "plain format", charString(formatted.Inlines[1].(*doc.Text).Format), charString(doc.CharFormat{Bold: ptr(false), Italic: ptr(false), Underline: ptr(false), Strike: ptr(false),
		Superscript: ptr(false), Subscript: ptr(false), Size: ptr(1000), Font: ptr("함초롬바탕"), EastAsianFont: ptr("함초롬바탕"), Color: ptr("#000000"), Spacing: ptr(0)}))

	links := blocks[2].(*doc.Paragraph).Inlines
	eq(t, "bookmark", links[0].(*doc.Bookmark).Name, "처음")
	web := links[1].(*doc.Link)
	eq(t, "web", web.Target+" "+allText([]doc.Block{&doc.Paragraph{Inlines: web.Content}}), "http://www.hancom.com?a=1 한컴링크")
	eq(t, "after", links[2].(*doc.Text).Text, " 뒤")
	eq(t, "bookmark link", links[3].(*doc.Link).Target, "#처음")
	eq(t, "click-here", links[4].(*doc.Text).Text, "누름틀")

	bullet := blocks[3].(*doc.Paragraph).List
	eq(t, "bullet", fmt.Sprintf("%s %v", bullet.Numbering.Levels[0].Text, bullet.Numbering.Levels[0].Bullet), "● true")
	numbered := blocks[4].(*doc.Paragraph)
	l := numbered.List.Numbering.Levels[1]
	eq(t, "numbered", fmt.Sprintf("%d %s %s %d", numbered.List.Level, l.Text, l.Format, l.Start), "1 (^2) ROMAN_SMALL 3")
	if !numbered.PageBreakBefore {
		t.Error("no page break")
	}
	eq(t, "between lines", *blocks[5].(*doc.Paragraph).Format.LineSpacing, doc.LineSpacing{Kind: doc.SpacingAtLeast, Value: 1500})

	// A table inside a paragraph splits it: text before, the table, text after.
	eq(t, "before table", allText(blocks[6:7]), "표 앞")
	table := blocks[7].(*doc.Table)
	eq(t, "after table", allText(blocks[8:9]), "표 뒤")
	eq(t, "align", table.Align, doc.AlignRight)
	eq(t, "margin", *table.CellMargin, doc.Insets{Left: 141, Right: 141, Top: 141, Bottom: 141})
	eq(t, "widths", fmt.Sprint(table.ColumnWidths), "[8000 12000 10000]")
	eq(t, "caption", allText(table.Caption), "표 1 캡션")
	merged := table.Rows[0].Cells[0]
	eq(t, "merged", fmt.Sprintf("%d %d %d %s %v", merged.Column, merged.RowSpan, merged.ColSpan, merged.Fill, merged.VerticalAlign), fmt.Sprintf("0 1 2 #FFFF00 %v", doc.VAlignCenter))
	eq(t, "borders", *merged.Borders, doc.Borders{Left: doc.Border{Style: doc.BorderSolid, WidthMm: 0.12, Color: "#000000"}, Right: doc.Border{Style: doc.BorderDouble, WidthMm: 0.5, Color: "#FF0000"},
		Top: doc.Border{Style: doc.BorderDash, WidthMm: 0.2, Color: "#0000FF"}, Bottom: doc.Border{Style: doc.BorderDot, WidthMm: 0.12, Color: "#000000"}})
	tall := table.Rows[0].Cells[1]
	eq(t, "tall", fmt.Sprintf("%d %d %d %v", tall.Column, tall.RowSpan, tall.ColSpan, tall.VerticalAlign), fmt.Sprintf("2 2 1 %v", doc.VAlignBottom))
	eq(t, "headers", fmt.Sprint(table.Rows[0].Header, table.Rows[1].Header), "true false")
	eq(t, "heights", fmt.Sprint(*table.Rows[0].Height, *table.Rows[1].Height), "1500 1500")
	eq(t, "nested", allText(table.Rows[1].Cells[0].Blocks), "안쪽")

	var pictures []*doc.Image
	for _, in := range blocks[9].(*doc.Paragraph).Inlines {
		if img, ok := in.(*doc.Image); ok {
			pictures = append(pictures, img)
		}
	}
	if len(pictures) != 2 {
		t.Fatalf("%d pictures", len(pictures)) // the third refers to a missing image
	}
	eq(t, "picture size", fmt.Sprint(*pictures[0].Width, *pictures[0].Height), "5000 2500")
	eq(t, "anchor", *pictures[0].Anchor, doc.Anchor{HorizontalRelativeTo: "COLUMN", HorizontalAlign: "LEFT", HorizontalOffset: 1000, VerticalRelativeTo: "PARA", VerticalAlign: "TOP", VerticalOffset: -100, Wrap: "SQUARE"})
	if pictures[1].Anchor != nil || !readable(pictures[0].Path) || !readable(pictures[1].Path) {
		t.Error("pictures")
	}

	group := blocks[10].(*doc.Paragraph).Inlines
	box := group[0].(*doc.TextBox)
	eq(t, "box", fmt.Sprint(box.Width, box.Height, box.Anchor.HorizontalOffset, box.Anchor.VerticalOffset), "12000 2000 2000 2500")
	eq(t, "box style", fmt.Sprint(strings.Join(box.Gradient.Colors, ","), box.Gradient.Angle, box.Line.Style, box.CornerRatio), fmt.Sprint("#FF0000,#0000FF", 90, doc.BorderDash, 20))
	eq(t, "box text", fmt.Sprint(box.Padding, box.VerticalAlign, allText(box.Blocks)), fmt.Sprint(doc.Insets{Left: 100, Right: 100, Top: 50, Bottom: 50}, doc.VAlignCenter, "묶음 글상자"))
	ellipse := group[1].(*doc.Shape)
	eq(t, "ellipse", fmt.Sprint(ellipse.Kind, ellipse.Width, ellipse.Anchor.HorizontalOffset, ellipse.Fill, ellipse.Line.WidthMm), fmt.Sprint(doc.ShapeEllipse, 6000, 13000, "#00FF00", 0.1))

	eq(t, "equation text", allText(blocks[11:12]), "식: x over y 12본말")
	var notes []string
	for _, in := range blocks[12].(*doc.Paragraph).Inlines {
		if n, ok := in.(*doc.Note); ok {
			notes = append(notes, fmt.Sprintf("%s/%v", allText(n.Blocks), n.Endnote))
		}
	}
	eq(t, "notes", strings.Join(notes, ","), "각주 내용/false,미주 내용/true")
	eq(t, "after chart", allText(blocks[13:14]), "차트 뒤")
	if !blocks[14].(*doc.Paragraph).ColumnBreakBefore {
		t.Error("no column break")
	}

	second := d.Sections[1]
	eq(t, "second page", fmt.Sprint(second.Page.Width, second.Page.Height, second.Page.Landscape()), "59528 84188 false")
	eq(t, "restart", val(second.StartPageNumber, 0), 3)
	eq(t, "second text", allText(second.Blocks), "둘째 구역")
	if !isTrue(second.Blocks[0].(*doc.Paragraph).Inlines[0].(*doc.Text).Format.Bold) {
		t.Error("second section text not bold")
	}
	eq(t, "skipped", fmt.Sprint(skipped), "map[ole:1 pic:1]")
}

func TestTheTemplateSavedByHangulIsRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blank.hwpx")
	os.WriteFile(path, BlankTemplate(), 0o644)
	d, err := ReadHwpx(path, filepath.Join(dir, "media"), nil)
	if err != nil {
		t.Fatal(err)
	}
	s := d.Sections[0]
	eq(t, "page", *s.Page, doc.Page{Width: 59530, Height: 84190, Left: 7200, Right: 7200, Top: 4255, Bottom: 4960, Header: 4250, Footer: 2240})
	eq(t, "columns", s.Columns, doc.SingleColumn)
	if len(s.Blocks) != 1 {
		t.Fatalf("%d blocks", len(s.Blocks))
	}
	p := s.Blocks[0].(*doc.Paragraph)
	eq(t, "inlines", len(p.Inlines), 0)
	eq(t, "line spacing", *p.Format.LineSpacing, doc.LineSpacing{Kind: doc.SpacingPercent, Value: 180})
	eq(t, "after", *p.Format.After, 1000)
	eq(t, "mark size", *p.MarkFormat.Size, 1000)
}

func TestProtectedAndForeignFilesGiveAClearError(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "media")
	zipWith := func(name string, entries ...[2]string) string {
		var buf bytes.Buffer
		z := zip.NewWriter(&buf)
		for _, e := range entries {
			w, _ := z.Create(e[0])
			w.Write([]byte(e[1]))
		}
		z.Close()
		p := filepath.Join(dir, name)
		os.WriteFile(p, buf.Bytes(), 0o644)
		return p
	}
	expect := func(path, want string) {
		t.Helper()
		_, err := ReadHwpx(path, media, nil)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v (want %q)", filepath.Base(path), err, want)
		}
	}
	expect(zipWith("encrypted.hwpx", [2]string{"mimetype", "application/hwp+zip"},
		[2]string{"META-INF/manifest.xml", `<odf:manifest xmlns:odf="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0"><odf:file-entry odf:full-path="Contents/section0.xml" odf:media-type="application/xml"><odf:encryption-data odf:checksum-type="SHA1" odf:checksum="AAAA"><odf:algorithm odf:algorithm-name="AES"/></odf:encryption-data></odf:file-entry></odf:manifest>`},
		[2]string{"Contents/section0.xml", "\x01\x02 not xml"}), "password")
	drm := filepath.Join(dir, "drm.hwpx")
	os.WriteFile(drm, []byte("<DRMONE>This is a protected document</DRMONE>"), 0o644)
	expect(drm, "DRM")
	hwp := filepath.Join(dir, "binary.hwpx")
	os.WriteFile(hwp, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1, 0, 0, 0, 0}, 0o644)
	expect(hwp, "HWP 5.0")
	expect(zipWith("broken.hwpx", [2]string{"mimetype", "application/hwp+zip"},
		[2]string{"Contents/header.xml", `<hh:head xmlns:hh="http://www.hancom.co.kr/hwpml/2011/head"/>`},
		[2]string{"Contents/section0.xml", "PK\x03\x04 encrypted bytes"}), "encrypted")
}
