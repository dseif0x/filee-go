// Package formats is the central list of supported formats and the
// extension lookup. To add a format, add one line to Known and give at least
// one converter an edge for it.
package formats

import (
	"path/filepath"
	"sort"
	"strings"
)

// Category groups formats (donut slices, routes for raster sources, …).
type Category string

const (
	Image        Category = "Image"
	Pdf          Category = "Pdf"
	Document     Category = "Document"
	Spreadsheet  Category = "Spreadsheet"
	Presentation Category = "Presentation"
	Hwp          Category = "Hwp"
	Text         Category = "Text"
	Video        Category = "Video"
	Audio        Category = "Audio"
	Ebook        Category = "Ebook"
	Archive      Category = "Archive"
	Vector       Category = "Vector"
	Cad          Category = "Cad"
	Font         Category = "Font"
	Subtitle     Category = "Subtitle"
	Data         Category = "Data"
	Model3D      Category = "Model3D"
	Geo          Category = "Geo"
)

// Categories in display order.
var Categories = []Category{Image, Vector, Pdf, Document, Hwp, Spreadsheet, Presentation, Text, Data, Ebook,
	Video, Audio, Archive, Cad, Font, Subtitle, Model3D, Geo}

// Folder is the pseudo format of a dropped directory (extension "").
const Folder = "folder"

// Format describes one file format Filee understands (e.g. "png", "hwpx").
type Format struct {
	ID         string
	Name       string
	Category   Category
	Extensions []string
	// ReadOnly formats are sources only.
	ReadOnly bool
}

// Writable reports whether Filee can produce the format.
func (f *Format) Writable() bool { return !f.ReadOnly }

// PrimaryExtension is the extension written for the format.
func (f *Format) PrimaryExtension() string { return f.Extensions[0] }

var (
	byID        = map[string]*Format{}
	byExtension = map[string]*Format{}
	// compound extensions ("tar.gz", "kepub.epub"), longest first
	compound []string
)

func init() {
	for i := range Known {
		f := &Known[i]
		byID[strings.ToLower(f.ID)] = f
		for _, e := range f.Extensions {
			if e == "" {
				continue
			}
			e = strings.ToLower(e)
			if _, dup := byExtension[e]; !dup {
				byExtension[e] = f
			}
			if strings.Contains(e, ".") {
				compound = append(compound, e)
			}
		}
	}
	sort.SliceStable(compound, func(i, j int) bool { return len(compound[i]) > len(compound[j]) })
}

// ByID returns the format with id, or nil.
func ByID(id string) *Format { return byID[strings.ToLower(id)] }

// CategoryOf returns the category of the format id ("" when unknown).
func CategoryOf(id string) Category {
	if f := ByID(id); f != nil {
		return f.Category
	}
	return ""
}

// ByExtension returns the format of an extension (with or without the dot), or nil.
func ByExtension(ext string) *Format {
	ext = strings.TrimLeft(ext, ".")
	if ext == "" {
		return nil
	}
	return byExtension[strings.ToLower(ext)]
}

// Detect returns the format of a path by its extension, or nil.
func Detect(path string) *Format { return ByExtension(ExtensionOf(path)) }

// ExtensionOf returns the extension of path without the dot; compound
// extensions such as "tar.gz" are matched as a whole.
func ExtensionOf(path string) string {
	name := filepath.Base(path)
	lower := strings.ToLower(name)
	for _, c := range compound {
		if len(name) > len(c)+1 && strings.HasSuffix(lower, "."+c) {
			return name[len(name)-len(c):]
		}
	}
	return strings.TrimPrefix(filepath.Ext(name), ".")
}

// NameWithoutExtension returns the file name of path without ExtensionOf.
func NameWithoutExtension(path string) string {
	name := filepath.Base(path)
	ext := ExtensionOf(name)
	if ext == "" {
		return name
	}
	return name[:len(name)-len(ext)-1]
}
