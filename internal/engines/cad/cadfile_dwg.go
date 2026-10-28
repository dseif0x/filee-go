package cad

import (
	"errors"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// Placeholders until the DWG reader and writer land.
func readDwg(path string, warn func(string)) (*cadoc.Document, error) {
	return nil, errors.New("DWG reading is not available yet")
}

func writeDwg(doc *cadoc.Document, path string, warn func(string)) error {
	return errors.New("DWG writing is not available yet")
}
