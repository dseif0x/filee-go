// XML helpers of the PPTX and spreadsheet writers.

package pdf

import "strings"

const (
	xmlDeclaration = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`
	relBase        = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
)

func xmlEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '"':
			b.WriteString("&quot;")
		case r < 0x20 && r != '\t' && r != '\n' && r != '\r', r == 0xFFFE, r == 0xFFFF:
			// not allowed in XML
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
