package dxf

import (
	"reflect"
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

func (w *writer) writeBlocks() {
	w.section(tokBlocks, func() {
		for _, r := range w.doc.BlockRecords.Entries() {
			w.writeBlockBegin(r)
			if r.IsModelSpace() || strings.EqualFold(r.Name, cadoc.PaperSpaceName) {
				w.entities = append(w.entities, r.Entities...)
			} else {
				for _, e := range r.Entities {
					w.writeEntity(e)
				}
			}
			w.writeBlockEnd(r)
		}
	})
}

func (w *writer) writeBlockBegin(r *cadoc.BlockRecord) {
	b := r.BlockEntity
	if b == nil {
		b = cadoc.NewBlock()
		b.Owner = r
		r.BlockEntity = b
	}
	o := w.o
	o.str(0, cadoc.TokenBlock)
	w.commonObjectData(b)
	w.commonEntityData(b)
	o.str(100, cadoc.SubclassBlockBegin)
	o.str(2, r.Name)
	o.integer(70, int64(b.Flags))
	o.vec3(10, b.BasePoint)
	o.str(3, r.Name)
	o.str(1, b.XRefPath)
	if b.Comments != "" {
		o.str(4, b.Comments)
	}
	w.xdata(b)
}

func (w *writer) writeBlockEnd(r *cadoc.BlockRecord) {
	e := r.BlockEnd
	if e == nil {
		e = cadoc.NewBlockEnd()
		e.Owner = r
		r.BlockEnd = e
	}
	o := w.o
	o.str(0, cadoc.TokenEndBlock)
	w.commonObjectData(e)
	o.str(100, cadoc.SubclassEntity)
	if r.Layout != nil && r.Layout.IsPaperSpace() {
		o.integer(67, 1)
	}
	o.str(8, e.LayerName())
	o.str(100, cadoc.SubclassBlockEnd)
	w.xdata(e)
}

func (w *writer) writeEntitiesSection() {
	w.section(tokEntities, func() {
		for _, e := range w.entities {
			w.writeEntity(e)
		}
	})
}

// entityWriters maps a model type to the function writing its specific
// groups (after the common object and entity data).
var entityWriters = map[reflect.Type]func(w *writer, e cadoc.Entity){}

func registerEntityWriter(sample cadoc.Entity, f func(w *writer, e cadoc.Entity)) {
	entityWriters[reflect.TypeOf(sample)] = f
}

// entitySupported mirrors isEntitySupported: some entities are not written.
func (w *writer) entitySupported(e cadoc.Entity) bool {
	ok := w.entityWritable(e)
	if !ok {
		switch e.(type) {
		case *cadoc.TableEntity, *cadoc.Surface:
			w.note(LevelNotImplemented, "Entity type not implemented %s", e.ObjectName())
		default:
			if d, isDim := e.(cadoc.Dimension); isDim && d.Dimension().Block == nil {
				w.note(LevelWarning, "Dimension %X without a block, left out", e.Base().Handle)
			}
		}
	}
	return ok
}

// entityWritable reports whether a block-level entity is written.
func (w *writer) entityWritable(e cadoc.Entity) bool {
	switch x := e.(type) {
	case *cadoc.Seqend, *cadoc.VertexFaceRecord, *cadoc.Vertex2D, *cadoc.Vertex3D, *cadoc.VertexFaceMesh,
		*cadoc.PolygonMeshVertex:
		// Written by their owner.
		return false
	case *cadoc.UnknownEntity:
		return w.cfg.WriteUnknownEntities && x.DxfName != ""
	case *cadoc.Shape:
		return w.cfg.WriteShapes
	case *cadoc.TableEntity, *cadoc.Surface:
		return false
	case *cadoc.Solid3D, *cadoc.CadBody, *cadoc.Region:
		return w.version >= cadoc.AC1014
	}
	if d, ok := e.(cadoc.Dimension); ok && d.Dimension().Block == nil {
		return false
	}
	if _, ok := entityWriters[reflect.TypeOf(e)]; !ok {
		return false
	}
	return true
}

func (w *writer) writeEntity(e cadoc.Entity) {
	if !w.entitySupported(e) {
		return
	}
	w.writeEntityData(e)
}

// writeEntityData writes an entity without the support check (used for
// owned entities like vertices and attributes).
func (w *writer) writeEntityData(e cadoc.Entity) {
	f, ok := entityWriters[reflect.TypeOf(e)]
	if !ok {
		w.note(LevelNotImplemented, "Entity not implemented %s", e.ObjectName())
		return
	}
	w.o.str(0, e.ObjectName())
	w.commonObjectData(e)
	w.commonEntityData(e)
	f(w, e)
	w.xdata(e)
}

func (w *writer) writeSeqend(s *cadoc.Seqend, owner cadoc.Entity) {
	if s == nil {
		s = cadoc.NewSeqend()
		s.Owner = owner
		s.Layer = owner.Entity().Layer
	}
	o := w.o
	o.str(0, cadoc.TokenEntitySeqend)
	h := s.Handle
	if h == 0 {
		h = w.doc.Header.HandleSeed
		w.doc.Header.HandleSeed++
		s.Handle = h
	}
	o.handle(5, h)
	o.ref(330, owner)
	o.str(100, cadoc.SubclassEntity)
	o.str(8, s.LayerName())
}
