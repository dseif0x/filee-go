package images

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"

	"github.com/mrjoshuak/go-openexr/exr"
	"github.com/woozymasta/bcn"
)

// High dynamic range pictures (OpenEXR, Radiance HDR) hold linear light: as
// ImageMagick does, they become sRGB with values above 1 clipped. DDS textures
// are read and written by github.com/woozymasta/bcn.

func decodeEXR(data []byte) (*Picture, error) {
	f, err := exr.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("OpenEXR: %w", err)
	}
	defer f.Close()
	in, err := exr.NewRGBAInputFile(f)
	if err != nil {
		return nil, fmt.Errorf("OpenEXR: %w", err)
	}
	rgba, err := in.ReadRGBA()
	if err != nil {
		return nil, fmt.Errorf("OpenEXR: %w", err)
	}
	r := rgba.Rect
	w, h := r.Dx(), r.Dy()
	if w <= 0 || h <= 0 {
		return nil, errors.New("the OpenEXR picture is empty")
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	alpha := false
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			rr, g, b, a := rgba.RGBA(r.Min.X+x, r.Min.Y+y)
			o := y*img.Stride + x*4
			img.Pix[o], img.Pix[o+1], img.Pix[o+2] = linearToSRGB8(float64(rr)), linearToSRGB8(float64(g)), linearToSRGB8(float64(b))
			img.Pix[o+3] = unit8(float64(a))
			if img.Pix[o+3] != 255 {
				alpha = true
			}
		}
	}
	return one(&Frame{Img: img, Alpha: alpha}), nil
}

// decodeHDR reads a Radiance RGBE picture (flat or with adaptive run-length rows).
func decodeHDR(data []byte) (*Picture, error) {
	if !bytes.HasPrefix(data, []byte("#?")) {
		return nil, errors.New("the file is not a Radiance HDR picture")
	}
	p := 0
	line := func() (string, bool) {
		i := bytes.IndexByte(data[p:], '\n')
		if i < 0 {
			return "", false
		}
		s := string(data[p : p+i])
		p += i + 1
		return strings.TrimRight(s, "\r"), true
	}
	xyze := false
	for {
		l, ok := line()
		if !ok {
			return nil, errors.New("the HDR file is damaged (header)")
		}
		if l == "" {
			break
		}
		if strings.HasPrefix(l, "FORMAT=") && strings.Contains(l, "xyze") {
			xyze = true
		}
	}
	res, ok := line()
	if !ok {
		return nil, errors.New("the HDR file is damaged (resolution)")
	}
	f := strings.Fields(res)
	if len(f) != 4 {
		return nil, errors.New("the HDR file is damaged (resolution)")
	}
	h, _ := strconv.Atoi(f[1])
	w, _ := strconv.Atoi(f[3])
	if w <= 0 || h <= 0 || int64(w)*int64(h) > 1<<29 {
		return nil, errors.New("the HDR file has an impossible size")
	}
	flipY := f[0] == "+Y"
	flipX := f[2] == "-X"
	transposed := f[0] == "-X" || f[0] == "+X"
	if transposed {
		w, h = h, w
	}
	rgbe := make([]byte, w*h*4)
	row := 0
	for row < h && p < len(data) {
		dst := rgbe[row*w*4 : (row+1)*w*4]
		if w >= 8 && w < 32768 && p+4 <= len(data) && data[p] == 2 && data[p+1] == 2 && data[p+2]&0x80 == 0 {
			if int(data[p+2])<<8|int(data[p+3]) != w {
				return nil, errors.New("the HDR file is damaged (scanline length)")
			}
			p += 4
			for c := 0; c < 4; c++ {
				for x := 0; x < w && p < len(data); {
					n := int(data[p])
					p++
					if n > 128 {
						n -= 128
						if p >= len(data) {
							break
						}
						v := data[p]
						p++
						for k := 0; k < n && x < w; k++ {
							dst[x*4+c] = v
							x++
						}
					} else {
						for k := 0; k < n && x < w && p < len(data); k++ {
							dst[x*4+c] = data[p]
							p++
							x++
						}
					}
				}
			}
		} else {
			// flat (or old run-length) pixels
			for x := 0; x < w && p+4 <= len(data); {
				if data[p] == 1 && data[p+1] == 1 && data[p+2] == 1 && x > 0 {
					n := int(data[p+3])
					for k := 0; k < n && x < w; k++ {
						copy(dst[x*4:x*4+4], dst[(x-1)*4:x*4])
						x++
					}
				} else {
					copy(dst[x*4:x*4+4], data[p:p+4])
					x++
				}
				p += 4
			}
		}
		row++
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			s := rgbe[(y*w+x)*4:]
			var c [3]float64
			if s[3] != 0 {
				scale := math.Ldexp(1, int(s[3])-136)
				c = [3]float64{(float64(s[0]) + 0.5) * scale, (float64(s[1]) + 0.5) * scale, (float64(s[2]) + 0.5) * scale}
			}
			if xyze {
				x0, y0, z0 := c[0], c[1], c[2]
				c = [3]float64{3.2404542*x0 - 1.5371385*y0 - 0.4985314*z0, -0.9692660*x0 + 1.8760108*y0 + 0.0415560*z0, 0.0556434*x0 - 0.2040259*y0 + 1.0572252*z0}
			}
			dx, dy := x, y
			if flipX {
				dx = w - 1 - x
			}
			if flipY {
				dy = h - 1 - y
			}
			o := dy*img.Stride + dx*4
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = linearToSRGB8(c[0]), linearToSRGB8(c[1]), linearToSRGB8(c[2]), 255
		}
	}
	if transposed {
		img = transpose(img)
	}
	return one(&Frame{Img: img}), nil
}

func decodeDDS(data []byte) (*Picture, error) {
	_, img, err := bcn.DecodeDDS(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("DDS: %w", err)
	}
	return one(&Frame{Img: ToNRGBA(img), Alpha: !opaque(img)}), nil
}

// encodeDDS writes DXT5 (BC3) when the picture has transparency, else DXT1
// (BC1), with mipmaps: ImageMagick's defaults.
func encodeDDS(f *Frame) ([]byte, error) {
	format := bcn.FormatBC1
	if f.Alpha {
		format = bcn.FormatBC3
	}
	d, err := bcn.EncodeDDSWithOptions([]image.Image{f.Img}, format, &bcn.EncodeOptions{GenerateMipmaps: true, UseSRGB: true, QualityLevel: 6})
	if err != nil {
		return nil, fmt.Errorf("DDS: %w", err)
	}
	var buf bytes.Buffer
	if err := d.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
