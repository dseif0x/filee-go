package web

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/jobs"
	"github.com/dseif0x/filee-go/internal/presets"
)

var png = append([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, make([]byte, 100)...)

func refusedKey(err error) string {
	var r *convert.RefusedError
	if errors.As(err, &r) {
		return r.Key
	}
	return ""
}

func server(t *testing.T, h http.HandlerFunc) *httptest.Server {
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s
}

func mustURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

func TestDownloadNamesTheFileAsTheServerSaysAndByWhatItIs(t *testing.T) {
	dir := t.TempDir()
	s := server(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			w.Header().Set("Content-Disposition", `attachment; filename*=UTF-8''Q3%20r%C3%A9sum%C3%A9.jpg`)
			_, _ = w.Write(png) // a PNG served as .jpg
		case "/files/photo.png":
			_, _ = w.Write(png)
		}
	})
	d := NewDownloader(nil, "Filee/test")
	p, err := d.Download(context.Background(), mustURL(s.URL+"/a"), dir, "", nil)
	if err != nil || filepath.Base(p) != "Q3 résumé.png" {
		t.Fatal(p, err)
	}
	p1, _ := d.Download(context.Background(), mustURL(s.URL+"/files/photo.png"), dir, "", nil)
	p2, _ := d.Download(context.Background(), mustURL(s.URL+"/files/photo.png"), dir, "", nil)
	if filepath.Base(p1) != "photo.png" || filepath.Base(p2) != "photo (2).png" {
		t.Fatal(p1, p2)
	}
}

func TestRedirectsAreFollowedAFewTimesAndNeverOffTheWeb(t *testing.T) {
	dir := t.TempDir()
	var s *httptest.Server
	s = server(t, func(w http.ResponseWriter, r *http.Request) {
		var n int
		if _, err := fmt.Sscanf(r.URL.Path, "/r%d", &n); err == nil && n > 0 {
			http.Redirect(w, r, fmt.Sprintf("/r%d", n-1), http.StatusFound)
			return
		}
		if r.URL.Path == "/ftp" {
			w.Header().Set("Location", "ftp://example.com/a.pdf")
			w.WriteHeader(http.StatusMovedPermanently)
			return
		}
		_, _ = w.Write(png)
	})
	d := NewDownloader(nil, "")
	if p, err := d.Download(context.Background(), mustURL(s.URL+"/r3"), dir, "x.png", nil); err != nil || filepath.Base(p) != "x.png" {
		t.Fatal(p, err)
	}
	if _, err := d.Download(context.Background(), mustURL(s.URL+"/r9"), dir, "", nil); refusedKey(err) != "error.web_download_failed" {
		t.Fatal(err)
	}
	if _, err := d.Download(context.Background(), mustURL(s.URL+"/ftp"), dir, "", nil); refusedKey(err) != "error.web_download_failed" {
		t.Fatal(err)
	}
}

func TestPagesProgramsMissingFilesAndOversizeAreRefused(t *testing.T) {
	dir := t.TempDir()
	s := server(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>"))
		case "/sneaky.pdf":
			_, _ = w.Write([]byte("  <!DOCTYPE html><html>sign in"))
		case "/prog":
			_, _ = w.Write([]byte("MZ\x90\x00 a program"))
		case "/big":
			_, _ = w.Write(bytes.Repeat([]byte("x"), 5000))
		default:
			http.NotFound(w, r)
		}
	})
	d := NewDownloader(nil, "")
	d.MaxBytes = 1000
	for path, key := range map[string]string{"/page": "error.web_page", "/sneaky.pdf": "error.web_page", "/prog": "error.web_blocked",
		"/big": "error.web_too_large", "/missing.pdf": "error.web_download_failed"} {
		if _, err := d.Download(context.Background(), mustURL(s.URL+path), dir, "", nil); refusedKey(err) != key {
			t.Errorf("%s: %v", path, err)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatal("left a partial file", entries)
	}
	if _, err := d.Download(context.Background(), mustURL("file:///etc/passwd"), dir, "", nil); refusedKey(err) != "error.web_download_failed" {
		t.Fatal(err)
	}
}

func TestAServerThatStallsTimesOutAndCancellingRemovesThePartialFile(t *testing.T) {
	dir := t.TempDir()
	release := make(chan struct{})
	s := server(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(png)
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	d := NewDownloader(nil, "")
	d.StallTimeout = 200 * time.Millisecond
	if _, err := d.Download(context.Background(), mustURL(s.URL+"/slow.png"), dir, "", nil); refusedKey(err) != "error.web_timeout" {
		t.Fatal(err)
	}
	d.StallTimeout = time.Minute
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	if _, err := d.Download(ctx, mustURL(s.URL+"/slow.png"), dir, "", nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatal("left a partial file")
	}
}

func TestProgressReportsTheBytesAndTheSize(t *testing.T) {
	dir := t.TempDir()
	d := NewDownloader(nil, "")
	var last jobs.TransferProgress
	n := 0
	data := append(png, bytes.Repeat([]byte("y"), 200_000)...)
	_, err := d.Save(context.Background(), bytes.NewReader(data), dir, "a.png", "", int64(len(data)), func(p jobs.TransferProgress) { last = p; n++ })
	if err != nil || last.Bytes != int64(len(data)) || last.Total != int64(len(data)) || n < 1 {
		t.Fatal(err, last, n)
	}
}

func TestNames(t *testing.T) {
	for in, want := range map[string]string{
		"../../x.png": "x.png", `C:\x.png`: "x.png", "a/b.png": "b.png", "CON.txt": "_CON.txt", "  ": "download",
		"what?.pdf": "what_.pdf", strings.Repeat("a", 200) + ".pdf": strings.Repeat("a", 116) + ".pdf",
	} {
		if got := SanitizeName(in, ""); got != want {
			t.Errorf("%q → %q", in, got)
		}
	}
	if WithExtension("photo", "jpg") != "photo.jpg" || WithExtension("photo.jpeg", "jpg") != "photo.jpeg" || WithExtension("photo.png", "webp") != "photo.webp" {
		t.Error("WithExtension")
	}
	if !IsBlocked("setup.EXE") || IsBlocked("a.pdf") || !IsPageName("index.html") {
		t.Error("blocked / page")
	}
	if FromURL(mustURL("https://example.com/files/Q3%20report.pdf?download=1")) != "Q3 report.pdf" || FromURL(mustURL("https://example.com/")) != "" {
		t.Error("FromURL")
	}
	if !IsBrowser("chrome.exe") || !IsBrowser("Google Chrome") || IsBrowser("explorer") {
		t.Error("IsBrowser")
	}
}

func TestSniffing(t *testing.T) {
	webp := []byte("RIFF\x00\x00\x00\x00WEBPVP8 ")
	avif := []byte("\x00\x00\x00\x1cftypmif1\x00\x00\x00\x00mif1avif")
	heic := []byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic")
	cases := map[string]string{"png": string(png), "webp": string(webp), "avif": string(avif), "heic": string(heic), "pdf": "%PDF-1.7", "gif": "GIF89a.."}
	for want, head := range cases {
		if got := Sniff([]byte(head)); got != want {
			t.Errorf("%s: %s", want, got)
		}
	}
	if NameFor("a.jpg", webp, "") != "a.webp" || NameFor("a.apng", png, "") != "a.apng" || NameFor("a.docx", []byte("PK\x03\x04"), "") != "a.docx" ||
		NameFor("download", []byte("PK"), "application/pdf") != "download.pdf" {
		t.Error("NameFor")
	}
}

type closer struct{ closed bool }

func (c *closer) Close() error { c.closed = true; return nil }

func TestDragFiles(t *testing.T) {
	shortcut := &closer{}
	c := &DragContent{Files: []DropItem{
		FromStream("photo.jpg", func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(png)), nil }, -1, nil),
		FromStream("photo.url", nil, -1, shortcut),
	}}
	got := DragFiles(c)
	if len(got) != 1 || got[0].Name() != "photo.jpg" || !shortcut.closed {
		t.Fatal(got)
	}
	link := func(u string) []DropItem {
		return DragFiles(&DragContent{Texts: map[string]string{UniformResourceLocatorW: u}})
	}
	for u, name := range map[string]string{"https://example.com/files/Q3%20report.pdf?download=1": "Q3 report.pdf", "http://example.com/sheet.xlsx": "sheet.xlsx"} {
		if items := link(u); len(items) != 1 || items[0].Name() != name {
			t.Errorf("%s: %v", u, items)
		}
	}
	for _, u := range []string{"https://example.com/news/article", "https://example.com/", "https://example.com/index.html",
		"https://example.com/view.php?id=3", "https://example.com/setup.exe", "ftp://example.com/a.pdf", "file:///C:/a.pdf", "javascript:alert(1)"} {
		if items := link(u); len(items) != 0 {
			t.Errorf("%s was taken", u)
		}
	}
	// A dragged picture is its address, even inside a link and without an extension; relative to the page.
	pic := DragFiles(&DragContent{Texts: map[string]string{
		HTMLFormat: `<a href="/x"><img src="/img/cat?w=200" alt="a cat"></a>`, MozURL: "https://example.com/page\nTitle"}})
	if len(pic) != 1 || pic[0].Name() != "cat.jpg" || pic[0].(*addressItem).URL().String() != "https://example.com/img/cat?w=200" {
		t.Fatal(pic)
	}
	// A text selection is not a file.
	if items := DragFiles(&DragContent{Texts: map[string]string{HTMLFormat: `Look <img src="https://e.com/a.png"> here`}}); len(items) != 0 {
		t.Fatal(items)
	}
	// An inline data picture is decoded.
	data := DragFiles(&DragContent{Texts: map[string]string{HTMLFormat: `<img src="data:image/png;base64,iVBORw0KGgo=">`}})
	if len(data) != 1 || data[0].Name() != "image.png" {
		t.Fatal(data)
	}
	// A download a page offers is named as the page says.
	dl := DragFiles(&DragContent{Texts: map[string]string{DownloadURLFormat: "application/pdf:Invoice 12.pdf:https://mail.example.com/att?id=1"}})
	if len(dl) != 1 || dl[0].Name() != "Invoice 12.pdf" {
		t.Fatal(dl)
	}
	// Several links once each.
	many := DragFiles(&DragContent{Texts: map[string]string{URIList: "# comment\nhttps://e.com/a.pdf\r\nhttps://e.com/b.docx\nhttps://e.com/a.pdf"}})
	if len(many) != 2 {
		t.Fatal(many)
	}
	// Pixels stand in only when the picture has no address Filee can fetch.
	px := DragFiles(&DragContent{Texts: map[string]string{MozURL: "blob:https://e.com/1234\n"}, Picture: func() DropItem { return FromBytes("image.png", png, "image/png") }})
	if len(px) != 1 || px[0].Name() != "image.png" {
		t.Fatal(px)
	}
}

func TestDragFormatParsing(t *testing.T) {
	entries := []FileDescriptor{{Name: "photos\\a.jpg", Size: 1234, IsFolder: false}, {Name: "folder", Size: -1, IsFolder: true}}
	got := ParseFileGroupDescriptor(BuildFileGroupDescriptor(entries))
	if len(got) != 2 || got[0] != entries[0] || got[1] != entries[1] {
		t.Fatal(got)
	}
	if ParseFileGroupDescriptor([]byte{1, 0, 0, 0, 1}) != nil {
		t.Fatal("cut short")
	}
	utf16 := []byte{'h', 0, 't', 0, 't', 0, 'p', 0, 0, 0, 0, 0}
	if DecodeText(utf16) != "http" || DecodeText([]byte("abcd\x00\x00")) != "abcd" || DecodeText([]byte{0xFF, 0xFE, 'x', 0}) != "x" {
		t.Fatal("DecodeText")
	}
	if u := ParseMozURL("https://a\nTitle A\nhttps://b\nTitle B"); len(u) != 2 || u[1] != "https://b" {
		t.Fatal(u)
	}
	if d := ParseDownloadURL("image/png:photo.png:https://e.com/p.png"); d == nil || d.Name != "photo.png" || d.URL.Host != "e.com" {
		t.Fatal(d)
	}
	if ParseDownloadURL("nonsense") != nil {
		t.Fatal("parsed nonsense")
	}
	if ParseInternetShortcut("[InternetShortcut]\r\nURL=https://e.com/a.pdf\r\n") != "https://e.com/a.pdf" {
		t.Fatal("shortcut")
	}
	win := "Version:0.9\r\nStartHTML:0\r\n<html><body><!--StartFragment--><img src='https://e.com/a.png'><!--EndFragment--></body></html>"
	if ImageOnly(win) != "https://e.com/a.png" {
		t.Fatal(ImageOnly(win))
	}
	if m, b, ok := DecodeDataURL("data:text/plain,hello%20world", 100); !ok || m != "text/plain" || string(b) != "hello world" {
		t.Fatal(m, b, ok)
	}
	if _, _, ok := DecodeDataURL("data:image/png;base64,"+strings.Repeat("A", 1000), 10); ok {
		t.Fatal("over the limit")
	}
}

type pngFake struct{}

func (pngFake) ID() string             { return "magick" }
func (pngFake) Name() string           { return "magick" }
func (pngFake) Edges() []convert.Edge  { return []convert.Edge{convert.E("png", "webp")} }
func (pngFake) MaxParallelism() int    { return 0 }
func (pngFake) Status() convert.Status { return convert.Available("", "") }
func (pngFake) Convert(_ context.Context, st *convert.Step, _ convert.Progress) ([]string, error) {
	p := st.Output.Allocate(st.To, "")
	return []string{p}, os.WriteFile(p, []byte("webp"), 0o644)
}

func TestADropIsSavedWhenItsJobRunsAndCleanedUpAfter(t *testing.T) {
	for _, onlyConverted := range []bool{false, true} {
		root, downloads := t.TempDir(), t.TempDir()
		drop := NewDrop([]DropItem{FromBytes("pic.png", png, "image/png"), FromBytes("bad.png", []byte("MZ program"), "")}, root)
		if !IsPlaceholder(filepath.Join(DefaultDropRoot(), "abc", "drop", "1", "x.png")) || len(drop.Placeholders()) != 2 {
			t.Fatal("placeholders")
		}
		fetcher := drop.Fetcher(DropOptions{Folder: downloads, SaveOnlyConverted: onlyConverted}, NewDownloader(nil, ""))
		q := jobs.NewQueue(convert.NewCatalog(pngFake{}), jobs.Options{WorkRoot: t.TempDir()})
		p := fetcher.Apply(presets.New("", "webp"))
		j := jobs.NewJob(drop.Placeholders(), p, "WEBP", fetcher)
		q.Run(j)
		q.Close()
		fs := j.Files()
		if fs[0].State != jobs.FileDone || fs[1].ErrorKey != "error.web_blocked" {
			t.Fatalf("%+v", fs)
		}
		if out := j.Outputs(); len(out) != 1 || out[0] != filepath.Join(downloads, "pic.webp") {
			t.Fatal(out)
		}
		_, err := os.Stat(filepath.Join(downloads, "pic.png"))
		if (err == nil) == onlyConverted {
			t.Fatalf("original kept: %v (only converted %v)", err == nil, onlyConverted)
		}
		if _, err := os.Stat(drop.WorkFolder()); err == nil {
			t.Fatal("work folder left")
		}
	}
}
