package i18n

import (
	"testing"

	"github.com/dseif0x/filee-go/internal/presets"
)

func TestTexts(t *testing.T) {
	l := New("en")
	if l.Text("error.no_route") != "No engine can do this conversion" || l.Text("no.such.key") != "no.such.key" {
		t.Fatal(l.Text("error.no_route"))
	}
	if got := l.Text("tray.update", "2.0"); got != "Update to 2.0" {
		t.Fatal(got)
	}
	if New("xx_YY.UTF-8").Language() != "en" || Languages()[0] != "en" {
		t.Fatal("fallback language")
	}
	p := presets.FindBuiltIn("half-size")
	if l.PresetName(p) == "preset.half_size" || l.PresetName(presets.New("", "webp")) != "WEBP" {
		t.Fatal(l.PresetName(p))
	}
	if got := l.Notice("notice.original_kept", "trash.in_use", "locked"); got != "The original was kept, the result was saved as a copy (the file is open in another program: locked)" {
		t.Fatal(got)
	}
	if Text("error.empty_file") == "error.empty_file" {
		t.Fatal("default localizer")
	}
}
