package htmldoc

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
)

// The small part of CSS the HTML reader understands: inline style=""
// declarations, simple style sheet rules (tag, .class, tag.class), colours
// and lengths. Enough for the formatting e-books and saved web pages rely on
// (bold and italic classes, centred text, page breaks, first-line indents)
// without a full CSS engine.

// declarations are property → value pairs, lower-case property names.
type declarations map[string]string

// parseDeclarations parses "color: red; font-weight: bold" (later ones win, !important is ignored).
func parseDeclarations(text string) declarations {
	out := declarations{}
	for _, d := range strings.Split(text, ";") {
		colon := strings.IndexByte(d, ':')
		if colon <= 0 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(d[:colon]))
		value := strings.TrimSpace(replaceFold(d[colon+1:], "!important", ""))
		if name != "" && value != "" {
			out[name] = value
		}
	}
	return out
}

func replaceFold(s, old, repl string) string {
	i := strings.Index(strings.ToLower(s), old)
	if i < 0 {
		return s
	}
	return s[:i] + repl + s[i+len(old):]
}

func (d declarations) get(name string) string { return d[name] }

// sheetProperties are taken from style sheets. Colours and sizes are not:
// whole-book rules (body colour, base font size) would otherwise be copied
// onto every run.
var sheetProperties = map[string]bool{
	"font-weight": true, "font-style": true, "text-decoration": true, "text-decoration-line": true, "vertical-align": true,
	"text-align": true, "display": true, "page-break-before": true, "page-break-after": true, "break-before": true,
	"break-after": true, "white-space": true, "text-indent": true, "list-style": true, "list-style-type": true, "font-variant": true,
}

type cssRule struct {
	tag         string // "" for any element
	classes     []string
	specificity int
	order       int
	decl        declarations
}

// styleSheet holds the rules that can be matched without a selector engine.
type styleSheet struct{ rules []cssRule }

var (
	cssComment     = regexp.MustCompile(`(?s)/\*.*?\*/`)
	simpleSelector = regexp.MustCompile(`^(\*|[A-Za-z][\w-]*)?(\.[\w-]+)*$`)
)

// add adds the rules of a style sheet; @media, @font-face and other at-rules are skipped.
func (s *styleSheet) add(css string) {
	css = cssComment.ReplaceAllString(css, "")
	pos := 0
	for pos < len(css) {
		open := strings.IndexByte(css[pos:], '{')
		if open < 0 {
			break
		}
		open += pos
		selectors := strings.TrimSpace(css[pos:open])
		closeAt := matchingBrace(css, open)
		end := closeAt
		if end < 0 {
			end = len(css)
		}
		body := css[open+1 : max(open+1, end)]
		if closeAt < 0 {
			pos = len(css)
		} else {
			pos = closeAt + 1
		}
		if strings.HasPrefix(selectors, "@") || strings.Contains(body, "{") {
			continue // at-rules (nested blocks): not applied
		}
		decl := parseDeclarations(body)
		for k := range decl {
			if !sheetProperties[k] {
				delete(decl, k)
			}
		}
		if len(decl) == 0 {
			continue
		}
		for _, sel := range strings.Split(selectors, ",") {
			sel = strings.TrimSpace(sel)
			if !simpleSelector.MatchString(sel) || sel == "" {
				continue
			}
			parts := strings.Split(sel, ".")
			tag := strings.ToLower(parts[0])
			if tag == "*" {
				tag = ""
			}
			r := cssRule{tag: tag, classes: parts[1:], order: len(s.rules), decl: decl}
			r.specificity = len(r.classes) * 10
			if tag != "" {
				r.specificity++
			}
			s.rules = append(s.rules, r)
		}
	}
}

func matchingBrace(css string, open int) int {
	depth := 0
	for i := open; i < len(css); i++ {
		switch css[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// match returns the declarations that apply to an element, in cascade order (the last one wins).
func (s *styleSheet) match(tag string, classes []string) declarations {
	out := declarations{}
	if len(s.rules) == 0 {
		return out
	}
	var matching []cssRule
	for _, r := range s.rules {
		if r.tag != "" && r.tag != tag {
			continue
		}
		ok := true
		for _, c := range r.classes {
			found := false
			for _, have := range classes {
				if have == c {
					found = true
					break
				}
			}
			if !found {
				ok = false
				break
			}
		}
		if ok {
			matching = append(matching, r)
		}
	}
	sort.SliceStable(matching, func(i, j int) bool {
		if matching[i].specificity != matching[j].specificity {
			return matching[i].specificity < matching[j].specificity
		}
		return matching[i].order < matching[j].order
	})
	for _, r := range matching {
		for k, v := range r.decl {
			out[k] = v
		}
	}
	return out
}

var namedColors = map[string]string{
	"black": "#000000", "white": "#FFFFFF", "red": "#FF0000", "green": "#008000", "blue": "#0000FF", "yellow": "#FFFF00",
	"gray": "#808080", "grey": "#808080", "silver": "#C0C0C0", "maroon": "#800000", "purple": "#800080", "fuchsia": "#FF00FF",
	"magenta": "#FF00FF", "lime": "#00FF00", "olive": "#808000", "navy": "#000080", "teal": "#008080", "aqua": "#00FFFF",
	"cyan": "#00FFFF", "orange": "#FFA500", "brown": "#A52A2A", "pink": "#FFC0CB", "gold": "#FFD700", "darkred": "#8B0000",
	"darkblue": "#00008B", "darkgreen": "#006400", "darkgray": "#A9A9A9", "darkgrey": "#A9A9A9", "lightgray": "#D3D3D3",
	"lightgrey": "#D3D3D3", "dimgray": "#696969", "dimgrey": "#696969", "crimson": "#DC143C", "indigo": "#4B0082",
	"violet": "#EE82EE", "coral": "#FF7F50", "tomato": "#FF6347", "steelblue": "#4682B4", "royalblue": "#4169E1",
	"lightblue": "#ADD8E6", "lightyellow": "#FFFFE0", "lightgreen": "#90EE90", "whitesmoke": "#F5F5F5", "beige": "#F5F5DC",
	"ivory": "#FFFFF0",
}

func isHex(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// cssColor is "#RRGGBB" for #rgb, #rrggbb, rgb()/rgba() and common colour names; "" otherwise.
func cssColor(value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return ""
	}
	if c, ok := namedColors[strings.ToLower(text)]; ok {
		return c
	}
	if strings.HasPrefix(text, "#") {
		hex := text[1:]
		switch {
		case (len(hex) == 3 || len(hex) == 4) && isHex(hex):
			var sb strings.Builder
			sb.WriteByte('#')
			for _, c := range hex[:3] {
				sb.WriteRune(c)
				sb.WriteRune(c)
			}
			return strings.ToUpper(sb.String())
		case (len(hex) == 6 || len(hex) == 8) && isHex(hex):
			return "#" + strings.ToUpper(hex[:6])
		}
		return ""
	}
	if strings.HasPrefix(strings.ToLower(text), "rgb") {
		open, closeAt := strings.IndexByte(text, '('), strings.IndexByte(text, ')')
		if open <= 0 || closeAt <= open {
			return ""
		}
		parts := strings.FieldsFunc(text[open+1:closeAt], func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
		if len(parts) < 3 {
			return ""
		}
		var ch [3]int
		for i := 0; i < 3; i++ {
			p := parts[i]
			percent := strings.HasSuffix(p, "%")
			n, err := strconv.ParseFloat(strings.TrimSuffix(p, "%"), 64)
			if err != nil {
				return ""
			}
			if percent {
				n *= 2.55
			}
			ch[i] = int(math.Max(0, math.Min(255, math.Round(n))))
		}
		return fmt.Sprintf("#%02X%02X%02X", ch[0], ch[1], ch[2])
	}
	return ""
}

// firstColor is the first colour inside a shorthand such as "background: #eee url(x.png)".
func firstColor(value string) string {
	if c := cssColor(value); c != "" {
		return c
	}
	for _, tok := range strings.Fields(value) {
		if c := cssColor(tok); c != "" {
			return c
		}
	}
	return ""
}

// cssLength is a length in HWPUNIT; em is the size of 1em (and the base of %) in HWPUNIT.
func cssLength(value string, em float64) (int, bool) {
	text := strings.ToLower(strings.TrimSpace(value))
	if text == "" {
		return 0, false
	}
	split := 0
	for split < len(text) && (text[split] >= '0' && text[split] <= '9' || strings.IndexByte(".-+", text[split]) >= 0) {
		split++
	}
	if split == 0 {
		return 0, false
	}
	n, err := strconv.ParseFloat(text[:split], 64)
	if err != nil {
		return 0, false
	}
	var r float64
	switch strings.TrimSpace(text[split:]) {
	case "em", "rem":
		r = n * em
	case "ex", "ch":
		r = n * em / 2
	case "%":
		r = n * em / 100
	case "pt":
		r = n * 100
	case "px", "":
		r = n * doc.PerPixel
	case "in":
		r = n * 7200
	case "cm":
		r = n * doc.PerMm * 10
	case "mm":
		r = n * doc.PerMm
	case "pc":
		r = n * 1200
	default:
		return 0, false
	}
	return int(math.Round(r)), true
}

// cssFontSize is a font size in 1/100 pt; relative sizes are based on parent (1/100 pt).
func cssFontSize(value string, parent int) (int, bool) {
	factors := map[string]float64{
		"xx-small": 0.6, "x-small": 0.75, "small": 0.89, "medium": 1.0, "large": 1.2, "x-large": 1.5,
		"xx-large": 2.0, "xxx-large": 3.0, "smaller": 0.83, "larger": 1.2,
	}
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return 0, false
	}
	if f, ok := factors[v]; ok {
		return int(math.Round(float64(parent) * f)), true
	}
	// HWPUNIT and 1/100 pt are the same scale (1 pt = 100 HWPUNIT), so lengths fit font sizes directly.
	if size, ok := cssLength(v, float64(parent)); ok && size > 0 {
		return max(100, min(20000, size)), true
	}
	return 0, false
}
