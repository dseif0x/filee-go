package subtitles

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// Writers of every subtitle target. All write UTF-8: SRT, ASS, SAMI and
// MicroDVD with a byte order mark (Windows players such as PotPlayer and
// KMPlayer take files without one for the PC's code page), WebVTT, TTML and
// LRC without (browsers, XML and music players expect plain UTF-8).

// WriteFormats are the format ids this engine writes.
var WriteFormats = []string{"srt", "ass", "vtt", "smi", "lrc", "ttml", "sub"}

// MicroDvdFps is the frame rate MicroDVD files are written with (stated in their first line).
const MicroDvdFps = MicroDvdDefaultFps

const utf8BOM = "\xEF\xBB\xBF"

// Write writes a document in format.
func Write(d *Document, format string) ([]byte, error) {
	var text string
	bom := true
	switch format {
	case "srt":
		text = writeSrt(d)
	case "ass":
		text = writeAss(d)
	case "vtt":
		text, bom = writeVtt(d), false
	case "smi":
		text = writeSami(d)
	case "lrc":
		text, bom = writeLrc(d), false
	case "ttml":
		text, bom = writeTtml(d), false
	case "sub":
		text = writeMicroDvd(d)
	default:
		return nil, fmt.Errorf("Subtitles can't be written as '%s'.", format)
	}
	if bom {
		text = utf8BOM + text
	}
	return []byte(text), nil
}

// writeSrt keeps the markup as it is: SubRip's own tags.
func writeSrt(d *Document) string {
	var b strings.Builder
	for i, c := range d.Cues {
		fmt.Fprintf(&b, "%d\r\n%s --> %s\r\n", i+1, millisecondsTime(c.Start, ','), millisecondsTime(c.End, ','))
		b.WriteString(strings.ReplaceAll(c.Text, "\n", "\r\n"))
		b.WriteString("\r\n\r\n")
	}
	return b.String()
}

func escapeHTML(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// writeVtt writes i / b / u tags, colours as the basic colour classes (others
// are dropped), text escaped.
func writeVtt(d *Document) string {
	var b strings.Builder
	b.WriteString("WEBVTT\n")
	if d.Language != "" {
		b.WriteString("Language: " + d.Language + "\n")
	}
	b.WriteByte('\n')
	for _, c := range d.Cues {
		b.WriteString(millisecondsTime(c.Start, '.') + " --> " + millisecondsTime(c.End, '.') + "\n")
		var colors []bool
		for _, t := range tokens(c.Text) {
			switch {
			case t.kind == tokText:
				// "-->" would end the cue text early.
				b.WriteString(strings.ReplaceAll(escapeHTML(t.value), "--&gt;", "-&gt;"))
			case t.kind == tokBreak:
				b.WriteByte('\n')
			case t.kind == tokOpen && t.isColor():
				name := colorName(t.value)
				colors = append(colors, name != "")
				if name != "" {
					b.WriteString("<c." + name + ">")
				}
			case t.kind == tokOpen:
				b.WriteString("<" + t.value + ">")
			case t.value == "font":
				if len(colors) > 0 {
					top := colors[len(colors)-1]
					colors = colors[:len(colors)-1]
					if top {
						b.WriteString("</c>")
					}
				}
			default:
				b.WriteString("</" + t.value + ">")
			}
		}
		b.WriteString("\n\n")
	}
	return b.String()
}

// writeAss writes Advanced SubStation Alpha for a 1920 × 1080 picture: one
// "Default" style (white Arial with a black outline at the bottom centre),
// markup as override codes.
func writeAss(d *Document) string {
	var b strings.Builder
	b.WriteString("[Script Info]\r\n")
	if d.Title != "" {
		b.WriteString("Title: " + strings.ReplaceAll(d.Title, "\n", " ") + "\r\n")
	}
	b.WriteString("ScriptType: v4.00+\r\nWrapStyle: 0\r\nScaledBorderAndShadow: yes\r\nPlayResX: 1920\r\nPlayResY: 1080\r\n\r\n")
	b.WriteString("[V4+ Styles]\r\n")
	b.WriteString("Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\r\n")
	b.WriteString("Style: Default,Arial,64,&H00FFFFFF,&H000000FF,&H00000000,&H80000000,0,0,0,0,100,100,0,0,1,3,1,2,60,60,50,1\r\n\r\n")
	b.WriteString("[Events]\r\n")
	b.WriteString("Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\r\n")
	for _, c := range d.Cues {
		b.WriteString("Dialogue: 0," + centisecondsTime(c.Start) + "," + centisecondsTime(c.End) + ",Default,,0,0,0,," + AssText(c.Text) + "\r\n")
	}
	return b.String()
}

// AssText is cue markup as ASS text: \N between lines, {\i1}…{\i0}, colours as &HBBGGRR&.
func AssText(text string) string {
	var b strings.Builder
	var colors []string
	for _, t := range tokens(text) {
		switch {
		case t.kind == tokText:
			// A brace would start an override block.
			b.WriteString(strings.NewReplacer("{", "(", "}", ")").Replace(t.value))
		case t.kind == tokBreak:
			b.WriteString(`\N`)
		case t.kind == tokOpen && t.isColor():
			colors = append(colors, t.value)
			b.WriteString(`{\c` + assHex(t.value) + "}")
		case t.kind == tokOpen:
			b.WriteString(`{\` + t.value + "1}")
		case t.value == "font":
			// Back to the colour around it, or with a bare \c to the style's colour.
			if len(colors) > 0 {
				colors = colors[:len(colors)-1]
			}
			back := ""
			if len(colors) > 0 {
				back = assHex(colors[len(colors)-1])
			}
			b.WriteString(`{\c` + back + "}")
		default:
			b.WriteString(`{\` + t.value + "0}")
		}
	}
	return strings.ReplaceAll(b.String(), "}{", "")
}

func assHex(hex string) string { return "&H" + hex[5:7] + hex[3:5] + hex[1:3] + "&" }

// writeSami writes SAMI with one language class (KRCC for Korean, ENCC for
// English, …): a SYNC per cue and an &nbsp; SYNC where the screen goes blank
// before the next one.
func writeSami(d *Document) string {
	lang := d.GuessLanguage()
	class, name := samiClass(lang)
	var b strings.Builder
	b.WriteString("<SAMI>\r\n<HEAD>\r\n<TITLE>" + escapeHTML(d.Title) + "</TITLE>\r\n")
	b.WriteString("<STYLE TYPE=\"text/css\">\r\n<!--\r\n")
	b.WriteString("P { margin-left: 8pt; margin-right: 8pt; margin-bottom: 2pt; margin-top: 2pt; text-align: center; font-size: 20pt; font-family: sans-serif; font-weight: normal; color: white; }\r\n")
	b.WriteString("." + class + " { Name: " + name + "; lang: " + lang + "; SAMIType: CC; }\r\n")
	b.WriteString("-->\r\n</STYLE>\r\n</HEAD>\r\n<BODY>\r\n")
	for i, c := range d.Cues {
		fmt.Fprintf(&b, "<SYNC Start=%d><P Class=%s>", roundEven(totalMs(c.Start)), class)
		b.WriteString(htmlMarkup(c.Text, "<br>") + "\r\n")
		if i+1 >= len(d.Cues) || d.Cues[i+1].Start > c.End {
			fmt.Fprintf(&b, "<SYNC Start=%d><P Class=%s>&nbsp;\r\n", roundEven(totalMs(c.End)), class)
		}
	}
	b.WriteString("</BODY>\r\n</SAMI>\r\n")
	return b.String()
}

// samiClass is the usual SAMI class of a language ("ko-KR" → KRCC, "Korean").
func samiClass(lang string) (string, string) {
	code := strings.ToLower(strings.Split(lang, "-")[0])
	switch code {
	case "ko":
		return "KRCC", "Korean"
	case "en":
		return "ENCC", "English"
	case "ja":
		return "JPCC", "Japanese"
	case "zh":
		return "ZHCC", "Chinese"
	}
	name := code
	if t, err := language.Parse(code); err == nil {
		if n := display.English.Languages().Name(t); n != "" {
			name = n
		}
	}
	return strings.ToUpper(code) + "CC", name
}

// htmlMarkup is cue markup as HTML (SAMI): tags as they are, text escaped, lineBreak between lines.
func htmlMarkup(text, lineBreak string) string {
	var b strings.Builder
	for _, t := range tokens(text) {
		switch {
		case t.kind == tokText:
			b.WriteString(escapeHTML(t.value))
		case t.kind == tokBreak:
			b.WriteString(lineBreak)
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

// writeLrc writes one [mm:ss.xx] line per cue (its lines joined), plain text,
// and an empty time line where the screen goes blank before the next cue.
func writeLrc(d *Document) string {
	var b strings.Builder
	if d.Title != "" {
		b.WriteString("[ti:" + strings.ReplaceAll(strings.ReplaceAll(d.Title, "\n", " "), "]", ")") + "]\r\n")
	}
	for i, c := range d.Cues {
		parts := strings.Split(PlainText(c.Text), "\n")
		for j := range parts {
			parts[j] = strings.TrimSpace(parts[j])
		}
		b.WriteString("[" + lrcTime(c.Start) + "]" + strings.Join(parts, " ") + "\r\n")
		if i+1 >= len(d.Cues) || d.Cues[i+1].Start-c.End >= 10*time.Millisecond {
			b.WriteString("[" + lrcTime(c.End) + "]\r\n")
		}
	}
	return b.String()
}

func xmlAttr(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "\n", "&#xA;", "\r", "&#xD;", "\t", "&#x9;").Replace(s)
}

// writeTtml writes TTML 1: a bottom region, one p per cue, spans for italic /
// bold / underline / colour.
func writeTtml(d *Document) string {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString(`<tt xmlns:tts="http://www.w3.org/ns/ttml#styling" xmlns:ttm="http://www.w3.org/ns/ttml#metadata" xml:lang="` + xmlAttr(d.GuessLanguage()) + `" xmlns="http://www.w3.org/ns/ttml">` + "\n")
	b.WriteString("  <head>\n")
	if d.Title != "" {
		b.WriteString("    <metadata>\n      <ttm:title>" + escapeHTML(d.Title) + "</ttm:title>\n    </metadata>\n")
	}
	b.WriteString("    <styling>\n      <style xml:id=\"default\" tts:color=\"white\" tts:fontFamily=\"proportionalSansSerif\" tts:textAlign=\"center\" />\n    </styling>\n")
	b.WriteString("    <layout>\n      <region xml:id=\"bottom\" tts:origin=\"10% 75%\" tts:extent=\"80% 20%\" tts:displayAlign=\"after\" tts:textAlign=\"center\" />\n    </layout>\n")
	b.WriteString("  </head>\n")
	b.WriteString("  <body region=\"bottom\" style=\"default\">\n    <div>\n")
	for _, c := range d.Cues {
		// Written as one piece: indenting would put line breaks (spaces when read) before spans.
		b.WriteString(`      <p begin="` + millisecondsTime(c.Start, '.') + `" end="` + millisecondsTime(c.End, '.') + `">` + ttmlMarkup(c.Text) + "</p>\n")
	}
	b.WriteString("    </div>\n  </body>\n</tt>\n")
	return b.String()
}

// ttmlMarkup is the content of a TTML p: escaped text, br and spans with tts: styles.
func ttmlMarkup(text string) string {
	var b strings.Builder
	for _, t := range tokens(text) {
		switch t.kind {
		case tokText:
			b.WriteString(escapeHTML(t.value))
		case tokBreak:
			b.WriteString("<br />")
		case tokOpen:
			switch t.value {
			case "i":
				b.WriteString(`<span tts:fontStyle="italic">`)
			case "b":
				b.WriteString(`<span tts:fontWeight="bold">`)
			case "u":
				b.WriteString(`<span tts:textDecoration="underline">`)
			default:
				b.WriteString(`<span tts:color="` + t.value + `">`)
			}
		default:
			b.WriteString("</span>")
		}
	}
	return b.String()
}

// writeMicroDvd writes MicroDVD at MicroDvdFps (stated in the first line, as
// players expect): plain text with | between lines; a cue that is italic
// throughout gets {Y:i}.
func writeMicroDvd(d *Document) string {
	var b strings.Builder
	b.WriteString("{1}{1}" + strconv.FormatFloat(MicroDvdFps, 'f', -1, 64) + "\r\n")
	for _, c := range d.Cues {
		start := roundEven(c.Start.Seconds() * MicroDvdFps)
		end := max(start+1, roundEven(c.End.Seconds()*MicroDvdFps))
		ts := tokens(c.Text)
		italics := 0
		for _, t := range ts {
			if t.value == "i" {
				italics++
			}
		}
		italic := len(ts) > 2 && ts[0].kind == tokOpen && ts[0].value == "i" && ts[len(ts)-1].kind == tokClose && ts[len(ts)-1].value == "i" && italics == 2
		text := strings.Join(strings.Split(PlainText(c.Text), "\n"), "|")
		code := ""
		if italic {
			code = "{Y:i}"
		}
		fmt.Fprintf(&b, "{%d}{%d}%s%s\r\n", start, end, code, strings.NewReplacer("{", "(", "}", ")").Replace(text))
	}
	return b.String()
}
