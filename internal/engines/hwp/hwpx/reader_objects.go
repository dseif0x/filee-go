package hwpx

// Objects inside runs: pictures (inline or floating), rectangles / ellipses / lines, text boxes (drawing objects with
// hp:drawText), groups (hp:container), equations and other objects that only carry text. Objects the model cannot
// hold (OLE, charts, video, form controls, free-form shapes without text) are skipped and counted.

import (
	"math"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/hwp/xmltree"
)

// shapeNames are the drawing objects that may appear in a run or a group.
var shapeNames = map[string]bool{"pic": true, "rect": true, "ellipse": true, "line": true, "arc": true, "polygon": true, "curve": true, "connectLine": true, "container": true}

// placement is the size and position of an object: anchor is nil for objects laid out like a character.
type placement struct {
	width, height int
	anchor        *doc.Anchor
}

func (r *hwpxReader) drawingObject(element *xmltree.Element, b *paragraphBuilder, format doc.CharFormat) {
	name := element.Local
	if shapeNames[name] {
		for _, in := range r.shapes(element, placementOf(element)) {
			b.add(in)
		}
		return
	}
	attrOrChild := func(name string) string {
		if v, ok := element.Attr(name); ok {
			return v
		}
		if c := child(element, name); c != nil {
			return c.Value()
		}
		return ""
	}
	switch name {
	case "equation":
		// The equation script (한글's equation language, e.g. "x over y") is the closest text form.
		if script := strings.TrimSpace(child(element, "script").Value()); script != "" {
			b.addText(script, format)
		}
	case "textart":
		b.addText(attrOrChild("text"), format)
	case "compose":
		// Overlapping characters (글자 겹치기): the characters one after the other.
		b.addText(attrOrChild("composeText"), format)
	case "dutmal":
		// Ruby text (덧말): the main text; the small text above it has no place in the model.
		b.addText(attrOrChild("mainText"), format)
	default:
		r.skip(name) // ole, chart, video, form controls, ...
	}
}

// shapes are the model objects for one drawing object; groups give one object per member.
func (r *hwpxReader) shapes(element *xmltree.Element, p placement) []doc.Inline {
	switch element.Local {
	case "pic":
		if picture := r.picture(element, p); picture != nil {
			return []doc.Inline{picture}
		}
		r.skip("pic")
		return nil
	case "rect":
		return []doc.Inline{r.box(element, doc.ShapeRectangle, p)}
	case "ellipse":
		return []doc.Inline{r.box(element, doc.ShapeEllipse, p)}
	case "line":
		start, end := child(element, "startPt"), child(element, "endPt")
		points := start != nil && end != nil
		line := &doc.Shape{Kind: doc.ShapeLine, Line: doc.ThinBorder}
		line.Width, line.Height = p.width, p.height
		// A horizontal or vertical line still has a size of one unit across in the file.
		if points && intAttr(start, "x", 0) == intAttr(end, "x", 0) {
			line.Width = 0
		}
		if points && intAttr(start, "y", 0) == intAttr(end, "y", 0) {
			line.Height = 0
		}
		line.Anchor = p.anchor
		line.Line = lineShapeOf(child(element, "lineShape"))
		// A line runs corner to corner of its box; the end points say which corners.
		line.FlipHorizontal = intAttr(start, "x", 0) > intAttr(end, "x", 0)
		line.FlipVertical = intAttr(start, "y", 0) > intAttr(end, "y", 0)
		return []doc.Inline{line}
	case "container":
		return r.group(element, p)
	}
	// Arcs, polygons, curves and connectors: only their text survives, in a box without outline.
	if child(element, "drawText") != nil {
		box := r.box(element, doc.ShapeRectangle, p).(*doc.TextBox)
		box.Line = doc.NoBorder
		box.Fill = ""
		box.Gradient = nil
		return []doc.Inline{box}
	}
	r.skip(element.Local)
	return nil
}

func (r *hwpxReader) picture(pic *xmltree.Element, p placement) *doc.Image {
	var image *xmltree.Element
	pic.Walk(func(e *xmltree.Element) bool {
		if image == nil && e.Local == "img" && hasAttr(e, "binaryItemIDRef") {
			image = e
		}
		return image == nil
	})
	path := r.extractBinary(image.A("binaryItemIDRef"))
	if path == "" {
		return nil
	}
	img := &doc.Image{Path: path, Anchor: p.anchor}
	if p.width > 0 {
		img.Width = ptr(p.width)
	}
	if p.height > 0 {
		img.Height = ptr(p.height)
	}
	return img
}

// box is a rectangle or ellipse: a text box when it has text (hp:drawText), else a plain shape.
func (r *hwpxReader) box(element *xmltree.Element, kind doc.ShapeKind, p placement) doc.Inline {
	line := lineShapeOf(child(element, "lineShape"))
	fill, gradient := fillBrushOf(child(element, "fillBrush"))
	ratio := 0
	if kind == doc.ShapeRectangle {
		ratio = intAttr(element, "ratio", 0)
	}
	drawText := child(element, "drawText")
	if drawText == nil {
		return &doc.Shape{Kind: kind, Width: p.width, Height: p.height, Anchor: p.anchor, Fill: fill, Gradient: gradient, Line: line, CornerRatio: ratio}
	}
	subList := child(drawText, "subList")
	margin := child(drawText, "textMargin")
	box := doc.NewTextBox()
	box.Width, box.Height, box.Anchor = p.width, p.height, p.anchor
	box.Fill, box.Gradient, box.Line = fill, gradient, line
	box.Shape, box.CornerRatio = kind, ratio
	box.VerticalAlign = verticalAlignOf(subList.A("vertAlign"))
	if margin != nil {
		box.Padding = doc.Insets{Left: intAttr(margin, "left", 0), Right: intAttr(margin, "right", 0), Top: intAttr(margin, "top", 0), Bottom: intAttr(margin, "bottom", 0)}
	}
	box.Blocks = r.readSubList(subList)
	return box
}

// group: members of a group (묶음 개체) become separate objects at their place inside the group. An inline group
// floats over its paragraph instead, like the DOCX reader does with inline groups.
func (r *hwpxReader) group(container *xmltree.Element, p placement) []doc.Inline {
	anchor := doc.Anchor{HorizontalRelativeTo: "COLUMN", HorizontalAlign: "LEFT", VerticalRelativeTo: "PARA", VerticalAlign: "TOP", Wrap: "IN_FRONT_OF_TEXT", AllowOverlap: true}
	if p.anchor != nil {
		anchor = *p.anchor
	}
	var out []doc.Inline
	for _, member := range elements(container) {
		if !shapeNames[member.Local] {
			continue // the group's own size, position and rendering properties
		}
		offset := child(member, "offset")
		x, y := intAttr(offset, "x", 0), intAttr(offset, "y", 0)
		width, height := memberSize(member)
		a := anchor
		a.HorizontalOffset = anchor.HorizontalOffset + alignedOffset(anchor.HorizontalAlign, x, width, p.width)
		a.VerticalOffset = anchor.VerticalOffset + alignedOffset(anchor.VerticalAlign, y, height, p.height)
		a.AllowOverlap = true
		out = append(out, r.shapes(member, placement{width, height, &a})...)
	}
	return out
}

// alignedOffset keeps a member's distance from the group's centre or far edge when the group is aligned, not placed.
func alignedOffset(align string, position, size, groupSize int) int {
	switch align {
	case "CENTER":
		return position + size/2 - groupSize/2
	case "RIGHT", "BOTTOM":
		return position + size - groupSize
	}
	return position
}

// memberSize is the size of a group member: its current size, else its original size scaled by its rendering matrix.
func memberSize(member *xmltree.Element) (int, int) {
	current := child(member, "curSz")
	if intAttr(current, "width", 0) > 0 && intAttr(current, "height", 0) > 0 {
		return intAttr(current, "width", 0), intAttr(current, "height", 0)
	}
	original := child(member, "orgSz")
	scale := child(child(member, "renderingInfo"), "scaMatrix")
	return iround(float64(intAttr(original, "width", 0)) * floatAttr(scale, "e1", 1)), iround(float64(intAttr(original, "height", 0)) * floatAttr(scale, "e5", 1))
}

func placementOf(element *xmltree.Element) placement {
	size := child(element, "sz")
	width, height := intAttr(size, "width", 0), intAttr(size, "height", 0)
	if width <= 0 || height <= 0 {
		width, height = memberSize(element)
	}
	return placement{width, height, anchorOf(element)}
}

// anchorOf is the position of a floating object (hp:pos); nil when it is laid out like a character (treatAsChar).
func anchorOf(element *xmltree.Element) *doc.Anchor {
	pos := child(element, "pos")
	if pos == nil || flag(pos, "treatAsChar", false) {
		return nil
	}
	or := func(name, def string) string {
		if v, ok := pos.Attr(name); ok {
			return v
		}
		return def
	}
	wrap := "TOP_AND_BOTTOM"
	switch element.A("textWrap") {
	case "SQUARE", "TIGHT", "THROUGH":
		wrap = "SQUARE"
	case "BEHIND_TEXT":
		wrap = "BEHIND_TEXT"
	case "IN_FRONT_OF_TEXT":
		wrap = "IN_FRONT_OF_TEXT"
	}
	return &doc.Anchor{
		HorizontalRelativeTo: or("horzRelTo", "COLUMN"),
		HorizontalAlign:      or("horzAlign", "LEFT"),
		HorizontalOffset:     intAttr(pos, "horzOffset", 0),
		VerticalRelativeTo:   or("vertRelTo", "PARA"),
		VerticalAlign:        or("vertAlign", "TOP"),
		VerticalOffset:       intAttr(pos, "vertOffset", 0),
		Wrap:                 wrap,
		AllowOverlap:         flag(pos, "allowOverlap", false),
	}
}

// lineShapeOf is the outline of a drawing object; its width is in HWPUNIT (a width of 0 is 한글's thinnest line).
func lineShapeOf(lineShape *xmltree.Element) doc.Border {
	style := lineStyleOf(lineShape.A("style"), hasAttr(lineShape, "style"))
	if lineShape == nil || style == doc.BorderNone {
		return doc.NoBorder
	}
	width := intAttr(lineShape, "width", 0)
	mm := 0.1
	if width > 0 {
		mm = float64(width) / doc.PerMm
	}
	color := hwpColor(lineShape.A("color"))
	if color == "" {
		color = "#000000"
	}
	return doc.Border{Style: style, WidthMm: mm, Color: color}
}

// fillBrushOf is a solid colour (hc:winBrush) or linear gradient (hc:gradation); picture fills are not kept.
func fillBrushOf(fillBrush *xmltree.Element) (string, *doc.Gradient) {
	if gradation := child(fillBrush, "gradation"); gradation != nil {
		var colors []string
		for _, c := range children(gradation, "color") {
			if v := hwpColor(c.A("value")); v != "" {
				colors = append(colors, v)
			}
		}
		if len(colors) >= 2 {
			return "", &doc.Gradient{Colors: colors, Angle: intAttr(gradation, "angle", 0)}
		}
		if len(colors) == 1 {
			return colors[0], nil
		}
	}
	return hwpColor(child(fillBrush, "winBrush").A("faceColor")), nil
}

func floatAttr(e *xmltree.Element, name string, fallback float64) float64 {
	if v, err := strconv.ParseFloat(e.A(name), 64); err == nil && v != 0 && !math.IsNaN(v) {
		return v
	}
	return fallback
}
