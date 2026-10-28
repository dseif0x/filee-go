package subtitles

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SAMI (.smi), Microsoft's HTML-like subtitle format and the usual one for
// Korean subtitles. Files are hand-made and rarely valid HTML: <SYNC> and <P>
// are seldom closed, attribute values are unquoted, and one file may hold
// several languages as classes (KRCC, ENCC, …). The text is read with regular
// expressions, not an HTML parser.

var (
	reSamiTitle     = regexp.MustCompile(`(?is)<title[^>]*>(?P<text>.*?)</title>`)
	reSamiBodyStart = regexp.MustCompile(`(?i)<body[^>]*>`)
	reSamiBodyEnd   = regexp.MustCompile(`(?is)</body\s*>.*|</sami\s*>.*`)
	reSamiComment   = regexp.MustCompile(`(?s)<!--.*?-->`)
	reSamiSync      = regexp.MustCompile(`(?i)<sync\b[^>]*?start\s*=\s*["']?(?P<start>-?\d+)[^>]*>`)
	reSamiP         = regexp.MustCompile(`(?i)<p\b(?P<attributes>[^>]*)>`)
	reSamiClass     = regexp.MustCompile(`(?i)class\s*=\s*["']?(?P<name>[\w-]+)`)
	reSamiLang      = regexp.MustCompile(`(?i)lang\s*:\s*(?P<value>[A-Za-z]{2,3}(?:-[A-Za-z0-9]+)*)`)
	reWhitespace    = regexp.MustCompile(`\s+`)
	reLooseNbsp     = regexp.MustCompile(`(?i)&nbsp;?`)
)

// readSami reads every <SYNC Start=ms>: its text lasts until the next SYNC,
// and a SYNC with only &nbsp; clears the screen. Of several languages, the one
// with the most lines is read (the first one on a tie).
func readSami(text string) *Document {
	d := &Document{}
	if m := reSamiTitle.FindStringSubmatch(text); m != nil {
		if name := strings.TrimSpace(htmlDecode(m[1])); name != "" {
			d.Title = name
		}
	}
	// Body only: the <STYLE> block's comment would otherwise look like text.
	body := text
	if loc := reSamiBodyStart.FindStringIndex(text); loc != nil {
		body = text[loc[1]:]
	}
	body = reSamiComment.ReplaceAllString(body, "")
	syncs := reSamiSync.FindAllStringSubmatchIndex(body, -1)
	type entry struct {
		at    time.Duration
		class string
		text  string
	}
	var entries []entry
	for i, s := range syncs {
		ms, err := strconv.Atoi(body[s[2]:s[3]])
		if err != nil {
			continue
		}
		contentEnd := len(body)
		if i+1 < len(syncs) {
			contentEnd = syncs[i+1][0]
		}
		content := reSamiBodyEnd.ReplaceAllString(body[s[1]:contentEnd], "")
		at := time.Duration(max(0, ms)) * time.Millisecond
		ps := reSamiP.FindAllStringSubmatchIndex(content, -1)
		if len(ps) == 0 {
			entries = append(entries, entry{at, "", samiText(content)})
			continue
		}
		for p, m := range ps {
			end := len(content)
			if p+1 < len(ps) {
				end = ps[p+1][0]
			}
			class := ""
			if c := reSamiClass.FindStringSubmatch(content[m[2]:m[3]]); c != nil {
				class = strings.ToUpper(c[1])
			}
			entries = append(entries, entry{at, class, samiText(content[m[1]:end])})
		}
	}

	// The language with the most lines; lines without a class belong to every language.
	counts := map[string]int{}
	var order []string
	for _, e := range entries {
		if e.class != "" && strings.TrimSpace(PlainText(e.text)) != "" {
			if counts[e.class] == 0 {
				order = append(order, e.class)
			}
			counts[e.class]++
		}
	}
	chosen := ""
	for _, c := range order {
		if chosen == "" || counts[c] > counts[chosen] {
			chosen = c
		}
	}
	var lines []entry
	for _, e := range entries {
		if e.class == "" || e.class == chosen {
			lines = append(lines, e)
		}
	}
	sortStable(len(lines), func(i, j int) bool { return lines[i].at < lines[j].at }, func(i, j int) { lines[i], lines[j] = lines[j], lines[i] })
	for i, l := range lines {
		end := l.at + DefaultDuration
		if i+1 < len(lines) {
			end = lines[i+1].at
		}
		// Two entries at the same time: the later one wins.
		if end == l.at && i+1 < len(lines) {
			continue
		}
		d.Add(l.at, end, l.text)
	}
	if chosen != "" {
		if lang := samiLanguageOf(text, chosen); lang != "" {
			d.Language = lang
		}
	}
	return d
}

// samiText is a paragraph's text as cue markup: HTML whitespace rules (source
// line breaks are spaces), <br> breaks. Hand-written files clear the screen
// with &nbsp without the semicolon, or in capitals.
func samiText(h string) string {
	return fromHTML(reWhitespace.ReplaceAllString(reLooseNbsp.ReplaceAllString(h, "&nbsp;"), " "), true)
}

// samiLanguageOf is the lang: of a class in the style sheet: .KRCC { Name: Korean; lang: ko-KR; }.
func samiLanguageOf(text, class string) string {
	re := regexp.MustCompile(`(?i)\.` + regexp.QuoteMeta(class) + `\s*\{(?P<rule>[^}]*)\}`)
	m := re.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	if l := reSamiLang.FindStringSubmatch(m[1]); l != nil {
		return l[1]
	}
	return ""
}

// sortStable is an insertion sort (stable, for the few entries a file has).
func sortStable(n int, less func(i, j int) bool, swap func(i, j int)) {
	for i := 1; i < n; i++ {
		for j := i; j > 0 && less(j, j-1); j-- {
			swap(j, j-1)
		}
	}
}
