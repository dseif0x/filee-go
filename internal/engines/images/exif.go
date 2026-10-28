package images

import (
	"bytes"
	"encoding/binary"
)

// Exif data is a TIFF structure: a byte order mark, IFD0 (the picture's tags,
// orientation among them) and maybe IFD1, the thumbnail. These helpers read
// and patch it in place, so everything else in it stays as the camera wrote it.

const (
	tagOrientation    = 0x0112
	tagThumbOffset    = 0x0201
	tagThumbLength    = 0x0202
	tagStripOffsets   = 0x0111
	tagStripBytes     = 0x0117
	tagXResolution    = 0x011A
	tagYResolution    = 0x011B
	tagResolutionUnit = 0x0128
)

type exifView struct {
	b     []byte
	order binary.ByteOrder
	ifd0  int
}

// exifStart finds the TIFF header in exif ("Exif\0\0" and other prefixes skipped).
func exifStart(exif []byte) int {
	for i := 0; i+4 <= len(exif) && i < 16; i++ {
		if bytes.HasPrefix(exif[i:], []byte("II*\x00")) || bytes.HasPrefix(exif[i:], []byte("MM\x00*")) {
			return i
		}
	}
	return -1
}

// normalizeExif returns exif from its TIFF header on, or nil.
func normalizeExif(exif []byte) []byte {
	i := exifStart(exif)
	if i < 0 {
		return nil
	}
	return exif[i:]
}

func viewExif(exif []byte) *exifView {
	if len(exif) < 8 {
		return nil
	}
	v := &exifView{b: exif}
	switch string(exif[:2]) {
	case "II":
		v.order = binary.LittleEndian
	case "MM":
		v.order = binary.BigEndian
	default:
		return nil
	}
	v.ifd0 = int(v.order.Uint32(exif[4:]))
	if v.ifd0 < 8 || v.ifd0+2 > len(exif) {
		return nil
	}
	return v
}

// entries calls fn for every entry of the IFD at off (its offset in b) and
// returns the offset of the next-IFD field, or -1.
func (v *exifView) entries(off int, fn func(entry int, tag, typ uint16, count uint32)) int {
	if off+2 > len(v.b) {
		return -1
	}
	n := int(v.order.Uint16(v.b[off:]))
	if off+2+n*12+4 > len(v.b) {
		return -1
	}
	for i := 0; i < n; i++ {
		e := off + 2 + i*12
		fn(e, v.order.Uint16(v.b[e:]), v.order.Uint16(v.b[e+2:]), v.order.Uint32(v.b[e+4:]))
	}
	return off + 2 + n*12
}

// exifOrientation reads the orientation tag of IFD0 (0 when there is none).
func exifOrientation(exif []byte) int {
	v := viewExif(exif)
	if v == nil {
		return 0
	}
	o := 0
	v.entries(v.ifd0, func(e int, tag, typ uint16, count uint32) {
		if tag == tagOrientation && typ == 3 && count == 1 {
			o = int(v.order.Uint16(v.b[e+8:]))
		}
	})
	if o < 1 || o > 8 {
		return 0
	}
	return o
}

// setExifOrientation writes orientation into a copy of exif (when it has the tag).
func setExifOrientation(exif []byte, orientation int) []byte {
	out := bytes.Clone(exif)
	v := viewExif(out)
	if v == nil {
		return out
	}
	v.entries(v.ifd0, func(e int, tag, typ uint16, count uint32) {
		if tag == tagOrientation && typ == 3 && count == 1 {
			v.order.PutUint16(v.b[e+8:], uint16(orientation))
		}
	})
	return out
}

// exifHasThumbnail reports whether exif has an IFD1.
func exifHasThumbnail(exif []byte) bool {
	v := viewExif(exif)
	if v == nil {
		return false
	}
	next := v.entries(v.ifd0, func(int, uint16, uint16, uint32) {})
	return next > 0 && v.order.Uint32(v.b[next:]) != 0
}

// removeExifThumbnail returns a copy of exif without its thumbnail: IFD1 is
// unlinked and the thumbnail's bytes are zeroed, so the old picture cannot be
// read out of the file any more.
func removeExifThumbnail(exif []byte) []byte {
	out := bytes.Clone(exif)
	v := viewExif(out)
	if v == nil {
		return out
	}
	next := v.entries(v.ifd0, func(int, uint16, uint16, uint32) {})
	if next < 0 {
		return out
	}
	ifd1 := int(v.order.Uint32(out[next:]))
	v.order.PutUint32(out[next:], 0)
	if ifd1 < 8 || ifd1 >= len(out) {
		return out
	}
	var offs, lens []int
	value := func(e int, typ uint16) int {
		if typ == 3 {
			return int(v.order.Uint16(out[e+8:]))
		}
		return int(v.order.Uint32(out[e+8:]))
	}
	v.entries(ifd1, func(e int, tag, typ uint16, count uint32) {
		if count != 1 {
			return
		}
		switch tag {
		case tagThumbOffset, tagStripOffsets:
			offs = append(offs, value(e, typ))
		case tagThumbLength, tagStripBytes:
			lens = append(lens, value(e, typ))
		}
	})
	for i := range min(len(offs), len(lens)) {
		if offs[i] > 0 && lens[i] > 0 && offs[i]+lens[i] <= len(out) {
			clear(out[offs[i] : offs[i]+lens[i]])
		}
	}
	return out
}
