package spreadsheet

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// BuiltInFormats are the built-in number formats (ECMA-376 18.8.30); 14
// uses the ISO date order most locales read.
var BuiltInFormats = map[int]string{
	0: "General", 1: "0", 2: "0.00", 3: "#,##0", 4: "#,##0.00", 9: "0%", 10: "0.00%", 11: "0.00E+00",
	12: "# ?/?", 13: "# ??/??", 14: "yyyy-mm-dd", 15: "d-mmm-yy", 16: "d-mmm", 17: "mmm-yy",
	18: "h:mm AM/PM", 19: "h:mm:ss AM/PM", 20: "h:mm", 21: "h:mm:ss", 22: "yyyy-mm-dd h:mm",
	37: "#,##0 ;(#,##0)", 38: "#,##0 ;[Red](#,##0)", 39: "#,##0.00;(#,##0.00)", 40: "#,##0.00;[Red](#,##0.00)",
	45: "mm:ss", 46: "[h]:mm:ss", 47: "mmss.0", 48: "##0.0E+0", 49: "@",
}

// IsGeneral reports a missing format or "General" (any case).
func IsGeneral(format string) bool { return format == "" || strings.EqualFold(format, "General") }

// NumberFormatter formats cell values with Excel number format codes, the
// way Excel shows them (invariant culture); parsed formats are cached.
type NumberFormatter struct {
	date1904 bool
	cache    map[string]*numFormat
}

func NewFormatter(date1904 bool) *NumberFormatter {
	return &NumberFormatter{date1904: date1904, cache: map[string]*numFormat{}}
}

// Format returns the text Excel shows for value under format.
func (f *NumberFormatter) Format(value float64, format string) string {
	if IsGeneral(format) {
		return General(value)
	}
	p := f.parse(format)
	if p == nil {
		return General(value)
	}
	if out, ok := p.format(value, f.date1904); ok {
		return out
	}
	return General(value)
}

// IsDate reports whether format shows a date or time of day (not a duration like [h]:mm).
func (f *NumberFormatter) IsDate(format string) bool {
	if IsGeneral(format) {
		return false
	}
	p := f.parse(format)
	return p != nil && len(p.sections) > 0 && p.sections[0].date && !p.sections[0].elapsed
}

func (f *NumberFormatter) parse(format string) *numFormat {
	if p, ok := f.cache[format]; ok {
		return p
	}
	p := parseFormat(format)
	f.cache[format] = p
	return p
}

// General is Excel's General format: up to 11 significant characters, no trailing zeros.
func General(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "#NUM!"
	}
	if v == math.Floor(v) && math.Abs(v) < 1e11 {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	// .NET "G10": scientific when the exponent is ≥ 10 or < -5.
	exp := 0
	if v != 0 {
		e := strconv.FormatFloat(v, 'e', 9, 64)
		exp, _ = strconv.Atoi(e[strings.IndexByte(e, 'e')+1:])
	}
	if exp >= 10 || exp < -5 {
		mant, ex := v/math.Pow(10, float64(exp)), exp
		m := strconv.FormatFloat(mant, 'f', 5, 64)
		if strings.HasPrefix(strings.TrimPrefix(m, "-"), "10") {
			mant /= 10
			ex++
			m = strconv.FormatFloat(mant, 'f', 5, 64)
		}
		m = strings.TrimRight(strings.TrimRight(m, "0"), ".")
		sign := "+"
		if ex < 0 {
			sign, ex = "-", -ex
		}
		es := strconv.Itoa(ex)
		if len(es) < 2 {
			es = "0" + es
		}
		return m + "E" + sign + es
	}
	s := strconv.FormatFloat(v, 'g', 10, 64)
	if strings.ContainsRune(s, 'e') {
		s = strconv.FormatFloat(v, 'f', -1, 64)
	}
	if strings.ContainsRune(s, '.') {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

var oaEpoch = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

// ToSerial converts a date to Excel serial days. Excel counts the
// non-existent 29 February 1900, so serials before 1 March 1900 are one
// lower than OLE Automation dates.
func ToSerial(t time.Time) float64 {
	t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
	oa := float64(t.Sub(oaEpoch)) / float64(24*time.Hour)
	if oa < 0 {
		// OLE dates before 1899-12-30 count the day backwards and the time forwards.
		days := math.Floor(oa)
		frac := oa - days
		oa = days - frac
		if frac == 0 {
			oa = days
		}
	}
	if oa >= 1 && oa < 61 {
		return oa - 1
	}
	return oa
}

// FromSerial is the inverse of ToSerial (rounded to the millisecond).
func FromSerial(serial float64) time.Time {
	if serial >= 0 && serial < 60 {
		serial++
	}
	ms := math.Round(serial * 86400000)
	return oaEpoch.Add(time.Duration(ms) * time.Millisecond)
}

// ---- parsing ----

type tokKind int

const (
	tLiteral tokKind = iota
	tDigit           // 0 # ?
	tPoint
	tComma
	tPercent
	tExponent // E+ / E-
	tSlash
	tText // @
	tYear
	tMonth
	tDay
	tHour
	tMinute
	tSecond
	tSubSecond // the 0 digits after "ss."
	tAmPm
	tGeneral
	tElapsedHour
	tElapsedMinute
	tElapsedSecond
)

type token struct {
	kind tokKind
	text string // literal text, digit char, AM/PM marker
	n    int    // run length of date parts
}

type section struct {
	tokens   []token
	cond     string // "", "<", "<=", ">", ">=", "=", "<>"
	condVal  float64
	date     bool
	elapsed  bool
	text     bool
	hasAmPm  bool
	explicit bool // the section is chosen by a condition
}

type numFormat struct{ sections []*section }

func parseFormat(code string) *numFormat {
	var p numFormat
	for _, s := range splitSections(code) {
		sec := parseSection(s)
		if sec == nil {
			return nil
		}
		p.sections = append(p.sections, sec)
	}
	if len(p.sections) == 0 || len(p.sections) > 4 {
		return nil
	}
	return &p
}

func splitSections(code string) []string {
	var out []string
	start, quoted, bracket := 0, false, false
	for i := 0; i < len(code); i++ {
		switch ch := code[i]; {
		case ch == '\\' && !quoted:
			i++
		case ch == '"':
			quoted = !quoted
		case !quoted && ch == '[':
			bracket = true
		case !quoted && ch == ']':
			bracket = false
		case !quoted && !bracket && ch == ';':
			out = append(out, code[start:i])
			start = i + 1
		}
	}
	return append(out, code[start:])
}

func parseSection(s string) *section {
	sec := &section{}
	rs := []rune(s)
	lit := func(t string) { sec.tokens = append(sec.tokens, token{kind: tLiteral, text: t}) }
	for i := 0; i < len(rs); i++ {
		ch := rs[i]
		lower := ch | 0x20
		switch {
		case ch == '"':
			j := i + 1
			for j < len(rs) && rs[j] != '"' {
				j++
			}
			lit(string(rs[i+1 : min(j, len(rs))]))
			i = j
		case ch == '\\':
			if i+1 < len(rs) {
				i++
				lit(string(rs[i]))
			}
		case ch == '_':
			i++
			lit(" ")
		case ch == '*':
			i++
		case ch == '[':
			j := i + 1
			for j < len(rs) && rs[j] != ']' {
				j++
			}
			if j >= len(rs) {
				return nil
			}
			if !sec.bracket(string(rs[i+1 : j])) {
				return nil
			}
			i = j
		case ch == '0' || ch == '#' || ch == '?':
			sec.tokens = append(sec.tokens, token{kind: tDigit, text: string(ch)})
		case ch == '.':
			sec.tokens = append(sec.tokens, token{kind: tPoint})
		case ch == ',':
			sec.tokens = append(sec.tokens, token{kind: tComma})
		case ch == '%':
			sec.tokens = append(sec.tokens, token{kind: tPercent})
		case ch == '/':
			sec.tokens = append(sec.tokens, token{kind: tSlash})
		case ch == '@':
			sec.tokens = append(sec.tokens, token{kind: tText})
			sec.text = true
		case (ch == 'E' || ch == 'e') && i+1 < len(rs) && (rs[i+1] == '+' || rs[i+1] == '-'):
			sec.tokens = append(sec.tokens, token{kind: tExponent, text: string(rs[i+1])})
			i++
		case hasPrefixFold(rs[i:], "AM/PM"):
			sec.tokens = append(sec.tokens, token{kind: tAmPm, text: "AM/PM"})
			sec.hasAmPm = true
			i += 4
		case hasPrefixFold(rs[i:], "A/P"):
			sec.tokens = append(sec.tokens, token{kind: tAmPm, text: string(rs[i : i+3])})
			sec.hasAmPm = true
			i += 2
		case hasPrefixFold(rs[i:], "General"):
			sec.tokens = append(sec.tokens, token{kind: tGeneral})
			i += 6
		case lower == 'y' || lower == 'm' || lower == 'd' || lower == 'h' || lower == 's' || lower == 'e':
			n := 1
			for i+n < len(rs) && rs[i+n]|0x20 == lower {
				n++
			}
			kind := map[rune]tokKind{'y': tYear, 'm': tMonth, 'd': tDay, 'h': tHour, 's': tSecond, 'e': tYear}[lower]
			if lower == 'e' {
				n = 4
			}
			sec.tokens = append(sec.tokens, token{kind: kind, n: n})
			i += n - 1
		case lower == 'b' && i+1 < len(rs) && (rs[i+1] == '1' || rs[i+1] == '2'):
			i++ // Buddhist / Gregorian calendar switch
		default:
			lit(string(ch))
		}
	}
	// Date parts: "m" is minutes right after hours or right before seconds.
	for i, t := range sec.tokens {
		if t.kind != tMonth {
			continue
		}
		before, after := tLiteral, tLiteral
		for j := i - 1; j >= 0; j-- {
			if isDatePart(sec.tokens[j].kind) {
				before = sec.tokens[j].kind
				break
			}
		}
		for j := i + 1; j < len(sec.tokens); j++ {
			if isDatePart(sec.tokens[j].kind) {
				after = sec.tokens[j].kind
				break
			}
		}
		if before == tHour || before == tElapsedHour || after == tSecond || after == tElapsedSecond {
			sec.tokens[i].kind = tMinute
		}
	}
	for i, t := range sec.tokens {
		if isDatePart(t.kind) {
			sec.date = true
		}
		if t.kind == tElapsedHour || t.kind == tElapsedMinute || t.kind == tElapsedSecond {
			sec.elapsed = true
		}
		// "ss.00": the zeros after a seconds part are fractions of a second.
		if t.kind == tPoint && i > 0 && sec.date {
			prev := sec.tokens[i-1].kind
			if prev == tSecond || prev == tElapsedSecond {
				for j := i + 1; j < len(sec.tokens) && sec.tokens[j].kind == tDigit && sec.tokens[j].text == "0"; j++ {
					sec.tokens[j].kind = tSubSecond
				}
			}
		}
	}
	return sec
}

func isDatePart(k tokKind) bool {
	switch k {
	case tYear, tMonth, tDay, tHour, tMinute, tSecond, tAmPm, tElapsedHour, tElapsedMinute, tElapsedSecond:
		return true
	}
	return false
}

func hasPrefixFold(rs []rune, p string) bool {
	if len(rs) < len(p) {
		return false
	}
	return strings.EqualFold(string(rs[:len(p)]), p)
}

// bracket handles [Red], [$€-407], [h], [>100], [DBNum1], …
func (sec *section) bracket(inner string) bool {
	if inner == "" {
		return true
	}
	if inner[0] == '$' {
		sym := inner[1:]
		if i := strings.IndexByte(sym, '-'); i >= 0 {
			sym = sym[:i]
		}
		if sym != "" {
			sec.tokens = append(sec.tokens, token{kind: tLiteral, text: sym})
		}
		return true
	}
	l := strings.ToLower(inner)
	if strings.Trim(l, "h") == "" {
		sec.tokens = append(sec.tokens, token{kind: tElapsedHour, n: len(l)})
		return true
	}
	if strings.Trim(l, "m") == "" {
		sec.tokens = append(sec.tokens, token{kind: tElapsedMinute, n: len(l)})
		return true
	}
	if strings.Trim(l, "s") == "" {
		sec.tokens = append(sec.tokens, token{kind: tElapsedSecond, n: len(l)})
		return true
	}
	for _, op := range []string{"<=", ">=", "<>", "<", ">", "="} {
		if strings.HasPrefix(inner, op) {
			v, err := strconv.ParseFloat(strings.TrimSpace(inner[len(op):]), 64)
			if err != nil {
				return false
			}
			sec.cond, sec.condVal, sec.explicit = op, v, true
			return true
		}
	}
	return true // colours, locales, number systems: no text of their own
}

func (sec *section) matches(v float64) bool {
	switch sec.cond {
	case "<":
		return v < sec.condVal
	case "<=":
		return v <= sec.condVal
	case ">":
		return v > sec.condVal
	case ">=":
		return v >= sec.condVal
	case "=":
		return v == sec.condVal
	case "<>":
		return v != sec.condVal
	}
	return true
}

// ---- formatting ----

func (p *numFormat) format(v float64, date1904 bool) (string, bool) {
	var numeric []*section
	for _, s := range p.sections {
		if !(s.text && len(numeric) == 3) {
			numeric = append(numeric, s)
		}
	}
	if len(numeric) > 3 {
		numeric = numeric[:3]
	}
	// A lone text section shows the number as General.
	if len(numeric) == 1 && numeric[0].text && !numeric[0].date {
		return replaceText(numeric[0], General(v)), true
	}
	var sec *section
	abs := false
	conditional := false
	for _, s := range numeric {
		if s.explicit {
			conditional = true
		}
	}
	if conditional {
		for _, s := range numeric {
			if s.explicit && s.matches(v) {
				sec = s
				break
			}
		}
		if sec == nil {
			for _, s := range numeric {
				if !s.explicit {
					sec = s
					break
				}
			}
		}
		if sec == nil {
			return "", false
		}
		// A negative value shown by a second section loses its sign.
		abs = v < 0 && sec != numeric[0]
	} else {
		switch {
		case len(numeric) >= 3 && v == 0:
			sec = numeric[2]
		case len(numeric) >= 2 && v < 0:
			sec, abs = numeric[1], true
		default:
			sec = numeric[0]
		}
	}
	if abs {
		v = -v
	}
	if sec.text {
		return replaceText(sec, General(v)), true
	}
	if sec.date {
		return formatDate(sec, v, date1904)
	}
	return formatNumber(sec, v), true
}

func replaceText(sec *section, text string) string {
	var sb strings.Builder
	for _, t := range sec.tokens {
		switch t.kind {
		case tText:
			sb.WriteString(text)
		case tLiteral:
			sb.WriteString(t.text)
		}
	}
	return sb.String()
}

var (
	monthNames = []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	dayNames   = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
)

func formatDate(sec *section, v float64, date1904 bool) (string, bool) {
	if date1904 {
		v += 1462
	}
	if v < 0 || v > 2958465.99999 {
		return "", false
	}
	// Round to what is shown: fractions of a second or whole seconds.
	sub := 0
	for _, t := range sec.tokens {
		if t.kind == tSubSecond {
			sub++
		}
	}
	unit := math.Pow(10, float64(sub))
	totalUnits := math.Round(v * 86400 * unit)
	serial := totalUnits / unit / 86400
	day := math.Floor(serial)
	secondsUnits := totalUnits - day*86400*unit
	t := FromSerial(day)
	secs := int64(secondsUnits / unit)
	frac := int64(secondsUnits) % int64(unit)
	hour, minute, second := int(secs/3600), int(secs/60%60), int(secs%60)

	var sb strings.Builder
	subIndex := 0
	fracDigits := strconv.FormatInt(frac, 10)
	for len(fracDigits) < sub {
		fracDigits = "0" + fracDigits
	}
	for _, tk := range sec.tokens {
		switch tk.kind {
		case tLiteral:
			sb.WriteString(tk.text)
		case tPoint:
			sb.WriteByte('.')
		case tComma:
			sb.WriteByte(',')
		case tPercent:
			sb.WriteByte('%')
		case tSlash:
			sb.WriteByte('/')
		case tDigit:
			sb.WriteString(tk.text)
		case tSubSecond:
			if subIndex < len(fracDigits) {
				sb.WriteByte(fracDigits[subIndex])
			}
			subIndex++
		case tYear:
			y := t.Year()
			// 1900-01-00 is serial 0: shown as 1900.
			if day == 0 {
				y = 1900
			}
			if tk.n <= 2 {
				sb.WriteString(pad(y%100, 2))
			} else {
				sb.WriteString(pad(y, 4))
			}
		case tMonth:
			m := int(t.Month())
			if day == 0 {
				m = 1
			}
			switch {
			case tk.n == 1:
				sb.WriteString(strconv.Itoa(m))
			case tk.n == 2:
				sb.WriteString(pad(m, 2))
			case tk.n == 3:
				sb.WriteString(monthNames[m-1][:3])
			case tk.n == 5:
				sb.WriteString(monthNames[m-1][:1])
			default:
				sb.WriteString(monthNames[m-1])
			}
		case tDay:
			d := t.Day()
			if day == 0 {
				d = 0
			}
			wd := int(t.Weekday())
			if day < 60 && day >= 0 {
				// Excel's days before the phantom 29 February 1900 are a weekday later.
				wd = (wd + 6) % 7
			}
			switch {
			case tk.n == 1:
				sb.WriteString(strconv.Itoa(d))
			case tk.n == 2:
				sb.WriteString(pad(d, 2))
			case tk.n == 3:
				sb.WriteString(dayNames[wd][:3])
			default:
				sb.WriteString(dayNames[wd])
			}
		case tHour:
			h := hour
			if sec.hasAmPm {
				h = hour % 12
				if h == 0 {
					h = 12
				}
			}
			if tk.n >= 2 {
				sb.WriteString(pad(h, 2))
			} else {
				sb.WriteString(strconv.Itoa(h))
			}
		case tMinute:
			if tk.n >= 2 {
				sb.WriteString(pad(minute, 2))
			} else {
				sb.WriteString(strconv.Itoa(minute))
			}
		case tSecond:
			if tk.n >= 2 {
				sb.WriteString(pad(second, 2))
			} else {
				sb.WriteString(strconv.Itoa(second))
			}
		case tElapsedHour:
			sb.WriteString(pad(int(totalUnits/unit/3600), tk.n))
		case tElapsedMinute:
			sb.WriteString(pad(int(totalUnits/unit/60), tk.n))
		case tElapsedSecond:
			sb.WriteString(pad(int(totalUnits/unit), tk.n))
		case tAmPm:
			pm := hour >= 12
			if tk.text == "AM/PM" {
				if pm {
					sb.WriteString("PM")
				} else {
					sb.WriteString("AM")
				}
			} else {
				if pm {
					sb.WriteByte(tk.text[2])
				} else {
					sb.WriteByte(tk.text[0])
				}
			}
		case tGeneral:
			sb.WriteString(General(v))
		}
	}
	return sb.String(), true
}

func pad(v, n int) string {
	s := strconv.Itoa(v)
	for len(s) < n {
		s = "0" + s
	}
	return s
}

func formatNumber(sec *section, v float64) string {
	toks := sec.tokens
	neg := v < 0
	if neg {
		v = -v
	}
	// Where the number is: from the first digit placeholder to the last.
	first, last := -1, -1
	for i, t := range toks {
		if t.kind == tDigit || t.kind == tPoint && first >= 0 || t.kind == tGeneral {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	for i, t := range toks {
		if t.kind == tPoint && first < 0 {
			first, last = i, i
		}
	}
	percent := 0
	exponent := -1
	slash := -1
	for i, t := range toks {
		switch t.kind {
		case tPercent:
			percent++
		case tExponent:
			if exponent < 0 && i > first {
				exponent = i
			}
		case tSlash:
			if slash < 0 && first >= 0 && i > first {
				slash = i
			}
		}
	}
	// Extend the number over the exponent digits and fraction denominator.
	if exponent >= 0 || slash >= 0 {
		for i := last + 1; i < len(toks); i++ {
			if toks[i].kind == tDigit || toks[i].kind == tExponent || toks[i].kind == tSlash ||
				toks[i].kind == tLiteral && slash >= 0 && isDigits(toks[i].text) {
				last = i
			} else if toks[i].kind != tLiteral && toks[i].kind != tComma {
				break
			}
		}
	}
	v *= math.Pow(100, float64(percent))
	// Commas right after the last digit of the integer part divide by 1000 each; others group.
	grouping := false
	intEnd := last
	for i := first; i >= 0 && i <= last; i++ {
		if toks[i].kind == tPoint || toks[i].kind == tExponent || toks[i].kind == tSlash {
			intEnd = i - 1
			break
		}
	}
	scale := 0
	for i := intEnd; i >= 0 && first >= 0 && i >= first; i-- {
		if toks[i].kind == tComma {
			scale++
			continue
		}
		break
	}
	// Scaling commas may also follow the number ("0,").
	for i := last + 1; i < len(toks) && toks[i].kind == tComma; i++ {
		scale++
	}
	for i := first; first >= 0 && i <= intEnd-scale; i++ {
		if toks[i].kind == tComma {
			grouping = true
		}
	}
	v /= math.Pow(1000, float64(scale))

	var number string
	switch {
	case first < 0:
		number = ""
	case toks[first].kind == tGeneral && first == last:
		number = General(v)
	case exponent >= 0:
		number = scientific(toks[first:last+1], exponent-first, v)
	case slash >= 0:
		number = fraction(toks[first:last+1], slash-first, v)
	default:
		number = decimal(toks[first:last+1], v, grouping)
	}
	var sb strings.Builder
	if neg && !(isZeroText(number)) {
		sb.WriteByte('-')
	}
	for i := 0; i < len(toks); i++ {
		if i == first {
			sb.WriteString(number)
			i = last
			continue
		}
		switch t := toks[i]; t.kind {
		case tLiteral:
			sb.WriteString(t.text)
		case tPercent:
			sb.WriteByte('%')
		case tSlash:
			sb.WriteByte('/')
		case tGeneral:
			sb.WriteString(General(v))
		}
	}
	return sb.String()
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// isZeroText: the number rounds to zero, so it gets no minus sign.
func isZeroText(s string) bool {
	for _, c := range s {
		if c >= '1' && c <= '9' {
			return false
		}
	}
	return true
}

// roundDecimal rounds v ≥ 0 half away from zero at decimals places, on its
// 15 significant digits as Excel does; returns the integer and fraction digits.
func roundDecimal(v float64, decimals int) (string, string) {
	s := strconv.FormatFloat(v, 'e', 14, 64) // d.dddddddddddddde±x
	e := strings.IndexByte(s, 'e')
	exp, _ := strconv.Atoi(s[e+1:])
	digits := []byte(s[:1] + s[2:e])
	// The decimal point is after digit exp+1.
	point := exp + 1
	for point <= 0 {
		digits = append([]byte{'0'}, digits...)
		point++
	}
	for len(digits) < point+decimals+1 {
		digits = append(digits, '0')
	}
	keep := point + decimals
	if digits[keep] >= '5' {
		i := keep - 1
		for ; i >= 0; i-- {
			if digits[i] == '9' {
				digits[i] = '0'
				continue
			}
			digits[i]++
			break
		}
		if i < 0 {
			digits = append([]byte{'1'}, digits...)
			point++
			keep++
		}
	}
	intPart := strings.TrimLeft(string(digits[:point]), "0")
	if intPart == "" {
		intPart = "0"
	}
	return intPart, string(digits[point:keep])
}

// decimal renders "#,##0.00"-like placeholders.
func decimal(toks []token, v float64, grouping bool) string {
	point := -1
	for i, t := range toks {
		if t.kind == tPoint {
			point = i
			break
		}
	}
	intToks, fracToks := toks, []token(nil)
	if point >= 0 {
		intToks, fracToks = toks[:point], toks[point+1:]
	}
	decimals := 0
	for _, t := range fracToks {
		if t.kind == tDigit {
			decimals++
		}
	}
	ip, fp := roundDecimal(v, decimals)
	var sb strings.Builder
	sb.WriteString(integerPart(intToks, ip, grouping))
	if point >= 0 {
		var frac strings.Builder
		k := 0
		for _, t := range fracToks {
			switch t.kind {
			case tDigit:
				frac.WriteByte(fp[k])
				k++
			case tLiteral:
				frac.WriteString(t.text)
			}
		}
		// Optional places drop trailing zeros ('#') or show spaces ('?').
		f := []byte(frac.String())
		digitPos := len(fracToks) - 1
		for i := len(f) - 1; i >= 0 && digitPos >= 0; i-- {
			for digitPos >= 0 && fracToks[digitPos].kind != tDigit {
				digitPos--
			}
			if digitPos < 0 || f[i] != '0' || fracToks[digitPos].text == "0" {
				break
			}
			if fracToks[digitPos].text == "#" {
				f = f[:i]
			} else {
				f[i] = ' '
			}
			digitPos--
		}
		sb.WriteByte('.')
		sb.Write(f)
	}
	return sb.String()
}

// integerPart fills integer placeholders from the right; extra digits go to
// the first placeholder.
func integerPart(toks []token, digits string, grouping bool) string {
	var holders []int
	for i, t := range toks {
		if t.kind == tDigit {
			holders = append(holders, i)
		}
	}
	if digits == "0" {
		digits = ""
	}
	if len(holders) == 0 {
		var sb strings.Builder
		for _, t := range toks {
			if t.kind == tLiteral {
				sb.WriteString(t.text)
			}
		}
		return sb.String()
	}
	out := make([]string, len(toks))
	rest := digits
	for h := len(holders) - 1; h >= 0; h-- {
		i := holders[h]
		var d string
		if h == 0 {
			d, rest = rest, ""
		} else if rest != "" {
			d, rest = rest[len(rest)-1:], rest[:len(rest)-1]
		}
		if d == "" {
			switch toks[i].text {
			case "0":
				d = "0"
			case "?":
				d = " "
			}
		}
		out[i] = d
	}
	for i, t := range toks {
		if t.kind == tLiteral {
			out[i] = t.text
		}
	}
	s := strings.Join(out, "")
	if grouping {
		s = group(s)
	}
	return s
}

// group inserts thousands separators into the leading digit run.
func group(s string) string {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	run := s[:end]
	if len(run) <= 3 {
		return s
	}
	var sb strings.Builder
	for i, c := range run {
		if i > 0 && (len(run)-i)%3 == 0 {
			sb.WriteByte(',')
		}
		sb.WriteRune(c)
	}
	return sb.String() + s[end:]
}

func scientific(toks []token, e int, v float64) string {
	mant, expToks := toks[:e], toks[e+1:]
	sign := toks[e].text
	point := -1
	intHolders, decimals := 0, 0
	for i, t := range mant {
		if t.kind == tPoint && point < 0 {
			point = i
		} else if t.kind == tDigit {
			if point < 0 {
				intHolders++
			} else {
				decimals++
			}
		}
	}
	exp := 0
	if v != 0 {
		exp = int(math.Floor(math.Log10(v)))
		// "##0.0E+0": engineering notation, exponents in steps of the integer placeholders.
		if intHolders > 1 {
			exp = int(math.Floor(float64(exp)/float64(intHolders))) * intHolders
		}
	}
	m := v / math.Pow(10, float64(exp))
	ip, fp := roundDecimal(m, decimals)
	if len(ip) > max(1, intHolders) {
		step := max(1, intHolders)
		exp += step
		ip, fp = roundDecimal(v/math.Pow(10, float64(exp)), decimals)
	}
	var sb strings.Builder
	intToks, fracToks := mant, []token(nil)
	if point >= 0 {
		intToks, fracToks = mant[:point], mant[point+1:]
	}
	if ip == "0" && v == 0 {
		ip = "0"
	}
	sb.WriteString(integerPart(intToks, ip, false))
	if point >= 0 {
		sb.WriteByte('.')
		k := 0
		for _, t := range fracToks {
			if t.kind == tDigit && k < len(fp) {
				sb.WriteByte(fp[k])
				k++
			}
		}
	}
	sb.WriteByte('E')
	if exp < 0 {
		sb.WriteByte('-')
		exp = -exp
	} else if sign == "+" {
		sb.WriteByte('+')
	}
	width := 0
	for _, t := range expToks {
		if t.kind == tDigit && t.text == "0" {
			width++
		}
	}
	sb.WriteString(pad(exp, width))
	return sb.String()
}

func fraction(toks []token, slash int, v float64) string {
	num, den := toks[:slash], toks[slash+1:]
	// A whole-number part: digits, then a literal (space), then the numerator.
	split := -1
	for i := 0; i < len(num); i++ {
		if num[i].kind == tLiteral && i > 0 && num[i-1].kind == tDigit {
			for j := i + 1; j < len(num); j++ {
				if num[j].kind == tDigit {
					split = i
				}
			}
			if split >= 0 {
				break
			}
		}
	}
	whole := 0.0
	rest := v
	var wholeToks, numToks []token
	if split >= 0 {
		wholeToks, numToks = num[:split], num[split+1:]
		whole = math.Floor(v)
		rest = v - whole
	} else {
		numToks = num
	}
	fixed := 0
	denDigits := 0
	for _, t := range den {
		if t.kind == tLiteral && isDigits(t.text) {
			fixed, _ = strconv.Atoi(t.text)
		}
		if t.kind == tDigit {
			denDigits++
		}
	}
	var n, d int
	if fixed > 0 {
		d = fixed
		n = int(math.Round(rest * float64(d)))
	} else {
		limit := int(math.Pow(10, float64(max(1, denDigits)))) - 1
		best := math.Inf(1)
		for q := 1; q <= limit; q++ {
			p := math.Round(rest * float64(q))
			if diff := math.Abs(rest - p/float64(q)); diff < best-1e-12 {
				best, n, d = diff, int(p), q
			}
		}
	}
	if split >= 0 && n == d && d > 0 {
		whole++
		n = 0
	}
	var sb strings.Builder
	if split >= 0 {
		w := strconv.FormatFloat(whole, 'f', 0, 64)
		if whole == 0 && n != 0 {
			w = ""
		}
		sb.WriteString(integerPart(wholeToks, w, false))
		if n == 0 {
			// "1" with no fraction: Excel pads the fraction with spaces.
			sb.WriteString(strings.Repeat(" ", len(numToks)+1+len(den)+1))
			return strings.TrimRight(sb.String(), " ")
		}
		sb.WriteString(num[split].text)
	} else if n == 0 && v == 0 {
		n = 0
	}
	sb.WriteString(integerPart(numToks, strconv.Itoa(n), false))
	sb.WriteByte('/')
	if fixed > 0 {
		sb.WriteString(strconv.Itoa(fixed))
	} else {
		ds := strconv.Itoa(d)
		for len(ds) < denDigits {
			ds += " "
		}
		sb.WriteString(ds)
	}
	return strings.TrimRight(sb.String(), " ")
}
