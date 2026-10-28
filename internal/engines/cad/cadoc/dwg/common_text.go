package dwg

import (
	"strings"
	"unicode/utf16"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// DwgCodePage describes one entry of the DWG code page index table
// (CadUtils._pageCodes): the value stored in file headers and string
// prefixes.
type DwgCodePage struct {
	// Windows is the Windows code page number (ACadSharp's CodePage value).
	Windows int
	// Name is the $DWGCODEPAGE name ACadSharp gives the code page (the first
	// matching key of CadUtils._dxfEncodingMap), empty when it has none.
	Name string
	// Encoding decodes the code page; nil means Windows-1252 (no Go encoding
	// or no code page at all).
	Encoding encoding.Encoding
	// UTF16 is set for the Unicode entry (index 43).
	UTF16 bool
}

// DwgCodePages is indexed by the DWG code page number.
var DwgCodePages = []DwgCodePage{
	0:  {0, "", nil, false},
	1:  {20127, "ascii", charmap.Windows1252, false},
	2:  {28591, "iso8859-1", charmap.ISO8859_1, false},
	3:  {28592, "iso8859-2", charmap.ISO8859_2, false},
	4:  {28593, "iso8859-3", charmap.ISO8859_3, false},
	5:  {28594, "iso8859-4", charmap.ISO8859_4, false},
	6:  {28595, "iso8859-5", charmap.ISO8859_5, false},
	7:  {28596, "iso8859-6", charmap.ISO8859_6, false},
	8:  {28597, "iso8859-7", charmap.ISO8859_7, false},
	9:  {28598, "iso8859-8", charmap.ISO8859_8, false},
	10: {28599, "iso8859-9", charmap.ISO8859_9, false},
	11: {437, "dos437", charmap.CodePage437, false},
	12: {850, "dos850", charmap.CodePage850, false},
	13: {852, "dos852", charmap.CodePage852, false},
	14: {855, "dos855", charmap.CodePage855, false},
	15: {857, "dos857", nil, false},
	16: {860, "dos860", charmap.CodePage860, false},
	17: {861, "dos861", nil, false},
	18: {863, "dos863", charmap.CodePage863, false},
	19: {864, "dos864", nil, false},
	20: {865, "dos865", charmap.CodePage865, false},
	21: {869, "dos869", nil, false},
	22: {932, "dos932", japanese.ShiftJIS, false},
	23: {10000, "", charmap.Macintosh, false},
	24: {950, "big5", traditionalchinese.Big5, false},
	25: {949, "kcs5601", korean.EUCKR, false},
	26: {1361, "johab", korean.EUCKR, false},
	27: {866, "dos866", charmap.CodePage866, false},
	28: {1250, "ansi_1250", charmap.Windows1250, false},
	29: {1251, "ansi_1251", charmap.Windows1251, false},
	30: {1252, "ansi_1252", charmap.Windows1252, false},
	31: {936, "gb2312", simplifiedchinese.GBK, false},
	32: {1253, "ansi_1253", charmap.Windows1253, false},
	33: {1254, "ansi_1254", charmap.Windows1254, false},
	34: {1255, "ansi_1255", charmap.Windows1255, false},
	35: {1256, "ansi_1256", charmap.Windows1256, false},
	36: {1257, "ansi_1257", charmap.Windows1257, false},
	37: {874, "ansi_874", charmap.Windows874, false},
	38: {932, "dos932", japanese.ShiftJIS, false},
	39: {936, "gb2312", simplifiedchinese.GBK, false},
	40: {949, "kcs5601", korean.EUCKR, false},
	41: {950, "big5", traditionalchinese.Big5, false},
	42: {1361, "johab", korean.EUCKR, false},
	43: {1200, "", nil, true},
	44: {1258, "", charmap.Windows1258, false},
}

// DwgCodePageAt returns the table entry of a DWG code page number
// (CadUtils.GetCodePage: unknown numbers give entry 0).
func DwgCodePageAt(index int) DwgCodePage {
	if index < 0 || index >= len(DwgCodePages) {
		return DwgCodePages[0]
	}
	return DwgCodePages[index]
}

// textDecoder turns single byte (code page) strings into Go strings.
type textDecoder struct {
	enc  encoding.Encoding
	dec  *encoding.Decoder
	utf8 bool
}

func newTextDecoder(enc encoding.Encoding) *textDecoder {
	if enc == nil {
		enc = charmap.Windows1252
	}
	return &textDecoder{enc: enc, dec: enc.NewDecoder()}
}

// utf8Decoder decodes as UTF-8, what .NET's Encoding.Default is.
var utf8Decoder = &textDecoder{utf8: true}

// windows1252Decoder is used where ACadSharp hard codes Windows-1252.
var windows1252Decoder = newTextDecoder(charmap.Windows1252)

func (d *textDecoder) decode(b []byte) string {
	if d == nil || d.utf8 || isASCIIBytes(b) {
		return strings.ToValidUTF8(string(b), "�")
	}
	out, err := d.dec.Bytes(b)
	if err != nil {
		return string(b)
	}
	return string(out)
}

// decodeUTF16LE decodes little endian UTF-16 like Encoding.Unicode.GetString
// (unpaired surrogates become U+FFFD; an odd trailing byte is dropped).
func decodeUTF16LE(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(u))
}

func isASCIIBytes(b []byte) bool {
	for _, c := range b {
		if c >= 0x80 {
			return false
		}
	}
	return true
}

// removeNUL drops the NUL characters ACadSharp strips with Replace("\0", "").
func removeNUL(s string) string {
	if strings.IndexByte(s, 0) < 0 {
		return s
	}
	return strings.ReplaceAll(s, "\x00", "")
}
