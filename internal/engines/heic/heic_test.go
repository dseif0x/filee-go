package heic

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/gen2brain/heic"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/images"
	"github.com/dseif0x/filee-go/internal/presets"
	"github.com/dseif0x/filee-go/internal/tools"
)

type bitWriter struct {
	b    []byte
	bits int
}

func (w *bitWriter) put(v, n int) {
	for i := n - 1; i >= 0; i-- {
		if w.bits%8 == 0 {
			w.b = append(w.b, 0)
		}
		if v>>i&1 == 1 {
			w.b[len(w.b)-1] |= 0x80 >> (w.bits % 8)
		}
		w.bits++
	}
}

func (w *bitWriter) golomb(v int) {
	v++
	n := 0
	for x := v; x > 1; x >>= 1 {
		n++
	}
	w.put(0, n)
	w.put(v, n+1)
}

func syntheticStream(tiles int) []byte {
	sps := &bitWriter{}
	sps.put(0, 4)
	sps.put(0, 3)
	sps.put(1, 1)
	for i := 0; i < 12; i++ {
		sps.put(i+1, 8)
	}
	sps.golomb(0)
	sps.golomb(1) // 4:2:0
	sps.golomb(512)
	sps.golomb(512)
	sps.put(0, 1)
	sps.golomb(0)
	sps.golomb(0)
	sps.put(1, 1)
	var b bytes.Buffer
	unit := func(header []byte, body []byte) {
		b.Write([]byte{0, 0, 0, 1})
		b.Write(header)
		b.Write(body)
	}
	for t := 0; t < tiles; t++ {
		unit([]byte{0x40, 0x01}, []byte{0x0C, 0x01})
		unit([]byte{0x42, 0x01}, sps.b)
		unit([]byte{0x44, 0x01}, []byte{0xC1, 0x72})
		unit([]byte{0x26, 0x01}, []byte{0x80 | byte(t), 0x55, 0x66})
	}
	return b.Bytes()
}

func TestStreamAndContainer(t *testing.T) {
	s, err := ParseStream(syntheticStream(4))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Pictures) != 4 || s.BitDepth != 8 {
		t.Fatalf("%d pictures, depth %d", len(s.Pictures), s.BitDepth)
	}
	if s.Configuration[0] != 1 || s.Configuration[1] != 1 || s.Configuration[16]&3 != 1 {
		t.Errorf("hvcC %x", s.Configuration[:20])
	}
	cols, rows, tw, th := Layout(1000, 700)
	if cols != 2 || rows != 2 || tw != 512 || th != 512 {
		t.Fatalf("layout %d %d %d %d", cols, rows, tw, th)
	}
	exif := []byte("Exif\x00\x00MM\x00*\x00\x00\x00\x08\x00\x00")
	file, err := WriteHEIF(&Image{Width: 1000, Height: 700, Stream: s, TileWidth: tw, TileHeight: th, Columns: cols, Rows: rows, Exif: exif})
	if err != nil {
		t.Fatal(err)
	}
	if string(file[4:12]) != "ftypheic" {
		t.Fatalf("starts with %q", file[4:12])
	}
	// every tile's bytes are where iloc says: the first one right after the mdat header
	mdat := bytes.Index(file, []byte("mdat"))
	grid := file[mdat+4 : mdat+4+8]
	if grid[2] != 1 || grid[3] != 1 || binary.BigEndian.Uint16(grid[4:]) != 1000 || binary.BigEndian.Uint16(grid[6:]) != 700 {
		t.Errorf("grid description %x", grid)
	}
	if !bytes.Contains(file, s.Pictures[3]) {
		t.Error("the last tile is missing")
	}
	if payload := ExifPayload(exif); binary.BigEndian.Uint32(payload) != 6 {
		t.Errorf("Exif offset %d, want 6", binary.BigEndian.Uint32(payload))
	}
	if Crf(90) != 14 || Crf(80) != 18 || Crf(50) != 30 {
		t.Error("CRF mapping changed")
	}
}

func TestConvertWithFFmpeg(t *testing.T) {
	ffmpeg := tools.Find(tools.Spec{Names: []string{"ffmpeg"}, Env: "FILEE_FFMPEG"})
	if ffmpeg == "" {
		t.Skip("FFmpeg is not installed")
	}
	img := image.NewNRGBA(image.Rect(0, 0, 700, 300))
	for i := range img.Pix {
		img.Pix[i] = byte(i)
	}
	in := filepath.Join(t.TempDir(), "a.png")
	if err := images.Encode(in, img, "png", nil); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	s := &convert.Step{InputPath: in, From: "png", To: "heic", Preset: presets.New("t", "heic"), WorkDir: t.TempDir(),
		Output: convert.AllocatorFunc(func(ext, suffix string) string { return filepath.Join(dir, "out"+suffix+"."+ext) })}
	out, err := Convert(context.Background(), ffmpeg, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(out[0])
	defer f.Close()
	got, err := heic.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds().Dx() != 700 || got.Bounds().Dy() != 300 {
		t.Errorf("decoded %v", got.Bounds())
	}
	_ = color.White
}
