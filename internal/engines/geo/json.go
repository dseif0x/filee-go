package geo

// A small JSON reader that keeps the order of object members (attributes are
// written back in file order) and, like System.Text.Json with the options the
// GeoJSON reader uses, skips comments and allows trailing commas. Numbers keep
// their text, so whole numbers stay whole and huge ones can be reported.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type jsonKind int

const (
	jsonNull jsonKind = iota
	jsonBool
	jsonNumber
	jsonString
	jsonArray
	jsonObject
)

type jsonMember struct {
	name  string
	value *jsonValue
}

type jsonValue struct {
	kind    jsonKind
	boolean bool
	text    string // string value, or the number as written
	items   []*jsonValue
	members []jsonMember
	raw     string // the value's own JSON text (objects and arrays as attributes)
}

func (v *jsonValue) get(name string) (*jsonValue, bool) {
	if v == nil || v.kind != jsonObject {
		return nil, false
	}
	for _, m := range v.members {
		if m.name == name {
			return m.value, true
		}
	}
	return nil, false
}

// float is the number's value; ok is false when it is not a number. Huge numbers give ±Inf.
func (v *jsonValue) float() (float64, bool) {
	if v.kind != jsonNumber {
		return 0, false
	}
	f, err := strconv.ParseFloat(v.text, 64)
	if err != nil && !isRangeError(err) {
		return 0, false
	}
	return f, true
}

const jsonMaxDepth = 128

type jsonParser struct {
	s   string
	pos int
}

func parseJSON(s string) (*jsonValue, error) {
	p := &jsonParser{s: s}
	p.space()
	v, err := p.value(0)
	if err != nil {
		return nil, err
	}
	p.space()
	if p.pos < len(p.s) {
		return nil, p.fail("'%c' is invalid after a single JSON value", p.s[p.pos])
	}
	return v, nil
}

func (p *jsonParser) fail(format string, args ...any) error {
	line, col := 0, 0
	for i := 0; i < p.pos && i < len(p.s); i++ {
		col++
		if p.s[i] == '\n' {
			line++
			col = 0
		}
	}
	return fmt.Errorf("%s. LineNumber: %d | BytePositionInLine: %d.", fmt.Sprintf(format, args...), line, col)
}

func (p *jsonParser) space() {
	for p.pos < len(p.s) {
		switch c := p.s[p.pos]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			p.pos++
		case c == '/' && p.pos+1 < len(p.s) && p.s[p.pos+1] == '/':
			end := strings.IndexByte(p.s[p.pos:], '\n')
			if end < 0 {
				p.pos = len(p.s)
			} else {
				p.pos += end + 1
			}
		case c == '/' && p.pos+1 < len(p.s) && p.s[p.pos+1] == '*':
			end := strings.Index(p.s[p.pos+2:], "*/")
			if end < 0 {
				p.pos = len(p.s)
			} else {
				p.pos += end + 4
			}
		default:
			return
		}
	}
}

func (p *jsonParser) value(depth int) (*jsonValue, error) {
	if depth > jsonMaxDepth {
		return nil, p.fail("The maximum configured depth of %d has been exceeded", jsonMaxDepth)
	}
	if p.pos >= len(p.s) {
		return nil, p.fail("Expected a value, but the end of the data was reached")
	}
	start := p.pos
	switch c := p.s[p.pos]; {
	case c == '{':
		p.pos++
		v := &jsonValue{kind: jsonObject}
		for {
			p.space()
			if p.pos < len(p.s) && p.s[p.pos] == '}' {
				p.pos++
				break
			}
			if p.pos >= len(p.s) || p.s[p.pos] != '"' {
				return nil, p.fail("Expected a property name or '}'")
			}
			name, err := p.str()
			if err != nil {
				return nil, err
			}
			p.space()
			if p.pos >= len(p.s) || p.s[p.pos] != ':' {
				return nil, p.fail("Expected ':' after a property name")
			}
			p.pos++
			p.space()
			item, err := p.value(depth + 1)
			if err != nil {
				return nil, err
			}
			v.members = append(v.members, jsonMember{name, item})
			p.space()
			if p.pos < len(p.s) && p.s[p.pos] == ',' {
				p.pos++
				continue
			}
			if p.pos < len(p.s) && p.s[p.pos] == '}' {
				p.pos++
				break
			}
			return nil, p.fail("Expected ',' or '}' after a property value")
		}
		v.raw = p.s[start:p.pos]
		return v, nil
	case c == '[':
		p.pos++
		v := &jsonValue{kind: jsonArray}
		for {
			p.space()
			if p.pos < len(p.s) && p.s[p.pos] == ']' {
				p.pos++
				break
			}
			item, err := p.value(depth + 1)
			if err != nil {
				return nil, err
			}
			v.items = append(v.items, item)
			p.space()
			if p.pos < len(p.s) && p.s[p.pos] == ',' {
				p.pos++
				continue
			}
			if p.pos < len(p.s) && p.s[p.pos] == ']' {
				p.pos++
				break
			}
			return nil, p.fail("Expected ',' or ']' after an array value")
		}
		v.raw = p.s[start:p.pos]
		return v, nil
	case c == '"':
		s, err := p.str()
		if err != nil {
			return nil, err
		}
		return &jsonValue{kind: jsonString, text: s, raw: p.s[start:p.pos]}, nil
	case c == '-' || c >= '0' && c <= '9':
		p.pos++
		for p.pos < len(p.s) && strings.IndexByte("0123456789+-.eE", p.s[p.pos]) >= 0 {
			p.pos++
		}
		text := p.s[start:p.pos]
		if !validJSONNumber(text) {
			return nil, p.fail("'%s' is an invalid number", text)
		}
		return &jsonValue{kind: jsonNumber, text: text, raw: text}, nil
	case strings.HasPrefix(p.s[p.pos:], "true"):
		p.pos += 4
		return &jsonValue{kind: jsonBool, boolean: true, raw: "true"}, nil
	case strings.HasPrefix(p.s[p.pos:], "false"):
		p.pos += 5
		return &jsonValue{kind: jsonBool, raw: "false"}, nil
	case strings.HasPrefix(p.s[p.pos:], "null"):
		p.pos += 4
		return &jsonValue{kind: jsonNull, raw: "null"}, nil
	}
	return nil, p.fail("'%c' is an invalid start of a value", p.s[p.pos])
}

func validJSONNumber(s string) bool {
	i := 0
	if i < len(s) && s[i] == '-' {
		i++
	}
	if i >= len(s) {
		return false
	}
	if s[i] == '0' {
		i++
	} else if s[i] >= '1' && s[i] <= '9' {
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	} else {
		return false
	}
	if i < len(s) && s[i] == '.' {
		i++
		d := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == d {
			return false
		}
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		d := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == d {
			return false
		}
	}
	return i == len(s)
}

func (p *jsonParser) str() (string, error) {
	p.pos++ // opening quote
	var b strings.Builder
	for {
		if p.pos >= len(p.s) {
			return "", p.fail("Expected end of string, but the end of the data was reached")
		}
		c := p.s[p.pos]
		switch {
		case c == '"':
			p.pos++
			return b.String(), nil
		case c == '\\':
			if p.pos+1 >= len(p.s) {
				return "", p.fail("Expected end of string")
			}
			e := p.s[p.pos+1]
			p.pos += 2
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
				r, err := p.hex4()
				if err != nil {
					return "", err
				}
				if utf16.IsSurrogate(r) && p.pos+6 <= len(p.s) && p.s[p.pos] == '\\' && p.s[p.pos+1] == 'u' {
					p.pos += 2
					r2, err := p.hex4()
					if err != nil {
						return "", err
					}
					r = utf16.DecodeRune(r, r2)
				}
				if !utf8.ValidRune(r) {
					r = utf8.RuneError
				}
				b.WriteRune(r)
			default:
				return "", p.fail("'%c' is not a valid escapable character within a JSON string", e)
			}
		case c < 0x20:
			return "", p.fail("'0x%02X' is invalid within a JSON string. The string should be correctly escaped", c)
		default:
			b.WriteByte(c)
			p.pos++
		}
	}
}

func (p *jsonParser) hex4() (rune, error) {
	if p.pos+4 > len(p.s) {
		return 0, errors.New("invalid \\u escape")
	}
	v, err := strconv.ParseUint(p.s[p.pos:p.pos+4], 16, 32)
	if err != nil {
		return 0, p.fail("'%s' is not a valid hex escape", p.s[p.pos:p.pos+4])
	}
	p.pos += 4
	return rune(v), nil
}

// ---- writing ----

// appendJSONString writes s as a JSON string; non-ASCII stays as it is
// (JavaScriptEncoder.UnsafeRelaxedJsonEscaping), control characters are escaped.
func appendJSONString(b []byte, s string) []byte {
	b = append(b, '"')
	for _, r := range s {
		switch {
		case r == '"':
			b = append(b, '\\', '"')
		case r == '\\':
			b = append(b, '\\', '\\')
		case r == '\n':
			b = append(b, '\\', 'n')
		case r == '\r':
			b = append(b, '\\', 'r')
		case r == '\t':
			b = append(b, '\\', 't')
		case r == '\b':
			b = append(b, '\\', 'b')
		case r == '\f':
			b = append(b, '\\', 'f')
		case r < 0x20 || r == 0x7F || r == 0x2028 || r == 0x2029 || r == utf8.RuneError || r >= 0xFFFE && r <= 0xFFFF:
			b = fmt.Appendf(b, "\\u%04X", r)
		default:
			b = utf8.AppendRune(b, r)
		}
	}
	return append(b, '"')
}

// jsonNumber writes a double as System.Text.Json does: the shortest round trip, exponent form for very large or small values.
func jsonFloat(v float64) string {
	a := v
	if a < 0 {
		a = -a
	}
	if a != 0 && (a >= 1e15 || a < 1e-5) {
		return strconv.FormatFloat(v, 'E', -1, 64)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}
