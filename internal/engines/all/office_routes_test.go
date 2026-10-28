package all

import (
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
)

func TestOfficeRoutes(t *testing.T) {
	var office []convert.Converter
	for _, c := range Converters() {
		switch c.ID() {
		case "markdown", "spreadsheet", "ooxml", "docx-writer":
			office = append(office, c)
		}
	}
	p := convert.NewPlanner(office, nil)
	for _, c := range []struct{ from, to, engine string }{
		{"md", "html", "markdown"}, {"md", "docx", "docx-writer"}, {"csv", "docx", "docx-writer"}, {"ofd", "docx", "docx-writer"},
		{"docm", "docx", "ooxml"}, {"xlsm", "xlsx", "ooxml"}, {"xls", "xlsx", "spreadsheet"}, {"txt", "docx", "docx-writer"},
	} {
		r := p.Plan(c.from, c.to, convert.Options{})
		if r == nil || len(r.Steps) != 1 || r.Steps[0].Converter.ID() != c.engine {
			t.Errorf("%s → %s: %+v", c.from, c.to, r)
		}
	}
}
