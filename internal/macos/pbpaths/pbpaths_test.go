package pbpaths

import (
	"slices"
	"testing"
)

func TestParsing(t *testing.T) {
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><array><string>/Users/a/x.png</string><string>relative</string><string>/Users/a/b &amp; c.pdf</string></array></plist>`
	if got := FromPropertyList([]byte(plist)); !slices.Equal(got, []string{"/Users/a/x.png", "/Users/a/b & c.pdf"}) {
		t.Errorf("plist: %q", got)
	}
	if got := FromPropertyList([]byte("<array><string>/a</string></array>")); !slices.Equal(got, []string{"/a"}) {
		t.Errorf("bare array: %q", got)
	}
	for in, want := range map[string]string{
		"file:///Users/a/b%20c.png":        "/Users/a/b c.png",
		"file://localhost/tmp/x":           "/tmp/x",
		"file://server/share/x":            "",
		"https://example.com/x.png":        "",
		"/plain/path":                      "",
		"file:///.file/id=6571367.8679890": "",
	} {
		if got := FromFileURL(in); got != want {
			t.Errorf("FromFileURL(%q) = %q, want %q", in, got, want)
		}
	}
	if got := FromText("/a/b\r\nnot a path\nfile:///c%20d\n"); !slices.Equal(got, []string{"/a/b", "/c d"}) {
		t.Errorf("text: %q", got)
	}
	exists := func(p string) bool { return p != "/gone" }
	got := FirstExisting([]func() []string{
		func() []string { return []string{"/gone"} },
		func() []string { return []string{"/x", "/x", "/gone", "/y"} },
		func() []string { t.Error("read a later source"); return nil },
	}, exists)
	if !slices.Equal(got, []string{"/x", "/y"}) {
		t.Errorf("first existing: %q", got)
	}
	if !IsFilenamesType(FilenamesUTI) || IsFilenamesType(FileURLType) {
		t.Error("filenames type")
	}
}
