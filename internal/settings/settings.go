// Package settings holds the user's settings and their library (presets,
// donut profiles, history) and saves them as JSON files in
// os.UserConfigDir()/filee (or FILEE_DATA_DIR). Writes are atomic: a crash
// never leaves a half-written file.
package settings

import (
	"encoding/json"
	"math"
	"runtime"
	"slices"
	"strings"

	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/presets"
	"github.com/dseif0x/filee-go/internal/watch"
)

// CurrentSchemaVersion is the settings format; files start fresh at it (no
// migrations of the C# Filee's files).
const CurrentSchemaVersion = 1

type ThemeMode string

const (
	ThemeSystem ThemeMode = "System"
	ThemeLight  ThemeMode = "Light"
	ThemeDark   ThemeMode = "Dark"
)

type ThemeStyle string

const (
	StyleSoft        ThemeStyle = "Soft"
	StyleMaterialYou ThemeStyle = "MaterialYou"
)

// CloseAction is what closing the main window does.
type CloseAction string

const (
	CloseAsk  CloseAction = "Ask"
	CloseTray CloseAction = "Tray"
	CloseQuit CloseAction = "Quit"
)

// Theme is the look and feel.
type Theme struct {
	Style ThemeStyle `json:"style"`
	Mode  ThemeMode  `json:"mode"`
	// Accent is #RRGGBB.
	Accent string `json:"accent"`
	// PresetKey is the curated light/dark pair in use ("mint"), "" for custom choices.
	PresetKey    string  `json:"presetKey,omitempty"`
	CornerRadius float64 `json:"cornerRadius"`
	// ReduceMotion: nil follows the operating system.
	ReduceMotion *bool `json:"reduceMotion,omitempty"`
}

// Donut is the geometry and appearance of the donut toolbar.
type Donut struct {
	OuterRadius float64 `json:"outerRadius"`
	// HoleRatio is the hole's radius as a fraction of the outer radius (0.2–0.7).
	HoleRatio float64 `json:"holeRatio"`
	// Opacity of the slices (MinDonutOpacity–1); labels stay opaque.
	Opacity float64 `json:"opacity"`
	// FlipKey flips the open donut to its other face ("Tab", "F2"); "" for none.
	FlipKey       string `json:"flipKey"`
	FlipWithWheel bool   `json:"flipWithWheel"`
}

const (
	MinDonutOpacity = 0.1
	DefaultFlipKey  = "Tab"
)

// ReservedFlipKeys never flip the donut: Esc closes it, Enter and Space
// convert, the arrows move between slices, Backspace folds a group's ring.
var ReservedFlipKeys = []string{"Escape", "Enter", "Space", "Left", "Right", "Up", "Down", "Backspace"}

// IsFlipKeyAllowed: key is not one the donut uses nor a keyboard shortcut's key.
func IsFlipKeyAllowed(key string, gestures []*TriggerGesture) bool {
	if strings.TrimSpace(key) == "" || slices.ContainsFunc(ReservedFlipKeys, func(r string) bool { return strings.EqualFold(r, key) }) {
		return false
	}
	return !slices.ContainsFunc(gestures, func(g *TriggerGesture) bool { return g.Kind == KeyChord && strings.EqualFold(g.Key, key) })
}

// EffectiveFlipKey is the key that flips the donut now: none when cleared,
// Tab in place of one that isn't allowed, or none when Tab isn't either.
func (d *Donut) EffectiveFlipKey(gestures []*TriggerGesture) string {
	k := strings.TrimSpace(d.FlipKey)
	if k == "" {
		return ""
	}
	if IsFlipKeyAllowed(k, gestures) {
		return k
	}
	if IsFlipKeyAllowed(DefaultFlipKey, gestures) {
		return DefaultFlipKey
	}
	return ""
}

// WindowBounds is where a window was: its corner in screen pixels and its
// size in device-independent pixels; a part not known is nil.
type WindowBounds struct {
	X         *int     `json:"x,omitempty"`
	Y         *int     `json:"y,omitempty"`
	Width     *float64 `json:"width,omitempty"`
	Height    *float64 `json:"height,omitempty"`
	Maximized bool     `json:"maximized,omitempty"`
}

// DefaultOutput is where presets set to "Default" save.
type DefaultOutput struct {
	Location      presets.OutputLocation `json:"location"`
	SubfolderName string                 `json:"subfolderName"`
	CustomFolder  string                 `json:"customFolder"`
}

// BrowserDrops is where files dragged from a web browser are saved.
type BrowserDrops struct {
	// Folder is "" for the user's Downloads folder (looked up each time).
	Folder string `json:"folder"`
	// SaveOnlyConverted: the original goes into a work folder and is deleted afterwards.
	SaveOnlyConverted bool `json:"saveOnlyConverted,omitempty"`
}

const (
	MinHistoryLimit = 50
	MaxHistoryLimit = 2000
)

// DefaultEnginePriority is the engine order of a new install (ids, preferred first).
var DefaultEnginePriority = []string{
	"magick", "pdfium", "pdfsharp", "rhwp", "unhwp", "markdown", "spreadsheet", "ooxml", "hwpx-writer", "docx-writer",
	"pdf-text", "pdf-slides", "email", "data", "vector", "icns", "font", "cad", "model3d", "geo", "archive", "ebook",
	"subtitle", "wic", "ffmpeg", "pandoc", "ghostscript", "calibre", "libreoffice",
}

// AppSettings are all user settings except presets, profiles and history.
type AppSettings struct {
	SchemaVersion int `json:"schemaVersion"`
	// Language is "auto" or a code ("en", "ko", "zh-CN").
	Language           string      `json:"language"`
	StartWithSystem    bool        `json:"startWithSystem"`
	ContextMenuEnabled bool        `json:"contextMenuEnabled"`
	CheckForUpdates    bool        `json:"checkForUpdates"`
	CloseAction        CloseAction `json:"closeAction"`
	KeepHistory        bool        `json:"keepHistory"`
	// HistoryLimit is how many finished conversions the history keeps (see EffectiveHistoryLimit).
	HistoryLimit    int  `json:"historyLimit"`
	ShowInDock      bool `json:"showInDock"`
	ShowMenuBarIcon bool `json:"showMenuBarIcon"`
	// NotifiedUpdateVersion is the newest version the user was told about.
	NotifiedUpdateVersion string `json:"notifiedUpdateVersion,omitempty"`
	WhatsNewSeenVersion   string `json:"whatsNewSeenVersion,omitempty"`
	// NotifiedEngineVersions: engine package id → the version its update notice was shown for.
	NotifiedEngineVersions map[string]string `json:"notifiedEngineVersions"`
	// Paused turns every trigger gesture off (tray "Pause").
	Paused   bool              `json:"paused,omitempty"`
	Theme    Theme             `json:"theme"`
	Donut    Donut             `json:"donut"`
	Triggers []*TriggerGesture `json:"triggers"`
	// ExcludedProcesses are apps in which gestures never trigger.
	ExcludedProcesses []string `json:"excludedProcesses"`
	// EnginePriority are engine ids in preferred order.
	EnginePriority           []string      `json:"enginePriority"`
	FirstRunCompleted        bool          `json:"firstRunCompleted,omitempty"`
	SuccessfulConversions    int           `json:"successfulConversions,omitempty"`
	FeedbackPromptShown      bool          `json:"feedbackPromptShown,omitempty"`
	AccessibilityPromptShown bool          `json:"accessibilityPromptShown,omitempty"`
	WatchFolders             []*watch.Rule `json:"watchFolders"`
	DefaultOutput            DefaultOutput `json:"defaultOutput"`
	BrowserDrops             BrowserDrops  `json:"browserDrops"`
	// EngineOwnCopies: engine package id ("ffmpeg") → the program the user picked instead of Filee's download.
	EngineOwnCopies         map[string]string `json:"engineOwnCopies"`
	LibreOfficeForSlides    bool              `json:"libreOfficeForSlides"`
	LibreOfficeForHwp       bool              `json:"libreOfficeForHwp,omitempty"`
	LibreOfficeForDocuments bool              `json:"libreOfficeForDocuments,omitempty"`
	LibreOfficeForSheets    bool              `json:"libreOfficeForSheets,omitempty"`
	// KeepFileDates: every conversion keeps the original's dates, whatever the preset says.
	KeepFileDates bool `json:"keepFileDates,omitempty"`
	// ZipGeoResults: every geospatial conversion is saved as a ZIP.
	ZipGeoResults bool `json:"zipGeoResults,omitempty"`
	// UIScale is how large the windows draw everything (0.7–1.8).
	UIScale    float64       `json:"uiScale"`
	MainWindow *WindowBounds `json:"mainWindow,omitempty"`
}

// Defaults returns the settings of a new install.
func Defaults() *AppSettings {
	return &AppSettings{
		SchemaVersion: CurrentSchemaVersion, Language: "auto", StartWithSystem: true, ContextMenuEnabled: true, CheckForUpdates: true,
		CloseAction: CloseAsk, KeepHistory: true, HistoryLimit: 200, ShowInDock: true, ShowMenuBarIcon: true,
		NotifiedEngineVersions: map[string]string{},
		Theme:                  Theme{Style: StyleSoft, Mode: ThemeSystem, Accent: "#7F77DD", CornerRadius: 16},
		Donut:                  Donut{OuterRadius: 150, HoleRatio: 0.42, Opacity: 0.96, FlipKey: DefaultFlipKey, FlipWithWheel: true},
		Triggers:               DefaultTriggers(), ExcludedProcesses: []string{}, EnginePriority: slices.Clone(DefaultEnginePriority),
		WatchFolders: []*watch.Rule{}, DefaultOutput: DefaultOutput{Location: presets.LocationSameFolder, SubfolderName: "converted"},
		EngineOwnCopies: map[string]string{}, LibreOfficeForSlides: true, UIScale: 1,
	}
}

// UnmarshalJSON reads settings over the defaults, so missing fields keep
// their default values; unknown enumeration values become the defaults.
func (s *AppSettings) UnmarshalJSON(b []byte) error {
	type plain AppSettings
	d := plain(*Defaults())
	// Lists the file has replace the defaults instead of being merged into them.
	d.Triggers, d.EnginePriority = nil, nil
	if err := json.Unmarshal(b, &d); err != nil {
		return err
	}
	*s = AppSettings(d)
	s.normalize()
	return nil
}

func (s *AppSettings) normalize() {
	def := Defaults()
	lenient(&s.CloseAction, CloseAsk, CloseAsk, CloseTray, CloseQuit)
	lenient(&s.Theme.Mode, ThemeSystem, ThemeSystem, ThemeLight, ThemeDark)
	lenient(&s.Theme.Style, StyleSoft, StyleSoft, StyleMaterialYou)
	lenient(&s.DefaultOutput.Location, presets.LocationSameFolder, presets.LocationSameFolder, presets.LocationSubfolder, presets.LocationCustomFolder)
	if len(s.Triggers) == 0 {
		s.Triggers = DefaultTriggers()
	}
	for _, t := range s.Triggers {
		t.normalize()
	}
	if s.EnginePriority == nil {
		s.EnginePriority = def.EnginePriority
	}
	if s.NotifiedEngineVersions == nil {
		s.NotifiedEngineVersions = map[string]string{}
	}
	if s.EngineOwnCopies == nil {
		s.EngineOwnCopies = map[string]string{}
	}
	if s.ExcludedProcesses == nil {
		s.ExcludedProcesses = []string{}
	}
	if s.WatchFolders == nil {
		s.WatchFolders = []*watch.Rule{}
	}
	for _, w := range s.WatchFolders {
		lenient(&w.Originals, watch.Keep, watch.Keep, watch.MoveToOriginals)
	}
	if s.UIScale <= 0 || math.IsNaN(s.UIScale) {
		s.UIScale = 1
	}
	s.SchemaVersion = CurrentSchemaVersion
}

func lenient[T ~string](v *T, def T, all ...T) {
	for _, a := range all {
		if strings.EqualFold(string(*v), string(a)) {
			*v = a
			return
		}
	}
	*v = def
}

// EffectiveHistoryLimit is HistoryLimit within its range.
func (s *AppSettings) EffectiveHistoryLimit() int {
	return min(MaxHistoryLimit, max(MinHistoryLimit, s.HistoryLimit))
}

// EffectiveUIScale is UIScale within 0.7–1.8.
func (s *AppSettings) EffectiveUIScale() float64 { return min(1.8, max(0.7, s.UIScale)) }

// WithDefaultOutput returns p itself, or a copy with the app-wide output
// settings applied: it saves to DefaultOutput when its location is Default,
// keeps the original dates with KeepFileDates, and is zipped with
// ZipGeoResults for a geospatial target. Name pattern and conflict handling
// stay the preset's.
func (s *AppSettings) WithDefaultOutput(p *presets.Preset) *presets.Preset {
	location := p.Output.Location == presets.LocationDefault
	dates := s.KeepFileDates && !p.Output.KeepDates
	zip := s.ZipGeoResults && !p.Output.ZipResult && formats.CategoryOf(p.TargetFormat) == formats.Geo
	if !location && !dates && !zip {
		return p
	}
	c := p.Clone()
	if location {
		c.Output.Location = s.DefaultOutput.Location
		if c.Output.Location == presets.LocationDefault || c.Output.Location == "" {
			c.Output.Location = presets.LocationSameFolder
		}
		c.Output.SubfolderName = s.DefaultOutput.SubfolderName
		c.Output.SubfolderOnlyForSeveralFiles = false
		c.Output.CustomFolder = s.DefaultOutput.CustomFolder
	}
	if dates {
		c.Output.KeepDates = true
	}
	if zip {
		c.Output.ZipResult = true
	}
	return c
}

// Clone returns a deep copy (via JSON, which every field survives).
func (s *AppSettings) Clone() *AppSettings {
	b, _ := json.Marshal(s)
	c := &AppSettings{}
	_ = json.Unmarshal(b, c)
	return c
}

var isMac = runtime.GOOS == "darwin"
