package vector

import (
	"context"
	"errors"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
	"github.com/dseif0x/filee-go/internal/engines/vector/svg"
)

func toDxf(ctx context.Context, step *convert.Step) ([]string, error) {
	doc, err := load(step.InputPath)
	if err != nil {
		return nil, err
	}
	c := collectOutlines(doc)
	if len(c.shapes) == 0 {
		return nil, errors.New("the SVG has no shapes that can be written as CAD geometry (only images or empty groups)")
	}
	return nil, errors.New("DXF writing is not ready")
}

func collectOutlines(doc *svg.Document) *outlineCollector {
	c := newOutlineCollector(doc.Height)
	doc.Draw(c, gfx.Identity, svg.Options{})
	return c
}
