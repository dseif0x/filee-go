package cad

import (
	"math"
	"strconv"
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// The control codes inside CAD text: %%d / %%c / %%p specials and \U+ / \M+
// escapes in TEXT, and the MTEXT formatting language (\P paragraphs, \H
// height, \C colour, \f font, \S stacked fractions, {} groups …).

// MTextFormat is the formatting state of an MTEXT run.
type MTextFormat struct {
	Height         float64
	ColorIndex     int // -1 when unset
	TrueColor      int // -1 when unset
	FontFamily     string
	Bold, Italic   bool
	WidthFactor    float64
	ObliqueDegrees float64
}

// MTextRun is a piece of MTEXT with one format.
type MTextRun struct {
	Text   string
	Format MTextFormat
}

// multiByteEncodings are the code pages of \M+n escapes (AutoCAD's numbering).
var multiByteEncodings = []encoding.Encoding{nil, japanese.ShiftJIS, traditionalchinese.Big5, korean.EUCKR, nil, simplifiedchinese.GBK}

// DecodeText decodes a single-line TEXT / ATTRIB value: %%d → °, %%c → Ø,
// %%p → ±, %%nnn → character nnn, underline / overline toggles removed,
// \U+XXXX and \M+nXXXX escapes decoded.
func DecodeText(value string) string {
	if value == "" {
		return ""
	}
	rs := []rune(value)
	var b strings.Builder
	for i := 0; i < len(rs); i++ {
		if tryEscape(rs, &i, &b) || trySpecial(rs, &i, &b) {
			continue
		}
		if rs[i] == '\t' {
			b.WriteRune(' ')
		} else {
			b.WriteRune(rs[i])
		}
	}
	return b.String()
}

// ParseMText parses an MTEXT value into paragraphs of formatted runs.
// Paragraph, line and column breaks start a new paragraph; stacked text
// becomes an inline fraction ("1/2"); codes that only affect layout details
// (tracking, indents, underline) are dropped.
func ParseMText(value string, initial MTextFormat) [][]MTextRun {
	paragraphs := [][]MTextRun{{}}
	if value == "" {
		return paragraphs
	}
	state := initial
	var stack []MTextFormat
	var text strings.Builder
	flush := func() {
		if text.Len() == 0 {
			return
		}
		paragraphs[len(paragraphs)-1] = append(paragraphs[len(paragraphs)-1], MTextRun{text.String(), state})
		text.Reset()
	}
	newParagraph := func() {
		flush()
		paragraphs = append(paragraphs, nil)
	}
	rs := []rune(value)
	for i := 0; i < len(rs); i++ {
		ch := rs[i]
		switch {
		case ch == '{':
			flush()
			stack = append(stack, state)
			continue
		case ch == '}':
			flush()
			if len(stack) > 0 {
				state = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
			continue
		case ch == '\n' || ch == '\r':
			if ch == '\r' && i+1 < len(rs) && rs[i+1] == '\n' {
				i++
			}
			newParagraph()
			continue
		}
		if ch != '\\' || i+1 >= len(rs) {
			if !trySpecial(rs, &i, &text) {
				if ch == '\t' {
					text.WriteString("    ")
				} else {
					text.WriteRune(ch)
				}
			}
			continue
		}
		code := rs[i+1]
		switch code {
		case '\\', '{', '}':
			text.WriteRune(code)
			i++
		case 'P', 'N', 'X':
			i++
			newParagraph()
		case '~':
			text.WriteRune(' ')
			i++
		case 'U', 'M':
			if !tryEscape(rs, &i, &text) {
				i++
			}
		case 'H', 'W', 'Q':
			arg := readArgument(rs, &i)
			flush()
			state = applyNumeric(state, code, arg)
		case 'C', 'c':
			arg := readArgument(rs, &i)
			flush()
			if n, err := strconv.Atoi(strings.TrimSpace(arg)); err == nil {
				if code == 'C' {
					state.ColorIndex, state.TrueColor = max(0, min(256, n)), -1
				} else {
					state.TrueColor, state.ColorIndex = n&0xFFFFFF, -1
				}
			}
		case 'f', 'F':
			arg := readArgument(rs, &i)
			flush()
			state = applyFont(state, arg)
		case 'S':
			arg := readArgument(rs, &i)
			text.WriteString(stacked(arg))
		case 'A', 'T', 'p', 'q':
			readArgument(rs, &i)
		default:
			// \L \l \O \o \K \k and unknown codes: drop the code.
			i++
		}
	}
	flush()
	return paragraphs
}

// readArgument reads the argument of a code like \H2.5;, leaving i on the ';'.
func readArgument(rs []rune, i *int) string {
	start := *i + 2
	end := start
	for end < len(rs) && rs[end] != ';' {
		if rs[end] == '\\' && end+1 < len(rs) {
			end++
		}
		end++
	}
	*i = min(end, len(rs)-1)
	if start < len(rs) {
		return string(rs[start:min(end, len(rs))])
	}
	return ""
}

func applyNumeric(state MTextFormat, code rune, arg string) MTextFormat {
	relative := strings.HasSuffix(arg, "x") || strings.HasSuffix(arg, "X")
	if relative {
		arg = arg[:len(arg)-1]
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(arg), 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return state
	}
	switch code {
	case 'H':
		if n > 0 {
			if relative {
				state.Height *= n
			} else {
				state.Height = n
			}
		}
	case 'W':
		if n > 0 {
			if relative {
				state.WidthFactor *= n
			} else {
				state.WidthFactor = n
			}
		}
	case 'Q':
		state.ObliqueDegrees = math.Max(-85, math.Min(85, n))
	}
	return state
}

// applyFont reads \fArial|b1|i0|c0|p34; (TrueType) or \Ftxt.shx; (SHX: the default font).
func applyFont(state MTextFormat, arg string) MTextFormat {
	parts := strings.Split(arg, "|")
	family := strings.TrimSpace(parts[0])
	bold, italic := false, false
	for _, p := range parts[1:] {
		if len(p) >= 2 && p[0] == 'b' {
			bold = p[1] == '1'
		} else if len(p) >= 2 && p[0] == 'i' {
			italic = p[1] == '1'
		}
	}
	if strings.HasSuffix(strings.ToLower(family), ".shx") {
		family = ""
	}
	state.FontFamily, state.Bold, state.Italic = family, bold, italic
	return state
}

// stacked shows 1/2, 1#2 (diagonal) or +0.1^-0.2 (tolerance) inline as "1/2".
func stacked(arg string) string {
	un := strings.ReplaceAll(arg, `\;`, ";")
	split := strings.IndexAny(un, "/#^")
	if split < 0 {
		return DecodeText(un)
	}
	top := strings.TrimSpace(DecodeText(un[:split]))
	bottom := strings.TrimSpace(DecodeText(un[split+1:]))
	switch {
	case top == "":
		return bottom
	case bottom == "":
		return top
	}
	return top + "/" + bottom
}

func tryEscape(rs []rune, i *int, b *strings.Builder) bool {
	if rs[*i] != '\\' || *i+2 >= len(rs) || rs[*i+2] != '+' {
		return false
	}
	c := rs[*i+1]
	if (c == 'U' || c == 'u') && *i+7 <= len(rs) {
		if code, err := strconv.ParseUint(string(rs[*i+3:*i+7]), 16, 32); err == nil {
			b.WriteRune(rune(code))
			*i += 6
			return true
		}
	}
	if (c == 'M' || c == 'm') && *i+8 <= len(rs) && rs[*i+3] >= '1' && rs[*i+3] <= '5' {
		if code, err := strconv.ParseUint(string(rs[*i+4:*i+8]), 16, 32); err == nil {
			if enc := multiByteEncodings[rs[*i+3]-'0']; enc != nil {
				if s, err := enc.NewDecoder().Bytes([]byte{byte(code >> 8), byte(code)}); err == nil {
					b.Write(s)
					*i += 7
					return true
				}
			}
		}
	}
	return false
}

// trySpecial decodes the %% specials of AutoCAD text.
func trySpecial(rs []rune, i *int, b *strings.Builder) bool {
	if rs[*i] != '%' || *i+2 >= len(rs) || rs[*i+1] != '%' {
		return false
	}
	code := rs[*i+2]
	if code >= 'A' && code <= 'Z' {
		code += 'a' - 'A'
	}
	switch {
	case code == 'd':
		b.WriteRune('°')
	case code == 'c':
		b.WriteRune('Ø')
	case code == 'p':
		b.WriteRune('±')
	case code == '%':
		b.WriteRune('%')
	case code == 'u' || code == 'o' || code == 'k':
	case code >= '0' && code <= '9':
		end := *i + 2
		for end < len(rs) && end < *i+5 && rs[end] >= '0' && rs[end] <= '9' {
			end++
		}
		n, _ := strconv.Atoi(string(rs[*i+2 : end]))
		b.WriteRune(rune(n))
		*i = end - 1
		return true
	default:
		return false
	}
	*i += 2
	return true
}
