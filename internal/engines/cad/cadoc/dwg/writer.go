package dwg

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// WriteConfig is DwgWriterConfiguration. The zero value is ACadSharp's
// default configuration.
type WriteConfig struct {
	// ResetDxfClasses rebuilds the CLASSES section from scratch.
	ResetDxfClasses bool
	// UpdateDimensionsInModel / UpdateDimensionsInBlocks regenerate the
	// dimension blocks first.
	UpdateDimensionsInModel  bool
	UpdateDimensionsInBlocks bool
	// SkipXData leaves out the extended data (ACadSharp's WriteXData=false).
	SkipXData bool
	// SkipXRecords leaves out XRECORD objects (WriteXRecords=false).
	SkipXRecords bool
	// WriteShapes writes SHAPE entities, which ACadSharp leaves out by
	// default because it can't validate them against the shape file.
	WriteShapes bool
	// WriteDynamicBlockData writes dynamic block objects
	// (ACDB_DYNAMICBLOCKPURGEPREVENTER_VERSION); off by default like ACadSharp.
	WriteDynamicBlockData bool
}

// Notification levels passed to the notify callback of Write: ACadSharp's
// NotificationType values, as the reader's Notify* constants.
const (
	writerLevelNotImplemented = -1
	writerLevelWarning        = 2
)

// WriteFile writes the document as DWG to a file (see Write).
func WriteFile(path string, doc *cadoc.Document, cfg WriteConfig, notify func(level int, msg string)) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	bw := bufio.NewWriterSize(f, 1<<16)
	if err := Write(bw, doc, cfg, notify); err != nil {
		return err
	}
	return bw.Flush()
}

// Write writes the document as DWG in doc.Header.Version: AC1014 (R14),
// AC1015 (2000), AC1018 (2004), AC1024 (2010), AC1027 (2013) or AC1032
// (2018), the versions ACadSharp's DwgWriter writes. AC1021 (2007) and older
// versions return an error.
//
// Like ACadSharp, writing prepares the document: missing default entries are
// created, handles assigned, owners, image reactors and the CLASSES section
// updated. Entities and objects that can't be written are left out and
// reported to notify (levels as ACadSharp's NotificationType: -1 not
// implemented, 2 warning); notify may be nil.
func Write(out io.Writer, doc *cadoc.Document, cfg WriteConfig, notify func(level int, msg string)) (err error) {
	if doc == nil {
		return errors.New("dwg: nil document")
	}
	if doc.Header == nil {
		doc.Header = cadoc.NewHeader()
	}
	v := doc.Header.Version
	switch v {
	case cadoc.AC1014, cadoc.AC1015, cadoc.AC1018, cadoc.AC1024, cadoc.AC1027, cadoc.AC1032:
	case cadoc.AC1021:
		return fmt.Errorf("dwg: writing version %s (AutoCAD 2007) is not supported", v)
	default:
		return fmt.Errorf("dwg: writing version %s is not supported", v)
	}
	w := &writer{doc: doc, cfg: cfg, notify: notify, version: v}
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("dwg: %v", p)
		}
	}()
	data := w.write()
	_, err = out.Write(data)
	return err
}

type writer struct {
	doc     *cadoc.Document
	cfg     WriteConfig
	notify  func(int, string)
	version cadoc.Version
	enc     *wTextEncoder
	index   map[uint64]cadoc.Object

	vEntityControlHandle uint64

	// objects to write after the tables and blocks, in order.
	queue  []cadoc.Object
	queued map[cadoc.Object]bool
	// writableMemo caches writable.
	writableMemo map[cadoc.Object]bool
}

func (w *writer) note(level int, format string, args ...any) {
	if w.notify != nil {
		w.notify(level, fmt.Sprintf(format, args...))
	}
}

func (w *writer) write() []byte {
	w.prepare()

	out := &wMemStream{}
	var fh wFileHeaderWriter
	codePage := writerFileCodePage(w.doc.Header.CodePage)
	if w.version >= cadoc.AC1018 {
		fh = newWFileHeaderAC18(out, w.doc, codePage)
	} else {
		fh = newWFileHeaderAC15(out, w.doc, codePage)
	}
	r2004 := w.version >= cadoc.AC1018

	fh.addSection(writerSecHeader, w.writeHeaderSection(), true, 0x7400)
	fh.addSection(writerSecClasses, w.writeClassesSection(), true, 0x7400)
	if r2004 {
		fh.addSection(writerSecSummaryInfo, w.writeSummaryInfoSection(), false, 0x100)
	}
	fh.addSection(writerSecPreview, w.writePreviewSection(), false, 0x400)
	if r2004 {
		fh.addSection(writerSecAppInfo, w.writeAppInfoSection(), false, 0x80)
		fh.addSection(writerSecFileDepList, writeFileDepListSection(), false, 0x80)
		fh.addSection(writerSecRevHistory, writeRevHistorySection(), true, 0x7400)
	}
	fh.addSection(writerSecAuxHeader, w.writeAuxHeaderSection(), true, 0x7400)
	ow := newWObjectWriter(w)
	objects := ow.write()
	fh.addSection(writerSecAcDbObjects, objects, true, 0x7400)
	fh.addSection(writerSecObjFreeSpace, w.writeObjFreeSpaceSection(len(ow.handles)), true, 0x7400)
	fh.addSection(writerSecTemplate, w.writeTemplateSection(), true, 0x7400)
	// The handles go last, the AC15 header needs the other sections' sizes.
	fh.addSection(writerSecHandles, writeHandlesSection(ow.handles, fh.handleSectionOffset()), true, 0x7400)
	fh.writeFile()
	return out.bytes()
}

// prepare brings the document into a writable state (CadWriterBase.Write
// plus what ACadSharp's document model guarantees by construction).
func (w *writer) prepare() {
	doc := w.doc
	doc.CreateDefaults()
	if w.cfg.UpdateDimensionsInModel || w.cfg.UpdateDimensionsInBlocks {
		for _, r := range doc.BlockRecords.Entries() {
			if r.IsModelSpace() && !w.cfg.UpdateDimensionsInModel || !r.IsModelSpace() && !w.cfg.UpdateDimensionsInBlocks {
				continue
			}
			for _, e := range r.Entities {
				if d, ok := e.(cadoc.Dimension); ok {
					if err := cadoc.UpdateDimensionBlock(doc, d); err != nil {
						w.note(writerLevelWarning, "Dimension %X: %v", e.Base().Handle, err)
					}
				}
			}
		}
	}
	doc.UpdateImageReactors()
	writerEnsureSeqends(doc)
	// The TrueType family and style of a text style live in its ACAD xdata
	// (as the DXF writer does it).
	for _, ts := range doc.TextStyles.Entries() {
		if ts.FontFamily != "" {
			ts.SetExtendedData(cadoc.DefaultAppIdName, writerTrueTypeXData(ts))
		}
	}
	w.registerAppIds()
	doc.UpdateOwners()
	doc.AssignHandles()
	if w.version < cadoc.AC1018 {
		// The viewport entity header control object (VPORT_ENTITY_HEADER
		// table) exists only up to R2000; it has no model, so it only takes a
		// handle.
		w.vEntityControlHandle = doc.Header.HandleSeed
		doc.Header.HandleSeed++
	}
	doc.UpdateDxfClasses(w.cfg.ResetDxfClasses)
	w.index = doc.Index()

	enc, ok := writerEncoding(doc.Header.CodePage)
	if !ok {
		w.note(writerLevelWarning, "Encoding %s not found, using Windows-1252 as default", doc.Header.CodePage)
	}
	if w.version >= cadoc.AC1021 {
		enc = wUTF16Encoder
	}
	w.enc = enc

	w.queued = map[cadoc.Object]bool{}
	w.writableMemo = map[cadoc.Object]bool{}
	w.enqueue(doc.RootDictionary)
}

// writerTrueTypeXData builds the ACAD xdata of a TrueType text style.
func writerTrueTypeXData(ts *cadoc.TextStyle) []cadoc.ExtendedDataRecord {
	var flags uint32 = 0x22 // ANSI charset, variable pitch, swiss family (AutoCAD's usual value)
	if ts.TrueType&cadoc.FontFlagsItalic != 0 {
		flags |= 0x01000000
	}
	if ts.TrueType&cadoc.FontFlagsBold != 0 {
		flags |= 0x02000000
	}
	return []cadoc.ExtendedDataRecord{{Code: 1000, Value: ts.FontFamily}, {Code: 1071, Value: int32(flags)}}
}

// registerAppIds adds APPID entries for every application used in xdata.
func (w *writer) registerAppIds() {
	doc := w.doc
	doc.Walk(func(o cadoc.Object) {
		for _, x := range o.Base().ExtendedData {
			if x.AppName != "" && !doc.AppIds.Contains(x.AppName) {
				_ = doc.AppIds.Add(cadoc.NewAppId(x.AppName))
			}
		}
	})
}

// writerEnsureSeqends gives polylines with vertices and inserts with
// attributes a SEQEND, so that it gets a handle.
func writerEnsureSeqends(doc *cadoc.Document) {
	doc.Walk(func(o cadoc.Object) {
		switch x := o.(type) {
		case *cadoc.Insert:
			if len(x.Attributes) > 0 && x.AttributesSeqend == nil {
				s := cadoc.NewSeqend()
				s.Layer, s.Owner = x.Layer, x
				x.AttributesSeqend = s
			}
		case cadoc.Polyline:
			b := x.Polyline()
			if len(x.VertexEntities()) > 0 && b.Seqend == nil {
				s := cadoc.NewSeqend()
				s.Layer, s.Owner = b.Layer, x
				b.Seqend = s
			}
		}
	})
}

func wIsNil(o cadoc.Object) bool {
	if o == nil {
		return true
	}
	v := reflect.ValueOf(o)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

// inDocument reports whether o is the document's object with its handle.
func (w *writer) inDocument(o cadoc.Object) bool {
	if wIsNil(o) {
		return false
	}
	h := o.Base().Handle
	return h != 0 && w.index[h] == o
}

// handleOf is the handle to write for a reference: 0 when the object is not
// written. Objects reached only through a reference are queued for the
// objects section.
func (w *writer) handleOf(o cadoc.Object) uint64 {
	if !w.writable(o) {
		return 0
	}
	w.enqueue(o)
	return o.Base().Handle
}

func (w *writer) enqueue(o cadoc.Object) {
	if wIsNil(o) || w.queued[o] {
		return
	}
	if _, ok := o.(cadoc.NonGraphicalObject); !ok {
		return
	}
	w.queued[o] = true
	w.queue = append(w.queue, o)
}

// writable reports whether an object ends up in the file.
func (w *writer) writable(o cadoc.Object) bool {
	if !w.inDocument(o) {
		return false
	}
	if v, ok := w.writableMemo[o]; ok {
		return v
	}
	w.writableMemo[o] = false // breaks owner cycles
	r := w.computeWritable(o)
	w.writableMemo[o] = r
	return r
}

func (w *writer) computeWritable(o cadoc.Object) bool {
	switch x := o.(type) {
	case *cadoc.AppIdsTable, *cadoc.BlockRecordsTable, *cadoc.DimensionStylesTable, *cadoc.LayersTable,
		*cadoc.LineTypesTable, *cadoc.TextStylesTable, *cadoc.UCSTable, *cadoc.ViewsTable, *cadoc.VPortsTable:
		return true
	case cadoc.TableEntry:
		return true
	case *cadoc.Block, *cadoc.BlockEnd:
		return true
	case cadoc.Entity:
		if ok, _ := w.entitySupported(x); !ok {
			return false
		}
		switch owner := x.Base().Owner.(type) {
		case *cadoc.BlockRecord:
			return true
		case cadoc.Entity:
			return w.writable(owner) && w.isChildOf(x, owner)
		}
		return false
	}
	if skip, _ := w.skipObject(o); skip {
		return false
	}
	// An object owned by something that isn't written (the extension
	// dictionary of a skipped entity, …) would dangle.
	if owner := o.Base().Owner; !wIsNil(owner) && owner != o && w.inDocument(owner) && !w.writable(owner) {
		return false
	}
	return true
}

// isChildOf reports whether e is an attribute, vertex or SEQEND of parent.
func (w *writer) isChildOf(e cadoc.Entity, parent cadoc.Entity) bool {
	switch p := parent.(type) {
	case *cadoc.Insert:
		if cadoc.Entity(p.AttributesSeqend) == e && p.AttributesSeqend != nil {
			return true
		}
		for _, a := range p.Attributes {
			if cadoc.Entity(a) == e {
				return true
			}
		}
	case cadoc.Polyline:
		if s := p.Polyline().Seqend; s != nil && cadoc.Entity(s) == e {
			return true
		}
		for _, v := range p.VertexEntities() {
			if v == e {
				return true
			}
		}
	}
	return false
}

// entitySupported is DwgObjectWriter.isEntitySupported (without the
// notification): validity checks and the entity types ACadSharp can't write.
func (w *writer) entitySupported(e cadoc.Entity) (ok bool, reason string) {
	if msg := writerEntityInvalid(e); msg != "" {
		return false, fmt.Sprintf("Invalid entity %s with handle %X: %s", e.ObjectName(), e.Base().Handle, msg)
	}
	switch x := e.(type) {
	case *cadoc.UnknownEntity:
		return false, ""
	case *cadoc.Shape:
		return w.cfg.WriteShapes, ""
	case *cadoc.TableEntity:
		// ACadSharp writes tables from R2010 on from the table content model,
		// which this model doesn't have.
		return false, fmt.Sprintf("Entity type not implemented %s", x.ObjectName())
	case *cadoc.Surface:
		return false, fmt.Sprintf("Entity type not implemented %s", x.ObjectName())
	case *cadoc.Mesh, *cadoc.MultiLeader, *cadoc.DimensionArc, *cadoc.RasterImage, *cadoc.Wipeout, *cadoc.PdfUnderlay:
		if _, found := w.doc.Classes.Get(e.ObjectName()); !found {
			return false, fmt.Sprintf("Dxf Class not found for %s", e.ObjectName())
		}
	}
	return true, ""
}

// writerEntityInvalid is Entity.IsValid for DWG: a zero normal, an insert
// without block, an ellipse whose normal isn't perpendicular to its major
// axis, an image without definition or clip boundary.
func writerEntityInvalid(e cadoc.Entity) string {
	if n, ok := writerNormalOf(e); ok && n.IsZero() {
		return "Normal vector cannot be zero."
	}
	switch x := e.(type) {
	case *cadoc.Insert:
		if x.Block == nil {
			return "Insert entity has no block reference."
		}
	case *cadoc.Ellipse:
		n, m := x.Normal.Normalized(), x.MajorAxisEndPoint.Normalized()
		if d := n.Dot(m); d > 1e-9 || d < -1e-9 {
			return "Normal and MajorAxisEndPoint must be perpendicular."
		}
	case *cadoc.RasterImage:
		if x.DefinitionReactor == nil {
			return "The DefinitionReactor property is null."
		}
		if x.Definition == nil {
			return "The Definition property is null."
		}
		if len(x.ClipBoundaryVertices) < 2 {
			return fmt.Sprintf("Invalid ClipBoundaryVertices count: %d, must be at least 2.", len(x.ClipBoundaryVertices))
		}
	case *cadoc.Wipeout:
		if len(x.ClipBoundaryVertices) < 2 {
			return fmt.Sprintf("Invalid ClipBoundaryVertices count: %d, must be at least 2.", len(x.ClipBoundaryVertices))
		}
	}
	return ""
}

// writerNormalOf returns the normal of the entities ACadSharp marks as
// IOrientable.
func writerNormalOf(e cadoc.Entity) (cadoc.Vec3, bool) {
	switch x := e.(type) {
	case *cadoc.Point:
		return x.Normal, true
	case *cadoc.Ellipse:
		return x.Normal, true
	case *cadoc.Shape:
		return x.Normal, true
	case *cadoc.MLine:
		return x.Normal, true
	case *cadoc.PdfUnderlay:
		return x.Normal, true
	case *cadoc.Circle:
		return x.Normal, true
	case *cadoc.Arc:
		return x.Normal, true
	case cadoc.Dimension:
		return x.Dimension().Normal, true
	case *cadoc.Insert:
		return x.Normal, true
	case *cadoc.Tolerance:
		return x.Normal, true
	case *cadoc.Leader:
		return x.Normal, true
	case *cadoc.Line:
		return x.Normal, true
	case *cadoc.Solid:
		return x.Normal, true
	case *cadoc.Spline:
		return x.Normal, true
	case *cadoc.Hatch:
		return x.Normal, true
	case *cadoc.LwPolyline:
		return x.Normal, true
	case cadoc.Polyline:
		return x.Polyline().Normal, true
	case cadoc.TextLike:
		return x.Text().Normal, true
	case *cadoc.MText:
		return x.Normal, true
	}
	return cadoc.Vec3{}, false
}

// skipObject is DwgObjectWriter.skipEntry: objects that are not written,
// and whether to report them.
func (w *writer) skipObject(o cadoc.Object) (skip bool, report bool) {
	switch o.(type) {
	case *cadoc.XRecord:
		return w.cfg.SkipXRecords, false
	case *cadoc.DynamicBlockPurgePreventer:
		return !w.cfg.WriteDynamicBlockData, false
	case *cadoc.UnknownObject, *cadoc.VisualStyle:
		return true, true
	case *cadoc.MTextAttributeObjectContextData:
		// Not implemented in ACadSharp's writer either.
		return true, true
	}
	if o.ObjectType() == cadoc.ObjectTypeUnlisted || o.ObjectType() == cadoc.ObjectTypeLayout && w.version < cadoc.AC1018 {
		if _, ok := w.doc.Classes.Get(o.ObjectName()); !ok {
			return true, true
		}
	}
	return false, false
}

// ----- lookups for the header -----

func (w *writer) currentLineType() cadoc.Object {
	if lt, ok := w.doc.LineTypes.Get(w.doc.Header.CurrentLineTypeName); ok {
		return lt
	}
	return w.lineTypeByName(cadoc.ByLayerName)
}

func (w *writer) currentMLineStyle() cadoc.Object {
	styles := w.doc.MLineStyles()
	for _, s := range styles {
		if strings.EqualFold(s.Name, w.doc.Header.CurrentMLineStyleName) {
			return s
		}
	}
	for _, s := range styles {
		if strings.EqualFold(s.Name, cadoc.DefaultMLineStyleName) {
			return s
		}
	}
	return nil
}

func (w *writer) lineTypeByName(name string) cadoc.Object {
	if name == "" {
		return nil
	}
	if lt, ok := w.doc.LineTypes.Get(name); ok {
		return lt
	}
	return nil
}

// arrowBlockByName finds the block of a DIMBLK-like name ("ArchTick" is
// the block "_ArchTick").
func (w *writer) arrowBlockByName(name string) cadoc.Object {
	if name == "" {
		return nil
	}
	for _, n := range []string{name, "_" + name} {
		if b, ok := w.doc.BlockRecords.Get(n); ok {
			return b
		}
	}
	return nil
}

func (w *writer) textStyleByName(name string) cadoc.Object {
	if s, ok := w.doc.TextStyles.Get(name); ok {
		return s
	}
	return nil
}

func (w *writer) ucsByName(name string) cadoc.Object {
	if name == "" {
		return nil
	}
	if u, ok := w.doc.UCSs.Get(name); ok {
		return u
	}
	return nil
}

func (w *writer) namedDictionary(name string) cadoc.Object {
	if w.doc.RootDictionary == nil {
		return nil
	}
	if o, ok := w.doc.RootDictionary.Get(name); ok {
		return o
	}
	return nil
}
