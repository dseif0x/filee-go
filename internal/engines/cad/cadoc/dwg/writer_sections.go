package dwg

import (
	"sort"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// writeSizeAndCRC wraps a section body as DwgHeaderWriter and
// DwgClassesWriter do: start sentinel, RL size (plus an RL 0 for R2010+
// files with maintenance version > 3 and R2018+), the data, the CRC of size
// and data, end sentinel.
func writerSectionWithCRC(start, end, body []byte, extraSizeLong bool) *wMemStream {
	out := &wMemStream{}
	out.write(start)
	crcStart := out.pos
	out.writeInt32(int32(len(body)))
	if extraSizeLong {
		out.writeInt32(0)
	}
	out.write(body)
	out.writeInt16(int16(wCRC8(0xC0C1, out.bytes()[crcStart:])))
	out.write(end)
	return out
}

// ----- Classes -----

func (w *writer) writeClassesSection() []byte {
	r2004 := w.version >= cadoc.AC1018
	sw := newWMerged(w.version, w.enc)
	if w.version >= cadoc.AC1021 {
		sw.savePositionForSize()
	}
	classes := w.doc.Classes.Entries()
	if r2004 {
		var maxClass int16
		for _, c := range classes {
			maxClass = max(maxClass, c.ClassNumber)
		}
		sw.writeBitShort(maxClass)
		sw.writeByte(0)
		sw.writeByte(0)
		sw.writeBit(true)
	}
	for _, c := range classes {
		sw.writeBitShort(c.ClassNumber)
		sw.writeBitShort(int16(c.ProxyFlags))
		sw.writeVariableText(c.ApplicationName)
		sw.writeVariableText(c.CppClassName)
		sw.writeVariableText(c.DxfName)
		sw.writeBit(c.WasZombie)
		sw.writeBitShort(c.ItemClassId())
		if r2004 {
			sw.writeBitLong(c.InstanceCount)
			sw.writeBitLong(int32(c.DwgVersion))
			sw.writeBitLong(int32(c.MaintenanceVersion))
			sw.writeBitLong(0)
			sw.writeBitLong(0)
		}
	}
	sw.writeSpearShift()

	h := w.doc.Header
	extra := w.version >= cadoc.AC1024 && h.MaintenanceVersion > 3 || w.version > cadoc.AC1027
	out := writerSectionWithCRC(writerClassesStartSentinel, writerClassesEndSentinel, sw.m.ms.bytes(), extra)
	if r2004 {
		// R2004+: 8 unknown bytes, the ODA writes zeros.
		out.writeInt32(0)
		out.writeInt32(0)
	}
	return out.bytes()
}

// ----- Handles (object map) -----

// writeHandlesSection is DwgHandleWriter: pages of at most 2032 bytes of
// (handle delta, offset delta) modular chars, each with its size and CRC.
func writeHandlesSection(handles map[uint64]int64, sectionOffset int) []byte {
	keys := make([]uint64, 0, len(handles))
	for k := range handles {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	s := &wMemStream{}
	var arr [10]byte
	var arr2 [5]byte
	var offset uint64
	var initialLoc int64
	lastPosition := s.pos
	s.writeByte(0)
	s.writeByte(0)
	for _, k := range keys {
		handleOff := k - offset
		lastLoc := handles[k] + int64(sectionOffset)
		locDiff := lastLoc - initialLoc
		offsetSize := wModularChar(handleOff, arr[:])
		locSize := wSignedModularChar(int32(locDiff), arr2[:])
		if s.pos-lastPosition+offsetSize+locSize > 2032 {
			writerProcessHandlePage(s, lastPosition)
			offset, initialLoc = 0, 0
			lastPosition = s.pos
			s.writeByte(0)
			s.writeByte(0)
			handleOff = k
			locDiff = lastLoc
			offsetSize = wModularChar(handleOff, arr[:])
			locSize = wSignedModularChar(int32(locDiff), arr2[:])
		}
		s.write(arr[:offsetSize])
		s.write(arr2[:locSize])
		offset = k
		initialLoc = lastLoc
	}
	writerProcessHandlePage(s, lastPosition)
	lastPosition = s.pos
	s.writeByte(0)
	s.writeByte(0)
	writerProcessHandlePage(s, lastPosition)
	return s.bytes()
}

func wModularChar(v uint64, arr []byte) int {
	i := 0
	for v >= 0x80 {
		arr[i] = byte(v&0x7F | 0x80)
		i++
		v >>= 7
	}
	arr[i] = byte(v)
	return i + 1
}

func wSignedModularChar(v int32, arr []byte) int {
	i := 0
	if v < 0 {
		for v = -v; v >= 64; v >>= 7 {
			arr[i] = byte(uint32(v)&0x7F | 0x80)
			i++
		}
		arr[i] = byte(uint32(v) | 0x40)
		return i + 1
	}
	for v >= 0x40 {
		arr[i] = byte(uint32(v)&0x7F | 0x80)
		i++
		v >>= 7
	}
	arr[i] = byte(v)
	return i + 1
}

// writerProcessHandlePage writes the page size (big endian) at pos and
// appends the CRC (big endian) of the page.
func writerProcessHandlePage(s *wMemStream, pos int) {
	diff := uint16(s.pos - pos)
	end := s.pos
	s.pos = pos
	s.writeByte(byte(diff >> 8))
	s.writeByte(byte(diff))
	s.pos = end
	crc := wCRC8(0xC0C1, s.bytes()[pos:])
	s.writeByte(byte(crc >> 8))
	s.writeByte(byte(crc))
}

// ----- AuxHeader -----

func (w *writer) writeAuxHeaderSection() []byte {
	h := w.doc.Header
	b := newWBits(w.version, &wMemStream{}, w.enc)
	maint := func() {
		if w.version > cadoc.AC1027 {
			b.writeRawLong(int64(h.MaintenanceVersion))
		} else {
			b.writeRawShort(h.MaintenanceVersion)
		}
	}
	b.writeByte(0xFF)
	b.writeByte(0x77)
	b.writeByte(0x01)
	b.writeRawShort(int16(w.version))
	maint()
	b.writeRawLong(1)  // number of saves
	b.writeRawLong(-1) // -1
	b.writeRawShort(1)
	b.writeRawShort(0)
	b.writeRawLong(0)
	b.writeRawShort(int16(w.version))
	maint()
	b.writeRawShort(int16(w.version))
	maint()
	b.writeRawShort(0x5)
	b.writeRawShort(0x893)
	b.writeRawShort(5)
	b.writeRawShort(0x893)
	b.writeRawShort(0)
	b.writeRawShort(1)
	for i := 0; i < 5; i++ {
		b.writeRawLong(0)
	}
	b.write8BitJulianDate(h.CreateDateTime)
	b.write8BitJulianDate(h.UpdateDateTime)
	handseed := int64(-1)
	if h.HandleSeed <= 0x7FFFFFFF {
		handseed = int64(h.HandleSeed)
	}
	b.writeRawLong(handseed)
	b.writeRawLong(0) // educational plot stamp
	b.writeRawShort(0)
	b.writeRawShort(1)
	b.writeRawLong(0)
	b.writeRawLong(0)
	b.writeRawLong(0)
	b.writeRawLong(1) // number of saves
	for i := 0; i < 4; i++ {
		b.writeRawLong(0)
	}
	if w.version >= cadoc.AC1032 {
		b.writeRawShort(0)
		b.writeRawShort(0)
		b.writeRawShort(0)
	}
	return b.ms.bytes()
}

// ----- Preview -----

// writePreviewSection is DwgPreviewWriter.Write without an image.
func (w *writer) writePreviewSection() []byte {
	b := newWBits(w.version, &wMemStream{}, nil)
	b.writeBytes(writerPreviewStartSentinel)
	b.writeRawLong(1) // overall size
	b.writeByte(0)    // images present
	b.writeBytes(writerPreviewEndSentinel)
	return b.ms.bytes()
}

// ----- AppInfo (R2004+) -----

const writerAppVersion = "1.0.0.0"

func (w *writer) writeAppInfoSection() []byte {
	b := newWBits(w.version, &wMemStream{}, wUTF16Encoder)
	empty := make([]byte, 16)
	b.writeInt(3)
	b.writeTextUnicode("AppInfoDataList")
	b.writeInt(3)
	b.writeBytes(empty)
	b.writeTextUnicode(writerAppVersion)
	b.writeBytes(empty)
	b.writeTextUnicode("This is a comment from Filee")
	b.writeBytes(empty)
	b.writeTextUnicode(`<ProductInformation name ="Filee" build_version="` + writerAppVersion + `" registry_version="` +
		writerAppVersion + `" install_id_string="Filee" registry_localeID="1033"/>`)
	return b.ms.bytes()
}

// ----- SummaryInfo (R2004+) -----

func (w *writer) writeSummaryInfoSection() []byte {
	info := w.doc.SummaryInfo
	if info == nil {
		info = &cadoc.SummaryInfo{}
	}
	enc, _ := writerEncoding("ANSI_1252")
	b := newWBits(w.version, &wMemStream{}, enc)
	for _, s := range []string{info.Title, info.Subject, info.Author, info.Keywords, info.Comments,
		info.LastSavedBy, info.RevisionNumber, info.HyperlinkBase} {
		b.writeTextUnicode(s)
	}
	// Total editing time (the ODA writes two zero longs).
	b.writeInt(0)
	b.writeInt(0)
	// The model keeps no summary dates; the header dates are what AutoCAD
	// shows there.
	b.write8BitJulianDate(w.doc.Header.CreateDateTime)
	b.write8BitJulianDate(w.doc.Header.UpdateDateTime)
	keys := make([]string, 0, len(info.Properties))
	for k := range info.Properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b.writeRawShort(int16(len(keys)))
	for _, k := range keys {
		b.writeTextUnicode(k)
		b.writeTextUnicode(info.Properties[k])
	}
	b.writeInt(0)
	b.writeInt(0)
	return b.ms.bytes()
}

// ----- small sections -----

func writeFileDepListSection() []byte {
	s := &wMemStream{}
	s.writeUint32(0) // feature count
	s.writeUint32(0) // file count
	return s.bytes()
}

func writeRevHistorySection() []byte {
	s := &wMemStream{}
	s.writeUint32(0)
	s.writeUint32(0)
	s.writeUint32(0)
	return s.bytes()
}

func (w *writer) writeObjFreeSpaceSection(objectCount int) []byte {
	s := &wMemStream{}
	s.writeInt32(0)
	s.writeUint32(uint32(objectCount))
	// Julian date: TDUUPDATE for R2000+, TDUPDATE before (as ACadSharp).
	t := w.doc.Header.UpdateDateTime
	if w.version >= cadoc.AC1015 {
		t = w.doc.Header.UniversalUpdateDateTime
	}
	jd, ms := wDateToJulian(t)
	s.writeInt32(jd)
	s.writeInt32(ms)
	s.writeUint32(0) // offset of the objects section
	s.writeByte(4)
	for _, v := range []uint32{0x32, 0, 0x64, 0, 0x200, 0, 0xFFFFFFFF, 0} {
		s.writeUint32(v)
	}
	return s.bytes()
}

func (w *writer) writeTemplateSection() []byte {
	s := &wMemStream{}
	s.writeInt16(0)
	s.writeInt16(int16(w.doc.Header.MeasurementUnits))
	return s.bytes()
}
