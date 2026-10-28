package dxf

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// CodePageEncoding returns the text encoding of a $DWGCODEPAGE name
// (ANSI_1252, ANSI_949, KCS5601, GB2312, BIG5, DOS932, ISO8859-1 …), nil when
// the name is not known. ACadSharp's table (CadUtils) plus ANSI_949, which
// Korean AutoCAD writes.
func CodePageEncoding(name string) encoding.Encoding {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "ansi_874", "ansi874", "dos874":
		return charmap.Windows874
	case "ansi_932", "ansi932", "dos932", "shift_jis", "sjis":
		return japanese.ShiftJIS
	case "ansi_936", "ansi936", "gb2312", "gbk", "dos936":
		return simplifiedchinese.GBK
	case "ansi_949", "ansi949", "kcs5601", "ksc5601", "johab", "dos949":
		return korean.EUCKR
	case "ansi_950", "ansi950", "big5", "dos950":
		return traditionalchinese.Big5
	case "ansi_1250", "ansi1250":
		return charmap.Windows1250
	case "ansi_1251", "ansi1251":
		return charmap.Windows1251
	case "ansi_1252", "ansi1252":
		return charmap.Windows1252
	case "ansi_1253", "ansi1253":
		return charmap.Windows1253
	case "ansi_1254", "ansi1254":
		return charmap.Windows1254
	case "ansi_1255", "ansi1255":
		return charmap.Windows1255
	case "ansi_1256", "ansi1256":
		return charmap.Windows1256
	case "ansi_1257", "ansi1257":
		return charmap.Windows1257
	case "ansi_1258", "ansi1258":
		return charmap.Windows1258
	case "ascii", "us-ascii":
		return charmap.Windows1252
	case "mac-roman", "macroman":
		return charmap.Macintosh
	case "dos437":
		return charmap.CodePage437
	case "dos850":
		return charmap.CodePage850
	case "dos852":
		return charmap.CodePage852
	case "dos855":
		return charmap.CodePage855
	case "dos857", "dos864", "dos869", "dos737", "dos775", "dos720":
		// Not in x/text: Windows-1252 keeps ASCII and approximates the rest.
		return nil
	case "dos860":
		return charmap.CodePage860
	case "dos861":
		return nil
	case "dos862":
		return charmap.CodePage862
	case "dos863":
		return charmap.CodePage863
	case "dos865":
		return charmap.CodePage865
	case "dos866":
		return charmap.CodePage866
	}
	if strings.HasPrefix(n, "iso8859") || strings.HasPrefix(n, "iso-8859") {
		num := strings.TrimLeft(strings.TrimPrefix(strings.TrimPrefix(n, "iso-8859"), "iso8859"), "-_")
		switch num {
		case "1":
			return charmap.ISO8859_1
		case "2":
			return charmap.ISO8859_2
		case "3":
			return charmap.ISO8859_3
		case "4":
			return charmap.ISO8859_4
		case "5":
			return charmap.ISO8859_5
		case "6":
			return charmap.ISO8859_6
		case "7":
			return charmap.ISO8859_7
		case "8":
			return charmap.ISO8859_8
		case "9":
			return charmap.ISO8859_9
		case "10":
			return charmap.ISO8859_10
		case "13":
			return charmap.ISO8859_13
		case "14":
			return charmap.ISO8859_14
		case "15":
			return charmap.ISO8859_15
		case "16":
			return charmap.ISO8859_16
		}
	}
	return nil
}

// textCodec converts between file bytes and Go strings.
type textCodec struct {
	utf8 bool
	enc  encoding.Encoding
	dec  *encoding.Decoder
	encr *encoding.Encoder
}

func newCodec(utf8Text bool, enc encoding.Encoding) *textCodec {
	c := &textCodec{utf8: utf8Text, enc: enc}
	if !utf8Text {
		if c.enc == nil {
			c.enc = charmap.Windows1252
		}
		c.dec = c.enc.NewDecoder()
		c.encr = c.enc.NewEncoder()
	}
	return c
}

func (c *textCodec) decode(b []byte) string {
	if c.utf8 || isASCII(b) {
		if c.utf8 && !utf8.Valid(b) {
			// Files that claim UTF-8 but are not: read the bytes as Windows-1252.
			s, err := charmap.Windows1252.NewDecoder().Bytes(b)
			if err == nil {
				return string(s)
			}
		}
		return string(b)
	}
	out, err := c.dec.Bytes(b)
	if err != nil {
		return string(b)
	}
	return string(out)
}

// encode converts a string to the file's code page; characters it can't hold
// are written as \U+XXXX, which AutoCAD reads in text, names and attributes.
func (c *textCodec) encode(s string) []byte {
	if c.utf8 || isASCIIString(s) {
		return []byte(s)
	}
	if out, err := c.encr.Bytes([]byte(s)); err == nil {
		return out
	}
	var buf []byte
	for _, r := range s {
		if r < 0x80 {
			buf = append(buf, byte(r))
			continue
		}
		out, err := c.encr.Bytes([]byte(string(r)))
		if err == nil && len(out) > 0 {
			buf = append(buf, out...)
			continue
		}
		if r > 0xFFFF {
			buf = append(buf, '?')
			continue
		}
		buf = append(buf, fmt.Sprintf("\\U+%04X", r)...)
	}
	return buf
}

func isASCII(b []byte) bool {
	for _, c := range b {
		if c >= 0x80 {
			return false
		}
	}
	return true
}

func isASCIIString(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
