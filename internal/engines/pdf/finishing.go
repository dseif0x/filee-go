// The last touch on a PDF a preset writes (PdfOptions.FinishesPdf): a
// watermark across every page, page numbers at the bottom, and a password it
// opens with. Text is drawn as glyph outlines (glyphs.go), so no font is
// embedded; the password encrypts with AES-256. Runs after whichever engine
// made the PDF, once for a merged PDF.

package pdf

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/dseif0x/filee-go/internal/presets"
)

// MaxPasswordBytes is the most UTF-8 bytes of a password readers use (AES-256, revision 6).
const MaxPasswordBytes = 127

const pointsPerMm = 72 / 25.4

// affine maps (x, y) to (a·x + c·y + e, b·x + d·y + f).
type affine struct{ a, b, c, d, e, f float64 }

func (m affine) apply(x, y float64) (float64, float64) {
	return m.a*x + m.c*y + m.e, m.b*x + m.d*y + m.f
}

// then is m applied first, then o.
func (m affine) then(o affine) affine {
	return affine{
		m.a*o.a + m.b*o.c, m.a*o.b + m.b*o.d,
		m.c*o.a + m.d*o.c, m.c*o.b + m.d*o.d,
		m.e*o.a + m.f*o.c + o.e, m.e*o.b + m.f*o.d + o.f,
	}
}

func translate(x, y float64) affine { return affine{1, 0, 0, 1, x, y} }
func scaleBy(s float64) affine      { return affine{s, 0, 0, s, 0, 0} }
func rotateBy(rad float64) affine {
	s, c := math.Sin(rad), math.Cos(rad)
	return affine{c, s, -s, c, 0, 0}
}

// FinishPdf rewrites path with the watermark, page numbers and password options ask for.
func FinishPdf(ctx context.Context, path string, options *presets.PdfOptions) error {
	if len(options.Password) > MaxPasswordBytes {
		return fmt.Errorf("The PDF password is longer than %d bytes, so PDF readers couldn't open the file with it.", MaxPasswordBytes)
	}
	watermark := strings.TrimSpace(options.Watermark)
	finished := path + ".finished"
	defer os.Remove(finished)
	if watermark != "" || options.PageNumbers {
		pc, err := readContext(ctx, path)
		if err != nil {
			return err
		}
		var wm outline
		if watermark != "" {
			wm = textOutline(watermark, 100, true)
		}
		for i := 1; i <= pc.PageCount; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			number := ""
			if options.PageNumbers {
				number = fmt.Sprintf("%d / %d", i, pc.PageCount)
			}
			if err := stampPage(ctx, pc, i, wm, number); err != nil {
				return err
			}
		}
		if err := writeContext(ctx, pc, finished); err != nil {
			return err
		}
	} else if err := copyWritable(path, finished); err != nil {
		return err
	}
	if options.Password != "" {
		conf := pdfConf()
		conf.UserPW = options.Password
		conf.OwnerPW = options.Password
		conf.EncryptUsingAES = true
		conf.EncryptKeyLength = 256
		encrypted := path + ".encrypted"
		defer os.Remove(encrypted)
		if err := api.EncryptFile(ctx, finished, encrypted, conf); err != nil {
			return fmt.Errorf("could not encrypt the PDF: %w", err)
		}
		return os.Rename(encrypted, path)
	}
	return os.Rename(finished, path)
}

func rectOf(o *types.Rectangle) (x1, y1, x2, y2 float64) {
	return o.LL.X, o.LL.Y, o.UR.X, o.UR.Y
}

// stampPage draws the stamps on page n as it is shown: inside its crop box
// and the right way up for its /Rotate.
func stampPage(ctx context.Context, pc *pdfContext, n int, watermark outline, number string) error {
	if watermark.empty() && number == "" {
		return nil
	}
	d, _, inh, err := pc.PageDict(ctx, n, false)
	if err != nil {
		return err
	}
	if inh == nil || inh.MediaBox == nil {
		return nil
	}
	mx1, my1, mx2, my2 := rectOf(inh.MediaBox)
	cx1, cy1, cx2, cy2 := mx1, my1, mx2, my2
	if inh.CropBox != nil {
		cx1, cy1, cx2, cy2 = rectOf(inh.CropBox)
	}
	left, bottom := max(min(cx1, cx2), min(mx1, mx2)), max(min(cy1, cy2), min(my1, my2))
	right, top := min(max(cx1, cx2), max(mx1, mx2)), min(max(cy1, cy2), max(my1, my2))
	width, height := right-left, top-bottom
	if width <= 0 || height <= 0 {
		return nil
	}
	// The shown page (x right, y down from its top left) onto PDF space.
	var shown affine
	shownW, shownH := width, height
	switch normaliseRotation(inh.Rotate) {
	case 90:
		shown, shownW, shownH = affine{0, 1, 1, 0, left, bottom}, height, width
	case 180:
		shown = affine{-1, 0, 0, 1, right, bottom}
	case 270:
		shown, shownW, shownH = affine{0, -1, -1, 0, right, top}, height, width
	default:
		shown = affine{1, 0, 0, -1, left, top}
	}

	var content bytes.Buffer
	content.WriteString("Q q\n")
	if !watermark.empty() {
		bw, bh := watermark.MaxX-watermark.MinX, watermark.MaxY-watermark.MinY
		diagonal := math.Hypot(shownW, shownH)
		s := 0.7 * diagonal / bw
		// Short text isn't blown up to cover the page: at most 1/4 of the short side high.
		s = min(s, min(shownW, shownH)/4/bh)
		m := translate(-watermark.midX(), -watermark.midY()).then(scaleBy(s)).
			then(rotateBy(-math.Atan2(shownH, shownW))).then(translate(shownW/2, shownH/2)).then(shown)
		content.WriteString("/FileeStamp gs 0.502 g\n")
		writePath(&content, watermark, m)
		content.WriteString("f\n")
	}
	if number != "" {
		o := textOutline(number, 9, false)
		if !o.empty() {
			m := translate(shownW/2-o.midX(), shownH-10*pointsPerMm).then(shown)
			content.WriteString("/FileeOpaque gs 0.376 g\n")
			writePath(&content, o, m)
			content.WriteString("f\n")
		}
	}
	content.WriteString("Q\n")

	// The page's own content is put between q and Q, so its graphics state
	// can't move the stamps.
	open, err := newContentStream(pc, []byte("q\n"))
	if err != nil {
		return err
	}
	stamp, err := newContentStream(pc, content.Bytes())
	if err != nil {
		return err
	}
	contents := types.Array{*open}
	switch c := d["Contents"].(type) {
	case types.IndirectRef:
		if o, _ := pc.Dereference(c); o != nil {
			if arr, ok := o.(types.Array); ok {
				contents = append(contents, arr...)
			} else {
				contents = append(contents, c)
			}
		}
	case types.Array:
		contents = append(contents, c...)
	}
	d["Contents"] = append(contents, *stamp)

	// Resources of its own, with the transparency of the stamps.
	res := types.Dict{}
	for k, v := range inh.Resources {
		res[k] = v
	}
	ext := types.Dict{}
	if o, _ := pc.Dereference(res["ExtGState"]); o != nil {
		if e, ok := o.(types.Dict); ok {
			for k, v := range e {
				ext[k] = v
			}
		}
	}
	ext["FileeStamp"] = types.Dict{"Type": types.Name("ExtGState"), "ca": types.Float(56.0 / 255), "CA": types.Float(56.0 / 255)}
	ext["FileeOpaque"] = types.Dict{"Type": types.Name("ExtGState"), "ca": types.Float(1), "CA": types.Float(1)}
	res["ExtGState"] = ext
	d["Resources"] = res
	return nil
}

func newContentStream(pc *pdfContext, data []byte) (*types.IndirectRef, error) {
	sd, err := pc.NewStreamDictForBuf(data)
	if err != nil {
		return nil, err
	}
	if err := sd.Encode(); err != nil {
		return nil, err
	}
	return pc.IndRefForNewObject(*sd)
}

// writePath writes an outline transformed by m as path operators.
func writePath(b *bytes.Buffer, o outline, m affine) {
	for _, s := range o.Segs {
		switch s.Op {
		case 'M', 'L':
			x, y := m.apply(s.Pts[0][0], s.Pts[0][1])
			fmt.Fprintf(b, "%s %s %c\n", num(x), num(y), map[byte]byte{'M': 'm', 'L': 'l'}[s.Op])
		case 'C':
			for i := 0; i < 3; i++ {
				x, y := m.apply(s.Pts[i][0], s.Pts[i][1])
				fmt.Fprintf(b, "%s %s ", num(x), num(y))
			}
			b.WriteString("c\n")
		case 'Z':
			b.WriteString("h\n")
		}
	}
}
