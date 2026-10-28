package dwg

import (
	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

const minRadius = 1e-12 // MathHelper.Epsilon

// ----- blocks, inserts, sequences -----

func (or *objectReader) readBlock() *tmpl {
	blk := cadoc.NewBlock()
	t := or.newEntityTmpl(blk)
	or.readCommonEntityData(t)
	if name := or.text.ReadVariableText(); name != "" && !isBlank(name) {
		t.name = name
	}
	return t
}

func isBlank(s string) bool {
	for _, c := range s {
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' && c != '\v' && c != '\f' {
			return false
		}
	}
	return true
}

func (or *objectReader) readEndBlock() *tmpl {
	t := or.newEntityTmpl(cadoc.NewBlockEnd())
	or.readCommonEntityData(t)
	return t
}

func (or *objectReader) readSeqend() *tmpl {
	t := or.newEntityTmpl(cadoc.NewSeqend())
	or.readCommonEntityData(t)
	return t
}

type insertTmpl struct {
	hasAtts                 bool
	ownedCount              int
	block                   uint64
	firstAttrib, lastAttrib uint64
	hasFirst                bool
	seqend                  uint64
	owned                   []uint64
}

func (or *objectReader) readInsertCommonData(t *tmpl, ins *cadoc.Insert, it *insertTmpl) {
	or.readCommonEntityData(t)
	ins.InsertPoint = or.obj.Read3BitDouble()
	if or.R13_14Only {
		s := or.obj.Read3BitDouble()
		ins.XScale, ins.YScale, ins.ZScale = s.X, s.Y, s.Z
	}
	if or.R2000Plus {
		switch or.obj.Read2Bits() {
		case 0:
			ins.XScale = or.obj.ReadDouble()
			ins.YScale = or.obj.ReadBitDoubleWithDefault(ins.XScale)
			ins.ZScale = or.obj.ReadBitDoubleWithDefault(ins.XScale)
		case 1:
			ins.YScale = or.obj.ReadBitDoubleWithDefault(ins.XScale)
			ins.ZScale = or.obj.ReadBitDoubleWithDefault(ins.XScale)
		case 2:
			s := or.obj.ReadDouble()
			ins.XScale, ins.YScale, ins.ZScale = s, s, s
		case 3:
			ins.XScale, ins.YScale, ins.ZScale = 1, 1, 1
		}
	}
	ins.Rotation = or.obj.ReadBitDouble()
	ins.Normal = or.obj.Read3BitDouble()
	it.hasAtts = or.obj.ReadBit()
	if or.R2004Plus && it.hasAtts {
		it.ownedCount = int(or.obj.ReadBitLong())
	}
}

func (or *objectReader) readInsertCommonHandles(it *insertTmpl) {
	it.block = or.handleRef()
	if !it.hasAtts {
		return
	}
	if or.ver >= cadoc.AC1012 && or.ver <= cadoc.AC1015 {
		it.firstAttrib = or.handleRef()
		it.lastAttrib = or.handleRef()
		it.hasFirst = true
	} else if or.R2004Plus {
		for i := 0; i < it.ownedCount; i++ {
			it.owned = append(it.owned, or.handleRef())
		}
	}
	it.seqend = or.handleRef()
}

// buildInsert resolves the block, attributes and SEQEND (CadInsertTemplate).
func (b *builder) buildInsert(t *tmpl, ins *cadoc.Insert, it *insertTmpl) {
	if r, ok := tableRef(b, t, b.blockRecords, it.block, ""); ok {
		ins.Block = r
	} else {
		b.notify(NotifyWarning, "Block %d |  not found for Insert %d", it.block, ins.Handle)
	}
	if s, ok := b.object(it.seqend).(*cadoc.Seqend); ok {
		ins.AttributesSeqend = s
		s.Owner = t.obj
	}
	var atts []cadoc.Entity
	if it.hasFirst {
		atts = b.entitiesCollection(it.firstAttrib, it.lastAttrib)
	} else {
		for _, h := range it.owned {
			if a := b.entity(h); a != nil {
				atts = append(atts, a)
			}
		}
	}
	for _, e := range atts {
		a, ok := e.(*cadoc.AttributeEntity)
		if !ok {
			continue
		}
		if a.Owner != nil {
			b.notify(NotifyError, "Item %s already has an owner", a.ObjectName())
			continue
		}
		a.Owner = t.obj
		ins.Attributes = append(ins.Attributes, a)
	}
}

func (or *objectReader) readInsert() *tmpl {
	ins := cadoc.NewInsert(nil)
	t := or.newEntityTmpl(ins)
	it := &insertTmpl{}
	or.readInsertCommonData(t, ins, it)
	or.readInsertCommonHandles(it)
	t.build = func(b *builder) { b.buildInsert(t, ins, it) }
	return t
}

func (or *objectReader) readMInsert() *tmpl {
	ins := cadoc.NewInsert(nil)
	t := or.newEntityTmpl(ins)
	it := &insertTmpl{}
	or.readInsertCommonData(t, ins, it)
	ins.ColumnCount = uint16(or.obj.ReadBitShort())
	ins.RowCount = uint16(or.obj.ReadBitShort())
	ins.ColumnSpacing = or.obj.ReadBitDouble()
	ins.RowSpacing = or.obj.ReadBitDouble()
	or.readInsertCommonHandles(it)
	t.build = func(b *builder) { b.buildInsert(t, ins, it) }
	return t
}

// ----- text -----

// readCommonTextData reads the TEXT data shared with ATTRIB and ATTDEF.
func (or *objectReader) readCommonTextData(t *tmpl, text *cadoc.TextEntity) (style uint64) {
	or.readCommonEntityData(t)
	elevation := 0.0
	if or.R13_14Only {
		elevation = or.obj.ReadBitDouble()
		pt := or.obj.Read2RawDouble()
		text.InsertPoint = cadoc.Vec3{X: pt.X, Y: pt.Y, Z: elevation}
		pt = or.obj.Read2RawDouble()
		text.AlignmentPoint = cadoc.Vec3{X: pt.X, Y: pt.Y, Z: elevation}
		text.Normal = or.obj.Read3BitDouble()
		text.Thickness = or.obj.ReadBitDouble()
		text.ObliqueAngle = or.obj.ReadBitDouble()
		text.Rotation = or.obj.ReadBitDouble()
		text.Height = or.obj.ReadBitDouble()
		text.WidthFactor = or.obj.ReadBitDouble()
		text.Value = or.text.ReadVariableText()
		text.Mirror = cadoc.TextMirrorFlag(or.obj.ReadBitShort())
		text.HorizontalAlignment = cadoc.TextHorizontalAlignment(or.obj.ReadBitShort())
		text.VerticalAlignment = cadoc.TextVerticalAlignmentType(or.obj.ReadBitShort())
		return or.handleRef()
	}
	flags := or.obj.ReadRC()
	if flags&0x1 == 0 {
		elevation = or.obj.ReadDouble()
	}
	pt := or.obj.Read2RawDouble()
	text.InsertPoint = cadoc.Vec3{X: pt.X, Y: pt.Y, Z: elevation}
	if flags&0x2 == 0 {
		x := or.obj.ReadBitDoubleWithDefault(text.InsertPoint.X)
		y := or.obj.ReadBitDoubleWithDefault(text.InsertPoint.Y)
		text.AlignmentPoint = cadoc.Vec3{X: x, Y: y, Z: elevation}
	}
	text.Normal = or.obj.ReadBitExtrusion()
	text.Thickness = or.obj.ReadBitThickness()
	if flags&0x4 == 0 {
		text.ObliqueAngle = or.obj.ReadDouble()
	}
	if flags&0x8 == 0 {
		text.Rotation = or.obj.ReadDouble()
	}
	text.Height = or.obj.ReadDouble()
	if flags&0x10 == 0 {
		text.WidthFactor = or.obj.ReadDouble()
	}
	text.Value = or.text.ReadVariableText()
	if flags&0x20 == 0 {
		text.Mirror = cadoc.TextMirrorFlag(or.obj.ReadBitShort())
	}
	if flags&0x40 == 0 {
		text.HorizontalAlignment = cadoc.TextHorizontalAlignment(or.obj.ReadBitShort())
	}
	if flags&0x80 == 0 {
		text.VerticalAlignment = cadoc.TextVerticalAlignmentType(or.obj.ReadBitShort())
	}
	return or.handleRef()
}

func (b *builder) buildTextStyle(t *tmpl, style uint64, set func(*cadoc.TextStyle)) {
	if s, ok := tableRef(b, t, b.textStyles, style, ""); ok {
		set(s)
	}
}

func (or *objectReader) readText() *tmpl {
	text := cadoc.NewText()
	t := or.newEntityTmpl(text)
	style := or.readCommonTextData(t, text)
	t.build = func(b *builder) { b.buildTextStyle(t, style, func(s *cadoc.TextStyle) { text.Style = s }) }
	return t
}

// readCommonAttData reads the ATTRIB/ATTDEF data; a multiline attribute
// embeds a whole MTEXT.
func (or *objectReader) readCommonAttData(att *cadoc.AttributeBase) (mtext *tmpl) {
	if or.R2010Plus {
		att.Version = or.obj.ReadRC()
	}
	if or.R2018Plus {
		att.AttributeType = cadoc.AttributeType(or.obj.ReadRC())
	}
	switch att.AttributeType {
	case cadoc.AttributeTypeMultiLine, cadoc.AttributeTypeConstantMultiLine:
		att.MText = cadoc.NewMText()
		mtext = or.newEntityTmpl(att.MText)
		or.readEntityMode(mtext)
		or.readMTextData(mtext, att.MText)
		if n := or.obj.ReadBitShort(); n > 0 {
			or.obj.ReadBytes(int(n)) // annotative data
			or.handleRef()           // registered application
			or.obj.ReadBitShort()
		}
	}
	att.Tag = or.text.ReadVariableText()
	or.obj.ReadBitShort() // field length
	att.Flags = cadoc.AttributeFlags(or.obj.ReadRC())
	if or.R2007Plus {
		att.IsLocked = or.obj.ReadBit()
	}
	return mtext
}

func (or *objectReader) readAttribute() *tmpl {
	att := cadoc.NewAttribute()
	t := or.newEntityTmpl(att)
	style := or.readCommonTextData(t, &att.TextEntity)
	mt := or.readCommonAttData(&att.AttributeBase)
	t.build = func(b *builder) {
		b.buildTextStyle(t, style, func(s *cadoc.TextStyle) { att.Style = s })
		b.buildTemplate(mt)
	}
	return t
}

func (or *objectReader) readAttributeDefinition() *tmpl {
	def := cadoc.NewAttributeDefinition()
	t := or.newEntityTmpl(def)
	style := or.readCommonTextData(t, &def.TextEntity)
	mt := or.readCommonAttData(&def.AttributeBase)
	if or.R2010Plus {
		def.Version = or.obj.ReadRC()
	}
	def.Prompt = or.text.ReadVariableText()
	t.build = func(b *builder) {
		b.buildTextStyle(t, style, func(s *cadoc.TextStyle) { def.Style = s })
		b.buildTemplate(mt)
	}
	return t
}

func (or *objectReader) readMText() *tmpl {
	m := cadoc.NewMText()
	t := or.newEntityTmpl(m)
	or.readCommonEntityData(t)
	or.readMTextData(t, m)
	return t
}

// readMTextData reads the MTEXT data after the common entity data and sets
// the style reference of t.
func (or *objectReader) readMTextData(t *tmpl, m *cadoc.MText) {
	m.InsertPoint = or.obj.Read3BitDouble()
	m.Normal = or.obj.Read3BitDouble()
	m.AlignmentPoint = or.obj.Read3BitDouble()
	m.RectangleWidth = or.obj.ReadBitDouble()
	if or.R2007Plus {
		m.RectangleHeight = or.obj.ReadBitDouble()
	}
	m.Height = or.obj.ReadBitDouble()
	m.AttachmentPoint = cadoc.AttachmentPointType(or.obj.ReadBitShort())
	m.DrawingDirection = cadoc.DrawingDirectionType(or.obj.ReadBitShort())
	// Extents height and width: undocumented, ACadSharp drops them.
	or.obj.ReadBitDouble()
	or.obj.ReadBitDouble()
	m.Value = or.text.ReadVariableText()
	style := or.handleRef()
	t.build = func(b *builder) { b.buildTextStyle(t, style, func(s *cadoc.TextStyle) { m.Style = s }) }
	if or.R2000Plus {
		m.LineSpacingStyle = cadoc.LineSpacingStyleType(or.obj.ReadBitShort())
		m.LineSpacing = or.obj.ReadBitDouble()
		or.obj.ReadBit()
	}
	if or.R2004Plus {
		m.BackgroundFillFlags = cadoc.BackgroundFillFlags(or.obj.ReadBitLong())
		if m.BackgroundFillFlags&cadoc.BackgroundFillFlagsUseBackgroundFillColor != 0 ||
			or.ver > cadoc.AC1027 && m.BackgroundFillFlags&cadoc.BackgroundFillFlagsTextFrame > 0 {
			m.BackgroundScale = or.obj.ReadBitDouble()
			m.BackgroundColor = or.merged.ReadCmColor(false)
			// ACadSharp casts the value to a percentage; it is the alpha
			// encoded value of DXF code 441.
			m.BackgroundTransparency = transparencyFromAlpha(or.obj.ReadBitLong())
		}
	}
	if !or.R2018Plus {
		return
	}
	m.IsAnnotative = !or.obj.ReadBit()
	if m.IsAnnotative {
		return
	}
	or.obj.ReadBitShort() // version
	or.obj.ReadBit()      // default flag
	or.handleRef()        // registered application
	// Redundant copies of the fields above.
	or.obj.ReadBitLong()
	or.obj.Read3BitDouble()
	or.obj.Read3BitDouble()
	or.obj.ReadBitDouble()
	or.obj.ReadBitDouble()
	or.obj.ReadBitDouble()
	or.obj.ReadBitDouble()
	m.ColumnData.ColumnType = cadoc.ColumnType(or.obj.ReadBitShort())
	if m.ColumnData.ColumnType != cadoc.ColumnTypeNoColumns {
		count := int(or.obj.ReadBitLong())
		m.ColumnData.ColumnCount = int32(count)
		m.ColumnData.Width = or.obj.ReadBitDouble()
		m.ColumnData.Gutter = or.obj.ReadBitDouble()
		m.ColumnData.AutoHeight = or.obj.ReadBit()
		m.ColumnData.FlowReversed = or.obj.ReadBit()
		if !m.ColumnData.AutoHeight && m.ColumnData.ColumnType == cadoc.ColumnTypeDynamicColumns && count > 0 {
			for i := 0; i < count; i++ {
				m.ColumnData.Heights = append(m.ColumnData.Heights, or.obj.ReadBitDouble())
			}
		}
	}
}

// ----- polylines -----

type polylineTmpl struct {
	first, last uint64
	hasFirst    bool
	seqend      uint64
	owned       []uint64
}

func (or *objectReader) readPolylineHandles(pt *polylineTmpl, ownedFirst bool) {
	readOwned := func() {
		if or.R2004Plus {
			n := int(or.obj.ReadBitLong())
			for i := 0; i < n; i++ {
				pt.owned = append(pt.owned, or.handleRef())
			}
		}
	}
	readFirstLast := func() {
		if or.R13_15Only {
			pt.first = or.handleRef()
			pt.last = or.handleRef()
			pt.hasFirst = true
		}
	}
	if ownedFirst {
		readOwned()
		readFirstLast()
	} else {
		readFirstLast()
		readOwned()
	}
	pt.seqend = or.handleRef()
}

// buildPolyline adds the vertices and the SEQEND (CadPolyLineTemplate).
func (b *builder) buildPolyline(pl cadoc.Polyline, pt *polylineTmpl) {
	base := pl.Polyline()
	setSeqend := func(s *cadoc.Seqend) {
		base.Seqend = s
		s.Owner = pl
	}
	if s, ok := b.object(pt.seqend).(*cadoc.Seqend); ok {
		setSeqend(s)
	}
	add := func(v cadoc.Entity) {
		if v.Base().Owner != nil {
			b.notify(NotifyError, "Item %s already has an owner", v.ObjectName())
			return
		}
		ok := false
		switch p := pl.(type) {
		case *cadoc.Polyline2D:
			var x *cadoc.Vertex2D
			if x, ok = v.(*cadoc.Vertex2D); ok {
				p.Vertices = append(p.Vertices, x)
			}
		case *cadoc.Polyline3D:
			var x *cadoc.Vertex3D
			if x, ok = v.(*cadoc.Vertex3D); ok {
				p.Vertices = append(p.Vertices, x)
			}
		case *cadoc.PolygonMesh:
			var x *cadoc.PolygonMeshVertex
			if x, ok = v.(*cadoc.PolygonMeshVertex); ok {
				p.Vertices = append(p.Vertices, x)
			}
		case *cadoc.PolyfaceMesh:
			switch x := v.(type) {
			case *cadoc.VertexFaceMesh:
				p.Vertices, ok = append(p.Vertices, x), true
			case *cadoc.VertexFaceRecord:
				p.Faces, ok = append(p.Faces, x), true
			}
		}
		if !ok {
			b.notify(NotifyWarning, "Unidentified vertex type %s for polyline %d", v.ObjectName(), base.Handle)
			return
		}
		v.Base().Owner = pl
	}
	if pt.hasFirst {
		for _, v := range b.entitiesCollection(pt.first, pt.last) {
			if _, isVertex := v.(cadoc.Vertex); isVertex {
				add(v)
			}
		}
		return
	}
	for _, h := range pt.owned {
		e := b.entity(h)
		if e == nil {
			b.notify(NotifyWarning, "Vertex %d not found for polyline %d", h, base.Handle)
			continue
		}
		if s, ok := e.(*cadoc.Seqend); ok {
			setSeqend(s)
			continue
		}
		add(e)
	}
}

func (or *objectReader) readPolyline2D() *tmpl {
	pl := cadoc.NewPolyline2D()
	t := or.newEntityTmpl(pl)
	or.readCommonEntityData(t)
	pl.Flags = cadoc.PolylineFlags(or.obj.ReadBitShort())
	pl.SmoothSurface = cadoc.SmoothSurfaceType(or.obj.ReadBitShort())
	pl.StartWidth = or.obj.ReadBitDouble()
	pl.EndWidth = or.obj.ReadBitDouble()
	pl.Thickness = or.obj.ReadBitThickness()
	pl.Elevation = or.obj.ReadBitDouble()
	pl.Normal = or.obj.ReadBitExtrusion()
	pt := &polylineTmpl{}
	or.readPolylineHandles(pt, true)
	t.build = func(b *builder) { b.buildPolyline(pl, pt) }
	return t
}

func (or *objectReader) readPolyline3D() *tmpl {
	pl := cadoc.NewPolyline3D()
	t := or.newEntityTmpl(pl)
	or.readCommonEntityData(t)
	flags := or.obj.ReadRC()
	splined := flags&0b1 > 0
	splined1 := flags&0b10 > 0
	if splined {
		pl.SmoothSurface = cadoc.SmoothSurfaceTypeQuadratic
	} else if splined1 {
		pl.SmoothSurface = cadoc.SmoothSurfaceTypeCubic
	}
	if splined || splined1 {
		pl.Flags |= cadoc.PolylineFlagsSplineFit
	}
	pl.Flags |= cadoc.PolylineFlagsPolyline3D
	if or.obj.ReadRC()&1 > 0 {
		pl.Flags |= cadoc.PolylineFlagsClosedPolylineOrClosedPolygonMeshInM
	}
	pt := &polylineTmpl{}
	or.readPolylineHandles(pt, true)
	t.build = func(b *builder) { b.buildPolyline(pl, pt) }
	return t
}

func (or *objectReader) readPolyfaceMesh() *tmpl {
	pl := cadoc.NewPolyfaceMesh()
	t := or.newEntityTmpl(pl)
	or.readCommonEntityData(t)
	or.obj.ReadBitShort() // number of vertices
	or.obj.ReadBitShort() // number of faces
	pt := &polylineTmpl{}
	or.readPolylineHandles(pt, true)
	t.build = func(b *builder) { b.buildPolyline(pl, pt) }
	return t
}

func (or *objectReader) readPolylineMesh() *tmpl {
	pl := cadoc.NewPolygonMesh()
	t := or.newEntityTmpl(pl)
	or.readCommonEntityData(t)
	pl.Flags = cadoc.PolylineFlags(or.obj.ReadBitShort())
	pl.SmoothSurface = cadoc.SmoothSurfaceType(or.obj.ReadBitShort())
	pl.MVertexCount = or.obj.ReadBitShort()
	pl.NVertexCount = or.obj.ReadBitShort()
	pl.MSmoothSurfaceDensity = or.obj.ReadBitShort()
	pl.NSmoothSurfaceDensity = or.obj.ReadBitShort()
	pt := &polylineTmpl{}
	or.readPolylineHandles(pt, false)
	t.build = func(b *builder) { b.buildPolyline(pl, pt) }
	return t
}

func (or *objectReader) readVertex2D() *tmpl {
	v := cadoc.NewVertex2D()
	t := or.newEntityTmpl(v)
	or.readCommonEntityData(t)
	v.Flags = cadoc.VertexFlags(or.obj.ReadRC())
	v.Location = or.obj.Read3BitDouble()
	// A negative start width stands for equal start and end widths.
	w := or.obj.ReadBitDouble()
	if w < 0 {
		v.StartWidth, v.EndWidth = -w, -w
	} else {
		v.StartWidth = w
		v.EndWidth = or.obj.ReadBitDouble()
	}
	v.Bulge = or.obj.ReadBitDouble()
	if or.R2010Plus {
		v.Id = or.obj.ReadBitLong()
	}
	v.CurveTangent = or.obj.ReadBitDouble()
	return t
}

func (or *objectReader) readVertex3D(v cadoc.Vertex) *tmpl {
	t := or.newEntityTmpl(v)
	or.readCommonEntityData(t)
	vb := v.Vertex()
	vb.Flags = cadoc.VertexFlags(or.obj.ReadRC())
	vb.Location = or.obj.Read3BitDouble()
	return t
}

func (or *objectReader) readPfaceVertex() *tmpl {
	f := cadoc.NewVertexFaceRecord()
	t := or.newEntityTmpl(f)
	or.readCommonEntityData(t)
	f.Index1 = or.obj.ReadBitShort()
	f.Index2 = or.obj.ReadBitShort()
	f.Index3 = or.obj.ReadBitShort()
	f.Index4 = or.obj.ReadBitShort()
	return t
}

func (or *objectReader) readLWPolyline() (t *tmpl) {
	pl := cadoc.NewLwPolyline()
	t = or.newEntityTmpl(pl)
	defer func() {
		// ACadSharp keeps what was read before an error.
		if p := recover(); p != nil {
			or.notify(NotifyError, "Exception while reading LwPolyline: %v", p)
		}
	}()
	or.readCommonEntityData(t)
	flags := uint16(or.obj.ReadBitShort())
	if flags&0x100 != 0 {
		pl.Flags |= cadoc.LwPolylineFlagsPlinegen
	}
	if flags&0x200 != 0 {
		pl.Flags |= cadoc.LwPolylineFlagsClosed
	}
	if flags&0x4 != 0 {
		pl.ConstantWidth = or.obj.ReadBitDouble()
	}
	if flags&0x8 != 0 {
		pl.Elevation = or.obj.ReadBitDouble()
	}
	if flags&0x2 != 0 {
		pl.Thickness = or.obj.ReadBitDouble()
	}
	if flags&0x1 != 0 {
		pl.Normal = or.obj.Read3BitDouble()
	}
	nVertices := int(or.obj.ReadBitLong())
	nBulges, nIDs, nWidths := 0, 0, 0
	if flags&0x10 != 0 {
		nBulges = int(or.obj.ReadBitLong())
	}
	if flags&0x400 != 0 {
		nIDs = int(or.obj.ReadBitLong())
	}
	if flags&0x20 != 0 {
		nWidths = int(or.obj.ReadBitLong())
	}
	if or.R13_14Only {
		for i := 0; i < nVertices; i++ {
			pl.Vertices = append(pl.Vertices, cadoc.LwPolylineVertex{Location: or.obj.Read2RawDouble()})
		}
	}
	if or.R2000Plus && nVertices > 0 {
		loc := or.obj.Read2RawDouble()
		pl.Vertices = append(pl.Vertices, cadoc.LwPolylineVertex{Location: loc})
		for j := 1; j < nVertices; j++ {
			loc = or.obj.Read2BitDoubleWithDefault(loc)
			pl.Vertices = append(pl.Vertices, cadoc.LwPolylineVertex{Location: loc})
		}
	}
	for k := 0; k < nBulges; k++ {
		v := or.obj.ReadBitDouble()
		pl.Vertices[k].Bulge = v
	}
	for l := 0; l < nIDs; l++ {
		v := or.obj.ReadBitLong()
		pl.Vertices[l].Id = v
	}
	for m := 0; m < nWidths; m++ {
		s := or.obj.ReadBitDouble()
		e := or.obj.ReadBitDouble()
		pl.Vertices[m].StartWidth = s
		pl.Vertices[m].EndWidth = e
	}
	return t
}

// ----- simple entities -----

func (or *objectReader) readLine() *tmpl {
	l := cadoc.NewLine()
	t := or.newEntityTmpl(l)
	or.readCommonEntityData(t)
	if or.R13_14Only {
		l.StartPoint = or.obj.Read3BitDouble()
		l.EndPoint = or.obj.Read3BitDouble()
	}
	if or.R2000Plus {
		zZero := or.obj.ReadBit()
		sx := or.obj.ReadDouble()
		ex := or.obj.ReadBitDoubleWithDefault(sx)
		sy := or.obj.ReadDouble()
		ey := or.obj.ReadBitDoubleWithDefault(sy)
		var sz, ez float64
		if !zZero {
			sz = or.obj.ReadDouble()
			ez = or.obj.ReadBitDoubleWithDefault(sz)
		}
		l.StartPoint = cadoc.Vec3{X: sx, Y: sy, Z: sz}
		l.EndPoint = cadoc.Vec3{X: ex, Y: ey, Z: ez}
	}
	l.Thickness = or.obj.ReadBitThickness()
	l.Normal = or.obj.ReadBitExtrusion()
	return t
}

func (or *objectReader) readCircle() *tmpl {
	c := cadoc.NewCircle()
	t := or.newEntityTmpl(c)
	or.readCommonEntityData(t)
	c.Center = or.obj.Read3BitDouble()
	c.Radius = or.obj.ReadBitDouble()
	if c.Radius <= 0 {
		c.Radius = minRadius
	}
	c.Thickness = or.obj.ReadBitThickness()
	c.Normal = or.obj.ReadBitExtrusion()
	return t
}

func (or *objectReader) readArc() *tmpl {
	a := cadoc.NewArc()
	t := or.newEntityTmpl(a)
	or.readCommonEntityData(t)
	a.Center = or.obj.Read3BitDouble()
	a.Radius = or.obj.ReadBitDouble()
	if a.Radius <= 0 {
		a.Radius = minRadius
	}
	a.Thickness = or.obj.ReadBitThickness()
	a.Normal = or.obj.ReadBitExtrusion()
	a.StartAngle = or.obj.ReadBitDouble()
	a.EndAngle = or.obj.ReadBitDouble()
	return t
}

func (or *objectReader) readPoint() *tmpl {
	p := cadoc.NewPoint()
	t := or.newEntityTmpl(p)
	or.readCommonEntityData(t)
	p.Location = or.obj.Read3BitDouble()
	p.Thickness = or.obj.ReadBitThickness()
	p.Normal = or.obj.ReadBitExtrusion()
	p.Rotation = or.obj.ReadBitDouble()
	return t
}

func (or *objectReader) read3dFace() *tmpl {
	f := cadoc.NewFace3D()
	t := or.newEntityTmpl(f)
	or.readCommonEntityData(t)
	if or.R13_14Only {
		f.FirstCorner = or.obj.Read3BitDouble()
		f.SecondCorner = or.obj.Read3BitDouble()
		f.ThirdCorner = or.obj.Read3BitDouble()
		f.FourthCorner = or.obj.Read3BitDouble()
		f.Flags = cadoc.InvisibleEdgeFlags(or.obj.ReadBitShort())
	}
	if or.R2000Plus {
		noFlags := or.obj.ReadBit()
		zZero := or.obj.ReadBit()
		x := or.obj.ReadDouble()
		y := or.obj.ReadDouble()
		z := 0.0
		if !zZero {
			z = or.obj.ReadDouble()
		}
		f.FirstCorner = cadoc.Vec3{X: x, Y: y, Z: z}
		f.SecondCorner = or.obj.Read3BitDoubleWithDefault(f.FirstCorner)
		f.ThirdCorner = or.obj.Read3BitDoubleWithDefault(f.SecondCorner)
		f.FourthCorner = or.obj.Read3BitDoubleWithDefault(f.ThirdCorner)
		if !noFlags {
			f.Flags = cadoc.InvisibleEdgeFlags(or.obj.ReadBitShort())
		}
	}
	return t
}

func (or *objectReader) readSolid(trace bool) *tmpl {
	s := cadoc.NewSolid()
	s.IsTrace = trace
	t := or.newEntityTmpl(s)
	or.readCommonEntityData(t)
	s.Thickness = or.obj.ReadBitThickness()
	elevation := or.obj.ReadBitDouble()
	corner := func() cadoc.Vec3 {
		p := or.obj.Read2RawDouble()
		return cadoc.Vec3{X: p.X, Y: p.Y, Z: elevation}
	}
	s.FirstCorner = corner()
	s.SecondCorner = corner()
	s.ThirdCorner = corner()
	s.FourthCorner = corner()
	s.Normal = or.obj.ReadBitExtrusion()
	return t
}

func (or *objectReader) readShape() *tmpl {
	s := cadoc.NewShape()
	t := or.newEntityTmpl(s)
	or.readCommonEntityData(t)
	s.InsertionPoint = or.obj.Read3BitDouble()
	s.Size = or.obj.ReadBitDouble()
	s.Rotation = or.obj.ReadBitDouble()
	s.RelativeXScale = or.obj.ReadBitDouble()
	s.ObliqueAngle = or.obj.ReadBitDouble()
	s.Thickness = or.obj.ReadBitDouble()
	s.ShapeIndex = uint16(or.obj.ReadBitShort())
	s.Normal = or.obj.Read3BitDouble()
	file := or.handleRef()
	t.build = func(b *builder) {
		if ts, ok := tableRef(b, t, b.textStyles, file, ""); ok {
			if ts.IsShapeFile() {
				s.ShapeStyle = ts
			} else {
				b.notify(NotifyWarning, "Shape style %d |  not found", file)
			}
		}
	}
	return t
}

func (or *objectReader) readEllipse() *tmpl {
	e := cadoc.NewEllipse()
	t := or.newEntityTmpl(e)
	or.readCommonEntityData(t)
	e.Center = or.obj.Read3BitDouble()
	e.MajorAxisEndPoint = or.obj.Read3BitDouble()
	e.Normal = or.obj.Read3BitDouble()
	e.RadiusRatio = or.obj.ReadBitDouble()
	e.StartParameter = or.obj.ReadBitDouble()
	e.EndParameter = or.obj.ReadBitDouble()
	return t
}

func (or *objectReader) readRay() *tmpl {
	r := cadoc.NewRay()
	t := or.newEntityTmpl(r)
	or.readCommonEntityData(t)
	r.StartPoint = or.obj.Read3BitDouble()
	r.Direction = or.obj.Read3BitDouble()
	return t
}

func (or *objectReader) readXLine() *tmpl {
	x := cadoc.NewXLine()
	t := or.newEntityTmpl(x)
	or.readCommonEntityData(t)
	x.FirstPoint = or.obj.Read3BitDouble()
	x.Direction = or.obj.Read3BitDouble()
	return t
}

func (or *objectReader) readTolerance() *tmpl {
	tol := cadoc.NewTolerance()
	t := or.newEntityTmpl(tol)
	or.readCommonEntityData(t)
	if or.R13_14Only {
		or.obj.ReadBitShort()
		or.obj.ReadBitDouble() // height
		or.obj.ReadBitDouble() // dimgap
	}
	tol.InsertionPoint = or.obj.Read3BitDouble()
	tol.Direction = or.obj.Read3BitDouble()
	tol.Normal = or.obj.Read3BitDouble()
	tol.Text = or.text.ReadVariableText()
	style := or.handleRef()
	t.build = func(b *builder) {
		if s, ok := tableRef(b, t, b.dimensionStyles, style, ""); ok {
			tol.Style = s
		}
	}
	return t
}

func (or *objectReader) readSpline() *tmpl {
	s := cadoc.NewSpline()
	t := or.newEntityTmpl(s)
	or.readCommonEntityData(t)
	scenario := or.obj.ReadBitLong()
	setClosed := func(v bool) {
		if v {
			s.Flags |= cadoc.SplineFlagsClosed
			s.Flags1 |= cadoc.SplineFlags1Closed
		} else {
			s.Flags &^= cadoc.SplineFlagsClosed
			s.Flags1 &^= cadoc.SplineFlags1Closed
		}
	}
	if or.R2013Plus {
		s.Flags1 = cadoc.SplineFlags1(or.merged.ReadBitLong())
		setClosed(s.Flags1&cadoc.SplineFlags1Closed != 0)
		s.KnotParametrization = cadoc.KnotParametrization(or.merged.ReadBitLong())
		// ACadSharp derives the scenario from the UseKnotParameter bit,
		// which loses the control points of splines that have both; the
		// method-fit-points bit decides (as in the ODA documentation).
		if s.Flags1&cadoc.SplineFlags1MethodFitPoints != 0 {
			scenario = 2
		}
		if s.KnotParametrization == cadoc.KnotParametrizationCustom {
			scenario = 1
		}
	} else if scenario == 2 {
		s.Flags1 |= cadoc.SplineFlags1MethodFitPoints
	} else {
		s.KnotParametrization = cadoc.KnotParametrizationCustom
	}
	s.Degree = or.obj.ReadBitLong()
	nFit, nKnots, nCtrl := 0, 0, 0
	weights := false
	switch scenario {
	case 1:
		if or.obj.ReadBit() {
			s.Flags |= cadoc.SplineFlagsRational
		}
		if or.obj.ReadBit() {
			s.Flags |= cadoc.SplineFlagsClosed
		}
		if or.obj.ReadBit() {
			s.Flags |= cadoc.SplineFlagsPeriodic
		}
		s.KnotTolerance = or.obj.ReadBitDouble()
		s.ControlPointTolerance = or.obj.ReadBitDouble()
		nKnots = int(or.obj.ReadBitLong())
		nCtrl = int(or.obj.ReadBitLong())
		weights = or.obj.ReadBit()
	case 2:
		s.FitTolerance = or.obj.ReadBitDouble()
		s.StartTangent = or.obj.Read3BitDouble()
		s.EndTangent = or.obj.Read3BitDouble()
		nFit = int(or.obj.ReadBitLong())
	}
	for i := 0; i < nKnots; i++ {
		s.Knots = append(s.Knots, or.obj.ReadBitDouble())
	}
	for j := 0; j < nCtrl; j++ {
		s.ControlPoints = append(s.ControlPoints, or.obj.Read3BitDouble())
		if weights {
			s.Weights = append(s.Weights, or.obj.ReadBitDouble())
		}
	}
	for k := 0; k < nFit; k++ {
		s.FitPoints = append(s.FitPoints, or.obj.Read3BitDouble())
	}
	return t
}

func (or *objectReader) readMLine() *tmpl {
	m := cadoc.NewMLine()
	t := or.newEntityTmpl(m)
	or.readCommonEntityData(t)
	m.ScaleFactor = or.obj.ReadBitDouble()
	m.Justification = cadoc.MLineJustification(or.obj.ReadRC())
	m.StartPoint = or.obj.Read3BitDouble()
	m.Normal = or.obj.Read3BitDouble()
	if or.obj.ReadBitShort() == 3 {
		m.Flags |= cadoc.MLineFlagsClosed
	} else {
		m.Flags |= cadoc.MLineFlagsHasVertices
	}
	nLines := int(or.obj.ReadRC())
	nVerts := int(or.obj.ReadBitShort())
	for i := 0; i < nVerts; i++ {
		v := cadoc.MLineVertex{}
		v.Position = or.obj.Read3BitDouble()
		v.Direction = or.obj.Read3BitDouble()
		v.Miter = or.obj.Read3BitDouble()
		for j := 0; j < nLines; j++ {
			var seg cadoc.MLineSegment
			n := int(or.obj.ReadBitShort())
			for k := 0; k < n; k++ {
				seg.Parameters = append(seg.Parameters, or.obj.ReadBitDouble())
			}
			n = int(or.obj.ReadBitShort())
			for k := 0; k < n; k++ {
				seg.AreaFillParameters = append(seg.AreaFillParameters, or.obj.ReadBitDouble())
			}
			v.Segments = append(v.Segments, seg)
		}
		m.Vertices = append(m.Vertices, v)
	}
	style := or.handleRef()
	t.build = func(b *builder) {
		if s, ok := b.object(style).(*cadoc.MLineStyle); ok {
			m.Style = s
		}
	}
	return t
}

func (or *objectReader) readMesh() *tmpl {
	m := cadoc.NewMesh()
	t := or.newEntityTmpl(m)
	or.readCommonEntityData(t)
	m.Version = or.obj.ReadBitShort()
	m.BlendCrease = or.obj.ReadBit()
	m.SubdivisionLevel = or.obj.ReadBitLong()
	nv := int(or.obj.ReadBitLong())
	for i := 0; i < nv; i++ {
		m.Vertices = append(m.Vertices, or.obj.Read3BitDouble())
	}
	// The face count is the total number of values in the face list.
	nf := int(or.obj.ReadBitLong())
	for i := 0; i < nf; i++ {
		size := or.obj.count(or.obj.ReadBitLong(), 2)
		face := make([]int32, size)
		for j := range face {
			face[j] = or.obj.ReadBitLong()
		}
		i += size
		m.Faces = append(m.Faces, face)
	}
	ne := int(or.obj.ReadBitLong())
	for k := 0; k < ne; k++ {
		start := or.obj.ReadBitLong()
		end := or.obj.ReadBitLong()
		m.Edges = append(m.Edges, cadoc.MeshEdge{Start: start, End: end})
	}
	nc := int(or.obj.ReadBitLong())
	for l := 0; l < nc; l++ {
		c := or.obj.ReadBitDouble()
		m.Edges[l].Crease = &c
	}
	or.obj.ReadBitLong()
	return t
}

// ----- viewport -----

func (or *objectReader) readViewport() *tmpl {
	vp := cadoc.NewViewport()
	t := or.newEntityTmpl(vp)
	or.readCommonEntityData(t)
	vp.Center = or.obj.Read3BitDouble()
	vp.Width = or.obj.ReadBitDouble()
	vp.Height = or.obj.ReadBitDouble()
	if or.R2000Plus {
		vp.ViewTarget = or.obj.Read3BitDouble()
		vp.ViewDirection = or.obj.Read3BitDouble()
		vp.TwistAngle = or.obj.ReadBitDouble()
		vp.ViewHeight = or.obj.ReadBitDouble()
		vp.LensLength = or.obj.ReadBitDouble()
		vp.FrontClipPlane = or.obj.ReadBitDouble()
		vp.BackClipPlane = or.obj.ReadBitDouble()
		vp.SnapAngle = or.obj.ReadBitDouble()
		vp.ViewCenter = or.obj.Read2RawDouble()
		vp.SnapBase = or.obj.Read2RawDouble()
		vp.SnapSpacing = or.obj.Read2RawDouble()
		vp.GridSpacing = or.obj.Read2RawDouble()
		vp.CircleZoomPercent = or.obj.ReadBitShort()
	}
	if or.R2007Plus {
		vp.MajorGridLineFrequency = or.obj.ReadBitShort()
	}
	frozen := 0
	if or.R2000Plus {
		frozen = int(or.obj.ReadBitLong())
		vp.Status = cadoc.ViewportStatusFlags(or.obj.ReadBitLong())
		vp.StyleSheetName = or.text.ReadVariableText()
		vp.RenderMode = cadoc.RenderMode(or.obj.ReadRC())
		vp.DisplayUcsIcon = or.obj.ReadBit()
		vp.UcsPerViewport = or.obj.ReadBit()
		vp.UcsOrigin = or.obj.Read3BitDouble()
		vp.UcsXAxis = or.obj.Read3BitDouble()
		vp.UcsYAxis = or.obj.Read3BitDouble()
		vp.Elevation = or.obj.ReadBitDouble()
		vp.UcsOrthographicType = cadoc.OrthographicType(or.obj.ReadBitShort())
	}
	if or.R2004Plus {
		vp.ShadePlotMode = cadoc.ShadePlotMode(or.obj.ReadBitShort())
	}
	if or.R2007Plus {
		vp.UseDefaultLighting = or.obj.ReadBit()
		vp.DefaultLightingType = cadoc.LightingType(or.obj.ReadRC())
		vp.Brightness = or.obj.ReadBitDouble()
		vp.Contrast = or.obj.ReadBitDouble()
		vp.AmbientLightColor = or.obj.ReadCmColor()
	}
	if or.R13_14Only {
		or.handleRef() // viewport entity header
	}
	var frozenLayers []uint64
	var boundary uint64
	if or.R2000Plus {
		for i := 0; i < frozen; i++ {
			frozenLayers = append(frozenLayers, or.handleRef())
		}
		boundary = or.handleRef()
	}
	if or.ver == cadoc.AC1015 {
		or.handleRef() // viewport entity header
	}
	var namedUcs, baseUcs, visualStyle uint64
	if or.R2000Plus {
		namedUcs = or.handleRef()
		baseUcs = or.handleRef()
	}
	if or.R2007Plus {
		or.handleRef() // background
		visualStyle = or.handleRef()
		or.handleRef() // shade plot
		or.handleRef() // sun
	}
	t.build = func(b *builder) {
		if e := b.entity(boundary); e != nil {
			vp.Boundary = e
		} else if boundary > 0 {
			b.notify(NotifyWarning, "Boundary %d not found for viewport %d", boundary, vp.Handle)
		}
		// ACadSharp does not resolve the UCS and visual style references.
		if u, ok := b.object(namedUcs).(*cadoc.UCS); ok {
			vp.NamedUcs = u
		}
		if u, ok := b.object(baseUcs).(*cadoc.UCS); ok {
			vp.BaseUcs = u
		}
		if vs := b.object(visualStyle); vs != nil {
			vp.VisualStyle = vs
		}
		seen := map[uint64]bool{}
		for _, h := range frozenLayers {
			if seen[h] {
				continue
			}
			seen[h] = true
			if l, ok := b.object(h).(*cadoc.Layer); ok {
				vp.FrozenLayers = append(vp.FrozenLayers, l)
			} else {
				b.notify(NotifyWarning, "Frozen layer %d not found for viewport %d", h, vp.Handle)
			}
		}
	}
	return t
}
