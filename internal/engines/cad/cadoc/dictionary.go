package cadoc

import (
	"fmt"
	"strings"
)

// Well-known dictionary names (CadDictionary constants).
const (
	RootDictionaryName         = "ROOT"
	AcadColorDictionary        = "ACAD_COLOR"
	AcadFieldListDictionary    = "ACAD_FIELDLIST"
	AcadGroupDictionary        = "ACAD_GROUP"
	AcadImageDictionary        = "ACAD_IMAGE_DICT"
	AcadImageVars              = "ACAD_IMAGE_VARS"
	AcadLayoutDictionary       = "ACAD_LAYOUT"
	AcadMaterialDictionary     = "ACAD_MATERIAL"
	AcadMLeaderStyleDictionary = "ACAD_MLEADERSTYLE"
	AcadMLineStyleDictionary   = "ACAD_MLINESTYLE"
	AcadPdfDefinitions         = "ACAD_PDFDEFINITIONS"
	AcadPlotSettings           = "ACAD_PLOTSETTINGS"
	AcadPlotStyleName          = "ACAD_PLOTSTYLENAME"
	AcadScaleList              = "ACAD_SCALELIST"
	AcadSortEnts               = "ACAD_SORTENTS"
	AcadTableStyleDictionary   = "ACAD_TABLESTYLE"
	AcadVisualStyle            = "ACAD_VISUALSTYLE"
	AcadWipeoutVars            = "ACAD_WIPEOUT_VARS"
	GeographicDataDictionary   = "ACAD_GEOGRAPHICDATA"
	VariableDictionaryName     = "AcDbVariableDictionary"
	SortEntsDictionaryName     = "ACAD_SORTENTS"
)

// Dictionary is CadDictionary (DICTIONARY): named references to objects, in
// order. Each entry's key is the object's Name; Set keeps both in step.
type Dictionary struct {
	NonGraphicalBase
	// HardOwnerFlag (280): entries are hard-owned (written as 360).
	HardOwnerFlag bool
	ClonningFlags DictionaryCloningFlags
	entries       []NonGraphicalObject
	// index maps lower-case names to positions; entries renamed after they
	// were added are found by a scan that refreshes it.
	index map[string]int
}

// NewDictionary returns an empty dictionary.
func NewDictionary() *Dictionary {
	return &Dictionary{ClonningFlags: DictionaryCloningFlagsKeepExisting}
}

// NewNamedDictionary returns an empty dictionary with an entry name.
func NewNamedDictionary(name string) *Dictionary {
	d := NewDictionary()
	d.Name = name
	return d
}

func (*Dictionary) ObjectName() string     { return TokenObjectDictionary }
func (*Dictionary) SubclassMarker() string { return SubclassDictionary }
func (*Dictionary) ObjectType() ObjectType { return ObjectTypeDictionary }

// Len is the number of entries.
func (d *Dictionary) Len() int { return len(d.entries) }

// Entries returns the entries in order; the slice must not be modified.
func (d *Dictionary) Entries() []NonGraphicalObject { return d.entries }

// Get returns the entry with that name (case-insensitive). It is
// ACadSharp's TryGetEntry.
func (d *Dictionary) Get(name string) (NonGraphicalObject, bool) {
	i := d.find(name)
	if i < 0 {
		return nil, false
	}
	return d.entries[i], true
}

func (d *Dictionary) find(name string) int {
	if i, ok := d.index[strings.ToLower(name)]; ok && i < len(d.entries) &&
		strings.EqualFold(*d.entries[i].ObjectNameKey(), name) {
		return i
	}
	for i, e := range d.entries {
		if strings.EqualFold(*e.ObjectNameKey(), name) {
			d.reindex()
			return i
		}
	}
	return -1
}

func (d *Dictionary) reindex() {
	d.index = make(map[string]int, len(d.entries))
	for i, e := range d.entries {
		k := strings.ToLower(*e.ObjectNameKey())
		if _, dup := d.index[k]; !dup {
			d.index[k] = i
		}
	}
}

// GetDictionary returns a sub-dictionary.
func (d *Dictionary) GetDictionary(name string) (*Dictionary, bool) {
	o, ok := d.Get(name)
	if !ok {
		return nil, false
	}
	switch x := o.(type) {
	case *Dictionary:
		return x, true
	case *DictionaryWithDefault:
		return &x.Dictionary, true
	}
	return nil, false
}

// Add appends an entry under its Name; it fails for empty or duplicate names.
func (d *Dictionary) Add(e NonGraphicalObject) error {
	name := *e.ObjectNameKey()
	if name == "" {
		return fmt.Errorf("cadoc: dictionary entry %s without a name", e.ObjectName())
	}
	if _, ok := d.Get(name); ok {
		return fmt.Errorf("cadoc: dictionary entry %q already exists", name)
	}
	d.entries = append(d.entries, e)
	if d.index == nil {
		d.index = map[string]int{}
	}
	d.index[strings.ToLower(name)] = len(d.entries) - 1
	e.Base().Owner = d
	return nil
}

// Set stores e under name, replacing an entry with the same name.
func (d *Dictionary) Set(name string, e NonGraphicalObject) {
	if i := d.find(name); i >= 0 {
		*e.ObjectNameKey() = name
		d.entries[i] = e
		e.Base().Owner = d
		return
	}
	*e.ObjectNameKey() = name
	_ = d.Add(e)
}

// TryAdd adds e unless its name exists, and returns the stored entry.
func (d *Dictionary) TryAdd(e NonGraphicalObject) NonGraphicalObject {
	if x, ok := d.Get(*e.ObjectNameKey()); ok {
		return x
	}
	_ = d.Add(e)
	return e
}

// Remove deletes the entry with that name.
func (d *Dictionary) Remove(name string) (NonGraphicalObject, bool) {
	i := d.find(name)
	if i < 0 {
		return nil, false
	}
	x := d.entries[i]
	d.entries = append(d.entries[:i], d.entries[i+1:]...)
	d.reindex()
	return x, true
}

// RemoveObject deletes an entry by identity.
func (d *Dictionary) RemoveObject(o Object) bool {
	for i, x := range d.entries {
		if Object(x) == o {
			d.entries = append(d.entries[:i], d.entries[i+1:]...)
			d.reindex()
			return true
		}
	}
	return false
}

// DictionaryWithDefault is ACDBDICTIONARYWDFLT: a dictionary with a default
// entry (used for ACAD_PLOTSTYLENAME).
type DictionaryWithDefault struct {
	Dictionary
	DefaultEntry Object
}

func NewDictionaryWithDefault(name string) *DictionaryWithDefault {
	d := &DictionaryWithDefault{Dictionary: *NewNamedDictionary(name)}
	return d
}

func (*DictionaryWithDefault) ObjectName() string     { return TokenObjectDictionaryWithDefault }
func (*DictionaryWithDefault) SubclassMarker() string { return SubclassDictionaryWithDefault }
func (*DictionaryWithDefault) ObjectType() ObjectType { return ObjectTypeUnlisted }
