package images

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
)

// FITS images (Flexible Image Transport System: telescope cameras, capture and
// stacking software), read by Filee itself from the FITS standard 4.0:
// 2880-byte blocks, a header of 80-character cards, big-endian data. The
// values are read as they are (BZERO + BSCALE × stored) to be stretched,
// debayered or written into a TIFF of the file's own depth. The first HDU
// holding a picture is read: the primary one, or else the first IMAGE
// extension. Tile-compressed images (fpack, ".fz") are not read.

const (
	fitsBlock        = 2880
	fitsCard         = 80
	fitsMaxSamples   = 256 * 1024 * 1024
	fitsMaxHeaderBlk = 10_000
)

var fitsPatterns = []string{"RGGB", "BGGR", "GRBG", "GBRG"}

// fitsImage is the physical values of a FITS picture's planes in stored order.
type fitsImage struct {
	w, h     int
	planes   [][]float32 // one (mono, or a colour camera's mosaic) or three (R, G, B)
	bitpix   int
	min, max float32
	integral bool
	pattern  string // RGGB, BGGR, GRBG, GBRG or ""
	offX     int
	offY     int
	bottomUp bool
}

func readFits(path string) (*fitsImage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return parseFits(f, st.Size())
}

func parseFits(r io.ReaderAt, size int64) (*fitsImage, error) {
	var primary map[string]string
	compressed := false
	pos := int64(0)
	for hdu := 0; pos < size; hdu++ {
		header, dataStart, err := fitsHeader(r, pos, hdu == 0)
		if err != nil {
			return nil, err
		}
		if primary == nil {
			primary = header
		}
		bitpix, err := fitsInt(header, "BITPIX")
		if err != nil {
			return nil, err
		}
		if !slices.Contains([]int64{8, 16, 32, 64, -32, -64}, bitpix) {
			return nil, fmt.Errorf("this FITS file has an unknown BITPIX (%d)", bitpix)
		}
		naxis, err := fitsInt(header, "NAXIS")
		if err != nil || naxis < 0 || naxis > 999 {
			return nil, errors.New("this FITS file is damaged (NAXIS)")
		}
		axes := make([]int64, naxis)
		for i := range axes {
			if axes[i], err = fitsInt(header, "NAXIS"+strconv.Itoa(i+1)); err != nil {
				return nil, err
			}
			if axes[i] < 0 {
				return nil, errors.New("this FITS file is damaged (a negative axis length)")
			}
		}
		ext := ""
		if hdu > 0 {
			ext = header["XTENSION"]
		}
		if len(axes) >= 2 && axes[0] > 0 && axes[1] > 0 && (hdu == 0 || ext == "IMAGE") {
			return fitsRead(r, dataStart, header, primary, int(bitpix), axes)
		}
		if ext == "BINTABLE" && header["ZIMAGE"] == "T" {
			compressed = true
		}
		elements := int64(0)
		if len(axes) > 0 {
			elements = 1
			for _, a := range axes {
				elements *= a
			}
		}
		groups, params := int64(1), int64(0)
		if hdu > 0 {
			groups, params = fitsOptional(header, "GCOUNT", 1), fitsOptional(header, "PCOUNT", 0)
		}
		bytes := int64(0)
		if elements != 0 || params != 0 {
			bytes = abs64(bitpix) / 8 * groups * (params + elements)
		}
		pos = dataStart + (bytes+fitsBlock-1)/fitsBlock*fitsBlock
	}
	if compressed {
		return nil, errors.New("this FITS file is tile-compressed (fpack). Uncompress it first (funpack) and try again")
	}
	return nil, errors.New("this FITS file has no picture: it holds only tables or an empty header")
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func fitsRead(r io.ReaderAt, dataStart int64, header, primary map[string]string, bitpix int, axes []int64) (*fitsImage, error) {
	w, h := axes[0], axes[1]
	planeCount := int64(1)
	if len(axes) >= 3 && axes[2] == 3 {
		rest := true
		for _, a := range axes[3:] {
			rest = rest && a == 1
		}
		if rest {
			planeCount = 3
		}
	}
	if w > fitsMaxSamples || h > fitsMaxSamples || w*h > fitsMaxSamples/planeCount {
		return nil, fmt.Errorf("this FITS picture is too large (%d × %d)", w, h)
	}
	bzero := fitsReal(header, "BZERO", 0)
	bscale := fitsReal(header, "BSCALE", 1)
	var blank int64
	hasBlank := false
	if bitpix > 0 {
		if _, ok := header["BLANK"]; ok {
			if v, err := fitsInt(header, "BLANK"); err == nil {
				blank, hasBlank = v, true
			}
		}
	}
	bpv := abs(bitpix) / 8
	pixels := int(w * h)
	row := make([]byte, int(w)*bpv)
	img := &fitsImage{w: int(w), h: int(h), bitpix: bitpix}
	lo, hi := float32(math.Inf(1)), float32(math.Inf(-1))
	blanks := false
	off := dataStart
	for p := 0; p < int(planeCount); p++ {
		plane := make([]float32, pixels)
		for y := 0; y < int(h); y++ {
			if _, err := r.ReadAt(row, off); err != nil {
				return nil, errors.New("this FITS file is cut short")
			}
			off += int64(len(row))
			vals := plane[y*int(w) : (y+1)*int(w)]
			for i := range vals {
				var stored int64
				var fv float64
				isFloat := false
				switch bitpix {
				case 8:
					stored = int64(row[i])
				case 16:
					stored = int64(int16(binary.BigEndian.Uint16(row[i*2:])))
				case 32:
					stored = int64(int32(binary.BigEndian.Uint32(row[i*4:])))
				case 64:
					stored = int64(binary.BigEndian.Uint64(row[i*8:]))
				case -32:
					fv, isFloat = float64(math.Float32frombits(binary.BigEndian.Uint32(row[i*4:]))), true
				default:
					fv, isFloat = math.Float64frombits(binary.BigEndian.Uint64(row[i*8:])), true
				}
				var v float32
				switch {
				case isFloat:
					v = float32(bzero + bscale*fv)
				case hasBlank && stored == blank:
					v = float32(math.NaN())
					blanks = true
				default:
					v = float32(bzero + bscale*float64(stored))
				}
				vals[i] = v
				if !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) {
					lo, hi = min(lo, v), max(hi, v)
				}
			}
		}
		img.planes = append(img.planes, plane)
	}
	if lo > hi {
		lo, hi = 0, 0
	}
	img.min, img.max = lo, hi
	img.integral = bitpix > 0 && bzero == math.Floor(bzero) && bscale == math.Floor(bscale) && !blanks
	get := func(k string) string {
		if v, ok := header[k]; ok {
			return v
		}
		return primary[k]
	}
	if planeCount == 1 {
		for _, k := range []string{"BAYERPAT", "COLORTYP"} {
			t := strings.ToUpper(strings.TrimSpace(get(k)))
			if slices.Contains(fitsPatterns, t) {
				img.pattern = t
				break
			}
		}
	}
	parity := func(v int64) int { return int(((v % 2) + 2) % 2) }
	img.offX = parity(fitsOptional(header, "XBAYROFF", fitsOptional(primary, "XBAYROFF", 0)))
	img.offY = parity(fitsOptional(header, "YBAYROFF", fitsOptional(primary, "YBAYROFF", 0)))
	img.bottomUp = strings.ToUpper(strings.TrimSpace(get("ROWORDER"))) == "BOTTOM-UP"
	return img, nil
}

// fitsHeader reads the keyword = value cards of one header up to END; it
// returns them and where the data begins.
func fitsHeader(r io.ReaderAt, pos int64, primary bool) (map[string]string, int64, error) {
	cards := map[string]string{}
	block := make([]byte, fitsBlock)
	for b := 0; b < fitsMaxHeaderBlk; b++ {
		if n, _ := r.ReadAt(block, pos); n < fitsBlock {
			if b == 0 && primary {
				return nil, 0, errors.New("this is not a FITS file")
			}
			return nil, 0, errors.New("this FITS file is cut short")
		}
		pos += fitsBlock
		for c := 0; c < fitsBlock/fitsCard; c++ {
			card := string(block[c*fitsCard : (c+1)*fitsCard])
			keyword := strings.TrimRight(card[:8], " ")
			if b == 0 && c == 0 {
				want := "XTENSION"
				if primary {
					want = "SIMPLE"
				}
				if keyword != want {
					if primary {
						return nil, 0, errors.New("this is not a FITS file")
					}
					return nil, 0, errors.New("this FITS file is damaged (an extension without XTENSION)")
				}
			}
			if keyword == "END" {
				return cards, pos, nil
			}
			if _, seen := cards[keyword]; keyword != "" && card[8] == '=' && card[9] == ' ' && !seen {
				cards[keyword] = fitsValue(card[10:])
			}
		}
	}
	return nil, 0, errors.New("this FITS file is damaged (its header has no END)")
}

// fitsValue is a card's value: a string between quotes (” is a quote), else the text before the comment.
func fitsValue(field string) string {
	text := strings.TrimLeft(field, " ")
	if !strings.HasPrefix(text, "'") {
		if i := strings.IndexByte(text, '/'); i >= 0 {
			text = text[:i]
		}
		return strings.TrimSpace(text)
	}
	var v strings.Builder
	for i := 1; i < len(text); i++ {
		if text[i] == '\'' {
			if i+1 < len(text) && text[i+1] == '\'' {
				v.WriteByte('\'')
				i++
				continue
			}
			break
		}
		v.WriteByte(text[i])
	}
	return strings.TrimRight(v.String(), " ")
}

func fitsNumber(h map[string]string, k string) (float64, bool) {
	t, ok := h[k]
	if !ok {
		return 0, false
	}
	t = strings.NewReplacer("D", "E", "d", "e").Replace(t)
	v, err := strconv.ParseFloat(t, 64)
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, false
	}
	return v, true
}

func fitsInt(h map[string]string, k string) (int64, error) {
	v, ok := fitsNumber(h, k)
	if !ok || v != math.Floor(v) || math.Abs(v) >= 9e15 {
		return 0, fmt.Errorf("this FITS file is damaged (no valid %s)", k)
	}
	return int64(v), nil
}

func fitsOptional(h map[string]string, k string, def int64) int64 {
	if v, ok := fitsNumber(h, k); ok && v == math.Floor(v) && math.Abs(v) < 9e15 {
		return int64(v)
	}
	return def
}

func fitsReal(h map[string]string, k string, def float64) float64 {
	if v, ok := fitsNumber(h, k); ok {
		return v
	}
	return def
}
