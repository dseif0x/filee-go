package ocr

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"

	_ "golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// HasResolution: the picture says how large it is meant to be. Without that
// Tesseract guesses 70 dpi (a page of a metre and more); 300 dpi is what scanners write.
func HasResolution(path string) bool { return resolution(path) > 1 }

// resolution is the picture's dpi (0 when it says none): PNG pHYs, JPEG JFIF or Exif, TIFF.
func resolution(path string) float64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	head := make([]byte, 256<<10)
	n, _ := f.Read(head)
	d := head[:n]
	switch {
	case bytes.HasPrefix(d, []byte("\x89PNG\r\n\x1a\n")):
		for at := 8; at+12 <= len(d); {
			l := int(binary.BigEndian.Uint32(d[at:]))
			typ := string(d[at+4 : at+8])
			if typ == "pHYs" && l >= 9 && at+8+9 <= len(d) {
				x := float64(binary.BigEndian.Uint32(d[at+8:]))
				if d[at+16] == 1 { // per metre
					return x * 0.0254
				}
				return 0
			}
			if typ == "IDAT" || l < 0 {
				return 0
			}
			at += 12 + l
		}
	case len(d) > 4 && d[0] == 0xff && d[1] == 0xd8:
		for at := 2; at+4 <= len(d) && d[at] == 0xff; {
			m := d[at+1]
			l := int(binary.BigEndian.Uint16(d[at+2:]))
			if m == 0xda || at+2+l > len(d) {
				break
			}
			p := d[at+4 : at+2+l]
			if m == 0xe0 && bytes.HasPrefix(p, []byte("JFIF\x00")) && len(p) >= 12 {
				units, x := p[7], float64(binary.BigEndian.Uint16(p[8:]))
				switch units {
				case 1:
					return x
				case 2:
					return x * 2.54
				}
			}
			if m == 0xe1 && bytes.HasPrefix(p, []byte("Exif\x00\x00")) {
				if r := tiffResolution(p[6:]); r > 0 {
					return r
				}
			}
			at += 2 + l
		}
	case bytes.HasPrefix(d, []byte("II*\x00")) || bytes.HasPrefix(d, []byte("MM\x00*")):
		return tiffResolution(d)
	}
	return 0
}

// tiffResolution reads XResolution (282) and ResolutionUnit (296) of the first IFD.
func tiffResolution(t []byte) float64 {
	if len(t) < 8 {
		return 0
	}
	var o binary.ByteOrder = binary.BigEndian
	if t[0] == 'I' {
		o = binary.LittleEndian
	}
	ifd := int(o.Uint32(t[4:]))
	if ifd <= 0 || ifd+2 > len(t) {
		return 0
	}
	n := int(o.Uint16(t[ifd:]))
	x, unit := 0.0, 2
	for i := 0; i < n && ifd+2+i*12+12 <= len(t); i++ {
		e := t[ifd+2+i*12:]
		switch o.Uint16(e) {
		case 282:
			off := int(o.Uint32(e[8:]))
			if off+8 <= len(t) {
				if den := o.Uint32(t[off+4:]); den != 0 {
					x = float64(o.Uint32(t[off:])) / float64(den)
				}
			}
		case 296:
			unit = int(o.Uint16(e[8:]))
		}
	}
	switch unit {
	case 3:
		return x * 2.54
	case 2:
		return x
	}
	return 0
}

// visionPicture is one page of a picture: where it is, its size in pixels and its resolution.
type visionPicture struct {
	path          string
	width, height int
	dpi           float64
}

// picturePages are the pages of a picture as files the PDF writer embeds: a
// JPEG as it is (unless Exif turns it), anything else as PNG in scratch,
// turned upright. TIFF gives its first page (Go's TIFF reader reads one).
func picturePages(path, format, scratch string) ([]visionPicture, error) {
	dpi := resolution(path)
	if dpi <= 1 {
		dpi = Dpi
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if format == "jpg" && jpegOrientation(data) <= 1 {
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		if cfg.ColorModel != nil {
			return []visionPicture{{path, cfg.Width, cfg.Height, dpi}}, nil
		}
	}
	var img image.Image
	if format == "tiff" {
		img, err = tiff.Decode(bytes.NewReader(data))
	} else {
		img, _, err = image.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read the picture: %w", err)
	}
	if format == "jpg" {
		img = orient(img, jpegOrientation(data))
	}
	out := filepath.Join(scratch, "picture-0.png")
	if err := writePng(img, out); err != nil {
		return nil, err
	}
	b := img.Bounds()
	return []visionPicture{{out, b.Dx(), b.Dy(), dpi}}, nil
}

func jpegOrientation(d []byte) int {
	for at := 2; at+4 <= len(d) && d[at] == 0xff; {
		m := d[at+1]
		l := int(binary.BigEndian.Uint16(d[at+2:]))
		if m == 0xda || at+2+l > len(d) {
			break
		}
		p := d[at+4 : at+2+l]
		if m == 0xe1 && bytes.HasPrefix(p, []byte("Exif\x00\x00")) && len(p) > 14 {
			t := p[6:]
			var o binary.ByteOrder = binary.BigEndian
			if t[0] == 'I' {
				o = binary.LittleEndian
			}
			ifd := int(o.Uint32(t[4:]))
			if ifd+2 <= len(t) {
				for i := 0; i < int(o.Uint16(t[ifd:])) && ifd+2+i*12+12 <= len(t); i++ {
					e := t[ifd+2+i*12:]
					if o.Uint16(e) == 0x0112 {
						return int(o.Uint16(e[8:]))
					}
				}
			}
		}
		at += 2 + l
	}
	return 1
}

// orient turns or mirrors img by an Exif orientation (1–8).
func orient(img image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}
