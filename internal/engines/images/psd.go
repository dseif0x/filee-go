package images

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"math"
)

// Photoshop documents (PSD, and PSB, the large document format), read and
// written by Filee itself. Reading takes the composite Photoshop stores of all
// visible layers; a file saved without one ("maximize compatibility" off) is
// composed from its layers (normal blending, opacity and visibility). Writing
// stores the picture as one layer plus the composite.

type psdReader struct {
	b   []byte
	p   int
	psb bool
	err error
}

func (r *psdReader) n(k int) []byte {
	if r.err != nil || r.p+k > len(r.b) || k < 0 {
		r.err = errors.New("the Photoshop file is cut short")
		return make([]byte, max(0, k))
	}
	s := r.b[r.p : r.p+k]
	r.p += k
	return s
}
func (r *psdReader) u16() int { return int(binary.BigEndian.Uint16(r.n(2))) }
func (r *psdReader) u32() int { return int(binary.BigEndian.Uint32(r.n(4))) }
func (r *psdReader) length() int {
	if r.psb {
		return int(binary.BigEndian.Uint64(r.n(8)))
	}
	return r.u32()
}

type psdLayer struct {
	top, left, bottom, right int
	channels                 []struct{ id, length int }
	opacity                  int
	hidden                   bool
	blend                    string
	planes                   map[int][]byte
}

func decodePSD(data []byte) (*Picture, error) {
	r := &psdReader{b: data}
	if string(r.n(4)) != "8BPS" {
		return nil, errors.New("the file is not a Photoshop document")
	}
	version := r.u16()
	if version != 1 && version != 2 {
		return nil, fmt.Errorf("Photoshop file version %d is not supported", version)
	}
	r.psb = version == 2
	r.n(6)
	channels, h, w, depth, mode := r.u16(), r.u32(), r.u32(), r.u16(), r.u16()
	if r.err != nil || w <= 0 || h <= 0 || int64(w)*int64(h) > 1<<31 || channels < 1 || channels > 56 {
		return nil, errors.New("the Photoshop file is damaged (header)")
	}
	if depth != 1 && depth != 8 && depth != 16 && depth != 32 {
		return nil, fmt.Errorf("Photoshop depth %d is not supported", depth)
	}
	palette := r.n(r.u32())
	f := &Frame{}
	merged := true
	resources := r.n(r.u32())
	for q := 0; q+12 <= len(resources); {
		if string(resources[q:q+4]) != "8BIM" {
			break
		}
		id := int(binary.BigEndian.Uint16(resources[q+4:]))
		nameLen := int(resources[q+6])
		q += 6 + (nameLen+2)&^1
		if q+4 > len(resources) {
			break
		}
		size := int(binary.BigEndian.Uint32(resources[q:]))
		q += 4
		if q+size > len(resources) {
			break
		}
		d := resources[q : q+size]
		switch id {
		case 1005:
			if len(d) >= 16 {
				x := float64(binary.BigEndian.Uint32(d)) / 65536
				y := float64(binary.BigEndian.Uint32(d[8:])) / 65536
				if binary.BigEndian.Uint16(d[4:]) == 2 {
					x *= 2.54
				}
				if binary.BigEndian.Uint16(d[12:]) == 2 {
					y *= 2.54
				}
				f.Meta.DpiX, f.Meta.DpiY = x, y
			}
		case 1039:
			f.Meta.ICC = bytes.Clone(d)
		case 1058:
			f.Meta.Exif = normalizeExif(bytes.Clone(d))
		case 1060:
			f.Meta.XMP = bytes.Clone(d)
		case 1057:
			if len(d) >= 5 && d[4] == 0 {
				merged = false
			}
		}
		q += (size + 1) &^ 1
	}
	layerStart := r.p
	layerLen := r.length()
	layerEnd := r.p + layerLen
	if r.err != nil || layerEnd > len(data) {
		return nil, errors.New("the Photoshop file is damaged (layer section)")
	}
	var layers []*psdLayer
	if !merged && layerLen > 0 {
		layers, _ = readPSDLayers(r, depth)
	}
	_ = layerStart
	r.p = layerEnd
	f.Meta.Orientation = exifOrientation(f.Meta.Exif)
	if !merged && len(layers) > 0 {
		img := image.NewNRGBA(image.Rect(0, 0, w, h))
		for _, l := range layers {
			if l.hidden {
				continue
			}
			lw, lh := l.right-l.left, l.bottom-l.top
			if lw <= 0 || lh <= 0 {
				continue
			}
			li := image.NewNRGBA(image.Rect(0, 0, lw, lh))
			fill(li, colorWhite)
			for id, plane := range l.planes {
				c := -1
				switch {
				case id >= 0 && id <= 2 && mode == 3:
					c = id
				case id == 0 && mode == 1:
					c = 4
				case id == -1:
					c = 3
				}
				if c < 0 {
					continue
				}
				for i := 0; i < lw*lh && i < len(plane); i++ {
					if c == 4 {
						li.Pix[i*4], li.Pix[i*4+1], li.Pix[i*4+2] = plane[i], plane[i], plane[i]
					} else {
						li.Pix[i*4+c] = plane[i]
					}
				}
			}
			r := image.Rect(l.left, l.top, l.right, l.bottom).Intersect(img.Rect)
			for y := r.Min.Y; y < r.Max.Y; y++ {
				for x := r.Min.X; x < r.Max.X; x++ {
					s := li.Pix[(y-l.top)*li.Stride+(x-l.left)*4:]
					d := img.Pix[y*img.Stride+x*4:]
					blendOver(d[:4], s[:4], uint32(l.opacity))
				}
			}
		}
		f.Img, f.Alpha, f.Gray = img, true, mode == 1
		return one(f), nil
	}
	// composite image data
	compression := r.u16()
	planes, err := readPSDPlanes(r, compression, channels, w, h, depth)
	if err != nil {
		return nil, err
	}
	img, alpha, gray, err := psdCompose(planes, mode, w, h, palette, layerLen > 0)
	if err != nil {
		return nil, err
	}
	f.Img, f.Alpha, f.Gray = img, alpha, gray
	return one(f), nil
}

// readPSDPlanes reads n planes of w × h samples (raw or RLE) as 8-bit values.
func readPSDPlanes(r *psdReader, compression, n, w, h, depth int) ([][]byte, error) {
	rowBytes := (w*depth + 7) / 8
	planes := make([][]byte, n)
	switch compression {
	case 0:
		for c := 0; c < n; c++ {
			planes[c] = to8(r.n(rowBytes*h), w, h, depth)
		}
	case 1:
		counts := make([]int, n*h)
		for i := range counts {
			if r.psb {
				counts[i] = r.u32()
			} else {
				counts[i] = r.u16()
			}
		}
		for c := 0; c < n; c++ {
			raw := make([]byte, 0, rowBytes*h)
			for y := 0; y < h; y++ {
				row := unpackBits(r.n(counts[c*h+y]), rowBytes)
				raw = append(raw, row...)
			}
			planes[c] = to8(raw, w, h, depth)
		}
	default:
		return nil, fmt.Errorf("Photoshop compression %d (ZIP) is not supported for the composite", compression)
	}
	if r.err != nil {
		return nil, r.err
	}
	return planes, nil
}

// to8 brings samples of depth bits down to one byte each.
func to8(raw []byte, w, h, depth int) []byte {
	out := make([]byte, w*h)
	switch depth {
	case 8:
		copy(out, raw)
	case 16:
		for i := range out {
			if i*2 < len(raw) {
				out[i] = raw[i*2]
			}
		}
	case 32:
		for i := range out {
			if i*4+4 <= len(raw) {
				out[i] = unit8(float64(math.Float32frombits(binary.BigEndian.Uint32(raw[i*4:]))))
			}
		}
	case 1:
		stride := (w + 7) / 8
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if y*stride+x/8 < len(raw) && raw[y*stride+x/8]>>(7-x%8)&1 == 0 {
					out[y*w+x] = 255
				}
			}
		}
	}
	return out
}

func psdCompose(planes [][]byte, mode, w, h int, palette []byte, hasLayers bool) (*image.NRGBA, bool, bool, error) {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	alpha, gray := false, false
	n := w * h
	at := func(c, i int) byte {
		if c < len(planes) {
			return planes[c][i]
		}
		return 255
	}
	switch mode {
	case 0, 1, 8: // bitmap, grey, duotone (its first ink as grey)
		gray = true
		alpha = len(planes) >= 2 && hasLayers
		for i := 0; i < n; i++ {
			v := at(0, i)
			a := byte(255)
			if alpha {
				a = at(1, i)
			}
			img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2], img.Pix[i*4+3] = v, v, v, a
		}
	case 2:
		for i := 0; i < n; i++ {
			k := int(at(0, i))
			if len(palette) >= 768 {
				img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2] = palette[k], palette[256+k], palette[512+k]
			}
			img.Pix[i*4+3] = 255
		}
	case 3:
		alpha = len(planes) >= 4
		for i := 0; i < n; i++ {
			img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2] = at(0, i), at(1, i), at(2, i)
			img.Pix[i*4+3] = 255
			if alpha {
				img.Pix[i*4+3] = at(3, i)
			}
		}
	case 4:
		// CMYK is stored inverted: 255 is no ink
		alpha = len(planes) >= 5 && hasLayers
		for i := 0; i < n; i++ {
			c, m, y, k := uint32(at(0, i)), uint32(at(1, i)), uint32(at(2, i)), uint32(at(3, i))
			img.Pix[i*4] = uint8(c * k / 255)
			img.Pix[i*4+1] = uint8(m * k / 255)
			img.Pix[i*4+2] = uint8(y * k / 255)
			img.Pix[i*4+3] = 255
			if alpha {
				img.Pix[i*4+3] = at(4, i)
			}
		}
	case 9:
		for i := 0; i < n; i++ {
			r, g, b := labToSRGB(float64(at(0, i))*100/255, float64(at(1, i))-128, float64(at(2, i))-128)
			img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2], img.Pix[i*4+3] = r, g, b, 255
		}
	case 7: // multichannel: the first three as RGB
		for i := 0; i < n; i++ {
			img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2], img.Pix[i*4+3] = at(0, i), at(min(1, len(planes)-1), i), at(min(2, len(planes)-1), i), 255
		}
	default:
		return nil, false, false, fmt.Errorf("Photoshop colour mode %d is not supported", mode)
	}
	if alpha && opaque(img) {
		alpha = true
	}
	return img, alpha, gray, nil
}

// labToSRGB converts CIE L*a*b* (D50, as Photoshop) to 8-bit sRGB.
func labToSRGB(l, a, b float64) (uint8, uint8, uint8) {
	fy := (l + 16) / 116
	fx, fz := fy+a/500, fy-b/200
	inv := func(t float64) float64 {
		if t > 6.0/29 {
			return t * t * t
		}
		return 3 * (6.0 / 29) * (6.0 / 29) * (t - 4.0/29)
	}
	x, y, z := 0.9642*inv(fx), inv(fy), 0.8249*inv(fz)
	// Bradford-adapted D50 XYZ to linear sRGB
	r := 3.1338561*x - 1.6168667*y - 0.4906146*z
	g := -0.9787684*x + 1.9161415*y + 0.0334540*z
	bl := 0.0719453*x - 0.2289914*y + 1.4052427*z
	return linearToSRGB8(r), linearToSRGB8(g), linearToSRGB8(bl)
}

func readPSDLayers(r *psdReader, depth int) ([]*psdLayer, error) {
	infoLen := r.length()
	end := r.p + infoLen
	if infoLen == 0 || r.err != nil {
		return nil, r.err
	}
	count := int(int16(r.u16()))
	if count < 0 {
		count = -count
	}
	layers := make([]*psdLayer, count)
	for i := range layers {
		l := &psdLayer{planes: map[int][]byte{}}
		l.top, l.left, l.bottom, l.right = int(int32(r.u32())), int(int32(r.u32())), int(int32(r.u32())), int(int32(r.u32()))
		nc := r.u16()
		for c := 0; c < nc && r.err == nil; c++ {
			id := int(int16(r.u16()))
			l.channels = append(l.channels, struct{ id, length int }{id, r.length()})
		}
		r.n(4) // 8BIM
		l.blend = string(r.n(4))
		l.opacity = int(r.n(1)[0])
		r.n(1)
		flags := r.n(1)[0]
		l.hidden = flags&2 != 0
		r.n(1)
		r.n(r.u32()) // mask, blending ranges, name, additional information
		layers[i] = l
		if r.err != nil {
			return nil, r.err
		}
	}
	for _, l := range layers {
		w, h := l.right-l.left, l.bottom-l.top
		for _, c := range l.channels {
			start := r.p
			comp := r.u16()
			if w > 0 && h > 0 && c.id >= -1 {
				sub := &psdReader{b: r.b[:min(len(r.b), start+c.length)], p: r.p, psb: r.psb}
				if planes, err := readPSDPlanes(sub, comp, 1, w, h, depth); err == nil {
					l.planes[c.id] = planes[0]
				}
			}
			r.p = start + c.length
		}
	}
	r.p = end
	return layers, r.err
}

// unpackBits expands PackBits data to n bytes.
func unpackBits(src []byte, n int) []byte {
	out := make([]byte, 0, n)
	for i := 0; i < len(src) && len(out) < n; {
		c := int(int8(src[i]))
		i++
		switch {
		case c >= 0:
			k := min(c+1, len(src)-i)
			out = append(out, src[i:i+k]...)
			i += c + 1
		case c != -128:
			if i < len(src) {
				for k := 0; k < 1-c; k++ {
					out = append(out, src[i])
				}
				i++
			}
		}
	}
	for len(out) < n {
		out = append(out, 0)
	}
	return out[:n]
}

// packBits compresses one row.
func packBits(row []byte) []byte {
	var out []byte
	for i := 0; i < len(row); {
		run := 1
		for i+run < len(row) && run < 128 && row[i+run] == row[i] {
			run++
		}
		if run >= 2 {
			out = append(out, byte(1-run), row[i])
			i += run
			continue
		}
		lit := 1
		for i+lit < len(row) && lit < 128 && (i+lit+1 >= len(row) || row[i+lit] != row[i+lit+1]) {
			lit++
		}
		out = append(out, byte(lit-1))
		out = append(out, row[i:i+lit]...)
		i += lit
	}
	return out
}

// encodePSD writes an 8-bit RGB (or grey) document: the picture as one layer
// (so transparency is kept) and as the composite.
func encodePSD(f *Frame, psb bool) ([]byte, error) {
	w, h := f.Width(), f.Height()
	limit := 30000
	if psb {
		limit = 300000
	}
	if w > limit || h > limit {
		return nil, fmt.Errorf("a %d × %d picture is too large for %s", w, h, map[bool]string{false: "PSD (at most 30,000 px a side)", true: "PSB"}[psb])
	}
	gray := f.Gray
	colorChannels := 3
	if gray {
		colorChannels = 1
	}
	plane := func(c int) []byte {
		p := make([]byte, w*h)
		for y := 0; y < h; y++ {
			row := f.Img.Pix[y*f.Img.Stride:]
			for x := 0; x < w; x++ {
				p[y*w+x] = row[x*4+c]
			}
		}
		return p
	}
	var b bytes.Buffer
	be := binary.BigEndian
	put16 := func(v int) { b.Write([]byte{byte(v >> 8), byte(v)}) }
	put32 := func(v int) { var t [4]byte; be.PutUint32(t[:], uint32(v)); b.Write(t[:]) }
	putLen := func(v int) {
		if psb {
			var t [8]byte
			be.PutUint64(t[:], uint64(v))
			b.Write(t[:])
		} else {
			put32(v)
		}
	}
	b.WriteString("8BPS")
	if psb {
		put16(2)
	} else {
		put16(1)
	}
	b.Write(make([]byte, 6))
	total := colorChannels
	if f.Alpha {
		total++
	}
	put16(total)
	put32(h)
	put32(w)
	put16(8)
	if gray {
		put16(1)
	} else {
		put16(3)
	}
	put32(0) // colour mode data
	var res bytes.Buffer
	resource := func(id int, d []byte) {
		res.WriteString("8BIM")
		res.Write([]byte{byte(id >> 8), byte(id), 0, 0})
		var t [4]byte
		be.PutUint32(t[:], uint32(len(d)))
		res.Write(t[:])
		res.Write(d)
		if len(d)&1 == 1 {
			res.WriteByte(0)
		}
	}
	if f.Meta.DpiX > 0 {
		d := make([]byte, 16)
		be.PutUint32(d, uint32(f.Meta.DpiX*65536))
		be.PutUint16(d[4:], 1)
		be.PutUint16(d[6:], 1)
		be.PutUint32(d[8:], uint32(f.Meta.DpiY*65536))
		be.PutUint16(d[12:], 1)
		be.PutUint16(d[14:], 1)
		resource(1005, d)
	}
	if len(f.Meta.ICC) > 0 {
		resource(1039, f.Meta.ICC)
	}
	if f.Meta.XMP != nil {
		resource(1060, f.Meta.XMP)
	}
	put32(res.Len())
	b.Write(res.Bytes())

	channelData := func(p []byte) []byte {
		// RLE: the row byte counts, then the rows
		var counts, rows bytes.Buffer
		for y := 0; y < h; y++ {
			packed := packBits(p[y*w : (y+1)*w])
			if psb {
				var t [4]byte
				be.PutUint32(t[:], uint32(len(packed)))
				counts.Write(t[:])
			} else {
				counts.Write([]byte{byte(len(packed) >> 8), byte(len(packed))})
			}
			rows.Write(packed)
		}
		return append(counts.Bytes(), rows.Bytes()...)
	}
	// the layer section: one layer holding the picture with its transparency
	var layer bytes.Buffer
	if f.Alpha {
		ids := []int{-1}
		srcs := []int{3}
		for c := 0; c < colorChannels; c++ {
			ids = append(ids, c)
			srcs = append(srcs, c)
		}
		datas := make([][]byte, len(ids))
		for i, s := range srcs {
			datas[i] = append([]byte{0, 1}, channelData(plane(s))...)
		}
		var rec bytes.Buffer
		lput16 := func(v int) { rec.Write([]byte{byte(v >> 8), byte(v)}) }
		lput32 := func(v int) { var t [4]byte; be.PutUint32(t[:], uint32(v)); rec.Write(t[:]) }
		lput16(1) // one layer
		lput32(0)
		lput32(0)
		lput32(h)
		lput32(w)
		lput16(len(ids))
		for i, id := range ids {
			lput16(id & 0xFFFF)
			if psb {
				var t [8]byte
				be.PutUint64(t[:], uint64(len(datas[i])))
				rec.Write(t[:])
			} else {
				lput32(len(datas[i]))
			}
		}
		rec.WriteString("8BIMnorm")
		rec.Write([]byte{255, 0, 0, 0})
		name := []byte{7, 'L', 'a', 'y', 'e', 'r', ' ', '1'}
		lput32(4 + 4 + len(name))
		lput32(0) // no mask
		lput32(0) // no blending ranges
		rec.Write(name)
		for _, d := range datas {
			rec.Write(d)
		}
		if rec.Len()&1 == 1 {
			rec.WriteByte(0)
		}
		if psb {
			var t [8]byte
			be.PutUint64(t[:], uint64(rec.Len()))
			layer.Write(t[:])
		} else {
			var t [4]byte
			be.PutUint32(t[:], uint32(rec.Len()))
			layer.Write(t[:])
		}
		layer.Write(rec.Bytes())
		layer.Write([]byte{0, 0, 0, 0}) // global layer mask
	}
	putLen(layer.Len())
	b.Write(layer.Bytes())

	// composite
	put16(1)
	var counts, rows bytes.Buffer
	srcs := []int{0, 1, 2}
	if gray {
		srcs = []int{0}
	}
	if f.Alpha {
		srcs = append(srcs, 3)
	}
	for _, s := range srcs {
		p := plane(s)
		for y := 0; y < h; y++ {
			packed := packBits(p[y*w : (y+1)*w])
			if psb {
				var t [4]byte
				be.PutUint32(t[:], uint32(len(packed)))
				counts.Write(t[:])
			} else {
				counts.Write([]byte{byte(len(packed) >> 8), byte(len(packed))})
			}
			rows.Write(packed)
		}
	}
	b.Write(counts.Bytes())
	b.Write(rows.Bytes())
	return b.Bytes(), nil
}
