package dxf

import (
	"reflect"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// objectWriters maps object types to the function writing their specific
// groups (after the common object data).
var objectWriters = map[reflect.Type]func(w *writer, o cadoc.Object){}

func registerObjectWriter(sample cadoc.Object, f func(w *writer, o cadoc.Object)) {
	objectWriters[reflect.TypeOf(sample)] = f
}

// enqueue adds an object to the OBJECTS section queue.
func (w *writer) enqueue(o cadoc.Object) {
	if isNilObject(o) {
		return
	}
	w.objects = append(w.objects, o)
}

// objectSupported mirrors isObjectSupported: unknown and some unported
// objects are not written, and dictionary entries pointing to them are left
// out.
func (w *writer) objectSupported(o cadoc.Object) bool {
	switch o.(type) {
	case *cadoc.UnknownObject, *cadoc.Material, *cadoc.VisualStyle, *cadoc.MultiLeaderObjectContextData:
		return false
	case *cadoc.XRecord:
		return !w.cfg.SkipXRecords
	}
	_, ok := objectWriters[reflect.TypeOf(o)]
	return ok
}

func (w *writer) writeObjectsSection() {
	w.section(tokObjects, func() {
		for i := 0; i < len(w.objects); i++ {
			o := w.objects[i]
			if w.written[o] {
				continue
			}
			w.written[o] = true
			w.writeObject(o)
		}
	})
}

func (w *writer) writeObject(obj cadoc.Object) {
	if !w.objectSupported(obj) {
		return
	}
	f := objectWriters[reflect.TypeOf(obj)]
	w.o.str(0, obj.ObjectName())
	w.commonObjectData(obj)
	f(w, obj)
	w.xdata(obj)
}

func init() {
	registerObjectWriter(&cadoc.Dictionary{}, func(w *writer, o cadoc.Object) { w.writeDictionary(o.(*cadoc.Dictionary)) })
	registerObjectWriter(&cadoc.DictionaryWithDefault{}, func(w *writer, o cadoc.Object) {
		d := o.(*cadoc.DictionaryWithDefault)
		w.writeDictionary(&d.Dictionary)
		w.o.str(100, cadoc.SubclassDictionaryWithDefault)
		if d.DefaultEntry != nil && w.objectSupported(d.DefaultEntry) {
			w.o.ref(340, d.DefaultEntry)
			w.enqueue(d.DefaultEntry)
		} else {
			w.o.handle(340, 0)
		}
	})
	registerObjectWriter(&cadoc.DictionaryVariable{}, func(w *writer, o cadoc.Object) {
		v := o.(*cadoc.DictionaryVariable)
		w.o.str(100, cadoc.SubclassDictionaryVariables)
		w.o.integer(280, int64(v.ObjectSchemaNumber))
		w.o.str(1, v.Value)
	})
	registerObjectWriter(&cadoc.PlotSettings{}, func(w *writer, o cadoc.Object) {
		w.writePlotSettings(o.(*cadoc.PlotSettings))
	})
	registerObjectWriter(&cadoc.Layout{}, func(w *writer, o cadoc.Object) { w.writeLayout(o.(*cadoc.Layout)) })
}

func (w *writer) writeDictionary(d *cadoc.Dictionary) {
	o := w.o
	o.str(100, cadoc.SubclassDictionary)
	o.boolean(280, d.HardOwnerFlag)
	o.integer(281, int64(d.ClonningFlags))
	for _, e := range d.Entries() {
		name := *e.ObjectNameKey()
		if name == "" {
			continue
		}
		if sub, ok := e.(*cadoc.Dictionary); ok && sub.Len() == 0 {
			continue
		}
		if name == cadoc.AcadMaterialDictionary {
			continue
		}
		if !w.objectSupported(e) {
			continue
		}
		o.str(3, name)
		if d.HardOwnerFlag {
			o.handle(360, e.Base().Handle)
		} else {
			o.handle(350, e.Base().Handle)
		}
		w.enqueue(e)
	}
}

func (w *writer) writePlotSettings(p *cadoc.PlotSettings) {
	o := w.o
	o.str(100, cadoc.SubclassPlotSettings)
	o.str(1, p.PageName)
	o.str(2, p.SystemPrinterName)
	o.str(4, p.PaperSize)
	o.str(6, p.PlotViewName)
	o.dbl(40, p.UnprintableMargin.Left)
	o.dbl(41, p.UnprintableMargin.Bottom)
	o.dbl(42, p.UnprintableMargin.Right)
	o.dbl(43, p.UnprintableMargin.Top)
	o.dbl(44, p.PaperWidth)
	o.dbl(45, p.PaperHeight)
	o.dbl(46, p.PlotOriginX)
	o.dbl(47, p.PlotOriginY)
	o.dbl(48, p.WindowLowerLeftX)
	o.dbl(49, p.WindowLowerLeftY)
	o.dbl(140, p.WindowUpperLeftX)
	o.dbl(141, p.WindowUpperLeftY)
	num, den := p.NumeratorScale, p.DenominatorScale
	if num <= 0 {
		num = 1
	}
	if den <= 0 {
		den = 1
	}
	o.dbl(142, num)
	o.dbl(143, den)
	o.integer(70, int64(p.Flags))
	o.integer(72, int64(p.PaperUnits))
	o.integer(73, int64(p.PaperRotation))
	o.integer(74, int64(p.PlotType))
	o.str(7, p.StyleSheet)
	o.integer(75, int64(p.ScaledFit))
	o.integer(76, int64(p.ShadePlotMode))
	o.integer(77, int64(p.ShadePlotResolutionMode))
	dpi := p.ShadePlotDPI
	if dpi < 100 {
		dpi = 300
	}
	o.integer(78, int64(dpi))
	o.dbl(147, num/den)
	o.dbl(148, p.PaperImageOrigin.X)
	o.dbl(149, p.PaperImageOrigin.Y)
}

func (w *writer) writeLayout(l *cadoc.Layout) {
	o := w.o
	w.writePlotSettings(&l.PlotSettings)
	o.str(100, cadoc.SubclassLayout)
	o.str(1, l.Name)
	o.integer(70, int64(l.LayoutFlags))
	o.integer(71, int64(l.TabOrder))
	o.vec2(10, l.MinLimits)
	o.vec2(11, l.MaxLimits)
	o.vec3(12, l.InsertionBasePoint)
	o.vec3(14, l.MinExtents)
	o.vec3(15, l.MaxExtents)
	o.dbl(146, l.Elevation)
	o.vec3(13, l.Origin)
	o.vec3(16, l.XAxis)
	o.vec3(17, l.YAxis)
	o.integer(76, int64(l.UcsOrthographicType))
	var block cadoc.Object
	if l.AssociatedBlock != nil {
		block = l.AssociatedBlock
	}
	o.ref(330, block)
	if l.LastActiveViewport != nil && w.known(l.LastActiveViewport) {
		o.handle(331, l.LastActiveViewport.Handle)
	}
	if l.UCS != nil && w.known(l.UCS) {
		o.handle(345, l.UCS.Handle)
	}
	if l.BaseUCS != nil && w.known(l.BaseUCS) {
		o.handle(346, l.BaseUCS.Handle)
	}
}
