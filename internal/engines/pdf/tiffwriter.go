package pdf

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"image"
	"io"

	"github.com/dseif0x/filee-go/internal/presets"
)

// writeTiff writes the pages as one (multi-page) TIFF, 8 bits per sample:
// gray for gray pictures, RGB or RGBA otherwise; LZW, Deflate or none, with
// horizontal prediction. x/image/tiff writes one page and no LZW.
func writeTiff(w io.Writer, pages []image.Image, compression presets.TiffCompression, dpi int) error {
	type page struct {
		data                []byte
		width, height, spp  int
		alpha               bool
	}
	var built []page
	for _, img := range pages {
		b := img.Bounds()
		p := page{width: b.Dx(), height: b.Dy()}
		var raw []byte
		if g, ok := img.(*image.Gray); ok {
			p.spp = 1
			raw = make([]byte, 0, p.width*p.height)
			for y := 0; y < p.height; y++ {
				raw = append(raw, g.Pix[y*g.Stride:y*g.Stride+p.width]...)
			}
		} else {
			n := toNRGBA(img)
			p.alpha = hasAlpha(n)
			p.spp = 3
			if p.alpha {
				p.spp = 4
			}
			raw = make([]byte, 0, p.width*p.height*p.spp)
			for i := 0; i < len(n.Pix); i += 4 {
				raw = append(raw, n.Pix[i:i+p.spp]...)
			}
		}
		if compression != presets.TiffNone {
			predict(raw, p.width*p.spp, p.spp)
		}
		switch compression {
		case presets.TiffNone:
			p.data = raw
		case presets.TiffZip:
			var buf bytes.Buffer
			z, _ := zlib.NewWriterLevel(&buf, zlib.BestCompression)
			z.Write(raw)
			z.Close()
			p.data = buf.Bytes()
		default:
			p.data = lzwEncode(raw)
		}
		built = append(built, p)
	}
	if dpi <= 0 {
		dpi = 72
	}

	le := binary.LittleEndian
	var out bytes.Buffer
	out.Write([]byte{'I', 'I', 42, 0, 0, 0, 0, 0})
	firstIFD := 4
	for _, p := range built {
		// Strip data, then the IFD with its out-of-line values.
		dataOffset := out.Len()
		out.Write(p.data)
		if out.Len()%2 == 1 {
			out.WriteByte(0)
		}
		type entry struct {
			tag, typ uint16
			count    uint32
			value    []byte // inline when ≤ 4 bytes
		}
		short := func(v ...uint16) []byte {
			b := make([]byte, 2*len(v))
			for i, x := range v {
				le.PutUint16(b[2*i:], x)
			}
			return b
		}
		long := func(v uint32) []byte { b := make([]byte, 4); le.PutUint32(b, v); return b }
		bits := make([]uint16, p.spp)
		for i := range bits {
			bits[i] = 8
		}
		photometric := uint16(2)
		if p.spp == 1 {
			photometric = 1
		}
		comp := uint16(1)
		switch compression {
		case presets.TiffZip:
			comp = 8
		case presets.TiffLzw, "":
			comp = 5
		}
		rational := func(v uint32) []byte { b := make([]byte, 8); le.PutUint32(b, v); le.PutUint32(b[4:], 1); return b }
		entries := []entry{
			{256, 4, 1, long(uint32(p.width))},
			{257, 4, 1, long(uint32(p.height))},
			{258, 3, uint32(p.spp), short(bits...)},
			{259, 3, 1, short(comp)},
			{262, 3, 1, short(photometric)},
			{273, 4, 1, long(uint32(dataOffset))},
			{277, 3, 1, short(uint16(p.spp))},
			{278, 4, 1, long(uint32(p.height))},
			{279, 4, 1, long(uint32(len(p.data)))},
			{282, 5, 1, rational(uint32(dpi))},
			{283, 5, 1, rational(uint32(dpi))},
			{284, 3, 1, short(1)},
			{296, 3, 1, short(2)},
		}
		if compression != presets.TiffNone {
			entries = append(entries, entry{317, 3, 1, short(2)})
		}
		if p.alpha {
			entries = append(entries, entry{338, 3, 1, short(2)}) // unassociated alpha
		}
		ifd := out.Len()
		le.PutUint32(out.Bytes()[firstIFD:], uint32(ifd))
		extra := ifd + 2 + 12*len(entries) + 4
		var tail bytes.Buffer
		var dir bytes.Buffer
		binary.Write(&dir, le, uint16(len(entries)))
		for _, e := range entries {
			binary.Write(&dir, le, e.tag)
			binary.Write(&dir, le, e.typ)
			binary.Write(&dir, le, e.count)
			if len(e.value) <= 4 {
				v := make([]byte, 4)
				copy(v, e.value)
				dir.Write(v)
			} else {
				binary.Write(&dir, le, uint32(extra+tail.Len()))
				tail.Write(e.value)
			}
		}
		firstIFD = out.Len() + dir.Len()
		dir.Write([]byte{0, 0, 0, 0})
		out.Write(dir.Bytes())
		out.Write(tail.Bytes())
	}
	_, err := w.Write(out.Bytes())
	return err
}

// predict applies TIFF's horizontal differencing (predictor 2) in place.
func predict(raw []byte, stride, spp int) {
	for row := 0; row+stride <= len(raw); row += stride {
		for x := stride - 1; x >= spp; x-- {
			raw[row+x] -= raw[row+x-spp]
		}
	}
}

// lzwEncode is TIFF's LZW: MSB first, codes growing one code early.
func lzwEncode(data []byte) []byte {
	const clearCode, eoi = 256, 257
	var out bytes.Buffer
	var acc uint32
	var nbits uint
	width := uint(9)
	emit := func(code int) {
		acc = acc<<width | uint32(code)
		nbits += width
		for nbits >= 8 {
			out.WriteByte(byte(acc >> (nbits - 8)))
			nbits -= 8
		}
		acc &= (1 << nbits) - 1
	}
	table := make(map[uint32]int, 4096)
	next := 258
	emit(clearCode)
	if len(data) == 0 {
		emit(eoi)
		if nbits > 0 {
			out.WriteByte(byte(acc << (8 - nbits)))
		}
		return out.Bytes()
	}
	prefix := int(data[0])
	for _, c := range data[1:] {
		key := uint32(prefix)<<8 | uint32(c)
		if code, ok := table[key]; ok {
			prefix = code
			continue
		}
		emit(prefix)
		table[key] = next
		next++
		switch next {
		case 512:
			width = 10
		case 1024:
			width = 11
		case 2048:
			width = 12
		case 4094:
			emit(clearCode)
			clear(table)
			next = 258
			width = 9
		}
		prefix = int(c)
	}
	emit(prefix)
	// The decoder grows its width as it adds the entry for this last code.
	if next+1 == 512 || next+1 == 1024 || next+1 == 2048 {
		width++
	}
	emit(eoi)
	if nbits > 0 {
		out.WriteByte(byte(acc << (8 - nbits)))
	}
	return out.Bytes()
}
