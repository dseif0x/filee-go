package vector

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// HP-GL and HP-GL/2 plot files (.plt, issue #410): what AutoCAD and other
// programs "plot to file" for pen plotters and large-format printers. The pen
// moves become SVG polylines at the plot's real size (40 plotter units per
// millimetre).
//
// Read: IN, SP / PC / NP / PW (pens, colours, widths), PU / PD / PA / PR, CI,
// AA / AR (arcs as short lines), PE (HP-GL/2 encoded polylines), PM / EP / FP
// (polygons, filled with FP). A PJL / PCL wrapper around HP-GL/2 is skipped up
// to "enter HP-GL/2 mode" (ESC % … B), and HP-GL/1 device-control sequences
// (ESC . …) are skipped. Not drawn: text (LB), fill patterns, raster data;
// user scaling (SC) is ignored, so a scaled plot keeps its shape but not its size.

const unitsPerMm = 40

var hpglColors = []string{"#FFFFFF", "#000000", "#FF0000", "#00A000", "#E0C000", "#0000FF", "#FF00FF", "#00B0B0"}

type hpPoint struct{ X, Y float64 }

type hpStroke struct {
	Color  string
	Width  float64
	Points []hpPoint
	Filled bool
}

type hpCommand struct{ Name, Args string }

func isLetter(c byte) bool { return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' }

// hpglCommands splits the plot into commands: two letters and their arguments
// up to ';' or the next command (quoted text included). PE's arguments are its
// encoded bytes; LB's run to the label terminator (ETX).
func hpglCommands(s []byte) []hpCommand {
	var out []hpCommand
	i := 0
	for i < len(s) {
		c := s[i]
		if c == 27 {
			if i+1 < len(s) && s[i+1] == '.' {
				i = deviceControlEnd(s, i)
			} else {
				i = entryPoint(s, i)
			}
			continue
		}
		if c <= 32 || c == ';' || !isLetter(c) {
			i++
			continue
		}
		if i+1 >= len(s) {
			break
		}
		name := strings.ToUpper(string(s[i : i+2]))
		start := i + 2
		end := start
		switch name {
		case "PE":
			for end < len(s) && s[end] != ';' && s[end] != 27 {
				end++
			}
		case "LB":
			for end < len(s) && s[end] != 3 {
				end++
			}
		default:
			for end < len(s) && s[end] != ';' && s[end] != 27 && !isLetter(s[end]) {
				if s[end] == '"' {
					if close := bytes.IndexByte(s[end+1:], '"'); close >= 0 {
						end = end + 1 + close + 1
					} else {
						end = len(s)
					}
				} else {
					end++
				}
			}
		}
		// Latin-1 bytes as runes.
		args := make([]rune, 0, end-start)
		for _, b := range s[start:end] {
			args = append(args, rune(b))
		}
		out = append(out, hpCommand{name, string(args)})
		if end < len(s) && (s[end] == ';' || s[end] == 3) {
			i = end + 1
		} else {
			i = end
		}
	}
	return out
}

func deviceControlEnd(s []byte, at int) int {
	next := at + 3
	if next > len(s) {
		return len(s)
	}
	end := next
	for end < len(s) && (s[end] >= '0' && s[end] <= '9' || s[end] == ';' || s[end] == '-' || s[end] == '.') {
		end++
	}
	if end < len(s) && s[end] == ':' {
		return end + 1
	}
	return next
}

func entryPoint(s []byte, from int) int {
	for i := from; i+3 < len(s); i++ {
		if s[i] != 27 || s[i+1] != '%' {
			continue
		}
		j := i + 2
		if s[j] == '-' {
			j = i + 3
		}
		if j+1 < len(s) && s[j] >= '0' && s[j] <= '3' && s[j+1] == 'B' {
			return j + 2
		}
	}
	return len(s)
}

func hpNumbers(args string) []float64 {
	var out []float64
	for _, f := range strings.FieldsFunc(args, func(r rune) bool { return r == ',' || r == ' ' }) {
		if v, err := strconv.ParseFloat(strings.TrimSpace(f), 64); err == nil && !math.IsNaN(v) {
			out = append(out, v)
		}
	}
	return out
}

// decodePE reads the numbers of a PE command from *start until a flag or the
// end: base 64 (base 32 in 7-bit mode), digits 63.. then a terminating digit
// (191.. or 95..), least significant first; the lowest bit is the sign.
func decodePE(data []rune, start *int, fractionBits, base int) []float64 {
	terminator := 191
	if base != 64 {
		terminator = 95
	}
	var out []float64
	var value, scale int64 = 0, 1
	for ; *start < len(data); *start++ {
		c := int(data[*start])
		if c < 63 {
			break
		}
		if c >= terminator {
			value += int64(c-terminator) * scale
			signed := value >> 1
			if value&1 == 1 {
				signed = -signed
			}
			out = append(out, float64(signed)/float64(int64(1)<<fractionBits))
			value, scale = 0, 1
		} else {
			value += int64(c-63) * scale
			scale *= int64(base)
		}
	}
	return out
}

type plotter struct {
	colors    map[int]string
	widths    map[int]float64
	line      []hpPoint
	hasLine   bool
	penDown   bool
	relative  bool
	pen       int
	at        hpPoint
	polygon   [][]hpPoint
	inPolygon bool
	strokes   []hpStroke
}

func (p *plotter) color() string {
	if c, ok := p.colors[p.pen]; ok {
		return c
	}
	if p.pen > 0 && p.pen < 8 {
		return hpglColors[p.pen]
	}
	return hpglColors[1]
}

func (p *plotter) width() float64 {
	if w, ok := p.widths[p.pen]; ok {
		return w
	}
	return 0.35 * unitsPerMm
}

func (p *plotter) run(name, args string) {
	switch name {
	case "IN":
		p.endLine(false)
		p.penDown, p.relative = false, false
		p.pen = 1
		p.at = hpPoint{}
	case "SP":
		p.endLine(false)
		p.pen = 0
		if n := hpNumbers(args); len(n) > 0 {
			p.pen = int(n[0])
		}
	case "PC":
		if n := hpNumbers(args); len(n) >= 4 {
			ch := func(v float64) int { return int(math.Max(0, math.Min(255, v))) }
			p.colors[int(n[0])] = fmt.Sprintf("#%02X%02X%02X", ch(n[1]), ch(n[2]), ch(n[3]))
		}
	case "PW":
		n := hpNumbers(args)
		if len(n) >= 2 {
			p.widths[int(n[1])] = math.Max(0.05, n[0]) * unitsPerMm
		} else if len(n) == 1 {
			for k := 0; k < 256; k++ {
				p.widths[k] = math.Max(0.05, n[0]) * unitsPerMm
			}
		}
		p.endLine(false)
	case "PU", "PD":
		p.endLine(true)
		p.penDown = name == "PD"
		p.moveAll(hpNumbers(args))
	case "PA", "PR":
		p.relative = name == "PR"
		p.moveAll(hpNumbers(args))
	case "CI":
		if n := hpNumbers(args); len(n) > 0 {
			p.circle(n[0])
		}
	case "AA", "AR":
		if n := hpNumbers(args); len(n) >= 3 {
			c := hpPoint{n[0], n[1]}
			if name == "AR" {
				c = hpPoint{p.at.X + n[0], p.at.Y + n[1]}
			}
			p.arc(c, n[2])
		}
	case "PE":
		p.encoded([]rune(args))
	case "PM":
		p.endLine(true)
		mode := 0
		if n := hpNumbers(args); len(n) > 0 {
			mode = int(n[0])
		}
		if mode == 0 || p.polygon == nil {
			p.polygon = [][]hpPoint{}
		}
		p.inPolygon = mode != 2
	case "EP", "FP":
		p.endLine(false)
		for _, o := range p.polygon {
			if len(o) > 1 {
				p.strokes = append(p.strokes, hpStroke{p.color(), p.width(), o, name == "FP"})
			}
		}
	}
}

func (p *plotter) moveAll(c []float64) {
	for i := 0; i+1 < len(c); i += 2 {
		to := hpPoint{c[i], c[i+1]}
		if p.relative {
			to = hpPoint{p.at.X + c[i], p.at.Y + c[i+1]}
		}
		p.moveTo(to, p.penDown)
	}
}

func (p *plotter) moveTo(to hpPoint, draw bool) {
	if draw {
		if !p.hasLine {
			p.line = []hpPoint{p.at}
			p.hasLine = true
		}
		p.line = append(p.line, to)
	} else {
		p.endLine(true)
	}
	p.at = to
}

// endLine ends the line being drawn: in polygon mode it becomes an outline of
// the polygon (drawn by EP / FP), else a stroke.
func (p *plotter) endLine(keepPolygon bool) {
	if p.hasLine && len(p.line) > 1 {
		if p.inPolygon && keepPolygon {
			p.polygon = append(p.polygon, p.line)
		} else if p.pen != 0 {
			p.strokes = append(p.strokes, hpStroke{p.color(), p.width(), p.line, false})
		}
	}
	p.line, p.hasLine = nil, false
}

func (p *plotter) circle(radius float64) {
	p.endLine(true)
	c := p.at
	pts := make([]hpPoint, 73)
	for i := range pts {
		a := float64(i) * math.Pi / 36
		pts[i] = hpPoint{c.X + radius*math.Cos(a), c.Y + radius*math.Sin(a)}
	}
	if p.inPolygon {
		p.polygon = append(p.polygon, pts)
	} else if p.pen != 0 {
		p.strokes = append(p.strokes, hpStroke{p.color(), p.width(), pts, false})
	}
}

func (p *plotter) arc(c hpPoint, sweep float64) {
	r := math.Hypot(p.at.X-c.X, p.at.Y-c.Y)
	start := math.Atan2(p.at.Y-c.Y, p.at.X-c.X)
	steps := max(2, int(math.Ceil(math.Abs(sweep)/5)))
	for i := 1; i <= steps; i++ {
		a := start + sweep*math.Pi/180*float64(i)/float64(steps)
		p.moveTo(hpPoint{c.X + r*math.Cos(a), c.Y + r*math.Sin(a)}, p.penDown)
	}
}

// encoded runs PE: flags ':' (select pen), '<' (next point is a move), '>'
// (fraction bits), '=' (next point is absolute), '7' (7-bit mode); points
// are relative and drawn otherwise. The pen stays down after it.
func (p *plotter) encoded(data []rune) {
	penUp, absolute := false, false
	fractionBits, base := 0, 64
	var queue []float64
	i := 0
	for i < len(data) {
		flag := data[i]
		switch {
		case flag == ':' || flag == '<' || flag == '>' || flag == '=' || flag == '7':
			i++
			switch flag {
			case ':':
				pen := decodePE(data, &i, 0, base)
				if len(pen) > 0 {
					p.endLine(false)
					p.pen = int(pen[0])
					queue = append(queue, pen[1:]...)
				}
			case '<':
				penUp = true
			case '>':
				b := decodePE(data, &i, 0, base)
				if len(b) > 0 {
					fractionBits = int(b[0])
					queue = append(queue, b[1:]...)
				}
			case '=':
				absolute = true
			case '7':
				base = 32
			}
		case flag < 63:
			i++
		default:
			queue = append(queue, decodePE(data, &i, fractionBits, base)...)
		}
		k := 0
		for ; k+1 < len(queue); k += 2 {
			to := hpPoint{p.at.X + queue[k], p.at.Y + queue[k+1]}
			if absolute {
				to = hpPoint{queue[k], queue[k+1]}
			}
			p.moveTo(to, !penUp)
			penUp, absolute = false, false
		}
		queue = append(queue[:0], queue[k:]...)
	}
	p.penDown = true
}

func readHpgl(data []byte) ([]hpStroke, error) {
	p := &plotter{colors: map[int]string{}, widths: map[int]float64{}, pen: 1}
	for _, c := range hpglCommands(data) {
		p.run(c.Name, c.Args)
	}
	p.endLine(false)
	if len(p.strokes) == 0 {
		return nil, errors.New("no HP-GL drawing found: the plot file has no pen moves (it may be PostScript or PCL raster output)")
	}
	return p.strokes, nil
}

// HpglToSVG draws the plot at its real size, with a small margin and white paper.
func HpglToSVG(data []byte) (string, error) {
	strokes, err := readHpgl(data)
	if err != nil {
		return "", err
	}
	left, right := math.Inf(1), math.Inf(-1)
	bottom, top := math.Inf(1), math.Inf(-1)
	for _, s := range strokes {
		for _, q := range s.Points {
			left, right = math.Min(left, q.X), math.Max(right, q.X)
			bottom, top = math.Min(bottom, q.Y), math.Max(top, q.Y)
		}
	}
	margin := 2.0 * unitsPerMm
	left, right, bottom, top = left-margin, right+margin, bottom-margin, top+margin
	w, h := right-left, top-bottom
	n := func(v float64) string { return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64) }
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%smm" height="%smm" viewBox="0 0 %s %s">`, n(w/unitsPerMm), n(h/unitsPerMm), n(w), n(h))
	fmt.Fprintf(&b, `<rect width="%s" height="%s" fill="#FFFFFF"/>`, n(w), n(h))
	for _, s := range strokes {
		pts := make([]string, len(s.Points))
		for i, q := range s.Points {
			pts[i] = n(q.X-left) + "," + n(top-q.Y)
		}
		path := strings.Join(pts, " ")
		if s.Filled {
			fmt.Fprintf(&b, `<polygon points="%s" fill="%s"/>`, path, s.Color)
		} else {
			fmt.Fprintf(&b, `<polyline points="%s" fill="none" stroke="%s" stroke-width="%s" stroke-linecap="round" stroke-linejoin="round"/>`, path, s.Color, n(s.Width))
		}
	}
	b.WriteString("</svg>")
	return b.String(), nil
}
