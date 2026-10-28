package images

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"math"
	"strconv"
	"strings"

	jpeg2000 "github.com/mrjoshuak/go-jpeg2000"
)

// DICOM images (medical scans), read by Filee itself from the DICOM standard
// (PS3.5 data encoding, PS3.3 C.7.6.3 image pixel module, C.11.2 VOI LUT).
// Every frame becomes an 8-bit picture: stored values → rescale slope /
// intercept → the file's first window (centre / width), or the full range of
// values when there is none. Uncompressed, deflated, RLE, baseline JPEG and
// JPEG 2000 pixel data are read; JPEG lossless and JPEG-LS are not.

const (
	dcmImplicitLE = "1.2.840.10008.1.2"
	dcmExplicitLE = "1.2.840.10008.1.2.1"
	dcmDeflated   = "1.2.840.10008.1.2.1.99"
	dcmExplicitBE = "1.2.840.10008.1.2.2"
	dcmRLE        = "1.2.840.10008.1.2.5"
	dcmPixelData  = 0x7FE00010
	dcmUndefined  = 0xFFFFFFFF
)

var dcmLongVRs = map[string]bool{"OB": true, "OD": true, "OF": true, "OL": true, "OV": true, "OW": true, "SQ": true,
	"SV": true, "UC": true, "UN": true, "UR": true, "UT": true, "UV": true}

var dcmWanted = map[uint32]bool{0x00280002: true, 0x00280004: true, 0x00280006: true, 0x00280008: true, 0x00280010: true,
	0x00280011: true, 0x00280100: true, 0x00280101: true, 0x00280102: true, 0x00280103: true, 0x00281050: true,
	0x00281051: true, 0x00281052: true, 0x00281053: true, dcmPixelData: true}

var errDicomDamaged = errors.New("the DICOM file is damaged")

const dcmUnreadable = "This DICOM image is compressed in a way Filee can't read (JPEG lossless or JPEG-LS). Export it uncompressed from the viewing software and try again."

type dcmElement struct {
	tag       uint32
	value     []byte
	undefined bool
}

type dcmReader struct {
	b           []byte
	p           int
	explicit    bool
	order       binary.ByteOrder
}

func (r *dcmReader) u16(at int) (uint16, error) {
	if at+2 > len(r.b) {
		return 0, errDicomDamaged
	}
	return r.order.Uint16(r.b[at:]), nil
}

func (r *dcmReader) u32(at int) (uint32, error) {
	if at+4 > len(r.b) {
		return 0, errDicomDamaged
	}
	return r.order.Uint32(r.b[at:]), nil
}

func (r *dcmReader) next() (dcmElement, error) {
	g, err := r.u16(r.p)
	if err != nil {
		return dcmElement{}, err
	}
	e, _ := r.u16(r.p + 2)
	tag := uint32(g)<<16 | uint32(e)
	var length uint32
	if r.explicit && g != 0xFFFE {
		if r.p+8 > len(r.b) {
			return dcmElement{}, errDicomDamaged
		}
		vr := string(r.b[r.p+4 : r.p+6])
		if dcmLongVRs[vr] {
			if length, err = r.u32(r.p + 8); err != nil {
				return dcmElement{}, err
			}
			r.p += 12
		} else {
			l, _ := r.u16(r.p + 6)
			length = uint32(l)
			r.p += 8
		}
	} else {
		if length, err = r.u32(r.p + 4); err != nil {
			return dcmElement{}, err
		}
		r.p += 8
	}
	if length == dcmUndefined {
		return dcmElement{tag: tag, undefined: true}, nil
	}
	if int(length) > len(r.b)-r.p {
		return dcmElement{}, errDicomDamaged
	}
	v := r.b[r.p : r.p+int(length)]
	r.p += int(length)
	return dcmElement{tag: tag, value: v}, nil
}

func (r *dcmReader) header() (uint32, uint32, error) {
	g, err := r.u16(r.p)
	if err != nil {
		return 0, 0, err
	}
	e, _ := r.u16(r.p + 2)
	l, err := r.u32(r.p + 4)
	if err != nil {
		return 0, 0, err
	}
	r.p += 8
	return uint32(g)<<16 | uint32(e), l, nil
}

// skipSequence skips a sequence of undefined length.
func (r *dcmReader) skipSequence(depth int) error {
	if depth > 64 {
		return errDicomDamaged
	}
	for {
		tag, l, err := r.header()
		if err != nil {
			return err
		}
		if tag == 0xFFFEE0DD {
			return nil
		}
		if tag != 0xFFFEE000 {
			return errDicomDamaged
		}
		if l != dcmUndefined {
			if int(l) > len(r.b)-r.p {
				return errDicomDamaged
			}
			r.p += int(l)
			continue
		}
		for {
			g, _ := r.u16(r.p)
			e, _ := r.u16(r.p + 2)
			if g == 0xFFFE && e == 0xE00D {
				r.header()
				break
			}
			el, err := r.next()
			if err != nil {
				return err
			}
			if el.undefined {
				if err := r.skipSequence(depth + 1); err != nil {
					return err
				}
			}
		}
	}
}

func dcmText(v []byte) string { return strings.TrimRight(string(v), "\x00 ") }

func decodeDICOM(file []byte) (*Picture, error) {
	pos := 0
	if len(file) >= 132 && string(file[128:132]) == "DICM" {
		pos = 132
	}
	syntax := ""
	meta := &dcmReader{b: file, p: pos, explicit: true, order: binary.LittleEndian}
	for meta.p+4 <= len(file) {
		g, _ := meta.u16(meta.p)
		if g != 0x0002 {
			break
		}
		el, err := meta.next()
		if err != nil {
			return nil, err
		}
		if el.tag == 0x00020010 {
			syntax = dcmText(el.value)
		}
	}
	data, start := file, meta.p
	explicit, order := true, binary.ByteOrder(binary.LittleEndian)
	encapsulated := false
	switch syntax {
	case "":
		explicit = start+6 <= len(file) && file[start+4] >= 'A' && file[start+4] <= 'Z' && file[start+5] >= 'A' && file[start+5] <= 'Z'
	case dcmImplicitLE:
		explicit = false
	case dcmExplicitLE:
	case dcmExplicitBE:
		order = binary.BigEndian
	case dcmDeflated:
		out, err := io.ReadAll(flate.NewReader(bytes.NewReader(file[start:])))
		if err != nil {
			return nil, errDicomDamaged
		}
		data, start = out, 0
	default:
		encapsulated = true
	}
	values := map[uint32][]byte{}
	var fragments [][]byte
	r := &dcmReader{b: data, p: start, explicit: explicit, order: order}
	for r.p+8 <= len(data) {
		el, err := r.next()
		if err != nil {
			return nil, err
		}
		if el.undefined {
			if el.tag == dcmPixelData {
				// encapsulated frames: items until the sequence delimiter
				for {
					tag, l, err := r.header()
					if err != nil || tag == 0xFFFEE0DD {
						break
					}
					if tag != 0xFFFEE000 || int(l) > len(data)-r.p {
						return nil, errDicomDamaged
					}
					fragments = append(fragments, data[r.p:r.p+int(l)])
					r.p += int(l)
				}
				values[dcmPixelData] = nil
				break
			}
			if err := r.skipSequence(0); err != nil {
				return nil, err
			}
			continue
		}
		if dcmWanted[el.tag] {
			values[el.tag] = el.value
		}
		if el.tag == dcmPixelData {
			break
		}
	}
	if _, ok := values[dcmPixelData]; !ok {
		return nil, errors.New("the DICOM file has no image")
	}
	return dcmDecode(values, fragments, syntax, encapsulated, order)
}

func dcmDecode(values map[uint32][]byte, fragments [][]byte, syntax string, encapsulated bool, order binary.ByteOrder) (*Picture, error) {
	number := func(tag uint32, def int) int {
		if v, ok := values[tag]; ok && len(v) >= 2 {
			return int(order.Uint16(v))
		}
		return def
	}
	decimal := func(tag uint32) (float64, bool) {
		v, ok := values[tag]
		if !ok {
			return 0, false
		}
		s := strings.TrimSpace(strings.Split(dcmText(v), `\`)[0])
		d, err := strconv.ParseFloat(s, 64)
		return d, err == nil
	}
	samples := number(0x00280002, 1)
	photometric := "MONOCHROME2"
	if v, ok := values[0x00280004]; ok {
		photometric = strings.TrimSpace(dcmText(v))
	}
	planar := number(0x00280006, 0)
	framesF, ok := decimal(0x00280008)
	frames := 1
	if ok {
		frames = int(framesF)
	}
	rows, cols := number(0x00280010, 0), number(0x00280011, 0)
	allocated := number(0x00280100, 8)
	stored := number(0x00280101, allocated)
	highBit := number(0x00280102, stored-1)
	signed := number(0x00280103, 0) == 1
	slope, ok := decimal(0x00281053)
	if !ok {
		slope = 1
	}
	intercept, _ := decimal(0x00281052)
	center, hasCenter := decimal(0x00281050)
	width, hasWidth := decimal(0x00281051)

	gray := samples == 1 && (photometric == "MONOCHROME1" || photometric == "MONOCHROME2")
	colour := samples == 3 && (photometric == "RGB" || photometric == "YBR_FULL" || photometric == "YBR_FULL_422")
	if !gray && !colour {
		return nil, errors.New("this DICOM image's colour layout (" + photometric + ") is not supported")
	}
	if (allocated != 8 && allocated != 16) || stored < 1 || stored > 16 || stored > allocated || highBit >= allocated {
		return nil, errors.New("this DICOM image's sample size is not supported")
	}
	if rows == 0 || cols == 0 || frames < 1 {
		return nil, errors.New("the DICOM image has no size")
	}
	n := rows * cols
	// raw[frame][sample]: values as stored (before the rescale)
	var raw [][]int
	shift := highBit - stored + 1
	mask := 1<<stored - 1
	norm := func(v int) int {
		v = (v >> shift) & mask
		if signed && v&(1<<(stored-1)) != 0 {
			v -= 1 << stored
		}
		return v
	}
	if !encapsulated {
		pixels := values[dcmPixelData]
		frameBytes := n * samples * allocated / 8
		if len(pixels) < frameBytes*frames {
			return nil, errors.New("the DICOM file is damaged (its image data is incomplete)")
		}
		for f := 0; f < frames; f++ {
			s := make([]int, n*samples)
			fb := pixels[f*frameBytes:]
			for i := range s {
				if allocated == 8 {
					s[i] = norm(int(fb[i]))
				} else {
					s[i] = norm(int(order.Uint16(fb[i*2:])))
				}
			}
			raw = append(raw, s)
		}
	} else {
		frameData := dcmFrames(fragments, frames)
		for _, fd := range frameData {
			s, err := dcmDecodeFrame(fd, syntax, rows, cols, samples, allocated, norm)
			if err != nil {
				return nil, err
			}
			raw = append(raw, s)
		}
		if photometric == "YBR_FULL_422" && strings.HasPrefix(syntax, "1.2.840.10008.1.2.4.5") {
			photometric = "RGB" // the JPEG decoder converts to RGB
		}
		if strings.HasPrefix(syntax, "1.2.840.10008.1.2.4.9") {
			photometric = "RGB"
		}
		planar = 0
	}
	var toGray func(float64) float64
	switch {
	case gray && hasCenter && hasWidth && width >= 1:
		c, w := center, width
		toGray = func(x float64) float64 {
			switch {
			case x <= c-0.5-(w-1)/2:
				return 0
			case x > c-0.5+(w-1)/2:
				return 255
			}
			return ((x-(c-0.5))/(w-1) + 0.5) * 255
		}
	case gray:
		lo, hi := math.MaxFloat64, -math.MaxFloat64
		for _, fr := range raw {
			for _, v := range fr {
				x := float64(v)*slope + intercept
				lo, hi = math.Min(lo, x), math.Max(hi, x)
			}
		}
		toGray = func(x float64) float64 {
			if hi-lo <= 0 {
				return 0
			}
			return (x - lo) / (hi - lo) * 255
		}
	}
	p := &Picture{}
	for _, fr := range raw {
		img := image.NewNRGBA(image.Rect(0, 0, cols, rows))
		for i := 0; i < n; i++ {
			o := i * 4
			img.Pix[o+3] = 255
			if gray {
				level := math.Max(0, math.Min(255, toGray(float64(fr[i])*slope+intercept)))
				if photometric == "MONOCHROME1" {
					level = 255 - level
				}
				v := uint8(math.Round(level))
				img.Pix[o], img.Pix[o+1], img.Pix[o+2] = v, v, v
				continue
			}
			var a, b, d int
			if planar == 1 {
				a, b, d = fr[i], fr[n+i], fr[2*n+i]
			} else {
				a, b, d = fr[i*3], fr[i*3+1], fr[i*3+2]
			}
			down := max(0, stored-8)
			a, b, d = a>>down, b>>down, d>>down
			if photometric == "YBR_FULL" || photometric == "YBR_FULL_422" {
				y, cb, cr := float64(a), float64(b), float64(d)
				a = int(math.Round(y + 1.402*(cr-128)))
				b = int(math.Round(y - 0.344136*(cb-128) - 0.714136*(cr-128)))
				d = int(math.Round(y + 1.772*(cb-128)))
			}
			img.Pix[o], img.Pix[o+1], img.Pix[o+2] = uint8(max(0, min(255, a))), uint8(max(0, min(255, b))), uint8(max(0, min(255, d)))
		}
		p.Frames = append(p.Frames, &Frame{Img: img, Gray: gray})
	}
	return p, nil
}

// dcmFrames groups the encapsulated fragments into frames (the first item is
// the basic offset table): one fragment per frame, or all of them for one frame.
func dcmFrames(fragments [][]byte, frames int) [][]byte {
	if len(fragments) == 0 {
		return nil
	}
	fragments = fragments[1:]
	if frames <= 1 {
		return [][]byte{bytes.Join(fragments, nil)}
	}
	if len(fragments) == frames {
		return fragments
	}
	// several fragments per frame: a JPEG / J2K frame starts with its own marker
	var out [][]byte
	for _, f := range fragments {
		if len(out) == 0 || bytes.HasPrefix(f, []byte{0xFF, 0xD8}) || bytes.HasPrefix(f, []byte{0xFF, 0x4F, 0xFF, 0x51}) {
			out = append(out, bytes.Clone(f))
		} else {
			out[len(out)-1] = append(out[len(out)-1], f...)
		}
	}
	return out
}

func dcmDecodeFrame(data []byte, syntax string, rows, cols, samples, allocated int, norm func(int) int) ([]int, error) {
	n := rows * cols
	switch {
	case syntax == dcmRLE:
		if len(data) < 64 {
			return nil, errDicomDamaged
		}
		segments := int(binary.LittleEndian.Uint32(data))
		bytesPer := allocated / 8
		if segments != samples*bytesPer || segments > 15 {
			return nil, errDicomDamaged
		}
		out := make([]int, n*samples)
		for s := 0; s < segments; s++ {
			start := int(binary.LittleEndian.Uint32(data[4+s*4:]))
			end := len(data)
			if s+1 < segments {
				end = int(binary.LittleEndian.Uint32(data[8+s*4:]))
			}
			if start < 64 || start > end || end > len(data) {
				return nil, errDicomDamaged
			}
			plane := unpackBits(data[start:end], n)
			sample, byteIdx := s/bytesPer, s%bytesPer
			shiftBits := 8 * (bytesPer - 1 - byteIdx)
			for i := 0; i < n; i++ {
				// planes of R, G, B (colour by plane)
				out[sample*n+i] |= int(plane[i]) << shiftBits
			}
		}
		for i := range out {
			out[i] = norm(out[i])
		}
		if samples == 3 {
			// back to interleaved
			il := make([]int, len(out))
			for i := 0; i < n; i++ {
				il[i*3], il[i*3+1], il[i*3+2] = out[i], out[n+i], out[2*n+i]
			}
			out = il
		}
		return out, nil
	case syntax == "1.2.840.10008.1.2.4.50" || syntax == "1.2.840.10008.1.2.4.51":
		img, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, errors.New(dcmUnreadable + " (" + err.Error() + ")")
		}
		return dcmSamples(img, samples), nil
	case syntax == "1.2.840.10008.1.2.4.90" || syntax == "1.2.840.10008.1.2.4.91":
		img, err := jpeg2000.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, errors.New("the DICOM image's JPEG 2000 data can't be read: " + err.Error())
		}
		return dcmSamples(img, samples), nil
	}
	return nil, errors.New(dcmUnreadable)
}

// dcmSamples are the decoded picture's samples, interleaved (16-bit grey kept whole).
func dcmSamples(img image.Image, samples int) []int {
	b := img.Bounds()
	out := make([]int, 0, b.Dx()*b.Dy()*samples)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if samples == 1 {
				switch g := img.(type) {
				case *image.Gray16:
					out = append(out, int(g.Gray16At(x, y).Y))
				case *image.Gray:
					out = append(out, int(g.GrayAt(x, y).Y))
				default:
					r, _, _, _ := img.At(x, y).RGBA()
					out = append(out, int(r>>8))
				}
				continue
			}
			r, g, bb, _ := img.At(x, y).RGBA()
			out = append(out, int(r>>8), int(g>>8), int(bb>>8))
		}
	}
	return out
}
