// Where the pictures of a PDF are drawn and how large, for the compressor:
// the content streams of the pages, of the forms they draw and of annotation
// appearances are read for "cm" and "Do". A picture's largest drawn size
// decides how far it may be scaled down; pictures whose drawing can't be
// followed are taken as large as the page lets them be.

package pdf

import (
	"context"
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const (
	maxFormDepth = 12
	maxForms     = 20_000
)

// pictureUse is the largest size, in inches, a picture is drawn at.
type pictureUse struct{ width, height float64 }

// matrix is the part of a transformation that sizes things.
type matrix struct{ a, b, c, d float64 }

var identity = matrix{1, 0, 0, 1}

// then is m applied first, then outer.
func (m matrix) then(o matrix) matrix {
	return matrix{m.a*o.a + m.b*o.c, m.a*o.b + m.b*o.d, m.c*o.a + m.d*o.c, m.c*o.b + m.d*o.d}
}

type useFinder struct {
	pc     *pdfContext
	drawn  map[int]pictureUse
	found  map[int]bool
	unsure map[int]bool
	forms  int
}

func findPictureUses(ctx context.Context, pc *pdfContext) map[int]pictureUse {
	u := &useFinder{pc: pc, drawn: map[int]pictureUse{}, found: map[int]bool{}, unsure: map[int]bool{}}
	var pageW, pageH float64
	for n := 1; n <= pc.PageCount; n++ {
		d, _, inh, err := pc.PageDict(ctx, n, false)
		if err != nil || d == nil {
			continue
		}
		var res types.Dict
		if inh != nil {
			res = inh.Resources
			if inh.MediaBox != nil {
				pageW = max(pageW, inh.MediaBox.Width())
				pageH = max(pageH, inh.MediaBox.Height())
			}
		}
		u.collect(res)
		content, err := pc.PageContent(d, n)
		if err != nil {
			u.markUnsure(res)
		} else {
			u.walk(content, res, identity, 0)
		}
		u.walkAnnotations(d, res)
	}
	pw := max(pageW, 1) / pointsPerInch
	ph := max(pageH, 1) / pointsPerInch
	out := map[int]pictureUse{}
	for nr := range u.found {
		sd := u.stream(nr)
		if sd == nil {
			continue
		}
		w := float64(max(1, intEntry(pc, sd.Dict, "Width")))
		h := float64(max(1, intEntry(pc, sd.Dict, "Height")))
		dpi := min(max(w/pw, h/ph), max(w/ph, h/pw))
		use, ok := u.drawn[nr]
		if !ok || u.unsure[nr] {
			use = pictureUse{max(use.width, w/dpi), max(use.height, h/dpi)}
		}
		out[nr] = use
	}
	return out
}

func (u *useFinder) stream(nr int) *types.StreamDict {
	e := u.pc.Table[nr]
	if e == nil {
		return nil
	}
	if sd, ok := e.Object.(types.StreamDict); ok {
		return &sd
	}
	return nil
}

// xobjects lists a resource dictionary's XObjects: name → object number.
func (u *useFinder) xobjects(res types.Dict) map[string]int {
	if res == nil {
		return nil
	}
	o, err := u.pc.Dereference(res["XObject"])
	if err != nil {
		return nil
	}
	d, ok := o.(types.Dict)
	if !ok {
		return nil
	}
	out := map[string]int{}
	for k, v := range d {
		if ref, ok := v.(types.IndirectRef); ok {
			out[k] = ref.ObjectNumber.Value()
		}
	}
	return out
}

func (u *useFinder) isImage(nr int) bool {
	sd := u.stream(nr)
	if sd == nil {
		return false
	}
	s := sd.NameEntry("Subtype")
	return s != nil && *s == "Image"
}

func (u *useFinder) collect(res types.Dict) {
	for _, nr := range u.xobjects(res) {
		if u.isImage(nr) {
			u.found[nr] = true
		}
	}
}

func (u *useFinder) markUnsure(res types.Dict) {
	for _, nr := range u.xobjects(res) {
		u.unsure[nr] = true
	}
}

func (u *useFinder) walk(content []byte, res types.Dict, start matrix, depth int) {
	var saved []matrix
	current := start
	xo := u.xobjects(res)
	scanContent(content, func(op contentOp) bool {
		switch op.Op {
		case "q":
			saved = append(saved, current)
		case "Q":
			if len(saved) > 0 {
				current = saved[len(saved)-1]
				saved = saved[:len(saved)-1]
			}
		case "cm":
			if len(op.Operands) == 6 {
				m := matrix{operandNumber(op.Operands[0]), operandNumber(op.Operands[1]), operandNumber(op.Operands[2]), operandNumber(op.Operands[3])}
				current = m.then(current)
			}
		case "Do":
			if len(op.Operands) == 1 {
				if n, ok := op.Operands[0].(name); ok {
					if nr, ok := xo[string(n)]; ok {
						u.draw(nr, res, current, depth)
					}
				}
			}
		}
		return true
	})
}

func (u *useFinder) draw(nr int, res types.Dict, ctm matrix, depth int) {
	sd := u.stream(nr)
	if sd == nil {
		return
	}
	s := sd.NameEntry("Subtype")
	if s == nil {
		return
	}
	switch *s {
	case "Image":
		w := math.Hypot(ctm.a, ctm.b) / pointsPerInch
		h := math.Hypot(ctm.c, ctm.d) / pointsPerInch
		u.found[nr] = true
		known := u.drawn[nr]
		u.drawn[nr] = pictureUse{max(known.width, w), max(known.height, h)}
	case "Form":
		u.walkForm(sd, res, ctm, depth+1)
	}
}

func (u *useFinder) formResources(sd *types.StreamDict, inherited types.Dict) types.Dict {
	if o, err := u.pc.Dereference(sd.Dict["Resources"]); err == nil {
		if d, ok := o.(types.Dict); ok {
			return d
		}
	}
	return inherited
}

func (u *useFinder) walkForm(sd *types.StreamDict, inherited types.Dict, parent matrix, depth int) {
	res := u.formResources(sd, inherited)
	u.forms++
	if depth > maxFormDepth || u.forms > maxForms {
		u.markUnsure(res)
		return
	}
	u.collect(res)
	c := *sd
	if err := c.Decode(); err != nil {
		u.markUnsure(res)
		return
	}
	u.walk(c.Content, res, formMatrix(u.pc, sd).then(parent), depth)
}

func formMatrix(pc *pdfContext, sd *types.StreamDict) matrix {
	a := arrayNumbers(pc, sd.Dict["Matrix"])
	if len(a) == 6 {
		return matrix{a[0], a[1], a[2], a[3]}
	}
	return identity
}

func arrayNumbers(pc *pdfContext, o types.Object) []float64 {
	o, _ = pc.Dereference(o)
	arr, ok := o.(types.Array)
	if !ok {
		return nil
	}
	out := make([]float64, 0, len(arr))
	for _, v := range arr {
		v, _ = pc.Dereference(v)
		switch n := v.(type) {
		case types.Integer:
			out = append(out, float64(n))
		case types.Float:
			out = append(out, float64(n))
		default:
			out = append(out, 0)
		}
	}
	return out
}

// walkAnnotations follows annotation appearances (stamps, signatures, form
// fields): the form's box, transformed by its matrix, fitted into the
// annotation's rectangle.
func (u *useFinder) walkAnnotations(page types.Dict, res types.Dict) {
	o, _ := u.pc.Dereference(page["Annots"])
	annots, ok := o.(types.Array)
	if !ok {
		return
	}
	for _, item := range annots {
		a, _ := u.pc.Dereference(item)
		annot, ok := a.(types.Dict)
		if !ok {
			continue
		}
		apo, _ := u.pc.Dereference(annot["AP"])
		ap, ok := apo.(types.Dict)
		rect := arrayNumbers(u.pc, annot["Rect"])
		if !ok || len(rect) != 4 {
			continue
		}
		rw, rh := math.Abs(rect[2]-rect[0]), math.Abs(rect[3]-rect[1])
		for _, key := range []string{"N", "R", "D"} {
			if ref, ok := ap[key].(types.IndirectRef); ok {
				if sd := u.stream(ref.ObjectNumber.Value()); sd != nil {
					u.walkAppearance(sd, res, rw, rh)
					continue
				}
			}
			so, _ := u.pc.Dereference(ap[key])
			if states, ok := so.(types.Dict); ok {
				for _, v := range states {
					if ref, ok := v.(types.IndirectRef); ok {
						if sd := u.stream(ref.ObjectNumber.Value()); sd != nil {
							u.walkAppearance(sd, res, rw, rh)
						}
					}
				}
			}
		}
	}
}

func (u *useFinder) walkAppearance(sd *types.StreamDict, res types.Dict, rw, rh float64) {
	m := formMatrix(u.pc, sd)
	var bw, bh float64
	if box := arrayNumbers(u.pc, sd.Dict["BBox"]); len(box) == 4 {
		w, h := math.Abs(box[2]-box[0]), math.Abs(box[3]-box[1])
		bw = math.Abs(m.a)*w + math.Abs(m.c)*h
		bh = math.Abs(m.b)*w + math.Abs(m.d)*h
	}
	fit := identity
	if bw > 0 && bh > 0 {
		fit = matrix{rw / bw, 0, 0, rh / bh}
	}
	u.walkForm(sd, res, fit, 1)
}
