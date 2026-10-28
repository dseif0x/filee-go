package vector

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
	"github.com/dseif0x/filee-go/internal/engines/vector/svg"
	"github.com/dseif0x/filee-go/internal/presets"
)

func init() { gfx.SystemFonts = false }

const testSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="200" height="100">
<rect x="10" y="10" width="80" height="80" fill="red"/>
<circle cx="150" cy="50" r="40" fill="blue" stroke="black"/>
<text x="10" y="95" font-size="10">Hi</text></svg>`

func step(t *testing.T, input, from, to string) (*convert.Step, string) {
	dir := t.TempDir()
	p := presets.New("t", to)
	out := filepath.Join(dir, "out")
	return &convert.Step{
		InputPath: input, From: from, To: to, Preset: p, WorkDir: dir,
		Output: convert.AllocatorFunc(func(ext, suffix string) string { return out + suffix + "." + ext }),
	}, out
}

func write(t *testing.T, name string, data []byte) string {
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func run(t *testing.T, s *convert.Step) []string {
	out, err := New().Convert(context.Background(), s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("outputs %v", out)
	}
	return out
}

func TestSvgToPng(t *testing.T) {
	in := write(t, "a.svg", []byte(testSVG))
	s, _ := step(t, in, "svg", "png")
	out := run(t, s)
	f, _ := os.Open(out[0])
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	// At least 1024 px on the long edge.
	if img.Bounds().Dx() != 1024 || img.Bounds().Dy() != 512 {
		t.Fatalf("size %v", img.Bounds())
	}
	r, g, b, _ := img.At(250, 250).RGBA()
	if r>>8 < 250 || g>>8 > 5 || b>>8 > 5 {
		t.Fatalf("red square pixel %d %d %d", r>>8, g>>8, b>>8)
	}
}

func TestSvgToJpgResize(t *testing.T) {
	in := write(t, "a.svg", []byte(testSVG))
	s, _ := step(t, in, "svg", "jpg")
	s.Preset.Image.Resize = presets.ResizeLongEdge
	s.Preset.Image.LongEdge = 300
	out := run(t, s)
	data, _ := os.ReadFile(out[0])
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width != 300 || cfg.Height != 150 {
		t.Fatalf("%v %+v", err, cfg)
	}
}

func TestSvgToPdf(t *testing.T) {
	in := write(t, "a.svg", []byte(testSVG))
	s, _ := step(t, in, "svg", "pdf")
	s.Preset.Document.PdfA = true
	out := run(t, s)
	data, _ := os.ReadFile(out[0])
	if !bytes.HasPrefix(data, []byte("%PDF-")) || !bytes.Contains(data, []byte("/MediaBox[0 0 150 75]")) {
		t.Fatalf("pdf: %.200s", data)
	}
}

func TestSvgzRoundTrip(t *testing.T) {
	in := write(t, "a.svg", []byte(testSVG))
	s, _ := step(t, in, "svg", "svgz")
	z := run(t, s)
	zdata, _ := os.ReadFile(z[0])
	if !svg.IsGzip(zdata) {
		t.Fatal("not gzip")
	}
	s2, _ := step(t, z[0], "svgz", "svg")
	back := run(t, s2)
	data, _ := os.ReadFile(back[0])
	if string(data) != testSVG {
		t.Fatal("round trip changed the file")
	}
	// svgz → png works too.
	s3, _ := step(t, z[0], "svgz", "png")
	run(t, s3)
	// A renamed non-SVG is refused.
	bad := write(t, "b.svg", []byte("hello"))
	s4, _ := step(t, bad, "svg", "svgz")
	if _, err := New().Convert(context.Background(), s4, nil); err == nil {
		t.Fatal("expected an error")
	}
}

func TestDstToSvg(t *testing.T) {
	data := make([]byte, 512)
	copy(data, "LA:test")
	// Three stitches right by 1, a colour change, two stitches up by 9, the end.
	rec := func(b0, b1, b2 byte) { data = append(data, b0, b1, b2) }
	rec(0x01, 0, 0x03)
	rec(0x01, 0, 0x03)
	rec(0x01, 0, 0x03)
	rec(0, 0, 0xC3)
	rec(0x20, 0, 0x03)
	rec(0x20, 0, 0x03)
	rec(0, 0, 0xF3)
	svgText, err := DstToSVG(data)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(svgText, "<polyline") != 2 || !strings.Contains(svgText, dstPalette[1]) {
		t.Fatal(svgText)
	}
	if _, err := DstToSVG([]byte("nope")); err == nil {
		t.Fatal("expected an error")
	}
}

func TestHpglToSvg(t *testing.T) {
	plot := "IN;SP1;PU0,0;PD400,0,400,400;SP2;PU800,800;CI100;PM0;PU0,800;PD200,800,200,1000;PM2;FP;"
	svgText, err := HpglToSVG([]byte(plot))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(svgText, "<polyline") != 2 || !strings.Contains(svgText, "<polygon") || !strings.Contains(svgText, "#FF0000") {
		t.Fatal(svgText)
	}
	// Encoded polyline: PE with relative moves (base 64).
	var pe []byte
	enc := func(v int) {
		u := v * 2
		if v < 0 {
			u = -v*2 + 1
		}
		for u >= 64 {
			pe = append(pe, byte(63+u%64))
			u /= 64
		}
		pe = append(pe, byte(191+u))
	}
	enc(400)
	enc(0)
	enc(0)
	enc(400)
	svgText, err = HpglToSVG(append(append([]byte("IN;SP1;PE"), pe...), ';'))
	if err != nil || !strings.Contains(svgText, "<polyline") {
		t.Fatalf("%v %s", err, svgText)
	}
	if _, err := HpglToSVG([]byte("%!PS-Adobe")); err == nil {
		t.Fatal("expected an error")
	}
}

func TestTracePng(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			c := color.NRGBA{255, 255, 255, 255}
			if x > 16 && x < 48 && y > 16 && y < 48 {
				c = color.NRGBA{0, 128, 0, 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	in := write(t, "a.png", b.Bytes())
	s, _ := step(t, in, "png", "svg")
	out := run(t, s)
	data, _ := os.ReadFile(out[0])
	if !strings.Contains(string(data), `width="64"`) || strings.Count(string(data), "<path") != 2 {
		t.Fatal(string(data))
	}
}

func TestIllustratorPdf(t *testing.T) {
	in := write(t, "a.ai", []byte("%PDF-1.5\n%fake"))
	s, _ := step(t, in, "ai", "pdf")
	out := run(t, s)
	data, _ := os.ReadFile(out[0])
	if !bytes.HasPrefix(data, []byte("%PDF-1.5")) {
		t.Fatal("not copied")
	}
	bad := write(t, "b.ai", []byte("garbage"))
	s2, _ := step(t, bad, "ai", "pdf")
	if _, err := New().Convert(context.Background(), s2, nil); err == nil {
		t.Fatal("expected an error")
	}
}

func TestEdges(t *testing.T) {
	var trace, png bool
	for _, e := range New().Edges() {
		if e.From == "jpg" && e.To == "svg" && e.ImageSourcesOnly && e.Cost == TraceCost {
			trace = true
		}
		if e.From == "svg" && e.To == "png" {
			png = true
		}
	}
	if !trace || !png {
		t.Fatal("missing edges")
	}
}
