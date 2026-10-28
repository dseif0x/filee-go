package dxf

import (
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

func (r *reader) readBlocks() {
	s := r.s
	s.next()
	for !s.eof && !(s.code == 0 && s.str == tokEndSection) {
		if s.code == 0 && s.str == cadoc.TokenBlock {
			r.guard("a block", r.readBlock)
			if !(s.code == 0 && (s.str == cadoc.TokenBlock || s.str == tokEndSection)) {
				// Skip to the next block after an error.
				for !s.eof && !(s.code == 0 && (s.str == cadoc.TokenBlock || s.str == tokEndSection)) {
					s.next()
				}
			}
			continue
		}
		r.warn("Unexpected token at the BLOCKS table: %s", s.str)
		s.next()
		for !r.at0() {
			s.next()
		}
	}
}

func (r *reader) readBlock() {
	s := r.s
	blk := cadoc.NewBlock()
	st := &entityState{objectState: objectState{obj: blk}}
	var name string
	var record *cadoc.BlockRecord
	r.subclass = ""
	s.next()
	for !r.at0() {
		switch s.code {
		case 2, 3:
			name = s.str
			if strings.EqualFold(name, "$MODEL_SPACE") {
				name = cadoc.ModelSpaceName
			} else if strings.EqualFold(name, "$PAPER_SPACE") {
				name = cadoc.PaperSpaceName
			}
			if record == nil {
				if rec, ok := r.doc.BlockRecords.Get(name); ok {
					record = rec
				}
			}
		case 330:
			h := s.handle()
			st.owner, st.hasOwner = h, true
			if record == nil {
				if rec, ok := r.object(h).(*cadoc.BlockRecord); ok {
					record = rec
				}
			}
		case 1:
			blk.XRefPath = s.str
		case 4:
			blk.Comments = s.str
		case 70:
			blk.Flags = cadoc.BlockTypeFlags(s.i16())
		case 71:
			blk.IsUnloaded = s.bool()
		case 10, 20, 30:
			setVec3(&blk.BasePoint, s.code, s.float())
		default:
			r.commonEntityCode(st)
		}
		if r.hold {
			r.hold = false
			continue
		}
		s.next()
	}
	if record == nil {
		if name == "" {
			r.warn("Block without a name, discarded")
			for !s.eof && !(s.code == 0 && s.str == cadoc.TokenEndBlock) {
				s.next()
			}
			r.readBlockEnd(cadoc.NewBlockEnd())
			return
		}
		record = cadoc.NewBlockRecord(name)
		if err := r.doc.BlockRecords.Add(record); err != nil {
			r.warn("Block %s: %v", name, err)
		}
	}
	// The record's flags live on its BLOCK entity.
	record.BlockEntity = blk
	blk.Owner = record
	r.finishEntity(st)

	for !s.eof && !(s.code == 0 && (s.str == cadoc.TokenEndBlock || s.str == tokEndSection || s.str == tokEOF)) {
		if s.code != 0 {
			s.next()
			continue
		}
		var e cadoc.Entity
		var est *entityState
		ok := r.guard("an entity of block "+record.Name, func() { e, est = r.readEntity() })
		if !ok || e == nil {
			continue
		}
		target := record
		if est.hasOwner && est.owner != 0 {
			if rec, ok := r.object(est.owner).(*cadoc.BlockRecord); ok && rec != record {
				target = rec
			}
		}
		target.AddEntity(e)
	}
	if s.code == 0 && s.str == cadoc.TokenEndBlock {
		end := cadoc.NewBlockEnd()
		r.readBlockEnd(end)
		record.BlockEnd = end
		end.Owner = record
	}
}

func (r *reader) readBlockEnd(end *cadoc.BlockEnd) {
	s := r.s
	st := &entityState{objectState: objectState{obj: end}}
	s.next()
	for !r.at0() {
		r.commonEntityCode(st)
		if r.hold {
			r.hold = false
			continue
		}
		s.next()
	}
	r.finishEntity(st)
}

func (r *reader) readEntitiesSection() {
	s := r.s
	s.next()
	for !s.eof && !(s.code == 0 && s.str == tokEndSection) {
		if s.code != 0 {
			s.next()
			continue
		}
		var e cadoc.Entity
		var est *entityState
		ok := r.guard("an entity", func() { e, est = r.readEntity() })
		if !ok || e == nil {
			continue
		}
		r.placed = append(r.placed, placedEntity{e: e, owner: est.owner, hasOwner: est.hasOwner, paper: est.paperSpace})
	}
}
