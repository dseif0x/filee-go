package dwg

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// ReadConfig is DwgReaderConfiguration.
type ReadConfig struct {
	// Failsafe: an object that can't be read is skipped (with an error
	// notification) instead of failing the whole file.
	Failsafe bool
	// KeepUnknownEntities keeps entities without a model as
	// cadoc.UnknownEntity instead of dropping them.
	KeepUnknownEntities bool
	// KeepUnknownObjects keeps objects without a model as cadoc.UnknownObject
	// instead of dropping them.
	KeepUnknownObjects bool
}

// ReadFile reads a DWG file.
func ReadFile(path string, cfg ReadConfig, notify func(level int, msg string)) (*cadoc.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ReadBytes(data, cfg, notify)
}

// Read reads a DWG document from r.
func Read(r io.Reader, cfg ReadConfig, notify func(level int, msg string)) (*cadoc.Document, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return ReadBytes(data, cfg, notify)
}

// ReadBytes reads a DWG document held in memory.
func ReadBytes(data []byte, cfg ReadConfig, notify func(level int, msg string)) (doc *cadoc.Document, err error) {
	rd := &reader{cfg: cfg, notify: notify, data: data, text: utf8Decoder}
	defer func() {
		if p := recover(); p != nil {
			doc = nil
			if e, ok := p.(error); ok {
				err = e
			} else {
				err = fmt.Errorf("dwg: %v", p)
			}
			if !strings.HasPrefix(err.Error(), "dwg:") {
				err = fmt.Errorf("dwg: %w", err)
			}
		}
	}()
	return rd.read()
}

// FileVersion returns the version of a DWG file from its signature;
// VersionUnknown when data is not a DWG file.
func FileVersion(data []byte) cadoc.Version {
	if len(data) < 6 {
		return cadoc.VersionUnknown
	}
	return cadoc.ParseVersion(strings.ReplaceAll(string(data[:6]), "_", "."))
}

type reader struct {
	cfg    ReadConfig
	notify func(level int, msg string)
	data   []byte
	fh     *fileHeader
	// text is the drawing code page (ACadSharp's _encoding).
	text *textDecoder
	doc  *cadoc.Document
	b    *builder
}

func (r *reader) note(level int, format string, args ...any) {
	if r.notify != nil {
		r.notify(level, fmt.Sprintf(format, args...))
	}
}

func (r *reader) read() (*cadoc.Document, error) {
	if err := r.readFileHeader(); err != nil {
		return nil, err
	}
	r.doc = cadoc.NewEmptyDocument()
	r.b = newBuilder(r, r.fh.version)

	r.doc.SummaryInfo = r.readSummaryInfo()
	r.doc.Header = r.readHeader()
	// MEASUREMENT is only stored in the template section, often empty;
	// AutoCAD then uses English units (the DXF of such files says 0).
	r.doc.Header.MeasurementUnits = cadoc.MeasurementUnitsEnglish
	if r.doc.SummaryInfo.LastSavedBy != "" {
		r.doc.Header.LastSavedBy = r.doc.SummaryInfo.LastSavedBy
	}
	r.readTemplate()
	r.readClasses()
	r.readDsPrototype()
	r.readObjects()

	r.b.buildDocument()
	r.b.buildDataStorage()
	r.b.finish()
	return r.doc, nil
}

// ----- file header -----

type sectionLocator struct {
	number int
	seeker int64
	size   int64
}

type localSectionMap struct {
	isEmpty          bool
	offset           uint64
	compressedSize   uint64
	pageNumber       int
	decompressedSize uint64
	seeker           int64
	size             int64
	checksum         uint64
	crc              uint64
	pageSize         int64
}

type sectionDescriptor struct {
	name             string
	compressedSize   uint64
	pageCount        int
	decompressedSize uint64
	compressedCode   int
	sectionID        int
	encrypted        int
	hashCode         uint64
	encoding         uint64
	localSections    []*localSectionMap
}

// fileHeader is DwgFileHeaderAC15/AC18/AC21.
type fileHeader struct {
	version        cadoc.Version
	previewAddress int64
	maintenance    int
	codePage       int
	records        map[int]*sectionLocator

	// R2004+
	dwgVersion, appReleaseVersion      byte
	summaryInfoAddr, securityType      int64
	vbaProjectAddr                     int64
	rootTreeNodeGap, leftGap, rightGap int32
	lastPageID                         int32
	lastSectionAddr, secondHeaderAddr  uint64
	gapAmount, sectionAmount           uint32
	sectionPageMapID, sectionMapID     uint32
	pageMapAddress                     uint64
	sectionArrayPageSize, gapArraySize uint32
	crcSeed                            uint32
	descriptors                        map[string]*sectionDescriptor
	compressed                         *compressedMetadata21
}

// compressedMetadata21 is Dwg21CompressedMetadata.
type compressedMetadata21 struct {
	headerSize, fileSize, pagesMapCrcCompressed, pagesMapCorrectionFactor, pagesMapCrcSeed uint64
	map2Offset, map2ID, pagesMapOffset, pagesMapID, header2Offset                          uint64
	pagesMapSizeCompressed, pagesMapSizeUncompressed, pagesAmount, pagesMaxID              uint64
	unknown0x20, unknown0x40, pagesMapCrcUncompressed, unknown0xF800, unknown4, unknown1   uint64
	sectionsAmount, sectionsMapCrcUncompressed, sectionsMapSizeCompressed                  uint64
	sectionsMap2ID, sectionsMapID, sectionsMapSizeUncompressed, sectionsMapCrcCompressed   uint64
	sectionsMapCorrectionFactor, sectionsMapCrcSeed, streamVersion, crcSeed                uint64
	crcSeedEncoded, randomSeed, headerCRC64                                                uint64
}

func (r *reader) readFileHeader() (err error) {
	version := FileVersion(r.data)
	switch {
	case version == cadoc.VersionUnknown:
		return fmt.Errorf("%w: unknown DWG signature %q", ErrNotSupported, string(r.data[:min(6, len(r.data))]))
	case version <= cadoc.AC1009:
		return fmt.Errorf("%w: %s", ErrNotSupported, version)
	}
	fh := &fileHeader{version: version, previewAddress: -1, records: map[int]*sectionLocator{},
		descriptors: map[string]*sectionDescriptor{}}
	r.fh = fh
	defer func() {
		if p := recover(); p != nil {
			if e, ok := p.(error); ok {
				err = fmt.Errorf("dwg: invalid file header: %w", e)
			} else {
				err = fmt.Errorf("dwg: invalid file header: %v", p)
			}
		}
	}()
	s := NewBitReader(version, r.data, 6, utf8Decoder)
	switch version {
	case cadoc.AC1012, cadoc.AC1014, cadoc.AC1015:
		r.readFileHeaderAC15(fh, s)
	case cadoc.AC1018, cadoc.AC1024, cadoc.AC1027, cadoc.AC1032:
		r.readFileHeaderAC18(fh, s)
	case cadoc.AC1021:
		r.readFileHeaderAC21(fh, s)
	default:
		return fmt.Errorf("%w: %s", ErrNotSupported, version)
	}
	return nil
}

func (r *reader) setCodePage(index int) {
	r.fh.codePage = index
	r.text = newTextDecoder(DwgCodePageAt(index).Encoding)
}

// readFileHeaderAC15 reads the R13-R15 header: the section locator records.
func (r *reader) readFileHeaderAC15(fh *fileHeader, s *BitReader) {
	s.ReadBytes(7)
	fh.previewAddress = int64(s.ReadInt())
	s.ReadBytes(2)
	r.setCodePage(int(s.ReadShort()))
	n := int(s.ReadRawLong())
	for i := 0; i < n; i++ {
		rec := &sectionLocator{}
		rec.number = int(s.ReadRC())
		rec.seeker = s.ReadRawLong()
		rec.size = s.ReadRawLong()
		if _, dup := fh.records[rec.number]; dup {
			panic(errorf("repeated section locator record %d", rec.number))
		}
		fh.records[rec.number] = rec
	}
	s.ResetShift()
	s.ReadSentinel()
}

func (r *reader) readFileMetaData(fh *fileHeader, s *BitReader) {
	s.Advance(5)
	fh.maintenance = int(s.ReadRC())
	s.Advance(1)
	fh.previewAddress = s.ReadRawLong()
	fh.dwgVersion = s.ReadRC()
	fh.appReleaseVersion = s.ReadRC()
	r.setCodePage(int(s.ReadShort()))
	s.text = r.text
	s.Advance(3)
	fh.securityType = s.ReadRawLong()
	s.ReadRawLong()
	fh.summaryInfoAddr = s.ReadRawLong()
	fh.vbaProjectAddr = s.ReadRawLong()
	s.ReadRawLong()
	s.ReadRawLong()
	s.Advance(80)
}

// pageHeader reads the 20 byte header of a system page (getPageHeaderData).
func pageHeader(s *BitReader) (sectionType, decompressedSize, compressedSize, compressionType, checksum int64) {
	sectionType = s.ReadRawLong()
	decompressedSize = s.ReadRawLong()
	compressedSize = s.ReadRawLong()
	compressionType = s.ReadRawLong()
	checksum = s.ReadRawLong()
	return
}

// decompressSystemPage decompresses the AC18 LZ77 data following a page
// header at the reader position.
func decompressSystemPage(s *BitReader, size int64) *BitReader {
	out, _, err := DecompressAC18(s.Buf, s.Pos, make([]byte, 0, max(0, int(min(size, 1<<26)))))
	if err != nil {
		panic(err)
	}
	return NewBitReader(s.Version, out, 0, windows1252Decoder)
}

// readFileHeaderAC18 reads the R2004 (and R2010+) header: the encrypted
// header block, the page map and the section map.
func (r *reader) readFileHeaderAC18(fh *fileHeader, s *BitReader) {
	r.readFileMetaData(fh, s)
	enc := s.ReadBytes(0x6C)
	DecryptAC18Header(enc)
	h := NewBitReader(fh.version, enc, 0, windows1252Decoder)
	s.ReadBytes(20)

	if id := string(h.ReadBytes(12)); id != AC18FileIDString {
		r.note(NotifyWarning, "File validation failed, id should be : AcFssFcAJMB\x00, but is : %s", id)
	}
	h.ReadInt()
	h.ReadInt()
	h.ReadInt()
	fh.rootTreeNodeGap = h.ReadInt()
	fh.leftGap = h.ReadInt()
	fh.rightGap = h.ReadInt()
	h.ReadInt()
	fh.lastPageID = h.ReadInt()
	fh.lastSectionAddr = h.ReadRawULong()
	fh.secondHeaderAddr = h.ReadRawULong()
	fh.gapAmount = h.ReadUInt()
	fh.sectionAmount = h.ReadUInt()
	h.ReadInt()
	h.ReadInt()
	h.ReadInt()
	fh.sectionPageMapID = h.ReadUInt()
	fh.pageMapAddress = h.ReadRawULong() + 256
	fh.sectionMapID = h.ReadUInt()
	fh.sectionArrayPageSize = h.ReadUInt()
	fh.gapArraySize = h.ReadUInt()
	fh.crcSeed = h.ReadUInt()

	// Page map: page numbers and sizes; the pages follow each other from 0x100.
	s.SetPosition(int(fh.pageMapAddress))
	_, size, _, _, _ := pageHeader(s)
	pm := decompressSystemPage(s, size)
	total := int64(0x100)
	for pm.Pos < pm.Length() {
		rec := &sectionLocator{}
		rec.number = int(pm.ReadInt())
		rec.size = int64(pm.ReadInt())
		if rec.number >= 0 {
			rec.seeker = total
			if _, dup := fh.records[rec.number]; dup {
				panic(errorf("repeated page number %d", rec.number))
			}
			fh.records[rec.number] = rec
		} else {
			// Gap: parent, left, right, 0.
			pm.ReadInt()
			pm.ReadInt()
			pm.ReadInt()
			pm.ReadInt()
		}
		total += rec.size
	}

	// Section map: the logical sections and their pages.
	mapRec, ok := fh.records[int(fh.sectionMapID)]
	if !ok {
		panic(errorf("section map page %d not found", fh.sectionMapID))
	}
	s.SetPosition(int(mapRec.seeker))
	_, size, _, _, _ = pageHeader(s)
	sm := decompressSystemPage(s, size)
	n := int(sm.ReadInt())
	sm.ReadInt()
	sm.ReadInt()
	sm.ReadInt()
	sm.ReadInt()
	for i := 0; i < n; i++ {
		d := &sectionDescriptor{decompressedSize: 0x7400, compressedCode: 2}
		d.compressedSize = sm.ReadRawULong()
		d.pageCount = int(sm.ReadInt())
		d.decompressedSize = uint64(sm.ReadInt())
		sm.ReadInt()
		d.compressedCode = int(sm.ReadInt())
		if d.compressedCode != 1 && d.compressedCode != 2 {
			panic(errorf("invalid section compression code %d", d.compressedCode))
		}
		d.sectionID = int(sm.ReadInt())
		d.encrypted = int(sm.ReadInt())
		name := sm.ReadBytes(64)
		if i := bytes.IndexByte(name, 0); i >= 0 {
			name = name[:i]
		}
		d.name = windows1252Decoder.decode(name)

		var counter uint64
		for j := 0; j < d.pageCount; j++ {
			m := &localSectionMap{}
			m.pageNumber = int(sm.ReadInt())
			m.compressedSize = uint64(sm.ReadInt())
			m.offset = sm.ReadRawULong()
			// Pages of zeros are not stored: fill the gap with empty pages.
			for counter < m.offset {
				d.localSections = append(d.localSections, &localSectionMap{isEmpty: true, offset: counter,
					decompressedSize: d.decompressedSize})
				counter += d.decompressedSize
				if d.decompressedSize == 0 {
					break
				}
			}
			m.decompressedSize = d.decompressedSize
			rec, ok := fh.records[m.pageNumber]
			if !ok {
				panic(errorf("page %d of section %s not found", m.pageNumber, d.name))
			}
			m.seeker = rec.seeker
			d.localSections = append(d.localSections, m)
			counter += m.decompressedSize
		}
		if d.decompressedSize != 0 {
			if left := uint32(d.compressedSize % d.decompressedSize); left > 0 && len(d.localSections) > 0 {
				d.localSections[len(d.localSections)-1].decompressedSize = uint64(left)
			}
		}
		if _, dup := fh.descriptors[d.name]; dup {
			panic(errorf("repeated section %q", d.name))
		}
		fh.descriptors[d.name] = d
	}
}

// readFileHeaderAC21 reads the R2007 header: Reed-Solomon encoded and LZ77
// AC21 compressed metadata, page map and section map.
func (r *reader) readFileHeaderAC21(fh *fileHeader, s *BitReader) {
	r.readFileMetaData(fh, s)
	compressed := s.ReadBytes(0x400)
	decoded := make([]byte, 3*239)
	ReedSolomonDeinterleave(compressed, decoded, 3, 239)
	comprLen := int32(binary.LittleEndian.Uint32(decoded[24:]))
	buf := make([]byte, 0x110)
	if comprLen < 0 {
		panic(errorf("uncompressed R2007 file header data is not supported"))
	}
	if err := DecompressAC21(decoded, 32, uint32(comprLen), buf); err != nil {
		panic(err)
	}
	m := NewBitReader(fh.version, buf, 0, nil)
	c := &compressedMetadata21{}
	for _, f := range []*uint64{&c.headerSize, &c.fileSize, &c.pagesMapCrcCompressed, &c.pagesMapCorrectionFactor,
		&c.pagesMapCrcSeed, &c.map2Offset, &c.map2ID, &c.pagesMapOffset, &c.pagesMapID, &c.header2Offset,
		&c.pagesMapSizeCompressed, &c.pagesMapSizeUncompressed, &c.pagesAmount, &c.pagesMaxID, &c.unknown0x20,
		&c.unknown0x40, &c.pagesMapCrcUncompressed, &c.unknown0xF800, &c.unknown4, &c.unknown1, &c.sectionsAmount,
		&c.sectionsMapCrcUncompressed, &c.sectionsMapSizeCompressed, &c.sectionsMap2ID, &c.sectionsMapID,
		&c.sectionsMapSizeUncompressed, &c.sectionsMapCrcCompressed, &c.sectionsMapCorrectionFactor,
		&c.sectionsMapCrcSeed, &c.streamVersion, &c.crcSeed, &c.crcSeedEncoded, &c.randomSeed, &c.headerCRC64} {
		*f = m.ReadRawULong()
	}
	fh.compressed = c

	pages := r.pageBuffer21(c.pagesMapOffset, c.pagesMapSizeCompressed, c.pagesMapSizeUncompressed,
		c.pagesMapCorrectionFactor, 0xEF)
	ps := NewBitReader(fh.version, pages, 0, nil)
	var offset int64
	for ps.Pos < ps.Length() {
		size := int64(ps.ReadRawULong())
		id := int64(ps.ReadRawULong())
		if id < 0 {
			id = -id
		}
		if _, dup := fh.records[int(id)]; dup {
			panic(errorf("repeated page number %d", id))
		}
		fh.records[int(id)] = &sectionLocator{number: int(id), seeker: int64(int32(offset)), size: int64(int32(size))}
		offset += size
	}

	mapRec, ok := fh.records[int(c.sectionsMapID)]
	if !ok {
		panic(errorf("section map page %d not found", c.sectionsMapID))
	}
	sections := r.pageBuffer21(uint64(mapRec.seeker), c.sectionsMapSizeCompressed, c.sectionsMapSizeUncompressed,
		c.sectionsMapCorrectionFactor, 239)
	ss := NewBitReader(fh.version, sections, 0, nil)
	for ss.Pos < ss.Length() {
		d := &sectionDescriptor{decompressedSize: 0x7400, compressedCode: 2}
		d.compressedSize = ss.ReadRawULong()
		d.decompressedSize = ss.ReadRawULong()
		d.encrypted = int(ss.ReadRawULong())
		d.hashCode = ss.ReadRawULong()
		nameLength := int(int64(ss.ReadRawULong()))
		ss.ReadRawULong()
		d.encoding = ss.ReadRawULong()
		d.pageCount = int(ss.ReadRawULong())
		if nameLength > 0 {
			d.name = removeNUL(decodeUTF16LE(ss.ReadBytes(nameLength)))
		}
		var current uint64
		for i := 0; i < d.pageCount; i++ {
			p := &localSectionMap{}
			p.offset = ss.ReadRawULong()
			p.size = int64(ss.ReadRawULong())
			p.pageNumber = int(int64(ss.ReadRawULong()))
			p.decompressedSize = ss.ReadRawULong()
			p.compressedSize = ss.ReadRawULong()
			p.checksum = ss.ReadRawULong()
			p.crc = ss.ReadRawULong()
			if current < p.offset {
				d.localSections = append(d.localSections, &localSectionMap{isEmpty: true, offset: current,
					decompressedSize: p.offset - current})
			}
			d.localSections = append(d.localSections, p)
			current = p.offset + p.decompressedSize
		}
		if nameLength > 0 {
			if _, dup := fh.descriptors[d.name]; dup {
				panic(errorf("repeated section %q", d.name))
			}
			fh.descriptors[d.name] = d
		}
	}
}

// pageBuffer21 reads a Reed-Solomon encoded, AC21 compressed system page
// (DwgReader.getPageBuffer).
func (r *reader) pageBuffer21(pageOffset, compressedSize, uncompressedSize, correctionFactor uint64, blockSize int) []byte {
	v := (compressedSize + 7) & 0xFFFFFFF8
	totalSize := uint32(v * correctionFactor)
	factor := (int(totalSize) + blockSize - 1) / blockSize
	length := factor * 255
	checkAlloc(uint64(length))
	checkAlloc(uncompressedSize)
	buffer := make([]byte, length)
	pos := int64(0x480) + int64(pageOffset)
	if pos >= 0 && pos < int64(len(r.data)) {
		copy(buffer, r.data[pos:])
	}
	checkAlloc(uint64(totalSize))
	data := make([]byte, totalSize)
	ReedSolomonDeinterleave(buffer, data, factor, blockSize)
	out := make([]byte, uncompressedSize)
	if err := DecompressAC21(data, 0, uint32(compressedSize), out); err != nil {
		panic(err)
	}
	return out
}

// ----- section streams -----

// sectionStream returns a reader for a section, nil when the file has none
// (DwgReader.getSectionStream).
func (r *reader) sectionStream(name string) *BitReader {
	fh := r.fh
	switch fh.version {
	case cadoc.AC1012, cadoc.AC1014, cadoc.AC1015:
		idx := SectionLocatorIndex(name)
		if idx < 0 {
			return nil
		}
		rec, ok := fh.records[idx]
		if !ok {
			return nil
		}
		// R13-R15 sections are read in place from the file.
		return NewBitReader(fh.version, r.data, int(rec.seeker), r.text)
	case cadoc.AC1021:
		buf := r.sectionBuffer21(name)
		if buf == nil {
			return nil
		}
		return NewBitReader(fh.version, buf, 0, r.text)
	default:
		buf := r.sectionBuffer18(name)
		if buf == nil {
			return nil
		}
		return NewBitReader(fh.version, buf, 0, r.text)
	}
}

// sectionBuffer18 joins the (decompressed) pages of an R2004 section.
func (r *reader) sectionBuffer18(name string) []byte {
	d, ok := r.fh.descriptors[name]
	if !ok {
		return nil
	}
	out := make([]byte, 0, int(min(d.decompressedSize*uint64(len(d.localSections)), 1<<28)))
	for _, sec := range d.localSections {
		if sec.isEmpty {
			checkAlloc(uint64(len(out)) + sec.decompressedSize)
			out = append(out, make([]byte, sec.decompressedSize)...)
			continue
		}
		s := NewBitReader(r.fh.version, r.data, int(sec.seeker), nil)
		r.decryptDataSection(sec, s)
		if d.compressedCode == 2 {
			var err error
			out, _, err = DecompressAC18(r.data, s.Pos, out)
			if err != nil {
				panic(err)
			}
		} else {
			out = append(out, s.ReadBytes(int(sec.compressedSize))...)
		}
	}
	return out
}

// decryptDataSection reads the XOR masked header of a data page.
func (r *reader) decryptDataSection(sec *localSectionMap, s *BitReader) {
	mask := int64(int32(DataSectionMaskSeed ^ uint32(s.Pos)))
	s.ReadRawLong() // page type
	s.ReadRawLong() // section number
	sec.compressedSize = uint64(s.ReadRawLong() ^ mask)
	sec.pageSize = s.ReadRawLong() ^ mask
	startOffset := s.ReadRawLong() ^ mask
	checksum := s.ReadRawLong() ^ mask
	sec.offset = uint64(checksum + startOffset)
	sec.checksum = uint64(uint32(s.ReadRawLong() ^ mask))
	s.ReadRawLong()
}

// sectionBuffer21 joins the pages of an R2007 section.
func (r *reader) sectionBuffer21(name string) []byte {
	d, ok := r.fh.descriptors[name]
	if !ok {
		return nil
	}
	var total uint64
	for _, p := range d.localSections {
		total += p.decompressedSize
	}
	out := make([]byte, 0, int(min(total, 1<<28)))
	for _, p := range d.localSections {
		if p.isEmpty {
			checkAlloc(uint64(len(out)) + p.decompressedSize)
			out = append(out, make([]byte, p.decompressedSize)...)
			continue
		}
		rec, ok := r.fh.records[p.pageNumber]
		if !ok {
			panic(errorf("page %d of section %s not found", p.pageNumber, name))
		}
		pos := rec.seeker + 0x480
		if pos < 0 || pos+rec.size > int64(len(r.data)) || rec.size < 0 {
			panic(errEndOfStream)
		}
		page := r.data[pos : pos+rec.size]
		checkAlloc(p.compressedSize)
		checkAlloc(p.decompressedSize)
		if d.encoding == 4 {
			v := (p.compressedSize + 7) & 0xFFFFFFF8
			aligned := int((v + 251 - 1) / 251)
			arr := make([]byte, aligned*251)
			ReedSolomonDeinterleave(page, arr, aligned, 251)
			page = arr
		}
		if p.compressedSize != p.decompressedSize {
			arr := make([]byte, p.decompressedSize)
			if err := DecompressAC21(page, 0, uint32(p.compressedSize), arr); err != nil {
				panic(err)
			}
			page = arr
		}
		if uint64(len(page)) < p.decompressedSize {
			panic(errEndOfStream)
		}
		out = append(out, page[:p.decompressedSize]...)
	}
	return out
}

// guardSection runs fn and turns a panic into an error notification; it
// reports whether fn completed.
func (r *reader) guardSection(name string, fn func()) (ok bool) {
	defer func() {
		if p := recover(); p != nil {
			r.note(NotifyError, "An error occurred while reading the section %s: %v", name, p)
			ok = false
		}
	}()
	fn()
	return true
}

// ----- summary info, template -----

// readSummaryInfo reads AcDb:SummaryInfo (R2004+; DwgSummaryInfoReader).
func (r *reader) readSummaryInfo() *cadoc.SummaryInfo {
	info := &cadoc.SummaryInfo{Properties: map[string]string{}}
	if r.fh.version < cadoc.AC1018 {
		return info
	}
	var s *BitReader
	if !r.guardSection(SectionSummaryInfo, func() { s = r.sectionStream(SectionSummaryInfo) }) || s == nil {
		return info
	}
	readString := func() string {
		if r.fh.version >= cadoc.AC1021 {
			return s.ReadTextUnicode()
		}
		n := s.ReadShort()
		if n == 0 {
			return ""
		}
		return removeNUL(s.ReadString(int(n), windows1252Decoder))
	}
	func() {
		defer func() {
			if p := recover(); p != nil && s.Pos != s.Length() {
				r.note(NotifyError, "An error occurred while reading the Summary Info: %v", p)
			}
		}()
		info.Title = readString()
		info.Subject = readString()
		info.Author = readString()
		info.Keywords = readString()
		info.Comments = readString()
		info.LastSavedBy = readString()
		info.RevisionNumber = readString()
		info.HyperlinkBase = readString()
		s.ReadInt()
		s.ReadInt()
		s.Read8BitJulianDate()
		s.Read8BitJulianDate()
		n := int(s.ReadShort())
		for i := 0; i < n; i++ {
			name := readString()
			value := readString()
			if _, dup := info.Properties[name]; dup {
				r.note(NotifyError, "[SummaryInfo] An error ocurred while adding a property in the SummaryInfo")
				continue
			}
			info.Properties[name] = value
		}
		s.ReadInt()
		s.ReadInt()
	}()
	return info
}

// readTemplate reads AcDb:Template (R2004+): the MEASUREMENT variable.
func (r *reader) readTemplate() {
	if r.fh.version < cadoc.AC1018 {
		return
	}
	r.guardSection(SectionTemplate, func() {
		s := r.sectionStream(SectionTemplate)
		if s == nil || s.Length() < 4 {
			return
		}
		n := int(s.ReadShort())
		if n > 0 {
			if s.Length() < 4+n {
				return
			}
			s.ReadBytes(n)
		}
		r.doc.Header.MeasurementUnits = cadoc.MeasurementUnits(s.ReadShort())
	})
}
