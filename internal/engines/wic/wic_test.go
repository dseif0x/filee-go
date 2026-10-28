package wic

import (
	"runtime"
	"testing"
)

func TestToneMap(t *testing.T) {
	// SDR white at 1.0 stays white; a highlight at 4× white is compressed, not clipped flat
	tm := newToneMap(referenceWhite([]float32{0.2, 0.5, 1, 1}), 4)
	px := make([]byte, 4)
	tm.mapPixel(0.5, 0.5, 0.5, 1, px)
	if px[0] < 185 || px[0] > 190 {
		t.Errorf("mid grey %d, want about 188 (sRGB of 0.5)", px[0])
	}
	tm.mapPixel(4, 2, 0, 1, px)
	if px[0] != 255 || px[1] >= 255 || px[2] != 0 {
		t.Errorf("highlight %v: hue should be kept", px)
	}
	if w := referenceWhite([]float32{10, 10, 10}); w != 4 {
		t.Errorf("white %v, want the 4.0 cap", w)
	}
}

func TestStatus(t *testing.T) {
	s := New().Status()
	if runtime.GOOS != "windows" && (s.Available || s.ReasonKey != "engine.reason.unsupported_platform") {
		t.Errorf("status %+v", s)
	}
	if len(New().Edges()) == 0 {
		t.Error("no edges")
	}
}
