package hwpx

// The small part of CSS the HTML reader understands: inline style="" declarations, simple stylesheet rules (tag,
// .class, tag.class), colours and lengths. Enough for the formatting e-books and saved web pages rely on (bold and
// italic classes, centred text, page breaks, first-line indents) without a full CSS engine.

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
)

// CSSDeclarations are property → value pairs of one declaration block, lower-case property names.
type CSSDeclarations map[string]string

// ParseCSSDeclarations parses "color: red; font-weight: bold" (later declarations win, !important is ignored).
func ParseCSSDeclarations(text string) CSSDeclarations {
	result := CSSDeclarations{}
	for _, declaration := range strings.Split(text, ";") {
		colon := strings.Index(declaration, ":")
		if colon <= 0 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(declaration[:colon]))
		value := strings.TrimSpace(replaceFold(declaration[colon+1:], "!important", ""))
		if name != "" && value != "" {
			result[name] = value
		}
	}
	return result
}

func replaceFold(s, old, replacement string) string {
	lower := strings.ToLower(s)
	i := strings.Index(lower, old)
	for i >= 0 {
		s = s[:i] + replacement + s[i+len(old):]
		lower = strings.ToLower(s)
		i = strings.Index(lower, old)
	}
	return s
}

// Get is the value of a property ("" when unset).
func (d CSSDeclarations) Get(name string) string { return d[name] }

// sheetProperties are the properties taken from style sheets. Colours and sizes are not: whole-book rules (body
// colour, base font size) would otherwise be copied onto every run.
var sheetProperties = map[string]bool{
	"font-weight": true, "font-style": true, "text-decoration": true, "text-decoration-line": true, "vertical-align": true, "text-align": true,
	"display": true, "page-break-before": true, "page-break-after": true, "break-before": true, "break-after": true, "white-space": true,
	"text-indent": true, "list-style": true, "list-style-type": true, "font-variant": true,
}

type cssRule struct {
	tag         string // "" = any element
	classes     []string
	specificity int
	order       int
	decls       CSSDeclarations
}

// CSSStyleSheet holds the rules of the document's style sheets that the reader can match without a selector engine.
type CSSStyleSheet struct{ rules []cssRule }

func (s *CSSStyleSheet) empty() bool { return len(s.rules) == 0 }

var (
	cssComment        = regexp.MustCompile(`(?s)/\*.*?\*/`)
	cssSimpleSelector = regexp.MustCompile(`^(\*|[A-Za-z][\w-]*)?(\.[\w-]+)*$`)
)

// Add adds the rules of a style sheet. @media, @font-face and other at-rules are skipped.
func (s *CSSStyleSheet) Add(css string) {
	css = cssComment.ReplaceAllString(css, "")
	position := 0
	for position < len(css) {
		open := strings.Index(css[position:], "{")
		if open < 0 {
			break
		}
		open += position
		selectors := strings.TrimSpace(css[position:open])
		closing := matchingBrace(css, open)
		end := closing
		if end < open+1 {
			end = open + 1
		}
		body := css[open+1 : end]
		if closing < 0 {
			position = len(css)
		} else {
			position = closing + 1
		}
		if strings.HasPrefix(selectors, "@") || strings.Contains(body, "{") {
			continue // at-rules (nested blocks): not applied
		}
		declarations := ParseCSSDeclarations(body)
		for k := range declarations {
			if !sheetProperties[k] {
				delete(declarations, k)
			}
		}
		if len(declarations) == 0 {
			continue
		}
		for _, selector := range strings.Split(selectors, ",") {
			if rule, ok := parseSelector(strings.TrimSpace(selector)); ok {
				rule.order = len(s.rules)
				rule.decls = declarations
				s.rules = append(s.rules, rule)
			}
		}
	}
}

// Match is the declarations that apply to an element, in cascade order (the last one wins).
func (s *CSSStyleSheet) Match(tag string, classes []string) CSSDeclarations {
	result := CSSDeclarations{}
	if len(s.rules) == 0 {
		return result
	}
	has := map[string]bool{}
	for _, c := range classes {
		has[c] = true
	}
	var matching []cssRule
	for _, r := range s.rules {
		if r.tag != "" && r.tag != tag {
			continue
		}
		all := true
		for _, c := range r.classes {
			if !has[c] {
				all = false
				break
			}
		}
		if all {
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
		for k, v := range r.decls {
			result[k] = v
		}
	}
	return result
}

// parseSelector reads "p", ".note", "span.bold", "p.a.b"; anything else (descendants, ids, pseudo classes) is skipped.
func parseSelector(selector string) (cssRule, bool) {
	if !cssSimpleSelector.MatchString(selector) {
		return cssRule{}, false
	}
	parts := strings.Split(selector, ".")
	tag := ""
	if parts[0] != "" && parts[0] != "*" {
		tag = strings.ToLower(parts[0])
	}
	classes := parts[1:]
	specificity := len(classes) * 10
	if tag != "" {
		specificity++
	}
	return cssRule{tag: tag, classes: classes, specificity: specificity}, true
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

// CSSColor is "#RRGGBB" for #rgb, #rrggbb, rgb()/rgba() and common colour names; "" for anything else.
func CSSColor(value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return ""
	}
	if c, ok := namedColors[strings.ToLower(text)]; ok {
		return c
	}
	if strings.HasPrefix(text, "#") {
		hex := text[1:]
		if (len(hex) == 3 || len(hex) == 4) && isHex(hex) {
			var sb strings.Builder
			sb.WriteByte('#')
			for _, c := range hex[:3] {
				sb.WriteRune(c)
				sb.WriteRune(c)
			}
			return strings.ToUpper(sb.String())
		}
		if (len(hex) == 6 || len(hex) == 8) && isHex(hex) {
			return "#" + strings.ToUpper(hex[:6])
		}
		return ""
	}
	open, closing := strings.Index(text, "("), strings.Index(text, ")")
	if len(text) >= 3 && strings.EqualFold(text[:3], "rgb") && open > 0 && closing > open {
		parts := strings.FieldsFunc(text[open+1:closing], func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
		if len(parts) < 3 {
			return ""
		}
		var channels [3]int
		for i := 0; i < 3; i++ {
			part := parts[i]
			percent := strings.HasSuffix(part, "%")
			number, err := strconv.ParseFloat(strings.TrimSuffix(part, "%"), 64)
			if err != nil {
				return ""
			}
			if percent {
				number *= 2.55
			}
			channels[i] = int(math.Max(0, math.Min(255, math.RoundToEven(number))))
		}
		return fmt.Sprintf("#%02X%02X%02X", channels[0], channels[1], channels[2])
	}
	return ""
}

// CSSFirstColor is the first colour inside a shorthand such as "background: #eee url(x.png)".
func CSSFirstColor(value string) string {
	if c := CSSColor(value); c != "" {
		return c
	}
	for _, token := range strings.Fields(value) {
		if c := CSSColor(token); c != "" {
			return c
		}
	}
	return ""
}

// CSSLength is a length in HWPUNIT; em is the size of 1em (and the base of %) in HWPUNIT. ok is false when the value
// is not a length (auto, inherit, ...).
func CSSLength(value string, em float64) (int, bool) {
	text := strings.ToLower(strings.TrimSpace(value))
	if text == "" {
		return 0, false
	}
	split := 0
	for split < len(text) && (text[split] >= '0' && text[split] <= '9' || text[split] == '.' || text[split] == '-' || text[split] == '+') {
		split++
	}
	if split == 0 {
		return 0, false
	}
	number, err := strconv.ParseFloat(text[:split], 64)
	if err != nil {
		return 0, false
	}
	var result float64
	switch strings.TrimSpace(text[split:]) {
	case "em", "rem":
		result = number * em
	case "ex", "ch":
		result = number * em / 2
	case "%":
		result = number * em / 100
	case "pt":
		result = number * 100
	case "px", "":
		result = number * doc.PerPixel
	case "in":
		result = number * 7200
	case "cm":
		result = number * doc.PerMm * 10
	case "mm":
		result = number * doc.PerMm
	case "pc":
		result = number * 1200
	default:
		return 0, false
	}
	return iround(result), true
}

// CSSFontSize is a font size in 1/100 pt, relative sizes based on parent (1/100 pt).
func CSSFontSize(value string, parent int) (int, bool) {
	text := strings.ToLower(strings.TrimSpace(value))
	if text == "" {
		return 0, false
	}
	factors := map[string]float64{"xx-small": 0.6, "x-small": 0.75, "small": 0.89, "medium": 1.0, "large": 1.2, "x-large": 1.5,
		"xx-large": 2.0, "xxx-large": 3.0, "smaller": 0.83, "larger": 1.2}
	if f, ok := factors[text]; ok {
		return iround(float64(parent) * f), true
	}
	// HWPUNIT and 1/100 pt are the same scale (1 pt = 100 HWPUNIT), so CSSLength fits font sizes directly.
	if size, ok := CSSLength(value, float64(parent)); ok && size > 0 {
		return clamp(size, 100, 20000), true
	}
	return 0, false
}
