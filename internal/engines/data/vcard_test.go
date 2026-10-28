package data

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode/utf16"

	"golang.org/x/text/encoding/korean"
)

func cp949(s string) []byte {
	b, _ := korean.EUCKR.NewEncoder().Bytes([]byte(s))
	return b
}

// quotedPrintable encodes text, every byte outside printable ASCII as =XX, soft breaks at 60.
func quotedPrintable(b []byte) string {
	var sb strings.Builder
	col := 0
	for _, c := range b {
		piece := string(rune(c))
		if c < 33 || c > 126 || c == '=' {
			piece = fmt.Sprintf("=%02X", c)
		}
		if col+len(piece) > 60 {
			sb.WriteString("=\r\n")
			col = 0
		}
		sb.WriteString(piece)
		col += len(piece)
	}
	return sb.String()
}

func csvRows(csv string) [][]string {
	var rows [][]string
	for _, line := range strings.Split(strings.TrimPrefix(csv, "\xEF\xBB\xBF"), "\r\n") {
		if line != "" {
			rows = append(rows, strings.Split(line, ","))
		}
	}
	return rows
}

func TestCardsOfEveryVersionBecomeOneCSVRowEach(t *testing.T) {
	var f bytes.Buffer
	f.WriteString("BEGIN:VCARD\r\nVERSION:2.1\r\n")
	f.WriteString("N;CHARSET=KS_C_5601-1987;ENCODING=QUOTED-PRINTABLE:" + quotedPrintable(cp949("홍")) + ";" + quotedPrintable(cp949("길동")) + ";;;\r\n")
	f.WriteString("TEL;CELL:010-1234-5678\r\nTEL;WORK;VOICE:02-123-4567\r\nEMAIL;PREF;INTERNET:hong@example.com\r\n")
	f.WriteString("ADR;HOME;ENCODING=QUOTED-PRINTABLE:;;" + quotedPrintable(cp949("서울특별시 종로구 세종대로 209 정부서울청사 별관 3층")) + ";;;03171;\r\n")
	f.WriteString("NOTE:")
	f.Write(cp949("전화 주세요"))
	f.WriteString("\r\nPHOTO;ENCODING=BASE64;TYPE=JPEG:\r\n /9j/4AAQSkZJRgABAQAAAQABAAD\r\n 2wBDAAgGBgcGBQgHBwcJ\r\n\r\n")
	f.WriteString("AGENT:\r\nBEGIN:VCARD\r\nVERSION:2.1\r\nFN:Assistant\r\nTEL:000\r\nEND:VCARD\r\n")
	f.WriteString("BDAY:19800102\r\nEND:VCARD\r\n")
	f.WriteString(strings.Join([]string{
		"BEGIN:VCARD", "VERSION:3.0", "FN:Ada Lovelace", "N:Lovelace;Ada;;;",
		`ORG:Analytical Engines\, Ltd.;Research`, "TITLE:Engineer",
		"item1.EMAIL;TYPE=INTERNET,WORK:ada@example.com", "EMAIL;TYPE=HOME:ada@home.example",
		"TEL;TYPE=CELL,VOICE:+44 20 7946 0000",
		`NOTE:First line\nSecond line\; folded over two li`, " nes here",
		"END:VCARD", ""}, "\r\n"))
	f.WriteString(strings.Join([]string{
		"BEGIN:VCARD", "VERSION:4.0", "FN:王小明", "N:王;小明;;;",
		`TEL;VALUE=uri;TYPE="cell,voice":tel:+86-10-1234-5678`, "BDAY:--0412", "END:VCARD", ""}, "\r\n"))

	out, err := run(t, writeFile(t, "contacts.vcf", f.Bytes()), "csv")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(out)
	if !bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("CSV with BOM for Excel")
	}
	eq(t, strings.Join([]string{
		"Full name,Given name,Family name,Organization,Title,Email,Phone,Address,Birthday,Note",
		"홍길동,길동,홍,,,hong@example.com,010-1234-5678 (mobile); 02-123-4567 (work),\"서울특별시 종로구 세종대로 209 정부서울청사 별관 3층, 03171 (home)\",1980-01-02,전화 주세요",
		"Ada Lovelace,Ada,Lovelace,\"Analytical Engines, Ltd., Research\",Engineer,ada@example.com (work); ada@home.example (home),+44 20 7946 0000 (mobile),,,\"First line\nSecond line; folded over two lines here\"",
		"王小明,小明,王,,,,+86-10-1234-5678 (mobile),,--0412,",
		""}, "\r\n"), string(b[3:]))
}

func TestUTF16CardsAreRead(t *testing.T) {
	u := utf16.Encode([]rune("BEGIN:VCARD\r\nVERSION:3.0\r\nFN:김철수\r\nTEL:010-0000-0000\r\nEND:VCARD\r\n"))
	b := []byte{0xFF, 0xFE}
	for _, c := range u {
		b = append(b, byte(c), byte(c>>8))
	}
	out, err := run(t, writeFile(t, "phone.vcf", b), "csv")
	if err != nil {
		t.Fatal(err)
	}
	csv, _ := os.ReadFile(out)
	eq(t, "김철수", csvRows(string(csv))[1][0])
}

func TestCSVWithKoreanHeadersBecomesVCard3AndReadsBack(t *testing.T) {
	src := "\xEF\xBB\xBF이름,전화번호,이메일,회사,메모,관계\r\n" +
		"홍길동,010-1111-2222 (mobile); 02-333-4444 (work),hong@example.com,\"필이, 주식회사\",아주 긴 메모입니다. 한 줄이 75바이트를 넘으면 vCard는 줄을 접어요. 접힌 줄은 공백으로 시작해요.,친구\r\n" +
		",,,,,\r\n"
	vcf, err := run(t, writeFile(t, "주소록.csv", []byte(src)), "vcf")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(vcf)
	text := string(b)
	if !strings.HasPrefix(text, "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:홍길동\r\nN:;;;;\r\nORG:필이\\, 주식회사\r\n") {
		t.Fatal(text)
	}
	has(t, text, "\r\nEMAIL;TYPE=INTERNET:hong@example.com\r\nTEL;TYPE=CELL:010-1111-2222\r\nTEL;TYPE=WORK:02-333-4444\r\n")
	if strings.Count(text, "BEGIN:VCARD") != 1 {
		t.Fatal("the empty row is no contact")
	}
	for _, line := range strings.Split(text, "\r\n") {
		if len(line) > 75 {
			t.Fatalf("not folded: %s", line)
		}
	}
	out, err := run(t, vcf, "csv")
	if err != nil {
		t.Fatal(err)
	}
	csv, _ := os.ReadFile(out)
	row := csvRows(string(csv))[1]
	eq(t, "홍길동", row[0])
	has(t, strings.Join(row, ","), "아주 긴 메모입니다. 한 줄이 75바이트를 넘으면 vCard는 줄을 접어요. 접힌 줄은 공백으로 시작해요.")
	has(t, strings.Join(row, ","), "010-1111-2222 (mobile); 02-333-4444 (work)")
}

func TestCSVWithoutContactColumnsIsRefused(t *testing.T) {
	has(t, fail(t, "numbers.csv", "x,y\r\n1,2\r\n", "vcf"), "No contact columns")
}

func TestLongCombiningSequenceFoldsAndRoundTrips(t *testing.T) {
	name := "e" + strings.Repeat("́", 40)
	text := writeVCard([]*contact{{FullName: name}})
	has(t, text, "\r\n ")
	for _, line := range strings.Split(text, "\r\n") {
		if len(line) > 75 {
			t.Fatal(line)
		}
	}
	got := readVCard([]byte(text))
	if len(got) != 1 || got[0].FullName != name {
		t.Fatal(got)
	}
}
