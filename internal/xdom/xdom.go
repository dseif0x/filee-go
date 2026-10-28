// Package xdom is a small XML tree (like .NET's XDocument) for the readers of
// XML-based formats: elements with namespace-qualified names and attributes,
// text kept in document order.
package xdom

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/text/encoding/htmlindex"
)

// Node is an element, or a text node (Name.Local == "" and Text set).
type Node struct {
	Name     xml.Name
	Attrs    []xml.Attr
	Children []*Node
	Parent   *Node
	// Text of a text node (character data, CDATA).
	Text string
	// NS holds the namespace declarations of the element as read
	// (xmlns="…" and xmlns:p="…"), so a rewritten part keeps its prefixes
	// (mc:Ignorable names them).
	NS []xml.Attr
}

// IsText reports whether the node is character data.
func (n *Node) IsText() bool { return n != nil && n.Name.Local == "" }

// ErrTooLarge is returned when a document exceeds the size limit.
var ErrTooLarge = errors.New("xml document too large")

// Parse reads a document; whitespace-only text is kept (it can be content).
// limit caps the bytes read (0 = no limit). A document type declaration is
// refused, like XmlReader's DtdProcessing.Prohibit.
func Parse(r io.Reader, limit int64) (*Node, error) {
	if limit > 0 {
		r = &limitReader{r: r, n: limit}
	}
	d := xml.NewDecoder(r)
	d.Strict = true
	d.CharsetReader = func(label string, input io.Reader) (io.Reader, error) {
		e, err := htmlindex.Get(label)
		if err != nil {
			return nil, err
		}
		return e.NewDecoder().Reader(input), nil
	}
	root := &Node{}
	cur := root
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
			n := &Node{Name: t.Name, Parent: cur}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || a.Name.Space == "" && a.Name.Local == "xmlns" {
					n.NS = append(n.NS, a)
					continue
				}
				n.Attrs = append(n.Attrs, a)
			}
			cur.Children = append(cur.Children, n)
			cur = n
		case xml.EndElement:
			if cur.Parent != nil {
				cur = cur.Parent
			}
		case xml.CharData:
			if cur == root {
				continue
			}
			if k := len(cur.Children); k > 0 && cur.Children[k-1].IsText() {
				cur.Children[k-1].Text += string(t)
			} else {
				cur.Children = append(cur.Children, &Node{Parent: cur, Text: string(t)})
			}
		case xml.Directive:
			if bytes.HasPrefix(bytes.TrimSpace(t), []byte("DOCTYPE")) {
				return nil, fmt.Errorf("xml: document type declarations are not allowed")
			}
		}
	}
	for _, c := range root.Children {
		if !c.IsText() {
			c.Parent = nil
			return c, nil
		}
	}
	return nil, errors.New("xml: no root element")
}

// ParseBytes parses b.
func ParseBytes(b []byte) (*Node, error) { return Parse(bytes.NewReader(b), 0) }

type limitReader struct {
	r io.Reader
	n int64
}

func (l *limitReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, ErrTooLarge
	}
	if int64(len(p)) > l.n {
		p = p[:l.n]
	}
	n, err := l.r.Read(p)
	l.n -= int64(n)
	return n, err
}

// Is reports whether the element has namespace space and local name.
func (n *Node) Is(space, local string) bool {
	return n != nil && n.Name.Local == local && n.Name.Space == space
}

// Elements returns the element children; with names, only those with that
// local name (any namespace) when space is "*", or in space.
func (n *Node) Elements() []*Node {
	if n == nil {
		return nil
	}
	out := make([]*Node, 0, len(n.Children))
	for _, c := range n.Children {
		if !c.IsText() {
			out = append(out, c)
		}
	}
	return out
}

// All returns the element children named space:local.
func (n *Node) All(space, local string) []*Node {
	if n == nil {
		return nil
	}
	var out []*Node
	for _, c := range n.Children {
		if c.Is(space, local) {
			out = append(out, c)
		}
	}
	return out
}

// AllLocal returns the element children with a local name, any namespace.
func (n *Node) AllLocal(local string) []*Node {
	if n == nil {
		return nil
	}
	var out []*Node
	for _, c := range n.Children {
		if !c.IsText() && c.Name.Local == local {
			out = append(out, c)
		}
	}
	return out
}

// Child returns the first element child named space:local, or nil.
func (n *Node) Child(space, local string) *Node {
	if n == nil {
		return nil
	}
	for _, c := range n.Children {
		if c.Is(space, local) {
			return c
		}
	}
	return nil
}

// ChildLocal returns the first element child with a local name, any namespace.
func (n *Node) ChildLocal(local string) *Node {
	if n == nil {
		return nil
	}
	for _, c := range n.Children {
		if !c.IsText() && c.Name.Local == local {
			return c
		}
	}
	return nil
}

// Descendants returns every element below n named space:local (document order).
func (n *Node) Descendants(space, local string) []*Node {
	var out []*Node
	n.Walk(func(d *Node) bool {
		if d != n && d.Is(space, local) {
			out = append(out, d)
		}
		return true
	})
	return out
}

// DescendantsLocal is Descendants by local name in any namespace.
func (n *Node) DescendantsLocal(local string) []*Node {
	var out []*Node
	n.Walk(func(d *Node) bool {
		if d != n && !d.IsText() && d.Name.Local == local {
			out = append(out, d)
		}
		return true
	})
	return out
}

// Walk visits n and its elements depth first; returning false skips children.
func (n *Node) Walk(visit func(*Node) bool) {
	if n == nil || n.IsText() {
		return
	}
	if !visit(n) {
		return
	}
	for _, c := range n.Children {
		c.Walk(visit)
	}
}

// Attr returns the attribute space:local ("" space for unqualified ones).
func (n *Node) Attr(space, local string) (string, bool) {
	if n == nil {
		return "", false
	}
	for _, a := range n.Attrs {
		if a.Name.Local == local && a.Name.Space == space {
			return a.Value, true
		}
	}
	return "", false
}

// A returns the unqualified attribute local, or "".
func (n *Node) A(local string) string {
	v, _ := n.Attr("", local)
	return v
}

// AN returns the attribute space:local, or "".
func (n *Node) AN(space, local string) string {
	v, _ := n.Attr(space, local)
	return v
}

// AttrLocal returns the first attribute with a local name, any namespace.
func (n *Node) AttrLocal(local string) (string, bool) {
	if n == nil {
		return "", false
	}
	for _, a := range n.Attrs {
		if a.Name.Local == local {
			return a.Value, true
		}
	}
	return "", false
}

// SetAttr sets (or adds) an attribute.
func (n *Node) SetAttr(space, local, value string) {
	for i, a := range n.Attrs {
		if a.Name.Local == local && a.Name.Space == space {
			n.Attrs[i].Value = value
			return
		}
	}
	n.Attrs = append(n.Attrs, xml.Attr{Name: xml.Name{Space: space, Local: local}, Value: value})
}

// RemoveAttr removes an attribute.
func (n *Node) RemoveAttr(space, local string) {
	for i, a := range n.Attrs {
		if a.Name.Local == local && a.Name.Space == space {
			n.Attrs = append(n.Attrs[:i], n.Attrs[i+1:]...)
			return
		}
	}
}

// Value is the concatenated text of n and its descendants.
func (n *Node) Value() string {
	if n == nil {
		return ""
	}
	if n.IsText() {
		return n.Text
	}
	var sb strings.Builder
	n.value(&sb)
	return sb.String()
}

func (n *Node) value(sb *strings.Builder) {
	for _, c := range n.Children {
		if c.IsText() {
			sb.WriteString(c.Text)
		} else {
			c.value(sb)
		}
	}
}

// Remove detaches n from its parent.
func (n *Node) Remove() {
	if n == nil || n.Parent == nil {
		return
	}
	p := n.Parent
	for i, c := range p.Children {
		if c == n {
			p.Children = append(p.Children[:i], p.Children[i+1:]...)
			break
		}
	}
	n.Parent = nil
}

// Add appends child to n.
func (n *Node) Add(child *Node) {
	child.Parent = n
	n.Children = append(n.Children, child)
}

// NewElement returns an element.
func NewElement(space, local string) *Node { return &Node{Name: xml.Name{Space: space, Local: local}} }
