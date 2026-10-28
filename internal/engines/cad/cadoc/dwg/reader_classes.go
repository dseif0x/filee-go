package dwg

import (
	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// readClasses reads AcDb:Classes into the document (DwgClassesReader).
func (r *reader) readClasses() {
	s := r.sectionStream(SectionClasses)
	if s == nil {
		panic(errorf("classes section not found"))
	}
	f := newVersionFeatures(r.fh.version)
	if s.ReadSentinel() != SentinelClassesStart {
		r.note(NotifyWarning, "Invalid section sentinel found in %s", SectionClasses)
	}
	size := s.ReadRawLong()
	endSection := int64(s.Pos) + size
	if r.fh.version >= cadoc.AC1024 && r.fh.maintenance > 3 || r.fh.version > cadoc.AC1027 {
		s.ReadRawLong()
	}
	rd := newMerged(s, s, s)
	var flagPos int64
	if f.R2007Plus {
		flagPos = s.PositionInBits() + s.ReadRawLong() - 1
		saved := s.PositionInBits()
		endSection = s.SetPositionByFlag(flagPos)
		s.SetPositionInBits(saved)
		text := s.clone()
		text.SetPositionInBits(endSection)
		rd = newMerged(s, text, nil)
		rd.ReadBitLong()
		rd.ReadBit()
	}
	if r.fh.version == cadoc.AC1018 {
		rd.ReadBitShort() // maximum class number
		rd.ReadRawChar()
		rd.ReadRawChar()
		rd.ReadBit()
	}
	currPos := func() int64 {
		if f.R2007Plus {
			return s.PositionInBits()
		}
		return int64(s.Pos)
	}
	for currPos() < endSection {
		c := &cadoc.DxfClass{}
		c.ClassNumber = rd.ReadBitShort()
		c.ProxyFlags = cadoc.ProxyFlags(uint16(rd.ReadBitShort()))
		c.ApplicationName = rd.ReadVariableText()
		c.CppClassName = rd.ReadVariableText()
		c.DxfName = rd.ReadVariableText()
		c.WasZombie = rd.ReadBit()
		c.IsAnEntity = rd.ReadBitShort() == 0x1F2
		if f.R2004Plus {
			c.InstanceCount = rd.ReadBitLong()
			c.DwgVersion = cadoc.Version(rd.ReadBitLong())
			c.MaintenanceVersion = int16(rd.ReadBitLong())
			rd.ReadBitLong()
			rd.ReadBitLong()
		}
		r.doc.Classes.TryAdd(c)
	}
	if f.R2007Plus {
		s.SetPositionInBits(flagPos + 1)
	}
	s.ResetShift()
	if s.ReadSentinel() != SentinelClassesEnd {
		r.note(NotifyWarning, "Invalid section sentinel found in %s", SectionClasses)
	}
}

// readHandles reads AcDb:Handles, the object map from handle to the offset
// of the object in the objects stream (DwgHandleReader).
func (r *reader) readHandles() *handleMap {
	s := r.sectionStream(SectionHandles)
	if s == nil {
		panic(errorf("handles section not found"))
	}
	m := &handleMap{offsets: map[uint64]int64{}}
	for {
		var lastHandle uint64
		var lastLoc int64
		size := int(s.ReadShortBE())
		if size == 2 {
			break
		}
		start := s.Pos
		maxOffset := size - 2
		if maxOffset > 2032 {
			maxOffset = 2032
		}
		last := start + maxOffset
		for s.Pos < last {
			offset := s.ReadModularChar()
			lastHandle += offset
			lastLoc += s.ReadSignedModularChar()
			if offset > 0 {
				m.set(lastHandle, lastLoc)
			} else {
				r.note(NotifyWarning, "Negative offset: %d for the handle: %d", offset, lastHandle)
			}
		}
		// CRC, most significant byte first.
		s.ReadRC()
		s.ReadRC()
	}
	return m
}

// handleMap is the object map, keeping the file order of the handles.
type handleMap struct {
	offsets map[uint64]int64
	order   []uint64
}

func (m *handleMap) set(h uint64, loc int64) {
	if _, ok := m.offsets[h]; !ok {
		m.order = append(m.order, h)
	}
	m.offsets[h] = loc
}
