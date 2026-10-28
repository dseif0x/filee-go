package webui

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/xuri/excelize/v2"

	"github.com/dseif0x/filee-go/internal/app"
)

// TestServeForBrowser runs the web UI on a fresh data folder with sample
// files for driving it in a real browser (Playwright): FILEE_WEBUI_SERVE=<info.json>
// writes the address and the sample files there and serves until interrupted.
// FILEE_WEBUI_FFMPEG may name an FFmpeg that writes WebM, for a sample video.
func TestServeForBrowser(t *testing.T) {
	out := os.Getenv("FILEE_WEBUI_SERVE")
	if out == "" {
		t.Skip("set FILEE_WEBUI_SERVE to serve the web UI for a browser")
	}
	dir := os.Getenv("FILEE_WEBUI_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	files := writeSamples(t, filepath.Join(dir, "samples"))
	a, err := app.New(app.Options{DataDir: filepath.Join(dir, "data"), WorkRoot: filepath.Join(dir, "work")})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer stop()
	base, err := Start(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"base": base, "files": files, "dir": dir})
	if err := os.WriteFile(out, b, 0o644); err != nil {
		t.Fatal(err)
	}
	<-ctx.Done()
}

func writeSamples(t *testing.T, dir string) map[string]string {
	t.Helper()
	_ = os.MkdirAll(dir, 0o755)
	files := map[string]string{}
	img := image.NewRGBA(image.Rect(0, 0, 640, 420))
	for y := 0; y < 420; y++ {
		for x := 0; x < 640; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 255 / 640), uint8(y * 255 / 420), 180, 255})
		}
	}
	for x := 200; x < 440; x++ {
		for y := 150; y < 270; y++ {
			img.Set(x, y, color.RGBA{250, 250, 250, 255})
		}
	}
	write := func(name string, f func(*os.File) error) string {
		p := filepath.Join(dir, name)
		fh, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := f(fh); err != nil {
			t.Fatal(err)
		}
		fh.Close()
		return p
	}
	files["png"] = write("sunset.png", func(f *os.File) error { return png.Encode(f, img) })
	files["jpg"] = write("photo.jpg", func(f *os.File) error { return jpeg.Encode(f, img, nil) })
	files["jpg2"] = write("second.jpg", func(f *os.File) error { return jpeg.Encode(f, img.SubImage(image.Rect(0, 0, 400, 400)), nil) })
	files["pdf"] = filepath.Join(dir, "pages.pdf")
	if err := api.ImportImagesFile(context.Background(), []string{files["png"], files["jpg"], files["jpg2"]}, files["pdf"], nil, nil); err != nil {
		t.Fatal(err)
	}
	x := excelize.NewFile()
	for r := 1; r <= 40; r++ {
		for c := 1; c <= 8; c++ {
			cell, _ := excelize.CoordinatesToCellName(c, r)
			_ = x.SetCellValue("Sheet1", cell, r*c)
		}
	}
	_, _ = x.NewSheet("Wide")
	for c := 1; c <= 70; c++ {
		cell, _ := excelize.CoordinatesToCellName(c, 1)
		_ = x.SetCellValue("Wide", cell, c)
	}
	files["xlsx"] = filepath.Join(dir, "budget.xlsx")
	if err := x.SaveAs(files["xlsx"]); err != nil {
		t.Fatal(err)
	}
	if ff := os.Getenv("FILEE_WEBUI_FFMPEG"); ff != "" {
		p := filepath.Join(dir, "clip.webm")
		// This FFmpeg reads files only (no pipes): the frames go into one MJPEG stream file.
		frames, err := os.Create(filepath.Join(dir, "frames.mjpeg"))
		if err != nil {
			t.Fatal(err)
		}
		frame := image.NewRGBA(image.Rect(0, 0, 320, 180))
		for n := 0; n < 80; n++ {
			for y := 0; y < 180; y++ {
				for xx := 0; xx < 320; xx++ {
					frame.Set(xx, y, color.RGBA{uint8((xx + n*4) % 256), uint8(y), uint8(n * 3), 255})
				}
			}
			_ = jpeg.Encode(frames, frame, nil)
		}
		frames.Close()
		cmd := exec.Command(ff, "-y", "-f", "image2pipe", "-framerate", "10", "-c:v", "mjpeg", "-i", frames.Name(), "-c:v", "libvpx-vp9", "-b:v", "200k", p)
		if out, err := cmd.CombinedOutput(); err == nil {
			files["webm"] = p
		} else {
			t.Logf("no sample video: %v %s", err, out)
		}
		_ = os.Remove(frames.Name())
	}
	return files
}
