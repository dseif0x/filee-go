// Package watch converts the files that land in a watched folder (Settings →
// Watch folders, or "filee watch" on the command line).
package watch

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/presets"
)

// AfterConversion is what happens to a source file once it was converted.
type AfterConversion string

const (
	// Keep leaves the source where it is.
	Keep AfterConversion = "Keep"
	// MoveToOriginals moves it into the originals folder (an inbox).
	MoveToOriginals AfterConversion = "MoveToOriginals"
)

const (
	// DefaultOutputName is the default output folder inside the watched folder.
	DefaultOutputName = "converted"
	// OriginalsName is the folder converted sources move into with MoveToOriginals.
	OriginalsName = "originals"
)

// Rule is one watched folder and how its files are converted.
type Rule struct {
	ID                string `json:"id"`
	Enabled           bool   `json:"enabled"`
	Folder            string `json:"folder"`
	PresetID          string `json:"presetId"`
	IncludeSubfolders bool   `json:"includeSubfolders,omitempty"`
	// OutputFolder is where converted files go; "" means "converted" inside Folder.
	OutputFolder string          `json:"outputFolder"`
	Originals    AfterConversion `json:"originals"`
	// Include are the file name patterns converted (FileNameFilter), empty for all.
	Include []string `json:"include"`
	// FileNamePattern of converted files; "" for the preset's.
	FileNamePattern string `json:"fileNamePattern"`
	// Renames are applied after the preset's.
	Renames []presets.RenameStep `json:"renames"`
}

// NewRule returns an enabled rule with a fresh id.
func NewRule(folder, presetID string) *Rule {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return &Rule{ID: hex.EncodeToString(b), Enabled: true, Folder: folder, PresetID: presetID, Originals: Keep, Include: []string{}, Renames: []presets.RenameStep{}}
}

// UnmarshalJSON reads a rule over the defaults of a new one.
func (r *Rule) UnmarshalJSON(b []byte) error {
	type plain Rule
	d := plain(*NewRule("", ""))
	d.ID = ""
	if err := json.Unmarshal(b, &d); err != nil {
		return err
	}
	*r = Rule(d)
	if r.ID == "" {
		r.ID = NewRule("", "").ID
	}
	if r.Include == nil {
		r.Include = []string{}
	}
	if r.Renames == nil {
		r.Renames = []presets.RenameStep{}
	}
	return nil
}

// ResolvedOutputFolder is the output folder actually used.
func (r *Rule) ResolvedOutputFolder() string {
	if strings.TrimSpace(r.OutputFolder) == "" {
		return filepath.Join(r.Folder, DefaultOutputName)
	}
	return r.OutputFolder
}

// OriginalsFolder is where converted sources go with MoveToOriginals.
func (r *Rule) OriginalsFolder() string { return filepath.Join(r.Folder, OriginalsName) }

// Apply is the preset as this folder converts with it: output into the
// resolved output folder, files from subfolders into the same subfolders
// there, and the folder's own name rules where set.
func (r *Rule) Apply(p *presets.Preset) *presets.Preset {
	a := p.Clone()
	a.Output.Location = presets.LocationCustomFolder
	a.Output.CustomFolder = r.ResolvedOutputFolder()
	a.Output.KeepFoldersFrom = r.Folder
	// A watch folder never replaces what it watches: the result would be converted again.
	a.Output.ReplaceOriginal = false
	if strings.TrimSpace(r.FileNamePattern) != "" {
		a.Output.FileNamePattern = strings.TrimSpace(r.FileNamePattern)
	}
	// Steps without a pattern are being typed; leaving them out keeps the name.
	for _, s := range r.Renames {
		if !s.IsBlank() {
			a.Output.Renames = append(a.Output.Renames, s)
		}
	}
	return a
}

// Clone returns a deep copy.
func (r *Rule) Clone() *Rule {
	c := *r
	c.Include = slices.Clone(r.Include)
	c.Renames = slices.Clone(r.Renames)
	return &c
}

// ---- file name filter ----

// Filter says which files of a watch folder are converted: patterns such as
// "*.heic" and "scan_*", or a regular expression between slashes
// ("/^IMG_\d+/"). A file matching any pattern is converted; none means all.
type Filter struct{ patterns []*regexp.Regexp }

// AllFiles lets every file through.
var AllFiles = &Filter{}

var filterCache sync.Map

// NormalizePattern turns what someone typed into a pattern: an extension
// (heic, .heic) becomes *.heic, text without * or ? matches names containing
// it (scan → *scan*), and a pattern or a /regular expression/ stays. "" for blank input.
func NormalizePattern(typed string) string {
	t := strings.TrimSpace(typed)
	if t == "" {
		return ""
	}
	if isRegex(t) || strings.ContainsAny(t, "*?") {
		return t
	}
	ext := strings.TrimLeft(t, ".")
	if ext != "" && !strings.Contains(ext, ".") && formats.ByExtension(ext) != nil {
		return "*." + strings.ToLower(ext)
	}
	return "*" + t + "*"
}

// ValidatePattern says why a pattern can't be used (an invalid regular expression), or "".
func ValidatePattern(p string) string {
	if _, err := toRegex(p); err != nil {
		return err.Error()
	}
	return ""
}

// FilterOf is the filter of patterns; invalid ones are skipped.
func FilterOf(patterns []string) *Filter {
	if len(patterns) == 0 {
		return AllFiles
	}
	key := strings.Join(patterns, "\n")
	if f, ok := filterCache.Load(key); ok {
		return f.(*Filter)
	}
	f := &Filter{}
	for _, p := range patterns {
		if re, err := toRegex(p); err == nil {
			f.patterns = append(f.patterns, re)
		}
	}
	if len(f.patterns) == 0 {
		f = AllFiles
	}
	filterCache.Store(key, f)
	return f
}

// Matches reports whether the file name (without folder) matches a pattern.
// Go's regular expressions run in linear time, so no pattern can hang.
func (f *Filter) Matches(name string) bool {
	if len(f.patterns) == 0 {
		return true
	}
	for _, p := range f.patterns {
		if p.MatchString(name) {
			return true
		}
	}
	return false
}

func isRegex(p string) bool {
	return len(p) > 2 && strings.HasPrefix(p, "/") && strings.HasSuffix(p, "/")
}

func toRegex(p string) (*regexp.Regexp, error) {
	if isRegex(p) {
		return regexp.Compile("(?i)" + p[1:len(p)-1])
	}
	var b strings.Builder
	b.WriteString("(?is)^")
	for _, c := range strings.TrimSpace(p) {
		switch c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// DefaultSettleTime is how long a file must stay unchanged before it is
// converted. Longer on macOS and Linux: there a file still being written can
// be opened all the same, so only the wait tells a paused copy from a finished one.
var DefaultSettleTime = defaultSettle()

func defaultSettle() time.Duration {
	if windows {
		return 2 * time.Second
	}
	return 5 * time.Second
}
