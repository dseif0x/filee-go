// Package platform is the operating-system specific part of Filee as an
// interface: the GUI agents implement Services per OS (macOS first); every
// other package only sees the interface. Null is the no-op default.
package platform

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// TrashRefusal says why a file was not moved to the Recycle Bin / Trash: a
// localization key (trash.*) and, when the system said more, its own words.
type TrashRefusal struct {
	ReasonKey string
	Detail    string
}

// ForegroundWindow is the window in front: its process name as ProcessNameAt
// reports it, and whether it belongs to the file manager or the desktop.
type ForegroundWindow struct {
	ProcessName   string
	IsFileManager bool
}

// ModernContextMenuState tells whether "Convert with Filee" is in the
// top-level Windows 11 Explorer menu.
type ModernContextMenuState string

const (
	ModernMenuUnsupported ModernContextMenuState = "Unsupported"
	ModernMenuOff         ModernContextMenuState = "Off"
	ModernMenuOn          ModernContextMenuState = "On"
	ModernMenuOutdated    ModernContextMenuState = "Outdated"
)

// ModernContextMenuResult is the outcome of SetModernContextMenu.
type ModernContextMenuResult struct {
	Succeeded bool
	Cancelled bool
	Error     string
}

// ErrStartupEntryNotKept: the "start with the system" entry read back after
// writing it was gone or different (security software took it away).
var ErrStartupEntryNotKept = errors.New("the start-up entry was not kept")

// Services are the operating-system specific services.
type Services interface {
	SupportsGlobalPointerGestures() bool
	SupportsSelectionShortcut() bool
	// PressCarriesPosition is false where mouse presses arrive without a
	// position (Wayland reports them at (0, 0)).
	PressCarriesPosition() bool
	SupportsDockVisibility() bool
	// SetShowInDock returns whether the platform accepted the change.
	SetShowInDock(show bool) bool

	// IsFileManagerAt reports whether the window at the screen point belongs
	// to the file manager or desktop (pixels on Windows/Linux, points on macOS).
	IsFileManagerAt(x, y int) bool
	// ProcessNameAt is the process owning the window under the point, or "".
	ProcessNameAt(x, y int) string
	// Foreground is the window keys go to; nil where the platform can't tell.
	Foreground() *ForegroundWindow
	// RunningAppNames are the apps with a window open, sorted, without Filee.
	RunningAppNames() []string

	// MoveToTrash moves a file to the Recycle Bin / Trash, never deleting it
	// for good. nil when it worked; otherwise why not, and the file stays.
	MoveToTrash(path string) *TrashRefusal

	// ShortcutTarget is the file a shortcut (.lnk, Finder alias) points to, or "".
	ShortcutTarget(path string) string
	// FileManagerSelection lists the files selected in the front file manager window.
	FileManagerSelection() []string
	// DescribeForegroundWindow is for the log ("CabinetWClass / explorer"), "" if unknown.
	DescribeForegroundWindow() string
	// DownloadsFolder is where files dragged from a browser are saved by default.
	DownloadsFolder() string

	SystemDragThreshold() int
	PrefersReducedMotion() bool

	SetStartWithSystem(enabled bool, executable string) error
	SetContextMenu(enabled bool, executable, label string) error
	ModernContextMenu(executable string) ModernContextMenuState
	SetModernContextMenu(ctx context.Context, enabled bool, executable string) ModernContextMenuResult

	RevealInFileManager(path string)
	OpenFile(path string)
	SupportsOpenWith() bool
	OpenWith(path string)
	// RaiseTopmost puts an always-on-top window back on top without activating it.
	RaiseTopmost(window uintptr)
}

// ResolveShortcuts replaces every shortcut by what it points to, keeping the
// order; a shortcut and its target brought in together give that file once.
// A broken shortcut stays, so it is reported as unsupported.
func ResolveShortcuts(s Services, paths []string) []string {
	resolved := make([]string, len(paths))
	changed := false
	for i, p := range paths {
		resolved[i] = p
		if t := s.ShortcutTarget(p); t != "" {
			resolved[i] = t
			changed = true
		}
	}
	if !changed {
		return paths
	}
	var out []string
	for _, p := range resolved {
		if !slices.ContainsFunc(out, func(o string) bool { return SamePath(o, p) }) {
			out = append(out, p)
		}
	}
	return out
}

// Null is the fallback where no implementation exists yet: everything is a
// no-op, except opening files and folders with the system's opener.
type Null struct{}

var _ Services = Null{}

func (Null) SupportsGlobalPointerGestures() bool   { return false }
func (Null) SupportsSelectionShortcut() bool       { return false }
func (Null) PressCarriesPosition() bool            { return true }
func (Null) SupportsDockVisibility() bool          { return false }
func (Null) SetShowInDock(bool) bool               { return false }
func (Null) IsFileManagerAt(int, int) bool         { return false }
func (Null) ProcessNameAt(int, int) string         { return "" }
func (Null) Foreground() *ForegroundWindow         { return nil }
func (Null) RunningAppNames() []string             { return nil }
func (Null) ShortcutTarget(string) string          { return "" }
func (Null) FileManagerSelection() []string        { return nil }
func (Null) DescribeForegroundWindow() string      { return "" }
func (Null) SystemDragThreshold() int              { return 4 }
func (Null) PrefersReducedMotion() bool            { return false }
func (Null) SupportsOpenWith() bool                { return false }
func (Null) OpenWith(string)                       {}
func (Null) RaiseTopmost(uintptr)                  {}
func (Null) SetStartWithSystem(bool, string) error { return nil }
func (Null) SetContextMenu(bool, string, string) error {
	return nil
}
func (Null) ModernContextMenu(string) ModernContextMenuState { return ModernMenuUnsupported }
func (Null) SetModernContextMenu(context.Context, bool, string) ModernContextMenuResult {
	return ModernContextMenuResult{Error: "Not supported on this platform."}
}

// MoveToTrash: there is no Recycle Bin to use.
func (Null) MoveToTrash(string) *TrashRefusal {
	return &TrashRefusal{ReasonKey: "trash.no_recycle_bin"}
}

// DownloadsFolder is ~/Downloads.
func (Null) DownloadsFolder() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	return filepath.Join(home, "Downloads")
}

func (Null) RevealInFileManager(path string) {
	target := path
	if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
		target = filepath.Dir(path)
	}
	open(target)
}

func (Null) OpenFile(path string) { open(path) }

func open(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("explorer", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if cmd.Start() == nil {
		go func() { _ = cmd.Wait() }()
	}
}

// CaseInsensitivePaths: Windows compares paths without case; Unix volumes can
// be case-sensitive (APFS too, when formatted that way).
var CaseInsensitivePaths = runtime.GOOS == "windows"

// PathKey is a path as a map key under the platform's comparison.
func PathKey(p string) string {
	if CaseInsensitivePaths {
		return strings.ToLower(p)
	}
	return p
}

// SamePath compares two paths as the platform does (after making them absolute).
func SamePath(a, b string) bool {
	a, _ = filepath.Abs(a)
	b, _ = filepath.Abs(b)
	if CaseInsensitivePaths {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// IsInside reports whether path lies strictly below folder.
func IsInside(path, folder string) bool {
	if strings.TrimSpace(folder) == "" {
		return false
	}
	root, err := filepath.Abs(folder)
	if err != nil {
		return false
	}
	root = strings.TrimRight(root, `/\`) + string(filepath.Separator)
	full, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	if CaseInsensitivePaths {
		return strings.HasPrefix(strings.ToLower(full), strings.ToLower(root))
	}
	return strings.HasPrefix(full, root)
}
