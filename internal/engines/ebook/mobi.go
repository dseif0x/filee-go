package ebook

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"

	"github.com/dseif0x/filee-go/internal/doc"
)

// MOBI / AZW / AZW3 / PRC → Book, and the PDF inside AZW4 (Print Replica).
// Written from the MOBI format description on the MobileRead wiki:
//   - PalmDB records; record 0 holds the PalmDOC header, the MOBI header and
//     EXTH metadata (title, authors, cover);
//   - text records are PalmDOC (LZ77) or HUFF/CDIC compressed, with trailing
//     entries stripped first;
//   - MOBI 6 (KF7): one HTML document; filepos links become anchors,
//     recindex pictures come from image records;
//   - KF8 (AZW3, also the KF8 half of joint MOBI files): the text is cut into
//     files by the skeleton (SKEL) and fragment (FRAG) indexes; kindle:pos,
//     kindle:embed and kindle:flow references are resolved;
//   - plain PalmDOC books (TEXtREAd) are text.
// Books with DRM are refused; KFX and Topaz books are recognised and refused too.

func readMobiBook(ctx context.Context, path, work string) (*Book, error) {
	db, err := openMobi(path)
	if err != nil {
		return nil, err
	}
	folder, err := newFolder(work, "mobi")
	if err != nil {
		return nil, err
	}
	if db.typeCreator == "TEXtREAd" {
		return &Book{textToDocument(decodeMobiText(palmDocText(db))), &Metadata{Title: db.name}}, nil
	}
	header, err := parseMobiHeader(db, 0)
	if err != nil {
		return nil, err
	}
	if header.encryption != 0 {
		return nil, errDRM
	}
	meta := header.metadata(db.name)
	var d *doc.Document
	var res *mobiResources
	kf8Start := header.kf8Boundary
	if header.version >= 8 {
		kf8Start = 0
	}
	if kf8Start >= 0 && kf8Start < db.count() {
		kf8 := header
		if kf8Start != 0 {
			if kf8, err = parseMobiHeader(db, kf8Start); err != nil {
				return nil, err
			}
		}
		if kf8.encryption != 0 {
			return nil, errDRM
		}
		// Joint files keep the pictures once, before the KF8 half, numbered from the MOBI 6 header.
		first := header.firstImage
		if kf8Start == 0 {
			first = kf8.firstImage
		}
		res = &mobiResources{db: db, first: first, folder: folder, saved: map[int]string{}}
		d, err = readKf8(ctx, db, kf8, kf8Start, res, folder)
	} else {
		res = &mobiResources{db: db, first: header.firstImage, folder: folder, saved: map[int]string{}}
		d, err = readMobi6(ctx, db, header, res, folder)
	}
	if err != nil {
		return nil, err
	}
	if header.coverOffset >= 0 {
		if f := res.save(header.coverOffset); f != "" {
			meta.CoverImage = f
		}
	}
	d.Title = meta.Title
	return &Book{d, meta}, nil
}

// printReplicaPdf is the first PDF of an AZW4 (Print Replica) book.
func printReplicaPdf(path string) ([]byte, error) {
	db, err := openMobi(path)
	if err != nil {
		return nil, err
	}
	header, err := parseMobiHeader(db, 0)
	if err != nil {
		return nil, err
	}
	if header.encryption != 0 {
		return nil, errDRM
	}
	text, err := mobiText(db, header, 0)
	if err != nil {
		return nil, err
	}
	if bytes.HasPrefix(text, []byte("%MOP")) && len(text) >= 12 {
		// "%MOP", table count, section count per table, then (offset, length) per section; the first is the PDF.
		tables := binary.BigEndian.Uint32(text[4:])
		index := 8 + 4*int(min(tables, 1000))
		off, length := int(u32(text, index)), int(u32(text, index+4))
		if off > 0 && length > 0 && off+length <= len(text) {
			return text[off : off+length], nil
		}
	}
	at := bytes.Index(text, []byte("%PDF-"))
	if at < 0 {
		return nil, errors.New("this book is not a Print Replica (AZW4) book: it contains no PDF")
	}
	return text[at:], nil
}

func openMobi(path string) (*palmDatabase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if bytes.HasPrefix(data, []byte("CONT")) || bytes.HasPrefix(data, []byte("\xEADRMION")) {
		return nil, errors.New("this is a KFX book (Kindle's newest format), which cannot be converted. Download it from Amazon as an AZW3 or MOBI file (\"Download & transfer via USB\")")
	}
	if bytes.HasPrefix(data, []byte("TPZ")) {
		return nil, errors.New("this is a Topaz book, which cannot be converted")
	}
	db, err := openPalm(data)
	if err != nil {
		return nil, err
	}
	if db.typeCreator != "BOOKMOBI" && db.typeCreator != "TEXtREAd" {
		return nil, fmt.Errorf("this is not a Mobipocket or Kindle book (type %q)", db.typeCreator)
	}
	return db, nil
}

// mobiText is the decompressed text of the book whose header is at record start.
func mobiText(db *palmDatabase, h *mobiHeader, start int) ([]byte, error) {
	var decompress func([]byte) ([]byte, error)
	switch h.compression {
	case 1:
		decompress = func(b []byte) ([]byte, error) { return b, nil }
	case 2:
		decompress = func(b []byte) ([]byte, error) { return palmDocDecompress(b), nil }
	case 17480:
		huff := start + h.huffRecord
		var cdics [][]byte
		for i := huff + 1; i < huff+h.huffCount; i++ {
			cdics = append(cdics, db.record(i))
		}
		hc, err := newHuffCdic(db.record(huff), cdics)
		if err != nil {
			return nil, err
		}
		decompress = hc.decompress
	default:
		return nil, fmt.Errorf("the book uses an unknown compression (%d)", h.compression)
	}
	var text bytes.Buffer
	for i := 1; i <= h.textRecords; i++ {
		rec := db.record(start + i)
		size := len(rec) - trailingSize(rec, h.extraFlags)
		b, err := decompress(rec[:max(0, size)])
		if err != nil {
			return nil, err
		}
		text.Write(b)
	}
	out := text.Bytes()
	if h.textLength > 0 && h.textLength < int64(len(out)) {
		out = out[:h.textLength]
	}
	return out, nil
}

// trailingSize is the size of the extra data at the end of a text record
// (multibyte overlap and indexing entries), described by the header's extra
// flags: each set bit above bit 0 adds an entry whose size is a backward varint.
func trailingSize(rec []byte, flags int) int {
	size := 0
	for b := flags >> 1; b != 0; b >>= 1 {
		if b&1 == 0 {
			continue
		}
		value, shift := 0, 0
		for p := len(rec) - size - 1; p >= 0; p-- {
			c := rec[p]
			value |= int(c&0x7F) << shift
			shift += 7
			if c&0x80 != 0 || shift >= 28 || p == 0 {
				break
			}
		}
		size += value
	}
	if flags&1 != 0 && len(rec)-size-1 >= 0 {
		size += int(rec[len(rec)-size-1]&0x3) + 1
	}
	return max(0, min(size, len(rec)))
}

func palmDocText(db *palmDatabase) []byte {
	h := db.record(0)
	compression, records := u16(h, 0), u16(h, 8)
	var text bytes.Buffer
	for i := 1; i <= records && i < db.count(); i++ {
		if compression == 2 {
			text.Write(palmDocDecompress(db.record(i)))
		} else {
			text.Write(db.record(i))
		}
	}
	return text.Bytes()
}

// decodeMobiText is UTF-8 when valid, else Windows-1252 (the Mobipocket default).
func decodeMobiText(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	s, _ := charmap.Windows1252.NewDecoder().Bytes(b)
	return string(s)
}

func codePageEncoding(cp int) encoding.Encoding {
	switch cp {
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
	case 1253:
		return charmap.Windows1253
	case 1254:
		return charmap.Windows1254
	}
	return charmap.Windows1252
}

// ---- MOBI 6 ----

// Digit counts are capped so the numbers always fit (filepos values are 10 digits, recindex 5).
var (
	filePosRe  = regexp.MustCompile(`(?i)\bfilepos\s*=\s*['"]?0*(\d{1,12})['"]?`)
	recIndexRe = regexp.MustCompile(`(?i)\b(?:hi|lo)?recindex\s*=\s*['"]?(\d{1,9})['"]?`)
)

func readMobi6(ctx context.Context, db *palmDatabase, h *mobiHeader, res *mobiResources, folder string) (*doc.Document, error) {
	text, err := mobiText(db, h, 0)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Byte offsets: filepos values index the text directly.
	set := map[int]bool{}
	for _, m := range filePosRe.FindAllSubmatch(text, -1) {
		if p, err := strconv.Atoi(string(m[1])); err == nil && p <= len(text) {
			set[p] = true
		}
	}
	positions := make([]int, 0, len(set))
	for p := range set {
		positions = append(positions, p)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(positions)))
	out := append([]byte(nil), text...)
	for _, p := range positions {
		// A position inside a tag moves to the start of that tag.
		at := p
		if at > 0 {
			open := bytes.LastIndexByte(text[:at], '<')
			closeAt := bytes.LastIndexByte(text[:at], '>')
			if open > closeAt {
				at = open
			}
		}
		anchor := []byte(`<a id="filepos` + strconv.Itoa(p) + `"></a>`)
		out = append(out[:at], append(anchor, out[at:]...)...)
	}
	out = filePosRe.ReplaceAllFunc(out, func(m []byte) []byte {
		n, _ := strconv.Atoi(string(filePosRe.FindSubmatch(m)[1]))
		return []byte(`href="#filepos` + strconv.Itoa(n) + `"`)
	})
	out = recIndexRe.ReplaceAllFunc(out, func(m []byte) []byte {
		n, _ := strconv.Atoi(string(recIndexRe.FindSubmatch(m)[1]))
		if f := res.save(n - 1); f != "" {
			return []byte(`src="` + filepath.Base(f) + `"`)
		}
		return nil
	})
	var html []byte
	if h.codepage == 65001 {
		html = out
	} else {
		html, _ = codePageEncoding(h.codepage).NewDecoder().Bytes(out)
	}
	page := filepath.Join(folder, "book.html")
	if err := os.WriteFile(page, append([]byte{0xEF, 0xBB, 0xBF}, html...), 0o644); err != nil {
		return nil, err
	}
	return readChapters(ctx, []string{page}, filepath.Join(folder, "~media"))
}

// ---- KF8 ----

var (
	kindlePosRe   = regexp.MustCompile(`kindle:pos:fid:([0-9A-Va-v]{4}):off:([0-9A-Va-v]{10})`)
	kindleEmbedRe = regexp.MustCompile(`kindle:embed:([0-9A-Va-v]{4})(?:\?mime=[^'"\)\s]*)?`)
	kindleFlowRe  = regexp.MustCompile(`kindle:flow:([0-9A-Va-v]{4})(?:\?mime=([^'"\)\s]*))?`)
	aidRe         = regexp.MustCompile(`\said\s*=\s*['"]([^'"]+)['"]`)
	idAttrRe      = regexp.MustCompile(`(?i)^<[^>]*\s(?:id|name)\s*=\s*['"]([^'"]*)['"]`)
	aidAttrRe     = regexp.MustCompile(`(?i)^<[^>]+\said\s*=\s*['"]([^'"]+)['"]`)
	selectorAidRe = regexp.MustCompile(`@aid='([^']+)'`)
)

type kf8Part struct {
	text  []byte
	start int64
}

func readKf8(ctx context.Context, db *palmDatabase, h *mobiHeader, start int, res *mobiResources, folder string) (*doc.Document, error) {
	text, err := mobiText(db, h, start)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Flows: flow 0 is the XHTML text, the others are style sheets and SVG images.
	var flows [][]byte
	if h.fdst >= 0 {
		fdst := db.record(start + int(h.fdst))
		if len(fdst) >= 12 && string(fdst[:4]) == "FDST" {
			table := int(u32(fdst, 4))
			count := int(u32(fdst, 8))
			for i := 0; i < count; i++ {
				from := int(max(0, min(u32(fdst, table+i*8), int64(len(text)))))
				to := int(max(int64(from), min(u32(fdst, table+i*8+4), int64(len(text)))))
				flows = append(flows, text[from:to])
			}
		}
	}
	if len(flows) == 0 {
		flows = append(flows, text)
	}
	markup := flows[0]
	strs := map[int64]string{}
	var skeletons, fragments []indexEntry
	if h.skeleton >= 0 {
		if skeletons, err = readIndex(db, start+int(h.skeleton), strs); err != nil {
			return nil, err
		}
	}
	if h.fragment >= 0 {
		if fragments, err = readIndex(db, start+int(h.fragment), strs); err != nil {
			return nil, err
		}
	}
	parts := assembleKf8(markup, skeletons, fragments, strs)
	insertPositions := make([]int64, len(fragments))
	for i, f := range fragments {
		insertPositions[i], _ = strconv.ParseInt(f.key, 10, 64)
	}
	linkedAids := map[string]bool{}
	resolve := func(m [][]byte) string {
		fragment := int(base32(string(m[1])))
		var pos int64
		if fragment < len(insertPositions) {
			pos = insertPositions[fragment]
		}
		pos += base32(string(m[2]))
		part := -1
		for i, p := range parts {
			if pos >= p.start && pos < p.start+int64(len(p.text)) {
				part = i
				break
			}
		}
		if part < 0 {
			return partName(0)
		}
		if id := idBefore(parts[part].text, int(pos-parts[part].start), linkedAids); id != "" {
			return partName(part) + "#" + id
		}
		return partName(part)
	}
	texts := make([][]byte, len(parts))
	for i, p := range parts {
		x := kindlePosRe.ReplaceAllFunc(p.text, func(m []byte) []byte { return []byte(resolve(kindlePosRe.FindSubmatch(m))) })
		x = kindleEmbedRe.ReplaceAllFunc(x, func(m []byte) []byte {
			if f := res.save(int(base32(string(kindleEmbedRe.FindSubmatch(m)[1]))) - 1); f != "" {
				return []byte(filepath.Base(f))
			}
			return []byte("missing")
		})
		x = kindleFlowRe.ReplaceAllFunc(x, func(m []byte) []byte {
			sm := kindleFlowRe.FindSubmatch(m)
			return []byte(kf8Flow(flows, int(base32(string(sm[1]))), string(sm[2]), folder))
		})
		texts[i] = x
	}
	var files []string
	for i, x := range texts {
		// Links that point at an element by its Amazon "aid" get an id to land on.
		if len(linkedAids) > 0 {
			x = aidRe.ReplaceAllFunc(x, func(m []byte) []byte {
				aid := string(aidRe.FindSubmatch(m)[1])
				if linkedAids[aid] {
					return append(append([]byte{}, m...), []byte(` id="aid-`+aid+`"`)...)
				}
				return m
			})
		}
		file := filepath.Join(folder, partName(i))
		if err := os.WriteFile(file, append([]byte{0xEF, 0xBB, 0xBF}, x...), 0o644); err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return readChapters(ctx, files, filepath.Join(folder, "~media"))
}

func partName(i int) string { return fmt.Sprintf("part%04d.xhtml", i) }

// assembleKf8 rebuilds the XHTML files: each skeleton is followed in the
// text by its fragments, which are inserted into it at their insert positions.
func assembleKf8(markup []byte, skeletons, fragments []indexEntry, strs map[int64]string) []kf8Part {
	if len(skeletons) == 0 {
		return []kf8Part{{markup, 0}}
	}
	var parts []kf8Part
	next := 0
	n := int64(len(markup))
	for _, sk := range skeletons {
		skStart := max(0, min(sk.tag(6, 0), n))
		skLen := max(0, min(sk.tag(6, 1), n-skStart))
		part := append([]byte(nil), markup[skStart:skStart+skLen]...)
		pos := skStart + skLen
		count := max(0, sk.tag(1, 0))
		for i := int64(0); i < count && next < len(fragments); i, next = i+1, next+1 {
			fr := fragments[next]
			length := max(0, min(fr.tag(6, 1), n-pos))
			slice := markup[pos : pos+length]
			pos += length
			key, _ := strconv.ParseInt(fr.key, 10, 64)
			insert := int(max(0, min(key-skStart, int64(len(part)))))
			if insideTag(part, insert) {
				if at := afterAidTag(part, strs[fr.tag(2, 0)]); at >= 0 {
					insert = at
				} else {
					insert = nextTagEnd(part, insert)
				}
			}
			part = append(part[:insert], append(append([]byte(nil), slice...), part[insert:]...)...)
		}
		parts = append(parts, kf8Part{part, skStart})
	}
	return parts
}

func insideTag(t []byte, pos int) bool {
	for i := pos - 1; i >= 0; i-- {
		switch t[i] {
		case '>':
			return false
		case '<':
			return true
		}
	}
	return false
}

func nextTagEnd(t []byte, pos int) int {
	if e := bytes.IndexByte(t[pos:], '>'); e >= 0 {
		return pos + e + 1
	}
	return len(t)
}

// afterAidTag is the insert position after the tag a fragment selector such as P-//*[@aid='3'] names, or -1.
func afterAidTag(t []byte, selector string) int {
	m := selectorAidRe.FindStringSubmatch(selector)
	if m == nil {
		return -1
	}
	at := bytes.Index(t, []byte(`aid="`+m[1]+`"`))
	if at < 0 {
		return -1
	}
	return nextTagEnd(t, at)
}

// idBefore is the id a link to position lands on: the nearest id or name
// attribute of a tag before it (an "aid" is noted so an id can be added), or
// "" for the top of the file.
func idBefore(t []byte, pos int, linkedAids map[string]bool) string {
	pos = max(0, min(pos, len(t)))
	open := bytes.IndexByte(t[pos:], '<')
	closeAt := bytes.IndexByte(t[pos:], '>')
	if closeAt >= 0 && (open == 0 || open < 0 || closeAt < open) {
		pos += closeAt + 1 // inside a tag (or at its start): that tag counts
	}
	end := pos
	for end > 0 {
		tagEnd := bytes.LastIndexByte(t[:end], '>')
		if tagEnd < 0 {
			break
		}
		tagStart := bytes.LastIndexByte(t[:tagEnd+1], '<')
		if tagStart < 0 {
			break
		}
		tag := string(t[tagStart : tagEnd+1])
		end = tagStart
		lower := strings.ToLower(tag)
		if strings.HasPrefix(lower, "<body") {
			return ""
		}
		if strings.HasPrefix(lower, "<meta") {
			continue
		}
		if m := idAttrRe.FindStringSubmatch(tag); m != nil {
			return m[1]
		}
		if m := aidAttrRe.FindStringSubmatch(tag); m != nil {
			linkedAids[m[1]] = true
			return "aid-" + m[1]
		}
	}
	return ""
}

func kf8Flow(flows [][]byte, index int, mime, folder string) string {
	if index <= 0 || index >= len(flows) {
		return "missing"
	}
	ext := "txt"
	switch {
	case strings.Contains(strings.ToLower(mime), "svg"):
		ext = "svg"
	case strings.Contains(strings.ToLower(mime), "css"):
		ext = "css"
	}
	name := fmt.Sprintf("flow%04d.%s", index, ext)
	file := filepath.Join(folder, name)
	if !isFile(file) {
		os.WriteFile(file, flows[index], 0o644)
	}
	return name
}

// base32 is Kindle's base-32 numbers: digits 0-9 then A-V.
func base32(s string) int64 {
	var v int64
	for _, c := range strings.ToUpper(s) {
		d := int64(c - '0')
		if c > '9' {
			d = int64(c-'A') + 10
		}
		v = v*32 + d
	}
	return v
}

// ---- pictures ----

// mobiResources are the picture records, numbered from the first image record; written to files on first use.
type mobiResources struct {
	db     *palmDatabase
	first  int64
	folder string
	saved  map[int]string
}

func (r *mobiResources) save(index int) string {
	if r.first < 0 || index < 0 {
		return ""
	}
	if f, ok := r.saved[index]; ok {
		return f
	}
	rec := r.db.record(int(r.first) + index)
	ext := ""
	switch {
	case bytes.HasPrefix(rec, []byte{0xFF, 0xD8, 0xFF}):
		ext = "jpg"
	case bytes.HasPrefix(rec, []byte("\x89PNG")):
		ext = "png"
	case bytes.HasPrefix(rec, []byte("GIF8")):
		ext = "gif"
	case bytes.HasPrefix(rec, []byte("BM")):
		ext = "bmp"
	}
	file := ""
	if ext != "" {
		file = filepath.Join(r.folder, fmt.Sprintf("image%05d.%s", index+1, ext))
		if os.WriteFile(file, rec, 0o644) != nil {
			file = ""
		}
	}
	r.saved[index] = file
	return file
}

// ---- header ----

// mobiHeader is the MOBI header of record 0 (or of the KF8 half of a joint file) and its EXTH metadata.
type mobiHeader struct {
	compression, textRecords, encryption, codepage int
	textLength, version, firstImage                int64
	huffRecord, huffCount, extraFlags              int
	fdst, fragment, skeleton                       int64
	kf8Boundary                                    int // record of the KF8 header in a joint file (EXTH 121), -1
	coverOffset                                    int // cover picture from the first image record (EXTH 201), -1
	fullName                                       string
	locale                                         int
	exth                                           map[int][][]byte
}

func parseMobiHeader(db *palmDatabase, record int) (*mobiHeader, error) {
	d := db.record(record)
	if len(d) < 16 {
		return nil, errors.New("the book's header is damaged")
	}
	hasMobi := len(d) >= 24 && string(d[16:20]) == "MOBI"
	length := 0
	if hasMobi {
		length = int(u32(d, 20))
	}
	h := &mobiHeader{
		compression: u16(d, 0), textLength: u32(d, 4), textRecords: u16(d, 8), encryption: u16(d, 12),
		codepage: 1252, firstImage: -1, fdst: -1, fragment: -1, skeleton: -1, kf8Boundary: -1, coverOffset: -1,
		exth: map[int][][]byte{},
	}
	if !hasMobi {
		return h, nil
	}
	h.codepage = int(u32(d, 28))
	h.version = u32(d, 36)
	h.firstImage = u32(d, 108)
	h.huffRecord = int(max(0, u32(d, 112)))
	h.huffCount = int(max(0, u32(d, 116)))
	// MOBI 6 files reuse 0xC0 for "first content record": only KF8 headers have an FDST index there.
	if h.version >= 8 {
		if length >= 0xE4 {
			h.extraFlags = u16(d, 0xF2)
			h.fdst = u32(d, 0xC0)
		}
		if length >= 0xE8 {
			h.fragment = u32(d, 0xF8)
		}
		if length >= 0xEC {
			h.skeleton = u32(d, 0xFC)
		}
	} else if length >= 0xE4 {
		h.extraFlags = u16(d, 0xF2)
	}
	nameOffset, nameLength := int(u32(d, 84)), int(u32(d, 88))
	h.locale = int(max(0, u32(d, 92)))
	if nameOffset > 0 && nameLength > 0 && nameOffset+nameLength <= len(d) {
		h.fullName = h.decode(d[nameOffset : nameOffset+nameLength])
	}
	if u32(d, 128)&0x40 != 0 && 16+length+12 <= len(d) && string(d[16+length:16+length+4]) == "EXTH" {
		exth := 16 + length
		count := int(u32(d, exth+8))
		pos := exth + 12
		for i := 0; i < count && pos+8 <= len(d); i++ {
			typ, size := int(u32(d, pos)), int(u32(d, pos+4))
			if size < 8 || pos+size > len(d) {
				break
			}
			h.exth[typ] = append(h.exth[typ], d[pos+8:pos+size])
			pos += size
		}
		if b, ok := h.number(121); ok && b > 0 {
			h.kf8Boundary = b
		}
		if c, ok := h.number(201); ok {
			h.coverOffset = c
		}
	}
	return h, nil
}

func (h *mobiHeader) decode(b []byte) string {
	if h.codepage == 65001 {
		return strings.ToValidUTF8(string(b), "")
	}
	s, _ := charmap.Windows1252.NewDecoder().Bytes(b)
	return string(s)
}

func (h *mobiHeader) strings(typ int) []string {
	var out []string
	for _, v := range h.exth[typ] {
		if s := strings.Trim(h.decode(v), "\x00 "); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (h *mobiHeader) number(typ int) (int, bool) {
	v := h.exth[typ]
	if len(v) == 0 || len(v[0]) != 4 {
		return 0, false
	}
	n := binary.BigEndian.Uint32(v[0])
	if n == 0xFFFFFFFF {
		return 0, false
	}
	return int(min(n, 1<<31-1)), true
}

func firstOf(s []string) string {
	if len(s) > 0 {
		return s[0]
	}
	return ""
}

// metadata: title (EXTH 503, else the full name, else the database name), authors, publisher, language.
func (h *mobiHeader) metadata(dbName string) *Metadata {
	m := &Metadata{
		Title:       firstOf(h.strings(503)),
		Publisher:   firstOf(h.strings(101)),
		Description: firstOf(h.strings(103)),
		Language:    firstOf(h.strings(524)),
	}
	if m.Title == "" {
		m.Title = h.fullName
	}
	if m.Title == "" {
		m.Title = strings.ReplaceAll(dbName, "_", " ")
	}
	if m.Language == "" {
		m.Language = mobiLanguage(h.locale)
	}
	for _, a := range h.strings(100) {
		for _, n := range strings.Split(a, "&") {
			if n = strings.TrimSpace(n); n != "" && !contains(m.Authors, n) {
				m.Authors = append(m.Authors, n)
			}
		}
	}
	return m
}

// mobiLanguage: Windows language id (low byte of the MOBI locale) → language tag.
func mobiLanguage(locale int) string {
	return map[int]string{0x04: "zh", 0x07: "de", 0x09: "en", 0x0A: "es", 0x0C: "fr", 0x10: "it", 0x11: "ja", 0x12: "ko", 0x13: "nl", 0x16: "pt", 0x19: "ru"}[locale&0xFF]
}
