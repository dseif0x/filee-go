package model3d

// glTF 2.0 (.gltf with embedded or separate buffers, and binary .glb) through
// github.com/qmuntal/gltf. Metres, Y up. Read: the default scene's nodes
// flattened to world coordinates, triangle primitives (strips and fans too)
// with POSITION, NORMAL, TEXCOORD_n, COLOR_0, and the base colour factor and
// texture of metallic-roughness or specular-glossiness materials. Points,
// lines, skins, morph targets, animations and cameras are ignored. Written:
// one node and mesh per part, metallic-roughness materials (not metallic, as
// OBJ / 3MF colours are meant), textures as PNG or JPEG. A .gltf is written as
// one self-contained file: the buffer is a base64 data URI.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"io/fs"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	_ "image/gif"
	_ "image/jpeg"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

const millimetresPerMetre = 1000

// gltfFS reads a glTF's separate buffers only from the model's folder (resolveReference).
type gltfFS struct{ dir string }

type missingFileError struct{ name string }

func (e *missingFileError) Error() string { return e.name + " is not in the model's folder." }

func (g gltfFS) Open(name string) (fs.File, error) {
	path := resolveReference(g.dir, name)
	if path == "" {
		return nil, &missingFileError{name}
	}
	return os.Open(path)
}

func (g gltfFS) ReadFile(name string) ([]byte, error) {
	path := resolveReference(g.dir, name)
	if path == "" {
		return nil, &missingFileError{name}
	}
	return os.ReadFile(path)
}

func describeGltf(message string) string {
	switch {
	case strings.Contains(message, "KHR_draco_mesh_compression"):
		return "The glTF model is Draco-compressed (KHR_draco_mesh_compression), which Filee cannot decode."
	case strings.Contains(message, "meshopt_compression"):
		return "The glTF model is meshopt-compressed (EXT_meshopt_compression), which Filee cannot decode."
	}
	return "The file is not a valid glTF model: " + message
}

func readGltf(ctx context.Context, path string, progress func(float64)) (*Model, error) {
	abs, _ := filepath.Abs(path)
	dir := filepath.Dir(abs)
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	doc := new(gltf.Document)
	if err := gltf.NewDecoderFS(f, gltfFS{dir}).Decode(doc); err != nil {
		var missing *missingFileError
		if errors.As(err, &missing) {
			return nil, fmt.Errorf("A file the glTF model refers to is missing: %s", filepath.Base(missing.name))
		}
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			return nil, fmt.Errorf("A file the glTF model refers to is missing: %s", filepath.Base(pathErr.Path))
		}
		return nil, errors.New(describeGltf(strings.TrimPrefix(err.Error(), "gltf: ")))
	}
	if progress != nil {
		progress(0.4)
	}
	for _, e := range doc.ExtensionsUsed {
		if e == "KHR_draco_mesh_compression" || e == "EXT_meshopt_compression" || e == "KHR_meshopt_compression" {
			return nil, errors.New(describeGltf(e))
		}
	}
	model, err := readGltfSceneSafe(ctx, doc, dir)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		return nil, fmt.Errorf("The glTF model could not be read: %w", err)
	}
	return model, nil
}

func nodeLocal(n *gltf.Node) mat4 {
	m := n.MatrixOrDefault()
	if m != gltf.DefaultMatrix {
		// Column-major for column vectors is row-major for row vectors.
		var r mat4
		for i := 0; i < 16; i++ {
			r[i/4][i%4] = float32(m[i])
		}
		return r
	}
	q := n.Rotation
	x, y, z, w := float32(q[0]), float32(q[1]), float32(q[2]), float32(q[3])
	rot := mat4{
		{1 - 2*(y*y+z*z), 2 * (x*y + z*w), 2 * (x*z - y*w), 0},
		{2 * (x*y - z*w), 1 - 2*(x*x+z*z), 2 * (y*z + x*w), 0},
		{2 * (x*z + y*w), 2 * (y*z - x*w), 1 - 2*(x*x+y*y), 0},
		{0, 0, 0, 1},
	}
	s := n.Scale
	t := n.Translation
	return scaleMatrix(float32(s[0]), float32(s[1]), float32(s[2])).mul(rot).mul(translation(vec3{float32(t[0]), float32(t[1]), float32(t[2])}))
}

// readGltfSceneSafe turns a panic of the accessor reader on a damaged file into an error.
func readGltfSceneSafe(ctx context.Context, doc *gltf.Document, dir string) (m *Model, err error) {
	defer func() {
		if r := recover(); r != nil {
			m, err = nil, fmt.Errorf("%v", r)
		}
	}()
	return readGltfScene(ctx, doc, dir)
}

func readGltfScene(ctx context.Context, doc *gltf.Document, dir string) (*Model, error) {
	model := &Model{}
	materials := map[int]int{}
	var roots []int
	switch {
	case doc.Scene != nil && *doc.Scene >= 0 && *doc.Scene < len(doc.Scenes):
		roots = doc.Scenes[*doc.Scene].Nodes
	case len(doc.Scenes) > 0:
		roots = doc.Scenes[0].Nodes
	default:
		child := map[int]bool{}
		for _, n := range doc.Nodes {
			for _, c := range n.Children {
				child[c] = true
			}
		}
		for i := range doc.Nodes {
			if !child[i] {
				roots = append(roots, i)
			}
		}
	}
	scale := uniformScale(millimetresPerMetre)
	visited := 0
	var walk func(index int, parent mat4, depth int) error
	walk = func(index int, parent mat4, depth int) error {
		if index < 0 || index >= len(doc.Nodes) || depth > 256 {
			return nil
		}
		visited++
		if visited > 1<<20 {
			return errors.New("the node hierarchy is too large")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		node := doc.Nodes[index]
		world := nodeLocal(node).mul(parent)
		if node.Mesh != nil && *node.Mesh >= 0 && *node.Mesh < len(doc.Meshes) {
			mesh := doc.Meshes[*node.Mesh]
			for _, prim := range mesh.Primitives {
				switch prim.Mode {
				case gltf.PrimitiveTriangles, gltf.PrimitiveTriangleStrip, gltf.PrimitiveTriangleFan:
				default:
					continue
				}
				part, err := readGltfPrimitive(doc, prim, model, materials, dir)
				if err != nil {
					return err
				}
				if part == nil {
					continue
				}
				switch {
				case mesh.Name != "":
					part.Name = mesh.Name
				case node.Name != "":
					part.Name = node.Name
				default:
					part.Name = fmt.Sprintf("Mesh %d", *node.Mesh+1)
				}
				part.Transform(world.mul(scale))
				model.Parts = append(model.Parts, part)
			}
		}
		for _, c := range node.Children {
			if err := walk(c, world, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	for _, r := range roots {
		if err := walk(r, identity, 0); err != nil {
			return nil, err
		}
	}
	return model, nil
}

// maxSparseElements bounds an accessor without a buffer view (all zeros, maybe sparse).
const maxSparseElements = 1 << 26

// checkAccessorSize refuses an accessor whose count does not fit its buffer
// view: the reader allocates count elements before it reads, so a hostile
// count would ask for gigabytes.
func checkAccessorSize(doc *gltf.Document, acr *gltf.Accessor) error {
	if acr.Count < 0 {
		return errors.New("an accessor has a negative count")
	}
	if acr.BufferView == nil {
		if acr.Count > maxSparseElements {
			return errors.New("an accessor is too large")
		}
		return nil
	}
	if *acr.BufferView < 0 || *acr.BufferView >= len(doc.BufferViews) {
		return fmt.Errorf("buffer view %d does not exist", *acr.BufferView)
	}
	bv := doc.BufferViews[*acr.BufferView]
	elem := int64(acr.ComponentType.ByteSize()) * int64(acr.Type.Components())
	if acr.Count == 0 {
		return nil
	}
	stride := max(int64(bv.ByteStride), elem)
	if int64(acr.ByteOffset)+stride*int64(acr.Count-1)+elem > int64(bv.ByteLength) {
		return errors.New("an accessor does not fit its buffer view")
	}
	return nil
}

// accessorFloats reads an accessor as float32 values (comps per element), normalizing integers when the accessor says so.
func accessorFloats(doc *gltf.Document, index int) ([]float32, int, int, error) {
	if index < 0 || index >= len(doc.Accessors) {
		return nil, 0, 0, fmt.Errorf("accessor %d does not exist", index)
	}
	acr := doc.Accessors[index]
	if err := checkAccessorSize(doc, acr); err != nil {
		return nil, 0, 0, err
	}
	data, err := modeler.ReadAccessor(doc, acr, nil)
	if err != nil {
		return nil, 0, 0, err
	}
	switch d := data.(type) {
	case [][3]float32:
		out := make([]float32, 0, len(d)*3)
		for _, e := range d {
			out = append(out, e[0], e[1], e[2])
		}
		return out, 3, len(d), nil
	case [][2]float32:
		out := make([]float32, 0, len(d)*2)
		for _, e := range d {
			out = append(out, e[0], e[1])
		}
		return out, 2, len(d), nil
	case [][4]float32:
		out := make([]float32, 0, len(d)*4)
		for _, e := range d {
			out = append(out, e[0], e[1], e[2], e[3])
		}
		return out, 4, len(d), nil
	}
	v := reflect.ValueOf(data)
	if v.Kind() != reflect.Slice {
		return nil, 0, 0, errors.New("unexpected accessor data")
	}
	count := v.Len()
	comps := 1
	if count > 0 && v.Index(0).Kind() == reflect.Array {
		comps = v.Index(0).Len()
	}
	out := make([]float32, 0, count*comps)
	conv := func(e reflect.Value) float32 {
		switch e.Kind() {
		case reflect.Float32, reflect.Float64:
			return float32(e.Float())
		case reflect.Uint8:
			if acr.Normalized {
				return float32(e.Uint()) / 255
			}
			return float32(e.Uint())
		case reflect.Uint16:
			if acr.Normalized {
				return float32(e.Uint()) / 65535
			}
			return float32(e.Uint())
		case reflect.Uint32:
			return float32(e.Uint())
		case reflect.Int8:
			if acr.Normalized {
				return max(float32(e.Int())/127, -1)
			}
			return float32(e.Int())
		case reflect.Int16:
			if acr.Normalized {
				return max(float32(e.Int())/32767, -1)
			}
			return float32(e.Int())
		}
		return 0
	}
	for i := 0; i < count; i++ {
		e := v.Index(i)
		if e.Kind() == reflect.Array {
			for k := 0; k < comps; k++ {
				out = append(out, conv(e.Index(k)))
			}
		} else {
			out = append(out, conv(e))
		}
	}
	return out, comps, count, nil
}

func readGltfPrimitive(doc *gltf.Document, prim *gltf.Primitive, model *Model, materials map[int]int, dir string) (*Part, error) {
	posIndex, ok := prim.Attributes[gltf.POSITION]
	if !ok {
		return nil, nil
	}
	positions, comps, count, err := accessorFloats(doc, posIndex)
	if err != nil {
		return nil, err
	}
	if comps != 3 {
		return nil, errors.New("POSITION is not a VEC3")
	}
	part := newPart("")
	part.Positions = positions

	if ni, ok := prim.Attributes[gltf.NORMAL]; ok {
		if normals, c, n, err := accessorFloats(doc, ni); err == nil && c == 3 && n == count {
			part.Normals = normals
		}
	}

	texCoordSet := 0
	if prim.Material != nil && *prim.Material >= 0 && *prim.Material < len(doc.Materials) {
		mi := *prim.Material
		index, ok := materials[mi]
		if !ok {
			index = len(model.Materials)
			m, set, err := readGltfMaterial(doc, doc.Materials[mi], index, dir)
			if err != nil {
				return nil, err
			}
			model.Materials = append(model.Materials, m)
			materials[mi] = index
			_ = set
		}
		part.Material = index
		texCoordSet = gltfBaseColorTexCoord(doc.Materials[mi])
	}

	uvIndex, ok := prim.Attributes[fmt.Sprintf("TEXCOORD_%d", texCoordSet)]
	if !ok {
		uvIndex, ok = prim.Attributes["TEXCOORD_0"]
	}
	if ok {
		if uvs, c, n, err := accessorFloats(doc, uvIndex); err == nil && c == 2 && n == count {
			for i := 1; i < len(uvs); i += 2 {
				uvs[i] = 1 - uvs[i]
			}
			part.TexCoords = uvs
		}
	}

	if ci, ok := prim.Attributes["COLOR_0"]; ok {
		if colors, c, n, err := accessorFloats(doc, ci); err == nil && (c == 3 || c == 4) && n == count {
			part.Colors = make([]float32, 0, count*4)
			for v := 0; v < count; v++ {
				col := vec4{colors[v*c], colors[v*c+1], colors[v*c+2], 1}
				if c == 4 {
					col.W = colors[v*c+3]
				}
				col = linearToSrgb4(col)
				part.Colors = append(part.Colors, col.X, col.Y, col.Z, col.W)
			}
		}
	}

	var indices []uint32
	if prim.Indices != nil {
		if *prim.Indices < 0 || *prim.Indices >= len(doc.Accessors) {
			return nil, fmt.Errorf("accessor %d does not exist", *prim.Indices)
		}
		if err := checkAccessorSize(doc, doc.Accessors[*prim.Indices]); err != nil {
			return nil, err
		}
		if indices, err = modeler.ReadIndices(doc, doc.Accessors[*prim.Indices], nil); err != nil {
			return nil, err
		}
	} else {
		indices = make([]uint32, count)
		for i := range indices {
			indices[i] = uint32(i)
		}
	}
	add := func(a, b, c uint32) {
		part.AddTriangle(int(a), int(b), int(c))
	}
	switch prim.Mode {
	case gltf.PrimitiveTriangleStrip:
		for i := 0; i+2 < len(indices); i++ {
			if i%2 == 0 {
				add(indices[i], indices[i+1], indices[i+2])
			} else {
				add(indices[i+1], indices[i], indices[i+2])
			}
		}
	case gltf.PrimitiveTriangleFan:
		for i := 1; i+1 < len(indices); i++ {
			add(indices[0], indices[i], indices[i+1])
		}
	default:
		for i := 0; i+2 < len(indices); i += 3 {
			add(indices[i], indices[i+1], indices[i+2])
		}
	}
	return part, nil
}

// specGloss is the part of KHR_materials_pbrSpecularGlossiness read here.
type specGloss struct {
	DiffuseFactor  *[4]float64       `json:"diffuseFactor"`
	DiffuseTexture *gltf.TextureInfo `json:"diffuseTexture"`
}

func gltfSpecGloss(m *gltf.Material) *specGloss {
	raw, ok := m.Extensions["KHR_materials_pbrSpecularGlossiness"]
	if !ok {
		return nil
	}
	var b []byte
	switch r := raw.(type) {
	case json.RawMessage:
		b = r
	default:
		b, _ = json.Marshal(r)
	}
	var s specGloss
	if json.Unmarshal(b, &s) != nil {
		return nil
	}
	return &s
}

// gltfBaseColor returns the base colour (or diffuse) channel: factor and texture.
func gltfBaseColor(m *gltf.Material) (vec4, *gltf.TextureInfo, bool) {
	if pbr := m.PBRMetallicRoughness; pbr != nil {
		c := vec4{1, 1, 1, 1}
		if pbr.BaseColorFactor != nil {
			f := pbr.BaseColorFactor
			c = vec4{float32(f[0]), float32(f[1]), float32(f[2]), float32(f[3])}
		}
		return c, pbr.BaseColorTexture, true
	}
	if sg := gltfSpecGloss(m); sg != nil {
		c := vec4{1, 1, 1, 1}
		if sg.DiffuseFactor != nil {
			f := sg.DiffuseFactor
			c = vec4{float32(f[0]), float32(f[1]), float32(f[2]), float32(f[3])}
		}
		return c, sg.DiffuseTexture, true
	}
	// A material without pbrMetallicRoughness still has the default base colour channel.
	return vec4{1, 1, 1, 1}, nil, true
}

func gltfBaseColorTexCoord(m *gltf.Material) int {
	if _, tex, ok := gltfBaseColor(m); ok && tex != nil {
		return tex.TexCoord
	}
	return 0
}

func readGltfMaterial(doc *gltf.Document, source *gltf.Material, index int, dir string) (*Material, bool, error) {
	name := source.Name
	if name == "" {
		name = fmt.Sprintf("Material %d", index+1)
	}
	m := newMaterial(name)
	m.DoubleSided = source.DoubleSided
	color, tex, ok := gltfBaseColor(source)
	if !ok {
		return m, false, nil
	}
	m.BaseColor = color
	if tex != nil && tex.Index >= 0 && tex.Index < len(doc.Textures) {
		t := doc.Textures[tex.Index]
		if t.Source != nil && *t.Source >= 0 && *t.Source < len(doc.Images) {
			img := doc.Images[*t.Source]
			data, ext, err := gltfImageData(doc, img, dir)
			if err != nil {
				return nil, false, err
			}
			if len(data) > 0 {
				texName := fmt.Sprintf("texture%d", index+1)
				if img.Name != "" {
					texName = strings.TrimSuffix(img.Name, filepath.Ext(img.Name))
				} else if img.URI != "" && !strings.HasPrefix(img.URI, "data:") {
					if u, err := url.PathUnescape(img.URI); err == nil {
						base := filepath.Base(filepath.FromSlash(u))
						texName = strings.TrimSuffix(base, filepath.Ext(base))
					}
				}
				m.BaseColorTexture = &Texture{Data: data, Extension: sniff(data, ext), Name: texName}
			}
		}
	}
	return m, true, nil
}

func gltfImageData(doc *gltf.Document, img *gltf.Image, dir string) ([]byte, string, error) {
	ext := "png"
	if img.MimeType == "image/jpeg" {
		ext = "jpg"
	}
	if img.BufferView != nil {
		if *img.BufferView < 0 || *img.BufferView >= len(doc.BufferViews) {
			return nil, "", fmt.Errorf("buffer view %d does not exist", *img.BufferView)
		}
		data, err := modeler.ReadBufferView(doc, doc.BufferViews[*img.BufferView])
		if err != nil {
			return nil, "", err
		}
		return bytes.Clone(data), ext, nil
	}
	if img.URI == "" {
		return nil, ext, nil
	}
	if strings.HasPrefix(img.URI, "data:") {
		comma := strings.IndexByte(img.URI, ',')
		if comma < 0 || !strings.Contains(img.URI[:comma], ";base64") {
			return nil, "", errors.New("an embedded picture is not base64")
		}
		data, err := base64.StdEncoding.DecodeString(img.URI[comma+1:])
		if err != nil {
			return nil, "", errors.New("an embedded picture is not valid base64")
		}
		return data, ext, nil
	}
	name, err := url.PathUnescape(img.URI)
	if err != nil {
		name = img.URI
	}
	path := resolveReference(dir, name)
	if path == "" {
		return nil, "", fmt.Errorf("A file the glTF model refers to is missing: %s", filepath.Base(filepath.FromSlash(name)))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	return data, strings.TrimPrefix(filepath.Ext(path), "."), nil
}

// gltfWriter collects one buffer with all the data, 4-byte aligned.
type gltfWriter struct {
	doc *gltf.Document
	buf []byte
}

func (w *gltfWriter) view(data []byte, target gltf.Target) int {
	for len(w.buf)%4 != 0 {
		w.buf = append(w.buf, 0)
	}
	offset := len(w.buf)
	w.buf = append(w.buf, data...)
	w.doc.BufferViews = append(w.doc.BufferViews, &gltf.BufferView{Buffer: 0, ByteOffset: offset, ByteLength: len(data), Target: target})
	return len(w.doc.BufferViews) - 1
}

func floatBytes(values []float32) []byte {
	b := make([]byte, 0, len(values)*4)
	for _, v := range values {
		b = binary.LittleEndian.AppendUint32(b, math.Float32bits(v))
	}
	return b
}

func (w *gltfWriter) accessor(values []float32, typ gltf.AccessorType, comps int, bounds bool) int {
	view := w.view(floatBytes(values), gltf.TargetArrayBuffer)
	a := &gltf.Accessor{BufferView: gltf.Index(view), ComponentType: gltf.ComponentFloat, Count: len(values) / comps, Type: typ}
	if bounds && len(values) >= comps {
		lo := make([]float64, comps)
		hi := make([]float64, comps)
		for k := 0; k < comps; k++ {
			lo[k], hi[k] = math.Inf(1), math.Inf(-1)
		}
		for i := 0; i+comps <= len(values); i += comps {
			for k := 0; k < comps; k++ {
				v := float64(values[i+k])
				lo[k], hi[k] = math.Min(lo[k], v), math.Max(hi[k], v)
			}
		}
		a.Min, a.Max = lo, hi
	}
	w.doc.Accessors = append(w.doc.Accessors, a)
	return len(w.doc.Accessors) - 1
}

// writeGltf writes a GLB, or a self-contained .gltf when binary is false.
func writeGltf(ctx context.Context, m *Model, out io.Writer, binaryFile bool) error {
	doc := &gltf.Document{Asset: gltf.Asset{Version: "2.0", Generator: "Filee"}, Scene: gltf.Index(0)}
	scene := &gltf.Scene{Name: "Scene"}
	doc.Scenes = []*gltf.Scene{scene}
	w := &gltfWriter{doc: doc}

	images := map[*Texture]int{}
	for _, mat := range m.Materials {
		gm, err := writeGltfMaterial(w, mat, images)
		if err != nil {
			return err
		}
		doc.Materials = append(doc.Materials, gm)
	}
	for _, p := range m.Parts {
		if err := ctx.Err(); err != nil {
			return err
		}
		count := p.VertexCount()
		attrs := gltf.PrimitiveAttributes{}
		positions := make([]float32, len(p.Positions))
		for i, v := range p.Positions {
			positions[i] = v / millimetresPerMetre
		}
		attrs[gltf.POSITION] = w.accessor(positions, gltf.AccessorVec3, 3, true)
		if p.Normals != nil {
			normals := make([]float32, 0, count*3)
			for v := 0; v < count; v++ {
				n := p.Normal(v)
				if n.lengthSquared() > 0 {
					n = n.normalize()
				} else {
					n = vec3{0, 1, 0}
				}
				normals = append(normals, n.X, n.Y, n.Z)
			}
			attrs[gltf.NORMAL] = w.accessor(normals, gltf.AccessorVec3, 3, false)
		}
		if p.TexCoords != nil {
			uvs := make([]float32, 0, count*2)
			for v := 0; v < count; v++ {
				uv := p.TexCoord(v)
				uvs = append(uvs, uv.X, 1-uv.Y)
			}
			attrs[gltf.TEXCOORD_0] = w.accessor(uvs, gltf.AccessorVec2, 2, false)
		}
		if p.Colors != nil {
			colors := make([]float32, 0, count*4)
			for v := 0; v < count; v++ {
				c := srgbToLinear4(p.Color(v)).clamp01()
				colors = append(colors, c.X, c.Y, c.Z, c.W)
			}
			attrs[gltf.COLOR_0] = w.accessor(colors, gltf.AccessorVec4, 4, false)
		}
		wide := count > math.MaxUint16
		var ib []byte
		if wide {
			ib = make([]byte, 0, len(p.Indices)*4)
			for _, i := range p.Indices {
				ib = binary.LittleEndian.AppendUint32(ib, uint32(i))
			}
		} else {
			ib = make([]byte, 0, len(p.Indices)*2)
			for _, i := range p.Indices {
				ib = binary.LittleEndian.AppendUint16(ib, uint16(i))
			}
		}
		iv := w.view(ib, gltf.TargetElementArrayBuffer)
		ct := gltf.ComponentUshort
		if wide {
			ct = gltf.ComponentUint
		}
		doc.Accessors = append(doc.Accessors, &gltf.Accessor{BufferView: gltf.Index(iv), ComponentType: ct, Count: len(p.Indices), Type: gltf.AccessorScalar})
		prim := &gltf.Primitive{Attributes: attrs, Indices: gltf.Index(len(doc.Accessors) - 1), Mode: gltf.PrimitiveTriangles}
		if p.Material >= 0 && p.Material < len(doc.Materials) {
			prim.Material = gltf.Index(p.Material)
		}
		doc.Meshes = append(doc.Meshes, &gltf.Mesh{Name: p.Name, Primitives: []*gltf.Primitive{prim}})
		doc.Nodes = append(doc.Nodes, &gltf.Node{Name: p.Name, Mesh: gltf.Index(len(doc.Meshes) - 1),
			Matrix: gltf.DefaultMatrix, Rotation: gltf.DefaultRotation, Scale: gltf.DefaultScale})
		scene.Nodes = append(scene.Nodes, len(doc.Nodes)-1)
	}
	for len(w.buf)%4 != 0 {
		w.buf = append(w.buf, 0)
	}
	if len(w.buf) > 0 {
		doc.Buffers = []*gltf.Buffer{{ByteLength: len(w.buf), Data: w.buf}}
	}
	enc := gltf.NewEncoder(out)
	enc.AsBinary = binaryFile
	if !binaryFile {
		enc.SetJSONIndent("", "  ")
	}
	return enc.Encode(doc)
}

func writeGltfMaterial(w *gltfWriter, src *Material, images map[*Texture]int) (*gltf.Material, error) {
	color := src.BaseColor.clamp01()
	factor := [4]float64{float64(color.X), float64(color.Y), float64(color.Z), float64(color.W)}
	metallic, roughness := 0.0, 0.8
	gm := &gltf.Material{
		Name:        src.Name,
		DoubleSided: src.DoubleSided,
		// OBJ, 3MF and PLY colours describe plastic-like surfaces; glTF's default would make them fully metallic.
		PBRMetallicRoughness: &gltf.PBRMetallicRoughness{BaseColorFactor: &factor, MetallicFactor: &metallic, RoughnessFactor: &roughness},
	}
	if color.W < 1 {
		gm.AlphaMode = gltf.AlphaBlend
	}
	if t := src.BaseColorTexture; t != nil {
		image, ok := images[t]
		if !ok {
			data, mime, err := textureForGltf(t)
			if err != nil {
				return nil, err
			}
			view := w.view(data, gltf.TargetNone)
			w.doc.Images = append(w.doc.Images, &gltf.Image{Name: t.Name, MimeType: mime, BufferView: gltf.Index(view)})
			if len(w.doc.Samplers) == 0 {
				w.doc.Samplers = []*gltf.Sampler{{WrapS: gltf.WrapRepeat, WrapT: gltf.WrapRepeat}}
			}
			w.doc.Textures = append(w.doc.Textures, &gltf.Texture{Sampler: gltf.Index(0), Source: gltf.Index(len(w.doc.Images) - 1)})
			image = len(w.doc.Textures) - 1
			images[t] = image
		}
		gm.PBRMetallicRoughness.BaseColorTexture = &gltf.TextureInfo{Index: image}
	}
	return gm, nil
}

// textureForGltf: glTF viewers read PNG and JPEG; other pictures (BMP, GIF, TIFF, WebP) are converted to PNG.
func textureForGltf(t *Texture) ([]byte, string, error) {
	switch t.Extension {
	case "png":
		return t.Data, "image/png", nil
	case "jpg":
		return t.Data, "image/jpeg", nil
	}
	img, _, err := image.Decode(bytes.NewReader(t.Data))
	if err != nil {
		return nil, "", fmt.Errorf("The texture \"%s.%s\" cannot be read: %v", t.Name, t.Extension, err)
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return nil, "", err
	}
	return b.Bytes(), "image/png", nil
}
