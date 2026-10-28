// Ruled tables for the reader: tables drawn with lines become real tables.
//
// The page's vector paths give the rules: stroked straight lines and
// rectangle edges, and thin filled rectangles. Rules on one line are joined,
// rules that touch form a grid, and the grid's lines are the column and row
// edges. A unit of the grid without a rule between it and its neighbour is
// merged with it (a spanned cell). Words go into the cell under their centre.
//
// Conservative on purpose: a grid needs 2 rows and 2 columns, merged cells
// must be rectangles, text must sit in the cells (a rule through a word means
// a chart or a strike-through) and at least two cells hold text.

package pdf

import (
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const (
	maxRuleThickness = 4.0
	minRuleLength    = 4.0
	sameLine         = 1.5
	ruleGap          = 2.5
	edgeTolerance    = 3.0
)

// rule is horizontal (Position is y, Start..End x) or vertical (Position x, Start..End y).
type rule struct {
	Horizontal                      bool
	Position, Start, End, Thickness float64
	Color                           string
	Dashed                          bool
}

func (r rule) length() float64 { return r.End - r.Start }

type ruledCell struct {
	Row, Column, RowSpan, ColumnSpan int
	Bounds                           rect
	Words                            []*textWord
	Borders                          Borders
	Fill                             string
}

type ruledTable struct {
	Bounds  rect
	Columns []float64 // left to right
	Rows    []float64 // top to bottom
	Cells   []*ruledCell
	Padding float64
}

func (t *ruledTable) Text() string {
	var parts []string
	for _, c := range t.Cells {
		for _, w := range c.Words {
			parts = append(parts, w.Text)
		}
	}
	return strings.Join(parts, " ")
}

func isWhite(color string) bool {
	if len(color) != 7 {
		return false
	}
	ch := func(s string) int64 { v, _ := strconv.ParseInt(s, 16, 32); return v }
	return ch(color[1:3]) >= 242 && ch(color[3:5]) >= 242 && ch(color[5:7]) >= 242
}

type shade struct {
	Bounds rect
	Color  string
}

// pageRules: the rules of a page and the larger filled rectangles that may be cell shading.
func pageRules(paths []pagePath, crop rect) ([]rule, []shade) {
	var rules []rule
	var shades []shade
	for _, p := range paths {
		for si, sub := range p.Subpaths {
			type seg struct{ fx, fy, tx, ty float64 }
			var lines []seg
			curved := false
			for i := 1; i < len(sub); i++ {
				if sub[i].Curve {
					curved = true
					continue
				}
				lines = append(lines, seg{sub[i-1].X, sub[i-1].Y, sub[i].X, sub[i].Y})
			}
			if p.Closed[si] && len(sub) > 1 {
				lines = append(lines, seg{sub[len(sub)-1].X, sub[len(sub)-1].Y, sub[0].X, sub[0].Y})
			}
			for i := range lines {
				lines[i] = seg{lines[i].fx - crop.Left, lines[i].fy - crop.Bottom, lines[i].tx - crop.Left, lines[i].ty - crop.Bottom}
			}
			if p.Stroked && !isWhite(p.StrokeColor) {
				thickness := max(0.25, p.LineWidth)
				for _, l := range lines {
					if math.Abs(l.fy-l.ty) <= 0.5 && math.Abs(l.fx-l.tx) >= minRuleLength {
						rules = append(rules, rule{true, (l.fy + l.ty) / 2, min(l.fx, l.tx), max(l.fx, l.tx), thickness, p.StrokeColor, p.Dashed})
					} else if math.Abs(l.fx-l.tx) <= 0.5 && math.Abs(l.fy-l.ty) >= minRuleLength {
						rules = append(rules, rule{false, (l.fx + l.tx) / 2, min(l.fy, l.ty), max(l.fy, l.ty), thickness, p.StrokeColor, p.Dashed})
					}
				}
			}
			// A filled shape of straight, upright edges: a thin one is a rule, a larger one may shade a cell.
			if !p.Filled || curved || len(lines) < 3 || isWhite(p.FillColor) {
				continue
			}
			upright := true
			l, r, b, t := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
			for _, s := range lines {
				if math.Abs(s.fx-s.tx) > 0.5 && math.Abs(s.fy-s.ty) > 0.5 {
					upright = false
				}
				l, r = min(l, s.fx, s.tx), max(r, s.fx, s.tx)
				b, t = min(b, s.fy, s.ty), max(t, s.fy, s.ty)
			}
			if !upright {
				continue
			}
			w, h := r-l, t-b
			switch {
			case h <= maxRuleThickness && w >= minRuleLength && w > 2*h:
				rules = append(rules, rule{true, (t + b) / 2, l, r, max(0.25, h), p.FillColor, false})
			case w <= maxRuleThickness && h >= minRuleLength && h > 2*w:
				rules = append(rules, rule{false, (l + r) / 2, b, t, max(0.25, w), p.FillColor, false})
			case w > maxRuleThickness && h > maxRuleThickness:
				shades = append(shades, shade{rect{l, b, r, t}, p.FillColor})
			}
		}
	}
	return joinRules(rules), shades
}

// joinRules: rules on one line that overlap or nearly touch become one rule.
func joinRules(rules []rule) []rule {
	var out []rule
	for _, horizontal := range []bool{false, true} {
		var dir []rule
		for _, r := range rules {
			if r.Horizontal == horizontal {
				dir = append(dir, r)
			}
		}
		sort.SliceStable(dir, func(i, j int) bool { return dir[i].Position < dir[j].Position })
		for i := 0; i < len(dir); {
			j := i + 1
			for j < len(dir) && dir[j].Position-dir[j-1].Position <= sameLine {
				j++
			}
			group := slices.Clone(dir[i:j])
			sort.SliceStable(group, func(a, b int) bool { return group[a].Start < group[b].Start })
			var cur *rule
			for _, r := range group {
				if cur != nil && r.Start <= cur.End+ruleGap {
					longer := r.length() > cur.length()
					cur.End = max(cur.End, r.End)
					cur.Thickness = max(cur.Thickness, r.Thickness)
					// The longer piece decides where the line is and how it looks.
					if longer {
						cur.Position, cur.Color = r.Position, r.Color
					}
					cur.Dashed = cur.Dashed && r.Dashed
				} else {
					if cur != nil {
						out = append(out, *cur)
					}
					c := r
					cur = &c
				}
			}
			out = append(out, *cur)
			i = j
		}
	}
	return out
}

// ruledTables finds the tables of a page; the words in them are taken out of words.
func ruledTables(paths []pagePath, crop rect, words []*textWord) ([]*ruledTable, []*textWord) {
	rules, shades := pageRules(paths, crop)
	groups := grids(rules)
	sort.SliceStable(groups, func(i, j int) bool { return area(extent(groups[i])) > area(extent(groups[j])) })
	var tables []*ruledTable
	for _, g := range groups {
		ext := extent(g)
		if slices.ContainsFunc(tables, func(t *ruledTable) bool { return overlaps(t.Bounds, ext) }) {
			continue
		}
		t := makeTable(g, words, shades)
		if t == nil {
			continue
		}
		tables = append(tables, t)
		taken := map[*textWord]bool{}
		for _, c := range t.Cells {
			for _, w := range c.Words {
				taken[w] = true
			}
		}
		words = slices.DeleteFunc(slices.Clone(words), func(w *textWord) bool { return taken[w] })
	}
	return tables, words
}

// grids groups rules that touch (a horizontal and a vertical rule crossing or meeting).
func grids(rules []rule) [][]rule {
	parent := make([]int, len(rules))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	for a := range rules {
		if !rules[a].Horizontal {
			continue
		}
		for b := range rules {
			if !rules[b].Horizontal && touch(rules[a], rules[b]) {
				parent[find(a)] = find(b)
			}
		}
	}
	byRoot := map[int][]rule{}
	var order []int
	for i := range rules {
		r := find(i)
		if _, ok := byRoot[r]; !ok {
			order = append(order, r)
		}
		byRoot[r] = append(byRoot[r], rules[i])
	}
	var out [][]rule
	for _, r := range order {
		g := byRoot[r]
		h, v := false, false
		for _, x := range g {
			h = h || x.Horizontal
			v = v || !x.Horizontal
		}
		if h && v && len(g) >= 4 {
			out = append(out, g)
		}
	}
	return out
}

func touch(h, v rule) bool {
	reach := ruleGap + max(h.Thickness, v.Thickness)/2
	return v.Position >= h.Start-reach && v.Position <= h.End+reach && h.Position >= v.Start-reach && h.Position <= v.End+reach
}

func splitRules(g []rule) (hs, vs []rule) {
	for _, r := range g {
		if r.Horizontal {
			hs = append(hs, r)
		} else {
			vs = append(vs, r)
		}
	}
	return
}

// extent of a grid: its outer rules, or further out where most rules of the other direction reach.
func extent(g []rule) rect {
	hs, vs := splitRules(g)
	l, r := math.Inf(1), math.Inf(-1)
	for _, v := range vs {
		l, r = min(l, v.Position), max(r, v.Position)
	}
	b, t := math.Inf(1), math.Inf(-1)
	var hStarts, hEnds, vStarts, vEnds []float64
	for _, h := range hs {
		b, t = min(b, h.Position), max(t, h.Position)
		hStarts, hEnds = append(hStarts, h.Start), append(hEnds, h.End)
	}
	for _, v := range vs {
		vStarts, vEnds = append(vStarts, v.Start), append(vEnds, v.End)
	}
	return rect{min(l, median(hStarts)), min(b, median(vStarts)), max(r, median(hEnds)), max(t, median(vEnds))}
}

func area(b rect) float64 { return b.Width() * b.Height() }

func overlaps(a, b rect) bool {
	return min(a.Right, b.Right)-max(a.Left, b.Left) > edgeTolerance && min(a.Top, b.Top)-max(a.Bottom, b.Bottom) > edgeTolerance
}

// edges merges positions closer than edgeTolerance into their mean, sorted.
func edges(positions []float64) []float64 {
	s := slices.Clone(positions)
	sort.Float64s(s)
	var groups [][]float64
	for _, p := range s {
		if n := len(groups); n > 0 && p-groups[n-1][len(groups[n-1])-1] <= edgeTolerance {
			groups[n-1] = append(groups[n-1], p)
		} else {
			groups = append(groups, []float64{p})
		}
	}
	out := make([]float64, len(groups))
	for i, g := range groups {
		sum := 0.0
		for _, v := range g {
			sum += v
		}
		out[i] = sum / float64(len(g))
	}
	return out
}

// covering is the rule covering most of from..to on the line at position, or
// nil when rules cover less than half of it.
func covering(rules []rule, position, from, to float64) *rule {
	length := to - from
	if length <= 0 {
		return nil
	}
	covered, bestOverlap := 0.0, 0.0
	var best *rule
	for i := range rules {
		r := &rules[i]
		if math.Abs(r.Position-position) > edgeTolerance+r.Thickness/2 {
			continue
		}
		o := min(to, r.End) - max(from, r.Start)
		if o <= 0 {
			continue
		}
		covered += o
		if o > bestOverlap {
			best, bestOverlap = r, o
		}
	}
	if covered >= 0.5*length {
		return best
	}
	return nil
}

// makeTable builds the table a grid draws, with the words in its cells; nil when it does not look like one.
func makeTable(g []rule, words []*textWord, shades []shade) *ruledTable {
	ext := extent(g)
	hs, vs := splitRules(g)
	var xp, yp []float64
	for _, v := range vs {
		if v.Position > ext.Left && v.Position < ext.Right {
			xp = append(xp, v.Position)
		}
	}
	for _, h := range hs {
		if h.Position > ext.Bottom && h.Position < ext.Top {
			yp = append(yp, h.Position)
		}
	}
	xs := edges(append(xp, ext.Left, ext.Right))
	ys := edges(append(yp, ext.Bottom, ext.Top))
	slices.Reverse(ys) // top to bottom

	verticalEdge := func(c, r int) bool { return covering(vs, xs[c], ys[r+1], ys[r]) != nil }
	horizontalEdge := func(r, c int) bool { return covering(hs, ys[r], xs[c], xs[c+1]) != nil }
	// Grid lines that separate no two units anywhere are dropped.
	for {
		removed := false
		for c := 1; c < len(xs)-1; c++ {
			any := false
			for r := 0; r < len(ys)-1; r++ {
				if verticalEdge(c, r) {
					any = true
					break
				}
			}
			if !any {
				xs = slices.Delete(xs, c, c+1)
				removed = true
				break
			}
		}
		if removed {
			continue
		}
		for r := 1; r < len(ys)-1; r++ {
			any := false
			for c := 0; c < len(xs)-1; c++ {
				if horizontalEdge(r, c) {
					any = true
					break
				}
			}
			if !any {
				ys = slices.Delete(ys, r, r+1)
				removed = true
				break
			}
		}
		if !removed {
			break
		}
	}
	rows, columns := len(ys)-1, len(xs)-1
	if rows < 2 || columns < 2 || rows*columns > 2500 {
		return nil
	}

	// Units joined where no rule separates them; each group must be a rectangle.
	parent := make([]int, rows*columns)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	for r := 0; r < rows; r++ {
		for c := 0; c < columns; c++ {
			if c+1 < columns && !verticalEdge(c+1, r) {
				parent[find(r*columns+c)] = find(r*columns + c + 1)
			}
			if r+1 < rows && !horizontalEdge(r+1, c) {
				parent[find(r*columns+c)] = find((r+1)*columns + c)
			}
		}
	}
	units := map[int][]int{}
	var order []int
	for u := 0; u < rows*columns; u++ {
		root := find(u)
		if _, ok := units[root]; !ok {
			order = append(order, root)
		}
		units[root] = append(units[root], u)
	}
	owner := make([]int, rows*columns)
	var cells []*ruledCell
	for _, root := range order {
		unit := units[root]
		top, bottom, left, right := rows, -1, columns, -1
		for _, u := range unit {
			top, bottom = min(top, u/columns), max(bottom, u/columns)
			left, right = min(left, u%columns), max(right, u%columns)
		}
		if (bottom-top+1)*(right-left+1) != len(unit) {
			return nil // an L-shaped region: not a table cell
		}
		cell := &ruledCell{Row: top, Column: left, RowSpan: bottom - top + 1, ColumnSpan: right - left + 1,
			Bounds: rect{xs[left], ys[bottom+1], xs[right+1], ys[top]}}
		for _, u := range unit {
			owner[u] = len(cells)
		}
		cells = append(cells, cell)
	}
	if len(cells) < 4 {
		rowSet, colSet := map[int]bool{}, map[int]bool{}
		for _, c := range cells {
			rowSet[c.Row], colSet[c.Column] = true, true
		}
		if len(rowSet) < 2 || len(colSet) < 2 {
			return nil
		}
	}

	// Words by their centre; a rule through the middle of a word means this is no table around text.
	bounds := rect{xs[0], ys[len(ys)-1], xs[len(xs)-1], ys[0]}
	for _, w := range words {
		x, y := (w.Left+w.Right)/2, (w.Top+w.Bottom)/2
		if x <= bounds.Left || x >= bounds.Right || y <= bounds.Bottom || y >= bounds.Top {
			continue
		}
		band := (w.Top - w.Bottom) * 0.3
		for _, v := range vs {
			if v.Position > w.Left+1 && v.Position < w.Right-1 && v.Start < y && v.End > y {
				return nil
			}
		}
		for _, h := range hs {
			if h.Position > w.Bottom+band && h.Position < w.Top-band && h.Start < w.Right-1 && h.End > w.Left+1 {
				return nil
			}
		}
		column := 0
		for i, e := range xs {
			if e <= x {
				column = i
			}
		}
		row := 0
		for i, e := range ys {
			if e >= y {
				row = i
			}
		}
		column, row = max(0, min(column, columns-1)), max(0, min(row, rows-1))
		cells[owner[row*columns+column]].Words = append(cells[owner[row*columns+column]].Words, w)
	}
	withText := 0
	for _, c := range cells {
		if len(c.Words) > 0 {
			withText++
		}
	}
	if withText < 2 {
		return nil
	}
	for _, c := range cells {
		b := c.Bounds
		c.Borders = Borders{
			Left:   border(covering(vs, b.Left, b.Bottom, b.Top)),
			Right:  border(covering(vs, b.Right, b.Bottom, b.Top)),
			Top:    border(covering(hs, b.Top, b.Left, b.Right)),
			Bottom: border(covering(hs, b.Bottom, b.Left, b.Right)),
		}
		// Shading: a filled rectangle inside the table covering most of the cell.
		for i := len(shades) - 1; i >= 0; i-- {
			s := shades[i]
			if s.Bounds.Left >= bounds.Left-edgeTolerance && s.Bounds.Right <= bounds.Right+edgeTolerance &&
				s.Bounds.Bottom >= bounds.Bottom-edgeTolerance && s.Bounds.Top <= bounds.Top+edgeTolerance &&
				intersection(s.Bounds, b) >= 0.8*area(b) {
				c.Fill = s.Color
				break
			}
		}
	}
	t := &ruledTable{Bounds: bounds, Columns: xs, Rows: ys, Cells: cells}
	padding := math.Inf(1)
	for _, c := range cells {
		for _, w := range c.Words {
			padding = min(padding, w.Left-c.Bounds.Left)
		}
	}
	t.Padding = max(1, min(padding, 8))
	return t
}

func border(r *rule) Border {
	if r == nil {
		return Border{}
	}
	style := BorderSolid
	if r.Dashed {
		style = BorderDash
	}
	return Border{style, max(0.1, r.Thickness*25.4/72), r.Color}
}

func intersection(a, b rect) float64 {
	return max(0, min(a.Right, b.Right)-max(a.Left, b.Left)) * max(0, min(a.Top, b.Top)-max(a.Bottom, b.Bottom))
}

// tableBlock: column widths and row heights from the PDF, each cell's text
// read like a page, borders and shading per cell.
func tableBlock(it *item, lineRatio float64) *Table {
	g := it.Table
	pad := g.Padding
	t := &Table{ColumnCount: len(g.Columns) - 1, CellMargin: Insets{hwp(pad), hwp(pad), hwp(1), hwp(1)}}
	for i := 1; i < len(g.Columns); i++ {
		t.ColumnWidths = append(t.ColumnWidths, hwp(g.Columns[i]-g.Columns[i-1]))
	}
	containerWidth := it.ContainerRight - it.ContainerLeft
	center := (g.Bounds.Left + g.Bounds.Right) / 2
	if math.Abs(center-(it.ContainerLeft+it.ContainerRight)/2) < max(6, containerWidth*0.05) && g.Bounds.Width() < containerWidth*0.9 {
		t.Align = AlignCenter
	} else if g.Bounds.Left-it.ContainerLeft > 2 {
		t.Indent = hwp(g.Bounds.Left - it.ContainerLeft)
	}
	for r := 0; r < len(g.Rows)-1; r++ {
		t.Rows = append(t.Rows, &Row{Height: hwp(g.Rows[r] - g.Rows[r+1])})
	}
	cells := slices.Clone(g.Cells)
	sort.SliceStable(cells, func(i, j int) bool {
		if cells[i].Row != cells[j].Row {
			return cells[i].Row < cells[j].Row
		}
		return cells[i].Column < cells[j].Column
	})
	noHeadings := map[float64]int{}
	for _, c := range cells {
		hc := &Cell{Column: c.Column, RowSpan: c.RowSpan, ColSpan: c.ColumnSpan, Borders: c.Borders, Fill: c.Fill}
		if len(c.Words) > 0 {
			items := textItems(c.Words)
			sort.SliceStable(items, func(i, j int) bool {
				if items[i].Top() != items[j].Top() {
					return items[i].Top() > items[j].Top()
				}
				return items[i].Left() < items[j].Left()
			})
			var previous *item
			for _, p := range items {
				p.ContainerLeft, p.ContainerRight = c.Bounds.Left+pad, c.Bounds.Right-pad
				hc.Blocks = append(hc.Blocks, textParagraph(p, previous, noHeadings, lineRatio))
				previous = p
			}
			hc.VerticalAlign = verticalAlign(c, items)
		}
		t.Rows[c.Row].Cells = append(t.Rows[c.Row].Cells, hc)
	}
	return t
}

// verticalAlign: text about as far from the top as from the bottom of a
// taller cell is centred; text at the bottom stays there.
func verticalAlign(c *ruledCell, items []*item) VerticalAlign {
	top, bottom := math.Inf(-1), math.Inf(1)
	for _, it := range items {
		top, bottom = max(top, it.Top()), min(bottom, it.Bottom())
	}
	above := c.Bounds.Top - top
	below := bottom - c.Bounds.Bottom
	if above < 0 || below < 0 {
		return VAlignTop
	}
	if math.Abs(above-below) <= max(2, 0.25*(above+below)) && above+below > 4 {
		return VAlignCenter
	}
	if below < 0.4*above && above > items[0].Size()*1.5 {
		return VAlignBottom
	}
	return VAlignTop
}
