package archive

// Microsoft Cabinet files (.cab): stored and MSZIP folders are read here;
// LZX and Quantum folders (and cabinets split over several files) need 7-Zip.
// Layout as in the Cabinet File Format specification (MS-CAB).

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// errCabNeeds7Zip: the cabinet uses a method only 7-Zip decodes.
var errCabNeeds7Zip = errors.New("needs 7-Zip")

type cabFolder struct {
	start  int64
	blocks int
	method int
}

type cabFile struct {
	name     string
	size     int64
	offset   int64
	folder   int
	modified time.Time
}

type cabSource struct {
	in       *input
	folders  []cabFolder
	files    []cabFile
	dataResv int
}

func openCab(in *input) (*cabSource, error) {
	h := make([]byte, 36)
	if _, err := in.ra.ReadAt(h, 0); err != nil || string(h[:4]) != "MSCF" {
		return nil, errNotArchive(in.name)
	}
	le := binary.LittleEndian
	coffFiles := int64(le.Uint32(h[16:]))
	nFolders, nFiles := int(le.Uint16(h[26:])), int(le.Uint16(h[28:]))
	flags := le.Uint16(h[30:])
	pos := int64(36)
	folderResv, dataResv := 0, 0
	if flags&4 != 0 {
		r := make([]byte, 4)
		if _, err := in.ra.ReadAt(r, pos); err != nil {
			return nil, errDamaged(in.name, "the cabinet ends too early")
		}
		pos += 4 + int64(le.Uint16(r))
		folderResv, dataResv = int(r[2]), int(r[3])
	}
	if flags&3 != 0 {
		return nil, fmt.Errorf("%s is part of a cabinet split over several files; open it with 7-Zip: %w", in.name, errCabNeeds7Zip)
	}
	s := &cabSource{in: in, dataResv: dataResv}
	for i := 0; i < nFolders; i++ {
		f := make([]byte, 8)
		if _, err := in.ra.ReadAt(f, pos); err != nil {
			return nil, errDamaged(in.name, "the cabinet ends too early")
		}
		pos += 8 + int64(folderResv)
		s.folders = append(s.folders, cabFolder{int64(le.Uint32(f)), int(le.Uint16(f[4:])), int(le.Uint16(f[6:]) & 0x0F)})
	}
	pos = coffFiles
	for i := 0; i < nFiles; i++ {
		f := make([]byte, 16)
		if _, err := in.ra.ReadAt(f, pos); err != nil {
			return nil, errDamaged(in.name, "the cabinet ends too early")
		}
		pos += 16
		name, n := cString(in.ra, pos, 1024)
		pos += int64(n)
		folder := int(le.Uint16(f[8:]))
		if folder >= 0xFFFD {
			return nil, fmt.Errorf("%s is part of a cabinet split over several files; open it with 7-Zip: %w", in.name, errCabNeeds7Zip)
		}
		if folder >= len(s.folders) {
			return nil, errDamaged(in.name, "invalid cabinet file entry")
		}
		attribs := le.Uint16(f[14:])
		if attribs&0x80 == 0 || !utf8.Valid(name) {
			name = []byte(legacyName(string(name)))
		}
		d, t := le.Uint16(f[10:]), le.Uint16(f[12:])
		s.files = append(s.files, cabFile{
			name: strings.ReplaceAll(string(name), `\`, "/"), size: int64(le.Uint32(f)), offset: int64(le.Uint32(f[4:])),
			folder: folder, modified: fromDosTime(uint32(d)<<16 | uint32(t)),
		})
	}
	for _, f := range s.folders {
		if f.method != 0 && f.method != 1 {
			return nil, fmt.Errorf("%s uses LZX or Quantum compression: %w", in.name, errCabNeeds7Zip)
		}
	}
	return s, nil
}

func cString(ra io.ReaderAt, pos int64, limit int) ([]byte, int) {
	buf := make([]byte, limit)
	n, _ := ra.ReadAt(buf, pos)
	if i := bytes.IndexByte(buf[:n], 0); i >= 0 {
		return buf[:i], i + 1
	}
	return buf[:n], n
}

func (s *cabSource) List() ([]member, error) {
	out := make([]member, len(s.files))
	for i, f := range s.files {
		out[i] = member{entryInfo{Path: f.name, Size: f.size}, f.modified}
	}
	return out, nil
}

func (s *cabSource) Walk(visit func(member, io.Reader) error) error {
	byFolder := map[int][]cabFile{}
	for _, f := range s.files {
		byFolder[f.folder] = append(byFolder[f.folder], f)
	}
	for fi, folder := range s.folders {
		files := byFolder[fi]
		sort.SliceStable(files, func(i, j int) bool { return files[i].offset < files[j].offset })
		r := &cabFolderReader{s: s, folder: folder, pos: folder.start}
		var at int64
		for _, f := range files {
			if f.offset < at {
				return errDamaged(s.in.name, "overlapping cabinet entries")
			}
			if _, err := io.CopyN(io.Discard, r, f.offset-at); err != nil {
				return errDamaged(s.in.name, "the cabinet ends too early")
			}
			lr := &io.LimitedReader{R: r, N: f.size}
			if err := visit(member{entryInfo{Path: f.name, Size: f.size}, f.modified}, lr); err != nil {
				return err
			}
			if lr.N > 0 {
				return errDamaged(s.in.name, "the cabinet ends too early")
			}
			at = f.offset + f.size
		}
	}
	return nil
}

func (s *cabSource) Close() error { return nil }

// cabFolderReader decodes a folder's data blocks one after the other.
type cabFolderReader struct {
	s       *cabSource
	folder  cabFolder
	pos     int64
	block   int
	pending []byte
	history []byte // the last 32 KB of output: MSZIP blocks refer back into it
}

func (r *cabFolderReader) Read(p []byte) (int, error) {
	for len(r.pending) == 0 {
		if r.block >= r.folder.blocks {
			return 0, io.EOF
		}
		if err := r.next(); err != nil {
			return 0, err
		}
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func (r *cabFolderReader) next() error {
	h := make([]byte, 8)
	if _, err := r.s.in.ra.ReadAt(h, r.pos); err != nil {
		return io.ErrUnexpectedEOF
	}
	packed := int(binary.LittleEndian.Uint16(h[4:]))
	size := int(binary.LittleEndian.Uint16(h[6:]))
	data := make([]byte, packed)
	if _, err := r.s.in.ra.ReadAt(data, r.pos+8+int64(r.s.dataResv)); err != nil {
		return io.ErrUnexpectedEOF
	}
	r.pos += 8 + int64(r.s.dataResv) + int64(packed)
	r.block++
	if r.folder.method == 0 {
		r.pending = data
		return nil
	}
	if len(data) < 2 || data[0] != 'C' || data[1] != 'K' {
		return errors.New("MSZIP block signature missing")
	}
	fr := flate.NewReaderDict(bytes.NewReader(data[2:]), r.history)
	out := make([]byte, 0, size)
	buf := bytes.NewBuffer(out)
	if _, err := io.CopyN(buf, fr, int64(size)); err != nil && err != io.EOF {
		return err
	}
	fr.Close()
	r.pending = buf.Bytes()
	r.history = append(r.history, r.pending...)
	if len(r.history) > 32<<10 {
		r.history = append([]byte(nil), r.history[len(r.history)-32<<10:]...)
	}
	return nil
}
