package cadoc

// Insert is INSERT (block reference); with more than one row or column it is
// a MINSERT.
type Insert struct {
	EntityBase
	Block         *BlockRecord
	InsertPoint   Vec3
	XScale        float64
	YScale        float64
	ZScale        float64
	Rotation      float64 // radians
	Normal        Vec3
	ColumnCount   uint16
	RowCount      uint16
	ColumnSpacing float64
	RowSpacing    float64
	// Attributes are the ATTRIB entities; AttributesSeqend ends them.
	Attributes       []*AttributeEntity
	AttributesSeqend *Seqend
}

// InitInsert sets ACadSharp's defaults.
func (i *Insert) InitInsert() {
	i.InitEntity()
	i.XScale, i.YScale, i.ZScale = 1, 1, 1
	i.Normal = AxisZ
	i.ColumnCount, i.RowCount = 1, 1
}

func NewInsert(block *BlockRecord) *Insert { i := &Insert{Block: block}; i.InitInsert(); return i }

func (*Insert) ObjectName() string { return TokenEntityInsert }
func (i *Insert) SubclassMarker() string {
	if i.IsMultiple() {
		return SubclassMInsert
	}
	return SubclassInsert
}
func (i *Insert) ObjectType() ObjectType {
	if i.IsMultiple() {
		return ObjectTypeMInsert
	}
	return ObjectTypeInsert
}

// IsMultiple reports a MINSERT (more than one row or column).
func (i *Insert) IsMultiple() bool { return i.RowCount > 1 || i.ColumnCount > 1 }

// HasAttributes reports attached attributes.
func (i *Insert) HasAttributes() bool { return len(i.Attributes) > 0 }

// AddAttribute attaches an attribute.
func (i *Insert) AddAttribute(a *AttributeEntity) {
	a.Owner = i
	i.Attributes = append(i.Attributes, a)
}

// BlockReference returns the INSERT data of an INSERT or ACAD_TABLE.
func (i *Insert) BlockReference() *Insert { return i }

// SpatialFilter returns the XCLIP filter (ACAD_FILTER/SPATIAL), or nil.
func (i *Insert) SpatialFilter() *SpatialFilter {
	if i.XDictionary == nil {
		return nil
	}
	f, ok := i.XDictionary.GetDictionary("ACAD_FILTER")
	if !ok {
		return nil
	}
	o, ok := f.Get("SPATIAL")
	if !ok {
		return nil
	}
	s, _ := o.(*SpatialFilter)
	return s
}

// TableEntity is ACAD_TABLE. It is a block reference to the anonymous block
// that draws the table (that is how it renders); the table structure is
// read as far as rows, columns and the text of each cell. Writers skip it,
// as ACadSharp does.
type TableEntity struct {
	Insert
	Version                  int16
	HorizontalDirection      Vec3
	ValueFlag                int32
	OverrideFlag             bool
	OverrideBorderColor      bool
	OverrideBorderLineWeight bool
	OverrideBorderVisibility bool
	// Style is the TABLESTYLE object (kept unparsed).
	Style      Object
	TableBlock *BlockRecord
	Rows       []TableRow
	Columns    []TableColumn
	// Cells are in row-major order.
	Cells []TableCell
}

// TableRow is TableEntity.Row.
type TableRow struct{ Height float64 }

// TableColumn is TableEntity.Column.
type TableColumn struct{ Width float64 }

// TableCell is the part of TableEntity.Cell that is read.
type TableCell struct {
	Type      TableCellType
	EdgeFlags int16
	Merged    int16
	AutoFit   bool
	Rotation  float64
	// Text is the cell text (MTEXT codes kept).
	Text string
}

func NewTableEntity() *TableEntity {
	t := &TableEntity{HorizontalDirection: AxisX}
	t.InitInsert()
	return t
}

func (*TableEntity) ObjectName() string     { return TokenEntityTable }
func (*TableEntity) SubclassMarker() string { return SubclassTableEntity }
func (*TableEntity) ObjectType() ObjectType { return ObjectTypeUnlisted }

func (*TableEntity) DxfClass() DxfClass {
	return entClass(TokenEntityTable, SubclassTableEntity, AC1018, proxyEraseDis)
}
