package hwpx

// Word equations (Office Math, m:oMath) → the equation tree, which the HWPX writer stores as a 한글 equation (and other
// writers as linear text). Covers what Word's equation editor makes: fractions, scripts, roots, sums and integrals
// with limits, brackets, matrices, equation arrays and cases, functions and limits, accents and bars. Boxes, border
// boxes, phantoms and group characters keep their content. Plain (m:sty="p") and normal text is upright.

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/dseif0x/filee-go/internal/engines/hwp/xmltree"
)

// mathCommands are Unicode symbols written as 한글 equation commands (all known to rhwp too).
var mathCommands = map[rune]string{
	'α': "alpha", 'β': "beta", 'γ': "gamma", 'δ': "delta", 'ε': "epsilon", 'ϵ': "epsilon", 'ζ': "zeta", 'η': "eta",
	'θ': "theta", 'ϑ': "vartheta", 'ι': "iota", 'κ': "kappa", 'λ': "lambda", 'μ': "mu", 'ν': "nu", 'ξ': "xi",
	'ο': "omicron", 'π': "pi", 'ϖ': "varpi", 'ρ': "rho", 'σ': "sigma", 'ς': "varsigma", 'τ': "tau", 'υ': "upsilon",
	'φ': "phi", 'ϕ': "phi", 'χ': "chi", 'ψ': "psi", 'ω': "omega",
	'Α': "Alpha", 'Β': "Beta", 'Γ': "Gamma", 'Δ': "Delta", 'Ε': "Epsilon", 'Ζ': "Zeta", 'Η': "Eta", 'Θ': "Theta",
	'Ι': "Iota", 'Κ': "Kappa", 'Λ': "Lambda", 'Μ': "Mu", 'Ν': "Nu", 'Ξ': "Xi", 'Ο': "Omicron", 'Π': "Pi", 'Ρ': "Rho",
	'Σ': "Sigma", 'Τ': "Tau", 'Υ': "Upsilon", 'Φ': "Phi", 'Χ': "Chi", 'Ψ': "Psi", 'Ω': "Omega",
	'×': "TIMES", '÷': "DIV", '±': "PLUSMINUS", '∓': "MINUSPLUS", '·': "CDOT", '⋅': "CDOT", '∘': "CIRC", '•': "BULLET",
	'∙': "BULLET", '≠': "NEQ", '≤': "LEQ", '≦': "LEQ", '≥': "GEQ", '≧': "GEQ", '≈': "APPROX", '≡': "EQUIV", '≅': "CONG",
	'∼': "SIM", '≃': "SIMEQ", '∝': "PROPTO", '∞': "INF", '∂': "PARTIAL", '∈': "IN", '∉': "NOTIN", '∋': "OWNS",
	'⊂': "SUBSET", '⊃': "SUPERSET", '⊆': "SUBSETEQ", '⊇': "SUPSETEQ", '∪': "CUP", '∩': "CAP", '∀': "FORALL",
	'∃': "EXIST", '¬': "LNOT", '∧': "WEDGE", '∨': "VEE", '∅': "EMPTYSET", '∴': "THEREFORE", '∵': "BECAUSE",
	'→': "rarrow", '←': "larrow", '↔': "lrarrow", '↑': "uparrow", '↓': "downarrow", '⇒': "RARROW", '⇐': "LARROW",
	'⇔': "LRARROW", '⋯': "CDOTS", '…': "LDOTS", '⋮': "VDOTS", '⋱': "DDOTS", '°': "DEG", '′': "prime", 'ℓ': "ELL",
	'ℏ': "HBAR", 'ℵ': "ALEPH", '∠': "ANGLE", '△': "TRIANGLE", '⊥': "BOT", '{': "LBRACE", '}': "RBRACE",
	'\\': "RSLANT", '~': "SIM",
}

// bigOperators are large operators (m:nary, or the character typed as text) → their command.
var bigOperators = map[string]string{
	"∑": "SUM", "∏": "PROD", "∐": "COPROD", "⋃": "BIGCUP", "⋂": "BIGCAP", "⋁": "BIGVEE", "⋀": "BIGWEDGE",
	"∫": "INT", "∬": "DINT", "∭": "TINT", "∮": "OINT", "∯": "ODINT", "∰": "OTINT",
}

// mathFunctions are function names both 한글 and rhwp write upright by themselves.
var mathFunctions = setOf("sin", "cos", "tan", "cot", "sec", "csc", "arcsin", "arccos", "arctan", "sinh", "cosh", "tanh", "coth",
	"log", "ln", "lg", "exp", "det", "dim", "ker", "hom", "arg", "gcd", "lcm", "max", "min", "mod")

// mathReserved are words of the equation language, compared ignoring case: a variable spelled like one ("in", "to",
// "max", "Pi") is written one letter at a time so that 한글 and rhwp do not read it as a command.
var mathReserved = setOf("over", "atop", "sup", "sub", "sqrt", "root", "of", "left", "right", "bigg", "int", "integral", "oint", "smallint",
	"lim", "sum", "union", "inter", "prod", "rel", "buildrel", "pile", "lpile", "rpile", "cases", "choose", "binom",
	"matrix", "pmatrix", "bmatrix", "dmatrix", "vmatrix", "col", "lcol", "rcol", "lsub", "lsup", "ladder", "sladder",
	"longdiv", "color", "arch", "under", "not", "rm", "it", "bold", "from", "to", "inf", "infty", "in", "le", "ge",
	"ne", "ll", "gg", "text", "frac", "begin", "end", "quad", "qquad", "phantom", "eqalign", "benzene", "base",
	"top", "bot", "well", "att", "hund", "thou", "ohm", "mho", "wp", "ell", "liter", "image", "imag", "dots", "vert",
	"nabla", "deg", "prime", "star", "ast", "pm", "mp", "neg", "land", "lor", "xor", "gets", "implies", "iff",
	"mapsto", "pr", "limsup", "liminf", "ni", "exists", "angle", "triangle", "sim", "times", "div", "divide")

// mathReservedPrefixes are beginnings that rhwp (and Filee's equation fixes before rhwp) split off a longer word as a
// command ("overa" → over a, "sinx" → sin x, "lek" → le k, "INPUT" → IN PUT).
var mathReservedPrefixes = []string{
	"over", "atop", "bold", "it", "rm", "times", "sim", "prime", "root", "sqrt", "sin", "cos", "tan", "sec", "csc",
	"cot", "bar", "vec", "hat", "tilde", "dot", "ddot", "acute", "grave", "check", "breve", "leq", "geq", "neq",
	"triangle", "angle", "cap", "cup", "cdot", "ldots", "vdots", "ddots", "widehat", "widetilde", "le", "ge", "in",
	"circ", "subset", "superset", "notin", "approx", "equiv", "div", "pm", "mp", "smallinter", "smallunion", "owns",
	"inter", "union", "cong", "plusminus", "minusplus", "bullet", "oplus", "forall", "exist", "therefore", "because",
	"deg", "inf", "partial", "rarrow", "larrow", "lrarrow", "emptyset", "propto", "underline",
}

// mathAccents are accent characters (m:acc) → decoration command.
var mathAccents = map[rune]string{
	'̂': "hat", '^': "hat", '̌': "check", '̃': "tilde", '~': "tilde", '́': "acute", '̀': "grave",
	'̇': "dot", '̈': "ddot", '̄': "bar", '̅': "bar", '¯': "bar", '⃗': "vec", '⃖': "vec",
	'→': "vec", '⃡': "dyad",
}

func setOf(words ...string) map[string]bool {
	m := map[string]bool{}
	for _, w := range words {
		m[strings.ToLower(w)] = true
	}
	return m
}

var allMathCommandWords = func() map[string]bool {
	m := map[string]bool{}
	for _, v := range mathCommands {
		m[strings.ToLower(v)] = true
	}
	for _, v := range bigOperators {
		m[strings.ToLower(v)] = true
	}
	for v := range mathFunctions {
		m[v] = true
	}
	for _, v := range mathAccents {
		m[strings.ToLower(v)] = true
	}
	return m
}()

// isPlainWord tells a word that stays one variable (italic letters side by side) in 한글 and rhwp.
func isPlainWord(word string) bool {
	if len([]rune(word)) == 1 {
		return true
	}
	lower := strings.ToLower(word)
	if mathReserved[lower] || allMathCommandWords[lower] {
		return false
	}
	for _, p := range mathReservedPrefixes {
		if strings.HasPrefix(lower, p) {
			return false
		}
	}
	return true
}

// ReadOmml is the equation tree of one m:oMath.
func ReadOmml(oMath *xmltree.Element) MathNode { return mathContent(oMath, false) }

// ───────────────────────── Containers ─────────────────────────

// mathContent is the children of an argument (m:e, m:num, m:sub ...) side by side; nil gives an empty row.
func mathContent(container *xmltree.Element, roman bool) *MathRow {
	var items []MathNode
	for _, c := range container.Elements() {
		items = mathElement(c, items, roman)
	}
	return &MathRow{Items: items}
}

func mathArgument(parent *xmltree.Element, name string, roman bool) *MathRow {
	return mathContent(parent.ChildNS(nsM, name), roman)
}

// addRow adds a row as one group, or its only item by itself (the same to 한글, a simpler script).
func addRow(output []MathNode, row *MathRow) []MathNode {
	if len(row.Items) == 1 {
		return append(output, row.Items[0])
	}
	if len(row.Items) > 0 {
		return append(output, row)
	}
	return output
}

func mathElement(e *xmltree.Element, output []MathNode, roman bool) []MathNode {
	if e.Space == nsW {
		switch e.Local {
		case "r":
			// A non-math run inside an equation: upright text.
			var sb strings.Builder
			for _, t := range e.ChildrenNS(nsW, "t") {
				sb.WriteString(t.Value())
			}
			return mathText(sb.String(), output, styleNormal)
		case "ins", "smartTag", "customXml", "sdt", "sdtContent":
			for _, c := range e.Elements() {
				output = mathElement(c, output, roman)
			}
		}
		return output
	}
	if e.Space != nsM {
		return output
	}
	switch e.Local {
	case "r":
		return mathRun(e, output, roman)
	case "f":
		return append(output, mathFraction(e))
	case "sSub":
		return append(output, mathScripts(mathArgument(e, "e", roman), mathArgument(e, "sub", false), nil))
	case "sSup":
		return append(output, mathScripts(mathArgument(e, "e", roman), nil, mathArgument(e, "sup", false)))
	case "sSubSup":
		return append(output, mathScripts(mathArgument(e, "e", roman), mathArgument(e, "sub", false), mathArgument(e, "sup", false)))
	case "sPre":
		// Scripts before the base: {} _{a} ^{b} base.
		output = append(output, &MathScripts{Base: &MathRow{}, Sub: mathArgument(e, "sub", false), Sup: mathArgument(e, "sup", false)})
		return addRow(output, mathArgument(e, "e", false))
	case "rad":
		degree := mathArgument(e, "deg", false)
		hidden := mathFlag(e.ChildNS(nsM, "radPr").ChildNS(nsM, "degHide"))
		root := &MathRoot{Body: mathArgument(e, "e", false)}
		if !hidden && !isEmptyRow(degree) {
			root.Index = degree
		}
		return append(output, root)
	case "nary":
		return mathNary(e, output)
	case "d":
		return append(output, mathDelimiter(e))
	case "m":
		return append(output, mathMatrix(e, "matrix"))
	case "eqArr":
		pile := &MathPile{}
		for _, c := range e.ChildrenNS(nsM, "e") {
			pile.Rows = append(pile.Rows, withoutAlign(mathContent(c, roman)))
		}
		return append(output, pile)
	case "func":
		output = addRow(output, mathArgument(e, "fName", true))
		return addRow(output, mathArgument(e, "e", false))
	case "limLow":
		name := mathArgument(e, "e", roman)
		limit := mathArgument(e, "lim", false)
		if len(name.Items) == 1 {
			if _, ok := name.Items[0].(*MathLimit); ok {
				return append(output, &MathLimit{Lower: limit})
			}
		}
		return append(output, mathScripts(name, limit, nil))
	case "limUpp":
		return append(output, mathScripts(mathArgument(e, "e", roman), nil, mathArgument(e, "lim", false)))
	case "acc":
		chr := mathVal(e.ChildNS(nsM, "accPr").ChildNS(nsM, "chr"))
		body := mathArgument(e, "e", roman)
		mark := '̂'
		if chr != "" {
			mark = []rune(chr)[0]
		}
		if command, ok := mathAccents[mark]; ok {
			return append(output, &MathAccent{Command: command, Body: body})
		}
		return append(output, body)
	case "bar":
		top := mathVal(e.ChildNS(nsM, "barPr").ChildNS(nsM, "pos")) == "top"
		command := "UNDERLINE"
		if top {
			command = "OVERLINE"
		}
		return append(output, &MathAccent{Command: command, Body: mathArgument(e, "e", roman)})
	case "box", "borderBox", "phant", "groupChr":
		return addRow(output, mathArgument(e, "e", roman))
	case "oMath":
		return addRow(output, mathContent(e, roman))
	}
	return output
}

func mathFraction(f *xmltree.Element) MathNode {
	numerator := mathArgument(f, "num", false)
	denominator := mathArgument(f, "den", false)
	switch mathVal(f.ChildNS(nsM, "fPr").ChildNS(nsM, "type")) {
	case "noBar":
		return &MathFraction{Numerator: numerator, Denominator: denominator, Bar: false}
	case "lin", "skw":
		return &MathRow{Items: []MathNode{numerator, &MathSymbol{Text: "/"}, denominator}}
	}
	return &MathFraction{Numerator: numerator, Denominator: denominator, Bar: true}
}

// mathScripts: a large operator character as the base (∑ typed as text) takes the scripts as limits.
func mathScripts(base *MathRow, sub, sup *MathRow) MathNode {
	if len(base.Items) == 1 {
		if big, ok := base.Items[0].(*MathBigOperator); ok && big.Lower == nil && big.Upper == nil {
			c := *big
			c.Lower, c.Upper = nodeOf(sub), nodeOf(sup)
			return &c
		}
	}
	var b MathNode = base
	if len(base.Items) == 1 {
		b = base.Items[0]
	}
	return &MathScripts{Base: b, Sub: nodeOf(sub), Sup: nodeOf(sup)}
}

// nodeOf turns a nil row into a nil node (a typed nil would not compare to nil).
func nodeOf(row *MathRow) MathNode {
	if row == nil {
		return nil
	}
	return row
}

func mathNary(nary *xmltree.Element, output []MathNode) []MathNode {
	properties := nary.ChildNS(nsM, "naryPr")
	chr := "∫"
	if c := properties.ChildNS(nsM, "chr"); c != nil {
		if v, ok := c.AttrNS(nsM, "val"); ok {
			chr = v
		}
	}
	var sub, sup *MathRow
	if !mathFlag(properties.ChildNS(nsM, "subHide")) {
		sub = mathArgument(nary, "sub", false)
		if isEmptyRow(sub) {
			sub = nil
		}
	}
	if !mathFlag(properties.ChildNS(nsM, "supHide")) {
		sup = mathArgument(nary, "sup", false)
		if isEmptyRow(sup) {
			sup = nil
		}
	}
	if command, ok := bigOperators[chr]; ok {
		output = append(output, &MathBigOperator{Command: command, Glyph: chr, Lower: nodeOf(sub), Upper: nodeOf(sup)})
	} else {
		output = append(output, &MathScripts{Base: &MathText{Text: chr}, Sub: nodeOf(sub), Sup: nodeOf(sup)})
	}
	return addRow(output, mathArgument(nary, "e", false))
}

func mathDelimiter(d *xmltree.Element) MathNode {
	properties := d.ChildNS(nsM, "dPr")
	character := func(name, fallback string) string {
		if e := properties.ChildNS(nsM, name); e != nil {
			return mathVal(e)
		}
		return fallback
	}
	open := character("begChr", "(")
	close := character("endChr", ")")
	separator := character("sepChr", "|")
	parts := d.ChildrenNS(nsM, "e")

	// A matrix in brackets is a pmatrix / bmatrix / dmatrix; a brace before an equation array is cases.
	if len(parts) == 1 {
		if args := mathArguments(parts[0]); len(args) == 1 {
			only := args[0]
			style := ""
			switch open + close {
			case "()":
				style = "pmatrix"
			case "[]":
				style = "bmatrix"
			case "||":
				style = "dmatrix"
			}
			if only.Is(nsM, "m") && style != "" && len(open) == 1 && len(close) == 1 {
				return mathMatrix(only, style)
			}
			if only.Is(nsM, "eqArr") && open == "{" && close == "" {
				return mathCases(only)
			}
		}
	}
	var items []MathNode
	for i, p := range parts {
		if i > 0 {
			items = mathText(separator, items, stylePlain)
		}
		items = append(items, mathContent(p, false).Items...)
	}
	body := &MathRow{Items: items}
	if IsBracket(open) && IsBracket(close) {
		return &MathBrackets{Left: open, Right: close, Body: body}
	}
	// Brackets 한글 cannot stretch (rare): written as characters around the content.
	var row []MathNode
	row = mathText(open, row, stylePlain)
	row = append(row, body)
	row = mathText(close, row, stylePlain)
	return &MathRow{Items: row}
}

func mathMatrix(m *xmltree.Element, command string) *MathMatrix {
	matrix := &MathMatrix{Command: command}
	for _, row := range m.ChildrenNS(nsM, "mr") {
		var cells []MathNode
		for _, e := range row.ChildrenNS(nsM, "e") {
			cells = append(cells, mathContent(e, false))
		}
		matrix.Rows = append(matrix.Rows, cells)
	}
	return matrix
}

// mathCases splits each line of the equation array into cells at its alignment marks.
func mathCases(eqArr *xmltree.Element) *MathCases {
	cases := &MathCases{}
	for _, line := range eqArr.ChildrenNS(nsM, "e") {
		var cells []MathNode
		var cell []MathNode
		for _, item := range mathContent(line, false).Items {
			if _, ok := item.(*MathAlign); ok {
				cells = append(cells, &MathRow{Items: cell})
				cell = nil
			} else {
				cell = append(cell, item)
			}
		}
		cells = append(cells, &MathRow{Items: cell})
		cases.Rows = append(cases.Rows, cells)
	}
	return cases
}

func withoutAlign(row *MathRow) MathNode {
	var items []MathNode
	for _, i := range row.Items {
		if _, ok := i.(*MathAlign); !ok {
			items = append(items, i)
		}
	}
	return &MathRow{Items: items}
}

// mathArguments are the argument elements of a container (properties left out).
func mathArguments(container *xmltree.Element) []*xmltree.Element {
	var out []*xmltree.Element
	for _, e := range container.Elements() {
		if e.Space == nsM && !strings.HasSuffix(e.Local, "Pr") {
			out = append(out, e)
		}
	}
	return out
}

// ───────────────────────── Text ─────────────────────────

type mathStyle int

const (
	styleItalic mathStyle = iota
	stylePlain
	styleBold
	styleNormal // non-math text: upright, spaces kept
)

func mathRun(e *xmltree.Element, output []MathNode, roman bool) []MathNode {
	properties := e.ChildNS(nsM, "rPr")
	style := styleItalic
	if roman {
		style = stylePlain
	}
	if mathFlag(properties.ChildNS(nsM, "nor")) {
		style = styleNormal
	} else {
		switch mathVal(properties.ChildNS(nsM, "sty")) {
		case "p":
			style = stylePlain
		case "b", "bi":
			style = styleBold
		}
	}
	var text strings.Builder
	for _, c := range e.Elements() {
		switch {
		case c.Is(nsM, "t"), c.Is(nsW, "t"):
			text.WriteString(c.Value())
		case c.Is(nsW, "sym"):
			v, _ := c.AttrNS(nsW, "char")
			if code, err := strconv.ParseUint(v, 16, 32); err == nil {
				font, _ := c.AttrNS(nsW, "font")
				if m := SymbolCode(font, int(code)); m != "" {
					text.WriteString(m)
				} else {
					text.WriteRune(rune(code))
				}
			}
		}
	}
	return mathText(text.String(), output, style)
}

func isASCIIDigit(c rune) bool  { return c >= '0' && c <= '9' }
func isASCIILetter(c rune) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// mathText tokenises text: letters one by one, numbers, operators, symbols as commands.
func mathText(text string, output []MathNode, style mathStyle) []MathNode {
	if text == "" {
		return output
	}
	upright := style == stylePlain || style == styleNormal
	if trimmed := strings.TrimSpace(text); upright && mathFunctions[trimmed] && trimmed == strings.ToLower(trimmed) {
		return append(output, &MathFunction{Name: trimmed})
	}
	if upright && strings.TrimSpace(text) == "lim" {
		return append(output, &MathLimit{})
	}
	rs := []rune(text)
	var items []MathNode
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch {
		case isASCIIDigit(c):
			start := i
			for i+1 < len(rs) && (isASCIIDigit(rs[i+1]) || rs[i+1] == '.' && i+2 < len(rs) && isASCIIDigit(rs[i+2])) {
				i++
			}
			items = append(items, &MathNumber{Digits: string(rs[start : i+1])})
		case isASCIILetter(c):
			start := i
			for i+1 < len(rs) && isASCIILetter(rs[i+1]) {
				i++
			}
			word := string(rs[start : i+1])
			if isPlainWord(word) {
				items = append(items, &MathText{Text: word})
			} else {
				for _, letter := range word {
					items = append(items, &MathText{Text: string(letter)})
				}
			}
		case c == ' ' || c == ' ' || c == ' ' || c == ' ' || c == ' ' || c == ' ':
			if style == styleNormal {
				items = append(items, &MathSpace{})
			}
		case c == '&':
			items = append(items, &MathAlign{})
		case c == '−' || c == '–':
			items = append(items, &MathSymbol{Text: "-"})
		case strings.ContainsRune("+-=<>,.!|/*:'()[]", c):
			items = append(items, &MathSymbol{Text: string(c)})
		default:
			if command, ok := mathCommands[c]; ok {
				items = append(items, &MathCommand{Command: command, Glyph: string(c)})
			} else if big, ok := bigOperators[string(c)]; ok {
				items = append(items, &MathBigOperator{Command: big, Glyph: string(c)})
			} else if c == '#' || c == '_' || c == '^' || c == '`' {
				items = append(items, &MathQuoted{Text: string(c)})
			} else if c == '"' || unicode.IsControl(c) || c == '​' {
				// Quote marks would end a quoted text; zero-width and control characters show nothing.
			} else if c > 0xFFFF {
				items = append(items, &MathText{Text: mathAlphanumeric(c)})
			} else {
				items = append(items, &MathText{Text: string(c)})
			}
		}
	}
	hasText := false
	for _, i := range items {
		if _, ok := i.(*MathText); ok {
			hasText = true
			break
		}
	}
	if (style == stylePlain || style == styleNormal || style == styleBold) && hasText {
		command := "rm"
		if style == styleBold {
			command = "bold"
		}
		return append(output, &MathStyled{Command: command, Body: &MathRow{Items: items}})
	}
	return append(output, items...)
}

// mathAlphanumeric maps mathematical alphanumeric symbols (U+1D400…, italic 𝑥 from some converters) to the plain
// letter or digit; the equation's own style makes them italic or bold.
func mathAlphanumeric(c rune) string {
	if c >= 0x1D400 && c <= 0x1D6A3 {
		index := (c - 0x1D400) % 52
		if index < 26 {
			return string('A' + index)
		}
		return string('a' + index - 26)
	}
	if c >= 0x1D7CE && c <= 0x1D7FF {
		return string('0' + (c-0x1D7CE)%10)
	}
	return string(c)
}

// ───────────────────────── Helpers ─────────────────────────

func mathVal(e *xmltree.Element) string {
	v, _ := e.AttrNS(nsM, "val")
	return v
}

// mathFlag is an on/off property: present without a value, or with 1 / on / true.
func mathFlag(e *xmltree.Element) bool {
	if e == nil {
		return false
	}
	v, ok := e.AttrNS(nsM, "val")
	return !ok || v == "1" || v == "on" || v == "true"
}

func isEmptyRow(row *MathRow) bool {
	for _, i := range row.Items {
		switch v := i.(type) {
		case *MathRow:
			if !isEmptyRow(v) {
				return false
			}
		case *MathAlign:
		default:
			return false
		}
	}
	return true
}
