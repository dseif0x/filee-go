package pdf

import (
	"image"
	"image/color"
	"math/rand"
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
