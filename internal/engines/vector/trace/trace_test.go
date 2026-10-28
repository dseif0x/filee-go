package trace

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
	"github.com/dseif0x/filee-go/internal/engines/vector/svg"
	"github.com/dseif0x/filee-go/internal/presets"
)

func testPicture() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 200, 150))
	for y := 0; y < 150; y++ {
		for x := 0; x < 200; x++ {
			c := color.NRGBA{255, 255, 255, 255}
			if dx, dy := float64(x)-60, float64(y)-75; dx*dx+dy*dy < 40*40 {
				c = color.NRGBA{200, 0, 0, 255}
			}
			if x >= 120 && x < 180 && y >= 40 && y < 110 {
				c = color.NRGBA{0, 0, 180, 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func render(t *testing.T, data []byte) *image.NRGBA {
	d, err := svg.Parse(data, "")
	if err != nil {
		t.Fatal(err)
	}
	r := gfx.NewRaster(int(d.Width), int(d.Height))
	r.Clear(gfx.White)
	d.Draw(r, gfx.Identity, svg.Options{})
	return r.Straight()
}

func TestTraceColorRoundTrip(t *testing.T) {
	src := testPicture()
	res, err := Picture(context.Background(), src, presets.TracingOptions{Mode: presets.TraceColor, Colors: 8, Detail: 50}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Layers) != 3 {
		t.Fatalf("layers: %d", len(res.Layers))
	}
	var b bytes.Buffer
	if err := WriteSVG(res, &b, 200, 150, ""); err != nil {
		t.Fatal(err)
	}
	out := render(t, b.Bytes())
	diff := 0
	for y := 0; y < 150; y++ {
		for x := 0; x < 200; x++ {
			a, c := src.NRGBAAt(x, y), out.NRGBAAt(x, y)
			if absd(a.R, c.R)+absd(a.G, c.G)+absd(a.B, c.B) > 120 {
				diff++
			}
		}
	}
	if diff > 300 {
		t.Fatalf("%d pixels differ", diff)
	}
}

func absd(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func TestTraceBlackAndWhite(t *testing.T) {
	res, err := Picture(context.Background(), testPicture(), presets.TracingOptions{Mode: presets.TraceBlackAndWhite, Detail: 80}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Layers) != 1 || res.Layers[0].Color != 0 || len(res.Layers[0].Paths) != 2 {
		t.Fatalf("got %+v", len(res.Layers))
	}
	var b bytes.Buffer
	WriteSVG(res, &b, 400, 300, "Traced -- reduced")
	if !strings.Contains(b.String(), `width="400"`) || !strings.Contains(b.String(), "- -") {
		t.Fatal(b.String()[:200])
	}
}

func TestOtsu(t *testing.T) {
	var h [256]int64
	h[20], h[230] = 100, 100
	if th := Otsu(h[:]); th <= 20 || th > 230 {
		t.Fatalf("threshold %d", th)
	}
}

func TestTracedSize(t *testing.T) {
	if w, h := TracedSize(8000, 6000); w*h > MaxPixels+10000 || w > MaxSide {
		t.Fatalf("%d x %d", w, h)
	}
}
