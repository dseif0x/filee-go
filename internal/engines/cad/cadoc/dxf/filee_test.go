package dxf

import (
	"bytes"
	"os"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// TestFileeFlow mimics how Filee's CadFile uses the library: read leniently
// with defaults, compute dimension measurements, drop entities the target
// version lacks and stray SEQENDs, rename layers in the code page, write an
// older version and read it back.
func TestFileeFlow(t *testing.T) {
	data, err := os.ReadFile("testdata/r12_blocks.dxf")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := ReadBytes(data, ReadConfig{Failsafe: true, CreateDefaults: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ms := doc.ModelSpace()
	mesh := cadoc.NewMesh()
	mesh.Vertices = []cadoc.Vec3{{}, {X: 1}, {Y: 1}}
	mesh.Faces = [][]int32{{0, 1, 2}}
	ms.AddEntity(mesh)
	dim := cadoc.NewDimensionAligned()
	dim.FirstPoint, dim.SecondPoint, dim.DefinitionPoint = cadoc.Vec3{}, cadoc.Vec3{X: 10}, cadoc.Vec3{X: 10, Y: 2}
	ms.AddEntity(dim)
	if m, err := dim.Measurement(); err != nil || m != 10 {
		t.Fatalf("measurement %v %v", m, err)
	}
	if err := cadoc.UpdateDimensionBlock(doc, dim); err != nil || dim.Block == nil || len(dim.Block.Entities) == 0 {
		t.Fatalf("UpdateDimensionBlock: %v", err)
	}

	doc.Header.SetVersion(cadoc.AC1018)
	dropped := 0
	for _, r := range doc.BlockRecords.Entries() {
		dropped += r.RemoveEntities(func(e cadoc.Entity) bool {
			switch e.(type) {
			case *cadoc.Mesh:
				return doc.Header.Version < cadoc.AC1024
			case *cadoc.Seqend:
				return true
			}
			return false
		})
	}
	if dropped != 2 {
		t.Errorf("dropped %d (mesh and the stray SEQEND)", dropped)
	}
	l0, _ := doc.Layers.Get("0")
	walls := cadoc.NewLayer("벽")
	_ = doc.Layers.Add(walls)
	ms.Entities[0].Entity().Layer = walls
	walls.Name = "벽체"
	doc.Header.CodePage = "ANSI_949"
	for _, layout := range doc.Layouts() {
		_ = layout.Name
		_ = layout.TabOrder
		_ = layout.AssociatedBlock
	}
	if _, ok := doc.BlockRecords.TryGetValue(cadoc.PaperSpaceName); !ok {
		t.Fatal("paper space")
	}

	var buf bytes.Buffer
	if err := Write(&buf, doc, WriteConfig{}, nil); err != nil {
		t.Fatal(err)
	}
	checkStructure(t, buf.Bytes())
	back, err := ReadBytes(buf.Bytes(), ReadConfig{Failsafe: true, CreateDefaults: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if back.Header.Version != cadoc.AC1018 || back.Header.CodePage != "ANSI_949" {
		t.Errorf("header %s %s", back.Header.Version, back.Header.CodePage)
	}
	if _, ok := back.Layers.Get("벽체"); !ok {
		t.Error("renamed Korean layer")
	}
	if l0 == nil {
		t.Error("layer 0")
	}
	if len(entitiesByType[*cadoc.Mesh](back.ModelSpace())) != 0 {
		t.Error("mesh written to AC1018")
	}
	dims := entitiesByType[*cadoc.DimensionAligned](back.ModelSpace())
	if len(dims) != 1 || dims[0].Block == nil || len(dims[0].Block.Entities) == 0 {
		t.Errorf("dimension with block: %v", dims)
	}
}

func TestDocumentAPI(t *testing.T) {
	doc := cadoc.NewDocument()
	l := cadoc.NewLayer("A")
	if err := doc.Layers.Add(l); err != nil {
		t.Fatal(err)
	}
	if err := doc.Layers.Add(cadoc.NewLayer("a")); err == nil {
		t.Error("duplicate (case-insensitive) layer accepted")
	}
	l.Name = "B"
	if got, ok := doc.Layers.Get("b"); !ok || got != l {
		t.Error("renamed entry not found")
	}
	if _, ok := doc.Layers.Get("A"); ok {
		t.Error("old name still found")
	}
	if doc.ModelSpace() == nil || doc.PaperSpace() == nil || len(doc.Layouts()) != 2 {
		t.Fatal("defaults")
	}
	line := cadoc.NewLine()
	doc.ModelSpace().AddEntity(line)
	if line.Owner != cadoc.Object(doc.ModelSpace()) {
		t.Error("owner")
	}
	if !doc.ModelSpace().RemoveEntity(line) || len(doc.ModelSpace().Entities) != 0 {
		t.Error("remove")
	}
	doc.AssignHandles()
	idx := doc.Index()
	if len(idx) < 30 {
		t.Errorf("index %d", len(idx))
	}
	if o, ok := doc.GetObject(doc.ModelSpace().Handle); !ok || o != cadoc.Object(doc.ModelSpace()) {
		t.Error("GetObject")
	}
	if doc.Header.HandleSeed <= doc.ModelSpace().Handle {
		t.Error("handle seed")
	}
}
