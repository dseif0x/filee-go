package archive

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// errTooMuchData: the data unpacks to more than its header declares (a lying or damaged header).
var errTooMuchData = errors.New("the data unpacks to more than the archive declares")

// copyLimited copies, failing when from holds more than maxBytes.
func copyLimited(ctx context.Context, to io.Writer, from io.Reader, maxBytes int64, progress func(int64)) (int64, error) {
	buf := make([]byte, 1<<16)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, err := from.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > maxBytes {
				return total, errTooMuchData
			}
			if _, werr := to.Write(buf[:n]); werr != nil {
				return total, werr
			}
			if progress != nil {
				progress(int64(n))
			}
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// uniquePath is p when free, else "name_1.ext", "name_2.ext" … (as 7-Zip
// names clashing entries), so entries that differ only in case both survive
// on Windows and macOS.
func uniquePath(p string) string {
	if !exists(p) {
		return p
	}
	dir, name := filepath.Split(p)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for n := 1; ; n++ {
		c := filepath.Join(dir, fmt.Sprintf("%s_%d%s", stem, n, ext))
		if !exists(c) {
			return c
		}
	}
}

// contentRoot is the folder whose content forms the result of "Extract":
// the unpacked folder, or its only entry when that is a folder
// ("photos.zip" holding "photos/…" becomes "photos/…", not "photos/photos/…").
func contentRoot(unpacked string) string {
	entries, err := os.ReadDir(unpacked)
	if err == nil && len(entries) == 1 && entries[0].IsDir() {
		return filepath.Join(unpacked, entries[0].Name())
	}
	return unpacked
}

// moveFolder moves the folder source to target. An existing target (the
// preset overwrites) receives the files, replacing those with the same names.
func moveFolder(source, target string) error {
	if st, err := os.Stat(target); err == nil && !st.IsDir() {
		return fmt.Errorf("a file named %s is in the way of the extracted folder", filepath.Base(target))
	}
	if !exists(target) {
		if err := os.Rename(source, target); err == nil {
			return nil
		}
		// Different drives: fall through to copying file by file.
	}
	return mergeInto(source, target)
}

func mergeInto(source, target string) error {
	if info, err := os.Lstat(target); err == nil && (info.Mode()&os.ModeSymlink != 0 || isReparsePoint(info)) {
		return fmt.Errorf("%s is a link; Filee does not extract into links", target)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, e := range entries {
		from := filepath.Join(source, e.Name())
		to := filepath.Join(target, e.Name())
		if !e.IsDir() {
			if st, err := os.Lstat(to); err == nil && st.IsDir() {
				if err := os.RemoveAll(to); err != nil {
					return err
				}
			}
			if err := moveFile(from, to); err != nil {
				return err
			}
			continue
		}
		if st, err := os.Lstat(to); err == nil && !st.IsDir() {
			if err := os.Remove(to); err != nil {
				return err
			}
		}
		if !exists(to) && os.Rename(from, to) == nil {
			continue
		}
		if err := mergeInto(from, to); err != nil {
			return err
		}
	}
	if st, err := os.Stat(source); err == nil {
		os.Chtimes(target, st.ModTime(), st.ModTime())
	}
	return nil
}

// moveFile renames, or copies across drives.
func moveFile(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	st, _ := in.Stat()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if st != nil {
		os.Chtimes(to, st.ModTime(), st.ModTime())
	}
	in.Close()
	return os.Remove(from)
}

func tryDeleteFolder(p string) { os.RemoveAll(p) }
func tryDeleteFile(p string)   { os.Remove(p) }

// setTime sets a modification time when it is known.
func setTime(p string, t time.Time) {
	if !t.IsZero() && t.Year() > 1601 {
		os.Chtimes(p, t, t)
	}
}

// randomSuffix is 8 hex characters for scratch names.
func randomSuffix() string {
	var b [4]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
