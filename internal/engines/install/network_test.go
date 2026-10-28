package install

import (
	"context"
	"os"
	"testing"
)

// TestInstallFfmpegForReal downloads Filee's FFmpeg for this platform into
// FILEE_ENGINES_DIR; only with FILEE_NETWORK_TESTS=1 (150 MB).
func TestInstallFfmpegForReal(t *testing.T) {
	if os.Getenv("FILEE_NETWORK_TESTS") != "1" || os.Getenv("FILEE_ENGINES_DIR") == "" {
		t.Skip("set FILEE_NETWORK_TESTS=1 and FILEE_ENGINES_DIR to download FFmpeg")
	}
	in := &Installer{}
	p := *PackageByID("ffmpeg")
	if !CanDownload(p, in.components()) {
		t.Skip(UnavailableReason(p, in.components(), "linux"))
	}
	if err := in.Install(context.Background(), p, nil); err != nil {
		t.Fatal(err)
	}
	if !in.IsUpToDate(p) {
		t.Fatal("not installed")
	}
}
