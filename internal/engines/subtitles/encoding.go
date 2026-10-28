package subtitles

import (
	"bytes"
	"regexp"
	"strings"
	"sync"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
)

// Subtitle files come in whatever encoding their author's PC used. Korean SAMI
// and SRT files are usually CP949 (EUC-KR plus Microsoft's extra Hangul),
// Chinese ones GBK, Western ones Windows-1252. Filee reads them all and always
// writes UTF-8.

var (
	acpOnce sync.Once
	acp     int
)

// AnsiCodePage is the PC's ANSI code page (1252 when unknown).
func AnsiCodePage() int {
	acpOnce.Do(func() { acp = systemCodePage() })
	return acp
}

// Decode is the text of a subtitle file: by its byte order mark; as UTF-8 when
// the bytes are valid UTF-8; by the charset the file declares (SAMI, TTML); as
// CP949 when the bytes look like Korean, or on Korean Windows when they are
// valid CP949; as the PC's code page on Chinese and Japanese Windows; else as
// the PC's Western code page.
func Decode(b []byte) string { return DecodeWith(b, AnsiCodePage(), true) }

// DecodeText is Decode for files where a charset= / encoding= in the text is
// content, not what the file is saved in (TXT, Markdown, CSV).
func DecodeText(b []byte) string { return DecodeWith(b, AnsiCodePage(), false) }

// DecodeWith is Decode with the code page given (for tests).
func DecodeWith(b []byte, ansiCodePage int, declaredCharset bool) string {
	if bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}) {
		return toValidUTF8(b[3:])
	}
	if bytes.HasPrefix(b, []byte{0xFF, 0xFE}) {
		return utf16String(b[2:], false)
	}
	if bytes.HasPrefix(b, []byte{0xFE, 0xFF}) {
		return utf16String(b[2:], true)
	}
	// UTF-16 without a byte order mark: every other byte of ASCII text is zero.
	if len(b) >= 4 && b[1] == 0 && b[3] == 0 && b[0] != 0 {
		return utf16String(b, false)
	}
	if utf8.Valid(b) {
		return string(b)
	}
	if declaredCharset {
		if e := declared(b); e != nil {
			return decodeWith(e, b)
		}
	}
	if LooksKorean(b) || (ansiCodePage == 949 && isCP949(b)) {
		return decodeWith(korean.EUCKR, b)
	}
	switch ansiCodePage {
	case 932, 936, 950:
		return decodeWith(CodePage(ansiCodePage), b)
	}
	if e := CodePage(ansiCodePage); e != nil && ansiCodePage > 0 && ansiCodePage != 949 && ansiCodePage != 1361 && ansiCodePage != 65001 {
		return decodeWith(e, b)
	}
	return decodeWith(charmap.Windows1252, b)
}

func toValidUTF8(b []byte) string { return strings.ToValidUTF8(string(b), "�") }

func utf16String(b []byte, bigEndian bool) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		if bigEndian {
			u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		} else {
			u[i] = uint16(b[2*i+1])<<8 | uint16(b[2*i])
		}
	}
	s := string(utf16.Decode(u))
	if len(b)%2 == 1 {
		s += "�"
	}
	return s
}

func decodeWith(e encoding.Encoding, b []byte) string {
	out, err := e.NewDecoder().Bytes(b)
	if err != nil {
		return toValidUTF8(b)
	}
	return string(out)
}

// CodePage is the encoding of a Windows code page, nil when unknown.
func CodePage(cp int) encoding.Encoding {
	switch cp {
	case 874:
		return charmap.Windows874
	case 932:
		return japanese.ShiftJIS
	case 936:
		return simplifiedchinese.GBK
	case 949:
		return korean.EUCKR
	case 950:
		return traditionalchinese.Big5
	case 1250:
		return charmap.Windows1250
	case 1251:
		return charmap.Windows1251
	case 1252:
		return charmap.Windows1252
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
	case 437:
		return charmap.CodePage437
	case 850:
		return charmap.CodePage850
	case 866:
		return charmap.CodePage866
	case 28591:
		return charmap.ISO8859_1
	case 65001:
		return unicode.UTF8
	}
	return nil
}

// Charset is the encoding of a charset name ("EUC-KR", "windows-1252",
// "ks_c_5601-1987"), nil when unknown. CP949 names map to the Windows code page,
// which is what Korean files hold whatever they declare.
func Charset(name string) encoding.Encoding {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "cp949", "ms949", "uhc", "x-windows-949", "windows-949", "ks_c_5601-1987", "ks_c_5601", "euc-kr", "euckr":
		return korean.EUCKR
	case "latin1", "latin-1", "iso8859-1", "iso-8859-1":
		return charmap.ISO8859_1
	case "cp1252":
		return charmap.Windows1252
	case "cp932", "ms932":
		return japanese.ShiftJIS
	case "cp936":
		return simplifiedchinese.GBK
	case "cp950":
		return traditionalchinese.Big5
	}
	if strings.HasPrefix(n, "cp") || strings.HasPrefix(n, "windows-") {
		digits := strings.TrimPrefix(strings.TrimPrefix(n, "cp"), "windows-")
		cp := 0
		for _, c := range digits {
			if c < '0' || c > '9' {
				cp = -1
				break
			}
			cp = cp*10 + int(c-'0')
		}
		if cp > 0 {
			if e := CodePage(cp); e != nil {
				return e
			}
		}
	}
	e, err := htmlindex.Get(n)
	if err != nil {
		return nil
	}
	return e
}

// IsUnicode tells UTF-8 and UTF-16 encodings apart from the others.
func IsUnicode(e encoding.Encoding) bool {
	name, _ := htmlindex.Name(e)
	switch name {
	case "utf-8", "utf-16le", "utf-16be":
		return true
	}
	return e == unicode.UTF8
}

// isCP949 is true when every byte decodes as CP949 without a substitute. Western
// text fails here (an accented letter followed by a space or punctuation is no
// CP949 pair), so a Korean PC still reads a Windows-1252 file as Windows-1252.
func isCP949(b []byte) bool {
	out, err := korean.EUCKR.NewDecoder().Bytes(b)
	return err == nil && !bytes.ContainsRune(out, utf8.RuneError)
}

var reDeclaredCharset = regexp.MustCompile(`(?i)(?:charset|encoding)\s*=\s*["']?([A-Za-z0-9_.:-]+)`)

// declared is the encoding named by charset=… (SAMI's HTML) or encoding="…"
// (XML) near the start.
func declared(b []byte) encoding.Encoding {
	head := b[:min(len(b), 2048)]
	m := reDeclaredCharset.FindSubmatch(head)
	if m == nil {
		return nil
	}
	e := Charset(string(m[1]))
	// A file claiming UTF-8 that isn't valid UTF-8 was saved by a program that lied: guess instead.
	if e == nil || IsUnicode(e) {
		return nil
	}
	return e
}

// LooksKorean is true when the non-ASCII bytes form CP949 pairs, most of them
// Hangul syllables of KS X 1001 (lead byte B0–C8, trail byte A1–FE) and hardly
// any with a lead byte above C8, as Korean text is. Western text decoded as
// CP949 gives pairs with ASCII trail bytes; Chinese GBK text uses lead bytes
// above C8 for a good part of its common characters, where Korean has only the
// rare Hanja.
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

// codePageOfLocale maps a POSIX locale ("ko_KR.UTF-8") to the ANSI code page
// Windows uses for that language.
func codePageOfLocale(locale string) int {
	l := strings.ToLower(locale)
	if i := strings.IndexAny(l, ".@"); i >= 0 {
		l = l[:i]
	}
	lang, region, _ := strings.Cut(strings.ReplaceAll(l, "-", "_"), "_")
	switch lang {
	case "ko":
		return 949
	case "ja":
		return 932
	case "zh":
		if region == "tw" || region == "hk" || region == "mo" {
			return 950
		}
		return 936
	case "th":
		return 874
	case "pl", "cs", "sk", "hu", "sl", "hr", "ro", "sq", "bs":
		return 1250
	case "ru", "uk", "bg", "be", "sr", "mk", "kk", "ky", "tt", "mn":
		return 1251
	case "el":
		return 1253
	case "tr", "az", "uz":
		return 1254
	case "he", "yi":
		return 1255
	case "ar", "fa", "ur":
		return 1256
	case "lt", "lv", "et":
		return 1257
	case "vi":
		return 1258
	}
	return 1252
}
