// Package doc is the in-memory document model between the document readers
// (DOCX, Markdown, plain text, spreadsheets, OFD, PDF, HWPX, …) and the
// writers (DOCX, HWPX, HTML, Markdown, plain text). It is the Go form of
// Filee's HDocument (Hwp/Hwpx/HwpxModel.cs).
//
// Lengths are HWPUNIT (1/7200 inch) and font sizes are 1/100 pt, like OWPML
// itself. A nil format field means "inherit from the style".
package doc

// Document is a document ready to be written.
type Document struct {
	Title    string
	Sections []*Section
}

// Section is a run of pages sharing page setup, headers and footers (한글 "구역").
type Section struct {
	// Page is the paper and margins; nil keeps the writer's A4 page.
	Page    *Page
	Columns Columns
	Headers []*HeaderFooter
	Footers []*HeaderFooter
	// HideFirstHeader / HideFirstFooter hide them on the first page.
	HideFirstHeader bool
	HideFirstFooter bool
	// StartPageNumber restarts numbering; nil continues.
	StartPageNumber *int
	Blocks          []Block
}

// NewSection returns a section with single columns.
func NewSection() *Section { return &Section{Columns: SingleColumn} }

// Page is paper size and margins. Width and Height are as printed.
type Page struct {
	Width, Height, Left, Right, Top, Bottom, Header, Footer, Gutter int
}

func (p *Page) Landscape() bool { return p.Width > p.Height }
func (p *Page) TextWidth() int  { return max(2000, p.Width-p.Left-p.Right-p.Gutter) }

// Columns is newspaper-style columns.
type Columns struct {
	Count, Gap int
	Separator  bool
}

// SingleColumn is the default layout.
var SingleColumn = Columns{Count: 1}

// PageType tells which pages a header or footer applies to.
type PageType int

const (
	BothPages PageType = iota
	EvenPages
	OddPages
)

type HeaderFooter struct {
	Pages  PageType
	Blocks []Block
}

// ---- blocks ----

// Block is a *Paragraph or a *Table.
type Block interface{ isBlock() }

type Paragraph struct {
	Format ParaFormat
	// MarkFormat is the format of the paragraph mark (height of an empty paragraph).
	MarkFormat CharFormat
	// HeadingLevel is 1..9 for headings, else 0.
	HeadingLevel      int
	List              *ListRef
	PageBreakBefore   bool
	ColumnBreakBefore bool
	// ColumnsChange: columns change here without a new page; nil = unchanged.
	ColumnsChange *Columns
	Inlines       []Inline
}

func (*Paragraph) isBlock() {}

// ListRef is the list a paragraph belongs to.
type ListRef struct {
	Numbering *Numbering
	Level     int // 0-based
	// Numbered is false for continuation paragraphs of an item.
	Numbered bool
}

// Numbering is a list definition, one entry per nesting level.
type Numbering struct{ Levels []NumberingLevel }

// NumberingLevel: Format is an OWPML numFormat (DIGIT, LATIN_SMALL, …); Text
// holds ^n for the level-n counter ("^1.") or the bullet character.
type NumberingLevel struct {
	Format string
	Text   string
	Start  int
	Bullet bool
}

type Table struct {
	Rows []*Row
	// ColumnWidths are absolute widths; nil = RelativeWidths or equal shares.
	ColumnWidths []int
	// RelativeWidths are fractions of the text width; zero entries share the rest.
	RelativeWidths  []float64
	ColumnCount     int
	Align           Align
	Indent          int
	CellMargin      *Insets
	Borders         *Borders
	PageBreakBefore bool
	// Tight tables take no more height than their rows (fixed layouts such as OFD).
	Tight   bool
	Caption []Block
}

func (*Table) isBlock() {}

type Row struct {
	Cells []*Cell
	// Height is a minimum row height; nil = fit content.
	Height *int
	Header bool
}

type Cell struct {
	// Column is the grid column; -1 = right after the previous cell.
	Column        int
	RowSpan       int
	ColSpan       int
	VerticalAlign VerticalAlign
	Fill          string // "#RRGGBB" or ""
	Borders       *Borders
	CellMargin    *Insets
	Blocks        []Block
}

// NewCell returns a cell placed after the previous one, spanning 1×1.
func NewCell() *Cell { return &Cell{Column: -1, RowSpan: 1, ColSpan: 1} }

type VerticalAlign int

const (
	VAlignTop VerticalAlign = iota
	VAlignCenter
	VAlignBottom
)

type Insets struct{ Left, Right, Top, Bottom int }

// BorderStyle of a line.
type BorderStyle int

const (
	BorderNone BorderStyle = iota
	BorderSolid
	BorderDash
	BorderDot
	BorderDashDot
	BorderDouble
)

// Border is one line; WidthMm is in millimetres.
type Border struct {
	Style   BorderStyle
	WidthMm float64
	Color   string
}

var (
	NoBorder   = Border{BorderNone, 0.1, "#000000"}
	ThinBorder = Border{BorderSolid, 0.12, "#000000"}
)

type Borders struct{ Left, Right, Top, Bottom Border }

var (
	GridBorders  = Borders{ThinBorder, ThinBorder, ThinBorder, ThinBorder}
	EmptyBorders = Borders{NoBorder, NoBorder, NoBorder, NoBorder}
)

// ---- inlines ----

// Inline is one of the inline types below.
type Inline interface{ isInline() }

type Text struct {
	Text   string
	Format CharFormat
}

// Equation is Word math: Linear is its linear text form, which writers
// without equations write; Math is the reader's tree (opaque here).
type Equation struct {
	Text
	Math any
}

type LineBreak struct{ Format CharFormat }
type Tab struct{ Format CharFormat }

// Link is a hyperlink to a URL, or to a bookmark when Target starts with '#'.
type Link struct {
	Target  string
	Content []Inline
}

type Bookmark struct{ Name string }

type Note struct {
	Endnote bool
	Blocks  []Block
}

type FieldKind int

const (
	PageNumberField FieldKind = iota
	TotalPagesField
)

// Field is an automatic number (page number in headers and footers).
type Field struct {
	Kind   FieldKind
	Format CharFormat
}

// Image is a picture from a local file. Width/Height nil = natural size.
type Image struct {
	Path          string
	Width, Height *int
	Anchor        *Anchor
}

type ShapeKind int

const (
	ShapeRectangle ShapeKind = iota
	ShapeEllipse
	ShapeLine
)

// TextBox is a box with its own paragraphs.
type TextBox struct {
	Width, Height int
	Anchor        *Anchor
	Fill          string
	Gradient      *Gradient
	Line          Border
	Shape         ShapeKind
	CornerRatio   int
	Padding       Insets
	VerticalAlign VerticalAlign
	Blocks        []Block
}

// NewTextBox returns a box with the default thin line and padding.
func NewTextBox() *TextBox {
	return &TextBox{Line: ThinBorder, Padding: Insets{283, 283, 283, 283}}
}

// Shape is a drawing without text.
type Shape struct {
	Kind           ShapeKind
	Width, Height  int
	Anchor         *Anchor
	Fill           string
	Gradient       *Gradient
	Line           Border
	CornerRatio    int
	FlipHorizontal bool
	FlipVertical   bool
}

// Gradient is a linear gradient: colours from start to end, angle in degrees.
type Gradient struct {
	Colors []string
	Angle  int
}

// Anchor positions a floating object (OWPML horzRelTo / vertRelTo / textWrap values).
type Anchor struct {
	HorizontalRelativeTo string // PAPER, PAGE, COLUMN, PARA
	HorizontalAlign      string // LEFT, CENTER, RIGHT, INSIDE, OUTSIDE
	HorizontalOffset     int
	VerticalRelativeTo   string // PAPER, PAGE, PARA
	VerticalAlign        string // TOP, CENTER, BOTTOM, INSIDE, OUTSIDE
	VerticalOffset       int
	Wrap                 string // SQUARE, TOP_AND_BOTTOM, BEHIND_TEXT, IN_FRONT_OF_TEXT
	AllowOverlap         bool
}

func (*Text) isInline()      {}
func (*Equation) isInline()  {}
func (*LineBreak) isInline() {}
func (*Tab) isInline()       {}
func (*Link) isInline()      {}
func (*Bookmark) isInline()  {}
func (*Note) isInline()      {}
func (*Field) isInline()     {}
func (*Image) isInline()     {}
func (*TextBox) isInline()   {}
func (*Shape) isInline()     {}

// ---- formats ----

type Align int

const (
	AlignLeft Align = iota
	AlignCenter
	AlignRight
	AlignJustify
	AlignDistribute
)

type LineSpacingKind int

const (
	// SpacingPercent is percent of the font size (한글 default 160 %).
	SpacingPercent LineSpacingKind = iota
	SpacingFixed
	SpacingAtLeast
)

type LineSpacing struct {
	Kind  LineSpacingKind
	Value int
}

type TabKind int

const (
	TabLeft TabKind = iota
	TabRight
	TabCenter
	TabDecimal
)

// TabStop: Leader is an OWPML line type (NONE, DOT, DASH, SOLID …).
type TabStop struct {
	Position int
	Kind     TabKind
	Leader   string
}

// ParaFormat is paragraph formatting; nil members keep the style's value.
// FirstLine is relative to Left (negative = hanging). RightToLeft paragraphs
// name the start and end side with Align, Left and Right.
type ParaFormat struct {
	Align        *Align
	Left, Right  *int
	FirstLine    *int
	Before       *int
	After        *int
	LineSpacing  *LineSpacing
	KeepWithNext *bool
	KeepLines    *bool
	WidowOrphan  *bool
	Tabs         []TabStop
	RightToLeft  *bool
}

// CharFormat is character formatting; nil members keep the style's value.
// Size is 1/100 pt; Color and Shade are "#RRGGBB".
type CharFormat struct {
	Bold, Italic, Underline, Strike *bool
	Superscript, Subscript          *bool
	Size                            *int
	Font                            *string
	EastAsianFont                   *string
	Color                           *string
	Shade                           *string
	Spacing                         *int
}

// IsEmpty reports whether nothing is set.
func (f CharFormat) IsEmpty() bool { return f == CharFormat{} }

// Ptr returns a pointer to v (for the optional format fields).
func Ptr[T any](v T) *T { return &v }

// Val returns *p, or def when p is nil.
func Val[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}
