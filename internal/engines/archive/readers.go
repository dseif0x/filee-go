package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/bodgit/sevenzip"
	"github.com/nwaples/rardecode/v2"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/korean"

	"github.com/dseif0x/filee-go/internal/doc"
)

// input is an archive's bytes: one file or the joined volumes of a split archive.
type input struct {
	ra   io.ReaderAt
	size int64
	name string // file name for messages
	path string // the first volume on disk
	c    io.Closer
}

func openInput(p string) (*input, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &input{ra: f, size: st.Size(), name: baseName(p), path: p, c: f}, nil
}

func (in *input) Close() error {
	if in.c != nil {
		return in.c.Close()
	}
	return nil
}

func (in *input) reader() io.Reader { return io.NewSectionReader(in.ra, 0, in.size) }

func baseName(p string) string { return path.Base(strings.ReplaceAll(p, `\`, "/")) }

// legacyName decodes an entry name written without a UTF-8 flag: as UTF-8
// when valid, CP949 when it looks Korean, else the DOS code page (CP437) that
// unflagged ZIP names are defined in.
func legacyName(raw string) string {
	if utf8.ValidString(raw) {
		return raw
	}
	b := []byte(raw)
	if doc.LooksKorean(b) {
		if s, err := korean.EUCKR.NewDecoder().Bytes(b); err == nil {
			return string(s)
		}
	}
	if s, err := charmap.CodePage437.NewDecoder().Bytes(b); err == nil {
		return string(s)
	}
	return raw
}

// ---- ZIP ----

type zipSource struct {
	in *input
	z  *zip.Reader
}

func openZip(in *input) (source, error) {
	z, err := zip.NewReader(in.ra, in.size)
	if err != nil {
		return nil, errNotArchive(in.name)
	}
	return &zipSource{in, z}, nil
}

func zipName(f *zip.File) string {
	// Info-ZIP's Unicode Path field holds the UTF-8 name of a legacy-coded one.
	extra := f.Extra
	for len(extra) >= 4 {
		id, size := binary.LittleEndian.Uint16(extra), int(binary.LittleEndian.Uint16(extra[2:]))
		if 4+size > len(extra) {
			break
		}
		if id == 0x7075 && size > 5 && extra[4] == 1 {
			if crc32.ChecksumIEEE([]byte(f.Name)) == binary.LittleEndian.Uint32(extra[5:]) {
				if name := string(extra[9 : 4+size]); utf8.ValidString(name) {
					return name
				}
			}
		}
		extra = extra[4+size:]
	}
	if f.NonUTF8 {
		return legacyName(f.Name)
	}
	return f.Name
}

func (s *zipSource) List() ([]member, error) {
	out := make([]member, 0, len(s.z.File))
	for _, f := range s.z.File {
		name := zipName(f)
		out = append(out, member{entryInfo{
			Path:      name,
			Folder:    strings.HasSuffix(name, "/") || strings.HasSuffix(name, `\`) || f.Mode().IsDir(),
			Size:      int64(f.UncompressedSize64),
			Encrypted: f.Flags&1 != 0,
			Symlink:   f.Mode()&fs.ModeSymlink != 0,
		}, f.Modified})
	}
	return out, nil
}

func (s *zipSource) Walk(visit func(member, io.Reader) error) error {
	list, _ := s.List()
	for i, f := range s.z.File {
		m := list[i]
		if m.Folder || m.Symlink {
			if err := visit(m, nil); err != nil {
				return err
			}
			continue
		}
		rc, err := f.Open()
		if err != nil {
			if errors.Is(err, zip.ErrAlgorithm) {
				return fmt.Errorf("%s uses a compression method Filee cannot unpack (%s)", s.in.name, m.Path)
			}
			return errDamaged(s.in.name, err.Error())
		}
		err = visit(m, rc)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *zipSource) Close() error { return nil }

// ---- 7z ----

type sevenSource struct {
	in *input
	r  *sevenzip.Reader
}

func open7z(in *input) (source, error) {
	r, err := sevenzip.NewReaderWithPassword(in.ra, in.size, "filee-no-password")
	if err != nil {
		if looksEncrypted(err) || sevenZipUsesAES(in) {
			return nil, errEncrypted(in.name)
		}
		return nil, errNotArchive(in.name)
	}
	return &sevenSource{in, r}, nil
}

// sevenZipUsesAES: the archive's header names the AES coder (06 F1 07 01),
// for encrypted content or an encrypted header. The reader itself only sees
// garbage after decrypting with a wrong password.
func sevenZipUsesAES(in *input) bool {
	var sig [32]byte
	if _, err := in.ra.ReadAt(sig[:], 0); err != nil || string(sig[:6]) != "7z\xBC\xAF\x27\x1C" {
		return false
	}
	offset := int64(binary.LittleEndian.Uint64(sig[12:]))
	size := int64(binary.LittleEndian.Uint64(sig[20:]))
	if offset < 0 || size <= 0 || size > 16<<20 || 32+offset+size > in.size {
		return false
	}
	header := make([]byte, size)
	if _, err := in.ra.ReadAt(header, 32+offset); err != nil {
		return false
	}
	return bytes.Contains(header, []byte{0x06, 0xF1, 0x07, 0x01})
}

// looksEncrypted: the reader failed in its AES layer, i.e. the password was wrong.
func looksEncrypted(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "aes") || strings.Contains(s, "password")
}

func (s *sevenSource) List() ([]member, error) {
	encrypted := sevenZipUsesAES(s.in)
	out := make([]member, 0, len(s.r.File))
	for _, f := range s.r.File {
		mode := f.Mode()
		out = append(out, member{entryInfo{
			Path:      f.Name,
			Folder:    mode.IsDir(),
			Size:      int64(f.UncompressedSize),
			Symlink:   mode&fs.ModeSymlink != 0,
			Encrypted: encrypted && !mode.IsDir(),
		}, f.Modified})
	}
	return out, nil
}

func (s *sevenSource) Walk(visit func(member, io.Reader) error) error {
	list, _ := s.List()
	for i, f := range s.r.File {
		m := list[i]
		if m.Folder || m.Symlink {
			if err := visit(m, nil); err != nil {
				return err
			}
			continue
		}
		rc, err := f.Open()
		if err != nil {
			if looksEncrypted(err) {
				return errEncrypted(s.in.name)
			}
			return errDamaged(s.in.name, err.Error())
		}
		err = visit(m, rc)
		rc.Close()
		if err != nil {
			if looksEncrypted(err) {
				return errEncrypted(s.in.name)
			}
			return err
		}
	}
	return nil
}

func (s *sevenSource) Close() error { return nil }

// ---- RAR ----

type rarSource struct {
	in   *input
	path string
}

func openRar(in *input) (source, error) {
	if in.path == "" {
		return nil, errors.New("split RAR archives are not supported; open the .part1.rar volume")
	}
	return &rarSource{in, in.path}, nil
}

func rarError(name string, err error) error {
	switch {
	case errors.Is(err, rardecode.ErrArchiveEncrypted), errors.Is(err, rardecode.ErrArchivedFileEncrypted), errors.Is(err, rardecode.ErrBadPassword):
		return errEncrypted(name)
	case errors.Is(err, rardecode.ErrNoSig):
		return errNotArchive(name)
	case errors.Is(err, rardecode.ErrMultiVolume), errors.Is(err, os.ErrNotExist):
		return errDamaged(name, "a volume of the archive is missing")
	}
	return errDamaged(name, err.Error())
}

func rarMember(h *rardecode.FileHeader) member {
	m := member{entryInfo{Path: h.Name, Folder: h.IsDir, Size: h.UnPackedSize, Encrypted: h.Encrypted}, h.ModificationTime}
	if h.UnKnownSize {
		m.Size = -1
	}
	if h.LinkType != 0 {
		m.Symlink = true // links (and junctions) are never unpacked
	}
	return m
}

func (s *rarSource) List() ([]member, error) {
	files, err := rardecode.List(s.path)
	if err != nil {
		return nil, rarError(s.in.name, err)
	}
	out := make([]member, 0, len(files))
	for _, f := range files {
		out = append(out, rarMember(&f.FileHeader))
	}
	return out, nil
}

func (s *rarSource) Walk(visit func(member, io.Reader) error) error {
	r, err := rardecode.OpenReader(s.path)
	if err != nil {
		return rarError(s.in.name, err)
	}
	defer r.Close()
	for {
		h, err := r.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return rarError(s.in.name, err)
		}
		m := rarMember(h)
		var data io.Reader
		if !m.Folder && !m.Symlink {
			data = rarData{r, s.in.name}
		}
		if err := visit(m, data); err != nil {
			return err
		}
	}
}

type rarData struct {
	r    *rardecode.ReadCloser
	name string
}

func (d rarData) Read(p []byte) (int, error) {
	n, err := d.r.Read(p)
	if err != nil && err != io.EOF {
		err = rarError(d.name, err)
	}
	return n, err
}

func (s *rarSource) Close() error { return nil }

// ---- TAR ----

type tarSource struct{ in *input }

func openTar(in *input) (source, error) { return &tarSource{in}, nil }

func tarMember(h *tar.Header) (member, bool) {
	name := h.Name
	if h.Format != tar.FormatPAX {
		name = legacyName(name)
	}
	m := member{entryInfo{Path: name, Size: h.Size}, h.ModTime}
	switch h.Typeflag {
	case tar.TypeDir:
		m.Folder = true
		m.Size = 0
	case tar.TypeReg, tar.TypeRegA, tar.TypeGNUSparse:
	case tar.TypeSymlink:
		m.Symlink = true
	case tar.TypeLink:
		m.HardLink = h.Linkname
		m.Size = 0
	default:
		return m, false // devices, FIFOs, global headers
	}
	return m, true
}

func (s *tarSource) each(visit func(member, io.Reader) error) error {
	tr := tar.NewReader(s.in.reader())
	count := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			if count == 0 {
				return errNotArchive(s.in.name)
			}
			return nil
		}
		if err != nil {
			if count == 0 {
				return errNotArchive(s.in.name)
			}
			return errDamaged(s.in.name, err.Error())
		}
		count++
		m, ok := tarMember(h)
		if !ok {
			continue
		}
		if err := visit(m, tr); err != nil {
			return err
		}
	}
}

func (s *tarSource) List() ([]member, error) {
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

func (s *tarSource) Walk(visit func(member, io.Reader) error) error { return s.each(visit) }
func (s *tarSource) Close() error                                   { return nil }
