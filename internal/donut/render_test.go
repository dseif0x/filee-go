package donut

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sampleMenu() *Menu {
	m := NewMenu()
	m.SetItems([]Item{
		{ID: "a", Label: "PNG", Caption: "PNG image", Enabled: true},
		{ID: "b", Label: "JPG small", Caption: "JPEG", Enabled: true},
		{ID: "c", Label: "Rotate", Caption: "Group of 3", Enabled: true, Group: true, Children: []Item{
			{ID: "c1", Label: "Left", Enabled: true}, {ID: "c2", Label: "Right", Enabled: true}, {ID: "c3", Label: "180°", Enabled: false}}},
		{ID: "d", Label: "WebP", Caption: "WebP", Enabled: false, DisabledReason: "no engine"},
		{ID: "e", Label: "Grayscale everything", Caption: "Same format", Enabled: true},
	})
	m.CenterTitle = "3 × PNG"
	m.CenterSubtitle = "Drop → PNG"
	m.CenterHint = "Tab back"
	m.CanFlip = true
	return m
}

func settle(m *Menu) {
	for i := 0; i < 400 && m.Advance(1.0/60); i++ {
	}
}

// writePNG keeps the picture for a look when FILEE_DONUT_PNG names a folder.
func writePNG(t *testing.T, name string, r *Renderer, m *Menu, pal Palette) {
	img := r.Render(m, pal)
	if dir := os.Getenv("FILEE_DONUT_PNG"); dir != "" {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRenderDrawsTheDonut(t *testing.T) {
	m := sampleMenu()
	settle(m)
	r := NewRenderer(nil, 2)
	pal := NewPalette(false, DefaultAccent, nil)
	c := m.CenterPoint()
	m.SetExternalHit(m.HitTest(m.SliceCenter(1)), ptr(m.SliceCenter(1)))
	settle(m)
	img := r.Render(m, pal)
	if img.Rect.Dx() != int(m.Side()*2+0.999) {
		t.Fatalf("size %v for side %v", img.Rect, m.Side())
	}
	// The hub is drawn opaque, the corners are transparent.
	at := func(p Point) (uint8, uint8, uint8, uint8) {
		i := img.PixOffset(int(p.X*2), int(p.Y*2))
		return img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]
	}
	if _, _, _, a := at(Point{c.X, c.Y + m.innerRadius() - 10}); a != 255 {
		t.Errorf("hub alpha %d", a)
	}
	if _, _, _, a := at(Point{2, 2}); a != 0 {
		t.Errorf("corner alpha %d", a)
	}
	// The lit slice is the accent colour (#7F77DD) near its outer edge.
	lit := PointAt(c, m.OuterRadius-6, MidAngle(1, 5))
	lit = lit.Add(PointAt(Point{}, PopOut, MidAngle(1, 5)))
	if r, g, b, _ := at(lit); !(b > r && b > g && b > 150) {
		t.Errorf("lit slice colour %d,%d,%d", r, g, b)
	}
	writePNG(t, "light.png", r, m, pal)
	m.SetExternalHit(m.HitTest(m.SliceCenter(2)), ptr(m.SliceCenter(2)))
	settle(m)
	writePNG(t, "group.png", r, m, NewPalette(true, "#E0C060", nil))
	m.BeginProgress()
	m.SetProgress(0.4)
	for range 40 {
		m.Advance(1.0 / 60)
	}
	writePNG(t, "progress.png", r, m, pal)
	done := false
	m.CompleteProgress(Succeeded, func() { done = true })
	for i := 0; i < 60; i++ {
		m.Advance(1.0 / 60)
	}
	writePNG(t, "done.png", r, m, pal)
	settle(m)
	if !done {
		t.Error("the finished ring never called back")
	}
}

func ptr(p Point) *Point { return &p }

func TestRenderIsFastEnough(t *testing.T) {
	m := sampleMenu()
	m.PlayOpenAnimation()
	r := NewRenderer(nil, 2)
	pal := NewPalette(false, DefaultAccent, nil)
	start := time.Now()
	frames := 0
	for m.Advance(1.0/60) && frames < 60 {
		r.Render(m, pal)
		frames++
	}
	per := time.Since(start) / time.Duration(max(1, frames))
	t.Logf("%d frames, %v per frame at 2x", frames, per)
}
