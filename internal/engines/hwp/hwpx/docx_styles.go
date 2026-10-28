package hwpx

// DOCX formatting: styles (with basedOn inheritance), document defaults, theme fonts and list numbering, resolved to
// the same property bags as direct formatting so each paragraph and run gets one merged format.

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/hwp/xmltree"
)

// wordLineToHwpPercent: 한글 line spacing is a percentage of the font size, Word's "multiple" of the font's natural
// line height (about 1.3 em for the fonts Korean documents use). Word "single" therefore corresponds to about 130%.
const wordLineToHwpPercent = 130

// ───────────────────────── Property bags ─────────────────────────

type tabDef struct {
	position     int
	kind, leader string
}

// paraProps are paragraph properties; later layers (style → numbering → direct) override earlier ones.
type paraProps struct {
	jc                                                   string
	left, right, firstLine, hanging, before, after, line *int
	// Indents in hundredths of a character (w:leftChars ...), which Chinese and Japanese documents use. A non-zero
	// value wins over the twips one; "0" means the twips value counts again.
	leftChars, rightChars, firstLineChars, hangingChars            *int
	lineRule                                                       string
	keepNext, keepLines, pageBreakBefore, widowControl, snapToGrid *bool
	outlineLevel                                                   *int
	numID                                                          string
	hasNumID                                                       bool
	ilvl                                                           *int
	tabs                                                           []tabDef
}

func (p *paraProps) clone() *paraProps {
	c := *p
	c.tabs = append([]tabDef(nil), p.tabs...)
	return &c
}

func (p *paraProps) apply(pPr *xmltree.Element) {
	if pPr == nil {
		return
	}
	if jc := pPr.ChildNS(nsW, "jc"); jc != nil {
		p.jc = wval(jc)
	}
	if ind := pPr.ChildNS(nsW, "ind"); ind != nil {
		p.left = firstOf(twips(ind, "left"), twips(ind, "start"), p.left)
		p.right = firstOf(twips(ind, "right"), twips(ind, "end"), p.right)
		if h := twips(ind, "hanging"); h != nil {
			p.hanging, p.firstLine, p.firstLineChars = h, nil, nil
		}
		if f := twips(ind, "firstLine"); f != nil {
			p.firstLine, p.hanging, p.hangingChars = f, nil, nil
		}
		nonZero := func(v *int) *int {
			if *v != 0 {
				return v
			}
			return nil
		}
		if v := firstOf(twips(ind, "leftChars"), twips(ind, "startChars"), nil); v != nil {
			p.leftChars = nonZero(v)
		}
		if v := firstOf(twips(ind, "rightChars"), twips(ind, "endChars"), nil); v != nil {
			p.rightChars = nonZero(v)
		}
		if v := twips(ind, "hangingChars"); v != nil {
			p.hangingChars = nonZero(v)
			if *v != 0 {
				p.firstLine, p.firstLineChars = nil, nil
			}
		}
		if v := twips(ind, "firstLineChars"); v != nil {
			p.firstLineChars = nonZero(v)
			if *v != 0 {
				p.hanging, p.hangingChars = nil, nil
			}
		}
	}
	if spacing := pPr.ChildNS(nsW, "spacing"); spacing != nil {
		if isTrue(boolAttr(spacing, "beforeAutospacing")) {
			p.before = ptr(280)
		} else if v := twips(spacing, "before"); v != nil {
			p.before = v
		}
		if isTrue(boolAttr(spacing, "afterAutospacing")) {
			p.after = ptr(280)
		} else if v := twips(spacing, "after"); v != nil {
			p.after = v
		}
		if line := twips(spacing, "line"); line != nil {
			p.line, p.lineRule = line, or(wattr(spacing, "lineRule"), "auto")
		}
	}
	p.keepNext = toggleOr(pPr, "keepNext", p.keepNext)
	p.keepLines = toggleOr(pPr, "keepLines", p.keepLines)
	p.pageBreakBefore = toggleOr(pPr, "pageBreakBefore", p.pageBreakBefore)
	p.widowControl = toggleOr(pPr, "widowControl", p.widowControl)
	p.snapToGrid = toggleOr(pPr, "snapToGrid", p.snapToGrid)
	if outline := pPr.ChildNS(nsW, "outlineLvl"); outline != nil {
		if level, err := strconv.Atoi(wval(outline)); err == nil {
			p.outlineLevel = ptr(level)
		}
	}
	if numPr := pPr.ChildNS(nsW, "numPr"); numPr != nil {
		if v, ok := wattrOK(numPr.ChildNS(nsW, "numId"), "val"); ok {
			p.numID, p.hasNumID = v, true
		}
		if ilvl := numPr.ChildNS(nsW, "ilvl"); ilvl != nil {
			if v, err := strconv.Atoi(wval(ilvl)); err == nil {
				p.ilvl = ptr(v)
			}
		}
	}
	if tabs := pPr.ChildNS(nsW, "tabs"); tabs != nil {
		for _, tab := range tabs.ChildrenNS(nsW, "tab") {
			position := val(twips(tab, "pos"), 0)
			kept := p.tabs[:0]
			for _, t := range p.tabs {
				if t.position != position {
					kept = append(kept, t)
				}
			}
			p.tabs = kept
			if kind := wval(tab); kind != "" && kind != "clear" {
				p.tabs = append(p.tabs, tabDef{position, kind, or(wattr(tab, "leader"), "none")})
			}
		}
		sort.SliceStable(p.tabs, func(i, j int) bool { return p.tabs[i].position < p.tabs[j].position })
	}
}

func firstOf(a, b, c *int) *int {
	if a != nil {
		return a
	}
	if b != nil {
		return b
	}
	return c
}

// runProps are run (character) properties.
type runProps struct {
	bold, italic, underline, strike, hidden                                        *bool
	vertAlign, color, highlight, shade, ascii, eastAsia, asciiTheme, eastAsiaTheme string
	size, spacing                                                                  *int
}

func (r *runProps) clone() *runProps {
	c := *r
	return &c
}

func (r *runProps) apply(rPr *xmltree.Element) {
	if rPr == nil {
		return
	}
	r.bold = toggleOr(rPr, "b", r.bold)
	r.italic = toggleOr(rPr, "i", r.italic)
	if s := toggle(rPr, "strike"); s != nil {
		r.strike = s
	} else if s := toggle(rPr, "dstrike"); s != nil {
		r.strike = s
	}
	r.hidden = toggleOr(rPr, "vanish", r.hidden)
	if u := rPr.ChildNS(nsW, "u"); u != nil {
		v, ok := wattrOK(u, "val")
		r.underline = ptr(ok && v != "none")
	}
	if va := rPr.ChildNS(nsW, "vertAlign"); va != nil {
		r.vertAlign = wval(va)
	}
	if c := rPr.ChildNS(nsW, "color"); c != nil {
		if v := wval(c); len(v) == 6 {
			r.color = "#" + strings.ToUpper(v)
		} else {
			r.color = ""
		}
	}
	if h := rPr.ChildNS(nsW, "highlight"); h != nil {
		r.highlight = highlightColor(wval(h))
	}
	if shd := rPr.ChildNS(nsW, "shd"); shd != nil {
		r.shade = shadingColor(shd)
	}
	if sz := rPr.ChildNS(nsW, "sz"); sz != nil {
		if v, err := strconv.Atoi(wval(sz)); err == nil {
			r.size = ptr(v)
		}
	}
	if sp := rPr.ChildNS(nsW, "spacing"); sp != nil {
		if v, err := strconv.Atoi(wval(sp)); err == nil {
			r.spacing = ptr(v)
		}
	}
	if fonts := rPr.ChildNS(nsW, "rFonts"); fonts != nil {
		// A theme reference wins over an explicit name in Word, so a later explicit name clears it.
		if a := or(wattr(fonts, "ascii"), wattr(fonts, "hAnsi")); a != "" {
			r.ascii, r.asciiTheme = a, ""
		}
		if t := or(wattr(fonts, "asciiTheme"), wattr(fonts, "hAnsiTheme")); t != "" {
			r.asciiTheme = t
		}
		if e := wattr(fonts, "eastAsia"); e != "" {
			r.eastAsia, r.eastAsiaTheme = e, ""
		}
		if t := wattr(fonts, "eastAsiaTheme"); t != "" {
			r.eastAsiaTheme = t
		}
	}
}

type docxStyle struct {
	id, kind, name, basedOn string
	pPr, rPr, tblPr, tcPr   *xmltree.Element
	conditions              []*xmltree.Element
}

type numberingLevel struct {
	start    int
	format   string
	text     string
	pPr, rPr *xmltree.Element
}

type resolvedStyle struct {
	para *paraProps
	run  *runProps
}

// ───────────────────────── Loading ─────────────────────────

func (r *docxReader) loadStyles(part *docxPart) {
	if part == nil {
		return
	}
	root := part.root
	defaults := root.ChildNS(nsW, "docDefaults")
	r.defaultPara.apply(defaults.ChildNS(nsW, "pPrDefault").ChildNS(nsW, "pPr"))
	r.defaultRun.apply(defaults.ChildNS(nsW, "rPrDefault").ChildNS(nsW, "rPr"))
	for _, e := range root.ChildrenNS(nsW, "style") {
		s := &docxStyle{
			id: wattr(e, "styleId"), kind: or(wattr(e, "type"), "paragraph"),
			name: wval(e.ChildNS(nsW, "name")), basedOn: wval(e.ChildNS(nsW, "basedOn")),
			pPr: e.ChildNS(nsW, "pPr"), rPr: e.ChildNS(nsW, "rPr"), tblPr: e.ChildNS(nsW, "tblPr"), tcPr: e.ChildNS(nsW, "tcPr"),
			conditions: e.ChildrenNS(nsW, "tblStylePr"),
		}
		r.styles[s.id] = s
		if isTrue(boolAttr(e, "default")) {
			switch s.kind {
			case "paragraph":
				r.defaultParaStyle = s.id
			case "table":
				r.defaultTableStyle = s.id
			}
		}
	}
}

func (r *docxReader) loadTheme(part *docxPart) {
	if part == nil {
		return
	}
	fonts := part.root.FirstDescendantNS(nsA, "fontScheme")
	read := func(name string) [2]string {
		font := fonts.ChildNS(nsA, name)
		latin, _ := font.ChildNS(nsA, "latin").Attr("typeface")
		eastAsian, _ := font.ChildNS(nsA, "ea").Attr("typeface")
		if eastAsian == "" {
			for _, f := range font.ChildrenNS(nsA, "font") {
				if f.A("script") == "Hang" {
					eastAsian = f.A("typeface")
					break
				}
			}
		}
		return [2]string{nullIfEmpty(latin), nullIfEmpty(eastAsian)}
	}
	r.minorFont = read("minorFont")
	r.majorFont = read("majorFont")
}

func (r *docxReader) loadNumbering(part *docxPart) {
	if part == nil {
		return
	}
	root := part.root
	abstracts := map[string]*xmltree.Element{}
	for _, a := range root.ChildrenNS(nsW, "abstractNum") {
		abstracts[wattr(a, "abstractNumId")] = a
	}
	applyLevel := func(levels []*numberingLevel, lvl *xmltree.Element) {
		index, err := strconv.Atoi(wattr(lvl, "ilvl"))
		if err != nil || index < 0 || index > 8 {
			return
		}
		level := levels[index]
		if start, err := strconv.Atoi(wval(lvl.ChildNS(nsW, "start"))); err == nil {
			level.start = start
		}
		if f, ok := wattrOK(lvl.ChildNS(nsW, "numFmt"), "val"); ok {
			level.format = f
		}
		if t, ok := wattrOK(lvl.ChildNS(nsW, "lvlText"), "val"); ok {
			level.text = t
		}
		if p := lvl.ChildNS(nsW, "pPr"); p != nil {
			level.pPr = p
		}
		if p := lvl.ChildNS(nsW, "rPr"); p != nil {
			level.rPr = p
		}
	}
	for _, num := range root.ChildrenNS(nsW, "num") {
		numID := wattr(num, "numId")
		abstract, ok := abstracts[wval(num.ChildNS(nsW, "abstractNumId"))]
		if !ok {
			continue
		}
		levels := make([]*numberingLevel, 9)
		for i := range levels {
			levels[i] = &numberingLevel{start: 1, format: "decimal", text: fmt.Sprintf("%%%d.", i+1)}
		}
		for _, lvl := range abstract.ChildrenNS(nsW, "lvl") {
			applyLevel(levels, lvl)
		}
		for _, over := range num.ChildrenNS(nsW, "lvlOverride") {
			index, err := strconv.Atoi(wattr(over, "ilvl"))
			if err != nil || index < 0 || index > 8 {
				continue
			}
			if lvl := over.ChildNS(nsW, "lvl"); lvl != nil {
				applyLevel(levels, lvl)
			}
			if start, err := strconv.Atoi(wval(over.ChildNS(nsW, "startOverride"))); err == nil {
				levels[index].start = start
			}
		}
		r.numberingLevels[numID] = levels
	}
}

// ───────────────────────── Resolution ─────────────────────────

// resolveStyle merges the properties of a paragraph or character style including everything it is based on.
func (r *docxReader) resolveStyle(styleID string) (*paraProps, *runProps) {
	style, ok := r.styles[styleID]
	if styleID == "" || !ok {
		return r.defaultPara.clone(), r.defaultRun.clone()
	}
	if cached, ok := r.resolvedStyles()[styleID]; ok {
		return cached.para.clone(), cached.run.clone()
	}
	// Guard against basedOn cycles in damaged files.
	r.resolvedStyles()[styleID] = resolvedStyle{r.defaultPara.clone(), r.defaultRun.clone()}
	para, run := r.defaultPara.clone(), r.defaultRun.clone()
	if style.basedOn != "" && style.basedOn != styleID {
		para, run = r.resolveStyle(style.basedOn)
	}
	para.apply(style.pPr)
	run.apply(style.rPr)
	r.resolvedStyles()[styleID] = resolvedStyle{para.clone(), run.clone()}
	return para, run
}

func (r *docxReader) resolvedStyles() map[string]resolvedStyle {
	if r.resolvedCache == nil {
		r.resolvedCache = map[string]resolvedStyle{}
	}
	return r.resolvedCache
}

// paragraphProperties: defaults → style chain → list level → direct formatting.
func (r *docxReader) paragraphProperties(pPr *xmltree.Element) (*paraProps, *runProps) {
	styleID := wval(pPr.ChildNS(nsW, "pStyle"))
	if styleID == "" {
		styleID = r.defaultParaStyle
	}
	para, run := r.resolveStyle(styleID)
	direct := &paraProps{}
	direct.apply(pPr)
	numID, hasNumID := direct.numID, direct.hasNumID
	if !hasNumID {
		numID, hasNumID = para.numID, para.hasNumID
	}
	ilvl := 0
	if direct.ilvl != nil {
		ilvl = *direct.ilvl
	} else if para.ilvl != nil {
		ilvl = *para.ilvl
	}
	if levels, ok := r.numberingLevels[numID]; hasNumID && ok {
		para.apply(levels[clamp(ilvl, 0, 8)].pPr)
	}
	// numId 0 on the paragraph itself turns numbering off, and Word then drops the indents the paragraph would
	// inherit for the list (a style's first-line indent too): only the paragraph's own indents count.
	if direct.hasNumID && direct.numID == "0" {
		para.left, para.firstLine, para.hanging = nil, nil, nil
		para.leftChars, para.firstLineChars, para.hangingChars = nil, nil, nil
	}
	para.apply(pPr)
	para.numID, para.hasNumID = numID, hasNumID
	para.ilvl = ptr(ilvl)
	run.apply(pPr.ChildNS(nsW, "rPr"))
	return para, run
}

// runProperties are the properties of a run inside a paragraph whose style gave paragraphRun.
func (r *docxReader) runProperties(rPr *xmltree.Element, paragraphRun *runProps) *runProps {
	run := paragraphRun.clone()
	if style, ok := r.styles[wval(rPr.ChildNS(nsW, "rStyle"))]; ok {
		// Character styles only carry run properties; apply their chain on top of the paragraph's.
		var chain []*docxStyle
		for current := style; current != nil && len(chain) < 20; current = r.styles[current.basedOn] {
			chain = append(chain, current)
			if current.basedOn == "" {
				break
			}
		}
		for i := len(chain) - 1; i >= 0; i-- {
			run.apply(chain[i].rPr)
		}
	}
	run.apply(rPr)
	return run
}

// headingLevel is 1..9 from the outline level (Word's heading styles set it), else 0.
func (r *docxReader) headingLevel(para *paraProps, pPr *xmltree.Element) int {
	if para.outlineLevel != nil && *para.outlineLevel >= 0 && *para.outlineLevel <= 8 {
		return *para.outlineLevel + 1
	}
	name := ""
	if s, ok := r.styles[wval(pPr.ChildNS(nsW, "pStyle"))]; ok {
		name = s.name
	}
	if len(name) > 8 && strings.EqualFold(name[:8], "heading ") {
		if level, err := strconv.Atoi(name[8:]); err == nil && level >= 1 && level <= 9 {
			return level
		}
	}
	return 0
}

// paraFormat: fontSize is the paragraph's font size (half points): one character of the *Chars indents.
func (r *docxReader) paraFormat(p *paraProps, fontSize *int) doc.ParaFormat {
	// A character is the paragraph's font size: 10.5 pt (21 half points) = 210 twips.
	character := float64(val(fontSize, 20) * 10)
	chars := func(hundredths *int) *int {
		if hundredths == nil {
			return nil
		}
		return ptr(iround(float64(*hundredths) * character / 100.0))
	}
	left := firstOf(chars(p.leftChars), p.left, nil)
	right := firstOf(chars(p.rightChars), p.right, nil)
	var firstLine int
	switch {
	case chars(p.hangingChars) != nil:
		firstLine = -*chars(p.hangingChars)
	case chars(p.firstLineChars) != nil:
		firstLine = *chars(p.firstLineChars)
	case p.hanging != nil:
		firstLine = -*p.hanging
	default:
		firstLine = val(p.firstLine, 0)
	}
	lineSpacing := doc.LineSpacing{Kind: doc.SpacingPercent, Value: wordLineToHwpPercent}
	if p.line != nil {
		switch p.lineRule {
		case "exact":
			lineSpacing = doc.LineSpacing{Kind: doc.SpacingFixed, Value: doc.FromTwips(float64(*p.line))}
		case "atLeast":
			lineSpacing = doc.LineSpacing{Kind: doc.SpacingAtLeast, Value: doc.FromTwips(float64(*p.line))}
		default:
			lineSpacing = doc.LineSpacing{Kind: doc.SpacingPercent, Value: iround(float64(*p.line) / 240.0 * wordLineToHwpPercent)}
		}
	}
	align := doc.AlignLeft
	switch p.jc {
	case "center":
		align = doc.AlignCenter
	case "right", "end":
		align = doc.AlignRight
	case "both", "lowKashida", "mediumKashida", "highKashida", "thaiDistribute":
		align = doc.AlignJustify
	case "distribute":
		align = doc.AlignDistribute
	}
	f := doc.ParaFormat{
		Align:        ptr(align),
		Left:         ptr(doc.FromTwips(float64(val(left, 0)))),
		Right:        ptr(doc.FromTwips(float64(val(right, 0)))),
		FirstLine:    ptr(doc.FromTwips(float64(firstLine))),
		Before:       ptr(doc.FromTwips(float64(val(p.before, 0)))),
		After:        ptr(doc.FromTwips(float64(val(p.after, 0)))),
		LineSpacing:  &lineSpacing,
		KeepWithNext: ptr(val(p.keepNext, false)),
		KeepLines:    ptr(val(p.keepLines, false)),
		WidowOrphan:  ptr(val(p.widowControl, true)),
	}
	if len(p.tabs) > 0 {
		f.Tabs = []doc.TabStop{}
		for _, t := range p.tabs {
			if t.kind == "bar" || t.kind == "num" {
				continue
			}
			kind := doc.TabLeft
			switch t.kind {
			case "right", "end":
				kind = doc.TabRight
			case "center":
				kind = doc.TabCenter
			case "decimal":
				kind = doc.TabDecimal
			}
			leader := "NONE"
			switch t.leader {
			case "dot", "middleDot":
				leader = "DOT"
			case "hyphen":
				leader = "DASH"
			case "underscore", "heavy":
				leader = "SOLID"
			}
			f.Tabs = append(f.Tabs, doc.TabStop{Position: doc.FromTwips(float64(t.position)), Kind: kind, Leader: leader})
		}
	}
	return f
}

func (r *docxReader) charFormat(p *runProps) doc.CharFormat {
	latin, eastAsian := r.fonts(p)
	size := val(p.size, 20)
	f := doc.CharFormat{
		Bold:        ptr(val(p.bold, false)),
		Italic:      ptr(val(p.italic, false)),
		Underline:   ptr(val(p.underline, false)),
		Strike:      ptr(val(p.strike, false)),
		Superscript: ptr(p.vertAlign == "superscript"),
		Subscript:   ptr(p.vertAlign == "subscript"),
		Size:        ptr(size * 50),
		Color:       ptr(or(p.color, "#000000")),
		Spacing:     ptr(0),
	}
	if latin != "" {
		f.Font = ptr(latin)
	}
	if eastAsian != "" {
		f.EastAsianFont = ptr(eastAsian)
	}
	if shade := or(p.highlight, p.shade); shade != "" {
		f.Shade = ptr(shade)
	}
	// Word letter spacing is absolute (twips); 한글's is a percentage of the font size.
	if p.spacing != nil && *p.spacing != 0 && size != 0 {
		f.Spacing = ptr(clamp(iround(float64(*p.spacing)*10.0/float64(size)), -50, 50))
	}
	return f
}

func (r *docxReader) fonts(p *runProps) (string, string) {
	theme := func(name string) string {
		switch {
		case name == "":
			return ""
		case strings.HasPrefix(name, "major"):
			if strings.HasSuffix(name, "EastAsia") {
				return r.majorFont[1]
			}
			return r.majorFont[0]
		case strings.HasSuffix(name, "EastAsia"):
			return r.minorFont[1]
		}
		return r.minorFont[0]
	}
	latin := or(theme(p.asciiTheme), p.ascii)
	eastAsian := or(theme(p.eastAsiaTheme), p.eastAsia)
	return latin, or(eastAsian, latin)
}

// numbering is the list definition for a Word numbering instance (one per numId, shared by all its paragraphs).
func (r *docxReader) numbering(numID string) *doc.Numbering {
	levels, ok := r.numberingLevels[numID]
	if numID == "" || numID == "0" || !ok {
		return nil
	}
	if n, ok := r.numberings[numID]; ok {
		return n
	}
	n := &doc.Numbering{}
	for _, level := range levels {
		if level.format == "bullet" {
			n.Levels = append(n.Levels, doc.NumberingLevel{Format: "DIGIT", Text: bulletText(level.text, levelFont(level.rPr)), Start: level.start, Bullet: true})
		} else {
			n.Levels = append(n.Levels, doc.NumberingLevel{Format: numberFormatOf(level.format), Text: numberText(level.text, level.format), Start: level.start})
		}
	}
	r.numberings[numID] = n
	return n
}

func numberFormatOf(word string) string {
	switch word {
	case "upperRoman":
		return "ROMAN_CAPITAL"
	case "lowerRoman":
		return "ROMAN_SMALL"
	case "upperLetter":
		return "LATIN_CAPITAL"
	case "lowerLetter":
		return "LATIN_SMALL"
	case "ganada":
		return "HANGUL_SYLLABLE"
	case "chosung":
		return "HANGUL_JAMO"
	case "decimalEnclosedCircle", "decimalEnclosedCircleChinese":
		return "CIRCLED_DIGIT"
	case "koreanDigital", "koreanCounting", "koreanLegal", "koreanDigital2":
		return "HANGUL_PHONETIC"
	case "ideographDigital", "chineseCounting", "chineseCountingThousand", "japaneseCounting", "ideographTraditional":
		return "IDEOGRAPH"
	}
	return "DIGIT"
}

// numberText turns "%1.%2." into "^1.^2."; a "none" level keeps only its literal text.
func numberText(text, format string) string {
	if format == "none" {
		var sb strings.Builder
		for _, c := range text {
			if c != '%' && !unicode.IsDigit(c) {
				sb.WriteRune(c)
			}
		}
		return sb.String()
	}
	b := []rune(text)
	for i := 0; i+1 < len(b); i++ {
		if b[i] == '%' && unicode.IsDigit(b[i+1]) {
			b[i] = '^'
		}
	}
	return string(b)
}

// levelFont is the font of a list level's marker (w:lvl/w:rPr/w:rFonts), e.g. Wingdings for ▪ and ☐ bullets.
func levelFont(rPr *xmltree.Element) string {
	fonts := rPr.ChildNS(nsW, "rFonts")
	if fonts == nil {
		return ""
	}
	return or(wattr(fonts, "ascii"), wattr(fonts, "hAnsi"))
}

// bulletText: Word bullets often use Symbol/Wingdings private-use code points; map them to Unicode, by the
// bullet's font when it is a symbol font, else by the usual bullets of those fonts.
func bulletText(text, font string) string {
	if m := SymbolText(font, text); m != "" {
		return m
	}
	switch text {
	case "":
		return "●"
	case "\uf0b7", "\uf09f":
		return "•"
	case "o", "\uf06f":
		return "○"
	case "\uf0a7":
		return "▪"
	case "\uf06e":
		return "■"
	case "\uf0d8":
		return "➢"
	case "\uf076":
		return "❖"
	case "\uf0fc":
		return "✓"
	case "\uf071":
		return "❑"
	case "\uf075", "\uf0a8":
		return "◆"
	case "\uf02d":
		return "-"
	}
	for _, c := range text {
		if c >= 0xF000 && c <= 0xFFFF {
			return "●"
		}
		break
	}
	return text
}

// shadingColor is the colour of a w:shd: the fill, the pattern colour for "solid", and for "pctN" the pattern colour
// mixed into the fill at N percent (Word's classic grey header cells are "pct10" or "pct15" with automatic colours).
func shadingColor(shd *xmltree.Element) string {
	rgb := func(hex string) (int, int, int, bool) {
		if len(hex) != 6 {
			return 0, 0, 0, false
		}
		v, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			return 0, 0, 0, false
		}
		return int(v >> 16 & 0xFF), int(v >> 8 & 0xFF), int(v & 0xFF), true
	}
	hexOf := func(r, g, b int) string { return fmt.Sprintf("#%02X%02X%02X", r, g, b) }
	fr, fg, fb, hasFill := rgb(wattr(shd, "fill"))
	cr, cg, cb, hasColor := rgb(wattr(shd, "color"))
	pattern, ok := wattrOK(shd, "val")
	if !ok || pattern == "clear" || pattern == "nil" {
		if hasFill {
			return hexOf(fr, fg, fb)
		}
		return ""
	}
	if pattern == "solid" {
		return hexOf(cr, cg, cb)
	}
	if strings.HasPrefix(pattern, "pct") {
		if percent, err := strconv.Atoi(pattern[3:]); err == nil {
			if !hasFill {
				fr, fg, fb = 255, 255, 255
			}
			if !hasColor {
				cr, cg, cb = 0, 0, 0
			}
			t := float64(clamp(percent, 0, 100)) / 100.0
			mix := func(a, b int) int { return int(math.RoundToEven(float64(a) + float64(b-a)*t)) }
			return hexOf(mix(fr, cr), mix(fg, cg), mix(fb, cb))
		}
	}
	if hasFill {
		return hexOf(fr, fg, fb)
	}
	return ""
}

func highlightColor(name string) string {
	switch name {
	case "yellow":
		return "#FFFF00"
	case "green":
		return "#00FF00"
	case "cyan":
		return "#00FFFF"
	case "magenta":
		return "#FF00FF"
	case "blue":
		return "#0000FF"
	case "red":
		return "#FF0000"
	case "darkBlue":
		return "#000080"
	case "darkCyan":
		return "#008080"
	case "darkGreen":
		return "#008000"
	case "darkMagenta":
		return "#800080"
	case "darkRed":
		return "#800000"
	case "darkYellow":
		return "#808000"
	case "darkGray":
		return "#808080"
	case "lightGray":
		return "#C0C0C0"
	case "black":
		return "#000000"
	case "white":
		return "#FFFFFF"
	}
	return ""
}

// ───────────────────────── XML helpers ─────────────────────────

// wattrOK is a w:-prefixed attribute, or the unprefixed one.
func wattrOK(e *xmltree.Element, name string) (string, bool) {
	if v, ok := e.AttrNS(nsW, name); ok {
		return v, true
	}
	return e.Attr(name)
}

func wattr(e *xmltree.Element, name string) string {
	v, _ := wattrOK(e, name)
	return v
}

func wval(e *xmltree.Element) string { return wattr(e, "val") }

func twips(e *xmltree.Element, name string) *int {
	v, ok := wattrOK(e, name)
	if !ok {
		return nil
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return nil
	}
	return ptr(int(f))
}

func long(e *xmltree.Element, name string) *int64 {
	v, ok := wattrOK(e, name)
	if !ok {
		return nil
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil {
		return nil
	}
	return &n
}

func boolAttr(e *xmltree.Element, name string) *bool {
	v, ok := wattrOK(e, name)
	if !ok {
		return nil
	}
	switch v {
	case "0", "false", "off":
		return ptr(false)
	}
	return ptr(true)
}

// toggle is an on/off property element: present = on unless its val says otherwise; missing = inherit (nil).
func toggle(parent *xmltree.Element, name string) *bool {
	e := parent.ChildNS(nsW, name)
	if e == nil {
		return nil
	}
	switch wval(e) {
	case "0", "false", "off", "none":
		return ptr(false)
	}
	return ptr(true)
}

func toggleOr(parent *xmltree.Element, name string, current *bool) *bool {
	if v := toggle(parent, name); v != nil {
		return v
	}
	return current
}

func nullIfEmpty(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return text
}
