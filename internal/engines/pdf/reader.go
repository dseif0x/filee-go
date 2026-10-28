// PDF → Document, so PDF → DOCX / TXT / Markdown / XLSX need no Word or
// LibreOffice. The C# original read PDFs with PdfPig; here PDFium (as
// WebAssembly) supplies the characters, paths and pictures (pageextract.go).
//
// Every page's words (with their positions, fonts, sizes and colours) become
// lines, lines become paragraphs, and the paragraphs and pictures are put in
// reading order, also on two-column pages (layout.go). Headings come from font
// sizes, bold / italic from the fonts, page size and margins from the page.
// Running headers and footers, page numbers and a logo repeated at the top or
// bottom of the pages are left out. A page without text (a scan) becomes the
// rendered page as a picture, so the result is complete; with the OCR layer
// mode the invisible text of a scan is the page's text.
//
// Rows laid out with tab stops stay one line with tabs at the PDF's
// positions, and a line that ends early although the next line's first word
// would have fitted (an address) keeps its line break. Tables drawn with
// lines become tables (tables.go). Not converted: tables without rules, other
// vector drawings on text pages, annotations other than links, form fields.
//
// Words are built from PDFium's characters in its reading order (it applies
// /ActualText and orders right-to-left text logically itself), split at the
// spaces and line ends it finds or generates.

package pdf

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// readerDpi is the resolution of rendered scans and of pictures PDFium can't decode.
const readerDpi = 150

// hwpUnit per point.
const hwpUnit = 100

var subsetPrefix = regexp.MustCompile(`^[A-Z]{6}\+`)

// knownFonts maps PostScript names of common fonts to the family names Word knows.
var knownFonts = map[string]string{
	"helvetica": "Arial", "arial": "Arial", "arialmt": "Arial", "arialnarrow": "Arial Narrow",
	"times": "Times New Roman", "timesnewroman": "Times New Roman", "timesnewromanps": "Times New Roman", "timesnewromanpsmt": "Times New Roman",
	"courier": "Courier New", "couriernew": "Courier New", "couriernewps": "Courier New", "couriernewpsmt": "Courier New",
	"malgungothic": "Malgun Gothic", "segoeui": "Segoe UI", "trebuchetms": "Trebuchet MS", "centurygothic": "Century Gothic",
	"bookantiqua": "Book Antiqua", "comicsansms": "Comic Sans MS", "lucidaconsole": "Lucida Console",
	"palatinolinotype": "Palatino Linotype", "microsoftsansserif": "Microsoft Sans Serif",
	"arialunicodems": "Arial Unicode MS", "nirmalaui": "Nirmala UI", "nirmalatext": "Nirmala Text", "leelawadeeui": "Leelawadee UI",
	"khmerui": "Khmer UI", "laoui": "Lao UI", "myanmartext": "Myanmar Text", "iskoolapota": "Iskoola Pota",
	"traditionalarabic": "Traditional Arabic", "simplifiedarabic": "Simplified Arabic", "simplifiedarabicfixed": "Simplified Arabic Fixed",
	"arabictypesetting": "Arabic Typesetting", "sakkalmajalla": "Sakkal Majalla", "urdutypesetting": "Urdu Typesetting",
	"angsananew": "Angsana New", "angsanaupc": "AngsanaUPC", "browallianew": "Browallia New", "cordianew": "Cordia New",
}

var (
	spacedFamily = regexp.MustCompile(`^(Noto|Liberation|DejaVu)[A-Z]`)
	cjkSuffix    = regexp.MustCompile(`CJK(kr|jp|sc|tc|hk)$`)
	noFamily     = regexp.MustCompile(`^(F|T3Font_?|Font)\d+$`)
	boldName     = regexp.MustCompile(`Bold|Black|Heavy|Semibold|SemiBold|Demi`)
	italicName   = regexp.MustCompile(`Italic|Oblique`)
)

// readerMode says what a read keeps.
type readerMode int

const (
	// ReadFull: text, pictures, tables, scans as pictures.
	ReadFull readerMode = iota
	// ReadOcrLayer: like ReadFull, but the invisible text of a scan is its text.
	ReadOcrLayer
	// ReadTextOnly: text only, invisible text (OCR layers) included.
	ReadTextOnly
)

type docReader struct {
	ctx      context.Context
	doc      *pdfDoc
	media    string
	textOnly bool
	ocrLayer bool
	pictures int
}

// ---- model of a page ----

type textWord struct {
	Text                                   string
	Left, Right, Top, Bottom, Baseline, Sz float64
	Format                                 CharFormat
	Link                                   string
}

type textLine struct {
	Words []*textWord
	Row   int
	// Tabs are the indexes of the words a tab leads to.
	Tabs []int
	rtl  *bool
}

func (l *textLine) Left() float64     { return l.Words[0].Left }
func (l *textLine) Right() float64    { return l.Words[len(l.Words)-1].Right }
func (l *textLine) Baseline() float64 { return l.Words[0].Baseline }
func (l *textLine) Top() float64 {
	v := math.Inf(-1)
	for _, w := range l.Words {
		v = max(v, w.Top)
	}
	return v
}
func (l *textLine) Bottom() float64 {
	v := math.Inf(1)
	for _, w := range l.Words {
		v = min(v, w.Bottom)
	}
	return v
}
func (l *textLine) Size() float64 {
	v := 0.0
	for _, w := range l.Words {
		v = max(v, w.Sz)
	}
	return v
}
func (l *textLine) Text() string {
	parts := make([]string, len(l.Words))
	for i, w := range l.Words {
		parts[i] = w.Text
	}
	return strings.Join(parts, " ")
}

// RightToLeft: mostly Arabic or Hebrew, read from right to left.
func (l *textLine) RightToLeft() bool {
	if l.rtl == nil {
		var b strings.Builder
		for _, w := range l.Words {
			b.WriteString(w.Text)
		}
		v := direction(b.String()) < 0
		l.rtl = &v
	}
	return *l.rtl
}

// First is the word the line starts with in reading order.
func (l *textLine) First() *textWord {
	if l.RightToLeft() {
		return l.Words[len(l.Words)-1]
	}
	return l.Words[0]
}

// Last is the word the line ends with in reading order.
func (l *textLine) Last() *textWord {
	if l.RightToLeft() {
		return l.Words[0]
	}
	return l.Words[len(l.Words)-1]
}

// item is a paragraph, a picture or a ruled table: the unit of reading order.
type item struct {
	Lines          []*textLine
	PicturePath    string
	PictureBounds  *rect
	Table          *ruledTable
	ContainerLeft  float64
	ContainerRight float64
}

func (it *item) box() *rect {
	if it.PictureBounds != nil {
		return it.PictureBounds
	}
	if it.Table != nil {
		return &it.Table.Bounds
	}
	return nil
}

func (it *item) Left() float64 {
	if b := it.box(); b != nil {
		return b.Left
	}
	v := math.Inf(1)
	for _, l := range it.Lines {
		v = min(v, l.Left())
	}
	return v
}

func (it *item) Right() float64 {
	if b := it.box(); b != nil {
		return b.Right
	}
	v := math.Inf(-1)
	for _, l := range it.Lines {
		v = max(v, l.Right())
	}
	return v
}

func (it *item) Top() float64 {
	if b := it.box(); b != nil {
		return b.Top
	}
	return it.Lines[0].Top()
}

func (it *item) Bottom() float64 {
	if b := it.box(); b != nil {
		return b.Bottom
	}
	return it.Lines[len(it.Lines)-1].Bottom()
}

func (it *item) Width() float64  { return it.Right() - it.Left() }
func (it *item) IsPicture() bool { return it.PicturePath != "" }
func (it *item) IsTable() bool   { return it.Table != nil }

// Size of most of the text (by characters).
func (it *item) Size() float64 {
	var words []*textWord
	for _, l := range it.Lines {
		words = append(words, l.Words...)
	}
	return dominantSize(words)
}

func (it *item) Text() string {
	if it.Table != nil {
		return it.Table.Text()
	}
	parts := make([]string, len(it.Lines))
	for i, l := range it.Lines {
		parts[i] = l.Text()
	}
	return strings.Join(parts, " ")
}

func (it *item) RightToLeft() bool {
	if it.IsPicture() || it.IsTable() {
		return false
	}
	n := 0
	for _, l := range it.Lines {
		if l.RightToLeft() {
			n++
		}
	}
	return n*2 > len(it.Lines)
}

type pageContent struct {
	Index         int
	Width, Height float64
	Items         []*item
	Scanned       bool
	ScanPath      string
}

// dominantSize is the size of most text (by characters, rounded to half points), 10 when none.
func dominantSize(words []*textWord) float64 {
	counts := map[float64]int{}
	var order []float64
	for _, w := range words {
		k := math.Round(w.Sz*2) / 2
		if _, ok := counts[k]; !ok {
			order = append(order, k)
		}
		counts[k] += len([]rune(w.Text))
	}
	best, bestCount := 10.0, -1
	for _, k := range order {
		if counts[k] > bestCount {
			best, bestCount = k, counts[k]
		}
	}
	return best
}

// ---- document ----

// ReadDocument reads a PDF into the document model. Pictures are written
// into mediaDir (not used for ReadTextOnly).
func ReadDocument(ctx context.Context, path, mediaDir string, mode readerMode) (*Document, error) {
	d, err := openPdfiumFile(ctx, path)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	r := &docReader{ctx: ctx, doc: d, media: mediaDir, textOnly: mode == ReadTextOnly, ocrLayer: mode == ReadOcrLayer}
	n, err := d.PageCount()
	if err != nil {
		return nil, err
	}
	var pages []*pageContent
	for i := 0; i < n; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p, err := r.readPage(i)
		if err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	removeRepeatedHeadersAndFooters(pages)
	for _, p := range pages {
		if !p.Scanned {
			p.Items = readingOrder(p.Items)
		}
	}
	return r.build(pages, strings.TrimSpace(d.metaText("Title"))), nil
}

// PageTexts is the text of each page in pages (0-based), read like
// ReadTextOnly but page by page, with running heads and page numbers kept:
// "" for a page without text. Paragraphs are separated by an empty line,
// lines end with CR LF.
func PageTexts(ctx context.Context, path string, pages []int) ([]string, error) {
	d, err := openPdfiumFile(ctx, path)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	r := &docReader{ctx: ctx, doc: d, textOnly: true}
	out := make([]string, 0, len(pages))
	for _, index := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p, err := r.readPage(index)
		if err != nil {
			return nil, err
		}
		if p.Scanned {
			out = append(out, "")
			continue
		}
		p.Items = readingOrder(p.Items)
		out = append(out, strings.TrimRight(PlainText(r.build([]*pageContent{p}, "")), "\r\n\t "))
	}
	return out, nil
}

func shift(b rect, crop rect) rect {
	return rect{b.Left - crop.Left, b.Bottom - crop.Bottom, b.Right - crop.Left, b.Top - crop.Bottom}
}

func inside(b rect, x, y float64) bool {
	return x >= b.Left && x <= b.Right && y >= b.Bottom && y <= b.Top
}

// pageWord is a word of a page before it is placed relative to the crop box.
type pageWord struct {
	Text    string
	Box     rect
	Letters []letter
	Lead    letter
}

func sameBaseline(a, b letter) bool {
	return math.Abs(a.OriginY-b.OriginY) <= 0.3*max(1, min(a.Size, b.Size))
}

// words cuts PDFium's characters into words at spaces, line ends and gaps.
func pageWords(letters []letter) []pageWord {
	var out []pageWord
	var cur []letter
	flush := func() {
		if len(cur) == 0 {
			return
		}
		var b strings.Builder
		box := cur[0].Box
		lead := cur[0]
		for _, l := range cur {
			b.WriteString(l.Text)
			box = rect{min(box.Left, l.Box.Left), min(box.Bottom, l.Box.Bottom), max(box.Right, l.Box.Right), max(box.Top, l.Box.Top)}
			if l.Box.Left < lead.Box.Left && !isMark(l.Text) {
				lead = l
			}
		}
		out = append(out, pageWord{Text: b.String(), Box: box, Letters: cur, Lead: lead})
		cur = nil
	}
	for _, l := range letters {
		if l.Space || !l.Horizontal {
			flush()
			continue
		}
		if len(cur) > 0 {
			prev := cur[len(cur)-1]
			size := max(1, min(prev.Size, l.Size))
			gap := max(l.Loose.Left-prev.Loose.Right, prev.Loose.Left-l.Loose.Right)
			if !isMark(l.Text) && (!sameBaseline(prev, l) || gap > 0.25*size) {
				flush()
			}
		}
		cur = append(cur, l)
	}
	flush()
	return out
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	s := slices.Clone(values)
	sort.Float64s(s)
	return s[len(s)/2]
}

func (r *docReader) readPage(index int) (*pageContent, error) {
	pd, err := r.doc.readPageData(index, !r.textOnly)
	if err != nil {
		return nil, err
	}
	crop := pd.Crop
	rotated := pd.Rotation%180 != 0
	content := &pageContent{Index: index, Width: crop.Width(), Height: crop.Height()}
	if rotated {
		content.Width, content.Height = crop.Height(), crop.Width()
	}
	var links []pageLink
	for _, l := range pd.Links {
		links = append(links, pageLink{Box: shift(l.Box, crop), URI: l.URI})
	}

	var words []*textWord
	for _, w := range pageWords(pd.Letters) {
		if strings.TrimSpace(w.Text) == "" {
			continue
		}
		invisible := true
		var sizes []float64
		for _, l := range w.Letters {
			invisible = invisible && l.Invisible
			sizes = append(sizes, l.Size)
		}
		// Invisible text is the OCR layer of a scan: part of the text, not of the page's look.
		if !r.textOnly && !r.ocrLayer && invisible {
			continue
		}
		box := shift(w.Box, crop)
		size := median(sizes)
		cx, cy := (box.Left+box.Right)/2, (box.Top+box.Bottom)/2
		link := ""
		for _, l := range links {
			if inside(l.Box, cx, cy) {
				link = l.URI
				break
			}
		}
		format := charFormat(w.Lead, size)
		// An OCR layer's font only places the words: the document's own font is used.
		if invisible {
			format.Font, format.Bold, format.Italic = "", false, false
		}
		words = append(words, &textWord{Text: cleanText(w.Text), Left: box.Left, Right: box.Right, Top: box.Top, Bottom: box.Bottom,
			Baseline: w.Lead.OriginY - crop.Bottom, Sz: max(1, size), Format: format, Link: link})
	}

	if len(words) == 0 || (rotated && !r.textOnly) {
		// A scan, a page of drawings, or a turned page: the rendered page keeps everything.
		content.Scanned = true
		if !r.textOnly {
			path, err := r.renderScan(index)
			if err != nil {
				return nil, err
			}
			content.ScanPath = path
		}
		return content, nil
	}

	var tables []*ruledTable
	if !r.textOnly && !pd.TooManySegments {
		tables, words = ruledTables(pd.Paths, crop, words)
	}
	if len(words) > 0 {
		content.Items = textItems(words)
	}
	for _, t := range tables {
		content.Items = append(content.Items, &item{Table: t})
	}
	if !r.textOnly {
		pics, err := r.pagePictures(pd, content, crop)
		if err != nil {
			return nil, err
		}
		content.Items = append(content.Items, pics...)
	}
	return content, nil
}

// textItems: words → lines → paragraphs, with tabbed rows joined.
func textItems(words []*textWord) []*item {
	lines := makeLines(words)
	items := paragraphs(lines)
	if joined := joinTabbedRows(lines, items); joined != nil {
		items = paragraphs(joined)
	}
	return items
}

// ---- text formatting ----

func charFormat(l letter, size float64) CharFormat {
	name := l.Font
	return CharFormat{
		Bold:   l.Bold || boldName.MatchString(name),
		Italic: l.Italic || italicName.MatchString(name),
		Size:   int(math.Round(size*2) * 50),
		Font:   fontFamily(name),
		Color:  l.Color,
	}
}

var wordStart = regexp.MustCompile(`([a-z0-9])([A-Z])|([A-Z])([A-Z][a-z])`)

// fontFamily: "ABCDEF+TimesNewRomanPS-BoldMT" → "Times New Roman"; "" for names that are no family (F1, T3Font_0).
func fontFamily(postScript string) string {
	name := subsetPrefix.ReplaceAllString(postScript, "")
	if cut := strings.IndexAny(name, "-,"); cut > 0 {
		name = name[:cut]
	}
	for _, suffix := range []string{"PSMT", "MT", "PS"} {
		if len(name) > len(suffix)+2 && strings.HasSuffix(name, suffix) {
			if mapped, ok := knownFonts[strings.ToLower(name)]; ok {
				return mapped
			}
			name = name[:len(name)-len(suffix)]
		}
	}
	if known, ok := knownFonts[strings.ToLower(name)]; ok {
		return known
	}
	if spacedFamily.MatchString(name) {
		base, cjk := name, ""
		if m := cjkSuffix.FindStringSubmatchIndex(name); m != nil {
			base, cjk = name[:m[0]], strings.ToUpper(name[m[2]:m[3]])
		}
		// A space before a capital after a small letter or digit, and between "UI" and a word (applied twice for overlaps).
		spaced := base
		for i := 0; i < 2; i++ {
			spaced = wordStart.ReplaceAllString(spaced, "$1$3 $2$4")
		}
		spaced = strings.ReplaceAll(spaced, "Deja Vu", "DejaVu")
		if cjk != "" {
			return spaced + " CJK " + cjk
		}
		return spaced
	}
	if len(name) < 3 || noFamily.MatchString(name) {
		return ""
	}
	return name
}

// cleanText writes Latin ligatures (ﬁ, ﬂ, …) as letters, so the text can be searched and edited.
func cleanText(text string) string {
	if !strings.ContainsFunc(text, func(r rune) bool { return r >= 0xFB00 && r <= 0xFB06 }) {
		return text
	}
	var b strings.Builder
	for _, r := range text {
		if r >= 0xFB00 && r <= 0xFB06 {
			b.WriteString(norm.NFKC.String(string(r)))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ---- pictures ----

func (r *docReader) newMediaPath(ext string) (string, error) {
	if err := os.MkdirAll(r.media, 0o755); err != nil {
		return "", err
	}
	r.pictures++
	return filepath.Join(r.media, fmt.Sprintf("pdf-%d.%s", r.pictures, ext)), nil
}

func (r *docReader) renderScan(index int) (string, error) {
	img, err := r.doc.Render(index, readerDpi)
	if err != nil {
		return "", err
	}
	path, err := r.newMediaPath("jpg")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, encodeJpegBytes(img, 85), 0o644)
}

func writePng(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	err = png.Encode(f, img)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// pagePictures of a text page. A picture covering the page behind text is the scan under an OCR layer: skipped.
func (r *docReader) pagePictures(pd *pageData, content *pageContent, crop rect) ([]*item, error) {
	var out []*item
	var rendered *image.RGBA
	for _, img := range pd.Images {
		b := shift(img.Box, crop)
		b = rect{max(0, b.Left), max(0, b.Bottom), min(content.Width, b.Right), min(content.Height, b.Top)}
		if b.Width() < 6 || b.Height() < 6 || img.IsMask {
			continue
		}
		if b.Width()*b.Height() > 0.85*content.Width*content.Height {
			continue
		}
		var path string
		var err error
		if img.Jpeg != nil {
			if path, err = r.newMediaPath("jpg"); err != nil {
				return nil, err
			}
			err = os.WriteFile(path, img.Jpeg, 0o644)
		} else if px := r.doc.imagePixels(img); px != nil {
			if path, err = r.newMediaPath("png"); err != nil {
				return nil, err
			}
			err = writePng(path, px)
		} else {
			// What PDFium can't hand out: cut the picture out of the rendered page.
			if rendered == nil {
				if rendered, err = r.doc.Render(content.Index, readerDpi); err != nil {
					return nil, err
				}
			}
			path, err = r.cropPicture(rendered, b, content)
		}
		if err != nil {
			return nil, err
		}
		if path != "" {
			bb := b
			out = append(out, &item{PicturePath: path, PictureBounds: &bb})
		}
	}
	return out, nil
}

func (r *docReader) cropPicture(page *image.RGBA, b rect, content *pageContent) (string, error) {
	sx := float64(page.Bounds().Dx()) / content.Width
	sy := float64(page.Bounds().Dy()) / content.Height
	area := image.Rect(int(math.Round(b.Left*sx)), int(math.Round((content.Height-b.Top)*sy)),
		int(math.Round(b.Right*sx)), int(math.Round((content.Height-b.Bottom)*sy))).Intersect(page.Bounds())
	if area.Dx() < 2 || area.Dy() < 2 {
		return "", nil
	}
	path, err := r.newMediaPath("png")
	if err != nil {
		return "", err
	}
	return path, writePng(path, page.SubImage(area))
}

// ---- headers and footers ----

var (
	pageNumber      = regexp.MustCompile(`(?i)^[\s\-–—(\[]*(page|p\.|페이지|第)?\s*\d+\s*(/|of|페이지|쪽|页)?\s*\d*[\s\-–—)\]]*$`)
	romanPageNumber = regexp.MustCompile(`(?i)^[\s\-–—(\[]*m{0,3}(cm|cd|d?c{0,3})(xc|xl|l?x{0,3})(ix|iv|v?i{0,3})[\s\-–—)\]]*$`)
	digitsRun       = regexp.MustCompile(`\d+`)
)

func isRomanPageNumber(s string) bool {
	t := strings.TrimLeft(s, " \t\r\n-–—([")
	return t != "" && strings.ContainsRune("ivxlcdmIVXLCDM", rune(t[0])) && romanPageNumber.MatchString(s)
}

// removeRepeatedHeadersAndFooters leaves out what sits at the top or bottom
// edge of the pages only because it is a page: paragraphs repeating on most
// pages (digits ignored), running heads at the same height on three pages or
// more and no larger than the body text, bare page numbers, and the same
// picture (a logo) at the same place on most pages.
func removeRepeatedHeadersAndFooters(pages []*pageContent) {
	var textPages []*pageContent
	for _, p := range pages {
		if !p.Scanned {
			textPages = append(textPages, p)
		}
	}
	if len(textPages) < 2 {
		return
	}
	atEdge := func(it *item, p *pageContent) bool { return it.Top() > p.Height*0.9 || it.Bottom() < p.Height*0.1 }
	key := func(it *item) string { return strings.TrimSpace(digitsRun.ReplaceAllString(it.Text(), "#")) }
	place := func(it *item) string { return fmt.Sprintf("%s\x00%d", key(it), int64(math.Round(it.Top()/2))) }

	counts := map[string]int{}
	placeCounts := map[string]int{}
	var all []*item
	for _, p := range textPages {
		seenKey, seenPlace := map[string]bool{}, map[string]bool{}
		for _, it := range p.Items {
			if it.IsPicture() {
				continue
			}
			all = append(all, it)
			if !atEdge(it, p) {
				continue
			}
			if k := key(it); !seenKey[k] {
				seenKey[k] = true
				counts[k]++
			}
			if k := place(it); !seenPlace[k] {
				seenPlace[k] = true
				placeCounts[k]++
			}
		}
	}
	bodySize := bodySizeOf(all)
	needed := max(2, int(math.Ceil(float64(len(textPages))*0.5)))
	running := func(it *item) bool {
		if len(it.Lines) > 2 {
			return false
		}
		text := it.Text()
		return counts[key(it)] >= needed || (placeCounts[place(it)] >= 3 && it.Size() <= bodySize+0.5) ||
			pageNumber.MatchString(text) || isRomanPageNumber(text)
	}

	pictureKeys := map[*item]string{}
	pictureCounts := map[string]int{}
	for _, p := range textPages {
		seen := map[string]bool{}
		for _, it := range p.Items {
			if it.IsPicture() && atEdge(it, p) {
				k := pictureKey(it)
				pictureKeys[it] = k
				if !seen[k] {
					seen[k] = true
					pictureCounts[k]++
				}
			}
		}
	}
	for _, p := range textPages {
		p.Items = slices.DeleteFunc(p.Items, func(it *item) bool {
			if !atEdge(it, p) {
				return false
			}
			if it.IsPicture() {
				return pictureCounts[pictureKeys[it]] >= needed
			}
			return running(it)
		})
	}
}

// pictureKey: the same logo on every page has the same place (to 2 points) and content.
func pictureKey(it *item) string {
	b := it.PictureBounds
	data, _ := os.ReadFile(it.PicturePath)
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%v,%v,%v,%v:%s", math.Round(b.Left/2), math.Round(b.Top/2), math.Round(b.Width()/2), math.Round(b.Height()/2), hex.EncodeToString(sum[:]))
}

// ---- building the document ----

func hwp(points float64) int { return int(math.Round(points * hwpUnit)) }

func bodySizeOf(items []*item) float64 {
	var words []*textWord
	for _, it := range items {
		for _, l := range it.Lines {
			words = append(words, l.Words...)
		}
	}
	return dominantSize(words)
}

func isHeadingCandidate(it *item, bodySize float64) bool {
	return !it.IsPicture() && len(it.Lines) <= 3 && len([]rune(it.Text())) <= 200 && it.Size() >= bodySize*1.15 && it.Size() >= bodySize+1
}

// headingLevels by size: short paragraphs clearly larger than the body text, the largest first.
func headingLevels(items []*item, bodySize float64) map[float64]int {
	var sizes []float64
	seen := map[float64]bool{}
	for _, it := range items {
		if isHeadingCandidate(it, bodySize) {
			s := math.Round(it.Size())
			if !seen[s] {
				seen[s] = true
				sizes = append(sizes, s)
			}
		}
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(sizes)))
	levels := map[float64]int{}
	for i, s := range sizes {
		levels[s] = min(6, i+1)
	}
	return levels
}

// margins shared by the text pages: the smallest distances from the paper edges to any text or picture.
func margins(pages []*pageContent) (left, right, top, bottom float64) {
	const lo, hi, def = 18.0, 108.0, 72.0
	l, r, t, b := math.Inf(1), math.Inf(1), math.Inf(1), math.Inf(1)
	for _, p := range pages {
		if p.Scanned || len(p.Items) == 0 {
			continue
		}
		for _, it := range p.Items {
			l = min(l, it.Left())
			r = min(r, p.Width-it.Right())
			t = min(t, p.Height-it.Top())
			b = min(b, it.Bottom())
		}
	}
	clampM := func(v float64) float64 {
		if math.IsInf(v, 0) {
			v = def
		}
		return max(lo, min(v, hi))
	}
	return clampM(l), clampM(r), clampM(t), clampM(b)
}

func pitch(it *item) float64 {
	var gaps []float64
	for i := 1; i < len(it.Lines); i++ {
		gaps = append(gaps, it.Lines[i-1].Baseline()-it.Lines[i].Baseline())
	}
	if len(gaps) == 0 {
		return it.Size() * 1.2
	}
	return median(gaps)
}

// typicalLineRatio: distance between baselines relative to the font size (1.2 when unknown).
func typicalLineRatio(items []*item) float64 {
	var ratios []float64
	for _, it := range items {
		if len(it.Lines) >= 2 {
			if r := pitch(it) / it.Size(); r > 0.8 && r < 3 {
				ratios = append(ratios, r)
			}
		}
	}
	if len(ratios) == 0 {
		return 1.2
	}
	return median(ratios)
}

func (r *docReader) build(pages []*pageContent, title string) *Document {
	doc := &Document{Title: title}
	var items []*item
	for _, p := range pages {
		for _, it := range p.Items {
			if !it.IsPicture() && !it.IsTable() {
				items = append(items, it)
			}
		}
	}
	bodySize := bodySizeOf(items)
	levels := headingLevels(items, bodySize)
	left, right, top, bottom := margins(pages)
	lineRatio := typicalLineRatio(items)

	var section *Section
	for _, page := range pages {
		setup := PageSetup{hwp(page.Width), hwp(page.Height), hwp(left), hwp(right), hwp(top), hwp(bottom)}
		newSection := section == nil || section.Page != setup
		if newSection {
			section = &Section{Page: setup}
			doc.Sections = append(doc.Sections, section)
		}
		first := len(section.Blocks)
		if page.Scanned {
			if page.ScanPath != "" {
				// Behind the (empty) paragraph at the paper's corner: the page image never pushes text.
				p := &Paragraph{MarkFormat: CharFormat{Size: 100}}
				p.Inlines = append(p.Inlines, &Image{Path: page.ScanPath, Width: setup.Width, Height: setup.Height, Behind: true})
				section.Blocks = append(section.Blocks, p)
			} else {
				section.Blocks = append(section.Blocks, &Paragraph{})
			}
		} else {
			var previous *item
			for _, it := range page.Items {
				switch {
				case it.IsPicture():
					section.Blocks = append(section.Blocks, pictureParagraph(it, previous))
				case it.IsTable():
					section.Blocks = append(section.Blocks, tableBlock(it, lineRatio))
				default:
					section.Blocks = append(section.Blocks, textParagraph(it, previous, levels, lineRatio))
				}
				previous = it
			}
			if len(section.Blocks) == first {
				section.Blocks = append(section.Blocks, &Paragraph{})
			}
		}
		// A table cannot start a page in every format: a tiny empty paragraph before it carries the page break.
		if !newSection {
			if _, isTable := section.Blocks[first].(*Table); isTable {
				section.Blocks = slices.Insert(section.Blocks, first, Block(&Paragraph{MarkFormat: CharFormat{Size: 100}}))
			}
			if p, ok := section.Blocks[first].(*Paragraph); ok {
				p.PageBreakBefore = true
			}
		}
	}
	if len(doc.Sections) == 0 {
		doc.Sections = append(doc.Sections, &Section{Blocks: []Block{&Paragraph{}}})
	}
	return doc
}

func pictureParagraph(it *item, previous *item) *Paragraph {
	b := it.PictureBounds
	center := (it.ContainerLeft + it.ContainerRight) / 2
	width := it.ContainerRight - it.ContainerLeft
	centered := math.Abs((b.Left+b.Right)/2-center) < max(6, width*0.05) && b.Width() < width*0.9
	align := AlignLeft
	if centered {
		align = AlignCenter
	}
	p := &Paragraph{Format: ParaFormat{Align: align, Before: spaceBefore(it, previous, 1.2),
		// Word's single line spacing (130 % in the model): a smaller multiple would cut off the top of the picture.
		LineSpacing: LineSpacing{SpacingPercent, 130}}}
	p.Inlines = append(p.Inlines, &Image{Path: it.PicturePath, Width: hwp(b.Width()), Height: hwp(b.Height())})
	return p
}

func intPtr(v int) *int { return &v }

func textParagraph(it *item, previous *item, levels map[float64]int, lineRatio float64) *Paragraph {
	size := it.Size()
	p := &Paragraph{}
	if level, ok := levels[math.Round(size)]; ok && len(it.Lines) <= 3 && len([]rune(it.Text())) <= 200 {
		p.HeadingLevel = level
	}
	// Alignment and indents relative to the column the paragraph is in, measured from its start side.
	rtl := it.RightToLeft()
	containerWidth := max(1, it.ContainerRight-it.ContainerLeft)
	lefts := make([]float64, len(it.Lines))
	rights := make([]float64, len(it.Lines))
	for i, l := range it.Lines {
		lefts[i] = l.Left() - it.ContainerLeft
		rights[i] = it.ContainerRight - l.Right()
	}
	starts, ends := lefts, rights
	if rtl {
		starts, ends = rights, lefts
	}
	tolerance := max(3, containerWidth*0.03)
	allCentered := true
	for i := range it.Lines {
		if math.Abs(lefts[i]-rights[i]) >= tolerance {
			allCentered = false
		}
	}
	allEndsTight := true
	for _, e := range ends {
		if e >= 3 {
			allEndsTight = false
		}
	}
	justified := len(it.Lines) >= 2 && it.Width() > containerWidth*0.5
	for _, e := range ends[:len(ends)-1] {
		if e >= 2.5 {
			justified = false
		}
	}
	var align Align
	switch {
	case allCentered && slices.Min(lefts) > containerWidth*0.04:
		align = AlignCenter
	case allEndsTight && slices.Min(starts) > containerWidth*0.15:
		align = AlignRight
	case justified:
		align = AlignJustify
	default:
		align = AlignLeft
	}
	var start, firstLine *int
	if align == AlignLeft || align == AlignJustify {
		body := starts[0]
		if len(it.Lines) > 1 {
			body = slices.Min(starts[1:])
		}
		if body > 2 {
			start = intPtr(hwp(body))
		}
		if f := starts[0] - max(0, body); math.Abs(f) > 2 {
			firstLine = intPtr(hwp(f))
		}
	}
	// A tabbed row: a left tab stop where each part starts in the PDF.
	var tabs []TabStop
	if !rtl {
		seen := map[int]bool{}
		var positions []int
		for _, l := range it.Lines {
			for _, t := range l.Tabs {
				pos := hwp(l.Words[t].Left - it.ContainerLeft)
				if !seen[pos] {
					seen[pos] = true
					positions = append(positions, pos)
				}
			}
		}
		sort.Ints(positions)
		for _, pos := range positions {
			tabs = append(tabs, TabStop{pos})
		}
	}
	pt := size * lineRatio
	if len(it.Lines) >= 2 {
		pt = pitch(it)
	}
	p.Format = ParaFormat{
		Align: align, Left: start, FirstLine: firstLine, Tabs: tabs,
		Before: spaceBefore(it, previous, lineRatio),
		// "At least" the PDF's line distance: close to the original and never cuts off a larger font.
		LineSpacing: LineSpacing{SpacingAtLeast, hwp(max(size, min(pt, size*3)))},
		RightToLeft: rtl,
	}
	p.MarkFormat = CharFormat{Size: int(math.Round(size*2) * 50)}
	p.Inlines = runs(it, rtl)
	return p
}

// spaceBefore is the extra space above an item: its distance to the item right
// above it (in the same column) minus what a line takes anyway.
func spaceBefore(it, previous *item, lineRatio float64) int {
	if previous == nil || previous.Bottom() <= it.Top()-1 || previous.Right() <= it.Left() || previous.Left() >= it.Right() {
		return 0
	}
	// A text line reaches about 0.25 em below its baseline and 0.95 em above it.
	above := previous.Bottom()
	if len(previous.Lines) > 0 {
		above = previous.Lines[len(previous.Lines)-1].Baseline() - 0.25*previous.Size()
	}
	top := it.Top()
	if len(it.Lines) > 0 {
		top = it.Lines[0].Baseline() + 0.95*it.Size()
	}
	normal := 0.0
	if len(it.Lines) > 0 && len(previous.Lines) > 0 {
		normal = (lineRatio - 1.2) * it.Size()
	}
	if extra := above - top - normal; extra > 1 {
		return hwp(min(extra, 72))
	}
	return 0
}

type piece struct {
	text   string
	format CharFormat
	link   string
}

// runs joins words into runs of equal formatting, with a space between words
// (not between two Chinese / Japanese words); line ends become spaces (not
// inside CJK text or after a hyphen), or line breaks where the line ended
// early on purpose. Tabs of a tabbed row stay tabs.
func runs(it *item, rtl bool) []Inline {
	var pieces []piece
	for li, line := range it.Lines {
		words := line.Words
		if len(line.Tabs) == 0 {
			words = inReadingOrder(line.Words, rtl)
		}
		for wi, w := range words {
			text := w.Text
			if len(pieces) > 0 {
				last := &pieces[len(pieces)-1]
				prevRunes := []rune(last.text)
				lastRune := prevRunes[len(prevRunes)-1]
				firstRune := []rune(text)[0]
				switch {
				case wi > 0 && slices.Contains(line.Tabs, wi):
					pieces = append(pieces, piece{"\t", w.Format, ""})
				case wi > 0:
					// Not between two Chinese or Japanese words (an OCR layer spaces every word).
					if !(isCjk(lastRune) && isCjk(firstRune)) {
						last.text += " "
					}
				case strings.HasSuffix(last.text, "-") && len(prevRunes) > 1 && unicode.IsLetter(prevRunes[len(prevRunes)-2]) && unicode.IsLower(firstRune):
					last.text = string(prevRunes[:len(prevRunes)-1]) // hyphenated at the line end
				case endedEarly(it, li, rtl):
					pieces = append(pieces, piece{"\n", w.Format, ""})
				case !(isCjk(lastRune) || isCjk(firstRune)):
					last.text += " "
				}
			}
			pieces = append(pieces, piece{text, w.Format, w.Link})
		}
	}

	var out []Inline
	var link *Link
	for _, g := range mergePieces(pieces) {
		if g.text == "\t" || g.text == "\n" {
			link = nil
			if g.text == "\t" {
				out = append(out, &Tab{g.format})
			} else {
				out = append(out, &LineBreak{g.format})
			}
			continue
		}
		t := &Text{g.text, g.format}
		if g.link == "" {
			link = nil
			out = append(out, t)
			continue
		}
		if link == nil || link.Target != g.link {
			link = &Link{Target: g.link}
			out = append(out, link)
		}
		link.Content = append(link.Content, t)
	}
	return out
}

// endedEarly: line l-1 ended although the first word of line l would have
// fitted after it, a line break the author made (an address, a poem).
func endedEarly(it *item, l int, rtl bool) bool {
	previous, line := it.Lines[l-1], it.Lines[l]
	if strings.HasSuffix(previous.Last().Text, "-") {
		return false
	}
	em := line.Size()
	column := it.ContainerRight - it.ContainerLeft
	first := line.First()
	firstWord := first.Right - first.Left
	if rtl {
		start := previous.Left() - 0.3*em - firstWord
		if start > it.Left()+0.5*em {
			return true
		}
		return start > it.ContainerLeft+0.5*em && it.Left()-it.ContainerLeft > 0.25*column
	}
	end := previous.Right() + 0.3*em + firstWord
	if end < it.Right()-0.5*em {
		return true
	}
	return end < it.ContainerRight-0.5*em && it.ContainerRight-it.Right() > 0.25*column
}

const sentencePunctuation = ".,:;!?،؛؟"

// inReadingOrder puts a line's words (left to right) in reading order: right
// to left in a right-to-left paragraph, numbers and Latin words in it kept
// left to right, Arabic or Hebrew words in a left-to-right line turned.
func inReadingOrder(words []*textWord, rtl bool) []*textWord {
	if !rtl && !slices.ContainsFunc(words, func(w *textWord) bool { return direction(w.Text) < 0 }) {
		return words
	}
	dir := func(w *textWord) int { return direction(w.Text) }
	var ordered []*textWord
	if rtl {
		rev := slices.Clone(words)
		slices.Reverse(rev)
		ordered = logicalOrder(rev, dir, true)
	} else {
		ordered = logicalOrder(words, dir, false)
	}
	// A full stop or comma after a word of the other direction is drawn on that
	// word's far side: it comes first in the word's letters and goes back to the end.
	other := -1
	if rtl {
		other = 1
	}
	for i, w := range ordered {
		runes := []rune(w.Text)
		lead := 0
		for lead < len(runes)-1 && strings.ContainsRune(sentencePunctuation, runes[lead]) {
			lead++
		}
		if lead > 0 && direction(w.Text) == other {
			c := *w
			c.Text = string(runes[lead:]) + string(runes[:lead])
			ordered[i] = &c
		}
	}
	return ordered
}

func mergePieces(pieces []piece) []piece {
	var out []piece
	var b strings.Builder
	var cur piece
	open := false
	for _, p := range pieces {
		s := b.String()
		if open && (p.format != cur.format || p.link != cur.link || p.text == "\t" || p.text == "\n" || s == "\t" || s == "\n") {
			out = append(out, piece{s, cur.format, cur.link})
			b.Reset()
		}
		b.WriteString(p.text)
		cur, open = p, true
	}
	if open {
		s := b.String()
		if s != "\t" && s != "\n" {
			s = strings.TrimRightFunc(s, unicode.IsSpace)
		}
		out = append(out, piece{s, cur.format, cur.link})
	}
	return out
}

// isCjk: Chinese and Japanese text has no spaces between words (Korean does).
func isCjk(r rune) bool {
	return r >= 0x3040 && r <= 0x30FF || r >= 0x3400 && r <= 0x9FFF || r >= 0xF900 && r <= 0xFAFF || r >= 0xFF00 && r <= 0xFF60 || r >= 0x3000 && r <= 0x303F
}

// PlainText of a document: paragraphs separated by an empty line, CR LF line ends.
func PlainText(doc *Document) string {
	var b strings.Builder
	for _, s := range doc.Sections {
		for _, blk := range s.Blocks {
			p, ok := blk.(*Paragraph)
			if !ok {
				continue
			}
			text := strings.TrimSpace(plainInlines(p.Inlines))
			if text == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteString("\r\n\r\n")
			}
			b.WriteString(text)
		}
	}
	if b.Len() == 0 {
		return ""
	}
	b.WriteString("\r\n")
	return b.String()
}

func plainInlines(inlines []Inline) string {
	var b strings.Builder
	for _, in := range inlines {
		switch v := in.(type) {
		case *Text:
			b.WriteString(v.Text)
		case *Tab:
			b.WriteByte('\t')
		case *LineBreak:
			b.WriteString("\r\n")
		case *Link:
			b.WriteString(plainInlines(v.Content))
		}
	}
	return b.String()
}
