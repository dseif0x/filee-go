// Package heic writes HEIC files: the image engine (package images) reads the
// picture and applies the preset, FFmpeg's x265 encodes it as HEVC tiles (there
// is no HEVC encoder in pure Go) and Filee's own HEIF writer puts them into
// the container with the colour profile and Exif. The FFmpeg engine (package
// media) calls Convert for its edges to HEIC and Encode for Live Photos.
package heic

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/images"
	"github.com/dseif0x/filee-go/internal/tools"
)

// TileSize of larger pictures, as cameras write them: every decoder handles 512 × 512 HEVC pictures.
const TileSize = 512

// commonArgs are FFmpeg's quiet, non-interactive options.
var commonArgs = []string{"-hide_banner", "-nostdin", "-nostats", "-v", "error", "-y"}

type picture struct {
	png           string
	width, height int
	icc, exif     []byte
}

// Convert writes every picture of the step's source (TIFF pages; the first
// frame of an animation) as HEIC, after the preset's picture options. HEIC is
// written without alpha: transparency is flattened onto the preset's background.
func Convert(ctx context.Context, ffmpeg string, step *convert.Step, progress convert.Progress) ([]string, error) {
	pics, err := prepare(ctx, step)
	if err != nil {
		return nil, err
	}
	progress.Report(0.15)
	var written []string
	for i, p := range pics {
		share := 0.85 / float64(len(pics))
		start := 0.15 + float64(i)*share
		est := tools.StartEstimate(progress, 5*time.Second, start, start+share*0.95)
		heif, err := Encode(ctx, ffmpeg, p.png, p.width, p.height, p.icc, p.exif, step.Preset.Image.Quality, step.WorkDir)
		est.Stop()
		if err != nil {
			return written, err
		}
		suffix := ""
		if len(pics) > 1 {
			suffix = fmt.Sprintf("_p%d", i+1)
		}
		out := step.Output.Allocate("heic", suffix)
		if out == "" {
			continue
		}
		if err := os.WriteFile(out, heif, 0o644); err != nil {
			return written, err
		}
		written = append(written, out)
		progress.Report(0.15 + share*float64(i+1))
	}
	progress.Report(1)
	return written, nil
}

func prepare(ctx context.Context, step *convert.Step) ([]picture, error) {
	from := step.From
	pic, err := images.Decode(step.InputPath, from, step.Preset)
	if err != nil {
		return nil, err
	}
	var out []picture
	for _, f := range images.SelectFrames(pic, from, "heic") {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		images.ApplyOptions(f, &step.Preset.Image, "heic")
		p := picture{width: f.Width(), height: f.Height(), icc: f.Meta.ICC}
		if f.Meta.Exif != nil {
			// The pixels are upright now, and an old thumbnail would show them as they were.
			p.exif = images.ExifUpright(f.Meta.Exif)
		}
		p.png = filepath.Join(step.WorkDir, "heic-"+randomName()+".png")
		// FFmpeg only needs the pixels; the profiles go into the HEIF container.
		if err := images.Encode(p.png, f.Img, "png", nil); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func randomName() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Encode is a HEIC file of the 8-bit RGB PNG png (width × height) with its
// colour profile and Exif: x265 encodes the tiles into workDir, WriteHEIF
// puts them together.
func Encode(ctx context.Context, ffmpeg, png string, width, height int, icc, exif []byte, quality int, workDir string) ([]byte, error) {
	stream := filepath.Join(workDir, "heic-"+randomName()+".hevc")
	defer os.Remove(stream)
	cols, rows, tw, th := Layout(width, height)
	if _, err := tools.Run(ctx, tools.Cmd{Path: ffmpeg, Args: Arguments(png, stream, width, height, quality)}); err != nil {
		if ee, ok := err.(*tools.ExitError); ok {
			reason := summarize(ee.Stderr)
			if reason != "" {
				return nil, fmt.Errorf("FFmpeg failed (exit code %d): %s", ee.Code, reason)
			}
			return nil, fmt.Errorf("FFmpeg failed (exit code %d)", ee.Code)
		}
		return nil, err
	}
	data, err := os.ReadFile(stream)
	if err != nil {
		return nil, err
	}
	s, err := ParseStream(data)
	if err != nil {
		return nil, err
	}
	return WriteHEIF(&Image{Width: width, Height: height, Stream: s, TileWidth: tw, TileHeight: th, Columns: cols, Rows: rows, ICC: icc, Exif: exif})
}

// summarize is the last distinct lines of FFmpeg's errors, without their "[… @ 0x…]" prefixes.
func summarize(stderr string) string {
	var lines []string
	seen := map[string]bool{}
	for _, l := range strings.Split(stderr, "\n") {
		l = strings.TrimSpace(strings.TrimPrefix(l, "…"))
		for strings.HasPrefix(l, "[") {
			i := strings.Index(l, "] ")
			if i < 0 {
				break
			}
			l = strings.TrimSpace(l[i+2:])
		}
		if l != "" && !seen[l] {
			seen[l] = true
			lines = append(lines, l)
		}
	}
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return strings.Join(lines, " ")
}

// Layout is one coded picture for a picture up to 512 × 512, otherwise 512 ×
// 512 tiles. Coded pictures have even sides (4:2:0); the grid crops the padding away.
func Layout(width, height int) (cols, rows, tileW, tileH int) {
	if width <= TileSize && height <= TileSize {
		return 1, 1, (width + 1) &^ 1, (height + 1) &^ 1
	}
	return (width + TileSize - 1) / TileSize, (height + TileSize - 1) / TileSize, TileSize, TileSize
}

// Crf is x265's constant quality for a preset quality of 1–100: 90 → 14, 80 → 18, 50 → 30.
func Crf(quality int) int {
	q := max(1, min(100, quality))
	return max(0, min(51, int(math.Round(50-0.4*float64(q)))))
}

// Arguments turn the PNG into raw HEVC: padded to whole tiles with its own
// edge pixels, BT.601 full-range 4:2:0 like JPEG and phone cameras, cut into
// tiles row by row and encoded by x265 as independent IDR pictures, 8-bit
// Main profile, which every HEIC decoder plays.
func Arguments(png, output string, width, height, quality int) []string {
	cols, rows, tw, th := Layout(width, height)
	pw, ph := cols*tw, rows*th
	var filters []string
	if pw != width || ph != height {
		filters = append(filters, fmt.Sprintf("pad=%d:%d:0:0", pw, ph),
			fmt.Sprintf("fillborders=right=%d:bottom=%d:mode=smear", pw-width, ph-height))
	}
	filters = append(filters, "scale=out_color_matrix=bt601:out_range=pc", "format=yuv420p")
	if cols*rows > 1 {
		filters = append(filters, fmt.Sprintf("untile=%dx%d", cols, rows))
	}
	args := append([]string{}, commonArgs...)
	return append(args, "-i", png, "-vf", strings.Join(filters, ","),
		// Every tile a forced IDR picture rather than keyint=1: with keyint=1 x265 signals "Main Intra",
		// which the "heic" brand does not allow and Windows' decoder gets wrong.
		"-c:v", "libx265", "-preset", "medium", "-crf", strconv.Itoa(Crf(quality)),
		"-force_key_frames", "expr:1", "-forced-idr", "1",
		"-x265-params", "bframes=0:open-gop=0:info=0:range=full:colormatrix=smpte170m:log-level=error",
		"-f", "hevc", output)
}
