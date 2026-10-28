package images

import (
	"bytes"
	"image"
	"image/jpeg"
	"os"
	"testing"
	"time"
)

func TestSpeedOfALargePhoto(t *testing.T) {
	if os.Getenv("FILEE_BENCH") == "" {
		t.Skip("set FILEE_BENCH=1 to time a 24-megapixel photo")
	}
	img := image.NewNRGBA(image.Rect(0, 0, 6000, 4000))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 7 / 3)
	}
	var buf bytes.Buffer
	jpeg.Encode(&buf, rgbaView(img), &jpeg.Options{Quality: 85})
	start := time.Now()
	p, err := DecodeBytes(buf.Bytes(), "jpg")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("decode %v", time.Since(start))
	start = time.Now()
	small := Resize(p.Frames[0].Img, 1920, 1280)
	t.Logf("resize %v", time.Since(start))
	start = time.Now()
	f := NewFrame(p.Frames[0].Img)
	f.enc.quality = 90
	if _, err := encodeFrame(f, "jpg", "jpg"); err != nil {
		t.Fatal(err)
	}
	t.Logf("encode jpg %v", time.Since(start))
	start = time.Now()
	if _, err := encodeFrame(f, "png", "png"); err != nil {
		t.Fatal(err)
	}
	t.Logf("encode png %v", time.Since(start))
	start = time.Now()
	f.enc.tiff = "Lzw"
	if _, err := encodeFrame(f, "tiff", "tiff"); err != nil {
		t.Fatal(err)
	}
	t.Logf("encode tiff lzw %v", time.Since(start))
	start = time.Now()
	sf := NewFrame(small)
	sf.enc.quality = 90
	if _, err := encodeFrame(sf, "webp", "webp"); err != nil {
		t.Fatal(err)
	}
	t.Logf("encode webp 1920 %v", time.Since(start))
	start = time.Now()
	if _, err := encodeFrame(sf, "avif", "avif"); err != nil {
		t.Fatal(err)
	}
	t.Logf("encode avif 1920 %v", time.Since(start))
}
