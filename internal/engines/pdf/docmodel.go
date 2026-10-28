// The document a PDF is read into (ReadDocument): sections with their page
// setup, paragraphs with runs of formatted text, links, tabs, line breaks and
// pictures, and ruled tables. It mirrors the C# HDocument as far as the PDF
// reader fills it; lengths are HWPUNIT (1/100 point), like there, so the
// DOCX / HWPX writers of the office engines can map it one to one.

package pdf

// Document is a read PDF.
type Document struct {
	Title    string
	Sections []*Section
}

// PageSetup is a section's paper and margins (HWPUNIT).
type PageSetup struct {
	Width, Height            int
	Left, Right, Top, Bottom int
}

type Section struct {
	Page   PageSetup
	Blocks []Block
}

// Block is a *Paragraph or a *Table.
type Block interface{ isBlock() }

type Align int

const (
	AlignLeft Align = iota
	AlignCenter
	AlignRight
	AlignJustify
)

type LineSpacingKind int

const (
	SpacingPercent LineSpacingKind = iota
	SpacingAtLeast
)

type LineSpacing struct {
	Kind  LineSpacingKind
	Value int // percent, or HWPUNIT for AtLeast
}

// TabStop is a left tab at Position (HWPUNIT from the paragraph's container).
type TabStop struct{ Position int }

type ParaFormat struct {
	Align         Align
	Left          *int // indent from the column's start
	FirstLine     *int // first-line indent relative to Left
	Tabs          []TabStop
	Before, After int
	LineSpacing   LineSpacing
	RightToLeft   bool
}

// CharFormat: Size is in HWPUNIT (100 per point), 0 when unknown; Color "#RRGGBB".
type CharFormat struct {
	Bold, Italic bool
	Size         int
	Font         string
	Color        string
}

type Paragraph struct {
	HeadingLevel    int
	Format          ParaFormat
	MarkFormat      CharFormat
	PageBreakBefore bool
	Inlines         []Inline
}

func (*Paragraph) isBlock() {}

// Inline is *Text, *Tab, *LineBreak, *Link or *Image.
type Inline interface{ isInline() }

type Text struct {
	Text   string
	Format CharFormat
}
type Tab struct{ Format CharFormat }
type LineBreak struct{ Format CharFormat }
type Link struct {
	Target  string
	Content []Inline
}

// Image is a picture file; Behind marks a page scan placed behind the
// (empty) paragraph at the paper's top left corner.
type Image struct {
	Path          string
	Width, Height int
	Behind        bool
}

func (*Text) isInline()      {}
func (*Tab) isInline()       {}
func (*LineBreak) isInline() {}
func (*Link) isInline()      {}
func (*Image) isInline()     {}

type BorderStyle int

const (
	BorderNone BorderStyle = iota
	BorderSolid
	BorderDash
)

type Border struct {
	Style   BorderStyle
	WidthMm float64
	Color   string
}

type Borders struct{ Left, Right, Top, Bottom Border }

type VerticalAlign int

const (
	VAlignTop VerticalAlign = iota
	VAlignCenter
	VAlignBottom
)

type Cell struct {
	Column, RowSpan, ColSpan int
	Borders                  Borders
	Fill                     string
	VerticalAlign            VerticalAlign
	Blocks                   []Block
}

type Row struct {
	Height int
	Cells  []*Cell
}

// Insets: Left, Right, Top, Bottom.
type Insets struct{ Left, Right, Top, Bottom int }

type Table struct {
	ColumnCount  int
	ColumnWidths []int
	CellMargin   Insets
	Align        Align
	Indent       int
	Rows         []*Row
}

func (*Table) isBlock() {}
