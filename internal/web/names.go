// Package web brings files from the web into conversions: files dragged out
// of a browser (their bytes, or an address Filee downloads itself), named
// safely, recognised by their first bytes, and saved when their job runs.
package web

import (
	"errors"
	"fmt"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/presets"
)

// MaxNameLength is the longest name Filee gives a downloaded file, extension included.
const MaxNameLength = 120

// blocked are programs, installers and scripts: Filee converts none of them,
// and a drag or a server that offers one is refused rather than saved.
var blocked = toSet("exe", "msi", "msix", "msixbundle", "appx", "appxbundle", "msp", "com", "scr", "pif", "cpl", "dll", "sys",
	"bat", "cmd", "ps1", "psm1", "psd1", "vbs", "vbe", "js", "jse", "mjs", "wsf", "wsh", "hta", "reg", "inf",
	"lnk", "url", "scf", "application", "appref-ms", "gadget", "msc",
	"sh", "bash", "zsh", "csh", "ksh", "command", "tool", "run", "bin", "appimage", "desktop",
	"app", "dmg", "pkg", "mpkg", "kext", "workflow", "scpt", "applescript",
	"jar", "jnlp", "apk", "xapk", "ipa", "deb", "rpm", "snap", "flatpak", "py", "pyw", "pl", "rb")

// pageExtensions: a link to one is a page to read, not a file to convert.
var pageExtensions = toSet("htm", "html", "xhtml", "shtml", "php", "php3", "php4", "php5", "phtml", "asp", "aspx", "jsp", "jspx", "cgi",
	"cfm", "do", "action")

// reservedStems are names Windows keeps for devices.
var reservedStems = toSet("con", "prn", "aux", "nul", "conin$", "conout$",
	"com0", "com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9",
	"lpt0", "lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9")

func toSet(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, i := range items {
		m[i] = true
	}
	return m
}

func lastExtension(name string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(strings.TrimRight(name, " .")), "."))
}

// IsBlocked reports a program, installer or script (by extension).
func IsBlocked(name string) bool { return blocked[lastExtension(name)] }

// IsPageName reports the extension of a web page (.html, .php, …).
func IsPageName(name string) bool { return pageExtensions[lastExtension(name)] }

// SanitizeName makes a plain file name of name: only its last path part,
// without characters any OS refuses, not a device name, at most
// MaxNameLength characters (the extension kept), never empty (fallback).
func SanitizeName(name, fallback string) string {
	if fallback == "" {
		fallback = "download"
	}
	// Only the last part: "../../x.png", "C:\x.png" and "a/b.png" all become a name in the target folder.
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Trim(presets.Sanitize(name, ""), " .")
	if name == "" {
		name = fallback
	}
	ext := filepath.Ext(name)
	if utf8.RuneCountInString(ext) > 16 {
		ext = ""
	}
	stem := strings.TrimSuffix(name, ext)
	if reservedStems[strings.ToLower(strings.TrimRight(stem, " ."))] {
		stem = "_" + stem
	}
	if r := []rune(stem); len(r)+utf8.RuneCountInString(ext) > MaxNameLength {
		stem = strings.TrimRight(string(r[:max(0, MaxNameLength-utf8.RuneCountInString(ext))]), " .")
	}
	if stem == "" {
		stem = fallback
	}
	return stem + ext
}

// FromContentDisposition is the file name a Content-Disposition header gives
// (RFC 6266, filename* first), or "".
func FromContentDisposition(header string) string {
	if strings.TrimSpace(header) == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(header)
	if err != nil {
		return ""
	}
	// mime decodes filename* (RFC 2231) into "filename".
	return strings.Trim(strings.TrimSpace(params["filename"]), `"`)
}

// FromURL is the last part of the address's path, unescaped, or "".
func FromURL(u *url.URL) string {
	if u == nil || !u.IsAbs() {
		return ""
	}
	p := u.EscapedPath()
	last := p[strings.LastIndex(p, "/")+1:]
	if last == "" {
		return ""
	}
	if un, err := url.PathUnescape(last); err == nil {
		last = un
	}
	return strings.TrimSpace(last)
}

// WithExtension is name with ext (no dot) when it doesn't already have an
// extension of the same format: "photo" → "photo.jpg", "photo.jpeg" stays,
// "photo.png" with "webp" → "photo.webp".
func WithExtension(name, ext string) string {
	current := formats.ExtensionOf(name)
	wanted := formats.ByExtension(ext)
	if current != "" && wanted != nil && formats.ByExtension(current) == wanted {
		return name
	}
	stem := name
	if current != "" && formats.ByExtension(current) != nil {
		stem = formats.NameWithoutExtension(name)
	}
	return SanitizeName(stem+"."+ext, "")
}

func numbered(name string, n int) string {
	if n == 1 {
		return name
	}
	ext := formats.ExtensionOf(name)
	if ext == "" {
		return fmt.Sprintf("%s (%d)", name, n)
	}
	return fmt.Sprintf("%s (%d).%s", name[:len(name)-len(ext)-1], n, ext)
}

// CreateUnique creates name in folder, or "name (2).ext" … when that is taken,
// and returns the open file. It never replaces an existing file.
func CreateUnique(folder, name string) (*os.File, string, error) {
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return nil, "", err
	}
	for n := 1; n < 10_000; n++ {
		path := filepath.Join(folder, numbered(name, n))
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			return f, path, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, "", err
		}
	}
	return nil, "", fmt.Errorf("no free name for %s", name)
}

// MoveUnique moves source into folder as name, numbered when taken, and
// returns where it went. It never replaces an existing file.
func MoveUnique(source, folder, name string) (string, error) {
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", err
	}
	for n := 1; n < 10_000; n++ {
		path := filepath.Join(folder, numbered(name, n))
		if _, err := os.Lstat(path); err == nil {
			continue
		}
		if err := os.Rename(source, path); err != nil {
			return "", err
		}
		return path, nil
	}
	return "", fmt.Errorf("no free name for %s", name)
}

// ---- web browsers ----

// browsers by process name (Windows without ".exe", the Linux executable, the
// macOS app name). A drag gesture over one opens the donut even when gestures
// are limited to the file manager.
var browsers = []string{
	"chrome", "msedge", "firefox", "brave", "opera", "vivaldi", "whale", "arc", "zen", "librewolf", "waterfox",
	"floorp", "chromium", "yandex",
	"firefox-esr", "firefox-bin", "google-chrome", "chromium-browser", "microsoft-edge", "brave-browser",
	"vivaldi-bin", "naver-whale",
	"Safari", "Google Chrome", "Microsoft Edge", "Brave Browser", "Naver Whale", "Chromium", "Zen Browser",
	"Firefox Developer Edition", "Firefox Nightly", "Google Chrome Canary", "Microsoft Edge Dev", "Orion",
}

// IsBrowser reports a web browser's process, by the name the platform reports.
func IsBrowser(process string) bool {
	if process == "" {
		return false
	}
	if len(process) > 4 && strings.EqualFold(process[len(process)-4:], ".exe") {
		process = process[:len(process)-4]
	}
	return slices.ContainsFunc(browsers, func(b string) bool { return strings.EqualFold(b, process) })
}
