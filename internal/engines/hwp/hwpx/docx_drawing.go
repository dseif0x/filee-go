package hwpx

// DOCX graphic objects: DrawingML pictures, text boxes (wps), groups and drawing canvases, plus legacy VML
// (w:pict / w:object) used by older documents and as the fallback of mc:AlternateContent.

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/hwp/xmltree"
)

// defaultTextBoxPadding are the DrawingML text box insets: 0.1" left/right, 0.05" top/bottom.
var defaultTextBoxPadding = doc.Insets{Left: 720, Right: 720, Top: 360, Bottom: 360}

type groupTransform struct{ x, y, scaleX, scaleY float64 }

func (r *docxReader) drawing(drawing *xmltree.Element) []doc.Inline {
	var out []doc.Inline
	for _, container := range drawing.Elements() {
		anchored := container.Is(nsWP, "anchor")
		if !anchored && !container.Is(nsWP, "inline") {
			continue
		}
		extent := container.ChildNS(nsWP, "extent")
		width := doc.FromEmu(float64(val(long(extent, "cx"), 0)))
		height := doc.FromEmu(float64(val(long(extent, "cy"), 0)))
		var anchor *doc.Anchor
		if anchored {
			anchor = drawingAnchor(container)
		}
		data := container.ChildNS(nsA, "graphic").ChildNS(nsA, "graphicData")
		out = append(out, r.graphicObjects(data.Elements(), width, height, anchor, groupTransform{0, 0, 1, 1})...)
	}
	return out
}

// graphicObjects are the pictures and text boxes of a graphic frame. Members of groups and canvases become separate
// objects placed at their position inside the group.
func (r *docxReader) graphicObjects(objects []*xmltree.Element, width, height int, anchor *doc.Anchor, transform groupTransform) []doc.Inline {
	var out []doc.Inline
	for _, obj := range objects {
		switch {
		case obj.Is(nsPic, "pic"):
			if p := r.drawingPicture(obj, width, height, anchor); p != nil {
				out = append(out, p)
			}
		case obj.Is(nsWPS, "wsp"):
			if s := r.drawingShape(obj, width, height, anchor); s != nil {
				out = append(out, s)
			}
		case obj.Is(nsWPG, "wgp"), obj.Is(nsWPG, "grpSp"), obj.Is(nsWPC, "wpc"):
			out = append(out, r.drawingGroup(obj, width, height, anchor, transform)...)
		}
	}
	return out
}

func (r *docxReader) drawingGroup(group *xmltree.Element, width, height int, anchor *doc.Anchor, parent groupTransform) []doc.Inline {
	xfrm := group.ChildNS(nsWPG, "grpSpPr").ChildNS(nsA, "xfrm")
	if xfrm == nil {
		xfrm = group.ChildNS(nsA, "grpSpPr").ChildNS(nsA, "xfrm")
	}
	chOff := xfrm.ChildNS(nsA, "chOff")
	chExt := xfrm.ChildNS(nsA, "chExt")
	// Child coordinates → HWPUNIT relative to the group's top-left corner.
	scaleX, scaleY := 1/127.0, 1/127.0
	if cx := val(long(chExt, "cx"), 0); cx > 0 {
		scaleX = float64(width) / float64(cx)
	}
	if cy := val(long(chExt, "cy"), 0); cy > 0 {
		scaleY = float64(height) / float64(cy)
	}
	originX := val(long(chOff, "x"), 0)
	originY := val(long(chOff, "y"), 0)

	var out []doc.Inline
	for _, c := range group.Elements() {
		childXfrm := c.FirstDescendantNS(nsA, "xfrm")
		off := childXfrm.ChildNS(nsA, "off")
		childExt := childXfrm.ChildNS(nsA, "ext")
		x := int(float64(val(long(off, "x"), 0)-originX) * scaleX)
		y := int(float64(val(long(off, "y"), 0)-originY) * scaleY)
		childWidth := int(float64(val(long(childExt, "cx"), 0)) * scaleX)
		childHeight := int(float64(val(long(childExt, "cy"), 0)) * scaleY)
		// An inline group becomes objects floating over the paragraph at the same place.
		var a doc.Anchor
		if anchor == nil {
			a = doc.Anchor{HorizontalRelativeTo: "COLUMN", HorizontalAlign: "LEFT", HorizontalOffset: x, VerticalRelativeTo: "PARA", VerticalAlign: "TOP", VerticalOffset: y, Wrap: "IN_FRONT_OF_TEXT", AllowOverlap: true}
		} else {
			a = *anchor
			a.HorizontalOffset = anchor.HorizontalOffset + alignedOffset(anchor.HorizontalAlign, x, childWidth, width)
			a.VerticalOffset = anchor.VerticalOffset + alignedOffset(anchor.VerticalAlign, y, childHeight, height)
			a.AllowOverlap = true
		}
		out = append(out, r.graphicObjects([]*xmltree.Element{c}, childWidth, childHeight, &a, parent)...)
	}
	return out
}

func (r *docxReader) drawingPicture(pic *xmltree.Element, width, height int, anchor *doc.Anchor) *doc.Image {
	blip := pic.FirstDescendantNS(nsA, "blip")
	id, _ := blip.AttrNS(nsR, "embed")
	p := r.extractImage(id)
	if p == "" {
		return nil
	}
	img := &doc.Image{Path: p, Anchor: anchor}
	if width > 0 {
		img.Width = ptr(width)
	}
	if height > 0 {
		img.Height = ptr(height)
	}
	return img
}

// drawingShape is a text box, or a rectangle / ellipse / line without text. Other geometries are skipped.
func (r *docxReader) drawingShape(wsp *xmltree.Element, width, height int, anchor *doc.Anchor) doc.Inline {
	spPr := wsp.ChildNS(nsWPS, "spPr")
	style := wsp.ChildNS(nsWPS, "style")
	fill, gradient := r.fillOf(spPr, style)
	geometry := spPr.ChildNS(nsA, "prstGeom").A("prst")
	if content := wsp.ChildNS(nsWPS, "txbx").ChildNS(nsW, "txbxContent"); content != nil {
		switch geometry {
		case "rect", "roundRect", "flowChartProcess", "flowChartAlternateProcess", "ellipse", "flowChartConnector":
			boxFill := fill
			if boxFill == "" && gradient != nil {
				boxFill = gradient.Colors[0]
			}
			framed := r.textBox(wsp, content, width, height, anchor, boxFill)
			switch geometry {
			case "ellipse", "flowChartConnector":
				framed.Shape = doc.ShapeEllipse
			case "roundRect", "flowChartAlternateProcess":
				framed.CornerRatio = 20
			}
			return framed
		}
		// Text on a free-form or decorative shape: keep the text, drop the outline 한글 cannot draw.
		if strings.TrimSpace(content.Value()) == "" {
			return nil
		}
		box := r.textBox(wsp, content, width, height, anchor, "")
		box.Line = doc.NoBorder
		return box
	}
	var kind doc.ShapeKind
	switch geometry {
	case "rect", "roundRect", "snip1Rect", "snip2SameRect", "round1Rect", "round2SameRect", "flowChartProcess", "flowChartAlternateProcess", "frame":
		kind = doc.ShapeRectangle
	case "ellipse", "flowChartConnector":
		kind = doc.ShapeEllipse
	case "line", "straightConnector1":
		kind = doc.ShapeLine
	default:
		return nil // free-form and other preset shapes have no 한글 equivalent
	}
	xfrm := spPr.ChildNS(nsA, "xfrm")
	s := &doc.Shape{Kind: kind, Width: width, Height: height, Anchor: anchor, Line: r.lineOf(spPr.ChildNS(nsA, "ln"), style)}
	if kind != doc.ShapeLine {
		s.Fill, s.Gradient = fill, gradient
	}
	switch geometry {
	case "roundRect", "round1Rect", "round2SameRect", "flowChartAlternateProcess":
		s.CornerRatio = 20
	}
	fh, fv := xfrm.A("flipH"), xfrm.A("flipV")
	s.FlipHorizontal = fh == "1" || fh == "true"
	s.FlipVertical = fv == "1" || fv == "true"
	return s
}

func (r *docxReader) textBox(wsp, content *xmltree.Element, width, height int, anchor *doc.Anchor, fill string) *doc.TextBox {
	spPr := wsp.ChildNS(nsWPS, "spPr")
	bodyPr := wsp.ChildNS(nsWPS, "bodyPr")
	box := newDocxTextBox(width, height, anchor)
	box.Fill = fill
	box.Line = r.lineOf(spPr.ChildNS(nsA, "ln"), wsp.ChildNS(nsWPS, "style"))
	box.Padding = doc.Insets{
		Left: inset(bodyPr, "lIns", defaultTextBoxPadding.Left), Right: inset(bodyPr, "rIns", defaultTextBoxPadding.Right),
		Top: inset(bodyPr, "tIns", defaultTextBoxPadding.Top), Bottom: inset(bodyPr, "bIns", defaultTextBoxPadding.Bottom)}
	switch bodyPr.A("anchor") {
	case "ctr":
		box.VerticalAlign = doc.VAlignCenter
	case "b":
		box.VerticalAlign = doc.VAlignBottom
	}
	box.Blocks = r.readTextBoxContent(content)
	return box
}

// newDocxTextBox is a text box at least 1000 × 600 HWPUNIT with the DrawingML padding.
func newDocxTextBox(width, height int, anchor *doc.Anchor) *doc.TextBox {
	return &doc.TextBox{Width: max(width, 1000), Height: max(height, 600), Anchor: anchor, Line: doc.ThinBorder, Padding: defaultTextBoxPadding}
}

// readTextBoxContent reads text box paragraphs; Word's document grid does not apply inside text boxes.
func (r *docxReader) readTextBoxContent(content *xmltree.Element) []doc.Block {
	r.textBoxes++
	defer func() { r.textBoxes-- }()
	return r.readBlocks(content.Elements(), nil)
}

func inset(bodyPr *xmltree.Element, name string, fallback int) int {
	if v := long(bodyPr, name); v != nil {
		return doc.FromEmu(float64(*v))
	}
	return fallback
}

// fillOf is the solid colour or linear gradient of a shape; without its own fill the shape style decides.
func (r *docxReader) fillOf(spPr, style *xmltree.Element) (string, *doc.Gradient) {
	if spPr.ChildNS(nsA, "noFill") != nil {
		return "", nil
	}
	if solid := spPr.ChildNS(nsA, "solidFill"); solid != nil {
		return r.drawingColor(solid), nil
	}
	if gradient := spPr.ChildNS(nsA, "gradFill"); gradient != nil {
		stops := gradient.ChildNS(nsA, "gsLst").ChildrenNS(nsA, "gs")
		sort.SliceStable(stops, func(i, j int) bool { return val(long(stops[i], "pos"), 0) < val(long(stops[j], "pos"), 0) })
		var colors []string
		for _, gs := range stops {
			if c := r.drawingColor(gs); c != "" {
				colors = append(colors, c)
			}
		}
		angle := int(val(long(gradient.ChildNS(nsA, "lin"), "ang"), 0) / 60000)
		if len(colors) >= 2 {
			return "", &doc.Gradient{Colors: colors, Angle: angle}
		}
		if len(colors) == 1 {
			return colors[0], nil
		}
		return "", nil
	}
	if fillRef := style.ChildNS(nsA, "fillRef"); fillRef != nil {
		if idx, ok := fillRef.Attr("idx"); ok && idx != "0" {
			return r.drawingColor(fillRef), nil
		}
	}
	return "", nil
}

func (r *docxReader) lineOf(ln, style *xmltree.Element) doc.Border {
	if ln == nil {
		// No explicit outline: the shape style decides (lnRef idx 0 = no line).
		if lnRef := style.ChildNS(nsA, "lnRef"); lnRef != nil {
			if idx, ok := lnRef.Attr("idx"); ok && idx != "0" {
				b := doc.ThinBorder
				b.Color = or(r.drawingColor(lnRef), "#000000")
				return b
			}
		}
		return doc.NoBorder
	}
	if ln.ChildNS(nsA, "noFill") != nil {
		return doc.NoBorder
	}
	widthMm := 0.12
	if w := long(ln, "w"); w != nil {
		widthMm = float64(*w) / 36000.0
	}
	borderStyle := doc.BorderDash
	dash, ok := ln.ChildNS(nsA, "prstDash").Attr("val")
	switch {
	case !ok || dash == "solid":
		borderStyle = doc.BorderSolid
	case dash == "dot" || dash == "sysDot":
		borderStyle = doc.BorderDot
	case dash == "dashDot" || dash == "sysDashDot" || dash == "lgDashDot":
		borderStyle = doc.BorderDashDot
	}
	return doc.Border{Style: borderStyle, WidthMm: widthMm, Color: or(r.drawingColor(ln.ChildNS(nsA, "solidFill")), "#000000")}
}

// drawingColor is the colour of a fill element: srgbClr, sysClr or a theme colour (luminance modifiers ignored).
func (r *docxReader) drawingColor(fill *xmltree.Element) string {
	if fill == nil {
		return ""
	}
	if rgb := fill.ChildNS(nsA, "srgbClr"); rgb != nil {
		return "#" + strings.ToUpper(or(rgb.A("val"), "000000"))
	}
	if sys := fill.ChildNS(nsA, "sysClr"); sys != nil {
		return "#" + strings.ToUpper(or(sys.A("lastClr"), "000000"))
	}
	if scheme := fill.ChildNS(nsA, "schemeClr"); scheme != nil {
		name := scheme.A("val")
		switch name {
		case "bg1":
			name = "lt1"
		case "tx1":
			name = "dk1"
		case "bg2":
			name = "lt2"
		case "tx2":
			name = "dk2"
		case "":
			name = "dk1"
		}
		if c, ok := r.themeColors[name]; ok {
			return c
		}
		if strings.HasPrefix(name, "lt") {
			return "#FFFFFF"
		}
		return "#000000"
	}
	return ""
}

func (r *docxReader) loadThemeColors(theme *docxPart) {
	if theme == nil {
		return
	}
	for _, c := range theme.root.FirstDescendantNS(nsA, "clrScheme").Elements() {
		v := c.ChildNS(nsA, "srgbClr").A("val")
		if v == "" {
			v = c.ChildNS(nsA, "sysClr").A("lastClr")
		}
		if len(v) == 6 {
			r.themeColors[c.Local] = "#" + strings.ToUpper(v)
		}
	}
}

// drawingAnchor is the position of a floating DrawingML object (wp:anchor).
func drawingAnchor(anchor *xmltree.Element) *doc.Anchor {
	horizontal := anchor.ChildNS(nsWP, "positionH")
	vertical := anchor.ChildNS(nsWP, "positionV")
	wrap := ""
	for _, e := range anchor.Elements() {
		if e.Space == nsWP && strings.HasPrefix(e.Local, "wrap") {
			wrap = e.Local
			break
		}
	}
	behind := anchor.A("behindDoc") == "1" || anchor.A("behindDoc") == "true"
	a := &doc.Anchor{
		HorizontalRelativeTo: "COLUMN", HorizontalAlign: "LEFT", HorizontalOffset: offsetOf(horizontal),
		VerticalRelativeTo: "PARA", VerticalAlign: "TOP", VerticalOffset: offsetOf(vertical),
		AllowOverlap: anchor.A("allowOverlap") != "0" && anchor.A("allowOverlap") != "false",
	}
	switch horizontal.A("relativeFrom") {
	case "page", "leftMargin", "rightMargin", "insideMargin", "outsideMargin":
		a.HorizontalRelativeTo = "PAPER"
	case "margin":
		a.HorizontalRelativeTo = "PAGE"
	case "character":
		a.HorizontalRelativeTo = "PARA"
	}
	switch strings.TrimSpace(horizontal.ChildNS(nsWP, "align").Value()) {
	case "center":
		a.HorizontalAlign = "CENTER"
	case "right":
		a.HorizontalAlign = "RIGHT"
	case "inside":
		a.HorizontalAlign = "INSIDE"
	case "outside":
		a.HorizontalAlign = "OUTSIDE"
	}
	switch vertical.A("relativeFrom") {
	case "page", "topMargin", "bottomMargin", "insideMargin", "outsideMargin":
		a.VerticalRelativeTo = "PAPER"
	case "margin":
		a.VerticalRelativeTo = "PAGE"
	}
	switch strings.TrimSpace(vertical.ChildNS(nsWP, "align").Value()) {
	case "center":
		a.VerticalAlign = "CENTER"
	case "bottom":
		a.VerticalAlign = "BOTTOM"
	case "inside":
		a.VerticalAlign = "INSIDE"
	case "outside":
		a.VerticalAlign = "OUTSIDE"
	}
	switch wrap {
	case "wrapSquare", "wrapTight", "wrapThrough":
		a.Wrap = "SQUARE"
	case "wrapTopAndBottom":
		a.Wrap = "TOP_AND_BOTTOM"
	default:
		a.Wrap = "IN_FRONT_OF_TEXT"
		if behind {
			a.Wrap = "BEHIND_TEXT"
		}
	}
	return a
}

func offsetOf(position *xmltree.Element) int {
	if v, err := strconv.ParseInt(strings.TrimSpace(position.ChildNS(nsWP, "posOffset").Value()), 10, 64); err == nil {
		return doc.FromEmu(float64(v))
	}
	return 0
}

// ───────────────────────── VML (legacy) ─────────────────────────

// vml reads w:pict / w:object; format is the formatting of the run holding the objects (WordArt starts from it).
func (r *docxReader) vml(container *xmltree.Element, format doc.CharFormat) []doc.Inline {
	var out []doc.Inline
	for _, shape := range container.Elements() {
		if shape.Space != nsV {
			continue
		}
		if shape.Local == "group" {
			out = append(out, r.vml(shape, format)...)
			continue
		}
		if shape.Local == "shapetype" {
			continue
		}
		style := vmlStyle(shape.A("style"))
		width, _ := doc.ParseLength(style["width"])
		height, _ := doc.ParseLength(style["height"])
		var anchor *doc.Anchor
		if style["position"] == "absolute" {
			anchor = vmlAnchor(shape, style)
		}
		imageData := shape.ChildNS(nsV, "imagedata")
		id, _ := imageData.AttrNS(nsR, "id")
		if p := r.extractImage(id); imageData != nil && p != "" {
			img := &doc.Image{Path: p, Anchor: anchor}
			if width > 0 {
				img.Width = ptr(width)
			}
			if height > 0 {
				img.Height = ptr(height)
			}
			out = append(out, img)
		} else if content := shape.ChildNS(nsV, "textbox").ChildNS(nsW, "txbxContent"); content != nil {
			box := newDocxTextBox(width, height, anchor)
			box.Fill = vmlFill(shape)
			box.Line = vmlLine(shape)
			box.Blocks = r.readTextBoxContent(content)
			out = append(out, box)
		} else if text := wordArt(shape, width, height, format); text != nil {
			if anchor == nil {
				out = append(out, text)
			} else {
				box := newDocxTextBox(width, height, anchor)
				box.Line, box.Padding, box.VerticalAlign = doc.NoBorder, doc.Insets{}, doc.VAlignCenter
				box.Blocks = []doc.Block{&doc.Paragraph{Format: doc.ParaFormat{Align: ptr(doc.AlignCenter)}, Inlines: []doc.Inline{text}}}
				out = append(out, box)
			}
		} else if shape.Local == "rect" || shape.Local == "roundrect" || shape.Local == "oval" || shape.Local == "line" {
			kind := doc.ShapeRectangle
			switch shape.Local {
			case "oval":
				kind = doc.ShapeEllipse
			case "line":
				kind = doc.ShapeLine
			}
			if kind == doc.ShapeLine {
				// v:line has from="x,y" to="x,y" instead of a size.
				fx, fy := vmlPoint(shape.A("from"))
				tx, ty := vmlPoint(shape.A("to"))
				width, height = abs(tx-fx), abs(ty-fy)
				lineAnchor := anchor
				if lineAnchor == nil {
					lineAnchor = vmlAnchor(shape, style)
				}
				a := *lineAnchor
				a.HorizontalOffset += min(fx, tx)
				a.VerticalOffset += min(fy, ty)
				anchor = &a
			}
			s := &doc.Shape{Kind: kind, Width: width, Height: height, Anchor: anchor, Line: vmlLine(shape)}
			if kind != doc.ShapeLine {
				s.Fill = vmlFill(shape)
			}
			if shape.Local == "roundrect" {
				s.CornerRatio = 20
			}
			out = append(out, s)
		}
	}
	return out
}

// wordArt is legacy WordArt (a VML shape with v:textpath, as WPS and older Word write it) as plain text in its font
// and fill colour. When the text is stretched to the shape (fitpath / fitshape), the font size is chosen so it fills
// about the same box.
func wordArt(shape *xmltree.Element, width, height int, format doc.CharFormat) *doc.Text {
	path := shape.ChildNS(nsV, "textpath")
	text, ok := path.Attr("string")
	if on := path.A("on"); path == nil || on == "f" || on == "false" || !ok || strings.TrimSpace(text) == "" {
		return nil
	}
	font := vmlStyle(path.A("style"))
	family := strings.Trim(font["font-family"], "\"' ")
	size := val(format.Size, 1000)
	if points, ok := doc.ParseLength(font["font-size"]); ok && points > 0 {
		size = points
	}
	stretched := path.A("fitpath") == "t" || path.A("fitpath") == "true" || path.A("fitshape") == "t" || path.A("fitshape") == "true"
	if stretched && width > 0 && height > 0 {
		// The glyphs fill the shape: capitals and digits are about 0.72 em tall, Han characters about 0.9 em; East
		// Asian characters are 1 em wide, others about half.
		wide := 0
		for _, c := range text {
			if c >= '⺀' && c <= '鿿' || c >= '가' && c <= '힣' || c >= '＀' && c <= '｠' {
				wide++
			}
		}
		ems := float64(wide) + float64(utf8.RuneCountInString(text)-wide)*0.5
		byHeight := float64(height) / 0.72
		if wide > 0 {
			byHeight = float64(height) / 0.9
		}
		size = int(math.Min(byHeight, float64(width)/math.Max(ems, 1)))
	}
	color := vmlColor(shape.A("fillcolor"))
	if f := shape.A("filled"); f == "f" || f == "false" {
		color = vmlColor(shape.A("strokecolor"))
	}
	f := format
	f.Size = ptr(max(size, 100))
	if family != "" {
		f.Font, f.EastAsianFont = ptr(family), ptr(family)
	}
	if weight, ok := font["font-weight"]; ok {
		n, err := strconv.Atoi(weight)
		f.Bold = ptr(weight == "bold" || err == nil && n >= 600)
	}
	if italic, ok := font["font-style"]; ok {
		f.Italic = ptr(italic == "italic")
	}
	if color != "" {
		f.Color = ptr(color)
	}
	return &doc.Text{Text: strings.NewReplacer("\n", " ", "\r", " ").Replace(text), Format: f}
}

func vmlFill(shape *xmltree.Element) string {
	if f := shape.A("filled"); f == "f" || f == "false" {
		return ""
	}
	return or(vmlColor(shape.A("fillcolor")), "#FFFFFF")
}

func vmlLine(shape *xmltree.Element) doc.Border {
	if s := shape.A("stroked"); s == "f" || s == "false" {
		return doc.NoBorder
	}
	b := doc.ThinBorder
	b.Color = or(vmlColor(shape.A("strokecolor")), "#000000")
	if weight, ok := doc.ParseLength(shape.A("strokeweight")); ok {
		b.WidthMm = float64(weight) / doc.PerMm
	}
	return b
}

// vmlPoint is the "x,y" of a VML line end; bare numbers are points.
func vmlPoint(point string) (int, int) {
	if point == "" {
		point = "0,0"
	}
	parts := strings.Split(point, ",")
	coordinate := func(i int) int {
		text := "0"
		if i < len(parts) {
			text = strings.TrimSpace(parts[i])
		}
		if text == "" {
			text = "0"
		}
		if last := text[len(text)-1]; last >= '0' && last <= '9' {
			text += "pt"
		}
		v, _ := doc.ParseLength(text)
		return v
	}
	return coordinate(0), coordinate(1)
}

func vmlStyle(style string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(style, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, ":", 2)
		if len(kv) == 2 {
			out[strings.ToLower(strings.TrimSpace(kv[0]))] = strings.TrimSpace(kv[1])
		}
	}
	return out
}

func vmlAnchor(shape *xmltree.Element, style map[string]string) *doc.Anchor {
	wrap := shape.ChildNS(nsW10, "wrap").A("type")
	left, _ := doc.ParseLength(style["margin-left"])
	top, _ := doc.ParseLength(style["margin-top"])
	a := &doc.Anchor{HorizontalRelativeTo: "COLUMN", HorizontalAlign: "LEFT", HorizontalOffset: left, VerticalRelativeTo: "PARA", VerticalAlign: "TOP", VerticalOffset: top, AllowOverlap: true}
	switch style["mso-position-horizontal-relative"] {
	case "page":
		a.HorizontalRelativeTo = "PAPER"
	case "margin":
		a.HorizontalRelativeTo = "PAGE"
	case "char":
		a.HorizontalRelativeTo = "PARA"
	}
	switch style["mso-position-horizontal"] {
	case "center":
		a.HorizontalAlign = "CENTER"
	case "right":
		a.HorizontalAlign = "RIGHT"
	}
	switch style["mso-position-vertical-relative"] {
	case "page":
		a.VerticalRelativeTo = "PAPER"
	case "margin":
		a.VerticalRelativeTo = "PAGE"
	}
	switch style["mso-position-vertical"] {
	case "center":
		a.VerticalAlign = "CENTER"
	case "bottom":
		a.VerticalAlign = "BOTTOM"
	}
	switch wrap {
	case "square", "tight", "through":
		a.Wrap = "SQUARE"
	case "topAndBottom":
		a.Wrap = "TOP_AND_BOTTOM"
	default:
		a.Wrap = "IN_FRONT_OF_TEXT"
		if strings.HasPrefix(style["z-index"], "-") {
			a.Wrap = "BEHIND_TEXT"
		}
	}
	return a
}

// vmlColor reads VML colours: "#RRGGBB", "#RGB" or "#RRGGBB [index]".
func vmlColor(value string) string {
	color := strings.TrimSpace(strings.Split(value, " ")[0])
	if !strings.HasPrefix(color, "#") {
		return ""
	}
	if len(color) == 4 {
		color = "#" + strings.Repeat(color[1:2], 2) + strings.Repeat(color[2:3], 2) + strings.Repeat(color[3:4], 2)
	}
	if len(color) != 7 {
		return ""
	}
	if _, err := strconv.ParseUint(color[1:], 16, 32); err != nil {
		return ""
	}
	return strings.ToUpper(color)
}
