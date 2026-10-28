package cadoc

import "strings"

// BlockRecord is a BLOCK_RECORD table entry: a block definition with its
// entities. Model space and paper space layouts are block records too.
type BlockRecord struct {
	TableEntryBase
	// BlockEntity is the BLOCK entity (base point, flags, xref path).
	BlockEntity *Block
	// BlockEnd is the ENDBLK entity.
	BlockEnd *BlockEnd
	// Entities are the entities of the block in drawing order. Use
	// AddEntity/RemoveEntity to keep owners right; plain slice edits are
	// fine for writers, which derive ownership from the structure.
	Entities []Entity
	// Layout is the layout of a model or paper space block, nil otherwise.
	Layout       *Layout
	Units        UnitsType
	IsExplodable bool
	CanScale     bool
	Preview      []byte
}

// NewBlockRecord returns an empty block record with its BLOCK and ENDBLK.
func NewBlockRecord(name string) *BlockRecord {
	r := &BlockRecord{TableEntryBase: TableEntryBase{Name: name}, CanScale: true, IsExplodable: true}
	r.BlockEntity = NewBlock()
	r.BlockEntity.Owner = r
	r.BlockEnd = NewBlockEnd()
	r.BlockEnd.Owner = r
	return r
}

func (*BlockRecord) ObjectName() string     { return TokenTableBlockRecord }
func (*BlockRecord) SubclassMarker() string { return SubclassBlockRecord }
func (*BlockRecord) ObjectType() ObjectType { return ObjectTypeBlockHeader }

// EntryFlags are the block flags, stored on the BLOCK entity as in ACadSharp.
func (r *BlockRecord) EntryFlags() int16 { return int16(r.Flags()) }

func (r *BlockRecord) SetEntryFlags(f int16) { r.SetFlags(BlockTypeFlags(f)) }

// Flags returns the BLOCK flags.
func (r *BlockRecord) Flags() BlockTypeFlags {
	if r.BlockEntity == nil {
		return 0
	}
	return r.BlockEntity.Flags
}

// SetFlags sets the BLOCK flags.
func (r *BlockRecord) SetFlags(f BlockTypeFlags) {
	if r.BlockEntity == nil {
		r.BlockEntity = NewBlock()
		r.BlockEntity.Owner = r
	}
	r.BlockEntity.Flags = f
}

// IsAnonymous reports the anonymous flag.
func (r *BlockRecord) IsAnonymous() bool { return r.Flags()&BlockTypeFlagsAnonymous != 0 }

// IsModelSpace / IsPaperSpace compare the name with *Model_Space and
// *Paper_Space (any *Paper_SpaceN counts as paper space).
func (r *BlockRecord) IsModelSpace() bool { return strings.EqualFold(r.Name, ModelSpaceName) }
func (r *BlockRecord) IsPaperSpace() bool {
	return len(r.Name) >= len(PaperSpaceName) && strings.EqualFold(r.Name[:len(PaperSpaceName)], PaperSpaceName)
}

// AddEntity appends an entity and makes the record its owner.
func (r *BlockRecord) AddEntity(e Entity) {
	e.Base().Owner = r
	r.Entities = append(r.Entities, e)
}

// RemoveEntity removes an entity; false when it isn't in the block.
func (r *BlockRecord) RemoveEntity(e Entity) bool {
	for i, x := range r.Entities {
		if x == e {
			r.Entities = append(r.Entities[:i], r.Entities[i+1:]...)
			if e.Base().Owner == Object(r) {
				e.Base().Owner = nil
			}
			return true
		}
	}
	return false
}

// RemoveEntities removes all entities for which drop returns true and returns
// how many were removed.
func (r *BlockRecord) RemoveEntities(drop func(Entity) bool) int {
	kept := r.Entities[:0]
	n := 0
	for _, e := range r.Entities {
		if drop(e) {
			n++
			if e.Base().Owner == Object(r) {
				e.Base().Owner = nil
			}
			continue
		}
		kept = append(kept, e)
	}
	clear(r.Entities[len(kept):])
	r.Entities = kept
	return n
}

// AttributeDefinitions returns the ATTDEF entities of the block.
func (r *BlockRecord) AttributeDefinitions() []*AttributeDefinition {
	var defs []*AttributeDefinition
	for _, e := range r.Entities {
		if d, ok := e.(*AttributeDefinition); ok {
			defs = append(defs, d)
		}
	}
	return defs
}

// HasAttributes reports whether the block defines attributes.
func (r *BlockRecord) HasAttributes() bool { return len(r.AttributeDefinitions()) > 0 }

// Viewports returns the VIEWPORT entities of the block.
func (r *BlockRecord) Viewports() []*Viewport {
	var v []*Viewport
	for _, e := range r.Entities {
		if vp, ok := e.(*Viewport); ok {
			v = append(v, vp)
		}
	}
	return v
}

// SortEntitiesTable returns the ACAD_SORTENTS table of the extension
// dictionary, or nil.
func (r *BlockRecord) SortEntitiesTable() *SortEntitiesTable {
	if r.XDictionary == nil {
		return nil
	}
	if o, ok := r.XDictionary.Get(SortEntsDictionaryName); ok {
		t, _ := o.(*SortEntitiesTable)
		return t
	}
	return nil
}

// Block is the BLOCK entity that starts a block definition.
type Block struct {
	EntityBase
	BasePoint  Vec3
	XRefPath   string
	Comments   string
	Flags      BlockTypeFlags
	IsUnloaded bool
}

func NewBlock() *Block { b := &Block{}; b.InitEntity(); return b }

func (*Block) ObjectName() string     { return TokenBlock }
func (*Block) SubclassMarker() string { return SubclassBlockBegin }
func (*Block) ObjectType() ObjectType { return ObjectTypeBlock }

// Name is the owning record's name.
func (b *Block) Name() string {
	if r, ok := b.Owner.(*BlockRecord); ok {
		return r.Name
	}
	return ""
}

// BlockEnd is the ENDBLK entity that ends a block definition.
type BlockEnd struct {
	EntityBase
}

func NewBlockEnd() *BlockEnd { b := &BlockEnd{}; b.InitEntity(); return b }

func (*BlockEnd) ObjectName() string     { return TokenEndBlock }
func (*BlockEnd) SubclassMarker() string { return SubclassBlockEnd }
func (*BlockEnd) ObjectType() ObjectType { return ObjectTypeEndblk }
