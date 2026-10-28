package dwg

import (
	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// Section names (DwgSectionDefinition).
const (
	writerSecAcDbObjects  = "AcDb:AcDbObjects"
	writerSecAppInfo      = "AcDb:AppInfo"
	writerSecAuxHeader    = "AcDb:AuxHeader"
	writerSecClasses      = "AcDb:Classes"
	writerSecFileDepList  = "AcDb:FileDepList"
	writerSecHandles      = "AcDb:Handles"
	writerSecHeader       = "AcDb:Header"
	writerSecObjFreeSpace = "AcDb:ObjFreeSpace"
	writerSecPreview      = "AcDb:Preview"
	writerSecRevHistory   = "AcDb:RevHistory"
	writerSecSummaryInfo  = "AcDb:SummaryInfo"
	writerSecTemplate     = "AcDb:Template"
)

// Section sentinels (DwgSectionDefinition.StartSentinels / EndSentinels).
var (
	writerHeaderStartSentinel  = []byte{0xCF, 0x7B, 0x1F, 0x23, 0xFD, 0xDE, 0x38, 0xA9, 0x5F, 0x7C, 0x68, 0xB8, 0x4E, 0x6D, 0x33, 0x5F}
	writerHeaderEndSentinel    = []byte{0x30, 0x84, 0xE0, 0xDC, 0x02, 0x21, 0xC7, 0x56, 0xA0, 0x83, 0x97, 0x47, 0xB1, 0x92, 0xCC, 0xA0}
	writerClassesStartSentinel = []byte{0x8D, 0xA1, 0xC4, 0xB8, 0xC4, 0xA9, 0xF8, 0xC5, 0xC0, 0xDC, 0xF4, 0x5F, 0xE7, 0xCF, 0xB6, 0x8A}
	writerClassesEndSentinel   = []byte{0x72, 0x5E, 0x3B, 0x47, 0x3B, 0x56, 0x07, 0x3A, 0x3F, 0x23, 0x0B, 0xA0, 0x18, 0x30, 0x49, 0x75}
	writerPreviewStartSentinel = []byte{0x1F, 0x25, 0x6D, 0x07, 0xD4, 0x36, 0x28, 0x28, 0x9D, 0x57, 0xCA, 0x3F, 0x9D, 0x44, 0x10, 0x2B}
	writerPreviewEndSentinel   = []byte{0xE0, 0xDA, 0x92, 0xF8, 0x2B, 0xC9, 0xD7, 0xD7, 0x62, 0xA8, 0x35, 0xC0, 0x62, 0xBB, 0xEF, 0xD4}
	writerAC15EndSentinel      = []byte{0x95, 0xA0, 0x4E, 0x28, 0x99, 0x82, 0x1A, 0xE5, 0x5E, 0x41, 0xE0, 0x5F, 0x9D, 0x3A, 0x4D, 0x00}
)

// wFileHeaderWriter is IDwgFileHeaderWriter.
type wFileHeaderWriter interface {
	handleSectionOffset() int
	addSection(name string, data []byte, compressed bool, decompSize int)
	writeFile()
}

// ----- AC1014/AC1015 -----

type wSectionRecord struct {
	number int // -1: no locator record
	seeker int64
	data   []byte
}

// wFileHeaderAC15 is DwgFileHeaderWriterAC15: the sections are written one
// after the other behind a 0x61 byte header with the section locators.
type wFileHeaderAC15 struct {
	out      *wMemStream
	doc      *cadoc.Document
	version  cadoc.Version
	order    []string
	records  map[string]*wSectionRecord
	codePage byte
}

const writerAC15HeaderSize = 0x61

func newWFileHeaderAC15(out *wMemStream, doc *cadoc.Document, codePage byte) *wFileHeaderAC15 {
	h := &wFileHeaderAC15{out: out, doc: doc, version: doc.Header.Version, codePage: codePage,
		records: map[string]*wSectionRecord{}}
	for _, r := range []struct {
		name string
		n    int
	}{{writerSecAuxHeader, 5}, {writerSecPreview, -1}, {writerSecHeader, 0}, {writerSecClasses, 1},
		{writerSecAcDbObjects, -1}, {writerSecHandles, 2}, {writerSecObjFreeSpace, 3}, {writerSecTemplate, 4}} {
		h.order = append(h.order, r.name)
		h.records[r.name] = &wSectionRecord{number: r.n}
	}
	return h
}

func (h *wFileHeaderAC15) handleSectionOffset() int {
	return writerAC15HeaderSize + len(h.records[writerSecAuxHeader].data) + len(h.records[writerSecPreview].data) +
		len(h.records[writerSecHeader].data) + len(h.records[writerSecClasses].data)
}

func (h *wFileHeaderAC15) addSection(name string, data []byte, _ bool, _ int) {
	h.records[name].data = data
}

func (h *wFileHeaderAC15) writeFile() {
	off := int64(writerAC15HeaderSize)
	for _, n := range h.order {
		r := h.records[n]
		r.seeker = off
		off += int64(len(r.data))
	}

	w := newWBits(h.version, &wMemStream{}, nil)
	w.writeBytes([]byte(h.version.String())[:6])
	// Six zero bytes (R14: five and $ACADMAINTVER) and a 1.
	w.writeBytes([]byte{0, 0, 0, 0, 0, 15, 1})
	// 0x0D: seeker of the preview image.
	w.writeRawLong(h.records[writerSecPreview].seeker)
	w.writeByte(0x1B)
	w.writeByte(0x19)
	// 0x13: code page.
	w.writeBytes([]byte{h.codePage, 0})
	w.writeBytes([]byte{6, 0, 0, 0})
	for _, n := range []string{writerSecHeader, writerSecClasses, writerSecHandles, writerSecObjFreeSpace,
		writerSecTemplate, writerSecAuxHeader} {
		r := h.records[n]
		w.writeByte(byte(r.number))
		w.writeRawLong(r.seeker)
		w.writeRawLong(int64(len(r.data)))
	}
	w.writeSpearShift()
	w.writeRawShort(int16(wCRC8(0xC0C1, w.ms.bytes())))
	w.writeBytes(writerAC15EndSentinel)
	h.out.write(w.ms.bytes())
	for _, n := range h.order {
		h.out.write(h.records[n].data)
	}
}

// ----- AC1018+ -----

const writerDataPageType = 0x4163043B

type wLocalSectionMap struct {
	compression      int32
	offset           uint64
	compressedSize   uint64
	pageNumber       int32
	decompressedSize uint64
	seeker           int64
	size             int64
	checksum         uint32
	pageSize         int64
	oda              uint32
	sectionMap       int32
}

type wSectionDescriptor struct {
	name             string
	compressedSize   uint64
	pageCount        int32
	decompressedSize uint64
	compressedCode   int32
	sectionID        int32
	encrypted        int32
	localSections    []*wLocalSectionMap
}

// wFileHeaderAC18 is DwgFileHeaderWriterAC18: every section is split into
// pages (LZ77 compressed or not) written as they are added, followed by the
// section map, the page map and the encrypted file header, which is
// repeated at the end of the file.
type wFileHeaderAC18 struct {
	out         *wMemStream
	doc         *cadoc.Document
	version     cadoc.Version
	codePage    byte
	descriptors []*wSectionDescriptor
	byName      map[string]*wSectionDescriptor
	localMaps   []*wLocalSectionMap

	sectionArrayPageSize uint32
	sectionPageMapID     uint32
	sectionMapID         uint32
	gapAmount            uint32
	lastPageID           int32
	lastSectionAddr      uint64
	sectionAmount        uint32
	pageMapAddress       uint64
	secondHeaderAddr     uint64
}

const writerAC18HeaderSize = 0x100

func newWFileHeaderAC18(out *wMemStream, doc *cadoc.Document, codePage byte) *wFileHeaderAC18 {
	h := &wFileHeaderAC18{out: out, doc: doc, version: doc.Header.Version, codePage: codePage,
		byName: map[string]*wSectionDescriptor{}}
	for i := 0; i < writerAC18HeaderSize; i++ {
		out.writeByte(0)
	}
	return h
}

func (h *wFileHeaderAC18) handleSectionOffset() int { return 0 }

func (h *wFileHeaderAC18) addSection(name string, data []byte, compressed bool, decompSize int) {
	d := &wSectionDescriptor{name: name, decompressedSize: uint64(decompSize), compressedSize: uint64(len(data)),
		compressedCode: 2}
	if !compressed {
		d.compressedCode = 1
	}
	h.descriptors = append(h.descriptors, d)
	h.byName[name] = d

	n := len(data) / decompSize
	offset := 0
	for i := 0; i < n; i++ {
		h.createLocalSection(d, data, decompSize, offset, decompSize, compressed)
		offset += decompSize
	}
	spare := len(data) % decompSize
	if spare > 0 && !wAllZero(data[offset:offset+spare]) {
		h.createLocalSection(d, data, decompSize, offset, spare, compressed)
	}
}

func wAllZero(p []byte) bool {
	for _, b := range p {
		if b != 0 {
			return false
		}
	}
	return true
}

func (h *wFileHeaderAC18) applyCompression(data []byte, decompSize, offset, total int, compressed bool) []byte {
	page := make([]byte, decompSize)
	copy(page, data[offset:offset+total])
	if !compressed {
		return page
	}
	dest := &wMemStream{}
	wCompressAC18(page, 0, decompSize, dest)
	return dest.bytes()
}

// writeMagicNumber is DwgFileHeaderWriterBase.writeMagicNumber (writes
// position%32 bytes of the magic sequence; pages are aligned, so usually
// nothing).
func (h *wFileHeaderAC18) writeMagicNumber() {
	n := h.out.pos % 0x20
	h.out.write(writerMagicSequence[:n])
}

func (h *wFileHeaderAC18) createLocalSection(d *wSectionDescriptor, data []byte, decompSize, offset, total int, compressed bool) {
	page := h.applyCompression(data, decompSize, offset, total, compressed)
	h.writeMagicNumber()
	position := int64(h.out.pos)

	m := &wLocalSectionMap{compression: 2, offset: uint64(offset), seeker: position,
		pageNumber: int32(len(h.localMaps) + 1), oda: wChecksum(0, page)}
	compressDiff := wCompressionPadding(len(page))
	m.compressedSize = uint64(len(page))
	m.decompressedSize = uint64(total)
	m.pageSize = int64(m.compressedSize) + 32 + int64(compressDiff)

	hdr := h.dataSectionHeader(d, m)
	m.checksum = wChecksum(m.oda, hdr)
	hdr = h.dataSectionHeader(d, m)
	h.applyMask(hdr)
	h.out.write(hdr)
	h.out.write(page)
	if compressed {
		h.out.write(writerMagicSequence[:compressDiff])
	} else if compressDiff != 0 {
		panic("dwg: uncompressed page not aligned")
	}
	if m.pageNumber > 0 {
		d.pageCount++
	}
	m.size = int64(h.out.pos) - position
	d.localSections = append(d.localSections, m)
	h.localMaps = append(h.localMaps, m)
}

// applyMask XORs the page header with 0x4164536B ^ position.
func (h *wFileHeaderAC18) applyMask(p []byte) {
	v := uint32(0x4164536B ^ int32(h.out.pos))
	b := []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
	for i := 0; i+4 <= len(p); i += 4 {
		for j := 0; j < 4; j++ {
			p[i+j] ^= b[j]
		}
	}
}

func (h *wFileHeaderAC18) dataSectionHeader(d *wSectionDescriptor, m *wLocalSectionMap) []byte {
	s := &wMemStream{}
	s.writeInt32(writerDataPageType)
	s.writeInt32(d.sectionID)
	s.writeInt32(int32(m.compressedSize))
	s.writeInt32(int32(m.pageSize))
	s.writeInt64(int64(m.offset))
	s.writeUint32(m.checksum)
	s.writeUint32(m.oda)
	return s.bytes()
}

func (h *wFileHeaderAC18) writeFile() {
	h.sectionArrayPageSize = uint32(len(h.localMaps) + 2)
	h.sectionPageMapID = h.sectionArrayPageSize
	h.sectionMapID = h.sectionArrayPageSize - 1
	h.writeDescriptors()
	h.writeRecords()
	h.writeFileMetaData()
}

func (h *wFileHeaderAC18) addSystemSection(m *wLocalSectionMap) {
	m.pageNumber = int32(len(h.localMaps) + 1)
	h.localMaps = append(h.localMaps, m)
}

// compressChecksum compresses a system page and writes it with its header.
func (h *wFileHeaderAC18) compressChecksum(m *wLocalSectionMap, data []byte) {
	m.decompressedSize = uint64(len(data))
	main := &wMemStream{}
	wCompressAC18(data, 0, len(data), main)
	m.compressedSize = uint64(main.length())
	m.checksum = wChecksum(0, h.pageHeader(m))
	m.checksum = wChecksum(m.checksum, main.bytes())
	h.out.write(h.pageHeader(m))
	h.out.write(main.bytes())
}

func (h *wFileHeaderAC18) pageHeader(m *wLocalSectionMap) []byte {
	s := &wMemStream{}
	s.writeInt32(m.sectionMap)
	s.writeInt32(int32(m.decompressedSize))
	s.writeInt32(int32(m.compressedSize))
	s.writeInt32(m.compression)
	s.writeUint32(m.checksum)
	return s.bytes()
}

func (h *wFileHeaderAC18) writeDescriptors() {
	s := &wMemStream{}
	s.writeInt32(int32(len(h.descriptors)))
	s.writeInt32(2)
	s.writeInt32(0x7400)
	s.writeInt32(0)
	s.writeInt32(int32(len(h.descriptors)))
	for _, d := range h.descriptors {
		s.writeInt64(int64(d.compressedSize))
		s.writeInt32(d.pageCount)
		s.writeInt32(int32(d.decompressedSize))
		s.writeInt32(1)
		s.writeInt32(d.compressedCode)
		s.writeInt32(d.sectionID)
		s.writeInt32(d.encrypted)
		var name [64]byte
		copy(name[:], d.name)
		s.write(name[:])
		for _, m := range d.localSections {
			if m.pageNumber > 0 {
				s.writeInt32(m.pageNumber)
				s.writeInt32(int32(m.compressedSize))
				s.writeInt64(int64(m.offset))
			}
		}
	}
	// Section map: 0x4163003B.
	holder := &wLocalSectionMap{compression: 2, sectionMap: 0x4163003B}
	h.writeMagicNumber()
	holder.seeker = int64(h.out.pos)
	h.compressChecksum(holder, s.bytes())
	count := wCompressionPadding(h.out.pos - int(holder.seeker))
	h.out.write(writerMagicSequence[:count])
	holder.size = int64(h.out.pos) - holder.seeker
	h.addSystemSection(holder)
}

func (h *wFileHeaderAC18) writeRecords() {
	h.writeMagicNumber()
	// Section page map: 0x41630E3B.
	section := &wLocalSectionMap{compression: 2, sectionMap: 0x41630E3B}
	h.addSystemSection(section)
	counter := len(h.localMaps) * 8
	section.seeker = int64(h.out.pos)
	size := counter + wCompressionPadding(counter)
	section.size = int64(size)

	s := &wMemStream{}
	for _, m := range h.localMaps {
		s.writeInt32(m.pageNumber)
		s.writeInt32(int32(m.size))
	}
	h.compressChecksum(section, s.bytes())

	last := h.localMaps[len(h.localMaps)-1]
	h.gapAmount = 0
	h.lastPageID = last.pageNumber
	h.lastSectionAddr = uint64(last.seeker + int64(size) - 256)
	h.sectionAmount = uint32(len(h.localMaps) - 1)
	h.pageMapAddress = uint64(section.seeker)
}

// fileHeaderData is the 0x6C byte R2004 file header (with its CRC32),
// XORed with the magic sequence.
func (h *wFileHeaderAC18) fileHeaderData() []byte {
	s := &wMemStream{}
	s.write([]byte("AcFssFcAJMB\x00"))
	s.writeInt32(0)
	s.writeInt32(0x6C)
	s.writeInt32(0x04)
	s.writeInt32(0) // root tree node gap
	s.writeInt32(0) // lowermost left tree node gap
	s.writeInt32(0) // lowermost right tree node gap
	s.writeInt32(1)
	s.writeInt32(h.lastPageID)
	s.writeInt64(int64(h.lastSectionAddr))
	s.writeInt64(int64(h.secondHeaderAddr))
	s.writeUint32(h.gapAmount)
	s.writeUint32(h.sectionAmount)
	s.writeInt32(0x20)
	s.writeInt32(0x80)
	s.writeInt32(0x40)
	s.writeUint32(h.sectionPageMapID)
	s.writeInt64(int64(h.pageMapAddress - 256))
	s.writeUint32(h.sectionMapID)
	s.writeUint32(h.sectionArrayPageSize)
	s.writeUint32(0) // gap array size
	crcPos := s.pos
	s.writeUint32(0)
	// The CRC covers the whole 0x6C bytes with the CRC field still zero.
	crc := wCRC32(0, s.bytes())
	s.pos = crcPos
	s.writeUint32(crc)
	b := s.bytes()
	for i := range b {
		b[i] ^= writerMagicSequence[i]
	}
	return b
}

func (h *wFileHeaderAC18) writeFileMetaData() {
	h.secondHeaderAddr = uint64(h.out.pos)
	data := h.fileHeaderData()
	h.out.write(data)

	h.out.pos = 0
	h.out.write([]byte(h.version.String())[:6])
	h.out.write(make([]byte, 5))
	h.out.writeByte(byte(h.doc.Header.MaintenanceVersion))
	h.out.writeByte(3)
	// 0x0D: preview address (page + page header size).
	h.out.writeUint32(uint32(int32(h.byName[writerSecPreview].localSections[0].seeker) + 0x20))
	// 0x11: version of the writing application; 0x12 its maintenance version.
	h.out.writeByte(33)
	h.out.writeByte(byte(h.doc.Header.MaintenanceVersion))
	h.out.writeInt16(int16(h.codePage))
	h.out.write(make([]byte, 3))
	h.out.writeInt32(0) // security type
	h.out.writeInt32(0)
	h.out.writeUint32(uint32(int32(h.byName[writerSecSummaryInfo].localSections[0].seeker) + 0x20))
	h.out.writeUint32(0) // VBA project
	h.out.writeInt32(0x80)
	h.out.writeUint32(uint32(int32(h.byName[writerSecAppInfo].localSections[0].seeker) + 0x20))
	h.out.write(make([]byte, 80))
	h.out.write(data)
	h.out.write(writerMagicSequence[236 : 236+20])
}
