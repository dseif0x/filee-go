package archive

// EGG archives, ALZip's newer format, which 7-Zip does not read. Layout as
// documented by EggDotNet (MIT) and unegg (MIT); this reader is Filee's own.
// All numbers are little endian.
//
//	"EGGA" version u16, header id u32, reserved u32, extra fields…, END      archive header
//	FILE (0x0A8590E3) file id u32, size u64, extra fields…, END              one per entry, followed by
//	BLOCK (0x02B50C13) method u8, hint u8, size u32, packed u32, CRC-32 u32, END, data   one or more blocks
//	END (0x08E28222)                                                         end of archive
//
// An extra field is magic u32, flags u8, size u16 (u32 when flags bit 0 is
// set) and data. The file name field has a locale u16 before the name when
// flags & 0x08 (else the name is UTF-8) and a parent id u32 when flags &
// 0x10. Methods: 0 store, 1 deflate, 2 bzip2, 3 AZO (ESTsoft's own, not
// supported), 4 LZMA (ZIP-style: 4-byte version and size header, 5 property
// bytes, raw data). Solid, split and encrypted archives are refused.

import (
	"bufio"
	"bytes"
	"compress/bzip2"
	"compress/flate"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ulikunitz/xz/lzma"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

const (
	eggArchive  = 0x41474745 // "EGGA"
	eggEnd      = 0x08E28222
	eggFile     = 0x0A8590E3
	eggBlock    = 0x02B50C13
	eggFileName = 0x0A8591AC
	eggWinInfo  = 0x2C86950B
	eggPosix    = 0x1EE922E5
	eggEncrypt  = 0x08D1470F
	eggSplit    = 0x24F5A262
	eggSolid    = 0x24E5A060
)

type eggBlockInfo struct {
	method             int
	size, packed, data int64
	crc                uint32
}

type eggItem struct {
	name     string
	size     int64
	folder   bool
	modified time.Time
	blocks   []eggBlockInfo
}

func extractEgg(ctx context.Context, archive, target string) error {
	name := filepath.Base(archive)
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	entries, err := readEggEntries(f, name)
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
		out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		var written int64
		for _, b := range e.blocks {
			crc := crc32.NewIEEE()
			n, err := decodeEggBlock(ctx, f, b, io.MultiWriter(out, crc), name)
			if err == nil && (n != b.size || crc.Sum32() != b.crc) {
				err = errDamaged(name, "checksum error in "+e.name)
			}
			if err != nil {
				out.Close()
				os.Remove(path)
				return err
			}
			written += n
		}
		if err := out.Close(); err != nil {
			return err
		}
		if written != e.size {
			os.Remove(path)
			return errDamaged(name, "wrong size of "+e.name)
		}
		setTime(path, e.modified)
	}
	for _, ft := range folders {
		setTime(ft.path, ft.t)
	}
	return nil
}

func decodeEggBlock(ctx context.Context, f *os.File, b eggBlockInfo, out io.Writer, name string) (int64, error) {
	packed := io.NewSectionReader(f, b.data, b.packed)
	var n int64
	var err error
	switch b.method {
	case 0:
		n, err = copyLimited(ctx, out, packed, b.size, nil)
	case 1:
		fr := flate.NewReader(packed)
		n, err = copyLimited(ctx, out, fr, b.size, nil)
		fr.Close()
	case 2:
		n, err = copyLimited(ctx, out, bzip2.NewReader(bufio.NewReader(packed)), b.size, nil)
	case 3:
		return 0, fmt.Errorf("%s is compressed with ALZip's own AZO method, which Filee cannot unpack. Save it again in ALZip as ZIP or 7Z, or as EGG with another method", name)
	case 4:
		// ZIP's LZMA header (version u16, property size u16, 5 property bytes) rewritten as a
		// classic .lzma header (properties, dictionary size, unpacked size).
		var head [9]byte
		if _, err := io.ReadFull(packed, head[:]); err != nil {
			return 0, errDamaged(name, "LZMA data could not be decoded")
		}
		var classic [13]byte
		copy(classic[:5], head[4:9])
		binary.LittleEndian.PutUint64(classic[5:], uint64(b.size))
		lr, lerr := lzma.NewReader(io.MultiReader(bytes.NewReader(classic[:]), bufio.NewReader(packed)))
		if lerr != nil {
			return 0, errDamaged(name, "LZMA data could not be decoded")
		}
		n, err = copyLimited(ctx, out, lr, b.size, nil)
	default:
		return 0, fmt.Errorf("%s uses an unknown EGG compression method (%d)", name, b.method)
	}
	if err != nil && err != ctx.Err() {
		return n, errDamaged(name, "the data cannot be decoded")
	}
	return n, err
}

func readEggEntries(f *os.File, name string) ([]*eggItem, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()
	r := &binReader{r: f}
	if size < 14 || r.u32() != eggArchive {
		return nil, fmt.Errorf("%s is not an EGG archive", name)
	}
	r.u16()
	r.u32()
	r.u32()
	if err := readEggExtra(r, name, nil); err != nil {
		return nil, err
	}
	var entries []*eggItem
	var current *eggItem
	for r.err == nil && r.pos+4 <= size {
		magic := r.u32()
		switch {
		case magic == eggFile:
			if len(entries) >= MaxEntries {
				return nil, tooManyEntries(name)
			}
			current = &eggItem{}
			r.u32()
			current.size = int64(r.u64())
			if err := readEggExtra(r, name, current); err != nil {
				return nil, err
			}
			entries = append(entries, current)
		case magic == eggBlock && current != nil:
			b := eggBlockInfo{method: int(r.u8())}
			r.u8()
			b.size = int64(r.u32())
			b.packed = int64(r.u32())
			b.crc = r.u32()
			if r.u32() != eggEnd && r.err == nil {
				return nil, errDamaged(name, "invalid EGG block header")
			}
			b.data = r.pos
			if r.pos+b.packed > size {
				return nil, errDamaged(name, "the archive ends too early")
			}
			current.blocks = append(current.blocks, b)
			r.skip(b.packed)
		case magic == eggEnd:
			return validateEgg(entries, name)
		default:
			// Comments and other extra fields between entries.
			flags := r.u8()
			n := eggFieldSize(r, flags)
			if r.pos+n > size {
				return nil, errDamaged(name, "unknown EGG header")
			}
			r.skip(n)
		}
	}
	if r.err != nil {
		return nil, errDamaged(name, "the archive ends too early")
	}
	return validateEgg(entries, name)
}

func validateEgg(entries []*eggItem, name string) ([]*eggItem, error) {
	for _, e := range entries {
		if e.name == "" {
			return nil, errDamaged(name, "an entry has no name")
		}
		if strings.HasSuffix(e.name, "/") || strings.HasSuffix(e.name, `\`) {
			e.folder = true
		}
		var sum int64
		for _, b := range e.blocks {
			sum += b.size
		}
		if !e.folder && sum != e.size {
			return nil, errDamaged(name, "the blocks of "+e.name+" do not add up")
		}
	}
	return entries, nil
}

func eggFieldSize(r *binReader, flags byte) int64 {
	if flags&1 != 0 {
		return int64(r.u32())
	}
	return int64(r.u16())
}

// readEggExtra reads extra fields up to END, filling entry (nil: the archive header).
func readEggExtra(r *binReader, name string, entry *eggItem) error {
	for {
		magic := r.u32()
		if r.err != nil {
			return errDamaged(name, "the archive ends too early")
		}
		if magic == eggEnd {
			return nil
		}
		flags := r.u8()
		switch {
		case magic == eggSplit:
			return fmt.Errorf("%s is part of a split EGG archive; Filee cannot unpack split archives", name)
		case magic == eggSolid:
			return fmt.Errorf("%s is a solid EGG archive, which Filee cannot unpack", name)
		case magic == eggEncrypt:
			return errEncrypted(name)
		case magic == eggFileName && entry != nil:
			n := eggFieldSize(r, flags)
			if flags&0x04 != 0 {
				return errEncrypted(name)
			}
			var enc encoding.Encoding
			if flags&0x08 != 0 {
				enc = codePage(int(r.u16()))

			}
			if flags&0x10 != 0 {
				r.u32() // parent path id; names already hold the full path

			}
			raw := r.bytes(int(max(0, n)))
			if enc != nil {
				if s, err := enc.NewDecoder().Bytes(raw); err == nil {
					raw = s
				}
			}
			entry.name = string(raw)
		case magic == eggWinInfo && entry != nil:
			n := eggFieldSize(r, flags)
			fileTime := int64(r.u64())
			attributes := r.u8()
			r.skip(n - 9)
			entry.modified = fromFileTime(fileTime)
			entry.folder = entry.folder || attributes&0x10 != 0
		case magic == eggPosix && entry != nil:
			n := eggFieldSize(r, flags)
			mode := r.u32()
			r.skip(n - 4)
			entry.folder = entry.folder || mode&0xF000 == 0x4000
		default:
			r.skip(eggFieldSize(r, flags))
		}
	}
}

// fromFileTime is a Windows FILETIME (100 ns since 1601), zero when unset.
func fromFileTime(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	const epochDiff = 116444736000000000
	return time.Unix(0, 0).Add(time.Duration(v-epochDiff) * 100).UTC()
}

// codePage is the decoder of a Windows code page, CP949 when unknown.
func codePage(cp int) encoding.Encoding {
	switch cp {
	case 65001:
		return nil
	case 932:
		return japanese.ShiftJIS
	case 936:
		return simplifiedchinese.GBK
	case 950:
		return traditionalchinese.Big5
	case 1250:
		return charmap.Windows1250
	case 1251:
		return charmap.Windows1251
	case 1252:
		return charmap.Windows1252
	case 437:
		return charmap.CodePage437
	case 866:
		return charmap.CodePage866
	}
	return korean.EUCKR
}
