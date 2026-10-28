package webui

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/presets"
)

// cropPdf sets every page's crop box to the part a of what it shows.
func cropPdf(ctx context.Context, in, out string, a presets.HiddenArea) error {
	pdf, err := api.ReadContextFile(ctx, in)
	if err != nil {
		return err
	}
	for n := 1; n <= pdf.PageCount; n++ {
		d, _, inh, err := pdf.PageDict(ctx, n, false)
		if err != nil {
			return err
		}
		if d == nil {
			continue
		}
		box := inh.CropBox
		if box == nil {
			box = inh.MediaBox
		}
		if box == nil {
			return fmt.Errorf("page %d has no media box", n)
		}
		llx, lly, urx, ury := cropRect(box.LL.X, box.LL.Y, box.UR.X, box.UR.Y, inh.Rotate, a)
		d.Update("CropBox", types.NewNumberArray(llx, lly, urx, ury))
	}
	return api.WriteContextFile(ctx, pdf, out)
}

// cropRect maps a (parts of the page as shown, from the top left) onto the
// page's own coordinates (origin bottom left, before /Rotate).
func cropRect(x0, y0, x1, y1 float64, rotate int, a presets.HiddenArea) (llx, lly, urx, ury float64) {
	w, h := x1-x0, y1-y0
	// The part as fractions of the unrotated page, from its bottom left.
	var fx0, fy0, fx1, fy1 float64
	switch ((rotate % 360) + 360) % 360 {
	case 90:
		// Shown turned clockwise: shown x runs along the page's y, shown y along its x.
		fx0, fx1 = a.Y, a.Y+a.Height
		fy0, fy1 = a.X, a.X+a.Width
	case 180:
		fx0, fx1 = 1-a.X-a.Width, 1-a.X
		fy0, fy1 = a.Y, a.Y+a.Height
	case 270:
		fx0, fx1 = 1-a.Y-a.Height, 1-a.Y
		fy0, fy1 = 1-a.X-a.Width, 1-a.X
	default:
		fx0, fx1 = a.X, a.X+a.Width
		fy0, fy1 = 1-a.Y-a.Height, 1-a.Y
	}
	r := func(v float64) float64 { return math.Round(v*100) / 100 }
	return r(x0 + w*fx0), r(y0 + h*fy0), r(x0 + w*fx1), r(y0 + h*fy1)
}

// PdfPageRef is one page of the edited PDF: a source file, its page (0-based)
// and the quarter turns added to it (degrees, a multiple of 90).
type PdfPageRef struct {
	File     int `json:"file"`
	Page     int `json:"page"`
	Rotation int `json:"rotation"`
}

// savePdfPages writes the pages in order to out: pages stay PDF objects,
// only /Rotate changes where asked.
func savePdfPages(ctx context.Context, paths []string, refs []PdfPageRef, out string) error {
	if len(refs) == 0 {
		return errors.New("no pages")
	}
	tmp, err := os.MkdirTemp("", "filee-pages")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	// Runs of pages from the same file are collected in one go.
	var parts []string
	for i := 0; i < len(refs); {
		j := i
		var sel []string
		for j < len(refs) && refs[j].File == refs[i].File {
			sel = append(sel, strconv.Itoa(refs[j].Page+1))
			j++
		}
		if refs[i].File < 0 || refs[i].File >= len(paths) {
			return fmt.Errorf("no file %d", refs[i].File)
		}
		part := filepath.Join(tmp, fmt.Sprintf("part%03d.pdf", len(parts)))
		if err := api.CollectFile(ctx, paths[refs[i].File], part, sel, nil); err != nil {
			return err
		}
		parts = append(parts, part)
		i = j
	}
	merged := filepath.Join(tmp, "merged.pdf")
	if len(parts) == 1 {
		merged = parts[0]
	} else if err := api.MergeCreateFile(ctx, parts, merged, false, nil); err != nil {
		return err
	}
	byAngle := map[int][]string{}
	for i, r := range refs {
		if a := ((r.Rotation % 360) + 360) % 360; a != 0 {
			byAngle[a] = append(byAngle[a], strconv.Itoa(i+1))
		}
	}
	for _, a := range []int{90, 180, 270} {
		if pages := byAngle[a]; len(pages) > 0 {
			if err := api.RotateFile(ctx, merged, "", a, pages, nil); err != nil {
				return err
			}
		}
	}
	return moveFile(merged, out)
}

func (s *server) pdfAdd(w http.ResponseWriter, r *http.Request, t *ToolJob) {
	var in struct {
		Paths []string `json:"paths"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	t.mu.Lock()
	for _, p := range in.Paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if fi, err := os.Stat(abs); err != nil || fi.IsDir() {
			continue
		}
		if f := formats.Detect(abs); f != nil && f.ID == "pdf" {
			t.Files = append(t.Files, ToolFile{Path: abs, Name: filepath.Base(abs), Format: f.ID, Category: f.Category, State: "waiting"})
		}
	}
	t.mu.Unlock()
	toolsOf(s.a).changed(t)
	writeJSON(w, t.snapshot())
}

func (s *server) pdfSave(w http.ResponseWriter, r *http.Request, t *ToolJob) {
	var in struct {
		Pages []PdfPageRef `json:"pages"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if len(in.Pages) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("no pages"))
		return
	}
	t.mu.Lock()
	var paths []string
	for _, f := range t.Files {
		paths = append(paths, f.Path)
	}
	p := t.Preset.Clone()
	t.mu.Unlock()
	applied := s.a.Settings().WithDefaultOutput(p)
	primary := paths[in.Pages[0].File]
	tokens := presets.Tokens{SourcePath: paths[0], PresetName: s.a.Texts().PresetName(applied), Index: 1, Now: time.Now()}
	out := presets.Resolve(&applied.Output, tokens, "pdf", nil, "")
	if out == "" {
		writeJSON(w, map[string]any{"skipped": true, "message": s.a.Text("toast.skipped")})
		return
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	if err := savePdfPages(ctx, paths, in.Pages, out); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("%s (%v)", s.a.Text("error.conversion_failed"), err))
		return
	}
	if applied.Output.KeepDates {
		if fi, err := os.Stat(primary); err == nil {
			_ = os.Chtimes(out, fi.ModTime(), fi.ModTime())
		}
	}
	var sources []string
	for _, ref := range in.Pages {
		if !slices.Contains(sources, paths[ref.File]) {
			sources = append(sources, paths[ref.File])
		}
	}
	s.record(p, t.Name, sources, []string{out})
	t.mu.Lock()
	t.pdfMade = append(t.pdfMade, out)
	t.mu.Unlock()
	writeJSON(w, map[string]any{"path": out, "message": s.a.Text("pdf_editor.saved", filepath.Base(out))})
}
