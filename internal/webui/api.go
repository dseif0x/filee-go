package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/dseif0x/filee-go/internal/app"
	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/i18n"
	"github.com/dseif0x/filee-go/internal/jobs"
	"github.com/dseif0x/filee-go/internal/presets"
	"github.com/dseif0x/filee-go/internal/profiles"
	"github.com/dseif0x/filee-go/internal/settings"
	"github.com/dseif0x/filee-go/internal/watch"
	"github.com/dseif0x/filee-go/internal/web"
)

// maxJSON bounds a JSON request body.
const maxJSON = 8 << 20

func (s *server) routes() {
	m := s.mux
	m.HandleFunc("GET /", s.serveStatic)

	m.HandleFunc("GET /api/bootstrap", s.bootstrap)
	m.HandleFunc("GET /api/texts", s.texts)
	m.HandleFunc("GET /api/events", s.events)

	m.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.a.Settings()) })
	m.HandleFunc("PUT /api/settings", s.putSettings)

	m.HandleFunc("GET /api/library", s.library)
	m.HandleFunc("POST /api/presets", s.savePreset)
	m.HandleFunc("PUT /api/presets/{id}", s.savePreset)
	m.HandleFunc("DELETE /api/presets/{id}", s.deletePreset)
	m.HandleFunc("POST /api/presets/reorder", s.reorderPresets)
	m.HandleFunc("GET /api/presets/factory", s.factoryPresets)
	m.HandleFunc("POST /api/library/reset", func(w http.ResponseWriter, r *http.Request) { writeResult(w, nil, s.a.ResetLibrary()) })
	m.HandleFunc("GET /api/library/export", s.exportLibrary)
	m.HandleFunc("POST /api/library/import", s.importLibrary)
	m.HandleFunc("POST /api/profiles", s.saveProfile)
	m.HandleFunc("PUT /api/profiles/{id}", s.saveProfile)
	m.HandleFunc("DELETE /api/profiles/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeResult(w, nil, s.a.DeleteProfile(r.PathValue("id")))
	})
	m.HandleFunc("POST /api/profiles/{id}/move", s.moveProfile)
	m.HandleFunc("POST /api/profiles/{id}/reset", func(w http.ResponseWriter, r *http.Request) {
		ok, err := s.a.ResetProfile(r.PathValue("id"))
		writeResult(w, map[string]bool{"reset": ok}, err)
	})

	m.HandleFunc("GET /api/engines", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.engines()) })
	m.HandleFunc("POST /api/engines/refresh", func(w http.ResponseWriter, r *http.Request) {
		s.a.RefreshEngines()
		writeJSON(w, s.engines())
	})
	m.HandleFunc("GET /api/profiles/{id}/usable", s.profileUsable)
	m.HandleFunc("GET /api/history/thumb", s.historyThumb)
	m.HandleFunc("GET /api/targets", s.targets)
	m.HandleFunc("POST /api/donut", s.donut)

	m.HandleFunc("GET /api/history", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.history()) })
	m.HandleFunc("DELETE /api/history", func(w http.ResponseWriter, r *http.Request) { writeResult(w, nil, s.a.ClearHistory()) })
	m.HandleFunc("POST /api/history/{job}/undo", s.undo)
	m.HandleFunc("POST /api/history/{job}/retry", func(w http.ResponseWriter, r *http.Request) {
		j, err := s.a.Retry(r.PathValue("job"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, j.Snapshot())
	})

	m.HandleFunc("GET /api/watch", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.a.WatchFolders()) })
	m.HandleFunc("POST /api/watch/start", func(w http.ResponseWriter, r *http.Request) {
		s.a.StartWatchers()
		writeJSON(w, s.a.WatchFolders())
	})
	m.HandleFunc("POST /api/watch/pattern", s.watchPattern)
	m.HandleFunc("POST /api/rename/check", s.renameCheck)

	m.HandleFunc("GET /api/jobs", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.a.Jobs()) })
	m.HandleFunc("POST /api/jobs", s.convert)
	m.HandleFunc("POST /api/jobs/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]bool{"cancelled": s.a.Cancel(r.PathValue("id"))})
	})
	m.HandleFunc("POST /api/upload", s.upload)

	m.HandleFunc("GET /api/fs", s.listFolder)
	m.HandleFunc("POST /api/open", s.open)
	m.HandleFunc("GET /api/theme", s.customTheme)
	m.HandleFunc("GET /api/update", s.checkUpdate)

	s.toolRoutes()
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

type apiError struct {
	Error string `json:"error"`
	Key   string `json:"key,omitempty"`
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiError{Error: err.Error()})
}

// writeResult writes v (or {"ok":true}) or the error.
func writeResult(w http.ResponseWriter, v any, err error) {
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if v == nil {
		v = map[string]bool{"ok": true}
	}
	writeJSON(w, v)
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSON))
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("bad request: %w", err))
		return false
	}
	return true
}

// ---- bootstrap ----

type formatInfo struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	Category   formats.Category `json:"category"`
	Extensions []string         `json:"extensions"`
	Writable   bool             `json:"writable"`
}

func (s *server) bootstrap(w http.ResponseWriter, r *http.Request) {
	var fs []formatInfo
	for _, f := range formats.Known {
		fs = append(fs, formatInfo{ID: f.ID, Name: f.Name, Category: f.Category, Extensions: f.Extensions, Writable: f.Writable()})
	}
	p := s.a.Platform()
	writeJSON(w, map[string]any{
		"version":          app.Version,
		"os":               runtime.GOOS,
		"language":         s.a.Texts().Language(),
		"languages":        i18n.Languages(),
		"texts":            s.textTable(),
		"dataDir":          s.a.DataDir(),
		"downloads":        p.DownloadsFolder(),
		"formats":          fs,
		"categories":       formats.Categories,
		"reservedFlipKeys": settings.ReservedFlipKeys,
		"supportsDock":     p.SupportsDockVisibility(),
		"supportsGestures": p.SupportsGlobalPointerGestures(),
		"supportsOpenWith": p.SupportsOpenWith(),
		"limits": map[string]any{
			"maxSlices": profiles.MaxSlices, "maxSubPresets": presets.MaxSubPresets,
			"minHistory": settings.MinHistoryLimit, "maxHistory": settings.MaxHistoryLimit,
			"minDonutOpacity": settings.MinDonutOpacity, "minTraceColors": presets.MinTraceColors,
			"maxTraceColors": presets.MaxTraceColors, "minSide": presets.MinSide, "maxSide": presets.MaxSide,
			"clipSpeeds": presets.ClipSpeeds, "animationSpeeds": presets.AnimationSpeeds,
			"maxClipFade": presets.MaxClipFade, "maxClipVolume": presets.MaxClipVolume,
			"minHideStrength": presets.MinHideStrength, "maxHideStrength": presets.MaxHideStrength,
		},
		"themeColorKeys": settings.ThemeColorKeys,
	})
}

// textTable is every text of the language in use (English behind it).
func (s *server) textTable() map[string]string {
	l := s.a.Texts()
	out := map[string]string{}
	for _, k := range l.Keys() {
		out[k] = l.Text(k)
	}
	return out
}

func (s *server) texts(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.textTable()) }

// ---- settings ----

func (s *server) putSettings(w http.ResponseWriter, r *http.Request) {
	var in settings.AppSettings
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.a.SaveSettings(&in); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, s.a.Settings())
}

// ---- library ----

type libraryResponse struct {
	Presets  []*presets.Preset   `json:"presets"`
	Profiles []*profiles.Profile `json:"profiles"`
	// CanReset: profile id → it is a built-in donut that can be put back.
	CanReset map[string]bool `json:"canReset"`
	// Builtin are the ids of the factory presets.
	Builtin []string `json:"builtin"`
}

func (s *server) library(w http.ResponseWriter, r *http.Request) {
	out := libraryResponse{Presets: s.a.Presets(), Profiles: s.a.Profiles(), CanReset: map[string]bool{}}
	for _, p := range out.Profiles {
		out.CanReset[p.ID] = s.a.CanResetProfile(p.ID)
	}
	for _, p := range presets.BuiltIn() {
		out.Builtin = append(out.Builtin, p.ID)
	}
	writeJSON(w, out)
}

func (s *server) savePreset(w http.ResponseWriter, r *http.Request) {
	var p presets.Preset
	if !readJSON(w, r, &p) {
		return
	}
	if id := r.PathValue("id"); id != "" {
		p.ID = id
	} else if r.URL.Query().Get("new") == "1" || p.ID == "" {
		p.ID = presets.NewID()
	}
	p.Normalize()
	if err := s.a.SavePreset(&p); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, s.a.Preset(p.ID))
}

func (s *server) deletePreset(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Its slices go from the donuts and groups too.
	for _, prof := range s.a.Profiles() {
		front := slices.DeleteFunc(slices.Clone(prof.PresetIDs), func(x string) bool { return x == id })
		back := slices.DeleteFunc(slices.Clone(prof.BackPresetIDs), func(x string) bool { return x == id })
		if len(front) != len(prof.PresetIDs) || len(back) != len(prof.BackPresetIDs) {
			prof.PresetIDs, prof.BackPresetIDs = front, back
			_ = s.a.SaveProfile(prof)
		}
	}
	for _, g := range s.a.Presets() {
		if g.IsGroup() && slices.Contains(g.SubPresetIDs, id) {
			g.SubPresetIDs = slices.DeleteFunc(g.SubPresetIDs, func(x string) bool { return x == id })
			_ = s.a.SavePreset(g)
		}
	}
	writeResult(w, nil, s.a.DeletePreset(id))
}

func (s *server) reorderPresets(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if readJSON(w, r, &in) {
		writeResult(w, nil, s.a.ReorderPresets(in.IDs))
	}
}

func (s *server) factoryPresets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"builtin": s.a.BuiltInPresets(), "place": s.a.PlacePresets()})
}

func (s *server) exportLibrary(w http.ResponseWriter, r *http.Request) {
	s.exportMu.Lock()
	defer s.exportMu.Unlock()
	dir, err := os.MkdirTemp("", "filee-export")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "filee-library.json")
	if err := s.a.ExportLibrary(path); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if to := strings.TrimSpace(r.URL.Query().Get("to")); to != "" {
		// Saved straight into a folder the user picked in the folder browser.
		b, err := os.ReadFile(path)
		if err == nil {
			dest := filepath.Join(to, "Filee library "+time.Now().Format("2006-01-02")+".json")
			err = os.WriteFile(dest, b, 0o644)
			writeResult(w, map[string]string{"path": dest}, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="filee-library.json"`)
	w.Header().Set("Content-Type", "application/json")
	http.ServeFile(w, r, path)
}

func (s *server) importLibrary(w http.ResponseWriter, r *http.Request) {
	f, err := os.CreateTemp("", "filee-import-*.json")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer os.Remove(f.Name())
	_, err = io.Copy(f, http.MaxBytesReader(w, r.Body, 64<<20))
	f.Close()
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	n, err := s.a.ImportLibrary(f.Name())
	writeResult(w, map[string]int{"imported": n}, err)
}

func (s *server) saveProfile(w http.ResponseWriter, r *http.Request) {
	var p profiles.Profile
	if !readJSON(w, r, &p) {
		return
	}
	if id := r.PathValue("id"); id != "" {
		p.ID = id
	}
	if p.Extensions == nil {
		p.Extensions = []string{}
	}
	if p.PresetIDs == nil {
		p.PresetIDs = []string{}
	}
	if p.BackPresetIDs == nil {
		p.BackPresetIDs = []string{}
	}
	if len(p.PresetIDs) > profiles.MaxSlices || len(p.BackPresetIDs) > profiles.MaxSlices {
		writeError(w, http.StatusBadRequest, fmt.Errorf("a donut holds at most %d slices", profiles.MaxSlices))
		return
	}
	if err := s.a.SaveProfile(&p); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, p)
}

func (s *server) moveProfile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Direction int `json:"direction"`
	}
	if readJSON(w, r, &in) {
		ok, err := s.a.MoveProfile(r.PathValue("id"), in.Direction)
		writeResult(w, map[string]bool{"moved": ok}, err)
	}
}

// ---- engines, targets, donut ----

type engineInfo struct {
	app.Engine
	Reason string `json:"reason,omitempty"`
	// Formats are the formats it reads and writes.
	Reads  []string `json:"reads"`
	Writes []string `json:"writes"`
}

func (s *server) engines() []engineInfo {
	var out []engineInfo
	texts := s.a.Texts()
	for _, e := range s.a.Engines() {
		info := engineInfo{Engine: e, Reads: []string{}, Writes: []string{}}
		if e.ReasonKey != "" {
			info.Reason = texts.Text(e.ReasonKey)
		}
		if c := s.a.Catalog().Find(e.ID); c != nil {
			for _, edge := range c.Edges() {
				if !slices.Contains(info.Reads, edge.From) {
					info.Reads = append(info.Reads, edge.From)
				}
				if !slices.Contains(info.Writes, edge.To) {
					info.Writes = append(info.Writes, edge.To)
				}
			}
		}
		out = append(out, info)
	}
	if out == nil {
		out = []engineInfo{}
	}
	return out
}

func (s *server) targets(w http.ResponseWriter, r *http.Request) {
	from := r.URL.Query().Get("from")
	t := s.a.Planner().Targets(from)
	if t == nil {
		t = []string{}
	}
	writeJSON(w, t)
}

func (s *server) donut(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Paths []string `json:"paths"`
	}
	if readJSON(w, r, &in) {
		writeJSON(w, s.a.DonutFor(in.Paths))
	}
}

// ---- history ----

type historyEntry struct {
	JobID       string            `json:"jobId"`
	FinishedAt  time.Time         `json:"finishedAt"`
	PresetID    string            `json:"presetId"`
	PresetName  string            `json:"presetName"`
	State       jobs.JobState     `json:"state"`
	Sources     []string          `json:"sources"`
	Outputs     []string          `json:"outputs"`
	WatchRuleID string            `json:"watchRuleId,omitempty"`
	Failures    []failureInfo     `json:"failures"`
	CanUndo     bool              `json:"canUndo"`
	Undone      bool              `json:"undone"`
	Existing    map[string]bool   `json:"existing"`
	Sizes       map[string]int64  `json:"sizes"`
	Extra       map[string]string `json:"extra,omitempty"`
}

type failureInfo struct {
	Source  string `json:"source"`
	Message string `json:"message"`
}

func (s *server) history() []historyEntry {
	texts := s.a.Texts()
	out := []historyEntry{}
	for _, e := range s.a.History() {
		h := historyEntry{
			JobID: e.JobID, FinishedAt: e.FinishedAt, PresetID: e.PresetID, PresetName: e.PresetName, State: e.State,
			Sources: e.Sources, Outputs: e.Outputs, WatchRuleID: e.WatchRuleID, CanUndo: e.CanUndo(), Undone: e.Undone,
			Existing: map[string]bool{}, Sizes: map[string]int64{}, Failures: []failureInfo{},
		}
		for _, f := range e.Failures {
			msg := texts.Text(f.ErrorKey)
			if f.ErrorKey == "" {
				msg = texts.Text("error.conversion_failed")
			}
			if f.ErrorDetail != "" {
				msg += " (" + f.ErrorDetail + ")"
			}
			h.Failures = append(h.Failures, failureInfo{Source: f.Source, Message: msg})
		}
		for _, o := range e.Outputs {
			if fi, err := os.Stat(o); err == nil {
				h.Existing[o] = true
				h.Sizes[o] = fi.Size()
			}
		}
		out = append(out, h)
	}
	return out
}

func (s *server) undo(w http.ResponseWriter, r *http.Request) {
	refusal, err := s.a.Undo(r.PathValue("job"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	res := map[string]any{"ok": refusal == nil}
	if refusal != nil {
		res["reason"] = s.a.Texts().Notice("home.undo_failed", refusal.ReasonKey, refusal.Detail)
	}
	writeJSON(w, res)
}

// ---- watch folders, renames ----

func (s *server) watchPattern(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Typed string `json:"typed"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	p := watch.NormalizePattern(in.Typed)
	writeJSON(w, map[string]string{"pattern": p, "error": watch.ValidatePattern(p)})
}

func (s *server) renameCheck(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Sample  string               `json:"sample"`
		Pattern string               `json:"pattern"`
		Preset  string               `json:"preset"`
		Steps   []presets.RenameStep `json:"steps"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	name := presets.ExpandPattern(in.Pattern, presets.Tokens{SourcePath: in.Sample, PresetName: in.Preset, Index: 1, Now: time.Now()})
	errs := make([]string, len(in.Steps))
	for i, st := range in.Steps {
		if st.Kind == presets.RenameRegex {
			errs[i] = presets.RenameError(st.Find)
		}
		name = st.Apply(name)
	}
	writeJSON(w, map[string]any{"name": presets.Sanitize(name, formats.NameWithoutExtension(in.Sample)), "errors": errs})
}

// ---- converting ----

func (s *server) convert(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Paths    []string `json:"paths"`
		PresetID string   `json:"presetId"`
		Format   string   `json:"format"`
		// Folder saves the results there (files uploaded from the browser).
		Folder string `json:"folder"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if len(in.Paths) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("no files"))
		return
	}
	var p *presets.Preset
	switch {
	case in.PresetID != "":
		p = s.a.FindPreset(in.PresetID)
	case in.Format != "":
		if f := app.FindFormat(in.Format); f != nil && f.Writable() {
			p = presets.New("", f.ID)
		}
	}
	if p == nil || p.IsGroup() || p.IsTool() {
		writeError(w, http.StatusBadRequest, errors.New("no such conversion"))
		return
	}
	if in.Folder != "" {
		p = presets.ForChosenFolder(s.a.Settings().WithDefaultOutput(p), in.Folder)
	}
	j := s.a.ConvertWith(in.Paths, p, s.a.Texts().PresetName(p), nil)
	writeJSON(w, j.Snapshot())
}

// upload saves files dropped on the browser page into a drop folder of
// their own (the browser hands over contents, not paths).
func (s *server) upload(w http.ResponseWriter, r *http.Request) {
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	dir, err := os.MkdirTemp(web.DefaultDropRoot(), "upload-")
	if err != nil {
		if err = os.MkdirAll(web.DefaultDropRoot(), 0o755); err == nil {
			dir, err = os.MkdirTemp(web.DefaultDropRoot(), "upload-")
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	var paths []string
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if part.FileName() == "" {
			continue
		}
		path, err := savePart(dir, part)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		paths = append(paths, path)
	}
	writeJSON(w, map[string]any{"paths": paths})
}

func savePart(dir string, part *multipart.Part) (string, error) {
	name := presets.Sanitize(filepath.Base(part.FileName()), "file")
	path := filepath.Join(dir, name)
	for n := 2; ; n++ {
		if _, err := os.Lstat(path); err != nil {
			break
		}
		ext := filepath.Ext(name)
		path = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, ext), n, ext))
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, part)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return path, err
}

// ---- files and folders ----

type folderListing struct {
	Path    string   `json:"path"`
	Parent  string   `json:"parent"`
	Folders []string `json:"folders"`
	Files   []string `json:"files,omitempty"`
	Home    string   `json:"home"`
	Places  []string `json:"places"`
}

// listFolder backs the folder picker: the web page can't open the system's.
func (s *server) listFolder(w http.ResponseWriter, r *http.Request) {
	home, _ := os.UserHomeDir()
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		path = home
	}
	path = filepath.Clean(path)
	entries, err := os.ReadDir(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	out := folderListing{Path: path, Folders: []string{}, Home: home, Places: places(home)}
	if parent := filepath.Dir(path); parent != path {
		out.Parent = parent
	}
	withFiles := r.URL.Query().Get("files") == "1"
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		isDir := e.IsDir()
		if e.Type()&os.ModeSymlink != 0 {
			if fi, err := os.Stat(filepath.Join(path, e.Name())); err == nil {
				isDir = fi.IsDir()
			}
		}
		if isDir {
			out.Folders = append(out.Folders, e.Name())
		} else if withFiles {
			out.Files = append(out.Files, e.Name())
		}
	}
	writeJSON(w, out)
}

func places(home string) []string {
	var out []string
	for _, p := range []string{home, filepath.Join(home, "Desktop"), filepath.Join(home, "Documents"), filepath.Join(home, "Downloads"), filepath.Join(home, "Pictures")} {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			out = append(out, p)
		}
	}
	if runtime.GOOS == "windows" {
		for c := 'C'; c <= 'Z'; c++ {
			d := string(c) + `:\`
			if _, err := os.Stat(d); err == nil {
				out = append(out, d)
			}
		}
	} else {
		out = append(out, "/")
	}
	return out
}

func (s *server) open(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path   string `json:"path"`
		Reveal bool   `json:"reveal"`
		With   bool   `json:"with"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if _, err := os.Stat(in.Path); err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	p := s.a.Platform()
	switch {
	case in.With && p.SupportsOpenWith():
		p.OpenWith(in.Path)
	case in.Reveal:
		p.RevealInFileManager(in.Path)
	default:
		p.OpenFile(in.Path)
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *server) customTheme(w http.ResponseWriter, r *http.Request) {
	t := settings.LoadCustomTheme(s.a.DataDir(), nil)
	writeJSON(w, map[string]any{"theme": t, "path": filepath.Join(s.a.DataDir(), settings.ThemeFileName)})
}

// profileUsable tells for every preset whether a file of the profile's
// extensions converts with it (the palette dims the others).
func (s *server) profileUsable(w http.ResponseWriter, r *http.Request) {
	var prof *profiles.Profile
	for _, p := range s.a.Profiles() {
		if p.ID == r.PathValue("id") {
			prof = p
		}
	}
	if prof == nil {
		writeError(w, http.StatusNotFound, errors.New("no such profile"))
		return
	}
	planner := s.a.Planner()
	var sources []string
	for _, e := range prof.Extensions {
		if f := formats.ByExtension(e); f != nil && !slices.Contains(sources, f.ID) {
			sources = append(sources, f.ID)
		}
	}
	out := map[string]bool{}
	for _, p := range s.a.Presets() {
		ok := p.IsGroup() || p.IsTool() || p.PacksIntoOneArchive() || len(sources) == 0
		for _, src := range sources {
			if ok {
				break
			}
			target := p.TargetFormat
			if target == presets.SameAsSource {
				target = src
			}
			o := convert.Options{Frames: p.SavesFramesOf(src), MotionPhoto: p.ExtractsMotionOf(src), LivePhoto: p.MakesLivePhotoOf(src), Ocr: p.RecognizesTextOf(src)}
			ok = planner.Plan(src, target, o) != nil
		}
		out[p.ID] = ok
	}
	writeJSON(w, out)
}

// historyThumb is a small picture of a result the history lists.
func (s *server) historyThumb(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	known := false
	for _, e := range s.a.History() {
		if slices.Contains(e.Outputs, path) {
			known = true
			break
		}
	}
	if !known {
		writeError(w, http.StatusNotFound, errors.New("not a result in the history"))
		return
	}
	p, err := s.conv.preview(r.Context(), path, 0, 96)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=600")
	_, _ = w.Write(p.PNG)
}
