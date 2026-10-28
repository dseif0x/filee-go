package cad

import (
	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc/dwg"
)

// Warnings and errors are passed on.
const dwgWarning = dwg.NotifyWarning

func readDwg(path string, warn func(string)) (*cadoc.Document, error) {
	return dwg.ReadFile(path, dwg.ReadConfig{Failsafe: true}, func(level int, msg string) {
		if level >= dwgWarning && warn != nil {
			warn(msg)
		}
	})
}

func writeDwg(doc *cadoc.Document, path string, warn func(string)) error {
	return dwg.WriteFile(path, doc, dwg.WriteConfig{}, func(level int, msg string) {
		if level >= dwgWarning && warn != nil {
			warn(msg)
		}
	})
}
