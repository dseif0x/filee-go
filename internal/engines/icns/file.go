package icns

import (
	"bytes"
	"encoding/binary"
	"errors"
	"slices"
)

// Apple icon files (.icns): a list of typed entries, each holding one icon
// size as PNG, JPEG 2000 or (in old files) run-length encoded RGB plus a
// separate 8-bit mask.

// Entry is one entry of an .icns file: its four-character type and payload.
type Entry struct {
	Type string
	Data []byte
}

// Bitmap is a decoded icon: straight RGBA pixels, row by row.
type Bitmap struct {
	Width, Height int
	RGBA          []byte
}

// WrittenTypes are the entry types written for a new icon, smallest first,
// with their pixel size (iconutil's set).
var WrittenTypes = []struct {
	Type string
	Size int
}{
	{"icp4", 16},
	{"ic11", 32}, // 16 pt @2x
	{"icp5", 32},
	{"ic12", 64}, // 32 pt @2x
	{"ic07", 128},
	{"ic13", 256}, // 128 pt @2x
	{"ic08", 256},
	{"ic14", 512}, // 256 pt @2x
	{"ic09", 512},
	{"ic10", 1024}, // 512 pt @2x
}

// imageTypes are the pixel sizes of the types carrying PNG or JPEG 2000 data (or ARGB for ic04/ic05).
var imageTypes = map[string]int{
	"icp4": 16, "icp5": 32, "icp6": 64, "ic07": 128, "ic08": 256, "ic09": 512, "ic10": 1024,
	"ic11": 32, "ic12": 64, "ic13": 256, "ic14": 512, "ic04": 16, "ic05": 32,
	"icsb": 18, "icsB": 36, "sb24": 24, "SB24": 48,
}

// legacyTypes are the RLE RGB types, their size and the type of their 8-bit mask.
var legacyTypes = map[string]struct {
	Size int
	Mask string
}{
	"is32": {16, "s8mk"}, "il32": {32, "l8mk"}, "ih32": {48, "h8mk"}, "it32": {128, "t8mk"},
}

var (
	pngSignature = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	jp2Signature = []byte{0, 0, 0, 0x0C, 'j', 'P', 0x20, 0x20}
	j2kSignature = []byte{0xFF, 0x4F, 0xFF, 0x51}
)

// Parse reads the top-level entries of an .icns file; a truncated or corrupt
// file keeps what was readable.
func Parse(file []byte) ([]Entry, error) {
	if len(file) < 8 || string(file[:4]) != "icns" {
		return nil, errors.New("not an ICNS file (the 'icns' signature is missing)")
	}
	end := int(min(uint64(binary.BigEndian.Uint32(file[4:])), uint64(len(file))))
	var entries []Entry
	for off := 8; off+8 <= end; {
		typ := string(file[off : off+4])
		n := int(binary.BigEndian.Uint32(file[off+4:]))
		if n < 8 || off+n > end {
			break
		}
		entries = append(entries, Entry{typ, bytes.Clone(file[off+8 : off+n])})
		off += n
	}
	return entries, nil
}

// Write writes entries, preceded by a table of contents as iconutil does.
func Write(entries []Entry) []byte {
	toc := 8 + 8*len(entries)
	total := 8 + toc
	for _, e := range entries {
		total += 8 + len(e.Data)
	}
	out := make([]byte, 0, total)
	be := binary.BigEndian
	out = append(out, "icns"...)
	out = be.AppendUint32(out, uint32(total))
	out = append(out, "TOC "...)
	out = be.AppendUint32(out, uint32(toc))
	for _, e := range entries {
		out = append(out, e.Type...)
		out = be.AppendUint32(out, uint32(8+len(e.Data)))
	}
	for _, e := range entries {
		out = append(out, e.Type...)
		out = be.AppendUint32(out, uint32(8+len(e.Data)))
		out = append(out, e.Data...)
	}
	return out
}

// Candidate is an icon picture in the file, not decoded yet: PNG or JPEG 2000
// bytes (Encoded) or a decoder for legacy / ARGB data.
type Candidate struct {
	Type    string
	Size    int
	Encoded []byte
	Decode  func() (*Bitmap, error)
}

// Candidates are all pictures of the file, largest first (PNG / JPEG 2000
// before legacy data of the same size).
func Candidates(entries []Entry) []Candidate {
	byType := map[string][]byte{}
	for _, e := range entries {
		if _, ok := byType[e.Type]; !ok {
			byType[e.Type] = e.Data
		}
	}
	var out []Candidate
	for _, e := range entries {
		if size, ok := imageTypes[e.Type]; ok {
			switch {
			case IsEncodedImage(e.Data):
				out = append(out, Candidate{e.Type, size, e.Data, nil})
			case bytes.HasPrefix(e.Data, []byte("ARGB")):
				data := e.Data
				out = append(out, Candidate{e.Type, size, nil, func() (*Bitmap, error) { return DecodeARGB(data, size), nil }})
			}
		} else if legacy, ok := legacyTypes[e.Type]; ok {
			typ, data, mask := e.Type, e.Data, byType[legacy.Mask]
			out = append(out, Candidate{typ, legacy.Size, nil, func() (*Bitmap, error) { return DecodeLegacy(typ, data, mask, legacy.Size), nil }})
		}
	}
	slices.SortStableFunc(out, func(a, b Candidate) int {
		if a.Size != b.Size {
			return b.Size - a.Size
		}
		rank := func(c Candidate) int {
			if c.Encoded == nil {
				return 1
			}
			return 0
		}
		return rank(a) - rank(b)
	})
	return out
}

// IsEncodedImage is true for PNG and JPEG 2000 (JP2 container or raw codestream) data.
func IsEncodedImage(data []byte) bool {
	return bytes.HasPrefix(data, pngSignature) || bytes.HasPrefix(data, jp2Signature) || bytes.HasPrefix(data, j2kSignature)
}

// DecodeLegacy decodes is32/il32/ih32/it32: RGB stored channel after channel,
// each run-length encoded, plus an optional 8-bit mask entry as alpha. it32
// data starts with four zero bytes. Data exactly 4 × size² long is
// uncompressed ARGB, which some writers use.
func DecodeLegacy(typ string, data, mask []byte, size int) *Bitmap {
	n := size * size
	rgba := make([]byte, n*4)
	if len(data) == n*4 {
		for i := 0; i < n; i++ {
			rgba[i*4], rgba[i*4+1], rgba[i*4+2] = data[i*4+1], data[i*4+2], data[i*4+3]
		}
	} else {
		if typ == "it32" && len(data) >= 4 {
			data = data[4:]
		}
		planes := UnpackBits(data, n*3)
		for i := 0; i < n; i++ {
			rgba[i*4], rgba[i*4+1], rgba[i*4+2] = planes[i], planes[n+i], planes[2*n+i]
		}
	}
	for i := 0; i < n; i++ {
		if len(mask) >= n {
			rgba[i*4+3] = mask[i]
		} else {
			rgba[i*4+3] = 255
		}
	}
	return &Bitmap{size, size, rgba}
}

// DecodeARGB decodes ic04/ic05 "ARGB" entries: the tag, then A, R, G and B planes run-length encoded.
func DecodeARGB(data []byte, size int) *Bitmap {
	n := size * size
	planes := UnpackBits(data[4:], n*4)
	rgba := make([]byte, n*4)
	for i := 0; i < n; i++ {
		rgba[i*4], rgba[i*4+1], rgba[i*4+2], rgba[i*4+3] = planes[n+i], planes[2*n+i], planes[3*n+i], planes[i]
	}
	return &Bitmap{size, size, rgba}
}

// UnpackBits is Apple's icon RLE: a control byte below 0x80 copies the next
// n + 1 bytes, one of 0x80 or more repeats the next byte n − 125 times.
// Missing data (truncated files) stays zero.
func UnpackBits(src []byte, length int) []byte {
	out := make([]byte, length)
	w, i := 0, 0
	for w < length && i < len(src) {
		c := int(src[i])
		i++
		if c < 0x80 {
			n := min(c+1, length-w, len(src)-i)
			copy(out[w:], src[i:i+n])
			i += c + 1
			w += n
			continue
		}
		if i >= len(src) {
			break
		}
		v := src[i]
		i++
		n := min(c-125, length-w)
		for k := 0; k < n; k++ {
			out[w+k] = v
		}
		w += n
	}
	return out
}

// PackBits is the encoder of UnpackBits (for legacy entries in tests and tools).
func PackBits(data []byte) []byte {
	var out []byte
	for i := 0; i < len(data); {
		run := 1
		for i+run < len(data) && run < 130 && data[i+run] == data[i] {
			run++
		}
		if run >= 3 {
			out = append(out, byte(run+125), data[i])
			i += run
			continue
		}
		lit := 0
		for i+lit < len(data) && lit < 128 {
			if i+lit+2 < len(data) && data[i+lit] == data[i+lit+1] && data[i+lit] == data[i+lit+2] {
				break
			}
			lit++
		}
		out = append(out, byte(lit-1))
		out = append(out, data[i:i+lit]...)
		i += lit
	}
	return out
}
