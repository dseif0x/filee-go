// Package i18n looks up the user-facing texts by key. Translations are JSON
// files in locales/ (key → text, {0} {1} … for arguments), embedded in the
// binary. Only English ships for now; adding a language is adding
// locales/<code>.json (same keys as en.json, missing ones fall back to English).
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/dseif0x/filee-go/internal/presets"
	"github.com/dseif0x/filee-go/internal/profiles"
)

//go:embed locales/*.json
var locales embed.FS

// Fallback is the language every lookup falls back to.
const Fallback = "en"

// Localizer looks up texts in one language with English behind it.
type Localizer struct {
	lang     string
	texts    map[string]string
	fallback map[string]string
}

var (
	loadMu sync.Mutex
	loaded = map[string]map[string]string{}
)

func table(lang string) map[string]string {
	loadMu.Lock()
	defer loadMu.Unlock()
	if t, ok := loaded[lang]; ok {
		return t
	}
	t := map[string]string{}
	if b, err := locales.ReadFile("locales/" + lang + ".json"); err == nil {
		var raw map[string]any
		if json.Unmarshal(b, &raw) == nil {
			for k, v := range raw {
				if s, ok := v.(string); ok {
					t[k] = s
				}
			}
		}
	}
	loaded[lang] = t
	return t
}

// Languages lists the language codes that have a translation, English first.
func Languages() []string {
	out := []string{Fallback}
	entries, _ := fs.ReadDir(locales, "locales")
	for _, e := range entries {
		code := strings.TrimSuffix(e.Name(), ".json")
		if code != Fallback {
			out = append(out, code)
		}
	}
	slices.Sort(out[1:])
	return out
}

// New returns a localizer for lang ("auto" or "" picks the system language,
// an unknown one English).
func New(lang string) *Localizer {
	if lang == "" || strings.EqualFold(lang, "auto") {
		lang = SystemLanguage()
	}
	if !slices.Contains(Languages(), lang) {
		// "pt_BR.UTF-8" → "pt-BR", then "pt".
		lang = strings.ReplaceAll(strings.SplitN(lang, ".", 2)[0], "_", "-")
		switch {
		case slices.Contains(Languages(), lang):
		case slices.Contains(Languages(), strings.SplitN(lang, "-", 2)[0]):
			lang = strings.SplitN(lang, "-", 2)[0]
		default:
			lang = Fallback
		}
	}
	return &Localizer{lang: lang, texts: table(lang), fallback: table(Fallback)}
}

// SystemLanguage reads the language from the environment (LANG and friends),
// "en" when none is set. The GUI may know better (macOS preferences).
func SystemLanguage() string {
	for _, v := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		if s := os.Getenv(v); s != "" && s != "C" && s != "POSIX" {
			return strings.SplitN(s, ":", 2)[0]
		}
	}
	return Fallback
}

// Language is the code of the language in use.
func (l *Localizer) Language() string { return l.lang }

// Has reports whether key has a text (in the language or English).
func (l *Localizer) Has(key string) bool {
	_, a := l.texts[key]
	_, b := l.fallback[key]
	return a || b
}

var placeholder = regexp.MustCompile(`\{(\d+)\}`)

// Text returns the text for key, falling back to English, then to the key
// itself, with {0} {1} … replaced by args.
func (l *Localizer) Text(key string, args ...any) string {
	s, ok := l.texts[key]
	if !ok {
		if s, ok = l.fallback[key]; !ok {
			s = key
		}
	}
	if len(args) == 0 {
		return s
	}
	return placeholder.ReplaceAllStringFunc(s, func(m string) string {
		i, _ := strconv.Atoi(m[1 : len(m)-1])
		if i < len(args) {
			return fmt.Sprint(args[i])
		}
		return m
	})
}

var std = struct {
	sync.RWMutex
	l *Localizer
}{l: nil}

// Default is the app-wide localizer (English until SetLanguage).
func Default() *Localizer {
	std.RLock()
	l := std.l
	std.RUnlock()
	if l == nil {
		l = New(Fallback)
		std.Lock()
		if std.l == nil {
			std.l = l
		}
		l = std.l
		std.Unlock()
	}
	return l
}

// SetLanguage switches the app-wide localizer.
func SetLanguage(lang string) {
	l := New(lang)
	std.Lock()
	std.l = l
	std.Unlock()
}

// Text looks key up in the app-wide localizer.
func Text(key string, args ...any) string { return Default().Text(key, args...) }

// Notice is a file's notice with why in brackets, the reason translated and
// the system's own words after it: "The original was kept… (the file is open
// in another program: <detail>)".
func (l *Localizer) Notice(noticeKey, reasonKey, detail string) string {
	why := ""
	if reasonKey != "" {
		why = l.Text(reasonKey)
	}
	if strings.TrimSpace(detail) != "" {
		if why == "" {
			why = detail
		} else {
			why += ": " + detail
		}
	}
	if why == "" {
		return l.Text(noticeKey)
	}
	return l.Text(noticeKey) + " (" + why + ")"
}

// PresetName is the name a preset is shown with: the user's, else the
// localized built-in name, else the target format.
func (l *Localizer) PresetName(p *presets.Preset) string {
	switch {
	case strings.TrimSpace(p.Name) != "":
		return p.Name
	case p.NameKey != "":
		return l.Text(p.NameKey)
	}
	return strings.ToUpper(p.TargetFormat)
}

// DonutName is the compact name on the donut (where the target is shown underneath).
func (l *Localizer) DonutName(p *presets.Preset) string { return l.variant(p, ".donut") }

// RingName is the name on a slice of a group's ring ("Left" for "Rotate left").
func (l *Localizer) RingName(p *presets.Preset) string { return l.variant(p, ".short") }

func (l *Localizer) variant(p *presets.Preset, suffix string) string {
	if strings.TrimSpace(p.Name) != "" || p.NameKey == "" {
		return l.PresetName(p)
	}
	key := p.NameKey + suffix
	if t := l.Text(key); t != key {
		return t
	}
	return l.PresetName(p)
}

// ProfileName is the name a donut profile is shown with.
func (l *Localizer) ProfileName(p *profiles.Profile) string {
	switch {
	case strings.TrimSpace(p.Name) != "":
		return p.Name
	case p.NameKey != "":
		return l.Text(p.NameKey)
	}
	return p.ID
}

// Keys lists every key that has a text in the language or in English, sorted.
func (l *Localizer) Keys() []string {
	out := make([]string, 0, len(l.fallback)+len(l.texts))
	for k := range l.fallback {
		out = append(out, k)
	}
	for k := range l.texts {
		if _, ok := l.fallback[k]; !ok {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}
