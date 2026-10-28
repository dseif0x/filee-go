// Package dxf reads and writes DXF files (ASCII and binary, AC1009 through
// AC1032) to and from the cadoc document model. It ports ACadSharp's DxfReader
// and DxfWriter.
package dxf

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// Level is the severity of a notification (NotificationType).
type Level int

const (
	LevelNone Level = iota
	LevelNotImplemented
	LevelWarning
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelNotImplemented:
		return "not implemented"
	case LevelWarning:
		return "warning"
	case LevelError:
		return "error"
	}
	return "info"
}

// Notify receives reader and writer messages; it may be nil.
type Notify func(level Level, message string)

// ReadConfig is DxfReaderConfiguration.
type ReadConfig struct {
	// Failsafe: an entity, block or object that can't be read is skipped
	// (with an error notification) instead of failing the whole file.
	Failsafe bool
	// CreateDefaults adds the tables, standard entries, dictionaries and
	// model/paper space a drawing needs when the file lacks them (minimal
	// DXF writers, R12 files with only an ENTITIES section).
	CreateDefaults bool
	// KeepUnknownEntities keeps entities without a model as UnknownEntity
	// (with their group codes) instead of dropping them.
	KeepUnknownEntities bool
	// KeepUnknownObjects keeps objects without a model as UnknownObject
	// instead of dropping them.
	KeepUnknownObjects bool
}

// ErrNotSupported is returned for DXF versions older than AC1002 (R2.5).
var ErrNotSupported = errors.New("dxf: version not supported")

// IsBinary reports whether data starts with the binary DXF sentinel.
func IsBinary(data []byte) bool { return bytes.HasPrefix(data, []byte(binarySentinel)) }

// ReadFile reads a DXF file.
func ReadFile(path string, cfg ReadConfig, notify Notify) (*cadoc.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ReadBytes(data, cfg, notify)
}

// Read reads a DXF document from r.
func Read(r io.Reader, cfg ReadConfig, notify Notify) (*cadoc.Document, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return ReadBytes(data, cfg, notify)
}

// ReadBytes reads a DXF document held in memory.
func ReadBytes(data []byte, cfg ReadConfig, notify Notify) (doc *cadoc.Document, err error) {
	rd := &reader{cfg: cfg, notify: notify, objects: map[uint64]cadoc.Object{}}
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("dxf: %v", p)
			doc = nil
		}
	}()
	return rd.read(data)
}

// Sniff returns the $ACADVER and $DWGCODEPAGE of a DXF without reading it
// fully, and whether it is binary.
func Sniff(data []byte) (version cadoc.Version, codePage string, binary bool) {
	s := newStream(data, newCodec(true, nil))
	version = cadoc.VersionUnknown
	if !findSection(s, tokHeader) {
		return version, "", s.binary
	}
	for s.next() {
		if s.code == 0 {
			break
		}
		if s.code == 9 {
			switch strings.ToUpper(s.str) {
			case "$ACADVER":
				if s.next() {
					version = cadoc.ParseVersion(s.str)
				}
			case "$DWGCODEPAGE":
				if s.next() {
					codePage = s.str
				}
			}
		}
	}
	return version, codePage, s.binary
}

func findSection(s *stream, name string) bool {
	for s.next() {
		if s.code == 0 && s.str == tokSection {
			if s.next() && s.code == 2 && strings.EqualFold(s.str, name) {
				return true
			}
		}
		if s.code == 0 && s.str == tokEOF {
			return false
		}
	}
	return false
}

type reader struct {
	cfg     ReadConfig
	notify  Notify
	s       *stream
	doc     *cadoc.Document
	version cadoc.Version
	// hold: the current group has not been consumed yet; the next loop
	// iteration must look at it instead of reading a new one (lockPointer).
	hold bool
	// subclass is the last 100 marker of the object being read.
	subclass string

	objects   map[uint64]cadoc.Object
	maxHandle uint64

	// fixDicts run after all sections are read, before fixups.
	fixDicts []func()
	fixups   []func()

	// entities read in the ENTITIES section with their owner.
	placed []placedEntity
	// blockRecords by handle, filled when the BLOCK_RECORD table is read.
	modelSpace, paperSpace *cadoc.BlockRecord
	dimBlockNames          map[*cadoc.DimensionBase]string
}

type placedEntity struct {
	e        cadoc.Entity
	owner    uint64
	hasOwner bool
	paper    bool
}

func (r *reader) warn(format string, args ...any) { r.note(LevelWarning, format, args...) }

func (r *reader) note(level Level, format string, args ...any) {
	if r.notify != nil {
		r.notify(level, fmt.Sprintf(format, args...))
	}
}

func (r *reader) read(data []byte) (*cadoc.Document, error) {
	version, codePage, _ := Sniff(data)
	if version != cadoc.VersionUnknown && version < cadoc.AC1002 {
		return nil, fmt.Errorf("%w: %s", ErrNotSupported, version)
	}
	r.version = version
	var codec *textCodec
	switch {
	case version >= cadoc.AC1021:
		codec = newCodec(true, nil)
	case codePage != "":
		enc := CodePageEncoding(codePage)
		if enc == nil {
			r.warn("Encoding %s not found, using Windows-1252", codePage)
		}
		codec = newCodec(false, enc)
	default:
		// Without a code page ACadSharp decodes as UTF-8; invalid UTF-8 falls
		// back to Windows-1252.
		codec = newCodec(true, nil)
	}
	if version == cadoc.VersionUnknown {
		r.warn("Dxf version not found, using a generic reader.")
	}
	r.s = newStream(data, codec)
	r.doc = cadoc.NewEmptyDocument()
	r.doc.Header = nil
	r.doc.SummaryInfo = &cadoc.SummaryInfo{}
	r.dimBlockNames = map[*cadoc.DimensionBase]string{}

	s := r.s
	sections := 0
	s.next()
	for !s.eof {
		if s.code == 0 && s.str == tokEOF {
			break
		}
		if s.code == 0 && s.str == tokTable {
			r.warn("Non-standard DXF: TABLE entry found outside of a TABLES section. Attempting recovery.")
			r.readTable()
			continue
		}
		if !(s.code == 0 && s.str == tokSection) {
			s.next()
			continue
		}
		s.next()
		if s.code != 2 {
			continue
		}
		sections++
		switch strings.ToUpper(s.str) {
		case tokHeader:
			r.readHeader()
		case tokClasses:
			r.readClasses()
		case tokTables:
			r.readTables()
		case tokBlocks:
			r.readBlocks()
		case tokEntities:
			r.readEntitiesSection()
		case tokObjects:
			r.readObjectsSection()
		case tokAcdsData:
			r.readAcdsData()
		default:
			r.note(LevelNotImplemented, "Section not implemented %s", s.str)
			r.skipSection()
		}
		if s.code == 0 && s.str == tokEndSection {
			s.next()
		}
	}
	// A broken group (bad code, truncated binary value) means the file is
	// damaged; like ACadSharp this fails even in failsafe mode.
	if s.err != nil {
		return nil, s.err
	}
	if sections == 0 {
		return nil, errors.New("dxf: no SECTION found, not a DXF file")
	}
	if r.doc.Header == nil {
		r.doc.Header = cadoc.NewHeader()
		r.doc.Header.Version = version
	}
	if r.doc.Header.HandleSeed == 0 {
		r.doc.Header.HandleSeed = 1
	}
	if err := r.build(); err != nil {
		return nil, err
	}
	return r.doc, nil
}

func (r *reader) skipSection() {
	s := r.s
	for s.next() {
		if s.code == 0 && (s.str == tokEndSection || s.str == tokEOF) {
			return
		}
	}
}

// at0 reports a group with code 0 (start of the next object) or the end.
func (r *reader) at0() bool { return r.s.code == 0 || r.s.eof }

// guard runs fn, turning a panic into an error notification in failsafe
// mode; the reader then skips to the next code 0.
func (r *reader) guard(what string, fn func()) (ok bool) {
	if !r.cfg.Failsafe {
		fn()
		return true
	}
	defer func() {
		if p := recover(); p != nil {
			r.note(LevelError, "Error while reading %s at %s: %v", what, r.s.position(), p)
			r.hold = false
			for !r.at0() {
				r.s.next()
			}
			ok = false
		}
	}()
	fn()
	return true
}

// register records an object under its handle.
func (r *reader) register(o cadoc.Object) {
	h := o.Base().Handle
	if h == 0 {
		return
	}
	if prev, dup := r.objects[h]; dup && prev != o {
		r.warn("Repeated handle found %X.", h)
		o.Base().Handle = 0
		return
	}
	r.objects[h] = o
	if h > r.maxHandle {
		r.maxHandle = h
	}
}

func (r *reader) object(h uint64) cadoc.Object {
	if h == 0 {
		return nil
	}
	return r.objects[h]
}

// ----- HEADER -----

func (r *reader) readHeader() {
	s := r.s
	h := cadoc.NewHeader()
	r.doc.Header = h
	s.next()
	for !s.eof && !(s.code == 0 && s.str == tokEndSection) {
		if s.code != 9 {
			s.next()
			continue
		}
		name := s.str
		v, ok := cadoc.LookupHeaderVariable(name)
		if !ok {
			s.next()
			continue
		}
		values := make([]any, len(v.Codes))
		complete := true
		for i := range v.Codes {
			s.next()
			if s.code == 9 || s.code == 0 {
				complete = false
				break
			}
			values[i] = s.value()
		}
		if complete {
			func() {
				defer func() {
					if p := recover(); p != nil {
						r.warn("Invalid value for header variable %s | %v", name, values[0])
					}
				}()
				v.Set(h, values)
			}()
			s.next()
		}
	}
	h.MaintenanceVersion = h.Version.MaintenanceVersion()
	if h.Version == cadoc.VersionUnknown && r.version != cadoc.VersionUnknown {
		h.Version = r.version
	}
}

// ----- CLASSES -----

func (r *reader) readClasses() {
	s := r.s
	s.next()
	for !s.eof && !(s.code == 0 && s.str == tokEndSection) {
		if !(s.code == 0 && s.str == tokClass) {
			s.next()
			continue
		}
		c := &cadoc.DxfClass{ApplicationName: cadoc.DefaultClassApplicationName, ClassNumber: 500}
		s.next()
		for !r.at0() {
			switch s.code {
			case 1:
				c.DxfName = s.str
			case 2:
				c.CppClassName = s.str
			case 3:
				c.ApplicationName = s.str
			case 90:
				c.ProxyFlags = cadoc.ProxyFlags(uint16(s.int()))
			case 91:
				c.InstanceCount = s.i32()
			case 280:
				c.WasZombie = s.bool()
			case 281:
				c.IsAnEntity = s.bool()
			}
			s.next()
		}
		if c.ClassNumber <= 500 {
			c.ClassNumber = int16(500 + r.doc.Classes.Len())
		}
		r.doc.Classes.TryAdd(c)
	}
}

// ----- common object data -----

// objectState collects the data every object has while it is read.
type objectState struct {
	obj      cadoc.Object
	owner    uint64
	hasOwner bool
	xdict    uint64
	reactors []uint64
	xdata    []cadoc.ExtendedData
}

// readDefinedGroup reads a 102 application group ("{ACAD_XDICTIONARY",
// "{ACAD_REACTORS", others are skipped). The reader ends on the closing "}".
func (r *reader) readDefinedGroup(st *objectState) {
	s := r.s
	name := s.str
	switch name {
	case "{ACAD_XDICTIONARY":
		s.next()
		st.xdict = s.handle()
		for !r.at0() && !(s.code == 102) {
			s.next()
		}
	case "{ACAD_REACTORS":
		s.next()
		for !r.at0() && s.code != 102 {
			st.reactors = append(st.reactors, s.handle())
			s.next()
		}
	default:
		for !r.at0() {
			s.next()
			if s.code == 102 {
				break
			}
		}
	}
}

// readXData reads extended data starting at a 1001 group; afterwards the
// reader is on the first group that is not xdata (hold is set).
func (r *reader) readXData(st *objectState) {
	s := r.s
	for !s.eof && s.code == 1001 {
		x := cadoc.ExtendedData{AppName: s.str}
		s.next()
		for !s.eof && s.code >= 1000 && s.code != 1001 {
			rec := cadoc.ExtendedDataRecord{Code: s.code}
			switch {
			case s.code == 1000 || s.code == 1002 || s.code == 1003:
				rec.Value = s.str
			case s.code == 1004:
				rec.Value = s.chunk
			case s.code == 1005:
				rec.Value = s.handle()
			case s.code >= 1010 && s.code <= 1013:
				p := cadoc.Vec3{X: s.float()}
				base := s.code
				s.next()
				if s.code == base+10 {
					p.Y = s.float()
					s.next()
					if s.code == base+20 {
						p.Z = s.float()
					} else {
						r.hold = true
					}
				} else {
					r.hold = true
				}
				rec.Value = p
			case s.code >= 1040 && s.code <= 1042:
				rec.Value = s.float()
			case s.code == 1070:
				rec.Value = s.i16()
			case s.code == 1071:
				rec.Value = s.i32()
			default:
				r.warn("Unknown code for extended data: %d", s.code)
				rec.Value = s.value()
			}
			x.Records = append(x.Records, rec)
			if r.hold {
				r.hold = false
				continue
			}
			s.next()
		}
		st.xdata = append(st.xdata, x)
	}
	r.hold = true
}

// commonObjectCode handles the groups every object can have; it reports
// whether it used the group.
func (r *reader) commonObjectCode(st *objectState) bool {
	s := r.s
	switch s.code {
	case 5:
		st.obj.Base().Handle = s.handle()
	case 330:
		if !st.hasOwner {
			st.owner, st.hasOwner = s.handle(), true
		}
	case 102:
		r.readDefinedGroup(st)
	case 100:
		r.subclass = s.str
	case 1001:
		r.readXData(st)
	default:
		return false
	}
	return true
}

// finishObject stores what objectState collected on the object and registers
// it. References to other objects are resolved after reading.
func (r *reader) finishObject(st *objectState) {
	b := st.obj.Base()
	b.ExtendedData = append(b.ExtendedData, st.xdata...)
	r.register(st.obj)
	if st.xdict != 0 || len(st.reactors) > 0 {
		xd, reactors := st.xdict, st.reactors
		r.fixDicts = append(r.fixDicts, func() {
			if d, ok := r.object(xd).(*cadoc.Dictionary); ok {
				b.XDictionary = d
				d.Owner = st.obj
			}
			for _, h := range reactors {
				if o := r.object(h); o != nil {
					b.Reactors = append(b.Reactors, o)
				}
			}
		})
	}
}

// later queues a reference fix-up that runs once everything is read.
func (r *reader) later(fn func()) { r.fixups = append(r.fixups, fn) }
