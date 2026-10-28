package htmldoc

import (
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
)

// ListNumbers counts list items and formats their numbers like 한글 does ("1.", "가.", "iv)", "●").
type ListNumbers struct{ counters map[*doc.Numbering][]int }

func NewListNumbers() *ListNumbers { return &ListNumbers{counters: map[*doc.Numbering][]int{}} }

// Next is the marker of the next numbered item of a list.
func (n *ListNumbers) Next(item *doc.ListRef) string {
	if item.Numbering == nil || len(item.Numbering.Levels) == 0 {
		return "•"
	}
	levels := item.Numbering.Levels
	level := min(item.Level, len(levels)-1)
	def := levels[level]
	if def.Bullet {
		if def.Text != "" {
			return def.Text
		}
		return "•"
	}
	counters := n.counters[item.Numbering]
	if counters == nil {
		counters = make([]int, max(10, len(levels)))
		n.counters[item.Numbering] = counters
	}
	if counters[level] == 0 {
		counters[level] = def.Start
	} else {
		counters[level]++
	}
	for deeper := level + 1; deeper < len(counters); deeper++ {
		counters[deeper] = 0
	}
	text := def.Text
	for l := min(9, len(levels)); l >= 1; l-- {
		v := counters[l-1]
		if v == 0 {
			v = levels[l-1].Start
		}
		text = strings.ReplaceAll(text, "^"+strconv.Itoa(l), FormatNumber(v, levels[l-1].Format))
	}
	return text
}

// FormatNumber writes a number in an OWPML number format.
func FormatNumber(v int, format string) string {
	switch format {
	case "LATIN_SMALL":
		return letters(v, 'a')
	case "LATIN_CAPITAL":
		return letters(v, 'A')
	case "ROMAN_SMALL":
		return strings.ToLower(roman(v))
	case "ROMAN_CAPITAL":
		return roman(v)
	case "HANGUL_SYLLABLE":
		if v >= 1 && v <= 14 {
			return string([]rune("가나다라마바사아자차카타파하")[v-1])
		}
	case "CIRCLED_DIGIT":
		if v >= 1 && v <= 20 {
			return string(rune('①' + v - 1))
		}
	}
	return strconv.Itoa(v)
}

func letters(v int, first rune) string {
	if v < 1 {
		return strconv.Itoa(v)
	}
	var out []rune
	for ; v > 0; v = (v - 1) / 26 {
		out = append([]rune{first + rune((v-1)%26)}, out...)
	}
	return string(out)
}

func roman(v int) string {
	if v < 1 || v > 3999 {
		return strconv.Itoa(v)
	}
	numerals := []struct {
		n int
		s string
	}{{1000, "M"}, {900, "CM"}, {500, "D"}, {400, "CD"}, {100, "C"}, {90, "XC"}, {50, "L"}, {40, "XL"}, {10, "X"}, {9, "IX"}, {5, "V"}, {4, "IV"}, {1, "I"}}
	var sb strings.Builder
	for _, x := range numerals {
		for ; v >= x.n; v -= x.n {
			sb.WriteString(x.s)
		}
	}
	return sb.String()
}

// Text is the visible text of inlines (links included, notes left out), line breaks as spaces.
func Text(inlines []doc.Inline) string { return doc.PlainText(inlines) }

func isLine(in doc.Inline) bool {
	s, ok := in.(*doc.Shape)
	return ok && s.Kind == doc.ShapeLine
}

func hasLine(inlines []doc.Inline) bool {
	for _, in := range inlines {
		if isLine(in) {
			return true
		}
	}
	return false
}

// isCodeParagraph: a paragraph that is all code (shaded like the readers mark code blocks).
func isCodeParagraph(p *doc.Paragraph, plainOnly bool) bool {
	if plainOnly && (p.List != nil || p.HeadingLevel != 0) {
		return false
	}
	if len(p.Inlines) == 0 {
		return false
	}
	for _, in := range p.Inlines {
		t, ok := in.(*doc.Text)
		if !ok || doc.Val(t.Format.Shade, "") != CodeShade {
			return false
		}
	}
	return true
}

func codeText(p *doc.Paragraph) string {
	var sb strings.Builder
	for _, in := range p.Inlines {
		if t, ok := in.(*doc.Text); ok {
			sb.WriteString(t.Text)
		}
	}
	return sb.String()
}
