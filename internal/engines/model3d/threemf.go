package model3d

// 3MF (3D Manufacturing Format): a ZIP package with XML models (core
// specification 1.3). Z up, unit from the model's "unit" attribute. Read:
// objects with meshes and components (also in other model files of the
// package, as slicers such as Bambu Studio write them), build items with
// transforms, base materials (one part per material) and the materials
// extension's colour groups (per-corner colours). Objects of type "support"
// and "other" are skipped. Written: millimetres, one object per part with its
// vertices welded by position (slicers need closed meshes), base materials
// from the material colours and colour groups from the vertex colours. UVs,
// normals and textures are not written.

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	threeMfCore         = "http://schemas.microsoft.com/3dmanufacturing/core/2015/02"
	threeMfMaterial     = "http://schemas.microsoft.com/3dmanufacturing/material/2015/02"
	threeMfProduction   = "http://schemas.microsoft.com/3dmanufacturing/production/2015/06"
	threeMfRelationship = "http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"
	threeMfMaxDepth     = 32
)

func threeMfUnitScale(unit string) (float64, error) {
	switch unit {
	case "", "millimeter":
		return 1, nil
	case "micron":
		return 0.001, nil
	case "centimeter":
		return 10, nil
	case "inch":
		return 25.4, nil
	case "foot":
		return 304.8, nil
	case "meter":
		return 1000, nil
	}
	return 0, fmt.Errorf("The 3MF model has an unknown unit %q.", unit)
}

type tmfBase struct {
	name  string
	color vec4
}

type tmfRef struct {
	objectID  int
	path      string
	hasPath   bool
	transform mat4
}

type tmfFile struct {
	scale         float64
	objects       map[int]*tmfObject
	baseMaterials map[int][]tmfBase
	colorGroups   map[int][]vec4
	build         []tmfRef
}

type tmfObject struct {
	name         string
	typ          string
	pid, pindex  int
	hasPid, hasP bool
	vertices     []float32
	// v1, v2, v3, pid, p1, p2, p3 per triangle (-1 when not given).
	triangles  []int
	components []tmfRef
}

type tmfReader struct {
	zip   *zip.Reader
	files map[string]*tmfFile
}

type tmfMaterialKey struct {
	file       *tmfFile
	pid, index int
}

func readThreeMf(ctx context.Context, path string, progress func(float64)) (*Model, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("The file is not a 3MF package (it is not a valid ZIP file).")
	}
	defer zr.Close()
	r := &tmfReader{zip: &zr.Reader, files: map[string]*tmfFile{}}
	rootPath, err := r.findRoot()
	if err != nil {
		return nil, err
	}
	root, err := r.load(ctx, rootPath)
	if err != nil {
		return nil, err
	}
	if progress != nil {
		progress(0.6)
	}
	model := &Model{}
	materials := map[tmfMaterialKey]int{}
	for _, item := range root.build {
		file := root
		if item.hasPath {
			if file, err = r.load(ctx, item.path); err != nil {
				return nil, err
			}
		}
		if err := r.instantiate(ctx, model, file, item.objectID, item.transform, materials, 0); err != nil {
			return nil, err
		}
	}
	toYUp := uniformScale(float32(root.scale)).mul(zUpToYUp)
	for _, p := range model.Parts {
		p.Transform(toYUp)
	}
	return model, nil
}

func (r *tmfReader) entry(name string) *zip.File {
	for _, f := range r.zip.File {
		if f.Name == name {
			return f
		}
	}
	for _, f := range r.zip.File {
		if strings.EqualFold(f.Name, name) {
			return f
		}
	}
	return nil
}

func (r *tmfReader) findRoot() (string, error) {
	if rels := r.entry("_rels/.rels"); rels != nil {
		if rc, err := rels.Open(); err == nil {
			d := xml.NewDecoder(rc)
			for {
				tok, err := d.Token()
				if err != nil {
					break
				}
				if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "Relationship" {
					if attr(se, "Type") == threeMfRelationship {
						if target := attr(se, "Target"); target != "" {
							rc.Close()
							return strings.TrimLeft(target, "/"), nil
						}
					}
				}
			}
			rc.Close()
		}
	}
	for _, f := range r.zip.File {
		if strings.HasSuffix(strings.ToLower(f.Name), ".model") {
			return f.Name, nil
		}
	}
	return "", errors.New("The 3MF package has no 3D model part.")
}

func attr(se xml.StartElement, local string) string {
	for _, a := range se.Attr {
		if a.Name.Local == local && (a.Name.Space == "" || a.Name.Space == se.Name.Space) {
			return a.Value
		}
	}
	return ""
}

func attrNS(se xml.StartElement, space, local string) (string, bool) {
	for _, a := range se.Attr {
		if a.Name.Local == local && a.Name.Space == space {
			return a.Value, true
		}
	}
	return "", false
}

func hasAttr(se xml.StartElement, local string) (string, bool) {
	for _, a := range se.Attr {
		if a.Name.Local == local && a.Name.Space == "" {
			return a.Value, true
		}
	}
	return "", false
}

func (r *tmfReader) load(ctx context.Context, path string) (*tmfFile, error) {
	path = strings.TrimLeft(path, "/")
	key := strings.ToLower(path)
	if f, ok := r.files[key]; ok {
		return f, nil
	}
	e := r.entry(path)
	if e == nil {
		return nil, fmt.Errorf("The 3MF package has no part %q.", path)
	}
	file := &tmfFile{scale: 1, objects: map[int]*tmfObject{}, baseMaterials: map[int][]tmfBase{}, colorGroups: map[int][]vec4{}}
	r.files[key] = file
	rc, err := e.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	if err := parseThreeMf(ctx, bufio.NewReaderSize(rc, 1<<16), file); err != nil {
		var se *xml.SyntaxError
		if errors.As(err, &se) {
			return nil, fmt.Errorf("The 3MF model %q is not valid XML: %s", path, se.Msg)
		}
		return nil, err
	}
	return file, nil
}

func tmfInt(se xml.StartElement, name string) (int, error) {
	if v, ok := tmfOptionalInt(se, name); ok {
		return v, nil
	}
	return 0, fmt.Errorf("The 3MF model has a %s without a valid %q.", se.Name.Local, name)
}

func tmfOptionalInt(se xml.StartElement, name string) (int, bool) {
	text, ok := hasAttr(se, name)
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseInt(strings.TrimSpace(text), 10, 32)
	if err != nil {
		return 0, false
	}
	return int(v), true
}

func tmfFloat(se xml.StartElement, name string) (float32, error) {
	text, _ := hasAttr(se, name)
	v, ok := parseFloat32(strings.TrimSpace(text))
	if !ok || !finite(v) {
		return 0, fmt.Errorf("The 3MF model has a %s with an invalid %q.", se.Name.Local, name)
	}
	return v, nil
}

func tmfTransform(se xml.StartElement) (mat4, error) {
	text, _ := hasAttr(se, "transform")
	if strings.TrimSpace(text) == "" {
		return identity, nil
	}
	fields := strings.Fields(text)
	if len(fields) != 12 {
		return mat4{}, fmt.Errorf("The 3MF model has an invalid transform %q.", text)
	}
	var v [12]float32
	for i, f := range fields {
		x, ok := parseFloat32(f)
		if !ok || !finite(x) {
			return mat4{}, fmt.Errorf("The 3MF model has an invalid transform %q.", text)
		}
		v[i] = x
	}
	return mat4{{v[0], v[1], v[2], 0}, {v[3], v[4], v[5], 0}, {v[6], v[7], v[8], 0}, {v[9], v[10], v[11], 1}}, nil
}

func parseThreeMf(ctx context.Context, rd io.Reader, file *tmfFile) error {
	d := xml.NewDecoder(rd)
	var current *tmfObject
	var bases *[]tmfBase
	var colors *[]vec4
	baseID, colorID := 0, 0
	count := 0
	optional := func(se xml.StartElement, name string) int {
		if v, ok := tmfOptionalInt(se, name); ok {
			return v
		}
		return -1
	}
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if ee, ok := tok.(xml.EndElement); ok {
			switch ee.Name.Local {
			case "object":
				current = nil
			case "basematerials":
				if bases != nil {
					file.baseMaterials[baseID] = *bases
				}
				bases = nil
			case "colorgroup":
				if colors != nil {
					file.colorGroups[colorID] = *colors
				}
				colors = nil
			}
			continue
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		count++
		if count&0xFFFF == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		switch se.Name.Local {
		case "model":
			unit, _ := hasAttr(se, "unit")
			s, err := threeMfUnitScale(unit)
			if err != nil {
				return err
			}
			file.scale = s
		case "vertex":
			if current == nil {
				continue
			}
			x, err := tmfFloat(se, "x")
			if err != nil {
				return err
			}
			y, err := tmfFloat(se, "y")
			if err != nil {
				return err
			}
			z, err := tmfFloat(se, "z")
			if err != nil {
				return err
			}
			current.vertices = append(current.vertices, x, y, z)
		case "triangle":
			if current == nil {
				continue
			}
			var v [3]int
			for i, n := range []string{"v1", "v2", "v3"} {
				if v[i], err = tmfInt(se, n); err != nil {
					return err
				}
			}
			current.triangles = append(current.triangles, v[0], v[1], v[2],
				optional(se, "pid"), optional(se, "p1"), optional(se, "p2"), optional(se, "p3"))
		case "object":
			id, err := tmfInt(se, "id")
			if err != nil {
				return err
			}
			o := &tmfObject{typ: "model"}
			o.name, _ = hasAttr(se, "name")
			if t, ok := hasAttr(se, "type"); ok {
				o.typ = t
			}
			o.pid, o.hasPid = tmfOptionalInt(se, "pid")
			o.pindex, o.hasP = tmfOptionalInt(se, "pindex")
			file.objects[id] = o
			current = o
		case "component":
			if current == nil {
				continue
			}
			ref, err := tmfReference(se)
			if err != nil {
				return err
			}
			current.components = append(current.components, ref)
		case "item":
			ref, err := tmfReference(se)
			if err != nil {
				return err
			}
			file.build = append(file.build, ref)
		case "basematerials":
			id, err := tmfInt(se, "id")
			if err != nil {
				return err
			}
			baseID = id
			list := []tmfBase{}
			bases = &list
			file.baseMaterials[id] = list
		case "base":
			if bases == nil {
				continue
			}
			name, _ := hasAttr(se, "name")
			color := vec4{1, 1, 1, 1}
			if text, ok := hasAttr(se, "displaycolor"); ok {
				if c, ok := parseHex(text); ok {
					color = c
				}
			}
			*bases = append(*bases, tmfBase{name, color})
		case "colorgroup":
			if se.Name.Space != threeMfMaterial && se.Name.Space != "" {
				continue
			}
			id, err := tmfInt(se, "id")
			if err != nil {
				return err
			}
			colorID = id
			list := []vec4{}
			colors = &list
			file.colorGroups[id] = list
		case "color":
			if colors == nil {
				continue
			}
			color := vec4{1, 1, 1, 1}
			if text, ok := hasAttr(se, "color"); ok {
				if c, ok := parseHex(text); ok {
					color = c
				}
			}
			*colors = append(*colors, color)
		}
	}
	return nil
}

func tmfReference(se xml.StartElement) (tmfRef, error) {
	id, err := tmfInt(se, "objectid")
	if err != nil {
		return tmfRef{}, err
	}
	t, err := tmfTransform(se)
	if err != nil {
		return tmfRef{}, err
	}
	p, ok := attrNS(se, threeMfProduction, "path")
	return tmfRef{objectID: id, path: p, hasPath: ok, transform: t}, nil
}

func (r *tmfReader) instantiate(ctx context.Context, model *Model, file *tmfFile, objectID int, transform mat4, materials map[tmfMaterialKey]int, depth int) error {
	if depth > threeMfMaxDepth {
		return errors.New("The 3MF model's components are nested too deeply (or refer to themselves).")
	}
	def, ok := file.objects[objectID]
	if !ok {
		return fmt.Errorf("The 3MF model refers to object %d, which does not exist.", objectID)
	}
	if def.typ == "support" || def.typ == "other" {
		return nil
	}
	if len(def.triangles) > 0 {
		if err := addThreeMfMesh(ctx, model, file, def, transform, materials); err != nil {
			return err
		}
	}
	for _, c := range def.components {
		child := file
		if c.hasPath {
			var err error
			if child, err = r.load(ctx, c.path); err != nil {
				return err
			}
		}
		if err := r.instantiate(ctx, model, child, c.objectID, c.transform.mul(transform), materials, depth+1); err != nil {
			return err
		}
	}
	return nil
}

type tmfGroup struct {
	part *Part
	vmap map[[2]int]int
}

func addThreeMfMesh(ctx context.Context, model *Model, file *tmfFile, def *tmfObject, transform mat4, materials map[tmfMaterialKey]int) error {
	vertexCount := len(def.vertices) / 3
	// One part per base material; per-corner colours split vertices by colour.
	groups := map[int]*tmfGroup{}
	tris := def.triangles
	mirrored := transform.determinant() < 0
	var corners [3]int
	for t := 0; t+6 < len(tris); t += 7 {
		if t&0xFFFF == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		pid := tris[t+3]
		if pid < 0 {
			pid = -1
			if def.hasPid {
				pid = def.pid
			}
		}
		p1 := tris[t+4]
		if p1 < 0 {
			p1 = 0
			if def.hasP {
				p1 = def.pindex
			}
		}
		key := -1
		var colorGroup []vec4
		if bases, ok := file.baseMaterials[pid]; ok && p1 < len(bases) {
			key = threeMfMaterialIndex(model, file, pid, p1, bases[p1], materials)
		} else if cg, ok := file.colorGroups[pid]; ok {
			colorGroup = cg
			key = -2
		}
		g, ok := groups[key]
		if !ok {
			name := def.name
			if name == "" {
				name = "Object"
			}
			part := newPart(name)
			if key >= 0 {
				part.Material = key
			}
			if key == -2 {
				part.Colors = []float32{}
			}
			g = &tmfGroup{part: part, vmap: map[[2]int]int{}}
			groups[key] = g
			model.Parts = append(model.Parts, part)
		}
		for k := 0; k < 3; k++ {
			v := tris[t+k]
			if v < 0 || v >= vertexCount {
				return fmt.Errorf("A triangle of 3MF object %q refers to vertex %d, but it has only %d.", def.name, v, vertexCount)
			}
			colorIndex := -1
			if key == -2 {
				p := p1
				if k > 0 && tris[t+4+k] >= 0 {
					p = tris[t+4+k]
				}
				if p < len(colorGroup) {
					colorIndex = p
				}
			}
			mk := [2]int{v, colorIndex}
			index, ok := g.vmap[mk]
			if !ok {
				pos := transform.transform(vec3{def.vertices[v*3], def.vertices[v*3+1], def.vertices[v*3+2]})
				color := vec4{1, 1, 1, 1}
				if colorIndex >= 0 {
					color = colorGroup[colorIndex]
				}
				index = g.part.AddVertex(pos, nil, nil, &color)
				g.vmap[mk] = index
			}
			corners[k] = index
		}
		if mirrored {
			g.part.AddTriangle(corners[0], corners[2], corners[1])
		} else {
			g.part.AddTriangle(corners[0], corners[1], corners[2])
		}
	}
	return nil
}

func threeMfMaterialIndex(model *Model, file *tmfFile, pid, index int, base tmfBase, materials map[tmfMaterialKey]int) int {
	key := tmfMaterialKey{file, pid, index}
	if i, ok := materials[key]; ok {
		return i
	}
	i := len(model.Materials)
	name := base.name
	if name == "" {
		name = fmt.Sprintf("Material %d", i+1)
	}
	m := newMaterial(name)
	m.BaseColor = srgbToLinear4(base.color)
	model.Materials = append(model.Materials, m)
	materials[key] = i
	return i
}

// writeThreeMf writes a 3MF package (millimetres, Z up) with one object per part.
func writeThreeMf(ctx context.Context, m *Model, w io.Writer) error {
	zw := zip.NewWriter(w)
	text := func(name, content string) error {
		f, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(f, content)
		return err
	}
	if err := text("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?>`+"\n"+
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="model" ContentType="application/vnd.ms-package.3dmanufacturing-3dmodel+xml"/></Types>`); err != nil {
		return err
	}
	if err := text("_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?>`+"\n"+
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Target="/3D/3dmodel.model" Id="rel0" Type="`+threeMfRelationship+`"/></Relationships>`); err != nil {
		return err
	}
	f, err := zw.Create("3D/3dmodel.model")
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(f, 1<<16)
	esc := func(s string) string {
		var b strings.Builder
		xml.EscapeText(&b, []byte(s))
		return strings.ReplaceAll(b.String(), `"`, "&quot;")
	}
	anyColors := false
	for _, p := range m.Parts {
		anyColors = anyColors || p.Colors != nil
	}
	bw.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	bw.WriteString(`<model unit="millimeter" xml:lang="en-US"`)
	if anyColors {
		bw.WriteString(` xmlns:m="` + threeMfMaterial + `"`)
	}
	bw.WriteString(` xmlns="` + threeMfCore + `">` + "\n")
	bw.WriteString("  <metadata name=\"Application\">Filee</metadata>\n  <resources>\n")
	nextID := 1
	baseMaterialsID := 0
	if len(m.Materials) > 0 {
		baseMaterialsID = nextID
		nextID++
		fmt.Fprintf(bw, "    <basematerials id=\"%d\">\n", baseMaterialsID)
		for i, mat := range m.Materials {
			name := mat.Name
			if name == "" {
				name = fmt.Sprintf("Material %d", i+1)
			}
			fmt.Fprintf(bw, "      <base name=\"%s\" displaycolor=\"%s\" />\n", esc(name), toHex(linearToSrgb4(mat.BaseColor)))
		}
		bw.WriteString("    </basematerials>\n")
	}
	var objectIDs []int
	for _, p := range m.Parts {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Welded by position: split vertices (for normals or UVs) would leave the mesh open.
		weld := map[vec3]int{}
		vertexOf := make([]int, p.VertexCount())
		var positions []vec3
		for v := 0; v < p.VertexCount(); v++ {
			q := toZUp(p.Position(v)).noNegativeZero()
			i, ok := weld[q]
			if !ok {
				i = len(positions)
				positions = append(positions, q)
				weld[q] = i
			}
			vertexOf[v] = i
		}
		colorGroupID := 0
		var colorOf []int
		if p.Colors != nil {
			colorGroupID = nextID
			nextID++
			unique := map[uint32]int{}
			colorOf = make([]int, p.VertexCount())
			fmt.Fprintf(bw, "    <m:colorgroup id=\"%d\">\n", colorGroupID)
			for v := 0; v < p.VertexCount(); v++ {
				c := p.Color(v)
				key := uint32(toByte(c.X))<<24 | uint32(toByte(c.Y))<<16 | uint32(toByte(c.Z))<<8 | uint32(toByte(c.W))
				i, ok := unique[key]
				if !ok {
					i = len(unique)
					unique[key] = i
					fmt.Fprintf(bw, "      <m:color color=\"%s\" />\n", toHex(c))
				}
				colorOf[v] = i
			}
			bw.WriteString("    </m:colorgroup>\n")
		}
		objectID := nextID
		nextID++
		objectIDs = append(objectIDs, objectID)
		fmt.Fprintf(bw, "    <object id=\"%d\" type=\"model\"", objectID)
		if p.Name != "" {
			fmt.Fprintf(bw, " name=\"%s\"", esc(p.Name))
		}
		if colorGroupID > 0 {
			fmt.Fprintf(bw, " pid=\"%d\" pindex=\"0\"", colorGroupID)
		} else if p.Material >= 0 && p.Material < len(m.Materials) {
			fmt.Fprintf(bw, " pid=\"%d\" pindex=\"%d\"", baseMaterialsID, p.Material)
		}
		bw.WriteString(">\n      <mesh>\n        <vertices>\n")
		buf := make([]byte, 0, 128)
		for _, q := range positions {
			buf = append(buf[:0], `          <vertex x="`...)
			buf = appendInvariantFloat(buf, q.X)
			buf = append(buf, `" y="`...)
			buf = appendInvariantFloat(buf, q.Y)
			buf = append(buf, `" z="`...)
			buf = appendInvariantFloat(buf, q.Z)
			buf = append(buf, "\" />\n"...)
			bw.Write(buf)
		}
		bw.WriteString("        </vertices>\n        <triangles>\n")
		for i := 0; i+2 < len(p.Indices); i += 3 {
			a, b, c := vertexOf[p.Indices[i]], vertexOf[p.Indices[i+1]], vertexOf[p.Indices[i+2]]
			if a == b || b == c || a == c {
				continue // collapsed by welding
			}
			buf = append(buf[:0], `          <triangle v1="`...)
			buf = strconv.AppendInt(buf, int64(a), 10)
			buf = append(buf, `" v2="`...)
			buf = strconv.AppendInt(buf, int64(b), 10)
			buf = append(buf, `" v3="`...)
			buf = strconv.AppendInt(buf, int64(c), 10)
			buf = append(buf, '"')
			if colorOf != nil {
				buf = append(buf, ` pid="`...)
				buf = strconv.AppendInt(buf, int64(colorGroupID), 10)
				for k, name := range []string{"p1", "p2", "p3"} {
					buf = append(buf, `" `+name+`="`...)
					buf = strconv.AppendInt(buf, int64(colorOf[p.Indices[i+k]]), 10)
				}
				buf = append(buf, '"')
			}
			buf = append(buf, " />\n"...)
			bw.Write(buf)
		}
		bw.WriteString("        </triangles>\n      </mesh>\n    </object>\n")
	}
	bw.WriteString("  </resources>\n  <build>\n")
	for _, id := range objectIDs {
		fmt.Fprintf(bw, "    <item objectid=\"%d\" />\n", id)
	}
	bw.WriteString("  </build>\n</model>")
	if err := bw.Flush(); err != nil {
		return err
	}
	return zw.Close()
}

// appendInvariantFloat formats like .NET's float.ToString(InvariantCulture) (0 for NaN / infinity).
func appendInvariantFloat(buf []byte, v float32) []byte {
	if !finite(v) {
		v = 0
	}
	return appendShortFloat(buf, v)
}
