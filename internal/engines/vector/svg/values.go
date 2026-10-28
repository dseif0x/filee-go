package svg

import (
	"math"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
)

// Length is a number with a unit.
type Length struct {
	V    float64
	Unit string // "", "px", "%", "em", "ex", "mm", "cm", "in", "pt", "pc", "q", "rem", "vw", "vh"
}

// numberPrefix returns the length of the number at the start of s.
func numberPrefix(s string) int {
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	digits := false
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		digits = true
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			digits = true
		}
	}
	if !digits {
		return 0
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		k := j
		for k < len(s) && s[k] >= '0' && s[k] <= '9' {
			k++
		}
		if k > j {
			i = k
		}
	}
	return i
}

func parseLength(s string) (Length, bool) {
	s = strings.TrimSpace(s)
	n := numberPrefix(s)
	if n == 0 {
		return Length{}, false
	}
	v, err := strconv.ParseFloat(s[:n], 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return Length{}, false
	}
	unit := strings.ToLower(strings.TrimSpace(s[n:]))
	switch unit {
	case "", "px", "%", "em", "ex", "mm", "cm", "in", "pt", "pc", "q", "rem", "vw", "vh", "vmin", "vmax", "ch":
	default:
		return Length{}, false
	}
	return Length{v, unit}, true
}

// lengthContext resolves relative units.
type lengthContext struct {
	vw, vh   float64 // viewport size for percentages
	fontSize float64
}

const (
	axisX = iota
	axisY
	axisOther
)

func (c lengthContext) resolve(l Length, axis int) float64 {
	switch l.Unit {
	case "", "px":
		return l.V
	case "%":
		switch axis {
		case axisX:
			return l.V / 100 * c.vw
		case axisY:
			return l.V / 100 * c.vh
		}
		return l.V / 100 * math.Sqrt((c.vw*c.vw+c.vh*c.vh)/2)
	case "em", "rem":
		return l.V * c.fontSize
	case "ex", "ch":
		return l.V * c.fontSize / 2
	case "mm":
		return l.V * 96 / 25.4
	case "cm":
		return l.V * 96 / 2.54
	case "q":
		return l.V * 96 / 101.6
	case "in":
		return l.V * 96
	case "pt":
		return l.V * 96 / 72
	case "pc":
		return l.V * 16
	case "vw":
		return l.V / 100 * c.vw
	case "vh":
		return l.V / 100 * c.vh
	case "vmin":
		return l.V / 100 * math.Min(c.vw, c.vh)
	case "vmax":
		return l.V / 100 * math.Max(c.vw, c.vh)
	}
	return l.V
}

// parseNumbers reads a list of numbers separated by spaces and/or commas.
func parseNumbers(s string) []float64 {
	var out []float64
	for len(s) > 0 {
		s = strings.TrimLeft(s, " \t\r\n,")
		n := numberPrefix(s)
		if n == 0 {
			break
		}
		v, err := strconv.ParseFloat(s[:n], 64)
		if err != nil {
			break
		}
		out = append(out, v)
		s = s[n:]
	}
	return out
}

// parseLengthList reads lists like "10 20% 3em".
func parseLengthList(s string) []Length {
	var out []Length
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' }) {
		l, ok := parseLength(f)
		if !ok {
			return out
		}
		out = append(out, l)
	}
	return out
}

func parseFloat(s string, def float64) float64 {
	s = strings.TrimSpace(s)
	n := numberPrefix(s)
	if n == 0 {
		return def
	}
	v, err := strconv.ParseFloat(s[:n], 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return def
	}
	if strings.HasSuffix(s, "%") {
		v /= 100
	}
	return v
}

// parseTransform reads an SVG transform list.
func parseTransform(s string) gfx.Matrix {
	m := gfx.Identity
	s = strings.TrimSpace(s)
	for len(s) > 0 {
		s = strings.TrimLeft(s, " \t\r\n,")
		open := strings.IndexByte(s, '(')
		if open < 0 {
			break
		}
		name := strings.ToLower(strings.TrimSpace(s[:open]))
		end := strings.IndexByte(s[open:], ')')
		if end < 0 {
			break
		}
		args := parseNumbers(s[open+1 : open+end])
		s = s[open+end+1:]
		arg := func(i int, def float64) float64 {
			if i < len(args) {
				return args[i]
			}
			return def
		}
		var t gfx.Matrix
		switch name {
		case "matrix":
			if len(args) < 6 {
				return m
			}
			t = gfx.Matrix{A: args[0], B: args[1], C: args[2], D: args[3], E: args[4], F: args[5]}
		case "translate":
			t = gfx.Translate(arg(0, 0), arg(1, 0))
		case "scale":
			sx := arg(0, 1)
			t = gfx.Scale(sx, arg(1, sx))
		case "rotate":
			a := arg(0, 0) * math.Pi / 180
			cx, cy := arg(1, 0), arg(2, 0)
			t = gfx.Translate(cx, cy).Mul(gfx.Rotate(a)).Mul(gfx.Translate(-cx, -cy))
		case "skewx":
			t = gfx.SkewX(arg(0, 0) * math.Pi / 180)
		case "skewy":
			t = gfx.SkewY(arg(0, 0) * math.Pi / 180)
		default:
			return m
		}
		m = m.Mul(t)
	}
	return m
}

// parsePathData reads SVG path data; it stops at the first error, keeping
// what was read (as the SVG error handling rules say).
func parsePathData(d string) *gfx.Path {
	p := gfx.NewPath()
	s := d
	var cmd byte
	var cur, start, lastCtrl gfx.Point
	var lastCmd byte
	num := func() (float64, bool) {
		s = strings.TrimLeft(s, " \t\r\n,")
		n := numberPrefix(s)
		if n == 0 {
			return 0, false
		}
		v, err := strconv.ParseFloat(s[:n], 64)
		if err != nil {
			return 0, false
		}
		s = s[n:]
		return v, true
	}
	flag := func() (bool, bool) {
		s = strings.TrimLeft(s, " \t\r\n,")
		if len(s) == 0 || (s[0] != '0' && s[0] != '1') {
			return false, false
		}
		f := s[0] == '1'
		s = s[1:]
		return f, true
	}
	for {
		s = strings.TrimLeft(s, " \t\r\n,")
		if len(s) == 0 {
			break
		}
		c := s[0]
		if strings.IndexByte("MmLlHhVvCcSsQqTtAaZz", c) >= 0 {
			cmd = c
			s = s[1:]
		} else if cmd == 0 {
			break
		} else if cmd == 'M' {
			cmd = 'L'
		} else if cmd == 'm' {
			cmd = 'l'
		} else if cmd == 'Z' || cmd == 'z' {
			break
		}
		rel := cmd >= 'a'
		off := gfx.Point{}
		if rel {
			off = cur
		}
		ok := true
		switch cmd | 0x20 {
		case 'm':
			x, ok1 := num()
			y, ok2 := num()
			if ok = ok1 && ok2; ok {
				cur = gfx.Point{X: x + off.X, Y: y + off.Y}
				start = cur
				p.MoveTo(cur.X, cur.Y)
			}
		case 'l':
			x, ok1 := num()
			y, ok2 := num()
			if ok = ok1 && ok2; ok {
				cur = gfx.Point{X: x + off.X, Y: y + off.Y}
				p.LineTo(cur.X, cur.Y)
			}
		case 'h':
			var x float64
			if x, ok = num(); ok {
				cur.X = x + off.X
				p.LineTo(cur.X, cur.Y)
			}
		case 'v':
			var y float64
			if y, ok = num(); ok {
				cur.Y = y + off.Y
				p.LineTo(cur.X, cur.Y)
			}
		case 'c':
			v := make([]float64, 6)
			for i := range v {
				if v[i], ok = num(); !ok {
					break
				}
			}
			if ok {
				c1 := gfx.Point{X: v[0] + off.X, Y: v[1] + off.Y}
				c2 := gfx.Point{X: v[2] + off.X, Y: v[3] + off.Y}
				cur = gfx.Point{X: v[4] + off.X, Y: v[5] + off.Y}
				p.CubeTo(c1.X, c1.Y, c2.X, c2.Y, cur.X, cur.Y)
				lastCtrl = c2
			}
		case 's':
			v := make([]float64, 4)
			for i := range v {
				if v[i], ok = num(); !ok {
					break
				}
			}
			if ok {
				c1 := cur
				if lc := lastCmd | 0x20; lc == 'c' || lc == 's' {
					c1 = cur.Mul(2).Sub(lastCtrl)
				}
				c2 := gfx.Point{X: v[0] + off.X, Y: v[1] + off.Y}
				cur = gfx.Point{X: v[2] + off.X, Y: v[3] + off.Y}
				p.CubeTo(c1.X, c1.Y, c2.X, c2.Y, cur.X, cur.Y)
				lastCtrl = c2
			}
		case 'q':
			v := make([]float64, 4)
			for i := range v {
				if v[i], ok = num(); !ok {
					break
				}
			}
			if ok {
				c := gfx.Point{X: v[0] + off.X, Y: v[1] + off.Y}
				cur = gfx.Point{X: v[2] + off.X, Y: v[3] + off.Y}
				p.QuadTo(c.X, c.Y, cur.X, cur.Y)
				lastCtrl = c
			}
		case 't':
			x, ok1 := num()
			y, ok2 := num()
			if ok = ok1 && ok2; ok {
				c := cur
				if lc := lastCmd | 0x20; lc == 'q' || lc == 't' {
					c = cur.Mul(2).Sub(lastCtrl)
				}
				cur = gfx.Point{X: x + off.X, Y: y + off.Y}
				p.QuadTo(c.X, c.Y, cur.X, cur.Y)
				lastCtrl = c
			}
		case 'a':
			rx, ok1 := num()
			ry, ok2 := num()
			rot, ok3 := num()
			large, ok4 := flag()
			sweep, ok5 := flag()
			x, ok6 := num()
			y, ok7 := num()
			if ok = ok1 && ok2 && ok3 && ok4 && ok5 && ok6 && ok7; ok {
				cur = gfx.Point{X: x + off.X, Y: y + off.Y}
				p.ArcTo(rx, ry, rot*math.Pi/180, large, sweep, cur.X, cur.Y)
			}
		case 'z':
			p.Close()
			cur = start
		}
		if !ok {
			break
		}
		lastCmd = cmd
	}
	return p
}

// parsePoints reads a points list (polyline / polygon); an odd count drops the last.
func parsePoints(s string) []gfx.Point {
	nums := parseNumbers(s)
	pts := make([]gfx.Point, 0, len(nums)/2)
	for i := 0; i+1 < len(nums); i += 2 {
		pts = append(pts, gfx.Point{X: nums[i], Y: nums[i+1]})
	}
	return pts
}

// viewBoxTransform maps a viewBox into a viewport per preserveAspectRatio.
func viewBoxTransform(vb [4]float64, par string, x, y, w, h float64) gfx.Matrix {
	if vb[2] <= 0 || vb[3] <= 0 {
		return gfx.Translate(x, y)
	}
	sx, sy := w/vb[2], h/vb[3]
	fields := strings.Fields(par)
	align := "xmidymid"
	slice := false
	if len(fields) > 0 {
		f := fields[0]
		if f == "defer" && len(fields) > 1 {
			fields = fields[1:]
			f = fields[0]
		}
		align = strings.ToLower(f)
		if len(fields) > 1 && strings.ToLower(fields[1]) == "slice" {
			slice = true
		}
	}
	if align == "none" {
		return gfx.Translate(x, y).Mul(gfx.Scale(sx, sy)).Mul(gfx.Translate(-vb[0], -vb[1]))
	}
	s := math.Min(sx, sy)
	if slice {
		s = math.Max(sx, sy)
	}
	tx := x - vb[0]*s
	ty := y - vb[1]*s
	cw, ch := vb[2]*s, vb[3]*s
	switch {
	case strings.Contains(align, "xmid"):
		tx += (w - cw) / 2
	case strings.Contains(align, "xmax"):
		tx += w - cw
	}
	switch {
	case strings.Contains(align, "ymid"):
		ty += (h - ch) / 2
	case strings.Contains(align, "ymax"):
		ty += h - ch
	}
	return gfx.Translate(tx, ty).Mul(gfx.Scale(s, s))
}

func parseViewBox(s string) ([4]float64, bool) {
	n := parseNumbers(s)
	if len(n) != 4 || n[2] <= 0 || n[3] <= 0 {
		return [4]float64{}, false
	}
	return [4]float64{n[0], n[1], n[2], n[3]}, true
}

// urlRef extracts the id of "url(#id)" (or "#id"); rest is what follows (a fallback).
func urlRef(s string) (id, rest string, ok bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(strings.ToLower(s), "url(") {
		end := strings.IndexByte(s, ')')
		if end < 0 {
			return "", "", false
		}
		inner := strings.Trim(strings.TrimSpace(s[4:end]), `"'`)
		rest = strings.TrimSpace(s[end+1:])
		if i := strings.IndexByte(inner, '#'); i >= 0 {
			return inner[i+1:], rest, true
		}
		return "", rest, false
	}
	if strings.HasPrefix(s, "#") {
		return s[1:], "", true
	}
	return "", "", false
}
