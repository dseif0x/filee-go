//go:build darwin

// Package macplatform is platform.Services on macOS, through purego (no cgo):
// the window under the pointer (Quartz window list), the Trash
// (NSFileManager), Finder aliases (bookmark data), Finder's selection
// (osascript), launch at login (SMAppService, or a launch agent outside a
// bundle), Finder's "Convert with Filee" service switch, the Dock icon and
// the input permissions.
package macplatform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/text/unicode/norm"

	"github.com/dseif0x/filee-go/internal/macos/bundle"
	"github.com/dseif0x/filee-go/internal/macos/ns"
	"github.com/dseif0x/filee-go/internal/platform"
)

// Services is the macOS implementation. Its methods may be called from any goroutine.
type Services struct {
	platform.Null
	mu sync.Mutex
	// LastSelectionError says why the last FileManagerSelection failed.
	LastSelectionError string
}

var _ platform.Services = (*Services)(nil)

// New loads the frameworks and returns the services.
func New() (*Services, error) {
	if err := ns.Load(); err != nil {
		return nil, err
	}
	return &Services{}, nil
}

// HasAccessibility: Filee may watch input (Accessibility, or Input Monitoring
// for the listen-only event tap).
func (s *Services) HasAccessibility() bool {
	return ns.AXIsProcessTrusted() || ns.CGPreflightListenEventAccess()
}

// RequestAccessibility shows macOS's own Accessibility alert (nothing when
// Filee has it) and asks for Input Monitoring; it returns whether Filee may
// watch input now.
func (s *Services) RequestAccessibility() bool {
	key := ns.CFString("AXTrustedCheckOptionPrompt")
	defer ns.CFRelease(key)
	value := uintptr(ns.Constant("kCFBooleanTrue"))
	keys, values := []uintptr{key}, []uintptr{value}
	kcb := symbolAddress("kCFTypeDictionaryKeyCallBacks")
	vcb := symbolAddress("kCFTypeDictionaryValueCallBacks")
	opts := ns.CFDictionaryCreate(0, &keys[0], &values[0], 1, kcb, vcb)
	defer ns.CFRelease(opts)
	trusted := ns.AXIsProcessTrustedWithOptions(opts)
	if !ns.CGPreflightListenEventAccess() {
		ns.CGRequestListenEventAccess()
	}
	return trusted || ns.CGPreflightListenEventAccess()
}

// OpenAccessibilitySettings opens System Settings at Privacy › Accessibility.
func (s *Services) OpenAccessibilitySettings() {
	_ = run("/usr/bin/open", "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility")
}

func (s *Services) SupportsGlobalPointerGestures() bool { return s.HasAccessibility() }
func (s *Services) SupportsSelectionShortcut() bool     { return true }
func (s *Services) PressCarriesPosition() bool          { return true }
func (s *Services) SupportsDockVisibility() bool        { return true }

// SetShowInDock changes the activation policy (regular or accessory); a change
// deactivates the app, so it is activated again.
func (s *Services) SetShowInDock(show bool) bool {
	ok := false
	ns.Sync(func() {
		app := ns.Send(ns.Class("NSApplication"), "sharedApplication")
		policy := 1 // NSApplicationActivationPolicyAccessory
		if show {
			policy = 0 // Regular
		}
		ok = ns.Bool(app, "setActivationPolicy:", policy)
		if ok {
			ns.Send(app, "activateIgnoringOtherApps:", true)
		}
	})
	return ok
}

// window is one on-screen window of the Quartz window list.
type window struct {
	pid    int32
	layer  int32
	alpha  float64
	bounds ns.Rect
	owner  string
}

var (
	keysOnce                                      sync.Once
	boundsKey, pidKey, alphaKey, layerKey, ownKey uintptr
)

// windows lists the on-screen windows front to back (kCGWindowListOptionOnScreenOnly).
func windows() []window {
	keysOnce.Do(func() {
		boundsKey, pidKey = ns.CFString("kCGWindowBounds"), ns.CFString("kCGWindowOwnerPID")
		alphaKey, layerKey, ownKey = ns.CFString("kCGWindowAlpha"), ns.CFString("kCGWindowLayer"), ns.CFString("kCGWindowOwnerName")
	})
	list := ns.CGWindowListCopyWindowInfo(1, 0)
	if list == 0 {
		return nil
	}
	defer ns.CFRelease(list)
	var out []window
	for i := range ns.CFArrayGetCount(list) {
		d := ns.CFArrayGetValueAtIndex(list, i)
		w := window{alpha: 1, layer: 0}
		if v, ok := ns.CFInt(ns.CFDictionaryGetValue(d, pidKey)); ok {
			w.pid = v
		} else {
			continue
		}
		if v, ok := ns.CFInt(ns.CFDictionaryGetValue(d, layerKey)); ok {
			w.layer = v
		}
		if v, ok := ns.CFFloat(ns.CFDictionaryGetValue(d, alphaKey)); ok {
			w.alpha = v
		}
		if b := ns.CFDictionaryGetValue(d, boundsKey); b != 0 {
			var r ns.Rect
			if ns.CGRectMakeWithDictionaryRepresentation(b, &r) {
				w.bounds = r
			}
		}
		out = append(out, w)
	}
	return out
}

// ProcessNameAt is the process of the app window (or the desktop) under the
// point (Quartz coordinates: top-left points). The Dock, the menu bar and
// other overlays (layer > 0) and invisible windows are looked through.
func (s *Services) ProcessNameAt(x, y int) string {
	fx, fy := float64(x), float64(y)
	for _, w := range windows() {
		b := w.bounds
		if fx < b.Origin.X || fy < b.Origin.Y || fx >= b.Origin.X+b.Size.Width || fy >= b.Origin.Y+b.Size.Height {
			continue
		}
		if w.layer > 0 || w.alpha <= 0 {
			continue
		}
		if name := ns.ProcessName(w.pid); name != "" && !strings.EqualFold(name, "WindowServer") {
			return name
		}
	}
	return ""
}

// IsFileManagerAt: Finder's window or the desktop is under the point.
func (s *Services) IsFileManagerAt(x, y int) bool {
	return strings.EqualFold(s.ProcessNameAt(x, y), "Finder")
}

// RunningAppNames are the apps with a window on screen, sorted, without Filee.
func (s *Services) RunningAppNames() []string {
	own := ns.ProcessName(int32(os.Getpid()))
	var out []string
	for _, w := range windows() {
		if w.layer != 0 {
			continue
		}
		name := ns.ProcessName(w.pid)
		if name == "" || strings.EqualFold(name, own) || slices.ContainsFunc(out, func(o string) bool { return strings.EqualFold(o, name) }) {
			continue
		}
		out = append(out, name)
	}
	slices.SortFunc(out, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return out
}

// frontmost is the frontmost app's process id and bundle identifier.
func frontmost() (int32, string, string) {
	var pid int32
	var id, name string
	ns.WithPool(func() {
		ws := ns.Send(ns.Class("NSWorkspace"), "sharedWorkspace")
		app := ns.Send(ws, "frontmostApplication")
		if app == 0 {
			return
		}
		pid = int32(ns.Int(app, "processIdentifier"))
		id = ns.GoString(ns.Send(app, "bundleIdentifier"))
		name = ns.GoString(ns.Send(app, "localizedName"))
	})
	return pid, id, name
}

// Foreground is the app keys go to.
func (s *Services) Foreground() *platform.ForegroundWindow {
	pid, id, _ := frontmost()
	if pid == 0 {
		return nil
	}
	return &platform.ForegroundWindow{ProcessName: ns.ProcessName(pid), IsFileManager: id == "com.apple.finder"}
}

// DescribeForegroundWindow is for the log.
func (s *Services) DescribeForegroundWindow() string {
	pid, id, name := frontmost()
	if pid == 0 {
		return ""
	}
	return fmt.Sprintf("%s (%s, pid %d)", name, id, pid)
}

// MoveToTrash moves the file to its volume's Trash, where Finder can put it back.
func (s *Services) MoveToTrash(path string) *platform.TrashRefusal {
	full, err := filepath.Abs(path)
	if err != nil {
		return &platform.TrashRefusal{ReasonKey: "trash.failed", Detail: err.Error()}
	}
	var refusal *platform.TrashRefusal
	ns.WithPool(func() {
		fm := ns.Send(ns.Class("NSFileManager"), "defaultManager")
		errp := new(ns.ID)
		if ns.Bool(fm, "trashItemAtURL:resultingItemURL:error:", ns.FileURL(full), uintptr(0), unsafe.Pointer(errp)) {
			return
		}
		refusal = &platform.TrashRefusal{ReasonKey: "trash.failed", Detail: ns.ErrorText(*errp)}
	})
	return refusal
}

// ShortcutTarget is the existing file or folder a Finder alias points to;
// "" for anything else, a broken alias, a target in a Trash, or one on a
// volume that isn't mounted (resolving never shows UI or mounts a server).
func (s *Services) ShortcutTarget(path string) string {
	full, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	fi, err := os.Lstat(full)
	if err != nil || fi.IsDir() {
		return ""
	}
	b := []byte(full)
	url := ns.CFURLCreateFromFileSystemRepresentation(0, &b[0], len(b), false)
	if url == 0 {
		return ""
	}
	defer ns.CFRelease(url)
	var value, cfErr uintptr
	if !ns.CFURLCopyResourcePropertyForKey(url, uintptr(ns.Constant("kCFURLIsAliasFileKey")), &value, &cfErr) {
		release(cfErr)
		return ""
	}
	isAlias := value != 0 && value == uintptr(ns.Constant("kCFBooleanTrue"))
	release(value)
	if !isAlias {
		return ""
	}
	data := ns.CFURLCreateBookmarkDataFromFile(0, url, &cfErr)
	if data == 0 {
		release(cfErr)
		return ""
	}
	defer ns.CFRelease(data)
	const withoutUI, withoutMounting = 1 << 8, 1 << 9
	var stale bool
	target := ns.CFURLCreateByResolvingBookmarkData(0, data, withoutUI|withoutMounting, 0, 0, &stale, &cfErr)
	release(cfErr)
	if target == 0 {
		return ""
	}
	defer ns.CFRelease(target)
	buf := make([]byte, 4096)
	if !ns.CFURLGetFileSystemRepresentation(target, true, &buf[0], len(buf)) {
		return ""
	}
	n := slices.Index(buf, 0)
	if n < 0 || !utf8.Valid(buf[:n]) {
		return ""
	}
	// The file system representation is decomposed (NFD); dropped paths are composed.
	resolved := norm.NFC.String(string(buf[:n]))
	if inTrash(resolved) {
		return ""
	}
	if _, err := os.Stat(resolved); err != nil {
		return ""
	}
	return resolved
}

func release(v uintptr) {
	if v != 0 {
		ns.CFRelease(v)
	}
}

func inTrash(p string) bool {
	home, _ := os.UserHomeDir()
	trash := filepath.Join(home, ".Trash") + "/"
	return strings.HasPrefix(p+"/", trash) || strings.Contains(p, "/.Trashes/")
}

const finderSelectionScript = `ObjC.import('Foundation');
const finder = Application('com.apple.finder');
JSON.stringify(finder.selection().map(item => ObjC.unwrap($.NSURL.URLWithString(item.url()).path)));`

// ErrSelection says Finder's selection couldn't be read (Automation permission).
var ErrSelection = errors.New("could not read Finder's selection. Allow Filee to control Finder in System Settings › Privacy & Security › Automation")

// FileManagerSelection lists the files selected in Finder when it is in front.
func (s *Services) FileManagerSelection() []string {
	files, err := s.Selection()
	if err != nil {
		return nil
	}
	return files
}

// Selection is FileManagerSelection with the reason it failed.
func (s *Services) Selection() ([]string, error) {
	s.mu.Lock()
	s.LastSelectionError = ""
	s.mu.Unlock()
	if _, id, _ := frontmost(); id != "com.apple.finder" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", finderSelectionScript).Output()
	var paths []string
	if err == nil {
		err = json.Unmarshal(out, &paths)
	}
	if err != nil {
		s.mu.Lock()
		s.LastSelectionError = err.Error()
		s.mu.Unlock()
		return nil, fmt.Errorf("%w: %v", ErrSelection, err)
	}
	var files []string
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			files = append(files, p)
		}
	}
	return files, nil
}

// PrefersReducedMotion follows Accessibility › Display › Reduce motion.
func (s *Services) PrefersReducedMotion() bool {
	reduce := false
	ns.WithPool(func() {
		ws := ns.Send(ns.Class("NSWorkspace"), "sharedWorkspace")
		reduce = ns.Bool(ws, "accessibilityDisplayShouldReduceMotion")
	})
	return reduce
}

// InBundle reports whether this executable runs from inside an .app bundle.
func InBundle() bool {
	id := ""
	ns.WithPool(func() { id = ns.GoString(ns.Send(ns.Send(ns.Class("NSBundle"), "mainBundle"), "bundleIdentifier")) })
	return id != ""
}

func launchAgentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", bundle.BundleID+".plist")
}

// SetStartWithSystem registers Filee as a login item: SMAppService (macOS 13+)
// when it runs as Filee.app, else a launch agent.
func (s *Services) SetStartWithSystem(enabled bool, executable string) error {
	if InBundle() && ns.Class("SMAppService") != 0 {
		var err error
		ns.WithPool(func() {
			svc := ns.Send(ns.Class("SMAppService"), "mainAppService")
			errp := new(ns.ID)
			sel := "unregisterAndReturnError:"
			if enabled {
				sel = "registerAndReturnError:"
			}
			// Unregistering one that isn't registered fails: only an error when enabling.
			if !ns.Bool(svc, sel, unsafe.Pointer(errp)) && enabled {
				err = errors.New(ns.ErrorText(*errp))
			}
		})
		// An agent an earlier copy wrote would start Filee twice.
		_ = os.Remove(launchAgentPath())
		return err
	}
	path := launchAgentPath()
	if !enabled {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if !filepath.IsAbs(executable) {
		return fmt.Errorf("filee's executable must have an absolute path: %q", executable)
	}
	if _, err := os.Stat(executable); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(bundle.LaunchAgent(executable)), 0o644)
}

// LoginItemStatus is SMAppService's status (0 not registered, 1 enabled,
// 2 requires approval, 3 not found), or -1 without it.
func LoginItemStatus() int {
	if ns.Class("SMAppService") == 0 {
		return -1
	}
	status := -1
	ns.WithPool(func() { status = ns.Int(ns.Send(ns.Class("SMAppService"), "mainAppService"), "status") })
	return status
}

// SetContextMenu shows or hides Filee's own service in Finder's right-click
// menu (the switch of System Settings › Keyboard › Keyboard Shortcuts ›
// Services) and removes the Automator Quick Action earlier builds installed.
func (s *Services) SetContextMenu(enabled bool, executable, label string) error {
	home, _ := os.UserHomeDir()
	_ = os.RemoveAll(filepath.Join(home, "Library", "Services", "Filee.workflow"))
	if err := run("/usr/bin/defaults", "write", "pbs", "NSServicesStatus", "-dict-add", bundle.ServiceStatusKey(), bundle.ServiceStatus(enabled)); err != nil {
		return err
	}
	return run("/System/Library/CoreServices/pbs", "-update")
}

func (s *Services) SetModernContextMenu(context.Context, bool, string) platform.ModernContextMenuResult {
	return platform.ModernContextMenuResult{Error: "The Windows Explorer extension is unavailable on macOS. Use Finder's Convert with Filee service."}
}

// RevealInFileManager selects the file in Finder (opens a folder).
func (s *Services) RevealInFileManager(path string) {
	full, _ := filepath.Abs(path)
	if fi, err := os.Stat(full); err == nil && !fi.IsDir() {
		go func() { _ = run("/usr/bin/open", "-R", full) }()
		return
	}
	go func() { _ = run("/usr/bin/open", full) }()
}

// OpenFile opens it like a double-click in Finder.
func (s *Services) OpenFile(path string) {
	full, _ := filepath.Abs(path)
	go func() { _ = run("/usr/bin/open", full) }()
}

// RaiseTopmost puts a window (an NSWindow pointer) back on top without activating Filee.
func (s *Services) RaiseTopmost(w uintptr) {
	if w == 0 {
		return
	}
	ns.OnMain(func() {
		win := ns.ID(w)
		ns.Send(win, "setLevel:", 101) // NSPopUpMenuWindowLevel
		ns.Send(win, "orderFrontRegardless")
	})
}

func run(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %v: %s", filepath.Base(name), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func symbolAddress(name string) uintptr { return uintptr(ns.SymbolAddress(name)) }

// PreferredLanguage is the first language of System Settings › Language &
// Region ("ko-KR", "zh-Hans-CN"); "" when macOS gives none. An app started
// from Finder has no LANG variable.
func PreferredLanguage() string {
	langs := ns.CFLocaleCopyPreferredLangs()
	if langs == 0 {
		return ""
	}
	defer ns.CFRelease(langs)
	if ns.CFArrayGetCount(langs) == 0 {
		return ""
	}
	return ns.GoCFString(ns.CFArrayGetValueAtIndex(langs, 0))
}
