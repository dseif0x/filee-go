package subtitles

import (
	"encoding/xml"
	"errors"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// TTML / DFXP (W3C Timed Text): XML with <p begin=… end=…> paragraphs.
// Elements are matched by local name, so both the TTML 1 namespace and the
// older DFXP one (2006/10/ttaf1) read. Times are relative to the parent's
// begin; clock times, frames (ttp:frameRate) and offsets in h / m / s / ms / f
// / ticks are understood.

type xnode struct {
	name     string // local name; "" for text
	attrs    []xml.Attr
	children []*xnode
	text     string
}

func (n *xnode) elements() []*xnode {
	var out []*xnode
	for _, c := range n.children {
		if c.name != "" {
			out = append(out, c)
		}
	}
	return out
}

func (n *xnode) descendants(visit func(*xnode)) {
	for _, c := range n.children {
		if c.name != "" {
			visit(c)
			c.descendants(visit)
		}
	}
}

func (n *xnode) value() string {
	if n.name == "" {
		return n.text
	}
	var b strings.Builder
	for _, c := range n.children {
		b.WriteString(c.value())
	}
	return b.String()
}

// attr is an attribute by local name in any namespace (tts:color, xml:lang,
// ttp:frameRate, plain begin), trimmed; ok is false when absent.
func (n *xnode) attr(local string) (string, bool) {
	for _, a := range n.attrs {
		if a.Name.Local == local {
			return strings.TrimSpace(a.Value), true
		}
	}
	return "", false
}

func parseXMLTree(text string) (*xnode, error) {
	d := xml.NewDecoder(strings.NewReader(text))
	d.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	var root *xnode
	var stack []*xnode
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &xnode{name: t.Name.Local, attrs: t.Attr}
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.children = append(p.children, n)
			} else if root == nil {
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.children = append(p.children, &xnode{text: string(t)})
			}
		}
	}
	return root, nil
}

type ttmlContext struct {
	frameRate, tickRate float64
	styles              map[string]*xnode
}

func readTtml(text string) (*Document, error) {
	root, err := parseXMLTree(text)
	if err != nil {
		return nil, errors.New("The TTML file is not valid XML: " + err.Error())
	}
	if root == nil {
		return nil, errors.New("The TTML file is empty.")
	}
	d := &Document{}
	d.Language, _ = root.attr("lang")
	fr, _ := root.attr("frameRate")
	frm, _ := root.attr("frameRateMultiplier")
	tr, _ := root.attr("tickRate")
	ctx := &ttmlContext{frameRate: positive(fr, 30) * ratio(frm), tickRate: positive(tr, 0), styles: map[string]*xnode{}}
	root.descendants(func(e *xnode) {
		if id, ok := e.attr("id"); ok && e.name == "style" {
			if _, seen := ctx.styles[id]; !seen {
				ctx.styles[id] = e
			}
		}
	})
	if ctx.tickRate <= 0 {
		ctx.tickRate = 1
		if ctx.frameRate > 0 {
			ctx.tickRate = ctx.frameRate
		}
	}
	titled := false
	root.descendants(func(e *xnode) {
		if e.name == "title" && !titled {
			titled = true
			if t := strings.TrimSpace(e.value()); t != "" {
				d.Title = t
			}
		}
	})
	for _, e := range root.elements() {
		if e.name == "body" {
			ttmlVisit(e, 0, nil, d, ctx)
			break
		}
	}
	return d, nil
}

// ttmlVisit walks body / div / p, adding each parent's begin to its children's times.
func ttmlVisit(e *xnode, parentBegin time.Duration, parentEnd *time.Duration, d *Document, ctx *ttmlContext) {
	begin := parentBegin
	if b, ok := ttmlTimeAttr(e, "begin", ctx); ok {
		begin += b
	}
	end := parentEnd
	if v, ok := ttmlTimeAttr(e, "end", ctx); ok {
		x := parentBegin + v
		end = &x
	} else if v, ok := ttmlTimeAttr(e, "dur", ctx); ok {
		x := begin + v
		end = &x
	}
	if e.name == "p" {
		var b strings.Builder
		ttmlContent(e, &b, ctx)
		stop := begin + DefaultDuration
		if end != nil {
			stop = *end
		}
		d.Add(begin, stop, b.String())
		return
	}
	for _, c := range e.elements() {
		if c.name == "div" || c.name == "p" {
			ttmlVisit(c, begin, end, d, ctx)
		}
	}
}

func ttmlTimeAttr(e *xnode, name string, ctx *ttmlContext) (time.Duration, bool) {
	v, ok := e.attr(name)
	if !ok {
		return 0, false
	}
	return ttmlTime(v, ctx.frameRate, ctx.tickRate)
}

// ttmlContent appends the text of a p or span with its styles as markup; br breaks the line.
func ttmlContent(e *xnode, b *strings.Builder, ctx *ttmlContext) {
	styles := ttmlStyles(e, ctx)
	b.WriteString(openStyles(styles))
	for _, c := range e.children {
		switch c.name {
		case "":
			b.WriteString(reWhitespace.ReplaceAllString(c.text, " "))
		case "br":
			b.WriteByte('\n')
		case "span":
			ttmlContent(c, b, ctx)
		}
	}
	b.WriteString(closeStyles(styles))
}

// ttmlStyles is italic, bold, underline and colour of an element: its own tts:
// attributes and the styles it references.
func ttmlStyles(e *xnode, ctx *ttmlContext) []string {
	var sources []*xnode
	ids, _ := e.attr("style")
	for _, id := range strings.Fields(ids) {
		if s, ok := ctx.styles[id]; ok {
			sources = append(sources, s)
		}
	}
	sources = append(sources, e)
	var result []string
	for _, s := range sources {
		if v, _ := s.attr("fontStyle"); (v == "italic" || v == "oblique") && !containsString(result, "i") {
			result = append(result, "i")
		}
		if v, _ := s.attr("fontWeight"); v == "bold" && !containsString(result, "b") {
			result = append(result, "b")
		}
		if v, ok := s.attr("textDecoration"); ok && strings.Contains(v, "underline") && !containsString(result, "u") {
			result = append(result, "u")
		}
		if v, ok := s.attr("color"); ok {
			if c := ParseColor(v); c != "" && c != "#FFFFFF" {
				kept := result[:0]
				for _, r := range result {
					if !strings.HasPrefix(r, "#") {
						kept = append(kept, r)
					}
				}
				result = append(kept, c)
			}
		}
	}
	return result
}

var (
	reTtmlClock  = regexp.MustCompile(`^(?P<h>\d+):(?P<m>\d{2}):(?P<s>\d{2}(?:\.\d+)?)(?::(?P<frames>\d+(?:\.\d+)?))?$`)
	reTtmlOffset = regexp.MustCompile(`^(?P<n>\d+(?:\.\d+)?)(?P<unit>h|ms|m|s|f|t)$`)
)

// ttmlTime reads a TTML time expression; false when absent or unreadable.
func ttmlTime(v string, frameRate, tickRate float64) (time.Duration, bool) {
	if strings.TrimSpace(v) == "" {
		return 0, false
	}
	if m := reTtmlClock.FindStringSubmatch(v); m != nil {
		h, _ := strconv.Atoi(m[1])
		mi, _ := strconv.Atoi(m[2])
		s, _ := strconv.ParseFloat(m[3], 64)
		seconds := float64(h)*3600 + float64(mi)*60 + s
		if m[4] != "" && frameRate > 0 {
			f, _ := strconv.ParseFloat(m[4], 64)
			seconds += f / frameRate
		}
		return fromSeconds(seconds), true
	}
	if m := reTtmlOffset.FindStringSubmatch(v); m != nil {
		n, _ := strconv.ParseFloat(m[1], 64)
		var seconds float64
		switch m[2] {
		case "h":
			seconds = n * 3600
		case "m":
			seconds = n * 60
		case "ms":
			seconds = n / 1000
		case "f":
			if frameRate > 0 {
				seconds = n / frameRate
			}
		case "t":
			if tickRate > 0 {
				seconds = n / tickRate
			}
		default:
			seconds = n
		}
		return fromSeconds(seconds), true
	}
	return 0, false
}

func positive(s string, fallback float64) float64 {
	if v, err := strconv.ParseFloat(s, 64); err == nil && v > 0 && !math.IsInf(v, 0) {
		return v
	}
	return fallback
}

// ratio reads ttp:frameRateMultiplier="1000 1001" as 0.999 (29.97 fps written as 30 × 1000/1001).
func ratio(s string) float64 {
	parts := strings.Fields(s)
	if len(parts) == 2 {
		n, err1 := strconv.ParseFloat(parts[0], 64)
		d, err2 := strconv.ParseFloat(parts[1], 64)
		if err1 == nil && err2 == nil && n > 0 && d > 0 {
			return n / d
		}
	}
	return 1
}
