package images

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
)

// MNG (Multiple-image Network Graphics): read are the PNG datastreams it
// holds, placed by DEFI and timed by FRAM, as the frames of an animation;
// written is MNG-LC: one whole PNG per frame, its delay in FRAM, the loop in TERM.

var mngSignature = []byte{0x8A, 'M', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}

func decodeMNG(data []byte) (*Picture, error) {
	if !bytes.HasPrefix(data, mngSignature) {
		return nil, errors.New("the file is not an MNG animation")
	}
	be := binary.BigEndian
	var w, h int
	ticks := 0
	delay := 1
	loops := 0
	p := &Picture{}
	var canvas *image.NRGBA
	var png0 bytes.Buffer
	inPNG := false
	var shared []pngChunk // global PLTE / tRNS of the MNG
	x0, y0 := 0, 0
	for q := 8; q+12 <= len(data); {
		n := int(be.Uint32(data[q:]))
		if n < 0 || q+12+n > len(data) {
			break
		}
		t := string(data[q+4 : q+8])
		d := data[q+8 : q+8+n]
		raw := data[q : q+12+n]
		q += 12 + n
		switch t {
		case "MHDR":
			if len(d) >= 12 {
				w, h, ticks = int(be.Uint32(d)), int(be.Uint32(d[4:])), int(be.Uint32(d[8:]))
				if w > 0 && h > 0 && int64(w)*int64(h) <= 1<<30 {
					canvas = image.NewNRGBA(image.Rect(0, 0, w, h))
				}
			}
		case "TERM":
			if len(d) >= 10 && d[0] == 3 {
				it := int(be.Uint32(d[6:]))
				if it < 0x7FFFFFFF {
					loops = it + 1
				}
			}
		case "FRAM":
			if len(d) > 1 {
				rest := d[1:]
				if i := bytes.IndexByte(rest, 0); i >= 0 {
					rest = rest[i+1:]
					if len(rest) >= 8 && rest[0] != 0 {
						delay = int(be.Uint32(rest[4:]))
					}
				}
			}
		case "DEFI":
			if len(d) >= 12 {
				x0, y0 = int(int32(be.Uint32(d[4:]))), int(int32(be.Uint32(d[8:])))
			} else {
				x0, y0 = 0, 0
			}
		case "PLTE", "tRNS":
			if !inPNG {
				shared = append(shared, pngChunk{t, d})
				continue
			}
			png0.Write(raw)
		case "IHDR":
			inPNG = true
			png0.Reset()
			png0.Write(pngSignature)
			png0.Write(raw)
			if len(d) >= 10 && d[9] == 3 {
				for _, c := range shared {
					writePNGChunk(&png0, c.Type, c.Data)
				}
			}
		case "IEND":
			if !inPNG {
				continue
			}
			inPNG = false
			png0.Write(raw)
			img, err := png.Decode(bytes.NewReader(png0.Bytes()))
			if err != nil {
				continue
			}
			pic := ToNRGBA(img)
			if canvas == nil {
				canvas = image.NewNRGBA(pic.Rect)
			}
			over(canvas, pic, x0, y0)
			out := image.NewNRGBA(canvas.Rect)
			copy(out.Pix, canvas.Pix)
			f := &Frame{Img: out, Ticks: 100}
			if ticks > 0 {
				f.Ticks = ticks
			}
			f.Delay = delay
			p.Frames = append(p.Frames, f)
		case "MEND":
			q = len(data)
		default:
			if inPNG {
				png0.Write(raw)
			}
		}
	}
	if len(p.Frames) == 0 {
		return nil, errors.New("the MNG file holds no PNG picture Filee can read (JNG and delta images are not supported)")
	}
	alpha := false
	for _, f := range p.Frames {
		alpha = alpha || !opaque(f.Img)
	}
	for _, f := range p.Frames {
		f.Alpha = alpha
	}
	p.Loops = loops
	return p, nil
}

func encodeMNG(frames []*Frame, loops int) ([]byte, error) {
	w, h := 0, 0
	for _, f := range frames {
		w, h = max(w, f.Width()), max(h, f.Height())
	}
	be := binary.BigEndian
	var b bytes.Buffer
	b.Write(mngSignature)
	mhdr := make([]byte, 28)
	be.PutUint32(mhdr, uint32(w))
	be.PutUint32(mhdr[4:], uint32(h))
	be.PutUint32(mhdr[8:], 100)
	be.PutUint32(mhdr[16:], uint32(len(frames)))
	be.PutUint32(mhdr[24:], 1|2|8|64) // MNG-LC, transparency
	writePNGChunk(&b, "MHDR", mhdr)
	if len(frames) > 1 {
		term := make([]byte, 10)
		term[0] = 3
		it := uint32(0x7FFFFFFF)
		if loops > 0 {
			it = uint32(loops - 1)
		}
		be.PutUint32(term[6:], it)
		writePNGChunk(&b, "TERM", term)
	}
	last := -1
	for _, f := range frames {
		delay := f.Delay
		if f.Ticks > 0 && f.Ticks != 100 {
			delay = int(float64(f.Delay)*100/float64(f.Ticks) + 0.5)
		}
		if delay != last {
			fram := make([]byte, 10)
			fram[0] = 1
			fram[2] = 2 // change the delay for every following frame
			be.PutUint32(fram[6:], uint32(delay))
			writePNGChunk(&b, "FRAM", fram)
			last = delay
		}
		g := *f
		g.Meta = Meta{}
		data, err := encodePNG(&g)
		if err != nil {
			return nil, err
		}
		b.Write(data[8:]) // the PNG without its signature
	}
	writePNGChunk(&b, "MEND", nil)
	return b.Bytes(), nil
}
