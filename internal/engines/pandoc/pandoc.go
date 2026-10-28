// Package pandoc converts documents with Pandoc (an external program):
// Markdown ↔ DOCX / ODT / RTF, HTML ↔ DOCX / ODT, the markup languages
// (reStructuredText, LaTeX, Org, AsciiDoc, Typst, MediaWiki, DocBook,
// Jupyter, Textile, man pages, OPML) ↔ Markdown / HTML / DOCX / ODT and each
// other (and → RTF, EPUB, TXT), and Markdown and the lightweight markups →
// PPTX slides. Pictures of a text result are extracted into a "<name>_files"
// folder next to it; Markdown with "Leave out pictures" gets none. Markdown
// stays free of HTML: tables come out as plain pipe tables.
package pandoc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/tools"
)

const timeout = 2 * time.Minute

var readers = map[string]string{
	"md": "markdown", "html": "html", "docx": "docx", "odt": "odt", "rtf": "rtf", "rst": "rst", "tex": "latex", "org": "org",
	"adoc": "asciidoc", "typ": "typst", "mediawiki": "mediawiki", "dbk": "docbook", "ipynb": "ipynb", "textile": "textile",
	"man": "man", "opml": "opml",
}

var writers = map[string]string{
	"md": "gfm", "html": "html", "docx": "docx", "odt": "odt", "rtf": "rtf", "txt": "plain", "rst": "rst", "tex": "latex",
	"epub": "epub", "org": "org", "adoc": "asciidoc", "typ": "typst", "mediawiki": "mediawiki", "dbk": "docbook5",
	"ipynb": "ipynb", "textile": "textile", "man": "man", "opml": "opml", "pptx": "pptx",
}

// Markup languages Pandoc reads and writes well.
var Markup = []string{"rst", "tex", "org", "adoc", "typ", "mediawiki", "dbk", "ipynb", "textile", "man", "opml"}

// Slides are the sources of Pandoc's PowerPoint writer (a slide per heading).
var Slides = []string{"md", "rst", "org", "adoc", "typ", "mediawiki", "textile", "ipynb"}

const slidesCost = 20

var (
	// standalone targets are complete documents only with --standalone.
	standalone = []string{"html", "tex", "dbk", "man", "opml", "typ"}
	// extractsMedia: text targets whose pictures are extracted into a folder next to them.
	extractsMedia = []string{"md", "html", "rst", "tex", "org", "adoc", "typ", "mediawiki", "textile", "dbk"}
)

// Converter is the "pandoc" engine.
type Converter struct {
	mu     sync.Mutex
	pandoc string
}

func New() *Converter { return &Converter{} }

func (*Converter) ID() string          { return "pandoc" }
func (*Converter) Name() string        { return "Pandoc" }
func (*Converter) MaxParallelism() int { return 0 }

func (*Converter) Edges() []convert.Edge {
	var e []convert.Edge
	// Markdown in / out: only Pandoc does this.
	for _, f := range []string{"docx", "odt", "html", "rtf"} {
		e = append(e, convert.E(f, "md"))
	}
	for _, t := range []string{"docx", "odt", "html", "rtf", "txt"} {
		e = append(e, convert.E("md", t))
	}
	// Overlaps with LibreOffice (the fallback engine, which costs more).
	for _, t := range []string{"docx", "odt"} {
		e = append(e, convert.EC("html", t, 14))
	}
	for _, f := range []string{"docx", "odt"} {
		e = append(e, convert.EC(f, "html", 14))
	}
	// Markup in / out. Writing one costs a little more than writing Markdown, so a route with a
	// Pandoc step in the middle goes through Markdown.
	for _, m := range Markup {
		for _, t := range []string{"md", "html", "docx", "odt", "rtf", "epub", "txt"} {
			e = append(e, convert.E(m, t))
		}
	}
	for _, m := range Markup {
		for _, s := range append([]string{"md", "html", "docx", "odt"}, Markup...) {
			if s != m {
				e = append(e, convert.EC(s, m, 11))
			}
		}
	}
	// Slides from headings: expensive, so text → PDF never takes the shortcut through slides.
	for _, s := range Slides {
		e = append(e, convert.EC(s, "pptx", slidesCost))
	}
	return e
}

// Locate finds pandoc: FILEE_PANDOC, Filee's engine folder, the PATH.
func Locate() string {
	return tools.Find(tools.Spec{Names: []string{"pandoc"}, Env: "FILEE_PANDOC", Package: "pandoc", Dirs: map[string][]string{
		"darwin": {"pandoc", "pandoc/bin"}, "linux": {"pandoc", "pandoc/bin"}, "windows": {"pandoc", "pandoc/bin", `C:\Program Files\Pandoc`},
	}})
}

func (c *Converter) Status() convert.Status {
	p := Locate()
	c.mu.Lock()
	c.pandoc = p
	c.mu.Unlock()
	if p == "" {
		return convert.Unavailable("engine.reason.not_installed", "")
	}
	return convert.Available(p, "")
}

func (c *Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	c.mu.Lock()
	pandoc := c.pandoc
	c.mu.Unlock()
	if pandoc == "" {
		pandoc = Locate()
	}
	if pandoc == "" {
		return nil, errors.New("Pandoc was not found")
	}
	reader, okR := readers[step.From]
	writer, okW := writers[step.To]
	if !okR || !okW {
		return nil, fmt.Errorf("pandoc: %s → %s is not supported", step.From, step.To)
	}
	output := step.Output.Allocate(formats.ByID(step.To).PrimaryExtension(), "")
	if output == "" {
		return nil, nil
	}
	progress.Report(0.1)
	input, _ := filepath.Abs(step.InputPath)
	output, _ = filepath.Abs(output)
	outFolder := filepath.Dir(output)
	args := []string{input, "-f", reader, "-t", writer, "-o", output}
	if slices.Contains(standalone, step.To) {
		args = append(args, "--standalone")
	}
	if step.To == "md" {
		f, err := filter(step.WorkDir, "markdown", plainMarkdown)
		if err != nil {
			return nil, err
		}
		args = append(args, "--lua-filter="+f)
	}
	media := ""
	if step.To == "md" && step.Preset != nil && step.Preset.Document.LeaveOutPictures {
		f, err := filter(step.WorkDir, "no-pictures", withoutPictures)
		if err != nil {
			return nil, err
		}
		args = append(args, "--lua-filter="+f)
	} else if slices.Contains(extractsMedia, step.To) {
		// Relative to the result (Pandoc runs in its folder), so the links keep working when both move.
		// Pictures the source refers to by a relative path are still looked up next to the source.
		media = strings.TrimSuffix(filepath.Base(output), filepath.Ext(output)) + "_files"
		args = append(args, "--extract-media="+media, "--resource-path="+filepath.Dir(input))
	}
	dir := filepath.Dir(input)
	if media != "" {
		dir = outFolder
	}
	stop := estimate(progress, 1500*time.Millisecond)
	rctx, cancel := context.WithTimeout(ctx, timeout)
	res, err := tools.Run(rctx, tools.Cmd{Path: pandoc, Args: args, Dir: dir})
	cancel()
	stop()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if _, statErr := os.Stat(output); statErr != nil {
		msg := ""
		if res != nil {
			msg = strings.TrimSpace(string(res.Stderr))
		}
		if err != nil {
			return nil, fmt.Errorf("Pandoc failed: %w", err)
		}
		return nil, fmt.Errorf("Pandoc failed. %s", msg)
	}
	// Word's EMF / WMF pictures stay as they are: no pure-Go metafile renderer (the original converts them on Windows only).
	progress.Report(1)
	return []string{output}, nil
}

// filter writes a Lua filter into the job's work folder and returns its path.
func filter(workDir, name, code string) (string, error) {
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return "", err
	}
	b := make([]byte, 8)
	rand.Read(b)
	p := filepath.Join(workDir, name+"-"+hex.EncodeToString(b)+".lua")
	return p, os.WriteFile(p, []byte(code), 0o644)
}

func estimate(p convert.Progress, typical time.Duration) func() {
	if p == nil {
		return func() {}
	}
	done := make(chan struct{})
	start := time.Now()
	go func() {
		t := time.NewTicker(150 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				x := time.Since(start).Seconds() / typical.Seconds()
				p(0.1 + 0.8*(1-expNeg(x)))
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}

// withoutPictures drops every picture; a figure keeps its caption as text.
const withoutPictures = `function Image(image) return {} end
function Figure(figure) return figure.caption.long end
`

// plainMarkdown keeps the Markdown free of HTML, which many editors show as
// code: every table becomes a plain grid (merged cells keep their text in
// their first position), the paragraphs of a cell are joined with <br>, a
// table without a header row gets its first row as the header. Pictures lose
// their Word size, which only an <img> can hold.
const plainMarkdown = `local br = pandoc.RawInline('html', '<br>')

local function inlines_of(blocks)
  local out = pandoc.Inlines{}
  local function add(inl)
    if #inl == 0 then return end
    if #out > 0 then out:insert(br) end
    out:extend(inl)
  end
  for _, block in ipairs(blocks) do
    if block.t == 'Para' or block.t == 'Plain' then
      add(block.content)
    elseif block.t == 'BulletList' or block.t == 'OrderedList' then
      for i, item in ipairs(block.content) do
        local mark = block.t == 'BulletList' and '•' or (tostring(i) .. '.')
        local inl = pandoc.Inlines{pandoc.Str(mark), pandoc.Space()}
        inl:extend(inlines_of(item))
        add(inl)
      end
    elseif block.t == 'LineBlock' then
      for _, line in ipairs(block.content) do add(line) end
    elseif block.t == 'Div' or block.t == 'BlockQuote' then
      add(inlines_of(block.content))
    else
      local text = pandoc.utils.stringify(block)
      if text ~= '' then add(pandoc.Inlines{pandoc.Str(text)}) end
    end
  end
  return out:walk{
    LineBreak = function() return br end,
    SoftBreak = function() return pandoc.Space() end,
  }
end

local function empty() return pandoc.Cell({}) end

-- Rows of one section (head, an intermediate head, a body, the foot); row spans stay within it.
local function grid(rows, columns)
  local below = {}
  for _, row in ipairs(rows) do
    local cells = pandoc.List()
    local source = row.cells
    local next = 1
    local column = 1
    while column <= columns do
      if (below[column] or 0) > 0 then
        below[column] = below[column] - 1
        cells:insert(empty())
        column = column + 1
      elseif next <= #source then
        local cell = source[next]
        next = next + 1
        local text = inlines_of(cell.contents)
        cells:insert(pandoc.Cell(#text > 0 and {pandoc.Plain(text)} or {}, cell.alignment, 1, 1, cell.attr))
        for c = column, column + cell.col_span - 1 do
          if c > column then cells:insert(empty()) end
          if cell.row_span > 1 then below[c] = cell.row_span - 1 end
        end
        column = column + cell.col_span
      else
        cells:insert(empty())
        column = column + 1
      end
    end
    while #cells > columns do cells:remove() end
    row.cells = cells
  end
  return rows
end

function Image(image)
  image.attributes = {}
  return image
end

function Table(tbl)
  local columns = #tbl.colspecs
  if #tbl.head.rows == 0 and #tbl.bodies > 0 and #tbl.bodies[1].body > 0 then
    tbl.head.rows:insert(tbl.bodies[1].body:remove(1))
  end
  tbl.head.rows = grid(tbl.head.rows, columns)
  for _, body in ipairs(tbl.bodies) do
    body.head = grid(body.head, columns)
    body.body = grid(body.body, columns)
  end
  tbl.foot.rows = grid(tbl.foot.rows, columns)
  return tbl
end
`
