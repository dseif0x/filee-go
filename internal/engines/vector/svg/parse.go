// Package svg reads SVG drawings and draws them onto a gfx.Device: shapes,
// paths, text (as glyph outlines), images, gradients, patterns, clipping,
// masks, markers, <use>, nested <svg>, CSS style sheets and the common filter
// effects.
package svg

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"golang.org/x/net/html/charset"
)

// Node is an element of the SVG tree, or a text node (Name == "").
type Node struct {
	Name     string
	Attrs    map[string]string
	Children []*Node
	Parent   *Node
	Text     string
	// styles holds the declarations from style sheets and the style attribute
	// (after the cascade), consulted before the presentation attributes.
	styles map[string]string
	index  int // position among the element siblings
}

// Attr returns an attribute (namespace prefixes dropped, except xml:space).
func (n *Node) Attr(name string) string { return n.Attrs[name] }

// Has reports whether the attribute is set.
func (n *Node) Has(name string) bool { _, ok := n.Attrs[name]; return ok }

// Elements are the element children.
func (n *Node) Elements() []*Node {
	out := make([]*Node, 0, len(n.Children))
	for _, c := range n.Children {
		if c.Name != "" {
			out = append(out, c)
		}
	}
	return out
}

// ErrNotSVG is returned when the document has no <svg> root.
var ErrNotSVG = errors.New("the file is not an SVG drawing (no <svg> element)")

var entityDecl = regexp.MustCompile(`<!ENTITY\s+([A-Za-z_][A-Za-z0-9._-]*)\s+(?:"([^"]*)"|'([^']*)')`)

// parseXML builds the node tree. strict false forgives unknown entities and
// stray ampersands (the parser keeps them as text).
func parseXML(data []byte, strict bool) (*Node, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = strict
	d.CharsetReader = charset.NewReaderLabel
	entities := map[string]string{}
	for k, v := range xml.HTMLEntity {
		entities[k] = v
	}
	if strict {
		entities = map[string]string{}
	}
	d.Entity = entities
	var root *Node
	var stack []*Node
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			if root != nil && !strict {
				break // keep what was read
			}
			return nil, err
		}
		switch t := tok.(type) {
		case xml.Directive:
			// Entities declared in the DOCTYPE (Illustrator's &ns_svg;).
			for _, m := range entityDecl.FindAllStringSubmatch(string(t), -1) {
				v := m[2]
				if v == "" {
					v = m[3]
				}
				entities[m[1]] = v
			}
		case xml.StartElement:
			n := &Node{Name: t.Name.Local, Attrs: make(map[string]string, len(t.Attr))}
			for _, a := range t.Attr {
				key := a.Name.Local
				switch {
				case a.Name.Space == "xmlns" || key == "xmlns" && a.Name.Space == "":
					continue
				case a.Name.Space == "xml" || a.Name.Space == "http://www.w3.org/XML/1998/namespace":
					key = "xml:" + key
				case key == "href" && n.Attrs["href"] != "" && a.Name.Space != "":
					// SVG 2's plain href wins over xlink:href.
					continue
				}
				n.Attrs[key] = a.Value
			}
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				n.Parent = p
				p.Children = append(p.Children, n)
			} else if root == nil {
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.Children = append(p.Children, &Node{Text: string(t), Parent: p})
			}
		}
	}
	if root == nil {
		return nil, ErrNotSVG
	}
	return root, nil
}

// parseDocument reads the XML: strictly first, then with stray ampersands and
// HTML entities repaired, then forgivingly.
func parseDocument(data []byte) (*Node, error) {
	root, err := parseXML(data, true)
	if err == nil {
		return root, nil
	}
	if fixed := FixAmpersands(data); fixed != nil {
		if r, err2 := parseXML(fixed, true); err2 == nil {
			return r, nil
		}
	}
	if r, err2 := parseXML(data, false); err2 == nil && r.Name == "svg" {
		return r, nil
	}
	if err == ErrNotSVG {
		return nil, err
	}
	return nil, fmt.Errorf("the SVG file could not be read: %w", err)
}

// textContent concatenates the text below n.
func textContent(n *Node) string {
	var b strings.Builder
	var walk func(*Node)
	walk = func(n *Node) {
		if n.Name == "" {
			b.WriteString(n.Text)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}
