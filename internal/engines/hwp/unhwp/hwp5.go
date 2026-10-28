package unhwp

// HWP 5.0 (binary) → document model, without Hancom Office: an OLE compound file whose streams hold records
// (FileHeader, DocInfo with fonts, character and paragraph shapes, numberings, border fills and styles,
// BodyText/SectionN with the paragraphs, BinData/BINxxxx with the pictures), deflated when the file says so.
//
// The record tree: a record's children are the records after it one level deeper. A paragraph (PARA_HEADER) has its
// text (PARA_TEXT, UTF-16 with control characters), character shape runs (PARA_CHAR_SHAPE) and controls
// (CTRL_HEADER: section and column definitions, tables, drawing objects, headers and footers, notes, fields ...).
// An extended control character in the text stands for the next control of the paragraph.
//
// What the model holds is read: text with fonts, sizes, colours, bold / italic / underline / strike / super- and
// subscript, paragraph alignment, margins, line spacing, tab stops, lists and outline numbering, page setup and
// columns, headers and footers with page numbers, footnotes and endnotes, hyperlinks and bookmarks, tables with
// spans, borders and fills, pictures (inline or floating), text boxes and simple shapes, equations as their script.
// Distribution documents (배포용 문서, the text encrypted) and password-protected files are refused.

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/richardlehane/mscfb"

	"github.com/dseif0x/filee-go/internal/doc"
)

// Record tags (HWPTAG_BEGIN = 0x10).
const (
	tagDocumentProperties = 0x10
	tagIDMappings         = 0x11
	tagBinData            = 0x12
	tagFaceName           = 0x13
	tagBorderFill         = 0x14
	tagCharShape          = 0x15
	tagTabDef             = 0x16
	tagNumbering          = 0x17
	tagBullet             = 0x18
	tagParaShape          = 0x19
	tagStyle              = 0x1A

	tagParaHeader     = 0x42
	tagParaText       = 0x43
	tagParaCharShape  = 0x44
	tagParaLineSeg    = 0x45
	tagCtrlHeader     = 0x47
	tagListHeader     = 0x48
	tagPageDef        = 0x49
	tagShapeComponent = 0x4C
	tagTable          = 0x4D
	tagShapeLine      = 0x4E
	tagShapeRectangle = 0x4F
	tagShapeEllipse   = 0x50
	tagShapeArc       = 0x51
	tagShapePolygon   = 0x52
	tagShapeCurve     = 0x53
	tagShapeOLE       = 0x54
	tagShapePicture   = 0x55
	tagShapeContainer = 0x56
	tagCtrlData       = 0x57
	tagEqEdit         = 0x58
	tagShapeTextArt   = 0x5A
)

func ctrlID(s string) uint32 {
	return uint32(s[0])<<24 | uint32(s[1])<<16 | uint32(s[2])<<8 | uint32(s[3])
}

var (
	ctrlSection    = ctrlID("secd")
	ctrlColumn     = ctrlID("cold")
	ctrlTable      = ctrlID("tbl ")
	ctrlShape      = ctrlID("gso ")
	ctrlEquation   = ctrlID("eqed")
	ctrlHeader     = ctrlID("head")
	ctrlFooter     = ctrlID("foot")
	ctrlFootnote   = ctrlID("fn  ")
	ctrlEndnote    = ctrlID("en  ")
	ctrlAutoNumber = ctrlID("atno")
	ctrlNewNumber  = ctrlID("nwno")
	ctrlPageNumber = ctrlID("pgnp")
	ctrlBookmark   = ctrlID("bokm")
	ctrlDutmal     = ctrlID("tdut")
	ctrlCompose    = ctrlID("tcps")
	ctrlHyperlink  = ctrlID("%hlk")
	shapePicture   = ctrlID("$pic")
	shapeRectangle = ctrlID("$rec")
	shapeEllipse   = ctrlID("$ell")
	shapeLine      = ctrlID("$lin")
	shapeContainer = ctrlID("$con")
)

type record struct {
	tag, level int
	data       []byte
	children   []*record
}

// ───────────────────────── Binary helpers ─────────────────────────

type reader struct {
	b   []byte
	pos int
	bad bool
}

func (r *reader) need(n int) bool {
	if r.pos+n > len(r.b) {
		r.bad = true
		return false
	}
	return true
}

func (r *reader) u8() int {
	if !r.need(1) {
		return 0
	}
	r.pos++
	return int(r.b[r.pos-1])
}

func (r *reader) u16() int {
	if !r.need(2) {
		return 0
	}
	r.pos += 2
	return int(binary.LittleEndian.Uint16(r.b[r.pos-2:]))
}

func (r *reader) i16() int { return int(int16(r.u16())) }

func (r *reader) u32() uint32 {
	if !r.need(4) {
		return 0
	}
	r.pos += 4
	return binary.LittleEndian.Uint32(r.b[r.pos-4:])
}

func (r *reader) i32() int { return int(int32(r.u32())) }

func (r *reader) skip(n int) {
	if r.need(n) {
		r.pos += n
	}
}

// str reads a length-prefixed (WORD) UTF-16 string.
func (r *reader) str() string {
	n := r.u16()
	if !r.need(2 * n) {
		return ""
	}
	u := make([]uint16, n)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(r.b[r.pos+2*i:])
	}
	r.pos += 2 * n
	return string(utf16.Decode(u))
}

func at(b []byte, offset, size int) (uint32, bool) {
	if offset+size > len(b) {
		return 0, false
	}
	switch size {
	case 1:
		return uint32(b[offset]), true
	case 2:
		return uint32(binary.LittleEndian.Uint16(b[offset:])), true
	}
	return binary.LittleEndian.Uint32(b[offset:]), true
}

// colorRef turns a COLORREF (0x00BBGGRR) into "#RRGGBB"; "" for "none" (0xFFFFFFFF).
func colorRef(v uint32) string {
	if v == 0xFFFFFFFF {
		return ""
	}
	return fmt.Sprintf("#%02X%02X%02X", v&0xFF, v>>8&0xFF, v>>16&0xFF)
}

// parseRecords splits a stream into records and builds the tree by levels.
func parseRecords(b []byte) []*record {
	var all []*record
	for pos := 0; pos+4 <= len(b); {
		header := binary.LittleEndian.Uint32(b[pos:])
		pos += 4
		tag, level, size := int(header&0x3FF), int(header>>10&0x3FF), int(header>>20&0xFFF)
		if size == 0xFFF {
			if pos+4 > len(b) {
				break
			}
			size = int(binary.LittleEndian.Uint32(b[pos:]))
			pos += 4
		}
		if size < 0 || pos+size > len(b) {
			size = len(b) - pos
		}
		all = append(all, &record{tag: tag, level: level, data: b[pos : pos+size]})
		pos += size
	}
	var roots []*record
	var stack []*record
	for _, r := range all {
		for len(stack) > 0 && stack[len(stack)-1].level >= r.level {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			roots = append(roots, r)
		} else {
			parent := stack[len(stack)-1]
			parent.children = append(parent.children, r)
		}
		stack = append(stack, r)
	}
	return roots
}

// flattenRecords lists a record tree in file order (DocInfo nests its lists under ID_MAPPINGS).
func flattenRecords(roots []*record) []*record {
	var out []*record
	for _, r := range roots {
		out = append(out, r)
		out = append(out, flattenRecords(r.children)...)
	}
	return out
}

func inflate(b []byte) ([]byte, error) {
	return io.ReadAll(flate.NewReader(bytes.NewReader(b)))
}

// ───────────────────────── Document info ─────────────────────────

type charShape struct {
	faces          [7]int
	spacing        int
	size           int
	attr           uint32
	textColor      string
	underlineColor string
	shadeColor     string
}

type paraShape struct {
	attr1, attr3               uint32
	left, right, indent        int
	prev, next, lineSpacingOld int
	tabDef, numbering          int
	lineSpacing                int
	hasNewSpacing              bool
}

type binItem struct {
	id        int
	extension string
	kind      int
	compress  int
}

type numberingDef struct {
	levels []doc.NumberingLevel
}

type styleDef struct {
	name, engName string
}

type docInfo struct {
	compressed bool
	faces      [7][]string
	charShapes []charShape
	paraShapes []paraShape
	borders    []borderFill
	tabDefs    [][]doc.TabStop
	numberings []numberingDef
	bullets    []string
	binItems   []binItem
	styles     []styleDef
}

type borderFill struct {
	borders doc.Borders
	fill    string
}

// borderWidths are the line widths HWP numbers 0..15 (mm).
var borderWidths = []float64{0.1, 0.12, 0.15, 0.2, 0.25, 0.3, 0.4, 0.5, 0.6, 0.7, 1.0, 1.5, 2.0, 3.0, 4.0, 5.0}

func lineStyle(kind int) doc.BorderStyle {
	switch kind {
	case 0:
		return doc.BorderNone
	case 1:
		return doc.BorderSolid
	case 2, 6:
		return doc.BorderDash
	case 3, 7:
		return doc.BorderDot
	case 4, 5:
		return doc.BorderDashDot
	case 8, 9, 10, 11, 13:
		return doc.BorderDouble
	}
	return doc.BorderSolid
}

func (info *docInfo) read(records []*record) {
	var counts []int
	faceIndex := 0
	for _, rec := range records {
		r := &reader{b: rec.data}
		switch rec.tag {
		case tagIDMappings:
			for !r.bad && r.pos+4 <= len(r.b) {
				counts = append(counts, r.i32())
			}
		case tagFaceName:
			r.u8()
			name := r.str()
			// Fonts come grouped by language, in the numbers ID_MAPPINGS gives (Hangul, Latin, Hanja, ...).
			language := 0
			for total := 0; language < 7; language++ {
				n := 0
				if 1+language < len(counts) {
					n = counts[1+language]
				}
				if faceIndex < total+n {
					break
				}
				total += n
			}
			if language < 7 {
				info.faces[language] = append(info.faces[language], name)
			}
			faceIndex++
		case tagCharShape:
			var c charShape
			for i := range c.faces {
				c.faces[i] = r.u16()
			}
			r.skip(7) // ratios
			c.spacing = int(int8(r.u8()))
			r.skip(6 + 7 + 7) // other spacings, relative sizes, offsets
			c.size = r.i32()
			c.attr = r.u32()
			r.skip(2) // shadow gaps
			c.textColor = colorRef(r.u32())
			c.underlineColor = colorRef(r.u32())
			c.shadeColor = colorRef(r.u32())
			info.charShapes = append(info.charShapes, c)
		case tagParaShape:
			var p paraShape
			p.attr1 = r.u32()
			p.left, p.right, p.indent = r.i32(), r.i32(), r.i32()
			p.prev, p.next, p.lineSpacingOld = r.i32(), r.i32(), r.i32()
			p.tabDef, p.numbering = r.u16(), r.u16()
			r.skip(2 + 8) // border fill, border offsets
			r.u32()       // attr2
			if r.pos+8 <= len(r.b) {
				p.attr3 = r.u32()
				p.lineSpacing = int(r.u32())
				p.hasNewSpacing = true
			}
			info.paraShapes = append(info.paraShapes, p)
		case tagBorderFill:
			r.u16()
			var sides [4]doc.Border
			for i := range sides {
				kind, width, color := r.u8(), r.u8(), r.u32()
				style := lineStyle(kind)
				if style == doc.BorderNone {
					sides[i] = doc.NoBorder
					continue
				}
				w := 0.12
				if width < len(borderWidths) {
					w = borderWidths[width]
				}
				sides[i] = doc.Border{Style: style, WidthMm: w, Color: colorRef(color)}
			}
			r.skip(6) // diagonal
			fill := ""
			if kinds := r.u32(); kinds&1 != 0 {
				fill = colorRef(r.u32())
				if fill == "#FFFFFF" {
					fill = ""
				}
			}
			info.borders = append(info.borders, borderFill{doc.Borders{Left: sides[0], Right: sides[1], Top: sides[2], Bottom: sides[3]}, fill})
		case tagTabDef:
			r.u32()
			count := r.i16()
			var tabs []doc.TabStop
			for i := 0; i < count && !r.bad; i++ {
				position, kind, leader := int(r.u32()), r.u8(), r.u8()
				r.skip(2)
				t := doc.TabStop{Position: position / 2, Leader: "NONE"}
				switch kind {
				case 1:
					t.Kind = doc.TabRight
				case 2:
					t.Kind = doc.TabCenter
				case 3:
					t.Kind = doc.TabDecimal
				}
				switch leader {
				case 1:
					t.Leader = "SOLID"
				case 2, 6:
					t.Leader = "DASH"
				case 3, 7:
					t.Leader = "DOT"
				}
				if !r.bad {
					tabs = append(tabs, t)
				}
			}
			info.tabDefs = append(info.tabDefs, tabs)
		case tagNumbering:
			info.numberings = append(info.numberings, readNumbering(r))
		case tagBullet:
			r.skip(12)
			symbol := "●"
			if c := r.u16(); c > 0 && !(c >= 0xE000 && c <= 0xF8FF) {
				symbol = string(rune(c))
			}
			info.bullets = append(info.bullets, symbol)
		case tagBinData:
			attr := r.u16()
			item := binItem{kind: attr & 0xF, compress: attr >> 4 & 0x3}
			if item.kind == 0 {
				r.str()
				r.str()
			} else {
				item.id = r.u16()
				if item.kind == 1 {
					item.extension = strings.ToLower(r.str())
				}
			}
			info.binItems = append(info.binItems, item)
		case tagStyle:
			info.styles = append(info.styles, styleDef{name: r.str(), engName: r.str()})
		}
	}
}

// numberFormats are HWP's number shapes (0 = 1, 2, 3 ...) in OWPML.
var numberFormats = []string{"DIGIT", "CIRCLED_DIGIT", "ROMAN_CAPITAL", "ROMAN_SMALL", "LATIN_CAPITAL", "LATIN_SMALL",
	"CIRCLED_LATIN_CAPITAL", "CIRCLED_LATIN_SMALL", "HANGUL_SYLLABLE", "CIRCLED_HANGUL_SYLLABLE", "HANGUL_JAMO",
	"CIRCLED_HANGUL_JAMO", "HANGUL_PHONETIC", "IDEOGRAPH", "CIRCLED_IDEOGRAPH"}

func readNumbering(r *reader) numberingDef {
	var n numberingDef
	var formats []string
	for level := 0; level < 7 && !r.bad; level++ {
		attr := r.u32()
		r.skip(8) // width adjust, text offset, character shape
		text := r.str()
		format := "DIGIT"
		if shape := int(attr >> 5 & 0xF); shape < len(numberFormats) {
			format = numberFormats[shape]
		}
		formats = append(formats, format)
		n.levels = append(n.levels, doc.NumberingLevel{Format: format, Text: text, Start: 1, Bullet: text != "" && !hasCounter(text)})
	}
	start := r.u16()
	for i := range n.levels {
		n.levels[i].Start = max(1, start)
	}
	// Level start numbers (5.0.2.5 and later).
	for i := range n.levels {
		if r.pos+4 > len(r.b) {
			break
		}
		if v := int(r.u32()); v > 0 {
			n.levels[i].Start = v
		}
	}
	_ = formats
	return n
}

func hasCounter(text string) bool {
	for i := 0; i+1 < len(text); i++ {
		if text[i] == '^' && text[i+1] >= '0' && text[i+1] <= '9' {
			return true
		}
	}
	return false
}

// ───────────────────────── Package ─────────────────────────

// ErrDistribution and ErrPassword are documents Filee cannot read.
var (
	ErrPassword     = errors.New("the HWP file is password-protected. Open it in 한글 (Hangul), remove the password, save it and convert it again")
	ErrDistribution = errors.New("the HWP file is a distribution document (배포용 문서) whose text is encrypted; it cannot be converted")
	ErrOldVersion   = errors.New("the file is an HWP 3.0 or older document, which is not supported")
)

type hwpFile struct {
	streams     map[string][]byte
	info        docInfo
	mediaFolder string
	media       map[int]string
	title       string
}

// ReadHWP reads an HWP 5.0 file into the document model; pictures are extracted into mediaFolder.
func ReadHWP(path, mediaFolder string) (*doc.Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	head := make([]byte, 32)
	if n, _ := f.ReadAt(head, 0); n >= 17 && bytes.HasPrefix(head, []byte("HWP Document File V")) {
		return nil, ErrOldVersion
	}
	h, err := readStreams(path)
	if err != nil {
		return nil, err
	}
	h.mediaFolder = mediaFolder
	return h.read()
}

// readStreams reads every stream of the compound file.
func readStreams(path string) (*hwpFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cfb, err := mscfb.New(f)
	if err != nil {
		return nil, fmt.Errorf("the file is not an HWP document: %w", err)
	}
	h := &hwpFile{streams: map[string][]byte{}, media: map[int]string{}}
	for entry, err := cfb.Next(); err == nil; entry, err = cfb.Next() {
		if entry.Size <= 0 {
			continue
		}
		data := make([]byte, entry.Size)
		if _, err := io.ReadFull(entry, data); err != nil && err != io.EOF {
			continue
		}
		name := strings.Join(append(append([]string{}, entry.Path...), entry.Name), "/")
		h.streams[name] = data
	}
	if header := h.streams["FileHeader"]; len(header) >= 40 {
		h.info.compressed = binary.LittleEndian.Uint32(header[36:])&1 != 0
	}
	return h, nil
}

func (h *hwpFile) read() (*doc.Document, error) {
	header := h.streams["FileHeader"]
	if len(header) < 40 || !bytes.HasPrefix(header, []byte("HWP Document File")) {
		return nil, errors.New("the file is not an HWP 5.0 document (no FileHeader)")
	}
	properties := binary.LittleEndian.Uint32(header[36:])
	h.info.compressed = properties&1 != 0
	if properties&2 != 0 {
		return nil, ErrPassword
	}
	if properties&4 != 0 {
		return nil, ErrDistribution
	}
	docInfo, err := h.stream("DocInfo")
	if err != nil {
		return nil, err
	}
	h.info.read(flattenRecords(parseRecords(docInfo)))
	h.title = summaryTitle(h.streams["\x05HwpSummaryInformation"])

	d := &doc.Document{Title: h.title}
	for i := 0; ; i++ {
		data, ok := h.streams[fmt.Sprintf("BodyText/Section%d", i)]
		if !ok {
			break
		}
		if h.info.compressed {
			if data, err = inflate(data); err != nil {
				return nil, fmt.Errorf("BodyText/Section%d cannot be read: %w", i, err)
			}
		}
		h.readSection(parseRecords(data), d)
	}
	if len(d.Sections) == 0 {
		return nil, errors.New("the HWP file has no body text")
	}
	return d, nil
}

func (h *hwpFile) stream(name string) ([]byte, error) {
	data, ok := h.streams[name]
	if !ok {
		return nil, fmt.Errorf("the HWP file has no %s", name)
	}
	if h.info.compressed {
		return inflate(data)
	}
	return data, nil
}

// summaryTitle is the title (PID 2) of the OLE summary information, if any.
func summaryTitle(b []byte) string {
	if len(b) < 48 {
		return ""
	}
	sectionOffset := int(binary.LittleEndian.Uint32(b[44:]))
	if sectionOffset+8 > len(b) {
		return ""
	}
	count := int(binary.LittleEndian.Uint32(b[sectionOffset+4:]))
	for i := 0; i < count && sectionOffset+8+8*i+8 <= len(b); i++ {
		pid := binary.LittleEndian.Uint32(b[sectionOffset+8+8*i:])
		offset := sectionOffset + int(binary.LittleEndian.Uint32(b[sectionOffset+8+8*i+4:]))
		if pid != 2 || offset+8 > len(b) {
			continue
		}
		kind := binary.LittleEndian.Uint32(b[offset:])
		n := int(binary.LittleEndian.Uint32(b[offset+4:]))
		switch kind {
		case 0x1F: // VT_LPWSTR
			if offset+8+2*n > len(b) {
				return ""
			}
			u := make([]uint16, n)
			for j := range u {
				u[j] = binary.LittleEndian.Uint16(b[offset+8+2*j:])
			}
			return strings.TrimSpace(strings.TrimRight(string(utf16.Decode(u)), "\x00"))
		case 0x1E: // VT_LPSTR
			if offset+8+n > len(b) {
				return ""
			}
			return strings.TrimSpace(strings.TrimRight(string(b[offset+8:offset+8+n]), "\x00"))
		}
	}
	return ""
}

// picture extracts a BinData item (1-based id) into the media folder once; "" when it is missing.
func (h *hwpFile) picture(id int) string {
	if p, ok := h.media[id]; ok {
		return p
	}
	result := ""
	if id >= 1 && id <= len(h.info.binItems) {
		item := h.info.binItems[id-1]
		ext := item.extension
		if ext == "" {
			ext = "bin"
		}
		data, ok := h.streams[fmt.Sprintf("BinData/BIN%04X.%s", item.id, ext)]
		if !ok {
			data, ok = h.streams[fmt.Sprintf("BinData/BIN%04X.%s", item.id, strings.ToUpper(ext))]
		}
		if ok {
			if item.compress == 1 || item.compress == 0 && h.info.compressed {
				if inflated, err := inflate(data); err == nil {
					data = inflated
				}
			}
			if os.MkdirAll(h.mediaFolder, 0o755) == nil {
				p := filepath.Join(h.mediaFolder, fmt.Sprintf("hwp-media-%d.%s", len(h.media)+1, ext))
				if os.WriteFile(p, data, 0o644) == nil {
					result = p
				}
			}
		}
	}
	h.media[id] = result
	return result
}

func itoa(v int) string { return strconv.Itoa(v) }
