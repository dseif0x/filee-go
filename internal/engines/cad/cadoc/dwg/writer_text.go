package dwg

import (
	"fmt"
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc/dxf"
)

// wTextEncoder converts strings to the bytes of pre-2007 text (the drawing
// code page), or to UTF-16LE.
type wTextEncoder struct {
	utf16 bool
	enc   *encoding.Encoder
	// codeIndex is the DWG code page number (CadUtils.GetCodeIndex) of the
	// encoding, written before pre-2007 xdata strings.
	codeIndex byte
}

func newWTextEncoder(e encoding.Encoding, codeIndex byte) *wTextEncoder {
	if e == nil {
		e = charmap.Windows1252
	}
	return &wTextEncoder{enc: e.NewEncoder(), codeIndex: codeIndex}
}

var wUTF16Encoder = &wTextEncoder{utf16: true, codeIndex: 43}

// encode converts s; characters the code page can't hold become \U+XXXX,
// which AutoCAD understands in texts, names and attributes.
func (t *wTextEncoder) encode(s string) []byte {
	if t.utf16 {
		return wUTF16(s)
	}
	if wIsASCII(s) {
		return []byte(s)
	}
	if out, err := t.enc.Bytes([]byte(s)); err == nil {
		return out
	}
	var buf []byte
	for _, r := range s {
		if r < 0x80 {
			buf = append(buf, byte(r))
			continue
		}
		out, err := t.enc.Bytes([]byte(string(r)))
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

func wIsASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// writerCodePageIndex maps $DWGCODEPAGE names to the DWG code page number:
// ACadSharp's name table (CadUtils) resolved through its code page list,
// plus ANSI_949 and ANSI_1258. Names it doesn't know give 30 (ANSI_1252),
// as in DwgFileHeaderWriterBase.getFileCodePage.
var writerCodePageIndex = map[string]byte{
	"gb2312": 31, "kcs5601": 25, "ascii": 1, "big5": 24, "johab": 26,
	"dos437": 11, "dos850": 12, "dos852": 13, "dos866": 27, "dos855": 14, "dos857": 15, "dos860": 16,
	"dos861": 17, "dos863": 18, "dos864": 19, "dos865": 20, "dos869": 21, "dos932": 22, "dos950": 24,
	"ansi_874": 37, "ansi_932": 22, "ansi_936": 31, "ansi_949": 25, "ansi_950": 24,
	"ansi_1250": 28, "ansi1250": 28, "ansi_1251": 29, "ansi1251": 29, "ansi_1252": 30, "ansi1252": 30,
	"ansi_1253": 32, "ansi1253": 32, "ansi_1254": 33, "ansi1254": 33, "ansi_1255": 34, "ansi1255": 34,
	"ansi_1256": 35, "ansi1256": 35, "ansi_1257": 36, "ansi1257": 36, "ansi_1258": 44,
	"iso8859-1": 2, "iso88591": 2, "iso8859-2": 3, "iso88592": 3, "iso8859-3": 4, "iso88593": 4,
	"iso8859-4": 5, "iso88594": 5, "iso8859-5": 6, "iso88595": 6, "iso8859-6": 7, "iso88596": 7,
	"iso8859-7": 8, "iso88597": 8, "iso8859-8": 9, "iso88598": 9, "iso8859-9": 10, "iso88599": 10,
}

func writerFileCodePage(name string) byte {
	if i, ok := writerCodePageIndex[strings.ToLower(strings.TrimSpace(name))]; ok && i >= 1 {
		return i
	}
	return 30
}

// writerEncoding resolves the drawing code page; ok is false when the name
// is unknown (Windows-1252 is used then).
func writerEncoding(codePage string) (enc *wTextEncoder, ok bool) {
	e := dxf.CodePageEncoding(codePage)
	if e == nil {
		return newWTextEncoder(charmap.Windows1252, 30), false
	}
	return newWTextEncoder(e, writerFileCodePage(codePage)), true
}
