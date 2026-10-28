package media

// The few Exif pieces the media engine needs without an image library: the
// Apple maker note of a photo (JPEG or HEIC), a JPEG's orientation and ICC
// profile, and writing an Exif segment that holds a maker note.

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"os"
)

const (
	tagExifIFD     = 0x8769
	tagMakerNote   = 0x927c
	tagOrientation = 0x0112
)

var exifPrefix = []byte("Exif\x00\x00")

// jpegSegments calls f with each marker and payload before the picture data.
func jpegSegments(data []byte, f func(marker byte, payload []byte) bool) {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return
	}
	at := 2
	for at+4 <= len(data) && data[at] == 0xff {
		m := data[at+1]
		if m == 0xda || m == 0xd9 {
			return
		}
		n := int(binary.BigEndian.Uint16(data[at+2:]))
		if n < 2 || at+2+n > len(data) {
			return
		}
		if !f(m, data[at+4:at+2+n]) {
			return
		}
		at += 2 + n
	}
}

// JpegExif is the TIFF data of a JPEG's Exif segment, or nil.
func JpegExif(data []byte) []byte {
	var tiff []byte
	jpegSegments(data, func(m byte, p []byte) bool {
		if m == 0xe1 && bytes.HasPrefix(p, exifPrefix) {
			tiff = p[len(exifPrefix):]
			return false
		}
		return true
	})
	return tiff
}

// JpegIcc is a JPEG's ICC profile (APP2 ICC_PROFILE chunks in order), or nil.
func JpegIcc(data []byte) []byte {
	sig := []byte("ICC_PROFILE\x00")
	chunks := map[int][]byte{}
	count := 0
	jpegSegments(data, func(m byte, p []byte) bool {
		if m == 0xe2 && bytes.HasPrefix(p, sig) && len(p) > len(sig)+2 {
			chunks[int(p[len(sig)])] = p[len(sig)+2:]
			count = int(p[len(sig)+1])
		}
		return true
	})
	if count == 0 {
		return nil
	}
	var out []byte
	for i := 1; i <= count; i++ {
		c, ok := chunks[i]
		if !ok {
			return nil
		}
		out = append(out, c...)
	}
	return out
}

// PngIcc is a PNG's ICC profile (iCCP chunk, decompressed), or nil.
func PngIcc(data []byte) []byte {
	if len(data) < 8 || !bytes.Equal(data[:8], []byte("\x89PNG\r\n\x1a\n")) {
		return nil
	}
	at := 8
	for at+8 <= len(data) {
		n := int(binary.BigEndian.Uint32(data[at:]))
		typ := string(data[at+4 : at+8])
		if n < 0 || at+12+n > len(data) {
			return nil
		}
		body := data[at+8 : at+8+n]
		switch typ {
		case "iCCP":
			z := bytes.IndexByte(body, 0)
			if z < 0 || z+2 > len(body) {
				return nil
			}
			r, err := zlib.NewReader(bytes.NewReader(body[z+2:]))
			if err != nil {
				return nil
			}
			p, err := io.ReadAll(io.LimitReader(r, 16<<20))
			if err != nil {
				return nil
			}
			return p
		case "IDAT", "IEND":
			return nil
		}
		at += 12 + n
	}
	return nil
}

type tiffReader struct {
	d     []byte
	order binary.ByteOrder
}

func newTiff(d []byte) *tiffReader {
	if len(d) < 8 {
		return nil
	}
	switch {
	case d[0] == 'I' && d[1] == 'I':
		return &tiffReader{d, binary.LittleEndian}
	case d[0] == 'M' && d[1] == 'M':
		return &tiffReader{d, binary.BigEndian}
	}
	return nil
}

type ifdEntry struct {
	tag, typ uint16
	count    uint32
	value    []byte // the 4-byte field
	at       int    // offset of the entry
}

func (t *tiffReader) ifd(offset uint32) ([]ifdEntry, uint32) {
	o := int(offset)
	if o <= 0 || o+2 > len(t.d) {
		return nil, 0
	}
	n := int(t.order.Uint16(t.d[o:]))
	if o+2+n*12+4 > len(t.d) {
		return nil, 0
	}
	es := make([]ifdEntry, n)
	for i := range es {
		e := t.d[o+2+i*12:]
		es[i] = ifdEntry{t.order.Uint16(e), t.order.Uint16(e[2:]), t.order.Uint32(e[4:]), e[8:12], o + 2 + i*12}
	}
	return es, t.order.Uint32(t.d[o+2+n*12:])
}

func typeSize(t uint16) int {
	switch t {
	case 1, 2, 6, 7:
		return 1
	case 3, 8:
		return 2
	case 4, 9, 11:
		return 4
	case 5, 10, 12:
		return 8
	}
	return 0
}

func (t *tiffReader) bytesOf(e ifdEntry) []byte {
	n := int(e.count) * typeSize(e.typ)
	if n <= 4 {
		return e.value[:n]
	}
	o := int(t.order.Uint32(e.value))
	if o < 0 || n < 0 || o+n > len(t.d) {
		return nil
	}
	return t.d[o : o+n]
}

// TiffMakerNote is the MakerNote of Exif TIFF data, or nil.
func TiffMakerNote(tiff []byte) []byte {
	t := newTiff(tiff)
	if t == nil {
		return nil
	}
	ifd0, _ := t.ifd(t.order.Uint32(tiff[4:]))
	for _, e := range ifd0 {
		if e.tag != tagExifIFD {
			continue
		}
		sub, _ := t.ifd(t.order.Uint32(e.value))
		for _, s := range sub {
			if s.tag == tagMakerNote {
				return t.bytesOf(s)
			}
		}
	}
	return nil
}

// TiffOrientation is the Exif orientation (1–8), 1 when unknown.
func TiffOrientation(tiff []byte) int {
	t := newTiff(tiff)
	if t == nil {
		return 1
	}
	ifd0, _ := t.ifd(t.order.Uint32(tiff[4:]))
	for _, e := range ifd0 {
		if e.tag == tagOrientation && e.typ == 3 {
			if v := int(t.order.Uint16(e.value)); v >= 1 && v <= 8 {
				return v
			}
		}
	}
	return 1
}

// UprightExif returns a copy of tiff with the orientation 1 (the pixels are
// upright now) and without the thumbnail IFD (it would show them as they were).
func UprightExif(tiff []byte) []byte {
	t := newTiff(bytes.Clone(tiff))
	if t == nil {
		return nil
	}
	off := t.order.Uint32(t.d[4:])
	ifd0, _ := t.ifd(off)
	for _, e := range ifd0 {
		if e.tag == tagOrientation && e.typ == 3 && e.count == 1 {
			t.order.PutUint16(t.d[e.at+8:], 1)
		}
	}
	if len(ifd0) > 0 {
		next := int(off) + 2 + len(ifd0)*12
		t.order.PutUint32(t.d[next:], 0)
	}
	return t.d
}

// MakerNoteExif is Exif TIFF data (big-endian) holding only maker note: IFD0
// with the Exif IFD pointer, the Exif IFD with the maker note.
func MakerNoteExif(note []byte) []byte {
	be := binary.BigEndian
	b := []byte("MM\x00\x2a")
	b = be.AppendUint32(b, 8)
	// IFD0 at 8: one entry, next IFD 0 → 8+2+12+4 = 26.
	b = be.AppendUint16(b, 1)
	b = be.AppendUint16(b, tagExifIFD)
	b = be.AppendUint16(b, 4)
	b = be.AppendUint32(b, 1)
	b = be.AppendUint32(b, 26)
	b = be.AppendUint32(b, 0)
	// Exif IFD at 26: one entry → value at 26+18 = 44.
	b = be.AppendUint16(b, 1)
	b = be.AppendUint16(b, tagMakerNote)
	b = be.AppendUint16(b, 7)
	b = be.AppendUint32(b, uint32(len(note)))
	if len(note) <= 4 {
		v := make([]byte, 4)
		copy(v, note)
		b = append(b, v...)
		return be.AppendUint32(b, 0)
	}
	b = be.AppendUint32(b, 44)
	b = be.AppendUint32(b, 0)
	return append(b, note...)
}

// WithExif returns jpeg with tiff as its Exif segment (right after the start
// marker, before any JFIF segment as cameras write it); an existing Exif
// segment is replaced.
func WithExif(jpeg, tiff []byte) ([]byte, error) {
	if len(jpeg) < 4 || jpeg[0] != 0xff || jpeg[1] != 0xd8 {
		return nil, invalid("not a JPEG")
	}
	payload := append(bytes.Clone(exifPrefix), tiff...)
	if len(payload)+2 > 0xffff {
		return nil, invalid("Exif too large")
	}
	out := []byte{0xff, 0xd8, 0xff, 0xe1}
	out = binary.BigEndian.AppendUint16(out, uint16(len(payload)+2))
	out = append(out, payload...)
	at := 2
	for at+4 <= len(jpeg) && jpeg[at] == 0xff {
		m := jpeg[at+1]
		n := int(binary.BigEndian.Uint16(jpeg[at+2:]))
		if m == 0xda || at+2+n > len(jpeg) {
			break
		}
		if m == 0xe1 && bytes.HasPrefix(jpeg[at+4:], exifPrefix) {
			jpeg = append(bytes.Clone(jpeg[:at]), jpeg[at+2+n:]...)
			continue
		}
		at += 2 + n
	}
	return append(out, jpeg[2:]...), nil
}

// HeifExif is the TIFF data of a HEIC / AVIF picture's Exif item, or nil.
func HeifExif(r io.ReadSeeker) []byte {
	total, err := streamLength(r)
	if err != nil {
		return nil
	}
	r.Seek(0, io.SeekStart)
	var meta []byte
	for pos := int64(0); pos < total; {
		b, err := ReadBoxHeader(r)
		if err != nil {
			return nil
		}
		if b.Type == "meta" {
			if b.Length > 16<<20 {
				return nil
			}
			meta = make([]byte, b.Length-int64(b.HeaderLength))
			if _, err := io.ReadFull(r, meta); err != nil {
				return nil
			}
			break
		}
		pos = b.End()
		r.Seek(pos, io.SeekStart)
	}
	if len(meta) < 4 {
		return nil
	}
	children, err := Children(meta, 4)
	if err != nil {
		return nil
	}
	id := exifItemID(firstChild(children, "iinf"))
	if id == 0 {
		return nil
	}
	idat := firstChild(children, "idat")
	data := itemData(r, firstChild(children, "iloc"), id, idat, total)
	if len(data) < 4 {
		return nil
	}
	skip := int(binary.BigEndian.Uint32(data))
	if 4+skip > len(data) {
		return nil
	}
	return data[4+skip:]
}

func exifItemID(iinf []byte) uint32 {
	if len(iinf) < 6 {
		return 0
	}
	at := 6
	if iinf[0] != 0 {
		at = 8
	}
	entries, err := Children(iinf, at)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		p := e.Payload
		if e.Type != "infe" || len(p) < 4 {
			continue
		}
		v := p[0]
		switch {
		case v == 2 && len(p) >= 12:
			if string(p[8:12]) == "Exif" {
				return uint32(binary.BigEndian.Uint16(p[4:]))
			}
		case v == 3 && len(p) >= 14:
			if string(p[10:14]) == "Exif" {
				return binary.BigEndian.Uint32(p[4:])
			}
		}
	}
	return 0
}

// itemData reads item id's extents from iloc (file offsets or idat).
func itemData(r io.ReadSeeker, iloc []byte, id uint32, idat []byte, total int64) []byte {
	if len(iloc) < 8 {
		return nil
	}
	v := iloc[0]
	offSize, lenSize := int(iloc[4]>>4), int(iloc[4]&15)
	baseSize, idxSize := int(iloc[5]>>4), 0
	if v == 1 || v == 2 {
		idxSize = int(iloc[5] & 15)
	}
	at := 6
	rd := func(n int) (uint64, bool) {
		if at+n > len(iloc) {
			return 0, false
		}
		var x uint64
		for i := 0; i < n; i++ {
			x = x<<8 | uint64(iloc[at+i])
		}
		at += n
		return x, true
	}
	cnt := 2
	if v == 2 {
		cnt = 4
	}
	count, ok := rd(cnt)
	if !ok {
		return nil
	}
	for i := uint64(0); i < count; i++ {
		item, ok := rd(cnt)
		if !ok {
			return nil
		}
		method := uint64(0)
		if v == 1 || v == 2 {
			m, _ := rd(2)
			method = m & 15
		}
		rd(2) // data_reference_index
		base, _ := rd(baseSize)
		extents, ok := rd(2)
		if !ok {
			return nil
		}
		var out []byte
		for k := uint64(0); k < extents; k++ {
			rd(idxSize)
			off, _ := rd(offSize)
			l, ok := rd(lenSize)
			if !ok {
				return nil
			}
			if uint32(item) != id {
				continue
			}
			start := int64(base + off)
			if l > 16<<20 {
				return nil
			}
			switch method {
			case 0:
				if start < 0 || start+int64(l) > total {
					return nil
				}
				buf := make([]byte, l)
				r.Seek(start, io.SeekStart)
				if _, err := io.ReadFull(r, buf); err != nil {
					return nil
				}
				out = append(out, buf...)
			case 1:
				if start < 0 || start+int64(l) > int64(len(idat)) {
					return nil
				}
				out = append(out, idat[start:start+int64(l)]...)
			default:
				return nil
			}
		}
		if uint32(item) == id {
			return out
		}
	}
	return nil
}

// PhotoExif is the Exif TIFF data of a JPEG or HEIC/AVIF file, or nil.
func PhotoExif(path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var head [2]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return nil
	}
	if head[0] == 0xff && head[1] == 0xd8 {
		f.Seek(0, io.SeekStart)
		// The Exif segment comes first; 256 KB covers it.
		data, _ := io.ReadAll(io.LimitReader(f, 256<<10))
		return JpegExif(data)
	}
	return HeifExif(f)
}
