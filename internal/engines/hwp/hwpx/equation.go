package hwpx

import (
	"math"
	"strings"
	"unicode/utf8"
)

// Equations for 한글 (hp:equation): a small math tree that readers build (DOCX math, see the OMML reader), written as
// 한글's equation script (Equation Version 60, e.g. "{a} over {b}", "sqrt {x}", "SUM _{i=1} ^{n}"), as linear text
// ("(a)/(b)", "√(x)") for formats without equations, and measured for the equation's box.
//
// The box: 한글 stores the size an equation was laid out at (hp:sz), and rhwp stretches the drawing to that width when
// it renders the document (to PDF), so a box that is too wide or too narrow distorts the equation. Measure follows
// rhwp's own equation layout (rhwp 0.8.7, src/renderer/equation/layout.rs, MIT): its estimated character widths,
// script scale 0.7, fraction, root, bracket and matrix spacing.
//
// The script is written so that 한글 and rhwp read the same tree from it: every token is separated by a space,
// variables are written one letter at a time (so "in", "ln" or "max" in a subscript never become commands), groups
// are braced, and only commands that both know are used.

// MathNode is one node of an equation tree.
type MathNode interface{ mathNode() }

// MathRow is items side by side.
type MathRow struct{ Items []MathNode }

// MathText is one character of a variable or text: italic Latin letters, upright CJK.
type MathText struct{ Text string }

type MathNumber struct{ Digits string }

// MathSymbol is an ASCII operator or bracket written as it is: + - = < > ( ) [ ] , . / | ! * : '
type MathSymbol struct{ Text string }

// MathCommand is a symbol written as a command: Greek letters, TIMES, LEQ, INF, rarrow, LBRACE ...
type MathCommand struct{ Command, Glyph string }

// MathQuoted is a text written in quotes (characters with a meaning in the script: # & _ ^).
type MathQuoted struct{ Text string }

// MathFunction is a function name 한글 writes upright by itself: sin, log, max ...
type MathFunction struct{ Name string }

// MathFraction is a fraction, or one part above the other without a bar (ATOP).
type MathFraction struct {
	Numerator, Denominator MathNode
	Bar                    bool
}

// MathRoot is a square root, or a root with an index.
type MathRoot struct{ Index, Body MathNode }

// MathScripts is a subscript and / or superscript.
type MathScripts struct{ Base, Sub, Sup MathNode }

// MathBigOperator is a large operator with limits: SUM, PROD ... above and below, INT, OINT ... at the side.
type MathBigOperator struct {
	Command, Glyph string
	Lower, Upper   MathNode
}

func (b *MathBigOperator) integral() bool {
	switch b.Glyph {
	case "∫", "∬", "∭", "∮", "∯", "∰":
		return true
	}
	return false
}

// MathLimit is lim with what goes under it.
type MathLimit struct{ Lower MathNode }

// MathMatrix: Command is matrix, pmatrix (round brackets), bmatrix (square) or dmatrix (bars).
type MathMatrix struct {
	Command string
	Rows    [][]MathNode
}

// MathCases are rows of cells after one large left brace.
type MathCases struct{ Rows [][]MathNode }

// MathPile is lines stacked and centred (PILE).
type MathPile struct{ Rows []MathNode }

// MathBrackets grow with their content (LEFT ... RIGHT); an empty side has no bracket.
type MathBrackets struct {
	Left, Right string
	Body        MathNode
}

// MathAccent is a mark over (or a line under) the content: hat, bar, vec, tilde, OVERLINE, UNDERLINE ...
type MathAccent struct {
	Command string
	Body    MathNode
}

// MathStyled is upright (rm) or bold content.
type MathStyled struct {
	Command string
	Body    MathNode
}

// MathSpace is a normal space (~).
type MathSpace struct{}

// MathAlign is an alignment mark (&) of a Word equation array: the cell boundary of cases, nothing elsewhere.
type MathAlign struct{}

// mathTab is the tab rhwp makes of & in cases: one font size wide.
type mathTab struct{}

func (*MathRow) mathNode()         {}
func (*MathText) mathNode()        {}
func (*MathNumber) mathNode()      {}
func (*MathSymbol) mathNode()      {}
func (*MathCommand) mathNode()     {}
func (*MathQuoted) mathNode()      {}
func (*MathFunction) mathNode()    {}
func (*MathFraction) mathNode()    {}
func (*MathRoot) mathNode()        {}
func (*MathScripts) mathNode()     {}
func (*MathBigOperator) mathNode() {}
func (*MathLimit) mathNode()       {}
func (*MathMatrix) mathNode()      {}
func (*MathCases) mathNode()       {}
func (*MathPile) mathNode()        {}
func (*MathBrackets) mathNode()    {}
func (*MathAccent) mathNode()      {}
func (*MathStyled) mathNode()      {}
func (*MathSpace) mathNode()       {}
func (*MathAlign) mathNode()       {}
func (*mathTab) mathNode()         {}

// MathBox is the width, height and baseline (from the top) of laid out math.
type MathBox struct{ Width, Height, Baseline float64 }

// rhwp's layout constants (fractions of the font size).
const (
	scriptScale       = 0.7
	fractionPad       = 0.2
	fractionLine      = 0.04
	rootPad           = 0.1
	bracketPad        = 0.08
	bigOperatorScale  = 1.5
	integralScale     = 2.5
	bigOperatorTrail  = 0.45
	matrixColumnGap   = 0.8
	matrixRowGap      = 0.3
	mathAxis          = 0.25
)

// ───────────────────────── Script ─────────────────────────

// Script is the 한글 equation script of node.
func Script(node MathNode) string {
	var sb strings.Builder
	writeScript(node, &sb)
	return collapse(sb.String())
}

// collapse leaves one space between tokens and none inside the braces of a group ("x _{i}"), quoted text alone.
func collapse(script string) string {
	sb := make([]byte, 0, len(script))
	quoted := false
	for i := 0; i < len(script); i++ {
		ch := script[i]
		if ch == '"' {
			quoted = !quoted
		}
		if !quoted && ch == ' ' && (len(sb) == 0 || sb[len(sb)-1] == ' ' || sb[len(sb)-1] == '{') {
			continue
		}
		if !quoted && ch == '}' && len(sb) > 0 && sb[len(sb)-1] == ' ' {
			sb = sb[:len(sb)-1]
		}
		sb = append(sb, ch)
	}
	return strings.TrimSpace(string(sb))
}

func writeScript(node MathNode, sb *strings.Builder) {
	switch n := node.(type) {
	case *MathRow:
		for _, item := range n.Items {
			sb.WriteByte(' ')
			if inner, ok := item.(*MathRow); ok {
				group(inner, sb)
			} else {
				writeScript(item, sb)
			}
		}
	case *MathText:
		sb.WriteString(" " + n.Text + " ")
	case *MathNumber:
		sb.WriteString(" " + n.Digits + " ")
	case *MathSymbol:
		sb.WriteString(" " + n.Text + " ")
	case *MathCommand:
		sb.WriteString(" " + n.Command + " ")
	case *MathQuoted:
		sb.WriteString(" \"" + strings.ReplaceAll(n.Text, "\"", "'") + "\" ")
	case *MathFunction:
		sb.WriteString(" " + n.Name + " ")
	case *MathFraction:
		sb.WriteString(" {")
		group(n.Numerator, sb)
		if n.Bar {
			sb.WriteString(" over ")
		} else {
			sb.WriteString(" atop ")
		}
		group(n.Denominator, sb)
		sb.WriteString("} ")
	case *MathRoot:
		if n.Index == nil {
			sb.WriteString(" sqrt ")
		} else {
			sb.WriteString(" root ")
			group(n.Index, sb)
			sb.WriteString(" of ")
		}
		group(n.Body, sb)
	case *MathScripts:
		if isAtom(n.Base) {
			writeScript(n.Base, sb)
		} else {
			group(n.Base, sb)
		}
		if n.Sub != nil {
			sb.WriteString(" _")
			group(n.Sub, sb)
		}
		if n.Sup != nil {
			sb.WriteString(" ^")
			group(n.Sup, sb)
		}
		sb.WriteByte(' ')
	case *MathBigOperator:
		sb.WriteString(" " + n.Command)
		if n.Lower != nil {
			sb.WriteString(" _")
			group(n.Lower, sb)
		}
		if n.Upper != nil {
			sb.WriteString(" ^")
			group(n.Upper, sb)
		}
		sb.WriteByte(' ')
	case *MathLimit:
		sb.WriteString(" lim ")
		if n.Lower != nil {
			sb.WriteByte('_')
			group(n.Lower, sb)
		}
		sb.WriteByte(' ')
	case *MathMatrix:
		sb.WriteString(" " + n.Command + " {")
		cells(n.Rows, sb)
		sb.WriteString("} ")
	case *MathCases:
		sb.WriteString(" cases {")
		cells(n.Rows, sb)
		sb.WriteString("} ")
	case *MathPile:
		sb.WriteString(" pile {")
		for i, row := range n.Rows {
			if i > 0 {
				sb.WriteString(" # ")
			}
			writeScript(row, sb)
		}
		sb.WriteString("} ")
	case *MathBrackets:
		sb.WriteString(" LEFT " + bracketToken(n.Left) + " ")
		writeScript(n.Body, sb)
		sb.WriteString(" RIGHT " + bracketToken(n.Right) + " ")
	case *MathAccent:
		sb.WriteString(" " + n.Command + " ")
		group(n.Body, sb)
	case *MathStyled:
		// Braced so the style ends with the content (rm and bold switch the rest of a group in 한글).
		sb.WriteString(" {" + n.Command + " ")
		group(n.Body, sb)
		sb.WriteString("} ")
	case *MathSpace:
		sb.WriteString(" ~ ")
	}
}

func group(node MathNode, sb *strings.Builder) {
	sb.WriteByte('{')
	writeScript(node, sb)
	sb.WriteByte('}')
}

func cells(rows [][]MathNode, sb *strings.Builder) {
	for r, row := range rows {
		if r > 0 {
			sb.WriteString(" # ")
		}
		for c, cell := range row {
			if c > 0 {
				sb.WriteString(" & ")
			}
			writeScript(cell, sb)
		}
	}
}

// isAtom: a base that takes scripts without braces: one letter, number, symbol command or function name.
func isAtom(node MathNode) bool {
	switch node.(type) {
	case *MathText, *MathNumber, *MathCommand, *MathFunction:
		return true
	}
	return false
}

func bracketToken(bracket string) string {
	switch bracket {
	case "":
		return "."
	case "{":
		return "LBRACE"
	case "}":
		return "RBRACE"
	case "⟨", "〈":
		return "LANGLE"
	case "⟩", "〉":
		return "RANGLE"
	case "⌈":
		return "LCEIL"
	case "⌉":
		return "RCEIL"
	case "⌊":
		return "LFLOOR"
	case "⌋":
		return "RFLOOR"
	case "‖":
		return "VERT"
	}
	return bracket
}

// IsBracket tells the brackets MathBrackets can hold; others are written as text inside.
func IsBracket(bracket string) bool {
	switch bracket {
	case "", "(", ")", "[", "]", "{", "}", "|", "⟨", "⟩", "〈", "〉", "⌈", "⌉", "⌊", "⌋", "‖":
		return true
	}
	return false
}

// ───────────────────────── Linear text ─────────────────────────

// Linear is the equation as one line of text, for formats without equations: "(a+b)/(2c)", "x_i^2", "√(x)".
func Linear(node MathNode) string {
	var sb strings.Builder
	linear(node, &sb)
	return sb.String()
}

func linear(node MathNode, sb *strings.Builder) {
	switch n := node.(type) {
	case *MathRow:
		for _, item := range n.Items {
			linear(item, sb)
		}
	case *MathText:
		sb.WriteString(n.Text)
	case *MathNumber:
		sb.WriteString(n.Digits)
	case *MathSymbol:
		sb.WriteString(n.Text)
	case *MathCommand:
		sb.WriteString(n.Glyph)
	case *MathQuoted:
		sb.WriteString(n.Text)
	case *MathFunction:
		sb.WriteString(n.Name + " ")
	case *MathFraction:
		linearOperand(n.Numerator, sb)
		if n.Bar {
			sb.WriteString("/")
		} else {
			sb.WriteString(" ")
		}
		linearOperand(n.Denominator, sb)
	case *MathRoot:
		if n.Index != nil {
			sb.WriteByte('[')
			linear(n.Index, sb)
			sb.WriteByte(']')
		}
		sb.WriteString("√")
		linearOperand(n.Body, sb)
	case *MathScripts:
		linear(n.Base, sb)
		linearScripts(n.Sub, n.Sup, sb)
	case *MathBigOperator:
		sb.WriteString(n.Glyph)
		linearScripts(n.Lower, n.Upper, sb)
		sb.WriteByte(' ')
	case *MathLimit:
		sb.WriteString("lim")
		linearScripts(n.Lower, nil, sb)
		sb.WriteByte(' ')
	case *MathMatrix:
		open, close := "[", "]"
		switch n.Command {
		case "pmatrix":
			open, close = "(", ")"
		case "dmatrix":
			open, close = "|", "|"
		}
		sb.WriteString(open)
		linearCells(n.Rows, ", ", sb)
		sb.WriteString(close)
	case *MathCases:
		sb.WriteByte('{')
		linearCells(n.Rows, " ", sb)
	case *MathPile:
		for i, row := range n.Rows {
			if i > 0 {
				sb.WriteString("; ")
			}
			linear(row, sb)
		}
	case *MathBrackets:
		sb.WriteString(n.Left)
		linear(n.Body, sb)
		sb.WriteString(n.Right)
	case *MathAccent:
		mark := "̲" // UNDERLINE, under
		switch n.Command {
		case "hat":
			mark = "̂"
		case "check":
			mark = "̌"
		case "tilde":
			mark = "̃"
		case "acute":
			mark = "́"
		case "grave":
			mark = "̀"
		case "dot":
			mark = "̇"
		case "ddot":
			mark = "̈"
		case "bar", "OVERLINE":
			mark = "̅"
		case "vec":
			mark = "⃗"
		case "dyad":
			mark = "⃡"
		}
		body := Linear(n.Body)
		if utf8.RuneCountInString(body) == 1 {
			sb.WriteString(body + mark)
		} else {
			sb.WriteString(strings.ToLower(n.Command) + "(" + body + ")")
		}
	case *MathStyled:
		linear(n.Body, sb)
	case *MathSpace:
		sb.WriteByte(' ')
	}
}

func linearScripts(sub, sup MathNode, sb *strings.Builder) {
	if sub != nil {
		sb.WriteByte('_')
		linearOperand(sub, sb)
	}
	if sup != nil {
		sb.WriteByte('^')
		linearOperand(sup, sb)
	}
}

// linearOperand is an operand of /, _ or ^: in brackets unless it is one letter, symbol, number or bracketed group.
func linearOperand(node MathNode, sb *strings.Builder) {
	text := Linear(node)
	single := node
	if row, ok := node.(*MathRow); ok && len(row.Items) == 1 {
		single = row.Items[0]
	}
	simple := false
	switch s := single.(type) {
	case *MathNumber, *MathCommand, *MathBrackets:
		simple = true
	case *MathText:
		simple = utf8.RuneCountInString(s.Text) == 1
	}
	if simple || utf8.RuneCountInString(text) <= 1 {
		sb.WriteString(text)
	} else {
		sb.WriteString("(" + text + ")")
	}
}

func linearCells(rows [][]MathNode, separator string, sb *strings.Builder) {
	for r, row := range rows {
		if r > 0 {
			sb.WriteString("; ")
		}
		for c, cell := range row {
			if c > 0 {
				sb.WriteString(separator)
			}
			linear(cell, sb)
		}
	}
}

// ───────────────────────── Size ─────────────────────────

// MeasureMath is the box of node at font size (any unit; the box is in the same unit).
func MeasureMath(node MathNode, size float64) MathBox {
	switch n := node.(type) {
	case *MathRow:
		return measureRow(n.Items, size)
	case *MathText:
		return character(MathTextWidth(n.Text, size, !isCjk(n.Text)), size)
	case *MathNumber:
		return character(MathTextWidth(n.Digits, size, false), size)
	case *MathSymbol:
		pad := 0.05
		switch n.Text {
		case "+", "-", "=", "<", ">":
			pad = 0.15
		}
		return character(MathTextWidth(n.Text, size, false)+2*size*pad, size)
	case *MathCommand:
		return character(MathTextWidth(n.Glyph, size, false), size)
	case *MathQuoted:
		return character(MathTextWidth(n.Text, size, false), size)
	case *MathFunction:
		return character(MathTextWidth(n.Name, size, false)+size*0.02, size)
	case *MathFraction:
		return measureFraction(n, size)
	case *MathRoot:
		return measureRoot(n, size)
	case *MathScripts:
		return measureScripts(MeasureMath(n.Base, size), n.Sub, n.Sup, size)
	case *MathBigOperator:
		if n.integral() {
			return measureIntegral(n, size)
		}
		return measureBigOperator(n, size)
	case *MathLimit:
		return measureLimit(n, size)
	case *MathMatrix:
		return measureMatrix(n, size)
	case *MathCases:
		return measureCases(n, size)
	case *MathPile:
		return measurePile(n, size)
	case *MathBrackets:
		return measureBrackets(n, size)
	case *MathAccent:
		return raise(MeasureMath(n.Body, size), size*0.25)
	case *MathStyled:
		return MeasureMath(n.Body, size)
	case *MathSpace:
		return character(size*0.33, size)
	case *mathTab:
		return character(size, size)
	}
	return MathBox{}
}

func character(width, size float64) MathBox { return MathBox{width, size, size * 0.8} }

// raise leaves room above the content for an accent mark.
func raise(body MathBox, room float64) MathBox {
	return MathBox{body.Width, body.Height + room, body.Baseline + room}
}

// measureRow is a group as rhwp parses it: an empty group is nothing, a group of one item is that item.
func measureRow(items []MathNode, size float64) MathBox {
	var parts []MathNode
	for _, i := range items {
		if _, ok := i.(*MathAlign); !ok {
			parts = append(parts, i)
		}
	}
	if len(parts) == 0 {
		return MathBox{}
	}
	if len(parts) == 1 {
		return MeasureMath(parts[0], size)
	}
	var boxes []MathBox
	for _, p := range parts {
		if b := MeasureMath(p, size); b.Width > 0 {
			boxes = append(boxes, b)
		}
	}
	if len(boxes) == 0 {
		return MathBox{0, size, size * 0.8}
	}
	ascent, descent, width := math.Inf(-1), math.Inf(-1), 0.0
	for _, b := range boxes {
		ascent = math.Max(ascent, b.Baseline)
		descent = math.Max(descent, b.Height-b.Baseline)
		width += b.Width
	}
	return MathBox{width, ascent + descent, ascent}
}

func measureFraction(f *MathFraction, size float64) MathBox {
	n := MeasureMath(f.Numerator, size)
	d := MeasureMath(f.Denominator, size)
	pad := size * fractionPad
	width := math.Max(n.Width, d.Width) + pad*2
	top := n.Height + pad
	bottom := d.Height + pad
	if !f.Bar {
		return MathBox{width, top + bottom, top + size*mathAxis}
	}
	line := size * fractionLine
	return MathBox{width, top + line + bottom, top + line/2 + size*mathAxis}
}

func measureRoot(r *MathRoot, size float64) MathBox {
	body := MeasureMath(r.Body, size)
	pad := size * rootPad
	sign := size * 0.6
	index := 0.0
	if r.Index != nil {
		index = MeasureMath(r.Index, size*scriptScale).Width
	}
	bodyWidth := body.Width + pad
	return MathBox{math.Max(index, sign*0.5) + sign*0.5 + bodyWidth, body.Height + pad*2, pad + body.Baseline}
}

func measureScripts(b MathBox, subNode, supNode MathNode, size float64) MathBox {
	if subNode == nil && supNode == nil {
		return b
	}
	if subNode == nil {
		s := MeasureMath(supNode, size*scriptScale)
		shift := b.Baseline - s.Height*0.7
		baseY := -shift
		if shift >= 0 {
			baseY = math.Max(s.Height-b.Height, 0)
		}
		return MathBox{b.Width + s.Width, math.Max(baseY+b.Height, s.Height), baseY + b.Baseline}
	}
	if supNode == nil {
		s := MeasureMath(subNode, size*scriptScale)
		shift := b.Baseline * 0.4
		return MathBox{b.Width + s.Width, math.Max(b.Height, shift+s.Height), b.Baseline}
	}
	sub := MeasureMath(subNode, size*scriptScale)
	sup := MeasureMath(supNode, size*scriptScale)
	supShift := b.Baseline - sup.Height*0.7
	subShift := b.Baseline * 0.4
	ascent := math.Max(sup.Height, 0)
	if supShift < 0 {
		ascent = sup.Height - math.Abs(supShift)
	}
	top := math.Abs(math.Min(supShift, 0))
	height := math.Max(math.Max(top+b.Height, top+subShift+sub.Height), ascent+b.Height)
	y := top
	if supShift <= 0 {
		y = math.Max(top, math.Max(sup.Height-math.Abs(supShift)-b.Baseline, 0))
	} else {
		y = math.Max(top, 0)
	}
	height = math.Max(math.Max(height, y+b.Height), y+subShift+sub.Height)
	return MathBox{b.Width + math.Max(sup.Width, sub.Width), height, y + b.Baseline}
}

func measureBigOperator(big *MathBigOperator, size float64) MathBox {
	opSize := size * bigOperatorScale
	opWidth := MathTextWidth(big.Glyph, opSize, false)
	var upperHeight, lowerHeight, lowerWidth, upperWidth float64
	if big.Lower != nil {
		l := MeasureMath(big.Lower, size*scriptScale)
		lowerHeight, lowerWidth = l.Height+size*0.05, l.Width
	}
	if big.Upper != nil {
		u := MeasureMath(big.Upper, size*scriptScale)
		upperHeight, upperWidth = u.Height+size*0.05, u.Width
	}
	width := math.Max(opWidth, math.Max(lowerWidth, upperWidth))
	return MathBox{width + size*bigOperatorTrail, upperHeight + opSize + lowerHeight, upperHeight + opSize*0.6}
}

// measureIntegral: an integral is a large symbol (INT) whose limits are ordinary scripts placed next to the sign.
func measureIntegral(in *MathBigOperator, size float64) MathBox {
	height := size * integralScale
	glyphWidth := size * 0.52
	sign := MathBox{glyphWidth + size*bigOperatorTrail, height, height * 0.7}
	if in.Lower == nil || in.Upper == nil {
		return measureScripts(sign, in.Lower, in.Upper, size)
	}
	sub := MeasureMath(in.Lower, size*scriptScale)
	sup := MeasureMath(in.Upper, size*scriptScale)
	gap := size * 0.22
	topY := height * 0.04
	bottomY := height * 0.96
	head := math.Max(-(topY - sup.Height*0.30), 0)
	supX := size*0.50 + gap
	subX := size*0.04 + gap
	supY := head + topY - sup.Height*0.30
	subY := head + bottomY - sub.Height*0.72
	right := math.Max(math.Max(supX+sup.Width, subX+sub.Width), glyphWidth)
	total := math.Max(math.Max(subY+sub.Height, head+height), supY+sup.Height)
	return MathBox{right + size*bigOperatorTrail, total, head + height*0.7}
}

func measureLimit(l *MathLimit, size float64) MathBox {
	nameWidth := MathTextWidth("lim", size, false)
	if l.Lower == nil {
		return MathBox{nameWidth, size, size * 0.8}
	}
	lower := MeasureMath(l.Lower, size*scriptScale)
	return MathBox{math.Max(nameWidth, lower.Width), size + lower.Height + size*0.05, size * 0.8}
}

func measureMatrix(m *MathMatrix, size float64) MathBox {
	if len(m.Rows) == 0 {
		return MathBox{0, size, size * 0.8}
	}
	columns := 0
	cellsBox := make([][]MathBox, len(m.Rows))
	for r, row := range m.Rows {
		for _, c := range row {
			cellsBox[r] = append(cellsBox[r], MeasureMath(c, size))
		}
		columns = max(columns, len(row))
	}
	widths := make([]float64, columns)
	height := 0.0
	for _, row := range cellsBox {
		rowHeight := size
		for c, b := range row {
			widths[c] = math.Max(widths[c], b.Width)
			rowHeight = math.Max(rowHeight, b.Height)
		}
		height += rowHeight
	}
	height += size * matrixRowGap * float64(len(cellsBox)-1)
	width := 0.0
	for _, w := range widths {
		width += w
	}
	width += size * matrixColumnGap * float64(max(0, columns-1))
	brackets := size * 0.3
	if m.Command == "matrix" {
		brackets = 0
	}
	return MathBox{width + brackets*2 + size*0.2*2, height, height / 2}
}

func measureCases(c *MathCases, size float64) MathBox {
	height, width := 0.0, 0.0
	for _, r := range c.Rows {
		b := measureRow(caseRow(r), size)
		height += b.Height
		width = math.Max(width, b.Width)
	}
	height += size * matrixRowGap * float64(max(0, len(c.Rows)-1))
	width += size * 0.6
	return MathBox{width + size*0.3, height, height / 2}
}

// caseRow is a row of cases as rhwp reads it: the cells with a tab (&) between them.
func caseRow(cells []MathNode) []MathNode {
	var items []MathNode
	for c, cell := range cells {
		if c > 0 {
			items = append(items, &mathTab{})
		}
		if row, ok := cell.(*MathRow); ok {
			items = append(items, row.Items...)
		} else {
			items = append(items, cell)
		}
	}
	return items
}

func measurePile(p *MathPile, size float64) MathBox {
	height, width := 0.0, 0.0
	for _, r := range p.Rows {
		b := MeasureMath(r, size)
		height += b.Height
		width = math.Max(width, b.Width)
	}
	height += size * matrixRowGap * float64(max(0, len(p.Rows)-1))
	return MathBox{width, height, height / 2}
}

func measureBrackets(b *MathBrackets, size float64) MathBox {
	body := MeasureMath(b.Body, size)
	stretch := body.Height > size*1.2 && b.Left == "(" && b.Right == ")"
	pad := size * bracketPad
	bracket := size * 0.333
	if stretch {
		pad = size * 0.03
		bracket = size * 0.27
	}
	left, right := bracket, bracket
	if b.Left == "" {
		left = 0
	}
	if b.Right == "" {
		right = 0
	}
	return MathBox{left + pad + body.Width + pad + right, body.Height, body.Baseline}
}

func isCjk(text string) bool {
	for _, c := range text {
		if c >= 0x3000 && c <= 0x9FFF || c >= 0xF900 && c <= 0xFAFF || c >= 0xAC00 && c <= 0xD7AF {
			return true
		}
	}
	return false
}

// MathTextWidth is rhwp's estimate of a text's width.
func MathTextWidth(text string, size float64, italic bool) float64 {
	width := 0.0
	for _, r := range text {
		width += size * characterWidth(r)
	}
	if italic {
		return width * 1.05
	}
	return width
}

func characterWidth(c rune) float64 {
	if c < 0x80 {
		switch {
		case c >= 'A' && c <= 'Z':
			return 0.65
		case c >= 'a' && c <= 'z' || c >= '0' && c <= '9':
			return 0.55
		}
		return 0.5
	}
	switch c {
	case '′', '″', '‴':
		return 0.3
	case 'ϑ', 'ϖ':
		return 0.55
	case 'ϒ':
		return 0.65
	case '±', '∓', '×', '÷', '·', '∘', '†', '‡', '•':
		return 0.6
	case '≠', '≤', '≥', '≈', '≡', '≅', '∼', '≃', '≍', '≐', '∝', '≺', '≻':
		return 0.7
	case '∈', '∉', '∋', '⊂', '⊃', '⊆', '⊇', '∀', '∃', '¬', '∧', '∨':
		return 0.65
	case '⊏', '⊐', '⊑', '⊒', '⊻', '⊢', '⊣', '⊨':
		return 0.65
	case '∫', '∬', '∭', '∮', '∯', '∰':
		return 0.5
	case '∑', '∏', '∐':
		return 0.8
	case '∪', '∩', '⊔', '⊓', '⊎', '⋀', '⋁':
		return 0.7
	case '⊕', '⊗', '⊙', '⊖', '⊘':
		return 0.7
	case '↖', '↗', '↘', '↙', '↦', '↩', '↪':
		return 0.8
	case '⋯', '…', '⋮', '⋱':
		return 0.8
	case '∂', '∅', '∇', '∞', '∠', '∡', '∢', '⊾':
		return 0.6
	case '⊥', '⊤', '°', '‰', '‱', '♯':
		return 0.5
	case 'ℵ', 'ℏ', 'ı', 'ȷ', 'ℓ', '℘', 'ℑ', 'ℜ', 'ℒ', 'Å', '℧':
		return 0.6
	case '℃', '℉':
		return 0.9
	case '△', '▽', '○', '◇', '⋄':
		return 0.7
	}
	switch {
	case c >= 'α' && c <= 'ω':
		return 0.55
	case c >= 'Α' && c <= 'Ω':
		return 0.65
	case c >= '←' && c <= '↕' || c >= '⇐' && c <= '⇕':
		return 0.8
	case c >= 0x3000 && c <= 0x9FFF || c >= 0xF900 && c <= 0xFAFF || c >= 0xAC00 && c <= 0xD7AF:
		return 1.0
	}
	return 0.6
}
