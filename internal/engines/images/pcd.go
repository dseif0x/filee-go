package images

import (
	"errors"
	"image"
)

// Kodak Photo CD: the Base picture (768 × 512, stored uncompressed as PhotoYCC
// with half-resolution chroma), as ImageMagick reads it by default. The larger
// 4Base and 16Base pictures need the Huffman-coded residuals and are not read.

const (
	pcdBaseOffset = 0x30000
	pcdW, pcdH    = 768, 512
)

func decodePCD(data []byte) (*Picture, error) {
	if len(data) < pcdBaseOffset+pcdW*pcdH*3/2 || string(data[0x800:0x807]) != "PCD_IPI" {
		return nil, errors.New("the file is not a Photo CD image")
	}
	img := image.NewNRGBA(image.Rect(0, 0, pcdW, pcdH))
	clamp := func(v float64) uint8 { return uint8(max(0, min(255, v+0.5))) }
	for pair := 0; pair < pcdH/2; pair++ {
		base := pcdBaseOffset + pair*pcdW*3
		y1, y2 := data[base:base+pcdW], data[base+pcdW:base+2*pcdW]
		c1, c2 := data[base+2*pcdW:base+2*pcdW+pcdW/2], data[base+2*pcdW+pcdW/2:base+3*pcdW]
		for r, ys := range [][]byte{y1, y2} {
			y := pair*2 + r
			for x := 0; x < pcdW; x++ {
				l := 1.3584 * float64(ys[x])
				cb := 2.2179 * (float64(c1[x/2]) - 156)
				cr := 1.8215 * (float64(c2[x/2]) - 137)
				o := y*img.Stride + x*4
				img.Pix[o] = clamp(l + cr)
				img.Pix[o+1] = clamp(l - 0.194*cb - 0.509*cr)
				img.Pix[o+2] = clamp(l + cb)
				img.Pix[o+3] = 255
			}
		}
	}
	switch data[0x0e02] & 3 {
	case 1:
		img = rotate270(img)
	case 2:
		img = rotate180(img)
	case 3:
		img = rotate90(img)
	}
	return one(&Frame{Img: img}), nil
}
