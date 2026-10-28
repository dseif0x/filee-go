package ebook

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/dseif0x/filee-go/internal/xdom"
)

// OPF package documents (EPUB 2 and 3, also the metadata.opf of HTMLZ and
// TXTZ): metadata, manifest and spine. Element names are matched by local
// name so files from sloppy generators (missing or odd prefixes) still read.

type opfItem struct {
	ID, Path, MediaType, Properties string
}

type opfPackage struct {
	meta     Metadata
	manifest map[string]opfItem
	spine    []opfItem // linear and non-linear items, as calibre does
}

func loadXMLFile(path string) (*xdom.Node, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	root, _, err := parseXML(b)
	return root, err
}

func unescapePath(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		return u
	}
	return s
}

// loadOpf reads an OPF file whose manifest paths are relative to its folder.
func loadOpf(opfPath string) (*opfPackage, error) {
	root, err := loadXMLFile(opfPath)
	if err != nil {
		return nil, err
	}
	abs, _ := filepath.Abs(opfPath)
	folder := filepath.Dir(abs)
	p := &opfPackage{manifest: map[string]opfItem{}}
	for _, m := range localChildren(root, "manifest") {
		for _, item := range localChildren(m, "item") {
			id, href := item.A("id"), item.A("href")
			if id == "" || href == "" {
				continue
			}
			path := safePath(folder, unescapePath(strings.Split(href, "#")[0]))
			if path == "" {
				continue
			}
			if _, dup := p.manifest[id]; !dup {
				p.manifest[id] = opfItem{id, path, strings.ToLower(strings.TrimSpace(item.A("media-type"))), item.A("properties")}
			}
		}
	}
	for _, s := range localChildren(root, "spine") {
		for _, ref := range localChildren(s, "itemref") {
			if item, ok := p.manifest[ref.A("idref")]; ok {
				p.spine = append(p.spine, item)
			}
		}
	}
	if md := localChild(root, "metadata"); md != nil {
		p.readMetadata(md)
	}
	return p, nil
}

func (p *opfPackage) readMetadata(md *xdom.Node) {
	// EPUB 2 puts dc: elements in <dc-metadata>: search descendants.
	first := func(name string) string {
		for _, e := range md.DescendantsLocal(name) {
			if v := strings.TrimSpace(e.Value()); v != "" {
				return v
			}
		}
		return ""
	}
	m := &p.meta
	m.Title = first("title")
	m.Language = first("language")
	m.Description = first("description")
	m.Publisher = first("publisher")
	for _, c := range md.DescendantsLocal("creator") {
		name := strings.TrimSpace(c.Value())
		role, hasRole := c.AttrLocal("role")
		if name != "" && (!hasRole || role == "aut") && !contains(m.Authors, name) {
			m.Authors = append(m.Authors, name)
		}
	}
	// Cover: EPUB 3 "cover-image" property, else EPUB 2 <meta name="cover" content="item id">.
	var cover *opfItem
	ids := make([]string, 0, len(p.manifest))
	for id := range p.manifest {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		it := p.manifest[id]
		if contains(strings.Fields(it.Properties), "cover-image") {
			cover = &it
			break
		}
	}
	if cover == nil {
		for _, meta := range md.DescendantsLocal("meta") {
			if meta.A("name") == "cover" {
				if it, ok := p.manifest[meta.A("content")]; ok {
					cover = &it
				}
				break
			}
		}
	}
	if cover != nil && strings.HasPrefix(cover.MediaType, "image/") && isFile(cover.Path) {
		m.CoverImage = cover.Path
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// opfRootFile is the first rootfile META-INF/container.xml names; with a
// broken container, the first package document of the book.
func opfRootFile(bookFolder string) (string, error) {
	container := filepath.Join(bookFolder, "META-INF", "container.xml")
	if root, err := loadXMLFile(container); err == nil {
		for _, rf := range root.DescendantsLocal("rootfile") {
			mt := rf.A("media-type")
			if mt != "" && mt != "application/oebps-package+xml" {
				continue
			}
			if full := rf.A("full-path"); full != "" {
				if p := safePath(bookFolder, unescapePath(full)); p != "" && isFile(p) {
					return p, nil
				}
			}
			break
		}
	}
	var found string
	filepath.Walk(bookFolder, func(p string, info os.FileInfo, err error) error {
		if err == nil && found == "" && !info.IsDir() && strings.EqualFold(filepath.Ext(p), ".opf") {
			found = p
		}
		return nil
	})
	if found == "" {
		return "", errors.New("this is not a valid EPUB file: it has no package document (content.opf)")
	}
	return found, nil
}

// metadataOnlyOpf is a minimal OPF with Dublin Core metadata (HTMLZ and TXTZ keep their metadata this way).
func metadataOnlyOpf(m *Metadata, title, language string) string {
	var sb strings.Builder
	sb.WriteString("<?xml version='1.0' encoding='utf-8'?>\n")
	sb.WriteString(`<package xmlns="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="uuid_id">`)
	sb.WriteString(`<metadata xmlns:dc="http://purl.org/dc/elements/1.1/">`)
	fmt.Fprintf(&sb, "<dc:title>%s</dc:title><dc:language>%s</dc:language><dc:identifier id=\"uuid_id\">urn:uuid:%s</dc:identifier>", esc(title), esc(language), uuid.NewString())
	for _, a := range m.Authors {
		fmt.Fprintf(&sb, `<dc:creator xmlns:opf="http://www.idpf.org/2007/opf" opf:role="aut">%s</dc:creator>`, esc(a))
	}
	if m.Publisher != "" {
		fmt.Fprintf(&sb, "<dc:publisher>%s</dc:publisher>", esc(m.Publisher))
	}
	if m.Description != "" {
		fmt.Fprintf(&sb, "<dc:description>%s</dc:description>", esc(m.Description))
	}
	sb.WriteString("</metadata></package>")
	return sb.String()
}
