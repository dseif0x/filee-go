package htmldoc

import "github.com/dseif0x/filee-go/internal/doc"

// HTML is a built-in document reader (DOCX and HWPX writers read it directly).
func init() { doc.RegisterReader("html", Read) }
