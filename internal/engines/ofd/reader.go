// Package ofd reads OFD (GB/T 33190-2016, the Chinese standard for
// fixed-layout documents: e-invoices, contracts, official documents) into the
// document model with no external engine, so OFD → DOCX / HWPX / TXT works
// out of the box. It registers the "ofd" document reader.
//
// An OFD file is a ZIP package of XML: OFD.xml names the document, which
// lists the pages, the template pages they are drawn on and the resources
// (fonts, pictures, drawing parameters). Like a PDF a page holds positioned
// text, pictures and vector paths, in millimetres from the top-left corner.
// Text objects are read with their CTM, character advances (DeltaX, also
// the "g" shorthand) and vertical runs; the layout groups them into lines
// and paragraphs, and grids drawn with lines become tables. Pictures are
// unpacked and kept in reading order, or float at their place when text sits
// beside or under them (QR codes, seals). Seals stored in signatures float
// where their stamp is.
//
// Not converted: signature checks, embedded fonts (the name is kept),
// composite objects, JBIG2 and JPEG 2000 pictures. Encrypted documents are
// refused.
package ofd

import (
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/xdom"
)

const (
	maxPages            = 10_000
	maxObjects          = 2_000_000
	maxFragmentsPerPage = 50_000
	maxPictures         = 2_000
	maxPicturePixels    = 100_000_000
	maxDepth            = 32
	// defaultSize is the font size (mm) when a text object names none.
	defaultSize = 3.5
)

func init() {
	doc.RegisterReader("ofd", Read)
}

// failure is a refusal raised deep in the reader and returned by Read.
type failure struct{ err error }

func fail(format string, args ...any) { panic(failure{fmt.Errorf(format, args...)}) }

type template struct {
	root *xdom.Node
	res  *resources
}

type reader struct {
	pkg       *pkg
	media     string
	extracted map[string]string
	templates map[string]*template
	objects   int
}

// Read reads an OFD file; pictures are unpacked into media.
func Read(path, media string) (d *doc.Document, err error) {
	p, err := openPackage(path)
	if err != nil {
		return nil, err
	}
	defer p.Close()
	defer func() {
		if r := recover(); r != nil {
			f, ok := r.(failure)
			if !ok {
				panic(r)
			}
			d, err = nil, f.err
		}
	}()
	r := &reader{pkg: p, media: media, extracted: map[string]string{}, templates: map[string]*template{}}
	return r.read(), nil
}

func (r *reader) xml(name string) *xdom.Node {
	n, err := r.pkg.loadXML(name)
	if err != nil {
		panic(failure{err})
	}
	return n
}

func resolveOr(base, ref string) string {
	s, _ := resolve(base, ref)
	return s
}

func (r *reader) read() *doc.Document {
	ofd := r.xml("OFD.xml")
	if ofd == nil || ofd.Name.Local != "OFD" {
		fail("the file is not an OFD document (it has no OFD.xml)")
	}
	r.refuseEncrypted(ofd)
	// A package may hold several documents; the first is read.
	docPath, ok := resolve("", child(child(ofd, "DocBody"), "DocRoot").Value())
	if !ok {
		fail("the OFD file names no document (DocRoot)")
	}
	document := r.xml(docPath)
	if document == nil {
		fail("the OFD package has no %s", docPath)
	}
	docDir := dirName(docPath)
	common := child(document, "CommonData")
	defaultArea, ok := pageArea(child(common, "PageArea"))
	if !ok {
		defaultArea = [2]float64{210, 297}
	}
	// Fonts, pictures and drawing parameters of the whole document; WPS keeps them in a Res.xml it does not name.
	docRes := newResources(nil)
	for _, res := range append(children(common, "PublicRes"), children(common, "DocumentRes")...) {
		r.loadRes(docRes, resolveOr(docDir, res.Value()), docDir)
	}
	r.loadRes(docRes, resolveOr(docDir, "Res.xml"), docDir)

	templates := map[string]string{}
	for _, t := range children(common, "TemplatePage") {
		if id := attr(t, "ID"); id != "" {
			if p := pagePath(docDir, attr(t, "BaseLoc")); p != "" {
				if _, dup := templates[id]; !dup {
					templates[id] = p
				}
			}
		}
	}
	annotations := r.annotationFiles(document, docDir)
	var pages []*pageContent
	for _, pg := range children(child(document, "Pages"), "Page") {
		if len(pages) == maxPages {
			fail("the OFD document has more than %d pages; Filee does not read it", maxPages)
		}
		path := pagePath(docDir, attr(pg, "BaseLoc"))
		content := r.xml(path)
		if content == nil {
			continue
		}
		id := attr(pg, "ID")
		p := r.readPage(content, path, docDir, docRes, templates, defaultArea, annotations[id])
		p.id = id
		pages = append(pages, p)
	}
	r.addSeals(ofd, pages)
	return r.build(pages)
}

// annotationFiles are the annotation files of each page (by page ID) Annotations.xml lists.
func (r *reader) annotationFiles(document *xdom.Node, docDir string) map[string][]string {
	result := map[string][]string{}
	listPath, ok := resolve(docDir, child(document, "Annotations").Value())
	if !ok {
		return result
	}
	list := r.xml(listPath)
	for i, pg := range children(list, "Page") {
		if i >= maxPages {
			break
		}
		id := attr(pg, "PageID")
		file, ok := resolve(dirName(listPath), child(pg, "FileLoc").Value())
		if id == "" || !ok {
			continue
		}
		if len(result[id]) < 8 {
			result[id] = append(result[id], file)
		}
	}
	return result
}

// refuseEncrypted: encrypted OFD (GM/T 0099) is refused, never decrypted.
func (r *reader) refuseEncrypted(ofd *xdom.Node) {
	for _, n := range r.pkg.names {
		if strings.EqualFold(n, "Encryptions.xml") || strings.HasSuffix(strings.ToLower(n), "/encryptions.xml") {
			fail("the OFD document is encrypted; Filee does not convert encrypted (DRM-protected) documents")
		}
	}
	found := false
	ofd.Walk(func(n *xdom.Node) bool {
		switch n.Name.Local {
		case "Encryptions", "Encryption", "EncryptInfo":
			found = true
		}
		return !found
	})
	if found {
		fail("the OFD document is encrypted; Filee does not convert encrypted (DRM-protected) documents")
	}
}

// pagePath is a page or template location: the content file, or (some writers) its folder.
func pagePath(docDir, baseLoc string) string {
	p, ok := resolve(docDir, baseLoc)
	if !ok {
		return ""
	}
	if strings.HasSuffix(strings.ToLower(p), ".xml") {
		return p
	}
	return p + "/Content.xml"
}

// ---- pages ----

type pageContent struct {
	id            string
	width, height float64
	fragments     []*fragment
	pictures      []picture
	rules         []rule
	shades        []shade
	segments      int
}

// fragment is a run of text on one baseline (or one vertical column).
type fragment struct {
	text      []rune
	format    doc.CharFormat
	size      float64 // mm
	template  bool
	continued bool
	x         float64
	baseline  float64
	width     float64
	run       float64
}

func (f *fragment) vertical() bool  { return f.run > 0 }
func (f *fragment) left() float64   { return f.x }
func (f *fragment) right() float64  { return f.x + f.width }
func (f *fragment) top() float64    { return f.baseline - 0.88*f.size }
func (f *fragment) bottom() float64 { return f.baseline + f.run + 0.12*f.size }

// picture is a part of the package, or a file already made (a seal).
type picture struct {
	part, file               string
	left, top, width, height float64
}

func (p picture) right() float64  { return p.left + p.width }
func (p picture) bottom() float64 { return p.top + p.height }

func (r *reader) readPage(content *xdom.Node, path, docDir string, docRes *resources, templates map[string]string, defaultArea [2]float64, annotationFiles []string) *pageContent {
	page := &pageContent{}
	area, hasArea := pageArea(child(content, "Area"))
	// Template pages first (the form behind the values).
	var seen []string
	for _, t := range children(content, "Template") {
		id := attr(t, "TemplateID")
		if id == "" || slices.Contains(seen, id) || len(seen) >= 8 {
			continue
		}
		seen = append(seen, id)
		tp, ok := templates[id]
		if !ok {
			continue
		}
		tpl := r.template(tp, docDir, docRes)
		if tpl == nil {
			continue
		}
		if !hasArea {
			area, hasArea = pageArea(child(tpl.root, "Area"))
		}
		r.collect(child(tpl.root, "Content"), tpl.res, page, "", true, 0, [2]float64{}, true)
	}
	res := r.pageResources(content, path, docDir, docRes)
	r.collect(child(content, "Content"), res, page, "", false, 0, [2]float64{}, true)

	// Annotations drawn on the page: the text and pictures of their appearance. Their paths are no table rules.
	for _, file := range annotationFiles {
		for i, annot := range children(r.xml(file), "Annot") {
			if i >= 1000 {
				break
			}
			if attr(annot, "Visible") == "false" {
				continue
			}
			for _, ap := range children(annot, "Appearance") {
				b, _ := box(attr(ap, "Boundary"))
				r.collect(ap, res, page, "", false, 1, [2]float64{b[0], b[1]}, false)
			}
		}
	}
	templateCount := 0
	for _, f := range page.fragments {
		if f.template {
			templateCount++
		}
	}
	if templateCount <= 5000 {
		page.fragments = mergeTemplateLabels(page.fragments)
	}
	if hasArea {
		page.width, page.height = area[0], area[1]
	} else {
		page.width, page.height = defaultArea[0], defaultArea[1]
	}
	return page
}

func (r *reader) template(path, docDir string, docRes *resources) *template {
	if t, ok := r.templates[path]; ok {
		return t
	}
	var t *template
	if root := r.xml(path); root != nil {
		t = &template{root, r.pageResources(root, path, docDir, docRes)}
	}
	r.templates[path] = t
	return t
}

// pageResources: the resource files a page names, a Res.xml next to it, the document's behind them.
func (r *reader) pageResources(content *xdom.Node, path, docDir string, docRes *resources) *resources {
	pageDir := dirName(path)
	res := newResources(docRes)
	for _, pr := range children(content, "PageRes") {
		name, ok := resolve(pageDir, pr.Value())
		if !ok || !r.pkg.has(name) {
			name = resolveOr(docDir, pr.Value())
		}
		r.loadRes(res, name, docDir)
	}
	r.loadRes(res, resolveOr(pageDir, "Res.xml"), docDir)
	return res
}

// pageArea is "x y width height" (PhysicalBox, else ApplicationBox) in mm.
func pageArea(area *xdom.Node) ([2]float64, bool) {
	for _, name := range []string{"PhysicalBox", "ApplicationBox"} {
		if b, ok := box(child(area, name).Value()); ok && b[2] >= 1 && b[2] <= 5000 && b[3] >= 1 && b[3] <= 5000 {
			return [2]float64{b[2], b[3]}, true
		}
	}
	return [2]float64{}, false
}

// collect gathers the drawing objects of a content tree (layers, page blocks).
func (r *reader) collect(container *xdom.Node, res *resources, page *pageContent, layerDraw string, tpl bool, depth int, origin [2]float64, paths bool) {
	if container == nil || depth > maxDepth {
		return
	}
	for _, el := range container.Elements() {
		if attr(el, "Visible") == "false" {
			continue
		}
		switch el.Name.Local {
		case "Layer":
			ld := attr(el, "DrawParam")
			if ld == "" {
				ld = layerDraw
			}
			r.collect(el, res, page, ld, tpl, depth+1, origin, paths)
		case "PageBlock":
			r.collect(el, res, page, layerDraw, tpl, depth+1, origin, paths)
		case "TextObject":
			r.count()
			textObject(el, res, page, layerDraw, tpl, origin)
		case "ImageObject":
			r.count()
			imageObject(el, res, page, origin)
		case "PathObject":
			if paths {
				r.count()
				pathObject(el, res, page, layerDraw)
			}
		}
	}
}

func (r *reader) count() {
	r.objects++
	if r.objects > maxObjects {
		fail("the OFD document has more than %d drawing objects; Filee does not read it", maxObjects)
	}
}

// ---- text ----

func textObject(el *xdom.Node, res *resources, page *pageContent, layerDraw string, tpl bool, origin [2]float64) {
	b, _ := box(attr(el, "Boundary"))
	ctm := parseMatrix(attr(el, "CTM"))
	size := defaultSize
	if s, ok := number(attr(el, "Size")); ok && s > 0 && s < 1000 {
		size = s
	}
	var font *fontInfo
	if id := attr(el, "Font"); id != "" {
		font = res.font(id)
	}
	w, _ := number(attr(el, "Weight"))
	bold := w >= 600 || font != nil && font.bold
	italic := attr(el, "Italic") == "true" || font != nil && font.italic
	// Colour: the object's own, its drawing parameter, the layer's. Outline-only text takes the stroke colour.
	own, layer := res.draw(attr(el, "DrawParam")), res.draw(layerDraw)
	color := first(colorValue(attr(el, "FillColor")), colorOf(child(el, "FillColor")), own.fill, layer.fill)
	if attr(el, "Fill") == "false" && attr(el, "Stroke") == "true" {
		color = first(colorOf(child(el, "StrokeColor")), own.stroke, layer.stroke, color)
	}
	scaleX := ctm.scaleX()
	fontSize := math.Max(0.1, math.Min(1000, size*ctm.scaleY()))
	family := ""
	if font != nil {
		family = fontFamily(font.name)
	}
	format := doc.CharFormat{Size: doc.Ptr(int(math.RoundToEven(fontSize*72/25.4*2) * 50))}
	if bold {
		format.Bold = doc.Ptr(true)
	}
	if italic {
		format.Italic = doc.Ptr(true)
	}
	if family != "" {
		format.Font, format.EastAsianFont = doc.Ptr(family), doc.Ptr(family)
	}
	if color != "" {
		format.Color = doc.Ptr(color)
	}
	add := func(text []rune, x, y, width, run float64, continued bool) {
		px, py := ctm.apply(x, y)
		if len(page.fragments) == maxFragmentsPerPage {
			fail("an OFD page has more than %d pieces of text; Filee does not read it", maxFragmentsPerPage)
		}
		page.fragments = append(page.fragments, &fragment{text: text, format: format, size: fontSize, template: tpl, continued: continued,
			x: origin[0] + b[0] + px, baseline: origin[1] + b[1] + py, width: width * scaleX, run: run * ctm.scaleY()})
	}
	var penX, penY float64
	for _, code := range children(el, "TextCode") {
		text, xs, ys, vertical, ok := glyphs(code, size, &penX, &penY)
		if !ok {
			continue
		}
		if vertical {
			f, l := trimRange(text, 0, len(text))
			if f <= l {
				add(text[f:l+1], xs[f], ys[f], size, ys[l]-ys[f], false)
			}
			continue
		}
		// A step back or to another baseline starts a new line: some writers put a whole block into one TextCode.
		lineStart := 0
		for i := 1; i <= len(text); i++ {
			if i < len(text) && math.Abs(ys[i]-ys[i-1]) < 0.01 && xs[i] >= xs[i-1]-0.01 {
				continue
			}
			f, l := trimRange(text, lineStart, i)
			if f <= l {
				add(text[f:l+1], xs[f], ys[f], xs[l]-xs[f]+advance(text[l], size), 0, lineStart > 0)
			}
			lineStart = i
		}
	}
}

// trimRange is the first and last non-space index of text[from:to].
func trimRange(text []rune, from, to int) (int, int) {
	f, l := from, to-1
	for f < to && unicode.IsSpace(text[f]) {
		f++
	}
	for l >= f && unicode.IsSpace(text[l]) {
		l--
	}
	return f, l
}

func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// glyphs are the characters of a TextCode and where each is drawn; vertical
// when only DeltaY moves them. A TextCode without X or Y goes on where the
// one before ended. A short DeltaX list goes on with its last value while the
// characters are as wide; without DeltaX the advance is estimated.
func glyphs(code *xdom.Node, size float64, penX, penY *float64) ([]rune, []float64, []float64, bool, bool) {
	text := []rune(code.Value())
	for i, c := range text {
		if unicode.IsControl(c) {
			text[i] = ' '
		}
	}
	x, ok := number(attr(code, "X"))
	if !ok {
		x = *penX
	}
	y, ok := number(attr(code, "Y"))
	if !ok {
		y = *penY
	}
	if len(text) == 0 {
		return nil, nil, nil, false, false
	}
	dx := deltas(attr(code, "DeltaX"), len(text)-1)
	dy := deltas(attr(code, "DeltaY"), len(text)-1)
	vertical := false
	if dy != nil && slices.ContainsFunc(dy, func(d float64) bool { return d != 0 }) && (dx == nil || !slices.ContainsFunc(dx, func(d float64) bool { return d != 0 })) {
		vertical = true
	}
	xs, ys := make([]float64, len(text)), make([]float64, len(text))
	xs[0], ys[0] = x, y
	for i := 1; i < len(text); i++ {
		if vertical {
			xs[i] = x
			step := size
			if i-1 < len(dy) {
				step = dy[i-1]
			}
			ys[i] = ys[i-1] + step
			continue
		}
		var step float64
		switch {
		case dx == nil:
			step = advance(text[i-1], size)
		case i-1 < len(dx):
			step = dx[i-1]
		case dx[len(dx)-1] > 0 && isWide(text[i-1]) == isWide(text[len(dx)-1]):
			step = dx[len(dx)-1]
		default:
			step = advance(text[i-1], size)
		}
		xs[i] = xs[i-1] + step
		ys[i] = ys[i-1]
		if dy != nil && i-1 < len(dy) {
			ys[i] += dy[i-1]
		}
	}
	if vertical {
		*penX, *penY = x, ys[len(ys)-1]+size
	} else {
		*penX, *penY = xs[len(xs)-1]+advance(text[len(text)-1], size), ys[len(ys)-1]
	}
	return text, xs, ys, vertical, true
}

// deltas reads DeltaX / DeltaY ("g n d" stands for n times d), at most count values.
func deltas(value string, count int) []float64 {
	if value == "" || count <= 0 {
		return nil
	}
	var out []float64
	tokens := strings.Fields(value)
	for i := 0; i < len(tokens) && len(out) < count; i++ {
		if tokens[i] == "g" && i+2 < len(tokens) {
			n, err := strconv.Atoi(tokens[i+1])
			d, ok := number(tokens[i+2])
			if err == nil && ok {
				for k := 0; k < n && len(out) < count; k++ {
					out = append(out, d)
				}
				i += 2
				continue
			}
		}
		if d, ok := number(tokens[i]); ok {
			out = append(out, d)
		}
	}
	return out
}

// advance of a character without DeltaX: full width for CJK, half for the rest.
func advance(c rune, size float64) float64 {
	if isWide(c) {
		return size
	}
	return size * 0.5
}

func isWide(c rune) bool { return c >= 0x2E80 && (c < 0xFF61 || c > 0xFFDC) }

// mergeTemplateLabels: single characters of a template stacked in one column
// (购买方信息) or spread over one line (合　计) become one fragment.
func mergeTemplateLabels(fragments []*fragment) []*fragment {
	var singles []*fragment
	for _, f := range fragments {
		if f.template && len(f.text) == 1 && isWide(f.text[0]) && !f.vertical() {
			singles = append(singles, f)
		}
	}
	merged := map[*fragment]bool{}
	// Stacked: same X, one under the other, at most three lines apart.
	columns := map[float64][]*fragment{}
	var keys []float64
	for _, f := range singles {
		k := math.RoundToEven(f.x*2) / 2
		if _, ok := columns[k]; !ok {
			keys = append(keys, k)
		}
		columns[k] = append(columns[k], f)
	}
	for _, k := range keys {
		col := slices.Clone(columns[k])
		slices.SortStableFunc(col, func(a, b *fragment) int { return cmp(a.baseline, b.baseline) })
		var stack []*fragment
		for i := 0; i <= len(col); i++ {
			var f *fragment
			if i < len(col) {
				f = col[i]
			}
			if f != nil && len(stack) > 0 && len(stack) < 8 && math.Abs(f.size-stack[0].size) < 0.1 &&
				f.baseline-stack[len(stack)-1].baseline > 0 && f.baseline-stack[len(stack)-1].baseline < 3*f.size {
				stack = append(stack, f)
				continue
			}
			if len(stack) >= 2 {
				var t []rune
				for _, s := range stack {
					t = append(t, s.text...)
				}
				stack[0].text = t
				stack[0].run = stack[len(stack)-1].baseline - stack[0].baseline
				for _, s := range stack[1:] {
					merged[s] = true
				}
			}
			stack = nil
			if f != nil {
				stack = []*fragment{f}
			}
		}
	}
	// Spread: a pair of single characters on one baseline with nothing between them.
	var row []*fragment
	for _, f := range singles {
		if !merged[f] && !f.vertical() {
			row = append(row, f)
		}
	}
	slices.SortStableFunc(row, func(a, b *fragment) int {
		if c := cmp(a.baseline, b.baseline); c != 0 {
			return c
		}
		return cmp(a.x, b.x)
	})
	for i := 1; i < len(row); i++ {
		left, right := row[i-1], row[i]
		gap := right.left() - left.right()
		if len(left.text) != 1 || math.Abs(left.baseline-right.baseline) > 0.3 || math.Abs(left.size-right.size) > 0.1 || gap < -0.1 || gap > 8*right.size {
			continue
		}
		between := false
		for _, f := range fragments {
			if f != left && f != right && math.Abs(f.baseline-left.baseline) < 0.5*f.size &&
				(f.left()+f.right())/2 > left.right()-0.1 && (f.left()+f.right())/2 < right.left()+0.1 {
				between = true
				break
			}
		}
		if between {
			continue
		}
		left.text = append(left.text, right.text...)
		left.width = right.right() - left.left()
		merged[right] = true
		row[i] = left
	}
	out := fragments[:0:0]
	for _, f := range fragments {
		if !merged[f] {
			out = append(out, f)
		}
	}
	return out
}

func cmp(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// chineseFonts are the English family names of common Chinese fonts.
var chineseFonts = map[string]string{
	"宋体": "SimSun", "新宋体": "NSimSun", "黑体": "SimHei", "楷体": "KaiTi", "楷体_gb2312": "KaiTi_GB2312", "仿宋": "FangSong",
	"仿宋_gb2312": "FangSong_GB2312", "微软雅黑": "Microsoft YaHei", "等线": "DengXian", "隶书": "LiSu", "幼圆": "YouYuan",
	"华文宋体": "STSong", "华文中宋": "STZhongsong", "华文楷体": "STKaiti", "华文仿宋": "STFangsong", "华文细黑": "STXihei",
	"方正小标宋简体": "FZXiaoBiaoSong-B05S",
}

// fontFamily: subset prefixes ("ABCDEF+") and style suffixes (",Bold") off, Chinese names in English.
func fontFamily(name string) string {
	name = strings.TrimSpace(name)
	if len(name) > 7 && name[6] == '+' && strings.Trim(name[:6], "ABCDEFGHIJKLMNOPQRSTUVWXYZ") == "" {
		name = name[7:]
	}
	name = strings.TrimSpace(strings.Split(name, ",")[0])
	if name == "" {
		return ""
	}
	if e, ok := chineseFonts[strings.ToLower(name)]; ok {
		return e
	}
	return name
}

// ---- pictures ----

func imageObject(el *xdom.Node, res *resources, page *pageContent, origin [2]float64) {
	id := attr(el, "ResourceID")
	part := res.media(id)
	if id == "" || part == "" {
		return
	}
	b, ok := box(attr(el, "Boundary"))
	if !ok {
		return
	}
	// The picture fills the unit square, which the CTM scales to its size within the boundary.
	ctm := parseMatrix(attr(el, "CTM"))
	left, top, w, h := b[0], b[1], b[2], b[3]
	if ctm != identity {
		left, top, w, h = ctm.bounds(b[0], b[1])
	}
	if w >= 0.5 && h >= 0.5 && w < 10_000 && h < 10_000 {
		page.pictures = append(page.pictures, picture{part: part, left: origin[0] + left, top: origin[1] + top, width: w, height: h})
	}
}

// extract unpacks a picture into the media folder (once per part); "" when it cannot be decoded or is too large.
func (r *reader) extract(part string) string {
	if f, ok := r.extracted[part]; ok {
		return f
	}
	file := ""
	if len(r.extracted) < maxPictures {
		if b, err := r.pkg.readPart(part, maxPictureBytes); err == nil && b != nil {
			file = r.save(b)
		} else if err != nil {
			panic(failure{err})
		}
	}
	r.extracted[part] = file
	return file
}

func (r *reader) save(b []byte) string {
	// The part name is never used for the file: the format comes from the bytes.
	kind := sniff(b)
	if kind == "" {
		return ""
	}
	switch kind {
	case "png", "jpg", "gif", "bmp":
		name := r.newMediaName(kind)
		if name == "" || os.WriteFile(name, b, 0o644) != nil {
			return ""
		}
		return name
	case "tiff", "jp2", "j2k":
		if name := r.newMediaName("png"); name != "" {
			if kind == "j2k" {
				kind = "jp2" // the JPEG 2000 reader takes both the file and the bare codestream
			}
			return toPNG(b, kind, name)
		}
	}
	return "" // a picture Filee cannot decode is left out; the text still converts
}

func sniff(b []byte) string {
	switch {
	case len(b) >= 4 && string(b[:4]) == "\x89PNG":
		return "png"
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "jpg"
	case len(b) >= 4 && string(b[:4]) == "GIF8":
		return "gif"
	case len(b) >= 2 && string(b[:2]) == "BM":
		return "bmp"
	case len(b) >= 4 && (string(b[:4]) == "II*\x00" || string(b[:4]) == "MM\x00*"):
		return "tiff"
	case len(b) >= 8 && string(b[:8]) == "\x00\x00\x00\x0CjP  ":
		return "jp2"
	case len(b) >= 4 && string(b[:4]) == "\xFF\x4F\xFF\x51":
		return "j2k"
	}
	return ""
}

// ---- resources ----

type fontInfo struct {
	name         string
	bold, italic bool
}

type drawParam struct {
	relative, fill, stroke string
	lineWidth              *float64
}

type drawn struct {
	fill, stroke string
	lineWidth    *float64
}

// resources by ID; a page's own come first, then the document's.
type resources struct {
	parent     *resources
	fonts      map[string]fontInfo
	mediaParts map[string]string
	drawParams map[string]drawParam
}

func newResources(parent *resources) *resources {
	return &resources{parent: parent, fonts: map[string]fontInfo{}, mediaParts: map[string]string{}, drawParams: map[string]drawParam{}}
}

func (r *resources) font(id string) *fontInfo {
	for x := r; x != nil; x = x.parent {
		if f, ok := x.fonts[id]; ok {
			return &f
		}
	}
	return nil
}

func (r *resources) media(id string) string {
	for x := r; x != nil; x = x.parent {
		if m, ok := x.mediaParts[id]; ok {
			return m
		}
	}
	return ""
}

func (r *resources) find(id string) (drawParam, bool) {
	for x := r; x != nil; x = x.parent {
		if d, ok := x.drawParams[id]; ok {
			return d, true
		}
	}
	return drawParam{}, false
}

// draw is the fill, stroke and line width of a drawing parameter, completed from the ones it is relative to.
func (r *resources) draw(id string) drawn {
	var d drawn
	for hops := 0; id != "" && hops < 8; hops++ {
		p, ok := r.find(id)
		if !ok {
			break
		}
		if d.fill == "" {
			d.fill = p.fill
		}
		if d.stroke == "" {
			d.stroke = p.stroke
		}
		if d.lineWidth == nil {
			d.lineWidth = p.lineWidth
		}
		id = p.relative
	}
	return d
}

// loadRes reads a resource file (IDs already there are kept).
func (r *reader) loadRes(into *resources, name, docDir string) {
	if name == "" {
		return
	}
	root := r.xml(name)
	if root == nil {
		return
	}
	resDir := dirName(name)
	// Media files are relative to the BaseLoc folder; writers differ, so the file's own folder,
	// its Res subfolder, the document and the package root are tried too.
	baseDir, ok := resolve(resDir, attr(root, "BaseLoc"))
	if !ok {
		baseDir = resDir
	}
	resSub := "Res"
	if resDir != "" {
		resSub = resDir + "/Res"
	}
	root.Walk(func(el *xdom.Node) bool {
		id := attr(el, "ID")
		if el == root || id == "" {
			return true
		}
		switch el.Name.Local {
		case "Font":
			if _, dup := into.fonts[id]; !dup {
				n := attr(el, "FontName")
				if n == "" {
					n = attr(el, "FamilyName")
				}
				into.fonts[id] = fontInfo{n, attr(el, "Bold") == "true", attr(el, "Italic") == "true"}
			}
		case "MultiMedia":
			if t := attr(el, "Type"); t != "" && !strings.EqualFold(t, "Image") {
				return true
			}
			file := attr(el, "MediaFile")
			if file == "" {
				file = child(el, "MediaFile").Value()
			}
			for _, dir := range []string{baseDir, resDir, resSub, docDir, ""} {
				if p, ok := resolve(dir, file); ok && r.pkg.has(p) {
					if _, dup := into.mediaParts[id]; !dup {
						into.mediaParts[id] = p
					}
					break
				}
			}
		case "DrawParam":
			if _, dup := into.drawParams[id]; !dup {
				var lw *float64
				if v, ok := number(attr(el, "LineWidth")); ok {
					lw = &v
				}
				into.drawParams[id] = drawParam{attr(el, "Relative"),
					first(colorValue(attr(el, "FillColor")), colorOf(child(el, "FillColor"))),
					first(colorValue(attr(el, "StrokeColor")), colorOf(child(el, "StrokeColor"))), lw}
			}
		}
		return true
	})
}

// ---- helpers ----

// matrix "a b c d e f": (x, y) → (a·x + c·y + e, b·x + d·y + f).
type matrix struct{ a, b, c, d, e, f float64 }

var identity = matrix{1, 0, 0, 1, 0, 0}

func parseMatrix(v string) matrix {
	n, ok := numbers(v)
	if !ok || len(n) != 6 || math.Abs(n[0]*n[3]-n[1]*n[2]) < 1e-9 {
		return identity
	}
	return matrix{n[0], n[1], n[2], n[3], n[4], n[5]}
}

func (m matrix) apply(x, y float64) (float64, float64) {
	return m.a*x + m.c*y + m.e, m.b*x + m.d*y + m.f
}
func (m matrix) scaleX() float64 { return math.Sqrt(m.a*m.a + m.b*m.b) }
func (m matrix) scaleY() float64 { return math.Sqrt(m.c*m.c + m.d*m.d) }

// bounds is the box of the unit square after the transformation, moved by (x, y).
func (m matrix) bounds(x, y float64) (float64, float64, float64, float64) {
	l, t, r, b := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range [][2]float64{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
		px, py := m.apply(p[0], p[1])
		l, t, r, b = math.Min(l, px), math.Min(t, py), math.Max(r, px), math.Max(b, py)
	}
	return x + l, y + t, r - l, b - t
}

// box is "x y width height" in mm.
func box(v string) ([4]float64, bool) {
	n, ok := numbers(v)
	if !ok || len(n) != 4 || n[2] < 0 || n[3] < 0 {
		return [4]float64{}, false
	}
	return [4]float64{n[0], n[1], n[2], n[3]}, true
}

func numbers(v string) ([]float64, bool) {
	if v == "" {
		return nil, false
	}
	parts := strings.FieldsFunc(v, func(r rune) bool { return r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == ',' })
	if len(parts) > 16 {
		return nil, false
	}
	out := make([]float64, len(parts))
	for i, p := range parts {
		n, ok := number(p)
		if !ok {
			return nil, false
		}
		out[i] = n
	}
	return out, true
}

// number is a number written the XML way; false when none, not finite or absurdly large.
func number(v string) (float64, bool) {
	n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || math.IsNaN(n) || math.Abs(n) > 1e6 {
		return 0, false
	}
	return n, true
}

func colorOf(el *xdom.Node) string { return colorValue(attr(el, "Value")) }

// colorValue reads "R G B", grey, CMYK (decimal or "#hex" channels) or "#RRGGBB".
func colorValue(v string) string {
	parts := strings.Fields(v)
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 && strings.HasPrefix(parts[0], "#") && len(parts[0]) >= 7 {
		if rgb, err := strconv.ParseUint(parts[0][len(parts[0])-6:], 16, 32); err == nil {
			return fmt.Sprintf("#%06X", rgb)
		}
	}
	ch := make([]int, len(parts))
	for i, p := range parts {
		var n int64
		var err error
		if strings.HasPrefix(p, "#") {
			n, err = strconv.ParseInt(p[1:], 16, 32)
		} else {
			n, err = strconv.ParseInt(p, 10, 32)
		}
		if err != nil {
			return ""
		}
		ch[i] = int(max(0, min(255, n)))
	}
	cmyk := func(c, k int) int { return int(math.RoundToEven(float64((255-c)*(255-k)) / 255)) }
	switch len(ch) {
	case 1:
		return fmt.Sprintf("#%02X%02X%02X", ch[0], ch[0], ch[0])
	case 3:
		return fmt.Sprintf("#%02X%02X%02X", ch[0], ch[1], ch[2])
	case 4:
		return fmt.Sprintf("#%02X%02X%02X", cmyk(ch[0], ch[3]), cmyk(ch[1], ch[3]), cmyk(ch[2], ch[3]))
	}
	return ""
}

func attr(el *xdom.Node, name string) string {
	if el == nil {
		return ""
	}
	if v, ok := el.Attr("", name); ok {
		return v
	}
	v, _ := el.AttrLocal(name)
	return v
}

func child(el *xdom.Node, name string) *xdom.Node      { return el.ChildLocal(name) }
func children(el *xdom.Node, name string) []*xdom.Node { return el.AllLocal(name) }
