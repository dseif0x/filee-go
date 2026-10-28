package libreoffice

import (
	"archive/zip"
	"encoding/binary"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode/utf16"
)

// Font preflight for ZIP-based office documents: renderers otherwise
// substitute absent families silently, which changes wrapping, pagination
// and table geometry. The scan is read-only and bounded; a malformed part
// never prevents conversion.

const (
	maxFontPartBytes  = 16 << 20
	maxFontTotalBytes = 64 << 20
)

var (
	genericFonts = []string{"serif", "sans-serif", "sans serif", "monospace", "cursive", "fantasy", "system-ui", "emoji", "math"}
	packageExts  = []string{".docx", ".docm", ".dotx", ".dotm", ".xlsx", ".xlsm", ".xltx", ".xltm", ".pptx", ".pptm", ".potx", ".potm", ".ppsx", ".ppsm", ".hwpx", ".zip"}
	// symbolFonts are glyph sets for bullets, not document typefaces.
	symbolFonts = []string{"symbol", "wingdings", "wingdings 2", "wingdings 3", "webdings", "opensymbol"}
	// metricAliases preserve the original family's metrics closely enough not to warn.
	metricAliases = map[string][]string{
		"arial": {"Liberation Sans"}, "calibri": {"Carlito"}, "calibri light": {"Carlito"}, "cambria": {"Caladea"},
		"courier new": {"Liberation Mono"}, "times new roman": {"Liberation Serif"},
	}
)

// MissingFonts lists the fonts a document names that are not installed (sorted).
func MissingFonts(path string) []string { return missingFonts(path, Installed) }

func missingFonts(path string, installed func(string) bool) (out []string) {
	defer func() {
		if recover() != nil {
			out = nil
		}
	}()
	ext := strings.ToLower(filepath.Ext(path))
	if !contains(packageExts, ext) {
		return nil
	}
	for _, face := range ReferencedFonts(path) {
		if installed(face) {
			continue
		}
		aliased := false
		for _, a := range metricAliases[strings.ToLower(face)] {
			if installed(a) {
				aliased = true
			}
		}
		if !aliased {
			out = append(out, face)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ReferencedFonts are the font families a document's parts name, without embedded ones.
func ReferencedFonts(path string) []string {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil
	}
	defer z.Close()
	embedded := embeddedFonts(z)
	result := map[string]string{}
	add := func(face string) {
		face = strings.Trim(strings.TrimSpace(face), `'"`)
		l := strings.ToLower(face)
		if face == "" || strings.HasPrefix(face, "+") || contains(genericFonts, l) || contains(symbolFonts, l) {
			return
		}
		if _, ok := result[l]; !ok {
			result[l] = face
		}
	}
	var total int64
	for _, e := range z.File {
		if !isDocumentXML(e) || int64(e.UncompressedSize64) > maxFontPartBytes {
			continue
		}
		if total += int64(e.UncompressedSize64); total > maxFontTotalBytes {
			continue
		}
		rc, err := e.Open()
		if err != nil {
			continue
		}
		d := xml.NewDecoder(io.LimitReader(rc, maxFontPartBytes))
		var stack []string
		for {
			tok, err := d.Token()
			if err != nil {
				break
			}
			switch t := tok.(type) {
			case xml.StartElement:
				parent := ""
				if len(stack) > 0 {
					parent = stack[len(stack)-1]
				}
				a := func(n string) string {
					for _, x := range t.Attr {
						if x.Name.Local == n {
							return x.Value
						}
					}
					return ""
				}
				switch l := t.Name.Local; {
				case l == "rFonts":
					// WordprocessingML run fonts.
					for _, n := range []string{"ascii", "hAnsi", "eastAsia", "cs"} {
						add(a(n))
					}
				case l == "latin" || l == "ea" || l == "cs":
					// DrawingML fonts (PowerPoint, drawings and charts).
					add(a("typeface"))
				case l == "name" && parent == "font":
					// SpreadsheetML: <font><name val="Aptos"/></font>.
					add(a("val"))
				case l == "font":
					// HWPX header fonts: <hh:font face="맑은 고딕"/>.
					add(a("face"))
				}
				stack = append(stack, t.Name.Local)
			case xml.EndElement:
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
			case xml.Directive:
				rc.Close()
				return nil // DTDs are not processed
			}
		}
		rc.Close()
	}
	var out []string
	for l, face := range result {
		if !embedded[l] {
			out = append(out, face)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

func embeddedFonts(z *zip.ReadCloser) map[string]bool {
	out := map[string]bool{}
	for _, e := range z.File {
		if e.Name != "word/fontTable.xml" || e.UncompressedSize64 > maxFontPartBytes {
			continue
		}
		rc, err := e.Open()
		if err != nil {
			return out
		}
		defer rc.Close()
		d := xml.NewDecoder(rc)
		face, depth, fontDepth := "", 0, -1
		for {
			tok, err := d.Token()
			if err != nil {
				break
			}
			switch t := tok.(type) {
			case xml.StartElement:
				depth++
				if t.Name.Local == "font" {
					face, fontDepth = "", depth
					for _, a := range t.Attr {
						if a.Name.Local == "name" {
							face = a.Value
						}
					}
				} else if fontDepth >= 0 && depth > fontDepth && strings.HasPrefix(strings.ToLower(t.Name.Local), "embed") && face != "" {
					out[strings.ToLower(strings.TrimSpace(face))] = true
				}
			case xml.EndElement:
				if t.Name.Local == "font" && depth == fontDepth {
					face, fontDepth = "", -1
				}
				depth--
			}
		}
	}
	return out
}

func isDocumentXML(e *zip.File) bool {
	p := e.Name
	lp := strings.ToLower(p)
	if e.UncompressedSize64 == 0 || !strings.HasSuffix(lp, ".xml") {
		return false
	}
	if strings.HasPrefix(lp, "word/") {
		n := lp[5:]
		return n == "document.xml" || n == "styles.xml" || n == "numbering.xml" || n == "footnotes.xml" || n == "endnotes.xml" ||
			strings.HasPrefix(n, "header") || strings.HasPrefix(n, "footer") || strings.HasPrefix(n, "theme/")
	}
	return lp == "xl/styles.xml" || lp == "contents/header.xml" || strings.HasPrefix(lp, "ppt/slides/slide") ||
		strings.HasPrefix(lp, "ppt/slidelayouts/slidelayout") || strings.HasPrefix(lp, "ppt/slidemasters/slidemaster") || strings.HasPrefix(lp, "ppt/theme/")
}

// ---- installed fonts ----

var (
	fontsMu      sync.Mutex
	fontFamilies map[string]bool
)

// ForgetFonts drops the font index (after the user installed a font).
func ForgetFonts() {
	fontsMu.Lock()
	fontFamilies = nil
	fontsMu.Unlock()
}

// Installed reports whether a font family is installed (by its family names, any language).
func Installed(face string) bool {
	fontsMu.Lock()
	defer fontsMu.Unlock()
	if fontFamilies == nil {
		fontFamilies = scanFonts()
	}
	return fontFamilies[strings.ToLower(strings.TrimSpace(face))]
}

// FontDirs are the font folders of the system and the user.
func FontDirs() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		dirs := []string{filepath.Join(os.Getenv("WINDIR"), "Fonts")}
		if l := os.Getenv("LOCALAPPDATA"); l != "" {
			dirs = append(dirs, filepath.Join(l, "Microsoft", "Windows", "Fonts"))
		}
		return dirs
	case "darwin":
		return []string{"/System/Library/Fonts", "/Library/Fonts", filepath.Join(home, "Library", "Fonts")}
	}
	return []string{"/usr/share/fonts", "/usr/local/share/fonts", filepath.Join(home, ".fonts"), filepath.Join(home, ".local", "share", "fonts")}
}

func scanFonts() map[string]bool {
	out := map[string]bool{}
	for _, dir := range FontDirs() {
		filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			switch strings.ToLower(filepath.Ext(p)) {
			case ".ttf", ".otf", ".ttc", ".otc":
			default:
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			for _, n := range FamilyNames(b) {
				out[strings.ToLower(n)] = true
			}
			return nil
		})
	}
	return out
}

// FamilyNames are the family, typographic family and full names of a font
// file (TTF, OTF or a collection) in every language its name table holds.
func FamilyNames(b []byte) []string {
	be := binary.BigEndian
	var offsets []uint32
	if len(b) >= 12 && string(b[:4]) == "ttcf" {
		n := int(be.Uint32(b[8:]))
		for i := 0; i < n && i < 256 && 16+4*i <= len(b); i++ {
			offsets = append(offsets, be.Uint32(b[12+4*i:]))
		}
	} else {
		offsets = []uint32{0}
	}
	seen := map[string]bool{}
	var out []string
	for _, off := range offsets {
		o := int(off)
		if o+12 > len(b) {
			continue
		}
		tables := int(be.Uint16(b[o+4:]))
		for t := 0; t < tables && o+12+16*t+16 <= len(b); t++ {
			rec := b[o+12+16*t:]
			if string(rec[:4]) != "name" {
				continue
			}
			start, length := int(be.Uint32(rec[8:])), int(be.Uint32(rec[12:]))
			if start < 0 || length < 6 || start+length > len(b) {
				break
			}
			tbl := b[start : start+length]
			count, strOff := int(be.Uint16(tbl[2:])), int(be.Uint16(tbl[4:]))
			for i := 0; i < count && 6+12*i+12 <= len(tbl); i++ {
				r := tbl[6+12*i:]
				platform, encoding, id := be.Uint16(r), be.Uint16(r[2:]), be.Uint16(r[6:])
				if id != 1 && id != 4 && id != 16 {
					continue
				}
				l, so := int(be.Uint16(r[8:])), strOff+int(be.Uint16(r[10:]))
				if so+l > len(tbl) {
					continue
				}
				raw := tbl[so : so+l]
				var name string
				switch {
				case platform == 0 || platform == 3 && (encoding == 1 || encoding == 10 || encoding == 0):
					u := make([]uint16, len(raw)/2)
					for k := range u {
						u[k] = be.Uint16(raw[2*k:])
					}
					name = string(utf16.Decode(u))
				case platform == 1 && encoding == 0:
					name = string(raw)
				default:
					continue
				}
				name = strings.TrimSpace(name)
				if name != "" && !seen[name] {
					seen[name] = true
					out = append(out, name)
				}
			}
			break
		}
	}
	return out
}
