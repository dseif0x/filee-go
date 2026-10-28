package media

// Image → HEIC. The picture is read in Go and the preset's orientation, size
// and background applied (pictures.go), FFmpeg's x265 encodes it as HEVC
// tiles and WriteHeif puts them into the container with the colour profile
// and Exif.

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/tools"
)

// HeicTileSize: larger pictures are 512 × 512 tiles, as cameras write them.
const HeicTileSize = 512

// HeicLayout is one coded picture up to 512 × 512, else 512 × 512 tiles.
// Coded pictures have even sides; the grid crops the padding.
func HeicLayout(w, h int) (columns, rows, tileW, tileH int) {
	if w <= HeicTileSize && h <= HeicTileSize {
		return 1, 1, (w + 1) &^ 1, (h + 1) &^ 1
	}
	return (w + HeicTileSize - 1) / HeicTileSize, (h + HeicTileSize - 1) / HeicTileSize, HeicTileSize, HeicTileSize
}

// HeicCrf is x265's constant quality for a preset quality: 90 → 14, 80 → 18, 50 → 30.
func HeicCrf(quality int) int {
	return min(max(int(math.RoundToEven(50-0.4*float64(min(max(quality, 1), 100)))), 0), 51)
}

// HeicArgs turn the PNG into raw HEVC: padded to whole tiles with its own
// edge pixels, BT.601 full-range 4:2:0 like JPEG, cut into tiles and encoded
// as independent IDR pictures, 8-bit Main profile.
func HeicArgs(pngPath, output string, w, h, quality int) []string {
	cols, rows, tw, th := HeicLayout(w, h)
	pw, ph := cols*tw, rows*th
	var f []string
	if pw != w || ph != h {
		f = append(f, fmt.Sprintf("pad=%d:%d:0:0", pw, ph), fmt.Sprintf("fillborders=right=%d:bottom=%d:mode=smear", pw-w, ph-h))
	}
	f = append(f, "scale=out_color_matrix=bt601:out_range=pc", "format=yuv420p")
	if cols*rows > 1 {
		f = append(f, fmt.Sprintf("untile=%dx%d", cols, rows))
	}
	// Every tile a forced IDR picture rather than keyint=1: with keyint=1 x265
	// signals "Main Intra", which the heic brand does not allow.
	return append(common(), "-i", pngPath, "-vf", strings.Join(f, ","),
		"-c:v", "libx265", "-preset", "medium", "-crf", itoa(HeicCrf(quality)),
		"-force_key_frames", "expr:1", "-forced-idr", "1",
		"-x265-params", "bframes=0:open-gop=0:info=0:range=full:colormatrix=smpte170m:log-level=error",
		"-f", "hevc", output)
}

// EncodeHeic returns a HEIC file of the 8-bit RGB PNG (w × h) with its colour profile and Exif.
func EncodeHeic(ctx context.Context, ffmpeg, pngPath string, w, h int, icc, exif []byte, quality int, workDir string) ([]byte, error) {
	stream := filepath.Join(workDir, "heic-"+randomName()+".hevc")
	defer os.Remove(stream)
	cols, rows, tw, th := HeicLayout(w, h)
	if err := runChecked(ctx, ffmpeg, HeicArgs(pngPath, stream, w, h, quality), nil, 0); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(stream)
	if err != nil {
		return nil, err
	}
	s, err := ParseHevc(data)
	if err != nil {
		return nil, err
	}
	return WriteHeif(&HeifImage{Width: w, Height: h, Stream: s, TileWidth: tw, TileHeight: th, Columns: cols, Rows: rows, IccProfile: icc, Exif: exif})
}

func (c *Converter) toHeic(ctx context.Context, ffmpeg string, step *convert.Step, progress convert.Progress) ([]string, error) {
	pics, err := preparePictures(step.InputPath, step.From, &step.Preset.Image, step.WorkDir)
	if err != nil {
		return nil, err
	}
	progress.Report(0.15)
	var written []string
	for i, p := range pics {
		share := 0.85 / float64(len(pics))
		start := 0.15 + float64(i)*share
		est := tools.StartEstimate(progress, 5*time.Second, start, start+share*0.95)
		data, err := EncodeHeic(ctx, ffmpeg, p.png, p.width, p.height, p.icc, p.exif, step.Preset.Image.Quality, step.WorkDir)
		est.Stop()
		if err != nil {
			return nil, err
		}
		suffix := ""
		if len(pics) > 1 {
			suffix = fmt.Sprintf("_p%d", i+1)
		}
		out := step.Output.Allocate("heic", suffix)
		if out == "" {
			continue
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			return nil, err
		}
		written = append(written, out)
	}
	progress.Report(1)
	return written, nil
}
