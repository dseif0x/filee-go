package cadoc

import (
	"fmt"
	"iter"
	"strings"
)

// TableEntry is an entry of a symbol table (ACadSharp's TableEntry). Concrete
// entries embed TableEntryBase and keep their flags in a typed Flags field.
type TableEntry interface {
	Object
	// EntryName returns the entry name.
	EntryName() string
	// SetEntryName renames the entry; tables follow renames.
	SetEntryName(name string)
	// EntryFlags returns the code 70 flags as stored.
	EntryFlags() int16
	// SetEntryFlags sets the code 70 flags.
	SetEntryFlags(flags int16)
}

// TableEntryBase is the AcDbSymbolTableRecord data.
type TableEntryBase struct {
	ObjectBase
	Name string
}

func (t *TableEntryBase) EntryName() string        { return t.Name }
func (t *TableEntryBase) SetEntryName(name string) { t.Name = name }

// Table is a symbol table (ACadSharp's Table<T>): an ordered collection of
// entries indexed by name, case-insensitively. The table itself is an object
// (the table control object) with its own handle.
type Table[T TableEntry] struct {
	ObjectBase
	name       string
	objectType ObjectType
	entries    []T
	index      map[string]T
}

// NewTable creates an empty table; name is the DXF table name ("LAYER").
func NewTable[T TableEntry](name string, objectType ObjectType) *Table[T] {
	return &Table[T]{name: name, objectType: objectType, index: map[string]T{}}
}

func (t *Table[T]) ObjectName() string     { return t.name }
func (t *Table[T]) SubclassMarker() string { return SubclassTable }
func (t *Table[T]) ObjectType() ObjectType { return t.objectType }

// Len is the number of entries.
func (t *Table[T]) Len() int { return len(t.entries) }

// Entries returns the entries in order. The slice must not be modified.
func (t *Table[T]) Entries() []T { return t.entries }

// All iterates the entries in order.
func (t *Table[T]) All() iter.Seq[T] {
	return func(yield func(T) bool) {
		for _, e := range t.entries {
			if !yield(e) {
				return
			}
		}
	}
}

func key(name string) string { return strings.ToLower(name) }

// Get returns the entry with that name (case-insensitive). It is
// ACadSharp's TryGetValue.
func (t *Table[T]) Get(name string) (T, bool) {
	if e, ok := t.index[key(name)]; ok && strings.EqualFold(e.EntryName(), name) {
		return e, true
	}
	// An entry was renamed after it was added: find it and refresh the index.
	for _, e := range t.entries {
		if strings.EqualFold(e.EntryName(), name) {
			t.reindex()
			return e, true
		}
	}
	var zero T
	return zero, false
}

// TryGetValue is Get under ACadSharp's name.
func (t *Table[T]) TryGetValue(name string) (T, bool) { return t.Get(name) }

// Contains reports whether an entry with that name exists.
func (t *Table[T]) Contains(name string) bool { _, ok := t.Get(name); return ok }

// Add appends an entry and makes the table its owner. It fails when the name
// is empty or already used.
func (t *Table[T]) Add(e T) error {
	name := e.EntryName()
	if name == "" {
		return fmt.Errorf("cadoc: %s entry without a name", t.name)
	}
	if t.Contains(name) {
		return fmt.Errorf("cadoc: %s entry %q already exists", t.name, name)
	}
	t.entries = append(t.entries, e)
	t.index[key(name)] = e
	e.Base().Owner = t
	return nil
}

// TryAdd adds the entry unless one with its name exists, and returns the
// entry in the table.
func (t *Table[T]) TryAdd(e T) T {
	if existing, ok := t.Get(e.EntryName()); ok {
		return existing
	}
	_ = t.Add(e)
	return e
}

// Remove deletes the entry with that name.
func (t *Table[T]) Remove(name string) (T, bool) {
	for i, e := range t.entries {
		if strings.EqualFold(e.EntryName(), name) {
			t.entries = append(t.entries[:i], t.entries[i+1:]...)
			t.reindex()
			return e, true
		}
	}
	var zero T
	return zero, false
}

func (t *Table[T]) reindex() {
	t.index = make(map[string]T, len(t.entries))
	for _, e := range t.entries {
		t.index[key(e.EntryName())] = e
	}
}

type (
	AppIdsTable          = Table[*AppId]
	BlockRecordsTable    = Table[*BlockRecord]
	DimensionStylesTable = Table[*DimensionStyle]
	LayersTable          = Table[*Layer]
	LineTypesTable       = Table[*LineType]
	TextStylesTable      = Table[*TextStyle]
	UCSTable             = Table[*UCS]
	ViewsTable           = Table[*View]
	VPortsTable          = Table[*VPort]
)

func NewAppIdsTable() *AppIdsTable { return NewTable[*AppId]("APPID", ObjectTypeAppIdControlObj) }
func NewBlockRecordsTable() *BlockRecordsTable {
	return NewTable[*BlockRecord]("BLOCK_RECORD", ObjectTypeBlockControlObj)
}
func NewDimensionStylesTable() *DimensionStylesTable {
	return NewTable[*DimensionStyle]("DIMSTYLE", ObjectTypeDimStyleControlObj)
}
func NewLayersTable() *LayersTable { return NewTable[*Layer]("LAYER", ObjectTypeLayerControlObj) }
func NewLineTypesTable() *LineTypesTable {
	return NewTable[*LineType]("LTYPE", ObjectTypeLTypeControlObj)
}
func NewTextStylesTable() *TextStylesTable {
	return NewTable[*TextStyle]("STYLE", ObjectTypeStyleControlObj)
}
func NewUCSTable() *UCSTable       { return NewTable[*UCS]("UCS", ObjectTypeUCSControlObj) }
func NewViewsTable() *ViewsTable   { return NewTable[*View]("VIEW", ObjectTypeViewControlObj) }
func NewVPortsTable() *VPortsTable { return NewTable[*VPort]("VPORT", ObjectTypeVPortControlObj) }
