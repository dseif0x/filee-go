package install

// Every download is checked against its pinned SHA-256 before anything is
// unpacked; unpacking happens in a staging folder that replaces the engine
// folder only when complete, so a failed or cancelled install never leaves a
// half-installed engine. A marker file with the component's hash records
// what is installed, so an engines.json update is noticed.

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/tools"
)

const markerName = ".filee-component"

// Stage of an install.
type Stage int

const (
	Downloading Stage = iota
	Unpacking
)

// Progress of an install: Fraction covers the whole package (0..1).
type Progress struct {
	Stage       Stage
	ComponentID string
	Fraction    float64
	BytesDone   int64
	BytesTotal  int64
}

// Installer installs and removes packages.
type Installer struct {
	// Root receives one folder per component (tools.EnginesDir when "").
	Root string
	// Components is the download list (Components() when nil).
	Components map[string]Component
	HTTP       *http.Client
	Log        *slog.Logger
	// UserAgent is sent with every download.
	UserAgent string
}

func (in *Installer) root() string {
	if in.Root != "" {
		return in.Root
	}
	return tools.EnginesDir()
}

func (in *Installer) components() map[string]Component {
	if in.Components == nil {
		in.Components = Components()
	}
	return in.Components
}

func (in *Installer) log() *slog.Logger {
	if in.Log != nil {
		return in.Log
	}
	return slog.Default()
}

// IsInstalled: every component is installed (any version).
func (in *Installer) IsInstalled(p Package) bool {
	for _, c := range p.Components {
		if _, ok := in.components()[c]; !ok || in.marker(c) == "" {
			return false
		}
	}
	return true
}

// IsUpToDate: every component is installed in the version engines.json pins.
func (in *Installer) IsUpToDate(p Package) bool {
	for _, c := range p.Components {
		comp, ok := in.components()[c]
		if !ok || !strings.EqualFold(in.marker(c), comp.Sha256) {
			return false
		}
	}
	return true
}

// Install downloads and unpacks the components that are missing or outdated.
func (in *Installer) Install(ctx context.Context, p Package, progress func(Progress)) error {
	comps := in.components()
	for _, c := range p.Components {
		if _, ok := comps[c]; !ok {
			if r := UnavailableReason(p, comps, runtime.GOOS); r != "" {
				return errors.New(r)
			}
			return fmt.Errorf("The %s package is unavailable on this platform.", p.ID)
		}
	}
	var pending []Component
	var total int64
	for _, c := range p.Components {
		if !strings.EqualFold(in.marker(c), comps[c].Sha256) {
			pending = append(pending, comps[c])
			total += comps[c].Size
		}
	}
	total = max(1, total)
	if err := os.MkdirAll(in.root(), 0o755); err != nil {
		return err
	}
	var done int64
	report := func(s Stage, id string, bytes int64) {
		if progress != nil {
			progress(Progress{s, id, min(1, float64(bytes)/float64(total)), bytes, total})
		}
	}
	for _, c := range pending {
		offset := done
		file, err := in.download(ctx, c, func(n int64) { report(Downloading, c.ID, offset+n) })
		if err != nil {
			return err
		}
		report(Unpacking, c.ID, offset+c.Size)
		err = in.unpack(ctx, c, file)
		os.Remove(file)
		if err != nil {
			return err
		}
		done += c.Size
		in.log().Info("installed engine component", "component", c.ID, "version", c.Version)
	}
	if len(pending) > 0 {
		return in.copyRuntime(p)
	}
	tools.Forget()
	return nil
}

// Uninstall removes the package's folders.
func (in *Installer) Uninstall(p Package) error {
	for _, id := range p.Components {
		if err := os.RemoveAll(in.folderOf(id)); err != nil {
			return err
		}
	}
	tools.Forget()
	return nil
}

// ───────────────────────── Download ─────────────────────────

func (in *Installer) client() *http.Client {
	if in.HTTP != nil {
		return in.HTTP
	}
	// Download sites send users to mirrors, some plain HTTP: safe here, every file is checked against its SHA-256.
	return &http.Client{Timeout: 60 * time.Minute}
}

func (in *Installer) download(ctx context.Context, c Component, reportBytes func(int64)) (string, error) {
	downloads := filepath.Join(in.root(), ".downloads")
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		return "", err
	}
	u, err := url.Parse(c.URL)
	if err != nil {
		return "", err
	}
	file := filepath.Join(downloads, c.ID+"-"+c.Version+path.Ext(u.Path))
	partial := file + ".part"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return "", err
	}
	if in.UserAgent != "" {
		req.Header.Set("User-Agent", in.UserAgent)
	}
	resp, err := in.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("%s: download failed (%s)", c.ID, resp.Status)
	}
	out, err := os.Create(partial)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	buf := make([]byte, 1<<16)
	var received int64
	last := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := out.Write(buf[:n]); err != nil {
				out.Close()
				os.Remove(partial)
				return "", err
			}
			h.Write(buf[:n])
			received += int64(n)
			if time.Since(last) > 150*time.Millisecond {
				reportBytes(received)
				last = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			os.Remove(partial)
			return "", rerr
		}
	}
	reportBytes(received)
	if err := out.Close(); err != nil {
		return "", err
	}
	actual := strings.ToUpper(hex.EncodeToString(h.Sum(nil)))
	if !strings.EqualFold(actual, c.Sha256) {
		os.Remove(partial)
		return "", fmt.Errorf("%s: checksum mismatch (expected %s, got %s). The download was discarded.", c.ID, c.Sha256, actual)
	}
	if err := os.Rename(partial, file); err != nil {
		return "", err
	}
	return file, nil
}

// ───────────────────────── Unpack ─────────────────────────

func (in *Installer) unpack(ctx context.Context, c Component, file string) error {
	staging := filepath.Join(in.root(), "~"+randomHex(6))
	defer os.RemoveAll(staging)
	var err error
	switch c.Kind {
	case "zip":
		if err = UnzipTo(file, staging); err == nil {
			root := singleRoot(staging)
			ensureExecutable(c.ID, root)
			err = in.replace(c, root)
		}
	case "tar.gz", "tar.xz":
		if err = UntarTo(ctx, file, staging, c.Kind); err == nil {
			root := singleRoot(staging)
			ensureExecutable(c.ID, root)
			err = in.replace(c, root)
		}
	case "dmg":
		if err = unpackDiskImage(ctx, c.ID, file, staging, in.root()); err == nil {
			err = in.replace(c, staging)
		}
	case "msi":
		var folder string
		if folder, err = unpackMsi(ctx, c.ID, file, staging); err == nil {
			err = in.replace(c, folder)
		}
	case "conda":
		if err = UnpackConda(ctx, file, staging); err == nil {
			err = in.replace(c, staging)
		}
	case "traineddata":
		// One Tesseract language: the file as it is, in a folder of its own.
		if err = os.MkdirAll(staging, 0o755); err == nil {
			if err = copyFile(file, filepath.Join(staging, c.ID+".traineddata")); err == nil {
				err = in.replace(c, staging)
			}
		}
	case "nsis":
		// NSIS installers (Tesseract) are unpacked with 7-Zip, never run: files only.
		seven := tools.Find(tools.Spec{Names: []string{"7z", "7zz", "7za"}, Env: "FILEE_7ZIP", Package: "7zip", Dirs: map[string][]string{"windows": {"7zip", "C:/Program Files/7-Zip"}}})
		if seven == "" {
			return fmt.Errorf("%s needs 7-Zip, which is missing.", c.ID)
		}
		if err = os.MkdirAll(staging, 0o755); err == nil {
			_, err = tools.Run(ctx, tools.Cmd{Path: seven, Args: []string{"x", "-y", "-o" + staging, file}, Timeout: 15 * time.Minute})
			if err == nil {
				TrimTesseract(staging)
				err = in.replace(c, staging)
			}
		}
	case "oxt":
		// LibreOffice registers bundled extensions (folders under share/extensions) on start-up.
		ext := filepath.Join(in.root(), "libreoffice", "share", "extensions")
		if st, e := os.Stat(ext); e != nil || !st.IsDir() {
			return fmt.Errorf("%s needs LibreOffice, which is not installed.", c.ID)
		}
		if err = UnzipTo(file, staging); err == nil {
			target := in.folderOf(c.ID)
			os.RemoveAll(target)
			err = os.Rename(staging, target)
		}
	default:
		return fmt.Errorf("Unknown engine package kind '%s'.", c.Kind)
	}
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(in.folderOf(c.ID), markerName), []byte(c.Sha256), 0o644)
}

// replace makes source the component's folder, replacing a previous version.
func (in *Installer) replace(c Component, source string) error {
	if progs, ok := Programs[c.ID]; ok {
		goos := runtime.GOOS
		if c.Kind == "msi" || c.Kind == "conda" || c.Kind == "nsis" {
			goos = "windows"
		}
		if folderWith(source, ProgramsOn(c.ID, goos)) == "" {
			return fmt.Errorf("The %s package is missing required executable programs in its expected folders (%s).", c.ID, strings.Join(progs, ", "))
		}
	}
	target := in.folderOf(c.ID)
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	return os.Rename(source, target)
}

// folderWith is the folder (root, bin, program, Contents/MacOS, an .app's) holding every program.
func folderWith(root string, programs []string) string {
	candidates := []string{root, filepath.Join(root, "bin"), filepath.Join(root, "program"), filepath.Join(root, "Contents", "MacOS")}
	for _, app := range []string{"LibreOffice.app", "calibre.app"} {
		candidates = append(candidates, filepath.Join(root, app, "Contents", "MacOS"))
	}
	for _, f := range candidates {
		ok := true
		for _, p := range programs {
			if st, err := os.Stat(filepath.Join(f, p)); err != nil || st.IsDir() {
				ok = false
				break
			}
		}
		if ok {
			return f
		}
	}
	return ""
}

// ensureExecutable sets the execute bits of the package's programs (a zip may not keep them).
func ensureExecutable(id, root string) {
	if runtime.GOOS == "windows" {
		return
	}
	progs := ProgramsOn(id, runtime.GOOS)
	f := folderWith(root, progs)
	if f == "" {
		return
	}
	for _, p := range progs {
		path := filepath.Join(f, p)
		if st, err := os.Stat(path); err == nil {
			os.Chmod(path, st.Mode()|0o111)
		}
	}
}

// singleRoot: an archive with a single top-level folder is unpacked without that level.
func singleRoot(folder string) string {
	e, err := os.ReadDir(folder)
	if err == nil && len(e) == 1 && e[0].IsDir() {
		return filepath.Join(folder, e[0].Name())
	}
	return folder
}

// safeJoin is name below root, or an error for absolute names and ones that climb out.
func safeJoin(root, name string) (string, error) {
	name = strings.ReplaceAll(name, `\`, "/")
	clean := path.Clean("/" + name)
	if strings.HasPrefix(name, "/") || strings.Contains(name, ":") && runtime.GOOS == "windows" || slices.Contains(strings.Split(name, "/"), "..") {
		return "", fmt.Errorf("unsafe path in package: %s", name)
	}
	return filepath.Join(root, filepath.FromSlash(clean[1:])), nil
}

// UnzipTo unpacks a zip file into target (paths checked, modes kept).
func UnzipTo(file, target string) error {
	z, err := zip.OpenReader(file)
	if err != nil {
		return err
	}
	defer z.Close()
	for _, f := range z.File {
		p, err := safeJoin(target, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		mode := f.Mode().Perm() | 0o600
		if f.Mode()&os.ModeSymlink != 0 {
			r, err := f.Open()
			if err != nil {
				return err
			}
			link, _ := io.ReadAll(io.LimitReader(r, 4096))
			r.Close()
			if err := safeLink(target, p, string(link)); err != nil {
				return err
			}
			continue
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		err = writeFile(p, r, mode)
		r.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// UntarTo unpacks a tar.gz or tar.xz into target.
func UntarTo(ctx context.Context, file, target, kind string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	var r io.Reader
	switch kind {
	case "tar.gz":
		g, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer g.Close()
		r = g
	case "tar.xz":
		x, err := xz.NewReader(f)
		if err != nil {
			return err
		}
		r = x
	default:
		return fmt.Errorf("unsupported archive %s", kind)
	}
	return untar(ctx, tar.NewReader(r), target, "")
}

// untar extracts the entries below prefix ("" for all) into target, without the prefix.
func untar(ctx context.Context, t *tar.Reader, target, prefix string) error {
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	count := 0
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		h, err := t.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(strings.ReplaceAll(h.Name, `\`, "/"), "./")
		if prefix != "" {
			if !strings.HasPrefix(name, prefix) {
				continue
			}
			name = name[len(prefix):]
		}
		if name == "" {
			continue
		}
		p, err := safeJoin(target, name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if prefix == "" {
				if err := os.MkdirAll(p, 0o755); err != nil {
					return err
				}
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			if err := writeFile(p, t, os.FileMode(h.Mode).Perm()|0o600); err != nil {
				return err
			}
			count++
		case tar.TypeSymlink:
			if prefix != "" {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			if err := safeLink(target, p, h.Linkname); err != nil {
				return err
			}
		}
	}
	if prefix != "" && count == 0 {
		return fmt.Errorf("the package contains nothing under %s", prefix)
	}
	return nil
}

// safeLink makes a symbolic link only when it stays inside root.
func safeLink(root, at, link string) error {
	if filepath.IsAbs(link) {
		return fmt.Errorf("unsafe link in package: %s", link)
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(at), link))
	rel, err := filepath.Rel(root, resolved)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("unsafe link in package: %s", link)
	}
	if runtime.GOOS == "windows" {
		return nil // links in Unix packages only
	}
	return os.Symlink(link, at)
}

func writeFile(p string, r io.Reader, mode os.FileMode) error {
	out, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	return writeFile(to, in, 0o644)
}

// UnpackConda unpacks the Windows binaries of a conda package (a zip with
// pkg-*.tar.zst): the files under Library/bin land directly in target.
func UnpackConda(ctx context.Context, file, target string) error {
	z, err := zip.OpenReader(file)
	if err != nil {
		return err
	}
	defer z.Close()
	var payload *zip.File
	for _, f := range z.File {
		if strings.HasPrefix(f.Name, "pkg-") && strings.HasSuffix(f.Name, ".tar.zst") {
			payload = f
			break
		}
	}
	if payload == nil {
		return errors.New("Not a conda package (pkg-*.tar.zst is missing).")
	}
	r, err := payload.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	d, err := zstd.NewReader(r)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := untar(ctx, tar.NewReader(d), target, "Library/bin/"); err != nil {
		return fmt.Errorf("The conda package contains no Windows binaries (Library/bin): %w", err)
	}
	return nil
}

// unpackDiskImage copies the .app of a disk image (macOS: hdiutil, ditto).
func unpackDiskImage(ctx context.Context, id, file, target, root string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("Disk image packages can only be installed on macOS.")
	}
	app := map[string]string{"libreoffice": "LibreOffice.app", "calibre": "calibre.app"}[id]
	if app == "" {
		return fmt.Errorf("No application bundle is specified for %s.", id)
	}
	// The mount stays outside staging, so a failed unmount is never removed with it.
	mount := filepath.Join(root, ".mount-"+randomHex(16))
	if err := os.MkdirAll(mount, 0o755); err != nil {
		return err
	}
	attached := false
	defer func() {
		_, derr := tools.Run(context.Background(), tools.Cmd{Path: "/usr/bin/hdiutil", Args: []string{"detach", mount}, Timeout: 30 * time.Second})
		if derr == nil || !attached {
			os.Remove(mount)
		}
	}()
	if _, err := tools.Run(ctx, tools.Cmd{Path: "/usr/bin/hdiutil", Args: []string{"attach", "-readonly", "-nobrowse", "-mountpoint", mount, file}, Timeout: 3 * time.Minute}); err != nil {
		return fmt.Errorf("Could not open %s disk image: %w", id, err)
	}
	attached = true
	src := filepath.Join(mount, app)
	if st, err := os.Lstat(src); err != nil || !st.IsDir() {
		return fmt.Errorf("%s is missing from the disk image.", app)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	if _, err := tools.Run(ctx, tools.Cmd{Path: "/usr/bin/ditto", Args: []string{src, filepath.Join(target, app)}, Timeout: 10 * time.Minute}); err != nil {
		return fmt.Errorf("Could not copy %s: %w", app, err)
	}
	return nil
}

// unpackMsi unpacks an MSI with an administrative install (msiexec /a): files
// only, no registry entries, no admin rights. It returns the folder to keep.
func unpackMsi(ctx context.Context, id, msi, target string) (string, error) {
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("The %s package is a Windows installer.", id)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", err
	}
	log := filepath.Join(os.TempDir(), "filee-msiexec.log")
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	// msiexec parses its own command line: TARGETDIR="..." must be quoted as one argument.
	cmd := exec.CommandContext(ctx, "msiexec.exe")
	setRawCommandLine(cmd, fmt.Sprintf(`msiexec.exe /a "%s" /qn /l*v "%s" TARGETDIR="%s"`, msi, log, target))
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return "", MsiFailure(id, ee.ExitCode(), log)
	}
	if err != nil {
		return "", err
	}
	if id == "calibre" {
		f := findFile(target, "ebook-convert.exe")
		if f == "" {
			return "", errors.New("ebook-convert.exe not found in the calibre package.")
		}
		return filepath.Dir(f), nil
	}
	soffice := findFile(target, "soffice.exe")
	if soffice == "" {
		return "", errors.New("soffice.exe not found in the LibreOffice package.")
	}
	lo := filepath.Dir(filepath.Dir(soffice))
	TrimLibreOffice(lo)
	CopyLibreOfficeRuntime(lo)
	return lo, nil
}

// MsiFailure is what the Engines card shows for an msiexec exit code.
func MsiFailure(id string, code int, log string) error {
	switch code {
	case 1618:
		return convert.Refuse("error.msi_busy", "")
	case 1620:
		return convert.Refuse("error.msi_link", fmt.Sprintf("msiexec 1620, log: %s", log))
	}
	return fmt.Errorf("Unpacking %s failed (msiexec exit code %d, log: %s).", id, code, log)
}

func findFile(root, name string) string {
	var found string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && found == "" && !d.IsDir() && strings.EqualFold(d.Name(), name) {
			found = p
		}
		return nil
	})
	return found
}

// TrimLibreOffice removes what headless conversion never uses (~500 MB):
// help, readmes, the MSI copy, the gallery and dictionaries except English
// and Korean (their hyphenation affects page layout).
func TrimLibreOffice(root string) {
	for _, f := range []string{"help", "readmes", filepath.Join("share", "gallery")} {
		os.RemoveAll(filepath.Join(root, f))
	}
	if m, _ := filepath.Glob(filepath.Join(root, "*.msi")); m != nil {
		for _, f := range m {
			os.Remove(f)
		}
	}
	d, _ := filepath.Glob(filepath.Join(root, "share", "extensions", "dict-*"))
	for _, f := range d {
		if b := filepath.Base(f); b != "dict-en" && b != "dict-ko" {
			os.RemoveAll(f)
		}
	}
}

// CopyLibreOfficeRuntime copies the C++ runtime an administrative install
// leaves in System64 into program/, where soffice.exe looks; it never
// replaces a file. It returns the number of files copied.
func CopyLibreOfficeRuntime(root string) int {
	sys, prog := filepath.Join(root, "System64"), filepath.Join(root, "program")
	if !isDir(sys) || !isDir(prog) {
		return 0
	}
	n := 0
	dlls, _ := filepath.Glob(filepath.Join(sys, "*.dll"))
	for _, d := range dlls {
		t := filepath.Join(prog, filepath.Base(d))
		if _, err := os.Stat(t); err == nil {
			continue
		}
		if copyFile(d, t) == nil {
			n++
		}
	}
	return n
}

// TrimTesseract removes what OCR never uses: the training programs, the
// installer's files and the bundled language data (Filee downloads its own).
func TrimTesseract(root string) {
	for _, f := range []string{"$PLUGINSDIR", "tessdata"} {
		os.RemoveAll(filepath.Join(root, f))
	}
	for _, pat := range []string{"*.exe", "*.html"} {
		m, _ := filepath.Glob(filepath.Join(root, pat))
		for _, f := range m {
			if !strings.EqualFold(filepath.Base(f), "tesseract.exe") {
				os.Remove(f)
			}
		}
	}
}

// copyRuntime copies the "vcruntime" DLLs next to the programs of the
// package's other conda components (Windows looks in a program's folder first).
func (in *Installer) copyRuntime(p Package) error {
	const rt = "vcruntime"
	if !slices.Contains(p.Components, rt) || !isDir(in.folderOf(rt)) {
		return nil
	}
	dlls, _ := filepath.Glob(filepath.Join(in.folderOf(rt), "*.dll"))
	for _, id := range p.Components {
		if id == rt || in.components()[id].Kind != "conda" {
			continue
		}
		for _, d := range dlls {
			if err := copyFile(d, filepath.Join(in.folderOf(id), filepath.Base(d))); err != nil {
				return err
			}
		}
	}
	tools.Forget()
	return nil
}

// folderOf is a component's folder; the LibreOffice extension lives inside LibreOffice.
func (in *Installer) folderOf(id string) string {
	if c, ok := in.components()[id]; ok && c.Kind == "oxt" {
		name := id
		if id == "h2orestart" {
			name = "H2Orestart"
		}
		return filepath.Join(in.root(), "libreoffice", "share", "extensions", name)
	}
	return filepath.Join(in.root(), id)
}

// FolderOf is where component id is installed.
func (in *Installer) FolderOf(id string) string { return in.folderOf(id) }

func (in *Installer) marker(id string) string {
	b, err := os.ReadFile(filepath.Join(in.folderOf(id), markerName))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func isDir(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }
