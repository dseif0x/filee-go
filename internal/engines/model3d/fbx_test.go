package model3d

// FBX (read only) into the 3D formats and pictures. The files are written
// here: binary 7.4 and 7.5, with plain and compressed arrays, and ASCII; then
// converted through the engine or read directly.

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/formats"
)

// fn is a test FBX node: name, properties (int, int64, float64, string, []byte, []float64, []int32) and children.
type fn struct {
	name     string
	props    []any
	children []*fn
}

func node(name string, props ...any) *fn { return &fn{name: name, props: props} }

func (n *fn) with(children ...*fn) *fn { n.children = append(n.children, children...); return n }

const (
	geometryID = int64(100)
	parentID   = int64(200)
	cubeID     = int64(300)
	materialID = int64(400)
	textureID  = int64(500)
	videoID    = int64(600)
)

// fbxCube is the 8 corners and 6 outward quads of the cube from −1 to 1 (the last corner of each quad bit-inverted).
func fbxCube() ([]float64, []int32) {
	var positions []float64
	for i := 0; i < 8; i++ {
		c := func(bit int) float64 {
			if i&bit == 0 {
				return -1
			}
			return 1
		}
		positions = append(positions, c(1), c(2), c(4))
	}
	quads := [][4]int32{{0, 2, 3, 1}, {4, 5, 7, 6}, {0, 1, 5, 4}, {2, 6, 7, 3}, {0, 4, 6, 2}, {1, 3, 7, 5}}
	var polygons []int32
	for _, q := range quads {
		polygons = append(polygons, q[0], q[1], q[2], ^q[3])
	}
	return positions, polygons
}

func fbxMesh(id int64, name string, positions []float64, polygons []int32, layers ...*fn) *fn {
	return node("Geometry", id, "Geometry::"+name, "Mesh").with(
		node("Vertices", positions), node("PolygonVertexIndex", polygons), node("GeometryVersion", 124)).with(layers...)
}

func fbxNormals(positions []float64, polygons []int32) *fn {
	var normals []float64
	start := 0
	for i, p := range polygons {
		if p >= 0 {
			continue
		}
		at := func(k int) vec3 {
			v := polygons[k]
			if v < 0 {
				v = ^v
			}
			return vec3{float32(positions[v*3]), float32(positions[v*3+1]), float32(positions[v*3+2])}
		}
		n := at(start + 1).sub(at(start)).cross(at(start + 2).sub(at(start))).normalize()
		for k := start; k <= i; k++ {
			normals = append(normals, float64(n.X), float64(n.Y), float64(n.Z))
		}
		start = i + 1
	}
	return node("LayerElementNormal", 0).with(node("Version", 101), node("MappingInformationType", "ByPolygonVertex"),
		node("ReferenceInformationType", "Direct"), node("Normals", normals))
}

func fbxMaterialLayer(mapping string, materials ...int32) *fn {
	return node("LayerElementMaterial", 0).with(node("Version", 101), node("MappingInformationType", mapping),
		node("ReferenceInformationType", "IndexToDirect"), node("Materials", materials))
}

type fbxProp struct {
	name  string
	value vec3
}

func fbxModel(id int64, name, typ string, props ...fbxProp) *fn {
	list := node("Properties70")
	for _, p := range props {
		list.with(node("P", p.name, p.name, "", "A", float64(p.value.X), float64(p.value.Y), float64(p.value.Z)))
	}
	return node("Model", id, "Model::"+name, typ).with(node("Version", 232), list)
}

func fbxColorProperty(name string, c vec3) *fn {
	return node("P", name, "Color", "", "A", float64(c.X), float64(c.Y), float64(c.Z))
}

func fbxMaterial(id int64, name string, c vec3) *fn {
	return node("Material", id, "Material::"+name, "").with(node("ShadingModel", "phong"), node("Properties70").with(fbxColorProperty("DiffuseColor", c)))
}

func fbxLink(child, parent int64) *fn { return node("C", "OO", child, parent) }

func fbxIntProperty(name string, v int) *fn { return node("P", name, "int", "Integer", "", v) }

func fbxDocument(version, upAxis int, unitScale float64, objects, connections *fn) []*fn {
	front, frontSign := 2, 1
	if upAxis == 2 {
		front, frontSign = 1, -1
	}
	return []*fn{
		node("FBXHeaderExtension").with(node("FBXHeaderVersion", 1003), node("FBXVersion", version), node("Creator", "Filee tests")),
		node("GlobalSettings").with(node("Version", 1000), node("Properties70").with(
			fbxIntProperty("UpAxis", upAxis), fbxIntProperty("UpAxisSign", 1),
			// Z up as 3ds Max writes it: front is −Y.
			fbxIntProperty("FrontAxis", front), fbxIntProperty("FrontAxisSign", frontSign),
			fbxIntProperty("CoordAxis", 0), fbxIntProperty("CoordAxisSign", 1),
			node("P", "UnitScaleFactor", "double", "Number", "", unitScale))),
		node("Definitions").with(node("Version", 100), node("ObjectType", "Material").with(node("Count", 1),
			node("PropertyTemplate", "FbxSurfacePhong").with(node("Properties70").with(
				fbxColorProperty("DiffuseColor", vec3{0.8, 0.8, 0.8}), node("P", "Opacity", "double", "Number", "", 0.5))))),
		objects,
		connections,
	}
}

// cubeScene: a cube from −1 to 1 (quads, normals per corner, UVs by index) in
// model "Cube" (scaled 1, 2, 3, turned 90° about Z, moved 5 up) under model
// "Parent" (moved 10 along X), with the red material "Red" whose opacity 0.5
// comes from the material template. In centimetres and Y up the cube spans
// (80, 40, −30)–(120, 60, 30) mm.
func cubeScene(version, upAxis int, unitScale float64, texture []byte) []*fn {
	positions, polygons := fbxCube()
	uvIndex := make([]int32, len(polygons))
	for i := range uvIndex {
		uvIndex[i] = int32(i % 4)
	}
	objects := node("Objects").with(
		fbxMesh(geometryID, "Cube", positions, polygons, fbxNormals(positions, polygons),
			node("LayerElementUV", 0).with(node("Version", 101), node("Name", "UVMap"), node("MappingInformationType", "ByPolygonVertex"),
				node("ReferenceInformationType", "IndexToDirect"), node("UV", []float64{0, 0, 1, 0, 1, 1, 0, 1}), node("UVIndex", uvIndex)),
			fbxMaterialLayer("AllSame", 0)),
		fbxModel(parentID, "Parent", "Null", fbxProp{"Lcl Translation", vec3{10, 0, 0}}),
		fbxModel(cubeID, "Cube", "Mesh", fbxProp{"Lcl Translation", vec3{0, 5, 0}}, fbxProp{"Lcl Rotation", vec3{0, 0, 90}}, fbxProp{"Lcl Scaling", vec3{1, 2, 3}}),
		fbxMaterial(materialID, "Red", vec3{1, 0, 0}))
	connections := node("Connections").with(fbxLink(parentID, 0), fbxLink(cubeID, parentID), fbxLink(geometryID, cubeID), fbxLink(materialID, cubeID))
	if texture != nil {
		objects.with(
			node("Texture", textureID, "Texture::Checker", "").with(node("Type", "TextureVideoClip"),
				node("FileName", `C:\Somewhere\else\checker.png`), node("RelativeFilename", "checker.png")),
			node("Video", videoID, "Video::Checker", "Clip").with(node("Type", "Clip"), node("RelativeFilename", "checker.png"), node("Content", texture)))
		connections.with(node("C", "OP", textureID, materialID, "DiffuseColor"), fbxLink(videoID, textureID))
	}
	return fbxDocument(version, upAxis, unitScale, objects, connections)
}

// binaryName is "Model::Cube" as binary FBX writes object names: "Cube\x00\x01Model".
func binaryName(s string) string {
	if i := strings.Index(s, "::"); i > 0 {
		switch s[:i] {
		case "Model", "Geometry", "Material", "Texture", "Video":
			return s[i+2:] + "\x00\x01" + s[:i]
		}
	}
	return s
}

func writeFbxBinary(t testing.TB, path string, nodes []*fn, version int, compress bool) {
	wide := version >= 7500
	var b bytes.Buffer
	b.WriteString("Kaydara FBX Binary  \x00")
	b.WriteByte(0x1A)
	b.WriteByte(0)
	binary.Write(&b, binary.LittleEndian, uint32(version))
	nullSize := 13
	if wide {
		nullSize = 25
	}
	var writeNode func(n *fn)
	writeNode = func(n *fn) {
		start := b.Len()
		if wide {
			b.Write(make([]byte, 24))
		} else {
			b.Write(make([]byte, 12))
		}
		b.WriteByte(byte(len(n.name)))
		b.WriteString(n.name)
		propsStart := b.Len()
		for _, p := range n.props {
			writeFbxProperty(&b, p, compress)
		}
		propsLen := b.Len() - propsStart
		if len(n.children) > 0 {
			for _, c := range n.children {
				writeNode(c)
			}
			b.Write(make([]byte, nullSize))
		}
		end := b.Len()
		h := b.Bytes()[start:]
		if wide {
			binary.LittleEndian.PutUint64(h, uint64(end))
			binary.LittleEndian.PutUint64(h[8:], uint64(len(n.props)))
			binary.LittleEndian.PutUint64(h[16:], uint64(propsLen))
		} else {
			binary.LittleEndian.PutUint32(h, uint32(end))
			binary.LittleEndian.PutUint32(h[4:], uint32(len(n.props)))
			binary.LittleEndian.PutUint32(h[8:], uint32(propsLen))
		}
	}
	for _, n := range nodes {
		writeNode(n)
	}
	b.Write(make([]byte, nullSize))
	b.Write(make([]byte, 16)) // stands for the footer
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeFbxProperty(b *bytes.Buffer, p any, compress bool) {
	le := binary.LittleEndian
	array := func(typ byte, count int, data []byte) {
		b.WriteByte(typ)
		binary.Write(b, le, uint32(count))
		if !compress {
			binary.Write(b, le, uint32(0))
			binary.Write(b, le, uint32(len(data)))
			b.Write(data)
			return
		}
		var packed bytes.Buffer
		z := zlib.NewWriter(&packed)
		z.Write(data)
		z.Close()
		binary.Write(b, le, uint32(1))
		binary.Write(b, le, uint32(packed.Len()))
		b.Write(packed.Bytes())
	}
	switch v := p.(type) {
	case int:
		b.WriteByte('I')
		binary.Write(b, le, int32(v))
	case int64:
		b.WriteByte('L')
		binary.Write(b, le, v)
	case float64:
		b.WriteByte('D')
		binary.Write(b, le, v)
	case string:
		s := binaryName(v)
		b.WriteByte('S')
		binary.Write(b, le, uint32(len(s)))
		b.WriteString(s)
	case []byte:
		b.WriteByte('R')
		binary.Write(b, le, uint32(len(v)))
		b.Write(v)
	case []float64:
		var d bytes.Buffer
		binary.Write(&d, le, v)
		array('d', len(v), d.Bytes())
	case []int32:
		var d bytes.Buffer
		binary.Write(&d, le, v)
		array('i', len(v), d.Bytes())
	default:
		panic(fmt.Sprintf("no FBX property type for %T", p))
	}
}

func fbxFormat(v any) string {
	switch x := v.(type) {
	case string:
		return `"` + x + `"`
	case []byte:
		return `"` + base64.StdEncoding.EncodeToString(x) + `"`
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case int32:
		return strconv.Itoa(int(x))
	}
	return fmt.Sprint(v)
}

func writeFbxASCII(t testing.TB, path string, nodes []*fn) {
	var b strings.Builder
	b.WriteString("; FBX 7.4.0 project file\n; Written by the Filee tests\n\n")
	var write func(n *fn, depth int)
	write = func(n *fn, depth int) {
		indent := strings.Repeat("\t", depth)
		b.WriteString(indent + n.name + ":")
		if len(n.props) == 1 {
			var values []string
			switch a := n.props[0].(type) {
			case []float64:
				for _, v := range a {
					values = append(values, fbxFormat(v))
				}
			case []int32:
				for _, v := range a {
					values = append(values, fbxFormat(v))
				}
			}
			if values != nil {
				fmt.Fprintf(&b, " *%d {\n%s\ta: ", len(values), indent)
				// Long arrays are wrapped after a comma, as the FBX SDK does.
				for i, v := range values {
					switch {
					case i == 0:
					case i%8 == 0:
						b.WriteString(",\n" + indent + "\t")
					default:
						b.WriteString(",")
					}
					b.WriteString(v)
				}
				b.WriteString("\n" + indent + "} \n")
				return
			}
		}
		// Embedded files start with an empty value: "Content: , "base64...""
		if len(n.props) > 0 {
			if _, ok := n.props[0].([]byte); ok {
				b.WriteString(" , ")
			} else {
				b.WriteString(" ")
			}
		} else {
			b.WriteString(" ")
		}
		var parts []string
		for _, p := range n.props {
			parts = append(parts, fbxFormat(p))
		}
		b.WriteString(strings.Join(parts, ", "))
		if len(n.children) > 0 {
			b.WriteString(" {\n")
			for _, c := range n.children {
				write(c, depth+1)
			}
			b.WriteString(indent + "}")
		}
		b.WriteString("\n")
	}
	for _, n := range nodes {
		write(n, 0)
	}
	os.WriteFile(path, []byte(b.String()), 0o644)
}

func fbxBinaryFile(t *testing.T, nodes []*fn, version int, compress bool) string {
	path := filepath.Join(t.TempDir(), "model.fbx")
	writeFbxBinary(t, path, nodes, version, compress)
	return path
}

var cubeMin, cubeMax = vec3{80, 40, -30}, vec3{120, 60, 30}

func TestFbxIsReadOnlyAndOneStepToEvery3dFormatAndPicture(t *testing.T) {
	if f := formats.ByID("fbx"); f == nil || f.Writable() || formats.ByExtension(".FBX").Category != formats.Model3D {
		t.Fatal("format table")
	}
	planner := convert.NewPlanner([]convert.Converter{New()}, nil)
	for _, to := range append(Formats, "png", "jpg") {
		if r := planner.Plan("fbx", to, convert.Options{}); r == nil || len(r.Steps) != 1 {
			t.Fatal(to)
		}
	}
}

func TestBinaryFbxKeepsHierarchyTransformsAndMaterial(t *testing.T) {
	for _, c := range []struct {
		version  int
		compress bool
	}{{7400, false}, {7400, true}, {7500, false}, {7500, true}} {
		t.Run(fmt.Sprint(c.version, c.compress), func(t *testing.T) {
			fbx := fbxBinaryFile(t, cubeScene(c.version, 1, 1, nil), c.version, c.compress)
			m := load(t, run(t, fbx, "fbx", "glb")[0], "glb")
			if m.TriangleCount() != 12 || m.VertexCount() != 24 {
				t.Fatal(m.TriangleCount(), m.VertexCount()) // a corner per face: the normals differ
			}
			lo, hi := m.Bounds()
			closeV3(t, cubeMin, lo, 1e-2)
			closeV3(t, cubeMax, hi, 1e-2)
			// 40 × 20 × 60 mm, closed and facing outwards after the turn and the scaling.
			near(t, 40*20*60, signedVolume(m), 1, "volume")
			if len(m.Parts) != 1 {
				t.Fatal(len(m.Parts))
			}
			p := m.Parts[0]
			if p.Normals == nil || p.TexCoords == nil || p.Name != "Cube" {
				t.Fatal("attributes or name")
			}
			center := lo.add(hi).scale(0.5)
			for tr := 0; tr < p.TriangleCount(); tr++ {
				a := p.Position(p.Indices[tr*3])
				if p.Normal(p.Indices[tr*3]).dot(a.sub(center)) <= 0 {
					t.Fatalf("triangle %d has an inward normal", tr)
				}
			}
			// Red from the material, opacity 0.5 from the material template.
			closeV4(t, vec4{1, 0, 0, 0.5}, m.Materials[0].BaseColor, 1e-4)
		})
	}
}

func TestAsciiFbxReadsLikeBinaryAndBecomesObj(t *testing.T) {
	ascii := filepath.Join(t.TempDir(), "ascii.fbx")
	writeFbxASCII(t, ascii, cubeScene(7400, 1, 1, nil))
	m := load(t, ascii, "fbx")
	b := load(t, fbxBinaryFile(t, cubeScene(7400, 1, 1, nil), 7400, false), "fbx")
	if m.TriangleCount() != b.TriangleCount() || len(positionSet(m)) != len(positionSet(b)) {
		t.Fatal("ASCII differs")
	}
	for k := range positionSet(b) {
		if !positionSet(m)[k] {
			t.Fatal(k)
		}
	}
	lo, hi := m.Bounds()
	closeV3(t, cubeMin, lo, 1e-2)
	closeV3(t, cubeMax, hi, 1e-2)
	outputs := run(t, ascii, "fbx", "obj")
	var obj, mtl string
	for _, o := range outputs {
		switch filepath.Ext(o) {
		case ".obj":
			obj = o
		case ".mtl":
			mtl = o
		}
	}
	om := load(t, obj, "obj")
	if om.TriangleCount() != 12 || len(om.Materials) != 1 {
		t.Fatal("obj")
	}
	closeV4(t, vec4{1, 0, 0, 0.5}, om.Materials[0].BaseColor, 1e-4)
	if text, _ := os.ReadFile(mtl); !strings.Contains(string(text), "Kd 1 0 0") {
		t.Fatal(string(text))
	}
}

func TestZUpFbxInMetresStandsUpInGltf(t *testing.T) {
	// In metres, the cube spans (8, 4, −3)–(12, 6, 3) with Z up; Y up that is (8, −3, −6)–(12, 3, −4).
	fbx := fbxBinaryFile(t, cubeScene(7400, 2, 100, nil), 7400, true)
	glb := run(t, fbx, "fbx", "glb")[0]
	doc := glbJSON(t, glb)
	pos := doc.Accessors[doc.Meshes[0].Primitives[0].Attributes["POSITION"]]
	closeV3(t, vec3{8, -3, -6}, vec3{float32(pos.Min[0]), float32(pos.Min[1]), float32(pos.Min[2])}, 1e-4)
	closeV3(t, vec3{12, 3, -4}, vec3{float32(pos.Max[0]), float32(pos.Max[1]), float32(pos.Max[2])}, 1e-4)
	// The axis change is a turn, not a mirror: still closed and outward.
	near(t, 48, signedVolume(load(t, glb, "glb"))/1e9, 1e-3, "volume") // 4 × 6 × 2 m
}

func TestFbxRotationOrderPreRotationPolygonsAndMaterialsByPolygon(t *testing.T) {
	// "Turned": one triangle (0,0,0) (2,0,0) (0,1,0), rotation (90, 90, 0) in
	// ZYX order (Y first, then X), then a pre-rotation of 90° about Z.
	// "Shapes": a triangle (material 0) and a concave pentagon (material 1, area 10 cm²).
	turned := fbxModel(301, "Turned", "Mesh", fbxProp{"Lcl Rotation", vec3{90, 90, 0}}, fbxProp{"PreRotation", vec3{0, 0, 90}})
	turned.children[1].with(fbxIntProperty("RotationOrder", 5))
	objects := node("Objects").with(
		fbxMesh(101, "Triangle", []float64{0, 0, 0, 2, 0, 0, 0, 1, 0}, []int32{0, 1, ^2}),
		turned,
		fbxMesh(102, "Shapes", []float64{10, 0, 0, 11, 0, 0, 10, 1, 0, 0, 0, 0, 4, 0, 0, 4, 4, 0, 2, 1, 0, 0, 4, 0},
			[]int32{0, 1, ^2, 3, 4, 5, 6, ^7}, fbxMaterialLayer("ByPolygon", 0, 1)),
		fbxModel(302, "Shapes", "Mesh"),
		fbxMaterial(401, "Red", vec3{1, 0, 0}),
		fbxMaterial(402, "Blue", vec3{0, 0, 1}))
	connections := node("Connections").with(fbxLink(301, 0), fbxLink(101, 301), fbxLink(302, 0), fbxLink(102, 302), fbxLink(401, 302), fbxLink(402, 302))
	m := load(t, fbxBinaryFile(t, fbxDocument(7400, 1, 1, objects, connections), 7400, false), "fbx")
	var shapes []*Part
	for _, p := range m.Parts {
		switch p.Name {
		case "Turned":
			lo, hi := (&Model{Parts: []*Part{p}}).Bounds()
			closeV3(t, vec3{-20, 0, 0}, lo, 1e-3)
			closeV3(t, vec3{0, 0, 10}, hi, 1e-3)
		case "Shapes":
			shapes = append(shapes, p)
		}
	}
	if len(shapes) != 2 {
		t.Fatal(len(shapes))
	}
	for _, p := range shapes {
		switch m.MaterialOf(p).Name {
		case "Red":
			if p.TriangleCount() != 1 {
				t.Fatal(p.TriangleCount())
			}
		case "Blue":
			if p.TriangleCount() != 3 {
				t.Fatal(p.TriangleCount())
			}
			closeV4(t, vec4{0, 0, 1, 0.5}, m.MaterialOf(p).BaseColor, 1e-4)
			// Ear clipping keeps the notch: 4 × 4 − 6 = 10 cm² = 1000 mm².
			near(t, 1000, area(&Model{Parts: []*Part{p}}), 1e-2, "pentagon area")
		default:
			t.Fatal(m.MaterialOf(p).Name)
		}
	}
}

func TestAnEmbeddedFbxTextureGoesIntoTheGlb(t *testing.T) {
	fbx := fbxBinaryFile(t, cubeScene(7400, 1, 1, checker()), 7400, true)
	m := load(t, run(t, fbx, "fbx", "glb")[0], "glb")
	if !bytes.Equal(m.Materials[0].BaseColorTexture.Data, checker()) {
		t.Fatal("texture")
	}
	// ASCII files embed it as base64.
	ascii := filepath.Join(t.TempDir(), "textured.fbx")
	writeFbxASCII(t, ascii, cubeScene(7400, 1, 1, checker()))
	tex := load(t, ascii, "fbx").Materials[0].BaseColorTexture
	if tex == nil || !bytes.Equal(tex.Data, checker()) || tex.Extension != "png" || tex.Name != "checker" {
		t.Fatalf("%+v", tex)
	}
}

func TestAFbxTextureFileNextToTheModelIsFound(t *testing.T) {
	fbx := fbxBinaryFile(t, cubeScene(7400, 1, 1, []byte{}), 7400, false)
	os.WriteFile(filepath.Join(filepath.Dir(fbx), "checker.png"), checker(), 0o644)
	m := load(t, run(t, fbx, "fbx", "glb")[0], "glb")
	if m.Materials[0].BaseColorTexture == nil || !bytes.Equal(m.Materials[0].BaseColorTexture.Data, checker()) {
		t.Fatal("texture")
	}
}

func TestFbxRendersAPngInTheMaterialColour(t *testing.T) {
	img := decodePNG(t, run(t, fbxBinaryFile(t, cubeScene(7400, 1, 1, nil), 7400, false), "fbx", "png")[0])
	opaque, reddish := 0, 0
	for i := 0; i+3 < len(img.Pix); i += 4 {
		if img.Pix[i+3] < 128 {
			continue
		}
		opaque++
		if int(img.Pix[i]) > int(img.Pix[i+1])*2 && int(img.Pix[i]) > int(img.Pix[i+2])*2 {
			reddish++
		}
	}
	if opaque <= 1000 || reddish <= opaque/2 {
		t.Fatal(opaque, reddish)
	}
}

func TestBrokenFbxFilesFailWithAClearMessage(t *testing.T) {
	folder := t.TempDir()
	text := filepath.Join(folder, "text.fbx")
	os.WriteFile(text, []byte("hello"), 0o644)
	if msg := fail(t, text, "fbx", "glb"); !strings.Contains(msg, "not an FBX model") {
		t.Fatal(msg)
	}
	whole, _ := os.ReadFile(fbxBinaryFile(t, cubeScene(7400, 1, 1, nil), 7400, true))
	cut := filepath.Join(folder, "cut.fbx")
	os.WriteFile(cut, whole[:len(whole)/2], 0o644)
	if msg := fail(t, cut, "fbx", "glb"); !strings.Contains(msg, "cut short or damaged") {
		t.Fatal(msg)
	}
	positions, polygons := fbxCube()
	polygons[5] = 42
	bad := filepath.Join(folder, "bad.fbx")
	writeFbxBinary(t, bad, fbxDocument(7400, 1, 1, node("Objects").with(fbxMesh(100, "Cube", positions, polygons), fbxModel(300, "Cube", "Mesh")),
		node("Connections").with(fbxLink(300, 0), fbxLink(100, 300))), 7400, false)
	if msg := fail(t, bad, "fbx", "stl"); !strings.Contains(msg, "refers to vertex 43") {
		t.Fatal(msg)
	}
	empty := filepath.Join(folder, "empty.fbx")
	writeFbxBinary(t, empty, fbxDocument(7400, 1, 1, node("Objects"), node("Connections")), 7400, false)
	if msg := fail(t, empty, "fbx", "stl"); !strings.Contains(msg, "contains no triangles") {
		t.Fatal(msg)
	}
}

func TestEveryCutOrFlippedFbxByteReadsOrFailsCleanly(t *testing.T) {
	// A damaged file must end in a model or an error: no panic, no hang.
	dir := t.TempDir()
	check := func(data []byte) {
		// Without readFbxData's recover, so a panic fails the test.
		doc, err := parseFbx(context.Background(), data)
		if err != nil {
			return
		}
		if m, err := newFbxScene(doc, dir).build(context.Background()); err == nil {
			m.Validate()
		}
	}
	start := time.Now()
	for _, c := range []struct {
		version  int
		compress bool
	}{{7400, false}, {7500, true}} {
		whole, _ := os.ReadFile(fbxBinaryFile(t, cubeScene(c.version, 1, 1, checker()), c.version, c.compress))
		for n := 0; n < len(whole); n++ {
			check(whole[:n])
		}
		for i := 23; i < len(whole); i++ {
			cp := bytes.Clone(whole)
			if i%3 == 0 {
				cp[i] ^= 0x80
			} else {
				cp[i] ^= 0xFF
			}
			check(cp)
		}
	}
	ascii := filepath.Join(t.TempDir(), "ascii.fbx")
	writeFbxASCII(t, ascii, cubeScene(7400, 1, 1, nil))
	text, _ := os.ReadFile(ascii)
	for n := 0; n < len(text); n++ {
		check(text[:n])
	}
	if d := time.Since(start); d > 60*time.Second {
		t.Fatalf("took %v", d)
	}
}

