package icns

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/presets"
)

func step(t *testing.T, input, from, to string) *convert.Step {
	dir := t.TempDir()
	return &convert.Step{InputPath: input, From: from, To: to, Preset: presets.New("t", to), WorkDir: t.TempDir(),
		Output: convert.AllocatorFunc(func(ext, suffix string) string { return filepath.Join(dir, "out"+suffix+"."+ext) })}
}

func TestIconRoundTrip(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 60, 30))
	for y := 0; y < 30; y++ {
		for x := 0; x < 60; x++ {
			img.SetNRGBA(x, y, color.NRGBA{200, uint8(x * 4), 10, 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	in := filepath.Join(t.TempDir(), "logo.png")
	os.WriteFile(in, buf.Bytes(), 0o644)
	out, err := New().Convert(context.Background(), step(t, in, "png", "icns"), nil)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out[0])
	entries, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 11 || entries[0].Type != "TOC " {
		t.Fatalf("%d entries, want the TOC and 10 icons", len(entries))
	}
	ic10, err := png.Decode(bytes.NewReader(entries[10].Data))
	if err != nil || ic10.Bounds().Dx() != 1024 {
		t.Fatalf("ic10: %v", err)
	}
	// centred on a transparent square
	if _, _, _, a := ic10.At(512, 10).RGBA(); a != 0 {
		t.Error("the padding is not transparent")
	}
	back, err := New().Convert(context.Background(), step(t, out[0], "icns", "png"), nil)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(back[0])
	defer f.Close()
	got, err := png.Decode(f)
	if err != nil || got.Bounds().Dx() != 1024 {
		t.Fatalf("ICNS → PNG: %v %v", err, got.Bounds())
	}
}

func TestLegacyEntries(t *testing.T) {
	const size = 16
	planes := make([]byte, size*size*3)
	for i := range planes {
		planes[i] = byte(i / (size * size) * 100)
	}
	mask := bytes.Repeat([]byte{200}, size*size)
	file := Write([]Entry{{"is32", PackBits(planes)}, {"s8mk", mask}})
	entries, err := Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	c := Candidates(entries)
	if len(c) != 1 || c[0].Size != 16 {
		t.Fatalf("candidates %+v", c)
	}
	bm, _ := c[0].Decode()
	if bm.RGBA[0] != 0 || bm.RGBA[1] != 100 || bm.RGBA[2] != 200 || bm.RGBA[3] != 200 {
		t.Errorf("pixel %v", bm.RGBA[:4])
	}
}
