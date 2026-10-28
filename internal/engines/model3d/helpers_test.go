package model3d

// Generated 3D test meshes (no sample files): a textured cube with face
// normals and UVs, a sphere with smooth normals and vertex colours, and a flat
// fan of coloured triangles. All in the model convention: millimetres, Y up,
// counter-clockwise front faces.

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/presets"
	"github.com/dseif0x/filee-go/internal/testutil"
)

var cubeColor = vec4{0.8, 0.2, 0.1, 1}

func ptr[T any](v T) *T { return &v }

// cubeModel is a 20 mm cube from (0, 0, 0) to (20, 20, 20): 24 vertices with face normals and UVs, a textured material.
func cubeModel() *Model {
	m := &Model{}
	mat := newMaterial("Brick red")
	mat.BaseColor = cubeColor
	mat.BaseColorTexture = &Texture{Data: checker(), Extension: "png", Name: "checker"}
	m.Materials = append(m.Materials, mat)
	p := newPart("Cube")
	p.Material = 0
	p.Normals, p.TexCoords = []float32{}, []float32{}
	center := vec3{10, 10, 10}
	for _, n := range []vec3{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
		var u vec3
		if abs32(n.Y) < 0.9 {
			u = vec3{0, 1, 0}.cross(n).normalize()
		} else {
			u = n.cross(vec3{1, 0, 0}).normalize()
		}
		v := n.cross(u)
		face := center.add(n.scale(10))
		first := p.AddVertex(face.add(u.neg().sub(v).scale(10)), ptr(n), &vec2{0, 0}, nil)
		p.AddVertex(face.add(u.sub(v).scale(10)), ptr(n), &vec2{1, 0}, nil)
		p.AddVertex(face.add(u.add(v).scale(10)), ptr(n), &vec2{1, 1}, nil)
		p.AddVertex(face.add(u.neg().add(v).scale(10)), ptr(n), &vec2{0, 1}, nil)
		p.AddTriangle(first, first+1, first+2)
		p.AddTriangle(first, first+2, first+3)
	}
	m.Parts = append(m.Parts, p)
	return m
}

func sphereColor(n vec3) vec4 { return vec4{(n.X + 1) / 2, (n.Y + 1) / 2, (n.Z + 1) / 2, 1} }

// sphereModel is a sphere of radius 10 mm around the origin with smooth normals and colours from the normal direction.
func sphereModel() *Model {
	const stacks, slices = 12, 16
	p := newPart("Sphere")
	p.Normals, p.Colors = []float32{}, []float32{}
	add := func(n vec3) int { return p.AddVertex(n.scale(10), ptr(n), nil, ptr(sphereColor(n))) }
	top := add(vec3{0, 1, 0})
	var rings [stacks - 1][slices]int
	for i := 1; i < stacks; i++ {
		phi := math.Pi * float64(i) / stacks
		for j := 0; j < slices; j++ {
			theta := 2 * math.Pi * float64(j) / slices
			rings[i-1][j] = add(vec3{float32(math.Sin(phi) * math.Cos(theta)), float32(math.Cos(phi)), float32(-math.Sin(phi) * math.Sin(theta))})
		}
	}
	bottom := add(vec3{0, -1, 0})
	for j := 0; j < slices; j++ {
		next := (j + 1) % slices
		p.AddTriangle(top, rings[0][j], rings[0][next])
		for i := 0; i < stacks-2; i++ {
			p.AddTriangle(rings[i][j], rings[i+1][j], rings[i+1][next])
			p.AddTriangle(rings[i][j], rings[i+1][next], rings[i][next])
		}
		p.AddTriangle(bottom, rings[stacks-2][next], rings[stacks-2][j])
	}
	return &Model{Parts: []*Part{p}}
}

// fanModel is six triangles around a white centre, flat in the XZ plane facing up, with pure colours on the rim.
func fanModel() *Model {
	p := newPart("Fan")
	p.Colors = []float32{}
	center := p.AddVertex(vec3{}, nil, nil, &vec4{1, 1, 1, 1})
	colors := []vec4{{1, 0, 0, 1}, {1, 1, 0, 1}, {0, 1, 0, 1}, {0, 1, 1, 1}, {0, 0, 1, 1}, {1, 0, 1, 1}}
	for k := 0; k < 6; k++ {
		a := math.Pi / 3 * float64(k)
		p.AddVertex(vec3{float32(math.Cos(a)) * 10, 0, float32(-math.Sin(a)) * 10}, nil, nil, &colors[k])
	}
	for k := 0; k < 6; k++ {
		p.AddTriangle(center, 1+k, 1+(k+1)%6)
	}
	return &Model{Parts: []*Part{p}}
}

func makeModel(kind string) *Model {
	switch kind {
	case "cube":
		return cubeModel()
	case "sphere":
		return sphereModel()
	}
	return fanModel()
}

// checker is a 4 × 4 PNG checkerboard.
func checker() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if (x+y)%2 == 0 {
				img.Set(x, y, color.NRGBA{250, 240, 230, 255})
			} else {
				img.Set(x, y, color.NRGBA{40, 60, 200, 255})
			}
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

// folderAllocator hands out files named name{suffix}.ext in one folder.
func folderAllocator(folder, name string) convert.OutputAllocator {
	return convert.AllocatorFunc(func(ext, suffix string) string {
		return filepath.Join(folder, name+suffix+"."+ext)
	})
}

// save writes m as format into folder; returns the main file.
func save(t testing.TB, m *Model, folder, name, format string) string {
	t.Helper()
	var outputs []string
	if err := Write(context.Background(), m, format, folderAllocator(folder, name), nil, &outputs); err != nil {
		t.Fatalf("write %s: %v", format, err)
	}
	return outputs[0]
}

func load(t testing.TB, path, format string) *Model {
	t.Helper()
	m, err := Read(context.Background(), path, format, nil)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return m
}

// convertFile runs the engine on input and returns the outputs.
func convertFile(t testing.TB, input, from, to string, tweak func(*presets.Preset)) ([]string, error) {
	t.Helper()
	dir := t.TempDir()
	base := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
	preset := presets.New("test", to)
	if tweak != nil {
		tweak(preset)
	}
	step := &convert.Step{InputPath: input, From: from, To: to, Preset: preset, WorkDir: t.TempDir(),
		Output: convert.AllocatorFunc(func(ext, suffix string) string { return filepath.Join(dir, base+suffix+"."+ext) })}
	return New().Convert(context.Background(), step, nil)
}

func run(t testing.TB, input, from, to string) []string {
	t.Helper()
	out, err := convertFile(t, input, from, to, nil)
	if err != nil {
		t.Fatalf("%s → %s: %v", from, to, err)
	}
	return out
}

func fail(t testing.TB, input, from, to string) string {
	t.Helper()
	_, err := convertFile(t, input, from, to, nil)
	if err == nil {
		t.Fatalf("%s → %s did not fail", from, to)
	}
	return err.Error()
}

// signedVolume is positive for a closed mesh with outward faces: it changes sign when the winding flips.
func signedVolume(m *Model) float64 {
	var v float64
	for _, p := range m.Parts {
		for t := 0; t < p.TriangleCount(); t++ {
			a, b, c := p.Position(p.Indices[t*3]), p.Position(p.Indices[t*3+1]), p.Position(p.Indices[t*3+2])
			v += float64(a.dot(b.cross(c))) / 6
		}
	}
	return v
}

func area(m *Model) float64 {
	var s float64
	for _, p := range m.Parts {
		for t := 0; t < p.TriangleCount(); t++ {
			a, b, c := p.Position(p.Indices[t*3]), p.Position(p.Indices[t*3+1]), p.Position(p.Indices[t*3+2])
			s += float64(b.sub(a).cross(c.sub(a)).length()) / 2
		}
	}
	return s
}

type key3 [3]int

// posKey rounds a position to 1/100 mm so positions from different files compare equal.
func posKey(p vec3) key3 {
	r := func(f float32) int { return int(math.Round(float64(f * 100))) }
	return key3{r(p.X), r(p.Y), r(p.Z)}
}

func positionSet(m *Model) map[key3]bool {
	s := map[key3]bool{}
	for _, p := range m.Parts {
		for v := 0; v < p.VertexCount(); v++ {
			s[posKey(p.Position(v))] = true
		}
	}
	return s
}

func dist3(a, b vec3) float32 { return a.sub(b).length() }

func closeV3(t testing.TB, want, got vec3, tol float32) {
	t.Helper()
	if dist3(want, got) > tol {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func closeV4(t testing.TB, want, got vec4, tol float32) {
	t.Helper()
	if want.distance(got) > tol {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func near(t testing.TB, want, got, tol float64, what string) {
	t.Helper()
	if math.Abs(want-got) > tol {
		t.Fatalf("%s: expected %v, got %v", what, want, got)
	}
}

// box is the 12 triangles of an axis-aligned box, outward facing.
func box(lo, hi vec3) [][3]vec3 {
	c := func(i int) vec3 {
		v := lo
		if i&1 != 0 {
			v.X = hi.X
		}
		if i&2 != 0 {
			v.Y = hi.Y
		}
		if i&4 != 0 {
			v.Z = hi.Z
		}
		return v
	}
	quads := [][4]int{{0, 2, 3, 1}, {4, 5, 7, 6}, {0, 1, 5, 4}, {2, 6, 7, 3}, {0, 4, 6, 2}, {1, 3, 7, 5}}
	var out [][3]vec3
	for _, q := range quads {
		out = append(out, [3]vec3{c(q[0]), c(q[1]), c(q[2])}, [3]vec3{c(q[0]), c(q[2]), c(q[3])})
	}
	return out
}

func stlFacet(b *bytes.Buffer, a, bb, c vec3) {
	rec := make([]byte, 50)
	for i, v := range []vec3{a, bb, c} {
		o := 12 + i*12
		binary.LittleEndian.PutUint32(rec[o:], math.Float32bits(v.X))
		binary.LittleEndian.PutUint32(rec[o+4:], math.Float32bits(v.Y))
		binary.LittleEndian.PutUint32(rec[o+8:], math.Float32bits(v.Z))
	}
	b.Write(rec)
}

func writeBinaryStl(t testing.TB, path string, tris [][3]vec3) {
	var b bytes.Buffer
	header := make([]byte, 84)
	binary.LittleEndian.PutUint32(header[80:], uint32(len(tris)))
	b.Write(header)
	for _, tr := range tris {
		stlFacet(&b, tr[0], tr[1], tr[2])
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

type stlFacetData struct{ normal, a, b, c vec3 }

func readBinaryStl(t testing.TB, path string) ([]stlFacetData, vec3, vec3) {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	count := int(binary.LittleEndian.Uint32(data[80:]))
	lo := vec3{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}
	hi := lo.neg()
	var out []stlFacetData
	for i := 0; i < count; i++ {
		var v [4]vec3
		for k := 0; k < 4; k++ {
			o := 84 + i*50 + k*12
			v[k] = vec3{math.Float32frombits(binary.LittleEndian.Uint32(data[o:])), math.Float32frombits(binary.LittleEndian.Uint32(data[o+4:])), math.Float32frombits(binary.LittleEndian.Uint32(data[o+8:]))}
			if k > 0 {
				lo, hi = lo.min(v[k]), hi.max(v[k])
			}
		}
		out = append(out, stlFacetData{v[0], v[1], v[2], v[3]})
	}
	return out, lo, hi
}

func write3mf(t testing.TB, path, model string) {
	t.Helper()
	testutil.Zip(t, path, "_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Target="/3D/3dmodel.model" Id="rel0" Type="http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"/></Relationships>`,
		"3D/3dmodel.model", model)
}
