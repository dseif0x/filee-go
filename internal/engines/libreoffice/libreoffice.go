// Package libreoffice converts office documents through LibreOffice in
// headless mode: the optional fallback for formats the built-in engines
// don't handle (DOC, XLS, PPT, OpenDocument, …) and the only reader of rarer
// ones (Works, WordPerfect, Lotus Word Pro, AbiWord, Apple Pages / Numbers /
// Keynote, StarOffice, WPS Office, ClarisWorks, MacWrite, T602, Pocket Word,
// flat OpenDocument, Publisher, CorelDRAW, Visio, CGM). It also writes the
// OpenDocument templates, FODT, XLT and EMF / WMF, turns SVG into EMF / WMF /
// ODG and a PDF page into SVG.
//
// Gotchas handled here:
//   - Each concurrent soffice needs its own user profile
//     (-env:UserInstallation), otherwise conversions silently fail.
//   - soffice writes <outdir>/<input name>.<ext>: we convert into a private
//     folder and move the result.
//   - Always a timeout: a document with a modal dialog would hang forever.
//   - HWP / HWPX import needs the H2Orestart extension (Java).
//   - The first conversion with a new profile is slow: WarmUp converts a tiny
//     file once per worker profile.
//   - Its ZIP reader refuses some containers other readers open (Tencent
//     Docs exports): such a file is tried once more as a repacked copy.
package libreoffice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/tools"
)

const (
	workers = 2
	timeout = 3 * time.Minute
	// typicalDuration of one conversion with a prepared profile (drives the progress estimate).
	typicalDuration = 4 * time.Second
)

// Settings → Engines: which conversions to PDF go through LibreOffice instead
// of the built-in routes. Call Converter.Status (or Catalog.Refresh) after a change.
var (
	// ForSlides: presentations → PDF (slide backgrounds and positions as in PowerPoint).
	ForSlides = true
	// ForHwp: HWP / HWPX → PDF with H2Orestart instead of rhwp.
	ForHwp = false
	// ForDocuments: Word documents → PDF.
	ForDocuments = false
	// ForSheets: spreadsheets → PDF (print areas and page breaks as Excel prints them).
	ForSheets = false
)

// RemoveBlankPages, when set (by the PDF engine), removes pages without any
// painting operator from a PDF and returns how many; Calc turns stale print
// tiles into empty pages.
var RemoveBlankPages func(path string) (int, error)

// FirstPage, when set (by the PDF engine), writes the first page of the page
// range of a PDF as a PDF of its own: Draw's SVG export draws every page of
// a PDF on top of each other.
var FirstPage func(input, output, pageRange string) error

var (
	writer  = []string{"docx", "doc", "odt", "rtf", "fodt", "ott"}
	calc    = []string{"xlsx", "xls", "ods", "csv", "xlt", "ots"}
	impress = []string{"pptx", "ppt", "odp", "otp"}
	// Formats LibreOffice only reads, by the application that opens them.
	writerImport  = []string{"dot", "wps", "wpd", "lwp", "abw", "pages", "sdw", "cwk", "mcw", "602", "psw"}
	calcImport    = []string{"et", "numbers", "sdc", "fods"}
	impressImport = []string{"key", "dps", "sda", "fodp"}
	// CGM opens in Impress but is a drawing.
	impressGraphics = []string{"cgm"}
	draw            = []string{"pub", "cdr", "vsd", "odg", "fodg", "emf", "wmf"}
	drawTargets     = []string{"pdf", "odg", "svg", "png", "emf", "wmf"}
	// drawOnlyFor are opened in Draw although another engine reads them better.
	drawOnlyFor = []string{"svg", "pdf"}
	// renderedTargets: LibreOffice lays out the text (fonts matter).
	renderedTargets = []string{"pdf", "png", "svg", "emf", "wmf"}
)

// Converter is the "libreoffice" engine.
type Converter struct {
	mu       sync.Mutex
	soffice  string
	hwp      bool
	edges    []convert.Edge
	free     chan int
	Log      *slog.Logger
	profiles string
}

// New locates LibreOffice; profiles live in the user's config folder.
func New() *Converter {
	c := &Converter{free: make(chan int, workers), Log: slog.Default()}
	for i := 0; i < workers; i++ {
		c.free <- i
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	c.profiles = filepath.Join(base, "filee", "libreoffice-profiles")
	c.locate()
	return c
}

func (*Converter) ID() string          { return "libreoffice" }
func (*Converter) Name() string        { return "LibreOffice" }
func (*Converter) MaxParallelism() int { return workers }

func (c *Converter) Edges() []convert.Edge {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.edges)
}

func (c *Converter) Status() convert.Status {
	c.locate()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.soffice == "" {
		return convert.Unavailable("engine.reason.not_installed", "")
	}
	version := ""
	if c.hwp {
		version = "H2Orestart"
	}
	return convert.Available(c.soffice, version)
}

func (c *Converter) locate() {
	s := Locate()
	h := s != "" && hasH2Orestart(s)
	c.mu.Lock()
	c.soffice, c.hwp = s, h
	c.edges = BuildEdges(h, ForSlides, ForHwp, ForDocuments, ForSheets)
	c.mu.Unlock()
}

// Locate finds soffice: FILEE_LIBREOFFICE, Filee's engine folder, the PATH and the usual install places.
func Locate() string {
	spec := tools.Spec{
		Names:   []string{"soffice"},
		Env:     "FILEE_LIBREOFFICE",
		Package: "libreoffice",
		Dirs: map[string][]string{
			"darwin":  {"libreoffice/LibreOffice.app/Contents/MacOS", "libreoffice/Contents/MacOS", "/Applications/LibreOffice.app/Contents/MacOS", "~/Applications/LibreOffice.app/Contents/MacOS"},
			"linux":   {"libreoffice/program", "/usr/lib/libreoffice/program", "/usr/lib64/libreoffice/program"},
			"windows": {"libreoffice/program", `C:\Program Files\LibreOffice\program`, `C:\Program Files (x86)\LibreOffice\program`},
		},
	}
	if runtime.GOOS == "linux" {
		opts, _ := filepath.Glob("/opt/libreoffice*/program")
		slices.Sort(opts)
		slices.Reverse(opts)
		spec.Dirs["linux"] = append(spec.Dirs["linux"], opts...)
		spec.Names = append(spec.Names, "libreoffice")
	}
	if runtime.GOOS == "windows" {
		for _, v := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
			if d := os.Getenv(v); d != "" {
				spec.Dirs["windows"] = append(spec.Dirs["windows"], filepath.Join(d, "LibreOffice", "program"))
			}
		}
	}
	return tools.Find(spec)
}

// hasH2Orestart detects the H2Orestart extension in the installation's shared extension folders.
func hasH2Orestart(soffice string) bool {
	if p, err := filepath.EvalSymlinks(soffice); err == nil {
		soffice = p
	}
	root := filepath.Dir(filepath.Dir(soffice))
	for _, sub := range []string{"share/uno_packages", "share/extensions", "Resources/uno_packages", "Resources/extensions"} {
		dir := filepath.Join(root, filepath.FromSlash(sub))
		found := false
		filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if strings.Contains(strings.ToLower(d.Name()), "h2orestart") {
				found = true
				return filepath.SkipAll
			}
			return nil
		})
		if found {
			return true
		}
	}
	return false
}

// BuildEdges lists the conversions. LibreOffice is the fallback: its costs
// make the built-in engines win wherever they can do the job.
func BuildEdges(hwp, slides, hwpToPdf, documents, sheets bool) []convert.Edge {
	var edges []convert.Edge
	add := func(sources, targets []string, cost int) {
		for _, s := range sources {
			for _, t := range targets {
				if s != t {
					// An image that reached a drawing would come back as a picture inside the SVG: images are traced instead.
					edges = append(edges, convert.Edge{From: s, To: t, Cost: cost, NotForImageSources: t == "svg"})
				}
			}
		}
	}
	const (
		fallback = 25
		// preferred beats the built-in route even at the bottom of the engine list.
		preferred = convert.Free
		// chosen wins for the file itself, not as a hop for other formats.
		chosen = 10
	)
	pick := func(on bool, a, b int) int {
		if on {
			return a
		}
		return b
	}
	cat := func(lists ...[]string) []string {
		var out []string
		for _, l := range lists {
			out = append(out, l...)
		}
		return out
	}
	add(writer, cat(writer, []string{"txt", "html"}), fallback)
	add(writer, []string{"pdf"}, pick(documents, chosen, fallback))
	add(calc, cat(calc, []string{"html"}), fallback)
	add(calc, []string{"pdf"}, pick(sheets, chosen, fallback))
	add(impress, impress, fallback)
	add(impress, []string{"pdf"}, pick(slides, preferred, fallback))
	add([]string{"txt", "html"}, []string{"docx", "odt", "pdf"}, fallback+3)
	add(writerImport, cat(writer, []string{"pdf", "txt", "html"}), fallback)
	add(calcImport, cat(calc, []string{"pdf", "html"}), fallback)
	add(impressImport, cat(impress, []string{"pdf"}), fallback)
	add(cat(impressGraphics, draw), drawTargets, fallback)
	add([]string{"svg"}, []string{"odg", "emf", "wmf"}, fallback)
	add([]string{"pdf"}, []string{"svg", "odg"}, fallback)
	if hwp {
		add([]string{"hwp", "hwpx"}, []string{"docx", "odt", "rtf"}, fallback+6)
		add([]string{"hwp", "hwpx"}, []string{"pdf"}, pick(hwpToPdf, preferred, fallback+6))
	}
	// The planner does not know a cost of 0 (it reads it as the default 10): keep it the lowest possible.
	for i := range edges {
		if edges[i].Cost == 0 {
			edges[i].Cost = 1
		}
	}
	return edges
}

// FamilyOf is the LibreOffice application that opens a format: writer, calc, impress or draw.
func FamilyOf(from, ext string) string {
	switch {
	case from == "sda":
		if strings.EqualFold(ext, "sda") {
			return "draw"
		}
		return "impress"
	case slices.Contains(calc, from) || slices.Contains(calcImport, from):
		return "calc"
	case slices.Contains(impress, from) || slices.Contains(impressImport, from) || slices.Contains(impressGraphics, from):
		return "impress"
	case slices.Contains(draw, from) || slices.Contains(drawOnlyFor, from):
		return "draw"
	}
	return "writer"
}

// FilterFor maps a target to a --convert-to filter specification; ext is the
// input's extension, for format ids that cover files of two applications.
func FilterFor(from, to string, pdfA bool, ext string) string {
	family := FamilyOf(from, ext)
	switch to {
	case "pdf":
		if pdfA {
			// PDF/A-2b via the JSON filter options syntax (LibreOffice 7.4+).
			return "pdf:" + family + `_pdf_Export:{"SelectPdfVersion":{"type":"long","value":"2"}}`
		}
		return "pdf:" + family + "_pdf_Export"
	case "docx":
		return "docx:MS Word 2007 XML"
	case "doc":
		return "doc:MS Word 97"
	case "odt":
		return "odt"
	case "fodt":
		return "fodt:OpenDocument Text Flat XML"
	case "ott":
		return "ott:writer8_template"
	case "rtf":
		return "rtf:Rich Text Format"
	case "txt":
		return "txt:Text (encoded):UTF8"
	case "html":
		if family == "calc" {
			return "html:HTML (StarCalc)"
		}
		return "html:HTML (StarWriter)"
	case "xlsx":
		return "xlsx:Calc MS Excel 2007 XML"
	case "xls":
		return "xls:MS Excel 97"
	case "xlt":
		return "xlt:MS Excel 97 Vorlage/Template"
	case "ods":
		return "ods"
	case "ots":
		return "ots:calc8_template"
	case "csv":
		return "csv:Text - txt - csv (StarCalc):44,34,76"
	case "pptx":
		return "pptx:Impress MS PowerPoint 2007 XML"
	case "ppt":
		return "ppt:MS PowerPoint 97"
	case "odp":
		return "odp"
	case "otp":
		return "otp:impress8_template"
	case "odg":
		if family == "impress" {
			return "odg:impress8_draw"
		}
		return "odg:draw8"
	case "svg", "png", "emf", "wmf":
		return to + ":" + family + "_" + to + "_Export"
	}
	return to
}

func (c *Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	c.mu.Lock()
	soffice := c.soffice
	c.mu.Unlock()
	if soffice == "" {
		return nil, errors.New("LibreOffice was not found")
	}
	if slices.Contains(renderedTargets, step.To) && step.MissingFonts != nil {
		src := step.OriginalSourcePath
		if src == "" {
			src = step.InputPath
		}
		if missing := MissingFonts(src); len(missing) > 0 {
			step.MissingFonts(missing)
		}
	}
	slot, err := c.take(ctx)
	if err != nil {
		return nil, err
	}
	defer c.give(slot)
	progress.Report(0.1)

	input := step.InputPath
	if step.From == "pdf" && step.To == "svg" && FirstPage != nil {
		folder, err := os.MkdirTemp(step.WorkDir, "lo-page-")
		if err != nil {
			return nil, err
		}
		page := filepath.Join(folder, formats.NameWithoutExtension(step.InputPath)+".pdf")
		pr := ""
		if step.Preset != nil {
			pr = step.Preset.Pdf.PageRange
		}
		if err := FirstPage(step.InputPath, page, pr); err != nil {
			return nil, err
		}
		input = page
	}
	pdfA := step.Preset != nil && step.Preset.Document.PdfA
	filter := FilterFor(step.From, step.To, pdfA, formats.ExtensionOf(step.InputPath))
	produced, runErr := c.convertOnce(ctx, soffice, slot, filter, step.WorkDir, input, progress)
	if produced == "" && ctx.Err() == nil && IsPackage(step.InputPath) {
		if repacked := c.repack(step.InputPath, step.WorkDir); repacked != "" {
			c.Log.Info("LibreOffice could not load it, trying again with the ZIP container repacked", "file", step.InputPath)
			produced, _ = c.convertOnce(ctx, soffice, slot, filter, step.WorkDir, repacked, progress)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if produced == "" {
		// The first attempt's error, which is about the user's own file.
		if runErr != nil {
			return nil, fmt.Errorf("LibreOffice produced no output: %w", runErr)
		}
		return nil, errors.New("LibreOffice produced no output")
	}
	// Calc sometimes turns stale Excel print tiles into empty PDF pages. PDF/A stays as LibreOffice wrote it.
	if step.To == "pdf" && !pdfA && FamilyOf(step.From, formats.ExtensionOf(step.InputPath)) == "calc" && RemoveBlankPages != nil {
		if n, err := RemoveBlankPages(produced); err != nil {
			c.Log.Info("could not check empty spreadsheet PDF pages", "file", step.InputPath, "error", err)
		} else if n > 0 && step.Notice != nil {
			step.Notice("notice.empty_spreadsheet_pages_removed")
		}
	}
	target := step.Output.Allocate(formats.ByID(step.To).PrimaryExtension(), "")
	if target == "" {
		return nil, nil
	}
	if err := moveFile(produced, target); err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{target}, nil
}

func moveFile(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	b, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	if err := os.WriteFile(to, b, 0o644); err != nil {
		return err
	}
	return os.Remove(from)
}

// convertOnce runs LibreOffice into a fresh folder and returns the file it wrote ("" if none).
func (c *Converter) convertOnce(ctx context.Context, soffice string, slot int, filter, workDir, input string, progress convert.Progress) (string, error) {
	out, err := os.MkdirTemp(workDir, "lo-")
	if err != nil {
		return "", err
	}
	stop := estimate(progress, typicalDuration)
	runErr := c.run(ctx, soffice, slot, filter, out, input)
	stop()
	entries, _ := os.ReadDir(out)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && !e.IsDir() && info.Size() > 0 {
			return filepath.Join(out, e.Name()), runErr
		}
	}
	return "", runErr
}

func (c *Converter) repack(input, workDir string) string {
	folder, err := os.MkdirTemp(workDir, "lo-repack-")
	if err != nil {
		return ""
	}
	// Under the same file name: LibreOffice also goes by the name.
	copy := filepath.Join(folder, filepath.Base(input))
	if err := Repack(input, copy); err != nil {
		c.Log.Info("could not repack the ZIP container", "file", input, "error", err)
		return ""
	}
	return copy
}

func (c *Converter) profileDir(slot int) string {
	return filepath.Join(c.profiles, fmt.Sprintf("worker-%d", slot))
}

func (c *Converter) take(ctx context.Context) (int, error) {
	select {
	case s := <-c.free:
		return s, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func (c *Converter) give(slot int) { c.free <- slot }

func fileURL(p string) string {
	abs, err := filepath.Abs(p)
	if err == nil {
		p = abs
	}
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

func (c *Converter) run(ctx context.Context, soffice string, slot int, filter, outDir, input string) error {
	profile := c.profileDir(slot)
	if err := os.MkdirAll(profile, 0o755); err != nil {
		return err
	}
	args := []string{"--headless", "--norestore", "--nologo", "--nodefault", "--nolockcheck", "--nofirststartwizard",
		"-env:UserInstallation=" + fileURL(profile)}
	// The HWP import filter (H2Orestart) is written in Java: point LibreOffice at Filee's JRE.
	if jre := filepath.Join(tools.EnginesDir(), "jre"); isDir(jre) {
		if home := filepath.Join(jre, "Contents", "Home"); isDir(home) {
			jre = home
		}
		args = append(args, "-env:UNO_JAVA_JFW_JREHOME="+fileURL(jre)+"/")
	}
	args = append(args, "--convert-to", filter, "--outdir", outDir, input)
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, err := tools.Run(rctx, tools.Cmd{Path: soffice, Args: args})
	return err
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// WarmUp prepares the worker profiles never used by converting a tiny file
// with each, so the user's first conversion doesn't pay for LibreOffice's
// first start. Returns the number of profiles prepared.
func (c *Converter) WarmUp(ctx context.Context) (int, error) {
	c.mu.Lock()
	soffice := c.soffice
	c.mu.Unlock()
	if soffice == "" {
		return 0, nil
	}
	prepared := 0
	var held []int
	defer func() {
		for _, s := range held {
			c.give(s)
		}
	}()
	for i := 0; i < workers; i++ {
		// Taken while the previous one is held, so every round gets a different profile.
		slot, err := c.take(ctx)
		if err != nil {
			return prepared, err
		}
		held = append(held, slot)
		if isDir(filepath.Join(c.profileDir(slot), "user")) {
			continue
		}
		folder, err := os.MkdirTemp("", "filee-warmup-")
		if err != nil {
			return prepared, err
		}
		input := filepath.Join(folder, "warmup.txt")
		os.WriteFile(input, []byte("Filee"), 0o644)
		c.run(ctx, soffice, slot, FilterFor("txt", "pdf", false, ""), folder, input)
		os.RemoveAll(folder)
		prepared++
	}
	return prepared, nil
}

// estimate keeps the progress moving while a program that reports none
// runs: after the typical duration it has covered about 63 % of 0.1–0.9.
func estimate(p convert.Progress, typical time.Duration) (stop func()) {
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
				x := time.Since(start).Seconds() / max(0.1, typical.Seconds())
				p(0.1 + 0.8*(1-expNeg(x)))
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}
