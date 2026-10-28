package ebook

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "image/gif"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/formats"
)

// An e-book in memory: the document model (content) plus the metadata
// e-book formats carry (title, authors, language, cover). Every e-book reader
// produces a Book and every e-book writer consumes one.

// Metadata is the title, authors, language and cover of a book.
type Metadata struct {
	Title       string
	Authors     []string
	Language    string // BCP 47 ("ko", "en-US")
	Description string
	Publisher   string
	CoverImage  string // local picture file of the cover
}

// Book is content and metadata.
type Book struct {
	Doc  *doc.Document
	Meta *Metadata
}

// DisplayTitle: the metadata title, the document's own, the first heading, else the file name.
func (b *Book) DisplayTitle(sourcePath string) string {
	if t := strings.TrimSpace(b.Meta.Title); t != "" {
		return t
	}
	if t := strings.TrimSpace(b.Doc.Title); t != "" {
		return t
	}
	for _, s := range b.Doc.Sections {
		for _, bl := range s.Blocks {
			if p, ok := bl.(*doc.Paragraph); ok && p.HeadingLevel > 0 {
				if t := []rune(doc.PlainText(p.Inlines)); len(t) > 0 {
					if len(t) > 200 {
						t = t[:200]
					}
					return string(t)
				}
			}
		}
	}
	return formats.NameWithoutExtension(sourcePath)
}

// DisplayLanguage: the metadata language, else guessed from the text: Korean, Japanese, Chinese or English.
func (b *Book) DisplayLanguage() string {
	if l := strings.TrimSpace(b.Meta.Language); l != "" {
		return l
	}
	hangul, han, kana, latin := 0, 0, 0, 0
	n := 0
	for _, s := range b.Doc.Sections {
		for _, in := range doc.Inlines(s.Blocks) {
			t, ok := in.(*doc.Text)
			if !ok {
				continue
			}
			if n++; n > 2000 {
				break
			}
			for _, c := range t.Text {
				switch {
				case c >= '가' && c <= '힣':
					hangul++
				case c >= '぀' && c <= 'ヿ':
					kana++
				case c >= '一' && c <= '鿿':
					han++
				case c < 128 && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'):
					latin++
				}
			}
		}
	}
	switch {
	case hangul > 0 && hangul*3 >= latin:
		return "ko"
	case kana > 0 && kana*3 >= latin:
		return "ja"
	case han > 0 && han*3 >= latin:
		return "zh"
	}
	return "en"
}

// drmMessage: Filee never removes copy protection.
const drmMessage = "This book is protected with DRM (copy protection), so it cannot be converted. Filee does not remove DRM; open it in the reading app it was bought for."

var errDRM = errors.New(drmMessage)

// extractZip unpacks a ZIP into folder; entries that would land outside it are skipped.
func extractZip(z *zip.Reader, folder string) error {
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return err
	}
	for _, f := range z.File {
		if strings.HasSuffix(f.Name, "/") || strings.HasSuffix(f.Name, `\`) {
			continue
		}
		target := safePath(folder, f.Name)
		if target == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// safePath is a path inside root for an archive entry name, or "" if it would escape.
func safePath(root, entry string) string {
	rel := strings.TrimLeft(strings.ReplaceAll(entry, `\`, "/"), "/")
	if rel == "" || strings.Contains(rel, ":") {
		return ""
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	full := filepath.Join(absRoot, filepath.FromSlash(rel))
	prefix := strings.TrimRight(absRoot, `/\`) + string(filepath.Separator)
	if !strings.HasPrefix(strings.ToLower(full), strings.ToLower(prefix)) {
		return ""
	}
	return full
}

// newFolder is a fresh folder inside the job's work directory.
func newFolder(work, prefix string) (string, error) {
	f := filepath.Join(work, prefix+"-"+randomHex(4))
	return f, os.MkdirAll(f, 0o755)
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// imageMediaType is the media type of a picture file by extension, "" for files that are no pictures.
func imageMediaType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".jpe", ".jfif":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".tif", ".tiff":
		return "image/tiff"
	case ".avif":
		return "image/avif"
	case ".jxl":
		return "image/jxl"
	}
	return ""
}

// commonImage is a picture in a format every reading system shows: JPEG and
// PNG (plus GIF and SVG when gifAndSvg) are kept, others are converted into
// folder: to JPEG for opaque photos (WEBP, TIFF … would grow a lot as PNG),
// else PNG. ok false when the file can't be read.
func commonImage(path, folder string, gifAndSvg bool) (file, mediaType string, ok bool) {
	mt := imageMediaType(path)
	if mt == "image/jpeg" || mt == "image/png" || gifAndSvg && (mt == "image/gif" || mt == "image/svg+xml") {
		return path, mt, true
	}
	f, err := os.Open(path)
	if err != nil {
		return "", "", false
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return "", "", false
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", "", false
	}
	asJPEG := opaque(img) && (mt == "image/webp" || mt == "image/avif" || mt == "image/jxl" || mt == "image/tiff")
	ext, mt := "png", "image/png"
	if asJPEG {
		ext, mt = "jpg", "image/jpeg"
	}
	out := filepath.Join(folder, randomHex(8)+"."+ext)
	w, err := os.Create(out)
	if err != nil {
		return "", "", false
	}
	if asJPEG {
		err = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	} else {
		err = png.Encode(w, img)
	}
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", "", false
	}
	return out, mt, true
}

func opaque(img image.Image) bool {
	if o, ok := img.(interface{ Opaque() bool }); ok {
		return o.Opaque()
	}
	return true
}

// imageSize is the pixel size of a picture, ok false when it can't be read.
func imageSize(path string) (int, int, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	c, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, false
	}
	return c.Width, c.Height, true
}


func esc(s string) string { return doc.EscapeXML(s) }
