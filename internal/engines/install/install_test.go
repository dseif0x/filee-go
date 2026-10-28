package install

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

func tarOf(t *testing.T, files map[string]string, mode int64) []byte {
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	for name, body := range files {
		if err := w.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	w.Close()
	return b.Bytes()
}

func zipOf(t *testing.T, files map[string][]byte) []byte {
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, body := range files {
		f, _ := w.Create(name)
		f.Write(body)
	}
	w.Close()
	return b.Bytes()
}

func sum(b []byte) string { h := sha256.Sum256(b); return strings.ToUpper(hex.EncodeToString(h[:])) }

type served map[string][]byte

func (s served) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/ffmpeg.tar.xz", http.StatusFound)
			return
		}
		b, ok := s[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestInstallUnpacksVerifiesAndMarks(t *testing.T) {
	prog := "ffmpeg"
	probe := "ffprobe"
	if runtime.GOOS == "windows" {
		prog, probe = "ffmpeg.exe", "ffprobe.exe"
	}
	tarball := tarOf(t, map[string]string{"ffmpeg-9/bin/" + prog: "#!/bin/sh\n", "ffmpeg-9/bin/" + probe: "#!/bin/sh\n", "ffmpeg-9/LICENSE": "GPL"}, 0o755)
	var xzb bytes.Buffer
	xw, _ := xz.NewWriter(&xzb)
	xw.Write(tarball)
	xw.Close()
	var gz bytes.Buffer
	gw := gzip.NewWriter(&gz)
	gw.Write(tarOf(t, map[string]string{"pandoc-3/bin/pandoc" + map[bool]string{true: ".exe"}[runtime.GOOS == "windows"]: "x"}, 0o644))
	gw.Close()
	files := served{"/ffmpeg.tar.xz": xzb.Bytes(), "/pandoc.tar.gz": gz.Bytes(), "/eng.traineddata": []byte("eng data")}
	srv := files.server(t)
	comps := map[string]Component{
		"ffmpeg": {ID: "ffmpeg", Version: "9", URL: srv.URL + "/redirect", Sha256: sum(xzb.Bytes()), Size: int64(xzb.Len()), Kind: "tar.xz"},
		"pandoc": {ID: "pandoc", Version: "3", URL: srv.URL + "/pandoc.tar.gz", Sha256: sum(gz.Bytes()), Size: int64(gz.Len()), Kind: "tar.gz"},
		"eng":    {ID: "eng", Version: "4.1.0", URL: srv.URL + "/eng.traineddata", Sha256: sum([]byte("eng data")), Size: 8, Kind: "traineddata"},
	}
	root := t.TempDir()
	in := &Installer{Root: root, Components: comps}
	pkg := Package{ID: "ffmpeg", Components: []string{"ffmpeg"}}
	if in.IsInstalled(pkg) {
		t.Fatal("installed before")
	}
	var last Progress
	if err := in.Install(context.Background(), pkg, func(p Progress) { last = p }); err != nil {
		t.Fatal(err)
	}
	if !in.IsInstalled(pkg) || !in.IsUpToDate(pkg) || last.Fraction != 1 {
		t.Fatal("not installed", last)
	}
	// The single top folder is dropped; the programs are executable.
	st, err := os.Stat(filepath.Join(root, "ffmpeg", "bin", prog))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode()&0o111 == 0 {
		t.Error("not executable")
	}
	// A pinned hash that changed: outdated, and installing again replaces it.
	c := comps["ffmpeg"]
	c.Sha256 = strings.Repeat("0", 64)
	comps["ffmpeg"] = c
	if in.IsUpToDate(pkg) || !in.IsInstalled(pkg) {
		t.Error("update not noticed")
	}
	err = in.Install(context.Background(), pkg, nil)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Error("bad download accepted:", err)
	}
	if _, err := os.Stat(filepath.Join(root, "ffmpeg", "bin", prog)); err != nil {
		t.Error("a failed update removed the installed copy")
	}
	// A tar.gz with its program in bin/.
	bad := Package{ID: "pandoc", Components: []string{"pandoc"}}
	if err := in.Install(context.Background(), bad, nil); err != nil {
		t.Fatal(err)
	}
	if err := in.Install(context.Background(), Package{ID: "ocr-eng", Components: []string{"eng"}}, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "eng", "eng.traineddata")); string(b) != "eng data" {
		t.Error("traineddata")
	}
	if err := in.Uninstall(pkg); err != nil || in.IsInstalled(pkg) {
		t.Error("uninstall", err)
	}
	if err := in.Install(context.Background(), Package{ID: "x", Components: []string{"nothing"}}, nil); err == nil {
		t.Error("unknown component")
	}
}

func TestUnsafeArchivesAreRefused(t *testing.T) {
	dir := t.TempDir()
	z := filepath.Join(dir, "evil.zip")
	os.WriteFile(z, zipOf(t, map[string][]byte{"../evil.txt": []byte("x")}), 0o644)
	if err := UnzipTo(z, filepath.Join(dir, "out")); err == nil {
		t.Error("zip slip accepted")
	}
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	w.WriteHeader(&tar.Header{Name: "link", Linkname: "/etc/passwd", Typeflag: tar.TypeSymlink})
	w.Close()
	var gz bytes.Buffer
	g := gzip.NewWriter(&gz)
	g.Write(b.Bytes())
	g.Close()
	tgz := filepath.Join(dir, "evil.tar.gz")
	os.WriteFile(tgz, gz.Bytes(), 0o644)
	if err := UntarTo(context.Background(), tgz, filepath.Join(dir, "t"), "tar.gz"); err == nil {
		t.Error("absolute link accepted")
	}
}

func TestCondaPackagesGiveTheirWindowsBinaries(t *testing.T) {
	inner := tarOf(t, map[string]string{"Library/bin/gswin64c.exe": "gs", "Library/bin/gsdll64.dll": "dll", "info/index.json": "{}"}, 0o644)
	var zs bytes.Buffer
	e, _ := zstd.NewWriter(&zs)
	e.Write(inner)
	e.Close()
	dir := t.TempDir()
	conda := filepath.Join(dir, "gs.conda")
	os.WriteFile(conda, zipOf(t, map[string][]byte{"metadata.json": []byte("{}"), "pkg-ghostscript.tar.zst": zs.Bytes()}), 0o644)
	out := filepath.Join(dir, "out")
	if err := UnpackConda(context.Background(), conda, out); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "gswin64c.exe")); string(b) != "gs" {
		t.Error("gs missing")
	}
	if _, err := os.Stat(filepath.Join(out, "index.json")); err == nil {
		t.Error("non-binary unpacked")
	}
	os.WriteFile(conda, zipOf(t, map[string][]byte{"x": nil}), 0o644)
	if err := UnpackConda(context.Background(), conda, out); err == nil {
		t.Error("not a conda package accepted")
	}
}

func TestEnginesJsonPerPlatform(t *testing.T) {
	win := ComponentsFor("win-x64", "")
	for _, id := range []string{"ffmpeg", "ghostscript", "tesseract", "libreoffice", "pandoc", "calibre"} {
		if c, ok := win[id]; !ok || len(c.Sha256) != 64 || c.Size <= 0 || !strings.HasPrefix(c.URL, "https://") {
			t.Errorf("win-x64 %s: %+v", id, c)
		}
	}
	mac := ComponentsFor("osx-arm64", "15.1")
	for _, id := range []string{"pandoc", "libreoffice", "calibre", "rhwp", "7zip"} {
		if _, ok := mac[id]; !ok {
			t.Errorf("osx-arm64 has no %s", id)
		}
	}
	for _, id := range []string{"ffmpeg", "ghostscript", "tesseract"} {
		if _, ok := mac[id]; ok {
			t.Errorf("osx-arm64 has %s (Homebrew is the route)", id)
		}
	}
	if _, ok := ComponentsFor("osx-arm64", "13.6")["calibre"]; ok {
		t.Error("calibre 9 offered on macOS 13")
	}
	ff := Package{ID: "ffmpeg", Components: []string{"ffmpeg"}}
	if r := UnavailableReason(ff, mac, "darwin"); !strings.Contains(r, "brew install ffmpeg") {
		t.Error(r)
	}
	if UnavailableReason(ff, win, "windows") != "" || !CanDownload(ff, win) || IsSuggested(ff, win) {
		t.Error("ffmpeg on windows")
	}
	if len(ComponentsFor("plan9-x64", "")) != 0 {
		t.Error("unknown platform")
	}
	if runtimeID("darwin", "arm64") != "osx-arm64" || runtimeID("windows", "amd64") != "win-x64" || runtimeID("linux", "amd64") != "linux-x64" {
		t.Error("runtime ids")
	}
	if compareVersions("14.0", "14") != 0 || compareVersions("13.6.1", "14.0") >= 0 {
		t.Error("versions")
	}
	if ProgramsOn("ghostscript", "windows")[0] != "gswin64c.exe" || ProgramsOn("ffmpeg", "darwin")[1] != "ffprobe" {
		t.Error("programs")
	}
}

func TestTrimming(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"help/a", "share/gallery/b", "share/extensions/dict-en/x", "share/extensions/dict-de/x", "program/soffice.exe", "System64/msvcp140.dll", "x.msi"} {
		os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o755)
		os.WriteFile(filepath.Join(root, p), nil, 0o644)
	}
	TrimLibreOffice(root)
	for p, want := range map[string]bool{"help": false, "share/gallery": false, "share/extensions/dict-en": true, "share/extensions/dict-de": false, "x.msi": false} {
		if _, err := os.Stat(filepath.Join(root, p)); (err == nil) != want {
			t.Error(p)
		}
	}
	if CopyLibreOfficeRuntime(root) != 1 || CopyLibreOfficeRuntime(root) != 0 {
		t.Error("runtime copy")
	}
	ts := t.TempDir()
	for _, p := range []string{"tesseract.exe", "unicharset_extractor.exe", "doc.html", "tessdata/eng.traineddata", "$PLUGINSDIR/x", "libtesseract.dll"} {
		os.MkdirAll(filepath.Dir(filepath.Join(ts, p)), 0o755)
		os.WriteFile(filepath.Join(ts, p), nil, 0o644)
	}
	TrimTesseract(ts)
	left, _ := os.ReadDir(ts)
	if len(left) != 2 {
		t.Error(left)
	}
}

// Every Mac download is the build of its own architecture (or a universal one).
func TestMacDownloadsMatchTheirArchitecture(t *testing.T) {
	for rid, want := range map[string][]string{"osx-arm64": {"aarch64", "arm64"}, "osx-x64": {"x86_64", "x86-64"}} {
		c := ComponentsFor(rid, "")
		for _, id := range []string{"rhwp", "7zip", "pandoc", "libreoffice"} {
			if _, ok := c[id]; !ok {
				t.Errorf("%s has no %s download", rid, id)
			}
		}
		for id, comp := range c {
			if id == "7zip" || id == "calibre" {
				continue // universal builds
			}
			if !strings.Contains(comp.URL, want[0]) && !strings.Contains(comp.URL, want[1]) {
				t.Errorf("%s %s: %s", rid, id, comp.URL)
			}
		}
	}
	if nativeArch("arm64") != "arm64" {
		t.Error("arm64 stays arm64")
	}
}
