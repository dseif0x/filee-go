package fonts

import (
	"encoding/binary"
	"unicode/utf16"
)

// Embedded OpenType (EOT, W3C Member Submission 2008), the web font format of
// Internet Explorer 6–8: a little-endian header with names and OS/2 fields in
// front of the font. Writes version 0x00020001 without MicroType Express
// compression or XOR obfuscation; reads uncompressed files of every version.

const (
	eotMagic          = 0x504C
	eotVersion        = 0x00020001
	eotCompressedFlag = 0x4        // TTEMBED_TTCOMPRESSED
	eotXorFlag        = 0x10000000 // TTEMBED_XORENCRYPTDATA
	eotDefaultCharset = 1
)

func isEot(data []byte) bool {
	if len(data) < 82 || binary.LittleEndian.Uint16(data[34:]) != eotMagic {
		return false
	}
	switch binary.LittleEndian.Uint32(data[8:]) {
	case 0x00010000, 0x00020001, 0x00020002:
		return true
	}
	return false
}

// encodeEot wraps a TrueType font in an EOT header (the caller converts CFF outlines first).
func encodeEot(font *sfnt) []byte {
	data := font.write()
	written := readSfnt(data, 0)
	os2 := written.table("OS/2")
	head := written.require("head")
	names := readNameTable(written.table("name"))

	le := binary.LittleEndian
	out := make([]byte, 0, len(data)+512)
	u16 := func(v uint16) { out = le.AppendUint16(out, v) }
	u32 := func(v uint32) { out = le.AppendUint32(out, v) }
	u32(0) // EOTSize, patched below
	u32(uint32(len(data)))
	u32(eotVersion)
	u32(0)                                   // flags: not compressed, not obfuscated, not a subset
	out = append(out, slice(os2, 32, 10)...) // PANOSE
	out = append(out, eotDefaultCharset)
	out = append(out, byte(tableU16(os2, 62)&1)) // italic: fsSelection bit 0
	u32(uint32(tableU16(os2, 4)))                // usWeightClass
	u16(tableU16(os2, 8))                        // fsType (embedding permissions)
	u16(eotMagic)
	for i := 0; i < 4; i++ {
		u32(tableU32(os2, 42+4*i)) // ulUnicodeRange1–4
	}
	for i := 0; i < 2; i++ {
		u32(tableU32(os2, 78+4*i)) // ulCodePageRange1–2
	}
	u32(tableU32(head, 8)) // checkSumAdjustment
	for i := 0; i < 4; i++ {
		u32(0) // reserved
	}
	u16(0) // padding1
	name := func(s string) {
		u := utf16.Encode([]rune(s))
		u16(uint16(2 * len(u)))
		for _, c := range u {
			u16(c)
		}
	}
	name(names.get(1)) // family
	u16(0)
	name(names.get(2)) // style
	u16(0)
	name(names.get(5)) // version
	u16(0)
	name(names.get(4)) // full name
	u16(0)             // padding5
	u16(0)             // RootStringSize: no URL restriction
	out = append(out, data...)
	le.PutUint32(out, uint32(len(out)))
	return out
}

// decodeEot extracts the font from an EOT file.
func decodeEot(data []byte) *sfnt {
	if !isEot(data) {
		fail("This is not an EOT file.")
	}
	le := binary.LittleEndian
	eotSize, fontSize, flags := le.Uint32(data), le.Uint32(data[4:]), le.Uint32(data[12:])
	if flags&eotCompressedFlag != 0 {
		fail("This EOT file is compressed with MicroType Express, which Filee can't unpack. Only uncompressed EOT files can be converted.")
	}
	if uint64(eotSize) > uint64(len(data)) || eotSize < 82 || fontSize > eotSize-82 {
		fail("The EOT file is truncated or damaged.")
	}
	// The font data is the last field of every EOT version, so its offset doesn't depend on the header version.
	font := append([]byte(nil), data[eotSize-fontSize:eotSize]...)
	if flags&eotXorFlag != 0 {
		for i := range font {
			font[i] ^= 0x50
		}
	}
	return readSfnt(font, 0)
}

func slice(t []byte, off, n int) []byte {
	if len(t) >= off+n {
		return t[off : off+n]
	}
	return make([]byte, n)
}

func tableU16(t []byte, off int) uint16 {
	if len(t) >= off+2 {
		return be16(t, off)
	}
	return 0
}

func tableU32(t []byte, off int) uint32 {
	if len(t) >= off+4 {
		return be32(t, off)
	}
	return 0
}

type nameRecord struct {
	platform, encoding, language, nameID int
	value                                string
}

// nameTable holds the strings of the 'name' table.
type nameTable struct{ records []nameRecord }

func readNameTable(name []byte) (t *nameTable) {
	t = &nameTable{}
	if len(name) < 6 {
		return t
	}
	defer func() {
		// A damaged name table only costs the names.
		if r := recover(); r != nil {
			if _, ok := r.(dataError); !ok {
				panic(r)
			}
		}
	}()
	r := newReader(name)
	r.u16() // format
	count := int(r.u16())
	stringOffset := int(r.u16())
	for i := 0; i < count; i++ {
		platform, encoding, language, nameID, length, offset := int(r.u16()), int(r.u16()), int(r.u16()), int(r.u16()), int(r.u16()), int(r.u16())
		if stringOffset+offset+length > len(name) {
			continue
		}
		b := name[stringOffset+offset : stringOffset+offset+length]
		var v string
		switch {
		case platform == 0 || platform == 3:
			u := make([]uint16, len(b)/2)
			for j := range u {
				u[j] = be16(b, 2*j)
			}
			v = string(utf16.Decode(u))
		case platform == 1 && encoding == 0:
			rs := make([]rune, len(b)) // Mac Roman; ASCII names read the same
			for j, c := range b {
				rs[j] = rune(c)
			}
			v = string(rs)
		default:
			continue
		}
		t.records = append(t.records, nameRecord{platform, encoding, language, nameID, v})
	}
	return t
}

// get returns a name, preferring Windows English (US), then any Windows or
// Unicode entry, then Macintosh.
func (t *nameTable) get(id int) string {
	var candidates []nameRecord
	for _, r := range t.records {
		if r.nameID == id {
			candidates = append(candidates, r)
		}
	}
	for _, pick := range []func(nameRecord) bool{
		func(r nameRecord) bool { return r.platform == 3 && r.language == 0x409 },
		func(r nameRecord) bool { return r.platform == 3 },
		func(r nameRecord) bool { return r.platform == 0 },
		func(nameRecord) bool { return true },
	} {
		for _, r := range candidates {
			if pick(r) {
				return r.value
			}
		}
	}
	return ""
}
