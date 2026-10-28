package media

// .m4s files: a fragmented MP4 with its own moov is read by FFmpeg like any
// MP4; a DASH media segment holds only fragments and needs a separate init
// segment, so Filee refuses it first with a message that says why.

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dseif0x/filee-go/internal/convert"
)

const maxTopBoxes = 4096

func checkInitSegment(path string) error {
	if !strings.EqualFold(filepath.Ext(path), ".m4s") {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil // FFmpeg reports what is wrong with the file.
	}
	defer f.Close()
	if NeedsInitSegment(f) {
		return convert.Refuse("error.m4s_needs_init", "")
	}
	return nil
}

// NeedsInitSegment is true when the top-level boxes reach a moof before any
// moov. False when a moov comes first, and for data that isn't boxes.
func NeedsInitSegment(r io.ReadSeeker) bool {
	total, err := streamLength(r)
	if err != nil {
		return false
	}
	pos, _ := r.Seek(0, io.SeekCurrent)
	for i := 0; i < maxTopBoxes && total-pos >= 8; i++ {
		b, err := ReadBoxHeader(r)
		if err != nil {
			return false // not ISO BMFF, or damaged: FFmpeg decides
		}
		switch b.Type {
		case "moov":
			return false
		case "moof":
			return true
		}
		pos = b.End()
		if _, err := r.Seek(pos, io.SeekStart); err != nil {
			return false
		}
	}
	return false
}
