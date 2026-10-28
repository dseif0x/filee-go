package cadoc

import "strings"

// Object is ACadSharp's CadObject: anything stored in a drawing with a handle.
// Concrete types embed ObjectBase (or EntityBase / TableEntryBase).
type Object interface {
	// Base returns the data every object has (handle, owner, reactors,
	// extension dictionary, extended data).
	Base() *ObjectBase
	// ObjectName is the DXF name ("LINE", "LAYER", "DICTIONARY").
	ObjectName() string
	// SubclassMarker is the last DXF subclass marker ("AcDbLine").
	SubclassMarker() string
	// ObjectType is the DWG object type number.
	ObjectType() ObjectType
}

// ObjectBase is the common CadObject data.
type ObjectBase struct {
	// Handle identifies the object in the file; 0 means not assigned yet
	// (Document.AssignHandles gives it one).
	Handle uint64
	// Owner is the owning object (block record, table, dictionary, polyline,
	// insert …); nil for objects owned by the document itself.
	Owner Object
	// Reactors are the objects notified of changes ("{ACAD_REACTORS").
	Reactors []Object
	// XDictionary is the extension dictionary ("{ACAD_XDICTIONARY"), or nil.
	XDictionary *Dictionary
	// ExtendedData is the xdata, one entry per registered application.
	ExtendedData []ExtendedData
}

func (o *ObjectBase) Base() *ObjectBase { return o }

// GetExtendedData returns the xdata of an application (case-insensitive).
func (o *ObjectBase) GetExtendedData(appName string) (*ExtendedData, bool) {
	for i := range o.ExtendedData {
		if strings.EqualFold(o.ExtendedData[i].AppName, appName) {
			return &o.ExtendedData[i], true
		}
	}
	return nil, false
}

// SetExtendedData replaces (or adds) the xdata of an application.
func (o *ObjectBase) SetExtendedData(appName string, records []ExtendedDataRecord) {
	if x, ok := o.GetExtendedData(appName); ok {
		x.Records = records
		return
	}
	o.ExtendedData = append(o.ExtendedData, ExtendedData{AppName: appName, Records: records})
}

// RemoveExtendedData drops the xdata of an application.
func (o *ObjectBase) RemoveExtendedData(appName string) {
	for i := range o.ExtendedData {
		if strings.EqualFold(o.ExtendedData[i].AppName, appName) {
			o.ExtendedData = append(o.ExtendedData[:i], o.ExtendedData[i+1:]...)
			return
		}
	}
}

// CreateExtendedDictionary returns the extension dictionary, creating it.
func (o *ObjectBase) CreateExtendedDictionary(self Object) *Dictionary {
	if o.XDictionary == nil {
		o.XDictionary = NewDictionary()
		o.XDictionary.Owner = self
	}
	return o.XDictionary
}

// AddReactor appends a reactor.
func (o *ObjectBase) AddReactor(r Object) { o.Reactors = append(o.Reactors, r) }

// RemoveReactor drops a reactor.
func (o *ObjectBase) RemoveReactor(r Object) bool {
	for i, x := range o.Reactors {
		if x == r {
			o.Reactors = append(o.Reactors[:i], o.Reactors[i+1:]...)
			return true
		}
	}
	return false
}

// ExtendedData is the xdata of one application (XData.ExtendedData).
type ExtendedData struct {
	AppName string
	Records []ExtendedDataRecord
}

// ExtendedDataRecord is one xdata group (XData.ExtendedDataRecord). Value is
// typed by Code:
//
//	1000 string, 1002 control string "{" / "}", 1003 layer name: string
//	1004 binary chunk: []byte
//	1005 database handle: uint64
//	1010..1013 points / directions: Vec3
//	1040..1042 real, distance, scale: float64
//	1070 int16, 1071 int32
type ExtendedDataRecord struct {
	Code  int
	Value any
}

func (r ExtendedDataRecord) String() string { s, _ := r.Value.(string); return s }
func (r ExtendedDataRecord) Float() float64 { f, _ := r.Value.(float64); return f }
func (r ExtendedDataRecord) Int16() int16   { i, _ := r.Value.(int16); return i }
func (r ExtendedDataRecord) Int32() int32   { i, _ := r.Value.(int32); return i }
func (r ExtendedDataRecord) Handle() uint64 { h, _ := r.Value.(uint64); return h }
func (r ExtendedDataRecord) Point() Vec3    { p, _ := r.Value.(Vec3); return p }
func (r ExtendedDataRecord) Bytes() []byte  { b, _ := r.Value.([]byte); return b }

// Entity is a graphical object (ACadSharp's Entity). Concrete types embed
// EntityBase.
type Entity interface {
	Object
	// Entity returns the common entity properties.
	Entity() *EntityBase
}

// EntityBase is the AcDbEntity data shared by all entities.
type EntityBase struct {
	ObjectBase
	// Layer is never nil in a document produced by a reader; writers treat
	// nil as layer "0".
	Layer *Layer
	// LineType nil means ByLayer.
	LineType      *LineType
	Color         Color
	LineTypeScale float64
	LineWeight    LineWeight
	IsInvisible   bool
	Transparency  Transparency
	// Material is the 347 material reference, or nil.
	Material *Material
	// BookColor is the colour-book colour (430), or nil.
	BookColor *BookColor
	// ProxyGraphics is the raw proxy graphics (92/160 + 310), kept as is.
	ProxyGraphics []byte
}

func (e *EntityBase) Entity() *EntityBase { return e }

// InitEntity sets ACadSharp's defaults: ByLayer colour, line type and line
// weight, line type scale 1, ByLayer transparency.
func (e *EntityBase) InitEntity() {
	e.Color = ColorByLayer
	e.LineTypeScale = 1
	e.LineWeight = LineWeightByLayer
	e.Transparency = TransparencyByLayer
}

// LayerName is the layer's name, "0" without a layer.
func (e *EntityBase) LayerName() string {
	if e.Layer == nil {
		return DefaultLayerName
	}
	return e.Layer.Name
}

// LineTypeName is the line type's name, "ByLayer" without one.
func (e *EntityBase) LineTypeName() string {
	if e.LineType == nil {
		return ByLayerName
	}
	return e.LineType.Name
}

// ActiveColor resolves ByLayer (and ByBlock against the owning block).
func (e *EntityBase) ActiveColor() Color {
	switch {
	case e.Color.IsByLayer():
		if e.Layer != nil {
			return e.Layer.Color
		}
	case e.Color.IsByBlock():
		if r, ok := e.Owner.(*BlockRecord); ok && r.BlockEntity != nil {
			c := r.BlockEntity.Color
			if c.IsByLayer() && e.Layer != nil {
				c = e.Layer.Color
			}
			return c
		}
	}
	return e.Color
}

// ActiveLineType resolves ByLayer and ByBlock.
func (e *EntityBase) ActiveLineType() *LineType {
	name := e.LineTypeName()
	switch {
	case strings.EqualFold(name, ByLayerName):
		if e.Layer != nil {
			return e.Layer.LineType
		}
	case strings.EqualFold(name, ByBlockName):
		if r, ok := e.Owner.(*BlockRecord); ok && r.BlockEntity != nil {
			return r.BlockEntity.LineType
		}
	}
	return e.LineType
}

// ActiveLineWeight resolves ByLayer and ByBlock.
func (e *EntityBase) ActiveLineWeight() LineWeight {
	switch e.LineWeight {
	case LineWeightByLayer:
		if e.Layer != nil {
			return e.Layer.LineWeight
		}
	case LineWeightByBlock:
		if r, ok := e.Owner.(*BlockRecord); ok && r.BlockEntity != nil {
			return r.BlockEntity.ActiveLineWeight()
		}
	}
	return e.LineWeight
}

// MatchProperties copies the common properties of another entity.
func (e *EntityBase) MatchProperties(o *EntityBase) {
	e.Layer, e.LineType, e.Material = o.Layer, o.LineType, o.Material
	e.Color, e.LineWeight, e.LineTypeScale = o.Color, o.LineWeight, o.LineTypeScale
	e.IsInvisible, e.Transparency = o.IsInvisible, o.Transparency
}

// NonGraphicalObject is a named object stored in a dictionary (ACadSharp's
// NonGraphicalObject); Name is the key in the owning dictionary.
type NonGraphicalObject interface {
	Object
	ObjectNameKey() *string
}

// NonGraphicalBase is embedded by named objects.
type NonGraphicalBase struct {
	ObjectBase
	// Name is the entry name in the owning dictionary.
	Name string
}

func (n *NonGraphicalBase) ObjectNameKey() *string { return &n.Name }

// ObjectType is ACadSharp's ObjectType (DWG object type numbers).
type ObjectType int16

const (
	ObjectTypeUnlisted           ObjectType = -999
	ObjectTypeInvalid            ObjectType = -1
	ObjectTypeUndefined          ObjectType = 0
	ObjectTypeText               ObjectType = 1
	ObjectTypeAttrib             ObjectType = 2
	ObjectTypeAttdef             ObjectType = 3
	ObjectTypeBlock              ObjectType = 4
	ObjectTypeEndblk             ObjectType = 5
	ObjectTypeSeqend             ObjectType = 6
	ObjectTypeInsert             ObjectType = 7
	ObjectTypeMInsert            ObjectType = 8
	ObjectTypeVertex2D           ObjectType = 0x0A
	ObjectTypeVertex3D           ObjectType = 0x0B
	ObjectTypeVertexMesh         ObjectType = 0x0C
	ObjectTypeVertexPFace        ObjectType = 0x0D
	ObjectTypeVertexPFaceFace    ObjectType = 0x0E
	ObjectTypePolyline2D         ObjectType = 0x0F
	ObjectTypePolyline3D         ObjectType = 0x10
	ObjectTypeArc                ObjectType = 0x11
	ObjectTypeCircle             ObjectType = 0x12
	ObjectTypeLine               ObjectType = 0x13
	ObjectTypeDimensionOrdinate  ObjectType = 0x14
	ObjectTypeDimensionLinear    ObjectType = 0x15
	ObjectTypeDimensionAligned   ObjectType = 0x16
	ObjectTypeDimensionAng3Pt    ObjectType = 0x17
	ObjectTypeDimensionAng2Ln    ObjectType = 0x18
	ObjectTypeDimensionRadius    ObjectType = 0x19
	ObjectTypeDimensionDiameter  ObjectType = 0x1A
	ObjectTypePoint              ObjectType = 0x1B
	ObjectTypeFace3D             ObjectType = 0x1C
	ObjectTypePolylinePFace      ObjectType = 0x1D
	ObjectTypePolylineMesh       ObjectType = 0x1E
	ObjectTypeSolid              ObjectType = 0x1F
	ObjectTypeTrace              ObjectType = 0x20
	ObjectTypeShape              ObjectType = 0x21
	ObjectTypeViewport           ObjectType = 0x22
	ObjectTypeEllipse            ObjectType = 0x23
	ObjectTypeSpline             ObjectType = 0x24
	ObjectTypeRegion             ObjectType = 0x25
	ObjectTypeSolid3D            ObjectType = 0x26
	ObjectTypeBody               ObjectType = 0x27
	ObjectTypeRay                ObjectType = 0x28
	ObjectTypeXLine              ObjectType = 0x29
	ObjectTypeDictionary         ObjectType = 0x2A
	ObjectTypeOleFrame           ObjectType = 0x2B
	ObjectTypeMText              ObjectType = 0x2C
	ObjectTypeLeader             ObjectType = 0x2D
	ObjectTypeTolerance          ObjectType = 0x2E
	ObjectTypeMLine              ObjectType = 0x2F
	ObjectTypeBlockControlObj    ObjectType = 0x30
	ObjectTypeBlockHeader        ObjectType = 0x31
	ObjectTypeLayerControlObj    ObjectType = 0x32
	ObjectTypeLayer              ObjectType = 0x33
	ObjectTypeStyleControlObj    ObjectType = 0x34
	ObjectTypeStyle              ObjectType = 0x35
	ObjectTypeLTypeControlObj    ObjectType = 0x38
	ObjectTypeLType              ObjectType = 0x39
	ObjectTypeViewControlObj     ObjectType = 0x3C
	ObjectTypeView               ObjectType = 0x3D
	ObjectTypeUCSControlObj      ObjectType = 0x3E
	ObjectTypeUCS                ObjectType = 0x3F
	ObjectTypeVPortControlObj    ObjectType = 0x40
	ObjectTypeVPort              ObjectType = 0x41
	ObjectTypeAppIdControlObj    ObjectType = 0x42
	ObjectTypeAppId              ObjectType = 0x43
	ObjectTypeDimStyleControlObj ObjectType = 0x44
	ObjectTypeDimStyle           ObjectType = 0x45
	ObjectTypeVPEntHdrCtrlObj    ObjectType = 0x46
	ObjectTypeVPEntHdr           ObjectType = 0x47
	ObjectTypeGroup              ObjectType = 0x48
	ObjectTypeMLineStyle         ObjectType = 0x49
	ObjectTypeOle2Frame          ObjectType = 0x4A
	ObjectTypeDummy              ObjectType = 0x4B
	ObjectTypeLongTransaction    ObjectType = 0x4C
	ObjectTypeLwPolyline         ObjectType = 0x4D
	ObjectTypeHatch              ObjectType = 0x4E
	ObjectTypeXRecord            ObjectType = 0x4F
	ObjectTypeAcDbPlaceholder    ObjectType = 0x50
	ObjectTypeVbaProject         ObjectType = 0x51
	ObjectTypeLayout             ObjectType = 0x52
	ObjectTypeAcadProxyEntity    ObjectType = 0x1f2
	ObjectTypeAcadProxyObject    ObjectType = 0x1f3
)
