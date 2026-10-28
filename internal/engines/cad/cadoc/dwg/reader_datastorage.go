package dwg

import (
	"bytes"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// The AcDb:AcDsPrototype_1b section (R2013+) is a small database; its
// records with an AcDbDs::ID and ASM_Data column carry the SAB data of the
// modeler geometry with that handle (DwgPrototype1bReader).

const (
	dsColumnID  = "AcDbDs::ID"
	dsColumnASM = "ASM_Data"
)

// acdsRecord is the part of an AcdsRecord the model uses.
type acdsRecord struct {
	handle    uint64
	hasHandle bool
	data      []byte
	hasData   bool
}

type dsSegmentHeader struct {
	segmentIndex              uint32
	segmentSize               uint32
	systemDataAlignmentOffset int32
	objectDataAlignmentOffset int32
}

type dsSchemaRecord struct {
	flags     int32
	nameIndex uint32
	typeSize  uint32
	name      string
}

type dsSchema struct {
	index   uint32
	records []*dsSchemaRecord
}

type dsPointer struct{ index, segmentIndex, localOffset uint32 }

type dsDataIndexEntry struct{ segmentIndex, localOffset, schemaIndex uint32 }

var dsTypeSizes = []uint32{0, 0, 2, 1, 2, 4, 8, 1, 2, 4, 8, 4, 8, 0, 0, 0}

func (r *reader) readPrototype1b(s *BitReader) {
	var records []acdsRecord
	ok := r.guardSection(SectionAcDsProto, func() { records = readDataStorage(s) })
	if ok {
		r.b.dataStorage = records
	}
}

func dsHeader(s *BitReader) dsSegmentHeader {
	var h dsSegmentHeader
	s.ReadShort() // signature
	s.ReadBytes(6)
	h.segmentIndex = s.ReadUInt()
	s.ReadInt() // is blob
	h.segmentSize = s.ReadUInt()
	s.ReadInt()
	s.ReadInt() // revision
	s.ReadInt()
	h.systemDataAlignmentOffset = s.ReadInt()
	h.objectDataAlignmentOffset = s.ReadInt()
	s.ReadBytes(8)
	return h
}

func dsString(s *BitReader) string {
	var b []byte
	for c := s.ReadRC(); c != 0; c = s.ReadRC() {
		b = append(b, c)
	}
	return s.text.decode(b)
}

func readDataStorage(s *BitReader) []acdsRecord {
	// File header.
	s.ReadUInt()
	s.ReadInt()
	s.ReadInt()
	s.ReadInt()
	s.ReadInt()
	s.ReadInt()
	segmentIndexOffset := s.ReadInt()
	s.ReadInt()
	segmentIndexCount := int(s.ReadInt())
	schemaIndexSegment := int(s.ReadInt())
	dataIndexSegment := int(s.ReadInt())
	s.ReadInt()
	s.ReadInt()
	s.ReadInt()

	// Segment index: the offset of every segment.
	s.SetPosition(int(segmentIndexOffset))
	dsHeader(s)
	type segment struct {
		offset uint64
		size   uint32
	}
	segments := make([]segment, s.count(int32(segmentIndexCount), 96))
	for i := range segments {
		segments[i].offset = s.ReadRawULong()
		segments[i].size = s.ReadUInt()
	}
	segmentAt := func(i uint32) int {
		if int(i) >= len(segments) {
			panic(errEndOfStream)
		}
		return int(segments[i].offset)
	}

	// Schema index.
	start := segmentAt(uint32(schemaIndexSegment))
	s.SetPosition(start)
	sh := dsHeader(s)
	n := int(s.ReadUInt())
	s.ReadUInt()
	var schemaPointers, propertyPointers []dsPointer
	for i := 0; i < n; i++ {
		p := dsPointer{}
		p.index = s.ReadUInt()
		p.segmentIndex = s.ReadUInt()
		p.localOffset = s.ReadUInt()
		schemaPointers = append(schemaPointers, p)
	}
	s.ReadRawULong()
	n = int(s.ReadUInt())
	s.ReadUInt()
	for i := 0; i < n; i++ {
		p := dsPointer{}
		p.segmentIndex = s.ReadUInt()
		p.localOffset = s.ReadUInt()
		p.index = s.ReadUInt()
		propertyPointers = append(propertyPointers, p)
	}
	var schemaNames []string
	if off := int64(sh.systemDataAlignmentOffset) << 4; off != 0 {
		s.SetPosition(start + int(off))
		n := int(s.ReadUInt())
		for i := 0; i < n; i++ {
			schemaNames = append(schemaNames, dsString(s))
		}
	}

	// Schema data segments.
	var schemas []*dsSchema
	seenSeg := map[uint32]bool{}
	for _, p := range schemaPointers {
		if seenSeg[p.segmentIndex] {
			continue
		}
		seenSeg[p.segmentIndex] = true
		s.SetPosition(segmentAt(p.segmentIndex))
		h := dsHeader(s)
		for _, pp := range propertyPointers {
			if pp.segmentIndex == h.segmentIndex {
				s.ReadUInt()
				s.ReadUInt()
			}
		}
		var segSchemas []*dsSchema
		for _, sp := range schemaPointers {
			if sp.segmentIndex != h.segmentIndex {
				continue
			}
			sc := readDsSchema(s)
			sc.index = sp.index
			if int(sp.index) >= len(schemaNames) {
				panic(errEndOfStream)
			}
			segSchemas = append(segSchemas, sc)
		}
		if b := s.Pos % 16; b != 0 {
			s.ReadBytes(16 - b)
		}
		nNames := int(s.ReadUInt())
		names := make([]string, s.count(int32(nNames), 8))
		for i := range names {
			names[i] = dsString(s)
		}
		for _, sc := range segSchemas {
			for _, rec := range sc.records {
				if int(rec.nameIndex) >= len(names) {
					panic(errEndOfStream)
				}
				rec.name = names[rec.nameIndex]
			}
		}
		schemas = append(schemas, segSchemas...)
	}

	// Data index.
	s.SetPosition(segmentAt(uint32(dataIndexSegment)))
	dsHeader(s)
	n = int(s.ReadInt())
	s.ReadInt()
	var entries []dsDataIndexEntry
	for i := 0; i < n; i++ {
		e := dsDataIndexEntry{}
		e.segmentIndex = s.ReadUInt()
		e.localOffset = s.ReadUInt()
		e.schemaIndex = s.ReadUInt()
		if e.segmentIndex == 0 {
			continue
		}
		entries = append(entries, e)
	}

	// Data segments.
	var records []acdsRecord
	seenData := map[uint32]bool{}
	for _, de := range entries {
		index := de.segmentIndex
		if seenData[index] {
			continue
		}
		seenData[index] = true
		headerStart := segmentAt(index)
		s.SetPosition(headerStart)
		fh := dsHeader(s)
		if fh.segmentSize == 0 {
			continue
		}
		headerEnd := s.Pos
		var segEntries []dsDataIndexEntry
		for _, e := range entries {
			if e.segmentIndex == index {
				segEntries = append(segEntries, e)
			}
		}
		type dataHeader struct {
			handle      uint64
			localOffset uint32
		}
		headers := make([]dataHeader, len(segEntries))
		for i, e := range segEntries {
			s.SetPosition(headerEnd + int(e.localOffset))
			s.ReadUInt() // size
			s.ReadUInt()
			headers[i].handle = s.ReadRawULong()
			headers[i].localOffset = s.ReadUInt()
		}
		dataRef := headerStart + int(int64(fh.objectDataAlignmentOffset)<<4)
		for i, dh := range headers {
			s.SetPosition(dataRef + int(dh.localOffset))
			var maxSize uint32
			if i+1 < len(headers) {
				maxSize = headers[i+1].localOffset - dh.localOffset
			} else {
				maxSize = fh.segmentSize - uint32(fh.objectDataAlignmentOffset<<4) - dh.localOffset
			}
			size := s.ReadUInt()
			var data []byte
			hasData := false
			if size+4 <= maxSize {
				data, hasData = s.ReadBytes(int(size)), true
			} else if size == 0xbb106bb1 {
				s.ReadRawULong() // total size
				pages := int(s.ReadUInt())
				s.ReadUInt()
				s.ReadUInt()
				s.ReadUInt()
				s.ReadUInt()
				s.ReadUInt()
				var buf bytes.Buffer
				for j := 0; j < pages; j++ {
					seg := s.ReadUInt()
					s.ReadUInt()
					next := s.Pos
					s.SetPosition(segmentAt(seg))
					dsHeader(s)
					s.ReadRawULong()
					s.ReadRawULong()
					s.ReadUInt()
					s.ReadUInt()
					n := s.ReadRawULong()
					buf.Write(s.ReadBytes(int(n)))
					s.SetPosition(next)
				}
				data, hasData = buf.Bytes(), true
			}
			var schema *dsSchema
			for _, sc := range schemas {
				if sc.index == segEntries[i].schemaIndex {
					schema = sc
					break
				}
			}
			if schema == nil {
				panic(errorf("data storage schema %d not found", segEntries[i].schemaIndex))
			}
			rec := acdsRecord{}
			for _, col := range schema.records {
				switch col.name {
				case dsColumnID:
					rec.handle, rec.hasHandle = dh.handle, true
				case dsColumnASM:
					if !hasData {
						panic(errorf("data storage record %d without data", dh.handle))
					}
					rec.data, rec.hasData = data, true
				}
			}
			records = append(records, rec)
		}
	}
	return records
}

func readDsSchema(s *BitReader) *dsSchema {
	sc := &dsSchema{}
	n := int(s.ReadUShort())
	for i := 0; i < n; i++ {
		s.ReadRawULong()
	}
	n = int(s.ReadUShort())
	for i := 0; i < n; i++ {
		sc.records = append(sc.records, readDsSchemaRecord(s))
	}
	return sc
}

func readDsSchemaRecord(s *BitReader) *dsSchemaRecord {
	r := &dsSchemaRecord{}
	r.flags = s.ReadInt()
	r.nameIndex = s.ReadUInt()
	if r.flags&2 == 0 {
		typ := s.ReadUInt()
		if typ == 0xE {
			r.typeSize = s.ReadUInt()
		} else if int(typ) < len(dsTypeSizes) {
			r.typeSize = dsTypeSizes[typ]
		} else {
			panic(errorf("invalid data storage type %d", typ))
		}
	}
	switch r.flags {
	case 1, 8:
		s.ReadUInt()
	}
	values := int(s.ReadUShort())
	if r.typeSize == 0 {
		return r
	}
	for i := 0; i < values; i++ {
		s.ReadBytes(int(r.typeSize))
	}
	return r
}

// buildDataStorage gives the modeler geometries their SAB data
// (CadDocumentBuilder.BuildDataStorage).
func (b *builder) buildDataStorage() {
	for _, rec := range b.dataStorage {
		if !rec.hasHandle || !rec.hasData {
			continue
		}
		m, ok := b.objects[rec.handle].(interface {
			Modeler() *cadoc.ModelerGeometry
		})
		if !ok {
			b.notify(NotifyWarning, "ModelerGeometry with handle %d not found in the document", rec.handle)
			continue
		}
		m.Modeler().AcisData = rec.data
	}
}
