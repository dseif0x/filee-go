package hwpx

// DOCX (Office Open XML) → document model, read directly instead of through Pandoc so the layout survives: page size
// and margins per section, columns, headers and footers with page numbers, text boxes, floating pictures, paragraph
// and character formatting, tables with borders, shading and merged cells, lists, footnotes and endnotes, bookmarks
// and hyperlinks (including the links of a Word table of contents).
//
// Split over files: this one walks the package and the text, docx_styles.go resolves formatting, docx_tables.go and
// docx_drawing.go handle tables and graphic objects, omml.go Word math.

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/hwp/xmltree"
)

const (
	nsW    = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	nsR    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	nsPkgR = "http://schemas.openxmlformats.org/package/2006/relationships"
	nsA    = "http://schemas.openxmlformats.org/drawingml/2006/main"
	nsWP   = "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"
	nsPic  = "http://schemas.openxmlformats.org/drawingml/2006/picture"
	nsWPS  = "http://schemas.microsoft.com/office/word/2010/wordprocessingShape"
	nsWPG  = "http://schemas.microsoft.com/office/word/2010/wordprocessingGroup"
	nsWPC  = "http://schemas.microsoft.com/office/word/2010/wordprocessingCanvas"
	nsMC   = "http://schemas.openxmlformats.org/markup-compatibility/2006"
	nsV    = "urn:schemas-microsoft-com:vml"
	nsW10  = "urn:schemas-microsoft-com:office:word"
	nsM    = "http://schemas.openxmlformats.org/officeDocument/2006/math"
	nsDC   = "http://purl.org/dc/elements/1.1/"
)

func init() {
	for _, f := range []string{"docx", "docm", "dotx", "dotm"} {
		doc.RegisterReader(f, ReadDocx)
	}
}

type docxRel struct {
	kind, target string
	external     bool
}

type docxPart struct {
	path string
	root *xmltree.Element
	rels map[string]docxRel
}

type docxReader struct {
	zip         *zip.ReadCloser
	entries     map[string]*zip.File
	mediaFolder string
	parts       map[string]*docxPart
	media       map[string]string
	footnotes   map[string]*xmltree.Element
	endnotes    map[string]*xmltree.Element
	fields      []*fieldState
	footnotesP  *docxPart
	endnotesP   *docxPart
	evenOdd     bool
	linePitches []int
	section     int
	textBoxes   int
	part        *docxPart

	// styles (docx_styles.go)
	styles               map[string]*docxStyle
	resolvedCache        map[string]resolvedStyle
	numberingLevels      map[string][]*numberingLevel
	numberings           map[string]*doc.Numbering
	defaultPara          *paraProps
	defaultRun           *runProps
	defaultParaStyle     string
	defaultTableStyle    string
	minorFont, majorFont [2]string
	themeColors          map[string]string
}

// ReadDocx reads a .docx file; embedded pictures are extracted into mediaFolder.
func ReadDocx(p, mediaFolder string) (*doc.Document, error) {
	z, err := zip.OpenReader(p)
	if err != nil {
		return nil, fmt.Errorf("the file is not a Word document: %w", err)
	}
	defer z.Close()
	r := &docxReader{zip: z, entries: map[string]*zip.File{}, mediaFolder: mediaFolder, parts: map[string]*docxPart{},
		media: map[string]string{}, footnotes: map[string]*xmltree.Element{}, endnotes: map[string]*xmltree.Element{},
		styles: map[string]*docxStyle{}, numberingLevels: map[string][]*numberingLevel{},
		numberings: map[string]*doc.Numbering{}, defaultPara: &paraProps{}, defaultRun: &runProps{}, themeColors: map[string]string{}}
	for _, f := range z.File {
		r.entries[f.Name] = f
	}
	mainPath := "word/document.xml"
	for _, rel := range r.relationshipsOf("") {
		if strings.HasSuffix(rel.kind, "/officeDocument") {
			mainPath = rel.target
			break
		}
	}
	r.part = r.loadPart(mainPath)
	if r.part == nil {
		return nil, errors.New("the file is not a Word document (no word/document.xml)")
	}
	return r.readDocument()
}

// ───────────────────────── Package ─────────────────────────

func (r *docxReader) loadPart(p string) *docxPart {
	if part, ok := r.parts[p]; ok {
		return part
	}
	var part *docxPart
	if root := r.xml(p); root != nil {
		part = &docxPart{path: p, root: root, rels: r.relationshipsOf(p)}
	}
	r.parts[p] = part
	return part
}

func (r *docxReader) xml(name string) *xmltree.Element {
	entry := r.entries[name]
	if entry == nil {
		return nil
	}
	rc, err := entry.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	// Whitespace matters: <w:t xml:space="preserve"> </w:t> is a real space.
	d, err := xmltree.Parse(rc)
	if err != nil {
		return nil
	}
	return d.Root
}

// relationshipsOf are the relationships of a part ("" = the package), with targets resolved to zip paths.
func (r *docxReader) relationshipsOf(partPath string) map[string]docxRel {
	folder := ""
	if i := strings.LastIndex(partPath, "/"); i >= 0 {
		folder = partPath[:i]
	}
	relsPath := "_rels/.rels"
	if partPath != "" {
		relsPath = strings.TrimLeft(folder+"/_rels/"+partPath[strings.LastIndex(partPath, "/")+1:]+".rels", "/")
	}
	result := map[string]docxRel{}
	root := r.xml(relsPath)
	for _, rel := range root.ChildrenNS(nsPkgR, "Relationship") {
		target := rel.A("Target")
		external := rel.A("TargetMode") == "External"
		if !external {
			target = resolvePath(folder, target)
		}
		result[rel.A("Id")] = docxRel{rel.A("Type"), target, external}
	}
	return result
}

func resolvePath(folder, target string) string {
	if strings.HasPrefix(target, "/") {
		return strings.TrimLeft(target, "/")
	}
	var parts []string
	if folder != "" {
		parts = strings.Split(folder, "/")
	}
	unescaped, err := url.PathUnescape(target)
	if err != nil {
		unescaped = target
	}
	for _, segment := range strings.Split(unescaped, "/") {
		switch segment {
		case "..":
			if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}
		case ".", "":
		default:
			parts = append(parts, segment)
		}
	}
	return strings.Join(parts, "/")
}

func (r *docxReader) relatedPart(from *docxPart, suffix string) *docxPart {
	for _, rel := range from.rels {
		if !rel.external && strings.HasSuffix(rel.kind, suffix) && rel.target != "" {
			return r.loadPart(rel.target)
		}
	}
	return nil
}

// withPart runs read with relationship ids resolved against part.
func (r *docxReader) withPart(part *docxPart, read func()) {
	previous := r.part
	r.part = part
	defer func() { r.part = previous }()
	read()
}

// ───────────────────────── Document and sections ─────────────────────────

func (r *docxReader) readDocument() (*doc.Document, error) {
	main := r.part
	r.loadStyles(r.relatedPart(main, "/styles"))
	theme := r.relatedPart(main, "/theme")
	r.loadTheme(theme)
	r.loadThemeColors(theme)
	r.loadNumbering(r.relatedPart(main, "/numbering"))
	if settings := r.relatedPart(main, "/settings"); settings != nil {
		if e := settings.root.ChildNS(nsW, "evenAndOddHeaders"); e != nil {
			v := wval(e)
			r.evenOdd = v != "0" && v != "false"
		}
	}
	r.footnotesP = r.relatedPart(main, "/footnotes")
	r.endnotesP = r.relatedPart(main, "/endnotes")
	if r.footnotesP != nil {
		for _, n := range r.footnotesP.root.ChildrenNS(nsW, "footnote") {
			r.footnotes[wattr(n, "id")] = n
		}
	}
	if r.endnotesP != nil {
		for _, n := range r.endnotesP.root.ChildrenNS(nsW, "endnote") {
			r.endnotes[wattr(n, "id")] = n
		}
	}

	d := &doc.Document{Title: r.title()}
	body := main.root.ChildNS(nsW, "body")
	if body == nil {
		return nil, errors.New("word/document.xml has no body")
	}
	// A section ends with the paragraph whose properties hold its sectPr; the last one is the body's own sectPr.
	flat := flatten(body.Elements())
	for _, e := range flat {
		if e.Is(nsW, "p") {
			if sectPr := e.ChildNS(nsW, "pPr").ChildNS(nsW, "sectPr"); sectPr != nil {
				r.linePitches = append(r.linePitches, linePitch(sectPr))
			}
		}
	}
	r.linePitches = append(r.linePitches, linePitch(body.ChildNS(nsW, "sectPr")))

	var blocks []doc.Block
	headers := map[string][]doc.Block{}
	footers := map[string][]doc.Block{}
	for _, e := range flat {
		if e.Is(nsW, "sectPr") {
			continue
		}
		blocks = r.readBlock(e, blocks)
		if e.Is(nsW, "p") {
			if sectPr := e.ChildNS(nsW, "pPr").ChildNS(nsW, "sectPr"); sectPr != nil {
				r.addSection(d, blocks, sectPr, headers, footers)
				blocks = nil
				r.section++
			}
		}
	}
	r.addSection(d, blocks, body.ChildNS(nsW, "sectPr"), headers, footers)
	return d, nil
}

func (r *docxReader) title() string {
	core := r.xml("docProps/core.xml")
	if t := core.FirstDescendantNS(nsDC, "title"); t != nil {
		return nullIfEmpty(t.Value())
	}
	return ""
}

func (r *docxReader) addSection(d *doc.Document, blocks []doc.Block, sectPr *xmltree.Element, headers, footers map[string][]doc.Block) {
	page := pageOf(sectPr)
	columns := columnsOfSectPr(sectPr)
	var previous *doc.Section
	if n := len(d.Sections); n > 0 {
		previous = d.Sections[n-1]
	}
	// Word uses continuous section breaks to change the number of columns mid-page. 한글 does that with a column
	// definition inside the section, so keep one section when the paper does not change.
	if previous != nil && wval(sectPr.ChildNS(nsW, "type")) == "continuous" && previous.Page != nil && *previous.Page == page {
		first, ok := firstParagraph(blocks)
		if !ok {
			first = &doc.Paragraph{}
			blocks = append([]doc.Block{first}, blocks...)
		}
		c := columns
		first.ColumnsChange = &c
		previous.Blocks = append(previous.Blocks, blocks...)
		return
	}
	section := doc.NewSection()
	section.Page = &page
	section.Columns = columns
	section.Blocks = blocks

	// Sections without their own header or footer reference inherit the previous section's.
	for _, ref := range sectPr.ChildrenNS(nsW, "headerReference") {
		headers[or(wattr(ref, "type"), "default")] = r.headerFooterBlocks(ref)
	}
	for _, ref := range sectPr.ChildrenNS(nsW, "footerReference") {
		footers[or(wattr(ref, "type"), "default")] = r.headerFooterBlocks(ref)
	}
	add := func(source map[string][]doc.Block, target *[]*doc.HeaderFooter) {
		if r.evenOdd {
			if odd, ok := source["default"]; ok {
				*target = append(*target, &doc.HeaderFooter{Pages: doc.OddPages, Blocks: odd})
			}
			if even, ok := source["even"]; ok {
				*target = append(*target, &doc.HeaderFooter{Pages: doc.EvenPages, Blocks: even})
			}
		} else if both, ok := source["default"]; ok {
			*target = append(*target, &doc.HeaderFooter{Pages: doc.BothPages, Blocks: both})
		}
	}
	add(headers, &section.Headers)
	add(footers, &section.Footers)

	// "Different first page": 한글 can only hide the header/footer on the first page, which is what an empty (or
	// missing) first-page header means in Word.
	if titlePg := sectPr.ChildNS(nsW, "titlePg"); titlePg != nil && wval(titlePg) != "0" && wval(titlePg) != "false" {
		section.HideFirstHeader = isBlankBlocks(headers["first"])
		section.HideFirstFooter = isBlankBlocks(footers["first"])
	}
	if start, err := strconv.Atoi(wattr(sectPr.ChildNS(nsW, "pgNumType"), "start")); err == nil {
		section.StartPageNumber = ptr(start)
	}
	d.Sections = append(d.Sections, section)
}

func firstParagraph(blocks []doc.Block) (*doc.Paragraph, bool) {
	if len(blocks) == 0 {
		return nil, false
	}
	p, ok := blocks[0].(*doc.Paragraph)
	return p, ok
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func (r *docxReader) headerFooterBlocks(ref *xmltree.Element) []doc.Block {
	var blocks []doc.Block
	id, ok := ref.AttrNS(nsR, "id")
	if !ok {
		return blocks
	}
	rel, ok := r.part.rels[id]
	if !ok {
		return blocks
	}
	part := r.loadPart(rel.target)
	if part == nil {
		return blocks
	}
	r.withPart(part, func() {
		for _, e := range flatten(part.root.Elements()) {
			blocks = r.readBlock(e, blocks)
		}
	})
	return blocks
}

func isBlankBlocks(blocks []doc.Block) bool {
	for _, b := range blocks {
		p, ok := b.(*doc.Paragraph)
		if !ok {
			return false
		}
		for _, in := range p.Inlines {
			switch v := in.(type) {
			case *doc.Text:
				if strings.TrimSpace(v.Text) != "" {
					return false
				}
			case *doc.Bookmark:
			default:
				return false
			}
		}
	}
	return true
}

func pageOf(sectPr *xmltree.Element) doc.Page {
	size := sectPr.ChildNS(nsW, "pgSz")
	margin := sectPr.ChildNS(nsW, "pgMar")
	get := func(e *xmltree.Element, name string, fallback int) int {
		v := twips(e, name)
		if v == nil {
			return abs(fallback)
		}
		return abs(*v)
	}
	width := get(size, "w", 11906)
	height := get(size, "h", 16838)
	if wattr(size, "orient") == "landscape" && width < height {
		width, height = height, width
	}
	// Word: top = paper edge → body, header = paper edge → header text.
	// 한글: top = paper edge → header, header = header area height (body starts at top + header).
	top := get(margin, "top", 1440)
	bottom := get(margin, "bottom", 1440)
	headerDistance := min(get(margin, "header", 720), top)
	footerDistance := min(get(margin, "footer", 720), bottom)
	return doc.Page{
		Width: doc.FromTwips(float64(width)), Height: doc.FromTwips(float64(height)),
		Left: doc.FromTwips(float64(get(margin, "left", 1440))), Right: doc.FromTwips(float64(get(margin, "right", 1440))),
		Top: doc.FromTwips(float64(headerDistance)), Bottom: doc.FromTwips(float64(footerDistance)),
		Header: doc.FromTwips(float64(top - headerDistance)), Footer: doc.FromTwips(float64(bottom - footerDistance)),
		Gutter: doc.FromTwips(float64(get(margin, "gutter", 0))),
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// linePitch is the line pitch of a "lines" document grid (twips), or 0.
func linePitch(sectPr *xmltree.Element) int {
	grid := sectPr.ChildNS(nsW, "docGrid")
	switch wattr(grid, "type") {
	case "lines", "linesAndChars", "snapToChars":
		return val(twips(grid, "linePitch"), 0)
	}
	return 0
}

// snapToGrid: Word's document grid ("lines") puts every line on a multiple of the grid pitch: a single-spaced 10 pt
// line with an 18 pt grid is 18 pt high. 한글 has no such grid for body text, so the paragraph gets an "at least"
// line spacing of the snapped height (larger content such as pictures can still grow the line).
func (r *docxReader) snapToGrid(p *doc.Paragraph, props *paraProps) {
	pitch := 0
	if r.textBoxes == 0 && r.section < len(r.linePitches) {
		pitch = r.linePitches[r.section]
	}
	if pitch <= 0 || isFalse(props.snapToGrid) || props.lineRule == "exact" || props.lineRule == "atLeast" {
		return
	}
	size := maxFontSize(p.Inlines)
	if size == 0 {
		size = val(p.MarkFormat.Size, 1000)
	}
	cell := doc.FromTwips(float64(pitch))
	natural := float64(size) * wordLineToHwpPercent / 100.0 // 1/100 pt = HWPUNIT
	lines := max(1, ceil(natural/float64(cell)))
	multiple := float64(val(props.line, 240)) / 240.0
	p.Format.LineSpacing = &doc.LineSpacing{Kind: doc.SpacingAtLeast, Value: iround(float64(lines) * float64(cell) * multiple)}
}

func ceil(v float64) int {
	i := int(v)
	if float64(i) < v {
		i++
	}
	return i
}

func maxFontSize(inlines []doc.Inline) int {
	m := 0
	for _, in := range inlines {
		size := 0
		switch v := in.(type) {
		case *doc.Text:
			size = val(v.Format.Size, 0)
		case *doc.Equation:
			size = val(v.Text.Format.Size, 0)
		case *doc.Link:
			size = maxFontSize(v.Content)
		case *doc.Field:
			size = val(v.Format.Size, 0)
		}
		m = max(m, size)
	}
	return m
}

func columnsOfSectPr(sectPr *xmltree.Element) doc.Columns {
	cols := sectPr.ChildNS(nsW, "cols")
	count := 1
	if n, err := strconv.Atoi(wattr(cols, "num")); err == nil {
		count = n
	} else if cols != nil {
		count = len(cols.ChildrenNS(nsW, "col"))
	}
	if count <= 1 {
		return doc.SingleColumn
	}
	return doc.Columns{Count: min(count, 255), Gap: doc.FromTwips(float64(val(twips(cols, "space"), 720))), Separator: isTrue(boolAttr(cols, "sep"))}
}

// ───────────────────────── Blocks ─────────────────────────

// flatten yields paragraphs and tables, looking through content controls, custom XML and tracked insertions.
func flatten(elements []*xmltree.Element) []*xmltree.Element {
	var out []*xmltree.Element
	for _, e := range elements {
		switch {
		case e.Is(nsW, "sdt"):
			out = append(out, flatten(e.ChildNS(nsW, "sdtContent").Elements())...)
		case e.Is(nsW, "customXml"), e.Is(nsW, "ins"), e.Is(nsW, "moveTo"):
			out = append(out, flatten(e.Elements())...)
		case e.Is(nsW, "p"), e.Is(nsW, "tbl"), e.Is(nsW, "sectPr"):
			out = append(out, e)
		}
	}
	return out
}

func (r *docxReader) readBlocks(elements []*xmltree.Element, output []doc.Block) []doc.Block {
	for _, e := range flatten(elements) {
		output = r.readBlock(e, output)
	}
	return output
}

func (r *docxReader) readBlock(e *xmltree.Element, output []doc.Block) []doc.Block {
	switch {
	case e.Is(nsW, "p"):
		return r.readParagraph(e, output)
	case e.Is(nsW, "tbl"):
		return append(output, r.readTable(e))
	}
	return output
}

// ───────────────────────── Paragraphs ─────────────────────────

// docxParagraph builds the paragraph(s) for one w:p. A page or column break inside the text starts a new 한글
// paragraph (breaks are paragraph attributes there), so one Word paragraph can become several.
type docxParagraph struct {
	reader  *docxReader
	output  []doc.Block
	format  doc.ParaFormat
	mark    doc.CharFormat
	heading int
	list    *doc.ListRef
	props   *paraProps
	sinks   []*doc.Link
	current *doc.Paragraph
}

func (r *docxReader) newDocxParagraph(output []doc.Block, props *paraProps, mark *runProps, pPr *xmltree.Element) *docxParagraph {
	b := &docxParagraph{reader: r, output: output, props: props}
	b.format = r.paraFormat(props, mark.size)
	b.mark = r.charFormat(mark)
	b.heading = r.headingLevel(props, pPr)
	if n := r.numbering(props.numID); n != nil {
		b.list = &doc.ListRef{Numbering: n, Level: clamp(val(props.ilvl, 0), 0, 8), Numbered: true}
	}
	b.current = b.newParagraph(b.list)
	b.current.PageBreakBefore = isTrue(props.pageBreakBefore)
	// A hyperlink field that started in an earlier paragraph (Word's table of contents) continues here.
	for i := len(r.fields) - 1; i >= 0; i-- {
		if f := r.fields[i]; f.result && f.target != "" {
			b.openLink(f.target)
			break
		}
	}
	return b
}

func (b *docxParagraph) newParagraph(list *doc.ListRef) *doc.Paragraph {
	return &doc.Paragraph{Format: b.format, MarkFormat: b.mark, HeadingLevel: b.heading, List: list}
}

func (b *docxParagraph) add(in doc.Inline) {
	if n := len(b.sinks); n > 0 {
		b.sinks[n-1].Content = append(b.sinks[n-1].Content, in)
	} else {
		b.current.Inlines = append(b.current.Inlines, in)
	}
}

func (b *docxParagraph) openLink(target string) {
	link := &doc.Link{Target: target}
	b.add(link)
	b.sinks = append(b.sinks, link)
}

func (b *docxParagraph) closeLink() {
	if n := len(b.sinks); n > 0 {
		b.sinks = b.sinks[:n-1]
	}
}

// breakAt continues in a new paragraph that starts on a new page or column.
func (b *docxParagraph) breakAt(page bool) {
	if len(b.current.Inlines) > 0 {
		b.emit()
		var list *doc.ListRef
		if b.list != nil {
			l := *b.list
			l.Numbered = false
			list = &l
		}
		b.current = b.newParagraph(list)
		b.sinks = nil
	}
	if page {
		b.current.PageBreakBefore = true
	} else {
		b.current.ColumnBreakBefore = true
	}
}

func (b *docxParagraph) finish() []doc.Block {
	b.emit()
	return b.output
}

func (b *docxParagraph) emit() {
	b.reader.snapToGrid(b.current, b.props)
	b.output = append(b.output, b.current)
}

func (r *docxReader) readParagraph(p *xmltree.Element, output []doc.Block) []doc.Block {
	pPr := p.ChildNS(nsW, "pPr")
	props, mark := r.paragraphProperties(pPr)
	b := r.newDocxParagraph(output, props, mark, pPr)
	// Run formatting starts from the paragraph style, not from the paragraph mark's direct formatting.
	styleID := wval(pPr.ChildNS(nsW, "pStyle"))
	if styleID == "" {
		styleID = r.defaultParaStyle
	}
	_, styleRun := r.resolveStyle(styleID)
	r.paragraphContent(p.Elements(), b, styleRun)
	return b.finish()
}

func (r *docxReader) paragraphContent(elements []*xmltree.Element, b *docxParagraph, styleRun *runProps) {
	for _, e := range elements {
		if e.Space == nsM {
			if e.Local == "oMath" || e.Local == "oMathPara" {
				r.equations(e, b, styleRun)
			}
			continue
		}
		if e.Space != nsW {
			if e.Is(nsMC, "AlternateContent") {
				r.paragraphContent(chooseAlternative(e), b, styleRun)
			}
			continue
		}
		switch e.Local {
		case "r":
			r.run(e, b, styleRun)
		case "hyperlink":
			target := ""
			if id, ok := e.AttrNS(nsR, "id"); ok {
				if rel, ok := r.part.rels[id]; ok {
					target = rel.target
				}
			}
			if target == "" {
				if anchor := wattr(e, "anchor"); anchor != "" {
					target = "#" + anchor
				}
			}
			if target != "" {
				b.openLink(target)
			}
			r.paragraphContent(e.Elements(), b, styleRun)
			if target != "" {
				b.closeLink()
			}
		case "fldSimple":
			field := &fieldState{}
			field.instruction.WriteString(wattr(e, "instr"))
			r.fields = append(r.fields, field)
			r.startFieldResult(field, b, r.charFormat(styleRun))
			r.paragraphContent(e.Elements(), b, styleRun)
			r.endField(b)
		case "bookmarkStart":
			if name := wattr(e, "name"); name != "" && name != "_GoBack" {
				b.add(&doc.Bookmark{Name: name})
			}
		case "ins", "moveTo", "smartTag", "customXml", "dir", "bdo":
			r.paragraphContent(e.Elements(), b, styleRun)
		case "sdt":
			r.paragraphContent(e.ChildNS(nsW, "sdtContent").Elements(), b, styleRun)
		}
		// pPr, del, moveFrom, proofErr, permStart, commentRangeStart ...: no visible content
	}
}

func (r *docxReader) run(e *xmltree.Element, b *docxParagraph, styleRun *runProps) {
	props := r.runProperties(e.ChildNS(nsW, "rPr"), styleRun)
	if isTrue(props.hidden) {
		return
	}
	format := r.charFormat(props)
	for _, c := range e.Elements() {
		if c.Is(nsMC, "AlternateContent") {
			for _, chosen := range chooseAlternative(c) {
				r.runChild(chosen, b, format)
			}
			continue
		}
		r.runChild(c, b, format)
	}
}

func (r *docxReader) runChild(c *xmltree.Element, b *docxParagraph, format doc.CharFormat) {
	if c.Space != nsW {
		return
	}
	switch c.Local {
	case "t":
		if r.showsText() {
			// Text typed in Wingdings & co. ("þ" for ☑) becomes the Unicode characters it shows.
			for _, t := range SymbolRuns(c.Value(), format) {
				b.add(t)
			}
		}
	case "tab", "ptab":
		if r.showsText() {
			b.add(&doc.Tab{Format: format})
		}
	case "br":
		if !r.showsText() {
			return
		}
		switch wattr(c, "type") {
		case "page":
			b.breakAt(true)
		case "column":
			b.breakAt(false)
		default:
			b.add(&doc.LineBreak{Format: format})
		}
	case "cr":
		if r.showsText() {
			b.add(&doc.LineBreak{Format: format})
		}
	case "noBreakHyphen":
		if r.showsText() {
			b.add(&doc.Text{Text: "-", Format: format})
		}
	case "sym":
		// Insert → Symbol: a code of the font in w:font (☑ is F052 in Wingdings 2), in Unicode where known.
		if code, err := strconv.ParseUint(wattr(c, "char"), 16, 32); r.showsText() && err == nil {
			if symbol := SymbolCode(wattr(c, "font"), int(code)); symbol != "" {
				b.add(&doc.Text{Text: symbol, Format: WithoutSymbolFont(format)})
			} else {
				b.add(&doc.Text{Text: bulletText(string(rune(code)), ""), Format: format})
			}
		}
	case "fldChar":
		r.fieldChar(c, b, format)
	case "instrText":
		if n := len(r.fields); n > 0 && !r.fields[n-1].result {
			r.fields[n-1].instruction.WriteString(c.Value())
		}
	case "drawing":
		if r.showsText() {
			for _, in := range r.drawing(c) {
				b.add(in)
			}
		}
	case "pict", "object":
		if r.showsText() {
			for _, in := range r.vml(c, format) {
				b.add(in)
			}
		}
	case "footnoteReference":
		if n := r.note(c, r.footnotes, r.footnotesP, false); n != nil {
			b.add(n)
		}
	case "endnoteReference":
		if n := r.note(c, r.endnotes, r.endnotesP, true); n != nil {
			b.add(n)
		}
	}
	// footnoteRef (the number, 한글 adds its own), separator, lastRenderedPageBreak, delText ...
}

// equations: Word math (m:oMath, or m:oMathPara with one or more lines) as equations in the size of its runs. A
// display equation alone in its paragraph is aligned like Word shows it (centred unless m:jc says otherwise).
func (r *docxReader) equations(math *xmltree.Element, b *docxParagraph, styleRun *runProps) {
	var rPr *xmltree.Element
	for _, mr := range math.DescendantsNS(nsM, "r") {
		if p := mr.ChildNS(nsW, "rPr"); p != nil {
			rPr = p
			break
		}
	}
	format := r.charFormat(r.runProperties(rPr, styleRun))
	format.Italic = ptr(true)
	lines := []*xmltree.Element{math}
	if math.Local == "oMathPara" {
		lines = math.ChildrenNS(nsM, "oMath")
		if paragraph := math.Parent; paragraph != nil && !hasTextOutsideMath(paragraph) {
			align := doc.AlignCenter
			jc, _ := math.ChildNS(nsM, "oMathParaPr").ChildNS(nsM, "jc").AttrNS(nsM, "val")
			switch jc {
			case "left":
				align = doc.AlignLeft
			case "right":
				align = doc.AlignRight
			}
			b.current.Format.Align = ptr(align)
		}
	}
	first := true
	for _, line := range lines {
		equation := ReadOmml(line)
		if row, ok := equation.(*MathRow); ok && len(row.Items) == 0 {
			continue
		}
		if !first {
			b.add(&doc.LineBreak{Format: format})
		}
		b.add(NewEquation(equation, format))
		first = false
	}
}

func hasTextOutsideMath(paragraph *xmltree.Element) bool {
	for _, t := range paragraph.DescendantsNS(nsW, "t") {
		if strings.TrimSpace(t.Value()) == "" {
			continue
		}
		inside := false
		for _, a := range t.Ancestors() {
			if a.Is(nsM, "oMathPara") {
				inside = true
				break
			}
		}
		if !inside {
			return true
		}
	}
	return false
}

func (r *docxReader) note(reference *xmltree.Element, notes map[string]*xmltree.Element, part *docxPart, endnote bool) *doc.Note {
	element, ok := notes[wattr(reference, "id")]
	if part == nil || !ok {
		return nil
	}
	note := &doc.Note{Endnote: endnote}
	r.withPart(part, func() { note.Blocks = r.readBlocks(element.Elements(), nil) })
	// Word notes start with the reference mark and a space; 한글 writes its own number.
	if first, ok := firstParagraph(note.Blocks); ok && len(first.Inlines) > 0 {
		if lead, ok := first.Inlines[0].(*doc.Text); ok {
			first.Inlines[0] = &doc.Text{Text: strings.TrimLeftFunc(lead.Text, unicode.IsSpace), Format: lead.Format}
		}
	}
	return note
}

// chooseAlternative: the Choice of mc:AlternateContent when it is DrawingML we understand, else the (VML) Fallback.
func chooseAlternative(alternate *xmltree.Element) []*xmltree.Element {
	for _, c := range alternate.ChildrenNS(nsMC, "Choice") {
		switch c.A("Requires") {
		case "wps", "wpg", "wp14", "w14", "a14", "wpc":
			return c.Elements()
		}
	}
	if f := alternate.ChildNS(nsMC, "Fallback"); f != nil {
		return f.Elements()
	}
	if c := alternate.ChildNS(nsMC, "Choice"); c != nil {
		return c.Elements()
	}
	return nil
}

// ───────────────────────── Fields ─────────────────────────

// fieldState is a complex field (w:fldChar begin … separate … end) that may span runs and paragraphs.
type fieldState struct {
	instruction strings.Builder
	result      bool
	suppress    bool
	target      string
}

// showsText: field result text is shown unless we are inside an instruction or a field we replace (PAGE ...).
func (r *docxReader) showsText() bool {
	for _, f := range r.fields {
		if !f.result || f.suppress {
			return false
		}
	}
	return true
}

func (r *docxReader) fieldChar(c *xmltree.Element, b *docxParagraph, format doc.CharFormat) {
	last := func() *fieldState {
		if n := len(r.fields); n > 0 {
			return r.fields[n-1]
		}
		return nil
	}
	switch wattr(c, "fldCharType") {
	case "begin":
		r.fields = append(r.fields, &fieldState{})
	case "separate":
		if f := last(); f != nil && !f.result {
			r.startFieldResult(f, b, format)
		}
	case "end":
		if f := last(); f != nil && !f.result {
			r.startFieldResult(f, b, format)
		}
		r.endField(b)
	}
}

func (r *docxReader) startFieldResult(field *fieldState, b *docxParagraph, format doc.CharFormat) {
	field.result = true
	words := splitInstruction(field.instruction.String())
	if len(words) == 0 {
		return
	}
	switch strings.ToUpper(words[0]) {
	case "HYPERLINK":
		// HYPERLINK "url" [\l "bookmark"] or HYPERLINK \l "bookmark"
		var urlText, anchor string
		hasURL, hasAnchor := false, false
		for i := 1; i < len(words); i++ {
			switch {
			case words[i] == `\l` && i+1 < len(words):
				i++
				anchor, hasAnchor = words[i], true
			case !strings.HasPrefix(words[i], `\`) && !hasURL:
				urlText, hasURL = words[i], true
			case strings.HasPrefix(words[i], `\`) && i+1 < len(words) && !strings.HasPrefix(words[i+1], `\`):
				i++ // other switch with an argument (\o "tooltip", \t "frame")
			}
		}
		if hasAnchor {
			field.target = urlText + "#" + anchor
		} else if hasURL {
			field.target = urlText
		}
		if field.target != "" {
			b.openLink(field.target)
		}
	case "PAGE":
		b.add(&doc.Field{Kind: doc.PageNumberField, Format: format})
		field.suppress = true
	case "NUMPAGES", "SECTIONPAGES":
		b.add(&doc.Field{Kind: doc.TotalPagesField, Format: format})
		field.suppress = true
	}
}

func (r *docxReader) endField(b *docxParagraph) {
	n := len(r.fields)
	if n == 0 {
		return
	}
	field := r.fields[n-1]
	r.fields = r.fields[:n-1]
	if field.target != "" {
		b.closeLink()
	}
}

// splitInstruction splits a field instruction into words, keeping quoted arguments together.
func splitInstruction(instruction string) []string {
	var words []string
	var current strings.Builder
	quoted := false
	for _, ch := range instruction {
		if ch == '"' {
			quoted = !quoted
			continue
		}
		if unicode.IsSpace(ch) && !quoted {
			if current.Len() > 0 {
				words = append(words, current.String())
			}
			current.Reset()
			continue
		}
		current.WriteRune(ch)
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}
	return words
}

// ───────────────────────── Media ─────────────────────────

func (r *docxReader) extractImage(relationshipID string) string {
	rel, ok := r.part.rels[relationshipID]
	if relationshipID == "" || !ok || rel.external {
		return ""
	}
	if p, ok := r.media[rel.target]; ok {
		return p
	}
	entry := r.entries[rel.target]
	if entry == nil {
		return ""
	}
	if os.MkdirAll(r.mediaFolder, 0o755) != nil {
		return ""
	}
	target := filepath.Join(r.mediaFolder, fmt.Sprintf("docx-media-%d%s", len(r.media)+1, path.Ext(rel.target)))
	if !extractEntry(entry, target) {
		return ""
	}
	r.media[rel.target] = target
	return target
}

func extractEntry(entry *zip.File, target string) bool {
	rc, err := entry.Open()
	if err != nil {
		return false
	}
	defer rc.Close()
	out, err := os.Create(target)
	if err != nil {
		return false
	}
	_, err = io.Copy(out, rc)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err == nil
}
