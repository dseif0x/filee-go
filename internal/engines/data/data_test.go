package data

import (
	"fmt"
	"os"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
	"golang.org/x/text/encoding/korean"

	"github.com/dseif0x/filee-go/internal/convert"
)

const sample = `{
  "name": "Filee 변환기",
  "version": 3,
  "ratio": 3.0,
  "big": 12345678901234567890,
  "enabled": true,
  "missing": null,
  "notes": "first line\nsecond line",
  "looksLikeABoolean": "true",
  "looksLikeANumber": "007",
  "empty": "",
  "tags": ["a", "b"],
  "nothing": [],
  "owner": { "id": 1, "email": "me@example.com" },
  "rows": [ { "x": 1 }, { "x": 2 } ]
}`

func TestJSONToYAMLKeepsTypesQuotesAmbiguousTextAndIndentsByTwo(t *testing.T) {
	yaml := convertText(t, "sample.json", sample, "yaml")
	eq(t, lines(
		"name: Filee 변환기",
		"version: 3",
		"ratio: 3.0",
		"big: 12345678901234567890",
		"enabled: true",
		"missing: null",
		"notes: |-",
		"  first line",
		"  second line",
		`looksLikeABoolean: "true"`,
		`looksLikeANumber: "007"`,
		`empty: ""`,
		"tags:",
		"  - a",
		"  - b",
		"nothing: []",
		"owner:",
		"  id: 1",
		"  email: me@example.com",
		"rows:",
		"  - x: 1",
		"  - x: 2",
		""), yaml)
}

func TestJSONRoundTripsThroughYAMLTOMLAndXML(t *testing.T) {
	yamlPath, err := run(t, writeFile(t, "sample.json", []byte(sample)), "yaml")
	if err != nil {
		t.Fatal(err)
	}
	backPath, err := run(t, yamlPath, "json")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(backPath)
	back := string(b)
	has(t, back, `"name": "Filee 변환기"`)
	has(t, back, `"ratio": 3.0,`)
	has(t, back, `"big": 12345678901234567890,`)
	has(t, back, `"missing": null,`)
	has(t, back, `"looksLikeABoolean": "true",`)
	has(t, back, `"notes": "first line\nsecond line",`)
	has(t, back, "  \"tags\": [\n    \"a\",")

	tm := convertText(t, "sample.json", sample, "toml")
	hasNot(t, tm, "missing")
	has(t, tm, "big = \"12345678901234567890\"\n")
	has(t, tm, "ratio = 3.0\n")
	has(t, tm, "\n[owner]\nid = 1\n")
	has(t, tm, "\n[[rows]]\nx = 1\n\n[[rows]]\nx = 2\n")
	var table map[string]any
	if err := toml.Unmarshal([]byte(tm), &table); err != nil {
		t.Fatal(err)
	}
	if table["name"] != "Filee 변환기" || table["version"] != int64(3) {
		t.Fatal(table)
	}

	x := convertText(t, "sample.json", sample, "xml")
	if !strings.HasPrefix(x, "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<root>\n  <name>Filee 변환기</name>\n") {
		t.Fatal(x)
	}
	has(t, x, "  <missing />\n")
	has(t, x, "  <empty></empty>\n")
	has(t, x, "  <tags>a</tags>\n  <tags>b</tags>\n")
}

func TestYAMLAnchorsMergeKeysAndCoreSchemaTypes(t *testing.T) {
	src := lines(
		"# comments are dropped",
		"defaults: &defaults",
		"  adapter: postgres",
		"  port: 5432",
		"development:",
		"  <<: *defaults",
		"  port: 5433",
		"  database: dev",
		"flags: [yes, no, on, ~, 0x1F, 0o17, .inf, -1.5e3, '42']",
		"when: 2026-10-16",
		"text: |",
		"  line one",
		"  line two")
	json := convertText(t, "config.yml", src, "json")
	has(t, json, lines(
		`  "development": {`,
		`    "adapter": "postgres",`,
		`    "port": 5433,`,
		`    "database": "dev"`,
		`  },`))
	has(t, json, lines(
		`  "flags": [`,
		`    "yes",`,
		`    "no",`,
		`    "on",`,
		`    null,`,
		`    31,`,
		`    15,`,
		`    "Infinity",`,
		`    -1500.0,`,
		`    "42"`,
		`  ],`))
	has(t, json, `"when": "2026-10-16"`)
	has(t, json, `"text": "line one\nline two"`)
	hasNot(t, json, "comments")
}

func TestSeveralYAMLDocumentsAndAliasBombs(t *testing.T) {
	json := convertText(t, "docs.yaml", "a: 1\n---\nb: 2\n", "json")
	eq(t, "[\n  {\n    \"a\": 1\n  },\n  {\n    \"b\": 2\n  }\n]\n", json)

	has(t, fail(t, "bad.yaml", "key: [unclosed\n", "json"), "Not valid YAML")

	var bomb strings.Builder
	bomb.WriteString("l0: &l0 [x, x, x, x, x, x, x, x, x, x]\n")
	for i := 1; i <= 8; i++ {
		refs := make([]string, 10)
		for j := range refs {
			refs[j] = fmt.Sprintf("*l%d", i-1)
		}
		fmt.Fprintf(&bomb, "l%d: &l%d [%s]\n", i, i, strings.Join(refs, ", "))
	}
	has(t, fail(t, "bomb.yaml", bomb.String(), "json"), "too many values")
}

func TestTOMLDatesTablesAndArraysOfTables(t *testing.T) {
	src := lines(
		`title = "TOML 예제"`,
		`released = 1979-05-27T07:32:00-08:00`,
		`day = 2026-10-16`,
		``,
		`[server]`,
		`ports = [8000, 8001]`,
		`limits = { cpu = 2.5, memory = "1G" }`,
		``,
		`[[products]]`,
		`name = "Hammer"`,
		`sku = 738594937`,
		``,
		`[[products]]`,
		`name = "Nail"`)
	json := convertText(t, "config.toml", src, "json")
	has(t, json, `"title": "TOML 예제"`)
	has(t, json, `"released": "1979-05-27T07:32:00-08:00"`)
	has(t, json, `"day": "2026-10-16"`)
	has(t, json, "\"limits\": {\n      \"cpu\": 2.5,\n      \"memory\": \"1G\"\n    }")
	has(t, json, "\"products\": [\n    {\n      \"name\": \"Hammer\",\n      \"sku\": 738594937\n    },")

	yaml := convertText(t, "config.toml", src, "yaml")
	has(t, yaml, "released: 1979-05-27T07:32:00-08:00\n")
	has(t, yaml, "products:\n  - name: Hammer\n    sku: 738594937\n  - name: Nail\n")

	// And back to TOML: the same layout.
	back := convertText(t, "config.json", json, "toml")
	has(t, back, "title = \"TOML 예제\"\nreleased = \"1979-05-27T07:32:00-08:00\"\n")
	has(t, back, "\n[server]\nports = [8000, 8001]\n\n[server.limits]\ncpu = 2.5\nmemory = \"1G\"\n")
	has(t, fail(t, "bad.toml", "a = 1\na = 2\n", "json"), "Not valid TOML")
}

func TestARootThatIsNotATableGoesUnderItemsInTOML(t *testing.T) {
	eq(t, "[[items]]\na = 1\n\n[[items]]\na = 2\n", convertText(t, "list.json", `[{"a": 1, "b": null}, {"a": 2}]`, "toml"))
	eq(t, "items = \"just text\"\n", convertText(t, "value.json", `"just text"`, "toml"))
}

func TestXMLAttributesRepeatsTextAndNamespaces(t *testing.T) {
	src := lines(
		`<?xml version="1.0" encoding="utf-8"?>`,
		`<!-- dropped -->`,
		`<catalog xmlns:dc="http://purl.org/dc/elements/1.1/" version="2">`,
		`  <book id="b1" lang="ko">`,
		`    <dc:title>한국어 책</dc:title>`,
		`    <price currency="KRW">15000</price>`,
		`    <note/>`,
		`    <summary></summary>`,
		`    <body><![CDATA[a < b]]></body>`,
		`  </book>`,
		`  <book id="b2">`,
		`    <dc:title>Second</dc:title>`,
		`  </book>`,
		`</catalog>`)
	json := convertText(t, "catalog.xml", src, "json")
	eq(t, lines(
		`{`,
		`  "catalog": {`,
		`    "@xmlns:dc": "http://purl.org/dc/elements/1.1/",`,
		`    "@version": "2",`,
		`    "book": [`,
		`      {`,
		`        "@id": "b1",`,
		`        "@lang": "ko",`,
		`        "dc:title": "한국어 책",`,
		`        "price": {`,
		`          "@currency": "KRW",`,
		`          "#text": "15000"`,
		`        },`,
		`        "note": null,`,
		`        "summary": "",`,
		`        "body": "a < b"`,
		`      },`,
		`      {`,
		`        "@id": "b2",`,
		`        "dc:title": "Second"`,
		`      }`,
		`    ]`,
		`  }`,
		`}`,
		``), json)

	x := convertText(t, "catalog.json", json, "xml")
	has(t, x, `<catalog xmlns:dc="http://purl.org/dc/elements/1.1/" version="2">`)
	has(t, x, "    <dc:title>한국어 책</dc:title>\n    <price currency=\"KRW\">15000</price>\n    <note />\n    <summary></summary>\n    <body>a &lt; b</body>\n")
	if n := strings.Count(x, "<book "); n != 2 {
		t.Fatal(x)
	}
}

func TestADefaultNamespaceSurvivesXMLToYAMLAndBack(t *testing.T) {
	src := `<feed xmlns="http://www.w3.org/2005/Atom" xml:lang="ko"><title>소식</title><entry><id>1</id></entry></feed>`
	yaml := convertText(t, "feed.xml", src, "yaml")
	eq(t, "feed:\n  '@xmlns': http://www.w3.org/2005/Atom\n  '@xml:lang': ko\n  title: 소식\n  entry:\n    id: \"1\"\n", yaml)
	x := convertText(t, "feed.yaml", yaml, "xml")
	eq(t, "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<feed xmlns=\"http://www.w3.org/2005/Atom\" xml:lang=\"ko\">\n  <title>소식</title>\n  <entry>\n    <id>1</id>\n  </entry>\n</feed>\n", x)
}

func TestXMLInEUCKRAndNamesThatAreNotXMLNames(t *testing.T) {
	enc, _ := korean.EUCKR.NewEncoder().Bytes([]byte(`<?xml version="1.0" encoding="EUC-KR"?><memo><to>홍길동</to></memo>`))
	out, err := run(t, writeFile(t, "old.xml", enc), "json")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(out)
	has(t, string(b), `"to": "홍길동"`)

	x := convertText(t, "names.json", `{"first name": "Ada", "1st": 1, "list": [[1, 2]]}`, "xml")
	has(t, x, "<first_x0020_name>Ada</first_x0020_name>")
	has(t, x, "<_x0031_st>1</_x0031_st>")
	has(t, x, "<list>\n    <item>1</item>\n    <item>2</item>\n  </list>")
	back := convertText(t, "names.xml", x, "json")
	has(t, back, `"first name": "Ada"`)
}

func TestXMLMixedContentAndErrors(t *testing.T) {
	json := convertText(t, "mixed.xml", `<p class="x">Hello <b>world</b> again</p>`, "json")
	has(t, json, `"#text": "Hello again"`)
	has(t, json, `"b": "world"`)
	x := convertText(t, "mixed.json", json, "xml")
	has(t, x, "<p class=\"x\">Hello again<b>world</b></p>")
	has(t, fail(t, "bad.xml", "<a><b></a>", "json"), "Not valid XML")
}

func TestCSVBecomesTypedRecordsAndBack(t *testing.T) {
	src := "name,zip,age,member,score\r\n홍길동,01234,42,TRUE,3.5\r\n\"Kim, Ada\",,7,false,\r\n"
	json := convertText(t, "people.csv", src, "json")
	eq(t, lines(
		`[`,
		`  {`,
		`    "name": "홍길동",`,
		`    "zip": "01234",`,
		`    "age": 42,`,
		`    "member": true,`,
		`    "score": 3.5`,
		`  },`,
		`  {`,
		`    "name": "Kim, Ada",`,
		`    "zip": "",`,
		`    "age": 7,`,
		`    "member": false,`,
		`    "score": ""`,
		`  }`,
		`]`,
		``), json)
	eq(t, "name,zip,age,member,score\r\n홍길동,01234,42,true,3.5\r\n\"Kim, Ada\",,7,false,\r\n", convertText(t, "people.json", json, "csv"))
}

func TestTablesBecomeCSVInEveryShape(t *testing.T) {
	eq(t, "id,address.city,vip\r\n1,Seoul,\r\n2,,true\r\n",
		convertText(t, "users.json", `{"users": [{"id": 1, "address": {"city": "Seoul"}}, {"id": 2, "vip": true}]}`, "csv"))
	eq(t, "name,age\r\na,1\r\nb,\r\n", convertText(t, "columns.yaml", "name: [a, b]\nage: [1]\n", "csv"))
	eq(t, "a,b\r\n1,x\r\n2,y\r\n", convertText(t, "rows.xml", "<rows><row><a>1</a><b>x</b></row><row><a>2</a><b>y</b></row></rows>", "csv"))

	e := fail(t, "nested.json", `[{"id": 1, "tags": ["a", "b"]}]`, "csv")
	has(t, e, "“tags” holds a list")
	has(t, e, "CSV holds a table")
	has(t, fail(t, "deep.json", `[{"a": {"b": {"c": 1}}}]`, "csv"), "“a.b” is nested more than one level")
	has(t, fail(t, "value.json", "42", "csv"), "single value")
}

func TestJSONLinesBothWays(t *testing.T) {
	json := convertText(t, "events.jsonl", "{\"a\":1}\n\n{\"a\":2,\"b\":\"한\"}\n", "json")
	eq(t, "[\n  {\n    \"a\": 1\n  },\n  {\n    \"a\": 2,\n    \"b\": \"한\"\n  }\n]\n", json)
	eq(t, "{\"a\":1}\n{\"a\":2,\"b\":\"한\"}\n", convertText(t, "events.json", json, "jsonl"))
	has(t, fail(t, "broken.jsonl", "{\"a\":1}\n{oops}\n", "json"), "Line 2")
}

func TestJSONCommentsTrailingCommasAndNumbers(t *testing.T) {
	json := convertText(t, "settings.json", "{\n // a comment\n \"a\": [1e3, 1.5E-10, 1e300, -0.0, 1e20,], /* more */\n}", "json")
	has(t, json, "\"a\": [\n    1000.0,\n    0.00000000015,\n    1E+300,\n    -0.0,\n    100000000000000000000.0\n  ]")
	has(t, fail(t, "bad.json", "{\"a\" 1}", "yaml"), "Not valid JSON")
}

func TestHARBecomesATableOfRequests(t *testing.T) {
	har := `{"log": {"entries": [
	  {"startedDateTime": "2026-10-16T10:00:00Z", "time": 12.5,
	   "request": {"method": "GET", "url": "https://example.com/"},
	   "response": {"status": 200, "bodySize": -1, "content": {"size": 512, "mimeType": "text/html"}}},
	  {"startedDateTime": "2026-10-16T10:00:01Z", "time": 3,
	   "request": {"method": "POST", "url": "https://example.com/api"},
	   "response": {"status": 204, "bodySize": -1, "content": {"mimeType": ""}}}
	]}}`
	eq(t, "Started,Method,URL,Status,Time (ms),Size (bytes),Type\r\n"+
		"2026-10-16T10:00:00Z,GET,https://example.com/,200,12.5,512,text/html\r\n"+
		"2026-10-16T10:00:01Z,POST,https://example.com/api,204,3,,\r\n", convertText(t, "net.har", har, "csv"))
	has(t, convertText(t, "net.har", har, "yaml"), "method: GET")
	has(t, fail(t, "notahar.har", `{"a": 1}`, "csv"), "isn't a HAR log")
}

func TestEdgesAndRoutes(t *testing.T) {
	c := New()
	if n := len(c.Edges()); n != 20+5+5+2+6+2 {
		t.Fatalf("%d edges", n)
	}
	p := convert.NewPlanner([]convert.Converter{c}, nil)
	r := p.Plan("vcf", "json", convert.Options{})
	if r == nil || len(r.Steps) != 2 {
		t.Fatalf("vcf → json: %+v", r)
	}
	if r := p.Plan("csv", "json", convert.Options{}); r == nil || len(r.Steps) != 1 {
		t.Fatal("csv → json")
	}
}

func TestFormatDouble(t *testing.T) {
	for v, want := range map[float64]string{3: "3.0", 0.1: "0.1", 1e15: "1E+15", 1e14: "100000000000000.0", 1e-5: "1E-05", 0.0001: "0.0001", -1500: "-1500.0", 1.2345678901234568e20: "1.2345678901234568E+20"} {
		if got := formatDouble(v); got != want {
			t.Errorf("%v: %s, want %s", v, got, want)
		}
	}
}
