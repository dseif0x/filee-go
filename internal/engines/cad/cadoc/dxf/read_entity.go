package dxf

import (
	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// entityState collects an entity's common data while it is read.
type entityState struct {
	objectState
	layer      string
	ltype      string
	paperSpace bool
	material   uint64
	bookColor  string
	// afterRead runs once the entity's own groups are read, with the reader
	// on the next code 0 (used to read VERTEX/ATTRIB/SEQEND children).
	afterRead []func()
}

// parseFunc handles one group of a specific entity and reports whether it
// used it. It may consume more groups and set r.hold.
type parseFunc func(st *entityState) bool

// entityKinds maps DXF entity names to a constructor of the model entity and
// its group parser. The constructor may queue st.afterRead work.
var entityKinds = map[string]func(r *reader, st *entityState) (cadoc.Entity, parseFunc){}

func registerEntity(name string, f func(r *reader, st *entityState) (cadoc.Entity, parseFunc)) {
	entityKinds[name] = f
}

// readEntity reads the entity at the current code 0 and returns it (nil
// when it is dropped). Afterwards the reader is on the next code 0.
func (r *reader) readEntity() (cadoc.Entity, *entityState) {
	s := r.s
	name := s.str
	ctor, ok := entityKinds[name]
	if !ok {
		return r.readUnknownEntity(name)
	}
	st := &entityState{}
	e, parse := ctor(r, st)
	st.obj = e
	r.readEntityGroups(st, parse)
	for _, f := range st.afterRead {
		f()
	}
	if st.obj == nil {
		return nil, st
	}
	r.finishEntity(st)
	return st.obj.(cadoc.Entity), st
}

// readEntityGroups reads groups up to the next code 0, giving each to parse
// first and to the common entity handling otherwise.
func (r *reader) readEntityGroups(st *entityState, parse parseFunc) {
	s := r.s
	r.subclass = ""
	r.hold = false
	s.next()
	for !r.at0() {
		if parse == nil || !parse(st) {
			if !r.hold {
				r.commonEntityCode(st)
			}
		}
		if r.hold {
			r.hold = false
			continue
		}
		s.next()
	}
}

// commonEntityCode handles AcDbEntity groups and the common object groups.
func (r *reader) commonEntityCode(st *entityState) bool {
	s := r.s
	e := st.obj.(cadoc.Entity).Entity()
	switch s.code {
	case 8:
		st.layer = s.str
	case 6:
		st.ltype = s.str
	case 62:
		if !e.Color.IsTrueColor() {
			e.Color = cadoc.ColorIndex(s.i16())
		}
	case 420:
		e.Color = cadoc.ColorFromTrueColor(s.i32())
	case 430:
		st.bookColor = s.str
	case 440:
		e.Transparency = cadoc.TransparencyFromAlpha(s.i32())
	case 370:
		e.LineWeight = cadoc.LineWeight(s.i16())
	case 48:
		e.LineTypeScale = s.float()
	case 60:
		e.IsInvisible = s.bool()
	case 67:
		st.paperSpace = s.bool()
	case 92, 160:
		if r.subclass != "" && r.subclass != cadoc.SubclassEntity {
			return false
		}
		r.readProxyGraphics(e)
	case 310:
		e.ProxyGraphics = append(e.ProxyGraphics, s.chunk...)
	case 347:
		st.material = s.handle()
	case 284, 390, 348, 380:
		// Shadow mode, plot style handle, visual style, plot style type.
	default:
		return r.commonObjectCode(&st.objectState)
	}
	return true
}

func (r *reader) readProxyGraphics(e *cadoc.EntityBase) {
	s := r.s
	n := s.int()
	if n == 0 {
		return
	}
	s.next()
	for s.code == 310 && !s.eof {
		e.ProxyGraphics = append(e.ProxyGraphics, s.chunk...)
		s.next()
	}
	r.hold = true
}

// finishEntity registers the entity and queues its table references.
func (r *reader) finishEntity(st *entityState) {
	r.finishObject(&st.objectState)
	e := st.obj.(cadoc.Entity).Entity()
	layer, ltype, mat, book := st.layer, st.ltype, st.material, st.bookColor
	r.later(func() {
		if layer != "" {
			if l, ok := r.doc.Layers.Get(layer); ok {
				e.Layer = l
			} else {
				r.warn("Layer %s not found in the LAYER table, created for %s with handle %X", layer, st.obj.ObjectName(), e.Handle)
				l = cadoc.NewLayer(layer)
				l.LineType, _ = r.doc.LineTypes.Get(cadoc.ContinuousName)
				_ = r.doc.Layers.Add(l)
				e.Layer = l
			}
		}
		if ltype != "" {
			if lt, ok := r.doc.LineTypes.Get(ltype); ok {
				e.LineType = lt
			} else {
				r.warn("Line type %s not found for %s with handle %X", ltype, st.obj.ObjectName(), e.Handle)
			}
		}
		if mat != 0 {
			e.Material, _ = r.object(mat).(*cadoc.Material)
		}
		if book != "" {
			for _, c := range r.doc.Colors() {
				if c.Name == book {
					e.BookColor = c
				}
			}
		}
	})
}

// readUnknownEntity keeps (or skips) an entity type without a model.
func (r *reader) readUnknownEntity(name string) (cadoc.Entity, *entityState) {
	s := r.s
	cls, known := r.doc.Classes.Get(name)
	if known {
		r.note(LevelNotImplemented, "Entity not supported read as an UnknownEntity: %s", name)
	} else {
		r.note(LevelNotImplemented, "Entity not supported: %s", name)
	}
	u := &cadoc.UnknownEntity{DxfName: name, Class: cls}
	u.InitEntity()
	st := &entityState{objectState: objectState{obj: u}}
	r.readEntityGroups(st, func(st *entityState) bool {
		switch s.code {
		case 5, 330, 102, 8, 6, 62, 420, 440, 370, 48, 60, 67, 1001:
			if r.subclass == "" || r.subclass == cadoc.SubclassEntity || s.code == 1001 || s.code == 5 || s.code == 102 {
				return false
			}
		case 100:
			r.subclass = s.str
			if s.str == cadoc.SubclassEntity {
				return true
			}
		}
		u.Groups = append(u.Groups, cadoc.GroupCode{Code: s.code, Value: s.value()})
		return true
	})
	if !r.cfg.KeepUnknownEntities {
		return nil, st
	}
	r.finishEntity(st)
	return u, st
}

// handleRef queues the resolution of a handle reference.
func (r *reader) handleRef(h uint64, set func(cadoc.Object)) {
	if h == 0 {
		return
	}
	r.later(func() {
		if o := r.object(h); o != nil {
			set(o)
		}
	})
}
