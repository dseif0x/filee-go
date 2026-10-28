package settings

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// ModifierKeys are modifier keys (left and right count the same). Meta is the
// Windows key on Windows and Command on macOS. In JSON a comma list: "Ctrl, Alt".
type ModifierKeys int

const (
	ModNone  ModifierKeys = 0
	ModCtrl  ModifierKeys = 1
	ModAlt   ModifierKeys = 2
	ModShift ModifierKeys = 4
	ModMeta  ModifierKeys = 8
)

var modNames = []struct {
	m    ModifierKeys
	name string
}{{ModCtrl, "Ctrl"}, {ModAlt, "Alt"}, {ModShift, "Shift"}, {ModMeta, "Meta"}}

func (m ModifierKeys) String() string {
	if m == ModNone {
		return "None"
	}
	var parts []string
	for _, n := range modNames {
		if m&n.m != 0 {
			parts = append(parts, n.name)
		}
	}
	return strings.Join(parts, ", ")
}

func (m ModifierKeys) MarshalJSON() ([]byte, error) { return json.Marshal(m.String()) }

func (m *ModifierKeys) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		var n int
		if json.Unmarshal(b, &n) == nil {
			*m = ModifierKeys(n) & 15
		}
		return nil
	}
	*m = ModNone
	for _, part := range strings.Split(s, ",") {
		for _, n := range modNames {
			if strings.EqualFold(strings.TrimSpace(part), n.name) {
				*m |= n.m
			}
		}
	}
	return nil
}

// MouseButton is a mouse button a gesture can use.
type MouseButton string

const (
	ButtonNone   MouseButton = "None"
	ButtonLeft   MouseButton = "Left"
	ButtonRight  MouseButton = "Right"
	ButtonMiddle MouseButton = "Middle"
	ButtonX1     MouseButton = "X1"
	ButtonX2     MouseButton = "X2"
)

// TriggerKind is the action that completes a gesture.
type TriggerKind string

const (
	// Drag: hold the modifiers and drag past the threshold (file drags).
	Drag TriggerKind = "Drag"
	// Hold: hold the modifiers and keep the button pressed without moving.
	Hold TriggerKind = "Hold"
	// KeyChord: modifiers + Key, no mouse; uses the file manager selection.
	KeyChord TriggerKind = "KeyChord"
)

// TriggerScope is where a gesture is active.
type TriggerScope string

const (
	// ScopeFileManager: only over a file manager or the desktop.
	ScopeFileManager TriggerScope = "FileManager"
	// ScopeAnywhere: everywhere except excluded apps.
	ScopeAnywhere TriggerScope = "Anywhere"
)

// TriggerGesture is a user-configurable gesture that opens the donut toolbar.
type TriggerGesture struct {
	ID        string       `json:"id"`
	Enabled   bool         `json:"enabled"`
	Kind      TriggerKind  `json:"kind"`
	Modifiers ModifierKeys `json:"modifiers"`
	Button    MouseButton  `json:"button"`
	// Key is an extra non-modifier key ("Space"); required for KeyChord.
	Key string `json:"key,omitempty"`
	// DragThreshold in logical pixels; 0 is the system default.
	DragThreshold    int          `json:"dragThreshold"`
	HoldMilliseconds int          `json:"holdMilliseconds"`
	Scope            TriggerScope `json:"scope"`
}

// NewTrigger returns a drag gesture with a fresh id.
func NewTrigger() *TriggerGesture {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return &TriggerGesture{ID: hex.EncodeToString(b), Enabled: true, Kind: Drag, Modifiers: ModCtrl, Button: ButtonLeft, HoldMilliseconds: 450, Scope: ScopeFileManager}
}

// DefaultTriggers are the gestures for this OS. On macOS Ctrl+click means
// right-click, so the drag holds Option there.
func DefaultTriggers() []*TriggerGesture {
	drag := NewTrigger()
	if isMac {
		drag.Modifiers = ModAlt
	}
	chord := NewTrigger()
	chord.Kind, chord.Modifiers, chord.Button, chord.Key = KeyChord, ModCtrl|ModAlt, ButtonNone, "Space"
	return []*TriggerGesture{drag, chord}
}

func (t *TriggerGesture) normalize() {
	lenient(&t.Kind, Drag, Drag, Hold, KeyChord)
	lenient(&t.Button, ButtonLeft, ButtonNone, ButtonLeft, ButtonRight, ButtonMiddle, ButtonX1, ButtonX2)
	lenient(&t.Scope, ScopeFileManager, ScopeFileManager, ScopeAnywhere)
	if t.ID == "" {
		t.ID = NewTrigger().ID
	}
}

// UnmarshalJSON reads a gesture over the defaults.
func (t *TriggerGesture) UnmarshalJSON(b []byte) error {
	type plain TriggerGesture
	d := plain(*NewTrigger())
	d.ID = ""
	if err := json.Unmarshal(b, &d); err != nil {
		return err
	}
	*t = TriggerGesture(d)
	t.normalize()
	return nil
}

// Clone returns a copy.
func (t *TriggerGesture) Clone() *TriggerGesture { c := *t; return &c }
