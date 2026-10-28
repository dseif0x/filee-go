package images

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"hash/crc32"
	"io"
	"math"
	"strings"
)

// Metadata in the containers Filee reads and writes: JPEG segments, PNG
// chunks, WebP RIFF chunks (TIFF tags are in tiffwrite.go / decode.go).

var (
	exifHeader = []byte("Exif\x00\x00")
	xmpHeader  = []byte("http://ns.adobe.com/xap/1.0/\x00")
	iccHeader  = []byte("ICC_PROFILE\x00")
)

// ---- JPEG ----

// jpegMeta reads the metadata and estimates the quality of a JPEG file.
func jpegMeta(data []byte) Meta {
	var m Meta
	var icc [][]byte
	var tables [][]int
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			break
		}
		marker := data[i+1]
		if marker == 0xFF {
			i++
			continue
		}
		if marker == 0xD8 || (marker >= 0xD0 && marker <= 0xD7) || marker == 0x01 {
			i += 2
			continue
		}
		if marker == 0xDA || marker == 0xD9 {
			break
		}
		n := int(binary.BigEndian.Uint16(data[i+2:]))
		if n < 2 || i+2+n > len(data) {
			break
		}
		seg := data[i+4 : i+2+n]
		switch marker {
		case 0xE0:
			if bytes.HasPrefix(seg, []byte("JFIF\x00")) && len(seg) >= 12 {
				unit := seg[7]
				x, y := float64(binary.BigEndian.Uint16(seg[8:])), float64(binary.BigEndian.Uint16(seg[10:]))
				switch unit {
				case 1:
					m.DpiX, m.DpiY = x, y
				case 2:
					m.DpiX, m.DpiY = x*2.54, y*2.54
				}
			}
		case 0xE1:
			switch {
			case bytes.HasPrefix(seg, exifHeader) && m.Exif == nil:
				m.Exif = bytes.Clone(seg[len(exifHeader):])
			case bytes.HasPrefix(seg, xmpHeader) && m.XMP == nil:
				m.XMP = bytes.Clone(seg[len(xmpHeader):])
			}
		case 0xE2:
			if bytes.HasPrefix(seg, iccHeader) && len(seg) > 14 {
				icc = append(icc, seg)
			}
		case 0xFE:
			m.Comment = string(seg)
		case 0xDB:
			for p := 0; p < len(seg); {
				prec, id := seg[p]>>4, seg[p]&15
				p++
				size := 64
				if prec != 0 {
					size = 128
				}
				if p+size > len(seg) {
					break
				}
				t := make([]int, 64)
				for k := 0; k < 64; k++ {
					if prec != 0 {
						t[k] = int(binary.BigEndian.Uint16(seg[p+2*k:]))
					} else {
						t[k] = int(seg[p+k])
					}
				}
				for len(tables) <= int(id) {
					tables = append(tables, nil)
				}
				tables[id] = t
				p += size
			}
		}
		i += 2 + n
	}
	if len(icc) > 0 {
		// Chunks are numbered from 1; join them in order.
		parts := make([][]byte, 256)
		for _, c := range icc {
			parts[c[12]] = c[14:]
		}
		var buf []byte
		for _, p := range parts {
			buf = append(buf, p...)
		}
		m.ICC = buf
	}
	m.Orientation = exifOrientation(m.Exif)
	m.Quality = estimateJpegQuality(tables)
	return m
}

// libjpeg's example tables (Annex K), which the quality scales.
var stdLuminance = [64]int{16, 11, 10, 16, 24, 40, 51, 61, 12, 12, 14, 19, 26, 58, 60, 55, 14, 13, 16, 24, 40, 57, 69, 56,
	14, 17, 22, 29, 51, 87, 80, 62, 18, 22, 37, 56, 68, 109, 103, 77, 24, 35, 55, 64, 81, 104, 113, 92, 49, 64, 78, 87, 103,
	121, 120, 101, 72, 92, 95, 98, 112, 100, 103, 99}
var stdChrominance = [64]int{17, 18, 24, 47, 99, 99, 99, 99, 18, 21, 26, 66, 99, 99, 99, 99, 24, 26, 56, 99, 99, 99, 99, 99,
	47, 66, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99,
	99, 99, 99, 99, 99, 99, 99, 99, 99}

// estimateJpegQuality finds the libjpeg quality whose scaled tables come
// closest to the file's (what ImageMagick reports as the image's quality).
func estimateJpegQuality(tables [][]int) int {
	if len(tables) == 0 || tables[0] == nil {
		return 0
	}
	sum := func(t []int) int {
		s := 0
		for _, v := range t {
			s += v
		}
		return s
	}
	lum := sum(tables[0])
	chrom := -1
	if len(tables) > 1 && tables[1] != nil {
		chrom = sum(tables[1])
	}
	best, bestDiff := 0, math.MaxInt
	for q := 1; q <= 100; q++ {
		scale := 200 - 2*q
		if q < 50 {
			scale = 5000 / q
		}
		scaled := func(std *[64]int) int {
			s := 0
			for _, v := range std {
				s += min(255, max(1, (v*scale+50)/100))
			}
			return s
		}
		d := abs(scaled(&stdLuminance) - lum)
		if chrom >= 0 {
			d += abs(scaled(&stdChrominance) - chrom)
		}
		if d <= bestDiff {
			best, bestDiff = q, d
		}
	}
	return best
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// jpegWithMeta inserts JFIF density, Exif, XMP, ICC and a comment after the SOI
// of a JPEG written by image/jpeg (which writes none of them).
func jpegWithMeta(jpg []byte, m *Meta) []byte {
	if len(jpg) < 2 {
		return jpg
	}
	var out bytes.Buffer
	out.Write(jpg[:2])
	seg := func(marker byte, parts ...[]byte) {
		n := 2
		for _, p := range parts {
			n += len(p)
		}
		if n > 0xFFFF {
			return
		}
		out.Write([]byte{0xFF, marker, byte(n >> 8), byte(n)})
		for _, p := range parts {
			out.Write(p)
		}
	}
	jfif := []byte{'J', 'F', 'I', 'F', 0, 1, 1, 0, 0, 1, 0, 1, 0, 0}
	if m.DpiX > 0 && m.DpiY > 0 {
		jfif[7] = 1
		binary.BigEndian.PutUint16(jfif[8:], uint16(min(65535, math.Round(m.DpiX))))
		binary.BigEndian.PutUint16(jfif[10:], uint16(min(65535, math.Round(m.DpiY))))
	}
	seg(0xE0, jfif)
	if m.Exif != nil {
		seg(0xE1, exifHeader, m.Exif)
	}
	if m.XMP != nil {
		seg(0xE1, xmpHeader, m.XMP)
	}
	if len(m.ICC) > 0 {
		const chunk = 65519
		count := (len(m.ICC) + chunk - 1) / chunk
		if count <= 255 {
			for i := 0; i < count; i++ {
				part := m.ICC[i*chunk : min(len(m.ICC), (i+1)*chunk)]
				seg(0xE2, iccHeader, []byte{byte(i + 1), byte(count)}, part)
			}
		}
	}
	if m.Comment != "" {
		seg(0xFE, []byte(m.Comment))
	}
	rest := jpg[2:]
	// image/jpeg writes no APP0; a JFIF one from elsewhere is replaced.
	if len(rest) > 4 && rest[0] == 0xFF && rest[1] == 0xE0 {
		rest = rest[2+int(binary.BigEndian.Uint16(rest[2:])):]
	}
	out.Write(rest)
	return out.Bytes()
}

// ---- PNG ----

var pngSignature = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}

type pngChunk struct {
	Type string
	Data []byte
}

// pngChunks splits a PNG file into its chunks (up to IEND).
func pngChunks(file []byte) ([]pngChunk, error) {
	if !bytes.HasPrefix(file, pngSignature) {
		return nil, errNotPNG
	}
	var chunks []pngChunk
	for p := 8; p+12 <= len(file); {
		n := binary.BigEndian.Uint32(file[p:])
		if uint64(n) > uint64(len(file)-p-12) {
			return chunks, errDamagedPNG
		}
		t := string(file[p+4 : p+8])
		chunks = append(chunks, pngChunk{t, file[p+8 : p+8+int(n)]})
		p += 12 + int(n)
		if t == "IEND" {
			break
		}
	}
	return chunks, nil
}

func writePNGChunk(w io.Writer, typ string, data []byte) {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(data)))
	w.Write(n[:])
	w.Write([]byte(typ))
	w.Write(data)
	c := crc32.NewIEEE()
	c.Write([]byte(typ))
	c.Write(data)
	binary.BigEndian.PutUint32(n[:], c.Sum32())
	w.Write(n[:])
}

// pngMeta reads ICC (iCCP), Exif (eXIf or ImageMagick's "Raw profile type
// exif" text), XMP (iTXt), resolution (pHYs) and a comment.
func pngMeta(chunks []pngChunk) Meta {
	var m Meta
	for _, c := range chunks {
		switch c.Type {
		case "iCCP":
			if i := bytes.IndexByte(c.Data, 0); i >= 0 && i+2 <= len(c.Data) {
				m.ICC, _ = inflate(c.Data[i+2:])
			}
		case "eXIf":
			m.Exif = normalizeExif(bytes.Clone(c.Data))
		case "pHYs":
			if len(c.Data) == 9 && c.Data[8] == 1 {
				m.DpiX = float64(binary.BigEndian.Uint32(c.Data)) * 0.0254
				m.DpiY = float64(binary.BigEndian.Uint32(c.Data[4:])) * 0.0254
			}
		case "iTXt":
			key, rest, ok := bytes.Cut(c.Data, []byte{0})
			if ok && string(key) == "XML:com.adobe.xmp" && len(rest) >= 2 {
				compressed := rest[0] == 1
				rest = rest[2:]
				// language tag and translated keyword
				for k := 0; k < 2; k++ {
					if i := bytes.IndexByte(rest, 0); i >= 0 {
						rest = rest[i+1:]
					}
				}
				if compressed {
					rest, _ = inflate(rest)
				}
				m.XMP = bytes.Clone(rest)
			}
		case "tEXt", "zTXt":
			key, rest, ok := bytes.Cut(c.Data, []byte{0})
			if !ok {
				continue
			}
			if c.Type == "zTXt" && len(rest) > 1 {
				rest, _ = inflate(rest[1:])
			}
			switch k := string(key); {
			case k == "Comment":
				m.Comment = string(rest)
			case k == "Raw profile type exif" || k == "Raw profile type APP1":
				if m.Exif == nil {
					m.Exif = normalizeExif(rawProfile(rest))
				}
			case k == "Raw profile type icc" || k == "Raw profile type icm":
				if m.ICC == nil {
					m.ICC = rawProfile(rest)
				}
			case k == "Raw profile type xmp":
				if m.XMP == nil {
					m.XMP = rawProfile(rest)
				}
			}
		}
	}
	m.Orientation = exifOrientation(m.Exif)
	return m
}

// rawProfile decodes ImageMagick's "\nexif\n   1234\n<hex…>" text profile.
func rawProfile(text []byte) []byte {
	lines := strings.SplitN(strings.TrimLeft(string(text), "\n"), "\n", 3)
	if len(lines) < 3 {
		return nil
	}
	h := strings.Map(func(r rune) rune {
		if r == '\n' || r == ' ' || r == '\r' {
			return -1
		}
		return r
	}, lines[2])
	b, err := hex.DecodeString(h)
	if err != nil {
		return nil
	}
	return b
}

func inflate(b []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, 256<<20))
}

func deflate(b []byte, level int) []byte {
	var buf bytes.Buffer
	w, _ := zlib.NewWriterLevel(&buf, level)
	w.Write(b)
	w.Close()
	return buf.Bytes()
}

// pngMetaChunks are the chunks that carry m, in the order they go after IHDR.
func pngMetaChunks(m *Meta) []pngChunk {
	var out []pngChunk
	if len(m.ICC) > 0 {
		out = append(out, pngChunk{"iCCP", append([]byte("ICC profile\x00\x00"), deflate(m.ICC, zlib.BestCompression)...)})
	}
	if m.DpiX > 0 && m.DpiY > 0 {
		d := make([]byte, 9)
		binary.BigEndian.PutUint32(d, uint32(math.Round(m.DpiX/0.0254)))
		binary.BigEndian.PutUint32(d[4:], uint32(math.Round(m.DpiY/0.0254)))
		d[8] = 1
		out = append(out, pngChunk{"pHYs", d})
	}
	if m.Exif != nil {
		out = append(out, pngChunk{"eXIf", m.Exif})
	}
	if m.XMP != nil {
		out = append(out, pngChunk{"iTXt", append([]byte("XML:com.adobe.xmp\x00\x00\x00\x00\x00"), m.XMP...)})
	}
	if m.Comment != "" {
		out = append(out, pngChunk{"tEXt", append([]byte("Comment\x00"), []byte(latin1(m.Comment))...)})
	}
	return out
}

func latin1(s string) string {
	return strings.Map(func(r rune) rune {
		if r > 255 {
			return '?'
		}
		return r
	}, s)
}

// pngWithMeta inserts the metadata chunks after IHDR of a PNG written by image/png.
func pngWithMeta(png []byte, m *Meta) []byte {
	extra := pngMetaChunks(m)
	if len(extra) == 0 {
		return png
	}
	chunks, err := pngChunks(png)
	if err != nil || len(chunks) == 0 {
		return png
	}
	var out bytes.Buffer
	out.Write(pngSignature)
	for i, c := range chunks {
		writePNGChunk(&out, c.Type, c.Data)
		if i == 0 {
			for _, e := range extra {
				writePNGChunk(&out, e.Type, e.Data)
			}
		}
	}
	return out.Bytes()
}

// ---- WebP ----

// webpMeta reads the ICCP, EXIF and XMP chunks of a WebP file.
func webpMeta(data []byte) Meta {
	var m Meta
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return m
	}
	for p := 12; p+8 <= len(data); {
		t := string(data[p : p+4])
		n := int(binary.LittleEndian.Uint32(data[p+4:]))
		if n < 0 || p+8+n > len(data) {
			break
		}
		c := data[p+8 : p+8+n]
		switch t {
		case "ICCP":
			m.ICC = bytes.Clone(c)
		case "EXIF":
			m.Exif = normalizeExif(bytes.Clone(c))
		case "XMP ":
			m.XMP = bytes.Clone(c)
		}
		p += 8 + n + n&1
	}
	m.Orientation = exifOrientation(m.Exif)
	return m
}

// webpWithMeta adds ICCP, EXIF and XMP chunks to a still WebP file (turning a
// simple file into the extended format they need).
func webpWithMeta(data []byte, m *Meta, width, height int) []byte {
	if len(m.ICC) == 0 && m.Exif == nil && m.XMP == nil {
		return data
	}
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return data
	}
	type chunk struct {
		t string
		d []byte
	}
	var chunks []chunk
	var flags byte
	for p := 12; p+8 <= len(data); {
		t := string(data[p : p+4])
		n := int(binary.LittleEndian.Uint32(data[p+4:]))
		if n < 0 || p+8+n > len(data) {
			return data
		}
		if t == "VP8X" {
			flags = data[p+8]
		} else {
			chunks = append(chunks, chunk{t, data[p+8 : p+8+n]})
		}
		p += 8 + n + n&1
	}
	for _, c := range chunks {
		if c.t == "ANIM" {
			return data // animations keep what the encoder wrote
		}
		if c.t == "ALPH" || c.t == "VP8L" {
			flags |= 0x10
		}
	}
	var body []chunk
	if len(m.ICC) > 0 {
		flags |= 0x20
		body = append(body, chunk{"ICCP", m.ICC})
	}
	body = append(body, chunks...)
	if m.Exif != nil {
		flags |= 0x08
		body = append(body, chunk{"EXIF", m.Exif})
	}
	if m.XMP != nil {
		flags |= 0x04
		body = append(body, chunk{"XMP ", m.XMP})
	}
	vp8x := make([]byte, 10)
	vp8x[0] = flags
	put24 := func(b []byte, v int) { b[0], b[1], b[2] = byte(v), byte(v>>8), byte(v>>16) }
	put24(vp8x[4:], width-1)
	put24(vp8x[7:], height-1)
	body = append([]chunk{{"VP8X", vp8x}}, body...)

	var out bytes.Buffer
	out.WriteString("RIFF\x00\x00\x00\x00WEBP")
	for _, c := range body {
		var n [4]byte
		binary.LittleEndian.PutUint32(n[:], uint32(len(c.d)))
		out.WriteString(c.t)
		out.Write(n[:])
		out.Write(c.d)
		if len(c.d)&1 == 1 {
			out.WriteByte(0)
		}
	}
	b := out.Bytes()
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	return b
}

// inflatePartial inflates a zlib stream that may be cut short or carry more
// after it, returning up to want bytes.
func inflatePartial(b []byte, want int) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	out, err := io.ReadAll(io.LimitReader(r, int64(want)))
	if len(out) >= want {
		return out, nil
	}
	return out, err
}

func float32frombits(b uint32) float32 { return math.Float32frombits(b) }
