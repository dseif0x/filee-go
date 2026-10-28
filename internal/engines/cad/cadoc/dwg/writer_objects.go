package dwg

import (
	"encoding/binary"
	"math"
	"strings"
	"unicode/utf16"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// wObjectWriter is DwgObjectWriter: the AcDb:AcDbObjects section and the
// handle → offset map for AcDb:Handles.
type wObjectWriter struct {
	*writer
	out     *wMemStream
	sw      *wMerged
	handles map[uint64]int64

	r13_14, r13_15, r2000, r2004pre, r2004, r2007, r2010, r2013, r2018 bool

	blockEntities  map[*cadoc.BlockRecord][]cadoc.Entity
	insertsByBlock map[*cadoc.BlockRecord][]*cadoc.Insert
	prev, next     cadoc.Entity
}

func newWObjectWriter(w *writer) *wObjectWriter {
	v := w.version
	ow := &wObjectWriter{writer: w, out: &wMemStream{}, sw: newWMerged(v, w.enc), handles: map[uint64]int64{},
		r13_14: v <= cadoc.AC1014, r13_15: v <= cadoc.AC1015, r2000: v >= cadoc.AC1015,
		r2004pre: v < cadoc.AC1018, r2004: v >= cadoc.AC1018, r2007: v >= cadoc.AC1021,
		r2010: v >= cadoc.AC1024, r2013: v >= cadoc.AC1027, r2018: v >= cadoc.AC1032,
		blockEntities: map[*cadoc.BlockRecord][]cadoc.Entity{}, insertsByBlock: map[*cadoc.BlockRecord][]*cadoc.Insert{}}
	if ms := w.doc.ModelSpace(); ms != nil {
		for _, e := range ms.Entities {
			if ins, ok := e.(*cadoc.Insert); ok && ins.Block != nil {
				ow.insertsByBlock[ins.Block] = append(ow.insertsByBlock[ins.Block], ins)
			}
		}
	}
	return ow
}

func (w *wObjectWriter) write() []byte {
	// R2004+: the section data starts with an RL 0x0DCA.
	if w.r2004 {
		w.out.writeInt32(0xDCA)
	}
	doc := w.doc
	w.writeBlockControl()
	writeWTable(w, doc.Layers, w.writeLayer)
	writeWTable(w, doc.TextStyles, w.writeTextStyle)
	w.writeLTypeControl()
	writeWTable(w, doc.Views, w.writeView)
	writeWTable(w, doc.UCSs, w.writeUCS)
	writeWTable(w, doc.VPorts, w.writeVPort)
	writeWTable(w, doc.AppIds, w.writeAppId)
	// The dimension styles have to be the last table.
	writeWTable(w, doc.DimensionStyles, w.writeDimensionStyle)
	if w.r2004pre {
		w.writeVEntityControl()
	}
	w.writeBlockEntities()
	w.writeObjects()
	return w.out.bytes()
}

// registerObject is DwgObjectWriter.registerObject: MS size, (R2010+) MC
// handle stream size in bits, the object data and its CRC.
func (w *wObjectWriter) registerObject(o cadoc.Object) {
	w.sw.writeSpearShift()
	position := int64(w.out.pos)
	data := w.sw.m.ms.bytes()
	start := w.out.pos

	size := uint32(len(data))
	if size >= 0x8000 {
		w.out.writeByte(byte(size))
		w.out.writeByte(byte(size>>8&0x7F | 0x80))
		w.out.writeByte(byte(size >> 15))
		w.out.writeByte(byte(size >> 23))
	} else {
		w.out.writeByte(byte(size))
		w.out.writeByte(byte(size >> 8))
	}
	if w.r2010 {
		sizeb := uint64(int64(len(data))<<3 - w.sw.savedPositionInBits())
		if sizeb == 0 {
			w.out.writeByte(0)
		} else {
			for sizeb != 0 {
				b := byte(sizeb & 0x7F)
				if sizeb>>7 != 0 {
					b |= 0x80
				}
				w.out.writeByte(b)
				sizeb >>= 7
			}
		}
	}
	w.out.write(data)
	crc := wCRC8(0xC0C1, w.out.bytes()[start:w.out.pos])
	w.out.writeInt16(int16(crc))
	w.handles[o.Base().Handle] = position
}

// objectTypeCode is the DWG type number written for an object: fixed types,
// or the class number for types without one (and LAYOUT before R2004).
func (w *wObjectWriter) objectTypeCode(o cadoc.Object) (int16, bool) {
	t := o.ObjectType()
	switch {
	case t == cadoc.ObjectTypeUnlisted || t == cadoc.ObjectTypeLayout && w.r2004pre:
		if c, ok := w.doc.Classes.Get(o.ObjectName()); ok {
			return c.ClassNumber, true
		}
		w.note(writerLevelWarning, "Dxf Class not found for %s", o.ObjectName())
		return 0, false
	case t == cadoc.ObjectTypeInvalid || t == cadoc.ObjectTypeUndefined:
		w.note(writerLevelNotImplemented, "CadObject type: %d name: %s", t, o.ObjectName())
		return 0, false
	}
	return int16(t), true
}

func (w *wObjectWriter) writeCommonData(o cadoc.Object, typeCode int16) {
	w.sw.resetStream()
	w.sw.writeObjectType(typeCode)
	if w.version >= cadoc.AC1015 && w.version < cadoc.AC1024 {
		// RL: size of the object in bits, patched by writeSpearShift.
		w.sw.savePositionForSize()
	}
	w.sw.main().handleReference(wRefUndefined, o.Base().Handle)
	w.writeExtendedData(o)
}

func (w *wObjectWriter) writeCommonNonEntityData(o cadoc.Object) bool {
	code, ok := w.objectTypeCode(o)
	if !ok {
		return false
	}
	w.writeCommonNonEntityDataType(o, code)
	return true
}

func (w *wObjectWriter) writeCommonNonEntityDataType(o cadoc.Object, code int16) {
	w.writeCommonData(o, code)
	if w.r13_14 {
		w.sw.savePositionForSize()
	}
	var owner uint64
	if ob := o.Base().Owner; !wIsNil(ob) {
		owner = ob.Base().Handle
	}
	w.sw.handleReference(wRefSoftPointer, owner)
	w.writeReactorsAndDictionaryHandle(o)
}

func (w *wObjectWriter) writeCommonEntityData(e cadoc.Entity) bool {
	code, ok := w.objectTypeCode(e)
	if !ok {
		return false
	}
	w.writeCommonData(e, code)
	eb := e.Entity()
	if len(eb.ProxyGraphics) > 0 {
		w.sw.writeBit(true)
		if w.version >= cadoc.AC1024 {
			w.sw.writeBitLongLong(int64(len(eb.ProxyGraphics)))
		} else {
			w.sw.writeRawLong(int64(len(eb.ProxyGraphics)))
		}
		w.sw.writeBytes(eb.ProxyGraphics)
	} else {
		w.sw.writeBit(false)
	}
	if w.r13_14 {
		w.sw.savePositionForSize()
	}
	w.writeEntityMode(e)
	return true
}

func (w *wObjectWriter) entMode(e cadoc.Entity) byte {
	owner := e.Base().Owner
	if wIsNil(owner) {
		return 0
	}
	if ps := w.doc.PaperSpace(); ps != nil && owner.Base().Handle == ps.Handle {
		return 0b01
	}
	if ms := w.doc.ModelSpace(); ms != nil && owner.Base().Handle == ms.Handle {
		return 0b10
	}
	return 0
}

func (w *wObjectWriter) writeEntityMode(e cadoc.Entity) {
	eb := e.Entity()
	mode := w.entMode(e)
	w.sw.write2Bits(mode)
	if mode == 0 {
		var owner uint64
		if !wIsNil(eb.Owner) {
			owner = eb.Owner.Base().Handle
		}
		w.sw.handleReference(wRefSoftPointer, owner)
	}
	w.writeReactorsAndDictionaryHandle(e)

	ltName := cadoc.ByLayerName
	if eb.LineType != nil {
		ltName = eb.LineType.Name
	}
	layer := w.handleOf(w.entityLayer(eb))
	if w.r13_14 {
		w.sw.handleReference(wRefHardPointer, layer)
		byLayer := strings.EqualFold(ltName, cadoc.ByLayerName)
		w.sw.writeBit(byLayer)
		// The line type handle is present when Isbylayerlt is 0 (ACadSharp
		// writes it the other way round).
		if !byLayer {
			w.sw.handleReference(wRefHardPointer, w.handleOf(eb.LineType))
		}
	}
	if !w.r2004 {
		// Nolinks: 1 when the previous and next entities have the adjacent
		// handles, else both are written.
		h := eb.Handle
		hasLinks := !wIsNil(w.prev) && w.prev.Base().Handle == h-1 && !wIsNil(w.next) && w.next.Base().Handle == h+1
		w.sw.writeBit(hasLinks)
		if !hasLinks {
			w.sw.handleReference(wRefSoftPointer, w.entityHandle(w.prev))
			w.sw.handleReference(wRefSoftPointer, w.entityHandle(w.next))
		}
	}

	isBook := eb.BookColor != nil && w.writable(eb.BookColor)
	color := eb.Color
	if isBook && eb.BookColor.Color.IsTrueColor() {
		color = eb.BookColor.Color
	}
	w.sw.writeEnColor(color, eb.Transparency, isBook)
	if w.r2004 && isBook {
		w.sw.handleReference(wRefHardPointer, w.handleOf(eb.BookColor))
	}
	w.sw.writeBitDouble(eb.LineTypeScale)
	if !w.r2000 {
		w.sw.writeBitShort(wB2S(eb.IsInvisible))
		return
	}
	w.sw.handleReference(wRefHardPointer, layer)
	switch {
	case strings.EqualFold(ltName, cadoc.ByLayerName):
		w.sw.write2Bits(0b00)
	case strings.EqualFold(ltName, cadoc.ByBlockName):
		w.sw.write2Bits(0b01)
	case strings.EqualFold(ltName, cadoc.ContinuousName):
		w.sw.write2Bits(0b10)
	default:
		w.sw.write2Bits(0b11)
		w.sw.handleReference(wRefHardPointer, w.handleOf(eb.LineType))
	}
	if w.r2007 {
		if mh := w.handleOf(eb.Material); mh == 0 {
			w.sw.write2Bits(0b00)
		} else {
			w.sw.write2Bits(0b11)
			w.sw.handleReference(wRefHardPointer, mh)
		}
		// Shadow flags.
		w.sw.writeByte(0)
	}
	// Plot style flags: by layer.
	w.sw.write2Bits(0b00)
	if w.version > cadoc.AC1021 {
		// No full / face / edge visual styles.
		w.sw.writeBit(false)
		w.sw.writeBit(false)
		w.sw.writeBit(false)
	}
	w.sw.writeBitShort(wB2S(eb.IsInvisible))
	w.sw.writeByte(cadoc.LineWeightIndex(eb.LineWeight))
}

func (w *wObjectWriter) entityLayer(eb *cadoc.EntityBase) cadoc.Object {
	if eb.Layer != nil {
		return eb.Layer
	}
	if l, ok := w.doc.Layers.Get(cadoc.DefaultLayerName); ok {
		return l
	}
	return nil
}

func (w *wObjectWriter) entityHandle(e cadoc.Entity) uint64 {
	if wIsNil(e) {
		return 0
	}
	return e.Base().Handle
}

func (w *wObjectWriter) writeReactorsAndDictionaryHandle(o cadoc.Object) {
	b := o.Base()
	var reactors []uint64
	for _, r := range b.Reactors {
		if h := w.handleOf(r); h != 0 {
			reactors = append(reactors, h)
		}
	}
	w.sw.writeBitLong(int32(len(reactors)))
	for _, h := range reactors {
		w.sw.handleReference(wRefSoftPointer, h)
	}
	var xdict uint64
	if b.XDictionary != nil && w.inDocument(b.XDictionary) {
		xdict = b.XDictionary.Handle
	}
	if w.r2004 {
		w.sw.writeBit(xdict == 0)
		if xdict != 0 {
			w.sw.handleReference(wRefHardOwnership, xdict)
		}
	} else {
		w.sw.handleReference(wRefHardOwnership, xdict)
	}
	if w.r2013 {
		// Has DS binary data.
		w.sw.writeBit(false)
	}
	if xdict != 0 {
		w.enqueue(b.XDictionary)
	}
}

// ----- extended data -----

func (w *wObjectWriter) writeExtendedData(o cadoc.Object) {
	if !w.cfg.SkipXData {
		for _, x := range o.Base().ExtendedData {
			app, ok := w.doc.AppIds.Get(x.AppName)
			if !ok || x.AppName == "" {
				continue
			}
			w.writeExtendedDataEntry(app.Handle, x.Records)
		}
	}
	w.sw.writeBitShort(0)
}

func (w *wObjectWriter) writeExtendedDataEntry(app uint64, records []cadoc.ExtendedDataRecord) {
	ms := &wMemStream{}
	putVec := func(p cadoc.Vec3) {
		ms.writeFloat64(p.X)
		ms.writeFloat64(p.Y)
		ms.writeFloat64(p.Z)
	}
	putHandle := func(h uint64) {
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], h)
		ms.write(b[:])
	}
	for _, r := range records {
		switch {
		case r.Code == 1000 || r.Code == 1001 || r.Code == 1002 || r.Code == 1003 || r.Code == 1005,
			r.Code >= 1010 && r.Code <= 1013, r.Code >= 1040 && r.Code <= 1042, r.Code == 1070, r.Code == 1071:
		case r.Code == 1004:
			// The chunk length is a byte: longer chunks are split.
			data := r.Bytes()
			for {
				n := min(len(data), 255)
				ms.writeByte(byte(r.Code - 1000))
				ms.writeByte(byte(n))
				ms.write(data[:n])
				data = data[n:]
				if len(data) == 0 {
					break
				}
			}
			continue
		default:
			// Not a DWG xdata code (R12 DXF splits points into 1020/1030
			// groups, for instance): ACadSharp fails on these.
			continue
		}
		ms.writeByte(byte(r.Code - 1000))
		switch {
		case r.Code == 1002:
			if r.String() == "}" {
				ms.writeByte(1)
			} else {
				ms.writeByte(0)
			}
		case r.Code == 1003:
			// A layer reference: the layer's handle.
			var h uint64
			if l, ok := w.doc.Layers.Get(r.String()); ok {
				h = l.Handle
			}
			putHandle(h)
		case r.Code == 1005:
			h := r.Handle()
			if _, ok := w.index[h]; !ok {
				h = 0
			}
			putHandle(h)
		case r.Code >= 1010 && r.Code <= 1013:
			putVec(r.Point())
		case r.Code >= 1040 && r.Code <= 1042:
			ms.writeFloat64(wAsFloat(r.Value))
		case r.Code == 1070:
			ms.writeInt16(int16(wAsInt(r.Value)))
		case r.Code == 1071:
			ms.writeInt32(int32(wAsInt(r.Value)))
		default:
			s := wAsString(r.Value)
			if w.r2007 {
				u := utf16.Encode([]rune(s))
				ms.writeInt16(int16(len(u) + 1))
				ms.write(wUTF16Bytes(u))
				ms.writeByte(0)
				ms.writeByte(0)
			} else {
				b := w.enc.encode(s)
				ms.writeInt16(int16(len(b)))
				ms.writeByte(w.enc.codeIndex)
				ms.write(b)
			}
		}
	}
	w.sw.writeBitShort(int16(ms.length()))
	w.sw.main().handleReference(wRefHardPointer, app)
	w.sw.writeBytes(ms.bytes())
}

// ----- tables -----

// writeWTable is DwgObjectWriter.writeTable: the control object and its
// entries.
func writeWTable[T cadoc.TableEntry](w *wObjectWriter, t *cadoc.Table[T], entry func(T)) {
	w.writeCommonNonEntityDataType(t, int16(t.ObjectType()))
	w.sw.writeBitLong(int32(t.Len()))
	if w.r2000 && t.ObjectType() == cadoc.ObjectTypeDimStyleControlObj {
		// Undocumented byte of the dimension style table.
		w.sw.writeByte(0)
	}
	for _, e := range t.Entries() {
		w.sw.handleReference(wRefSoftOwnership, e.Base().Handle)
	}
	w.registerObject(t)
	for _, e := range t.Entries() {
		entry(e)
	}
}

func (w *wObjectWriter) writeBlockControl() {
	t := w.doc.BlockRecords
	w.writeCommonNonEntityDataType(t, int16(t.ObjectType()))
	// Model and paper space aren't counted.
	var listed []*cadoc.BlockRecord
	for _, r := range t.Entries() {
		if strings.EqualFold(r.Name, cadoc.ModelSpaceName) || strings.EqualFold(r.Name, cadoc.PaperSpaceName) {
			continue
		}
		listed = append(listed, r)
	}
	w.sw.writeBitLong(int32(t.Len() - 2))
	for _, r := range listed {
		w.sw.handleReference(wRefSoftOwnership, r.Handle)
	}
	w.sw.handleReference(wRefHardOwnership, w.entityOrNil(w.doc.ModelSpace()))
	w.sw.handleReference(wRefHardOwnership, w.entityOrNil(w.doc.PaperSpace()))
	w.registerObject(t)
	for _, r := range t.Entries() {
		w.writeBlockRecord(r)
	}
}

func (w *wObjectWriter) entityOrNil(r *cadoc.BlockRecord) uint64 {
	if r == nil {
		return 0
	}
	return r.Handle
}

func (w *wObjectWriter) writeLTypeControl() {
	t := w.doc.LineTypes
	w.writeCommonNonEntityDataType(t, int16(t.ObjectType()))
	w.sw.writeBitLong(int32(t.Len() - 2))
	for _, lt := range t.Entries() {
		if strings.EqualFold(lt.Name, cadoc.ByBlockName) || strings.EqualFold(lt.Name, cadoc.ByLayerName) {
			continue
		}
		w.sw.handleReference(wRefSoftOwnership, lt.Handle)
	}
	// The line types end with BYBLOCK and BYLAYER.
	w.sw.handleReference(wRefHardOwnership, w.handleOf(w.lineTypeByName(cadoc.ByBlockName)))
	w.sw.handleReference(wRefHardOwnership, w.handleOf(w.lineTypeByName(cadoc.ByLayerName)))
	w.registerObject(t)
	for _, lt := range t.Entries() {
		w.writeLineType(lt)
	}
}

// writeVEntityControl writes the empty VPORT_ENTITY_HEADER control object
// of R13-R2000 files.
func (w *wObjectWriter) writeVEntityControl() {
	w.sw.resetStream()
	w.sw.writeObjectType(int16(cadoc.ObjectTypeVPEntHdrCtrlObj))
	if w.version >= cadoc.AC1015 {
		w.sw.savePositionForSize()
	}
	w.sw.main().handleReference(wRefUndefined, w.vEntityControlHandle)
	w.sw.writeBitShort(0) // no xdata
	if w.r13_14 {
		w.sw.savePositionForSize()
	}
	w.sw.handleReference(wRefSoftPointer, 0)
	w.sw.writeBitLong(0)
	w.sw.handleReference(wRefHardOwnership, 0)
	w.sw.writeBitLong(0)
	w.registerHandle(w.vEntityControlHandle)
}

func (w *wObjectWriter) registerHandle(h uint64) {
	o := &wHandleOnly{}
	o.Handle = h
	w.registerObject(o)
}

// wHandleOnly lets registerObject record objects without a model.
type wHandleOnly struct{ cadoc.ObjectBase }

func (*wHandleOnly) ObjectName() string           { return "" }
func (*wHandleOnly) SubclassMarker() string       { return "" }
func (*wHandleOnly) ObjectType() cadoc.ObjectType { return cadoc.ObjectTypeVPEntHdrCtrlObj }

// writeXrefDependantBit is the xref part of every table entry.
func (w *wObjectWriter) writeXrefDependantBit(e cadoc.TableEntry) {
	flags := e.EntryFlags()
	if w.r2007 {
		var v int16
		if flags&16 != 0 {
			v = 0b100000000
		}
		w.sw.writeBitShort(v)
		return
	}
	w.sw.writeBit(flags&64 != 0)
	w.sw.writeBitShort(0)
	w.sw.writeBit(flags&16 != 0)
}

func (w *wObjectWriter) writeAppId(a *cadoc.AppId) {
	w.writeCommonNonEntityDataType(a, int16(a.ObjectType()))
	w.sw.writeVariableText(a.Name)
	w.writeXrefDependantBit(a)
	// Unknown RC (71).
	w.sw.writeByte(0)
	w.sw.handleReference(wRefHardPointer, 0)
	w.registerObject(a)
}

func (w *wObjectWriter) writeBlockRecord(r *cadoc.BlockRecord) {
	entities := w.compatibleEntities(r.Entities)
	w.blockEntities[r] = entities

	w.writeCommonNonEntityDataType(r, int16(r.ObjectType()))
	flags := r.Flags()
	switch {
	case flags&cadoc.BlockTypeFlagsAnonymous != 0:
		// Anonymous blocks keep only "*" and the type letter.
		name := []rune(r.Name)
		w.sw.writeVariableText(string(name[:min(2, len(name))]))
	case r.Layout != nil:
		var b strings.Builder
		for _, c := range r.Name {
			if c < '0' || c > '9' {
				b.WriteRune(c)
			}
		}
		w.sw.writeVariableText(b.String())
	default:
		w.sw.writeVariableText(r.Name)
	}
	w.writeXrefDependantBit(r)
	isXref := flags&cadoc.BlockTypeFlagsXRef != 0
	isOverlay := flags&cadoc.BlockTypeFlagsXRefOverlay != 0
	w.sw.writeBit(flags&cadoc.BlockTypeFlagsAnonymous != 0)
	w.sw.writeBit(r.HasAttributes())
	w.sw.writeBit(isXref)
	w.sw.writeBit(isOverlay)
	block := r.BlockEntity
	if block == nil {
		block = cadoc.NewBlock()
	}
	if w.r2000 {
		w.sw.writeBit(block.IsUnloaded)
	}
	if w.r2004 && !isXref && !isOverlay {
		w.sw.writeBitLong(int32(len(entities)))
	}
	w.sw.write3BitDouble(block.BasePoint)
	w.sw.writeVariableText(block.XRefPath)
	inserts := w.insertsByBlock[r]
	if w.r2000 {
		for range inserts {
			w.sw.writeByte(1)
		}
		w.sw.writeByte(0)
		w.sw.writeVariableText(block.Comments)
		// Size of the preview data.
		w.sw.writeBitLong(0)
	}
	if w.r2007 {
		w.sw.writeBitShort(int16(r.Units))
		w.sw.writeBit(r.IsExplodable)
		w.sw.writeByte(byte(wB2S(r.CanScale)))
	}
	w.sw.handleReference(wRefHardPointer, 0)
	w.sw.handleReference(wRefHardOwnership, w.objHandle(r.BlockEntity))
	if w.r13_15 && !isXref && !isOverlay {
		if len(entities) > 0 {
			w.sw.handleReference(wRefSoftPointer, entities[0].Base().Handle)
			w.sw.handleReference(wRefSoftPointer, entities[len(entities)-1].Base().Handle)
		} else {
			w.sw.handleReference(wRefSoftPointer, 0)
			w.sw.handleReference(wRefSoftPointer, 0)
		}
	}
	if w.r2004 {
		for _, e := range entities {
			w.sw.handleReference(wRefHardOwnership, e.Base().Handle)
		}
	}
	w.sw.handleReference(wRefHardOwnership, w.objHandle(r.BlockEnd))
	if w.r2000 {
		for _, ins := range inserts {
			w.sw.handleReference(wRefSoftPointer, ins.Handle)
		}
		w.sw.handleReference(wRefHardPointer, w.handleOf(r.Layout))
	}
	w.registerObject(r)
}

func (w *wObjectWriter) objHandle(o cadoc.Object) uint64 {
	if wIsNil(o) {
		return 0
	}
	return o.Base().Handle
}

// compatibleEntities filters the entities that are written, reporting the
// others (getCompatibleEntities).
func (w *wObjectWriter) compatibleEntities(entities []cadoc.Entity) []cadoc.Entity {
	out := make([]cadoc.Entity, 0, len(entities))
	for _, e := range entities {
		if wIsNil(e) {
			continue
		}
		ok, reason := w.entitySupported(e)
		if !ok {
			if reason != "" {
				level := writerLevelNotImplemented
				if strings.HasPrefix(reason, "Invalid") {
					level = writerLevelWarning
				}
				w.note(level, "%s", reason)
			}
			continue
		}
		if !w.inDocument(e) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func (w *wObjectWriter) writeBlockEntities() {
	for _, r := range w.doc.BlockRecords.Entries() {
		w.prev, w.next = nil, nil
		if r.BlockEntity != nil {
			w.writeBlockBegin(r)
		}
		entities := w.blockEntities[r]
		for i, e := range entities {
			w.prev, w.next = nil, nil
			if i > 0 {
				w.prev = entities[i-1]
			}
			if i+1 < len(entities) {
				w.next = entities[i+1]
			}
			w.writeEntity(e)
		}
		w.prev, w.next = nil, nil
		if r.BlockEnd != nil {
			w.writeBlockEnd(r.BlockEnd)
		}
	}
}

func (w *wObjectWriter) writeBlockBegin(r *cadoc.BlockRecord) {
	if !w.writeCommonEntityData(r.BlockEntity) {
		return
	}
	w.sw.writeVariableText(r.Name)
	w.registerObject(r.BlockEntity)
}

func (w *wObjectWriter) writeBlockEnd(b *cadoc.BlockEnd) {
	if !w.writeCommonEntityData(b) {
		return
	}
	w.registerObject(b)
}

func (w *wObjectWriter) writeLayer(l *cadoc.Layer) {
	w.writeCommonNonEntityDataType(l, int16(l.ObjectType()))
	w.sw.writeVariableText(l.Name)
	w.writeXrefDependantBit(l)
	if w.r13_14 {
		w.sw.writeBit(l.Flags&cadoc.LayerFlagsFrozen != 0)
		w.sw.writeBit(!l.IsOn)
		w.sw.writeBit(l.Flags&cadoc.LayerFlagsFrozenNewViewports != 0)
		w.sw.writeBit(l.Flags&cadoc.LayerFlagsLocked != 0)
	}
	if w.r2000 {
		values := int16(cadoc.LineWeightIndex(l.LineWeight)) << 5
		if l.Flags&cadoc.LayerFlagsFrozen != 0 {
			values |= 0b1
		}
		if !l.IsOn {
			values |= 0b10
		}
		// Frozen in new viewports (ACadSharp tests the frozen flag here).
		if l.Flags&cadoc.LayerFlagsFrozenNewViewports != 0 {
			values |= 0b100
		}
		if l.Flags&cadoc.LayerFlagsLocked != 0 {
			values |= 0b1000
		}
		if l.Plots() {
			values |= 0b10000
		}
		w.sw.writeBitShort(values)
	}
	w.sw.writeCmColor(l.Color, false)
	w.sw.handleReference(wRefHardPointer, 0)
	if w.r2000 {
		// Plot style (ACadSharp writes null).
		var ps uint64
		if o, ok := w.index[l.PlotStyleName]; ok && w.writable(o) {
			ps = w.handleOf(o)
		}
		w.sw.handleReference(wRefHardPointer, ps)
	}
	if w.r2007 {
		w.sw.handleReference(wRefHardPointer, w.handleOf(l.Material))
	}
	lt := cadoc.Object(l.LineType)
	if l.LineType == nil {
		lt = w.lineTypeByName(cadoc.ContinuousName)
	}
	w.sw.handleReference(wRefHardPointer, w.handleOf(lt))
	if w.r2013 {
		w.sw.handleReference(wRefHardPointer, 0)
	}
	w.registerObject(l)
}

func (w *wObjectWriter) writeLineType(lt *cadoc.LineType) {
	w.writeCommonNonEntityDataType(lt, int16(lt.ObjectType()))
	w.sw.writeVariableText(lt.Name)
	w.writeXrefDependantBit(lt)
	w.sw.writeVariableText(lt.Description)
	w.sw.writeBitDouble(lt.PatternLength())
	align := lt.Alignment
	if align == 0 {
		align = 'A'
	}
	w.sw.writeByte(byte(align))
	w.sw.writeByte(byte(len(lt.Segments)))

	hasText := false
	for _, s := range lt.Segments {
		if s.Flags&cadoc.LineTypeShapeFlagsText != 0 {
			hasText = true
			break
		}
	}
	textEncode := w.enc.encode
	terminator := []byte{0}
	if w.r2007 {
		textEncode = wUTF16
		terminator = []byte{0, 0}
	}
	var textArea []byte
	textCursor := 0
	if w.version <= cadoc.AC1018 {
		textArea = make([]byte, 256)
		if w.version <= cadoc.AC1014 {
			textCursor = 1
		}
	} else if w.r2007 && hasText {
		textArea = make([]byte, 512)
	}
	for _, s := range lt.Segments {
		shape := s.ShapeNumber
		if s.Flags&cadoc.LineTypeShapeFlagsText != 0 {
			// The shape number of a text segment is its offset in the text
			// area.
			shape = 0
			if textArea != nil && s.Text != "" {
				tb := textEncode(s.Text)
				if textCursor+len(tb)+len(terminator) <= len(textArea) {
					shape = int16(textCursor)
					copy(textArea[textCursor:], tb)
					textCursor += len(tb)
					copy(textArea[textCursor:], terminator)
					textCursor += len(terminator)
				}
			}
		}
		w.sw.writeBitDouble(s.Length)
		w.sw.writeBitShort(shape)
		w.sw.writeRawDouble(s.Offset.X)
		w.sw.writeRawDouble(s.Offset.Y)
		w.sw.writeBitDouble(s.Scale)
		w.sw.writeBitDouble(s.Rotation)
		w.sw.writeBitShort(int16(s.Flags))
	}
	if w.version <= cadoc.AC1018 || w.r2007 && hasText {
		for _, b := range textArea {
			w.sw.writeByte(b)
		}
	}
	w.sw.handleReference(wRefHardPointer, 0)
	for _, s := range lt.Segments {
		w.sw.handleReference(wRefHardPointer, w.handleOf(s.Style))
	}
	w.registerObject(lt)
}

func (w *wObjectWriter) writeTextStyle(s *cadoc.TextStyle) {
	w.writeCommonNonEntityDataType(s, int16(s.ObjectType()))
	if s.IsShapeFile() {
		w.sw.writeVariableText("")
	} else {
		w.sw.writeVariableText(s.Name)
	}
	w.writeXrefDependantBit(s)
	w.sw.writeBit(s.Flags&cadoc.StyleFlagsIsShape != 0)
	w.sw.writeBit(s.Flags&cadoc.StyleFlagsVerticalText != 0)
	w.sw.writeBitDouble(s.Height)
	w.sw.writeBitDouble(s.Width)
	w.sw.writeBitDouble(s.ObliqueAngle)
	w.sw.writeByte(byte(s.MirrorFlag))
	w.sw.writeBitDouble(s.LastHeight)
	w.sw.writeVariableText(s.Filename)
	w.sw.writeVariableText(s.BigFontFilename)
	// ACadSharp writes the style control object as the xref block handle.
	w.sw.handleReference(wRefHardPointer, w.doc.TextStyles.Handle)
	w.registerObject(s)
}

func (w *wObjectWriter) writeUCS(u *cadoc.UCS) {
	w.writeCommonNonEntityDataType(u, int16(u.ObjectType()))
	w.sw.writeVariableText(u.Name)
	w.writeXrefDependantBit(u)
	w.sw.write3BitDouble(u.Origin)
	w.sw.write3BitDouble(u.XAxis)
	w.sw.write3BitDouble(u.YAxis)
	if w.r2000 {
		w.sw.writeBitDouble(u.Elevation)
		w.sw.writeBitShort(int16(u.OrthographicViewType))
		w.sw.writeBitShort(int16(u.OrthographicType))
	}
	w.sw.handleReference(wRefSoftPointer, w.doc.UCSs.Handle)
	if w.r2000 {
		w.sw.handleReference(wRefHardPointer, 0)
		w.sw.handleReference(wRefHardPointer, 0)
	}
	w.registerObject(u)
}

func (w *wObjectWriter) writeView(v *cadoc.View) {
	w.writeCommonNonEntityDataType(v, int16(v.ObjectType()))
	w.sw.writeVariableText(v.Name)
	w.writeXrefDependantBit(v)
	w.sw.writeBitDouble(v.Height)
	w.sw.writeBitDouble(v.Width)
	w.sw.write2RawDouble(v.Center)
	w.sw.write3BitDouble(v.Target)
	w.sw.write3BitDouble(v.Direction)
	w.sw.writeBitDouble(v.Angle)
	w.sw.writeBitDouble(v.LensLength)
	w.sw.writeBitDouble(v.FrontClipping)
	w.sw.writeBitDouble(v.BackClipping)
	w.sw.writeBit(v.ViewMode&cadoc.ViewModeTypePerspectiveView != 0)
	w.sw.writeBit(v.ViewMode&cadoc.ViewModeTypeFrontClipping != 0)
	w.sw.writeBit(v.ViewMode&cadoc.ViewModeTypeBackClipping != 0)
	w.sw.writeBit(v.ViewMode&cadoc.ViewModeTypeFrontClippingZ != 0)
	if w.r2000 {
		w.sw.writeByte(byte(v.RenderMode))
	}
	if w.r2007 {
		w.sw.writeBit(true)
		w.sw.writeByte(1)
		w.sw.writeBitDouble(0)
		w.sw.writeBitDouble(0)
		w.sw.writeCmColor(cadoc.ColorIndex(250), false)
	}
	w.sw.writeBit(v.Flags&1 != 0)
	if w.r2000 {
		w.sw.writeBit(v.IsUcsAssociated)
		if v.IsUcsAssociated {
			w.sw.write3BitDouble(v.UcsOrigin)
			w.sw.write3BitDouble(v.UcsXAxis)
			w.sw.write3BitDouble(v.UcsYAxis)
			w.sw.writeBitDouble(v.UcsElevation)
			w.sw.writeBitShort(int16(v.UcsOrthographicType))
		}
	}
	w.sw.handleReference(wRefSoftPointer, w.doc.Views.Handle)
	if w.r2007 {
		w.sw.writeBit(v.IsPlottable)
		w.sw.handleReference(wRefSoftPointer, 0)
		w.sw.handleReference(wRefHardPointer, 0)
		w.sw.handleReference(wRefHardOwnership, 0)
	}
	if w.r2000 && v.IsUcsAssociated {
		w.sw.handleReference(wRefHardPointer, w.handleOf(v.BaseUcs))
		w.sw.handleReference(wRefHardPointer, w.handleOf(v.NamedUcs))
	}
	if w.r2007 {
		w.sw.handleReference(wRefSoftPointer, 0)
	}
	w.registerObject(v)
}

func (w *wObjectWriter) writeVPort(v *cadoc.VPort) {
	w.writeCommonNonEntityDataType(v, int16(v.ObjectType()))
	w.sw.writeVariableText(v.Name)
	w.writeXrefDependantBit(v)
	w.sw.writeBitDouble(v.ViewHeight)
	// The aspect ratio is stored multiplied by the view height (R13 quirk).
	w.sw.writeBitDouble(v.AspectRatio * v.ViewHeight)
	w.sw.write2RawDouble(v.Center)
	w.sw.write3BitDouble(v.Target)
	w.sw.write3BitDouble(v.Direction)
	w.sw.writeBitDouble(v.TwistAngle)
	w.sw.writeBitDouble(v.LensLength)
	w.sw.writeBitDouble(v.FrontClippingPlane)
	w.sw.writeBitDouble(v.BackClippingPlane)
	w.sw.writeBit(v.ViewMode&cadoc.ViewModeTypePerspectiveView != 0)
	w.sw.writeBit(v.ViewMode&cadoc.ViewModeTypeFrontClipping != 0)
	w.sw.writeBit(v.ViewMode&cadoc.ViewModeTypeBackClipping != 0)
	w.sw.writeBit(v.ViewMode&cadoc.ViewModeTypeFrontClippingZ != 0)
	if w.r2000 {
		w.sw.writeByte(byte(v.RenderMode))
	}
	if w.r2007 {
		w.sw.writeBit(v.UseDefaultLighting)
		w.sw.writeByte(byte(v.DefaultLighting))
		w.sw.writeBitDouble(v.Brightness)
		w.sw.writeBitDouble(v.Contrast)
		w.sw.writeCmColor(v.AmbientColor, false)
	}
	w.sw.write2RawDouble(v.BottomLeft)
	w.sw.write2RawDouble(v.TopRight)
	w.sw.writeBit(v.ViewMode&cadoc.ViewModeTypeFollow != 0)
	w.sw.writeBitShort(v.CircleZoomPercent)
	w.sw.writeBit(true) // fast zoom
	w.sw.writeBit(v.UcsIconDisplay&cadoc.UscIconTypeOnLower != 0)
	w.sw.writeBit(v.UcsIconDisplay&cadoc.UscIconTypeOffOrigin != 0)
	w.sw.writeBit(v.ShowGrid)
	w.sw.write2RawDouble(v.GridSpacing)
	w.sw.writeBit(v.SnapOn)
	w.sw.writeBit(v.IsometricSnap)
	w.sw.writeBitShort(v.SnapIsoPair)
	w.sw.writeBitDouble(v.SnapRotation)
	w.sw.write2RawDouble(v.SnapBasePoint)
	w.sw.write2RawDouble(v.SnapSpacing)
	if w.r2000 {
		w.sw.writeBit(false)
		w.sw.writeBit(true) // UCS per viewport
		w.sw.write3BitDouble(v.Origin)
		w.sw.write3BitDouble(v.XAxis)
		w.sw.write3BitDouble(v.YAxis)
		w.sw.writeBitDouble(v.Elevation)
		w.sw.writeBitShort(int16(v.OrthographicType))
	}
	if w.r2007 {
		w.sw.writeBitShort(int16(v.GridFlags))
		w.sw.writeBitShort(v.MinorGridLinesPerMajorGridLine)
	}
	w.sw.handleReference(wRefHardPointer, 0)
	if w.r2007 {
		w.sw.handleReference(wRefSoftPointer, 0)
		w.sw.handleReference(wRefHardPointer, 0)
		w.sw.handleReference(wRefHardOwnership, 0)
	}
	if w.r2000 {
		if v.OrthographicType == cadoc.OrthographicTypeNone {
			w.sw.handleReference(wRefHardPointer, w.handleOf(v.NamedUcs))
			w.sw.handleReference(wRefHardPointer, 0)
		} else {
			w.sw.handleReference(wRefHardPointer, 0)
			w.sw.handleReference(wRefHardPointer, w.handleOf(v.BaseUcs))
		}
	}
	w.registerObject(v)
}

func (w *wObjectWriter) writeDimensionStyle(d *cadoc.DimensionStyle) {
	w.writeCommonNonEntityDataType(d, int16(d.ObjectType()))
	s := w.sw
	s.writeVariableText(d.Name)
	w.writeXrefDependantBit(d)
	name := func(r *cadoc.BlockRecord) string {
		if r == nil {
			return ""
		}
		return r.Name
	}
	if w.r13_14 {
		s.writeBit(d.GenerateTolerances)
		s.writeBit(d.LimitsGeneration)
		// DIMTIH, DIMTOH, DIMSE1, DIMSE2 in spec order (ACadSharp shifts
		// them by one).
		s.writeBit(d.TextInsideHorizontal)
		s.writeBit(d.TextOutsideHorizontal)
		s.writeBit(d.SuppressFirstExtensionLine)
		s.writeBit(d.SuppressSecondExtensionLine)
		s.writeBit(d.AlternateUnitDimensioning)
		s.writeBit(d.TextOutsideExtensions)
		s.writeBit(d.SeparateArrowBlocks)
		s.writeBit(d.TextInsideExtensions)
		s.writeBit(d.SuppressOutsideExtensions)
		s.writeByte(byte(d.AlternateUnitDecimalPlaces))
		s.writeByte(byte(d.ZeroHandling))
		s.writeBit(d.SuppressFirstDimensionLine)
		s.writeBit(d.SuppressSecondDimensionLine)
		s.writeByte(byte(d.ToleranceAlignment))
		s.writeByte(byte(d.TextHorizontalAlignment))
		s.writeByte(byte(d.DimensionFit))
		s.writeBit(d.CursorUpdate)
		s.writeByte(byte(d.ToleranceZeroHandling))
		s.writeByte(byte(d.AlternateUnitZeroHandling))
		s.writeByte(byte(d.AlternateUnitToleranceZeroHandling))
		s.writeByte(byte(d.TextVerticalAlignment))
		s.writeBitShort(d.DimensionUnit)
		s.writeBitShort(int16(d.AngularUnit))
		s.writeBitShort(d.DecimalPlaces)
		s.writeBitShort(d.ToleranceDecimalPlaces)
		s.writeBitShort(int16(d.AlternateUnitFormat))
		s.writeBitShort(d.AlternateUnitToleranceDecimalPlaces)
		for _, v := range []float64{d.ScaleFactor, d.ArrowSize, d.ExtensionLineOffset, d.DimensionLineIncrement,
			d.ExtensionLineExtension, d.Rounding, d.DimensionLineExtension, d.PlusTolerance, d.MinusTolerance,
			d.TextHeight, d.CenterMarkSize, d.TickSize, d.AlternateUnitScaleFactor, d.LinearScaleFactor,
			d.TextVerticalPosition, d.ToleranceScaleFactor, d.DimensionLineGap} {
			s.writeBitDouble(v)
		}
		s.writeVariableText(d.PostFix)
		s.writeVariableText(d.AlternateDimensioningSuffix)
		s.writeVariableText(name(d.ArrowBlock))
		s.writeVariableText(name(d.DimArrow1))
		s.writeVariableText(name(d.DimArrow2))
		s.writeCmColor(d.DimensionLineColor, false)
		s.writeCmColor(d.ExtensionLineColor, false)
		s.writeCmColor(d.TextColor, false)
	}
	if w.r2000 {
		s.writeVariableText(d.PostFix)
		s.writeVariableText(d.AlternateDimensioningSuffix)
		for _, v := range []float64{d.ScaleFactor, d.ArrowSize, d.ExtensionLineOffset, d.DimensionLineIncrement,
			d.ExtensionLineExtension, d.Rounding, d.DimensionLineExtension, d.PlusTolerance, d.MinusTolerance} {
			s.writeBitDouble(v)
		}
	}
	if w.r2007 {
		s.writeBitDouble(d.FixedExtensionLineLength)
		s.writeBitDouble(d.JoggedRadiusDimensionTransverseSegmentAngle)
		s.writeBitShort(int16(d.TextBackgroundFillMode))
		s.writeCmColor(d.TextBackgroundColor, false)
	}
	if w.r2000 {
		s.writeBit(d.GenerateTolerances)
		s.writeBit(d.LimitsGeneration)
		s.writeBit(d.TextInsideHorizontal)
		s.writeBit(d.TextOutsideHorizontal)
		s.writeBit(d.SuppressFirstExtensionLine)
		s.writeBit(d.SuppressSecondExtensionLine)
		s.writeBitShort(int16(d.TextVerticalAlignment))
		s.writeBitShort(int16(d.ZeroHandling))
		s.writeBitShort(int16(d.AngularZeroHandling))
	}
	if w.r2007 {
		s.writeBitShort(int16(d.ArcLengthSymbolPosition))
	}
	if w.r2000 {
		for _, v := range []float64{d.TextHeight, d.CenterMarkSize, d.TickSize, d.AlternateUnitScaleFactor,
			d.LinearScaleFactor, d.TextVerticalPosition, d.ToleranceScaleFactor, d.DimensionLineGap,
			d.AlternateUnitRounding} {
			s.writeBitDouble(v)
		}
		s.writeBit(d.AlternateUnitDimensioning)
		s.writeBitShort(d.AlternateUnitDecimalPlaces)
		s.writeBit(d.TextOutsideExtensions)
		s.writeBit(d.SeparateArrowBlocks)
		s.writeBit(d.TextInsideExtensions)
		s.writeBit(d.SuppressOutsideExtensions)
		s.writeCmColor(d.DimensionLineColor, false)
		s.writeCmColor(d.ExtensionLineColor, false)
		s.writeCmColor(d.TextColor, false)
		s.writeBitShort(d.AngularDecimalPlaces)
		s.writeBitShort(d.DecimalPlaces)
		s.writeBitShort(d.ToleranceDecimalPlaces)
		s.writeBitShort(int16(d.AlternateUnitFormat))
		s.writeBitShort(d.AlternateUnitToleranceDecimalPlaces)
		s.writeBitShort(int16(d.AngularUnit))
		s.writeBitShort(int16(d.FractionFormat))
		s.writeBitShort(int16(d.LinearUnitFormat))
		s.writeBitShort(int16(d.DecimalSeparator))
		s.writeBitShort(int16(d.TextMovement))
		s.writeBitShort(int16(d.TextHorizontalAlignment))
		s.writeBit(d.SuppressFirstDimensionLine)
		s.writeBit(d.SuppressSecondDimensionLine)
		s.writeBitShort(int16(d.ToleranceAlignment))
		s.writeBitShort(int16(d.ToleranceZeroHandling))
		s.writeBitShort(int16(d.AlternateUnitZeroHandling))
		s.writeBitShort(int16(d.AlternateUnitToleranceZeroHandling))
		s.writeBit(d.CursorUpdate)
		// DIMFIT: ACadSharp always writes 3.
		s.writeBitShort(3)
	}
	if w.r2007 {
		s.writeBit(d.IsExtensionLineLengthFixed)
	}
	if w.r2010 {
		s.writeBit(d.TextDirection == cadoc.TextDirectionRightToLeft)
		s.writeBitDouble(d.AltMzf)
		s.writeVariableText(d.AltMzs)
		s.writeBitDouble(d.Mzf)
		s.writeVariableText(d.Mzs)
	}
	if w.r2000 {
		s.writeBitShort(int16(d.DimensionLineWeight))
		s.writeBitShort(int16(d.ExtensionLineWeight))
	}
	// Unknown bit (bit 0 of the 70 group).
	s.writeBit(false)
	s.handleReference(wRefHardPointer, 0)
	s.handleReference(wRefHardPointer, w.handleOf(d.Style))
	if w.r2000 {
		s.handleReference(wRefHardPointer, w.handleOf(d.LeaderArrow))
		s.handleReference(wRefHardPointer, w.handleOf(d.ArrowBlock))
		s.handleReference(wRefHardPointer, w.handleOf(d.DimArrow1))
		s.handleReference(wRefHardPointer, w.handleOf(d.DimArrow2))
	}
	if w.r2007 {
		s.handleReference(wRefHardPointer, w.handleOf(d.LineType))
		s.handleReference(wRefHardPointer, w.handleOf(d.LineTypeExt1))
		s.handleReference(wRefHardPointer, w.handleOf(d.LineTypeExt2))
	}
	w.registerObject(d)
}

// ----- value helpers -----

func wB2S(b bool) int16 {
	if b {
		return 1
	}
	return 0
}

func wAsFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int16:
		return float64(x)
	case int32:
		return float64(x)
	case int64:
		return float64(x)
	case int:
		return float64(x)
	case uint8:
		return float64(x)
	case bool:
		if x {
			return 1
		}
	}
	return 0
}

func wAsInt(v any) int64 {
	switch x := v.(type) {
	case int16:
		return int64(x)
	case int32:
		return int64(x)
	case int64:
		return x
	case int:
		return int64(x)
	case uint8:
		return int64(x)
	case uint16:
		return int64(x)
	case uint32:
		return int64(x)
	case uint64:
		return int64(x)
	case float64:
		if math.IsNaN(x) {
			return 0
		}
		return int64(x)
	case bool:
		if x {
			return 1
		}
	}
	return 0
}

func wAsString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	case []byte:
		return string(x)
	}
	return ""
}
