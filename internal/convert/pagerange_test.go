package convert

import (
	"slices"
	"testing"
)

func TestParsePageRange(t *testing.T) {
	for _, tc := range []struct {
		expr string
		n    int
		want []int
	}{
		{"", 3, []int{0, 1, 2}},
		{"1-3, 5, 8-", 9, []int{0, 1, 2, 4, 7, 8}},
		{"-2", 5, []int{0, 1}},
		{"4-2", 5, []int{1, 2, 3}},
		{"x, 99", 2, []int{0, 1}},
	} {
		if got := ParsePageRange(tc.expr, tc.n); !slices.Equal(got, tc.want) {
			t.Errorf("%q/%d = %v want %v", tc.expr, tc.n, got, tc.want)
		}
	}
}
