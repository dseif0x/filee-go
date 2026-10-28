// Package webui is Filee's settings and tool windows as a local web app: a
// JSON API over internal/app, Server-Sent Events for job progress and an
// embedded frontend (plain ES modules with a vendored preact+htm, no build
// step).
//
// The server listens on 127.0.0.1 with a random port. Every request must
// carry the run's secret in its path (baseURL is http://127.0.0.1:<port>/s/<secret>),
// so other local processes and web pages can't drive it; requests whose Host
// or Origin isn't this server are refused, which blocks DNS rebinding.
//
// Routes the GUIs open:
//
//	baseURL + "/#/<page>"                     home, toolbar, presets, triggers, watch,
//	                                          engines, history, general, theme, about
//	baseURL + "/#/tool/<tool>?job=<id>"       crop, clip, hide, image-edit, pdf-pages, print
//
// A tool's job id comes from NewToolJob (or POST <baseURL>/api/tools).
package webui

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dseif0x/filee-go/internal/app"
)

//go:embed static
var staticFiles embed.FS

// server is one running web UI.
type server struct {
	a      *app.App
	log    *slog.Logger
	secret string
	prefix string // "/s/<secret>"
	port   string
	mux    *http.ServeMux
	static fs.FS

	conv *converter // private queue for previews (not in the history)

	exportMu sync.Mutex
}

// Start serves the web UI for a until ctx ends and returns its base URL.
func Start(ctx context.Context, a *app.App) (baseURL string, err error) {
	if a == nil {
		return "", errors.New("webui: no app")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	s := newServer(a, ln.Addr().(*net.TCPAddr).Port)
	hs := &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Warn("web UI stopped", "error", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = hs.Shutdown(shutdown)
		s.conv.close()
	}()
	return "http://127.0.0.1:" + s.port + s.prefix, nil
}

func newServer(a *app.App, port int) *server {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	secret := base64.RawURLEncoding.EncodeToString(b)
	sub, _ := fs.Sub(staticFiles, "static")
	s := &server{
		a: a, log: slog.Default(), secret: secret, prefix: "/s/" + secret, port: strconv.Itoa(port),
		mux: http.NewServeMux(), static: sub,
	}
	s.conv = newConverter(a)
	s.routes()
	return s
}

// ServeHTTP checks host, origin and secret before anything else.
func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.allowedHost(r.Host) {
		http.Error(w, "forbidden host", http.StatusForbidden)
		return
	}
	if o := r.Header.Get("Origin"); o != "" && !s.allowedOrigin(o) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}
	// The secret is the first path segment after /s/.
	rest, ok := strings.CutPrefix(r.URL.Path, "/s/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	given, tail, _ := strings.Cut(rest, "/")
	if subtle.ConstantTimeCompare([]byte(given), []byte(s.secret)) != 1 {
		http.NotFound(w, r)
		return
	}
	if tail == "" && !strings.HasSuffix(r.URL.Path, "/") {
		// Relative URLs of the page resolve against the folder.
		http.Redirect(w, r, s.prefix+"/", http.StatusFound)
		return
	}
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; media-src 'self' blob:; style-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'")
	r2 := r.Clone(r.Context())
	r2.URL.Path = "/" + tail
	r2.URL.RawPath = ""
	s.mux.ServeHTTP(w, r2)
}

func (s *server) allowedHost(host string) bool {
	return host == "127.0.0.1:"+s.port || host == "localhost:"+s.port || host == "[::1]:"+s.port
}

func (s *server) allowedOrigin(origin string) bool {
	u, err := url.Parse(origin)
	return err == nil && u.Scheme == "http" && s.allowedHost(u.Host)
}

// static serves the frontend; unknown paths get index.html (the router is in the fragment).
func (s *server) serveStatic(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	if _, err := fs.Stat(s.static, name); err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFileFS(w, r, s.static, name)
}
