package hwpx

// Reads HWPX (한글 / OWPML) packages into the document model — the inverse of the writer — so HWP and HWPX can become
// DOCX, HTML or EPUB without Hancom Office or LibreOffice (HWP is turned into HWPX by rhwp first).
//
// Files come from 한글 itself, from rhwp's export-hwpx and from Filee's own writer, which differ in details. The reader
// therefore matches elements by local name (any prefix, any namespace version), resolves hp:switch blocks and looks
// every idRef up in header.xml. Split over files:
//   - reader.go          package: container, manifest and spine, pictures (BinData), section setup, XML helpers;
//   - reader_header.go   header.xml: fonts, character and paragraph shapes, numberings, bullets, borders, tab stops;
//   - reader_body.go     paragraphs, runs, text, fields (hyperlinks), notes, bookmarks and tables;
//   - reader_objects.go  pictures, drawing objects, text boxes, groups and equations.

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/hwp/xmltree"
)

// hp:case branches written for readers that know these namespaces hold the exact values.
const (
	hwpUnitCharMarker      = "HwpUnitChar"
	paragraph2016Namespace = "http://www.hancom.co.kr/hwpml/2016/paragraph"
)

func init() {
	doc.RegisterReader("hwpx", func(p, media string) (*doc.Document, error) { return ReadHwpx(p, media, nil) })
}

type hwpxReader struct {
	zip         *zip.Reader
	entries     map[string]*zip.File
	mediaFolder string
	// binaryItems maps manifest item ids to zip entries of embedded binaries (pictures).
	binaryItems map[string]string
	// media maps binary item ids to extracted files ("" when the item is missing).
	media map[string]string
	// skipped counts the elements the model cannot hold, by local name.
	skipped map[string]int

	header headerState
	body   bodyState
}

// ReadHwpx reads an .hwpx file; embedded pictures are extracted into mediaFolder. skipped, when not nil, receives
// the elements that were skipped because the model has no equivalent (local name → count).
func ReadHwpx(p, mediaFolder string, skipped map[string]int) (*doc.Document, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, notAPackage(data)
	}
	r := &hwpxReader{zip: z, entries: map[string]*zip.File{}, mediaFolder: mediaFolder,
		binaryItems: map[string]string{}, media: map[string]string{}, skipped: map[string]int{}}
	for _, f := range z.File {
		r.entries[f.Name] = f
	}
	d, err := r.readDocument()
	if err != nil {
		return nil, err
	}
	for k, v := range r.skipped {
		if skipped != nil {
			skipped[k] = v
		}
	}
	return d, nil
}

// notAPackage is a clear message for files that are not a zip package: HWP 5.0 binaries and DRM-wrapped files.
func notAPackage(data []byte) error {
	if bytes.HasPrefix(data, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}) {
		return errors.New("the file is an HWP 5.0 (binary) document, not HWPX. Rename it to .hwp and convert it again")
	}
	return errors.New("the file is not an HWPX package. It may be damaged or protected by document security (DRM) software")
}

// ───────────────────────── Package ─────────────────────────

func (r *hwpxReader) readDocument() (*doc.Document, error) {
	if err := r.rejectEncrypted(); err != nil {
		return nil, err
	}
	packagePath := r.rootFile()
	pkg, err := r.loadXML(packagePath, false)
	if err != nil {
		return nil, err
	}
	folder := ""
	if i := strings.LastIndex(packagePath, "/"); i >= 0 {
		folder = packagePath[:i+1]
	}
	// Manifest items: id → zip path. Hrefs are relative to the package root in files from 한글, but resolve them
	// against the manifest's folder too for other producers.
	items := map[string]string{}
	var itemOrder []string
	for _, item := range descendants(pkg, "item") {
		id, href := item.A("id"), item.A("href")
		if id == "" || href == "" {
			continue
		}
		entry := r.entryPath(href, folder)
		if entry == "" {
			continue
		}
		if _, ok := items[id]; !ok {
			itemOrder = append(itemOrder, id)
		}
		items[id] = entry
		if strings.HasPrefix(strings.ToLower(entry), "bindata/") && item.A("isEmbeded") != "0" {
			r.binaryItems[id] = entry
		}
	}
	headerPath := "Contents/header.xml"
	for _, id := range itemOrder {
		if strings.HasSuffix(strings.ToLower(items[id]), "header.xml") {
			headerPath = items[id]
			break
		}
	}
	header, err := r.loadXML(headerPath, true)
	if err != nil {
		return nil, err
	}
	r.loadHeader(header)

	d := &doc.Document{}
	if t := firstDescendant(pkg, "title"); t != nil && strings.TrimSpace(t.Value()) != "" {
		d.Title = strings.TrimSpace(t.Value())
	}
	for _, sectionPath := range r.sectionPaths(pkg, items, itemOrder) {
		root, err := r.loadXML(sectionPath, true)
		if err != nil {
			return nil, err
		}
		r.readSection(root, d)
	}
	if len(d.Sections) == 0 {
		return nil, errors.New("the HWPX file has no sections (Contents/section0.xml is missing)")
	}
	return d, nil
}

// rejectEncrypted: password-protected HWPX files keep their parts encrypted, declared in META-INF/manifest.xml.
func (r *hwpxReader) rejectEncrypted() error {
	manifest, err := r.loadXML("META-INF/manifest.xml", false)
	if err != nil {
		return nil
	}
	if len(descendants(manifest, "encryption-data")) > 0 {
		return errors.New("the HWPX file is password-protected. Open it in 한글 (Hangul), remove the password, save it and convert it again")
	}
	return nil
}

// rootFile is the package document (content.hpf) named in META-INF/container.xml.
func (r *hwpxReader) rootFile() string {
	container, _ := r.loadXML("META-INF/container.xml", false)
	var hpf *xmltree.Element
	roots := descendants(container, "rootfile")
	for _, rf := range roots {
		if rf.A("media-type") == "application/hwpml-package+xml" {
			hpf = rf
			break
		}
	}
	if hpf == nil {
		for _, rf := range roots {
			if strings.HasSuffix(strings.ToLower(rf.A("full-path")), ".hpf") {
				hpf = rf
				break
			}
		}
	}
	if v, ok := hpf.Attr("full-path"); ok {
		return v
	}
	return "Contents/content.hpf"
}

func isSectionFile(p string) bool {
	name := strings.ToLower(path.Base(p))
	return strings.HasPrefix(name, "section") && strings.HasSuffix(name, ".xml")
}

// sectionPaths are the section parts in reading order: the spine, else Contents/sectionN.xml by number.
func (r *hwpxReader) sectionPaths(pkg *xmltree.Element, items map[string]string, order []string) []string {
	var result []string
	seen := map[string]bool{}
	for _, ref := range descendants(pkg, "itemref") {
		if p, ok := items[ref.A("idref")]; ok && isSectionFile(p) && !seen[p] {
			seen[p] = true
			result = append(result, p)
		}
	}
	if len(result) > 0 {
		return result
	}
	type numbered struct {
		name string
		n    int
	}
	var sections []numbered
	for _, f := range r.zip.File {
		if strings.HasPrefix(strings.ToLower(f.Name), "contents/") && isSectionFile(f.Name) {
			base := strings.TrimSuffix(path.Base(f.Name), path.Ext(f.Name))
			n, err := strconv.Atoi(base[7:])
			if err != nil {
				n = math.MaxInt
			}
			sections = append(sections, numbered{f.Name, n})
		}
	}
	sort.SliceStable(sections, func(i, j int) bool { return sections[i].n < sections[j].n })
	for _, s := range sections {
		result = append(result, s.name)
	}
	return result
}

func (r *hwpxReader) entryPath(href, folder string) string {
	p, err := url.PathUnescape(href)
	if err != nil {
		p = href
	}
	p = strings.TrimLeft(strings.ReplaceAll(p, "\\", "/"), "/")
	if r.entries[p] != nil {
		return p
	}
	if folder != "" && r.entries[folder+p] != nil {
		return folder + p
	}
	return p
}

func (r *hwpxReader) loadXML(name string, required bool) (*xmltree.Element, error) {
	entry := r.entries[name]
	if entry == nil {
		if required {
			return nil, fmt.Errorf("the HWPX file is incomplete: %s is missing", name)
		}
		return nil, nil
	}
	rc, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	// Whitespace matters: <hp:t> </hp:t> is a real space.
	d, err := xmltree.ParseBytes(data)
	if err != nil {
		// Encrypted (distribution / DRM) documents have binary parts where XML is expected.
		return nil, fmt.Errorf("the HWPX file cannot be read (%s is not valid XML). It may be encrypted or damaged. %v", name, err)
	}
	return d.Root, nil
}

// extractBinary extracts a BinData item (by manifest id) into the media folder once; "" when it is missing.
func (r *hwpxReader) extractBinary(itemID string) string {
	if itemID == "" {
		return ""
	}
	if p, ok := r.media[itemID]; ok {
		return p
	}
	// Without a manifest entry, 한글's naming (BinData/<id>.<ext>) still finds the file.
	entryPath := r.binaryItems[itemID]
	if entryPath == "" {
		for _, f := range r.zip.File {
			name := path.Base(f.Name)
			if strings.HasPrefix(strings.ToLower(f.Name), "bindata/") && strings.EqualFold(strings.TrimSuffix(name, path.Ext(name)), itemID) {
				entryPath = f.Name
				break
			}
		}
	}
	result := ""
	if entry := r.entries[entryPath]; entry != nil {
		if os.MkdirAll(r.mediaFolder, 0o755) == nil {
			target := filepath.Join(r.mediaFolder, fmt.Sprintf("hwpx-media-%d%s", len(r.media)+1, strings.ToLower(path.Ext(entry.Name))))
			if rc, err := entry.Open(); err == nil {
				if out, err := os.Create(target); err == nil {
					_, cerr := io.Copy(out, rc)
					out.Close()
					if cerr == nil {
						result = target
					}
				}
				rc.Close()
			}
		}
	}
	r.media[itemID] = result
	return result
}

// ───────────────────────── Sections ─────────────────────────

// sectionState is the state of the section being read. A secPr starts a 한글 section (구역); its first colPr gives the
// columns and later ones change them mid-page. Header, footer and page number controls may sit in any body paragraph.
type sectionState struct {
	document       *doc.Document
	section        *doc.Section
	columnsSet     bool
	startNumberSet bool
	pageNumber     *xmltree.Element
}

func (s *sectionState) current() *doc.Section {
	if s.section == nil {
		return s.begin(nil)
	}
	return s.section
}

func (s *sectionState) begin(section *doc.Section) *doc.Section {
	s.finish()
	if section == nil {
		section = doc.NewSection()
	}
	s.section = section
	s.columnsSet = false
	s.startNumberSet = section.StartPageNumber != nil
	s.pageNumber = nil
	s.document.Sections = append(s.document.Sections, section)
	return section
}

// finish turns an automatic page number (hp:pageNum, "쪽 번호 매기기") into a header or footer paragraph.
func (s *sectionState) finish() {
	if s.section == nil || s.pageNumber == nil {
		return
	}
	control := s.pageNumber
	s.pageNumber = nil
	position, ok := control.Attr("pos")
	if !ok {
		position = "BOTTOM_CENTER"
	}
	if position == "NONE" {
		return
	}
	side := control.A("sideChar")
	if side == "\x00" {
		side = ""
	}
	align := doc.AlignCenter
	switch {
	case strings.HasSuffix(position, "LEFT") || strings.HasPrefix(position, "INSIDE"):
		align = doc.AlignLeft
	case strings.HasSuffix(position, "RIGHT") || strings.HasPrefix(position, "OUTSIDE"):
		align = doc.AlignRight
	}
	paragraph := &doc.Paragraph{Format: doc.ParaFormat{Align: ptr(align)}}
	if side != "" {
		paragraph.Inlines = append(paragraph.Inlines, &doc.Text{Text: side + " "})
	}
	paragraph.Inlines = append(paragraph.Inlines, &doc.Field{Kind: doc.PageNumberField})
	if side != "" {
		paragraph.Inlines = append(paragraph.Inlines, &doc.Text{Text: " " + side})
	}
	target := &s.section.Footers
	if strings.Contains(position, "TOP") {
		target = &s.section.Headers
	}
	if len(*target) == 0 {
		*target = append(*target, &doc.HeaderFooter{Pages: doc.BothPages})
	}
	for _, item := range *target {
		item.Blocks = append(item.Blocks, paragraph)
	}
}

func (r *hwpxReader) readSection(root *xmltree.Element, d *doc.Document) {
	state := &sectionState{document: d}
	for _, e := range elements(root) {
		if e.Local == "p" {
			r.readTopParagraph(e, state)
		} else {
			r.skip(e.Local)
		}
	}
	if state.section == nil {
		state.begin(nil) // a section part without paragraphs is still a section
	}
	state.finish()
}

// readTopParagraph reads a body paragraph: a secPr in it starts a new section (normally only in a part's first one).
func (r *hwpxReader) readTopParagraph(p *xmltree.Element, state *sectionState) {
	var secPr *xmltree.Element
	for _, run := range elements(p) {
		if run.Local != "run" {
			continue
		}
		for _, e := range elements(run) {
			if e.Local == "secPr" {
				secPr = e
				break
			}
		}
		if secPr != nil {
			break
		}
	}
	if secPr != nil {
		state.begin(sectionOf(secPr))
		r.body.outlineNumberingID = secPr.A("outlineShapeIDRef")
	}
	sec := state.current()
	sec.Blocks = r.readParagraph(p, sec.Blocks, state)
}

func sectionOf(secPr *xmltree.Element) *doc.Section {
	pagePr := child(secPr, "pagePr")
	margin := child(pagePr, "margin")
	width := intAttr(pagePr, "width", 59530)
	height := intAttr(pagePr, "height", 84190)
	// Width and height describe the portrait sheet; "NARROWLY" turns it (landscape).
	if pagePr.A("landscape") == "NARROWLY" {
		width, height = max(width, height), min(width, height)
	}
	visibility := child(secPr, "visibility")
	start := intAttr(child(secPr, "startNum"), "page", 0)
	s := doc.NewSection()
	s.Page = &doc.Page{Width: width, Height: height,
		Left: intAttr(margin, "left", 7200), Right: intAttr(margin, "right", 7200),
		Top: intAttr(margin, "top", 4255), Bottom: intAttr(margin, "bottom", 4960),
		Header: intAttr(margin, "header", 4250), Footer: intAttr(margin, "footer", 2240),
		Gutter: intAttr(margin, "gutter", 0)}
	s.HideFirstHeader = flag(visibility, "hideFirstHeader", false)
	s.HideFirstFooter = flag(visibility, "hideFirstFooter", false)
	if start > 0 {
		s.StartPageNumber = ptr(start)
	}
	return s
}

func columnsOf(colPr *xmltree.Element) doc.Columns {
	count := max(1, intAttr(colPr, "colCount", 1))
	if count == 1 {
		return doc.SingleColumn
	}
	// Columns of different widths (sameSz="0") list each width and gap; the model keeps one gap.
	gap := intAttr(colPr, "sameGap", 0)
	if !flag(colPr, "sameSz", true) {
		gap = intAttr(child(colPr, "colSz"), "gap", gap)
	}
	line := child(colPr, "colLine")
	t, ok := line.Attr("type")
	return doc.Columns{Count: count, Gap: gap, Separator: line != nil && ok && t != "NONE"}
}

// ───────────────────────── XML helpers ─────────────────────────

// elements are the child elements with hp:switch blocks replaced by the branch this reader understands.
func elements(parent *xmltree.Element) []*xmltree.Element {
	if parent == nil {
		return nil
	}
	var out []*xmltree.Element
	for _, c := range parent.Elements() {
		if c.Local == "switch" {
			b, _ := branch(c)
			out = append(out, elements(b)...)
		} else {
			out = append(out, c)
		}
	}
	return out
}

// branch is the hp:case whose required namespace is known (exact HWPUNIT values, 2016 paragraph features), else
// hp:default. exact is false for hp:default, where 한글 doubles lengths for older readers.
func branch(sw *xmltree.Element) (*xmltree.Element, bool) {
	for _, b := range sw.Elements() {
		if b.Local != "case" {
			continue
		}
		required, _ := b.AttrAny("required-namespace")
		if strings.Contains(required, hwpUnitCharMarker) || required == paragraph2016Namespace {
			return b, true
		}
	}
	for _, b := range sw.Elements() {
		if b.Local == "default" {
			return b, false
		}
	}
	return nil, false
}

// child is the first child with this local name, looking into hp:switch blocks.
func child(parent *xmltree.Element, name string) *xmltree.Element {
	if parent == nil {
		return nil
	}
	for _, c := range parent.Elements() {
		if c.Local == name {
			return c
		}
		if c.Local == "switch" {
			b, _ := branch(c)
			if inner := child(b, name); inner != nil {
				return inner
			}
		}
	}
	return nil
}

func children(parent *xmltree.Element, name string) []*xmltree.Element {
	var out []*xmltree.Element
	for _, e := range elements(parent) {
		if e.Local == name {
			out = append(out, e)
		}
	}
	return out
}

func descendants(root *xmltree.Element, name string) []*xmltree.Element {
	if root == nil {
		return nil
	}
	return root.DescendantsNamed(name)
}

func firstDescendant(root *xmltree.Element, name string) *xmltree.Element {
	if root == nil {
		return nil
	}
	return root.FirstDescendant(name)
}

// intAttr is an integer attribute. Offsets are unsigned 32-bit values in the file, so negative positions written by
// some producers arrive as numbers above 2³¹ and are wrapped back.
func intAttr(e *xmltree.Element, name string, fallback int) int {
	s, ok := e.Attr(name)
	if !ok {
		return fallback
	}
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		f, ferr := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if ferr != nil {
			return fallback
		}
		return int(math.Max(math.MinInt32, math.Min(math.MaxInt32, math.RoundToEven(f))))
	}
	if v > math.MaxInt32 && v <= math.MaxUint32 {
		v -= 1 << 32
	}
	return int(max(math.MinInt32, min(math.MaxInt32, v)))
}

func flag(e *xmltree.Element, name string, fallback bool) bool {
	s, ok := e.Attr(name)
	if !ok {
		return fallback
	}
	return s == "1" || s == "true"
}

// hwpColor is "#RRGGBB" in upper case; "" for "none" or unknown values. 한글 may write "#AARRGGBB".
func hwpColor(value string) string {
	if !strings.HasPrefix(value, "#") {
		return ""
	}
	hex := value[1:]
	if len(value) == 9 {
		hex = value[3:]
	}
	if len(hex) != 6 {
		return ""
	}
	if _, err := strconv.ParseUint(hex, 16, 32); err != nil {
		return ""
	}
	return "#" + strings.ToUpper(hex)
}

func (r *hwpxReader) skip(name string) { r.skipped[name]++ }
