package pdf

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os"
	"time"
)

// pdfMaker writes a new PDF of picture pages, page by page: each picture is
// written as it comes, so a long TIFF never sits in memory as a whole.
type pdfMaker struct {
	f       *os.File
	w       *bufio.Writer
	offsets []int64
	pos     int64
	pages   []int // object numbers
	// object 1 is the catalog, 2 the page tree: written at the end.
}

func newPdfMaker(path string) (*pdfMaker, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	m := &pdfMaker{f: f, w: bufio.NewWriter(f), offsets: make([]int64, 3)}
	m.write("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	return m, nil
}

func (m *pdfMaker) write(s string) { n, _ := m.w.WriteString(s); m.pos += int64(n) }
func (m *pdfMaker) writeBytes(b []byte) {
	n, _ := m.w.Write(b)
	m.pos += int64(n)
}

// object writes the next object (or object n when n > 0) and returns its number.
func (m *pdfMaker) object(n int, dict string, stream []byte) int {
	if n == 0 {
		m.offsets = append(m.offsets, 0)
		n = len(m.offsets) - 1
	}
	m.offsets[n] = m.pos
	m.write(fmt.Sprintf("%d 0 obj\n%s", n, dict))
	if stream != nil {
		m.write("\nstream\n")
		m.writeBytes(stream)
		m.write("\nendstream")
	}
	m.write("\nendobj\n")
	return n
}

func (m *pdfMaker) reserve() int {
	m.offsets = append(m.offsets, 0)
	return len(m.offsets) - 1
}

func deflate(b []byte) []byte {
	var buf bytes.Buffer
	z, _ := zlib.NewWriterLevel(&buf, zlib.BestCompression)
	z.Write(b)
	z.Close()
	return buf.Bytes()
}

// imageObject writes a picture as an image XObject (with a soft mask for
// transparency). jpegData, when given, is embedded as it is.
func (m *pdfMaker) imageObject(img image.Image, jpegData []byte) (int, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if c := jpegComponents(jpegData); jpegData != nil && (c == 1 || c == 3) {
		if cfg, err := jpeg.DecodeConfig(bytes.NewReader(jpegData)); err == nil {
			cs := "/DeviceRGB"
			if c == 1 {
				cs = "/DeviceGray"
			}
			return m.object(0, fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace %s /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>",
				cfg.Width, cfg.Height, cs, len(jpegData)), jpegData), nil
		}
	}
	gray := isGray(img)
	alpha := !gray && hasAlpha(img)
	var pix, mask []byte
	if gray {
		g := img.(*image.Gray)
		pix = make([]byte, 0, w*h)
		for y := 0; y < h; y++ {
			pix = append(pix, g.Pix[y*g.Stride:y*g.Stride+w]...)
		}
	} else {
		n := toNRGBA(img)
		pix = make([]byte, 0, w*h*3)
		if alpha {
			mask = make([]byte, 0, w*h)
		}
		for i := 0; i < len(n.Pix); i += 4 {
			pix = append(pix, n.Pix[i], n.Pix[i+1], n.Pix[i+2])
			if alpha {
				mask = append(mask, n.Pix[i+3])
			}
		}
	}
	smask := ""
	if alpha {
		data := deflate(mask)
		id := m.object(0, fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /FlateDecode /Length %d >>",
			w, h, len(data)), data)
		smask = fmt.Sprintf(" /SMask %d 0 R", id)
	}
	cs := "/DeviceRGB"
	if gray {
		cs = "/DeviceGray"
	}
	data := deflate(pix)
	return m.object(0, fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace %s /BitsPerComponent 8 /Filter /FlateDecode%s /Length %d >>",
		w, h, cs, smask, len(data)), data), nil
}

// jpegComponents is the number of colour components of a JPEG's frame.
func jpegComponents(data []byte) int {
	n := 0
	for i := 2; i+9 < len(data); {
		if data[i] != 0xFF {
			return n
		}
		marker := data[i+1]
		length := int(data[i+2])<<8 | int(data[i+3])
		if marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC {
			return int(data[i+9])
		}
		i += 2 + length
	}
	return n
}

// addPicturePage adds a page of width × height points showing the picture in
// the box (x, y from the bottom left, w × h points).
func (m *pdfMaker) addPicturePage(pageW, pageH float64, img image.Image, jpegData []byte, x, y, w, h float64) error {
	id, err := m.imageObject(img, jpegData)
	if err != nil {
		return err
	}
	content := []byte(fmt.Sprintf("q %s 0 0 %s %s %s cm /Im0 Do Q", num(w), num(h), num(x), num(y)))
	cid := m.object(0, fmt.Sprintf("<< /Length %d >>", len(content)), content)
	p := m.object(0, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %s %s] /Resources << /XObject << /Im0 %d 0 R >> >> /Contents %d 0 R >>",
		num(pageW), num(pageH), id, cid), nil)
	m.pages = append(m.pages, p)
	return nil
}

// Close writes the page tree, catalog and cross-reference table.
func (m *pdfMaker) Close() error {
	var kids bytes.Buffer
	for _, p := range m.pages {
		fmt.Fprintf(&kids, "%d 0 R ", p)
	}
	m.object(2, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids.String(), len(m.pages)), nil)
	m.object(1, "<< /Type /Catalog /Pages 2 0 R >>", nil)
	info := m.object(0, fmt.Sprintf("<< /Producer (Filee) /CreationDate (D:%s) >>", time.Now().UTC().Format("20060102150405Z")), nil)
	xref := m.pos
	m.write(fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \n", len(m.offsets)))
	for _, o := range m.offsets[1:] {
		m.write(fmt.Sprintf("%010d 00000 n \n", o))
	}
	m.write(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(m.offsets), info, xref))
	if err := m.w.Flush(); err != nil {
		m.f.Close()
		return err
	}
	return m.f.Close()
}

// Abort closes and removes the unfinished file.
func (m *pdfMaker) Abort() {
	m.f.Close()
	os.Remove(m.f.Name())
}

// num formats a PDF number without exponent and trailing zeros.
func num(v float64) string {
	s := fmt.Sprintf("%.4f", v)
	s = trimZeros(s)
	if s == "-0" {
		return "0"
	}
	return s
}

func trimZeros(s string) string {
	if !bytes.ContainsRune([]byte(s), '.') {
		return s
	}
	for len(s) > 0 && s[len(s)-1] == '0' {
		s = s[:len(s)-1]
	}
	if len(s) > 0 && s[len(s)-1] == '.' {
		s = s[:len(s)-1]
	}
	return s
}

var _ io.Closer = (*pdfMaker)(nil)
