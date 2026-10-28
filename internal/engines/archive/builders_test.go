package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/flate"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	dbzip2 "github.com/dsnet/compress/bzip2"
	"github.com/klauspost/compress/gzip"
	"github.com/ulikunitz/xz/lzma"
	"golang.org/x/text/encoding/korean"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/presets"
)

// knownTime is a fixed modification time for checking that times survive.
var knownTime = time.Date(2021, 5, 6, 7, 8, 10, 0, time.Local)

// makeTree creates name below parent: Korean names, a nested folder, an empty folder, a file with knownTime.
func makeTree(t *testing.T, parent, name string) string {
	t.Helper()
	root := filepath.Join(parent, name)
	must(t, os.MkdirAll(filepath.Join(root, "docs", "보고서"), 0o755))
	must(t, os.MkdirAll(filepath.Join(root, "empty"), 0o755))
	write(t, filepath.Join(root, "readme.txt"), "Filee archive test\n")
	write(t, filepath.Join(root, "한글 문서.txt"), "안녕하세요, archive 文件")
	write(t, filepath.Join(root, "docs", "보고서", "data.csv"), "a,b\n1,2\n")
	must(t, os.WriteFile(filepath.Join(root, "docs", "noise.bin"), noise(200_000, 7), 0o644))
	must(t, os.Chtimes(filepath.Join(root, "readme.txt"), knownTime, knownTime))
	return root
}

// snapshot is relative path → SHA-256 of every file below root, and "dir/" for empty folders.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if info.IsDir() {
			if entries, _ := os.ReadDir(p); len(entries) == 0 && rel != "." {
				out[rel+"/"] = ""
			}
			return nil
		}
		b, _ := os.ReadFile(p)
		h := sha256.Sum256(b)
		out[rel] = hex.EncodeToString(h[:])
		return nil
	})
	must(t, err)
	return out
}

func sameSnapshot(t *testing.T, want, got map[string]string) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("snapshot: want %v\ngot %v", want, got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("snapshot %s: want %q got %q\nall: %v", k, v, got[k], got)
		}
	}
}

func noise(n int, seed int64) []byte {
	b := make([]byte, n)
	rand.New(rand.NewSource(seed)).Read(b)
	return b
}

// words is text that compresses well but not trivially.
func words(n int) []byte {
	r := rand.New(rand.NewSource(42))
	list := []string{"파일", "변환", "archive", "Filee", "압축", "folder", "데이터", "zip", "문서", "7-Zip", "\n"}
	var sb strings.Builder
	for sb.Len() < n {
		sb.WriteString(list[r.Intn(len(list))])
		sb.WriteByte(' ')
	}
	return []byte(sb.String())[:n]
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, p, s string) {
	t.Helper()
	must(t, os.MkdirAll(filepath.Dir(p), 0o755))
	must(t, os.WriteFile(p, []byte(s), 0o644))
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	must(t, err)
	return string(b)
}

// zipDir zips root, with the folder itself as top entry when withBase.
func zipDir(t *testing.T, root, out string, withBase bool) {
	t.Helper()
	f, err := os.Create(out)
	must(t, err)
	zw := zip.NewWriter(f)
	base := filepath.Dir(root)
	if !withBase {
		base = root
	}
	must(t, filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(base, p)
		if rel == "." {
			return nil
		}
		h, _ := zip.FileInfoHeader(info)
		h.Name = filepath.ToSlash(rel)
		if info.IsDir() {
			h.Name += "/"
			_, err := zw.CreateHeader(h)
			return err
		}
		h.Method = zip.Deflate
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		b, _ := os.ReadFile(p)
		_, err = w.Write(b)
		return err
	}))
	must(t, zw.Close())
	must(t, f.Close())
}

// tarGzDir writes the content of root (without the folder itself) as a .tar.gz.
func tarGzDir(t *testing.T, root, out string) {
	t.Helper()
	f, err := os.Create(out)
	must(t, err)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	must(t, filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			return nil
		}
		h, _ := tar.FileInfoHeader(info, "")
		h.Name = filepath.ToSlash(rel)
		h.Format = tar.FormatPAX
		if info.IsDir() {
			h.Name += "/"
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if !info.IsDir() {
			b, _ := os.ReadFile(p)
			tw.Write(b)
		}
		return nil
	}))
	must(t, tw.Close())
	must(t, gz.Close())
	must(t, f.Close())
}

// step converts input to "to"; outputs go to outDir as "<base><suffix>.<ext>" (a folder for ext "").
func step(t *testing.T, input, from, to, outDir string, preset *presets.Preset) *convert.Step {
	t.Helper()
	if preset == nil {
		preset = presets.New("test", to)
	}
	base := formats.NameWithoutExtension(input)
	return &convert.Step{
		InputPath: input, From: from, To: to, Preset: preset, WorkDir: t.TempDir(),
		Output: convert.AllocatorFunc(func(ext, suffix string) string {
			if ext == "" {
				return filepath.Join(outDir, base+suffix)
			}
			return filepath.Join(outDir, base+suffix+"."+ext)
		}),
	}
}

// ---- compressed streams ----

func bzip2Bytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := dbzip2.NewWriter(&buf, &dbzip2.WriterConfig{Level: 9})
	must(t, err)
	w.Write(data)
	must(t, w.Close())
	return buf.Bytes()
}

func deflateBytes(data []byte) []byte {
	var buf bytes.Buffer
	w, _ := flate.NewWriter(&buf, 6)
	w.Write(data)
	w.Close()
	return buf.Bytes()
}

// rawLzma is a raw LZMA stream (lc=3, lp=0, pb=2, 1 MiB dictionary): the .lzma file without its 13-byte header.
func rawLzma(t *testing.T, data []byte, endMarker bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	cfg := lzma.WriterConfig{DictCap: 1 << 20, EOSMarker: endMarker, Size: -1}
	if !endMarker {
		cfg.Size = int64(len(data))
	}
	w, err := cfg.NewWriter(&buf)
	must(t, err)
	w.Write(data)
	must(t, w.Close())
	return buf.Bytes()[13:]
}

// lzipBytes is one lzip member of data.
func lzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	body := rawLzma(t, data, true)
	var buf bytes.Buffer
	buf.WriteString("LZIP\x01")
	buf.WriteByte(20) // 2^20 = 1 MiB dictionary
	buf.Write(body)
	binary.Write(&buf, binary.LittleEndian, crc32.ChecksumIEEE(data))
	binary.Write(&buf, binary.LittleEndian, int64(len(data)))
	binary.Write(&buf, binary.LittleEndian, int64(6+len(body)+20))
	return buf.Bytes()
}

// toAlzBzip2 rewrites a standard bzip2 stream into ALZip's framing: no
// "BZh9", "DLZ\x01" instead of each block's magic, CRC and randomised bit,
// "DLZ\x02" instead of the end magic and combined CRC.
func toAlzBzip2(t *testing.T, standard []byte) []byte {
	t.Helper()
	if string(standard[:4]) != "BZh9" {
		t.Fatalf("not a 900k bzip2 stream: %q", standard[:4])
	}
	bits := make([]bool, len(standard)*8)
	for i := range bits {
		bits[i] = standard[i>>3]&(0x80>>(i&7)) != 0
	}
	const blockMagic, endMagic = 0x314159265359, 0x177245385090
	read48 := func(at int) uint64 {
		var v uint64
		for i := 0; i < 48 && at+i < len(bits); i++ {
			v <<= 1
			if bits[at+i] {
				v |= 1
			}
		}
		return v
	}
	var out []bool
	put := func(v uint32, n int) {
		for i := n - 1; i >= 0; i-- {
			out = append(out, v>>i&1 != 0)
		}
	}
	pos := 32
	for {
		magic := read48(pos)
		if magic == endMagic {
			put(0x444C5A02, 32)
			break
		}
		if magic != blockMagic {
			t.Fatalf("no block magic at bit %d", pos)
		}
		put(0x444C5A01, 32)
		pos += 48 + 32 + 1
		next := pos
		for m := read48(next); m != blockMagic && m != endMagic; m = read48(next) {
			next++
		}
		out = append(out, bits[pos:next]...)
		pos = next
	}
	b := make([]byte, (len(out)+7)/8)
	for i, v := range out {
		if v {
			b[i>>3] |= 0x80 >> (i & 7)
		}
	}
	return b
}

// compressZ writes data in Unix compress format (block mode, up to 16-bit codes, no clears).
func compressZ(data []byte) []byte {
	out := []byte{0x1F, 0x9D, 0x80 | 16}
	bits, maxcode, free := 9, 511, 257
	var group []byte
	var acc uint64
	var nacc int
	flushGroup := func() {
		if nacc > 0 {
			group = append(group, byte(acc))
			acc, nacc = 0, 0
		}
		if len(group) > 0 {
			for len(group) < bits {
				group = append(group, 0)
			}
			out = append(out, group...)
			group = group[:0]
		}
	}
	codesInGroup := 0
	emit := func(code int) {
		acc |= uint64(code) << nacc
		nacc += bits
		for nacc >= 8 {
			group = append(group, byte(acc))
			acc >>= 8
			nacc -= 8
		}
		codesInGroup++
		if codesInGroup == 8 {
			out = append(out, group...)
			group, codesInGroup = group[:0], 0
		}
		if free > maxcode && bits < 16 {
			flushGroup()
			codesInGroup = 0
			bits++
			maxcode = 1<<bits - 1
		}
	}
	table := map[[2]int]int{}
	if len(data) == 0 {
		return out
	}
	ent := int(data[0])
	for _, c := range data[1:] {
		key := [2]int{ent, int(c)}
		if code, ok := table[key]; ok {
			ent = code
			continue
		}
		emit(ent)
		ent = int(c)
		if free < 1<<16 {
			table[key] = free
			free++
		}
	}
	emit(ent)
	// The last group is not padded: only the bytes holding codes are written.
	out = append(out, group...)
	if nacc > 0 {
		out = append(out, byte(acc))
	}
	return out
}

// ---- ALZ / EGG ----

type alzItem2 struct {
	name      string
	data      []byte // nil: folder
	method    int    // 0 store, 1 ALZ bzip2 (packed given), 2 deflate
	packed    []byte
	encrypted bool
}

func dosTime(t time.Time) uint32 {
	return uint32(t.Year()-1980)<<25 | uint32(t.Month())<<21 | uint32(t.Day())<<16 | uint32(t.Hour())<<11 | uint32(t.Minute())<<5 | uint32(t.Second()/2)
}

// writeAlz writes an ALZ archive; names are stored in CP949 like ALZip does.
func writeAlz(t *testing.T, path string, items []alzItem2) {
	t.Helper()
	var w bytes.Buffer
	le := func(v any) { binary.Write(&w, binary.LittleEndian, v) }
	w.WriteString("ALZ\x01")
	w.Write([]byte{0x0A, 0, 0, 0})
	for _, it := range items {
		name, err := korean.EUCKR.NewEncoder().Bytes([]byte(it.name))
		must(t, err)
		var packed []byte
		switch {
		case it.data == nil:
		case it.method == 0:
			packed = it.data
		case it.method == 1:
			packed = it.packed
		default:
			packed = deflateBytes(it.data)
		}
		w.WriteString("BLZ\x01")
		le(uint16(len(name)))
		if it.data == nil {
			w.WriteByte(0x10)
		} else {
			w.WriteByte(0x20)
		}
		le(dosTime(knownTime))
		desc := byte(0)
		if it.data != nil {
			desc = 0x40
		}
		if it.encrypted {
			desc |= 1
		}
		w.WriteByte(desc)
		w.WriteByte(0)
		if it.data != nil {
			w.WriteByte(byte(it.method))
			w.WriteByte(0)
			le(crc32.ChecksumIEEE(it.data))
			le(uint32(len(packed)))
			le(uint32(len(it.data)))
		}
		w.Write(name)
		if it.encrypted {
			w.Write(make([]byte, 12))
		}
		w.Write(packed)
	}
	w.WriteString("CLZ\x01")
	w.Write(make([]byte, 12))
	w.WriteString("CLZ\x02")
	w.Write(make([]byte, 12))
	must(t, os.WriteFile(path, w.Bytes(), 0o644))
}

type eggBlock2 struct {
	data   []byte
	method int // 0 store, 1 deflate, 2 bzip2, 4 LZMA (packed given for 2 and 4)
	packed []byte
}

type eggItem2 struct {
	name   string
	folder bool
	blocks []eggBlock2
}

// writeEgg writes an EGG archive (UTF-8 names, Windows file info with knownTime).
func writeEgg(t *testing.T, path string, items []eggItem2, solid bool) {
	t.Helper()
	const end = 0x08E28222
	var w bytes.Buffer
	le := func(v any) { binary.Write(&w, binary.LittleEndian, v) }
	le(uint32(0x41474745))
	le(uint16(0x0100))
	le(uint32(0x12345678))
	le(uint32(0))
	if solid {
		le(uint32(0x24E5A060))
		w.WriteByte(0)
		le(uint16(0))
	}
	le(uint32(end))
	for id, it := range items {
		var size int64
		for _, b := range it.blocks {
			size += int64(len(b.data))
		}
		le(uint32(0x0A8590E3))
		le(uint32(id))
		le(size)
		name := []byte(it.name)
		le(uint32(0x0A8591AC))
		w.WriteByte(0)
		le(uint16(len(name)))
		w.Write(name)
		le(uint32(0x2C86950B))
		w.WriteByte(0)
		le(uint16(9))
		le(knownTime.UTC().UnixNano()/100 + 116444736000000000)
		if it.folder {
			w.WriteByte(0x10)
		} else {
			w.WriteByte(0x20)
		}
		le(uint32(end))
		for _, b := range it.blocks {
			packed := b.packed
			switch b.method {
			case 0:
				packed = b.data
			case 1:
				packed = deflateBytes(b.data)
			}
			le(uint32(0x02B50C13))
			w.WriteByte(byte(b.method))
			w.WriteByte(0)
			le(uint32(len(b.data)))
			le(uint32(len(packed)))
			le(crc32.ChecksumIEEE(b.data))
			le(uint32(end))
			w.Write(packed)
		}
	}
	le(uint32(end))
	must(t, os.WriteFile(path, w.Bytes(), 0o644))
}

// eggLzma is an EGG LZMA block: ZIP's LZMA header (version, property size, 5 property bytes) + raw data.
func eggLzma(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	cfg := lzma.WriterConfig{DictCap: 1 << 20, Size: int64(len(data))}
	w, err := cfg.NewWriter(&buf)
	must(t, err)
	w.Write(data)
	must(t, w.Close())
	full := buf.Bytes()
	out := []byte{9, 20, 5, 0}
	out = append(out, full[:5]...)
	return append(out, full[13:]...)
}

// ---- RAR 5 (stored entries) ----

func vint(v uint64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7F)
		v >>= 7
		if v != 0 {
			out = append(out, b|0x80)
			continue
		}
		return append(out, b)
	}
}

// rar5Header frames a block: CRC32, size, then the given header fields.
func rar5Header(fields []byte) []byte {
	body := append(vint(uint64(len(fields))), fields...)
	crc := make([]byte, 4)
	binary.LittleEndian.PutUint32(crc, crc32.ChecksumIEEE(body))
	return append(crc, body...)
}

type rarFile struct {
	name string
	data []byte
	dir  bool
}

// writeRar5 writes a RAR 5 archive of stored files.
func writeRar5(t *testing.T, path string, files []rarFile) {
	t.Helper()
	var w bytes.Buffer
	w.WriteString("Rar!\x1A\x07\x01\x00")
	main := append(vint(1), vint(0)...) // type 1, header flags 0
	main = append(main, vint(0)...)     // archive flags
	w.Write(rar5Header(main))
	for _, f := range files {
		var h []byte
		h = append(h, vint(2)...) // file header
		if f.dir {
			h = append(h, vint(0)...)
		} else {
			h = append(h, vint(2)...) // data area present
			h = append(h, vint(uint64(len(f.data)))...)
		}
		flags := uint64(0x02 | 0x04) // mtime and CRC32 present
		if f.dir {
			flags = 0x01 | 0x02
		}
		h = append(h, vint(flags)...)
		h = append(h, vint(uint64(len(f.data)))...)
		h = append(h, vint(0x20)...) // attributes
		mt := make([]byte, 4)
		binary.LittleEndian.PutUint32(mt, uint32(knownTime.Unix()))
		h = append(h, mt...)
		if !f.dir {
			c := make([]byte, 4)
			binary.LittleEndian.PutUint32(c, crc32.ChecksumIEEE(f.data))
			h = append(h, c...)
		}
		h = append(h, vint(0)...) // compression: version 0, stored
		h = append(h, vint(0)...) // host OS: Windows
		h = append(h, vint(uint64(len(f.name)))...)
		h = append(h, f.name...)
		w.Write(rar5Header(h))
		w.Write(f.data)
	}
	end := append(vint(5), vint(0)...)
	end = append(end, vint(0)...)
	w.Write(rar5Header(end))
	must(t, os.WriteFile(path, w.Bytes(), 0o644))
}

// ---- CPIO, ar, RPM ----

func cpioNewc(files map[string]string, order []string) []byte {
	var w bytes.Buffer
	pad := func() {
		for w.Len()%4 != 0 {
			w.WriteByte(0)
		}
	}
	entry := func(name string, mode int, data string) {
		w.WriteString("070701")
		fields := []int{0, mode, 0, 0, 1, int(knownTime.Unix()), len(data), 0, 0, 0, 0, len(name) + 1, 0}
		for _, f := range fields {
			fmt.Fprintf(&w, "%08X", f)
		}
		w.WriteString(name)
		w.WriteByte(0)
		pad()
		w.WriteString(data)
		pad()
	}
	for _, n := range order {
		if strings.HasSuffix(n, "/") {
			entry("./"+strings.TrimSuffix(n, "/"), 0o40755, "")
			continue
		}
		entry("./"+n, 0o100644, files[n])
	}
	entry("TRAILER!!!", 0, "")
	return w.Bytes()
}

func arArchive(members [][2]string) []byte {
	var w bytes.Buffer
	w.WriteString("!<arch>\n")
	for _, m := range members {
		h := []byte(strings.Repeat(" ", 60))
		copy(h[0:], m[0]+"/")
		copy(h[16:], "1620280090")
		copy(h[28:], "0")
		copy(h[34:], "0")
		copy(h[40:], "100644")
		copy(h[48:], strconv.Itoa(len(m[1])))
		copy(h[58:], "`\n")
		w.Write(h)
		w.WriteString(m[1])
		if len(m[1])%2 == 1 {
			w.WriteByte('\n')
		}
	}
	return w.Bytes()
}

// rpmBytes is a minimal RPM: lead, an empty signature and header, and the payload.
func rpmBytes(payload []byte) []byte {
	var w bytes.Buffer
	lead := make([]byte, 96)
	copy(lead, []byte{0xED, 0xAB, 0xEE, 0xDB, 3, 0})
	w.Write(lead)
	header := func() {
		w.Write([]byte{0x8E, 0xAD, 0xE8, 0x01, 0, 0, 0, 0})
		binary.Write(&w, binary.BigEndian, uint32(1)) // one index entry
		binary.Write(&w, binary.BigEndian, uint32(4)) // 4 bytes of data
		w.Write(make([]byte, 16))
		w.Write([]byte{0, 0, 0, 0})
	}
	header()
	for w.Len()%8 != 0 {
		w.WriteByte(0)
	}
	header()
	w.Write(payload)
	return w.Bytes()
}

func gzipBytes(data []byte) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write(data)
	w.Close()
	return buf.Bytes()
}

var _ = io.EOF
