package dwg

import (
	"bytes"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// ----- table control objects -----

// readDocumentTable reads a table control object: the entry handles.
func readDocumentTable[T cadoc.TableEntry](or *objectReader, table *cadoc.Table[T]) *tmpl {
	t := or.newObjectTmpl(table)
	t.kind = kindTable
	or.readCommonNonEntityData(t)
	n := int(or.obj.ReadBitLong())
	for i := 0; i < n; i++ {
		t.entryHandles = append(t.entryHandles, or.handleRef())
	}
	t.build = func(b *builder) { addTableEntries(b, table, t.entryHandles) }
	return t
}

func addTableEntries[T cadoc.TableEntry](b *builder, table *cadoc.Table[T], handles []uint64) {
	seen := map[uint64]bool{}
	for _, h := range handles {
		if seen[h] {
			continue
		}
		seen[h] = true
		e, ok := b.object(h).(T)
		if !ok {
			continue
		}
		if e.EntryName() == "" {
			e.SetEntryName(unnamedEntryName(table, e))
		}
		if err := table.Add(e); err != nil {
			b.notify(NotifyError, "[%s] the entry %s already exists", table.SubclassMarker(), e.EntryName())
		}
	}
}

// unnamedEntryName names an entry stored without a name: shape file styles
// after their file, as the DXF reader does, others "unnamedN" like
// ACadSharp's Table.Add.
func unnamedEntryName[T cadoc.TableEntry](table *cadoc.Table[T], e T) string {
	base, sep, i := "unnamed", "", 0
	if ts, ok := any(e).(*cadoc.TextStyle); ok && ts.IsShapeFile() {
		base, sep, i = strings.TrimSuffix(ts.Filename, ".shx"), "_", 1
		if base == "" {
			base = "SHAPE"
		}
		if !table.Contains(base) {
			return base
		}
	}
	for ; ; i++ {
		if n := base + sep + strconv.Itoa(i); !table.Contains(n) {
			return n
		}
	}
}

func (or *objectReader) readBlockControlObject() *tmpl {
	table := cadoc.NewBlockRecordsTable()
	t := readDocumentTable(or, table)
	model := or.handleRef()
	paper := or.handleRef()
	t.build = func(b *builder) {
		addTableEntries(b, table, t.entryHandles)
		for _, h := range []uint64{model, paper} {
			if r, ok := b.object(h).(*cadoc.BlockRecord); ok {
				if err := table.Add(r); err != nil {
					b.notify(NotifyError, "[%s] the entry %s already exists", table.SubclassMarker(), r.Name)
				}
			}
		}
	}
	or.b.blockRecords = table
	return t
}

func (or *objectReader) readLTypeControlObject() *tmpl {
	table := cadoc.NewLineTypesTable()
	t := readDocumentTable(or, table)
	// The entries end with BYLAYER and BYBLOCK, outside the counted ones.
	t.entryHandles = append(t.entryHandles, or.handleRef(), or.handleRef())
	or.b.lineTypes = table
	return t
}

// ----- table entries -----

func (or *objectReader) newEntryTmpl(e cadoc.Object) *tmpl {
	t := or.newObjectTmpl(e)
	t.kind = kindTableEntry
	return t
}

// readEntryName reads the entry name, kept only when it is not blank.
func (or *objectReader) readEntryName(e cadoc.TableEntry) {
	if name := or.text.ReadVariableText(); strings.TrimSpace(name) != "" {
		e.SetEntryName(name)
	}
}

func (or *objectReader) readAppId() *tmpl {
	app := &cadoc.AppId{}
	t := or.newObjectTmpl(app)
	or.readCommonNonEntityData(t)
	or.readEntryName(app)
	or.readXrefDependantBit(app)
	or.obj.ReadRC() // unknown 71
	or.handleRef()  // external reference block
	return t
}

func (or *objectReader) readBlockHeader() *tmpl {
	rec := cadoc.NewBlockRecord("")
	rec.CanScale, rec.IsExplodable = true, true
	blk := rec.BlockEntity
	t := or.newEntryTmpl(rec)
	t.rec = &blockRecordTmpl{}
	or.b.blockRecordTemplates = append(or.b.blockRecordTemplates, t)
	or.readCommonNonEntityData(t)

	// Anonymous blocks only store "*" and the type letter here; the BLOCK
	// entity has the full name.
	name := or.text.ReadVariableText()
	t.name = name
	if strings.EqualFold(name, cadoc.ModelSpaceName) || strings.EqualFold(name, cadoc.PaperSpaceName) {
		rec.Name = name
	}
	or.readXrefDependantBit(rec)
	if or.obj.ReadBit() {
		blk.Flags |= cadoc.BlockTypeFlagsAnonymous
	}
	// ACadSharp drops the attributes bit; DXF has it as block flag 2.
	if or.obj.ReadBit() {
		blk.Flags |= cadoc.BlockTypeFlagsNonConstantAttributeDefinitions
	}
	if or.obj.ReadBit() {
		blk.Flags |= cadoc.BlockTypeFlagsXRef
	}
	if or.obj.ReadBit() {
		blk.Flags |= cadoc.BlockTypeFlagsXRefOverlay
	}
	if or.R2000Plus {
		blk.IsUnloaded = or.obj.ReadBit()
	}
	isXRef := blk.Flags&(cadoc.BlockTypeFlagsXRef|cadoc.BlockTypeFlagsXRefOverlay) != 0
	nOwned := 0
	if or.R2004Plus && blk.Flags&cadoc.BlockTypeFlagsXRef == 0 && blk.Flags&cadoc.BlockTypeFlagsXRefOverlay == 0 {
		nOwned = int(or.obj.ReadBitLong())
	}
	blk.BasePoint = or.obj.Read3BitDouble()
	blk.XRefPath = or.text.ReadVariableText()
	insertCount := 0
	if or.R2000Plus {
		for i := or.obj.ReadRC(); i != 0; i = or.obj.ReadRC() {
			insertCount++
		}
		blk.Comments = or.text.ReadVariableText()
		n := int(or.obj.ReadBitLong())
		if n < 0 {
			panic(errEndOfStream)
		}
		rec.Preview = or.obj.ReadBytes(n)
	}
	if or.R2007Plus {
		rec.Units = cadoc.UnitsType(or.obj.ReadBitShort())
		rec.IsExplodable = or.obj.ReadBit()
		rec.CanScale = or.obj.ReadRC() > 0
	}
	or.handleRef() // NULL
	t.rec.beginBlock = or.handleRef()
	if or.ver >= cadoc.AC1012 && or.ver <= cadoc.AC1015 && !isXRef {
		t.rec.firstEntity = or.handleRef()
		t.rec.lastEntity = or.handleRef()
		t.rec.hasFirst = true
	}
	if or.R2004Plus {
		for i := 0; i < nOwned; i++ {
			t.rec.ownedObjects = append(t.rec.ownedObjects, or.handleRef())
		}
	}
	t.rec.endBlock = or.handleRef()
	if or.R2000Plus {
		for i := 0; i < insertCount; i++ {
			t.rec.insertHandles = append(t.rec.insertHandles, or.handleRef())
		}
		t.rec.layout = or.handleRef()
	}
	t.build = func(b *builder) { b.buildBlockRecord(t, rec) }
	return t
}

// buildBlockRecord adds the entities of a block (CadBlockRecordTemplate).
func (b *builder) buildBlockRecord(t *tmpl, rec *cadoc.BlockRecord) {
	if l, ok := b.object(t.rec.layout).(*cadoc.Layout); ok {
		rec.Layout = l
	}
	var entities []cadoc.Entity
	if t.rec.hasFirst && t.rec.firstEntity != 0 {
		entities = b.entitiesCollection(t.rec.firstEntity, t.rec.lastEntity)
	} else {
		seen := map[uint64]bool{}
		for _, h := range t.rec.ownedObjects {
			if seen[h] {
				continue
			}
			seen[h] = true
			if e := b.entity(h); e != nil {
				entities = append(entities, e)
			}
		}
	}
	for _, e := range entities {
		if _, unknown := e.(*cadoc.UnknownEntity); unknown && !b.r.cfg.KeepUnknownEntities {
			continue
		}
		if e.Base().Owner != nil {
			b.notify(NotifyError, "Item %s already has an owner", e.ObjectName())
			continue
		}
		rec.AddEntity(e)
	}
}

func (or *objectReader) readLayer() *tmpl {
	l := cadoc.NewLayer("")
	t := or.newEntryTmpl(l)
	or.readCommonNonEntityData(t)
	or.readEntryName(l)
	or.readXrefDependantBit(l)
	if or.R13_14Only {
		if or.obj.ReadBit() {
			l.Flags |= cadoc.LayerFlagsFrozen
		}
		l.IsOn = !or.obj.ReadBit()
		if or.obj.ReadBit() {
			l.Flags |= cadoc.LayerFlagsFrozenNewViewports
		}
		if or.obj.ReadBit() {
			l.Flags |= cadoc.LayerFlagsLocked
		}
	}
	if or.R2000Plus {
		v := uint16(or.obj.ReadBitShort())
		if v&0b1 > 0 {
			l.Flags |= cadoc.LayerFlagsFrozen
		}
		l.IsOn = v&0b10 == 0
		if v&0b100 > 0 {
			l.Flags |= cadoc.LayerFlagsFrozenNewViewports
		}
		if v&0b1000 > 0 {
			l.Flags |= cadoc.LayerFlagsLocked
		}
		l.PlotFlag = v&0b10000 > 0
		l.LineWeight = cadoc.LineWeightFromIndex(byte((v & 0x3E0) >> 5))
	}
	c := or.merged.ReadCmColor(false)
	if c.IsByBlock() || c.IsByLayer() {
		c = cadoc.ColorDefault
	}
	l.Color = c
	or.handleRef() // layer control
	var plotStyle, material uint64
	if or.R2000Plus {
		plotStyle = or.handleRef()
	}
	if or.R2007Plus {
		material = or.handleRef()
	}
	ltype := or.handleRef()
	if or.R2013Plus {
		or.handleRef()
	}
	t.build = func(b *builder) {
		l.PlotStyleName = plotStyle
		// ACadSharp leaves the material unassigned (TODO in CadLayerTemplate).
		if m, ok := b.object(material).(*cadoc.Material); ok {
			l.Material = m
		}
		if lt, ok := tableRef(b, t, b.lineTypes, ltype, ""); ok {
			l.LineType = lt
		} else {
			b.notify(NotifyWarning, "Linetype with handle %d could not be found for layer %s", ltype, l.Name)
		}
	}
	return t
}

func (or *objectReader) readTextStyle() *tmpl {
	s := cadoc.NewTextStyle("")
	t := or.newEntryTmpl(s)
	or.readCommonNonEntityData(t)
	or.readEntryName(s)
	or.readXrefDependantBit(s)
	if or.obj.ReadBit() {
		s.Flags |= cadoc.StyleFlagsIsShape
	}
	if or.obj.ReadBit() {
		s.Flags |= cadoc.StyleFlagsVerticalText
	}
	s.Height = or.obj.ReadBitDouble()
	s.Width = or.obj.ReadBitDouble()
	s.ObliqueAngle = or.obj.ReadBitDouble()
	s.MirrorFlag = cadoc.TextMirrorFlag(or.obj.ReadRC())
	s.LastHeight = or.obj.ReadBitDouble()
	s.Filename = or.text.ReadVariableText()
	s.BigFontFilename = or.text.ReadVariableText()
	or.handleRef() // style control
	return t
}

type ltypeSegmentTmpl struct {
	style uint64
}

func (or *objectReader) readLType() *tmpl {
	lt := cadoc.NewLineType("")
	t := or.newEntryTmpl(lt)
	or.readCommonNonEntityData(t)
	or.readEntryName(lt)
	or.readXrefDependantBit(lt)
	lt.Description = or.text.ReadVariableText()
	or.obj.ReadBitDouble() // pattern length
	lt.Alignment = rune(or.obj.ReadRawChar())
	n := int(or.obj.ReadRC())
	isText := false
	segments := make([]cadoc.LineTypeSegment, n)
	for i := range segments {
		seg := &segments[i]
		seg.Length = or.obj.ReadBitDouble()
		seg.ShapeNumber = or.obj.ReadBitShort()
		x := or.obj.ReadDouble()
		seg.Offset = cadoc.Vec2{X: x, Y: or.obj.ReadDouble()}
		seg.Scale = or.obj.ReadBitDouble()
		seg.Rotation = or.obj.ReadBitDouble()
		seg.Flags = cadoc.LineTypeShapeFlags(or.obj.ReadBitShort())
		if seg.Flags&cadoc.LineTypeShapeFlagsText != 0 {
			isText = true
		}
	}
	var area []byte
	if or.ver <= cadoc.AC1018 {
		area = or.obj.ReadBytes(256)
	}
	if or.R2007Plus && isText {
		area = or.obj.ReadBytes(512)
	}
	if isText {
		or.readLineTypeSegmentTexts(segments, area)
	}
	or.handleRef() // ltype control
	styles := make([]uint64, n)
	for i := range styles {
		styles[i] = or.handleRef()
	}
	t.build = func(b *builder) {
		for i, seg := range segments {
			if s, ok := b.object(styles[i]).(*cadoc.TextStyle); ok {
				seg.Style = s
			}
			lt.AddSegment(seg)
		}
	}
	return t
}

// readLineTypeSegmentTexts takes the texts of text segments from the string
// area; the shape number is the offset into it.
func (or *objectReader) readLineTypeSegmentTexts(segments []cadoc.LineTypeSegment, area []byte) {
	if len(area) == 0 {
		return
	}
	for i := range segments {
		seg := &segments[i]
		if seg.Flags&cadoc.LineTypeShapeFlagsText == 0 {
			continue
		}
		offset := int(uint16(seg.ShapeNumber))
		if offset >= len(area) {
			or.notify(NotifyWarning, "Unable to read linetype text segment; offset %d is outside the available buffer (%d bytes).", offset, len(area))
			seg.Text = ""
			seg.ShapeNumber = 0
			continue
		}
		seg.Text = or.lineTypeText(area, offset)
		seg.ShapeNumber = 0
	}
}

func (or *objectReader) lineTypeText(buf []byte, offset int) string {
	// The single byte code page path: UTF-16 text is recognised by its
	// zero high bytes (looksLikeUtf16Le).
	if looksLikeUTF16LE(buf, offset) {
		end := offset
		for end+1 < len(buf) {
			if buf[end] == 0 && buf[end+1] == 0 {
				break
			}
			end += 2
		}
		u := make([]uint16, (end-offset)/2)
		for i := range u {
			u[i] = uint16(buf[offset+2*i]) | uint16(buf[offset+2*i+1])<<8
		}
		return string(utf16.Decode(u))
	}
	end := bytes.IndexByte(buf[offset:], 0)
	if end < 0 {
		end = len(buf) - offset
	}
	if end == 0 {
		return ""
	}
	return or.r.text.decode(buf[offset : offset+end])
}

func looksLikeUTF16LE(buf []byte, offset int) bool {
	if offset < 0 || offset+1 >= len(buf) {
		return false
	}
	sawNonZero := false
	for i := offset; i+1 < len(buf); i += 2 {
		lo, hi := buf[i], buf[i+1]
		if lo == 0 && hi == 0 {
			return sawNonZero
		}
		if hi != 0 {
			return false
		}
		if lo != 0 {
			sawNonZero = true
		}
	}
	return false
}

func (or *objectReader) readView() *tmpl {
	v := cadoc.NewView("")
	t := or.newEntryTmpl(v)
	or.readCommonNonEntityData(t)
	v.Name = or.text.ReadVariableText()
	or.readXrefDependantBit(v)
	v.Height = or.obj.ReadBitDouble()
	v.Width = or.obj.ReadBitDouble()
	v.Center = or.obj.Read2RawDouble()
	v.Target = or.obj.Read3BitDouble()
	v.Direction = or.obj.Read3BitDouble()
	v.Angle = or.obj.ReadBitDouble()
	v.LensLength = or.obj.ReadBitDouble()
	v.FrontClipping = or.obj.ReadBitDouble()
	v.BackClipping = or.obj.ReadBitDouble()
	or.readViewMode(&v.ViewMode)
	if or.R2000Plus {
		v.RenderMode = cadoc.RenderMode(or.obj.ReadRC())
	}
	if or.R2007Plus {
		or.merged.ReadBit()
		or.merged.ReadRC()
		or.merged.ReadBitDouble()
		or.merged.ReadBitDouble()
		or.merged.ReadCmColor(false)
	}
	if or.obj.ReadBit() {
		v.Flags |= 0b1
	}
	if or.R2000Plus {
		v.IsUcsAssociated = or.obj.ReadBit()
		if v.IsUcsAssociated {
			v.UcsOrigin = or.obj.Read3BitDouble()
			v.UcsXAxis = or.obj.Read3BitDouble()
			v.UcsYAxis = or.obj.Read3BitDouble()
			v.UcsElevation = or.obj.ReadBitDouble()
			v.UcsOrthographicType = cadoc.OrthographicType(or.obj.ReadBitShort())
		}
	}
	or.handleRef() // view control
	var visualStyle uint64
	if or.R2007Plus {
		v.IsPlottable = or.obj.ReadBit()
		or.handleRef() // background
		visualStyle = or.handleRef()
		or.handleRef() // sun
	}
	var baseUcs, namedUcs uint64
	if or.R2000Plus && v.IsUcsAssociated {
		baseUcs = or.handleRef()
		namedUcs = or.handleRef()
	}
	if or.R2007Plus {
		or.handleRef() // live section
	}
	t.build = func(b *builder) {
		// ACadSharp leaves these unresolved (TODO in CadViewTemplate).
		if u, ok := b.object(baseUcs).(*cadoc.UCS); ok {
			v.BaseUcs = u
		}
		if u, ok := b.object(namedUcs).(*cadoc.UCS); ok {
			v.NamedUcs = u
		}
		if vs := b.object(visualStyle); vs != nil {
			v.VisualStyle = vs
		}
	}
	return t
}

// readViewMode reads the 4 view mode bits of VIEW and VPORT.
func (or *objectReader) readViewMode(m *cadoc.ViewModeType) {
	if or.obj.ReadBit() {
		*m |= cadoc.ViewModeTypePerspectiveView
	}
	if or.obj.ReadBit() {
		*m |= cadoc.ViewModeTypeFrontClipping
	}
	if or.obj.ReadBit() {
		*m |= cadoc.ViewModeTypeBackClipping
	}
	// The fourth bit is the opposite of bit 16 (ACadSharp sets 16 on 1,
	// the DXF files of the samples confirm the documentation).
	if !or.obj.ReadBit() {
		*m |= cadoc.ViewModeTypeFrontClippingZ
	}
}

func (or *objectReader) readUcs() *tmpl {
	u := cadoc.NewUCS("")
	t := or.newObjectTmpl(u)
	or.readCommonNonEntityData(t)
	u.Name = or.text.ReadVariableText()
	or.readXrefDependantBit(u)
	u.Origin = or.obj.Read3BitDouble()
	u.XAxis = or.obj.Read3BitDouble()
	u.YAxis = or.obj.Read3BitDouble()
	if or.R2000Plus {
		u.Elevation = or.obj.ReadBitDouble()
		u.OrthographicViewType = cadoc.OrthographicType(or.obj.ReadBitShort())
		u.OrthographicType = cadoc.OrthographicType(or.obj.ReadBitShort())
	}
	or.handleRef() // ucs control
	if or.R2000Plus {
		or.handleRef() // base ucs
		or.handleRef() // named ucs
	}
	return t
}

func (or *objectReader) readVPort() *tmpl {
	v := cadoc.NewVPort("")
	v.ViewMode = 0
	t := or.newEntryTmpl(v)
	or.readCommonNonEntityData(t)
	v.Name = or.text.ReadVariableText()
	or.readXrefDependantBit(v)
	v.ViewHeight = or.obj.ReadBitDouble()
	// Stored as aspect ratio × view height.
	v.AspectRatio = or.obj.ReadBitDouble() / v.ViewHeight
	v.Center = or.obj.Read2RawDouble()
	v.Target = or.obj.Read3BitDouble()
	v.Direction = or.obj.Read3BitDouble()
	v.TwistAngle = or.obj.ReadBitDouble()
	v.LensLength = or.obj.ReadBitDouble()
	v.FrontClippingPlane = or.obj.ReadBitDouble()
	v.BackClippingPlane = or.obj.ReadBitDouble()
	or.readViewMode(&v.ViewMode)
	if or.R2000Plus {
		v.RenderMode = cadoc.RenderMode(or.obj.ReadRC())
	}
	if or.R2007Plus {
		v.UseDefaultLighting = or.obj.ReadBit()
		v.DefaultLighting = cadoc.DefaultLightingType(or.obj.ReadRC())
		v.Brightness = or.obj.ReadBitDouble()
		v.Contrast = or.obj.ReadBitDouble()
		v.AmbientColor = or.merged.ReadCmColor(false)
	}
	v.BottomLeft = or.obj.Read2RawDouble()
	v.TopRight = or.obj.Read2RawDouble()
	if or.obj.ReadBit() {
		v.ViewMode |= cadoc.ViewModeTypeFollow
	}
	v.CircleZoomPercent = or.obj.ReadBitShort()
	or.obj.ReadBit() // fast zoom
	if or.obj.ReadBit() {
		v.UcsIconDisplay = cadoc.UscIconTypeOnLower
	}
	if or.obj.ReadBit() {
		v.UcsIconDisplay = cadoc.UscIconTypeOnOrigin
	}
	v.ShowGrid = or.obj.ReadBit()
	v.GridSpacing = or.obj.Read2RawDouble()
	v.SnapOn = or.obj.ReadBit()
	v.IsometricSnap = or.obj.ReadBit()
	v.SnapIsoPair = or.obj.ReadBitShort()
	v.SnapRotation = or.obj.ReadBitDouble()
	v.SnapBasePoint = or.obj.Read2RawDouble()
	v.SnapSpacing = or.obj.Read2RawDouble()
	if or.R2000Plus {
		or.obj.ReadBit()
		or.obj.ReadBit() // UCS per viewport
		v.Origin = or.obj.Read3BitDouble()
		v.XAxis = or.obj.Read3BitDouble()
		v.YAxis = or.obj.Read3BitDouble()
		v.Elevation = or.obj.ReadBitDouble()
		v.OrthographicType = cadoc.OrthographicType(or.obj.ReadBitShort())
	}
	if or.R2007Plus {
		v.GridFlags = cadoc.GridFlags(or.obj.ReadBitShort())
		v.MinorGridLinesPerMajorGridLine = or.obj.ReadBitShort()
	}
	or.handleRef() // vport control
	var visualStyle uint64
	if or.R2007Plus {
		or.handleRef() // background
		visualStyle = or.handleRef()
		or.handleRef() // sun
	}
	var namedUcs, baseUcs uint64
	if or.R2000Plus {
		namedUcs = or.handleRef()
		baseUcs = or.handleRef()
	}
	t.build = func(b *builder) {
		if u, ok := b.object(baseUcs).(*cadoc.UCS); ok {
			v.BaseUcs = u
		} else if baseUcs > 0 {
			b.notify(NotifyWarning, "Boundary %d not found for viewport %d", baseUcs, v.Handle)
		}
		// ACadSharp assigns the named UCS to BaseUcs as well.
		if u, ok := b.object(namedUcs).(*cadoc.UCS); ok {
			v.NamedUcs = u
		} else if namedUcs > 0 {
			b.notify(NotifyWarning, "Boundary %d not found for viewport %d", baseUcs, v.Handle)
		}
		if vs := b.object(visualStyle); vs != nil {
			v.VisualStyle = vs
		}
	}
	return t
}

func (or *objectReader) readDimStyle() *tmpl {
	d := cadoc.NewDimensionStyle("")
	t := or.newEntryTmpl(d)
	or.readCommonNonEntityData(t)
	or.readEntryName(d)
	or.readXrefDependantBit(d)
	var blkName, blk1Name, blk2Name string
	if or.R13_14Only {
		// The R13-R14 order as ACadSharp reads it (DIMTIH/DIMTOH/DIMSE1/DIMSE2
		// shifted by one).
		d.GenerateTolerances = or.obj.ReadBit()
		d.LimitsGeneration = or.obj.ReadBit()
		d.TextOutsideHorizontal = or.obj.ReadBit()
		d.SuppressFirstExtensionLine = or.obj.ReadBit()
		d.SuppressSecondExtensionLine = or.obj.ReadBit()
		d.TextInsideHorizontal = or.obj.ReadBit()
		d.AlternateUnitDimensioning = or.obj.ReadBit()
		d.TextOutsideExtensions = or.obj.ReadBit()
		d.SeparateArrowBlocks = or.obj.ReadBit()
		d.TextInsideExtensions = or.obj.ReadBit()
		d.SuppressOutsideExtensions = or.obj.ReadBit()
		d.AlternateUnitDecimalPlaces = int16(or.obj.ReadRC())
		d.ZeroHandling = cadoc.ZeroHandling(or.obj.ReadRawChar())
		d.SuppressFirstDimensionLine = or.obj.ReadBit()
		d.SuppressSecondDimensionLine = or.obj.ReadBit()
		d.ToleranceAlignment = cadoc.ToleranceAlignment(or.obj.ReadRawChar())
		d.TextHorizontalAlignment = cadoc.DimensionTextHorizontalAlignment(or.obj.ReadRC())
		d.DimensionFit = int16(or.obj.ReadRawChar())
		d.CursorUpdate = or.obj.ReadBit()
		d.ToleranceZeroHandling = cadoc.ZeroHandling(or.obj.ReadRC())
		d.AlternateUnitZeroHandling = cadoc.ZeroHandling(or.obj.ReadRC())
		d.AlternateUnitToleranceZeroHandling = cadoc.ZeroHandling(or.obj.ReadRC())
		d.TextVerticalAlignment = cadoc.DimensionTextVerticalAlignment(or.obj.ReadRC())
		d.DimensionUnit = or.obj.ReadBitShort()
		d.AngularUnit = cadoc.AngularUnitFormat(or.obj.ReadBitShort())
		d.DecimalPlaces = or.obj.ReadBitShort()
		d.ToleranceDecimalPlaces = or.obj.ReadBitShort()
		d.AlternateUnitFormat = cadoc.LinearUnitFormat(or.obj.ReadBitShort())
		d.AlternateUnitToleranceDecimalPlaces = or.obj.ReadBitShort()
		d.ScaleFactor = or.obj.ReadBitDouble()
		d.ArrowSize = or.obj.ReadBitDouble()
		d.ExtensionLineOffset = or.obj.ReadBitDouble()
		d.DimensionLineIncrement = or.obj.ReadBitDouble()
		d.ExtensionLineExtension = or.obj.ReadBitDouble()
		d.Rounding = or.obj.ReadBitDouble()
		d.DimensionLineExtension = or.obj.ReadBitDouble()
		d.PlusTolerance = or.obj.ReadBitDouble()
		d.MinusTolerance = or.obj.ReadBitDouble()
		d.TextHeight = or.obj.ReadBitDouble()
		d.CenterMarkSize = or.obj.ReadBitDouble()
		d.TickSize = or.obj.ReadBitDouble()
		d.AlternateUnitScaleFactor = or.obj.ReadBitDouble()
		d.LinearScaleFactor = or.obj.ReadBitDouble()
		d.TextVerticalPosition = or.obj.ReadBitDouble()
		d.ToleranceScaleFactor = or.obj.ReadBitDouble()
		d.DimensionLineGap = or.obj.ReadBitDouble()
		d.PostFix = or.text.ReadVariableText()
		d.AlternateDimensioningSuffix = or.text.ReadVariableText()
		blkName = or.text.ReadVariableText()
		blk1Name = or.text.ReadVariableText()
		blk2Name = or.text.ReadVariableText()
		d.DimensionLineColor = or.obj.ReadColorByIndex()
		d.ExtensionLineColor = or.obj.ReadColorByIndex()
		d.TextColor = or.obj.ReadColorByIndex()
	}
	if or.R2000Plus {
		d.PostFix = or.text.ReadVariableText()
		d.AlternateDimensioningSuffix = or.text.ReadVariableText()
		d.ScaleFactor = or.obj.ReadBitDouble()
		d.ArrowSize = or.obj.ReadBitDouble()
		d.ExtensionLineOffset = or.obj.ReadBitDouble()
		d.DimensionLineIncrement = or.obj.ReadBitDouble()
		d.ExtensionLineExtension = or.obj.ReadBitDouble()
		d.Rounding = or.obj.ReadBitDouble()
		d.DimensionLineExtension = or.obj.ReadBitDouble()
		d.PlusTolerance = or.obj.ReadBitDouble()
		d.MinusTolerance = or.obj.ReadBitDouble()
	}
	if or.R2007Plus {
		d.FixedExtensionLineLength = or.obj.ReadBitDouble()
		d.JoggedRadiusDimensionTransverseSegmentAngle = or.obj.ReadBitDouble()
		d.TextBackgroundFillMode = cadoc.DimensionTextBackgroundFillMode(or.obj.ReadBitShort())
		d.TextBackgroundColor = or.merged.ReadCmColor(false)
	}
	if or.R2000Plus {
		d.GenerateTolerances = or.obj.ReadBit()
		d.LimitsGeneration = or.obj.ReadBit()
		d.TextInsideHorizontal = or.obj.ReadBit()
		d.TextOutsideHorizontal = or.obj.ReadBit()
		d.SuppressFirstExtensionLine = or.obj.ReadBit()
		d.SuppressSecondExtensionLine = or.obj.ReadBit()
		d.TextVerticalAlignment = cadoc.DimensionTextVerticalAlignment(or.obj.ReadBitShort())
		d.ZeroHandling = cadoc.ZeroHandling(or.obj.ReadBitShort())
		d.AngularZeroHandling = cadoc.AngularZeroHandling(or.obj.ReadBitShort())
	}
	if or.R2007Plus {
		d.ArcLengthSymbolPosition = cadoc.ArcLengthSymbolPosition(or.obj.ReadBitShort())
	}
	if or.R2000Plus {
		d.TextHeight = or.obj.ReadBitDouble()
		d.CenterMarkSize = or.obj.ReadBitDouble()
		d.TickSize = or.obj.ReadBitDouble()
		d.AlternateUnitScaleFactor = or.obj.ReadBitDouble()
		d.LinearScaleFactor = or.obj.ReadBitDouble()
		d.TextVerticalPosition = or.obj.ReadBitDouble()
		d.ToleranceScaleFactor = or.obj.ReadBitDouble()
		d.DimensionLineGap = or.obj.ReadBitDouble()
		d.AlternateUnitRounding = or.obj.ReadBitDouble()
		d.AlternateUnitDimensioning = or.obj.ReadBit()
		d.AlternateUnitDecimalPlaces = or.obj.ReadBitShort()
		d.TextOutsideExtensions = or.obj.ReadBit()
		d.SeparateArrowBlocks = or.obj.ReadBit()
		d.TextInsideExtensions = or.obj.ReadBit()
		d.SuppressOutsideExtensions = or.obj.ReadBit()
		d.DimensionLineColor = or.merged.ReadCmColor(false)
		d.ExtensionLineColor = or.merged.ReadCmColor(false)
		d.TextColor = or.merged.ReadCmColor(false)
		d.AngularDecimalPlaces = or.obj.ReadBitShort()
		d.DecimalPlaces = or.obj.ReadBitShort()
		d.ToleranceDecimalPlaces = or.obj.ReadBitShort()
		d.AlternateUnitFormat = cadoc.LinearUnitFormat(or.obj.ReadBitShort())
		d.AlternateUnitToleranceDecimalPlaces = or.obj.ReadBitShort()
		d.AngularUnit = cadoc.AngularUnitFormat(or.obj.ReadBitShort())
		d.FractionFormat = cadoc.FractionFormat(or.obj.ReadBitShort())
		d.LinearUnitFormat = cadoc.LinearUnitFormat(or.obj.ReadBitShort())
		d.DecimalSeparator = rune(uint16(or.obj.ReadBitShort()))
		d.TextMovement = cadoc.TextMovement(or.obj.ReadBitShort())
		d.TextHorizontalAlignment = cadoc.DimensionTextHorizontalAlignment(or.obj.ReadBitShort())
		d.SuppressFirstDimensionLine = or.obj.ReadBit()
		d.SuppressSecondDimensionLine = or.obj.ReadBit()
		d.ToleranceAlignment = cadoc.ToleranceAlignment(or.obj.ReadBitShort())
		d.ToleranceZeroHandling = cadoc.ZeroHandling(or.obj.ReadBitShort())
		d.AlternateUnitZeroHandling = cadoc.ZeroHandling(or.obj.ReadBitShort())
		d.AlternateUnitToleranceZeroHandling = cadoc.ZeroHandling(or.obj.ReadBitShort())
		d.CursorUpdate = or.obj.ReadBit()
		d.DimensionFit = or.obj.ReadBitShort()
	}
	if or.R2007Plus {
		d.IsExtensionLineLengthFixed = or.obj.ReadBit()
	}
	if or.R2010Plus {
		if or.obj.ReadBit() {
			d.TextDirection = cadoc.TextDirectionRightToLeft
		} else {
			d.TextDirection = cadoc.TextDirectionLeftToRight
		}
		d.AltMzf = or.obj.ReadBitDouble()
		d.AltMzs = or.text.ReadVariableText()
		d.Mzf = or.obj.ReadBitDouble()
		d.Mzs = or.text.ReadVariableText()
	}
	if or.R2000Plus {
		d.DimensionLineWeight = cadoc.LineWeight(or.obj.ReadBitShort())
		d.ExtensionLineWeight = cadoc.LineWeight(or.obj.ReadBitShort())
	}
	or.obj.ReadBit() // unknown 70 bit
	or.handleRef()   // external reference block
	textStyle := or.handleRef()
	var ldrblk, dimblk, dimblk1, dimblk2, ltype, ltex1, ltex2 uint64
	if or.R2000Plus {
		ldrblk = or.handleRef()
		dimblk = or.handleRef()
		dimblk1 = or.handleRef()
		dimblk2 = or.handleRef()
	}
	if or.R2007Plus {
		ltype = or.handleRef()
		ltex1 = or.handleRef()
		ltex2 = or.handleRef()
	}
	t.build = func(b *builder) {
		if s, ok := tableRef(b, t, b.textStyles, textStyle, ""); ok {
			d.Style = s
		}
		if lt, ok := tableRef(b, t, b.lineTypes, ltype, ""); ok {
			d.LineType = lt
		}
		if lt, ok := tableRef(b, t, b.lineTypes, ltex1, ""); ok {
			d.LineTypeExt1 = lt
		}
		if lt, ok := tableRef(b, t, b.lineTypes, ltex2, ""); ok {
			d.LineTypeExt2 = lt
		}
		if r, ok := tableRef(b, t, b.blockRecords, ldrblk, ""); ok {
			d.LeaderArrow = r
		}
		if r, ok := tableRef(b, t, b.blockRecords, dimblk, blkName); ok {
			d.ArrowBlock = r
		}
		if r, ok := tableRef(b, t, b.blockRecords, dimblk1, blk1Name); ok {
			d.DimArrow1 = r
		}
		if r, ok := tableRef(b, t, b.blockRecords, dimblk2, blk2Name); ok {
			d.DimArrow2 = r
		}
	}
	return t
}

// readViewportEntityControl and readViewportEntityHeader read the R13-R15
// viewport entity headers, which the model does not keep; their handles
// are still followed.
func (or *objectReader) readViewportEntityControl() *tmpl {
	t := or.newObjectTmpl(&cadoc.UnknownObject{DxfName: "VP_ENT_HDR_CTRL"})
	or.readCommonNonEntityData(t)
	n := int(or.obj.ReadBitLong())
	for i := 0; i < n; i++ {
		or.handleRef()
	}
	return nil
}

func (or *objectReader) readViewportEntityHeader() *tmpl {
	t := or.newObjectTmpl(&cadoc.UnknownObject{DxfName: "VP_ENT_HDR"})
	or.readCommonNonEntityData(t)
	or.text.ReadVariableText()
	or.readXrefDependantBit(&cadoc.AppId{})
	or.obj.ReadBit()
	or.handleRef()
	or.handleRef()
	or.handleRef()
	return nil
}
