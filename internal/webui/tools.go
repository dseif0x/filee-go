package webui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dseif0x/filee-go/internal/app"
	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/jobs"
	"github.com/dseif0x/filee-go/internal/presets"
)

// Tool pages of the web UI (the "#/tool/<name>" routes).
const (
	ToolCrop      = "crop"
	ToolClip      = "clip"
	ToolHide      = "hide"
	ToolImageEdit = "image-edit"
	ToolPdfPages  = "pdf-pages"
	ToolPrint     = "print"
)

// ToolFor is the tool page a tool preset opens ("" for a preset that isn't a
// tool). As the original: the print studio and the PDF page tools are tool
// presets without a Tool of their own, so they are known by id first.
func ToolFor(p *presets.Preset) string {
	if p == nil || !p.IsTool() {
		return ""
	}
	switch {
	case p.IsClipTool():
		return ToolClip
	case p.IsHideTool():
		return ToolHide
	case p.IsImageEditTool():
		return ToolImageEdit
	case p.ID == "tool-sheet-studio":
		return ToolPrint
	case p.ID == "tool-pdf-pages", p.ID == "tool-pdf-add":
		return ToolPdfPages
	case p.IsCropTool():
		return ToolCrop
	}
	return ""
}

// ToolFile is one file a tool works on.
type ToolFile struct {
	Path     string           `json:"path"`
	Name     string           `json:"name"`
	Format   string           `json:"format"`
	Category formats.Category `json:"category"`
	// Target is the format the tool writes the file in (clip, hide, image edit).
	Target string `json:"target,omitempty"`
	// State is "waiting", "current", "running" (its conversion runs), "done", "skipped" or "failed".
	State   string   `json:"state"`
	Message string   `json:"message,omitempty"`
	Outputs []string `json:"outputs,omitempty"`
	JobID   string   `json:"jobId,omitempty"`
}

// ToolJob is one opened tool window: the tool, its preset and the files.
type ToolJob struct {
	ID      string          `json:"id"`
	Tool    string          `json:"tool"`
	Preset  *presets.Preset `json:"preset"`
	Name    string          `json:"name"`
	Files   []ToolFile      `json:"files"`
	Created time.Time       `json:"created"`
	// AddPages: the PDF page editor opened to add pages ("Add pages").
	AddPages bool `json:"addPages,omitempty"`
	// Notice says why files were left out (not pictures, not videos, …).
	Notice string `json:"notice,omitempty"`
	Closed bool   `json:"closed,omitempty"`

	mu      sync.Mutex
	clips   map[int]*clipSession
	sheets  *sheetSession
	pdfMade []string
}

// snapshot is a copy for JSON.
func (t *ToolJob) snapshot() *ToolJob {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := &ToolJob{ID: t.ID, Tool: t.Tool, Preset: t.Preset.Clone(), Name: t.Name, Files: slices.Clone(t.Files),
		Created: t.Created, AddPages: t.AddPages, Notice: t.Notice, Closed: t.Closed}
	c.Preset.Pdf.Password = ""
	return c
}

func (t *ToolJob) setFile(i int, f func(*ToolFile)) {
	t.mu.Lock()
	if i >= 0 && i < len(t.Files) {
		f(&t.Files[i])
	}
	t.mu.Unlock()
}

// toolRegistry holds the open tools of one app.
type toolRegistry struct {
	mu    sync.Mutex
	jobs  map[string]*ToolJob
	subs  map[int]chan *ToolJob
	subID int
}

var (
	registriesMu sync.Mutex
	registries   = map[*app.App]*toolRegistry{}
)

func toolsOf(a *app.App) *toolRegistry {
	registriesMu.Lock()
	defer registriesMu.Unlock()
	r := registries[a]
	if r == nil {
		r = &toolRegistry{jobs: map[string]*ToolJob{}, subs: map[int]chan *ToolJob{}}
		registries[a] = r
	}
	return r
}

func (r *toolRegistry) get(id string) *ToolJob {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.jobs[id]
}

func (r *toolRegistry) subscribe() (<-chan *ToolJob, func()) {
	ch := make(chan *ToolJob, 64)
	r.mu.Lock()
	id := r.subID
	r.subID++
	r.subs[id] = ch
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		delete(r.subs, id)
		r.mu.Unlock()
	}
}

func (r *toolRegistry) changed(t *ToolJob) {
	s := t.snapshot()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ch := range r.subs {
		select {
		case ch <- s:
		default:
		}
	}
}

// NewToolJob opens a tool on the files and returns the id for the tool page
// (baseURL + "/#/tool/<tool>?job=<id>"). presetID is the tool preset (by id
// or name); tool may be "" to take the preset's own tool page, and presetID
// may be "" to use the first built-in preset of the tool. Files the tool
// can't open are left out; an error when none is left.
func NewToolJob(a *app.App, tool string, paths []string, presetID string) (id string, err error) {
	t, err := newToolJob(a, tool, paths, presetID)
	if err != nil {
		return "", err
	}
	return t.ID, nil
}

// defaultToolPresets are the built-in presets each tool opens with when none is named.
var defaultToolPresets = map[string]string{
	ToolCrop: "tool-crop", ToolClip: "tool-clip", ToolHide: "tool-hide", ToolImageEdit: "tool-image-edit",
	ToolPdfPages: "tool-pdf-pages", ToolPrint: "tool-sheet-studio",
}

func newToolJob(a *app.App, tool string, paths []string, presetID string) (*ToolJob, error) {
	var p *presets.Preset
	if presetID != "" {
		if p = a.Preset(presetID); p == nil {
			p = a.FindPreset(presetID)
		}
		if p == nil {
			return nil, fmt.Errorf("no preset %q", presetID)
		}
	}
	if tool == "" {
		tool = ToolFor(p)
	}
	if _, ok := defaultToolPresets[tool]; !ok {
		return nil, fmt.Errorf("no tool %q", tool)
	}
	if p == nil {
		if p = a.Preset(defaultToolPresets[tool]); p == nil {
			p = presets.FindBuiltIn(defaultToolPresets[tool])
		}
		if p == nil {
			p = presets.New("", presets.SameAsSource)
			p.Kind, p.Tool = presets.KindTool, tool
		}
	}
	t := &ToolJob{Tool: tool, Preset: p.Clone(), Name: a.Texts().PresetName(p), Created: time.Now(), clips: map[int]*clipSession{}}
	t.AddPages = p.ID == "tool-pdf-add"
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	t.ID = hex.EncodeToString(b)

	planner := a.Planner()
	left := 0
	for _, path := range paths {
		abs, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		fi, err := os.Stat(abs)
		if err != nil || fi.IsDir() {
			left++
			continue
		}
		f := formats.Detect(abs)
		if f == nil || !toolOpens(tool, f.ID) {
			left++
			continue
		}
		tf := ToolFile{Path: abs, Name: filepath.Base(abs), Format: f.ID, Category: f.Category, State: "waiting"}
		switch tool {
		case ToolClip:
			tf.Target = presets.ClipTargetFor(f.ID, planner)
		case ToolHide:
			tf.Target = presets.HideTargetFor(f.ID, planner)
		}
		t.Files = append(t.Files, tf)
	}
	if len(t.Files) == 0 {
		return nil, errors.New(a.Text("error.unsupported_source"))
	}
	if left > 0 {
		t.Notice = a.Text("tool.left_out", left)
		if t.Notice == "tool.left_out" {
			t.Notice = fmt.Sprintf("%d file(s) left out: the tool can't open them.", left)
		}
	}
	t.Files[0].State = "current"
	r := toolsOf(a)
	r.mu.Lock()
	r.jobs[t.ID] = t
	// Tools closed a day ago are forgotten.
	for id, j := range r.jobs {
		if time.Since(j.Created) > 24*time.Hour {
			delete(r.jobs, id)
		}
	}
	r.mu.Unlock()
	return t, nil
}

// toolOpens: the tool can work on a file of format id.
func toolOpens(tool, id string) bool {
	switch tool {
	case ToolCrop:
		return formats.CategoryOf(id) == formats.Image || id == "pdf"
	case ToolHide:
		return presets.CanHide(id)
	case ToolImageEdit:
		return presets.CanEditImage(id)
	case ToolClip:
		return formats.CategoryOf(id) == formats.Video
	case ToolPdfPages:
		return id == "pdf"
	case ToolPrint:
		return id == "xlsx"
	}
	return false
}

// ---- routes ----

func (s *server) toolRoutes() {
	m := s.mux
	m.HandleFunc("POST /api/tools", s.openTool)
	m.HandleFunc("GET /api/tools/{id}", s.tool(func(w http.ResponseWriter, r *http.Request, t *ToolJob) {
		writeJSON(w, t.snapshot())
	}))
	m.HandleFunc("DELETE /api/tools/{id}", s.tool(func(w http.ResponseWriter, r *http.Request, t *ToolJob) {
		t.mu.Lock()
		t.Closed = true
		t.mu.Unlock()
		s.releaseTool(t)
		toolsOf(s.a).changed(t)
		writeJSON(w, map[string]bool{"ok": true})
	}))
	m.HandleFunc("GET /api/tools/{id}/files/{i}", s.toolFile(s.serveSource))
	m.HandleFunc("GET /api/tools/{id}/files/{i}/preview", s.toolFile(s.servePreview))
	m.HandleFunc("GET /api/tools/{id}/files/{i}/info", s.toolFile(s.fileInfo))
	m.HandleFunc("POST /api/tools/{id}/files/{i}/skip", s.toolFile(func(w http.ResponseWriter, r *http.Request, t *ToolJob, i int) {
		t.setFile(i, func(f *ToolFile) { f.State = "skipped" })
		s.advance(t, i)
		writeJSON(w, t.snapshot())
	}))
	m.HandleFunc("POST /api/tools/{id}/files/{i}/crop", s.toolFile(s.crop))
	m.HandleFunc("POST /api/tools/{id}/files/{i}/hide", s.toolFile(s.hide))
	m.HandleFunc("POST /api/tools/{id}/image-edit", s.tool(s.imageEdit))
	m.HandleFunc("POST /api/tools/{id}/files/{i}/clip", s.toolFile(s.clipOp))
	m.HandleFunc("POST /api/tools/{id}/files/{i}/clip/save", s.toolFile(s.clipSave))
	m.HandleFunc("GET /api/tools/{id}/files/{i}/clip/media", s.toolFile(s.clipMedia))
	m.HandleFunc("POST /api/tools/{id}/pdf/add", s.tool(s.pdfAdd))
	m.HandleFunc("POST /api/tools/{id}/pdf/save", s.tool(s.pdfSave))
	m.HandleFunc("GET /api/tools/{id}/print", s.tool(s.printInspect))
	m.HandleFunc("POST /api/tools/{id}/print/preview", s.tool(s.printPreview))
	m.HandleFunc("GET /api/tools/{id}/print/page/{n}", s.tool(s.printPage))
	m.HandleFunc("POST /api/tools/{id}/print/export", s.tool(s.printExport))
}

func (s *server) openTool(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Tool     string   `json:"tool"`
		Paths    []string `json:"paths"`
		PresetID string   `json:"presetId"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	t, err := newToolJob(s.a, in.Tool, in.Paths, in.PresetID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, map[string]any{"id": t.ID, "tool": t.Tool, "url": "#/tool/" + t.Tool + "?job=" + t.ID})
}

func (s *server) tool(h func(http.ResponseWriter, *http.Request, *ToolJob)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := toolsOf(s.a).get(r.PathValue("id"))
		if t == nil {
			writeError(w, http.StatusNotFound, errors.New("this tool window is no longer open"))
			return
		}
		h(w, r, t)
	}
}

func (s *server) toolFile(h func(http.ResponseWriter, *http.Request, *ToolJob, int)) http.HandlerFunc {
	return s.tool(func(w http.ResponseWriter, r *http.Request, t *ToolJob) {
		i, err := strconv.Atoi(r.PathValue("i"))
		t.mu.Lock()
		n := len(t.Files)
		t.mu.Unlock()
		if err != nil || i < 0 || i >= n {
			writeError(w, http.StatusNotFound, errors.New("no such file"))
			return
		}
		h(w, r, t, i)
	})
}

func (t *ToolJob) file(i int) ToolFile {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Files[i]
}

// advance makes the next waiting file the current one.
func (s *server) advance(t *ToolJob, done int) {
	t.mu.Lock()
	for k := range t.Files {
		if t.Files[k].State == "current" && k != done {
			t.mu.Unlock()
			toolsOf(s.a).changed(t)
			return
		}
	}
	for k := done + 1; k < len(t.Files); k++ {
		if t.Files[k].State == "waiting" {
			t.Files[k].State = "current"
			break
		}
	}
	t.mu.Unlock()
	toolsOf(s.a).changed(t)
}

// serveSource serves the file itself (with ranges: the clip editor's <video>).
func (s *server) serveSource(w http.ResponseWriter, r *http.Request, t *ToolJob, i int) {
	f := t.file(i)
	fh, err := os.Open(f.Path)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	defer fh.Close()
	fi, err := fh.Stat()
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=60")
	if ct := mediaType(f.Format); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	http.ServeContent(w, r, "", fi.ModTime(), fh)
}

func mediaType(format string) string {
	switch format {
	case "mp4", "m4v":
		return "video/mp4"
	case "mov":
		return "video/quicktime"
	case "webm":
		return "video/webm"
	case "ogv":
		return "video/ogg"
	case "mkv":
		return "video/x-matroska"
	case "mp3":
		return "audio/mpeg"
	case "m4a":
		return "audio/mp4"
	case "wav":
		return "audio/wav"
	case "pdf":
		return "application/pdf"
	}
	return ""
}

func (s *server) servePreview(w http.ResponseWriter, r *http.Request, t *ToolJob, i int) {
	f := t.file(i)
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	maxDim, _ := strconv.Atoi(r.URL.Query().Get("max"))
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	p, err := s.conv.preview(ctx, f.Path, page, maxDim)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "image/png")
	h.Set("Cache-Control", "private, max-age=300")
	h.Set("X-Width", strconv.Itoa(p.Width))
	h.Set("X-Height", strconv.Itoa(p.Height))
	h.Set("X-Pages", strconv.Itoa(p.Pages))
	_, _ = w.Write(p.PNG)
}

func (s *server) fileInfo(w http.ResponseWriter, r *http.Request, t *ToolJob, i int) {
	f := t.file(i)
	info := map[string]any{"path": f.Path, "name": f.Name, "format": f.Format}
	if fi, err := os.Stat(f.Path); err == nil {
		info["size"] = fi.Size()
	}
	if w0, h0, ok := imageSize(f.Path); ok {
		info["width"], info["height"] = w0, h0
	}
	if f.Format == "pdf" {
		if n, err := pdfPageCount(f.Path); err == nil {
			info["pages"] = n
		}
	}
	if f.Category == formats.Video {
		info["playable"] = browserPlays(f.Format)
		if d, ok := probeDuration(r.Context(), f.Path); ok {
			info["duration"] = d
		}
	}
	writeJSON(w, info)
}

// ---- results ----

// track waits for a job the tool started and keeps the file's state in step.
func (s *server) track(t *ToolJob, i int, jobID string) {
	t.setFile(i, func(f *ToolFile) { f.JobID = jobID; f.State = "running"; f.Message = "" })
	toolsOf(s.a).changed(t)
	go func() {
		ch, stop := s.a.Events(64)
		defer stop()
		timeout := time.After(6 * time.Hour)
		for {
			if j := s.a.Job(jobID); j != nil && j.State().Final() {
				s.jobDone(t, i, jobID)
				return
			}
			select {
			case <-ch:
			case <-timeout:
				return
			}
		}
	}()
}

func (s *server) jobDone(t *ToolJob, i int, jobID string) {
	j := s.a.Job(jobID)
	if j == nil {
		return
	}
	snap := j.Snapshot()
	t.setFile(i, func(f *ToolFile) {
		if f.JobID != jobID {
			return
		}
		failed, found := false, false
		f.Outputs = nil
		for _, fr := range snap.Files {
			if fr.SourcePath != f.Path && len(snap.Files) > 1 {
				continue
			}
			found = true
			f.Outputs = append(f.Outputs, fr.Outputs...)
			if fr.State == jobs.FileFailed || fr.State == jobs.FileCancelled {
				failed = true
				msg := s.a.Text(orDefault(fr.ErrorKey, "error.conversion_failed"))
				if fr.ErrorDetail != "" {
					msg += " (" + fr.ErrorDetail + ")"
				}
				f.Message = msg
			}
		}
		if !found {
			// Joined results (one output for every source).
			f.Outputs = snap.Outputs()
		}
		if failed {
			f.State = "failed"
		} else if len(f.Outputs) == 0 {
			f.State = "skipped"
		} else {
			f.State = "done"
		}
	})
	toolsOf(s.a).changed(t)
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func (s *server) releaseTool(t *ToolJob) {
	t.mu.Lock()
	sheets := t.sheets
	t.sheets = nil
	t.mu.Unlock()
	if sheets != nil {
		sheets.release()
	}
}

// remember saves the tool preset's remembered choices (hide style, image edits).
func (s *server) remember(t *ToolJob, change func(*presets.Preset)) {
	t.mu.Lock()
	change(t.Preset)
	t.mu.Unlock()
	if saved := s.a.Preset(t.Preset.ID); saved != nil && saved.IsTool() {
		change(saved)
		if err := s.a.SavePreset(saved); err != nil {
			s.log.Warn("could not remember the tool's choices", "error", err)
		}
	}
}
