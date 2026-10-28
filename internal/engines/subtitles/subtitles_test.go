package subtitles

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"

	"github.com/dseif0x/filee-go/internal/convert"
)

func ms(n int) time.Duration { return time.Duration(n) * time.Millisecond }

func read(t *testing.T, format, text string) *Document {
	t.Helper()
	d, err := Read(format, []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func write(t *testing.T, d *Document, format string) string {
	t.Helper()
	b, err := Write(d, format)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimPrefix(string(b), utf8BOM)
}

func cues(t *testing.T, want []Cue, got []Cue) {
	t.Helper()
	if !slices.Equal(want, got) {
		t.Fatalf("want\n%q\ngot\n%q", want, got)
	}
}

func lines(l ...string) string { return strings.Join(l, "\n") }

func cp949(s string) []byte { b, _ := korean.EUCKR.NewEncoder().Bytes([]byte(s)); return b }

func TestSubtitlesConvertInOneBuiltInStep(t *testing.T) {
	p := convert.NewPlanner([]convert.Converter{New()}, nil)
	for _, r := range [][2]string{{"srt", "smi"}, {"smi", "srt"}, {"smi", "smi"}, {"ass", "vtt"}, {"ssa", "srt"}, {"vtt", "ttml"}, {"lrc", "srt"}, {"ttml", "srt"}, {"sub", "srt"}, {"scc", "vtt"}} {
		route := p.Plan(r[0], r[1], convert.Options{})
		if route == nil || len(route.Steps) != 1 || route.Steps[0].Converter.ID() != "subtitle" {
			t.Fatalf("%v: %+v", r, route)
		}
	}
	if n := len(New().Edges()); n != 9*7 {
		t.Fatal(n)
	}
}

func TestKoreanCP949IsRecognisedAndOtherCodePagesAreNotTakenForIt(t *testing.T) {
	const ko = "안녕하세요. 오늘 날씨가 좋네요! 똠방각하"
	if !LooksKorean(cp949(ko)) {
		t.Fatal("korean")
	}
	for _, cp := range []int{1252, 936} {
		if got := DecodeWith(cp949(ko), cp, true); got != ko {
			t.Fatal(cp, got)
		}
	}
	const zh = "我是一个学生，这是我的字幕文件。你好吗？"
	gbk, _ := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(zh))
	if LooksKorean(gbk) || DecodeWith(gbk, 936, true) != zh {
		t.Fatal("chinese")
	}
	const fr = "Café, déjà vu à l'été"
	latin, _ := charmap.Windows1252.NewEncoder().Bytes([]byte(fr))
	if LooksKorean(latin) || DecodeWith(latin, 1252, true) != fr || DecodeWith(latin, 949, true) != fr {
		t.Fatal("french")
	}
	if Decode(append([]byte{0xEF, 0xBB, 0xBF}, "가나"...)) != "가나" {
		t.Fatal("bom")
	}
	u16 := func(s string, bom bool) []byte {
		var b []byte
		if bom {
			b = []byte{0xFF, 0xFE}
		}
		for _, c := range utf16.Encode([]rune(s)) {
			b = append(b, byte(c), byte(c>>8))
		}
		return b
	}
	if Decode(u16("가나", true)) != "가나" || Decode(u16("Hi there", false)) != "Hi there" {
		t.Fatal("utf-16")
	}
}

func TestACP949SamiFileBecomesAUTF8SrtWithTheKoreanClass(t *testing.T) {
	dir := t.TempDir()
	smi := filepath.Join(dir, "영화.smi")
	os.WriteFile(smi, cp949(lines(
		"<SAMI>",
		"<HEAD>",
		"<TITLE>영화 자막</TITLE>",
		`<STYLE TYPE="text/css">`,
		"<!--",
		"P { margin-left:8pt; margin-right:8pt; text-align:center; }",
		".KRCC { Name:한국어; lang:ko-KR; SAMIType:CC; }",
		".ENCC { Name:English; lang:en-US; SAMIType:CC; }",
		"-->",
		"</STYLE>",
		"</HEAD>",
		"<BODY>",
		`<SYNC Start=1000><P Class=KRCC>안녕하세요,<br><font color="#ffff00">반가워요</font>`,
		"<SYNC Start=1000><P Class=ENCC>Hello",
		"<SYNC Start=2500><P Class=KRCC>&nbsp;",
		"<SYNC Start=3000><P Class=KRCC><i>두 번째",
		"줄입니다</i>",
		"<SYNC Start=4200><P Class=KRCC>&nbsp",
		"<SYNC Start=5000><P Class=KRCC>세 번째 &amp; 마지막",
		"</BODY>",
		"</SAMI>")), 0o644)
	step := &convert.Step{InputPath: smi, From: "smi", To: "srt", Output: convert.AllocatorFunc(func(ext, _ string) string { return filepath.Join(dir, "out."+ext) })}
	outs, err := New().Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(outs[0])
	if !strings.HasPrefix(string(b), utf8BOM) {
		t.Fatal("BOM")
	}
	want := strings.ReplaceAll(lines(
		"1",
		"00:00:01,000 --> 00:00:02,500",
		"안녕하세요,",
		`<font color="#FFFF00">반가워요</font>`,
		"",
		"2",
		"00:00:03,000 --> 00:00:04,200",
		"<i>두 번째 줄입니다</i>",
		"",
		"3",
		"00:00:05,000 --> 00:00:09,000",
		"세 번째 & 마지막",
		"",
		""), "\n", "\r\n")
	if got := string(b[3:]); got != want {
		t.Fatalf("%q\n%q", want, got)
	}
}

func TestSrtIsReadLeniently(t *testing.T) {
	d := read(t, "srt", "\uFEFF1\r\n00:00:01,000 --> 00:00:02,000\r\n<i>First</i> line\r\n\r\nstill the first\r\n\r\n2\r\n"+
		"00:00:03.5 --> 00:00:04,250\r\n{\\an8}<b>Second</b> <span>cue</span>\r\n3\r\n00:00:05,000 --> 00:00:06,000\r\nThird 2024\r\n")
	cues(t, []Cue{
		{ms(1000), ms(2000), "<i>First</i> line\nstill the first"},
		{ms(3500), ms(4250), "<b>Second</b> cue"},
		{ms(5000), ms(6000), "Third 2024"},
	}, d.Cues)
}

func TestWebVttCuesSettingsEntitiesAndVoices(t *testing.T) {
	d := read(t, "vtt", lines(
		"WEBVTT",
		"Language: ko",
		"",
		"NOTE This is a comment",
		"with two lines",
		"",
		"STYLE",
		"::cue { color: yellow }",
		"",
		"intro",
		"00:01.000 --> 00:02.500 align:start position:10%",
		"<v Mary>Hi &amp; <i>welcome</i></v>",
		"<c.yellow>&lt;3</c>",
		"",
		"01:00:00.000 --> 01:00:01.000",
		"<00:00:01.500>Karaoke"))
	if d.Language != "ko" {
		t.Fatal(d.Language)
	}
	cues(t, []Cue{
		{ms(1000), ms(2500), "Hi & <i>welcome</i>\n<font color=\"#FFFF00\"><3</font>"},
		{time.Hour, time.Hour + ms(1000), "Karaoke"},
	}, d.Cues)
}

func TestAssDialogueOverrideCodesAndStyles(t *testing.T) {
	d := read(t, "ass", lines(
		"[Script Info]",
		"Title: Test show",
		"ScriptType: v4.00+",
		"",
		"[V4+ Styles]",
		"Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding",
		"Style: Default,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,2,2,2,10,10,10,1",
		"Style: Thoughts,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,-1,0,0,100,100,0,0,1,2,2,2,10,10,10,1",
		"",
		"[Events]",
		"Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text",
		`Dialogue: 0,0:00:02.00,0:00:03.50,Default,,0,0,0,,{\an8\pos(10,10)}Hello, {\i1}world{\i0}!\NSecond {\c&H00FFFF&}yellow{\c} line`,
		"Comment: 0,0:00:02.00,0:00:03.50,Default,,0,0,0,,Not shown",
		`Dialogue: 0,0:00:01.00,0:00:01.50,Thoughts,,0,0,0,,I think\h so`,
		`Dialogue: 1,0:00:01.00,0:00:01.50,Thoughts,,0,0,0,,I think\h so`,
		`Dialogue: 0,0:00:04.00,0:00:05.00,Default,,0,0,0,,{\p1}m 0 0 l 100 0 100 100 0 100{\p0}`))
	if d.Title != "Test show" {
		t.Fatal(d.Title)
	}
	cues(t, []Cue{
		{ms(1000), ms(1500), "<i>I think so</i>"},
		{ms(2000), ms(3500), "Hello, <i>world</i>!\nSecond <font color=\"#FFFF00\">yellow</font> line"},
	}, d.Cues)
}

func TestLrcLinesLastUntilTheNextOne(t *testing.T) {
	d := read(t, "lrc", lines(
		"[ti:Song]",
		"[ar:Singer]",
		"[offset:+500]",
		"[00:10.00]First <00:10.50>line",
		"[00:12.30][00:20.00]Chorus",
		"[00:15.00]",
		"[00:25.5]Last"))
	if d.Title != "Song" {
		t.Fatal(d.Title)
	}
	cues(t, []Cue{
		{ms(9500), ms(11800), "First line"},
		{ms(11800), ms(14500), "Chorus"},
		{ms(19500), ms(25000), "Chorus"},
		{ms(25000), ms(29000), "Last"},
	}, d.Cues)
}

func TestTtmlTimesSpansAndStyles(t *testing.T) {
	d := read(t, "ttml", lines(
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<tt xmlns="http://www.w3.org/ns/ttml" xmlns:tts="http://www.w3.org/ns/ttml#styling"`,
		`    xmlns:ttp="http://www.w3.org/ns/ttml#parameter" xml:lang="en" ttp:frameRate="25" ttp:tickRate="10000000">`,
		`  <head><styling><style xml:id="it" tts:fontStyle="italic"/></styling></head>`,
		`  <body>`,
		`    <div begin="10s">`,
		`      <p begin="00:00:01.000" end="00:00:02.000">Line one<br/>line <span tts:fontWeight="bold">two</span></p>`,
		`      <p begin="00:00:03:05" dur="25f" style="it">Frames</p>`,
		`      <p begin="50000000t" end="60000000t"><span tts:color="#ff0000">Red</span> ticks</p>`,
		`    </div>`,
		`  </body>`,
		`</tt>`))
	if d.Language != "en" {
		t.Fatal(d.Language)
	}
	cues(t, []Cue{
		{ms(11000), ms(12000), "Line one\nline <b>two</b>"},
		{ms(13200), ms(14200), "<i>Frames</i>"},
		{ms(15000), ms(16000), "<font color=\"#FF0000\">Red</font> ticks"},
	}, d.Cues)
}

func TestMicroDvdFramesUseTheRateInTheFirstLine(t *testing.T) {
	d := read(t, "sub", "{1}{1}25\n{25}{50}{y:i}Hello|world\n{75}{100}{c:$0000FF}Red\n")
	cues(t, []Cue{
		{ms(1000), ms(2000), "<i>Hello</i>\nworld"},
		{ms(3000), ms(4000), "<font color=\"#FF0000\">Red</font>"},
	}, d.Cues)
	if _, err := Read("sub", []byte{0, 0, 1, 0xBA, 0x44, 0}); err == nil || !strings.Contains(err.Error(), "VobSub") {
		t.Fatal(err)
	}
	if _, err := Read("sub", []byte("[INFORMATION]\n00:00:01.00,00:00:02.00\nSubViewer")); err == nil || !strings.Contains(err.Error(), "MicroDVD") {
		t.Fatal(err)
	}
}

func TestSccPopOnAndRollUpCaptions(t *testing.T) {
	d := read(t, "scc", lines(
		"Scenarist_SCC V1.0",
		"",
		"00:00:01:00\t9420 9420 94ae 94ae 9452 9452 c8e5 ecec ef80 9470 9470 4361 665c 942f 942f",
		"",
		"00:00:03:00\t942c 942c",
		"",
		"00:00:05:00\t9425 9425 9470 9470 4f6e 6521 94ad 94ad 5477 6f21",
		"",
		"00:00:07:00\t942c 942c"))
	frame := 1001.0 / 30000
	near := func(want float64, got time.Duration) {
		t.Helper()
		if math.Abs(want-got.Seconds()) > 0.0005 {
			t.Fatalf("%v, want %v", got.Seconds(), want)
		}
	}
	if len(d.Cues) != 3 {
		t.Fatalf("%q", d.Cues)
	}
	if d.Cues[0].Text != "Hello\nCafé" {
		t.Fatalf("%q", d.Cues[0].Text)
	}
	near(30*frame+13*frame, d.Cues[0].Start)
	near(90*frame, d.Cues[0].End)
	if d.Cues[1].Text != "One!" || d.Cues[2].Text != "One!\nTwo!" {
		t.Fatalf("%q", d.Cues)
	}
	near(210*frame, d.Cues[2].End)
	if _, err := Read("scc", []byte("00:00:01:00 9420")); err == nil || !strings.Contains(err.Error(), "SCC") {
		t.Fatal(err)
	}
}

func TestEmptyFilesFailWithAClearMessage(t *testing.T) {
	if _, err := Read("srt", []byte("\n\n")); err == nil || !strings.Contains(err.Error(), "No subtitles") {
		t.Fatal(err)
	}
}

func sample() *Document {
	d := &Document{Title: "Sample"}
	d.Add(ms(1000), ms(2500), "<i>Hello</i> & <b>bye</b>\n<font color=\"#FFFF00\">yellow</font> 1 < 2")
	d.Add(ms(2500), ms(4000), "다음 줄")
	d.Add(ms(6000), ms(7000), "Last one")
	return d.Finish()
}

func TestEveryFormatWritesTheCuesWithItsOwnMarkup(t *testing.T) {
	d := sample()
	srt := write(t, d, "srt")
	if !strings.Contains(srt, "00:00:01,000 --> 00:00:02,500\r\n<i>Hello</i> & <b>bye</b>\r\n<font color=\"#FFFF00\">yellow</font> 1 < 2") {
		t.Fatal(srt)
	}
	if vtt := write(t, d, "vtt"); !strings.HasPrefix(vtt, "WEBVTT\n\n00:00:01.000 --> 00:00:02.500\n<i>Hello</i> &amp; <b>bye</b>\n<c.yellow>yellow</c> 1 &lt; 2\n\n") {
		t.Fatal(vtt)
	}
	ass := write(t, d, "ass")
	if !strings.Contains(ass, "Title: Sample") || !strings.Contains(ass, `Dialogue: 0,0:00:01.00,0:00:02.50,Default,,0,0,0,,{\i1}Hello{\i0} & {\b1}bye{\b0}\N{\c&H00FFFF&}yellow{\c} 1 < 2`) {
		t.Fatal(ass)
	}
	if AssText("a {b}") != "a (b)" {
		t.Fatal(AssText("a {b}"))
	}
	sami := write(t, d, "smi")
	for _, s := range []string{".KRCC { Name: Korean; lang: ko-KR; SAMIType: CC; }",
		"<SYNC Start=1000><P Class=KRCC><i>Hello</i> &amp; <b>bye</b><br><font color=\"#FFFF00\">yellow</font> 1 &lt; 2\r\n<SYNC Start=2500>",
		"<SYNC Start=4000><P Class=KRCC>&nbsp;\r\n<SYNC Start=6000>"} {
		if !strings.Contains(sami, s) {
			t.Fatal(sami)
		}
	}
	if lrc := write(t, d, "lrc"); lrc != "[ti:Sample]\r\n[00:01.00]Hello & bye yellow 1 < 2\r\n[00:02.50]다음 줄\r\n[00:04.00]\r\n[00:06.00]Last one\r\n[00:07.00]\r\n" {
		t.Fatal(lrc)
	}
	if ttml := write(t, d, "ttml"); !strings.Contains(ttml, `<p begin="00:00:01.000" end="00:00:02.500"><span tts:fontStyle="italic">Hello</span> &amp; <span tts:fontWeight="bold">bye</span><br /><span tts:color="#FFFF00">yellow</span> 1 &lt; 2</p>`) {
		t.Fatal(ttml)
	}
	if sub := write(t, d, "sub"); !strings.HasPrefix(sub, "{1}{1}23.976\r\n{24}{60}Hello & bye|yellow 1 < 2\r\n") {
		t.Fatal(sub)
	}
}

func TestFormatsWithStylesReadBackWhatTheyWrite(t *testing.T) {
	for _, f := range []string{"srt", "vtt", "ass", "smi", "ttml"} {
		d := sample()
		b, _ := Write(d, f)
		back, err := Read(f, b)
		if err != nil {
			t.Fatal(f, err)
		}
		if !slices.Equal(d.Cues, back.Cues) {
			t.Fatalf("%s:\n%q\n%q", f, d.Cues, back.Cues)
		}
	}
}

func TestPlainFormatsReadBackTheTextAndTimes(t *testing.T) {
	for _, f := range []string{"lrc", "sub"} {
		d := sample()
		b, _ := Write(d, f)
		back, err := Read(f, b)
		if err != nil {
			t.Fatal(f, err)
		}
		if len(back.Cues) != len(d.Cues) {
			t.Fatalf("%s: %q", f, back.Cues)
		}
		tolerance := 10.0
		if f == "sub" {
			tolerance = 45
		}
		for i, c := range d.Cues {
			want := PlainText(c.Text)
			if f == "lrc" {
				want = strings.ReplaceAll(want, "\n", " ")
			}
			if back.Cues[i].Text != want {
				t.Fatalf("%s: %q, want %q", f, back.Cues[i].Text, want)
			}
			if math.Abs(float64(c.Start-back.Cues[i].Start)/1e6) > tolerance {
				t.Fatalf("%s: %v vs %v", f, c.Start, back.Cues[i].Start)
			}
		}
	}
}
