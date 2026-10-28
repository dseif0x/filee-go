package model3d

// 3D models: GLB, glTF, STL, OBJ, 3MF and PLY into each other and to
// pictures. Meshes are generated (helpers_test.go) or written as text / bytes
// here; every format pair is converted through the engine and read back.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	_ "image/jpeg"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/presets"
)

var (
	withNormals      = []string{"obj", "ply", "glb", "gltf"}
	withUvs          = []string{"obj", "ply", "glb", "gltf"}
	withVertexColors = []string{"obj", "ply", "3mf", "glb", "gltf"}
	withMaterials    = []string{"obj", "3mf", "glb", "gltf"}
	withTextures     = []string{"obj", "glb", "gltf"}
)

func TestEveryPairAndPictureIsOneStep(t *testing.T) {
	planner := convert.NewPlanner([]convert.Converter{New()}, nil)
	for _, from := range InputFormats {
		for _, to := range append(slices.Clone(Formats), "png", "jpg") {
			if from == to {
				continue
			}
			r := planner.Plan(from, to, convert.Options{})
			if r == nil || len(r.Steps) != 1 {
				t.Fatalf("%s → %s: %+v", from, to, r)
			}
		}
	}
	if planner.Plan("glb", "fbx", convert.Options{}) != nil {
		t.Fatal("FBX is read only")
	}
}

func TestEveryFormatPairKeepsGeometry(t *testing.T) {
	for _, kind := range []string{"cube", "sphere", "fan"} {
		for _, from := range Formats {
			for _, to := range Formats {
				if from == to {
					continue
				}
				t.Run(kind+"/"+from+"-"+to, func(t *testing.T) {
					folder := t.TempDir()
					source := save(t, makeModel(kind), folder, kind, from)
					expected := load(t, source, from)
					outputs := run(t, source, from, to)
					var output string
					for _, o := range outputs {
						if strings.HasSuffix(o, "."+to) {
							output = o
						}
					}
					actual := load(t, output, to)
					if expected.TriangleCount() != actual.TriangleCount() {
						t.Fatalf("triangles %d → %d", expected.TriangleCount(), actual.TriangleCount())
					}
					el, eh := expected.Bounds()
					al, ah := actual.Bounds()
					closeV3(t, el, al, 1e-3)
					closeV3(t, eh, ah, 1e-3)
					near(t, signedVolume(expected), signedVolume(actual), 0.05, "volume")
					near(t, area(expected), area(actual), 0.05, "area")
					ep, ap := positionSet(expected), positionSet(actual)
					if len(ep) != len(ap) {
						t.Fatalf("positions %d → %d", len(ep), len(ap))
					}
					for k := range ep {
						if !ap[k] {
							t.Fatalf("position %v lost", k)
						}
					}
					has := func(f func(*Part) bool) bool { return slices.ContainsFunc(expected.Parts, f) }
					if slices.Contains(withNormals, to) && has(func(p *Part) bool { return p.Normals != nil }) {
						attributesMatch(t, expected, actual, func(p *Part) bool { return p.Normals != nil },
							func(p *Part, v int) vec4 { n := p.Normal(v); return vec4{n.X, n.Y, n.Z, 0} }, 1e-3)
					}
					if slices.Contains(withUvs, to) && has(func(p *Part) bool { return p.TexCoords != nil }) {
						attributesMatch(t, expected, actual, func(p *Part) bool { return p.TexCoords != nil },
							func(p *Part, v int) vec4 { uv := p.TexCoord(v); return vec4{uv.X, uv.Y, 0, 0} }, 1e-4)
					}
					if slices.Contains(withVertexColors, to) && has(func(p *Part) bool { return p.Colors != nil }) {
						attributesMatch(t, expected, actual, func(p *Part) bool { return p.Colors != nil },
							func(p *Part, v int) vec4 { return p.Color(v) }, 2.5/255)
					}
					if slices.Contains(withMaterials, to) && len(expected.Materials) > 0 {
						if len(actual.Materials) != 1 {
							t.Fatalf("%d materials", len(actual.Materials))
						}
						tol := float32(1e-4)
						if to == "3mf" {
							tol = 0.01
						}
						closeV4(t, expected.Materials[0].BaseColor, actual.Materials[0].BaseColor, tol)
						if slices.Contains(withTextures, to) && expected.Materials[0].BaseColorTexture != nil {
							if actual.Materials[0].BaseColorTexture == nil || !bytes.Equal(expected.Materials[0].BaseColorTexture.Data, actual.Materials[0].BaseColorTexture.Data) {
								t.Fatal("texture lost")
							}
						}
					}
				})
			}
		}
	}
}

// attributesMatch: every vertex of actual has a value that some vertex at the same place had before.
func attributesMatch(t *testing.T, expected, actual *Model, has func(*Part) bool, value func(*Part, int) vec4, tol float32) {
	t.Helper()
	known := map[key3][]vec4{}
	for _, p := range expected.Parts {
		if !has(p) {
			continue
		}
		for v := 0; v < p.VertexCount(); v++ {
			k := posKey(p.Position(v))
			known[k] = append(known[k], value(p, v))
		}
	}
	if !slices.ContainsFunc(actual.Parts, has) {
		t.Fatal("the attribute is lost")
	}
	for _, p := range actual.Parts {
		if !has(p) {
			continue
		}
		for v := 0; v < p.VertexCount(); v++ {
			found := value(p, v)
			ok := false
			for _, c := range known[posKey(p.Position(v))] {
				if c.distance(found) <= tol*2 {
					ok = true
				}
			}
			if !ok {
				t.Fatalf("vertex %d at %v has %v; expected one of %v", v, p.Position(v), found, known[posKey(p.Position(v))])
			}
		}
	}
}

type gltfJSON struct {
	Buffers []struct {
		URI        string `json:"uri"`
		ByteLength int    `json:"byteLength"`
	} `json:"buffers"`
	Accessors []struct {
		Min []float64 `json:"min"`
		Max []float64 `json:"max"`
	} `json:"accessors"`
	Meshes []struct {
		Primitives []struct {
			Attributes map[string]int `json:"attributes"`
		} `json:"primitives"`
	} `json:"meshes"`
	Images []struct {
		URI string `json:"uri"`
	} `json:"images"`
}

// glbJSON is the JSON chunk of a GLB.
func glbJSON(t *testing.T, path string) gltfJSON {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data[:4]) != "glTF" {
		t.Fatal("not a GLB")
	}
	n := binary.LittleEndian.Uint32(data[12:])
	var doc gltfJSON
	if err := json.Unmarshal(data[20:20+n], &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestStlMillimetresZUpBecomeGltfMetresYUp(t *testing.T) {
	// A box 10 × 20 mm wide and 50 mm tall along Z, as a 3D printer sees it.
	folder := t.TempDir()
	stl := filepath.Join(folder, "tower.stl")
	writeBinaryStl(t, stl, box(vec3{}, vec3{10, 20, 50}))
	glb := run(t, stl, "stl", "glb")[0]
	doc := glbJSON(t, glb)
	pos := doc.Accessors[doc.Meshes[0].Primitives[0].Attributes["POSITION"]]
	near(t, 0.010, pos.Max[0]-pos.Min[0], 1e-6, "x")
	near(t, 0.050, pos.Max[1]-pos.Min[1], 1e-6, "up")
	near(t, 0.020, pos.Max[2]-pos.Min[2], 1e-6, "z")

	facets, lo, hi := readBinaryStl(t, run(t, glb, "glb", "stl")[0])
	closeV3(t, vec3{10, 20, 50}, hi.sub(lo), 1e-4)
	if len(facets) != 12 {
		t.Fatal(len(facets))
	}
}

func TestGltfFrom3mfInInchesIsScaledToMetres(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inch.3mf")
	write3mf(t, path, `<model unit="inch" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02">
 <resources>
  <object id="1" type="model"><mesh>
   <vertices><vertex x="0" y="0" z="0"/><vertex x="1" y="0" z="0"/><vertex x="0" y="1" z="0"/><vertex x="0" y="0" z="1"/></vertices>
   <triangles><triangle v1="0" v2="2" v3="1"/><triangle v1="0" v2="1" v3="3"/><triangle v1="0" v2="3" v3="2"/><triangle v1="1" v2="2" v3="3"/></triangles>
  </mesh></object>
 </resources>
 <build><item objectid="1"/></build>
</model>`)
	doc := glbJSON(t, run(t, path, "3mf", "glb")[0])
	pos := doc.Accessors[doc.Meshes[0].Primitives[0].Attributes["POSITION"]]
	near(t, 0.0254, pos.Max[0]-pos.Min[0], 1e-6, "x")
	near(t, 0.0254, pos.Max[1]-pos.Min[1], 1e-6, "y")
}

func TestStlFacetNormalsPointOutward(t *testing.T) {
	source := save(t, cubeModel(), t.TempDir(), "cube", "glb")
	facets, lo, hi := readBinaryStl(t, run(t, source, "glb", "stl")[0])
	center := lo.add(hi).scale(0.5)
	if len(facets) != 12 {
		t.Fatal(len(facets))
	}
	for _, f := range facets {
		near(t, 1, float64(f.normal.length()), 1e-4, "normal length")
		if f.normal.dot(f.a.add(f.b).add(f.c).scale(1.0/3).sub(center)) <= 0 {
			t.Fatal("inward facet normal")
		}
	}
}

func TestObjOutputWritesMaterialLibraryAndTexture(t *testing.T) {
	glb := save(t, cubeModel(), t.TempDir(), "kubus", "glb")
	outputs := run(t, glb, "glb", "obj")
	var obj, mtl, pngFile string
	for _, o := range outputs {
		switch filepath.Ext(o) {
		case ".obj":
			obj = o
		case ".mtl":
			mtl = o
		case ".png":
			pngFile = o
		}
	}
	if obj == "" || mtl == "" || pngFile == "" || len(outputs) != 3 {
		t.Fatalf("outputs %v", outputs)
	}
	objText, _ := os.ReadFile(obj)
	mtlText, _ := os.ReadFile(mtl)
	if !strings.Contains(string(objText), "mtllib "+filepath.Base(mtl)) || !strings.Contains(string(mtlText), "map_Kd "+filepath.Base(pngFile)) {
		t.Fatalf("%s\n%s", objText, mtlText)
	}
	data, _ := os.ReadFile(pngFile)
	if !bytes.Equal(data, checker()) {
		t.Fatal("texture changed")
	}
}

func TestGltfOutputIsOneFileWithTheBufferInside(t *testing.T) {
	folder := t.TempDir()
	source := save(t, cubeModel(), folder, "cube", "obj")
	outputs := run(t, source, "obj", "gltf")
	if len(outputs) != 1 {
		t.Fatal(outputs)
	}
	data, _ := os.ReadFile(outputs[0])
	var doc gltfJSON
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(doc.Buffers[0].URI, "data:application/octet-stream;base64,") {
		t.Fatal(doc.Buffers[0].URI[:40])
	}
	if bins, _ := filepath.Glob(filepath.Join(filepath.Dir(outputs[0]), "*.bin")); len(bins) > 0 {
		t.Fatal(bins)
	}
	m := load(t, outputs[0], "gltf")
	if m.TriangleCount() != 12 || m.Materials[0].BaseColorTexture == nil {
		t.Fatal("cube or texture lost")
	}
}

// texturedQuad writes quad.gltf with quad.bin and quad.png beside it; matrix is the node's (column-major).
func texturedQuad(t *testing.T, folder string, matrix []float64) string {
	var bin bytes.Buffer
	for _, v := range []float32{0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0} {
		binary.Write(&bin, binary.LittleEndian, v)
	}
	for _, v := range []float32{0, 1, 1, 1, 1, 0, 0, 0} {
		binary.Write(&bin, binary.LittleEndian, v)
	}
	for _, v := range []uint16{0, 1, 2, 0, 2, 3} {
		binary.Write(&bin, binary.LittleEndian, v)
	}
	os.WriteFile(filepath.Join(folder, "quad.bin"), bin.Bytes(), 0o644)
	os.WriteFile(filepath.Join(folder, "quad.png"), checker(), 0o644)
	node := map[string]any{"mesh": 0, "name": "Quad"}
	if matrix != nil {
		node["matrix"] = matrix
	}
	doc := map[string]any{
		"asset":       map[string]any{"version": "2.0"},
		"scene":       0,
		"scenes":      []any{map[string]any{"nodes": []int{0}}},
		"nodes":       []any{node},
		"meshes":      []any{map[string]any{"name": "Quad", "primitives": []any{map[string]any{"attributes": map[string]int{"POSITION": 0, "TEXCOORD_0": 1}, "indices": 2, "material": 0}}}},
		"materials":   []any{map[string]any{"name": "Painted", "pbrMetallicRoughness": map[string]any{"baseColorTexture": map[string]int{"index": 0}}}},
		"textures":    []any{map[string]int{"source": 0}},
		"images":      []any{map[string]string{"uri": "quad.png"}},
		"accessors":   []any{map[string]any{"bufferView": 0, "componentType": 5126, "count": 4, "type": "VEC3", "min": []float64{0, 0, 0}, "max": []float64{1, 1, 0}}, map[string]any{"bufferView": 1, "componentType": 5126, "count": 4, "type": "VEC2"}, map[string]any{"bufferView": 2, "componentType": 5123, "count": 6, "type": "SCALAR"}},
		"bufferViews": []any{map[string]int{"buffer": 0, "byteOffset": 0, "byteLength": 48}, map[string]int{"buffer": 0, "byteOffset": 48, "byteLength": 32}, map[string]int{"buffer": 0, "byteOffset": 80, "byteLength": 12}},
		"buffers":     []any{map[string]any{"uri": "quad.bin", "byteLength": 92}},
	}
	b, _ := json.Marshal(doc)
	path := filepath.Join(folder, "quad.gltf")
	os.WriteFile(path, b, 0o644)
	return path
}

func TestGltfWithSeparateBufferAndTextureIsRead(t *testing.T) {
	// The node scales by 2 and moves up by 1 m: both end up in the vertices.
	gltfPath := texturedQuad(t, t.TempDir(), []float64{2, 0, 0, 0, 0, 2, 0, 0, 0, 0, 2, 0, 0, 1, 0, 1})
	m := load(t, run(t, gltfPath, "gltf", "ply")[0], "ply")
	lo, hi := m.Bounds()
	closeV3(t, vec3{0, 1000, 0}, lo, 1e-2)
	closeV3(t, vec3{2000, 3000, 0}, hi, 1e-2)
	if m.TriangleCount() != 2 {
		t.Fatal(m.TriangleCount())
	}
	// glTF's UV origin is the top left, PLY's the bottom left.
	p := m.Parts[0]
	for v := 0; v < 4; v++ {
		if q := p.Position(v); q.X < 1 && q.Y < 1001 && p.TexCoord(v) != (vec2{0, 0}) {
			t.Fatalf("uv %v", p.TexCoord(v))
		}
	}
	if out := run(t, gltfPath, "gltf", "obj"); len(out) != 3 {
		t.Fatal(out) // .obj, .mtl and the texture
	}
}

func TestGltfBuffersAndPicturesStayBesideTheModel(t *testing.T) {
	folder := t.TempDir()
	source := filepath.Join(folder, "source")
	outside := filepath.Join(folder, "outside")
	os.MkdirAll(filepath.Join(source, "textures"), 0o755)
	os.MkdirAll(outside, 0o755)
	gltfPath := texturedQuad(t, source, nil)
	original, _ := os.ReadFile(gltfPath)
	loadWith := func(from, to string) (*Model, error) {
		os.WriteFile(gltfPath, []byte(strings.ReplaceAll(string(original), `"`+from+`"`, `"`+to+`"`)), 0o644)
		return Read(context.Background(), gltfPath, "gltf", nil)
	}
	// A folder below the model's is fine.
	os.Rename(filepath.Join(source, "quad.png"), filepath.Join(source, "textures", "quad.png"))
	m, err := loadWith("quad.png", "textures/quad.png")
	if err != nil || !bytes.Equal(m.MaterialOf(m.Parts[0]).BaseColorTexture.Data, checker()) {
		t.Fatal(err)
	}
	// A picture up and out of it, or a buffer at an absolute path, is never opened.
	os.Rename(filepath.Join(source, "textures", "quad.png"), filepath.Join(outside, "quad.png"))
	if _, err := loadWith("quad.png", "../outside/quad.png"); err == nil || !strings.Contains(err.Error(), "quad.png") {
		t.Fatalf("picture outside: %v", err)
	}
	os.Rename(filepath.Join(outside, "quad.png"), filepath.Join(source, "quad.png"))
	os.Rename(filepath.Join(source, "quad.bin"), filepath.Join(outside, "quad.bin"))
	abs := filepath.ToSlash(filepath.Join(outside, "quad.bin"))
	if _, err := loadWith("quad.bin", "file://"+abs); err == nil {
		t.Fatal("file URI outside was read")
	}
	if _, err := loadWith("quad.bin", abs); err == nil {
		t.Fatal("absolute path outside was read")
	}
}

func TestGltfReadsPercentEncodedNames(t *testing.T) {
	folder := t.TempDir()
	source := filepath.Join(folder, "source")
	os.MkdirAll(source, 0o755)
	gltfPath := texturedQuad(t, source, nil)
	os.Rename(filepath.Join(source, "quad.png"), filepath.Join(source, "paint image.png"))
	os.Rename(filepath.Join(source, "quad.bin"), filepath.Join(source, "mesh data.bin"))
	original, _ := os.ReadFile(gltfPath)
	text := strings.ReplaceAll(strings.ReplaceAll(string(original), `"quad.png"`, `"paint%20image.png"`), `"quad.bin"`, `"mesh%20data.bin"`)
	os.WriteFile(gltfPath, []byte(text), 0o644)
	m := load(t, gltfPath, "gltf")
	if m.TriangleCount() != 2 || !bytes.Equal(m.MaterialOf(m.Parts[0]).BaseColorTexture.Data, checker()) {
		t.Fatal("not read")
	}
	// Decoding must still leave the folder boundary in force.
	outside := filepath.Join(folder, "outside")
	os.MkdirAll(outside, 0o755)
	os.Rename(filepath.Join(source, "paint image.png"), filepath.Join(outside, "paint image.png"))
	os.WriteFile(gltfPath, []byte(strings.ReplaceAll(text, `"paint%20image.png"`, `"..%2Foutside%2Fpaint%20image.png"`)), 0o644)
	if _, err := Read(context.Background(), gltfPath, "gltf", nil); err == nil {
		t.Fatal("picture outside was read")
	}
}

func TestAsciiStlWithTwoSolids(t *testing.T) {
	path := filepath.Join(t.TempDir(), "two.stl")
	os.WriteFile(path, []byte(`solid first
  facet normal 0 0 1
    outer loop
      vertex 0 0 0
      vertex 1 0 0
      vertex 0 1 0
    endloop
  endfacet
endsolid first
solid second
  facet normal 0 0 1
    outer loop
      vertex 5 0 0
      vertex 6.5e0 0 0
      vertex 5 1 0
    endloop
  endfacet
endsolid second
`), 0o644)
	m := load(t, path, "stl")
	if len(m.Parts) != 2 || m.Parts[0].Name != "first" || m.Parts[1].Name != "second" {
		t.Fatalf("%+v", m.Parts)
	}
	if doc := glbJSON(t, run(t, path, "stl", "glb")[0]); len(doc.Meshes) != 2 {
		t.Fatal(len(doc.Meshes))
	}
}

func TestBinaryStlWithSolidHeaderAndExtraBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "solid.stl")
	writeBinaryStl(t, path, box(vec3{}, vec3{1, 2, 3}))
	data, _ := os.ReadFile(path)
	copy(data, "solid exported by a CAD program")
	os.WriteFile(path, append(data, "trailing"...), 0o644)
	m := load(t, path, "stl")
	if m.TriangleCount() != 12 || m.VertexCount() != 8 {
		t.Fatal(m.TriangleCount(), m.VertexCount())
	}
}

func TestObjPolygonsNegativeIndicesColoursAndMaterials(t *testing.T) {
	folder := t.TempDir()
	os.WriteFile(filepath.Join(folder, "wood grain.png"), checker(), 0o644)
	os.WriteFile(filepath.Join(folder, "parts.mtl"), []byte("newmtl wood\nKd 0.5 0.25 0.125\nmap_Kd -s 1 1 1 -bm 0.5 wood grain.png\n"), 0o644)
	// A U-shaped octagon (concave: a fan from its first corner would cover the
	// notch) and a quad with negative indices and vertex colours.
	obj := filepath.Join(folder, "parts.obj")
	os.WriteFile(obj, []byte(`mtllib parts.mtl
o U
v 2 2 0 1 0 0
v 2 1 0 1 0 0
v 1 1 0 1 0 0
v 1 2 0 1 0 0
v 0 2 0 1 0 0
v 0 0 0 1 0 0
v 3 0 0 1 0 0
v 3 2 0 1 0 0
usemtl wood
f 1 2 3 4 5 6 7 8
o Quad
v 10 0 0 0 0 255
v 11 0 0 0 0 255
v 11 1 0 0 0 255
v 10 1 0 0 0 255
vt 0 0
vt 1 1
f -4/-2 -3/-2 -2/-1 -1/-1
`), 0o644)
	m := load(t, obj, "obj")
	var u, quad *Part
	for _, p := range m.Parts {
		switch p.Name {
		case "U":
			u = p
		case "Quad":
			quad = p
		}
	}
	if u == nil || quad == nil || u.TriangleCount() != 6 {
		t.Fatal("parts")
	}
	near(t, 5, area(&Model{Parts: []*Part{u}}), 1e-4, "U area")
	mat := m.MaterialOf(u)
	if mat.Name != "wood" || mat.BaseColor != (vec4{0.5, 0.25, 0.125, 1}) || !bytes.Equal(mat.BaseColorTexture.Data, checker()) {
		t.Fatalf("%+v", mat)
	}
	if quad.TriangleCount() != 2 || quad.Color(0) != (vec4{0, 0, 1, 1}) {
		t.Fatal(quad.Color(0)) // 0–255 values are scaled
	}
	out := load(t, run(t, obj, "obj", "glb")[0], "glb")
	if out.TriangleCount() != 8 {
		t.Fatal(out.TriangleCount())
	}
	found := false
	for _, mm := range out.Materials {
		if mm.Name == "wood" && mm.BaseColorTexture != nil {
			found = true
		}
	}
	if !found {
		t.Fatal("wood texture lost")
	}
}

func TestObjTexturesStayBesideTheModel(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	outside := filepath.Join(root, "outside")
	os.MkdirAll(source, 0o755)
	os.MkdirAll(outside, 0o755)
	os.WriteFile(filepath.Join(source, "allowed.png"), checker(), 0o644)
	secret := filepath.Join(outside, "secret.png")
	os.WriteFile(secret, checker(), 0o644)
	os.WriteFile(filepath.Join(source, "materials.mtl"), []byte("newmtl allowed\nmap_Kd allowed.png\nnewmtl parent\nmap_Kd ../outside/secret.png\nnewmtl absolute\nmap_Kd "+filepath.ToSlash(secret)+"\n"), 0o644)
	os.WriteFile(filepath.Join(source, "drawing.obj"), []byte("mtllib materials.mtl\nv 0 0 0\nv 1 0 0\nv 0 1 0\nvt 0 0\nvt 1 0\nvt 0 1\no allowed\nusemtl allowed\nf 1/1 2/2 3/3\no parent\nusemtl parent\nf 1/1 2/2 3/3\no absolute\nusemtl absolute\nf 1/1 2/2 3/3\n"), 0o644)
	m := load(t, filepath.Join(source, "drawing.obj"), "obj")
	for _, p := range m.Parts {
		has := m.MaterialOf(p).BaseColorTexture != nil
		if has != (p.Name == "allowed") {
			t.Fatalf("%s: texture %v", p.Name, has)
		}
	}
}

func TestPlyASCIIAndBigEndian(t *testing.T) {
	folder := t.TempDir()
	ascii := filepath.Join(folder, "ascii.ply")
	os.WriteFile(ascii, []byte(`ply
format ascii 1.0
comment a quad and a triangle
element vertex 5
property float x
property float y
property float z
property uchar red
property uchar green
property uchar blue
element face 2
property list uchar int vertex_indices
end_header
0 0 0 255 0 0
1 0 0 0 255 0
1 1 0 0 0 255
0 1 0 255 255 255
2 0 0 0 0 0
4 0 1 2 3
3 1 4 2

`), 0o644)
	m := load(t, ascii, "ply")
	if m.TriangleCount() != 3 || m.Parts[0].Color(1) != (vec4{0, 1, 0, 1}) {
		t.Fatal(m.TriangleCount(), m.Parts[0].Color(1))
	}
	// Z up → Y up: the quad lies in the XZ plane.
	if m.Parts[0].Position(2) != (vec3{1, 0, -1}) {
		t.Fatal(m.Parts[0].Position(2))
	}

	var b bytes.Buffer
	b.WriteString("ply\nformat binary_big_endian 1.0\nelement vertex 3\nproperty double x\nproperty double y\nproperty double z\nproperty ushort red\nproperty ushort green\nproperty ushort blue\nelement edge 1\nproperty int vertex1\nproperty int vertex2\nelement face 1\nproperty list uchar uint vertex_indices\nend_header\n")
	for _, p := range [][3]float64{{0, 0, 0}, {4, 0, 0}, {0, 3, 0}} {
		for _, v := range p {
			binary.Write(&b, binary.BigEndian, v)
		}
		for _, c := range []uint16{65535, 0, 32768} {
			binary.Write(&b, binary.BigEndian, c)
		}
	}
	binary.Write(&b, binary.BigEndian, int32(0))
	binary.Write(&b, binary.BigEndian, int32(1))
	b.WriteByte(3)
	for _, i := range []uint32{0, 1, 2} {
		binary.Write(&b, binary.BigEndian, i)
	}
	big := filepath.Join(folder, "big.ply")
	os.WriteFile(big, b.Bytes(), 0o644)
	bm := load(t, big, "ply")
	if bm.TriangleCount() != 1 {
		t.Fatal(bm.TriangleCount())
	}
	near(t, 6, area(bm), 1e-5, "area")
	near(t, 1, float64(bm.Parts[0].Color(0).X), 1e-4, "red")
	near(t, 0.5, float64(bm.Parts[0].Color(0).Z), 1e-3, "blue")
}

func TestThreeMfComponentsTransformsMaterialsAndColourGroups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "assembly.3mf")
	write3mf(t, path, `<model unit="millimeter" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02" xmlns:m="http://schemas.microsoft.com/3dmanufacturing/material/2015/02">
 <resources>
  <basematerials id="1"><base name="Orange" displaycolor="#FF8000"/><base name="Blue" displaycolor="#0000FFFF"/></basematerials>
  <m:colorgroup id="2"><m:color color="#FF0000"/><m:color color="#00FF00"/></m:colorgroup>
  <object id="3" type="model" pid="1" pindex="0"><mesh>
   <vertices><vertex x="0" y="0" z="0"/><vertex x="10" y="0" z="0"/><vertex x="0" y="10" z="0"/><vertex x="10" y="10" z="0"/></vertices>
   <triangles><triangle v1="0" v2="1" v3="2"/><triangle v1="1" v2="3" v3="2" pid="1" p1="1"/></triangles>
  </mesh></object>
  <object id="4" type="model"><mesh>
   <vertices><vertex x="0" y="0" z="0"/><vertex x="1" y="0" z="0"/><vertex x="0" y="1" z="0"/></vertices>
   <triangles><triangle v1="0" v2="1" v3="2" pid="2" p1="0" p2="1"/></triangles>
  </mesh></object>
  <object id="5" type="support"><mesh>
   <vertices><vertex x="0" y="0" z="0"/><vertex x="1" y="0" z="0"/><vertex x="0" y="1" z="0"/></vertices>
   <triangles><triangle v1="0" v2="1" v3="2"/></triangles>
  </mesh></object>
  <object id="6" type="model"><components>
   <component objectid="3"/>
   <component objectid="4" transform="1 0 0 0 1 0 0 0 1 100 0 0"/>
   <component objectid="5"/>
  </components></object>
 </resources>
 <build><item objectid="6" transform="1 0 0 0 1 0 0 0 1 0 0 5"/></build>
</model>`)
	m := load(t, path, "3mf")
	if m.TriangleCount() != 3 {
		t.Fatal(m.TriangleCount()) // the support is left out
	}
	if len(m.Materials) != 2 || m.Materials[0].Name != "Orange" || m.Materials[1].Name != "Blue" {
		t.Fatal(m.Materials)
	}
	closeV4(t, vec4{1, srgbToLinear(128.0 / 255), 0, 1}, m.Materials[0].BaseColor, 1e-4)
	var colored *Part
	for _, p := range m.Parts {
		if p.Colors != nil {
			colored = p
		}
	}
	for v := 0; v < colored.VertexCount(); v++ {
		if colored.Position(v).X > 100.5 && colored.Color(v) != (vec4{0, 1, 0, 1}) {
			t.Fatal(colored.Color(v)) // p2 → second colour
		}
	}
	// Z up → Y up, and the item's 5 mm lift becomes Y.
	for _, p := range m.Parts {
		near(t, 5, float64(p.Position(0).Y), 1e-4, "lift")
	}
}

func TestPlyOutputTurnsMaterialColoursIntoVertexColours(t *testing.T) {
	source := save(t, cubeModel(), t.TempDir(), "cube", "3mf")
	m := load(t, run(t, source, "3mf", "ply")[0], "ply")
	want := linearToSrgb4(cubeColor)
	for v := 0; v < m.VertexCount(); v++ {
		closeV4(t, want, m.Parts[0].Color(v), 1.5/255)
	}
}

func TestMissingNormalsAreComputed(t *testing.T) {
	// A sphere with normals and a fan without, in one OBJ: every vertex of the
	// PLY gets a unit normal, the fan's pointing up.
	m := sphereModel()
	fan := fanModel().Parts[0]
	fan.Colors = nil
	m.Parts = append(m.Parts, fan)
	obj := save(t, m, t.TempDir(), "mixed", "obj")
	ply := load(t, run(t, obj, "obj", "ply")[0], "ply")
	p := ply.Parts[0]
	if p.Normals == nil {
		t.Fatal("no normals")
	}
	up := 0
	for v := 0; v < p.VertexCount(); v++ {
		near(t, 1, float64(p.Normal(v).length()), 1e-3, "normal length")
		if q := p.Position(v); q.Y == 0 && q.length() > 9.9 && p.Normal(v).Y > 0.99 {
			up++
		}
	}
	if up != 6 {
		t.Fatal(up)
	}
	// A face without normals among faces with them: the part's normals are computed from the faces.
	path := filepath.Join(t.TempDir(), "partial.obj")
	os.WriteFile(path, []byte("v 0 0 0\nv 1 0 0\nv 0 0 -1\nv 1 0 -1\nvn 0 1 0\nf 1//1 2//1 3//1\nf 2 4 3\n"), 0o644)
	partial := load(t, path, "obj").Parts[0]
	for v := 0; v < partial.VertexCount(); v++ {
		near(t, 1, float64(partial.Normal(v).Y), 1e-4, "computed normal")
	}
}

func decodePNG(t *testing.T, path string) *image.NRGBA {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := img.(*image.NRGBA); ok {
		return n
	}
	out := image.NewNRGBA(img.Bounds())
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			out.Set(x, y, img.At(x, y))
		}
	}
	return out
}

func TestARotatePresetTurnsTheRenderedPictureOnce(t *testing.T) {
	source := save(t, fanModel(), t.TempDir(), "fan", "stl")
	plain := decodePNG(t, run(t, source, "stl", "png")[0])
	out, err := convertFile(t, source, "stl", "png", func(p *presets.Preset) { p.Image.Rotation = presets.Left90 })
	if err != nil {
		t.Fatal(err)
	}
	turned := decodePNG(t, out[0])
	expected := rotate(plain, 270)
	diff := func(a, b *image.NRGBA) float64 {
		var s float64
		for i := range a.Pix {
			d := float64(a.Pix[i]) - float64(b.Pix[i])
			s += d * d
		}
		return math.Sqrt(s/float64(len(a.Pix))) / 255
	}
	if diff(plain, expected) <= 0.01 || diff(turned, expected) >= 0.01 {
		t.Fatalf("plain %.4f, turned %.4f", diff(plain, expected), diff(turned, expected))
	}
}

func TestAModelRendersShadedWithTransparentBackground(t *testing.T) {
	source := save(t, sphereModel(), t.TempDir(), "ball", "ply")
	img := decodePNG(t, run(t, source, "ply", "png")[0])
	if img.Bounds().Dx() != 1024 || img.Bounds().Dy() != 1024 {
		t.Fatal(img.Bounds())
	}
	if img.NRGBAAt(2, 2).A != 0 || img.NRGBAAt(512, 512).A != 255 {
		t.Fatal("background or centre")
	}
	colors := map[[3]uint8]bool{}
	for i := 0; i < 16; i++ {
		c := img.NRGBAAt(300+i*25, 512)
		if c.A == 255 {
			colors[[3]uint8{c.R, c.G, c.B}] = true
		}
	}
	if len(colors) <= 4 {
		t.Fatal(len(colors)) // the sphere's colours come through
	}
	jpg := run(t, source, "ply", "jpg")[0]
	data, _ := os.ReadFile(jpg)
	if _, format, err := image.Decode(bytes.NewReader(data)); err != nil || format != "jpeg" {
		t.Fatal(format, err)
	}
}

func TestALargeStlConvertsQuickly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terrain.stl")
	grid := 708 // 708 × 708 × 2 = 1,002,528 triangles
	if testing.Short() {
		grid = 200
	}
	var b bytes.Buffer
	header := make([]byte, 84)
	binary.LittleEndian.PutUint32(header[80:], uint32(grid*grid*2))
	b.Write(header)
	p := func(x, y int) vec3 {
		return vec3{float32(x), float32(y), float32(math.Sin(float64(x)*0.05) * math.Cos(float64(y)*0.05) * 5)}
	}
	for y := 0; y < grid; y++ {
		for x := 0; x < grid; x++ {
			stlFacet(&b, p(x, y), p(x+1, y), p(x+1, y+1))
			stlFacet(&b, p(x, y), p(x+1, y+1), p(x, y+1))
		}
	}
	os.WriteFile(path, b.Bytes(), 0o644)
	start := time.Now()
	glb := run(t, path, "stl", "glb")[0]
	if d := time.Since(start); d > 60*time.Second {
		t.Fatalf("took %v", d)
	}
	m := load(t, glb, "glb")
	if m.TriangleCount() != grid*grid*2 || m.VertexCount() != (grid+1)*(grid+1) {
		t.Fatal(m.TriangleCount(), m.VertexCount()) // merged by position
	}
}

func TestBrokenFilesFailWithAClearMessage(t *testing.T) {
	folder := t.TempDir()
	write := func(name, text string) string {
		p := filepath.Join(folder, name)
		os.WriteFile(p, []byte(text), 0o644)
		return p
	}
	header := make([]byte, 84+50)
	binary.LittleEndian.PutUint32(header[80:], 10)
	cut := write("cut.stl", string(header))
	cases := []struct{ path, from, want string }{
		{cut, "stl", "cut short"},
		{write("bad.obj", "v 0 0 0\nv 1 0 0\nv 0 1 0\nf 1 2 7\n"), "obj", "vertex 7, which does not exist"},
		{write("points.obj", "v 0 0 0\nv 1 0 0\n"), "obj", "contains no triangles"},
		{write("text.ply", "hello"), "ply", "not a PLY file"},
		{write("text.3mf", "hello"), "3mf", "not a 3MF package"},
		{write("text.gltf", `{ "asset": `), "gltf", "glTF"},
		{write("draco.gltf", `{"asset":{"version":"2.0"},"extensionsUsed":["KHR_draco_mesh_compression"],"extensionsRequired":["KHR_draco_mesh_compression"],
 "meshes":[{"primitives":[{"attributes":{"POSITION":0},"extensions":{"KHR_draco_mesh_compression":{"bufferView":0,"attributes":{"POSITION":0}}}}]}],
 "accessors":[{"componentType":5126,"count":3,"type":"VEC3","min":[0,0,0],"max":[1,1,0]}],
 "bufferViews":[{"buffer":0,"byteLength":4}],"buffers":[{"byteLength":4,"uri":"data:application/octet-stream;base64,`+base64.StdEncoding.EncodeToString([]byte{0, 0, 0, 0})+`"}],
 "nodes":[{"mesh":0}],"scenes":[{"nodes":[0]}],"scene":0}`), "gltf", "Draco-compressed"},
	}
	for _, c := range cases {
		if msg := fail(t, c.path, c.from, "stl"); !strings.Contains(msg, c.want) {
			t.Errorf("%s: %q does not say %q", filepath.Base(c.path), msg, c.want)
		}
	}
}
