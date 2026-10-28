package images

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"golang.org/x/image/tiff"
)

// ---- reading ----

type tiffIFD struct {
	offset int
	tags   map[uint16][]uint64 // numeric values
	raw    map[uint16][]byte   // value bytes of UNDEFINED/BYTE/ASCII tags
}

// tiffDirectories walks the IFD chain of a TIFF file.
func tiffDirectories(data []byte) ([]tiffIFD, binary.ByteOrder, error) {
	if len(data) < 8 {
		return nil, nil, errors.New("the file is not a TIFF image")
	}
	var order binary.ByteOrder
	switch string(data[:4]) {
	case "II*\x00":
		order = binary.LittleEndian
	case "MM\x00*":
		order = binary.BigEndian
	default:
		if string(data[:4]) == "II+\x00" || string(data[:4]) == "MM\x00+" {
			return nil, nil, errors.New("BigTIFF files are not supported")
		}
		return nil, nil, errors.New("the file is not a TIFF image")
	}
	sizes := map[uint16]int{1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 1, 8: 2, 9: 4, 10: 8, 11: 4, 12: 8, 16: 8}
	var dirs []tiffIFD
	seen := map[int]bool{}
	for off := int(order.Uint32(data[4:])); off != 0 && !seen[off] && len(dirs) < 10000; {
		seen[off] = true
		if off+2 > len(data) {
			break
		}
		n := int(order.Uint16(data[off:]))
		if off+2+n*12+4 > len(data) {
			break
		}
		d := tiffIFD{offset: off, tags: map[uint16][]uint64{}, raw: map[uint16][]byte{}}
		for i := 0; i < n; i++ {
			e := data[off+2+i*12:]
			tag, typ, count := order.Uint16(e), order.Uint16(e[2:]), order.Uint32(e[4:])
			size, ok := sizes[typ]
			if !ok || uint64(count)*uint64(size) > uint64(len(data)) {
				continue
			}
			total := int(count) * size
			val := e[8:12]
			if total > 4 {
				p := int(order.Uint32(e[8:]))
				if p+total > len(data) {
					continue
				}
				val = data[p : p+total]
			}
			switch typ {
			case 1, 2, 6, 7:
				d.raw[tag] = val[:min(total, len(val))]
				for k := 0; k < int(count) && k < 16; k++ {
					d.tags[tag] = append(d.tags[tag], uint64(val[k]))
				}
			case 3, 8:
				for k := 0; k < int(count) && k < 1<<20; k++ {
					d.tags[tag] = append(d.tags[tag], uint64(order.Uint16(val[k*2:])))
				}
			case 4, 9:
				for k := 0; k < int(count) && k < 1<<20; k++ {
					d.tags[tag] = append(d.tags[tag], uint64(order.Uint32(val[k*4:])))
				}
			case 5, 10:
				for k := 0; k < int(count) && k < 16; k++ {
					num, den := order.Uint32(val[k*8:]), order.Uint32(val[k*8+4:])
					// stored as value × 1000 to keep fractions
					if den != 0 {
						d.tags[tag] = append(d.tags[tag], uint64(float64(num)/float64(den)*1000))
					}
				}
			}
		}
		dirs = append(dirs, d)
		off = int(order.Uint32(data[off+2+n*12:]))
	}
	if len(dirs) == 0 {
		return nil, nil, errors.New("the TIFF file is damaged (no image directory)")
	}
	return dirs, order, nil
}

func (d *tiffIFD) first(tag uint16, def uint64) uint64 {
	if v := d.tags[tag]; len(v) > 0 {
		return v[0]
	}
	return def
}

// decodeTIFF reads every page of a TIFF (reduced-resolution previews skipped).
// Each page is decoded by golang.org/x/image/tiff from a copy of the file whose
// header points at that page's directory.
func decodeTIFF(data []byte) (*Picture, error) {
	dirs, order, err := tiffDirectories(data)
	if err != nil {
		return nil, err
	}
	p := &Picture{}
	patched := bytes.Clone(data)
	var firstErr error
	for i, d := range dirs {
		if i > 0 && d.first(254, 0)&1 != 0 {
			continue // a reduced-resolution version of another page
		}
		order.PutUint32(patched[4:], uint32(d.offset))
		img, err := tiff.Decode(bytes.NewReader(patched))
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		f := NewFrame(img)
		photometric := d.first(262, 2)
		f.Gray = photometric <= 1
		if extra := d.tags[338]; len(extra) > 0 {
			f.Alpha = true
		}
		if icc := d.raw[34675]; len(icc) > 0 {
			f.Meta.ICC = bytes.Clone(icc)
		}
		if xmp := d.raw[700]; len(xmp) > 0 {
			f.Meta.XMP = bytes.Clone(xmp)
		}
		if o := int(d.first(274, 0)); o >= 1 && o <= 8 {
			f.Meta.Orientation = o
		}
		if x, y := d.first(282, 0), d.first(283, 0); x > 0 && y > 0 {
			unit := d.first(296, 2)
			fx, fy := float64(x)/1000, float64(y)/1000
			if unit == 3 {
				fx, fy = fx*2.54, fy*2.54
			}
			if unit != 1 {
				f.Meta.DpiX, f.Meta.DpiY = fx, fy
			}
		}
		p.Frames = append(p.Frames, f)
	}
	if len(p.Frames) == 0 {
		if firstErr == nil {
			firstErr = errors.New("no page could be read")
		}
		return nil, fmt.Errorf("TIFF: %w", firstErr)
	}
	return p, nil
}

// ---- writing ----

// tiffKind is how a sample is stored.
type tiffKind int

const (
	tiffU8 tiffKind = iota
	tiffU16
	tiffF32
)

// tiffPage is one page to write: rows of samples produced on demand.
type tiffPage struct {
	w, h, samples int
	kind          tiffKind
	alpha         bool // the last sample is unassociated alpha
	row           func(y int, dst []byte) // little-endian samples of row y
	compression   string                  // "None", "Lzw", "Zip"
	dpiX, dpiY    float64
	icc, xmp      []byte
	subfile2      bool
}

func framePage(f *Frame) *tiffPage {
	p := &tiffPage{w: f.Width(), h: f.Height(), kind: tiffU8, compression: f.enc.tiff,
		dpiX: f.Meta.DpiX, dpiY: f.Meta.DpiY, icc: f.Meta.ICC, xmp: f.Meta.XMP, subfile2: f.enc.subfile2}
	if p.compression == "" {
		p.compression = "Lzw"
	}
	alpha := f.Alpha
	gray := f.Gray
	switch {
	case gray && alpha:
		p.samples, p.alpha = 2, true
	case gray:
		p.samples = 1
	case alpha:
		p.samples, p.alpha = 4, true
	default:
		p.samples = 3
	}
	img := f.Img
	p.row = func(y int, dst []byte) {
		src := img.Pix[y*img.Stride:]
		for x := 0; x < p.w; x++ {
			s := src[x*4 : x*4+4]
			switch p.samples {
			case 1:
				dst[x] = s[0]
			case 2:
				dst[x*2], dst[x*2+1] = s[0], s[3]
			case 3:
				copy(dst[x*3:x*3+3], s[:3])
			case 4:
				copy(dst[x*4:x*4+4], s)
			}
		}
	}
	return p
}

// encodeTIFF writes the frames as the pages of one TIFF.
func encodeTIFF(frames []*Frame) ([]byte, error) {
	pages := make([]*tiffPage, len(frames))
	for i, f := range frames {
		pages[i] = framePage(f)
	}
	return writeTIFF(pages)
}

const tiffStripBytes = 256 * 1024

// writeTIFF writes pages as a baseline little-endian TIFF in strips,
// uncompressed or with LZW or Deflate (integer samples with the horizontal
// predictor), with resolution, ICC profile and XMP.
func writeTIFF(pages []*tiffPage) ([]byte, error) {
	var out bytes.Buffer
	out.WriteString("II*\x00\x00\x00\x00\x00")
	le := binary.LittleEndian
	nextPtr := 4 // where the offset of the next IFD goes
	lzw := &lzwEncoder{}
	for pi, p := range pages {
		bps := map[tiffKind]int{tiffU8: 1, tiffU16: 2, tiffF32: 4}[p.kind]
		rowBytes := p.w * p.samples * bps
		rowsPerStrip := max(1, min(p.h, tiffStripBytes/max(1, rowBytes)))
		strips := (p.h + rowsPerStrip - 1) / rowsPerStrip
		predictor := p.compression != "None" && p.kind != tiffF32
		offsets := make([]uint32, strips)
		counts := make([]uint32, strips)
		strip := make([]byte, rowsPerStrip*rowBytes)
		for s := 0; s < strips; s++ {
			first := s * rowsPerStrip
			rows := min(rowsPerStrip, p.h-first)
			data := strip[:rows*rowBytes]
			for r := 0; r < rows; r++ {
				row := data[r*rowBytes : (r+1)*rowBytes]
				p.row(first+r, row)
				if predictor {
					applyPredictor(row, p.samples, p.kind)
				}
			}
			var enc []byte
			switch p.compression {
			case "Lzw":
				enc = lzw.encode(data)
			case "Zip":
				enc = deflate(data, zlib.DefaultCompression)
			default:
				enc = bytes.Clone(data)
			}
			if out.Len()+len(enc) > math.MaxUint32 {
				return nil, errors.New("the picture is too large for a TIFF file (over 4 GB)")
			}
			offsets[s] = uint32(out.Len())
			counts[s] = uint32(len(enc))
			out.Write(enc)
			if out.Len()&1 == 1 {
				out.WriteByte(0)
			}
		}
		type entry struct {
			tag, typ uint16
			count    uint32
			value    []byte
		}
		u16 := func(v ...uint16) []byte {
			b := make([]byte, 2*len(v))
			for i, x := range v {
				le.PutUint16(b[i*2:], x)
			}
			return b
		}
		u32 := func(v ...uint32) []byte {
			b := make([]byte, 4*len(v))
			for i, x := range v {
				le.PutUint32(b[i*4:], x)
			}
			return b
		}
		repeat := func(v uint16, n int) []uint16 {
			s := make([]uint16, n)
			for i := range s {
				s[i] = v
			}
			return s
		}
		comp := uint16(1)
		switch p.compression {
		case "Lzw":
			comp = 5
		case "Zip":
			comp = 8
		}
		photometric := uint16(2)
		if p.samples <= 2 {
			photometric = 1
		}
		format := uint16(1)
		if p.kind == tiffF32 {
			format = 3
		}
		dpiX, dpiY := p.dpiX, p.dpiY
		if dpiX <= 0 || dpiY <= 0 {
			dpiX, dpiY = 72, 72
		}
		rational := func(v float64) []byte {
			den := uint32(1)
			for den < 10000 && math.Abs(v*float64(den)-math.Round(v*float64(den))) > 1e-6 {
				den *= 10
			}
			return u32(uint32(math.Round(v*float64(den))), den)
		}
		entries := []entry{}
		if p.subfile2 {
			entries = append(entries, entry{254, 4, 1, u32(2)})
		}
		entries = append(entries,
			entry{256, 4, 1, u32(uint32(p.w))},
			entry{257, 4, 1, u32(uint32(p.h))},
			entry{258, 3, uint32(p.samples), u16(repeat(uint16(bps*8), p.samples)...)},
			entry{259, 3, 1, u16(comp)},
			entry{262, 3, 1, u16(photometric)},
			entry{273, 4, uint32(strips), u32(offsets...)},
			entry{277, 3, 1, u16(uint16(p.samples))},
			entry{278, 4, 1, u32(uint32(rowsPerStrip))},
			entry{279, 4, uint32(strips), u32(counts...)},
			entry{282, 5, 1, rational(dpiX)},
			entry{283, 5, 1, rational(dpiY)},
			entry{284, 3, 1, u16(1)},
			entry{296, 3, 1, u16(2)},
		)
		if len(pages) > 1 {
			entries = append(entries, entry{297, 3, 2, u16(uint16(pi), uint16(len(pages)))})
		}
		if predictor {
			entries = append(entries, entry{317, 3, 1, u16(2)})
		}
		if len(p.xmp) > 0 {
			entries = append(entries, entry{700, 1, uint32(len(p.xmp)), p.xmp})
		}
		if p.alpha {
			entries = append(entries, entry{338, 3, 1, u16(2)}) // unassociated alpha
		}
		entries = append(entries, entry{339, 3, uint32(p.samples), u16(repeat(format, p.samples)...)})
		if len(p.icc) > 0 {
			entries = append(entries, entry{34675, 7, uint32(len(p.icc)), p.icc})
		}
		ifd := out.Len()
		le.PutUint32(out.Bytes()[nextPtr:], uint32(ifd))
		extra := ifd + 2 + len(entries)*12 + 4
		var dir, values bytes.Buffer
		dir.Write(u16(uint16(len(entries))))
		for _, e := range entries {
			dir.Write(u16(e.tag, e.typ))
			dir.Write(u32(e.count))
			if len(e.value) <= 4 {
				v := make([]byte, 4)
				copy(v, e.value)
				dir.Write(v)
				continue
			}
			dir.Write(u32(uint32(extra + values.Len())))
			values.Write(e.value)
			if values.Len()&1 == 1 {
				values.WriteByte(0)
			}
		}
		nextPtr = ifd + dir.Len()
		dir.Write(u32(0))
		out.Write(dir.Bytes())
		out.Write(values.Bytes())
		if out.Len() > math.MaxUint32 {
			return nil, errors.New("the picture is too large for a TIFF file (over 4 GB)")
		}
	}
	return out.Bytes(), nil
}

// applyPredictor replaces each sample with its difference to the one before (TIFF predictor 2).
func applyPredictor(row []byte, samples int, kind tiffKind) {
	switch kind {
	case tiffU8:
		for i := len(row) - 1; i >= samples; i-- {
			row[i] -= row[i-samples]
		}
	case tiffU16:
		n := len(row) / 2
		for i := n - 1; i >= samples; i-- {
			v := binary.LittleEndian.Uint16(row[i*2:]) - binary.LittleEndian.Uint16(row[(i-samples)*2:])
			binary.LittleEndian.PutUint16(row[i*2:], v)
		}
	}
}

// lzwEncode is TIFF's LZW (TIFF 6.0 section 13): codes of 9 to 12 bits, most
// significant bit first, starting with a clear code, one bit wider as soon as
// the next code would not fit ("early change"), a clear code when the table is full.
type lzwEncoder struct {
	// (prefix code << 8 | next byte) → code, valid where its stamp is the current generation
	codes      []int16
	stamps     []int32
	generation int32
}

func lzwEncode(data []byte) []byte { return (&lzwEncoder{}).encode(data) }

func (z *lzwEncoder) encode(data []byte) []byte {
	if z.codes == nil {
		z.codes, z.stamps = make([]int16, 1<<20), make([]int32, 1<<20)
	}
	codes, stamps := z.codes, z.stamps
	const clearCode, endCode, firstCode, full = 256, 257, 258, 4094
	out := bytes.NewBuffer(make([]byte, 0, len(data)/2+16))
	var buffer uint64
	buffered := 0
	put := func(code, width int) {
		buffer = buffer<<width | uint64(code)
		buffered += width
		for buffered >= 8 {
			buffered -= 8
			out.WriteByte(byte(buffer >> buffered))
		}
	}
	z.generation++
	width, next := 9, firstCode
	put(clearCode, width)
	if len(data) > 0 {
		prefix := int(data[0])
		for _, b := range data[1:] {
			key := prefix<<8 | int(b)
			if stamps[key] == z.generation {
				prefix = int(codes[key])
				continue
			}
			put(prefix, width)
			stamps[key], codes[key] = z.generation, int16(next)
			next++
			prefix = int(b)
			if next == full {
				put(clearCode, width)
				z.generation++
				next, width = firstCode, 9
			} else if next > (1<<width)-1 {
				width++
			}
		}
		put(prefix, width)
		// The reader adds a table entry for the last code too, and may widen before the end code.
		next++
		if next == full {
			put(clearCode, width)
			width = 9
		} else if next > (1<<width)-1 {
			width++
		}
	}
	put(endCode, width)
	if buffered > 0 {
		out.WriteByte(byte(buffer << (8 - buffered)))
	}
	return out.Bytes()
}

