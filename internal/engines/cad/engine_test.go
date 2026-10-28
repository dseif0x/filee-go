package cad

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc/dxf"
	"github.com/dseif0x/filee-go/internal/presets"
)

// testDrawing builds a drawing with the common entity kinds.
func testDrawing(t *testing.T) string {
	doc := cadoc.NewDocument()
	ms := doc.ModelSpace()
	red := cadoc.NewLayer("Red")
	red.Color = cadoc.ColorIndex(1)
	doc.Layers.Add(red)

	line := cadoc.NewLine()
	line.StartPoint, line.EndPoint = cadoc.Vec3{X: 0, Y: 0}, cadoc.Vec3{X: 100, Y: 0}
	line.Layer = red
	ms.AddEntity(line)

	circle := cadoc.NewCircle()
	circle.Center, circle.Radius = cadoc.Vec3{X: 50, Y: 50}, 20
	ms.AddEntity(circle)

	text := cadoc.NewText()
	text.Value, text.InsertPoint, text.Height = "Hello %%c50", cadoc.Vec3{X: 10, Y: 80}, 5
	ms.AddEntity(text)

	mt := cadoc.NewMText()
	mt.Value, mt.InsertPoint, mt.Height, mt.RectangleWidth = `{\H1.5x;Big} text\Psecond line`, cadoc.Vec3{X: 60, Y: 90}, 3, 40
	ms.AddEntity(mt)

	hatch := cadoc.NewHatch()
	hatch.IsSolid = true
	hatch.Color = cadoc.ColorIndex(3)
	hatch.Paths = []*cadoc.HatchBoundaryPath{{Edges: []cadoc.HatchEdge{
		&cadoc.HatchLineEdge{Start: cadoc.Vec2{X: 80, Y: 10}, End: cadoc.Vec2{X: 95, Y: 10}},
		&cadoc.HatchLineEdge{Start: cadoc.Vec2{X: 95, Y: 10}, End: cadoc.Vec2{X: 95, Y: 25}},
		&cadoc.HatchLineEdge{Start: cadoc.Vec2{X: 95, Y: 25}, End: cadoc.Vec2{X: 80, Y: 10}},
	}}}
	ms.AddEntity(hatch)

	block := cadoc.NewBlockRecord("Box")
	doc.BlockRecords.Add(block)
	for _, seg := range [][4]float64{{0, 0, 5, 0}, {5, 0, 5, 5}, {5, 5, 0, 5}, {0, 5, 0, 0}} {
		l := cadoc.NewLine()
		l.StartPoint, l.EndPoint = cadoc.Vec3{X: seg[0], Y: seg[1]}, cadoc.Vec3{X: seg[2], Y: seg[3]}
		block.AddEntity(l)
	}
	ins := cadoc.NewInsert(block)
	ins.InsertPoint = cadoc.Vec3{X: 10, Y: 10}
	ins.XScale, ins.YScale, ins.ZScale = 2, 2, 1
	ms.AddEntity(ins)

	dim := cadoc.NewDimensionAligned()
	dim.FirstPoint, dim.SecondPoint = cadoc.Vec3{X: 0, Y: -10}, cadoc.Vec3{X: 100, Y: -10}
	dim.DefinitionPoint = cadoc.Vec3{X: 100, Y: -20}
	ms.AddEntity(dim)
	if err := cadoc.UpdateDimensionBlock(doc, dim); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "drawing.dxf")
	if err := dxf.WriteFile(path, doc, dxf.WriteConfig{}, nil); err != nil {
		t.Fatal(err)
	}
	return path
}

func step(t *testing.T, in, from, to string) *convert.Step {
	out := filepath.Join(t.TempDir(), "out")
	return &convert.Step{InputPath: in, From: from, To: to, Preset: presets.New("t", to), WorkDir: t.TempDir(),
		Output: convert.AllocatorFunc(func(ext, suffix string) string { return out + suffix + "." + ext })}
}

func run(t *testing.T, s *convert.Step) string {
	out, err := New().Convert(context.Background(), s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("outputs %v", out)
	}
	return out[0]
}

func TestDxfToPng(t *testing.T) {
	in := testDrawing(t)
	out := run(t, step(t, in, "dxf", "png"))
	f, _ := os.Open(out)
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if b.Dx() < 1000 {
		t.Fatalf("size %v", b)
	}
	// Something red (the line on layer Red) and something green (the hatch).
	var red, green bool
	for y := 0; y < b.Dy(); y += 2 {
		for x := 0; x < b.Dx(); x += 2 {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r>>8 > g>>8+80 && r>>8 > bl>>8+80 {
				red = true
			}
			if g>>8 > 200 && r>>8 < 60 && bl>>8 < 60 {
				green = true
			}
		}
	}
	if !red || !green {
		t.Fatalf("red %v green %v", red, green)
	}
}

func TestDxfToPdfAndSvgBlackAndWhite(t *testing.T) {
	in := testDrawing(t)
	s := step(t, in, "dxf", "pdf")
	s.Preset.Cad.Colors = presets.CadBlackAndWhite
	pdf, _ := os.ReadFile(run(t, s))
	// A3 portrait (the drawing is taller than wide).
	if !bytes.HasPrefix(pdf, []byte("%PDF")) || !bytes.Contains(pdf, []byte("/MediaBox[0 0 841.8898 1190.5512]")) {
		i := bytes.Index(pdf, []byte("/MediaBox"))
		t.Fatalf("pdf: %q", pdf[i:i+40])
	}
	svgOut, _ := os.ReadFile(run(t, step(t, in, "dxf", "svg")))
	if !strings.Contains(string(svgOut), "<svg") || !strings.Contains(string(svgOut), "#ff0000") {
		t.Fatal("svg without the red line")
	}
}

func TestDxfToDxfVersions(t *testing.T) {
	in := testDrawing(t)
	for _, v := range []presets.CadVersion{presets.AutoCad2000, presets.AutoCad2007, presets.AutoCad2018} {
		s := step(t, in, "dxf", "dxf")
		s.Preset.Cad.Version = v
		out := run(t, s)
		doc, err := dxf.ReadFile(out, dxf.ReadConfig{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		want := VersionFor(v, cadoc.AC1032, "dxf")
		if doc.Header.Version != want {
			t.Fatalf("version %v want %v", doc.Header.Version, want)
		}
		if n := len(doc.ModelSpace().Entities); n < 7 {
			for _, e := range doc.ModelSpace().Entities {
				t.Log(e.ObjectName())
			}
			t.Fatalf("%v: %d entities", v, n)
		}
	}
}

func TestVersionFor(t *testing.T) {
	if VersionFor(presets.AutoCad2007, cadoc.AC1032, "dwg") != cadoc.AC1018 {
		t.Fatal("2007 DWG should be written as 2004")
	}
	if VersionFor(presets.SameAsSourceVersion, cadoc.AC1021, "dwg") != cadoc.AC1024 {
		t.Fatal("2007 source DWG should be written as 2010")
	}
	if VersionFor(presets.SameAsSourceVersion, cadoc.AC1009, "dxf") != cadoc.AC1015 {
		t.Fatal("R12 should become 2000")
	}
}

func TestCodePage(t *testing.T) {
	if p := ChooseCodePage([]string{"도면 제목"}, 1252); p != 949 {
		t.Fatalf("Korean picked %d", p)
	}
	if p := ChooseCodePage([]string{"Größe"}, 1252); p != 1252 {
		t.Fatalf("German picked %d", p)
	}
	if p := ChooseCodePage([]string{"Привет"}, 1252); p != 1251 {
		t.Fatalf("Russian picked %d", p)
	}
}

func TestTextCodes(t *testing.T) {
	if got := DecodeText(`50%%d \U+00B1%%p`); got != "50° ±±" {
		t.Fatalf("got %q", got)
	}
	ps := ParseMText(`a\Pb{\H2x;c}\S1/2;`, MTextFormat{Height: 1, ColorIndex: -1, TrueColor: -1, WidthFactor: 1})
	if len(ps) != 2 || ps[1][1].Format.Height != 2 || ps[1][2].Text != "1/2" {
		t.Fatalf("%+v", ps)
	}
}

func TestPalette(t *testing.T) {
	p := Palette{Colors: presets.CadGrayscale}
	if g := p.Ink(RGB{255, 255, 0}); g.R != g.G || g.R > 160 {
		t.Fatalf("yellow ink %v", g)
	}
	if a := (Palette{Colors: presets.CadBlackAndWhite}).Area(RGB{255, 255, 0}); a.R < 200 {
		t.Fatalf("yellow area %v", a)
	}
}

func TestNotADrawing(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "x.dwg")
	os.WriteFile(bad, []byte("hello"), 0o644)
	if _, err := New().Convert(context.Background(), step(t, bad, "dwg", "pdf"), nil); err == nil || !strings.Contains(err.Error(), "not a DWG") {
		t.Fatalf("err %v", err)
	}
}
