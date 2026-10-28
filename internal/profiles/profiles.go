// Package profiles holds the donut toolbar layouts: which presets the donut
// shows for a set of dropped files, the choice of layout, its order and the
// built-in layouts.
package profiles

import (
	"crypto/rand"
	"encoding/hex"
	"slices"
	"strings"

	"github.com/dseif0x/filee-go/internal/formats"
)

// MaxSlices is the most slices on one donut ring.
const MaxSlices = 12

// Profile is a donut toolbar layout for one family of source files (e.g.
// "Images" for jpg/png/...).
type Profile struct {
	ID string `json:"id"`
	// Name is the user's name; when empty NameKey is localized instead.
	Name    string `json:"name"`
	NameKey string `json:"nameKey,omitempty"`
	// Extensions (without dot): for a normal profile the files it is used for;
	// for a mixed-selection profile (IsFallback) the extensions a mixed
	// selection must stay within, empty meaning any.
	Extensions []string `json:"extensions"`
	// IsFallback marks a "mixed files" profile, used when the dropped files
	// span several profiles or match none.
	IsFallback bool `json:"isFallback,omitempty"`
	// PresetIDs in donut order, clockwise from 12 o'clock.
	PresetIDs []string `json:"presetIds"`
	// BackPresetIDs is the back face Tab flips to; empty: the donut doesn't
	// flip. nil only as read from a file without back faces.
	BackPresetIDs []string `json:"backPresetIds"`
}

// New returns an empty profile with a fresh id.
func New(name string) *Profile {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return &Profile{ID: hex.EncodeToString(b), Name: name, Extensions: []string{}, PresetIDs: []string{}, BackPresetIDs: []string{}}
}

// Matches reports whether the extension (with or without dot) belongs to the profile.
func (p *Profile) Matches(ext string) bool {
	ext = strings.TrimPrefix(ext, ".")
	return slices.ContainsFunc(p.Extensions, func(e string) bool { return strings.EqualFold(e, ext) })
}

// Clone returns a deep copy.
func (p *Profile) Clone() *Profile {
	c := *p
	c.Extensions = slices.Clone(p.Extensions)
	c.PresetIDs = slices.Clone(p.PresetIDs)
	if p.BackPresetIDs != nil {
		c.BackPresetIDs = slices.Clone(p.BackPresetIDs)
	}
	return &c
}

// Select picks the profile for the files: the first normal profile that has
// every file's extension; otherwise the first mixed-selection profile whose
// extensions include every file's; otherwise the catch-all.
func Select(profiles []*Profile, paths []string) *Profile {
	var exts []string
	for _, p := range paths {
		e := formats.ExtensionOf(p)
		if !slices.ContainsFunc(exts, func(x string) bool { return strings.EqualFold(x, e) }) {
			exts = append(exts, e)
		}
	}
	if len(exts) > 0 {
		all := func(p *Profile) bool {
			for _, e := range exts {
				if !p.Matches(e) {
					return false
				}
			}
			return true
		}
		for _, p := range profiles {
			if !p.IsFallback && all(p) {
				return p
			}
		}
		for _, p := range profiles {
			if p.IsFallback && len(p.Extensions) > 0 && all(p) {
				return p
			}
		}
	}
	return CatchAll(profiles)
}

// CatchAll is the profile for selections no other takes: the first mixed
// profile without extensions, else the first mixed one, else the first.
func CatchAll(profiles []*Profile) *Profile {
	for _, p := range profiles {
		if p.IsFallback && len(p.Extensions) == 0 {
			return p
		}
	}
	for _, p := range profiles {
		if p.IsFallback {
			return p
		}
	}
	if len(profiles) > 0 {
		return profiles[0]
	}
	return nil
}

// DetectFormats lists the distinct formats of the files Filee recognises.
func DetectFormats(paths []string) []*formats.Format {
	var out []*formats.Format
	for _, p := range paths {
		if f := formats.Detect(p); f != nil && !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	return out
}

// Neighbor is the profile p would pass moving up (direction -1) or down (+1):
// the nearest one of the same kind (normal or mixed) among those shown lets
// through (all when shown is nil). nil when there is none. Of two normal
// profiles with the same extension the higher one opens, so a profile only
// trades places with one of its own kind.
func Neighbor(profiles []*Profile, p *Profile, direction int, shown func(*Profile) bool) *Profile {
	i := slices.Index(profiles, p)
	if i < 0 || direction == 0 {
		return nil
	}
	step := 1
	if direction < 0 {
		step = -1
	}
	for j := i + step; j >= 0 && j < len(profiles); j += step {
		o := profiles[j]
		if o.IsFallback == p.IsFallback && (shown == nil || shown(o)) {
			return o
		}
	}
	return nil
}

// Move moves p just past its Neighbor and returns the old and new index, or
// ok false when there is nothing to pass. Profiles in between keep their order.
func Move(profiles []*Profile, p *Profile, direction int, shown func(*Profile) bool) (out []*Profile, from, to int, ok bool) {
	n := Neighbor(profiles, p, direction, shown)
	if n == nil {
		return profiles, -1, -1, false
	}
	from = slices.Index(profiles, p)
	to = slices.Index(profiles, n)
	out = slices.Delete(slices.Clone(profiles), from, from+1)
	out = slices.Insert(out, to, p)
	return out, from, to, true
}
