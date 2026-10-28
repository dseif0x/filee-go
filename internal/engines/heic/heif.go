package heic

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// HEIF writing (ISO/IEC 23008-12, HEIF with HEVC pictures): a primary picture
// that is either one coded picture or, like an iPhone photo, a grid of
// 512 × 512 tiles, with its colour profile and Exif. FFmpeg only encodes the
// HEVC pictures; it has no HEIF muxer.

// Image is one picture to store as HEIC.
type Image struct {
	// Width and Height of the picture; the tiles may extend beyond it (the grid crops them).
	Width, Height int
	// Stream holds the coded tiles, row by row.
	Stream *Stream
	// TileWidth and TileHeight of every coded tile; Columns × Rows tiles.
	TileWidth, TileHeight, Columns, Rows int
	// ICC profile, or nil to declare sRGB.
	ICC []byte
	// Exif from its TIFF header (an "Exif\0\0" prefix is skipped), or nil.
	Exif []byte
}

// IsGrid: the picture is a grid item rather than the coded picture itself.
func (im *Image) IsGrid() bool {
	return im.Columns*im.Rows > 1 || im.TileWidth != im.Width || im.TileHeight != im.Height
}

const primaryID = 1

type item struct {
	id   int
	data []byte
}

// WriteHEIF builds the HEIC file of im.
func WriteHEIF(im *Image) ([]byte, error) {
	tiles := im.Stream.Pictures
	if len(tiles) != im.Columns*im.Rows {
		return nil, fmt.Errorf("%d coded pictures for %d × %d tiles", len(tiles), im.Columns, im.Rows)
	}
	if im.Columns > 256 || im.Rows > 256 {
		return nil, errors.New("a HEIF grid holds at most 256 × 256 tiles")
	}
	grid := im.IsGrid()
	var tileIDs []int
	if grid {
		for i := range tiles {
			tileIDs = append(tileIDs, primaryID+1+i)
		}
	} else {
		tileIDs = []int{primaryID}
	}
	exif := ExifPayload(im.Exif)
	exifID := 0
	if exif != nil {
		exifID = tileIDs[len(tileIDs)-1] + 1
	}
	var items []item
	if grid {
		items = append(items, item{primaryID, gridDescription(im)})
	}
	for i, id := range tileIDs {
		items = append(items, item{id, tiles[i]})
	}
	if exif != nil {
		items = append(items, item{exifID, exif})
	}
	ftyp := box("ftyp", func(w *boxWriter) {
		w.text("heic")
		w.u32(0)
		w.text("mif1")
		w.text("heic")
	})
	// The item locations hold absolute offsets: the header is built twice (its size does not depend on them).
	header := meta(im, items, tileIDs, exifID, map[int]int64{})
	offsets := map[int]int64{}
	at := int64(len(ftyp) + len(header) + 8)
	for _, it := range items {
		offsets[it.id] = at
		at += int64(len(it.data))
	}
	if at > math.MaxUint32 {
		return nil, errors.New("the picture is too large for a HEIC file")
	}
	header = meta(im, items, tileIDs, exifID, offsets)
	var out bytes.Buffer
	out.Write(ftyp)
	out.Write(header)
	size := int64(8)
	for _, it := range items {
		size += int64(len(it.data))
	}
	out.Write(binary.BigEndian.AppendUint32(nil, uint32(size)))
	out.WriteString("mdat")
	for _, it := range items {
		out.Write(it.data)
	}
	return out.Bytes(), nil
}

func meta(im *Image, items []item, tileIDs []int, exifID int, offsets map[int]int64) []byte {
	return fullBox("meta", 0, 0, func(w *boxWriter) {
		w.raw(fullBox("hdlr", 0, 0, func(h *boxWriter) {
			h.u32(0)
			h.text("pict")
			h.zeros(12)
			h.raw([]byte{0})
		}))
		w.raw(box("dinf", func(d *boxWriter) {
			d.raw(fullBox("dref", 0, 0, func(r *boxWriter) {
				r.u32(1)
				r.raw(fullBox("url ", 0, 1, func(*boxWriter) {}))
			}))
		}))
		w.raw(fullBox("pitm", 0, 0, func(p *boxWriter) { p.u16(primaryID) }))
		w.raw(fullBox("iloc", 0, 0, func(l *boxWriter) {
			l.raw([]byte{0x44, 0x00}) // offset size 4, length size 4, base offset size 0
			l.u16(len(items))
			for _, it := range items {
				l.u16(it.id)
				l.u16(0) // data reference: this file
				l.u16(1) // one extent
				l.u32(uint32(offsets[it.id]))
				l.u32(uint32(len(it.data)))
			}
		}))
		w.raw(fullBox("iinf", 0, 0, func(l *boxWriter) {
			l.u16(len(items))
			for _, it := range items {
				typ := "hvc1"
				switch {
				case it.id == exifID:
					typ = "Exif"
				case it.id == primaryID && im.IsGrid():
					typ = "grid"
				}
				// Tiles are hidden: viewers show the grid, not its pieces.
				flags := 0
				if im.IsGrid() && it.id != primaryID && it.id != exifID {
					flags = 1
				}
				l.raw(fullBox("infe", 2, flags, func(e *boxWriter) {
					e.u16(it.id)
					e.u16(0)
					e.text(typ)
					e.raw([]byte{0})
				}))
			}
		}))
		if im.IsGrid() || exifID != 0 {
			w.raw(fullBox("iref", 0, 0, func(r *boxWriter) {
				if im.IsGrid() {
					r.raw(box("dimg", func(d *boxWriter) { references(d, primaryID, tileIDs) }))
				}
				if exifID != 0 {
					r.raw(box("cdsc", func(d *boxWriter) { references(d, exifID, []int{primaryID}) }))
				}
			}))
		}
		w.raw(properties(im, tileIDs))
	})
}

func references(w *boxWriter, from int, to []int) {
	w.u16(from)
	w.u16(len(to))
	for _, id := range to {
		w.u16(id)
	}
}

// properties are ipco and which item has which (ipma, 1-based indexes; hvcC is essential).
func properties(im *Image, tileIDs []int) []byte {
	depth := byte(im.Stream.BitDepth)
	var colr []byte
	if im.ICC != nil {
		colr = box("colr", func(w *boxWriter) { w.text("prof"); w.raw(im.ICC) })
	} else {
		// sRGB (BT.709 primaries, sRGB transfer) stored as BT.601 full-range YCbCr, like JPEG.
		colr = box("colr", func(w *boxWriter) { w.text("nclx"); w.u16(1); w.u16(13); w.u16(6); w.raw([]byte{0x80}) })
	}
	props := [][]byte{
		box("hvcC", func(w *boxWriter) { w.raw(im.Stream.Configuration) }),
		fullBox("ispe", 0, 0, func(w *boxWriter) { w.u32(uint32(im.TileWidth)); w.u32(uint32(im.TileHeight)) }),
		fullBox("pixi", 0, 0, func(w *boxWriter) { w.raw([]byte{3, depth, depth, depth}) }),
		colr,
	}
	const hvcC, tileSize, pixi, colrIdx = 1, 2, 3, 4
	type assoc struct {
		index     int
		essential bool
	}
	type association struct {
		item  int
		props []assoc
	}
	var assocs []association
	if im.IsGrid() {
		props = append(props, fullBox("ispe", 0, 0, func(w *boxWriter) { w.u32(uint32(im.Width)); w.u32(uint32(im.Height)) }))
		assocs = append(assocs, association{primaryID, []assoc{{len(props), false}, {pixi, false}, {colrIdx, false}}})
		for _, id := range tileIDs {
			assocs = append(assocs, association{id, []assoc{{hvcC, true}, {tileSize, false}}})
		}
	} else {
		assocs = append(assocs, association{primaryID, []assoc{{hvcC, true}, {tileSize, false}, {pixi, false}, {colrIdx, false}}})
	}
	return box("iprp", func(w *boxWriter) {
		w.raw(box("ipco", func(c *boxWriter) {
			for _, p := range props {
				c.raw(p)
			}
		}))
		w.raw(fullBox("ipma", 0, 0, func(m *boxWriter) {
			m.u32(uint32(len(assocs)))
			for _, a := range assocs {
				m.u16(a.item)
				m.raw([]byte{byte(len(a.props))})
				for _, p := range a.props {
					v := byte(p.index)
					if p.essential {
						v |= 0x80
					}
					m.raw([]byte{v})
				}
			}
		}))
	})
}

// gridDescription is ImageGrid (23008-12, 6.6.2.3): rows, columns and the size the tiles are cropped to.
func gridDescription(im *Image) []byte {
	wide := im.Width > 0xFFFF || im.Height > 0xFFFF
	w := &boxWriter{}
	flag := byte(0)
	if wide {
		flag = 1
	}
	w.raw([]byte{0, flag, byte(im.Rows - 1), byte(im.Columns - 1)})
	if wide {
		w.u32(uint32(im.Width))
		w.u32(uint32(im.Height))
	} else {
		w.u16(im.Width)
		w.u16(im.Height)
	}
	return w.b.Bytes()
}

// ExifPayload is the Exif item: the offset of the TIFF header in the data, then the data.
func ExifPayload(exif []byte) []byte {
	if exif == nil {
		return nil
	}
	tiff := -1
	for i := 0; i+3 < len(exif) && i < 16 && tiff < 0; i++ {
		if bytes.HasPrefix(exif[i:], []byte("II*\x00")) || bytes.HasPrefix(exif[i:], []byte("MM\x00*")) {
			tiff = i
		}
	}
	if tiff < 0 {
		return nil
	}
	return append(binary.BigEndian.AppendUint32(nil, uint32(tiff)), exif...)
}

type boxWriter struct{ b bytes.Buffer }

func (w *boxWriter) raw(p []byte)  { w.b.Write(p) }
func (w *boxWriter) text(s string) { w.b.WriteString(s) }
func (w *boxWriter) zeros(n int)   { w.b.Write(make([]byte, n)) }
func (w *boxWriter) u16(v int)     { w.b.Write(binary.BigEndian.AppendUint16(nil, uint16(v))) }
func (w *boxWriter) u32(v uint32)  { w.b.Write(binary.BigEndian.AppendUint32(nil, v)) }

func box(typ string, body func(*boxWriter)) []byte {
	w := &boxWriter{}
	body(w)
	p := w.b.Bytes()
	out := binary.BigEndian.AppendUint32(nil, uint32(8+len(p)))
	out = append(out, typ...)
	return append(out, p...)
}

func fullBox(typ string, version, flags int, body func(*boxWriter)) []byte {
	return box(typ, func(w *boxWriter) {
		w.u32(uint32(version<<24 | flags))
		body(w)
	})
}
