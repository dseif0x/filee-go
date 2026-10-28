package ebook

import (
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
)

// textToDocument reads plain text for e-books: paragraphs are separated by
// empty lines, and hard-wrapped lines inside a paragraph (Project Gutenberg
// style, ~70 characters) are joined. Text without empty lines keeps one
// paragraph per line. (TXT → DOCX keeps every line, see doc.TextDocument.)
func textToDocument(text string) *doc.Document {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\n")
	hasEmpty := false
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			hasEmpty = true
			break
		}
	}
	var paragraphs [][]string
	var current []string
	for _, l := range lines {
		blank := strings.TrimSpace(l) == ""
		if blank || !hasEmpty {
			if len(current) > 0 {
				paragraphs = append(paragraphs, current)
			}
			current = nil
			if blank {
				continue
			}
		}
		current = append(current, strings.TrimRight(l, " \t"))
	}
	if len(current) > 0 {
		paragraphs = append(paragraphs, current)
	}
	// Hard-wrapped text: most lines of multi-line paragraphs are long and of similar length.
	multi, long := 0, 0
	for _, p := range paragraphs {
		if len(p) > 1 {
			for _, l := range p[:len(p)-1] {
				multi++
				if n := len([]rune(l)); n >= 50 && n <= 100 {
					long++
				}
			}
		}
	}
	wrapped := multi > 0 && float64(long) > float64(multi)*0.7
	s := doc.NewSection()
	for _, p := range paragraphs {
		para := &doc.Paragraph{}
		for i, l := range p {
			if i > 0 {
				if wrapped {
					para.Inlines = append(para.Inlines, &doc.Text{Text: " "})
					l = strings.TrimLeft(l, " \t")
				} else {
					para.Inlines = append(para.Inlines, &doc.LineBreak{})
				}
			}
			para.Inlines = append(para.Inlines, &doc.Text{Text: l})
		}
		para.Inlines = doc.MergeTexts(para.Inlines)
		s.Blocks = append(s.Blocks, para)
	}
	return &doc.Document{Sections: []*doc.Section{s}}
}
