package spreadsheet

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/xdom"
)

// Excel number format codes ↔ OpenDocument data styles, so an ODS written
// from XLSX / XLS keeps what the cells show and an XLSX written from ODS
// keeps real number formats. Covered: digits with decimals, grouping and
// thousands scaling, percent, scientific, currency symbols, literal text,
// colours, positive / negative (/ zero) sections, dates, times and
// durations. Anything else falls back to General.

const (
	numberNs = "urn:oasis:names:tc:opendocument:xmlns:datastyle:1.0"
	styleNs  = "urn:oasis:names:tc:opendocument:xmlns:style:1.0"
	foNs     = "urn:oasis:names:tc:opendocument:xmlns:xsl-fo-compatible:1.0"
	loextNs  = "urn:org:documentfoundation:names:experimental:office:xmlns:loext:1.0"
)

var excelColors = [][2]string{{"Black", "#000000"}, {"Blue", "#0000FF"}, {"Cyan", "#00FFFF"}, {"Green", "#00FF00"},
	{"Magenta", "#FF00FF"}, {"Red", "#FF0000"}, {"White", "#FFFFFF"}, {"Yellow", "#FFFF00"}}

// OdfToExcel is the Excel code of a data style, or "" when it has none
// Excel could use. find looks up the styles that style:map applies.
func OdfToExcel(style *xdom.Node, find func(string) *xdom.Node) string {
	main, ok := sectionCode(style)
	if !ok {
		return ""
	}
	type mp struct {
		cond   string
		target *xdom.Node
	}
	var maps []mp
	for _, m := range style.All(styleNs, "map") {
		maps = append(maps, mp{strings.ReplaceAll(m.AN(styleNs, "condition"), " ", ""), find(m.AN(styleNs, "apply-style-name"))})
	}
	mapped := func(cond string) (string, bool) {
		for _, m := range maps {
			if m.cond == cond {
				if m.target == nil {
					return "", false
				}
				return sectionCode(m.target)
			}
		}
		return "", false
	}
	// LibreOffice keeps the last section in the style itself and maps the others by condition.
	if pos, ok := mapped("value()>=0"); ok {
		return pos + ";" + main
	}
	if plus, ok := mapped("value()>0"); ok {
		if minus, ok := mapped("value()<0"); ok {
			return plus + ";" + minus + ";" + main
		}
	}
	return main
}

func numInt(n *xdom.Node, attr string) (int, bool) {
	v, err := strconv.Atoi(n.AN(numberNs, attr))
	return v, err == nil
}

func sectionCode(style *xdom.Node) (string, bool) {
	kind := style.Name.Local
	isDate := kind == "date-style" || kind == "time-style"
	elapsed := style.AN(numberNs, "truncate-on-overflow") == "false"
	var sb strings.Builder
	if color, ok := style.Child(styleNs, "text-properties").Attr(foNs, "color"); ok {
		for _, c := range excelColors {
			if strings.EqualFold(c[1], color) {
				sb.WriteString("[" + c[0] + "]")
				break
			}
		}
	}
	for _, part := range style.Elements() {
		if part.Name.Space != numberNs {
			continue
		}
		long := part.AN(numberNs, "style") == "long"
		switch part.Name.Local {
		case "text":
			if kind == "percentage-style" {
				pieces := strings.Split(part.Value(), "%")
				for i, p := range pieces {
					pieces[i] = literal(p, false)
				}
				sb.WriteString(strings.Join(pieces, "%"))
			} else {
				sb.WriteString(literal(part.Value(), isDate))
			}
		case "number":
			sb.WriteString(odfDigits(part))
		case "scientific-number":
			m, ok := numInt(part, "min-integer-digits")
			if !ok {
				m = 1
			}
			places, _ := numInt(part, "decimal-places")
			sb.WriteString(strings.Repeat("0", max(1, m)))
			if places > 0 {
				sb.WriteString("." + strings.Repeat("0", places))
			}
			e, ok := numInt(part, "min-exponent-digits")
			if !ok {
				e = 2
			}
			sb.WriteString("E+" + strings.Repeat("0", max(1, e)))
		case "fraction":
			if v, ok := numInt(part, "min-integer-digits"); ok && v > 0 {
				sb.WriteString("# ")
			}
			n, ok := numInt(part, "min-numerator-digits")
			if !ok {
				n = 1
			}
			sb.WriteString(strings.Repeat("?", max(1, n)) + "/")
			if d, ok := numInt(part, "denominator-value"); ok {
				sb.WriteString(strconv.Itoa(d))
			} else {
				d, ok := numInt(part, "min-denominator-digits")
				if !ok {
					d = 1
				}
				sb.WriteString(strings.Repeat("?", max(1, d)))
			}
		case "currency-symbol":
			sb.WriteString(literal(part.Value(), false))
		case "year":
			sb.WriteString(pick(long, "yyyy", "yy"))
		case "month":
			textual := part.AN(numberNs, "textual") == "true"
			switch {
			case textual && long:
				sb.WriteString("mmmm")
			case textual:
				sb.WriteString("mmm")
			case long:
				sb.WriteString("mm")
			default:
				sb.WriteString("m")
			}
		case "day":
			sb.WriteString(pick(long, "dd", "d"))
		case "day-of-week":
			sb.WriteString(pick(long, "dddd", "ddd"))
		case "hours":
			h := pick(long, "hh", "h")
			if elapsed {
				h = "[" + h + "]"
			}
			sb.WriteString(h)
		case "minutes":
			sb.WriteString(pick(long, "mm", "m"))
		case "seconds":
			sb.WriteString(pick(long, "ss", "s"))
			if d, ok := numInt(part, "decimal-places"); ok && d > 0 {
				sb.WriteString("." + strings.Repeat("0", d))
			}
		case "am-pm":
			sb.WriteString("AM/PM")
		case "text-content":
			sb.WriteByte('@')
		case "fill-character":
		default:
			return "", false // boolean, era, quarter, week of year
		}
	}
	return sb.String(), true
}

func pick(c bool, a, b string) string {
	if c {
		return a
	}
	return b
}

func odfDigits(n *xdom.Node) string {
	places, ok := numInt(n, "decimal-places")
	if !ok {
		return "General"
	}
	minPlaces, ok := numInt(n, "min-decimal-places")
	if !ok {
		if v, err := strconv.Atoi(n.AN(loextNs, "min-decimal-places")); err == nil {
			minPlaces = v
		} else {
			minPlaces = places
		}
	}
	minPlaces = min(minPlaces, places)
	minInteger, ok := numInt(n, "min-integer-digits")
	if !ok {
		minInteger = 1
	}
	var sb strings.Builder
	if n.AN(numberNs, "grouping") == "true" {
		integer := strings.Repeat("0", minInteger)
		for len(integer) < 4 {
			integer = "#" + integer
		}
		sb.WriteString(integer[:len(integer)-3] + "," + integer[len(integer)-3:])
	} else if minInteger > 0 {
		sb.WriteString(strings.Repeat("0", minInteger))
	} else {
		sb.WriteByte('#')
	}
	if places > 0 {
		sb.WriteString("." + strings.Repeat("0", minPlaces) + strings.Repeat("#", places-minPlaces))
	}
	if f, err := strconv.ParseFloat(n.AN(numberNs, "display-factor"), 64); err == nil {
		for ; f >= 1000; f /= 1000 {
			sb.WriteByte(',')
		}
	}
	return sb.String()
}

// literal writes text for an Excel code: unquoted when Excel shows those
// characters as they are, otherwise quoted.
func literal(text string, isDate bool) string {
	plain := " -:()$+"
	if isDate {
		plain = " -/:.,()$+"
	}
	ok := true
	for _, r := range text {
		if !strings.ContainsRune(plain, r) {
			ok = false
			break
		}
	}
	if ok {
		return text
	}
	parts := strings.Split(text, `"`)
	for i, p := range parts {
		if p != "" {
			parts[i] = `"` + p + `"`
		}
	}
	return strings.Join(parts, `\"`)
}

// ---- Excel → OpenDocument ----

type oKind int

const (
	oLiteral oKind = iota
	oDigit
	oPoint
	oComma
	oPercent
	oExponent
	oSlash
	oYear
	oMonth
	oMinute
	oDay
	oHour
	oSecond
	oAmPm
	oText
	oColor
	oCurrency
	oGeneral
	oUnsupported
)

type oTok struct {
	kind    oKind
	text    string
	elapsed bool
}

type oSection struct {
	element, body, color string
	elapsed              bool
}

// ExcelToOdf returns the data style XML for an Excel format code (style
// name plus the styles its sections map to) and the office:value-type of
// its cells; ok is false for General, text formats and codes without an
// equivalent (written as plain numbers).
func ExcelToOdf(code, name string) (xml, valueType string, ok bool) {
	if IsGeneral(code) {
		return "", "", false
	}
	sections := splitSections(code)
	if len(sections) > 4 {
		return "", "", false
	}
	var parsed []*oSection
	for i, s := range sections {
		if i == 3 {
			break // the fourth section formats text cells
		}
		p := buildSection(tokenize(s))
		if p == nil {
			return "", "", false
		}
		parsed = append(parsed, p)
	}
	if parsed[0].element == "text-style" {
		return "", "", false
	}
	first := parsed[0]
	element := first.element
	if len(parsed) > 1 && (element == "date-style" || element == "time-style") {
		parsed = parsed[:1]
	}
	styleXML := func(styleName string, s *oSection, maps string) string {
		var sb strings.Builder
		fmt.Fprintf(&sb, `<number:%s style:name="%s"`, element, styleName)
		if styleName != name {
			sb.WriteString(` style:volatile="true"`)
		}
		if s.elapsed {
			sb.WriteString(` number:truncate-on-overflow="false"`)
		}
		sb.WriteByte('>')
		if s.color != "" {
			fmt.Fprintf(&sb, `<style:text-properties fo:color="%s"/>`, s.color)
		}
		sb.WriteString(s.body + maps + "</number:" + element + ">")
		return sb.String()
	}
	var sb strings.Builder
	switch len(parsed) {
	case 1:
		sb.WriteString(styleXML(name, first, ""))
	case 2:
		sb.WriteString(styleXML(name+"P0", first, ""))
		sb.WriteString(styleXML(name, parsed[1], fmt.Sprintf(`<style:map style:condition="value()&gt;=0" style:apply-style-name="%sP0"/>`, name)))
	default:
		sb.WriteString(styleXML(name+"P0", first, ""))
		sb.WriteString(styleXML(name+"P1", parsed[1], ""))
		sb.WriteString(styleXML(name, parsed[2], fmt.Sprintf(`<style:map style:condition="value()&gt;0" style:apply-style-name="%sP0"/><style:map style:condition="value()&lt;0" style:apply-style-name="%sP1"/>`, name, name)))
	}
	switch element {
	case "percentage-style":
		valueType = "percentage"
	case "currency-style":
		valueType = "currency"
	case "date-style":
		valueType = "date"
	case "time-style":
		valueType = "time"
	default:
		valueType = "float"
	}
	return sb.String(), valueType, true
}

func tokenize(section string) []oTok {
	if strings.EqualFold(strings.TrimSpace(section), "General") {
		return []oTok{{kind: oGeneral}}
	}
	rs := []rune(section)
	var toks []oTok
	for i := 0; i < len(rs); i++ {
		ch := rs[i]
		switch {
		case ch == '"':
			end := i + 1
			for end < len(rs) && rs[end] != '"' {
				end++
			}
			toks = append(toks, oTok{kind: oLiteral, text: string(rs[i+1 : min(end, len(rs))])})
			i = end
		case ch == '\\':
			if i+1 < len(rs) {
				i++
				toks = append(toks, oTok{kind: oLiteral, text: string(rs[i])})
			}
		case ch == '_':
			i++
			toks = append(toks, oTok{kind: oLiteral, text: " "})
		case ch == '*':
			i++
		case ch == '[':
			end := i + 1
			for end < len(rs) && rs[end] != ']' {
				end++
			}
			if end >= len(rs) {
				return []oTok{{kind: oUnsupported}}
			}
			toks = append(toks, bracketTok(string(rs[i+1:end])))
			i = end
		case ch == '0' || ch == '#' || ch == '?':
			toks = append(toks, oTok{kind: oDigit, text: string(ch)})
		case ch == '.':
			toks = append(toks, oTok{kind: oPoint, text: "."})
		case ch == ',':
			toks = append(toks, oTok{kind: oComma, text: ","})
		case ch == '%':
			toks = append(toks, oTok{kind: oPercent, text: "%"})
		case ch == '/':
			toks = append(toks, oTok{kind: oSlash, text: "/"})
		case ch == '@':
			toks = append(toks, oTok{kind: oText, text: "@"})
		case (ch == 'E' || ch == 'e') && i+1 < len(rs) && (rs[i+1] == '+' || rs[i+1] == '-'):
			i++
			toks = append(toks, oTok{kind: oExponent, text: "E" + string(rs[i])})
		case hasPrefixFold(rs[i:], "AM/PM"):
			toks = append(toks, oTok{kind: oAmPm, text: string(rs[i : i+5])})
			i += 4
		case hasPrefixFold(rs[i:], "A/P"):
			toks = append(toks, oTok{kind: oAmPm, text: string(rs[i : i+3])})
			i += 2
		default:
			lower := ch | 0x20
			kind := oLiteral
			switch lower {
			case 'y':
				kind = oYear
			case 'm':
				kind = oMonth
			case 'd':
				kind = oDay
			case 'h':
				kind = oHour
			case 's':
				kind = oSecond
			case 'e', 'g', 'b':
				kind = oUnsupported
			}
			if !(ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z') {
				kind = oLiteral
			}
			run := 1
			if kind != oLiteral {
				for i+run < len(rs) && rs[i+run]|0x20 == lower {
					run++
				}
			}
			text := string(ch)
			if kind != oLiteral {
				text = strings.Repeat(string(lower), run)
			}
			toks = append(toks, oTok{kind: kind, text: text})
			i += run - 1
		}
	}
	// "m" means minutes right after hours or right before seconds.
	for i := range toks {
		if toks[i].kind != oMonth {
			continue
		}
		before, after := oLiteral, oLiteral
		for j := i - 1; j >= 0; j-- {
			if isODatePart(toks[j].kind) {
				before = toks[j].kind
				break
			}
		}
		for j := i + 1; j < len(toks); j++ {
			if isODatePart(toks[j].kind) {
				after = toks[j].kind
				break
			}
		}
		if before == oHour || after == oSecond {
			toks[i].kind = oMinute
		}
	}
	return toks
}

func isODatePart(k oKind) bool {
	return k == oYear || k == oMonth || k == oMinute || k == oDay || k == oHour || k == oSecond || k == oAmPm
}

func bracketTok(inner string) oTok {
	if strings.HasPrefix(inner, "$") {
		sym := inner[1:]
		if i := strings.IndexByte(sym, '-'); i >= 0 {
			sym = sym[:i]
		}
		if sym != "" {
			return oTok{kind: oCurrency, text: sym}
		}
		return oTok{kind: oLiteral}
	}
	l := strings.ToLower(inner)
	if l != "" && strings.Trim(l, l[:1]) == "" && (l[0] == 'h' || l[0] == 'm' || l[0] == 's') {
		k := map[byte]oKind{'h': oHour, 'm': oMinute, 's': oSecond}[l[0]]
		return oTok{kind: k, text: l, elapsed: true}
	}
	for _, c := range excelColors {
		if strings.EqualFold(c[0], inner) {
			return oTok{kind: oColor, text: c[1]}
		}
	}
	return oTok{kind: oUnsupported, text: inner}
}

func buildSection(toks []oTok) *oSection {
	color := ""
	for _, t := range toks {
		if t.kind == oUnsupported {
			return nil
		}
		if t.kind == oColor {
			color = t.text
		}
	}
	for _, t := range toks {
		if t.kind == oText {
			return &oSection{element: "text-style"}
		}
	}
	for _, t := range toks {
		if isODatePart(t.kind) {
			return dateSection(toks, color)
		}
	}
	return numberSection(toks, color)
}

func dateSection(toks []oTok, color string) *oSection {
	var body, lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			body.WriteString("<number:text>" + doc.EscapeXML(lit.String()) + "</number:text>")
			lit.Reset()
		}
	}
	elapsed := false
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		length := len(strings.Trim(t.text, "[]"))
		switch t.kind {
		case oLiteral, oPoint, oComma, oSlash, oPercent:
			lit.WriteString(t.text)
			continue
		case oColor:
			continue
		case oDigit, oExponent, oCurrency, oGeneral:
			return nil
		}
		flush()
		switch t.kind {
		case oYear:
			body.WriteString(pick(length > 2, `<number:year number:style="long"/>`, "<number:year/>"))
		case oMonth:
			switch length {
			case 1:
				body.WriteString("<number:month/>")
			case 2:
				body.WriteString(`<number:month number:style="long"/>`)
			case 3, 5:
				body.WriteString(`<number:month number:textual="true"/>`)
			default:
				body.WriteString(`<number:month number:style="long" number:textual="true"/>`)
			}
		case oDay:
			switch length {
			case 1:
				body.WriteString("<number:day/>")
			case 2:
				body.WriteString(`<number:day number:style="long"/>`)
			case 3:
				body.WriteString("<number:day-of-week/>")
			default:
				body.WriteString(`<number:day-of-week number:style="long"/>`)
			}
		case oHour:
			elapsed = elapsed || t.elapsed
			body.WriteString(pick(length > 1, `<number:hours number:style="long"/>`, "<number:hours/>"))
		case oMinute:
			body.WriteString(pick(length > 1, `<number:minutes number:style="long"/>`, "<number:minutes/>"))
		case oSecond:
			places := 0
			if i+1 < len(toks) && toks[i+1].kind == oPoint {
				for i+2+places < len(toks) && toks[i+2+places].kind == oDigit && toks[i+2+places].text == "0" {
					places++
				}
				if places > 0 {
					i += 1 + places
				}
			}
			body.WriteString("<number:seconds")
			if length > 1 {
				body.WriteString(` number:style="long"`)
			}
			if places > 0 {
				fmt.Fprintf(&body, ` number:decimal-places="%d"`, places)
			}
			body.WriteString("/>")
		case oAmPm:
			body.WriteString("<number:am-pm/>")
		}
	}
	flush()
	isDate := false
	for _, t := range toks {
		if t.kind == oYear || t.kind == oMonth || t.kind == oDay {
			isDate = true
		}
	}
	return &oSection{element: pick(isDate, "date-style", "time-style"), body: body.String(), color: color, elapsed: elapsed && !isDate}
}

func numberSection(toks []oTok, color string) *oSection {
	for _, t := range toks {
		if t.kind == oSlash {
			return nil // fractions
		}
	}
	start := -1
	for i, t := range toks {
		if t.kind == oDigit || t.kind == oPoint || t.kind == oGeneral {
			start = i
			break
		}
	}
	end := start
	if start >= 0 {
		for end+1 < len(toks) && (toks[end+1].kind == oDigit || toks[end+1].kind == oPoint || toks[end+1].kind == oComma || toks[end+1].kind == oExponent) {
			end++
		}
		for _, t := range toks[end+1:] {
			if t.kind == oDigit || t.kind == oPoint || t.kind == oGeneral {
				return nil // text between the digits ("000-0000")
			}
		}
	}
	var body, lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			body.WriteString("<number:text>" + doc.EscapeXML(lit.String()) + "</number:text>")
			lit.Reset()
		}
	}
	percent, currency := false, false
	for i := 0; i < len(toks); i++ {
		if i == start {
			flush()
			if toks[i].kind == oGeneral {
				body.WriteString(`<number:number number:min-integer-digits="1"/>`)
			} else {
				body.WriteString(oDigits(toks[start : end+1]))
			}
			i = end
			continue
		}
		t := toks[i]
		switch t.kind {
		case oPercent:
			percent = true
			lit.WriteByte('%')
		case oCurrency:
			currency = true
			flush()
			body.WriteString("<number:currency-symbol>" + doc.EscapeXML(t.text) + "</number:currency-symbol>")
		case oLiteral, oComma, oPoint:
			lit.WriteString(t.text)
		}
	}
	flush()
	el := "number-style"
	if currency {
		el = "currency-style"
	} else if percent {
		el = "percentage-style"
	}
	return &oSection{element: el, body: body.String(), color: color}
}

func oDigits(d []oTok) string {
	exponent := -1
	for i, t := range d {
		if t.kind == oExponent {
			exponent = i
			break
		}
	}
	mant := d
	if exponent >= 0 {
		mant = d[:exponent]
	}
	point := -1
	for i, t := range mant {
		if t.kind == oPoint {
			point = i
			break
		}
	}
	integer, fraction := mant, []oTok(nil)
	if point >= 0 {
		integer, fraction = mant[:point], mant[point+1:]
	}
	count := func(ts []oTok, f func(oTok) bool) int {
		n := 0
		for _, t := range ts {
			if f(t) {
				n++
			}
		}
		return n
	}
	minInteger := count(integer, func(t oTok) bool { return t.text == "0" })
	lastIntDigit := -1
	for i, t := range integer {
		if t.kind == oDigit {
			lastIntDigit = i
		}
	}
	grouping := false
	for i := 0; i < max(0, lastIntDigit); i++ {
		if integer[i].kind == oComma {
			grouping = true
		}
	}
	lastDigit := -1
	for i, t := range mant {
		if t.kind == oDigit {
			lastDigit = i
		}
	}
	scaling := count(mant[lastDigit+1:], func(t oTok) bool { return t.kind == oComma })
	places := count(fraction, func(t oTok) bool { return t.kind == oDigit })
	minPlaces := count(fraction, func(t oTok) bool { return t.text == "0" })
	if exponent >= 0 {
		ed := count(d[exponent+1:], func(t oTok) bool { return t.kind == oDigit })
		return fmt.Sprintf(`<number:scientific-number number:decimal-places="%d" number:min-integer-digits="%d" number:min-exponent-digits="%d"/>`, places, max(1, minInteger), max(1, ed))
	}
	s := fmt.Sprintf(`<number:number number:decimal-places="%d" number:min-decimal-places="%d" number:min-integer-digits="%d"`, places, minPlaces, minInteger)
	if grouping {
		s += ` number:grouping="true"`
	}
	if scaling > 0 {
		s += fmt.Sprintf(` number:display-factor="%s"`, strconv.FormatFloat(math.Pow(1000, float64(scaling)), 'f', -1, 64))
	}
	return s + "/>"
}
