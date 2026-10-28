package archive

import (
	"context"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"
)

// member is one entry of an archive read by a built-in reader.
type member struct {
	entryInfo
	Modified time.Time
}

// source is an archive opened by a built-in reader: a listing for the
// checks, then its entries in order with their data.
type source interface {
	List() ([]member, error)
	Walk(visit func(m member, data io.Reader) error) error
	Close() error
}

// extractMembers checks the listing (encryption, paths leaving the folder,
// disk space), then unpacks the entries into folder. Symbolic links are
// never created; hard links become copies of their target; clashing names
// are numbered.
func extractMembers(ctx context.Context, src source, archiveName, folder string) error {
	list, err := src.List()
	if err != nil {
		return err
	}
	infos := make([]entryInfo, len(list))
	for i, m := range list {
		infos[i] = m.entryInfo
	}
	if err := checkListing(infos, archiveName, folder); err != nil {
		return err
	}

	type folderTime struct {
		path string
		t    time.Time
	}
	var folders []folderTime
	written := map[string]string{} // entry path → file
	type link struct {
		path, target string
		t            time.Time
	}
	var links []link
	err = src.Walk(func(m member, data io.Reader) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if m.Symlink || m.Path == "" {
			return nil
		}
		path, err := resolveEntryPath(folder, m.Path, archiveName)
		if err != nil {
			return err
		}
		if m.Folder {
			if st, err := os.Stat(path); err == nil && !st.IsDir() {
				path = uniquePath(path)
			}
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
			folders = append(folders, folderTime{path, m.Modified})
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		path = uniquePath(path)
		if m.HardLink != "" {
			links = append(links, link{path, m.HardLink, m.Modified})
			return nil
		}
		out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		limit := int64(math.MaxInt64)
		if m.Size >= 0 {
			limit = m.Size
		}
		_, err = copyLimited(ctx, out, data, limit, nil)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(path)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errDamaged(archiveName, err.Error())
		}
		setTime(path, m.Modified)
		written[cleanEntry(m.Path)] = path
		return nil
	})
	if err != nil {
		return err
	}
	for _, l := range links {
		target, ok := written[cleanEntry(l.target)]
		if !ok {
			continue
		}
		if err := copyFile(target, l.path); err != nil {
			return err
		}
		setTime(l.path, l.t)
	}
	for i := len(folders) - 1; i >= 0; i-- {
		setTime(folders[i].path, folders[i].t)
	}
	return nil
}

func cleanEntry(p string) string {
	return filepath.ToSlash(filepath.Clean("/" + filepath.FromSlash(p)))
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
