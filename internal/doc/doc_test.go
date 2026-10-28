package doc

import "testing"

func TestDecodeText(t *testing.T) {
	cases := []struct {
		in   []byte
		ansi int
		want string
	}{
		{[]byte("\xEF\xBB\xBFhé"), 1252, "hé"},
		{[]byte("h\xc3\xa9"), 1252, "hé"},
		{[]byte("h\xe9"), 1252, "hé"},
		{[]byte{0xFF, 0xFE, 'a', 0, 'b', 0}, 1252, "ab"},
		{[]byte("\xc7\xd1\xb1\xdb"), 1252, "한글"}, // CP949, read as Korean on any PC
		{[]byte("\xce\xd2\xca\xc7"), 936, "我是"},
	}
	for _, c := range cases {
		if got := DecodeTextWith(c.in, c.ansi); got != c.want {
			t.Errorf("%q: %q", c.in, got)
		}
	}
}

func TestColumnWidths(t *testing.T) {
	tb := &Table{ColumnWidths: []int{3000, 3000}}
	if w := ColumnWidths(tb, 3, 6000); w[0]+w[1]+w[2] > 6000 {
		t.Fatal(w)
	}
	tb = &Table{RelativeWidths: []float64{0.5}}
	if w := ColumnWidths(tb, 2, 1000); w[0] != 500 || w[1] != 500 {
		t.Fatal(w)
	}
	placed, n := PlaceCells(&Table{Rows: []*Row{{Cells: []*Cell{{Column: -1, RowSpan: 2, ColSpan: 1}, NewCell()}}, {Cells: []*Cell{NewCell()}}}})
	if n != 2 || placed[1][0].Column != 1 {
		t.Fatal(placed, n)
	}
}
