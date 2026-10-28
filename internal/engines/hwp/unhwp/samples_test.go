package unhwp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/hwp/hwpx"
)

// TestSamples reads every .hwp in FILEE_HWP_SAMPLES (real documents, not shipped with the repository).
func TestSamples(t *testing.T) {
	dir := os.Getenv("FILEE_HWP_SAMPLES")
	if dir == "" {
		t.Skip("FILEE_HWP_SAMPLES not set")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.hwp"))
	for _, f := range files {
		d, err := ReadHWP(f, t.TempDir())
		if err != nil {
			t.Logf("%s: %v", filepath.Base(f), err)
			continue
		}
		text := hwpx.PlainTextWrite(d)
		if len(text) > 300 {
			text = text[:300]
		}
		t.Logf("%s: %d sections, page %+v\n%s", filepath.Base(f), len(d.Sections), d.Sections[0].Page, strings.TrimSpace(text))
	}
}
