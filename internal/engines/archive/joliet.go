package archive

// ISO 9660 images with Joliet names (what Windows and most burning programs
// write): the supplementary volume descriptor's directory tree carries the
// long Unicode names that the primary one shortens to 8.3. Images without
// Joliet go to the Rock Ridge reader (iso9660 package).

import (
	"encoding/binary"
	"io"
	"strings"
	"time"
	"unicode/utf16"
)

const isoSector = 2048

type jolietSource struct {
	in   *input
	root jolietRecord
}

type jolietRecord struct {
	name     string
	extent   int64
	size     int64
	dir      bool
	modified time.Time
}

// openJoliet finds a Joliet volume descriptor; nil when the image has none.
func openJoliet(in *input) *jolietSource {
	for i := int64(16); i < 64; i++ {
		d := make([]byte, isoSector)
		if _, err := in.ra.ReadAt(d, i*isoSector); err != nil || string(d[1:6]) != "CD001" {
			return nil
		}
		switch d[0] {
		case 255:
			return nil // terminator
		case 2:
			esc := string(d[88:91])
			if esc == "%/@" || esc == "%/C" || esc == "%/E" {
				if r, ok := parseIsoRecord(d[156:190], true); ok {
					return &jolietSource{in, r}
				}
			}
		}
	}
	return nil
}

func parseIsoRecord(b []byte, joliet bool) (jolietRecord, bool) {
	if len(b) < 34 || int(b[0]) > len(b) || b[0] < 34 {
		return jolietRecord{}, false
	}
	nameLen := int(b[32])
	if 33+nameLen > len(b) {
		return jolietRecord{}, false
	}
	raw := b[33 : 33+nameLen]
	name := string(raw)
	if joliet && nameLen != 1 {
		u := make([]uint16, nameLen/2)
		for i := range u {
			u[i] = binary.BigEndian.Uint16(raw[i*2:])
		}
		name = string(utf16.Decode(u))
	}
	if i := strings.LastIndexByte(name, ';'); i >= 0 {
		name = name[:i]
	}
	name = strings.TrimSuffix(name, ".")
	r := jolietRecord{
		name:   name,
		extent: int64(binary.LittleEndian.Uint32(b[2:])),
		size:   int64(binary.LittleEndian.Uint32(b[10:])),
		dir:    b[25]&0x02 != 0,
	}
	if t := b[18:25]; t[1] >= 1 && t[1] <= 12 {
		zone := time.FixedZone("", int(int8(t[6]))*15*60)
		r.modified = time.Date(1900+int(t[0]), time.Month(t[1]), int(t[2]), int(t[3]), int(t[4]), int(t[5]), 0, zone)
	}
	return r, true
}

type jolietEntry struct {
	path string
	rec  jolietRecord
}

func (s *jolietSource) entries() ([]jolietEntry, error) {
	var out []jolietEntry
	seen := map[int64]bool{}
	var walk func(dir jolietRecord, prefix string, depth int) error
	walk = func(dir jolietRecord, prefix string, depth int) error {
		if depth > 64 || seen[dir.extent] {
			return errDamaged(s.in.name, "the folder tree is damaged")
		}
		seen[dir.extent] = true
		if dir.size > 64<<20 {
			return errDamaged(s.in.name, "a folder is too large")
		}
		data := make([]byte, dir.size)
		if _, err := s.in.ra.ReadAt(data, dir.extent*isoSector); err != nil && err != io.EOF {
			return errDamaged(s.in.name, "the image ends too early")
		}
		for pos := 0; pos < len(data); {
			l := int(data[pos])
			if l == 0 {
				pos = (pos/isoSector + 1) * isoSector // records do not cross sectors
				continue
			}
			if pos+l > len(data) {
				break
			}
			r, ok := parseIsoRecord(data[pos:pos+l], true)
			pos += l
			if !ok || r.name == "\x00" || r.name == "\x01" || r.name == "" {
				continue
			}
			if len(out) > MaxEntries {
				return tooManyEntries(s.in.name)
			}
			p := prefix + r.name
			out = append(out, jolietEntry{p, r})
			if r.dir {
				if err := walk(r, p+"/", depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return out, walk(s.root, "", 0)
}

func (s *jolietSource) List() ([]member, error) {
	es, err := s.entries()
	out := make([]member, 0, len(es))
	for _, e := range es {
		out = append(out, member{entryInfo{Path: e.path, Folder: e.rec.dir, Size: e.rec.size}, e.rec.modified})
	}
	return out, err
}

func (s *jolietSource) Walk(visit func(member, io.Reader) error) error {
	es, err := s.entries()
	if err != nil {
		return err
	}
	for _, e := range es {
		m := member{entryInfo{Path: e.path, Folder: e.rec.dir, Size: e.rec.size}, e.rec.modified}
		var r io.Reader
		if !e.rec.dir {
			r = io.NewSectionReader(s.in.ra, e.rec.extent*isoSector, e.rec.size)
		}
		if err := visit(m, r); err != nil {
			return err
		}
	}
	return nil
}

func (s *jolietSource) Close() error { return nil }
