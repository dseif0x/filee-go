package vector

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Tajima DST embroidery designs (issue #397) as SVG: the stitches as lines, a
// colour per thread. A DST file is a 512-byte text header ("LA:" name, stitch
// count, extents) and 3-byte stitch records. Each record moves the needle by
// up to ±121 in X and Y (0.1 mm units): byte 0 holds the ±1 and ±9 bits of
// both axes, byte 1 the ±3 and ±27 bits, byte 2 the ±81 bits and, in its top
// two bits, what the record is (a stitch, a jump, a colour change or a
// sequin-mode switch; 0xF3 there ends the design). Threads have no colours in
// DST, so the colour changes cycle through a fixed palette.

const dstHeaderSize = 512

var dstPalette = []string{"#1D3557", "#E63946", "#2A9D8F", "#F4A261", "#6A4C93", "#264653", "#E9C46A", "#8D99AE"}

type dstPoint struct{ X, Y int }

// readDst returns the stitched runs of each thread, in 0.1 mm with Y up.
func readDst(data []byte) ([][][]dstPoint, error) {
	if len(data) < dstHeaderSize || data[0] != 'L' || data[1] != 'A' || data[2] != ':' {
		return nil, errors.New(`the file isn't a Tajima DST embroidery design (it has no "LA:" header)`)
	}
	threads := [][][]dstPoint{{}}
	var run []dstPoint
	runOpen := false
	x, y := 0, 0
	for i := dstHeaderSize; i+3 <= len(data); i += 3 {
		b0, b1, b2 := int(data[i]), int(data[i+1]), int(data[i+2])
		if b2&0xF3 == 0xF3 {
			break
		}
		dx := dstStep(b0, b1, b2, 0x01, 0x02, 0x04, 0x08, 0x01, 0x02, 0x04, 0x08, 0x04, 0x08)
		dy := dstStep(b0, b1, b2, 0x80, 0x40, 0x20, 0x10, 0x80, 0x40, 0x20, 0x10, 0x20, 0x10)
		x += dx
		y += dy
		closeRun := func() {
			if runOpen {
				t := len(threads) - 1
				threads[t][len(threads[t])-1] = run
			}
			run, runOpen = nil, false
		}
		switch {
		case b2&0xC3 == 0xC3:
			closeRun()
			threads = append(threads, nil)
		case b2&0x43 == 0x43 || b2&0x83 == 0x83:
			closeRun()
		default:
			if !runOpen {
				run = []dstPoint{{x - dx, y - dy}}
				threads[len(threads)-1] = append(threads[len(threads)-1], nil)
				runOpen = true
			}
			run = append(run, dstPoint{x, y})
			t := len(threads) - 1
			threads[t][len(threads[t])-1] = run
		}
	}
	out := threads[:0]
	for _, t := range threads {
		if len(t) > 0 {
			out = append(out, t)
		}
	}
	return out, nil
}

func bit(b, mask, v int) int {
	if b&mask != 0 {
		return v
	}
	return 0
}

func dstStep(b0, b1, b2, p1, m1, p9, m9, p3, m3, p27, m27, p81, m81 int) int {
	return bit(b0, p1, 1) - bit(b0, m1, 1) + bit(b0, p9, 9) - bit(b0, m9, 9) +
		bit(b1, p3, 3) - bit(b1, m3, 3) + bit(b1, p27, 27) - bit(b1, m27, 27) +
		bit(b2, p81, 81) - bit(b2, m81, 81)
}

// DstToSVG draws the design at its real size (millimetres).
func DstToSVG(data []byte) (string, error) {
	threads, err := readDst(data)
	if err != nil {
		return "", err
	}
	first := true
	var minX, maxX, minY, maxY int
	for _, t := range threads {
		for _, r := range t {
			for _, p := range r {
				if first {
					minX, maxX, minY, maxY = p.X, p.X, p.Y, p.Y
					first = false
				}
				minX, maxX = min(minX, p.X), max(maxX, p.X)
				minY, maxY = min(minY, p.Y), max(maxY, p.Y)
			}
		}
	}
	if first {
		return "", errors.New("the DST file has no stitches")
	}
	const margin = 20 // 2 mm around the design
	minX, maxX, minY, maxY = minX-margin, maxX+margin, minY-margin, maxY+margin
	width, height := maxX-minX, maxY-minY
	mm := func(tenths int) string { return strconv.FormatFloat(float64(tenths)/10, 'f', -1, 64) }
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%smm" height="%smm" viewBox="0 0 %d %d">`, mm(width), mm(height), width, height)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#FFFFFF"/>`, width, height)
	for i, t := range threads {
		// A thread about 0.4 mm thick; Y flipped, DST counts it upwards.
		fmt.Fprintf(&b, `<g fill="none" stroke="%s" stroke-width="4" stroke-linecap="round" stroke-linejoin="round">`, dstPalette[i%len(dstPalette)])
		for _, r := range t {
			b.WriteString(`<polyline points="`)
			for j, p := range r {
				if j > 0 {
					b.WriteByte(' ')
				}
				fmt.Fprintf(&b, "%d,%d", p.X-minX, maxY-p.Y)
			}
			b.WriteString(`"/>`)
		}
		b.WriteString("</g>")
	}
	b.WriteString("</svg>")
	return b.String(), nil
}
