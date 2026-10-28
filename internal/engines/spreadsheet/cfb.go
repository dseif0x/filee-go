package spreadsheet

import (
	"encoding/binary"
	"strings"
	"unicode/utf16"
)

// A minimal reader of Microsoft compound files (OLE2 structured storage),
// enough to take one stream out of an .xls file.

const lastSector = 0xFFFFFFFA // sector numbers at and above are markers

// cfbStream returns the stream with one of names (case-insensitive) from a
// compound file, or nil.
func cfbStream(file []byte, names ...string) []byte {
	le := binary.LittleEndian
	if len(file) < 512 || le.Uint64(file) != 0xE11AB1A1E011CFD0 {
		return nil
	}
	sectorSize := 1 << le.Uint16(file[30:])
	miniSectorSize := 1 << le.Uint16(file[32:])
	if sectorSize != 512 && sectorSize != 4096 || miniSectorSize > sectorSize {
		return nil
	}
	sectorCount := (len(file) + sectorSize - 1) / sectorSize
	sector := func(i uint32) []byte {
		start := min((int64(i)+1)*int64(sectorSize), int64(len(file)))
		end := min(start+int64(sectorSize), int64(len(file)))
		return file[start:end]
	}
	// The FAT is listed by the header (109 sectors) and, in big files, a chain of DIFAT sectors.
	var fatSectors []uint32
	for i := 0; i < 109; i++ {
		fatSectors = append(fatSectors, le.Uint32(file[76+i*4:]))
	}
	for difat := le.Uint32(file[68:]); difat < lastSector && len(fatSectors) < sectorCount*2; {
		s := sector(difat)
		if len(s) < sectorSize {
			break
		}
		for i := 0; i+4 < len(s); i += 4 {
			fatSectors = append(fatSectors, le.Uint32(s[i:]))
		}
		difat = le.Uint32(s[len(s)-4:])
	}
	var fat []uint32
	for _, fs := range fatSectors {
		if fs >= lastSector {
			continue
		}
		d := sector(fs)
		for i := 0; i+4 <= len(d); i += 4 {
			fat = append(fat, le.Uint32(d[i:]))
		}
	}
	// A stream's sectors, each pointing to the next; the step count stops broken, circular chains.
	chain := func(start uint32, table []uint32, read func(uint32) []byte) []byte {
		var out []byte
		steps := 0
		for s := start; s < lastSector && int(s) < len(table) && steps <= len(table); s, steps = table[s], steps+1 {
			out = append(out, read(s)...)
		}
		return out
	}
	directory := chain(le.Uint32(file[48:]), fat, sector)
	if len(directory) < 128 {
		return nil
	}
	found := false
	var start uint32
	var size int64
	for i := 0; i+128 <= len(directory) && !found; i += 128 {
		e := directory[i : i+128]
		nameLen := max(0, min(int(le.Uint16(e[64:]))-2, 62))
		u := make([]uint16, nameLen/2)
		for k := range u {
			u[k] = le.Uint16(e[2*k:])
		}
		name := string(utf16.Decode(u))
		if e[66] != 2 {
			continue
		}
		for _, n := range names {
			if strings.EqualFold(name, n) {
				found, start, size = true, le.Uint32(e[116:]), int64(le.Uint32(e[120:]))
			}
		}
	}
	if !found {
		return nil
	}
	var b []byte
	if size < int64(le.Uint32(file[56:])) {
		// Small streams live in the mini stream (held by the root entry), addressed through the mini FAT.
		mini := chain(le.Uint32(directory[116:]), fat, sector)
		miniFatBytes := chain(le.Uint32(file[60:]), fat, sector)
		miniFat := make([]uint32, len(miniFatBytes)/4)
		for i := range miniFat {
			miniFat[i] = le.Uint32(miniFatBytes[4*i:])
		}
		b = chain(start, miniFat, func(s uint32) []byte {
			if (int64(s)+1)*int64(miniSectorSize) <= int64(len(mini)) {
				return mini[int(s)*miniSectorSize : (int(s)+1)*miniSectorSize]
			}
			return nil
		})
	} else {
		b = chain(start, fat, sector)
	}
	if int64(len(b)) < size {
		return nil
	}
	return b[:size]
}
