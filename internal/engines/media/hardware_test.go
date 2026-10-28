package media

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/presets"
)

func TestHardwareCandidates(t *testing.T) {
	eq(t, "h264_nvenc", HardwareCandidates("h264", "linux", nil, false)[0].Name)
	l := HardwareCandidates("h264", "linux", nil, true)
	if len(l) != 2 || l[1].API != Vaapi {
		t.Error(l)
	}
	w := HardwareCandidates("h264", "windows", setOf("h264_qsv", "h264_amf"), false)
	if len(w) != 2 || w[0].Name != "h264_qsv" {
		t.Error(w)
	}
	m := HardwareCandidates("hevc", "darwin", nil, false)
	if len(m) != 2 || m[0].Rate != RateQuality || m[1].Rate != RateBitrate {
		t.Error(m)
	}
	if HardwareCandidates("av1", "darwin", nil, false) != nil || HardwareCandidates("vp9", "windows", nil, false) != nil {
		t.Error("no AV1 on VideoToolbox, no VP9")
	}
	eq(t, "hevc", (&HardwareEncoder{Name: "hevc_videotoolbox"}).Codec())
}

func TestHardwareCodecChoice(t *testing.T) {
	on := opts(func(o *presets.MediaOptions) { o.HardwareEncoding = true })
	clip := infoOf(10, &VideoStream{Width: 1920, Height: 1080, SampleAspect: 1, FrameRate: 30, Codec: "mpeg2video", PixelFormat: "yuv420p"})
	eq(t, "h264", HardwareCodec("mp4", on, clip, false))
	eq(t, "h264", HardwareCodec("ts", on, clip, false))
	eq(t, "hevc", HardwareCodec("mkv", opts(func(o *presets.MediaOptions) { o.HardwareEncoding = true; o.VideoCodec = presets.VideoHevc }), clip, false))
	av1 := opts(func(o *presets.MediaOptions) { o.HardwareEncoding = true; o.VideoCodec = presets.VideoAv1 })
	eq(t, "av1", HardwareCodec("webm", av1, clip, false))
	eq(t, "h264", HardwareCodec("mov", av1, clip, false))
	eq(t, "", HardwareCodec("webm", on, clip, false)) // VP9
	eq(t, "", HardwareCodec("mp4", opts(nil), clip, false))
	playable := infoOf(10, &VideoStream{Width: 1920, Height: 1080, SampleAspect: 1, FrameRate: 30, Codec: "h264", PixelFormat: "yuv420p"})
	eq(t, "", HardwareCodec("mp4", on, playable, false)) // copied anyway
	eq(t, "h264", HardwareCodec("mp4", on, playable, true))
}

func TestHardwareCodecArgs(t *testing.T) {
	c := func(e HardwareEncoder, target string, q presets.MediaQuality, max int) string {
		return strings.Join(HardwareCodecArgs(&e, target, q, hd, max), " ")
	}
	eq(t, "-c:v h264_nvenc -preset p5 -rc vbr -cq 24 -b:v 0 -profile:v high -pix_fmt nv12", c(HardwareEncoder{"h264_nvenc", Nvenc, RateQuality}, "mp4", presets.MediaBalanced, 0))
	eq(t, "-c:v hevc_qsv -preset medium -global_quality 23 -profile:v main -pix_fmt nv12 -tag:v hvc1", c(HardwareEncoder{"hevc_qsv", Qsv, RateQuality}, "mp4", presets.MediaHigh, 0))
	eq(t, "-c:v h264_amf -quality balanced -rc cqp -qp_i 27 -qp_p 29 -qp_b 29 -profile:v high -pix_fmt nv12", c(HardwareEncoder{"h264_amf", Amf, RateQuality}, "mkv", presets.MediaSmall, 0))
	eq(t, "-c:v hevc_amf -quality balanced -rc cqp -qp_i 25 -qp_p 27 -profile:v main -pix_fmt nv12", c(HardwareEncoder{"hevc_amf", Amf, RateQuality}, "mkv", presets.MediaBalanced, 0))
	eq(t, "-c:v h264_videotoolbox -q:v 60 -profile:v high -pix_fmt nv12", c(HardwareEncoder{"h264_videotoolbox", VideoToolbox, RateQuality}, "mov", presets.MediaBalanced, 0))
	eq(t, "-c:v hevc_vaapi -rc_mode CQP -qp 31 -profile:v main", c(HardwareEncoder{"hevc_vaapi", Vaapi, RateQuality}, "mkv", presets.MediaSmall, 0))
	eq(t, "-c:v h264_videotoolbox -b:v 5593k -profile:v high -pix_fmt nv12", c(HardwareEncoder{"h264_videotoolbox", VideoToolbox, RateBitrate}, "mp4", presets.MediaBalanced, 0))
	eq(t, "2486k", after(HardwareCodecArgs(&HardwareEncoder{"h264_videotoolbox", VideoToolbox, RateBitrate}, "mp4", presets.MediaBalanced, hd, 720), "-b:v"))
	eq(t, 300, HardwareBitrateKbps("hevc", presets.MediaSmall, &VideoStream{Width: 64, Height: 64, SampleAspect: 1, FrameRate: 5}, 0))
	eq(t, "-c:v av1_nvenc -preset p5 -rc vbr -cq 34 -b:v 0 -pix_fmt nv12", c(HardwareEncoder{"av1_nvenc", Nvenc, RateQuality}, "mp4", presets.MediaBalanced, 0))
	probe := HardwareProbeArgs(&HardwareEncoder{"hevc_vaapi", Vaapi, RateQuality}, true)
	eq(t, VaapiDevice, after(probe, "-vaapi_device"))
	eq(t, "null,format=p010le,hwupload", after(probe, "-vf"))
}

func TestHardwareFailureIsRetriedInSoftware(t *testing.T) {
	c := New()
	hw := &HardwareEncoder{"h264_nvenc", Nvenc, RateQuality}
	var runs [][]Pass
	plan := func(e *HardwareEncoder) ([]Pass, error) {
		name := "libx264"
		if e != nil {
			name = e.Name
		}
		return []Pass{{[]string{name}, 1}}, nil
	}
	first, _ := plan(hw)
	err := c.runWithFallback(context.Background(), first, hw, plan, func(p []Pass) error {
		runs = append(runs, p)
		if p[0].Args[0] == "h264_nvenc" {
			return errors.New("driver error")
		}
		return nil
	}, "out.mp4")
	if err != nil || len(runs) != 2 || runs[1][0].Args[0] != "libx264" {
		t.Fatal(err, runs)
	}
	// Cancellation is never retried.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runs = nil
	err = c.runWithFallback(ctx, first, hw, plan, func(p []Pass) error { runs = append(runs, p); return context.Canceled }, "out.mp4")
	if !errors.Is(err, context.Canceled) || len(runs) != 1 {
		t.Error(err, len(runs))
	}
}
