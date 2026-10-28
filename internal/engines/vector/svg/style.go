package svg

import (
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
)

type paintKind uint8

const (
	paintNone paintKind = iota
	paintColor
	paintURL
	paintCurrentColor
	paintContextFill
	paintContextStroke
)

type paintSpec struct {
	kind     paintKind
	color    gfx.Color
	url      string
	fallback *paintSpec
}

// style is the computed style of an element.
type style struct {
	// inherited
	fill, stroke               paintSpec
	fillOpacity, strokeOpacity float64
	fillRule, clipRule         gfx.FillRule
	strokeWidth                Length
	lineCap                    gfx.LineCap
	lineJoin                   gfx.LineJoin
	miterLimit                 float64
	dashArray                  []Length
	dashOffset                 Length
	color                      gfx.Color
	visible                    bool
	fontFamily                 []string
	fontSize                   float64
	fontWeight                 float64
	italic                     bool
	textAnchor                 string
	letterSpacing              float64
	wordSpacing                float64
	markerStart, markerMid     string
	markerEnd                  string
	paintOrder                 []string
	preserveSpace              bool
	direction                  string
	writingMode                string
	lang                       string
	textDecorationInherited    []string
	colorInterp                string

	// not inherited
	display          bool
	opacity          float64
	clipPath, mask   string
	filter           string
	stopColor        gfx.Color
	stopOpacity      float64
	nonScalingStroke bool
	overflowVisible  bool
	baseline         string
	baselineShift    string
	textDecoration   []string
	floodColor       gfx.Color
	floodOpacity     float64
	lightingColor    gfx.Color
	maskType         gfx.MaskKind
	transformCSS     string
	blend            gfx.BlendMode
	isolate          bool
}

func defaultStyle() *style {
	return &style{
		fill:          paintSpec{kind: paintColor, color: gfx.Black},
		stroke:        paintSpec{kind: paintNone},
		fillOpacity:   1,
		strokeOpacity: 1,
		strokeWidth:   Length{V: 1},
		miterLimit:    4,
		color:         gfx.Black,
		visible:       true,
		fontFamily:    []string{"sans-serif"},
		fontSize:      16,
		fontWeight:    400,
		textAnchor:    "start",
		lineJoin:      gfx.MiterJoin,
		lineCap:       gfx.ButtCap,
		display:       true,
		opacity:       1,
		stopColor:     gfx.Black,
		stopOpacity:   1,
		floodColor:    gfx.Black,
		floodOpacity:  1,
		lightingColor: gfx.White,
		lang:          "en",
	}
}

// spec returns the specified value of a property: style sheets and the style
// attribute win over presentation attributes.
func spec(n *Node, prop string) (string, bool) {
	if n.styles != nil {
		if v, ok := n.styles[prop]; ok {
			return v, true
		}
	}
	if v, ok := n.Attrs[prop]; ok {
		return v, true
	}
	return "", false
}

// computeStyle derives n's style from its parent's.
func computeStyle(n *Node, parent *style, lc lengthContext) *style {
	s := *parent
	s.dashArray = parent.dashArray
	// Reset the properties that are not inherited.
	d := defaultStyle()
	s.display, s.opacity, s.clipPath, s.mask, s.filter = true, 1, "", "", ""
	s.stopColor, s.stopOpacity = d.stopColor, d.stopOpacity
	s.nonScalingStroke, s.overflowVisible = false, false
	s.baseline, s.baselineShift = "", ""
	s.textDecoration = nil
	s.floodColor, s.floodOpacity, s.lightingColor = d.floodColor, d.floodOpacity, d.lightingColor
	s.maskType = gfx.MaskLuminance
	s.transformCSS = ""
	s.blend, s.isolate = gfx.BlendNormal, false
	if n.Name == "svg" || n.Name == "symbol" || n.Name == "marker" || n.Name == "pattern" {
		s.overflowVisible = false
	}

	get := func(prop string) (string, bool) {
		v, ok := spec(n, prop)
		if !ok {
			return "", false
		}
		v = strings.TrimSpace(v)
		if v == "inherit" || v == "" {
			return "", false
		}
		return v, true
	}

	// color first: currentColor refers to it.
	if v, ok := get("color"); ok {
		if c, ok := parseColor(v); ok {
			s.color = c
		}
	}
	if v, ok := get("font-size"); ok {
		s.fontSize = parseFontSize(v, parent.fontSize, lc)
	}
	if v, ok := get("font"); ok {
		parseFontShorthand(v, &s, parent.fontSize, lc)
	}
	flc := lc
	flc.fontSize = s.fontSize
	if v, ok := get("fill"); ok {
		if p, ok := parsePaint(v, s.color); ok {
			s.fill = p
		}
	}
	if v, ok := get("stroke"); ok {
		if p, ok := parsePaint(v, s.color); ok {
			s.stroke = p
		}
	}
	if v, ok := get("fill-opacity"); ok {
		s.fillOpacity = parseOpacity(v, s.fillOpacity)
	}
	if v, ok := get("stroke-opacity"); ok {
		s.strokeOpacity = parseOpacity(v, s.strokeOpacity)
	}
	if v, ok := get("fill-rule"); ok {
		s.fillRule = rule(v)
	}
	if v, ok := get("clip-rule"); ok {
		s.clipRule = rule(v)
	}
	if v, ok := get("stroke-width"); ok {
		if l, ok := parseLength(v); ok && l.V >= 0 {
			s.strokeWidth = l
		}
	}
	if v, ok := get("stroke-linecap"); ok {
		switch v {
		case "round":
			s.lineCap = gfx.RoundCap
		case "square":
			s.lineCap = gfx.SquareCap
		case "butt":
			s.lineCap = gfx.ButtCap
		}
	}
	if v, ok := get("stroke-linejoin"); ok {
		switch v {
		case "round":
			s.lineJoin = gfx.RoundJoin
		case "bevel":
			s.lineJoin = gfx.BevelJoin
		case "miter", "miter-clip", "arcs":
			s.lineJoin = gfx.MiterJoin
		}
	}
	if v, ok := get("stroke-miterlimit"); ok {
		if f := parseFloat(v, 4); f >= 1 {
			s.miterLimit = f
		}
	}
	if v, ok := get("stroke-dasharray"); ok {
		if v == "none" {
			s.dashArray = nil
		} else {
			list := parseLengthList(v)
			valid := len(list) > 0
			for _, l := range list {
				if l.V < 0 {
					valid = false
				}
			}
			if valid {
				s.dashArray = list
			} else {
				s.dashArray = nil
			}
		}
	}
	if v, ok := get("stroke-dashoffset"); ok {
		if l, ok := parseLength(v); ok {
			s.dashOffset = l
		}
	}
	if v, ok := get("visibility"); ok {
		s.visible = v == "visible"
	}
	if v, ok := get("display"); ok {
		s.display = v != "none"
	}
	if v, ok := get("opacity"); ok {
		s.opacity = parseOpacity(v, 1)
	}
	if v, ok := get("clip-path"); ok && v != "none" {
		s.clipPath = v
	}
	if v, ok := get("mask"); ok && v != "none" {
		s.mask = v
	}
	if v, ok := get("filter"); ok && v != "none" {
		s.filter = v
	}
	if v, ok := get("stop-color"); ok {
		if v == "currentcolor" || v == "currentColor" {
			s.stopColor = s.color
		} else if c, ok := parseColor(v); ok {
			s.stopColor = c
		}
	}
	if v, ok := get("stop-opacity"); ok {
		s.stopOpacity = parseOpacity(v, 1)
	}
	if v, ok := get("flood-color"); ok {
		if strings.EqualFold(v, "currentcolor") {
			s.floodColor = s.color
		} else if c, ok := parseColor(v); ok {
			s.floodColor = c
		}
	}
	if v, ok := get("flood-opacity"); ok {
		s.floodOpacity = parseOpacity(v, 1)
	}
	if v, ok := get("lighting-color"); ok {
		if c, ok := parseColor(v); ok {
			s.lightingColor = c
		}
	}
	if v, ok := get("vector-effect"); ok {
		s.nonScalingStroke = v == "non-scaling-stroke"
	}
	if v, ok := get("overflow"); ok {
		s.overflowVisible = v == "visible" || v == "auto"
	}
	if v, ok := get("mix-blend-mode"); ok {
		s.blend = gfx.ParseBlendMode(v)
	}
	if v, ok := get("isolation"); ok {
		s.isolate = v == "isolate"
	}
	if v, ok := get("mask-type"); ok && v == "alpha" {
		s.maskType = gfx.MaskAlpha
	}
	if v, ok := get("font-family"); ok {
		s.fontFamily = parseFamilies(v)
	}
	if v, ok := get("font-weight"); ok {
		s.fontWeight = parseWeight(v, parent.fontWeight)
	}
	if v, ok := get("font-style"); ok {
		s.italic = v == "italic" || strings.HasPrefix(v, "oblique")
	}
	if v, ok := get("text-anchor"); ok {
		s.textAnchor = v
	}
	if v, ok := get("letter-spacing"); ok {
		if v == "normal" {
			s.letterSpacing = 0
		} else if l, ok := parseLength(v); ok {
			s.letterSpacing = flc.resolve(l, axisX)
		}
	}
	if v, ok := get("word-spacing"); ok {
		if v == "normal" {
			s.wordSpacing = 0
		} else if l, ok := parseLength(v); ok {
			s.wordSpacing = flc.resolve(l, axisX)
		}
	}
	marker := func(v string) string {
		if id, _, ok := urlRef(v); ok {
			return id
		}
		return ""
	}
	if v, ok := get("marker"); ok {
		s.markerStart, s.markerMid, s.markerEnd = marker(v), marker(v), marker(v)
	}
	if v, ok := get("marker-start"); ok {
		s.markerStart = marker(v)
	}
	if v, ok := get("marker-mid"); ok {
		s.markerMid = marker(v)
	}
	if v, ok := get("marker-end"); ok {
		s.markerEnd = marker(v)
	}
	if v, ok := get("paint-order"); ok {
		s.paintOrder = parsePaintOrder(v)
	}
	if v, ok := n.Attrs["xml:space"]; ok {
		s.preserveSpace = v == "preserve"
	}
	if v, ok := get("white-space"); ok {
		s.preserveSpace = v == "pre" || v == "pre-wrap" || v == "break-spaces"
	}
	if v, ok := get("color-interpolation-filters"); ok && v != "auto" {
		s.colorInterp = v
	}
	if v, ok := get("direction"); ok {
		s.direction = v
	}
	if v, ok := get("writing-mode"); ok {
		s.writingMode = v
	}
	if v, ok := n.Attrs["xml:lang"]; ok && v != "" {
		s.lang = v
	} else if v, ok := n.Attrs["lang"]; ok && v != "" {
		s.lang = v
	}
	if v, ok := get("dominant-baseline"); ok {
		s.baseline = v
	}
	if v, ok := get("alignment-baseline"); ok {
		s.baseline = v
	}
	if v, ok := get("baseline-shift"); ok {
		s.baselineShift = v
	}
	if v, ok := get("text-decoration"); ok {
		s.textDecoration = strings.Fields(v)
	}
	if v, ok := get("text-decoration-line"); ok {
		s.textDecoration = strings.Fields(v)
	}
	if len(s.textDecoration) > 0 {
		s.textDecorationInherited = s.textDecoration
	}
	if n.styles != nil {
		if v, ok := n.styles["transform"]; ok {
			s.transformCSS = v
		}
	}
	return &s
}

func rule(v string) gfx.FillRule {
	if strings.TrimSpace(v) == "evenodd" {
		return gfx.EvenOdd
	}
	return gfx.NonZero
}

func parsePaint(v string, current gfx.Color) (paintSpec, bool) {
	v = strings.TrimSpace(v)
	lv := strings.ToLower(v)
	switch lv {
	case "none":
		return paintSpec{kind: paintNone}, true
	case "currentcolor":
		// Resolved against each element's own color when used.
		return paintSpec{kind: paintCurrentColor}, true
	case "context-fill":
		return paintSpec{kind: paintContextFill}, true
	case "context-stroke":
		return paintSpec{kind: paintContextStroke}, true
	}
	if strings.HasPrefix(lv, "url(") {
		id, rest, _ := urlRef(v)
		p := paintSpec{kind: paintURL, url: id}
		if rest != "" {
			if fb, ok := parsePaint(rest, current); ok {
				p.fallback = &fb
			}
		}
		return p, true
	}
	// "#f00 icc-color(...)" keeps the sRGB colour.
	if f := strings.Fields(v); len(f) > 1 && !strings.Contains(v, "(") {
		v = f[0]
	}
	if c, ok := parseColor(v); ok {
		return paintSpec{kind: paintColor, color: c}, true
	}
	return paintSpec{}, false
}

func parsePaintOrder(v string) []string {
	if v == "normal" {
		return nil
	}
	order := []string{}
	seen := map[string]bool{}
	for _, f := range strings.Fields(v) {
		if (f == "fill" || f == "stroke" || f == "markers") && !seen[f] {
			order = append(order, f)
			seen[f] = true
		}
	}
	for _, f := range []string{"fill", "stroke", "markers"} {
		if !seen[f] {
			order = append(order, f)
		}
	}
	return order
}

func parseFamilies(v string) []string {
	var out []string
	for _, f := range splitOutsideParens(v, ',') {
		f = strings.Trim(strings.TrimSpace(f), `"'`)
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

func parseWeight(v string, parent float64) float64 {
	switch strings.ToLower(v) {
	case "normal":
		return 400
	case "bold":
		return 700
	case "bolder":
		if parent < 400 {
			return 400
		}
		if parent < 600 {
			return 700
		}
		return 900
	case "lighter":
		if parent > 700 {
			return 700
		}
		if parent > 500 {
			return 400
		}
		return 100
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 1 && f <= 1000 {
		return f
	}
	return parent
}

var fontSizeKeywords = map[string]float64{
	"xx-small": 9, "x-small": 10, "small": 13, "medium": 16, "large": 18, "x-large": 24, "xx-large": 32, "xxx-large": 48,
}

func parseFontSize(v string, parent float64, lc lengthContext) float64 {
	lv := strings.ToLower(v)
	if k, ok := fontSizeKeywords[lv]; ok {
		return k
	}
	switch lv {
	case "larger":
		return parent * 1.2
	case "smaller":
		return parent / 1.2
	}
	l, ok := parseLength(v)
	if !ok || l.V < 0 {
		return parent
	}
	switch l.Unit {
	case "%":
		return parent * l.V / 100
	case "em":
		return parent * l.V
	case "ex":
		return parent * l.V / 2
	}
	return lc.resolve(l, axisOther)
}

// parseFontShorthand reads "italic bold 12px/1.2 Arial, sans-serif".
func parseFontShorthand(v string, s *style, parentSize float64, lc lengthContext) {
	fields := strings.Fields(v)
	for i, f := range fields {
		lf := strings.ToLower(f)
		switch {
		case lf == "italic" || lf == "oblique":
			s.italic = true
		case lf == "bold" || lf == "bolder" || lf == "lighter" || (len(lf) == 3 && lf[1] == '0' && lf[2] == '0'):
			s.fontWeight = parseWeight(lf, s.fontWeight)
		case lf == "normal" || lf == "small-caps":
		default:
			size := f
			if slash := strings.IndexByte(size, '/'); slash >= 0 {
				size = size[:slash]
			}
			if _, ok := parseLength(size); ok || fontSizeKeywords[strings.ToLower(size)] > 0 {
				s.fontSize = parseFontSize(size, parentSize, lc)
				if rest := strings.Join(fields[i+1:], " "); rest != "" {
					s.fontFamily = parseFamilies(rest)
				}
				return
			}
		}
	}
}

// parseOpacity reads a number or a percentage; anything else keeps def.
func parseOpacity(v string, def float64) float64 {
	l, ok := parseLength(v)
	if !ok {
		return def
	}
	switch l.Unit {
	case "":
		return clamp01(l.V)
	case "%":
		return clamp01(l.V / 100)
	}
	return def
}
