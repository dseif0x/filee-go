package xdom

import (
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"
)

const xmlURL = "http://www.w3.org/XML/1998/namespace"

// Write serializes a tree with an XML declaration. Namespace declarations
// read with the tree are written where they were; prefixes maps further
// namespaces (URI → prefix, "" = default) and any others get ns0, ns1, …,
// declared on the root.
func Write(w io.Writer, root *Node, prefixes map[string]string) error {
	p := map[string]string{}
	declared := map[string]bool{}
	root.Walk(func(n *Node) bool {
		for _, a := range n.NS {
			uri := a.Value
			prefix := a.Name.Local
			if a.Name.Space == "" {
				prefix = ""
			}
			if _, ok := p[uri]; !ok {
				p[uri] = prefix
				declared[uri] = true
			}
		}
		return true
	})
	used := map[string]bool{}
	root.Walk(func(n *Node) bool {
		used[n.Name.Space] = true
		for _, a := range n.Attrs {
			if a.Name.Space != "" {
				used[a.Name.Space] = true
			}
		}
		return true
	})
	var extra []string
	for s := range used {
		if s == "" || s == xmlURL || declared[s] {
			continue
		}
		extra = append(extra, s)
	}
	sort.Strings(extra)
	k := 0
	for _, s := range extra {
		if v, ok := prefixes[s]; ok {
			p[s] = v
			continue
		}
		p[s] = fmt.Sprintf("ns%d", k)
		k++
	}
	p[xmlURL] = "xml"
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\r\n")
	write(&sb, root, p, extra, true)
	_, err := io.WriteString(w, sb.String())
	return err
}

func qname(n xml.Name, p map[string]string, attr bool) string {
	if n.Space == "" {
		return n.Local
	}
	prefix, ok := p[n.Space]
	if !ok {
		prefix = n.Space // an undeclared prefix kept by the decoder
	}
	if prefix == "" {
		return n.Local
	}
	return prefix + ":" + n.Local
}

func write(sb *strings.Builder, n *Node, p map[string]string, extra []string, root bool) {
	if n.IsText() {
		sb.WriteString(textEscaper.Replace(n.Text))
		return
	}
	name := qname(n.Name, p, false)
	sb.WriteByte('<')
	sb.WriteString(name)
	for _, a := range n.NS {
		if a.Name.Space == "" {
			fmt.Fprintf(sb, ` xmlns="%s"`, attrEscape(a.Value))
		} else {
			fmt.Fprintf(sb, ` xmlns:%s="%s"`, a.Name.Local, attrEscape(a.Value))
		}
	}
	if root {
		for _, s := range extra {
			if p[s] == "" {
				fmt.Fprintf(sb, ` xmlns="%s"`, attrEscape(s))
			} else {
				fmt.Fprintf(sb, ` xmlns:%s="%s"`, p[s], attrEscape(s))
			}
		}
	}
	for _, a := range n.Attrs {
		fmt.Fprintf(sb, ` %s="%s"`, qname(a.Name, p, true), attrEscape(a.Value))
	}
	if len(n.Children) == 0 {
		sb.WriteString("/>")
		return
	}
	sb.WriteByte('>')
	for _, c := range n.Children {
		write(sb, c, p, extra, false)
	}
	sb.WriteString("</")
	sb.WriteString(name)
	sb.WriteByte('>')
}

func attrEscape(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			sb.WriteString("&amp;")
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		case '"':
			sb.WriteString("&quot;")
		case '\n':
			sb.WriteString("&#xA;")
		case '\r':
			sb.WriteString("&#xD;")
		case '\t':
			sb.WriteString("&#x9;")
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

var textEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\r", "&#xD;")
