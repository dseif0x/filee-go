package cad

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// Text in a DWG or DXF before AutoCAD 2007 is stored in the drawing's code
// page ($DWGCODEPAGE), not UTF-8 (#489). A 2007+ drawing often still says
// ANSI_1252 while holding Korean or Chinese text, so the code page is picked
// from the text; what it can't hold the writer escapes as \U+XXXX.

// codePageCandidates in the order a tie goes after the drawing's own one. The
// East Asian pages come first only for the script they are for.
var codePageCandidates = []int{1252, 1250, 1251, 1253, 1254, 1255, 1256, 1257, 1258, 874, 950, 936, 949, 932}

func codePageEncoding(page int) encoding.Encoding {
	switch page {
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
	case 874:
		return charmap.Windows874
	case 950:
		return traditionalchinese.Big5
	case 936:
		return simplifiedchinese.GBK
	case 949:
		return korean.EUCKR
	case 932:
		return japanese.ShiftJIS
	}
	return charmap.Windows1252
}

// PrepareCodePage gives a drawing written in a pre-2007 version the code page
// that holds most of its text (its own one on a tie), and returns it.
func PrepareCodePage(doc *cadoc.Document) int {
	page := ChooseCodePage(documentTexts(doc), codePageNumber(doc.Header.CodePage))
	doc.Header.CodePage = fmt.Sprintf("ANSI_%d", page)
	return page
}

// ChooseCodePage is the candidate holding the most distinct non-ASCII characters.
func ChooseCodePage(texts []string, current int) int {
	chars := map[rune]bool{}
	for _, t := range texts {
		for _, r := range t {
			if r > 0x7F {
				chars[r] = true
			}
		}
	}
	var first []int
	if current != 0 && slices.Contains(codePageCandidates, current) {
		first = append(first, current)
	}
	has := func(f func(rune) bool) bool {
		for r := range chars {
			if f(r) {
				return true
			}
		}
		return false
	}
	if has(func(r rune) bool {
		return r >= '가' && r <= '힣' || r >= 'ᄀ' && r <= 'ᇿ' || r >= '㄰' && r <= '㆏'
	}) {
		first = append(first, 949)
	}
	if has(func(r rune) bool { return r >= '぀' && r <= 'ヿ' }) {
		first = append(first, 932)
	}
	if has(func(r rune) bool {
		return r >= '一' && r <= '鿿' || r >= '㐀' && r <= '䶿' || r >= '豈' && r <= '﫿'
	}) {
		first = append(first, 950, 936)
	}
	order := append([]int(nil), first...)
	for _, c := range codePageCandidates {
		if !slices.Contains(order, c) {
			order = append(order, c)
		}
	}
	best, bestHeld := order[0], -1
	for _, page := range order {
		enc := codePageEncoding(page)
		held := 0
		for r := range chars {
			if holds(enc, r) {
				held++
			}
		}
		if held > bestHeld {
			best, bestHeld = page, held
		}
	}
	return best
}

func holds(enc encoding.Encoding, r rune) bool {
	s := string(r)
	b, err := enc.NewEncoder().String(s)
	if err != nil {
		return false
	}
	back, err := enc.NewDecoder().String(b)
	return err == nil && back == s
}

// codePageNumber maps a $DWGCODEPAGE name to its number (0 when unknown).
func codePageNumber(name string) int {
	u := strings.ToUpper(strings.TrimSpace(name))
	switch u {
	case "KCS5601", "JOHAB":
		return 949
	case "GB2312":
		return 936
	case "BIG5":
		return 950
	case "DOS932", "SHIFT_JIS":
		return 932
	}
	if strings.HasPrefix(u, "ANSI_") {
		if n, err := strconv.Atoi(u[5:]); err == nil {
			return n
		}
	}
	return 0
}

// documentTexts is every text the drawing holds in its code page: TEXT and
// attributes, MTEXT, dimension text, and the names of layers, blocks, styles,
// line types and layouts.
func documentTexts(doc *cadoc.Document) []string {
	var out []string
	for l := range doc.Layers.All() {
		out = append(out, l.Name)
	}
	for s := range doc.TextStyles.All() {
		out = append(out, s.Name)
	}
	for s := range doc.DimensionStyles.All() {
		out = append(out, s.Name)
	}
	for t := range doc.LineTypes.All() {
		out = append(out, t.Name)
	}
	for _, l := range doc.Layouts() {
		out = append(out, l.Name)
	}
	for r := range doc.BlockRecords.All() {
		if !strings.HasPrefix(r.Name, "*") {
			out = append(out, r.Name)
		}
		for _, e := range r.Entities {
			switch x := e.(type) {
			case *cadoc.AttributeDefinition:
				out = append(out, x.Value, x.Tag)
			case *cadoc.AttributeEntity:
				out = append(out, x.Value, x.Tag)
			case *cadoc.TextEntity:
				out = append(out, x.Value)
			case *cadoc.MText:
				out = append(out, x.Value)
			case cadoc.Dimension:
				out = append(out, x.Dimension().Text)
			case *cadoc.Insert:
				for _, a := range x.Attributes {
					out = append(out, a.Value, a.Tag)
				}
			}
		}
	}
	return out
}
