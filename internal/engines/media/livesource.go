package media

// The video of a Live / Motion Photo, copied into the job's work directory:
// the photo, the MOV next to it and a LIVP are only read. Archive entry
// names are never used as paths.

import (
	"archive/zip"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/dseif0x/filee-go/internal/convert"
)

const (
	maxMotionVideoBytes = 512 << 20
	maxLiveImageBytes   = 64 << 20
	// A LIVP holds a photo and its MOV; anything much larger is not one.
	maxPackageEntries = 16
)

var (
	movieExtensions = []string{".mov", ".mp4"}
	stillExtensions = []string{".jpg", ".jpeg", ".heic", ".heif"}
)

// ExtractMotion copies the video of input (format from) into workDir: the
// video appended to an Android Motion Photo, the MOV of a LIVP, or the MOV
// next to an iPhone photo with the same name and content identifier. It
// returns the copy and the iPhone MOV it came from ("" for the others): no
// output may replace that one.
func ExtractMotion(input, from, workDir string, log *slog.Logger) (video, companion string, err error) {
	// Stale or broken metadata goes to the log, the user gets the plain message.
	var reason error
	if from == "livp" {
		v, err := copyPackageVideo(input, workDir, log)
		if err == nil {
			return v, "", nil
		}
		if !unreadable(err) {
			return "", "", err
		}
		reason = err
	} else {
		v, err := copyEmbedded(input, workDir)
		switch {
		case err == nil && v != "":
			return v, "", nil
		case err != nil && !unreadable(err):
			return "", "", err
		case err != nil:
			reason = err
		}
	}
	if from == "jpg" || from == "heic" {
		if c := pairedMovie(input); c != "" {
			v, err := copyMovieFile(c, workDir)
			if err == nil {
				return v, c, nil
			}
			if !unreadable(err) {
				return "", "", err
			}
			if reason == nil {
				reason = err
			}
		}
	}
	if reason != nil && log != nil {
		log.Info("no motion video", "file", input, "reason", reason)
	}
	return "", "", convert.Refuse("error.no_motion_photo", "")
}

func unreadable(err error) bool {
	return errors.Is(err, ErrInvalidData) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) || errors.Is(err, zip.ErrFormat)
}

func copyEmbedded(input, workDir string) (string, error) {
	f, err := os.Open(input)
	if err != nil {
		return "", err
	}
	defer f.Close()
	off, length, ok, err := FindMotionVideo(f)
	if err != nil || !ok {
		return "", err
	}
	if length > maxMotionVideoBytes {
		return "", invalid("Motion Photo video exceeds 512 MiB")
	}
	out := filepath.Join(workDir, randomName()+".mp4")
	t, err := os.Create(out)
	if err != nil {
		return "", err
	}
	defer t.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return "", err
	}
	if err := copyRange(t, f, length); err != nil {
		return "", err
	}
	return out, t.Close()
}

// pairedMovie is the MOV saved next to an iPhone photo (IMG_0001.HEIC +
// IMG_0001.MOV) when both carry the same content identifier.
func pairedMovie(input string) string {
	id := ImageIdentifier(input)
	if id == "" {
		return ""
	}
	full, err := filepath.Abs(input)
	if err != nil {
		return ""
	}
	name := strings.TrimSuffix(filepath.Base(full), filepath.Ext(full))
	entries, err := os.ReadDir(filepath.Dir(full))
	if err != nil {
		return ""
	}
	var movies []string
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.EqualFold(filepath.Ext(n), ".mov") && strings.EqualFold(strings.TrimSuffix(n, filepath.Ext(n)), name) {
			movies = append(movies, filepath.Join(filepath.Dir(full), n))
		}
	}
	if len(movies) != 1 {
		return ""
	}
	if m, err := MovieIdentifier(movies[0]); err == nil && strings.EqualFold(m, id) {
		return movies[0]
	}
	return ""
}

// ReadLivpStill is the photo of a LIVP for the image engines: its bytes and format (jpg or heic).
func ReadLivpStill(livp string) ([]byte, string, error) {
	z, err := zip.OpenReader(livp)
	if err != nil {
		return nil, "", err
	}
	defer z.Close()
	e, err := singleEntry(&z.Reader, stillExtensions, "photo")
	if err != nil {
		return nil, "", err
	}
	if e.UncompressedSize64 == 0 || e.UncompressedSize64 > maxLiveImageBytes {
		return nil, "", invalid("the Live Photo's picture is empty or too large")
	}
	r, err := e.Open()
	if err != nil {
		return nil, "", err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, maxLiveImageBytes+1))
	if err != nil {
		return nil, "", err
	}
	format := "jpg"
	if x := strings.ToLower(filepath.Ext(e.Name)); x == ".heic" || x == ".heif" {
		format = "heic"
	}
	return data, format, nil
}

// copyPackageVideo is the MOV of a LIVP. The package makes the pair: an
// identifier that doesn't match the photo's is only logged.
func copyPackageVideo(path, workDir string, log *slog.Logger) (string, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer z.Close()
	e, err := singleEntry(&z.Reader, movieExtensions, "video")
	if err != nil {
		return "", err
	}
	movie := filepath.Join(workDir, randomName()+".mov")
	if err := copyEntry(e, movie, maxMotionVideoBytes); err != nil {
		return "", err
	}
	imageID := ""
	if stills, err := entries(&z.Reader, stillExtensions); err == nil && len(stills) == 1 &&
		stills[0].UncompressedSize64 > 0 && stills[0].UncompressedSize64 <= maxLiveImageBytes {
		image := filepath.Join(workDir, randomName()+strings.ToLower(filepath.Ext(stills[0].Name)))
		if copyEntry(stills[0], image, maxLiveImageBytes) == nil {
			imageID = ImageIdentifier(image)
		}
		os.Remove(image)
	}
	movieID, _ := MovieIdentifier(movie)
	if (imageID == "" || movieID == "" || !strings.EqualFold(imageID, movieID)) && log != nil {
		log.Warn("the photo's and video's Live Photo identifiers don't match; taking the package's video",
			"file", path, "image", orNone(imageID), "movie", orNone(movieID))
	}
	return movie, nil
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

// entries are the package's files with one of extensions, without macOS resource forks.
func entries(z *zip.Reader, extensions []string) ([]*zip.File, error) {
	if len(z.File) > maxPackageEntries {
		return nil, invalid("a LIVP holds a photo and its video, not this many files")
	}
	var out []*zip.File
	for _, f := range z.File {
		name := f.Name[strings.LastIndexAny(f.Name, `/\`)+1:]
		if name == "" || strings.HasPrefix(name, "._") || strings.HasPrefix(strings.ToUpper(f.Name), "__MACOSX/") {
			continue
		}
		x := strings.ToLower(filepath.Ext(name))
		for _, e := range extensions {
			if x == e {
				out = append(out, f)
				break
			}
		}
	}
	return out, nil
}

func singleEntry(z *zip.Reader, extensions []string, what string) (*zip.File, error) {
	es, err := entries(z, extensions)
	if err != nil {
		return nil, err
	}
	if len(es) != 1 {
		return nil, invalid("a LIVP must contain exactly one %s", what)
	}
	return es[0], nil
}

func copyEntry(e *zip.File, target string, limit int64) error {
	if e.UncompressedSize64 == 0 || e.UncompressedSize64 > uint64(limit) {
		return invalid("Live Photo archive entry is empty or too large")
	}
	r, err := e.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	defer out.Close()
	if err := copyRange(out, r, int64(e.UncompressedSize64)); err != nil {
		return err
	}
	return out.Close()
}

// copyMovieFile copies the iPhone MOV, so the conversion never reads the
// user's file it might be told to replace.
func copyMovieFile(path, workDir string) (string, error) {
	src, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer src.Close()
	st, err := src.Stat()
	if err != nil {
		return "", err
	}
	if st.Size() <= 0 || st.Size() > maxMotionVideoBytes {
		return "", invalid("the Live Photo video is empty or too large")
	}
	out := filepath.Join(workDir, randomName()+".mov")
	t, err := os.Create(out)
	if err != nil {
		return "", err
	}
	defer t.Close()
	if err := copyRange(t, src, st.Size()); err != nil {
		return "", err
	}
	return out, t.Close()
}
