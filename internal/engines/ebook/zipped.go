package ebook

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/htmldoc"
	"github.com/dseif0x/filee-go/internal/engines/markdown"
)

// HTMLZ and TXTZ, calibre's zipped single-file formats: HTMLZ is index.html +
// style.css + images + metadata.opf, TXTZ is index.txt (Markdown) + images +
// metadata.opf. Both are read and written here.

func extractZipped(path, work, prefix string) (string, error) {
	folder, err := newFolder(work, prefix)
	if err != nil {
		return "", err
	}
	z, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("this is not a valid %s file (it is not a ZIP archive)", strings.ToUpper(prefix))
	}
	defer z.Close()
	return folder, extractZip(&z.Reader, folder)
}

// mainFile is the preferred name at the top level, else the first file with
// one of the extensions (shallowest first).
func mainFile(folder, preferred string, extensions ...string) string {
	if top := filepath.Join(folder, preferred); isFile(top) {
		return top
	}
	var found []string
	filepath.Walk(folder, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && contains(extensions, strings.ToLower(filepath.Ext(p))) {
			found = append(found, p)
		}
		return nil
	})
	sort.SliceStable(found, func(i, j int) bool {
		di, dj := strings.Count(found[i], string(filepath.Separator)), strings.Count(found[j], string(filepath.Separator))
		if di != dj {
			return di < dj
		}
		return strings.ToLower(found[i]) < strings.ToLower(found[j])
	})
	if len(found) == 0 {
		return ""
	}
	return found[0]
}

func zippedMetadata(folder string) *Metadata {
	opf := mainFile(folder, "metadata.opf", ".opf")
	if opf == "" {
		return &Metadata{}
	}
	p, err := loadOpf(opf)
	if err != nil {
		return &Metadata{} // broken metadata: the content still converts
	}
	m := p.meta
	return &m
}

func readHtmlzBook(ctx context.Context, path, work string) (*Book, error) {
	folder, err := extractZipped(path, work, "htmlz")
	if err != nil {
		return nil, err
	}
	page := mainFile(folder, "index.html", ".html", ".htm", ".xhtml")
	if page == "" {
		return nil, errors.New("the HTMLZ file contains no HTML page")
	}
	d, err := readChapters(ctx, []string{page}, filepath.Join(folder, "~media"))
	if err != nil {
		return nil, err
	}
	meta := zippedMetadata(folder)
	if meta.Title != "" {
		d.Title = meta.Title
	}
	return &Book{d, meta}, nil
}

func readTxtzBook(path, work string) (*Book, error) {
	folder, err := extractZipped(path, work, "txtz")
	if err != nil {
		return nil, err
	}
	text := mainFile(folder, "index.txt", ".txt", ".md", ".markdown", ".text")
	if text == "" {
		return nil, errors.New("the TXTZ file contains no text file")
	}
	b, err := os.ReadFile(text)
	if err != nil {
		return nil, err
	}
	// calibre writes TXTZ text as plain text, Markdown or Textile; Markdown reads all of them sensibly.
	d := markdown.Read(doc.DecodeText(b), filepath.Dir(text), folder)
	meta := zippedMetadata(folder)
	if meta.Title != "" {
		d.Title = meta.Title
	}
	return &Book{d, meta}, nil
}

// imageSet is the pictures of a zipped book: images/imgNNNN.ext, converted to JPEG / PNG / GIF where needed.
type imageSet struct {
	folder string
	names  map[string]string
	files  [][2]string // name, file
}

func newImageSet(folder string) *imageSet {
	return &imageSet{folder: folder, names: map[string]string{}}
}

func (s *imageSet) add(path string) string {
	key := strings.ToLower(path)
	if n, ok := s.names[key]; ok {
		return n
	}
	file, _, ok := commonImage(path, s.folder, true)
	if !ok {
		return ""
	}
	name := fmt.Sprintf("images/img%04d%s", len(s.files)+1, strings.ToLower(filepath.Ext(file)))
	s.files = append(s.files, [2]string{name, file})
	s.names[key] = name
	return name
}

func writeHtmlz(b *Book, output, sourcePath, work string) error {
	title, language := b.DisplayTitle(sourcePath), b.DisplayLanguage()
	folder, err := newFolder(work, "htmlz-images")
	if err != nil {
		return err
	}
	images := newImageSet(folder)
	page := htmldoc.WriteXhtml(b.Doc, htmldoc.XhtmlOptions{ImageSource: images.add})[0]
	return saveZipped(output, [][2]string{
		{"index.html", htmldoc.Page(title, page.Body, language, "style.css", false, "")},
		{"style.css", htmldoc.Css},
		{"metadata.opf", metadataOnlyOpf(b.Meta, title, language)},
	}, images)
}

func writeTxtz(b *Book, output, sourcePath, work string) error {
	title := b.DisplayTitle(sourcePath)
	folder, err := newFolder(work, "txtz-images")
	if err != nil {
		return err
	}
	images := newImageSet(folder)
	text := htmldoc.Markdown(b.Doc, images.add)
	return saveZipped(output, [][2]string{
		{"index.txt", text},
		{"metadata.opf", metadataOnlyOpf(b.Meta, title, b.DisplayLanguage())},
	}, images)
}

func saveZipped(output string, texts [][2]string, images *imageSet) error {
	temp := output + ".tmp"
	f, err := os.Create(temp)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	z := zip.NewWriter(f)
	for _, t := range texts {
		if err := zipText(z, t[0], t[1], zip.Deflate); err != nil {
			f.Close()
			return err
		}
	}
	for _, im := range images.files {
		if err := zipFile(z, im[0], im[1], zip.Store); err != nil {
			f.Close()
			return err
		}
	}
	if err := z.Close(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	os.Remove(output)
	return os.Rename(temp, output)
}
