package data

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// JSON and JSON Lines. Reading allows comments and trailing commas (JSONC, as
// in settings files); writing is indented by two spaces, keeps non-ASCII text
// as it is (Korean, Chinese) and ends with a line break.

func readJSON(text string) (any, error) {
	v, err := parseJSON(text)
	if err != nil {
		return nil, fmt.Errorf("Not valid JSON: %v", err)
	}
	return v, nil
}

// readJSONLines reads one value per line (blank lines skipped) as an array.
func readJSONLines(text string) ([]any, error) {
	values := []any{}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		v, err := parseJSON(line)
		if err != nil {
			return nil, fmt.Errorf("Line %d is not valid JSON: %v", i+1, err)
		}
		values = append(values, v)
	}
	return values, nil
}

type jsonParser struct {
	s     string
	i     int
	depth int
}

func parseJSON(s string) (any, error) {
	p := &jsonParser{s: strings.TrimPrefix(s, "\xEF\xBB\xBF")}
	if err := p.skip(); err != nil {
		return nil, err
	}
	if p.i >= len(p.s) {
		return nil, p.fail("the input is empty")
	}
	v, err := p.value()
	if err != nil {
		return nil, err
	}
	if err := p.skip(); err != nil {
		return nil, err
	}
	if p.i < len(p.s) {
		return nil, p.fail("unexpected %q after the value", p.s[p.i])
	}
	return v, nil
}

func (p *jsonParser) fail(format string, args ...any) error {
	line, col := 1, 1
	for _, c := range p.s[:min(p.i, len(p.s))] {
		if c == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return fmt.Errorf("%s at line %d, column %d", fmt.Sprintf(format, args...), line, col)
}

// skip skips white space and comments.
func (p *jsonParser) skip() error {
	for p.i < len(p.s) {
		switch c := p.s[p.i]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			p.i++
		case c == '/' && p.i+1 < len(p.s) && p.s[p.i+1] == '/':
			for p.i < len(p.s) && p.s[p.i] != '\n' {
				p.i++
			}
		case c == '/' && p.i+1 < len(p.s) && p.s[p.i+1] == '*':
			end := strings.Index(p.s[p.i+2:], "*/")
			if end < 0 {
				return p.fail("unterminated comment")
			}
			p.i += end + 4
		default:
			return nil
		}
	}
	return nil
}

func (p *jsonParser) value() (any, error) {
	if p.i >= len(p.s) {
		return nil, p.fail("unexpected end of the input")
	}
	switch c := p.s[p.i]; {
	case c == '{':
		return p.object()
	case c == '[':
		return p.array()
	case c == '"':
		return p.str()
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	case strings.HasPrefix(p.s[p.i:], "true"):
		p.i += 4
		return true, nil
	case strings.HasPrefix(p.s[p.i:], "false"):
		p.i += 5
		return false, nil
	case strings.HasPrefix(p.s[p.i:], "null"):
		p.i += 4
		return nil, nil
	default:
		return nil, p.fail("unexpected %q", c)
	}
}

func (p *jsonParser) enter() error {
	p.depth++
	if p.depth > MaxDepth {
		return p.fail("the maximum depth of %d is exceeded", MaxDepth)
	}
	return nil
}

func (p *jsonParser) object() (any, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer func() { p.depth-- }()
	p.i++
	m := NewMap()
	for {
		if err := p.skip(); err != nil {
			return nil, err
		}
		if p.i < len(p.s) && p.s[p.i] == '}' {
			p.i++
			return m, nil
		}
		if p.i >= len(p.s) || p.s[p.i] != '"' {
			return nil, p.fail("expected a property name")
		}
		k, err := p.str()
		if err != nil {
			return nil, err
		}
		if err := p.skip(); err != nil {
			return nil, err
		}
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			return nil, p.fail("expected ':'")
		}
		p.i++
		if err := p.skip(); err != nil {
			return nil, err
		}
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		m.Set(k, v)
		if err := p.skip(); err != nil {
			return nil, err
		}
		if p.i < len(p.s) && p.s[p.i] == ',' {
			p.i++
			continue
		}
		if p.i < len(p.s) && p.s[p.i] == '}' {
			p.i++
			return m, nil
		}
		return nil, p.fail("expected ',' or '}'")
	}
}

func (p *jsonParser) array() (any, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer func() { p.depth-- }()
	p.i++
	list := []any{}
	for {
		if err := p.skip(); err != nil {
			return nil, err
		}
		if p.i < len(p.s) && p.s[p.i] == ']' {
			p.i++
			return list, nil
		}
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		list = append(list, v)
		if err := p.skip(); err != nil {
			return nil, err
		}
		if p.i < len(p.s) && p.s[p.i] == ',' {
			p.i++
			continue
		}
		if p.i < len(p.s) && p.s[p.i] == ']' {
			p.i++
			return list, nil
		}
		return nil, p.fail("expected ',' or ']'")
	}
}

func (p *jsonParser) str() (string, error) {
	p.i++
	var b strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c == '"':
			p.i++
			return b.String(), nil
		case c < 0x20:
			return "", p.fail("a control character in a string")
		case c == '\\':
			if p.i+1 >= len(p.s) {
				return "", p.fail("unterminated string")
			}
			e := p.s[p.i+1]
			p.i += 2
			switch e {
			case '"', '\\', '/':
				b.WriteByte(e)
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'u':
				r, ok := p.hex4()
				if !ok {
					return "", p.fail("a bad \\u escape")
				}
				if utf16.IsSurrogate(r) && strings.HasPrefix(p.s[p.i:], "\\u") {
					save := p.i
					p.i += 2
					if r2, ok := p.hex4(); ok && utf16.DecodeRune(r, r2) != utf8.RuneError {
						r = utf16.DecodeRune(r, r2)
					} else {
						p.i = save
					}
				}
				b.WriteRune(r)
			default:
				return "", p.fail("a bad escape \\%c", e)
			}
		default:
			b.WriteByte(c)
			p.i++
		}
	}
	return "", p.fail("unterminated string")
}

func (p *jsonParser) hex4() (rune, bool) {
	if p.i+4 > len(p.s) {
		return 0, false
	}
	n, err := strconv.ParseUint(p.s[p.i:p.i+4], 16, 32)
	if err != nil {
		return 0, false
	}
	p.i += 4
	return rune(n), true
}

func (p *jsonParser) number() (any, error) {
	start := p.i
	if p.s[p.i] == '-' {
		p.i++
	}
	digits := func() int {
		n := 0
		for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
			p.i++
			n++
		}
		return n
	}
	if p.i < len(p.s) && p.s[p.i] == '0' {
		p.i++
	} else if digits() == 0 {
		return nil, p.fail("a bad number")
	}
	if p.i < len(p.s) && p.s[p.i] == '.' {
		p.i++
		if digits() == 0 {
			return nil, p.fail("a bad number")
		}
	}
	if p.i < len(p.s) && (p.s[p.i] == 'e' || p.s[p.i] == 'E') {
		p.i++
		if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
			p.i++
		}
		if digits() == 0 {
			return nil, p.fail("a bad number")
		}
	}
	return number(p.s[start:p.i]), nil
}

// ---- writing ----

// writeJSON is indented JSON (two spaces, "\n").
func writeJSON(root any) (string, error) {
	var b strings.Builder
	if err := jsonValue(&b, root, 0, true); err != nil {
		return "", err
	}
	b.WriteByte('\n')
	return b.String(), nil
}

// writeJSONLines is one compact line per record (see unwrapRecords); anything
// else becomes one line.
func writeJSONLines(root any) (string, error) {
	records, ok := unwrapRecords(root).([]any)
	if !ok {
		records = []any{root}
	}
	var b strings.Builder
	for _, r := range records {
		if err := jsonValue(&b, r, 0, false); err != nil {
			return "", err
		}
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func compactJSON(v any) string {
	var b strings.Builder
	_ = jsonValue(&b, v, 0, true)
	return b.String()
}

func newline(b *strings.Builder, indent bool, depth int) {
	if indent {
		b.WriteByte('\n')
		b.WriteString(strings.Repeat("  ", depth))
	}
}

func jsonValue(b *strings.Builder, v any, depth int, indent bool) error {
	if depth > MaxDepth {
		return errTooDeep
	}
	switch x := v.(type) {
	case *Map:
		if x.Len() == 0 {
			b.WriteString("{}")
			return nil
		}
		b.WriteByte('{')
		for i, k := range x.keys {
			if i > 0 {
				b.WriteByte(',')
			}
			newline(b, indent, depth+1)
			jsonString(b, k)
			b.WriteByte(':')
			if indent {
				b.WriteByte(' ')
			}
			if err := jsonValue(b, x.vals[k], depth+1, indent); err != nil {
				return err
			}
		}
		newline(b, indent, depth)
		b.WriteByte('}')
	case []any:
		if len(x) == 0 {
			b.WriteString("[]")
			return nil
		}
		b.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			newline(b, indent, depth+1)
			if err := jsonValue(b, item, depth+1, indent); err != nil {
				return err
			}
		}
		newline(b, indent, depth)
		b.WriteByte(']')
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(text(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case *big.Int:
		b.WriteString(jsonNumber(x.String(), false))
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			jsonString(b, text(x)) // JSON has no number for these
		} else {
			b.WriteString(jsonNumber(formatDouble(x), true))
		}
	default:
		jsonString(b, text(v))
	}
	return nil
}

// decimal's range in .NET, which the original writes numbers through.
var maxDecimal, _ = new(big.Float).SetString("79228162514264337593543950335")

// jsonNumber writes a big integer or a float ("3.0" stays a float) as .NET's
// decimal would: digits and scale kept, exponents expanded; numbers beyond
// decimal's range (1e300) as written.
func jsonNumber(t string, isFloat bool) string {
	mant, exps, hasExp := strings.Cut(strings.ToLower(t), "e")
	if !hasExp {
		return t // a big integer or a float with a fraction: as written
	}
	exp, err := strconv.Atoi(exps)
	if err != nil {
		return t
	}
	neg := strings.HasPrefix(mant, "-")
	mant = strings.TrimPrefix(mant, "-")
	intPart, frac, _ := strings.Cut(mant, ".")
	digits := strings.TrimLeft(intPart+frac, "0")
	if digits == "" {
		return t // zero with an exponent
	}
	point := len(intPart) + exp // digits before the point in intPart+frac
	all := intPart + frac
	var s string
	switch {
	case point <= 0:
		s = "0." + strings.Repeat("0", -point) + all
	case point >= len(all):
		s = all + strings.Repeat("0", point-len(all))
	default:
		s = all[:point] + "." + all[point:]
	}
	ip, fp, _ := strings.Cut(s, ".")
	ip = strings.TrimLeft(ip, "0")
	if ip == "" {
		ip = "0"
	}
	if len(fp) > 28 || len(strings.TrimLeft(ip, "0")) > 29 {
		return t
	}
	if v, ok := new(big.Float).SetString(ip); ok && v.Cmp(maxDecimal) > 0 {
		return t
	}
	s = ip
	if fp != "" {
		s += "." + fp
	} else if isFloat {
		s += ".0"
	}
	if neg {
		s = "-" + s
	}
	return s
}

// jsonString escapes only what JSON requires (quotes, backslashes, control characters).
func jsonString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 || r == 0x7F {
				fmt.Fprintf(b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}
