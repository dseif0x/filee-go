package gfx

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"strings"
	"testing"
)

func alphaAt(img *image.RGBA, x, y int) uint8 { return img.Pix[y*img.Stride+x*4+3] }

func TestFillCircleCoverage(t *testing.T) {
	r := NewRaster(100, 100)
	p := NewPath()
	p.Ellipse(50, 50, 40, 40)
	r.Fill(p, Identity, NonZero, Solid(Black))
	img := r.Image()
	if a := alphaAt(img, 50, 50); a != 255 {
		t.Fatalf("centre alpha %d", a)
	}
	if a := alphaAt(img, 2, 2); a != 0 {
		t.Fatalf("corner alpha %d", a)
	}
	// Area should be close to pi r^2.
	sum := 0.0
	for i := 3; i < len(img.Pix); i += 4 {
		sum += float64(img.Pix[i]) / 255
	}
	if want := math.Pi * 1600; math.Abs(sum-want) > want*0.01 {
		t.Fatalf("area %.1f want %.1f", sum, want)
	}
}

func TestEvenOddHole(t *testing.T) {
	r := NewRaster(100, 100)
	p := NewPath()
	p.Rect(10, 10, 80, 80)
	p.Rect(30, 30, 40, 40)
	r.Fill(p, Identity, EvenOdd, Solid(Black))
	if a := alphaAt(r.Image(), 50, 50); a != 0 {
		t.Fatalf("hole filled: %d", a)
	}
	if a := alphaAt(r.Image(), 20, 20); a != 255 {
		t.Fatalf("ring empty: %d", a)
	}
}

func TestStrokeAndDash(t *testing.T) {
	r := NewRaster(100, 20)
	p := NewPath()
	p.MoveTo(0, 10)
	p.LineTo(100, 10)
	r.Stroke(p, Identity, &StrokeStyle{Width: 4, Dashes: []float64{10, 10}}, Solid(Black))
	img := r.Image()
	if alphaAt(img, 5, 10) != 255 || alphaAt(img, 15, 10) != 0 || alphaAt(img, 25, 10) != 255 {
		t.Fatalf("dash pattern wrong: %d %d %d", alphaAt(img, 5, 10), alphaAt(img, 15, 10), alphaAt(img, 25, 10))
	}
	if alphaAt(img, 5, 5) != 0 {
		t.Fatal("stroke too wide")
	}
}

func TestClipAndGroup(t *testing.T) {
	r := NewRaster(50, 50)
	clip := NewPath()
	clip.Rect(0, 0, 25, 50)
	r.PushClip(clip, Identity, NonZero)
	r.PushGroup(0.5)
	all := NewPath()
	all.Rect(0, 0, 50, 50)
	r.Fill(all, Identity, NonZero, Solid(Black))
	r.Fill(all, Identity, NonZero, Solid(Black))
	r.PopGroup()
	r.PopClip()
	if a := alphaAt(r.Image(), 10, 10); a < 125 || a > 130 {
		t.Fatalf("group opacity: %d", a)
	}
	if a := alphaAt(r.Image(), 40, 10); a != 0 {
		t.Fatalf("clip leaked: %d", a)
	}
}

func TestGradient(t *testing.T) {
	r := NewRaster(100, 10)
	p := NewPath()
	p.Rect(0, 0, 100, 10)
	g := &Gradient{X1: 0, X2: 100, Stops: []Stop{{0, Black}, {1, White}}, Matrix: Identity}
	r.Fill(p, Identity, NonZero, Paint{Gradient: g})
	img := r.Straight()
	left := img.NRGBAAt(1, 5)
	right := img.NRGBAAt(98, 5)
	if left.R > 10 || right.R < 245 {
		t.Fatalf("gradient ends %v %v", left, right)
	}
	rg := &Gradient{Radial: true, CX: 50, CY: 5, R: 50, FX: 50, FY: 5, Stops: []Stop{{0, White}, {1, Black}}, Matrix: Identity}
	r.Fill(p, Identity, NonZero, Paint{Gradient: rg})
	if c := r.Straight().NRGBAAt(50, 5); c.R < 245 {
		t.Fatalf("radial centre %v", c)
	}
}

func TestTextShape(t *testing.T) {
	f := NewFonts(false)
	st := f.Shape("Hello", FontQuery{Families: []string{"sans-serif"}}, 20, "en")
	if len(st.Glyphs) != 5 || st.Advance < 30 || st.Advance > 80 {
		t.Fatalf("shape: %d glyphs, advance %v", len(st.Glyphs), st.Advance)
	}
	path := f.TextPath(st, 0, 0)
	b := path.Bounds()
	if b.Empty() || b.Y0 > -10 || b.Y1 > 1 {
		t.Fatalf("text bounds %+v", b)
	}
}

func TestPDFAndSVG(t *testing.T) {
	d := NewPDF()
	pg := d.Page(200, 100)
	p := NewPath()
	p.Ellipse(50, 50, 30, 30)
	pg.Fill(p, Identity, NonZero, Solid(Color{1, 0, 0, 0.5}))
	pg.Stroke(p, Identity, &StrokeStyle{Width: 2, Dashes: []float64{3, 2}}, Solid(Black))
	pg.PushClip(p, Identity, NonZero)
	pg.PushGroup(0.5)
	pg.Fill(p, Identity, NonZero, Paint{Gradient: &Gradient{X1: 0, X2: 100, Stops: []Stop{{0, Black}, {1, Color{1, 1, 1, 0}}}, Spread: SpreadReflect, Matrix: Identity}})
	pg.PopGroup()
	pg.PopClip()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.NRGBA{255, 0, 0, 128})
	pg.DrawImage(img, Translate(100, 10).Mul(Scale(10, 10)), 1)
	pg.Finish()
	var b bytes.Buffer
	if err := d.Write(&b, PDFInfo{Title: "Tëst", PdfA: true}); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b.Bytes(), []byte("%PDF-1.7")) || !bytes.Contains(b.Bytes(), []byte("/OutputIntents")) {
		t.Fatal("not a PDF/A")
	}
	s := NewSVGWriter(100, 100)
	s.Fill(p, Identity, EvenOdd, Solid(Black))
	var sb bytes.Buffer
	s.Write(&sb)
	if !strings.Contains(sb.String(), `fill-rule="evenodd"`) {
		t.Fatal(sb.String())
	}
}
