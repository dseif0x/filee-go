package doc

import (
	"bytes"
	"os"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// DecodeText decodes text in whatever encoding its PC used: by its byte
// order mark, as UTF-8 when it is valid UTF-8, as CP949 when the bytes look
// Korean (or, on a Korean PC, are valid CP949), else in the PC's own code
// page (GBK on Chinese, Shift-JIS on Japanese, Windows-1252 on Western).
// A charset= in the text is content, not the file's encoding.
func DecodeText(b []byte) string { return DecodeTextWith(b, AnsiCodePage()) }

// DecodeTextWith is DecodeText with the PC's ANSI code page given (tests).
func DecodeTextWith(b []byte, ansi int) string {
	switch {
	case bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}):
		return string(b[3:])
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE}):
		return utf16String(b[2:], false)
	case bytes.HasPrefix(b, []byte{0xFE, 0xFF}):
		return utf16String(b[2:], true)
	case len(b) >= 4 && b[1] == 0 && b[3] == 0 && b[0] != 0:
		return utf16String(b, false)
	}
	if utf8.Valid(b) {
		return string(b)
	}
	if LooksKorean(b) || ansi == 949 && valid(korean.EUCKR, b) {
		return decode(korean.EUCKR, b)
	}
	switch ansi {
	case 932:
		return decode(japanese.ShiftJIS, b)
	case 936:
		return decode(simplifiedchinese.GBK, b)
	case 950:
		return decode(traditionalchinese.Big5, b)
	}
	if e := codePage(ansi); e != nil {
		return decode(e, b)
	}
	return decode(charmap.Windows1252, b)
}

func codePage(cp int) encoding.Encoding {
	switch cp {
	case 874:
		return charmap.Windows874
	case 1250:
		return charmap.Windows1250
	case 1251:
		return charmap.Windows1251
	case 1253:
		return charmap.Windows1253
	case 1254:
		return charmap.Windows1254
	case 1255:
		return charmap.Windows1255
	case 1256:
		return charmap.Windows1256
	case 1257:
		return charmap.Windows1257
	case 1258:
		return charmap.Windows1258
	}
	return nil
}

func utf16String(b []byte, big bool) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		if big {
			u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		} else {
			u[i] = uint16(b[2*i+1])<<8 | uint16(b[2*i])
		}
	}
	return string(utf16.Decode(u))
}

func decode(e encoding.Encoding, b []byte) string {
	out, err := e.NewDecoder().Bytes(b)
	if err != nil {
		return string(b)
	}
	return string(out)
}

// valid reports whether b decodes without replacement characters.
func valid(e encoding.Encoding, b []byte) bool {
	out, err := e.NewDecoder().Bytes(b)
	return err == nil && !bytes.ContainsRune(out, utf8.RuneError)
}

// LooksKorean: the non-ASCII bytes form CP949 pairs, most of them Hangul
// syllables of KS X 1001 and hardly any with a lead byte above C8.
func LooksKorean(b []byte) bool {
	pairs, hangul, high := 0, 0, 0
	for i := 0; i < len(b); i++ {
		lead := b[i]
		if lead < 0x80 {
			continue
		}
		if lead < 0x81 || lead > 0xFE || i+1 >= len(b) {
			return false
		}
		i++
		trail := b[i]
		if !(trail >= 0x41 && trail <= 0x5A || trail >= 0x61 && trail <= 0x7A || trail >= 0x81 && trail <= 0xFE) {
			return false
		}
		pairs++
		if lead >= 0xB0 && lead <= 0xC8 && trail >= 0xA1 {
			hangul++
		} else if lead > 0xC8 {
			high++
		}
	}
	return pairs > 0 && hangul*2 >= pairs && high*10 <= pairs
}

// AnsiCodePage is the PC's legacy code page (GetACP on Windows, the locale
// elsewhere); 1252 when unknown.
func AnsiCodePage() int {
	if cp := ansiCodePage(); cp > 0 {
		return cp
	}
	for _, v := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if l := os.Getenv(v); l != "" {
			return codePageOfLocale(l)
		}
	}
	return 1252
}

func codePageOfLocale(l string) int {
	l = strings.ToLower(l)
	switch {
	case strings.HasPrefix(l, "ko"):
		return 949
	case strings.HasPrefix(l, "ja"):
		return 932
	case strings.HasPrefix(l, "zh_tw"), strings.HasPrefix(l, "zh_hk"), strings.HasPrefix(l, "zh-hant"):
		return 950
	case strings.HasPrefix(l, "zh"):
		return 936
	case strings.HasPrefix(l, "ru"), strings.HasPrefix(l, "uk"), strings.HasPrefix(l, "bg"), strings.HasPrefix(l, "sr"):
		return 1251
	case strings.HasPrefix(l, "pl"), strings.HasPrefix(l, "cs"), strings.HasPrefix(l, "hu"), strings.HasPrefix(l, "sk"):
		return 1250
	case strings.HasPrefix(l, "el"):
		return 1253
	case strings.HasPrefix(l, "tr"):
		return 1254
	case strings.HasPrefix(l, "he"):
		return 1255
	case strings.HasPrefix(l, "ar"):
		return 1256
	case strings.HasPrefix(l, "th"):
		return 874
	case strings.HasPrefix(l, "vi"):
		return 1258
	}
	return 1252
}
