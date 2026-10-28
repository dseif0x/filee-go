package ebook

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/htmldoc"
)

// Book → EPUB 3 with an EPUB 2 NCX for older readers: "mimetype" first and
// stored, container.xml, an OPF with Dublin Core metadata, a navigation
// document built from the headings, one XHTML file per chapter (split at
// level-1 headings and page breaks), one style sheet and the pictures.
// Pictures in formats reading systems do not all show (BMP, TIFF, WEBP, …)
// are converted to PNG or JPEG. Fixed-layout books (comics) have one page per
// picture, sized like the picture.

func writeEpub(b *Book, output, sourcePath, work string) error {
	title := b.DisplayTitle(sourcePath)
	language := b.DisplayLanguage()
	pkg := newEpubPackage(title, language, b.Meta)
	imageFolder, err := newFolder(work, "epub-images")
	if err != nil {
		return err
	}
	chapters := htmldoc.WriteXhtml(b.Doc, htmldoc.XhtmlOptions{
		SplitChapters: true,
		ImageSource:   func(p string) string { return pkg.addImage(p, imageFolder, false) },
		Epub:          true,
	})
	// A cover page for covers the content does not show already (MOBI and FB2 keep the cover apart).
	if c := b.Meta.CoverImage; c != "" && isFile(c) {
		if src := pkg.addImage(c, imageFolder, true); src != "" {
			shown := false
			for _, im := range doc.Images(b.Doc) {
				if strings.EqualFold(im.Path, c) {
					shown = true
				}
			}
			if !shown {
				body := `<div style="text-align:center"><img src="` + esc(src) + `" alt="` + esc(title) + `" style="max-height:100%"/></div>` + "\n"
				pkg.addPage("cover.xhtml", htmldoc.Page(title, body, language, "style.css", true, ""))
			}
		}
	}
	for _, ch := range chapters {
		t := ch.Title
		if t == "" {
			t = title
		}
		pkg.addPage(ch.FileName, htmldoc.Page(t, ch.Body, language, "style.css", true, ""))
	}
	pkg.toc = tableOfContents(chapters, title)
	return pkg.save(output, htmldoc.Css, false)
}

// writeFixedLayoutEpub writes one page per picture (comics).
func writeFixedLayoutEpub(ctx context.Context, title string, pages []string, output, work string) error {
	pkg := newEpubPackage(title, "en", &Metadata{})
	imageFolder, err := newFolder(work, "epub-images")
	if err != nil {
		return err
	}
	count := 0
	for _, picture := range pages {
		if err := ctx.Err(); err != nil {
			return err
		}
		src := pkg.addImage(picture, imageFolder, count == 0)
		w, h, ok := imageSize(picture)
		if src == "" || !ok {
			continue // unreadable picture: leave the page out
		}
		count++
		head := fmt.Sprintf("<meta name=\"viewport\" content=\"width=%d, height=%d\"/>\n", w, h)
		body := fmt.Sprintf("<div class=\"page\"><img src=\"%s\" alt=\"\" width=\"%d\" height=\"%d\"/></div>\n", esc(src), w, h)
		pkg.addPage(fmt.Sprintf("p%04d.xhtml", count), htmldoc.Page(title, body, "en", "style.css", true, head))
	}
	if count == 0 {
		return errors.New("the comic contains no readable pictures")
	}
	return pkg.save(output, "body{margin:0;padding:0;}\n.page{margin:0;padding:0;}\nimg{display:block;margin:0;padding:0;}\n", true)
}

// tableOfContents: the two highest heading levels used; chapters without
// headings appear by their first words when no chapter has a heading.
func tableOfContents(chapters []htmldoc.Chapter, title string) []htmldoc.Heading {
	var headings []htmldoc.Heading
	for _, c := range chapters {
		for _, h := range c.Headings {
			if h.Text != "" {
				headings = append(headings, h)
			}
		}
	}
	if len(headings) > 0 {
		top := headings[0].Level
		for _, h := range headings {
			top = min(top, h.Level)
		}
		var out []htmldoc.Heading
		for _, h := range headings {
			if h.Level <= top+1 {
				h.Level = h.Level - top + 1
				out = append(out, h)
			}
		}
		return out
	}
	var out []htmldoc.Heading
	for i, c := range chapters {
		text := title
		if i > 0 {
			text = itoa(i + 1)
			if c.PlainStart != "" {
				text = c.PlainStart + "…"
			}
		}
		out = append(out, htmldoc.Heading{Level: 1, Text: text, Target: c.FileName})
	}
	return out
}

type epubImage struct {
	href, source, mediaType string
	cover                   bool
}

type epubPage struct{ href, content string }

// epubPackage collects the files of an EPUB and writes the package.
type epubPackage struct {
	title, language string
	meta            *Metadata
	pages           []epubPage
	images          []epubImage
	bySource        map[string]int
	toc             []htmldoc.Heading
}

func newEpubPackage(title, language string, meta *Metadata) *epubPackage {
	return &epubPackage{title: title, language: language, meta: meta, bySource: map[string]int{}}
}

func (p *epubPackage) addPage(href, content string) {
	p.pages = append(p.pages, epubPage{href, content})
}

// addImage adds a picture and returns its path in the package, "" when it can't be read.
func (p *epubPackage) addImage(path, convertFolder string, cover bool) string {
	key := strings.ToLower(path)
	if i, ok := p.bySource[key]; ok {
		if cover {
			p.images[i].cover = true
		}
		return p.images[i].href
	}
	file, mt, ok := commonImage(path, convertFolder, true)
	if !ok {
		return ""
	}
	ext := map[string]string{"image/jpeg": "jpg", "image/png": "png", "image/gif": "gif"}[mt]
	if ext == "" {
		ext = "svg"
	}
	href := fmt.Sprintf("images/img%04d.%s", len(p.images)+1, ext)
	p.images = append(p.images, epubImage{href, file, mt, cover})
	p.bySource[key] = len(p.images) - 1
	return href
}

func (p *epubPackage) save(output, css string, fixedLayout bool) error {
	identifier := "urn:uuid:" + uuid.NewString()
	temp := output + ".tmp"
	f, err := os.Create(temp)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	z := zip.NewWriter(f)
	// OCF: "mimetype" first, stored, without extra fields.
	if err := zipText(z, "mimetype", "application/epub+zip", zip.Store); err != nil {
		f.Close()
		return err
	}
	files := []struct{ name, text string }{
		{"META-INF/container.xml", "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<container version=\"1.0\" xmlns=\"urn:oasis:names:tc:opendocument:xmlns:container\">\n" +
			"<rootfiles><rootfile full-path=\"OEBPS/content.opf\" media-type=\"application/oebps-package+xml\"/></rootfiles>\n</container>\n"},
		{"OEBPS/content.opf", p.opf(identifier, fixedLayout)},
		{"OEBPS/nav.xhtml", p.nav()},
		{"OEBPS/toc.ncx", p.ncx(identifier)},
		{"OEBPS/style.css", css},
	}
	for _, pg := range p.pages {
		files = append(files, struct{ name, text string }{"OEBPS/" + pg.href, pg.content})
	}
	for _, fl := range files {
		if err := zipText(z, fl.name, fl.text, zip.Deflate); err != nil {
			f.Close()
			return err
		}
	}
	for _, im := range p.images {
		// Pictures are compressed already.
		if err := zipFile(z, "OEBPS/"+im.href, im.source, zip.Store); err != nil {
			f.Close()
			return err
		}
	}
	if err := z.Close(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	os.Remove(output)
	return os.Rename(temp, output)
}

func zipText(z *zip.Writer, name, text string, method uint16) error {
	w, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: time.Now()})
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, text)
	return err
}

func zipFile(z *zip.Writer, name, path string, method uint16) error {
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	w, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: time.Now()})
	if err != nil {
		return err
	}
	_, err = io.Copy(w, in)
	return err
}

func (p *epubPackage) opf(identifier string, fixedLayout bool) string {
	var sb strings.Builder
	sb.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	fmt.Fprintf(&sb, `<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="book-id" xml:lang="%s"`, esc(p.language))
	if fixedLayout {
		sb.WriteString(` prefix="rendition: http://www.idpf.org/vocab/rendition/#"`)
	}
	sb.WriteString(">\n<metadata xmlns:dc=\"http://purl.org/dc/elements/1.1/\">\n")
	fmt.Fprintf(&sb, "<dc:identifier id=\"book-id\">%s</dc:identifier>\n<dc:title>%s</dc:title>\n<dc:language>%s</dc:language>\n", identifier, esc(p.title), esc(p.language))
	for _, a := range p.meta.Authors {
		fmt.Fprintf(&sb, "<dc:creator>%s</dc:creator>\n", esc(a))
	}
	if p.meta.Publisher != "" {
		fmt.Fprintf(&sb, "<dc:publisher>%s</dc:publisher>\n", esc(p.meta.Publisher))
	}
	if p.meta.Description != "" {
		fmt.Fprintf(&sb, "<dc:description>%s</dc:description>\n", esc(p.meta.Description))
	}
	fmt.Fprintf(&sb, "<meta property=\"dcterms:modified\">%s</meta>\n", time.Now().UTC().Format("2006-01-02T15:04:05Z"))
	for i, im := range p.images {
		if im.cover {
			fmt.Fprintf(&sb, "<meta name=\"cover\" content=\"img%d\"/>\n", i+1) // EPUB 2 readers (and Kindle converters)
			break
		}
	}
	if fixedLayout {
		sb.WriteString("<meta property=\"rendition:layout\">pre-paginated</meta>\n<meta property=\"rendition:spread\">auto</meta>\n")
	}
	sb.WriteString("</metadata>\n<manifest>\n")
	sb.WriteString("<item id=\"nav\" href=\"nav.xhtml\" media-type=\"application/xhtml+xml\" properties=\"nav\"/>\n")
	sb.WriteString("<item id=\"ncx\" href=\"toc.ncx\" media-type=\"application/x-dtbncx+xml\"/>\n")
	sb.WriteString("<item id=\"css\" href=\"style.css\" media-type=\"text/css\"/>\n")
	for i, pg := range p.pages {
		fmt.Fprintf(&sb, "<item id=\"page%d\" href=\"%s\" media-type=\"application/xhtml+xml\"/>\n", i+1, esc(pg.href))
	}
	for i, im := range p.images {
		props := ""
		if im.cover {
			props = ` properties="cover-image"`
		}
		fmt.Fprintf(&sb, "<item id=\"img%d\" href=\"%s\" media-type=\"%s\"%s/>\n", i+1, esc(im.href), im.mediaType, props)
	}
	sb.WriteString("</manifest>\n<spine toc=\"ncx\">\n")
	for i := range p.pages {
		fmt.Fprintf(&sb, "<itemref idref=\"page%d\"/>\n", i+1)
	}
	sb.WriteString("</spine>\n</package>\n")
	return sb.String()
}

// entries are the navigation entries; at least one, as EPUB requires a non-empty table of contents.
func (p *epubPackage) entries() []htmldoc.Heading {
	if len(p.toc) > 0 {
		return p.toc
	}
	target := "nav.xhtml"
	if len(p.pages) > 0 {
		target = p.pages[0].href
	}
	return []htmldoc.Heading{{Level: 1, Text: p.title, Target: target}}
}

func (p *epubPackage) contentsLabel() string {
	switch strings.ToLower(strings.Split(p.language, "-")[0]) {
	case "ko":
		return "목차"
	case "zh":
		return "目录"
	case "ja":
		return "目次"
	}
	return "Contents"
}

// nav is the EPUB 3 navigation document: a nested list of the table of contents.
func (p *epubPackage) nav() string {
	var sb strings.Builder
	sb.WriteString("<nav epub:type=\"toc\" id=\"toc\">\n<h1>" + esc(p.contentsLabel()) + "</h1>\n")
	depth := 0
	for _, e := range p.entries() {
		level := min(e.Level, depth+1)
		if level > depth {
			sb.WriteString("<ol>\n")
		} else {
			sb.WriteString("</li>\n")
		}
		for ; depth > level; depth-- {
			sb.WriteString("</ol>\n</li>\n")
		}
		depth = level
		fmt.Fprintf(&sb, "<li><a href=\"%s\">%s</a>", esc(e.Target), esc(e.Text))
	}
	for ; depth > 0; depth-- {
		sb.WriteString("</li>\n</ol>\n")
	}
	sb.WriteString("</nav>\n")
	return htmldoc.Page(p.title, sb.String(), p.language, "style.css", true, "")
}

func (p *epubPackage) ncx(identifier string) string {
	entries := p.entries()
	maxLevel := 1
	for _, e := range entries {
		maxLevel = max(maxLevel, e.Level)
	}
	var sb strings.Builder
	sb.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<ncx xmlns=\"http://www.daisy.org/z3986/2005/ncx/\" version=\"2005-1\">\n<head>\n")
	fmt.Fprintf(&sb, "<meta name=\"dtb:uid\" content=\"%s\"/>\n<meta name=\"dtb:depth\" content=\"%d\"/>\n", identifier, maxLevel)
	sb.WriteString("<meta name=\"dtb:totalPageCount\" content=\"0\"/>\n<meta name=\"dtb:maxPageNumber\" content=\"0\"/>\n</head>\n")
	fmt.Fprintf(&sb, "<docTitle><text>%s</text></docTitle>\n<navMap>\n", esc(p.title))
	depth := 0
	for i, e := range entries {
		level := min(e.Level, depth+1)
		for ; depth >= level; depth-- {
			sb.WriteString("</navPoint>\n")
		}
		depth = level
		fmt.Fprintf(&sb, "<navPoint id=\"nav%d\" playOrder=\"%d\"><navLabel><text>%s</text></navLabel><content src=\"%s\"/>\n", i+1, i+1, esc(e.Text), esc(e.Target))
	}
	for ; depth > 0; depth-- {
		sb.WriteString("</navPoint>\n")
	}
	sb.WriteString("</navMap>\n</ncx>\n")
	return sb.String()
}
