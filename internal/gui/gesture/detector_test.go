package gesture

import (
	"testing"
	"time"

	"github.com/dseif0x/filee-go/internal/settings"
)

func gestures() []*settings.TriggerGesture {
	drag := settings.NewTrigger()
	drag.Modifiers = settings.ModAlt
	hold := settings.NewTrigger()
	hold.Kind, hold.Modifiers, hold.HoldMilliseconds = settings.Hold, settings.ModAlt, 400
	chord := settings.NewTrigger()
	chord.Kind, chord.Modifiers, chord.Button, chord.Key = settings.KeyChord, settings.ModCtrl|settings.ModAlt, settings.ButtonNone, "Space"
	return []*settings.TriggerGesture{drag, hold, chord}
}

func TestDragPastThreshold(t *testing.T) {
	d := New()
	var timer func()
	d.AfterFunc = func(_ time.Duration, f func()) { timer = f }
	d.Configure(gestures())
	started, ended, held := 0, 0, 0
	d.DragStarted = func(*settings.TriggerGesture, int, int) { started++ }
	d.DragEnded = func(int, int) { ended++ }
	d.HoldTriggered = func(*settings.TriggerGesture, int, int) { held++ }
	d.MouseDown(settings.ButtonLeft, 100, 100, settings.ModAlt)
	d.MouseMove(103, 102, settings.ModAlt, settings.ButtonLeft)
	if started != 0 {
		t.Fatal("started within the threshold")
	}
	d.MouseMove(110, 100, settings.ModAlt, settings.ButtonLeft)
	if started != 1 {
		t.Fatal("drag didn't start")
	}
	timer() // the hold of this press was cancelled by moving
	if held != 0 {
		t.Error("hold fired after a drag")
	}
	d.MouseUp(settings.ButtonLeft, 110, 100)
	if ended != 1 {
		t.Error("no drag end")
	}
	// Holding still holds.
	d.MouseDown(settings.ButtonLeft, 50, 50, settings.ModAlt)
	timer()
	if held != 1 {
		t.Error("hold didn't fire")
	}
	d.MouseMove(80, 50, settings.ModAlt, settings.ButtonLeft)
	if started != 1 {
		t.Error("moving after a hold started the drag")
	}
}

func TestLateModifierAndScope(t *testing.T) {
	d := New()
	d.Configure(gestures())
	inFinder := true
	d.ScopeFilter = func(*settings.TriggerGesture, int, int) bool { return inFinder }
	started := 0
	d.DragStarted = func(*settings.TriggerGesture, int, int) { started++ }
	d.MouseDown(settings.ButtonLeft, 0, 0, settings.ModNone)
	d.MouseMove(30, 0, settings.ModNone, settings.ButtonLeft)
	d.MouseMove(40, 0, settings.ModAlt, settings.ButtonLeft)
	if started != 1 {
		t.Fatal("pressing Option during a drag didn't start it")
	}
	d.MouseUp(settings.ButtonLeft, 40, 0)
	inFinder = false
	d.MouseDown(settings.ButtonLeft, 0, 0, settings.ModAlt)
	d.MouseMove(30, 0, settings.ModAlt, settings.ButtonLeft)
	if started != 1 {
		t.Error("started out of scope")
	}
}

func TestKeyChordAndPause(t *testing.T) {
	d := New()
	d.Configure(gestures())
	fired := 0
	d.KeyChordTriggered = func(_ *settings.TriggerGesture, x, y int) {
		if x != 7 || y != 9 {
			t.Errorf("chord at %d,%d", x, y)
		}
		fired++
	}
	d.MouseMove(7, 9, settings.ModNone, settings.ButtonNone)
	d.KeyDown("space", settings.ModCtrl|settings.ModAlt)
	d.KeyDown("Space", settings.ModCtrl)
	if fired != 1 {
		t.Fatalf("fired %d", fired)
	}
	d.SetPaused(true)
	d.KeyDown("Space", settings.ModCtrl|settings.ModAlt)
	if fired != 1 {
		t.Error("fired while paused")
	}
	if MacKeyName(0x31) != "Space" || MacKeyName(0x30) != "Tab" {
		t.Error("key names")
	}
}
