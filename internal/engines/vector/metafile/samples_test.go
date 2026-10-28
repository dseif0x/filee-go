package metafile

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
)

// TestSamples renders the metafiles of METAFILE_DIR to PNG next to them in METAFILE_OUT.
func TestSamples(t *testing.T) {
	dir, out := os.Getenv("METAFILE_DIR"), os.Getenv("METAFILE_OUT")
	if dir == "" || out == "" {
		t.Skip("METAFILE_DIR / METAFILE_OUT not set")
	}
	entries, _ := os.ReadDir(dir)
	fonts := gfx.NewFonts(false)
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".emf") && !strings.HasSuffix(n, ".wmf") {
			continue
		}
		data, _ := os.ReadFile(filepath.Join(dir, n))
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: panic %v", n, r)
				}
			}()
			pic, err := Parse(data)
			if err != nil {
				t.Logf("%s: %v", n, err)
				return
			}
			scale := 400 / max(pic.Width, pic.Height)
			r := gfx.NewRaster(max(1, int(pic.Width*scale)), max(1, int(pic.Height*scale)))
			r.Clear(gfx.White)
			pic.Draw(r, gfx.Scale(scale, scale), fonts)
			f, _ := os.Create(filepath.Join(out, n+".png"))
			png.Encode(f, r.Straight())
			f.Close()
			t.Logf("%s: %.0f x %.0f", n, pic.Width, pic.Height)
		}()
	}
}
