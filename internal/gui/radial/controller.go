// Package radial opens, fills and closes the live donut toolbar
// (RadialController and RadialViewModel of the original), independent of
// the windowing system: a Host shows the overlay, runs timers and opens
// windows.
//
// Drag mode (modifier + drag in Finder): the gesture places an invisible
// overlay at the cursor; a file drag entering it brings the file list and
// the donut pops out; a drop on a slice converts, in the hole or elsewhere
// closes. Without files nothing ever shows.
//
// Click mode (keyboard shortcut, Services menu): the files are known up
// front, the donut appears activated, a click converts, Esc or a click
// outside closes.
//
// After a drop or click the donut becomes a progress ring at the same spot:
// finished quickly → ✓, then it fades; still running after HandOffAfter,
// failed, Esc or a new gesture → a notification takes over.
package radial

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dseif0x/filee-go/internal/app"
	"github.com/dseif0x/filee-go/internal/donut"
	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/jobs"
	"github.com/dseif0x/filee-go/internal/platform"
	"github.com/dseif0x/filee-go/internal/presets"
	"github.com/dseif0x/filee-go/internal/profiles"
	"github.com/dseif0x/filee-go/internal/settings"
	"github.com/dseif0x/filee-go/internal/web"
)

// Host is what the controller needs of the GUI. Every method is called on
// the UI thread, and the functions it is handed (After, Post) run there too.
type Host interface {
	// ShowOverlay shows the overlay of side points centred on the screen
	// point (x, y) in top-left global points, kept inside that screen's
	// visible area; activate gives it the keyboard (click mode).
	ShowOverlay(x, y, side float64, activate bool)
	HideOverlay()
	// Redraw asks for frames: the host calls Menu.Advance and draws until
	// the menu needs no more.
	Redraw()
	// HitBackdrop: the overlay catches drags everywhere (alpha 1/255) while a
	// drag waits for files.
	SetHitBackdrop(on bool)
	After(d time.Duration, f func()) (cancel func())
	// WatchFlipKey asks the global hook to report the flip key and the wheel.
	WatchFlipKey(on bool)
	// Notify shows a finished job that the ring handed off (the toast).
	Notify(job jobs.Snapshot)
	// ShowTool opens a tool's window (crop, clip, hide, image-edit, pdf-pages, print).
	ShowTool(tool string, preset *presets.Preset, files []string)
	// OpenSettings opens the settings window on a page.
	OpenSettings(page string)
	// ChooseFolder asks where to save (an "Ask every time" preset); ok false when cancelled.
	ChooseFolder(title string) (folder string, ok bool)
	Reveal(path string)
	Log() *slog.Logger
}

// HandOffAfter is how long the ring shows a running job before the notification takes over.
const HandOffAfter = 5 * time.Second

// EmptyDragLimit is how long a drag that never brought files keeps the invisible overlay up.
const EmptyDragLimit = 10 * time.Second

// Controller is the live donut. UI thread only.
type Controller struct {
	app  *app.App
	host Host
	menu *donut.Menu

	visible, dropped, converted bool
	clickMode                   bool
	files                       []string
	front                       *face
	back                        *face
	backIDs                     []string
	fs                          []*formats.Format
	backFace                    bool
	loads                       int
	dragGeneration              int
	dragReachedDonut            bool
	progressJob                 string // job the ring shows ("" once finished or handed off)
	lastRingJob                 string
	cancelHandOff               func()
	web                         *webDrop
	graceUntil                  time.Time
	notifyOnFinish              map[string]bool
}

// webDrop is a browser drop the click-mode donut's files stand for.
type webDrop struct {
	drop    *web.Drop
	fetcher *web.Fetcher
}

// New makes the controller; its menu calls back into it.
func New(a *app.App, h Host) *Controller {
	c := &Controller{app: a, host: h, menu: donut.NewMenu(), notifyOnFinish: map[string]bool{}}
	m := c.menu
	m.OnAnimate = h.Redraw
	m.OnActiveIndexChanged = func(int) { c.showHint() }
	m.OnFaceSwapping = c.toggleFace
	m.OnSegmentInvoked = func(i int) { c.convert(i, -1, c.files, c.takeWeb()) }
	m.OnSubSegmentInvoked = func(g, s int) { c.convert(g, s, c.files, c.takeWeb()) }
	m.OnCenterInvoked = func() { c.Close(true) }
	m.OnContextRequested = func(int, int) { h.OpenSettings("toolbar") }
	m.OnProgressCancel = func() {
		if j := c.app.Job(c.progressJob); j != nil {
			j.Cancel()
		}
	}
	m.OnProgressClicked = func() {
		if j := c.app.Job(c.lastRingJob); j != nil {
			if outs := j.Outputs(); len(outs) > 0 {
				h.Reveal(outs[0])
			}
		}
	}
	return c
}

// Menu is the donut the host draws and feeds pointer events.
func (c *Controller) Menu() *donut.Menu { return c.menu }

// Visible reports whether the overlay is up.
func (c *Controller) Visible() bool { return c.visible }

// ClickMode: opened with known files (clicks, keys).
func (c *Controller) ClickMode() bool { return c.clickMode }

// HasFiles: files are on the donut.
func (c *Controller) HasFiles() bool { return len(c.files) > 0 }

// Files are the files on the donut.
func (c *Controller) Files() []string { return c.files }

func (c *Controller) applyAppearance() {
	s := c.app.Settings()
	m := c.menu
	m.OuterRadius = clamp(s.Donut.OuterRadius, 100, 240)
	m.HoleRatio = s.Donut.HoleRatio
	m.SliceOpacity = clamp(s.Donut.Opacity, settings.MinDonutOpacity, 1)
	m.CornerRadius = s.Theme.CornerRadius
	m.FlipKey = s.Donut.EffectiveFlipKey(s.Triggers)
	m.FlipWithWheel = s.Donut.FlipWithWheel
}

func clamp(v, lo, hi float64) float64 { return max(lo, min(hi, v)) }

// ShowForDrag: a drag gesture started at (x, y). The overlay waits there,
// invisible, until files are dragged into it.
func (c *Controller) ShowForDrag(x, y float64) {
	c.releaseProgress()
	c.graceUntil = time.Time{}
	c.replaceWeb(nil)
	c.menu.LeaveProgressMode()
	c.dropped, c.dragReachedDonut, c.converted = false, false, false
	c.clickMode = false
	c.files = nil
	c.applyAppearance()
	c.menu.ReserveSubRing = anyProfileHasGroups(c.app)
	c.loads++
	c.front, c.back, c.backIDs, c.fs = &face{}, &face{}, nil, nil
	c.showFace(false, true)
	c.menu.CenterTitle = c.app.Text("donut.center_drag")
	c.menu.CenterSubtitle = ""
	c.menu.Opacity = 0
	c.menu.AllowContextEdit = false
	c.host.SetHitBackdrop(true)
	c.place(x, y, false)
	c.watchFlipKey()
	c.dragGeneration++
	gen := c.dragGeneration
	c.host.After(EmptyDragLimit, func() {
		if gen == c.dragGeneration && c.visible && !c.HasFiles() && !c.clickMode {
			c.Close(false)
		}
	})
}

// ShowForFiles opens the donut for known files (click mode).
// forAnotherProcess: the files came from the Services menu or a later
// launch, so the donut may lose focus once while Filee becomes active.
func (c *Controller) ShowForFiles(x, y float64, files []string, forAnotherProcess bool) {
	files = platform.ResolveShortcuts(c.app.Platform(), files)
	if len(files) == 0 {
		return
	}
	c.releaseProgress()
	c.replaceWeb(nil)
	if forAnotherProcess {
		c.graceUntil = time.Now().Add(1500 * time.Millisecond)
	} else {
		c.graceUntil = time.Time{}
	}
	c.dropped, c.converted = false, false
	c.load(files, true, false)
	c.menu.ReserveSubRing = hasItemGroups(c.front) || hasGroups(c.app, c.backIDs)
	c.menu.AllowContextEdit = true
	c.place(x, y, true)
	c.watchFlipKey()
	c.menu.PlayOpenAnimation()
}

func hasItemGroups(f *face) bool {
	return slices.ContainsFunc(f.items, func(i donut.Item) bool { return i.Group })
}

// load fills the donut for the files, on the front or (backFace) the back.
func (c *Controller) load(files []string, clickMode, backFace bool) {
	c.clickMode = clickMode
	c.files = files
	c.menu.Opacity = 1
	c.host.SetHitBackdrop(false)
	c.applyAppearance()
	counted := formats.WithoutCompanions(files)
	prof := profiles.Select(c.app.Profiles(), counted)
	c.fs = profiles.DetectFormats(counted)
	var frontIDs []string
	c.backIDs = nil
	if prof != nil {
		frontIDs, c.backIDs = prof.PresetIDs, slices.Clone(prof.BackPresetIDs)
	}
	c.front = buildFace(c.app, frontIDs, c.fs)
	c.back = nil
	c.loads++
	if backFace {
		c.showFace(c.backFaceOf().usable(), true)
	} else {
		c.showFace(false, false)
		if len(c.backIDs) > 0 {
			load := c.loads
			c.host.After(0, func() {
				if load == c.loads && c.back == nil {
					c.showFace(c.backFace, true)
				}
			})
		}
	}
	if len(c.fs) == 1 {
		c.menu.CenterTitle = c.app.Text("donut.files_same", len(counted), c.fs[0].Name)
	} else {
		c.menu.CenterTitle = c.app.Text("donut.files_mixed", len(counted))
	}
	c.menu.CenterSubtitle = ""
	if clickMode {
		c.menu.CenterSubtitle = c.app.Text("donut.close_hint")
	}
}

func (c *Controller) backFaceOf() *face {
	if c.back == nil {
		c.back = buildFace(c.app, c.backIDs, c.fs)
	}
	return c.back
}

func (c *Controller) showFace(back, checkBack bool) {
	f := c.front
	if back {
		f = c.backFaceOf()
	}
	c.backFace = back
	c.menu.SetItems(f.items)
	var other *face
	switch {
	case back:
		other = c.front
	case checkBack:
		other = c.backFaceOf()
	default:
		other = c.back
	}
	m := c.menu
	m.CanFlip = other != nil && other.usable()
	m.CenterHint, m.CenterHintWheel = "", false
	if m.CanFlip && (m.FlipKey != "" || m.FlipWithWheel) {
		key := "front"
		if !back {
			key = "back"
		}
		m.CenterHint = strings.TrimSpace(c.app.Text("donut.flip_to_"+key, keyDisplay(m.FlipKey)))
		m.CenterHintWheel = m.FlipKey == ""
	}
}

// keyDisplay is a key as the hub names it (macOS writes ⇥ for Tab).
func keyDisplay(key string) string {
	switch key {
	case "Tab":
		return "⇥ Tab"
	}
	return key
}

func (c *Controller) toggleFace() {
	if c.back == nil {
		c.showFace(c.backFace, true)
	}
	if !c.menu.CanFlip {
		return
	}
	c.showFace(!c.backFace, true)
	c.menu.CenterSubtitle = ""
	if c.clickMode {
		c.menu.CenterSubtitle = c.app.Text("donut.close_hint")
	}
}

// presetAt is the preset slice index converts with (or slice sub of its
// group's ring); nil when none or it can't convert these files.
func (c *Controller) presetAt(index, sub int) *presets.Preset {
	f := c.front
	if c.backFace {
		f = c.backFaceOf()
	}
	if index < 0 || index >= len(f.items) {
		return nil
	}
	if sub < 0 {
		if it := f.items[index]; it.Enabled && !it.Group {
			return f.presets[index]
		}
		return nil
	}
	ch := f.items[index].Children
	if sub < len(ch) && ch[sub].Enabled {
		return f.subs[index][sub]
	}
	return nil
}

func (c *Controller) showHint() {
	m := c.menu
	items := m.Items()
	index, sub := m.ActiveIndex(), m.ActiveSubIndex()
	to := "donut.drop_to"
	if c.clickMode {
		to = "donut.click_to"
	}
	switch {
	case m.IsCenterHot():
		m.CenterSubtitle = c.app.Text("donut.cancel")
	case index >= 0 && index < len(items) && len(items[index].Children) > 0 && (sub >= 0 || items[index].Enabled):
		ch := items[index].Children
		if sub >= 0 && sub < len(ch) {
			if ch[sub].Enabled {
				m.CenterSubtitle = c.app.Text(to, items[index].Label+" · "+ch[sub].Label)
			} else {
				m.CenterSubtitle = ch[sub].DisabledReason
			}
		} else {
			m.CenterSubtitle = c.app.Text("donut.pick_in_group")
		}
	case index >= 0 && index < len(items):
		if items[index].Enabled {
			m.CenterSubtitle = c.app.Text(to, items[index].Label)
		} else {
			m.CenterSubtitle = items[index].DisabledReason
		}
	default:
		m.CenterSubtitle = ""
		if c.clickMode {
			m.CenterSubtitle = c.app.Text("donut.close_hint")
		}
	}
	c.host.Redraw()
}

func (c *Controller) place(x, y float64, activate bool) {
	c.host.ShowOverlay(x, y, c.menu.Side(), activate)
	c.visible = true
	c.host.Redraw()
}

// OnDragGestureEnded: the mouse button of the drag gesture went up somewhere.
func (c *Controller) OnDragGestureEnded() {
	if !c.visible || c.clickMode {
		return
	}
	if !c.dragReachedDonut {
		c.host.Log().Debug("drag gesture ended without a drag entering the donut")
	}
	if !c.HasFiles() && !c.menu.IsInProgressMode() {
		c.Close(false)
		return
	}
	// A drop on the donut arrives around the same time as the button release.
	c.host.After(250*time.Millisecond, func() {
		if c.visible && !c.dropped && !c.clickMode {
			c.Close(true)
		}
	})
}

// Close closes the donut (a ring of a running job hands it to a notification).
func (c *Controller) Close(animate bool) {
	if !c.visible {
		return
	}
	m := c.menu
	if m.IsInProgressMode() {
		if c.progressJob != "" {
			c.handOff()
			return
		}
		if m.IsFinishingProgress() {
			return
		}
		m.LeaveProgressMode()
	}
	c.visible = false
	c.watchFlipKey()
	c.graceUntil = time.Time{}
	c.replaceWeb(nil)
	m.CollapseGroup()
	if animate {
		m.PlayCloseAnimation(func() {
			if !c.visible {
				c.host.HideOverlay()
			}
		})
	} else {
		c.host.HideOverlay()
	}
}

// Deactivated: the click-mode overlay lost the keyboard (a click elsewhere).
func (c *Controller) Deactivated() {
	if !c.visible || !c.clickMode {
		return
	}
	if time.Now().Before(c.graceUntil) {
		c.graceUntil = time.Time{}
		return
	}
	c.Close(true)
}

// Escape: Esc reached the overlay (not taken by the donut).
func (c *Controller) Escape() { c.Close(true) }

// ---- drag and drop on the overlay ----

// DragEntered: a drag entered the overlay with these files (or a browser's
// content when it has no files). It returns whether it brought files.
func (c *Controller) DragEntered(files []string, content *web.DragContent) bool {
	c.dragReachedDonut = true
	if len(files) == 0 && content != nil {
		// Only the names count while dragging: the drop reads the browser's files again.
		if d, _ := c.app.BrowserDrop(content); d != nil {
			files = slices.Clone(d.Placeholders())
			d.Close()
		}
	}
	if len(files) == 0 {
		return false
	}
	if c.clickMode || sameFiles(files, c.files) || sameWebFiles(files, c.files) {
		return true
	}
	c.load(files, false, false)
	c.menu.PlayOpenAnimation()
	c.watchFlipKey()
	return true
}

// DragOver: the drag is at p (overlay points). It returns whether a drop there converts.
func (c *Controller) DragOver(p donut.Point) bool {
	h := c.menu.HitTest(p)
	c.menu.SetExternalHit(h, &p)
	return c.HasFiles() && c.menu.Converts(h)
}

// DragExited: the drag left the overlay, last seen at last.
func (c *Controller) DragExited(last *donut.Point) { c.menu.ExternalDragLeft(last) }

// Drop: files (or a browser's content) were dropped at p. It returns
// whether the drop was taken.
func (c *Controller) Drop(p donut.Point, files []string, content *web.DragContent) bool {
	m := c.menu
	h := m.HitTest(p)
	m.CollapseGroup()
	m.SetExternalHighlight(-1, false)
	c.dropped = true
	c.watchFlipKey()
	var wd *webDrop
	if len(files) == 0 && content != nil {
		if d, f := c.app.BrowserDrop(content); d != nil {
			wd = &webDrop{d, f}
			files = slices.Clone(d.Placeholders())
		}
	}
	if h.Kind == donut.Segment || h.Kind == donut.SubSegment {
		if len(files) == 0 {
			files = c.files
		}
		sub := -1
		if h.Kind == donut.SubSegment {
			sub = h.Sub
		}
		c.convert(h.Index, sub, files, wd)
		return m.Converts(h)
	}
	if wd != nil {
		wd.drop.Close()
	}
	c.Close(true)
	return false
}

// ---- flipping from the global hook (drag mode) ----

func (c *Controller) flipsFromHook() bool {
	return c.visible && !c.dropped && !c.clickMode && c.HasFiles()
}

func (c *Controller) watchFlipKey() {
	on := c.flipsFromHook()
	c.host.WatchFlipKey(on)
	c.menu.FlipKeyFromHook = on
}

// FlipKeyPressed: the hook saw the flip key.
func (c *Controller) FlipKeyPressed() {
	if c.flipsFromHook() {
		c.menu.Flip()
	}
}

// WheelTurned: the hook saw the wheel turn.
func (c *Controller) WheelTurned() {
	if c.flipsFromHook() && c.menu.FlipWithWheel {
		c.menu.FlipFromWheel(time.Now().UnixMilli())
	}
}

// ---- converting ----

// sourcesFor: folders only go into "Compress into one archive"; any other
// preset converts the files beside them.
func sourcesFor(p *presets.Preset, files []string) []string {
	if p.PacksIntoOneArchive() {
		return files
	}
	var out []string
	for _, f := range files {
		if fi, err := os.Stat(f); err == nil && fi.IsDir() {
			continue
		}
		out = append(out, f)
	}
	return out
}

// toolName is the web UI tool editor for a tool preset.
func toolName(p *presets.Preset) string {
	switch {
	case p.IsClipTool():
		return "clip"
	case p.IsHideTool():
		return "hide"
	case p.IsImageEditTool():
		return "image-edit"
	case p.ID == "tool-sheet-studio":
		return "print"
	case p.ID == "tool-pdf-pages", p.ID == "tool-pdf-add":
		return "pdf-pages"
	}
	return "crop"
}

func (c *Controller) convert(index, sub int, files []string, wd *webDrop) {
	orphaned := wd == nil && slices.ContainsFunc(files, web.IsPlaceholder)
	p := c.presetAt(index, sub)
	var sources []string
	if p != nil {
		sources = sourcesFor(p, files)
	}
	if p == nil || len(sources) == 0 || orphaned {
		if wd != nil {
			wd.drop.Close()
		}
		c.Close(true)
		return
	}
	name := c.app.Texts().PresetName(p)
	if p.IsTool() {
		c.Close(false)
		if wd != nil {
			// The browser's files are saved first (a job of the drop with nothing to convert isn't there): the tool waits.
			wd.drop.Close()
			c.host.Log().Info("a tool on files from a browser is not supported yet", "preset", p.ID)
			return
		}
		c.host.ShowTool(toolName(p), p, sources)
		return
	}
	var fetcher jobs.SourceFetcher
	if wd != nil {
		fetcher = wd.fetcher
	}
	if p.Output.AskEveryTime && wd == nil {
		c.Close(false)
		folder, ok := c.host.ChooseFolder(name)
		if !ok {
			return
		}
		asked := p.Clone()
		asked.Output.Location = presets.LocationCustomFolder
		asked.Output.CustomFolder = folder
		asked.Output.AskEveryTime = false
		j := c.app.ConvertWith(sources, asked, name, nil)
		c.notifyOnFinish[j.ID()] = true
		return
	}
	merges := (p.TargetFormat == "pdf" && p.Pdf.MergeIntoSingle || p.JoinsIntoOneMedia() || p.JoinsImages()) && len(sources) > 1
	if merges {
		// The original asks for the order first; the files go in the order they were dropped (by name for a selection).
		if c.clickMode {
			slices.SortFunc(sources, func(a, b string) int {
				return strings.Compare(strings.ToLower(filepath.Base(a)), strings.ToLower(filepath.Base(b)))
			})
		}
		c.Close(false)
		j := c.app.ConvertWith(sources, p, name, fetcher)
		c.notifyOnFinish[j.ID()] = true
		return
	}
	c.host.Log().Info("converting", "files", len(sources), "preset", p.ID, "browser", wd != nil)
	j := c.app.ConvertWith(sources, p, name, fetcher)
	c.converted = true
	c.showProgress(j)
}

// ---- progress ring ----

func (c *Controller) showProgress(j *jobs.Job) {
	c.dropped = true
	c.progressJob, c.lastRingJob = j.ID(), j.ID()
	c.menu.BeginProgress()
	c.menu.SetProgress(j.Progress())
	id := j.ID()
	c.cancelHandOff = c.host.After(HandOffAfter, func() {
		if c.progressJob == id {
			if jj := c.app.Job(id); jj != nil && !jj.State().Final() {
				c.handOff()
			}
		}
	})
	// A job may finish before its first event reaches the UI thread.
	if s := j.Snapshot(); s.State.Final() {
		c.JobChanged(s)
	}
}

// JobChanged: a job's snapshot changed (from the app's events, on the UI thread).
func (c *Controller) JobChanged(s jobs.Snapshot) {
	if s.State.Final() && c.notifyOnFinish[s.ID] {
		delete(c.notifyOnFinish, s.ID)
		c.host.Notify(s)
	}
	if s.ID != c.progressJob {
		return
	}
	m := c.menu
	m.SetProgress(s.Progress)
	if !s.State.Final() {
		return
	}
	outcome := donut.Failed
	switch s.State {
	case jobs.JobCompleted:
		outcome = donut.Succeeded
	case jobs.JobCancelled:
		outcome = donut.Cancelled
	}
	if outcome == donut.Failed {
		c.host.Notify(s) // the notification says what went wrong
	}
	c.detachProgress()
	m.CompleteProgress(outcome, c.hideWindow)
}

func (c *Controller) handOff() {
	if c.progressJob == "" {
		return
	}
	c.notifyOnFinish[c.progressJob] = true
	c.detachProgress()
	c.menu.HandOffProgress(c.hideWindow)
}

// releaseProgress: a new gesture needs the overlay now; the job goes to a notification.
func (c *Controller) releaseProgress() {
	if c.progressJob != "" {
		c.notifyOnFinish[c.progressJob] = true
		if j := c.app.Job(c.progressJob); j != nil {
			if s := j.Snapshot(); s.State.Final() {
				c.JobChanged(s)
			}
		}
	}
	c.detachProgress()
}

func (c *Controller) detachProgress() {
	if c.cancelHandOff != nil {
		c.cancelHandOff()
		c.cancelHandOff = nil
	}
	c.progressJob = ""
}

func (c *Controller) hideWindow() {
	c.visible = false
	c.watchFlipKey()
	c.menu.CollapseGroup()
	c.host.HideOverlay()
}

func (c *Controller) takeWeb() *webDrop {
	w := c.web
	c.web = nil
	return w
}

func (c *Controller) replaceWeb(w *webDrop) {
	if c.web != nil && c.web != w {
		c.web.drop.Close()
	}
	c.web = w
}

func sameFiles(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !platform.SamePath(a[i], b[i]) {
			return false
		}
	}
	return true
}

func sameWebFiles(a, b []string) bool {
	if len(a) != len(b) || len(a) == 0 {
		return false
	}
	for i := range a {
		if !web.IsPlaceholder(a[i]) || !web.IsPlaceholder(b[i]) || filepath.Base(a[i]) != filepath.Base(b[i]) {
			return false
		}
	}
	return true
}

// NotificationText is the title and body of a job's notification (the toast's lines).
func NotificationText(a *app.App, s jobs.Snapshot) (title, body string) {
	what := ""
	if len(s.Files) == 1 {
		what = filepath.Base(s.Files[0].SourcePath)
	} else {
		what = a.Text("home.files", len(s.Files))
	}
	title = a.Text("toast.converting", what, s.PresetName)
	failed := s.Count(jobs.FileFailed)
	switch s.State {
	case jobs.JobCompleted:
		if s.Count(jobs.FileSkipped) == len(s.Files) {
			body = a.Text("toast.skipped")
		} else {
			body = a.Text("toast.done")
		}
	case jobs.JobCompletedWithErrors:
		body = a.Text("toast.done_errors", failed, len(s.Files))
	case jobs.JobFailed:
		body = a.Text("toast.failed")
	case jobs.JobCancelled:
		body = a.Text("toast.cancelled")
	default:
		body = fmt.Sprintf("%.0f%%", s.Progress*100)
	}
	for _, f := range s.Files {
		if f.State == jobs.FileFailed {
			body += "\n" + filepath.Base(f.SourcePath) + " — " + a.Texts().Notice(f.ErrorKey, "", f.ErrorDetail)
			break
		}
	}
	return title, body
}
