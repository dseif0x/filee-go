package data

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

func calendar(l ...string) string {
	return strings.Join(append(append([]string{"BEGIN:VCALENDAR", "VERSION:2.0"}, l...), "END:VCALENDAR", ""), "\r\n")
}

func event(uid string, l ...string) string {
	return strings.Join(append(append([]string{"BEGIN:VEVENT", "UID:" + uid}, l...), "END:VEVENT"), "\r\n")
}

func zone(l ...string) string { return strings.Join(l, "\r\n") }

func readCalendar(t *testing.T, text string) [][]string {
	t.Helper()
	return parseCSV(strings.TrimPrefix(convertText(t, "source.ics", text, "csv"), "\xEF\xBB\xBF"), ',')
}

func cell(rows [][]string, row int, name string) string {
	return rows[row][slices.Index(rows[0], name)]
}

func csvText(rows ...[]string) string {
	var out []string
	for _, row := range rows {
		cells := make([]string, len(row))
		for i, v := range row {
			if strings.ContainsAny(v, ",\"\n\r") {
				v = `"` + strings.ReplaceAll(v, `"`, `""`) + `"`
			}
			cells[i] = v
		}
		out = append(out, strings.Join(cells, ","))
	}
	return strings.Join(out, "\r\n") + "\r\n"
}

func toICal(t *testing.T, rows ...[]string) string {
	t.Helper()
	return convertText(t, "source.csv", csvText(rows...), "ics")
}

func TestGoogleExportPreservesColumnsRecurrenceAttendeesFolding(t *testing.T) {
	z := zone("BEGIN:VTIMEZONE", "TZID:Asia/Seoul", "BEGIN:STANDARD", "DTSTART:19700101T000000", "TZOFFSETFROM:+0900", "TZOFFSETTO:+0900", "END:STANDARD", "END:VTIMEZONE")
	rows := readCalendar(t, calendar(z, event("007",
		"SUMMARY:123", "DTSTART:20261021T000000Z", "DTEND:20261021T010000Z", "LOCATION:true",
		"DESCRIPTION:긴 설명이 접힌 줄의 전반부 ", "\t후반부\\n둘째 줄",
		"RRULE:FREQ=WEEKLY;COUNT=3", "EXDATE;TZID=Asia/Seoul:20261028T090000,20261104T090000",
		"EXDATE;TZID=Asia/Seoul:20261111T090000",
		`ATTENDEE;CN="Kim, A":mailto:a@example.com`, "ATTENDEE:mailto:b@example.com",
		"BEGIN:VALARM", "DESCRIPTION:do not replace event description", "END:VALARM"),
		event("seoul", "DTSTART;TZID=Asia/Seoul:20261021T090000")))
	if !slices.Equal(rows[0], icalColumns) || len(rows) != 3 {
		t.Fatal(rows)
	}
	eq(t, "2026-10-21T00:00:00Z", cell(rows, 1, "Start"))
	eq(t, "2026-10-21T09:00:00+09:00", cell(rows, 2, "Start"))
	eq(t, "123", cell(rows, 1, "Title"))
	eq(t, "true", cell(rows, 1, "Location"))
	eq(t, "007", cell(rows, 1, "UID"))
	eq(t, "긴 설명이 접힌 줄의 전반부 후반부\n둘째 줄", cell(rows, 1, "Description"))
	eq(t, "FREQ=WEEKLY;COUNT=3", cell(rows, 1, "RRULE"))
	eq(t, "EXDATE;TZID=Asia/Seoul:20261028T090000,20261104T090000\nEXDATE;TZID=Asia/Seoul:20261111T090000", cell(rows, 1, "EXDATE"))
	eq(t, "Kim, A <a@example.com>\nb@example.com", cell(rows, 1, "Attendees"))
}

func TestAllDayExclusiveEndAndDuration(t *testing.T) {
	rows := readCalendar(t, calendar(
		event("all", "DTSTART;VALUE=DATE:20261021", "DTEND;VALUE=DATE:20261022", "X-APPLE-STRUCTURED-LOCATION:ignored"),
		event("duration", "DTSTART:20261021T090000Z", "DURATION:PT1H30M"),
		event("weeks", "DTSTART;VALUE=DATE:20261021", "DURATION:P2W")))
	eq(t, "2026-10-22", cell(rows, 1, "End"))
	eq(t, "true", cell(rows, 1, "All day"))
	eq(t, "", cell(rows, 1, "Time zone"))
	eq(t, "2026-10-21T10:30:00Z", cell(rows, 2, "End"))
	eq(t, "2026-11-04", cell(rows, 3, "End"))
}

func TestDurationCrossingDaylightSavingTime(t *testing.T) {
	custom := zone("BEGIN:VTIMEZONE", "TZID:Custom/Spring", "BEGIN:DAYLIGHT", "DTSTART:20260308T020000", "TZOFFSETFROM:-0500", "TZOFFSETTO:-0400", "END:DAYLIGHT", "END:VTIMEZONE")
	rows := readCalendar(t, calendar(custom,
		event("spring-clock", "DTSTART;TZID=America/New_York:20260308T013000", "DURATION:PT2H"),
		event("spring-day", "DTSTART;TZID=America/New_York:20260308T013000", "DURATION:P1D"),
		event("spring-24h", "DTSTART;TZID=America/New_York:20260308T013000", "DURATION:PT24H"),
		event("spring-mixed", "DTSTART;TZID=America/New_York:20260307T013000", "DURATION:P1DT2H"),
		event("fall-clock", "DTSTART;TZID=America/New_York:20261101T003000", "DURATION:PT2H"),
		event("custom", "DTSTART;TZID=Custom/Spring:20260308T013000", "DURATION:PT2H")))
	eq(t, "2026-03-08T04:30:00-04:00", cell(rows, 1, "End"))
	eq(t, "2026-03-09T01:30:00-04:00", cell(rows, 2, "End"))
	eq(t, "2026-03-09T02:30:00-04:00", cell(rows, 3, "End"))
	eq(t, "2026-03-08T04:30:00-04:00", cell(rows, 4, "End"))
	eq(t, "2026-11-01T01:30:00-05:00", cell(rows, 5, "End"))
	eq(t, "2026-03-08T04:30:00-04:00", cell(rows, 6, "End"))
}

func TestWindowsZoneCustomZoneUnknownRulesAndRecurrenceInstance(t *testing.T) {
	fixed := zone("BEGIN:VTIMEZONE", "TZID:Customized Time Zone", "BEGIN:STANDARD", "DTSTART:19700101T000000", "TZOFFSETFROM:+0530", "TZOFFSETTO:+0530", "END:STANDARD", "END:VTIMEZONE")
	unknown := zone("BEGIN:VTIMEZONE", "TZID:Unknown/Rule", "BEGIN:STANDARD", "DTSTART:19700101T000000", "TZOFFSETFROM:+0100", "TZOFFSETTO:+0200",
		"RRULE:FREQ=YEARLY;BYMONTH=1;BYDAY=1SU", "END:STANDARD", "END:VTIMEZONE")
	rows := readCalendar(t, calendar(fixed, unknown,
		event("windows", "DTSTART;TZID=Korea Standard Time:20261021T090000"),
		event("custom", "DTSTART;TZID=Customized Time Zone:20261021T090000"),
		event("unknown", "DTSTART;TZID=Unknown/Rule:20261021T090000"),
		event("exception", "DTSTART:20261021T100000Z", "RECURRENCE-ID;TZID=Asia/Seoul:20261021T090000")))
	eq(t, "2026-10-21T09:00:00+09:00", cell(rows, 1, "Start"))
	eq(t, "2026-10-21T09:00:00+05:30", cell(rows, 2, "Start"))
	eq(t, "2026-10-21T09:00:00", cell(rows, 3, "Start"))
	eq(t, "Unknown/Rule", cell(rows, 3, "Time zone"))
	eq(t, "RECURRENCE-ID;TZID=Asia/Seoul:20261021T090000", cell(rows, 4, "Recurrence ID"))
}

func TestFileTimezoneWinsAndTransitionsAreScopedToEachCalendar(t *testing.T) {
	z := func(id, from, to string) string {
		return zone("BEGIN:VTIMEZONE", "TZID:"+id, "BEGIN:STANDARD", "DTSTART:20261021T100000", "TZOFFSETFROM:"+from, "TZOFFSETTO:"+to, "END:STANDARD", "END:VTIMEZONE")
	}
	text := calendar(z("America/New_York", "+0100", "+0100"), event("file", "DTSTART;TZID=America/New_York:20261021T090000")) +
		calendar(z("Custom/Change", "+0100", "+0200"),
			event("before", "DTSTART;TZID=Custom/Change:20261021T090000"),
			event("after", "DTSTART;TZID=Custom/Change:20261021T110000")) +
		calendar(z("America/New_York", "+0300", "+0300"), event("second", "DTSTART;TZID=America/New_York:20261021T090000"))
	rows := readCalendar(t, text)
	for i, want := range []string{"+01:00", "+01:00", "+02:00", "+03:00"} {
		if !strings.HasSuffix(cell(rows, i+1, "Start"), want) {
			t.Fatalf("row %d: %s", i+1, cell(rows, i+1, "Start"))
		}
	}
}

func TestCustomZoneWithSecondOffsets(t *testing.T) {
	z := zone("BEGIN:VTIMEZONE", "TZID:Custom/Seconds", "BEGIN:STANDARD", "DTSTART:19000101T000000", "TZOFFSETFROM:+001932", "TZOFFSETTO:+001932", "END:STANDARD",
		"BEGIN:DAYLIGHT", "DTSTART:19000601T020000", "TZOFFSETFROM:+001932", "TZOFFSETTO:+012032", "END:DAYLIGHT", "END:VTIMEZONE")
	rows := readCalendar(t, calendar(z,
		event("before", "DTSTART;TZID=Custom/Seconds:19000601T013000", "DURATION:PT2H"),
		event("after", "DTSTART;TZID=Custom/Seconds:19000601T040000", "DTEND;TZID=Custom/Seconds:19000601T050000")))
	eq(t, "1900-06-01T01:10:28Z", cell(rows, 1, "Start"))
	eq(t, "1900-06-01T03:10:28Z", cell(rows, 1, "End"))
	eq(t, "1900-06-01T02:39:28Z", cell(rows, 2, "Start"))
	eq(t, "1900-06-01T03:39:28Z", cell(rows, 2, "End"))
	for offset, want := range map[string]string{"+001932": "1900-06-01T11:40:28Z", "-003015": "1900-06-01T12:30:15Z", "+1500": "1900-05-31T21:00:00Z", "+053000": "1900-06-01T12:00:00+05:30"} {
		z := zone("BEGIN:VTIMEZONE", "TZID:Custom/Fixed", "BEGIN:STANDARD", "DTSTART:19000101T000000", "TZOFFSETFROM:"+offset, "TZOFFSETTO:"+offset, "END:STANDARD", "END:VTIMEZONE")
		eq(t, want, cell(readCalendar(t, calendar(z, event("fixed", "DTSTART;TZID=Custom/Fixed:19000601T120000"))), 1, "Start"))
	}
}

func TestRdateTransitionsAndEscapes(t *testing.T) {
	z := strings.Join([]string{"BEGIN:VTIMEZONE", "TZID:Custom/Rdate", "BEGIN:STANDARD", "DTSTART:20261021T100000", "RDATE:20261121T100000",
		"TZOFFSETFROM:+0100", "TZOFFSETTO:+0200", "END:STANDARD", "BEGIN:DAYLIGHT", "DTSTART:20261221T100000", "RDATE:20270121T100000",
		"TZOFFSETFROM:+0200", "TZOFFSETTO:+0300", "END:DAYLIGHT", "END:VTIMEZONE"}, "\n")
	rows := readCalendar(t, strings.ReplaceAll(calendar(z,
		event("first", "DTSTART;TZID=Custom/Rdate:20261020T090000", `SUMMARY:a\\b\;c\,d\ne\Nlast`),
		event("middle", "DTSTART;TZID=Custom/Rdate:20261101T090000"),
		event("last", "DTSTART;TZID=Custom/Rdate:20270122T090000")), "\r\n", "\n"))
	for i, want := range []string{"+01:00", "+02:00", "+03:00"} {
		if !strings.HasSuffix(cell(rows, i+1, "Start"), want) {
			t.Fatalf("row %d: %s", i+1, cell(rows, i+1, "Start"))
		}
	}
	eq(t, "a\\b;c,d\ne\nlast", cell(rows, 1, "Title"))
}

func TestTodoOnlyDueAndMixedEndZones(t *testing.T) {
	rows := readCalendar(t, calendar(
		"BEGIN:VTODO", "UID:due", "DUE;VALUE=DATE:20261030", "END:VTODO",
		"BEGIN:VTODO", "UID:duration", "DTSTART:20261021T090000Z", "DURATION:PT1H", "END:VTODO",
		"BEGIN:VTODO", "UID:empty", "END:VTODO",
		event("mixed", "DTSTART;TZID=Asia/Seoul:20261021T090000", "DTEND;TZID=America/New_York:20261021T090000")))
	if len(rows) != 5 {
		t.Fatal(rows)
	}
	eq(t, "", cell(rows, 1, "Start"))
	eq(t, "2026-10-30", cell(rows, 1, "End"))
	eq(t, "false", cell(rows, 1, "All day"))
	eq(t, "2026-10-21T10:00:00Z", cell(rows, 2, "End"))
	eq(t, "", cell(rows, 3, "End"))
	eq(t, "Asia/Seoul", cell(rows, 4, "Time zone"))
	if !strings.HasSuffix(cell(rows, 4, "End"), "-04:00") {
		t.Fatal(cell(rows, 4, "End"))
	}
	out := toICal(t, icalColumns, rows[1])
	hasNot(t, out, "DTSTART")
	has(t, out, "DUE;VALUE=DATE:20261030")
}

func TestMalformedInputKeepsOnlyClosedComponents(t *testing.T) {
	rows := readCalendar(t, calendar("garbage", "NO-COLON",
		event("closed", "DTSTART:tomorrow", "DTEND:tomorrow", "EXDATE:abc"),
		event("bad-duration", "DTSTART:20261021T090000Z", "DURATION:abc"),
		event("missing-start", "DTEND:20261021T090000Z"))+"BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:unclosed")
	if len(rows) != 4 {
		t.Fatal(rows)
	}
	eq(t, "tomorrow", cell(rows, 1, "Start"))
	eq(t, "tomorrow", cell(rows, 1, "End"))
	eq(t, "EXDATE:abc", cell(rows, 1, "EXDATE"))
	eq(t, "", cell(rows, 2, "End"))
	eq(t, "", cell(rows, 3, "Start"))
	eq(t, "2026-10-21T09:00:00Z", cell(rows, 3, "End"))
}

func TestAmbiguousNewYorkTimeUsesStandardOffset(t *testing.T) {
	eq(t, "2026-11-01T01:30:00-05:00", cell(readCalendar(t, calendar(event("dst", "DTSTART;TZID=America/New_York:20261101T013000"))), 1, "Start"))
}

func TestEmptyCalendarAndInvalidTableFail(t *testing.T) {
	has(t, fail(t, "empty.ics", calendar("BEGIN:VJOURNAL", "END:VJOURNAL"), "csv"), "no iCalendar events")
	has(t, fail(t, "empty.csv", "Title,Start\r\nA,\r\nB,10/21/2026\r\n", "ics"), "no iCalendar events")
	fail(t, "unknown.csv", "Unknown,Other\r\nA,B\r\n", "ics")
}

func TestICalToJSONUsesCSVTypeInference(t *testing.T) {
	csvPath, err := run(t, writeFile(t, "cal.ics", []byte(calendar(event("007", "SUMMARY:123", "LOCATION:true", "DTSTART;VALUE=DATE:20261021")))), "csv")
	if err != nil {
		t.Fatal(err)
	}
	jsonPath, err := run(t, csvPath, "json")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(jsonPath)
	j := string(b)
	has(t, j, `"All day": true`)
	has(t, j, `"Location": true`)
	has(t, j, `"Title": 123`)
	has(t, j, `"UID": "007"`)
}

func TestCSVWriterSkipsInvalidEventsAndUsesLeftmostAlias(t *testing.T) {
	text := toICal(t, []string{"Title", "Summary", "Start", "End", "Unknown"},
		[]string{"A", "B", "2026-10-21", "soon", "secret"},
		[]string{"skip", "x", "", "", "secret"},
		[]string{"skip2", "y", "10/21/2026", "", "secret"})
	if strings.Count(text, "BEGIN:VEVENT") != 1 {
		t.Fatal(text)
	}
	has(t, text, "SUMMARY:A")
	hasNot(t, text, "SUMMARY:B")
	hasNot(t, text, "DTEND")
	hasNot(t, text, "secret")
	for _, h := range [][]string{{"Summary", "Begin", "Finish"}, {"Subject", "시작", "종료"}, {"제목", "开始", "结束"}, {"title", "START", "End"}} {
		out := toICal(t, h, []string{"alias", "2026-10-21", "2026-10-22"})
		has(t, out, "SUMMARY:alias")
		has(t, out, "DTSTART;VALUE=DATE:20261021")
		has(t, out, "DTEND;VALUE=DATE:20261022")
	}
	for alias, field := range map[string]string{"장소": "LOCATION", "地点": "LOCATION", "Notes": "DESCRIPTION", "설명": "DESCRIPTION"} {
		has(t, toICal(t, []string{"Title", "Start", alias}, []string{"A", "2026-10-21", "sample"}), field+":sample")
	}
}

func TestOutputContractUIDStampFoldingTimezoneAndEscaping(t *testing.T) {
	row := []string{"event", "제목, 쉼표; 역슬래시\\ 줄\n둘", "2026-07-21T09:00:00+09:00",
		"2026-07-21T10:00:00+09:00", "false", "Asia/Seoul", "", strings.Repeat("가", 55),
		"Kim, A <a@example.com>", "Kim; A <b@example.com>", "FREQ=DAILY;COUNT=2",
		"EXDATE;TZID=Asia/Seoul:20260722T090000", "RECURRENCE-ID;TZID=Asia/Seoul:20260720T090000", ""}
	before := time.Now().UTC().Add(-2 * time.Second)
	text := toICal(t, icalColumns, row)
	if !strings.HasPrefix(text, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Filee//Filee//EN\r\n") {
		t.Fatal(text)
	}
	has(t, text, "BEGIN:VTIMEZONE\r\nTZID:Asia/Seoul\r\n")
	has(t, text, "TZOFFSETFROM:+0900\r\nTZOFFSETTO:+0900")
	has(t, text, `SUMMARY:제목\, 쉼표\; 역슬래시\\ 줄\n둘`)
	has(t, text, `ORGANIZER;CN="Kim, A":mailto:a@example.com`)
	has(t, text, `ATTENDEE;CN="Kim; A":mailto:b@example.com`)
	has(t, text, "DTSTART;TZID=Asia/Seoul:20260721T090000")
	has(t, text, "EXDATE;TZID=Asia/Seoul:20260722T090000")
	has(t, text, "RECURRENCE-ID;TZID=Asia/Seoul:20260720T090000")
	hasNot(t, strings.ReplaceAll(text, "\r\n", ""), "\n")
	for _, line := range strings.Split(text, "\r\n") {
		if len(line) > 75 {
			t.Fatal(line)
		}
	}
	unfolded := strings.ReplaceAll(text, "\r\n ", "")
	stamp := regexp.MustCompile(`DTSTAMP:(\S+)`).FindStringSubmatch(unfolded)[1]
	if st, err := time.Parse("20060102T150405Z", stamp); err != nil || st.Before(before) || st.After(time.Now().Add(2*time.Second)) {
		t.Fatal(stamp)
	}
	uid := regexp.MustCompile(`UID:\S+`).FindString(unfolded)
	has(t, strings.ReplaceAll(toICal(t, icalColumns, row), "\r\n ", ""), uid)
	back := readCalendar(t, text)
	eq(t, "Kim, A <a@example.com>", cell(back, 1, "Organizer"))
	eq(t, "Kim; A <b@example.com>", cell(back, 1, "Attendees"))
}

func TestInjectedLinesCannotCreateContent(t *testing.T) {
	row := make([]string, len(icalColumns))
	row[1] = "Safe"
	row[2] = "2026-10-22T09:00:00Z"
	row[10] = "FREQ=DAILY\nBEGIN:VALARM"
	row[11] = "EXDATE:20261022T090000Z\r\nEND:VEVENT\nBEGIN:VEVENT\rSUMMARY:Injected\nexdate;VALUE=DATE:20261023\rEXDATEX:20261024T090000Z"
	second := slices.Clone(row)
	second[1] = "Second"
	second[10] = "FREQ=WEEKLY\rX-INJECT:1"
	second[11] = ""
	text := strings.ReplaceAll(toICal(t, icalColumns, row, second), "\r\n ", "")
	lines := strings.Split(strings.TrimSuffix(text, "\r\n"), "\r\n")
	n := 0
	for _, l := range lines {
		if l == "BEGIN:VEVENT" {
			n++
		}
	}
	if n != 2 {
		t.Fatal(text)
	}
	hasNot(t, text, "VALARM")
	hasNot(t, text, "Injected")
	hasNot(t, text, "RRULE:")
	if !slices.Contains(lines, "EXDATE:20261022T090000Z") || !slices.Contains(lines, "exdate;VALUE=DATE:20261023") || slices.Contains(lines, "EXDATEX:20261024T090000Z") {
		t.Fatal(text)
	}
	re := regexp.MustCompile(`^[A-Za-z-]+(?:;[^:\r\n]+)?:[^\r\n]*$`)
	for _, l := range lines {
		if !re.MatchString(l) {
			t.Fatal(l)
		}
	}
}

func TestInvalidTimezoneCellIsDiscarded(t *testing.T) {
	for _, z := range []string{"Asia/Seoul\nX-INJECT:1", "Asia/Seoul\rX-INJECT:1", "Asia/Seoul:X", "Asia/Seoul;X", "Asia/Seoul\"X"} {
		text := toICal(t, []string{"Title", "Start", "Time zone"}, []string{"Safe", "2026-10-21T09:00:00+09:00", z})
		hasNot(t, text, "TZID")
		hasNot(t, text, "VTIMEZONE")
		has(t, text, "SUMMARY:Safe\r\nDTSTART:20261021T000000Z")
	}
	text := strings.ReplaceAll(toICal(t, []string{"Title", "Start", "Time zone"},
		[]string{"Valid", "2026-10-21T09:00:00+09:00", "Asia/Seoul"},
		[]string{"Invalid", "2026-10-21T09:00:00+09:00", "Asia/Seoul;X"}), "\r\n ", "")
	has(t, text, "BEGIN:VTIMEZONE\r\nTZID:Asia/Seoul")
	has(t, text, "SUMMARY:Valid\r\nDTSTART;TZID=Asia/Seoul:20261021T090000")
	has(t, text, "SUMMARY:Invalid\r\nDTSTART:20261021T000000Z")
}

func TestUIDWithLineBreakUsesGeneratedUID(t *testing.T) {
	text := strings.ReplaceAll(toICal(t, []string{"Title", "Start", "UID"}, []string{"Safe", "2026-10-21", "bad\nBEGIN:VEVENT"}), "\r\n ", "")
	if !regexp.MustCompile(`(?m)^UID:[0-9a-f]{16}@filee\r?$`).MatchString(text) {
		t.Fatal(text)
	}
	has(t, text, "SUMMARY:Safe\r\nDTSTART;VALUE=DATE:20261021")
}

func TestPeopleRemoveQuotesAndLineBreaks(t *testing.T) {
	text := strings.ReplaceAll(toICal(t, []string{"Title", "Start", "Organizer", "Attendees"},
		[]string{"Safe", "2026-10-21", "Lee\r\nX-INJECT:1, B <o@example.com>", "Kim \"A\", B <a@example.com>\r\nPark\rInjected, C <b@example.com>"}), "\r\n ", "")
	has(t, text, "ORGANIZER;CN=\"LeeX-INJECT:1, B\":mailto:o@example.com\r\n")
	has(t, text, "ATTENDEE;CN=\"Kim A, B\":mailto:a@example.com\r\n")
	has(t, text, "ATTENDEE;CN=\"ParkInjected, C\":mailto:b@example.com\r\n")
	hasNot(t, text, "\r\nX-INJECT:")
}

func TestCanonicalRowsRoundTripAllFourteenColumns(t *testing.T) {
	rows := [][]string{
		{"event", "all", "2026-10-21", "2026-10-22", "true", "", "", "", "", "", "", "", "", "all"},
		{"event", "seoul", "2026-10-21T09:00:00+09:00", "2026-10-21T10:00:00+09:00", "false", "Asia/Seoul", "", "", "", "", "", "", "", "seoul"},
		{"event", "summer", "2026-07-21T09:00:00-04:00", "2026-07-21T10:00:00-04:00", "false", "America/New_York", "", "", "", "", "", "", "", "summer"},
		{"event", "winter", "2026-12-21T09:00:00-05:00", "2026-12-21T10:00:00-05:00", "false", "America/New_York", "", "", "", "", "", "", "", "winter"},
		{"event", "overlap", "2026-11-01T01:30:00-05:00", "", "false", "America/New_York", "", "", "", "", "", "", "", "overlap"},
		{"event", "gap", "2026-03-08T02:30:00-05:00", "", "false", "America/New_York", "", "", "", "", "", "", "", "gap"},
		{"event", "utc", "2026-10-21T00:00:00Z", "", "false", "", "", "", "", "", "", "", "", "utc"},
		{"event", "float", "2026-10-21T09:00:00", "", "false", "", "", "", "", "", "", "", "", "float"},
		{"todo", "task", "", "2026-10-21T09:00:00", "false", "", "", "", "", "", "", "", "", "todo"},
		{"event", "a,b;c\\d\nsecond", "2026-10-21T09:00:00Z", "", "false", "", "", "x,y;z\\q\nagain",
			"name <a@example.com>", "one <b@example.com>\ntwo <c@example.com>", "FREQ=WEEKLY;COUNT=2",
			"EXDATE:20261028T090000Z\nEXDATE:20261104T090000Z", "RECURRENCE-ID:20261021T090000Z", "details"},
		{"event", "uri", "2026-10-21T09:00:00Z", "", "false", "", "", "", "urn:organizer:abc", "", "", "", "", "uri"},
	}
	ics := toICal(t, append([][]string{icalColumns}, rows...)...)
	back := readCalendar(t, ics)
	if len(back) != len(rows)+1 {
		t.Fatal(back)
	}
	for i, r := range rows {
		if !slices.Equal(r, back[i+1]) {
			t.Errorf("row %d:\n%q\n%q", i+1, r, back[i+1])
		}
	}
}

func TestNoncanonicalRowsFollowLossAndOffsetRules(t *testing.T) {
	rows := [][]string{
		{"event", "generated", "2026-10-21T09:00:00Z", "", "false", "", "", "", "", "", "", "", "", ""},
		{"event", "bad end", "2026-10-21T09:00:00Z", "tomorrow", "false", "", "", "", "", "", "", "", "", "bad"},
		{"event", "floating end", "2026-10-21T09:00:00+09:00", "2026-10-21T10:00:00", "false", "Asia/Seoul", "", "", "", "", "", "", "", "floating"},
		{"event", "overlap front", "2026-11-01T01:30:00-04:00", "", "false", "America/New_York", "", "", "", "", "", "", "", "front"},
		{"event", "unknown", "2026-10-21T09:00:00+09:00", "", "false", "Unknown/Area", "", "", "", "", "", "", "", "unknown"},
		{"event", "numeric", "2026-10-21T09:00:00+09:00", "", "false", "", "", "", "", "", "", "", "", "numeric"},
		{"event", "invalid exdates", "2026-10-21T09:00:00Z", "", "false", "", "", "", "", "", "", "EXDATEX:20261028T090000\nEXDATE :20261029T090000", "", "exdates"},
		{"event", "missing start", "", "2026-10-21T09:00:00Z", "false", "", "", "", "", "", "", "", "", "missing"},
	}
	back := readCalendar(t, toICal(t, append([][]string{icalColumns}, rows...)...))
	if len(back) != 8 {
		t.Fatal(back)
	}
	if !strings.HasSuffix(cell(back, 1, "UID"), "@filee") {
		t.Fatal(cell(back, 1, "UID"))
	}
	eq(t, "", cell(back, 2, "End"))
	eq(t, "2026-10-21T10:00:00+09:00", cell(back, 3, "End"))
	eq(t, "2026-11-01T01:30:00-05:00", cell(back, 4, "Start"))
	eq(t, "", cell(back, 5, "Time zone"))
	eq(t, "2026-10-21T00:00:00Z", cell(back, 5, "Start"))
	eq(t, "2026-10-21T00:00:00Z", cell(back, 6, "Start"))
	eq(t, "", cell(back, 7, "EXDATE"))
}

func TestFixedZoneUsesEarliestStartAndHistoricalOffsets(t *testing.T) {
	row1 := []string{"todo", "old", "", "2013-01-01T09:00:00+04:00", "false", "Europe/Moscow", "", "", "", "", "", "", "", "old"}
	row2 := []string{"event", "new", "2026-01-01T09:00:00+03:00", "", "false", "Europe/Moscow", "", "", "", "", "", "", "", "new"}
	has(t, toICal(t, icalColumns, row1), "TZOFFSETFROM:+0400\r\nTZOFFSETTO:+0400")
	old := slices.Clone(row1)
	old[0], old[2] = "event", old[3]
	out := toICal(t, icalColumns, old, row2)
	has(t, out, "TZOFFSETFROM:+0400\r\nTZOFFSETTO:+0400")
	if s := cell(readCalendar(t, out), 2, "Start"); !strings.HasSuffix(s, "+04:00") {
		t.Fatal(s)
	}
}

func TestTimezoneBuilderObservesTransitionWallTimes(t *testing.T) {
	utc := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	ny, _ := time.LoadLocation("America/New_York")
	s := buildVTimeZone(ny, utc, "America/New_York")
	has(t, s, "BEGIN:DAYLIGHT\r\nDTSTART:20260308T020000\r\nTZOFFSETFROM:-0500\r\nTZOFFSETTO:-0400\r\nRRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=2SU")
	has(t, s, "BEGIN:STANDARD\r\nDTSTART:20261101T020000\r\nTZOFFSETFROM:-0400\r\nTZOFFSETTO:-0500\r\nRRULE:FREQ=YEARLY;BYMONTH=11;BYDAY=1SU")
	berlin, _ := time.LoadLocation("Europe/Berlin")
	s = buildVTimeZone(berlin, utc, "Europe/Berlin")
	has(t, s, "BEGIN:DAYLIGHT\r\nDTSTART:20260329T020000\r\nTZOFFSETFROM:+0100\r\nTZOFFSETTO:+0200\r\nRRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU")
	has(t, s, "BEGIN:STANDARD\r\nDTSTART:20261025T030000\r\nTZOFFSETFROM:+0200\r\nTZOFFSETTO:+0100\r\nRRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU")
}

func TestLongCombiningTitleFoldsAndRoundTrips(t *testing.T) {
	title := "e" + strings.Repeat("\u0301", 40)
	text := toICal(t, []string{"Title", "Start"}, []string{title, "2026-10-21"})
	for _, line := range strings.Split(text, "\r\n") {
		if len(line) > 75 {
			t.Fatal(line)
		}
	}
	eq(t, title, cell(readCalendar(t, text), 1, "Title"))
}
