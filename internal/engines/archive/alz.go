package archive

// ALZ archives, the format of the Korean ALZip (ESTsoft), which 7-Zip does not
// read. Layout as documented by unalz (zlib licence) and the "unalz" Rust
// crate (MIT); this reader is Filee's own:
//
//	"ALZ\x01" + 4 bytes                          archive header
//	"BLZ\x01" + local file header + name + data  one per entry
//	"CLZ\x01" + 12 bytes                         central directory marker
//	"CLZ\x02" (+ 12 bytes of end info)           end of archive; "ELZ\x01" is a comment section
//
// Local file header (little endian): name length u16, attributes u8 (0x10 =
// folder), DOS time u32, descriptor u8 (bit 0 = encrypted, bits 4-7 = width
// of the size fields: 0x10/0x20/0x40/0x80 = 1/2/4/8 bytes), unknown u8; then,
// when the width is not 0: method u8 (0 store, 1 ALZ bzip2, 2 deflate),
// unknown u8, CRC-32 u32, packed size, unpacked size. Names are CP949
// (Korean Windows) unless they are valid UTF-8.

import (
	"bufio"
	"compress/flate"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/korean"
)

const (
	alzArchive   = 0x015A4C41 // "ALZ\x01"
	alzEntry     = 0x015A4C42 // "BLZ\x01"
	alzDirectory = 0x015A4C43 // "CLZ\x01"
	alzEnd       = 0x025A4C43 // "CLZ\x02"
	alzSplitMark = 0x035A4C43 // "CLZ\x03"
	alzComment   = 0x015A4C45 // "ELZ\x01"
	alzSplit     = 0x015A4C53 // "SLZ\x01"
)

type alzItem struct {
	name               string
	folder             bool
	modified           time.Time
	method             int
	crc                uint32
	packed, size, data int64
}

// extractAlz unpacks every entry of archive into target.
func extractAlz(ctx context.Context, archive, target string) error {
	name := filepath.Base(archive)
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	entries, err := readAlzEntries(f, name)
	if err != nil {
		return err
	}
	var total int64
	for _, e := range entries {
		if !e.folder {
			total += e.size
		}
	}
	if err := ensureRoom(total, target, name); err != nil {
		return err
	}

	type folderTime struct {
		path string
		t    time.Time
	}
	var folders []folderTime
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		path, err := resolveEntryPath(target, e.name, name)
		if err != nil {
			return err
		}
		if e.folder {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
			folders = append(folders, folderTime{path, e.modified})
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		path = uniquePath(path)
		if err := writeAlzEntry(ctx, f, e, path, name); err != nil {
			return err
		}
		setTime(path, e.modified)
	}
	// Folder times last: writing files into a folder changes its time.
	for _, ft := range folders {
		setTime(ft.path, ft.t)
	}
	return nil
}

func writeAlzEntry(ctx context.Context, f *os.File, e alzItem, path, name string) error {
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	crc := crc32.NewIEEE()
	w := io.MultiWriter(out, crc)
	packed := io.NewSectionReader(f, e.data, e.packed)
	var n int64
	switch e.method {
	case 0:
		n, err = copyLimited(ctx, w, packed, e.size, nil)
	case 1:
		n, err = decompressAlzBzip2(ctx, bufio.NewReader(packed), w, e.size)
	case 2:
		fr := flate.NewReader(packed)
		n, err = copyLimited(ctx, w, fr, e.size, nil)
		fr.Close()
	default:
		err = fmt.Errorf("%s uses an unknown ALZ compression method (%d)", name, e.method)
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		if err == errTooMuchData || err == errBzEnd || isDataError(err) {
			return errDamaged(name, "checksum error in "+e.name)
		}
		return err
	}
	if n != e.size || crc.Sum32() != e.crc {
		os.Remove(path)
		return errDamaged(name, "checksum error in "+e.name)
	}
	return nil
}

func isDataError(err error) bool {
	_, ok := err.(flate.CorruptInputError)
	return ok || err == io.ErrUnexpectedEOF
}

func readAlzEntries(f *os.File, name string) ([]alzItem, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()
	r := &binReader{r: f}
	if size < 8 || r.u32() != alzArchive {
		return nil, fmt.Errorf("%s is not an ALZ archive", name)
	}
	r.u32() // version / unknown
	var entries []alzItem
	for r.err == nil && r.pos+4 <= size {
		switch r.u32() {
		case alzEntry:
			if len(entries) >= MaxEntries {
				return nil, tooManyEntries(name)
			}
			e, err := readAlzEntry(r, size, name)
			if err != nil {
				return nil, err
			}
			entries = append(entries, e)
		case alzDirectory:
			r.skip(12)
		case alzSplitMark:
		case alzComment:
			// Its size is in the 12 bytes after the end record (and includes the signature).
			at := r.pos
			r.seek(size - 12)
			n := r.u32()
			r.seek(at + max(0, int64(n)-4))
		case alzEnd:
			return entries, nil
		case alzSplit:
			return nil, fmt.Errorf("%s is part of a split ALZ archive (.alz + .a00 …); Filee cannot unpack split archives", name)
		default:
			if r.err != nil {
				break
			}
			return nil, errDamaged(name, "unknown ALZ header")
		}
	}
	if r.err != nil {
		return nil, errDamaged(name, "the archive ends too early")
	}
	return entries, nil // no end record: keep what was found, the data checks catch damage
}

func readAlzEntry(r *binReader, size int64, name string) (alzItem, error) {
	nameLen := int(r.u16())
	attributes := r.u8()
	dosTime := r.u32()
	descriptor := r.u8()
	r.u8()
	if descriptor&1 != 0 {
		return alzItem{}, errEncrypted(name)
	}
	var width int
	switch descriptor & 0xF0 {
	case 0:
		width = 0
	case 0x10:
		width = 1
	case 0x20:
		width = 2
	case 0x40:
		width = 4
	case 0x80:
		width = 8
	default:
		return alzItem{}, errDamaged(name, "invalid ALZ size field")
	}
	e := alzItem{folder: attributes&0x10 != 0, modified: fromDosTime(dosTime)}
	if width > 0 {
		e.method = int(r.u8())
		r.u8()
		e.crc = r.u32()
		e.packed = r.uintN(width)
		e.size = r.uintN(width)
	}
	if r.err != nil {
		return alzItem{}, errDamaged(name, "the archive ends too early")
	}
	if nameLen == 0 || e.packed < 0 || e.size < 0 {
		return alzItem{}, errDamaged(name, "invalid ALZ entry")
	}
	e.name = decodeName(r.bytes(nameLen))
	e.data = r.pos
	if r.err != nil || e.data+e.packed > size {
		return alzItem{}, errDamaged(name, "the archive ends too early")
	}
	r.skip(e.packed)
	return e, nil
}

// decodeName: CP949 names as Korean Windows writes them, UTF-8 when the bytes are valid UTF-8.
func decodeName(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	if s, err := korean.EUCKR.NewDecoder().Bytes(b); err == nil {
		return string(s)
	}
	return string(b)
}

// fromDosTime is a DOS date and time (local time, 2-second steps), zero when invalid.
func fromDosTime(v uint32) time.Time {
	sec, minute, hour := int(v&0x1F)*2, int(v>>5)&0x3F, int(v>>11)&0x1F
	day, month, year := int(v>>16)&0x1F, int(v>>21)&0x0F, 1980+int(v>>25)
	if month < 1 || month > 12 || day < 1 || hour > 23 || minute > 59 || sec > 59 {
		return time.Time{}
	}
	t := time.Date(year, time.Month(month), day, hour, minute, sec, 0, time.Local)
	if t.Day() != day {
		return time.Time{}
	}
	return t
}

// binReader reads little-endian numbers from a seekable file, remembering the first error.
type binReader struct {
	r   io.ReadSeeker
	pos int64
	err error
	buf [8]byte
}

func (b *binReader) read(n int) []byte {
	if b.err != nil {
		return b.buf[:n:n]
	}
	if _, err := io.ReadFull(b.r, b.buf[:n]); err != nil {
		b.err = err
		clear(b.buf[:])
	}
	b.pos += int64(n)
	return b.buf[:n]
}

func (b *binReader) u8() byte    { return b.read(1)[0] }
func (b *binReader) u16() uint16 { return binary.LittleEndian.Uint16(b.read(2)) }
func (b *binReader) u32() uint32 { return binary.LittleEndian.Uint32(b.read(4)) }
func (b *binReader) u64() uint64 { return binary.LittleEndian.Uint64(b.read(8)) }

func (b *binReader) uintN(width int) int64 {
	var v [8]byte
	copy(v[:], b.read(width))
	return int64(binary.LittleEndian.Uint64(v[:]))
}

func (b *binReader) bytes(n int) []byte {
	out := make([]byte, n)
	if b.err != nil {
		return out
	}
	if _, err := io.ReadFull(b.r, out); err != nil {
		b.err = err
	}
	b.pos += int64(n)
	return out
}

func (b *binReader) skip(n int64) { b.seek(b.pos + n) }

func (b *binReader) seek(p int64) {
	if b.err != nil {
		return
	}
	if _, err := b.r.Seek(p, io.SeekStart); err != nil {
		b.err = err
	}
	b.pos = p
}
