package archive

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/kdomanski/iso9660"
)

// ---- ISO 9660 (Rock Ridge names; Joliet and UDF need 7-Zip) ----

type isoSource struct {
	in  *input
	img *iso9660.Image
}

func openIso(in *input) (source, error) {
	img, err := iso9660.OpenImage(in.ra)
	if err != nil {
		return nil, errNotArchive(in.name)
	}
	return &isoSource{in, img}, nil
}

type isoEntry struct {
	path string
	f    *iso9660.File
}

func (s *isoSource) entries() ([]isoEntry, error) {
	root, err := s.img.RootDir()
	if err != nil {
		return nil, errNotArchive(s.in.name)
	}
	var out []isoEntry
	var walk func(dir *iso9660.File, prefix string, depth int) error
	walk = func(dir *iso9660.File, prefix string, depth int) error {
		if depth > 64 {
			return errDamaged(s.in.name, "folders nested too deep")
		}
		children, err := dir.GetChildren()
		if err != nil {
			return errDamaged(s.in.name, err.Error())
		}
		for _, c := range children {
			if len(out) > MaxEntries {
				return tooManyEntries(s.in.name)
			}
			p := prefix + c.Name()
			out = append(out, isoEntry{p, c})
			if c.IsDir() {
				if err := walk(c, p+"/", depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return out, walk(root, "", 0)
}

func isoMember(e isoEntry) member {
	return member{entryInfo{Path: e.path, Folder: e.f.IsDir(), Size: e.f.Size(), Symlink: e.f.Mode()&0o170000 == 0o120000}, e.f.ModTime()}
}

func (s *isoSource) List() ([]member, error) {
	es, err := s.entries()
	out := make([]member, 0, len(es))
	for _, e := range es {
		out = append(out, isoMember(e))
	}
	return out, err
}

func (s *isoSource) Walk(visit func(member, io.Reader) error) error {
	es, err := s.entries()
	if err != nil {
		return err
	}
	for _, e := range es {
		m := isoMember(e)
		var r io.Reader
		if !m.Folder {
			r = e.f.Reader()
		}
		if err := visit(m, r); err != nil {
			return err
		}
	}
	return nil
}

func (s *isoSource) Close() error { return nil }

// ---- CPIO (new ASCII, old ASCII and binary headers) ----

type cpioSource struct{ in *input }

func openCpio(in *input) (source, error) { return &cpioSource{in}, nil }

func (s *cpioSource) each(visit func(member, io.Reader) error) error {
	r := bufio.NewReader(s.in.reader())
	var pos int64
	read := func(n int) ([]byte, error) {
		b := make([]byte, n)
		_, err := io.ReadFull(r, b)
		pos += int64(n)
		return b, err
	}
	skip := func(n int64) error {
		_, err := io.CopyN(io.Discard, r, n)
		pos += n
		return err
	}
	pad := func(align int64) error {
		if rem := pos % align; rem != 0 {
			return skip(align - rem)
		}
		return nil
	}
	count := 0
	for {
		magic, err := r.Peek(6)
		if err != nil {
			if count == 0 {
				return errNotArchive(s.in.name)
			}
			return errDamaged(s.in.name, "the archive ends too early")
		}
		var mode, size, nameSize, mtime int64
		var align int64
		switch {
		case string(magic) == "070701" || string(magic) == "070702":
			h, err := read(110)
			if err != nil {
				return errDamaged(s.in.name, "the archive ends too early")
			}
			hex := func(i int) int64 { v, _ := strconv.ParseInt(string(h[6+i*8:14+i*8]), 16, 64); return v }
			mode, mtime, size, nameSize = hex(1), hex(5), hex(6), hex(11)
			align = 4
		case string(magic) == "070707":
			h, err := read(76)
			if err != nil {
				return errDamaged(s.in.name, "the archive ends too early")
			}
			oct := func(from, n int) int64 { v, _ := strconv.ParseInt(string(h[from:from+n]), 8, 64); return v }
			mode, mtime, nameSize, size = oct(18, 6), oct(48, 11), oct(59, 6), oct(65, 11)
			align = 1
		case magic[0] == 0xC7 && magic[1] == 0x71 || magic[0] == 0x71 && magic[1] == 0xC7:
			h, err := read(26)
			if err != nil {
				return errDamaged(s.in.name, "the archive ends too early")
			}
			var bo binary.ByteOrder = binary.LittleEndian
			if h[0] == 0x71 {
				bo = binary.BigEndian
			}
			u16 := func(i int) int64 { return int64(bo.Uint16(h[i:])) }
			mode = u16(6)
			mtime = u16(16)<<16 | u16(18)
			nameSize = u16(20)
			size = u16(22)<<16 | u16(24)
			align = 2
		default:
			if count == 0 {
				return errNotArchive(s.in.name)
			}
			return errDamaged(s.in.name, "unknown CPIO header")
		}
		count++
		nameBytes, err := read(int(nameSize))
		if err != nil {
			return errDamaged(s.in.name, "the archive ends too early")
		}
		if err := pad(align); err != nil {
			return errDamaged(s.in.name, "the archive ends too early")
		}
		name := legacyName(strings.TrimRight(string(nameBytes), "\x00"))
		if name == "TRAILER!!!" {
			return nil
		}
		name = strings.TrimPrefix(strings.TrimPrefix(name, "./"), "/")
		m := member{entryInfo{Path: name, Size: size}, time.Unix(mtime, 0)}
		kind := mode & 0o170000
		ok := true
		switch kind {
		case 0o040000:
			m.Folder = true
		case 0o100000:
		case 0o120000:
			m.Symlink = true
		default:
			ok = false
		}
		lr := &io.LimitedReader{R: r, N: size}
		if ok && name != "" && name != "." {
			if err := visit(m, lr); err != nil {
				return err
			}
		}
		rest := lr.N
		pos += size - rest
		if err := skip(rest); err != nil {
			return errDamaged(s.in.name, "the archive ends too early")
		}
		if err := pad(align); err != nil {
			return nil
		}
	}
}

func (s *cpioSource) List() ([]member, error) {
	var out []member
	err := s.each(func(m member, _ io.Reader) error {
		if len(out) > MaxEntries {
			return tooManyEntries(s.in.name)
		}
		out = append(out, m)
		return nil
	})
	return out, err
}

func (s *cpioSource) Walk(visit func(member, io.Reader) error) error { return s.each(visit) }
func (s *cpioSource) Close() error                                  { return nil }

// ---- ar (DEB packages: debian-binary, control.tar.*, data.tar.*) ----

type arSource struct{ in *input }

func openAr(in *input) (source, error) {
	head := make([]byte, 8)
	if _, err := in.ra.ReadAt(head, 0); err != nil || string(head) != "!<arch>\n" {
		return nil, errNotArchive(in.name)
	}
	return &arSource{in}, nil
}

func (s *arSource) each(visit func(member, io.Reader) error) error {
	pos := int64(8)
	var longNames []byte
	for pos+60 <= s.in.size {
		h := make([]byte, 60)
		if _, err := s.in.ra.ReadAt(h, pos); err != nil {
			return errDamaged(s.in.name, "the archive ends too early")
		}
		if h[58] != '`' || h[59] != '\n' {
			return errDamaged(s.in.name, "invalid ar header")
		}
		name := strings.TrimSpace(string(h[0:16]))
		mtime, _ := strconv.ParseInt(strings.TrimSpace(string(h[16:28])), 10, 64)
		size, err := strconv.ParseInt(strings.TrimSpace(string(h[48:58])), 10, 64)
		if err != nil || size < 0 || pos+60+size > s.in.size {
			return errDamaged(s.in.name, "invalid ar header")
		}
		data := io.NewSectionReader(s.in.ra, pos+60, size)
		pos += 60 + size + size%2
		switch {
		case name == "//": // GNU long name table
			longNames = make([]byte, size)
			data.ReadAt(longNames, 0)
			continue
		case name == "/" || name == "/SYM64/":
			continue // symbol tables
		case strings.HasPrefix(name, "#1/"): // BSD long name in front of the data
			n, _ := strconv.Atoi(name[3:])
			if n < 0 || int64(n) > size {
				return errDamaged(s.in.name, "invalid ar header")
			}
			b := make([]byte, n)
			data.ReadAt(b, 0)
			name = strings.TrimRight(string(b), "\x00")
			data = io.NewSectionReader(s.in.ra, pos-size-size%2+int64(n), size-int64(n))
			size -= int64(n)
		case strings.HasPrefix(name, "/") && longNames != nil:
			off, _ := strconv.Atoi(name[1:])
			if off >= 0 && off < len(longNames) {
				end := bytes.IndexByte(longNames[off:], '\n')
				if end < 0 {
					end = len(longNames) - off
				}
				name = string(longNames[off : off+end])
			}
		}
		name = strings.TrimSuffix(name, "/")
		if err := visit(member{entryInfo{Path: name, Size: size}, time.Unix(mtime, 0)}, data); err != nil {
			return err
		}
	}
	return nil
}

func (s *arSource) List() ([]member, error) {
	var out []member
	err := s.each(func(m member, _ io.Reader) error { out = append(out, m); return nil })
	return out, err
}

func (s *arSource) Walk(visit func(member, io.Reader) error) error { return s.each(visit) }
func (s *arSource) Close() error                                  { return nil }

// ---- RPM: the payload after the lead, signature and header ----

// rpmPayload returns the offset of an RPM's payload and its file extension
// (cpio.gz, cpio.xz, cpio.zst …), as 7-Zip shows it: one compressed CPIO file.
func rpmPayload(in *input) (int64, string, error) {
	lead := make([]byte, 96)
	if _, err := in.ra.ReadAt(lead, 0); err != nil || !bytes.Equal(lead[:4], []byte{0xED, 0xAB, 0xEE, 0xDB}) {
		return 0, "", errNotArchive(in.name)
	}
	pos := int64(96)
	for i := 0; i < 2; i++ {
		h := make([]byte, 16)
		if _, err := in.ra.ReadAt(h, pos); err != nil || !bytes.Equal(h[:3], []byte{0x8E, 0xAD, 0xE8}) {
			return 0, "", errDamaged(in.name, "invalid RPM header")
		}
		entries := int64(binary.BigEndian.Uint32(h[8:]))
		dataSize := int64(binary.BigEndian.Uint32(h[12:]))
		pos += 16 + entries*16 + dataSize
		if i == 0 && pos%8 != 0 { // the signature is padded to 8 bytes
			pos += 8 - pos%8
		}
	}
	if pos >= in.size {
		return 0, "", errDamaged(in.name, "the package has no payload")
	}
	magic := make([]byte, 6)
	in.ra.ReadAt(magic, pos)
	ext := "cpio"
	switch sniff(magic) {
	case "gz":
		ext = "cpio.gz"
	case "bz2":
		ext = "cpio.bz2"
	case "xz":
		ext = "cpio.xz"
	case "zst":
		ext = "cpio.zst"
	case "lzma":
		ext = "cpio.lzma"
	}
	return pos, ext, nil
}

// ---- Unix compress (.Z): LZW with 9–16 bit codes and block mode ----

type lzwZReader struct {
	r        *bufio.Reader
	maxBits  int
	block    bool
	bits     int
	buf      uint64
	nbuf     int
	prefix   []int32
	suffix   []byte
	next     int
	prev     int
	first    byte
	out      []byte
	pending  []byte
	codesIn  int // codes read with the current width (codes come in groups of 8)
	finished bool
}

var errBadZ = errors.New("not compressed data, or damaged")

func newZReader(r io.Reader) (io.Reader, error) {
	br := bufio.NewReader(r)
	var h [3]byte
	if _, err := io.ReadFull(br, h[:]); err != nil || h[0] != 0x1F || h[1] != 0x9D {
		return nil, errBadZ
	}
	z := &lzwZReader{r: br, maxBits: int(h[2] & 0x1F), block: h[2]&0x80 != 0, bits: 9, prev: -1}
	if z.maxBits < 9 || z.maxBits > 16 {
		return nil, errBadZ
	}
	z.prefix = make([]int32, 1<<z.maxBits)
	z.suffix = make([]byte, 1<<z.maxBits)
	for i := 0; i < 256; i++ {
		z.suffix[i] = byte(i)
		z.prefix[i] = -1
	}
	z.reset()
	return z, nil
}

func (z *lzwZReader) reset() {
	z.next = 256
	if z.block {
		z.next = 257
	}
	z.bits = 9
	z.prev = -1
}

func (z *lzwZReader) code() (int, bool) {
	for z.nbuf < z.bits {
		b, err := z.r.ReadByte()
		if err != nil {
			return 0, false
		}
		z.buf |= uint64(b) << z.nbuf
		z.nbuf += 8
	}
	c := int(z.buf & (1<<z.bits - 1))
	z.buf >>= z.bits
	z.nbuf -= z.bits
	z.codesIn++
	return c, true
}

// skipGroup: compress writes codes in groups of 8 of one width; a width change
// (or a clear) drops the rest of the group.
func (z *lzwZReader) skipGroup() {
	for z.codesIn%8 != 0 {
		if _, ok := z.code(); !ok {
			return
		}
	}
	z.buf, z.nbuf = 0, 0
	z.codesIn = 0
}

func (z *lzwZReader) Read(p []byte) (int, error) {
	for len(z.pending) == 0 {
		if z.finished {
			return 0, io.EOF
		}
		c, ok := z.code()
		if !ok {
			z.finished = true
			return 0, io.EOF
		}
		if z.block && c == 256 {
			z.skipGroup()
			z.reset()
			continue
		}
		var entry []byte
		switch {
		case z.prev < 0:
			if c >= 256 {
				return 0, errBadZ
			}
			entry = []byte{byte(c)}
		case c < z.next:
			entry = z.expand(c)
		case c == z.next:
			entry = append(z.expand(z.prev), z.first)
		default:
			return 0, errBadZ
		}
		if len(entry) == 0 {
			return 0, errBadZ
		}
		if z.prev >= 0 && z.next < 1<<z.maxBits {
			z.prefix[z.next] = int32(z.prev)
			z.suffix[z.next] = entry[0]
			z.next++
			if z.next >= 1<<z.bits && z.bits < z.maxBits {
				z.skipGroup()
				z.bits++
			}
		}
		z.prev = c
		z.first = entry[0]
		z.pending = entry
	}
	n := copy(p, z.pending)
	z.pending = z.pending[n:]
	return n, nil
}

func (z *lzwZReader) expand(c int) []byte {
	z.out = z.out[:0]
	for i := 0; c >= 0 && i <= 1<<16; i++ {
		z.out = append(z.out, z.suffix[c])
		if c < 256 {
			break
		}
		c = int(z.prefix[c])
	}
	for i, j := 0, len(z.out)-1; i < j; i, j = i+1, j-1 {
		z.out[i], z.out[j] = z.out[j], z.out[i]
	}
	return append([]byte(nil), z.out...)
}

// ---- format sniffing ----

// sniff names the format of a file from its first bytes (at least 262 for TAR), or "".
func sniff(b []byte) string {
	has := func(off int, sig string) bool { return len(b) >= off+len(sig) && string(b[off:off+len(sig)]) == sig }
	switch {
	case has(0, "PK\x03\x04"), has(0, "PK\x05\x06"), has(0, "PK\x07\x08"):
		return "zip"
	case has(0, "7z\xBC\xAF\x27\x1C"):
		return "7z"
	case has(0, "Rar!\x1A\x07"):
		return "rar"
	case has(0, "\x1F\x8B"):
		return "gz"
	case has(0, "BZh"):
		return "bz2"
	case has(0, "\xFD7zXZ\x00"):
		return "xz"
	case has(0, "\x28\xB5\x2F\xFD"):
		return "zst"
	case has(0, "LZIP"):
		return "lz"
	case has(0, "\x1F\x9D"):
		return "z"
	case has(0, "MSCF"):
		return "cab"
	case has(0, "!<arch>\n"):
		return "deb"
	case has(0, "\xED\xAB\xEE\xDB"):
		return "rpm"
	case has(0, "070701"), has(0, "070702"), has(0, "070707"), has(0, "\xC7\x71"), has(0, "\x71\xC7"):
		return "cpio"
	case has(0, "ALZ\x01"):
		return "alz"
	case has(0, "EGGA"):
		return "egg"
	case has(257, "ustar"):
		return "tar"
	case has(0, "\x60\xEA"):
		return "arj"
	case has(2, "-lh"), has(2, "-lz"):
		return "lha"
	case len(b) >= 13 && b[0] == 0x5D && b[1] == 0 && b[2] == 0:
		return "lzma"
	}
	return ""
}
