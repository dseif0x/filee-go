package images

import (
	"testing"
)

// The samples come from the test data of github.com/gen2brain/{heic,avif,jpegxl} (MIT): a 480 × 640 photo with
// Exif, and an AVIF whose irot box turns it.
func TestHEIFSamples(t *testing.T) {
	for _, n := range []string{"exif.heic", "exif.avif", "rot.avif", "exif.jxl"} {
		p, err := Decode("testdata/"+n, "", nil)
		if err != nil {
			t.Errorf("%s: %v", n, err)
			continue
		}
		f := p.Frames[0]
		if f.Width() != 480 || f.Height() != 640 {
			t.Errorf("%s: %dx%d, want 480x640", n, f.Width(), f.Height())
		}
		if n != "rot.avif" && f.Meta.Exif == nil {
			t.Errorf("%s: the Exif data was not read", n)
		}
		if o := exifOrientation(f.Meta.Exif); o > 1 {
			t.Errorf("%s: Exif orientation %d left on a picture the decoder turned", n, o)
		}
	}
	out := run(t, step(t, "testdata/exif.heic", "heic", "jpg", nil))
	if decodeFile(t, out[0]).Frames[0].Meta.Exif == nil {
		t.Error("HEIC → JPG lost the Exif data")
	}
}
