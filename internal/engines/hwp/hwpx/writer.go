package hwpx

// Writes HWPX (한글 / OWPML, KS X 6101) packages from the document model — no Hancom Office needed.
//
// The package layout, styles and the reference document blank.hwpx come from pypandoc-hwpx
// (MIT, https://github.com/msjang/pypandoc-hwpx). Element order and attribute values follow files saved by
// 한글 itself where the schema and 한글 disagree (see the notes in each file):
//   - writer.go         package: mimetype first and stored, one section file per 한글 section, manifest;
//   - writer_header.go  header.xml: fonts, character/paragraph shapes, numberings, borders, tab stops;
//   - writer_body.go    sections (page setup, columns, headers/footers), paragraphs, runs and tables;
//   - writer_objects.go pictures, text boxes, notes, hyperlinks, bookmarks and page numbers.

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/hwp/xmltree"
)

const (
	nsHH = "http://www.hancom.co.kr/hwpml/2011/head"
	nsHC = "http://www.hancom.co.kr/hwpml/2011/core"
	nsHP = "http://www.hancom.co.kr/hwpml/2011/paragraph"
	nsHS = "http://www.hancom.co.kr/hwpml/2011/section"
)

//go:embed blank.hwpx
var blankHwpx []byte

// BlankTemplate is the built-in reference document.
func BlankTemplate() []byte { return blankHwpx }

var hwpImageTypes = map[string]bool{"png": true, "jpg": true, "gif": true, "bmp": true}

type styleIDs struct{ style, paraPr, charPr string }

type imageEntry struct{ id, source, ext string }

// writer converts one document into an HWPX package.
type writer struct {
	head            *xmltree.Element
	templateSection *xmltree.Element
	images          []imageEntry
	plain           strings.Builder
	nextObjectID    int

	// header.xml caches (writer_header.go)
	charPrCache   map[charPrKey]string
	paraPrCache   map[paraPrKeyFull]string
	numberingIDs  map[*doc.Numbering]string
	borderFills   map[borderFillKey]string
	tabPrIDs      map[string]string
	fontIDs       map[[2]string]int
	headings      map[int]styleIDs
	charPrs       map[string]*xmltree.Element
	paraPrs       map[string]*xmltree.Element
	maxCharPr     int
	maxParaPr     int
	normal        styleIDs
	footnoteStyle *styleIDs

	// body state (writer_body.go)
	width    int
	pageLeft int
	pageTop  int
	base     styleIDs

	footnotes, endnotes, zOrder int
}

// Write writes document as an HWPX file at outputPath (replaced atomically). template is a reference HWPX
// supplying styles and the default page; nil uses the built-in blank.hwpx.
func Write(document *doc.Document, outputPath string, template []byte) error {
	if template == nil {
		template = blankHwpx
	}
	reference, err := zip.NewReader(bytes.NewReader(template), int64(len(template)))
	if err != nil {
		return fmt.Errorf("reference HWPX: %w", err)
	}
	headerXML, err := readEntry(reference, "Contents/header.xml")
	if err != nil {
		return err
	}
	sectionXML, err := readEntry(reference, "Contents/section0.xml")
	if err != nil {
		return err
	}
	header, err := xmltree.ParseString(headerXML)
	if err != nil {
		return fmt.Errorf("header.xml: %w", err)
	}
	section, err := xmltree.ParseString(sectionXML)
	if err != nil {
		return fmt.Errorf("section0.xml: %w", err)
	}
	header.Root.StripWhitespace()
	section.Root.StripWhitespace()
	w := &writer{head: header.Root, templateSection: section.Root, nextObjectID: 1_000_000}
	if err := w.initializeHeader(); err != nil {
		return err
	}
	sections := document.Sections
	if len(sections) == 0 {
		sections = []*doc.Section{doc.NewSection()}
	}
	var parts []string
	for _, s := range sections {
		xml, err := w.section(s)
		if err != nil {
			return err
		}
		parts = append(parts, xml)
	}
	return w.writePackage(reference, outputPath, parts, document.Title)
}

// ───────────────────────── Package ─────────────────────────

func (w *writer) writePackage(reference *zip.Reader, outputPath string, sections []string, title string) error {
	header := w.serializeHeader(len(sections))
	temp := outputPath + ".tmp"
	file, err := os.Create(temp)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			file.Close()
			os.Remove(temp)
		}
	}()
	out := zip.NewWriter(file)
	mimetype, err := readEntry(reference, "mimetype")
	if err != nil {
		return err
	}
	// OCF rule: "mimetype" first and stored uncompressed.
	if err := writeEntry(out, "mimetype", []byte(mimetype), zip.Store); err != nil {
		return err
	}
	for _, entry := range reference.File {
		name := entry.Name
		if name == "mimetype" || strings.HasSuffix(name, "/") {
			continue
		}
		switch name {
		case "Contents/section0.xml":
			for i, s := range sections {
				if err := writeEntry(out, fmt.Sprintf("Contents/section%d.xml", i), []byte(s), zip.Deflate); err != nil {
					return err
				}
			}
		case "Contents/header.xml":
			err = writeEntry(out, name, []byte(header), zip.Deflate)
		case "Contents/content.hpf":
			var hpf string
			if hpf, err = readEntry(reference, name); err == nil {
				err = writeEntry(out, name, []byte(w.buildManifest(hpf, len(sections), title)), zip.Deflate)
			}
		case "META-INF/container.rdf":
			var rdf string
			if rdf, err = readEntry(reference, name); err == nil {
				err = writeEntry(out, name, []byte(buildRdf(rdf, len(sections))), zip.Deflate)
			}
		case "Preview/PrvText.txt":
			err = writeEntry(out, name, []byte(w.previewText()), zip.Deflate)
		default:
			err = copyEntry(out, entry)
		}
		if err != nil {
			return err
		}
	}
	for _, image := range w.images {
		target := image.ext
		if !hwpImageTypes[target] {
			target = "png"
		}
		dst, err := out.CreateHeader(&zip.FileHeader{Name: "BinData/" + image.id + "." + target, Method: zip.Deflate})
		if err != nil {
			return err
		}
		if target == image.ext {
			err = copyFile(image.source, dst)
		} else {
			// WEBP, TIFF ... → PNG, which 한글 always displays.
			err = writePNG(image.source, dst)
		}
		if err != nil {
			return fmt.Errorf("picture %s: %w", image.source, err)
		}
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	ok = true
	return os.Rename(temp, outputPath)
}

// sectionOpenTag is the opening tag of a section part, declaring the same namespaces as header.xml (as 한글 does).
func (w *writer) sectionOpenTag() string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes" ?><hs:sec`)
	hasHS := false
	for _, a := range w.head.Attrs {
		var name string
		switch {
		case a.Prefix == "xmlns":
			name = "xmlns:" + a.Local
		case a.Prefix == "" && a.Local == "xmlns":
			name = "xmlns"
		default:
			continue
		}
		if a.Value == nsHS {
			hasHS = true
		}
		sb.WriteString(" " + name + `="` + a.Value + `"`)
	}
	if !hasHS {
		sb.WriteString(` xmlns:hs="` + nsHS + `"`)
	}
	sb.WriteString(">")
	return sb.String()
}

func (w *writer) buildManifest(hpf string, sectionCount int, title string) string {
	if strings.TrimSpace(title) != "" {
		start := strings.Index(hpf, "<opf:title>")
		end := strings.Index(hpf, "</opf:title>")
		if start >= 0 && end > start {
			hpf = hpf[:start+len("<opf:title>")] + escape(title) + hpf[end:]
		} else {
			hpf = strings.ReplaceAll(hpf, "<opf:title/>", "<opf:title>"+escape(title)+"</opf:title>")
		}
	}
	var items, spine strings.Builder
	for i := 1; i < sectionCount; i++ {
		fmt.Fprintf(&items, `<opf:item id="section%d" href="Contents/section%d.xml" media-type="application/xml"/>`, i, i)
		fmt.Fprintf(&spine, `<opf:itemref idref="section%d" linear="yes"/>`, i)
	}
	for _, image := range w.images {
		target := image.ext
		if !hwpImageTypes[target] {
			target = "png"
		}
		mime := "image/png"
		switch target {
		case "jpg":
			mime = "image/jpeg"
		case "gif":
			mime = "image/gif"
		case "bmp":
			mime = "image/bmp"
		}
		fmt.Fprintf(&items, `<opf:item id="%s" href="BinData/%s.%s" media-type="%s" isEmbeded="1"/>`, image.id, image.id, target, mime)
	}
	if s, ok := insertAfter(hpf, `href="Contents/section0.xml" media-type="application/xml"/>`, items.String()); ok {
		hpf = s
	} else {
		hpf = insertBefore(hpf, "</opf:manifest>", items.String())
	}
	if s, ok := insertAfter(hpf, `<opf:itemref idref="section0" linear="yes"/>`, spine.String()); ok {
		return s
	}
	return insertBefore(hpf, "</opf:spine>", spine.String())
}

func buildRdf(rdf string, sectionCount int) string {
	var parts strings.Builder
	for i := 1; i < sectionCount; i++ {
		fmt.Fprintf(&parts, `<rdf:Description rdf:about=""><ns0:hasPart xmlns:ns0="http://www.hancom.co.kr/hwpml/2016/meta/pkg#" rdf:resource="Contents/section%d.xml"/></rdf:Description>`, i)
		fmt.Fprintf(&parts, `<rdf:Description rdf:about="Contents/section%d.xml"><rdf:type rdf:resource="http://www.hancom.co.kr/hwpml/2016/meta/pkg#SectionFile"/></rdf:Description>`, i)
	}
	return insertBefore(rdf, "</rdf:RDF>", parts.String())
}

func insertAfter(text, marker, insert string) (string, bool) {
	i := strings.Index(text, marker)
	if i < 0 {
		return text, false
	}
	return text[:i+len(marker)] + insert + text[i+len(marker):], true
}

func insertBefore(text, marker, insert string) string {
	i := strings.LastIndex(text, marker)
	if i < 0 {
		return text
	}
	return text[:i] + insert + text[i:]
}

func (w *writer) previewText() string {
	text := strings.TrimSpace(w.plain.String())
	if r := []rune(text); len(r) > 1000 {
		return string(r[:1000])
	}
	return text
}

// ───────────────────────── Helpers ─────────────────────────

func (w *writer) nextID() int {
	w.nextObjectID++
	return w.nextObjectID - 1
}

func maxID(elements []*xmltree.Element) int {
	m := 0
	for _, e := range elements {
		if id, err := strconv.Atoi(e.A("id")); err == nil && id > m {
			m = id
		}
	}
	return m
}

// escape XML-escapes text and removes characters XML 1.0 does not allow (e.g. vertical tab from Word).
func escape(text string) string { return doc.EscapeXML(text) }

// Escape is the escaping the HWPX writer uses (exported for the other writers).
func Escape(text string) string { return doc.EscapeXML(text) }

func readEntry(archive *zip.Reader, name string) (string, error) {
	for _, f := range archive.File {
		if f.Name == name {
			r, err := f.Open()
			if err != nil {
				return "", err
			}
			defer r.Close()
			b, err := io.ReadAll(r)
			return strings.TrimPrefix(string(b), "\ufeff"), err
		}
	}
	return "", errors.New("reference HWPX has no " + name)
}

func writeEntry(out *zip.Writer, name string, data []byte, method uint16) error {
	w, err := out.CreateHeader(&zip.FileHeader{Name: name, Method: method})
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func copyEntry(out *zip.Writer, entry *zip.File) error {
	r, err := entry.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := out.CreateHeader(&zip.FileHeader{Name: entry.Name, Method: zip.Deflate})
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	return err
}
