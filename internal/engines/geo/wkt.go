package geo

// Well-known text (WKT / EWKT) for geometries: POINT, LINESTRING, POLYGON,
// their MULTI* forms and GEOMETRYCOLLECTION, with Z (or M, which is dropped)
// and EMPTY. Used for the geometry column of a CSV. An "SRID=4326;" prefix is
// accepted and ignored (the CSV reader treats coordinates as longitude / latitude).

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

const wktGeometryMaxDepth = 16

// parseWkt parses WKT; the error says what is wrong. An empty geometry gives nil.
func parseWkt(text string) (Geometry, error) {
	p := &wktGeomParser{text: []rune(text)}
	g, err := p.geometry(0)
	if err != nil {
		return nil, err
	}
	p.skip()
	if p.pos < len(p.text) {
		return nil, fmt.Errorf("unexpected text at position %d", p.pos)
	}
	return g, nil
}

// writeWkt writes a geometry as WKT.
func writeWkt(g Geometry) string {
	var b strings.Builder
	wktWrite(&b, g)
	return b.String()
}

func wktWrite(b *strings.Builder, g Geometry) {
	z := hasZ(g)
	if c, ok := g.(*Collection); ok {
		// "GEOMETRYCOLLECTION" has no Z of its own: members carry it.
		b.WriteString("GEOMETRYCOLLECTION (")
		for i, m := range c.Geometries {
			if i > 0 {
				b.WriteString(", ")
			}
			wktWrite(b, m)
		}
		b.WriteString(")")
		return
	}
	b.WriteString(strings.ToUpper(g.TypeName()))
	if z {
		b.WriteString(" Z ")
	} else {
		b.WriteString(" ")
	}
	switch x := g.(type) {
	case *Point:
		b.WriteString("(")
		wktPosition(b, x.Position, z)
		b.WriteString(")")
	case *MultiPoint:
		b.WriteString("(")
		for i, c := range x.Points {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString("(")
			wktPosition(b, c, z)
			b.WriteString(")")
		}
		b.WriteString(")")
	case *LineString:
		wktLine(b, x.Points, z)
	case *MultiLineString:
		b.WriteString("(")
		for i, l := range x.Lines {
			if i > 0 {
				b.WriteString(", ")
			}
			wktLine(b, l, z)
		}
		b.WriteString(")")
	case *Polygon:
		wktPolygon(b, x, z)
	case *MultiPolygon:
		b.WriteString("(")
		for i, p := range x.Polygons {
			if i > 0 {
				b.WriteString(", ")
			}
			wktPolygon(b, p, z)
		}
		b.WriteString(")")
	}
}

func wktPolygon(b *strings.Builder, p *Polygon, z bool) {
	b.WriteString("(")
	for i, r := range p.Rings {
		if i > 0 {
			b.WriteString(", ")
		}
		wktLine(b, oriented(r, i == 0), z)
	}
	b.WriteString(")")
}

func wktLine(b *strings.Builder, points []Coord, z bool) {
	b.WriteString("(")
	for i, c := range points {
		if i > 0 {
			b.WriteString(", ")
		}
		wktPosition(b, c, z)
	}
	b.WriteString(")")
}

func wktPosition(b *strings.Builder, c Coord, z bool) {
	b.WriteString(degrees(c.X))
	b.WriteString(" ")
	b.WriteString(degrees(c.Y))
	if z {
		h := 0.0
		if c.HasZ() {
			h = c.Z
		}
		b.WriteString(" ")
		b.WriteString(height(h))
	}
}

type wktGeomParser struct {
	text []rune
	pos  int
}

type dimension struct{ z, m bool }

var wktKinds = map[string]bool{"POINT": true, "LINESTRING": true, "POLYGON": true, "MULTIPOINT": true,
	"MULTILINESTRING": true, "MULTIPOLYGON": true, "GEOMETRYCOLLECTION": true}

func (p *wktGeomParser) geometry(depth int) (Geometry, error) {
	if depth > wktGeometryMaxDepth {
		return nil, errors.New("geometry nested too deeply")
	}
	p.skip()
	// EWKT: "SRID=4326;POINT(...)".
	if rest := string(p.text[p.pos:min(p.pos+5, len(p.text))]); strings.EqualFold(rest, "SRID=") {
		semicolon := -1
		for i := p.pos; i < len(p.text); i++ {
			if p.text[i] == ';' {
				semicolon = i
				break
			}
		}
		if semicolon < 0 {
			return nil, errors.New("SRID prefix without ';'")
		}
		p.pos = semicolon + 1
		p.skip()
	}
	word := p.word()
	if word == "" {
		return nil, fmt.Errorf("expected a geometry type at position %d", p.pos)
	}
	kind := strings.ToUpper(word)
	// "POINTZ", "POINT Z", "POINT ZM", "POINT M".
	var dim dimension
	for _, suffix := range []string{"ZM", "Z", "M"} {
		if len(kind) > len(suffix) && strings.HasSuffix(kind, suffix) && wktKinds[kind[:len(kind)-len(suffix)]] {
			dim = dimension{strings.Contains(suffix, "Z"), strings.Contains(suffix, "M")}
			kind = kind[:len(kind)-len(suffix)]
			break
		}
	}
	p.skip()
	switch flag := p.peekWord(); flag {
	case "Z", "ZM", "M", "z", "zm", "m":
		p.word()
		f := strings.ToUpper(flag)
		dim = dimension{strings.Contains(f, "Z"), strings.Contains(f, "M")}
		p.skip()
	}
	if strings.EqualFold(p.peekWord(), "EMPTY") {
		p.word()
		return nil, nil
	}
	switch kind {
	case "POINT":
		if err := p.open(); err != nil {
			return nil, err
		}
		c, err := p.position(dim)
		if err != nil {
			return nil, err
		}
		if err := p.close(); err != nil {
			return nil, err
		}
		return &Point{c}, nil
	case "LINESTRING":
		line, err := p.line(dim)
		if err != nil {
			return nil, err
		}
		if len(line) < 2 {
			return nil, errors.New("a LINESTRING needs two positions")
		}
		return &LineString{Points: line}, nil
	case "POLYGON":
		poly, err := p.polygon(dim)
		if err != nil || poly == nil {
			return nil, err
		}
		return poly, nil
	case "MULTIPOINT":
		if err := p.open(); err != nil {
			return nil, err
		}
		var points []Coord
		for {
			p.skip()
			switch {
			case p.peek() == '(':
				p.open()
				c, err := p.position(dim)
				if err != nil {
					return nil, err
				}
				if err := p.close(); err != nil {
					return nil, err
				}
				points = append(points, c)
			case strings.EqualFold(p.peekWord(), "EMPTY"):
				p.word()
			default:
				c, err := p.position(dim)
				if err != nil {
					return nil, err
				}
				points = append(points, c)
			}
			if !p.comma() {
				break
			}
		}
		if err := p.close(); err != nil {
			return nil, err
		}
		if len(points) == 0 {
			return nil, nil
		}
		return &MultiPoint{points}, nil
	case "MULTILINESTRING":
		if err := p.open(); err != nil {
			return nil, err
		}
		var lines [][]Coord
		for {
			if !p.tryEmpty() {
				line, err := p.line(dim)
				if err != nil {
					return nil, err
				}
				if len(line) >= 2 {
					lines = append(lines, line)
				}
			}
			if !p.comma() {
				break
			}
		}
		if err := p.close(); err != nil {
			return nil, err
		}
		if len(lines) == 0 {
			return nil, nil
		}
		return &MultiLineString{lines}, nil
	case "MULTIPOLYGON":
		if err := p.open(); err != nil {
			return nil, err
		}
		var polygons []*Polygon
		for {
			if !p.tryEmpty() {
				poly, err := p.polygon(dim)
				if err != nil {
					return nil, err
				}
				if poly != nil {
					polygons = append(polygons, poly)
				}
			}
			if !p.comma() {
				break
			}
		}
		if err := p.close(); err != nil {
			return nil, err
		}
		if len(polygons) == 0 {
			return nil, nil
		}
		return &MultiPolygon{polygons}, nil
	case "GEOMETRYCOLLECTION":
		if err := p.open(); err != nil {
			return nil, err
		}
		var parts []Geometry
		for {
			g, err := p.geometry(depth + 1)
			if err != nil {
				return nil, err
			}
			if g != nil {
				parts = append(parts, g)
			}
			if !p.comma() {
				break
			}
		}
		if err := p.close(); err != nil {
			return nil, err
		}
		if len(parts) == 0 {
			return nil, nil
		}
		return &Collection{parts}, nil
	}
	return nil, fmt.Errorf("unknown geometry type %q", word)
}

func (p *wktGeomParser) polygon(dim dimension) (*Polygon, error) {
	if err := p.open(); err != nil {
		return nil, err
	}
	var rings [][]Coord
	for {
		if !p.tryEmpty() {
			line, err := p.line(dim)
			if err != nil {
				return nil, err
			}
			if r := closeRing(line); r != nil {
				rings = append(rings, r)
			}
		}
		if !p.comma() {
			break
		}
	}
	if err := p.close(); err != nil {
		return nil, err
	}
	if len(rings) == 0 {
		return nil, nil
	}
	return &Polygon{rings}, nil
}

func (p *wktGeomParser) line(dim dimension) ([]Coord, error) {
	if err := p.open(); err != nil {
		return nil, err
	}
	var line []Coord
	for {
		c, err := p.position(dim)
		if err != nil {
			return nil, err
		}
		line = append(line, c)
		if !p.comma() {
			break
		}
	}
	return line, p.close()
}

func (p *wktGeomParser) position(dim dimension) (Coord, error) {
	var numbers []float64
	for {
		p.skip()
		start := p.pos
		for p.pos < len(p.text) && (p.text[p.pos] >= '0' && p.text[p.pos] <= '9' || strings.ContainsRune("-+.eE", p.text[p.pos])) {
			p.pos++
		}
		if p.pos == start {
			break
		}
		v, err := strconv.ParseFloat(string(p.text[start:p.pos]), 64)
		if err != nil || !isFinite(v) {
			return Coord{}, fmt.Errorf("bad number at position %d", start)
		}
		numbers = append(numbers, v)
	}
	if len(numbers) < 2 {
		return Coord{}, fmt.Errorf("a position needs two numbers (position %d)", p.pos)
	}
	// X Y [Z] [M]: with M only (POINT M) the third number is the measure; without a flag three numbers mean Z.
	z := math.NaN()
	if len(numbers) >= 3 && (dim.z || !dim.m) {
		z = numbers[2]
	}
	return Coord{numbers[0], numbers[1], z}, nil
}

func (p *wktGeomParser) tryEmpty() bool {
	p.skip()
	if !strings.EqualFold(p.peekWord(), "EMPTY") {
		return false
	}
	p.word()
	return true
}

func (p *wktGeomParser) open() error {
	p.skip()
	if p.peek() != '(' {
		return fmt.Errorf("expected '(' at position %d", p.pos)
	}
	p.pos++
	return nil
}

func (p *wktGeomParser) close() error {
	p.skip()
	if p.peek() != ')' {
		return fmt.Errorf("expected ')' at position %d", p.pos)
	}
	p.pos++
	return nil
}

func (p *wktGeomParser) comma() bool {
	p.skip()
	if p.peek() != ',' {
		return false
	}
	p.pos++
	return true
}

func (p *wktGeomParser) peek() rune {
	if p.pos < len(p.text) {
		return p.text[p.pos]
	}
	return 0
}

func (p *wktGeomParser) skip() {
	for p.pos < len(p.text) && unicode.IsSpace(p.text[p.pos]) {
		p.pos++
	}
}

func (p *wktGeomParser) word() string {
	start := p.pos
	for p.pos < len(p.text) && (p.text[p.pos] >= 'a' && p.text[p.pos] <= 'z' || p.text[p.pos] >= 'A' && p.text[p.pos] <= 'Z') {
		p.pos++
	}
	return string(p.text[start:p.pos])
}

func (p *wktGeomParser) peekWord() string {
	saved := p.pos
	w := p.word()
	p.pos = saved
	return w
}
