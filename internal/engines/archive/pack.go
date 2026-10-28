package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/flate"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	dbzip2 "github.com/dsnet/compress/bzip2"
	"github.com/klauspost/compress/gzip"
	"github.com/ulikunitz/xz"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/presets"
)

// Writes archives. ZIP, TAR and its GZ / BZ2 / XZ forms and the single-file
// GZ / BZ2 / XZ are written in-process, so names are exactly what Filee
// chooses: UTF-8 in ZIP (with the language encoding flag), PAX headers in
// TAR. 7Z is written by 7-Zip.

// packItem is a file or folder to store under EntryName; folders go in with their content.
type packItem struct{ SourcePath, EntryName string }

// builtInWriters are written without 7-Zip.
var builtInWriters = map[string]bool{"zip": true, "tar": true, "tgz": true, "tbz2": true, "txz": true, "gz": true, "bz2": true, "xz": true}

// singleFileFormats hold exactly one file, without name or folders.
var singleFileFormats = []string{"gz", "bz2", "xz"}

type packer struct{ sevenZip string }

// pack writes items into a new archive of format at output. It is built under
// a temporary name next to it and renamed when complete, so a failed or
// cancelled run never leaves a broken archive (or replaces an existing one).
func (p *packer) pack(ctx context.Context, items []packItem, output, format string, level presets.ArchiveLevel, workDir string, progress convert.Progress) error {
	dir := filepath.Dir(output)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	temp := filepath.Join(dir, "."+filepath.Base(output)+".filee-"+randomSuffix()+".tmp")
	defer tryDeleteFile(temp)
	var err error
	switch format {
	case "zip":
		err = writeZip(ctx, items, temp, level, progress)
	case "tar", "tgz", "tbz2", "txz":
		err = writeTar(ctx, items, temp, format, level, progress)
	case "gz", "bz2", "xz":
		if len(items) != 1 || !isFile(items[0].SourcePath) {
			return fmt.Errorf("a .%s file holds a single file. Choose TAR.%s to compress several files or folders", format, strings.ToUpper(format))
		}
		err = writeSingle(ctx, items[0], temp, format, level, progress)
	case "7z":
		err = p.write7z(ctx, items, temp, level, workDir, progress)
	default:
		return fmt.Errorf("Filee cannot write %s archives", format)
	}
	if err != nil {
		return err
	}
	os.Remove(output)
	return os.Rename(temp, output)
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

func (p *packer) write7z(ctx context.Context, items []packItem, temp string, level presets.ArchiveLevel, workDir string, progress convert.Progress) error {
	if p.sevenZip == "" {
		return errNo7Zip
	}
	paths, cleanup, err := namedAs(items, workDir)
	defer cleanup()
	if err != nil {
		return err
	}
	// 7-Zip reports nothing while it works: estimate from the amount of data (~40 MB/s at normal level).
	var bytes int64
	for _, it := range paths {
		bytes += totalSize(it)
	}
	typical := time.Duration(max(0.5, float64(bytes)/40e6) * float64(time.Second))
	est := startEstimate(progress, typical, 0.05, 0.95)
	defer est()
	return (&sevenZip{p.sevenZip}).add(ctx, temp, "7z", level, paths, workDir)
}

// namedAs: 7-Zip stores each path under its own name, so items whose entry
// name differs (a renamed duplicate) are copied under that name first.
func namedAs(items []packItem, workDir string) ([]string, func(), error) {
	var paths, temps []string
	cleanup := func() {
		for _, t := range temps {
			os.RemoveAll(t)
		}
	}
	for _, it := range items {
		if filepath.Base(it.SourcePath) == it.EntryName {
			paths = append(paths, it.SourcePath)
			continue
		}
		folder := filepath.Join(workDir, "rename-"+randomSuffix())
		if err := os.MkdirAll(folder, 0o755); err != nil {
			return nil, cleanup, err
		}
		temps = append(temps, folder)
		dst := filepath.Join(folder, it.EntryName)
		if err := copyTree(it.SourcePath, dst); err != nil {
			return nil, cleanup, err
		}
		paths = append(paths, dst)
	}
	return paths, cleanup, nil
}

// copyTree copies a file or a folder tree without following links.
func copyTree(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || isReparsePoint(info) {
		return nil
	}
	if !info.IsDir() {
		if err := copyFile(src, dst); err != nil {
			return err
		}
		setTime(dst, info.ModTime())
		return nil
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	setTime(dst, info.ModTime())
	return nil
}

func totalSize(p string) int64 {
	var total int64
	filepath.Walk(p, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// packFile is a file or folder in the order it is written, with its name in the archive.
type packFile struct {
	path, entry string
	folder      bool
	length      int64
	modified    time.Time
}

// expand lists every file and folder of the items, folders before their
// content. Links are skipped, never followed, so nothing outside the given
// folders ends up in the archive.
func expand(items []packItem) ([]packFile, error) {
	var out []packFile
	for _, it := range items {
		name := strings.Trim(strings.ReplaceAll(it.EntryName, `\`, "/"), "/")
		info, err := os.Stat(it.SourcePath)
		if err != nil {
			return nil, fmt.Errorf("%s was not found", it.SourcePath)
		}
		if !info.IsDir() {
			out = append(out, packFile{it.SourcePath, name, false, info.Size(), info.ModTime()})
			continue
		}
		if err := expandFolder(it.SourcePath, name, info, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func expandFolder(folder, name string, info os.FileInfo, out *[]packFile) error {
	*out = append(*out, packFile{folder, name, true, 0, info.ModTime()})
	entries, err := os.ReadDir(folder)
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		p := filepath.Join(folder, e.Name())
		ci, err := os.Lstat(p)
		if err != nil || ci.Mode()&os.ModeSymlink != 0 || isReparsePoint(ci) {
			continue
		}
		child := name + "/" + e.Name()
		if ci.IsDir() {
			if err := expandFolder(p, child, ci, out); err != nil {
				return err
			}
		} else if ci.Mode().IsRegular() {
			*out = append(*out, packFile{p, child, false, ci.Size(), ci.ModTime()})
		}
	}
	return nil
}

// byteCounter turns bytes written into progress.
type byteCounter struct {
	total, done int64
	reported    float64
	progress    convert.Progress
}

func (c *byteCounter) add(n int64) {
	c.done += n
	v := 1.0
	if c.total > 0 {
		v = min(1, float64(c.done)/float64(c.total))
	}
	if v-c.reported >= 0.01 {
		c.reported = v
		c.progress.Report(v)
	}
}

func newCounter(files []packFile, progress convert.Progress) *byteCounter {
	c := &byteCounter{progress: progress}
	for _, f := range files {
		c.total += f.length
	}
	return c
}

func flateLevel(level presets.ArchiveLevel) int {
	switch level {
	case presets.ArchiveStore:
		return flate.NoCompression
	case presets.ArchiveFast:
		return flate.BestSpeed
	case presets.ArchiveMaximum:
		return flate.BestCompression
	}
	return flate.DefaultCompression
}

// zipTime: ZIP stores DOS times (local, 1980–2107).
func zipTime(t time.Time) time.Time {
	l := t.Local()
	switch {
	case l.Year() < 1980:
		return time.Date(1980, 1, 1, 0, 0, 0, 0, time.Local)
	case l.Year() > 2107:
		return time.Date(2107, 12, 31, 0, 0, 0, 0, time.Local)
	}
	return l
}

func writeZip(ctx context.Context, items []packItem, path string, level presets.ArchiveLevel, progress convert.Progress) error {
	files, err := expand(items)
	if err != nil {
		return err
	}
	counter := newCounter(files, progress)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	lv := flateLevel(level)
	zw.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) { return flate.NewWriter(w, lv) })
	for _, pf := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Non-ASCII UTF-8 names get the "language encoding" flag from archive/zip, so every
		// unzip tool shows Korean or Chinese names correctly.
		h := &zip.FileHeader{Name: pf.entry, Modified: zipTime(pf.modified)}
		if pf.folder {
			h.Name += "/"
			h.SetMode(os.ModeDir | 0o755)
			if _, err := zw.CreateHeader(h); err != nil {
				return err
			}
			continue
		}
		h.SetMode(0o644)
		h.Method = zip.Deflate
		if level == presets.ArchiveStore {
			h.Method = zip.Store
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		if err := copyFrom(ctx, w, pf.path, counter); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return f.Close()
}

func copyFrom(ctx context.Context, w io.Writer, path string, counter *byteCounter) error {
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	_, err = copyLimited(ctx, w, in, 1<<62, counter.add)
	return err
}

func gzipLevel(level presets.ArchiveLevel) int {
	switch level {
	case presets.ArchiveStore:
		return gzip.NoCompression
	case presets.ArchiveFast:
		return gzip.BestSpeed
	case presets.ArchiveMaximum:
		return gzip.BestCompression
	}
	return gzip.DefaultCompression
}

func bzip2Level(level presets.ArchiveLevel) int {
	switch level {
	case presets.ArchiveStore, presets.ArchiveFast:
		return dbzip2.BestSpeed
	case presets.ArchiveMaximum:
		return dbzip2.BestCompression
	}
	return 5
}

// compressor wraps w in the stream compressor of format.
func compressor(w io.Writer, format string, level presets.ArchiveLevel) (io.WriteCloser, error) {
	switch format {
	case "gz", "tgz":
		return gzip.NewWriterLevel(w, gzipLevel(level))
	case "bz2", "tbz2":
		return dbzip2.NewWriter(w, &dbzip2.WriterConfig{Level: bzip2Level(level)})
	case "xz", "txz":
		cfg := xz.WriterConfig{DictCap: 8 << 20}
		switch level {
		case presets.ArchiveStore, presets.ArchiveFast:
			cfg.DictCap = 1 << 20
		case presets.ArchiveMaximum:
			cfg.DictCap = 64 << 20
		}
		return cfg.NewWriter(w)
	}
	return nopCloser{w}, nil
}

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

// writeTar writes a PAX TAR (UTF-8 names, times), compressed for tgz, tbz2 and txz.
func writeTar(ctx context.Context, items []packItem, path, format string, level presets.ArchiveLevel, progress convert.Progress) error {
	files, err := expand(items)
	if err != nil {
		return err
	}
	counter := newCounter(files, progress)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	cw, err := compressor(f, format, level)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(cw)
	for _, pf := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		h := &tar.Header{Name: pf.entry, ModTime: pf.modified.Truncate(time.Second), Format: tar.FormatPAX, Mode: 0o644, Typeflag: tar.TypeReg, Size: pf.length}
		if pf.folder {
			h.Name += "/"
			h.Typeflag = tar.TypeDir
			h.Mode = 0o755
			h.Size = 0
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if !pf.folder {
			if err := copyFrom(ctx, tw, pf.path, counter); err != nil {
				return err
			}
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := cw.Close(); err != nil {
		return err
	}
	return f.Close()
}

func writeSingle(ctx context.Context, item packItem, path, format string, level presets.ArchiveLevel, progress convert.Progress) error {
	st, err := os.Stat(item.SourcePath)
	if err != nil {
		return err
	}
	counter := &byteCounter{total: st.Size(), progress: progress}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	var cw io.WriteCloser
	if format == "gz" {
		gw, err := gzip.NewWriterLevel(f, gzipLevel(level))
		if err != nil {
			return err
		}
		gw.Name = item.EntryName
		gw.ModTime = st.ModTime()
		cw = gw
	} else if cw, err = compressor(f, format, level); err != nil {
		return err
	}
	if err := copyFrom(ctx, cw, item.SourcePath, counter); err != nil {
		return err
	}
	if err := cw.Close(); err != nil {
		return err
	}
	return f.Close()
}
