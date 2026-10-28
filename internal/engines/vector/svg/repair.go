package svg

import (
	"html"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Repairs the one mistake that makes many hand-written and AI-generated SVGs
// unreadable: an "&" that isn't an entity ("A & B" in a <text>, a URL query in
// an href) and HTML entities XML doesn't know (&nbsp;, &copy;).

var (
	ampersand     = regexp.MustCompile(`&(?:(#[0-9]+;|#[xX][0-9A-Fa-f]+;)|([A-Za-z_][A-Za-z0-9._-]*);)?`)
	untouched     = regexp.MustCompile(`(?s)<!--.*?-->|<!\[CDATA\[.*?\]\]>|<!DOCTYPE[^\[>]*(?:\[.*?\])?\s*>`)
	declaredRegex = regexp.MustCompile(`<!ENTITY\s+([A-Za-z_][A-Za-z0-9._-]*)`)
	xmlEntities   = map[string]bool{"amp": true, "lt": true, "gt": true, "quot": true, "apos": true}
)

// FixAmpersands returns svg with every "&" that doesn't start a character
// reference or a known entity escaped, and HTML named entities turned into
// character references. Entities declared in the DTD, the DOCTYPE, comments
// and CDATA sections are left alone. nil when nothing needed repair or the
// bytes aren't UTF-8.
func FixAmpersands(svg []byte) []byte {
	if !strings.Contains(string(svg), "&") || !utf8.Valid(svg) {
		return nil
	}
	text := string(svg)
	declared := map[string]bool{}
	for _, m := range declaredRegex.FindAllStringSubmatch(text, -1) {
		declared[m[1]] = true
	}
	var b strings.Builder
	changed := false
	pos := 0
	for _, loc := range untouched.FindAllStringIndex(text, -1) {
		changed = repairPart(text[pos:loc[0]], declared, &b) || changed
		b.WriteString(text[loc[0]:loc[1]])
		pos = loc[1]
	}
	changed = repairPart(text[pos:], declared, &b) || changed
	if !changed {
		return nil
	}
	return []byte(b.String())
}

func repairPart(part string, declared map[string]bool, b *strings.Builder) bool {
	changed := false
	last := 0
	for _, m := range ampersand.FindAllStringSubmatchIndex(part, -1) {
		b.WriteString(part[last:m[0]])
		last = m[1]
		whole := part[m[0]:m[1]]
		number := m[2] >= 0
		name := ""
		if m[4] >= 0 {
			name = part[m[4]:m[5]]
		}
		if number || name != "" && (xmlEntities[name] || declared[name]) {
			b.WriteString(whole)
			continue
		}
		changed = true
		if name != "" {
			if decoded := html.UnescapeString(whole); decoded != whole {
				for _, r := range decoded {
					b.WriteString("&#" + strconv.Itoa(int(r)) + ";")
				}
				continue
			}
		}
		b.WriteString("&amp;" + whole[1:])
	}
	b.WriteString(part[last:])
	return changed
}
