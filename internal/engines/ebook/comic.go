package ebook

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/bodgit/sevenzip"
	"github.com/nwaples/rardecode/v2"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/pdf"
)

// Comic book archives: CBZ (ZIP), CBR (RAR), CB7 (7-Zip), CBT (TAR) and CBC
// (a ZIP of CBZ files) are pictures in an archive. Pages are sorted by name
// the way people number them ("page2" before "page10"). Comics become PDF
// (one page per picture, sized like the picture, JPEGs embedded as they
// are), EPUB (fixed layout) or CBZ; PDF becomes CBZ by rendering its pages.

var pictureExtensions = map[string]bool{".jpg": true, ".jpeg": true, ".jpe": true, ".jfif": true, ".png": true, ".gif": true,
	".webp": true, ".bmp": true, ".avif": true, ".jxl": true, ".tif": true, ".tiff": true, ".heic": true, ".heif": true}

var errComicArchive = errors.New("the comic archive cannot be opened (damaged, or not the format its extension says)")
var errComicPassword = errors.New("the comic archive is password-protected")

// comicPages extracts the pages of a comic archive into folder, in reading order.
func comicPages(ctx context.Context, path, format, folder string) ([]string, error) {
	if format == "cbc" {
		// A collection: the CBZ files inside, in the order of comics.txt ("file.cbz:Title" per line) or by name.
		sub, err := newFolder(folder, "cbc")
		if err != nil {
			return nil, err
		}
		files, err := extractComic(ctx, path, "cbz", sub, func(name string) bool {
			return strings.EqualFold(filepath.Ext(name), ".cbz") || strings.EqualFold(name, "comics.txt")
		})
		if err != nil {
			return nil, err
		}
		var list string
		var books []string
		for _, f := range files {
			if f.name == "comics.txt" || strings.EqualFold(filepath.Base(f.key), "comics.txt") {
				list = f.file
			} else {
				books = append(books, f.key+"\x00"+f.file)
			}
		}
		if list != "" {
			b, _ := os.ReadFile(list)
			var order []string
			for _, l := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
				if o := strings.TrimSpace(strings.Split(l, ":")[0]); o != "" {
					order = append(order, o)
				}
			}
			rank := func(entry string) int {
				key := strings.SplitN(entry, "\x00", 2)[0]
				for i, o := range order {
					if strings.HasSuffix(strings.ToLower(key), strings.ToLower(o)) {
						return i
					}
				}
				return len(order)
			}
			sort.SliceStable(books, func(i, j int) bool { return rank(books[i]) < rank(books[j]) })
		}
		var pages []string
		for _, b := range books {
			file := strings.SplitN(b, "\x00", 2)[1]
			pf, err := newFolder(folder, "cbz")
			if err != nil {
				return nil, err
			}
			p, err := comicPages(ctx, file, "cbz", pf)
			if err != nil {
				return nil, err
			}
			pages = append(pages, p...)
		}
		if len(pages) == 0 {
			return nil, errors.New("the comic contains no pictures")
		}
		return pages, nil
	}
	files, err := extractComic(ctx, path, format, folder, func(name string) bool { return pictureExtensions[strings.ToLower(filepath.Ext(name))] })
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("the comic contains no pictures")
	}
	pages := make([]string, len(files))
	for i, f := range files {
		pages[i] = f.file
	}
	return pages, nil
}

type comicFile struct{ key, name, file string }

// extractComic extracts the wanted entries (skipping macOS metadata) and returns them sorted naturally by path.
func extractComic(ctx context.Context, path, format, folder string, wanted func(string) bool) ([]comicFile, error) {
	var files []comicFile
	target := func(key string, dir bool) string {
		name := filepath.Base(strings.ReplaceAll(key, `\`, "/"))
		if dir || strings.Contains(strings.ToUpper(key), "__MACOSX") || strings.HasPrefix(name, ".") || !wanted(name) {
			return ""
		}
		t := filepath.Join(folder, fmt.Sprintf("%05d%s", len(files), strings.ToLower(filepath.Ext(name))))
		files = append(files, comicFile{key, name, t})
		return t
	}
	write := func(t string, r io.Reader) error {
		out, err := os.Create(t)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, r)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		return err
	}
	switch sniffComic(path, format) {
	case "zip":
		z, err := zip.OpenReader(path)
		if err != nil {
			return nil, errComicArchive
		}
		defer z.Close()
		for _, f := range z.File {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			t := target(f.Name, f.Mode().IsDir() || strings.HasSuffix(f.Name, "/"))
			if t == "" {
				continue
			}
			if f.Flags&1 != 0 {
				return nil, errComicPassword
			}
			rc, err := f.Open()
			if err != nil {
				return nil, errComicArchive
			}
			err = write(t, rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
		}
	case "rar":
		r, err := rardecode.OpenReader(path)
		if err != nil {
			if errors.Is(err, rardecode.ErrArchiveEncrypted) {
				return nil, errComicPassword
			}
			return nil, errComicArchive
		}
		defer r.Close()
		for {
			h, err := r.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				if errors.Is(err, rardecode.ErrArchivedFileEncrypted) || errors.Is(err, rardecode.ErrArchiveEncrypted) {
					return nil, errComicPassword
				}
				return nil, errComicArchive
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			t := target(h.Name, h.IsDir)
			if t == "" {
				continue
			}
			if h.Encrypted {
				return nil, errComicPassword
			}
			if err := write(t, r); err != nil {
				return nil, errComicArchive
			}
		}
	case "7z":
		z, err := sevenzip.OpenReader(path)
		if err != nil {
			return nil, errComicArchive
		}
		defer z.Close()
		for _, f := range z.File {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			t := target(f.Name, f.FileInfo().IsDir())
			if t == "" {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil, errComicArchive
			}
			err = write(t, rc)
			rc.Close()
			if err != nil {
				return nil, errComicArchive
			}
		}
	case "tar":
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		tr := tar.NewReader(bufio.NewReader(f))
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				if len(files) == 0 {
					return nil, errComicArchive
				}
				break
			}
			if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA && h.Typeflag != tar.TypeDir {
				continue
			}
			t := target(h.Name, h.Typeflag == tar.TypeDir)
			if t == "" {
				continue
			}
			if err := write(t, tr); err != nil {
				return nil, err
			}
		}
	default:
		return nil, errComicArchive
	}
	sort.SliceStable(files, func(i, j int) bool { return naturalLess(files[i].key, files[j].key) })
	return files, nil
}

// sniffComic: the archive kind from the first bytes (a CBR may be a ZIP and the other way round), else the format.
func sniffComic(path, format string) string {
	f, err := os.Open(path)
	if err == nil {
		head := make([]byte, 262)
		n, _ := io.ReadFull(f, head)
		f.Close()
		head = head[:n]
		switch {
		case bytes.HasPrefix(head, []byte("PK")):
			return "zip"
		case bytes.HasPrefix(head, []byte("Rar!")):
			return "rar"
		case bytes.HasPrefix(head, []byte("7z\xBC\xAF")):
			return "7z"
		case len(head) >= 262 && string(head[257:262]) == "ustar":
			return "tar"
		}
	}
	return map[string]string{"cbz": "zip", "cbc": "zip", "cbr": "rar", "cb7": "7z", "cbt": "tar"}[format]
}

// naturalLess compares names the way people number them: digit runs by value.
func naturalLess(a, b string) bool {
	ra, rb := []rune(strings.ToLower(a)), []rune(strings.ToLower(b))
	i, j := 0, 0
	for i < len(ra) && j < len(rb) {
		if unicode.IsDigit(ra[i]) && unicode.IsDigit(rb[j]) {
			si, sj := i, j
			for i < len(ra) && unicode.IsDigit(ra[i]) {
				i++
			}
			for j < len(rb) && unicode.IsDigit(rb[j]) {
				j++
			}
			na := strings.TrimLeft(string(ra[si:i]), "0")
			nb := strings.TrimLeft(string(rb[sj:j]), "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			continue
		}
		if ra[i] != rb[j] {
			return ra[i] < rb[j]
		}
		i++
		j++
	}
	if len(ra)-i != len(rb)-j {
		return len(ra)-i < len(rb)-j
	}
	return a < b
}

// comicToCbz writes the pages into a CBZ (stored: pictures do not compress further), numbered in order.
func comicToCbz(ctx context.Context, pages []string, output string) error {
	temp := output + ".tmp"
	f, err := os.Create(temp)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	z := zip.NewWriter(f)
	digits := max(3, len(strconv.Itoa(len(pages))))
	for i, p := range pages {
		if err := ctx.Err(); err != nil {
			f.Close()
			return err
		}
		name := fmt.Sprintf("%0*d%s", digits, i+1, strings.ToLower(filepath.Ext(p)))
		if err := zipFile(z, name, p, zip.Store); err != nil {
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

// renderPdfPages renders the PDF's pages (the preset's page range) as JPEG files at the preset's DPI and quality.
func renderPdfPages(ctx context.Context, path string, step *convert.Step, folder string, progress convert.Progress) ([]string, error) {
	count, err := pdf.PageCount(path)
	if err != nil {
		return nil, err
	}
	dpi, quality, rng := 150, 90, ""
	if step.Preset != nil {
		dpi, quality, rng = step.Preset.Pdf.RenderDpi, step.Preset.Image.Quality, step.Preset.Pdf.PageRange
	}
	dpi = max(36, min(600, dpi))
	quality = max(1, min(100, quality))
	pages := convert.ParsePageRange(rng, count)
	var out []string
	err = pdf.RenderPDF(ctx, path, pages, dpi, func(_ int, img *image.RGBA) error {
		file := filepath.Join(folder, fmt.Sprintf("%05d.jpg", len(out)+1))
		f, err := os.Create(file)
		if err != nil {
			return err
		}
		err = jpeg.Encode(f, img, &jpeg.Options{Quality: quality})
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		out = append(out, file)
		progress.Report(0.9 * float64(len(out)) / float64(max(1, len(pages))))
		return err
	})
	return out, err
}

// comicToPdf writes one PDF page per picture, each as large as its picture
// at 96 dpi (or the picture's own density for JPEG files that state one).
// JPEGs are embedded as they are; other pictures become Flate-compressed RGB.
func comicToPdf(ctx context.Context, pages []string, output, work string, progress convert.Progress) error {
	converted, err := newFolder(work, "pdf-pages")
	if err != nil {
		return err
	}
	w := &pdfWriter{objects: make([][]byte, 2)}
	for i, p := range pages {
		if err := ctx.Err(); err != nil {
			return err
		}
		file, mt, ok := commonImage(p, converted, false)
		if !ok {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var img image.Image
		var jpegData []byte
		if mt == "image/jpeg" {
			cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
			if err != nil {
				continue
			}
			img = image.NewGray(image.Rect(0, 0, cfg.Width, cfg.Height)) // size only
			jpegData = data
		} else if img, _, err = image.Decode(bytes.NewReader(data)); err != nil {
			continue
		}
		dpi := 96.0
		if jpegData != nil {
			if d := jfifDensity(jpegData); d >= 36 {
				dpi = d
			}
		}
		b := img.Bounds()
		w.addPage(float64(b.Dx())/dpi*72, float64(b.Dy())/dpi*72, img, jpegData)
		progress.Report(float64(i+1) / float64(len(pages)))
	}
	if len(w.pages) == 0 {
		return errors.New("the comic contains no readable pictures")
	}
	return w.save(output)
}

// jfifDensity is the dots per inch a JPEG's JFIF header states, 0 when none.
func jfifDensity(b []byte) float64 {
	if len(b) < 18 || b[2] != 0xFF || b[3] != 0xE0 || string(b[6:11]) != "JFIF\x00" {
		return 0
	}
	x := float64(int(b[14])<<8 | int(b[15]))
	switch b[13] {
	case 1:
		return x
	case 2:
		return x * 2.54
	}
	return 0
}

// pdfWriter is a minimal PDF writer for picture pages.
type pdfWriter struct {
	objects [][]byte
	pages   []int
}

func (w *pdfWriter) add(obj []byte) int {
	w.objects = append(w.objects, obj)
	return len(w.objects)
}

func (w *pdfWriter) stream(dict string, data []byte) int {
	var b bytes.Buffer
	fmt.Fprintf(&b, "<<%s/Length %d>>\nstream\n", dict, len(data))
	b.Write(data)
	b.WriteString("\nendstream")
	return w.add(b.Bytes())
}

func deflateBytes(data []byte) []byte {
	var b bytes.Buffer
	z, _ := zlib.NewWriterLevel(&b, zlib.BestCompression)
	z.Write(data)
	z.Close()
	return b.Bytes()
}

func (w *pdfWriter) addPage(width, height float64, img image.Image, jpegData []byte) {
	bounds := img.Bounds()
	var imageObj int
	if jpegData != nil {
		cs := "/DeviceRGB"
		if jpegComponents(jpegData) == 1 {
			cs = "/DeviceGray"
		} else if jpegComponents(jpegData) == 4 {
			cs = "/DeviceCMYK/Decode[1 0 1 0 1 0 1 0]"
		}
		imageObj = w.stream(fmt.Sprintf("/Type/XObject/Subtype/Image/Width %d/Height %d/ColorSpace%s/BitsPerComponent 8/Filter/DCTDecode", bounds.Dx(), bounds.Dy(), cs), jpegData)
	} else {
		rgba := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
		draw.Draw(rgba, rgba.Bounds(), img, bounds.Min, draw.Src)
		rgb := make([]byte, 0, bounds.Dx()*bounds.Dy()*3)
		alpha := make([]byte, 0, bounds.Dx()*bounds.Dy())
		hasAlpha := false
		for i := 0; i < len(rgba.Pix); i += 4 {
			rgb = append(rgb, rgba.Pix[i], rgba.Pix[i+1], rgba.Pix[i+2])
			alpha = append(alpha, rgba.Pix[i+3])
			if rgba.Pix[i+3] != 255 {
				hasAlpha = true
			}
		}
		mask := ""
		if hasAlpha {
			m := w.stream(fmt.Sprintf("/Type/XObject/Subtype/Image/Width %d/Height %d/ColorSpace/DeviceGray/BitsPerComponent 8/Filter/FlateDecode", bounds.Dx(), bounds.Dy()), deflateBytes(alpha))
			mask = fmt.Sprintf("/SMask %d 0 R", m)
		}
		imageObj = w.stream(fmt.Sprintf("/Type/XObject/Subtype/Image/Width %d/Height %d/ColorSpace/DeviceRGB/BitsPerComponent 8/Filter/FlateDecode%s", bounds.Dx(), bounds.Dy(), mask), deflateBytes(rgb))
	}
	content := w.stream("", []byte(fmt.Sprintf("q %s 0 0 %s 0 0 cm /Im0 Do Q", num(width), num(height))))
	page := w.add([]byte(fmt.Sprintf("<</Type/Page/Parent 1 0 R/MediaBox[0 0 %s %s]/Resources<</XObject<</Im0 %d 0 R>>>>/Contents %d 0 R>>", num(width), num(height), imageObj, content)))
	w.pages = append(w.pages, page)
}

func jpegComponents(b []byte) int {
	for i := 2; i+9 < len(b); {
		if b[i] != 0xFF {
			return 3
		}
		marker := b[i+1]
		length := int(b[i+2])<<8 | int(b[i+3])
		if marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC {
			return int(b[i+9])
		}
		i += 2 + length
	}
	return 3
}

func num(v float64) string {
	s := strconv.FormatFloat(v, 'f', 3, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" {
		return "0"
	}
	return s
}

func (w *pdfWriter) save(output string) error {
	kids := make([]string, len(w.pages))
	for i, p := range w.pages {
		kids[i] = fmt.Sprintf("%d 0 R", p)
	}
	// Objects 1 (the page tree) and 2 (the catalog) were reserved by newPdfWriter.
	w.objects[0] = []byte(fmt.Sprintf("<</Type/Pages/Kids[%s]/Count %d>>", strings.Join(kids, " "), len(w.pages)))
	w.objects[1] = []byte("<</Type/Catalog/Pages 1 0 R>>")
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n")
	offsets := make([]int, len(w.objects))
	for i, o := range w.objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n", i+1)
		b.Write(o)
		b.WriteString("\nendobj\n")
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(w.objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<</Size %d/Root 2 0 R>>\nstartxref\n%d\n%%%%EOF\n", len(w.objects)+1, xref)
	temp := output + ".tmp"
	if err := os.WriteFile(temp, b.Bytes(), 0o644); err != nil {
		return err
	}
	os.Remove(output)
	return os.Rename(temp, output)
}
