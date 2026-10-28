package images

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"
)

// Netpbm: PBM, PGM, PPM (plain and raw), PAM and PFM, read and written by
// Filee itself.

// decodeNetpbm reads any of P1–P7, PF and Pf.
func decodeNetpbm(data []byte) (*Picture, error) {
	if len(data) < 3 || data[0] != 'P' {
		return nil, errors.New("the file is not a Netpbm image")
	}
	switch data[1] {
	case '7':
		return decodePAM(data)
	case 'F', 'f':
		return decodePFM(data)
	}
	kind := data[1] - '0'
	if kind < 1 || kind > 6 {
		return nil, errors.New("the file is not a Netpbm image")
	}
	p := 2
	token := func() (int, error) {
		for p < len(data) {
			c := data[p]
			if c == '#' {
				for p < len(data) && data[p] != '\n' && data[p] != '\r' {
					p++
				}
				continue
			}
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f' {
				p++
				continue
			}
			break
		}
		start := p
		for p < len(data) && data[p] >= '0' && data[p] <= '9' {
			p++
		}
		if start == p {
			return 0, errors.New("the Netpbm file is damaged")
		}
		return strconv.Atoi(string(data[start:p]))
	}
	w, err := token()
	if err != nil {
		return nil, err
	}
	h, err := token()
	if err != nil {
		return nil, err
	}
	maxv := 1
	if kind != 1 && kind != 4 {
		if maxv, err = token(); err != nil {
			return nil, err
		}
	}
	if w <= 0 || h <= 0 || maxv <= 0 || maxv > 65535 || int64(w)*int64(h) > 1<<30 {
		return nil, errors.New("the Netpbm file is damaged (size)")
	}
	p++ // the single whitespace before raw data
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	scale := func(v int) uint8 { return uint8(min(255, (v*255+maxv/2)/maxv)) }
	channels := map[byte]int{1: 1, 2: 1, 3: 3, 4: 1, 5: 1, 6: 3}[kind]
	f := &Frame{Img: img, Gray: channels == 1}
	switch kind {
	case 1, 2, 3:
		p--
		for i := 0; i < w*h; i++ {
			var v [3]int
			for c := 0; c < channels; c++ {
				if kind == 1 {
					// plain PBM digits may be packed without spaces
					for p < len(data) && (data[p] != '0' && data[p] != '1') {
						p++
					}
					if p >= len(data) {
						return nil, errors.New("the Netpbm file is cut short")
					}
					v[c] = int(data[p] - '0')
					p++
					continue
				}
				if v[c], err = token(); err != nil {
					return nil, errors.New("the Netpbm file is cut short")
				}
			}
			o := i * 4
			switch kind {
			case 1:
				g := uint8(255)
				if v[0] == 1 {
					g = 0
				}
				img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = g, g, g, 255
			case 2:
				g := scale(v[0])
				img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = g, g, g, 255
			default:
				img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = scale(v[0]), scale(v[1]), scale(v[2]), 255
			}
		}
	case 4:
		stride := (w + 7) / 8
		if p+stride*h > len(data) {
			return nil, errors.New("the Netpbm file is cut short")
		}
		for y := 0; y < h; y++ {
			row := data[p+y*stride:]
			for x := 0; x < w; x++ {
				g := uint8(255)
				if row[x/8]>>(7-x%8)&1 == 1 {
					g = 0
				}
				o := y*img.Stride + x*4
				img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = g, g, g, 255
			}
		}
	default:
		bps := 1
		if maxv > 255 {
			bps = 2
		}
		if p+w*h*channels*bps > len(data) {
			return nil, errors.New("the Netpbm file is cut short")
		}
		readRaw(data[p:], img, channels, bps, false, scale)
	}
	return one(f), nil
}

// readRaw fills img from interleaved samples (1–4 channels; 2 and 4 carry alpha).
func readRaw(data []byte, img *image.NRGBA, channels, bps int, _ bool, scale func(int) uint8) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	sample := func(i int) uint8 {
		if bps == 2 {
			return scale(int(binary.BigEndian.Uint16(data[i*2:])))
		}
		return scale(int(data[i]))
	}
	for i := 0; i < w*h; i++ {
		o := i * 4
		s := i * channels
		switch channels {
		case 1:
			g := sample(s)
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = g, g, g, 255
		case 2:
			g := sample(s)
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = g, g, g, sample(s+1)
		case 3:
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = sample(s), sample(s+1), sample(s+2), 255
		default:
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = sample(s), sample(s+1), sample(s+2), sample(s+3)
		}
	}
}

func decodePAM(data []byte) (*Picture, error) {
	end := bytes.Index(data, []byte("ENDHDR\n"))
	if end < 0 {
		return nil, errors.New("the PAM file is damaged (no ENDHDR)")
	}
	var w, h, depth, maxv int
	tupl := ""
	for _, line := range strings.Split(string(data[3:end]), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		v, _ := strconv.Atoi(fields[1])
		switch fields[0] {
		case "WIDTH":
			w = v
		case "HEIGHT":
			h = v
		case "DEPTH":
			depth = v
		case "MAXVAL":
			maxv = v
		case "TUPLTYPE":
			tupl = strings.Join(fields[1:], " ")
		}
	}
	if w <= 0 || h <= 0 || depth < 1 || depth > 4 || maxv <= 0 || maxv > 65535 || int64(w)*int64(h) > 1<<30 {
		return nil, errors.New("the PAM file is damaged (header)")
	}
	data = data[end+7:]
	bps := 1
	if maxv > 255 {
		bps = 2
	}
	if len(data) < w*h*depth*bps {
		return nil, errors.New("the PAM file is cut short")
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	scale := func(v int) uint8 { return uint8(min(255, (v*255+maxv/2)/maxv)) }
	if strings.HasPrefix(tupl, "BLACKANDWHITE") && maxv == 1 {
		scale = func(v int) uint8 { return uint8(v * 255) }
	}
	readRaw(data, img, depth, bps, false, scale)
	return one(&Frame{Img: img, Gray: depth <= 2, Alpha: depth == 2 || depth == 4}), nil
}

// decodePFM reads Portable FloatMap: bottom-up rows of 32-bit floats, the
// scale's sign the byte order; values 0–1 become 0–255 as they are (ImageMagick
// reads them without a colour conversion).
func decodePFM(data []byte) (*Picture, error) {
	color := data[1] == 'F'
	fields := make([]string, 0, 3)
	p := 2
	for len(fields) < 3 && p < len(data) {
		for p < len(data) && (data[p] == ' ' || data[p] == '\n' || data[p] == '\r' || data[p] == '\t') {
			p++
		}
		start := p
		for p < len(data) && !(data[p] == ' ' || data[p] == '\n' || data[p] == '\r' || data[p] == '\t') {
			p++
		}
		fields = append(fields, string(data[start:p]))
	}
	p++
	if len(fields) < 3 {
		return nil, errors.New("the PFM file is damaged")
	}
	w, _ := strconv.Atoi(fields[0])
	h, _ := strconv.Atoi(fields[1])
	scale, err := strconv.ParseFloat(fields[2], 64)
	if err != nil || w <= 0 || h <= 0 || int64(w)*int64(h) > 1<<28 {
		return nil, errors.New("the PFM file is damaged")
	}
	var order binary.ByteOrder = binary.BigEndian
	if scale < 0 {
		order = binary.LittleEndian
	}
	channels := 1
	if color {
		channels = 3
	}
	if p+w*h*channels*4 > len(data) {
		return nil, errors.New("the PFM file is cut short")
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		row := data[p+(h-1-y)*w*channels*4:]
		for x := 0; x < w; x++ {
			o := y*img.Stride + x*4
			for c := 0; c < 3; c++ {
				cc := c
				if channels == 1 {
					cc = 0
				}
				v := math.Float32frombits(order.Uint32(row[(x*channels+cc)*4:]))
				img.Pix[o+c] = unit8(float64(v))
			}
			img.Pix[o+3] = 255
		}
	}
	return one(&Frame{Img: img, Gray: !color}), nil
}

// encodeNetpbm writes P6 (ppm), P5 (pgm), P4 (pbm), P7 (pam) or PF/Pf (pfm).
func encodeNetpbm(f *Frame, ext string) []byte {
	w, h := f.Width(), f.Height()
	img := f.Img
	var b bytes.Buffer
	switch ext {
	case "pbm":
		fmt.Fprintf(&b, "P4\n%d %d\n", w, h)
		stride := (w + 7) / 8
		row := make([]byte, stride)
		for y := 0; y < h; y++ {
			clear(row)
			for x := 0; x < w; x++ {
				s := img.Pix[y*img.Stride+x*4:]
				if luma(s[0], s[1], s[2]) < 128 {
					row[x/8] |= 0x80 >> (x % 8)
				}
			}
			b.Write(row)
		}
	case "pgm":
		fmt.Fprintf(&b, "P5\n%d %d\n255\n", w, h)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				s := img.Pix[y*img.Stride+x*4:]
				b.WriteByte(luma(s[0], s[1], s[2]))
			}
		}
	case "pam":
		depth, tupl := 3, "RGB"
		switch {
		case f.Gray && f.Alpha:
			depth, tupl = 2, "GRAYSCALE_ALPHA"
		case f.Gray:
			depth, tupl = 1, "GRAYSCALE"
		case f.Alpha:
			depth, tupl = 4, "RGB_ALPHA"
		}
		fmt.Fprintf(&b, "P7\nWIDTH %d\nHEIGHT %d\nDEPTH %d\nMAXVAL 255\nTUPLTYPE %s\nENDHDR\n", w, h, depth, tupl)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				s := img.Pix[y*img.Stride+x*4:]
				switch depth {
				case 1:
					b.WriteByte(s[0])
				case 2:
					b.Write([]byte{s[0], s[3]})
				case 3:
					b.Write(s[:3])
				default:
					b.Write(s[:4])
				}
			}
		}
	case "pfm":
		if f.Gray {
			fmt.Fprintf(&b, "Pf\n%d %d\n-1.0\n", w, h)
		} else {
			fmt.Fprintf(&b, "PF\n%d %d\n-1.0\n", w, h)
		}
		var v [4]byte
		for y := h - 1; y >= 0; y-- {
			for x := 0; x < w; x++ {
				s := img.Pix[y*img.Stride+x*4:]
				n := 3
				if f.Gray {
					n = 1
				}
				for c := 0; c < n; c++ {
					binary.LittleEndian.PutUint32(v[:], math.Float32bits(float32(s[c])/255))
					b.Write(v[:])
				}
			}
		}
	default:
		fmt.Fprintf(&b, "P6\n%d %d\n255\n", w, h)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				b.Write(img.Pix[y*img.Stride+x*4 : y*img.Stride+x*4+3])
			}
		}
	}
	return b.Bytes()
}

func luma(r, g, b uint8) uint8 {
	return uint8(min(255, 0.212656*float64(r)+0.715158*float64(g)+0.072186*float64(b)+0.5))
}

// linearToSRGB8 encodes a linear value (clamped to 0–1) as an 8-bit sRGB value.
func linearToSRGB8(v float64) uint8 {
	if math.IsNaN(v) || v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	if v <= 0.0031308 {
		v *= 12.92
	} else {
		v = 1.055*math.Pow(v, 1/2.4) - 0.055
	}
	return uint8(v*255 + 0.5)
}

// unit8 maps 0–1 to 0–255, clamped.
func unit8(v float64) uint8 {
	if math.IsNaN(v) || v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint8(v*255 + 0.5)
}

func srgbToLinear(c uint8) float64 {
	v := float64(c) / 255
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}
