package presets

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestJSONDefaultsAndLenientEnums(t *testing.T) {
	var p Preset
	if err := json.Unmarshal([]byte(`{"name":"x","targetFormat":"jpg","image":{"resize":"longedge","rotation":"bogus"},"kind":"weird"}`), &p); err != nil {
		t.Fatal(err)
	}
	if p.ID == "" || p.Image.Quality != 90 || p.Image.Resize != ResizeLongEdge || p.Image.Rotation != RotateNone || p.Kind != KindGroup {
		t.Fatalf("%+v", p)
	}
	if p.Output.FileNamePattern != "{name}" || p.Pdf.RenderDpi != 150 {
		t.Fatalf("defaults lost: %+v", p.Output)
	}
}

func TestResolve(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "Minutes {date}.docx")
	now := time.Date(2026, 10, 28, 9, 30, 0, 0, time.UTC)
	rule := DefaultOutputRule()
	rule.FileNamePattern = "{name}-{date}"
	taken := map[string]bool{filepath.Join(dir, "Minutes {date}-20261028.pdf"): true}
	got := Resolve(&rule, Tokens{SourcePath: src, Now: now}, "pdf", func(p string) bool { return taken[p] }, "")
	if want := filepath.Join(dir, "Minutes {date}-20261028 (2).pdf"); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	rule.Conflict = ConflictSkip
	if got := Resolve(&rule, Tokens{SourcePath: src, Now: now}, "pdf", func(p string) bool { return taken[p] }, ""); got != "" {
		t.Fatalf("skip gave %q", got)
	}
	// Never the source.
	rule = DefaultOutputRule()
	rule.Conflict = ConflictOverwrite
	src = filepath.Join(dir, "a.png")
	if got := Resolve(&rule, Tokens{SourcePath: src}, "PNG", func(p string) bool { return p == filepath.Join(dir, "a.PNG") }, ""); got == src || filepath.Base(got) != "a (2).PNG" {
		t.Fatalf("got %q", got)
	}
	rule.Location = LocationSubfolder
	rule.Renames = []RenameStep{{Kind: RenameUppercase}, {Kind: RenamePrefix, Replace: "x_"}}
	if got := Resolve(&rule, Tokens{SourcePath: src}, "jpg", func(string) bool { return false }, "-1"); got != filepath.Join(dir, "converted", "x_A-1.jpg") {
		t.Fatalf("got %q", got)
	}
}

func TestRenames(t *testing.T) {
	for _, tc := range []struct {
		step RenameStep
		in   string
		want string
	}{
		{RenameStep{Kind: RenameReplace, Find: "IMG", Replace: "photo"}, "img_1 IMG", "photo_1 photo"},
		{RenameStep{Kind: RenameRemoveCopyNumber}, "report (3)", "report"},
		{RenameStep{Kind: RenameRemoveBrackets}, "song [live] (2020)", "song"},
		{RenameStep{Kind: RenameRemoveLeadingNumber}, "01 - intro", "intro"},
		{RenameStep{Kind: RenameRemoveSymbols}, "a#b!c.d", "abc.d"},
		{RenameStep{Kind: RenameRegex, Find: `(\d+)`, Replace: "<$1>"}, "a12", "a<12>"},
		{RenameStep{Kind: RenameRegex, Find: `(`}, "keep", "keep"},
	} {
		if got := tc.step.Apply(tc.in); got != tc.want {
			t.Errorf("%v(%q) = %q want %q", tc.step.Kind, tc.in, got, tc.want)
		}
	}
}
