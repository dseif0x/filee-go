package presets

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/dseif0x/filee-go/internal/formats"
)

type router map[string]int

func (r router) Steps(from, to string) int { return r[from+">"+to] }

func TestBuiltInPresetsAreSound(t *testing.T) {
	seen := map[string]bool{}
	all := append(BuiltIn(), PlacePresets()...)
	for _, p := range all {
		if seen[p.ID] {
			t.Errorf("duplicate id %s", p.ID)
		}
		seen[p.ID] = true
		if p.Name == "" && p.NameKey == "" {
			t.Errorf("%s has no name", p.ID)
		}
		switch p.TargetFormat {
		case SameAsSource, GroupTarget, formats.Folder:
		default:
			if f := formats.ByID(p.TargetFormat); f == nil || !f.Writable() {
				t.Errorf("%s targets %q", p.ID, p.TargetFormat)
			}
		}
	}
	for _, p := range BuiltIn() {
		if !p.IsGroup() {
			continue
		}
		if len(p.SubPresetIDs) == 0 || len(p.SubPresetIDs) > MaxSubPresets {
			t.Errorf("group %s holds %d", p.ID, len(p.SubPresetIDs))
		}
		for _, id := range p.SubPresetIDs {
			if !seen[id] {
				t.Errorf("group %s holds missing %s", p.ID, id)
			}
		}
	}
	if !FindBuiltIn("tool-clip").IsClipTool() || !FindBuiltIn("tool-hide").IsHideTool() || !FindBuiltIn("tool-crop").IsCropTool() ||
		!FindBuiltIn("merge-images").JoinsImages() || !FindBuiltIn("zip-all").PacksIntoOneArchive() || FindBuiltIn("rotate-left").Image.Quality != 95 {
		t.Fatal("built-in preset kinds")
	}
}

func TestToolsBecomeConversions(t *testing.T) {
	r := router{"mp4>mp4": 1, "rm>mp4": 1, "png>png": 1, "raw>png": 1, "raw>jpg": 1}
	if ClipTargetFor("mp4", r) != "mp4" || ClipTargetFor("rm", r) != "mp4" || !CanCutClip("mp4", r) || CanCutClip("png", r) {
		t.Fatal("clip targets")
	}
	if HideTargetFor("png", r) != SameAsSource || HideTargetFor("raw", r) != "png" || !CanHide("raw") || CanHide("pdf") {
		t.Fatal("hide targets")
	}
	tool := FindBuiltIn("tool-hide")
	tool.Output.OnlyIfSmaller = true
	conv := HideConversionOf(tool, SameAsSource, []HiddenArea{{X: 0.9, Y: 0.9, Width: 0.5, Height: -0.2}, {X: 2, Y: 2, Width: 1, Height: 1}})
	if conv.IsTool() || len(conv.Image.HiddenAreas) != 1 || conv.Output.OnlyIfSmaller || !tool.IsTool() || conv.Image.HideStrength != DefaultHideStrength {
		t.Fatal(conv.Image.HiddenAreas)
	}
	if a := conv.Image.HiddenAreas[0]; !near(a.X, 0.9) || !near(a.Width, 0.1) || !near(a.Y, 0.7) || !near(a.Height, 0.2) {
		t.Fatal(a)
	}
	if x, y, w, h, ok := PixelBounds(HiddenArea{X: 0.1, Y: 0.1, Width: 0.25, Height: 0.001}, 100, 100); !ok || x != 10 || y != 10 || w != 25 || h != 1 {
		t.Fatal(x, y, w, h)
	}
	if HideEffectSize(1, 100, 100) != MinEffectPixels || HideEffectSize(5, 2000, 1000) != 40 || HideEffectSize(10, 4000, 2000) != 130 {
		t.Fatal(HideEffectSize(5, 2000, 1000))
	}
	if CellOf(1, 2.5) != 0 || CellOf(2, 2.5) != 1 || CellStart(1, 2.5) != 2 || CellStart(2, 2.5) != 5 {
		t.Fatal("cells")
	}
	if got := ImageEditTargetsFrom([]string{"raw"}, r); len(got) != 2 || got[0] != "png" || got[1] != "jpg" {
		t.Fatal(got)
	}
	if ImageEditTargetFor("raw", SameAsSource, r) != "png" || ImageEditTargetFor("png", SameAsSource, r) != SameAsSource || ImageEditTargetFor("raw", "jpg", r) != "jpg" {
		t.Fatal("edit targets")
	}
	edit := FindBuiltIn("tool-image-edit")
	img := DefaultImageOptions()
	img.Resize, img.Width, img.Quality = ResizeWidth, 800, 70
	RememberImageEdit(edit, "webp", img)
	conv = ImageEditConversionOf(edit, "webp", img)
	conv.Image.IcoSizes[0] = 1
	if edit.TargetFormat != "webp" || edit.Image.Width != 800 || conv.Image.Quality != 70 || img.IcoSizes[0] != 256 {
		t.Fatal("image edit")
	}
}

func TestAskedLocation(t *testing.T) {
	p := New("", "pdf")
	if !MakesOneResult(p, 1) || MakesOneResult(p, 2) {
		t.Fatal("one result")
	}
	p.Pdf.MergeIntoSingle = true
	if !MakesOneResult(p, 3) {
		t.Fatal("a merged PDF is one result")
	}
	j := New("", "jpg")
	j.Output.FileNamePattern = "{name}_x"
	j.Output.Renames = []RenameStep{{Kind: RenameUppercase}}
	if SuggestedName(j, "JPG", "/a/photo.png") != "PHOTO_X.jpg" {
		t.Fatal(SuggestedName(j, "JPG", "/a/photo.png"))
	}
	s := New("", SameAsSource)
	if ResultExtension(s, "a.HEIC") != "HEIC" || ResultExtension(New("", formats.Folder), "a.zip") != "" {
		t.Fatal("extension")
	}
	s.Output.ZipResult = true
	if ResultExtension(s, "a.png") != "zip" {
		t.Fatal("zip")
	}
	dir := t.TempDir()
	chosen := filepath.Join(dir, "Report final.pdf")
	_ = os.WriteFile(chosen, []byte("old"), 0o644)
	j.Output.ReplaceOriginal = true
	f := ForChosenFile(j, chosen)
	got := Resolve(&f.Output, Tokens{SourcePath: "/elsewhere/a.docx"}, "pdf", nil, "")
	if got != chosen || f.Output.ReplaceOriginal || f.Output.AskEveryTime {
		t.Fatal(got)
	}
	// Another page of it doesn't replace what is there.
	if Resolve(&f.Output, Tokens{SourcePath: "/elsewhere/a.docx"}, "pdf", func(p string) bool { return p == chosen }, "-p2") == chosen {
		t.Fatal("a page replaced the chosen file")
	}
}

func TestSizeLimit(t *testing.T) {
	p := New("", "png")
	if SizeLimitFor(p, "a.png") != 0 {
		t.Fatal("no limit")
	}
	p.Image.MaxSizeMb = 10
	p.Media.MaxSizeMb = 18
	if SizeLimitFor(p, "a.png") != 10_000_000 || SizeLimitFor(p, "a.mp4") != 18_000_000 || SizeLimitFor(p, "a.pdf") != 0 {
		t.Fatal("limits")
	}
	if DescribeSize(12_345_678, 10_000_000) != "12.35 MB > 10 MB" {
		t.Fatal(DescribeSize(12_345_678, 10_000_000))
	}
	_ = math.Pi
}
