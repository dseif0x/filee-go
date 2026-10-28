package images

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"strconv"
	"strings"
)

// MIFF, ImageMagick's own format: a text header of key=value pairs ending in
// ":\x1a", an optional colour map, then the pixels (uncompressed, RLE or Zip).
// Read here with the common classes and colour spaces; written uncompressed,
// 8 bits, direct class.

func decodeMIFF(data []byte) (*Picture, error) {
	p := &Picture{}
	for len(data) > 0 {
		f, rest, err := decodeMIFFImage(data)
		if err != nil {
			if len(p.Frames) > 0 {
				break
			}
			return nil, err
		}
		p.Frames = append(p.Frames, f)
		data = bytes.TrimLeft(rest, "\x00\n\r\t \f")
		if !bytes.HasPrefix(data, []byte("id=")) {
			break
		}
	}
	return p, nil
}

func decodeMIFFImage(data []byte) (*Frame, []byte, error) {
	end := bytes.Index(data, []byte(":\x1a"))
	if end < 0 || !bytes.Contains(data[:min(end, 4096)], []byte("ImageMagick")) {
		return nil, nil, errors.New("the file is not a MIFF image")
	}
	header := map[string]string{}
	text := string(data[:end])
	for len(text) > 0 {
		text = strings.TrimLeft(text, " \t\r\n\f")
		if strings.HasPrefix(text, "{") { // comment
			if i := strings.IndexByte(text, '}'); i >= 0 {
				text = text[i+1:]
				continue
			}
			break
		}
		eq := strings.IndexByte(text, '=')
		if eq < 0 {
			break
		}
		key := strings.ToLower(strings.TrimSpace(text[:eq]))
		text = text[eq+1:]
		var val string
		if strings.HasPrefix(text, "{") {
			i := strings.IndexByte(text, '}')
			if i < 0 {
				break
			}
			val, text = text[1:i], text[i+1:]
		} else {
			i := strings.IndexAny(text, " \t\r\n\f")
			if i < 0 {
				i = len(text)
			}
			val, text = text[:i], text[i:]
		}
		header[key] = val
	}
	data = data[end+2:]
	num := func(k string, d int) int {
		if v, err := strconv.Atoi(header[k]); err == nil {
			return v
		}
		return d
	}
	w, h := num("columns", 0), num("rows", 0)
	depth := num("depth", 8)
	if w <= 0 || h <= 0 || int64(w)*int64(h) > 1<<30 || (depth != 8 && depth != 16 && depth != 32) {
		return nil, nil, errors.New("the MIFF file is damaged (size or depth)")
	}
	bps := depth / 8
	class := strings.ToLower(header["class"])
	space := strings.ToLower(header["colorspace"])
	alpha := strings.EqualFold(header["matte"], "true") ||
		(header["alpha-trait"] != "" && !strings.EqualFold(header["alpha-trait"], "undefined"))
	gray := space == "gray" || space == "lineargray"
	cmyk := space == "cmyk"
	sample := func(b []byte) uint8 {
		switch bps {
		case 2:
			return b[0]
		case 4:
			if strings.EqualFold(header["quantum:format"], "floating-point") {
				var v float32
				bits := binary.BigEndian.Uint32(b)
				if strings.EqualFold(header["endian"], "lsb") {
					bits = binary.LittleEndian.Uint32(b)
				}
				v = float32frombits(bits)
				return unit8(float64(v))
			}
			return b[0]
		}
		return b[0]
	}
	var colormap [][3]uint8
	if class == "pseudoclass" {
		n := num("colors", 0)
		if n <= 0 || n > 65536 || n*3*bps > len(data) {
			return nil, nil, errors.New("the MIFF file is damaged (colour map)")
		}
		for i := 0; i < n; i++ {
			e := data[i*3*bps:]
			colormap = append(colormap, [3]uint8{sample(e), sample(e[bps:]), sample(e[2*bps:])})
		}
		data = data[n*3*bps:]
	}
	var channels int
	switch {
	case class == "pseudoclass":
		channels = 1
		if len(colormap) > 256 {
			bps = 2
		} else {
			bps = 1
		}
	case gray:
		channels = 1
	case cmyk:
		channels = 4
	default:
		channels = 3
	}
	if alpha {
		channels++
	}
	packet := channels * bps
	if class == "pseudoclass" && alpha {
		packet = bps + depth/8
	}
	need := packet * w * h
	var pixels []byte
	rest := data
	switch strings.ToLower(header["compression"]) {
	case "", "none", "undefined":
		if len(data) < need {
			return nil, nil, errors.New("the MIFF file is cut short")
		}
		pixels, rest = data[:need], data[need:]
	case "rle":
		for len(pixels) < need && len(data) >= packet+1 {
			pk, count := data[:packet], int(data[packet])+1
			for k := 0; k < count; k++ {
				pixels = append(pixels, pk...)
			}
			data = data[packet+1:]
		}
		if len(pixels) < need {
			return nil, nil, errors.New("the MIFF file is cut short")
		}
		pixels, rest = pixels[:need], data
	case "zip":
		var stream []byte
		for len(data) >= 4 {
			n := int(binary.BigEndian.Uint32(data))
			if n <= 0 || 4+n > len(data) {
				break
			}
			stream = append(stream, data[4:4+n]...)
			data = data[4+n:]
			if out, err := inflatePartial(stream, need); err == nil && len(out) >= need {
				pixels = out
				break
			}
		}
		if len(pixels) < need {
			return nil, nil, errors.New("the MIFF file is damaged (Zip data)")
		}
		pixels, rest = pixels[:need], data
	default:
		return nil, nil, fmt.Errorf("MIFF compression %q is not supported", header["compression"])
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		s := pixels[i*packet:]
		var c [4]uint8
		switch {
		case class == "pseudoclass":
			idx := int(s[0])
			if bps == 2 {
				idx = int(binary.BigEndian.Uint16(s))
			}
			if idx < len(colormap) {
				c = [4]uint8{colormap[idx][0], colormap[idx][1], colormap[idx][2], 255}
			}
			if alpha {
				c[3] = sample(s[bps:])
			}
		case gray:
			v := sample(s)
			c = [4]uint8{v, v, v, 255}
			if alpha {
				c[3] = sample(s[bps:])
			}
		case cmyk:
			cc, m, y, k := 255-uint32(sample(s)), 255-uint32(sample(s[bps:])), 255-uint32(sample(s[2*bps:])), 255-uint32(sample(s[3*bps:]))
			c = [4]uint8{uint8(cc * k / 255), uint8(m * k / 255), uint8(y * k / 255), 255}
			if alpha {
				c[3] = sample(s[4*bps:])
			}
		default:
			c = [4]uint8{sample(s), sample(s[bps:]), sample(s[2*bps:]), 255}
			if alpha {
				c[3] = sample(s[3*bps:])
			}
		}
		copy(img.Pix[i*4:], c[:])
	}
	f := &Frame{Img: img, Alpha: alpha, Gray: gray}
	if v := header["delay"]; v != "" {
		f.Delay, _ = strconv.Atoi(v)
		f.Ticks = 100
	}
	if res := header["resolution"]; res != "" {
		var x, y float64
		if n, _ := fmt.Sscanf(strings.Replace(res, "x", " ", 1), "%g %g", &x, &y); n == 2 {
			if strings.EqualFold(header["units"], "pixelspercentimeter") {
				x, y = x*2.54, y*2.54
			}
			f.Meta.DpiX, f.Meta.DpiY = x, y
		}
	}
	return f, rest, nil
}

func encodeMIFF(frames []*Frame) []byte {
	var b bytes.Buffer
	for _, f := range frames {
		w, h := f.Width(), f.Height()
		space := "sRGB"
		if f.Gray {
			space = "Gray"
		}
		trait := "Undefined"
		if f.Alpha {
			trait = "Blend"
		}
		fmt.Fprintf(&b, "id=ImageMagick  version=1.0\nclass=DirectClass  colorspace=%s  alpha-trait=%s\ncompression=None  columns=%d  rows=%d  depth=8\n", space, trait, w, h)
		if f.Meta.DpiX > 0 {
			fmt.Fprintf(&b, "resolution=%gx%g  units=PixelsPerInch\n", f.Meta.DpiX, f.Meta.DpiY)
		}
		if f.Delay > 0 {
			fmt.Fprintf(&b, "delay=%d  ticks-per-second=100\n", int(f.DelayMs()/10+0.5))
		}
		b.WriteString("\f\n:\x1a")
		for y := 0; y < h; y++ {
			row := f.Img.Pix[y*f.Img.Stride:]
			for x := 0; x < w; x++ {
				s := row[x*4:]
				switch {
				case f.Gray && f.Alpha:
					b.Write([]byte{s[0], s[3]})
				case f.Gray:
					b.WriteByte(s[0])
				case f.Alpha:
					b.Write(s[:4])
				default:
					b.Write(s[:3])
				}
			}
		}
	}
	return b.Bytes()
}
