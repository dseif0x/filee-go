package web

import (
	"bytes"
	"strings"

	"github.com/dseif0x/filee-go/internal/formats"
)

// What a file from the web is: by its media type (what a server or a drag
// says) and by its first bytes (what it really is). Only formats whose
// signature is unambiguous are sniffed; a ZIP is not taken for a DOCX.

var byMime = map[string]string{
	"image/png": "png", "image/apng": "apng", "image/jpeg": "jpg", "image/jpg": "jpg", "image/pjpeg": "jpg",
	"image/gif": "gif", "image/webp": "webp", "image/bmp": "bmp", "image/x-ms-bmp": "bmp", "image/tiff": "tiff",
	"image/svg+xml": "svg", "image/avif": "avif", "image/heic": "heic", "image/heif": "heic", "image/jxl": "jxl",
	"image/x-icon": "ico", "image/vnd.microsoft.icon": "ico", "image/jp2": "jp2", "image/vnd.adobe.photoshop": "psd",
	"application/pdf": "pdf", "application/x-pdf": "pdf",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   "docx",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         "xlsx",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": "pptx",
	"application/msword": "doc", "application/vnd.ms-excel": "xls", "application/vnd.ms-powerpoint": "ppt",
	"application/vnd.oasis.opendocument.text": "odt", "application/vnd.oasis.opendocument.spreadsheet": "ods",
	"application/vnd.oasis.opendocument.presentation": "odp", "application/rtf": "rtf", "text/rtf": "rtf",
	"application/epub+zip": "epub", "application/x-hwp": "hwp", "application/haansofthwp": "hwp",
	"application/vnd.hancom.hwp": "hwp", "application/vnd.hancom.hwpx": "hwpx", "application/hwp+zip": "hwpx",
	"text/plain": "txt", "text/csv": "csv", "text/markdown": "md", "application/json": "json",
	"application/xml": "xml", "text/xml": "xml", "application/zip": "zip", "application/x-zip-compressed": "zip",
	"application/x-7z-compressed": "7z", "application/vnd.rar": "rar", "application/x-rar-compressed": "rar",
	"application/gzip": "gz", "audio/mpeg": "mp3", "audio/mp4": "m4a", "audio/wav": "wav", "audio/x-wav": "wav",
	"audio/ogg": "ogg", "audio/flac": "flac", "audio/webm": "weba", "video/mp4": "mp4", "video/webm": "webm",
	"video/quicktime": "mov", "video/x-matroska": "mkv", "video/x-msvideo": "avi",
	"font/ttf": "ttf", "font/otf": "otf", "font/woff": "woff", "font/woff2": "woff2",
}

// programMimes are what servers send for programs; refused like blocked extensions.
var programMimes = toSet(
	"application/x-msdownload", "application/x-msdos-program", "application/x-msi", "application/x-ms-installer",
	"application/vnd.microsoft.portable-executable", "application/x-dosexec", "application/x-executable",
	"application/x-sh", "application/x-shellscript", "application/x-bat", "application/java-archive",
	"application/x-java-archive", "application/vnd.android.package-archive", "application/x-apple-diskimage",
	"application/x-debian-package", "application/x-rpm", "application/x-mach-binary", "text/javascript",
	"application/javascript", "application/x-powershell")

// NormalizeMime is the media type without parameters, lower case, or "".
func NormalizeMime(m string) string {
	if i := strings.IndexByte(m, ';'); i >= 0 {
		m = m[:i]
	}
	return strings.ToLower(strings.TrimSpace(m))
}

// ExtensionForMime is the extension Filee writes for a media type it knows, or "".
func ExtensionForMime(m string) string { return byMime[NormalizeMime(m)] }

// IsPageMime reports a web page (HTML) media type.
func IsPageMime(m string) bool {
	n := NormalizeMime(m)
	return n == "text/html" || n == "application/xhtml+xml"
}

// IsImageMime reports an image media type.
func IsImageMime(m string) bool { return strings.HasPrefix(NormalizeMime(m), "image/") }

// IsProgramMime reports a program's media type.
func IsProgramMime(m string) bool { return programMimes[NormalizeMime(m)] }

// SniffLength is enough bytes for Sniff.
const SniffLength = 64

var (
	pngSig  = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	jpegSig = []byte{0xFF, 0xD8, 0xFF}
	tiffLE  = []byte{0x49, 0x49, 0x2A, 0x00}
	tiffBE  = []byte{0x4D, 0x4D, 0x00, 0x2A}
	icoSig  = []byte{0x00, 0x00, 0x01, 0x00}
	jxlCode = []byte{0xFF, 0x0A}
	jxlBox  = []byte{0x00, 0x00, 0x00, 0x0C, 0x4A, 0x58, 0x4C, 0x20, 0x0D, 0x0A, 0x87, 0x0A}

	programSigs = [][]byte{
		{0x4D, 0x5A},             // "MZ": Windows programs and libraries
		{0x7F, 0x45, 0x4C, 0x46}, // ELF
		{0x23, 0x21},             // "#!": scripts
		{0xCA, 0xFE, 0xBA, 0xBE}, // Mach-O universal (also Java classes)
		{0xCF, 0xFA, 0xED, 0xFE}, // Mach-O 64-bit
		{0xCE, 0xFA, 0xED, 0xFE}, // Mach-O 32-bit
	}
)

// Sniff is the extension of a format recognised by its first bytes, or "".
// Only formats whose signature can't be mistaken: images and PDF.
func Sniff(head []byte) string {
	switch {
	case bytes.HasPrefix(head, pngSig):
		return "png"
	case bytes.HasPrefix(head, jpegSig):
		return "jpg"
	case bytes.HasPrefix(head, []byte("GIF87a")), bytes.HasPrefix(head, []byte("GIF89a")):
		return "gif"
	case len(head) >= 12 && bytes.HasPrefix(head, []byte("RIFF")) && string(head[8:12]) == "WEBP":
		return "webp"
	case bytes.HasPrefix(head, []byte("%PDF-")):
		return "pdf"
	// BITMAPFILEHEADER: "BM", the size, then two reserved words that are always zero.
	case len(head) >= 14 && bytes.HasPrefix(head, []byte("BM")) && head[6] == 0 && head[7] == 0 && head[8] == 0 && head[9] == 0:
		return "bmp"
	case bytes.HasPrefix(head, tiffLE), bytes.HasPrefix(head, tiffBE):
		return "tiff"
	case bytes.HasPrefix(head, icoSig):
		return "ico"
	case bytes.HasPrefix(head, []byte("8BPS")):
		return "psd"
	case bytes.HasPrefix(head, jxlCode), bytes.HasPrefix(head, jxlBox):
		return "jxl"
	case len(head) >= 12 && string(head[4:8]) == "ftyp":
		// The major brand, then the compatible brands: an AVIF may name itself "mif1" first.
		brands := string(head[8:min(len(head), 40)])
		if strings.Contains(brands, "avif") || strings.Contains(brands, "avis") {
			return "avif"
		}
		switch brands[:4] {
		case "heic", "heix", "heim", "heis", "mif1", "msf1", "hevc", "hevx":
			return "heic"
		}
	}
	return ""
}

// LooksLikeProgram: the bytes start like a program or a script.
func LooksLikeProgram(head []byte) bool {
	for _, s := range programSigs {
		if bytes.HasPrefix(head, s) {
			return true
		}
	}
	return false
}

// LooksLikePage: the bytes start like an HTML page (a sign-in or error page instead of the file).
func LooksLikePage(head []byte) bool {
	t := strings.ToLower(strings.TrimLeft(string(head), "\ufeff \t\r\n"))
	return strings.HasPrefix(t, "<!doctype html") || strings.HasPrefix(t, "<html") || strings.HasPrefix(t, "<head")
}

var sniffable = toSet("png", "jpg", "gif", "webp", "pdf", "bmp", "tiff", "ico", "psd", "jxl", "avif", "heic")

// NameFor is the name a file should have once its first bytes are known: a
// sniffed image or PDF replaces an extension that says otherwise (a ".jpg"
// address that serves WebP), or is added to a name Filee can't place; a known
// extension whose format can't be sniffed (".docx" for a ZIP) stays.
func NameFor(name string, head []byte, mime string) string {
	sniffed := Sniff(head)
	current := formats.Detect(name)
	if sniffed != "" {
		real := formats.ByExtension(sniffed)
		// "a.apng" sniffs as PNG and "a.ai" as PDF: keep a name whose format is a kind of what was sniffed.
		if current == nil || (current != real && sniffable[current.ID]) {
			return WithExtension(name, sniffed)
		}
		return name
	}
	if current == nil {
		if e := ExtensionForMime(mime); e != "" {
			return WithExtension(name, e)
		}
	}
	return name
}
