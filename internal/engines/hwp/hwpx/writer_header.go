package hwpx

// header.xml: fonts, character and paragraph shapes, list numberings, border/fill definitions and tab stops.
// Everything is created on demand and cached, so equal formatting shares one definition like 한글 does.

import (
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/hwp/xmltree"
)

const hwpUnitCharNamespace = "http://www.hancom.co.kr/hwpml/2016/HwpUnitChar"

// charPrOrder is the schema order of charPr children. New elements are inserted accordingly.
var charPrOrder = []string{"fontRef", "ratio", "spacing", "relSz", "offset", "italic", "bold", "underline", "strikeout", "outline", "shadow", "emboss", "engrave", "supscript", "subscript"}

// lineWidths are the line widths 한글 offers (mm); others are snapped to the nearest.
var lineWidths = []float64{0.1, 0.12, 0.15, 0.2, 0.25, 0.3, 0.4, 0.5, 0.6, 0.7, 1.0, 1.5, 2.0, 3.0, 4.0, 5.0}

var languages = []string{"HANGUL", "LATIN", "HANJA", "JAPANESE", "OTHER", "SYMBOL", "USER"}

type charPrKey struct {
	base   string
	format charKey
	link   bool
}

type paraPrKeyFull struct {
	base      string
	format    paraKey
	numbering string
	level     int
	numbered  bool
}

type borderFillKey struct {
	borders doc.Borders
	fill    string
}

func hh(local string, attrs ...string) *xmltree.Element { return xmltree.New("hh", nsHH, local, attrs...) }
func hc(local string, attrs ...string) *xmltree.Element { return xmltree.New("hc", nsHC, local, attrs...) }
func hp(local string, attrs ...string) *xmltree.Element { return xmltree.New("hp", nsHP, local, attrs...) }

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

func (w *writer) refList(name string) *xmltree.Element {
	return w.head.FirstDescendantNS(nsHH, name)
}

func (w *writer) initializeHeader() error {
	w.charPrCache = map[charPrKey]string{}
	w.paraPrCache = map[paraPrKeyFull]string{}
	w.numberingIDs = map[*doc.Numbering]string{}
	w.borderFills = map[borderFillKey]string{}
	w.tabPrIDs = map[string]string{}
	w.fontIDs = map[[2]string]int{}
	w.headings = map[int]styleIDs{}
	w.charPrs = map[string]*xmltree.Element{}
	w.paraPrs = map[string]*xmltree.Element{}
	w.normal = styleIDs{"0", "0", "0"}
	w.width, w.pageLeft, w.pageTop = 42520, 7200, 8505

	charPrs := w.head.DescendantsNS(nsHH, "charPr")
	paraPrs := w.head.DescendantsNS(nsHH, "paraPr")
	for _, c := range charPrs {
		if _, ok := w.charPrs[c.A("id")]; !ok {
			w.charPrs[c.A("id")] = c
		}
	}
	for _, p := range paraPrs {
		if _, ok := w.paraPrs[p.A("id")]; !ok {
			w.paraPrs[p.A("id")] = p
		}
	}
	if len(paraPrs) == 0 {
		return errors.New("header.xml has no paraPr")
	}
	w.maxCharPr = maxID(charPrs)
	w.maxParaPr = maxID(paraPrs)

	// Numbering definitions live in refList, before paraProperties.
	if w.refList("numberings") == nil {
		refList := w.head.ChildNS(nsHH, "refList")
		if refList == nil {
			refList = w.head
		}
		numberings := hh("numberings", "itemCnt", "0")
		if pp := refList.ChildNS(nsHH, "paraProperties"); pp != nil {
			pp.AddBeforeSelf(numberings)
		} else {
			refList.Add(numberings)
		}
	}
	for _, name := range []string{"charProperties", "paraProperties", "borderFills", "tabProperties"} {
		if w.refList(name) == nil {
			return errors.New("header.xml has no " + name)
		}
	}

	styles := w.head.DescendantsNS(nsHH, "style")
	ids := func(style *xmltree.Element) styleIDs {
		get := func(name, def string) string {
			if v, ok := style.Attr(name); ok {
				return v
			}
			return def
		}
		return styleIDs{get("id", w.normal.style), get("paraPrIDRef", w.normal.paraPr), get("charPrIDRef", w.normal.charPr)}
	}
	var normal *xmltree.Element
	for _, s := range styles {
		if s.A("id") == "0" {
			normal = s
			break
		}
	}
	if normal == nil && len(styles) > 0 {
		normal = styles[0]
	}
	if normal != nil {
		w.normal = ids(normal)
	}
	// "Heading 1".."Heading 9" by English style name; fall back to outline levels of paragraph properties.
	for _, s := range styles {
		name := s.A("engName")
		if len(name) > 8 && strings.EqualFold(name[:8], "Heading ") {
			if level, err := strconv.Atoi(name[8:]); err == nil && level >= 1 && level <= 9 {
				w.headings[level] = ids(s)
				continue
			}
		}
		if strings.Contains(strings.ToLower(name), "footnote") {
			v := ids(s)
			w.footnoteStyle = &v
		}
	}
	if len(w.headings) == 0 {
		for _, paraPr := range paraPrs {
			heading := paraPr.ChildNS(nsHH, "heading")
			if heading == nil || heading.A("type") != "OUTLINE" {
				continue
			}
			outline, err := strconv.Atoi(heading.A("level"))
			if err != nil {
				continue
			}
			id := paraPr.A("id")
			for _, s := range styles {
				if s.A("paraPrIDRef") == id {
					if _, ok := w.headings[outline+1]; !ok {
						w.headings[outline+1] = ids(s)
					}
					break
				}
			}
		}
	}
	return nil
}

func (w *writer) serializeHeader(sectionCount int) string {
	count := func(container, item string) {
		if e := w.refList(container); e != nil {
			e.SetAttr("itemCnt", itoa(len(e.ChildrenNS(nsHH, item))))
		}
	}
	count("charProperties", "charPr")
	count("paraProperties", "paraPr")
	count("numberings", "numbering")
	count("borderFills", "borderFill")
	count("tabProperties", "tabPr")
	w.head.SetAttr("secCnt", itoa(sectionCount))
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + w.head.String()
}

// ───────────────────────── Fonts ─────────────────────────

// fontID is the index of face in the font list of language (added if missing).
func (w *writer) fontID(language, face string) int {
	key := [2]string{language, strings.ToLower(face)}
	if id, ok := w.fontIDs[key]; ok {
		return id
	}
	var fontface *xmltree.Element
	for _, f := range w.head.DescendantsNS(nsHH, "fontface") {
		if f.A("lang") == language {
			fontface = f
			break
		}
	}
	if fontface == nil {
		return 0
	}
	fonts := fontface.ChildrenNS(nsHH, "font")
	id := -1
	for _, f := range fonts {
		if strings.EqualFold(f.A("face"), face) {
			id, _ = strconv.Atoi(f.A("id"))
			break
		}
	}
	if id < 0 {
		id = len(fonts)
		fontface.Add(hh("font", "id", itoa(id), "face", face, "type", "TTF", "isEmbedded", "0"))
		fontface.SetAttr("fontCnt", itoa(id+1))
	}
	w.fontIDs[key] = id
	return id
}

// ───────────────────────── Character shapes ─────────────────────────

// charPr is the character shape id for baseID with format applied (created on demand).
func (w *writer) charPr(baseID string, format doc.CharFormat, link bool) string {
	// Links without their own colour get the usual blue underline.
	link = link && format.Color == nil
	if format.IsEmpty() && !link {
		return baseID
	}
	key := charPrKey{baseID, keyOfChar(format), link}
	if id, ok := w.charPrCache[key]; ok {
		return id
	}
	source := w.charPrs[baseID]
	if source == nil {
		if all := w.head.DescendantsNS(nsHH, "charPr"); len(all) > 0 {
			source = all[0]
		}
	}
	if source == nil {
		return baseID
	}
	node := source.Copy()
	w.maxCharPr++
	id := itoa(w.maxCharPr)
	node.SetAttr("id", id)

	if format.Size != nil {
		node.SetAttr("height", itoa(clamp(*format.Size, 100, 409600)))
	}
	if format.Color != nil {
		node.SetAttr("textColor", *format.Color)
	}
	if link {
		node.SetAttr("textColor", "#0000FF")
	}
	if format.Shade != nil {
		node.SetAttr("shadeColor", *format.Shade)
	}
	if format.Font != nil || format.EastAsianFont != nil {
		fontRef := ensureChild(node, "fontRef")
		for _, language := range languages {
			// The symbol slot holds the CJK punctuation (、。「」《》), which Word draws in the East Asian font.
			eastAsian := language == "HANGUL" || language == "HANJA" || language == "JAPANESE" || language == "SYMBOL"
			var face *string
			if eastAsian {
				face = format.EastAsianFont
				if face == nil {
					face = format.Font
				}
			} else {
				face = format.Font
				if face == nil {
					face = format.EastAsianFont
				}
			}
			if face != nil {
				fontRef.SetAttr(strings.ToLower(language), itoa(w.fontID(language, *face)))
			}
		}
	}
	if format.Spacing != nil {
		e := ensureChild(node, "spacing")
		for _, language := range languages {
			e.SetAttr(strings.ToLower(language), itoa(*format.Spacing))
		}
	}
	setFlag(node, "bold", format.Bold)
	setFlag(node, "italic", format.Italic)
	if format.Underline != nil || link {
		underline := ensureChild(node, "underline")
		on := link || isTrue(format.Underline)
		underline.SetAttr("type", map[bool]string{true: "BOTTOM", false: "NONE"}[on])
		underline.SetAttr("shape", "SOLID")
		color := val(format.Color, "#000000")
		if link {
			color = "#0000FF"
		}
		underline.SetAttr("color", color)
	}
	if format.Strike != nil {
		strikeout := ensureChild(node, "strikeout")
		strikeout.SetAttr("shape", map[bool]string{true: "SOLID", false: "NONE"}[*format.Strike])
		strikeout.SetAttr("color", val(format.Color, "#000000"))
	}
	switch {
	case isTrue(format.Superscript):
		removeChild(node, "subscript")
		ensureChild(node, "supscript")
	case isTrue(format.Subscript):
		removeChild(node, "supscript")
		ensureChild(node, "subscript")
	case isFalse(format.Superscript) || isFalse(format.Subscript):
		removeChild(node, "supscript")
		removeChild(node, "subscript")
	}

	w.refList("charProperties").Add(node)
	w.charPrs[id] = node
	w.charPrCache[key] = id
	return id
}

func removeChild(e *xmltree.Element, name string) {
	if c := e.ChildNS(nsHH, name); c != nil {
		c.Remove()
	}
}

func setFlag(charPr *xmltree.Element, name string, value *bool) {
	if value == nil {
		return
	}
	if *value {
		ensureChild(charPr, name)
	} else {
		removeChild(charPr, name)
	}
}

// ensureChild returns the child element, inserting it at its schema position if missing.
func ensureChild(charPr *xmltree.Element, name string) *xmltree.Element {
	if c := charPr.ChildNS(nsHH, name); c != nil {
		return c
	}
	element := hh(name)
	rank := indexOf(charPrOrder, name)
	var after *xmltree.Element
	for _, c := range charPr.Elements() {
		if r := indexOf(charPrOrder, c.Local); r >= 0 && r < rank {
			after = c
		}
	}
	if after != nil {
		after.AddAfterSelf(element)
	} else {
		charPr.AddFirst(element)
	}
	return element
}

// ───────────────────────── Paragraph shapes ─────────────────────────

// paraPr is the paragraph shape id for baseID with a format and list numbering applied.
func (w *writer) paraPr(baseID string, format doc.ParaFormat, list *doc.ListRef) string {
	numberingID := ""
	key := paraPrKeyFull{base: baseID, format: keyOfPara(format)}
	if list != nil {
		numberingID = w.numberingID(list.Numbering)
		key.numbering, key.level, key.numbered = numberingID, list.Level, list.Numbered
	}
	if paraEmpty(format) && list == nil {
		return baseID
	}
	if id, ok := w.paraPrCache[key]; ok {
		return id
	}
	source := w.paraPrs[baseID]
	if source == nil {
		source = w.head.FirstDescendantNS(nsHH, "paraPr")
	}
	node := source.Copy()
	w.maxParaPr++
	id := itoa(w.maxParaPr)
	node.SetAttr("id", id)

	if format.Align != nil {
		element := node.ChildNS(nsHH, "align")
		if element == nil {
			element = hh("align", "vertical", "BASELINE")
			node.AddFirst(element)
		}
		h := "LEFT"
		switch *format.Align {
		case doc.AlignCenter:
			h = "CENTER"
		case doc.AlignRight:
			h = "RIGHT"
		case doc.AlignJustify:
			h = "JUSTIFY"
		case doc.AlignDistribute:
			h = "DISTRIBUTE"
		}
		element.SetAttr("horizontal", h)
	}

	if list != nil {
		heading := node.ChildNS(nsHH, "heading")
		if heading == nil {
			heading = hh("heading")
			if align := node.ChildNS(nsHH, "align"); align != nil {
				align.AddAfterSelf(heading)
			} else {
				node.AddFirst(heading)
			}
		}
		if list.Numbered {
			heading.SetAttr("type", "NUMBER")
			heading.SetAttr("idRef", numberingID)
			heading.SetAttr("level", itoa(list.Level))
		} else {
			heading.SetAttr("type", "NONE")
			heading.SetAttr("idRef", "0")
			heading.SetAttr("level", "0")
		}
	}

	if bs := node.ChildNS(nsHH, "breakSetting"); bs != nil {
		flag := func(name string, v *bool) {
			if v != nil {
				bs.SetAttr(name, map[bool]string{true: "1", false: "0"}[*v])
			}
		}
		flag("keepWithNext", format.KeepWithNext)
		flag("keepLines", format.KeepLines)
		flag("widowOrphan", format.WidowOrphan)
	}

	// List items without their own indentation hang the bullet in front of the text.
	left := format.Left
	firstLine := format.FirstLine
	if list != nil && left == nil {
		const hanging = 2000
		left = ptr((list.Level + 1) * hanging)
		if firstLine == nil {
			if list.Numbered {
				firstLine = ptr(-hanging)
			} else {
				firstLine = ptr(0)
			}
		}
	}
	setSpacing(node, left, format.Right, firstLine, format.Before, format.After, format.LineSpacing)

	if len(format.Tabs) > 0 {
		node.SetAttr("tabPrIDRef", w.tabPrID(format.Tabs))
	}

	w.refList("paraProperties").Add(node)
	w.paraPrs[id] = node
	w.paraPrCache[key] = id
	return id
}

// setSpacing rewrites margins and line spacing. 한글 stores them twice: in hp:case (HwpUnitChar-aware readers, true
// HWPUNIT) and in hp:default for older readers, where lengths are doubled (percentages are not).
func setSpacing(paraPr *xmltree.Element, left, right, firstLine, before, after *int, lineSpacing *doc.LineSpacing) {
	existingCase := paraPr.FirstDescendantNS(nsHP, "case")
	current := func(name string) int {
		if existingCase == nil {
			return 0
		}
		if e := existingCase.FirstDescendantNS(nsHC, name); e != nil {
			v, _ := strconv.Atoi(e.A("value"))
			return v
		}
		return 0
	}
	var existingLine *xmltree.Element
	if existingCase != nil {
		existingLine = existingCase.ChildNS(nsHH, "lineSpacing")
	}
	if existingLine == nil {
		existingLine = paraPr.FirstDescendantNS(nsHH, "lineSpacing")
	}

	// The model (like Word) puts the first line at left + firstLine. 한글 differs for hanging indents: a negative
	// "intent" keeps the first line at "left" and moves the other lines right by its size, so the left margin is
	// reduced by the hanging amount (a positive intent works like Word's first-line indent).
	or := func(p *int, name string) int {
		if p != nil {
			return *p
		}
		return current(name)
	}
	hwpLeft := or(left, "left")
	if left != nil && firstLine != nil && *firstLine < 0 {
		hwpLeft += *firstLine
	}
	values := []struct {
		name  string
		value int
	}{
		{"intent", or(firstLine, "intent")},
		{"left", hwpLeft},
		{"right", or(right, "right")},
		{"prev", or(before, "prev")},
		{"next", or(after, "next")},
	}
	lineType := "PERCENT"
	if lineSpacing != nil {
		switch lineSpacing.Kind {
		case doc.SpacingFixed:
			lineType = "FIXED"
		case doc.SpacingAtLeast:
			lineType = "AT_LEAST"
		}
	} else if existingLine != nil {
		if t, ok := existingLine.Attr("type"); ok {
			lineType = t
		}
	}
	lineValue := 160
	if lineSpacing != nil {
		lineValue = lineSpacing.Value
	} else if existingLine != nil {
		if v, err := strconv.Atoi(existingLine.A("value")); err == nil {
			lineValue = v
		}
	}

	branch := func(name string, factor int) *xmltree.Element {
		line := lineValue
		if lineType != "PERCENT" {
			line = lineValue * factor
		}
		e := hp(name)
		margin := hh("margin")
		for _, v := range values {
			margin.Add(hc(v.name, "value", itoa(v.value*factor), "unit", "HWPUNIT"))
		}
		e.Add(margin, hh("lineSpacing", "type", lineType, "value", itoa(line), "unit", "HWPUNIT"))
		return e
	}
	caseBranch := branch("case", 1)
	caseBranch.Attrs = append([]xmltree.Attr{{Prefix: "hp", Space: nsHP, Local: "required-namespace", Value: hwpUnitCharNamespace}}, caseBranch.Attrs...)
	sw := hp("switch")
	sw.Add(caseBranch, branch("default", 2))

	for _, c := range paraPr.Elements() {
		if c.Is(nsHP, "switch") || c.Is(nsHH, "margin") || c.Is(nsHH, "lineSpacing") {
			c.Remove()
		}
	}
	// Order: align, heading, breakSetting, autoSpacing, switch, border.
	var anchor *xmltree.Element
	for _, name := range []string{"autoSpacing", "breakSetting", "heading", "align"} {
		if anchor = paraPr.ChildNS(nsHH, name); anchor != nil {
			break
		}
	}
	if anchor != nil {
		anchor.AddAfterSelf(sw)
	} else {
		paraPr.AddFirst(sw)
	}
}

// ───────────────────────── Numbering ─────────────────────────

func (w *writer) numberingID(numbering *doc.Numbering) string {
	if id, ok := w.numberingIDs[numbering]; ok {
		return id
	}
	numberings := w.refList("numberings")
	id := itoa(maxID(numberings.ChildrenNS(nsHH, "numbering")) + 1)
	start := 1
	if len(numbering.Levels) > 0 {
		start = numbering.Levels[0].Start
	}
	element := hh("numbering", "id", id, "start", itoa(start))
	for i := 0; i < len(numbering.Levels) && i < 10; i++ {
		level := numbering.Levels[i]
		head := hh("paraHead", "start", itoa(level.Start), "level", itoa(i+1), "align", "LEFT", "useInstWidth", "1",
			"autoIndent", "0", "widthAdjust", "0", "textOffsetType", "PERCENT", "textOffset", "50",
			"numFormat", level.Format, "charPrIDRef", "4294967295", "checkable", "0")
		if level.Text != "" {
			head.AddText(level.Text)
		}
		element.Add(head)
	}
	numberings.Add(element)
	w.numberingIDs[numbering] = id
	return id
}

// ───────────────────────── Borders and fills ─────────────────────────

func (w *writer) borderFillID(borders doc.Borders, fill string) string {
	key := borderFillKey{borders, fill}
	if id, ok := w.borderFills[key]; ok {
		return id
	}
	container := w.refList("borderFills")
	id := itoa(maxID(container.ChildrenNS(nsHH, "borderFill")) + 1)
	line := func(name string, b doc.Border) *xmltree.Element {
		return hh(name, "type", lineType(b.Style), "width", LineWidth(b.WidthMm), "color", b.Color)
	}
	face := fill
	if face == "" {
		face = "none"
	}
	element := hh("borderFill", "id", id, "threeD", "0", "shadow", "0", "centerLine", "NONE", "breakCellSeparateLine", "0")
	brush := hc("fillBrush")
	brush.Add(hc("winBrush", "faceColor", face, "hatchColor", "#000000", "alpha", "0"))
	element.Add(
		hh("slash", "type", "NONE", "Crooked", "0", "isCounter", "0"),
		hh("backSlash", "type", "NONE", "Crooked", "0", "isCounter", "0"),
		line("leftBorder", borders.Left), line("rightBorder", borders.Right),
		line("topBorder", borders.Top), line("bottomBorder", borders.Bottom),
		hh("diagonal", "type", "SOLID", "width", "0.1 mm", "color", "#000000"),
		brush)
	container.Add(element)
	w.borderFills[key] = id
	return id
}

func lineType(style doc.BorderStyle) string {
	switch style {
	case doc.BorderNone:
		return "NONE"
	case doc.BorderDash:
		return "DASH"
	case doc.BorderDot:
		return "DOT"
	case doc.BorderDashDot:
		return "DASH_DOT"
	case doc.BorderDouble:
		return "DOUBLE_SLIM"
	}
	return "SOLID"
}

// LineWidth snaps a width in mm to the nearest width 한글 offers, e.g. "0.12 mm".
func LineWidth(mm float64) string {
	nearest := lineWidths[0]
	for _, w := range lineWidths {
		if math.Abs(w-mm) < math.Abs(nearest-mm) {
			nearest = w
		}
	}
	if nearest >= 1 {
		return strconv.FormatFloat(nearest, 'f', 1, 64) + " mm"
	}
	s := strconv.FormatFloat(nearest, 'f', 2, 64)
	if strings.HasSuffix(s, "0") {
		s = s[:len(s)-1]
	}
	return s + " mm"
}

// ───────────────────────── Tab stops ─────────────────────────

func (w *writer) tabPrID(tabs []doc.TabStop) string {
	key := tabKey(tabs)
	if id, ok := w.tabPrIDs[key]; ok {
		return id
	}
	container := w.refList("tabProperties")
	id := itoa(maxID(container.ChildrenNS(nsHH, "tabPr")) + 1)
	element := hh("tabPr", "id", id, "autoTabLeft", "0", "autoTabRight", "0")
	for _, tab := range tabs {
		kind := "LEFT"
		switch tab.Kind {
		case doc.TabRight:
			kind = "RIGHT"
		case doc.TabCenter:
			kind = "CENTER"
		case doc.TabDecimal:
			kind = "DECIMAL"
		}
		// Same doubling rule as paragraph margins: hp:default holds twice the value and no unit.
		sw := hp("switch")
		c := hp("case")
		c.Attrs = append(c.Attrs, xmltree.Attr{Prefix: "hp", Space: nsHP, Local: "required-namespace", Value: hwpUnitCharNamespace})
		c.Add(hh("tabItem", "pos", itoa(tab.Position), "type", kind, "leader", tab.Leader, "unit", "HWPUNIT"))
		d := hp("default")
		d.Add(hh("tabItem", "pos", itoa(tab.Position*2), "type", kind, "leader", tab.Leader))
		sw.Add(c, d)
		element.Add(sw)
	}
	container.Add(element)
	w.tabPrIDs[key] = id
	return id
}
