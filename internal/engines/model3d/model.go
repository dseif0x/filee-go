package model3d

// The in-memory 3D model every reader produces and every writer takes.
//
// Conventions, chosen once so every reader and writer agrees:
//   - positions are millimetres, Y up (glTF's axes). Unitless files (STL, OBJ,
//     PLY) are read as millimetres; STL, 3MF and PLY are Z up like Blender
//     treats them, OBJ and glTF Y up;
//   - triangles only (polygons are triangulated on reading), counter-clockwise
//     front faces;
//   - UVs have their origin at the bottom left (OBJ, PLY); glTF's top-left
//     origin is flipped on the way in and out;
//   - vertex colours are sRGB 0–1 (as PLY, 3MF and OBJ store them), material
//     colours linear (as glTF stores them);
//   - node hierarchies are flattened: every mesh instance becomes a part in
//     world coordinates.

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strings"
)

// Model is a whole model: parts (meshes) and the materials they use.
type Model struct {
	Parts     []*Part
	Materials []*Material
}

func (m *Model) TriangleCount() int {
	n := 0
	for _, p := range m.Parts {
		n += p.TriangleCount()
	}
	return n
}

func (m *Model) VertexCount() int {
	n := 0
	for _, p := range m.Parts {
		n += p.VertexCount()
	}
	return n
}

// MaterialOf returns the material of part, or nil.
func (m *Model) MaterialOf(p *Part) *Material {
	if p.Material >= 0 && p.Material < len(m.Materials) {
		return m.Materials[p.Material]
	}
	return nil
}

// Bounds of every position, or zeros for an empty model.
func (m *Model) Bounds() (vec3, vec3) {
	inf := float32(math.MaxFloat32)
	lo, hi := vec3{inf, inf, inf}, vec3{-inf, -inf, -inf}
	for _, p := range m.Parts {
		for i := 0; i+2 < len(p.Positions); i += 3 {
			v := vec3{p.Positions[i], p.Positions[i+1], p.Positions[i+2]}
			lo, hi = lo.min(v), hi.max(v)
		}
	}
	if lo.X > hi.X {
		return vec3{}, vec3{}
	}
	return lo, hi
}

// Validate drops parts without triangles and checks every index, so writers
// can trust the data.
func (m *Model) Validate() error {
	kept := m.Parts[:0]
	for _, p := range m.Parts {
		if p.TriangleCount() > 0 {
			kept = append(kept, p)
		}
	}
	m.Parts = kept
	for _, p := range m.Parts {
		if err := p.validate(); err != nil {
			return err
		}
	}
	if len(m.Parts) == 0 {
		return errors.New("The file contains no triangles (only points, lines or nothing at all).")
	}
	return nil
}

// Part is one mesh with per-vertex attributes and a triangle list. A nil
// attribute slice means the part does not have it.
type Part struct {
	Name string
	// Material indexes Model.Materials, or -1.
	Material  int
	Positions []float32 // x, y, z per vertex
	Normals   []float32 // x, y, z per vertex
	TexCoords []float32 // u, v per vertex
	Colors    []float32 // r, g, b, a per vertex (sRGB)
	Indices   []int     // three per triangle
}

func newPart(name string) *Part { return &Part{Name: name, Material: -1} }

func (p *Part) VertexCount() int   { return len(p.Positions) / 3 }
func (p *Part) TriangleCount() int { return len(p.Indices) / 3 }

func (p *Part) Position(v int) vec3 {
	return vec3{p.Positions[v*3], p.Positions[v*3+1], p.Positions[v*3+2]}
}

func (p *Part) Normal(v int) vec3 {
	if p.Normals == nil {
		return vec3{}
	}
	return vec3{p.Normals[v*3], p.Normals[v*3+1], p.Normals[v*3+2]}
}

func (p *Part) TexCoord(v int) vec2 {
	if p.TexCoords == nil {
		return vec2{}
	}
	return vec2{p.TexCoords[v*2], p.TexCoords[v*2+1]}
}

func (p *Part) Color(v int) vec4 {
	if p.Colors == nil {
		return vec4{1, 1, 1, 1}
	}
	return vec4{p.Colors[v*4], p.Colors[v*4+1], p.Colors[v*4+2], p.Colors[v*4+3]}
}

// AddVertex appends a vertex; attributes the part does not have are ignored.
func (p *Part) AddVertex(pos vec3, normal *vec3, uv *vec2, color *vec4) int {
	index := p.VertexCount()
	p.Positions = append(p.Positions, pos.X, pos.Y, pos.Z)
	if p.Normals != nil {
		var n vec3
		if normal != nil {
			n = *normal
		}
		p.Normals = append(p.Normals, n.X, n.Y, n.Z)
	}
	if p.TexCoords != nil {
		var t vec2
		if uv != nil {
			t = *uv
		}
		p.TexCoords = append(p.TexCoords, t.X, t.Y)
	}
	if p.Colors != nil {
		c := vec4{1, 1, 1, 1}
		if color != nil {
			c = *color
		}
		p.Colors = append(p.Colors, c.X, c.Y, c.Z, c.W)
	}
	return index
}

func (p *Part) AddTriangle(a, b, c int) { p.Indices = append(p.Indices, a, b, c) }

// ComputeNormals sets smooth vertex normals: the area-weighted sum of the
// faces around each vertex (vertices split at hard edges stay hard).
func (p *Part) ComputeNormals() {
	normals := make([]float32, len(p.Positions))
	for t := 0; t+2 < len(p.Indices); t += 3 {
		a := p.Position(p.Indices[t])
		n := p.Position(p.Indices[t+1]).sub(a).cross(p.Position(p.Indices[t+2]).sub(a))
		for k := 0; k < 3; k++ {
			v := p.Indices[t+k] * 3
			normals[v] += n.X
			normals[v+1] += n.Y
			normals[v+2] += n.Z
		}
	}
	for v := 0; v < len(normals); v += 3 {
		n := vec3{normals[v], normals[v+1], normals[v+2]}.normalize()
		if !finite(n.X) {
			n = vec3{0, 1, 0}
		}
		normals[v], normals[v+1], normals[v+2] = n.X, n.Y, n.Z
	}
	p.Normals = normals
}

// Transform applies a transform to positions and normals; a mirroring
// transform also flips the winding.
func (p *Part) Transform(m mat4) {
	if m.isIdentity() {
		return
	}
	for i := 0; i+2 < len(p.Positions); i += 3 {
		v := m.transform(vec3{p.Positions[i], p.Positions[i+1], p.Positions[i+2]})
		p.Positions[i], p.Positions[i+1], p.Positions[i+2] = v.X, v.Y, v.Z
	}
	if p.Normals != nil {
		nm := m
		if inv, ok := m.invert(); ok {
			nm = inv.transpose()
		}
		for i := 0; i+2 < len(p.Normals); i += 3 {
			v := nm.transformNormal(vec3{p.Normals[i], p.Normals[i+1], p.Normals[i+2]})
			if l := v.length(); l > 0 {
				v = v.scale(1 / l)
			}
			p.Normals[i], p.Normals[i+1], p.Normals[i+2] = v.X, v.Y, v.Z
		}
	}
	if m.determinant() < 0 {
		for t := 0; t+2 < len(p.Indices); t += 3 {
			p.Indices[t+1], p.Indices[t+2] = p.Indices[t+2], p.Indices[t+1]
		}
	}
}

func (p *Part) validate() error {
	if len(p.Indices)%3 != 0 {
		return errors.New("A mesh has an incomplete triangle.")
	}
	count := p.VertexCount()
	for _, i := range p.Indices {
		if i < 0 || i >= count {
			return fmt.Errorf("A triangle refers to vertex %d, but the mesh has only %d vertices.", i+1, count)
		}
	}
	if p.Normals != nil && len(p.Normals) != count*3 {
		p.Normals = nil
	}
	if p.TexCoords != nil && len(p.TexCoords) != count*2 {
		p.TexCoords = nil
	}
	if p.Colors != nil && len(p.Colors) != count*4 {
		p.Colors = nil
	}
	return nil
}

// Material is a simple material: base colour (linear RGBA, as glTF) and an
// optional base colour texture.
type Material struct {
	Name             string
	BaseColor        vec4
	BaseColorTexture *Texture
	DoubleSided      bool
}

func newMaterial(name string) *Material { return &Material{Name: name, BaseColor: vec4{1, 1, 1, 1}} }

// Texture is an encoded picture (PNG, JPEG, …).
type Texture struct {
	Data []byte
	// Extension is lower case without the dot ("png", "jpg").
	Extension string
	// Name is the original file name without extension, used for written texture files.
	Name string
}

// sniff recognises PNG, JPEG, WebP, BMP and GIF by their signature, else the given extension.
func sniff(data []byte, fallback string) string {
	switch {
	case bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G'}):
		return "png"
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		return "jpg"
	case len(data) > 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "webp"
	case bytes.HasPrefix(data, []byte("BM")):
		return "bmp"
	case bytes.HasPrefix(data, []byte("GIF8")):
		return "gif"
	}
	ext := strings.ToLower(strings.TrimLeft(fallback, "."))
	if ext == "jpeg" {
		return "jpg"
	}
	return ext
}

// Z up (STL, 3MF, PLY) → Y up: (x, y, z) → (x, z, −y).
var zUpToYUp = mat4{{1, 0, 0, 0}, {0, 0, -1, 0}, {0, 1, 0, 0}, {0, 0, 0, 1}}

// toZUp turns Y up into Z up: (x, y, z) → (x, −z, y).
func toZUp(v vec3) vec3 { return vec3{v.X, -v.Z, v.Y} }

func srgbToLinear(c float32) float32 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return pow32((c+0.055)/1.055, 2.4)
}

func linearToSrgb(c float32) float32 {
	if c <= 0.0031308 {
		return c * 12.92
	}
	return 1.055*pow32(c, 1/2.4) - 0.055
}

func srgbToLinear4(c vec4) vec4 {
	return vec4{srgbToLinear(c.X), srgbToLinear(c.Y), srgbToLinear(c.Z), c.W}
}

func linearToSrgb4(c vec4) vec4 {
	return vec4{linearToSrgb(c.X), linearToSrgb(c.Y), linearToSrgb(c.Z), c.W}
}

// toByte maps 0–1 to 0–255, rounded and clamped.
func toByte(v float32) byte {
	r := math.RoundToEven(float64(v * 255))
	return byte(max(0, min(255, r)))
}

// toHex is the colour as #RRGGBB, or #RRGGBBAA when not opaque (3MF).
func toHex(c vec4) string {
	hex := fmt.Sprintf("#%02X%02X%02X", toByte(c.X), toByte(c.Y), toByte(c.Z))
	if a := toByte(c.W); a != 255 {
		hex += fmt.Sprintf("%02X", a)
	}
	return hex
}

// parseHex reads #RGB, #RRGGBB or #RRGGBBAA (3MF colours).
func parseHex(text string) (vec4, bool) {
	if !strings.HasPrefix(text, "#") {
		return vec4{}, false
	}
	hex := text[1:]
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	if len(hex) != 6 && len(hex) != 8 {
		return vec4{}, false
	}
	var value uint32
	for _, c := range hex {
		var d uint32
		switch {
		case c >= '0' && c <= '9':
			d = uint32(c - '0')
		case c >= 'a' && c <= 'f':
			d = uint32(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = uint32(c-'A') + 10
		default:
			return vec4{}, false
		}
		value = value<<4 | d
	}
	if len(hex) == 6 {
		value = value<<8 | 0xFF
	}
	return vec4{float32(value>>24) / 255, float32((value>>16)&0xFF) / 255, float32((value>>8)&0xFF) / 255, float32(value&0xFF) / 255}, true
}
