package archive

// lzip files (.lz, .tar.lz), which 7-Zip does not open. An lzip member is a
// 6-byte header ("LZIP", version 1, coded dictionary size), a raw LZMA stream
// (lc=3, lp=0, pb=2, always ended by an end marker) and a 20-byte trailer
// (CRC-32 u32, data size u64, member size u64). Each member is read as a
// classic .lzma stream; the result is checked against the trailer.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"

	"github.com/ulikunitz/xz/lzma"
)

const (
	lzipHeader  = 6
	lzipTrailer = 20
)

type lzipMember struct {
	start, size, dataSize int64
	crc                   uint32
}

// decompressLzip decompresses lzipFile into outputFile.
func decompressLzip(ctx context.Context, lzipFile, outputFile string) (err error) {
	name := filepath.Base(lzipFile)
	in, err := os.Open(lzipFile)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	members, err := findLzipMembers(in, st.Size(), name)
	if err != nil {
		return err
	}
	var total int64
	for _, m := range members {
		total += m.dataSize
	}
	if err := ensureRoom(total, filepath.Dir(outputFile), name); err != nil {
		return err
	}
	out, err := os.OpenFile(outputFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			out.Close()
		}
		if err != nil {
			os.Remove(outputFile)
		}
	}()
	for _, m := range members {
		if err := ctx.Err(); err != nil {
			return err
		}
		var coded [1]byte
		if _, err := in.ReadAt(coded[:], m.start+5); err != nil {
			return invalidLzip(name)
		}
		// .lzma header: properties (lc=3, lp=0, pb=2), dictionary size, unknown size (-1: until the end marker).
		var head [13]byte
		head[0] = 0x5D
		binary.LittleEndian.PutUint32(head[1:], lzipDictionarySize(int(coded[0])))
		binary.LittleEndian.PutUint64(head[5:], ^uint64(0))
		body := io.NewSectionReader(in, m.start+lzipHeader, m.size-lzipHeader-lzipTrailer)
		lr, err := lzma.NewReader(io.MultiReader(bytes.NewReader(head[:]), bufio.NewReader(body)))
		if err != nil {
			return errDamaged(name, "LZMA data could not be decoded")
		}
		crc := crc32.NewIEEE()
		n, err := copyLimited(ctx, io.MultiWriter(out, crc), lr, m.dataSize, nil)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errDamaged(name, "LZMA data could not be decoded")
		}
		if n != m.dataSize {
			return errDamaged(name, "wrong data size")
		}
		if crc.Sum32() != m.crc {
			return errDamaged(name, "checksum error")
		}
	}
	closed = true
	if err := out.Close(); err != nil {
		return err
	}
	setTime(outputFile, st.ModTime())
	return nil
}

// findLzipMembers finds the members from the end backwards (every trailer
// ends with its member's size), as lzip does.
func findLzipMembers(in io.ReaderAt, size int64, name string) ([]lzipMember, error) {
	var members []lzipMember
	end := size
	var trailer [lzipTrailer]byte
	var header [5]byte
	for end > 0 {
		if end < lzipHeader+lzipTrailer {
			return nil, invalidLzip(name)
		}
		if _, err := in.ReadAt(trailer[:], end-lzipTrailer); err != nil {
			return nil, invalidLzip(name)
		}
		m := lzipMember{
			crc:      binary.LittleEndian.Uint32(trailer[0:]),
			dataSize: int64(binary.LittleEndian.Uint64(trailer[4:])),
			size:     int64(binary.LittleEndian.Uint64(trailer[12:])),
		}
		if m.size < lzipHeader+lzipTrailer || m.size > end || m.dataSize < 0 {
			return nil, invalidLzip(name)
		}
		m.start = end - m.size
		if _, err := in.ReadAt(header[:], m.start); err != nil || string(header[:4]) != "LZIP" || header[4] != 1 {
			return nil, invalidLzip(name)
		}
		members = append([]lzipMember{m}, members...)
		end = m.start
	}
	if len(members) == 0 {
		return nil, invalidLzip(name)
	}
	return members, nil
}

// lzipDictionarySize decodes the header's dictionary size: bits 0-4 are the
// base-2 logarithm, bits 5-7 subtract that many sixteenths.
func lzipDictionarySize(coded int) uint32 {
	size := uint32(1) << (coded & 0x1F)
	return size - size/16*uint32((coded>>5)&7)
}

func invalidLzip(name string) error {
	return fmt.Errorf("%s is not a valid lzip file, or it is damaged", name)
}
