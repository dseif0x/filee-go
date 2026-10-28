package dxf

import (
	"fmt"
	"math"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// TestAllHeaderVariables changes every header variable, writes them all and
// checks that each one reads back.
func TestAllHeaderVariables(t *testing.T) {
	doc := cadoc.NewDocument()
	h := doc.Header
	h.SetVersion(cadoc.AC1032)
	skip := map[string]bool{"$ACADVER": true, "$HANDSEED": true, "$DWGCODEPAGE": true}
	for i := range cadoc.HeaderVariables {
		v := &cadoc.HeaderVariables[i]
		if skip[v.Name] || v.Ignored {
			continue
		}
		vals := v.Get(h)
		next := make([]any, len(vals))
		for j, x := range vals {
			switch y := x.(type) {
			case string:
				next[j] = fmt.Sprintf("V%d", i)
			case float64:
				if v.Codes[j] == 40 && (v.Name[:3] == "$TD") {
					next[j] = 2460000.5
				} else {
					next[j] = y + 1.25
				}
			case int16:
				next[j] = int16(1 + (int(y)+1)%2)
			case int32:
				next[j] = int32(1 + (int(y)+1)%2)
			case int64:
				next[j] = int64(1 + (int(y)+1)%2)
			case bool:
				next[j] = !y
			default:
				next[j] = x
			}
		}
		v.Set(h, next)
	}
	back, _ := roundTrip(t, doc, WriteConfig{WriteAllHeaderVariables: true})
	compared := 0
	defer func() {
		if compared < 200 {
			t.Errorf("only %d variables compared", compared)
		}
	}()
	for i := range cadoc.HeaderVariables {
		v := &cadoc.HeaderVariables[i]
		if skip[v.Name] || v.Ignored {
			continue
		}
		want, got := v.Get(h), v.Get(back.Header)
		compared++
		for j := range want {
			if !sameValue(want[j], got[j]) {
				t.Errorf("%s[%d]: wrote %v, read %v", v.Name, j, want[j], got[j])
			}
		}
	}
}

func sameValue(a, b any) bool {
	if fa, ok := a.(float64); ok {
		fb, _ := b.(float64)
		return math.Abs(fa-fb) < 1e-6
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}
