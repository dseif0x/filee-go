package svg

import (
	"encoding/base64"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
	"golang.org/x/image/font/gofont/gomono"
)

func textWidth(t *testing.T, doc string) float64 {
	d, err := Parse([]byte(doc), "")
	if err != nil {
		t.Fatal(err)
	}
	if d.Fonts == nil {
		d.Fonts = gfx.NewFonts(false)
	}
	r := &renderer{doc: d, fonts: d.Fonts, active: map[*Node]int{}}
	c := r.rootContext(gfx.Identity)
	var text *Node
	for _, n := range d.root.Elements() {
		if n.Name == "text" {
			text = n
		}
	}
	return r.textOutline(text, context{m: gfx.Identity, st: computeStyle(text, c.st, c.lc), lc: c.lc}).Bounds().Width()
}

func TestFontFace(t *testing.T) {
	gfx.SystemFonts = false
	plain := `<svg xmlns="http://www.w3.org/2000/svg" width="200" height="50"><text x="0" y="40" font-family="Embedded" font-size="20">iiiiiiii</text></svg>`
	embedded := `<svg xmlns="http://www.w3.org/2000/svg" width="200" height="50"><style>@font-face { font-family: "Embedded"; src: url(data:font/ttf;base64,` +
		base64.StdEncoding.EncodeToString(gomono.TTF) + `) format("truetype"); }</style><text x="0" y="40" font-family="Embedded" font-size="20">iiiiiiii</text></svg>`
	w0, w1 := textWidth(t, plain), textWidth(t, embedded)
	if !(w1 > w0*1.5) {
		t.Fatalf("embedded monospace font not used: %.1f vs %.1f", w1, w0)
	}
}
