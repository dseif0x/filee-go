package media

// Hardware video encoders (NVENC, Quick Sync, AMF, VideoToolbox, VA-API) for
// H.264, HEVC and AV1: which ones to try on which system and their options.
// Whether one works on this computer is found out by a test encode
// (SelectHardware), cached per FFmpeg, encoder and bit depth.

import (
	"context"
	"log/slog"
	"math"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/dseif0x/filee-go/internal/presets"
)

// HardwareAPI is the vendor interface an encoder goes through.
type HardwareAPI int

const (
	Nvenc HardwareAPI = iota
	Qsv
	Amf
	VideoToolbox
	Vaapi
)

// RateControl is how the preset's quality reaches a hardware encoder.
type RateControl int

const (
	// RateQuality is a constant quality or quantizer, like x264's CRF.
	RateQuality RateControl = iota
	// RateBitrate is an average bitrate: VideoToolbox on Intel Macs has no constant quality mode.
	RateBitrate
)

// HardwareEncoder is one hardware encoder of FFmpeg (e.g. hevc_nvenc).
type HardwareEncoder struct {
	Name string
	API  HardwareAPI
	Rate RateControl
}

// Codec is "h264", "hevc" or "av1": the part before the underscore.
func (e *HardwareEncoder) Codec() string { c, _, _ := strings.Cut(e.Name, "_"); return c }

// VaapiDevice is the first DRM render node: the integrated or only GPU on almost every Linux system.
const VaapiDevice = "/dev/dri/renderD128"

// HardwareCandidates are the encoders for codec worth trying on goos, best
// first: Windows NVIDIA, Intel, AMD; macOS VideoToolbox with constant quality
// (Apple Silicon), then with a bitrate (Intel Macs), none for AV1; Linux
// NVIDIA, then VA-API when a render node exists. Only encoders FFmpeg lists
// are tried (all when the list is unknown).
func HardwareCandidates(codec, goos string, encoders map[string]bool, vaapi bool) []HardwareEncoder {
	if codec != "h264" && codec != "hevc" && codec != "av1" {
		return nil
	}
	var c []HardwareEncoder
	switch goos {
	case "windows":
		c = []HardwareEncoder{{codec + "_nvenc", Nvenc, RateQuality}, {codec + "_qsv", Qsv, RateQuality}, {codec + "_amf", Amf, RateQuality}}
	case "darwin":
		if codec != "av1" {
			c = []HardwareEncoder{{codec + "_videotoolbox", VideoToolbox, RateQuality}, {codec + "_videotoolbox", VideoToolbox, RateBitrate}}
		}
	case "linux":
		c = []HardwareEncoder{{codec + "_nvenc", Nvenc, RateQuality}}
		if vaapi {
			c = append(c, HardwareEncoder{codec + "_vaapi", Vaapi, RateQuality})
		}
	}
	if encoders == nil {
		return c
	}
	var out []HardwareEncoder
	for _, e := range c {
		if encoders[e.Name] {
			out = append(out, e)
		}
	}
	return out
}

// TenBit: HEVC and AV1 keep 10-bit and HDR sources in 10 bits; H.264 is always 8-bit 4:2:0.
func TenBit(codec string, v *VideoStream) bool {
	return (codec == "hevc" || codec == "av1") && (v.IsHighBitDepth() || v.IsHdr())
}

// HardwareQuantizer is the quality on the encoders' 0–51 scale: one step
// coarser than x264/x265's CRF, which keeps the size close.
func HardwareQuantizer(codec string, q presets.MediaQuality) int {
	if codec == "hevc" || codec == "av1" {
		return choose(q, 23, 27, 31)
	}
	return choose(q, 19, 24, 29)
}

// Av1Quantizer is the quality on AV1's 0–63 scale (NVENC -cq).
func Av1Quantizer(q presets.MediaQuality) int { return choose(q, 28, 34, 42) }

// Av1QIndex is Av1Quantizer on AV1's 0–255 quantizer index (AMF, VA-API).
func Av1QIndex(q presets.MediaQuality) int { return Av1Quantizer(q) * 4 }

// VideoToolboxQuality is the constant quality (-q:v, 1–100) on Apple Silicon.
func VideoToolboxQuality(q presets.MediaQuality) int { return choose(q, 70, 60, 50) }

// HardwareBitrateKbps is VideoToolbox's average bitrate on Intel Macs: bits
// per pixel per frame of the output picture.
func HardwareBitrateKbps(codec string, q presets.MediaQuality, v *VideoStream, maxHeight int) int {
	height := 1080.0
	if v.DisplayHeight() > 0 {
		height = float64(v.DisplayHeight())
	}
	if maxHeight > 0 && height > float64(max(16, maxHeight)) {
		height = float64(max(16, maxHeight))
	}
	aspect := v.DisplayAspect()
	if !(aspect > 0) || math.IsInf(aspect, 0) {
		aspect = 16 / 9.0
	}
	width := height * aspect
	fps := v.FrameRate
	if !(fps > 0 && fps <= 240) {
		fps = 30
	}
	bpp := choose(q, 0.14, 0.09, 0.05)
	if codec == "hevc" {
		bpp *= 0.65
	}
	return int(min(max(math.RoundToEven(width*height*fps*bpp/1000), 300), 100_000))
}

// HardwareInputArgs are global options before the input: VA-API needs its device.
func HardwareInputArgs(e *HardwareEncoder) []string {
	if e.API == Vaapi {
		return []string{"-vaapi_device", VaapiDevice}
	}
	return nil
}

// HardwareFilterSuffix uploads VA-API frames to the GPU as NV12 / P010.
func HardwareFilterSuffix(e *HardwareEncoder, tenBit bool) string {
	if e.API != Vaapi {
		return ""
	}
	if tenBit {
		return ",format=p010le,hwupload"
	}
	return ",format=nv12,hwupload"
}

// HardwareCodecArgs are the encoder options for one video.
func HardwareCodecArgs(e *HardwareEncoder, target string, q presets.MediaQuality, v *VideoStream, maxHeight int) []string {
	codec := e.Codec()
	av1 := codec == "av1"
	ten := TenBit(codec, v)
	quantizer := itoa(HardwareQuantizer(codec, q))
	a := []string{"-c:v", e.Name}
	switch e.API {
	case Nvenc:
		cq := quantizer
		if av1 {
			cq = itoa(Av1Quantizer(q))
		}
		a = append(a, "-preset", "p5", "-rc", "vbr", "-cq", cq, "-b:v", "0")
	case Qsv:
		a = append(a, "-preset", "medium", "-global_quality", quantizer)
	case Amf:
		if av1 {
			i := Av1QIndex(q)
			a = append(a, "-quality", "balanced", "-rc", "cqp", "-qp_i", itoa(i-8), "-qp_p", itoa(i), "-qp_b", itoa(i))
		} else {
			a = append(a, "-quality", "balanced", "-rc", "cqp", "-qp_i", itoa(max(0, HardwareQuantizer(codec, q)-2)), "-qp_p", quantizer)
			if codec == "h264" {
				a = append(a, "-qp_b", quantizer)
			}
		}
	case VideoToolbox:
		if e.Rate == RateBitrate {
			a = append(a, "-b:v", itoa(HardwareBitrateKbps(codec, q, v, maxHeight))+"k")
		} else {
			a = append(a, "-q:v", itoa(VideoToolboxQuality(q)))
		}
	case Vaapi:
		if av1 {
			a = append(a, "-rc_mode", "CQP", "-global_quality", itoa(Av1QIndex(q)))
		} else {
			a = append(a, "-rc_mode", "CQP", "-qp", quantizer)
		}
	}
	switch codec {
	case "h264":
		a = append(a, "-profile:v", "high")
	case "hevc":
		if ten {
			a = append(a, "-profile:v", "main10")
		} else {
			a = append(a, "-profile:v", "main")
		}
	}
	if e.API != Vaapi {
		if ten {
			a = append(a, "-pix_fmt", "p010le")
		} else {
			a = append(a, "-pix_fmt", "nv12")
		}
	}
	if ten {
		a = append(a, ColorArgs(v)...)
	}
	if appleHevc(codec, target) {
		a = append(a, "-tag:v", "hvc1")
	}
	return a
}

// HardwareProbeArgs is a tiny test encode with exactly the options a real one
// uses, so an encoder FFmpeg lists but the computer can't run fails here.
func HardwareProbeArgs(e *HardwareEncoder, tenBit bool) []string {
	pf := "yuv420p"
	if tenBit {
		pf = "yuv420p10le"
	}
	pic := &VideoStream{Width: 256, Height: 144, SampleAspect: 1, FrameRate: 30, PixelFormat: pf}
	a := append(common(), HardwareInputArgs(e)...)
	a = append(a, "-f", "lavfi", "-i", "testsrc=size=256x144:rate=30:duration=0.1", "-vf", "null"+HardwareFilterSuffix(e, tenBit))
	a = append(a, HardwareCodecArgs(e, "mkv", presets.MediaBalanced, pic, 0)...)
	return append(a, "-f", "null", "-")
}

// hardwareProbeTimeout: a test encode takes 0.1–2 s; a driver that hangs is given up on.
const hardwareProbeTimeout = 15 * time.Second

type probeEntry struct {
	once sync.Once
	ok   bool
}

var (
	probeMu sync.Mutex
	probes  = map[string]*probeEntry{}
)

// SelectHardware returns the first hardware encoder for codec whose test
// encode works here, or nil (then x264 / x265 / SVT-AV1 encode).
func SelectHardware(ctx context.Context, ffmpeg, codec string, tenBit bool, encoders map[string]bool, log *slog.Logger) *HardwareEncoder {
	_, err := os.Stat(VaapiDevice)
	vaapi := runtime.GOOS == "linux" && err == nil
	for _, c := range HardwareCandidates(codec, runtime.GOOS, encoders, vaapi) {
		if ctx.Err() != nil {
			return nil
		}
		if hardwareWorks(ffmpeg, c, tenBit, log) {
			e := c
			return &e
		}
	}
	return nil
}

func hardwareWorks(ffmpeg string, e HardwareEncoder, tenBit bool, log *slog.Logger) bool {
	bits := 8
	if tenBit {
		bits = 10
	}
	key := cacheKey(ffmpeg) + "|" + e.Name + "|" + itoa(int(e.Rate)) + "|" + itoa(bits)
	probeMu.Lock()
	p := probes[key]
	if p == nil {
		p = &probeEntry{}
		probes[key] = p
	}
	probeMu.Unlock()
	p.once.Do(func() {
		// Not cancelled with the conversion: others may wait for its result.
		ctx, cancel := context.WithTimeout(context.Background(), hardwareProbeTimeout)
		defer cancel()
		r, err := RunFfmpeg(ctx, ffmpeg, HardwareProbeArgs(&e, tenBit), nil, hardwareProbeTimeout)
		switch {
		case err != nil:
			log.Info("hardware encoder is not usable", "encoder", e.Name, "rate", e.Rate, "bits", bits, "reason", err)
		case r.ExitCode != 0:
			log.Info("hardware encoder is not usable", "encoder", e.Name, "rate", e.Rate, "bits", bits, "reason", Summarize(r.Stderr))
		default:
			log.Info("hardware encoder works", "encoder", e.Name, "rate", e.Rate, "bits", bits)
			p.ok = true
		}
	})
	return p.ok
}
