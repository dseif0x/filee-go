package hwpx

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/korean"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/hwp/xmltree"
)

// The built-in HTML reader and XHTML writer: structure, formatting, lists, tables, pictures, links, encodings and
// XHTML quirks, and HTML → HWPX without Pandoc.

func parseHTML(t *testing.T, text, folder string) *HTMLContent {
	if folder == "" {
		folder = t.TempDir()
	}
	return ParseHTML(text, HTMLReadOptions{BaseFolder: folder, MediaFolder: filepath.Join(folder, "media")}, false)
}

func paragraphsOf(c *HTMLContent) []*doc.Paragraph {
	var out []*doc.Paragraph
	for _, b := range c.Blocks {
		if p, ok := b.(*doc.Paragraph); ok {
			out = append(out, p)
		}
	}
	return out
}

func runOf(t *testing.T, c *HTMLContent, text string) *doc.Text {
	t.Helper()
	for _, in := range doc.Inlines(c.Blocks) {
		if tx, ok := in.(*doc.Text); ok && strings.Contains(tx.Text, text) {
			return tx
		}
	}
	t.Fatalf("no run %q", text)
	return nil
}

func paragraphText(t *testing.T, ps []*doc.Paragraph, text string) *doc.Paragraph {
	t.Helper()
	for _, p := range ps {
		if doc.PlainText(p.Inlines) == text {
			return p
		}
	}
	t.Fatalf("no paragraph %q", text)
	return nil
}

func TestHTMLHeadingsParagraphsAndInlineFormatting(t *testing.T) {
	c := parseHTML(t, `<html><head><title>제목 </title><style>.x{font-weight:bold} p.c{text-align:center} body{color:red}</style>
<script>document.write('no')</script></head>
<body>
<h1>큰   제목</h1>
<h3>작은 제목</h3>
<p>일반 <b>굵게</b> <i>기울임</i> <u>밑줄</u> <del>취소</del> H<sub>2</sub>O x<sup>2</sup> <code>code</code> <mark>표시</mark></p>
<p><span style="color:#ff0000; font-size:20pt; font-style:italic">빨강</span> <span class="x">클래스</span></p>
<p class="c">가운데</p>
<p style="text-align:right">오른쪽</p>
<nav>메뉴</nav><div style="display:none">숨김</div>
</body></html>`, "")
	eq(t, "title", c.Title, "제목")
	ps := paragraphsOf(c)
	eq(t, "h1", ps[0].HeadingLevel, 1)
	eq(t, "h1 text", doc.PlainText(ps[0].Inlines), "큰 제목")
	eq(t, "h3", ps[1].HeadingLevel, 3)
	for word, check := range map[string]func(doc.CharFormat) bool{
		"굵게": func(f doc.CharFormat) bool { return isTrue(f.Bold) }, "기울임": func(f doc.CharFormat) bool { return isTrue(f.Italic) },
		"밑줄": func(f doc.CharFormat) bool { return isTrue(f.Underline) }, "취소": func(f doc.CharFormat) bool { return isTrue(f.Strike) },
		"2": func(f doc.CharFormat) bool { return isTrue(f.Subscript) }, "code": func(f doc.CharFormat) bool { return strIs(f.Shade, CodeShade) },
		"표시": func(f doc.CharFormat) bool { return strIs(f.Shade, MarkShade) }, "클래스": func(f doc.CharFormat) bool { return isTrue(f.Bold) },
		"일반": func(f doc.CharFormat) bool { return f.Color == nil },
	} {
		if !check(runOf(t, c, word).Format) {
			t.Errorf("format of %q: %s", word, charString(runOf(t, c, word).Format))
		}
	}
	red := runOf(t, c, "빨강").Format
	if val(red.Color, "") != "#FF0000" || val(red.Size, 0) != 2000 || !isTrue(red.Italic) {
		t.Errorf("red %s", charString(red))
	}
	eq(t, "centre", *paragraphText(t, ps, "가운데").Format.Align, doc.AlignCenter)
	eq(t, "right", *paragraphText(t, ps, "오른쪽").Format.Align, doc.AlignRight)
	var all []string
	for _, p := range ps {
		all = append(all, doc.PlainText(p.Inlines))
	}
	text := strings.Join(all, "\n")
	for _, hidden := range []string{"메뉴", "숨김", "document.write"} {
		if strings.Contains(text, hidden) {
			t.Errorf("%q is shown", hidden)
		}
	}
}

func TestHTMLWhitespaceCollapsesAndLineBreaksStay(t *testing.T) {
	ps := paragraphsOf(parseHTML(t, "<p>  하나   \n  둘<br>셋<br></p><p><br></p><p></p><div>\n</div><pre>  들여쓴\n\n코드</pre>", ""))
	var texts []string
	breaks := 0
	for _, in := range ps[0].Inlines {
		switch v := in.(type) {
		case *doc.Text:
			texts = append(texts, v.Text)
		case *doc.LineBreak:
			breaks++
		}
	}
	eq(t, "texts", strings.Join(texts, "|"), "하나 둘|셋")
	eq(t, "breaks", breaks, 1)
	eq(t, "empty line", len(ps[1].Inlines), 0)
	var code []string
	for _, p := range ps[2:] {
		code = append(code, codeText(p))
		for _, in := range p.Inlines {
			if !strIs(in.(*doc.Text).Format.Shade, CodeShade) {
				t.Error("code not shaded")
			}
		}
	}
	eq(t, "code", strings.Join(code, "|"), "  들여쓴||코드")
}

func TestHTMLNestedListsKeepLevelsStartNumbersAndTypes(t *testing.T) {
	ps := paragraphsOf(parseHTML(t, `<ol start="3" type="a">
  <li>셋째<ul><li>안쪽</li><li>안쪽 둘</li></ul>이어지는 글</li>
  <li><p>넷째</p><p>둘째 문단</p></li>
</ol>
<ul style="list-style:none"><li>표시 없음</li></ul>`, ""))
	third := paragraphText(t, ps, "셋째")
	if third.List.Level != 0 || !third.List.Numbered {
		t.Errorf("third %+v", third.List)
	}
	l0 := third.List.Numbering.Levels[0]
	eq(t, "level 0", l0.Format+" "+itoa(l0.Start), "LATIN_SMALL 3")
	inner := paragraphText(t, ps, "안쪽")
	if inner.List.Level != 1 || !inner.List.Numbering.Levels[1].Bullet {
		t.Errorf("inner %+v", inner.List)
	}
	if paragraphText(t, ps, "안쪽 둘").List.Numbering != inner.List.Numbering {
		t.Error("inner items do not share their list")
	}
	continued := paragraphText(t, ps, "이어지는 글")
	if continued.List.Level != 0 || continued.List.Numbered || continued.List.Numbering != third.List.Numbering {
		t.Errorf("continued %+v", continued.List)
	}
	if !paragraphText(t, ps, "넷째").List.Numbered || paragraphText(t, ps, "둘째 문단").List.Numbered {
		t.Error("item paragraphs")
	}
	unmarked := paragraphText(t, ps, "표시 없음")
	if unmarked.List != nil || val(unmarked.Format.Left, 0) <= 0 {
		t.Error("unmarked list")
	}
}

func TestHTMLTablesKeepSpansHeaderRowsAndBorders(t *testing.T) {
	c := parseHTML(t, `<table border="0">
  <caption>표 제목</caption>
  <thead><tr><th>이름</th><th colspan="2">값</th></tr></thead>
  <tbody>
    <tr><td rowspan="2" bgcolor="#eeeeee">병합</td><td>1</td><td>2</td></tr>
    <tr><td>3</td><td>4</td></tr>
  </tbody>
</table>
<table><tr><td style="width:25%">a</td><td style="width:75%">b</td></tr></table>`, "")
	var tables []*doc.Table
	for _, b := range c.Blocks {
		if tb, ok := b.(*doc.Table); ok {
			tables = append(tables, tb)
		}
	}
	table := tables[0]
	eq(t, "columns", table.ColumnCount, 3)
	if !table.Rows[0].Header || table.Rows[1].Header || table.Rows[2].Header {
		t.Error("header rows")
	}
	if !isTrue(table.Rows[0].Cells[0].Blocks[0].(*doc.Paragraph).Inlines[0].(*doc.Text).Format.Bold) {
		t.Error("header not bold")
	}
	eq(t, "colspan", table.Rows[0].Cells[1].ColSpan, 2)
	eq(t, "rowspan", table.Rows[1].Cells[0].RowSpan, 2)
	eq(t, "fill", table.Rows[1].Cells[0].Fill, "#EEEEEE")
	eq(t, "borders", *table.Borders, doc.EmptyBorders)
	eq(t, "caption", allText(table.Caption), "표 제목")
	if tables[1].Borders != nil {
		t.Error("default grid expected")
	}
	eq(t, "relative", len(tables[1].RelativeWidths), 2)
	if tables[1].RelativeWidths[0] != 0.25 || tables[1].RelativeWidths[1] != 0.75 {
		t.Errorf("relative widths %v", tables[1].RelativeWidths)
	}
}

func TestHTMLPicturesFromFilesAndDataURIsLinksAndAnchors(t *testing.T) {
	folder := t.TempDir()
	os.MkdirAll(filepath.Join(folder, "img"), 0o755)
	os.WriteFile(filepath.Join(folder, "img", "로고 1.png"), pngBytes(40, 20), 0o644)
	data := base64.StdEncoding.EncodeToString(pngBytes(10, 10))
	c := parseHTML(t, `<p><a href="#part2">둘째로</a> <a href="https://example.com/x?a=1">웹</a> <a name="unused">아무도</a></p>
<p><img src="img/%EB%A1%9C%EA%B3%A0%201.png" width="200"> <img src="data:image/png;base64,`+data+`"> <img src="https://example.com/remote.png"></p>
<h2 id="part2">둘째 부분</h2>`, folder)
	d := HTMLDocument(c)
	var links, bookmarks []string
	var images []*doc.Image
	for _, in := range doc.Inlines(d.Sections[0].Blocks) {
		switch v := in.(type) {
		case *doc.Link:
			links = append(links, v.Target)
		case *doc.Bookmark:
			bookmarks = append(bookmarks, v.Name)
		case *doc.Image:
			images = append(images, v)
		}
	}
	eq(t, "links", strings.Join(links, ","), "#part2,https://example.com/x?a=1")
	eq(t, "bookmarks", strings.Join(bookmarks, ","), "part2")
	if len(images) != 2 {
		t.Fatalf("%d images", len(images))
	}
	eq(t, "path", images[0].Path, filepath.Join(folder, "img", "로고 1.png"))
	eq(t, "width", *images[0].Width, int(200*doc.PerPixel))
	if !fileExists(images[1].Path) || !strings.HasPrefix(images[1].Path, filepath.Join(folder, "media")) {
		t.Errorf("data URI picture %s", images[1].Path)
	}
}

func TestHTMLCSSPageBreaksStartNewPages(t *testing.T) {
	ps := paragraphsOf(parseHTML(t, `<p style="page-break-before:always">첫</p>
<p>둘</p>
<div style="break-after: page"><p>셋</p></div>
<p>넷</p>
<mbp:pagebreak/>
<p>다섯</p>`, ""))
	var breaks []bool
	for _, p := range ps {
		breaks = append(breaks, p.PageBreakBefore)
	}
	eq(t, "breaks", fmtBools(breaks), "false false false true true")
}

func fmtBools(b []bool) string {
	s := make([]string, len(b))
	for i, v := range b {
		if v {
			s[i] = "true"
		} else {
			s[i] = "false"
		}
	}
	return strings.Join(s, " ")
}

func TestHTMLEncodings(t *testing.T) {
	const ko, ru = "한글 문서", "Русский текст"
	enc := func(e interface{ Bytes([]byte) ([]byte, error) }, s string) []byte {
		b, err := e.Bytes([]byte(s))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	cp949 := korean.EUCKR.NewEncoder()
	if !strings.Contains(DecodeHTML(enc(cp949, `<html><head><meta charset="euc-kr"></head><body>`+ko+`</body></html>`)), ko) {
		t.Error("meta charset")
	}
	if !strings.Contains(DecodeHTML(enc(korean.EUCKR.NewEncoder(), `<meta http-equiv="Content-Type" content="text/html; charset=ks_c_5601-1987"><p>`+ko+`</p>`)), ko) {
		t.Error("ks_c_5601-1987")
	}
	if !strings.Contains(DecodeHTML(enc(charmap.Windows1251.NewEncoder(), `<?xml version="1.0" encoding="windows-1251"?><html><body>`+ru+`</body></html>`)), ru) {
		t.Error("xml declaration")
	}
	var utf16 []byte
	utf16 = append(utf16, 0xFF, 0xFE)
	for _, r := range "<p>" + ko + "</p>" {
		utf16 = append(utf16, byte(r), byte(r>>8))
	}
	if !strings.Contains(DecodeHTML(utf16), ko) {
		t.Error("UTF-16 BOM")
	}
	if !strings.Contains(DecodeHTML([]byte("<p>"+ko+"</p>")), ko) {
		t.Error("undeclared UTF-8")
	}
}

func TestXhtmlSelfClosedElementsDoNotSwallowThePage(t *testing.T) {
	c := ParseHTML(`<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title/></head>
<body><div id="a"/><p>첫 문단</p><a id="b"/><p>둘째 문단</p><br/><hr/></body></html>`, HTMLReadOptions{BaseFolder: t.TempDir(), MediaFolder: t.TempDir()}, true)
	ps := paragraphsOf(c)
	eq(t, "first", doc.PlainText(ps[0].Inlines), "첫 문단")
	eq(t, "second", doc.PlainText(ps[1].Inlines), "둘째 문단")
	for _, in := range ps[1].Inlines {
		if _, ok := in.(*doc.Link); ok {
			t.Error("link swallowed the paragraph")
		}
	}
	last := ps[len(ps)-1].Inlines
	if _, ok := last[0].(*doc.Shape); len(last) != 1 || !ok {
		t.Error("<hr/> is not a short line")
	}
}

func TestAbsurdlyDeepHTMLIsReadAsText(t *testing.T) {
	html := strings.Repeat("<div><span>", 5000) + "깊은 글" + strings.Repeat("</span></div>", 5000)
	found := false
	for _, p := range paragraphsOf(parseHTML(t, html, "")) {
		found = found || doc.PlainText(p.Inlines) == "깊은 글"
	}
	if !found {
		t.Error("deep text lost")
	}
}

func TestXhtmlWriterOutputIsWellFormedAndLinksAcrossChapters(t *testing.T) {
	c := parseHTML(t, `<h1>하나</h1><p>본문 <a href="#target">링크</a> <b>굵게</b> &amp; &lt;기호&gt;</p>
<ol start="2"><li>둘<ul><li>안</li></ul></li><li>셋</li></ol>
<table><tr><th>A</th><th>B</th></tr><tr><td colspan="2">합침</td></tr></table>
<h1>둘</h1><p id="target">목표</p><pre>code  line</pre>`, "")
	s := doc.NewSection()
	s.Blocks = c.Blocks
	d := &doc.Document{Sections: []*doc.Section{s}}
	p := s.Blocks[1].(*doc.Paragraph)
	p.Inlines = append(p.Inlines, &doc.Note{Blocks: []doc.Block{para(txt("각주"))}})
	chapters := XhtmlWrite(d, XhtmlOptions{SplitChapters: true, Epub: true})
	if len(chapters) != 2 {
		t.Fatalf("%d chapters", len(chapters))
	}
	eq(t, "titles", chapters[0].Title+","+chapters[1].Title, "하나,둘")
	for _, ch := range chapters {
		page := XhtmlPage(ch.Title, ch.Body, "ko", "style.css", true, "")
		if _, err := xmltree.ParseString(strings.Replace(page, "<!DOCTYPE html>", "", 1)); err != nil {
			t.Errorf("not well-formed: %v\n%s", err, page)
		}
	}
	for _, want := range []string{`href="ch002.xhtml#target"`, `<ol start="2">`, `colspan="2"`, `epub:type="footnote"`} {
		if !strings.Contains(chapters[0].Body, want) {
			t.Errorf("chapter 1 lacks %s:\n%s", want, chapters[0].Body)
		}
	}
	if !regexp.MustCompile(`<li>둘<ul>\s*<li>안</li>\s*</ul>\s*</li>`).MatchString(chapters[0].Body) {
		t.Errorf("nested list:\n%s", chapters[0].Body)
	}
	if !strings.Contains(chapters[1].Body, `id="target"`) || !strings.Contains(chapters[1].Body, "<pre><code>code  line</code></pre>") {
		t.Errorf("chapter 2:\n%s", chapters[1].Body)
	}
}

func TestHTMLBecomesHwpx(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "chart.png"), pngBytes(300, 150), 0o644)
	page := filepath.Join(dir, "보고서.html")
	os.WriteFile(page, []byte(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>보고서</title></head><body>
<h1>분기 보고서</h1>
<p>매출이 <strong>12%</strong> 늘었습니다.</p>
<ul><li>서울</li><li>부산</li></ul>
<table><tr><th>지역</th><th>매출</th></tr><tr><td>서울</td><td>100</td></tr></table>
<p><img src="chart.png" alt="차트"></p>
<p style="page-break-before:always">둘째 쪽</p>
</body></html>`), 0o644)
	d, err := doc.Read(page, "html", filepath.Join(dir, "media"))
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "보고서.hwpx")
	if err := Write(d, out, nil); err != nil {
		t.Fatal(err)
	}
	read, err := ReadHwpx(out, filepath.Join(dir, "back"), nil)
	if err != nil {
		t.Fatal(err)
	}
	text := allText(read.Sections[0].Blocks)
	for _, want := range []string{"분기 보고서", "12%", "서울", "부산", "매출", "둘째 쪽"} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q", want)
		}
	}
	eq(t, "tables", len(section0(t, out).DescendantsNamed("tbl")), 1)
	eq(t, "pictures", strings.Count(zipEntry(t, out, "Contents/content.hpf"), "BinData/"), 1)
}

func TestMarkdownAndPlainTextWriters(t *testing.T) {
	c := parseHTML(t, `<h2>제목</h2><p>본문 <b>굵게</b> <code>x</code> <a href="https://e.com">링크</a></p>
<ol><li>하나</li><li>둘</li></ol><table><tr><th>A</th><th>B</th></tr><tr><td>1</td><td>2|3</td></tr></table><pre>code</pre><p>1. 번호처럼</p>`, "")
	s := doc.NewSection()
	s.Blocks = c.Blocks
	p := s.Blocks[1].(*doc.Paragraph)
	p.Inlines = append(p.Inlines, &doc.Note{Blocks: []doc.Block{para(txt("각주"))}})
	d := &doc.Document{Title: "문서", Sections: []*doc.Section{s}}
	md := MarkdownWrite(d, nil)
	for _, want := range []string{"---\ntitle: \"문서\"\n---", "## 제목", "**굵게**", "`x`", "[링크](<https://e.com>)", "1. 하나\n2. 둘", "| A | B |\n|---|---|\n| 1 | 2\\\\|3 |", "```\ncode\n```", "1\\. 번호처럼", "[^1]: 각주"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	text := PlainTextWrite(d)
	for _, want := range []string{"제목", "본문 굵게 x 링크[1]", "1. 하나\n2. 둘", "A\tB\n1\t2|3", "----\n[1] 각주"} {
		if !strings.Contains(strings.ReplaceAll(text, "\r\n", "\n"), want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
	numbers := NewListNumbers()
	n := &doc.Numbering{Levels: []doc.NumberingLevel{{Format: "HANGUL_SYLLABLE", Text: "^1.", Start: 1}, {Format: "ROMAN_SMALL", Text: "^1-^2)", Start: 4}}}
	var got []string
	for _, l := range []int{0, 1, 1, 0, 1} {
		got = append(got, numbers.Next(&doc.ListRef{Numbering: n, Level: l, Numbered: true}))
	}
	eq(t, "numbers", strings.Join(got, " "), "가. 가-iv) 가-v) 나. 나-iv)")
}
