package images

import (
	"fmt"
	"image"
	"testing"

	"github.com/dseif0x/filee-go/internal/presets"
)

func TestChangeSpeedRounding(t *testing.T) {
	var frames []*Frame
	// 0 and 1 hundredths are shown for 10/100 s by browsers: that is what gets sped up.
	for _, d := range []int{0, 1, 3, 3, 3} {
		frames = append(frames, &Frame{Img: image.NewNRGBA(image.Rect(0, 0, 4, 4)), Delay: d, Ticks: 100})
	}
	fourth := frames[3]
	fast := ChangeSpeed(frames, 2, "gif")
	// 5, 5, 1.5, 1.5, 1.5 → starts 0, 5, 10, 11.5 (12), 13, end 14.5 (15): the last frame is dropped
	var got []int
	for _, f := range fast {
		got = append(got, f.Delay)
		if f.Ticks != 100 {
			t.Errorf("ticks %d", f.Ticks)
		}
	}
	if fmt.Sprint(got) != "[5 5 2 3]" {
		t.Errorf("delays %v, want [5 5 2 3]", got)
	}
	if fast[3] != fourth {
		t.Error("the fourth frame should be the source's fourth")
	}
	if len(ChangeSpeed(frames[:1], 3, "webp")) != 1 {
		t.Error("one frame stays one frame")
	}
}

func TestSpeedInMilliseconds(t *testing.T) {
	var frames []*Frame
	for _, d := range []int{10, 20, 30, 40} {
		frames = append(frames, &Frame{Img: image.NewNRGBA(image.Rect(0, 0, 2, 2)), Delay: d, Ticks: 100})
	}
	var got []int
	for _, f := range ChangeSpeed(frames, 1.5, "webp") {
		got = append(got, f.Delay)
	}
	if fmt.Sprint(got) != "[67 133 200 267]" {
		t.Errorf("delays %v, want [67 133 200 267]", got)
	}
}

func TestJoinLayout(t *testing.T) {
	sizes := [][2]int{{400, 300}, {200, 200}, {100, 50}}
	o := presetsDefault()
	o.JoinGap = 5
	l, err := planJoin(sizes, &o)
	if err != nil {
		t.Fatal(err)
	}
	if l.W != 400 || l.H != 910 || fmt.Sprint(l.Pictures) != "[{0 0 400 300} {0 305 400 400} {0 710 400 200}]" {
		t.Errorf("same size: %dx%d %v", l.W, l.H, l.Pictures)
	}
	o = presetsDefault()
	o.JoinFit = "Original"
	l, _ = planJoin(sizes, &o)
	if l.W != 400 || l.H != 550 || fmt.Sprint(l.Pictures) != "[{0 0 400 300} {100 300 200 200} {150 500 100 50}]" {
		t.Errorf("kept sizes: %dx%d %v", l.W, l.H, l.Pictures)
	}
	o.JoinDirection, o.JoinGap = "Horizontal", 2
	l, _ = planJoin(sizes, &o)
	if l.W != 704 || l.H != 300 || fmt.Sprint(l.Pictures) != "[{0 0 400 300} {402 50 200 200} {604 125 100 50}]" {
		t.Errorf("row: %dx%d %v", l.W, l.H, l.Pictures)
	}
	var many [][2]int
	for i := 0; i < 12; i++ {
		many = append(many, [2]int{6000, 4000})
	}
	o = presetsDefault()
	o.JoinDirection = "Horizontal"
	if _, err := planJoin(many, &o); err == nil || err.Error() != "error.merged_image_too_large (72000 × 4000 px)" {
		t.Errorf("12 photos: %v", err)
	}
	if l, err := planJoin(many[:8], &o); err != nil || l.W != 48000 {
		t.Errorf("8 photos: %v", err)
	}
}

func presetsDefault() presets.ImageOptions { return presets.DefaultImageOptions() }
