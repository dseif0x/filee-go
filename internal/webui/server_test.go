package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"

	"github.com/dseif0x/filee-go/internal/app"
	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/presets"
	"github.com/dseif0x/filee-go/internal/settings"
)

type harness struct {
	t    *testing.T
	a    *app.App
	s    *server
	ts   *httptest.Server
	base string
	dir  string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	a, err := app.New(app.Options{DataDir: filepath.Join(dir, "data"), WorkRoot: filepath.Join(dir, "work"), Catalog: convert.NewCatalog()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	h := &harness{t: t, a: a, dir: dir}
	h.ts = httptest.NewUnstartedServer(nil)
	h.ts.Start()
	port := h.ts.Listener.Addr().(*net.TCPAddr).Port
	h.s = newServer(a, port)
	h.ts.Config.Handler = h.s
	t.Cleanup(h.ts.Close)
	t.Cleanup(h.s.conv.close)
	h.base = "http://127.0.0.1:" + h.s.port + h.s.prefix
	return h
}

func (h *harness) do(method, path string, body any, out any) int {
	h.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, h.base+path, r)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		b, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(b, out); err != nil && resp.StatusCode < 300 {
			h.t.Fatalf("%s %s: %v (%s)", method, path, err, b)
		}
	}
	return resp.StatusCode
}

func TestSecretHostAndOrigin(t *testing.T) {
	h := newHarness(t)
	get := func(url string, header map[string]string) int {
		req, _ := http.NewRequest("GET", url, nil)
		for k, v := range header {
			if k == "Host" {
				req.Host = v
			} else {
				req.Header.Set(k, v)
			}
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	root := "http://127.0.0.1:" + h.s.port
	if c := get(h.base+"/api/settings", nil); c != 200 {
		t.Fatalf("with the secret: %d", c)
	}
	if c := get(root+"/api/settings", nil); c != 404 {
		t.Fatalf("without the secret: %d", c)
	}
	if c := get(root+"/s/wrong/api/settings", nil); c != 404 {
		t.Fatalf("wrong secret: %d", c)
	}
	if c := get(h.base+"/api/settings", map[string]string{"Host": "evil.example:" + h.s.port}); c != 403 {
		t.Fatalf("rebinding host: %d", c)
	}
	if c := get(h.base+"/api/settings", map[string]string{"Origin": "https://evil.example"}); c != 403 {
		t.Fatalf("foreign origin: %d", c)
	}
	if c := get(h.base+"/api/settings", map[string]string{"Origin": "http://localhost:" + h.s.port}); c != 200 {
		t.Fatalf("own origin: %d", c)
	}
	// The page and its scripts are served under the secret.
	resp, err := http.Get(h.base + "/")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(b), "js/app.js") {
		t.Fatalf("index: %s", b)
	}
	if c := get(h.base+"/js/tools/clip.js", nil); c != 200 {
		t.Fatalf("script: %d", c)
	}
	if c := get(h.base, nil); c != 200 { // redirected to the folder
		t.Fatalf("base: %d", c)
	}
}

func TestStartServes(t *testing.T) {
	a, err := app.New(app.Options{DataDir: t.TempDir(), Catalog: convert.NewCatalog()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ctx, cancel := context.WithCancel(context.Background())
	base, err := Start(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(base, "http://127.0.0.1:") || !strings.Contains(base, "/s/") {
		t.Fatalf("base %q", base)
	}
	resp, err := http.Get(base + "/api/bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	var boot map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&boot)
	resp.Body.Close()
	if boot["version"] != app.Version || boot["texts"].(map[string]any)["nav.home"] != "Home" {
		t.Fatalf("bootstrap %v", boot["version"])
	}
	cancel()
	time.Sleep(100 * time.Millisecond)
	if _, err := http.Get(base + "/api/bootstrap"); err == nil {
		t.Fatal("still serving after the context ended")
	}
}

func TestSettingsLibraryAndProfiles(t *testing.T) {
	h := newHarness(t)
	var s settings.AppSettings
	h.do("GET", "/api/settings", nil, &s)
	s.KeepFileDates = true
	s.Theme.Accent = "#123456"
	var saved settings.AppSettings
	if c := h.do("PUT", "/api/settings", s, &saved); c != 200 || !saved.KeepFileDates || h.a.Settings().Theme.Accent != "#123456" {
		t.Fatalf("settings not saved: %d %+v", c, saved.Theme)
	}

	var lib libraryResponse
	h.do("GET", "/api/library", nil, &lib)
	if len(lib.Presets) == 0 || len(lib.Profiles) == 0 || len(lib.Builtin) == 0 {
		t.Fatal("empty library")
	}
	var p presets.Preset
	h.do("POST", "/api/presets?new=1", map[string]any{"name": "Mine", "targetFormat": "webp", "kind": "Convert"}, &p)
	if p.ID == "" || p.Name != "Mine" || p.Image.Quality != 90 {
		t.Fatalf("new preset %+v", p)
	}
	p.Image.Quality = 42
	h.do("PUT", "/api/presets/"+p.ID, p, nil)
	if h.a.Preset(p.ID).Image.Quality != 42 {
		t.Fatal("preset not saved")
	}
	prof := lib.Profiles[0]
	prof.PresetIDs = append(prof.PresetIDs[:0:0], p.ID)
	h.do("PUT", "/api/profiles/"+prof.ID, prof, nil)
	if c := h.do("DELETE", "/api/presets/"+p.ID, nil, nil); c != 200 {
		t.Fatalf("delete %d", c)
	}
	for _, x := range h.a.Profiles() {
		for _, id := range x.PresetIDs {
			if id == p.ID {
				t.Fatal("deleted preset still on a donut")
			}
		}
	}
	var usable map[string]bool
	h.do("GET", "/api/profiles/"+prof.ID+"/usable", nil, &usable)
	if len(usable) == 0 {
		t.Fatal("no usability")
	}
	var moved map[string]bool
	h.do("POST", "/api/profiles/"+lib.Profiles[1].ID+"/move", map[string]int{"direction": -1}, &moved)
	if !moved["moved"] || h.a.Profiles()[0].ID != lib.Profiles[1].ID {
		t.Fatal("profile not moved")
	}
	// Export and import.
	resp, err := http.Get(h.base + "/api/library/export")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("export: %v", err)
	}
	bundle, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	req, _ := http.NewRequest("POST", h.base+"/api/library/import", bytes.NewReader(bundle))
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("import: %v %s", err, b)
	}
	resp.Body.Close()

	var check struct {
		Name   string   `json:"name"`
		Errors []string `json:"errors"`
	}
	h.do("POST", "/api/rename/check", map[string]any{"sample": "IMG_0412.jpg", "pattern": "{name}", "steps": []presets.RenameStep{{Kind: presets.RenameReplace, Find: "IMG_", Replace: "Photo_"}, {Kind: presets.RenameRegex, Find: "("}}}, &check)
	if check.Name != "Photo_0412" || check.Errors[1] == "" {
		t.Fatalf("rename check %+v", check)
	}
	var pat map[string]string
	h.do("POST", "/api/watch/pattern", map[string]string{"typed": "heic"}, &pat)
	if pat["pattern"] != "*.heic" {
		t.Fatalf("pattern %v", pat)
	}
}

func writePNG(t *testing.T, path string, w, h int) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = png.Encode(f, img)
	f.Close()
}

func TestCropTool(t *testing.T) {
	h := newHarness(t)
	src := filepath.Join(h.dir, "pic.png")
	writePNG(t, src, 200, 100)
	other := filepath.Join(h.dir, "notes.txt")
	_ = os.WriteFile(other, []byte("x"), 0o644)
	var opened map[string]string
	token := "0123456789abcdef"
	if c := h.do("POST", "/api/tools", map[string]any{"id": token, "tool": "crop", "presetId": "tool-crop", "paths": []string{src, other}}, &opened); c != 200 || opened["id"] != token {
		t.Fatalf("open %d %v", c, opened)
	}
	// Asking again with the token is the same window.
	h.do("POST", "/api/tools", map[string]any{"id": token, "tool": "crop", "paths": []string{src}}, &opened)
	var job ToolJob
	h.do("GET", "/api/tools/"+token, nil, &job)
	if len(job.Files) != 1 || job.Files[0].State != "current" || job.Notice == "" {
		t.Fatalf("job %+v %q", job.Files, job.Notice)
	}
	resp, err := http.Get(h.base + "/api/tools/" + token + "/files/0/preview?max=50")
	if err != nil || resp.StatusCode != 200 || resp.Header.Get("X-Width") != "200" {
		t.Fatalf("preview %v %v", err, resp.Header)
	}
	resp.Body.Close()
	h.do("POST", "/api/tools/"+token+"/files/0/crop", cropRequest{X: 0.25, Y: 0, Width: 0.5, Height: 1}, &job)
	if job.Files[0].State != "done" || len(job.Files[0].Outputs) != 1 {
		t.Fatalf("crop %+v", job.Files[0])
	}
	f, _ := os.Open(job.Files[0].Outputs[0])
	cfg, _, err := image.DecodeConfig(f)
	f.Close()
	if err != nil || cfg.Width != 100 || cfg.Height != 100 {
		t.Fatalf("cropped %v %dx%d", err, cfg.Width, cfg.Height)
	}
	if hist := h.a.History(); len(hist) != 1 || !hist[0].CanUndo() {
		t.Fatalf("history %+v", hist)
	}
	if c := h.do("GET", "/api/tools/nope", nil, nil); c != 404 {
		t.Fatalf("unknown tool %d", c)
	}
}

func TestPdfPagesAndCrop(t *testing.T) {
	h := newHarness(t)
	var imgs []string
	for i, size := range [][2]int{{300, 200}, {200, 300}, {100, 100}} {
		p := filepath.Join(h.dir, "p"+string(rune('a'+i))+".png")
		writePNG(t, p, size[0], size[1])
		imgs = append(imgs, p)
	}
	pdfPath := filepath.Join(h.dir, "doc.pdf")
	if err := api.ImportImagesFile(context.Background(), imgs, pdfPath, nil, nil); err != nil {
		t.Fatal(err)
	}
	var opened map[string]string
	h.do("POST", "/api/tools", map[string]any{"tool": "pdf-pages", "paths": []string{pdfPath}}, &opened)
	id := opened["id"]
	var info map[string]any
	h.do("GET", "/api/tools/"+id+"/files/0/info", nil, &info)
	if info["pages"].(float64) != 3 {
		t.Fatalf("info %v", info)
	}
	var saved map[string]any
	h.do("POST", "/api/tools/"+id+"/pdf/save", map[string]any{"pages": []PdfPageRef{{File: 0, Page: 2}, {File: 0, Page: 0, Rotation: 90}}}, &saved)
	out, _ := saved["path"].(string)
	ctx, err := api.ReadContextFile(context.Background(), out)
	if err != nil || ctx.PageCount != 2 {
		t.Fatalf("saved %v %v", err, saved)
	}
	_, _, inh, _ := ctx.PageDict(context.Background(), 2, false)
	if inh.Rotate != 90 || inh.MediaBox.Width() != 300 {
		t.Fatalf("page 2: rotate %d box %v", inh.Rotate, inh.MediaBox)
	}
	_, _, inh, _ = ctx.PageDict(context.Background(), 1, false)
	if inh.MediaBox.Width() != 100 {
		t.Fatalf("page 1 box %v", inh.MediaBox)
	}

	cropped := filepath.Join(h.dir, "cropped.pdf")
	if err := cropPdf(context.Background(), pdfPath, cropped, presets.HiddenArea{X: 0, Y: 0, Width: 0.5, Height: 0.5}); err != nil {
		t.Fatal(err)
	}
	ctx, _ = api.ReadContextFile(context.Background(), cropped)
	_, _, inh, _ = ctx.PageDict(context.Background(), 1, false)
	if b := inh.CropBox; b == nil || b.LL.X != 0 || b.UR.X != 150 || b.LL.Y != 100 || b.UR.Y != 200 {
		t.Fatalf("crop box %v", inh.CropBox)
	}
}

func TestCropRectRotations(t *testing.T) {
	a := presets.HiddenArea{X: 0.1, Y: 0.2, Width: 0.3, Height: 0.4}
	cases := map[int][4]float64{
		0:   {10, 40, 40, 80},
		90:  {20, 10, 60, 40},
		180: {60, 20, 90, 60},
		270: {40, 60, 80, 90},
	}
	for rot, want := range cases {
		llx, lly, urx, ury := cropRect(0, 0, 100, 100, rot, a)
		if [4]float64{llx, lly, urx, ury} != want {
			t.Errorf("rotate %d: %v %v %v %v, want %v", rot, llx, lly, urx, ury, want)
		}
	}
}

func TestClipOperations(t *testing.T) {
	h := newHarness(t)
	src := filepath.Join(h.dir, "movie.mp4")
	_ = os.WriteFile(src, []byte("not really a video"), 0o644)
	var opened map[string]string
	h.do("POST", "/api/tools", map[string]any{"tool": "clip", "paths": []string{src}}, &opened)
	base := "/api/tools/" + opened["id"] + "/files/0/clip"
	var st clipState
	h.do("POST", base, map[string]any{"op": "init", "duration": 10}, &st)
	if st.Duration != 10 || st.End != 10 || len(st.Pieces) != 1 {
		t.Fatalf("init %+v", st)
	}
	h.do("POST", base, map[string]any{"op": "split", "at": 4}, &st)
	h.do("POST", base, map[string]any{"op": "remove", "index": 0}, &st)
	if len(st.Pieces) != 2 || st.Pieces[0].State != "blank" || !st.HasGaps || st.Plain {
		t.Fatalf("after remove %+v", st)
	}
	h.do("POST", base, map[string]any{"op": "close-gaps"}, &st)
	if st.HasGaps || st.Length != 6 {
		t.Fatalf("after close %+v", st)
	}
	h.do("POST", base, map[string]any{"op": "undo"}, &st)
	if !st.HasGaps {
		t.Fatal("undo")
	}
	// A drag joined into one undo step.
	h.do("POST", base, map[string]any{"op": "move-end", "at": 9, "join": "d1"}, &st)
	h.do("POST", base, map[string]any{"op": "move-end", "at": 8, "join": "d1"}, &st)
	if st.End != 8 {
		t.Fatalf("end %v", st.End)
	}
	h.do("POST", base, map[string]any{"op": "undo"}, &st)
	if st.End != 10 {
		t.Fatalf("drag not one step: end %v", st.End)
	}
	var job ToolJob
	h.do("POST", base+"/save", map[string]any{"fast": true}, &job)
	if job.Files[0].JobID == "" {
		t.Fatalf("save %+v", job.Files[0])
	}
	// Served with ranges for the <video>.
	req, _ := http.NewRequest("GET", h.base+base+"/media", nil)
	req.Header.Set("Range", "bytes=0-3")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("range %v %v", err, resp.StatusCode)
	}
	resp.Body.Close()
}

func TestSheetInspectAndPrintCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "book.xlsx")
	writeSamples(t, dir) // budget.xlsx: Sheet1 A1:H40, Wide A1:BR1
	path = filepath.Join(dir, "budget.xlsx")
	sheets, err := InspectWorkbook(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(sheets) != 2 || sheets[0].UsedRange != "A1:H40" || sheets[1].UsedRange != "A1:BR1" || !sheets[1].WideUnbounded {
		t.Fatalf("sheets %+v", sheets)
	}
	r := RegionFromSource(sheets[1])
	r.Range, r.WritePrintArea = "A1:J1", true
	r.ScaleMode, r.FitWidth, r.FitHeight = "fit", 1, 0
	r.Orientation, r.Paper = "landscape", "a4"
	out := filepath.Join(dir, "print.xlsx")
	if err := writePrintCopy(path, out, sheets, r); err != nil {
		t.Fatal(err)
	}
	again, err := InspectWorkbook(out)
	if err != nil {
		t.Fatal(err)
	}
	w := again[1]
	if w.PrintArea != "A1:J1" || w.PageSetup.Orientation != "landscape" || w.PageSetup.Paper != "a4" || w.PageSetup.FitWidth != 1 {
		t.Fatalf("print copy %+v", w)
	}
	if !again[0].Hidden || again[1].Hidden {
		t.Fatalf("visibility %v %v", again[0].Hidden, again[1].Hidden)
	}
	for _, c := range []struct {
		r  string
		ok bool
	}{{"A1:S52", true}, {"A:C", true}, {"1:3", true}, {"A1:B2,D4", true}, {"", false}, {"A1:", false}, {"XFE1", false}} {
		if ValidSheetRange(c.r) != c.ok {
			t.Errorf("ValidSheetRange(%q) != %v", c.r, c.ok)
		}
	}
}

func TestUpdateCheck(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"tag_name":"v9.1.0","body":"new","html_url":"https://x/9.1","assets":[]},
			{"tag_name":"v9.2.0-beta","prerelease":true},{"tag_name":"v0.0.1"}]`))
	}))
	defer gh.Close()
	old := releasesAPI
	releasesAPI = gh.URL
	defer func() { releasesAPI = old }()
	info := CheckForUpdate(context.Background(), gh.Client())
	if !info.Newer || info.Latest != "9.1.0" || len(info.Notes) != 1 || info.URL != "https://x/9.1" {
		t.Fatalf("%+v", info)
	}
	if !isNewer("v1.10", "1.9.3") || isNewer("1.2", "1.2.0") || isNewer("junk", "1.0") {
		t.Fatal("isNewer")
	}
}

func TestToolFor(t *testing.T) {
	for id, want := range map[string]string{"tool-crop": ToolCrop, "tool-clip": ToolClip, "tool-hide": ToolHide, "tool-image-edit": ToolImageEdit,
		"tool-pdf-pages": ToolPdfPages, "tool-pdf-add": ToolPdfPages, "tool-sheet-studio": ToolPrint, "tool-pdf-crop": ToolCrop} {
		p := presets.FindBuiltIn(id)
		if p == nil {
			t.Errorf("no built-in %s", id)
			continue
		}
		if got := ToolFor(p); got != want {
			t.Errorf("ToolFor(%s) = %q, want %q", id, got, want)
		}
	}
}
