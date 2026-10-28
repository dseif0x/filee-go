package rhwp

// What Filee changes in an HWPX before rhwp renders it to PDF, in a copy (the user's file is never touched):
//
//   - Fonts: installed fonts for the text's language and characters, no CFF2 variable fonts (#61, #64).
//   - Punctuation widths: quotes and CJK punctuation in Chinese and Japanese fonts get letter spacing up to their real
//     width, which rhwp guesses as half a character (#237).
//   - Shape sizes: a box inside a text box stores its size relative to the column (hp:sz widthRelTo="COLUMN", 9920 =
//     99.2 %); rhwp takes the number as absolute (#65). The size 한글 laid the object out at (hp:curSz) is written.
//   - Equation scripts: 한글 2022 saves a command glued to the next letter ("f CIRCf", "A SMALLINTERB", "lek",
//     "rootx") and writes != and +- for ≠ and ±. They are spaced and spelled out.
//   - Tables floating in a table cell, in an HWPX rhwp exported from an HWP (#49): when the anchor paragraph has no
//     text, its stored line is moved up to the paragraph's top, and rhwp then puts the table where 한글 does.
//   - Equation lines (#143): in paragraphs without stored lines (Word equations written by Filee) rhwp draws a
//     fraction, matrix or sum partly below its line; those paragraphs get the overhang as space after them.
//
// rhwp's equation fonts are Latin Modern Math, STIX Two and Times New Roman; without the first two or Arial Unicode MS
// ∪ ∈ ⊂ ∘ ∼ came out empty, so Cambria Math (part of Windows) is asked for then.

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/hwp/xmltree"
)

// upperCommands are equation commands 한글 separates from a following identifier (found in 한글 2022 documents).
var upperCommands = []string{
	"CIRC", "SMALLINTER", "SMALLUNION", "SUBSET", "SUPERSET", "IN", "NOTIN", "OWNS", "INTER", "UNION", "APPROX",
	"EQUIV", "CONG", "DIV", "PLUSMINUS", "MINUSPLUS", "CDOT", "BULLET", "OPLUS", "FORALL", "EXIST", "THEREFORE",
	"BECAUSE", "DEG", "INF", "PARTIAL", "RARROW", "LARROW", "LRARROW", "EMPTYSET", "PROPTO",
}

// lowerCommands are only split before one or two letters or digits ("lek", "le28", "rootxy").
var lowerCommands = []string{"le", "ge", "root", "sqrt", "circ", "subset", "superset", "in", "notin", "approx", "equiv", "div", "pm", "mp", "cdot"}

// whole are longer commands that start like one of the above and stay whole.
var whole = []string{
	"left", "leq", "geq", "int", "inf", "inter", "cdots", "CDOTS", "SUBSETEQ", "SUPSETEQ", "INTER", "INF", "NOTIN",
	"notin", "pmatrix", "LEFT", "RIGHT", "LEQ", "GEQ", "INT", "INFTY", "infty", "DIVIDE",
}

var operators = [][2]string{{"!=", " NEQ "}, {"<=", " LEQ "}, {">=", " GEQ "}, {"+-", " PLUSMINUS "}, {"-+", " MINUSPLUS "}}

// Hwp5OriginMarker is the entry rhwp's export-hwpx adds to an HWPX it wrote for an HWP file.
const Hwp5OriginMarker = "META-INF/rhwp-hwp5-origin"

var sizedObjects = map[string]bool{"rect": true, "ellipse": true, "arc": true, "polygon": true, "curve": true, "line": true,
	"connectLine": true, "container": true, "pic": true, "ole": true, "textart": true}

var identifier = regexp.MustCompile(`[A-Za-z][A-Za-z0-9]*`)

type part struct {
	name string
	doc  *xmltree.Document
}

// Prepare writes a copy of the HWPX into workDir with the changes above and returns its path, or "" when nothing
// needs changing (or the file is not an HWPX package Filee can read). lookup finds installed fonts (nil = Find).
func Prepare(hwpx, workDir string, lookup Lookup) string {
	if lookup == nil {
		lookup = Find
	}
	z, err := zip.OpenReader(hwpx)
	if err != nil {
		return ""
	}
	var header *xmltree.Document
	var sections []part
	fromHwp := false
	for _, f := range z.File {
		switch {
		case f.Name == "Contents/header.xml":
			header = loadPart(f)
		case f.Name == Hwp5OriginMarker:
			fromHwp = true
		case strings.HasPrefix(f.Name, "Contents/section") && strings.HasSuffix(f.Name, ".xml"):
			if d := loadPart(f); d != nil {
				sections = append(sections, part{f.Name, d})
			} else {
				z.Close()
				return ""
			}
		}
	}
	z.Close()
	if header == nil {
		return ""
	}
	changed := map[string]*xmltree.Document{}

	var text strings.Builder
	for _, s := range sections {
		for _, t := range s.doc.Root.DescendantsNamed("t") {
			if text.Len() < 200_000 {
				text.WriteString(t.Value())
			}
		}
	}
	if applyFonts(header.Root, text.String(), lookup) {
		changed["Contents/header.xml"] = header
	}
	roots := make([]*xmltree.Element, len(sections))
	for i, s := range sections {
		roots[i] = s.doc.Root
	}
	if WidenPunctuation(header.Root, roots, lookup) {
		changed["Contents/header.xml"] = header
		for _, s := range sections {
			changed[s.name] = s.doc
		}
	}
	for _, s := range sections {
		a := FixShapeSizes(s.doc.Root)
		b := NormaliseEquations(s.doc.Root)
		c := fromHwp && AnchorTablesInCells(s.doc.Root)
		if a || b || c {
			changed[s.name] = s.doc
		}
		if SpaceEquationLines(s.doc.Root, header.Root) {
			changed[s.name] = s.doc
			changed["Contents/header.xml"] = header
		}
	}
	if len(changed) == 0 {
		return ""
	}
	copyPath := filepath.Join(workDir, fmt.Sprintf("rhwp-%s.hwpx", randomName()))
	if err := rewrite(hwpx, copyPath, changed); err != nil {
		os.Remove(copyPath)
		return ""
	}
	return copyPath
}

func loadPart(f *zip.File) *xmltree.Document {
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	d, err := xmltree.Parse(rc) // spaces in hp:t are text
	if err != nil {
		return nil
	}
	return d
}

// rewrite copies the package with the changed parts replaced in place (mimetype stays first and stored).
func rewrite(source, target string, changed map[string]*xmltree.Document) error {
	z, err := zip.OpenReader(source)
	if err != nil {
		return err
	}
	defer z.Close()
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	w := zip.NewWriter(out)
	for _, f := range z.File {
		d, ok := changed[f.Name]
		if !ok {
			if err := w.Copy(f); err != nil {
				out.Close()
				return err
			}
			continue
		}
		dst, err := w.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate})
		if err != nil {
			out.Close()
			return err
		}
		if _, err := dst.Write(d.Bytes()); err != nil {
			out.Close()
			return err
		}
	}
	if err := w.Close(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// PdfOptions are the options for `rhwp export-pdf`: installed generic fallback families (some Linux installations do
// not have rhwp's Korean defaults, which otherwise leaves every missing-font run blank), and Cambria Math for
// equations when none of the fonts rhwp draws their symbols with is installed.
func PdfOptions(lookup Lookup) []string {
	if lookup == nil {
		lookup = Find
	}
	var options []string
	addFallback := func(option string, families []string) {
		for _, f := range families {
			if font := lookup(f); font != nil && font.Usable {
				options = append(options, option, f)
				return
			}
		}
	}
	addFallback("--fallback-serif", []string{"Times New Roman", "Noto Serif", "DejaVu Serif", "Georgia", "Liberation Serif"})
	addFallback("--fallback-sans", []string{"Segoe UI", "Tahoma", "Arial", "Noto Sans", "DejaVu Sans", "Helvetica", "Liberation Sans"})
	addFallback("--fallback-mono", []string{"Cascadia Mono", "Consolas", "SF Mono", "Menlo", "Noto Sans Mono", "DejaVu Sans Mono", "Liberation Mono"})
	hasSymbols := false
	for _, f := range []string{"Latin Modern Math", "STIX Two Math", "Arial Unicode MS"} {
		if font := lookup(f); font != nil && font.Usable {
			hasSymbols = true
		}
	}
	if font := lookup("Cambria Math"); !hasSymbols && font != nil && font.Usable {
		options = append(options, "--equation-font", "Cambria Math")
	}
	return options
}

func applyFonts(header *xmltree.Element, text string, lookup Lookup) bool {
	type entry struct {
		e    *xmltree.Element
		slot slotFace
	}
	var fonts []entry
	var slots []slotFace
	for _, e := range header.DescendantsNamed("font") {
		face, ok := e.Attr("face")
		if !ok || e.Parent == nil || e.Parent.Local != "fontface" {
			continue
		}
		sf := slotFace{e.Parent.A("lang"), face}
		fonts = append(fonts, entry{e, sf})
		slots = append(slots, sf)
	}
	plan := Plan(slots, text, lookup)
	for _, f := range fonts {
		if replacement, ok := plan[f.slot]; ok {
			f.e.SetAttr("face", replacement)
		}
	}
	return len(plan) > 0
}

func child(e *xmltree.Element, name string) *xmltree.Element { return e.Child(name) }

func number(e *xmltree.Element, name string) (int, bool) {
	v, err := strconv.Atoi(e.A(name))
	return v, err == nil && e != nil
}

// FixShapeSizes writes the laid-out size (hp:curSz) of objects sized relative to the column, paragraph or page as
// their absolute size. True when something changed.
func FixShapeSizes(section *xmltree.Element) bool {
	changed := false
	section.Walk(func(shape *xmltree.Element) bool {
		if !sizedObjects[shape.Local] {
			return true
		}
		size, current := shape.Child("sz"), shape.Child("curSz")
		if size == nil || current == nil {
			return true
		}
		w, hasW := size.Attr("widthRelTo")
		h, hasH := size.Attr("heightRelTo")
		relative := hasW && w != "ABSOLUTE" || hasH && h != "ABSOLUTE"
		width, okW := number(current, "width")
		height, okH := number(current, "height")
		if !relative || !okW || width <= 0 || !okH || height <= 0 {
			return true
		}
		size.SetAttr("width", strconv.Itoa(width))
		size.SetAttr("height", strconv.Itoa(height))
		size.SetAttr("widthRelTo", "ABSOLUTE")
		size.SetAttr("heightRelTo", "ABSOLUTE")
		changed = true
		return true
	})
	return changed
}

// AnchorTablesInCells: in table cells, moves the stored line of a paragraph that holds nothing but one floating table
// (vertRelTo="PARA", top aligned, text above and below) from under the table up to the paragraph's top. Only when the
// line is where 한글 puts it, below the table. True when something changed.
func AnchorTablesInCells(section *xmltree.Element) bool {
	changed := false
	for _, paragraph := range section.DescendantsNamed("p") {
		parent := paragraph.Parent
		if parent == nil || parent.Local != "subList" || parent.Parent == nil || parent.Parent.Local != "tc" {
			continue
		}
		var content []*xmltree.Element
		for _, r := range paragraph.Children("run") {
			content = append(content, r.Elements()...)
		}
		var others []*xmltree.Element
		textual := false
		for _, e := range content {
			if e.Local != "t" {
				others = append(others, e)
			} else if strings.TrimSpace(e.Value()) != "" {
				textual = true
			}
		}
		if len(others) != 1 || others[0].Local != "tbl" || textual {
			continue
		}
		table := others[0]
		lines := child(paragraph, "linesegarray").Elements()
		if len(lines) != 1 {
			continue
		}
		line := lines[0]
		pos, size := child(table, "pos"), child(table, "sz")
		if pos == nil || size == nil || table.A("textWrap") != "TOP_AND_BOTTOM" || pos.A("treatAsChar") != "0" || pos.A("vertRelTo") != "PARA" {
			continue
		}
		if align, ok := pos.Attr("vertAlign"); ok && align != "TOP" {
			continue
		}
		vertpos, ok1 := number(line, "vertpos")
		offset, ok2 := number(pos, "vertOffset")
		height, ok3 := number(size, "height")
		if !ok1 || !ok2 || !ok3 {
			continue
		}
		margin := child(table, "outMargin")
		mt, _ := number(margin, "top")
		mb, _ := number(margin, "bottom")
		top := vertpos - offset - height - mt - mb
		// The previous paragraph's last line ends above the anchor paragraph: otherwise the line was not under the table.
		above := 0
		var previous *xmltree.Element
		for _, sibling := range parent.Elements() {
			if sibling == paragraph {
				break
			}
			if sibling.Local == "p" {
				previous = sibling
			}
		}
		if previous != nil {
			if ls := child(previous, "linesegarray").Elements(); len(ls) > 0 {
				last := ls[len(ls)-1]
				vp, _ := number(last, "vertpos")
				vs, _ := number(last, "vertsize")
				above = vp + vs
			}
		}
		if top < above || top >= vertpos {
			continue
		}
		line.SetAttr("vertpos", strconv.Itoa(top))
		changed = true
	}
	return changed
}

// SpaceEquationLines gives paragraphs without stored lines (written by Filee) that hold equations room above and
// below for what rhwp draws outside the line. rhwp makes a line as tall as its tallest equation, puts the line's
// baseline at 85 % of that height and the equation's own baseline on it, so an equation whose baseline is higher (a
// fraction, a matrix, a sum with limits) reaches below the line into the next one. True when something changed.
func SpaceEquationLines(section, header *xmltree.Element) bool {
	const minimumOverhang = 100
	paraProperties := header.FirstDescendant("paraProperties")
	if paraProperties == nil {
		return false
	}
	shapes := map[string]*xmltree.Element{}
	nextID := -1
	for _, e := range paraProperties.Children("paraPr") {
		id := e.A("id")
		if _, ok := shapes[id]; !ok {
			shapes[id] = e
		}
		v, err := strconv.Atoi(id)
		if err != nil {
			v = 0
		}
		nextID = max(nextID, v)
	}
	nextID++
	type key struct {
		id            string
		before, after int
	}
	spaced := map[key]string{}
	changed := false
	for _, paragraph := range section.DescendantsNamed("p") {
		if child(paragraph, "linesegarray") != nil {
			continue
		}
		type box struct{ height, baseline int }
		var boxes []box
		for _, r := range paragraph.Children("run") {
			for _, e := range r.Children("equation") {
				if child(e, "pos").A("treatAsChar") != "1" {
					continue
				}
				h, _ := number(child(e, "sz"), "height")
				b, ok := number(e, "baseLine")
				if !ok {
					b = 85
				}
				if h > 0 {
					boxes = append(boxes, box{h, b})
				}
			}
		}
		if len(boxes) == 0 {
			continue
		}
		line := 0
		for _, b := range boxes {
			line = max(line, b.height)
		}
		lineBaseline := float64(line) * 0.85
		var beforeF, afterF float64
		for _, b := range boxes {
			beforeF = math.Max(beforeF, math.Max(0, float64(b.height)*float64(b.baseline)/100.0-lineBaseline))
			afterF = math.Max(afterF, math.Max(0, lineBaseline-float64(b.height)*float64(b.baseline)/100.0+float64(b.height)-float64(line)))
		}
		before, after := int(math.Ceil(beforeF)), int(math.Ceil(afterF))
		// Less than a point (a subscript in running text) stays in the line spacing.
		if before < minimumOverhang {
			before = 0
		}
		if after < minimumOverhang {
			after = 0
		}
		id := paragraph.A("paraPrIDRef")
		shape, ok := shapes[id]
		if before == 0 && after == 0 || !ok {
			continue
		}
		k := key{id, before, after}
		newID, ok := spaced[k]
		if !ok {
			copy := shape.Copy()
			newID = strconv.Itoa(nextID)
			nextID++
			copy.SetAttr("id", newID)
			copy.Walk(func(margin *xmltree.Element) bool {
				if margin.Local != "prev" && margin.Local != "next" {
					return true
				}
				extra := after
				if margin.Local == "prev" {
					extra = before
				}
				// 한글 writes the margins doubled in hp:default (hp:case holds the real values).
				for _, a := range margin.Ancestors() {
					if a.Local == "default" {
						extra *= 2
						break
					}
				}
				v, _ := number(margin, "value")
				margin.SetAttr("value", strconv.Itoa(v+extra))
				return true
			})
			paraProperties.Add(copy)
			spaced[k] = newID
		}
		paragraph.SetAttr("paraPrIDRef", newID)
		changed = true
	}
	if changed {
		paraProperties.SetAttr("itemCnt", strconv.Itoa(len(paraProperties.Children("paraPr"))))
	}
	return changed
}

// NormaliseEquations rewrites every equation script as rhwp reads it.
func NormaliseEquations(section *xmltree.Element) bool {
	changed := false
	for _, script := range section.DescendantsNamed("script") {
		if script.Parent == nil || script.Parent.Local != "equation" {
			continue
		}
		if normalised := NormaliseEquation(script.Value()); normalised != script.Value() {
			script.SetValue(normalised)
			changed = true
		}
	}
	return changed
}

// NormaliseEquation is the script as rhwp reads it: commands glued to the next identifier spaced ("f CIRCf" →
// "f CIRC f"), and !=, +- written as NEQ, PLUSMINUS. Quoted text stays as it is.
func NormaliseEquation(script string) string {
	parts := strings.Split(script, `"`)
	for i := 0; i < len(parts); i += 2 { // odd parts are inside quotes
		p := identifier.ReplaceAllStringFunc(parts[i], splitIdentifier)
		for _, op := range operators {
			p = strings.ReplaceAll(p, op[0], op[1])
		}
		parts[i] = p
	}
	return strings.Join(parts, `"`)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func longestPrefix(list []string, s string) string {
	best := ""
	for _, c := range list {
		if strings.HasPrefix(s, c) && len(c) > len(best) {
			best = c
		}
	}
	return best
}

func allDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func splitIdentifier(id string) string {
	var words []string
	for {
		if contains(whole, id) {
			words = append(words, id)
			break
		}
		command := longestPrefix(upperCommands, id)
		if command == "" {
			lower := longestPrefix(lowerCommands, id)
			rest := ""
			if lower != "" {
				rest = id[len(lower):]
			}
			if rest != "" && (len(rest) <= 2 || allDigits(rest)) {
				command = lower
			}
		}
		longerWhole := false
		for _, w := range whole {
			if strings.HasPrefix(id, w) && len(w) > len(command) {
				longerWhole = true
			}
		}
		if command == "" || len(command) == len(id) || longerWhole {
			words = append(words, id)
			break
		}
		words = append(words, command)
		id = id[len(command):]
	}
	return strings.Join(words, " ")
}

// ───────────────────────── Punctuation widths (#237) ─────────────────────────
//
// rhwp lays text out with width tables of its own: Korean fonts and common Latin fonts have real ones, every other
// font (SimSun, Microsoft YaHei, KaiTi, MS Mincho, …) gets a guess in which quotes, dashes and CJK punctuation are
// half a character wide. In the copy rhwp renders, such characters get a run of their own whose character shape adds
// letter spacing up to the font's real width. rhwp's letter spacing is a percentage of the character's own width.

type guess struct {
	slot  string
	width float64
}

var guessedWidths = func() map[rune]guess {
	g := map[rune]guess{'‘': {"HANGUL", 0.3}, '’': {"HANGUL", 0.3}}
	for _, c := range "“”—―\u2013‥…•※‰′" {
		g[c] = guess{"HANGUL", 0.5}
	}
	for _, c := range "、。〃々〇〈〉《》「」『』【】〔〕〖〗〝〞" {
		g[c] = guess{"SYMBOL", 0.5}
	}
	return g
}()

var slotNames = []string{"hangul", "latin", "hanja", "japanese", "other", "symbol", "user"}

// WidenPunctuation gives the punctuation above its font's real width in rhwp's layout. True when something changed.
func WidenPunctuation(header *xmltree.Element, sections []*xmltree.Element, lookup Lookup) bool {
	charProperties := header.FirstDescendant("charProperties")
	if charProperties == nil {
		return false
	}
	shapes := map[string]*xmltree.Element{}
	nextID := -1
	for _, e := range charProperties.Children("charPr") {
		id := e.A("id")
		if _, ok := shapes[id]; !ok {
			shapes[id] = e
		}
		v, err := strconv.Atoi(id)
		if err != nil {
			v = 0
		}
		nextID = max(nextID, v)
	}
	nextID++
	faces := map[string]map[string]string{}
	for _, ff := range header.DescendantsNamed("fontface") {
		m := map[string]string{}
		for _, f := range ff.Children("font") {
			m[f.A("id")] = f.A("face")
		}
		faces[ff.A("lang")] = m
	}
	type sk struct {
		id string
		c  rune
	}
	spacings := map[sk]*int{}
	widened := map[[2]string]string{}

	// spacing brings a character in this shape to its real width; nil when rhwp's is right.
	spacing := func(id string, c rune) *int {
		if v, ok := spacings[sk{id, c}]; ok {
			return v
		}
		var result *int
		g, ok := guessedWidths[c]
		shape := shapes[id]
		if ok && shape != nil {
			slot := strings.ToLower(g.slot)
			fontID, hasFont := child(shape, "fontRef").Attr(slot)
			face := ""
			if list, ok := faces[g.slot]; hasFont && ok {
				face = list[fontID]
			}
			if face != "" {
				if font := lookup(face); font != nil && font.Usable && font.Advance != nil && guesses(face, font) {
					if real, ok := font.Advance(c); ok && real > g.width+0.05 {
						current, _ := number(child(shape, "spacing"), slot)
						v := clamp(int(math.RoundToEven(real/g.width*float64(100+current)))-100, -50, 500)
						result = &v
					}
				}
			}
		}
		spacings[sk{id, c}] = result
		return result
	}
	widen := func(id string, s int) string {
		k := [2]string{id, strconv.Itoa(s)}
		if existing, ok := widened[k]; ok {
			return existing
		}
		copy := shapes[id].Copy()
		newID := strconv.Itoa(nextID)
		nextID++
		copy.SetAttr("id", newID)
		element := child(copy, "spacing")
		if element == nil {
			element = xmltree.New(copy.Prefix, copy.Space, "spacing")
			if anchor := child(copy, "ratio"); anchor != nil {
				anchor.AddAfterSelf(element)
			} else if anchor := child(copy, "fontRef"); anchor != nil {
				anchor.AddAfterSelf(element)
			} else {
				copy.AddFirst(element)
			}
		}
		// The run holds only the widened characters, so every slot can take the same spacing.
		for _, slot := range slotNames {
			element.SetAttr(slot, strconv.Itoa(s))
		}
		charProperties.Add(copy)
		widened[k] = newID
		return newID
	}

	changed := false
	for _, section := range sections {
		for _, run := range section.DescendantsNamed("run") {
			id := run.A("charPrIDRef")
			needs := false
			for _, t := range run.Children("t") {
				for _, c := range t.Value() {
					if spacing(id, c) != nil {
						needs = true
					}
				}
			}
			if !needs {
				continue
			}
			pieces := splitRun(run, func(c rune) string {
				if s := spacing(id, c); s != nil {
					return widen(id, *s)
				}
				return ""
			})
			nodes := make([]xmltree.Node, len(pieces))
			for i, p := range pieces {
				nodes[i] = p
			}
			run.ReplaceWith(nodes...)
			changed = true
		}
	}
	if changed {
		charProperties.SetAttr("itemCnt", strconv.Itoa(len(charProperties.Children("charPr"))))
	}
	return changed
}

// guesses: rhwp has no width table for the font: Chinese and Japanese fonts. Korean fonts (with Hangul) and Latin
// fonts (without Han) have tables.
func guesses(face string, font *InstalledFont) bool {
	switch Script(face) {
	case "zh-Hans", "zh-Hant", "ja":
		return true
	}
	return font.Has('一') && !font.Has('가')
}

// splitRun splits a run so that the characters shapeOf gives a character shape for are in runs of that shape;
// everything else (other text, tabs, controls) stays in the run's own shape, in order.
func splitRun(run *xmltree.Element, shapeOf func(rune) string) []*xmltree.Element {
	baseID := run.A("charPrIDRef")
	var pieces []*xmltree.Element
	var current, text *xmltree.Element
	currentID := ""
	piece := func(id string) *xmltree.Element {
		if current == nil || currentID != id {
			current = xmltree.New(run.Prefix, run.Space, run.Local)
			for _, a := range run.Attrs {
				if a.Local != "charPrIDRef" {
					current.Attrs = append(current.Attrs, a)
				}
			}
			current.SetAttr("charPrIDRef", id)
			currentID = id
			pieces = append(pieces, current)
			text = nil
		}
		return current
	}
	for _, node := range run.Kids {
		t, isT := node.(*xmltree.Element)
		if !isT || t.Local != "t" {
			switch n := node.(type) {
			case *xmltree.Element:
				piece(baseID).Add(n.Copy())
			case *xmltree.Text:
				piece(baseID).Add(&xmltree.Text{Data: n.Data})
			}
			text = nil
			continue
		}
		textOf := func(id string) *xmltree.Element {
			p := piece(id)
			if text == nil {
				text = xmltree.New(t.Prefix, t.Space, t.Local)
				text.Attrs = append(text.Attrs, t.Attrs...)
				p.Add(text)
			}
			return text
		}
		for _, sub := range t.Kids {
			switch v := sub.(type) {
			case *xmltree.Text:
				// Consecutive characters of the same shape stay together ("……", "——").
				chars := []rune(v.Data)
				for start := 0; start < len(chars); {
					id := or(shapeOf(chars[start]), baseID)
					end := start + 1
					for end < len(chars) && or(shapeOf(chars[end]), baseID) == id {
						end++
					}
					textOf(id).AddText(string(chars[start:end]))
					start = end
				}
			case *xmltree.Element:
				textOf(baseID).Add(v.Copy())
			}
		}
		if len(t.Kids) == 0 {
			textOf(baseID)
		}
		text = nil
	}
	return pieces
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func clamp(v, lo, hi int) int {
	return max(lo, min(hi, v))
}

func randomName() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
