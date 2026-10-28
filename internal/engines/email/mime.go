package email

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/encoding/korean"

	"github.com/dseif0x/filee-go/internal/doc"
)

// A MIME entity tree (RFC 5322 / 2045-2049) as Filee needs it: headers,
// media type, decoded content of leaves, children of multiparts and the
// message inside message/rfc822 parts. Header text in raw 8-bit that is not
// UTF-8 is read as CP949, as older Korean mail programs wrote it; encoded
// words (RFC 2047), RFC 2231 parameters and the Korean charsets (EUC-KR /
// CP949, ISO-2022-KR) are understood.

type part struct {
	header      textproto.MIMEHeader
	mediaType   string // lower case "type/subtype"
	params      map[string]string
	disposition string
	dispParams  map[string]string
	body        []byte  // decoded content of a leaf
	children    []*part // multipart
	message     *part   // message/rfc822
	raw         []byte  // the attached message as written
}

// maxDepth limits nested multiparts and attached messages.
const maxDepth = 50

func loadMessage(path string) (*part, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseMessage(b)
}

func parseMessage(b []byte) (*part, error) {
	// An mbox "From " line in front of the headers (messages saved from Unix mail boxes).
	if bytes.HasPrefix(b, []byte("From ")) {
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		}
	}
	p, err := parsePart(b, 0)
	if err != nil {
		return nil, err
	}
	if len(p.header) == 0 {
		return nil, errors.New("this is not an e-mail message (no headers)")
	}
	return p, nil
}

func parsePart(b []byte, depth int) (*part, error) {
	r := textproto.NewReader(bufio.NewReader(bytes.NewReader(b)))
	h, err := r.ReadMIMEHeader()
	if err != nil && err != io.EOF && len(h) == 0 {
		// No header block: the whole text is the content.
		h = textproto.MIMEHeader{}
		r = textproto.NewReader(bufio.NewReader(bytes.NewReader(b)))
	}
	body, _ := io.ReadAll(r.R)
	p := &part{header: h, mediaType: "text/plain", params: map[string]string{}}
	if ct := h.Get("Content-Type"); ct != "" {
		if mt, params, err := mime.ParseMediaType(ct); err == nil {
			p.mediaType, p.params = strings.ToLower(mt), params
		} else if mt := strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0])); strings.Contains(mt, "/") {
			p.mediaType = mt
			p.params = looseParams(ct)
		}
	}
	if cd := h.Get("Content-Disposition"); cd != "" {
		if d, params, err := mime.ParseMediaType(cd); err == nil {
			p.disposition, p.dispParams = strings.ToLower(d), params
		} else {
			p.disposition = strings.ToLower(strings.TrimSpace(strings.Split(cd, ";")[0]))
			p.dispParams = looseParams(cd)
		}
	}
	switch {
	case strings.HasPrefix(p.mediaType, "multipart/") && p.params["boundary"] != "" && depth < maxDepth:
		mr := multipart.NewReader(bytes.NewReader(body), p.params["boundary"])
		for {
			np, err := mr.NextRawPart()
			if err != nil {
				break
			}
			content, err := io.ReadAll(np)
			if err != nil && len(content) == 0 {
				break
			}
			child, err := parsePart(append(headerBytes(np.Header), content...), depth+1)
			if err != nil {
				break
			}
			p.children = append(p.children, child)
		}
	case p.mediaType == "message/rfc822" && depth < maxDepth:
		raw := decodeTransfer(body, h.Get("Content-Transfer-Encoding"))
		p.raw = raw
		if m, err := parsePart(raw, depth+1); err == nil {
			p.message = m
		}
	default:
		p.body = decodeTransfer(body, h.Get("Content-Transfer-Encoding"))
	}
	return p, nil
}

// looseParams reads name=value parameters of a header mime refuses (duplicate or broken parameters).
func looseParams(v string) map[string]string {
	out := map[string]string{}
	for _, seg := range strings.Split(v, ";")[1:] {
		k, val, ok := strings.Cut(seg, "=")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		if _, dup := out[k]; !dup {
			out[k] = strings.Trim(strings.TrimSpace(val), `"`)
		}
	}
	return out
}

func headerBytes(h textproto.MIMEHeader) []byte {
	var b bytes.Buffer
	for k, vs := range h {
		for _, v := range vs {
			b.WriteString(k + ": " + v + "\r\n")
		}
	}
	b.WriteString("\r\n")
	return b.Bytes()
}

func decodeTransfer(body []byte, encodingName string) []byte {
	switch strings.ToLower(strings.TrimSpace(encodingName)) {
	case "base64":
		clean := make([]byte, 0, len(body))
		for _, c := range body {
			if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/' {
				clean = append(clean, c)
			}
		}
		if len(clean)%4 == 1 {
			clean = clean[:len(clean)-1] // a stray character can't be decoded
		}
		out := make([]byte, base64.RawStdEncoding.DecodedLen(len(clean)))
		n, _ := base64.RawStdEncoding.Decode(out, clean)
		return out[:n]
	case "quoted-printable":
		out, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(body)))
		if err != nil && len(out) == 0 {
			return body
		}
		return out
	}
	return body
}

// ---- charsets ----

// charsetEncoding is the decoder of a charset name, nil for UTF-8 and unknown names.
func charsetEncoding(name string) encoding.Encoding {
	n := strings.ToLower(strings.Trim(strings.TrimSpace(name), `"`))
	switch n {
	case "", "utf-8", "utf8", "us-ascii", "ascii":
		return nil
	case "iso-2022-kr", "csiso2022kr":
		return iso2022kr{}
	case "ks_c_5601-1987", "ks_c_5601", "cp949", "uhc", "x-windows-949", "ksc5601":
		return korean.EUCKR
	}
	e, err := htmlindex.Get(n)
	if err != nil || e == encoding.Replacement {
		return nil
	}
	return e
}

// decodeCharset decodes text of a declared charset; without one (or a wrong
// "us-ascii" over 8-bit text) UTF-8 when valid, else the PC's code page or CP949.
func decodeCharset(b []byte, name string) string {
	if e := charsetEncoding(name); e != nil {
		if s, err := e.NewDecoder().Bytes(b); err == nil {
			return string(s)
		}
	}
	if utf8.Valid(b) {
		return string(b)
	}
	return doc.DecodeText(b)
}

// rawHeader is header text with 8-bit bytes that are not UTF-8 read as CP949.
func rawHeader(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	if out, err := korean.EUCKR.NewDecoder().String(s); err == nil {
		return out
	}
	return strings.ToValidUTF8(s, "�")
}

var wordDecoder = &mime.WordDecoder{CharsetReader: func(charset string, input io.Reader) (io.Reader, error) {
	e := charsetEncoding(charset)
	if e == nil {
		return input, nil
	}
	return e.NewDecoder().Reader(input), nil
}}

// decodeHeader decodes encoded words.
func decodeHeader(s string) string {
	s = rawHeader(s)
	if out, err := wordDecoder.DecodeHeader(s); err == nil {
		return out
	}
	return s
}

// ---- ISO-2022-KR (old Korean mail): SO/SI shift between ASCII and KS X 1001 ----

type iso2022kr struct{ encoding.Encoding }

func (iso2022kr) NewDecoder() *encoding.Decoder {
	return &encoding.Decoder{Transformer: &iso2022krDecoder{}}
}

type iso2022krDecoder struct{ shifted bool }

func (d *iso2022krDecoder) Reset() { d.shifted = false }

func (d *iso2022krDecoder) Transform(dst, src []byte, atEOF bool) (nDst, nSrc int, err error) {
	var euc []byte
	flush := func() error {
		if len(euc) == 0 {
			return nil
		}
		out, err := korean.EUCKR.NewDecoder().Bytes(euc)
		if err != nil {
			return err
		}
		if nDst+len(out) > len(dst) {
			return errShortDst
		}
		nDst += copy(dst[nDst:], out)
		euc = euc[:0]
		return nil
	}
	for nSrc < len(src) {
		c := src[nSrc]
		switch {
		case c == 0x1B && nSrc+3 < len(src) && string(src[nSrc:nSrc+4]) == "\x1B$)C":
			nSrc += 4
			continue
		case c == 0x1B && !atEOF && nSrc+4 > len(src):
			if err := flush(); err != nil {
				return nDst, nSrc, err
			}
			return nDst, nSrc, errShortSrc
		case c == 0x0E:
			d.shifted = true
			nSrc++
			continue
		case c == 0x0F:
			if err := flush(); err != nil {
				return nDst, nSrc, err
			}
			d.shifted = false
			nSrc++
			continue
		}
		if d.shifted && c > 0x20 && c < 0x7F {
			if nSrc+1 >= len(src) {
				if !atEOF {
					if err := flush(); err != nil {
						return nDst, nSrc, err
					}
					return nDst, nSrc, errShortSrc
				}
				nSrc++
				continue
			}
			euc = append(euc, c|0x80, src[nSrc+1]|0x80)
			nSrc += 2
			continue
		}
		if err := flush(); err != nil {
			return nDst, nSrc, err
		}
		if c == '\n' || c == '\r' {
			d.shifted = false // the shift state ends with the line
		}
		if nDst >= len(dst) {
			return nDst, nSrc, errShortDst
		}
		dst[nDst] = c
		nDst++
		nSrc++
	}
	return nDst, nSrc, flush()
}

var (
	errShortDst = errors.New("transform: short destination buffer")
	errShortSrc = errors.New("transform: short source buffer")
)

// ---- accessors ----

func (p *part) get(name string) string { return decodeHeader(p.header.Get(name)) }

// mailbox is one sender or recipient.
type mailbox struct{ name, address string }

func (m mailbox) String() string {
	if strings.TrimSpace(m.name) == "" {
		return m.address
	}
	return m.name + " <" + m.address + ">"
}

func (p *part) addresses(name string) []mailbox {
	raw := rawHeader(p.header.Get(name))
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parser := mail.AddressParser{WordDecoder: wordDecoder}
	if list, err := parser.ParseList(raw); err == nil {
		out := make([]mailbox, len(list))
		for i, a := range list {
			out[i] = mailbox{a.Name, a.Address}
		}
		return out
	}
	// Broken address syntax: keep what is written, one entry per comma.
	var out []mailbox
	for _, s := range strings.Split(decodeHeader(raw), ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, mailbox{"", s})
		}
	}
	return out
}

func (p *part) date() (time.Time, bool) {
	v := strings.TrimSpace(p.header.Get("Date"))
	if v == "" {
		return time.Time{}, false
	}
	t, err := mail.ParseDate(v)
	return t, err == nil
}

func (p *part) contentID() string {
	return strings.Trim(strings.TrimSpace(p.header.Get("Content-Id")), "<>")
}

func (p *part) fileName() string {
	if n := p.dispParams["filename"]; n != "" {
		return decodeHeader(n)
	}
	return decodeHeader(p.params["name"])
}

func (p *part) isAttachment() bool { return p.disposition == "attachment" }

// text is the content of a text part, decoded from its charset.
func (p *part) text() string { return decodeCharset(p.body, p.params["charset"]) }

// findBody is the body part of a subtype ("plain", "html"): the last match of
// multipart/alternative, the root of multipart/related, the first match elsewhere.
func (p *part) findBody(subtype string) *part {
	switch {
	case len(p.children) == 0 && p.message == nil:
		if p.mediaType == "text/"+subtype && !p.isAttachment() {
			return p
		}
		return nil
	case p.mediaType == "multipart/alternative":
		for i := len(p.children) - 1; i >= 0; i-- {
			if b := p.children[i].findBody(subtype); b != nil {
				return b
			}
		}
	case p.mediaType == "multipart/related":
		if root := p.relatedRoot(); root != nil {
			return root.findBody(subtype)
		}
	case strings.HasPrefix(p.mediaType, "multipart/"):
		for _, c := range p.children {
			if b := c.findBody(subtype); b != nil {
				return b
			}
		}
	}
	return nil
}

// relatedRoot is the root of multipart/related: the "start" parameter, else the first part.
func (p *part) relatedRoot() *part {
	if len(p.children) == 0 {
		return nil
	}
	if start := strings.Trim(p.params["start"], "<> "); start != "" {
		for _, c := range p.children {
			if strings.EqualFold(c.contentID(), start) {
				return c
			}
		}
	}
	return p.children[0]
}

func (p *part) textBody() (string, bool) {
	if b := p.findBody("plain"); b != nil {
		return b.text(), true
	}
	return "", false
}

func (p *part) htmlBody() (string, bool) {
	if b := p.findBody("html"); b != nil {
		return b.text(), true
	}
	return "", false
}

// bodyParts are every leaf and attached message, depth first (attached messages are not entered).
func (p *part) bodyParts() []*part {
	var out []*part
	var walk func(*part)
	walk = func(q *part) {
		if q.message != nil || q.mediaType == "message/rfc822" {
			out = append(out, q)
			return
		}
		if len(q.children) == 0 && !strings.HasPrefix(q.mediaType, "multipart/") {
			out = append(out, q)
			return
		}
		for _, c := range q.children {
			walk(c)
		}
	}
	if strings.HasPrefix(p.mediaType, "multipart/") {
		for _, c := range p.children {
			walk(c)
		}
	} else {
		walk(p)
	}
	return out
}

func (p *part) String() string { return fmt.Sprintf("%s (%d children)", p.mediaType, len(p.children)) }
