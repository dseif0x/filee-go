package cadoc

import "strings"

// TextEntity is TEXT; attributes embed it.
type TextEntity struct {
	EntityBase
	Value          string
	InsertPoint    Vec3
	AlignmentPoint Vec3
	Normal         Vec3
	Height         float64
	WidthFactor    float64
	// Rotation and ObliqueAngle are in radians.
	Rotation            float64
	ObliqueAngle        float64
	Thickness           float64
	Style               *TextStyle
	Mirror              TextMirrorFlag
	HorizontalAlignment TextHorizontalAlignment
	VerticalAlignment   TextVerticalAlignmentType
}

// InitText sets ACadSharp's defaults.
func (t *TextEntity) InitText() {
	t.InitEntity()
	t.Normal, t.Height, t.WidthFactor = AxisZ, 1, 1
}

func NewText() *TextEntity { t := &TextEntity{}; t.InitText(); return t }

func (*TextEntity) ObjectName() string     { return TokenEntityText }
func (*TextEntity) SubclassMarker() string { return SubclassText }
func (*TextEntity) ObjectType() ObjectType { return ObjectTypeText }

// Text returns the TEXT data of a TEXT, ATTRIB or ATTDEF.
func (t *TextEntity) Text() *TextEntity { return t }

// TextLike is implemented by TEXT, ATTRIB and ATTDEF.
type TextLike interface {
	Entity
	Text() *TextEntity
}

// AttributeBase is the shared data of ATTRIB and ATTDEF.
type AttributeBase struct {
	TextEntity
	Tag           string
	Flags         AttributeFlags
	AttributeType AttributeType
	IsLocked      bool
	Version       uint8
	// MText is the multiline content of a multiline attribute (R2018+).
	MText *MText
}

// Attribute returns the attribute data.
func (a *AttributeBase) Attribute() *AttributeBase { return a }

// AttributeEntity is ATTRIB, an attribute value of an INSERT.
type AttributeEntity struct{ AttributeBase }

func NewAttribute() *AttributeEntity { a := &AttributeEntity{}; a.InitText(); return a }

func (*AttributeEntity) ObjectName() string     { return TokenEntityAttribute }
func (*AttributeEntity) SubclassMarker() string { return SubclassAttribute }
func (*AttributeEntity) ObjectType() ObjectType { return ObjectTypeAttrib }

// AttributeDefinition is ATTDEF.
type AttributeDefinition struct {
	AttributeBase
	Prompt string
}

func NewAttributeDefinition() *AttributeDefinition {
	a := &AttributeDefinition{}
	a.InitText()
	return a
}

func (*AttributeDefinition) ObjectName() string     { return TokenEntityAttributeDefinition }
func (*AttributeDefinition) SubclassMarker() string { return SubclassAttributeDefinition }
func (*AttributeDefinition) ObjectType() ObjectType { return ObjectTypeAttdef }

// MText is MTEXT.
type MText struct {
	EntityBase
	// Value is the raw MTEXT string with its formatting codes.
	Value       string
	InsertPoint Vec3
	// AlignmentPoint is the X axis direction (code 11).
	AlignmentPoint   Vec3
	Normal           Vec3
	Height           float64
	RectangleWidth   float64
	RectangleHeight  float64
	HorizontalWidth  float64
	VerticalHeight   float64
	LineSpacing      float64
	LineSpacingStyle LineSpacingStyleType
	AttachmentPoint  AttachmentPointType
	DrawingDirection DrawingDirectionType
	Style            *TextStyle
	// Background fill (R2004+) and text frame (R2018+).
	BackgroundFillFlags    BackgroundFillFlags
	BackgroundScale        float64
	BackgroundColor        Color
	BackgroundTransparency Transparency
	IsAnnotative           bool
	ColumnData             TextColumnData
}

// TextColumnData is MText.TextColumnData.
type TextColumnData struct {
	ColumnType   ColumnType
	ColumnCount  int32
	FlowReversed bool
	AutoHeight   bool
	Width        float64
	Gutter       float64
	Heights      []float64
}

func NewMText() *MText {
	m := &MText{AlignmentPoint: AxisX, Normal: AxisZ, Height: 1, LineSpacing: 1,
		AttachmentPoint: AttachmentPointTypeTopLeft, DrawingDirection: DrawingDirectionTypeLeftToRight,
		BackgroundScale: 1.5, HorizontalWidth: 0.9, VerticalHeight: 0.2}
	m.InitEntity()
	return m
}

func (*MText) ObjectName() string     { return TokenEntityMText }
func (*MText) SubclassMarker() string { return SubclassMText }
func (*MText) ObjectType() ObjectType { return ObjectTypeMText }

// HasColumns reports column data.
func (m *MText) HasColumns() bool { return m.ColumnData.ColumnType != ColumnTypeNoColumns }

// PlainTextValue strips the most common MTEXT formatting codes (a rough
// equivalent of ACadSharp's PlainText; renderers parse Value themselves).
func (m *MText) PlainTextValue() string {
	var b strings.Builder
	s := m.Value
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '{', '}':
			continue
		case '\\':
			if i+1 >= len(s) {
				continue
			}
			n := s[i+1]
			switch n {
			case 'P', 'X':
				b.WriteByte('\n')
				i++
			case '~':
				b.WriteByte(' ')
				i++
			case '\\', '{', '}':
				b.WriteByte(n)
				i++
			case 'L', 'l', 'O', 'o', 'K', 'k':
				i++
			default:
				if j := strings.IndexByte(s[i:], ';'); j > 0 {
					i += j
				} else {
					i++
				}
			}
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
