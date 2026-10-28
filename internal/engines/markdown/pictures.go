package markdown

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	"github.com/dseif0x/filee-go/internal/convert"
)

// FolderSuffix names the picture folder of a Markdown file: "report.md" → "report_files".
const FolderSuffix = "_files"

// Pictures writes the pictures of one Markdown output into its "_files"
// folder (a Markdown file can't hold pictures) and hands out relative links,
// which every editor shows and which keep working when both are moved.
type Pictures struct {
	markdown string
	output   convert.OutputAllocator
	links    map[string]string
	folder   string
	skipped  bool
}

// NewPictures: markdown is the file being written, output the allocator that handed it out.
func NewPictures(markdown string, output convert.OutputAllocator) *Pictures {
	return &Pictures{markdown: markdown, output: output, links: map[string]string{}}
}

// Add copies the picture at path into the folder (formats editors don't
// show, such as BMP, TIFF or WebP, as PNG) and returns its relative link; ""
// when it can't be read or the folder is skipped. A picture used twice is
// written once.
func (p *Pictures) Add(path string) string {
	key := strings.ToLower(path)
	if l, ok := p.links[key]; ok {
		return l
	}
	ext := strings.ToLower(filepath.Ext(path))
	convertToPNG := false
	switch ext {
	case ".png", ".gif", ".svg":
	case ".jpg", ".jpeg":
		ext = ".jpg"
	default:
		convertToPNG, ext = true, ".png"
	}
	folder := p.ensureFolder()
	if folder == "" {
		return ""
	}
	name := fmt.Sprintf("img%04d%s", len(p.links)+1, ext)
	target := filepath.Join(folder, name)
	if err := copyPicture(path, target, convertToPNG); err != nil {
		return ""
	}
	link := url.PathEscape(filepath.Base(folder)) + "/" + name
	p.links[key] = link
	return link
}

func copyPicture(from, to string, toPNG bool) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if toPNG {
		img, _, derr := image.Decode(in)
		if derr == nil {
			derr = png.Encode(out, img)
		}
		out.Close()
		if derr != nil {
			os.Remove(to)
		}
		return derr
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// ensureFolder is created on the first picture, reserved through the
// allocator ("report_files", "report_files (2)" or skipped). An intermediate
// Markdown file keeps it right beside itself, so the next step finds it.
func (p *Pictures) ensureFolder() string {
	if p.folder != "" || p.skipped {
		return p.folder
	}
	abs, _ := filepath.Abs(p.markdown)
	beside := filepath.Dir(abs)
	folder := p.output.Allocate("", FolderSuffix)
	if folder != "" {
		if fa, _ := filepath.Abs(folder); !strings.EqualFold(filepath.Dir(fa), beside) {
			folder = filepath.Join(beside, strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))+FolderSuffix)
		}
	}
	if folder == "" || os.MkdirAll(folder, 0o755) != nil {
		p.skipped = true
		return ""
	}
	p.folder = folder
	return folder
}
