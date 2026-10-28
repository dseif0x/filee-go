package subtitles

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The common model every subtitle reader produces and every writer consumes:
// timed cues whose text uses a tiny markup (line breaks plus <i>, <b>, <u> and
// <font color="#RRGGBB">, SRT's own tags), so styling survives between formats
// that have it and is dropped cleanly by those that don't (LRC, MicroDVD).

// Cue is one subtitle shown from Start to End. Text is lines separated by
// "\n", with the markup of the package.
type Cue struct {
	Start, End time.Duration
	Text       string
}

// Document is a subtitle file: its cues in time order, and the title and
// language (a tag such as "ko-KR") when the file says.
type Document struct {
	Cues     []Cue
	Title    string
	Language string
}

// DefaultDuration is how long a cue without an end time (LRC, the last SAMI
// line) stays on screen.
const DefaultDuration = 4 * time.Second

// Add adds a cue with normalized markup; empty and zero-length cues are dropped.
func (d *Document) Add(start, end time.Duration, text string) {
	if start < 0 {
		start = 0
	}
	n := Normalize(text)
	if end <= start || strings.TrimSpace(PlainText(n)) == "" {
		return
	}
	d.Cues = append(d.Cues, Cue{start, end, n})
}

// Finish puts the cues in time order (stable) and drops exact duplicates (ASS shadow layers).
func (d *Document) Finish() *Document {
	seen := map[Cue]bool{}
	var cues []Cue
	for _, c := range d.Cues {
		if !seen[c] {
			seen[c] = true
			cues = append(cues, c)
		}
	}
	sort.SliceStable(cues, func(i, j int) bool {
		if cues[i].Start != cues[j].Start {
			return cues[i].Start < cues[j].Start
		}
		return cues[i].End < cues[j].End
	})
	d.Cues = cues
	return d
}

// GuessLanguage is the language of the text when the file doesn't name one:
// Korean, Japanese and Chinese by their scripts, English otherwise.
func (d *Document) GuessLanguage() string {
	if strings.TrimSpace(d.Language) != "" {
		return d.Language
	}
	hangul, kana, han := 0, 0, 0
	for _, c := range d.Cues {
		for _, r := range c.Text {
			switch {
			case r >= '가' && r <= '힣' || r >= 'ᄀ' && r <= 'ᇿ' || r >= '㄰' && r <= '㆏':
				hangul++
			case r >= '぀' && r <= 'ヿ':
				kana++
			case r >= '一' && r <= '鿿':
				han++
			}
		}
	}
	switch {
	case hangul > 0 && hangul >= kana && hangul >= han/4:
		return "ko-KR"
	case kana > 0:
		return "ja-JP"
	case han > 0:
		return "zh-CN"
	}
	return "en-US"
}

// ---- markup ----

type tokenKind int

const (
	tokText tokenKind = iota
	tokBreak
	tokOpen
	tokClose
)

// token is a piece of cue text: text, a line break, or the start / end of a
// style ("i", "b", "u" or a colour "#RRGGBB", closed as "font").
type token struct {
	kind  tokenKind
	value string
}

func (t token) isColor() bool { return strings.HasPrefix(t.value, "#") }

var reCanonicalTag = regexp.MustCompile(`<(?P<close>/?)(?P<tag>i|b|u)>|<font color="#(?P<color>[0-9A-Fa-f]{6})">|</font>`)

// tokens splits normalized cue text into tokens.
func tokens(text string) []token {
	var out []token
	pos := 0
	for _, m := range reCanonicalTag.FindAllStringSubmatchIndex(text, -1) {
		out = addText(out, text[pos:m[0]])
		switch {
		case m[6] >= 0:
			out = append(out, token{tokOpen, "#" + strings.ToUpper(text[m[6]:m[7]])})
		case text[m[0]:m[1]] == "</font>":
			out = append(out, token{tokClose, "font"})
		default:
			kind := tokOpen
			if m[3] > m[2] {
				kind = tokClose
			}
			out = append(out, token{kind, text[m[4]:m[5]]})
		}
		pos = m[1]
	}
	return addText(out, text[pos:])
}

func addText(out []token, text string) []token {
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			out = append(out, token{tokBreak, ""})
		}
		if line != "" {
			out = append(out, token{tokText, line})
		}
	}
	return out
}

var reMultipleSpaces = regexp.MustCompile(` {2,}`)

func closeOf(open string) string {
	if strings.HasPrefix(open, "#") {
		return "font"
	}
	return open
}

// Normalize cleans up cue text written by a reader: tags properly nested
// (overlapping ones are closed and reopened), unclosed ones closed, stray
// closing tags and empty styles dropped, spaces at line ends and empty lines
// removed.
func Normalize(text string) string {
	text = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\u00A0", " ", "\t", " ").Replace(text)
	var balanced []token
	var open []string
	for _, t := range tokens(text) {
		switch t.kind {
		case tokOpen:
			if !t.isColor() && containsString(open, t.value) {
				continue // already on
			}
			open = append(open, t.value)
			balanced = append(balanced, t)
		case tokClose:
			index := -1
			for i := len(open) - 1; i >= 0; i-- {
				if t.value == "font" && strings.HasPrefix(open[i], "#") || t.value == open[i] {
					index = i
					break
				}
			}
			if index < 0 {
				continue
			}
			reopen := append([]string{}, open[index+1:]...)
			for i := len(open) - 1; i >= index; i-- {
				balanced = append(balanced, token{tokClose, closeOf(open[i])})
			}
			open = open[:index]
			for _, tag := range reopen {
				open = append(open, tag)
				balanced = append(balanced, token{tokOpen, tag})
			}
		default:
			balanced = append(balanced, t)
		}
	}
	for i := len(open) - 1; i >= 0; i-- {
		balanced = append(balanced, token{tokClose, closeOf(open[i])})
	}
	// Lines without their surrounding spaces, empty styles removed, no empty lines.
	var lines []string
	for _, line := range strings.Split(render(removeEmptyStyles(balanced)), "\n") {
		line = strings.TrimSpace(reMultipleSpaces.ReplaceAllString(line, " "))
		if strings.TrimSpace(PlainText(line)) != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func removeEmptyStyles(ts []token) []token {
	for changed := true; changed; {
		changed = false
		for i := 0; i+1 < len(ts); i++ {
			if ts[i].kind == tokOpen && ts[i+1].kind == tokClose {
				ts = append(ts[:i], ts[i+2:]...)
				changed = true
				break
			}
		}
	}
	return ts
}

func render(ts []token) string {
	var b strings.Builder
	for _, t := range ts {
		switch {
		case t.kind == tokText:
			b.WriteString(t.value)
		case t.kind == tokBreak:
			b.WriteByte('\n')
		case t.kind == tokOpen && t.isColor():
			b.WriteString(`<font color="` + t.value + `">`)
		case t.kind == tokOpen:
			b.WriteString("<" + t.value + ">")
		default:
			b.WriteString("</" + t.value + ">")
		}
	}
	return b.String()
}

// PlainText is the text without any markup (lines still separated by "\n").
func PlainText(text string) string {
	var b strings.Builder
	for _, t := range tokens(text) {
		switch t.kind {
		case tokText:
			b.WriteString(t.value)
		case tokBreak:
			b.WriteByte('\n')
		}
	}
	return b.String()
}

var (
	reAnyTag         = regexp.MustCompile(`<\s*(?P<close>/?)\s*(?P<name>[^\s>/]+)(?P<attributes>[^>]*)>`)
	reColorAttribute = regexp.MustCompile(`(?i)color\s*=\s*(?P<value>"[^"]*"|'[^']*'|[^\s>]+)`)
)

// fromHTML turns HTML-like subtitle text (SRT, WebVTT, SAMI) into cue markup:
// i, b, u, font colours and WebVTT's colour classes (<c.yellow>) are kept, br
// breaks the line, every other tag is dropped (its text stays). decodeEntities
// decodes &amp;, &nbsp;, … (WebVTT and SAMI; SRT has no entities).
func fromHTML(text string, decodeEntities bool) string {
	var b strings.Builder
	pos, colors := 0, 0
	var classes []bool // WebVTT <c.yellow>: whether the class gave a colour
	for _, m := range reAnyTag.FindAllStringSubmatchIndex(text, -1) {
		appendHTMLText(&b, text[pos:m[0]], decodeEntities)
		pos = m[1]
		closing := m[3] > m[2]
		name := strings.ToLower(text[m[4]:m[5]])
		attributes := text[m[6]:m[7]]
		if name == "c" || strings.HasPrefix(name, "c.") {
			switch {
			case closing:
				if len(classes) > 0 {
					top := classes[len(classes)-1]
					classes = classes[:len(classes)-1]
					if top {
						b.WriteString("</font>")
					}
				}
			default:
				color := ""
				for _, c := range strings.Split(name, ".")[1:] {
					if v, ok := namedColors[strings.ToLower(c)]; ok {
						color = v
						break
					}
				}
				classes = append(classes, color != "")
				if color != "" {
					b.WriteString(`<font color="` + color + `">`)
				}
			}
			continue
		}
		switch name {
		case "i", "em":
			b.WriteString(tag("i", closing))
		case "b", "strong":
			b.WriteString(tag("b", closing))
		case "u":
			b.WriteString(tag("u", closing))
		case "br":
			b.WriteByte('\n')
		case "font":
			if closing {
				if colors > 0 {
					colors--
					b.WriteString("</font>")
				}
				break
			}
			// A <font face=…> without colour has nothing to keep; its </font> then must not close a colour.
			if a := reColorAttribute.FindStringSubmatch(attributes); a != nil {
				if color := ParseColor(a[1]); color != "" {
					colors++
					b.WriteString(`<font color="` + color + `">`)
				}
			}
		}
	}
	appendHTMLText(&b, text[pos:], decodeEntities)
	return b.String()
}

func tag(name string, closing bool) string {
	if closing {
		return "</" + name + ">"
	}
	return "<" + name + ">"
}

func appendHTMLText(b *strings.Builder, text string, decodeEntities bool) {
	if decodeEntities {
		text = htmlDecode(text)
	}
	// Bidirectional marks of WebVTT (&lrm; &rlm;) and zero-width characters carry nothing visible.
	b.WriteString(strings.NewReplacer("\u200E", "", "\u200F", "", "\u200B", "").Replace(text))
}

var namedColors = map[string]string{
	"white": "#FFFFFF", "black": "#000000", "red": "#FF0000", "lime": "#00FF00", "green": "#008000",
	"blue": "#0000FF", "yellow": "#FFFF00", "cyan": "#00FFFF", "aqua": "#00FFFF", "magenta": "#FF00FF",
	"fuchsia": "#FF00FF", "gray": "#808080", "grey": "#808080", "silver": "#C0C0C0", "orange": "#FFA500",
	"purple": "#800080", "maroon": "#800000", "navy": "#000080", "olive": "#808000", "teal": "#008080",
	"pink": "#FFC0CB",
}

// ParseColor reads #rgb, #rrggbb, #rrggbbaa, bare hex digits, rgb(…) or a
// colour name as "#RRGGBB"; "" when it can't be read.
func ParseColor(value string) string {
	value = strings.TrimSpace(strings.Trim(strings.TrimSpace(value), `"'`))
	if len(value) >= 3 && strings.EqualFold(value[:3], "rgb") {
		if open := strings.IndexByte(value, '('); open > 0 && strings.HasSuffix(value, ")") {
			parts := strings.Split(value[open+1:len(value)-1], ",")
			if len(parts) < 3 {
				return ""
			}
			out := "#"
			for _, p := range parts[:3] {
				v, err := strconv.Atoi(strings.TrimSpace(p))
				if err != nil || v < 0 || v > 255 {
					return ""
				}
				out += fmt.Sprintf("%02X", v)
			}
			return out
		}
	}
	if v, ok := namedColors[strings.ToLower(value)]; ok {
		return v
	}
	hex := strings.TrimLeft(value, "#")
	for _, c := range hex {
		if !isHexDigit(c) {
			return ""
		}
	}
	switch len(hex) {
	case 3:
		var b strings.Builder
		b.WriteByte('#')
		for _, c := range strings.ToUpper(hex) {
			b.WriteRune(c)
			b.WriteRune(c)
		}
		return b.String()
	case 6, 8:
		return "#" + strings.ToUpper(hex[:6])
	}
	return ""
}

func isHexDigit(c rune) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// colorName is the colour's name when it is one of the basic CSS colours (WebVTT has classes for those).
func colorName(hex string) string {
	for _, n := range []string{"white", "black", "red", "lime", "blue", "yellow", "cyan", "magenta"} {
		if namedColors[n] == hex {
			return n
		}
	}
	return ""
}

// ---- time ----

func roundEven(f float64) int64 { return int64(math.RoundToEven(f)) }

// fromSeconds is .NET's TimeSpan.FromSeconds: truncated to 100 ns ticks.
func fromSeconds(s float64) time.Duration { return time.Duration(int64(s*1e7)) * 100 }

// timeFrom makes a time of hours, minutes, seconds and a decimal fraction ("5" = 0.5 s, "045" = 45 ms).
func timeFrom(hours, minutes, seconds, fraction string) time.Duration {
	var ms int64
	if fraction != "" {
		f, _ := strconv.ParseFloat("0."+fraction, 64)
		ms = roundEven(f * 1000)
	}
	n := func(s string) int64 {
		v, _ := strconv.ParseInt(s, 10, 64)
		return v
	}
	return time.Duration(n(hours))*time.Hour + time.Duration(n(minutes))*time.Minute + time.Duration(n(seconds))*time.Second + time.Duration(ms)*time.Millisecond
}

func totalMs(t time.Duration) float64 { return float64(t/100) / 1e4 }

// millisecondsTime is HH:MM:SS{sep}mmm (SRT: comma, WebVTT and TTML: dot).
func millisecondsTime(t time.Duration, sep byte) string {
	ms := roundEven(math.Max(0, totalMs(t)))
	return fmt.Sprintf("%02d:%02d:%02d%c%03d", ms/3_600_000, ms/60_000%60, ms/1000%60, sep, ms%1000)
}

// centisecondsTime is H:MM:SS.cc (ASS).
func centisecondsTime(t time.Duration) string {
	cs := roundEven(math.Max(0, totalMs(t)) / 10)
	return fmt.Sprintf("%d:%02d:%02d.%02d", cs/360_000, cs/6000%60, cs/100%60, cs%100)
}

// lrcTime is mm:ss.cc (LRC; minutes go past 99 for long recordings).
func lrcTime(t time.Duration) string {
	cs := roundEven(math.Max(0, totalMs(t)) / 10)
	return fmt.Sprintf("%02d:%02d.%02d", cs/6000, cs/100%60, cs%100)
}
