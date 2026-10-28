package ebook

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/htmlindex"

	"github.com/dseif0x/filee-go/internal/xdom"
)

// parseXML reads a document into an xdom tree without resolving external
// DTDs (no network, no XXE). The document type declaration is skipped and
// HTML's named entities (&nbsp; …), which some generators use, are known.
// prolog is the source text in front of the root element.
func parseXML(b []byte) (root *xdom.Node, prolog string, err error) {
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	if bytes.HasPrefix(b, []byte{0xFF, 0xFE}) || bytes.HasPrefix(b, []byte{0xFE, 0xFF}) {
		if e, err := htmlindex.Get("utf-16"); err == nil {
			if s, err := e.NewDecoder().Bytes(b); err == nil {
				b = s
				// The declaration still says UTF-16; the text is UTF-8 now.
				b = bytes.Replace(b, []byte(`encoding="UTF-16"`), []byte(`encoding="UTF-8"`), 1)
				b = bytes.Replace(b, []byte(`encoding="utf-16"`), []byte(`encoding="UTF-8"`), 1)
			}
		}
	}
	d := xml.NewDecoder(bytes.NewReader(b))
	d.Strict = true
	d.Entity = xml.HTMLEntity
	d.CharsetReader = func(label string, input io.Reader) (io.Reader, error) {
		if strings.EqualFold(label, "utf-8") || strings.EqualFold(label, "utf8") {
			return input, nil
		}
		e, err := htmlindex.Get(label)
		if err != nil {
			return nil, err
		}
		return e.NewDecoder().Reader(input), nil
	}
	top := &xdom.Node{}
	cur := top
	for {
		offset := d.InputOffset()
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if cur == top && prolog == "" && int(offset) <= len(b) {
				prolog = string(b[:offset])
			}
			n := &xdom.Node{Name: t.Name, Parent: cur}
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
			if cur == top {
				continue
			}
			if k := len(cur.Children); k > 0 && cur.Children[k-1].IsText() {
				cur.Children[k-1].Text += string(t)
			} else {
				cur.Children = append(cur.Children, &xdom.Node{Parent: cur, Text: string(t)})
			}
		}
	}
	for _, c := range top.Children {
		if !c.IsText() {
			c.Parent = nil
			if !utf8.ValidString(prolog) {
				prolog = ""
			}
			return c, prolog, nil
		}
	}
	return nil, "", errors.New("xml: no root element")
}

// writeXML serializes root after prolog (the original declaration and
// DOCTYPE), or with a UTF-8 declaration when prolog is empty.
func writeXML(root *xdom.Node, prolog string) []byte {
	var buf bytes.Buffer
	xdom.Write(&buf, root, nil)
	out := buf.Bytes()
	if i := bytes.Index(out, []byte("?>")); i >= 0 && bytes.HasPrefix(out, []byte("<?xml")) {
		out = bytes.TrimLeft(out[i+2:], "\r\n")
	}
	if strings.TrimSpace(prolog) == "" {
		prolog = "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n"
	} else {
		// The text is written as UTF-8 whatever the source was.
		if i := strings.Index(prolog, "?>"); strings.HasPrefix(strings.TrimSpace(prolog), "<?xml") && i >= 0 {
			prolog = "<?xml version=\"1.0\" encoding=\"utf-8\"?>" + prolog[i+2:]
		}
	}
	return append([]byte(prolog), out...)
}

// localChildren are the element children of n with a local name.
func localChildren(n *xdom.Node, local string) []*xdom.Node { return n.AllLocal(local) }

// localChild is the first element child of n with a local name, or nil.
func localChild(n *xdom.Node, local string) *xdom.Node { return n.ChildLocal(local) }

// attrLocal is the value of the first attribute named local in any namespace.
func attrLocal(n *xdom.Node, local string) string {
	v, _ := n.AttrLocal(local)
	return v
}
