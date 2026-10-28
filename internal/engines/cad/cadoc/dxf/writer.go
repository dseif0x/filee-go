package dxf

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// WriteConfig is DxfWriterConfiguration.
type WriteConfig struct {
	// Binary writes a binary DXF instead of ASCII.
	Binary bool
	// WriteAllHeaderVariables writes every header variable instead of the
	// standard set (DefaultHeaderVariables plus HeaderVariables).
	WriteAllHeaderVariables bool
	HeaderVariables         []string
	// DecimalPrecision limits ASCII doubles to that many decimals; 0 writes
	// the shortest exact representation.
	DecimalPrecision int
	SkipXData        bool
	SkipXRecords     bool
	// ResetDxfClasses rebuilds the CLASSES section from scratch.
	ResetDxfClasses bool
	// UpdateDimensionsInModel / InBlocks regenerate dimension blocks first.
	UpdateDimensionsInModel  bool
	UpdateDimensionsInBlocks bool
	// WriteShapes writes SHAPE entities (ACadSharp leaves them out by default).
	WriteShapes bool
	// WriteUnknownEntities writes UnknownEntity back with its groups.
	WriteUnknownEntities bool
}

// DefaultHeaderVariables are the variables written by default
// (DxfWriterConfiguration.Variables).
var DefaultHeaderVariables = []string{
	"$ACADVER", "$DWGCODEPAGE", "$LASTSAVEDBY", "$HANDSEED", "$ANGBASE", "$ANGDIR", "$ATTMODE", "$AUNITS",
	"$AUPREC", "$CECOLOR", "$CELTSCALE", "$CELTYPE", "$CELWEIGHT", "$CLAYER", "$CMLJUST", "$CMLSCALE",
	"$CMLSTYLE", "$DIMSTYLE", "$TEXTSIZE", "$TEXTSTYLE", "$LUNITS", "$LUPREC", "$MIRRTEXT", "$EXTNAMES",
	"$INSBASE", "$INSUNITS", "$MEASUREMENT", "$LTSCALE", "$LWDISPLAY", "$PDMODE", "$PDSIZE", "$PLINEGEN",
	"$PSLTSCALE", "$SPLINESEGS", "$SURFU", "$SURFV", "$TDCREATE", "$TDUCREATE", "$TDUPDATE", "$TDUUPDATE",
	"$TDINDWG",
}

// WriteFile writes the document as DXF to a file.
func WriteFile(path string, doc *cadoc.Document, cfg WriteConfig, notify Notify) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	return Write(f, doc, cfg, notify)
}

// Write writes the document as DXF in doc.Header.Version (AC1015…AC1032;
// older versions are written as AC1015). Before AC1021 text is encoded in the
// code page named by Header.CodePage. Writing assigns missing handles,
// creates missing default entries, updates owners, image reactors and the
// CLASSES section of the document, like ACadSharp.
func Write(wr io.Writer, doc *cadoc.Document, cfg WriteConfig, notify Notify) (err error) {
	w := &writer{doc: doc, cfg: cfg, notify: notify, written: map[cadoc.Object]bool{}}
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("dxf: %v", p)
		}
	}()
	return w.write(wr)
}

type writer struct {
	doc     *cadoc.Document
	cfg     WriteConfig
	notify  Notify
	o       *out
	version cadoc.Version
	// queued entities of model and paper space for the ENTITIES section.
	entities []cadoc.Entity
	// objects to write in the OBJECTS section, in order.
	objects []cadoc.Object
	written map[cadoc.Object]bool
	index   map[uint64]cadoc.Object
}

func (w *writer) note(level Level, format string, args ...any) {
	if w.notify != nil {
		w.notify(level, fmt.Sprintf(format, args...))
	}
}

func (w *writer) write(wr io.Writer) error {
	doc := w.doc
	if doc.Header == nil {
		doc.Header = cadoc.NewHeader()
	}
	w.version = doc.Header.Version
	if w.version < cadoc.AC1015 {
		w.note(LevelWarning, "DXF version %s can't be written, using AC1015", w.version)
		w.version = cadoc.AC1015
	}
	if w.version > cadoc.AC1032 {
		w.version = cadoc.AC1032
	}
	w.prepare()

	var codec *textCodec
	if w.version >= cadoc.AC1021 {
		codec = newCodec(true, nil)
	} else {
		enc := CodePageEncoding(doc.Header.CodePage)
		if enc == nil {
			w.note(LevelWarning, "Encoding %s not found, using Windows-1252 as default", doc.Header.CodePage)
		}
		codec = newCodec(false, enc)
	}
	bw := bufio.NewWriterSize(wr, 1<<16)
	w.o = &out{w: bw, binary: w.cfg.Binary, codec: codec, prec: w.cfg.DecimalPrecision}
	if w.cfg.Binary {
		w.o.putString(binarySentinel)
	}

	// The root dictionary must be the first object (readers take the first
	// DICTIONARY as the root), before extension dictionaries queued while
	// tables and entities are written.
	w.enqueue(doc.RootDictionary)
	w.writeHeader()
	w.writeClasses()
	w.writeTables()
	w.writeBlocks()
	w.writeEntitiesSection()
	w.writeObjectsSection()
	w.writeAcdsData()
	w.o.str(0, tokEOF)
	if w.o.err != nil {
		return w.o.err
	}
	return bw.Flush()
}

// prepare brings the document into a writable state.
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
						w.note(LevelWarning, "Dimension %X: %v", e.Base().Handle, err)
					}
				}
			}
		}
	}
	sanitizeTableNames(w, doc.Layers)
	sanitizeTableNames(w, doc.LineTypes)
	sanitizeTableNames(w, doc.TextStyles)
	sanitizeTableNames(w, doc.DimensionStyles)
	sanitizeTableNames(w, doc.BlockRecords)
	sanitizeTableNames(w, doc.AppIds)
	sanitizeTableNames(w, doc.UCSs)
	sanitizeTableNames(w, doc.Views)
	sanitizeTableNames(w, doc.VPorts)
	doc.UpdateImageReactors()
	ensureSeqends(doc)
	w.registerAppIds()
	doc.UpdateOwners()
	doc.AssignHandles()
	doc.UpdateDxfClasses(w.cfg.ResetDxfClasses)
	w.index = doc.Index()
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

// known reports whether an object will be in the written file, so that a
// reference to it can be written: it belongs to the document and its type is
// written. Referenced objects are queued for the OBJECTS section, so an
// object reachable only through a reference is written too.
func (w *writer) known(o cadoc.Object) bool {
	if isNilObject(o) {
		return false
	}
	h := o.Base().Handle
	if h == 0 || w.index[h] != o {
		return false
	}
	switch x := o.(type) {
	case *cadoc.Seqend, cadoc.Vertex, *cadoc.AttributeEntity, *cadoc.Block, *cadoc.BlockEnd:
		return true
	case cadoc.Entity:
		return w.entityWritable(x)
	case cadoc.TableEntry:
		return true
	case *cadoc.AppIdsTable, *cadoc.BlockRecordsTable, *cadoc.DimensionStylesTable, *cadoc.LayersTable,
		*cadoc.LineTypesTable, *cadoc.TextStylesTable, *cadoc.UCSTable, *cadoc.ViewsTable, *cadoc.VPortsTable:
		return true
	}
	if !w.objectSupported(o) {
		return false
	}
	// An object owned by something that isn't written would dangle.
	if owner := o.Base().Owner; !isNilObject(owner) && owner != o && !w.known(owner) {
		return false
	}
	w.enqueue(o)
	return true
}

func (w *writer) section(name string, body func()) {
	w.o.str(0, tokSection)
	w.o.str(2, name)
	body()
	w.o.str(0, tokEndSection)
}

// ----- HEADER -----

func (w *writer) writeHeader() {
	h := w.doc.Header
	want := map[string]bool{}
	for _, n := range DefaultHeaderVariables {
		want[n] = true
	}
	for _, n := range w.cfg.HeaderVariables {
		want[strings.ToUpper(n)] = true
	}
	var order []*cadoc.HeaderVariable
	if v, ok := cadoc.LookupHeaderVariable("$ACADVER"); ok {
		order = append(order, v)
	}
	if w.cfg.WriteAllHeaderVariables {
		for i := range cadoc.HeaderVariables {
			if cadoc.HeaderVariables[i].Name != "$ACADVER" {
				order = append(order, &cadoc.HeaderVariables[i])
			}
		}
	} else {
		for _, n := range DefaultHeaderVariables {
			if v, ok := cadoc.LookupHeaderVariable(n); ok && n != "$ACADVER" {
				order = append(order, v)
			}
		}
		for _, n := range w.cfg.HeaderVariables {
			if v, ok := cadoc.LookupHeaderVariable(n); ok && !inDefault(v.Name) {
				order = append(order, v)
			}
		}
	}
	w.section(tokHeader, func() {
		for _, v := range order {
			if v.Ignored {
				continue
			}
			switch v.Name {
			case "$ACADVER":
				w.o.str(9, v.Name)
				w.o.str(1, w.version.String())
				continue
			case "$HANDSEED":
				w.o.str(9, v.Name)
				w.o.handle(5, h.HandleSeed)
				continue
			case "$DWGCODEPAGE":
				w.o.str(9, v.Name)
				cp := h.CodePage
				if cp == "" {
					cp = "ANSI_1252"
				}
				w.o.str(3, cp)
				continue
			}
			values := v.Get(h)
			if len(values) == 1 {
				if s, ok := values[0].(string); ok && s == "" && v.IsName {
					continue
				}
			}
			w.o.str(9, v.Name)
			for i, c := range v.Codes {
				w.o.any(c, values[i])
			}
		}
	})
}

func inDefault(name string) bool {
	for _, n := range DefaultHeaderVariables {
		if strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}

// ----- CLASSES -----

func (w *writer) writeClasses() {
	w.section(tokClasses, func() {
		for _, c := range w.doc.Classes.Entries() {
			w.o.str(0, tokClass)
			w.o.str(1, c.DxfName)
			w.o.str(2, c.CppClassName)
			w.o.str(3, c.ApplicationName)
			w.o.integer(90, int64(c.ProxyFlags))
			if w.version > cadoc.AC1015 {
				w.o.integer(91, int64(c.InstanceCount))
			}
			w.o.boolean(280, c.WasZombie)
			w.o.boolean(281, c.IsAnEntity)
		}
	})
}

// ----- common data -----

func (w *writer) commonObjectData(obj cadoc.Object) {
	b := obj.Base()
	if _, ok := obj.(*cadoc.DimensionStyle); ok {
		w.o.handle(105, b.Handle)
	} else {
		w.o.handle(5, b.Handle)
	}
	if b.XDictionary != nil {
		w.o.str(102, "{ACAD_XDICTIONARY")
		w.o.handle(360, b.XDictionary.Handle)
		w.o.str(102, "}")
		w.enqueue(b.XDictionary)
	}
	var reactors []cadoc.Object
	for _, r := range b.Reactors {
		if w.known(r) {
			reactors = append(reactors, r)
		}
	}
	if len(reactors) > 0 {
		w.o.str(102, "{ACAD_REACTORS")
		for _, r := range reactors {
			w.o.handle(330, r.Base().Handle)
		}
		w.o.str(102, "}")
	}
	w.o.ref(330, b.Owner)
}

func (w *writer) commonEntityData(e cadoc.Entity) {
	b := e.Entity()
	o := w.o
	o.str(100, cadoc.SubclassEntity)
	if r, ok := b.Owner.(*cadoc.BlockRecord); ok && r.Layout != nil && r.Layout.IsPaperSpace() {
		o.integer(67, 1)
	}
	o.str(8, b.LayerName())
	if lt := b.LineTypeName(); !strings.EqualFold(lt, cadoc.ByLayerName) {
		o.str(6, lt)
	}
	switch {
	case b.BookColor != nil && w.version >= cadoc.AC1018:
		o.integer(62, int64(b.BookColor.Color.ApproxIndex()))
		o.integer(420, int64(b.BookColor.Color.TrueColor()))
		o.str(430, b.BookColor.Name)
	case b.Color.IsTrueColor():
		o.integer(62, int64(b.Color.ApproxIndex()))
		if w.version >= cadoc.AC1018 {
			o.integer(420, int64(b.Color.TrueColor()))
		}
	default:
		o.integer(62, int64(b.Color.Index()))
	}
	if w.version >= cadoc.AC1015 && b.LineWeight != cadoc.LineWeightByLayer {
		o.integer(370, int64(b.LineWeight))
	}
	if b.LineTypeScale != 1 {
		o.dbl(48, b.LineTypeScale)
	}
	if b.IsInvisible {
		o.integer(60, 1)
	}
	if w.version >= cadoc.AC1024 && !b.Transparency.IsByLayer() {
		o.integer(440, int64(b.Transparency.AlphaValue()))
	}
}

func (w *writer) xdata(obj cadoc.Object) {
	if w.cfg.SkipXData {
		return
	}
	for _, x := range obj.Base().ExtendedData {
		if len(x.Records) == 0 && x.AppName == "" {
			continue
		}
		w.o.str(1001, x.AppName)
		for _, r := range x.Records {
			switch {
			case r.Code == 1005:
				h := r.Handle()
				if _, ok := w.index[h]; !ok {
					h = 0
				}
				w.o.handle(1005, h)
			case r.Code >= 1010 && r.Code <= 1013:
				w.o.vec3(r.Code, r.Point())
			case r.Code == 1004:
				w.o.chunk(1004, r.Bytes())
			default:
				w.o.any(r.Code, r.Value)
			}
		}
	}
}

// longText writes a text longer than 250 characters as subcode chunks
// followed by the final code (writeLongTextValue).
func (w *writer) longText(code, subcode int, text string) {
	r := []rune(text)
	for len(r) > 250 {
		w.o.str(subcode, string(r[:250]))
		r = r[250:]
	}
	w.o.str(code, string(r))
}

// ensureSeqends gives polylines with vertices and inserts with attributes a
// SEQEND, so that it gets a handle before the header is written.
func ensureSeqends(doc *cadoc.Document) {
	doc.Walk(func(o cadoc.Object) {
		switch x := o.(type) {
		case *cadoc.Insert:
			if len(x.Attributes) > 0 && x.AttributesSeqend == nil {
				s := cadoc.NewSeqend()
				s.Layer, s.Owner = x.Layer, x
				x.AttributesSeqend = s
			}
		case *cadoc.TableEntity:
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
