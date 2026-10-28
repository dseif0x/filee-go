package dwg

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// docDiff collects the differences between a document read from DWG and
// one read from the DXF of the same drawing.
type docDiff struct {
	t     *testing.T
	name  string
	diffs []string
	// r14 compares an R14 file with a 2000 DXF: names are upper case with
	// the characters R14 does not allow replaced, there are no line weights.
	r14 bool
}

// norm maps a name to what R14 stores for it.
func (d *docDiff) norm(name string) string {
	if !d.r14 {
		return name
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-', r == '$', r == '*':
			return r
		}
		return '_'
	}, name)
}

func (d *docDiff) add(format string, args ...any) {
	d.diffs = append(d.diffs, fmt.Sprintf(format, args...))
}

func (d *docDiff) report(max int) {
	d.t.Helper()
	for i, s := range d.diffs {
		if i == max {
			d.t.Errorf("%s: … %d more differences", d.name, len(d.diffs)-max)
			break
		}
		d.t.Errorf("%s: %s", d.name, s)
	}
}

func typeCounts(entities []cadoc.Entity) map[string]int {
	m := map[string]int{}
	for _, e := range entities {
		m[entityKind(e)]++
	}
	return m
}

func entityKind(e cadoc.Entity) string {
	return strings.TrimPrefix(reflect.TypeOf(e).String(), "*cadoc.")
}

func countsString(m map[string]int) string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s:%d ", k, m[k])
	}
	return b.String()
}

func near(a, b float64) bool {
	if math.IsNaN(a) && math.IsNaN(b) {
		return true
	}
	return math.Abs(a-b) <= 1e-9*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
}

func nearVec(a, b cadoc.Vec3) bool { return near(a.X, b.X) && near(a.Y, b.Y) && near(a.Z, b.Z) }

func nearVec2(a, b cadoc.Vec2) bool { return near(a.X, b.X) && near(a.Y, b.Y) }

// compareDocs compares the structure (tables, blocks, entity counts) and
// the geometry of the entities found in both documents by handle.
func compareDocs(d *docDiff, dwg, dxf *cadoc.Document) {
	// Layers: names, colours, on/frozen, line types.
	for _, l := range dxf.Layers.Entries() {
		w, ok := dwg.Layers.Get(d.norm(l.Name))
		if !ok {
			d.add("layer %q missing", l.Name)
			continue
		}
		if w.Color != l.Color || w.IsOn != l.IsOn || w.IsFrozen() != l.IsFrozen() {
			d.add("layer %q: color %v/%v on %v/%v frozen %v/%v", l.Name, w.Color, l.Color, w.IsOn, l.IsOn, w.IsFrozen(), l.IsFrozen())
		}
		if !strings.EqualFold(w.LineTypeName(), d.norm(l.LineTypeName())) {
			d.add("layer %q: line type %q/%q", l.Name, w.LineTypeName(), l.LineTypeName())
		}
		if w.LineWeight != l.LineWeight && !d.r14 {
			d.add("layer %q: line weight %d/%d", l.Name, w.LineWeight, l.LineWeight)
		}
	}
	if dwg.Layers.Len() != dxf.Layers.Len() {
		d.add("layer count %d/%d", dwg.Layers.Len(), dxf.Layers.Len())
	}
	for _, lt := range dxf.LineTypes.Entries() {
		w, ok := dwg.LineTypes.Get(d.norm(lt.Name))
		if !ok {
			d.add("line type %q missing", lt.Name)
			continue
		}
		if len(w.Segments) != len(lt.Segments) || !near(w.PatternLength(), lt.PatternLength()) {
			d.add("line type %q: %d/%d segments", lt.Name, len(w.Segments), len(lt.Segments))
		}
	}
	for _, s := range dxf.TextStyles.Entries() {
		w, ok := dwg.TextStyles.Get(d.norm(s.Name))
		if !ok && d.r14 && s.IsShapeFile() {
			continue
		}
		if !ok {
			d.add("text style %q missing", s.Name)
			continue
		}
		if !strings.EqualFold(w.Filename, s.Filename) || !near(w.Height, s.Height) || !near(w.Width, s.Width) {
			d.add("text style %q: %q/%q %v/%v", s.Name, w.Filename, s.Filename, w.Height, s.Height)
		}
	}
	for _, s := range dxf.DimensionStyles.Entries() {
		w, ok := dwg.DimensionStyles.Get(d.norm(s.Name))
		if !ok {
			d.add("dimension style %q missing", s.Name)
			continue
		}
		if !near(w.TextHeight, s.TextHeight) || !near(w.ArrowSize, s.ArrowSize) || !near(w.ScaleFactor, s.ScaleFactor) {
			d.add("dimension style %q: text height %v/%v arrow %v/%v", s.Name, w.TextHeight, s.TextHeight, w.ArrowSize, s.ArrowSize)
		}
	}

	// Blocks: names and the entities of each block. Anonymous blocks are
	// matched by handle: their numbers are given by the application.
	byHandle := map[uint64]*cadoc.BlockRecord{}
	for _, r := range dwg.BlockRecords.Entries() {
		byHandle[r.Handle] = r
	}
	for _, r := range dxf.BlockRecords.Entries() {
		w, ok := dwg.BlockRecords.Get(d.norm(r.Name))
		if !ok && r.IsAnonymous() {
			w, ok = byHandle[r.Handle]
			if ok && !w.IsAnonymous() {
				ok = false
			}
		}
		if !ok {
			d.add("block %q missing", r.Name)
			continue
		}
		wc, xc := typeCounts(w.Entities), typeCounts(r.Entities)
		if !reflect.DeepEqual(wc, xc) {
			d.add("block %q entities\n  dwg %s\n  dxf %s", r.Name, countsString(wc), countsString(xc))
		}
		if !nearVec(w.BlockEntity.BasePoint, r.BlockEntity.BasePoint) {
			d.add("block %q base point %v/%v", r.Name, w.BlockEntity.BasePoint, r.BlockEntity.BasePoint)
		}
	}
	if dwg.BlockRecords.Len() != dxf.BlockRecords.Len() {
		d.add("block count %d/%d", dwg.BlockRecords.Len(), dxf.BlockRecords.Len())
	}

	// Entities by handle; the DXF files of the samples were saved later
	// and have objects the DWG files lack (above their handle seed).
	index := dwg.Index()
	seed := dwg.Header.HandleSeed
	n := 0
	dxf.Walk(func(o cadoc.Object) {
		x, ok := o.(cadoc.Entity)
		if !ok || x.Base().Handle == 0 {
			return
		}
		wo, ok := index[x.Base().Handle]
		if !ok && (x.Base().Handle >= seed || d.r14) {
			// R14 regenerates the anonymous blocks with new handles.
			return
		}
		if !ok {
			if _, unknown := x.(*cadoc.UnknownEntity); !unknown {
				d.add("entity %s %X missing", entityKind(x), x.Base().Handle)
			}
			return
		}
		w, ok := wo.(cadoc.Entity)
		if !ok || entityKind(w) != entityKind(x) {
			d.add("entity %X: %T/%T", x.Base().Handle, wo, x)
			return
		}
		n++
		compareEntity(d, w, x)
	})
	if n == 0 {
		d.add("no entity matched by handle")
	}
}

func compareEntity(d *docDiff, w, x cadoc.Entity) {
	h := x.Base().Handle
	we, xe := w.Entity(), x.Entity()
	bad := func(what string, a, b any) { d.add("%s %X %s: %v / %v", entityKind(x), h, what, a, b) }
	if we.LayerName() != d.norm(xe.LayerName()) {
		bad("layer", we.LayerName(), xe.LayerName())
	}
	if we.Color != xe.Color {
		bad("color", we.Color, xe.Color)
	}
	if !strings.EqualFold(we.LineTypeName(), d.norm(xe.LineTypeName())) {
		bad("line type", we.LineTypeName(), xe.LineTypeName())
	}
	if !near(we.LineTypeScale, xe.LineTypeScale) {
		bad("line type scale", we.LineTypeScale, xe.LineTypeScale)
	}
	if we.LineWeight != xe.LineWeight && !d.r14 {
		bad("line weight", we.LineWeight, xe.LineWeight)
	}
	if we.IsInvisible != xe.IsInvisible {
		bad("invisible", we.IsInvisible, xe.IsInvisible)
	}
	if ownerHandle(w) != ownerHandle(x) {
		bad("owner", owner(w), owner(x))
	}
	switch a := w.(type) {
	case *cadoc.Line:
		b := x.(*cadoc.Line)
		if !nearVec(a.StartPoint, b.StartPoint) || !nearVec(a.EndPoint, b.EndPoint) || !near(a.Thickness, b.Thickness) || !nearVec(a.Normal, b.Normal) {
			bad("geometry", fmt.Sprint(a.StartPoint, a.EndPoint), fmt.Sprint(b.StartPoint, b.EndPoint))
		}
	case *cadoc.Circle:
		b := x.(*cadoc.Circle)
		if !nearVec(a.Center, b.Center) || !near(a.Radius, b.Radius) || !nearVec(a.Normal, b.Normal) {
			bad("geometry", fmt.Sprint(a.Center, a.Radius), fmt.Sprint(b.Center, b.Radius))
		}
	case *cadoc.Arc:
		b := x.(*cadoc.Arc)
		if !nearVec(a.Center, b.Center) || !near(a.Radius, b.Radius) || !near(a.StartAngle, b.StartAngle) || !near(a.EndAngle, b.EndAngle) {
			bad("geometry", fmt.Sprint(a.Center, a.Radius, a.StartAngle, a.EndAngle), fmt.Sprint(b.Center, b.Radius, b.StartAngle, b.EndAngle))
		}
	case *cadoc.Ellipse:
		b := x.(*cadoc.Ellipse)
		if !nearVec(a.Center, b.Center) || !nearVec(a.MajorAxisEndPoint, b.MajorAxisEndPoint) || !near(a.RadiusRatio, b.RadiusRatio) ||
			!near(a.StartParameter, b.StartParameter) || !near(a.EndParameter, b.EndParameter) {
			bad("geometry", *a, *b)
		}
	case *cadoc.Point:
		b := x.(*cadoc.Point)
		if !nearVec(a.Location, b.Location) {
			bad("location", a.Location, b.Location)
		}
	case *cadoc.TextEntity:
		compareText(d, h, a, x.(*cadoc.TextEntity))
	case *cadoc.AttributeEntity:
		b := x.(*cadoc.AttributeEntity)
		compareText(d, h, &a.TextEntity, &b.TextEntity)
		if a.Tag != b.Tag {
			bad("tag", a.Tag, b.Tag)
		}
	case *cadoc.AttributeDefinition:
		b := x.(*cadoc.AttributeDefinition)
		compareText(d, h, &a.TextEntity, &b.TextEntity)
		if a.Tag != b.Tag || a.Prompt != b.Prompt {
			bad("tag", a.Tag+"|"+a.Prompt, b.Tag+"|"+b.Prompt)
		}
	case *cadoc.MText:
		b := x.(*cadoc.MText)
		if a.Value != b.Value || !nearVec(a.InsertPoint, b.InsertPoint) || !near(a.Height, b.Height) || !near(a.RectangleWidth, b.RectangleWidth) {
			bad("mtext", fmt.Sprintf("%q %v %v", a.Value, a.InsertPoint, a.Height), fmt.Sprintf("%q %v %v", b.Value, b.InsertPoint, b.Height))
		}
		if styleName(a.Style) != d.norm(styleName(b.Style)) {
			bad("style", styleName(a.Style), styleName(b.Style))
		}
	case *cadoc.Insert:
		b := x.(*cadoc.Insert)
		if !nearVec(a.InsertPoint, b.InsertPoint) || !near(a.XScale, b.XScale) || !near(a.YScale, b.YScale) || !near(a.Rotation, b.Rotation) {
			bad("insert", fmt.Sprint(a.InsertPoint, a.XScale, a.YScale, a.Rotation), fmt.Sprint(b.InsertPoint, b.XScale, b.YScale, b.Rotation))
		}
		if blockName(a.Block) != d.norm(blockName(b.Block)) && !a.Block.IsAnonymous() || len(a.Attributes) != len(b.Attributes) || (a.AttributesSeqend == nil) != (b.AttributesSeqend == nil) {
			bad("insert block", fmt.Sprint(blockName(a.Block), len(a.Attributes)), fmt.Sprint(blockName(b.Block), len(b.Attributes)))
		}
		if a.RowCount != b.RowCount || a.ColumnCount != b.ColumnCount {
			bad("insert array", fmt.Sprint(a.RowCount, a.ColumnCount), fmt.Sprint(b.RowCount, b.ColumnCount))
		}
	case *cadoc.LwPolyline:
		b := x.(*cadoc.LwPolyline)
		if len(a.Vertices) != len(b.Vertices) || a.IsClosed() != b.IsClosed() {
			bad("vertices", len(a.Vertices), len(b.Vertices))
			break
		}
		for i := range a.Vertices {
			va, vb := a.Vertices[i], b.Vertices[i]
			if !nearVec2(va.Location, vb.Location) || !near(va.Bulge, vb.Bulge) || !near(va.StartWidth, vb.StartWidth) {
				bad(fmt.Sprintf("vertex %d", i), va, vb)
			}
		}
	case cadoc.Polyline:
		b := x.(cadoc.Polyline)
		va, vb := a.VertexEntities(), b.VertexEntities()
		if len(va) != len(vb) || a.Polyline().Flags != b.Polyline().Flags || (a.Polyline().Seqend == nil) != (b.Polyline().Seqend == nil) {
			bad("vertices", fmt.Sprint(len(va), a.Polyline().Flags), fmt.Sprint(len(vb), b.Polyline().Flags))
			break
		}
		for i := range va {
			if v, ok := va[i].(cadoc.Vertex); ok {
				if !nearVec(v.Vertex().Location, vb[i].(cadoc.Vertex).Vertex().Location) {
					bad(fmt.Sprintf("vertex %d", i), v.Vertex().Location, vb[i].(cadoc.Vertex).Vertex().Location)
				}
			}
		}
	case *cadoc.Spline:
		b := x.(*cadoc.Spline)
		// DWG keeps fit points only for fit splines and has no planar or
		// linear flags.
		const m = cadoc.SplineFlagsClosed | cadoc.SplineFlagsPeriodic | cadoc.SplineFlagsRational
		if len(a.ControlPoints) == 0 && len(a.FitPoints) > 0 {
			// A fit spline: AutoCAD writes the computed control points to DXF.
			break
		}
		if a.Degree != b.Degree || len(a.ControlPoints) != len(b.ControlPoints) || len(a.Knots) != len(b.Knots) ||
			len(a.FitPoints) > 0 && len(a.FitPoints) != len(b.FitPoints) || a.Flags&m != b.Flags&m {
			bad("spline", fmt.Sprint(a.Degree, len(a.ControlPoints), len(a.Knots), len(a.FitPoints), a.Flags),
				fmt.Sprint(b.Degree, len(b.ControlPoints), len(b.Knots), len(b.FitPoints), b.Flags))
			break
		}
		for i := range a.ControlPoints {
			if !nearVec(a.ControlPoints[i], b.ControlPoints[i]) {
				bad("control point", a.ControlPoints[i], b.ControlPoints[i])
			}
		}
	case *cadoc.Hatch:
		b := x.(*cadoc.Hatch)
		if len(a.Paths) != len(b.Paths) || a.Pattern.Name != b.Pattern.Name || a.IsSolid != b.IsSolid || len(a.Pattern.Lines) != len(b.Pattern.Lines) {
			bad("hatch", fmt.Sprint(len(a.Paths), a.Pattern.Name, a.IsSolid), fmt.Sprint(len(b.Paths), b.Pattern.Name, b.IsSolid))
			break
		}
		for i := range a.Paths {
			if len(a.Paths[i].Edges) != len(b.Paths[i].Edges) || len(a.Paths[i].Entities) != len(b.Paths[i].Entities) {
				bad(fmt.Sprintf("path %d", i), len(a.Paths[i].Edges), len(b.Paths[i].Edges))
			}
		}
	case cadoc.Dimension:
		b := x.(cadoc.Dimension)
		da, db := a.Dimension(), b.Dimension()
		if !nearVec(da.DefinitionPoint, db.DefinitionPoint) || !nearVec(da.TextMiddlePoint, db.TextMiddlePoint) || da.Text != db.Text ||
			da.Flags != db.Flags {
			bad("dimension", fmt.Sprint(da.DefinitionPoint, da.TextMiddlePoint, da.Text, da.Flags), fmt.Sprint(db.DefinitionPoint, db.TextMiddlePoint, db.Text, db.Flags))
		}
		if da.Block == nil || db.Block == nil || da.Block.Handle != db.Block.Handle {
			bad("dimension block", blockName(da.Block), blockName(db.Block))
		}
		if da.Style != nil && db.Style != nil && da.Style.Name != d.norm(db.Style.Name) {
			bad("dimension style", da.Style.Name, db.Style.Name)
		}
	case *cadoc.Solid:
		b := x.(*cadoc.Solid)
		if !nearVec(a.FirstCorner, b.FirstCorner) || !nearVec(a.FourthCorner, b.FourthCorner) {
			bad("corners", a.FirstCorner, b.FirstCorner)
		}
	case *cadoc.Face3D:
		b := x.(*cadoc.Face3D)
		if !nearVec(a.FirstCorner, b.FirstCorner) || !nearVec(a.ThirdCorner, b.ThirdCorner) || a.Flags != b.Flags {
			bad("corners", a.FirstCorner, b.FirstCorner)
		}
	case *cadoc.Viewport:
		b := x.(*cadoc.Viewport)
		if !nearVec(a.Center, b.Center) || !near(a.Width, b.Width) || !near(a.Height, b.Height) || a.Id != b.Id {
			bad("viewport", fmt.Sprint(a.Center, a.Width, a.Height, a.Id), fmt.Sprint(b.Center, b.Width, b.Height, b.Id))
		}
	case *cadoc.Leader:
		b := x.(*cadoc.Leader)
		// DXF adds the end point of the hook line as a vertex.
		if n := len(b.Vertices) - len(a.Vertices); n != 0 && n != 1 {
			bad("vertices", len(a.Vertices), len(b.Vertices))
		}
		for i := range a.Vertices {
			j := i
			if i == len(a.Vertices)-1 {
				j = len(b.Vertices) - 1
			}
			if !nearVec(a.Vertices[i], b.Vertices[j]) {
				bad("vertex", a.Vertices[i], b.Vertices[j])
			}
		}
	case *cadoc.MultiLeader:
		b := x.(*cadoc.MultiLeader)
		ca, cb := a.ContextData, b.ContextData
		if len(ca.LeaderRoots) != len(cb.LeaderRoots) || ca.TextLabel != cb.TextLabel || !nearVec(ca.TextLocation, cb.TextLocation) {
			bad("context", fmt.Sprint(len(ca.LeaderRoots), ca.TextLabel, ca.TextLocation), fmt.Sprint(len(cb.LeaderRoots), cb.TextLabel, cb.TextLocation))
		}
	case *cadoc.MLine:
		b := x.(*cadoc.MLine)
		if len(a.Vertices) != len(b.Vertices) || !nearVec(a.StartPoint, b.StartPoint) {
			bad("mline", len(a.Vertices), len(b.Vertices))
		}
	case *cadoc.Mesh:
		b := x.(*cadoc.Mesh)
		if len(a.Vertices) != len(b.Vertices) || len(a.Faces) != len(b.Faces) || len(a.Edges) != len(b.Edges) {
			bad("mesh", fmt.Sprint(len(a.Vertices), len(a.Faces), len(a.Edges)), fmt.Sprint(len(b.Vertices), len(b.Faces), len(b.Edges)))
		}
	case *cadoc.RasterImage:
		b := x.(*cadoc.RasterImage)
		if !nearVec(a.InsertPoint, b.InsertPoint) || !nearVec2(a.Size, b.Size) || (a.Definition == nil) != (b.Definition == nil) {
			bad("image", fmt.Sprint(a.InsertPoint, a.Size), fmt.Sprint(b.InsertPoint, b.Size))
		}
	case *cadoc.Shape:
		b := x.(*cadoc.Shape)
		if !nearVec(a.InsertionPoint, b.InsertionPoint) || a.ShapeStyle == nil {
			bad("shape", fmt.Sprint(a.InsertionPoint, styleName(a.ShapeStyle)), fmt.Sprint(b.InsertionPoint, styleName(b.ShapeStyle)))
		}
	case *cadoc.Tolerance:
		b := x.(*cadoc.Tolerance)
		if a.Text != b.Text || !nearVec(a.InsertionPoint, b.InsertionPoint) {
			bad("tolerance", a.Text, b.Text)
		}
	}
}

func compareText(d *docDiff, h uint64, a, b *cadoc.TextEntity) {
	if a.Value != b.Value || !nearVec(a.InsertPoint, b.InsertPoint) || !near(a.Height, b.Height) || !near(a.Rotation, b.Rotation) ||
		!near(a.WidthFactor, b.WidthFactor) || a.HorizontalAlignment != b.HorizontalAlignment || a.VerticalAlignment != b.VerticalAlignment {
		d.add("%s %X text: %q %v %v %v / %q %v %v %v", a.ObjectName(), h, a.Value, a.InsertPoint, a.Height, a.Rotation, b.Value, b.InsertPoint, b.Height, b.Rotation)
	}
	if a.HorizontalAlignment != 0 || a.VerticalAlignment != 0 {
		if !nearVec(a.AlignmentPoint, b.AlignmentPoint) {
			d.add("%s %X alignment point %v / %v", a.ObjectName(), h, a.AlignmentPoint, b.AlignmentPoint)
		}
	}
	if styleName(a.Style) != d.norm(styleName(b.Style)) {
		d.add("%s %X style %s / %s", a.ObjectName(), h, styleName(a.Style), styleName(b.Style))
	}
}

func styleName(s *cadoc.TextStyle) string {
	if s == nil {
		return "<nil>"
	}
	return s.Name
}

func blockName(r *cadoc.BlockRecord) string {
	if r == nil {
		return "<nil>"
	}
	return r.Name
}

func ownerHandle(e cadoc.Entity) uint64 {
	if o := e.Base().Owner; o != nil {
		return o.Base().Handle
	}
	return 0
}

func owner(e cadoc.Entity) string {
	switch o := e.Base().Owner.(type) {
	case nil:
		return "<nil>"
	case *cadoc.BlockRecord:
		return o.Name
	default:
		return fmt.Sprintf("%s %X", o.ObjectName(), o.Base().Handle)
	}
}
