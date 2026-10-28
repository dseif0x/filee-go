package hwpx

// Text of PPTX shapes and table cells. PowerPoint text inherits from a chain of level styles (lvlNpPr): the paragraph
// itself → the shape's list style → the layout placeholder → the master placeholder → the master's title / body /
// other text style → the presentation default. Every property is looked up along that chain.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/office/opc"
	"github.com/dseif0x/filee-go/internal/xdom"
)

const pptxDefaultSize = 1800 // 18 pt, PowerPoint's default text size

type bodyLayout struct {
	padding doc.Insets
	anchor  doc.VerticalAlign
}

// bodyProperties are the insets and vertical anchoring of a text body, inherited from the placeholders.
func bodyProperties(shape, layoutPh, masterPh *xdom.Node) bodyLayout {
	var chain []*xdom.Node
	for _, e := range []*xdom.Node{shape, layoutPh, masterPh} {
		if b := e.Child(nsP, "txBody").Child(nsA, "bodyPr"); b != nil {
			chain = append(chain, b)
		}
	}
	attr := func(name string) (string, bool) {
		for _, b := range chain {
			if v, ok := b.Attr("", name); ok {
				return v, true
			}
		}
		return "", false
	}
	inset := func(name string, fallback int64) int {
		if v, ok := attr(name); ok {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				return hwpOfEmu(n)
			}
		}
		return hwpOfEmu(fallback)
	}
	anchor := doc.VAlignTop
	v, _ := attr("anchor")
	switch v {
	case "ctr":
		anchor = doc.VAlignCenter
	case "b":
		anchor = doc.VAlignBottom
	}
	return bodyLayout{doc.Insets{Left: inset("lIns", 91440), Right: inset("rIns", 91440), Top: inset("tIns", 45720), Bottom: inset("bIns", 45720)}, anchor}
}

type styleChain struct{ specific, general []*xdom.Node }

// textBody is the paragraphs of a text body; empty when there is no visible text. defaultColor is the text colour
// from the shape style (p:style/a:fontRef), if any.
func (r *pptxReader) textBody(ctx *slideContext, partPath string, body, ph, layoutPh, masterPh *xdom.Node, defaultColor string) []doc.Block {
	var blocks []doc.Block
	if body == nil {
		return blocks
	}
	visible := false
	for _, t := range body.Descendants(nsA, "t") {
		if strings.TrimSpace(t.Value()) != "" {
			visible = true
			break
		}
	}
	if !visible {
		return blocks
	}
	// "Shrink text on overflow" stores the scale PowerPoint applied.
	fontScale := 1.0
	if v, err := strconv.Atoi(body.Child(nsA, "bodyPr").Child(nsA, "normAutofit").A("fontScale")); err == nil {
		fontScale = float64(v) / 100000
	}
	counters := make([]int, 10)
	lastNumbered := make([]bool, 10)

	for _, p := range body.All(nsA, "p") {
		pPr := p.Child(nsA, "pPr")
		level := 0
		if l, err := strconv.Atoi(pPr.A("lvl")); err == nil {
			level = l
		}
		level = clamp(level, 0, 8)
		chain := r.styleChain(ctx, body, ph, layoutPh, masterPh, level+1)
		var paragraphChain []*xdom.Node
		if pPr != nil {
			paragraphChain = append(paragraphChain, pPr)
		}
		paragraphChain = append(paragraphChain, chain.specific...)
		paragraphChain = append(paragraphChain, chain.general...)
		attr := func(name string) (string, bool) {
			for _, e := range paragraphChain {
				if v, ok := e.Attr("", name); ok {
					return v, true
				}
			}
			return "", false
		}
		align := doc.AlignLeft
		a, _ := attr("algn")
		switch a {
		case "ctr":
			align = doc.AlignCenter
		case "r":
			align = doc.AlignRight
		case "just":
			align = doc.AlignJustify
		case "dist":
			align = doc.AlignDistribute
		}
		emuAttr := func(name string) int {
			if v, ok := attr(name); ok {
				if n, err := strconv.ParseInt(v, 10, 64); err == nil {
					return hwpOfEmu(n)
				}
			}
			return 0
		}
		paragraph := &doc.Paragraph{Format: doc.ParaFormat{Align: ptr(align), Left: ptr(emuAttr("marL")), FirstLine: ptr(emuAttr("indent")),
			Before: pptxSpacing(paragraphChain, "spcBef"), After: pptxSpacing(paragraphChain, "spcAft"), LineSpacing: pptxLineSpacing(paragraphChain)}}

		var runs []*xdom.Node
		var firstRun *xdom.Node
		for _, e := range p.Elements() {
			switch e.Name.Local {
			case "r", "br", "fld":
				runs = append(runs, e)
				if firstRun == nil && e.Name.Local != "br" {
					firstRun = e
				}
			}
		}
		markSource := p.Child(nsA, "endParaRPr")
		if markSource == nil {
			markSource = firstRun.Child(nsA, "rPr")
		}
		markFormat := r.runFormat(ctx, markSource, pPr, chain, defaultColor, fontScale)
		mark := markFormat
		mark.Color = nil
		paragraph.MarkFormat = mark

		// Bullet or number (not on empty paragraphs, as PowerPoint shows them).
		hasText := false
		for _, run := range runs {
			if run.Child(nsA, "t").Value() != "" {
				hasText = true
				break
			}
		}
		var bullet *bulletInfo
		if hasText {
			bullet = pptxBullet(paragraphChain)
		}
		for deeper := level + 1; deeper < len(counters); deeper++ {
			counters[deeper], lastNumbered[deeper] = 0, false
		}
		if bullet != nil && bullet.autoNumber != "" {
			if lastNumbered[level] {
				counters[level]++
			} else {
				counters[level] = bullet.start
			}
			lastNumbered[level] = true
			paragraph.Inlines = append(paragraph.Inlines, &doc.Text{Text: AutoNumberLabel(bullet.autoNumber, counters[level]) + " ", Format: markFormat})
		} else {
			lastNumbered[level] = false
			if bullet != nil && bullet.char != "" {
				f := markFormat
				f.Bold, f.Italic, f.Underline = nil, nil, nil
				paragraph.Inlines = append(paragraph.Inlines, &doc.Text{Text: bullet.char + " ", Format: f})
			}
		}

		for _, run := range runs {
			format := r.runFormat(ctx, run.Child(nsA, "rPr"), pPr, chain, defaultColor, fontScale)
			if run.Name.Local == "br" {
				paragraph.Inlines = append(paragraph.Inlines, &doc.LineBreak{Format: format})
				continue
			}
			text := run.Child(nsA, "t").Value()
			if text == "" {
				continue
			}
			link := r.externalLink(partPath, run.Child(nsA, "rPr").Child(nsA, "hlinkClick").AN(nsR, "id"))
			// Text in Wingdings & co. as the Unicode characters it shows.
			if link == "" {
				paragraph.Inlines = append(paragraph.Inlines, SymbolRuns(text, format)...)
			} else {
				paragraph.Inlines = append(paragraph.Inlines, &doc.Link{Target: link, Content: SymbolRuns(text, format)})
			}
		}
		blocks = append(blocks, paragraph)
	}
	return blocks
}

// styleChain is the level styles for one paragraph level: specific ones (shape, layout and master placeholder list
// styles) and general ones (master text style, presentation default). The shape style's colour sits between them.
func (r *pptxReader) styleChain(ctx *slideContext, body, ph, layoutPh, masterPh *xdom.Node, level int) styleChain {
	name := fmt.Sprintf("lvl%dpPr", level)
	var chain styleChain
	for _, owner := range []*xdom.Node{body, layoutPh.Child(nsP, "txBody"), masterPh.Child(nsP, "txBody")} {
		if e := owner.Child(nsA, "lstStyle").Child(nsA, name); e != nil {
			chain.specific = append(chain.specific, e)
		}
	}
	textStyles := ctx.master.Child(nsP, "txStyles")
	if ph != nil {
		var masterStyle *xdom.Node
		switch normalizePlaceholder(ph.A("type")) {
		case "title":
			masterStyle = textStyles.Child(nsP, "titleStyle")
		case "body":
			masterStyle = textStyles.Child(nsP, "bodyStyle")
		default:
			masterStyle = textStyles.Child(nsP, "otherStyle")
		}
		if e := masterStyle.Child(nsA, name); e != nil {
			chain.general = append(chain.general, e)
		}
	}
	if e := r.defaultTextStyle.Child(nsA, name); e != nil {
		chain.general = append(chain.general, e)
	}
	return chain
}

func (r *pptxReader) runFormat(ctx *slideContext, rPr, pPr *xdom.Node, chain styleChain, defaultColor string, fontScale float64) doc.CharFormat {
	// Run → paragraph default → specific level styles → [shape style colour] → general level styles.
	var properties []*xdom.Node
	if rPr != nil {
		properties = append(properties, rPr)
	}
	if d := pPr.Child(nsA, "defRPr"); d != nil {
		properties = append(properties, d)
	}
	for _, s := range chain.specific {
		if d := s.Child(nsA, "defRPr"); d != nil {
			properties = append(properties, d)
		}
	}
	specificCount := len(properties)
	for _, s := range chain.general {
		if d := s.Child(nsA, "defRPr"); d != nil {
			properties = append(properties, d)
		}
	}
	attr := func(name string) (string, bool) {
		for _, e := range properties {
			if v, ok := e.Attr("", name); ok {
				return v, true
			}
		}
		return "", false
	}
	color := ""
	for i := 0; i < len(properties) && color == ""; i++ {
		if i == specificCount && defaultColor != "" {
			color = defaultColor
		} else if fill := properties[i].Child(nsA, "solidFill"); fill != nil {
			color = ctx.colors.Drawing(fill, "")
		}
	}
	if color == "" {
		color = defaultColor
	}
	if color == "" {
		color = ctx.colors.Theme("tx1")
	}
	size := pptxDefaultSize
	if v, ok := attr("sz"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			size = n
		}
	}
	typeface := func(element string) string {
		for _, e := range properties {
			if t, ok := e.Child(nsA, element).Attr("", "typeface"); ok {
				return ctx.fonts.resolve(t)
			}
		}
		return ""
	}
	baseline := 0
	if v, ok := attr("baseline"); ok {
		baseline, _ = strconv.Atoi(v)
	}
	f := doc.CharFormat{Size: ptr(max(100, iround(float64(size)*fontScale))), Color: ptr(color)}
	if v, _ := attr("b"); v == "1" || v == "true" {
		f.Bold = ptr(true)
	}
	if v, _ := attr("i"); v == "1" || v == "true" {
		f.Italic = ptr(true)
	}
	if v, ok := attr("u"); ok && v != "none" {
		f.Underline = ptr(true)
	}
	if v, ok := attr("strike"); ok && v != "noStrike" {
		f.Strike = ptr(true)
	}
	if baseline > 0 {
		f.Superscript = ptr(true)
	}
	if baseline < 0 {
		f.Subscript = ptr(true)
	}
	if latin := typeface("latin"); latin != "" {
		f.Font = ptr(latin)
	}
	if ea := typeface("ea"); ea != "" {
		f.EastAsianFont = ptr(ea)
	}
	return f
}

// resolve turns theme font references (+mj-lt, +mn-ea, ...) into the theme's fonts.
func (t themeFonts) resolve(typeface string) string {
	switch typeface {
	case "+mj-lt":
		return t.majorLatin
	case "+mn-lt":
		return t.minorLatin
	case "+mj-ea":
		return t.majorEastAsian
	case "+mn-ea":
		return t.minorEastAsian
	}
	if strings.HasPrefix(typeface, "+") {
		return ""
	}
	return typeface
}

type bulletInfo struct {
	char, autoNumber string
	start            int
}

func pptxBullet(chain []*xdom.Node) *bulletInfo {
	for _, style := range chain {
		if style.Child(nsA, "buNone") != nil {
			return nil
		}
		if character := style.Child(nsA, "buChar"); character != nil {
			// "§" in Wingdings is ▪, "ü" ✓: a bullet in a symbol font as the Unicode character it shows.
			text, ok := character.Attr("", "char")
			if !ok {
				text = "•"
			}
			font := ""
			for _, s := range chain {
				f := s.Child(nsA, "buFont")
				if f == nil {
					f = s.Child(nsA, "buFontTx")
				}
				if f != nil {
					font = f.A("typeface")
					break
				}
			}
			if m := SymbolText(font, text); m != "" {
				text = m
			}
			return &bulletInfo{char: text, start: 1}
		}
		if number := style.Child(nsA, "buAutoNum"); number != nil {
			scheme := number.A("type")
			if scheme == "" {
				scheme = "arabicPeriod"
			}
			start := 1
			if v, err := strconv.Atoi(number.A("startAt")); err == nil {
				start = v
			}
			return &bulletInfo{autoNumber: scheme, start: start}
		}
		if style.Child(nsA, "buBlip") != nil {
			return &bulletInfo{char: "•", start: 1}
		}
	}
	return nil
}

// AutoNumberLabel is the label of item n in a PowerPoint auto-number scheme (arabicPeriod, alphaLcParenR, ...).
func AutoNumberLabel(scheme string, n int) string {
	var value string
	switch {
	case strings.HasPrefix(scheme, "alphaLc"):
		value = strings.ToLower(letters(n))
	case strings.HasPrefix(scheme, "alphaUc"):
		value = letters(n)
	case strings.HasPrefix(scheme, "romanLc"):
		value = strings.ToLower(roman(n))
	case strings.HasPrefix(scheme, "romanUc"):
		value = roman(n)
	case strings.HasPrefix(scheme, "circleNum") && n >= 1 && n <= 20:
		value = string(rune('①' + n - 1))
	default:
		value = strconv.Itoa(n)
	}
	switch {
	case strings.HasSuffix(scheme, "ParenBoth"):
		return "(" + value + ")"
	case strings.HasSuffix(scheme, "ParenR"):
		return value + ")"
	case strings.HasSuffix(scheme, "Plain"):
		return value
	}
	return value + "."
}

func letters(n int) string {
	text := ""
	for ; n > 0; n = (n - 1) / 26 {
		text = string(rune('A'+(n-1)%26)) + text
	}
	return text
}

func roman(n int) string {
	text := ""
	for _, p := range []struct {
		v int
		s string
	}{{1000, "M"}, {900, "CM"}, {500, "D"}, {400, "CD"}, {100, "C"}, {90, "XC"}, {50, "L"}, {40, "XL"}, {10, "X"}, {9, "IX"}, {5, "V"}, {4, "IV"}, {1, "I"}} {
		for ; n >= p.v; n -= p.v {
			text += p.s
		}
	}
	return text
}

// pptxSpacing is the space before / after in HWPUNIT (1/100 pt = 1 HWPUNIT); percentages are of an 18 pt line.
func pptxSpacing(chain []*xdom.Node, name string) *int {
	for _, style := range chain {
		spacing := style.Child(nsA, name)
		if spacing == nil {
			continue
		}
		if v, err := strconv.Atoi(spacing.Child(nsA, "spcPts").A("val")); err == nil {
			return ptr(v)
		}
		if v, err := strconv.Atoi(spacing.Child(nsA, "spcPct").A("val")); err == nil {
			return ptr(int(float64(v) / 100000.0 * pptxDefaultSize * 1.2))
		}
	}
	return ptr(0)
}

// pptxLineSpacing: PowerPoint's "single" line is about 1.2 × the font size, 한글's percentages count the font size.
func pptxLineSpacing(chain []*xdom.Node) *doc.LineSpacing {
	for _, style := range chain {
		spacing := style.Child(nsA, "lnSpc")
		if spacing == nil {
			continue
		}
		if v, err := strconv.Atoi(spacing.Child(nsA, "spcPct").A("val")); err == nil {
			return &doc.LineSpacing{Kind: doc.SpacingPercent, Value: max(60, iround(float64(v)/1000.0*1.2))}
		}
		if v, err := strconv.Atoi(spacing.Child(nsA, "spcPts").A("val")); err == nil {
			return &doc.LineSpacing{Kind: doc.SpacingFixed, Value: v}
		}
	}
	return &doc.LineSpacing{Kind: doc.SpacingPercent, Value: 120}
}

func (r *pptxReader) externalLink(partPath, id string) string {
	if id == "" {
		return ""
	}
	if rel, ok := r.pkg.Rel(partPath, id); ok && rel.External {
		return rel.Target
	}
	return ""
}

// ───────────────────────── Tables ─────────────────────────

func (r *pptxReader) table(ctx *slideContext, partPath string, frame *xdom.Node, t transform, output []doc.Inline) []doc.Inline {
	tbl := frame.Child(nsA, "graphic").Child(nsA, "graphicData").Child(nsA, "tbl")
	b, ok := boundsOf(frame.Child(nsP, "xfrm"), t)
	if tbl == nil || !ok {
		return output // charts, SmartArt and embedded objects are not converted
	}
	var grid []int
	for _, c := range tbl.Child(nsA, "tblGrid").All(nsA, "gridCol") {
		grid = append(grid, max(1, hwpOfEmu(emu(c, "w"))))
	}
	if len(grid) == 0 {
		return output
	}
	tablePr := tbl.Child(nsA, "tblPr")
	styled := tablePr.Child(nsA, "tableStyleId") != nil
	firstRow := tablePr.A("firstRow") == "1" || tablePr.A("firstRow") == "true"
	banded := tablePr.A("bandRow") == "1" || tablePr.A("bandRow") == "true"
	// Without the style part, table styles are approximated by PowerPoint's default "Medium Style 2 – Accent 1".
	accent := ctx.colors.Theme("accent1")
	gridLine := doc.ThinBorder
	if styled {
		gridLine = doc.Border{Style: doc.BorderSolid, WidthMm: 0.35, Color: "#FFFFFF"}
	}
	table := &doc.Table{ColumnCount: len(grid), ColumnWidths: grid, CellMargin: &doc.Insets{Left: 720, Right: 720, Top: 360, Bottom: 360},
		Borders: &doc.Borders{Left: gridLine, Right: gridLine, Top: gridLine, Bottom: gridLine}}
	rows := tbl.All(nsA, "tr")
	totalHeight := 0
	for ri, tr := range rows {
		header := firstRow && ri == 0
		row := &doc.Row{Header: header, Height: ptr(max(1, hwpOfEmu(emu(tr, "h"))))}
		totalHeight += *row.Height
		column := 0
		for _, tc := range tr.All(nsA, "tc") {
			colSpan := 1
			if v, err := strconv.Atoi(tc.A("gridSpan")); err == nil {
				colSpan = max(1, v)
			}
			if h, v := tc.A("hMerge"), tc.A("vMerge"); h == "1" || h == "true" || v == "1" || v == "true" {
				column += colSpan
				continue // covered by a merged cell
			}
			tcPr := tc.Child(nsA, "tcPr")
			fill := ""
			if solid := tcPr.Child(nsA, "solidFill"); solid != nil {
				fill = ctx.colors.Drawing(solid, "")
			}
			if fill == "" && styled && tcPr.Child(nsA, "noFill") == nil {
				switch {
				case header:
					fill = accent
				case banded && (ri-b2i(firstRow))%2 == 0:
					fill = opc.Tint(accent, 0.6)
				default:
					fill = opc.Tint(accent, 0.8)
				}
			}
			cell := &doc.Cell{Column: column, ColSpan: colSpan, RowSpan: 1, Fill: fill,
				Borders: &doc.Borders{Left: cellLine(tcPr.Child(nsA, "lnL"), ctx.colors, gridLine), Right: cellLine(tcPr.Child(nsA, "lnR"), ctx.colors, gridLine),
					Top: cellLine(tcPr.Child(nsA, "lnT"), ctx.colors, gridLine), Bottom: cellLine(tcPr.Child(nsA, "lnB"), ctx.colors, gridLine)}}
			if v, err := strconv.Atoi(tc.A("rowSpan")); err == nil {
				cell.RowSpan = max(1, v)
			}
			switch tcPr.A("anchor") {
			case "ctr":
				cell.VerticalAlign = doc.VAlignCenter
			case "b":
				cell.VerticalAlign = doc.VAlignBottom
			}
			headerColor := ""
			if styled && header {
				headerColor = ctx.colors.Theme("lt1")
			}
			text := r.textBody(ctx, partPath, tc.Child(nsA, "txBody"), nil, nil, nil, headerColor)
			if styled && header {
				emphasize(text)
			}
			if len(text) == 0 {
				text = []doc.Block{&doc.Paragraph{}}
			}
			cell.Blocks = text
			row.Cells = append(row.Cells, cell)
			column += colSpan
		}
		table.Rows = append(table.Rows, row)
	}
	box := &doc.TextBox{Width: max(1, hwpOfEmu(b.w)), Height: max(1, totalHeight), Anchor: pageAnchor(hwpOfEmu(b.x), hwpOfEmu(b.y)),
		Line: doc.NoBorder, Blocks: []doc.Block{table}}
	return append(output, box)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func cellLine(line *xdom.Node, colors *opc.Colors, fallback doc.Border) doc.Border {
	if line == nil {
		return fallback
	}
	if line.Child(nsA, "noFill") != nil {
		return doc.NoBorder
	}
	color := ""
	if solid := line.Child(nsA, "solidFill"); solid != nil {
		color = colors.Drawing(solid, "")
	}
	if color == "" {
		return fallback
	}
	w := float64(emu(line, "w")) / 36000.0
	if w < 0.1 {
		w = 0.1
	}
	return doc.Border{Style: doc.BorderSolid, WidthMm: w, Color: color}
}

// emphasize makes the header cells of styled tables bold.
func emphasize(blocks []doc.Block) {
	for _, b := range blocks {
		if p, ok := b.(*doc.Paragraph); ok {
			for i, in := range p.Inlines {
				if t, ok := in.(*doc.Text); ok && t.Format.Bold == nil {
					f := t.Format
					f.Bold = ptr(true)
					p.Inlines[i] = &doc.Text{Text: t.Text, Format: f}
				}
			}
		}
	}
}
