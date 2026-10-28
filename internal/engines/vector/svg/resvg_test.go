package svg

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
)

// TestResvgSuite compares renders with the reference pictures of resvg's test
// suite (set RESVG_TESTS to its crates/resvg/tests/tests folder).
func TestResvgSuite(t *testing.T) {
	root := os.Getenv("RESVG_TESTS")
	if root == "" {
		t.Skip("RESVG_TESTS not set")
	}
	var files []string
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && strings.HasSuffix(p, ".svg") && (os.Getenv("RESVG_FILTER") == "" || strings.Contains(p, os.Getenv("RESVG_FILTER"))) {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	fonts := gfx.NewFonts(false)
	fontDir := filepath.Join(filepath.Dir(root), "fonts")
	entries, _ := os.ReadDir(fontDir)
	for _, e := range entries {
		if n := e.Name(); strings.HasSuffix(n, ".ttf") || strings.HasSuffix(n, ".otf") {
			fonts.AddFontFile(filepath.Join(fontDir, n))
		}
	}
	type res struct {
		name string
		diff float64
	}
	var results []res
	pass := 0
	for _, f := range files {
		ref := strings.TrimSuffix(f, ".svg") + ".png"
		rf, err := os.Open(ref)
		if err != nil {
			continue
		}
		want, err := png.Decode(rf)
		rf.Close()
		if err != nil {
			continue
		}
		data, _ := os.ReadFile(f)
		diff := 1.0
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Logf("PANIC %s: %v", f, r)
				}
			}()
			d, err := Parse(data, filepath.Dir(f))
			if err != nil {
				return
			}
			d.Fonts = fonts
			b := want.Bounds()
			r := gfx.NewRaster(b.Dx(), b.Dy())
			d.Draw(r, gfx.Scale(float64(b.Dx())/d.Width, float64(b.Dy())/d.Height), Options{})
			diff = compare(r.Straight(), want)
			if dir := os.Getenv("RESVG_SAVE"); dir != "" {
				name := strings.ReplaceAll(strings.TrimPrefix(f, root+"/"), "/", "_")
				out, _ := os.Create(filepath.Join(dir, strings.TrimSuffix(name, ".svg")+".png"))
				png.Encode(out, sideBySide(r.Straight(), want))
				out.Close()
			}
		}()
		rel, _ := filepath.Rel(root, f)
		results = append(results, res{rel, diff})
		if diff < 0.02 {
			pass++
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].diff > results[j].diff })
	var b strings.Builder
	for _, r := range results {
		fmt.Fprintf(&b, "%.3f %s\n", r.diff, r.name)
	}
	if out := os.Getenv("RESVG_REPORT"); out != "" {
		os.WriteFile(out, []byte(b.String()), 0o644)
	}
	t.Logf("%d of %d within 2%%", pass, len(results))
}

// compare is the fraction of pixels that differ visibly (on white).
func compare(got *image.NRGBA, want image.Image) float64 {
	b := want.Bounds()
	bad := 0
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, g, bl, a := want.At(b.Min.X+x, b.Min.Y+y).RGBA()
			// Reference over white.
			wr := (r + (0xffff - a)) >> 8
			wg := (g + (0xffff - a)) >> 8
			wb := (bl + (0xffff - a)) >> 8
			c := got.NRGBAAt(x, y)
			ga := int(c.A)
			c.R = uint8((int(c.R)*ga + 255*(255-ga)) / 255)
			c.G = uint8((int(c.G)*ga + 255*(255-ga)) / 255)
			c.B = uint8((int(c.B)*ga + 255*(255-ga)) / 255)
			d := absDiff(int(c.R), int(wr)) + absDiff(int(c.G), int(wg)) + absDiff(int(c.B), int(wb))
			if d > 96 {
				bad++
			}
		}
	}
	return float64(bad) / float64(b.Dx()*b.Dy())
}

func absDiff(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

func sideBySide(got *image.NRGBA, want image.Image) *image.NRGBA {
	b := want.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx()*2+4, b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			c := got.NRGBAAt(x, y)
			ga := int(c.A)
			out.Set(x, y, color.NRGBA{uint8((int(c.R)*ga + 255*(255-ga)) / 255), uint8((int(c.G)*ga + 255*(255-ga)) / 255), uint8((int(c.B)*ga + 255*(255-ga)) / 255), 255})
			r, g, bl, a := want.At(b.Min.X+x, b.Min.Y+y).RGBA()
			out.Set(b.Dx()+4+x, y, color.RGBA64{uint16(r + 0xffff - a), uint16(g + 0xffff - a), uint16(bl + 0xffff - a), 0xffff})
		}
	}
	return out
}
