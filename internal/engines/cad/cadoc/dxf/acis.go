package dxf

import "strings"

// acisCode swaps the printable characters of SAT text, the "encryption"
// pre-2013 DXF files use (AcisTextCodec); the swap is its own inverse.
func acisCode(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, c := range s {
		if c > ' ' && c < 0x9F {
			b.WriteRune(0x9F - c)
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// acisIsEncoded reports encoded SAT: plain SAT starts with the numeric
// version line.
func acisIsEncoded(s string) bool {
	for _, c := range s {
		if c <= ' ' {
			continue
		}
		return c < '0' || c > '9'
	}
	return false
}
