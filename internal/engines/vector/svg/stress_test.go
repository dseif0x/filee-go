package svg

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
)

func TestStressGroupsAndPatterns(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="1000" height="1000"><defs><pattern id="p" width="4" height="4" patternUnits="userSpaceOnUse"><rect width="2" height="2" fill="red"/></pattern></defs><rect width="1000" height="1000" fill="url(#p)"/>`)
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&b, `<circle cx="%d" cy="%d" r="10" fill="blue" opacity="0.5"/>`, (i*37)%1000, (i*91)%1000)
	}
	b.WriteString(`</svg>`)
	d, err := Parse([]byte(b.String()), "")
	if err != nil {
		t.Fatal(err)
	}
	d.Fonts = gfx.NewFonts(false)
	start := time.Now()
	r := gfx.NewRaster(2000, 2000)
	d.Draw(r, gfx.Scale(2, 2), Options{})
	if el := time.Since(start); el > 60*time.Second {
		t.Fatalf("too slow: %v", el)
	} else {
		t.Logf("rendered in %v", el)
	}
}
