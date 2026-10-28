package hwpx

// Objects inside runs: pictures (inline or floating), text boxes, footnotes/endnotes, hyperlinks, page numbers.
// Child order follows files saved by 한글: object-specific children first, then sz / pos / outMargin.

import (
	"fmt"
	"math"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
)

// hyperlinkFieldID is the fieldid 한글 gives every hyperlink field (control id "%hlk").
const hyperlinkFieldID = "627600491"

const identityRendering = `<hp:renderingInfo><hc:transMatrix e1="1" e2="0" e3="0" e4="0" e5="1" e6="0"/><hc:scaMatrix e1="1" e2="0" e3="0" e4="0" e5="1" e6="0"/><hc:rotMatrix e1="1" e2="0" e3="0" e4="0" e5="1" e6="0"/></hp:renderingInfo>`

// ───────────────────────── Pictures ─────────────────────────

// naturalSize is the size of a picture: the given one, the other side from the aspect ratio, or its pixels at 96 dpi.
func naturalSize(path string, width, height *int) (int, int) {
	if width == nil || height == nil {
		if pw, ph, ok := imageSize(path); ok && pw > 0 && ph > 0 {
			switch {
			case width == nil && height == nil:
				width, height = ptr(int(float64(pw)*doc.PerPixel)), ptr(int(float64(ph)*doc.PerPixel))
			case width != nil:
				height = ptr(int(float64(*width) * float64(ph) / float64(pw)))
			default:
				width = ptr(int(float64(*height) * float64(pw) / float64(ph)))
			}
		}
	}
	w := max(1, val(width, 8504))
	h := max(1, val(height, w))
	return w, h
}

func (w *writer) picture(image *doc.Image) string {
	if !fileExists(image.Path) {
		return "" // missing image: skip rather than produce a broken reference
	}
	e := ext(image.Path)
	if !hwpImageTypes[e] && !readable(image.Path) {
		return "" // no picture Go can convert to PNG
	}
	pw, ph := naturalSize(image.Path, image.Width, image.Height)
	if image.Anchor == nil && pw > w.width {
		// Inline pictures never overflow the text width.
		ph = int(int64(ph) * int64(w.width) / int64(pw))
		pw = w.width
	}
	binaryID := ""
	for _, i := range w.images {
		if i.source == image.Path {
			binaryID = i.id
			break
		}
	}
	if binaryID == "" {
		binaryID = fmt.Sprintf("image%d", len(w.images)+1)
		w.images = append(w.images, imageEntry{binaryID, image.Path, e})
	}
	id := w.nextID()
	wrap := "TOP_AND_BOTTOM"
	numbering := "NONE"
	if image.Anchor != nil {
		wrap, numbering = image.Anchor.Wrap, "PICTURE"
	}
	z := w.zOrder
	w.zOrder++
	return fmt.Sprintf(`<hp:pic id="%d" zOrder="%d" numberingType="%s" textWrap="%s" textFlow="BOTH_SIDES" lock="0" dropcapstyle="None" href="" groupLevel="0" instid="%d" reverse="0">`, id, z, numbering, wrap, id) +
		`<hp:offset x="0" y="0"/>` +
		fmt.Sprintf(`<hp:orgSz width="%d" height="%d"/><hp:curSz width="%d" height="%d"/>`, pw, ph, pw, ph) +
		`<hp:flip horizontal="0" vertical="0"/>` +
		fmt.Sprintf(`<hp:rotationInfo angle="0" centerX="%d" centerY="%d" rotateimage="1"/>`, pw/2, ph/2) +
		identityRendering +
		fmt.Sprintf(`<hc:img binaryItemIDRef="%s" bright="0" contrast="0" effect="REAL_PIC" alpha="0"/>`, binaryID) +
		fmt.Sprintf(`<hp:imgRect><hc:pt0 x="0" y="0"/><hc:pt1 x="%d" y="0"/><hc:pt2 x="%d" y="%d"/><hc:pt3 x="0" y="%d"/></hp:imgRect>`, pw, pw, ph, ph) +
		`<hp:imgClip left="0" right="0" top="0" bottom="0"/><hp:inMargin left="0" right="0" top="0" bottom="0"/>` +
		`<hp:imgDim dimwidth="0" dimheight="0"/><hp:effects/>` +
		fmt.Sprintf(`<hp:sz width="%d" widthRelTo="ABSOLUTE" height="%d" heightRelTo="ABSOLUTE" protect="0"/>`, pw, ph) +
		w.position(image.Anchor) +
		`<hp:outMargin left="0" right="0" top="0" bottom="0"/><hp:shapeComment/>` +
		`</hp:pic>`
}

// ───────────────────────── Equations ─────────────────────────

// equationMargin is the space left and right of an equation (as 한글 inserts them).
const equationMargin = 56

// equation is a 한글 equation laid out like a character. The box is the size rhwp lays the script out at, so it
// draws the equation unstretched; baseLine is where the text line runs, in percent of the height from the top.
func (w *writer) equation(math_ MathNode, format doc.CharFormat, linearText, charPrBase string) string {
	size := max(100, val(format.Size, w.baseSize(charPrBase)))
	box := MeasureMath(math_, float64(size))
	width := max(1, int(math.Ceil(box.Width)))
	height := max(1, int(math.Ceil(box.Height)))
	baseline := 85
	if box.Height > 0 {
		baseline = clamp(iround(box.Baseline*100/box.Height), 0, 100)
	}
	id := w.nextID()
	z := w.zOrder
	w.zOrder++
	return fmt.Sprintf(`<hp:equation id="%d" zOrder="%d" numberingType="EQUATION" textWrap="TOP_AND_BOTTOM" textFlow="BOTH_SIDES" lock="0" dropcapstyle="None" `, id, z) +
		fmt.Sprintf(`version="Equation Version 60" baseLine="%d" textColor="%s" baseUnit="%d" lineMode="CHAR" font="HYhwpEQ">`, baseline, val(format.Color, "#000000"), size) +
		fmt.Sprintf(`<hp:sz width="%d" widthRelTo="ABSOLUTE" height="%d" heightRelTo="ABSOLUTE" protect="0"/>`, width, height) +
		w.position(nil) +
		// The comment (alternative text) is the linear form. Never an empty element: rhwp reads an empty
		// <hp:shapeComment/> of an equation up to the next closing tag and loses the rest of the section.
		fmt.Sprintf(`<hp:outMargin left="%d" right="%d" top="0" bottom="0"/><hp:shapeComment>%s</hp:shapeComment>`, equationMargin, equationMargin, escape(linearText)) +
		`<hp:script>` + escape(Script(math_)) + `</hp:script></hp:equation>`
}

// position is hp:pos: inline objects are laid out like a character; floating ones use the anchor.
func (w *writer) position(anchor *doc.Anchor) string {
	if anchor == nil {
		return `<hp:pos treatAsChar="1" affectLSpacing="0" flowWithText="1" allowOverlap="1" holdAnchorAndSO="0" vertRelTo="PARA" horzRelTo="COLUMN" vertAlign="TOP" horzAlign="LEFT" vertOffset="0" horzOffset="0"/>`
	}
	a := w.nonNegative(*anchor)
	flowWithText := b01(a.VerticalRelativeTo == "PARA")
	return fmt.Sprintf(`<hp:pos treatAsChar="0" affectLSpacing="0" flowWithText="%s" allowOverlap="%s" holdAnchorAndSO="0" `, flowWithText, b01(a.AllowOverlap)) +
		fmt.Sprintf(`vertRelTo="%s" horzRelTo="%s" vertAlign="%s" horzAlign="%s" `, a.VerticalRelativeTo, a.HorizontalRelativeTo, a.VerticalAlign, a.HorizontalAlign) +
		fmt.Sprintf(`vertOffset="%d" horzOffset="%d"/>`, a.VerticalOffset, a.HorizontalOffset)
}

// nonNegative: object offsets are unsigned in the 한글 format, and readers fail on negative values. An object reaching
// into the margin is re-anchored to the paper edge, which keeps it in place; one above its paragraph (which has no
// paper-relative equivalent) moves down to the paragraph.
func (w *writer) nonNegative(a doc.Anchor) doc.Anchor {
	if a.HorizontalOffset < 0 {
		if (a.HorizontalRelativeTo == "PAGE" || a.HorizontalRelativeTo == "COLUMN") && a.HorizontalAlign == "LEFT" {
			a.HorizontalRelativeTo, a.HorizontalOffset = "PAPER", max(0, w.pageLeft+a.HorizontalOffset)
		} else {
			a.HorizontalOffset = 0
		}
	}
	if a.VerticalOffset < 0 {
		if a.VerticalRelativeTo == "PAGE" && a.VerticalAlign == "TOP" {
			a.VerticalRelativeTo, a.VerticalOffset = "PAPER", max(0, w.pageTop+a.VerticalOffset)
		} else {
			a.VerticalOffset = 0
		}
	}
	return a
}

// ───────────────────────── Text boxes ─────────────────────────

func verticalAlignName(v doc.VerticalAlign) string {
	switch v {
	case doc.VAlignCenter:
		return "CENTER"
	case doc.VAlignBottom:
		return "BOTTOM"
	}
	return "TOP"
}

func wrapOf(anchor *doc.Anchor) string {
	if anchor == nil {
		return "TOP_AND_BOTTOM"
	}
	return anchor.Wrap
}

func (w *writer) textBox(box *doc.TextBox) string {
	id := w.nextID()
	bw, bh := box.Width, box.Height

	// Readers lay out (and align) the text within textWidth, so it is the box minus its margins.
	outerWidth := w.width
	w.width = max(1000, bw-box.Padding.Left-box.Padding.Right)
	content := w.subList(box.Blocks, verticalAlignName(box.VerticalAlign), w.width, max(0, bh-box.Padding.Top-box.Padding.Bottom), "")
	w.width = outerWidth

	ellipse := box.Shape == doc.ShapeEllipse
	element, extra := "rect", fmt.Sprintf(` ratio="%d"`, box.CornerRatio)
	if ellipse {
		element, extra = "ellipse", ` intervalDirty="0" hasArcPr="0" arcType="NORMAL"`
	}
	z := w.zOrder
	w.zOrder++
	var sb strings.Builder
	fmt.Fprintf(&sb, `<hp:%s id="%d" zOrder="%d" numberingType="NONE" textWrap="%s" textFlow="BOTH_SIDES" lock="0" dropcapstyle="None" href="" groupLevel="0" instid="%d"%s>`, element, id, z, wrapOf(box.Anchor), id, extra)
	sb.WriteString(`<hp:offset x="0" y="0"/>`)
	fmt.Fprintf(&sb, `<hp:orgSz width="%d" height="%d"/><hp:curSz width="0" height="0"/>`, bw, bh)
	sb.WriteString(`<hp:flip horizontal="0" vertical="0"/>`)
	fmt.Fprintf(&sb, `<hp:rotationInfo angle="0" centerX="%d" centerY="%d" rotateimage="1"/>`, bw/2, bh/2)
	sb.WriteString(identityRendering)
	sb.WriteString(lineShape(box.Line))
	sb.WriteString(fillBrush(box.Fill, box.Gradient))
	sb.WriteString(`<hp:shadow type="NONE" color="#B2B2B2" offsetX="0" offsetY="0" alpha="0"/>`)
	fmt.Fprintf(&sb, `<hp:drawText lastWidth="%d" name="" editable="0">`, bw)
	sb.WriteString(content)
	fmt.Fprintf(&sb, `<hp:textMargin left="%d" right="%d" top="%d" bottom="%d"/>`, box.Padding.Left, box.Padding.Right, box.Padding.Top, box.Padding.Bottom)
	sb.WriteString("</hp:drawText>")
	if ellipse {
		sb.WriteString(ellipseGeometry(bw, bh))
	} else {
		sb.WriteString(rectangleGeometry(bw, bh))
	}
	fmt.Fprintf(&sb, `<hp:sz width="%d" widthRelTo="ABSOLUTE" height="%d" heightRelTo="ABSOLUTE" protect="0"/>`, bw, bh)
	sb.WriteString(w.position(box.Anchor))
	sb.WriteString(`<hp:outMargin left="0" right="0" top="0" bottom="0"/>`)
	sb.WriteString("</hp:" + element + ">")
	return sb.String()
}

func rectangleGeometry(w, h int) string {
	return fmt.Sprintf(`<hc:pt0 x="0" y="0"/><hc:pt1 x="%d" y="0"/><hc:pt2 x="%d" y="%d"/><hc:pt3 x="0" y="%d"/>`, w, w, h, h)
}

func ellipseGeometry(w, h int) string {
	return fmt.Sprintf(`<hc:center x="%d" y="%d"/><hc:ax1 x="%d" y="%d"/><hc:ax2 x="%d" y="0"/>`, w/2, h/2, w, h/2, w/2) +
		`<hc:start1 x="0" y="0"/><hc:end1 x="0" y="0"/><hc:start2 x="0" y="0"/><hc:end2 x="0" y="0"/>`
}

// ───────────────────────── Shapes ─────────────────────────

func (w *writer) shape(shape *doc.Shape) string {
	id := w.nextID()
	sw, sh := max(1, shape.Width), max(1, shape.Height)
	element, extra := "rect", fmt.Sprintf(` ratio="%d"`, shape.CornerRatio)
	switch shape.Kind {
	case doc.ShapeEllipse:
		element, extra = "ellipse", ` intervalDirty="0" hasArcPr="0" arcType="NORMAL"`
	case doc.ShapeLine:
		element, extra = "line", ` isReverseHV="0"`
	}
	z := w.zOrder
	w.zOrder++
	var sb strings.Builder
	fmt.Fprintf(&sb, `<hp:%s id="%d" zOrder="%d" numberingType="NONE" textWrap="%s" textFlow="BOTH_SIDES" lock="0" dropcapstyle="None" href="" groupLevel="0" instid="%d"%s>`, element, id, z, wrapOf(shape.Anchor), id, extra)
	sb.WriteString(`<hp:offset x="0" y="0"/>`)
	fmt.Fprintf(&sb, `<hp:orgSz width="%d" height="%d"/><hp:curSz width="0" height="0"/>`, sw, sh)
	sb.WriteString(`<hp:flip horizontal="0" vertical="0"/>`)
	fmt.Fprintf(&sb, `<hp:rotationInfo angle="0" centerX="%d" centerY="%d" rotateimage="1"/>`, sw/2, sh/2)
	sb.WriteString(identityRendering)
	sb.WriteString(lineShape(shape.Line))
	if shape.Kind != doc.ShapeLine {
		sb.WriteString(fillBrush(shape.Fill, shape.Gradient))
	}
	sb.WriteString(`<hp:shadow type="NONE" color="#B2B2B2" offsetX="0" offsetY="0" alpha="0"/>`)
	switch shape.Kind {
	case doc.ShapeLine:
		// A line runs corner to corner of its box; flips choose which corners.
		x1, x2 := 0, shape.Width
		if shape.FlipHorizontal {
			x1, x2 = shape.Width, 0
		}
		y1, y2 := 0, shape.Height
		if shape.FlipVertical {
			y1, y2 = shape.Height, 0
		}
		fmt.Fprintf(&sb, `<hc:startPt x="%d" y="%d"/><hc:endPt x="%d" y="%d"/>`, x1, y1, x2, y2)
	case doc.ShapeEllipse:
		sb.WriteString(ellipseGeometry(sw, sh))
	default:
		sb.WriteString(rectangleGeometry(sw, sh))
	}
	fmt.Fprintf(&sb, `<hp:sz width="%d" widthRelTo="ABSOLUTE" height="%d" heightRelTo="ABSOLUTE" protect="0"/>`, sw, sh)
	sb.WriteString(w.position(shape.Anchor))
	sb.WriteString(`<hp:outMargin left="0" right="0" top="0" bottom="0"/>`)
	sb.WriteString("</hp:" + element + ">")
	return sb.String()
}

// lineShape is the outline of a drawing object; the width is in HWPUNIT here (33 ≈ 0.12 mm), not a named width.
func lineShape(line doc.Border) string {
	none := line.Style == doc.BorderNone
	width, style := 0, "NONE"
	if !none {
		width = max(1, iround(line.WidthMm*doc.PerMm))
		style = lineType(line.Style)
	}
	return fmt.Sprintf(`<hp:lineShape color="%s" width="%d" style="%s" endCap="FLAT" headStyle="NORMAL" tailStyle="NORMAL" headfill="1" tailfill="1" headSz="MEDIUM_MEDIUM" tailSz="MEDIUM_MEDIUM" outlineStyle="NORMAL" alpha="0"/>`, line.Color, width, style)
}

// fillBrush: hc:fillBrush holds exactly one of winBrush (solid) or gradation; no brush = transparent.
func fillBrush(fill string, gradient *doc.Gradient) string {
	if gradient != nil && len(gradient.Colors) >= 2 {
		var colors strings.Builder
		for _, c := range gradient.Colors {
			colors.WriteString(`<hc:color value="` + c + `"/>`)
		}
		return fmt.Sprintf(`<hc:fillBrush><hc:gradation type="LINEAR" angle="%d" centerX="50" centerY="50" step="255" colorNum="%d" stepCenter="50" alpha="0">%s</hc:gradation></hc:fillBrush>`, gradient.Angle, len(gradient.Colors), colors.String())
	}
	if fill == "" {
		return ""
	}
	return `<hc:fillBrush><hc:winBrush faceColor="` + fill + `" hatchColor="#000000" alpha="0"/></hc:fillBrush>`
}

// ───────────────────────── Notes ─────────────────────────

func (w *writer) note(note *doc.Note) string {
	kind := "footNote"
	var number int
	if note.Endnote {
		kind = "endNote"
		w.endnotes++
		number = w.endnotes
	} else {
		w.footnotes++
		number = w.footnotes
	}
	previous := w.base
	if w.footnoteStyle != nil {
		w.base = *w.footnoteStyle
	}
	// The number is an autoNum control at the start of the note's first paragraph (as 한글 writes it).
	numType := "FOOTNOTE"
	if note.Endnote {
		numType = "ENDNOTE"
	}
	numberRun := `<hp:run charPrIDRef="` + w.base.charPr + `">` + autoNumber(numType, number, ")") + `<hp:t> </hp:t></hp:run>`
	content := w.subList(note.Blocks, "TOP", 0, 0, numberRun)
	w.base = previous
	return fmt.Sprintf(`<hp:ctrl><hp:%s number="%d" suffixChar="41" instId="%d">%s</hp:%s></hp:ctrl>`, kind, number, w.nextID(), content, kind)
}

// autoNumber is an automatic number: PAGE, TOTAL_PAGE, FOOTNOTE or ENDNOTE (number is a display cache).
func autoNumber(kind string, number int, suffix string) string {
	return fmt.Sprintf(`<hp:ctrl><hp:autoNum num="%d" numType="%s">`, number, kind) +
		`<hp:autoNumFormat type="DIGIT" userChar="" prefixChar="" suffixChar="` + escape(suffix) + `" supscript="0"/>` +
		`</hp:autoNum></hp:ctrl>`
}

// ───────────────────────── Hyperlinks ─────────────────────────

// fieldBegin is a HYPERLINK field as 한글 stores it: the target in "Command" with ':', '?', ';' and '#' escaped,
// followed by a kind tail (";1;0;0;" web, ";2;0;0" mail, "?name;0;0;0;" bookmark in this document).
func fieldBegin(id int, target string) string {
	escaped := func(text string) string {
		var sb strings.Builder
		for _, c := range text {
			if c == ':' || c == '?' || c == ';' || c == '#' {
				sb.WriteByte('\\')
			}
			sb.WriteRune(c)
		}
		return sb.String()
	}
	var command, category, path string
	hasPath := true
	switch {
	case strings.HasPrefix(target, "#"):
		command, category, hasPath = "?"+escaped(target[1:])+";0;0;0;", "HWPHYPERLINK_TYPE_HWP", false
	case len(target) >= 7 && strings.EqualFold(target[:7], "mailto:"):
		command, path, category = target+";2;0;0", target, "HWPHYPERLINK_TYPE_EMAIL"
	default:
		command, path, category = escaped(target)+";1;0;0;", target, "HWPHYPERLINK_TYPE_URL"
	}
	var parameters strings.Builder
	parameters.WriteString(`<hp:integerParam name="Prop">0</hp:integerParam>`)
	parameters.WriteString(`<hp:stringParam name="Command">` + escape(command) + `</hp:stringParam>`)
	if hasPath {
		parameters.WriteString(`<hp:stringParam name="Path">` + escape(path) + `</hp:stringParam>`)
	}
	parameters.WriteString(`<hp:stringParam name="Category">` + category + `</hp:stringParam>`)
	parameters.WriteString(`<hp:stringParam name="TargetType">HWPHYPERLINK_TARGET_BOOKMARK</hp:stringParam>`)
	parameters.WriteString(`<hp:stringParam name="DocOpenType">HWPHYPERLINK_JUMP_CURRENTTAB</hp:stringParam>`)
	count := 5
	if hasPath {
		count = 6
	}
	return fmt.Sprintf(`<hp:ctrl><hp:fieldBegin id="%d" type="HYPERLINK" name="" editable="0" dirty="1" zorder="-1" fieldid="%s">`, id, hyperlinkFieldID) +
		fmt.Sprintf(`<hp:parameters cnt="%d" name="">%s</hp:parameters></hp:fieldBegin></hp:ctrl>`, count, parameters.String())
}
