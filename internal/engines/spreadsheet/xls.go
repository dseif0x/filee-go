package spreadsheet

import (
	"bytes"
	"crypto/md5"
	"crypto/rc4"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"unicode/utf16"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// XLS (Excel 97–2003 BIFF8, also .xlt and the older BIFF5 of Excel 5/95)
// → Workbook: values formatted with the same number formats as XLSX, merged
// cells, column widths, row heights, hidden rows / columns / sheets and
// alignment. Like the original (which used ExcelDataReader) XLS cells keep
// the default font, fill and borders. Workbooks encrypted with the default
// "VelvetSweatshop" password (write protection) are decrypted (RC4).

var errXlsPassword = errors.New("the workbook is protected with a password (encrypted); remove the password in Excel and convert it again")

// ReadXLS reads every visible sheet of an XLS / XLT file.
func ReadXLS(path string) (*Workbook, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Web apps often save XLSX with an .xls name; the XLSX reader keeps more of such files.
	if bytes.HasPrefix(b, []byte("PK")) {
		return ReadXLSX(path)
	}
	stream := cfbStream(b, "Workbook", "Book")
	if stream == nil {
		if bytes.HasPrefix(b, []byte("<")) || bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF, '<'}) {
			return nil, errors.New("the file is not an Excel 97–2003 workbook but a web page or XML file saved with an .xls name; open it in Excel and save it as XLSX")
		}
		// BIFF2–4 files are a bare record stream.
		if len(b) >= 4 && (binary.LittleEndian.Uint16(b) == 0x0009 || binary.LittleEndian.Uint16(b) == 0x0209 || binary.LittleEndian.Uint16(b) == 0x0409) {
			return readBiff(b, true)
		}
		return nil, errors.New("the file is not an Excel 97–2003 workbook")
	}
	return readBiff(stream, false)
}

type biffRecord struct {
	id   uint16
	data []byte
	pos  int
}

func records(stream []byte, offset int) []biffRecord {
	var out []biffRecord
	le := binary.LittleEndian
	for p := offset; p+4 <= len(stream); {
		id, n := le.Uint16(stream[p:]), int(le.Uint16(stream[p+2:]))
		if p+4+n > len(stream) {
			break
		}
		out = append(out, biffRecord{id, stream[p+4 : p+4+n], p})
		p += 4 + n
		if id == 0x000A {
			break
		}
	}
	return out
}

type xlsSheet struct {
	name    string
	offset  int
	visible bool
	sheet   bool
}

type xlsReader struct {
	biff8     bool
	encoding  encoding.Encoding
	date1904  bool
	formats   map[int]string
	xfs       []xlsXF
	sst       []string
	formatter *NumberFormatter
}

type xlsXF struct {
	format int
	align  CellAlign
	valign CellVerticalAlign
}

func readBiff(stream []byte, bare bool) (*Workbook, error) {
	le := binary.LittleEndian
	r := &xlsReader{formats: map[int]string{}, encoding: charmap.Windows1252, formatter: NewFormatter(false)}
	if len(stream) < 8 {
		return nil, errors.New("the file is not an Excel 97–2003 workbook")
	}
	bof := le.Uint16(stream)
	if bof == 0x0809 {
		r.biff8 = le.Uint16(stream[4:]) == 0x0600
	} else if !bare {
		return nil, errors.New("the file is not an Excel 97–2003 workbook")
	}
	if bare {
		// BIFF2–4: one worksheet in the stream itself.
		sheet := NewWorksheet("Sheet1")
		r.globals(stream, records(stream, 0), nil)
		r.sheetRecords(sheet, records(stream, 0))
		return &Workbook{Sheets: []*Worksheet{sheet}}, nil
	}
	recs := records(stream, 0)
	for _, rec := range recs {
		if rec.id == 0x002F {
			if err := decrypt(stream, rec); err != nil {
				return nil, err
			}
			recs = records(stream, 0)
			break
		}
	}
	var sheets []xlsSheet
	r.globals(stream, recs, &sheets)
	layouts := xlsLayout(stream, sheets)
	book := &Workbook{}
	for i, s := range sheets {
		if !s.sheet || !s.visible {
			continue
		}
		name := s.name
		if name == "" {
			name = "Sheet"
		}
		sheet := NewWorksheet(name)
		r.sheetRecords(sheet, records(stream, s.offset))
		if l := layouts[i]; l != nil {
			l.apply(sheet)
		}
		book.Sheets = append(book.Sheets, sheet)
	}
	return book, nil
}

// decrypt undoes RC4 encryption made with the default password
// "VelvetSweatshop" in place; other passwords are refused.
func decrypt(stream []byte, filepass biffRecord) error {
	le := binary.LittleEndian
	d := filepass.data
	if len(d) < 2 || le.Uint16(d) != 1 || len(d) < 6+48 || le.Uint16(d[2:]) != 1 {
		return errXlsPassword // XOR obfuscation and CryptoAPI are not handled
	}
	salt, verifier, verifierHash := d[6:22], d[22:38], d[38:54]
	key := rc4Key("VelvetSweatshop", salt)
	block := func(n uint32) []byte {
		h := md5.New()
		h.Write(key[:5])
		var b [4]byte
		le.PutUint32(b[:], n)
		h.Write(b[:])
		return h.Sum(nil)
	}
	c, _ := rc4.NewCipher(block(0))
	v := make([]byte, 16)
	c.XORKeyStream(v, verifier)
	vh := make([]byte, 16)
	c.XORKeyStream(vh, verifierHash)
	sum := md5.Sum(v)
	if !bytes.Equal(sum[:], vh) {
		return errXlsPassword
	}
	// Record headers stay plain; record data is encrypted as one keystream per 1024-byte block of the stream.
	start := filepass.pos + 4 + len(filepass.data)
	// The keystream runs over the whole stream (headers included, which stay plain), restarting every 1024 bytes.
	var cipher *rc4.Cipher
	pos, cur := -1, -1
	keystream := func(offset int, buf []byte) {
		for i := range buf {
			at := offset + i
			if cipher == nil || at/1024 != cur || at < pos {
				cur = at / 1024
				cipher, _ = rc4.NewCipher(block(uint32(cur)))
				pos = cur * 1024
			}
			if at > pos {
				skip := make([]byte, at-pos)
				cipher.XORKeyStream(skip, skip)
			}
			one := buf[i : i+1]
			cipher.XORKeyStream(one, one)
			pos = at + 1
		}
	}
	for p := start; p+4 <= len(stream); {
		id, n := le.Uint16(stream[p:]), int(le.Uint16(stream[p+2:]))
		if p+4+n > len(stream) {
			break
		}
		data := stream[p+4 : p+4+n]
		switch id {
		case 0x0809, 0x002F, 0x00E1, 0x00E2:
			// BOF, FILEPASS, INTERFACEHDR, INTERFACEEND are never encrypted.
			keystream(p+4, make([]byte, n))
		case 0x0085:
			// BOUNDSHEET: its stream offset stays plain.
			if n >= 4 {
				keystream(p+4, make([]byte, 4))
				keystream(p+8, data[4:])
			}
		default:
			keystream(p+4, data)
		}
		p += 4 + n
	}
	// The filepass record must not be read again.
	le.PutUint16(stream[filepass.pos:], 0x0000)
	return nil
}

func rc4Key(password string, salt []byte) []byte {
	u := utf16.Encode([]rune(password))
	pw := make([]byte, 2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(pw[2*i:], c)
	}
	h := md5.Sum(pw)
	var buf []byte
	for i := 0; i < 16; i++ {
		buf = append(buf, h[:5]...)
		buf = append(buf, salt...)
	}
	k := md5.Sum(buf)
	return k[:]
}

func (r *xlsReader) globals(stream []byte, recs []biffRecord, sheets *[]xlsSheet) {
	le := binary.LittleEndian
	for i := 0; i < len(recs); i++ {
		rec := recs[i]
		d := rec.data
		switch rec.id {
		case 0x0042: // CODEPAGE
			if len(d) >= 2 {
				if e := codePageEncoding(int(le.Uint16(d))); e != nil {
					r.encoding = e
				}
			}
		case 0x0022: // DATEMODE
			if len(d) >= 2 && le.Uint16(d) == 1 {
				r.date1904 = true
			}
		case 0x041E, 0x001E: // FORMAT (BIFF4+), FORMAT (BIFF2-3)
			if rec.id == 0x041E && len(d) >= 3 {
				idx := int(le.Uint16(d))
				if r.biff8 {
					s, _ := r.unicodeString(d[2:], 2)
					r.formats[idx] = s
				} else {
					r.formats[idx] = r.byteString(d[2:], 1)
				}
			} else if rec.id == 0x001E && len(d) >= 1 {
				r.formats[len(r.formats)] = r.byteString(d, 1)
			}
		case 0x00E0, 0x0443, 0x0243, 0x0043: // XF
			r.xfs = append(r.xfs, r.xf(rec.id, d))
		case 0x0085: // BOUNDSHEET
			if sheets == nil || len(d) < 7 {
				continue
			}
			s := xlsSheet{offset: int(le.Uint32(d)), visible: d[4]&0x03 == 0, sheet: d[5] == 0}
			if r.biff8 {
				s.name, _ = r.unicodeString(d[6:], 1)
			} else {
				s.name = r.byteString(d[6:], 1)
			}
			*sheets = append(*sheets, s)
		case 0x00FC: // SST, with its CONTINUE records
			parts := [][]byte{d}
			for i+1 < len(recs) && recs[i+1].id == 0x003C {
				i++
				parts = append(parts, recs[i].data)
			}
			r.readSST(parts)
		}
	}
	r.formatter = NewFormatter(false)
}

func (r *xlsReader) xf(id uint16, d []byte) xlsXF {
	x := xlsXF{}
	var align byte
	switch {
	case id == 0x00E0 && len(d) >= 7: // BIFF5 / BIFF8
		x.format = int(binary.LittleEndian.Uint16(d[2:]))
		align = d[6]
	case id == 0x0443 && len(d) >= 6: // BIFF4
		x.format = int(d[1])
		align = d[4]
	case len(d) >= 4: // BIFF2–3
		x.format = int(d[2] & 0x3F)
		if len(d) > 3 {
			align = d[3] & 0x07
		}
	}
	switch align & 0x07 {
	case 1, 4, 5, 7: // left, filled, justified, distributed
		x.align = AlignLeft
	case 2, 6: // centred, centred across selection
		x.align = AlignCenter
	case 3:
		x.align = AlignRight
	}
	if id == 0x00E0 || id == 0x0443 {
		switch (align >> 4) & 0x07 {
		case 0:
			x.valign = VAlignTop
		case 1, 3, 4:
			x.valign = VAlignCenter
		}
	}
	return x
}

func codePageEncoding(cp int) encoding.Encoding {
	switch cp {
	case 1200:
		return nil // UTF-16: BIFF8 strings say so themselves
	case 367, 20127:
		return charmap.Windows1252
	case 437:
		return charmap.CodePage437
	case 850:
		return charmap.CodePage850
	case 852:
		return charmap.CodePage852
	case 866:
		return charmap.CodePage866
	case 874:
		return charmap.Windows874
	case 932:
		return japanese.ShiftJIS
	case 936:
		return simplifiedchinese.GBK
	case 949:
		return korean.EUCKR
	case 950:
		return traditionalchinese.Big5
	case 1250:
		return charmap.Windows1250
	case 1251:
		return charmap.Windows1251
	case 1252, 32769:
		return charmap.Windows1252
	case 1253:
		return charmap.Windows1253
	case 1254:
		return charmap.Windows1254
	case 1255:
		return charmap.Windows1255
	case 1256:
		return charmap.Windows1256
	case 1257:
		return charmap.Windows1257
	case 1258:
		return charmap.Windows1258
	case 10000, 32768:
		return charmap.Macintosh
	}
	return nil
}

func (r *xlsReader) ansi(b []byte) string {
	out, err := r.encoding.NewDecoder().Bytes(b)
	if err != nil {
		return string(b)
	}
	return string(out)
}

// byteString reads a BIFF5 string with a length of lenSize bytes.
func (r *xlsReader) byteString(d []byte, lenSize int) string {
	if len(d) < lenSize {
		return ""
	}
	n := int(d[0])
	if lenSize == 2 {
		n = int(binary.LittleEndian.Uint16(d))
	}
	d = d[lenSize:]
	return r.ansi(d[:min(n, len(d))])
}

// unicodeString reads a BIFF8 XLUnicodeString (length of lenSize bytes, flags,
// characters, rich text and phonetic data skipped); returns it and its size.
func (r *xlsReader) unicodeString(d []byte, lenSize int) (string, int) {
	if len(d) < lenSize+1 {
		return "", len(d)
	}
	le := binary.LittleEndian
	n := int(d[0])
	if lenSize == 2 {
		n = int(le.Uint16(d))
	}
	flags := d[lenSize]
	p := lenSize + 1
	runs, ext := 0, 0
	if flags&0x08 != 0 && p+2 <= len(d) {
		runs = int(le.Uint16(d[p:]))
		p += 2
	}
	if flags&0x04 != 0 && p+4 <= len(d) {
		ext = int(le.Uint32(d[p:]))
		p += 4
	}
	var s string
	if flags&0x01 != 0 {
		end := min(p+2*n, len(d))
		s = utf16le(d[p:end])
		p = end
	} else {
		end := min(p+n, len(d))
		s = latin1(d[p:end])
		p = end
	}
	return s, min(len(d), p+4*runs+ext)
}

func utf16le(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	return string(utf16.Decode(u))
}

func latin1(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

// readSST reads the shared strings; a string's characters may continue in
// the next CONTINUE record, which starts with a new flags byte.
func (r *xlsReader) readSST(parts [][]byte) {
	le := binary.LittleEndian
	if len(parts[0]) < 8 {
		return
	}
	count := int(le.Uint32(parts[0][4:]))
	part, p := 0, 8
	cur := parts[0]
	next := func() bool {
		part++
		if part >= len(parts) {
			return false
		}
		cur, p = parts[part], 0
		return true
	}
	for i := 0; i < count && i < 10_000_000; i++ {
		if p >= len(cur) && !next() {
			return
		}
		if p+3 > len(cur) {
			return
		}
		n := int(le.Uint16(cur[p:]))
		flags := cur[p+2]
		p += 3
		runs, ext := 0, 0
		if flags&0x08 != 0 {
			runs = int(le.Uint16(cur[p:]))
			p += 2
		}
		if flags&0x04 != 0 {
			ext = int(le.Uint32(cur[p:]))
			p += 4
		}
		wide := flags&0x01 != 0
		var u []uint16
		for len(u) < n {
			if p >= len(cur) {
				if !next() || len(cur) == 0 {
					break
				}
				wide = cur[0]&0x01 != 0
				p = 1
			}
			if wide {
				for len(u) < n && p+2 <= len(cur) {
					u = append(u, le.Uint16(cur[p:]))
					p += 2
				}
			} else {
				for len(u) < n && p < len(cur) {
					u = append(u, uint16(cur[p]))
					p++
				}
			}
		}
		r.sst = append(r.sst, string(utf16.Decode(u)))
		// Skip rich text runs and phonetic data, which may cross records too.
		skip := 4*runs + ext
		for skip > 0 {
			if p >= len(cur) && !next() {
				return
			}
			k := min(skip, len(cur)-p)
			p += k
			skip -= k
		}
	}
}

var xlsBuiltIns = map[int]string{
	5: `$#,##0_);($#,##0)`, 6: `$#,##0_);[Red]($#,##0)`, 7: `$#,##0.00_);($#,##0.00)`, 8: `$#,##0.00_);[Red]($#,##0.00)`,
	41: `_(* #,##0_);_(* \(#,##0\);_(* "-"_);_(@_)`, 42: `_("$"* #,##0_);_("$"* \(#,##0\);_("$"* "-"_);_(@_)`,
	43: `_(* #,##0.00_);_(* \(#,##0.00\);_(* "-"??_);_(@_)`, 44: `_("$"* #,##0.00_);_("$"* \(#,##0.00\);_("$"* "-"??_);_(@_)`,
}

func (r *xlsReader) formatOf(xf int) (string, xlsXF) {
	var x xlsXF
	if xf >= 0 && xf < len(r.xfs) {
		x = r.xfs[xf]
	}
	// Built-in formats the XLSX reader knows (dates in ISO order) look the same from XLS.
	if f, ok := BuiltInFormats[x.format]; ok {
		return f, x
	}
	if f, ok := r.formats[x.format]; ok {
		return f, x
	}
	if f, ok := xlsBuiltIns[x.format]; ok {
		return f, x
	}
	return "General", x
}

func (r *xlsReader) style(x xlsXF) CellStyle {
	s := DefaultStyle
	s.Align, s.VerticalAlign = x.align, x.valign
	return s
}

func (r *xlsReader) number(sheet *Worksheet, row, col, xf int, v float64) {
	format, x := r.formatOf(xf)
	value := v
	if r.date1904 && r.formatter.IsDate(format) {
		value += date1904Offset
	}
	nf := format
	if IsGeneral(nf) {
		nf = ""
	}
	sheet.Set(row, col, &Cell{Text: r.formatter.Format(value, format), Style: r.style(x), IsNumber: true, Value: fptr(value), NumberFormat: nf})
}

func (r *xlsReader) text(sheet *Worksheet, row, col, xf int, text string) {
	_, x := r.formatOf(xf)
	if c := TextCell(text, r.style(x)); c.HasContent() {
		sheet.Set(row, col, c)
	}
}

func rk(v uint32) float64 {
	var f float64
	if v&0x02 != 0 {
		f = float64(int32(v) >> 2)
	} else {
		f = math.Float64frombits(uint64(v&0xFFFFFFFC) << 32)
	}
	if v&0x01 != 0 {
		f /= 100
	}
	return f
}

func errorText(code byte) string {
	switch code {
	case 0x00:
		return "#NULL!"
	case 0x07:
		return "#DIV/0!"
	case 0x0F:
		return "#VALUE!"
	case 0x17:
		return "#REF!"
	case 0x1D:
		return "#NAME?"
	case 0x24:
		return "#NUM!"
	case 0x2A:
		return "#N/A"
	}
	return "#GETTING_DATA"
}

func (r *xlsReader) sheetRecords(sheet *Worksheet, recs []biffRecord) {
	le := binary.LittleEndian
	cell := func(d []byte) (int, int, int, bool) {
		if len(d) < 6 {
			return 0, 0, 0, false
		}
		return int(le.Uint16(d)), int(le.Uint16(d[2:])), int(le.Uint16(d[4:])), true
	}
	for i, rec := range recs {
		if i > 0 && rec.id == 0x0809 {
			break
		}
		d := rec.data
		switch rec.id {
		case 0x00FD: // LABELSST
			if row, col, xf, ok := cell(d); ok && len(d) >= 10 {
				if k := int(le.Uint32(d[6:])); k < len(r.sst) {
					r.text(sheet, row, col, xf, r.sst[k])
				}
			}
		case 0x0204, 0x00D6: // LABEL, RSTRING
			if row, col, xf, ok := cell(d); ok {
				var s string
				if r.biff8 {
					s, _ = r.unicodeString(d[6:], 2)
				} else {
					s = r.byteString(d[6:], 2)
				}
				r.text(sheet, row, col, xf, s)
			}
		case 0x0203: // NUMBER
			if row, col, xf, ok := cell(d); ok && len(d) >= 14 {
				r.number(sheet, row, col, xf, math.Float64frombits(le.Uint64(d[6:])))
			}
		case 0x027E: // RK
			if row, col, xf, ok := cell(d); ok && len(d) >= 10 {
				r.number(sheet, row, col, xf, rk(le.Uint32(d[6:])))
			}
		case 0x00BD: // MULRK
			if len(d) >= 6 {
				row, col := int(le.Uint16(d)), int(le.Uint16(d[2:]))
				for p := 4; p+6 <= len(d)-2; p += 6 {
					r.number(sheet, row, col, int(le.Uint16(d[p:])), rk(le.Uint32(d[p+2:])))
					col++
				}
			}
		case 0x0205: // BOOLERR
			if row, col, xf, ok := cell(d); ok && len(d) >= 8 {
				_, x := r.formatOf(xf)
				if d[7] != 0 {
					sheet.Set(row, col, TextCell(errorText(d[6]), r.style(x)))
				} else {
					v := float64(min(1, d[6]))
					sheet.Set(row, col, &Cell{Text: pick(v != 0, "TRUE", "FALSE"), Style: r.style(x), Value: fptr(v), IsBoolean: true})
				}
			}
		case 0x0006, 0x0406: // FORMULA: its cached result
			row, col, xf, ok := cell(d)
			if !ok || len(d) < 14 {
				continue
			}
			res := d[6:14]
			if le.Uint16(res[6:]) != 0xFFFF {
				r.number(sheet, row, col, xf, math.Float64frombits(le.Uint64(res)))
				continue
			}
			_, x := r.formatOf(xf)
			switch res[0] {
			case 0: // string: in the STRING record that follows
				for j := i + 1; j < len(recs) && j <= i+3; j++ {
					if recs[j].id == 0x0207 || recs[j].id == 0x0007 {
						var s string
						if r.biff8 {
							s, _ = r.unicodeString(recs[j].data, 2)
						} else {
							s = r.byteString(recs[j].data, 2)
						}
						r.text(sheet, row, col, xf, s)
						break
					}
				}
			case 1:
				v := float64(min(1, res[2]))
				sheet.Set(row, col, &Cell{Text: pick(v != 0, "TRUE", "FALSE"), Style: r.style(x), Value: fptr(v), IsBoolean: true})
			case 2:
				sheet.Set(row, col, TextCell(errorText(res[2]), r.style(x)))
			}
		case 0x00E5: // MERGECELLS
			if len(d) >= 2 {
				n := int(le.Uint16(d))
				for k := 0; k < n && 2+8*k+8 <= len(d); k++ {
					e := d[2+8*k:]
					m := CellRange{int(le.Uint16(e)), int(le.Uint16(e[4:])), int(le.Uint16(e[2:])), int(le.Uint16(e[6:]))}
					if m.Bottom > m.Top || m.Right > m.Left {
						sheet.Merges = append(sheet.Merges, m)
					}
				}
			}
		case 0x0004: // LABEL (BIFF2)
			if len(d) >= 8 {
				row, col := int(le.Uint16(d)), int(le.Uint16(d[2:]))
				r.text(sheet, row, col, 0, r.byteString(d[7:], 1))
			}
		case 0x0003: // NUMBER (BIFF2)
			if len(d) >= 15 {
				r.number(sheet, int(le.Uint16(d)), int(le.Uint16(d[2:])), int(d[4]&0x3F), math.Float64frombits(le.Uint64(d[7:])))
			}
		case 0x0002: // INTEGER (BIFF2)
			if len(d) >= 9 {
				r.number(sheet, int(le.Uint16(d)), int(le.Uint16(d[2:])), int(d[4]&0x3F), float64(le.Uint16(d[7:])))
			}
		}
	}
}

// ---- layout: column widths and row heights from the BIFF records ----

type xlsColumn struct {
	width  float64
	hidden bool
}

type xlsRow struct {
	points         float64
	custom, hidden bool
}

type xlsSheetLayout struct {
	columns map[int]xlsColumn
	rows    map[int]xlsRow
}

// xlsLayout reads the COLINFO width and the ROW "unsynced" flag Excel uses
// (ExcelDataReader reported only widths set by the user).
func xlsLayout(stream []byte, sheets []xlsSheet) map[int]*xlsSheetLayout {
	le := binary.LittleEndian
	out := map[int]*xlsSheetLayout{}
	for i, s := range sheets {
		if !s.sheet {
			continue
		}
		l := &xlsSheetLayout{columns: map[int]xlsColumn{}, rows: map[int]xlsRow{}}
		for k, rec := range records(stream, s.offset) {
			if k > 0 && rec.id == 0x0809 {
				break
			}
			d := rec.data
			switch {
			case rec.id == 0x007D && len(d) >= 10: // COLINFO
				first, last := int(le.Uint16(d)), min(int(le.Uint16(d[2:])), 16383)
				width := float64(le.Uint16(d[4:])) / 256
				hidden := d[8]&0x01 != 0 || width == 0
				for c := first; c <= last; c++ {
					l.columns[c] = xlsColumn{width, hidden}
				}
			case rec.id == 0x0208 && len(d) >= 14: // ROW
				height := float64(le.Uint16(d[6:])&0x7FFF) / 20
				flags := le.Uint16(d[12:])
				l.rows[int(le.Uint16(d))] = xlsRow{height, flags&0x40 != 0, flags&0x20 != 0}
			}
		}
		out[i] = l
	}
	return out
}

func (l *xlsSheetLayout) apply(sheet *Worksheet) {
	for c, col := range l.columns {
		if col.hidden {
			sheet.HiddenColumns[c] = true
		} else {
			sheet.ColumnWidths[c] = col.width
		}
	}
	for r, row := range l.rows {
		if row.hidden {
			sheet.HiddenRows[r] = true
		} else if row.custom {
			sheet.RowHeights[r] = row.points
		}
	}
}
