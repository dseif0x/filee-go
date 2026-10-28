package images

import (
	"image"
	"image/color"
	"testing"

	"github.com/dseif0x/filee-go/internal/presets"
)

// testPicture is a 40 × 30 gradient with a transparent corner.
func testPicture(alpha bool) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 40, 30))
	for y := 0; y < 30; y++ {
		for x := 0; x < 40; x++ {
			c := color.NRGBA{uint8(x * 6), uint8(y * 8), uint8(255 - x*3), 255}
			if alpha && x < 8 && y < 8 {
				c.A = 0
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func meanDiff(a, b *image.NRGBA) float64 {
	var sum, n float64
	for y := 0; y < a.Rect.Dy(); y++ {
		for x := 0; x < a.Rect.Dx(); x++ {
			p, q := a.NRGBAAt(x, y), b.NRGBAAt(x, y)
			if p.A == 0 && q.A == 0 {
				continue
			}
			d := func(u, v uint8) float64 {
				if u > v {
					return float64(u - v)
				}
				return float64(v - u)
			}
			sum += d(p.R, q.R) + d(p.G, q.G) + d(p.B, q.B) + d(p.A, q.A)
			n += 4
		}
	}
	return sum / n
}

func TestRoundTripEveryWritableFormat(t *testing.T) {
	lossy := map[string]float64{"jpg": 12, "xpm": 12, "webp": 12, "avif": 16, "jxl": 12, "jp2": 12, "gif": 12, "dds": 16, "xbm": 200, "wbmp": 200, "pfm": 2, "ppm": 2, "pcx": 2, "ras": 2}
	for _, format := range Writable {
		t.Run(format, func(t *testing.T) {
			src := testPicture(HoldsAlpha(format) && format != "xbm" && format != "wbmp")
			o := presets.DefaultImageOptions()
			data, err := EncodeImage(src, format, &o)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			pic, err := DecodeBytes(data, format)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			got := SelectFrames(pic, format, "png")[0]
			if got.Width() != 40 || got.Height() != 30 {
				if format == "ico" && got.Width() == 256 {
					return // icons are square: checked in their own test
				}
				t.Fatalf("size %dx%d", got.Width(), got.Height())
			}
			limit, ok := lossy[format]
			if !ok {
				limit = 0.5
			}
			want := src
			if !HoldsAlpha(format) {
				want = flatten(src, colorWhite)
			}
			if d := meanDiff(want, got.Img); d > limit {
				t.Errorf("mean difference %.2f > %.2f", d, limit)
			}
		})
	}
}
