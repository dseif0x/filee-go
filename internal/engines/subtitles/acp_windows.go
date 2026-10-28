package subtitles

import "golang.org/x/sys/windows"

func systemCodePage() int { return int(windows.GetACP()) }
