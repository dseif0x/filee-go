package cad

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// The scene builder turns a document into a Scene: it walks model space (or
// the active layout when model space is empty), resolves layers, colours,
// line weights and line types, expands block references and converts every
// supported entity into geometry in WCS.

const (
	// maxPrimitives: beyond this a drawing is cut short rather than exhausting memory.
	maxPrimitives = 1_000_000
	// maxDepth is the deepest block nesting followed (also stops self-referencing blocks).
	maxDepth = 16
	// maxArrayItems is the largest MINSERT array expanded (rows × columns).
	maxArrayItems = 10_000
	// defaultLineWeightMm is used for "Default" (AutoCAD's LWDEFAULT is 0.25 mm;
	// a thinner line reads better on screen).
	defaultLineWeightMm = 0.18
	// thinnestLineWeightMm draws line weight 0 as a fine line, not a hairline.
	thinnestLineWeightMm = 0.1
)

type builder struct {
	ctx             context.Context
	doc             *cadoc.Document
	scene           *Scene
	lineTypeScale   float64
	pointsVisible   bool
	visited         int
	patternSegments int
}

func newBuilder(ctx context.Context, doc *cadoc.Document) *builder {
	b := &builder{ctx: ctx, doc: doc, scene: NewScene(), lineTypeScale: 1, pointsVisible: true}
	if h := doc.Header; h != nil {
		if h.LineTypeScale > 0 && !math.IsInf(h.LineTypeScale, 0) {
			b.lineTypeScale = h.LineTypeScale
		}
		b.pointsVisible = h.PointDisplayMode != 1
	}
	return b
}

// drawContext is what the entities inside a block reference inherit: the
// transform to WCS, the insert's layer (entities on "0" take it over), and
// the colour, line weight and line type used by BYBLOCK.
type drawContext struct {
	transform     Affine3
	insertLayer   *cadoc.Layer
	blockColor    RGB
	blockWidthMm  float64
	blockLineType *cadoc.LineType
	depth         int
}

// rootContext: model space; BYBLOCK outside blocks draws like colour 7 (black on paper).
func rootContext() drawContext {
	return drawContext{transform: Identity3, blockColor: black, blockWidthMm: defaultLineWeightMm}
}

// BuildScene is the scene of a document: model space, or the active layout
// (paper space) when model space has nothing visible. Viewports are not followed.
func BuildScene(ctx context.Context, doc *cadoc.Document) (*Scene, error) {
	b := newBuilder(ctx, doc)
	if ms := doc.ModelSpace(); ms != nil {
		if err := b.drawEntities(ms.Entities, rootContext()); err != nil {
			return nil, err
		}
	}
	if len(b.scene.Items) == 0 {
		if paper := doc.PaperSpace(); paper != nil {
			b.scene.Space = paper.Name
			if paper.Layout != nil {
				b.scene.Space = paper.Layout.Name
			}
			if err := b.drawEntities(paper.Entities, rootContext()); err != nil {
				return nil, err
			}
		}
	}
	return b.scene, nil
}

// BuildLayouts is the paper-space layouts with something on them, in tab
// order (#172): each one's own entities (title block, notes) and the
// viewports that show model space. Empty when no layout has anything of its
// own besides viewports, so the drawing is printed as model space.
func BuildLayouts(ctx context.Context, doc *cadoc.Document) ([]*LayoutSheet, error) {
	var layouts []*cadoc.Layout
	for _, l := range doc.Layouts() {
		if !strings.EqualFold(l.Name, cadoc.ModelLayoutName) && l.AssociatedBlock != nil {
			layouts = append(layouts, l)
		}
	}
	slices.SortStableFunc(layouts, func(a, b *cadoc.Layout) int { return int(a.TabOrder) - int(b.TabOrder) })
	var sheets []*LayoutSheet
	anyPaper := false
	for _, layout := range layouts {
		b := newBuilder(ctx, doc)
		b.scene.Space = layout.Name
		if err := b.drawEntities(layout.AssociatedBlock.Entities, rootContext()); err != nil {
			return nil, err
		}
		// The viewport that is the sheet itself shows no model space, and one
		// that is off shows nothing. Twisted views are drawn untwisted, from the top.
		var views []ViewportView
		for _, e := range layout.AssociatedBlock.Entities {
			v, ok := e.(*cadoc.Viewport)
			if !ok || v.RepresentsPaper() || v.Status&cadoc.ViewportStatusFlagsViewportOff != 0 || v.Width <= 0 || v.Height <= 0 || v.ViewHeight <= 0 {
				continue
			}
			frame := EmptyBox()
			frame.Add(Vec2{v.Center.X - v.Width/2, v.Center.Y - v.Height/2})
			frame.Add(Vec2{v.Center.X + v.Width/2, v.Center.Y + v.Height/2})
			if frame.Empty {
				continue
			}
			// The view centre is in display coordinates, whose origin is the view target.
			views = append(views, ViewportView{Frame: frame, ViewCenter: Vec2{v.ViewTarget.X + v.ViewCenter.X, v.ViewTarget.Y + v.ViewCenter.Y}, Scale: v.Height / v.ViewHeight})
		}
		if len(b.scene.Items) > 0 || len(views) > 0 {
			sheets = append(sheets, &LayoutSheet{Name: layout.Name, Paper: b.scene, Viewports: views})
			if len(b.scene.Items) > 0 {
				anyPaper = true
			}
		}
	}
	// AutoCAD gives a layout a viewport the first time its tab is opened: a
	// drawing made in model space then has a Layout1 with only that viewport.
	// Without a border, title block or notes on any layout, model space is the better page.
	if !anyPaper {
		return nil, nil
	}
	return sheets, nil
}

func (b *builder) drawEntities(entities []cadoc.Entity, c drawContext) error {
	for _, e := range entities {
		b.visited++
		if b.visited&1023 == 0 && b.ctx.Err() != nil {
			return b.ctx.Err()
		}
		if b.scene.Truncated {
			return nil
		}
		if e.Entity().IsInvisible {
			continue
		}
		layer := effectiveLayer(e, c)
		func() {
			// One broken entity (missing references, degenerate data) must not lose the drawing.
			defer func() {
				if r := recover(); r != nil {
					b.skip(e.ObjectName() + " (invalid)")
				}
			}()
			if ins, ok := e.(*cadoc.Insert); ok {
				b.drawInsert(ins, layer, c)
				return
			}
			if !isShown(layer) {
				return
			}
			b.drawEntity(e, layer, c)
		}()
		if err := b.ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}

func (b *builder) drawEntity(e cadoc.Entity, layer *cadoc.Layer, c drawContext) {
	switch x := e.(type) {
	case *cadoc.Line:
		b.stroke(e, layer, c, figure2(c.transform.Point(v3(x.StartPoint)), c.transform.Point(v3(x.EndPoint))), 0)
	case *cadoc.Arc:
		m := c.transform.Multiply(Ocs(v3(x.Normal)))
		start, end := x.StartAngle, x.EndAngle
		for end <= start {
			end += 2 * math.Pi
		}
		b.stroke(e, layer, c, arcFigure(m, v3(x.Center), x.Radius, x.Radius, start, end), 0)
	case *cadoc.Circle:
		m := c.transform.Multiply(Ocs(v3(x.Normal)))
		f := arcFigure(m, v3(x.Center), x.Radius, x.Radius, 0, 2*math.Pi)
		f.Closed = true
		b.stroke(e, layer, c, f, 0)
	case *cadoc.Ellipse:
		b.drawEllipse(x, layer, c)
	case *cadoc.LwPolyline:
		b.drawLwPolyline(x, layer, c)
	case *cadoc.PolyfaceMesh:
		b.drawPolyfaceMesh(x, layer, c)
	case *cadoc.PolygonMesh:
		b.drawPolygonMesh(x, layer, c)
	case *cadoc.Polyline2D:
		b.drawPolyline2D(x, layer, c)
	case *cadoc.Polyline3D:
		b.drawPolyline3D(x, layer, c)
	case *cadoc.Spline:
		b.drawSpline(x, layer, c)
	case *cadoc.AttributeDefinition:
		// Inside blocks, definitions are replaced by the insert's attributes; alone they show their tag.
		if c.depth == 0 {
			b.drawText(&x.TextEntity, x.Tag, e, layer, c)
		}
	case *cadoc.AttributeEntity:
		if x.Flags&cadoc.AttributeFlagsHidden == 0 {
			if x.MText != nil {
				b.drawMText(x.MText, layer, c)
			} else {
				b.drawText(&x.TextEntity, x.Value, e, layer, c)
			}
		}
	case *cadoc.TextEntity:
		b.drawText(x, x.Value, e, layer, c)
	case *cadoc.MText:
		b.drawMText(x, layer, c)
	case cadoc.Dimension:
		b.drawDimension(x, layer, c)
	case *cadoc.Leader:
		b.drawLeader(x, layer, c)
	case *cadoc.MultiLeader:
		b.drawMultiLeader(x, layer, c)
	case *cadoc.Hatch:
		b.drawHatch(x, layer, c)
	case *cadoc.Solid:
		b.drawSolid(x, layer, c)
	case *cadoc.Face3D:
		b.drawFace(x, layer, c)
	case *cadoc.Point:
		if b.pointsVisible {
			loc := c.transform.Point(v3(x.Location))
			b.add(&PointPrimitive{Location: loc, Color: resolveColor(e.Entity().Color, layer, c), WidthMm: resolveWidth(e, layer, c)}, loc)
		}
	case *cadoc.XLine:
		b.drawInfinite(e, layer, c, v3(x.FirstPoint), v3(x.Direction), false)
	case *cadoc.Ray:
		b.drawInfinite(e, layer, c, v3(x.StartPoint), v3(x.Direction), true)
	case *cadoc.MLine:
		b.drawMLine(x, layer, c)
	case *cadoc.Mesh:
		b.drawMesh(x, layer, c)
	case *cadoc.Viewport:
		// layout viewports show model space through a view: not followed here
	case *cadoc.Seqend:
		// end marker of old-style polylines and attributes
	default:
		b.skip(e.ObjectName())
	}
}

func v3(p cadoc.Vec3) Vec3 { return Vec3{p.X, p.Y, p.Z} }

// ---- blocks ----

func (b *builder) drawInsert(ins *cadoc.Insert, layer *cadoc.Layer, c drawContext) {
	// A frozen layer hides the whole reference; an off (or non-plotting) layer
	// only hides what inherits it: the block's entities on layer "0".
	if layer.IsFrozen() {
		return
	}
	block := ins.Block
	if block == nil {
		b.skip("INSERT (missing block)")
		return
	}
	if c.depth >= maxDepth {
		b.scene.Truncated = true
		return
	}
	inherited := b.inherit(ins, layer, c)
	var base Vec3
	if block.BlockEntity != nil {
		base = v3(block.BlockEntity.BasePoint)
	}
	columns := max(1, int(ins.ColumnCount))
	rows := max(1, int(ins.RowCount))
	if columns*rows > maxArrayItems {
		b.scene.Truncated = true
		columns = min(columns, maxArrayItems)
		rows = max(1, min(rows, maxArrayItems/columns))
	}
	placement := c.transform.Multiply(Ocs(v3(ins.Normal))).Multiply(Translation(v3(ins.InsertPoint))).Multiply(RotationZ(ins.Rotation))
	scale := Scale3(ins.XScale, ins.YScale, ins.ZScale).Multiply(Translation(Vec3{-base.X, -base.Y, -base.Z}))
	for row := 0; row < rows && !b.scene.Truncated; row++ {
		for col := 0; col < columns && !b.scene.Truncated; col++ {
			cell := placement.Multiply(Translation(Vec3{float64(col) * ins.ColumnSpacing, float64(row) * ins.RowSpacing, 0})).Multiply(scale)
			cc := inherited
			cc.transform = cell
			b.drawEntities(block.Entities, cc)
		}
	}
	// Attributes belong to the reference: positioned in the parent's
	// coordinates, but like the block's contents they take over the insert's
	// layer when on "0" and its BYBLOCK properties.
	ac := inherited
	ac.transform, ac.depth = c.transform, c.depth
	for _, a := range ins.Attributes {
		al := effectiveLayer(a, ac)
		if !a.IsInvisible && isShown(al) {
			b.drawEntity(a, al, ac)
		}
	}
}

// inherit is the context for the contents of a block reference, dimension or multileader block.
func (b *builder) inherit(ref cadoc.Entity, layer *cadoc.Layer, c drawContext) drawContext {
	n := c
	n.insertLayer = layer
	n.blockColor = resolveColor(ref.Entity().Color, layer, c)
	n.blockWidthMm = resolveWidth(ref, layer, c)
	n.blockLineType = resolveLineType(ref, layer, c)
	n.depth = c.depth + 1
	return n
}

func (b *builder) drawDimension(d cadoc.Dimension, layer *cadoc.Layer, c drawContext) {
	base := d.Dimension()
	block := base.Block
	if block == nil || len(block.Entities) == 0 {
		// Some writers omit the dimension picture: generate one from the definition points.
		if err := cadoc.UpdateDimensionBlock(b.doc, d); err == nil {
			block = base.Block
		} else {
			block = nil
		}
	}
	if block == nil || len(block.Entities) == 0 {
		b.skip("DIMENSION (no block)")
		return
	}
	if c.depth >= maxDepth {
		return
	}
	// The picture is stored in drawing coordinates (each entity carries its
	// own extrusion), so it is drawn in place; colour, layer and line
	// properties come from the dimension (BYBLOCK).
	b.drawEntities(block.Entities, b.inherit(d, layer, c))
}

// ---- curves ----

func (b *builder) drawEllipse(e *cadoc.Ellipse, layer *cadoc.Layer, c drawContext) {
	// ELLIPSE is in WCS: the minor axis is the major axis turned 90° around the normal.
	major := v3(e.MajorAxisEndPoint)
	minor := Cross(v3(e.Normal).Normalized(), major).Mul(e.RadiusRatio)
	start, end := e.StartParameter, e.EndParameter
	for end <= start {
		end += 2 * math.Pi
	}
	m := c.transform
	center := m.Point(v3(e.Center))
	u, v := m.Vector(major), m.Vector(minor)
	f := NewFigure(center.Add(u.Mul(math.Cos(start))).Add(v.Mul(math.Sin(start))))
	f.ArcTo(center, u, v, start, end)
	f.Closed = math.Abs(end-start-2*math.Pi) < 1e-9
	b.stroke(e, layer, c, f, 0)
}

// arcFigure is an arc of the circle/ellipse with radii rx, ry in m's plane.
func arcFigure(m Affine3, center Vec3, rx, ry, start, end float64) *Figure {
	cc := m.Point(center)
	u := m.VectorXY(rx, 0)
	v := m.VectorXY(0, ry)
	f := NewFigure(cc.Add(u.Mul(math.Cos(start))).Add(v.Mul(math.Sin(start))))
	f.ArcTo(cc, u, v, start, end)
	return f
}

func (b *builder) drawSpline(s *cadoc.Spline, layer *cadoc.Layer, c drawContext) {
	var pts []Vec3
	if len(s.ControlPoints) >= 2 {
		ctrl := make([]Vec3, len(s.ControlPoints))
		for i, p := range s.ControlPoints {
			ctrl[i] = v3(p)
		}
		var w []float64
		if len(s.Weights) == len(s.ControlPoints) {
			w = s.Weights
		}
		pts = SampleNurbs(int(s.Degree), ctrl, s.Knots, w)
	} else {
		fit := make([]Vec3, len(s.FitPoints))
		for i, p := range s.FitPoints {
			fit[i] = v3(p)
		}
		pts = ThroughPoints(fit, s.Flags&cadoc.SplineFlagsClosed != 0)
	}
	if len(pts) >= 2 {
		b.stroke(s, layer, c, polylineFigure(c.transform, pts, false), 0)
	}
}

func polylineFigure(m Affine3, pts []Vec3, closed bool) *Figure {
	f := NewFigure(m.Point(pts[0]))
	for _, p := range pts[1:] {
		f.LineTo(m.Point(p))
	}
	f.Closed = closed
	return f
}

func figure2(a, b Vec2) *Figure {
	f := NewFigure(a)
	f.LineTo(b)
	return f
}

// ---- polylines ----

// polyVertex is a vertex in the entity's OCS with the widths of the following segment.
type polyVertex struct {
	loc                  Vec2
	bulge                float64
	startWidth, endWidth float64
}

func (b *builder) drawLwPolyline(p *cadoc.LwPolyline, layer *cadoc.Layer, c drawContext) {
	if len(p.Vertices) == 0 {
		return
	}
	vs := make([]polyVertex, len(p.Vertices))
	for i, v := range p.Vertices {
		sw, ew := p.ConstantWidth, p.ConstantWidth
		if v.StartWidth > 0 || v.EndWidth > 0 {
			sw, ew = v.StartWidth, v.EndWidth
		}
		vs[i] = polyVertex{Vec2{v.Location.X, v.Location.Y}, v.Bulge, sw, ew}
	}
	m := c.transform.Multiply(Ocs(v3(p.Normal))).Multiply(Translation(Vec3{0, 0, p.Elevation}))
	b.drawPolyline(p, layer, c, m, vs, p.Flags&cadoc.LwPolylineFlagsClosed != 0)
}

func (b *builder) drawPolyline2D(p *cadoc.Polyline2D, layer *cadoc.Layer, c drawContext) {
	// R12 files can give meshes as plain polylines whose face records look like vertices.
	if p.Flags&(cadoc.PolylineFlagsPolyfaceMesh|cadoc.PolylineFlagsPolygonMesh) != 0 {
		b.skip(p.ObjectName() + " (mesh)")
		return
	}
	smoothed := p.Flags&cadoc.PolylineFlagsSplineFit != 0
	var list []*cadoc.Vertex2D
	for _, v := range p.Vertices {
		// Spline-fit polylines keep their frame next to the fitted vertices: draw the fit.
		if smoothed && v.Flags&cadoc.VertexFlagsSplineFrameControlPoint != 0 {
			continue
		}
		list = append(list, v)
	}
	if len(list) == 0 {
		return
	}
	anyWidth := false
	for _, v := range list {
		if v.StartWidth > 0 || v.EndWidth > 0 {
			anyWidth = true
		}
	}
	vs := make([]polyVertex, len(list))
	for i, v := range list {
		bulge := v.Bulge
		if smoothed {
			bulge = 0
		}
		sw, ew := p.StartWidth, p.EndWidth
		if anyWidth {
			sw, ew = v.StartWidth, v.EndWidth
		}
		vs[i] = polyVertex{Vec2{v.Location.X, v.Location.Y}, bulge, sw, ew}
	}
	m := c.transform.Multiply(Ocs(v3(p.Normal))).Multiply(Translation(Vec3{0, 0, p.Elevation}))
	b.drawPolyline(p, layer, c, m, vs, p.Flags&cadoc.PolylineFlagsClosedPolylineOrClosedPolygonMeshInM != 0)
}

func (b *builder) drawPolyline3D(p *cadoc.Polyline3D, layer *cadoc.Layer, c drawContext) {
	if p.Flags&(cadoc.PolylineFlagsPolyfaceMesh|cadoc.PolylineFlagsPolygonMesh) != 0 {
		b.skip(p.ObjectName() + " (mesh)")
		return
	}
	smoothed := p.Flags&cadoc.PolylineFlagsSplineFit != 0
	var pts []Vec3
	for _, v := range p.Vertices {
		if smoothed && v.Flags&cadoc.VertexFlagsSplineFrameControlPoint != 0 {
			continue
		}
		pts = append(pts, v3(v.Location))
	}
	if len(pts) == 0 {
		return
	}
	if len(pts) == 1 {
		pts = append(pts, pts[0])
	}
	b.stroke(p, layer, c, polylineFigure(c.transform, pts, p.Flags&cadoc.PolylineFlagsClosedPolylineOrClosedPolygonMeshInM != 0), 0)
}

// drawPolyline draws a 2D polyline in plane m: thin polylines become one
// stroked figure with bulge arcs, a constant width a wide stroke, and
// varying widths (arrows, tapers) filled bands.
func (b *builder) drawPolyline(e cadoc.Entity, layer *cadoc.Layer, c drawContext, m Affine3, vs []polyVertex, closed bool) {
	segs := len(vs) - 1
	if closed {
		segs = len(vs)
	}
	f := NewFigure(m.PointXY(vs[0].loc.X, vs[0].loc.Y))
	if segs <= 0 {
		f.LineTo(f.Start) // a single vertex shows as a dot
		b.stroke(e, layer, c, f, 0)
		return
	}
	constant := vs[0].startWidth
	isConstant, allThin := true, true
	for i := 0; i < segs; i++ {
		if math.Abs(vs[i].startWidth-constant) >= 1e-12 || math.Abs(vs[i].endWidth-constant) >= 1e-12 {
			isConstant = false
		}
		if vs[i].startWidth > 0 || vs[i].endWidth > 0 {
			allThin = false
		}
	}
	if isConstant || allThin {
		for i := 0; i < segs; i++ {
			appendSegment(f, m, vs[i], vs[(i+1)%len(vs)])
		}
		f.Closed = closed
		w := 0.0
		if constant > 0 {
			w = constant * m.PlanScale()
		}
		b.stroke(e, layer, c, f, w)
		return
	}
	// Varying widths: each wide segment is a filled band, zero-width segments stay thin lines.
	color := resolveColor(e.Entity().Color, layer, c)
	for i := 0; i < segs; i++ {
		a, bv := vs[i], vs[(i+1)%len(vs)]
		if a.startWidth <= 0 && a.endWidth <= 0 {
			thin := NewFigure(m.PointXY(a.loc.X, a.loc.Y))
			appendSegment(thin, m, a, bv)
			b.stroke(e, layer, c, thin, 0)
			continue
		}
		b.addFill(&FillPrimitive{Color: color, Figures: []*Figure{band(m, a, bv)}})
	}
}

func appendSegment(f *Figure, m Affine3, a, b polyVertex) {
	end := m.PointXY(b.loc.X, b.loc.Y)
	if math.Abs(a.bulge) < 1e-10 || b.loc.Sub(a.loc).Length() < 1e-12 {
		f.LineTo(end)
		return
	}
	center, radius, start, sweep := BulgeArc(a.loc, b.loc, a.bulge)
	f.ArcTo(m.PointXY(center.X, center.Y), m.VectorXY(radius, 0), m.VectorXY(0, radius), start, start+sweep)
}

// band is the outline of a wide segment whose width changes from start to end.
func band(m Affine3, a, b polyVertex) *Figure {
	var left, right []Vec2
	if math.Abs(a.bulge) < 1e-10 {
		n := b.loc.Sub(a.loc).Normalized().Perp()
		left = append(left, a.loc.Add(n.Mul(a.startWidth/2)), b.loc.Add(n.Mul(a.endWidth/2)))
		right = append(right, a.loc.Sub(n.Mul(a.startWidth/2)), b.loc.Sub(n.Mul(a.endWidth/2)))
	} else {
		center, radius, start, sweep := BulgeArc(a.loc, b.loc, a.bulge)
		steps := max(4, int(math.Ceil(math.Abs(sweep)/(math.Pi/18))))
		for s := 0; s <= steps; s++ {
			fr := float64(s) / float64(steps)
			ang := start + sweep*fr
			half := (a.startWidth + (a.endWidth-a.startWidth)*fr) / 2
			dir := Vec2{math.Cos(ang), math.Sin(ang)}
			left = append(left, center.Add(dir.Mul(radius+half)))
			right = append(right, center.Add(dir.Mul(math.Max(radius-half, 0))))
		}
	}
	f := NewFigure(m.PointXY(left[0].X, left[0].Y))
	for _, p := range left[1:] {
		f.LineTo(m.PointXY(p.X, p.Y))
	}
	for i := len(right) - 1; i >= 0; i-- {
		f.LineTo(m.PointXY(right[i].X, right[i].Y))
	}
	f.Closed = true
	return f
}

func (b *builder) drawPolyfaceMesh(mesh *cadoc.PolyfaceMesh, layer *cadoc.Layer, c drawContext) {
	verts := make([]Vec3, len(mesh.Vertices))
	for i, v := range mesh.Vertices {
		verts[i] = v3(v.Location)
	}
	edges := b.collector(mesh, layer, c)
	for _, face := range mesh.Faces {
		var idx []int
		for _, i := range []int16{face.Index1, face.Index2, face.Index3, face.Index4} {
			if i != 0 {
				idx = append(idx, int(i))
			}
		}
		for i := range idx {
			from, to := idx[i], idx[(i+1)%len(idx)]
			// A negative index hides the edge that starts at that vertex.
			if from < 0 || absI(from) > len(verts) || absI(to) > len(verts) {
				continue
			}
			edges.line(verts[from-1], verts[absI(to)-1])
		}
	}
	edges.flush()
}

func absI(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (b *builder) drawPolygonMesh(mesh *cadoc.PolygonMesh, layer *cadoc.Layer, c drawContext) {
	verts := make([]Vec3, len(mesh.Vertices))
	for i, v := range mesh.Vertices {
		verts[i] = v3(v.Location)
	}
	m, n := int(mesh.MVertexCount), int(mesh.NVertexCount)
	if m <= 0 || n <= 0 || m*n > len(verts) {
		b.skip(mesh.ObjectName())
		return
	}
	edges := b.collector(mesh, layer, c)
	for i := 0; i < m; i++ {
		for j := 0; j < n; j++ {
			if j+1 < n {
				edges.line(verts[i*n+j], verts[i*n+j+1])
			}
			if i+1 < m {
				edges.line(verts[i*n+j], verts[(i+1)*n+j])
			}
		}
	}
	edges.flush()
}

func (b *builder) drawMesh(mesh *cadoc.Mesh, layer *cadoc.Layer, c drawContext) {
	edges := b.collector(mesh, layer, c)
	for _, face := range mesh.Faces {
		for i := range face {
			a, bb := int(face[i]), int(face[(i+1)%len(face)])
			if a >= 0 && bb >= 0 && a < len(mesh.Vertices) && bb < len(mesh.Vertices) {
				edges.line(v3(mesh.Vertices[a]), v3(mesh.Vertices[bb]))
			}
		}
	}
	edges.flush()
}

func (b *builder) drawMLine(ml *cadoc.MLine, layer *cadoc.Layer, c drawContext) {
	// Each element of the style is a polyline through the vertices, offset along their miters.
	elements := 0
	for _, v := range ml.Vertices {
		elements = max(elements, len(v.Segments))
	}
	for el := 0; el < elements; el++ {
		var pts []Vec3
		for _, v := range ml.Vertices {
			if el < len(v.Segments) && len(v.Segments[el].Parameters) > 0 {
				pts = append(pts, v3(v.Position).Add(v3(v.Miter).Mul(v.Segments[el].Parameters[0])))
			}
		}
		if len(pts) >= 2 {
			b.stroke(ml, layer, c, polylineFigure(c.transform, pts, ml.Flags&cadoc.MLineFlagsClosed != 0), 0)
		}
	}
}

// ---- other entities ----

func (b *builder) drawSolid(s *cadoc.Solid, layer *cadoc.Layer, c drawContext) {
	// SOLID / TRACE corners run 1-2-4-3 around the outline; a triangle repeats its third.
	m := c.transform.Multiply(Ocs(v3(s.Normal)))
	f := NewFigure(m.Point(v3(s.FirstCorner)))
	f.LineTo(m.Point(v3(s.SecondCorner)))
	f.LineTo(m.Point(v3(s.FourthCorner)))
	f.LineTo(m.Point(v3(s.ThirdCorner)))
	f.Closed = true
	b.addFill(&FillPrimitive{Color: resolveColor(s.Color, layer, c), Figures: []*Figure{f}})
}

func (b *builder) drawFace(face *cadoc.Face3D, layer *cadoc.Layer, c drawContext) {
	corners := []Vec3{v3(face.FirstCorner), v3(face.SecondCorner), v3(face.ThirdCorner), v3(face.FourthCorner)}
	edges := b.collector(face, layer, c)
	for i := 0; i < 4; i++ {
		if face.Flags&(cadoc.InvisibleEdgeFlags(1)<<i) == 0 {
			edges.line(corners[i], corners[(i+1)%4])
		}
	}
	edges.flush()
}

func (b *builder) drawInfinite(e cadoc.Entity, layer *cadoc.Layer, c drawContext, origin, dir Vec3, ray bool) {
	o := c.transform.Point(origin)
	d := c.transform.Vector(dir)
	if d.Length() < 1e-12 {
		return
	}
	b.add(&InfiniteLinePrimitive{
		Origin: o, Direction: d.Normalized(), IsRay: ray,
		Color: resolveColor(e.Entity().Color, layer, c), WidthMm: resolveWidth(e, layer, c),
		Dashes: b.resolveDashes(e, layer, c, c.transform),
	}, o)
}

func (b *builder) drawLeader(l *cadoc.Leader, layer *cadoc.Layer, c drawContext) {
	if len(l.Vertices) < 2 {
		return
	}
	pts := make([]Vec3, len(l.Vertices))
	for i, v := range l.Vertices {
		pts[i] = v3(v)
	}
	path := pts
	if l.PathType == cadoc.LeaderPathTypeSpline {
		path = ThroughPoints(pts, false)
	}
	b.stroke(l, layer, c, polylineFigure(c.transform, path, false), 0)
	if l.ArrowHeadEnabled {
		size := 0.0
		if s := l.Style; s != nil {
			k := s.ScaleFactor
			if k <= 0 {
				k = 1
			}
			size = s.ArrowSize * k
		}
		b.arrow(resolveColor(l.Color, layer, c), c, pts[0], pts[1], size)
	}
}

func (b *builder) drawMultiLeader(ml *cadoc.MultiLeader, layer *cadoc.Layer, c drawContext) {
	data := ml.ContextData
	if data == nil {
		b.skip(ml.ObjectName())
		return
	}
	lineCtx := c
	lineCtx.blockColor = resolveColor(ml.Color, layer, c)
	lineColor := resolveColor(ml.LineColor, layer, lineCtx)
	arrowSize := data.ArrowheadSize
	if arrowSize <= 0 {
		k := ml.ScaleFactor
		if k <= 0 {
			k = 1
		}
		arrowSize = ml.ArrowheadSize * k
	}
	width := resolveWidth(ml, layer, c)
	for _, root := range data.LeaderRoots {
		for _, line := range root.Lines {
			var pts []Vec3
			for _, p := range line.Points {
				pts = append(pts, v3(p))
			}
			pts = append(pts, v3(root.ConnectionPoint))
			if len(pts) < 2 {
				continue
			}
			b.addStroke(&StrokePrimitive{Color: lineColor, WidthMm: width}, polylineFigure(c.transform, pts, false))
			if arrowSize > 0 {
				b.arrow(lineColor, c, pts[0], pts[1], arrowSize)
			}
		}
		if ml.EnableLanding && root.LandingDistance > 0 {
			start := v3(root.ConnectionPoint)
			end := start.Add(v3(root.Direction).Normalized().Mul(root.LandingDistance))
			b.addStroke(&StrokePrimitive{Color: lineColor, WidthMm: width}, figure2(c.transform.Point(start), c.transform.Point(end)))
		}
	}
	if data.HasTextContents && data.TextLabel != "" {
		textColor := resolveColor(data.TextColor, layer, lineCtx)
		attach := cadoc.AttachmentPointTypeTopLeft
		switch data.TextAttachmentPoint {
		case cadoc.TextAttachmentPointTypeCenter:
			attach = cadoc.AttachmentPointTypeTopCenter
		case cadoc.TextAttachmentPointTypeRight:
			attach = cadoc.AttachmentPointTypeTopRight
		}
		height := data.TextHeight
		if height <= 0 {
			height = 2.5
		}
		style := data.TextStyle
		if style == nil {
			style = ml.TextStyle
		}
		b.drawMTextCore(data.TextLabel, v3(data.TextLocation), v3(data.Direction), v3(data.TextNormal), height, data.BoundaryWidth,
			attach, data.LineSpacingFactor, style, textColor, c)
	} else if data.HasContentsBlock && data.BlockContent != nil && c.depth < maxDepth {
		block := data.BlockContent
		s := v3(data.BlockContentScale)
		if s.X == 0 {
			s.X = 1
		}
		if s.Y == 0 {
			s.Y = 1
		}
		if s.Z == 0 {
			s.Z = 1
		}
		var base Vec3
		if block.BlockEntity != nil {
			base = v3(block.BlockEntity.BasePoint)
		}
		t := c.transform.Multiply(Ocs(v3(data.BlockContentNormal))).Multiply(Translation(v3(data.BlockContentLocation))).
			Multiply(RotationZ(data.BlockContentRotation)).Multiply(Scale3(s.X, s.Y, s.Z)).Multiply(Translation(Vec3{-base.X, -base.Y, -base.Z}))
		cc := b.inherit(ml, layer, c)
		cc.transform = t
		b.drawEntities(block.Entities, cc)
	}
}

// arrow is a filled closed arrowhead at tip pointing away from from.
func (b *builder) arrow(color RGB, c drawContext, tip, from Vec3, size float64) {
	dir := tip.Sub(from)
	l := dir.Length()
	if size <= 0 || l < 1e-12 {
		return
	}
	d := dir.Mul(1 / l)
	side := Cross(Vec3{0, 0, 1}, d).Normalized().Mul(size / 6)
	back := tip.Sub(d.Mul(size))
	m := c.transform
	f := NewFigure(m.Point(tip))
	f.LineTo(m.Point(back.Add(side)))
	f.LineTo(m.Point(back.Sub(side)))
	f.Closed = true
	b.addFill(&FillPrimitive{Color: color, Figures: []*Figure{f}})
}

// ---- properties ----

// effectiveLayer: entities on layer "0" inside a block take on the insert's layer.
func effectiveLayer(e cadoc.Entity, c drawContext) *cadoc.Layer {
	l := e.Entity().Layer
	if c.insertLayer != nil && (l == nil || l.Name == cadoc.DefaultLayerName) {
		return c.insertLayer
	}
	if l != nil {
		return l
	}
	if c.insertLayer != nil {
		return c.insertLayer
	}
	return defaultLayer
}

var defaultLayer = cadoc.NewLayer(cadoc.DefaultLayerName)

// isShown: on, thawed and plottable (the Defpoints layer never plots).
func isShown(l *cadoc.Layer) bool { return l.IsOn && !l.IsFrozen() && l.Plots() }

// resolveColor resolves BYLAYER / BYBLOCK, ACI or true colour. White and
// near-white (ACI 7 is white on AutoCAD's black screen) become black, because
// the page is white.
func resolveColor(col cadoc.Color, layer *cadoc.Layer, c drawContext) RGB {
	if col.IsByBlock() {
		return c.blockColor
	}
	if col.IsByLayer() {
		col = layer.Color
	}
	if col.IsByLayer() || col.IsByBlock() || (!col.IsTrueColor() && col.Index() == 257) {
		return black
	}
	r, g, b := col.RGB()
	if min(r, g, b) >= 224 {
		return black
	}
	return RGB{r, g, b}
}

func resolveWidth(e cadoc.Entity, layer *cadoc.Layer, c drawContext) float64 {
	switch w := e.Entity().LineWeight; w {
	case cadoc.LineWeightByLayer:
		return widthOf(layer.LineWeight, c)
	case cadoc.LineWeightByBlock:
		return c.blockWidthMm
	default:
		return widthOf(w, c)
	}
}

func widthOf(w cadoc.LineWeight, c drawContext) float64 {
	switch {
	case w == cadoc.LineWeightByBlock:
		return c.blockWidthMm
	case w == cadoc.LineWeightW0:
		return thinnestLineWeightMm
	case w > 0 && w <= cadoc.LineWeightW211:
		return math.Max(float64(w)/100, thinnestLineWeightMm)
	}
	return defaultLineWeightMm // Default, ByLayer on a layer, ByDIPs
}

func resolveLineType(e cadoc.Entity, layer *cadoc.Layer, c drawContext) *cadoc.LineType {
	t := e.Entity().LineType
	if t == nil || strings.EqualFold(t.Name, cadoc.ByLayerName) {
		return layer.LineType
	}
	if strings.EqualFold(t.Name, cadoc.ByBlockName) {
		return c.blockLineType
	}
	return t
}

// resolveDashes is the dash pattern in drawing units: segments × LTSCALE ×
// the entity's scale × the block scale. Shapes and text in complex line
// types are left out (their space stays empty).
func (b *builder) resolveDashes(e cadoc.Entity, layer *cadoc.Layer, c drawContext, m Affine3) []float64 {
	t := resolveLineType(e, layer, c)
	if t == nil || !t.IsComplex() {
		return nil
	}
	if len(t.Segments) < 2 {
		return nil
	}
	allPositive := true
	sum := 0.0
	for _, s := range t.Segments {
		if math.IsNaN(s.Length) || math.IsInf(s.Length, 0) {
			return nil
		}
		if s.Length < 0 {
			allPositive = false
		}
		sum += math.Abs(s.Length)
	}
	if allPositive {
		return nil
	}
	es := e.Entity().LineTypeScale
	if !(es > 0) || math.IsInf(es, 0) {
		es = 1
	}
	scale := b.lineTypeScale * es * m.PlanScale()
	if !(scale > 0) || sum*scale <= 0 {
		return nil
	}
	out := make([]float64, len(t.Segments))
	for i, s := range t.Segments {
		out[i] = s.Length * scale
	}
	return out
}

// ---- output ----

func (b *builder) stroke(e cadoc.Entity, layer *cadoc.Layer, c drawContext, f *Figure, worldWidth float64) {
	b.addStroke(&StrokePrimitive{
		Color: resolveColor(e.Entity().Color, layer, c), WidthMm: resolveWidth(e, layer, c),
		WorldWidth: worldWidth, Dashes: b.resolveDashes(e, layer, c, c.transform),
	}, f)
}

func (b *builder) addStroke(p *StrokePrimitive, f *Figure) {
	p.Figures = append(p.Figures, f)
	if b.reserve() {
		f.AddTo(&b.scene.Extents)
		b.scene.Items = append(b.scene.Items, p)
	}
}

func (b *builder) addFill(p *FillPrimitive) {
	if !b.reserve() {
		return
	}
	for _, f := range p.Figures {
		f.AddTo(&b.scene.Extents)
	}
	b.scene.Items = append(b.scene.Items, p)
}

func (b *builder) add(p Primitive, extent Vec2) {
	if !b.reserve() {
		return
	}
	b.scene.Extents.Add(extent)
	b.scene.Items = append(b.scene.Items, p)
}

func (b *builder) reserve() bool {
	if len(b.scene.Items) < maxPrimitives {
		return true
	}
	b.scene.Truncated = true
	return false
}

func (b *builder) skip(kind string) { b.scene.Skipped[kind]++ }

// edgeCollector gathers the edges of a mesh or face into one stroke.
type edgeCollector struct {
	b       *builder
	e       cadoc.Entity
	layer   *cadoc.Layer
	c       drawContext
	figures []*Figure
}

func (b *builder) collector(e cadoc.Entity, layer *cadoc.Layer, c drawContext) *edgeCollector {
	return &edgeCollector{b: b, e: e, layer: layer, c: c}
}

func (ec *edgeCollector) line(a, b Vec3) {
	ec.figures = append(ec.figures, figure2(ec.c.transform.Point(a), ec.c.transform.Point(b)))
}

func (ec *edgeCollector) flush() {
	if len(ec.figures) == 0 || !ec.b.reserve() {
		return
	}
	p := &StrokePrimitive{
		Color: resolveColor(ec.e.Entity().Color, ec.layer, ec.c), WidthMm: resolveWidth(ec.e, ec.layer, ec.c),
		Dashes: ec.b.resolveDashes(ec.e, ec.layer, ec.c, ec.c.transform),
	}
	for _, f := range ec.figures {
		f.AddTo(&ec.b.scene.Extents)
		p.Figures = append(p.Figures, f)
	}
	ec.b.scene.Items = append(ec.b.scene.Items, p)
}

// SkippedSummary formats what was left out, for the log.
func (s *Scene) SkippedSummary() string {
	keys := make([]string, 0, len(s.Skipped))
	for k := range s.Skipped {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s ×%d", k, s.Skipped[k])
	}
	return strings.Join(parts, ", ")
}
