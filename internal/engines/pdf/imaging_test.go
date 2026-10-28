package pdf

import (
	"bytes"
	"image"
	"image/color"
	"math/rand"
	"testing"

	"golang.org/x/image/tiff"

	"github.com/dseif0x/filee-go/internal/presets"
)

func noisy(w, h int, seed int64, smooth bool) *image.NRGBA {
	r := rand.New(rand.NewSource(seed))
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if smooth {
				img.Set(x, y, color.NRGBA{uint8(x), uint8(y), uint8(x + y), 255})
			} else {
				img.Set(x, y, color.NRGBA{uint8(r.Intn(256)), uint8(r.Intn(256)), uint8(r.Intn(256)), 255})
			}
		}
	}
	return img
}

func TestTiffRoundTrip(t *testing.T) {
	for _, c := range []presets.TiffCompression{presets.TiffLzw, presets.TiffZip, presets.TiffNone} {
		for _, smooth := range []bool{true, false} {
			for _, size := range []int{1, 7, 64, 300} {
				src := noisy(size, size, int64(size), smooth)
				var buf bytes.Buffer
				if err := writeTiff(&buf, []image.Image{src, src}, c, 150); err != nil {
					t.Fatal(err)
				}
				pages, err := readTiffPages(buf.Bytes())
				if err != nil {
					t.Fatalf("%s %d: %v", c, size, err)
				}
				if len(pages) != 2 || pages[1].Dpi != 150 {
					t.Fatalf("%s: %d pages dpi %v", c, len(pages), pages[0].Dpi)
				}
				got, err := tiff.Decode(bytes.NewReader(buf.Bytes()))
				if err != nil {
					t.Fatal(err)
				}
				for y := 0; y < size; y++ {
					for x := 0; x < size; x++ {
						a := color.NRGBAModel.Convert(got.At(x, y)).(color.NRGBA)
						if a != src.NRGBAAt(x, y) {
							t.Fatalf("%s smooth=%v size %d: pixel %d,%d %v != %v", c, smooth, size, x, y, a, src.NRGBAAt(x, y))
						}
					}
				}
			}
		}
	}
}
