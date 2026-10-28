package cad

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
	"github.com/dseif0x/filee-go/internal/presets"
)

// TestSampleDrawings renders the DXF/DWG files of CAD_SAMPLES to CAD_OUT (PNG and PDF).
func TestSampleDrawings(t *testing.T) {
	dir, out := os.Getenv("CAD_SAMPLES"), os.Getenv("CAD_OUT")
	if dir == "" || out == "" {
		t.Skip("CAD_SAMPLES / CAD_OUT not set")
	}
	fonts = func() *gfx.Fonts { return testFonts }
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		ext := strings.TrimPrefix(filepath.Ext(n), ".")
		if ext != "dxf" && ext != "dwg" {
			continue
		}
		doc, err := ReadDocument(filepath.Join(dir, n), ext, nil)
		if err != nil {
			t.Logf("%s: %v", n, err)
			continue
		}
		scene, err := BuildScene(context.Background(), doc)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: %d items, skipped %s", n, len(scene.Items), scene.SkippedSummary())
		r, _ := RenderBitmap(scene, 60, Palette{})
		f, _ := os.Create(filepath.Join(out, n+".png"))
		writePNG(f, r)
		f.Close()
		pf, _ := os.Create(filepath.Join(out, n+".pdf"))
		po := presets.DefaultPdfOptions()
		sheets, _ := BuildLayouts(context.Background(), doc)
		if len(sheets) > 0 {
			WritePdfSheets(sheets, scene, &po, Palette{}, n, pf)
		} else {
			WritePdf(scene, &po, Palette{}, n, pf)
		}
		pf.Close()
	}
}
