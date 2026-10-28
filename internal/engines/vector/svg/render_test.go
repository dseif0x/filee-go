package svg

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
)

const sample = `<?xml version="1.0"?>
<!DOCTYPE svg [ <!ENTITY ns_svg "http://www.w3.org/2000/svg"> ]>
<svg xmlns="&ns_svg;" xmlns:xlink="http://www.w3.org/1999/xlink" width="400" height="300" viewBox="0 0 400 300">
<style>.blue { fill: #36c } #star { stroke: black; stroke-width: 2 }</style>
<defs>
 <linearGradient id="lg" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="red"/><stop offset="1" stop-color="yellow" stop-opacity="0.5"/></linearGradient>
 <radialGradient id="rg"><stop offset="0" stop-color="white"/><stop offset="1" stop-color="green"/></radialGradient>
 <clipPath id="cp"><circle cx="300" cy="70" r="50"/></clipPath>
 <pattern id="pt" width="10" height="10" patternUnits="userSpaceOnUse"><rect width="5" height="5" fill="purple"/></pattern>
 <marker id="arrow" viewBox="0 0 10 10" refX="5" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path d="M0 0 L10 5 L0 10z" fill="context-stroke"/></marker>
 <mask id="mk"><rect x="0" y="200" width="200" height="100" fill="white"/><circle cx="100" cy="250" r="30" fill="black"/></mask>
 <filter id="blur"><feGaussianBlur stdDeviation="3"/></filter>
 <symbol id="sym" viewBox="0 0 10 10"><circle cx="5" cy="5" r="5" fill="orange"/></symbol>
</defs>
<rect x="10" y="10" width="180" height="80" rx="15" fill="url(#lg)"/>
<rect x="250" y="20" width="100" height="100" fill="url(#rg)" clip-path="url(#cp)"/>
<path id="star" class="blue" d="M100 110 l20 50 h50 l-40 30 l15 50 l-45 -30 l-45 30 l15 -50 l-40 -30 h50 z" opacity="0.8"/>
<rect x="220" y="140" width="80" height="60" fill="url(#pt)" stroke="black"/>
<polyline points="320,150 380,180 330,220" fill="none" stroke="blue" stroke-width="3" marker-start="url(#arrow)" marker-end="url(#arrow)"/>
<rect x="0" y="200" width="200" height="100" fill="teal" mask="url(#mk)"/>
<text x="210" y="260" font-family="sans-serif" font-size="24" fill="darkred" text-anchor="start">Hello &amp; SVG<tspan dy="-10" font-weight="bold" fill="navy">!</tspan></text>
<use xlink:href="#sym" x="350" y="240" width="40" height="40"/>
<circle cx="370" cy="40" r="20" fill="magenta" filter="url(#blur)"/>
<line x1="210" y1="280" x2="390" y2="280" stroke="gray" stroke-dasharray="5 3"/>
</svg>`

func TestRenderSample(t *testing.T) {
	d, err := Parse([]byte(sample), "")
	if err != nil {
		t.Fatal(err)
	}
	d.Fonts = gfx.NewFonts(false)
	if d.Width != 400 || d.Height != 300 {
		t.Fatalf("size %v %v", d.Width, d.Height)
	}
	r := gfx.NewRaster(800, 600)
	r.Clear(gfx.White)
	d.Draw(r, gfx.Scale(2, 2), Options{})
	if dir := os.Getenv("SVG_TEST_OUT"); dir != "" {
		f, _ := os.Create(filepath.Join(dir, "sample.png"))
		png.Encode(f, r.Straight())
		f.Close()
	}
	img := r.Straight()
	// Gradient start is red.
	if c := img.NRGBAAt(30, 100); c.R < 200 || c.G > 80 {
		t.Fatalf("gradient pixel %v", c)
	}
}

func TestRenderSamplePDF(t *testing.T) {
	d, err := Parse([]byte(sample), "")
	if err != nil {
		t.Fatal(err)
	}
	d.Fonts = gfx.NewFonts(false)
	doc := gfx.NewPDF()
	pg := doc.Page(d.Width*0.75, d.Height*0.75)
	d.Draw(pg, gfx.Scale(0.75, 0.75), Options{FilterResolution: 300.0 / 72})
	pg.Finish()
	dir := os.Getenv("SVG_TEST_OUT")
	if dir == "" {
		dir = t.TempDir()
	}
	f, _ := os.Create(filepath.Join(dir, "sample.pdf"))
	defer f.Close()
	if err := doc.Write(f, gfx.PDFInfo{Title: "sample"}); err != nil {
		t.Fatal(err)
	}
}
