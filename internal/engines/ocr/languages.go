package ocr

// The languages Tesseract reads (tessdata_fast 4.1.0, Apache-2.0), pinned in
// ocr-languages.json like the engines in engines.json. A language is
// downloaded the first time a preset needs it, checked against its SHA-256
// and kept in <engines>/ocr-languages/<code>/<code>.traineddata.

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"

	"github.com/dseif0x/filee-go/internal/engines/install"
	"github.com/dseif0x/filee-go/internal/tools"
)

//go:embed ocr-languages.json
var languagesJSON []byte

// DefaultLanguage is read when a preset doesn't say.
const DefaultLanguage = "eng"

const dataVersion = "4.1.0"

// Language is one language Tesseract reads, by its code (eng, chi_sim).
type Language struct {
	Code string
	// Culture names the language for the UI ("de", "zh-Hans").
	Culture string
	Size    int64
}

// NativeName is the language's own name ("Deutsch", "日本語").
func (l Language) NativeName() string {
	tag, err := language.Parse(l.Culture)
	if err != nil {
		return l.Code
	}
	n := display.Self.Name(tag)
	if n == "" {
		return l.Code
	}
	r, size := utf8.DecodeRuneInString(n)
	return string(unicode.ToUpper(r)) + n[size:]
}

var loadOnce = sync.OnceValues(func() ([]Language, map[string]install.Component) {
	var f struct {
		URL       string `json:"url"`
		Languages map[string]struct {
			Culture string `json:"culture"`
			Sha256  string `json:"sha256"`
			Size    int64  `json:"size"`
		} `json:"languages"`
	}
	if err := json.Unmarshal(languagesJSON, &f); err != nil {
		panic("ocr-languages.json: " + err.Error())
	}
	// The file's order: English first. JSON objects have none in Go, so read the keys in order.
	var order []string
	d := json.NewDecoder(strings.NewReader(string(languagesJSON)))
	depth := 0
	inLanguages := false
	for {
		t, err := d.Token()
		if err != nil {
			break
		}
		switch v := t.(type) {
		case json.Delim:
			if v == '{' || v == '[' {
				depth++
			} else {
				depth--
				if depth == 1 {
					inLanguages = false
				}
			}
		case string:
			if depth == 1 && v == "languages" {
				inLanguages = true
			} else if depth == 2 && inLanguages {
				order = append(order, v)
				// Skip the value object.
				var skip json.RawMessage
				if d.Decode(&skip) != nil {
					break
				}
			}
		}
	}
	var all []Language
	comps := map[string]install.Component{}
	for _, code := range order {
		e := f.Languages[code]
		all = append(all, Language{code, e.Culture, e.Size})
		comps[code] = install.Component{ID: code, Version: dataVersion, URL: strings.Replace(f.URL, "{0}", code, 1),
			Sha256: e.Sha256, Size: e.Size, Kind: "traineddata"}
	}
	return all, comps
})

// Languages are every language offered, English first.
func Languages() []Language { l, _ := loadOnce(); return l }

// LanguageComponents are the downloads, one component per language.
func LanguageComponents() map[string]install.Component { _, c := loadOnce(); return c }

// FindLanguage returns the language with code, or false.
func FindLanguage(code string) (Language, bool) {
	for _, l := range Languages() {
		if l.Code == code {
			return l, true
		}
	}
	return Language{}, false
}

// LanguagesRoot receives one folder per language.
func LanguagesRoot() string { return filepath.Join(tools.EnginesDir(), "ocr-languages") }

func languageInstaller() *install.Installer {
	return &install.Installer{Root: LanguagesRoot(), Components: LanguageComponents(), UserAgent: "Filee"}
}

func languagePackage(code string) install.Package {
	return install.Package{ID: "ocr-" + code, Components: []string{code}, InstalledSize: LanguageComponents()[code].Size, ConverterIDs: []string{"tesseract"}}
}

// LanguageFile is the language data, or "" while it isn't downloaded.
func LanguageFile(code string) string {
	f := filepath.Join(LanguagesRoot(), code, code+".traineddata")
	if _, err := os.Stat(f); err != nil {
		return ""
	}
	if _, ok := LanguageComponents()[code]; !ok || !languageInstaller().IsInstalled(languagePackage(code)) {
		return ""
	}
	return f
}

// IsLanguageInstalled: the language's data is here.
func IsLanguageInstalled(code string) bool { return LanguageFile(code) != "" }

var oneAtATime sync.Mutex

// EnsureLanguage downloads code if it isn't there yet; one download at a time.
func EnsureLanguage(ctx context.Context, code string) (string, error) {
	if _, ok := LanguageComponents()[code]; !ok {
		return "", fmt.Errorf("Tesseract has no language '%s' in Filee's list.", code)
	}
	oneAtATime.Lock()
	defer oneAtATime.Unlock()
	if f := LanguageFile(code); f != "" {
		return f, nil
	}
	if err := languageInstaller().Install(ctx, languagePackage(code), nil); err != nil {
		return "", err
	}
	if f := LanguageFile(code); f != "" {
		return f, nil
	}
	return "", fmt.Errorf("The %s language data is missing after its download.", code)
}
