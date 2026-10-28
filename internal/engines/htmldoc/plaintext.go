package htmldoc

import (
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
)

// PlainText writes a document as text: paragraphs separated by an empty
// line, list markers and numbers written out, table rows as tab-separated
// cells and footnotes collected at the end. nl is the line ending.
func PlainText(d *doc.Document, nl string) string {
	var sb strings.Builder
	var notes []string
	numbers := NewListNumbers()
	for _, s := range d.Sections {
		textBlocks(s.Blocks, &sb, &notes, numbers, nl)
	}
	if len(notes) > 0 {
		sb.WriteString(nl + "----" + nl)
		for i, n := range notes {
			sb.WriteString("[" + strconv.Itoa(i+1) + "] " + n + nl)
		}
	}
	return strings.TrimRight(sb.String(), " \t\r\n") + nl
}

func textBlocks(blocks []doc.Block, sb *strings.Builder, notes *[]string, numbers *ListNumbers, nl string) {
	var previous doc.Block
	for _, b := range blocks {
		switch b := b.(type) {
		case *doc.Paragraph:
			text := textInlines(b.Inlines, notes, nl)
			if hasLine(b.Inlines) && text == "" {
				text = "* * *"
			}
			if strings.TrimSpace(text) == "" && b.List == nil {
				continue // spacing paragraphs: the empty line between paragraphs already separates them
			}
			// List items and code lines stay together; everything else is separated by an empty line.
			together := false
			if before, ok := previous.(*doc.Paragraph); ok {
				together = before.List != nil && b.List != nil || isCodeParagraph(before, false) && isCodeParagraph(b, false)
			}
			if sb.Len() > 0 {
				sb.WriteString(nl)
				if !together {
					sb.WriteString(nl)
				}
			}
			indent := max(0, doc.Val(b.Format.Left, 0)) / 1000
			if b.List != nil {
				marker := ""
				if b.List.Numbered {
					marker = numbers.Next(b.List) + " "
				}
				pad := strings.Repeat(" ", b.List.Level*4+indent)
				sb.WriteString(pad + marker + strings.ReplaceAll(text, nl, nl+pad+strings.Repeat(" ", len([]rune(marker)))))
			} else {
				pad := strings.Repeat(" ", indent)
				if indent > 0 {
					text = strings.ReplaceAll(text, nl, nl+pad)
				}
				sb.WriteString(pad + text)
			}
			previous = b
		case *doc.Table:
			if len(b.Rows) == 0 {
				continue
			}
			if sb.Len() > 0 {
				sb.WriteString(nl + nl)
			}
			for i, row := range b.Rows {
				if i > 0 {
					sb.WriteString(nl)
				}
				cells := make([]string, len(row.Cells))
				for j, c := range row.Cells {
					cells[j] = textCell(c, notes, nl)
				}
				sb.WriteString(strings.Join(cells, "\t"))
			}
			previous = b
		}
	}
}

func textCell(c *doc.Cell, notes *[]string, nl string) string {
	var parts []string
	for _, b := range c.Blocks {
		if p, ok := b.(*doc.Paragraph); ok {
			t := strings.NewReplacer(nl, " ", "\n", " ", "\t", " ").Replace(textInlines(p.Inlines, notes, nl))
			if t != "" {
				parts = append(parts, t)
			}
		}
	}
	return strings.Join(parts, " ")
}

func textInlines(inlines []doc.Inline, notes *[]string, nl string) string {
	var sb strings.Builder
	for _, in := range inlines {
		switch in := in.(type) {
		case *doc.Text:
			sb.WriteString(in.Text)
		case *doc.Equation:
			sb.WriteString(in.Text.Text)
		case *doc.LineBreak:
			sb.WriteByte('\n')
		case *doc.Tab:
			sb.WriteByte('\t')
		case *doc.Link:
			sb.WriteString(textInlines(in.Content, notes, "\n"))
		case *doc.Note:
			var content strings.Builder
			textBlocks(in.Blocks, &content, notes, NewListNumbers(), nl)
			*notes = append(*notes, strings.ReplaceAll(strings.TrimSpace(content.String()), nl+nl, " "))
			sb.WriteString("[" + strconv.Itoa(len(*notes)) + "]")
		case *doc.TextBox:
			var parts []string
			for _, b := range in.Blocks {
				if p, ok := b.(*doc.Paragraph); ok {
					parts = append(parts, textInlines(p.Inlines, notes, "\n"))
				}
			}
			sb.WriteString(strings.Join(parts, " "))
		}
	}
	return strings.TrimRight(strings.ReplaceAll(sb.String(), "\n", nl), " \t\r\n")
}
