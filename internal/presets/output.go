package presets

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/dseif0x/filee-go/internal/formats"
)

type OutputLocation string

const (
	// LocationDefault is the app-wide default location (settings).
	LocationDefault      OutputLocation = "Default"
	LocationSameFolder   OutputLocation = "SameFolder"
	LocationSubfolder    OutputLocation = "Subfolder"
	LocationCustomFolder OutputLocation = "CustomFolder"
)

type ConflictPolicy string

const (
	ConflictRename    ConflictPolicy = "Rename"
	ConflictOverwrite ConflictPolicy = "Overwrite"
	ConflictSkip      ConflictPolicy = "Skip"
)

// OutputRule is the output location, file naming and conflict handling of a preset.
type OutputRule struct {
	Location     OutputLocation `json:"location"`
	AskEveryTime bool           `json:"askEveryTime,omitempty"`
	// ConfirmedPath is the file the save dialog already asked to replace.
	ConfirmedPath                string         `json:"-"`
	SubfolderName                string         `json:"subfolderName"`
	SubfolderOnlyForSeveralFiles bool           `json:"subfolderOnlyForSeveralFiles,omitempty"`
	CustomFolder                 string         `json:"customFolder,omitempty"`
	KeepFoldersFrom              string         `json:"keepFoldersFrom,omitempty"`
	FileNamePattern              string         `json:"fileNamePattern"`
	Renames                      []RenameStep   `json:"renames,omitempty"`
	Conflict                     ConflictPolicy `json:"conflict"`
	KeepDates                    bool           `json:"keepDates,omitempty"`
	// SourceFolder overrides the folder outputs are placed next to (watch folders, web drops).
	SourceFolder    string `json:"-"`
	ZipResult       bool   `json:"zipResult,omitempty"`
	OnlyIfSmaller   bool   `json:"onlyIfSmaller,omitempty"`
	ReplaceOriginal bool   `json:"replaceOriginal,omitempty"`
}

func DefaultOutputRule() OutputRule {
	return OutputRule{Location: LocationDefault, SubfolderName: "converted", FileNamePattern: "{name}", Conflict: ConflictRename}
}

type RenameKind string

const (
	RenameReplace             RenameKind = "Replace"
	RenameRemove              RenameKind = "Remove"
	RenamePrefix              RenameKind = "Prefix"
	RenameSuffix              RenameKind = "Suffix"
	RenameSpacesToUnderscores RenameKind = "SpacesToUnderscores"
	RenameRemoveCopyNumber    RenameKind = "RemoveCopyNumber"
	RenameRemoveBrackets      RenameKind = "RemoveBrackets"
	RenameRemoveLeadingNumber RenameKind = "RemoveLeadingNumber"
	RenameRemoveSymbols       RenameKind = "RemoveSymbols"
	RenameLowercase           RenameKind = "Lowercase"
	RenameUppercase           RenameKind = "Uppercase"
	RenameRegex               RenameKind = "Regex"
)

// RenameStep is one step of the file name rewrite.
type RenameStep struct {
	Kind    RenameKind `json:"kind"`
	Find    string     `json:"find,omitempty"`
	Replace string     `json:"replace,omitempty"`
}

func (r RenameStep) IsBlank() bool {
	switch r.Kind {
	case RenameReplace, RenameRemove, RenameRegex:
		return r.Find == ""
	case RenamePrefix, RenameSuffix:
		return r.Replace == ""
	}
	return false
}

var (
	reSpaces        = regexp.MustCompile(`\s+`)
	reCopyNumber    = regexp.MustCompile(`\s*\(\d+\)$`)
	reBrackets      = regexp.MustCompile(`\s*(\([^)]*\)|\[[^\]]*\]|\{[^}]*\})`)
	reLeadingNumber = regexp.MustCompile(`^\d+[\s._-]*`)
	reSymbols       = regexp.MustCompile(`[^\p{L}\p{N}\s._-]`)
	reToken         = regexp.MustCompile(`(?i)\{(name|ext|preset|date|time|index)\}`)
)

// Apply rewrites name.
func (r RenameStep) Apply(name string) string {
	if r.IsBlank() {
		return name
	}
	switch r.Kind {
	case RenameReplace:
		return replaceFold(name, r.Find, r.Replace)
	case RenameRemove:
		return replaceFold(name, r.Find, "")
	case RenamePrefix:
		return r.Replace + name
	case RenameSuffix:
		return name + r.Replace
	case RenameSpacesToUnderscores:
		return reSpaces.ReplaceAllString(name, "_")
	case RenameRemoveCopyNumber:
		return reCopyNumber.ReplaceAllString(name, "")
	case RenameRemoveBrackets:
		return strings.TrimSpace(reBrackets.ReplaceAllString(name, ""))
	case RenameRemoveLeadingNumber:
		return reLeadingNumber.ReplaceAllString(name, "")
	case RenameRemoveSymbols:
		return reSymbols.ReplaceAllString(name, "")
	case RenameLowercase:
		return strings.ToLower(name)
	case RenameUppercase:
		return strings.ToUpper(name)
	case RenameRegex:
		return RegexRename(name, r.Find, r.Replace)
	}
	return name
}

// replaceFold replaces every case-insensitive occurrence of find.
func replaceFold(s, find, repl string) string {
	if find == "" {
		return s
	}
	re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(find))
	return re.ReplaceAllLiteralString(s, repl)
}

// RegexRename applies a user regular expression; a broken one leaves name as is.
// The replacement uses $1 / ${name} for groups.
func RegexRename(name, find, repl string) string {
	if find == "" {
		return name
	}
	re, err := regexp.Compile(find)
	if err != nil {
		return name
	}
	return re.ReplaceAllString(name, repl)
}

// RenameError explains why find is not a valid regular expression ("" when it is).
func RenameError(find string) string {
	if find == "" {
		return ""
	}
	if _, err := regexp.Compile(find); err != nil {
		return err.Error()
	}
	return ""
}

// Tokens fill a file name pattern.
type Tokens struct {
	SourcePath string
	PresetName string
	Index      int
	Now        time.Time
}

// ExpandPattern fills {name} {ext} {preset} {date} {time} {index} in one pass,
// so a token inside the inserted name stays as it is.
func ExpandPattern(pattern string, t Tokens) string {
	if strings.TrimSpace(pattern) == "" {
		pattern = "{name}"
	}
	return reToken.ReplaceAllStringFunc(pattern, func(m string) string {
		switch strings.ToLower(m[1 : len(m)-1]) {
		case "name":
			return formats.NameWithoutExtension(t.SourcePath)
		case "ext":
			return formats.ExtensionOf(t.SourcePath)
		case "preset":
			return t.PresetName
		case "date":
			return t.Now.Format("20060102")
		case "time":
			return t.Now.Format("150405")
		}
		return strconv.Itoa(t.Index)
	})
}

// Sanitize makes name a valid file name on every OS, or fallback when nothing is left.
func Sanitize(name, fallback string) string {
	var b strings.Builder
	for _, c := range name {
		if strings.ContainsRune(`<>:"/\|?*`, c) || unicode.IsControl(c) {
			b.WriteByte('_')
		} else {
			b.WriteRune(c)
		}
	}
	s := strings.TrimRight(strings.TrimSpace(b.String()), ".")
	if s == "" {
		return fallback
	}
	return s
}

// Exists reports whether path exists (the default for Resolve).
func Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// Resolve turns rule and a source file into the output path, or "" when the
// file is to be skipped (ConflictSkip and it exists). suffix is appended to the
// name ("-page1"). The source itself is never returned.
func Resolve(rule *OutputRule, t Tokens, targetExt string, exists func(string) bool, suffix string) string {
	if exists == nil {
		exists = Exists
	}
	t.SourcePath = strings.TrimRight(t.SourcePath, `/\`)
	if t.SourcePath == "" {
		t.SourcePath = "/"
	}
	var sourceDir string
	if strings.TrimSpace(rule.SourceFolder) == "" {
		abs, _ := filepath.Abs(t.SourcePath)
		sourceDir = filepath.Dir(abs)
	} else {
		sourceDir, _ = filepath.Abs(rule.SourceFolder)
	}
	dir := sourceDir
	switch {
	case rule.Location == LocationSubfolder:
		dir = filepath.Join(sourceDir, Sanitize(ExpandPattern(rule.SubfolderName, t), "converted"))
	case rule.Location == LocationCustomFolder && strings.TrimSpace(rule.CustomFolder) != "":
		dir = filepath.Join(rule.CustomFolder, subfolderBelow(rule.KeepFoldersFrom, sourceDir))
	}
	name := ExpandPattern(rule.FileNamePattern, t)
	for _, r := range rule.Renames {
		name = r.Apply(name)
	}
	name = Sanitize(name, formats.NameWithoutExtension(t.SourcePath)) + suffix
	ext := strings.TrimLeft(targetExt, ".")
	pathFor := func(base string) string {
		if ext == "" {
			return filepath.Join(dir, base)
		}
		return filepath.Join(dir, base+"."+ext)
	}
	candidate := pathFor(name)
	isSource := pathsEqual(candidate, t.SourcePath)
	if !isSource {
		switch {
		case !exists(candidate):
			return candidate
		case rule.ConfirmedPath != "" && pathsEqual(candidate, rule.ConfirmedPath):
			return candidate
		case rule.Conflict == ConflictOverwrite:
			return candidate
		case rule.Conflict == ConflictSkip:
			return ""
		}
	}
	for n := 2; ; n++ {
		candidate = pathFor(fmt.Sprintf("%s (%d)", name, n))
		if !exists(candidate) && !pathsEqual(candidate, t.SourcePath) {
			return candidate
		}
	}
}

func subfolderBelow(root, folder string) string {
	if strings.TrimSpace(root) == "" {
		return ""
	}
	abs, _ := filepath.Abs(root)
	rel, err := filepath.Rel(abs, folder)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return rel
}

// pathsEqual compares case-insensitively: a mount may be case-insensitive,
// and a numbered output is better than risking the source.
func pathsEqual(a, b string) bool {
	a, _ = filepath.Abs(a)
	b, _ = filepath.Abs(b)
	return strings.EqualFold(a, b)
}
