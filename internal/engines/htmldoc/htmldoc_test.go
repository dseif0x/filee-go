package htmldoc

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/unicode"

	"github.com/dseif0x/filee-go/internal/doc"
)

func parse(t *testing.T, s string, folder string) *Content {
	t.Helper()
	if folder == "" {
		folder = t.TempDir()
	}
	return Parse(s, ReadOptions{BaseFolder: folder, MediaFolder: filepath.Join(folder, "media")}, false)
}

func paragraphs(c *Content) []*doc.Paragraph {
	var out []*doc.Paragraph
	for _, b := range c.Blocks {
		if p, ok := b.(*doc.Paragraph); ok {
			out = append(out, p)
		}
	}
	return out
}

func run(t *testing.T, c *Content, text string) *doc.Text {
	t.Helper()
	for _, in := range doc.Inlines(c.Blocks) {
		if tx, ok := in.(*doc.Text); ok && strings.Contains(tx.Text, text) {
			return tx
		}
	}
	t.Fatalf("no run %q", text)
	return nil
}

func byText(t *testing.T, ps []*doc.Paragraph, text string) *doc.Paragraph {
	t.Helper()
	for _, p := range ps {
		if Text(p.Inlines) == text {
			return p
		}
	}
	t.Fatalf("no paragraph %q", text)
	return nil
}

// PNG is a w×h picture of one colour.
func PNG(w, h int, c color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func TestHeadingsParagraphsAndInlineFormatting(t *testing.T) {
	c := parse(t, `<html><head><title>제목 </title><style>.x{font-weight:bold} p.c{text-align:center} body{color:red}</style>
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
	if c.Title != "제목" {
		t.Errorf("title %q", c.Title)
	}
	ps := paragraphs(c)
	if ps[0].HeadingLevel != 1 || Text(ps[0].Inlines) != "큰 제목" || ps[1].HeadingLevel != 3 {
		t.Errorf("headings %d %q %d", ps[0].HeadingLevel, Text(ps[0].Inlines), ps[1].HeadingLevel)
	}
	is := func(p *bool) bool { return p != nil && *p }
	if !is(run(t, c, "굵게").Format.Bold) || !is(run(t, c, "기울임").Format.Italic) || !is(run(t, c, "밑줄").Format.Underline) ||
		!is(run(t, c, "취소").Format.Strike) || !is(run(t, c, "2").Format.Subscript) {
		t.Error("inline formats")
	}
	if doc.Val(run(t, c, "code").Format.Shade, "") != CodeShade || doc.Val(run(t, c, "표시").Format.Shade, "") != MarkShade {
		t.Error("shades")
	}
	red := run(t, c, "빨강").Format
	if doc.Val(red.Color, "") != "#FF0000" || doc.Val(red.Size, 0) != 2000 || !is(red.Italic) {
		t.Errorf("red %+v", red)
	}
	if !is(run(t, c, "클래스").Format.Bold) {
		t.Error("style sheet class rule")
	}
	if run(t, c, "일반").Format.Color != nil {
		t.Error("body colour copied onto runs")
	}
	if doc.Val(byText(t, ps, "가운데").Format.Align, doc.AlignLeft) != doc.AlignCenter || doc.Val(byText(t, ps, "오른쪽").Format.Align, doc.AlignLeft) != doc.AlignRight {
		t.Error("alignment")
	}
	var all []string
	for _, p := range ps {
		all = append(all, Text(p.Inlines))
	}
	joined := strings.Join(all, "\n")
	for _, no := range []string{"메뉴", "숨김", "document.write"} {
		if strings.Contains(joined, no) {
			t.Errorf("%s was read", no)
		}
	}
}

func TestWhitespaceCollapsesAndLineBreaksStay(t *testing.T) {
	ps := paragraphs(parse(t, "<p>  하나   \n  둘<br>셋<br></p><p><br></p><p></p><div>\n</div><pre>  들여쓴\n\n코드</pre>", ""))
	var texts []string
	breaks := 0
	for _, in := range ps[0].Inlines {
		switch in := in.(type) {
		case *doc.Text:
			texts = append(texts, in.Text)
		case *doc.LineBreak:
			breaks++
		}
	}
	if !slices.Equal(texts, []string{"하나 둘", "셋"}) || breaks != 1 {
		t.Fatalf("%q %d", texts, breaks)
	}
	if len(ps[1].Inlines) != 0 {
		t.Fatal("<p><br></p> is an empty line")
	}
	var code []string
	for _, p := range ps[2:] {
		code = append(code, codeTextOf(p))
		for _, in := range p.Inlines {
			if tx, ok := in.(*doc.Text); ok && doc.Val(tx.Format.Shade, "") != CodeShade {
				t.Error("code shade")
			}
		}
	}
	if !slices.Equal(code, []string{"  들여쓴", "", "코드"}) {
		t.Fatalf("%q", code)
	}
}

func codeTextOf(p *doc.Paragraph) string {
	var sb strings.Builder
	for _, in := range p.Inlines {
		if tx, ok := in.(*doc.Text); ok {
			sb.WriteString(tx.Text)
		}
	}
	return sb.String()
}

func TestNestedListsKeepLevelsStartNumbersAndTypes(t *testing.T) {
	ps := paragraphs(parse(t, `<ol start="3" type="a">
  <li>셋째<ul><li>안쪽</li><li>안쪽 둘</li></ul>이어지는 글</li>
  <li><p>넷째</p><p>둘째 문단</p></li>
</ol>
<ul style="list-style:none"><li>표시 없음</li></ul>`, ""))
	third := byText(t, ps, "셋째")
	if third.List == nil || third.List.Level != 0 || !third.List.Numbered {
		t.Fatalf("%+v", third.List)
	}
	l0 := third.List.Numbering.Levels[0]
	if l0.Format != "LATIN_SMALL" || l0.Start != 3 || l0.Bullet {
		t.Errorf("%+v", l0)
	}
	inner := byText(t, ps, "안쪽")
	if inner.List.Level != 1 || !inner.List.Numbering.Levels[1].Bullet || byText(t, ps, "안쪽 둘").List.Numbering != inner.List.Numbering {
		t.Error("inner list")
	}
	cont := byText(t, ps, "이어지는 글")
	if cont.List == nil || cont.List.Level != 0 || cont.List.Numbered || cont.List.Numbering != third.List.Numbering {
		t.Errorf("continued %+v", cont.List)
	}
	if !byText(t, ps, "넷째").List.Numbered || byText(t, ps, "둘째 문단").List.Numbered {
		t.Error("item paragraphs")
	}
	un := byText(t, ps, "표시 없음")
	if un.List != nil || doc.Val(un.Format.Left, 0) <= 0 {
		t.Error("unmarked list")
	}
}

func TestTablesKeepSpansHeaderRowsAndBorders(t *testing.T) {
	c := parse(t, `<table border="0">
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
	tb := tables[0]
	if tb.ColumnCount != 3 || !tb.Rows[0].Header || tb.Rows[1].Header || tb.Rows[2].Header {
		t.Fatalf("columns %d", tb.ColumnCount)
	}
	if !doc.Val(tb.Rows[0].Cells[0].Blocks[0].(*doc.Paragraph).Inlines[0].(*doc.Text).Format.Bold, false) {
		t.Error("header bold")
	}
	if tb.Rows[0].Cells[1].ColSpan != 2 || tb.Rows[1].Cells[0].RowSpan != 2 || tb.Rows[1].Cells[0].Fill != "#EEEEEE" {
		t.Error("spans or fill")
	}
	if tb.Borders == nil || *tb.Borders != doc.EmptyBorders || Text(tb.Caption[0].(*doc.Paragraph).Inlines) != "표 제목" {
		t.Error("borders or caption")
	}
	if tables[1].Borders != nil || !slices.Equal(tables[1].RelativeWidths, []float64{0.25, 0.75}) {
		t.Errorf("second table %v", tables[1].RelativeWidths)
	}
}

func TestPicturesLinksAndAnchors(t *testing.T) {
	folder := t.TempDir()
	os.MkdirAll(filepath.Join(folder, "img"), 0o755)
	os.WriteFile(filepath.Join(folder, "img", "로고 1.png"), PNG(40, 20, color.RGBA{0, 128, 128, 255}), 0o644)
	data := base64.StdEncoding.EncodeToString(PNG(10, 10, color.RGBA{255, 0, 0, 255}))
	c := parse(t, `<p><a href="#part2">둘째로</a> <a href="https://example.com/x?a=1">웹</a> <a name="unused">아무도</a></p>
<p><img src="img/%EB%A1%9C%EA%B3%A0%201.png" width="200"> <img src="data:image/png;base64,`+data+`"> <img src="https://example.com/remote.png"></p>
<h2 id="part2">둘째 부분</h2>`, folder)
	d := ToDocument(c)
	var links []string
	var bookmarks []string
	var images []*doc.Image
	for _, in := range doc.Inlines(d.Sections[0].Blocks) {
		switch in := in.(type) {
		case *doc.Link:
			links = append(links, in.Target)
		case *doc.Bookmark:
			bookmarks = append(bookmarks, in.Name)
		case *doc.Image:
			images = append(images, in)
		}
	}
	if !slices.Equal(links, []string{"#part2", "https://example.com/x?a=1"}) || !slices.Equal(bookmarks, []string{"part2"}) {
		t.Fatalf("%v %v", links, bookmarks)
	}
	if len(images) != 2 || images[0].Path != filepath.Join(folder, "img", "로고 1.png") || doc.Val(images[0].Width, 0) != 15000 {
		t.Fatalf("images %+v", images)
	}
	if !strings.HasPrefix(images[1].Path, filepath.Join(folder, "media")) {
		t.Fatal(images[1].Path)
	}
}

func TestCssPageBreaks(t *testing.T) {
	ps := paragraphs(parse(t, `<p style="page-break-before:always">첫</p>
<p>둘</p>
<div style="break-after: page"><p>셋</p></div>
<p>넷</p>
<mbp:pagebreak/>
<p>다섯</p>`, ""))
	var got []bool
	for _, p := range ps {
		got = append(got, p.PageBreakBefore)
	}
	if !slices.Equal(got, []bool{false, false, false, true, true}) {
		t.Fatal(got)
	}
}

func TestEncodings(t *testing.T) {
	const ko, ru = "한글 문서", "Русский текст"
	euc, _ := korean.EUCKR.NewEncoder().String(`<html><head><meta charset="euc-kr"></head><body>` + ko + `</body></html>`)
	ks, _ := korean.EUCKR.NewEncoder().String(`<meta http-equiv="Content-Type" content="text/html; charset=ks_c_5601-1987"><p>` + ko + `</p>`)
	cyr, _ := charmap.Windows1251.NewEncoder().String(`<?xml version="1.0" encoding="windows-1251"?><html><body>` + ru + `</body></html>`)
	u16, _ := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder().String("<p>" + ko + "</p>")
	for i, c := range []struct {
		b    string
		want string
	}{{euc, ko}, {ks, ko}, {cyr, ru}, {u16, ko}, {"<p>" + ko + "</p>", ko}} {
		if !strings.Contains(Decode([]byte(c.b)), c.want) {
			t.Errorf("%d: %q", i, Decode([]byte(c.b)))
		}
	}
}

func TestXhtmlSelfClosedElements(t *testing.T) {
	c := Parse(`<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title/></head>
<body><div id="a"/><p>첫 문단</p><a id="b"/><p>둘째 문단</p><br/><hr/></body></html>`, ReadOptions{BaseFolder: t.TempDir(), MediaFolder: t.TempDir()}, true)
	ps := paragraphs(c)
	if Text(ps[0].Inlines) != "첫 문단" || Text(ps[1].Inlines) != "둘째 문단" {
		t.Fatalf("%q %q", Text(ps[0].Inlines), Text(ps[1].Inlines))
	}
	for _, in := range ps[1].Inlines {
		if _, ok := in.(*doc.Link); ok {
			t.Error("link swallowed the paragraph")
		}
	}
	last := ps[len(ps)-1]
	if _, ok := last.Inlines[0].(*doc.Shape); !ok || len(last.Inlines) != 1 {
		t.Error("<hr/> is a short line")
	}
}

func TestDeepHtmlIsReadAsText(t *testing.T) {
	s := strings.Repeat("<div><span>", 5000) + "깊은 글" + strings.Repeat("</span></div>", 5000)
	found := false
	for _, p := range paragraphs(parse(t, s, "")) {
		if Text(p.Inlines) == "깊은 글" {
			found = true
		}
	}
	if !found {
		t.Fatal("deep text lost")
	}
}

func wellFormed(t *testing.T, s string) {
	t.Helper()
	d := xml.NewDecoder(strings.NewReader(s))
	for {
		_, err := d.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("not well-formed: %v\n%s", err, s)
		}
	}
}

func TestXhtmlWriterIsWellFormedAndLinksAcrossChapters(t *testing.T) {
	c := parse(t, `<h1>하나</h1><p>본문 <a href="#target">링크</a> <b>굵게</b> &amp; &lt;기호&gt;</p>
<ol start="2"><li>둘<ul><li>안</li></ul></li><li>셋</li></ol>
<table><tr><th>A</th><th>B</th></tr><tr><td colspan="2">합침</td></tr></table>
<h1>둘</h1><p id="target">목표</p><pre>code  line</pre>`, "")
	s := doc.NewSection()
	s.Blocks = c.Blocks
	d := &doc.Document{Sections: []*doc.Section{s}}
	p := s.Blocks[1].(*doc.Paragraph)
	p.Inlines = append(p.Inlines, &doc.Note{Blocks: []doc.Block{&doc.Paragraph{Inlines: []doc.Inline{&doc.Text{Text: "각주"}}}}})
	chapters := WriteXhtml(d, XhtmlOptions{SplitChapters: true, Epub: true})
	if len(chapters) != 2 || chapters[0].Title != "하나" || chapters[1].Title != "둘" {
		t.Fatalf("%d chapters", len(chapters))
	}
	for _, ch := range chapters {
		wellFormed(t, Page(ch.Title, ch.Body, "ko", "style.css", true, ""))
	}
	b0, b1 := chapters[0].Body, chapters[1].Body
	for _, want := range []string{`href="ch002.xhtml#target"`, `<ol start="2">`, `colspan="2"`, `epub:type="footnote"`} {
		if !strings.Contains(b0, want) {
			t.Errorf("chapter 1 lacks %s:\n%s", want, b0)
		}
	}
	if !regexp.MustCompile(`<li>둘<ul>\s*<li>안</li>\s*</ul>\s*</li>`).MatchString(b0) {
		t.Errorf("nested list:\n%s", b0)
	}
	if !strings.Contains(b1, `id="target"`) || !strings.Contains(b1, "<pre><code>code  line</code></pre>") {
		t.Errorf("chapter 2:\n%s", b1)
	}
}

func TestMarkdownAndPlainTextWriters(t *testing.T) {
	c := parse(t, `<h2>제목</h2><p>본문 <b>굵게</b> <a href="https://x.org">링크</a></p><ul><li>하나</li><li>둘</li></ul>
<ol><li>첫</li><li>둘째</li></ol><blockquote><p>인용</p></blockquote>
<table><tr><th>A</th><th>B</th></tr><tr><td>1</td><td>2</td></tr></table><pre>x = 1</pre><hr/>`, "")
	d := ToDocument(c)
	md := Markdown(d, func(string) string { return "" })
	for _, want := range []string{"## 제목", "**굵게**", "[링크](<https://x.org>)", "- 하나\n- 둘", "1. 첫\n2. 둘째", "> 인용", "| A | B |\n|---|---|\n| 1 | 2 |", "```\nx = 1\n```", "---"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	txt := PlainText(d, "\n")
	for _, want := range []string{"제목\n\n본문 굵게 링크", "● 하나\n● 둘", "1. 첫\n2. 둘째", "A\tB\n1\t2", "* * *"} {
		if !strings.Contains(txt, want) {
			t.Errorf("text lacks %q:\n%s", want, txt)
		}
	}
}
