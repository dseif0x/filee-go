package svg

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
)

// maxNesting limits SVG images inside SVG images.
const maxNesting = 4

// loadHref returns the bytes an href points to: a data: URL, or a file in the
// drawing's folder or below it. Other places (../, absolute paths, the web)
// are refused, so converting a file never reads unrelated files or fetches
// anything.
func (d *Document) loadHref(href string) ([]byte, string, bool) {
	href = strings.TrimSpace(href)
	if strings.HasPrefix(href, "data:") {
		comma := strings.IndexByte(href, ',')
		if comma < 0 {
			return nil, "", false
		}
		meta := href[5:comma]
		payload := href[comma+1:]
		mime := strings.ToLower(strings.SplitN(meta, ";", 2)[0])
		if strings.HasSuffix(strings.ToLower(meta), ";base64") {
			clean := strings.Map(func(r rune) rune {
				if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
					return -1
				}
				return r
			}, payload)
			if u, err := url.PathUnescape(clean); err == nil {
				clean = u
			}
			data, err := base64.StdEncoding.DecodeString(clean)
			if err != nil {
				data, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(clean, "="))
				if err != nil {
					return nil, "", false
				}
			}
			return data, mime, true
		}
		u, err := url.PathUnescape(payload)
		if err != nil {
			u = payload
		}
		return []byte(u), mime, true
	}
	if d.BaseDir == "" || strings.Contains(href, "://") || strings.HasPrefix(href, "//") {
		return nil, "", false
	}
	if strings.HasPrefix(strings.ToLower(href), "file:") {
		return nil, "", false
	}
	if i := strings.IndexAny(href, "?#"); i >= 0 {
		href = href[:i]
	}
	if u, err := url.PathUnescape(href); err == nil {
		href = u
	}
	if filepath.IsAbs(href) || strings.HasPrefix(href, "/") || strings.HasPrefix(href, `\`) || filepath.VolumeName(href) != "" {
		return nil, "", false
	}
	clean := filepath.Clean(filepath.FromSlash(href))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, "", false
	}
	full := filepath.Join(d.BaseDir, clean)
	rel, err := filepath.Rel(d.BaseDir, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil, "", false
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, "", false
	}
	ext := strings.ToLower(filepath.Ext(full))
	mime := ""
	if ext == ".svg" || ext == ".svgz" {
		mime = "image/svg+xml"
	}
	return data, mime, true
}

func isSVGData(data []byte, mime string) bool {
	if mime == "image/svg+xml" {
		return true
	}
	head := data
	if len(head) > 2048 {
		head = head[:2048]
	}
	return bytes.Contains(bytes.ToLower(head), []byte("<svg"))
}

func (r *renderer) drawImage(n *Node, c context) {
	if !c.st.visible {
		return
	}
	data, mime, ok := r.doc.loadHref(n.Attr("href"))
	if !ok || len(data) == 0 {
		return
	}
	if len(data) > 2 && data[0] == 0x1f && data[1] == 0x8b {
		if plain, err := Gunzip(data); err == nil {
			data = plain
		}
	}
	var img image.Image
	var sub *Document
	var iw, ih float64
	if isSVGData(data, mime) {
		if r.doc.depth >= maxNesting {
			return
		}
		doc, err := parse(data, r.doc.BaseDir, r.doc.depth+1)
		if err != nil {
			return
		}
		doc.Fonts = r.doc.Fonts
		sub = doc
		iw, ih = doc.Width, doc.Height
	} else {
		im, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return
		}
		img = im
		iw, ih = float64(im.Bounds().Dx()), float64(im.Bounds().Dy())
	}
	if iw <= 0 || ih <= 0 {
		return
	}
	x := c.lc.resolveAttr(n, "x", axisX, 0)
	y := c.lc.resolveAttr(n, "y", axisY, 0)
	w := c.lc.resolveAttr(n, "width", axisX, -1)
	h := c.lc.resolveAttr(n, "height", axisY, -1)
	switch {
	case w < 0 && h < 0:
		w, h = iw, ih
	case w < 0:
		w = h * iw / ih
	case h < 0:
		h = w * ih / iw
	}
	if w <= 0 || h <= 0 {
		return
	}
	par := n.Attr("preserveAspectRatio")
	vbt := viewBoxTransform([4]float64{0, 0, iw, ih}, par, x, y, w, h)
	if strings.Contains(par, "slice") || sub != nil {
		clip := gfx.NewPath()
		clip.Rect(x, y, w, h)
		r.dev.PushClip(clip, c.m, gfx.NonZero)
		defer r.dev.PopClip()
	}
	m := c.m.Mul(vbt)
	if sub != nil {
		sr := &renderer{doc: sub, dev: r.dev, fonts: sub.fonts(), active: map[*Node]int{}, rootScale: m.MeanScale(), filterRes: r.filterRes}
		sr.drawChildren(sub.root, sr.rootContext(m))
		return
	}
	r.dev.DrawImage(img, m, 1)
}
