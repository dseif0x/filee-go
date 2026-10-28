package data

import (
	"strings"
	"testing"
)

// Text YAML has to quote reads back as the same text, whatever its shape.
func TestYAMLWriterRoundTrips(t *testing.T) {
	texts := []string{"plain", "a: b", "#comment", "- item", "key:", " lead", "trail ", "it's", `say "hi"`, "yes", "on", "1.5", "0x1F",
		"2026-10-16", "@at", "`tick", "a #b", "multi\nline", "multi\nline\n", "two\n\n", " indented\nblock", "tab\there", "ctrl\x01char",
		"[x]", "{y}", "*star", "&amp", "!bang", "%pct", "|pipe", ">gt", "?q", "? q", "---", "...", "null", "~", "", "한국어 텍스트", " sep"}
	m := NewMap()
	list := []any{}
	for i, s := range texts {
		m.Set(s, s)
		list = append(list, s, []any{s, map2(s)}, map2(s))
		_ = i
	}
	m.Set("list", list)
	m.Set("empty", []any{})
	m.Set("nested", []any{[]any{int64(1), int64(2)}, []any{}, NewMap()})
	for _, root := range []any{m, list, "just text", int64(7), nil} {
		y, err := writeYAML(root)
		if err != nil {
			t.Fatal(err)
		}
		back, err := readYAML(y)
		if err != nil {
			t.Fatalf("%v\n%s", err, y)
		}
		a, _ := writeJSON(normalizeInts(root))
		b, _ := writeJSON(back)
		if a != b {
			al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
			for i := range al {
				if i >= len(bl) || al[i] != bl[i] {
					t.Fatalf("line %d: want %q got %q", i, al[i], bl[min(i, len(bl)-1)])
				}
			}
		}
	}
}

func map2(s string) *Map {
	m := NewMap()
	m.Set("k", s)
	m.Set(s, "v")
	return m
}

func normalizeInts(v any) any {
	switch x := v.(type) {
	case int:
		return int64(x)
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = normalizeInts(x[i])
		}
		return out
	case *Map:
		m := NewMap()
		for _, k := range x.keys {
			m.Set(k, normalizeInts(x.vals[k]))
		}
		return m
	}
	return v
}

// TOML and XML written from awkward trees read back without errors.
func TestTOMLAndXMLWritersProduceValidFiles(t *testing.T) {
	m := NewMap()
	for _, s := range []string{"plain", "a b", "quote\"d", "back\\slash", "dotted.key", "", "한국어", "ctrl\x01", "new\nline"} {
		m.Set(s, s)
		m.Set("t"+s, map2(s))
	}
	m.Set("arr", []any{int64(1), "x", 2.5, true, nil, []any{int64(3)}, map2("in")})
	m.Set("tables", []any{map2("a"), map2("b")})
	tm, err := writeTOML(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readTOML(tm); err != nil {
		t.Fatalf("%v\n%s", err, tm)
	}
	x, err := writeXML(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readXML([]byte(x)); err != nil {
		t.Fatalf("%v\n%s", err, x)
	}
}
