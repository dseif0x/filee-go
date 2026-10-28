package ofd

import (
	"math"
	"regexp"
	"slices"
	"sort"

	"github.com/dseif0x/filee-go/internal/doc"
)

// Layout: the fragments of a page become lines (same baseline, left to
// right) and lines paragraphs (a line that ran to the right edge continues
// in the next one), in reading order from the top. A line with wide gaps
// keeps its parts at their places with tab stops; text under such a row in
// its columns is the wrapped end of its cells. Grids drawn with lines become
// tables first. Pictures alone in their band become paragraphs; pictures
// beside or over text float where they are. Vertical space between blocks is
// kept as space before paragraphs.

const (
	mmToUnit             = 7200.0 / 25.4
	minMargin, maxMargin = 3.0, 40.0
)

func hwp(mm float64) int { return int(math.RoundToEven(math.Max(-1e9, math.Min(1e9, mm*mmToUnit)))) }

type wrappedPart struct {
	f      *fragment
	column int
}

type line struct {
	fragments []*fragment
	tabs      map[int]bool
	wrapped   []wrappedPart
	baseline  float64
	cap       *float64
}

func newLine(f *fragment) *line {
	return &line{fragments: []*fragment{f}, tabs: map[int]bool{}, baseline: f.baseline}
}

func (l *line) size() float64 {
	s := 0.0
	for _, f := range l.fragments {
		s = math.Max(s, f.size)
	}
	return s
}

func (l *line) left() float64 {
	v := math.Inf(1)
	for _, f := range l.fragments {
		v = math.Min(v, f.left())
	}
	return v
}

func (l *line) right() float64 {
	v := math.Inf(-1)
	for _, f := range l.fragments {
		v = math.Max(v, f.right())
	}
	return v
}

func (l *line) lastBaseline() float64 {
	v := math.Inf(-1)
	for _, f := range l.fragments {
		v = math.Max(v, f.baseline+f.run)
	}
	for _, w := range l.wrapped {
		v = math.Max(v, w.f.baseline+w.f.run)
	}
	return v
}

func (l *line) vertical() bool { return l.fragments[0].vertical() }

type placed struct {
	top, bottom float64
	lines       []*line
	table       *ruledTable
	image       *doc.Image
	left        float64
}

func (r *reader) build(pages []*pageContent) *doc.Document {
	d := &doc.Document{}
	type size struct{ w, h float64 }
	groups := map[size][]*pageContent{}
	for _, p := range pages {
		k := size{p.width, p.height}
		groups[k] = append(groups[k], p)
	}
	margins := map[size][4]float64{}
	for k, g := range groups {
		margins[k] = pageMargins(g)
	}
	var section *doc.Section
	for _, p := range pages {
		m := margins[size{p.width, p.height}]
		left, top, right, bottom := m[0], m[1], m[2], m[3]
		hp := doc.Page{Width: hwp(p.width), Height: hwp(p.height), Left: hwp(left), Right: hwp(right), Top: hwp(top), Bottom: hwp(bottom)}
		newSection := section == nil || *section.Page != hp
		if newSection {
			section = doc.NewSection()
			section.Page = &hp
			d.Sections = append(d.Sections, section)
		}
		blocks := r.pageBlocks(p, left, p.width-right, top)
		// A table cannot start a page in every format: a tiny empty paragraph carries the page break.
		if _, isTable := first0(blocks).(*doc.Table); len(blocks) == 0 || isTable {
			blocks = append([]doc.Block{spacer()}, blocks...)
		}
		if fp, ok := blocks[0].(*doc.Paragraph); ok && !newSection {
			fp.PageBreakBefore = true
		}
		section.Blocks = append(section.Blocks, blocks...)
	}
	if len(d.Sections) == 0 {
		s := doc.NewSection()
		s.Blocks = []doc.Block{&doc.Paragraph{}}
		d.Sections = append(d.Sections, s)
	}
	return d
}

func first0(b []doc.Block) doc.Block {
	if len(b) == 0 {
		return nil
	}
	return b[0]
}

// pageMargins: the smallest distances from the edges to anything drawn; the bottom one halved.
func pageMargins(pages []*pageContent) [4]float64 {
	l, t, r, b := math.MaxFloat64, math.MaxFloat64, math.MaxFloat64, math.MaxFloat64
	for _, p := range pages {
		take := func(bl, bt, br, bb float64) {
			l, t = math.Min(l, bl), math.Min(t, bt)
			r, b = math.Min(r, p.width-br), math.Min(b, p.height-bb)
		}
		for _, f := range p.fragments {
			take(f.left(), f.top(), f.right(), f.bottom())
		}
		for _, ru := range p.rules {
			if ru.horizontal {
				take(ru.start, ru.position, ru.end, ru.position)
			} else {
				take(ru.position, ru.start, ru.position, ru.end)
			}
		}
		for _, pi := range p.pictures {
			take(pi.left, pi.top, pi.right(), pi.bottom())
		}
	}
	clamp := func(v float64) float64 {
		if v == math.MaxFloat64 {
			return 20
		}
		return math.Max(minMargin, math.Min(maxMargin, v))
	}
	return [4]float64{clamp(l), clamp(t), clamp(r), clamp(b / 2)}
}

// pageBlocks are the blocks of one page in reading order; floating pictures go into the first paragraph.
func (r *reader) pageBlocks(page *pageContent, containerLeft, containerRight, marginTop float64) []doc.Block {
	fragments := deduplicated(page.fragments)
	var items []placed
	tables, fragments := ruledTables(page, fragments)
	for _, t := range tables {
		items = append(items, placed{top: t.top(), bottom: t.bottom(), table: t})
	}
	for _, p := range fitLineHeights(paragraphs(lines(fragments), containerLeft, containerRight, 2.5)) {
		items = append(items, placed{top: textTop(p), bottom: textBottom(p), lines: p})
	}
	var floating []doc.Inline
	pics := slices.Clone(page.pictures)
	sort.SliceStable(pics, func(i, j int) bool { return pics[i].top < pics[j].top })
	for _, pic := range pics {
		file := pic.file
		if file == "" && pic.part != "" {
			file = r.extract(pic.part)
		}
		if file == "" {
			continue
		}
		img := &doc.Image{Path: file, Width: doc.Ptr(hwp(pic.width)), Height: doc.Ptr(hwp(pic.height))}
		overlaps := false
		for _, it := range items {
			if it.top < pic.bottom()-0.5 && it.bottom > pic.top+0.5 {
				overlaps = true
				break
			}
		}
		if overlaps {
			img.Anchor = &doc.Anchor{HorizontalRelativeTo: "PAPER", HorizontalAlign: "LEFT", HorizontalOffset: hwp(pic.left),
				VerticalRelativeTo: "PAPER", VerticalAlign: "TOP", VerticalOffset: hwp(pic.top), Wrap: "IN_FRONT_OF_TEXT", AllowOverlap: true}
			floating = append(floating, img)
		} else {
			items = append(items, placed{top: pic.top, bottom: pic.bottom(), image: img, left: pic.left})
		}
	}
	// The blocks follow each other down the page; one that starts above the end of the one before is pushed
	// down, and the next gaps take that back.
	sort.SliceStable(items, func(i, j int) bool { return items[i].top < items[j].top })
	var blocks []doc.Block
	flow := marginTop
	for _, it := range items {
		before := math.Max(0, math.Min(100, it.top-flow))
		switch {
		case it.table != nil:
			// A table has no space before: a 1 pt paragraph carries the distance.
			if before > 0.5 {
				s := spacer()
				s.Format.Before = doc.Ptr(hwp(before - 0.35))
				blocks = append(blocks, s)
			}
			blocks = append(blocks, tableBlock(it.table, containerLeft))
		case it.image != nil:
			p := &doc.Paragraph{}
			left := hwp(it.left - containerLeft)
			centred := absInt(left+*it.image.Width/2-hwp(containerRight-containerLeft)/2) < hwp(3)
			p.Format = doc.ParaFormat{Align: doc.Ptr(doc.AlignLeft), Before: doc.Ptr(hwp(before)), After: doc.Ptr(0),
				LineSpacing: &doc.LineSpacing{Kind: doc.SpacingPercent, Value: 100}}
			if centred {
				p.Format.Align = doc.Ptr(doc.AlignCenter)
			} else if left >= hwp(1) {
				p.Format.Left = doc.Ptr(left)
			}
			p.Inlines = append(p.Inlines, it.image)
			blocks = append(blocks, p)
		default:
			// Exact line distance where the largest text fits in it.
			maxSize := 0.0
			for _, l := range it.lines {
				maxSize = math.Max(maxSize, l.size())
			}
			exact := maxSize <= lineHeight(it.lines)+0.01
			blocks = append(blocks, textParagraph(it.lines, containerLeft, containerRight, hwp(before), exact))
		}
		flow += before + (it.bottom - it.top)
	}
	// The first block's distance from the top margin becomes the height of an empty line.
	if p, ok := first0(blocks).(*doc.Paragraph); ok && p.Format.Before != nil && *p.Format.Before > hwp(0.5) {
		s := spacer()
		s.Format.LineSpacing = &doc.LineSpacing{Kind: doc.SpacingFixed, Value: *p.Format.Before}
		p.Format.Before = doc.Ptr(0)
		blocks = append([]doc.Block{s}, blocks...)
	}
	if len(floating) > 0 {
		if _, ok := first0(blocks).(*doc.Paragraph); !ok {
			blocks = append([]doc.Block{spacer()}, blocks...)
		}
		p := blocks[0].(*doc.Paragraph)
		p.Inlines = append(floating, p.Inlines...)
	}
	return blocks
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// spacer is an empty 1 pt paragraph with no spacing.
func spacer() *doc.Paragraph {
	return &doc.Paragraph{
		Format:     doc.ParaFormat{Before: doc.Ptr(0), After: doc.Ptr(0), LineSpacing: &doc.LineSpacing{Kind: doc.SpacingPercent, Value: 100}},
		MarkFormat: doc.CharFormat{Size: doc.Ptr(100)},
	}
}

// deduplicated drops the same text drawn twice at one place (fake bold, a label in template and page).
func deduplicated(fragments []*fragment) []*fragment {
	type key struct {
		t    string
		x, y int64
	}
	seen := map[key]bool{}
	var out []*fragment
	for _, f := range fragments {
		k := key{string(f.text), int64(math.RoundToEven(f.x * 3)), int64(math.RoundToEven(f.baseline * 3))}
		if !seen[k] {
			seen[k] = true
			out = append(out, f)
		}
	}
	return out
}

// lines groups fragments by baseline (within a fraction of the font size), left to right.
func lines(fragments []*fragment) []*line {
	sorted := slices.Clone(fragments)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].baseline != sorted[j].baseline {
			return sorted[i].baseline < sorted[j].baseline
		}
		return sorted[i].x < sorted[j].x
	})
	var ls []*line
	for _, f := range sorted {
		var target *line
		if !f.vertical() {
			for i := len(ls) - 1; i >= max(0, len(ls)-4); i-- {
				l := ls[i]
				if !l.vertical() && math.Abs(l.baseline-f.baseline) <= 0.4*math.Min(l.size(), f.size) {
					target = l
					break
				}
			}
		}
		if target == nil {
			ls = append(ls, newLine(f))
		} else {
			target.fragments = append(target.fragments, f)
		}
	}
	topOf := func(l *line) float64 {
		v := math.Inf(1)
		for _, f := range l.fragments {
			v = math.Min(v, f.top())
		}
		return v
	}
	sort.SliceStable(ls, func(i, j int) bool { return topOf(ls[i]) < topOf(ls[j]) })
	for _, l := range ls {
		sort.SliceStable(l.fragments, func(i, j int) bool { return l.fragments[i].x < l.fragments[j].x })
		for i := 1; i < len(l.fragments); i++ {
			prev, f := l.fragments[i-1], l.fragments[i]
			if f.left()-prev.right() >= math.Max(1.5*f.size, 2) {
				l.tabs[i] = true
			}
		}
	}
	return joinWrappedCells(ls)
}

var amount = regexp.MustCompile(`^[-+¥￥$€]?[\d,.]+%?$`)

func isAmount(f *fragment) bool { return amount.MatchString(string(f.text)) }

// joinWrappedCells: a line right under a tabbed row whose parts all start in
// that row's columns and are no amounts belongs to the row.
func joinWrappedCells(ls []*line) []*line {
	for i := 1; i < len(ls); i++ {
		row, wrapped := ls[i-1], ls[i]
		above := row.baseline
		if len(row.wrapped) > 0 {
			above = row.wrapped[len(row.wrapped)-1].f.baseline
		}
		if len(row.tabs) < 2 || len(wrapped.fragments) >= len(row.fragments) || row.vertical() || wrapped.vertical() ||
			!slices.ContainsFunc(row.fragments, isAmount) || slices.ContainsFunc(wrapped.fragments, isAmount) ||
			wrapped.baseline-above > 2.2*wrapped.size() {
			continue
		}
		var columns []int
		ok := true
		seen := map[int]bool{}
		for _, f := range wrapped.fragments {
			c := slices.IndexFunc(row.fragments, func(x *fragment) bool { return math.Abs(x.left()-f.left()) < 1.5 })
			if c < 0 || c > 0 && !row.tabs[c] || seen[c] {
				ok = false
				break
			}
			seen[c] = true
			columns = append(columns, c)
		}
		if !ok {
			continue
		}
		for k, f := range wrapped.fragments {
			row.wrapped = append(row.wrapped, wrappedPart{f, columns[k]})
		}
		ls = append(ls[:i], ls[i+1:]...)
		i--
	}
	return ls
}

// paragraphs joins lines: a line continues the paragraph above when that one
// filled the measure, the size and line distance match and it starts no
// further right than the paragraph's body.
func paragraphs(ls []*line, containerLeft, containerRight, maxPitch float64) [][]*line {
	var out [][]*line
	for _, l := range ls {
		if n := len(out); n > 0 && continues(out[n-1], l, containerLeft, containerRight, maxPitch) {
			out[n-1] = append(out[n-1], l)
		} else {
			out = append(out, []*line{l})
		}
	}
	return out
}

func continues(p []*line, l *line, containerLeft, containerRight, maxPitch float64) bool {
	last := p[len(p)-1]
	size := last.size()
	if len(last.tabs) > 0 || len(l.tabs) > 0 || last.vertical() || l.vertical() || math.Abs(l.size()-size) > 0.15*size {
		return false
	}
	pitch := l.baseline - last.baseline
	if pitch < 0.9*size || pitch > maxPitch*size || len(p) > 1 && math.Abs(pitch-(last.baseline-p[len(p)-2].baseline)) > 0.25*size {
		return false
	}
	// A line its writer broke inside one TextCode follows after a line break.
	if l.fragments[0].continued {
		return len(l.fragments) == 1 && math.Abs(l.left()-last.left()) < 0.6*size
	}
	for _, x := range p[1:] {
		if x.fragments[0].continued {
			return false
		}
	}
	body := p[0].left()
	if len(p) > 1 {
		body = math.Inf(1)
		for _, x := range p[1:] {
			body = math.Min(body, x.left())
		}
	}
	if last.right() < containerRight-2.5*size || last.right()-math.Min(body, last.left()) < 0.5*(containerRight-containerLeft) {
		return false
	}
	return l.left() <= body+0.6*size && l.left() >= body-4*size
}

// lineHeight is a paragraph's baseline pitch (also to wrapped cell ends), or 1.2 times the font size.
func lineHeight(ls []*line) float64 {
	size := ls[0].size()
	wrappedLines := map[float64]bool{}
	for _, w := range ls[0].wrapped {
		wrappedLines[w.f.baseline] = true
	}
	var pitch float64
	switch {
	case len(ls) > 1:
		pitch = (ls[len(ls)-1].baseline - ls[0].baseline) / float64(len(ls)-1)
	case len(wrappedLines) > 0:
		pitch = (ls[0].lastBaseline() - ls[0].baseline) / float64(len(wrappedLines))
	case ls[0].cap != nil:
		pitch = *ls[0].cap
	default:
		pitch = size * 1.2
	}
	return math.Max(size, math.Min(size*3, pitch))
}

// fitLineHeights: a one-line paragraph closer to the line above than 1.2
// times its size gets a line distance that fits in between.
func fitLineHeights(ps [][]*line) [][]*line {
	for i := 1; i < len(ps); i++ {
		above, l := ps[i-1][len(ps[i-1])-1], ps[i][0]
		if len(ps[i]) > 1 || len(l.wrapped) > 0 || l.vertical() || above.vertical() {
			continue
		}
		room := l.baseline - above.lastBaseline() + 0.25*(l.size()-above.size())
		if room > 0 && room < 1.2*l.size() {
			l.cap = &room
		}
	}
	return ps
}

func textTop(ls []*line) float64 {
	if ls[0].vertical() {
		return ls[0].fragments[0].top()
	}
	return ls[0].baseline - (lineHeight(ls) - 0.25*ls[0].size())
}

func textBottom(ls []*line) float64 {
	l := ls[len(ls)-1]
	return l.lastBaseline() + 0.25*l.size()
}

// textParagraph: exact line distance for table cells, whose rows must keep their height; else "at least".
func textParagraph(ls []*line, containerLeft, containerRight float64, before int, exact bool) *doc.Paragraph {
	width := math.Max(1, containerRight-containerLeft)
	leftGap := ls[0].left() - containerLeft
	maxRight := math.Inf(-1)
	tabbed := false
	for _, l := range ls {
		maxRight = math.Max(maxRight, l.right())
		if len(l.tabs) > 0 {
			tabbed = true
		}
	}
	rightGap := containerRight - maxRight
	contLater := false
	for _, l := range ls[1:] {
		if l.fragments[0].continued {
			contLater = true
		}
	}
	align := doc.AlignLeft
	switch {
	case len(ls) == 1 && !tabbed && math.Abs(leftGap-rightGap) < math.Max(1.5, width*0.03) && leftGap > math.Max(3, width*0.05):
		align = doc.AlignCenter
	case len(ls) == 1 && !tabbed && rightGap < 1.5 && leftGap > width*0.2:
		align = doc.AlignRight
	case len(ls) > 1 && !contLater:
		align = doc.AlignJustify // not lines that end in a line break: Word would spread them
	}
	var left, firstLine *int
	if align == doc.AlignLeft || align == doc.AlignJustify {
		body := ls[0].left()
		if len(ls) > 1 {
			body = math.Inf(1)
			for _, l := range ls[1:] {
				body = math.Min(body, l.left())
			}
		}
		body -= containerLeft
		if body > 0.5 {
			left = doc.Ptr(hwp(body))
		}
		f := ls[0].left() - containerLeft - math.Max(0, body)
		if math.Abs(f) > 0.5 {
			firstLine = doc.Ptr(hwp(f))
		}
	}
	// A tabbed row: a left tab stop where each part starts on the page.
	stops := map[int]bool{}
	for _, l := range ls {
		for t := range l.tabs {
			stops[hwp(l.fragments[t].left()-containerLeft)] = true
		}
	}
	var tabs []doc.TabStop
	for _, pos := range sortedInts(stops) {
		tabs = append(tabs, doc.TabStop{Position: pos, Kind: doc.TabLeft, Leader: "NONE"})
	}
	kind := doc.SpacingAtLeast
	if exact {
		kind = doc.SpacingFixed
	}
	p := &doc.Paragraph{
		Format: doc.ParaFormat{Align: doc.Ptr(align), Left: left, FirstLine: firstLine, Tabs: tabs, Before: doc.Ptr(before), After: doc.Ptr(0),
			LineSpacing: &doc.LineSpacing{Kind: kind, Value: hwp(lineHeight(ls))}},
		MarkFormat: doc.CharFormat{Size: ls[0].fragments[0].format.Size},
	}
	p.Inlines = runs(ls)
	return p
}

func sortedInts(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

type piece struct {
	text   string
	format doc.CharFormat
}

// runs is the text of a paragraph: fragments joined into runs of one format,
// a space where the page leaves one (and at line ends, except next to Chinese
// text), a tab before the parts of a tabbed row; wrapped cell ends after a
// line break, tabbed to their columns.
func runs(ls []*line) []doc.Inline {
	var pieces []piece
	lastRune := func(s string) rune {
		r := []rune(s)
		return r[len(r)-1]
	}
	space := func() {
		if n := len(pieces); n > 0 && pieces[n-1].text != "\t" && pieces[n-1].text != "\n" && lastRune(pieces[n-1].text) != ' ' {
			pieces[n-1].text += " "
		}
	}
	for li, l := range ls {
		for i, f := range l.fragments {
			switch {
			case i > 0 && l.tabs[i]:
				pieces = append(pieces, piece{"\t", f.format})
			case i > 0:
				prev := l.fragments[i-1]
				gap := f.left() - prev.right()
				if gap > 0.5*f.size || gap > 0.2*f.size && !isWide(prev.text[len(prev.text)-1]) && !isWide(f.text[0]) {
					space()
				}
			case li > 0 && f.continued:
				pieces = append(pieces, piece{"\n", f.format})
			case li > 0 && len(pieces) > 0 && !isWide(lastRune(pieces[len(pieces)-1].text)) && !isWide(f.text[0]):
				space()
			}
			pieces = append(pieces, piece{string(f.text), f.format})
		}
		// Wrapped cell ends, a line per baseline: column k's tab stop is the row's n-th tab.
		var order []float64
		groups := map[float64][]wrappedPart{}
		for _, w := range l.wrapped {
			if _, ok := groups[w.f.baseline]; !ok {
				order = append(order, w.f.baseline)
			}
			groups[w.f.baseline] = append(groups[w.f.baseline], w)
		}
		for _, b := range order {
			g := slices.Clone(groups[b])
			pieces = append(pieces, piece{"\n", g[0].f.format})
			sort.SliceStable(g, func(i, j int) bool { return g[i].column < g[j].column })
			stop := 0
			for _, w := range g {
				target := 0
				for t := range l.tabs {
					if t <= w.column {
						target++
					}
				}
				for ; stop < target; stop++ {
					pieces = append(pieces, piece{"\t", w.f.format})
				}
				pieces = append(pieces, piece{string(w.f.text), w.f.format})
			}
		}
	}
	var out []doc.Inline
	for _, p := range pieces {
		switch p.text {
		case "\t":
			out = append(out, &doc.Tab{Format: p.format})
		case "\n":
			out = append(out, &doc.LineBreak{Format: p.format})
		default:
			if n := len(out); n > 0 {
				if t, ok := out[n-1].(*doc.Text); ok && t.Format.Equal(p.format) {
					out[n-1] = &doc.Text{Text: t.Text + p.text, Format: p.format}
					continue
				}
			}
			out = append(out, &doc.Text{Text: p.text, Format: p.format})
		}
	}
	return out
}
