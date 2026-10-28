package email

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/korean"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/presets"
)

var report = []byte("%PDF-1.4 fake report")

func pngOf(w, h int, c color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func eucKR(s string) []byte {
	b, err := korean.EUCKR.NewEncoder().Bytes([]byte(s))
	if err != nil {
		panic(err)
	}
	return b
}

func encodedWord(text string) string {
	return "=?EUC-KR?B?" + base64.StdEncoding.EncodeToString(eucKR(text)) + "?="
}

// lines is base64 in lines without the trailing line break.
func lines(b []byte) string { return strings.TrimSuffix(base64Lines(b), "\r\n") }

// quotedPrintable of bytes (every non-printable byte as =XX), lines under 76.
func quotedPrintable(b []byte, keepNewlines bool) string {
	var sb strings.Builder
	col := 0
	for _, c := range b {
		if keepNewlines && c == '\n' {
			sb.WriteString("\r\n")
			col = 0
			continue
		}
		piece := string(rune(c))
		if !(c >= 33 && c <= 126 && c != '=' || keepNewlines && c == ' ') {
			piece = fmt.Sprintf("=%02X", c)
		}
		if col+len(piece) > 72 {
			sb.WriteString("=\r\n")
			col = 0
		}
		sb.WriteString(piece)
		col += len(piece)
	}
	return sb.String()
}

var teal = color.RGBA{0, 128, 128, 255}

// sampleMessage is multipart/mixed: multipart/related (multipart/alternative
// text + HTML in EUC-KR, an inline PNG) and two attachments, one with an RFC
// 2231 UTF-8 file name.
func sampleMessage(t *testing.T) string {
	html := "<html><head><style>p { color: #C00000; }</style></head><body><p>안녕하세요. 자료를 보냅니다.</p>" +
		"<img src=\"cid:logo@filee\" alt=\"logo\"><script>alert('x')</script>" +
		"<img src=\"https://tracker.example/pixel\" onerror=\"alert('x')\">" +
		"<a href=\"javascript:alert('x')\">bad link</a><svg onload=\"alert('x')\"></svg>" +
		"<table bgcolor=\"#EEEEEE\"><tr><td valign=\"top\"><font color=\"#336699\">표 안</font></td></tr></table>" +
		"<form action=\"https://evil.example/\"><p>양식 안의 본문</p><input name=\"q\"></form>" +
		"<xmp><img src=x onerror=alert(1)></xmp><!-- <img src=x onerror=alert(2)> --></body></html>"
	eml := strings.Join([]string{
		"From: " + encodedWord("보낸이") + " <sender@example.com>",
		`To: receiver@example.com, "Second" <second@example.com>`,
		"Cc: other@example.com",
		"Subject: " + encodedWord("회의 자료"),
		"Date: Thu, 01 Oct 2026 09:30:00 +0900",
		"MIME-Version: 1.0",
		`Content-Type: multipart/mixed; boundary="mixed"`,
		"",
		"--mixed",
		`Content-Type: multipart/related; boundary="related"`,
		"",
		"--related",
		`Content-Type: multipart/alternative; boundary="alt"`,
		"",
		"--alt",
		"Content-Type: text/plain; charset=euc-kr",
		"Content-Transfer-Encoding: base64",
		"",
		lines(eucKR("안녕하세요. 자료를 보냅니다.\r\n둘째 줄")),
		"--alt",
		"Content-Type: text/html; charset=euc-kr",
		"Content-Transfer-Encoding: quoted-printable",
		"",
		quotedPrintable(eucKR(html), false),
		"--alt--",
		"--related",
		"Content-Type: image/png",
		"Content-ID: <logo@filee>",
		"Content-Transfer-Encoding: base64",
		"",
		lines(pngOf(16, 16, teal)),
		"--related--",
		"--mixed",
		`Content-Type: application/pdf; name="report.pdf"`,
		"Content-Disposition: attachment; filename*=UTF-8''%EB%B3%B4%EA%B3%A0%EC%84%9C.pdf",
		"Content-Transfer-Encoding: base64",
		"",
		lines(report),
		"--mixed",
		`Content-Type: text/plain; charset=utf-8; name="notes.txt"`,
		`Content-Disposition: attachment; filename="notes.txt"`,
		"",
		"memo",
		"--mixed--",
		"",
	}, "\r\n")
	p := filepath.Join(t.TempDir(), "회의.eml")
	os.WriteFile(p, []byte(eml), 0o644)
	return p
}

func convertTo(t *testing.T, input, from, to string) string {
	t.Helper()
	dir := t.TempDir()
	base := formats.NameWithoutExtension(input)
	s := &convert.Step{InputPath: input, From: from, To: to, Preset: presets.New("x", to), WorkDir: t.TempDir(),
		Output: convert.AllocatorFunc(func(ext, suffix string) string { return filepath.Join(dir, base+suffix+"."+ext) })}
	out, err := New().Convert(context.Background(), s, nil)
	if err != nil {
		t.Fatalf("%s → %s: %v", from, to, err)
	}
	return out[0]
}

func convertErr(t *testing.T, input, from, to string) error {
	t.Helper()
	dir := t.TempDir()
	s := &convert.Step{InputPath: input, From: from, To: to, Preset: presets.New("x", to), WorkDir: t.TempDir(),
		Output: convert.AllocatorFunc(func(ext, suffix string) string { return filepath.Join(dir, "out."+ext) })}
	_, err := New().Convert(context.Background(), s, nil)
	return err
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func has(t *testing.T, what, text string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Errorf("%s lacks %q\n%s", what, w, text)
		}
	}
}

func hasNot(t *testing.T, what, text string, unwanted ...string) {
	t.Helper()
	for _, w := range unwanted {
		if strings.Contains(text, w) {
			t.Errorf("%s contains %q", what, w)
		}
	}
}

func zipNames(t *testing.T, p string) ([]string, map[string][]byte) {
	t.Helper()
	z, err := zip.OpenReader(p)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	var names []string
	data := map[string][]byte{}
	for _, f := range z.File {
		names = append(names, f.Name)
		rc, _ := f.Open()
		data[f.Name], _ = io.ReadAll(rc)
		rc.Close()
	}
	return names, data
}

func TestRoutes(t *testing.T) {
	p := convert.NewPlanner([]convert.Converter{New()}, nil)
	if r := p.Plan("msg", "html", convert.Options{}); r == nil || len(r.Steps) != 1 {
		t.Fatal("MSG → HTML is direct")
	}
	for _, e := range New().Edges() {
		if formats.ByID(e.From) == nil || formats.ByID(e.To) == nil {
			t.Errorf("unknown format in %s → %s", e.From, e.To)
		}
	}
	if !doc.HasReader("mhtml") {
		t.Error("no MHTML document reader")
	}
}

func TestEmlToHtmlShowsHeadersAndTheHtmlBodyWithInlinePictures(t *testing.T) {
	html := read(t, convertTo(t, sampleMessage(t), "eml", "html"))
	has(t, "html", html, `<meta charset="utf-8">`, "<title>회의 자료</title>",
		"<th>From</th><td>보낸이 &lt;sender@example.com&gt;</td>",
		"<th>To</th><td>receiver@example.com, Second &lt;second@example.com&gt;</td>",
		"<th>Date</th><td>2026-10-01 09:30:00 +09:00</td>",
		"<th>Attachments</th><td>보고서.pdf, notes.txt</td>",
		"<p>안녕하세요. 자료를 보냅니다.</p>", "p { color: #C00000; }", `src="data:image/png;base64,`,
		`bgcolor="#EEEEEE"`, `valign="top"`, `<font color="#336699">`, "<p>양식 안의 본문</p>", "Content-Security-Policy")
	hasNot(t, "html", html, "cid:", "<script", "onerror", "onload", "tracker.example", "javascript:", "<svg", "<form", "<input", "<xmp", "<!--")
}

func TestEmlToTextAndAttachmentsToZip(t *testing.T) {
	eml := sampleMessage(t)
	text := strings.TrimPrefix(read(t, convertTo(t, eml, "eml", "txt")), "\uFEFF")
	if !strings.HasPrefix(text, "From: 보낸이 <sender@example.com>\r\n") || !strings.Contains(text, "Subject: 회의 자료\r\n") ||
		!strings.HasSuffix(text, "\r\n\r\n안녕하세요. 자료를 보냅니다.\r\n둘째 줄\r\n") {
		t.Fatalf("%q", text)
	}
	names, data := zipNames(t, convertTo(t, eml, "eml", "zip"))
	if !slices.Equal(names, []string{"보고서.pdf", "notes.txt"}) || !bytes.Equal(data["보고서.pdf"], report) {
		t.Fatalf("%v", names)
	}
}

func TestIso2022KrAndPlainTextMessages(t *testing.T) {
	enc := func(s string) string {
		// ESC $ ) C once, then SO … SI around the Korean bytes (KS X 1001 without the high bit).
		var b strings.Builder
		b.WriteString("\x1B$)C")
		shifted := false
		for _, r := range s {
			if r < 0x80 {
				if shifted {
					b.WriteByte(0x0F)
					shifted = false
				}
				b.WriteRune(r)
				continue
			}
			if !shifted {
				b.WriteByte(0x0E)
				shifted = true
			}
			for _, c := range eucKR(string(r)) {
				b.WriteByte(c & 0x7F)
			}
		}
		if shifted {
			b.WriteByte(0x0F)
		}
		return b.String()
	}
	subject := "=?ISO-2022-KR?B?" + base64.StdEncoding.EncodeToString([]byte(enc("한글 제목"))) + "?="
	body := enc("본문 <첫 줄>") + "\r\n" + enc("둘째 줄")
	p := filepath.Join(t.TempDir(), "old.eml")
	os.WriteFile(p, []byte("From: old@example.com\r\nSubject: "+subject+"\r\nContent-Type: text/plain; charset=ISO-2022-KR\r\nContent-Transfer-Encoding: 7bit\r\n\r\n"+body+"\r\n"), 0o644)
	html := strings.ReplaceAll(read(t, convertTo(t, p, "eml", "html")), "\r\n", "\n")
	has(t, "html", html, "<title>한글 제목</title>", "<pre class=\"filee-mail-text\">본문 &lt;첫 줄&gt;\n둘째 줄")
	hasNot(t, "html", html, "Attachments")
	if err := convertErr(t, p, "eml", "zip"); err == nil || !strings.Contains(err.Error(), "no attachments") {
		t.Fatal(err)
	}
}

// ---- MSG ----

func sampleMsg(t *testing.T) string {
	m := &msgBuilder{
		subject: "회의 자료", senderName: "보낸이", senderEmail: "sender@example.com",
		body:       ptr("안녕하세요. 자료를 보냅니다.\r\n둘째 줄"),
		html:       ptr(`<html><head><style>p { color: #C00000; }</style></head><body><p>안녕하세요. 자료를 보냅니다.</p><img src="cid:logo@filee"></body></html>`),
		messageID:  ptr("<meeting-1@example.com>"),
		recipients: []msgRecipient{{"Receiver", "receiver@example.com", 1}, {"Copy", "copy@example.com", 2}},
		attachments: []msgAttachment{
			{"보고서.pdf", report, "application/pdf", ""},
			{"logo.png", pngOf(16, 16, teal), "image/png", "logo@filee"},
		},
		attachedMessages: []*msgBuilder{{subject: "Earlier mail", senderName: "Old", senderEmail: "old@example.com", body: ptr("older text")}},
	}
	p := filepath.Join(t.TempDir(), "회의.msg")
	if err := m.save(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMsgToEmlRebuildsTheMimeMessage(t *testing.T) {
	eml := convertTo(t, sampleMsg(t), "msg", "eml")
	m, err := loadMessage(eml)
	if err != nil {
		t.Fatal(err)
	}
	from := m.addresses("From")
	if m.get("Subject") != "회의 자료" || len(from) != 1 || from[0].name != "보낸이" || from[0].address != "sender@example.com" {
		t.Fatalf("%q %v", m.get("Subject"), from)
	}
	if to, cc := m.addresses("To"), m.addresses("Cc"); len(to) != 1 || to[0].address != "receiver@example.com" || len(cc) != 1 || cc[0].address != "copy@example.com" {
		t.Fatalf("%v %v", to, cc)
	}
	if d, ok := m.date(); !ok || !d.UTC().Equal(d.UTC()) || d.UTC().Format("2006-01-02 15:04") != "2026-10-01 00:30" {
		t.Fatalf("date %v", d)
	}
	if id := m.contentIDOf("Message-Id"); id != "meeting-1@example.com" {
		t.Fatalf("id %q", id)
	}
	if text, _ := m.textBody(); strings.TrimRight(strings.ReplaceAll(text, "\r\n", "\n"), "\n") != "안녕하세요. 자료를 보냅니다.\n둘째 줄" {
		t.Fatalf("%q", text)
	}
	if h, _ := m.htmlBody(); !strings.Contains(h, `<img src="cid:logo@filee">`) {
		t.Fatal(h)
	}
	list := attachments(m)
	if len(list) != 2 || list[0].name != "보고서.pdf" || !bytes.Equal(list[0].p.body, report) || list[1].p.message == nil {
		t.Fatalf("%+v", list)
	}
	if s := list[1].p.message.get("Subject"); s != "Earlier mail" {
		t.Fatal(s)
	}
	if text, _ := list[1].p.message.textBody(); strings.TrimSpace(text) != "older text" {
		t.Fatal(text)
	}
	found := false
	for _, p := range m.bodyParts() {
		found = found || p.contentID() == "logo@filee"
	}
	if !found {
		t.Fatal("the logo is part of the body")
	}
}

func (p *part) contentIDOf(h string) string {
	return strings.Trim(strings.TrimSpace(p.header.Get(h)), "<>")
}

func TestMsgToHtmlTextAndZipDirectly(t *testing.T) {
	msg := sampleMsg(t)
	html := read(t, convertTo(t, msg, "msg", "html"))
	has(t, "html", html, "<title>회의 자료</title>", "<th>From</th><td>보낸이 &lt;sender@example.com&gt;</td>",
		"<th>Cc</th><td>Copy &lt;copy@example.com&gt;</td>", "<th>Attachments</th><td>보고서.pdf, Earlier mail.eml</td>", `src="data:image/png;base64,`)
	hasNot(t, "html", html, "cid:")
	text := read(t, convertTo(t, msg, "msg", "txt"))
	if !strings.Contains(text, "Subject: 회의 자료\r\n") || !strings.HasSuffix(text, "\r\n\r\n안녕하세요. 자료를 보냅니다.\r\n둘째 줄\r\n") {
		t.Fatalf("%q", text)
	}
	names, data := zipNames(t, convertTo(t, msg, "msg", "zip"))
	if !slices.Equal(names, []string{"보고서.pdf", "Earlier mail.eml"}) || !strings.Contains(string(data["Earlier mail.eml"]), "older text") {
		t.Fatal(names)
	}
}

func TestMsgWithRtfOnlyBodyGivesItsHtml(t *testing.T) {
	rtf := []byte(`{\rtf1\ansi\ansicpg1252\fromhtml1 \deff0{\fonttbl{\f0\fswiss Arial;}}{\*\htmltag19 <html>}{\*\htmltag34 <body>}\htmlrtf {\htmlrtf0 {\*\htmltag64 <p>}Caf\'e9 \u54620?\u44544? RTF{\*\htmltag72 </p>}}\htmlrtf }{\*\htmltag42 </body>}{\*\htmltag27 </html>}}`)
	// "MELA": the uncompressed form of PR_RTF_COMPRESSED.
	head := make([]byte, 16)
	binary.LittleEndian.PutUint32(head[0:], uint32(len(rtf)+12))
	binary.LittleEndian.PutUint32(head[4:], uint32(len(rtf)))
	binary.LittleEndian.PutUint32(head[8:], 0x414C454D)
	m := &msgBuilder{subject: "RTF", senderName: "A", senderEmail: "a@example.com", rtf: append(head, rtf...)}
	p := filepath.Join(t.TempDir(), "rtf.msg")
	m.save(p)
	has(t, "html", read(t, convertTo(t, p, "msg", "html")), "<p>Café 한글 RTF</p>")
	if out, err := decompressRTF(lzfuStored(rtf)); err != nil || !bytes.Equal(out, rtf) {
		t.Fatalf("LZFu: %v %q", err, out)
	}
}

// lzfuStored is LZFu data of literals only (every control byte 0), as a check of the decoder framing.
func lzfuStored(raw []byte) []byte {
	var body []byte
	for i := 0; i < len(raw); i += 8 {
		body = append(body, 0)
		body = append(body, raw[i:min(len(raw), i+8)]...)
	}
	// The end marker: a reference to the current write position.
	pos := (len(rtfPrebuf) + len(raw)) % 4096
	if len(raw)%8 == 0 {
		body = append(body, 1)
	} else {
		body[len(body)-1-len(raw)%8] |= 1 << (len(raw) % 8)
	}
	body = append(body, byte(pos>>4), byte(pos<<4))
	head := make([]byte, 16)
	binary.LittleEndian.PutUint32(head[0:], uint32(len(body)+12))
	binary.LittleEndian.PutUint32(head[4:], uint32(len(raw)))
	binary.LittleEndian.PutUint32(head[8:], 0x75465A4C)
	return append(head, body...)
}

func TestAFileThatIsNoOutlookMessageFailsClearly(t *testing.T) {
	p := filepath.Join(t.TempDir(), "broken.msg")
	os.WriteFile(p, []byte("not a compound file"), 0o644)
	if err := convertErr(t, p, "msg", "html"); err == nil || !strings.Contains(err.Error(), "Not an Outlook message") {
		t.Fatal(err)
	}
}

// ---- MHTML ----

var (
	photo  = pngOf(40, 30, teal)
	banner = pngOf(50, 10, color.RGBA{255, 165, 0, 255})
	secret = pngOf(12, 12, color.RGBA{255, 0, 0, 255})
)

func chromeArchive(t *testing.T, secretFile string) string {
	if secretFile == "" {
		secretFile = "secret.png"
	}
	html := `<!DOCTYPE html><html lang="ko"><head><meta http-equiv="Content-Type" content="text/html; charset=UTF-8">
<base href="https://example.com/news/">
<link rel="stylesheet" type="text/css" href="cid:css-1@mhtml.blink" />
<link rel="stylesheet" href="https://cdn.example.net/remote.css">
<title>오늘의 기사</title><script src="https://example.com/app.js"></script></head>
<body class="article" onload="alert(1)">
<h1>큰 제목</h1>
<p class="hero">본문 첫 문단입니다. 카페 메뉴 – “라떼”.</p>
<p><img src="https://example.com/img/photo.png" alt="사진"></p>
<p><img src="../img/banner.png" alt="배너"></p>
<p><img src="https://tracker.example/pixel.gif" alt="추적"></p>
<p><img src="` + secretFile + `" alt="비밀"></p>
<iframe src="https://ads.example/frame"></iframe>
<script>document.write('injected')</script>
<p><a href="https://example.com/news/other.html">다른 기사</a> · <a href="article.html#comments">댓글</a>
· <a href="javascript:alert(1)">나쁜 링크</a></p>
<p id="comments">댓글 영역</p>
</body></html>`
	css := `@charset "utf-8";
h1 { color: rgb(200, 0, 0); }
.hero { background-image: url("https://example.com/img/banner.png"); }
.remote { background: url(https://tracker.example/bg.png) no-repeat; }
@font-face { font-family: Remote; src: url(https://fonts.example/x.woff2) format("woff2"); }
.escape { content: "</style><script>alert(1)</script>"; }`
	content := strings.Join([]string{
		"From: <Saved by Blink>",
		"Snapshot-Content-Location: https://example.com/news/article.html",
		"Subject: =?utf-8?Q?=EC=98=A4=EB=8A=98=EC=9D=98_=EA=B8=B0=EC=82=AC?=",
		"Date: Tue, 20 Oct 2026 10:00:00 +0900",
		"MIME-Version: 1.0",
		"Content-Type: multipart/related;",
		"\ttype=\"text/html\";",
		"\tboundary=\"----MultipartBoundary--abc----\"",
		"", "",
		"------MultipartBoundary--abc----",
		"Content-Type: text/html",
		"Content-ID: <frame-1@mhtml.blink>",
		"Content-Transfer-Encoding: quoted-printable",
		"Content-Location: https://example.com/news/article.html",
		"",
		quotedPrintable([]byte(html), true),
		"------MultipartBoundary--abc----",
		"Content-Type: text/css",
		"Content-Transfer-Encoding: quoted-printable",
		"Content-Location: cid:css-1@mhtml.blink",
		"",
		quotedPrintable([]byte(css), true),
		"------MultipartBoundary--abc----",
		"Content-Type: image/png",
		"Content-Transfer-Encoding: base64",
		"Content-Location: https://example.com/img/photo.png",
		"",
		lines(photo),
		"------MultipartBoundary--abc----",
		"Content-Type: application/octet-stream",
		"Content-Transfer-Encoding: base64",
		"Content-Location: https://example.com/img/banner.png",
		"",
		lines(banner),
		"------MultipartBoundary--abc------",
		"",
	}, "\r\n")
	p := filepath.Join(t.TempDir(), "기사.mhtml")
	os.WriteFile(p, []byte(content), 0o644)
	return p
}

func TestChromeArchiveBecomesOneSafeHtmlFileWithItsPictures(t *testing.T) {
	html := read(t, convertTo(t, chromeArchive(t, ""), "mhtml", "html"))
	if !strings.HasPrefix(html, "<!DOCTYPE html>\n<html lang=\"ko\">") {
		t.Fatal(html[:60])
	}
	b64 := base64.StdEncoding.EncodeToString
	has(t, "html", html, `<meta charset="utf-8">`, "Content-Security-Policy", "<title>오늘의 기사</title>", `<body class="article">`,
		"<h1>큰 제목</h1>", "카페 메뉴 – “라떼”.",
		`src="data:image/png;base64,`+b64(photo)+`"`, `src="data:image/png;base64,`+b64(banner)+`"`,
		`.hero { background-image: url("data:image/png;base64,`+b64(banner)+`"); }`, "h1 { color: rgb(200, 0, 0); }",
		`src: none format("woff2")`, `href="https://example.com/news/other.html"`, `href="#comments"`)
	hasNot(t, "html", html, "tracker.example", "fonts.example", "cdn.example.net", "ads.example", "app.js", "<script", "<iframe",
		"<link", "<base", "onload", "javascript:", "cid:", "injected", "</style><script>", `src="secret.png"`)
}

func TestContentLocationNeverOpensALocalFile(t *testing.T) {
	folder := t.TempDir()
	s := filepath.Join(folder, "secret.png")
	os.WriteFile(s, secret, 0o644)
	u := (&url.URL{Scheme: "file", Path: filepath.ToSlash(s)}).String()
	archive := chromeArchive(t, u)
	os.WriteFile(filepath.Join(filepath.Dir(archive), "secret.png"), secret, 0o644)
	html := read(t, convertTo(t, archive, "mhtml", "html"))
	hasNot(t, "html", html, base64.StdEncoding.EncodeToString(secret), "secret.png")
	d, err := readMhtmlDocument(archive, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	images := doc.Images(d)
	if len(images) != 2 {
		t.Fatalf("%d pictures", len(images))
	}
	for _, im := range images {
		f, _ := os.Open(im.Path)
		c, _, err := image.DecodeConfig(f)
		f.Close()
		if err != nil || c.Width == 12 {
			t.Fatalf("%v %v", c, err)
		}
	}
}

func TestWordMhtWithFileLocationsAndWindows1252(t *testing.T) {
	html := `<html xmlns:v="urn:schemas-microsoft-com:vml" xmlns:o="urn:schemas-microsoft-com:office:office">
<head><meta http-equiv=Content-Type content="text/html; charset=windows-1252">
<link rel=File-List href="report_files/filelist.xml">
<style><!-- p.MsoNormal { margin: 0in; font-family: "Calibri"; } --></style></head>
<body lang=EN-US style='tab-interval:.5in'>
<p class=MsoNormal>Café crème – naïve</p>
<p class=MsoNormal><!--[if gte vml 1]><v:shape><v:imagedata src="report_files/image001.png" o:title=""/></v:shape><![endif]--><![if !vml]><img width=40 height=30 src="report_files/image002.png" v:shapes="Picture_x0020_1"><![endif]></p>
</body></html>`
	cp, _ := charmap.Windows1252.NewEncoder().Bytes([]byte(html))
	content := strings.Join([]string{
		"MIME-Version: 1.0",
		`Content-Type: multipart/related; boundary="----=_NextPart_01DB0000.12345678"`,
		"",
		"This document is a Single File Web Page, also known as a Web Archive file.",
		"",
		"------=_NextPart_01DB0000.12345678",
		"Content-Location: file:///C:/D0C1A2B3/report.htm",
		"Content-Transfer-Encoding: quoted-printable",
		`Content-Type: text/html; charset="windows-1252"`,
		"",
		quotedPrintable(cp, true),
		"",
		"------=_NextPart_01DB0000.12345678",
		"Content-Location: file:///C:/D0C1A2B3/report_files/image002.png",
		"Content-Transfer-Encoding: base64",
		"Content-Type: image/png",
		"",
		lines(photo),
		"",
		"------=_NextPart_01DB0000.12345678",
		"Content-Location: file:///C:/D0C1A2B3/report_files/filelist.xml",
		"Content-Transfer-Encoding: quoted-printable",
		`Content-Type: text/xml; charset="utf-8"`,
		"",
		"<xml></xml>",
		"------=_NextPart_01DB0000.12345678--",
		"",
	}, "\r\n")
	p := filepath.Join(t.TempDir(), "report.mht")
	os.WriteFile(p, []byte(content), 0o644)
	out := read(t, convertTo(t, p, "mhtml", "html"))
	has(t, "html", out, "Café crème – naïve", `src="data:image/png;base64,`+base64.StdEncoding.EncodeToString(photo)+`"`, `<body lang="EN-US" style="tab-interval:.5in">`)
	hasNot(t, "html", out, "file:", "<v:")
}

func TestCidReferencesInEucKrBase64Page(t *testing.T) {
	html := `<html><head><title>사진 보고서</title></head><body><h1>보고서</h1><p>첫 문단에 한글이 있습니다.</p>` +
		`<p><img src="cid:photo@archive" alt="사진"></p><p><img src="cid:missing@archive"></p>` +
		`<p><img src="data:image/svg+xml;base64,PHN2Zz48L3N2Zz4="></p></body></html>`
	content := strings.Join([]string{
		"MIME-Version: 1.0",
		`Content-Type: multipart/related; type="text/html"; start="<page@archive>"; boundary="b1"`,
		"",
		"--b1",
		"Content-Type: image/png",
		"Content-ID: <photo@archive>",
		"Content-Transfer-Encoding: base64",
		"",
		lines(photo),
		"--b1",
		"Content-Type: text/html; charset=euc-kr",
		"Content-ID: <page@archive>",
		"Content-Transfer-Encoding: base64",
		"",
		lines(eucKR(html)),
		"--b1--",
		"",
	}, "\r\n")
	p := filepath.Join(t.TempDir(), "보고서.mht")
	os.WriteFile(p, []byte(content), 0o644)
	out := read(t, convertTo(t, p, "mhtml", "html"))
	has(t, "html", out, "<title>사진 보고서</title>", "<p>첫 문단에 한글이 있습니다.</p>", `src="data:image/png;base64,`+base64.StdEncoding.EncodeToString(photo)+`"`)
	hasNot(t, "html", out, "cid:", "svg")
	d, err := readMhtmlDocument(p, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Images(d)) != 1 {
		t.Fatalf("%d pictures", len(doc.Images(d)))
	}
	var texts []string
	for _, b := range d.Sections[0].Blocks {
		if p, ok := b.(*doc.Paragraph); ok {
			texts = append(texts, doc.PlainText(p.Inlines))
		}
	}
	if !slices.Contains(texts, "보고서") || !slices.Contains(texts, "첫 문단에 한글이 있습니다.") {
		t.Fatal(texts)
	}
}

func TestArchiveWithoutAPageAndOversizedArchivesFailClearly(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.mhtml")
	os.WriteFile(empty, []byte("MIME-Version: 1.0\r\nContent-Type: image/png\r\nContent-Transfer-Encoding: base64\r\n\r\n"+lines(photo)+"\r\n"), 0o644)
	if err := convertErr(t, empty, "mhtml", "html"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "no html page") {
		t.Fatal(err)
	}
	huge := filepath.Join(dir, "huge.mhtml")
	f, _ := os.Create(huge)
	f.Truncate(maxMhtmlBytes + 1)
	f.Close()
	if _, err := loadMhtml(huge); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatal(err)
	}
}
