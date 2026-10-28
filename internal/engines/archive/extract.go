package archive

import (
	"bufio"
	"compress/bzip2"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
	"github.com/ulikunitz/xz/lzma"

	"github.com/dseif0x/filee-go/internal/formats"
)

// Unpacks every archive format Filee reads into a folder. Filee's own
// readers (pure Go) handle ZIP, 7Z, RAR, TAR and its compressed forms, the
// single-file compressors, CPIO, DEB, RPM, ISO 9660, ALZ, EGG, lzip and
// split archives; 7-Zip, when installed, handles the rest (CAB, DMG, IMG,
// LHA, ARJ) and ISO images (Joliet and UDF names). Compressed TARs and RPM
// payloads are unpacked stage by stage until the files appear. Every stage
// is checked first (encryption, paths leaving the folder, disk space) and
// links are removed.

// readableFormats are the formats that can be unpacked (C#: 7-Zip's plus the built-in ones).
var readableFormats = []string{
	"zip", "jar", "7z", "rar", "tar", "tgz", "tbz2", "txz", "tar7z", "tz", "gz", "bz2", "xz", "lzma", "z",
	"cab", "cpio", "deb", "rpm", "dmg", "iso", "img", "lha", "arj", "split",
	"alz", "egg", "lz", "tlz",
}

// sevenZipOnly are read by 7-Zip alone.
var sevenZipOnly = []string{"dmg", "img", "lha", "arj"}

// sevenZipPreferred are read by 7-Zip when it is installed (it knows Joliet
// and UDF names), else by the built-in reader.
var sevenZipPreferred = []string{"iso"}

// tarWrapped: a TAR inside a compressed stream (or a 7z): unpacking the outer layer yields one .tar file.
var tarWrapped = map[string]bool{"tgz": true, "tbz2": true, "txz": true, "tar7z": true, "tz": true, "tlz": true}

// rpmPayloadFormats are the formats an RPM payload comes in.
var rpmPayloadFormats = map[string]bool{"cpio": true, "gz": true, "bz2": true, "xz": true, "lzma": true, "z": true, "zst": true}

// singleStream formats hold one compressed stream.
var singleStream = map[string]bool{"tgz": true, "tbz2": true, "txz": true, "tz": true, "tlz": true, "gz": true, "bz2": true, "xz": true, "lzma": true, "z": true, "lz": true, "zst": true}

const maxStages = 3

type extractor struct {
	sevenZip string // "" when 7-Zip is not installed
}

// extract unpacks archive (of format) into a new folder below scratchRoot and returns it.
func (x *extractor) extract(ctx context.Context, archive, format, scratchRoot string) (string, error) {
	current, currentFormat := archive, format
	previous := ""
	for stage := 0; ; stage++ {
		folder := filepath.Join(scratchRoot, "x"+strconv.Itoa(stage))
		if err := os.MkdirAll(folder, 0o755); err != nil {
			return "", err
		}
		if err := x.extractOnce(ctx, current, currentFormat, folder, scratchRoot); err != nil {
			return "", err
		}
		if err := removeLinks(folder); err != nil {
			return "", err
		}
		if previous != "" {
			tryDeleteFolder(previous)
		}
		if stage+1 < maxStages {
			if p, f, ok := innerArchive(format, folder); ok {
				previous = folder
				current, currentFormat = p, f
				continue
			}
		}
		return folder, nil
	}
}

func (x *extractor) extractOnce(ctx context.Context, archive, format, folder, scratchRoot string) error {
	name := filepath.Base(archive)
	if slices.Contains(sevenZipOnly, format) || slices.Contains(sevenZipPreferred, format) && x.sevenZip != "" {
		return x.with7Zip(ctx, archive, folder, scratchRoot)
	}
	switch format {
	case "alz":
		return extractAlz(ctx, archive, folder)
	case "egg":
		return extractEgg(ctx, archive, folder)
	case "lz", "tlz":
		out := formats.NameWithoutExtension(archive)
		if format == "tlz" {
			out += ".tar"
		}
		return decompressLzip(ctx, archive, filepath.Join(folder, safeFileName(out)))
	case "split":
		return x.extractSplit(ctx, archive, folder, scratchRoot)
	}
	in, err := openInput(archive)
	if err != nil {
		return err
	}
	defer in.Close()
	return x.extractInput(ctx, in, format, folder, scratchRoot, name)
}

// extractInput unpacks one opened input with a built-in reader.
func (x *extractor) extractInput(ctx context.Context, in *input, format, folder, scratchRoot, outName string) error {
	if singleStream[format] && format != "lz" && format != "tlz" {
		return decompressStream(ctx, in, format, filepath.Join(folder, singleName(outName, format, in)))
	}
	var src source
	var err error
	switch format {
	case "zip", "jar":
		src, err = openZip(in)
	case "7z", "tar7z":
		src, err = open7z(in)
	case "rar":
		src, err = openRar(in)
	case "tar":
		src, err = openTar(in)
	case "cpio":
		src, err = openCpio(in)
	case "deb":
		src, err = openAr(in)
	case "iso":
		if j := openJoliet(in); j != nil {
			src = j
		} else {
			src, err = openIso(in)
		}
	case "cab":
		var cab *cabSource
		if cab, err = openCab(in); err == nil {
			src = cab
		} else if errors.Is(err, errCabNeeds7Zip) {
			if x.sevenZip != "" && in.path != "" {
				return x.with7Zip(ctx, in.path, folder, scratchRoot)
			}
			return fmt.Errorf("%v; install 7-Zip to unpack it", strings.TrimSuffix(err.Error(), ": "+errCabNeeds7Zip.Error()))
		}
	case "rpm":
		return extractRpm(ctx, in, folder, outName)
	default:
		if x.sevenZip != "" && in.path != "" {
			return x.with7Zip(ctx, in.path, folder, scratchRoot)
		}
		return fmt.Errorf("Filee cannot unpack %s archives without 7-Zip", strings.ToUpper(format))
	}
	if err != nil {
		return err
	}
	defer src.Close()
	return extractMembers(ctx, src, in.name, folder)
}

func (x *extractor) with7Zip(ctx context.Context, archive, folder, scratchRoot string) error {
	if x.sevenZip == "" {
		return errNo7Zip
	}
	tool := &sevenZip{x.sevenZip}
	entries, err := tool.list(ctx, archive)
	if err != nil {
		return err
	}
	infos := make([]entryInfo, 0, len(entries))
	var links []string
	for _, e := range entries {
		infos = append(infos, entryInfo{Path: e.Path, Folder: e.Folder, Size: e.Size, Encrypted: e.Encrypted, Symlink: e.IsSymbolicLink(), HardLink: e.HardLink})
		if e.IsSymbolicLink() {
			links = append(links, e.Path)
		}
	}
	if err := checkListing(infos, filepath.Base(archive), folder); err != nil {
		return err
	}
	return tool.extract(ctx, archive, folder, links, scratchRoot)
}

// innerArchive is the single archive file a stage produced that must be unpacked as well.
func innerArchive(originalFormat, folder string) (string, string, bool) {
	if !tarWrapped[originalFormat] && originalFormat != "rpm" {
		return "", "", false
	}
	entries, err := os.ReadDir(folder)
	if err != nil || len(entries) != 1 || entries[0].IsDir() {
		return "", "", false
	}
	file := filepath.Join(folder, entries[0].Name())
	if isTar(file) {
		return file, "tar", true
	}
	if originalFormat == "rpm" {
		detected := ""
		if f := formats.Detect(file); f != nil {
			detected = f.ID
		} else {
			detected = strings.ToLower(strings.TrimPrefix(filepath.Ext(file), "."))
		}
		if head := readHead(file, 512); sniff(head) != "" && !rpmPayloadFormats[detected] {
			detected = sniff(head)
		}
		if rpmPayloadFormats[detected] {
			return file, detected, true
		}
	}
	return "", "", false
}

// isTar: a .tar file or a file with the "ustar" signature at offset 257.
func isTar(p string) bool {
	if strings.HasSuffix(strings.ToLower(p), ".tar") {
		return true
	}
	h := readHead(p, 262)
	return len(h) == 262 && string(h[257:262]) == "ustar"
}

func readHead(p string, n int) []byte {
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	b := make([]byte, n)
	m, _ := io.ReadFull(f, b)
	return b[:m]
}

// singleName is the name of the file inside a single-stream archive: the
// archive's name without its extension ("notes.txt.gz" → "notes.txt";
// "backup.tgz" → "backup.tar"), or for GZ the name stored in its header.
func singleName(archiveName, format string, in *input) string {
	base := formats.NameWithoutExtension(archiveName)
	if tarWrapped[format] {
		return safeFileName(base + ".tar")
	}
	if format == "gz" {
		if zr, err := gzip.NewReader(in.reader()); err == nil {
			stored := baseName(zr.Name)
			zr.Close()
			if stored != "" && stored != "." && stored != "/" && !IsUnsafePath(stored) {
				return safeFileName(stored)
			}
		}
	}
	if base == "" {
		base = "data"
	}
	return safeFileName(base)
}

func safeFileName(n string) string { return sanitizeSegment(n) }

// streamReader opens the decompressor of a single-stream format.
func streamReader(r io.Reader, format string) (io.Reader, func(), error) {
	br := bufio.NewReaderSize(r, 1<<16)
	switch format {
	case "gz", "tgz":
		zr, err := gzip.NewReader(br)
		if err != nil {
			return nil, nil, err
		}
		return zr, func() { zr.Close() }, nil
	case "bz2", "tbz2":
		return bzip2.NewReader(br), func() {}, nil
	case "xz", "txz":
		zr, err := xz.NewReader(br)
		return zr, func() {}, err
	case "lzma":
		zr, err := lzma.NewReader(br)
		return zr, func() {}, err
	case "z", "tz":
		zr, err := newZReader(br)
		return zr, func() {}, err
	case "zst":
		zr, err := zstd.NewReader(br, zstd.WithDecoderMaxMemory(1<<31))
		if err != nil {
			return nil, nil, err
		}
		return zr, zr.Close, nil
	}
	return nil, nil, fmt.Errorf("unknown stream format %s", format)
}

// decompressStream writes the single file of a compressed stream.
func decompressStream(ctx context.Context, in *input, format, target string) (err error) {
	r, closeFn, err := streamReader(in.reader(), format)
	if err != nil {
		return errNotArchive(in.name)
	}
	defer closeFn()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = copyLimited(ctx, out, r, math.MaxInt64, nil)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(target)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errDamaged(in.name, err.Error())
	}
	if f, ok := in.c.(*os.File); ok {
		if st, err := f.Stat(); err == nil {
			setTime(target, st.ModTime())
		}
	}
	return nil
}

// extractRpm writes an RPM's payload as one file ("name.cpio.xz" …); the next stage unpacks it.
func extractRpm(ctx context.Context, in *input, folder, outName string) error {
	offset, ext, err := rpmPayload(in)
	if err != nil {
		return err
	}
	target := filepath.Join(folder, safeFileName(formats.NameWithoutExtension(outName)+"."+ext))
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = copyLimited(ctx, out, io.NewSectionReader(in.ra, offset, in.size-offset), math.MaxInt64, nil)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// ---- split archives (name.7z.001, name.zip.001, name.001 …) ----

var volumeNumber = regexp.MustCompile(`(?i)^(.*)\.(\d{3,})$`)

// splitVolumes lists the volumes of a split archive from any of its parts, in order.
func splitVolumes(first string) ([]string, error) {
	dir, name := filepath.Split(first)
	m := volumeNumber.FindStringSubmatch(name)
	if m == nil {
		return nil, fmt.Errorf("%s is not part of a split archive (.001, .002 …)", name)
	}
	width := len(m[2])
	var out []string
	for n := 1; ; n++ {
		p := filepath.Join(dir, fmt.Sprintf("%s.%0*d", m[1], width, n))
		if _, err := os.Stat(p); err != nil {
			break
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, errDamaged(name, "the first volume (.001) is missing")
	}
	return out, nil
}

// multiReaderAt joins the volumes into one ReaderAt.
type multiReaderAt struct {
	files  []*os.File
	starts []int64
	size   int64
}

func (m *multiReaderAt) ReadAt(p []byte, off int64) (int, error) {
	total := 0
	for len(p) > 0 {
		if off >= m.size {
			return total, io.EOF
		}
		i, _ := slices.BinarySearch(m.starts, off+1)
		i--
		local := off - m.starts[i]
		end := m.size
		if i+1 < len(m.starts) {
			end = m.starts[i+1]
		}
		chunk := p[:min(int64(len(p)), end-off)]
		n, err := m.files[i].ReadAt(chunk, local)
		total += n
		off += int64(n)
		p = p[n:]
		if err != nil && err != io.EOF {
			return total, err
		}
		if n < len(chunk) {
			return total, io.ErrUnexpectedEOF
		}
	}
	return total, nil
}

func (m *multiReaderAt) Close() error {
	for _, f := range m.files {
		f.Close()
	}
	return nil
}

func (x *extractor) extractSplit(ctx context.Context, archive, folder, scratchRoot string) error {
	volumes, err := splitVolumes(archive)
	if err != nil {
		return err
	}
	mr := &multiReaderAt{}
	for _, v := range volumes {
		f, err := os.Open(v)
		if err != nil {
			mr.Close()
			return err
		}
		st, _ := f.Stat()
		mr.files = append(mr.files, f)
		mr.starts = append(mr.starts, mr.size)
		mr.size += st.Size()
	}
	defer mr.Close()
	inner := strings.TrimSuffix(filepath.Base(volumes[0]), filepath.Ext(volumes[0])) // "photos.7z"
	in := &input{ra: mr, size: mr.size, name: filepath.Base(archive)}
	head := make([]byte, 512)
	n, _ := mr.ReadAt(head, 0)
	format := sniff(head[:n])
	if f := formats.Detect(inner); f != nil && f.Category == formats.Archive && f.ID != "split" {
		format = f.ID
	}
	switch {
	case format == "" || format == "split":
		// Not an archive inside: the joined file is the result.
		return joinTo(ctx, in, filepath.Join(folder, safeFileName(inner)))
	case format == "rar", slices.Contains(sevenZipOnly, format), format == "alz", format == "egg", format == "lz", format == "tlz":
		// Readers that need a file: join the volumes into the scratch folder first.
		joined := filepath.Join(scratchRoot, "joined-"+randomSuffix(), safeFileName(inner))
		os.MkdirAll(filepath.Dir(joined), 0o755)
		if err := joinTo(ctx, in, joined); err != nil {
			return err
		}
		defer os.RemoveAll(filepath.Dir(joined))
		if err := x.extractOnce(ctx, joined, format, folder, scratchRoot); err != nil {
			return err
		}
		return x.unwrapInner(ctx, format, folder, scratchRoot)
	}
	if err := x.extractInput(ctx, in, format, folder, scratchRoot, inner); err != nil {
		return err
	}
	return x.unwrapInner(ctx, format, folder, scratchRoot)
}

// unwrapInner unpacks the TAR a split compressed TAR produced, in place.
func (x *extractor) unwrapInner(ctx context.Context, format, folder, scratchRoot string) error {
	p, f, ok := innerArchive(format, folder)
	if !ok {
		return nil
	}
	tmp := filepath.Join(scratchRoot, "inner-"+randomSuffix())
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	moved := filepath.Join(tmp, filepath.Base(p))
	if err := os.Rename(p, moved); err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	return x.extractOnce(ctx, moved, f, folder, scratchRoot)
}

func joinTo(ctx context.Context, in *input, target string) error {
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	_, err = copyLimited(ctx, out, in.reader(), math.MaxInt64, nil)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}
