package subtitles

import (
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Scenarist SCC: broadcast closed captions (CEA-608) as hex byte pairs with
// SMPTE time codes. A small 608 decoder for caption channel 1 (CC1): pop-on
// captions (loaded off screen, shown at End Of Caption, removed at Erase
// Displayed Memory), paint-on and roll-up captions, row addresses, italics, and
// the special and extended character sets. Every byte pair takes one frame at
// 29.97 fps; control codes sent twice (as broadcasters do) count once.

// sccFrame is one NTSC frame: 1001/30000 s.
const sccFrame = 1001.0 / 30000

var reSccLine = regexp.MustCompile(`^(?P<h>\d{2}):(?P<m>\d{2}):(?P<s>\d{2})(?P<sep>[:;.,])(?P<f>\d{2})\s+(?P<data>.+)$`)

func readScc(text string) (*Document, error) {
	if len(strings.TrimLeft(text, "\uFEFF \n")) < 13 || !strings.EqualFold(strings.TrimLeft(text, "\uFEFF \n")[:13], "Scenarist_SCC") {
		return nil, errors.New(`This is not an SCC file (it must start with "Scenarist_SCC V1.0").`)
	}
	d := &Document{}
	dec := newSccDecoder(d)
	var last time.Duration
	for _, line := range strings.Split(text, "\n") {
		m := reSccLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		start := sccTimeCode(m)
		for i, w := range strings.Fields(m[6]) {
			word, err := strconv.ParseUint(w, 16, 16)
			if len(w) != 4 || err != nil {
				continue
			}
			last = start + fromSeconds(float64(i)*sccFrame)
			dec.pair(last, byte(word>>8)&0x7F, byte(word)&0x7F)
		}
	}
	dec.finish(last)
	return d, nil
}

// sccTimeCode reads a SMPTE time code: frames counted at 30 per second (with
// drop-frame numbering when written with ";"), played at 29.97.
func sccTimeCode(m []string) time.Duration {
	n := func(s string) int64 { v, _ := strconv.ParseInt(s, 10, 64); return v }
	h, mi, s, f := n(m[1]), n(m[2]), n(m[3]), n(m[5])
	frames := ((h*60+mi)*60+s)*30 + f
	if m[4] == ";" || m[4] == "." {
		minutes := 60*h + mi
		frames -= 2 * (minutes - minutes/10)
	}
	return fromSeconds(float64(frames) * sccFrame)
}

type sccMode int

const (
	popOn sccMode = iota
	paintOn
	rollUp
)

type sccDecoder struct {
	doc         *Document
	mode        sccMode
	rollUpRows  int
	displayed   *sccMemory
	hidden      *sccMemory
	shownSince  *time.Duration
	channelOne  bool
	lastControl *[2]byte
}

func newSccDecoder(d *Document) *sccDecoder {
	return &sccDecoder{doc: d, rollUpRows: 2, displayed: newSccMemory(), hidden: newSccMemory(), channelOne: true}
}

func (d *sccDecoder) target() *sccMemory {
	if d.mode == popOn {
		return d.hidden
	}
	return d.displayed
}

func (d *sccDecoder) pair(t time.Duration, hi, lo byte) {
	// Padding (0x80 0x80 with parity) and XDS data (first byte 0x01–0x0F) carry no caption text.
	if hi < 0x10 {
		d.lastControl = nil
		return
	}
	if hi <= 0x1F {
		// Control codes are sent twice so a damaged one is not missed: the repeat does nothing.
		if d.lastControl != nil && *d.lastControl == [2]byte{hi, lo} {
			d.lastControl = nil
			return
		}
		d.lastControl = &[2]byte{hi, lo}
		d.control(t, hi, lo)
		return
	}
	d.lastControl = nil
	if !d.channelOne {
		return
	}
	d.character(t, hi)
	if lo >= 0x20 {
		d.character(t, lo)
	}
}

func (d *sccDecoder) control(t time.Duration, hi, lo byte) {
	// Channel 2 uses the same codes with bit 3 of the first byte set; 0x15 / 0x1D misc codes are field 2.
	channel := 1
	if hi >= 0x18 {
		channel = 2
	}
	basic := hi & 0xF7
	if (basic == 0x14 || basic == 0x15) && lo >= 0x20 && lo <= 0x2F {
		d.channelOne = channel == 1 && basic == 0x14
		if d.channelOne {
			d.misc(t, lo)
		}
		return
	}
	d.channelOne = channel == 1
	if !d.channelOne {
		return
	}
	switch {
	case lo >= 0x40:
		d.preamble(basic, lo)
	case basic == 0x11 && lo >= 0x20 && lo <= 0x2F:
		// Mid-row code: colour or italics, shown as a space.
		d.target().append(t, " ")
		d.target().setItalic(lo&0x0E == 0x0E)
	case basic == 0x11 && lo >= 0x30 && lo <= 0x3F:
		d.target().append(t, string(sccSpecial[lo-0x30]))
	case (basic == 0x12 || basic == 0x13) && lo >= 0x20 && lo <= 0x3F:
		// Extended characters replace the plain fallback character sent before them.
		d.target().backspace()
		set := sccExtended1
		if basic == 0x13 {
			set = sccExtended2
		}
		d.target().append(t, string(set[lo-0x20]))
	}
	// Tab offsets and background attributes don't change the text.
}

func (d *sccDecoder) misc(t time.Duration, code byte) {
	switch code {
	case 0x20: // Resume Caption Loading
		d.mode = popOn
	case 0x21: // Backspace
		d.target().backspace()
	case 0x24: // Delete to End of Row
		d.target().clearRow()
	case 0x25, 0x26, 0x27: // Roll-Up 2, 3, 4 rows
		if d.mode != rollUp {
			d.show(t, newSccMemory())
		}
		d.mode = rollUp
		d.rollUpRows = int(code) - 0x23
	case 0x29: // Resume Direct Captioning
		d.mode = paintOn
	case 0x2C: // Erase Displayed Memory
		d.show(t, newSccMemory())
	case 0x2D: // Carriage Return: the row scrolls up
		if d.mode == rollUp {
			d.show(t, d.displayed.rolledUp(d.rollUpRows))
			d.shownSince = nil
		}
	case 0x2E: // Erase Non-displayed Memory
		d.hidden = newSccMemory()
	case 0x2F: // End Of Caption: swap memories
		next := d.hidden
		d.hidden = newSccMemory()
		d.show(t, next)
		d.mode = popOn
	}
}

var (
	sccPreambleRows     = []byte{0x11, 0x12, 0x15, 0x16, 0x17, 0x10, 0x13, 0x14}
	sccPreambleFirstRow = []int{1, 3, 5, 7, 9, 11, 12, 14}
)

func (d *sccDecoder) preamble(basic, lo byte) {
	row := 15
	for i, b := range sccPreambleRows {
		if b == basic {
			if basic == 0x10 {
				row = 11
			} else {
				row = sccPreambleFirstRow[i]
				if lo >= 0x60 {
					row++
				}
			}
			break
		}
	}
	if d.mode == rollUp {
		row = 15 // roll-up captions always write on the base row
	}
	d.target().moveTo(row, lo&0x1E == 0x0E)
}

func (d *sccDecoder) character(t time.Duration, code byte) {
	if code < 0x20 {
		return
	}
	d.target().append(t, string(sccBasic(code)))
	if d.mode != popOn && d.shownSince == nil {
		d.shownSince = d.displayed.firstWrite
	}
}

// show ends what is on screen (as a cue) and shows next from now on.
func (d *sccDecoder) show(t time.Duration, next *sccMemory) {
	shown := d.shownSince
	if shown == nil {
		shown = d.displayed.firstWrite
	}
	if shown != nil {
		if text := d.displayed.text(); text != "" {
			d.doc.Add(*shown, t, text)
		}
	}
	d.displayed = next
	if next.isEmpty() {
		d.shownSince = nil
	} else {
		x := t
		d.shownSince = &x
	}
}

func (d *sccDecoder) finish(t time.Duration) {
	since := d.shownSince
	if since == nil {
		since = d.displayed.firstWrite
	}
	// Captions still on screen when the file ends stay for a while.
	if since != nil {
		end := *since + DefaultDuration
		if t > end {
			end = t
		}
		d.show(end, newSccMemory())
	}
}

func sccBasic(code byte) rune {
	switch code {
	case 0x2A:
		return 'á'
	case 0x5C:
		return 'é'
	case 0x5E:
		return 'í'
	case 0x5F:
		return 'ó'
	case 0x60:
		return 'ú'
	case 0x7B:
		return 'ç'
	case 0x7C:
		return '÷'
	case 0x7D:
		return 'Ñ'
	case 0x7E:
		return 'ñ'
	case 0x7F:
		return '█'
	}
	return rune(code)
}

// sccMemory is caption memory: rows of text (with italics as markup) by row number.
type sccMemory struct {
	rows       map[int]*strings.Builder
	row        int
	italic     bool
	firstWrite *time.Duration // when the first character was written
}

func newSccMemory() *sccMemory { return &sccMemory{rows: map[int]*strings.Builder{}, row: 15} }

func (m *sccMemory) sortedRows() []int {
	keys := make([]int, 0, len(m.rows))
	for k := range m.rows {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

func (m *sccMemory) isEmpty() bool {
	for _, r := range m.rows {
		if strings.TrimSpace(PlainText(r.String())) != "" {
			return false
		}
	}
	return true
}

func (m *sccMemory) text() string {
	var lines []string
	for _, k := range m.sortedRows() {
		s := m.rows[k].String()
		if strings.Contains(s, "<i>") {
			s += "</i>"
		}
		lines = append(lines, s)
	}
	return strings.Join(lines, "\n")
}

func (m *sccMemory) current() *strings.Builder {
	r, ok := m.rows[m.row]
	if !ok {
		r = &strings.Builder{}
		m.rows[m.row] = r
	}
	return r
}

func (m *sccMemory) moveTo(row int, italic bool) {
	if m.italic {
		m.current().WriteString("</i>")
	}
	m.row = row
	m.italic = false
	m.setItalic(italic)
}

func (m *sccMemory) setItalic(italic bool) {
	if italic != m.italic {
		if italic {
			m.current().WriteString("<i>")
		} else {
			m.current().WriteString("</i>")
		}
	}
	m.italic = italic
}

func (m *sccMemory) append(t time.Duration, s string) {
	if m.firstWrite == nil {
		x := t
		m.firstWrite = &x
	}
	m.current().WriteString(s)
}

func (m *sccMemory) backspace() {
	r := m.current()
	s := r.String()
	if s != "" && !strings.HasSuffix(s, ">") {
		rs := []rune(s)
		r.Reset()
		r.WriteString(string(rs[:len(rs)-1]))
	}
}

func (m *sccMemory) clearRow() { delete(m.rows, m.row) }

// rolledUp is a new memory with the last rows − 1 rows moved up and an empty base row.
func (m *sccMemory) rolledUp(rows int) *sccMemory {
	next := newSccMemory()
	var kept []string
	for _, k := range m.sortedRows() {
		if s := m.rows[k].String(); strings.TrimSpace(PlainText(s)) != "" {
			kept = append(kept, s)
		}
	}
	if len(kept) > rows-1 {
		kept = kept[len(kept)-(rows-1):]
	}
	for i, s := range kept {
		b := &strings.Builder{}
		b.WriteString(s)
		next.rows[15-len(kept)+i] = b
	}
	return next
}
