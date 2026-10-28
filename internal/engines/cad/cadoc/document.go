package cadoc

import (
	"fmt"
	"reflect"
	"strings"
)

// Document is ACadSharp's CadDocument: the header, classes, symbol tables,
// block records with their entities, and the object dictionary tree.
//
// References between objects are Go pointers. Handles (ObjectBase.Handle)
// are what the file used; readers keep them, and AssignHandles gives new or
// duplicate ones a free handle before writing.
type Document struct {
	Header  *Header
	Classes *DxfClassCollection

	AppIds          *AppIdsTable
	BlockRecords    *BlockRecordsTable
	DimensionStyles *DimensionStylesTable
	Layers          *LayersTable
	LineTypes       *LineTypesTable
	TextStyles      *TextStylesTable
	UCSs            *UCSTable
	Views           *ViewsTable
	VPorts          *VPortsTable

	// RootDictionary is the named object dictionary (owner of ACAD_LAYOUT,
	// ACAD_GROUP, …).
	RootDictionary *Dictionary

	// SummaryInfo is the drawing properties (DWG only; DXF keeps it empty).
	SummaryInfo *SummaryInfo
}

// SummaryInfo is CadSummaryInfo (drawing properties).
type SummaryInfo struct {
	Title, Subject, Author, Keywords, Comments, LastSavedBy, RevisionNumber, HyperlinkBase string
	Properties                                                                             map[string]string
}

// NewDocument returns a document with every default entry, like
// new CadDocument() (AC1032).
func NewDocument() *Document {
	d := NewEmptyDocument()
	d.CreateDefaults()
	return d
}

// NewEmptyDocument returns a document with a header and empty tables but no
// default entries (readers fill it).
func NewEmptyDocument() *Document {
	return &Document{
		Header:          NewHeader(),
		Classes:         &DxfClassCollection{},
		AppIds:          NewAppIdsTable(),
		BlockRecords:    NewBlockRecordsTable(),
		DimensionStyles: NewDimensionStylesTable(),
		Layers:          NewLayersTable(),
		LineTypes:       NewLineTypesTable(),
		TextStyles:      NewTextStylesTable(),
		UCSs:            NewUCSTable(),
		Views:           NewViewsTable(),
		VPorts:          NewVPortsTable(),
	}
}

// CreateDefaults adds what a drawing needs and is missing (CadDocument.
// CreateDefaults): the tables and their standard entries (layer 0, line
// types ByLayer/ByBlock/Continuous, Standard text and dimension styles,
// *Active viewport, ACAD application), the root dictionary with its standard
// dictionaries and entries, and model and paper space with their layouts.
func (d *Document) CreateDefaults() {
	if d.Header == nil {
		d.Header = NewHeader()
	}
	if d.Classes == nil {
		d.Classes = &DxfClassCollection{}
	}
	if d.SummaryInfo == nil {
		d.SummaryInfo = &SummaryInfo{}
	}
	if d.BlockRecords == nil {
		d.BlockRecords = NewBlockRecordsTable()
	}
	if d.Layers == nil {
		d.Layers = NewLayersTable()
	}
	if d.DimensionStyles == nil {
		d.DimensionStyles = NewDimensionStylesTable()
	}
	if d.TextStyles == nil {
		d.TextStyles = NewTextStylesTable()
	}
	if d.LineTypes == nil {
		d.LineTypes = NewLineTypesTable()
	}
	if d.Views == nil {
		d.Views = NewViewsTable()
	}
	if d.UCSs == nil {
		d.UCSs = NewUCSTable()
	}
	if d.VPorts == nil {
		d.VPorts = NewVPortsTable()
	}
	if d.AppIds == nil {
		d.AppIds = NewAppIdsTable()
	}
	if d.RootDictionary == nil {
		d.RootDictionary = NewNamedDictionary(RootDictionaryName)
	}
	d.createDefaultDictionaries()

	d.AppIds.TryAdd(NewAppId(DefaultAppIdName))
	for _, n := range []string{ByLayerName, ByBlockName, ContinuousName} {
		if !d.LineTypes.Contains(n) {
			lt := NewLineType(n)
			if n == ContinuousName {
				lt.Description = "Solid line"
			}
			_ = d.LineTypes.Add(lt)
		}
	}
	if !d.Layers.Contains(DefaultLayerName) {
		l := NewLayer(DefaultLayerName)
		l.LineType, _ = d.LineTypes.Get(ContinuousName)
		_ = d.Layers.Add(l)
	}
	if !d.TextStyles.Contains(DefaultTextStyleName) {
		ts := NewTextStyle(DefaultTextStyleName)
		ts.Filename = "arial.ttf"
		_ = d.TextStyles.Add(ts)
	}
	if !d.DimensionStyles.Contains(DefaultDimStyleName) {
		ds := NewDimensionStyle(DefaultDimStyleName)
		ds.Style, _ = d.TextStyles.Get(DefaultTextStyleName)
		_ = d.DimensionStyles.Add(ds)
	}
	d.VPorts.TryAdd(NewVPort(DefaultVPortName))

	layouts, _ := d.RootDictionary.GetDictionary(AcadLayoutDictionary)
	model, ok := d.BlockRecords.Get(ModelSpaceName)
	if !ok {
		model = NewBlockRecord(ModelSpaceName)
		_ = d.BlockRecords.Add(model)
	}
	if model.Layout == nil || !layoutListed(layouts, model.Layout) {
		l := model.Layout
		if l == nil {
			l = NewLayout(ModelLayoutName)
		}
		if existing, ok := layouts.Get(l.Name); ok && existing != NonGraphicalObject(l) {
			l.Name = ModelLayoutName
		}
		l.SetBlock(model)
		layouts.Set(l.Name, l)
	}
	paper, ok := d.BlockRecords.Get(PaperSpaceName)
	if !ok {
		paper = NewBlockRecord(PaperSpaceName)
		_ = d.BlockRecords.Add(paper)
	}
	if paper.Layout == nil || !layoutListed(layouts, paper.Layout) {
		l := paper.Layout
		if l == nil {
			name := PaperLayoutName
			for i := 2; ; i++ {
				if _, ok := layouts.Get(name); !ok {
					break
				}
				name = fmt.Sprintf("Layout%d", i)
			}
			l = NewLayout(name)
			l.TabOrder = 1
		}
		l.SetBlock(paper)
		if len(paper.Viewports()) == 0 {
			l.UpdatePaperViewport()
			for _, vp := range paper.Viewports() {
				vp.Id = 1
			}
		}
		layouts.Set(l.Name, l)
	}
	d.FixReferences()
}

func (d *Document) createDefaultDictionaries() {
	root := d.RootDictionary
	for _, n := range []string{AcadColorDictionary, AcadGroupDictionary, AcadLayoutDictionary, AcadMaterialDictionary,
		AcadSortEnts, AcadMLeaderStyleDictionary, AcadMLineStyleDictionary, AcadTableStyleDictionary, AcadPlotSettings,
		VariableDictionaryName, AcadScaleList, AcadVisualStyle, AcadImageDictionary, AcadFieldListDictionary,
		AcadPdfDefinitions} {
		if _, ok := root.Get(n); !ok {
			_ = root.Add(NewNamedDictionary(n))
		}
	}
	if scales, ok := root.GetDictionary(AcadScaleList); ok && scales.Len() == 0 {
		for _, s := range [][3]any{{"1:1", 1.0, 1.0}, {"1:2", 1.0, 2.0}, {"1:4", 1.0, 4.0}, {"1:5", 1.0, 5.0},
			{"1:8", 1.0, 8.0}, {"1:10", 1.0, 10.0}, {"1:16", 1.0, 16.0}, {"1:20", 1.0, 20.0}, {"1:30", 1.0, 30.0},
			{"1:40", 1.0, 40.0}, {"1:50", 1.0, 50.0}, {"1:100", 1.0, 100.0}, {"2:1", 2.0, 1.0}, {"4:1", 4.0, 1.0},
			{"8:1", 8.0, 1.0}, {"10:1", 10.0, 1.0}, {"100:1", 100.0, 1.0}} {
			sc := &Scale{PaperUnits: s[1].(float64), DrawingUnits: s[2].(float64)}
			sc.Name = s[0].(string)
			sc.IsUnitScale = sc.Name == DefaultScaleName
			// AutoCAD names the scale entries A0, A1, … and keeps the scale
			// name in the object; ACadSharp keys them by name.
			_ = scales.Add(sc)
		}
	}
	if ml, ok := root.GetDictionary(AcadMLineStyleDictionary); ok {
		if _, ok := ml.Get(DefaultMLineStyleName); !ok {
			s := NewMLineStyle(DefaultMLineStyleName)
			s.Elements = []MLineStyleElement{{Offset: 0.5, Color: ColorByLayer}, {Offset: -0.5, Color: ColorByLayer}}
			_ = ml.Add(s)
		}
	}
	if ml, ok := root.GetDictionary(AcadMLeaderStyleDictionary); ok {
		if _, ok := ml.Get(DefaultMultiLeaderStyleName); !ok {
			_ = ml.Add(NewMultiLeaderStyle(DefaultMultiLeaderStyleName))
		}
	}
	if vars, ok := root.GetDictionary(VariableDictionaryName); ok {
		for _, kv := range [][2]string{{DictionaryVariableCurrentMultiLeaderStyle, DefaultMultiLeaderStyleName},
			{DictionaryVariableCurrentAnnotationScale, DefaultScaleName}, {DictionaryVariableCurrentTableStyle, "Standard"},
			{DictionaryVariableWipeoutFrame, "1"}, {"CVIEWDETAILSTYLE", "Metric50"}, {"CVIEWSECTIONSTYLE", "Metric50"}} {
			if _, ok := vars.Get(kv[0]); !ok {
				_ = vars.Add(NewDictionaryVariable(kv[0], kv[1]))
			}
		}
	}
	if mats, ok := root.GetDictionary(AcadMaterialDictionary); ok {
		for _, n := range []string{"Global", ByLayerName, ByBlockName} {
			if _, ok := mats.Get(n); !ok {
				m := &Material{}
				m.Name = n
				_ = mats.Add(m)
			}
		}
	}
}

// FixReferences points missing table references at the document's
// standard entries (creating them when needed): entities without a layer get
// layer 0 and ByLayer line type, layers Continuous, texts and dimension
// styles Standard.
func (d *Document) FixReferences() {
	layer0, ok := d.Layers.Get(DefaultLayerName)
	if !ok {
		layer0 = NewLayer(DefaultLayerName)
		_ = d.Layers.Add(layer0)
	}
	ensureLT := func(name string) *LineType {
		lt, ok := d.LineTypes.Get(name)
		if !ok {
			lt = NewLineType(name)
			_ = d.LineTypes.Add(lt)
		}
		return lt
	}
	byLayer := ensureLT(ByLayerName)
	ensureLT(ByBlockName)
	continuous := ensureLT(ContinuousName)
	standard, ok := d.TextStyles.Get(DefaultTextStyleName)
	if !ok {
		standard = NewTextStyle(DefaultTextStyleName)
		_ = d.TextStyles.Add(standard)
	}
	for _, l := range d.Layers.Entries() {
		if l.LineType == nil {
			l.LineType = continuous
		}
	}
	for _, s := range d.DimensionStyles.Entries() {
		if s.Style == nil {
			s.Style = standard
		}
	}
	d.Walk(func(o Object) {
		e, ok := o.(Entity)
		if !ok {
			return
		}
		b := e.Entity()
		if b.Layer == nil {
			b.Layer = layer0
		}
		if b.LineType == nil {
			b.LineType = byLayer
		}
		switch t := e.(type) {
		case TextLike:
			if t.Text().Style == nil {
				t.Text().Style = standard
			}
		case *MText:
			if t.Style == nil {
				t.Style = standard
			}
		}
	})
}

// ModelSpace returns *Model_Space (nil without one).
func (d *Document) ModelSpace() *BlockRecord {
	r, _ := d.BlockRecords.Get(ModelSpaceName)
	return r
}

// PaperSpace returns *Paper_Space (nil without one).
func (d *Document) PaperSpace() *BlockRecord {
	r, _ := d.BlockRecords.Get(PaperSpaceName)
	return r
}

// Entities returns the model space entities.
func (d *Document) Entities() []Entity {
	if m := d.ModelSpace(); m != nil {
		return m.Entities
	}
	return nil
}

// NamedDictionary returns an entry of the root dictionary as a dictionary,
// creating it when create is set.
func (d *Document) NamedDictionary(name string, create bool) *Dictionary {
	if d.RootDictionary == nil {
		if !create {
			return nil
		}
		d.RootDictionary = NewNamedDictionary(RootDictionaryName)
	}
	if dict, ok := d.RootDictionary.GetDictionary(name); ok {
		return dict
	}
	if !create {
		return nil
	}
	dict := NewNamedDictionary(name)
	_ = d.RootDictionary.Add(dict)
	return dict
}

func dictEntries[T Object](d *Dictionary) []T {
	if d == nil {
		return nil
	}
	var out []T
	for _, e := range d.Entries() {
		if t, ok := e.(T); ok {
			out = append(out, t)
		}
	}
	return out
}

// Layouts returns the layouts of ACAD_LAYOUT in dictionary order.
func (d *Document) Layouts() []*Layout {
	return dictEntries[*Layout](d.NamedDictionary(AcadLayoutDictionary, false))
}

// AddLayout adds a layout to ACAD_LAYOUT and its block record to the
// BLOCK_RECORD table (creating *Paper_SpaceN when it has none).
func (d *Document) AddLayout(l *Layout) error {
	if l.AssociatedBlock == nil {
		name := PaperSpaceName
		if d.BlockRecords.Contains(name) {
			for i := 0; ; i++ {
				name = fmt.Sprintf("%s%d", PaperSpaceName, i)
				if !d.BlockRecords.Contains(name) {
					break
				}
			}
		}
		l.SetBlock(NewBlockRecord(name))
	}
	if !d.BlockRecords.Contains(l.AssociatedBlock.Name) {
		if err := d.BlockRecords.Add(l.AssociatedBlock); err != nil {
			return err
		}
	}
	return d.NamedDictionary(AcadLayoutDictionary, true).Add(l)
}

// Groups returns the groups of ACAD_GROUP.
func (d *Document) Groups() []*Group {
	return dictEntries[*Group](d.NamedDictionary(AcadGroupDictionary, false))
}

// MLineStyles returns the multiline styles of ACAD_MLINESTYLE.
func (d *Document) MLineStyles() []*MLineStyle {
	return dictEntries[*MLineStyle](d.NamedDictionary(AcadMLineStyleDictionary, false))
}

// MLeaderStyles returns the multileader styles of ACAD_MLEADERSTYLE.
func (d *Document) MLeaderStyles() []*MultiLeaderStyle {
	return dictEntries[*MultiLeaderStyle](d.NamedDictionary(AcadMLeaderStyleDictionary, false))
}

// Scales returns the annotation scales of ACAD_SCALELIST.
func (d *Document) Scales() []*Scale {
	return dictEntries[*Scale](d.NamedDictionary(AcadScaleList, false))
}

// ImageDefinitions returns the image definitions of ACAD_IMAGE_DICT.
func (d *Document) ImageDefinitions() []*ImageDefinition {
	return dictEntries[*ImageDefinition](d.NamedDictionary(AcadImageDictionary, false))
}

// PdfDefinitions returns the PDF underlay definitions.
func (d *Document) PdfDefinitions() []*PdfUnderlayDefinition {
	return dictEntries[*PdfUnderlayDefinition](d.NamedDictionary(AcadPdfDefinitions, false))
}

// DictionaryVariables returns the variables of AcDbVariableDictionary.
func (d *Document) DictionaryVariables() []*DictionaryVariable {
	return dictEntries[*DictionaryVariable](d.NamedDictionary(VariableDictionaryName, false))
}

// Colors returns the book colours of ACAD_COLOR.
func (d *Document) Colors() []*BookColor {
	return dictEntries[*BookColor](d.NamedDictionary(AcadColorDictionary, false))
}

// Materials returns the materials of ACAD_MATERIAL.
func (d *Document) Materials() []*Material {
	return dictEntries[*Material](d.NamedDictionary(AcadMaterialDictionary, false))
}

// CurrentLayer resolves $CLAYER (layer 0 when it doesn't exist).
func (d *Document) CurrentLayer() *Layer {
	if l, ok := d.Layers.Get(d.Header.CurrentLayerName); ok {
		return l
	}
	l, _ := d.Layers.Get(DefaultLayerName)
	return l
}

// CurrentTextStyle resolves $TEXTSTYLE.
func (d *Document) CurrentTextStyle() *TextStyle {
	if s, ok := d.TextStyles.Get(d.Header.CurrentTextStyleName); ok {
		return s
	}
	s, _ := d.TextStyles.Get(DefaultTextStyleName)
	return s
}

// CurrentDimensionStyle resolves $DIMSTYLE.
func (d *Document) CurrentDimensionStyle() *DimensionStyle {
	if s, ok := d.DimensionStyles.Get(d.Header.CurrentDimensionStyleName); ok {
		return s
	}
	s, _ := d.DimensionStyles.Get(DefaultDimStyleName)
	return s
}

// tables returns the symbol tables in DXF order.
func (d *Document) tables() []Object {
	var out []Object
	for _, t := range []Object{d.VPorts, d.LineTypes, d.Layers, d.TextStyles, d.Views, d.UCSs, d.AppIds,
		d.DimensionStyles, d.BlockRecords} {
		out = append(out, t)
	}
	return out
}

// Walk calls fn once for every object of the document: tables and their
// entries, block records with their BLOCK/ENDBLK and entities (including
// attributes, vertices and SEQENDs), the dictionary tree, extension
// dictionaries and objects only referenced from others (image reactors,
// book colours, materials).
func (d *Document) Walk(fn func(Object)) {
	seen := map[Object]bool{}
	var visit func(o Object)
	visitDict := func(x *Dictionary) {
		for _, e := range x.Entries() {
			visit(e)
		}
	}
	visit = func(o Object) {
		if isNil(o) || seen[o] {
			return
		}
		seen[o] = true
		fn(o)
		b := o.Base()
		if b.XDictionary != nil {
			visit(b.XDictionary)
		}
		switch x := o.(type) {
		case *Table[*AppId]:
			for _, e := range x.Entries() {
				visit(e)
			}
		case *Table[*BlockRecord]:
			for _, e := range x.Entries() {
				visit(e)
			}
		case *Table[*DimensionStyle]:
			for _, e := range x.Entries() {
				visit(e)
			}
		case *Table[*Layer]:
			for _, e := range x.Entries() {
				visit(e)
			}
		case *Table[*LineType]:
			for _, e := range x.Entries() {
				visit(e)
			}
		case *Table[*TextStyle]:
			for _, e := range x.Entries() {
				visit(e)
			}
		case *Table[*UCS]:
			for _, e := range x.Entries() {
				visit(e)
			}
		case *Table[*View]:
			for _, e := range x.Entries() {
				visit(e)
			}
		case *Table[*VPort]:
			for _, e := range x.Entries() {
				visit(e)
			}
		case *BlockRecord:
			if x.BlockEntity != nil {
				visit(x.BlockEntity)
			}
			for _, e := range x.Entities {
				visit(e)
			}
			if x.BlockEnd != nil {
				visit(x.BlockEnd)
			}
			if x.Layout != nil {
				visit(x.Layout)
			}
		case *Dictionary:
			visitDict(x)
		case *DictionaryWithDefault:
			visitDict(&x.Dictionary)
			if x.DefaultEntry != nil {
				visit(x.DefaultEntry)
			}
		case *Insert:
			for _, a := range x.Attributes {
				visit(a)
			}
			if x.AttributesSeqend != nil {
				visit(x.AttributesSeqend)
			}
		case *TableEntity:
			for _, a := range x.Attributes {
				visit(a)
			}
			if x.AttributesSeqend != nil {
				visit(x.AttributesSeqend)
			}
		case Polyline:
			for _, v := range x.VertexEntities() {
				visit(v)
			}
			if s := x.Polyline().Seqend; s != nil {
				visit(s)
			}
		case *RasterImage:
			if x.Definition != nil {
				visit(x.Definition)
			}
			if x.DefinitionReactor != nil {
				visit(x.DefinitionReactor)
			}
		case *PdfUnderlay:
			if x.Definition != nil {
				visit(x.Definition)
			}
		case *FieldList:
			for _, f := range x.Fields {
				visit(f)
			}
		case *Field:
			for _, f := range x.Children {
				visit(f)
			}
		}
		if e, ok := o.(Entity); ok {
			eb := e.Entity()
			if eb.Material != nil {
				visit(eb.Material)
			}
			if eb.BookColor != nil {
				visit(eb.BookColor)
			}
		}
	}
	for _, t := range d.tables() {
		visit(t)
	}
	if d.RootDictionary != nil {
		visit(d.RootDictionary)
	}
}

func isNil(o Object) bool {
	if o == nil {
		return true
	}
	v := reflect.ValueOf(o)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

// Index maps every handle of the document to its object.
func (d *Document) Index() map[uint64]Object {
	m := map[uint64]Object{}
	d.Walk(func(o Object) {
		if h := o.Base().Handle; h != 0 {
			if _, dup := m[h]; !dup {
				m[h] = o
			}
		}
	})
	return m
}

// GetObject returns the object with a handle.
func (d *Document) GetObject(handle uint64) (Object, bool) {
	o, ok := d.Index()[handle]
	return o, ok
}

// AssignHandles gives every object without a handle, or with one already used
// by another object, a new handle above the highest one in use, and sets
// $HANDSEED to the next free handle. Existing handles are kept.
func (d *Document) AssignHandles() {
	used := map[uint64]bool{}
	var pending []Object
	var max uint64
	d.Walk(func(o Object) {
		h := o.Base().Handle
		if h == 0 || used[h] {
			pending = append(pending, o)
			return
		}
		used[h] = true
		if h > max {
			max = h
		}
	})
	next := max + 1
	if d.Header != nil && d.Header.HandleSeed > next && d.Header.HandleSeed < 1<<40 {
		next = d.Header.HandleSeed
	}
	for _, o := range pending {
		o.Base().Handle = next
		next++
	}
	if d.Header != nil {
		d.Header.HandleSeed = next
	}
}

// UpdateDxfClasses refreshes the CLASSES section: every object type that
// needs a class gets one (with its instance count); classes read from a file
// are kept. With reset, existing classes are dropped first.
func (d *Document) UpdateDxfClasses(reset bool) {
	if d.Classes == nil {
		d.Classes = &DxfClassCollection{}
	}
	if reset {
		d.Classes.Clear()
	}
	for _, c := range d.Classes.Entries() {
		c.InstanceCount = 0
	}
	d.Walk(func(o Object) {
		cd, ok := o.(DxfClassDefined)
		if !ok {
			return
		}
		switch o.(type) {
		case *UnknownObject, *UnknownEntity:
			return
		}
		c := cd.DxfClass()
		if c.DxfName == "" {
			return
		}
		if existing, ok := d.Classes.Get(c.DxfName); ok {
			existing.InstanceCount++
			return
		}
		c.InstanceCount = 1
		d.Classes.TryAdd(&c)
	})
	d.Classes.ResetClassNumbers()
}

// BlockRecordByName is BlockRecords.Get, case-insensitive.
func (d *Document) BlockRecordByName(name string) (*BlockRecord, bool) {
	return d.BlockRecords.Get(name)
}

// UniqueBlockName returns a block name with the prefix (e.g. "*D") that no
// block record uses yet.
func (d *Document) UniqueBlockName(prefix string) string {
	used := map[string]bool{}
	for _, r := range d.BlockRecords.Entries() {
		used[strings.ToUpper(r.Name)] = true
	}
	for i := 1; ; i++ {
		n := fmt.Sprintf("%s%d", prefix, i)
		if !used[strings.ToUpper(n)] {
			return n
		}
	}
}

// UpdateOwners sets every object's Owner from the document structure:
// table entries are owned by their table, BLOCK/ENDBLK and entities by their
// block record, attributes by their insert, vertices by their polyline,
// dictionary entries by their dictionary and extension dictionaries by their
// object. Writers call it so that owner handles match the structure.
func (d *Document) UpdateOwners() {
	setOwnerTable := func(t Object, entries []TableEntry) {
		t.Base().Owner = nil
		for _, e := range entries {
			e.Base().Owner = t
		}
	}
	setOwnerTable(d.AppIds, entriesOf(d.AppIds))
	setOwnerTable(d.BlockRecords, entriesOf(d.BlockRecords))
	setOwnerTable(d.DimensionStyles, entriesOf(d.DimensionStyles))
	setOwnerTable(d.Layers, entriesOf(d.Layers))
	setOwnerTable(d.LineTypes, entriesOf(d.LineTypes))
	setOwnerTable(d.TextStyles, entriesOf(d.TextStyles))
	setOwnerTable(d.UCSs, entriesOf(d.UCSs))
	setOwnerTable(d.Views, entriesOf(d.Views))
	setOwnerTable(d.VPorts, entriesOf(d.VPorts))
	var ownDict func(dict *Dictionary)
	ownXDict := func(o Object) {
		if x := o.Base().XDictionary; x != nil {
			x.Owner = o
			ownDict(x)
		}
	}
	ownDict = func(dict *Dictionary) {
		for _, e := range dict.Entries() {
			e.Base().Owner = dict
			switch x := e.(type) {
			case *Dictionary:
				ownDict(x)
			case *DictionaryWithDefault:
				ownDict(&x.Dictionary)
			}
			ownXDict(e)
		}
	}
	var ownEntity func(e Entity, owner Object)
	ownEntity = func(e Entity, owner Object) {
		e.Base().Owner = owner
		ownXDict(e)
		switch x := e.(type) {
		case *Insert:
			for _, a := range x.Attributes {
				ownEntity(a, x)
			}
			if x.AttributesSeqend != nil {
				ownEntity(x.AttributesSeqend, x)
			}
		case *TableEntity:
			for _, a := range x.Attributes {
				ownEntity(a, x)
			}
			if x.AttributesSeqend != nil {
				ownEntity(x.AttributesSeqend, x)
			}
		case Polyline:
			for _, v := range x.VertexEntities() {
				ownEntity(v, x)
			}
			if s := x.Polyline().Seqend; s != nil {
				ownEntity(s, x)
			}
		case *RasterImage:
			if x.DefinitionReactor != nil {
				x.DefinitionReactor.Owner = x
				x.DefinitionReactor.Image = x
			}
		}
	}
	for _, r := range d.BlockRecords.Entries() {
		ownXDict(r)
		if r.BlockEntity != nil {
			ownEntity(r.BlockEntity, r)
		}
		if r.BlockEnd != nil {
			ownEntity(r.BlockEnd, r)
		}
		for _, e := range r.Entities {
			ownEntity(e, r)
		}
	}
	for _, t := range d.tables() {
		ownXDict(t)
	}
	if d.RootDictionary != nil {
		d.RootDictionary.Owner = nil
		ownDict(d.RootDictionary)
		ownXDict(d.RootDictionary)
	}
}

func entriesOf[T TableEntry](t *Table[T]) []TableEntry {
	out := make([]TableEntry, 0, t.Len())
	for _, e := range t.Entries() {
		out = append(out, e)
	}
	return out
}

// UpdateImageReactors gives every IMAGE with a definition an
// IMAGEDEF_REACTOR registered on the definition (CadDocument.UpdateImageReactors).
func (d *Document) UpdateImageReactors() {
	images := map[*ImageDefinition][]*RasterImage{}
	d.Walk(func(o Object) {
		if img, ok := o.(*RasterImage); ok && img.Definition != nil {
			images[img.Definition] = append(images[img.Definition], img)
		}
	})
	for def, imgs := range images {
		kept := def.Reactors[:0]
		for _, r := range def.Reactors {
			if _, ok := r.(*ImageDefinitionReactor); !ok {
				kept = append(kept, r)
			}
		}
		def.Reactors = kept
		for _, img := range imgs {
			if img.DefinitionReactor == nil {
				img.DefinitionReactor = &ImageDefinitionReactor{ClassVersion: 2}
			}
			img.DefinitionReactor.Image = img
			img.DefinitionReactor.Owner = img
			def.Reactors = append(def.Reactors, img.DefinitionReactor)
		}
	}
}

func layoutListed(layouts *Dictionary, l *Layout) bool {
	for _, e := range layouts.Entries() {
		if e == NonGraphicalObject(l) {
			return true
		}
	}
	return false
}
