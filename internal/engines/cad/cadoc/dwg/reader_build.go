package dwg

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// tmplKind sorts templates into the builder's collections.
type tmplKind uint8

const (
	kindObject tmplKind = iota
	kindDictionary
	kindTable
	kindTableEntry
)

// tmpl is ACadSharp's CadTemplate: an object as read plus the handles it
// refers to. Type specific references are resolved by build, a closure set
// by the read function; the generic data is resolved by buildBase.
type tmpl struct {
	obj   cadoc.Object
	class *cadoc.DxfClass
	kind  tmplKind

	owner    uint64
	hasOwner bool
	xdict    uint64
	hasXDict bool
	reactors []uint64
	edata    []edataTmpl

	hasDsBinaryData bool

	ent *entityTmpl

	// name is the BLOCK entity name (blocks carry the anonymous block names).
	name string
	// rec is the block record specific data.
	rec *blockRecordTmpl
	// entryHandles are the entries of a table control object.
	entryHandles []uint64

	built bool
	build func(b *builder)
}

type edataTmpl struct {
	app     uint64
	records []cadoc.ExtendedDataRecord
}

// entityTmpl is the CadEntityTemplate data.
type entityTmpl struct {
	entityMode    byte
	layer         uint64
	ltype         uint64
	hasLtype      bool
	ltypeFlags    int // -1: not present (R13-R14)
	bookColor     uint64
	material      uint64
	prev, next    uint64
	hasNext       bool
	proxyGraphics []byte
}

// blockRecordTmpl is the CadBlockRecordTemplate data.
type blockRecordTmpl struct {
	beginBlock, endBlock uint64
	firstEntity          uint64
	lastEntity           uint64
	hasFirst             bool
	layout               uint64
	ownedObjects         []uint64
	insertHandles        []uint64
}

func (t *tmpl) addReactor(h uint64) {
	for _, x := range t.reactors {
		if x == h {
			return
		}
	}
	t.reactors = append(t.reactors, h)
}

// builder is DwgDocumentBuilder / CadDocumentBuilder.
type builder struct {
	r       *reader
	version cadoc.Version
	doc     *cadoc.Document

	headerHandles *headerHandles

	templates map[uint64]*tmpl
	objects   map[uint64]cadoc.Object

	dictTemplates       []*tmpl
	tableTemplates      map[uint64]*tmpl
	tableEntryTemplates []*tmpl
	objectTemplates     []*tmpl
	unassigned          []*tmpl
	initialHandSeed     uint64

	blockRecordTemplates []*tmpl

	appIds          *cadoc.AppIdsTable
	blockRecords    *cadoc.BlockRecordsTable
	dimensionStyles *cadoc.DimensionStylesTable
	layers          *cadoc.LayersTable
	lineTypes       *cadoc.LineTypesTable
	textStyles      *cadoc.TextStylesTable
	ucss            *cadoc.UCSTable
	views           *cadoc.ViewsTable
	vports          *cadoc.VPortsTable

	dataStorage []acdsRecord
}

func newBuilder(r *reader, v cadoc.Version) *builder {
	return &builder{r: r, version: v, doc: r.doc, headerHandles: newHeaderHandles(),
		templates: map[uint64]*tmpl{}, objects: map[uint64]cadoc.Object{}, tableTemplates: map[uint64]*tmpl{},
		appIds: cadoc.NewAppIdsTable(), blockRecords: cadoc.NewBlockRecordsTable(),
		dimensionStyles: cadoc.NewDimensionStylesTable(), layers: cadoc.NewLayersTable(),
		lineTypes: cadoc.NewLineTypesTable(), textStyles: cadoc.NewTextStylesTable(), ucss: cadoc.NewUCSTable(),
		views: cadoc.NewViewsTable(), vports: cadoc.NewVPortsTable()}
}

func (b *builder) notify(level int, format string, args ...any) { b.r.note(level, format, args...) }

// addTemplate registers a read object (CadDocumentBuilder.AddTemplate).
func (b *builder) addTemplate(t *tmpl) {
	if !b.addToMap(t) {
		return
	}
	switch t.kind {
	case kindDictionary:
		b.dictTemplates = append(b.dictTemplates, t)
	case kindTable:
		b.tableTemplates[t.obj.Base().Handle] = t
	case kindTableEntry:
		b.tableEntryTemplates = append(b.tableEntryTemplates, t)
	default:
		b.objectTemplates = append(b.objectTemplates, t)
	}
}

func (b *builder) addToMap(t *tmpl) bool {
	o := t.obj.Base()
	if o.Handle == 0 {
		b.unassigned = append(b.unassigned, t)
		return false
	}
	if _, dup := b.templates[o.Handle]; dup {
		b.notify(NotifyWarning, "Repeated handle found %d.", o.Handle)
		o.Handle = 0
		b.unassigned = append(b.unassigned, t)
		return false
	}
	if o.Handle > b.initialHandSeed {
		b.initialHandSeed = o.Handle
	}
	b.templates[o.Handle] = t
	b.objects[o.Handle] = t.obj
	return true
}

// hasTemplate reports a registered handle (TryGetObjectTemplate).
func (b *builder) hasTemplate(h uint64) bool {
	if h == 0 {
		return false
	}
	_, ok := b.templates[h]
	return ok
}

func (b *builder) template(h uint64) *tmpl {
	if h == 0 {
		return nil
	}
	return b.templates[h]
}

// object returns the object with a handle (TryGetCadObject without the type
// check): nil for 0, unknown handles and unknown objects that are not kept.
func (b *builder) object(h uint64) cadoc.Object {
	if h == 0 {
		return nil
	}
	o, ok := b.objects[h]
	if !ok {
		return nil
	}
	switch o.(type) {
	case *cadoc.UnknownEntity:
		if !b.r.cfg.KeepUnknownEntities {
			return nil
		}
	case *cadoc.UnknownObject:
		if !b.r.cfg.KeepUnknownObjects {
			return nil
		}
	}
	return o
}

// entity returns the entity with a handle, nil when it is something else.
func (b *builder) entity(h uint64) cadoc.Entity {
	e, _ := b.object(h).(cadoc.Entity)
	return e
}

// dictionaryOf returns a dictionary object as *cadoc.Dictionary.
func dictionaryOf(o cadoc.Object) *cadoc.Dictionary {
	switch d := o.(type) {
	case *cadoc.Dictionary:
		return d
	case *cadoc.DictionaryWithDefault:
		return &d.Dictionary
	}
	return nil
}

// tableRef is CadTemplate.getTableReference: the entry by handle, else by
// name, with a warning when neither resolves but one was given.
func tableRef[T cadoc.TableEntry](b *builder, t *tmpl, table *cadoc.Table[T], h uint64, name string) (T, bool) {
	if e, ok := b.object(h).(T); ok {
		return e, true
	}
	if name != "" {
		if e, ok := table.Get(name); ok {
			return e, true
		}
	}
	var zero T
	if name != "" || h != 0 {
		b.notify(NotifyWarning, "%s table reference with handle: %d | name: %s not found for %s with handle %d",
			table.ObjectName(), h, name, t.obj.ObjectName(), t.obj.Base().Handle)
	}
	return zero, false
}

// buildTemplate builds a template once (CadTemplate.Build).
func (b *builder) buildTemplate(t *tmpl) {
	if t == nil || t.built {
		return
	}
	t.built = true
	b.buildBase(t)
	if t.ent != nil {
		b.buildEntity(t)
	}
	if t.build != nil {
		t.build(b)
	}
}

// buildBase resolves the extension dictionary, reactors and extended data.
func (b *builder) buildBase(t *tmpl) {
	o := t.obj.Base()
	if t.hasXDict {
		if d := dictionaryOf(b.object(t.xdict)); d != nil {
			o.XDictionary = d
			d.Owner = t.obj
		}
	}
	for _, h := range t.reactors {
		if x := b.object(h); x != nil {
			if !hasReactor(o, x) {
				o.AddReactor(x)
			}
		} else {
			b.notify(NotifyWarning, "Reactor with handle %d not found", h)
		}
	}
	for _, e := range t.edata {
		app, ok := b.object(e.app).(*cadoc.AppId)
		if !ok {
			b.notify(NotifyWarning, "AppId in extended data with handle %d not found", e.app)
			continue
		}
		recs := make([]cadoc.ExtendedDataRecord, len(e.records))
		for i, rec := range e.records {
			if rec.Code == 1003 {
				// Layer references are handles in DWG and names in the model.
				if hv, ok := rec.Value.(uint64); ok {
					if l, ok := b.object(hv).(*cadoc.Layer); ok {
						rec.Value = l.Name
					} else {
						rec.Value = fmt.Sprintf("%X", hv)
					}
				}
			}
			recs[i] = rec
		}
		o.ExtendedData = append(o.ExtendedData, cadoc.ExtendedData{AppName: app.Name, Records: recs})
	}
}

func hasReactor(o *cadoc.ObjectBase, x cadoc.Object) bool {
	for _, r := range o.Reactors {
		if r == x {
			return true
		}
	}
	return false
}

// buildEntity resolves the common entity references (CadEntityTemplate).
func (b *builder) buildEntity(t *tmpl) {
	e := t.obj.(cadoc.Entity).Entity()
	et := t.ent
	if l, ok := tableRef(b, t, b.layers, et.layer, ""); ok {
		e.Layer = l
	}
	name := ""
	switch et.ltypeFlags {
	case 0:
		name = cadoc.ByLayerName
	case 1:
		name = cadoc.ByBlockName
	case 2:
		name = cadoc.ContinuousName
	}
	var lth uint64
	if et.hasLtype {
		lth = et.ltype
	}
	if lt, ok := tableRef(b, t, b.lineTypes, lth, name); ok {
		e.LineType = lt
	}
	if c, ok := b.object(et.bookColor).(*cadoc.BookColor); ok {
		e.BookColor = c
		// The entity colour of a book colour is stored as ByBlock; the
		// colour itself is in the DBCOLOR object (DXF writes it as 420).
		if e.Color.IsByBlock() {
			e.Color = c.Color
		}
	}
	if m, ok := b.object(et.material).(*cadoc.Material); ok {
		e.Material = m
	}
	if et.proxyGraphics != nil {
		e.ProxyGraphics = et.proxyGraphics
	}
}

// entitiesCollection follows an R13-R2000 entity chain from first to end
// (CadTemplate.getEntitiesCollection); next links default to handle+1.
func (b *builder) entitiesCollection(first, end uint64) []cadoc.Entity {
	t := b.template(first)
	if t == nil || t.ent == nil {
		b.notify(NotifyWarning, "Leading entity with handle %d not found.", first)
		t = b.template(end)
	}
	var out []cadoc.Entity
	seen := map[*tmpl]bool{}
	for t != nil && t.ent != nil && !seen[t] {
		seen[t] = true
		if e, ok := t.obj.(cadoc.Entity); ok {
			out = append(out, e)
		}
		h := t.obj.Base().Handle
		if h == end {
			break
		}
		if t.ent.hasNext {
			t = b.template(t.ent.next)
		} else {
			t = b.template(h + 1)
		}
	}
	return out
}

// ----- document build -----

// buildDocument is DwgDocumentBuilder.BuildDocument.
func (b *builder) buildDocument() {
	for _, t := range b.unassigned {
		t.obj.Base().Handle = b.initialHandSeed + 1
		b.addTemplate(t)
	}
	b.unassigned = nil

	for _, t := range b.blockRecordTemplates {
		b.setBlockToRecord(t)
	}

	d := b.doc
	d.AppIds, d.TextStyles, d.LineTypes, d.Layers = b.appIds, b.textStyles, b.lineTypes, b.layers
	d.UCSs, d.Views, d.BlockRecords, d.DimensionStyles, d.VPorts = b.ucss, b.views, b.blockRecords, b.dimensionStyles, b.vports

	for _, table := range []cadoc.Object{b.appIds, b.textStyles, b.lineTypes, b.layers, b.ucss, b.views,
		b.blockRecords, b.dimensionStyles, b.vports} {
		if t, ok := b.tableTemplates[table.Base().Handle]; ok && table.Base().Handle != 0 {
			b.buildTemplate(t)
		} else {
			b.notify(NotifyWarning, "Table %s not found in the document", table.ObjectName())
		}
	}

	b.buildDictionaries()

	for _, t := range b.tableEntryTemplates {
		b.buildTemplate(t)
	}
	for _, t := range b.objectTemplates {
		b.buildTemplate(t)
	}
	b.updateHeader()
}

// setBlockToRecord moves the BLOCK and ENDBLK entities into their record
// (CadBlockRecordTemplate.SetBlockToRecord).
func (b *builder) setBlockToRecord(t *tmpl) {
	rec := t.obj.(*cadoc.BlockRecord)
	if bt := b.template(t.rec.beginBlock); bt != nil {
		if blk, ok := b.object(t.rec.beginBlock).(*cadoc.Block); ok {
			if bt.name != "" {
				rec.Name = bt.name
			}
			// The BLOCK of an anonymous dynamic block representation carries
			// the dynamic block's name; AutoCAD names these *U<n> on load.
			if rec.IsAnonymous() && !strings.HasPrefix(rec.Name, "*") {
				prefix := t.name
				if !strings.HasPrefix(prefix, "*") {
					prefix = "*U"
				}
				rec.Name = b.uniqueAnonymousName(prefix)
			}
			old := rec.BlockEntity
			blk.Flags, blk.BasePoint, blk.XRefPath = old.Flags, old.BasePoint, old.XRefPath
			blk.Comments, blk.IsUnloaded = old.Comments, old.IsUnloaded
			blk.Owner = rec
			rec.BlockEntity = blk
		}
	}
	if be, ok := b.object(t.rec.endBlock).(*cadoc.BlockEnd); ok {
		be.Owner = rec
		rec.BlockEnd = be
	}
	b.ensureCorrectNaming(rec, "MODEL_SPACE", cadoc.ModelSpaceName)
	b.ensureCorrectNaming(rec, "PAPER_SPACE", cadoc.PaperSpaceName)
}

// uniqueAnonymousName returns prefix plus the lowest number no block record
// template uses.
func (b *builder) uniqueAnonymousName(prefix string) string {
	used := map[string]bool{}
	for _, t := range b.blockRecordTemplates {
		used[strings.ToUpper(t.obj.(*cadoc.BlockRecord).Name)] = true
	}
	for i := 1; ; i++ {
		if n := prefix + strconv.Itoa(i); !used[strings.ToUpper(n)] {
			return n
		}
	}
}

func (b *builder) ensureCorrectNaming(rec *cadoc.BlockRecord, handleName, expected string) {
	h, ok := b.headerHandles.get(handleName)
	if ok && rec.Handle == h && !strings.EqualFold(rec.Name, expected) {
		b.notify(NotifyWarning, "Invalid name for %s changed to %s", rec.Name, expected)
		rec.Name = expected
	}
}

// buildDictionaries builds the dictionaries and finds the root one.
func (b *builder) buildDictionaries() {
	for _, t := range b.dictTemplates {
		b.buildTemplate(t)
	}
	if b.doc.RootDictionary == nil {
		var roots []*cadoc.Dictionary
		for _, t := range b.dictTemplates {
			if d, ok := t.obj.(*cadoc.Dictionary); ok && d.Owner == nil {
				roots = append(roots, d)
			}
		}
		if len(roots) == 1 {
			b.doc.RootDictionary = roots[0]
		} else {
			b.notify(NotifyWarning, "The root dictionary could not be found.")
		}
	}
	// CadDocument.UpdateCollections(true, false): the standard dictionaries
	// exist even when the file lacks them.
	if b.doc.RootDictionary == nil {
		b.doc.RootDictionary = cadoc.NewNamedDictionary(cadoc.RootDictionaryName)
	}
	for _, n := range []string{cadoc.AcadLayoutDictionary, cadoc.AcadGroupDictionary, cadoc.AcadScaleList,
		cadoc.AcadMLineStyleDictionary, cadoc.AcadMLeaderStyleDictionary, cadoc.AcadTableStyleDictionary,
		cadoc.AcadImageDictionary, cadoc.AcadPdfDefinitions, cadoc.AcadColorDictionary, cadoc.VariableDictionaryName,
		cadoc.AcadMaterialDictionary} {
		if _, ok := b.doc.RootDictionary.Get(n); !ok {
			_ = b.doc.RootDictionary.Add(cadoc.NewNamedDictionary(n))
		}
	}
}

// updateHeader resolves the header handles to names
// (DwgHeaderHandlesCollection.UpdateHeader).
func (b *builder) updateHeader() {
	h := b.doc.Header
	hh := b.headerHandles
	entryName := func(key string) (string, bool) {
		v, _ := hh.get(key)
		if e, ok := b.object(v).(cadoc.TableEntry); ok {
			return e.EntryName(), true
		}
		return "", false
	}
	if n, ok := entryName("CLAYER"); ok {
		h.CurrentLayerName = n
	}
	if n, ok := entryName("CELTYPE"); ok {
		h.CurrentLineTypeName = n
	}
	if v, _ := hh.get("CMLSTYLE"); v != 0 {
		// ACadSharp looks for a table entry here and never finds the style.
		if s, ok := b.object(v).(*cadoc.MLineStyle); ok {
			h.CurrentMLineStyleName = s.Name
		}
	}
	if n, ok := entryName("TEXTSTYLE"); ok {
		h.CurrentTextStyleName = n
	}
	if n, ok := entryName("DIMTXSTY"); ok {
		h.DimensionTextStyleName = n
	}
	if n, ok := entryName("DIMSTYLE"); ok {
		h.CurrentDimensionStyleName = n
	}
	blockName := func(key string) (string, bool) {
		v, _ := hh.get(key)
		if r, ok := b.object(v).(*cadoc.BlockRecord); ok {
			return r.Name, true
		}
		return "", false
	}
	if n, ok := blockName("DIMBLK"); ok {
		h.DimensionBlockName = n
	}
	// ACadSharp writes DIMLDRBLK into DIMBLK; it is the leader arrow.
	if n, ok := blockName("DIMLDRBLK"); ok {
		h.ArrowBlockName = n
	}
	if n, ok := blockName("DIMBLK1"); ok {
		h.DimensionBlockNameFirst = n
	}
	if n, ok := blockName("DIMBLK2"); ok {
		h.DimensionBlockNameSecond = n
	}
	for key, set := range map[string]*string{"DIMLTYPE": &h.DimensionLineType, "DIMLTEX1": &h.DimensionTex1,
		"DIMLTEX2": &h.DimensionTex2, "UCSNAME_MSPACE": &h.UcsName, "UCSNAME_PSPACE": &h.PaperSpaceName,
		"UCSBASE": &h.UcsBaseName, "PUCSBASE": &h.PaperSpaceBaseName} {
		if n, ok := entryName(key); ok {
			*set = n
		}
	}
}

// finish gives the document the invariants of a document read from DXF:
// model space, standard line types, layouts linked to their blocks, table
// references resolved, a valid handle seed and consistent owners.
func (b *builder) finish() {
	doc := b.doc
	if doc.ModelSpace() == nil {
		_ = doc.BlockRecords.Add(cadoc.NewBlockRecord(cadoc.ModelSpaceName))
	}
	for _, n := range []string{cadoc.ByLayerName, cadoc.ByBlockName, cadoc.ContinuousName} {
		if !doc.LineTypes.Contains(n) {
			lt := cadoc.NewLineType(n)
			if n == cadoc.ContinuousName {
				lt.Description = "Solid line"
			}
			_ = doc.LineTypes.Add(lt)
		}
	}
	for _, l := range doc.Layouts() {
		if l.AssociatedBlock != nil {
			l.AssociatedBlock.Layout = l
		}
	}
	for _, r := range doc.BlockRecords.Entries() {
		if r.Layout != nil && r.Layout.AssociatedBlock == nil {
			r.Layout.AssociatedBlock = r
		}
	}
	for _, ts := range doc.TextStyles.Entries() {
		applyTrueTypeXData(ts)
	}
	// DWG viewports have no id (DXF 69); AutoCAD numbers them per layout in
	// drawing order, the first one being the paper space viewport.
	for _, r := range doc.BlockRecords.Entries() {
		if !r.IsPaperSpace() {
			continue
		}
		var id int16
		for _, vp := range r.Viewports() {
			if vp.Id == 0 {
				id++
				vp.Id = id
				// The stacking order (DXF 68) is not stored either.
				vp.ActiveStatus = id
			}
		}
	}
	doc.FixReferences()
	var maxHandle uint64
	for h := range b.objects {
		maxHandle = max(maxHandle, h)
	}
	if doc.Header.HandleSeed <= maxHandle {
		doc.Header.HandleSeed = maxHandle + 1
	}
	// Objects created here (missing standard entries) get handles, as
	// adding them to an ACadSharp document does.
	doc.AssignHandles()
	doc.UpdateOwners()
}

// applyTrueTypeXData reads the TrueType family (1000) and style flags (1071)
// a text style keeps in its ACAD xdata.
func applyTrueTypeXData(ts *cadoc.TextStyle) {
	x, ok := ts.GetExtendedData(cadoc.DefaultAppIdName)
	if !ok {
		return
	}
	for _, rec := range x.Records {
		switch rec.Code {
		case 1000:
			if ts.FontFamily == "" {
				ts.FontFamily = rec.String()
			}
		case 1071:
			flags := uint32(rec.Int32())
			var f cadoc.FontFlags
			if flags&0x01000000 != 0 {
				f |= cadoc.FontFlagsItalic
			}
			if flags&0x02000000 != 0 {
				f |= cadoc.FontFlagsBold
			}
			ts.TrueType = f
		}
	}
}
