package ocr

// Searchable PDFs written by Filee itself (Apple Vision): each page is the
// picture as it looks, with the recognized words on top in text render mode
// 3 (invisible), placed and stretched over the words of the picture so they
// can be searched, selected and copied. The words are set in a font without
// glyph shapes made here: every UTF-16 unit is one character code
// (Identity-H) and the ToUnicode map gives each code back as itself, so any
// script works without a real font in the file.

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
)

// Line is one recognized line. Positions are fractions of the picture (0–1)
// from its bottom-left corner, as Apple Vision reports them.
type Line struct {
	Left, Bottom, Width, Height float64
	Words                       []Word
}

// Word is one recognized word: its text and where it is across the line
// (fractions of the picture's width).
type Word struct {
	Text        string
	Left, Width float64
}

// EstimateLine splits text into words at white space and spreads them over the
// line by their length: for a line whose words have no boxes of their own.
func EstimateLine(text string, left, bottom, width, height float64) Line {
	l := Line{Left: left, Bottom: bottom, Width: width, Height: height}
	ranges := WordRanges(text)
	units := utf16.Encode([]rune(text))
	letters := 0
	for _, r := range ranges {
		letters += r[1]
	}
	if letters == 0 {
		return l
	}
	// A space between two words counts as one letter of width.
	unit := width / float64(letters+len(ranges)-1)
	x := left
	for _, r := range ranges {
		l.Words = append(l.Words, Word{string(utf16.Decode(units[r[0] : r[0]+r[1]])), x, unit * float64(r[1])})
		x += unit * float64(r[1]+1)
	}
	return l
}

// WordRanges are where each word of text starts and how long it is, in UTF-16 units.
func WordRanges(text string) [][2]int {
	units := utf16.Encode([]rune(text))
	var out [][2]int
	start := -1
	for i := 0; i <= len(units); i++ {
		space := i == len(units) || unicode.IsSpace(rune(units[i]))
		if !space && start < 0 {
			start = i
		} else if space && start >= 0 {
			out = append(out, [2]int{start, i - start})
			start = -1
		}
	}
	return out
}

const (
	fontResource  = "/FOcr"
	characterWide = 500 // width of every character code, in thousandths of the font size
)

func n3(v float64) string {
	r := math.Round(v*1000) / 1000
	if r == 0 {
		r = 0
	}
	return strconv.FormatFloat(r, 'f', -1, 64)
}

// TextLayer is the content stream of the invisible text: one font size per
// line (its height), the baseline at the line's bottom, and each word
// stretched (Tz) to where the next one starts, its trailing space included.
func TextLayer(lines []Line, pageW, pageH float64) string {
	var c strings.Builder
	for _, l := range lines {
		size := l.Height * pageH
		if size <= 0 {
			continue
		}
		base := l.Bottom * pageH
		for i, w := range l.Words {
			if strings.TrimSpace(w.Text) == "" {
				continue
			}
			x := w.Left * pageW
			last := i == len(l.Words)-1
			text := w.Text
			if !last {
				text += " "
			}
			width := w.Width * pageW
			if !last {
				if next := (l.Words[i+1].Left - w.Left) * pageW; next > width {
					width = next
				}
			}
			if width <= 0 {
				continue
			}
			units := utf16.Encode([]rune(text))
			scale := 100 * width / (float64(len(units)) * size * characterWide / 1000)
			fmt.Fprintf(&c, "%s %s Tf %s Tz 1 0 0 1 %s %s Tm <", fontResource, n3(size), n3(scale), n3(x), n3(base))
			for _, u := range units {
				fmt.Fprintf(&c, "%04X", u)
			}
			c.WriteString("> Tj\n")
		}
	}
	if c.Len() == 0 {
		return ""
	}
	return "q\nBT\n3 Tr\n" + c.String() + "ET\nQ\n"
}

// PdfWriter writes searchable PDFs: pictures as pages, each with its text as an invisible layer.
type PdfWriter struct {
	objects [][]byte // 1-based object n is objects[n-1]
	pages   []int
	font    int
}

// init keeps objects 1 and 2 for the catalog and the page tree.
func (w *PdfWriter) init() {
	if len(w.objects) == 0 {
		w.objects = [][]byte{nil, nil}
	}
}

func (w *PdfWriter) add(body []byte) int {
	w.init()
	w.objects = append(w.objects, body)
	return len(w.objects)
}

func stream(dict string, data []byte) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "<< %s /Length %d >>\nstream\n", dict, len(data))
	b.Write(data)
	b.WriteString("\nendstream")
	return b.Bytes()
}

func deflate(data []byte) []byte {
	var b bytes.Buffer
	z, _ := zlib.NewWriterLevel(&b, zlib.BestCompression)
	z.Write(data)
	z.Close()
	return b.Bytes()
}

// PageCount is the number of pages added.
func (w *PdfWriter) PageCount() int { return len(w.pages) }

// AddPage adds picture (a JPEG or PNG file) as a page of width × height
// points with lines on it as invisible text.
func (w *PdfWriter) AddPage(picture string, width, height float64, lines []Line) error {
	img, err := w.image(picture)
	if err != nil {
		return err
	}
	content := fmt.Sprintf("q %s 0 0 %s 0 0 cm /Im0 Do Q\n", n3(width), n3(height))
	resources := fmt.Sprintf("/XObject << /Im0 %d 0 R >>", img)
	if text := TextLayer(lines, width, height); text != "" {
		content += text
		if w.font == 0 {
			w.font = w.createFont()
		}
		resources += fmt.Sprintf(" /Font << %s %d 0 R >>", fontResource, w.font)
	}
	contents := w.add(stream("/Filter /FlateDecode", deflate([]byte(content))))
	page := w.add([]byte(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %s %s] /Resources << %s >> /Contents %d 0 R >>",
		n3(width), n3(height), resources, contents)))
	w.pages = append(w.pages, page)
	return nil
}

// image embeds a picture: a JPEG as it is (unless CMYK), anything else as Flate RGB or gray.
func (w *PdfWriter) image(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	if len(data) > 2 && data[0] == 0xff && data[1] == 0xd8 {
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return 0, err
		}
		switch cfg.ColorModel {
		case color.GrayModel:
			return w.add(stream(fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /DCTDecode", cfg.Width, cfg.Height), data)), nil
		case color.YCbCrModel:
			return w.add(stream(fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode", cfg.Width, cfg.Height), data)), nil
		}
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	b := img.Bounds()
	if g, ok := img.(*image.Gray); ok {
		raw := make([]byte, 0, b.Dx()*b.Dy())
		for y := b.Min.Y; y < b.Max.Y; y++ {
			raw = append(raw, g.Pix[(y-b.Min.Y)*g.Stride:(y-b.Min.Y)*g.Stride+b.Dx()]...)
		}
		return w.add(stream(fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /FlateDecode", b.Dx(), b.Dy()), deflate(raw))), nil
	}
	// Transparency on white, like paper.
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Over)
	raw := make([]byte, 0, b.Dx()*b.Dy()*3)
	for i := 0; i < len(rgba.Pix); i += 4 {
		raw = append(raw, rgba.Pix[i], rgba.Pix[i+1], rgba.Pix[i+2])
	}
	return w.add(stream(fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode", b.Dx(), b.Dy()), deflate(raw))), nil
}

// toUnicode: each two-byte code is the UTF-16 unit it shows.
const toUnicode = `/CIDInit /ProcSet findresource begin
12 dict begin
begincmap
/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def
/CMapName /Adobe-Identity-UCS def
/CMapType 2 def
1 begincodespacerange
<0000> <FFFF>
endcodespacerange
1 beginbfrange
<0000> <FFFF> <0000>
endbfrange
endcmap
CMapName currentdict /CMap defineresource pop
end
end`

// createFont is the Type 0 font with the glyphless TrueType font in it, once per document.
func (w *PdfWriter) createFont() int {
	font := glyphlessFont()
	file := w.add(stream(fmt.Sprintf("/Filter /FlateDecode /Length1 %d", len(font)), deflate(font)))
	desc := w.add([]byte(fmt.Sprintf("<< /Type /FontDescriptor /FontName /%s /Flags 5 /FontBBox [0 0 %d 1000] /ItalicAngle 0 /Ascent 1000 /Descent -1 /CapHeight 1000 /StemV 80 /FontFile2 %d 0 R >>",
		glyphlessName, characterWide, file)))
	// Every character code shows glyph 1, the empty one.
	m := make([]byte, 2*65536)
	for i := 1; i < len(m); i += 2 {
		m[i] = 1
	}
	cidToGid := w.add(stream("/Filter /FlateDecode", deflate(m)))
	cid := w.add([]byte(fmt.Sprintf("<< /Type /Font /Subtype /CIDFontType2 /BaseFont /%s /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor %d 0 R /DW %d /CIDToGIDMap %d 0 R >>",
		glyphlessName, desc, characterWide, cidToGid)))
	tu := w.add(stream("", []byte(toUnicode)))
	return w.add([]byte(fmt.Sprintf("<< /Type /Font /Subtype /Type0 /BaseFont /%s /Encoding /Identity-H /DescendantFonts [%d 0 R] /ToUnicode %d 0 R >>",
		glyphlessName, cid, tu)))
}

// Save writes the document.
func (w *PdfWriter) Save(path string) error {
	w.init()
	kids := make([]string, len(w.pages))
	for i, p := range w.pages {
		kids[i] = fmt.Sprintf("%d 0 R", p)
	}
	w.objects[0] = []byte("<< /Type /Catalog /Pages 2 0 R >>")
	w.objects[1] = []byte(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(w.pages)))
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, len(w.objects))
	for i, o := range w.objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n", i+1)
		b.Write(o)
		b.WriteString("\nendobj\n")
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(w.objects)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(w.objects)+1, xref)
	return os.WriteFile(path, b.Bytes(), 0o644)
}

const glyphlessName = "FileeGlyphless"

// glyphlessFont is a TrueType font with two empty glyphs, 500 of 1000 units
// wide: what the invisible text is set in. Built in code, no font file to ship.
func glyphlessFont() []byte {
	words := func(v ...int) []byte {
		b := make([]byte, 0, 2*len(v))
		for _, x := range v {
			b = binary.BigEndian.AppendUint16(b, uint16(x))
		}
		return b
	}
	tables := map[string][]byte{
		"cmap": words(0, 1, 3, 1, 0, 12, 4, 24, 0, 2, 2, 0, 0, 0xFFFF, 0, 0xFFFF, 1, 0),
		"glyf": {},
		"head": words(1, 0, 1, 0, 0, 0, 0x5F0F, 0x3CF5, 3, 1000, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 500, 1000, 0, 3, 2, 0, 0),
		"hhea": words(1, 0, 1000, 0, 0, 500, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 2),
		"hmtx": words(500, 0, 500, 0),
		"loca": words(0, 0, 0),
		"maxp": words(1, 0, 2, 0, 0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0),
		"name": words(0, 0, 6),
		"post": words(3, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0),
	}
	tags := make([]string, 0, len(tables))
	for t := range tables {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	count := len(tags)
	power, log := 1, 0
	for power*2 <= count {
		power *= 2
		log++
	}
	padded := func(n int) int { return (n + 3) &^ 3 }
	var f []byte
	f = binary.BigEndian.AppendUint32(f, 0x00010000)
	f = append(f, words(count, power*16, log, count*16-power*16)...)
	offset := 12 + 16*count
	headOffset := 0
	for _, t := range tags {
		d := tables[t]
		f = append(f, t...)
		f = binary.BigEndian.AppendUint32(f, checksum(d))
		f = binary.BigEndian.AppendUint32(f, uint32(offset))
		f = binary.BigEndian.AppendUint32(f, uint32(len(d)))
		if t == "head" {
			headOffset = offset
		}
		offset += padded(len(d))
	}
	for _, t := range tags {
		d := tables[t]
		f = append(f, d...)
		f = append(f, make([]byte, padded(len(d))-len(d))...)
	}
	binary.BigEndian.PutUint32(f[headOffset+8:], 0xB1B0AFBA-checksum(f))
	return f
}

func checksum(d []byte) uint32 {
	var sum uint32
	for i := 0; i < len(d); i += 4 {
		var v uint32
		for j := 0; j < 4; j++ {
			v <<= 8
			if i+j < len(d) {
				v |= uint32(d[i+j])
			}
		}
		sum += v
	}
	return sum
}

// writePng writes img as a PNG.
func writePng(img image.Image, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
