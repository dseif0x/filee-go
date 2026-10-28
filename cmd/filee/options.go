package main

import (
	"slices"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/presets"
)

// options is a parsed command line; err is set when the arguments are wrong.
type options struct {
	command       string
	inputs        []string
	to, preset    string
	output        string
	recursive     bool
	quality       int
	conflict      presets.ConflictPolicy
	json, quiet   bool
	moveOriginals bool
	keepDates     bool
	zip           bool
	settle        float64 // < 0 when not given
	background    bool    // app: started at sign-in, no window
	selftest      bool    // app: exercise the native bindings and exit
	webOnly       bool
	noBrowser     bool
	err           string
}

var commands = []string{"convert", "watch", "formats", "presets", "engines", "app", "settings", "help", "version"}

func parseOptions(args []string) *options {
	o := &options{command: "help", settle: -1}
	if len(args) == 0 {
		return o
	}
	first := strings.ToLower(args[0])
	switch first {
	case "-h", "--help", "/?":
		first = "help"
	case "-v", "--version":
		first = "version"
	}
	o.command = first
	if !slices.Contains(commands, first) {
		o.err = "Unknown command '" + args[0] + "'."
		return o
	}
	fail := func(msg string) {
		if o.err == "" {
			o.err = msg
		}
	}
	for i := 1; i < len(args); i++ {
		arg := args[i]
		value := func() (string, bool) {
			if i+1 < len(args) {
				i++
				return args[i], true
			}
			fail(arg + " needs a value.")
			return "", false
		}
		switch strings.ToLower(arg) {
		case "-t", "--to":
			o.to, _ = value()
		case "-p", "--preset":
			o.preset, _ = value()
		case "-o", "--output":
			o.output, _ = value()
		case "-r", "--recursive":
			o.recursive = true
		case "-q", "--quality":
			if v, ok := value(); ok {
				if q, err := strconv.Atoi(v); err == nil && q >= 1 && q <= 100 {
					o.quality = q
				} else {
					fail("--quality must be a number from 1 to 100.")
				}
			}
		case "--overwrite":
			o.conflict = presets.ConflictOverwrite
		case "--skip":
			o.conflict = presets.ConflictSkip
		case "--json":
			o.json = true
		case "--quiet":
			o.quiet = true
		case "--move-originals":
			o.moveOriginals = true
		case "--keep-dates":
			o.keepDates = true
		case "--zip":
			o.zip = true
		case "--settle":
			if v, ok := value(); ok {
				if s, err := strconv.ParseFloat(v, 64); err == nil && s >= 0 {
					o.settle = s
				} else {
					fail("--settle must be a number of seconds.")
				}
			}
		case "--background":
			o.background = true
		case "--selftest", "--self-test":
			o.selftest = true
		case "--web-only":
			o.webOnly = true
		case "--no-browser":
			o.noBrowser = true
		case "-h", "--help":
			o.inputs = append([]string{o.command}, o.inputs...)
			o.command = "help"
			return o
		default:
			if strings.HasPrefix(arg, "--") || (strings.HasPrefix(arg, "-") && len(arg) == 2) {
				fail("Unknown option '" + arg + "'.")
			} else {
				o.inputs = append(o.inputs, arg)
			}
		}
	}
	switch {
	case o.command == "convert" && len(o.inputs) == 0:
		fail("Give at least one file or folder to convert.")
	case (o.command == "convert" || o.command == "watch") && (o.to == "") == (o.preset == ""):
		fail("Use either --to <format> or --preset <preset>.")
	case o.command == "watch" && len(o.inputs) != 1:
		fail("Give exactly one folder to watch.")
	}
	return o
}
