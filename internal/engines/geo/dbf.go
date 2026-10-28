package geo

// dBASE (.dbf), the attribute table of a Shapefile. Reading handles dBASE III
// / IV / FoxPro tables: text (C), numbers (N, F), true / false (L), dates (D),
// binary integers and doubles (I, B); memo fields read as empty. The text
// encoding comes from the .cpg file or the file's language driver byte;
// without either the text is UTF-8 if it is valid, else Korean CP949 (like the
// CSV reader), else Windows-1252. Records marked as deleted are returned as
// nil. Writing makes a dBASE III table in UTF-8 (the .cpg says so): text
// columns up to 254 bytes, numbers as N with the decimals they need, booleans
// as L. Field names are cut to 10 bytes and made unique, as the format requires.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	xunicode "golang.org/x/text/encoding/unicode"
	"golang.org/x/text/encoding/unicode/utf32"
)

type dbfField struct {
	name     string
	typ      byte
	length   int
	decimals int
}

type dbfTable struct {
	fields []dbfField
	// rows are the values of each record in field order; nil for a record marked as deleted.
	rows [][]any
}

// dbfLosses says what the writer had to change to fit the format, so the file can carry a notice.
type dbfLosses struct{ namesChanged, textCut, columnsDropped bool }

var koreanCodePageNames = map[string]bool{"CP949": true, "UHC": true, "MS949": true, "WINDOWS-949": true, "X-WINDOWS-949": true, "KS_C_5601-1987": true}

const (
	dbfMaxTextBytes   = 254
	dbfMaxFields      = 255
	dbfMaxNumberWidth = 24
)

// textEncoding decodes bytes; nil means UTF-8.
type textEncoding struct{ enc encoding.Encoding }

func (t textEncoding) decode(b []byte) string {
	if t.enc == nil {
		return strings.ToValidUTF8(string(b), "�")
	}
	s, err := t.enc.NewDecoder().Bytes(b)
	if err != nil {
		return strings.ToValidUTF8(string(b), "�")
	}
	return string(s)
}

// readDbf reads a table; cpg is the text of the .cpg file ("" for none). At
// most maxRows rows are read, and only their bytes.
func readDbf(ctx context.Context, path, cpg string, maxRows int) (*dbfTable, error) {
	data, err := dbfReadStart(path, maxRows)
	if err != nil {
		return nil, err
	}
	if len(data) < 33 {
		return nil, errors.New("The .dbf file is too short to be a dBASE table.")
	}
	recordCount := binary.LittleEndian.Uint32(data[4:])
	headerLength := int(binary.LittleEndian.Uint16(data[8:]))
	recordLength := int(binary.LittleEndian.Uint16(data[10:]))
	if headerLength < 33 || headerLength > len(data) || recordLength < 1 {
		return nil, errors.New("The .dbf header is damaged.")
	}
	t := &dbfTable{}
	var offsets []int
	position := 1
	nameAt := func(at int) []byte {
		end := at + 11
		if i := bytes.IndexByte(data[at:at+11], 0); i >= 0 {
			end = at + i
		}
		return data[at:end]
	}
	for at := 32; at+32 <= headerLength && data[at] != 0x0D; at += 32 {
		if len(t.fields) >= 2046 {
			return nil, errors.New("The .dbf has too many fields.")
		}
		f := dbfField{name: latin1(nameAt(at)), typ: data[at+11], length: int(data[at+16]), decimals: int(data[at+17])}
		t.fields = append(t.fields, f)
		offsets = append(offsets, position)
		position += f.length
	}
	if position > recordLength {
		return nil, errors.New("The .dbf field lengths do not fit the record length.")
	}
	rowLimit := uint32(max(maxRows, 0))
	enc, ok := resolveDbfEncoding(cpg, data[29])
	if !ok {
		enc = detectDbfEncoding(data, headerLength, recordLength, t.fields, offsets, min(recordCount, rowLimit))
	}
	// Field names were read as Latin-1 above; re-read them in the table's encoding (they are ASCII almost always).
	for i := range t.fields {
		name := strings.TrimSpace(enc.decode(nameAt(32 + 32*i)))
		if name == "" {
			name = fmt.Sprintf("FIELD%d", i+1)
		}
		t.fields[i].name = name
	}
	available := (len(data) - headerLength) / recordLength
	count := int(min(recordCount, uint32(available), rowLimit))
	for r := 0; r < count; r++ {
		if r&0x3FF == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		start := headerLength + r*recordLength
		if data[start] == '*' {
			t.rows = append(t.rows, nil)
			continue
		}
		values := make([]any, len(t.fields))
		for f, field := range t.fields {
			values[f] = dbfValue(field, data[start+offsets[f]:start+offsets[f]+field.length], enc)
		}
		t.rows = append(t.rows, values)
	}
	return t, nil
}

// dbfReadStart reads the header and the first maxRows records of the file (all of it if it is shorter).
func dbfReadStart(path string, maxRows int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	head := make([]byte, 12)
	n, _ := io.ReadFull(f, head)
	if n < 12 {
		return head[:n], nil
	}
	headerLength := int64(binary.LittleEndian.Uint16(head[8:]))
	recordLength := int64(binary.LittleEndian.Uint16(head[10:]))
	// The records, and the end marker after them; at least the fixed header,
	// so a header that claims to be shorter is reported as damaged.
	wanted := max(headerLength+int64(max(maxRows, 0))*recordLength+1, 33)
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	data := make([]byte, min(st.Size(), wanted))
	if _, err := f.ReadAt(data, 0); err != nil && err != io.EOF {
		return nil, err
	}
	return data, nil
}

func latin1(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

func dbfValue(field dbfField, cell []byte, enc textEncoding) any {
	switch unicode.ToUpper(rune(field.typ)) {
	case 'C':
		end := len(cell)
		for end > 0 && (cell[end-1] == ' ' || cell[end-1] == 0) {
			end--
		}
		if end == 0 {
			return ""
		}
		return enc.decode(cell[:end])
	case 'N', 'F':
		text := strings.TrimSpace(latin1(cell))
		if text == "" || text[0] == '*' {
			return nil
		}
		if field.decimals == 0 {
			if v, err := strconv.ParseInt(text, 10, 64); err == nil {
				return v
			}
		}
		v, ok := xmlNumber(text)
		if !ok {
			return nil
		}
		if v == math.Floor(v) && math.Abs(v) < 1e15 && field.decimals == 0 {
			return int64(v)
		}
		return v
	case 'L':
		if len(cell) == 0 {
			return nil
		}
		switch cell[0] {
		case 'T', 't', 'Y', 'y':
			return true
		case 'F', 'f', 'N', 'n':
			return false
		}
		return nil
	case 'D':
		text := strings.TrimSpace(latin1(cell))
		if len(text) == 8 {
			if d, err := time.Parse("20060102", text); err == nil {
				return d.Format("2006-01-02")
			}
		}
		return nil
	case 'I', '+':
		if len(cell) >= 4 {
			return int64(int32(binary.LittleEndian.Uint32(cell)))
		}
	case 'B', 'O':
		if len(cell) >= 8 {
			v := math.Float64frombits(binary.LittleEndian.Uint64(cell))
			if isFinite(v) {
				return v
			}
		}
	}
	// Memo, date-time and other types that point elsewhere or need extra files.
	return nil
}

func codePageEncoding(page int) (encoding.Encoding, bool) {
	switch page {
	case 65001:
		return nil, true
	case 437:
		return charmap.CodePage437, true
	case 850:
		return charmap.CodePage850, true
	case 852:
		return charmap.CodePage852, true
	case 855:
		return charmap.CodePage855, true
	case 858:
		return charmap.CodePage858, true
	case 860:
		return charmap.CodePage860, true
	case 862:
		return charmap.CodePage862, true
	case 863:
		return charmap.CodePage863, true
	case 865:
		return charmap.CodePage865, true
	case 866:
		return charmap.CodePage866, true
	case 874:
		return charmap.Windows874, true
	case 932:
		return japanese.ShiftJIS, true
	case 936:
		return simplifiedchinese.GBK, true
	case 949:
		return korean.EUCKR, true
	case 950:
		return traditionalchinese.Big5, true
	case 1200:
		return xunicode.UTF16(xunicode.LittleEndian, xunicode.IgnoreBOM), true
	case 1201:
		return xunicode.UTF16(xunicode.BigEndian, xunicode.IgnoreBOM), true
	case 12000:
		return utf32.UTF32(utf32.LittleEndian, utf32.IgnoreBOM), true
	case 1250:
		return charmap.Windows1250, true
	case 1251:
		return charmap.Windows1251, true
	case 1252:
		return charmap.Windows1252, true
	case 1253:
		return charmap.Windows1253, true
	case 1254:
		return charmap.Windows1254, true
	case 1255:
		return charmap.Windows1255, true
	case 1256:
		return charmap.Windows1256, true
	case 1257:
		return charmap.Windows1257, true
	case 1258:
		return charmap.Windows1258, true
	case 20866:
		return charmap.KOI8R, true
	case 21866:
		return charmap.KOI8U, true
	case 28591:
		return charmap.ISO8859_1, true
	case 28592:
		return charmap.ISO8859_2, true
	case 28595:
		return charmap.ISO8859_5, true
	case 28597:
		return charmap.ISO8859_7, true
	case 28605:
		return charmap.ISO8859_15, true
	}
	return nil, false
}

// languageDriverCodePage is the dBASE "language driver ID" in the header.
var languageDriverCodePage = map[byte]int{
	0x01: 437, 0x02: 850, 0x03: 1252, 0x57: 1252, 0x58: 1252, 0x59: 1252, 0x08: 865, 0x09: 437, 0x0A: 850, 0x0B: 437,
	0x0D: 437, 0x0E: 850, 0x0F: 437, 0x10: 850, 0x11: 437, 0x12: 850, 0x13: 932, 0x14: 850, 0x15: 437, 0x16: 850,
	0x17: 865, 0x18: 437, 0x19: 437, 0x1A: 850, 0x1B: 437, 0x1C: 863, 0x1D: 850, 0x1F: 852, 0x22: 852, 0x23: 852,
	0x24: 860, 0x25: 850, 0x26: 866, 0x37: 850, 0x40: 852, 0x4D: 936, 0x4E: 949, 0x4F: 950, 0x50: 874, 0x64: 852,
	0x65: 866, 0x66: 865, 0x67: 861, 0x6A: 737, 0x6B: 857, 0x78: 950, 0x79: 949, 0x7A: 936, 0x7B: 932, 0x7C: 874,
	0x7D: 1255, 0x7E: 1256, 0xC8: 1250, 0xC9: 1251, 0xCA: 1254, 0xCB: 1253, 0xCC: 1257,
}

func resolveDbfEncoding(cpg string, languageDriver byte) (textEncoding, bool) {
	if strings.TrimSpace(cpg) != "" {
		name := strings.Trim(strings.TrimSpace(cpg), "\x00")
		upper := strings.ToUpper(name)
		switch {
		case upper == "UTF-8" || upper == "UTF8" || name == "65001":
			return textEncoding{}, true
		case koreanCodePageNames[upper]:
			return textEncoding{korean.EUCKR}, true
		}
		if page, err := strconv.Atoi(name); err == nil {
			if e, ok := codePageEncoding(page); ok {
				return textEncoding{e}, true
			}
		} else if strings.HasPrefix(upper, "ANSI ") {
			if page, err := strconv.Atoi(name[5:]); err == nil {
				if e, ok := codePageEncoding(page); ok {
					return textEncoding{e}, true
				}
			}
		} else {
			label := strings.ReplaceAll(upper, "LATIN1", "ISO-8859-1")
			if e, err := ianaindex.IANA.Encoding(label); err == nil && e != nil {
				return textEncoding{e}, true
			}
			if e, err := htmlindex.Get(label); err == nil {
				return textEncoding{e}, true
			}
		}
		// An encoding name this system doesn't know: fall back to the language driver byte, then to detection.
	}
	if page, ok := languageDriverCodePage[languageDriver]; ok {
		if e, ok := codePageEncoding(page); ok {
			return textEncoding{e}, true
		}
	}
	return textEncoding{}, false
}

// detectDbfEncoding: UTF-8 when the text of the first records is valid UTF-8, else CP949 if valid, else Windows-1252.
func detectDbfEncoding(data []byte, headerLength, recordLength int, fields []dbfField, offsets []int, recordCount uint32) textEncoding {
	var sample []byte
	count := int(min(recordCount, uint32((len(data)-headerLength)/recordLength)))
	for r := 0; r < count && len(sample) < 1<<20; r++ {
		start := headerLength + r*recordLength
		for f, field := range fields {
			if unicode.ToUpper(rune(field.typ)) == 'C' {
				sample = append(sample, data[start+offsets[f]:start+offsets[f]+field.length]...)
			}
		}
	}
	if utf8.Valid(sample) {
		return textEncoding{}
	}
	if validCP949(sample) {
		return textEncoding{korean.EUCKR}
	}
	return textEncoding{charmap.Windows1252}
}

// validCP949 checks the bytes are well-formed CP949 (Unified Hangul Code).
func validCP949(b []byte) bool {
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c < 0x80 {
			continue
		}
		if c < 0x81 || c > 0xFE || i+1 >= len(b) {
			return false
		}
		t := b[i+1]
		if !(t >= 0x41 && t <= 0x5A || t >= 0x61 && t <= 0x7A || t >= 0x81 && t <= 0xFE) {
			return false
		}
		i++
	}
	// x/text replaces what it can't map: a strict decoder would have failed there.
	out, err := korean.EUCKR.NewDecoder().Bytes(b)
	return err == nil && !bytes.ContainsRune(out, utf8.RuneError)
}

type dbfLayout struct {
	name     string
	typ      byte
	length   int
	decimals int
	source   string
	fid      bool
}

// writeDbf writes the attributes of features as a dBASE III table in UTF-8. A
// table needs at least one field, so features without attributes get a record
// number field "FID".
func writeDbf(ctx context.Context, path string, features []*Feature) (dbfLosses, error) {
	var columns []string
	seen := map[string]bool{}
	for _, f := range features {
		for _, p := range f.Properties {
			if !seen[p.Key] {
				seen[p.Key] = true
				columns = append(columns, p.Key)
			}
		}
	}
	var losses dbfLosses
	if len(columns) > dbfMaxFields {
		losses.columnsDropped = true
		columns = columns[:dbfMaxFields]
	}
	rows := make([]map[string]any, len(features))
	for i, f := range features {
		m := map[string]any{}
		for _, p := range f.Properties {
			if _, ok := m[p.Key]; !ok {
				m[p.Key] = p.Value
			}
		}
		rows[i] = m
	}
	used := map[string]bool{}
	var layouts []dbfLayout
	if len(columns) == 0 {
		layouts = append(layouts, dbfLayout{name: "FID", typ: 'N', length: 11, source: "FID", fid: true})
		used["FID"] = true
	}
	for _, c := range columns {
		name := dbfFieldName(c, used)
		losses.namesChanged = losses.namesChanged || name != c
		layouts = append(layouts, describeDbfColumn(name, c, rows))
	}
	recordLength := 1
	for _, l := range layouts {
		recordLength += l.length
	}
	headerLength := 32 + 32*len(layouts) + 1
	if recordLength > math.MaxUint16 || headerLength > math.MaxUint16 {
		return losses, errors.New("The attribute table is too wide for a dBASE file.")
	}
	f, err := os.Create(path)
	if err != nil {
		return losses, err
	}
	w := bufio.NewWriterSize(f, 1<<16)
	header := make([]byte, 32)
	header[0] = 0x03
	today := time.Now().UTC()
	header[1], header[2], header[3] = byte(today.Year()-1900), byte(today.Month()), byte(today.Day())
	binary.LittleEndian.PutUint32(header[4:], uint32(len(rows)))
	binary.LittleEndian.PutUint16(header[8:], uint16(headerLength))
	binary.LittleEndian.PutUint16(header[10:], uint16(recordLength))
	w.Write(header)
	for _, l := range layouts {
		d := make([]byte, 32)
		copy(d[:11], l.name)
		d[11] = l.typ
		d[16] = byte(l.length)
		d[17] = byte(l.decimals)
		w.Write(d)
	}
	w.WriteByte(0x0D)
	record := make([]byte, recordLength)
	for r := range rows {
		if r&0x3FF == 0 {
			if err := ctx.Err(); err != nil {
				f.Close()
				return losses, err
			}
		}
		for i := range record {
			record[i] = ' '
		}
		at := 1
		for _, l := range layouts {
			var v any
			if l.fid {
				v = int64(r)
			} else {
				v = rows[r][l.source]
			}
			if putDbfCell(record[at:at+l.length], l, v) {
				losses.textCut = true
			}
			at += l.length
		}
		w.Write(record)
	}
	w.WriteByte(0x1A)
	err = w.Flush()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return losses, err
}

func fixed(v float64, decimals int) string { return strconv.FormatFloat(v, 'f', decimals, 64) }

func describeDbfColumn(name, source string, rows []map[string]any) dbfLayout {
	strs, bools, longs, doubles := false, false, false, false
	textBytes, longWidth, decimals := 1, 1, 0
	for _, row := range rows {
		v, ok := row[source]
		if !ok || v == nil {
			continue
		}
		switch x := v.(type) {
		case bool:
			bools = true
		case int64:
			longs = true
			longWidth = max(longWidth, len(strconv.FormatInt(x, 10)))
		case float64:
			if isFinite(x) {
				doubles = true
				text := number(x)
				if dot := strings.IndexByte(text, '.'); dot >= 0 {
					decimals = max(decimals, len(text)-dot-1)
				}
			}
		default:
			strs = true
		}
		if strs {
			break
		}
	}
	if !strs && !(bools && (longs || doubles)) {
		switch {
		case bools:
			return dbfLayout{name: name, typ: 'L', length: 1, source: source}
		case doubles:
			// Fixed decimals, as many as the values need (15 at most), in a width of 24 at most.
			decimals = min(decimals, 15)
			width := 1
			for _, row := range rows {
				v, ok := row[source]
				if !ok || v == nil {
					continue
				}
				var real float64
				switch x := v.(type) {
				case int64:
					real = float64(x)
				case float64:
					real = x
				default:
					continue
				}
				if !isFinite(real) {
					continue
				}
				width = max(width, len(fixed(real, decimals)))
			}
			for width > dbfMaxNumberWidth && decimals > 0 {
				decimals--
				width--
			}
			if width <= dbfMaxNumberWidth {
				return dbfLayout{name: name, typ: 'N', length: max(width, 1), decimals: decimals, source: source}
			}
			strs = true
		case longs && longWidth <= 18:
			return dbfLayout{name: name, typ: 'N', length: longWidth, source: source}
		case !longs:
			// Only empty values: a text column of one blank.
			return dbfLayout{name: name, typ: 'C', length: 1, source: source}
		default:
			strs = true
		}
	}
	for _, row := range rows {
		if v, ok := row[source]; ok && v != nil {
			textBytes = max(textBytes, len(toText(v)))
		}
	}
	return dbfLayout{name: name, typ: 'C', length: min(textBytes, dbfMaxTextBytes), source: source}
}

// putDbfCell writes one value into its cell; it reports whether text had to be cut to the field width.
func putDbfCell(cell []byte, l dbfLayout, v any) bool {
	switch l.typ {
	case 'L':
		switch v {
		case true:
			cell[0] = 'T'
		case false:
			cell[0] = 'F'
		default:
			cell[0] = '?'
		}
		return false
	case 'N':
		var text string
		switch x := v.(type) {
		case int64:
			if l.decimals == 0 {
				text = strconv.FormatInt(x, 10)
			} else {
				text = fixed(float64(x), l.decimals)
			}
		case float64:
			if !isFinite(x) {
				return false
			}
			text = fixed(x, l.decimals)
		default:
			return false
		}
		if len(text) > len(cell) {
			text = text[:len(cell)]
		}
		copy(cell[len(cell)-len(text):], text)
		return false
	}
	b := []byte(toText(v))
	n := len(b)
	cut := n > len(cell)
	if cut {
		n = len(cell)
		// Don't end in the middle of a character.
		for n > 0 && b[n]&0xC0 == 0x80 {
			n--
		}
	}
	copy(cell, b[:n])
	return cut
}

// dbfFieldName is a field name of at most ten bytes of letters, digits and underscores that is not used yet.
func dbfFieldName(source string, used map[string]bool) string {
	var b strings.Builder
	for _, r := range source {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	name := b.String()
	if name == "" {
		name = "FIELD"
	}
	candidate := cutUTF8(name, 10)
	for n := 1; used[strings.ToUpper(candidate)]; n++ {
		suffix := "_" + strconv.Itoa(n)
		candidate = cutUTF8(name, 10-len(suffix)) + suffix
	}
	used[strings.ToUpper(candidate)] = true
	return candidate
}

// cutUTF8 is the start of s that fits in n bytes of UTF-8.
func cutUTF8(s string, n int) string {
	size := 0
	for i, r := range s {
		size += utf8.RuneLen(r)
		if size > n {
			return s[:i]
		}
	}
	return s
}
