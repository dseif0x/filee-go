package hwpx

import (
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
)

// Symbol fonts (Wingdings, Wingdings 2, Wingdings 3, Webdings, Symbol) draw their own pictures in place of letters:
// a check box picked in Word's Insert → Symbol is stored as a code of the font (w:sym w:font="Wingdings 2"
// w:char="F052"), in the private use area F020–F0FF, and text typed in such a font as the plain byte ("R"). Another
// font shows nothing or a wrong sign for those codes, so the readers write the Unicode character the picture stands
// for (#350). Codes the tables do not know keep the readers' old handling.
//
// Every character here is in the Basic Multilingual Plane and in Segoe UI Symbol, which every Windows has: rhwp
// draws a character its font lacks with the first installed font that has it, so these always come out.

type symbolRow struct {
	first int
	chars string
}

func symbolTable(rows ...symbolRow) map[int]string {
	table := map[int]string{}
	for _, row := range rows {
		i := 0
		for _, r := range row.chars {
			if r != ' ' {
				table[row.first+i] = string(r)
			}
			i++
		}
	}
	return table
}

func symbolTableOf(font string) map[int]string {
	if font == "" {
		return nil
	}
	return symbolTables[strings.ToLower(strings.ReplaceAll(font, " ", ""))]
}

// IsSymbolFont tells Wingdings, Wingdings 2, Wingdings 3, Webdings or Symbol (any case, with or without spaces).
func IsSymbolFont(font string) bool { return symbolTableOf(font) != nil }

// SymbolCode is the Unicode character code of a symbol font stands for (F020–F0FF or 20–FF), or "" for another
// font or a code the tables do not know.
func SymbolCode(font string, code int) string {
	table := symbolTableOf(font)
	if table == nil {
		return ""
	}
	if code >= 0xF000 && code <= 0xF0FF {
		code -= 0xF000
	}
	return table[code]
}

// SymbolText is a whole text of a symbol font in Unicode, or "" unless every character is known.
func SymbolText(font, text string) string {
	if text == "" || symbolTableOf(font) == nil {
		return ""
	}
	var sb strings.Builder
	for _, c := range text {
		m := SymbolCode(font, int(c))
		if m == "" {
			return ""
		}
		sb.WriteString(m)
	}
	return sb.String()
}

// WithoutSymbolFont is the format without symbol fonts: the Unicode characters are drawn with the text font.
func WithoutSymbolFont(f doc.CharFormat) doc.CharFormat {
	eastAsian := f.EastAsianFont
	if eastAsian != nil && IsSymbolFont(*eastAsian) {
		eastAsian = nil
	}
	if f.Font != nil && IsSymbolFont(*f.Font) {
		f.Font = eastAsian
	}
	f.EastAsianFont = eastAsian
	return f
}

// SymbolRuns is text in format: in a symbol font, the characters the tables know become their Unicode characters
// without the symbol font, and the others stay as they are.
func SymbolRuns(text string, format doc.CharFormat) []doc.Inline {
	if format.Font == nil || !IsSymbolFont(*format.Font) {
		return []doc.Inline{&doc.Text{Text: text, Format: format}}
	}
	plain := WithoutSymbolFont(format)
	var out []doc.Inline
	var run strings.Builder
	mappedRun := false
	flush := func() {
		f := format
		if mappedRun {
			f = plain
		}
		out = append(out, &doc.Text{Text: run.String(), Format: f})
		run.Reset()
	}
	for _, c := range text {
		mapped := SymbolCode(*format.Font, int(c))
		if run.Len() > 0 && mappedRun != (mapped != "") {
			flush()
		}
		mappedRun = mapped != ""
		if mapped != "" {
			run.WriteString(mapped)
		} else {
			run.WriteRune(c)
		}
	}
	if run.Len() > 0 {
		flush()
	}
	return out
}
