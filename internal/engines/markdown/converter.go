// Package markdown is the built-in "markdown" engine: Markdown → HTML / TXT
// and plain-text aliases → UTF-8 TXT, in-process, without executing source
// scripts. It also reads Markdown into the document model (DOCX / HWPX
// writers).
package markdown

import (
	"context"
	"errors"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/yuin/goldmark/ast"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/formats"
)

// Converter is the "markdown" engine.
type Converter struct{}

func New() *Converter { return &Converter{} }

func (*Converter) ID() string          { return "markdown" }
func (*Converter) Name() string        { return "Text & Markdown (built-in)" }
func (*Converter) MaxParallelism() int { return 0 }

// Edges are cheaper than Pandoc's Markdown edges, so the built-in path wins for HTML and TXT.
func (*Converter) Edges() []convert.Edge {
	return []convert.Edge{convert.EC("md", "html", 4), convert.EC("md", "txt", 5), convert.EC("txt", "txt", 4)}
}

func (*Converter) Status() convert.Status { return convert.Available("", "goldmark") }

func (*Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	b, err := os.ReadFile(step.InputPath)
	if err != nil {
		return nil, err
	}
	md := doc.DecodeText(b)
	progress.Report(0.3)
	output := step.Output.Allocate(formats.ByID(step.To).PrimaryExtension(), "")
	if output == "" {
		return nil, nil
	}
	var content string
	switch step.To {
	case "html":
		in, _ := filepath.Abs(step.InputPath)
		content, err = ToHTML(md, in, output)
		if err != nil {
			return nil, err
		}
	case "txt":
		if step.From == "txt" {
			content = md
		} else {
			content = ToPlainText(md)
		}
		// TXT gets a byte order mark, so Notepad and Excel read it as UTF-8.
		content = "\xEF\xBB\xBF" + content
	default:
		return nil, errors.New("markdown: unsupported target " + step.To)
	}
	if err := os.WriteFile(output, []byte(content), 0o644); err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{output}, nil
}

// ToHTML renders a complete page for the Markdown file at inputPath written
// to outputPath. Away from the Markdown file, relative image paths would
// break: they point at the original files then.
func ToHTML(md, inputPath, outputPath string) (string, error) {
	root, src, _ := Parse(md)
	inputFolder := filepath.Dir(inputPath)
	outAbs, _ := filepath.Abs(outputPath)
	if !strings.EqualFold(inputFolder, filepath.Dir(outAbs)) {
		ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
			if img, ok := n.(*ast.Image); ok && entering {
				if p := ResolveImage(string(img.Destination), inputFolder, ""); p != "" {
					img.Destination = []byte(FileURL(p))
				}
			}
			return ast.WalkContinue, nil
		})
	}
	var sb strings.Builder
	if err := Engine.Renderer().Render(&sb, src, root); err != nil {
		return "", err
	}
	title := ""
	ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if h, ok := n.(*ast.Heading); ok && entering {
			title = textOf(h, src)
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	if title == "" {
		title = formats.NameWithoutExtension(inputPath)
	}
	return HTMLPage(title, sb.String()), nil
}

// FileURL is the file:// URL of an absolute path.
func FileURL(p string) string {
	p = filepath.ToSlash(p)
	if runtime.GOOS == "windows" && !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// HTMLPage wraps an HTML fragment into a readable, self-contained page (UTF-8, Korean / Chinese fonts).
func HTMLPage(title, body string) string {
	return "<!DOCTYPE html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n<title>" + html.EscapeString(title) + "</title>\n" +
		"<style>body{font-family:'Noto Sans KR','Malgun Gothic','Microsoft YaHei',sans-serif;max-width:860px;margin:2rem auto;line-height:1.7;padding:0 1rem}table{border-collapse:collapse}td,th{border:1px solid #ccc;padding:4px 8px}pre,code{background:#f4f3f8;border-radius:4px}pre{padding:.6rem .8rem;overflow:auto}img{max-width:100%}blockquote{margin-left:0;padding-left:1rem;border-left:3px solid #ccc;color:#555}</style>\n" +
		"</head>\n<body>\n" + body + "\n</body>\n</html>"
}

// ToPlainText is the text of a Markdown document without its markup: one
// line per paragraph, list item, table row (cells separated by tabs) and
// code line, blank lines between blocks.
func ToPlainText(md string) string {
	root, src, _ := Parse(md)
	var sb strings.Builder
	plainBlocks(root, src, &sb)
	return strings.TrimRight(sb.String(), "\n") + "\n"
}

func plainBlocks(n ast.Node, src []byte, sb *strings.Builder) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c.Kind().String() {
		case "Paragraph", "TextBlock", "Heading", "DefinitionTerm":
			sb.WriteString(plainInline(c, src))
			sb.WriteString("\n")
			if _, ok := c.(*ast.TextBlock); !ok {
				sb.WriteString("\n")
			}
		case "FencedCodeBlock", "CodeBlock":
			sb.WriteString(lines(c, src))
			sb.WriteString("\n")
		case "HTMLBlock", "ThematicBreak", "LinkReferenceDefinition":
		case "TableHeader", "TableRow":
			var cells []string
			for cell := c.FirstChild(); cell != nil; cell = cell.NextSibling() {
				cells = append(cells, plainInline(cell, src))
			}
			sb.WriteString(strings.Join(cells, "\t") + "\n")
		case "Table":
			plainBlocks(c, src, sb)
			sb.WriteString("\n")
		default:
			plainBlocks(c, src, sb)
		}
	}
}

func plainInline(n ast.Node, src []byte) string {
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
			if c.HardLineBreak() {
				sb.WriteByte('\n')
			} else if c.SoftLineBreak() {
				sb.WriteByte('\n')
			}
		case *ast.String:
			sb.Write(c.Value)
		case *ast.AutoLink:
			sb.Write(c.Label(src))
		case *ast.RawHTML:
			return ast.WalkSkipChildren, nil
		}
		if c.Kind().String() == "FootnoteLink" || c.Kind().String() == "FootnoteBacklink" {
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return sb.String()
}

// ResolveImage is the local path of a Markdown image target, or "" for
// remote or missing images. With resourceRoot set the image must be inside it.
func ResolveImage(target, baseFolder, resourceRoot string) string {
	target = strings.TrimSpace(target)
	if target == "" || strings.Contains(target, "://") && !strings.HasPrefix(strings.ToLower(target), "file:") {
		return ""
	}
	var p string
	if strings.HasPrefix(strings.ToLower(target), "file:") {
		u, err := url.Parse(target)
		if err != nil {
			return ""
		}
		p = u.Path
		if runtime.GOOS == "windows" {
			p = strings.TrimPrefix(p, "/")
		}
		p = filepath.FromSlash(p)
	} else {
		u, err := url.PathUnescape(target)
		if err != nil {
			u = target
		}
		p = filepath.FromSlash(u)
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(baseFolder, p)
	}
	p, err := filepath.Abs(p)
	if err != nil {
		return ""
	}
	if resourceRoot != "" {
		root, _ := filepath.Abs(resourceRoot)
		if rel, err := filepath.Rel(root, p); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return ""
		}
	}
	if st, err := os.Stat(p); err != nil || st.IsDir() {
		return ""
	}
	return p
}
