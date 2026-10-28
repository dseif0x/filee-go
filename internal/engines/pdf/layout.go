// Page layout analysis for the reader: words → lines (split at column
// gutters) → paragraphs → reading order. One column; two or three columns,
// with titles, figures or footers spanning the columns in between. A gutter
// is a vertical strip no narrow paragraph crosses while text sits next to it
// on both sides; paragraphs wider than a column separate the page into bands
// read one after the other, and inside a band the columns left to right.

package pdf

import (
	"math"
	"regexp"
	"slices"
	"sort"
	"unicode"
)

// listStart: line starts that begin a new paragraph, bullets and "1." / "a)" / "(3)" numbers.
var listStart = regexp.MustCompile(`^([•◦▪▫●○■□‣⁃∙·\-–—*]|\(?\d{1,3}[.)]|\(?[a-zA-Z][.)]|[ivxIVX]{1,4}[.)]|[①-⑳]|[가-하][.)])$`)

// direction of text by its letters: -1 with right-to-left letters at least
// as many as left-to-right ones, 1 with left-to-right letters or digits only, 0 without either.
func direction(text string) int {
	rtl, ltr, digits := 0, 0, 0
	for _, r := range text {
		switch {
		case isRightToLeft(r):
			if r >= 0x0660 && r <= 0x0669 || r >= 0x06F0 && r <= 0x06F9 {
				digits++
			} else if unicode.IsLetter(r) {
				rtl++
			}
		case unicode.IsLetter(r):
			ltr++
		case unicode.IsDigit(r):
			digits++
		}
	}
	switch {
	case rtl > 0 && rtl >= ltr:
		return -1
	case ltr+digits > 0:
		return 1
	}
	return 0
}

func isRightToLeft(r rune) bool {
	return r >= 0x0590 && r <= 0x08FF || r >= 0xFB1D && r <= 0xFDFF || r >= 0xFE70 && r <= 0xFEFF ||
		r >= 0x10800 && r <= 0x10FFF || r >= 0x1E800 && r <= 0x1EFFF
}

// isMark: combining marks only (vowel signs, viramas, Arabic harakat, joiners).
func isMark(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r != 0x200C && r != 0x200D && !unicode.In(r, unicode.Mn, unicode.Mc, unicode.Me) {
			return false
		}
	}
	return true
}

// logicalOrder puts items read in visual order into logical order: runs of
// the other direction are turned around, also over neutral items between them.
func logicalOrder[T any](visual []T, dir func(T) int, rtl bool) []T {
	other := -1
	if rtl {
		other = 1
	}
	out := slices.Clone(visual)
	for i := 0; i < len(out); {
		if dir(out[i]) != other {
			i++
			continue
		}
		end := i
		for j := i + 1; j < len(out); j++ {
			d := dir(out[j])
			if d == other {
				end = j
			} else if d != 0 {
				break
			}
		}
		slices.Reverse(out[i : end+1])
		i = end + 1
	}
	return out
}

// makeLines groups words on a common baseline into lines, top to bottom, and
// splits a line where the gap between two words is much wider than a space.
func makeLines(words []*textWord) []*textLine {
	sorted := slices.Clone(words)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Baseline != sorted[j].Baseline {
			return sorted[i].Baseline > sorted[j].Baseline
		}
		return sorted[i].Left < sorted[j].Left
	})
	var rows [][]*textWord
	for _, w := range sorted {
		// A word belongs to a recent row with a close baseline; smaller superscripts sit a little higher.
		row := -1
		for r := len(rows) - 1; r >= max(0, len(rows)-3) && row < 0; r-- {
			first := rows[r][0]
			tol := 0.5 * min(first.Sz, w.Sz)
			if math.Abs(first.Baseline-w.Baseline) <= tol || (w.Bottom < first.Top && w.Top > first.Baseline && w.Sz < first.Sz*0.8) {
				row = r
			}
		}
		if row < 0 {
			rows = append(rows, nil)
			row = len(rows) - 1
		}
		rows[row] = append(rows[row], w)
	}
	// A space is about a quarter of the font size; the median gap tells the page's spacing.
	var gaps []float64
	for _, row := range rows {
		sort.SliceStable(row, func(i, j int) bool { return row[i].Left < row[j].Left })
		for i := 1; i < len(row); i++ {
			if g := row[i].Left - row[i-1].Right; g > 0 {
				gaps = append(gaps, g)
			}
		}
	}
	space := 3.0
	if len(gaps) > 0 {
		space = max(0.5, median(gaps))
	}
	var lines []*textLine
	for r, row := range rows {
		current := []*textWord{row[0]}
		for i := 1; i < len(row); i++ {
			gap := row[i].Left - row[i-1].Right
			size := min(row[i].Sz, row[i-1].Sz)
			if gap > max(5*space, 1.5*size) {
				lines = append(lines, &textLine{Words: current, Row: r})
				current = nil
			}
			current = append(current, row[i])
		}
		lines = append(lines, &textLine{Words: current, Row: r})
	}
	return lines
}

type gutter struct{ Start, End float64 }

// joinTabbedRows joins the parts of a split row again, with a tab between
// them, where no column gutter lies between them and the gap is a tab: after
// a list number, wider than 6 em, or at the same place as a tab in a row
// right above or below. Nil when no row changes.
func joinTabbedRows(lines []*textLine, items []*item) []*textLine {
	var rows [][]*textLine
	byRow := map[int]int{}
	for _, l := range lines {
		i, ok := byRow[l.Row]
		if !ok {
			i = len(rows)
			byRow[l.Row] = i
			rows = append(rows, nil)
		}
		rows[i] = append(rows[i], l)
	}
	split := false
	for _, r := range rows {
		if len(r) > 1 {
			split = true
		}
	}
	if len(items) == 0 || !split {
		return nil
	}
	left, right := math.Inf(1), math.Inf(-1)
	for _, it := range items {
		left, right = min(left, it.Left()), max(right, it.Right())
	}
	gutters := findGutters(items, left, right)
	type key struct{ row, part int }
	type candidate struct {
		row, part int
		x         float64
	}
	var candidates []candidate
	tabs := map[key]bool{}
	for _, row := range rows {
		for p := 1; p < len(row); p++ {
			before, after := row[p-1], row[p]
			if slices.ContainsFunc(gutters, func(g gutter) bool { return g.Start >= before.Right()-2 && g.End <= after.Left()+2 }) {
				continue
			}
			candidates = append(candidates, candidate{before.Row, p, after.Left()})
			number := len(before.Words) == 1 && listStart.MatchString(before.Words[0].Text)
			if number || after.Left()-before.Right() > 6*max(before.Size(), after.Size()) {
				tabs[key{before.Row, p}] = true
			}
		}
	}
	if len(tabs) == 0 {
		return nil
	}
	for grown := true; grown; {
		grown = false
		for _, c := range candidates {
			if tabs[key{c.row, c.part}] {
				continue
			}
			for _, o := range candidates {
				if tabs[key{o.row, o.part}] && abs(o.row-c.row) <= 2 && math.Abs(o.x-c.x) <= 1.5 {
					tabs[key{c.row, c.part}] = true
					grown = true
					break
				}
			}
		}
	}
	var result []*textLine
	for _, row := range rows {
		current := row[0]
		for p := 1; p < len(row); p++ {
			if tabs[key{current.Row, p}] {
				joined := &textLine{Words: append(slices.Clone(current.Words), row[p].Words...), Row: current.Row}
				joined.Tabs = append(slices.Clone(current.Tabs), len(current.Words))
				current = joined
			} else {
				result = append(result, current)
				current = row[p]
			}
		}
		result = append(result, current)
	}
	return result
}

// paragraphs stacks lines into paragraphs: a line continues the paragraph
// right above it when close below, overlapping it and of a similar size,
// unless it starts a new one (a bullet, a first-line indent, an early end).
func paragraphs(lines []*textLine) []*item {
	var items []*item
	for _, line := range lines {
		var best *item
		bestDistance := math.Inf(1)
		for _, it := range items {
			if len(line.Tabs) > 0 {
				break // a tabbed row starts its own paragraph
			}
			last := it.Lines[len(it.Lines)-1]
			// A tabbed row continues only with text under its last part.
			head := it.Lines[0]
			if len(head.Tabs) > 0 && math.Abs(line.Left()-head.Words[head.Tabs[len(head.Tabs)-1]].Left) > 0.5*line.Size() {
				continue
			}
			distance := last.Baseline() - line.Baseline()
			size := max(last.Size(), line.Size())
			if distance <= 0 || distance > 1.9*size || math.Abs(last.Size()-line.Size()) > 0.2*size {
				continue
			}
			if len(it.Lines) >= 2 && distance > 1.3*pitch(it) {
				continue // more space than between the lines so far: a new paragraph
			}
			overlap := min(it.Right(), line.Right()) - max(it.Left(), line.Left())
			if overlap < 0.3*min(it.Width(), line.Right()-line.Left()) {
				continue
			}
			if distance < bestDistance {
				best, bestDistance = it, distance
			}
		}
		if best != nil && !startsParagraph(best, line) {
			best.Lines = append(best.Lines, line)
		} else {
			items = append(items, &item{Lines: []*textLine{line}})
		}
	}
	return items
}

func startsParagraph(it *item, line *textLine) bool {
	last := it.Lines[len(it.Lines)-1]
	em := line.Size()
	if listStart.MatchString(line.First().Text) {
		return true
	}
	listItem := listStart.MatchString(it.Lines[0].First().Text) || len(it.Lines[0].Tabs) > 0
	firstWord := line.First().Right - line.First().Left
	if line.RightToLeft() && last.RightToLeft() {
		bodyRight := last.Right()
		if len(it.Lines) > 1 {
			bodyRight = math.Inf(-1)
			for _, l := range it.Lines[1:] {
				bodyRight = max(bodyRight, l.Right())
			}
		}
		if line.Right() < bodyRight-0.8*em && !listItem && (len(it.Lines) > 1 || line.Right() < last.Right()-0.8*em) {
			return true
		}
		start := min(it.Left(), line.Left())
		return last.Left()-0.3*em-firstWord > start+0.5*em && it.Right()-start > 10*em
	}
	// First-line indent: this line starts further right than the paragraph's other lines.
	bodyLeft := last.Left()
	if len(it.Lines) > 1 {
		bodyLeft = math.Inf(1)
		for _, l := range it.Lines[1:] {
			bodyLeft = min(bodyLeft, l.Left())
		}
	}
	if line.Left() > bodyLeft+0.8*em && !listItem && (len(it.Lines) > 1 || line.Left() > last.Left()+0.8*em) {
		return true
	}
	// The previous line ended although the next word would have fitted.
	right := max(it.Right(), line.Right())
	return last.Right()+0.3*em+firstWord < right-0.5*em && right-it.Left() > 10*em
}

// readingOrder orders a page's paragraphs and pictures and sets the column each sits in.
func readingOrder(items []*item) []*item {
	if len(items) == 0 {
		return items
	}
	areaLeft, areaRight := math.Inf(1), math.Inf(-1)
	for _, it := range items {
		areaLeft, areaRight = min(areaLeft, it.Left()), max(areaRight, it.Right())
	}
	for _, it := range items {
		it.ContainerLeft, it.ContainerRight = areaLeft, areaRight
	}
	byTopThenLeft := func(list []*item) {
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].Top() != list[j].Top() {
				return list[i].Top() > list[j].Top()
			}
			return list[i].Left() < list[j].Left()
		})
	}
	gutters := findGutters(items, areaLeft, areaRight)
	if len(gutters) == 0 {
		out := slices.Clone(items)
		byTopThenLeft(out)
		return out
	}
	edges := []float64{areaLeft}
	for _, g := range gutters {
		edges = append(edges, g.Start, g.End)
	}
	edges = append(edges, areaRight)
	columnOf := func(it *item) int {
		center := (it.Left() + it.Right()) / 2
		for c := 0; c < len(edges)/2; c++ {
			if center <= edges[2*c+1]+1 {
				return c
			}
		}
		return len(edges)/2 - 1
	}
	spans := func(it *item) bool {
		return slices.ContainsFunc(gutters, func(g gutter) bool { return it.Left() < g.Start-1 && it.Right() > g.End+1 })
	}
	var columnItems, spanning []*item
	for _, it := range items {
		if spans(it) {
			spanning = append(spanning, it)
		} else {
			columnItems = append(columnItems, it)
		}
	}
	for _, it := range columnItems {
		c := columnOf(it)
		l, r := math.Inf(1), math.Inf(-1)
		for _, m := range columnItems {
			if columnOf(m) == c {
				l, r = min(l, m.Left()), max(r, m.Right())
			}
		}
		it.ContainerLeft, it.ContainerRight = l, r
	}
	byColumn := func(list []*item) {
		sort.SliceStable(list, func(i, j int) bool {
			ci, cj := columnOf(list[i]), columnOf(list[j])
			if ci != cj {
				return ci < cj
			}
			return list[i].Top() > list[j].Top()
		})
	}
	// Spanning items split the page into bands; each band is read column by column.
	sort.SliceStable(spanning, func(i, j int) bool { return spanning[i].Top() > spanning[j].Top() })
	var result []*item
	remaining := slices.Clone(columnItems)
	for _, sep := range spanning {
		var above, rest []*item
		for _, it := range remaining {
			if it.Top() > sep.Top() {
				above = append(above, it)
			} else {
				rest = append(rest, it)
			}
		}
		byColumn(above)
		result = append(result, above...)
		result = append(result, sep)
		remaining = rest
	}
	byColumn(remaining)
	return append(result, remaining...)
}

// findGutters finds vertical strips between columns: no narrow item crosses
// them, text lies on both sides next to each other over several lines.
func findGutters(items []*item, left, right float64) []gutter {
	width := right - left
	var narrow []*item
	for _, it := range items {
		if it.Width() < width*0.6 {
			narrow = append(narrow, it)
		}
	}
	if len(narrow) < 2 || width < 100 {
		return nil
	}
	const bin = 2.0
	bins := int(math.Ceil(width / bin))
	covered := make([]bool, bins)
	for _, it := range narrow {
		for b := max(0, int((it.Left()-left)/bin)); b < min(bins, int(math.Ceil((it.Right()-left)/bin))); b++ {
			covered[b] = true
		}
	}
	top, bottom := math.Inf(-1), math.Inf(1)
	for _, it := range items {
		top, bottom = max(top, it.Top()), min(bottom, it.Bottom())
	}
	textHeight := max(1, (top-bottom)*0.2)
	height := func(side []*item) float64 {
		s := 0.0
		for _, it := range side {
			s += it.Top() - it.Bottom()
		}
		return s
	}
	lineCount := func(side []*item) int {
		n := 0
		for _, it := range side {
			n += max(1, len(it.Lines))
		}
		return n
	}
	var out []gutter
	for b := 0; b < bins; {
		if covered[b] {
			b++
			continue
		}
		start := b
		for b < bins && !covered[b] {
			b++
		}
		gl, gr := left+float64(start)*bin, left+float64(b)*bin
		if start == 0 || b >= bins || gr-gl < 6 {
			continue
		}
		var leftSide, rightSide []*item
		for _, it := range narrow {
			if it.Right() <= gl+1 {
				leftSide = append(leftSide, it)
			}
			if it.Left() >= gr-1 {
				rightSide = append(rightSide, it)
			}
		}
		overlapping := false
		for _, l := range leftSide {
			for _, r := range rightSide {
				if min(l.Top(), r.Top())-max(l.Bottom(), r.Bottom()) > 2*max(l.Size(), r.Size()) {
					overlapping = true
				}
			}
		}
		if height(leftSide) >= textHeight && height(rightSide) >= textHeight && lineCount(leftSide) >= 3 && lineCount(rightSide) >= 3 && overlapping {
			out = append(out, gutter{gl, gr})
		}
	}
	return out
}
