package ebook

import (
	"context"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/htmldoc"
)

// readChapters reads the XHTML chapters of a book (EPUB spine, KF8 parts,
// HTMLZ page) into one document: every chapter starts on a new page, and
// links between chapters ("ch2.xhtml#note3") become links to bookmarks
// inside the document. mediaFolder receives pictures embedded as data: URIs.
func readChapters(ctx context.Context, chapters []string, mediaFolder string) (*doc.Document, error) {
	index := map[string]int{}
	for i, c := range chapters {
		abs, _ := filepath.Abs(c)
		key := strings.ToLower(abs)
		if _, ok := index[key]; !ok {
			index[key] = i
		}
	}
	section := doc.NewSection()
	d := &doc.Document{Sections: []*doc.Section{section}}
	for i, c := range chapters {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		chapter, _ := filepath.Abs(c)
		if !isFile(chapter) {
			continue // a spine item missing from the package: skip it like reading systems do
		}
		number := i
		content, err := htmldoc.ReadFile(chapter, htmldoc.ReadOptions{
			BaseFolder:   filepath.Dir(chapter),
			MediaFolder:  mediaFolder,
			ResourceRoot: filepath.Dir(mediaFolder),
			MapID:        func(id string) (string, bool) { return anchor(number, id), true },
			MapLink:      func(href string) (string, bool) { return chapterLink(href, chapter, number, index) },
		})
		if err != nil {
			return nil, err
		}
		if d.Title == "" {
			d.Title = content.Title
		}
		if !anyContent(content.Blocks) {
			continue
		}
		// The chapter starts on a new page and carries a bookmark for links to the chapter file itself.
		first, ok := content.Blocks[0].(*doc.Paragraph)
		if !ok {
			first = &doc.Paragraph{}
			content.Blocks = append([]doc.Block{first}, content.Blocks...)
		}
		first.PageBreakBefore = len(section.Blocks) > 0
		first.Inlines = append([]doc.Inline{&doc.Bookmark{Name: anchor(number, "")}}, first.Inlines...)
		section.Blocks = append(section.Blocks, content.Blocks...)
	}
	htmldoc.PruneBookmarks(d)
	return d, nil
}

func anyContent(blocks []doc.Block) bool {
	for _, b := range blocks {
		if _, ok := b.(*doc.Table); ok {
			return true
		}
		for _, in := range doc.Inlines([]doc.Block{b}) {
			switch in := in.(type) {
			case *doc.Image:
				return true
			case *doc.Text:
				if strings.TrimSpace(in.Text) != "" {
					return true
				}
			}
		}
	}
	return false
}

// anchor is the bookmark name of an element id (or the chapter start when id is "") in a chapter.
func anchor(chapter int, id string) string {
	if id == "" {
		return "c" + itoa(chapter+1)
	}
	var sb strings.Builder
	for _, c := range id {
		if c < 128 && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			sb.WriteRune(c)
		} else {
			sb.WriteByte('_')
		}
	}
	return "c" + itoa(chapter+1) + "-" + sb.String()
}

// chapterLink: links inside the book point at bookmarks; web and mail links
// stay; links to other files are dropped.
func chapterLink(href, chapter string, number int, index map[string]int) (string, bool) {
	if strings.HasPrefix(href, "#") {
		if len(href) == 1 {
			return "", false
		}
		return "#" + anchor(number, unescapePath(href[1:])), true
	}
	if colon := strings.IndexByte(href, ':'); colon > 1 && isLetters(href[:colon]) {
		if strings.HasPrefix(strings.ToLower(href), "javascript:") {
			return "", false
		}
		return href, true
	}
	file, fragment, _ := strings.Cut(href, "#")
	fragment = unescapePath(fragment)
	if q := strings.IndexByte(file, '?'); q >= 0 {
		file = file[:q]
	}
	u, err := url.PathUnescape(file)
	if err != nil {
		u = file
	}
	target, err := filepath.Abs(filepath.Join(filepath.Dir(chapter), filepath.FromSlash(u)))
	if err != nil {
		return "", false
	}
	i, ok := index[strings.ToLower(target)]
	if !ok {
		return "", false
	}
	return "#" + anchor(i, fragment), true
}

func isLetters(s string) bool {
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
