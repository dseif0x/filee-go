package hwpx

// header.xml: fonts, character and paragraph shapes, numberings and bullets, border/fill definitions, tab stops and
// styles. Every shape a paragraph or run refers to is complete (no inheritance), so each id maps to one format.
//
// Lengths in paragraph shapes and tab stops come in two scales: hp:case branches for HwpUnitChar-aware readers hold
// true HWPUNIT, while hp:default branches and shapes without a switch (older files) hold them doubled.

import (
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/hwp/xmltree"
)

type borderFill struct {
	borders doc.Borders
	fill    string
}

type headerState struct {
	fonts       map[[2]string]string
	charPrs     map[string]*xmltree.Element
	paraPrs     map[string]*xmltree.Element
	borderFillE map[string]*xmltree.Element
	tabPrs      map[string]*xmltree.Element
	numberingE  map[string]*xmltree.Element
	bulletE     map[string]*xmltree.Element
	styles      map[string]*xmltree.Element

	charFormats map[string]doc.CharFormat
	paraShapes  map[string]*paraShape
	numberings  map[string]*doc.Numbering
	borderFills map[string]*borderFill
}

// paraShape is a paragraph shape: format plus the list or outline it belongs to. headingType is the OWPML heading
// type (NONE, OUTLINE, NUMBER or BULLET), headingLevel the 0-based level.
type paraShape struct {
	format          doc.ParaFormat
	headingType     string
	headingID       string
	headingLevel    int
	pageBreakBefore bool
}

func (r *hwpxReader) loadHeader(head *xmltree.Element) {
	h := &r.header
	h.fonts = map[[2]string]string{}
	h.charPrs = map[string]*xmltree.Element{}
	h.paraPrs = map[string]*xmltree.Element{}
	h.borderFillE = map[string]*xmltree.Element{}
	h.tabPrs = map[string]*xmltree.Element{}
	h.numberingE = map[string]*xmltree.Element{}
	h.bulletE = map[string]*xmltree.Element{}
	h.styles = map[string]*xmltree.Element{}
	h.charFormats = map[string]doc.CharFormat{}
	h.paraShapes = map[string]*paraShape{}
	h.numberings = map[string]*doc.Numbering{}
	h.borderFills = map[string]*borderFill{}
	add := func(m map[string]*xmltree.Element, id string, e *xmltree.Element) {
		if _, ok := m[id]; !ok {
			m[id] = e
		}
	}
	head.Walk(func(e *xmltree.Element) bool {
		id, hasID := e.Attr("id")
		parent := ""
		if e.Parent != nil {
			parent = e.Parent.Local
		}
		if !hasID {
			return true
		}
		switch e.Local {
		case "font":
			if parent == "fontface" {
				h.fonts[[2]string{e.Parent.A("lang"), id}] = e.A("face")
			}
		case "charPr":
			add(h.charPrs, id, e)
		case "paraPr":
			add(h.paraPrs, id, e)
		case "borderFill":
			add(h.borderFillE, id, e)
		case "tabPr":
			add(h.tabPrs, id, e)
		case "numbering":
			if parent == "numberings" {
				add(h.numberingE, id, e)
			}
		case "bullet":
			if parent == "bullets" {
				add(h.bulletE, id, e)
			}
		case "style":
			add(h.styles, id, e)
		}
		return true
	})
}

// ───────────────────────── Character shapes ─────────────────────────

// charFormat is the complete character format of a charPr id (every member set, so the look survives other styles).
func (r *hwpxReader) charFormat(id string) doc.CharFormat {
	h := &r.header
	if id == "" {
		id = "0"
	}
	if f, ok := h.charFormats[id]; ok {
		return f
	}
	charPr := h.charPrs[id]
	if charPr == nil {
		charPr = h.charPrs["0"]
	}
	if charPr == nil {
		h.charFormats[id] = doc.CharFormat{}
		return doc.CharFormat{}
	}
	fontRef := child(charPr, "fontRef")
	face := func(language string) *string {
		if fontID, ok := fontRef.Attr(strings.ToLower(language)); ok {
			if f := h.fonts[[2]string{language, fontID}]; f != "" {
				return ptr(f)
			}
		}
		return nil
	}
	strike, hasStrike := child(charPr, "strikeout").Attr("shape")
	color := hwpColor(charPr.A("textColor"))
	if color == "" {
		color = "#000000"
	}
	f := doc.CharFormat{
		Bold:      ptr(child(charPr, "bold") != nil),
		Italic:    ptr(child(charPr, "italic") != nil),
		Underline: ptr(child(charPr, "underline").A("type") == "BOTTOM"),
		// "3D" is 한글's placeholder for "no strikeout" in some versions, like NONE.
		Strike:        ptr(hasStrike && strike != "NONE" && strike != "3D"),
		Superscript:   ptr(child(charPr, "supscript") != nil),
		Subscript:     ptr(child(charPr, "subscript") != nil),
		Size:          ptr(intAttr(charPr, "height", 1000)),
		Font:          face("LATIN"),
		EastAsianFont: face("HANGUL"),
		Color:         ptr(color),
		Spacing:       ptr(intAttr(child(charPr, "spacing"), "hangul", 0)),
	}
	if shade := hwpColor(charPr.A("shadeColor")); shade != "" {
		f.Shade = ptr(shade)
	}
	h.charFormats[id] = f
	return f
}

// ───────────────────────── Paragraph shapes ─────────────────────────

func (r *hwpxReader) paraShapeOf(id string) *paraShape {
	h := &r.header
	if id == "" {
		id = "0"
	}
	if s, ok := h.paraShapes[id]; ok {
		return s
	}
	paraPr := h.paraPrs[id]
	if paraPr == nil {
		paraPr = h.paraPrs["0"]
	}
	if paraPr == nil {
		s := &paraShape{headingType: "NONE"}
		h.paraShapes[id] = s
		return s
	}
	heading := child(paraPr, "heading")
	breaks := child(paraPr, "breakSetting")
	intent, left, right, before, after, lineSpacing := spacingOf(paraPr)
	align := doc.AlignLeft
	switch child(paraPr, "align").A("horizontal") {
	case "CENTER":
		align = doc.AlignCenter
	case "RIGHT":
		align = doc.AlignRight
	case "JUSTIFY":
		align = doc.AlignJustify
	case "DISTRIBUTE", "DISTRIBUTE_SPACE":
		align = doc.AlignDistribute
	}
	// A negative "intent" (hanging indent) keeps the first line at "left" and moves the other lines right; the model,
	// like Word, measures "left" to the other lines and puts the first line at left + firstLine.
	modelLeft := left
	if intent < 0 {
		modelLeft = left - intent
	}
	format := doc.ParaFormat{
		Align:        ptr(align),
		Left:         ptr(modelLeft),
		Right:        ptr(right),
		FirstLine:    ptr(intent),
		Before:       ptr(before),
		After:        ptr(after),
		LineSpacing:  &lineSpacing,
		KeepWithNext: ptr(flag(breaks, "keepWithNext", false)),
		KeepLines:    ptr(flag(breaks, "keepLines", false)),
		WidowOrphan:  ptr(flag(breaks, "widowOrphan", false)),
		Tabs:         r.tabStops(paraPr.A("tabPrIDRef")),
	}
	headingType, ok := heading.Attr("type")
	if !ok {
		headingType = "NONE"
	}
	s := &paraShape{format, headingType, heading.A("idRef"), max(0, intAttr(heading, "level", 0)), flag(breaks, "pageBreakBefore", false)}
	h.paraShapes[id] = s
	return s
}

// spacingOf is the margins and line spacing from the switch branch with exact values, else the doubled ones halved.
func spacingOf(paraPr *xmltree.Element) (intent, left, right, before, after int, lineSpacing doc.LineSpacing) {
	var margin, line *xmltree.Element
	marginExact, lineExact := false, false
	for _, c := range paraPr.Elements() {
		switch c.Local {
		case "switch":
			b, exact := branch(c)
			if m := b.Child("margin"); m != nil {
				margin, marginExact = m, exact
			}
			if l := b.Child("lineSpacing"); l != nil {
				line, lineExact = l, exact
			}
		case "margin":
			if margin == nil {
				margin = c
			}
		case "lineSpacing":
			if line == nil {
				line = c
			}
		}
	}
	length := func(name, attribute string) int {
		// <hc:left value="..."/> children, or attributes on hh:margin in older files.
		var value int
		if e := margin.Child(name); e != nil {
			value = intAttr(e, "value", 0)
		} else {
			value = intAttr(margin, attribute, 0)
		}
		if marginExact {
			return value
		}
		return iround(float64(value) / 2)
	}
	kind, ok := line.Attr("type")
	if !ok {
		kind = "PERCENT"
	}
	value := intAttr(line, "value", 160)
	l := value
	if !lineExact {
		l = iround(float64(value) / 2)
	}
	switch kind {
	case "FIXED":
		lineSpacing = doc.LineSpacing{Kind: doc.SpacingFixed, Value: l}
	case "AT_LEAST", "MINIMUM":
		lineSpacing = doc.LineSpacing{Kind: doc.SpacingAtLeast, Value: l}
	case "BETWEEN_LINES", "SPACE_ONLY", "SPACEONLY":
		// "Space between lines only": a 10 pt line plus the gap, as the model has no such kind.
		lineSpacing = doc.LineSpacing{Kind: doc.SpacingAtLeast, Value: 1000 + l}
	default:
		lineSpacing = doc.LineSpacing{Kind: doc.SpacingPercent, Value: value}
	}
	return length("intent", "indent"), length("left", "left"), length("right", "right"), length("prev", "prev"), length("next", "next"), lineSpacing
}

// ───────────────────────── Tab stops ─────────────────────────

func (r *hwpxReader) tabStops(tabPrID string) []doc.TabStop {
	tabPr := r.header.tabPrs[tabPrID]
	if tabPrID == "" || tabPr == nil {
		return nil
	}
	var stops []doc.TabStop
	for _, c := range tabPr.Elements() {
		var items []*xmltree.Element
		var exact bool
		switch c.Local {
		case "switch":
			b, e := branch(c)
			items, exact = b.Elements(), e
		case "tabItem":
			// A tab item outside a switch is exact when it says so (unit="HWPUNIT"), else in the doubled scale.
			items, exact = []*xmltree.Element{c}, c.A("unit") == "HWPUNIT"
		default:
			continue
		}
		for _, item := range items {
			if item.Local != "tabItem" {
				continue
			}
			position := intAttr(item, "pos", 0)
			if !exact {
				position /= 2
			}
			kind := doc.TabLeft
			switch item.A("type") {
			case "RIGHT":
				kind = doc.TabRight
			case "CENTER":
				kind = doc.TabCenter
			case "DECIMAL":
				kind = doc.TabDecimal
			}
			leader, ok := item.Attr("leader")
			if !ok {
				leader = "NONE"
			}
			stops = append(stops, doc.TabStop{Position: position, Kind: kind, Leader: leader})
		}
	}
	return stops
}

// ───────────────────────── Numbering and bullets ─────────────────────────

// numbering is the list definition of a numbering (hh:numbering) id; each paraHead is one level ("^1." style text).
func (r *hwpxReader) numbering(id string) *doc.Numbering {
	if id == "" {
		return nil
	}
	h := &r.header
	if n, ok := h.numberings["n"+id]; ok {
		return n
	}
	var numbering *doc.Numbering
	if element := h.numberingE[id]; element != nil {
		numbering = &doc.Numbering{}
		start := intAttr(element, "start", 1)
		heads := children(element, "paraHead")
		sort.SliceStable(heads, func(i, j int) bool { return intAttr(heads[i], "level", 1) < intAttr(heads[j], "level", 1) })
		for _, head := range heads {
			text := head.Value()
			// A level without a counter (^n) is a symbol: Filee writes bullets that way.
			bullet := text != "" && !hasCounter(text)
			format, ok := head.Attr("numFormat")
			if !ok {
				format = "DIGIT"
			}
			numbering.Levels = append(numbering.Levels, doc.NumberingLevel{Format: format, Text: text, Start: intAttr(head, "start", max(start, 1)), Bullet: bullet})
		}
	}
	h.numberings["n"+id] = numbering
	return numbering
}

// bullet is the list definition of a bullet (hh:bullet) id: the same symbol on every level.
func (r *hwpxReader) bullet(id string) *doc.Numbering {
	if id == "" {
		return nil
	}
	h := &r.header
	if n, ok := h.numberings["b"+id]; ok {
		return n
	}
	var numbering *doc.Numbering
	if element := h.bulletE[id]; element != nil {
		symbol := "●"
		if c := element.A("char"); c != "" && !flag(element, "useImage", false) {
			symbol = bulletSymbol(c)
		}
		numbering = &doc.Numbering{}
		for i := 0; i < 10; i++ {
			numbering.Levels = append(numbering.Levels, doc.NumberingLevel{Format: "DIGIT", Text: symbol, Start: 1, Bullet: true})
		}
	}
	h.numberings["b"+id] = numbering
	return numbering
}

func hasCounter(text string) bool {
	for i := 0; i+1 < len(text); i++ {
		if text[i] == '^' && unicode.IsDigit(rune(text[i+1])) {
			return true
		}
	}
	return false
}

// bulletSymbol: symbol-font bullets use private-use code points that other programs do not show.
func bulletSymbol(text string) string {
	for _, c := range text {
		if c >= 0xE000 && c <= 0xF8FF {
			return "●"
		}
		break
	}
	return text
}

// ───────────────────────── Borders and fills ─────────────────────────

// borderFillOf is the borders and background of a borderFill id; nil when the id is unknown.
func (r *hwpxReader) borderFillOf(id string) *borderFill {
	if id == "" {
		return nil
	}
	h := &r.header
	if b, ok := h.borderFills[id]; ok {
		return b
	}
	var result *borderFill
	if element := h.borderFillE[id]; element != nil {
		edge := func(name string) doc.Border { return hwpBorder(child(element, name)) }
		brush := child(element, "fillBrush")
		fill := hwpColor(child(brush, "winBrush").A("faceColor"))
		if fill == "" {
			for _, c := range children(child(brush, "gradation"), "color") {
				if v := hwpColor(c.A("value")); v != "" {
					fill = v
					break
				}
			}
		}
		result = &borderFill{doc.Borders{Left: edge("leftBorder"), Right: edge("rightBorder"), Top: edge("topBorder"), Bottom: edge("bottomBorder")}, fill}
	}
	h.borderFills[id] = result
	return result
}

// hwpBorder is a cell or page border: line type, width like "0.12 mm" and colour.
func hwpBorder(edge *xmltree.Element) doc.Border {
	style := lineStyleOf(edge.A("type"), edge != nil && hasAttr(edge, "type"))
	if edge == nil || style == doc.BorderNone {
		return doc.NoBorder
	}
	width, ok := edge.Attr("width")
	if !ok {
		width = "0.12"
	}
	i := 0
	for i < len(width) && (width[i] >= '0' && width[i] <= '9' || width[i] == '.') {
		i++
	}
	mm, err := strconv.ParseFloat(width[:i], 64)
	if err != nil || mm <= 0 {
		mm = 0.12
	}
	color := hwpColor(edge.A("color"))
	if color == "" {
		color = "#000000"
	}
	return doc.Border{Style: style, WidthMm: mm, Color: color}
}

func hasAttr(e *xmltree.Element, name string) bool {
	_, ok := e.Attr(name)
	return ok
}

// lineStyleOf maps OWPML line types onto the few styles the model (and Word) share.
func lineStyleOf(kind string, present bool) doc.BorderStyle {
	if !present {
		return doc.BorderNone
	}
	switch kind {
	case "NONE":
		return doc.BorderNone
	case "DASH", "LONG_DASH":
		return doc.BorderDash
	case "DOT", "CIRCLE":
		return doc.BorderDot
	case "DASH_DOT", "DASH_DOT_DOT":
		return doc.BorderDashDot
	case "DOUBLE_SLIM", "SLIM_THICK", "THICK_SLIM", "SLIM_THICK_SLIM", "DOUBLE_WAVE":
		return doc.BorderDouble
	}
	return doc.BorderSolid
}

// ───────────────────────── Styles ─────────────────────────

// styleHeadingLevel is the heading level 1..9 of a style named "Heading n" (as Filee writes them), else 0.
func (r *hwpxReader) styleHeadingLevel(styleID string) int {
	style := r.header.styles[styleID]
	if styleID == "" || style == nil {
		return 0
	}
	name := style.A("engName")
	if len(name) > 8 && strings.EqualFold(name[:8], "Heading ") {
		if level, err := strconv.Atoi(name[8:]); err == nil && level >= 1 && level <= 9 {
			return level
		}
	}
	return 0
}
