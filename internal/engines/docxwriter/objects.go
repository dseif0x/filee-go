package docxwriter

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/images"
)

// Objects inside runs: pictures (inline or floating), text boxes and simple
// shapes (Word 2010 "wps" shapes inside mc:AlternateContent, as Word writes
// them), hyperlinks and bookmarks. Every drawing gets a unique docPr id.

const (
	pictureURI = "http://schemas.openxmlformats.org/drawingml/2006/picture"
	shapeURI   = "http://schemas.microsoft.com/office/word/2010/wordprocessingShape"
)

// pictureSize is a picture's size in pixels, or ok false (C# MagickImageInfo).
func pictureSize(path string) (int, int, bool) { return images.PictureSize(path) }

func (w *writer) picture(img *doc.Image) string {
	if st, err := os.Stat(img.Path); err != nil || st.IsDir() {
		return "" // missing picture: skip rather than write a broken reference
	}
	target := w.mediaTarget(img.Path)
	if target == "" {
		return ""
	}
	width, height := img.Width, img.Height
	if width == nil || height == nil {
		// Pixels at 96 dpi, like Word and browsers show pictures without a physical size.
		if pw, ph, ok := pictureSize(img.Path); ok {
			switch {
			case width == nil && height == nil:
				width, height = doc.Ptr(int(float64(pw)*doc.PerPixel)), doc.Ptr(int(float64(ph)*doc.PerPixel))
			case width != nil:
				height = doc.Ptr(int(float64(*width) * float64(ph) / float64(pw)))
			default:
				width = doc.Ptr(int(float64(*height) * float64(pw) / float64(ph)))
			}
		}
	}
	wd := max(1, doc.Val(width, 8504))
	ht := max(1, doc.Val(height, wd))
	if img.Anchor == nil && wd > w.width {
		// Inline pictures never overflow the text width.
		ht = int(int64(ht) * int64(w.width) / int64(wd))
		wd = w.width
	}
	rel := w.cur.relate(relBase+"image", target, false)
	id := w.nextDrawingID
	w.nextDrawingID++
	name := fmt.Sprintf("Picture %d", id)
	graphic := fmt.Sprintf(`<a:graphic><a:graphicData uri="%s"><pic:pic>`, pictureURI) +
		fmt.Sprintf(`<pic:nvPicPr><pic:cNvPr id="%d" name="%s"/><pic:cNvPicPr/></pic:nvPicPr>`, id, name) +
		fmt.Sprintf(`<pic:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>`, rel) +
		fmt.Sprintf(`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr>`, emu(wd), emu(ht)) +
		"</pic:pic></a:graphicData></a:graphic>"
	return "<w:r><w:drawing>" + w.frame(img.Anchor, wd, ht, id, name, graphic, `<a:graphicFrameLocks noChangeAspect="1"/>`) + "</w:drawing></w:r>"
}

// mediaTarget is the path (relative to word/) of a picture, added once.
// Formats Word may not show become PNG; "" when the file is no picture.
func (w *writer) mediaTarget(path string) string {
	if i, ok := w.mediaIndex[path]; ok {
		return strings.TrimPrefix(w.media[i].zipPath, "word/")
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	if ext == "jpeg" {
		ext = "jpg"
	}
	if _, ok := pictureTypes[ext]; !ok {
		if _, _, ok := pictureSize(path); !ok {
			return ""
		}
		ext = "png"
	}
	zipPath := fmt.Sprintf("word/media/image%d.%s", len(w.media)+1, ext)
	w.mediaIndex[path] = len(w.media)
	w.media = append(w.media, media{path, zipPath, ext})
	return strings.TrimPrefix(zipPath, "word/")
}

// frame is wp:inline for objects in the text, wp:anchor for floating ones.
func (w *writer) frame(a *doc.Anchor, width, height, id int, name, graphic, locks string) string {
	extent := fmt.Sprintf(`<wp:extent cx="%d" cy="%d"/><wp:effectExtent l="0" t="0" r="0" b="0"/>`, emu(width), emu(height))
	docPr := fmt.Sprintf(`<wp:docPr id="%d" name="%s"/><wp:cNvGraphicFramePr>%s</wp:cNvGraphicFramePr>`, id, name, locks)
	if a == nil {
		return `<wp:inline distT="0" distB="0" distL="0" distR="0">` + extent + docPr + graphic + "</wp:inline>"
	}
	behind := 0
	if a.Wrap == "BEHIND_TEXT" {
		behind = 1
	}
	wrap := "<wp:wrapNone/>"
	switch a.Wrap {
	case "SQUARE":
		wrap = `<wp:wrapSquare wrapText="bothSides"/>`
	case "TOP_AND_BOTTOM":
		wrap = "<wp:wrapTopAndBottom/>"
	}
	hRel := "column"
	switch a.HorizontalRelativeTo {
	case "PAPER":
		hRel = "page"
	case "PAGE":
		hRel = "margin"
	case "PARA":
		hRel = "character"
	}
	vRel := "paragraph"
	switch a.VerticalRelativeTo {
	case "PAPER":
		vRel = "page"
	case "PAGE":
		vRel = "margin"
	}
	overlap := 0
	if a.AllowOverlap {
		overlap = 1
	}
	rh := w.relativeHeight
	w.relativeHeight++
	return fmt.Sprintf(`<wp:anchor distT="0" distB="0" distL="114300" distR="114300" simplePos="0" relativeHeight="%d" behindDoc="%d" locked="0" layoutInCell="1" allowOverlap="%d">`, rh, behind, overlap) +
		`<wp:simplePos x="0" y="0"/>` + position("positionH", hRel, a.HorizontalAlign, a.HorizontalOffset, "LEFT") +
		position("positionV", vRel, a.VerticalAlign, a.VerticalOffset, "TOP") + extent + wrap + docPr + graphic + "</wp:anchor>"
}

// position is an aligned position without an offset, or an offset from the reference.
func position(element, relativeFrom, align string, offset int, start string) string {
	content := fmt.Sprintf("<wp:posOffset>%d</wp:posOffset>", emu(offset))
	if offset == 0 && align != start {
		switch align {
		case "CENTER", "RIGHT", "BOTTOM", "INSIDE", "OUTSIDE":
			content = "<wp:align>" + strings.ToLower(align) + "</wp:align>"
		}
	}
	return fmt.Sprintf(`<wp:%s relativeFrom="%s">%s</wp:%s>`, element, relativeFrom, content, element)
}

func (w *writer) textBox(box *doc.TextBox, ctx runContext) string {
	if w.textBoxDepth > 0 {
		// Word has no text boxes inside text boxes: keep the text in place.
		var sb strings.Builder
		for _, b := range box.Blocks {
			if p, ok := b.(*doc.Paragraph); ok {
				var keep []doc.Inline
				for _, in := range p.Inlines {
					switch in.(type) {
					case *doc.Text, *doc.LineBreak, *doc.Tab:
						keep = append(keep, in)
					}
				}
				w.inlines(keep, &sb, ctx)
			}
		}
		return sb.String()
	}
	wd, ht := max(1, box.Width), max(1, box.Height)
	outer := w.width
	w.width = max(1000, wd-box.Padding.Left-box.Padding.Right)
	w.textBoxDepth++
	content := w.blocks(box.Blocks, "", true, "", "")
	w.textBoxDepth--
	w.width = outer
	geometry := "rect"
	if box.Shape == doc.ShapeEllipse {
		geometry = "ellipse"
	} else if box.CornerRatio > 0 {
		geometry = "roundRect"
	}
	anchor := "t"
	switch box.VerticalAlign {
	case doc.VAlignCenter:
		anchor = "ctr"
	case doc.VAlignBottom:
		anchor = "b"
	}
	shape := `<wps:wsp><wps:cNvSpPr txBox="1"/>` +
		shapeProperties(wd, ht, geometry, box.CornerRatio, box.Fill, box.Gradient, box.Line, false, false) +
		"<wps:txbx><w:txbxContent>" + content + "</w:txbxContent></wps:txbx>" +
		fmt.Sprintf(`<wps:bodyPr rot="0" vert="horz" wrap="square" lIns="%d" tIns="%d" rIns="%d" bIns="%d" anchor="%s" anchorCtr="0"><a:noAutofit/></wps:bodyPr>`,
			emu(box.Padding.Left), emu(box.Padding.Top), emu(box.Padding.Right), emu(box.Padding.Bottom), anchor) +
		"</wps:wsp>"
	return w.drawingRun(box.Anchor, wd, ht, "Text Box", shape)
}

func (w *writer) shape(s *doc.Shape) string {
	wd, ht := max(1, s.Width), max(1, s.Height)
	geometry := "rect"
	switch {
	case s.Kind == doc.ShapeEllipse:
		geometry = "ellipse"
	case s.Kind == doc.ShapeLine:
		geometry = "line"
	case s.CornerRatio > 0:
		geometry = "roundRect"
	}
	line := s.Kind == doc.ShapeLine
	fill, grad := s.Fill, s.Gradient
	if line {
		fill, grad = "", nil
	}
	xml := "<wps:wsp><wps:cNvSpPr/>" + shapeProperties(wd, ht, geometry, s.CornerRatio, fill, grad, s.Line, s.FlipHorizontal, s.FlipVertical) + "<wps:bodyPr/></wps:wsp>"
	return w.drawingRun(s.Anchor, wd, ht, "Shape", xml)
}

// drawingRun is a wps shape in a run, inside mc:AlternateContent like Word writes it.
func (w *writer) drawingRun(a *doc.Anchor, width, height int, kind, shape string) string {
	id := w.nextDrawingID
	w.nextDrawingID++
	graphic := fmt.Sprintf(`<a:graphic><a:graphicData uri="%s">%s</a:graphicData></a:graphic>`, shapeURI, shape)
	return `<w:r><mc:AlternateContent><mc:Choice Requires="wps"><w:drawing>` + w.frame(a, width, height, id, fmt.Sprintf("%s %d", kind, id), graphic, "") +
		"</w:drawing></mc:Choice></mc:AlternateContent></w:r>"
}

// shapeProperties is wps:spPr: xfrm, preset geometry, fill and outline.
func shapeProperties(width, height int, geometry string, corner int, fill string, g *doc.Gradient, line doc.Border, flipH, flipV bool) string {
	var sb strings.Builder
	sb.WriteString("<wps:spPr><a:xfrm")
	if flipH {
		sb.WriteString(` flipH="1"`)
	}
	if flipV {
		sb.WriteString(` flipV="1"`)
	}
	fmt.Fprintf(&sb, `><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm>`, emu(width), emu(height))
	// 한글's corner ratio (50 = half circle) and DrawingML's "adj" scale alike.
	if geometry == "roundRect" {
		fmt.Fprintf(&sb, `<a:prstGeom prst="roundRect"><a:avLst><a:gd name="adj" fmla="val %d"/></a:avLst></a:prstGeom>`, max(0, min(50, corner))*1000)
	} else {
		fmt.Fprintf(&sb, `<a:prstGeom prst="%s"><a:avLst/></a:prstGeom>`, geometry)
	}
	switch {
	case g != nil && len(g.Colors) >= 2:
		sb.WriteString(`<a:gradFill rotWithShape="1"><a:gsLst>`)
		for i, c := range g.Colors {
			pos := int(math.Round(100000 * float64(i) / float64(len(g.Colors)-1)))
			hc := hexColor(c)
			if hc == "" {
				hc = "FFFFFF"
			}
			fmt.Fprintf(&sb, `<a:gs pos="%d"><a:srgbClr val="%s"/></a:gs>`, pos, hc)
		}
		fmt.Fprintf(&sb, `</a:gsLst><a:lin ang="%d" scaled="0"/></a:gradFill>`, ((g.Angle%360)+360)%360*60000)
	case hexColor(fill) != "":
		fmt.Fprintf(&sb, `<a:solidFill><a:srgbClr val="%s"/></a:solidFill>`, hexColor(fill))
	case geometry != "line":
		sb.WriteString("<a:noFill/>")
	}
	if line.Style == doc.BorderNone {
		sb.WriteString("<a:ln><a:noFill/></a:ln>")
	} else {
		dash := ""
		switch line.Style {
		case doc.BorderDash:
			dash = `<a:prstDash val="dash"/>`
		case doc.BorderDot:
			dash = `<a:prstDash val="sysDot"/>`
		case doc.BorderDashDot:
			dash = `<a:prstDash val="dashDot"/>`
		}
		cmpd := ""
		if line.Style == doc.BorderDouble {
			cmpd = ` cmpd="dbl"`
		}
		color := hexColor(line.Color)
		if color == "" {
			color = "000000"
		}
		fmt.Fprintf(&sb, `<a:ln w="%d"%s><a:solidFill><a:srgbClr val="%s"/></a:solidFill>%s</a:ln>`, max(3175, int64(math.Round(line.WidthMm*36000))), cmpd, color, dash)
	}
	sb.WriteString("</wps:spPr>")
	return sb.String()
}

func (w *writer) hyperlink(l *doc.Link, sb *strings.Builder, ctx runContext) {
	var content strings.Builder
	lc := ctx
	lc.link = true
	w.inlines(l.Content, &content, lc)
	if content.Len() == 0 {
		return
	}
	switch {
	case strings.HasPrefix(l.Target, "#"):
		fmt.Fprintf(sb, `<w:hyperlink w:anchor="%s" w:history="1">%s</w:hyperlink>`, esc(w.bookmarkName(l.Target[1:])), content.String())
	case l.Target != "":
		id := w.cur.relate(relBase+"hyperlink", safeURI(l.Target), true)
		fmt.Fprintf(sb, `<w:hyperlink r:id="%s" w:history="1">%s</w:hyperlink>`, id, content.String())
	default:
		sb.WriteString(content.String())
	}
}

// safeURI: absolute URIs are normalised, spaces in others escaped.
func safeURI(target string) string {
	if u, err := url.Parse(target); err == nil && u.IsAbs() && u.Scheme != "file" && len(u.Scheme) > 1 {
		return u.String()
	}
	return strings.ReplaceAll(strings.ReplaceAll(target, `\`, "/"), " ", "%20")
}

// bookmarkName: Word bookmark names start with a letter, hold letters,
// digits and underscores and are at most 40 characters. Names are mapped
// once, so a link to "#name" finds the bookmark written for "name".
func (w *writer) bookmarkName(name string) string {
	if k, ok := w.bookmarks[name]; ok {
		return k
	}
	var clean []rune
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			clean = append(clean, r)
		} else {
			clean = append(clean, '_')
		}
	}
	if len(clean) == 0 || !unicode.IsLetter(clean[0]) {
		clean = append([]rune{'b'}, clean...)
	}
	if len(clean) > 36 {
		clean = clean[:36]
	}
	base := string(clean)
	result := base
	for i := 2; w.bookmarkNames[result]; i++ {
		result = base + strconv.Itoa(i)
	}
	w.bookmarkNames[result] = true
	w.bookmarks[name] = result
	return result
}
