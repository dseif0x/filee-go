package ofd

import (
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/xdom"
)

// Ruled tables: grids drawn with lines become real tables — the frame of an
// e-invoice and the forms of official documents. Path objects give the rules
// (stroked straight segments, thin filled rectangles), rules on one line are
// joined, rules that touch form a grid, the grid's lines are the column and
// row edges, and units without a rule between them merge into one cell. Text
// goes into the cell its first character is in. Conservative: a grid needs
// 2 rows and 2 columns, cells must be rectangles and at least two hold text.

const (
	maxRuleThickness = 1.5
	minRuleLength    = 1.5
	sameLine         = 0.5
	ruleGap          = 0.9
	edgeTolerance    = 1.0
	maxSegments      = 5000
)

// rule: horizontal (position is y, start..end is x) or vertical.
type rule struct {
	horizontal                  bool
	position, start, end, thick float64
	color                       string
}

func (r rule) length() float64 { return r.end - r.start }

type shade struct {
	left, top, right, bottom float64
	color                    string
}

type ruledCell struct {
	row, column, rowSpan, columnSpan int
	left, top, right, bottom         float64
	fragments                        []*fragment
	borders                          doc.Borders
	fill                             string
}

type ruledTable struct {
	columns, rows []float64
	cells         []*ruledCell
}

func (t *ruledTable) left() float64   { return t.columns[0] }
func (t *ruledTable) right() float64  { return t.columns[len(t.columns)-1] }
func (t *ruledTable) top() float64    { return t.rows[0] }
func (t *ruledTable) bottom() float64 { return t.rows[len(t.rows)-1] }

type point struct{ x, y float64 }
type segment struct{ from, to point }

func pathObject(el *xdom.Node, res *resources, page *pageContent, layerDraw string) {
	data := child(el, "AbbreviatedData").Value()
	if page.segments > maxSegments || data == "" {
		return
	}
	b, _ := box(attr(el, "Boundary"))
	ctm := parseMatrix(attr(el, "CTM"))
	own, layer := res.draw(attr(el, "DrawParam")), res.draw(layerDraw)
	stroke := ""
	if attr(el, "Stroke") != "false" {
		stroke = first(colorOf(child(el, "StrokeColor")), own.stroke, layer.stroke, "#000000")
	}
	// Without a colour a fill is transparent (the standard's default).
	fill := ""
	if attr(el, "Fill") == "true" {
		fill = first(colorOf(child(el, "FillColor")), own.fill, layer.fill)
	}
	lw := 0.353
	if v, ok := number(attr(el, "LineWidth")); ok {
		lw = v
	} else if own.lineWidth != nil {
		lw = *own.lineWidth
	} else if layer.lineWidth != nil {
		lw = *layer.lineWidth
	}
	lw *= (ctm.scaleX() + ctm.scaleY()) / 2
	pt := func(x, y float64) point {
		px, py := ctm.apply(x, y)
		return point{b[0] + px, b[1] + py}
	}
	tokens := strings.FieldsFunc(data, func(r rune) bool { return r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == ',' })
	var segs []segment
	curved := false
	var start, cur *point
	flush := func() {
		addRules(page, segs, curved, stroke, fill, lw)
		segs, curved = nil, false
	}
	at := func(i int) float64 {
		if i < len(tokens) {
			v, _ := number(tokens[i])
			return v
		}
		return 0
	}
	for i := 0; i < len(tokens) && page.segments <= maxSegments; {
		switch tokens[i] {
		case "S", "M":
			flush()
			p := pt(at(i+1), at(i+2))
			start, cur = &p, &p
			i += 3
		case "L":
			to := pt(at(i+1), at(i+2))
			if cur != nil {
				segs = append(segs, segment{*cur, to})
			}
			if start == nil {
				start = &to
			}
			cur = &to
			i += 3
		case "Q":
			curved = true
			p := pt(at(i+3), at(i+4))
			cur = &p
			i += 5
		case "B":
			curved = true
			p := pt(at(i+5), at(i+6))
			cur = &p
			i += 7
		case "A":
			curved = true
			p := pt(at(i+6), at(i+7))
			cur = &p
			i += 8
		case "C":
			if cur != nil && start != nil {
				segs = append(segs, segment{*cur, *start})
			}
			cur = start
			i++
		default:
			i++
		}
	}
	flush()
}

func addRules(page *pageContent, segs []segment, curved bool, stroke, fill string, lw float64) {
	page.segments += len(segs)
	if len(segs) == 0 || page.segments > maxSegments {
		return
	}
	if stroke != "" && !isWhite(stroke) {
		t := math.Max(0.05, lw)
		for _, s := range segs {
			if math.Abs(s.from.y-s.to.y) <= 0.2 && math.Abs(s.from.x-s.to.x) >= minRuleLength {
				page.rules = append(page.rules, rule{true, (s.from.y + s.to.y) / 2, math.Min(s.from.x, s.to.x), math.Max(s.from.x, s.to.x), t, stroke})
			} else if math.Abs(s.from.x-s.to.x) <= 0.2 && math.Abs(s.from.y-s.to.y) >= minRuleLength {
				page.rules = append(page.rules, rule{false, (s.from.x + s.to.x) / 2, math.Min(s.from.y, s.to.y), math.Max(s.from.y, s.to.y), t, stroke})
			}
		}
	}
	// A filled shape of straight, upright edges: a thin one is a rule, a larger one may shade a cell.
	if fill == "" || curved || len(segs) < 3 || isWhite(fill) {
		return
	}
	l, t, r, b := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, s := range segs {
		if math.Abs(s.from.x-s.to.x) > 0.2 && math.Abs(s.from.y-s.to.y) > 0.2 {
			return
		}
		l, r = math.Min(l, math.Min(s.from.x, s.to.x)), math.Max(r, math.Max(s.from.x, s.to.x))
		t, b = math.Min(t, math.Min(s.from.y, s.to.y)), math.Max(b, math.Max(s.from.y, s.to.y))
	}
	w, h := r-l, b-t
	switch {
	case h <= maxRuleThickness && w >= minRuleLength && w > 2*h:
		page.rules = append(page.rules, rule{true, (t + b) / 2, l, r, math.Max(0.05, h), fill})
	case w <= maxRuleThickness && h >= minRuleLength && h > 2*w:
		page.rules = append(page.rules, rule{false, (l + r) / 2, t, b, math.Max(0.05, w), fill})
	case w > maxRuleThickness && h > maxRuleThickness:
		page.shades = append(page.shades, shade{l, t, r, b, fill})
	}
}

func isWhite(c string) bool {
	if len(c) < 7 {
		return false
	}
	ch := func(s string) int64 { v, _ := strconv.ParseInt(s, 16, 32); return v }
	return ch(c[1:3]) >= 242 && ch(c[3:5]) >= 242 && ch(c[5:7]) >= 242
}

// joinRules: rules on one line that overlap or nearly touch become one rule.
func joinRules(rules []rule) []rule {
	var result []rule
	for _, horizontal := range []bool{rules[0].horizontal, !rules[0].horizontal} {
		var dir []rule
		for _, r := range rules {
			if r.horizontal == horizontal {
				dir = append(dir, r)
			}
		}
		sort.SliceStable(dir, func(i, j int) bool { return dir[i].position < dir[j].position })
		for i := 0; i < len(dir); {
			j := i + 1
			for j < len(dir) && dir[j].position-dir[j-1].position <= sameLine {
				j++
			}
			group := slices.Clone(dir[i:j])
			sort.SliceStable(group, func(a, b int) bool { return group[a].start < group[b].start })
			var cur *rule
			for _, r := range group {
				if cur != nil && r.start <= cur.end+ruleGap {
					n := *cur
					n.end = math.Max(cur.end, r.end)
					n.thick = math.Max(cur.thick, r.thick)
					if r.length() > cur.length() {
						n.position, n.color = r.position, r.color
					}
					cur = &n
				} else {
					if cur != nil {
						result = append(result, *cur)
					}
					rr := r
					cur = &rr
				}
			}
			result = append(result, *cur)
			i = j
		}
	}
	return result
}

type extent struct{ left, top, right, bottom float64 }

// ruledTables finds the ruled tables of a page, largest first; their fragments leave the list.
func ruledTables(page *pageContent, fragments []*fragment) ([]*ruledTable, []*fragment) {
	var tables []*ruledTable
	if page.segments > maxSegments || len(page.rules) < 4 {
		return tables, fragments
	}
	rules := joinRules(page.rules)
	type g struct {
		rules []rule
		ext   extent
	}
	var groups []g
	for _, gr := range grids(rules) {
		groups = append(groups, g{gr, gridExtent(gr)})
	}
	area := func(e extent) float64 { return (e.right - e.left) * (e.bottom - e.top) }
	sort.SliceStable(groups, func(i, j int) bool { return area(groups[i].ext) > area(groups[j].ext) })
	for _, gr := range groups {
		clash := false
		for _, t := range tables {
			if overlap(t.left(), t.right(), gr.ext.left, gr.ext.right) > edgeTolerance && overlap(t.top(), t.bottom(), gr.ext.top, gr.ext.bottom) > edgeTolerance {
				clash = true
				break
			}
		}
		if clash {
			continue
		}
		t := buildTable(gr.rules, gr.ext, fragments, page.shades)
		if t == nil {
			continue
		}
		tables = append(tables, t)
		taken := map[*fragment]bool{}
		for _, c := range t.cells {
			for _, f := range c.fragments {
				taken[f] = true
			}
		}
		fragments = slices.DeleteFunc(slices.Clone(fragments), func(f *fragment) bool { return taken[f] })
	}
	return tables, fragments
}

// grids are groups of rules that touch each other.
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
		if !rules[a].horizontal {
			continue
		}
		for b := range rules {
			if !rules[b].horizontal && touch(rules[a], rules[b]) {
				parent[find(a)] = find(b)
			}
		}
	}
	var order []int
	byRoot := map[int][]rule{}
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
		h := slices.ContainsFunc(g, func(x rule) bool { return x.horizontal })
		v := slices.ContainsFunc(g, func(x rule) bool { return !x.horizontal })
		if h && v && len(g) >= 4 {
			out = append(out, g)
		}
	}
	return out
}

func touch(h, v rule) bool {
	reach := ruleGap + math.Max(h.thick, v.thick)/2
	return v.position >= h.start-reach && v.position <= h.end+reach && h.position >= v.start-reach && h.position <= v.end+reach
}

// gridExtent: its outer rules, or further out where most rules of the other direction reach.
func gridExtent(g []rule) extent {
	var hs, vs []rule
	for _, r := range g {
		if r.horizontal {
			hs = append(hs, r)
		} else {
			vs = append(vs, r)
		}
	}
	minPos := func(rs []rule) float64 {
		v := math.Inf(1)
		for _, r := range rs {
			v = math.Min(v, r.position)
		}
		return v
	}
	maxPos := func(rs []rule) float64 {
		v := math.Inf(-1)
		for _, r := range rs {
			v = math.Max(v, r.position)
		}
		return v
	}
	median := func(rs []rule, f func(rule) float64) float64 {
		vals := make([]float64, len(rs))
		for i, r := range rs {
			vals[i] = f(r)
		}
		sort.Float64s(vals)
		if len(vals) == 0 {
			return 0
		}
		return vals[len(vals)/2]
	}
	st := func(r rule) float64 { return r.start }
	en := func(r rule) float64 { return r.end }
	return extent{
		math.Min(minPos(vs), median(hs, st)), math.Min(minPos(hs), median(vs, st)),
		math.Max(maxPos(vs), median(hs, en)), math.Max(maxPos(hs), median(vs, en)),
	}
}

func overlap(a0, a1, b0, b1 float64) float64 { return math.Min(a1, b1) - math.Max(a0, b0) }

// edges merges positions closer than edgeTolerance into one (their mean).
func edges(positions []float64) []float64 {
	sort.Float64s(positions)
	var groups [][]float64
	for _, p := range positions {
		if n := len(groups); n > 0 && p-groups[n-1][len(groups[n-1])-1] <= edgeTolerance {
			groups[n-1] = append(groups[n-1], p)
		} else {
			groups = append(groups, []float64{p})
		}
	}
	out := make([]float64, len(groups))
	for i, g := range groups {
		s := 0.0
		for _, v := range g {
			s += v
		}
		out[i] = s / float64(len(g))
	}
	return out
}

// covering is the rule covering most of a segment on the line at position; nil when rules cover less than half.
func covering(rules []rule, position, from, to float64) *rule {
	length := to - from
	if length <= 0 {
		return nil
	}
	covered, bestOverlap := 0.0, 0.0
	var best *rule
	for i := range rules {
		r := &rules[i]
		if math.Abs(r.position-position) > edgeTolerance+r.thick/2 {
			continue
		}
		o := math.Min(to, r.end) - math.Max(from, r.start)
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

// buildTable is the table a grid of rules draws, or nil when it does not look like one.
func buildTable(g []rule, ext extent, fragments []*fragment, shades []shade) *ruledTable {
	var hs, vs []rule
	for _, r := range g {
		if r.horizontal {
			hs = append(hs, r)
		} else {
			vs = append(vs, r)
		}
	}
	xp := []float64{ext.left, ext.right}
	for _, r := range vs {
		if r.position > ext.left && r.position < ext.right {
			xp = append(xp, r.position)
		}
	}
	yp := []float64{ext.top, ext.bottom}
	for _, r := range hs {
		if r.position > ext.top && r.position < ext.bottom {
			yp = append(yp, r.position)
		}
	}
	xs, ys := edges(xp), edges(yp)
	if (len(xs)-1)*(len(ys)-1) > 2500 {
		return nil // a drawing (hatching, a chart's grid), not a form
	}
	vEdge := func(c, r int) bool { return covering(vs, xs[c], ys[r], ys[r+1]) != nil }
	hEdge := func(r, c int) bool { return covering(hs, ys[r], xs[c], xs[c+1]) != nil }
	// Grid lines that separate no two units anywhere are dropped.
	for {
		column := 0
		for c := 1; c <= len(xs)-2; c++ {
			any := false
			for r := 0; r < len(ys)-1; r++ {
				if vEdge(c, r) {
					any = true
					break
				}
			}
			if !any {
				column = c
				break
			}
		}
		if column > 0 {
			xs = slices.Delete(xs, column, column+1)
			continue
		}
		row := 0
		for r := 1; r <= len(ys)-2; r++ {
			any := false
			for c := 0; c < len(xs)-1; c++ {
				if hEdge(r, c) {
					any = true
					break
				}
			}
			if !any {
				row = r
				break
			}
		}
		if row == 0 {
			break
		}
		ys = slices.Delete(ys, row, row+1)
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
			if c+1 < columns && !vEdge(c+1, r) {
				parent[find(r*columns+c)] = find(r*columns + c + 1)
			}
			if r+1 < rows && !hEdge(r+1, c) {
				parent[find(r*columns+c)] = find((r+1)*columns + c)
			}
		}
	}
	var order []int
	units := map[int][]int{}
	for u := 0; u < rows*columns; u++ {
		k := find(u)
		if _, ok := units[k]; !ok {
			order = append(order, k)
		}
		units[k] = append(units[k], u)
	}
	owner := make([]int, rows*columns)
	var cells []*ruledCell
	for _, k := range order {
		unit := units[k]
		top, bottom, left, right := rows, -1, columns, -1
		for _, u := range unit {
			top, bottom = min(top, u/columns), max(bottom, u/columns)
			left, right = min(left, u%columns), max(right, u%columns)
		}
		if (bottom-top+1)*(right-left+1) != len(unit) {
			return nil // an L-shaped region: not a table cell
		}
		for _, u := range unit {
			owner[u] = len(cells)
		}
		cells = append(cells, &ruledCell{row: top, column: left, rowSpan: bottom - top + 1, columnSpan: right - left + 1,
			left: xs[left], top: ys[top], right: xs[right+1], bottom: ys[bottom+1]})
	}
	if len(cells) < 4 {
		rowSet, colSet := map[int]bool{}, map[int]bool{}
		for _, c := range cells {
			rowSet[c.row], colSet[c.column] = true, true
		}
		if len(rowSet) < 2 || len(colSet) < 2 {
			return nil
		}
	}
	// Fragments by their first character. Many rules through the middle of text mean a chart or crossed-out text.
	crossed, inside := 0, 0
	for _, f := range fragments {
		x := f.left() + math.Min(f.size, f.width)/2
		y := f.baseline - 0.35*f.size
		if f.vertical() {
			y = (f.top() + f.bottom()) / 2
		}
		if x <= xs[0] || x >= xs[len(xs)-1] || y <= ys[0] || y >= ys[len(ys)-1] {
			continue
		}
		inside++
		for _, h := range hs {
			if h.position > f.top()+0.3*f.size && h.position < f.baseline-0.2*f.size && h.start < f.right()-f.size && h.end > f.left()+f.size {
				crossed++
				break
			}
		}
		column := lastIndexLE(xs, x, columns)
		row := lastIndexLE(ys, y, rows)
		c := cells[owner[row*columns+column]]
		c.fragments = append(c.fragments, f)
	}
	filled := 0
	for _, c := range cells {
		if len(c.fragments) > 0 {
			filled++
		}
	}
	if crossed > max(1, inside/10) || filled < 2 {
		return nil
	}
	for _, c := range cells {
		c.borders = doc.Borders{
			Left: ruleBorder(covering(vs, c.left, c.top, c.bottom)), Right: ruleBorder(covering(vs, c.right, c.top, c.bottom)),
			Top: ruleBorder(covering(hs, c.top, c.left, c.right)), Bottom: ruleBorder(covering(hs, c.bottom, c.left, c.right)),
		}
		for i := len(shades) - 1; i >= 0; i-- {
			s := shades[i]
			cover := math.Max(0, overlap(s.left, s.right, c.left, c.right)) * math.Max(0, overlap(s.top, s.bottom, c.top, c.bottom))
			if cover >= 0.8*(c.right-c.left)*(c.bottom-c.top) && s.right-s.left < 1.5*(c.right-c.left)+2 && s.bottom-s.top < 1.5*(c.bottom-c.top)+2 {
				c.fill = s.color
				break
			}
		}
	}
	return &ruledTable{xs, ys, cells}
}

// lastIndexLE is the last edge index ≤ v, clamped to [0, n-1].
func lastIndexLE(edges []float64, v float64, n int) int {
	idx := -1
	for i, e := range edges {
		if e <= v {
			idx = i
		}
	}
	return max(0, min(n-1, idx))
}

func ruleBorder(r *rule) doc.Border {
	if r == nil {
		return doc.NoBorder
	}
	return doc.Border{Style: doc.BorderSolid, WidthMm: math.Max(0.1, r.thick), Color: r.color}
}

// tableBlock: column widths and row heights from the page, each cell's text laid out like the page.
func tableBlock(g *ruledTable, containerLeft float64) *doc.Table {
	const padding = 0.5
	t := &doc.Table{ColumnCount: len(g.columns) - 1, Borders: &doc.EmptyBorders,
		CellMargin: &doc.Insets{Left: hwp(padding), Right: hwp(padding)}, Tight: true}
	for i := 0; i+1 < len(g.columns); i++ {
		t.ColumnWidths = append(t.ColumnWidths, hwp(g.columns[i+1]-g.columns[i]))
	}
	if g.left()-containerLeft > 0.5 {
		t.Indent = hwp(g.left() - containerLeft)
	}
	for r := 0; r+1 < len(g.rows); r++ {
		t.Rows = append(t.Rows, &doc.Row{Height: doc.Ptr(hwp(g.rows[r+1] - g.rows[r]))})
	}
	cells := slices.Clone(g.cells)
	sort.SliceStable(cells, func(i, j int) bool {
		if cells[i].row != cells[j].row {
			return cells[i].row < cells[j].row
		}
		return cells[i].column < cells[j].column
	})
	for _, c := range cells {
		borders := c.borders
		hc := &doc.Cell{Column: c.column, RowSpan: c.rowSpan, ColSpan: c.columnSpan, Borders: &borders, Fill: c.fill}
		if len(c.fragments) > 0 {
			left, right := c.left+padding, c.right-padding
			// Only lines at about single spacing are one paragraph: widely spaced lines in a cell are rows.
			ps := fitLineHeights(paragraphs(lines(c.fragments), left, right, 1.6))
			spaceTop := textTop(ps[0]) - c.top
			spaceBottom := c.bottom - textBottom(ps[len(ps)-1])
			centred := len(ps) == 1 && spaceTop > 1 && math.Abs(spaceTop-spaceBottom) <= math.Max(1, 0.25*(spaceTop+spaceBottom))
			if centred {
				hc.VerticalAlign = doc.VAlignCenter
			} else if spaceTop > 0.2 {
				hc.CellMargin = &doc.Insets{Left: hwp(padding), Right: hwp(padding), Top: hwp(math.Min(spaceTop, 100))}
			}
			flow := textTop(ps[0])
			for _, p := range ps {
				before := math.Max(0, math.Min(100, textTop(p)-flow))
				hc.Blocks = append(hc.Blocks, textParagraph(p, left, right, hwp(before), true))
				flow += before + textBottom(p) - textTop(p)
			}
		}
		t.Rows[c.row].Cells = append(t.Rows[c.row].Cells, hc)
	}
	return t
}
