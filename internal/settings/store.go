package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/dseif0x/filee-go/internal/history"
	"github.com/dseif0x/filee-go/internal/presets"
	"github.com/dseif0x/filee-go/internal/profiles"
)

// DirVariable moves all of Filee's per-user data (settings, logs, downloaded
// engines) to another folder: a development or test copy then never touches
// the Filee the user runs.
const DirVariable = "FILEE_DATA_DIR"

// DefaultDir is FILEE_DATA_DIR, else <user config dir>/filee
// (~/Library/Application Support/filee on macOS, %AppData%\filee on Windows,
// ~/.config/filee on Linux).
func DefaultDir() string {
	if d := strings.TrimSpace(os.Getenv(DirVariable)); d != "" {
		if abs, err := filepath.Abs(d); err == nil {
			return strings.TrimRight(abs, `/\`)
		}
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "filee")
}

// Bundle is the file format presets and profiles are exported and imported as.
type Bundle struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Presets       []*presets.Preset   `json:"presets"`
	Profiles      []*profiles.Profile `json:"profiles"`
}

// Store is the in-memory copy of all user data plus its persistence: the app
// keeps exactly one. Its fields are not safe for concurrent changes; the app
// facade (internal/app) serializes access.
type Store struct {
	dir string
	log *slog.Logger
	mu  sync.Mutex // serializes writes

	Settings *AppSettings
	Presets  []*presets.Preset
	Profiles []*profiles.Profile
	// History is newest first.
	History []*history.Entry
}

// NewStore returns a store of defaults for dir ("" for DefaultDir); Load reads the files.
func NewStore(dir string, log *slog.Logger) *Store {
	if dir == "" {
		dir = DefaultDir()
	}
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Store{dir: dir, log: log, Settings: Defaults(), Presets: presets.BuiltIn(), Profiles: profiles.BuiltIn(), History: []*history.Entry{}}
}

// Dir is the data folder.
func (s *Store) Dir() string { return s.dir }

func (s *Store) path(name string) string { return filepath.Join(s.dir, name) }

const (
	settingsFile = "settings.json"
	presetsFile  = "presets.json"
	profilesFile = "profiles.json"
	historyFile  = "history.json"
)

// Load reads everything from disk, falling back to defaults for missing or
// broken files (a broken one is kept as <name>.broken).
func (s *Store) Load() error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	settings := Defaults()
	if !s.read(settingsFile, settings) {
		settings = Defaults()
	}
	var ps []*presets.Preset
	if !s.read(presetsFile, &ps) || ps == nil {
		ps = presets.BuiltIn()
	}
	var pr []*profiles.Profile
	if !s.read(profilesFile, &pr) || pr == nil {
		pr = profiles.BuiltIn()
	}
	var h []*history.Entry
	if !s.read(historyFile, &h) || h == nil {
		h = []*history.Entry{}
	}
	s.Settings, s.Presets, s.Profiles, s.History = settings, slices.DeleteFunc(ps, isNil), slices.DeleteFunc(pr, isNil), slices.DeleteFunc(h, isNil)
	addBackFaces(s.Presets, s.Profiles)
	s.ensureConsistency()
	return nil
}

func isNil[T any](p *T) bool { return p == nil }

// SaveSettings writes settings.json.
func (s *Store) SaveSettings() error { return s.write(settingsFile, s.Settings) }

// SaveLibrary writes presets.json and profiles.json.
func (s *Store) SaveLibrary() error {
	s.ensureConsistency()
	if err := s.write(presetsFile, s.Presets); err != nil {
		return err
	}
	return s.write(profilesFile, s.Profiles)
}

// AddHistory records a finished conversion, unless the history is off.
func (s *Store) AddHistory(e *history.Entry) error {
	if !s.Settings.KeepHistory {
		return nil
	}
	// A lowered limit drops the oldest entries here, with the next conversion.
	limit := s.Settings.EffectiveHistoryLimit()
	s.History = append([]*history.Entry{e}, s.History...)
	if len(s.History) > limit {
		s.History = s.History[:limit]
	}
	return s.SaveHistory()
}

// SaveHistory writes history.json (after an entry changed: Undo).
func (s *Store) SaveHistory() error { return s.write(historyFile, s.History) }

// ClearHistory empties the history.
func (s *Store) ClearHistory() error {
	s.History = []*history.Entry{}
	return s.SaveHistory()
}

// ResetLibrary replaces presets and profiles with the factory defaults.
func (s *Store) ResetLibrary() error {
	s.Presets, s.Profiles = presets.BuiltIn(), profiles.BuiltIn()
	return s.SaveLibrary()
}

// CanResetProfile: only a built-in donut has a default state, and not when
// the reset would leave no profile for mixed selections.
func (s *Store) CanResetProfile(p *profiles.Profile) bool {
	b := profiles.FindBuiltIn(p.ID)
	if b == nil || !slices.Contains(s.Profiles, p) {
		return false
	}
	mixed := 0
	for _, x := range s.Profiles {
		if x.IsFallback {
			mixed++
		}
	}
	return b.IsFallback || !p.IsFallback || mixed > 1
}

// ResetProfile puts one built-in donut back as a new library has it and
// leaves the others alone. An extension another normal donut takes now stays
// there; built-in presets of its default slices the user deleted come back.
func (s *Store) ResetProfile(p *profiles.Profile) (bool, error) {
	if !s.CanResetProfile(p) {
		return false, nil
	}
	b := profiles.FindBuiltIn(p.ID)
	s.restoreBuiltInPresets(append(slices.Clone(b.PresetIDs), b.BackPresetIDs...))
	claimed := map[string]bool{}
	for _, x := range s.Profiles {
		if x != p && !x.IsFallback {
			for _, e := range x.Extensions {
				claimed[strings.ToLower(e)] = true
			}
		}
	}
	p.Name, p.NameKey = "", b.NameKey
	if b.IsFallback {
		p.Extensions = b.Extensions
	} else {
		p.Extensions = slices.DeleteFunc(slices.Clone(b.Extensions), func(e string) bool { return claimed[strings.ToLower(e)] })
	}
	p.PresetIDs, p.BackPresetIDs = b.PresetIDs, b.BackPresetIDs
	if p.IsFallback != b.IsFallback {
		p.IsFallback = b.IsFallback
		i := slices.Index(s.Profiles, p)
		s.Profiles = slices.Delete(s.Profiles, i, i+1)
		at := slices.IndexFunc(s.Profiles, func(x *profiles.Profile) bool { return x.IsFallback })
		if at < 0 {
			at = len(s.Profiles)
		}
		s.Profiles = slices.Insert(s.Profiles, at, p)
	}
	return true, s.SaveLibrary()
}

// restoreBuiltInPresets adds back the built-in presets among ids (and the
// members of a built-in group among them) the library no longer has, each
// after the built-in preset before it.
func (s *Store) restoreBuiltInPresets(ids []string) {
	defaults := presets.BuiltIn()
	all := slices.Clone(ids)
	for _, id := range ids {
		for _, d := range defaults {
			if d.ID == id && d.IsGroup() {
				all = append(all, d.SubPresetIDs...)
			}
		}
	}
	for _, id := range all {
		if s.FindPreset(id) != nil {
			continue
		}
		at := slices.IndexFunc(defaults, func(d *presets.Preset) bool { return d.ID == id })
		if at < 0 {
			continue
		}
		after := -1
		for _, d := range defaults[:at] {
			if i := slices.IndexFunc(s.Presets, func(p *presets.Preset) bool { return p.ID == d.ID }); i >= 0 {
				after = i
			}
		}
		s.Presets = slices.Insert(s.Presets, after+1, defaults[at])
	}
}

// FindPreset returns the preset id, or nil.
func (s *Store) FindPreset(id string) *presets.Preset {
	for _, p := range s.Presets {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// FindConversion returns the preset id when it converts (a group or tool doesn't).
func (s *Store) FindConversion(id string) *presets.Preset {
	if p := s.FindPreset(id); p != nil && !p.IsGroup() && !p.IsTool() {
		return p
	}
	return nil
}

// FindProfile returns the profile id, or nil.
func (s *Store) FindProfile(id string) *profiles.Profile {
	for _, p := range s.Profiles {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// Export writes presets and profiles to a shareable file, without PDF passwords.
func (s *Store) Export(path string) error {
	b := Bundle{SchemaVersion: 1, Profiles: s.Profiles}
	for _, p := range s.Presets {
		b.Presets = append(b.Presets, p.WithoutPassword(false))
	}
	return writeJSON(path, b)
}

// Import reads a bundle: items with an existing id are replaced, new ones
// added. It returns how many presets and profiles it took.
func (s *Store) Import(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var b Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		return 0, fmt.Errorf("not a preset bundle: %w", err)
	}
	if b.Presets == nil && b.Profiles == nil {
		return 0, errors.New("empty preset bundle")
	}
	b.Presets, b.Profiles = slices.DeleteFunc(b.Presets, isNil), slices.DeleteFunc(b.Profiles, isNil)
	for _, p := range b.Presets {
		s.Presets = slices.DeleteFunc(s.Presets, func(x *presets.Preset) bool { return x.ID == p.ID })
		s.Presets = append(s.Presets, p)
	}
	addBackFaces(s.Presets, b.Profiles)
	// Mixed profiles stay mixed; the catch-all is the first one with nothing
	// checked, so an imported one added at the end never replaces ours.
	for _, p := range b.Profiles {
		s.Profiles = slices.DeleteFunc(s.Profiles, func(x *profiles.Profile) bool { return x.ID == p.ID })
		s.Profiles = append(s.Profiles, p)
	}
	return len(b.Presets) + len(b.Profiles), s.SaveLibrary()
}

// addBackFaces gives each built-in donut without a back face its default
// one, with only the presets the library has.
func addBackFaces(ps []*presets.Preset, prs []*profiles.Profile) {
	ids := map[string]bool{}
	for _, p := range ps {
		ids[p.ID] = true
	}
	for _, p := range prs {
		if p.BackPresetIDs != nil {
			continue
		}
		if b := profiles.FindBuiltIn(p.ID); b != nil {
			p.BackPresetIDs = slices.DeleteFunc(b.BackPresetIDs, func(id string) bool { return !ids[id] })
		}
	}
}

// ensureConsistency removes dangling preset references, keeps groups
// sensible and guarantees at least one mixed-selection profile.
func (s *Store) ensureConsistency() {
	ids := map[string]bool{}
	for _, p := range s.Presets {
		ids[p.ID] = true
	}
	keep := func(list []string) []string {
		out := []string{}
		for _, id := range list {
			if ids[id] && !slices.Contains(out, id) && len(out) < profiles.MaxSlices {
				out = append(out, id)
			}
		}
		return out
	}
	for _, p := range s.Profiles {
		p.PresetIDs = keep(p.PresetIDs)
		p.BackPresetIDs = keep(p.BackPresetIDs)
		if p.Extensions == nil {
			p.Extensions = []string{}
		}
	}
	// A conversion to "group" is a group (an older Filee dropped its kind).
	for _, p := range s.Presets {
		if !p.IsGroup() && p.TargetFormat == presets.GroupTarget {
			p.Kind = presets.KindGroup
		}
	}
	// A group fans out conversions and tools (never another group), each once.
	actions := map[string]bool{}
	for _, p := range s.Presets {
		if !p.IsGroup() {
			actions[p.ID] = true
		}
	}
	for _, g := range s.Presets {
		if !g.IsGroup() {
			continue
		}
		var subs []string
		for _, id := range g.SubPresetIDs {
			if actions[id] && !slices.Contains(subs, id) && len(subs) < presets.MaxSubPresets {
				subs = append(subs, id)
			}
		}
		g.SubPresetIDs = subs
		g.TargetFormat = presets.GroupTarget
	}
	if !slices.ContainsFunc(s.Profiles, func(p *profiles.Profile) bool { return p.IsFallback }) {
		for _, b := range profiles.BuiltIn() {
			if b.IsFallback {
				b.PresetIDs, b.BackPresetIDs = keep(b.PresetIDs), keep(b.BackPresetIDs)
				s.Profiles = append(s.Profiles, b)
				break
			}
		}
	}
}

// read decodes name into v; false when missing or broken.
func (s *Store) read(name string, v any) bool {
	path := s.path(name)
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if err := json.Unmarshal(data, v); err != nil {
		// Keep the broken file for inspection and continue with defaults.
		s.log.Warn("could not read a settings file; using defaults", "path", path, "error", err)
		_ = os.WriteFile(path+".broken", data, 0o644)
		return false
	}
	return true
}

func (s *Store) write(name string, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(s.path(name), v)
}

// writeJSON writes v atomically: a temporary file, then a rename over the old one.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	_, err = tmp.Write(append(data, '\n'))
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}
