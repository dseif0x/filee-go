package images

import (
	"bytes"
	"encoding/binary"
)

// heifMeta reads the Exif item and the ICC profile (colr "prof") of a HEIF
// (HEIC, AVIF) file. The decoders apply irot/imir themselves, so the Exif
// orientation is only informative there and is not used.
func heifMeta(data []byte) Meta {
	var m Meta
	meta := findBox(data, "meta")
	if meta == nil || len(meta) < 4 {
		return m
	}
	meta = meta[4:] // full box
	exifID := uint32(0)
	if iinf := findBox(meta, "iinf"); len(iinf) >= 6 {
		body := iinf[4:]
		if iinf[0] == 0 {
			body = body[2:]
		} else {
			body = body[4:]
		}
		eachBox(body, func(typ string, b []byte) {
			if typ != "infe" || len(b) < 4 || b[0] < 2 {
				return
			}
			v := b[0]
			b = b[4:]
			var id uint32
			if v == 2 {
				if len(b) < 8 {
					return
				}
				id, b = uint32(binary.BigEndian.Uint16(b)), b[4:]
			} else {
				if len(b) < 10 {
					return
				}
				id, b = binary.BigEndian.Uint32(b), b[6:]
			}
			if string(b[:4]) == "Exif" {
				exifID = id
			}
		})
	}
	if exifID != 0 {
		if off, n, ok := ilocExtent(findBox(meta, "iloc"), exifID); ok && off+n <= uint64(len(data)) && n > 4 {
			item := data[off : off+n]
			skip := binary.BigEndian.Uint32(item)
			if uint64(skip)+4 < n {
				// The decoders turn the picture (irot/imir): an orientation left in the Exif would turn it twice.
				if exif := normalizeExif(bytes.Clone(item[4+skip:])); exif != nil {
					m.Exif = setExifOrientation(exif, 1)
				}
			}
		}
	}
	if iprp := findBox(meta, "iprp"); iprp != nil {
		if ipco := findBox(iprp, "ipco"); ipco != nil {
			eachBox(ipco, func(typ string, b []byte) {
				if typ == "colr" && len(b) > 4 && (string(b[:4]) == "prof" || string(b[:4]) == "rICC") && m.ICC == nil {
					m.ICC = bytes.Clone(b[4:])
				}
			})
		}
	}
	return m
}

// eachBox calls fn for the boxes laid out in b.
func eachBox(b []byte, fn func(typ string, body []byte)) {
	for len(b) >= 8 {
		size := uint64(binary.BigEndian.Uint32(b))
		head := uint64(8)
		if size == 1 {
			if len(b) < 16 {
				return
			}
			size, head = binary.BigEndian.Uint64(b[8:]), 16
		} else if size == 0 {
			size = uint64(len(b))
		}
		if size < head || size > uint64(len(b)) {
			return
		}
		fn(string(b[4:8]), b[head:size])
		b = b[size:]
	}
}

// findBox returns the body of the first box of typ among the boxes in b.
func findBox(b []byte, typ string) []byte {
	var found []byte
	eachBox(b, func(t string, body []byte) {
		if found == nil && t == typ {
			found = body
		}
	})
	return found
}

// ilocExtent is the file offset and length of item id (its first extent).
func ilocExtent(iloc []byte, id uint32) (uint64, uint64, bool) {
	if len(iloc) < 8 {
		return 0, 0, false
	}
	version := iloc[0]
	p := 4
	offSize, lenSize := int(iloc[p]>>4), int(iloc[p]&15)
	baseSize, idxSize := int(iloc[p+1]>>4), int(iloc[p+1]&15)
	p += 2
	read := func(n int) (uint64, bool) {
		if p+n > len(iloc) {
			return 0, false
		}
		var v uint64
		for i := 0; i < n; i++ {
			v = v<<8 | uint64(iloc[p+i])
		}
		p += n
		return v, true
	}
	var count uint64
	var ok bool
	if version < 2 {
		count, ok = read(2)
	} else {
		count, ok = read(4)
	}
	if !ok {
		return 0, 0, false
	}
	for i := uint64(0); i < count; i++ {
		var itemID uint64
		if version < 2 {
			itemID, ok = read(2)
		} else {
			itemID, ok = read(4)
		}
		if !ok {
			return 0, 0, false
		}
		if version == 1 || version == 2 {
			if _, ok = read(2); !ok { // construction method
				return 0, 0, false
			}
		}
		read(2) // data reference index
		base, _ := read(baseSize)
		extents, ok := read(2)
		if !ok {
			return 0, 0, false
		}
		var firstOff, firstLen uint64
		for e := uint64(0); e < extents; e++ {
			if (version == 1 || version == 2) && idxSize > 0 {
				read(idxSize)
			}
			off, _ := read(offSize)
			n, ok := read(lenSize)
			if !ok {
				return 0, 0, false
			}
			if e == 0 {
				firstOff, firstLen = off, n
			}
		}
		if uint32(itemID) == id {
			return base + firstOff, firstLen, true
		}
	}
	return 0, 0, false
}
