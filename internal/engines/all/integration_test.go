package all

import (
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
)

// Seams between the engines: routes that need one engine's reader in another's writer.
func TestCrossEngineRoutes(t *testing.T) {
	p := convert.NewPlanner(Converters(), nil)
	for _, c := range []struct {
		from, to string
		engines  []string
		cost     int
	}{
		// PDF through the document model (C# DocumentReaders["pdf"], cost + PdfPenalty).
		{"pdf", "docx", []string{"docx-writer"}, 14},
		{"pdf", "hwpx", []string{"hwpx-writer"}, 16},
		{"pdf", "md", []string{"pdf-text"}, 16},
		{"pdf", "txt", []string{"pdf-text"}, 10},
	} {
		r := p.Plan(c.from, c.to, convert.Options{})
		if r == nil || len(r.Steps) != len(c.engines) {
			t.Errorf("%s → %s: %+v", c.from, c.to, r)
			continue
		}
		for i, s := range r.Steps {
			if s.Converter.ID() != c.engines[i] {
				t.Errorf("%s → %s: step %d is %s, want %s", c.from, c.to, i, s.Converter.ID(), c.engines[i])
			}
		}
		if c.cost != 0 && r.Cost != c.cost {
			t.Errorf("%s → %s costs %d, want %d", c.from, c.to, r.Cost, c.cost)
		}
	}
}
