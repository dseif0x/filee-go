package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dseif0x/filee-go/internal/history"
	"github.com/dseif0x/filee-go/internal/presets"
	"github.com/dseif0x/filee-go/internal/profiles"
)

func TestAFreshStoreHasTheDefaultsAndSavesThemAtomically(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, nil)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if s.FindConversion("to-png") == nil || s.FindConversion("rotate") != nil || len(s.Profiles) == 0 || s.Settings.HistoryLimit != 200 {
		t.Fatal("defaults")
	}
	s.Settings.Language = "ko"
	s.Settings.CloseAction = CloseTray
	s.Settings.Triggers[0].Modifiers = ModCtrl | ModShift
	s.Presets[0].Name = "My PNG"
	if err := s.SaveSettings(); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveLibrary(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatal("temporary file left", e.Name())
		}
	}
	again := NewStore(dir, nil)
	_ = again.Load()
	if again.Settings.Language != "ko" || again.Settings.CloseAction != CloseTray || again.Presets[0].Name != "My PNG" ||
		again.Settings.Triggers[0].Modifiers != ModCtrl|ModShift {
		t.Fatalf("%+v", again.Settings)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	if !json.Valid(raw) || !contains(string(raw), `"modifiers": "Ctrl, Shift"`) {
		t.Fatal(string(raw))
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestABrokenFileIsKeptAndDefaultsUsedAndUnknownValuesBecomeDefaults(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "presets.json"), []byte("{not json"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"closeAction":"Explode","theme":{"mode":"dark"},"historyLimit":5}`), 0o644)
	s := NewStore(dir, nil)
	_ = s.Load()
	if _, err := os.Stat(filepath.Join(dir, "presets.json.broken")); err != nil || s.FindPreset("to-png") == nil {
		t.Fatal("broken presets")
	}
	if s.Settings.CloseAction != CloseAsk || s.Settings.Theme.Mode != ThemeDark || s.Settings.EffectiveHistoryLimit() != MinHistoryLimit ||
		!s.Settings.StartWithSystem || len(s.Settings.Triggers) != 2 {
		t.Fatalf("%+v", s.Settings)
	}
}

func TestConsistencyAndProfileReset(t *testing.T) {
	s := NewStore(t.TempDir(), nil)
	_ = s.Load()
	img := s.FindProfile("images")
	img.PresetIDs = []string{"gone", "to-png", "to-png"}
	img.Extensions = []string{"png"}
	s.Profiles = append([]*profiles.Profile{{ID: "mine", Extensions: []string{"jpg"}, PresetIDs: []string{}}}, s.Profiles...)
	// Delete the built-in to-webp preset.
	for i, p := range s.Presets {
		if p.ID == "to-webp" {
			s.Presets = append(s.Presets[:i], s.Presets[i+1:]...)
			break
		}
	}
	_ = s.SaveLibrary()
	if len(img.PresetIDs) != 1 {
		t.Fatal(img.PresetIDs)
	}
	ok, err := s.ResetProfile(img)
	if !ok || err != nil || s.FindPreset("to-webp") == nil || len(img.PresetIDs) != 12 {
		t.Fatal(ok, err, img.PresetIDs)
	}
	for _, e := range img.Extensions {
		if e == "jpg" {
			t.Fatal("took the extension another donut claims")
		}
	}
	// No mixed profile: one comes back.
	var normal []*profiles.Profile
	for _, p := range s.Profiles {
		if !p.IsFallback {
			normal = append(normal, p)
		}
	}
	s.Profiles = normal
	_ = s.SaveLibrary()
	if profiles.CatchAll(s.Profiles).ID != "mixed" {
		t.Fatal("no mixed profile")
	}
}

func TestExportLeavesPasswordsOutAndImportReplacesById(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, nil)
	_ = s.Load()
	p := presets.New("Secret", "pdf")
	p.Pdf.Password = "hunter2"
	s.Presets = append(s.Presets, p)
	path := filepath.Join(dir, "bundle.json")
	if err := s.Export(path); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if indexOf(string(raw), "hunter2") >= 0 {
		t.Fatal("the password was exported")
	}
	other := NewStore(t.TempDir(), nil)
	_ = other.Load()
	n, err := other.Import(path)
	if err != nil || n != len(s.Presets)+len(s.Profiles) || other.FindPreset(p.ID).Name != "Secret" {
		t.Fatal(n, err)
	}
	if len(other.Presets) != len(s.Presets) {
		t.Fatal("duplicated presets", len(other.Presets), len(s.Presets))
	}
}

func TestHistoryKeepsItsLimitNewestFirst(t *testing.T) {
	s := NewStore(t.TempDir(), nil)
	_ = s.Load()
	s.Settings.HistoryLimit = 50
	for i := 0; i < 60; i++ {
		_ = s.AddHistory(&history.Entry{JobID: string(rune('a' + i%26))})
	}
	if len(s.History) != 50 {
		t.Fatal(len(s.History))
	}
	s.Settings.KeepHistory = false
	_ = s.AddHistory(&history.Entry{JobID: "x"})
	if s.History[0].JobID == "x" {
		t.Fatal("recorded with the history off")
	}
}

func TestWithDefaultOutputAndFlipKeys(t *testing.T) {
	a := Defaults()
	p := presets.New("", "geojson")
	if a.WithDefaultOutput(p).Output.Location != presets.LocationSameFolder {
		t.Fatal("default location")
	}
	a.DefaultOutput = DefaultOutput{Location: presets.LocationSubfolder, SubfolderName: "out"}
	a.KeepFileDates, a.ZipGeoResults = true, true
	c := a.WithDefaultOutput(p)
	if c == p || c.Output.Location != presets.LocationSubfolder || c.Output.SubfolderName != "out" || !c.Output.KeepDates || !c.Output.ZipResult {
		t.Fatal(c.Output)
	}
	fixed := presets.New("", "png")
	fixed.Output.Location = presets.LocationSameFolder
	b := Defaults()
	if b.WithDefaultOutput(fixed) != fixed {
		t.Fatal("copied without need")
	}
	g := DefaultTriggers()
	g[1].Key = "Tab"
	d := Defaults().Donut
	if d.EffectiveFlipKey(g) != "" || IsFlipKeyAllowed("Escape", nil) {
		t.Fatal("flip key")
	}
	d.FlipKey = "F2"
	if d.EffectiveFlipKey(g) != "F2" {
		t.Fatal("F2")
	}
}

func TestCustomTheme(t *testing.T) {
	dir := t.TempDir()
	if LoadCustomTheme(dir, nil) != nil {
		t.Fatal("no file is no theme")
	}
	_ = os.WriteFile(filepath.Join(dir, ThemeFileName), []byte(`{"light":{"PAGE":"#ffffff","nope":"#000000","text":"red"},"Dark":{"page":"#000000"}}`), 0o644)
	th := LoadCustomTheme(dir, nil)
	if th == nil || th.Light["page"] != "#ffffff" || len(th.Light) != 1 || len(th.Dark) != 0 {
		t.Fatalf("%+v", th)
	}
}
