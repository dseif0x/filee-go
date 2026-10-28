package subtitles

import (
	"bytes"
	"errors"
	"fmt"
	"html"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Readers of the line-based text subtitle formats: SubRip (SRT), WebVTT,
// ASS/SSA, LRC and MicroDVD. SAMI, TTML and SCC have their own files. Readers
// are lenient: missing blank lines, one- or two-digit fractions, comma or dot
// before the milliseconds and stray styling codes are accepted, because
// subtitle files are hand-made as often as not.

// ReadFormats are the format ids this engine reads.
var ReadFormats = []string{"srt", "ass", "ssa", "vtt", "smi", "lrc", "ttml", "sub", "scc"}

func htmlDecode(s string) string { return html.UnescapeString(s) }

// Read reads a file of format (decoding it with Decode). The error explains
// when the file is not a subtitle file of that format or has no cues.
func Read(format string, b []byte) (*Document, error) {
	if format == "sub" && looksBinary(b) {
		return nil, errors.New("This .sub file holds VobSub picture subtitles (from a DVD), not MicroDVD text. Picture subtitles can't be turned into text without OCR.")
	}
	text := strings.ReplaceAll(strings.ReplaceAll(Decode(b), "\r\n", "\n"), "\r", "\n")
	var d *Document
	var err error
	switch format {
	case "srt":
		d = readSrt(text)
	case "vtt":
		d = readVtt(text)
	case "ass", "ssa":
		d = readAss(text)
	case "lrc":
		d = readLrc(text)
	case "sub":
		d, err = readMicroDvd(text)
	case "smi":
		d = readSami(text)
	case "ttml":
		d, err = readTtml(text)
	case "scc":
		d, err = readScc(text)
	default:
		return nil, fmt.Errorf("Subtitles in '%s' can't be read.", format)
	}
	if err != nil {
		return nil, err
	}
	d.Finish()
	if len(d.Cues) == 0 {
		return nil, fmt.Errorf("No subtitles were found in this file (it is empty or not a %s subtitle file).", strings.ToUpper(format))
	}
	return d, nil
}

// looksBinary is true for a NUL byte or an MPEG pack header: a VobSub .sub, not text.
func looksBinary(b []byte) bool {
	return bytes.HasPrefix(b, []byte{0, 0, 1, 0xBA}) || bytes.IndexByte(b[:min(len(b), 4096)], 0) >= 0
}

// group returns a named group of a match ("" when it didn't take part).
func group(re *regexp.Regexp, m []string, name string) string {
	if i := re.SubexpIndex(name); i >= 0 && i < len(m) {
		return m[i]
	}
	return ""
}

// ---- SubRip ----

var (
	reSrtTiming   = regexp.MustCompile(`(?P<sh>\d+):(?P<sm>\d{1,2}):(?P<ss>\d{1,2})(?:[,.:](?P<sf>\d{1,3}))?\s*-+>\s*(?P<eh>\d+):(?P<em>\d{1,2}):(?P<es>\d{1,2})(?:[,.:](?P<ef>\d{1,3}))?`)
	reVttTiming   = regexp.MustCompile(`^\s*(?:(?P<sh>\d+):)?(?P<sm>\d{1,2}):(?P<ss>\d{2})[.,](?P<sf>\d{1,3})\s+-->\s+(?:(?P<eh>\d+):)?(?P<em>\d{1,2}):(?P<es>\d{2})[.,](?P<ef>\d{1,3})`)
	reBlankLines  = regexp.MustCompile(`\n[ \t]*\n+`)
	reAssTime     = regexp.MustCompile(`^(?P<h>\d+):(?P<m>\d{1,2}):(?P<s>\d{1,2})(?:[.:](?P<f>\d{1,3}))?$`)
	reAssOverride = regexp.MustCompile(`\{[^}]*\}`)
	reAssTag      = regexp.MustCompile(`\\(?P<name>1c|2c|3c|4c|1a|2a|3a|4a|alpha|an|fn|fs|fscx|fscy|fsp|fr[xyz]?|fe|bord|xbord|ybord|shad|xshad|yshad|be|blur|pos|move|org|clip|iclip|fad|fade|kf|ko|k|K|q|t|p|r|i|b|u|s|c|a)(?P<arg>[^\\]*)`)
	reLrcMetadata = regexp.MustCompile(`^\[(?P<key>[A-Za-z]+):(?P<value>[^\]]*)\]\s*$`)
	reLrcTime     = regexp.MustCompile(`\[(?P<m>\d+):(?P<s>\d{1,2})(?:[.:](?P<f>\d{1,3}))?\]`)
	reLrcWordTime = regexp.MustCompile(`<\d+:\d{1,2}(?:[.:]\d{1,3})?>`)
	reMicroDvd    = regexp.MustCompile(`^\{(?P<start>\d+)\}\{(?P<end>\d*)\}(?P<text>.*)$`)
	reMicroCode   = regexp.MustCompile(`\{(?P<key>[A-Za-z]):(?P<value>[^}]*)\}`)
)

func timing(re *regexp.Regexp, m []string, prefix string) time.Duration {
	return timeFrom(group(re, m, prefix+"h"), group(re, m, prefix+"m"), group(re, m, prefix+"s"), group(re, m, prefix+"f"))
}

func isNumber(line string) bool {
	t := strings.TrimSpace(line)
	return t != "" && strings.Trim(t, "0123456789") == ""
}

// readSrt reads SubRip: a number, 00:00:01,000 --> 00:00:02,500, text lines, a
// blank line. A cue starts at every timing line, so blank lines inside a cue's
// text and missing blank lines between cues don't matter.
func readSrt(text string) *Document {
	d := &Document{}
	var start, end time.Duration
	started := false
	var body []string
	flush := func(beforeNextCue bool) {
		if !started {
			return
		}
		for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
			body = body[:len(body)-1]
		}
		// The next cue's number ended up as this cue's last line.
		if beforeNextCue && len(body) > 0 && isNumber(body[len(body)-1]) {
			body = body[:len(body)-1]
		}
		// ASS override codes some SRT files carry ({\an8} = top of the screen) are not text.
		cue := reAssOverride.ReplaceAllString(strings.Join(body, "\n"), "")
		d.Add(start, end, fromHTML(cue, false))
	}
	for _, line := range strings.Split(text, "\n") {
		if m := reSrtTiming.FindStringSubmatch(line); m != nil {
			flush(true)
			start, end = timing(reSrtTiming, m, "s"), timing(reSrtTiming, m, "e")
			started = true
			body = nil
		} else if started {
			body = append(body, line)
		}
	}
	flush(false)
	return d
}

// ---- WebVTT ----

// readVtt reads WebVTT: a WEBVTT header, then blocks separated by blank lines.
// NOTE, STYLE and REGION blocks are skipped; a cue is an optional id line,
// [hh:]mm:ss.ttt --> [hh:]mm:ss.ttt [settings] and its text.
func readVtt(text string) *Document {
	d := &Document{}
	for _, block := range reBlankLines.Split(strings.Trim(text, "\n \uFEFF"), -1) {
		lines := strings.Split(block, "\n")
		timingLine := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "-->") })
		if timingLine < 0 && strings.HasPrefix(lines[0], "WEBVTT") {
			// Header lines such as "Language: ko".
			for _, l := range lines {
				if len(l) >= 9 && strings.EqualFold(l[:9], "Language:") {
					d.Language = strings.TrimSpace(l[9:])
					break
				}
			}
			continue
		}
		if timingLine < 0 || timingLine > 1 || strings.HasPrefix(lines[0], "NOTE") {
			continue
		}
		m := reVttTiming.FindStringSubmatch(lines[timingLine])
		if m == nil {
			continue
		}
		d.Add(timing(reVttTiming, m, "s"), timing(reVttTiming, m, "e"), fromHTML(strings.Join(lines[timingLine+1:], "\n"), true))
	}
	return d
}

// ---- ASS / SSA ----

// readAss reads Dialogue: lines of the [Events] section in the order its
// Format: line gives. Override codes become markup where it has them (\i, \b,
// \u, \c) and are dropped otherwise; vector drawings (\p1) are no text and are
// left out. Bold, italic and underline styles apply too.
func readAss(text string) *Document {
	d := &Document{}
	section := ""
	styleFormat := []string{"name", "fontname", "fontsize", "primarycolour", "secondarycolour", "outlinecolour", "backcolour", "bold", "italic", "underline"}
	eventFormat := []string{"layer", "start", "end", "style", "name", "marginl", "marginr", "marginv", "effect", "text"}
	styles := map[string]string{} // lower-case name → opening markup
	formatOf := func(v string) []string {
		parts := strings.Split(v, ",")
		for i := range parts {
			parts[i] = strings.ToLower(strings.TrimSpace(parts[i]))
		}
		return parts
	}
	field := func(format, fields []string, name string) string {
		if i := slices.Index(format, name); i >= 0 && i < len(fields) {
			return strings.TrimSpace(fields[i])
		}
		return ""
	}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimLeft(strings.TrimSpace(raw), "\uFEFF")
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") && len(line) >= 2 {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:colon]))
		value := strings.TrimLeftFunc(line[colon+1:], unicode.IsSpace)
		switch {
		case section == "script info" && key == "title" && value != "" && value != "<untitled>":
			d.Title = value
		case key == "format" && strings.Contains(section, "styles"):
			styleFormat = formatOf(value)
		case key == "format" && section == "events":
			eventFormat = formatOf(value)
		case key == "style" && strings.Contains(section, "styles"):
			fields := strings.Split(value, ",")
			on := func(flag string) bool { return flag == "-1" || flag == "1" }
			open := ""
			if on(field(styleFormat, fields, "bold")) {
				open += "<b>"
			}
			if on(field(styleFormat, fields, "italic")) {
				open += "<i>"
			}
			if on(field(styleFormat, fields, "underline")) {
				open += "<u>"
			}
			styles[strings.ToLower(field(styleFormat, fields, "name"))] = open
		case key == "dialogue" && section == "events":
			// The text is the last field and may itself contain commas.
			fields := strings.SplitN(value, ",", len(eventFormat))
			start := reAssTime.FindStringSubmatch(field(eventFormat, fields, "start"))
			end := reAssTime.FindStringSubmatch(field(eventFormat, fields, "end"))
			if start == nil || end == nil {
				continue
			}
			cueText := ""
			if i := slices.Index(eventFormat, "text"); i >= 0 && i < len(fields) {
				cueText = fields[i]
			}
			markup, ok := assText(cueText)
			if !ok {
				continue
			}
			style := styles[strings.ToLower(strings.TrimLeft(field(eventFormat, fields, "style"), "*"))]
			d.Add(timing(reAssTime, start, ""), timing(reAssTime, end, ""), style+markup)
		}
	}
	return d
}

// assText is ASS dialogue text as cue markup; false for a drawing.
func assText(text string) (string, bool) {
	var b strings.Builder
	pos := 0
	drawing := false
	for _, blk := range reAssOverride.FindAllStringIndex(text, -1) {
		if !drawing {
			b.WriteString(assPlain(text[pos:blk[0]]))
		}
		pos = blk[1]
		for _, t := range reAssTag.FindAllStringSubmatch(text[blk[0]+1:blk[1]-1], -1) {
			name := group(reAssTag, t, "name")
			arg := strings.TrimSpace(group(reAssTag, t, "arg"))
			switch name {
			case "i", "b", "u":
				on := arg != "" && arg != "0"
				if w, err := strconv.Atoi(arg); name == "b" && err == nil && w > 1 && w < 600 {
					on = false
				}
				b.WriteString(tag(name, !on))
			case "c", "1c":
				b.WriteString("</font>")
				if color := assColor(arg); color != "" {
					b.WriteString(`<font color="` + color + `">`)
				}
			case "r":
				b.WriteString("</font></u></b></i>")
			case "p":
				drawing = arg != "" && arg != "0"
			}
		}
	}
	if !drawing {
		b.WriteString(assPlain(text[pos:]))
	}
	result := b.String()
	if strings.TrimSpace(PlainText(Normalize(result))) == "" && strings.Contains(text, `\p`) {
		return "", false
	}
	return result, true
}

// assPlain turns \N into a line break, \n into a space (a break only in wrap
// style 2) and \h into a non-breaking space.
func assPlain(text string) string {
	return strings.NewReplacer(`\N`, "\n", `\n`, " ", `\h`, "\u00A0").Replace(text)
}

// assColor reads &HBBGGRR& (ASS stores colours blue first, sometimes with
// alpha in front) as #RRGGBB.
func assColor(value string) string {
	hex := strings.TrimLeft(strings.Trim(value, "&"), "Hh")
	if hex == "" {
		return ""
	}
	for _, c := range hex {
		if !isHexDigit(c) {
			return ""
		}
	}
	if len(hex) < 6 {
		hex = strings.Repeat("0", 6-len(hex)) + hex
	}
	hex = hex[len(hex)-6:]
	return strings.ToUpper("#" + hex[4:6] + hex[2:4] + hex[:2])
}

// ---- LRC ----

// readLrc reads LRC lyrics: [mm:ss.xx] time tags (several per line for
// repeated lines), [ti:…] and [offset:…] tags. A line lasts until the next one;
// an empty line only ends the one before. Word times (<mm:ss.xx>) of enhanced
// LRC are dropped.
func readLrc(text string) *Document {
	d := &Document{}
	var offset time.Duration
	type entry struct {
		at    time.Duration
		lyric string
	}
	var entries []entry
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimLeft(strings.TrimSpace(raw), "\uFEFF")
		if m := reLrcMetadata.FindStringSubmatch(line); m != nil {
			value := strings.TrimSpace(group(reLrcMetadata, m, "value"))
			switch strings.ToLower(group(reLrcMetadata, m, "key")) {
			case "ti":
				if value != "" {
					d.Title = value
				}
			case "la", "lang":
				if value != "" {
					d.Language = value
				}
			case "offset":
				// A positive offset shows the lyrics earlier.
				if ms, err := strconv.Atoi(value); err == nil {
					offset = time.Duration(ms) * time.Millisecond
				}
			}
			continue
		}
		times := reLrcTime.FindAllStringSubmatchIndex(line, -1)
		if len(times) == 0 || times[0][0] != 0 {
			continue
		}
		// Only the tags at the start of the line are times; the rest is the lyric.
		count := 0
		for expected := 0; count < len(times) && times[count][0] == expected; count++ {
			expected = times[count][1]
		}
		lyric := strings.TrimSpace(reLrcWordTime.ReplaceAllString(line[times[count-1][1]:], ""))
		for _, t := range times[:count] {
			sub := func(i int) string {
				if t[2*i] < 0 {
					return ""
				}
				return line[t[2*i]:t[2*i+1]]
			}
			entries = append(entries, entry{timeFrom("0", sub(1), sub(2), sub(3)), lyric})
		}
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].at < entries[j].at })
	for i, e := range entries {
		if e.lyric == "" {
			continue
		}
		end := e.at + DefaultDuration
		if i+1 < len(entries) {
			end = entries[i+1].at
		}
		d.Add(e.at-offset, end-offset, e.lyric)
	}
	return d
}

// ---- MicroDVD ----

// MicroDvdDefaultFps is the frame rate MicroDVD files are assumed to use when
// they don't say (the film rate of most rips).
const MicroDvdDefaultFps = 23.976

// readMicroDvd reads {start frame}{end frame}text, | between lines, {y:i}-style
// codes. A first line {1}{1}25 gives the frame rate.
func readMicroDvd(text string) (*Document, error) {
	d := &Document{}
	fps := MicroDvdDefaultFps
	type line struct {
		start  int64
		end    int64
		hasEnd bool
		body   string
	}
	var lines []line
	for _, raw := range strings.Split(text, "\n") {
		m := reMicroDvd.FindStringSubmatch(strings.TrimLeft(strings.TrimSpace(raw), "\uFEFF"))
		if m == nil {
			continue
		}
		start, _ := strconv.ParseInt(m[1], 10, 64)
		end, err := strconv.ParseInt(m[2], 10, 64)
		body := m[3]
		if len(lines) == 0 && start <= 1 {
			if rate, err := strconv.ParseFloat(strings.TrimSpace(body), 64); err == nil && rate > 1 && rate < 200 {
				fps = rate
				continue
			}
		}
		lines = append(lines, line{start, end, err == nil, body})
	}
	if len(lines) == 0 {
		return nil, errors.New("This .sub file is not a MicroDVD subtitle ({frame}{frame}text lines). SubViewer and VobSub .sub files are not supported.")
	}
	frameTime := func(f int64) time.Duration { return fromSeconds(float64(f) / fps) }
	for i, l := range lines {
		var end time.Duration
		switch {
		case l.hasEnd && l.end > l.start:
			end = frameTime(l.end)
		case i+1 < len(lines):
			end = frameTime(lines[i+1].start)
		default:
			end = frameTime(l.start) + DefaultDuration
		}
		d.Add(frameTime(l.start), end, microDvdText(l.body))
	}
	return d, nil
}

// microDvdText reads | as line breaks; {y:i} styles one line, {Y:i} all of them; {c:$BBGGRR} colours.
func microDvdText(body string) string {
	var all []string
	var lines []string
	for _, part := range strings.Split(body, "|") {
		var line []string
		for _, c := range reMicroCode.FindAllStringSubmatch(part, -1) {
			styles := microDvdStyles(c[1], c[2])
			if unicode.IsUpper(rune(c[1][0])) {
				all = append(all, styles...)
			} else {
				line = append(line, styles...)
			}
		}
		lines = append(lines, openStyles(line)+reMicroCode.ReplaceAllString(part, "")+closeStyles(line))
	}
	return openStyles(all) + strings.Join(lines, "\n") + closeStyles(all)
}

func openStyles(styles []string) string {
	var b strings.Builder
	for _, s := range styles {
		if strings.HasPrefix(s, "#") {
			b.WriteString(`<font color="` + s + `">`)
		} else {
			b.WriteString("<" + s + ">")
		}
	}
	return b.String()
}

func closeStyles(styles []string) string {
	var b strings.Builder
	for i := len(styles) - 1; i >= 0; i-- {
		if strings.HasPrefix(styles[i], "#") {
			b.WriteString("</font>")
		} else {
			b.WriteString("</" + styles[i] + ">")
		}
	}
	return b.String()
}

func microDvdStyles(key, value string) []string {
	switch unicode.ToLower(rune(key[0])) {
	case 'y':
		var out []string
		for _, c := range strings.ToLower(value) {
			if c == 'i' || c == 'b' || c == 'u' {
				out = append(out, string(c))
			}
		}
		return out
	case 'c':
		if color := assColor(strings.TrimLeft(value, "$")); color != "" {
			return []string{color}
		}
	}
	return nil
}
