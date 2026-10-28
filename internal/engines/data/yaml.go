package data

import (
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// YAML. Reading follows the YAML 1.2 core schema: unquoted true / false,
// null / ~, numbers (also 0x / 0o, .inf, .nan) get their type, everything else
// (and anything quoted) is text; "yes" / "no" stay text as in YAML 1.2.
// Anchors and aliases are resolved (the value is copied wherever it is
// referenced), merge keys ("<<") are applied, comments are dropped, and a file
// of several documents ("---") becomes an array of them.
// Writing uses block style indented by two spaces; text that would read back as
// another type ("true", "12", "") is quoted, multi-line text becomes a literal
// block ("|").

// maxYamlValues is the most values an alias may expand to ("billion laughs"
// files), far above any real document.
const maxYamlValues = 5_000_000

func readYAML(text string) (any, error) {
	dec := yaml.NewDecoder(strings.NewReader(text))
	var docs []*yaml.Node
	for {
		var n yaml.Node
		err := dec.Decode(&n)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("Not valid YAML: %v", err)
		}
		docs = append(docs, &n)
	}
	r := &yamlReader{}
	root := func(d *yaml.Node) (any, error) {
		if d.Kind == yaml.DocumentNode {
			if len(d.Content) == 0 {
				return nil, nil
			}
			d = d.Content[0]
		}
		return r.value(d, 0)
	}
	switch len(docs) {
	case 0:
		return nil, nil
	case 1:
		return root(docs[0])
	}
	out := make([]any, 0, len(docs))
	for _, d := range docs {
		v, err := root(d)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

type yamlReader struct{ values int }

func deref(n *yaml.Node) *yaml.Node {
	for i := 0; n.Kind == yaml.AliasNode && n.Alias != nil && i < MaxDepth; i++ {
		n = n.Alias
	}
	return n
}

func (r *yamlReader) value(n *yaml.Node, depth int) (any, error) {
	if depth > MaxDepth {
		return nil, errors.New("The YAML is nested too deeply (or an alias refers to itself).")
	}
	r.values++
	if r.values > maxYamlValues {
		return nil, errors.New("The YAML expands to too many values (aliases repeated too often).")
	}
	switch n.Kind {
	case yaml.AliasNode:
		if n.Alias == nil {
			return nil, nil
		}
		return r.value(n.Alias, depth+1)
	case yaml.MappingNode:
		return r.mapping(n, depth)
	case yaml.SequenceNode:
		list := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := r.value(c, depth+1)
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		return list, nil
	case yaml.ScalarNode:
		return yamlScalar(n)
	case yaml.DocumentNode:
		if len(n.Content) > 0 {
			return r.value(n.Content[0], depth)
		}
	}
	return nil, nil
}

func isPlain(n *yaml.Node) bool { return n.Style&^yaml.TaggedStyle == 0 }

func (r *yamlReader) mapping(n *yaml.Node, depth int) (any, error) {
	m := NewMap()
	for i := 0; i+1 < len(n.Content); i += 2 {
		kn, vn := deref(n.Content[i]), n.Content[i+1]
		if kn.Kind == yaml.ScalarNode && kn.Value == "<<" && isPlain(kn) && kn.Style&yaml.TaggedStyle == 0 {
			// Merge key: the merged mappings' entries, unless this mapping sets them itself (earlier or later).
			sources := []*yaml.Node{vn}
			if d := deref(vn); d.Kind == yaml.SequenceNode {
				sources = d.Content
			}
			for _, s := range sources {
				v, err := r.value(s, depth+1)
				if err != nil {
					return nil, err
				}
				merged, ok := v.(*Map)
				if !ok {
					return nil, errors.New("A YAML merge key (<<) must refer to a mapping.")
				}
				for _, k := range merged.keys {
					if !m.Has(k) && !yamlHasKey(n, k) {
						m.Set(k, merged.vals[k])
					}
				}
			}
			continue
		}
		var key string
		if kn.Kind == yaml.ScalarNode {
			key = kn.Value
		} else {
			// A complex key (a list or mapping, rare) is written as JSON.
			kv, err := r.value(kn, depth+1)
			if err != nil {
				return nil, err
			}
			key = strings.TrimSpace(compactJSON(kv))
		}
		v, err := r.value(vn, depth+1)
		if err != nil {
			return nil, err
		}
		m.Set(key, v)
	}
	return m, nil
}

func yamlHasKey(n *yaml.Node, key string) bool {
	for i := 0; i < len(n.Content); i += 2 {
		k := deref(n.Content[i])
		if k.Kind == yaml.ScalarNode && k.Value == key && k.Value != "<<" {
			return true
		}
	}
	return false
}

func yamlScalar(n *yaml.Node) (any, error) {
	t := n.Value
	tag := ""
	if n.Style&yaml.TaggedStyle != 0 {
		tag = n.ShortTag()
	}
	switch tag {
	case "!!str":
		return t, nil
	case "!!null":
		return nil, nil
	case "!!bool":
		if b, ok := resolveYAML(t).(bool); ok {
			return b, nil
		}
		return nil, fmt.Errorf("“%s” is not a YAML boolean.", t)
	case "!!int", "!!float":
		switch v := resolveYAML(t).(type) {
		case string, bool, nil:
			return nil, fmt.Errorf("“%s” is not a YAML number.", t)
		default:
			return v, nil
		}
	}
	// Quoted and block text is always text; only plain scalars are typed.
	if isPlain(n) && (tag == "" || tag == "!") {
		return resolveYAML(t), nil
	}
	return t, nil
}

var (
	reDecimalInt = regexp.MustCompile(`^[-+]?[0-9]+$`)
	reHexInt     = regexp.MustCompile(`^0x[0-9a-fA-F]+$`)
	reOctalInt   = regexp.MustCompile(`^0o[0-7]+$`)
	reFloat      = regexp.MustCompile(`^[-+]?(\.[0-9]+|[0-9]+(\.[0-9]*)?)([eE][-+]?[0-9]+)?$`)
	reYamlOneOne = regexp.MustCompile(`^(y|Y|yes|Yes|YES|n|N|no|No|NO|on|On|ON|off|Off|OFF|[-+]?[0-9][0-9_]*(\.[0-9_]*)?|[0-9]{4}-[0-9]{1,2}-[0-9]{1,2}([Tt ].*)?|[0-9]+(:[0-5]?[0-9])+(\.[0-9_]*)?)$`)
)

// resolveYAML is the YAML 1.2 core schema type of a plain scalar.
func resolveYAML(t string) any {
	switch t {
	case "", "~", "null", "Null", "NULL":
		return nil
	case "true", "True", "TRUE":
		return true
	case "false", "False", "FALSE":
		return false
	case ".inf", ".Inf", ".INF", "+.inf", "+.Inf", "+.INF":
		return math.Inf(1)
	case "-.inf", "-.Inf", "-.INF":
		return math.Inf(-1)
	case ".nan", ".NaN", ".NAN":
		return math.NaN()
	}
	switch {
	case reDecimalInt.MatchString(t):
		return number(strings.TrimLeft(t, "+"))
	case strings.HasPrefix(t, "0x") && reHexInt.MatchString(t):
		b, _ := new(big.Int).SetString(t[2:], 16)
		return small(b)
	case strings.HasPrefix(t, "0o") && reOctalInt.MatchString(t):
		b, _ := new(big.Int).SetString(t[2:], 8)
		return small(b)
	case reFloat.MatchString(t):
		f, _ := strconv.ParseFloat(t, 64)
		return f
	}
	return t
}

func small(b *big.Int) any {
	if b.IsInt64() {
		return b.Int64()
	}
	return b
}

// needsQuotes is true when a plain scalar with this text would read back as
// something other than the same text.
func needsQuotes(t string) bool {
	if _, isText := resolveYAML(t).(string); !isText {
		return true
	}
	// YAML 1.1 readers (PyYAML, older tools) take these as booleans, numbers with "_" or dates.
	return reYamlOneOne.MatchString(t) || strings.TrimSpace(t) != t
}

// ---- writing ----

type yamlCtx int

const (
	yamlRoot yamlCtx = iota
	yamlMapValue
	yamlSeqItem
)

func writeYAML(root any) (string, error) {
	var b strings.Builder
	if err := yamlNode(&b, root, 0, yamlRoot, 0); err != nil {
		return "", err
	}
	return b.String(), nil
}

// yamlNode writes v. indent is the column of the mapping the value belongs to
// (yamlMapValue), of the dash (yamlSeqItem) or 0 (yamlRoot); the cursor is just
// after "key:", after "- " or at the start of the document.
func yamlNode(b *strings.Builder, v any, indent int, ctx yamlCtx, depth int) error {
	if depth > MaxDepth {
		return errTooDeep
	}
	switch x := v.(type) {
	case *Map:
		if x.Len() == 0 {
			yamlInline(b, "{}", ctx)
			return nil
		}
		child := indent
		switch ctx {
		case yamlMapValue:
			child = indent + 2
			b.WriteByte('\n')
		case yamlSeqItem:
			child = indent + 2
		}
		for i, k := range x.keys {
			if i > 0 || ctx == yamlMapValue {
				b.WriteString(strings.Repeat(" ", child))
			}
			b.WriteString(yamlScalarText(k, false, child))
			b.WriteByte(':')
			if err := yamlNode(b, x.vals[k], child, yamlMapValue, depth+1); err != nil {
				return err
			}
		}
	case []any:
		if len(x) == 0 {
			yamlInline(b, "[]", ctx)
			return nil
		}
		child := indent
		switch ctx {
		case yamlMapValue:
			child = indent + 2
			b.WriteByte('\n')
		case yamlSeqItem:
			child = indent + 2
		}
		for i, item := range x {
			if i > 0 || ctx == yamlMapValue {
				b.WriteString(strings.Repeat(" ", child))
			}
			b.WriteString("- ")
			if err := yamlNode(b, item, child, yamlSeqItem, depth+1); err != nil {
				return err
			}
		}
	case string:
		if ctx == yamlMapValue {
			b.WriteByte(' ')
		}
		b.WriteString(yamlScalarText(x, true, indent))
		b.WriteByte('\n')
	default:
		var t string
		switch f, _ := v.(float64); {
		case v == nil:
			t = "null"
		case math.IsNaN(f):
			t = ".nan"
		case math.IsInf(f, 1):
			t = ".inf"
		case math.IsInf(f, -1):
			t = "-.inf"
		default:
			t = text(v)
		}
		yamlInline(b, t, ctx)
	}
	return nil
}

func yamlInline(b *strings.Builder, t string, ctx yamlCtx) {
	if ctx == yamlMapValue {
		b.WriteByte(' ')
	}
	b.WriteString(t)
	b.WriteByte('\n')
}

// yamlScalarText picks the style as YamlDotNet's emitter does: plain when the
// text allows it, else single-quoted, else double-quoted; multi-line values as
// a literal block.
func yamlScalarText(t string, multiLine bool, indent int) string {
	a := analyzeYAML(t)
	if multiLine && strings.Contains(t, "\n") && a.blockAllowed {
		return yamlLiteral(t, indent)
	}
	if needsQuotes(t) {
		return yamlDouble(t)
	}
	switch {
	case a.plainAllowed:
		return t
	case a.singleAllowed && !(strings.Contains(t, "\n")):
		return "'" + strings.ReplaceAll(t, "'", "''") + "'"
	}
	return yamlDouble(t)
}

type yamlAnalysis struct {
	plainAllowed, singleAllowed, blockAllowed bool
}

func isYamlPrintable(r rune) bool {
	return r == 0x9 || r == 0xA || r == 0xD || (r >= 0x20 && r <= 0x7E) || r == 0x85 ||
		(r >= 0xA0 && r <= 0xD7FF) || (r >= 0xE000 && r <= 0xFFFD && r != 0xFEFF) || (r >= 0x10000 && r <= 0x10FFFF)
}

func isBlank(r rune) bool { return r == ' ' || r == '\t' }
func isBreak(r rune) bool { return r == '\n' || r == '\r' || r == 0x85 || r == 0x2028 || r == 0x2029 }

// analyzeYAML follows libyaml's yaml_emitter_analyze_scalar (block context).
func analyzeYAML(t string) yamlAnalysis {
	if t == "" {
		return yamlAnalysis{singleAllowed: true}
	}
	plain, single, block := true, true, true
	if strings.HasPrefix(t, "---") || strings.HasPrefix(t, "...") {
		plain = false
	}
	runes := []rune(t)
	var leadingSpace, leadingBreak, trailingSpace, trailingBreak, breakSpace, spaceBreak, special bool
	prevSpace, prevBreak := false, false
	precededByWhitespace := true
	for i, r := range runes {
		next := rune(0)
		if i+1 < len(runes) {
			next = runes[i+1]
		}
		followedByWhitespace := i+1 >= len(runes) || isBlank(next) || isBreak(next)
		if i == 0 {
			switch r {
			case '#', ',', '[', ']', '{', '}', '&', '*', '!', '|', '>', '\'', '"', '%', '@', '`':
				plain = false
			case '?', ':', '-':
				if followedByWhitespace {
					plain = false
				}
			}
		} else {
			switch r {
			case ':':
				if followedByWhitespace {
					plain = false
				}
			case '#':
				if precededByWhitespace {
					plain = false
				}
			}
		}
		if !isYamlPrintable(r) || (r > 0x7E && r == 0xFEFF) {
			special = true
		}
		if isBreak(r) && r != '\n' {
			special = true // only "\n" is written as a line break
		}
		switch {
		case r == ' ':
			if i == 0 {
				leadingSpace = true
			}
			if i == len(runes)-1 {
				trailingSpace = true
			}
			if prevBreak {
				breakSpace = true
			}
			prevSpace, prevBreak = true, false
		case isBreak(r):
			if i == 0 {
				leadingBreak = true
			}
			if i == len(runes)-1 {
				trailingBreak = true
			}
			if prevSpace {
				spaceBreak = true
			}
			prevSpace, prevBreak = false, true
		default:
			prevSpace, prevBreak = false, false
		}
		precededByWhitespace = isBlank(r) || isBreak(r)
	}
	if leadingSpace || leadingBreak || trailingSpace || trailingBreak {
		plain = false
	}
	if trailingSpace {
		block = false
	}
	if breakSpace {
		plain, single = false, false
	}
	if spaceBreak || special {
		plain, single, block = false, false, false
	}
	if strings.ContainsAny(t, "\n\r") {
		plain = false
	}
	return yamlAnalysis{plainAllowed: plain, singleAllowed: single, blockAllowed: block}
}

// yamlLiteral writes a literal block ("|", "|-" or "|+"), indented two more
// than its parent.
func yamlLiteral(t string, indent int) string {
	var b strings.Builder
	b.WriteByte('|')
	if strings.HasPrefix(t, " ") || strings.HasPrefix(t, "\n") {
		b.WriteString("2")
	}
	switch {
	case !strings.HasSuffix(t, "\n"):
		b.WriteByte('-')
	case strings.HasSuffix(t, "\n\n") || t == "\n":
		b.WriteByte('+')
	}
	pad := strings.Repeat(" ", indent+2)
	body := strings.TrimSuffix(t, "\n")
	for line := range strings.SplitSeq(body, "\n") {
		b.WriteByte('\n')
		if line != "" {
			b.WriteString(pad)
			b.WriteString(line)
		}
	}
	return b.String()
}

func yamlDouble(t string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range t {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case 0:
			b.WriteString(`\0`)
		case '\a':
			b.WriteString(`\a`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\v':
			b.WriteString(`\v`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		case 0x1B:
			b.WriteString(`\e`)
		case 0x85:
			b.WriteString(`\N`)
		case 0xA0:
			b.WriteString(`\_`)
		case 0x2028:
			b.WriteString(`\L`)
		case 0x2029:
			b.WriteString(`\P`)
		default:
			switch {
			case !isYamlPrintable(r) || r == 0xFEFF:
				if r <= 0xFF {
					fmt.Fprintf(&b, `\x%02X`, r)
				} else if r <= 0xFFFF {
					fmt.Fprintf(&b, `\u%04X`, r)
				} else {
					fmt.Fprintf(&b, `\U%08X`, r)
				}
			default:
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
