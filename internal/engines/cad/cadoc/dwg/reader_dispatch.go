package dwg

import (
	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// readObject reads the object of a type (DwgObjectReader.readObject).
func (or *objectReader) readObject(typ cadoc.ObjectType) *tmpl {
	var t *tmpl
	switch typ {
	case cadoc.ObjectTypeText:
		t = or.readText()
	case cadoc.ObjectTypeAttrib:
		t = or.readAttribute()
	case cadoc.ObjectTypeAttdef:
		t = or.readAttributeDefinition()
	case cadoc.ObjectTypeBlock:
		t = or.readBlock()
	case cadoc.ObjectTypeEndblk:
		t = or.readEndBlock()
	case cadoc.ObjectTypeSeqend:
		t = or.readSeqend()
	case cadoc.ObjectTypeInsert:
		t = or.readInsert()
	case cadoc.ObjectTypeMInsert:
		t = or.readMInsert()
	case cadoc.ObjectTypeVertex2D:
		t = or.readVertex2D()
	case cadoc.ObjectTypeVertex3D:
		t = or.readVertex3D(cadoc.NewVertex3D())
	case cadoc.ObjectTypeVertexPFace:
		t = or.readVertex3D(cadoc.NewVertexFaceMesh())
	case cadoc.ObjectTypeVertexMesh:
		t = or.readVertex3D(cadoc.NewPolygonMeshVertex())
	case cadoc.ObjectTypeVertexPFaceFace:
		t = or.readPfaceVertex()
	case cadoc.ObjectTypePolyline2D:
		t = or.readPolyline2D()
	case cadoc.ObjectTypePolyline3D:
		t = or.readPolyline3D()
	case cadoc.ObjectTypeArc:
		t = or.readArc()
	case cadoc.ObjectTypeCircle:
		t = or.readCircle()
	case cadoc.ObjectTypeLine:
		t = or.readLine()
	case cadoc.ObjectTypeDimensionOrdinate:
		t = or.readDimOrdinate()
	case cadoc.ObjectTypeDimensionLinear:
		t = or.readDimLinear()
	case cadoc.ObjectTypeDimensionAligned:
		t = or.readDimAligned()
	case cadoc.ObjectTypeDimensionAng3Pt:
		t = or.readDimAngular3pt()
	case cadoc.ObjectTypeDimensionAng2Ln:
		t = or.readDimLine2pt()
	case cadoc.ObjectTypeDimensionRadius:
		t = or.readDimRadius()
	case cadoc.ObjectTypeDimensionDiameter:
		t = or.readDimDiameter()
	case cadoc.ObjectTypePoint:
		t = or.readPoint()
	case cadoc.ObjectTypeFace3D:
		t = or.read3dFace()
	case cadoc.ObjectTypePolylinePFace:
		t = or.readPolyfaceMesh()
	case cadoc.ObjectTypePolylineMesh:
		t = or.readPolylineMesh()
	case cadoc.ObjectTypeSolid:
		t = or.readSolid(false)
	case cadoc.ObjectTypeTrace:
		t = or.readSolid(true)
	case cadoc.ObjectTypeShape:
		t = or.readShape()
	case cadoc.ObjectTypeViewport:
		t = or.readViewport()
	case cadoc.ObjectTypeEllipse:
		t = or.readEllipse()
	case cadoc.ObjectTypeSpline:
		t = or.readSpline()
	case cadoc.ObjectTypeRegion:
		t = or.readRegion()
	case cadoc.ObjectTypeSolid3D:
		t = or.readSolid3D()
	case cadoc.ObjectTypeBody:
		t = or.readBody()
	case cadoc.ObjectTypeRay:
		t = or.readRay()
	case cadoc.ObjectTypeXLine:
		t = or.readXLine()
	case cadoc.ObjectTypeDictionary:
		t = or.readDictionary()
	case cadoc.ObjectTypeMText:
		t = or.readMText()
	case cadoc.ObjectTypeLeader:
		t = or.readLeader()
	case cadoc.ObjectTypeTolerance:
		t = or.readTolerance()
	case cadoc.ObjectTypeMLine:
		t = or.readMLine()
	case cadoc.ObjectTypeBlockControlObj:
		t = or.readBlockControlObject()
	case cadoc.ObjectTypeBlockHeader:
		t = or.readBlockHeader()
	case cadoc.ObjectTypeLayerControlObj:
		table := cadoc.NewLayersTable()
		t = readDocumentTable(or, table)
		or.b.layers = table
	case cadoc.ObjectTypeLayer:
		t = or.readLayer()
	case cadoc.ObjectTypeStyleControlObj:
		table := cadoc.NewTextStylesTable()
		t = readDocumentTable(or, table)
		or.b.textStyles = table
	case cadoc.ObjectTypeStyle:
		t = or.readTextStyle()
	case cadoc.ObjectTypeLTypeControlObj:
		t = or.readLTypeControlObject()
	case cadoc.ObjectTypeLType:
		t = or.readLType()
	case cadoc.ObjectTypeViewControlObj:
		table := cadoc.NewViewsTable()
		t = readDocumentTable(or, table)
		or.b.views = table
	case cadoc.ObjectTypeView:
		t = or.readView()
	case cadoc.ObjectTypeUCSControlObj:
		table := cadoc.NewUCSTable()
		t = readDocumentTable(or, table)
		or.b.ucss = table
	case cadoc.ObjectTypeUCS:
		t = or.readUcs()
	case cadoc.ObjectTypeVPortControlObj:
		table := cadoc.NewVPortsTable()
		t = readDocumentTable(or, table)
		or.b.vports = table
	case cadoc.ObjectTypeVPort:
		t = or.readVPort()
	case cadoc.ObjectTypeAppIdControlObj:
		table := cadoc.NewAppIdsTable()
		t = readDocumentTable(or, table)
		or.b.appIds = table
	case cadoc.ObjectTypeAppId:
		t = or.readAppId()
	case cadoc.ObjectTypeDimStyleControlObj:
		table := cadoc.NewDimensionStylesTable()
		t = readDocumentTable(or, table)
		or.b.dimensionStyles = table
	case cadoc.ObjectTypeDimStyle:
		t = or.readDimStyle()
	case cadoc.ObjectTypeVPEntHdrCtrlObj:
		return or.readViewportEntityControl()
	case cadoc.ObjectTypeVPEntHdr:
		return or.readViewportEntityHeader()
	case cadoc.ObjectTypeGroup:
		t = or.readGroup()
	case cadoc.ObjectTypeMLineStyle:
		t = or.readMLineStyle()
	case cadoc.ObjectTypeLwPolyline:
		t = or.readLWPolyline()
	case cadoc.ObjectTypeHatch:
		t = or.readHatch()
	case cadoc.ObjectTypeXRecord:
		t = or.readXRecord()
	case cadoc.ObjectTypeAcDbPlaceholder:
		t = or.readPlaceHolder()
	case cadoc.ObjectTypeLayout:
		t = or.readLayout()
	case cadoc.ObjectTypeAcadProxyEntity:
		t = or.readProxyEntity()
	case cadoc.ObjectTypeAcadProxyObject:
		t = or.readProxyObject()
	case cadoc.ObjectTypeOle2Frame:
		t = or.readOle2Frame()
	case cadoc.ObjectTypeOleFrame, cadoc.ObjectTypeDummy:
		t = or.readUnknownEntity(nil, objectTypeName(typ))
		or.notify(NotifyWarning, "Unlisted object with DXF name %s has been read as an UnknownEntity", objectTypeName(typ))
		return t
	case cadoc.ObjectTypeVbaProject, cadoc.ObjectTypeLongTransaction:
		t = or.readUnknownObject(nil, objectTypeName(typ))
		or.notify(NotifyWarning, "Unlisted object with DXF name %s has been read as an UnknownNonGraphicalObject", objectTypeName(typ))
		return t
	case cadoc.ObjectTypeUndefined, 0x3A, 0x3B, 0x36, 0x37, 9:
		// Unknown types without data.
	default:
		return or.readUnlistedType(int16(typ))
	}
	if t == nil {
		or.notify(NotifyNotImplemented, "Object type not implemented: %s", objectTypeName(typ))
	}
	return t
}

// readUnlistedType reads an object whose type is a class number.
func (or *objectReader) readUnlistedType(classNumber int16) *tmpl {
	c, ok := or.classes[classNumber]
	if !ok {
		or.notify(NotifyWarning, "Class number %d not found in classes dictionary.", classNumber)
		return nil
	}
	var t *tmpl
	switch c.DxfName {
	case "DICTIONARYWDFLT", cadoc.TokenObjectDictionaryWithDefault:
		t = or.readDictionaryWithDefault()
	case cadoc.TokenObjectPlaceholder:
		t = or.readPlaceHolder()
	case cadoc.TokenEntityArcDimension:
		t = or.readDimArc()
	case cadoc.TokenEntityTable:
		t = or.readTableEntity()
	case cadoc.TokenObjectDBColor:
		t = or.readDbColor()
	case cadoc.TokenObjectDictionaryVar:
		t = or.readDictionaryVar()
	case cadoc.TokenObjectGeoData:
		t = or.readGeoData()
	case cadoc.TokenObjectGroup:
		t = or.readGroup()
	case cadoc.TokenEntityHatch:
		t = or.readHatch()
	case cadoc.TokenEntityImage:
		img := cadoc.NewRasterImage(nil)
		t = or.readCadImage(&img.WipeoutBase, img)
	case cadoc.TokenObjectImageDefinition:
		t = or.readImageDefinition()
	case cadoc.TokenObjectImageDefinitionReactor:
		t = or.readImageDefinitionReactor()
	case cadoc.TokenObjectLayout:
		t = or.readLayout()
	case "LWPLINE", cadoc.TokenEntityLwPolyline:
		t = or.readLWPolyline()
	case cadoc.TokenObjectMaterial:
		t = or.readMaterial()
	case cadoc.TokenEntityMesh:
		t = or.readMesh()
	case cadoc.TokenEntityMultiLeader:
		t = or.readMultiLeader()
	case cadoc.TokenObjectMLeaderContextData:
		t = or.readMultiLeaderAnnotContext()
	case cadoc.TokenObjectMLeaderStyle:
		t = or.readMultiLeaderStyle()
	case cadoc.TokenObjectPdfDefinition:
		t = or.readPdfDefinition()
	case cadoc.TokenEntityPdfUnderlay, "PDFREFERENCE":
		t = or.readPdfUnderlay()
	case cadoc.TokenObjectScale:
		t = or.readScale()
	case cadoc.TokenObjectSortEntsTable:
		t = or.readSortentsTable()
	case cadoc.TokenEntitySurface, cadoc.TokenEntityExtrudedSurface, cadoc.TokenEntityLoftedSurface,
		cadoc.TokenEntityNurbSurface, cadoc.TokenEntityPlaneSurface, cadoc.TokenEntityRevolvedSurface,
		cadoc.TokenEntitySweptSurface:
		t = or.readSurface(c.DxfName)
	case cadoc.TokenObjectRasterVariables:
		t = or.readRasterVariables()
	case cadoc.TokenEntityWipeout:
		w := cadoc.NewWipeout()
		t = or.readCadImage(&w.WipeoutBase, w)
	case cadoc.TokenObjectXRecord:
		t = or.readXRecord()
	case cadoc.TokenObjectSpatialFilter:
		t = or.readSpatialFilter()
	case cadoc.TokenEntityProxyEntity:
		t = or.readProxyEntity()
	case cadoc.TokenObjectProxyObject:
		t = or.readProxyObject()
	case cadoc.TokenObjectVisualStyle:
		t = or.readVisualStyle()
	case cadoc.TokenObjectPlotSettings:
		t = or.readPlotSettings()
	case cadoc.TokenBlkRefObjectContextData:
		t = or.readBlkRefObjectContextData()
	case cadoc.TokenObjectFieldList:
		t = or.readFieldList()
	case cadoc.TokenObjectField:
		t = or.readField()
	case cadoc.TokenObjectWipeoutVariables:
		t = or.readWipeoutVariables()
	case cadoc.TokenObjectDynamicBlockPurgePreventer:
		t = or.readDynamicBlockPurgePreventer()
	case cadoc.TokenMTextAttributeObjectContextData:
		// Not read by ACadSharp either.
		return nil
	}
	// Types the model has no class for (dynamic block parameters, table
	// styles, dimension associations, AEC and mechanical objects …) keep
	// their common data only.
	if t == nil && c.IsAnEntity {
		t = or.readUnknownEntity(c, c.DxfName)
		or.notify(NotifyWarning, "Unlisted object with DXF name %s has been read as an UnknownEntity", c.DxfName)
	} else if t == nil {
		t = or.readUnknownObject(c, c.DxfName)
		or.notify(NotifyWarning, "Unlisted object with DXF name %s has been read as an UnknownNonGraphicalObject", c.DxfName)
	}
	if t != nil {
		t.class = c
	}
	return t
}

// readDsPrototype reads AcDb:AcDsPrototype_1b, the data store with the SAB
// data of AC1027+ modeler geometry.
func (r *reader) readDsPrototype() {
	var s *BitReader
	if !r.guardSection(SectionAcDsProto, func() { s = r.sectionStream(SectionAcDsProto) }) || s == nil {
		return
	}
	r.readPrototype1b(s)
}
