package model3d

// Autodesk FBX, read only: binary (6.x and 7.x) and ASCII files through
// Filee's own parser (fbxparse.go), no FBX SDK. Read: mesh geometry with
// polygons triangulated, the first normal, UV, colour and material layer (by
// polygon vertex, by control point, by polygon or all the same; direct or
// indexed); the model hierarchy from the connections, each model's
// translation, rotation (any Euler order, pre- and post-rotation, pivots and
// offsets) and scaling, and the geometric transform of its mesh; material
// diffuse colour and opacity; the diffuse texture, embedded (Video Content) or
// a file next to the model. GlobalSettings' axes and UnitScaleFactor
// (centimetres per unit) turn the scene Y up and into millimetres. Animation,
// skins, blend shapes, NURBS, cameras and lights are skipped: the model is
// read in its rest pose.

import (
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const millimetresPerCentimetre = 10

func readFbx(ctx context.Context, path string, progress func(float64)) (*Model, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	abs, _ := filepath.Abs(path)
	m, err := readFbxData(ctx, data, filepath.Dir(abs))
	if err == nil && progress != nil {
		progress(1)
	}
	return m, err
}

// readFbxData reads the FBX file data; texture files are looked for in directory.
func readFbxData(ctx context.Context, data []byte, directory string) (model *Model, err error) {
	defer func() {
		if r := recover(); r != nil {
			model, err = nil, fmt.Errorf("The FBX model could not be read: %v", r)
		}
	}()
	document, err := parseFbx(ctx, data)
	if err != nil {
		return nil, err
	}
	return newFbxScene(document, directory).build(ctx)
}

type fbxTextureLink struct {
	texture string
	exact   bool
}

type fbxScene struct {
	directory     string
	settings      *fbxNode
	ordered       []string
	objects       map[string]*fbxNode
	templates     map[string]*fbxNode
	parents       map[string]string
	geometries    map[string][]*fbxNode
	materials     map[string][]string
	textures      map[string]fbxTextureLink
	videos        map[string]string
	world         map[string]mat4
	materialIndex map[string]int
	loaded        map[string]*Texture
}

// fbxKey is an object's id (FBX 7) or its full name (FBX 6, "Model::Cube").
func fbxKey(v any) (string, bool) {
	switch x := v.(type) {
	case int64:
		return strconv.FormatInt(x, 10), true
	case string:
		return x, true
	}
	return "", false
}

func newFbxScene(document *fbxNode, directory string) *fbxScene {
	s := &fbxScene{
		directory: directory, settings: document.child("GlobalSettings"),
		objects: map[string]*fbxNode{}, templates: map[string]*fbxNode{}, parents: map[string]string{},
		geometries: map[string][]*fbxNode{}, materials: map[string][]string{}, textures: map[string]fbxTextureLink{},
		videos: map[string]string{}, world: map[string]mat4{}, materialIndex: map[string]int{}, loaded: map[string]*Texture{},
	}
	if objects := document.child("Objects"); objects != nil {
		for _, node := range objects.children {
			if len(node.properties) == 0 {
				continue
			}
			if key, ok := fbxKey(node.properties[0]); ok {
				if _, dup := s.objects[key]; !dup {
					s.objects[key] = node
					s.ordered = append(s.ordered, key)
				}
			}
		}
	}
	// Property templates: what an object of the type has where it does not say
	// (the FBX SDK writes only what differs from them).
	for _, t := range document.child("Definitions").childrenNamed("ObjectType") {
		if name, ok := t.text(0); ok {
			if tmpl := t.child("PropertyTemplate"); tmpl != nil {
				if _, dup := s.templates[name]; !dup {
					s.templates[name] = tmpl
				}
			}
		}
	}
	if connections := document.child("Connections"); connections != nil {
		for _, link := range connections.children {
			if (link.name != "C" && link.name != "Connect") || len(link.properties) < 3 {
				continue
			}
			childKey, ok1 := fbxKey(link.properties[1])
			parentKey, ok2 := fbxKey(link.properties[2])
			if !ok1 || !ok2 {
				continue
			}
			child, ok1 := s.objects[childKey]
			parent, ok2 := s.objects[parentKey]
			if !ok1 || !ok2 {
				continue
			}
			property, hasProperty := link.text(3)
			switch {
			case child.name == "Geometry" && parent.name == "Model":
				s.geometries[parentKey] = append(s.geometries[parentKey], child)
			case child.name == "Model" && parent.name == "Model":
				if _, dup := s.parents[childKey]; !dup {
					s.parents[childKey] = parentKey
				}
			case child.name == "Material" && parent.name == "Model":
				s.materials[parentKey] = append(s.materials[parentKey], childKey)
			case child.name == "Texture" && parent.name == "Material" && fbxIsDiffuse(property, hasProperty):
				// "DiffuseColor" wins over the other colour channels a material may have a texture on.
				exact := !hasProperty || property == "DiffuseColor"
				if existing, ok := s.textures[parentKey]; !ok || (exact && !existing.exact) {
					s.textures[parentKey] = fbxTextureLink{childKey, exact}
				}
			case child.name == "Video" && parent.name == "Texture":
				if _, dup := s.videos[parentKey]; !dup {
					s.videos[parentKey] = childKey
				}
			}
		}
	}
	return s
}

func fbxIsDiffuse(property string, has bool) bool {
	if !has {
		return true
	}
	l := strings.ToLower(property)
	return strings.Contains(l, "diffuse") || strings.Contains(l, "basecolor") || strings.Contains(l, "base_color")
}

func (s *fbxScene) build(ctx context.Context) (*Model, error) {
	model := &Model{}
	scene := s.axisConversion().mul(uniformScale(s.unitScale() * millimetresPerCentimetre))
	for _, key := range s.ordered {
		node := s.objects[key]
		if node.name != "Model" {
			continue
		}
		// FBX 6 keeps the mesh in the model itself.
		geometries, ok := s.geometries[key]
		if !ok {
			if node.child("PolygonVertexIndex") == nil {
				continue
			}
			geometries = []*fbxNode{node}
		}
		transform := s.geometric(node).mul(s.worldOf(key, 0)).mul(scene)
		var slots []int
		for _, m := range s.materials[key] {
			slots = append(slots, s.materialIndexOf(model, m))
		}
		for _, g := range geometries {
			if err := s.addMesh(ctx, model, g, fbxNameOf(node), transform, slots); err != nil {
				return nil, err
			}
		}
	}
	return model, nil
}

// axisConversion turns the file's axes to Y up (right X, up Y, front Z), right-handed as FBX always is.
func (s *fbxScene) axisConversion() mat4 {
	up := int(s.numberOr(s.settings, "UpAxis", 1))
	coord := int(s.numberOr(s.settings, "CoordAxis", 0))
	if up < 0 || up > 2 {
		up = 1
	}
	if coord < 0 || coord > 2 || coord == up {
		coord = 0
		if up == 0 {
			coord = 1
		}
	}
	front := 3 - up - coord
	m := identity
	m[0][0], m[1][1], m[2][2] = 0, 0, 0
	m[coord][0] = 1
	if v, ok := s.number(s.settings, "CoordAxisSign"); ok && v < 0 {
		m[coord][0] = -1
	}
	m[up][1] = 1
	if v, ok := s.number(s.settings, "UpAxisSign"); ok && v < 0 {
		m[up][1] = -1
	}
	m[front][2] = 1
	// The front axis follows from the other two: its sign in the file is not always consistent.
	if m.determinant() < 0 {
		m[front][2] = -1
	}
	return m
}

// unitScale is centimetres per file unit (1 when missing or nonsense).
func (s *fbxScene) unitScale() float32 {
	v := s.numberOr(s.settings, "UnitScaleFactor", 1)
	if v > 0 && !math.IsInf(v, 0) && !math.IsNaN(v) {
		return float32(v)
	}
	return 1
}

func (s *fbxScene) worldOf(key string, depth int) mat4 {
	if w, ok := s.world[key]; ok {
		return w
	}
	w := s.local(s.objects[key])
	// A cycle in the connections stops at a depth no real hierarchy has.
	if parent, ok := s.parents[key]; ok && depth < 512 {
		w = w.mul(s.worldOf(parent, depth+1))
	}
	s.world[key] = w
	return w
}

// local is the FBX SDK's local transform, T · Roff · Rp · Rpre · R · Rpost⁻¹ ·
// Rp⁻¹ · Soff · Sp · S · Sp⁻¹ for column vectors, written in reverse for row vectors.
func (s *fbxScene) local(node *fbxNode) mat4 {
	vec := func(name string, fallback vec3) vec3 {
		if v, ok := s.vector(node, name); ok {
			return v
		}
		return fallback
	}
	zero, one := vec3{}, vec3{1, 1, 1}
	t := vec("Lcl Translation", zero)
	rotation := vec("Lcl Rotation", zero)
	scaling := vec("Lcl Scaling", one)
	pre := vec("PreRotation", zero)
	post := vec("PostRotation", zero)
	rotationOffset := vec("RotationOffset", zero)
	rotationPivot := vec("RotationPivot", zero)
	scalingOffset := vec("ScalingOffset", zero)
	scalingPivot := vec("ScalingPivot", zero)
	order := int(s.numberOr(node, "RotationOrder", 0))
	inversePost, ok := fbxEuler(post, 0).invert()
	if !ok {
		inversePost = mat4{}
	}
	return translation(scalingPivot.neg()).
		mul(scaleVec(scaling)).
		mul(translation(scalingPivot.add(scalingOffset).sub(rotationPivot))).
		mul(inversePost).
		mul(fbxEuler(rotation, order)).
		mul(fbxEuler(pre, 0)).
		mul(translation(rotationPivot.add(rotationOffset).add(t)))
}

// geometric is the mesh's own offset from its model, which children do not inherit.
func (s *fbxScene) geometric(node *fbxNode) mat4 {
	scaleV, ok := s.vector(node, "GeometricScaling")
	if !ok {
		scaleV = vec3{1, 1, 1}
	}
	rot, _ := s.vector(node, "GeometricRotation")
	tr, _ := s.vector(node, "GeometricTranslation")
	return scaleVec(scaleV).mul(fbxEuler(rot, 0)).mul(translation(tr))
}

// fbxEuler: angles in degrees; order is FBX's EFbxRotationOrder (0 XYZ: X first).
func fbxEuler(degrees vec3, order int) mat4 {
	if degrees == (vec3{}) {
		return identity
	}
	const toRadians = math.Pi / 180
	x := rotationX(degrees.X * toRadians)
	y := rotationY(degrees.Y * toRadians)
	z := rotationZ(degrees.Z * toRadians)
	switch order {
	case 1:
		return x.mul(z).mul(y) // XZY
	case 2:
		return y.mul(z).mul(x) // YZX
	case 3:
		return y.mul(x).mul(z) // YXZ
	case 4:
		return z.mul(x).mul(y) // ZXY
	case 5:
		return z.mul(y).mul(x) // ZYX
	}
	return x.mul(y).mul(z) // XYZ (and spheric XYZ)
}

// property finds the property name of an object (Properties70 or FBX 6's Properties60), else of its type's template.
func (s *fbxScene) property(node *fbxNode, name string) *fbxNode {
	if node == nil {
		return nil
	}
	find := func(owner *fbxNode) *fbxNode {
		for _, list := range owner.children {
			if list.name != "Properties70" && list.name != "Properties60" {
				continue
			}
			for _, p := range list.children {
				if p.name == "P" || p.name == "Property" {
					if t, ok := p.text(0); ok && t == name {
						return p
					}
				}
			}
		}
		return nil
	}
	if p := find(node); p != nil {
		return p
	}
	if t, ok := s.templates[node.name]; ok {
		return find(t)
	}
	return nil
}

func fbxNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// numbers are the numbers of a property (after its name, type and flags).
func (s *fbxScene) numbers(node *fbxNode, name string) []float64 {
	var out []float64
	if p := s.property(node, name); p != nil {
		for _, v := range p.properties[1:] {
			if n, ok := fbxNumber(v); ok {
				out = append(out, n)
			}
		}
	}
	return out
}

func (s *fbxScene) number(node *fbxNode, name string) (float64, bool) {
	if v := s.numbers(node, name); len(v) > 0 {
		return v[0], true
	}
	return 0, false
}

func (s *fbxScene) numberOr(node *fbxNode, name string, fallback float64) float64 {
	if v, ok := s.number(node, name); ok {
		return v
	}
	return fallback
}

func (s *fbxScene) vector(node *fbxNode, name string) (vec3, bool) {
	v := s.numbers(node, name)
	if len(v) < 3 {
		return vec3{}, false
	}
	r := vec3{float32(v[0]), float32(v[1]), float32(v[2])}
	if !r.finite() {
		return vec3{}, false
	}
	return r, true
}

// fbxNameOf is "Cube" of "Model::Cube" (ASCII) or "Cube\x00\x01Model" (binary).
func fbxNameOf(node *fbxNode) string {
	name := ""
	for _, p := range node.properties {
		if s, ok := p.(string); ok {
			name = s
			break
		}
	}
	if i := strings.Index(name, "\x00\x01"); i >= 0 {
		return name[:i]
	}
	if i := strings.Index(name, "::"); i >= 0 {
		return name[i+2:]
	}
	return name
}

func (s *fbxScene) materialIndexOf(model *Model, key string) int {
	if i, ok := s.materialIndex[key]; ok {
		return i
	}
	node := s.objects[key]
	color, ok := s.vector(node, "DiffuseColor")
	if !ok {
		if color, ok = s.vector(node, "Diffuse"); !ok {
			color = vec3{0.8, 0.8, 0.8}
		}
	}
	opacity := float32(math.Max(0, math.Min(1, s.numberOr(node, "Opacity", 1))))
	m := newMaterial(fbxNameOf(node))
	m.BaseColor = vec4{clamp01(color.X), clamp01(color.Y), clamp01(color.Z), opacity}
	if t, ok := s.textures[key]; ok {
		m.BaseColorTexture = s.texture(t.texture)
	}
	i := len(model.Materials)
	model.Materials = append(model.Materials, m)
	s.materialIndex[key] = i
	return i
}

// texture is the picture of a texture: embedded in its video, else the file it names next to the model.
func (s *fbxScene) texture(key string) *Texture {
	if t, ok := s.loaded[key]; ok {
		return t
	}
	texture := s.objects[key]
	var video *fbxNode
	if vk, ok := s.videos[key]; ok {
		video = s.objects[vk]
	}
	var references []string
	seen := map[string]bool{}
	for _, n := range []*fbxNode{texture, video} {
		if n == nil {
			continue
		}
		for _, c := range n.children {
			if c.name != "RelativeFilename" && c.name != "FileName" && c.name != "Filename" {
				continue
			}
			if t, ok := c.text(0); ok {
				t = strings.ReplaceAll(t, `\`, "/")
				if t != "" && !seen[t] {
					seen[t] = true
					references = append(references, t)
				}
			}
		}
	}
	name, extension := "", ""
	for _, r := range references {
		base := r[strings.LastIndex(r, "/")+1:]
		if n := strings.TrimSuffix(base, filepath.Ext(base)); n != "" && name == "" {
			name = n
		}
		if e := filepath.Ext(base); e != "" && extension == "" {
			extension = e
		}
	}
	if name == "" {
		name = fbxNameOf(texture)
	}
	var result *Texture
	if content := video.child("Content"); content != nil {
		if data := fbxEmbedded(content); len(data) > 0 {
			result = &Texture{Data: data, Extension: sniff(data, extension), Name: name}
		}
	}
	if result == nil {
		for _, r := range references {
			if file := resolveReference(s.directory, r); file != "" {
				if data, err := os.ReadFile(file); err == nil {
					result = &Texture{Data: data, Extension: sniff(data, filepath.Ext(file)), Name: name}
					break
				}
			}
		}
	}
	s.loaded[key] = result
	return result
}

// fbxEmbedded is Video Content: raw bytes (binary) or base64 strings (ASCII).
func fbxEmbedded(content *fbxNode) []byte {
	if len(content.properties) == 1 {
		if b, ok := content.properties[0].([]byte); ok {
			return b
		}
	}
	var sb strings.Builder
	for _, p := range content.properties {
		if s, ok := p.(string); ok {
			sb.WriteString(s)
		}
	}
	if sb.Len() == 0 {
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(sb.String())
	if err != nil {
		return nil
	}
	return data
}

// fbxDoubles is an array property as float64s, or the numbers of an FBX 6 ASCII node written inline.
func fbxDoubles(node *fbxNode) []float64 {
	if node == nil {
		return nil
	}
	if len(node.properties) == 1 {
		switch a := node.properties[0].(type) {
		case []float64:
			return a
		case []float32:
			out := make([]float64, len(a))
			for i, v := range a {
				out[i] = float64(v)
			}
			return out
		case []int32:
			out := make([]float64, len(a))
			for i, v := range a {
				out[i] = float64(v)
			}
			return out
		case []int64:
			out := make([]float64, len(a))
			for i, v := range a {
				out[i] = float64(v)
			}
			return out
		}
	}
	out := []float64{}
	for _, p := range node.properties {
		if n, ok := fbxNumber(p); ok {
			out = append(out, n)
		}
	}
	return out
}

func clampInt(v float64) int {
	return int(math.Max(math.MinInt32, math.Min(math.MaxInt32, v)))
}

func fbxInts(node *fbxNode) []int {
	if node == nil {
		return nil
	}
	if len(node.properties) == 1 {
		switch a := node.properties[0].(type) {
		case []int32:
			out := make([]int, len(a))
			for i, v := range a {
				out[i] = int(v)
			}
			return out
		case []int64:
			out := make([]int, len(a))
			for i, v := range a {
				out[i] = clampInt(float64(v))
			}
			return out
		}
	}
	d := fbxDoubles(node)
	if d == nil {
		return nil
	}
	out := make([]int, len(d))
	for i, v := range d {
		out[i] = clampInt(v)
	}
	return out
}

type fbxMapping int

const (
	byPolygonVertex fbxMapping = iota
	byControlPoint
	byPolygon
	allSame
)

// fbxLayer is one layer element: which value each polygon corner takes.
type fbxLayer struct {
	values  []float64
	index   []int
	mapping fbxMapping
	width   int
}

// layerOf returns the first usable element of the geometry (layer 0), or nil.
func layerOf(geometry *fbxNode, element, data, indexName string, width int) *fbxLayer {
	for _, node := range geometry.childrenNamed(element) {
		values := fbxDoubles(node.child(data))
		if len(values) == 0 {
			continue
		}
		mt, _ := node.child("MappingInformationType").text(0)
		var mapping fbxMapping
		switch mt {
		case "ByPolygonVertex":
			mapping = byPolygonVertex
		case "ByVertice", "ByVertex", "ByControlPoint":
			mapping = byControlPoint
		case "ByPolygon":
			mapping = byPolygon
		case "AllSame":
			mapping = allSame
		default:
			continue // by edge: no corner value
		}
		rt, _ := node.child("ReferenceInformationType").text(0)
		indexed := rt == "IndexToDirect" || rt == "Index"
		var index []int
		if indexed && indexName != "" {
			index = fbxInts(node.child(indexName))
			if index == nil {
				continue
			}
		}
		return &fbxLayer{values: values, index: index, mapping: mapping, width: width}
	}
	return nil
}

// offset is where the value of a corner starts in the values, or -1 when the file has none for it.
func (l *fbxLayer) offset(corner, controlPoint, polygon int) int {
	i := 0
	switch l.mapping {
	case byPolygonVertex:
		i = corner
	case byControlPoint:
		i = controlPoint
	case byPolygon:
		i = polygon
	}
	if l.index != nil {
		if i >= 0 && i < len(l.index) {
			i = l.index[i]
		} else {
			i = -1
		}
	}
	if i >= 0 && (i+1)*l.width <= len(l.values) {
		return i * l.width
	}
	return -1
}

type fbxVertexKey struct {
	controlPoint int
	normal       vec3
	uv           vec2
	color        vec4
}

type fbxPartBuilder struct {
	part          *Part
	vertices      map[fbxVertexKey]int
	missingNormal bool
}

func (b *fbxPartBuilder) vertex(positions []float64, controlPoint int, normal *vec3, uv *vec2, color *vec4) int {
	if b.part.Normals != nil && (normal == nil || *normal == (vec3{})) {
		b.missingNormal = true
	}
	key := fbxVertexKey{controlPoint: controlPoint}
	if normal != nil {
		key.normal = *normal
	}
	if uv != nil {
		key.uv = *uv
	}
	if color != nil {
		key.color = *color
	}
	if v, ok := b.vertices[key]; ok {
		return v
	}
	pos := vec3{float32(positions[controlPoint*3]), float32(positions[controlPoint*3+1]), float32(positions[controlPoint*3+2])}
	v := b.part.AddVertex(pos, normal, uv, color)
	b.vertices[key] = v
	return v
}

func (s *fbxScene) addMesh(ctx context.Context, model *Model, geometry *fbxNode, name string, transform mat4, slots []int) error {
	positions := fbxDoubles(geometry.child("Vertices"))
	polygons := fbxInts(geometry.child("PolygonVertexIndex"))
	if positions == nil || polygons == nil {
		return nil
	}
	controlPoints := len(positions) / 3
	normals := layerOf(geometry, "LayerElementNormal", "Normals", "NormalsIndex", 3)
	uvs := layerOf(geometry, "LayerElementUV", "UV", "UVIndex", 2)
	colors := layerOf(geometry, "LayerElementColor", "Colors", "ColorIndex", 4)
	// The material layer's values are the indices themselves (into the model's materials).
	materials := layerOf(geometry, "LayerElementMaterial", "Materials", "", 1)

	parts := map[int]*fbxPartBuilder{}
	var order []*fbxPartBuilder
	var corners []int
	start, polygon := 0, 0
	for i := 0; i < len(polygons); i++ {
		if i&0xFFFF == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		// A negative index ends a polygon (it is the last corner's index, bit-inverted); so does the end.
		if polygons[i] >= 0 && i+1 < len(polygons) {
			continue
		}
		slot := 0
		if materials != nil {
			slot = -1
			if o := materials.offset(i, 0, polygon); o >= 0 {
				slot = int(materials.values[o])
			}
		}
		material := -1
		if slot >= 0 && slot < len(slots) {
			material = slots[slot]
		}
		b, ok := parts[material]
		if !ok {
			p := newPart(name)
			p.Material = material
			if normals != nil {
				p.Normals = []float32{}
			}
			if uvs != nil {
				p.TexCoords = []float32{}
			}
			if colors != nil {
				p.Colors = []float32{}
			}
			b = &fbxPartBuilder{part: p, vertices: map[fbxVertexKey]int{}}
			parts[material] = b
			order = append(order, b)
		}
		corners = corners[:0]
		for k := start; k <= i; k++ {
			cp := polygons[k]
			if cp < 0 {
				cp = ^cp
			}
			if cp >= controlPoints {
				return fmt.Errorf("A polygon of the FBX model refers to vertex %d, but the mesh has only %d vertices.", cp+1, controlPoints)
			}
			var np *vec3
			if normals != nil {
				if o := normals.offset(k, cp, polygon); o >= 0 {
					np = &vec3{float32(normals.values[o]), float32(normals.values[o+1]), float32(normals.values[o+2])}
				}
			}
			var up *vec2
			if uvs != nil {
				if o := uvs.offset(k, cp, polygon); o >= 0 {
					up = &vec2{float32(uvs.values[o]), float32(uvs.values[o+1])}
				}
			}
			var cpv *vec4
			if colors != nil {
				if o := colors.offset(k, cp, polygon); o >= 0 {
					cpv = &vec4{float32(colors.values[o]), float32(colors.values[o+1]), float32(colors.values[o+2]), float32(colors.values[o+3])}
				}
			}
			corners = append(corners, b.vertex(positions, cp, np, up, cpv))
		}
		addPolygon(b.part, corners)
		start = i + 1
		polygon++
	}
	for _, b := range order {
		if b.missingNormal {
			b.part.ComputeNormals()
		}
		b.part.Transform(transform)
		model.Parts = append(model.Parts, b.part)
	}
	return nil
}
