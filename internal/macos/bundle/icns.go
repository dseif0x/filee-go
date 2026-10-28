package bundle

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"

	"golang.org/x/image/draw"
)

// icnsSizes are the PNG icon types macOS reads (10.7+), with their pixel sizes.
var icnsSizes = []struct {
	kind string
	px   int
}{
	{"icp4", 16}, {"icp5", 32}, {"ic11", 32}, {"ic12", 64}, {"ic07", 128}, {"ic13", 256}, {"ic08", 256},
	{"ic14", 512}, {"ic09", 512}, {"ic10", 1024},
}

// ICNS makes an .icns file of PNG icons from a square picture. Sizes larger
// than the picture are left out (a 512-pixel picture gets no 1024 icon).
func ICNS(src image.Image) ([]byte, error) {
	side := min(src.Bounds().Dx(), src.Bounds().Dy())
	var body bytes.Buffer
	for _, s := range icnsSizes {
		if s.px > side {
			continue
		}
		dst := image.NewNRGBA(image.Rect(0, 0, s.px, s.px))
		draw.CatmullRom.Scale(dst, dst.Rect, src, src.Bounds(), draw.Src, nil)
		var p bytes.Buffer
		if err := png.Encode(&p, dst); err != nil {
			return nil, err
		}
		body.WriteString(s.kind)
		_ = binary.Write(&body, binary.BigEndian, uint32(8+p.Len()))
		body.Write(p.Bytes())
	}
	var out bytes.Buffer
	out.WriteString("icns")
	_ = binary.Write(&out, binary.BigEndian, uint32(8+body.Len()))
	out.Write(body.Bytes())
	return out.Bytes(), nil
}
