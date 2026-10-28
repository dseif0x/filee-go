//go:build !windows

package subtitles

import (
	"os"
)

// systemCodePage is the Windows ANSI code page .NET reports for the locale
// (LC_ALL, LC_CTYPE or LANG), as CultureInfo.TextInfo.ANSICodePage does.
func systemCodePage() int {
	locale := ""
	for _, v := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if s := os.Getenv(v); s != "" {
			locale = s
			break
		}
	}
	return codePageOfLocale(locale)
}
