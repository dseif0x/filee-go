package data

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dseif0x/filee-go/internal/engines/subtitles"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/korean"
)

// vCard (.vcf) contacts ↔ CSV, one row per contact. Reads vCard 2.1, 3.0 and
// 4.0: folded lines, quoted-printable (with its "=" soft line breaks), CHARSET
// parameters, escaped characters (\n \, \; \\), grouped names (item1.EMAIL), and
// 2.1's bare TYPE parameters (TEL;CELL;PREF). A value without a CHARSET is UTF-8
// when it is valid UTF-8 and CP949 otherwise, as Korean phones exported their
// address books; UTF-16 files with a BOM are read too. Pictures (PHOTO, LOGO)
// and other binary values are skipped.
// CSV → vCard reads the columns (and common other names for them, also in
// Korean and Chinese) and writes vCard 3.0 in UTF-8.

type kindValue struct {
	Value string
	Kind  string // "" for none
}

type contact struct {
	FullName, GivenName, FamilyName, Organization, Title string
	Emails, Phones, Addresses                            []kindValue
	Birthday                                             string
	Notes                                                []string
}

func (c *contact) isEmpty() bool {
	return c.FullName == "" && c.GivenName == "" && c.FamilyName == "" && c.Organization == "" && len(c.Emails) == 0 && len(c.Phones) == 0
}

var vcardColumns = []string{"Full name", "Given name", "Family name", "Organization", "Title", "Email", "Phone", "Address", "Birthday", "Note"}

const vcardSeparator = "; "

// latin1 keeps every byte as one rune: the structure is ASCII, values are decoded per property.
func latin1(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

func latin1Bytes(s string) []byte {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		b = append(b, byte(r))
	}
	return b
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n")
}

func readVCard(b []byte) []*contact {
	if bytes.HasPrefix(b, []byte{0xFF, 0xFE}) || bytes.HasPrefix(b, []byte{0xFE, 0xFF}) {
		b = []byte(subtitles.DecodeWith(b, 1252, false))
	} else {
		b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	}
	var contacts []*contact
	var c *contact
	depth := 0
	for _, line := range unfoldVCard(splitLines(latin1(b))) {
		p := parseVProperty(line)
		if p == nil {
			continue
		}
		switch {
		case p.name == "BEGIN" && strings.EqualFold(strings.TrimSpace(p.raw), "VCARD"):
			if depth == 0 {
				c = &contact{}
			}
			depth++
			continue
		case p.name == "END" && strings.EqualFold(strings.TrimSpace(p.raw), "VCARD"):
			if depth > 0 {
				depth--
				if depth == 0 && c != nil {
					finishContact(c)
					contacts = append(contacts, c)
					c = nil
				}
			}
			continue
		}
		// Properties of a nested card (2.1 AGENT) are not this contact's.
		if c != nil && depth == 1 {
			applyVProperty(c, p)
		}
	}
	return contacts
}

// unfoldVCard joins folded lines (a line starting with a space or tab continues
// the previous one) and quoted-printable soft line breaks.
func unfoldVCard(lines []string) []string {
	var out []string
	var cur strings.Builder
	started := false
	for _, line := range lines {
		// A soft line break first: the next line's leading space is part of the value then.
		if started && cur.Len() > 0 && strings.HasSuffix(cur.String(), "=") && isQuotedPrintable(cur.String()) {
			s := cur.String()
			cur.Reset()
			cur.WriteString(s[:len(s)-1])
			cur.WriteString(line)
			continue
		}
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

func isQuotedPrintable(line string) bool {
	colon := strings.IndexByte(line, ':')
	return colon > 0 && strings.Contains(strings.ToUpper(line[:colon]), "QUOTED-PRINTABLE")
}

func applyVProperty(c *contact, p *vProperty) {
	if p.binary {
		return
	}
	switch p.name {
	case "FN":
		c.FullName = strings.TrimSpace(vUnescape(p.value))
	case "N":
		name := vComponents(p.value)
		c.FamilyName = ""
		if len(name) > 0 {
			c.FamilyName = strings.TrimSpace(name[0])
		}
		var given []string
		for i := 1; i < len(name) && i < 3; i++ {
			if n := strings.TrimSpace(name[i]); n != "" {
				given = append(given, n)
			}
		}
		c.GivenName = strings.Join(given, " ")
	case "ORG":
		var parts []string
		for _, o := range vComponents(p.value) {
			if o = strings.TrimSpace(o); o != "" {
				parts = append(parts, o)
			}
		}
		c.Organization = strings.Join(parts, ", ")
	case "TITLE":
		c.Title = strings.TrimSpace(vUnescape(p.value))
	case "EMAIL":
		c.Emails = addValue(c.Emails, stripScheme(vUnescape(p.value), "mailto:"), p.kind)
	case "TEL":
		c.Phones = addValue(c.Phones, stripScheme(vUnescape(p.value), "tel:"), p.kind)
	case "ADR":
		var parts []string
		for _, a := range vComponents(p.value) {
			if a = reNewLines.ReplaceAllString(strings.TrimSpace(a), ", "); a != "" {
				parts = append(parts, a)
			}
		}
		c.Addresses = addValue(c.Addresses, strings.Join(parts, ", "), p.kind)
	case "BDAY":
		c.Birthday = birthday(strings.TrimSpace(vUnescape(p.value)))
	case "NOTE":
		if note := strings.TrimSpace(vUnescape(p.value)); note != "" {
			c.Notes = append(c.Notes, note)
		}
	}
}

var reNewLines = regexp.MustCompile(`\r\n|\n|\r`)

func addValue(list []kindValue, v, kind string) []kindValue {
	if v = strings.TrimSpace(v); v != "" {
		list = append(list, kindValue{v, kind})
	}
	return list
}

func stripScheme(v, scheme string) string {
	if len(v) >= len(scheme) && strings.EqualFold(v[:len(scheme)], scheme) {
		return v[len(scheme):]
	}
	return v
}

// birthday turns "19900131" into "1990-01-31"; other forms as written.
func birthday(v string) string {
	if len(v) == 8 && strings.Trim(v, "0123456789") == "" {
		return v[:4] + "-" + v[4:6] + "-" + v[6:]
	}
	return v
}

// finishContact makes a missing full name (common in 2.1 files) from the name
// parts: 홍길동, or "John Smith".
func finishContact(c *contact) {
	if c.FullName != "" {
		return
	}
	family, given := c.FamilyName, c.GivenName
	switch {
	case family == "" || given == "":
		c.FullName = family + given
	case isCJK(family) && isCJK(given):
		c.FullName = family + given
	default:
		c.FullName = given + " " + family
	}
}

func isCJK(s string) bool {
	for _, r := range s {
		if !(r >= '가' && r <= '힣' || r >= 'ᄀ' && r <= 'ᇿ' || r >= '㄰' && r <= '㆏' || r >= '一' && r <= '鿿' || r >= '぀' && r <= 'ヿ') {
			return false
		}
	}
	return true
}

// vComponents splits a structured value (N, ADR, ORG) at unescaped ";", each part unescaped.
func vComponents(v string) []string {
	var parts []string
	var cur strings.Builder
	rs := []rune(v)
	for i := 0; i < len(rs); i++ {
		switch {
		case rs[i] == '\\' && i+1 < len(rs):
			cur.WriteRune(rs[i])
			cur.WriteRune(rs[i+1])
			i++
		case rs[i] == ';':
			parts = append(parts, vUnescape(cur.String()))
			cur.Reset()
		default:
			cur.WriteRune(rs[i])
		}
	}
	return append(parts, vUnescape(cur.String()))
}

func vUnescape(v string) string {
	if !strings.Contains(v, `\`) {
		return v
	}
	var b strings.Builder
	rs := []rune(v)
	for i := 0; i < len(rs); i++ {
		if rs[i] != '\\' || i+1 >= len(rs) {
			b.WriteRune(rs[i])
			continue
		}
		i++
		if rs[i] == 'n' || rs[i] == 'N' {
			b.WriteByte('\n')
		} else {
			b.WriteRune(rs[i])
		}
	}
	return b.String()
}

// vProperty is one content line: name (group removed), parameters and the value decoded to text.
type vProperty struct {
	name   string
	raw    string // as written (Latin-1: one rune per byte), for BEGIN / END
	value  string
	binary bool
	kind   string
}

func parseVProperty(line string) *vProperty {
	colon := -1
	quoted := false
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
	head := splitParameters(line[:colon])
	name := head[0]
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		name = name[dot+1:]
	}
	p := &vProperty{name: strings.ToUpper(strings.TrimSpace(name)), raw: line[colon+1:]}
	var encoding, charset string
	var types []string
	for _, param := range head[1:] {
		eq := strings.IndexByte(param, '=')
		key, value := "TYPE", param
		if eq >= 0 {
			key, value = param[:eq], param[eq+1:]
		}
		key = strings.ToUpper(strings.TrimSpace(key))
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch key {
		case "ENCODING":
			encoding = strings.ToUpper(value)
		case "CHARSET":
			charset = value
		case "TYPE":
			for _, t := range strings.Split(value, ",") {
				if t = strings.TrimSpace(t); t != "" {
					types = append(types, t)
				}
			}
		case "VALUE":
			if strings.EqualFold(value, "binary") {
				p.binary = true
			}
		}
		// 2.1 also writes the encoding bare: TEL;QUOTED-PRINTABLE:…
		if u := strings.ToUpper(value); eq < 0 && (u == "QUOTED-PRINTABLE" || u == "BASE64" || u == "B") {
			encoding = u
		}
	}
	if encoding == "BASE64" || encoding == "B" {
		p.binary = true
		return p
	}
	b := latin1Bytes(p.raw)
	if encoding == "QUOTED-PRINTABLE" {
		b = decodeQuotedPrintable(b)
	}
	p.value = decodeCharset(b, charset)
	p.kind = kindOf(types, p.name)
	return p
}

// splitParameters splits "N;CHARSET=UTF-8;TYPE=\"a;b\"" into name and parameters, ";" inside quotes kept.
func splitParameters(head string) []string {
	var parts []string
	var cur strings.Builder
	quoted := false
	for _, ch := range head {
		if ch == '"' {
			quoted = !quoted
		}
		if ch == ';' && !quoted {
			parts = append(parts, cur.String())
			cur.Reset()
		} else {
			cur.WriteRune(ch)
		}
	}
	return append(parts, cur.String())
}

func isHex(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'A' && b <= 'F' || b >= 'a' && b <= 'f'
}

func decodeQuotedPrintable(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		switch {
		case b[i] == '=' && i+2 < len(b) && isHex(b[i+1]) && isHex(b[i+2]):
			n, _ := strconv.ParseUint(string(b[i+1:i+3]), 16, 8)
			out = append(out, byte(n))
			i += 2
		case b[i] == '=' && i == len(b)-1:
			// A soft line break left at the very end.
		default:
			out = append(out, b[i])
		}
	}
	return out
}

// decodeCharset decodes by the declared charset, else as UTF-8 when the bytes
// are valid UTF-8, else as CP949.
func decodeCharset(b []byte, charset string) string {
	if charset != "" {
		if e := subtitles.Charset(charset); e != nil {
			if out, err := e.NewDecoder().Bytes(b); err == nil {
				return string(out)
			}
		}
	}
	if utf8.Valid(b) {
		return string(b)
	}
	out, err := korean.EUCKR.NewDecoder().Bytes(b)
	if err != nil {
		out, _ = charmap.Windows1252.NewDecoder().Bytes(b)
	}
	return string(out)
}

// kindOf is the kind shown in brackets: mobile, home, work, fax, pager or main; "" for none of these.
func kindOf(types []string, property string) string {
	var kinds []string
	for _, t := range types {
		kind := ""
		switch strings.ToUpper(t) {
		case "CELL":
			if property == "TEL" {
				kind = "mobile"
			}
		case "HOME":
			kind = "home"
		case "WORK":
			kind = "work"
		case "FAX":
			if property == "TEL" {
				kind = "fax"
			}
		case "PAGER":
			if property == "TEL" {
				kind = "pager"
			}
		case "MAIN":
			if property == "TEL" {
				kind = "main"
			}
		}
		if kind != "" && !contains(kinds, kind) {
			kinds = append(kinds, kind)
		}
	}
	return strings.Join(kinds, ", ")
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ---- CSV ----

func vcardRows(contacts []*contact) [][]string {
	rows := [][]string{append([]string{}, vcardColumns...)}
	for _, c := range contacts {
		rows = append(rows, []string{
			c.FullName, c.GivenName, c.FamilyName, c.Organization, c.Title,
			joinKinds(c.Emails), joinKinds(c.Phones), joinKinds(c.Addresses), c.Birthday, strings.Join(c.Notes, "\n"),
		})
	}
	return rows
}

func joinKinds(values []kindValue) string {
	parts := make([]string, len(values))
	for i, v := range values {
		if v.Kind == "" {
			parts[i] = v.Value
		} else {
			parts[i] = v.Value + " (" + v.Kind + ")"
		}
	}
	return strings.Join(parts, vcardSeparator)
}

// vcardFromRows reads contacts from CSV rows (the first row names the columns).
func vcardFromRows(rows [][]string) ([]*contact, error) {
	if len(rows) == 0 {
		return nil, errors.New("The CSV file is empty.")
	}
	fields := make([]string, len(rows[0]))
	anyField := false
	for i, h := range rows[0] {
		fields[i] = vcardField(h)
		anyField = anyField || fields[i] != ""
	}
	if !anyField {
		return nil, errors.New("No contact columns found. Name the columns Full name, Email, Phone, Address and so on.")
	}
	var contacts []*contact
	for _, row := range rows[1:] {
		c := &contact{}
		for i := 0; i < len(row) && i < len(fields); i++ {
			v := strings.TrimSpace(row[i])
			if v == "" {
				continue
			}
			switch fields[i] {
			case "FN":
				c.FullName = v
			case "GIVEN":
				c.GivenName = v
			case "FAMILY":
				c.FamilyName = v
			case "ORG":
				c.Organization = v
			case "TITLE":
				c.Title = v
			case "EMAIL":
				c.Emails = append(c.Emails, splitKinds(v)...)
			case "TEL":
				c.Phones = append(c.Phones, splitKinds(v)...)
			case "ADR":
				c.Addresses = append(c.Addresses, splitKinds(v)...)
			case "BDAY":
				c.Birthday = v
			case "NOTE":
				c.Notes = append(c.Notes, v)
			}
		}
		if c.isEmpty() {
			continue
		}
		finishContact(c)
		if c.FullName == "" {
			switch {
			case c.Organization != "":
				c.FullName = c.Organization
			case len(c.Emails) > 0:
				c.FullName = c.Emails[0].Value
			case len(c.Phones) > 0:
				c.FullName = c.Phones[0].Value
			}
		}
		contacts = append(contacts, c)
	}
	if len(contacts) == 0 {
		return nil, errors.New("The CSV file has no contacts.")
	}
	return contacts, nil
}

var reKindSuffix = regexp.MustCompile(`^(.*?)\s*\(([^()]*)\)$`)

// splitKinds reads "a@x.com (work); b@y.com" as values with their kind.
func splitKinds(cell string) []kindValue {
	var out []kindValue
	for _, part := range strings.Split(cell, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if m := reKindSuffix.FindStringSubmatch(part); m != nil {
			out = append(out, kindValue{strings.TrimSpace(m[1]), strings.TrimSpace(m[2])})
		} else {
			out = append(out, kindValue{part, ""})
		}
	}
	return out
}

// vcardField is the vCard field a CSV column holds, by its header (English,
// Korean or Chinese), or "".
func vcardField(header string) string {
	var b strings.Builder
	for _, r := range header {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	switch b.String() {
	case "fullname", "name", "displayname", "이름", "성명", "姓名", "名称":
		return "FN"
	case "givenname", "firstname", "given", "first", "名":
		return "GIVEN"
	case "familyname", "lastname", "surname", "family", "last", "성", "姓":
		return "FAMILY"
	case "organization", "organisation", "company", "org", "회사", "소속", "公司", "单位":
		return "ORG"
	case "title", "jobtitle", "직함", "직책", "직위", "职位", "职务":
		return "TITLE"
	case "email", "emails", "emailaddress", "mail", "이메일", "전자메일", "메일", "邮箱", "电子邮件":
		return "EMAIL"
	case "phone", "phones", "telephone", "tel", "mobile", "phonenumber", "cell", "전화", "전화번호", "휴대폰", "휴대전화", "연락처", "电话", "手机":
		return "TEL"
	case "address", "addresses", "주소", "地址":
		return "ADR"
	case "birthday", "bday", "birthdate", "dateofbirth", "생일", "생년월일", "生日":
		return "BDAY"
	case "note", "notes", "memo", "메모", "비고", "备注":
		return "NOTE"
	}
	return ""
}

// ---- writing ----

// writeVCard writes vCard 3.0, UTF-8, CRLF, lines folded at 75 bytes.
func writeVCard(contacts []*contact) string {
	var b strings.Builder
	for _, c := range contacts {
		foldLine(&b, "BEGIN:VCARD")
		foldLine(&b, "VERSION:3.0")
		foldLine(&b, "FN:"+vEscape(c.FullName))
		foldLine(&b, fmt.Sprintf("N:%s;%s;;;", vEscape(c.FamilyName), vEscape(c.GivenName)))
		if c.Organization != "" {
			foldLine(&b, "ORG:"+vEscape(c.Organization))
		}
		if c.Title != "" {
			foldLine(&b, "TITLE:"+vEscape(c.Title))
		}
		for _, e := range c.Emails {
			foldLine(&b, "EMAIL;TYPE="+strings.Join(append([]string{"INTERNET"}, vTypes(e.Kind, "EMAIL")...), ",")+":"+vEscape(e.Value))
		}
		for _, p := range c.Phones {
			if t := vTypes(p.Kind, "TEL"); len(t) > 0 {
				foldLine(&b, "TEL;TYPE="+strings.Join(t, ",")+":"+vEscape(p.Value))
			} else {
				foldLine(&b, "TEL:"+vEscape(p.Value))
			}
		}
		// The address as one street line: the parts can't be told apart reliably from a single cell.
		for _, a := range c.Addresses {
			head := "ADR"
			if t := vTypes(a.Kind, "ADR"); len(t) > 0 {
				head += ";TYPE=" + strings.Join(t, ",")
			}
			foldLine(&b, head+":;;"+vEscape(a.Value)+";;;;")
		}
		if c.Birthday != "" {
			foldLine(&b, "BDAY:"+vEscape(c.Birthday))
		}
		if len(c.Notes) > 0 {
			foldLine(&b, "NOTE:"+vEscape(strings.Join(c.Notes, "\n")))
		}
		foldLine(&b, "END:VCARD")
	}
	return b.String()
}

func vTypes(kind, property string) []string {
	var types []string
	for _, word := range strings.FieldsFunc(kind, func(r rune) bool { return r == ',' || r == ' ' }) {
		t := ""
		switch strings.ToLower(word) {
		case "mobile", "cell", "휴대폰", "手机":
			if property == "TEL" {
				t = "CELL"
			}
		case "home", "집", "家":
			t = "HOME"
		case "work", "office", "직장", "회사", "工作":
			t = "WORK"
		case "fax", "팩스", "传真":
			if property == "TEL" {
				t = "FAX"
			}
		case "pager":
			if property == "TEL" {
				t = "PAGER"
			}
		case "main":
			if property == "TEL" {
				t = "MAIN"
			}
		}
		if t != "" && !contains(types, t) {
			types = append(types, t)
		}
	}
	return types
}

func vEscape(v string) string {
	return strings.NewReplacer(`\`, `\\`, "\r\n", `\n`, "\r", `\n`, "\n", `\n`, ",", `\,`, ";", `\;`).Replace(v)
}

// foldLine appends a content line folded at 75 bytes (RFC 6350), never inside a character.
func foldLine(b *strings.Builder, line string) {
	n := 0
	for _, r := range line {
		size := utf8.RuneLen(r)
		if n+size > 75 {
			b.WriteString("\r\n ")
			n = 1
		}
		b.WriteRune(r)
		n += size
	}
	b.WriteString("\r\n")
}
