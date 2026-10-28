package xdom

import (
	"bytes"
	"strings"
	"testing"
)

func TestRoundTripKeepsPrefixes(t *testing.T) {
	src := `<?xml version="1.0"?><workbook xmlns="urn:s" xmlns:r="urn:r" xmlns:mc="urn:mc" mc:Ignorable="x15"><sheets><sheet name="A &amp; B" r:id="rId1"/></sheets><t xml:space="preserve"> x </t></workbook>`
	n, err := ParseBytes([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if n.Child("urn:s", "sheets").Child("urn:s", "sheet").AN("urn:r", "id") != "rId1" {
		t.Fatal("attribute lookup")
	}
	var b bytes.Buffer
	if err := Write(&b, n, nil); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{`<workbook xmlns="urn:s" xmlns:r="urn:r"`, `mc:Ignorable="x15"`, `r:id="rId1"`, `name="A &amp; B"`, `xml:space="preserve"`, `> x </t>`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
}

func TestRefusesDoctype(t *testing.T) {
	if _, err := ParseBytes([]byte(`<!DOCTYPE x [<!ENTITY a "b">]><x/>`)); err == nil {
		t.Fatal("doctype accepted")
	}
}
