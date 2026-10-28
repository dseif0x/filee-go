package svg

import (
	"regexp"
	"sort"
	"strings"
)

type declaration struct {
	prop, value string
	important   bool
}

type cssRule struct {
	selector    *selector
	spec        int
	order       int
	decls       []declaration
}

// selector is a chain of compound selectors joined by combinators, last first.
type selector struct {
	parts []compound
	// combinators[i] joins parts[i] to parts[i+1] (which is to its left):
	// ' ' descendant, '>' child, '+' adjacent, '~' sibling.
	combinators []byte
}

type compound struct {
	tag     string // "" or "*" for any
	id      string
	classes []string
	attrs   []attrCond
	pseudo  []string
	not     []compound
}

type attrCond struct {
	name, op, value string
}

var cssComment = regexp.MustCompile(`(?s)/\*.*?\*/`)

// parseCSS returns the rules of a style sheet.
func parseCSS(text string, order *int) []cssRule {
	text = cssComment.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "<!--", "")
	text = strings.ReplaceAll(text, "-->", "")
	var rules []cssRule
	for len(text) > 0 {
		text = strings.TrimSpace(text)
		if text == "" {
			break
		}
		if strings.HasPrefix(text, "@") {
			// At-rules: blocks of @media screen/all are read, others skipped.
			brace := strings.IndexAny(text, "{;")
			if brace < 0 {
				break
			}
			if text[brace] == ';' {
				text = text[brace+1:]
				continue
			}
			head := strings.ToLower(strings.TrimSpace(text[:brace]))
			end := matchBrace(text, brace)
			body := text[brace+1 : end]
			if strings.HasPrefix(head, "@media") && !strings.Contains(head, "print") {
				rules = append(rules, parseCSS(body, order)...)
			}
			if end+1 > len(text) {
				break
			}
			text = text[end+1:]
			continue
		}
		brace := strings.IndexByte(text, '{')
		if brace < 0 {
			break
		}
		end := strings.IndexByte(text[brace:], '}')
		if end < 0 {
			end = len(text) - brace
		}
		sels := text[:brace]
		decls := parseDeclarations(text[brace+1 : brace+end])
		for _, s := range strings.Split(sels, ",") {
			sel, spec, ok := parseSelector(strings.TrimSpace(s))
			if !ok {
				continue
			}
			*order++
			rules = append(rules, cssRule{selector: sel, spec: spec, order: *order, decls: decls})
		}
		if brace+end+1 > len(text) {
			break
		}
		text = text[brace+end+1:]
	}
	return rules
}

func matchBrace(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(s) - 1
}

// parseDeclarations reads "a: b; c: d !important".
func parseDeclarations(s string) []declaration {
	var out []declaration
	for _, part := range splitOutsideParens(s, ';') {
		colon := strings.IndexByte(part, ':')
		if colon < 0 {
			continue
		}
		prop := strings.ToLower(strings.TrimSpace(part[:colon]))
		value := strings.TrimSpace(part[colon+1:])
		important := false
		if i := strings.Index(strings.ToLower(value), "!important"); i >= 0 {
			important = true
			value = strings.TrimSpace(value[:i])
		}
		if prop != "" {
			out = append(out, declaration{prop, value, important})
		}
	}
	return out
}

// splitOutsideParens splits at sep where it is not inside () or quotes (data URLs hold ';').
func splitOutsideParens(s string, sep byte) []string {
	var out []string
	depth := 0
	var quote byte
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			if depth > 0 {
				depth--
			}
		case c == sep && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

var selectorToken = regexp.MustCompile(`^(\*|[A-Za-z_][\w-]*|#[\w-]+|\.[\w-]+|\[[^\]]*\]|::?[\w-]+(?:\([^)]*\))?)`)

func parseSelector(s string) (*selector, int, bool) {
	if s == "" {
		return nil, 0, false
	}
	sel := &selector{}
	spec := 0
	var comps []compound
	var combs []byte
	cur := compound{}
	have := false
	pendingComb := byte(0)
	for len(s) > 0 {
		c := s[0]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '>' || c == '+' || c == '~' {
			comb := byte(' ')
			for len(s) > 0 && strings.IndexByte(" \t\r\n>+~", s[0]) >= 0 {
				if s[0] != ' ' && s[0] != '\t' && s[0] != '\n' && s[0] != '\r' {
					comb = s[0]
				}
				s = s[1:]
			}
			if have {
				comps = append(comps, cur)
				cur = compound{}
				have = false
				pendingComb = comb
			}
			continue
		}
		m := selectorToken.FindString(s)
		if m == "" {
			return nil, 0, false
		}
		s = s[len(m):]
		if have == false && len(comps) > 0 {
			combs = append(combs, pendingComb)
		}
		have = true
		switch {
		case m == "*":
			cur.tag = "*"
		case m[0] == '#':
			cur.id = m[1:]
			spec += 10000
		case m[0] == '.':
			cur.classes = append(cur.classes, m[1:])
			spec += 100
		case m[0] == '[':
			cur.attrs = append(cur.attrs, parseAttrCond(m[1:len(m)-1]))
			spec += 100
		case m[0] == ':':
			p := strings.ToLower(strings.TrimLeft(m, ":"))
			if strings.HasPrefix(p, "not(") {
				inner, _, ok := parseSelector(strings.TrimSuffix(p[4:], ")"))
				if ok && len(inner.parts) == 1 {
					cur.not = append(cur.not, inner.parts[0])
				}
			} else {
				cur.pseudo = append(cur.pseudo, p)
			}
			spec += 100
		default:
			cur.tag = m
			spec++
		}
	}
	if have {
		comps = append(comps, cur)
	}
	if len(comps) == 0 {
		return nil, 0, false
	}
	// Store right to left.
	for i := len(comps) - 1; i >= 0; i-- {
		sel.parts = append(sel.parts, comps[i])
	}
	for i := len(combs) - 1; i >= 0; i-- {
		sel.combinators = append(sel.combinators, combs[i])
	}
	return sel, spec, true
}

func parseAttrCond(s string) attrCond {
	for _, op := range []string{"~=", "|=", "^=", "$=", "*=", "="} {
		if i := strings.Index(s, op); i >= 0 {
			v := strings.Trim(strings.TrimSpace(s[i+len(op):]), `"'`)
			return attrCond{strings.TrimSpace(s[:i]), op, v}
		}
	}
	return attrCond{name: strings.TrimSpace(s)}
}

func (c *compound) matches(n *Node) bool {
	if n.Name == "" {
		return false
	}
	if c.tag != "" && c.tag != "*" && c.tag != n.Name {
		return false
	}
	if c.id != "" && n.Attr("id") != c.id {
		return false
	}
	if len(c.classes) > 0 {
		classes := strings.Fields(n.Attr("class"))
		for _, want := range c.classes {
			found := false
			for _, have := range classes {
				if have == want {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	for _, a := range c.attrs {
		v, ok := n.Attrs[a.name]
		if !ok {
			return false
		}
		switch a.op {
		case "=":
			ok = v == a.value
		case "~=":
			ok = false
			for _, f := range strings.Fields(v) {
				if f == a.value {
					ok = true
				}
			}
		case "|=":
			ok = v == a.value || strings.HasPrefix(v, a.value+"-")
		case "^=":
			ok = strings.HasPrefix(v, a.value)
		case "$=":
			ok = strings.HasSuffix(v, a.value)
		case "*=":
			ok = strings.Contains(v, a.value)
		}
		if !ok {
			return false
		}
	}
	for _, p := range c.pseudo {
		switch p {
		case "first-child":
			if prevElement(n) != nil {
				return false
			}
		case "last-child":
			if nextElement(n) != nil {
				return false
			}
		case "root":
			if n.Parent != nil {
				return false
			}
		default:
			// :hover and the like never apply to a still picture.
			return false
		}
	}
	for _, x := range c.not {
		if x.matches(n) {
			return false
		}
	}
	return true
}

func prevElement(n *Node) *Node {
	if n.Parent == nil {
		return nil
	}
	var prev *Node
	for _, c := range n.Parent.Children {
		if c == n {
			return prev
		}
		if c.Name != "" {
			prev = c
		}
	}
	return nil
}

func nextElement(n *Node) *Node {
	if n.Parent == nil {
		return nil
	}
	seen := false
	for _, c := range n.Parent.Children {
		if c == n {
			seen = true
			continue
		}
		if seen && c.Name != "" {
			return c
		}
	}
	return nil
}

func (s *selector) matches(n *Node) bool { return s.matchFrom(0, n) }

func (s *selector) matchFrom(i int, n *Node) bool {
	if !s.parts[i].matches(n) {
		return false
	}
	if i == len(s.parts)-1 {
		return true
	}
	switch s.combinators[i] {
	case '>':
		return n.Parent != nil && s.matchFrom(i+1, n.Parent)
	case '+':
		p := prevElement(n)
		return p != nil && s.matchFrom(i+1, p)
	case '~':
		for p := prevElement(n); p != nil; p = prevElement(p) {
			if s.matchFrom(i+1, p) {
				return true
			}
		}
		return false
	default:
		for p := n.Parent; p != nil; p = p.Parent {
			if s.matchFrom(i+1, p) {
				return true
			}
		}
		return false
	}
}

// cascade computes the style-sheet and style-attribute declarations of every element.
func cascade(root *Node) {
	order := 0
	var rules []cssRule
	var collect func(*Node)
	collect = func(n *Node) {
		if n.Name == "style" {
			t := strings.ToLower(n.Attr("type"))
			if t == "" || t == "text/css" {
				rules = append(rules, parseCSS(textContent(n), &order)...)
			}
		}
		for _, c := range n.Children {
			collect(c)
		}
	}
	collect(root)
	type applied struct {
		d     declaration
		spec  int
		order int
	}
	var walk func(*Node)
	walk = func(n *Node) {
		if n.Name == "" {
			return
		}
		var list []applied
		for _, r := range rules {
			if r.selector.matches(n) {
				for _, d := range r.decls {
					list = append(list, applied{d, r.spec, r.order})
				}
			}
		}
		if st := n.Attr("style"); st != "" {
			order++
			for _, d := range parseDeclarations(st) {
				list = append(list, applied{d, 1 << 30, order})
			}
		}
		if len(list) > 0 {
			sort.SliceStable(list, func(i, j int) bool {
				a, b := list[i], list[j]
				if a.d.important != b.d.important {
					return !a.d.important
				}
				if a.spec != b.spec {
					return a.spec < b.spec
				}
				return a.order < b.order
			})
			n.styles = map[string]string{}
			for _, a := range list {
				n.styles[a.d.prop] = a.d.value
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
}
