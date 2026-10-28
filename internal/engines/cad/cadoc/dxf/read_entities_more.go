package dxf

import (
	"math"
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

func init() {
	registerEntity(cadoc.TokenEntitySpline, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewSpline()
		return e, func(*entityState) bool { return r.parseSpline(e) }
	})
	registerEntity(cadoc.TokenEntityHatch, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewHatch()
		h := &hatchState{}
		return e, func(*entityState) bool { return r.parseHatch(e, h) }
	})
	dim := func(arc bool) func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		return func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
			d := &dimData{arc: arc}
			tmp := cadoc.NewDimensionAligned()
			d.base = &tmp.DimensionBase
			st.afterRead = append(st.afterRead, func() { r.finishDimension(st, d) })
			return tmp, func(*entityState) bool { return r.parseDimension(d) }
		}
	}
	registerEntity(cadoc.TokenEntityDimension, dim(false))
	registerEntity(cadoc.TokenEntityArcDimension, dim(true))
	registerEntity(cadoc.TokenEntityLeader, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewLeader()
		e.ArrowHeadEnabled = false
		return e, func(*entityState) bool { return r.parseLeader(e) }
	})
	registerEntity(cadoc.TokenEntityMultiLeader, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewMultiLeader()
		return e, func(*entityState) bool { return r.parseMultiLeader(e) }
	})
	registerEntity(cadoc.TokenEntityMLine, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewMLine()
		m := &mlineState{}
		return e, func(*entityState) bool { return r.parseMLine(e, m) }
	})
	registerEntity(cadoc.TokenEntityMesh, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewMesh()
		return e, func(*entityState) bool { return r.parseMesh(e) }
	})
	registerEntity(cadoc.TokenEntityViewport, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewViewport()
		return e, func(*entityState) bool { return r.parseViewport(e) }
	})
	registerEntity(cadoc.TokenEntityImage, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewRasterImage(nil)
		return e, func(*entityState) bool { return r.parseWipeoutBase(&e.WipeoutBase) }
	})
	registerEntity(cadoc.TokenEntityWipeout, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewWipeout()
		return e, func(*entityState) bool { return r.parseWipeoutBase(&e.WipeoutBase) }
	})
	registerEntity(cadoc.TokenEntityOle2Frame, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewOle2Frame()
		return e, func(*entityState) bool { return r.parseOle2Frame(e) }
	})
	registerEntity(cadoc.TokenEntityPdfUnderlay, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewPdfUnderlay()
		return e, func(*entityState) bool { return r.parsePdfUnderlay(e) }
	})
	modeler := func(newE func() (cadoc.Entity, *cadoc.ModelerGeometry)) func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		return func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
			e, m := newE()
			a := &acisState{}
			st.afterRead = append(st.afterRead, func() { m.AcisText = a.text.String() })
			return e, func(*entityState) bool {
				if s3, ok := e.(*cadoc.Solid3D); ok && r.s.code == 350 {
					r.handleRef(r.s.handle(), func(o cadoc.Object) { s3.History = o })
					return true
				}
				return r.parseModeler(m, a)
			}
		}
	}
	registerEntity(cadoc.TokenEntityRegion, modeler(func() (cadoc.Entity, *cadoc.ModelerGeometry) {
		e := cadoc.NewRegion()
		return e, &e.ModelerGeometry
	}))
	registerEntity(cadoc.TokenEntity3DSolid, modeler(func() (cadoc.Entity, *cadoc.ModelerGeometry) {
		e := cadoc.NewSolid3D()
		return e, &e.ModelerGeometry
	}))
	registerEntity(cadoc.TokenEntityBody, modeler(func() (cadoc.Entity, *cadoc.ModelerGeometry) {
		e := cadoc.NewCadBody()
		return e, &e.ModelerGeometry
	}))
	for _, name := range []string{cadoc.TokenEntitySurface, cadoc.TokenEntityExtrudedSurface, cadoc.TokenEntityLoftedSurface,
		cadoc.TokenEntityNurbSurface, cadoc.TokenEntityPlaneSurface, cadoc.TokenEntityRevolvedSurface,
		cadoc.TokenEntitySweptSurface} {
		name := name
		registerEntity(name, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
			e := &cadoc.Surface{DxfName: name}
			e.ModelerFormatVersion = 1
			e.InitEntity()
			a := &acisState{}
			st.afterRead = append(st.afterRead, func() { e.AcisText = a.text.String() })
			return e, func(*entityState) bool { return r.parseSurface(e, a) }
		})
	}
	registerEntity(cadoc.TokenEntityProxyEntity, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewProxyEntity()
		return e, func(*entityState) bool { return r.parseProxyEntity(e) }
	})
	registerEntity(cadoc.TokenEntityTable, func(r *reader, st *entityState) (cadoc.Entity, parseFunc) {
		e := cadoc.NewTableEntity()
		st.afterRead = append(st.afterRead, func() { r.readAttributes(&e.Insert) })
		return e, func(*entityState) bool { return r.parseTableEntity(e) }
	})
}

// ----- SPLINE -----

func (r *reader) parseSpline(e *cadoc.Spline) bool {
	s := r.s
	last := func(p []cadoc.Vec3) *cadoc.Vec3 {
		if len(p) == 0 {
			return nil
		}
		return &p[len(p)-1]
	}
	switch s.code {
	case 10:
		e.ControlPoints = append(e.ControlPoints, cadoc.Vec3{X: s.float()})
	case 20, 30:
		if p := last(e.ControlPoints); p != nil {
			setVec3(p, s.code, s.float())
		}
	case 11:
		e.FitPoints = append(e.FitPoints, cadoc.Vec3{X: s.float()})
	case 21, 31:
		if p := last(e.FitPoints); p != nil {
			setVec3(p, s.code-1, s.float())
		}
	case 12, 22, 32:
		setVec3(&e.StartTangent, s.code, s.float())
	case 13, 23, 33:
		setVec3(&e.EndTangent, s.code, s.float())
	case 40:
		e.Knots = append(e.Knots, s.float())
	case 41:
		e.Weights = append(e.Weights, s.float())
	case 42:
		e.KnotTolerance = s.float()
	case 43:
		e.ControlPointTolerance = s.float()
	case 44:
		e.FitTolerance = s.float()
	case 70:
		e.Flags = cadoc.SplineFlags(uint16(s.int()))
	case 71:
		e.Degree = s.i32()
	case 72, 73, 74:
	case 210, 220, 230:
		setVec3(&e.Normal, s.code, s.float())
	default:
		return false
	}
	return true
}

// ----- HATCH -----

type hatchState struct {
	seedsStarted bool
}

func (r *reader) parseHatch(h *cadoc.Hatch, hs *hatchState) bool {
	s := r.s
	switch s.code {
	case 2:
		if h.Pattern == nil {
			h.Pattern = cadoc.NewHatchPattern("")
		}
		h.Pattern.Name = s.str
	case 10:
		if hs.seedsStarted {
			h.SeedPoints = append(h.SeedPoints, cadoc.Vec2{X: s.float()})
		}
	case 20:
		if hs.seedsStarted && len(h.SeedPoints) > 0 {
			h.SeedPoints[len(h.SeedPoints)-1].Y = s.float()
		}
	case 30:
		h.Elevation = s.float()
	case 210, 220, 230:
		setVec3(&h.Normal, s.code, s.float())
	case 41:
		h.PatternScale = s.float()
	case 47:
		h.PixelSize = s.float()
	case 52:
		h.PatternAngle = s.angle()
	case 53:
		// Belongs to pattern lines; handled by readPattern.
	case 70:
		h.IsSolid = s.bool()
	case 71:
		h.IsAssociative = s.bool()
	case 75:
		h.Style = cadoc.HatchStyleType(s.i16())
	case 76:
		h.PatternType = cadoc.HatchPatternType(s.i16())
	case 77:
		h.IsDouble = s.bool()
	case 78:
		if h.Pattern == nil {
			h.Pattern = cadoc.NewHatchPattern("")
		}
		r.readPattern(h.Pattern, int(s.int()))
		r.hold = true
	case 90:
	case 91:
		r.readLoops(h, int(s.int()))
		r.hold = true
	case 98:
		hs.seedsStarted = true
	case 450:
		h.GradientColor.Enabled = s.bool()
	case 451:
		h.GradientColor.Reserved = s.i32()
	case 452:
		h.GradientColor.IsSingleColorGradient = s.bool()
	case 453:
	case 460:
		h.GradientColor.Angle = s.float()
	case 461:
		h.GradientColor.Shift = s.float()
	case 462:
		h.GradientColor.ColorTint = s.float()
	case 463:
		h.GradientColor.Colors = append(h.GradientColor.Colors, cadoc.GradientColor{Value: s.float()})
	case 63:
		if n := len(h.GradientColor.Colors); n > 0 {
			h.GradientColor.Colors[n-1].Color = cadoc.ColorIndex(int16(uint16(s.int())))
		}
	case 421:
		if n := len(h.GradientColor.Colors); n > 0 {
			h.GradientColor.Colors[n-1].Color = cadoc.ColorFromTrueColor(s.i32() & 0xFFFFFF)
		}
	case 470:
		h.GradientColor.Name = s.str
	default:
		return false
	}
	return true
}

// readPattern reads the pattern line families after code 78; afterwards
// the reader is on the first group after them.
func (r *reader) readPattern(p *cadoc.HatchPattern, n int) {
	s := r.s
	s.next()
	for i := 0; i < n && !r.at0(); i++ {
		var line cadoc.HatchPatternLine
		seen := map[int]bool{}
	loop:
		for !r.at0() {
			if seen[s.code] {
				break
			}
			seen[s.code] = true
			switch s.code {
			case 53:
				line.Angle = s.angle()
			case 43:
				line.BasePoint.X = s.float()
			case 44:
				line.BasePoint.Y = s.float()
			case 45:
				line.Offset.X = s.float()
			case 46:
				line.Offset.Y = s.float()
			case 79:
				nd := int(s.int())
				for j := 0; j < nd; j++ {
					s.next()
					if s.code != 49 {
						r.hold = true
						break
					}
					line.DashLengths = append(line.DashLengths, s.float())
				}
				if r.hold {
					r.hold = false
					p.Lines = append(p.Lines, line)
					return
				}
			case 49:
				line.DashLengths = append(line.DashLengths, s.float())
			default:
				break loop
			}
			s.next()
		}
		p.Lines = append(p.Lines, line)
	}
}

// readLoops reads the boundary paths after code 91; afterwards the reader
// is on the first group after them.
func (r *reader) readLoops(h *cadoc.Hatch, n int) {
	s := r.s
	s.next()
	for i := 0; i < n; i++ {
		if s.code != 92 {
			r.warn("Boundary path should start with code 92 but was %d", s.code)
			break
		}
		if p := r.readLoop(); p != nil {
			h.Paths = append(h.Paths, p)
		}
	}
}

func (r *reader) readLoop() *cadoc.HatchBoundaryPath {
	s := r.s
	p := &cadoc.HatchBoundaryPath{Flags: cadoc.BoundaryPathFlags(s.int())}
	if p.Flags&cadoc.BoundaryPathFlagsPolyline != 0 {
		if pl := r.readPolylineBoundary(); pl != nil {
			p.Edges = append(p.Edges, pl)
		}
	} else {
		s.next()
		if s.code != 93 {
			r.warn("Edge Boundary path should start with code 93 but was %d", s.code)
			return nil
		}
		n := int(s.int())
		s.next()
		for i := 0; i < n; i++ {
			if e := r.readEdge(); e != nil {
				p.Edges = append(p.Edges, e)
			} else {
				break
			}
		}
	}
	for !r.at0() {
		switch s.code {
		case 97:
		case 330:
			h := s.handle()
			idx := len(p.Entities)
			p.Entities = append(p.Entities, nil)
			r.later(func() {
				if e, ok := r.object(h).(cadoc.Entity); ok {
					p.Entities[idx] = e
				}
			})
		default:
			r.later(func() {
				kept := p.Entities[:0]
				for _, e := range p.Entities {
					if e != nil {
						kept = append(kept, e)
					}
				}
				p.Entities = kept
			})
			return p
		}
		s.next()
	}
	return p
}

func (r *reader) readPolylineBoundary() *cadoc.HatchPolylineEdge {
	s := r.s
	pl := &cadoc.HatchPolylineEdge{}
	s.next()
	if s.code != 72 {
		r.warn("Polyline Boundary path should start with code 72 but was %d", s.code)
		return nil
	}
	hasBulge := false
	for !r.at0() {
		switch s.code {
		case 72:
			hasBulge = s.bool()
		case 73:
			pl.IsClosed = s.bool()
		case 93:
			n := int(s.int())
			s.next()
			for i := 0; i < n && !r.at0(); i++ {
				v := cadoc.Vec3{X: s.float()}
				s.next()
				v.Y = s.float()
				s.next()
				if hasBulge && s.code == 42 {
					v.Z = s.float()
					s.next()
				}
				pl.Vertices = append(pl.Vertices, v)
			}
			continue
		default:
			return pl
		}
		s.next()
	}
	return pl
}

// readEdge reads one edge starting at its code 72; afterwards the reader is
// on the first group after it.
func (r *reader) readEdge() cadoc.HatchEdge {
	s := r.s
	if s.code != 72 {
		r.warn("Edge Boundary path should define the type with code 72 but was %d", s.code)
		return nil
	}
	t := cadoc.HatchEdgeType(s.int())
	s.next()
	switch t {
	case cadoc.HatchEdgeTypeLine:
		e := &cadoc.HatchLineEdge{}
		for {
			switch s.code {
			case 10, 20:
				setVec2(&e.Start, s.code, s.float())
			case 11, 21:
				setVec2(&e.End, s.code-1, s.float())
			default:
				return e
			}
			s.next()
		}
	case cadoc.HatchEdgeTypeCircularArc:
		e := &cadoc.HatchArcEdge{}
		for {
			switch s.code {
			case 10, 20:
				setVec2(&e.Center, s.code, s.float())
			case 40:
				e.Radius = s.float()
			case 50:
				e.StartAngle = s.angle()
			case 51:
				e.EndAngle = s.angle()
			case 73:
				e.CounterClockWise = s.bool()
			default:
				return e
			}
			s.next()
		}
	case cadoc.HatchEdgeTypeEllipticArc:
		e := &cadoc.HatchEllipseEdge{}
		for {
			switch s.code {
			case 10, 20:
				setVec2(&e.Center, s.code, s.float())
			case 11, 21:
				setVec2(&e.MajorAxisEndPoint, s.code-1, s.float())
			case 40:
				e.RadiusRatio = s.float()
			case 50:
				e.StartAngle = s.angle()
			case 51:
				e.EndAngle = s.angle()
			case 73:
				e.CounterClockWise = s.bool()
			default:
				return e
			}
			s.next()
		}
	case cadoc.HatchEdgeTypeSpline:
		e := &cadoc.HatchSplineEdge{}
		fitSeen := false
		for {
			switch s.code {
			case 10:
				e.ControlPoints = append(e.ControlPoints, cadoc.Vec3{X: s.float(), Z: 1})
			case 20:
				if n := len(e.ControlPoints); n > 0 {
					e.ControlPoints[n-1].Y = s.float()
				}
			case 42:
				if n := len(e.ControlPoints); n > 0 {
					e.ControlPoints[n-1].Z = s.float()
				}
			case 11:
				e.FitPoints = append(e.FitPoints, cadoc.Vec2{X: s.float()})
			case 21:
				if n := len(e.FitPoints); n > 0 {
					e.FitPoints[n-1].Y = s.float()
				}
			case 12, 22:
				setVec2(&e.StartTangent, s.code-2, s.float())
			case 13, 23:
				setVec2(&e.EndTangent, s.code-3, s.float())
			case 94:
				e.Degree = s.i32()
			case 73:
				e.IsRational = s.bool()
			case 74:
				e.IsPeriodic = s.bool()
			case 95, 96:
			case 97:
				// The second 97 is the path's source object count.
				if fitSeen {
					return e
				}
				fitSeen = true
			case 40:
				e.Knots = append(e.Knots, s.float())
			default:
				return e
			}
			s.next()
		}
	}
	r.warn("Unknown hatch edge type %d", t)
	return nil
}

// ----- DIMENSION -----

type dimData struct {
	arc       bool
	base      *cadoc.DimensionBase
	kind      string
	rotation  float64
	hasRot    bool
	extRot    float64
	p13, p14  cadoc.Vec3
	p15, p16  cadoc.Vec3
	p17       cadoc.Vec3
	leaderLen float64
	arcStart  float64
	arcEnd    float64
	partial   bool
	hasLeader bool
}

func (r *reader) parseDimension(d *dimData) bool {
	s := r.s
	b := d.base
	inArc := r.subclass == cadoc.SubclassArcDimension
	switch s.code {
	case 100:
		r.subclass = s.str
		switch s.str {
		case cadoc.SubclassAlignedDimension, cadoc.SubclassLinearDimension, cadoc.SubclassRadialDimension,
			cadoc.SubclassDiametricDimension, cadoc.SubclassAngular2LineDimension, cadoc.SubclassAngular3PointDimension,
			cadoc.SubclassOrdinateDimension, cadoc.SubclassArcDimension:
			if !(d.kind == cadoc.SubclassLinearDimension && s.str == cadoc.SubclassAlignedDimension) {
				d.kind = s.str
			}
		}
	case 2:
		name := s.str
		r.later(func() {
			if blk, ok := r.doc.BlockRecords.Get(name); ok {
				b.Block = blk
			}
		})
	case 3:
		name := s.str
		r.later(func() { b.Style, _ = r.doc.DimensionStyles.Get(name) })
	case 1:
		b.Text = s.str
	case 10, 20, 30:
		setVec3(&b.DefinitionPoint, s.code, s.float())
	case 11, 21, 31:
		setVec3(&b.TextMiddlePoint, s.code, s.float())
	case 12, 22, 32:
		setVec3(&b.InsertionPoint, s.code, s.float())
	case 13, 23, 33:
		setVec3(&d.p13, s.code, s.float())
	case 14, 24, 34:
		setVec3(&d.p14, s.code, s.float())
	case 15, 25, 35:
		setVec3(&d.p15, s.code, s.float())
	case 16, 26, 36:
		setVec3(&d.p16, s.code, s.float())
	case 17, 27, 37:
		setVec3(&d.p17, s.code, s.float())
	case 40:
		if inArc {
			d.arcStart = s.float()
		} else {
			d.leaderLen = s.float()
		}
	case 41:
		if inArc {
			d.arcEnd = s.float()
		} else {
			b.LineSpacingFactor = s.float()
		}
	case 42:
		b.MeasurementValue = s.float()
	case 50:
		d.rotation, d.hasRot = s.angle(), true
	case 51:
		b.HorizontalDirection = s.angle()
	case 52:
		d.extRot = s.angle()
	case 53:
		b.TextRotation = s.angle()
	case 70:
		if inArc {
			d.partial = s.bool()
		} else {
			b.Flags = cadoc.DimensionType(s.i16())
		}
	case 71:
		if inArc {
			d.hasLeader = s.bool()
		} else {
			b.AttachmentPoint = cadoc.AttachmentPointType(s.i16())
		}
	case 72:
		b.LineSpacingStyle = cadoc.LineSpacingStyleType(s.i16())
	case 74:
		b.FlipArrow1 = s.bool()
	case 75:
		b.FlipArrow2 = s.bool()
	case 280:
		b.Version = uint8(s.int())
	case 210, 220, 230:
		setVec3(&b.Normal, s.code, s.float())
	case 73, 90, 361:
	default:
		return false
	}
	return true
}

func (r *reader) finishDimension(st *entityState, d *dimData) {
	kind := d.kind
	if d.arc {
		kind = cadoc.SubclassArcDimension
	}
	if kind == cadoc.SubclassAlignedDimension && d.hasRot {
		kind = cadoc.SubclassLinearDimension
	}
	if kind == "" {
		switch d.base.Kind() {
		case cadoc.DimensionTypeLinear:
			kind = cadoc.SubclassLinearDimension
		case cadoc.DimensionTypeAligned:
			kind = cadoc.SubclassAlignedDimension
		case cadoc.DimensionTypeAngular:
			kind = cadoc.SubclassAngular2LineDimension
		case cadoc.DimensionTypeDiameter:
			kind = cadoc.SubclassDiametricDimension
		case cadoc.DimensionTypeRadius:
			kind = cadoc.SubclassRadialDimension
		case cadoc.DimensionTypeAngular3Point:
			kind = cadoc.SubclassAngular3PointDimension
		case cadoc.DimensionTypeOrdinate:
			kind = cadoc.SubclassOrdinateDimension
		}
	}
	b := *d.base
	var e cadoc.Entity
	switch kind {
	case cadoc.SubclassLinearDimension:
		x := &cadoc.DimensionLinear{}
		x.DimensionBase = b
		x.FirstPoint, x.SecondPoint, x.ExtLineRotation, x.Rotation = d.p13, d.p14, d.extRot, d.rotation
		e = x
	case cadoc.SubclassAlignedDimension:
		x := &cadoc.DimensionAligned{DimensionBase: b, FirstPoint: d.p13, SecondPoint: d.p14, ExtLineRotation: d.extRot}
		e = x
	case cadoc.SubclassRadialDimension:
		e = &cadoc.DimensionRadius{DimensionBase: b, AngleVertex: d.p15, LeaderLength: d.leaderLen}
	case cadoc.SubclassDiametricDimension:
		e = &cadoc.DimensionDiameter{DimensionBase: b, AngleVertex: d.p15, LeaderLength: d.leaderLen}
	case cadoc.SubclassAngular2LineDimension:
		e = &cadoc.DimensionAngular2Line{DimensionBase: b, FirstPoint: d.p13, SecondPoint: d.p14, AngleVertex: d.p15, DimensionArc: d.p16}
	case cadoc.SubclassAngular3PointDimension:
		e = &cadoc.DimensionAngular3Pt{DimensionBase: b, FirstPoint: d.p13, SecondPoint: d.p14, AngleVertex: d.p15}
	case cadoc.SubclassOrdinateDimension:
		e = &cadoc.DimensionOrdinate{DimensionBase: b, FeatureLocation: d.p13, LeaderEndpoint: d.p14}
	case cadoc.SubclassArcDimension:
		e = &cadoc.DimensionArc{DimensionBase: b, FirstPoint: d.p13, SecondPoint: d.p14, Center: d.p15, IsPartial: d.partial,
			StartAngle: d.arcStart, EndAngle: d.arcEnd, HasLeader: d.hasLeader, LeaderPoint1: d.p16, LeaderPoint2: d.p17}
	default:
		r.warn("[DIMENSION] No subtype found, object discarded.")
		st.obj = nil
		return
	}
	// References queued against the temporary base must land on the result.
	nb := e.(cadoc.Dimension).Dimension()
	old := d.base
	r.later(func() { nb.Block, nb.Style = old.Block, old.Style })
	st.obj = e
}

// ----- LEADER -----

func (r *reader) parseLeader(e *cadoc.Leader) bool {
	s := r.s
	switch s.code {
	case 3:
		name := s.str
		r.later(func() { e.Style, _ = r.doc.DimensionStyles.Get(name) })
	case 10:
		e.Vertices = append(e.Vertices, cadoc.Vec3{X: s.float()})
	case 20, 30:
		if n := len(e.Vertices); n > 0 {
			setVec3(&e.Vertices[n-1], s.code, s.float())
		}
	case 340:
		r.handleRef(s.handle(), func(o cadoc.Object) { e.AssociatedAnnotation, _ = o.(cadoc.Entity) })
	case 40:
		e.TextHeight = s.float()
	case 41:
		e.TextWidth = s.float()
	case 71:
		e.ArrowHeadEnabled = s.bool()
	case 72:
		e.PathType = cadoc.LeaderPathType(s.i16())
	case 73:
		e.CreationType = cadoc.LeaderCreationType(s.i16())
	case 74:
		e.HookLineDirection = cadoc.HookLineDirection(s.i16())
	case 75:
		e.HasHookline = s.bool()
	case 76, 77:
	case 210, 220, 230:
		setVec3(&e.Normal, s.code, s.float())
	case 211, 221, 231:
		setVec3(&e.HorizontalDirection, s.code-200, s.float())
	case 212, 222, 232:
		setVec3(&e.BlockOffset, s.code-200, s.float())
	case 213, 223, 233:
		setVec3(&e.AnnotationOffset, s.code-200, s.float())
	default:
		return false
	}
	return true
}

// ----- MULTILEADER -----

// cmColor decodes an AcCmColor stored in a 32-bit group: 0xC0 ByLayer,
// 0xC1 ByBlock (ACadSharp also writes indexes there), 0xC2 RGB, 0xC3 index.
func cmColor(v int32) cadoc.Color {
	u := uint32(v)
	switch u >> 24 {
	case 0xC0:
		return cadoc.ColorByLayer
	case 0xC1:
		if i := u & 0xFF; i != 0 {
			return cadoc.ColorIndex(int16(i))
		}
		return cadoc.ColorByBlock
	case 0xC2:
		return cadoc.ColorFromTrueColor(int32(u & 0xFFFFFF))
	case 0xC3:
		return cadoc.ColorIndex(int16(u & 0xFF))
	}
	return cadoc.ColorByBlock
}

func cmColorValue(c cadoc.Color) int64 {
	var u uint32
	switch {
	case c.IsTrueColor():
		u = 0xC2000000 | uint32(c.TrueColor())
	case c.IsByLayer():
		u = 0xC0000000
	case c.IsByBlock():
		u = 0xC1000000
	default:
		u = 0xC3000000 | uint32(c.Index())&0xFF
	}
	return int64(int32(u))
}

func (r *reader) parseMultiLeader(e *cadoc.MultiLeader) bool {
	s := r.s
	switch s.code {
	case 270:
	case 300:
		if e.ContextData == nil {
			e.ContextData = cadoc.NewMultiLeaderObjectContextData()
		}
		r.readMLeaderContext(e.ContextData)
	case 340:
		r.handleRef(s.handle(), func(o cadoc.Object) { e.Style, _ = o.(*cadoc.MultiLeaderStyle) })
	case 341:
		r.handleRef(s.handle(), func(o cadoc.Object) { e.LeaderLineType, _ = o.(*cadoc.LineType) })
	case 342:
		r.handleRef(s.handle(), func(o cadoc.Object) { e.Arrowhead, _ = o.(*cadoc.BlockRecord) })
	case 343:
		r.handleRef(s.handle(), func(o cadoc.Object) { e.TextStyle, _ = o.(*cadoc.TextStyle) })
	case 344:
		r.handleRef(s.handle(), func(o cadoc.Object) { e.BlockContent, _ = o.(*cadoc.BlockRecord) })
	case 330:
		if r.subclass != cadoc.SubclassMultiLeader {
			return false
		}
		a := cadoc.MultiLeaderBlockAttribute{}
		e.BlockAttributes = append(e.BlockAttributes, a)
		idx := len(e.BlockAttributes) - 1
		r.handleRef(s.handle(), func(o cadoc.Object) {
			e.BlockAttributes[idx].AttributeDefinition, _ = o.(*cadoc.AttributeDefinition)
		})
	case 177:
		if n := len(e.BlockAttributes); n > 0 {
			e.BlockAttributes[n-1].Index = s.i16()
		}
	case 44:
		if n := len(e.BlockAttributes); n > 0 {
			e.BlockAttributes[n-1].Width = s.float()
		}
	case 302:
		if n := len(e.BlockAttributes); n > 0 {
			e.BlockAttributes[n-1].Text = s.str
		}
	case 90:
		e.PropertyOverrideFlags = cadoc.MultiLeaderPropertyOverrideFlags(s.i32())
	case 170:
		e.PathType = cadoc.MultiLeaderPathType(s.i16())
	case 91:
		e.LineColor = cmColor(s.i32())
	case 171:
		e.LeaderLineWeight = cadoc.LineWeight(s.i16())
	case 290:
		e.EnableLanding = s.bool()
	case 291:
		e.EnableDogleg = s.bool()
	case 41:
		e.LandingDistance = s.float()
	case 42:
		e.ArrowheadSize = s.float()
	case 172:
		e.ContentType = cadoc.LeaderContentType(s.i16())
	case 173:
		e.TextLeftAttachment = cadoc.TextAttachmentType(s.i16())
	case 95:
		e.TextRightAttachment = cadoc.TextAttachmentType(s.i16())
	case 174:
		e.TextAngle = cadoc.TextAngleType(s.i16())
	case 175:
		e.TextAlignment = cadoc.TextAlignmentType(s.i16())
	case 92:
		e.TextColor = cmColor(s.i32())
	case 292:
		e.TextFrame = s.bool()
	case 93:
		e.BlockContentColor = cmColor(s.i32())
	case 10, 20, 30:
		setVec3(&e.BlockContentScale, s.code, s.float())
	case 43:
		e.BlockContentRotation = s.float()
	case 176:
		e.BlockContentConnection = cadoc.BlockContentConnectionType(s.i16())
	case 293:
		e.EnableAnnotationScale = s.bool()
	case 294:
		e.TextDirectionNegative = s.bool()
	case 178:
		e.TextAligninIPE = s.i16()
	case 179:
		e.TextAttachmentPoint = cadoc.TextAttachmentPointType(s.i16())
	case 45:
		e.ScaleFactor = s.float()
	case 271:
		e.TextAttachmentDirection = cadoc.TextAttachmentDirectionType(s.i16())
	case 272:
		e.TextBottomAttachment = cadoc.TextAttachmentType(s.i16())
	case 273:
		e.TextTopAttachment = cadoc.TextAttachmentType(s.i16())
	case 295:
		e.ExtendedToText = s.bool()
	default:
		return false
	}
	return true
}

// readMLeaderContext reads CONTEXT_DATA{ … } (the reader ends on "}").
func (r *reader) readMLeaderContext(c *cadoc.MultiLeaderObjectContextData) {
	s := r.s
	matrix := 0
	s.next()
	for !r.at0() {
		switch s.code {
		case 301:
			if s.str == "}" {
				return
			}
		case 302:
			if s.str == "LEADER{" {
				c.LeaderRoots = append(c.LeaderRoots, r.readMLeaderRoot())
			}
		case 340:
			r.handleRef(s.handle(), func(o cadoc.Object) { c.TextStyle, _ = o.(*cadoc.TextStyle) })
		case 341:
			r.handleRef(s.handle(), func(o cadoc.Object) { c.BlockContent, _ = o.(*cadoc.BlockRecord) })
		case 40:
			c.ScaleFactor = s.float()
		case 10, 20, 30:
			setVec3(&c.ContentBasePoint, s.code, s.float())
		case 41:
			c.TextHeight = s.float()
		case 140:
			c.ArrowheadSize = s.float()
		case 145:
			c.LandingGap = s.float()
		case 174:
			c.TextLeftAttachment = cadoc.TextAttachmentType(s.i16())
		case 175:
			c.TextRightAttachment = cadoc.TextAttachmentType(s.i16())
		case 176:
			c.TextAlignment = cadoc.TextAlignmentType(s.i16())
		case 177:
			c.BlockContentConnection = cadoc.BlockContentConnectionType(s.i16())
		case 290:
			c.HasTextContents = s.bool()
		case 304:
			c.TextLabel = s.str
		case 11, 21, 31:
			setVec3(&c.TextNormal, s.code, s.float())
		case 12, 22, 32:
			setVec3(&c.TextLocation, s.code, s.float())
		case 13, 23, 33:
			setVec3(&c.Direction, s.code, s.float())
		case 42:
			c.TextRotation = s.float()
		case 43:
			c.BoundaryWidth = s.float()
		case 44:
			c.BoundaryHeight = s.float()
		case 45:
			c.LineSpacingFactor = s.float()
		case 170:
			c.LineSpacing = cadoc.LineSpacingStyle(s.i16())
		case 90:
			c.TextColor = cmColor(s.i32())
		case 171:
			c.TextAttachmentPoint = cadoc.TextAttachmentPointType(s.i16())
		case 172:
			c.FlowDirection = cadoc.FlowDirectionType(s.i16())
		case 91:
			c.BackgroundFillColor = cmColor(s.i32())
		case 141:
			c.BackgroundScaleFactor = s.float()
		case 92:
			c.BackgroundTransparency = s.i32()
		case 291:
			c.BackgroundFillEnabled = s.bool()
		case 292:
			c.BackgroundMaskFillOn = s.bool()
		case 173:
			c.ColumnType = s.i16()
		case 293:
			c.TextHeightAutomatic = s.bool()
		case 142:
			c.ColumnWidth = s.float()
		case 143:
			c.ColumnGutter = s.float()
		case 144:
			c.ColumnSizes = append(c.ColumnSizes, s.float())
		case 294:
			c.ColumnFlowReversed = s.bool()
		case 295:
			c.WordBreak = s.bool()
		case 296:
			c.HasContentsBlock = s.bool()
		case 14, 24, 34:
			setVec3(&c.BlockContentNormal, s.code, s.float())
		case 15, 25, 35:
			setVec3(&c.BlockContentLocation, s.code, s.float())
		case 16, 26, 36:
			setVec3(&c.BlockContentScale, s.code, s.float())
		case 46:
			c.BlockContentRotation = s.float()
		case 93:
			c.BlockContentColor = cmColor(s.i32())
		case 47:
			if matrix < 16 {
				c.TransformationMatrix[matrix] = s.float()
				matrix++
			}
		case 110, 120, 130:
			setVec3(&c.BasePoint, s.code, s.float())
		case 111, 121, 131:
			setVec3(&c.BaseDirection, s.code, s.float())
		case 112, 122, 132:
			setVec3(&c.BaseVertical, s.code, s.float())
		case 297:
			c.NormalReversed = s.bool()
		case 272:
			c.TextBottomAttachment = cadoc.TextAttachmentType(s.i16())
		case 273:
			c.TextTopAttachment = cadoc.TextAttachmentType(s.i16())
		}
		s.next()
	}
	r.hold = true
}

func (r *reader) readMLeaderRoot() *cadoc.MultiLeaderLeaderRoot {
	s := r.s
	root := &cadoc.MultiLeaderLeaderRoot{}
	s.next()
	for !r.at0() {
		switch s.code {
		case 303:
			if s.str == "}" {
				return root
			}
		case 304:
			if s.str == "LEADER_LINE{" {
				root.Lines = append(root.Lines, r.readMLeaderLine())
			}
		case 290:
			root.ContentValid = s.bool()
		case 291:
			root.Unknown = s.bool()
		case 10, 20, 30:
			setVec3(&root.ConnectionPoint, s.code, s.float())
		case 11, 21, 31:
			setVec3(&root.Direction, s.code, s.float())
		case 12:
			root.BreakStartEndPointsPairs = append(root.BreakStartEndPointsPairs, cadoc.StartEndPointPair{StartPoint: cadoc.Vec3{X: s.float()}})
		case 22, 32:
			if n := len(root.BreakStartEndPointsPairs); n > 0 {
				setVec3(&root.BreakStartEndPointsPairs[n-1].StartPoint, s.code-2, s.float())
			}
		case 13, 23, 33:
			if n := len(root.BreakStartEndPointsPairs); n > 0 {
				setVec3(&root.BreakStartEndPointsPairs[n-1].EndPoint, s.code-3, s.float())
			}
		case 90:
			root.LeaderIndex = s.i32()
		case 40:
			root.LandingDistance = s.float()
		case 271:
			root.TextAttachmentDirection = cadoc.TextAttachmentDirectionType(s.i16())
		}
		s.next()
	}
	r.hold = true
	return root
}

func (r *reader) readMLeaderLine() *cadoc.MultiLeaderLeaderLine {
	s := r.s
	line := &cadoc.MultiLeaderLeaderLine{}
	s.next()
	for !r.at0() {
		switch s.code {
		case 305:
			if s.str == "}" {
				return line
			}
		case 10:
			line.Points = append(line.Points, cadoc.Vec3{X: s.float()})
		case 20, 30:
			if n := len(line.Points); n > 0 {
				setVec3(&line.Points[n-1], s.code, s.float())
			}
		case 90:
			line.SegmentIndex = s.i32()
		case 91:
			line.Index = s.i32()
		case 11:
			line.BreakInfo = append(line.BreakInfo, cadoc.StartEndPointPair{StartPoint: cadoc.Vec3{X: s.float()}})
		case 21, 31:
			if n := len(line.BreakInfo); n > 0 {
				setVec3(&line.BreakInfo[n-1].StartPoint, s.code-1, s.float())
			}
		case 12, 22, 32:
			if n := len(line.BreakInfo); n > 0 {
				setVec3(&line.BreakInfo[n-1].EndPoint, s.code-2, s.float())
			}
		case 170:
			line.PathType = cadoc.MultiLeaderPathType(s.i16())
		case 92:
			line.LineColor = cmColor(s.i32())
		case 340:
			r.handleRef(s.handle(), func(o cadoc.Object) { line.LineType, _ = o.(*cadoc.LineType) })
		case 171:
			line.LineWeight = cadoc.LineWeight(s.i16())
		case 40:
			line.ArrowheadSize = s.float()
		case 341:
			r.handleRef(s.handle(), func(o cadoc.Object) { line.Arrowhead, _ = o.(*cadoc.BlockRecord) })
		case 93:
			line.OverrideFlags = cadoc.LeaderLinePropertOverrideFlags(s.i32())
		}
		s.next()
	}
	r.hold = true
	return line
}

// ----- MLINE -----

type mlineState struct {
	styleName string
	vertex    *cadoc.MLineVertex
	segment   *cadoc.MLineSegment
}

func (r *reader) parseMLine(e *cadoc.MLine, m *mlineState) bool {
	s := r.s
	cur := func() *cadoc.MLineVertex {
		if len(e.Vertices) == 0 {
			e.Vertices = append(e.Vertices, cadoc.MLineVertex{})
		}
		return &e.Vertices[len(e.Vertices)-1]
	}
	switch s.code {
	case 2:
		name := s.str
		r.later(func() {
			if e.Style != nil {
				return
			}
			if d := r.doc.NamedDictionary(cadoc.AcadMLineStyleDictionary, false); d != nil {
				if o, ok := d.Get(name); ok {
					e.Style, _ = o.(*cadoc.MLineStyle)
				}
			}
		})
	case 340:
		r.handleRef(s.handle(), func(o cadoc.Object) {
			if st, ok := o.(*cadoc.MLineStyle); ok {
				e.Style = st
			}
		})
	case 40:
		e.ScaleFactor = s.float()
	case 70:
		e.Justification = cadoc.MLineJustification(s.i16())
	case 71:
		e.Flags = cadoc.MLineFlags(s.i16())
	case 72, 73, 75:
	case 10, 20, 30:
		setVec3(&e.StartPoint, s.code, s.float())
	case 210, 220, 230:
		setVec3(&e.Normal, s.code, s.float())
	case 11:
		e.Vertices = append(e.Vertices, cadoc.MLineVertex{Position: cadoc.Vec3{X: s.float()}})
	case 21, 31:
		setVec3(&cur().Position, s.code-1, s.float())
	case 12, 22, 32:
		setVec3(&cur().Direction, s.code-2, s.float())
	case 13, 23, 33:
		setVec3(&cur().Miter, s.code-3, s.float())
	case 74:
		v := cur()
		v.Segments = append(v.Segments, cadoc.MLineSegment{})
	case 41:
		v := cur()
		if n := len(v.Segments); n > 0 {
			v.Segments[n-1].Parameters = append(v.Segments[n-1].Parameters, s.float())
		}
	case 42:
		v := cur()
		if n := len(v.Segments); n > 0 {
			v.Segments[n-1].AreaFillParameters = append(v.Segments[n-1].AreaFillParameters, s.float())
		}
	default:
		return false
	}
	return true
}

// ----- MESH -----

func (r *reader) parseMesh(e *cadoc.Mesh) bool {
	s := r.s
	if r.subclass != cadoc.SubclassMesh {
		return false
	}
	switch s.code {
	case 71:
		e.Version = s.i16()
	case 72:
		e.BlendCrease = s.bool()
	case 91:
		e.SubdivisionLevel = s.i32()
	case 90:
	case 92:
		n := int(s.int())
		for i := 0; i < n; i++ {
			var p cadoc.Vec3
			s.next()
			p.X = s.float()
			s.next()
			p.Y = s.float()
			s.next()
			p.Z = s.float()
			e.Vertices = append(e.Vertices, p)
		}
	case 93:
		total := int(s.int())
		for i := 0; i < total; {
			s.next()
			n := int(s.int())
			face := make([]int32, 0, n)
			for j := 0; j < n; j++ {
				s.next()
				face = append(face, s.i32())
			}
			e.Faces = append(e.Faces, face)
			i += n + 1
		}
	case 94:
		n := int(s.int())
		for i := 0; i < n; i++ {
			var ed cadoc.MeshEdge
			s.next()
			ed.Start = s.i32()
			s.next()
			ed.End = s.i32()
			e.Edges = append(e.Edges, ed)
		}
	case 95:
		n := int(s.int())
		for i := 0; i < n; i++ {
			s.next()
			c := s.float()
			if i < len(e.Edges) {
				e.Edges[i].Crease = &c
			}
		}
	default:
		return false
	}
	return true
}

// ----- VIEWPORT -----

func (r *reader) parseViewport(e *cadoc.Viewport) bool {
	s := r.s
	switch s.code {
	case 67:
		return false
	case 68:
		e.ActiveStatus = s.i16()
	case 69:
		e.Id = s.i16()
	case 10, 20, 30:
		setVec3(&e.Center, s.code, s.float())
	case 40:
		e.Width = s.float()
	case 41:
		e.Height = s.float()
	case 12, 22:
		setVec2(&e.ViewCenter, s.code, s.float())
	case 13, 23:
		setVec2(&e.SnapBase, s.code, s.float())
	case 14, 24:
		setVec2(&e.SnapSpacing, s.code, s.float())
	case 15, 25:
		setVec2(&e.GridSpacing, s.code, s.float())
	case 16, 26, 36:
		setVec3(&e.ViewDirection, s.code, s.float())
	case 17, 27, 37:
		setVec3(&e.ViewTarget, s.code, s.float())
	case 42:
		e.LensLength = s.float()
	case 43:
		e.FrontClipPlane = s.float()
	case 44:
		e.BackClipPlane = s.float()
	case 45:
		e.ViewHeight = s.float()
	case 50:
		e.SnapAngle = s.angle()
	case 51:
		e.TwistAngle = s.angle()
	case 72:
		e.CircleZoomPercent = s.i16()
	case 331:
		h := s.handle()
		r.later(func() {
			if l, ok := r.object(h).(*cadoc.Layer); ok {
				e.FrozenLayers = append(e.FrozenLayers, l)
			} else {
				r.warn("Frozen layer %X not found for viewport %X", h, e.Handle)
			}
		})
	case 90:
		e.Status = cadoc.ViewportStatusFlags(s.i32())
	case 340:
		r.handleRef(s.handle(), func(o cadoc.Object) { e.Boundary, _ = o.(cadoc.Entity) })
	case 1:
		e.StyleSheetName = s.str
	case 281:
		e.RenderMode = cadoc.RenderMode(s.int())
	case 71:
		e.UcsPerViewport = s.bool()
	case 74:
		e.DisplayUcsIcon = s.bool()
	case 110, 120, 130:
		setVec3(&e.UcsOrigin, s.code, s.float())
	case 111, 121, 131:
		setVec3(&e.UcsXAxis, s.code, s.float())
	case 112, 122, 132:
		setVec3(&e.UcsYAxis, s.code, s.float())
	case 345:
		r.handleRef(s.handle(), func(o cadoc.Object) { e.NamedUcs, _ = o.(*cadoc.UCS) })
	case 346:
		r.handleRef(s.handle(), func(o cadoc.Object) { e.BaseUcs, _ = o.(*cadoc.UCS) })
	case 79:
		e.UcsOrthographicType = cadoc.OrthographicType(s.i16())
	case 146:
		e.Elevation = s.float()
	case 170:
		e.ShadePlotMode = cadoc.ShadePlotMode(s.i16())
	case 61:
		e.MajorGridLineFrequency = s.i16()
	case 332, 333:
	case 348:
		r.handleRef(s.handle(), func(o cadoc.Object) { e.VisualStyle = o })
	case 292:
		e.UseDefaultLighting = s.bool()
	case 282:
		e.DefaultLightingType = cadoc.LightingType(s.int())
	case 141:
		e.Brightness = s.float()
	case 142:
		e.Contrast = s.float()
	case 63:
		if !e.AmbientLightColor.IsTrueColor() {
			e.AmbientLightColor = cadoc.ColorIndex(s.i16())
		}
	case 421:
		e.AmbientLightColor = cadoc.ColorFromTrueColor(s.i32())
	case 431, 361, 335, 343, 344, 91:
	default:
		return false
	}
	return true
}

// ----- IMAGE / WIPEOUT -----

func (r *reader) parseWipeoutBase(w *cadoc.WipeoutBase) bool {
	s := r.s
	switch s.code {
	case 90:
		w.ClassVersion = s.i32()
	case 10, 20, 30:
		setVec3(&w.InsertPoint, s.code, s.float())
	case 11, 21, 31:
		setVec3(&w.UVector, s.code, s.float())
	case 12, 22, 32:
		setVec3(&w.VVector, s.code, s.float())
	case 13, 23:
		setVec2(&w.Size, s.code, s.float())
	case 340:
		r.handleRef(s.handle(), func(o cadoc.Object) { w.Definition, _ = o.(*cadoc.ImageDefinition) })
	case 360:
		r.handleRef(s.handle(), func(o cadoc.Object) { w.DefinitionReactor, _ = o.(*cadoc.ImageDefinitionReactor) })
	case 70:
		w.Flags = cadoc.ImageDisplayFlags(s.i16())
	case 280:
		w.ClippingState = s.bool()
	case 281:
		w.Brightness = uint8(s.int())
	case 282:
		w.Contrast = uint8(s.int())
	case 283:
		w.Fade = uint8(s.int())
	case 290:
		w.ClipMode = cadoc.ClipMode(s.int())
	case 71:
		w.ClipType = cadoc.ClipType(s.i16())
	case 91:
		n := int(s.int())
		for i := 0; i < n; i++ {
			var p cadoc.Vec2
			s.next()
			p.X = s.float()
			s.next()
			p.Y = s.float()
			w.ClipBoundaryVertices = append(w.ClipBoundaryVertices, p)
		}
		// A polygonal boundary repeats its first vertex; the model keeps it open.
		if v := w.ClipBoundaryVertices; w.ClipType == cadoc.ClipTypePolygonal && len(v) > 3 && v[0] == v[len(v)-1] {
			w.ClipBoundaryVertices = v[:len(v)-1]
		}
	case 14, 24:
	default:
		return false
	}
	return true
}

// ----- OLE2FRAME -----

func (r *reader) parseOle2Frame(e *cadoc.Ole2Frame) bool {
	s := r.s
	switch s.code {
	case 1, 90, 73:
	case 70:
		e.Version = s.i16()
	case 3:
		e.SourceApplication = s.str
	case 10, 20, 30:
		setVec3(&e.UpperLeftCorner, s.code, s.float())
	case 11, 21, 31:
		setVec3(&e.LowerRightCorner, s.code, s.float())
	case 71:
		e.OleObjectType = cadoc.OleObjectType(s.i16())
	case 72:
		e.IsPaperSpace = s.bool()
	case 310:
		e.BinaryData = append(e.BinaryData, s.chunk...)
	default:
		return false
	}
	return true
}

// ----- PDFUNDERLAY -----

func (r *reader) parsePdfUnderlay(e *cadoc.PdfUnderlay) bool {
	s := r.s
	switch s.code {
	case 340:
		r.handleRef(s.handle(), func(o cadoc.Object) { e.Definition, _ = o.(*cadoc.PdfUnderlayDefinition) })
	case 10, 20, 30:
		setVec3(&e.InsertPoint, s.code, s.float())
	case 41:
		e.XScale = s.float()
	case 42:
		e.YScale = s.float()
	case 43:
		e.ZScale = s.float()
	case 50:
		e.Rotation = s.angle()
	case 210, 220, 230:
		setVec3(&e.Normal, s.code, s.float())
	case 280:
		e.Flags = cadoc.UnderlayDisplayFlags(s.int())
	case 281:
		e.Contrast = uint8(s.int())
	case 282:
		e.Fade = uint8(s.int())
	case 11:
		e.ClipBoundaryVertices = append(e.ClipBoundaryVertices, cadoc.Vec2{X: s.float()})
	case 21:
		if n := len(e.ClipBoundaryVertices); n > 0 {
			e.ClipBoundaryVertices[n-1].Y = s.float()
		}
	default:
		return false
	}
	return true
}

// ----- REGION / 3DSOLID / BODY / SURFACE -----

type acisState struct {
	text    strings.Builder
	encoded *bool
}

func (a *acisState) append(chunk string, newLine bool) {
	if newLine && a.text.Len() > 0 {
		a.text.WriteByte('\n')
	}
	if chunk == "" {
		return
	}
	if a.encoded == nil {
		enc := acisIsEncoded(chunk)
		a.encoded = &enc
	}
	if *a.encoded {
		chunk = acisCode(chunk)
	}
	a.text.WriteString(chunk)
}

func (r *reader) parseModeler(m *cadoc.ModelerGeometry, a *acisState) bool {
	s := r.s
	switch s.code {
	case 1:
		a.append(s.str, true)
	case 3:
		a.append(s.str, false)
	case 2:
		m.Guid = s.str
	case 70:
		m.ModelerFormatVersion = s.i16()
	case 290:
	default:
		return false
	}
	return true
}

func (r *reader) parseSurface(e *cadoc.Surface, a *acisState) bool {
	s := r.s
	switch s.code {
	case 1, 2, 3, 70, 290:
		if r.subclass == "" || r.subclass == cadoc.SubclassModelerGeometry || r.subclass == cadoc.SubclassEntity {
			return r.parseModeler(&e.ModelerGeometry, a)
		}
	case 71:
		if r.subclass == cadoc.SubclassSurface {
			e.UIsolines = s.i16()
			return true
		}
	case 72:
		if r.subclass == cadoc.SubclassSurface {
			e.VIsolines = s.i16()
			return true
		}
	}
	switch s.code {
	case 100:
		r.subclass = s.str
		if s.str != cadoc.SubclassEntity && s.str != cadoc.SubclassModelerGeometry && s.str != cadoc.SubclassSurface {
			e.Groups = append(e.Groups, cadoc.GroupCode{Code: s.code, Value: s.str})
		}
		return true
	case 5, 330, 102, 1001:
		return false
	}
	if r.subclass == "" || r.subclass == cadoc.SubclassEntity {
		return false
	}
	if r.subclass == cadoc.SubclassModelerGeometry || r.subclass == cadoc.SubclassSurface {
		return true
	}
	e.Groups = append(e.Groups, cadoc.GroupCode{Code: s.code, Value: s.value()})
	return true
}

// ----- ACAD_PROXY_ENTITY -----

func (r *reader) readByteEntries(code int) []byte {
	s := r.s
	if s.int() == 0 {
		return nil
	}
	var out []byte
	s.next()
	for s.code == code && !s.eof {
		out = append(out, s.chunk...)
		s.next()
	}
	r.hold = true
	return out
}

func (r *reader) parseProxyEntity(e *cadoc.ProxyEntity) bool {
	s := r.s
	if r.subclass != cadoc.SubclassProxyEntity {
		return false
	}
	switch s.code {
	case 90:
		e.ProxyClassId = s.i32()
	case 91:
		e.ClassNumber = s.i32()
		if c, ok := r.doc.Classes.GetByClassNumber(int16(e.ClassNumber)); ok {
			e.Class = c
		}
	case 71:
		e.Version = s.i16()
	case 97:
		e.MaintenanceVersion = s.i32()
	case 70:
		e.OriginalDataFormatDxf = s.bool()
	case 92, 160:
		e.ProxyGraphics = r.readByteEntries(310)
	case 93, 161:
		e.BinaryData = r.readByteEntries(310)
	case 162:
		e.StringData = r.readByteEntries(311)
	case 94, 95, 330, 340, 350, 360:
	default:
		return false
	}
	return true
}

// ----- ACAD_TABLE -----

func (r *reader) parseTableEntity(e *cadoc.TableEntity) bool {
	s := r.s
	if r.subclass != cadoc.SubclassTableEntity {
		if s.code == 100 {
			return false
		}
		return r.parseInsert(&e.Insert)
	}
	cell := func() *cadoc.TableCell {
		if len(e.Cells) == 0 {
			e.Cells = append(e.Cells, cadoc.TableCell{})
		}
		return &e.Cells[len(e.Cells)-1]
	}
	switch s.code {
	case 1, 2:
		c := cell()
		c.Text += s.str
	case 280:
		e.Version = s.i16()
	case 342:
		r.handleRef(s.handle(), func(o cadoc.Object) { e.Style = o })
	case 343:
		r.handleRef(s.handle(), func(o cadoc.Object) { e.TableBlock, _ = o.(*cadoc.BlockRecord) })
	case 11, 21, 31:
		setVec3(&e.HorizontalDirection, s.code, s.float())
	case 90:
		e.ValueFlag = s.i32()
	case 91, 92:
	case 93:
		e.OverrideFlag = s.bool()
	case 94:
		e.OverrideBorderColor = s.bool()
	case 95:
		e.OverrideBorderLineWeight = s.bool()
	case 96:
		e.OverrideBorderVisibility = s.bool()
	case 141:
		e.Rows = append(e.Rows, cadoc.TableRow{Height: s.float()})
	case 142:
		e.Columns = append(e.Columns, cadoc.TableColumn{Width: s.float()})
	case 171:
		e.Cells = append(e.Cells, cadoc.TableCell{Type: cadoc.TableCellType(s.int())})
	case 172:
		cell().EdgeFlags = s.i16()
	case 173:
		cell().Merged = s.i16()
	case 174:
		cell().AutoFit = s.bool()
	case 145:
		cell().Rotation = s.float()
	case 301:
		c := cell()
		s.next()
		for !r.at0() && s.code != 304 {
			if s.code == 1 || s.code == 2 || s.code == 302 {
				if s.code == 302 && c.Text != "" {
					s.next()
					continue
				}
				c.Text += s.str
			}
			s.next()
		}
		if r.at0() {
			r.hold = true
		}
	default:
		if s.code == 10 || s.code == 20 || s.code == 30 || s.code == 41 || s.code == 42 || s.code == 43 ||
			s.code == 50 || s.code == 210 || s.code == 220 || s.code == 230 {
			return r.parseInsert(&e.Insert)
		}
		// Cell formatting groups are not modelled.
		return s.code != 100 && s.code != 1001 && s.code != 102 && s.code != 5 && s.code != 330
	}
	return true
}

var _ = math.Pi
