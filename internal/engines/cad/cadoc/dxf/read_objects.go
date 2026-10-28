package dxf

import (
	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// objectKinds maps DXF object names to a constructor and group parser, like
// entityKinds. The constructor may queue st.afterRead work (run after the
// groups) and reference fix-ups with r.later.
var objectKinds = map[string]func(r *reader, st *objectState) (cadoc.Object, func() bool){}

func registerObject(name string, f func(r *reader, st *objectState) (cadoc.Object, func() bool)) {
	objectKinds[name] = f
}

// dictionaryEntries holds a dictionary's entry names and handles until all
// objects are read.
type dictionaryEntry struct {
	name   string
	handle uint64
}

func (r *reader) readObjectsSection() {
	s := r.s
	s.next()
	for !s.eof && !(s.code == 0 && s.str == tokEndSection) {
		if s.code != 0 {
			s.next()
			continue
		}
		r.guard("an object", r.readObject)
	}
}

func (r *reader) readObject() {
	s := r.s
	name := s.str
	ctor, ok := objectKinds[name]
	st := &objectState{}
	var parse func() bool
	if ok {
		st.obj, parse = ctor(r, st)
	} else {
		u := &cadoc.UnknownObject{DxfName: name}
		u.Class, _ = r.doc.Classes.Get(name)
		if u.Class != nil {
			r.note(LevelNotImplemented, "NonGraphicalObject not supported read as an UnknownNonGraphicalObject: %s", name)
		} else {
			r.note(LevelNotImplemented, "UnknownNonGraphicalObject not supported: %s", name)
		}
		st.obj = u
		parse = func() bool {
			switch s.code {
			case 5, 102, 1001:
				return false
			case 330:
				if !st.hasOwner {
					return false
				}
			case 100:
				r.subclass = s.str
			}
			u.Groups = append(u.Groups, cadoc.GroupCode{Code: s.code, Value: s.value()})
			return true
		}
	}
	r.subclass = ""
	r.hold = false
	s.next()
	for !r.at0() {
		if !parse() && !r.hold {
			r.commonObjectCode(st)
		}
		if r.hold {
			r.hold = false
			continue
		}
		s.next()
	}
	if u, ok := st.obj.(*cadoc.UnknownObject); ok && !r.cfg.KeepUnknownObjects {
		// Keep the handle known so references don't warn, but drop the object.
		_ = u
		return
	}
	r.finishObject(st)
	r.placeObject(st)
}

// placeObject records the owner of a top-level dictionary so the root can be
// found.
func (r *reader) placeObject(st *objectState) {
	if d, ok := st.obj.(*cadoc.Dictionary); ok && r.doc.RootDictionary == nil && (!st.hasOwner || st.owner == 0) {
		r.doc.RootDictionary = d
		if d.Name == "" {
			d.Name = cadoc.RootDictionaryName
		}
	}
}

// dictionaryParser reads the AcDbDictionary groups into d; entries are bound
// after reading.
func (r *reader) dictionaryParser(d *cadoc.Dictionary) func() bool {
	var entries []dictionaryEntry
	r.fixDicts = append(r.fixDicts, func() {
		for _, e := range entries {
			o := r.object(e.handle)
			ng, ok := o.(cadoc.NonGraphicalObject)
			if !ok {
				if o == nil && e.handle != 0 {
					r.note(LevelNone, "Entry not found %s|%X for dictionary %s|%X", e.name, e.handle, d.Name, d.Handle)
				}
				continue
			}
			key := ng.ObjectNameKey()
			if *key == "" {
				*key = e.name
			}
			if err := d.Add(ng); err != nil {
				r.note(LevelError, "Error when trying to add the entry %s to %s|%X", *key, d.Name, d.Handle)
			}
		}
	})
	return func() bool {
		s := r.s
		switch s.code {
		case 280:
			d.HardOwnerFlag = s.bool()
		case 281:
			d.ClonningFlags = cadoc.DictionaryCloningFlags(s.i16())
		case 3:
			entries = append(entries, dictionaryEntry{name: s.str})
		case 350, 360:
			if len(entries) > 0 {
				entries[len(entries)-1].handle = s.handle()
			}
		default:
			return false
		}
		return true
	}
}

func init() {
	registerObject(cadoc.TokenObjectDictionary, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		d := cadoc.NewDictionary()
		d.ClonningFlags = 0
		return d, r.dictionaryParser(d)
	})
	registerObject(cadoc.TokenObjectDictionaryWithDefault, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		d := cadoc.NewDictionaryWithDefault("")
		d.ClonningFlags = 0
		parse := r.dictionaryParser(&d.Dictionary)
		return d, func() bool {
			if r.s.code == 340 {
				r.handleRef(r.s.handle(), func(o cadoc.Object) { d.DefaultEntry = o })
				return true
			}
			return parse()
		}
	})
	registerObject(cadoc.TokenObjectDictionaryVar, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		v := &cadoc.DictionaryVariable{}
		return v, func() bool {
			s := r.s
			switch s.code {
			case 1:
				v.Value = s.str
			case 280:
				v.ObjectSchemaNumber = s.i32()
			default:
				return false
			}
			return true
		}
	})
	registerObject(cadoc.TokenObjectPlotSettings, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		p := &cadoc.PlotSettings{}
		p.InitPlotSettings()
		return p, func() bool { return r.parsePlotSettings(p) }
	})
	registerObject(cadoc.TokenObjectLayout, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		l := cadoc.NewLayout("")
		return l, func() bool { return r.parseLayout(l, st) }
	})
}

func (r *reader) parsePlotSettings(p *cadoc.PlotSettings) bool {
	s := r.s
	switch s.code {
	case 1:
		p.PageName = s.str
	case 2:
		p.SystemPrinterName = s.str
	case 4:
		p.PaperSize = s.str
	case 6:
		p.PlotViewName = s.str
	case 7:
		p.StyleSheet = s.str
	case 40:
		p.UnprintableMargin.Left = s.float()
	case 41:
		p.UnprintableMargin.Bottom = s.float()
	case 42:
		p.UnprintableMargin.Right = s.float()
	case 43:
		p.UnprintableMargin.Top = s.float()
	case 44:
		p.PaperWidth = s.float()
	case 45:
		p.PaperHeight = s.float()
	case 46:
		p.PlotOriginX = s.float()
	case 47:
		p.PlotOriginY = s.float()
	case 48:
		p.WindowLowerLeftX = s.float()
	case 49:
		p.WindowLowerLeftY = s.float()
	case 140:
		p.WindowUpperLeftX = s.float()
	case 141:
		p.WindowUpperLeftY = s.float()
	case 142:
		if v := s.float(); v > 0 {
			p.NumeratorScale = v
		}
	case 143:
		if v := s.float(); v > 0 {
			p.DenominatorScale = v
		}
	case 70:
		p.Flags = cadoc.PlotFlags(s.i16())
	case 72:
		p.PaperUnits = cadoc.PlotPaperUnits(s.i16())
	case 73:
		p.PaperRotation = cadoc.PlotRotation(s.i16())
	case 74:
		p.PlotType = cadoc.PlotType(s.i16())
	case 75:
		p.ScaledFit = cadoc.ScaledType(s.i16())
	case 76:
		p.ShadePlotMode = cadoc.ShadePlotMode(s.i16())
	case 77:
		p.ShadePlotResolutionMode = cadoc.ShadePlotResolutionMode(s.i16())
	case 78:
		if v := s.i16(); v >= 100 {
			p.ShadePlotDPI = v
		}
	case 147:
		p.StandardScale = s.float()
	case 148:
		p.PaperImageOrigin.X = s.float()
	case 149:
		p.PaperImageOrigin.Y = s.float()
	case 333:
		p.ShadePlotIDHandle = s.handle()
	default:
		return false
	}
	return true
}

func (r *reader) parseLayout(l *cadoc.Layout, st *objectState) bool {
	s := r.s
	if r.subclass != cadoc.SubclassLayout {
		if s.code == 330 && st.hasOwner {
			// A second 330 before AcDbLayout is unusual; treat it as the block.
		} else {
			return r.parsePlotSettings(&l.PlotSettings)
		}
	}
	switch s.code {
	case 1:
		l.Name = s.str
	case 70:
		l.LayoutFlags = cadoc.LayoutFlags(s.i16())
	case 71:
		l.TabOrder = s.i32()
	case 10, 20:
		setVec2(&l.MinLimits, s.code, s.float())
	case 11, 21:
		setVec2(&l.MaxLimits, s.code, s.float())
	case 12, 22, 32:
		setVec3(&l.InsertionBasePoint, s.code, s.float())
	case 13, 23, 33:
		setVec3(&l.Origin, s.code, s.float())
	case 14, 24, 34:
		setVec3(&l.MinExtents, s.code, s.float())
	case 15, 25, 35:
		setVec3(&l.MaxExtents, s.code, s.float())
	case 16, 26, 36:
		setVec3(&l.XAxis, s.code, s.float())
	case 17, 27, 37:
		setVec3(&l.YAxis, s.code, s.float())
	case 146:
		l.Elevation = s.float()
	case 76:
		l.UcsOrthographicType = cadoc.OrthographicType(s.i16())
	case 330:
		if !st.hasOwner {
			return false
		}
		r.handleRef(s.handle(), func(o cadoc.Object) {
			if b, ok := o.(*cadoc.BlockRecord); ok {
				l.SetBlock(b)
			}
		})
	case 331:
		r.handleRef(s.handle(), func(o cadoc.Object) { l.LastActiveViewport, _ = o.(*cadoc.Viewport) })
	case 345:
		r.handleRef(s.handle(), func(o cadoc.Object) { l.UCS, _ = o.(*cadoc.UCS) })
	case 346:
		r.handleRef(s.handle(), func(o cadoc.Object) { l.BaseUCS, _ = o.(*cadoc.UCS) })
	default:
		return false
	}
	return true
}
