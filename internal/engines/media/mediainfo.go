package media

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/dseif0x/filee-go/internal/tools"
)

// VideoColor is the colour description of a video stream in FFmpeg's names;
// "" parts are unknown.
type VideoColor struct {
	Primaries, Transfer, Matrix, Range string
}

// VideoStream is the first real video stream of a file (cover pictures are
// not counted).
type VideoStream struct {
	// Width and Height are the coded size in pixels.
	Width, Height int
	// SampleAspect is the pixel aspect ratio (1 for square pixels).
	SampleAspect float64
	// Rotation in degrees from the display matrix (phones record portrait video this way).
	Rotation int
	// FrameRate is the average frames per second, 0 when unknown.
	FrameRate float64
	// Codec is FFmpeg's codec name (h264, hevc, …), "" when unknown.
	Codec string
	// PixelFormat is FFmpeg's pixel format (yuv420p, …), "" when unknown.
	PixelFormat string
	Color       *VideoColor
	// HeaderHash is a hash of the codec's setup data (parameter sets), equal
	// only for streams a decoder reads with the same setup.
	HeaderHash string
}

var (
	highBitDepthParts    = []string{"p10", "p12", "p14", "p16"}
	highBitDepthPrefixes = []string{"p010", "p012", "p016", "p210", "p216", "p410", "p416", "y210", "y212", "v210"}
)

// IsHighBitDepth is true for more than 8 bits per sample (phones' HDR video,
// ProRes, 10-bit HEVC and AV1).
func (v *VideoStream) IsHighBitDepth() bool {
	f := v.PixelFormat
	if f == "" {
		return false
	}
	for _, s := range highBitDepthParts {
		if strings.Contains(f, s) {
			return true
		}
	}
	for _, p := range highBitDepthPrefixes {
		if strings.HasPrefix(f, p) {
			return true
		}
	}
	return false
}

// IsHdr is true for PQ (HDR10, Dolby Vision) or HLG transfer.
func (v *VideoStream) IsHdr() bool {
	return v.Color != nil && (v.Color.Transfer == "smpte2084" || v.Color.Transfer == "arib-std-b67")
}

// IsRotated is true when the picture is shown turned by 90°.
func (v *VideoStream) IsRotated() bool { r := v.Rotation % 180; return r == 90 || r == -90 }

// DisplayHeight is the height as displayed (the width for portrait phone videos).
func (v *VideoStream) DisplayHeight() int {
	if v.IsRotated() {
		return v.Width
	}
	return v.Height
}

// DisplayAspect is width ÷ height as displayed: pixel aspect and rotation applied.
func (v *VideoStream) DisplayAspect() float64 {
	if v.Width <= 0 || v.Height <= 0 {
		return 16 / 9.0
	}
	a := float64(v.Width) * v.SampleAspect / float64(v.Height)
	if v.IsRotated() {
		return 1 / a
	}
	return a
}

// AudioStream is one audio stream.
type AudioStream struct {
	Channels, SampleRate int
	Codec                string
}

// SubtitleStream is one subtitle stream.
type SubtitleStream struct {
	// Index is the stream's index in the file (for -map 0:index).
	Index    int
	Codec    string
	Language string
}

// MediaInfo summarizes a media file.
type MediaInfo struct {
	// Duration is the playing time, 0 when the container does not say (raw streams).
	Duration time.Duration
	Video    *VideoStream
	// Audio is the first audio stream.
	Audio *AudioStream
	// CoverStream is the index of an embedded cover picture (JPEG or PNG), -1 for none.
	CoverStream int
	Subtitles   []SubtitleStream
	// RawStream is a bare H.264 / H.265 stream with no container: always encoded, never copied.
	RawStream bool
	// HasAttachments: the file carries attachments (an MKV's subtitle fonts).
	HasAttachments bool
	// AudioTracks is every audio stream in file order, Audio first.
	AudioTracks []AudioStream
}

// Seconds is the duration in seconds (0 when unknown).
func (m *MediaInfo) Seconds() float64 { return m.Duration.Seconds() }

const probeTimeout = time.Minute

// Probe runs ffprobe on input; it fails with FFmpeg's message when the file
// can't be read, and refuses an .m4s media segment without its init segment.
func Probe(ctx context.Context, ffprobe, input string) (*MediaInfo, error) {
	if err := checkInitSegment(input); err != nil {
		return nil, err
	}
	res, err := tools.Run(ctx, tools.Cmd{
		Path:    ffprobe,
		Args:    []string{"-hide_banner", "-v", "error", "-print_format", "json", "-show_format", "-show_streams", "-show_data_hash", "MD5", input},
		Timeout: probeTimeout, LowPriority: true,
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		reason := Summarize(string(res.Stderr))
		if reason != "" {
			return nil, fmt.Errorf("FFmpeg cannot read this file: %s", reason)
		}
		return nil, fmt.Errorf("FFmpeg cannot read this file: %w", err)
	}
	return ParseProbe(res.Stdout)
}

type probeJSON struct {
	Streams []map[string]any `json:"streams"`
	Format  map[string]any   `json:"format"`
}

// ParseProbe reads the output of ffprobe -print_format json -show_format -show_streams.
func ParseProbe(data []byte) (*MediaInfo, error) {
	var p probeJSON
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("ffprobe output: %w", err)
	}
	info := &MediaInfo{CoverStream: -1}
	longest := 0.0
	for _, s := range p.Streams {
		longest = max(longest, number(s, "duration"))
		switch text(s, "codec_type") {
		case "video":
			if attachedPicture(s) {
				// Album art and thumbnails: kept for audio targets that hold a cover, never encoded as video.
				if c := text(s, "codec_name"); info.CoverStream < 0 && (c == "mjpeg" || c == "png") {
					info.CoverStream = int(number(s, "index"))
				}
				continue
			}
			if info.Video != nil {
				continue
			}
			sar := ratio(text(s, "sample_aspect_ratio"), ":")
			if sar <= 0 {
				sar = 1
			}
			info.Video = &VideoStream{
				Width: int(number(s, "width")), Height: int(number(s, "height")), SampleAspect: sar,
				Rotation: rotationOf(s), FrameRate: frameRateOf(s), Codec: text(s, "codec_name"),
				PixelFormat: text(s, "pix_fmt"), Color: colorOf(s), HeaderHash: text(s, "extradata_hash"),
			}
		case "audio":
			info.AudioTracks = append(info.AudioTracks, AudioStream{
				Channels: int(number(s, "channels")), SampleRate: int(number(s, "sample_rate")), Codec: text(s, "codec_name"),
			})
		case "attachment":
			info.HasAttachments = true
		case "subtitle":
			lang := ""
			if tags, ok := s["tags"].(map[string]any); ok {
				lang = text(tags, "language")
			}
			info.Subtitles = append(info.Subtitles, SubtitleStream{Index: int(number(s, "index")), Codec: text(s, "codec_name"), Language: lang})
		}
	}
	if len(info.AudioTracks) > 0 {
		a := info.AudioTracks[0]
		info.Audio = &a
	}
	seconds := 0.0
	if p.Format != nil {
		seconds = number(p.Format, "duration")
		if n := text(p.Format, "format_name"); n == "h264" || n == "hevc" {
			info.RawStream = true
		}
	}
	if seconds <= 0 {
		seconds = longest
	}
	if seconds > 0 {
		info.Duration = time.Duration(seconds * float64(time.Second))
	}
	return info, nil
}

func colorOf(s map[string]any) *VideoColor {
	known := func(name string) string {
		if v := text(s, name); v != "unknown" {
			return v
		}
		return ""
	}
	c := VideoColor{known("color_primaries"), known("color_transfer"), known("color_space"), known("color_range")}
	if c == (VideoColor{}) {
		return nil
	}
	return &c
}

func attachedPicture(s map[string]any) bool {
	d, ok := s["disposition"].(map[string]any)
	if !ok {
		return false
	}
	v, ok := d["attached_pic"].(float64)
	return ok && v == 1
}

// frameRateOf is the average rate, or the nominal one when the average is missing or nonsense ("0/0").
func frameRateOf(s map[string]any) float64 {
	if a := ratio(text(s, "avg_frame_rate"), "/"); a > 0 && a < 1000 {
		return a
	}
	if n := ratio(text(s, "r_frame_rate"), "/"); n > 0 && n < 1000 {
		return n
	}
	return 0
}

// rotationOf reads the display matrix side data, or the older "rotate" tag.
func rotationOf(s map[string]any) int {
	if list, ok := s["side_data_list"].([]any); ok {
		for _, item := range list {
			if m, ok := item.(map[string]any); ok {
				if r, ok := m["rotation"].(float64); ok {
					return int(math.RoundToEven(r))
				}
			}
		}
	}
	if tags, ok := s["tags"].(map[string]any); ok {
		if r, err := strconv.Atoi(text(tags, "rotate")); err == nil {
			return r
		}
	}
	return 0
}

func text(m map[string]any, name string) string {
	s, _ := m[name].(string)
	return s
}

// number reads ffprobe's numbers, mostly strings ("2.000000"), some JSON numbers.
func number(m map[string]any, name string) float64 {
	switch v := m[name].(type) {
	case float64:
		return v
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0
		}
		return f
	}
	return 0
}

// ratio reads "30000/1001" → 29.97, "16:9" → 1.78; 0 for missing or invalid values.
func ratio(s, sep string) float64 {
	n, d, ok := strings.Cut(s, sep)
	if !ok || strings.Contains(d, sep) {
		return 0
	}
	num, err1 := strconv.ParseFloat(n, 64)
	den, err2 := strconv.ParseFloat(d, 64)
	if err1 != nil || err2 != nil || den <= 0 {
		return 0
	}
	return num / den
}
