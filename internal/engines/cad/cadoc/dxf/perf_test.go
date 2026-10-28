package dxf

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

func TestLargeDocument(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	doc := cadoc.NewDocument()
	ms := doc.ModelSpace()
	groups := doc.NamedDictionary(cadoc.AcadGroupDictionary, true)
	for i := 0; i < 2000; i++ {
		b := cadoc.NewBlockRecord(fmt.Sprintf("B%d", i))
		_ = doc.BlockRecords.Add(b)
		l := cadoc.NewLine()
		l.EndPoint = cadoc.Vec3{X: float64(i)}
		b.AddEntity(l)
		ins := cadoc.NewInsert(b)
		ms.AddEntity(ins)
		g := cadoc.NewGroup(fmt.Sprintf("G%d", i))
		g.Add(ins)
		_ = groups.Add(g)
	}
	for i := 0; i < 100000; i++ {
		l := cadoc.NewLine()
		l.EndPoint = cadoc.Vec3{X: float64(i), Y: 1}
		ms.AddEntity(l)
	}
	start := time.Now()
	var buf bytes.Buffer
	if err := Write(&buf, doc, WriteConfig{}, nil); err != nil {
		t.Fatal(err)
	}
	wrote := time.Since(start)
	start = time.Now()
	back, err := ReadBytes(buf.Bytes(), ReadConfig{Failsafe: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d MB, write %v, read %v", buf.Len()>>20, wrote, time.Since(start))
	if len(back.ModelSpace().Entities) != 102000 || len(back.Groups()) != 2000 {
		t.Errorf("entities %d groups %d", len(back.ModelSpace().Entities), len(back.Groups()))
	}
}
