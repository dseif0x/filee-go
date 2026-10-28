package rhwp

// Scripts whose text needs complex shaping to be drawn: letters that join (Arabic), right-to-left order (Arabic,
// Hebrew), vowel signs drawn before the consonant they follow, conjuncts and stacked marks (the Indic scripts, Thai,
// Khmer, Myanmar, Tibetan, ...). rhwp draws one glyph per character from left to right without any of that, so a
// document with such text goes to LibreOffice instead (issues #185 and #202).
//
// The scan reads a document's text cheaply, without reading it into the document model: the content parts of an
// Office Open XML / OWPML / OpenDocument / EPUB package as they are (markup is ASCII, so any match is text), a text
// file decoded like the plain text reader does (character references of HTML and RTF included).
//
// The same scan also finds text rhwp has no widths for: its width tables cover only ASCII and Latin-1, general
// punctuation, math, the CJK forms and Hangul (rhwp#8143), so a letter of any other script — Polish, Turkish,
// Vietnamese, Greek, Cyrillic, ... — or a combining mark is laid out at a fixed half-em.

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/encoding/htmlindex"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/spreadsheet"
)

type block struct {
	start, end rune
	name       string
}

// shapingBlocks are the Unicode blocks of scripts that need shaping, with the script's English name.
var shapingBlocks = []block{
	{0x0590, 0x05FF, "Hebrew"}, {0x0600, 0x06FF, "Arabic"}, {0x0700, 0x074F, "Syriac"}, {0x0750, 0x077F, "Arabic"},
	{0x0780, 0x07BF, "Thaana"}, {0x07C0, 0x07FF, "N'Ko"}, {0x0800, 0x083F, "Samaritan"}, {0x0840, 0x085F, "Mandaic"},
	{0x0860, 0x086F, "Syriac"}, {0x0870, 0x08FF, "Arabic"}, {0x0900, 0x097F, "Devanagari"}, {0x0980, 0x09FF, "Bengali"},
	{0x0A00, 0x0A7F, "Gurmukhi"}, {0x0A80, 0x0AFF, "Gujarati"}, {0x0B00, 0x0B7F, "Odia"}, {0x0B80, 0x0BFF, "Tamil"},
	{0x0C00, 0x0C7F, "Telugu"}, {0x0C80, 0x0CFF, "Kannada"}, {0x0D00, 0x0D7F, "Malayalam"}, {0x0D80, 0x0DFF, "Sinhala"},
	{0x0E00, 0x0E7F, "Thai"}, {0x0E80, 0x0EFF, "Lao"}, {0x0F00, 0x0FFF, "Tibetan"}, {0x1000, 0x109F, "Myanmar"},
	{0x1780, 0x17FF, "Khmer"}, {0x1800, 0x18AF, "Mongolian"}, {0x1900, 0x194F, "Limbu"}, {0x1950, 0x19DF, "Tai Le"},
	{0x19E0, 0x19FF, "Khmer"}, {0x1A00, 0x1A1F, "Buginese"}, {0x1A20, 0x1AAF, "Tai Tham"}, {0x1B00, 0x1B7F, "Balinese"},
	{0x1B80, 0x1BBF, "Sundanese"}, {0x1BC0, 0x1BFF, "Batak"}, {0x1C00, 0x1C4F, "Lepcha"}, {0xA800, 0xA82F, "Syloti Nagri"},
	{0xA880, 0xA8DF, "Saurashtra"}, {0xA8E0, 0xA8FF, "Devanagari"}, {0xA900, 0xA95F, "Kayah Li"}, {0xA980, 0xA9DF, "Javanese"},
	{0xA9E0, 0xA9FF, "Myanmar"}, {0xAA00, 0xAA5F, "Cham"}, {0xAA60, 0xAA7F, "Myanmar"}, {0xAA80, 0xAADF, "Tai Viet"},
	{0xAAE0, 0xAAFF, "Meetei Mayek"}, {0xABC0, 0xABFF, "Meetei Mayek"}, {0xFB1D, 0xFB4F, "Hebrew"}, {0xFB50, 0xFDFF, "Arabic"},
	{0xFE70, 0xFEFF, "Arabic"}, {0x10800, 0x10FFF, "right-to-left"}, {0x11000, 0x11FFF, "Brahmic"},
}

// measuredRanges are the code point ranges rhwp's width tables are generated for (rhwp#8143).
var measuredRanges = [][2]rune{{0x0020, 0x007E}, {0x00A0, 0x00FF}, {0x2000, 0x206F}, {0x2200, 0x22FF}, {0x3000, 0x303F},
	{0x3130, 0x318F}, {0xAC00, 0xD7A3}, {0xFF00, 0xFF5E}}

// fullWidthCjk are blocks rhwp does not measure but lays out at a full em, which is right for them.
var fullWidthCjk = [][2]rune{{0x1100, 0x11FF}, {0x2E80, 0x2FDF}, {0x3000, 0x303F}, {0x3040, 0x30FF}, {0x3100, 0x312F},
	{0x3130, 0x318F}, {0x31A0, 0x31FF}, {0x3200, 0x33FF}, {0x3400, 0x4DBF}, {0x4E00, 0x9FFF}, {0xA960, 0xA97F},
	{0xAC00, 0xD7FF}, {0xF900, 0xFAFF}, {0xFF00, 0xFFEF}, {0x20000, 0x3FFFF}}

// variationSelectors pick the glyph variant of the letter they follow and carry no width of their own.
var variationSelectors = [][2]rune{{0x180B, 0x180D}, {0xFE00, 0xFE0F}, {0xE0100, 0xE01EF}}

// unmeasuredNames are coarse names for the log line; else "U+XXXX".
var unmeasuredNames = []block{{0x0100, 0x024F, "Latin Extended"}, {0x0250, 0x02AF, "IPA"}, {0x0300, 0x036F, "combining marks"},
	{0x0370, 0x03FF, "Greek"}, {0x0400, 0x052F, "Cyrillic"}, {0x0530, 0x058F, "Armenian"}, {0x10A0, 0x10FF, "Georgian"},
	{0x1C80, 0x1C8F, "Cyrillic"}, {0x1E00, 0x1EFF, "Latin Extended"}, {0x1F00, 0x1FFF, "Greek"}, {0x2DE0, 0x2DFF, "Cyrillic"},
	{0xA640, 0xA69F, "Cyrillic"}}

// ScriptOf is the script of a code point when it needs shaping ("Arabic", "Gujarati"), else "".
func ScriptOf(c rune) string {
	// Latin, Greek, Cyrillic, Armenian; CJK, Hangul and punctuation in between: the common case, answered at once.
	if c < 0x0590 || c >= 0x2000 && c < 0xA800 || c >= 0xAC00 && c < 0xFB1D {
		return ""
	}
	for _, b := range shapingBlocks {
		if c < b.start {
			return ""
		}
		if c <= b.end {
			return b.name
		}
	}
	return ""
}

func inside(ranges [][2]rune, c rune) bool {
	for _, r := range ranges {
		if c < r[0] {
			return false
		}
		if c <= r[1] {
			return true
		}
	}
	return false
}

// unmeasuredScriptOf is the script of a code point when rhwp draws it but has no width for it ("Cyrillic"), else "".
func unmeasuredScriptOf(c rune) string {
	if c < 0 || c > 0x10FFFF || inside(variationSelectors, c) || !(unicode.IsLetter(c) || unicode.In(c, unicode.Mn, unicode.Mc)) ||
		inside(measuredRanges, c) || inside(fullWidthCjk, c) || ScriptOf(c) != "" {
		return ""
	}
	for _, b := range unmeasuredNames {
		if c < b.start {
			break
		}
		if c <= b.end {
			return b.name
		}
	}
	return fmt.Sprintf("U+%04X", c)
}

// unknownNeighbour stands for the neighbour of a character reference: not known, so it counts as part of a word.
const unknownNeighbour = -2

type classifier func(c, before, after rune) string

// FindBothInFile scans a document's text for the first script that needs shaping (which stops the scan) and the first
// script rhwp cannot measure. Only letters inside words count: a symbol or unit on its own (α, Ω, μm in a Korean
// report) draws fine. An extended Latin letter or combining mark counts next to any letter (Polish, Vietnamese words);
// a letter of another script only next to one of the same script (Отчёт, Αναφορά).
func FindBothInFile(path, format string) (shaping, unmeasured string) {
	both := func(c, before, after rune) string {
		if s := ScriptOf(c); s != "" {
			return s
		}
		if unmeasured == "" {
			if name := unmeasuredScriptOf(c); name != "" && inWord(name, before, after) {
				unmeasured = name
			}
		}
		return ""
	}
	// No letter below 0x0100 is unmeasured: ASCII and Latin-1 are, controls are no letters.
	shaping = findInFile(path, format, both, 0x0100)
	return shaping, unmeasured
}

func inWord(name string, before, after rune) bool {
	if before == unknownNeighbour || after == unknownNeighbour {
		return true
	}
	if name == "Latin Extended" || name == "IPA" || name == "combining marks" {
		return before >= 0 && unicode.IsLetter(before) || after >= 0 && unicode.IsLetter(after)
	}
	return before >= 0 && unmeasuredScriptOf(before) == name || after >= 0 && unmeasuredScriptOf(after) == name
}

func find(text string, classify classifier, skipBelow rune) string {
	rs := []rune(text)
	for i, c := range rs {
		if c < skipBelow {
			continue
		}
		before, after := rune(-1), rune(-1)
		if i > 0 {
			before = rs[i-1]
		}
		if i+1 < len(rs) {
			after = rs[i+1]
		}
		if s := classify(c, before, after); s != "" {
			return s
		}
	}
	return ""
}

func findInFile(path, format string, classify classifier, skipBelow rune) string {
	if isZip(path) {
		return findInPackage(path, format, classify, skipBelow)
	}
	switch format {
	case "xls", "xlt":
		// Excel 97–2003 is binary: its cells, as the built-in reader reads them.
		book, err := spreadsheet.Read(path, format)
		if err != nil {
			return ""
		}
		for _, sheet := range book.Sheets {
			for _, cells := range sheet.Rows {
				for _, cell := range cells {
					if s := find(cell.Text, classify, skipBelow); s != "" {
						return s
					}
				}
			}
		}
		return ""
	case "mhtml", "eml":
		// Web archives and mail are MIME: their HTML (or text) part is what reaches the PDF.
		return findInString(mimeText(path), classify, skipBelow)
	case "txt", "md", "html", "csv", "tsv", "fb2", "rtf", "rst", "tex", "org", "adoc", "typ", "mediawiki", "dbk", "textile", "man", "opml":
		b, err := readAtMost(path, 64<<20)
		if err != nil {
			return ""
		}
		return findInString(doc.DecodeText(b), classify, skipBelow)
	}
	return "" // binary formats (HWP, DOC, MOBI, ...): left to the engines
}

func readAtMost(path string, n int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, n))
}

func isZip(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 4)
	n, _ := io.ReadFull(f, head)
	return n == 4 && bytes.Equal(head, []byte{0x50, 0x4B, 0x03, 0x04})
}

var characterReference = regexp.MustCompile(`&#(?:[xX]([0-9a-fA-F]{1,6})|([0-9]{1,7}));|\\u(-?[0-9]{1,5})`)

func findInString(text string, classify classifier, skipBelow rune) string {
	if s := find(text, classify, skipBelow); s != "" {
		return s
	}
	// &#2693; / &#x0A85; in HTML and XML, ⚓? in RTF.
	for _, m := range characterReference.FindAllStringSubmatch(text, -1) {
		var value int64
		switch {
		case m[1] != "":
			value, _ = strconv.ParseInt(m[1], 16, 64)
		case m[2] != "":
			value, _ = strconv.ParseInt(m[2], 10, 64)
		default:
			value, _ = strconv.ParseInt(m[3], 10, 64)
		}
		if value < 0 {
			value += 0x10000 // RTF writes code points above 32767 as negative numbers
		}
		if value < 0 || value > 0x10FFFF {
			continue // a reference can name a value outside Unicode: it is no script
		}
		if s := classify(rune(value), unknownNeighbour, unknownNeighbour); s != "" {
			return s
		}
	}
	return ""
}

// isContentPart tells the parts of a package that hold its text (not styles, themes or font tables).
func isContentPart(name, format string) bool {
	lower := strings.ToLower(name)
	xml := func(folder, prefix string) bool {
		f := strings.ToLower(folder + prefix)
		return strings.HasPrefix(lower, f) && strings.HasSuffix(lower, ".xml") && !strings.Contains(lower[len(folder):], "/")
	}
	switch format {
	case "docx", "docm", "dotx", "dotm":
		return xml("word/", "document") || xml("word/", "header") || xml("word/", "footer") || xml("word/", "footnotes") || xml("word/", "endnotes")
	case "pptx", "pptm", "potx", "ppsx":
		return xml("ppt/slides/", "slide")
	case "xlsx", "xlsm", "xltx":
		return xml("xl/", "sharedStrings") || xml("xl/worksheets/", "sheet")
	case "hwpx":
		return xml("Contents/", "section")
	case "odt", "ods", "odp":
		return lower == "content.xml"
	case "ofd":
		return strings.HasSuffix(lower, "/content.xml")
	}
	// EPUB, HTMLZ, TXTZ, ...: the chapters.
	return !strings.HasPrefix(lower, "meta-inf/") && (strings.HasSuffix(lower, ".xhtml") || strings.HasSuffix(lower, ".html") ||
		strings.HasSuffix(lower, ".htm") || strings.HasSuffix(lower, ".txt"))
}

func findInPackage(path, format string, classify classifier, skipBelow rune) string {
	z, err := zip.OpenReader(path)
	if err != nil {
		return ""
	}
	defer z.Close()
	for _, f := range z.File {
		if !isContentPart(f.Name, format) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(rc, 256<<20))
		rc.Close()
		if s := find(strings.TrimPrefix(string(b), "\ufeff"), classify, skipBelow); s != "" {
			return s
		}
	}
	return ""
}

// mimeText is the HTML body of a MIME message (else its text body).
func mimeText(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	m, err := mail.ReadMessage(f)
	if err != nil {
		return ""
	}
	htmlBody, textBody := "", ""
	var walk func(header map[string][]string, body io.Reader, depth int)
	walk = func(header map[string][]string, body io.Reader, depth int) {
		get := func(k string) string {
			for key, v := range header {
				if strings.EqualFold(key, k) && len(v) > 0 {
					return v[0]
				}
			}
			return ""
		}
		mediaType, params, err := mime.ParseMediaType(get("Content-Type"))
		if err != nil {
			mediaType = "text/plain"
		}
		if strings.HasPrefix(mediaType, "multipart/") && depth < 10 {
			r := multipart.NewReader(body, params["boundary"])
			for {
				p, err := r.NextPart()
				if err != nil {
					return
				}
				walk(p.Header, p, depth+1)
			}
		}
		if mediaType != "text/html" && mediaType != "text/plain" {
			return
		}
		var data []byte
		switch strings.ToLower(get("Content-Transfer-Encoding")) {
		case "base64":
			data, _ = io.ReadAll(base64.NewDecoder(base64.StdEncoding, body))
		case "quoted-printable":
			data, _ = io.ReadAll(quotedprintable.NewReader(body))
		default:
			data, _ = io.ReadAll(body)
		}
		text := string(data)
		if cs := params["charset"]; cs != "" {
			if e, err := htmlindex.Get(cs); err == nil {
				if d, err := e.NewDecoder().Bytes(data); err == nil {
					text = string(d)
				}
			}
		}
		if mediaType == "text/html" && htmlBody == "" {
			htmlBody = text
		} else if mediaType == "text/plain" && textBody == "" {
			textBody = text
		}
	}
	walk(m.Header, m.Body, 0)
	if htmlBody != "" {
		return htmlBody
	}
	return textBody
}
