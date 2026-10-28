package profiles

import (
	"testing"

	"github.com/dseif0x/filee-go/internal/presets"
)

func TestBuiltInDonutsOnlyHoldBuiltInPresets(t *testing.T) {
	ids := map[string]bool{}
	for _, p := range presets.BuiltIn() {
		ids[p.ID] = true
	}
	mixed := 0
	for _, p := range BuiltIn() {
		if p.IsFallback {
			mixed++
		}
		if len(p.PresetIDs) > MaxSlices || len(p.BackPresetIDs) > MaxSlices {
			t.Errorf("%s holds too many", p.ID)
		}
		for _, id := range append(append([]string{}, p.PresetIDs...), p.BackPresetIDs...) {
			if !ids[id] {
				t.Errorf("%s holds missing preset %s", p.ID, id)
			}
		}
	}
	if mixed != 1 {
		t.Fatal(mixed)
	}
}

func TestSelectPicksTheDonutOfTheFiles(t *testing.T) {
	all := BuiltIn()
	if p := Select(all, []string{"/a/x.JPG", "/a/y.png"}); p.ID != "images" {
		t.Fatal(p.ID)
	}
	if p := Select(all, []string{"/a/x.jpg", "/a/y.pdf"}); p.ID != "mixed" {
		t.Fatal(p.ID)
	}
	limited := &Profile{ID: "photos-and-pdf", IsFallback: true, Extensions: []string{"jpg", "pdf"}}
	withLimited := append([]*Profile{limited}, all...)
	if p := Select(withLimited, []string{"/a/x.jpg", "/a/y.pdf"}); p != limited {
		t.Fatal(p.ID)
	}
	if p := Select(withLimited, []string{"/a/x.docx", "/a/y.pdf"}); p.ID != "mixed" {
		t.Fatal(p.ID)
	}
	if CatchAll(nil) != nil {
		t.Fatal("catch-all of nothing")
	}
}

func TestMovingAProfilePassesOnlyOneOfItsKind(t *testing.T) {
	a, b := &Profile{ID: "a"}, &Profile{ID: "b"}
	m := &Profile{ID: "m", IsFallback: true}
	c := &Profile{ID: "c"}
	list := []*Profile{a, b, m, c}
	out, from, to, ok := Move(list, b, 1, nil)
	if !ok || from != 1 || to != 3 || out[1] != m || out[2] != c || out[3] != b {
		t.Fatal(from, to)
	}
	out, _, _, ok = Move(list, c, -1, func(p *Profile) bool { return p != b })
	if !ok || out[0] != c || out[1] != a {
		t.Fatal(out)
	}
	if _, _, _, ok := Move(list, m, 1, nil); ok {
		t.Fatal("the mixed one passed a normal one")
	}
}
