// Makes a PDF smaller while it stays a PDF. Pictures drawn at more than the
// level's resolution are scaled down to it, photos are saved as JPEG at the
// level's quality and pictures with few colours (charts, screenshots, logos)
// losslessly; every other stream is deflated at the highest level; page
// thumbnails, other applications' private data and, when the preset drops
// metadata, the XMP metadata of pages and pictures are left out. Text, fonts
// and vector drawings stay as they are. A picture is changed only when the
// result is smaller.

package pdf

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/dseif0x/filee-go/internal/presets"
)

type pdfContext = model.Context

const (
	// Pictures are scaled down only when drawn at more than this many times
	// the level's resolution, like Ghostscript does.
	downsampleThreshold = 1.5
	minimumSide         = 8
	// A JPEG that keeps its size is encoded again only for this much saving.
	minimumJpegSaving = 0.1
	graphicColors     = 256
	graphicGrays      = 32
)

// compressionSettings: the resolution pictures are scaled down to and the JPEG quality.
func compressionSettings(level presets.PdfCompression) (dpi, quality int) {
	switch level {
	case presets.PdfCompressScreen:
		return 72, 55
	case presets.PdfCompressPrint:
		return 300, 85
	}
	return 150, 65
}

// compressContext shrinks a document in place; write it afterwards.
func compressContext(ctx context.Context, pc *pdfContext, level presets.PdfCompression, keepMetadata bool) error {
	dpi, quality := compressionSettings(level)
	uses := findPictureUses(ctx, pc)
	nrs := make([]int, 0, len(uses))
	for nr := range uses {
		nrs = append(nrs, nr)
	}
	sort.Ints(nrs)
	done := map[int]bool{}
	for _, nr := range nrs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !done[nr] {
			done[nr] = true
			shrinkPicture(pc, nr, uses[nr], dpi, quality, done)
		}
	}
	mergeDuplicatePictures(pc)

	for n := 1; n <= pc.PageCount; n++ {
		if d, _, _, err := pc.PageDict(ctx, n, false); err == nil {
			d.Delete("Thumb")
			d.Delete("PieceInfo")
		}
	}
	if root, err := pc.Catalog(); err == nil {
		root.Delete("PieceInfo")
		if !keepMetadata {
			root.Delete("Metadata")
		}
	}
	for nr, e := range pc.Table {
		if err := ctx.Err(); err != nil {
			return err
		}
		if e == nil || e.Free || e.Object == nil {
			continue
		}
		switch o := e.Object.(type) {
		case types.Dict:
			if !keepMetadata {
				o.Delete("Metadata")
			}
		case types.StreamDict:
			if !keepMetadata {
				o.Delete("Metadata")
			}
			deflateStream(&o)
			pc.Table[nr].Object = o
		}
	}
	pc.WriteObjectStream = true
	pc.WriteXRefStream = true
	return nil
}

func setRaw(sd *types.StreamDict, data []byte) {
	sd.Raw = data
	sd.Content = nil
	l := int64(len(data))
	sd.StreamLength = &l
	sd.StreamLengthObjNr = nil
	sd.Dict["Length"] = types.Integer(len(data))
}

func filterNames(sd *types.StreamDict) []string {
	var out []string
	for _, f := range sd.FilterPipeline {
		out = append(out, f.Name)
	}
	return out
}

func decodeParms(sd *types.StreamDict) types.Dict {
	if len(sd.FilterPipeline) == 1 {
		return sd.FilterPipeline[0].DecodeParms
	}
	return nil
}

// deflateStream deflates an uncompressed stream, or a Flate stream again at
// the highest level, when that is smaller.
func deflateStream(sd *types.StreamDict) {
	if t := sd.Type(); t != nil && (*t == "Metadata" || *t == "ObjStm" || *t == "XRef") {
		return
	}
	if _, ok := sd.Dict["F"]; ok || len(sd.Raw) < 64 {
		return
	}
	filters := filterNames(sd)
	switch {
	case len(filters) == 0:
		if packed := deflate(sd.Raw); len(packed) < len(sd.Raw) {
			setRaw(sd, packed)
			sd.Dict["Filter"] = types.Name("FlateDecode")
			sd.FilterPipeline = []types.PDFFilter{{Name: "FlateDecode"}}
		}
	case slices.Equal(filters, []string{"FlateDecode"}):
		// The data stays the same (predictor included), only packed tighter.
		if raw, err := inflate(sd.Raw); err == nil {
			if packed := deflate(raw); len(packed) < len(sd.Raw) {
				setRaw(sd, packed)
			}
		}
	case slices.Equal(filters, []string{"RunLengthDecode"}):
		if packed := deflate(undoRunLength(sd.Raw)); len(packed) < len(sd.Raw) {
			setRaw(sd, packed)
			sd.Dict["Filter"] = types.Name("FlateDecode")
			sd.FilterPipeline = []types.PDFFilter{{Name: "FlateDecode"}}
		}
	}
}

func inflate(data []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, 1<<31))
}

// undoRunLength decodes RunLengthDecode.
func undoRunLength(data []byte) []byte {
	out := make([]byte, 0, len(data)*2)
	for i := 0; i < len(data); {
		n := int(data[i])
		i++
		if n == 128 {
			break
		}
		if n < 128 {
			c := min(n+1, len(data)-i)
			out = append(out, data[i:i+c]...)
			i += c
		} else if i < len(data) {
			for k := 0; k < 257-n; k++ {
				out = append(out, data[i])
			}
			i++
		}
	}
	return out
}


// colourComponents of a colour space Filee re-encodes (gray, RGB, CMYK and
// their calibrated / ICC forms), else 0.
func colourComponents(pc *pdfContext, o types.Object) int {
	o, _ = pc.Dereference(o)
	switch v := o.(type) {
	case types.Name:
		switch v {
		case "DeviceGray":
			return 1
		case "DeviceRGB":
			return 3
		case "DeviceCMYK":
			return 4
		}
	case types.Array:
		if len(v) == 0 {
			return 0
		}
		first, _ := pc.Dereference(v[0])
		n, _ := first.(types.Name)
		switch n {
		case "CalGray":
			return 1
		case "CalRGB":
			return 3
		case "ICCBased":
			if len(v) > 1 {
				if sd, _, err := pc.DereferenceStreamDict(v[1]); err == nil && sd != nil {
					if c := sd.IntEntry("N"); c != nil && (*c == 1 || *c == 3 || *c == 4) {
						return *c
					}
				}
			}
		}
	}
	return 0
}

type decodedPicture struct {
	img        image.Image
	components int
	wasJpeg    bool
}

func intEntry(pc *pdfContext, d types.Dict, key string) int {
	o, err := pc.Dereference(d[key])
	if err != nil {
		return 0
	}
	switch v := o.(type) {
	case types.Integer:
		return int(v)
	case types.Float:
		return int(v)
	}
	return 0
}

// decodePicture returns the pixels of a picture Filee can encode again
// without changing how it looks: 8 bits, gray, RGB or CMYK (CMYK not from
// JPEG), stored as JPEG, Flate or uncompressed. Nil for everything else.
func decodePicture(pc *pdfContext, sd *types.StreamDict) *decodedPicture {
	d := sd.Dict
	if s := d.NameEntry("Subtype"); s == nil || *s != "Image" {
		return nil
	}
	if b := d.BooleanEntry("ImageMask"); b != nil && *b {
		return nil
	}
	if intEntry(pc, d, "BitsPerComponent") != 8 {
		return nil
	}
	if m, _ := pc.Dereference(d["Mask"]); m != nil {
		if _, isArray := m.(types.Array); isArray {
			return nil
		}
	}
	if _, ok := d["F"]; ok {
		return nil
	}
	if _, ok := d["Decode"]; ok {
		return nil
	}
	width, height := intEntry(pc, d, "Width"), intEntry(pc, d, "Height")
	components := colourComponents(pc, d["ColorSpace"])
	if width <= 0 || height <= 0 || components == 0 || int64(width)*int64(height) > 100_000_000 {
		return nil
	}
	filters := filterNames(sd)
	data := sd.Raw
	if slices.Equal(filters, []string{"DCTDecode"}) {
		if components == 4 {
			return nil
		}
		if p := decodeParms(sd); p != nil {
			if _, ok := p["ColorTransform"]; ok {
				return nil
			}
		}
		img, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			return nil
		}
		_, gray := img.(*image.Gray)
		if _, cmyk := img.(*image.CMYK); cmyk || img.Bounds().Dx() != width || img.Bounds().Dy() != height || gray != (components == 1) {
			return nil
		}
		return &decodedPicture{img, components, true}
	}
	var raw []byte
	switch {
	case len(filters) == 0:
		raw = data
	case slices.Equal(filters, []string{"RunLengthDecode"}):
		raw = undoRunLength(data)
	case slices.Equal(filters, []string{"FlateDecode"}):
		var err error
		if raw, err = inflate(data); err != nil {
			return nil
		}
		if p := decodeParms(sd); p != nil {
			predictor := intEntry(pc, p, "Predictor")
			if predictor > 1 {
				colors, columns, bits := 1, 1, 8
				if _, ok := p["Colors"]; ok {
					colors = intEntry(pc, p, "Colors")
				}
				if _, ok := p["Columns"]; ok {
					columns = intEntry(pc, p, "Columns")
				}
				if _, ok := p["BitsPerComponent"]; ok {
					bits = intEntry(pc, p, "BitsPerComponent")
				}
				if predictor < 10 || colors != components || columns != width || bits != 8 {
					return nil
				}
				if raw = undoPngPredictor(raw, width*components, height, components); raw == nil {
					return nil
				}
			}
		}
	default:
		return nil
	}
	size := width * height * components
	if len(raw) < size {
		return nil
	}
	raw = raw[:size]
	r := image.Rect(0, 0, width, height)
	switch components {
	case 1:
		return &decodedPicture{&image.Gray{Pix: raw, Stride: width, Rect: r}, 1, false}
	case 3:
		rgba := image.NewNRGBA(r)
		for i, j := 0, 0; i < size; i, j = i+3, j+4 {
			rgba.Pix[j], rgba.Pix[j+1], rgba.Pix[j+2], rgba.Pix[j+3] = raw[i], raw[i+1], raw[i+2], 0xFF
		}
		return &decodedPicture{rgba, 3, false}
	default:
		return &decodedPicture{&image.CMYK{Pix: raw, Stride: width * 4, Rect: r}, 4, false}
	}
}

// totalColors counts distinct colours up to limit+1.
func totalColors(img image.Image, limit int) int {
	seen := map[uint32]bool{}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			seen[(r>>8)<<16|(g>>8)<<8|bl>>8] = true
			if len(seen) > limit {
				return len(seen)
			}
		}
	}
	return len(seen)
}

func rawComponents(img image.Image, components int) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	out := make([]byte, 0, w*h*components)
	switch components {
	case 1:
		g := image.NewGray(image.Rect(0, 0, w, h))
		drawSrc(g, img)
		return g.Pix
	case 4:
		if c, ok := img.(*image.CMYK); ok && c.Stride == w*4 {
			return c.Pix
		}
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				c := color.CMYKModel.Convert(img.At(x, y)).(color.CMYK)
				out = append(out, c.C, c.M, c.Y, c.K)
			}
		}
		return out
	}
	n := toNRGBA(img)
	for i := 0; i < len(n.Pix); i += 4 {
		out = append(out, n.Pix[i], n.Pix[i+1], n.Pix[i+2])
	}
	return out
}

// resizePicture scales any kind of picture, keeping CMYK as CMYK.
func resizePicture(img image.Image, w, h int) image.Image {
	if c, ok := img.(*image.CMYK); ok {
		// Scale each ink as a gray plane.
		b := c.Bounds()
		out := image.NewCMYK(image.Rect(0, 0, w, h))
		for ch := 0; ch < 4; ch++ {
			plane := image.NewGray(b)
			for i := 0; i < b.Dx()*b.Dy(); i++ {
				plane.Pix[i] = c.Pix[i*4+ch]
			}
			s := scaleTo(plane, w, h)
			g := image.NewGray(image.Rect(0, 0, w, h))
			drawSrc(g, s)
			for i := 0; i < w*h; i++ {
				out.Pix[i*4+ch] = g.Pix[i]
			}
		}
		return out
	}
	if g, ok := img.(*image.Gray); ok {
		s := scaleTo(g, w, h)
		out := image.NewGray(image.Rect(0, 0, w, h))
		drawSrc(out, s)
		return out
	}
	return scaleTo(img, w, h)
}

// shrinkPicture scales a picture down and saves it as JPEG or Flate, when
// that's smaller. Its soft mask follows it.
func shrinkPicture(pc *pdfContext, nr int, use pictureUse, dpi, quality int, done map[int]bool) {
	e := pc.Table[nr]
	if e == nil {
		return
	}
	sd, ok := e.Object.(types.StreamDict)
	if !ok {
		return
	}
	pic := decodePicture(pc, &sd)
	if pic == nil {
		return
	}
	b := pic.img.Bounds()
	width, height := b.Dx(), b.Dy()
	scale := max(use.width*float64(dpi)/float64(width), use.height*float64(dpi)/float64(height))
	downsample := scale*downsampleThreshold < 1
	newW, newH := width, height
	if downsample {
		newW = max(min(width, minimumSide), min(width, int(math.Round(float64(width)*scale))))
		newH = max(min(height, minimumSide), min(height, int(math.Round(float64(height)*scale))))
	}
	downsample = newW < width || newH < height
	img := pic.img
	if downsample {
		img = resizePicture(img, newW, newH)
	}
	original := len(sd.Raw)
	limit := original
	if !downsample && pic.wasJpeg {
		limit = int(float64(original) * (1 - minimumJpegSaving))
	}
	limitColors := graphicColors
	if pic.components == 1 {
		limitColors = graphicGrays
	}
	graphic := !pic.wasJpeg && totalColors(img, limitColors) <= limitColors
	var data []byte
	var filter string
	var parms types.Dict
	if pic.components == 4 || graphic {
		data = deflate(pngPredict(rawComponents(img, pic.components), newW*pic.components, newH, pic.components))
		filter = "FlateDecode"
		parms = types.Dict{"Predictor": types.Integer(15), "Colors": types.Integer(pic.components),
			"BitsPerComponent": types.Integer(8), "Columns": types.Integer(newW)}
	} else {
		var src image.Image = img
		if pic.components == 1 {
			g := image.NewGray(image.Rect(0, 0, newW, newH))
			drawSrc(g, img)
			src = g
		} else if _, isGray := img.(*image.Gray); isGray {
			n := image.NewNRGBA(image.Rect(0, 0, newW, newH))
			drawSrc(n, img)
			src = n
		}
		data = encodeJpegBytes(src, quality)
		if data == nil || jpegComponents(data) != pic.components {
			return
		}
		filter = "DCTDecode"
	}
	if len(data) >= limit {
		return
	}
	setRaw(&sd, data)
	sd.Dict["Filter"] = types.Name(filter)
	if parms != nil {
		sd.Dict["DecodeParms"] = parms
	} else {
		delete(sd.Dict, "DecodeParms")
	}
	sd.FilterPipeline = []types.PDFFilter{{Name: filter, DecodeParms: parms}}
	sd.Dict["Width"] = types.Integer(newW)
	sd.Dict["Height"] = types.Integer(newH)
	sd.Dict["BitsPerComponent"] = types.Integer(8)
	pc.Table[nr].Object = sd

	if ref, ok := sd.Dict["SMask"].(types.IndirectRef); ok && downsample {
		mnr := ref.ObjectNumber.Value()
		if !done[mnr] {
			done[mnr] = true
			shrinkMask(pc, mnr, float64(newW)/float64(width), float64(newH)/float64(height))
		}
	}
}

func shrinkMask(pc *pdfContext, nr int, sx, sy float64) {
	e := pc.Table[nr]
	if e == nil {
		return
	}
	sd, ok := e.Object.(types.StreamDict)
	if !ok {
		return
	}
	pic := decodePicture(pc, &sd)
	if pic == nil || pic.components != 1 || pic.wasJpeg {
		return
	}
	b := pic.img.Bounds()
	w := max(1, int(math.Round(float64(b.Dx())*sx)))
	h := max(1, int(math.Round(float64(b.Dy())*sy)))
	img := resizePicture(pic.img, w, h)
	data := deflate(pngPredict(rawComponents(img, 1), w, h, 1))
	if len(data) >= len(sd.Raw) {
		return
	}
	setRaw(&sd, data)
	parms := types.Dict{"Predictor": types.Integer(15), "Colors": types.Integer(1), "BitsPerComponent": types.Integer(8), "Columns": types.Integer(w)}
	sd.Dict["Filter"] = types.Name("FlateDecode")
	sd.Dict["DecodeParms"] = parms
	sd.FilterPipeline = []types.PDFFilter{{Name: "FlateDecode", DecodeParms: parms}}
	sd.Dict["Width"] = types.Integer(w)
	sd.Dict["Height"] = types.Integer(h)
	sd.Dict["BitsPerComponent"] = types.Integer(8)
	pc.Table[nr].Object = sd
}

// mergeDuplicatePictures keeps pictures stored more than once with the same
// data and settings (a logo embedded again on every page) once. Soft masks
// first, so the pictures using them compare equal afterwards.
func mergeDuplicatePictures(pc *pdfContext) {
	var plain, masked []int
	for nr, e := range pc.Table {
		if e == nil || e.Free {
			continue
		}
		sd, ok := e.Object.(types.StreamDict)
		if !ok {
			continue
		}
		if s := sd.NameEntry("Subtype"); s == nil || *s != "Image" {
			continue
		}
		_, sm := sd.Dict["SMask"]
		_, m := sd.Dict["Mask"]
		if sm || m {
			masked = append(masked, nr)
		} else {
			plain = append(plain, nr)
		}
	}
	for _, pass := range [][]int{plain, masked} {
		sort.Ints(pass)
		first := map[string]int{}
		copies := map[int]int{}
		for _, nr := range pass {
			sd := pc.Table[nr].Object.(types.StreamDict)
			keys := make([]string, 0, len(sd.Dict))
			for k := range sd.Dict {
				if k != "Length" {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			var key strings.Builder
			for _, k := range keys {
				key.WriteString(k + " ")
				if v := sd.Dict[k]; v != nil {
					key.WriteString(v.PDFString())
				}
				key.WriteByte(' ')
			}
			sum := sha256.Sum256(sd.Raw)
			key.WriteString(hex.EncodeToString(sum[:]))
			if kept, ok := first[key.String()]; ok {
				copies[nr] = kept
			} else {
				first[key.String()] = nr
			}
		}
		if len(copies) == 0 {
			continue
		}
		for nr, e := range pc.Table {
			if e == nil || e.Free || e.Object == nil {
				continue
			}
			pc.Table[nr].Object = redirect(e.Object, copies, 0)
		}
	}
}

// redirect points references to copies at the kept picture, in direct objects too.
func redirect(o types.Object, copies map[int]int, depth int) types.Object {
	if depth > 32 {
		return o
	}
	switch v := o.(type) {
	case types.IndirectRef:
		if kept, ok := copies[v.ObjectNumber.Value()]; ok {
			return *types.NewIndirectRef(kept, 0)
		}
	case types.Dict:
		for k, x := range v {
			v[k] = redirect(x, copies, depth+1)
		}
	case types.StreamDict:
		for k, x := range v.Dict {
			v.Dict[k] = redirect(x, copies, depth+1)
		}
		return v
	case types.Array:
		for i, x := range v {
			v[i] = redirect(x, copies, depth+1)
		}
	}
	return o
}

// undoPngPredictor reverses PNG prediction (/Predictor 10–15).
func undoPngPredictor(data []byte, stride, rows, bpp int) []byte {
	if len(data) < (stride+1)*rows {
		return nil
	}
	out := make([]byte, stride*rows)
	none := make([]byte, stride)
	for r := 0; r < rows; r++ {
		typ := data[r*(stride+1)]
		src := data[r*(stride+1)+1 : r*(stride+1)+1+stride]
		row := out[r*stride : (r+1)*stride]
		up := none
		if r > 0 {
			up = out[(r-1)*stride : r*stride]
		}
		for x := 0; x < stride; x++ {
			var a, c int
			if x >= bpp {
				a, c = int(row[x-bpp]), int(up[x-bpp])
			}
			b := int(up[x])
			var p int
			switch typ {
			case 0:
			case 1:
				p = a
			case 2:
				p = b
			case 3:
				p = (a + b) / 2
			case 4:
				p = paeth(a, b, c)
			default:
				return nil
			}
			row[x] = src[x] + byte(p)
		}
	}
	return out
}

// pngPredict PNG-predicts rows, each with the filter leaving the smallest values.
func pngPredict(raw []byte, stride, rows, bpp int) []byte {
	out := make([]byte, (stride+1)*rows)
	cand := make([][]byte, 5)
	for i := range cand {
		cand[i] = make([]byte, stride)
	}
	none := make([]byte, stride)
	for r := 0; r < rows; r++ {
		row := raw[r*stride : (r+1)*stride]
		up := none
		if r > 0 {
			up = raw[(r-1)*stride : r*stride]
		}
		best, bestSum := 0, int64(math.MaxInt64)
		for t := 0; t < 5; t++ {
			var sum int64
			f := cand[t]
			for x := 0; x < stride; x++ {
				var a, c int
				if x >= bpp {
					a, c = int(row[x-bpp]), int(up[x-bpp])
				}
				b := int(up[x])
				var p int
				switch t {
				case 1:
					p = a
				case 2:
					p = b
				case 3:
					p = (a + b) / 2
				case 4:
					p = paeth(a, b, c)
				}
				v := row[x] - byte(p)
				f[x] = v
				if v < 128 {
					sum += int64(v)
				} else {
					sum += 256 - int64(v)
				}
			}
			if sum < bestSum {
				best, bestSum = t, sum
			}
		}
		out[r*(stride+1)] = byte(best)
		copy(out[r*(stride+1)+1:], cand[best])
	}
	return out
}

func paeth(a, b, c int) int {
	p := a + b - c
	pa, pb, pc := abs(p-a), abs(p-b), abs(p-c)
	if pa <= pb && pa <= pc {
		return a
	}
	if pb <= pc {
		return b
	}
	return c
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func encodeJpegBytes(img image.Image, quality int) []byte {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: max(1, min(100, quality))}); err != nil {
		return nil
	}
	return buf.Bytes()
}
