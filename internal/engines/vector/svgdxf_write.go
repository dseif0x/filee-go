package vector

import (
	"context"
	"errors"
	"math"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/cad"
	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
	"github.com/dseif0x/filee-go/internal/engines/vector/svg"
)

func toDxf(ctx context.Context, step *convert.Step) ([]string, error) {
	doc, err := load(step.InputPath)
	if err != nil {
		return nil, err
	}
	c := collectOutlines(doc)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if len(c.shapes) == 0 {
		return nil, errors.New("the SVG has no shapes that can be written as CAD geometry (only images or empty groups)")
	}
	path := step.Output.Allocate("dxf", "")
	if path == "" {
		return nil, nil
	}
	if _, err := cad.WriteDocument(outlineDocument(c.shapes), path, "dxf", step.Preset.Cad.Version, nil); err != nil {
		return nil, err
	}
	return []string{path}, nil
}

func collectOutlines(doc *svg.Document) *outlineCollector {
	c := newOutlineCollector(doc.Height)
	doc.Draw(c, gfx.Identity, svg.Options{})
	return c
}

// outlineDocument is a drawing in millimetres ($INSUNITS 4) holding the shapes.
func outlineDocument(shapes []dxfShape) *cadoc.Document {
	doc := cadoc.NewDocument()
	doc.Header.InsUnits = cadoc.UnitsTypeMillimeters
	doc.Header.MeasurementUnits = cadoc.MeasurementUnitsMetric
	ms := doc.ModelSpace()
	v3 := func(p gfx.Point) cadoc.Vec3 { return cadoc.Vec3{X: p.X, Y: p.Y} }
	for _, s := range shapes {
		color := cadoc.ColorRGB(s.R, s.G, s.B)
		switch s.Kind {
		case dxfCircle:
			e := cadoc.NewCircle()
			e.Center, e.Radius, e.Color = v3(s.Center), s.Radius, color
			ms.AddEntity(e)
		case dxfEllipse:
			e := cadoc.NewEllipse()
			e.Center, e.MajorAxisEndPoint, e.RadiusRatio = v3(s.Center), v3(s.Major), s.Ratio
			e.StartParameter, e.EndParameter, e.Color = 0, 2*math.Pi, color
			ms.AddEntity(e)
		case dxfLine:
			e := cadoc.NewLine()
			e.StartPoint, e.EndPoint, e.Color = v3(s.Points[0]), v3(s.Points[1]), color
			ms.AddEntity(e)
		default:
			e := cadoc.NewLwPolyline()
			for _, p := range s.Points {
				e.Vertices = append(e.Vertices, cadoc.LwPolylineVertex{Location: cadoc.Vec2{X: p.X, Y: p.Y}})
			}
			if s.Closed {
				e.Flags |= cadoc.LwPolylineFlagsClosed
			}
			e.Color = color
			ms.AddEntity(e)
		}
	}
	return doc
}
