// Package hwpx reads and writes HWPX (한글 / OWPML, KS X 6101) through the document model of internal/doc, and
// holds the readers whose C# originals live with it: DOCX (with Word math), PPTX, HTML and Pandoc's JSON AST. It
// also writes the model as XHTML, Markdown and plain text, and is the "hwpx-writer" engine.
package hwpx

import (
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/dseif0x/filee-go/internal/doc"
)

// Shades the readers mark code and highlighted (==marked==) text with.
const (
	CodeShade = "#F1F0F5"
	MarkShade = "#FFF3A3"
)

func ptr[T any](v T) *T { return &v }

func val[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

func isTrue(p *bool) bool  { return p != nil && *p }
func isFalse(p *bool) bool { return p != nil && !*p }

func strIs(p *string, v string) bool { return p != nil && *p == v }

// iround is C#'s Math.Round (to even) as an int.
func iround(v float64) int { return int(math.RoundToEven(v)) }

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func itoa(v int) string { return strconv.Itoa(v) }

// charKey is a comparable form of a character format, for caches.
type charKey struct {
	set                                          uint32
	bold, italic, underline, strike, sup, sub    bool
	size, spacing                                int
	font, eastAsian, color, shade                string
}

func keyOfChar(f doc.CharFormat) charKey {
	var k charKey
	b := func(bit uint32, p *bool, dst *bool) {
		if p != nil {
			k.set |= bit
			*dst = *p
		}
	}
	b(1, f.Bold, &k.bold)
	b(2, f.Italic, &k.italic)
	b(4, f.Underline, &k.underline)
	b(8, f.Strike, &k.strike)
	b(16, f.Superscript, &k.sup)
	b(32, f.Subscript, &k.sub)
	if f.Size != nil {
		k.set |= 64
		k.size = *f.Size
	}
	if f.Spacing != nil {
		k.set |= 128
		k.spacing = *f.Spacing
	}
	s := func(bit uint32, p *string, dst *string) {
		if p != nil {
			k.set |= bit
			*dst = *p
		}
	}
	s(256, f.Font, &k.font)
	s(512, f.EastAsianFont, &k.eastAsian)
	s(1024, f.Color, &k.color)
	s(2048, f.Shade, &k.shade)
	return k
}

// paraKey is a comparable form of a paragraph format (tab stops by content).
type paraKey struct {
	set                                         uint32
	align                                       doc.Align
	left, right, firstLine, before, after       int
	line                                        doc.LineSpacing
	keepWithNext, keepLines, widowOrphan, rtl   bool
	tabs                                        string
}

func keyOfPara(f doc.ParaFormat) paraKey {
	var k paraKey
	i := func(bit uint32, p *int, dst *int) {
		if p != nil {
			k.set |= bit
			*dst = *p
		}
	}
	b := func(bit uint32, p *bool, dst *bool) {
		if p != nil {
			k.set |= bit
			*dst = *p
		}
	}
	if f.Align != nil {
		k.set |= 1
		k.align = *f.Align
	}
	i(2, f.Left, &k.left)
	i(4, f.Right, &k.right)
	i(8, f.FirstLine, &k.firstLine)
	i(16, f.Before, &k.before)
	i(32, f.After, &k.after)
	if f.LineSpacing != nil {
		k.set |= 64
		k.line = *f.LineSpacing
	}
	b(128, f.KeepWithNext, &k.keepWithNext)
	b(256, f.KeepLines, &k.keepLines)
	b(512, f.WidowOrphan, &k.widowOrphan)
	b(1024, f.RightToLeft, &k.rtl)
	if f.Tabs != nil {
		k.set |= 2048
		k.tabs = tabKey(f.Tabs)
	}
	return k
}

func paraEmpty(f doc.ParaFormat) bool { return keyOfPara(f) == paraKey{} }

func tabKey(tabs []doc.TabStop) string {
	var sb strings.Builder
	for i, t := range tabs {
		if i > 0 {
			sb.WriteByte(';')
		}
		sb.WriteString(itoa(t.Position))
		sb.WriteByte(',')
		sb.WriteString(itoa(int(t.Kind)))
		sb.WriteByte(',')
		sb.WriteString(t.Leader)
	}
	return sb.String()
}

// textOf is the text of a *doc.Text or *doc.Equation (its linear form), ok false for other inlines.
func textOf(in doc.Inline) (string, doc.CharFormat, bool) {
	switch v := in.(type) {
	case *doc.Text:
		return v.Text, v.Format, true
	case *doc.Equation:
		return v.Text.Text, v.Text.Format, true
	}
	return "", doc.CharFormat{}, false
}

// NewEquation is an equation inline: the tree, and its linear form as text.
func NewEquation(math MathNode, format doc.CharFormat) *doc.Equation {
	return &doc.Equation{Text: doc.Text{Text: Linear(math), Format: format}, Math: math}
}

// trimSpaceLeft / Right are C#'s TrimStart / TrimEnd of whitespace.
func trimLeftSpace(s string) string  { return strings.TrimLeftFunc(s, unicode.IsSpace) }
func trimRightSpace(s string) string { return strings.TrimRightFunc(s, unicode.IsSpace) }

func isBlank(s string) bool { return strings.TrimSpace(s) == "" }
