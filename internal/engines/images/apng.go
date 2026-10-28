package images

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"io"
	"math"
	"os"
)

// Animated PNG (APNG) reader and writer, from the APNG specification (PNG 3rd
// edition): acTL (frame count, loops), one fcTL (region, delay, dispose and
// blend operations) per frame, the first frame's data in IDAT and the others'
// in fdAT chunks with a shared sequence number.

const (
	apngDisposeNone, apngDisposeBackground, apngDisposePrevious = 0, 1, 2
	apngBlendSource, apngBlendOver                              = 0, 1
	apngMaxChunk                                                = 1 << 20
)

// isAnimatedPNG: a PNG with an acTL chunk before its image data.
func isAnimatedPNG(data []byte) bool {
	if !bytes.HasPrefix(data, pngSignature) {
		return false
	}
	for p := 8; p+8 <= len(data); {
		n := binary.BigEndian.Uint32(data[p:])
		t := string(data[p+4 : p+8])
		if t == "acTL" {
			return true
		}
		if t == "IDAT" || t == "IEND" || n > math.MaxInt32 {
			return false
		}
		p += 12 + int(n)
	}
	return false
}

func isAnimatedPNGFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 8)
	if _, err := io.ReadFull(f, head); err != nil || !bytes.Equal(head, pngSignature) {
		return false
	}
	var h [8]byte
	for {
		if _, err := io.ReadFull(f, h[:]); err != nil {
			return false
		}
		n := binary.BigEndian.Uint32(h[:])
		switch string(h[4:]) {
		case "acTL":
			return true
		case "IDAT", "IEND":
			return false
		}
		if _, err := f.Seek(int64(n)+4, io.SeekCurrent); err != nil {
			return false
		}
	}
}

type apngFrame struct {
	w, h, x, y uint32
	num, den   uint16
	dispose    byte
	blend      byte
	data       bytes.Buffer
}

// decodeAPNG reads every frame of an APNG composed onto the canvas as a player
// shows it, with its delay and the loop count. A PNG without animation comes
// back as its one picture; a default image outside the animation is skipped.
func decodeAPNG(data []byte) (*Picture, error) {
	chunks, err := pngChunks(data)
	if err != nil {
		return nil, err
	}
	if len(chunks) == 0 || chunks[0].Type != "IHDR" || len(chunks[0].Data) != 13 {
		return nil, errors.New("the file is not a PNG image or is damaged")
	}
	header := chunks[0].Data
	cw, ch := binary.BigEndian.Uint32(header), binary.BigEndian.Uint32(header[4:])
	var actl []byte
	for _, c := range chunks {
		if c.Type == "acTL" {
			actl = c.Data
			break
		}
	}
	if len(actl) != 8 {
		return decodePNG(data)
	}
	plays := binary.BigEndian.Uint32(actl[4:])
	var shared []pngChunk
	var frames []*apngFrame
	var cur *apngFrame
	seenData := false
	for _, c := range chunks[1:] {
		switch c.Type {
		case "fcTL":
			if len(c.Data) != 26 {
				return nil, errors.New("the APNG file is damaged (fcTL chunk)")
			}
			d := c.Data
			cur = &apngFrame{
				w: binary.BigEndian.Uint32(d[4:]), h: binary.BigEndian.Uint32(d[8:]),
				x: binary.BigEndian.Uint32(d[12:]), y: binary.BigEndian.Uint32(d[16:]),
				num: binary.BigEndian.Uint16(d[20:]), den: binary.BigEndian.Uint16(d[22:]),
				dispose: d[24], blend: d[25],
			}
			if cur.w == 0 || cur.h == 0 || uint64(cur.x)+uint64(cur.w) > uint64(cw) || uint64(cur.y)+uint64(cur.h) > uint64(ch) ||
				cur.dispose > apngDisposePrevious || cur.blend > apngBlendOver {
				return nil, errors.New("the APNG file is damaged (a frame lies outside the picture)")
			}
			frames = append(frames, cur)
		case "IDAT":
			seenData = true
			if cur != nil {
				cur.data.Write(c.Data)
			}
		case "fdAT":
			if len(c.Data) < 4 || cur == nil {
				return nil, errors.New("the APNG file is damaged (fdAT chunk)")
			}
			cur.data.Write(c.Data[4:])
		case "IEND", "acTL":
		default:
			if !seenData {
				shared = append(shared, c)
			}
		}
	}
	var kept []*apngFrame
	for _, f := range frames {
		if f.data.Len() > 0 {
			kept = append(kept, f)
		}
	}
	if len(kept) == 0 {
		return nil, errors.New("the APNG file has no frames")
	}
	meta := pngMeta(chunks)
	canvas := image.NewNRGBA(image.Rect(0, 0, int(cw), int(ch)))
	p := &Picture{Loops: int(plays)}
	alpha := false
	for i, fr := range kept {
		img, err := png.Decode(bytes.NewReader(framePNG(header, shared, fr)))
		if err != nil {
			return nil, err
		}
		pic := ToNRGBA(img)
		dispose := fr.dispose
		if i == 0 && dispose == apngDisposePrevious {
			dispose = apngDisposeBackground
		}
		var previous *image.NRGBA
		if dispose == apngDisposePrevious {
			previous = image.NewNRGBA(canvas.Rect)
			copy(previous.Pix, canvas.Pix)
		}
		if fr.blend == apngBlendOver {
			over(canvas, pic, int(fr.x), int(fr.y))
		} else {
			paste(canvas, pic, int(fr.x), int(fr.y))
		}
		out := image.NewNRGBA(canvas.Rect)
		copy(out.Pix, canvas.Pix)
		alpha = alpha || !opaque(out)
		den := float64(fr.den)
		if den == 0 {
			den = 100
		}
		f := &Frame{Img: out, Ticks: 100, Delay: int(math.Round(float64(fr.num) * 100 / den)), Meta: meta}
		p.Frames = append(p.Frames, f)
		switch dispose {
		case apngDisposeBackground:
			r := image.Rect(int(fr.x), int(fr.y), int(fr.x+fr.w), int(fr.y+fr.h))
			for y := r.Min.Y; y < r.Max.Y; y++ {
				clear(canvas.Pix[canvas.PixOffset(r.Min.X, y):canvas.PixOffset(r.Max.X, y)])
			}
		case apngDisposePrevious:
			canvas = previous
		}
	}
	for _, f := range p.Frames {
		f.Alpha = alpha
	}
	return p, nil
}

// framePNG is a standalone PNG of one frame: the header with the frame's size, the shared chunks and its data.
func framePNG(header []byte, shared []pngChunk, f *apngFrame) []byte {
	var b bytes.Buffer
	b.Write(pngSignature)
	h := bytes.Clone(header)
	binary.BigEndian.PutUint32(h, f.w)
	binary.BigEndian.PutUint32(h[4:], f.h)
	writePNGChunk(&b, "IHDR", h)
	for _, c := range shared {
		writePNGChunk(&b, c.Type, c.Data)
	}
	writePNGChunk(&b, "IDAT", f.data.Bytes())
	writePNGChunk(&b, "IEND", nil)
	return b.Bytes()
}

// encodeAPNG writes frames as one APNG. Frames are placed at the top left of a
// canvas as large as the largest frame. Each frame after the first stores
// only the rectangle that changed, and a frame identical to the one before
// only lengthens its delay. All frames are 8-bit RGB, or RGBA when any frame
// has transparency (they share the header's pixel format).
func encodeAPNG(frames []*Frame, loops int) ([]byte, error) {
	if len(frames) == 0 {
		return nil, errors.New("no frames to write")
	}
	w, h := 0, 0
	for _, f := range frames {
		w, h = max(w, f.Width()), max(h, f.Height())
	}
	alpha := false
	for _, f := range frames {
		alpha = alpha || f.Alpha || f.Width() != w || f.Height() != h
	}
	type picture struct {
		img      *image.NRGBA
		num, den uint32
	}
	var pics []picture
	for _, f := range frames {
		img := f.Img
		if f.Width() != w || f.Height() != h {
			img = extent(img, w, h, 0, 0, colorTransparent)
		}
		if !alpha {
			img = flatten(img, colorWhite)
		}
		ticks := uint32(f.Ticks)
		if ticks == 0 {
			ticks = 100
		}
		if n := len(pics); n > 0 && bytes.Equal(img.Pix, pics[n-1].img.Pix) && pics[n-1].den == ticks {
			pics[n-1].num += uint32(f.Delay)
			continue
		}
		pics = append(pics, picture{img, uint32(f.Delay), ticks})
	}
	channels := 3
	if alpha {
		channels = 4
	}
	var out bytes.Buffer
	out.Write(pngSignature)
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr, uint32(w))
	binary.BigEndian.PutUint32(ihdr[4:], uint32(h))
	ihdr[8] = 8
	ihdr[9] = 2
	if alpha {
		ihdr[9] = 6
	}
	writePNGChunk(&out, "IHDR", ihdr)
	// Colour information and metadata of the first frame apply to the whole animation.
	for _, c := range pngMetaChunks(&frames[0].Meta) {
		writePNGChunk(&out, c.Type, c.Data)
	}
	actl := make([]byte, 8)
	binary.BigEndian.PutUint32(actl, uint32(len(pics)))
	binary.BigEndian.PutUint32(actl[4:], uint32(loops))
	writePNGChunk(&out, "acTL", actl)
	seq := uint32(0)
	for i, p := range pics {
		r := image.Rect(0, 0, w, h)
		if i > 0 {
			r = changedRegion(pics[i-1].img, p.img)
		}
		part := p.img
		if r != p.img.Rect {
			part = crop(p.img, r)
		}
		data := pngImageData(part, channels, zlib.DefaultCompression)
		num, den := fitDelay(p.num, p.den)
		fc := make([]byte, 26)
		binary.BigEndian.PutUint32(fc, seq)
		seq++
		binary.BigEndian.PutUint32(fc[4:], uint32(r.Dx()))
		binary.BigEndian.PutUint32(fc[8:], uint32(r.Dy()))
		binary.BigEndian.PutUint32(fc[12:], uint32(r.Min.X))
		binary.BigEndian.PutUint32(fc[16:], uint32(r.Min.Y))
		binary.BigEndian.PutUint16(fc[20:], num)
		binary.BigEndian.PutUint16(fc[22:], den)
		fc[24], fc[25] = apngDisposeNone, apngBlendSource
		writePNGChunk(&out, "fcTL", fc)
		for off := 0; ; {
			piece := data[off:min(len(data), off+apngMaxChunk)]
			off += len(piece)
			if i == 0 {
				writePNGChunk(&out, "IDAT", piece)
			} else {
				d := make([]byte, 4+len(piece))
				binary.BigEndian.PutUint32(d, seq)
				seq++
				copy(d[4:], piece)
				writePNGChunk(&out, "fdAT", d)
			}
			if off >= len(data) {
				break
			}
		}
	}
	writePNGChunk(&out, "IEND", nil)
	return out.Bytes(), nil
}

// changedRegion is the smallest rectangle holding every pixel that differs (at least one pixel).
func changedRegion(before, after *image.NRGBA) image.Rectangle {
	w, h := after.Rect.Dx(), after.Rect.Dy()
	top, bottom, left, right := -1, -1, w, -1
	for y := 0; y < h; y++ {
		rb := before.Pix[y*before.Stride : y*before.Stride+w*4]
		ra := after.Pix[y*after.Stride : y*after.Stride+w*4]
		if bytes.Equal(rb, ra) {
			continue
		}
		if top < 0 {
			top = y
		}
		bottom = y
		first := 0
		for first < w && bytes.Equal(rb[first*4:first*4+4], ra[first*4:first*4+4]) {
			first++
		}
		last := w - 1
		for last > first && bytes.Equal(rb[last*4:last*4+4], ra[last*4:last*4+4]) {
			last--
		}
		left, right = min(left, first), max(right, last)
	}
	if top < 0 {
		return image.Rect(0, 0, 1, 1) // only the delay differs: repeat one pixel
	}
	return image.Rect(left, top, right+1, bottom+1)
}

// fitDelay scales a delay down until both parts fit the 16-bit fields of fcTL.
func fitDelay(num, den uint32) (uint16, uint16) {
	for num > 65535 || den > 65535 {
		num = (num + 1) / 2
		den = max(1, (den+1)/2)
	}
	return uint16(num), uint16(den)
}

// pngImageData is the zlib stream of IDAT for img as 8-bit RGB (3) or RGBA (4)
// samples, each row with the filter that gives the smallest sum (libpng's heuristic).
func pngImageData(img *image.NRGBA, channels, level int) []byte {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	stride := w * channels
	prev := make([]byte, stride)
	cur := make([]byte, stride)
	cand := make([][]byte, 5)
	for i := range cand {
		cand[i] = make([]byte, stride+1)
	}
	var buf bytes.Buffer
	zw, _ := zlib.NewWriterLevel(&buf, level)
	for y := 0; y < h; y++ {
		row := img.Pix[y*img.Stride:]
		if channels == 4 {
			copy(cur, row[:stride])
		} else {
			for x := 0; x < w; x++ {
				copy(cur[x*3:x*3+3], row[x*4:x*4+3])
			}
		}
		best, bestSum := 0, -1
		for t := 0; t < 5; t++ {
			c := cand[t]
			c[0] = byte(t)
			sum := 0
			for i := 0; i < stride; i++ {
				var a, b, d byte
				if i >= channels {
					a = cur[i-channels]
					d = prev[i-channels]
				}
				b = prev[i]
				var v byte
				switch t {
				case 0:
					v = cur[i]
				case 1:
					v = cur[i] - a
				case 2:
					v = cur[i] - b
				case 3:
					v = cur[i] - byte((int(a)+int(b))/2)
				case 4:
					v = cur[i] - paeth(a, b, d)
				}
				c[i+1] = v
				sum += abs(int(int8(v)))
			}
			if bestSum < 0 || sum < bestSum {
				best, bestSum = t, sum
			}
		}
		zw.Write(cand[best])
		prev, cur = cur, prev
	}
	zw.Close()
	return buf.Bytes()
}

func paeth(a, b, c byte) byte {
	p := int(a) + int(b) - int(c)
	pa, pb, pc := abs(p-int(a)), abs(p-int(b)), abs(p-int(c))
	if pa <= pb && pa <= pc {
		return a
	}
	if pb <= pc {
		return b
	}
	return c
}
