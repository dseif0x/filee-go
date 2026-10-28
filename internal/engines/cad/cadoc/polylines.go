package cadoc

// LwPolyline is LWPOLYLINE (2D polyline in its OCS).
type LwPolyline struct {
	EntityBase
	Flags         LwPolylineFlags
	ConstantWidth float64
	Elevation     float64
	Thickness     float64
	Normal        Vec3
	Vertices      []LwPolylineVertex
}

// LwPolylineVertex is LwPolyline.Vertex.
type LwPolylineVertex struct {
	Location     Vec2
	StartWidth   float64
	EndWidth     float64
	Bulge        float64
	CurveTangent float64
	Flags        VertexFlags
	Id           int32
}

func NewLwPolyline() *LwPolyline { e := &LwPolyline{Normal: AxisZ}; e.InitEntity(); return e }

func (*LwPolyline) ObjectName() string     { return TokenEntityLwPolyline }
func (*LwPolyline) SubclassMarker() string { return SubclassLwPolyline }
func (*LwPolyline) ObjectType() ObjectType { return ObjectTypeLwPolyline }

// IsClosed reports the closed flag.
func (p *LwPolyline) IsClosed() bool { return p.Flags&LwPolylineFlagsClosed != 0 }

// SetClosed sets or clears the closed flag.
func (p *LwPolyline) SetClosed(closed bool) {
	if closed {
		p.Flags |= LwPolylineFlagsClosed
	} else {
		p.Flags &^= LwPolylineFlagsClosed
	}
}

// PolylineBase is the POLYLINE data shared by the four polyline kinds.
type PolylineBase struct {
	EntityBase
	Flags         PolylineFlags
	Elevation     float64
	Thickness     float64
	Normal        Vec3
	StartWidth    float64
	EndWidth      float64
	SmoothSurface SmoothSurfaceType
	// Seqend ends the vertices.
	Seqend *Seqend
}

func (p *PolylineBase) initPolyline() { p.InitEntity(); p.Normal = AxisZ }

// Polyline returns the shared POLYLINE data.
func (p *PolylineBase) Polyline() *PolylineBase { return p }

// IsClosed reports the closed flag (closed in M for meshes).
func (p *PolylineBase) IsClosed() bool {
	return p.Flags&PolylineFlagsClosedPolylineOrClosedPolygonMeshInM != 0
}

func (*PolylineBase) ObjectName() string { return TokenEntityPolyline }

// Polyline is implemented by the four POLYLINE kinds.
type Polyline interface {
	Entity
	Polyline() *PolylineBase
	// VertexEntities returns the vertices (and, for polyface meshes, the
	// face records after them) as entities.
	VertexEntities() []Entity
}

// VertexBase is the VERTEX data.
type VertexBase struct {
	EntityBase
	Location     Vec3
	StartWidth   float64
	EndWidth     float64
	Bulge        float64
	Flags        VertexFlags
	CurveTangent float64 // radians
	Id           int32
}

func (*VertexBase) ObjectName() string { return TokenEntityVertex }

// Vertex returns the VERTEX data.
func (v *VertexBase) Vertex() *VertexBase { return v }

// Vertex is implemented by the VERTEX kinds.
type Vertex interface {
	Entity
	Vertex() *VertexBase
}

// Polyline2D is a 2D POLYLINE (AcDb2dPolyline).
type Polyline2D struct {
	PolylineBase
	Vertices []*Vertex2D
}

func NewPolyline2D() *Polyline2D { p := &Polyline2D{}; p.initPolyline(); return p }

func (*Polyline2D) SubclassMarker() string { return SubclassPolyline }
func (*Polyline2D) ObjectType() ObjectType { return ObjectTypePolyline2D }
func (p *Polyline2D) VertexEntities() []Entity {
	out := make([]Entity, len(p.Vertices))
	for i, v := range p.Vertices {
		out[i] = v
	}
	return out
}

// Polyline3D is a 3D POLYLINE (AcDb3dPolyline).
type Polyline3D struct {
	PolylineBase
	Vertices []*Vertex3D
}

func NewPolyline3D() *Polyline3D {
	p := &Polyline3D{}
	p.initPolyline()
	p.Flags = PolylineFlagsPolyline3D
	return p
}

func (*Polyline3D) SubclassMarker() string { return SubclassPolyline3d }
func (*Polyline3D) ObjectType() ObjectType { return ObjectTypePolyline3D }
func (p *Polyline3D) VertexEntities() []Entity {
	out := make([]Entity, len(p.Vertices))
	for i, v := range p.Vertices {
		out[i] = v
	}
	return out
}

// PolyfaceMesh is a polyface mesh POLYLINE: vertices and face records.
type PolyfaceMesh struct {
	PolylineBase
	Vertices []*VertexFaceMesh
	Faces    []*VertexFaceRecord
}

func NewPolyfaceMesh() *PolyfaceMesh {
	p := &PolyfaceMesh{}
	p.initPolyline()
	p.Flags = PolylineFlagsPolyfaceMesh
	return p
}

func (*PolyfaceMesh) SubclassMarker() string { return SubclassPolyfaceMesh }
func (*PolyfaceMesh) ObjectType() ObjectType { return ObjectTypePolylinePFace }
func (p *PolyfaceMesh) VertexEntities() []Entity {
	out := make([]Entity, 0, len(p.Vertices)+len(p.Faces))
	for _, v := range p.Vertices {
		out = append(out, v)
	}
	for _, f := range p.Faces {
		out = append(out, f)
	}
	return out
}

// PolygonMesh is an M×N polygon mesh POLYLINE.
type PolygonMesh struct {
	PolylineBase
	MVertexCount          int16
	NVertexCount          int16
	MSmoothSurfaceDensity int16
	NSmoothSurfaceDensity int16
	Vertices              []*PolygonMeshVertex
}

func NewPolygonMesh() *PolygonMesh {
	p := &PolygonMesh{}
	p.initPolyline()
	p.Flags = PolylineFlagsPolygonMesh
	return p
}

func (*PolygonMesh) SubclassMarker() string { return SubclassPolygonMesh }
func (*PolygonMesh) ObjectType() ObjectType { return ObjectTypePolylineMesh }
func (p *PolygonMesh) VertexEntities() []Entity {
	out := make([]Entity, len(p.Vertices))
	for i, v := range p.Vertices {
		out[i] = v
	}
	return out
}

// Vertex2D is a vertex of a 2D polyline (OCS).
type Vertex2D struct{ VertexBase }

func NewVertex2D() *Vertex2D { v := &Vertex2D{}; v.InitEntity(); return v }

func (*Vertex2D) SubclassMarker() string { return SubclassPolylineVertex }
func (*Vertex2D) ObjectType() ObjectType { return ObjectTypeVertex2D }

// Vertex3D is a vertex of a 3D polyline.
type Vertex3D struct{ VertexBase }

func NewVertex3D() *Vertex3D {
	v := &Vertex3D{}
	v.InitEntity()
	v.Flags = VertexFlagsPolylineVertex3D
	return v
}

func (*Vertex3D) SubclassMarker() string { return SubclassPolyline3dVertex }
func (*Vertex3D) ObjectType() ObjectType { return ObjectTypeVertex3D }

// VertexFaceMesh is a vertex of a polyface mesh.
type VertexFaceMesh struct{ VertexBase }

func NewVertexFaceMesh() *VertexFaceMesh {
	v := &VertexFaceMesh{}
	v.InitEntity()
	v.Flags = VertexFlagsPolygonMesh3D | VertexFlagsPolyFaceMeshVertex
	return v
}

func (*VertexFaceMesh) SubclassMarker() string { return SubclassPolyfaceMeshVertex }
func (*VertexFaceMesh) ObjectType() ObjectType { return ObjectTypeVertexPFace }

// VertexFaceRecord is a face of a polyface mesh: 1-based vertex indexes, a
// negative index hides the edge starting at that vertex, 0 means unused.
type VertexFaceRecord struct {
	VertexBase
	Index1, Index2, Index3, Index4 int16
}

func NewVertexFaceRecord() *VertexFaceRecord {
	v := &VertexFaceRecord{}
	v.InitEntity()
	v.Flags = VertexFlagsPolyFaceMeshVertex
	return v
}

func (*VertexFaceRecord) SubclassMarker() string { return SubclassPolyfaceMeshFace }
func (*VertexFaceRecord) ObjectType() ObjectType { return ObjectTypeVertexPFaceFace }

// PolygonMeshVertex is a vertex of a polygon mesh.
type PolygonMeshVertex struct{ VertexBase }

func NewPolygonMeshVertex() *PolygonMeshVertex {
	v := &PolygonMeshVertex{}
	v.InitEntity()
	v.Flags = VertexFlagsPolygonMesh3D
	return v
}

func (*PolygonMeshVertex) SubclassMarker() string { return SubclassPolygonMeshVertex }
func (*PolygonMeshVertex) ObjectType() ObjectType { return ObjectTypeVertexMesh }
