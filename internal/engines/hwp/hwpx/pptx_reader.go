package hwpx

// PPTX (PresentationML) → document model without PowerPoint or LibreOffice: one page per slide, sized like the
// slide, with every object floating at its place on the page: text boxes (placeholders inherit position and text
// style from the layout and master), shapes with solid or gradient fills and outlines, pictures, tables, lines, groups
// and the slide background. PPTX → PDF / PNG then goes through the HWPX writer and rhwp.
//
// Split over files: this one walks slides and objects, pptx_text.go resolves text and its inherited styles. Not
// converted: charts, SmartArt, rotation, effects (shadows, glow), picture cropping, animations, notes, hidden slides.

import (
	"errors"
	"fmt"
	"math"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/office/opc"
	"github.com/dseif0x/filee-go/internal/xdom"
)

const nsP = "http://schemas.openxmlformats.org/presentationml/2006/main"

// pptxPictureTypes are the picture formats the HWPX writer and 한글 readers display.
var pptxPictureTypes = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".bmp": true, ".tif": true, ".tiff": true}

func init() {
	for _, f := range []string{"pptx", "pptm", "potx", "potm", "ppsx"} {
		doc.RegisterReader(f, ReadPptx)
	}
}

type pptxReader struct {
	pkg              *opc.Package
	mediaFolder      string
	defaultTextStyle *xdom.Node
	extracted        int
}

// ReadPptx reads a presentation; pictures are extracted into mediaFolder.
func ReadPptx(p, mediaFolder string) (*doc.Document, error) {
	pkg, err := opc.Open(p)
	if err != nil {
		return nil, fmt.Errorf("the file is not a PowerPoint presentation: %w", err)
	}
	defer pkg.Close()
	r := &pptxReader{pkg: pkg, mediaFolder: mediaFolder}
	return r.readPresentation()
}

// hwpOfEmu converts EMU (914400 per inch) to HWPUNIT (7200 per inch).
func hwpOfEmu(emu int64) int { return int(math.RoundToEven(float64(emu) / 127.0)) }

func emu(n *xdom.Node, name string) int64 {
	v, _ := strconv.ParseInt(n.A(name), 10, 64)
	return v
}

// ───────────────────────── Presentation & slides ─────────────────────────

func (r *pptxReader) readPresentation() (*doc.Document, error) {
	presentationPath := r.pkg.MainPartPath()
	if presentationPath == "" {
		presentationPath = "ppt/presentation.xml"
	}
	presentation := r.pkg.Part(presentationPath)
	if presentation == nil {
		return nil, errors.New("the file is not a PowerPoint presentation (no ppt/presentation.xml)")
	}
	r.defaultTextStyle = presentation.Child(nsP, "defaultTextStyle")
	size := presentation.Child(nsP, "sldSz")
	cx, cy := emu(size, "cx"), emu(size, "cy")
	if cx <= 0 {
		cx = 12192000
	}
	if cy <= 0 {
		cy = 6858000
	}
	width, height := hwpOfEmu(cx), hwpOfEmu(cy)

	section := doc.NewSection()
	section.Page = &doc.Page{Width: width, Height: height}
	for _, id := range presentation.Child(nsP, "sldIdLst").All(nsP, "sldId") {
		slidePath := r.pkg.Target(presentationPath, id.AN(nsR, "id"))
		slide := r.pkg.Part(slidePath)
		if slidePath == "" || slide == nil || slide.A("show") == "0" || slide.A("show") == "false" {
			continue
		}
		page := &doc.Paragraph{PageBreakBefore: len(section.Blocks) > 0}
		page.Inlines = r.readSlide(slidePath, slide, width, height)
		section.Blocks = append(section.Blocks, page)
	}
	if len(section.Blocks) == 0 {
		section.Blocks = append(section.Blocks, &doc.Paragraph{})
	}
	return &doc.Document{Sections: []*doc.Section{section}}, nil
}

// themeFonts are the Latin and East Asian fonts of the theme (what "+mj-lt", "+mn-ea" ... refer to).
type themeFonts struct{ majorLatin, minorLatin, majorEastAsian, minorEastAsian string }

func themeFontsOf(theme *xdom.Node) themeFonts {
	var scheme *xdom.Node
	if d := theme.Descendants(nsA, "fontScheme"); len(d) > 0 {
		scheme = d[0]
	}
	latin := func(kind string) string {
		return strings.TrimSpace(scheme.Child(nsA, kind).Child(nsA, "latin").A("typeface"))
	}
	// Korean templates name the Hangul font in <a:font script="Hang">, often with an empty <a:ea>.
	eastAsian := func(kind string) string {
		if v := strings.TrimSpace(scheme.Child(nsA, kind).Child(nsA, "ea").A("typeface")); v != "" {
			return v
		}
		for _, f := range scheme.Child(nsA, kind).All(nsA, "font") {
			if f.A("script") == "Hang" {
				return strings.TrimSpace(f.A("typeface"))
			}
		}
		return ""
	}
	return themeFonts{latin("majorFont"), latin("minorFont"), eastAsian("majorFont"), eastAsian("minorFont")}
}

// slideContext is the parts one slide draws from, with the colours and placeholders they define.
type slideContext struct {
	slidePath, layoutPath, masterPath string
	slide, layout, master             *xdom.Node
	colors                            *opc.Colors
	fonts                             themeFonts
}

func (r *pptxReader) readSlide(slidePath string, slide *xdom.Node, width, height int) []doc.Inline {
	layoutPath := r.pkg.TargetOfType(slidePath, "/slideLayout")
	layout := r.part(layoutPath)
	masterPath := ""
	if layoutPath != "" {
		masterPath = r.pkg.TargetOfType(layoutPath, "/slideMaster")
	}
	master := r.part(masterPath)
	themePath := ""
	if masterPath != "" {
		themePath = r.pkg.TargetOfType(masterPath, "/theme")
	}
	theme := r.part(themePath)
	// The master's colour map says which theme colours "text" and "background" are (dark templates swap them).
	colors := opc.FromTheme(theme)
	if m := master.Child(nsP, "clrMap"); m != nil {
		cmap := map[string]string{}
		for _, a := range m.Attrs {
			cmap[a.Name.Local] = a.Value
		}
		colors = colors.WithMap(cmap)
	}
	ctx := &slideContext{slidePath, layoutPath, masterPath, slide, layout, master, colors, themeFontsOf(theme)}

	var output []doc.Inline
	if bg := r.background(ctx, width, height); bg != nil {
		output = append(output, bg)
	}
	// Decorations of the master and the layout (logos, bars) lie under the slide's own objects.
	showMaster := slide.A("showMasterSp") != "0" && slide.A("showMasterSp") != "false" && layout.A("showMasterSp") != "0" && layout.A("showMasterSp") != "false"
	if showMaster && master != nil && masterPath != "" {
		output = r.objects(ctx, masterPath, spTree(master), identityTransform, output, true)
	}
	if layout != nil && layoutPath != "" {
		output = r.objects(ctx, layoutPath, spTree(layout), identityTransform, output, true)
	}
	return r.objects(ctx, slidePath, spTree(slide), identityTransform, output, false)
}

func (r *pptxReader) part(p string) *xdom.Node {
	if p == "" {
		return nil
	}
	return r.pkg.Part(p)
}

func spTree(part *xdom.Node) *xdom.Node { return part.Child(nsP, "cSld").Child(nsP, "spTree") }

func pageAnchor(x, y int) *doc.Anchor {
	return &doc.Anchor{HorizontalRelativeTo: "PAPER", HorizontalAlign: "LEFT", HorizontalOffset: max(0, x),
		VerticalRelativeTo: "PAPER", VerticalAlign: "TOP", VerticalOffset: max(0, y), Wrap: "IN_FRONT_OF_TEXT", AllowOverlap: true}
}

// background is the first background of slide, layout or master, as a full-page rectangle, or as a full-page picture
// for a picture background (chalkboards, photos); nil for plain white.
func (r *pptxReader) background(ctx *slideContext, width, height int) doc.Inline {
	for _, pp := range []struct {
		part *xdom.Node
		path string
	}{{ctx.slide, ctx.slidePath}, {ctx.layout, ctx.layoutPath}, {ctx.master, ctx.masterPath}} {
		bg := pp.part.Child(nsP, "cSld").Child(nsP, "bg")
		if bg == nil {
			continue
		}
		properties := bg.Child(nsP, "bgPr")
		if blip := properties.Child(nsA, "blipFill"); blip != nil && pp.path != "" {
			return r.pictureBackground(pp.path, blip, width, height)
		}
		var fill string
		var gradient *doc.Gradient
		if properties != nil {
			fill, gradient = pptxFill(properties, ctx.colors, nil)
		} else if ref := bg.Child(nsP, "bgRef"); ref != nil {
			fill = ctx.colors.Drawing(ref, "")
		}
		if gradient == nil && (fill == "" || strings.EqualFold(fill, "#FFFFFF")) {
			return nil
		}
		return &doc.Shape{Kind: doc.ShapeRectangle, Fill: fill, Gradient: gradient, Line: doc.NoBorder, Width: width, Height: height, Anchor: pageAnchor(0, 0)}
	}
	return nil
}

func (r *pptxReader) extract(target string) string {
	if target == "" || !pptxPictureTypes[strings.ToLower(path.Ext(target))] {
		return ""
	}
	r.extracted++
	return r.pkg.Extract(target, r.mediaFolder, fmt.Sprintf("pptx-media-%d", r.extracted))
}

// pictureBackground is a picture background stretched over the slide; nil when the picture can't be used.
func (r *pptxReader) pictureBackground(partPath string, blipFill *xdom.Node, width, height int) doc.Inline {
	file := r.extract(r.pkg.Target(partPath, blipFill.Child(nsA, "blip").AN(nsR, "embed")))
	if file == "" {
		return nil
	}
	return &doc.Image{Path: file, Width: ptr(width), Height: ptr(height), Anchor: pageAnchor(0, 0)}
}

// ───────────────────────── Objects ─────────────────────────

// transform maps child coordinates of a group to slide coordinates (EMU).
type transform struct{ scaleX, scaleY, offsetX, offsetY float64 }

var identityTransform = transform{1, 1, 0, 0}

type bounds struct{ x, y, w, h int64 }

func (t transform) apply(x, y, w, h int64) bounds {
	return bounds{int64(t.offsetX + float64(x)*t.scaleX), int64(t.offsetY + float64(y)*t.scaleY), int64(float64(w) * t.scaleX), int64(float64(h) * t.scaleY)}
}

func (r *pptxReader) objects(ctx *slideContext, partPath string, tree *xdom.Node, t transform, output []doc.Inline, decorationsOnly bool) []doc.Inline {
	for _, e := range tree.Elements() {
		// Placeholders of the master / layout are only templates for the slide's own placeholders.
		if decorationsOnly && placeholder(e) != nil {
			continue
		}
		switch e.Name.Local {
		case "sp":
			output = r.shape(ctx, partPath, e, t, output)
		case "cxnSp":
			output = r.connector(ctx, e, t, output)
		case "pic":
			output = r.picture(ctx, partPath, e, t, output)
		case "graphicFrame":
			output = r.table(ctx, partPath, e, t, output)
		case "grpSp":
			output = r.group(ctx, partPath, e, t, output, decorationsOnly)
		case "AlternateContent":
			// Newer object kinds come with a fallback that older readers understand.
			if fallback := e.ChildLocal("Fallback"); fallback != nil {
				output = r.objects(ctx, partPath, fallback, t, output, decorationsOnly)
			}
		}
	}
	return output
}

func (r *pptxReader) group(ctx *slideContext, partPath string, group *xdom.Node, t transform, output []doc.Inline, decorationsOnly bool) []doc.Inline {
	inner := t
	if xfrm := group.Child(nsP, "grpSpPr").Child(nsA, "xfrm"); xfrm != nil {
		x, y := emu(xfrm.Child(nsA, "off"), "x"), emu(xfrm.Child(nsA, "off"), "y")
		w, h := emu(xfrm.Child(nsA, "ext"), "cx"), emu(xfrm.Child(nsA, "ext"), "cy")
		cx, cy := emu(xfrm.Child(nsA, "chOff"), "x"), emu(xfrm.Child(nsA, "chOff"), "y")
		cw, ch := emu(xfrm.Child(nsA, "chExt"), "cx"), emu(xfrm.Child(nsA, "chExt"), "cy")
		sx, sy := 1.0, 1.0
		if cw > 0 {
			sx = float64(w) / float64(cw)
		}
		if ch > 0 {
			sy = float64(h) / float64(ch)
		}
		// child → group space → parent space
		inner = transform{t.scaleX * sx, t.scaleY * sy, t.offsetX + (float64(x)-float64(cx)*sx)*t.scaleX, t.offsetY + (float64(y)-float64(cy)*sy)*t.scaleY}
	}
	return r.objects(ctx, partPath, group, inner, output, decorationsOnly)
}

// placeholder is the p:ph element of a shape, picture or frame, if it is a placeholder.
func placeholder(e *xdom.Node) *xdom.Node {
	for _, c := range e.Elements() {
		if strings.HasPrefix(c.Name.Local, "nv") {
			return c.Child(nsP, "nvPr").Child(nsP, "ph")
		}
	}
	return nil
}

// inherited is the layout's and master's version of placeholder ph: matched by index, else by type (ctrTitle ↔ title,
// subTitle / obj / untyped ↔ body).
func inherited(ctx *slideContext, ph *xdom.Node) (*xdom.Node, *xdom.Node) {
	if ph == nil {
		return nil, nil
	}
	kind := normalizePlaceholder(ph.A("type"))
	index, hasIndex := ph.Attr("", "idx")
	find := func(part *xdom.Node, byIndex bool) *xdom.Node {
		var candidates []*xdom.Node
		spTree(part).Walk(func(n *xdom.Node) bool {
			if (n.Name.Local == "sp" || n.Name.Local == "pic") && placeholder(n) != nil && n != spTree(part) {
				candidates = append(candidates, n)
			}
			return true
		})
		if byIndex && hasIndex {
			for _, c := range candidates {
				if v, ok := placeholder(c).Attr("", "idx"); ok && v == index {
					return c
				}
			}
		}
		for _, c := range candidates {
			if normalizePlaceholder(placeholder(c).A("type")) == kind {
				return c
			}
		}
		return nil
	}
	return find(ctx.layout, true), find(ctx.master, false)
}

func normalizePlaceholder(kind string) string {
	switch kind {
	case "ctrTitle", "title":
		return "title"
	case "", "obj", "subTitle", "body":
		return "body"
	}
	return kind
}

// geometry is the a:xfrm of the object, else of the placeholder it inherits from.
func geometry(e, layoutPh, masterPh *xdom.Node) *xdom.Node {
	xfrm := func(n *xdom.Node) *xdom.Node {
		if x := n.Child(nsP, "spPr").Child(nsA, "xfrm"); x != nil {
			return x
		}
		return n.Child(nsP, "xfrm")
	}
	for _, n := range []*xdom.Node{e, layoutPh, masterPh} {
		if x := xfrm(n); x != nil {
			return x
		}
	}
	return nil
}

func boundsOf(xfrm *xdom.Node, t transform) (bounds, bool) {
	if xfrm == nil {
		return bounds{}, false
	}
	off, ext := xfrm.Child(nsA, "off"), xfrm.Child(nsA, "ext")
	return t.apply(emu(off, "x"), emu(off, "y"), emu(ext, "cx"), emu(ext, "cy")), true
}

func (r *pptxReader) shape(ctx *slideContext, partPath string, shape *xdom.Node, t transform, output []doc.Inline) []doc.Inline {
	ph := placeholder(shape)
	var layoutPh, masterPh *xdom.Node
	if partPath == ctx.slidePath {
		layoutPh, masterPh = inherited(ctx, ph)
	}
	b, ok := boundsOf(geometry(shape, layoutPh, masterPh), t)
	if !ok || b.w <= 0 && b.h <= 0 {
		return output
	}
	properties := shape.Child(nsP, "spPr")
	style := shape.Child(nsP, "style")
	fill, gradient := pptxFill(properties, ctx.colors, style.Child(nsA, "fillRef"))
	line := pptxLine(properties, ctx.colors, style.Child(nsA, "lnRef"))
	geom := properties.Child(nsA, "prstGeom").A("prst")
	if geom == "" {
		geom = "rect"
	}
	x, y, w, h := hwpOfEmu(b.x), hwpOfEmu(b.y), max(1, hwpOfEmu(b.w)), max(1, hwpOfEmu(b.h))
	textColor := ctx.colors.Drawing(style.Child(nsA, "fontRef"), "")
	blocks := r.textBody(ctx, partPath, shape.Child(nsP, "txBody"), ph, layoutPh, masterPh, textColor)
	if geom == "line" || geom == "straightConnector1" {
		return addLine(shape, b, line, output)
	}
	kind := doc.ShapeRectangle
	if geom == "ellipse" {
		kind = doc.ShapeEllipse
	}
	corners := 0
	if geom == "roundRect" {
		corners = cornerRatio(properties)
	}
	if len(blocks) > 0 {
		body := bodyProperties(shape, layoutPh, masterPh)
		padding := body.padding
		if kind == doc.ShapeEllipse {
			// PowerPoint puts the text of an ellipse into the rectangle inscribed in it.
			dx, dy := int(float64(w)*0.146), int(float64(h)*0.146)
			padding = doc.Insets{Left: padding.Left + dx, Right: padding.Right + dx, Top: padding.Top + dy, Bottom: padding.Bottom + dy}
		}
		return append(output, &doc.TextBox{Width: w, Height: h, Anchor: pageAnchor(x, y), Fill: fill, Gradient: gradient, Line: line,
			Shape: kind, CornerRatio: corners, Padding: padding, VerticalAlign: body.anchor, Blocks: blocks})
	}
	if fill == "" && gradient == nil && line.Style == doc.BorderNone {
		return output // an empty placeholder or an invisible box
	}
	return append(output, &doc.Shape{Kind: kind, Width: w, Height: h, Anchor: pageAnchor(x, y), Fill: fill, Gradient: gradient, Line: line, CornerRatio: corners})
}

// cornerRatio is the corner radius of a rounded rectangle as a percentage of the shorter side: the "adj" guide in
// 1/1000 %, 16 667 (a sixth) by default.
func cornerRatio(properties *xdom.Node) int {
	value := 16667
	for _, g := range properties.Child(nsA, "prstGeom").Child(nsA, "avLst").All(nsA, "gd") {
		if g.A("name") == "adj" {
			if parts := strings.Fields(g.A("fmla")); len(parts) == 2 && parts[0] == "val" {
				if v, err := strconv.Atoi(parts[1]); err == nil {
					value = v
				}
			}
			break
		}
	}
	return clamp(iround(float64(value)/1000.0), 0, 50)
}

func (r *pptxReader) connector(ctx *slideContext, connector *xdom.Node, t transform, output []doc.Inline) []doc.Inline {
	properties := connector.Child(nsP, "spPr")
	b, ok := boundsOf(properties.Child(nsA, "xfrm"), t)
	if !ok {
		return output
	}
	line := pptxLine(properties, ctx.colors, connector.Child(nsP, "style").Child(nsA, "lnRef"))
	return addLine(connector, b, line, output)
}

func addLine(e *xdom.Node, b bounds, line doc.Border, output []doc.Inline) []doc.Inline {
	if line.Style == doc.BorderNone {
		return output
	}
	xfrm := e.Child(nsP, "spPr").Child(nsA, "xfrm")
	fh, fv := xfrm.A("flipH"), xfrm.A("flipV")
	return append(output, &doc.Shape{Kind: doc.ShapeLine, Width: max(1, hwpOfEmu(b.w)), Height: max(1, hwpOfEmu(b.h)), Anchor: pageAnchor(hwpOfEmu(b.x), hwpOfEmu(b.y)),
		Line: line, FlipHorizontal: fh == "1" || fh == "true", FlipVertical: fv == "1" || fv == "true"})
}

func (r *pptxReader) picture(ctx *slideContext, partPath string, picture *xdom.Node, t transform, output []doc.Inline) []doc.Inline {
	var layoutPh, masterPh *xdom.Node
	if partPath == ctx.slidePath {
		layoutPh, masterPh = inherited(ctx, placeholder(picture))
	}
	b, ok := boundsOf(geometry(picture, layoutPh, masterPh), t)
	if !ok {
		return output
	}
	embed := picture.Child(nsP, "blipFill").Child(nsA, "blip").AN(nsR, "embed")
	file := r.extract(r.pkg.Target(partPath, embed))
	if file == "" {
		return output // vector formats (EMF, WMF, SVG without a bitmap fallback) are left out
	}
	return append(output, &doc.Image{Path: file, Width: ptr(max(1, hwpOfEmu(b.w))), Height: ptr(max(1, hwpOfEmu(b.h))), Anchor: pageAnchor(hwpOfEmu(b.x), hwpOfEmu(b.y))})
}

// ───────────────────────── Fills and lines ─────────────────────────

// pptxFill is the solid colour or gradient of a shape's properties; a style reference (p:style/a:fillRef) applies
// when the properties say nothing, as for shapes drawn with PowerPoint's default style.
func pptxFill(properties *xdom.Node, colors *opc.Colors, styleRef *xdom.Node) (string, *doc.Gradient) {
	if properties.Child(nsA, "noFill") != nil {
		return "", nil
	}
	if solid := properties.Child(nsA, "solidFill"); solid != nil {
		return colors.Drawing(solid, ""), nil
	}
	if gradFill := properties.Child(nsA, "gradFill"); gradFill != nil {
		stops := gradFill.Child(nsA, "gsLst").All(nsA, "gs")
		sort.SliceStable(stops, func(i, j int) bool { return emu(stops[i], "pos") < emu(stops[j], "pos") })
		var colorsOf []string
		for _, gs := range stops {
			if c := colors.Drawing(gs, ""); c != "" {
				colorsOf = append(colorsOf, c)
			}
		}
		angle := int(emu(gradFill.Child(nsA, "lin"), "ang") / 60000)
		if len(colorsOf) >= 2 {
			return colorsOf[0], &doc.Gradient{Colors: colorsOf, Angle: angle}
		}
		if len(colorsOf) == 1 {
			return colorsOf[0], nil
		}
		return "", nil
	}
	if properties.Child(nsA, "blipFill") != nil || properties.Child(nsA, "pattFill") != nil {
		return "", nil
	}
	if styleRef != nil && emu(styleRef, "idx") > 0 {
		return colors.Drawing(styleRef, ""), nil
	}
	return "", nil
}

func pptxLine(properties *xdom.Node, colors *opc.Colors, styleRef *xdom.Node) doc.Border {
	ln := properties.Child(nsA, "ln")
	if ln.Child(nsA, "noFill") != nil {
		return doc.NoBorder
	}
	width := emu(ln, "w")
	if width <= 0 {
		width = 12700 // 1 pt
	}
	color := ""
	if solid := ln.Child(nsA, "solidFill"); solid != nil {
		color = colors.Drawing(solid, "")
	} else if styleRef != nil && emu(styleRef, "idx") > 0 {
		color = colors.Drawing(styleRef, "")
	}
	if color == "" {
		return doc.NoBorder
	}
	kind := doc.BorderSolid
	switch ln.Child(nsA, "prstDash").A("val") {
	case "dot", "sysDot":
		kind = doc.BorderDot
	case "dash", "sysDash", "lgDash":
		kind = doc.BorderDash
	case "dashDot", "lgDashDot", "sysDashDot":
		kind = doc.BorderDashDot
	}
	return doc.Border{Style: kind, WidthMm: float64(width) / 36000.0, Color: color}
}
