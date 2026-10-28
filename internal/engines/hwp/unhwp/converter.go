// Package unhwp is the "unhwp" engine: HWP / HWPX → plain text, Markdown or HTML. The C# app used the Unhwp native
// library; this reads the HWP 5.0 compound file itself (records, DocInfo, BodyText sections, BinData pictures) and an
// HWPX through the HWPX reader, and writes the document model with the htmldoc writers.
//
// Markdown pictures go into a "<name>_files" folder next to the Markdown file; HTML embeds them as data: URIs, so the
// page stays one file. Pictures it can't provide are left out instead of becoming broken links; "Leave out pictures"
// leaves out all of them.
package unhwp

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/htmldoc"
	"github.com/dseif0x/filee-go/internal/engines/hwp/hwpx"
	"github.com/dseif0x/filee-go/internal/engines/markdown"
	"github.com/dseif0x/filee-go/internal/formats"
)

// Converter is the "unhwp" engine.
type Converter struct{}

func New() *Converter { return &Converter{} }

func (*Converter) ID() string          { return "unhwp" }
func (*Converter) Name() string        { return "Unhwp" }
func (*Converter) MaxParallelism() int { return 0 }

func (*Converter) Edges() []convert.Edge {
	return []convert.Edge{
		convert.E("hwp", "txt"), convert.E("hwpx", "txt"),
		convert.E("hwp", "md"), convert.E("hwpx", "md"),
		{From: "hwp", To: "html", Cost: 14}, {From: "hwpx", To: "html", Cost: 14},
	}
}

// Status: built in, always available.
func (*Converter) Status() convert.Status { return convert.Available("", "") }

var blankLines = regexp.MustCompile(`\n{3,}`)

func (*Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	media, err := os.MkdirTemp(step.WorkDir, "unhwp-pictures-")
	if err != nil {
		return nil, err
	}
	d, err := Read(step.InputPath, media)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	progress.Report(0.5)

	path := step.Output.Allocate(step.To, "")
	if path == "" {
		return nil, nil
	}
	var content string
	switch step.To {
	case "txt":
		content = "\ufeff" + htmldoc.PlainText(d, "\n")
	case "md":
		link := func(string) string { return "" }
		if step.Preset == nil || !step.Preset.Document.LeaveOutPictures {
			link = markdown.NewPictures(path, step.Output).Add
		}
		d.Title = "" // no front matter
		content = tidy(htmldoc.Markdown(d, link))
	case "html":
		title := d.Title
		if strings.TrimSpace(title) == "" {
			title = formats.NameWithoutExtension(step.InputPath)
		}
		d.Title = ""
		body, err := renderMarkdown(tidy(htmldoc.Markdown(d, dataURI)))
		if err != nil {
			return nil, err
		}
		content = markdown.HTMLPage(title, body)
	default:
		return nil, fmt.Errorf("unhwp cannot write %s", step.To)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{path}, nil
}

// Read reads an HWP 5.0 or HWPX file; pictures are extracted into mediaFolder.
func Read(path, mediaFolder string) (*doc.Document, error) {
	if isZip(path) {
		return hwpx.ReadHwpx(path, mediaFolder, nil)
	}
	return ReadHWP(path, mediaFolder)
}

func isZip(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 4)
	n, _ := f.Read(head)
	return n == 4 && string(head) == "PK\x03\x04"
}

// tidy: paragraphs that held only a left-out picture leave at most one empty line between blocks.
func tidy(md string) string {
	return strings.TrimSpace(blankLines.ReplaceAllString(md, "\n\n")) + "\n"
}

func renderMarkdown(md string) (string, error) {
	root, src, _ := markdown.Parse(md)
	var sb strings.Builder
	if err := markdown.Engine.Renderer().Render(&sb, src, root); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// dataURI embeds a picture browsers show; others are left out.
func dataURI(file string) string {
	typ := imageTypes[strings.ToLower(filepath.Ext(file))]
	if typ == "" {
		return ""
	}
	b, err := os.ReadFile(file)
	if err != nil || len(b) == 0 {
		return ""
	}
	return "data:" + typ + ";base64," + base64.StdEncoding.EncodeToString(b)
}

var imageTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".svg": "image/svg+xml",
	".webp": "image/webp", ".bmp": "image/bmp",
}
