package webui

import (
	"context"
	"errors"
	"net/http"
	"runtime"
	"sync"

	"github.com/dseif0x/filee-go/internal/app"
	"github.com/dseif0x/filee-go/internal/engines/install"
	"github.com/dseif0x/filee-go/internal/tools"
)

// The optional engines (Settings → Engines): download, cancel, remove, with
// the progress of a running install. The page polls while one runs.

var goos = runtime.GOOS

type packageInfo struct {
	ID            string   `json:"id"`
	Installed     bool     `json:"installed"`
	UpToDate      bool     `json:"upToDate"`
	CanDownload   bool     `json:"canDownload"`
	DownloadSize  int64    `json:"downloadSize"`
	InstalledSize int64    `json:"installedSize"`
	Version       string   `json:"version,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	Converters    []string `json:"converters"`
	// State: "", "downloading", "unpacking", "failed".
	State    string  `json:"state,omitempty"`
	Fraction float64 `json:"fraction"`
	Done     int64   `json:"bytesDone,omitempty"`
	Total    int64   `json:"bytesTotal,omitempty"`
	Error    string  `json:"error,omitempty"`
}

type installState struct {
	cancel   context.CancelFunc
	progress install.Progress
	err      string
	running  bool
}

type packageManager struct {
	a  *app.App
	in *install.Installer
	mu sync.Mutex
	st map[string]*installState
}

func newPackageManager(a *app.App) *packageManager {
	return &packageManager{a: a, in: &install.Installer{UserAgent: "Filee/" + app.Version}, st: map[string]*installState{}}
}

func (m *packageManager) list() []packageInfo {
	comps := install.Components()
	var out []packageInfo
	for _, p := range install.Packages() {
		info := packageInfo{
			ID: p.ID, Installed: m.in.IsInstalled(p), CanDownload: install.CanDownload(p, comps),
			DownloadSize: install.DownloadSize(p, comps), InstalledSize: p.InstalledSize, Converters: p.ConverterIDs,
			Reason: install.UnavailableReason(p, comps, goos), Version: install.Version(p.ID),
		}
		if info.Installed {
			info.UpToDate = m.in.IsUpToDate(p)
		}
		m.mu.Lock()
		if s := m.st[p.ID]; s != nil {
			switch {
			case s.running && s.progress.Stage == install.Unpacking:
				info.State = "unpacking"
			case s.running:
				info.State = "downloading"
			case s.err != "":
				info.State, info.Error = "failed", s.err
			}
			info.Fraction, info.Done, info.Total = s.progress.Fraction, s.progress.BytesDone, s.progress.BytesTotal
		}
		m.mu.Unlock()
		out = append(out, info)
	}
	return out
}

func (m *packageManager) start(id string) error {
	p := install.PackageByID(id)
	if p == nil {
		return errors.New("no such engine package")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.st[id]; s != nil && s.running {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &installState{cancel: cancel, running: true}
	m.st[id] = s
	go func() {
		err := m.in.Install(ctx, *p, func(pr install.Progress) {
			m.mu.Lock()
			s.progress = pr
			m.mu.Unlock()
		})
		m.mu.Lock()
		s.running = false
		if err != nil && !errors.Is(err, context.Canceled) {
			s.err = err.Error()
		} else if err == nil {
			delete(m.st, id)
		} else {
			delete(m.st, id)
		}
		m.mu.Unlock()
		tools.Forget()
		m.a.RefreshEngines()
	}()
	return nil
}

func (m *packageManager) cancel(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.st[id]; s != nil && s.cancel != nil {
		s.cancel()
	}
}

func (m *packageManager) remove(id string) error {
	p := install.PackageByID(id)
	if p == nil {
		return errors.New("no such engine package")
	}
	err := m.in.Uninstall(*p)
	tools.Forget()
	m.a.RefreshEngines()
	return err
}

func (s *server) packageRoutes() {
	m := s.mux
	m.HandleFunc("GET /api/packages", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.pkgs.list()) })
	m.HandleFunc("POST /api/packages/{id}/install", func(w http.ResponseWriter, r *http.Request) {
		writeResult(w, nil, s.pkgs.start(r.PathValue("id")))
	})
	m.HandleFunc("POST /api/packages/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		s.pkgs.cancel(r.PathValue("id"))
		writeJSON(w, map[string]bool{"ok": true})
	})
	m.HandleFunc("POST /api/packages/{id}/remove", func(w http.ResponseWriter, r *http.Request) {
		writeResult(w, nil, s.pkgs.remove(r.PathValue("id")))
	})
}
