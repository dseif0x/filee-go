// Package dwg reads and writes DWG files (R13 to 2018, AC1012 … AC1032) to and
// from the cadoc document model. The reader ports ACadSharp's DwgReader, the
// writer its DwgWriter.
package dwg

import (
	"errors"
	"fmt"
	"hash/crc32"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// Section names (DwgSectionDefinition).
const (
	SectionAcDbObjects  = "AcDb:AcDbObjects"
	SectionAcDsProto    = "AcDb:AcDsPrototype_1b"
	SectionAppInfo      = "AcDb:AppInfo"
	SectionAuxHeader    = "AcDb:AuxHeader"
	SectionClasses      = "AcDb:Classes"
	SectionFileDepList  = "AcDb:FileDepList"
	SectionHandles      = "AcDb:Handles"
	SectionHeader       = "AcDb:Header"
	SectionObjFreeSpace = "AcDb:ObjFreeSpace"
	SectionPreview      = "AcDb:Preview"
	SectionRevHistory   = "AcDb:RevHistory"
	SectionSummaryInfo  = "AcDb:SummaryInfo"
	SectionTemplate     = "AcDb:Template"
)

// Section sentinels (DwgSectionDefinition.StartSentinels/EndSentinels and
// DwgFileHeaderAC15.EndSentinel).
var (
	SentinelHeaderStart  = [16]byte{0xCF, 0x7B, 0x1F, 0x23, 0xFD, 0xDE, 0x38, 0xA9, 0x5F, 0x7C, 0x68, 0xB8, 0x4E, 0x6D, 0x33, 0x5F}
	SentinelHeaderEnd    = [16]byte{0x30, 0x84, 0xE0, 0xDC, 0x02, 0x21, 0xC7, 0x56, 0xA0, 0x83, 0x97, 0x47, 0xB1, 0x92, 0xCC, 0xA0}
	SentinelClassesStart = [16]byte{0x8D, 0xA1, 0xC4, 0xB8, 0xC4, 0xA9, 0xF8, 0xC5, 0xC0, 0xDC, 0xF4, 0x5F, 0xE7, 0xCF, 0xB6, 0x8A}
	SentinelClassesEnd   = [16]byte{0x72, 0x5E, 0x3B, 0x47, 0x3B, 0x56, 0x07, 0x3A, 0x3F, 0x23, 0x0B, 0xA0, 0x18, 0x30, 0x49, 0x75}
	SentinelPreviewStart = [16]byte{0x1F, 0x25, 0x6D, 0x07, 0xD4, 0x36, 0x28, 0x28, 0x9D, 0x57, 0xCA, 0x3F, 0x9D, 0x44, 0x10, 0x2B}
	SentinelPreviewEnd   = [16]byte{0xE0, 0xDA, 0x92, 0xF8, 0x2B, 0xC9, 0xD7, 0xD7, 0x62, 0xA8, 0x35, 0xC0, 0x62, 0xBB, 0xEF, 0xD4}
	SentinelFileHeader15 = [16]byte{0x95, 0xA0, 0x4E, 0x28, 0x99, 0x82, 0x1A, 0xE5, 0x5E, 0x41, 0xE0, 0x5F, 0x9D, 0x3A, 0x4D, 0x00}
)

// SectionLocatorIndex is the R13-R15 section locator record number of a
// section (DwgSectionDefinition.GetSectionLocatorByName); -1 when the section
// has no locator record in those versions.
func SectionLocatorIndex(name string) int {
	switch name {
	case SectionHeader:
		return 0
	case SectionClasses:
		return 1
	case SectionHandles:
		return 2
	case SectionObjFreeSpace:
		return 3
	case SectionTemplate:
		return 4
	case SectionAuxHeader:
		return 5
	}
	return -1
}

// R2004 page types and the data section page mask.
const (
	PageTypeSectionPageMap = 0x41630e3b
	PageTypeSectionMap     = 0x4163003b
	PageTypeDataSection    = 0x4163043b
	DataSectionMaskSeed    = 0x4164536B
	AC18FileIDString       = "AcFssFcAJMB\x00"
)

// versionFeatures holds the version predicates of DwgSectionIO.
type versionFeatures struct {
	R13_14Only bool
	R13_15Only bool
	R2000Plus  bool
	R2004Pre   bool
	// R2007Pre is ACadSharp's (inclusive) version <= AC1021.
	R2007Pre  bool
	R2004Plus bool
	R2007Plus bool
	R2010Plus bool
	R2013Plus bool
	R2018Plus bool
}

func newVersionFeatures(v cadoc.Version) versionFeatures {
	return versionFeatures{
		R13_14Only: v == cadoc.AC1014 || v == cadoc.AC1012,
		R13_15Only: v >= cadoc.AC1012 && v <= cadoc.AC1015,
		R2000Plus:  v >= cadoc.AC1015,
		R2004Pre:   v < cadoc.AC1018,
		R2007Pre:   v <= cadoc.AC1021,
		R2004Plus:  v >= cadoc.AC1018,
		R2007Plus:  v >= cadoc.AC1021,
		R2010Plus:  v >= cadoc.AC1024,
		R2013Plus:  v >= cadoc.AC1027,
		R2018Plus:  v >= cadoc.AC1032,
	}
}

// ----- CRC and checksums -----

// CRC8Table is ACadSharp's CRC.CrcTable (the CRC-16/ARC table the DWG
// documentation calls "CRC8").
var CRC8Table = [256]uint16{
	0x0000, 0xC0C1, 0xC181, 0x0140, 0xC301, 0x03C0, 0x0280, 0xC241, 0xC601, 0x06C0, 0x0780, 0xC741, 0x0500, 0xC5C1, 0xC481, 0x0440,
	0xCC01, 0x0CC0, 0x0D80, 0xCD41, 0x0F00, 0xCFC1, 0xCE81, 0x0E40, 0x0A00, 0xCAC1, 0xCB81, 0x0B40, 0xC901, 0x09C0, 0x0880, 0xC841,
	0xD801, 0x18C0, 0x1980, 0xD941, 0x1B00, 0xDBC1, 0xDA81, 0x1A40, 0x1E00, 0xDEC1, 0xDF81, 0x1F40, 0xDD01, 0x1DC0, 0x1C80, 0xDC41,
	0x1400, 0xD4C1, 0xD581, 0x1540, 0xD701, 0x17C0, 0x1680, 0xD641, 0xD201, 0x12C0, 0x1380, 0xD341, 0x1100, 0xD1C1, 0xD081, 0x1040,
	0xF001, 0x30C0, 0x3180, 0xF141, 0x3300, 0xF3C1, 0xF281, 0x3240, 0x3600, 0xF6C1, 0xF781, 0x3740, 0xF501, 0x35C0, 0x3480, 0xF441,
	0x3C00, 0xFCC1, 0xFD81, 0x3D40, 0xFF01, 0x3FC0, 0x3E80, 0xFE41, 0xFA01, 0x3AC0, 0x3B80, 0xFB41, 0x3900, 0xF9C1, 0xF881, 0x3840,
	0x2800, 0xE8C1, 0xE981, 0x2940, 0xEB01, 0x2BC0, 0x2A80, 0xEA41, 0xEE01, 0x2EC0, 0x2F80, 0xEF41, 0x2D00, 0xEDC1, 0xEC81, 0x2C40,
	0xE401, 0x24C0, 0x2580, 0xE541, 0x2700, 0xE7C1, 0xE681, 0x2640, 0x2200, 0xE2C1, 0xE381, 0x2340, 0xE101, 0x21C0, 0x2080, 0xE041,
	0xA001, 0x60C0, 0x6180, 0xA141, 0x6300, 0xA3C1, 0xA281, 0x6240, 0x6600, 0xA6C1, 0xA781, 0x6740, 0xA501, 0x65C0, 0x6480, 0xA441,
	0x6C00, 0xACC1, 0xAD81, 0x6D40, 0xAF01, 0x6FC0, 0x6E80, 0xAE41, 0xAA01, 0x6AC0, 0x6B80, 0xAB41, 0x6900, 0xA9C1, 0xA881, 0x6840,
	0x7800, 0xB8C1, 0xB981, 0x7940, 0xBB01, 0x7BC0, 0x7A80, 0xBA41, 0xBE01, 0x7EC0, 0x7F80, 0xBF41, 0x7D00, 0xBDC1, 0xBC81, 0x7C40,
	0xB401, 0x74C0, 0x7580, 0xB541, 0x7700, 0xB7C1, 0xB681, 0x7640, 0x7200, 0xB2C1, 0xB381, 0x7340, 0xB101, 0x71C0, 0x7080, 0xB041,
	0x5000, 0x90C1, 0x9181, 0x5140, 0x9301, 0x53C0, 0x5280, 0x9241, 0x9601, 0x56C0, 0x5780, 0x9741, 0x5500, 0x95C1, 0x9481, 0x5440,
	0x9C01, 0x5CC0, 0x5D80, 0x9D41, 0x5F00, 0x9FC1, 0x9E81, 0x5E40, 0x5A00, 0x9AC1, 0x9B81, 0x5B40, 0x9901, 0x59C0, 0x5880, 0x9841,
	0x8801, 0x48C0, 0x4980, 0x8941, 0x4B00, 0x8BC1, 0x8A81, 0x4A40, 0x4E00, 0x8EC1, 0x8F81, 0x4F40, 0x8D01, 0x4DC0, 0x4C80, 0x8C41,
	0x4400, 0x84C1, 0x8581, 0x4540, 0x8701, 0x47C0, 0x4680, 0x8641, 0x8201, 0x42C0, 0x4380, 0x8341, 0x4100, 0x81C1, 0x8081, 0x4040,
}

// CRC8 continues the DWG "CRC8" (CRC8StreamHandler) over p.
func CRC8(seed uint16, p []byte) uint16 {
	for _, b := range p {
		seed = seed>>8 ^ CRC8Table[b^byte(seed)]
	}
	return seed
}

// CRC32 continues the DWG CRC32 (CRC32StreamHandler: the IEEE CRC with a
// complemented seed, which is what hash/crc32 implements) over p.
func CRC32(seed uint32, p []byte) uint32 { return crc32.Update(seed, crc32.IEEETable, p) }

// MagicSequence is DwgCheckSumCalculator.MagicSequence: the pseudo random
// bytes used to encrypt the R2004 file header and pad sections.
var MagicSequence = func() (m [256]byte) {
	seed := int32(1)
	for i := range m {
		seed = seed*0x343FD + 0x269EC3
		m[i] = byte(seed >> 16)
	}
	return m
}()

// CompressionPadding is DwgCheckSumCalculator.CompressionCalculator.
func CompressionPadding(length int) int { return 0x1F - (length+0x20-1)%0x20 }

// Checksum is the R2004 section page checksum (DwgCheckSumCalculator.Calculate).
func Checksum(seed uint32, p []byte) uint32 {
	sum1 := seed & 0xFFFF
	sum2 := seed >> 16
	for len(p) > 0 {
		n := min(0x15B0, len(p))
		for _, b := range p[:n] {
			sum1 += uint32(b)
			sum2 += sum1
		}
		p = p[n:]
		sum1 %= 0xFFF1
		sum2 %= 0xFFF1
	}
	return sum2<<16 | sum1&0xFFFF
}

// DecryptAC18Header undoes the XOR "encryption" of the 0x6C byte R2004 file
// header block in place (the byte array constructor of CRC32StreamHandler).
func DecryptAC18Header(p []byte) {
	seed := int32(1)
	for i := range p {
		seed = seed*0x343fd + 0x269ec3
		p[i] ^= byte(seed >> 0x10)
	}
}

// ReedSolomonDeinterleave copies the data bytes of an R2007 Reed-Solomon
// encoded block into buffer (DwgReader.reedSolomonDecoding: the parity bytes
// are dropped, no error correction is done). It panics like the C# indexing
// when encoded is too short.
func ReedSolomonDeinterleave(encoded, buffer []byte, factor, blockSize int) {
	index := 0
	n := 0
	length := len(buffer)
	for i := 0; i < factor; i++ {
		cindex := n
		if n < len(encoded) {
			size := min(length, blockSize)
			length -= size
			offset := index + size
			for index < offset {
				buffer[index] = encoded[cindex]
				index++
				cindex += factor
			}
		}
		n++
	}
}

// ----- errors -----

// errEndOfStream is raised (as a panic, caught per object or section) when a
// read runs past the data, like C#'s EndOfStreamException.
var errEndOfStream = errors.New("dwg: unexpected end of stream")

// maxAlloc bounds the buffers sized from file data (a section, a page, an
// array), so damaged files fail instead of exhausting memory.
const maxAlloc = 1 << 30

// checkAlloc panics when n elements can't be right for a buffer.
func checkAlloc(n uint64) {
	if n > maxAlloc {
		panic(errorf("invalid size %d", n))
	}
}

// errorf builds a dwg error; readers panic with it and recover it at object
// or section level.
func errorf(format string, args ...any) error { return fmt.Errorf("dwg: "+format, args...) }

// ----- LZ77 AC18 (R2004) -----

// lz77Src is a byte source that, like a C# Stream, gives -1 at its end.
type lz77Src struct {
	data []byte
	pos  int
}

func (s *lz77Src) readByte() int {
	if s.pos >= len(s.data) {
		return -1
	}
	b := s.data[s.pos]
	s.pos++
	return int(b)
}

// DecompressAC18 decompresses one R2004 LZ77 stream starting at src[pos],
// appending to dst (DwgLZ77AC18Decompressor.DecompressToDest). It returns the
// grown dst and the position after the terminating 0x11 opcode.
func DecompressAC18(src []byte, pos int, dst []byte) (out []byte, next int, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("dwg: LZ77 AC18 decompression failed: %v", p)
		}
	}()
	s := &lz77Src{data: src, pos: pos}
	copyLit := func(count int) int {
		if count < 0 || s.pos+count > len(s.data) {
			panic(errEndOfStream)
		}
		dst = append(dst, s.data[s.pos:s.pos+count]...)
		s.pos += count
		return int(byte(s.readByte()))
	}
	literalCount := func(code int) int {
		lowbits := code & 0b1111
		if lowbits == 0 {
			var last byte
			for last = byte(s.readByte()); last == 0; last = byte(s.readByte()) {
				lowbits += 0xFF
			}
			lowbits += 0xF + int(last)
		}
		return lowbits
	}
	readCompressedBytes := func(opcode1, validBits int) int {
		n := opcode1 & validBits
		if n == 0 {
			var last byte
			for last = byte(s.readByte()); last == 0; last = byte(s.readByte()) {
				n += 0xFF
			}
			n += int(last) + validBits
		}
		return n + 2
	}
	twoByteOffset := func(offset *int, added int) int {
		first := s.readByte()
		*offset |= first >> 2
		*offset |= s.readByte() << 6
		*offset += added
		return first
	}

	opcode1 := int(byte(s.readByte()))
	if opcode1 > 0x11 {
		opcode1 = copyLit(opcode1 - 17)
	}
	if opcode1&0xF0 == 0 {
		opcode1 = copyLit(literalCount(opcode1) + 3)
	}
	for opcode1 != 0x11 {
		compOffset := 0
		compressedBytes := 0
		switch {
		case opcode1 < 0x10 || opcode1 >= 0x40:
			compressedBytes = (opcode1 >> 4) - 1
			opcode2 := int(byte(s.readByte()))
			compOffset = (opcode1>>2&3 | opcode2<<2) + 1
		case opcode1 < 0x20:
			compressedBytes = readCompressedBytes(opcode1, 0b0111)
			compOffset = (opcode1 & 8) << 11
			opcode1 = twoByteOffset(&compOffset, 0x4000)
		default:
			compressedBytes = readCompressedBytes(opcode1, 0b00011111)
			opcode1 = twoByteOffset(&compOffset, 1)
		}
		start := len(dst) - compOffset
		if start < 0 || compOffset <= 0 {
			panic(errorf("invalid LZ77 back reference %d at %d", compOffset, len(dst)))
		}
		if len(dst)+compressedBytes > maxAlloc {
			panic(errorf("LZ77 output too large"))
		}
		// Overlapping copies repeat the compOffset long pattern.
		for compressedBytes > 0 {
			n := min(compressedBytes, compOffset)
			dst = append(dst, dst[start:start+n]...)
			compressedBytes -= compOffset
		}
		litCount := opcode1 & 3
		if litCount == 0 {
			opcode1 = int(byte(s.readByte()))
			if opcode1&0b11110000 == 0 {
				litCount = literalCount(opcode1) + 3
			}
		}
		if litCount > 0 {
			opcode1 = copyLit(litCount)
		}
	}
	return dst, s.pos, nil
}

// ----- LZ77 AC21 (R2007) -----

// lz77AC21 is DwgLZ77AC21Decompressor.
type lz77AC21 struct {
	length       uint32
	opCode       uint32
	sourceIndex  uint32
	sourceOffset uint32
}

// DecompressAC21 decompresses length bytes of source starting at
// initialOffset into buffer (DwgLZ77AC21Decompressor.Decompress).
func DecompressAC21(source []byte, initialOffset, length uint32, buffer []byte) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("dwg: LZ77 AC21 decompression failed: %v", p)
		}
	}()
	d := &lz77AC21{}
	d.decompress(source, initialOffset, length, buffer)
	return nil
}

func (d *lz77AC21) decompress(source []byte, initialOffset, length uint32, buffer []byte) {
	d.sourceOffset = 0
	d.length = 0
	d.sourceIndex = initialOffset
	d.opCode = uint32(source[d.sourceIndex])
	var destIndex uint32
	endIndex := d.sourceIndex + length
	d.sourceIndex++
	if d.sourceIndex >= endIndex {
		return
	}
	if d.opCode&240 == 32 {
		d.sourceIndex += 3
		d.length = uint32(source[d.sourceIndex-1])
		d.length &= 7
	}
	for d.sourceIndex < endIndex {
		d.nextIndex(source, buffer, &destIndex)
		if d.sourceIndex >= endIndex {
			break
		}
		destIndex = d.copyDecompressedChunks(source, endIndex, buffer, destIndex)
	}
}

func ac21Copy1(src []byte, si uint32, dst []byte, di uint32) { dst[di] = src[si] }
func ac21Copy2(src []byte, si uint32, dst []byte, di uint32) {
	dst[di] = src[si+1]
	dst[di+1] = src[si]
}
func ac21Copy3(src []byte, si uint32, dst []byte, di uint32) {
	dst[di] = src[si+2]
	dst[di+1] = src[si+1]
	dst[di+2] = src[si]
}
func ac21Copy4(src []byte, si uint32, dst []byte, di uint32) {
	dst[di] = src[si]
	dst[di+1] = src[si+1]
	dst[di+2] = src[si+2]
	dst[di+3] = src[si+3]
}
func ac21Copy8(src []byte, si uint32, dst []byte, di uint32) {
	ac21Copy4(src, si, dst, di)
	ac21Copy4(src, si+4, dst, di+4)
}
func ac21Copy16(src []byte, si uint32, dst []byte, di uint32) {
	ac21Copy8(src, si+8, dst, di)
	ac21Copy8(src, si, dst, di+8)
}

// ac21CopyMethods are the literal copy routines for 1..31 bytes; the byte
// order within a run is the AC21 format's peculiar shuffle.
var ac21CopyMethods = [32]func(src []byte, si uint32, dst []byte, di uint32){
	nil,
	ac21Copy1,
	ac21Copy2,
	ac21Copy3,
	ac21Copy4,
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy1(src, si+4, dst, di)
		ac21Copy4(src, si, dst, di+1)
	},
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy1(src, si+5, dst, di)
		ac21Copy4(src, si+1, dst, di+1)
		ac21Copy1(src, si, dst, di+5)
	},
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy2(src, si+5, dst, di)
		ac21Copy4(src, si+1, dst, di+2)
		ac21Copy1(src, si, dst, di+6)
	},
	ac21Copy8,
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy1(src, si+8, dst, di)
		ac21Copy8(src, si, dst, di+1)
	},
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy1(src, si+9, dst, di)
		ac21Copy8(src, si+1, dst, di+1)
		ac21Copy1(src, si, dst, di+9)
	},
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy2(src, si+9, dst, di)
		ac21Copy8(src, si+1, dst, di+2)
		ac21Copy1(src, si, dst, di+10)
	},
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy4(src, si+8, dst, di)
		ac21Copy8(src, si, dst, di+4)
	},
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy1(src, si+12, dst, di)
		ac21Copy4(src, si+8, dst, di+1)
		ac21Copy8(src, si, dst, di+5)
	},
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy1(src, si+13, dst, di)
		ac21Copy4(src, si+9, dst, di+1)
		ac21Copy8(src, si+1, dst, di+5)
		ac21Copy1(src, si, dst, di+13)
	},
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy2(src, si+13, dst, di)
		ac21Copy4(src, si+9, dst, di+2)
		ac21Copy8(src, si+1, dst, di+6)
		ac21Copy1(src, si, dst, di+14)
	},
	ac21Copy16,
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy8(src, si+9, dst, di)
		ac21Copy1(src, si+8, dst, di+8)
		ac21Copy8(src, si, dst, di+9)
	},
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy1(src, si+17, dst, di)
		ac21Copy16(src, si+1, dst, di+1)
		ac21Copy1(src, si, dst, di+17)
	},
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy3(src, si+16, dst, di)
		ac21Copy16(src, si, dst, di+3)
	},
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy4(src, si+16, dst, di)
		ac21Copy8(src, si+8, dst, di+4)
		ac21Copy8(src, si, dst, di+12)
	},
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy1(src, si+20, dst, di)
		ac21Copy4(src, si+16, dst, di+1)
		ac21Copy8(src, si+8, dst, di+5)
		ac21Copy8(src, si, dst, di+13)
	},
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy2(src, si+20, dst, di)
		ac21Copy4(src, si+16, dst, di+2)
		ac21Copy8(src, si+8, dst, di+6)
		ac21Copy8(src, si, dst, di+14)
	},
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy3(src, si+20, dst, di)
		ac21Copy4(src, si+16, dst, di+3)
		ac21Copy8(src, si+8, dst, di+7)
		ac21Copy8(src, si, dst, di+15)
	},
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy8(src, si+16, dst, di)
		ac21Copy16(src, si, dst, di+8)
	},
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy8(src, si+17, dst, di)
		ac21Copy1(src, si+16, dst, di+8)
		ac21Copy16(src, si, dst, di+9)
	},
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy1(src, si+25, dst, di)
		ac21Copy8(src, si+17, dst, di+1)
		ac21Copy1(src, si+16, dst, di+9)
		ac21Copy16(src, si, dst, di+10)
	},
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy2(src, si+25, dst, di)
		ac21Copy8(src, si+17, dst, di+2)
		ac21Copy1(src, si+16, dst, di+10)
		ac21Copy16(src, si, dst, di+11)
	},
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy4(src, si+24, dst, di)
		ac21Copy8(src, si+16, dst, di+4)
		ac21Copy8(src, si+8, dst, di+12)
		ac21Copy8(src, si, dst, di+20)
	},
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy1(src, si+28, dst, di)
		ac21Copy4(src, si+24, dst, di+1)
		ac21Copy8(src, si+16, dst, di+5)
		ac21Copy8(src, si+8, dst, di+13)
		ac21Copy8(src, si, dst, di+21)
	},
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy2(src, si+28, dst, di)
		ac21Copy4(src, si+24, dst, di+2)
		ac21Copy8(src, si+16, dst, di+6)
		ac21Copy8(src, si+8, dst, di+14)
		ac21Copy8(src, si, dst, di+22)
	},
	func(src []byte, si uint32, dst []byte, di uint32) {
		ac21Copy1(src, si+30, dst, di)
		ac21Copy4(src, si+26, dst, di+1)
		ac21Copy8(src, si+18, dst, di+5)
		ac21Copy8(src, si+10, dst, di+13)
		ac21Copy8(src, si+2, dst, di+21)
		ac21Copy2(src, si, dst, di+29)
	},
}

func (d *lz77AC21) copyLiteral(src []byte, si uint32, dst []byte, di uint32, length uint32) {
	for ; length >= 32; length -= 32 {
		ac21Copy4(src, si+24, dst, di)
		ac21Copy4(src, si+28, dst, di+4)
		ac21Copy4(src, si+16, dst, di+8)
		ac21Copy4(src, si+20, dst, di+12)
		ac21Copy4(src, si+8, dst, di+16)
		ac21Copy4(src, si+12, dst, di+20)
		ac21Copy4(src, si, dst, di+24)
		ac21Copy4(src, si+4, dst, di+28)
		si += 32
		di += 32
	}
	if length == 0 {
		return
	}
	ac21CopyMethods[length](src, si, dst, di)
}

func (d *lz77AC21) copyBytes(dst []byte, di, length, srcOffset uint32) {
	initial := di - srcOffset
	maxIndex := initial + length
	for initial < maxIndex {
		dst[di] = dst[initial]
		di++
		initial++
	}
}

func (d *lz77AC21) copyDecompressedChunks(src []byte, endIndex uint32, dst []byte, destIndex uint32) uint32 {
	d.length = 0
	d.opCode = uint32(src[d.sourceIndex])
	d.sourceIndex++
	d.readInstructions(src)
	for {
		d.copyBytes(dst, destIndex, d.length, d.sourceOffset)
		destIndex += d.length
		d.length = d.opCode & 0x07
		if d.length != 0 || d.sourceIndex >= endIndex {
			break
		}
		d.opCode = uint32(src[d.sourceIndex])
		d.sourceIndex++
		if d.opCode>>4 == 0 {
			break
		}
		if d.opCode>>4 == 15 {
			d.opCode &= 15
		}
		d.readInstructions(src)
	}
	return destIndex
}

func (d *lz77AC21) nextIndex(source, dest []byte, index *uint32) {
	if d.length == 0 {
		d.readLiteralLength(source)
	}
	d.copyLiteral(source, d.sourceIndex, dest, *index, d.length)
	d.sourceIndex += d.length
	*index += d.length
}

func (d *lz77AC21) readInstructions(buf []byte) {
	switch d.opCode >> 4 {
	case 0:
		d.length = d.opCode&0xF + 0x13
		d.sourceOffset = uint32(buf[d.sourceIndex])
		d.sourceIndex++
		d.opCode = uint32(buf[d.sourceIndex])
		d.sourceIndex++
		d.length = d.opCode>>3&0x10 + d.length
		d.sourceOffset = (d.opCode&0x78)<<5 + 1 + d.sourceOffset
	case 1:
		d.length = d.opCode&0xF + 3
		d.sourceOffset = uint32(buf[d.sourceIndex])
		d.sourceIndex++
		d.opCode = uint32(buf[d.sourceIndex])
		d.sourceIndex++
		d.sourceOffset = (d.opCode&248)<<5 + 1 + d.sourceOffset
	case 2:
		d.sourceOffset = uint32(buf[d.sourceIndex])
		d.sourceIndex++
		d.sourceOffset = uint32(buf[d.sourceIndex])<<8&0xFF00 | d.sourceOffset
		d.sourceIndex++
		d.length = d.opCode & 7
		if d.opCode&8 == 0 {
			d.opCode = uint32(buf[d.sourceIndex])
			d.sourceIndex++
			d.length = d.opCode&0xF8 + d.length
		} else {
			d.sourceOffset++
			d.length = uint32(buf[d.sourceIndex])<<3 + d.length
			d.sourceIndex++
			d.opCode = uint32(buf[d.sourceIndex])
			d.sourceIndex++
			d.length = (d.opCode&0xF8)<<8 + d.length + 0x100
		}
	default:
		d.length = d.opCode >> 4
		d.sourceOffset = d.opCode & 15
		d.opCode = uint32(buf[d.sourceIndex])
		d.sourceIndex++
		d.sourceOffset = (d.opCode&0xF8)<<1 + d.sourceOffset + 1
	}
}

func (d *lz77AC21) readLiteralLength(buf []byte) {
	d.length = d.opCode + 8
	if d.length == 0x17 {
		n := uint32(buf[d.sourceIndex])
		d.sourceIndex++
		d.length += n
		if n == 0xFF {
			for {
				n = uint32(buf[d.sourceIndex])
				d.sourceIndex++
				n |= uint32(buf[d.sourceIndex]) << 8
				d.sourceIndex++
				d.length += n
				if n != 0xFFFF {
					break
				}
			}
		}
	}
}

// Notification levels passed to notify callbacks (ACadSharp's
// NotificationType values).
const (
	NotifyNotImplemented = -1
	NotifyNone           = 0
	NotifyNotSupported   = 1
	NotifyWarning        = 2
	NotifyError          = 3
)

// ErrNotSupported is returned for files of versions DWG IO does not handle
// (AC1009 and older, unknown signatures), like CadNotSupportedException.
var ErrNotSupported = errors.New("dwg: version not supported")
