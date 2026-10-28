package cad

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/dseif0x/filee-go/internal/presets"
)

func TestBenchSample(t *testing.T) {
	p := os.Getenv("CAD_BENCH")
	if p == "" {
		t.Skip()
	}
	start := time.Now()
	doc, err := ReadDocument(p, "dxf", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("read", time.Since(start))
	start = time.Now()
	scene, _ := BuildScene(context.Background(), doc)
	t.Log("scene", time.Since(start), len(scene.Items))
	start = time.Now()
	RenderBitmap(scene, 150, Palette{})
	t.Log("raster", time.Since(start))
	start = time.Now()
	po := presets.DefaultPdfOptions()
	f, _ := os.Create(os.DevNull)
	WritePdf(scene, &po, Palette{}, "x", f)
	t.Log("pdf", time.Since(start))
}
