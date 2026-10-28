package dwg

import (
	"encoding/binary"
	"fmt"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// readObjects reads the handle map and every object reachable from the
// header handles (DwgReader.readObjects).
func (r *reader) readObjects() {
	handles := r.readHandles()
	var s *BitReader
	if r.fh.version <= cadoc.AC1015 {
		// Handles are absolute file offsets in these versions.
		s = NewBitReader(r.fh.version, r.data, 0, r.text)
	} else {
		s = r.sectionStream(SectionAcDbObjects)
		if s == nil {
			panic(errorf("objects section not found"))
		}
	}
	var queue []uint64
	for _, h := range r.b.headerHandles.handles() {
		queue = append(queue, h)
	}
	or := newObjectReader(r, s, queue, handles)
	or.read()
}

// objectReader is DwgObjectReader.
type objectReader struct {
	r   *reader
	b   *builder
	ver cadoc.Version
	versionFeatures

	classes map[int16]*cadoc.DxfClass
	buf     []byte
	crc     *BitReader

	queue   []uint64
	offsets map[uint64]int64
	readed  map[uint64]cadoc.ObjectType

	// obj reads the object data, text the strings and hnd the handles; for
	// versions before R2007 text is obj.
	obj, text, hnd *BitReader
	merged         *mergedReader

	handleSectionOffset int64
	objectInitialPos    int64
	size                uint32
}

func newObjectReader(r *reader, s *BitReader, queue []uint64, handles *handleMap) *objectReader {
	or := &objectReader{r: r, b: r.b, ver: r.fh.version, versionFeatures: newVersionFeatures(r.fh.version),
		classes: map[int16]*cadoc.DxfClass{}, buf: s.Buf, queue: queue, offsets: handles.offsets,
		readed: map[uint64]cadoc.ObjectType{}, handleSectionOffset: -1}
	for _, c := range r.doc.Classes.Entries() {
		if _, dup := or.classes[c.ClassNumber]; !dup {
			or.classes[c.ClassNumber] = c
		}
	}
	or.crc = NewBitReader(or.ver, or.buf, 0, r.text)
	return or
}

func (or *objectReader) notify(level int, format string, args ...any) {
	or.r.note(level, format, args...)
}

func (or *objectReader) newReader() *BitReader { return NewBitReader(or.ver, or.buf, 0, or.r.text) }

func (or *objectReader) read() {
	for len(or.queue) > 0 {
		h := or.queue[0]
		or.queue = or.queue[1:]
		offset, ok := or.offsets[h]
		if !ok || or.b.hasTemplate(h) {
			continue
		}
		if _, done := or.readed[h]; done {
			continue
		}
		var typ cadoc.ObjectType
		t, err := or.readSafe(func() *tmpl {
			typ = or.objectType(offset)
			or.readed[h] = typ
			return or.readObject(typ)
		})
		if err != nil {
			if _, done := or.readed[h]; !done {
				or.readed[h] = cadoc.ObjectTypeInvalid
			}
			if !or.r.cfg.Failsafe {
				panic(err)
			}
			if c, ok := or.classes[int16(typ)]; ok {
				or.notify(NotifyError, "Could not read %s number %d with handle: %d: %v", c.DxfName, c.ClassNumber, h, err)
			} else {
				or.notify(NotifyError, "Could not read %s with handle: %d: %v", objectTypeName(typ), h, err)
			}
			continue
		}
		if t == nil {
			continue
		}
		or.b.addTemplate(t)
	}
}

// readSafe runs a read function and turns a panic into an error.
func (or *objectReader) readSafe(fn func() *tmpl) (t *tmpl, err error) {
	defer func() {
		if p := recover(); p != nil {
			if e, ok := p.(error); ok {
				err = e
			} else {
				err = fmt.Errorf("%v", p)
			}
			t = nil
		}
	}()
	return fn(), nil
}

// objectType positions the readers on the object at offset and reads its
// type (getEntityType).
func (or *objectReader) objectType(offset int64) cadoc.ObjectType {
	or.crc.SetPosition(int(offset))
	or.size = uint32(or.crc.ReadModularShort())
	if or.size <= 0 {
		return cadoc.ObjectTypeInvalid
	}
	sizeInBits := or.size << 3
	if or.R2010Plus {
		handleSize := or.crc.ReadModularChar()
		or.handleSectionOffset = int64(uint64(or.crc.PositionInBits()) + uint64(sizeInBits) - handleSize)
		or.obj = or.newReader()
		or.obj.SetPositionInBits(or.crc.PositionInBits())
		or.objectInitialPos = or.obj.PositionInBits()
		typ := or.obj.ReadObjectType()
		or.hnd = or.newReader()
		or.hnd.SetPositionInBits(or.handleSectionOffset)
		or.text = or.newReader()
		or.text.SetPositionByFlag(or.handleSectionOffset - 1)
		or.merged = newMerged(or.obj, or.text, or.hnd)
		return typ
	}
	or.obj = or.newReader()
	or.obj.SetPositionInBits(or.crc.PositionInBits())
	or.hnd = or.newReader()
	or.text = or.obj
	or.objectInitialPos = or.obj.PositionInBits()
	return or.obj.ReadObjectType()
}

// handleReference reads a handle from the handle stream and queues the
// object it refers to.
func (or *objectReader) handleReference(ref uint64) uint64 {
	v := or.hnd.HandleReference(ref)
	if v != 0 && !or.b.hasTemplate(v) {
		if _, done := or.readed[v]; !done {
			or.queue = append(or.queue, v)
		}
	}
	return v
}

func (or *objectReader) handleRef() uint64 { return or.handleReference(0) }

// updateHandleReader positions the handle (and R2007 string) stream after
// the object data, whose size in bits precedes it.
func (or *objectReader) updateHandleReader() {
	size := or.obj.ReadRawLong()
	or.hnd.SetPositionInBits(size + or.objectInitialPos)
	if or.ver == cadoc.AC1021 {
		or.text = or.newReader()
		or.text.SetPositionByFlag(size + or.objectInitialPos - 1)
	}
	or.merged = newMerged(or.obj, or.text, or.hnd)
}

// ----- templates -----

func (or *objectReader) newObjectTmpl(o cadoc.Object) *tmpl { return &tmpl{obj: o} }

func (or *objectReader) newEntityTmpl(e cadoc.Entity) *tmpl {
	return &tmpl{obj: e, ent: &entityTmpl{ltypeFlags: -1}}
}

// ----- common data -----

func (or *objectReader) readCommonData(t *tmpl) {
	if or.ver >= cadoc.AC1015 && or.ver < cadoc.AC1024 {
		or.updateHandleReader()
	}
	t.obj.Base().Handle = or.obj.HandleReference(0)
	or.readExtendedData(t)
}

// readCommonEntityData reads the data every entity starts with.
func (or *objectReader) readCommonEntityData(t *tmpl) {
	or.readCommonData(t)
	if or.obj.ReadBit() {
		var n int64
		if or.ver >= cadoc.AC1024 {
			n = or.obj.ReadBitLongLong()
		} else {
			n = or.obj.ReadRawLong()
		}
		t.ent.proxyGraphics = or.obj.ReadBytes(int(n))
	}
	if or.ver >= cadoc.AC1012 && or.ver <= cadoc.AC1014 {
		or.updateHandleReader()
	}
	or.readEntityMode(t)
}

// readCommonNonEntityData reads the data every non-entity object starts with.
func (or *objectReader) readCommonNonEntityData(t *tmpl) {
	or.readCommonData(t)
	if or.R13_14Only {
		or.updateHandleReader()
	}
	t.owner = or.handleReference(t.obj.Base().Handle)
	t.hasOwner = true
	or.readReactorsAndDictionaryHandle(t)
}

func (or *objectReader) readReactorsAndDictionaryHandle(t *tmpl) {
	n := int(or.obj.ReadBitLong())
	for i := 0; i < n; i++ {
		t.addReactor(or.handleRef())
	}
	missing := false
	if or.R2004Plus {
		missing = or.obj.ReadBit()
	}
	if !missing {
		t.xdict = or.handleRef()
		t.hasXDict = true
	}
	if or.R2013Plus {
		t.hasDsBinaryData = or.obj.ReadBit()
	}
}

// readEntityMode reads the owner, reactors, layer, line type, colour and
// the other common entity properties.
func (or *objectReader) readEntityMode(t *tmpl) {
	e := t.obj.(cadoc.Entity).Entity()
	et := t.ent
	et.entityMode = or.obj.Read2Bits()
	if et.entityMode == 0 {
		t.owner = or.hnd.HandleReference(e.Handle)
		t.hasOwner = true
	}
	or.readReactorsAndDictionaryHandle(t)
	if or.R13_14Only {
		et.layer = or.handleRef()
		if !or.obj.ReadBit() {
			et.ltype = or.handleRef()
			et.hasLtype = true
		}
	}
	if !or.R2004Plus && !or.obj.ReadBit() {
		et.prev = or.handleReference(e.Handle)
		et.next = or.handleReference(e.Handle)
		et.hasNext = true
	} else if !or.R2004Plus {
		// No links: the neighbours are handle-1 and handle+1.
		if _, ok := or.readed[e.Handle-1]; !ok {
			or.queue = append(or.queue, e.Handle-1)
		}
		if _, ok := or.readed[e.Handle+1]; !ok {
			or.queue = append(or.queue, e.Handle+1)
		}
	}
	c, tr, isBook := or.obj.ReadEnColor()
	e.Color, e.Transparency = c, tr
	if or.ver >= cadoc.AC1018 && isBook {
		et.bookColor = or.handleRef()
	}
	e.LineTypeScale = or.obj.ReadBitDouble()
	if or.ver < cadoc.AC1015 {
		e.IsInvisible = or.obj.ReadBitShort()&1 == 1
		return
	}
	et.layer = or.handleRef()
	et.ltypeFlags = int(or.obj.Read2Bits())
	if et.ltypeFlags == 3 {
		et.ltype = or.handleRef()
		et.hasLtype = true
	}
	if or.R2007Plus {
		if or.obj.Read2Bits() == 3 {
			et.material = or.handleRef()
		}
		or.obj.ReadRC() // shadow flags
	}
	if or.obj.Read2Bits() == 3 {
		or.handleRef() // plot style
	}
	if or.R2010Plus {
		for i := 0; i < 3; i++ {
			// Full, face and edge visual styles.
			if or.obj.ReadBit() {
				or.handleRef()
			}
		}
	}
	e.IsInvisible = or.obj.ReadBitShort()&1 == 1
	e.LineWeight = cadoc.LineWeightFromIndex(or.obj.ReadRC())
}

// readExtendedData reads the EED that follows the object handle.
func (or *objectReader) readExtendedData(t *tmpl) {
	size := or.obj.ReadBitShort()
	for size != 0 {
		app := or.obj.HandleReference(0)
		end := or.obj.Pos + int(size)
		recs := or.readExtendedDataRecords(end)
		for _, e := range t.edata {
			if e.app == app {
				panic(errorf("An item with the same key has already been added. Key: %d", app))
			}
		}
		t.edata = append(t.edata, edataTmpl{app: app, records: recs})
		size = or.obj.ReadBitShort()
	}
}

func (or *objectReader) readExtendedDataRecords(end int) []cadoc.ExtendedDataRecord {
	var recs []cadoc.ExtendedDataRecord
	s := or.obj
	for s.Pos < end {
		code := 1000 + int(s.ReadRC())
		var rec cadoc.ExtendedDataRecord
		switch code {
		case 1000, 1001:
			rec = cadoc.ExtendedDataRecord{Code: 1000, Value: s.ReadTextUnicode()}
		case 1002:
			v := "{"
			if s.ReadRC() == 1 {
				v = "}"
			}
			rec = cadoc.ExtendedDataRecord{Code: 1002, Value: v}
		case 1003:
			rec = cadoc.ExtendedDataRecord{Code: 1003, Value: binary.BigEndian.Uint64(s.ReadBytes(8))}
		case 1004:
			n := int(s.ReadRC())
			rec = cadoc.ExtendedDataRecord{Code: 1004, Value: s.ReadBytes(n)}
		case 1005:
			rec = cadoc.ExtendedDataRecord{Code: 1005, Value: binary.BigEndian.Uint64(s.ReadBytes(8))}
		case 1010, 1011, 1012, 1013:
			x := s.ReadDouble()
			y := s.ReadDouble()
			rec = cadoc.ExtendedDataRecord{Code: code, Value: cadoc.Vec3{X: x, Y: y, Z: s.ReadDouble()}}
		case 1040, 1041, 1042:
			rec = cadoc.ExtendedDataRecord{Code: code, Value: s.ReadDouble()}
		case 1070:
			rec = cadoc.ExtendedDataRecord{Code: 1070, Value: s.ReadShort()}
		case 1071:
			rec = cadoc.ExtendedDataRecord{Code: 1071, Value: s.ReadInt()}
		default:
			s.ReadBytes(end - s.Pos)
			or.notify(NotifyWarning, "Unknown code for extended data: %d", code)
			return recs
		}
		recs = append(recs, rec)
	}
	return recs
}

// readXrefDependantBit reads the xref flags of a table entry.
func (or *objectReader) readXrefDependantBit(e cadoc.TableEntry) {
	// ACadSharp sets these bits on TableEntry.Flags, which BlockRecord hides
	// behind its block flags: for blocks they are read and dropped.
	_, isBlock := e.(*cadoc.BlockRecord)
	if isBlock {
		defer func(f int16) { e.SetEntryFlags(f) }(e.EntryFlags())
	}
	flags := e.EntryFlags()
	if or.R2007Plus {
		xrefIndex := or.obj.ReadBitShort()
		if uint16(xrefIndex)&0b100000000 > 0 {
			flags |= int16(cadoc.StandardFlagsXrefDependent)
		}
	} else {
		if or.obj.ReadBit() {
			flags |= int16(cadoc.StandardFlagsReferenced)
		}
		or.obj.ReadBitShort() // xref index + 1
		if or.obj.ReadBit() {
			flags |= int16(cadoc.StandardFlagsXrefDependent)
		}
	}
	e.SetEntryFlags(flags)
}

// readUnknownEntity reads only the common entity data of an entity without
// a model.
func (or *objectReader) readUnknownEntity(c *cadoc.DxfClass, dxfName string) *tmpl {
	e := &cadoc.UnknownEntity{DxfName: dxfName, Class: c}
	e.InitEntity()
	if c != nil {
		e.DxfName = c.DxfName
	}
	t := or.newEntityTmpl(e)
	or.readCommonEntityData(t)
	return t
}

// readUnknownObject reads only the common data of an object without a model.
func (or *objectReader) readUnknownObject(c *cadoc.DxfClass, dxfName string) *tmpl {
	o := &cadoc.UnknownObject{DxfName: dxfName, Class: c}
	if c != nil {
		o.DxfName = c.DxfName
	}
	t := or.newObjectTmpl(o)
	or.readCommonNonEntityData(t)
	return t
}

func objectTypeName(t cadoc.ObjectType) string {
	if n, ok := objectTypeNames[t]; ok {
		return n
	}
	return fmt.Sprintf("%d", int16(t))
}

var objectTypeNames = map[cadoc.ObjectType]string{
	cadoc.ObjectTypeText: "TEXT", cadoc.ObjectTypeAttrib: "ATTRIB", cadoc.ObjectTypeAttdef: "ATTDEF",
	cadoc.ObjectTypeBlock: "BLOCK", cadoc.ObjectTypeEndblk: "ENDBLK", cadoc.ObjectTypeSeqend: "SEQEND",
	cadoc.ObjectTypeInsert: "INSERT", cadoc.ObjectTypeMInsert: "MINSERT", cadoc.ObjectTypeVertex2D: "VERTEX_2D",
	cadoc.ObjectTypeVertex3D: "VERTEX_3D", cadoc.ObjectTypeVertexMesh: "VERTEX_MESH",
	cadoc.ObjectTypeVertexPFace: "VERTEX_PFACE", cadoc.ObjectTypeVertexPFaceFace: "VERTEX_PFACE_FACE",
	cadoc.ObjectTypePolyline2D: "POLYLINE_2D", cadoc.ObjectTypePolyline3D: "POLYLINE_3D", cadoc.ObjectTypeArc: "ARC",
	cadoc.ObjectTypeCircle: "CIRCLE", cadoc.ObjectTypeLine: "LINE", cadoc.ObjectTypeDimensionOrdinate: "DIMENSION_ORDINATE",
	cadoc.ObjectTypeDimensionLinear: "DIMENSION_LINEAR", cadoc.ObjectTypeDimensionAligned: "DIMENSION_ALIGNED",
	cadoc.ObjectTypeDimensionAng3Pt: "DIMENSION_ANG_3_Pt", cadoc.ObjectTypeDimensionAng2Ln: "DIMENSION_ANG_2_Ln",
	cadoc.ObjectTypeDimensionRadius: "DIMENSION_RADIUS", cadoc.ObjectTypeDimensionDiameter: "DIMENSION_DIAMETER",
	cadoc.ObjectTypePoint: "POINT", cadoc.ObjectTypeFace3D: "FACE3D", cadoc.ObjectTypePolylinePFace: "POLYLINE_PFACE",
	cadoc.ObjectTypePolylineMesh: "POLYLINE_MESH", cadoc.ObjectTypeSolid: "SOLID", cadoc.ObjectTypeTrace: "TRACE",
	cadoc.ObjectTypeShape: "SHAPE", cadoc.ObjectTypeViewport: "VIEWPORT", cadoc.ObjectTypeEllipse: "ELLIPSE",
	cadoc.ObjectTypeSpline: "SPLINE", cadoc.ObjectTypeRegion: "REGION", cadoc.ObjectTypeSolid3D: "SOLID3D",
	cadoc.ObjectTypeBody: "BODY", cadoc.ObjectTypeRay: "RAY", cadoc.ObjectTypeXLine: "XLINE",
	cadoc.ObjectTypeDictionary: "DICTIONARY", cadoc.ObjectTypeOleFrame: "OLEFRAME", cadoc.ObjectTypeMText: "MTEXT",
	cadoc.ObjectTypeLeader: "LEADER", cadoc.ObjectTypeTolerance: "TOLERANCE", cadoc.ObjectTypeMLine: "MLINE",
	cadoc.ObjectTypeBlockControlObj: "BLOCK_CONTROL_OBJ", cadoc.ObjectTypeBlockHeader: "BLOCK_HEADER",
	cadoc.ObjectTypeLayerControlObj: "LAYER_CONTROL_OBJ", cadoc.ObjectTypeLayer: "LAYER",
	cadoc.ObjectTypeStyleControlObj: "STYLE_CONTROL_OBJ", cadoc.ObjectTypeStyle: "STYLE",
	cadoc.ObjectTypeLTypeControlObj: "LTYPE_CONTROL_OBJ", cadoc.ObjectTypeLType: "LTYPE",
	cadoc.ObjectTypeViewControlObj: "VIEW_CONTROL_OBJ", cadoc.ObjectTypeView: "VIEW",
	cadoc.ObjectTypeUCSControlObj: "UCS_CONTROL_OBJ", cadoc.ObjectTypeUCS: "UCS",
	cadoc.ObjectTypeVPortControlObj: "VPORT_CONTROL_OBJ", cadoc.ObjectTypeVPort: "VPORT",
	cadoc.ObjectTypeAppIdControlObj: "APPID_CONTROL_OBJ", cadoc.ObjectTypeAppId: "APPID",
	cadoc.ObjectTypeDimStyleControlObj: "DIMSTYLE_CONTROL_OBJ", cadoc.ObjectTypeDimStyle: "DIMSTYLE",
	cadoc.ObjectTypeVPEntHdrCtrlObj: "VP_ENT_HDR_CTRL_OBJ", cadoc.ObjectTypeVPEntHdr: "VP_ENT_HDR",
	cadoc.ObjectTypeGroup: "GROUP", cadoc.ObjectTypeMLineStyle: "MLINESTYLE", cadoc.ObjectTypeOle2Frame: "OLE2FRAME",
	cadoc.ObjectTypeDummy: "DUMMY", cadoc.ObjectTypeLongTransaction: "LONG_TRANSACTION",
	cadoc.ObjectTypeLwPolyline: "LWPOLYLINE", cadoc.ObjectTypeHatch: "HATCH", cadoc.ObjectTypeXRecord: "XRECORD",
	cadoc.ObjectTypeAcDbPlaceholder: "ACDBPLACEHOLDER", cadoc.ObjectTypeVbaProject: "VBA_PROJECT",
	cadoc.ObjectTypeLayout: "LAYOUT", cadoc.ObjectTypeAcadProxyEntity: "ACAD_PROXY_ENTITY",
	cadoc.ObjectTypeAcadProxyObject: "ACAD_PROXY_OBJECT", cadoc.ObjectTypeInvalid: "INVALID",
	cadoc.ObjectTypeUndefined: "UNDEFINED",
}
