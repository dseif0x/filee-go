package email

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/richardlehane/mscfb"
)

// Outlook .msg (MS-OXMSG: MAPI properties in an OLE compound file) rebuilt
// as a MIME message, so MSG → HTML, TXT and ZIP take the same path as EML
// and MSG → EML is the rebuilt message itself. Carried over: sender, To /
// Cc / Bcc, subject, sent date, Message-ID, the text and HTML bodies (the
// HTML out of RTF-only messages too), attachments, pictures shown in the
// body (Content-ID) and attached Outlook items, which become attached .eml
// messages. Attachments that only link to a file elsewhere are not followed.

const (
	ptLong    = 0x0003
	ptTime    = 0x0040
	ptString8 = 0x001E
	ptUnicode = 0x001F
	ptBinary  = 0x0102
	ptObject  = 0x000D
)

// msgStorage is one storage of the compound file: its streams and sub storages.
type msgStorage struct {
	streams  map[string][]byte
	storages map[string]*msgStorage
}

func newStorage() *msgStorage {
	return &msgStorage{streams: map[string][]byte{}, storages: map[string]*msgStorage{}}
}

func readCompound(path string) (*msgStorage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r, err := mscfb.New(f)
	if err != nil {
		return nil, fmt.Errorf("Not an Outlook message: %v", err)
	}
	root := newStorage()
	for entry, err := r.Next(); err == nil; entry, err = r.Next() {
		dir := root
		for _, p := range entry.Path {
			sub, ok := dir.storages[p]
			if !ok {
				sub = newStorage()
				dir.storages[p] = sub
			}
			dir = sub
		}
		if entry.FileInfo().IsDir() {
			if _, ok := dir.storages[entry.Name]; !ok {
				dir.storages[entry.Name] = newStorage()
			}
			continue
		}
		if entry.Size > 256<<20 {
			return nil, errors.New("the Outlook message is too large")
		}
		data := make([]byte, entry.Size)
		if _, err := io.ReadFull(entry, data); err != nil && err != io.EOF {
			return nil, fmt.Errorf("Not an Outlook message: %v", err)
		}
		dir.streams[entry.Name] = data
	}
	return root, nil
}

// msgProps are the properties of one storage.
type msgProps struct {
	s        *msgStorage
	fixed    map[uint32][]byte // tag → 8-byte value
	codepage int
}

func props(s *msgStorage, headerSize int) *msgProps {
	p := &msgProps{s: s, fixed: map[uint32][]byte{}}
	data := s.streams["__properties_version1.0"]
	for off := headerSize; off+16 <= len(data); off += 16 {
		tag := binary.LittleEndian.Uint32(data[off:])
		p.fixed[tag] = data[off+8 : off+16]
	}
	return p
}

func (p *msgProps) stream(id uint16, typ uint16) ([]byte, bool) {
	b, ok := p.s.streams[fmt.Sprintf("__substg1.0_%04X%04X", id, typ)]
	return b, ok
}

// str is a string property (Unicode or 8-bit in the message's code page).
func (p *msgProps) str(id uint16) string {
	if b, ok := p.stream(id, ptUnicode); ok {
		return strings.TrimRight(utf16le(b), "\x00")
	}
	if b, ok := p.stream(id, ptString8); ok {
		b = bytes.TrimRight(b, "\x00")
		if p.codepage != 0 && p.codepage != 65001 {
			if e := charsetEncoding("windows-" + strconv.Itoa(p.codepage)); e != nil {
				if s, err := e.NewDecoder().Bytes(b); err == nil {
					return string(s)
				}
			}
			if p.codepage == 949 {
				return decodeCharset(b, "cp949")
			}
		}
		return decodeCharset(b, "")
	}
	return ""
}

func (p *msgProps) bin(id uint16) ([]byte, bool) { return p.stream(id, ptBinary) }

func (p *msgProps) long(id uint16) (int, bool) {
	v, ok := p.fixed[uint32(id)<<16|ptLong]
	if !ok {
		return 0, false
	}
	return int(int32(binary.LittleEndian.Uint32(v))), true
}

func (p *msgProps) time(id uint16) (time.Time, bool) {
	v, ok := p.fixed[uint32(id)<<16|ptTime]
	if !ok {
		return time.Time{}, false
	}
	ft := int64(binary.LittleEndian.Uint64(v))
	if ft <= 0 {
		return time.Time{}, false
	}
	return time.Unix(0, 0).Add(time.Duration(ft-116444736000000000) * 100).UTC(), true
}

func utf16le(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[i*2:])
	}
	return string(utf16.Decode(u))
}

// loadOutlookMessage reads an Outlook .msg as MIME bytes.
func loadOutlookMessage(path string) ([]byte, error) {
	root, err := readCompound(path)
	if err != nil {
		return nil, err
	}
	if _, ok := root.streams["__properties_version1.0"]; !ok {
		return nil, errors.New("Not an Outlook message: it has no MAPI properties")
	}
	m := outlookToMime(root, 32, 0)
	return m.bytes(), nil
}

func outlookToMime(s *msgStorage, headerSize, depth int) *outMessage {
	p := props(s, headerSize)
	if cp, ok := p.long(0x3FDE); ok { // PR_INTERNET_CPID
		p.codepage = cp
	} else if cp, ok := p.long(0x3FFD); ok { // PR_MESSAGE_CODEPAGE
		p.codepage = cp
	}
	m := &outMessage{subject: p.str(0x0037)}
	senderEmail := firstNonEmpty(p.str(0x5D01), p.str(0x0C1F), p.str(0x0065))
	if mb := outlookMailbox(firstNonEmpty(p.str(0x0C1A), p.str(0x0042)), senderEmail); mb != nil {
		m.from = mb
	}
	// Recipients in row order.
	var recipNames []string
	for name := range s.storages {
		if strings.HasPrefix(name, "__recip_version1.0_") {
			recipNames = append(recipNames, name)
		}
	}
	sort.Strings(recipNames)
	for _, name := range recipNames {
		rp := props(s.storages[name], 8)
		rp.codepage = p.codepage
		mb := outlookMailbox(rp.str(0x3001), firstNonEmpty(rp.str(0x39FE), rp.str(0x3003)))
		if mb == nil {
			continue
		}
		switch t, _ := rp.long(0x0C15); t {
		case 2:
			m.cc = append(m.cc, *mb)
		case 3:
			m.bcc = append(m.bcc, *mb)
		default:
			m.to = append(m.to, *mb)
		}
	}
	if t, ok := p.time(0x0039); ok { // PR_CLIENT_SUBMIT_TIME
		m.date = t
	} else if t, ok := p.time(0x0E06); ok { // PR_MESSAGE_DELIVERY_TIME
		m.date = t
	}
	m.messageID = strings.Trim(p.str(0x1035), "<> ")
	if m.messageID == "" {
		for _, l := range strings.Split(p.str(0x007D), "\n") {
			if k, v, ok := strings.Cut(l, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "Message-ID") {
				m.messageID = strings.Trim(strings.TrimSpace(v), "<>")
			}
		}
	}
	if text := p.str(0x1000); text != "" {
		m.text, m.hasText = text, true
	}
	if h, ok := p.bin(0x1013); ok && len(h) > 0 {
		m.html, m.hasHTML = decodeCharset(bytes.TrimRight(h, "\x00"), htmlCharset(p.codepage)), true
	} else if h := p.str(0x1013); h != "" {
		m.html, m.hasHTML = h, true
	} else if rtf, ok := p.bin(0x1009); ok { // PR_RTF_COMPRESSED
		if raw, err := decompressRTF(rtf); err == nil {
			if h, ok := rtfToHTML(raw); ok {
				m.html, m.hasHTML = h, true
			} else if !m.hasText {
				if t := rtfToText(raw); strings.TrimSpace(t) != "" {
					m.text, m.hasText = t, true
				}
			}
		}
	}
	// Attachments in their order.
	var attachNames []string
	for name := range s.storages {
		if strings.HasPrefix(name, "__attach_version1.0_") {
			attachNames = append(attachNames, name)
		}
	}
	sort.Strings(attachNames)
	for _, name := range attachNames {
		as := s.storages[name]
		ap := props(as, 8)
		ap.codepage = p.codepage
		method, _ := ap.long(0x3705)
		if inner, ok := as.storages["__substg1.0_3701000D"]; ok && depth < maxDepth {
			msg := outlookToMime(inner, 24, depth+1)
			file := msg.subject
			if strings.TrimSpace(file) == "" {
				file = "message"
			}
			m.attachments = append(m.attachments, outPart{name: file, message: msg})
			continue
		}
		data, ok := ap.bin(0x3701)
		if !ok || method >= 2 && method <= 4 { // links to files elsewhere are not followed
			continue
		}
		file := firstNonEmpty(ap.str(0x3707), ap.str(0x3704), ap.str(0x3001))
		if strings.TrimSpace(file) == "" {
			file = "attachment"
		}
		mt := strings.TrimSpace(ap.str(0x370E))
		if _, _, err := mime.ParseMediaType(mt); err != nil || mt == "" {
			mt = mime.TypeByExtension(strings.ToLower(filepath.Ext(file)))
			if mt == "" {
				mt = "application/octet-stream"
			}
			mt, _, _ = mime.ParseMediaType(mt)
		}
		cid := strings.Trim(ap.str(0x3712), "<> ")
		part := outPart{name: file, mediaType: mt, contentID: cid, data: data}
		// A picture the HTML body shows ("cid:") goes with the body, not into the attachments.
		if cid != "" && m.hasHTML && strings.Contains(strings.ToLower(m.html), "cid:"+strings.ToLower(cid)) {
			m.resources = append(m.resources, part)
		} else {
			part.contentID = ""
			m.attachments = append(m.attachments, part)
		}
	}
	return m
}

func htmlCharset(cp int) string {
	switch cp {
	case 0, 65001:
		return ""
	case 949:
		return "cp949"
	}
	return "windows-" + strconv.Itoa(cp)
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// outlookMailbox is a sender or recipient; Exchange-only addresses (no "@") keep the name.
func outlookMailbox(name, address string) *mailbox {
	name, address = strings.TrimSpace(name), strings.TrimSpace(address)
	if name == "" && address == "" {
		return nil
	}
	if strings.EqualFold(name, address) {
		name = ""
	}
	return &mailbox{name, address}
}

// ---- compressed RTF (MS-OXRTFCP) ----

const rtfPrebuf = "{\\rtf1\\ansi\\mac\\deff0\\deftab720{\\fonttbl;}{\\f0\\fnil \\froman \\fswiss \\fmodern \\fscript \\fdecor MS Sans SerifSymbolArialTimes New RomanCourier{\\colortbl\\red0\\green0\\blue0\r\n\\par \\pard\\plain\\f0\\fs20\\b\\i\\u\\tab\\tx"

var errRTF = errors.New("the compressed RTF body is damaged")

func decompressRTF(data []byte) ([]byte, error) {
	if len(data) < 16 {
		return nil, errRTF
	}
	compSize := int(binary.LittleEndian.Uint32(data[0:]))
	rawSize := int(binary.LittleEndian.Uint32(data[4:]))
	magic := binary.LittleEndian.Uint32(data[8:])
	switch magic {
	case 0x414C454D: // "MELA": not compressed
		end := min(len(data), 16+rawSize)
		return data[16:end], nil
	case 0x75465A4C: // "LZFu"
	default:
		return nil, errRTF
	}
	in := data[16:min(len(data), compSize+4)]
	var dict [4096]byte
	copy(dict[:], rtfPrebuf)
	wpos := len(rtfPrebuf)
	out := make([]byte, 0, rawSize)
	for i := 0; i < len(in); {
		control := in[i]
		i++
		for bit := 0; bit < 8 && i < len(in); bit++ {
			if control&(1<<bit) == 0 {
				c := in[i]
				i++
				out = append(out, c)
				dict[wpos%4096] = c
				wpos++
				continue
			}
			if i+1 >= len(in) {
				return out, nil
			}
			ref := int(in[i])<<8 | int(in[i+1])
			i += 2
			offset, length := ref>>4, ref&0xF+2
			if offset == wpos%4096 {
				return out, nil // the end marker
			}
			for k := 0; k < length; k++ {
				c := dict[(offset+k)%4096]
				out = append(out, c)
				dict[wpos%4096] = c
				wpos++
			}
		}
	}
	return out, nil
}

// ---- RTF → HTML (MS-OXRTFEX de-encapsulation) and plain text ----

type rtfToken struct {
	kind  byte // '{', '}', '\\' control word, 't' text, 'x' hex char, 'u' unicode
	word  string
	param int
	has   bool
	text  []byte
}

func rtfTokens(b []byte, visit func(rtfToken)) {
	for i := 0; i < len(b); {
		c := b[i]
		switch c {
		case '{', '}':
			visit(rtfToken{kind: c})
			i++
		case '\\':
			i++
			if i >= len(b) {
				return
			}
			c = b[i]
			switch {
			case c == '\'' && i+2 < len(b):
				v, err := strconv.ParseUint(string(b[i+1:i+3]), 16, 8)
				if err == nil {
					visit(rtfToken{kind: 'x', param: int(v)})
				}
				i += 3
			case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
				start := i
				for i < len(b) && (b[i] >= 'a' && b[i] <= 'z' || b[i] >= 'A' && b[i] <= 'Z') {
					i++
				}
				t := rtfToken{kind: '\\', word: string(b[start:i])}
				ns := i
				if i < len(b) && (b[i] == '-' || b[i] >= '0' && b[i] <= '9') {
					i++
					for i < len(b) && b[i] >= '0' && b[i] <= '9' {
						i++
					}
					t.param, _ = strconv.Atoi(string(b[ns:i]))
					t.has = true
				}
				if i < len(b) && b[i] == ' ' {
					i++
				}
				visit(t)
			default:
				// Control symbols: \{ \} \\ are text; \* marks a destination; others (\~ \- …).
				switch c {
				case '{', '}', '\\':
					visit(rtfToken{kind: 't', text: []byte{c}})
				case '~':
					visit(rtfToken{kind: 't', text: []byte(" ")})
				case '*':
					visit(rtfToken{kind: '\\', word: "*"})
				case '\n', '\r':
					visit(rtfToken{kind: '\\', word: "par"})
				}
				i++
			}
		case '\r', '\n':
			i++
		default:
			start := i
			for i < len(b) && b[i] != '{' && b[i] != '}' && b[i] != '\\' && b[i] != '\r' && b[i] != '\n' {
				i++
			}
			visit(rtfToken{kind: 't', text: b[start:i]})
		}
	}
}

var rtfDestinations = map[string]bool{"fonttbl": true, "colortbl": true, "stylesheet": true, "info": true, "pict": true,
	"object": true, "header": true, "footer": true, "listtable": true, "listoverridetable": true, "rsidtbl": true,
	"generator": true, "themedata": true, "datastore": true, "latentstyles": true, "xmlnstbl": true, "mhtmltag": true}

type rtfState struct {
	skip, htmlTag, htmlRtf bool
	uc                     int
}

// rtfWalk extracts the text an RTF body shows; html selects the HTML de-encapsulation rules.
func rtfWalk(raw []byte, htmlMode bool) string {
	codepage := 1252
	var out []byte
	var pendingHex []byte
	flushHex := func() {
		if len(pendingHex) > 0 {
			out = append(out, decodeCharset(pendingHex, htmlCharset(codepage))...)
			pendingHex = nil
		}
	}
	stack := []rtfState{{uc: 1}}
	skipChars := 0
	destStart := false
	emit := func(s string, st rtfState) {
		if st.skip {
			return
		}
		if htmlMode && st.htmlRtf && !st.htmlTag {
			return
		}
		out = append(out, s...)
	}
	rtfTokens(raw, func(t rtfToken) {
		st := &stack[len(stack)-1]
		if t.kind != 'x' {
			flushHex()
		}
		switch t.kind {
		case '{':
			stack = append(stack, *st)
			destStart = true
			return
		case '}':
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
			destStart = false
			return
		}
		first := destStart
		destStart = false
		switch t.kind {
		case '\\':
			switch t.word {
			case "*":
				st.skip = true // unknown destinations are skipped; \*\htmltag turns it back on
				destStart = first
			case "htmltag":
				if htmlMode {
					st.skip, st.htmlTag = false, true
				}
			case "htmlrtf":
				st.htmlRtf = !t.has || t.param != 0
			case "ansicpg":
				codepage = t.param
			case "uc":
				st.uc = t.param
			case "u":
				v := t.param
				if v < 0 {
					v += 65536
				}
				emit(string(rune(v)), *st)
				skipChars = st.uc
			case "par", "line":
				emit("\r\n", *st)
			case "tab":
				emit("\t", *st)
			case "lquote":
				emit("‘", *st)
			case "rquote":
				emit("’", *st)
			case "ldblquote":
				emit("“", *st)
			case "rdblquote":
				emit("”", *st)
			case "emdash":
				emit("—", *st)
			case "endash":
				emit("–", *st)
			default:
				if first && rtfDestinations[t.word] {
					st.skip = true
				}
			}
		case 'x':
			if skipChars > 0 {
				skipChars--
				return
			}
			if st.skip || htmlMode && st.htmlRtf && !st.htmlTag {
				return
			}
			pendingHex = append(pendingHex, byte(t.param))
		case 't':
			text := t.text
			for skipChars > 0 && len(text) > 0 {
				text = text[1:]
				skipChars--
			}
			emit(string(text), *st)
		}
	})
	flushHex()
	return string(out)
}

// rtfToHTML de-encapsulates the HTML an RTF body carries (\fromhtml1); false when it is plain RTF.
func rtfToHTML(raw []byte) (string, bool) {
	head := raw[:min(len(raw), 1024)]
	if !bytes.Contains(head, []byte(`\fromhtml`)) {
		return "", false
	}
	return rtfWalk(raw, true), true
}

func rtfToText(raw []byte) string { return rtfWalk(raw, false) }
