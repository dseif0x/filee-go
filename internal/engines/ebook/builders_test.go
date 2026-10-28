package ebook

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"

	"github.com/dseif0x/filee-go/internal/xdom"
)

func pngBytes(w, h int, c color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func jpegBytes(w, h int, c color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	jpeg.Encode(&b, img, nil)
	return b.Bytes()
}

var (
	teal   = color.RGBA{0, 128, 128, 255}
	navy   = color.RGBA{0, 0, 128, 255}
	coral  = color.RGBA{255, 127, 80, 255}
	orange = color.RGBA{255, 165, 0, 255}
)

type zipEntry struct {
	name string
	data []byte
}

func writeZip(t *testing.T, path string, entries []zipEntry) string {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for _, e := range entries {
		method := zip.Deflate
		if e.name == "mimetype" {
			method = zip.Store
		}
		w, _ := z.CreateHeader(&zip.FileHeader{Name: e.name, Method: method})
		w.Write(e.data)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return path
}

// sampleEpub is an EPUB 3 with three chapters in OEBPS/Text (bold / italic,
// a class rule, a link from chapter 1 to an anchor in chapter 3, a picture,
// a table), a cover image, a style sheet, a nav document and an NCX.
func sampleEpub(t *testing.T, folder string) string {
	return writeZip(t, filepath.Join(folder, "소설.epub"), []zipEntry{
		{"mimetype", []byte("application/epub+zip")},
		{"META-INF/container.xml", []byte(`<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`)},
		{"OEBPS/content.opf", []byte(`<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="id">urn:uuid:12345678-1234-1234-1234-123456789012</dc:identifier>
    <dc:title>별빛 이야기</dc:title>
    <dc:creator>김작가</dc:creator>
    <dc:language>ko</dc:language>
    <meta property="dcterms:modified">2026-01-01T00:00:00Z</meta>
  </metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="css" href="Styles/book.css" media-type="text/css"/>
    <item id="c1" href="Text/ch1.xhtml" media-type="application/xhtml+xml"/>
    <item id="c2" href="Text/ch2.xhtml" media-type="application/xhtml+xml"/>
    <item id="c3" href="Text/ch%203.xhtml" media-type="application/xhtml+xml"/>
    <item id="pic" href="Images/pic.png" media-type="image/png"/>
    <item id="cover" href="Images/cover.png" media-type="image/png" properties="cover-image"/>
  </manifest>
  <spine toc="ncx">
    <itemref idref="c1"/>
    <itemref idref="c2"/>
    <itemref idref="c3"/>
  </spine>
</package>`)},
		{"OEBPS/nav.xhtml", []byte(`<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><head><title>목차</title></head>
<body><nav epub:type="toc"><ol><li><a href="Text/ch1.xhtml">1장</a></li></ol></nav></body></html>`)},
		{"OEBPS/toc.ncx", []byte(`<?xml version="1.0" encoding="utf-8"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1"><head/><docTitle><text>별빛</text></docTitle>
<navMap><navPoint id="n1" playOrder="1"><navLabel><text>1장</text></navLabel><content src="Text/ch1.xhtml"/></navPoint></navMap></ncx>`)},
		{"OEBPS/Styles/book.css", []byte(".loud { font-weight: bold } p.centre { text-align: center } body { color: #333 }")},
		{"OEBPS/Text/ch1.xhtml", []byte(`<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title/><link rel="stylesheet" type="text/css" href="../Styles/book.css"/></head>
<body>
<h1>첫 번째 장</h1>
<p>밤하늘에 <b>별</b>이 <i>반짝</i>였다. <span class="loud">크게</span> 외쳤다.</p>
<p class="centre">가운데 문단</p>
<p>주석은 <a href="ch%203.xhtml#note1">여기</a>를 보세요.</p>
<div id="empty"/>
<p>빈 div 다음 문단</p>
</body></html>`)},
		{"OEBPS/Text/ch2.xhtml", []byte(`<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>둘</title></head>
<body>
<h1>두 번째 장</h1>
<p>그림이 있는 장입니다.</p>
<p><img src="../Images/pic.png" alt="그림"/></p>
<table><tr><th>이름</th><th>값</th></tr><tr><td>별</td><td>42</td></tr></table>
</body></html>`)},
		{"OEBPS/Text/ch 3.xhtml", []byte(`<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>셋</title></head>
<body>
<h1>세 번째 장</h1>
<p id="note1">주석 내용입니다.</p>
<ul><li>목록 하나</li><li>목록 둘</li></ul>
</body></html>`)},
		{"OEBPS/Images/pic.png", pngBytes(160, 90, teal)},
		{"OEBPS/Images/cover.png", pngBytes(300, 450, navy)},
	})
}

// sampleFb2 is a windows-1251 FB2 book with two chapters, a nested section, a poem, a note and a cover picture.
func sampleFb2(t *testing.T, folder string) string {
	cover := base64.StdEncoding.EncodeToString(pngBytes(120, 180, color.RGBA{139, 0, 0, 255}))
	text := `<?xml version="1.0" encoding="windows-1251"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0" xmlns:l="http://www.w3.org/1999/xlink">
 <description>
  <title-info>
   <genre>prose_classic</genre>
   <author><first-name>Лев</first-name><last-name>Толстой</last-name></author>
   <book-title>Война и мир</book-title>
   <lang>ru</lang>
   <coverpage><image l:href="#cover.png"/></coverpage>
  </title-info>
 </description>
 <body>
  <title><p>Война и мир</p></title>
  <section>
   <title><p>Глава первая</p></title>
   <p>Первый <strong>абзац</strong> и <emphasis>курсив</emphasis>.<a l:href="#n1" type="note">[1]</a></p>
   <empty-line/>
   <poem><stanza><v>Строка стиха один</v><v>Строка стиха два</v></stanza></poem>
   <section>
    <title><p>Подглава</p></title>
    <p>Текст подглавы.</p>
   </section>
  </section>
  <section>
   <title><p>Глава вторая</p></title>
   <cite><p>Цитата из книги.</p></cite>
   <table><tr><th>A</th><th>B</th></tr><tr><td>1</td><td>2</td></tr></table>
  </section>
 </body>
 <body name="notes">
  <section id="n1"><title><p>1</p></title><p>Текст примечания.</p></section>
 </body>
 <binary id="cover.png" content-type="image/png">` + cover + `</binary>
</FictionBook>`
	b, err := charmap.Windows1251.NewEncoder().String(text)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(folder, "voina.fb2")
	os.WriteFile(p, []byte(b), 0o644)
	return p
}

// ---- MOBI (test-only writer following the MobileRead format description) ----

type mobiOptions struct {
	compression, encryption int
	cover                   int // EXTH 201, -1 for none
	title, author           string
}

func be32(v int) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(v))
	return b
}

func be16(b []byte, off, v int)          { binary.BigEndian.PutUint16(b[off:], uint16(v)) }
func be32at(b []byte, off int, v uint32) { binary.BigEndian.PutUint32(b[off:], v) }

// buildMobi is a MOBI 6 book of text and pictures (referenced as recindex="00001", …).
func buildMobi(text []byte, pictures [][]byte, o mobiOptions) []byte {
	const recordSize = 4096
	records := [][]byte{nil}
	for off := 0; off < len(text); off += recordSize {
		chunk := text[off:min(len(text), off+recordSize)]
		data := chunk
		if o.compression == 2 {
			data = palmDocCompress(chunk)
		}
		// Trailing entries (extra flags 0b11): a multibyte byte, then a 2-byte indexing entry at the very end.
		records = append(records, append(append([]byte{}, data...), 0x00, 0x00, 0x82))
	}
	textRecords := len(records) - 1
	firstImage := -1
	if len(pictures) > 0 {
		firstImage = len(records)
	}
	records = append(records, pictures...)
	huff := -1
	if o.compression == 17480 {
		huff = len(records)
		records = append(records, identityHuff(), identityCdic())
	}
	records = append(records, []byte{0xE9, 0x8E, 0x0D, 0x0A})
	records[0] = mobiRecord0(len(text), textRecords, firstImage, huff, o)
	return palmDb(o.title, "BOOKMOBI", records)
}

func mobiRecord0(textLength, textRecords, firstImage, huff int, o mobiOptions) []byte {
	type ex struct {
		typ  int
		data []byte
	}
	exth := []ex{{100, []byte(o.author)}, {503, []byte(o.title)}, {524, []byte("en")}}
	if o.cover >= 0 {
		exth = append(exth, ex{201, be32(o.cover)})
	}
	var eb bytes.Buffer
	eb.WriteString("EXTH")
	length := 12
	for _, e := range exth {
		length += 8 + len(e.data)
	}
	padding := (4 - length%4) % 4
	eb.Write(be32(length + padding))
	eb.Write(be32(len(exth)))
	for _, e := range exth {
		eb.Write(be32(e.typ))
		eb.Write(be32(8 + len(e.data)))
		eb.Write(e.data)
	}
	eb.Write(make([]byte, padding))
	const mobiLength = 0xE8
	h := make([]byte, 16+mobiLength)
	be16(h, 0, o.compression)
	be32at(h, 4, uint32(textLength))
	be16(h, 8, textRecords)
	be16(h, 10, 4096)
	be16(h, 12, o.encryption)
	copy(h[16:], "MOBI")
	be32at(h, 0x14, mobiLength)
	be32at(h, 0x18, 2)
	be32at(h, 0x1C, 65001)
	be32at(h, 0x20, 0x1234)
	be32at(h, 0x24, 6)
	for off := 0x28; off <= 0x4C; off += 4 {
		be32at(h, off, 0xFFFFFFFF)
	}
	be32at(h, 0x50, uint32(textRecords+1))
	name := []byte(o.title)
	be32at(h, 0x54, uint32(len(h)+eb.Len()))
	be32at(h, 0x58, uint32(len(name)))
	be32at(h, 0x5C, 0x09)
	be32at(h, 0x68, 6)
	if firstImage < 0 {
		be32at(h, 0x6C, 0xFFFFFFFF)
	} else {
		be32at(h, 0x6C, uint32(firstImage))
	}
	if huff >= 0 {
		be32at(h, 0x70, uint32(huff))
		be32at(h, 0x74, 2)
	}
	be32at(h, 0x80, 0x40)
	be32at(h, 0xA4, 0xFFFFFFFF)
	be32at(h, 0xA8, 0xFFFFFFFF)
	be16(h, 0xC0, 1)
	be16(h, 0xC2, textRecords)
	be32at(h, 0xC4, 1)
	be32at(h, 0xC8, 0xFFFFFFFF)
	be32at(h, 0xD0, 0xFFFFFFFF)
	be16(h, 0xF2, 0b11)
	out := append(h, eb.Bytes()...)
	out = append(out, name...)
	return append(out, 0, 0, 0, 0)
}

func palmDb(name, typeCreator string, records [][]byte) []byte {
	header := make([]byte, 78+len(records)*8+2)
	n := []byte(strings.ReplaceAll(name, " ", "_"))
	copy(header, n[:min(31, len(n))])
	copy(header[60:], typeCreator)
	be16(header, 76, len(records))
	off := len(header)
	for i, r := range records {
		be32at(header, 78+i*8, uint32(off))
		be32at(header, 78+i*8+4, uint32(i*2)&0x00FFFFFF)
		off += len(r)
	}
	out := header
	for _, r := range records {
		out = append(out, r...)
	}
	return out
}

// palmDocCompress uses every code: literals, literal runs, back-references and space pairs.
func palmDocCompress(in []byte) []byte {
	var out []byte
	for i := 0; i < len(in); {
		bestLen, bestDist := 0, 0
		for d := 1; d <= min(2047, i); d++ {
			l := 0
			for l < 10 && i+l < len(in) && in[i+l] == in[i-d+l] {
				l++
			}
			if l > bestLen {
				bestLen, bestDist = l, d
			}
		}
		if bestLen >= 3 {
			pair := 0x8000 | bestDist<<3 | (bestLen - 3)
			out = append(out, byte(pair>>8), byte(pair))
			i += bestLen
			continue
		}
		c := in[i]
		switch {
		case c == ' ' && i+1 < len(in) && in[i+1] >= 0x40 && in[i+1] <= 0x7F:
			out = append(out, in[i+1]^0x80)
			i += 2
		case c == 0 || c >= 0x09 && c <= 0x7F:
			out = append(out, c)
			i++
		default:
			var run []byte
			for i < len(in) && len(run) < 8 && (in[i] >= 1 && in[i] <= 8 || in[i] >= 0x80) {
				run = append(run, in[i])
				i++
			}
			out = append(out, byte(len(run)))
			out = append(out, run...)
		}
	}
	return out
}

// identityHuff: every byte is an 8-bit code for the dictionary entry of the same number.
func identityHuff() []byte {
	h := make([]byte, 24+256*4+64*4)
	copy(h, "HUFF")
	be32at(h, 4, 24)
	be32at(h, 8, 24)
	be32at(h, 12, 24+256*4)
	for b := 0; b < 256; b++ {
		be32at(h, 24+b*4, uint32(2*b)<<8|0x80|8)
	}
	return h
}

// identityCdic has 256 one-byte literal phrases.
func identityCdic() []byte {
	c := make([]byte, 16+256*2+256*3)
	copy(c, "CDIC")
	be32at(c, 4, 16)
	be32at(c, 8, 256)
	be32at(c, 12, 8)
	for b := 0; b < 256; b++ {
		off := 256*2 + b*3
		be16(c, 16+b*2, off)
		be16(c, 16+off, 0x8001)
		c[16+off+2] = byte(b)
	}
	return c
}

// printReplica is an AZW4 wrapper: "%MOP", one table with one section holding the PDF.
func printReplica(pdf []byte) []byte {
	text := make([]byte, 20+len(pdf))
	copy(text, "%MOP")
	be32at(text, 4, 1)
	be32at(text, 8, 1)
	be32at(text, 12, 20)
	be32at(text, 16, uint32(len(pdf)))
	copy(text[20:], pdf)
	return buildMobi(text, nil, mobiOptions{compression: 1, cover: -1, title: "Replica"})
}

// ---- comics ----

var comicPageList = []struct {
	name string
	w, h int
}{{"chapter/page1.png", 200, 300}, {"chapter/page2.jpg", 220, 330}, {"chapter/page10.png", 240, 360}}

func comicPicture(name string, w, h int) []byte {
	if strings.HasSuffix(name, ".jpg") {
		return jpegBytes(w, h, coral)
	}
	return pngBytes(w, h, coral)
}

func sampleCbz(t *testing.T, folder string) string {
	var entries []zipEntry
	for _, p := range comicPageList {
		entries = append(entries, zipEntry{p.name, comicPicture(p.name, p.w, p.h)})
	}
	entries = append(entries, zipEntry{"__MACOSX/chapter/._page1.png", []byte{0, 1, 2}}, zipEntry{"ComicInfo.xml", []byte("<ComicInfo/>")})
	return writeZip(t, filepath.Join(folder, "만화.cbz"), entries)
}

func sampleCbt(t *testing.T, folder string) string {
	p := filepath.Join(folder, "comic.cbt")
	f, _ := os.Create(p)
	tw := tar.NewWriter(f)
	for _, pg := range comicPageList {
		data := comicPicture(pg.name, pg.w, pg.h)
		tw.WriteHeader(&tar.Header{Name: pg.name, Size: int64(len(data)), Mode: 0o644, Typeflag: tar.TypeReg, Format: tar.FormatPAX})
		tw.Write(data)
	}
	tw.Close()
	f.Close()
	return p
}

// ---- EPUB checks ----

type epubInfo struct {
	title, language, allText string
	authors, toc             []string
	chapters, images         int
	hasCover, fixedLayout    bool
}

// validEpub checks the OCF container and package and returns what it holds.
func validEpub(t *testing.T, path string) epubInfo {
	t.Helper()
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	if z.File[0].Name != "mimetype" || z.File[0].Method != zip.Store {
		t.Fatalf("mimetype must come first, stored: %s", z.File[0].Name)
	}
	files := map[string][]byte{}
	var all strings.Builder
	for _, f := range z.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		files[f.Name] = b
		if strings.HasSuffix(f.Name, ".xhtml") {
			all.Write(b)
			if _, _, err := parseXML(b); err != nil {
				t.Fatalf("%s is not well-formed: %v", f.Name, err)
			}
		}
	}
	if string(files["mimetype"]) != "application/epub+zip" {
		t.Fatal("mimetype")
	}
	opf, _, err := parseXML(files["OEBPS/content.opf"])
	if err != nil {
		t.Fatal(err)
	}
	var info epubInfo
	md := opf.ChildLocal("metadata")
	info.title = strings.TrimSpace(md.ChildLocal("title").Value())
	info.language = strings.TrimSpace(md.ChildLocal("language").Value())
	for _, c := range md.AllLocal("creator") {
		info.authors = append(info.authors, c.Value())
	}
	for _, m := range md.AllLocal("meta") {
		if m.A("property") == "rendition:layout" && m.Value() == "pre-paginated" {
			info.fixedLayout = true
		}
	}
	manifest := map[string]*xdom.Node{}
	for _, it := range opf.ChildLocal("manifest").AllLocal("item") {
		manifest[it.A("id")] = it
		if strings.HasPrefix(it.A("media-type"), "image/") {
			info.images++
			if _, ok := files["OEBPS/"+it.A("href")]; !ok {
				t.Errorf("missing %s", it.A("href"))
			}
		}
		if strings.Contains(it.A("properties"), "cover-image") {
			info.hasCover = true
		}
	}
	for _, ref := range opf.ChildLocal("spine").AllLocal("itemref") {
		it := manifest[ref.A("idref")]
		if it == nil {
			t.Fatalf("spine names %s", ref.A("idref"))
		}
		if it.A("href") != "cover.xhtml" {
			info.chapters++
		}
	}
	nav, _, err := parseXML(files["OEBPS/nav.xhtml"])
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range nav.DescendantsLocal("a") {
		info.toc = append(info.toc, a.Value())
	}
	if _, _, err := parseXML(files["OEBPS/toc.ncx"]); err != nil {
		t.Fatal(err)
	}
	info.allText = all.String()
	return info
}
