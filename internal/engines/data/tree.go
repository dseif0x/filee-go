package data

import (
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// The in-memory tree every data format is read into and written from, the
// common ground of JSON, YAML, TOML, XML and CSV: objects (*Map, keys in file
// order), arrays ([]any) and scalars: string, bool, int64, *big.Int, float64,
// Date (a TOML date / time, kept as written) and nil.

// MaxDepth is the deepest nesting read or written; deeper input is rejected
// instead of overflowing the stack.
const MaxDepth = 256

// Map is an object: keys in the order the file has them; a repeated key keeps
// its first place and the last value.
type Map struct {
	keys []string
	vals map[string]any
}

func NewMap() *Map { return &Map{vals: map[string]any{}} }

func (m *Map) Len() int       { return len(m.keys) }
func (m *Map) Keys() []string { return m.keys }

func (m *Map) Set(k string, v any) {
	if _, ok := m.vals[k]; !ok {
		m.keys = append(m.keys, k)
	}
	m.vals[k] = v
}

func (m *Map) Get(k string) (any, bool) {
	v, ok := m.vals[k]
	return v, ok
}

func (m *Map) Has(k string) bool { _, ok := m.vals[k]; return ok }

// At returns the i-th entry.
func (m *Map) At(i int) (string, any) { return m.keys[i], m.vals[m.keys[i]] }

// Date is a TOML date, time or date-time as written in the file (RFC 3339).
type Date string

var errTooDeep = errors.New("The data is nested too deeply.")

// number reads a number in JSON syntax ("12", "-3.5", "1e3") as int64, *big.Int or float64.
func number(text string) any {
	if i, err := strconv.ParseInt(text, 10, 64); err == nil {
		return i
	}
	if !strings.ContainsAny(text, ".eE") {
		if b, ok := new(big.Int).SetString(strings.TrimPrefix(text, "+"), 10); ok {
			return b
		}
	}
	f, _ := strconv.ParseFloat(text, 64)
	return f
}

// formatDouble is a float as the shortest text that reads back the same, in
// .NET's spelling ("1E+15", "1E-05"), always with a fraction or exponent so it
// stays a float in YAML and TOML ("3.0", not "3"). Not for NaN or infinities.
func formatDouble(v float64) string {
	text := roundTrip(v)
	if !strings.ContainsAny(text, ".Ee") {
		return text + ".0"
	}
	return text
}

// roundTrip is .NET's double.ToString("R"): shortest digits, scientific
// notation from 1E+15 up and from 1E-05 down.
func roundTrip(v float64) string {
	if v == 0 {
		if math.Signbit(v) {
			return "-0"
		}
		return "0"
	}
	e := strconv.FormatFloat(v, 'e', -1, 64) // "-1.2345e+20"
	mant, exps, _ := strings.Cut(e, "e")
	exp, _ := strconv.Atoi(exps)
	neg := strings.HasPrefix(mant, "-")
	mant = strings.TrimPrefix(mant, "-")
	digits := strings.Replace(mant, ".", "", 1)
	sign := ""
	if neg {
		sign = "-"
	}
	if exp >= 15 || exp < -4 {
		m := digits[:1]
		if len(digits) > 1 {
			m += "." + digits[1:]
		}
		es := "+"
		if exp < 0 {
			es = "-"
			exp = -exp
		}
		ex := strconv.Itoa(exp)
		if len(ex) < 2 {
			ex = "0" + ex
		}
		return sign + m + "E" + es + ex
	}
	if exp < 0 {
		return sign + "0." + strings.Repeat("0", -exp-1) + digits
	}
	if len(digits) <= exp+1 {
		return sign + digits + strings.Repeat("0", exp+1-len(digits))
	}
	return sign + digits[:exp+1] + "." + digits[exp+1:]
}

// text of a scalar for formats without types (XML, CSV): "true", "12", "3.5", "" for null.
func text(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		switch {
		case math.IsNaN(x):
			return "NaN"
		case math.IsInf(x, 1):
			return "Infinity"
		case math.IsInf(x, -1):
			return "-Infinity"
		}
		return formatDouble(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case *big.Int:
		return x.String()
	case Date:
		return string(x)
	}
	return ""
}

func isScalar(v any) bool {
	switch v.(type) {
	case *Map, []any:
		return false
	}
	return true
}

// unwrapRecords is the list of records a table-like target (CSV, JSON Lines) is
// made of: the root array, or the array inside objects that hold nothing else
// ({"users": [...]}, XML's <rows><row/>…</rows>).
func unwrapRecords(root any) any {
	node := root
	for i := 0; i < MaxDepth; i++ {
		m, ok := node.(*Map)
		if !ok || m.Len() != 1 {
			break
		}
		_, only := m.At(0)
		// {"name": ["a", "b"]} is one column, not a wrapper around a list of values.
		if _, isMap := only.(*Map); isMap {
			node = only
			continue
		}
		if list, isList := only.([]any); isList && anyMap(list) {
			node = only
			continue
		}
		break
	}
	return node
}

func anyMap(list []any) bool {
	for _, x := range list {
		if _, ok := x.(*Map); ok {
			return true
		}
	}
	return false
}
