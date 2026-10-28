package media

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func randomName() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

func tryDelete(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func isDir(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }

func samePath(a, b string) bool {
	x, err1 := filepath.Abs(a)
	y, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return a == b
	}
	return strings.EqualFold(x, y)
}

// moveFile renames, or copies and deletes across drives.
func moveFile(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(to)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(to)
		return err
	}
	in.Close()
	return os.Remove(from)
}
