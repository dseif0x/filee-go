package spreadsheet

import (
	"testing"
	"time"
)

func TestFormats(t *testing.T) {
	f := NewFormatter(false)
	cases := []struct {
		v      float64
		format string
		want   string
	}{
		{1234.5, "#,##0.00", "1,234.50"},
		{1234567, "#,##0", "1,234,567"},
		{-1234.5, "#,##0.00", "-1,234.50"},
		{-5, "#,##0 ;(#,##0)", "(5)"},
		{0.256, "0%", "26%"},
		{0.256, "0.00%", "25.60%"},
		{12345, "0.00E+00", "1.23E+04"},
		{12345, "##0.0E+0", "12.3E+3"},
		{0.5, "#.##", ".5"},
		{2.675, "0.00", "2.68"},
		{5551234, "000-0000", "555-1234"},
		{1.5, "# ?/?", "1 1/2"},
		{0.75, "?/4", "3/4"},
		{45000, "yyyy-mm-dd", "2023-03-15"},
		{45000.75, "yyyy-mm-dd h:mm", "2023-03-15 18:00"},
		{45000.75, "h:mm AM/PM", "6:00 PM"},
		{1.5, "[h]:mm:ss", "36:00:00"},
		{45000, "d-mmm-yy", "15-Mar-23"},
		{45000, "dddd, mmmm d", "Wednesday, March 15"},
		{0.000694444, "mm:ss", "01:00"},
		{1234.5, `"$"#,##0.00`, "$1,234.50"},
		{1234.5, `[$€-407] #,##0.00`, "€ 1,234.50"},
		{1500000, `#,##0,"K"`, "1,500K"},
		{5, "[Red]0;[Blue]-0", "5"},
		{150, `[>100]"big";"small"`, "big"},
		{7, "@", "7"},
		{0, "0.0;-0.0;\"zero\"", "zero"},
		{-0.001, "0.00", "0.00"},
		{1, "General", "1"},
	}
	for _, c := range cases {
		if got := f.Format(c.v, c.format); got != c.want {
			t.Errorf("Format(%v, %q) = %q, want %q", c.v, c.format, got, c.want)
		}
	}
}

func TestGeneral(t *testing.T) {
	for v, want := range map[float64]string{1: "1", 0.1: "0.1", 1.0 / 3: "0.3333333333", 123456789012: "1.23457E+11", 0.00001: "0.00001", 0.000001: "1E-06", -2.5: "-2.5"} {
		if got := General(v); got != want {
			t.Errorf("General(%v) = %q, want %q", v, got, want)
		}
	}
}

func TestSerials(t *testing.T) {
	d := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if s := ToSerial(d); s != 46296.5 {
		t.Fatalf("serial %v", s)
	}
	if got := FromSerial(46296.5); !got.Equal(d) {
		t.Fatalf("from %v", got)
	}
	if s := ToSerial(time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)); s != 1 {
		t.Fatalf("1900-01-01 = %v", s)
	}
	f := NewFormatter(false)
	if !f.IsDate("yyyy-mm-dd") || f.IsDate("[h]:mm") || f.IsDate("0.00") {
		t.Fatal("IsDate")
	}
}
