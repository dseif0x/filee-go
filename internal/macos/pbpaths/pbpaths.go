// Package pbpaths reads file paths in the forms other Mac apps put them on a
// pasteboard. Finder writes one file URL per item (public.file-url); other
// file managers (QSpace Pro) write the older NSFilenamesPboardType, a property
// list array of paths, and some only plain text. Pure parsing, tested
// anywhere; the GUI reads the pasteboards themselves.
package pbpaths

import (
	"bytes"
	"encoding/xml"
	"net/url"
	"os"
	"strings"

	"github.com/dseif0x/filee-go/internal/platform"
)

const (
	// FilenamesType is the pasteboard type of an array of paths, as older apps write it.
	FilenamesType = "NSFilenamesPboardType"
	// FilenamesUTI is the same type as a pasteboard item lists it (AppKit's dynamic UTI).
	FilenamesUTI = "dyn.ah62d4rv4gu8y6y4grf0gn5xbrzw1gydcr7u1e3cytf2gn"
	FileURLType  = "public.file-url"
	TextType     = "public.utf8-plain-text"
)

// DragTypes are the types the donut registers for, so a drag offering only
// these still reaches it. File promises can't be received; a browser drag that
// offers one also offers the address (public.url) and HTML, read instead.
var DragTypes = []string{
	FileURLType, FilenamesType,
	"com.apple.NSFilePromiseItemMetaData", "com.apple.pasteboard.promised-file-url", "Apple files promise pasteboard type",
	"public.url", "public.html", "public.png", "public.jpeg", "public.tiff", "com.compuserve.gif", "org.webmproject.webp",
}

// IsFilenamesType: type names NSFilenamesPboardType, by its name or its dynamic UTI.
func IsFilenamesType(t string) bool { return t == FilenamesType || t == FilenamesUTI }

// FromPropertyList is the paths in an NSFilenamesPboardType property list (XML).
func FromPropertyList(data []byte) []string {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	var out []string
	depth := 0 // inside <array>
	inString := false
	var text strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "array":
				depth++
			case "string":
				if depth == 1 {
					inString = true
					text.Reset()
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "array":
				depth--
			case "string":
				if inString {
					if s := text.String(); isAbsolute(s) {
						out = append(out, s)
					}
					inString = false
				}
			}
		case xml.CharData:
			if inString {
				text.Write(t)
			}
		}
	}
	return out
}

// FromFileURL is the path of a file URL ("file:///Users/a/b%20c.png"), or "".
func FromFileURL(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 5 || !strings.EqualFold(s[:5], "file:") {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || !strings.EqualFold(u.Scheme, "file") || (u.Host != "" && !strings.EqualFold(u.Host, "localhost")) {
		return ""
	}
	p := u.Path
	// A file reference URL (/.file/id=…) names no path; AppKit resolves those itself.
	if !isAbsolute(p) || strings.HasPrefix(p, "/.file/id=") {
		return ""
	}
	return p
}

// FromText is the lines of plain text that are absolute paths or file URLs.
func FromText(text string) []string {
	var out []string
	for _, line := range strings.FieldsFunc(text, func(r rune) bool { return r == '\r' || r == '\n' }) {
		line = strings.TrimSpace(line)
		if len(line) >= 5 && strings.EqualFold(line[:5], "file:") {
			line = FromFileURL(line)
		}
		if isAbsolute(line) {
			out = append(out, line)
		}
	}
	return out
}

// FirstExisting is the paths of the first source, in order of preference,
// that names any path that exists: only those, without repeats. Later
// sources aren't read once one has given paths.
func FirstExisting(sources []func() []string, exists func(string) bool) []string {
	if exists == nil {
		exists = Exists
	}
	for _, src := range sources {
		var out []string
		for _, p := range src() {
			if !exists(p) {
				continue
			}
			dup := false
			for _, o := range out {
				if platform.SamePath(o, p) {
					dup = true
				}
			}
			if !dup {
				out = append(out, p)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

// Exists: a file or folder is at p.
func Exists(p string) bool { _, err := os.Stat(p); return err == nil }

func isAbsolute(p string) bool { return strings.HasPrefix(p, "/") && len(p) > 1 }
