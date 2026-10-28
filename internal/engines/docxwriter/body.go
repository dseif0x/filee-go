package docxwriter

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/dseif0x/filee-go/internal/doc"
)

// Document body: sections (page setup, columns, headers and footers),
// paragraphs, runs and tables. Word keeps a section's properties at its end:
// in the last paragraph of the section and, for the last section, as the last
// child of w:body. Columns that change mid-page become continuous section
// breaks, as Word does it.

// defaultPage is A4 with one-inch margins and headers / footers 1.5 cm from the edge.
var defaultPage = &doc.Page{Width: 59528, Height: 84188, Left: 7200, Right: 7200, Top: 4255, Bottom: 4255, Header: 2945, Footer: 2945}

// highlights are Word's highlight colours by "#RRGGBB".
var highlights = map[string]string{
	"#FFFF00": "yellow", "#00FF00": "green", "#00FFFF": "cyan", "#FF00FF": "magenta", "#0000FF": "blue", "#FF0000": "red",
	"#000080": "darkBlue", "#008080": "darkCyan", "#008000": "darkGreen", "#800080": "darkMagenta", "#800000": "darkRed",
	"#808000": "darkYellow", "#808080": "darkGray", "#C0C0C0": "lightGray", "#000000": "black",
}

// runContext tells what the paragraph style already sets (explicit "off" values override it).
type runContext struct{ styleBold, link bool }

func (w *writer) body(d *doc.Document) string {
	sections := d.Sections
	if len(sections) == 0 {
		sections = []*doc.Section{doc.NewSection()}
	}
	for _, s := range sections {
		for _, h := range append(slices.Clone(s.Headers), s.Footers...) {
			if h.Pages == doc.EvenPages {
				w.evenAndOdd = true
			}
		}
	}
	var sb strings.Builder
	for si, s := range sections {
		page := s.Page
		if page == nil {
			page = defaultPage
		}
		parts := splitAtColumnChanges(s)
		for pi, p := range parts {
			w.width = page.TextWidth()
			sectPr := w.sectionProperties(s, page, p.columns, pi > 0)
			w.width = columnWidth(page.TextWidth(), p.columns)
			if si == len(sections)-1 && pi == len(parts)-1 {
				sb.WriteString(w.blocks(p.blocks, "", true, "", ""))
				sb.WriteString(sectPr)
			} else {
				sb.WriteString(w.blocks(p.blocks, sectPr, false, "", ""))
			}
		}
	}
	return sb.String()
}

type sectionPart struct {
	columns doc.Columns
	blocks  []doc.Block
}

// splitAtColumnChanges splits a section where its columns change.
func splitAtColumnChanges(s *doc.Section) []sectionPart {
	var parts []sectionPart
	columns := s.Columns
	var cur []doc.Block
	for _, b := range s.Blocks {
		if p, ok := b.(*doc.Paragraph); ok && p.ColumnsChange != nil {
			if len(cur) > 0 {
				parts = append(parts, sectionPart{columns, cur})
				cur = nil
			}
			columns = *p.ColumnsChange
		}
		cur = append(cur, b)
	}
	return append(parts, sectionPart{columns, cur})
}

func columnWidth(text int, c doc.Columns) int {
	if c.Count <= 1 {
		return text
	}
	return max(2000, (text-c.Gap*(c.Count-1))/c.Count)
}

// sectionProperties is w:sectPr in schema order: references, type, pgSz, pgMar, pgNumType, cols, titlePg.
func (w *writer) sectionProperties(s *doc.Section, page *doc.Page, columns doc.Columns, continuous bool) string {
	var sb strings.Builder
	sb.WriteString("<w:sectPr>")
	titlePage := !continuous && (s.HideFirstHeader || s.HideFirstFooter)
	if !continuous {
		w.headerFooterReferences(&sb, "header", s.Headers, s.HideFirstHeader, titlePage, &w.headerTypes)
		w.headerFooterReferences(&sb, "footer", s.Footers, s.HideFirstFooter, titlePage, &w.footerTypes)
	} else {
		sb.WriteString(`<w:type w:val="continuous"/>`)
	}
	// The model's Top / Bottom run from the paper edge to the header / footer and Header / Footer are their
	// heights (as in 한글); Word's top / bottom margins reach the body and header / footer are the distances.
	orient := ""
	if page.Landscape() {
		orient = ` w:orient="landscape"`
	}
	fmt.Fprintf(&sb, `<w:pgSz w:w="%d" w:h="%d"%s/>`, twips(page.Width), twips(page.Height), orient)
	fmt.Fprintf(&sb, `<w:pgMar w:top="%d" w:right="%d" w:bottom="%d" w:left="%d" w:header="%d" w:footer="%d" w:gutter="%d"/>`,
		twips(page.Top+page.Header), twips(page.Right), twips(page.Bottom+page.Footer), twips(page.Left), twips(page.Top), twips(page.Bottom), twips(page.Gutter))
	if !continuous && s.StartPageNumber != nil {
		fmt.Fprintf(&sb, `<w:pgNumType w:start="%d"/>`, *s.StartPageNumber)
	}
	if columns.Count > 1 {
		sep := ""
		if columns.Separator {
			sep = ` w:sep="1"`
		}
		fmt.Fprintf(&sb, `<w:cols w:num="%d" w:space="%d"%s/>`, min(columns.Count, 45), twips(columns.Gap), sep)
	} else {
		sb.WriteString(`<w:cols w:space="720"/>`)
	}
	if titlePage {
		sb.WriteString("<w:titlePg/>")
	}
	sb.WriteString("</w:sectPr>")
	return sb.String()
}

// headerFooterReferences: a section without a reference would inherit the
// previous one's, so a type used before gets an empty part; "different first
// page" gets a first header that is empty or a copy of the default one.
func (w *writer) headerFooterReferences(sb *strings.Builder, kind string, items []*doc.HeaderFooter, hideFirst, titlePage bool, used *[]string) {
	var order []string
	byType := map[string][]doc.Block{}
	set := func(t string, b []doc.Block) {
		if _, ok := byType[t]; !ok {
			order = append(order, t)
		}
		byType[t] = b
	}
	for _, it := range items {
		if it.Pages == doc.EvenPages {
			set("even", it.Blocks)
		} else {
			set("default", it.Blocks)
		}
	}
	// With different odd and even pages, a header for both kinds must be given for even pages too.
	if _, ok := byType["even"]; w.evenAndOdd && !ok {
		for i := len(items) - 1; i >= 0; i-- {
			if items[i].Pages == doc.BothPages {
				set("even", items[i].Blocks)
				break
			}
		}
	}
	if titlePage {
		if hideFirst {
			set("first", nil)
		} else {
			set("first", byType["default"])
		}
	}
	for _, t := range *used {
		if _, ok := byType[t]; !ok {
			set(t, nil)
		}
	}
	for _, t := range order {
		blocks := byType[t]
		id := w.headerFooterPart(kind, func() string { return w.blocks(blocks, "", true, "", "") })
		fmt.Fprintf(sb, `<w:%sReference w:type="%s" r:id="%s"/>`, kind, t, id)
		if !slices.Contains(*used, t) {
			*used = append(*used, t)
		}
	}
}

// blocks writes a block list. sectionEnd goes into the last paragraph;
// requireParagraph: the container must end with a paragraph (cells, text
// boxes, notes, the body); paragraphStyle styles plain paragraphs (notes);
// leadingRuns go first into the first paragraph (the number of a note).
func (w *writer) blocks(blocks []doc.Block, sectionEnd string, requireParagraph bool, paragraphStyle, leadingRuns string) string {
	var sb strings.Builder
	if leadingRuns != "" {
		if _, ok := first(blocks).(*doc.Paragraph); !ok {
			blocks = append([]doc.Block{&doc.Paragraph{}}, blocks...)
		}
	}
	for i, b := range blocks {
		switch b := b.(type) {
		case *doc.Paragraph:
			end, lead := "", ""
			if i == len(blocks)-1 {
				end = sectionEnd
			}
			if i == 0 {
				lead = leadingRuns
			}
			sb.WriteString(w.paragraph(b, end, paragraphStyle, lead))
		case *doc.Table:
			// Two tables in a row would merge into one in Word.
			if i > 0 {
				if _, ok := blocks[i-1].(*doc.Table); ok {
					sb.WriteString("<w:p/>")
				}
			}
			sb.WriteString(w.table(b))
		}
	}
	_, endsWithParagraph := last(blocks).(*doc.Paragraph)
	if sectionEnd != "" && !endsWithParagraph {
		sb.WriteString("<w:p><w:pPr>" + sectionEnd + "</w:pPr></w:p>")
	} else if requireParagraph && !endsWithParagraph {
		sb.WriteString("<w:p/>")
	}
	return sb.String()
}

func first(b []doc.Block) doc.Block {
	if len(b) == 0 {
		return nil
	}
	return b[0]
}

func last(b []doc.Block) doc.Block {
	if len(b) == 0 {
		return nil
	}
	return b[len(b)-1]
}

func (w *writer) paragraph(p *doc.Paragraph, sectPr, style, leadingRuns string) string {
	heading := 0
	if p.HeadingLevel >= 1 && p.HeadingLevel <= 9 {
		heading = p.HeadingLevel
		style = fmt.Sprintf("Heading%d", heading)
	}
	ctx := runContext{styleBold: heading > 0}
	var sb strings.Builder
	sb.WriteString("<w:p>")
	sb.WriteString(w.paragraphProperties(p, style, heading > 0, sectPr, ctx))
	if p.ColumnBreakBefore {
		sb.WriteString(`<w:r><w:br w:type="column"/></w:r>`)
	}
	sb.WriteString(leadingRuns)
	w.inlines(p.Inlines, &sb, ctx)
	sb.WriteString("</w:p>")
	return sb.String()
}

// paragraphProperties is w:pPr in CT_PPr order.
func (w *writer) paragraphProperties(p *doc.Paragraph, style string, heading bool, sectPr string, ctx runContext) string {
	f := p.Format
	var sb strings.Builder
	if style != "" {
		fmt.Fprintf(&sb, `<w:pStyle w:val="%s"/>`, style)
	}
	if f.KeepWithNext != nil && *f.KeepWithNext {
		sb.WriteString("<w:keepNext/>")
	} else if f.KeepWithNext != nil && heading {
		sb.WriteString(`<w:keepNext w:val="0"/>`)
	}
	if f.KeepLines != nil && *f.KeepLines {
		sb.WriteString("<w:keepLines/>")
	} else if f.KeepLines != nil && heading {
		sb.WriteString(`<w:keepLines w:val="0"/>`)
	}
	if p.PageBreakBefore {
		sb.WriteString("<w:pageBreakBefore/>")
	}
	if f.WidowOrphan != nil && !*f.WidowOrphan {
		sb.WriteString(`<w:widowControl w:val="0"/>`)
	}
	if p.List != nil && p.List.Numbered {
		fmt.Fprintf(&sb, `<w:numPr><w:ilvl w:val="%d"/><w:numId w:val="%d"/></w:numPr>`, max(0, min(8, p.List.Level)), w.numberingID(p.List.Numbering))
	}
	if len(f.Tabs) > 0 {
		tabs := slices.Clone(f.Tabs)
		sort.SliceStable(tabs, func(i, j int) bool { return tabs[i].Position < tabs[j].Position })
		sb.WriteString("<w:tabs>")
		for _, t := range tabs {
			kind := "left"
			switch t.Kind {
			case doc.TabRight:
				kind = "right"
			case doc.TabCenter:
				kind = "center"
			case doc.TabDecimal:
				kind = "decimal"
			}
			leader := ""
			switch t.Leader {
			case "DOT":
				leader = ` w:leader="dot"`
			case "DASH":
				leader = ` w:leader="hyphen"`
			case "SOLID":
				leader = ` w:leader="underscore"`
			}
			fmt.Fprintf(&sb, `<w:tab w:val="%s"%s w:pos="%d"/>`, kind, leader, twips(t.Position))
		}
		sb.WriteString("</w:tabs>")
	}
	// Right to left: w:ind's left / right and w:jc's left / right then mean the start and the end side.
	if f.RightToLeft != nil && *f.RightToLeft {
		sb.WriteString("<w:bidi/>")
	}
	var spacing strings.Builder
	if f.Before != nil {
		fmt.Fprintf(&spacing, ` w:before="%d"`, max(0, twips(*f.Before)))
	}
	if f.After != nil {
		fmt.Fprintf(&spacing, ` w:after="%d"`, max(0, twips(*f.After)))
	}
	if l := f.LineSpacing; l != nil {
		// 한글 percentages are of the font size, Word's "auto" lines of the font's line height (≈ 1.3 em).
		switch l.Kind {
		case doc.SpacingFixed:
			fmt.Fprintf(&spacing, ` w:line="%d" w:lineRule="exact"`, max(20, twips(l.Value)))
		case doc.SpacingAtLeast:
			fmt.Fprintf(&spacing, ` w:line="%d" w:lineRule="atLeast"`, max(20, twips(l.Value)))
		default:
			fmt.Fprintf(&spacing, ` w:line="%d" w:lineRule="auto"`, max(24, min(2400, int(math.RoundToEven(float64(l.Value)*240/130)))))
		}
	}
	if spacing.Len() > 0 {
		sb.WriteString("<w:spacing" + spacing.String() + "/>")
	}
	left, firstLine := f.Left, f.FirstLine
	if p.List != nil && left == nil {
		// Continuation paragraphs of a list item line up with the item's text.
		if !p.List.Numbered {
			fmt.Fprintf(&sb, `<w:ind w:left="%d"/>`, listIndent(p.List.Level))
		}
	} else if left != nil || f.Right != nil || firstLine != nil {
		if p.List != nil || doc.Val(left, 0) != 0 || doc.Val(f.Right, 0) != 0 || doc.Val(firstLine, 0) != 0 {
			sb.WriteString("<w:ind")
			if left != nil {
				fmt.Fprintf(&sb, ` w:left="%d"`, twips(*left))
			}
			if f.Right != nil {
				fmt.Fprintf(&sb, ` w:right="%d"`, twips(*f.Right))
			}
			if firstLine != nil && *firstLine < 0 {
				fmt.Fprintf(&sb, ` w:hanging="%d"`, twips(-*firstLine))
			} else if firstLine != nil {
				fmt.Fprintf(&sb, ` w:firstLine="%d"`, twips(*firstLine))
			}
			sb.WriteString("/>")
		}
	}
	if f.Align != nil && *f.Align != doc.AlignLeft {
		switch *f.Align {
		case doc.AlignCenter:
			sb.WriteString(`<w:jc w:val="center"/>`)
		case doc.AlignRight:
			sb.WriteString(`<w:jc w:val="right"/>`)
		case doc.AlignDistribute:
			sb.WriteString(`<w:jc w:val="distribute"/>`)
		default:
			sb.WriteString(`<w:jc w:val="both"/>`)
		}
	}
	if mark := runProperties(p.MarkFormat, ctx, "", false); mark != "" {
		sb.WriteString("<w:rPr>" + mark + "</w:rPr>")
	}
	sb.WriteString(sectPr)
	if sb.Len() == 0 {
		return ""
	}
	return "<w:pPr>" + sb.String() + "</w:pPr>"
}

func (w *writer) inlines(inlines []doc.Inline, sb *strings.Builder, ctx runContext) {
	for _, in := range inlines {
		switch in := in.(type) {
		case *doc.Text:
			textRuns(sb, in.Text, in.Format, ctx)
		case *doc.Equation:
			textRuns(sb, in.Text.Text, in.Format, ctx)
		case *doc.LineBreak:
			sb.WriteString("<w:r>" + runPr(in.Format, ctx, "", false) + "<w:br/></w:r>")
		case *doc.Tab:
			sb.WriteString("<w:r>" + runPr(in.Format, ctx, "", false) + "<w:tab/></w:r>")
		case *doc.Link:
			w.hyperlink(in, sb, ctx)
		case *doc.Bookmark:
			id := w.nextBookmarkID
			w.nextBookmarkID++
			fmt.Fprintf(sb, `<w:bookmarkStart w:id="%d" w:name="%s"/><w:bookmarkEnd w:id="%d"/>`, id, esc(w.bookmarkName(in.Name)), id)
		case *doc.Field:
			instr := "NUMPAGES"
			if in.Kind == doc.PageNumberField {
				instr = "PAGE"
			}
			field(sb, instr, runPr(in.Format, ctx, "", false))
		case *doc.Note:
			sb.WriteString(w.noteReference(in))
		case *doc.Image:
			sb.WriteString(w.picture(in))
		case *doc.TextBox:
			sb.WriteString(w.textBox(in, ctx))
		case *doc.Shape:
			sb.WriteString(w.shape(in))
		}
	}
}

// isRightToLeft: Hebrew, Arabic, Syriac, Thaana, NKo, … and their presentation forms.
func isRightToLeft(r rune) bool {
	return r >= 0x0590 && r <= 0x08FF || r >= 0xFB1D && r <= 0xFDFF || r >= 0xFE70 && r <= 0xFEFF ||
		r >= 0x10800 && r <= 0x10FFF || r >= 0x1E800 && r <= 0x1EFFF
}

// direction of text by its letters: -1 right to left, 1 left to right, 0 neither.
func direction(text string) int {
	rtl, ltr, digits := 0, 0, 0
	for _, r := range text {
		switch {
		case isRightToLeft(r):
			// Arabic-Indic digits are numbers: they keep their order within right-to-left text.
			if r >= 0x0660 && r <= 0x0669 || r >= 0x06F0 && r <= 0x06F9 {
				digits++
			} else if unicode.IsLetter(r) {
				rtl++
			}
		case unicode.IsLetter(r):
			ltr++
		case unicode.IsDigit(r):
			digits++
		}
	}
	if rtl > 0 && rtl >= ltr {
		return -1
	}
	if ltr+digits > 0 {
		return 1
	}
	return 0
}

// textRuns writes a run; line feeds and tabs inside the text become w:br and w:tab.
func textRuns(sb *strings.Builder, text string, f doc.CharFormat, ctx runContext) {
	if text == "" {
		return
	}
	// Arabic or Hebrew text is marked right to left, as Word does.
	sb.WriteString("<w:r>" + runPr(f, ctx, "", direction(text) < 0))
	start := 0
	for i := 0; i <= len(text); i++ {
		if i < len(text) && text[i] != '\n' && text[i] != '\t' && text[i] != '\r' {
			continue
		}
		if i > start {
			sb.WriteString(`<w:t xml:space="preserve">` + esc(text[start:i]) + "</w:t>")
		}
		if i < len(text) {
			if text[i] == '\t' {
				sb.WriteString("<w:tab/>")
			} else if text[i] == '\n' || text[i] == '\r' && (i+1 >= len(text) || text[i+1] != '\n') {
				sb.WriteString("<w:br/>")
			}
		}
		start = i + 1
	}
	sb.WriteString("</w:r>")
}

func runPr(f doc.CharFormat, ctx runContext, characterStyle string, rtl bool) string {
	p := runProperties(f, ctx, characterStyle, rtl)
	if p == "" {
		return ""
	}
	return "<w:rPr>" + p + "</w:rPr>"
}

// runProperties is the content of w:rPr in CT_RPr order.
func runProperties(f doc.CharFormat, ctx runContext, characterStyle string, rtl bool) string {
	var sb strings.Builder
	if characterStyle == "" && ctx.link {
		characterStyle = "Hyperlink"
	}
	if characterStyle != "" {
		fmt.Fprintf(&sb, `<w:rStyle w:val="%s"/>`, characterStyle)
	}
	if f.Font != nil || f.EastAsianFont != nil {
		sb.WriteString("<w:rFonts")
		if f.Font != nil {
			fmt.Fprintf(&sb, ` w:ascii="%s" w:hAnsi="%s"`, esc(*f.Font), esc(*f.Font))
		}
		ea := f.EastAsianFont
		if ea == nil {
			ea = f.Font
		}
		if ea != nil {
			fmt.Fprintf(&sb, ` w:eastAsia="%s"`, esc(*ea))
		}
		if f.Font != nil {
			fmt.Fprintf(&sb, ` w:cs="%s"`, esc(*f.Font))
		}
		sb.WriteString("/>")
	}
	if f.Bold != nil && *f.Bold {
		sb.WriteString("<w:b/><w:bCs/>")
	} else if f.Bold != nil && ctx.styleBold {
		sb.WriteString(`<w:b w:val="0"/><w:bCs w:val="0"/>`)
	}
	if f.Italic != nil && *f.Italic {
		sb.WriteString("<w:i/><w:iCs/>")
	}
	if f.Strike != nil && *f.Strike {
		sb.WriteString("<w:strike/>")
	}
	if f.Color != nil {
		if c := hexColor(*f.Color); c != "" {
			fmt.Fprintf(&sb, `<w:color w:val="%s"/>`, c)
		}
	}
	size := doc.Val(f.Size, defaultSize)
	if f.Spacing != nil && *f.Spacing != 0 {
		// 한글 letter spacing is a percentage of the font size, Word's an absolute distance in twips.
		fmt.Fprintf(&sb, `<w:spacing w:val="%d"/>`, int(math.RoundToEven(float64(*f.Spacing*size)/500)))
	}
	if f.Size != nil {
		hp := max(2, min(3276, int(math.RoundToEven(float64(*f.Size)/50))))
		fmt.Fprintf(&sb, `<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, hp, hp)
	}
	highlight := ""
	if f.Shade != nil {
		highlight = highlights[strings.ToUpper(*f.Shade)]
	}
	if highlight != "" {
		fmt.Fprintf(&sb, `<w:highlight w:val="%s"/>`, highlight)
	}
	if f.Underline != nil && *f.Underline {
		sb.WriteString(`<w:u w:val="single"/>`)
	} else if f.Underline != nil && characterStyle == "Hyperlink" {
		sb.WriteString(`<w:u w:val="none"/>`)
	}
	if highlight == "" && f.Shade != nil {
		if fill := hexColor(*f.Shade); fill != "" {
			fmt.Fprintf(&sb, `<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, fill)
		}
	}
	if f.Superscript != nil && *f.Superscript {
		sb.WriteString(`<w:vertAlign w:val="superscript"/>`)
	} else if f.Subscript != nil && *f.Subscript {
		sb.WriteString(`<w:vertAlign w:val="subscript"/>`)
	}
	if rtl {
		sb.WriteString("<w:rtl/>")
	}
	return sb.String()
}

// field is a complex field (PAGE, NUMPAGES) with a cached result Word updates.
func field(sb *strings.Builder, instr, rp string) {
	fmt.Fprintf(sb, `<w:r>%s<w:fldChar w:fldCharType="begin"/></w:r>`, rp)
	fmt.Fprintf(sb, `<w:r>%s<w:instrText xml:space="preserve"> %s </w:instrText></w:r>`, rp, instr)
	fmt.Fprintf(sb, `<w:r>%s<w:fldChar w:fldCharType="separate"/></w:r>`, rp)
	fmt.Fprintf(sb, `<w:r>%s<w:t>1</w:t></w:r>`, rp)
	fmt.Fprintf(sb, `<w:r>%s<w:fldChar w:fldCharType="end"/></w:r>`, rp)
}

// noteReference: Word allows notes only in the body text; elsewhere the
// note's text stays in place, in brackets.
func (w *writer) noteReference(n *doc.Note) string {
	kind, style := "footnote", "Footnote"
	if n.Endnote {
		kind, style = "endnote", "Endnote"
	}
	if w.cur != w.document || w.textBoxDepth > 0 {
		var text strings.Builder
		for _, b := range n.Blocks {
			if p, ok := b.(*doc.Paragraph); ok {
				for _, in := range p.Inlines {
					if t, ok := in.(*doc.Text); ok {
						text.WriteString(t.Text)
					}
				}
			}
		}
		var sb strings.Builder
		textRuns(&sb, " ("+text.String()+")", doc.CharFormat{}, runContext{})
		return sb.String()
	}
	var p *part
	var list *[]string
	if n.Endnote {
		if w.endnotesPart == nil {
			w.endnotesPart = &part{path: "word/endnotes.xml", contentType: typeBase + "endnotes+xml"}
		}
		p, list = w.endnotesPart, &w.endnotes
	} else {
		if w.footnotesPart == nil {
			w.footnotesPart = &part{path: "word/footnotes.xml", contentType: typeBase + "footnotes+xml"}
		}
		p, list = w.footnotesPart, &w.footnotes
	}
	id := len(*list) + 1
	*list = append(*list, "")
	number := fmt.Sprintf(`<w:r><w:rPr><w:rStyle w:val="%sReference"/></w:rPr><w:%sRef/></w:r><w:r><w:t xml:space="preserve"> </w:t></w:r>`, style, kind)
	(*list)[id-1] = w.withPart(p, func() string { return w.blocks(n.Blocks, "", true, style+"Text", number) })
	return fmt.Sprintf(`<w:r><w:rPr><w:rStyle w:val="%sReference"/></w:rPr><w:%sReference w:id="%d"/></w:r>`, style, kind, id)
}

type cellPos struct{ r, c int }

func (w *writer) table(t *doc.Table) string {
	if len(t.Rows) == 0 {
		return ""
	}
	placed, columns := doc.PlaceCells(t)
	indent := 0
	if t.Align == doc.AlignLeft {
		indent = t.Indent
	}
	widths := doc.ColumnWidths(t, columns, max(4000, w.width-indent))
	borders := doc.GridBorders
	if t.Borders != nil {
		borders = *t.Borders
	}
	// Cells that span rows: the positions below their first row are continuation cells (w:vMerge).
	continuations := map[cellPos]*doc.Cell{}
	for r, row := range placed {
		for _, pc := range row {
			for below := 1; below < pc.Cell.RowSpan; below++ {
				continuations[cellPos{r + below, pc.Column}] = pc.Cell
			}
		}
	}
	sum := func(from, n int) int {
		s := 0
		for i := from; i < from+n && i < len(widths); i++ {
			s += widths[i]
		}
		return s
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, `<w:tbl><w:tblPr><w:tblW w:w="%d" w:type="dxa"/>`, twips(sum(0, len(widths))))
	switch {
	case t.Align == doc.AlignCenter:
		sb.WriteString(`<w:jc w:val="center"/>`)
	case t.Align == doc.AlignRight:
		sb.WriteString(`<w:jc w:val="right"/>`)
	case t.Indent != 0:
		fmt.Fprintf(&sb, `<w:tblInd w:w="%d" w:type="dxa"/>`, twips(t.Indent))
	}
	sb.WriteString("<w:tblBorders>" + border("top", borders.Top) + border("left", borders.Left) + border("bottom", borders.Bottom) +
		border("right", borders.Right) + border("insideH", borders.Top) + border("insideV", borders.Left) + "</w:tblBorders>")
	sb.WriteString(`<w:tblLayout w:type="fixed"/>`)
	if m := t.CellMargin; m != nil {
		sb.WriteString(cellMargins("tblCellMar", *m))
	}
	sb.WriteString(`<w:tblLook w:val="04A0"/></w:tblPr><w:tblGrid>`)
	for _, wd := range widths {
		fmt.Fprintf(&sb, `<w:gridCol w:w="%d"/>`, twips(wd))
	}
	sb.WriteString("</w:tblGrid>")
	padding := 1080
	if t.CellMargin != nil {
		padding = t.CellMargin.Left + t.CellMargin.Right
	}
	outer := w.width
	headerRows := 0
	for _, r := range t.Rows {
		if !r.Header {
			break
		}
		headerRows++
	}
	for r, row := range t.Rows {
		sb.WriteString("<w:tr>")
		if row.Height != nil && *row.Height > 0 || r < headerRows {
			sb.WriteString("<w:trPr>")
			if row.Height != nil && *row.Height > 0 {
				fmt.Fprintf(&sb, `<w:trHeight w:val="%d" w:hRule="atLeast"/>`, twips(*row.Height))
			}
			if r < headerRows {
				sb.WriteString("<w:tblHeader/>")
			}
			sb.WriteString("</w:trPr>")
		}
		starts := map[int]*doc.Cell{}
		if r < len(placed) {
			for _, pc := range placed[r] {
				if _, dup := starts[pc.Column]; !dup {
					starts[pc.Column] = pc.Cell
				}
			}
		}
		for column := 0; column < columns; {
			if cell, ok := starts[column]; ok {
				span := max(1, min(cell.ColSpan, columns-column))
				width := sum(column, span)
				w.width = max(1000, width-padding)
				vMerge := ""
				if cell.RowSpan > 1 {
					vMerge = `<w:vMerge w:val="restart"/>`
				}
				sb.WriteString(tableCell(cell, width, span, vMerge, w.blocks(cell.Blocks, "", true, "", "")))
				column += span
			} else if origin, ok := continuations[cellPos{r, column}]; ok {
				span := max(1, min(origin.ColSpan, columns-column))
				sb.WriteString(tableCell(origin, sum(column, span), span, "<w:vMerge/>", "<w:p/>"))
				column += span
			} else {
				// A gap in a ragged row: an empty cell keeps the grid rectangular.
				fmt.Fprintf(&sb, `<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/></w:tcPr><w:p/></w:tc>`, twips(widths[column]))
				column++
			}
		}
		sb.WriteString("</w:tr>")
	}
	w.width = outer
	sb.WriteString("</w:tbl>")
	if len(t.Caption) > 0 {
		sb.WriteString(w.blocks(t.Caption, "", false, "", ""))
	}
	return sb.String()
}

func cellMargins(name string, m doc.Insets) string {
	return fmt.Sprintf(`<w:%s><w:top w:w="%d" w:type="dxa"/><w:left w:w="%d" w:type="dxa"/><w:bottom w:w="%d" w:type="dxa"/><w:right w:w="%d" w:type="dxa"/></w:%s>`,
		name, twips(m.Top), twips(m.Left), twips(m.Bottom), twips(m.Right), name)
}

// tableCell is w:tc with w:tcPr in CT_TcPr order: tcW, gridSpan, vMerge, tcBorders, shd, tcMar, vAlign.
func tableCell(c *doc.Cell, width, span int, vMerge, content string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, `<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/>`, twips(width))
	if span > 1 {
		fmt.Fprintf(&sb, `<w:gridSpan w:val="%d"/>`, span)
	}
	sb.WriteString(vMerge)
	if b := c.Borders; b != nil {
		sb.WriteString("<w:tcBorders>" + border("top", b.Top) + border("left", b.Left) + border("bottom", b.Bottom) + border("right", b.Right) + "</w:tcBorders>")
	}
	if fill := hexColor(c.Fill); fill != "" {
		fmt.Fprintf(&sb, `<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, fill)
	}
	if c.CellMargin != nil {
		sb.WriteString(cellMargins("tcMar", *c.CellMargin))
	}
	switch c.VerticalAlign {
	case doc.VAlignCenter:
		sb.WriteString(`<w:vAlign w:val="center"/>`)
	case doc.VAlignBottom:
		sb.WriteString(`<w:vAlign w:val="bottom"/>`)
	}
	sb.WriteString("</w:tcPr>" + content + "</w:tc>")
	return sb.String()
}

// border is an edge: style, width in eighths of a point (2..96), colour.
func border(edge string, b doc.Border) string {
	if b.Style == doc.BorderNone {
		return fmt.Sprintf(`<w:%s w:val="nil"/>`, edge)
	}
	style := "single"
	switch b.Style {
	case doc.BorderDash:
		style = "dashed"
	case doc.BorderDot:
		style = "dotted"
	case doc.BorderDashDot:
		style = "dotDash"
	case doc.BorderDouble:
		style = "double"
	}
	eighths := max(2, min(96, int(math.RoundToEven(b.WidthMm/25.4*72*8))))
	color := hexColor(b.Color)
	if color == "" {
		color = "auto"
	}
	return fmt.Sprintf(`<w:%s w:val="%s" w:sz="%d" w:space="0" w:color="%s"/>`, edge, style, eighths, color)
}
