package svg

import (
	"image/color"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
)

// render draws an SVG at 1:1 on white.
func render(t *testing.T, src string) func(x, y int) color.NRGBA {
	t.Helper()
	d, err := Parse([]byte(src), "")
	if err != nil {
		t.Fatal(err)
	}
	d.Fonts = gfx.NewFonts(false)
	r := gfx.NewRaster(int(d.Width), int(d.Height))
	r.Clear(gfx.White)
	d.Draw(r, gfx.Identity, Options{})
	img := r.Straight()
	return img.NRGBAAt
}

func near(c color.NRGBA, r, g, b uint8) bool {
	d := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	return d(c.R, r) < 12 && d(c.G, g) < 12 && d(c.B, b) < 12
}

func TestFeatures(t *testing.T) {
	cases := []struct {
		name    string
		svg     string
		x, y    int
		r, g, b uint8
	}{
		{"css class beats attribute", `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><style>.a{fill:#00f}</style><rect class="a" width="20" height="20" fill="red"/></svg>`, 10, 10, 0, 0, 255},
		{"style attribute beats css", `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><style>rect{fill:#00f}</style><rect style="fill:lime" width="20" height="20"/></svg>`, 10, 10, 0, 255, 0},
		{"important wins", `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><style>rect{fill:#00f !important}</style><rect style="fill:lime" width="20" height="20"/></svg>`, 10, 10, 0, 0, 255},
		{"child combinator", `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><style>g > rect{fill:#00f}</style><g><rect width="20" height="20" fill="red"/></g></svg>`, 10, 10, 0, 0, 255},
		{"inherited fill", `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><g fill="lime"><rect width="20" height="20"/></g></svg>`, 10, 10, 0, 255, 0},
		{"currentColor", `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" color="blue"><rect width="20" height="20" fill="currentColor"/></svg>`, 10, 10, 0, 0, 255},
		{"use with translate", `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="40" height="20"><defs><rect id="r" width="10" height="10" fill="blue"/></defs><use xlink:href="#r" x="25" y="5"/></svg>`, 30, 10, 0, 0, 255},
		{"symbol viewBox", `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40"><symbol id="s" viewBox="0 0 2 2"><rect width="1" height="1" fill="blue"/></symbol><use href="#s" width="40" height="40"/></svg>`, 10, 10, 0, 0, 255},
		{"symbol viewBox other quadrant", `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40"><symbol id="s" viewBox="0 0 2 2"><rect width="1" height="1" fill="blue"/></symbol><use href="#s" width="40" height="40"/></svg>`, 30, 30, 255, 255, 255},
		{"transform rotate", `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40"><rect x="0" y="0" width="40" height="10" fill="blue" transform="rotate(90 20 20)"/></svg>`, 35, 20, 0, 0, 255},
		{"clip path", `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><clipPath id="c"><rect width="20" height="20"/></clipPath><rect width="40" height="20" fill="blue" clip-path="url(#c)"/></svg>`, 30, 10, 255, 255, 255},
		{"clip path inside", `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><clipPath id="c"><rect width="20" height="20"/></clipPath><rect width="40" height="20" fill="blue" clip-path="url(#c)"/></svg>`, 10, 10, 0, 0, 255},
		{"group opacity", `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><g opacity="0.5"><rect width="20" height="20" fill="black"/><rect width="20" height="20" fill="black"/></g></svg>`, 10, 10, 128, 128, 128},
		{"bbox gradient", `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="10"><linearGradient id="g"><stop offset="0" stop-color="black"/><stop offset="1" stop-color="white"/></linearGradient><rect x="50" width="50" height="10" fill="url(#g)"/></svg>`, 51, 5, 3, 3, 3},
		{"display none", `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><rect width="20" height="20" fill="blue" display="none"/></svg>`, 10, 10, 255, 255, 255},
		{"nested svg viewport clips", `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><svg width="20" height="20"><rect width="40" height="20" fill="blue"/></svg></svg>`, 30, 10, 255, 255, 255},
		{"percent lengths", `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><rect x="50%" width="50%" height="100%" fill="blue"/></svg>`, 30, 10, 0, 0, 255},
		{"evenodd", `<svg xmlns="http://www.w3.org/2000/svg" width="30" height="30"><path fill-rule="evenodd" d="M0 0H30V30H0Z M10 10H20V20H10Z" fill="blue"/></svg>`, 15, 15, 255, 255, 255},
		{"stroke dash", `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="10"><line x1="0" y1="5" x2="40" y2="5" stroke="blue" stroke-width="4" stroke-dasharray="10"/></svg>`, 15, 5, 255, 255, 255},
		{"mask", `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><mask id="m"><rect width="20" height="20" fill="white"/></mask><rect width="40" height="20" fill="blue" mask="url(#m)"/></svg>`, 30, 10, 255, 255, 255},
		{"hsl colour", `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><rect width="20" height="20" fill="hsl(120, 100%, 50%)"/></svg>`, 10, 10, 0, 255, 0},
		{"ampersand repaired", `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><title>A & B &nbsp;</title><rect width="20" height="20" fill="blue"/></svg>`, 10, 10, 0, 0, 255},
		{"switch picks first", `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><switch><rect width="20" height="20" fill="blue" systemLanguage="en"/><rect width="20" height="20" fill="red"/></switch></svg>`, 10, 10, 0, 0, 255},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			at := render(t, c.svg)
			if got := at(c.x, c.y); !near(got, c.r, c.g, c.b) {
				t.Fatalf("pixel (%d,%d) = %v, want %d,%d,%d", c.x, c.y, got, c.r, c.g, c.b)
			}
		})
	}
}

func TestTextAnchor(t *testing.T) {
	at := render(t, `<svg xmlns="http://www.w3.org/2000/svg" width="200" height="40"><text x="100" y="30" font-size="30" text-anchor="end">WWW</text></svg>`)
	// Ink left of x=100, none right of it.
	left, right := false, false
	for y := 0; y < 40; y++ {
		for x := 0; x < 200; x++ {
			if at(x, y).R < 100 {
				if x < 100 {
					left = true
				} else if x > 103 {
					right = true
				}
			}
		}
	}
	if !left || right {
		t.Fatalf("left %v right %v", left, right)
	}
}

func TestMarkersDrawn(t *testing.T) {
	at := render(t, `<svg xmlns="http://www.w3.org/2000/svg" width="60" height="20"><marker id="m" markerWidth="10" markerHeight="10" refX="5" refY="5" markerUnits="userSpaceOnUse"><rect width="10" height="10" fill="blue"/></marker><line x1="10" y1="10" x2="50" y2="10" stroke="black" marker-end="url(#m)"/></svg>`)
	if c := at(52, 12); !near(c, 0, 0, 255) {
		t.Fatalf("marker pixel %v", c)
	}
}

func TestNoSize(t *testing.T) {
	if _, err := Parse([]byte(`<html></html>`), ""); err == nil {
		t.Fatal("expected an error for a non-SVG")
	}
	d, err := Parse([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 30 15"/>`), "")
	if err != nil || d.Width != 30 || d.Height != 15 {
		t.Fatalf("%v %v %v", err, d.Width, d.Height)
	}
	d, _ = Parse([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10mm" height="1in"/>`), "")
	if d.Width < 37.7 || d.Width > 37.8 || d.Height != 96 {
		t.Fatalf("%v x %v", d.Width, d.Height)
	}
}
