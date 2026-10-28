// Package install downloads, verifies and unpacks the optional engines
// (FFmpeg, Pandoc, Ghostscript, Tesseract, LibreOffice, calibre, …) into
// Filee's engines folder (tools.EnginesDir), from the pinned list in
// engines.json: URL, SHA-256 and size per component and platform.
//
// macOS has downloads for rhwp, 7-Zip, Pandoc, LibreOffice and calibre; for
// FFmpeg, Ghostscript and Tesseract there is none (as in the original):
// Homebrew is the install route (UnavailableReason says so), and
// internal/tools finds Homebrew's copies.
package install

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"runtime"
	"strconv"
	"strings"
)

//go:embed engines.json
var enginesJSON []byte

// Component is one download from engines.json. Kind is the archive format:
// zip, tar.gz, tar.xz, dmg, msi, oxt, conda, nsis or traineddata.
type Component struct {
	ID, Version, URL, Sha256 string
	Size                     int64
	Kind                     string
	// MinimumMacOS is the oldest macOS a download runs on ("" for any).
	MinimumMacOS string
}

// Package is a set of components installed together.
type Package struct {
	ID string
	// Components are installed in this order (an extension needs its program first).
	Components []string
	// InstalledSize is the approximate size on disk, shown before installing.
	InstalledSize int64
	// ConverterIDs are the engines the package provides.
	ConverterIDs []string
}

// RuntimeIdentifier is the OS and architecture as engines.json names them (osx-arm64, win-x64, …).
func RuntimeIdentifier() string { return runtimeID(runtime.GOOS, runtime.GOARCH) }

func runtimeID(goos, goarch string) string {
	os := map[string]string{"windows": "win", "darwin": "osx", "linux": "linux"}[goos]
	if os == "" {
		os = "unknown"
	}
	arch := map[string]string{"amd64": "x64", "arm64": "arm64", "386": "x86", "arm": "arm"}[goarch]
	if arch == "" {
		arch = goarch
	}
	return os + "-" + arch
}

// Packages are offered in Settings › Engines; rhwp and 7-Zip are small and bundled.
func Packages() []Package {
	win := runtime.GOOS == "windows"
	lo := []string{"libreoffice"}
	gs := []string{"ghostscript"}
	if win {
		lo = []string{"libreoffice", "h2orestart", "jre"}
		// Ghostscript from conda-forge plus the C++ runtime it was built against.
		gs = []string{"ghostscript", "vcruntime"}
	}
	return []Package{
		{"libreoffice", lo, 1_265_000_000, []string{"libreoffice"}},
		{"pandoc", []string{"pandoc"}, 236_000_000, []string{"pandoc"}},
		{"ghostscript", gs, 31_000_000, []string{"ghostscript"}},
		{"ffmpeg", []string{"ffmpeg"}, 272_000_000, []string{"ffmpeg"}},
		{"calibre", []string{"calibre"}, 663_000_000, []string{"calibre"}},
		{"tesseract", []string{"tesseract"}, 168_000_000, []string{"tesseract"}},
	}
}

// PackageByID returns the package id, or nil.
func PackageByID(id string) *Package {
	for _, p := range Packages() {
		if p.ID == id {
			return &p
		}
	}
	return nil
}

// Programs are the executables each package must have (without .exe).
var Programs = map[string][]string{
	"libreoffice": {"soffice"},
	"pandoc":      {"pandoc"},
	"ghostscript": {"gs"},
	"ffmpeg":      {"ffmpeg", "ffprobe"},
	"calibre":     {"ebook-convert"},
	"rhwp":        {"rhwp"},
	"7zip":        {"7zz"},
	"tesseract":   {"tesseract"},
}

// ProgramsOn are the program names of package id on goos.
func ProgramsOn(id, goos string) []string {
	p := Programs[id]
	if goos != "windows" {
		return p
	}
	out := make([]string, len(p))
	for i, n := range p {
		switch n {
		case "gs":
			n = "gswin64c"
		case "7zz":
			n = "7z"
		}
		out[i] = n + ".exe"
	}
	return out
}

type componentJSON struct {
	Version      string `json:"version"`
	URL          string `json:"url"`
	Sha256       string `json:"sha256"`
	Size         int64  `json:"size"`
	Kind         string `json:"kind"`
	MinimumMacOS string `json:"minimumMacOS"`
}

type enginesFile struct {
	Components map[string]componentJSON `json:"components"`
	Platforms  map[string]struct {
		Components map[string]componentJSON `json:"components"`
	} `json:"platforms"`
}

// Components are the downloads for this computer; on a Mac older than a
// download's minimumMacOS that download is left out.
func Components() map[string]Component {
	return ComponentsFor(RuntimeIdentifier(), macOSVersion())
}

// ComponentsFor are the downloads of a runtime identifier; macOS "" keeps all.
func ComponentsFor(rid, macOS string) map[string]Component {
	var f enginesFile
	if err := json.Unmarshal(enginesJSON, &f); err != nil {
		panic("engines.json: " + err.Error())
	}
	src := f.Components
	if rid != "win-x64" {
		p, ok := f.Platforms[rid]
		if !ok {
			return map[string]Component{}
		}
		src = p.Components
	}
	out := map[string]Component{}
	for id, c := range src {
		if macOS != "" && c.MinimumMacOS != "" && compareVersions(macOS, c.MinimumMacOS) < 0 {
			continue
		}
		out[id] = Component{id, c.Version, c.URL, c.Sha256, c.Size, c.Kind, c.MinimumMacOS}
	}
	return out
}

// Version is the version engines.json pins for a component ("" when none).
func Version(id string) string { return Components()[id].Version }

func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < max(len(pa), len(pb)); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// DownloadSize is the total download size of a package in bytes.
func DownloadSize(p Package, components map[string]Component) int64 {
	var n int64
	for _, c := range p.Components {
		n += components[c].Size
	}
	return n
}

// CanDownload: every component of the package has a download here.
func CanDownload(p Package, components map[string]Component) bool {
	if len(p.Components) == 0 {
		return false
	}
	for _, c := range p.Components {
		if _, ok := components[c]; !ok {
			return false
		}
	}
	return true
}

// SuggestLimit: packages smaller than this are ticked by default.
const SuggestLimit = 60_000_000

// IsSuggested: small downloads are suggested; Ghostscript and Tesseract are
// small but only needed for PostScript and scans.
func IsSuggested(p Package, components map[string]Component) bool {
	return CanDownload(p, components) && DownloadSize(p, components) < SuggestLimit && p.ID != "ghostscript" && p.ID != "tesseract"
}

// UnavailableReason says how to get a package Filee has no download of here ("" when it has one).
func UnavailableReason(p Package, components map[string]Component, goos string) string {
	if CanDownload(p, components) {
		return ""
	}
	rid := runtimeID(goos, runtime.GOARCH)
	switch goos {
	case "darwin":
		switch p.ID {
		case "ffmpeg":
			return "Install FFmpeg with Homebrew (brew install ffmpeg), then check again or select its bin folder."
		case "ghostscript":
			return "Install Ghostscript with Homebrew (brew install ghostscript), then check again or select its bin folder."
		case "tesseract":
			return "Install Tesseract with Homebrew (brew install tesseract), then check again or select its bin folder."
		case "calibre":
			return "The calibre Filee downloads needs macOS 14 or later. Install calibre 7.26 from calibre-ebook.com and select its .app folder."
		}
		return fmt.Sprintf("No Filee download is available for %s. Install %s for this Mac and select its program or .app folder.", rid, p.ID)
	case "linux":
		switch p.ID {
		case "libreoffice":
			return "Install LibreOffice using your distribution's package manager (Ubuntu/Debian: sudo apt install libreoffice), then check again. HWP import through LibreOffice also needs Java and the H2Orestart extension."
		case "ghostscript":
			return "Install Ghostscript using your distribution's package manager (Ubuntu/Debian: sudo apt install ghostscript), then check again."
		case "tesseract":
			return "Install Tesseract using your distribution's package manager (Ubuntu/Debian: sudo apt install tesseract-ocr), then check again."
		}
		return fmt.Sprintf("No Filee download is available for %s. Install %s using your distribution's package manager and select its program folder.", rid, p.ID)
	}
	return fmt.Sprintf("No Filee download is available for %s. Select a compatible installed copy.", rid)
}
