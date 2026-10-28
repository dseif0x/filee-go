package cadoc

import "strings"

// DxfClass is an entry of the CLASSES section.
type DxfClass struct {
	DxfName         string
	CppClassName    string
	ApplicationName string
	ProxyFlags      ProxyFlags
	InstanceCount   int32
	WasZombie       bool
	// IsAnEntity: instances are entities (item class id 0x1F2) rather than
	// objects (0x1F3).
	IsAnEntity         bool
	ClassNumber        int16
	DwgVersion         Version
	MaintenanceVersion int16
}

const DefaultClassApplicationName = "ObjectDBX Classes"

// ItemClassId is 0x1F2 for entity classes and 0x1F3 for object classes.
func (c *DxfClass) ItemClassId() int16 {
	if c.IsAnEntity {
		return 0x1F2
	}
	return 0x1F3
}

// DxfClassCollection is the ordered set of classes, indexed by DXF name.
type DxfClassCollection struct {
	entries []*DxfClass
}

func (c *DxfClassCollection) Len() int             { return len(c.entries) }
func (c *DxfClassCollection) Entries() []*DxfClass { return c.entries }

// Get returns the class with a DXF name (case-insensitive).
func (c *DxfClassCollection) Get(dxfName string) (*DxfClass, bool) {
	for _, e := range c.entries {
		if strings.EqualFold(e.DxfName, dxfName) {
			return e, true
		}
	}
	return nil, false
}

// GetByClassNumber returns the class with a class number.
func (c *DxfClassCollection) GetByClassNumber(n int16) (*DxfClass, bool) {
	for _, e := range c.entries {
		if e.ClassNumber == n {
			return e, true
		}
	}
	return nil, false
}

// TryAdd adds a class unless one with its DXF name exists.
func (c *DxfClassCollection) TryAdd(cl *DxfClass) bool {
	if _, ok := c.Get(cl.DxfName); ok {
		return false
	}
	c.entries = append(c.entries, cl)
	return true
}

// Remove deletes a class by DXF name.
func (c *DxfClassCollection) Remove(dxfName string) bool {
	for i, e := range c.entries {
		if strings.EqualFold(e.DxfName, dxfName) {
			c.entries = append(c.entries[:i], c.entries[i+1:]...)
			return true
		}
	}
	return false
}

// Clear removes all classes.
func (c *DxfClassCollection) Clear() { c.entries = nil }

// ResetClassNumbers numbers the classes 500, 501, … in order.
func (c *DxfClassCollection) ResetClassNumbers() {
	for i, e := range c.entries {
		e.ClassNumber = int16(500 + i)
	}
}

// DxfClassDefined is implemented by objects whose type needs a CLASSES entry
// (ACadSharp's IDxfClassDefined).
type DxfClassDefined interface {
	DxfClass() DxfClass
}

const (
	proxyR13      = ProxyFlagsR13FormatProxy
	proxyAll      = ProxyFlags(4095)
	proxyEraseDis = ProxyFlagsEraseAllowed | ProxyFlagsDisablesProxyWarningDialog
	proxyECD      = ProxyFlagsEraseAllowed | ProxyFlagsCloningAllowed | ProxyFlagsDisablesProxyWarningDialog
	proxyImage    = ProxyFlagsEraseAllowed | ProxyFlagsTransformAllowed | ProxyFlagsColorChangeAllowed |
		ProxyFlagsLayerChangeAllowed | ProxyFlagsLinetypeChangeAllowed | ProxyFlagsLinetypeScaleChangeAllowed |
		ProxyFlagsVisibilityChangeAllowed | ProxyFlagsR13FormatProxy
)

func objClass(dxf, cpp string, v Version, maint int16, flags ProxyFlags) DxfClass {
	return DxfClass{DxfName: dxf, CppClassName: cpp, ApplicationName: DefaultClassApplicationName,
		DwgVersion: v, MaintenanceVersion: maint, ProxyFlags: flags}
}

func entClass(dxf, cpp string, v Version, flags ProxyFlags) DxfClass {
	c := objClass(dxf, cpp, v, 0, flags)
	c.IsAnEntity = true
	return c
}

func (*DictionaryWithDefault) DxfClass() DxfClass {
	return objClass(TokenObjectDictionaryWithDefault, SubclassDictionaryWithDefault, 22, 42, proxyR13)
}
func (*DictionaryVariable) DxfClass() DxfClass {
	return objClass(TokenObjectDictionaryVar, SubclassDictionaryVar, 20, 0, 0)
}
func (*PlotSettings) DxfClass() DxfClass {
	return objClass(TokenObjectPlotSettings, SubclassPlotSettings, AC1015, 42, 0)
}
func (*Layout) DxfClass() DxfClass { return objClass(TokenObjectLayout, SubclassLayout, 0, 0, 0) }
func (*MultiLeaderStyle) DxfClass() DxfClass {
	return objClass(TokenObjectMLeaderStyle, SubclassMLeaderStyle, AC1021, 25, proxyAll)
}
func (*ImageDefinition) DxfClass() DxfClass {
	c := objClass(TokenObjectImageDefinition, SubclassRasterImageDef, 20, 0, 0)
	c.ApplicationName = "ISM"
	return c
}
func (*ImageDefinitionReactor) DxfClass() DxfClass {
	c := objClass(TokenObjectImageDefinitionReactor, SubclassRasterImageDefReactor, 20, 0, ProxyFlagsEraseAllowed)
	c.ApplicationName = "ISM"
	return c
}
func (*RasterVariables) DxfClass() DxfClass {
	c := objClass(TokenObjectRasterVariables, SubclassRasterVariables, 20, 0, 0)
	c.ApplicationName = "ISM"
	return c
}
func (*WipeoutVariables) DxfClass() DxfClass {
	c := objClass(TokenObjectWipeoutVariables, SubclassWipeoutVariables, AC1018, 0, 0)
	c.ApplicationName = "\"WipeOut|Product Desc: WipeOut Dbx Application|Company: Autodesk, Inc.|WEB Address: www.autodesk.com\""
	return c
}
func (*Scale) DxfClass() DxfClass {
	return objClass(TokenObjectScale, SubclassScale, AC1021, 1, proxyECD)
}
func (*SortEntitiesTable) DxfClass() DxfClass {
	return objClass(TokenObjectSortEntsTable, SubclassSortentsTable, AC1014, 0, 0)
}
func (*BookColor) DxfClass() DxfClass {
	return objClass(TokenObjectDBColor, SubclassDbColor, AC1015, 14, 0)
}
func (*PdfUnderlayDefinition) DxfClass() DxfClass {
	return objClass(TokenObjectPdfDefinition, SubclassPdfDefinition, 26, 0, proxyECD)
}
func (*GeoData) DxfClass() DxfClass {
	return objClass(TokenObjectGeoData, SubclassGeoData, AC1021, 45, proxyAll)
}
func (*SpatialFilter) DxfClass() DxfClass {
	return objClass(TokenObjectSpatialFilter, SubclassSpatialFilter, 20, 0, 0)
}
func (*Material) DxfClass() DxfClass {
	return objClass(TokenObjectMaterial, SubclassMaterial, 0, 0, proxyECD)
}
func (*VisualStyle) DxfClass() DxfClass {
	return objClass(TokenObjectVisualStyle, SubclassVisualStyle, AC1021, 0, proxyAll)
}
func (*AcdbPlaceHolder) DxfClass() DxfClass {
	return objClass(TokenObjectPlaceholder, SubclassAcDbPlaceHolder, 0, 0, 0)
}
func (*BlockReferenceObjectContextData) DxfClass() DxfClass {
	return objClass(TokenBlkRefObjectContextData, SubclassBlkRefObjectContextData, AC1021, 0, proxyECD)
}
func (*MTextAttributeObjectContextData) DxfClass() DxfClass {
	return objClass(TokenMTextAttributeObjectContextData, SubclassMTextAttributeObjectContextData, AC1021, 0, proxyECD)
}
func (*DynamicBlockPurgePreventer) DxfClass() DxfClass {
	return objClass(TokenObjectDynamicBlockPurgePreventer, SubclassAcDbDynamicBlockPurgePreventer, AC1018, 61, proxyECD)
}
func (*Field) DxfClass() DxfClass {
	return objClass(TokenObjectField, SubclassField, AC1018, 0, ProxyFlagsCloningAllowed|ProxyFlagsDisablesProxyWarningDialog)
}
func (*FieldList) DxfClass() DxfClass {
	return objClass(TokenObjectFieldList, SubclassFieldList, AC1018, 0, ProxyFlagsCloningAllowed|ProxyFlagsDisablesProxyWarningDialog)
}
func (u *UnknownObject) DxfClass() DxfClass {
	if u.Class != nil {
		return *u.Class
	}
	return objClass(u.DxfName, "", 0, 0, 0)
}
