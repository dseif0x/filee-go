// Package docxwriter writes DOCX (WordprocessingML) from the document model
// by hand, so anything a built-in reader understands (Markdown, TXT,
// spreadsheets, OFD, …) becomes an editable Word document without Word,
// LibreOffice or Pandoc. Child elements are written in schema order (Word
// refuses files where they are not).
package docxwriter

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/images"
)

const (
	relBase  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
	typeBase = "application/vnd.openxmlformats-officedocument.wordprocessingml."
	// namespaces of every WordprocessingML part, declared on the root as Word does.
	namespaces = `xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" ` +
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
		`xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
		`xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" ` +
		`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
		`xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture" ` +
		`xmlns:wps="http://schemas.microsoft.com/office/word/2010/wordprocessingShape"`
	xmlDeclaration = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`
	// defaultSize is the body font size (1/100 pt) of the Normal style.
	defaultSize = 1000
)

// pictureTypes Word shows as they are; others are converted to PNG.
var pictureTypes = map[string]string{
	"png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg", "gif": "image/gif", "bmp": "image/bmp",
	"emf": "image/x-emf", "wmf": "image/x-wmf",
}

type relationship struct {
	id, typ, target string
	external        bool
}

type part struct {
	path, contentType, xml string
	rels                   []relationship
}

// relate returns the id of a relationship from the part (reused for the same target).
func (p *part) relate(typ, target string, external bool) string {
	for _, r := range p.rels {
		if r.typ == typ && r.target == target && r.external == external {
			return r.id
		}
	}
	id := "rId" + strconv.Itoa(len(p.rels)+1)
	p.rels = append(p.rels, relationship{id, typ, target, external})
	return id
}

type media struct {
	source, zipPath, ext string
}

type writer struct {
	parts          []*part
	media          []media
	mediaIndex     map[string]int
	footnotes      []string
	endnotes       []string
	numberings     []*doc.Numbering
	bookmarks      map[string]string
	bookmarkNames  map[string]bool
	document       *part
	footnotesPart  *part
	endnotesPart   *part
	cur            *part
	nextDrawingID  int
	nextBookmarkID int
	evenAndOdd     bool
	width          int
	headerTypes    []string
	footerTypes    []string
	relativeHeight int
	textBoxDepth   int
}

// Write writes d as a .docx file (replaced atomically).
func Write(d *doc.Document, output string) error {
	w := &writer{
		mediaIndex: map[string]int{}, bookmarks: map[string]string{}, bookmarkNames: map[string]bool{},
		document:      &part{path: "word/document.xml", contentType: typeBase + "document.main+xml"},
		nextDrawingID: 1, width: defaultPage.TextWidth(), relativeHeight: 251659264,
	}
	w.cur = w.document
	body := w.body(d)
	return w.writePackage(d, body, output)
}

func (w *writer) withPart(p *part, write func() string) string {
	prev := w.cur
	w.cur = p
	defer func() { w.cur = prev }()
	return write()
}

// headerFooterPart creates a header or footer part and relates it to the document.
func (w *writer) headerFooterPart(kind string, content func() string) string {
	index := 1
	for _, p := range w.parts {
		if strings.HasPrefix(p.path, "word/"+kind) {
			index++
		}
	}
	p := &part{path: fmt.Sprintf("word/%s%d.xml", kind, index), contentType: typeBase + kind + "+xml"}
	root := "w:ftr"
	if kind == "header" {
		root = "w:hdr"
	}
	p.xml = "<" + root + " " + namespaces + ">" + w.withPart(p, content) + "</" + root + ">"
	w.parts = append(w.parts, p)
	return w.document.relate(relBase+kind, fmt.Sprintf("%s%d.xml", kind, index), false)
}

func (w *writer) writePackage(d *doc.Document, body, output string) error {
	w.document.xml = "<w:document " + namespaces + "><w:body>" + body + "</w:body></w:document>"
	w.document.relate(relBase+"styles", "styles.xml", false)
	w.document.relate(relBase+"settings", "settings.xml", false)
	if len(w.numberings) > 0 {
		w.document.relate(relBase+"numbering", "numbering.xml", false)
	}
	if len(w.footnotes) > 0 {
		w.document.relate(relBase+"footnotes", "footnotes.xml", false)
	}
	if len(w.endnotes) > 0 {
		w.document.relate(relBase+"endnotes", "endnotes.xml", false)
	}
	parts := append([]*part{w.document}, w.parts...)
	parts = append(parts, &part{path: "word/styles.xml", contentType: typeBase + "styles+xml", xml: styles()})
	parts = append(parts, &part{path: "word/settings.xml", contentType: typeBase + "settings+xml", xml: w.settings()})
	if len(w.numberings) > 0 {
		parts = append(parts, &part{path: "word/numbering.xml", contentType: typeBase + "numbering+xml", xml: w.numbering()})
	}
	if w.footnotesPart != nil {
		w.footnotesPart.xml = notes("footnote", w.footnotes)
		parts = append(parts, w.footnotesPart)
	}
	if w.endnotesPart != nil {
		w.endnotesPart.xml = notes("endnote", w.endnotes)
		parts = append(parts, w.endnotesPart)
	}
	parts = append(parts, &part{path: "docProps/core.xml", contentType: "application/vnd.openxmlformats-package.core-properties+xml", xml: coreProperties(d.Title)})
	parts = append(parts, &part{path: "docProps/app.xml", contentType: "application/vnd.openxmlformats-officedocument.extended-properties+xml",
		xml: `<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Application>Filee</Application></Properties>`})

	temp := output + ".tmp"
	f, err := os.Create(temp)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	zw := zip.NewWriter(f)
	write := func(name, xml string) error {
		e, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			return err
		}
		_, err = io.WriteString(e, xmlDeclaration+xml)
		return err
	}
	err = write("[Content_Types].xml", w.contentTypes(parts))
	if err == nil {
		err = write("_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
			`<Relationship Id="rId1" Type="`+relBase+`officeDocument" Target="word/document.xml"/>`+
			`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>`+
			`<Relationship Id="rId3" Type="`+relBase+`extended-properties" Target="docProps/app.xml"/></Relationships>`)
	}
	for _, p := range parts {
		if err != nil {
			break
		}
		err = write(p.path, p.xml)
		if err == nil && len(p.rels) > 0 {
			var sb strings.Builder
			sb.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
			for _, r := range p.rels {
				ext := ""
				if r.external {
					ext = ` TargetMode="External"`
				}
				fmt.Fprintf(&sb, `<Relationship Id="%s" Type="%s" Target="%s"%s/>`, r.id, r.typ, esc(r.target), ext)
			}
			sb.WriteString("</Relationships>")
			i := strings.LastIndexByte(p.path, '/')
			err = write(p.path[:i]+"/_rels/"+p.path[i+1:]+".rels", sb.String())
		}
	}
	for _, m := range w.media {
		if err != nil {
			break
		}
		err = writeMedia(zw, m)
	}
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(temp, output)
}

func writeMedia(zw *zip.Writer, m media) error {
	e, err := zw.CreateHeader(&zip.FileHeader{Name: m.zipPath, Method: zip.Deflate})
	if err != nil {
		return err
	}
	src := strings.ToLower(strings.TrimPrefix(filepath.Ext(m.source), "."))
	if src == m.ext || m.ext == "jpg" && src == "jpeg" {
		f, err := os.Open(m.source)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(e, f)
		return err
	}
	// WEBP, TIFF, AVIF, … → PNG, which every Word version shows.
	return images.WritePNG(m.source, e)
}

func (w *writer) contentTypes(parts []*part) string {
	var sb strings.Builder
	sb.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)
	sb.WriteString(`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`)
	sb.WriteString(`<Default Extension="xml" ContentType="application/xml"/>`)
	seen := map[string]bool{}
	for _, m := range w.media {
		if !seen[m.ext] {
			seen[m.ext] = true
			fmt.Fprintf(&sb, `<Default Extension="%s" ContentType="%s"/>`, m.ext, pictureTypes[m.ext])
		}
	}
	for _, p := range parts {
		fmt.Fprintf(&sb, `<Override PartName="/%s" ContentType="%s"/>`, p.path, p.contentType)
	}
	sb.WriteString("</Types>")
	return sb.String()
}

func coreProperties(title string) string {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	t := ""
	if strings.TrimSpace(title) != "" {
		t = "<dc:title>" + esc(title) + "</dc:title>"
	}
	return `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" ` +
		`xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" ` +
		`xmlns:dcmitype="http://purl.org/dc/dcmitype/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` + t +
		`<dcterms:created xsi:type="dcterms:W3CDTF">` + now + `</dcterms:created>` +
		`<dcterms:modified xsi:type="dcterms:W3CDTF">` + now + `</dcterms:modified></cp:coreProperties>`
}

// headingSizes (half-points) of "heading 1".."heading 9", Word's built-in style names.
var headingSizes = []int{32, 28, 26, 24, 22, 20, 20, 20, 20}

func styles() string {
	var sb strings.Builder
	sb.WriteString("<w:styles " + namespaces + ">")
	// Normal matches the HWPX writer's body text: 10 pt, 160 % of the font size, no spacing.
	sb.WriteString(`<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:eastAsia="맑은 고딕" w:hAnsi="Calibri" w:cs="Calibri"/>`)
	sb.WriteString(`<w:sz w:val="20"/><w:szCs w:val="20"/></w:rPr></w:rPrDefault>`)
	sb.WriteString(`<w:pPrDefault><w:pPr><w:widowControl/><w:spacing w:after="0" w:line="295" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>`)
	sb.WriteString(`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>`)
	sb.WriteString(`<w:style w:type="character" w:default="1" w:styleId="DefaultParagraphFont"><w:name w:val="Default Paragraph Font"/><w:uiPriority w:val="1"/><w:semiHidden/><w:unhideWhenUsed/></w:style>`)
	sb.WriteString(`<w:style w:type="table" w:default="1" w:styleId="TableNormal"><w:name w:val="Normal Table"/><w:uiPriority w:val="99"/><w:semiHidden/><w:unhideWhenUsed/>`)
	sb.WriteString(`<w:tblPr><w:tblInd w:w="0" w:type="dxa"/><w:tblCellMar><w:top w:w="0" w:type="dxa"/><w:left w:w="108" w:type="dxa"/><w:bottom w:w="0" w:type="dxa"/><w:right w:w="108" w:type="dxa"/></w:tblCellMar></w:tblPr></w:style>`)
	sb.WriteString(`<w:style w:type="numbering" w:default="1" w:styleId="NoList"><w:name w:val="No List"/><w:uiPriority w:val="99"/><w:semiHidden/><w:unhideWhenUsed/></w:style>`)
	for level := 1; level <= 9; level++ {
		size := headingSizes[level-1]
		hide := ""
		if level > 1 {
			hide = "<w:unhideWhenUsed/>"
		}
		before := 160
		if level <= 2 {
			before = 240
		}
		fmt.Fprintf(&sb, `<w:style w:type="paragraph" w:styleId="Heading%d"><w:name w:val="heading %d"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/>`, level, level)
		fmt.Fprintf(&sb, `<w:uiPriority w:val="9"/>%s<w:qFormat/>`, hide)
		fmt.Fprintf(&sb, `<w:pPr><w:keepNext/><w:keepLines/><w:spacing w:before="%d" w:after="80"/><w:outlineLvl w:val="%d"/></w:pPr>`, before, level-1)
		fmt.Fprintf(&sb, `<w:rPr><w:b/><w:bCs/><w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr></w:style>`, size, size)
	}
	sb.WriteString(`<w:style w:type="character" w:styleId="Hyperlink"><w:name w:val="Hyperlink"/><w:basedOn w:val="DefaultParagraphFont"/><w:uiPriority w:val="99"/><w:unhideWhenUsed/><w:rPr><w:color w:val="0563C1"/><w:u w:val="single"/></w:rPr></w:style>`)
	for _, kind := range []string{"Footnote", "Endnote"} {
		lower := strings.ToLower(kind)
		fmt.Fprintf(&sb, `<w:style w:type="paragraph" w:styleId="%sText"><w:name w:val="%s text"/><w:basedOn w:val="Normal"/><w:uiPriority w:val="99"/><w:semiHidden/><w:unhideWhenUsed/>`, kind, lower)
		sb.WriteString(`<w:pPr><w:spacing w:line="240" w:lineRule="auto"/></w:pPr><w:rPr><w:sz w:val="18"/><w:szCs w:val="18"/></w:rPr></w:style>`)
		fmt.Fprintf(&sb, `<w:style w:type="character" w:styleId="%sReference"><w:name w:val="%s reference"/><w:basedOn w:val="DefaultParagraphFont"/><w:uiPriority w:val="99"/><w:semiHidden/><w:unhideWhenUsed/>`, kind, lower)
		sb.WriteString(`<w:rPr><w:vertAlign w:val="superscript"/></w:rPr></w:style>`)
	}
	for _, kind := range []string{"Header", "Footer"} {
		fmt.Fprintf(&sb, `<w:style w:type="paragraph" w:styleId="%s"><w:name w:val="%s"/><w:basedOn w:val="Normal"/><w:uiPriority w:val="99"/><w:unhideWhenUsed/>`, kind, strings.ToLower(kind))
		sb.WriteString(`<w:pPr><w:tabs><w:tab w:val="center" w:pos="4513"/><w:tab w:val="right" w:pos="9026"/></w:tabs><w:spacing w:line="240" w:lineRule="auto"/></w:pPr></w:style>`)
	}
	sb.WriteString(`<w:style w:type="table" w:styleId="TableGrid"><w:name w:val="Table Grid"/><w:basedOn w:val="TableNormal"/><w:uiPriority w:val="39"/>`)
	sb.WriteString(`<w:pPr><w:spacing w:line="240" w:lineRule="auto"/></w:pPr><w:tblPr><w:tblBorders>`)
	for _, e := range []string{"top", "left", "bottom", "right", "insideH", "insideV"} {
		fmt.Fprintf(&sb, `<w:%s w:val="single" w:sz="4" w:space="0" w:color="auto"/>`, e)
	}
	sb.WriteString("</w:tblBorders></w:tblPr></w:style></w:styles>")
	return sb.String()
}

// settings.xml; children follow the CT_Settings sequence.
func (w *writer) settings() string {
	var sb strings.Builder
	sb.WriteString("<w:settings " + namespaces + `><w:zoom w:percent="100"/><w:defaultTabStop w:val="800"/>`)
	if w.evenAndOdd {
		sb.WriteString("<w:evenAndOddHeaders/>")
	}
	sb.WriteString(`<w:characterSpacingControl w:val="doNotCompress"/>`)
	if len(w.footnotes) > 0 {
		sb.WriteString(`<w:footnotePr><w:footnote w:id="-1"/><w:footnote w:id="0"/></w:footnotePr>`)
	}
	if len(w.endnotes) > 0 {
		sb.WriteString(`<w:endnotePr><w:endnote w:id="-1"/><w:endnote w:id="0"/></w:endnotePr>`)
	}
	// Word 2013+ layout; without it Word opens the file in compatibility mode.
	sb.WriteString(`<w:compat><w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/></w:compat></w:settings>`)
	return sb.String()
}

// numberingID is the numId of a list (one abstractNum + num per list definition).
func (w *writer) numberingID(n *doc.Numbering) int {
	for i, x := range w.numberings {
		if x == n {
			return i + 1
		}
	}
	w.numberings = append(w.numberings, n)
	return len(w.numberings)
}

// listIndent of a level (twips): 400 per level with a 400 hanging number.
func listIndent(level int) int { return (level + 1) * 400 }

func (w *writer) numbering() string {
	var sb strings.Builder
	sb.WriteString("<w:numbering " + namespaces + ">")
	for i, n := range w.numberings {
		id := i + 1
		fmt.Fprintf(&sb, `<w:abstractNum w:abstractNumId="%d"><w:multiLevelType w:val="hybridMultilevel"/>`, id)
		for level := 0; level < 9; level++ {
			def := doc.NumberingLevel{Format: "DIGIT", Text: fmt.Sprintf("^%d.", level+1), Start: 1}
			if level < len(n.Levels) {
				def = n.Levels[level]
			}
			format, text := numberFormat(def.Format), numberText(def.Text)
			if def.Bullet {
				format, text = "bullet", def.Text
				if text == "" {
					text = "●"
				}
			}
			fmt.Fprintf(&sb, `<w:lvl w:ilvl="%d"><w:start w:val="%d"/><w:numFmt w:val="%s"/>`, level, max(0, def.Start), format)
			fmt.Fprintf(&sb, `<w:lvlText w:val="%s"/><w:lvlJc w:val="left"/>`, esc(text))
			fmt.Fprintf(&sb, `<w:pPr><w:ind w:left="%d" w:hanging="400"/></w:pPr></w:lvl>`, listIndent(level))
		}
		sb.WriteString("</w:abstractNum>")
	}
	for i := range w.numberings {
		fmt.Fprintf(&sb, `<w:num w:numId="%d"><w:abstractNumId w:val="%d"/></w:num>`, i+1, i+1)
	}
	sb.WriteString("</w:numbering>")
	return sb.String()
}

// numberFormat maps an OWPML number format to Word's numFmt.
func numberFormat(owpml string) string {
	switch owpml {
	case "ROMAN_CAPITAL":
		return "upperRoman"
	case "ROMAN_SMALL":
		return "lowerRoman"
	case "LATIN_CAPITAL":
		return "upperLetter"
	case "LATIN_SMALL":
		return "lowerLetter"
	case "HANGUL_SYLLABLE":
		return "ganada"
	case "HANGUL_JAMO":
		return "chosung"
	case "CIRCLED_DIGIT":
		return "decimalEnclosedCircle"
	case "HANGUL_PHONETIC":
		return "koreanDigital"
	case "IDEOGRAPH":
		return "ideographDigital"
	}
	return "decimal"
}

// numberText: "^1.^2." → "%1.%2." (Word's level placeholders).
func numberText(t string) string {
	b := []byte(t)
	for i := 0; i+1 < len(b); i++ {
		if b[i] == '^' && b[i+1] >= '0' && b[i+1] <= '9' {
			b[i] = '%'
		}
	}
	return string(b)
}

// notes is footnotes.xml / endnotes.xml: the two separators Word expects, then the notes from id 1.
func notes(kind string, list []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<w:%ss %s>", kind, namespaces)
	fmt.Fprintf(&sb, `<w:%s w:type="separator" w:id="-1"><w:p><w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr><w:r><w:separator/></w:r></w:p></w:%s>`, kind, kind)
	fmt.Fprintf(&sb, `<w:%s w:type="continuationSeparator" w:id="0"><w:p><w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr><w:r><w:continuationSeparator/></w:r></w:p></w:%s>`, kind, kind)
	for i, n := range list {
		fmt.Fprintf(&sb, `<w:%s w:id="%d">%s</w:%s>`, kind, i+1, n, kind)
	}
	fmt.Fprintf(&sb, "</w:%ss>", kind)
	return sb.String()
}

// twips converts HWPUNIT (1/7200 inch) to twips.
func twips(hwp int) int {
	if hwp >= 0 {
		return (hwp*2 + 5) / 10
	}
	return -((-hwp*2 + 5) / 10)
}

// emu converts HWPUNIT to EMU.
func emu(hwp int) int64 { return int64(hwp) * 127 }

// hexColor: "#RRGGBB" → "RRGGBB", "" when it is no colour.
func hexColor(c string) string {
	if len(c) != 7 || c[0] != '#' {
		return ""
	}
	if _, err := strconv.ParseUint(c[1:], 16, 32); err != nil {
		return ""
	}
	return strings.ToUpper(c[1:])
}

func esc(s string) string { return doc.EscapeXML(s) }
