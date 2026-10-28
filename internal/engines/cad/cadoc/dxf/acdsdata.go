package dxf

import (
	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// ACDSDATA holds the ACIS (SAB) data of 3D solids, regions and bodies in
// AC1027+ files (DxfAcdsDataSectionReader / DxfAcdsDataSectionWriter).
const (
	acdsRecord  = "ACDSRECORD"
	acdsSchema  = "ACDSSCHEMA"
	acdsID      = "AcDbDs::ID"
	acdsASMData = "ASM_Data"
)

type modeler interface {
	cadoc.Entity
	Modeler() *cadoc.ModelerGeometry
}

// readAcdsData reads the ACDSDATA section and attaches the ASM data of each
// record to the entity with the record's handle.
func (r *reader) readAcdsData() {
	s := r.s
	s.next()
	for !s.eof && !(s.code == 0 && s.str == tokEndSection) {
		if !(s.code == 0 && s.str == acdsRecord) {
			s.next()
			continue
		}
		var handle uint64
		var data []byte
		column := ""
		s.next()
		for !s.eof && s.code != 0 {
			switch s.code {
			case 2:
				column = s.str
			case 320:
				if column == acdsID {
					handle = s.handle()
				}
			case 310:
				if column == acdsASMData {
					data = append(data, s.chunk...)
				}
			}
			s.next()
		}
		if handle != 0 && len(data) > 0 {
			h, d := handle, data
			r.later(func() {
				if m, ok := r.object(h).(modeler); ok {
					m.Modeler().AcisData = d
				} else {
					r.warn("ModelerGeometry with handle %X not found in the document", h)
				}
			})
		}
	}
}

// writeAcdsData writes the ASM data of modeler geometries (AC1027+).
func (w *writer) writeAcdsData() {
	if w.version <= cadoc.AC1024 {
		return
	}
	var geometries []modeler
	seen := map[cadoc.Object]bool{}
	for _, r := range w.doc.BlockRecords.Entries() {
		for _, e := range r.Entities {
			if m, ok := e.(modeler); ok && len(m.Modeler().AcisData) > 0 && !seen[e] && w.entitySupported(e) {
				seen[e] = true
				geometries = append(geometries, m)
			}
		}
	}
	if len(geometries) == 0 {
		return
	}
	o := w.o
	w.section(tokAcdsData, func() {
		o.integer(70, 2)
		o.integer(71, 8)
		type prop struct {
			name  string
			typ   int64
			flags int64
		}
		schema := func(index int64, name string, props ...prop) {
			o.str(0, acdsSchema)
			o.integer(90, index)
			o.str(1, name)
			for _, p := range props {
				o.str(2, p.name)
				o.integer(280, p.typ)
				o.integer(91, p.flags)
			}
		}
		schema(0, "AcDb_Thumbnail_Schema", prop{acdsID, 10, 8}, prop{"Thumbnail_Data", 15, 0}, prop{"AcDbDs::TreatedAsObjectData", 1, 1})
		schema(1, "AcDbDs::TreatedAsObjectDataSchema", prop{"AcDbDs::TreatedAsObjectData", 1, 0})
		schema(2, "AcDbDs::LegacySchema", prop{"AcDbDs::Legacy", 1, 0})
		schema(3, "AcDbDs::IndexedPropertySchema", prop{"AcDs:Indexable", 1, 0})
		schema(4, "AcDbDs::HandleAttributeSchema", prop{"AcDbDs::HandleAttribute", 1, 0})
		schema(5, "AcDb3DSolid_ASM_Data", prop{acdsID, 10, 8}, prop{acdsASMData, 15, 0})
		for _, g := range geometries {
			data := g.Modeler().AcisData
			o.str(0, acdsRecord)
			o.integer(90, 5)
			o.str(2, acdsID)
			o.integer(280, 10)
			o.handle(320, g.Base().Handle)
			o.str(2, acdsASMData)
			o.integer(280, 15)
			o.integer(94, int64(len(data)))
			o.chunk(310, data)
		}
	})
}
