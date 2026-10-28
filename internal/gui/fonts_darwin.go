//go:build darwin

package gui

import (
	"os"

	"github.com/dseif0x/filee-go/internal/donut"
)

// fallbackFonts are macOS's own fonts for the scripts the embedded Latin
// font lacks (Korean, Chinese, Japanese, Arabic, Thai, Devanagari), loaded
// in the background at start; missing ones are skipped.
var fallbackFonts = []string{
	"/System/Library/Fonts/AppleSDGothicNeo.ttc",
	"/System/Library/Fonts/Hiragino Sans GB.ttc",
	"/System/Library/Fonts/ヒラギノ角ゴシック W6.ttc",
	"/System/Library/Fonts/Supplemental/Arial Unicode.ttf",
	"/Library/Fonts/Arial Unicode.ttf",
	"/System/Library/Fonts/GeezaPro.ttc",
	"/System/Library/Fonts/Thonburi.ttc",
	"/System/Library/Fonts/Kohinoor.ttc",
}

func loadFallbackFonts() int {
	n := 0
	for _, p := range fallbackFonts {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if donut.DefaultFonts().AddFallback(b) == nil {
			n++
		}
	}
	return n
}
