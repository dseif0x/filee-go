package doc

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
)

// ReadFunc reads a file into the document model; media is a fresh folder for
// extracted pictures (created on demand).
type ReadFunc func(path, media string) (*Document, error)

// PdfPenalty is the extra cost of reading a PDF: its text is rebuilt from
// positioned words, so a route keeping the source's structure wins.
const PdfPenalty = 6

// PdfOcrLayer, when set, reads a PDF with its OCR layer as its text (the PDF
// engine sets it); the writers use it when the preset recognises text.
var PdfOcrLayer ReadFunc

var (
	readersMu sync.RWMutex
	readers   = map[string]ReadFunc{}
	order     []string
)

// RegisterReader adds the built-in reader of a format (from an init func).
// The DOCX and HWPX writers take their input formats from here.
func RegisterReader(format string, read ReadFunc) {
	readersMu.Lock()
	defer readersMu.Unlock()
	format = strings.ToLower(format)
	if _, ok := readers[format]; !ok {
		order = append(order, format)
	}
	readers[format] = read
}

// ReaderFormats lists the formats with a built-in reader, in registration order.
func ReaderFormats() []string {
	readersMu.RLock()
	defer readersMu.RUnlock()
	return slices.Clone(order)
}

// HasReader reports whether format has a built-in reader.
func HasReader(format string) bool {
	readersMu.RLock()
	defer readersMu.RUnlock()
	_, ok := readers[strings.ToLower(format)]
	return ok
}

// Read reads path of format into the document model; media is the folder
// for extracted pictures.
func Read(path, format, media string) (*Document, error) {
	readersMu.RLock()
	read := readers[strings.ToLower(format)]
	readersMu.RUnlock()
	if read == nil {
		return nil, fmt.Errorf("there is no document reader for %s files", strings.ToUpper(format))
	}
	return read(path, media)
}

func init() {
	RegisterReader("txt", func(path, _ string) (*Document, error) {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return TextDocument(b), nil
	})
}

// TextDocument is plain text: one paragraph per line, text kept verbatim.
func TextDocument(b []byte) *Document {
	s := NewSection()
	for _, line := range strings.Split(strings.ReplaceAll(DecodeText(b), "\r\n", "\n"), "\n") {
		p := &Paragraph{}
		if line != "" {
			p.Inlines = append(p.Inlines, &Text{Text: line})
		}
		s.Blocks = append(s.Blocks, p)
	}
	return &Document{Sections: []*Section{s}}
}
