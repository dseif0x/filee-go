// Package office converts between the Office Open XML variants without
// Office: DOCM / DOTX / DOTM ↔ DOCX, XLSM / XLTX ↔ XLSX and PPTM / POTX /
// POTM / PPSX ↔ PPTX. A variant is the same package with another content
// type for its main part, so the package is copied with that type
// rewritten. Going to a macro-free type also drops the VBA project, its
// signatures and data, Word's key map customisations and Excel 4.0 macro
// sheets, with every relationship pointing to them (Office refuses a
// macro-free file that still holds them).
package office

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/office/opc"
	"github.com/dseif0x/filee-go/internal/xdom"
)

const (
	contentTypesNs       = "http://schemas.openxmlformats.org/package/2006/content-types"
	officeRelationshipNs = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	spreadsheetNs        = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
)

// Variant is one Office Open XML file type.
type Variant struct {
	Family      string
	ContentType string
	Macros      bool
}

// Variants maps a format id to its family, main part content type and
// whether it may hold macros.
var Variants = map[string]Variant{
	"docx": {"word", "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml", false},
	"docm": {"word", "application/vnd.ms-word.document.macroEnabled.main+xml", true},
	"dotx": {"word", "application/vnd.openxmlformats-officedocument.wordprocessingml.template.main+xml", false},
	"dotm": {"word", "application/vnd.ms-word.template.macroEnabledTemplate.main+xml", true},
	"xlsx": {"excel", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml", false},
	"xlsm": {"excel", "application/vnd.ms-excel.sheet.macroEnabled.main+xml", true},
	"xltx": {"excel", "application/vnd.openxmlformats-officedocument.spreadsheetml.template.main+xml", false},
	"pptx": {"powerpoint", "application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml", false},
	"pptm": {"powerpoint", "application/vnd.ms-powerpoint.presentation.macroEnabled.main+xml", true},
	"potx": {"powerpoint", "application/vnd.openxmlformats-officedocument.presentationml.template.main+xml", false},
	"potm": {"powerpoint", "application/vnd.ms-powerpoint.template.macroEnabled.main+xml", true},
	"ppsx": {"powerpoint", "application/vnd.openxmlformats-officedocument.presentationml.slideshow.main+xml", false},
}

// variantOrder is the order of the C# dictionary (edges come out the same).
var variantOrder = []string{"docx", "docm", "dotx", "dotm", "xlsx", "xlsm", "xltx", "pptx", "pptm", "potx", "potm", "ppsx"}

// MacroContentTypes are the parts only macro-enabled files may contain.
var MacroContentTypes = []string{
	"application/vnd.ms-office.vbaProject",
	"application/vnd.ms-office.vbaProjectSignature",
	"application/vnd.ms-office.vbaProjectSignatureAgile",
	"application/vnd.ms-office.vbaProjectSignatureV3",
	"application/vnd.ms-word.vbaData+xml",
	"application/vnd.ms-word.keyMapCustomizations+xml",
	"application/vnd.ms-excel.macrosheet+xml",
	"application/vnd.ms-excel.intlmacrosheet+xml",
}

func isMacroType(t string) bool {
	return slices.ContainsFunc(MacroContentTypes, func(m string) bool { return strings.EqualFold(m, t) })
}

// Converter is the "ooxml" engine.
type Converter struct{}

func New() *Converter { return &Converter{} }

func (*Converter) ID() string          { return "ooxml" }
func (*Converter) Name() string        { return "Office Open XML (built-in)" }
func (*Converter) MaxParallelism() int { return 0 }

func (*Converter) Edges() []convert.Edge {
	var edges []convert.Edge
	for _, s := range variantOrder {
		for _, t := range variantOrder {
			if s != t && Variants[s].Family == Variants[t].Family {
				edges = append(edges, convert.E(s, t))
			}
		}
	}
	return edges
}

func (*Converter) Status() convert.Status {
	return convert.Available("DOCM, DOTX, DOTM ↔ DOCX; XLSM, XLTX ↔ XLSX; PPTM, POTX, POTM, PPSX ↔ PPTX", "built-in")
}

func (*Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	progress.Report(0.1)
	output := step.Output.Allocate(step.To, "")
	if output == "" {
		return nil, nil
	}
	temp := output + ".tmp"
	defer os.Remove(temp)
	if err := Rewrite(step.InputPath, temp, step.To); err != nil {
		return nil, err
	}
	if err := os.Rename(temp, output); err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{output}, nil
}

// Rewrite copies the package input to output as the variant to.
func Rewrite(input, output, to string) error {
	target, ok := Variants[strings.ToLower(to)]
	if !ok {
		return errors.New("unknown Office Open XML variant " + to)
	}
	src, err := zip.OpenReader(input)
	if err != nil {
		return err
	}
	defer src.Close()
	entries := map[string]*zip.File{}
	for _, e := range src.File {
		if _, dup := entries[e.Name]; !dup {
			entries[e.Name] = e
		}
	}
	load := func(name string) *xdom.Node {
		e := entries[name]
		if e == nil {
			return nil
		}
		rc, err := e.Open()
		if err != nil {
			return nil
		}
		defer rc.Close()
		n, _ := xdom.Parse(rc, opc.MaxPartBytes)
		return n
	}
	types := load("[Content_Types].xml")
	if types == nil {
		return errors.New("the file is not an Office Open XML package (no [Content_Types].xml)")
	}
	main := ""
	for _, r := range load("_rels/.rels").All(opc.PackageRelationships, "Relationship") {
		if strings.HasSuffix(r.A("Type"), "/officeDocument") {
			main = strings.TrimLeft(r.A("Target"), "/")
			break
		}
	}
	if main == "" {
		return errors.New("the package has no main document part")
	}

	// The main part gets the target's content type.
	var mainOverride *xdom.Node
	for _, o := range types.All(contentTypesNs, "Override") {
		if strings.EqualFold(strings.TrimLeft(o.A("PartName"), "/"), main) {
			mainOverride = o
			break
		}
	}
	if mainOverride == nil {
		mainOverride = xdom.NewElement(contentTypesNs, "Override")
		mainOverride.SetAttr("", "PartName", "/"+main)
		types.Add(mainOverride)
	}
	mainOverride.SetAttr("", "ContentType", target.ContentType)

	// Macro parts (and their relationship parts) go when the target may not contain macros.
	removed := map[string]bool{}
	if !target.Macros {
		for _, e := range src.File {
			if t := contentTypeOf(types, e.Name); t != "" && isMacroType(t) {
				removed[strings.ToLower(e.Name)] = true
			}
		}
		for part := range maps(removed) {
			removed[strings.ToLower(relationshipsPartOf(part))] = true
		}
		for _, o := range types.All(contentTypesNs, "Override") {
			if removed[strings.ToLower(strings.TrimLeft(o.A("PartName"), "/"))] {
				o.Remove()
			}
		}
		for _, d := range types.All(contentTypesNs, "Default") {
			if isMacroType(d.A("ContentType")) {
				d.Remove()
			}
		}
	}

	// Relationships to removed parts, by source part (the workbook's sheet list refers to them by id).
	rewritten := map[string]*xdom.Node{}
	removedIds := map[string]map[string]bool{}
	if len(removed) > 0 {
		for _, e := range src.File {
			if !strings.HasSuffix(strings.ToLower(e.Name), ".rels") || removed[strings.ToLower(e.Name)] {
				continue
			}
			rels := load(e.Name)
			if rels == nil {
				continue
			}
			owner := ownerOf(e.Name)
			for _, r := range rels.All(opc.PackageRelationships, "Relationship") {
				if r.A("TargetMode") != "External" && removed[strings.ToLower(resolve(owner, r.A("Target")))] {
					if removedIds[strings.ToLower(owner)] == nil {
						removedIds[strings.ToLower(owner)] = map[string]bool{}
					}
					removedIds[strings.ToLower(owner)][r.A("Id")] = true
					r.Remove()
				}
			}
			rewritten[e.Name] = rels
		}
	}
	var workbook *xdom.Node
	if target.Family == "excel" && len(removedIds[strings.ToLower(main)]) > 0 {
		if workbook = load(main); workbook != nil {
			removeSheets(workbook, removedIds[strings.ToLower(main)])
		}
	}

	f, err := os.Create(output)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	written := map[string]bool{}
	for _, e := range src.File {
		if removed[strings.ToLower(e.Name)] || strings.HasSuffix(e.Name, "/") || written[e.Name] {
			continue
		}
		written[e.Name] = true
		switch {
		case e.Name == "[Content_Types].xml":
			err = save(zw, e.Name, types)
		case rewritten[e.Name] != nil:
			err = save(zw, e.Name, rewritten[e.Name])
		case workbook != nil && e.Name == main:
			err = save(zw, e.Name, workbook)
		default:
			err = zw.Copy(e)
		}
		if err != nil {
			zw.Close()
			f.Close()
			return err
		}
	}
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func maps(m map[string]bool) map[string]bool {
	c := make(map[string]bool, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// removeSheets drops <sheet> entries (and names scoped to them) whose relationship was removed.
func removeSheets(workbook *xdom.Node, ids map[string]bool) {
	sheets := workbook.Child(spreadsheetNs, "sheets").All(spreadsheetNs, "sheet")
	var kept []int
	for i, s := range sheets {
		if ids[s.AN(officeRelationshipNs, "id")] {
			s.Remove()
		} else {
			kept = append(kept, i)
		}
	}
	for _, name := range workbook.Child(spreadsheetNs, "definedNames").All(spreadsheetNs, "definedName") {
		local, err := strconv.Atoi(name.A("localSheetId"))
		if err != nil {
			continue
		}
		if i := slices.Index(kept, local); i < 0 {
			name.Remove()
		} else {
			name.SetAttr("", "localSheetId", strconv.Itoa(i))
		}
	}
	for _, view := range workbook.Descendants(spreadsheetNs, "workbookView") {
		view.RemoveAttr("", "activeTab")
		view.RemoveAttr("", "firstSheet")
	}
}

func contentTypeOf(types *xdom.Node, part string) string {
	name := "/" + part
	for _, o := range types.All(contentTypesNs, "Override") {
		if strings.EqualFold(o.A("PartName"), name) {
			return o.A("ContentType")
		}
	}
	ext := strings.TrimPrefix(path.Ext(part), ".")
	for _, d := range types.All(contentTypesNs, "Default") {
		if strings.EqualFold(d.A("Extension"), ext) {
			return d.A("ContentType")
		}
	}
	return ""
}

// relationshipsPartOf: "word/vbaProject.bin" → "word/_rels/vbaProject.bin.rels".
func relationshipsPartOf(part string) string {
	i := strings.LastIndexByte(part, '/')
	if i < 0 {
		return "_rels/" + part + ".rels"
	}
	return part[:i] + "/_rels/" + part[i+1:] + ".rels"
}

// ownerOf: "word/_rels/document.xml.rels" → "word/document.xml"; "_rels/.rels" → "".
func ownerOf(rels string) string {
	i := strings.Index(rels, "_rels/")
	if i < 0 {
		return ""
	}
	return rels[:i] + strings.TrimSuffix(path.Base(rels), ".rels")
}

func resolve(source, target string) string { return opc.ResolvePath(opc.Folder(source), target) }

func save(zw *zip.Writer, name string, n *xdom.Node) error {
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
	if err != nil {
		return err
	}
	var b bytes.Buffer
	if err := xdom.Write(&b, n, nil); err != nil {
		return err
	}
	_, err = io.Copy(w, &b)
	return err
}
