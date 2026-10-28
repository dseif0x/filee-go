package ebook

import (
	"archive/zip"
	"context"
	"fmt"
	"image"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/htmldoc"
	"github.com/dseif0x/filee-go/internal/engines/pdf"
	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/presets"
)

var bg = context.Background()

func step(t *testing.T, input, from, to string, preset *presets.Preset) *convert.Step {
	t.Helper()
	if preset == nil {
		preset = presets.New("test", to)
	}
	dir := t.TempDir()
	base := formats.NameWithoutExtension(input)
	return &convert.Step{InputPath: input, From: from, To: to, Preset: preset, WorkDir: t.TempDir(),
		Output: convert.AllocatorFunc(func(ext, suffix string) string { return filepath.Join(dir, base+suffix+"."+ext) })}
}

func convertTo(t *testing.T, input, from, to string, preset *presets.Preset) string {
	t.Helper()
	out, err := New().Convert(bg, step(t, input, from, to, preset), nil)
	if err != nil {
		t.Fatalf("%s → %s: %v", from, to, err)
	}
	if len(out) != 1 {
		t.Fatalf("outputs %v", out)
	}
	return out[0]
}

func failure(t *testing.T, input, from, to string) string {
	t.Helper()
	_, err := New().Convert(bg, step(t, input, from, to, nil), nil)
	if err == nil {
		t.Fatalf("%s → %s did not fail", from, to)
	}
	return err.Error()
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func epubText(t *testing.T, epub string) string {
	t.Helper()
	b, err := readEpubBook(bg, epub, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return htmldoc.PlainText(b.Doc, "\n")
}

func containsAll(t *testing.T, what, text string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Errorf("%s lacks %q:\n%s", what, w, text)
		}
	}
}

func allInlines(d *doc.Document) []doc.Inline {
	var out []doc.Inline
	for _, s := range d.Sections {
		out = append(out, doc.Inlines(s.Blocks)...)
	}
	return out
}

// ---- routes ----

func TestEbookRoutesUseTheBuiltInEngine(t *testing.T) {
	p := convert.NewPlanner([]convert.Converter{New()}, nil)
	for _, c := range [][2]string{{"mobi", "epub"}, {"azw3", "epub"}, {"fb2", "epub"}, {"epub", "fb2"}, {"md", "epub"}, {"epub", "txt"}, {"cbr", "pdf"}, {"pdf", "cbz"}, {"azw4", "pdf"}, {"htmlz", "txtz"}, {"epub", "kepub"}, {"kepub", "epub"}, {"html", "md"}} {
		r := p.Plan(c[0], c[1], convert.Options{})
		if r == nil || len(r.Steps) != 1 {
			t.Errorf("%s → %s: %+v", c[0], c[1], r)
		}
	}
	if p.Plan("epub", "mobi", convert.Options{}) != nil {
		t.Error("EPUB → MOBI needs calibre")
	}
	for _, e := range New().Edges() {
		if formats.ByID(e.From) == nil || formats.ByID(e.To) == nil {
			t.Errorf("unknown format in %s → %s", e.From, e.To)
		}
	}
	for _, f := range []string{"epub", "kepub", "mobi", "azw3", "fb2", "htmlz", "txtz", "html"} {
		if !doc.HasReader(f) {
			t.Errorf("no document reader for %s", f)
		}
	}
}

// ---- EPUB ----

func TestChaptersOnlyIncludeImagesFromTheUnpackedBook(t *testing.T) {
	folder := t.TempDir()
	book := filepath.Join(folder, "book")
	os.MkdirAll(filepath.Join(book, "Text"), 0o755)
	os.MkdirAll(filepath.Join(book, "Images"), 0o755)
	inside := filepath.Join(book, "Images", "inside.png")
	outside := filepath.Join(folder, "private.png")
	sibling := filepath.Join(folder, "book-evil", "sibling.png") // shares the "book" prefix
	os.MkdirAll(filepath.Dir(sibling), 0o755)
	for _, p := range []string{inside, outside, sibling} {
		os.WriteFile(p, pngBytes(4, 4, teal), 0o644)
	}
	chapter := filepath.Join(book, "Text", "chapter.xhtml")
	fileURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(outside)}).String()
	os.WriteFile(chapter, []byte(`<html><body><p><img src="../Images/inside.png"><img src="../../private.png"><img src="../../book-evil/sibling.png"><img src="`+fileURL+`"></p></body></html>`), 0o644)
	d, err := readChapters(bg, []string{chapter}, filepath.Join(book, "~media"))
	if err != nil {
		t.Fatal(err)
	}
	images := doc.Images(d)
	if len(images) != 1 || images[0].Path != inside {
		t.Fatalf("%+v", images)
	}
}

func TestEpubReaderKeepsChaptersLinksPicturesAndMetadata(t *testing.T) {
	b, err := readEpubBook(bg, sampleEpub(t, t.TempDir()), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if b.Meta.Title != "별빛 이야기" || !slices.Equal(b.Meta.Authors, []string{"김작가"}) || b.Meta.Language != "ko" || !strings.HasSuffix(b.Meta.CoverImage, "cover.png") {
		t.Fatalf("%+v", b.Meta)
	}
	blocks := b.Doc.Sections[0].Blocks
	var headings []string
	var breaks []bool
	tables := 0
	for _, bl := range blocks {
		switch bl := bl.(type) {
		case *doc.Paragraph:
			if bl.HeadingLevel == 1 {
				headings = append(headings, doc.PlainText(bl.Inlines))
				breaks = append(breaks, bl.PageBreakBefore)
			}
		case *doc.Table:
			tables++
		}
	}
	if !slices.Equal(headings, []string{"첫 번째 장", "두 번째 장", "세 번째 장"}) || !slices.Equal(breaks, []bool{false, true, true}) {
		t.Fatalf("%v %v", headings, breaks)
	}
	inl := allInlines(b.Doc)
	var links []*doc.Link
	bookmarks := map[string]bool{}
	images := 0
	boldLoud, afterDiv := false, false
	for _, in := range inl {
		switch in := in.(type) {
		case *doc.Link:
			links = append(links, in)
		case *doc.Bookmark:
			bookmarks[in.Name] = true
		case *doc.Image:
			images++
		case *doc.Text:
			if in.Text == "크게" && doc.Val(in.Format.Bold, false) {
				boldLoud = true
			}
			if in.Text == "빈 div 다음 문단" {
				afterDiv = true
			}
		}
	}
	if len(links) != 1 || !bookmarks[strings.TrimPrefix(links[0].Target, "#")] {
		t.Errorf("link %v, bookmarks %v", links, bookmarks) // chapter 1 → note in "ch 3.xhtml"
	}
	if images != 1 || tables != 1 || !boldLoud || !afterDiv {
		t.Errorf("images %d tables %d bold %v afterDiv %v", images, tables, boldLoud, afterDiv)
	}
}

func TestEpubToTxtHtmlAndMarkdown(t *testing.T) {
	epub := sampleEpub(t, t.TempDir())
	text := readFile(t, convertTo(t, epub, "epub", "txt", nil))
	containsAll(t, "text", text, "첫 번째 장", "밤하늘에 별이 반짝였다.", "주석 내용입니다.", "● 목록 하나", "이름\t값")
	if strings.Contains(text, "<") {
		t.Error("markup in text")
	}
	html := readFile(t, convertTo(t, epub, "epub", "html", nil))
	containsAll(t, "html", html, `<h1 id="_h1">첫 번째 장</h1>`, `src="data:image/png;base64,`, "<b>별</b>", "<title>별빛 이야기</title>")
	md := convertTo(t, epub, "epub", "md", nil)
	markdown := readFile(t, md)
	containsAll(t, "markdown", markdown, "# 첫 번째 장", "**별**", "| 이름 | 값 |")
	m := regexp.MustCompile(`!\[\]\(<([^>]+)>\)`).FindStringSubmatch(markdown)
	if m == nil {
		t.Fatal("no picture")
	}
	pic, _ := url.PathUnescape(m[1])
	if _, err := os.Stat(filepath.Join(filepath.Dir(md), pic)); err != nil {
		t.Fatal(err)
	}
	p := presets.New("x", "md")
	p.Document.LeaveOutPictures = true
	textOnly := readFile(t, convertTo(t, epub, "epub", "md", p))
	if !strings.Contains(textOnly, "# 첫 번째 장") || strings.Contains(textOnly, "![") {
		t.Error("pictures not left out")
	}
}

func TestMarkdownToEpubIsAValidPackageThatReadsBack(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "그림.png"), pngBytes(120, 80, orange), 0o644)
	md := filepath.Join(dir, "책.md")
	os.WriteFile(md, []byte(`---
title: 마크다운 책
---

# 1장 시작

첫 문단에 **굵게**와 [링크](https://example.com)가 있습니다.[^1]

![그림](그림.png)

## 1.1 절

- 하나
- 둘

# 2장 끝

| A | B |
|---|---|
| 1 | 2 |

[^1]: 각주입니다.
`), 0o644)
	epub := convertTo(t, md, "md", "epub", nil)
	info := validEpub(t, epub)
	if info.title != "마크다운 책" || info.language != "ko" || info.chapters != 2 || info.images != 1 {
		t.Fatalf("%+v", info)
	}
	if !slices.Contains(info.toc, "1장 시작") || !slices.Contains(info.toc, "1.1 절") {
		t.Fatalf("toc %v", info.toc)
	}
	containsAll(t, "read back", epubText(t, epub), "1장 시작", "굵게", "하나", "2장 끝", "각주입니다.")
}

func TestTextAndHtmlBecomeEpub(t *testing.T) {
	dir := t.TempDir()
	txt := filepath.Join(dir, "메모.txt")
	os.WriteFile(txt, []byte("첫 문단의 첫 줄\n같은 문단\n\n둘째 문단"), 0o644)
	html := filepath.Join(dir, "page.html")
	os.WriteFile(html, []byte("<html><head><title>웹 페이지</title></head><body><h1>제목</h1><p>본문</p></body></html>"), 0o644)
	e := convertTo(t, txt, "txt", "epub", nil)
	validEpub(t, e)
	containsAll(t, "txt epub", epubText(t, e), "둘째 문단")
	if info := validEpub(t, convertTo(t, html, "html", "epub", nil)); info.title != "웹 페이지" {
		t.Fatal(info.title)
	}
	if doc.HasReader("docx") {
		t.Log("DOCX reader linked in")
	}
}

func TestDrmProtectedEpubIsRefused(t *testing.T) {
	dir := t.TempDir()
	src := sampleEpub(t, dir)
	z, _ := zip.OpenReader(src)
	var entries []zipEntry
	for _, f := range z.File {
		entries = append(entries, zipEntry{f.Name, readAllZip(t, f)})
	}
	z.Close()
	entries = append(entries, zipEntry{"META-INF/encryption.xml", []byte(`<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container" xmlns:enc="http://www.w3.org/2001/04/xmlenc#">
  <enc:EncryptedData><enc:EncryptionMethod Algorithm="http://www.w3.org/2001/04/xmlenc#aes128-cbc"/>
  <enc:CipherData><enc:CipherReference URI="OEBPS/Text/ch1.xhtml"/></enc:CipherData></enc:EncryptedData>
</encryption>`)})
	locked := writeZip(t, filepath.Join(dir, "locked.epub"), entries)
	if msg := failure(t, locked, "epub", "txt"); !strings.Contains(msg, "DRM") {
		t.Fatal(msg)
	}
	if msg := failure(t, locked, "epub", "kepub"); !strings.Contains(msg, "DRM") {
		t.Fatal(msg)
	}
}

func readAllZip(t *testing.T, f *zip.File) []byte {
	rc, err := f.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	var out []byte
	buf := make([]byte, 4096)
	for {
		n, err := rc.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			return out
		}
	}
}

// ---- MOBI ----

const mobiHTML = `<html><head><guide><reference type="toc" title="Contents" filepos=0000000000 /></guide></head><body>
<h1>Chapter One</h1>
<p>It was a <b>dark</b> and stormy night; the rain fell in torrents — except at occasional intervals.</p>
<p>See <a filepos=FILEPOS>the note</a> below. Ünïcödé text and 한글 too.</p>
<p><img recindex="00001" /></p>
<mbp:pagebreak/>
<h1>Chapter Two</h1>
<p>The note is here.</p>
</body></html>`

// mobiSource has a filepos link pointing at "The note is here." (a byte offset).
func mobiSource() string {
	placeholder := strings.Replace(mobiHTML, "FILEPOS", "0000000000", 1)
	target := strings.Index(placeholder, "<p>The note")
	return strings.Replace(mobiHTML, "FILEPOS", fmt.Sprintf("%010d", target), 1)
}

func TestMobiToEpubKeepsTextPicturesLinksAndMetadata(t *testing.T) {
	for _, compression := range []int{2, 17480, 1} {
		t.Run(strconv.Itoa(compression), func(t *testing.T) {
			dir := t.TempDir()
			mobi := filepath.Join(dir, "book.mobi")
			os.WriteFile(mobi, buildMobi([]byte(mobiSource()), [][]byte{pngBytes(64, 32, orange), pngBytes(100, 150, navy)},
				mobiOptions{compression: compression, cover: 1, title: "Stormy Night", author: "E. Bulwer-Lytton"}), 0o644)
			epub := convertTo(t, mobi, "mobi", "epub", nil)
			info := validEpub(t, epub)
			if info.title != "Stormy Night" || !slices.Contains(info.authors, "E. Bulwer-Lytton") || info.images != 2 || !info.hasCover {
				t.Fatalf("%+v", info)
			}
			b, err := readEpubBook(bg, epub, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			containsAll(t, "mobi text", htmldoc.PlainText(b.Doc, "\n"), "Chapter One", "dark and stormy night", "Ünïcödé text and 한글 too", "Chapter Two", "The note is here.")
			var links []*doc.Link
			bookmarks := map[string]bool{}
			bold := false
			for _, in := range allInlines(b.Doc) {
				switch in := in.(type) {
				case *doc.Link:
					links = append(links, in)
				case *doc.Bookmark:
					bookmarks[in.Name] = true
				case *doc.Text:
					if in.Text == "dark" && doc.Val(in.Format.Bold, false) {
						bold = true
					}
				}
			}
			if len(links) != 1 || !bookmarks[strings.TrimPrefix(links[0].Target, "#")] || !bold {
				t.Fatalf("links %v bookmarks %v bold %v", links, bookmarks, bold)
			}
		})
	}
}

func TestDrmProtectedMobiIsRefused(t *testing.T) {
	p := filepath.Join(t.TempDir(), "locked.azw")
	os.WriteFile(p, buildMobi([]byte(mobiSource()), nil, mobiOptions{compression: 2, encryption: 2, cover: -1, title: "x", author: "y"}), 0o644)
	if msg := failure(t, p, "azw", "epub"); !strings.Contains(msg, "DRM") {
		t.Fatal(msg)
	}
}

func TestPalmDocTextBooksAreReadAsText(t *testing.T) {
	text := []byte("Plain PalmDOC text.\n\nSecond paragraph of the book.")
	r0 := make([]byte, 16)
	r0[1] = 2
	be32at(r0, 4, uint32(len(text)))
	r0[9] = 1
	prc := filepath.Join(t.TempDir(), "old.prc")
	os.WriteFile(prc, palmDb("Old Book", "TEXtREAd", [][]byte{r0, palmDocCompress(text)}), 0o644)
	containsAll(t, "prc", readFile(t, convertTo(t, prc, "prc", "txt", nil)), "Plain PalmDOC text.", "Second paragraph of the book.")
}

func TestPrintReplicaGivesBackItsPdf(t *testing.T) {
	dir := t.TempDir()
	pdfPath := convertTo(t, sampleCbz(t, dir), "cbz", "pdf", nil)
	data, _ := os.ReadFile(pdfPath)
	azw4 := filepath.Join(dir, "replica.azw4")
	os.WriteFile(azw4, printReplica(data), 0o644)
	if got, _ := os.ReadFile(convertTo(t, azw4, "azw4", "pdf", nil)); string(got) != string(data) {
		t.Fatal("PDF differs")
	}
}

// ---- FB2 ----

func TestFb2ToEpubKeepsSectionsNotesCoverAndTheLegacyEncoding(t *testing.T) {
	epub := convertTo(t, sampleFb2(t, t.TempDir()), "fb2", "epub", nil)
	info := validEpub(t, epub)
	if info.title != "Война и мир" || !slices.Equal(info.authors, []string{"Лев Толстой"}) || info.language != "ru" || !info.hasCover || !slices.Contains(info.toc, "Глава первая") {
		t.Fatalf("%+v", info)
	}
	if !strings.Contains(info.allText, `epub:type="footnote"`) {
		t.Error("the note is no footnote")
	}
	containsAll(t, "fb2 text", epubText(t, epub), "Первый абзац и курсив.", "Строка стиха один", "Текст подглавы.", "Цитата из книги.", "Текст примечания.")
}

func TestEpubToFb2RoundTrips(t *testing.T) {
	fb2 := convertTo(t, sampleEpub(t, t.TempDir()), "epub", "fb2", nil)
	root, _, err := parseXML([]byte(readFile(t, fb2)))
	if err != nil {
		t.Fatal(err)
	}
	if v := root.DescendantsLocal("book-title")[0].Value(); v != "별빛 이야기" {
		t.Fatal(v)
	}
	if n := len(root.ChildLocal("body").AllLocal("section")); n != 3 {
		t.Fatalf("%d sections", n)
	}
	if n := len(root.AllLocal("binary")); n != 2 {
		t.Fatalf("%d binaries", n) // picture + cover
	}
	b, err := readFb2Book(fb2, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	containsAll(t, "fb2", htmldoc.PlainText(b.Doc, "\n"), "첫 번째 장", "밤하늘에 별이 반짝였다.", "주석 내용입니다.", "목록 하나")
	bold := false
	for _, in := range allInlines(b.Doc) {
		if tx, ok := in.(*doc.Text); ok && tx.Text == "별" && doc.Val(tx.Format.Bold, false) {
			bold = true
		}
	}
	if !bold {
		t.Error("bold lost")
	}
}

// ---- HTMLZ / TXTZ ----

func TestTxtzOnlyIncludesImagesFromTheUnpackedBook(t *testing.T) {
	folder := t.TempDir()
	outside := filepath.Join(folder, "private.png")
	os.WriteFile(outside, pngBytes(4, 4, teal), 0o644)
	fileURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(outside)}).String()
	txtz := writeZip(t, filepath.Join(folder, "book.txtz"), []zipEntry{
		{"index.txt", []byte("![](images/inside.png) ![](../private.png) ![](" + fileURL + ")")},
		{"images/inside.png", pngBytes(4, 4, teal)},
	})
	b, err := readTxtzBook(txtz, folder)
	if err != nil {
		t.Fatal(err)
	}
	images := doc.Images(b.Doc)
	if len(images) != 1 || !strings.HasSuffix(images[0].Path, filepath.Join("images", "inside.png")) {
		t.Fatalf("%+v", images)
	}
}

func TestHtmlzAndTxtzAreWrittenAndRead(t *testing.T) {
	epub := sampleEpub(t, t.TempDir())
	htmlz := convertTo(t, epub, "epub", "htmlz", nil)
	z, err := zip.OpenReader(htmlz)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	hasImages := false
	for _, f := range z.File {
		names[f.Name] = true
		hasImages = hasImages || strings.HasPrefix(f.Name, "images/")
	}
	z.Close()
	if !names["index.html"] || !names["metadata.opf"] || !hasImages {
		t.Fatal(names)
	}
	containsAll(t, "htmlz", readFile(t, convertTo(t, htmlz, "htmlz", "txt", nil)), "주석 내용입니다.")
	txtz := convertTo(t, epub, "epub", "txtz", nil)
	b, err := readTxtzBook(txtz, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if b.Meta.Title != "별빛 이야기" || len(doc.Images(b.Doc)) != 1 {
		t.Fatalf("%+v %d", b.Meta, len(doc.Images(b.Doc)))
	}
	containsAll(t, "txtz", htmldoc.PlainText(b.Doc, "\n"), "밤하늘에")
}

// ---- KEPUB ----

func TestKepubAddsAndRemovesKoboSpans(t *testing.T) {
	dir := t.TempDir()
	epub := sampleEpub(t, dir)
	kepub := convertTo(t, epub, "epub", "kepub", nil)
	if !strings.HasSuffix(kepub, ".kepub.epub") {
		t.Fatal(kepub)
	}
	z, _ := zip.OpenReader(kepub)
	var ch1, css string
	for _, f := range z.File {
		switch f.Name {
		case "OEBPS/Text/ch1.xhtml":
			ch1 = string(readAllZip(t, f))
		case "OEBPS/Styles/book.css":
			css = string(readAllZip(t, f))
		}
	}
	z.Close()
	containsAll(t, "kepub chapter", ch1, `<div id="book-columns"><div id="book-inner">`, `class="koboSpan" id="kobo.1.1"`, `id="kobo.2.1"`)
	if css != ".loud { font-weight: bold } p.centre { text-align: center } body { color: #333 }" {
		t.Error("other files must be copied unchanged")
	}
	validEpub(t, kepub)
	back := convertTo(t, kepub, "kepub", "epub", nil)
	info := validEpub(t, back)
	if strings.Contains(info.allText, "koboSpan") || strings.Contains(info.allText, "book-columns") {
		t.Error("Kobo markup left")
	}
	containsAll(t, "back", epubText(t, back), "밤하늘에 별이 반짝였다. 크게 외쳤다.")
	if s := sentences("One. Two! 3.14 is pi… right? 終わり。次"); !slices.Equal(s, []string{"One. ", "Two! ", "3.14 is pi… ", "right? ", "終わり。", "次"}) {
		t.Errorf("%q", s)
	}
}

// ---- comics ----

var mediaBox = regexp.MustCompile(`/MediaBox\[0 0 ([\d.]+) ([\d.]+)\]`)

func TestComicsBecomePdfWithOnePagePerPictureInNaturalOrder(t *testing.T) {
	dir := t.TempDir()
	for _, c := range [][2]string{{sampleCbz(t, dir), "cbz"}, {sampleCbt(t, dir), "cbt"}} {
		out := convertTo(t, c[0], c[1], "pdf", nil)
		n, err := pdf.PageCount(out)
		if err != nil || n != len(comicPageList) {
			t.Fatalf("%s: %d pages, %v", c[1], n, err)
		}
		boxes := mediaBox.FindAllStringSubmatch(readFile(t, out), -1)
		for i, b := range boxes {
			w, _ := strconv.ParseFloat(b[1], 64)
			if want := float64(comicPageList[i].w) * 72 / 96; w < want-1 || w > want+1 {
				t.Errorf("%s page %d: width %v, want %v", c[1], i, w, want)
			}
		}
	}
}

func TestComicsBecomeFixedLayoutEpubAndPdfBecomesCbz(t *testing.T) {
	dir := t.TempDir()
	cbz := sampleCbz(t, dir)
	info := validEpub(t, convertTo(t, cbz, "cbz", "epub", nil))
	if !info.fixedLayout || info.chapters != len(comicPageList) || info.images != len(comicPageList) {
		t.Fatalf("%+v", info)
	}
	fromCbt := convertTo(t, sampleCbt(t, dir), "cbt", "cbz", nil)
	z, _ := zip.OpenReader(fromCbt)
	var names []string
	for _, f := range z.File {
		names = append(names, f.Name)
	}
	z.Close()
	if !slices.Equal(names, []string{"001.png", "002.jpg", "003.png"}) {
		t.Fatal(names)
	}
	pdfPath := convertTo(t, cbz, "cbz", "pdf", nil)
	p := presets.New("x", "cbz")
	p.Pdf.RenderDpi = 96
	back := convertTo(t, pdfPath, "pdf", "cbz", p)
	z, _ = zip.OpenReader(back)
	defer z.Close()
	names = nil
	for _, f := range z.File {
		names = append(names, f.Name)
	}
	if !slices.Equal(names, []string{"001.jpg", "002.jpg", "003.jpg"}) {
		t.Fatal(names)
	}
	rc, _ := z.File[0].Open()
	cfg, _, err := image.DecodeConfig(rc)
	rc.Close()
	if err != nil || cfg.Width < comicPageList[0].w-2 || cfg.Width > comicPageList[0].w+2 {
		t.Fatalf("%v %v", cfg, err)
	}
}

func TestPlainTextJoinsHardWrappedLines(t *testing.T) {
	line := strings.Repeat("word ", 13) // 65 characters
	d := textToDocument(line + "\n" + line + "\n" + line + "\n\nNext paragraph")
	ps := d.Sections[0].Blocks
	if len(ps) != 2 {
		t.Fatalf("%d paragraphs", len(ps))
	}
	for _, in := range ps[0].(*doc.Paragraph).Inlines {
		if _, ok := in.(*doc.LineBreak); ok {
			t.Fatal("wrapped lines must be joined")
		}
	}
	d = textToDocument("one\ntwo\nthree")
	if len(d.Sections[0].Blocks) != 3 {
		t.Fatal("lines without empty lines are paragraphs")
	}
}
