package web

import (
	"encoding/base64"
	"encoding/binary"
	"html"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/dseif0x/filee-go/internal/formats"
)

// The forms web browsers put a dragged picture, link or download into: pure
// parsing, so it can be tested anywhere. The GUI reads the raw formats from
// the drag and Files decides what in them is a file.
const (
	FileGroupDescriptorW    = "FileGroupDescriptorW"
	UniformResourceLocatorW = "UniformResourceLocatorW"
	UniformResourceLocator  = "UniformResourceLocator"
	MozURL                  = "text/x-moz-url"
	NetscapeURL             = "_NETSCAPE_URL"
	URIList                 = "text/uri-list"
	DownloadURLFormat       = "DownloadURL"
	MozFilePromiseURL       = "application/x-moz-file-promise-url"
	MozFilePromiseName      = "application/x-moz-file-promise-dest-filename"
	MacURL                  = "public.url"
	MacURLName              = "public.url-name"
	HTMLFormat              = "text/html"
	WindowsHTML             = "HTML Format"
	MacHTML                 = "public.html"
)

// AddressFormats are the formats that carry a web address.
var AddressFormats = []string{DownloadURLFormat, MozFilePromiseURL, MozURL, UniformResourceLocatorW, UniformResourceLocator, NetscapeURL, MacURL, URIList}

// DragContent is what Filee reads of a drag for files from a browser.
type DragContent struct {
	// Files the drag hands over with their bytes (virtual files, a browser's temporary copy).
	Files []DropItem
	// Texts of the address formats, by format name.
	Texts map[string]string
	// Picture makes the picture's pixels from the drag into a file; nil when there are none.
	Picture func() DropItem
}

// Text is the text of a format, "" when blank.
func (c *DragContent) Text(format string) string {
	if t := c.Texts[format]; strings.TrimSpace(t) != "" {
		return t
	}
	return ""
}

// HasAddress: the drag names a web address in a link format (it comes from a browser).
func (c *DragContent) HasAddress() bool {
	for _, f := range AddressFormats {
		if c.Text(f) != "" {
			return true
		}
	}
	return false
}

// DragFiles are the files in a browser drag, in the order dragged; empty when
// it holds none (a page link, a text selection). The rules, in order:
//  1. files the browser hands over itself (not Internet shortcuts or programs);
//  2. a picture (the HTML fragment is one <img> and no text; a Firefox file promise);
//  3. a download a page offers (DownloadURL);
//  4. links whose address ends in an extension Filee converts, never a web page;
//  5. else, when the drag carries the picture's pixels, those as PNG.
//
// Plain text is never read: a selected address is text, not a file. Files of
// c.Files not returned are closed.
func DragFiles(c *DragContent) []DropItem {
	var handed []DropItem
	for _, f := range c.Files {
		if IsBlocked(f.Name()) {
			_ = f.Close()
		} else {
			handed = append(handed, f)
		}
	}
	if len(handed) > 0 {
		return handed
	}
	picture := c.Picture != nil
	h := c.Text(HTMLFormat)
	if h == "" {
		h = c.Text(WindowsHTML)
	}
	if h == "" {
		h = c.Text(MacHTML)
	}
	if src := ImageOnly(h); src != "" {
		if it := addressItemFor(src, true, pageBase(c), "", ""); it != nil {
			return []DropItem{it}
		}
	}
	if promised := c.Text(MozFilePromiseURL); promised != "" {
		if it := addressItemFor(strings.TrimSpace(promised), true, nil, strings.TrimSpace(c.Text(MozFilePromiseName)), ""); it != nil {
			return []DropItem{it}
		}
	}
	if d := ParseDownloadURL(c.Text(DownloadURLFormat)); d != nil {
		if it := addressItemFor(d.URL.String(), false, nil, d.Name, d.Mime); it != nil {
			return []DropItem{it}
		}
	}
	var items []DropItem
	seen := map[string]bool{}
	for _, u := range links(c) {
		if !seen[u] {
			seen[u] = true
			if it := addressItemFor(u, picture, nil, "", ""); it != nil {
				items = append(items, it)
			}
		}
	}
	if len(items) > 0 {
		return items
	}
	if picture && c.HasAddress() {
		if px := c.Picture(); px != nil {
			return []DropItem{px}
		}
	}
	return nil
}

// links are the addresses of the link formats, the most precise first.
func links(c *DragContent) []string {
	var out []string
	out = append(out, ParseMozURL(c.Text(MozURL))...)
	for _, f := range []string{UniformResourceLocatorW, UniformResourceLocator, MacURL} {
		if t := c.Text(f); t != "" {
			out = append(out, strings.TrimSpace(t))
		}
	}
	out = append(out, ParseMozURL(c.Text(NetscapeURL))...)
	return append(out, ParseURIList(c.Text(URIList))...)
}

// pageBase is the page address, for a picture whose src is relative.
func pageBase(c *DragContent) *url.URL {
	for _, l := range links(c) {
		if u, err := url.Parse(l); err == nil && u.IsAbs() {
			return u
		}
	}
	return nil
}

// addressItemFor is the file at address, or nil when it isn't one Filee
// takes: not http(s) or data:, a web page, a program, or a link whose type
// can't be told before downloading. image: the address is a picture's (taken
// even without an extension; ".jpg" stands in until the bytes say).
func addressItemFor(address string, image bool, page *url.URL, name, mimeType string) DropItem {
	if strings.HasPrefix(strings.ToLower(address), "data:") {
		m, b, ok := DecodeDataURL(address, 256<<20)
		ext := ExtensionForMime(m)
		if !ok || ext == "" || formats.ByExtension(ext) == nil {
			return nil
		}
		if name == "" {
			name = "image"
		}
		return FromBytes(WithExtension(name, ext), b, m)
	}
	// "/img/a.png" and "//cdn/a.png" are relative to the page.
	var u *url.URL
	if strings.Contains(address, "://") {
		u, _ = url.Parse(address)
	} else if page != nil {
		if rel, err := url.Parse(address); err == nil {
			u = page.ResolveReference(rel)
		}
	}
	if u == nil || !IsWebAddress(u) {
		return nil
	}
	fallback := "download"
	if image {
		fallback = "image"
	}
	if name == "" {
		name = FromURL(u)
	}
	fileName := SanitizeName(name, fallback)
	if IsBlocked(fileName) {
		return nil
	}
	if IsPageName(fileName) {
		if !image {
			return nil
		}
		fileName = formats.NameWithoutExtension(fileName)
	}
	if formats.Detect(fileName) != nil {
		return FromAddress(fileName, u)
	}
	if image || IsImageMime(mimeType) {
		ext := ExtensionForMime(mimeType)
		if ext == "" {
			ext = "jpg"
		}
		return FromAddress(WithExtension(fileName, ext), u)
	}
	if ext := ExtensionForMime(mimeType); ext != "" && formats.ByExtension(ext) != nil {
		return FromAddress(WithExtension(fileName, ext), u)
	}
	return nil
}

// ---- parsing ----

// FileDescriptor is one entry of a FileGroupDescriptorW.
type FileDescriptor struct {
	// Name may contain a relative folder ("photos\a.jpg").
	Name string
	// Size is -1 when the source doesn't say it.
	Size     int64
	IsFolder bool
}

// FILEDESCRIPTORW: flags, clsid, sizel, pointl, attributes, three FILETIMEs,
// size high/low, cFileName[MAX_PATH] (UTF-16) = 592 bytes, after a 4-byte count.
const (
	descriptorSize   = 592
	sizeHighOffset   = 64
	attributesOffset = 36
	nameOffset       = 72
	nameChars        = 260
	fdAttributes     = 0x4
	fdFileSize       = 0x40
	attrDirectory    = 0x10
)

// ParseFileGroupDescriptor reads the entries; empty when the data is cut short or makes no sense.
func ParseFileGroupDescriptor(data []byte) []FileDescriptor {
	if len(data) < 4 {
		return nil
	}
	count := binary.LittleEndian.Uint32(data)
	if count == 0 || count > 10_000 || int64(len(data)) < 4+int64(count)*descriptorSize {
		return nil
	}
	var out []FileDescriptor
	for i := 0; i < int(count); i++ {
		e := data[4+i*descriptorSize : 4+(i+1)*descriptorSize]
		flags := binary.LittleEndian.Uint32(e)
		u := make([]uint16, nameChars)
		for k := range u {
			u[k] = binary.LittleEndian.Uint16(e[nameOffset+2*k:])
		}
		name := string(utf16.Decode(u))
		if z := strings.IndexByte(name, 0); z >= 0 {
			name = name[:z]
		}
		size := int64(-1)
		if flags&fdFileSize != 0 {
			size = int64(uint64(binary.LittleEndian.Uint32(e[sizeHighOffset:]))<<32 | uint64(binary.LittleEndian.Uint32(e[sizeHighOffset+4:])))
		}
		folder := flags&fdAttributes != 0 && binary.LittleEndian.Uint32(e[attributesOffset:])&attrDirectory != 0
		if name != "" {
			out = append(out, FileDescriptor{Name: name, Size: size, IsFolder: folder})
		}
	}
	return out
}

// BuildFileGroupDescriptor writes a FileGroupDescriptorW (for tests and fakes).
func BuildFileGroupDescriptor(entries []FileDescriptor) []byte {
	data := make([]byte, 4+len(entries)*descriptorSize)
	binary.LittleEndian.PutUint32(data, uint32(len(entries)))
	for i, en := range entries {
		e := data[4+i*descriptorSize:]
		flags := uint32(fdAttributes)
		if en.Size >= 0 {
			flags |= fdFileSize
		}
		binary.LittleEndian.PutUint32(e, flags)
		attr := uint32(0x80)
		if en.IsFolder {
			attr = attrDirectory
		}
		binary.LittleEndian.PutUint32(e[attributesOffset:], attr)
		if en.Size >= 0 {
			binary.LittleEndian.PutUint32(e[sizeHighOffset:], uint32(en.Size>>32))
			binary.LittleEndian.PutUint32(e[sizeHighOffset+4:], uint32(en.Size))
		}
		u := utf16.Encode([]rune(en.Name))
		if len(u) >= nameChars {
			u = u[:nameChars-1]
		}
		for k, c := range u {
			binary.LittleEndian.PutUint16(e[nameOffset+2*k:], c)
		}
	}
	return data
}

// DecodeText is the text of a drag format's bytes: UTF-16 little endian (as
// Windows and Firefox write it, with or without a BOM) or UTF-8, up to the
// first NUL (Windows pads its memory blocks).
func DecodeText(data []byte) string {
	var t string
	switch {
	case len(data) == 0:
		return ""
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE:
		t = utf16le(data[2:])
	case len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF:
		t = string(data[3:])
	case looksLikeUTF16(data):
		t = utf16le(data)
	default:
		t = string(data)
	}
	if z := strings.IndexByte(t, 0); z >= 0 {
		t = t[:z]
	}
	if !utf8.ValidString(t) {
		t = strings.ToValidUTF8(t, "�")
	}
	return t
}

func utf16le(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	return string(utf16.Decode(u))
}

// looksLikeUTF16: ASCII text in UTF-16 has a zero in every odd byte; UTF-8
// text has none. The zeros Windows pads a block with don't count.
func looksLikeUTF16(data []byte) bool {
	n := len(data)
	for n > 0 && data[n-1] == 0 {
		n--
	}
	pairs := min((n+1)/2, 64)
	if pairs == 0 {
		return false
	}
	zeros := 0
	for i := 0; i < pairs; i++ {
		if i*2+1 >= n || data[i*2+1] == 0 {
			zeros++
		}
	}
	return zeros*2 >= pairs
}

func lines(t string) []string {
	var out []string
	for _, l := range strings.FieldsFunc(t, func(r rune) bool { return r == '\r' || r == '\n' }) {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// ParseMozURL is the addresses of a text/x-moz-url or _NETSCAPE_URL value: lines alternate address and title.
func ParseMozURL(t string) []string {
	ls := lines(t)
	var out []string
	for i := 0; i < len(ls); i += 2 {
		out = append(out, ls[i])
	}
	return out
}

// ParseURIList is the addresses of a text/uri-list value (comments and blank lines skipped).
func ParseURIList(t string) []string {
	var out []string
	for _, l := range lines(t) {
		if !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out
}

// DownloadURL is a DownloadURL value: the file's media type, its name and where it is.
type DownloadURL struct {
	Mime, Name string
	URL        *url.URL
}

// ParseDownloadURL reads "image/png:photo.png:https://…", or nil.
func ParseDownloadURL(t string) *DownloadURL {
	t = strings.TrimSpace(t)
	first := strings.IndexByte(t, ':')
	if first <= 0 {
		return nil
	}
	second := strings.IndexByte(t[first+1:], ':')
	if second < 0 {
		return nil
	}
	second += first + 1
	m, name, addr := t[:first], t[first+1:second], t[second+1:]
	u, err := url.Parse(addr)
	if !strings.Contains(m, "/") || name == "" || err != nil || !u.IsAbs() {
		return nil
	}
	return &DownloadURL{Mime: m, Name: name, URL: u}
}

// ParseInternetShortcut is the URL= line of a ".url" file's contents, or "".
func ParseInternetShortcut(t string) string {
	for _, l := range lines(t) {
		if len(l) >= 4 && strings.EqualFold(l[:4], "URL=") {
			return strings.TrimSpace(l[4:])
		}
	}
	return ""
}

var (
	imageTag = regexp.MustCompile(`(?i)<img\b[^>]*?\bsrc\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	anyTag   = regexp.MustCompile(`<[^>]*>`)
	comment  = regexp.MustCompile(`(?s)<!--.*?-->`)
)

// ImageOnly is the src of a dragged HTML fragment's only <img> when the
// fragment shows no text (a selection of text with pictures isn't a
// picture), or "". Windows' "HTML Format" header is skipped.
func ImageOnly(h string) string {
	if strings.TrimSpace(h) == "" {
		return ""
	}
	lower := strings.ToLower(h)
	if s, e := strings.Index(lower, "<!--startfragment-->"), strings.Index(lower, "<!--endfragment-->"); s >= 0 && e > s {
		h = h[s+len("<!--StartFragment-->") : e]
	}
	imgs := imageTag.FindAllStringSubmatch(h, -1)
	if len(imgs) != 1 {
		return ""
	}
	text := anyTag.ReplaceAllString(comment.ReplaceAllString(h, ""), "")
	if strings.IndexFunc(html.UnescapeString(text), func(r rune) bool { return !unicode.IsSpace(r) }) >= 0 {
		return ""
	}
	src := imgs[0][1] + imgs[0][2] + imgs[0][3]
	return strings.TrimSpace(html.UnescapeString(src))
}

// DecodeDataURL is the media type and bytes of a data: URL (base64 or
// percent-encoded); false when it isn't one or holds more than maxBytes.
func DecodeDataURL(u string, maxBytes int) (string, []byte, bool) {
	if !strings.HasPrefix(strings.ToLower(u), "data:") {
		return "", nil, false
	}
	comma := strings.IndexByte(u, ',')
	if comma < 0 {
		return "", nil, false
	}
	parts := strings.Split(u[5:comma], ";")
	payload := u[comma+1:]
	m := strings.TrimSpace(parts[0])
	if m == "" {
		m = "text/plain"
	}
	isBase64 := false
	for _, p := range parts[1:] {
		if strings.EqualFold(strings.TrimSpace(p), "base64") {
			isBase64 = true
		}
	}
	un, err := url.PathUnescape(payload)
	if err != nil {
		return "", nil, false
	}
	if isBase64 {
		un = strings.NewReplacer(" ", "", "\r", "", "\n", "").Replace(un)
		if int64(len(un))*3/4 > int64(maxBytes) {
			return "", nil, false
		}
		b, err := base64.StdEncoding.DecodeString(un)
		if err != nil {
			if b, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(un, "=")); err != nil {
				return "", nil, false
			}
		}
		return m, b, true
	}
	if len(un) > maxBytes {
		return "", nil, false
	}
	return m, []byte(un), true
}
