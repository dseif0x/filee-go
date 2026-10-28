package data

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // time zones by IANA name on every OS, as .NET finds them

	"github.com/dseif0x/filee-go/internal/engines/subtitles"
)

// iCalendar VEVENT and VTODO ↔ the fourteen-column CSV calendar table.
// Unknown components are ignored; recurrence rules and exception content lines
// are preserved, not expanded.

var icalColumns = []string{"Type", "Title", "Start", "End", "All day", "Time zone", "Location", "Description",
	"Organizer", "Attendees", "RRULE", "EXDATE", "Recurrence ID", "UID"}

type icalProperty struct {
	name, value, raw string
	params           map[string]string // upper-case names
}

func (p *icalProperty) param(name string) string { return p.params[name] }

type observance struct {
	dates    []string
	from, to time.Duration
	rule     bool
}

type icalComponent struct {
	typ         string
	props       []*icalProperty
	zones       map[string]*zoneDefinition // shared by the components of one VCALENDAR (upper-case ids)
	observances []observance               // of a VTIMEZONE, while reading
	cells       []string
}

func newComponent(typ string, zones map[string]*zoneDefinition) *icalComponent {
	if zones == nil {
		zones = map[string]*zoneDefinition{}
	}
	return &icalComponent{typ: typ, zones: zones}
}

func (c *icalComponent) first(name string) *icalProperty {
	for _, p := range c.props {
		if p.name == name {
			return p
		}
	}
	return nil
}

func (c *icalComponent) all(name string) []*icalProperty {
	var out []*icalProperty
	for _, p := range c.props {
		if p.name == name {
			out = append(out, p)
		}
	}
	return out
}

type zoneTransition struct {
	local    time.Time // wall time, in time.UTC
	from, to time.Duration
}

type zoneDefinition struct {
	hasRule     bool
	transitions []zoneTransition
}

func parseICalLine(line string) *icalProperty {
	quoted := false
	colon := -1
	for i := 0; i < len(line); i++ {
		if line[i] == '"' {
			quoted = !quoted
		} else if line[i] == ':' && !quoted {
			colon = i
			break
		}
	}
	if colon <= 0 {
		return nil
	}
	var parts []string
	start := 0
	quoted = false
	for i := 0; i < colon; i++ {
		if line[i] == '"' {
			quoted = !quoted
		}
		if line[i] == ';' && !quoted {
			parts = append(parts, line[start:i])
			start = i + 1
		}
	}
	parts = append(parts, line[start:colon])
	if parts[0] == "" || strings.ContainsAny(parts[0], " \t\r\n\v\f") {
		return nil
	}
	params := map[string]string{}
	for _, part := range parts[1:] {
		if eq := strings.IndexByte(part, '='); eq > 0 {
			params[strings.ToUpper(part[:eq])] = strings.Trim(part[eq+1:], `"`)
		}
	}
	return &icalProperty{name: strings.ToUpper(parts[0]), value: line[colon+1:], raw: line, params: params}
}

func unfoldICal(text string) []string {
	var out []string
	var cur strings.Builder
	started := false
	for _, line := range splitLines(text) {
		if started && line != "" && (line[0] == ' ' || line[0] == '\t') {
			cur.WriteString(line[1:])
			continue
		}
		if started {
			out = append(out, cur.String())
		}
		cur.Reset()
		cur.WriteString(line)
		started = true
	}
	if started {
		out = append(out, cur.String())
	}
	return out
}

func readICal(b []byte) []*icalComponent {
	var result, stack []*icalComponent
	var zones map[string]*zoneDefinition
	for _, line := range unfoldICal(subtitles.DecodeText(b)) {
		p := parseICalLine(line)
		if p == nil {
			continue
		}
		switch {
		case p.name == "BEGIN":
			typ := strings.ToUpper(p.value)
			if typ == "VCALENDAR" && len(stack) == 0 {
				zones = map[string]*zoneDefinition{}
			}
			stack = append(stack, newComponent(typ, zones))
		case p.name == "END":
			if len(stack) == 0 || stack[len(stack)-1].typ != strings.ToUpper(p.value) {
				continue
			}
			closed := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if (closed.typ == "STANDARD" || closed.typ == "DAYLIGHT") && len(stack) > 0 && stack[len(stack)-1].typ == "VTIMEZONE" {
				from, ok1 := tryOffset(closed.first("TZOFFSETFROM"))
				to, ok2 := tryOffset(closed.first("TZOFFSETTO"))
				if ok1 && ok2 {
					var dates []string
					for _, d := range append(closed.all("DTSTART"), closed.all("RDATE")...) {
						dates = append(dates, strings.Split(d.value, ",")...)
					}
					parent := stack[len(stack)-1]
					parent.observances = append(parent.observances, observance{dates, from, to, closed.first("RRULE") != nil})
				}
			}
			if len(stack) == 1 && stack[0].typ == "VCALENDAR" {
				switch closed.typ {
				case "VEVENT", "VTODO":
					result = append(result, closed)
				case "VTIMEZONE":
					addZone(closed)
				}
			}
			if closed.typ == "VCALENDAR" {
				zones = nil
			}
		case len(stack) > 0:
			top := stack[len(stack)-1]
			top.props = append(top.props, p)
		}
	}
	return result
}

func addZone(c *icalComponent) {
	idp := c.first("TZID")
	if idp == nil || idp.value == "" {
		return
	}
	def := &zoneDefinition{}
	for _, o := range c.observances {
		def.hasRule = def.hasRule || o.rule
		for _, d := range o.dates {
			if local, err := time.Parse("20060102T150405", d); err == nil {
				def.transitions = append(def.transitions, zoneTransition{local, o.from, o.to})
			}
		}
	}
	c.zones[strings.ToUpper(idp.value)] = def
}

func tryOffset(p *icalProperty) (time.Duration, bool) {
	if p == nil {
		return 0, false
	}
	v := p.value
	if (len(v) != 5 && len(v) != 7) || (v[0] != '+' && v[0] != '-') {
		return 0, false
	}
	digits := func(s string) (int, bool) {
		n, err := strconv.Atoi(s)
		return n, err == nil && strings.Trim(s, "0123456789") == ""
	}
	h, ok1 := digits(v[1:3])
	m, ok2 := digits(v[3:5])
	if !ok1 || !ok2 {
		return 0, false
	}
	s := 0
	if len(v) == 7 {
		var ok bool
		if s, ok = digits(v[5:7]); !ok {
			return 0, false
		}
	}
	d := time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(s)*time.Second
	if v[0] == '-' {
		d = -d
	}
	return d, true
}

func icalUnescape(v string) string {
	var b strings.Builder
	rs := []rune(v)
	for i := 0; i < len(rs); i++ {
		if rs[i] == '\\' && i+1 < len(rs) {
			i++
			if rs[i] == 'n' || rs[i] == 'N' {
				b.WriteByte('\n')
			} else {
				b.WriteRune(rs[i])
			}
		} else {
			b.WriteRune(rs[i])
		}
	}
	return b.String()
}

func person(p *icalProperty) string {
	address := p.value
	if len(address) >= 7 && strings.EqualFold(address[:7], "mailto:") {
		address = address[7:]
	}
	name := p.param("CN")
	if name == "" {
		return address
	}
	return name + " <" + address + ">"
}

func parseICalDate(p *icalProperty) (value time.Time, date, utc, ok bool) {
	date = strings.EqualFold(p.param("VALUE"), "DATE") || len(p.value) == 8
	utc = strings.HasSuffix(p.value, "Z")
	for _, layout := range []string{"20060102", "20060102T150405", "20060102T150405Z"} {
		if len(p.value) != len(layout) {
			continue
		}
		if t, err := time.Parse(layout, p.value); err == nil {
			return t, date, utc, true
		}
	}
	return time.Time{}, date, utc, false
}

// loadZone finds a time zone by its IANA name.
func loadZone(id string) *time.Location {
	if id == "" || strings.EqualFold(id, "Local") {
		return nil
	}
	loc, err := time.LoadLocation(id)
	if err != nil {
		iana, ok := windowsZones[id]
		if !ok {
			return nil
		}
		if loc, err = time.LoadLocation(iana); err != nil {
			return nil
		}
	}
	return loc
}

func offsetAt(loc *time.Location, instant time.Time) time.Duration {
	_, s := instant.In(loc).Zone()
	return time.Duration(s) * time.Second
}

// wallOffset is the offset of a wall time in loc, as .NET's GetUtcOffset of an
// unspecified time: the standard (smaller) offset when the time is ambiguous or
// skipped.
func wallOffset(loc *time.Location, local time.Time) time.Duration {
	candidates := []time.Duration{offsetAt(loc, local.Add(-36*time.Hour)), offsetAt(loc, local.Add(36*time.Hour)), offsetAt(loc, local)}
	var valid []time.Duration
	for _, o := range candidates {
		if offsetAt(loc, local.Add(-o)) == o && !containsDuration(valid, o) {
			valid = append(valid, o)
		}
	}
	if len(valid) == 1 {
		return valid[0]
	}
	if len(valid) == 0 {
		valid = candidates
	}
	m := valid[0]
	for _, o := range valid {
		m = min(m, o)
	}
	return m
}

func containsDuration(list []time.Duration, d time.Duration) bool {
	for _, x := range list {
		if x == d {
			return true
		}
	}
	return false
}

func zoneOffset(id string, local time.Time, zones map[string]*zoneDefinition) (time.Duration, bool) {
	if def, ok := zones[strings.ToUpper(id)]; ok && !def.hasRule && len(def.transitions) > 0 {
		ordered := append([]zoneTransition{}, def.transitions...)
		sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].local.Before(ordered[j].local) })
		var found *zoneTransition
		for i := range ordered {
			if !ordered[i].local.After(local) {
				found = &ordered[i]
			}
		}
		if found == nil {
			return ordered[0].from, true
		}
		return found.to, true
	}
	loc := loadZone(id)
	if loc == nil {
		return 0, false
	}
	return wallOffset(loc, local), true
}

// zoneOffsetAtInstant resolves the offset at an instant after adding the clock
// part of a DURATION: the wall time may have crossed a daylight-saving
// transition since DTSTART.
func zoneOffsetAtInstant(id string, instant time.Time, zones map[string]*zoneDefinition) (time.Duration, bool) {
	if def, ok := zones[strings.ToUpper(id)]; ok && !def.hasRule && len(def.transitions) > 0 {
		ordered := append([]zoneTransition{}, def.transitions...)
		at := func(t zoneTransition) time.Time { return t.local.Add(-t.from) }
		sort.SliceStable(ordered, func(i, j int) bool { return at(ordered[i]).Before(at(ordered[j])) })
		var found *zoneTransition
		for i := range ordered {
			if !at(ordered[i]).After(instant) {
				found = &ordered[i]
			}
		}
		if found == nil {
			return ordered[0].from, true
		}
		return found.to, true
	}
	loc := loadZone(id)
	if loc == nil {
		return 0, false
	}
	return offsetAt(loc, instant), true
}

func dateCell(p *icalProperty, zones map[string]*zoneDefinition) string {
	if p == nil {
		return ""
	}
	value, date, utc, ok := parseICalDate(p)
	if !ok {
		return p.value
	}
	if date {
		return value.Format("2006-01-02")
	}
	t := value.Format("2006-01-02T15:04:05")
	if utc {
		return t + "Z"
	}
	id := p.param("TZID")
	if id == "" {
		return t
	}
	offset, ok := zoneOffset(id, value, zones)
	if !ok {
		return t
	}
	return dateCellAtOffset(value, offset)
}

// dateCellAtOffset writes a wall time with its offset; an offset with seconds
// or beyond 14 hours, which ISO offsets in the CSV can't hold, as the UTC instant.
func dateCellAtOffset(local time.Time, offset time.Duration) string {
	if offset%time.Minute != 0 || offset > 14*time.Hour || offset < -14*time.Hour {
		return local.Add(-offset).Format("2006-01-02T15:04:05Z")
	}
	return local.Format("2006-01-02T15:04:05") + offsetISO(offset)
}

func offsetISO(o time.Duration) string {
	sign := "+"
	if o < 0 {
		sign, o = "-", -o
	}
	return fmt.Sprintf("%s%02d:%02d", sign, int(o/time.Hour), int(o%time.Hour/time.Minute))
}

type calendarDuration struct{ days, clock time.Duration }

func parseDuration(t string) (calendarDuration, bool) {
	sign := time.Duration(1)
	if strings.HasPrefix(t, "-") || strings.HasPrefix(t, "+") {
		if t[0] == '-' {
			sign = -1
		}
		t = t[1:]
	}
	if !strings.HasPrefix(t, "P") {
		return calendarDuration{}, false
	}
	t = t[1:]
	inTime, digits, any := false, "", false
	var days, clock time.Duration
	for _, ch := range t {
		if ch == 'T' && !inTime {
			inTime = true
			continue
		}
		if ch >= '0' && ch <= '9' {
			digits += string(ch)
			continue
		}
		n, err := strconv.ParseInt(digits, 10, 32)
		if digits == "" || err != nil {
			return calendarDuration{}, false
		}
		d := time.Duration(n)
		switch {
		case ch == 'W' && !inTime:
			days += d * 7 * 24 * time.Hour
		case ch == 'D' && !inTime:
			days += d * 24 * time.Hour
		case ch == 'H' && inTime:
			clock += d * time.Hour
		case ch == 'M' && inTime:
			clock += d * time.Minute
		case ch == 'S' && inTime:
			clock += d * time.Second
		default:
			return calendarDuration{}, false
		}
		digits = ""
		any = true
	}
	return calendarDuration{days * sign, clock * sign}, any && digits == ""
}

func icalRows(components []*icalComponent) [][]string {
	rows := [][]string{append([]string{}, icalColumns...)}
	for _, c := range components {
		start := c.first("DTSTART")
		endName := "DTEND"
		if c.typ == "VTODO" {
			endName = "DUE"
		}
		end := c.first(endName)
		endCell := dateCell(end, c.zones)
		if end == nil && start != nil {
			startDate, date, utc, ok := parseICalDate(start)
			span := c.first("DURATION")
			if ok && span != nil {
				if d, ok := parseDuration(span.value); ok {
					// Calendar days keep the local wall time; hours, minutes and seconds are elapsed time (RFC 5545).
					dayEnd := startDate.Add(d.days)
					computed := dayEnd.Add(d.clock)
					layout := "20060102T150405"
					if date {
						layout = "20060102"
					}
					v := computed.Format(layout)
					if utc && !date {
						v += "Z"
					}
					endCell = dateCell(&icalProperty{name: start.name, value: v, params: start.params}, c.zones)
					zone := start.param("TZID")
					if !date && !utc && d.clock != 0 && zone != "" {
						if dayOffset, ok := zoneOffset(zone, dayEnd, c.zones); ok {
							instant := dayEnd.Add(-dayOffset).Add(d.clock)
							if endOffset, ok := zoneOffsetAtInstant(zone, instant, c.zones); ok {
								endCell = dateCellAtOffset(instant.Add(endOffset), endOffset)
							}
						}
					}
				}
			}
		}
		val := func(name string) string {
			if p := c.first(name); p != nil {
				return p.value
			}
			return ""
		}
		typ := "event"
		if c.typ == "VTODO" {
			typ = "todo"
		}
		allDay, zone := "false", ""
		if start != nil {
			if strings.EqualFold(start.param("VALUE"), "DATE") {
				allDay = "true"
			}
			if start.param("VALUE") != "DATE" && len(start.value) != 8 {
				zone = start.param("TZID")
			}
		}
		organizer := ""
		if p := c.first("ORGANIZER"); p != nil {
			organizer = person(p)
		}
		var attendees, exdates []string
		for _, p := range c.all("ATTENDEE") {
			attendees = append(attendees, person(p))
		}
		for _, p := range c.all("EXDATE") {
			exdates = append(exdates, p.raw)
		}
		recurrence := ""
		if p := c.first("RECURRENCE-ID"); p != nil {
			recurrence = p.raw
		}
		rows = append(rows, []string{
			typ,
			icalUnescape(val("SUMMARY")),
			dateCell(start, c.zones),
			endCell,
			allDay,
			zone,
			icalUnescape(val("LOCATION")),
			icalUnescape(val("DESCRIPTION")),
			organizer,
			strings.Join(attendees, "\n"),
			val("RRULE"),
			strings.Join(exdates, "\n"),
			recurrence,
			val("UID"),
		})
	}
	return rows
}

func icalField(name string) int {
	for i, c := range icalColumns {
		if strings.EqualFold(name, c) {
			return i
		}
	}
	switch strings.ToLower(name) {
	case "summary", "subject", "제목", "标题":
		return 1
	case "begin", "시작", "开始":
		return 2
	case "finish", "종료", "结束":
		return 3
	case "장소", "地点":
		return 6
	case "notes", "설명", "描述":
		return 7
	}
	return -1
}

var errNoEvents = errors.New("The table has no iCalendar events or to-dos.")

func icalFromRows(rows [][]string) ([]*icalComponent, error) {
	if len(rows) == 0 {
		return nil, errNoEvents
	}
	fields := make([]int, len(rows[0]))
	hasTitle, hasStart := false, false
	for i, h := range rows[0] {
		fields[i] = icalField(h)
		hasTitle = hasTitle || fields[i] == 1
		hasStart = hasStart || fields[i] == 2
	}
	if !hasTitle && !hasStart {
		return nil, errNoEvents
	}
	firstOf := map[int]int{}
	for i := len(fields) - 1; i >= 0; i-- {
		firstOf[fields[i]] = i
	}
	var result []*icalComponent
	for _, row := range rows[1:] {
		cells := make([]string, len(icalColumns))
		for i := 0; i < min(len(fields), len(row)); i++ {
			if f := fields[i]; f >= 0 && cells[f] == "" && firstOf[f] == i {
				cells[f] = row[i]
			}
		}
		todo := strings.EqualFold(cells[0], "todo")
		if _, _, ok := tryCsvDate(cells[2]); !todo && !ok {
			continue
		}
		typ := "VEVENT"
		if todo {
			typ = "VTODO"
		}
		c := newComponent(typ, nil)
		c.cells = cells
		result = append(result, c)
	}
	return result, nil
}

// tryCsvDate reads a Start / End cell: "2026-10-16", "2026-10-16T09:00:00Z",
// "…+09:00" or a floating "2026-10-16T09:00:00" (as UTC).
func tryCsvDate(t string) (time.Time, bool, bool) {
	if len(t) == 10 {
		if d, err := time.Parse("2006-01-02", t); err == nil {
			return d, true, true
		}
	}
	for _, layout := range []string{"2006-01-02T15:04:05Z", "2006-01-02T15:04:05-07:00", "2006-01-02T15:04:05"} {
		if d, err := time.Parse(layout, t); err == nil {
			return d, false, true
		}
	}
	return time.Time{}, false, false
}

func icalEscape(t string) string {
	return strings.NewReplacer(`\`, `\\`, "\r\n", `\n`, "\r", `\n`, "\n", `\n`, ";", `\;`, ",", `\,`).Replace(t)
}

func dateLine(name, t, zone string, loc *time.Location) string {
	instant, date, ok := tryCsvDate(t)
	if !ok {
		return ""
	}
	if date {
		return name + ";VALUE=DATE:" + instant.Format("20060102")
	}
	raw, _ := time.Parse("2006-01-02T15:04:05", t[:19])
	if loc != nil && zone != "" {
		local := raw
		_, s := instant.Zone()
		if !(len(t) == 19 || (!strings.HasSuffix(t, "Z") && time.Duration(s)*time.Second == wallOffset(loc, raw))) {
			w := instant.In(loc)
			local = time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), w.Second(), 0, time.UTC)
		}
		return name + ";TZID=" + zone + ":" + local.Format("20060102T150405")
	}
	if len(t) == 19 {
		return name + ":" + raw.Format("20060102T150405")
	}
	return name + ":" + instant.UTC().Format("20060102T150405Z")
}

func personLine(name, cell string) string {
	cell = strings.NewReplacer("\r", "", "\n", "").Replace(cell)
	marker := strings.LastIndex(cell, " <")
	display := ""
	if marker >= 0 && strings.HasSuffix(cell, ">") {
		display = cell[:marker]
	}
	address := cell
	if display != "" {
		address = cell[marker+2 : len(cell)-1]
	}
	if !strings.Contains(address, ":") {
		address = "mailto:" + address
	}
	if display == "" {
		return name + ":" + address
	}
	display = strings.ReplaceAll(display, `"`, "")
	if strings.ContainsAny(display, ";:,") {
		display = `"` + display + `"`
	}
	return name + ";CN=" + display + ":" + address
}

func cleanZoneID(cell string) string {
	if strings.ContainsAny(cell, "\r\n:;\"") {
		return ""
	}
	return cell
}

// now is replaced in tests.
var now = time.Now

func writeICal(components []*icalComponent) string {
	var out strings.Builder
	foldLine(&out, "BEGIN:VCALENDAR")
	foldLine(&out, "VERSION:2.0")
	foldLine(&out, "PRODID:-//Filee//Filee//EN")
	type zoneUse struct {
		id       string
		loc      *time.Location
		instant  time.Time
		hasStart bool
	}
	var zones []*zoneUse
	find := func(id string) *zoneUse {
		for _, z := range zones {
			if strings.EqualFold(z.id, id) {
				return z
			}
		}
		return nil
	}
	for _, c := range components {
		id := cleanZoneID(c.cells[5])
		loc := loadZone(id)
		if loc == nil {
			continue
		}
		start, startDate, parsedStart := tryCsvDate(c.cells[2])
		hasStart := c.cells[2] != "" && parsedStart && !startDate
		var candidate *time.Time
		if hasStart {
			candidate = &start
		} else if end, endDate, ok := tryCsvDate(c.cells[3]); ok && !endDate {
			candidate = &end
		}
		if candidate == nil {
			continue
		}
		prev := find(id)
		switch {
		case prev == nil:
			zones = append(zones, &zoneUse{id, loc, *candidate, hasStart})
		case hasStart && !prev.hasStart || hasStart == prev.hasStart && candidate.Before(prev.instant):
			prev.loc, prev.instant, prev.hasStart = loc, *candidate, hasStart
		}
	}
	for _, z := range zones {
		for _, line := range strings.Split(buildVTimeZone(z.loc, z.instant, z.id), "\r\n") {
			if line != "" {
				foldLine(&out, line)
			}
		}
	}
	for _, c := range components {
		cells := c.cells
		id := cleanZoneID(cells[5])
		var loc *time.Location
		if z := find(id); z != nil && id != "" {
			loc = z.loc
		}
		foldLine(&out, "BEGIN:"+c.typ)
		uid := cells[13]
		if uid == "" || strings.ContainsAny(uid, "\r\n") {
			sum := sha256.Sum256([]byte(strings.Join(cells, "\x00")))
			uid = hex.EncodeToString(sum[:])[:16] + "@filee"
		}
		foldLine(&out, "UID:"+uid)
		foldLine(&out, "DTSTAMP:"+now().UTC().Format("20060102T150405Z"))
		if cells[1] != "" {
			foldLine(&out, "SUMMARY:"+icalEscape(cells[1]))
		}
		if l := dateLine("DTSTART", cells[2], id, loc); l != "" {
			foldLine(&out, l)
		}
		endName := "DTEND"
		if c.typ == "VTODO" {
			endName = "DUE"
		}
		if l := dateLine(endName, cells[3], id, loc); l != "" {
			foldLine(&out, l)
		}
		if cells[6] != "" {
			foldLine(&out, "LOCATION:"+icalEscape(cells[6]))
		}
		if cells[7] != "" {
			foldLine(&out, "DESCRIPTION:"+icalEscape(cells[7]))
		}
		if cells[8] != "" {
			foldLine(&out, personLine("ORGANIZER", cells[8]))
		}
		for _, a := range strings.Split(cells[9], "\n") {
			if a != "" {
				foldLine(&out, personLine("ATTENDEE", a))
			}
		}
		if cells[10] != "" && !strings.ContainsAny(cells[10], "\r\n") {
			foldLine(&out, "RRULE:"+cells[10])
		}
		for _, f := range []struct {
			index int
			name  string
		}{{11, "EXDATE"}, {12, "RECURRENCE-ID"}} {
			for _, line := range splitLines(cells[f.index]) {
				if line == "" {
					continue
				}
				u := strings.ToUpper(line)
				if strings.HasPrefix(u, f.name+":") || strings.HasPrefix(u, f.name+";") {
					foldLine(&out, line)
				}
			}
		}
		foldLine(&out, "END:"+c.typ)
	}
	foldLine(&out, "END:VCALENDAR")
	return out.String()
}

type tzTransition struct {
	instant  time.Time
	local    time.Time // wall time before the change, in time.UTC
	from, to time.Duration
}

func (t tzTransition) daylight() bool { return t.to > t.from }

func zoneTransitions(loc *time.Location, year int) []tzTransition {
	var found []tzTransition
	until := time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC)
	previous := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	offset := offsetAt(loc, previous)
	for next := previous.Add(time.Hour); !next.After(until); next = next.Add(time.Hour) {
		current := offsetAt(loc, next)
		if current != offset {
			lower, upper := previous, next
			for upper.Sub(lower) > time.Minute {
				middle := lower.Add(time.Duration(upper.Sub(lower).Minutes()/2) * time.Minute)
				if offsetAt(loc, middle) == offset {
					lower = middle
				} else {
					upper = middle
				}
			}
			found = append(found, tzTransition{upper, upper.Add(offset), offset, current})
		}
		previous = next
		offset = current
	}
	return found
}

func weekdayCode(t time.Time) string { return strings.ToUpper(t.Weekday().String()[:2]) }

func tzRule(tr tzTransition, loc *time.Location, year int) string {
	day := tr.local
	weekday := weekdayCode(day)
	nth := (day.Day()-1)/7 + 1
	candidates := []string{
		"BYDAY=-1" + weekday,
		fmt.Sprintf("BYDAY=%d%s", nth, weekday),
		fmt.Sprintf("BYMONTHDAY=%d", day.Day()),
	}
	var matches []tzTransition
	for y := year; y < year+3; y++ {
		for _, t := range zoneTransitions(loc, y) {
			if t.daylight() == tr.daylight() && t.local.Month() == day.Month() {
				matches = append(matches, t)
			}
		}
	}
	for _, candidate := range candidates {
		if len(matches) != 3 {
			break
		}
		all := true
		for _, t := range matches {
			week := strconv.Itoa((t.local.Day()-1)/7 + 1)
			if t.local.AddDate(0, 0, 7).Month() != t.local.Month() {
				week = "-1"
			}
			if candidate != fmt.Sprintf("BYMONTHDAY=%d", t.local.Day()) && candidate != "BYDAY="+week+weekdayCode(t.local) {
				all = false
				break
			}
		}
		if all {
			return fmt.Sprintf("FREQ=YEARLY;BYMONTH=%d;%s", int(day.Month()), candidate)
		}
	}
	return fmt.Sprintf("FREQ=YEARLY;BYMONTH=%d;BYDAY=%d%s", int(day.Month()), nth, weekday)
}

func offsetText(o time.Duration) string {
	sign := "+"
	if o < 0 {
		sign, o = "-", -o
	}
	return fmt.Sprintf("%s%02d%02d", sign, int(o/time.Hour), int(o%time.Hour/time.Minute))
}

// buildVTimeZone builds observances from the zone's actual UTC transitions in
// the year of instant.
func buildVTimeZone(loc *time.Location, instant time.Time, tzid string) string {
	year := instant.Year() // the year at the instant's own offset, as DateTimeOffset.Year
	transitions := zoneTransitions(loc, year)
	var out strings.Builder
	foldLine(&out, "BEGIN:VTIMEZONE")
	foldLine(&out, "TZID:"+tzid)
	if len(transitions) < 2 {
		offset := offsetText(offsetAt(loc, instant))
		foldLine(&out, "BEGIN:STANDARD")
		foldLine(&out, "DTSTART:19700101T000000")
		foldLine(&out, "TZOFFSETFROM:"+offset)
		foldLine(&out, "TZOFFSETTO:"+offset)
		foldLine(&out, "END:STANDARD")
	} else {
		for _, t := range transitions {
			typ := "STANDARD"
			if t.daylight() {
				typ = "DAYLIGHT"
			}
			foldLine(&out, "BEGIN:"+typ)
			foldLine(&out, "DTSTART:"+t.local.Format("20060102T150405"))
			foldLine(&out, "TZOFFSETFROM:"+offsetText(t.from))
			foldLine(&out, "TZOFFSETTO:"+offsetText(t.to))
			foldLine(&out, "RRULE:"+tzRule(t, loc, year))
			foldLine(&out, "END:"+typ)
		}
	}
	foldLine(&out, "END:VTIMEZONE")
	return out.String()
}
