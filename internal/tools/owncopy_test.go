package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The user's own copy (Settings › Engines) wins over Filee's engines folder and the PATH.
func TestOwnCopiesComeFirst(t *testing.T) {
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	own := filepath.Join(t.TempDir(), "bin")
	engines := t.TempDir()
	t.Setenv("FILEE_ENGINES_DIR", engines)
	for _, dir := range []string{own, filepath.Join(engines, "ffmpeg")} {
		os.MkdirAll(dir, 0o755)
		for _, n := range []string{"ffmpeg", "ffprobe"} {
			os.WriteFile(filepath.Join(dir, n+exe), []byte("#!/bin/sh\n"), 0o755)
		}
	}
	spec := Spec{Names: []string{"ffmpeg"}, Package: "ffmpeg", Requires: []string{"ffprobe"}, Dirs: map[string][]string{runtime.GOOS: {"ffmpeg"}}}
	t.Cleanup(func() { SetOwnCopies(nil) })
	SetOwnCopies(nil)
	if got := Find(spec); filepath.Dir(got) != filepath.Join(engines, "ffmpeg") {
		t.Fatalf("Filee's copy: %q", got)
	}
	// The folder above the program's bin folder, as a user may pick it.
	if !SetOwnCopies(map[string]string{"ffmpeg": filepath.Dir(own)}) {
		t.Fatal("a change is reported")
	}
	if got := Find(spec); filepath.Dir(got) != own {
		t.Fatalf("own copy: %q", got)
	}
	if SetOwnCopies(map[string]string{"ffmpeg": filepath.Dir(own)}) {
		t.Error("no change")
	}
	// A copy without ffprobe is not used.
	os.Remove(filepath.Join(own, "ffprobe"+exe))
	Forget()
	if got := Find(spec); filepath.Dir(got) != filepath.Join(engines, "ffmpeg") {
		t.Fatalf("incomplete own copy: %q", got)
	}
}
