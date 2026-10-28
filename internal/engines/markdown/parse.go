package markdown

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Engine is GitHub-style Markdown plus the common extensions (tables,
// footnotes, task lists, definition lists, autolinks, heading ids), like
// Markdig's advanced extensions. Raw HTML passes through, as Markdig does.
var Engine = goldmark.New(
	goldmark.WithExtensions(extension.GFM, extension.Footnote, extension.DefinitionList),
	goldmark.WithParserOptions(parser.WithAutoHeadingID(), parser.WithAttribute()),
	goldmark.WithRendererOptions(html.WithUnsafe()),
)

// Parse parses Markdown; YAML front matter is cut off and its "title" returned.
func Parse(markdown string) (ast.Node, []byte, string) {
	body, title := frontMatter(markdown)
	src := []byte(body)
	return Engine.Parser().Parse(text.NewReader(src)), src, title
}

// frontMatter splits "---\n…\n---" off the start of the text.
func frontMatter(md string) (string, string) {
	s := strings.TrimPrefix(md, "\xEF\xBB\xBF")
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return md, ""
	}
	lines := strings.SplitAfter(s, "\n")
	for i := 1; i < len(lines); i++ {
		l := strings.TrimRight(lines[i], "\r\n")
		if l == "---" || l == "..." {
			title := ""
			for _, y := range lines[1:i] {
				t := strings.TrimSpace(y)
				if len(t) > 6 && strings.EqualFold(t[:6], "title:") {
					title = strings.Trim(strings.TrimSpace(t[6:]), `"'`)
					break
				}
			}
			return strings.Join(lines[i+1:], ""), title
		}
	}
	return md, ""
}

// unescape resolves backslash escapes and entities of a text segment.
func unescape(b []byte) string {
	b = util.UnescapePunctuations(b)
	b = util.ResolveNumericReferences(b)
	b = util.ResolveEntityNames(b)
	return string(b)
}

// textOf is the plain text of an inline subtree.
func textOf(n ast.Node, src []byte) string {
	var sb strings.Builder
	ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch c := c.(type) {
		case *ast.Text:
			if c.IsRaw() {
				sb.Write(c.Value(src))
			} else {
				sb.WriteString(unescape(c.Value(src)))
			}
			if c.SoftLineBreak() || c.HardLineBreak() {
				sb.WriteByte(' ')
			}
		case *ast.String:
			sb.Write(c.Value)
		case *ast.AutoLink:
			sb.Write(c.Label(src))
		}
		return ast.WalkContinue, nil
	})
	return sb.String()
}

// lines is the text of a code block.
func lines(n ast.Node, src []byte) string {
	var b bytes.Buffer
	l := n.Lines()
	for i := 0; i < l.Len(); i++ {
		s := l.At(i)
		b.Write(s.Value(src))
	}
	return b.String()
}
