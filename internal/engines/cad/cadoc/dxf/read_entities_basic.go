package dxf

import (
	"math"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

func init() {
	registerEntity(cadoc.TokenEntityLine, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewLine()
		return e, func(*entityState) bool {
			s := r.s
			switch s.code {
			case 10, 20, 30:
				setVec3(&e.StartPoint, s.code, s.float())
			case 11, 21, 31:
				setVec3(&e.EndPoint, s.code, s.float())
			case 39:
				e.Thickness = s.float()
			case 210, 220, 230:
				setVec3(&e.Normal, s.code, s.float())
			default:
				return false
			}
			return true
		}
	})
	registerEntity(cadoc.TokenEntityCircle, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewCircle()
		return e, func(*entityState) bool { return r.parseCircle(e) }
	})
	registerEntity(cadoc.TokenEntityArc, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewArc()
		return e, func(*entityState) bool {
			s := r.s
			switch s.code {
			case 50:
				e.StartAngle = s.angle()
			case 51:
				e.EndAngle = s.angle()
			default:
				return r.parseCircle(&e.Circle)
			}
			return true
		}
	})
	registerEntity(cadoc.TokenEntityEllipse, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewEllipse()
		return e, func(*entityState) bool {
			s := r.s
			switch s.code {
			case 10, 20, 30:
				setVec3(&e.Center, s.code, s.float())
			case 11, 21, 31:
				setVec3(&e.MajorAxisEndPoint, s.code, s.float())
			case 210, 220, 230:
				setVec3(&e.Normal, s.code, s.float())
			case 39:
				e.Thickness = s.float()
			case 40:
				e.RadiusRatio = s.float()
			case 41:
				e.StartParameter = s.float()
			case 42:
				e.EndParameter = s.float()
			default:
				return false
			}
			return true
		}
	})
	registerEntity(cadoc.TokenEntityPoint, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewPoint()
		return e, func(*entityState) bool {
			s := r.s
			switch s.code {
			case 10, 20, 30:
				setVec3(&e.Location, s.code, s.float())
			case 39:
				e.Thickness = s.float()
			case 210, 220, 230:
				setVec3(&e.Normal, s.code, s.float())
			case 50:
				e.Rotation = s.angle()
			default:
				return false
			}
			return true
		}
	})
	registerEntity(cadoc.TokenEntityXline, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewXLine()
		return e, func(*entityState) bool {
			s := r.s
			switch s.code {
			case 10, 20, 30:
				setVec3(&e.FirstPoint, s.code, s.float())
			case 11, 21, 31:
				setVec3(&e.Direction, s.code, s.float())
			default:
				return false
			}
			return true
		}
	})
	registerEntity(cadoc.TokenEntityRay, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewRay()
		return e, func(*entityState) bool {
			s := r.s
			switch s.code {
			case 10, 20, 30:
				setVec3(&e.StartPoint, s.code, s.float())
			case 11, 21, 31:
				setVec3(&e.Direction, s.code, s.float())
			default:
				return false
			}
			return true
		}
	})
	solid := func(trace bool) func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		return func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
			e := cadoc.NewSolid()
			e.IsTrace = trace
			return e, func(*entityState) bool {
				s := r.s
				switch s.code {
				case 10, 20, 30:
					setVec3(&e.FirstCorner, s.code, s.float())
				case 11, 21, 31:
					setVec3(&e.SecondCorner, s.code, s.float())
				case 12, 22, 32:
					setVec3(&e.ThirdCorner, s.code, s.float())
				case 13, 23, 33:
					setVec3(&e.FourthCorner, s.code, s.float())
				case 39:
					e.Thickness = s.float()
				case 210, 220, 230:
					setVec3(&e.Normal, s.code, s.float())
				default:
					return false
				}
				return true
			}
		}
	}
	registerEntity(cadoc.TokenEntitySolid, solid(false))
	registerEntity(cadoc.TokenEntityTrace, solid(true))
	registerEntity(cadoc.TokenEntity3DFace, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewFace3D()
		return e, func(*entityState) bool {
			s := r.s
			switch s.code {
			case 10, 20, 30:
				setVec3(&e.FirstCorner, s.code, s.float())
			case 11, 21, 31:
				setVec3(&e.SecondCorner, s.code, s.float())
			case 12, 22, 32:
				setVec3(&e.ThirdCorner, s.code, s.float())
			case 13, 23, 33:
				setVec3(&e.FourthCorner, s.code, s.float())
			case 70:
				e.Flags = cadoc.InvisibleEdgeFlags(s.i16())
			default:
				return false
			}
			return true
		}
	})
	registerEntity(cadoc.TokenEntityShape, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewShape()
		return e, func(*entityState) bool {
			s := r.s
			switch s.code {
			case 2:
				e.ShapeName = s.str
			case 10, 20, 30:
				setVec3(&e.InsertionPoint, s.code, s.float())
			case 39:
				e.Thickness = s.float()
			case 40:
				e.Size = s.float()
			case 41:
				e.RelativeXScale = s.float()
			case 50:
				e.Rotation = s.angle()
			case 51:
				e.ObliqueAngle = s.angle()
			case 210, 220, 230:
				setVec3(&e.Normal, s.code, s.float())
			default:
				return false
			}
			return true
		}
	})
	registerEntity(cadoc.TokenEntitySeqend, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		return cadoc.NewSeqend(), nil
	})
	registerEntity(cadoc.TokenEntityTolerance, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewTolerance()
		return e, func(*entityState) bool {
			s := r.s
			switch s.code {
			case 3:
				name := s.str
				r.later(func() { e.Style, _ = r.doc.DimensionStyles.Get(name) })
			case 1:
				e.Text = s.str
			case 10, 20, 30:
				setVec3(&e.InsertionPoint, s.code, s.float())
			case 11, 21, 31:
				setVec3(&e.Direction, s.code, s.float())
			case 210, 220, 230:
				setVec3(&e.Normal, s.code, s.float())
			default:
				return false
			}
			return true
		}
	})
	registerEntity(cadoc.TokenEntityText, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewText()
		return e, func(*entityState) bool { return r.parseText(e) }
	})
	registerEntity(cadoc.TokenEntityAttribute, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewAttribute()
		return e, func(*entityState) bool { return r.parseAttribute(&e.AttributeBase, nil) }
	})
	registerEntity(cadoc.TokenEntityAttributeDefinition, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewAttributeDefinition()
		return e, func(*entityState) bool { return r.parseAttribute(&e.AttributeBase, e) }
	})
	registerEntity(cadoc.TokenEntityMText, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewMText()
		return e, func(*entityState) bool { return r.parseMText(e) }
	})
	registerEntity(cadoc.TokenEntityInsert, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewInsert(nil)
		st.afterRead = append(st.afterRead, func() { r.readAttributes(e) })
		return e, func(*entityState) bool { return r.parseInsert(e) }
	})
	registerEntity(cadoc.TokenEntityLwPolyline, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewLwPolyline()
		return e, func(*entityState) bool { return r.parseLwPolyline(e) }
	})
	registerEntity(cadoc.TokenEntityPolyline, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		p := &polylineData{}
		tmp := cadoc.NewPolyline2D()
		st.afterRead = append(st.afterRead, func() { r.finishPolyline(st, p) })
		return tmp, func(*entityState) bool { return r.parsePolyline(p) }
	})
	registerEntity(cadoc.TokenEntityVertex, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		st.afterRead = append(st.afterRead, func() {
			r.warn("VERTEX outside of a POLYLINE, discarded")
			st.obj = nil
		})
		return cadoc.NewVertex2D(), nil
	})
}

func (r *reader) parseCircle(e *cadoc.Circle) bool {
	s := r.s
	switch s.code {
	case 10, 20, 30:
		setVec3(&e.Center, s.code, s.float())
	case 39:
		e.Thickness = s.float()
	case 40:
		e.Radius = s.float()
		if e.Radius <= 0 {
			e.Radius = 1e-12
		}
	case 210, 220, 230:
		setVec3(&e.Normal, s.code, s.float())
	default:
		return false
	}
	return true
}

func (r *reader) styleRef(name string, set func(*cadoc.TextStyle)) {
	r.later(func() {
		if ts, ok := r.doc.TextStyles.Get(name); ok {
			set(ts)
		} else if name != "" {
			r.warn("Text style %s not found", name)
		}
	})
}

func (r *reader) parseText(e *cadoc.TextEntity) bool {
	s := r.s
	switch s.code {
	case 1:
		e.Value = s.str
	case 7:
		r.styleRef(s.str, func(ts *cadoc.TextStyle) { e.Style = ts })
	case 10, 20, 30:
		setVec3(&e.InsertPoint, s.code, s.float())
	case 11, 21, 31:
		setVec3(&e.AlignmentPoint, s.code, s.float())
	case 210, 220, 230:
		setVec3(&e.Normal, s.code, s.float())
	case 39:
		e.Thickness = s.float()
	case 40:
		e.Height = s.float()
	case 41:
		e.WidthFactor = s.float()
	case 50:
		e.Rotation = s.angle()
	case 51:
		e.ObliqueAngle = s.angle()
	case 71:
		e.Mirror = cadoc.TextMirrorFlag(s.i16())
	case 72:
		e.HorizontalAlignment = cadoc.TextHorizontalAlignment(s.i16())
	case 73:
		e.VerticalAlignment = cadoc.TextVerticalAlignmentType(s.i16())
	default:
		return false
	}
	return true
}

// parseAttribute reads ATTRIB and ATTDEF groups (def is nil for ATTRIB).
func (r *reader) parseAttribute(a *cadoc.AttributeBase, def *cadoc.AttributeDefinition) bool {
	s := r.s
	inAttr := r.subclass == cadoc.SubclassAttribute || r.subclass == cadoc.SubclassAttributeDefinition
	switch s.code {
	case 2:
		a.Tag = s.str
	case 3:
		if def != nil {
			def.Prompt = s.str
		}
	case 70:
		a.Flags = cadoc.AttributeFlags(s.i16())
	case 73:
		// Field length (unused).
	case 74:
		a.VerticalAlignment = cadoc.TextVerticalAlignmentType(s.i16())
	case 280:
		a.Version = uint8(s.int())
	case 44, 46:
	case 71:
		if inAttr {
			a.AttributeType = cadoc.AttributeType(s.i16())
		} else {
			a.Mirror = cadoc.TextMirrorFlag(s.i16())
		}
	case 72:
		if !inAttr {
			a.HorizontalAlignment = cadoc.TextHorizontalAlignment(s.i16())
		}
	case 101:
		m := cadoc.NewMText()
		a.MText = m
		s.next()
		for !r.at0() {
			r.parseMText(m)
			if r.hold {
				r.hold = false
				continue
			}
			s.next()
		}
		r.hold = true
	default:
		return r.parseText(&a.TextEntity)
	}
	return true
}

func (r *reader) parseMText(e *cadoc.MText) bool {
	s := r.s
	switch s.code {
	case 1, 3:
		e.Value += s.str
	case 7:
		r.styleRef(s.str, func(ts *cadoc.TextStyle) { e.Style = ts })
	case 10, 20, 30:
		setVec3(&e.InsertPoint, s.code, s.float())
	case 11, 21, 31:
		setVec3(&e.AlignmentPoint, s.code, s.float())
	case 210, 220, 230:
		setVec3(&e.Normal, s.code, s.float())
	case 40:
		e.Height = s.float()
	case 41:
		e.RectangleWidth = s.float()
	case 42:
		e.HorizontalWidth = s.float()
	case 43:
		e.VerticalHeight = s.float()
	case 44:
		e.LineSpacing = s.float()
	case 45:
		e.BackgroundScale = s.float()
	case 46:
		e.RectangleHeight = s.float()
	case 50:
		a := s.angle()
		e.AlignmentPoint = cadoc.Vec3{X: math.Cos(a), Y: math.Sin(a)}
	case 63:
		if !e.BackgroundColor.IsTrueColor() {
			e.BackgroundColor = cadoc.ColorIndex(s.i16())
		}
	case 421:
		e.BackgroundColor = cadoc.ColorFromTrueColor(s.i32())
	case 430:
	case 441:
		e.BackgroundTransparency = cadoc.TransparencyFromAlpha(s.i32())
	case 71:
		e.AttachmentPoint = cadoc.AttachmentPointType(s.i16())
	case 72:
		e.DrawingDirection = cadoc.DrawingDirectionType(s.i16())
	case 73:
		e.LineSpacingStyle = cadoc.LineSpacingStyleType(s.i16())
	case 90:
		e.BackgroundFillFlags = cadoc.BackgroundFillFlags(s.int())
	case 101:
		r.readColumnData(e)
	default:
		return false
	}
	return true
}

func (r *reader) readColumnData(e *cadoc.MText) {
	s := r.s
	s.next()
	for !r.at0() {
		switch s.code {
		case 71:
			e.ColumnData.ColumnType = cadoc.ColumnType(s.i16())
		case 72:
			e.ColumnData.ColumnCount = s.i32()
		case 44:
			e.ColumnData.Width = s.float()
		case 45:
			e.ColumnData.Gutter = s.float()
		case 46:
			e.ColumnData.Heights = append(e.ColumnData.Heights, s.float())
		case 73:
			e.ColumnData.AutoHeight = s.bool()
		case 74:
			e.ColumnData.FlowReversed = s.bool()
		}
		s.next()
	}
	r.hold = true
}

func (r *reader) parseInsert(e *cadoc.Insert) bool {
	s := r.s
	switch s.code {
	case 2:
		name := s.str
		r.later(func() {
			if b, ok := r.doc.BlockRecords.Get(name); ok {
				e.Block = b
			} else {
				r.warn("Block %s not found for INSERT %X", name, e.Handle)
			}
		})
	case 10, 20, 30:
		setVec3(&e.InsertPoint, s.code, s.float())
	case 41:
		e.XScale = s.float()
	case 42:
		e.YScale = s.float()
	case 43:
		e.ZScale = s.float()
	case 44:
		e.ColumnSpacing = s.float()
	case 45:
		e.RowSpacing = s.float()
	case 50:
		e.Rotation = s.angle()
	case 70:
		e.ColumnCount = s.u16()
	case 71:
		e.RowCount = s.u16()
	case 210, 220, 230:
		setVec3(&e.Normal, s.code, s.float())
	case 66:
	case 100:
		r.subclass = s.str
	default:
		return false
	}
	return true
}

// readAttributes reads the ATTRIB entities and the SEQEND that follow an
// INSERT (the reader is on the code 0 after the INSERT).
func (r *reader) readAttributes(e *cadoc.Insert) {
	s := r.s
	for s.code == 0 && s.str == cadoc.TokenEntityAttribute {
		var a cadoc.Entity
		r.guard("an attribute", func() { a, _ = r.readEntity() })
		if att, ok := a.(*cadoc.AttributeEntity); ok {
			e.AddAttribute(att)
		}
	}
	if s.code == 0 && s.str == cadoc.TokenEntitySeqend && len(e.Attributes) > 0 {
		var q cadoc.Entity
		r.guard("a SEQEND", func() { q, _ = r.readEntity() })
		if sq, ok := q.(*cadoc.Seqend); ok {
			sq.Owner = e
			e.AttributesSeqend = sq
		}
	}
}

func (r *reader) parseLwPolyline(e *cadoc.LwPolyline) bool {
	s := r.s
	last := func() *cadoc.LwPolylineVertex {
		if len(e.Vertices) == 0 {
			return nil
		}
		return &e.Vertices[len(e.Vertices)-1]
	}
	switch s.code {
	case 10:
		e.Vertices = append(e.Vertices, cadoc.LwPolylineVertex{Location: cadoc.Vec2{X: s.float()}})
	case 20:
		if v := last(); v != nil {
			v.Location.Y = s.float()
		}
	case 40:
		if v := last(); v != nil {
			v.StartWidth = s.float()
		}
	case 41:
		if v := last(); v != nil {
			v.EndWidth = s.float()
		}
	case 42:
		if v := last(); v != nil {
			v.Bulge = s.float()
		}
	case 50:
		if v := last(); v != nil {
			v.CurveTangent = s.float()
		}
	case 91:
		if v := last(); v != nil {
			v.Id = s.i32()
		}
	case 66, 90:
	case 70:
		e.Flags = cadoc.LwPolylineFlags(s.i16())
	case 43:
		e.ConstantWidth = s.float()
	case 38:
		e.Elevation = s.float()
	case 39:
		e.Thickness = s.float()
	case 210, 220, 230:
		setVec3(&e.Normal, s.code, s.float())
	default:
		return false
	}
	return true
}

// polylineData holds POLYLINE groups until the kind is known.
type polylineData struct {
	marker       string
	flags        cadoc.PolylineFlags
	elevation    float64
	thickness    float64
	normal       cadoc.Vec3
	normalSet    bool
	startWidth   float64
	endWidth     float64
	smooth       cadoc.SmoothSurfaceType
	m, n         int16
	mDens, nDens int16
}

func (r *reader) parsePolyline(p *polylineData) bool {
	s := r.s
	switch s.code {
	case 100:
		r.subclass = s.str
		switch s.str {
		case cadoc.SubclassPolyline, cadoc.SubclassPolyline3d, cadoc.SubclassPolyfaceMesh, cadoc.SubclassPolygonMesh:
			p.marker = s.str
		}
	case 10, 20, 66:
	case 30:
		p.elevation = s.float()
	case 39:
		p.thickness = s.float()
	case 40:
		p.startWidth = s.float()
	case 41:
		p.endWidth = s.float()
	case 70:
		p.flags = cadoc.PolylineFlags(s.i16())
	case 71:
		p.m = s.i16()
	case 72:
		p.n = s.i16()
	case 73:
		p.mDens = s.i16()
	case 74:
		p.nDens = s.i16()
	case 75:
		p.smooth = cadoc.SmoothSurfaceType(s.i16())
	case 210, 220, 230:
		setVec3(&p.normal, s.code, s.float())
		p.normalSet = true
	default:
		return false
	}
	return true
}

// finishPolyline builds the POLYLINE kind and reads its VERTEX and SEQEND
// children (the reader is on the code 0 after the POLYLINE groups).
func (r *reader) finishPolyline(st *entityState, p *polylineData) {
	tmp := st.obj.(*cadoc.Polyline2D)
	kind := p.marker
	if kind == "" {
		// R12 files have no subclass markers: the flags tell the kind.
		switch {
		case p.flags&cadoc.PolylineFlagsPolyfaceMesh != 0:
			kind = cadoc.SubclassPolyfaceMesh
		case p.flags&cadoc.PolylineFlagsPolygonMesh != 0:
			kind = cadoc.SubclassPolygonMesh
		case p.flags&cadoc.PolylineFlagsPolyline3D != 0:
			kind = cadoc.SubclassPolyline3d
		default:
			kind = cadoc.SubclassPolyline
		}
	}
	base := &tmp.PolylineBase
	base.Flags, base.Elevation, base.Thickness = p.flags, p.elevation, p.thickness
	base.StartWidth, base.EndWidth, base.SmoothSurface = p.startWidth, p.endWidth, p.smooth
	if p.normalSet {
		base.Normal = p.normal
	}
	var result cadoc.Polyline
	switch kind {
	case cadoc.SubclassPolyline3d:
		x := &cadoc.Polyline3D{PolylineBase: *base}
		result = x
	case cadoc.SubclassPolyfaceMesh:
		x := &cadoc.PolyfaceMesh{PolylineBase: *base}
		result = x
	case cadoc.SubclassPolygonMesh:
		x := &cadoc.PolygonMesh{PolylineBase: *base, MVertexCount: p.m, NVertexCount: p.n,
			MSmoothSurfaceDensity: p.mDens, NSmoothSurfaceDensity: p.nDens}
		result = x
	default:
		result = tmp
	}
	st.obj = result
	s := r.s
	for s.code == 0 && s.str == cadoc.TokenEntityVertex {
		var v cadoc.Entity
		r.guard("a vertex", func() { v = r.readVertex(result) })
		if v == nil {
			continue
		}
		v.Base().Owner = result
		switch x := result.(type) {
		case *cadoc.Polyline2D:
			if vv, ok := v.(*cadoc.Vertex2D); ok {
				x.Vertices = append(x.Vertices, vv)
			}
		case *cadoc.Polyline3D:
			if vv, ok := v.(*cadoc.Vertex3D); ok {
				x.Vertices = append(x.Vertices, vv)
			}
		case *cadoc.PolyfaceMesh:
			switch vv := v.(type) {
			case *cadoc.VertexFaceMesh:
				x.Vertices = append(x.Vertices, vv)
			case *cadoc.VertexFaceRecord:
				x.Faces = append(x.Faces, vv)
			}
		case *cadoc.PolygonMesh:
			if vv, ok := v.(*cadoc.PolygonMeshVertex); ok {
				x.Vertices = append(x.Vertices, vv)
			}
		}
	}
	if s.code == 0 && s.str == cadoc.TokenEntitySeqend {
		var q cadoc.Entity
		r.guard("a SEQEND", func() { q, _ = r.readEntity() })
		if sq, ok := q.(*cadoc.Seqend); ok {
			sq.Owner = result
			result.Polyline().Seqend = sq
		}
	}
}

// readVertex reads a VERTEX of a polyline; its kind follows the subclass
// marker, or the polyline kind and flags in R12 files.
func (r *reader) readVertex(owner cadoc.Polyline) cadoc.Entity {
	var d cadoc.VertexBase
	d.InitEntity()
	marker := ""
	idx := [4]int16{}
	st := &entityState{objectState: objectState{obj: &cadoc.Vertex2D{}}}
	tmp := st.obj.(*cadoc.Vertex2D)
	tmp.InitEntity()
	r.readEntityGroups(st, func(*entityState) bool {
		s := r.s
		switch s.code {
		case 100:
			r.subclass = s.str
			switch s.str {
			case cadoc.SubclassPolylineVertex, cadoc.SubclassPolyline3dVertex, cadoc.SubclassPolyfaceMeshVertex,
				cadoc.SubclassPolyfaceMeshFace, cadoc.SubclassPolygonMeshVertex:
				marker = s.str
			}
		case 10, 20, 30:
			setVec3(&tmp.Location, s.code, s.float())
		case 40:
			tmp.StartWidth = s.float()
		case 41:
			tmp.EndWidth = s.float()
		case 42:
			tmp.Bulge = s.float()
		case 50:
			tmp.CurveTangent = s.angle()
		case 70:
			tmp.Flags = cadoc.VertexFlags(s.i16())
		case 71, 72, 73, 74:
			idx[s.code-71] = s.i16()
		case 91:
			tmp.Id = s.i32()
		default:
			return false
		}
		return true
	})
	if marker == "" {
		switch owner.(type) {
		case *cadoc.Polyline3D:
			marker = cadoc.SubclassPolyline3dVertex
		case *cadoc.PolygonMesh:
			marker = cadoc.SubclassPolygonMeshVertex
		case *cadoc.PolyfaceMesh:
			if tmp.Flags&cadoc.VertexFlagsPolygonMesh3D == 0 && tmp.Flags&cadoc.VertexFlagsPolyFaceMeshVertex != 0 {
				marker = cadoc.SubclassPolyfaceMeshFace
			} else {
				marker = cadoc.SubclassPolyfaceMeshVertex
			}
		default:
			marker = cadoc.SubclassPolylineVertex
		}
	}
	var v cadoc.Entity
	switch marker {
	case cadoc.SubclassPolyline3dVertex:
		v = &cadoc.Vertex3D{VertexBase: tmp.VertexBase}
	case cadoc.SubclassPolyfaceMeshVertex:
		v = &cadoc.VertexFaceMesh{VertexBase: tmp.VertexBase}
	case cadoc.SubclassPolyfaceMeshFace:
		v = &cadoc.VertexFaceRecord{VertexBase: tmp.VertexBase, Index1: idx[0], Index2: idx[1], Index3: idx[2], Index4: idx[3]}
	case cadoc.SubclassPolygonMeshVertex:
		v = &cadoc.PolygonMeshVertex{VertexBase: tmp.VertexBase}
	default:
		v = tmp
	}
	st.obj = v
	r.finishEntity(st)
	return v
}
