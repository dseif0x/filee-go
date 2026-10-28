//go:build darwin

package gui

import (
	_ "embed"

	"github.com/ebitengine/purego/objc"

	"github.com/dseif0x/filee-go/internal/macos/ns"
	"github.com/dseif0x/filee-go/internal/settings"
)

// The menu bar icon (the original's tray icon): open Filee, pause the
// gestures, quit. Its tooltip says which gesture opens the donut.

//go:embed filee.png
var iconPNG []byte

type statusItem struct {
	m                 *macApp
	item              ns.ID
	open, pause, quit ns.ID
}

var menuTargetClass objc.Class

func newStatusItem(m *macApp) *statusItem {
	if menuTargetClass == 0 {
		var err error
		menuTargetClass, err = objc.RegisterClass("FileeMenuTarget", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
			{Cmd: ns.Sel("openFilee:"), Fn: func(objc.ID, objc.SEL, objc.ID) { current.openSettings("") }},
			{Cmd: ns.Sel("togglePause:"), Fn: func(objc.ID, objc.SEL, objc.ID) {
				s := current.app.Settings()
				s.Paused = !s.Paused
				if err := current.app.SaveSettings(s); err != nil {
					current.log.Warn("could not save settings", "error", err)
				}
			}},
			{Cmd: ns.Sel("quitFilee:"), Fn: func(objc.ID, objc.SEL, objc.ID) { current.quit() }},
		})
		if err != nil {
			panic(err)
		}
	}
	target := ns.Send(ns.ID(menuTargetClass), "new")
	s := &statusItem{m: m}
	bar := ns.Send(ns.Class("NSStatusBar"), "systemStatusBar")
	s.item = ns.Retain(ns.Send(bar, "statusItemWithLength:", float64(-1))) // NSVariableStatusItemLength
	button := ns.Send(s.item, "button")
	data := ns.Send(ns.Class("NSData"), "dataWithBytes:length:", &iconPNG[0], uint(len(iconPNG)))
	img := ns.Send(ns.Send(ns.Alloc("NSImage"), "initWithData:", data), "autorelease")
	ns.Send(img, "setSize:", ns.Size{Width: 18, Height: 18})
	ns.Send(button, "setImage:", img)
	menu := ns.Send(ns.New("NSMenu"), "autorelease")
	add := func(action string) ns.ID {
		it := ns.Send(ns.Alloc("NSMenuItem"), "initWithTitle:action:keyEquivalent:", ns.String(""), ns.Sel(action), ns.String(""))
		ns.Send(it, "setTarget:", target)
		ns.Send(menu, "addItem:", it)
		ns.Release(it)
		return it
	}
	s.open = add("openFilee:")
	s.pause = add("togglePause:")
	ns.Send(menu, "addItem:", ns.Send(ns.Class("NSMenuItem"), "separatorItem"))
	s.quit = add("quitFilee:")
	ns.Send(s.item, "setMenu:", menu)
	return s
}

// refresh writes the texts in the language in use and shows or hides the icon.
func (s *statusItem) refresh(st *settings.AppSettings) {
	a := s.m.app
	ns.Send(s.open, "setTitle:", ns.String(a.Text("tray.open")))
	if st.Paused {
		ns.Send(s.pause, "setTitle:", ns.String(a.Text("tray.resume")))
	} else {
		ns.Send(s.pause, "setTitle:", ns.String(a.Text("tray.pause")))
	}
	ns.Send(s.quit, "setTitle:", ns.String(a.Text("tray.quit")))
	tip := a.Text("triggers.paused")
	if !st.Paused {
		gesture := "—"
		for _, t := range st.Triggers {
			if t.Enabled && t.Kind == settings.Drag {
				gesture = modifierText(t.Modifiers)
				break
			}
		}
		tip = a.Text("tray.tooltip", gesture)
	}
	ns.Send(ns.Send(s.item, "button"), "setToolTip:", ns.String(tip))
	// Without a Dock icon the menu bar icon is the only way in: it stays then.
	ns.Send(s.item, "setVisible:", st.ShowMenuBarIcon || !st.ShowInDock || s.m.inMenuBarOnly)
}

// modifierText writes modifiers as Mac keyboards label them.
func modifierText(m settings.ModifierKeys) string {
	out := ""
	for _, k := range []struct {
		m    settings.ModifierKeys
		name string
	}{{settings.ModCtrl, "⌃ Control"}, {settings.ModAlt, "⌥ Option"}, {settings.ModShift, "⇧ Shift"}, {settings.ModMeta, "⌘ Command"}} {
		if m&k.m != 0 {
			if out != "" {
				out += " + "
			}
			out += k.name
		}
	}
	if out == "" {
		return "—"
	}
	return out
}
