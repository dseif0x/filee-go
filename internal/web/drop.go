package web

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"crypto/rand"
	"encoding/hex"

	"github.com/dseif0x/filee-go/internal/jobs"
	"github.com/dseif0x/filee-go/internal/presets"
)

// DropItem is one file a drag brought from a browser, not saved yet.
type DropItem interface {
	// Name is the name it will most likely be saved under; its extension
	// decides the donut's slices. The saved file may still get another
	// extension when its bytes say it is something else.
	Name() string
	// Save writes the file into folder and returns its path.
	Save(ctx context.Context, folder string, d *Downloader, progress func(jobs.TransferProgress)) (string, error)
	// Close ends the transfer (the source app may wait for that).
	Close() error
}

type streamItem struct {
	name  string
	open  func(context.Context) (io.ReadCloser, error)
	size  int64
	mime  string
	owner io.Closer
}

func (s *streamItem) Name() string { return s.name }

func (s *streamItem) Save(ctx context.Context, folder string, d *Downloader, progress func(jobs.TransferProgress)) (string, error) {
	r, err := s.open(ctx)
	if err != nil {
		return "", err
	}
	defer r.Close()
	return d.Save(ctx, r, folder, s.name, s.mime, s.size, progress)
}

func (s *streamItem) Close() error {
	if s.owner != nil {
		return s.owner.Close()
	}
	return nil
}

// FromBytes is the bytes the browser hands over (a picture's file, a data: address).
func FromBytes(name string, b []byte, mime string) DropItem {
	return &streamItem{name: SanitizeName(name, ""), size: int64(len(b)), mime: mime,
		open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(b)), nil }}
}

// FromStream is a file whose bytes the source app streams on request (a
// Windows virtual file). owner is closed with the item, telling the source
// the transfer is over. size <= 0 when unknown.
func FromStream(name string, open func(context.Context) (io.ReadCloser, error), size int64, owner io.Closer) DropItem {
	return &streamItem{name: SanitizeName(name, ""), open: open, size: size, owner: owner}
}

// FromTemporaryFile is a file the browser wrote somewhere temporary
// (Firefox): copied, the browser's copy left alone.
func FromTemporaryFile(path string) DropItem {
	var size int64
	if fi, err := os.Stat(path); err == nil {
		size = fi.Size()
	}
	return &streamItem{name: SanitizeName(filepath.Base(path), ""), size: size,
		open: func(context.Context) (io.ReadCloser, error) { return os.Open(path) }}
}

type addressItem struct {
	name string
	url  *url.URL
}

// FromAddress is an address Filee downloads itself (without the browser's sign-in).
func FromAddress(name string, u *url.URL) DropItem {
	return &addressItem{name: SanitizeName(name, ""), url: u}
}

func (a *addressItem) Name() string { return a.name }
func (a *addressItem) Close() error { return nil }

// URL is the address the item downloads.
func (a *addressItem) URL() *url.URL { return a.url }

func (a *addressItem) Save(ctx context.Context, folder string, d *Downloader, progress func(jobs.TransferProgress)) (string, error) {
	// A name the server sends wins; a guessed extension is corrected from the bytes.
	return d.Download(ctx, a.url, folder, a.name, progress)
}

// Drop is the files of one browser drag, each standing in the donut and the
// job as a path that doesn't exist yet (a placeholder in WorkFolder, named
// like the file will be) until its conversion saves it.
type Drop struct {
	work         string
	items        map[string]DropItem
	placeholders []string
	closeOnce    sync.Once
}

// DefaultDropRoot is <temp>/Filee/web: originals that aren't kept are saved
// here and deleted after their conversion.
func DefaultDropRoot() string { return filepath.Join(os.TempDir(), "Filee", "web") }

// NewDrop makes a drop of items with its own folder below root ("" for DefaultDropRoot).
func NewDrop(items []DropItem, root string) *Drop {
	if root == "" {
		root = DefaultDropRoot()
	}
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	d := &Drop{work: filepath.Join(root, hex.EncodeToString(b)), items: map[string]DropItem{}}
	for i, it := range items {
		// A folder per item: two pictures called "image.jpg" stay two files.
		p := filepath.Join(d.work, "drop", strconv.Itoa(i+1), it.Name())
		d.items[p] = it
		d.placeholders = append(d.placeholders, p)
	}
	return d
}

// WorkFolder is the drop's own folder.
func (d *Drop) WorkFolder() string { return d.work }

// Placeholders are the paths the files stand as until they are saved.
func (d *Drop) Placeholders() []string { return d.placeholders }

// Holds reports a path of this drop that isn't a file yet.
func (d *Drop) Holds(path string) bool { _, ok := d.items[path]; return ok }

// IsPlaceholder reports a placeholder of any drop below the default root.
func IsPlaceholder(path string) bool {
	root, err := filepath.Abs(DefaultDropRoot())
	if err != nil {
		return false
	}
	full, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	prefix := strings.TrimRight(root, `/\`) + string(filepath.Separator)
	if !strings.HasPrefix(strings.ToLower(full), strings.ToLower(prefix)) {
		return false
	}
	parts := strings.Split(full[len(prefix):], string(filepath.Separator))
	return len(parts) == 4 && parts[1] == "drop"
}

// Close ends the transfers the items still hold open.
func (d *Drop) Close() {
	d.closeOnce.Do(func() {
		for _, it := range d.items {
			_ = it.Close()
		}
	})
}

// DeleteStale deletes drop folders older than age that a crash left behind.
func DeleteStale(root string, age time.Duration) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err == nil && time.Since(info.ModTime()) > age {
			_ = os.RemoveAll(filepath.Join(root, e.Name()))
		}
	}
}

// DropOptions say where files dragged from a browser are saved.
type DropOptions struct {
	// Folder is the download folder; "next to the source" means this folder for these files.
	Folder string
	// SaveOnlyConverted: the original goes into the work folder and is deleted after the conversion.
	SaveOnlyConverted bool
}

// Fetcher saves a drop's files for the job that converts them, and cleans up after it.
type Fetcher struct {
	drop       *Drop
	opt        DropOptions
	downloader *Downloader
}

var _ jobs.SourceFetcher = (*Fetcher)(nil)

// Fetcher returns the fetcher that saves the drop's files when a conversion runs.
func (d *Drop) Fetcher(o DropOptions, dl *Downloader) *Fetcher {
	return &Fetcher{drop: d, opt: o, downloader: dl}
}

// Options are the fetcher's options.
func (f *Fetcher) Options() DropOptions { return f.opt }

// Apply is the preset with "next to the source" pointing at the download
// folder, also when the original is only kept in the work folder.
func (f *Fetcher) Apply(p *presets.Preset) *presets.Preset {
	c := p.Clone()
	c.Output.SourceFolder = f.opt.Folder
	return c
}

// SaveFolder is the folder the originals are saved in.
func (f *Fetcher) SaveFolder() string {
	if f.opt.SaveOnlyConverted {
		return filepath.Join(f.drop.work, "files")
	}
	return f.opt.Folder
}

func (f *Fetcher) Fetches(path string) bool { return f.drop.Holds(path) }

func (f *Fetcher) Fetch(ctx context.Context, placeholder string, progress func(jobs.TransferProgress)) (string, error) {
	it, ok := f.drop.items[placeholder]
	if !ok {
		return "", os.ErrNotExist
	}
	return it.Save(ctx, f.SaveFolder(), f.downloader, progress)
}

func (f *Fetcher) Release(*jobs.Job) {
	f.drop.Close()
	// Holds the originals of "Save only the converted file"; otherwise it was never created.
	_ = os.RemoveAll(f.drop.work)
}
