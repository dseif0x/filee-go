package settings

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// A user colour theme: theme.json next to settings.json overrides the
// surface, text and status colours of the light and/or dark variant. It is a
// file of its own so a theme can be shared and replaced on its own; the
// accent colour stays in Theme.

const (
	// ThemeFileName is the file inside the data folder.
	ThemeFileName = "theme.json"
	// MaxThemeBytes: larger files are skipped (a real theme is well under 1 KB).
	MaxThemeBytes = 64 * 1024
)

// ThemeColorKeys are the colour names a theme may set.
var ThemeColorKeys = []string{"page", "surface", "surfaceAlt", "border", "text", "textSecondary", "danger", "success", "warning"}

var hexColor = regexp.MustCompile(`^#([0-9A-Fa-f]{6}|[0-9A-Fa-f]{8})$`)

// CustomTheme holds colours by variant (name → #RRGGBB or #AARRGGBB).
type CustomTheme struct {
	Light map[string]string `json:"light"`
	Dark  map[string]string `json:"dark"`
}

// IsEmpty: neither variant sets a colour.
func (t *CustomTheme) IsEmpty() bool { return len(t.Light) == 0 && len(t.Dark) == 0 }

// LoadCustomTheme reads theme.json from dir. Unknown names and values that
// aren't hex colours are dropped (and logged); a missing, empty or unreadable
// file gives nil, so Filee's own colours apply. The file is the user's: it is
// never rewritten.
func LoadCustomTheme(dir string, log *slog.Logger) *CustomTheme {
	if log == nil {
		log = slog.Default()
	}
	path := filepath.Join(dir, ThemeFileName)
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if fi.Size() > MaxThemeBytes {
		log.Warn("custom theme too large; using the built-in colours", "path", path, "max", MaxThemeBytes)
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		log.Warn("custom theme could not be read", "path", path, "error", err)
		return nil
	}
	var raw struct {
		Light map[string]any `json:"light"`
		Dark  map[string]any `json:"dark"`
	}
	// Variant names are case-sensitive: "Dark" sets nothing (encoding/json would match it).
	var strict map[string]json.RawMessage
	if err := json.Unmarshal(b, &strict); err != nil {
		log.Warn("custom theme could not be read", "path", path, "error", err)
		return nil
	}
	if v, ok := strict["light"]; ok {
		_ = json.Unmarshal(v, &raw.Light)
	}
	if v, ok := strict["dark"]; ok {
		_ = json.Unmarshal(v, &raw.Dark)
	}
	t := &CustomTheme{Light: clean(raw.Light, "light", path, log), Dark: clean(raw.Dark, "dark", path, log)}
	if t.IsEmpty() {
		log.Warn(`custom theme sets no colours Filee can use (expected "light"/"dark" objects of #RRGGBB values)`, "path", path)
		return nil
	}
	return t
}

func clean(colors map[string]any, variant, path string, log *slog.Logger) map[string]string {
	out := map[string]string{}
	for name, v := range colors {
		key := ""
		for _, k := range ThemeColorKeys {
			if strings.EqualFold(k, name) {
				key = k
			}
		}
		s, isString := v.(string)
		switch {
		case key == "":
			log.Warn("custom theme: unknown colour ignored", "path", path, "variant", variant, "name", name)
		case !isString || !hexColor.MatchString(s):
			log.Warn("custom theme: not #RRGGBB or #AARRGGBB, ignored", "path", path, "variant", variant, "name", name)
		default:
			out[key] = s
		}
	}
	return out
}
