package media

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
)

const liveID = "6F2C8E8A-1A3B-4C5D-9E0F-112233445566"

func testJpeg(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func jpegWithXmp(j []byte, xml string) []byte {
	payload := append(append([]byte{}, xmpHeader...), xml...)
	out := []byte{0xff, 0xd8, 0xff, 0xe1}
	out = binary.BigEndian.AppendUint16(out, uint16(len(payload)+2))
	return append(append(out, payload...), j[2:]...)
}

func motionXmp(length int, elements bool) string {
	n := itoa(length)
	if elements {
		return `<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description xmlns:GCamera="http://ns.google.com/photos/1.0/camera/" xmlns:Container="http://ns.google.com/photos/1.0/container/" xmlns:Item="http://ns.google.com/photos/1.0/container/item/">
<GCamera:MotionPhoto>1</GCamera:MotionPhoto>
<Container:Directory><rdf:Seq>
<rdf:li rdf:parseType="Resource"><Container:Item><Item:Mime>image/jpeg</Item:Mime><Item:Semantic>Primary</Item:Semantic></Container:Item></rdf:li>
<rdf:li rdf:parseType="Resource"><Container:Item><Item:Mime>video/mp4</Item:Mime><Item:Semantic>MotionPhoto</Item:Semantic><Item:Length>` + n + `</Item:Length></Container:Item></rdf:li>
</rdf:Seq></Container:Directory></rdf:Description></rdf:RDF></x:xmpmeta>`
	}
	return `<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description xmlns:GCamera="http://ns.google.com/photos/1.0/camera/" xmlns:Container="http://ns.google.com/photos/1.0/container/" xmlns:Item="http://ns.google.com/photos/1.0/container/item/"
  GCamera:MotionPhoto="1" GCamera:MotionPhotoVersion="1" GCamera:MotionPhotoPresentationTimestampUs="500000">
<Container:Directory><rdf:Seq>
<rdf:li rdf:parseType="Resource"><Container:Item Item:Semantic="Primary" Item:Mime="image/jpeg"/></rdf:li>
<rdf:li rdf:parseType="Resource"><Container:Item Item:Semantic="GainMap" Item:Mime="image/jpeg" Item:Length="0"/></rdf:li>
<rdf:li rdf:parseType="Resource"><Container:Item Item:Mime="video/mp4" Item:Semantic="MotionPhoto" Item:Length="` + n + `" Item:Padding="0"/></rdf:li>
</rdf:Seq></Container:Directory></rdf:Description></rdf:RDF></x:xmpmeta>`
}

// testMovie is a small MOV/MP4 as FFmpeg lays it out (movie header last):
// one video track of frames samples, timescale 1000, 2 s long.
func testMovie(frames uint32) []byte {
	mvhd := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhd[12:], 1000)
	binary.BigEndian.PutUint32(mvhd[16:], 2000)
	binary.BigEndian.PutUint32(mvhd[96:], 2) // next track id
	tkhd := make([]byte, 84)
	binary.BigEndian.PutUint32(tkhd[12:], 1)
	hdlr := append(make([]byte, 8), []byte("vide")...)
	hdlr = append(hdlr, make([]byte, 13)...)
	stsz := append(make([]byte, 8), u32(frames)...)
	trak := MakeBox("trak", MakeBox("tkhd", tkhd), MakeBox("mdia", MakeBox("hdlr", hdlr), MakeBox("minf", MakeBox("stbl", MakeBox("stsz", stsz)))))
	ftyp := MakeBox("ftyp", []byte("qt  \x00\x00\x02\x00qt  "))
	mdat := MakeBox("mdat", bytes.Repeat([]byte{7}, 300))
	return append(append(ftyp, mdat...), MakeBox("moov", MakeBox("mvhd", mvhd), trak)...)
}

func samsungTrailer() (before, after []byte) {
	before = append([]byte{0, 0, 0x30, 0x0a, 16, 0, 0, 0}, "MotionPhoto_Data"...)
	a := append([]byte("SEFH"), 107, 0, 0, 0, 0, 0, 0, 0)
	after = append(append(a, byte(len(a)), 0, 0, 0), "SEFT"...)
	return
}

func find(t *testing.T, data []byte) (int64, int64, bool, error) {
	t.Helper()
	return FindMotionVideo(bytes.NewReader(data))
}

func TestMotionPhotoVideoIsFound(t *testing.T) {
	j := testJpeg(t, 32, 24)
	movie := testMovie(60)
	for _, form := range []string{"attributes", "elements", "samsung", "microvideo"} {
		var before, after []byte
		if form == "samsung" {
			before, after = samsungTrailer()
		}
		length := len(movie) + len(after)
		x := motionXmp(length, form == "elements")
		if form == "microvideo" {
			x = `<x:xmpmeta xmlns:x='adobe:ns:meta/' xmlns:c='http://ns.google.com/photos/1.0/camera/' c:MicroVideo='1' c:MicroVideoVersion='1' c:MicroVideoOffset='` + itoa(length) + `'/>`
		}
		head := append(jpegWithXmp(j, x), before...)
		file := append(append(append([]byte{}, head...), movie...), after...)
		off, l, ok, err := find(t, file)
		if err != nil || !ok || off != int64(len(head)) || l != int64(len(movie)) {
			t.Errorf("%s: %d %d %v %v", form, off, l, ok, err)
		}
	}
	// HEIC with an mpvd box, and Samsung's trailer after it.
	_, after := samsungTrailer()
	heic := append(MakeBox("ftyp", []byte("heic\x00\x00\x00\x00heic")), MakeBox("mpvd", movie)...)
	heic = append(heic, after...)
	off, l, ok, err := find(t, heic)
	if err != nil || !ok || l != int64(len(movie)) || off != 20+8 {
		t.Error(off, l, ok, err)
	}
	// An ordinary JPEG has none.
	if _, _, ok, err := find(t, j); ok || err != nil {
		t.Error("plain jpeg", ok, err)
	}
}

func TestStaleDisabledOrHostileMetadata(t *testing.T) {
	j := testJpeg(t, 16, 16)
	ftyp := MakeBox("ftyp", []byte("isom\x00\x00\x00\x00isom"))
	if _, _, _, err := find(t, jpegWithXmp(j, motionXmp(len(ftyp), false))); !errors.Is(err, ErrInvalidData) {
		t.Error("stale:", err)
	}
	if _, _, _, err := find(t, append(jpegWithXmp(j, motionXmp(len(ftyp), false)), ftyp...)); !errors.Is(err, ErrInvalidData) {
		t.Error("headless:", err)
	}
	disabled := strings.Replace(motionXmp(len(ftyp), false), `GCamera:MotionPhoto="1"`, `GCamera:MotionPhoto="0"`, 1)
	if _, _, ok, err := find(t, append(jpegWithXmp(j, disabled), ftyp...)); ok || err != nil {
		t.Error("disabled:", ok, err)
	}
	movie := append(append([]byte{}, ftyp...), MakeBox("moov", make([]byte, 8))...)
	if _, l, ok, err := find(t, append(jpegWithXmp(j, motionXmp(len(movie), false)+"\x00\x00\x00"), movie...)); !ok || err != nil || l != int64(len(movie)) {
		t.Error("padded:", l, ok, err)
	}
	hostile := jpegWithXmp(j, "<!DOCTYPE x [<!ENTITY leak SYSTEM 'file:///secret'>]><x>&leak;</x>")
	if _, _, _, err := find(t, hostile); !errors.Is(err, ErrInvalidData) {
		t.Error("hostile:", err)
	}
	// Through the engine's extraction: the plain refusal.
	dir := t.TempDir()
	p := filepath.Join(dir, "hostile.jpg")
	os.WriteFile(p, hostile, 0o644)
	_, _, err := ExtractMotion(p, "jpg", dir, nil)
	var r *convert.RefusedError
	if !errors.As(err, &r) || r.Key != "error.no_motion_photo" || r.Detail != "" {
		t.Error(err)
	}
}

func TestMediaBoxesAreBounded(t *testing.T) {
	if _, _, _, err := find(t, append(u32(100), "mpvd"...)); !errors.Is(err, ErrInvalidData) {
		t.Error(err)
	}
	if _, _, _, err := find(t, append(append(u32(1), "mpvd"...), u64(^uint64(0))...)); !errors.Is(err, ErrInvalidData) {
		t.Error(err)
	}
	movie := append(MakeBox("ftyp", []byte("isom\x00\x00\x00\x00isom")), MakeBox("moov", make([]byte, 8))...)
	large := append(MakeBox("ftyp", []byte("heic\x00\x00\x00\x00heic")), append(append(u32(1), "mpvd"...), u64(uint64(len(movie)+16))...)...)
	large = append(large, movie...)
	if _, l, ok, err := find(t, large); !ok || err != nil || l != int64(len(movie)) {
		t.Error(l, ok, err)
	}
	if NeedsInitSegment(bytes.NewReader(append(MakeBox("styp", []byte("msdh")), MakeBox("moof", nil)...))) != true {
		t.Error("media segment")
	}
	if NeedsInitSegment(bytes.NewReader(append(MakeBox("ftyp", []byte("iso6")), MakeBox("moov", nil)...))) {
		t.Error("init segment")
	}
	if NeedsInitSegment(bytes.NewReader([]byte("not boxes at all"))) {
		t.Error("garbage")
	}
}

func TestMotionPhotoIsWrittenAsAPixelDoes(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "clip.mp4")
	movie := testMovie(45)
	os.WriteFile(video, movie, 0o644)
	j, err := WithExif(testJpeg(t, 40, 30), MakerNoteExif([]byte("note")))
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "photo.MP.jpg")
	if err := WriteMotionPhoto(j, video, out, 750000); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out)
	if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
		t.Fatal("not a jpeg any more:", err)
	}
	off, l, ok, err := find(t, data)
	if !ok || err != nil || l != int64(len(movie)) || !bytes.Equal(data[off:off+l], movie) {
		t.Fatal(off, l, ok, err)
	}
	// The XMP comes after the Exif segment, which stays readable.
	if string(TiffMakerNote(JpegExif(data))) != "note" {
		t.Error("exif lost")
	}
	if !bytes.Contains(data, []byte(`GCamera:MotionPhotoPresentationTimestampUs="750000"`)) {
		t.Error("presentation time")
	}
}

func TestAppleLivePhotoIdentifiers(t *testing.T) {
	note := MakerNote(liveID)
	eq(t, strings.ToLower(liveID), ReadMakerNote(note))
	eq(t, "", ReadMakerNote([]byte("Apple iOS\x00\x00\x01MM\x00\x00")))
	dir := t.TempDir()
	photo := filepath.Join(dir, "IMG_0001.JPG")
	j, _ := WithExif(testJpeg(t, 20, 20), MakerNoteExif(note))
	os.WriteFile(photo, j, 0o644)
	eq(t, strings.ToLower(liveID), ImageIdentifier(photo))

	mov := filepath.Join(dir, "IMG_0001.MOV")
	os.WriteFile(mov, testMovie(60), 0o644)
	if id, err := MovieIdentifier(mov); err != nil || id != "" {
		t.Fatal(id, err)
	}
	if err := TagMovie(mov, liveID, 1.5); err != nil {
		t.Fatal(err)
	}
	if id, err := MovieIdentifier(mov); err != nil || id != strings.ToLower(liveID) {
		t.Fatal(id, err)
	}
	f, _ := os.Open(mov)
	_, moov, err := ReadMovieHeader(f, 0)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if n, err := VideoSampleCount(moov); err != nil || n != 60 {
		t.Error(n, err)
	}
	c, _ := Children(moov, 0)
	var meta int
	for _, b := range c {
		if b.Type == "trak" && HandlerOf(b.Payload) == "meta" {
			meta++
			if !bytes.Contains(b.Payload, []byte(stillImageTime)) {
				t.Error("no still image time key")
			}
			if elst := descend(b.Payload, "edts", "elst"); binary.BigEndian.Uint32(elst[8:]) != 1500 {
				t.Error("still at", binary.BigEndian.Uint32(elst[8:]))
			}
		}
	}
	eq(t, 1, meta)
	// Tagging again replaces the identifier rather than adding one.
	other := "11111111-2222-3333-4444-555555555555"
	if err := TagMovie(mov, other, 0); err != nil {
		t.Fatal(err)
	}
	if id, _ := MovieIdentifier(mov); id != other {
		t.Error(id)
	}

	// The pair: the MOV next to the photo counts only with the same identifier.
	os.WriteFile(mov, testMovie(10), 0o644)
	TagMovie(mov, liveID, 0.2)
	video, companion, err := ExtractMotion(photo, "jpg", t.TempDir(), nil)
	if err != nil || companion != mov || !exists(video) {
		t.Fatal(video, companion, err)
	}
	TagMovie(mov, other, 0.2)
	if _, _, err := ExtractMotion(photo, "jpg", t.TempDir(), nil); err == nil {
		t.Error("different identifier accepted")
	}
}

func TestLivpPackage(t *testing.T) {
	dir := t.TempDir()
	livp := filepath.Join(dir, "x.livp")
	f, _ := os.Create(livp)
	z := zip.NewWriter(f)
	w, _ := z.Create("../../evil/IMG_1.HEIC")
	w.Write(testJpeg(t, 8, 8))
	w, _ = z.Create("__MACOSX/._IMG_1.MOV")
	w.Write([]byte("fork"))
	w, _ = z.Create("dir/IMG_1.MOV")
	movie := testMovie(5)
	w.Write(movie)
	z.Close()
	f.Close()
	work := t.TempDir()
	video, companion, err := ExtractMotion(livp, "livp", work, nil)
	if err != nil || companion != "" || filepath.Dir(video) != work {
		t.Fatal(video, companion, err)
	}
	if b, _ := os.ReadFile(video); !bytes.Equal(b, movie) {
		t.Error("video differs")
	}
	data, format, err := ReadLivpStill(livp)
	if err != nil || format != "heic" || len(data) == 0 {
		t.Error(format, err)
	}
}

func TestExifPieces(t *testing.T) {
	tiff := MakerNoteExif([]byte("Apple iOS..."))
	eq(t, "Apple iOS...", string(TiffMakerNote(tiff)))
	eq(t, 1, TiffOrientation(tiff))
	// An Exif with orientation 6 and a thumbnail IFD: upright, no thumbnail.
	be := binary.BigEndian
	b := []byte("MM\x00\x2a")
	b = be.AppendUint32(b, 8)
	b = be.AppendUint16(b, 1)
	b = be.AppendUint16(b, tagOrientation)
	b = be.AppendUint16(b, 3)
	b = be.AppendUint32(b, 1)
	b = append(b, 0, 6, 0, 0)
	b = be.AppendUint32(b, 26)
	b = append(b, 0, 0, 0, 0, 0, 0)
	eq(t, 6, TiffOrientation(b))
	u := UprightExif(b)
	eq(t, 1, TiffOrientation(u))
	eq(t, uint32(0), be.Uint32(u[22:]))
	eq(t, 6, TiffOrientation(b)) // the original is untouched
	j, err := WithExif(testJpeg(t, 4, 4), b)
	if err != nil {
		t.Fatal(err)
	}
	j2, _ := WithExif(j, tiff)
	if bytes.Count(j2, exifPrefix) != 1 || string(TiffMakerNote(JpegExif(j2))) != "Apple iOS..." {
		t.Error("exif not replaced")
	}
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	r := orient(img, 6)
	if r.Bounds().Dx() != 2 || r.Bounds().Dy() != 3 {
		t.Fatal(r.Bounds())
	}
	if c := color.RGBAModel.Convert(r.At(1, 0)).(color.RGBA); c.R != 255 {
		t.Error("orientation 6 turns right", c)
	}
	var pb bytes.Buffer
	png.Encode(&pb, img)
	if PngIcc(pb.Bytes()) != nil {
		t.Error("icc from nothing")
	}
}

func TestPictureBlockAndCover(t *testing.T) {
	eq(t, "image/jpeg", CoverMime([]byte{0xff, 0xd8, 0xff, 0xe0, 0}))
	eq(t, "image/png", CoverMime([]byte("\x89PNG\r\n\x1a\n\x00")))
	eq(t, "", CoverMime([]byte("GIF89a....")))
	eq(t, `a\=b\;c\#d\\e`, EscapeMetadata(`a=b;c#d\e`))
	block := PictureBlock([]byte{1, 2, 3}, "image/png", 10, 20)
	eq(t, "AAAAAwAAAAlpbWFnZS9wbmcAAAAAAAAACgAAABQAAAAYAAAAAAAAAAMBAgM=", block)
}
