package dwg

import (
	"encoding/binary"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// ----- dictionaries -----

type dictEntryTmpl struct {
	name   string
	handle uint64
}

// readCommonDictionary reads the entries of DICTIONARY and
// ACDBDICTIONARYWDFLT.
func (or *objectReader) readCommonDictionary(t *tmpl, d *cadoc.Dictionary) []dictEntryTmpl {
	t.kind = kindDictionary
	or.readCommonNonEntityData(t)
	n := int(or.obj.ReadBitLong())
	if or.ver == cadoc.AC1014 {
		or.obj.ReadRC()
	}
	if or.R2000Plus {
		d.ClonningFlags = cadoc.DictionaryCloningFlags(or.obj.ReadBitShort())
		d.HardOwnerFlag = or.obj.ReadRC() > 0
	}
	var entries []dictEntryTmpl
	keys := map[string]bool{}
	for i := 0; i < n; i++ {
		name := or.text.ReadVariableText()
		h := or.handleRef()
		if h == 0 || name == "" {
			continue
		}
		if keys[name] {
			panic(errorf("An item with the same key has already been added. Key: %s", name))
		}
		keys[name] = true
		entries = append(entries, dictEntryTmpl{name: name, handle: h})
	}
	return entries
}

// buildDictionary adds the entries (CadDictionaryTemplate).
func (b *builder) buildDictionary(t *tmpl, d *cadoc.Dictionary, self cadoc.Object, entries []dictEntryTmpl) {
	if t.hasOwner && t.owner == 0 && b.doc.RootDictionary == nil {
		if dd, ok := self.(*cadoc.Dictionary); ok {
			b.doc.RootDictionary = dd
			if dd.Name == "" {
				dd.Name = cadoc.RootDictionaryName
			}
		}
	}
	for _, e := range entries {
		entry, ok := b.object(e.handle).(cadoc.NonGraphicalObject)
		if !ok {
			b.notify(NotifyWarning, "Entry not found %s|%d for dictionary %s|%d", e.name, e.handle, d.Name, d.Handle)
			continue
		}
		// The entry keeps its own name when it has one (scales are keyed A0,
		// A1, … but named 1:1, 1:2, …).
		if *entry.ObjectNameKey() == "" {
			*entry.ObjectNameKey() = e.name
		}
		if err := d.Add(entry); err != nil {
			b.notify(NotifyError, "Error when trying to add the entry %s to %s|%d", *entry.ObjectNameKey(), d.Name, d.Handle)
		}
	}
}

func (or *objectReader) readDictionary() *tmpl {
	d := cadoc.NewDictionary()
	t := or.newObjectTmpl(d)
	entries := or.readCommonDictionary(t, d)
	t.build = func(b *builder) { b.buildDictionary(t, d, d, entries) }
	return t
}

func (or *objectReader) readDictionaryWithDefault() *tmpl {
	d := cadoc.NewDictionaryWithDefault("")
	t := or.newObjectTmpl(d)
	entries := or.readCommonDictionary(t, &d.Dictionary)
	def := or.handleRef()
	t.build = func(b *builder) {
		b.buildDictionary(t, &d.Dictionary, d, entries)
		if o := b.object(def); o != nil {
			d.DefaultEntry = o
		}
	}
	return t
}

func (or *objectReader) readDictionaryVar() *tmpl {
	v := &cadoc.DictionaryVariable{}
	t := or.newObjectTmpl(v)
	or.readCommonNonEntityData(t)
	or.obj.ReadRC() // integer value
	v.Value = or.text.ReadVariableText()
	return t
}

// ----- plain objects -----

func (or *objectReader) readPlaceHolder() *tmpl {
	t := or.newObjectTmpl(&cadoc.AcdbPlaceHolder{})
	or.readCommonNonEntityData(t)
	return t
}

func (or *objectReader) readGroup() *tmpl {
	g := cadoc.NewGroup("")
	t := or.newObjectTmpl(g)
	or.readCommonNonEntityData(t)
	g.Description = or.text.ReadVariableText()
	or.obj.ReadBitShort() // unnamed
	g.Selectable = or.obj.ReadBitShort() > 0
	n := int(or.obj.ReadBitLong())
	var handles []uint64
	for i := 0; i < n; i++ {
		handles = append(handles, or.handleRef())
	}
	t.build = func(b *builder) {
		seen := map[uint64]bool{}
		for _, h := range handles {
			if seen[h] {
				continue
			}
			seen[h] = true
			et := b.template(h)
			if et == nil || et.ent == nil {
				b.notify(NotifyWarning, "Entity with handle %d not found for group %d", h, g.Handle)
				continue
			}
			b.buildTemplate(et)
			e := et.obj.(cadoc.Entity)
			g.Entities = append(g.Entities, e)
			if !hasReactor(e.Base(), g) {
				e.Base().AddReactor(g)
			}
		}
	}
	return t
}

func (or *objectReader) readMLineStyle() *tmpl {
	s := cadoc.NewMLineStyle("")
	t := or.newObjectTmpl(s)
	or.readCommonNonEntityData(t)
	s.Name = or.text.ReadVariableText()
	s.Description = or.text.ReadVariableText()
	// The DWG bits of the flags differ from the DXF ones.
	flags := uint16(or.obj.ReadBitShort())
	var f cadoc.MLineStyleFlags
	for _, m := range [][2]uint16{{1, uint16(cadoc.MLineStyleFlagsDisplayJoints)}, {2, uint16(cadoc.MLineStyleFlagsFillOn)},
		{16, uint16(cadoc.MLineStyleFlagsStartSquareCap)}, {32, uint16(cadoc.MLineStyleFlagsStartRoundCap)},
		{64, uint16(cadoc.MLineStyleFlagsStartInnerArcsCap)}, {256, uint16(cadoc.MLineStyleFlagsEndSquareCap)},
		{512, uint16(cadoc.MLineStyleFlagsEndRoundCap)}, {1024, uint16(cadoc.MLineStyleFlagsEndInnerArcsCap)}} {
		if flags&m[0] != 0 {
			f |= cadoc.MLineStyleFlags(m[1])
		}
	}
	s.Flags = f
	s.FillColor = or.merged.ReadCmColor(false)
	s.StartAngle = or.obj.ReadBitDouble()
	s.EndAngle = or.obj.ReadBitDouble()
	n := int(or.obj.ReadRC())
	type elemRef struct {
		handle uint64
		index  int16
		byIdx  bool
	}
	refs := make([]elemRef, n)
	for i := 0; i < n; i++ {
		e := cadoc.MLineStyleElement{}
		e.Offset = or.obj.ReadBitDouble()
		e.Color = or.merged.ReadCmColor(false)
		if or.R2018Plus {
			refs[i].handle = or.handleRef()
		} else {
			refs[i].index = or.obj.ReadBitShort()
			refs[i].byIdx = true
		}
		s.Elements = append(s.Elements, e)
	}
	t.build = func(b *builder) {
		for i, ref := range refs {
			el := &s.Elements[i]
			if lt, ok := b.object(ref.handle).(*cadoc.LineType); ok {
				el.LineType = lt
				continue
			}
			if !ref.byIdx {
				continue
			}
			switch ref.index {
			case math.MaxInt16:
				el.LineType, _ = b.lineTypes.Get(cadoc.ByLayerName)
			case math.MaxInt16 - 1:
				el.LineType, _ = b.lineTypes.Get(cadoc.ByBlockName)
			default:
				if ref.index >= 0 && int(ref.index) < b.lineTypes.Len() {
					el.LineType = b.lineTypes.Entries()[ref.index]
				} else {
					b.notify(NotifyError, "Linetype not assigned, index %d", ref.index)
				}
			}
		}
	}
	return t
}

func (or *objectReader) readXRecord() *tmpl {
	x := &cadoc.XRecord{}
	t := or.newObjectTmpl(x)
	or.readCommonNonEntityData(t)
	s := or.obj
	n := int(s.ReadBitLong())
	end := n + s.Pos
	var handleIdx []int
	for s.Pos < end {
		code := int(s.ReadShort())
		e := cadoc.XRecordEntry{Code: code}
		switch xrecordValueType(code) {
		case xvString:
			e.Value = s.ReadTextUnicode()
		case xvPoint:
			px := s.ReadDouble()
			py := s.ReadDouble()
			e.Value = cadoc.Vec3{X: px, Y: py, Z: s.ReadDouble()}
		case xvDouble:
			e.Value = s.ReadDouble()
		case xvByte:
			// 280-289 hold a byte in DWG; the model keeps them as int16 like
			// the DXF reader.
			e.Value = int16(s.ReadRC())
		case xvInt16:
			e.Value = s.ReadShort()
		case xvInt32:
			e.Value = s.ReadInt()
		case xvInt64:
			e.Value = int64(s.ReadRawULong())
		case xvHandle:
			hex := s.ReadTextUnicode()
			h, err := strconv.ParseUint(strings.TrimSpace(hex), 16, 64)
			if err != nil {
				or.notify(NotifyWarning, "Failed to parse %s to handle", hex)
				continue
			}
			e.Value = h
			handleIdx = append(handleIdx, len(x.Entries))
		case xvBool:
			e.Value = s.ReadRC() > 0
		case xvChunk:
			n := int(s.ReadRC())
			e.Value = s.ReadBytes(n)
		case xvObjectID:
			e.Value = s.ReadRawULong()
			handleIdx = append(handleIdx, len(x.Entries))
		default:
			or.notify(NotifyWarning, "Unidentified GroupCodeValueType %d for XRecord [%d]", code, x.Handle)
			continue
		}
		x.Entries = append(x.Entries, e)
	}
	if or.R2000Plus {
		x.CloningFlags = cadoc.DictionaryCloningFlags(or.obj.ReadBitShort())
	}
	// The remaining handles are object ids of the data (or the owner,
	// reactors and xdictionary); they are only followed.
	size := or.objectInitialPos + int64(or.size)*8 - 7
	for or.hnd.PositionInBits() < size {
		or.handleRef()
	}
	t.build = func(b *builder) {
		for _, i := range handleIdx {
			h, _ := x.Entries[i].Value.(uint64)
			if o := b.object(h); o != nil {
				x.Entries[i].Value = o
			} else {
				x.Entries[i].Value = nil
				b.notify(NotifyWarning, "XRecord with handle %d reference with handle %d not found.", x.Handle, h)
			}
		}
	}
	return t
}

type xrecordValue uint8

const (
	xvNone xrecordValue = iota
	xvString
	xvPoint
	xvDouble
	xvByte
	xvInt16
	xvInt32
	xvInt64
	xvHandle
	xvObjectID
	xvBool
	xvChunk
)

// xrecordValueType is GroupCodeValue.TransformValue as the XRECORD reader
// uses it.
func xrecordValueType(code int) xrecordValue {
	switch {
	case code >= 0 && code <= 4, code >= 6 && code <= 9, code >= 100 && code <= 102, code >= 300 && code <= 309,
		code >= 410 && code <= 419, code >= 430 && code <= 439, code >= 470 && code <= 479, code >= 1000 && code <= 1003:
		return xvString
	case code == 5, code == 105, code >= 320 && code <= 329, code >= 480 && code <= 481:
		return xvHandle
	case code >= 10 && code <= 39:
		return xvPoint
	case code >= 40 && code <= 59, code >= 110 && code <= 149, code >= 210 && code <= 239, code >= 460 && code <= 469,
		code >= 1010 && code <= 1059:
		return xvDouble
	case code >= 60 && code <= 79, code >= 170 && code <= 179, code >= 270 && code <= 279, code >= 370 && code <= 389,
		code >= 400 && code <= 409, code >= 1060 && code <= 1070:
		return xvInt16
	case code >= 90 && code <= 99, code >= 420 && code <= 429, code >= 440 && code <= 459, code == 1071:
		return xvInt32
	case code >= 160 && code <= 169:
		return xvInt64
	case code >= 280 && code <= 289:
		return xvByte
	case code >= 290 && code <= 299:
		return xvBool
	case code >= 310 && code <= 319, code == 1004:
		return xvChunk
	case code >= 330 && code <= 369, code >= 390 && code <= 399, code >= 1005 && code <= 1009:
		return xvObjectID
	}
	return xvNone
}

// readPlotSettingsData reads the AcDbPlotSettings data of PLOTSETTINGS and
// LAYOUT.
func (or *objectReader) readPlotSettingsData(p *cadoc.PlotSettings) {
	p.PageName = or.text.ReadVariableText()
	p.SystemPrinterName = or.text.ReadVariableText()
	p.Flags = cadoc.PlotFlags(or.obj.ReadBitShort())
	p.UnprintableMargin.Left = or.obj.ReadBitDouble()
	p.UnprintableMargin.Bottom = or.obj.ReadBitDouble()
	p.UnprintableMargin.Right = or.obj.ReadBitDouble()
	p.UnprintableMargin.Top = or.obj.ReadBitDouble()
	p.PaperWidth = or.obj.ReadBitDouble()
	p.PaperHeight = or.obj.ReadBitDouble()
	p.PaperSize = or.text.ReadVariableText()
	p.PlotOriginX = or.obj.ReadBitDouble()
	p.PlotOriginY = or.obj.ReadBitDouble()
	p.PaperUnits = cadoc.PlotPaperUnits(or.obj.ReadBitShort())
	p.PaperRotation = cadoc.PlotRotation(or.obj.ReadBitShort())
	p.PlotType = cadoc.PlotType(or.obj.ReadBitShort())
	p.WindowLowerLeftX = or.obj.ReadBitDouble()
	p.WindowLowerLeftY = or.obj.ReadBitDouble()
	p.WindowUpperLeftX = or.obj.ReadBitDouble()
	p.WindowUpperLeftY = or.obj.ReadBitDouble()
	if or.ver >= cadoc.AC1012 && or.ver <= cadoc.AC1015 {
		p.PlotViewName = or.text.ReadVariableText()
	}
	p.NumeratorScale = or.obj.ReadBitDouble()
	p.DenominatorScale = or.obj.ReadBitDouble()
	p.StyleSheet = or.text.ReadVariableText()
	p.ScaledFit = cadoc.ScaledType(or.obj.ReadBitShort())
	p.StandardScale = or.obj.ReadBitDouble()
	p.PaperImageOrigin = or.obj.Read2BitDouble()
	if or.R2004Plus {
		p.ShadePlotMode = cadoc.ShadePlotMode(or.obj.ReadBitShort())
		p.ShadePlotResolutionMode = cadoc.ShadePlotResolutionMode(or.obj.ReadBitShort())
		p.ShadePlotDPI = or.obj.ReadBitShort()
		or.handleRef() // plot view
	}
	if or.R2007Plus {
		or.handleRef() // visual style
	}
}

func (or *objectReader) readPlotSettings() *tmpl {
	p := cadoc.NewPlotSettings("")
	t := or.newObjectTmpl(p)
	or.readCommonNonEntityData(t)
	or.readPlotSettingsData(p)
	return t
}

func (or *objectReader) readLayout() *tmpl {
	l := cadoc.NewLayout("")
	t := or.newObjectTmpl(l)
	or.readCommonNonEntityData(t)
	or.readPlotSettingsData(&l.PlotSettings)
	l.Name = or.text.ReadVariableText()
	l.TabOrder = or.obj.ReadBitLong()
	l.LayoutFlags = cadoc.LayoutFlags(or.obj.ReadBitShort())
	l.Origin = or.obj.Read3BitDouble()
	l.MinLimits = or.obj.Read2RawDouble()
	l.MaxLimits = or.obj.Read2RawDouble()
	l.InsertionBasePoint = or.obj.Read3BitDouble()
	l.XAxis = or.obj.Read3BitDouble()
	l.YAxis = or.obj.Read3BitDouble()
	l.Elevation = or.obj.ReadBitDouble()
	l.UcsOrthographicType = cadoc.OrthographicType(or.obj.ReadBitShort())
	l.MinExtents = or.obj.Read3BitDouble()
	l.MaxExtents = or.obj.Read3BitDouble()
	nViewports := 0
	if or.R2004Plus {
		nViewports = int(or.obj.ReadBitLong())
	}
	block := or.handleRef()
	viewport := or.handleRef()
	baseUcs := or.handleRef()
	namedUcs := or.handleRef()
	if or.R2004Plus {
		for i := 0; i < nViewports; i++ {
			or.handleRef()
		}
	}
	t.build = func(b *builder) {
		if r, ok := b.object(block).(*cadoc.BlockRecord); ok {
			l.SetBlock(r)
		}
		if vp, ok := b.object(viewport).(*cadoc.Viewport); ok {
			l.LastActiveViewport = vp
		}
		if u, ok := b.object(baseUcs).(*cadoc.UCS); ok {
			l.BaseUCS = u
		}
		if u, ok := b.object(namedUcs).(*cadoc.UCS); ok {
			l.UCS = u
		}
	}
	return t
}

func (or *objectReader) readImageDefinition() *tmpl {
	d := cadoc.NewImageDefinition("")
	t := or.newObjectTmpl(d)
	or.readCommonNonEntityData(t)
	d.ClassVersion = or.merged.ReadBitLong()
	d.Size = or.merged.Read2RawDouble()
	d.FileName = or.merged.ReadVariableText()
	d.IsLoaded = or.merged.ReadBit()
	d.Units = cadoc.ResolutionUnit(or.merged.ReadRC())
	d.DefaultSize = or.merged.Read2RawDouble()
	return t
}

func (or *objectReader) readImageDefinitionReactor() *tmpl {
	r := &cadoc.ImageDefinitionReactor{}
	t := or.newObjectTmpl(r)
	or.readCommonNonEntityData(t)
	r.ClassVersion = or.obj.ReadBitLong()
	return t
}

func (or *objectReader) readRasterVariables() *tmpl {
	v := &cadoc.RasterVariables{}
	t := or.newObjectTmpl(v)
	or.readCommonNonEntityData(t)
	v.ClassVersion = or.merged.ReadBitLong()
	v.IsDisplayFrameShown = or.merged.ReadBitShort() != 0
	v.DisplayQuality = cadoc.ImageDisplayQuality(or.merged.ReadBitShort())
	v.Units = cadoc.ImageUnits(or.merged.ReadBitShort())
	return t
}

func (or *objectReader) readWipeoutVariables() *tmpl {
	v := &cadoc.WipeoutVariables{}
	t := or.newObjectTmpl(v)
	or.readCommonNonEntityData(t)
	v.DisplayImageFrame = or.merged.ReadBitShort() != 0
	return t
}

func (or *objectReader) readScale() *tmpl {
	s := &cadoc.Scale{}
	t := or.newObjectTmpl(s)
	or.readCommonNonEntityData(t)
	or.merged.ReadBitShort()
	s.Name = or.merged.ReadVariableText()
	s.PaperUnits = or.merged.ReadBitDouble()
	s.DrawingUnits = or.merged.ReadBitDouble()
	s.IsUnitScale = or.merged.ReadBit()
	return t
}

func (or *objectReader) readSortentsTable() *tmpl {
	s := &cadoc.SortEntitiesTable{}
	t := or.newObjectTmpl(s)
	or.readCommonNonEntityData(t)
	owner := or.handleRef()
	n := or.obj.count(or.merged.ReadBitLong(), 8)
	type pair struct{ sort, entity uint64 }
	pairs := make([]pair, n)
	for i := range pairs {
		// The sort handle is in the data stream, the entity in the handles.
		pairs[i].sort = or.obj.HandleReference(0)
		pairs[i].entity = or.handleRef()
	}
	t.build = func(b *builder) {
		if o := b.object(owner); o != nil {
			r, ok := o.(*cadoc.BlockRecord)
			if !ok {
				b.notify(NotifyWarning, "Block owner for SortEntitiesTable %d is not a block %s | %d", s.Handle, o.ObjectName(), o.Base().Handle)
				return
			}
			s.BlockOwner = r
		}
		for _, p := range pairs {
			if e := b.entity(p.entity); e != nil {
				s.Add(e, p.sort)
			} else {
				b.notify(NotifyWarning, "Entity in SortEntitiesTable %d not found %d", s.Handle, p.entity)
			}
		}
	}
	return t
}

func (or *objectReader) read4x3Matrix() (m [12]float64) {
	for i := range m {
		m[i] = or.merged.ReadBitDouble()
	}
	return m
}

func (or *objectReader) readSpatialFilter() *tmpl {
	f := cadoc.NewSpatialFilter()
	f.Name = ""
	t := or.newObjectTmpl(f)
	or.readCommonNonEntityData(t)
	n := int(or.merged.ReadBitShort())
	for i := 0; i < n; i++ {
		f.BoundaryPoints = append(f.BoundaryPoints, or.merged.Read2RawDouble())
	}
	f.Normal = or.merged.Read3BitDouble()
	f.Origin = or.merged.Read3BitDouble()
	f.DisplayBoundary = or.merged.ReadBitShort() != 0
	f.ClipFrontPlane = or.merged.ReadBitShort() != 0
	if f.ClipFrontPlane {
		f.FrontDistance = or.merged.ReadBitDouble()
	}
	f.ClipBackPlane = or.merged.ReadBitShort() != 0
	if f.ClipBackPlane {
		f.BackDistance = or.merged.ReadBitDouble()
	}
	f.InverseInsertTransform = or.read4x3Matrix()
	f.InsertTransform = or.read4x3Matrix()
	return t
}

func (or *objectReader) readDbColor() *tmpl {
	c := &cadoc.BookColor{}
	t := or.newObjectTmpl(c)
	or.readCommonNonEntityData(t)
	index := or.obj.ReadBitShort()
	if or.R2004Plus {
		rgb := uint32(or.obj.ReadBitLong())
		flags := or.obj.ReadRC()
		var colorName, bookName string
		if flags&1 > 0 {
			colorName = or.text.ReadVariableText()
		}
		if flags&2 > 0 {
			bookName = or.text.ReadVariableText()
		}
		if colorName != "" {
			c.Name = bookName + "$" + colorName
		}
		c.Color = cadoc.ColorRGB(byte(rgb>>16), byte(rgb>>8), byte(rgb))
	} else {
		c.Color = colorIndex(index)
	}
	return t
}

func (or *objectReader) readGeoData() *tmpl {
	g := cadoc.NewGeoData()
	t := or.newObjectTmpl(g)
	or.readCommonNonEntityData(t)
	m := or.merged
	g.Version = cadoc.GeoDataVersion(m.ReadBitLong())
	host := or.handleRef()
	g.CoordinatesType = cadoc.DesignCoordinatesType(m.ReadBitShort())
	switch g.Version {
	case cadoc.GeoDataVersionR2009:
		g.ReferencePoint = m.Read3BitDouble()
		g.HorizontalUnits = cadoc.UnitsType(m.ReadBitLong())
		g.VerticalUnits = g.HorizontalUnits
		g.DesignPoint = m.Read3BitDouble()
		m.Read3BitDouble()
		g.UpDirection = m.Read3BitDouble()
		angle := math.Pi/2 - m.ReadBitDouble()
		g.NorthDirection = cadoc.Vec2{X: math.Cos(angle), Y: math.Sin(angle)}
		m.Read3BitDouble()
		g.CoordinateSystemDefinition = m.ReadVariableText()
		g.GeoRssTag = m.ReadVariableText()
		g.HorizontalUnitScale = m.ReadBitDouble()
		g.VerticalUnitScale = g.HorizontalUnitScale
		m.ReadVariableText()
		m.ReadVariableText()
	case cadoc.GeoDataVersionR2010, cadoc.GeoDataVersionR2013:
		g.DesignPoint = m.Read3BitDouble()
		g.ReferencePoint = m.Read3BitDouble()
		g.HorizontalUnitScale = m.ReadBitDouble()
		g.HorizontalUnits = cadoc.UnitsType(m.ReadBitLong())
		g.VerticalUnitScale = m.ReadBitDouble()
		// ACadSharp stores the vertical units in HorizontalUnits.
		g.VerticalUnits = cadoc.UnitsType(m.ReadBitLong())
		g.UpDirection = m.Read3BitDouble()
		g.NorthDirection = m.Read2RawDouble()
		g.ScaleEstimationMethod = cadoc.ScaleEstimationType(m.ReadBitLong())
		g.UserSpecifiedScaleFactor = m.ReadBitDouble()
		g.EnableSeaLevelCorrection = m.ReadBit()
		g.SeaLevelElevation = m.ReadBitDouble()
		g.CoordinateProjectionRadius = m.ReadBitDouble()
		g.CoordinateSystemDefinition = m.ReadVariableText()
		g.GeoRssTag = m.ReadVariableText()
	}
	g.ObservationFromTag = m.ReadVariableText()
	g.ObservationToTag = m.ReadVariableText()
	g.ObservationCoverageTag = m.ReadVariableText()
	n := int(m.ReadBitLong())
	for i := 0; i < n; i++ {
		s := m.Read2RawDouble()
		g.Points = append(g.Points, cadoc.GeoMeshPoint{Source: s, Destination: m.Read2RawDouble()})
	}
	n = int(m.ReadBitLong())
	for i := 0; i < n; i++ {
		f := cadoc.GeoMeshFace{}
		f.Index1 = m.ReadBitLong()
		f.Index2 = m.ReadBitLong()
		f.Index3 = m.ReadBitLong()
		g.Faces = append(g.Faces, f)
	}
	t.build = func(b *builder) {
		if r, ok := tableRef(b, t, b.blockRecords, host, ""); ok {
			g.HostBlock = r
		}
	}
	return t
}

func (or *objectReader) readMultiLeaderStyle() *tmpl {
	s := cadoc.NewMultiLeaderStyle("")
	t := or.newObjectTmpl(s)
	or.readCommonNonEntityData(t)
	if or.R2010Plus {
		or.obj.ReadBitShort() // version 2
	}
	s.ContentType = cadoc.LeaderContentType(or.obj.ReadBitShort())
	s.MultiLeaderDrawOrder = cadoc.MultiLeaderDrawOrderType(or.obj.ReadBitShort())
	s.LeaderDrawOrder = cadoc.LeaderDrawOrderType(or.obj.ReadBitShort())
	s.MaxLeaderSegmentsPoints = or.obj.ReadBitLong()
	s.FirstSegmentAngleConstraint = or.obj.ReadBitDouble()
	s.SecondSegmentAngleConstraint = or.obj.ReadBitDouble()
	s.PathType = cadoc.MultiLeaderPathType(or.obj.ReadBitShort())
	s.LineColor = or.merged.ReadCmColor(false)
	lineType := or.handleRef()
	s.LeaderLineWeight = cadoc.LineWeight(or.obj.ReadBitLong())
	s.EnableLanding = or.obj.ReadBit()
	s.LandingGap = or.obj.ReadBitDouble()
	s.EnableDogleg = or.obj.ReadBit()
	s.LandingDistance = or.obj.ReadBitDouble()
	s.Description = or.merged.ReadVariableText()
	arrow := or.handleRef()
	s.ArrowheadSize = or.obj.ReadBitDouble()
	s.DefaultTextContents = or.merged.ReadVariableText()
	textStyle := or.handleRef()
	s.TextLeftAttachment = cadoc.TextAttachmentType(or.obj.ReadBitShort())
	s.TextRightAttachment = cadoc.TextAttachmentType(or.obj.ReadBitShort())
	s.TextAngle = cadoc.TextAngleType(or.obj.ReadBitShort())
	s.TextAlignment = cadoc.TextAlignmentType(or.obj.ReadBitShort())
	s.TextColor = or.merged.ReadCmColor(false)
	s.TextHeight = or.obj.ReadBitDouble()
	s.TextFrame = or.obj.ReadBit()
	s.TextAlignAlwaysLeft = or.obj.ReadBit()
	s.AlignSpace = or.obj.ReadBitDouble()
	block := or.handleRef()
	s.BlockContentColor = or.merged.ReadCmColor(false)
	s.BlockContentScale = or.obj.Read3BitDouble()
	s.EnableBlockContentScale = or.obj.ReadBit()
	s.BlockContentRotation = or.obj.ReadBitDouble()
	s.EnableBlockContentRotation = or.obj.ReadBit()
	s.BlockContentConnection = cadoc.BlockContentConnectionType(or.obj.ReadBitShort())
	s.ScaleFactor = or.obj.ReadBitDouble()
	// ACadSharp reads these two the other way round; the DXF of every
	// sample version shows the annotative flag first.
	s.IsAnnotative = or.obj.ReadBit()
	s.OverwritePropertyValue = or.obj.ReadBit()
	s.BreakGapSize = or.obj.ReadBitDouble()
	if or.R2010Plus {
		s.TextAttachmentDirection = cadoc.TextAttachmentDirectionType(or.obj.ReadBitShort())
		s.TextBottomAttachment = cadoc.TextAttachmentType(or.obj.ReadBitShort())
		s.TextTopAttachment = cadoc.TextAttachmentType(or.obj.ReadBitShort())
	}
	if or.R2013Plus {
		s.UnknownFlag298 = or.obj.ReadBit()
	}
	t.build = func(b *builder) {
		if l, ok := b.object(lineType).(*cadoc.LineType); ok {
			s.LeaderLineType = l
		}
		if r, ok := b.object(arrow).(*cadoc.BlockRecord); ok {
			s.Arrowhead = r
		}
		if ts, ok := b.object(textStyle).(*cadoc.TextStyle); ok {
			s.TextStyle = ts
		}
		if r, ok := b.object(block).(*cadoc.BlockRecord); ok {
			s.BlockContent = r
		}
	}
	return t
}

// readAnnotScaleObjectContextData reads the ObjectContextData and
// AnnotScaleObjectContextData parts; it returns the scale handle.
func (or *objectReader) readAnnotScaleObjectContextData(t *tmpl, c *cadoc.ObjectContextDataBase) uint64 {
	or.readCommonNonEntityData(t)
	c.Version = or.obj.ReadBitShort()
	c.HasFileToExtensionDictionary = or.obj.ReadBit() // default flag
	return or.handleRef()
}

func (b *builder) buildScale(c *cadoc.ObjectContextDataBase, h uint64) {
	if s, ok := b.object(h).(*cadoc.Scale); ok {
		c.Scale = s
	}
}

func (or *objectReader) readMultiLeaderAnnotContext() *tmpl {
	c := cadoc.NewMultiLeaderObjectContextData()
	t := or.newObjectTmpl(c)
	scale := or.readAnnotScaleObjectContextData(t, &c.ObjectContextDataBase)
	ct := &mleaderContextTmpl{}
	or.readMultiLeaderAnnotContextData(c, ct)
	t.build = func(b *builder) {
		b.buildScale(&c.ObjectContextDataBase, scale)
		b.buildMLeaderContext(c, ct)
	}
	return t
}

func (or *objectReader) readBlkRefObjectContextData() *tmpl {
	c := &cadoc.BlockReferenceObjectContextData{}
	t := or.newObjectTmpl(c)
	scale := or.readAnnotScaleObjectContextData(t, &c.ObjectContextDataBase)
	c.Rotation = or.merged.ReadBitDouble()
	c.InsertionPoint = or.merged.Read3BitDouble()
	c.XScale = or.merged.ReadBitDouble()
	c.YScale = or.merged.ReadBitDouble()
	c.ZScale = or.merged.ReadBitDouble()
	t.build = func(b *builder) { b.buildScale(&c.ObjectContextDataBase, scale) }
	return t
}

func (or *objectReader) readMaterial() *tmpl {
	m := &cadoc.Material{}
	t := or.newObjectTmpl(m)
	or.readCommonNonEntityData(t)
	// The model keeps the names only; the material definition that follows
	// has no references and is not decoded.
	m.Name = or.merged.ReadVariableText()
	m.Description = or.merged.ReadVariableText()
	return t
}

func (or *objectReader) readVisualStyle() *tmpl {
	v := &cadoc.VisualStyle{}
	t := or.newObjectTmpl(v)
	or.readCommonNonEntityData(t)
	v.Name = or.text.ReadVariableText()
	v.Type = or.obj.ReadBitLong()
	// The rest of the object is undocumented (as in ACadSharp).
	return t
}

func (or *objectReader) readDynamicBlockPurgePreventer() *tmpl {
	p := &cadoc.DynamicBlockPurgePreventer{}
	t := or.newObjectTmpl(p)
	or.readCommonNonEntityData(t)
	p.Version = or.merged.ReadBitShort()
	block := or.handleRef()
	t.build = func(b *builder) {
		if r, ok := tableRef(b, t, b.blockRecords, block, ""); ok {
			p.Block = r
		}
	}
	return t
}

func (or *objectReader) readPdfDefinition() *tmpl {
	d := &cadoc.PdfUnderlayDefinition{}
	t := or.newObjectTmpl(d)
	or.readCommonNonEntityData(t)
	// ACadSharp reads these from the data stream, which only works before
	// R2007; the strings belong to the string stream.
	d.File = or.text.ReadVariableText()
	d.Page = or.text.ReadVariableText()
	return t
}

// ----- fields -----

// readCadValue reads a CadValue; a handle value is returned for the build.
func (or *objectReader) readCadValue() (v cadoc.CadValue, handle uint64) {
	m := or.merged
	if or.R2007Plus {
		v.Flags = m.ReadBitLong()
	}
	v.ValueType = cadoc.CadValueType(m.ReadBitLong())
	if !or.R2007Plus || v.Flags&1 == 0 {
		switch v.ValueType {
		case cadoc.CadValueTypeUnknown, cadoc.CadValueTypeLong:
			v.Value = m.ReadBitLong()
		case cadoc.CadValueTypeDouble:
			v.Value = m.ReadBitDouble()
		case cadoc.CadValueTypeGeneral, cadoc.CadValueTypeString:
			v.Value = or.readStringCadValue()
		case cadoc.CadValueTypeDate:
			if d, ok := or.readDateCadValue(); ok {
				v.Value = d
			}
		case cadoc.CadValueTypePoint2D:
			if m.ReadBitLong() <= 0 {
				panic(errorf("empty 2D point value"))
			}
			p := m.Read2RawDouble()
			v.Value = cadoc.Vec3{X: p.X, Y: p.Y}
		case cadoc.CadValueTypePoint3D:
			if m.ReadBitLong() <= 0 {
				panic(errorf("empty 3D point value"))
			}
			v.Value = m.Read3RawDouble()
		case cadoc.CadValueTypeHandle:
			handle = or.handleRef()
		default:
			panic(errorf("CadValue type %d not implemented", v.ValueType))
		}
	}
	if or.R2007Plus {
		v.Units = cadoc.CadValueUnitType(m.ReadBitLong())
		v.Format = m.ReadVariableText()
		v.FormattedValue = m.ReadVariableText()
	}
	return v, handle
}

func (or *objectReader) readStringCadValue() string {
	n := int(or.merged.ReadBitLong())
	b := or.merged.ReadBytes(n)
	if or.R2007Plus {
		if n < 2 {
			panic(errorf("invalid string value length %d", n))
		}
		return decodeUTF16LE(b[:n-2])
	}
	return or.r.text.decode(b)
}

func (or *objectReader) readDateCadValue() (time.Time, bool) {
	n := int(or.merged.ReadBitLong())
	if n <= 0 {
		return time.Time{}, false
	}
	a := or.merged.ReadBytes(n)
	i16 := func(o int) int { return int(int16(binary.LittleEndian.Uint16(a[o:]))) }
	if or.R2007Plus {
		switch n {
		case 16:
			return time.Date(i16(0), time.Month(i16(2)), i16(6), i16(8), i16(10), i16(12), i16(14)*1e6, time.Local), true
		case 14:
			return time.Date(i16(0), time.Month(i16(2)), i16(4), i16(6), i16(8), i16(10), i16(12)*1e6, time.Local), true
		}
		return time.Time{}, false
	}
	if n != 8 {
		return time.Time{}, false
	}
	high := int64(int32(binary.BigEndian.Uint32(a)))
	low := int64(binary.BigEndian.Uint32(a[4:]))
	return time.Unix(high<<32|low, 0).UTC(), true
}

func (or *objectReader) readField() *tmpl {
	f := &cadoc.Field{}
	t := or.newObjectTmpl(f)
	or.readCommonNonEntityData(t)
	m := or.merged
	f.EvaluatorId = m.ReadVariableText()
	f.FieldCode = m.ReadVariableText()
	n := int(m.ReadBitLong())
	var children, objects []uint64
	for i := 0; i < n; i++ {
		children = append(children, or.handleRef())
	}
	n = int(m.ReadBitLong())
	for i := 0; i < n; i++ {
		objects = append(objects, or.handleRef())
	}
	if or.ver < cadoc.AC1021 {
		f.FormatString = m.ReadVariableText()
	}
	f.EvaluationOptionFlags = cadoc.EvaluationOptionFlags(m.ReadBitLong())
	f.FilingOptionFlags = cadoc.FilingOptionFlags(m.ReadBitLong())
	f.FieldStateFlags = cadoc.FieldStateFlags(m.ReadBitLong())
	f.EvaluationStatusFlags = cadoc.EvaluationStatusFlags(m.ReadBitLong())
	f.EvaluationErrorCode = m.ReadBitLong()
	f.EvaluationErrorMessage = m.ReadVariableText()
	var valueHandles []uint64
	var vh uint64
	f.Value, vh = or.readCadValue()
	valueHandles = append(valueHandles, vh)
	// The value string; ACadSharp stores it in FormatString.
	f.FormatString = m.ReadVariableText()
	m.ReadBitLong()
	n = int(m.ReadBitLong())
	for i := 0; i < n; i++ {
		key := m.ReadVariableText()
		v, h := or.readCadValue()
		f.Values = append(f.Values, cadoc.FieldValue{Key: key, Value: v})
		valueHandles = append(valueHandles, h)
	}
	t.build = func(b *builder) {
		for _, h := range objects {
			if o := b.object(h); o != nil {
				f.CadObjects = append(f.CadObjects, o)
			} else {
				b.notify(NotifyWarning, "[%s] CadObject with handle %d not found.", f.SubclassMarker(), h)
			}
		}
		for _, h := range children {
			if c, ok := b.object(h).(*cadoc.Field); ok {
				f.Children = append(f.Children, c)
			} else {
				b.notify(NotifyWarning, "[%s] CadObject with handle %d not found.", f.SubclassMarker(), h)
			}
		}
		for i, h := range valueHandles {
			o := b.object(h)
			if o == nil {
				continue
			}
			if i == 0 {
				f.Value.Value, f.Value.ValueType = o, cadoc.CadValueTypeHandle
			} else {
				f.Values[i-1].Value.Value, f.Values[i-1].Value.ValueType = o, cadoc.CadValueTypeHandle
			}
		}
	}
	return t
}

func (or *objectReader) readFieldList() *tmpl {
	l := &cadoc.FieldList{}
	t := or.newObjectTmpl(l)
	or.readCommonNonEntityData(t)
	n := int(or.merged.ReadBitLong())
	or.merged.ReadBit()
	var handles []uint64
	for i := 0; i < n; i++ {
		handles = append(handles, or.handleRef())
	}
	t.build = func(b *builder) {
		seen := map[uint64]bool{}
		for _, h := range handles {
			if seen[h] {
				continue
			}
			seen[h] = true
			if f, ok := b.object(h).(*cadoc.Field); ok {
				l.Fields = append(l.Fields, f)
			} else {
				b.notify(NotifyWarning, "Field %d not found for FieldList %d", h, l.Handle)
			}
		}
	}
	return t
}
