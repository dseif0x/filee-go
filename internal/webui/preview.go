package webui

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sync"

	_ "golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	"github.com/dseif0x/filee-go/internal/app"
	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/pdf"
	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/jobs"
	"github.com/dseif0x/filee-go/internal/presets"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// maxPreview is the longest side of a preview in pixels (as the C# tools).
const maxPreview = 2560

// converter runs the conversions behind previews on a queue of its own, so
// they never show up in the history or the job list.
type converter struct {
	a     *app.App
	once  sync.Once
	queue *jobs.Queue
	root  string

	cacheMu sync.Mutex
	cache   map[string]*previewResult
	docs    map[string]*pdf.PreviewDocument
	order   []string
}

type previewResult struct {
	PNG []byte
	// Width and Height are the source's own size (after EXIF orientation).
	Width, Height int
	Pages         int
}

func newConverter(a *app.App) *converter {
	return &converter{a: a, cache: map[string]*previewResult{}}
}

func (c *converter) q() *jobs.Queue {
	c.once.Do(func() {
		c.root = filepath.Join(os.TempDir(), "Filee", "webui-previews")
		c.queue = jobs.NewQueue(c.a.Catalog(), jobs.Options{WorkRoot: filepath.Join(c.root, "work")})
	})
	return c.queue
}

func (c *converter) close() {
	if c.queue != nil {
		c.queue.Close()
		_ = os.RemoveAll(c.root)
	}
}

// errNoRoute: no installed engine makes a picture of the file.
var errNoRoute = errors.New("no installed engine can show this file")

// convertTo converts src to target in a scratch folder and returns the
// results and a cleanup func. pageRange limits a document's pages ("3").
func (c *converter) convertTo(ctx context.Context, src, target, pageRange string) ([]string, func(), error) {
	f := formats.Detect(src)
	if f == nil {
		return nil, func() {}, errNoRoute
	}
	if c.a.Planner().Plan(f.ID, target, convert.Options{}) == nil {
		return nil, func() {}, errNoRoute
	}
	q := c.q()
	dir, err := os.MkdirTemp(mkdir(c.root), "p-")
	if err != nil {
		return nil, func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	p := presets.New("preview", target)
	p.Output.Location = presets.LocationCustomFolder
	p.Output.CustomFolder = dir
	p.Pdf.PageRange = pageRange
	p.Pdf.RenderDpi = 110
	p.Image.KeepMetadata = false
	j := jobs.NewJob([]string{src}, p, "preview", nil)
	stop := context.AfterFunc(ctx, j.Cancel)
	defer stop()
	q.Run(j)
	s := j.Snapshot()
	if len(s.Files) == 0 || s.Files[0].State != jobs.FileDone || len(s.Files[0].Outputs) == 0 {
		msg := "conversion failed"
		if len(s.Files) > 0 && s.Files[0].ErrorKey != "" {
			msg = c.a.Text(s.Files[0].ErrorKey)
			if s.Files[0].ErrorDetail != "" {
				msg += " (" + s.Files[0].ErrorDetail + ")"
			}
		}
		cleanup()
		return nil, func() {}, errors.New(msg)
	}
	return s.Files[0].Outputs, cleanup, nil
}

func mkdir(p string) string {
	_ = os.MkdirAll(p, 0o755)
	return p
}

// preview renders page (0-based; documents only) of path as a PNG at most
// maxDim pixels on its long side.
func (c *converter) preview(ctx context.Context, path string, page, maxDim int) (*previewResult, error) {
	if maxDim <= 0 || maxDim > maxPreview {
		maxDim = maxPreview
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%s|%d|%d|%d|%d", path, fi.Size(), fi.ModTime().UnixNano(), page, maxDim)
	c.cacheMu.Lock()
	if r, ok := c.cache[key]; ok {
		c.cacheMu.Unlock()
		return r, nil
	}
	c.cacheMu.Unlock()

	r, err := c.render(ctx, path, page, maxDim)
	if err != nil {
		return nil, err
	}
	c.cacheMu.Lock()
	c.cache[key] = r
	c.order = append(c.order, key)
	// Previews of a big batch shouldn't pile up: keep the latest 64.
	for len(c.order) > 64 {
		delete(c.cache, c.order[0])
		c.order = c.order[1:]
	}
	c.cacheMu.Unlock()
	return r, nil
}

func (c *converter) render(ctx context.Context, path string, page, maxDim int) (*previewResult, error) {
	f := formats.Detect(path)
	if f != nil && f.ID == "pdf" {
		doc, err := c.pdfDoc(path)
		if err != nil {
			return nil, err
		}
		pp, err := doc.Preview(ctx, page, maxDim)
		if err != nil {
			return nil, err
		}
		img, err := png.Decode(bytes.NewReader(pp.Png))
		if err != nil {
			return nil, err
		}
		r := encodePreview(img, maxDim)
		r.PNG = pp.Png
		r.Pages = doc.PageCount()
		// The page's own size: in points, the crop works in parts of it.
		w, h := doc.PageSize(page)
		r.Width, r.Height = int(w+0.5), int(h+0.5)
		return r, nil
	}
	img, err := decodeFile(path)
	if err != nil {
		// Formats Go can't read (RAW, PSD, SVG, …) come through the engines.
		outs, cleanup, cerr := c.convertTo(ctx, path, "png", "1")
		if cerr != nil {
			return nil, fmt.Errorf("%w (%v)", cerr, err)
		}
		defer cleanup()
		if img, err = decodeFile(outs[0]); err != nil {
			return nil, err
		}
	}
	r := encodePreview(img, maxDim)
	r.Pages = 1
	return r, nil
}

// pdfDoc opens a PDF for previews once (its pages are rendered from memory).
func (c *converter) pdfDoc(path string) (*pdf.PreviewDocument, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%s|%d|%d", path, fi.Size(), fi.ModTime().UnixNano())
	c.cacheMu.Lock()
	if c.docs == nil {
		c.docs = map[string]*pdf.PreviewDocument{}
	}
	d := c.docs[key]
	c.cacheMu.Unlock()
	if d != nil {
		return d, nil
	}
	d, err = pdf.OpenPreview(path)
	if err != nil {
		return nil, err
	}
	c.cacheMu.Lock()
	if len(c.docs) > 8 {
		clear(c.docs)
	}
	c.docs[key] = d
	c.cacheMu.Unlock()
	return d, nil
}

func encodePreview(img image.Image, maxDim int) *previewResult {
	b := img.Bounds()
	r := &previewResult{Width: b.Dx(), Height: b.Dy()}
	small := scaleDown(img, maxDim)
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	_ = enc.Encode(&buf, small)
	r.PNG = buf.Bytes()
	return r
}

// scaleDown returns img at most maxDim on its long side.
func scaleDown(img image.Image, maxDim int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxDim && h <= maxDim {
		return img
	}
	scale := float64(maxDim) / float64(max(w, h))
	nw, nh := max(1, int(float64(w)*scale+0.5)), max(1, int(float64(h)*scale+0.5))
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}

// decodeFile reads a picture Go can decode, turned upright by its EXIF orientation.
func decodeFile(path string) (image.Image, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	return orient(img, exifOrientation(b)), nil
}

// imageSize is a picture's upright size without decoding all of it, ok false when Go can't read it.
func imageSize(path string) (int, int, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	head := make([]byte, 256<<10)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	cfg, _, err := image.DecodeConfig(bytes.NewReader(head))
	if err != nil {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return 0, 0, false
		}
		if cfg, _, err = image.DecodeConfig(f); err != nil {
			return 0, 0, false
		}
	}
	if o := exifOrientation(head); o >= 5 && o <= 8 {
		return cfg.Height, cfg.Width, true
	}
	return cfg.Width, cfg.Height, true
}

// exifOrientation is the EXIF orientation (1–8) of a JPEG, 1 when it has none.
func exifOrientation(b []byte) int {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(b) {
		if b[i] != 0xFF {
			return 1
		}
		marker := b[i+1]
		if marker == 0xDA || marker == 0xD9 {
			return 1
		}
		size := int(binary.BigEndian.Uint16(b[i+2:]))
		if size < 2 || i+2+size > len(b) {
			return 1
		}
		seg := b[i+4 : i+2+size]
		if marker == 0xE1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return tiffOrientation(seg[6:])
		}
		i += 2 + size
	}
	return 1
}

func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	off := int(bo.Uint32(t[4:]))
	if off+2 > len(t) {
		return 1
	}
	n := int(bo.Uint16(t[off:]))
	for k := 0; k < n; k++ {
		e := off + 2 + 12*k
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:]) == 0x0112 {
			v := int(bo.Uint16(t[e+8:]))
			if v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}

// orient turns img upright for EXIF orientation o.
func orient(img image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var nx, ny int
			switch o {
			case 2:
				nx, ny = w-1-x, y
			case 3:
				nx, ny = w-1-x, h-1-y
			case 4:
				nx, ny = x, h-1-y
			case 5:
				nx, ny = y, x
			case 6:
				nx, ny = h-1-y, x
			case 7:
				nx, ny = h-1-y, w-1-x
			case 8:
				nx, ny = y, w-1-x
			}
			dst.Set(nx, ny, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func pdfPageCount(path string) (int, error) {
	return api.PageCountFile(context.Background(), path)
}
