package images

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/tiff"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/presets"
)

// step builds a conversion of the file at input whose outputs go to t.TempDir().
func step(t *testing.T, input, from, to string, edit func(*presets.Preset)) *convert.Step {
	t.Helper()
	dir := t.TempDir()
	p := presets.New("test", to)
	if edit != nil {
		edit(p)
	}
	return &convert.Step{
		InputPath: input, From: from, To: to, Preset: p, WorkDir: t.TempDir(),
		Output: convert.AllocatorFunc(func(ext, suffix string) string {
			return filepath.Join(dir, "out"+suffix+"."+ext)
		}),
	}
}

func write(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func encodeTo(t *testing.T, img image.Image, format string) []byte {
	t.Helper()
	data, err := EncodeImage(img, format, nil)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func run(t *testing.T, s *convert.Step) []string {
	t.Helper()
	out, err := New().Convert(context.Background(), s, nil)
	if err != nil {
		t.Fatalf("%s → %s: %v", s.From, s.To, err)
	}
	return out
}

func decodeFile(t *testing.T, path string) *Picture {
	t.Helper()
	p, err := Decode(path, "", nil)
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return p
}

func TestEdgesAndCosts(t *testing.T) {
	c := New()
	edges := c.Edges()
	if want := len(Sources()) * len(Writable); len(edges) < want-len(Writable) {
		t.Fatalf("%d edges, want about %d", len(edges), want)
	}
	cost := map[string]int{}
	for _, e := range edges {
		cost[e.From+">"+e.To] = e.Cost
	}
	for edge, want := range map[string]int{"png>gif": 12, "gif>gif": 10, "tiff>gif": 10, "jpg>apng": 12, "webp>apng": 10, "fits>tiff": 11, "fits>png": 10, "png>jpg": 10, "livp>png": 10} {
		if cost[edge] != want {
			t.Errorf("%s costs %d, want %d", edge, cost[edge], want)
		}
	}
	if _, ok := cost["png>heic"]; ok {
		t.Error("HEIC is written by FFmpeg, not by this engine")
	}
	if c.ID() != "magick" {
		t.Error("the id must stay magick")
	}
}

// jpegWithOrientation is a JPEG of img whose Exif says it is turned (orientation 6: turn right to view).
func jpegWithOrientation(t *testing.T, img image.Image, orientation int, quality int) []byte {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		t.Fatal(err)
	}
	exif := []byte("MM\x00*\x00\x00\x00\x08\x00\x01\x01\x12\x00\x03\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00\x00\x00")
	binary.BigEndian.PutUint16(exif[18:], uint16(orientation))
	return jpegWithMeta(buf.Bytes(), &Meta{Exif: exif})
}

func TestOrientationQualityAndResize(t *testing.T) {
	src := testPicture(false) // 40 × 30
	in := write(t, "photo.jpg", jpegWithOrientation(t, src, 6, 75))
	s := step(t, in, "jpg", "jpg", func(p *presets.Preset) {
		p.Image.Resize = presets.ResizeLongEdge
		p.Image.LongEdge = 20
		p.Image.Rotation = presets.Right90
	})
	out := run(t, s)
	data, _ := os.ReadFile(out[0])
	pic := decodeFile(t, out[0])
	f := pic.Frames[0]
	// upright (30 × 40), turned right once more (40 × 30), long edge 20 → 20 × 15
	if f.Width() != 20 || f.Height() != 15 {
		t.Fatalf("size %dx%d, want 20x15", f.Width(), f.Height())
	}
	if f.Meta.Orientation != 1 {
		t.Errorf("orientation %d after turning, want 1", f.Meta.Orientation)
	}
	// turning re-encodes at the photo's own quality (75), not the preset's 90
	if q := jpegMeta(data).Quality; q < 70 || q > 80 {
		t.Errorf("quality %d, want about 75", q)
	}
}

func TestMetadataKeptAndStripped(t *testing.T) {
	icc := bytes.Repeat([]byte("ICCDATA."), 20)
	f := NewFrame(testPicture(false))
	f.Meta.ICC = icc
	f.Meta.Exif = []byte("II*\x00\x08\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	f.Meta.DpiX, f.Meta.DpiY = 300, 300
	png0, err := encodePNG(f)
	if err != nil {
		t.Fatal(err)
	}
	in := write(t, "a.png", png0)
	for _, target := range []string{"png", "jpg", "webp", "tiff"} {
		out := run(t, step(t, in, "png", target, nil))
		got := decodeFile(t, out[0]).Frames[0].Meta
		if !bytes.Equal(got.ICC, icc) {
			t.Errorf("%s lost the colour profile", target)
		}
		if target != "tiff" && got.Exif == nil {
			t.Errorf("%s lost the Exif data", target)
		}
		if target != "webp" && math.Round(got.DpiX) != 300 {
			t.Errorf("%s resolution %v, want 300", target, got.DpiX)
		}
		out = run(t, step(t, in, "png", target, func(p *presets.Preset) { p.Image.KeepMetadata = false; p.Image.KeepColorProfile = false }))
		got = decodeFile(t, out[0]).Frames[0].Meta
		if got.ICC != nil || got.Exif != nil {
			t.Errorf("%s kept metadata it was told to remove", target)
		}
	}
}

func animatedGIF(t *testing.T, frames int, delay int) []byte {
	g := &gif.GIF{LoopCount: 0}
	pal := color.Palette{color.NRGBA{0, 0, 0, 255}, color.NRGBA{255, 0, 0, 255}, color.NRGBA{0, 0, 255, 255}, color.NRGBA{}}
	for i := 0; i < frames; i++ {
		img := image.NewPaletted(image.Rect(0, 0, 32, 32), pal)
		for y := 0; y < 32; y++ {
			for x := 0; x < 32; x++ {
				img.SetColorIndex(x, y, uint8((x/8+i)%3))
			}
		}
		g.Image = append(g.Image, img)
		g.Delay = append(g.Delay, delay)
		g.Disposal = append(g.Disposal, gif.DisposalNone)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestAnimationsKeepFramesAndSpeed(t *testing.T) {
	in := write(t, "anim.gif", animatedGIF(t, 6, 10))
	for _, target := range []string{"webp", "apng", "gif", "mng"} {
		out := run(t, step(t, in, "gif", target, nil))
		if len(out) != 1 {
			t.Fatalf("%s: %d files", target, len(out))
		}
		pic, err := Decode(out[0], target, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(pic.Frames) != 6 {
			t.Errorf("%s: %d frames, want 6", target, len(pic.Frames))
		}
		if ms := pic.Frames[1].DelayMs(); math.Abs(ms-100) > 1 {
			t.Errorf("%s: frame delay %v ms, want 100", target, ms)
		}
	}
	// twice as fast
	out := run(t, step(t, in, "gif", "webp", func(p *presets.Preset) { p.Image.AnimationSpeed = 2 }))
	pic := decodeFile(t, out[0])
	if ms := pic.Frames[0].DelayMs(); math.Abs(ms-50) > 1 {
		t.Errorf("sped-up delay %v ms, want 50", ms)
	}
	// four times as fast: 25 ms frames would be under 20 ms after... still 25 ms, kept
	frames := ChangeSpeed(decodeFile(t, in).Frames, 10, "gif")
	for _, f := range frames {
		if f.DelayMs() < MinDelayMs {
			t.Errorf("delay %v ms below the minimum", f.DelayMs())
		}
	}
	if len(frames) >= 6 {
		t.Errorf("10× speed kept %d frames; frames under 20 ms should be dropped", len(frames))
	}
	// a still target takes the first frame
	out = run(t, step(t, in, "gif", "png", nil))
	if len(out) != 1 {
		t.Errorf("GIF → PNG wrote %d files", len(out))
	}
	// TIFF takes every frame as a page
	out = run(t, step(t, in, "gif", "tiff", nil))
	if n := len(decodeFile(t, out[0]).Frames); n != 6 {
		t.Errorf("GIF → TIFF: %d pages", n)
	}
}

func TestAnimatedPNGSavedAsPNG(t *testing.T) {
	f1, f2 := NewFrame(testPicture(true)), NewFrame(testPicture(false))
	f1.Delay, f1.Ticks, f2.Delay, f2.Ticks = 50, 1000, 70, 1000
	data, err := encodeAPNG([]*Frame{f1, f2}, 3)
	if err != nil {
		t.Fatal(err)
	}
	in := write(t, "moving.png", data)
	out := run(t, step(t, in, "png", "gif", nil))
	pic := decodeFile(t, out[0])
	if len(pic.Frames) != 2 {
		t.Fatalf("%d frames, want 2", len(pic.Frames))
	}
	// the standard PNG decoder sees the first frame
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil || img.Bounds().Dx() != 40 {
		t.Fatalf("not a valid PNG: %v", err)
	}
}

func TestMultiPageTIFF(t *testing.T) {
	a := write(t, "a.png", encodeTo(t, testPicture(true), "png"))
	b := write(t, "b.png", encodeTo(t, testPicture(false), "png"))
	for _, comp := range []presets.TiffCompression{presets.TiffNone, presets.TiffLzw, presets.TiffZip} {
		out := filepath.Join(t.TempDir(), "merged.tiff")
		o := presets.DefaultImageOptions()
		o.TiffCompression = comp
		o.Background = "#000000"
		if err := New().MergeTiffs(context.Background(), []string{a, b}, out, &o); err != nil {
			t.Fatal(err)
		}
		pic := decodeFile(t, out)
		if len(pic.Frames) != 2 {
			t.Fatalf("%s: %d pages", comp, len(pic.Frames))
		}
		// flattened onto the background
		if c := pic.Frames[0].Img.NRGBAAt(0, 0); c != (color.NRGBA{0, 0, 0, 255}) {
			t.Errorf("%s: corner %v, want black background", comp, c)
		}
		if d := meanDiff(testPicture(false), pic.Frames[1].Img); d > 0.01 {
			t.Errorf("%s: page 2 differs by %v", comp, d)
		}
		// x/image/tiff reads it independently
		data, _ := os.ReadFile(out)
		if _, err := tiff.Decode(bytes.NewReader(data)); err != nil {
			t.Errorf("%s: %v", comp, err)
		}
	}
}

func TestIconSizes(t *testing.T) {
	in := write(t, "logo.png", encodeTo(t, testPicture(true), "png"))
	out := run(t, step(t, in, "png", "ico", nil))
	pic := decodeFile(t, out[0])
	var sizes []int
	for _, f := range pic.Frames {
		sizes = append(sizes, f.Width())
		if f.Width() != f.Height() {
			t.Errorf("icon %dx%d is not square", f.Width(), f.Height())
		}
	}
	if fmt.Sprint(sizes) != "[256 128 64 48 32 16]" {
		t.Errorf("sizes %v", sizes)
	}
	// resized on purpose below 256: that size and the smaller ones asked for
	out = run(t, step(t, in, "png", "ico", func(p *presets.Preset) {
		p.Image.Resize = presets.ResizeLongEdge
		p.Image.LongEdge = 20
	}))
	sizes = nil
	for _, f := range decodeFile(t, out[0]).Frames {
		sizes = append(sizes, f.Width())
	}
	if fmt.Sprint(sizes) != "[20 16]" {
		t.Errorf("resized icon sizes %v, want [20 16]", sizes)
	}
	// an icon converts from its largest size
	out = run(t, step(t, out[0], "ico", "png", nil))
	if f := decodeFile(t, out[0]).Frames[0]; f.Width() != 20 {
		t.Errorf("ICO → PNG took the %d px picture", f.Width())
	}
}

func TestTrimTransparent(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 50, 40))
	for y := 10; y < 20; y++ {
		for x := 5; x < 30; x++ {
			img.SetNRGBA(x, y, color.NRGBA{200, 10, 10, 255})
		}
	}
	in := write(t, "t.png", encodeTo(t, img, "png"))
	out := run(t, step(t, in, "png", "png", func(p *presets.Preset) { p.Image.TrimTransparent = true }))
	f := decodeFile(t, out[0]).Frames[0]
	if f.Width() != 25 || f.Height() != 10 {
		t.Errorf("trimmed to %dx%d, want 25x10", f.Width(), f.Height())
	}
	_, err := New().Convert(context.Background(), step(t, in, "png", "jpg", func(p *presets.Preset) { p.Image.TrimTransparent = true }), nil)
	var refused *convert.RefusedError
	if err == nil || !errorsAs(err, &refused) || refused.Key != "error.no_transparency" {
		t.Errorf("trim to JPG: %v, want error.no_transparency", err)
	}
}

func TestRemoveBackground(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	fill(img, color.NRGBA{250, 250, 245, 255})
	for y := 20; y < 44; y++ {
		for x := 20; x < 44; x++ {
			img.SetNRGBA(x, y, color.NRGBA{10, 10, 60, 255})
		}
	}
	f := NewFrame(img)
	RemoveBackground(f, 25, false)
	if a := f.Img.NRGBAAt(2, 2).A; a != 0 {
		t.Errorf("paper alpha %d, want 0", a)
	}
	if c := f.Img.NRGBAAt(32, 32); c.A != 255 || c.B < 50 {
		t.Errorf("ink %v, want opaque blue-black", c)
	}
}

func TestHideAreas(t *testing.T) {
	img := testPicture(false)
	big := Resize(img, 400, 300)
	for _, style := range []presets.HideStyle{presets.HidePixelate, presets.HideBlur} {
		f := NewFrame(big)
		f.Img = crop(big, big.Rect) // a copy
		f.Meta.Exif = jpegMetaExifWithThumb()
		o := presets.DefaultImageOptions()
		o.HideStyle = style
		o.HideStrength = 5
		o.HiddenAreas = []presets.HiddenArea{{X: 0.25, Y: 0.25, Width: 0.5, Height: 0.5}}
		HideAreas(f, &o)
		if d := meanDiff(crop(big, image.Rect(0, 0, 90, 70)), crop(f.Img, image.Rect(0, 0, 90, 70))); d != 0 {
			t.Errorf("%s changed pixels outside the area (%v)", style, d)
		}
		if d := meanDiff(crop(big, image.Rect(100, 75, 300, 225)), crop(f.Img, image.Rect(100, 75, 300, 225))); d == 0 {
			t.Errorf("%s left the area as it was", style)
		}
		if exifHasThumbnail(f.Meta.Exif) {
			t.Errorf("%s kept the Exif thumbnail", style)
		}
	}
}

// jpegMetaExifWithThumb is Exif with an IFD1 thumbnail of 4 bytes.
func jpegMetaExifWithThumb() []byte {
	b := []byte("II*\x00\x08\x00\x00\x00" + // header, IFD0 at 8
		"\x00\x00" + "\x0e\x00\x00\x00" + // no entries, IFD1 at 14
		"")
	// IFD1 at 14: two entries (offset, length), next 0
	ifd1 := make([]byte, 2+2*12+4)
	binary.LittleEndian.PutUint16(ifd1, 2)
	e := ifd1[2:]
	binary.LittleEndian.PutUint16(e, tagThumbOffset)
	binary.LittleEndian.PutUint16(e[2:], 4)
	binary.LittleEndian.PutUint32(e[4:], 1)
	binary.LittleEndian.PutUint32(e[8:], 44)
	e = ifd1[14:]
	binary.LittleEndian.PutUint16(e, tagThumbLength)
	binary.LittleEndian.PutUint16(e[2:], 4)
	binary.LittleEndian.PutUint32(e[4:], 1)
	binary.LittleEndian.PutUint32(e[8:], 4)
	b[10] = 14
	b = append(b[:14], ifd1...)
	return append(b, 0xFF, 0xD8, 0xFF, 0xD9)
}

func TestMaxSize(t *testing.T) {
	rnd := rand.New(rand.NewSource(1))
	img := image.NewNRGBA(image.Rect(0, 0, 1600, 1200))
	for i := range img.Pix {
		img.Pix[i] = uint8(rnd.Intn(256))
		if i%4 == 3 {
			img.Pix[i] = 255
		}
	}
	in := write(t, "noise.png", encodeTo(t, img, "png"))
	out := run(t, step(t, in, "png", "jpg", func(p *presets.Preset) { p.Image.MaxSizeMb = 0.3 }))
	st, _ := os.Stat(out[0])
	if st.Size() > 300_000 {
		t.Errorf("%d bytes over the 0.3 MB limit", st.Size())
	}
	_, err := New().Convert(context.Background(), step(t, in, "png", "png", func(p *presets.Preset) { p.Image.MaxSizeMb = 0.01 }), nil)
	var refused *convert.RefusedError
	if !errorsAs(err, &refused) || refused.Key != "error.over_size_limit" {
		t.Errorf("impossible limit: %v", err)
	}
}

func TestJoinImages(t *testing.T) {
	a := write(t, "a.png", encodeTo(t, testPicture(false), "png"))                 // 40 × 30
	b := write(t, "b.png", encodeTo(t, Resize(testPicture(false), 80, 20), "png")) // 80 × 20
	out := filepath.Join(t.TempDir(), "joined.png")
	o := presets.DefaultImageOptions()
	o.JoinGap = 10
	err := New().JoinImages(context.Background(), []convert.ImageInput{{Path: a, Format: "png"}, {Path: b, Format: "png"}}, out, "png", &o, nil)
	if err != nil {
		t.Fatal(err)
	}
	f := decodeFile(t, out).Frames[0]
	// same width (80): 40 × 30 becomes 80 × 60; 60 + 10 + 20
	if f.Width() != 80 || f.Height() != 90 {
		t.Errorf("joined %dx%d, want 80x90", f.Width(), f.Height())
	}
	o.JoinDirection, o.JoinFit = presets.JoinHorizontal, presets.JoinOriginal
	out = filepath.Join(t.TempDir(), "joined.jpg")
	if err := New().JoinImages(context.Background(), []convert.ImageInput{{Path: a, Format: "png"}, {Path: b, Format: "png"}}, out, "jpg", &o, nil); err != nil {
		t.Fatal(err)
	}
	if f := decodeFile(t, out).Frames[0]; f.Width() != 130 || f.Height() != 30 {
		t.Errorf("side by side %dx%d, want 130x30", f.Width(), f.Height())
	}
}

func TestNetpbmKeepsSubformat(t *testing.T) {
	g := image.NewGray(image.Rect(0, 0, 8, 4))
	for i := range g.Pix {
		g.Pix[i] = uint8(i * 8)
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "P2\n# plain\n8 4\n255\n")
	for _, v := range g.Pix {
		fmt.Fprintf(&buf, "%d ", v)
	}
	in := write(t, "x.pgm", buf.Bytes())
	out := run(t, step(t, in, "ppm", "ppm", nil))
	if !strings.HasSuffix(out[0], ".pgm") {
		t.Errorf("wrote %s, want a .pgm", out[0])
	}
	f := decodeFile(t, out[0]).Frames[0]
	if f.Img.NRGBAAt(3, 1).R != g.Pix[11] {
		t.Error("grey values changed")
	}
}

func TestLivePhoto(t *testing.T) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	w, _ := z.Create("IMG_0001.JPG")
	jpeg.Encode(w, testPicture(false), nil)
	w, _ = z.Create("IMG_0001.MOV")
	w.Write([]byte("not a movie"))
	z.Close()
	in := write(t, "a.livp", buf.Bytes())
	out := run(t, step(t, in, "livp", "png", nil))
	if f := decodeFile(t, out[0]).Frames[0]; f.Width() != 40 {
		t.Errorf("LIVP photo %dx%d", f.Width(), f.Height())
	}
}

func TestPagesGetSuffixes(t *testing.T) {
	f1, f2 := NewFrame(testPicture(false)), NewFrame(testPicture(true))
	data, err := encodeTIFF([]*Frame{f1, f2})
	if err != nil {
		t.Fatal(err)
	}
	in := write(t, "two.tiff", data)
	out := run(t, step(t, in, "tiff", "png", nil))
	if len(out) != 2 || !strings.HasSuffix(out[0], "out_p1.png") || !strings.HasSuffix(out[1], "out_p2.png") {
		t.Errorf("outputs %v", out)
	}
}

func errorsAs(err error, target **convert.RefusedError) bool {
	r, ok := err.(*convert.RefusedError)
	if ok {
		*target = r
	}
	return ok
}

func TestPictureSizeAndWritePNG(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"png", "qoi", "psd", "tga", "webp"} {
		path := filepath.Join(dir, "p."+f)
		if err := Encode(path, image.NewNRGBA(image.Rect(0, 0, 7, 5)), f, nil); err != nil {
			t.Fatal(err)
		}
		if w, h, ok := PictureSize(path); !ok || w != 7 || h != 5 {
			t.Errorf("%s: %d × %d %v", f, w, h, ok)
		}
		var buf bytes.Buffer
		if err := WritePNG(path, &buf); err != nil {
			t.Errorf("%s: %v", f, err)
		}
		if cfg, err := png.DecodeConfig(&buf); err != nil || cfg.Width != 7 {
			t.Errorf("%s: %v", f, err)
		}
	}
	if _, _, ok := PictureSize(filepath.Join(dir, "missing.png")); ok {
		t.Error("a missing file has no size")
	}
}
