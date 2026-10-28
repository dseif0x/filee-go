package formats

import "testing"

func TestLookup(t *testing.T) {
	if len(Known) < 260 {
		t.Fatalf("only %d formats", len(Known))
	}
	for _, tc := range []struct{ path, ext, name, id string }{
		{"/a/b/Photo.JPEG", "JPEG", "Photo", "jpg"},
		{"backup.tar.gz", "tar.gz", "backup", "tgz"},
		{"book.kepub.epub", "kepub.epub", "book", "kepub"},
		{"noext", "", "noext", ""},
		{".gz", "gz", "", ""},
	} {
		if got := ExtensionOf(tc.path); got != tc.ext {
			t.Errorf("ExtensionOf(%q) = %q, want %q", tc.path, got, tc.ext)
		}
		if got := NameWithoutExtension(tc.path); got != tc.name {
			t.Errorf("NameWithoutExtension(%q) = %q, want %q", tc.path, got, tc.name)
		}
		if tc.id != "" {
			if f := Detect(tc.path); f == nil || f.ID != tc.id {
				t.Errorf("Detect(%q) = %+v, want %s", tc.path, f, tc.id)
			}
		}
	}
	if ByID("PNG") == nil || ByID(Folder).Category != Archive {
		t.Fatal("ByID")
	}
}
