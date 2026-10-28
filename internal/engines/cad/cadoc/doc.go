// Package cadoc is the CAD document model: a Go port of the object model of
// ACadSharp (MIT). DXF IO lives in the dxf subpackage; DWG readers and writers
// are meant to target the same model.
//
// Mapping of ACadSharp types to this package:
//
//	CadDocument                  Document (NewDocument, NewEmptyDocument, CreateDefaults)
//	CadHeader                    Header (+ HeaderVariables: $NAME ↔ field ↔ DXF codes)
//	ACadVersion                  Version (AC1009 … AC1032)
//	CadObject                    Object interface + ObjectBase (Handle, Owner, Reactors, XDictionary, ExtendedData)
//	Entity                       Entity interface + EntityBase
//	TableEntry / Table<T>        TableEntry interface + TableEntryBase / Table[T] (LayersTable = Table[*Layer], …)
//	NonGraphicalObject           NonGraphicalObject interface + NonGraphicalBase (Name = dictionary key)
//	CadDictionary(+WithDefault)  Dictionary, DictionaryWithDefault
//	DxfClass(Collection)         DxfClass, DxfClassCollection
//	ExtendedData(Record)         ExtendedData, ExtendedDataRecord (typed by code)
//	Color, Transparency          Color, Transparency (zero values are ByLayer)
//	LineWeightType               LineWeight
//	XY, XYZ                      Vec2, Vec3
//	Hatch.BoundaryPath.*         HatchBoundaryPath, HatchLineEdge, HatchArcEdge, …
//	LwPolyline.Vertex            LwPolylineVertex
//	Polyline<T>                  PolylineBase embedded in Polyline2D/Polyline3D/PolyfaceMesh/PolygonMesh
//	Vertex*                      VertexBase embedded in Vertex2D/Vertex3D/VertexFaceMesh/VertexFaceRecord/PolygonMeshVertex
//	AttributeBase                AttributeBase embedded in AttributeEntity/AttributeDefinition (both embed TextEntity)
//	Dimension                    Dimension interface + DimensionBase; DimensionAligned … DimensionArc
//	MultiLeaderObjectContextData MultiLeaderObjectContextData (+ MultiLeaderLeaderRoot/LeaderLine)
//	CadWipeoutBase               WipeoutBase embedded in RasterImage/Wipeout
//	ModelerGeometry              ModelerGeometry embedded in Region/Solid3D/CadBody/Surface
//	UnknownEntity/NonGraphical   UnknownEntity / UnknownObject (raw group codes kept)
//	enums (LayerFlags, …)        same names; members are TypeMember (LayerFlagsFrozen)
//
// References are Go pointers (Entity.Layer, Insert.Block, …). Handles are
// kept from the file; Document.AssignHandles gives new or duplicated objects
// free handles and sets $HANDSEED, and Document.UpdateOwners derives every
// owner from the structure, so code that builds or edits a document only has
// to place objects (BlockRecord.AddEntity, Table.Add, Dictionary.Add).
//
// Angles are radians throughout, as in ACadSharp; DXF IO converts degrees.
//
// Adding an entity type: define the struct embedding EntityBase with
// ObjectName/SubclassMarker/ObjectType methods (and DxfClass when the type
// needs a CLASSES entry), then register a reader with registerEntity and a
// writer with registerEntityWriter in package dxf. Objects work the same way
// with NonGraphicalBase, registerObject and registerObjectWriter.
package cadoc
