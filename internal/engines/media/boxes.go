package media

// Small, bounds-checked ISO BMFF box readers for photo metadata, and writers
// of the few boxes Filee adds; media payloads are streamed, never loaded whole.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// ErrInvalidData is malformed box or picture structure.
var ErrInvalidData = errors.New("invalid data")

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidData, fmt.Sprintf(format, a...))
}

// Box is one box header: its type, where it starts, its whole length and header length.
type Box struct {
	Type          string
	Start, Length int64
	HeaderLength  int
}

// End is the offset after the box.
func (b Box) End() int64 { return b.Start + b.Length }

func streamLength(r io.Seeker) (int64, error) {
	cur, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	end, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	_, err = r.Seek(cur, io.SeekStart)
	return end, err
}

// ReadBoxHeader reads the box header at the current position; a box reaching
// past the end of the stream is refused.
func ReadBoxHeader(r io.ReadSeeker) (Box, error) {
	start, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return Box{}, err
	}
	total, err := streamLength(r)
	if err != nil {
		return Box{}, err
	}
	var h [8]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return Box{}, err
	}
	size := int64(binary.BigEndian.Uint32(h[:]))
	typ := string(h[4:8])
	hl := 8
	switch size {
	case 1:
		if _, err := io.ReadFull(r, h[:]); err != nil {
			return Box{}, err
		}
		ext := binary.BigEndian.Uint64(h[:])
		if ext > math.MaxInt64 {
			return Box{}, invalid("media box is too large")
		}
		size, hl = int64(ext), 16
	case 0:
		size = total - start
	}
	if size < int64(hl) || size > total-start {
		return Box{}, invalid("media box extends outside its container")
	}
	return Box{typ, start, size, hl}, nil
}

// Child is a box inside a parent's payload.
type Child struct {
	Type    string
	Payload []byte
}

// Children are the boxes in data (a parent's payload) after skip bytes.
func Children(data []byte, skip int) ([]Child, error) {
	var out []Child
	at := skip
	for at < len(data) {
		if len(data)-at < 8 {
			return nil, invalid("truncated box")
		}
		size := int64(binary.BigEndian.Uint32(data[at:]))
		typ := string(data[at+4 : at+8])
		hl := 8
		switch size {
		case 1:
			if len(data)-at < 16 {
				return nil, invalid("truncated box")
			}
			ext := binary.BigEndian.Uint64(data[at+8:])
			if ext > math.MaxInt64 {
				return nil, invalid("media box is too large")
			}
			size, hl = int64(ext), 16
		case 0:
			size = int64(len(data) - at)
		}
		if size < int64(hl) || size > int64(len(data)-at) {
			return nil, invalid("media box extends outside its container")
		}
		out = append(out, Child{typ, data[at+hl : at+int(size)]})
		at += int(size)
	}
	return out, nil
}

func firstChild(children []Child, typ string) []byte {
	for _, c := range children {
		if c.Type == typ {
			return c.Payload
		}
	}
	return nil
}

// MakeBox is a box of typ (four characters) around parts.
func MakeBox(typ string, parts ...[]byte) []byte {
	if len(typ) != 4 {
		panic("a box type has four characters")
	}
	n := 8
	for _, p := range parts {
		n += len(p)
	}
	b := make([]byte, 8, n)
	binary.BigEndian.PutUint32(b, uint32(n))
	copy(b[4:], typ)
	for _, p := range parts {
		b = append(b, p...)
	}
	return b
}

// FullBox is a box that starts with a version and flags.
func FullBox(typ string, version byte, flags uint32, parts ...[]byte) []byte {
	return MakeBox(typ, append([][]byte{u32(uint32(version)<<24 | flags&0xffffff)}, parts...)...)
}

func u16(v int) []byte    { b := make([]byte, 2); binary.BigEndian.PutUint16(b, uint16(v)); return b }
func u32(v uint32) []byte { b := make([]byte, 4); binary.BigEndian.PutUint32(b, v); return b }
func u64(v uint64) []byte { b := make([]byte, 8); binary.BigEndian.PutUint64(b, v); return b }

// HandlerOf is the handler type of a track (vide, soun, meta, …) from its mdia/hdlr.
func HandlerOf(trak []byte) string {
	c, err := Children(trak, 0)
	if err != nil {
		return ""
	}
	mdia := firstChild(c, "mdia")
	if mdia == nil {
		return ""
	}
	m, err := Children(mdia, 0)
	if err != nil {
		return ""
	}
	h := firstChild(m, "hdlr")
	if len(h) < 12 {
		return ""
	}
	return string(h[8:12])
}

func descend(parent []byte, path ...string) []byte {
	cur := parent
	for _, t := range path {
		c, err := Children(cur, 0)
		if err != nil {
			return nil
		}
		cur = firstChild(c, t)
		if cur == nil {
			return nil
		}
	}
	return cur
}

// VideoSampleCount is the number of frames of the first video track of moov (its payload).
func VideoSampleCount(moov []byte) (uint32, error) {
	c, err := Children(moov, 0)
	if err != nil {
		return 0, err
	}
	for _, t := range c {
		if t.Type != "trak" || HandlerOf(t.Payload) != "vide" {
			continue
		}
		if stsz := descend(t.Payload, "mdia", "minf", "stbl", "stsz"); len(stsz) >= 12 {
			return binary.BigEndian.Uint32(stsz[8:]), nil
		}
	}
	return 0, invalid("the movie has no video track")
}

// ReadMovieHeader reads the moov box of r: its header and payload.
func ReadMovieHeader(r io.ReadSeeker, limit int64) (Box, []byte, error) {
	if limit <= 0 {
		limit = 16 << 20
	}
	total, err := streamLength(r)
	if err != nil {
		return Box{}, nil, err
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return Box{}, nil, err
	}
	for pos := int64(0); pos < total; {
		b, err := ReadBoxHeader(r)
		if err != nil {
			return Box{}, nil, err
		}
		if b.Type == "moov" {
			if b.Length > limit {
				return Box{}, nil, invalid("the movie header is too large")
			}
			data := make([]byte, b.Length-int64(b.HeaderLength))
			if _, err := io.ReadFull(r, data); err != nil {
				return Box{}, nil, err
			}
			return b, data, nil
		}
		pos = b.End()
		if _, err := r.Seek(pos, io.SeekStart); err != nil {
			return Box{}, nil, err
		}
	}
	return Box{}, nil, invalid("the file has no movie header")
}

// copyRange copies exactly n bytes; a source that ends sooner is an error.
func copyRange(dst io.Writer, src io.Reader, n int64) error {
	w, err := io.CopyN(dst, src, n)
	if err == io.EOF || (err == nil && w < n) {
		return io.ErrUnexpectedEOF
	}
	return err
}
