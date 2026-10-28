package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kdomanski/iso9660"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/presets"
)

var ctx = context.Background()

func convertOK(t *testing.T, c *Converter, s *convert.Step) string {
	t.Helper()
	out, err := c.Convert(ctx, s, nil)
	if err != nil {
		t.Fatalf("%s → %s: %v", s.From, s.To, err)
	}
	if len(out) != 1 {
		t.Fatalf("outputs %v", out)
	}
	return out[0]
}

func extractTo(t *testing.T, c *Converter, archive, format string) string {
	t.Helper()
	return convertOK(t, c, step(t, archive, format, formats.Folder, t.TempDir(), nil))
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	must(t, err)
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	slices.Sort(out)
	return out
}

func has7z() bool { return locateSevenZip() != "" }

// ---- routes ----

func TestEveryReadableArchiveConvertsInOneStep(t *testing.T) {
	p := convert.NewPlanner([]convert.Converter{New()}, nil)
	for _, from := range []string{"zip", "jar", "7z", "rar", "tgz", "tbz2", "txz", "tar7z", "tz", "iso", "dmg", "cab", "rpm", "deb", "lha", "arj", "alz", "egg", "lz", "tlz", "split"} {
		for _, to := range []string{"zip", "7z", "tar", "tgz", "tbz2", "txz", formats.Folder} {
			r := p.Plan(from, to, convert.Options{})
			if r == nil || len(r.Steps) != 1 || r.Steps[0].Converter.ID() != "archive" {
				t.Errorf("%s → %s: %+v", from, to, r)
			}
		}
	}
}

func TestSingleFileCompressorsConvertAmongEachOther(t *testing.T) {
	p := convert.NewPlanner([]convert.Converter{New()}, nil)
	for _, c := range [][2]string{{"gz", "xz"}, {"lzma", "bz2"}, {"lz", "gz"}} {
		if r := p.Plan(c[0], c[1], convert.Options{}); r == nil || len(r.Steps) != 1 {
			t.Errorf("%s → %s: %+v", c[0], c[1], r)
		}
	}
	if p.Plan("zip", "gz", convert.Options{}) != nil {
		t.Error("several files do not fit into one .gz")
	}
	if p.Plan("zip", "rar", convert.Options{}) != nil {
		t.Error("RAR cannot be written")
	}
	for _, e := range New().Edges() {
		if formats.ByID(e.From) == nil || formats.ByID(e.To) == nil {
			t.Errorf("unknown format in %s → %s", e.From, e.To)
		}
	}
}

// ---- archive → archive ----

func TestRoundTripKeepsNamesFoldersContentsAndTimes(t *testing.T) {
	dir := t.TempDir()
	tree := makeTree(t, dir, "프로젝트")
	want := snapshot(t, tree)
	current := filepath.Join(dir, "프로젝트.zip")
	zipDir(t, tree, current, true)
	c := New()
	targets := []string{"tgz", "tar", "tbz2", "txz", "zip"}
	if has7z() {
		targets = append([]string{"7z"}, targets...)
	}
	from := "zip"
	for i, to := range targets {
		out := t.TempDir()
		current = convertOK(t, c, step(t, current, from, to, out, nil))
		if !strings.HasSuffix(current, "."+formats.ByID(to).PrimaryExtension()) {
			t.Fatalf("step %d: %s", i, current)
		}
		from = to
	}
	// The last ZIP read by archive/zip: same tree below "프로젝트", UTF-8 names flagged.
	z, err := zip.OpenReader(current)
	must(t, err)
	for _, f := range z.File {
		if f.NonUTF8 {
			t.Errorf("%s has no UTF-8 flag", f.Name)
		}
	}
	z.Close()
	check := extractTo(t, c, current, "zip")
	sameSnapshot(t, want, snapshot(t, check))
	st, err := os.Stat(filepath.Join(check, "readme.txt"))
	must(t, err)
	if d := st.ModTime().Sub(knownTime); d < -2e9 || d > 2e9 {
		t.Errorf("time %v instead of %v", st.ModTime(), knownTime)
	}
}

func TestStoreAndMaximumLevelsAreApplied(t *testing.T) {
	dir := t.TempDir()
	text := filepath.Join(dir, "big.txt")
	must(t, os.WriteFile(text, words(300_000), 0o644))
	src := filepath.Join(dir, "source.tar")
	c := New()
	must(t, c.Combine(ctx, []string{text}, src, "tar", presets.New("x", "tar"), t.TempDir(), nil))
	for _, to := range []string{"zip", "tgz", "tbz2", "txz"} {
		store := presets.New("s", to)
		store.Archive.Level = presets.ArchiveStore
		maxi := presets.New("m", to)
		maxi.Archive.Level = presets.ArchiveMaximum
		s := convertOK(t, c, step(t, src, "tar", to, t.TempDir(), store))
		m := convertOK(t, c, step(t, src, "tar", to, t.TempDir(), maxi))
		ss, _ := os.Stat(s)
		ms, _ := os.Stat(m)
		if ms.Size() >= 100_000 {
			t.Errorf("%s maximum: %d bytes", to, ms.Size())
		}
		if (to == "zip" || to == "tgz") && ss.Size() <= 300_000 {
			t.Errorf("%s store: %d bytes", to, ss.Size())
		}
	}
}

func TestSingleFileStreamsConvert(t *testing.T) {
	dir := t.TempDir()
	data := words(50_000)
	gz := filepath.Join(dir, "data.csv.gz")
	must(t, os.WriteFile(gz, gzipBytes(data), 0o644))
	c := New()
	xz := convertOK(t, c, step(t, gz, "gz", "xz", t.TempDir(), nil))
	bz2 := convertOK(t, c, step(t, xz, "xz", "bz2", t.TempDir(), nil))
	gz2 := convertOK(t, c, step(t, bz2, "bz2", "gz", t.TempDir(), nil))
	folder := extractTo(t, c, gz2, "gz")
	if got := read(t, filepath.Join(folder, "data.csv")); got != string(data) {
		t.Fatalf("content differs (%d bytes)", len(got))
	}
}

func TestUnixCompressZ(t *testing.T) {
	dir := t.TempDir()
	data := words(120_000)
	z := filepath.Join(dir, "notes.txt.Z")
	must(t, os.WriteFile(z, compressZ(data), 0o644))
	folder := extractTo(t, New(), z, "z")
	if got := read(t, filepath.Join(folder, "notes.txt")); got != string(data) {
		t.Fatalf(".Z content differs (%d of %d bytes)", len(got), len(data))
	}
}

// ---- extract ----

func TestExtractDoesNotNestASingleTopFolder(t *testing.T) {
	dir := t.TempDir()
	tree := makeTree(t, dir, "photos")
	want := snapshot(t, tree)
	src := filepath.Join(dir, "사진.zip")
	zipDir(t, tree, src, true)
	out := t.TempDir()
	folder := convertOK(t, New(), step(t, src, "zip", formats.Folder, out, nil))
	if folder != filepath.Join(out, "사진") {
		t.Fatalf("folder %s", folder)
	}
	sameSnapshot(t, want, snapshot(t, folder)) // photos/… became 사진/…, not 사진/photos/…
	if n := names(t, out); !slices.Equal(n, []string{"사진"}) {
		t.Fatalf("left behind: %v", n) // no staging folders
	}
}

func TestExtractUnpacksTarGzFully(t *testing.T) {
	dir := t.TempDir()
	tree := makeTree(t, dir, "tree")
	tgz := filepath.Join(dir, "backup.tar.gz")
	tarGzDir(t, tree, tgz)
	out := t.TempDir()
	folder := convertOK(t, New(), step(t, tgz, "tgz", formats.Folder, out, nil))
	if filepath.Base(folder) != "backup" {
		t.Fatalf("folder %s", folder)
	}
	sameSnapshot(t, snapshot(t, tree), snapshot(t, folder)) // files, not a lone backup.tar
}

func TestExtractWithOverwriteMergesIntoTheExistingFolder(t *testing.T) {
	dir := t.TempDir()
	tree := makeTree(t, dir, "data")
	src := filepath.Join(dir, "data.zip")
	zipDir(t, tree, src, false)
	out := t.TempDir()
	target := filepath.Join(out, "data")
	write(t, filepath.Join(target, "readme.txt"), "old")
	write(t, filepath.Join(target, "mine.txt"), "keep")
	convertOK(t, New(), step(t, src, "zip", formats.Folder, out, nil))
	if read(t, filepath.Join(target, "readme.txt")) != "Filee archive test\n" || read(t, filepath.Join(target, "mine.txt")) != "keep" {
		t.Fatal("not merged")
	}
	if _, err := os.Stat(filepath.Join(target, "docs", "보고서", "data.csv")); err != nil {
		t.Fatal(err)
	}
}

func TestSplitArchiveExtractsAllVolumes(t *testing.T) {
	dir := t.TempDir()
	for i := 1; i <= 6; i++ {
		b, err := os.ReadFile(filepath.Join("testdata", "multi.7z.00"+string(rune('0'+i))))
		must(t, err)
		must(t, os.WriteFile(filepath.Join(dir, "multi.7z.00"+string(rune('0'+i))), b, 0o644))
	}
	folder := extractTo(t, New(), filepath.Join(dir, "multi.7z.001"), "split")
	if n := names(t, folder); len(n) != 10 || n[0] != "01" {
		t.Fatalf("entries %v", n)
	}
	if st, _ := os.Stat(filepath.Join(folder, "10")); st == nil || st.Size() != 3987 {
		t.Fatalf("10: %v", st)
	}

	// A split ZIP joined from plain .001/.002 parts.
	tree := makeTree(t, dir, "tree")
	zipPath := filepath.Join(dir, "tree.zip")
	zipDir(t, tree, zipPath, false)
	b, _ := os.ReadFile(zipPath)
	must(t, os.WriteFile(filepath.Join(dir, "parts.zip.001"), b[:len(b)/2], 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "parts.zip.002"), b[len(b)/2:], 0o644))
	got := extractTo(t, New(), filepath.Join(dir, "parts.zip.001"), "split")
	sameSnapshot(t, snapshot(t, tree), snapshot(t, got))
}

func TestSevenZipArchives(t *testing.T) {
	folder := extractTo(t, New(), filepath.Join("testdata", "t0.7z"), "7z")
	if n := names(t, folder); !slices.Equal(n, []string{"bar", "foo"}) {
		t.Fatalf("entries %v", n)
	}
	out := convertOK(t, New(), step(t, filepath.Join("testdata", "t0.7z"), "7z", "tgz", t.TempDir(), nil))
	if f := extractTo(t, New(), out, "tgz"); !slices.Equal(names(t, f), []string{"bar", "foo"}) {
		t.Fatal(names(t, f))
	}
}

func TestTarSymbolicLinksAreNotUnpacked(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "links.tar")
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	tw.WriteHeader(&tar.Header{Name: "real.txt", Size: 4, Mode: 0o644, Typeflag: tar.TypeReg})
	tw.Write([]byte("real"))
	tw.WriteHeader(&tar.Header{Name: "escape", Linkname: "../../../../Windows/win.ini", Typeflag: tar.TypeSymlink})
	tw.WriteHeader(&tar.Header{Name: "inside", Linkname: "real.txt", Typeflag: tar.TypeSymlink})
	tw.WriteHeader(&tar.Header{Name: "hard.txt", Linkname: "real.txt", Typeflag: tar.TypeLink})
	tw.Close()
	must(t, os.WriteFile(p, buf.Bytes(), 0o644))
	folder := extractTo(t, New(), p, "tar")
	if n := names(t, folder); !slices.Equal(n, []string{"hard.txt", "real.txt"}) {
		t.Fatalf("entries %v", n)
	}
	if read(t, filepath.Join(folder, "hard.txt")) != "real" {
		t.Fatal("hard link not copied")
	}
}

func TestZipSlipEntriesAreRejected(t *testing.T) {
	dir := t.TempDir()
	inner := filepath.Join(dir, "inner")
	must(t, os.MkdirAll(inner, 0o755))
	src := filepath.Join(inner, "evil.zip")
	f, _ := os.Create(src)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("good.txt")
	w.Write([]byte("good"))
	w, _ = zw.Create("../../evil.txt")
	w.Write([]byte("evil"))
	zw.Close()
	f.Close()
	out := t.TempDir()
	_, err := New().Convert(ctx, step(t, src, "zip", formats.Folder, out, nil), nil)
	if err == nil || !strings.Contains(err.Error(), "outside the target folder") {
		t.Fatalf("err %v", err)
	}
	if n := names(t, out); len(n) != 0 {
		t.Fatalf("half-extracted: %v", n)
	}
}

func TestPasswordProtectedArchivesFailWithAClearMessage(t *testing.T) {
	dir := t.TempDir()
	// A ZIP entry with the encryption flag (traditional PKWARE encryption).
	src := filepath.Join(dir, "locked.zip")
	f, _ := os.Create(src)
	zw := zip.NewWriter(f)
	w, _ := zw.CreateHeader(&zip.FileHeader{Name: "secret.txt", Method: zip.Store, Flags: 1})
	w.Write(make([]byte, 18))
	zw.Close()
	f.Close()
	for _, c := range [][2]string{{src, "zip"}, {filepath.Join("testdata", "aes7z.7z"), "7z"}} {
		_, err := New().Convert(ctx, step(t, c[0], c[1], "tgz", t.TempDir(), nil), nil)
		if err == nil || !strings.Contains(err.Error(), "password-protected") {
			t.Errorf("%s: %v", c[0], err)
		}
	}
}

func TestDamagedAndFakeArchivesFailCleanly(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake.zip")
	write(t, fake, "this is not a zip")
	out := t.TempDir()
	_, err := New().Convert(ctx, step(t, fake, "zip", formats.Folder, out, nil), nil)
	if err == nil || !strings.Contains(err.Error(), "not a valid archive") {
		t.Fatalf("err %v", err)
	}
	if n := names(t, out); len(n) != 0 {
		t.Fatalf("left behind: %v", n)
	}
	for _, f := range []string{"fake.7z", "fake.tar", "fake.rar", "fake.gz"} {
		p := filepath.Join(dir, f)
		write(t, p, "this is not an archive")
		if _, err := New().Convert(ctx, step(t, p, strings.TrimPrefix(filepath.Ext(f), "."), formats.Folder, t.TempDir(), nil), nil); err == nil {
			t.Errorf("%s: no error", f)
		}
	}
}

// ---- compress into one archive ----

func TestCombinePacksFilesUnderUniqueNames(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	report := filepath.Join(a, "보고서.txt")
	write(t, report, "보고서")
	unknown := filepath.Join(a, "data.xyz")
	must(t, os.WriteFile(unknown, noise(1000, 1), 0o644))
	same := filepath.Join(b, "보고서.txt")
	write(t, same, "다른 보고서")
	tree := makeTree(t, dir, "tree")
	c := New()
	for _, format := range []string{"zip", "tgz", "tbz2", "txz", "7z"} {
		if !c.CanCombineInto(format) {
			if format != "7z" || has7z() {
				t.Errorf("cannot combine into %s", format)
			}
			continue
		}
		out := filepath.Join(t.TempDir(), "all."+formats.ByID(format).PrimaryExtension())
		must(t, c.Combine(ctx, []string{report, unknown, same, tree}, out, format, presets.New("x", format), t.TempDir(), nil))
		folder := extractTo(t, c, out, format)
		if n := names(t, folder); !slices.Equal(n, []string{"data.xyz", "tree", "보고서 (2).txt", "보고서.txt"}) {
			t.Fatalf("%s: %v", format, n)
		}
		if read(t, filepath.Join(folder, "보고서 (2).txt")) != "다른 보고서" {
			t.Fatal("wrong content")
		}
		sameSnapshot(t, snapshot(t, tree), snapshot(t, filepath.Join(folder, "tree")))
	}
}

func TestCombineSingleFileHonoursTheLevel(t *testing.T) {
	dir := t.TempDir()
	text := filepath.Join(dir, "notes.md")
	must(t, os.WriteFile(text, words(100_000), 0o644))
	p := presets.New("x", "zip")
	p.Archive.Level = presets.ArchiveStore
	out := filepath.Join(dir, "notes.zip")
	must(t, New().Combine(ctx, []string{text}, out, "zip", p, t.TempDir(), nil))
	z, err := zip.OpenReader(out)
	must(t, err)
	defer z.Close()
	if len(z.File) != 1 || z.File[0].Name != "notes.md" || z.File[0].CompressedSize64 != z.File[0].UncompressedSize64 {
		t.Fatalf("%+v", z.File[0].FileHeader)
	}
	if err := New().Combine(ctx, []string{text, text}, filepath.Join(dir, "x.gz"), "gz", p, t.TempDir(), nil); err == nil {
		t.Fatal("a .gz holds one file")
	}
}

// ---- Filee's own readers ----

func TestAlzWithStoredDeflatedAndBzip2EntriesAndKoreanNames(t *testing.T) {
	dir := t.TempDir()
	big := words(2_000_000) // three 900k bzip2 blocks
	small := []byte("알집 테스트 ALZ")
	deflated := words(10_000)
	alz := filepath.Join(dir, "알집.alz")
	writeAlz(t, alz, []alzItem2{
		{name: `폴더\`},
		{name: `폴더\저장.txt`, data: small},
		{name: "압축.txt", data: deflated, method: 2},
		{name: "큰 파일.txt", data: big, method: 1, packed: toAlzBzip2(t, bzip2Bytes(t, big))},
	})
	folder := extractTo(t, New(), alz, "alz")
	if read(t, filepath.Join(folder, "폴더", "저장.txt")) != string(small) ||
		read(t, filepath.Join(folder, "압축.txt")) != string(deflated) ||
		read(t, filepath.Join(folder, "큰 파일.txt")) != string(big) {
		t.Fatal("content differs")
	}
	st, _ := os.Stat(filepath.Join(folder, "압축.txt"))
	if d := st.ModTime().Sub(knownTime); d < -2e9 || d > 2e9 {
		t.Errorf("time %v", st.ModTime())
	}
}

func TestAlzRefusesEscapingAndEncryptedEntries(t *testing.T) {
	dir := t.TempDir()
	evil := filepath.Join(dir, "evil.alz")
	writeAlz(t, evil, []alzItem2{{name: `..\..\evil.txt`, data: []byte("x")}})
	if err := extractAlz(ctx, evil, t.TempDir()); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Errorf("escaping: %v", err)
	}
	locked := filepath.Join(dir, "locked.alz")
	writeAlz(t, locked, []alzItem2{{name: "a.txt", data: []byte("x"), encrypted: true}})
	if err := extractAlz(ctx, locked, t.TempDir()); err == nil || !strings.Contains(err.Error(), "password") {
		t.Errorf("encrypted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.txt")); err == nil {
		t.Error("wrote outside")
	}
}

func TestEggWithStoreDeflateBzip2LzmaAndMultiBlockEntries(t *testing.T) {
	dir := t.TempDir()
	a, b, c, d := words(5000), words(7000), []byte("bzip2 블록"), words(30_000)
	egg := filepath.Join(dir, "에그.egg")
	writeEgg(t, egg, []eggItem2{
		{name: "폴더/", folder: true},
		{name: "폴더/stored.txt", blocks: []eggBlock2{{data: a, method: 0}}},
		{name: "multi.txt", blocks: []eggBlock2{{data: a, method: 1}, {data: b, method: 0}}},
		{name: "bz.txt", blocks: []eggBlock2{{data: c, method: 2, packed: bzip2Bytes(t, c)}}},
		{name: "lzma.txt", blocks: []eggBlock2{{data: d, method: 4, packed: eggLzma(t, d)}}},
	}, false)
	folder := extractTo(t, New(), egg, "egg")
	for name, want := range map[string][]byte{"폴더/stored.txt": a, "multi.txt": append(append([]byte{}, a...), b...), "bz.txt": c, "lzma.txt": d} {
		if got := read(t, filepath.Join(folder, filepath.FromSlash(name))); got != string(want) {
			t.Errorf("%s differs", name)
		}
	}
	solid := filepath.Join(dir, "solid.egg")
	writeEgg(t, solid, []eggItem2{{name: "a.txt", blocks: []eggBlock2{{data: a}}}}, true)
	if _, err := New().Convert(ctx, step(t, solid, "egg", formats.Folder, t.TempDir(), nil), nil); err == nil || !strings.Contains(err.Error(), "solid") {
		t.Errorf("solid: %v", err)
	}
}

func TestLzipFilesAndTarLz(t *testing.T) {
	dir := t.TempDir()
	data := words(80_000)
	lz := filepath.Join(dir, "notes.txt.lz")
	// Two members, as lzip writes for big or concatenated files.
	must(t, os.WriteFile(lz, append(lzipBytes(t, data[:30_000]), lzipBytes(t, data[30_000:])...), 0o644))
	folder := extractTo(t, New(), lz, "lz")
	if read(t, filepath.Join(folder, "notes.txt")) != string(data) {
		t.Fatal("lz content differs")
	}

	tree := makeTree(t, dir, "tree")
	tarPath := filepath.Join(dir, "x.tar")
	must(t, New().Combine(ctx, []string{tree}, tarPath, "tar", nil, t.TempDir(), nil))
	tb, _ := os.ReadFile(tarPath)
	tlz := filepath.Join(dir, "backup.tar.lz")
	must(t, os.WriteFile(tlz, lzipBytes(t, tb), 0o644))
	got := extractTo(t, New(), tlz, "tlz")
	sameSnapshot(t, snapshot(t, tree), snapshot(t, got))

	for coded, want := range map[int]uint32{20: 1 << 20, 0xD3: 1<<19 - 6*(1<<19)/16, 12: 4096} {
		if lzipDictionarySize(coded) != want {
			t.Errorf("dictionary %#x: %d", coded, lzipDictionarySize(coded))
		}
	}
}

func TestRarIsRead(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "box.rar")
	writeRar5(t, p, []rarFile{{name: "폴더", dir: true}, {name: "폴더/안녕.txt", data: []byte("안녕 RAR")}, {name: "b.txt", data: []byte("bee")}})
	folder := extractTo(t, New(), p, "rar")
	if read(t, filepath.Join(folder, "폴더", "안녕.txt")) != "안녕 RAR" || read(t, filepath.Join(folder, "b.txt")) != "bee" {
		t.Fatal(names(t, folder))
	}
}

func TestIsoCpioDebAndRpm(t *testing.T) {
	dir := t.TempDir()
	c := New()

	w, err := iso9660.NewWriter()
	must(t, err)
	w.AddFile(strings.NewReader("iso data"), "DOCS/README.TXT")
	w.AddFile(strings.NewReader("second"), "B.TXT")
	isoPath := filepath.Join(dir, "disk.iso")
	f, _ := os.Create(isoPath)
	must(t, w.WriteTo(f, "TEST"))
	f.Close()
	w.Cleanup()
	if !has7z() {
		folder := extractTo(t, c, isoPath, "iso")
		if got := read(t, filepath.Join(folder, "docs", "readme.txt")); got != "iso data" {
			if got2 := read(t, filepath.Join(folder, "DOCS", "README.TXT")); got2 != "iso data" {
				t.Fatalf("iso: %v", names(t, folder))
			}
		}
	}

	cp := cpioNewc(map[string]string{"etc/a.conf": "a=1", "readme": "cpio"}, []string{"etc/", "etc/a.conf", "readme"})
	cpioPath := filepath.Join(dir, "x.cpio")
	must(t, os.WriteFile(cpioPath, cp, 0o644))
	folder := extractTo(t, c, cpioPath, "cpio")
	if read(t, filepath.Join(folder, "etc", "a.conf")) != "a=1" || read(t, filepath.Join(folder, "readme")) != "cpio" {
		t.Fatal(names(t, folder))
	}

	deb := filepath.Join(dir, "pkg.deb")
	must(t, os.WriteFile(deb, arArchive([][2]string{{"debian-binary", "2.0\n"}, {"control.tar.gz", "ctl"}, {"data.tar.xz", "data"}}), 0o644))
	folder = extractTo(t, c, deb, "deb")
	if n := names(t, folder); !slices.Equal(n, []string{"control.tar.gz", "data.tar.xz", "debian-binary"}) {
		t.Fatal(n)
	}

	rpm := filepath.Join(dir, "tool-1.0.rpm")
	must(t, os.WriteFile(rpm, rpmBytes(gzipBytes(cp)), 0o644))
	folder = extractTo(t, c, rpm, "rpm")
	if read(t, filepath.Join(folder, "etc", "a.conf")) != "a=1" {
		t.Fatal(names(t, folder))
	}
}

func TestBzip2AlzDecoderMatchesStandardStreams(t *testing.T) {
	data := words(1_500_000)
	var out bytes.Buffer
	n, err := decompressAlzBzip2(ctx, bytes.NewReader(toAlzBzip2(t, bzip2Bytes(t, data))), &out, int64(len(data)))
	if err != nil || n != int64(len(data)) || !bytes.Equal(out.Bytes(), data) {
		t.Fatalf("n %d err %v", n, err)
	}
	if _, err := decompressAlzBzip2(ctx, bytes.NewReader(toAlzBzip2(t, bzip2Bytes(t, data))), io.Discard, 1000); err == nil {
		t.Fatal("output limit ignored")
	}
}

// ---- helpers ----

func TestUnsafeEntryPaths(t *testing.T) {
	for p, unsafe := range map[string]bool{"a/b.txt": false, "../x": true, "a/../../x": true, "/etc/passwd": true, `\x`: true, "C:/x": true, "c:x": true, "a..b/c": false, "": false} {
		if IsUnsafePath(p) != unsafe {
			t.Errorf("%q: %v", p, !unsafe)
		}
	}
	root := t.TempDir()
	p, err := resolveEntryPath(root, `a/CON.txt/b<c>.txt`, "x")
	must(t, err)
	if want := filepath.Join(root, "a", "_CON.txt", "b_c_.txt"); p != want {
		t.Errorf("%s instead of %s", p, want)
	}
}

func TestListingParser(t *testing.T) {
	out := "7-Zip 23.01\n\nListing archive: x.tar\n\n--\nPath = x.tar\nType = tar\n\n----------\nPath = dir\nFolder = +\nSize = 0\n\nPath = dir/a.txt\nFolder = -\nSize = 12\nMode = -rw-r--r--\n\nPath = link\nSize = 0\nMode = lrwxrwxrwx\nSymbolic Link = /etc/passwd\n\nPath = enc.txt\nSize = 3\nEncrypted = +\n"
	e := parseListing(out)
	if len(e) != 4 || !e[0].Folder || e[1].Size != 12 || !e[2].IsSymbolicLink() || !e[3].Encrypted || e[1].IsSymbolicLink() {
		t.Fatalf("%+v", e)
	}
}

func TestClashingNamesAreNumbered(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "A.txt")
	write(t, p, "x")
	if got := uniquePath(p); got != filepath.Join(dir, "A_1.txt") {
		t.Fatal(got)
	}
	items := uniqueEntries([]string{"/x/a.txt", "/y/A.txt", "/z/a.txt"})
	if items[1].EntryName != "A (2).txt" || items[2].EntryName != "a (3).txt" {
		t.Fatalf("%+v", items)
	}
}

func TestWorkbenchWbpzKeepsEveryEntry(t *testing.T) {
	dir := t.TempDir()
	tree := makeTree(t, dir, "project_files")
	wbpz := filepath.Join(dir, "project.wbpz")
	zipDir(t, tree, wbpz, true)
	if formats.Detect(wbpz).ID != "zip" {
		t.Fatal("wbpz is a ZIP")
	}
	out := convertOK(t, New(), step(t, wbpz, "zip", "zip", t.TempDir(), nil))
	folder := extractTo(t, New(), out, "zip")
	sameSnapshot(t, snapshot(t, tree), snapshot(t, folder))
	fake := filepath.Join(dir, "fake.wbpz")
	write(t, fake, "no archive")
	if _, err := New().Convert(ctx, step(t, fake, "zip", "zip", t.TempDir(), nil), nil); err == nil {
		t.Fatal("fake wbpz converted")
	}
}

func TestCabinetsStoredAndMszip(t *testing.T) {
	dir := t.TempDir()
	files := []cabEntry{{`폴더\a.txt`, words(50_000)}, {"b.bin", noise(70_000, 3)}, {"empty.txt", nil}}
	for _, method := range []int{0, 1} {
		p := filepath.Join(dir, "setup"+string(rune('0'+method))+".cab")
		writeCab(t, p, method, files)
		folder := extractTo(t, New(), p, "cab")
		for _, f := range files {
			if got := read(t, filepath.Join(folder, filepath.FromSlash(strings.ReplaceAll(f.name, `\`, "/")))); got != string(f.data) {
				t.Fatalf("method %d: %s differs", method, f.name)
			}
		}
	}
	lzx := filepath.Join(dir, "lzx.cab")
	writeCab(t, lzx, 3, files[:1])
	if !has7z() {
		if _, err := New().Convert(ctx, step(t, lzx, "cab", formats.Folder, t.TempDir(), nil), nil); err == nil || !strings.Contains(err.Error(), "7-Zip") {
			t.Fatalf("LZX without 7-Zip: %v", err)
		}
	}
}

func TestIsoWithJolietNames(t *testing.T) {
	if has7z() {
		t.Skip("7-Zip reads ISO images when installed")
	}
	p := filepath.Join(t.TempDir(), "disk.iso")
	jolietIso(t, p, map[string]string{"긴 이름의 문서.txt": "joliet", "폴더/안.txt": "inside"})
	folder := extractTo(t, New(), p, "iso")
	if read(t, filepath.Join(folder, "긴 이름의 문서.txt")) != "joliet" || read(t, filepath.Join(folder, "폴더", "안.txt")) != "inside" {
		t.Fatal(names(t, folder))
	}
}
