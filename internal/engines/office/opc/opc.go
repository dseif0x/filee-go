// Package opc reads Office Open XML packages (.docx, .xlsx, .pptx): parts as
// XML trees, relationships resolved to zip paths, and Office colours.
package opc

import (
	"archive/zip"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/dseif0x/filee-go/internal/xdom"
)

// PackageRelationships is the namespace of .rels parts.
const PackageRelationships = "http://schemas.openxmlformats.org/package/2006/relationships"

// MaxPartBytes caps one XML part (against zip bombs).
const MaxPartBytes = 256 << 20

// Relationship of a part; Target is a zip path unless External.
type Relationship struct {
	ID, Type, Target string
	External         bool
}

// Package is an open OPC zip package; parts and relationships are cached.
type Package struct {
	f     *os.File
	Zip   *zip.Reader
	parts map[string]*xdom.Node
	rels  map[string][]Relationship
	names map[string]*zip.File
}

// Open opens a package.
func Open(p string) (*Package, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	z, err := zip.NewReader(f, st.Size())
	if err != nil {
		f.Close()
		return nil, err
	}
	pk := &Package{f: f, Zip: z, parts: map[string]*xdom.Node{}, rels: map[string][]Relationship{}, names: map[string]*zip.File{}}
	for _, e := range z.File {
		k := strings.ToLower(e.Name)
		if _, dup := pk.names[k]; !dup {
			pk.names[k] = e
		}
	}
	return pk, nil
}

func (p *Package) Close() error { return p.f.Close() }

// Entry returns the zip entry of a part (case-insensitive), or nil.
func (p *Package) Entry(name string) *zip.File { return p.names[strings.ToLower(name)] }

// Part returns a part as XML, or nil when it doesn't exist or isn't XML.
func (p *Package) Part(name string) *xdom.Node {
	key := strings.ToLower(name)
	if n, ok := p.parts[key]; ok {
		return n
	}
	var n *xdom.Node
	if e := p.Entry(name); e != nil {
		if rc, err := e.Open(); err == nil {
			n, _ = xdom.Parse(rc, MaxPartBytes)
			rc.Close()
		}
	}
	p.parts[key] = n
	return n
}

// Bytes returns the raw content of a part, or nil.
func (p *Package) Bytes(name string) []byte {
	e := p.Entry(name)
	if e == nil {
		return nil
	}
	rc, err := e.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	b, _ := io.ReadAll(io.LimitReader(rc, MaxPartBytes))
	return b
}

// MainPartPath is the target of the package's officeDocument relationship.
func (p *Package) MainPartPath() string {
	for _, r := range p.Relationships("") {
		if strings.HasSuffix(r.Type, "/officeDocument") {
			return r.Target
		}
	}
	return ""
}

// Relationships of a part ("" = the package).
func (p *Package) Relationships(part string) []Relationship {
	key := strings.ToLower(part)
	if r, ok := p.rels[key]; ok {
		return r
	}
	folder := Folder(part)
	relsPath := "_rels/.rels"
	if part != "" {
		relsPath = strings.TrimPrefix(folder+"/_rels/"+path.Base(part)+".rels", "/")
	}
	var out []Relationship
	if root := p.Part(relsPath); root != nil {
		for _, rel := range root.All(PackageRelationships, "Relationship") {
			target := rel.A("Target")
			external := rel.A("TargetMode") == "External"
			if !external {
				target = ResolvePath(folder, target)
			}
			out = append(out, Relationship{ID: rel.A("Id"), Type: rel.A("Type"), Target: target, External: external})
		}
	}
	p.rels[key] = out
	return out
}

// Target is the zip path that relationship id of part points to, or "".
func (p *Package) Target(part, id string) string {
	if id == "" {
		return ""
	}
	for _, r := range p.Relationships(part) {
		if r.ID == id && !r.External {
			return r.Target
		}
	}
	return ""
}

// Rel returns relationship id of part.
func (p *Package) Rel(part, id string) (Relationship, bool) {
	for _, r := range p.Relationships(part) {
		if r.ID == id {
			return r, true
		}
	}
	return Relationship{}, false
}

// TargetOfType is the first internal target whose type ends with suffix ("/theme").
func (p *Package) TargetOfType(part, suffix string) string {
	for _, r := range p.Relationships(part) {
		if !r.External && strings.HasSuffix(r.Type, suffix) {
			return r.Target
		}
	}
	return ""
}

// Extract copies a binary part to folder under a fresh name; "" when missing.
func (p *Package) Extract(part, folder string, name string) string {
	e := p.Entry(part)
	if e == nil {
		return ""
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return ""
	}
	rc, err := e.Open()
	if err != nil {
		return ""
	}
	defer rc.Close()
	file := filepath.Join(folder, name+strings.ToLower(path.Ext(part)))
	out, err := os.Create(file)
	if err != nil {
		return ""
	}
	_, err = io.Copy(out, io.LimitReader(rc, MaxPartBytes))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(file)
		return ""
	}
	return file
}

// Folder is the folder of a zip path ("" at the top).
func Folder(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}

// ResolvePath resolves a relationship target against a folder, as a zip path.
func ResolvePath(folder, target string) string {
	if strings.HasPrefix(target, "/") {
		return strings.TrimLeft(target, "/")
	}
	var parts []string
	if folder != "" {
		parts = strings.Split(folder, "/")
	}
	if u, err := url.PathUnescape(target); err == nil {
		target = u
	}
	for _, seg := range strings.Split(target, "/") {
		switch seg {
		case "..":
			if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}
		case ".", "":
		default:
			parts = append(parts, seg)
		}
	}
	return strings.Join(parts, "/")
}
