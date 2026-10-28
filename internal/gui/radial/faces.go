package radial

import (
	"slices"
	"strings"

	"github.com/dseif0x/filee-go/internal/app"
	"github.com/dseif0x/filee-go/internal/donut"
	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/presets"
	"github.com/dseif0x/filee-go/internal/profiles"
)

// face is one side of the donut for the files on it.
type face struct {
	presets []*presets.Preset
	subs    [][]*presets.Preset
	items   []donut.Item
}

// usable: a slice converts these files (a face without one isn't flipped to).
func (f *face) usable() bool {
	return slices.ContainsFunc(f.items, func(i donut.Item) bool { return i.Enabled })
}

// buildFace makes the slices of the preset ids for files of the formats
// (RadialViewModel.BuildFace): a slice that can't convert them is greyed out
// and says why; a group shows its presets on the ring it fans out.
func buildFace(a *app.App, ids []string, fs []*formats.Format) *face {
	texts := a.Texts()
	planner := a.Planner()
	f := &face{}
	for _, id := range ids {
		p := a.Preset(id)
		if p == nil {
			continue
		}
		f.presets = append(f.presets, p)
		var subs []*presets.Preset
		if p.IsGroup() {
			for _, sid := range p.SubPresetIDs {
				if c := a.Preset(sid); c != nil && !c.IsGroup() && len(subs) < presets.MaxSubPresets {
					subs = append(subs, c)
				}
			}
		}
		f.subs = append(f.subs, subs)
	}
	itemOf := func(p *presets.Preset, caption bool) donut.Item {
		reason := unavailable(planner, texts, p, fs)
		it := donut.Item{ID: p.ID, Enabled: reason == "", DisabledReason: reason}
		if caption {
			it.Label = texts.DonutName(p)
			if p.TargetFormat == presets.SameAsSource {
				var names []string
				for _, x := range fs {
					if len(names) < 2 {
						names = append(names, x.Name)
					}
				}
				it.Caption = strings.Join(names, "/")
				if it.Caption == "" {
					it.Caption = texts.Text("donut.same_format")
				}
			} else if tf := formats.ByID(p.TargetFormat); tf != nil {
				it.Caption = tf.Name
			}
			if p.IsTool() {
				it.Caption = ""
			}
		} else {
			it.Label = texts.RingName(p)
		}
		return it
	}
	for i, p := range f.presets {
		if !p.IsGroup() {
			f.items = append(f.items, itemOf(p, true))
			continue
		}
		var children []donut.Item
		for _, c := range f.subs[i] {
			children = append(children, itemOf(c, false))
		}
		it := donut.Item{ID: p.ID, Label: texts.PresetName(p), Caption: texts.Text("donut.group_caption", len(children)), Group: true, Children: children}
		it.Enabled = slices.ContainsFunc(children, func(c donut.Item) bool { return c.Enabled })
		if len(children) == 0 {
			it.DisabledReason = texts.Text("donut.group_empty")
		} else {
			for _, c := range children {
				if c.DisabledReason != "" {
					it.DisabledReason = c.DisabledReason
					break
				}
			}
		}
		f.items = append(f.items, it)
	}
	return f
}

// hasGroups: a preset of ids is a group.
func hasGroups(a *app.App, ids []string) bool {
	for _, id := range ids {
		if p := a.Preset(id); p != nil && p.IsGroup() {
			return true
		}
	}
	return false
}

// anyProfileHasGroups: a drag is placed before its files are known.
func anyProfileHasGroups(a *app.App) bool {
	return slices.ContainsFunc(a.Profiles(), func(p *profiles.Profile) bool {
		return hasGroups(a, p.PresetIDs) || hasGroups(a, p.BackPresetIDs)
	})
}
