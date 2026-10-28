package data

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// TOML: validated and parsed with go-toml, written here so the layout is
// predictable: plain keys first, then [tables] and [[arrays of tables]] in
// document order, a blank line before each header. Dates and times keep their
// TOML spelling (JSON and YAML get them as text). TOML has no null and needs a
// table at the root: null values are left out, and anything that is not an
// object (an array of CSV rows, a single value) is written under "items".

// tomlRootKey is the key of the root table that holds a value that is not an object.
const tomlRootKey = "items"

func readTOML(text string) (any, error) {
	// go-toml's decoder checks what the parser alone doesn't (keys defined twice, tables reopened).
	var check map[string]any
	if err := toml.Unmarshal([]byte(text), &check); err != nil {
		return nil, fmt.Errorf("Not valid TOML: %v", err)
	}
	root := NewMap()
	current := root
	p := unstable.Parser{}
	p.Reset([]byte(text))
	for p.NextExpression() {
		e := p.Expression()
		switch e.Kind {
		case unstable.KeyValue:
			if err := tomlKeyValue(current, e); err != nil {
				return nil, err
			}
		case unstable.Table:
			t, err := tomlPath(root, keyParts(e), false)
			if err != nil {
				return nil, err
			}
			current = t
		case unstable.ArrayTable:
			t, err := tomlPath(root, keyParts(e), true)
			if err != nil {
				return nil, err
			}
			current = t
		}
	}
	if err := p.Error(); err != nil {
		return nil, fmt.Errorf("Not valid TOML: %v", err)
	}
	return root, nil
}

func keyParts(n *unstable.Node) []string {
	var parts []string
	it := n.Key()
	for it.Next() {
		parts = append(parts, string(it.Node().Data))
	}
	return parts
}

// tomlPath opens the table at path (the last element of an array of tables),
// or appends a new table to the array at path.
func tomlPath(root *Map, path []string, arrayTable bool) (*Map, error) {
	m := root
	for i, k := range path {
		last := i == len(path)-1
		v, ok := m.Get(k)
		if last && arrayTable {
			list, _ := v.([]any)
			t := NewMap()
			m.Set(k, append(list, t))
			return t, nil
		}
		switch x := v.(type) {
		case nil:
			if ok {
				return nil, errors.New("Not valid TOML: a key is used as a table")
			}
			t := NewMap()
			m.Set(k, t)
			m = t
		case *Map:
			m = x
		case []any:
			if len(x) == 0 {
				return nil, errors.New("Not valid TOML: a key is used as a table")
			}
			t, isMap := x[len(x)-1].(*Map)
			if !isMap {
				return nil, errors.New("Not valid TOML: a key is used as a table")
			}
			m = t
		default:
			return nil, errors.New("Not valid TOML: a key is used as a table")
		}
	}
	return m, nil
}

func tomlKeyValue(table *Map, e *unstable.Node) error {
	parts := keyParts(e)
	t, err := tomlPath(table, parts[:len(parts)-1], false)
	if err != nil {
		return err
	}
	v, err := tomlValue(e.Value(), 0)
	if err != nil {
		return err
	}
	t.Set(parts[len(parts)-1], v)
	return nil
}

func tomlValue(n *unstable.Node, depth int) (any, error) {
	if depth > MaxDepth {
		return nil, errors.New("The TOML is nested too deeply.")
	}
	raw := string(n.Data)
	switch n.Kind {
	case unstable.String:
		return raw, nil
	case unstable.Bool:
		return raw == "true", nil
	case unstable.Integer:
		i, err := strconv.ParseInt(strings.ReplaceAll(raw, "_", ""), 0, 64)
		if err != nil {
			return nil, fmt.Errorf("Not valid TOML: %v", err)
		}
		return i, nil
	case unstable.Float:
		s := strings.ReplaceAll(raw, "_", "")
		switch strings.TrimLeft(s, "+-") {
		case "inf":
			if strings.HasPrefix(s, "-") {
				return math.Inf(-1), nil
			}
			return math.Inf(1), nil
		case "nan":
			return math.NaN(), nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("Not valid TOML: %v", err)
		}
		return f, nil
	case unstable.LocalDate, unstable.LocalTime, unstable.LocalDateTime, unstable.DateTime:
		// Kept as written, with the separators of RFC 3339.
		d := []byte(raw)
		if len(d) > 10 && (d[10] == ' ' || d[10] == 't') && n.Kind != unstable.LocalTime {
			d[10] = 'T'
		}
		if len(d) > 0 && d[len(d)-1] == 'z' {
			d[len(d)-1] = 'Z'
		}
		return Date(d), nil
	case unstable.Array:
		list := []any{}
		it := n.Children()
		for it.Next() {
			c := it.Node()
			if c.Kind == unstable.Comment {
				continue
			}
			v, err := tomlValue(c, depth+1)
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		return list, nil
	case unstable.InlineTable:
		m := NewMap()
		it := n.Children()
		for it.Next() {
			c := it.Node()
			if c.Kind != unstable.KeyValue {
				continue
			}
			if err := tomlKeyValue(m, c); err != nil {
				return nil, err
			}
		}
		return m, nil
	}
	return nil, fmt.Errorf("Not valid TOML: unexpected %s", n.Kind)
}

// ---- writing ----

func writeTOML(root any) (string, error) {
	table, ok := root.(*Map)
	if !ok {
		table = NewMap()
		table.Set(tomlRootKey, root)
	}
	var b strings.Builder
	if err := tomlTable(&b, nil, table, 0); err != nil {
		return "", err
	}
	t := strings.TrimLeft(b.String(), "\n")
	if t == "" || strings.HasSuffix(t, "\n") {
		return t, nil
	}
	return t + "\n", nil
}

func allMaps(list []any) bool {
	for _, x := range list {
		if _, ok := x.(*Map); !ok {
			return false
		}
	}
	return true
}

func tomlTable(b *strings.Builder, path []string, table *Map, depth int) error {
	if depth > MaxDepth {
		return errTooDeep
	}
	// Keys with plain values first: after a [header] every key belongs to that table.
	var nested []string
	for _, k := range table.keys {
		switch x := table.vals[k].(type) {
		case nil:
			// TOML has no null.
		case *Map:
			nested = append(nested, k)
		case []any:
			if len(x) > 0 && allMaps(x) {
				nested = append(nested, k)
				continue
			}
			b.WriteString(tomlKey(k) + " = ")
			if err := tomlInline(b, x, depth+1); err != nil {
				return err
			}
			b.WriteByte('\n')
		default:
			b.WriteString(tomlKey(k) + " = ")
			if err := tomlInline(b, x, depth+1); err != nil {
				return err
			}
			b.WriteByte('\n')
		}
	}
	for _, k := range nested {
		child := append(append([]string{}, path...), k)
		keys := make([]string, len(child))
		for i, c := range child {
			keys[i] = tomlKey(c)
		}
		header := strings.Join(keys, ".")
		if m, ok := table.vals[k].(*Map); ok {
			b.WriteString("\n[" + header + "]\n")
			if err := tomlTable(b, child, m, depth+1); err != nil {
				return err
			}
			continue
		}
		for _, item := range table.vals[k].([]any) {
			b.WriteString("\n[[" + header + "]]\n")
			if err := tomlTable(b, child, item.(*Map), depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// tomlInline writes a value on one line: arrays as [a, b], objects as inline tables { a = 1 }.
func tomlInline(b *strings.Builder, v any, depth int) error {
	if depth > MaxDepth {
		return errTooDeep
	}
	switch x := v.(type) {
	case *Map:
		first := true
		for _, k := range x.keys {
			if x.vals[k] == nil {
				continue
			}
			if first {
				b.WriteString("{ ")
			} else {
				b.WriteString(", ")
			}
			first = false
			b.WriteString(tomlKey(k) + " = ")
			if err := tomlInline(b, x.vals[k], depth+1); err != nil {
				return err
			}
		}
		if first {
			b.WriteString("{}")
		} else {
			b.WriteString(" }")
		}
	case []any:
		b.WriteByte('[')
		first := true
		for _, item := range x {
			if item == nil {
				continue
			}
			if !first {
				b.WriteString(", ")
			}
			first = false
			if err := tomlInline(b, item, depth+1); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case bool:
		b.WriteString(text(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case float64:
		switch {
		case math.IsNaN(x):
			b.WriteString("nan")
		case math.IsInf(x, 1):
			b.WriteString("inf")
		case math.IsInf(x, -1):
			b.WriteString("-inf")
		default:
			b.WriteString(formatDouble(x))
		}
	case Date:
		b.WriteString(string(x))
	case *big.Int:
		// TOML integers are 64-bit: a larger one is kept as text rather than rounded.
		tomlQuote(b, x.String())
	default:
		tomlQuote(b, text(v))
	}
	return nil
}

var reBareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func tomlKey(k string) string {
	if reBareKey.MatchString(k) {
		return k
	}
	var b strings.Builder
	tomlQuote(&b, k)
	return b.String()
}

// tomlQuote writes a basic string: quotes, backslashes and control characters escaped.
func tomlQuote(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if unicode.IsControl(r) {
				fmt.Fprintf(b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}
