package media

// Writes HEIC files (HEIF with HEVC pictures): a primary picture that is one
// coded picture or, like an iPhone photo, a grid of 512 × 512 tiles, with its
// colour profile and Exif. FFmpeg only encodes the HEVC pictures.

import (
	"bytes"
	"encoding/binary"
)

// HeifImage is one picture to store as HEIC.
type HeifImage struct {
	Width, Height         int
	Stream                *HevcStream
	TileWidth, TileHeight int
	Columns, Rows         int
	// IccProfile, or nil to declare sRGB.
	IccProfile []byte
	// Exif starting with a TIFF header (an "Exif\0\0" prefix is fine), or nil.
	Exif []byte
}

// IsGrid: the picture is a grid item rather than the coded picture itself.
func (h *HeifImage) IsGrid() bool {
	return h.Columns*h.Rows > 1 || h.TileWidth != h.Width || h.TileHeight != h.Height
}

const heifPrimary = 1

type heifItem struct {
	id   int
	data []byte
}

// WriteHeif returns the HEIC file of img.
func WriteHeif(img *HeifImage) ([]byte, error) {
	tiles := img.Stream.Pictures
	if len(tiles) != img.Columns*img.Rows {
		return nil, invalid("%d coded pictures for %d × %d tiles", len(tiles), img.Columns, img.Rows)
	}
	if img.Columns > 256 || img.Rows > 256 {
		return nil, invalid("a HEIF grid holds at most 256 × 256 tiles")
	}
	grid := img.IsGrid()
	tileIDs := []int{heifPrimary}
	if grid {
		tileIDs = nil
		for i := range tiles {
			tileIDs = append(tileIDs, heifPrimary+1+i)
		}
	}
	exif := ExifPayload(img.Exif)
	exifID := 0
	if exif != nil {
		exifID = tileIDs[len(tileIDs)-1] + 1
	}
	var items []heifItem
	if grid {
		items = append(items, heifItem{heifPrimary, gridDescription(img)})
	}
	for i, id := range tileIDs {
		items = append(items, heifItem{id, tiles[i]})
	}
	if exif != nil {
		items = append(items, heifItem{exifID, exif})
	}
	ftyp := MakeBox("ftyp", []byte("heic"), u32(0), []byte("mif1"), []byte("heic"))
	// Item locations are absolute offsets: the header is built twice (its size doesn't depend on them).
	header := heifMeta(img, items, tileIDs, exifID, nil)
	offsets := map[int]int64{}
	at := int64(len(ftyp) + len(header) + 8)
	for _, it := range items {
		offsets[it.id] = at
		at += int64(len(it.data))
	}
	if at > 0xffffffff {
		return nil, invalid("the picture is too large for a HEIC file")
	}
	header = heifMeta(img, items, tileIDs, exifID, offsets)
	var f bytes.Buffer
	f.Write(ftyp)
	f.Write(header)
	size := int64(8)
	for _, it := range items {
		size += int64(len(it.data))
	}
	f.Write(u32(uint32(size)))
	f.WriteString("mdat")
	for _, it := range items {
		f.Write(it.data)
	}
	return f.Bytes(), nil
}

func heifMeta(img *HeifImage, items []heifItem, tileIDs []int, exifID int, offsets map[int]int64) []byte {
	hdlr := FullBox("hdlr", 0, 0, u32(0), []byte("pict"), make([]byte, 12), []byte{0})
	dinf := MakeBox("dinf", FullBox("dref", 0, 0, u32(1), FullBox("url ", 0, 1)))
	pitm := FullBox("pitm", 0, 0, u16(heifPrimary))
	iloc := []byte{0x44, 0x00} // offset_size 4, length_size 4, base_offset_size 0
	iloc = append(iloc, u16(len(items))...)
	for _, it := range items {
		iloc = append(iloc, u16(it.id)...)
		iloc = append(iloc, u16(0)...)
		iloc = append(iloc, u16(1)...)
		iloc = append(iloc, u32(uint32(offsets[it.id]))...)
		iloc = append(iloc, u32(uint32(len(it.data)))...)
	}
	iinf := u16(len(items))
	for _, it := range items {
		typ := "hvc1"
		if it.id == exifID {
			typ = "Exif"
		} else if it.id == heifPrimary && img.IsGrid() {
			typ = "grid"
		}
		// Tiles are hidden: viewers show the grid, not its pieces.
		flags := uint32(0)
		if img.IsGrid() && it.id != heifPrimary && it.id != exifID {
			flags = 1
		}
		iinf = append(iinf, FullBox("infe", 2, flags, u16(it.id), u16(0), []byte(typ), []byte{0})...)
	}
	parts := [][]byte{hdlr, dinf, pitm, FullBox("iloc", 0, 0, iloc), FullBox("iinf", 0, 0, iinf)}
	if img.IsGrid() || exifID != 0 {
		var refs []byte
		if img.IsGrid() {
			refs = append(refs, MakeBox("dimg", references(heifPrimary, tileIDs))...)
		}
		if exifID != 0 {
			refs = append(refs, MakeBox("cdsc", references(exifID, []int{heifPrimary}))...)
		}
		parts = append(parts, FullBox("iref", 0, 0, refs))
	}
	parts = append(parts, heifProperties(img, tileIDs))
	return FullBox("meta", 0, 0, parts...)
}

func references(from int, to []int) []byte {
	b := append(u16(from), u16(len(to))...)
	for _, id := range to {
		b = append(b, u16(id)...)
	}
	return b
}

type propRef struct {
	index     int
	essential bool
}

func heifProperties(img *HeifImage, tileIDs []int) []byte {
	d := byte(img.Stream.BitDepth)
	var colr []byte
	if img.IccProfile != nil {
		colr = MakeBox("colr", []byte("prof"), img.IccProfile)
	} else {
		// sRGB (BT.709 primaries, sRGB transfer) as BT.601 full-range YCbCr, like JPEG.
		colr = MakeBox("colr", []byte("nclx"), u16(1), u16(13), u16(6), []byte{0x80})
	}
	props := [][]byte{
		MakeBox("hvcC", img.Stream.Configuration),
		FullBox("ispe", 0, 0, u32(uint32(img.TileWidth)), u32(uint32(img.TileHeight))),
		FullBox("pixi", 0, 0, []byte{3, d, d, d}),
		colr,
	}
	const hvcC, tileSize, pixi, colrIdx = 1, 2, 3, 4
	type assoc struct {
		item  int
		props []propRef
	}
	var as []assoc
	if img.IsGrid() {
		props = append(props, FullBox("ispe", 0, 0, u32(uint32(img.Width)), u32(uint32(img.Height))))
		as = append(as, assoc{heifPrimary, []propRef{{len(props), false}, {pixi, false}, {colrIdx, false}}})
		for _, id := range tileIDs {
			as = append(as, assoc{id, []propRef{{hvcC, true}, {tileSize, false}}})
		}
	} else {
		as = append(as, assoc{heifPrimary, []propRef{{hvcC, true}, {tileSize, false}, {pixi, false}, {colrIdx, false}}})
	}
	ipma := u32(uint32(len(as)))
	for _, a := range as {
		ipma = append(ipma, u16(a.item)...)
		ipma = append(ipma, byte(len(a.props)))
		for _, p := range a.props {
			v := byte(p.index)
			if p.essential {
				v |= 0x80
			}
			ipma = append(ipma, v)
		}
	}
	return MakeBox("iprp", MakeBox("ipco", props...), FullBox("ipma", 0, 0, ipma))
}

// gridDescription is the ImageGrid: rows, columns and the size the tiles are cropped to.
func gridDescription(img *HeifImage) []byte {
	wide := img.Width > 0xffff || img.Height > 0xffff
	b := []byte{0, 0, byte(img.Rows - 1), byte(img.Columns - 1)}
	if wide {
		b[1] = 1
		return append(append(b, u32(uint32(img.Width))...), u32(uint32(img.Height))...)
	}
	return append(append(b, u16(img.Width)...), u16(img.Height)...)
}

// ExifPayload is the Exif item: the offset of the TIFF header in the data, then the data.
func ExifPayload(exif []byte) []byte {
	if exif == nil {
		return nil
	}
	tiff := -1
	for i := 0; i+3 < len(exif) && i < 16 && tiff < 0; i++ {
		if (exif[i] == 'I' && exif[i+1] == 'I' && exif[i+2] == 42 && exif[i+3] == 0) ||
			(exif[i] == 'M' && exif[i+1] == 'M' && exif[i+2] == 0 && exif[i+3] == 42) {
			tiff = i
		}
	}
	if tiff < 0 {
		return nil
	}
	return append(binary.BigEndian.AppendUint32(nil, uint32(tiff)), exif...)
}
