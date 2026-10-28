package main

import (
	"fmt"
	"strings"

	"github.com/dseif0x/filee-go/internal/watch"
)

func helpText(command string) string {
	switch strings.ToLower(command) {
	case "convert":
		return helpConvert
	case "watch":
		return fmt.Sprintf(helpWatch, watch.DefaultSettleTime.Seconds())
	case "formats":
		return "Usage: filee formats [<format>]\n\nLists every format, or what one format converts to.\n"
	case "presets":
		return "Usage: filee presets\n\nLists the presets saved in Filee (built-in and your own).\n"
	case "engines":
		return "Usage: filee engines\n\nLists the conversion engines in priority order and whether each can run here.\n"
	case "app":
		return "Usage: filee app\n\nStarts the Filee desktop app.\n"
	}
	return helpGeneral
}

const helpGeneral = `Filee: convert files offline from the command line.

Usage:
  filee convert <files or folders...> --to <format> [options]
  filee convert <files or folders...> --preset <preset> [options]
  filee watch <folder> --to <format> [options]
  filee formats [<format>]
  filee presets
  filee engines
  filee app
  filee help [<command>]
  filee version

Examples:
  filee convert photo.heic --to jpg
  filee convert *.png --to webp --quality 80 -o converted
  filee convert ~/Scans --recursive --preset to-pdf
  filee watch ~/Inbox --to pdf --move-originals

Presets and engines are the ones in the Filee app. Exit codes: 0 all converted, 1 some failed,
2 wrong arguments or a missing input, 3 nothing to convert.

`

const helpConvert = `Usage: filee convert <files or folders...> (--to <format> | --preset <preset>) [options]

Files can be wildcards (*.heic). Folders convert the files in them that Filee knows.

Options:
  -t, --to <format>      Target format, e.g. jpg, png, pdf, docx, mp4 (see 'filee formats')
  -p, --preset <preset>  A preset from the app, by id or name (see 'filee presets')
  -o, --output <folder>  Save there instead of next to the sources
  -r, --recursive        Include sub folders
  -q, --quality <1-100>  Image quality for JPG, WebP, AVIF, …
      --overwrite        Replace existing output files (default: add a number)
      --skip             Leave existing output files alone
      --keep-dates       Give converted files the created and modified dates of the originals
      --zip              Save the result of each file as one ZIP (a Shapefile's files together)
      --json             Print the result as JSON (for scripts and other programs)
      --quiet            Print nothing but errors

`

const helpWatch = `Usage: filee watch <folder> (--to <format> | --preset <preset>) [options]

Converts files that land in the folder until Ctrl+C. Files are converted once they are complete (no
longer growing or open in another program). Output goes to a "converted" folder inside it unless -o is given.

Options:
  -t, --to <format>      Target format
  -p, --preset <preset>  A preset from the app, by id or name
  -o, --output <folder>  Where converted files go (default: <folder>/converted)
  -r, --recursive        Also watch sub folders (their files keep the sub folder in the output)
      --move-originals   Move converted sources to <folder>/originals; files already waiting are
                         converted at start
      --settle <seconds> How long a file must stay unchanged (default %g)
      --keep-dates       Give converted files the created and modified dates of the originals
      --zip              Save the result of each file as one ZIP (a Shapefile's files together)
      --quiet            Print nothing but errors

`
