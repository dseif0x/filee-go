package media

// Album art for audio formats FFmpeg can't write a picture stream into. Ogg
// (Vorbis, Opus) keeps the picture as a METADATA_BLOCK_PICTURE comment (a
// FLAC picture block in base64) from an ffmetadata file; Matroska audio keeps
// it as a cover.jpg / cover.png attachment.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
)

// CoverExtras are extra inputs that carry the source's cover into the output.
type CoverExtras struct {
	// MetadataFile is an ffmetadata file with the source's tags and the picture comment (Ogg).
	MetadataFile string
	// AttachmentFile is the picture to attach (Matroska) and AttachmentMime its type.
	AttachmentFile, AttachmentMime string
}

func coverAsComment(target string) bool    { return target == "ogg" || target == "opus" }
func coverAsAttachment(target string) bool { return target == "mka" }

// PrepareCover extracts the cover (stream) of input and prepares what target
// needs to carry it; nil when the target doesn't need it or anything fails
// (the file is then converted without its cover rather than not at all).
func PrepareCover(ctx context.Context, ffmpeg, input string, stream int, target, workDir string) *CoverExtras {
	if !coverAsComment(target) && !coverAsAttachment(target) {
		return nil
	}
	folder := filepath.Join(workDir, "cover-"+randomName()[:8])
	if os.MkdirAll(folder, 0o755) != nil {
		return nil
	}
	raw := filepath.Join(folder, "cover.bin")
	run := func(args ...string) bool {
		r, err := RunFfmpeg(ctx, ffmpeg, append(common(), args...), nil, 0)
		return err == nil && r.ExitCode == 0
	}
	if !run("-i", input, "-map", "0:"+itoa(stream), "-c", "copy", "-frames:v", "1", "-f", "image2", raw) {
		return nil
	}
	img, err := os.ReadFile(raw)
	if err != nil {
		return nil
	}
	mime := CoverMime(img)
	if mime == "" {
		return nil
	}
	name := "cover.jpg"
	if mime == "image/png" {
		name = "cover.png"
	}
	picture := filepath.Join(folder, name)
	if os.Rename(raw, picture) != nil {
		return nil
	}
	if coverAsAttachment(target) {
		return &CoverExtras{AttachmentFile: picture, AttachmentMime: mime}
	}
	// The source's own tags go into the same file: the output takes its tags from it.
	meta := filepath.Join(folder, "metadata.txt")
	if !run("-i", input, "-f", "ffmetadata", meta) {
		return nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(img))
	if err != nil {
		return nil
	}
	text, err := os.ReadFile(meta)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(string(text), "\r\n", "\n"), "\n"), "\n")
	line := "METADATA_BLOCK_PICTURE=" + EscapeMetadata(PictureBlock(img, mime, cfg.Width, cfg.Height))
	// Global tags come right after ";FFMETADATA1".
	at := min(1, len(lines))
	lines = append(lines[:at], append([]string{line}, lines[at:]...)...)
	if os.WriteFile(meta, []byte(strings.Join(lines, "\n")+"\n"), 0o644) != nil {
		return nil
	}
	return &CoverExtras{MetadataFile: meta}
}

// PictureBlock is a FLAC METADATA_BLOCK_PICTURE (front cover) in base64.
func PictureBlock(img []byte, mime string, width, height int) string {
	var b []byte
	put := func(v int) { b = binary.BigEndian.AppendUint32(b, uint32(v)) }
	put(3) // front cover
	put(len(mime))
	b = append(b, mime...)
	put(0) // no description
	put(width)
	put(height)
	put(24) // colour depth
	put(0)  // not indexed
	put(len(img))
	b = append(b, img...)
	return base64.StdEncoding.EncodeToString(b)
}

// EscapeMetadata escapes '=', ';', '#', '\' and line breaks for ffmetadata.
func EscapeMetadata(v string) string {
	var b strings.Builder
	for _, c := range v {
		if c == '=' || c == ';' || c == '#' || c == '\\' || c == '\n' {
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// CoverMime is JPEG or PNG by the first bytes; "" for anything else.
func CoverMime(img []byte) string {
	switch {
	case len(img) > 4 && img[0] == 0xff && img[1] == 0xd8:
		return "image/jpeg"
	case len(img) > 8 && img[0] == 0x89 && img[1] == 'P' && img[2] == 'N' && img[3] == 'G':
		return "image/png"
	}
	return ""
}
