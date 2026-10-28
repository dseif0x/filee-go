package dxf

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// stream reads DXF groups from an ASCII or binary file held in memory
// (DxfTextReader / DxfBinaryReader / DxfBinaryReaderAC1009).
type stream struct {
	data      []byte
	pos       int
	binary    bool
	r12Binary bool
	codec     *textCodec
	line      int

	code  int
	kind  valueKind
	raw   []byte
	str   string
	f     float64
	i     int64
	b     bool
	h     uint64
	chunk []byte
	eof   bool
	err   error
}

func newStream(data []byte, codec *textCodec) *stream {
	s := &stream{data: data, codec: codec}
	if bytes.HasPrefix(data, []byte(binarySentinel)) {
		s.binary = true
		// AC1009 binary files use one-byte group codes: the byte after the
		// sentinel is a code (0) followed by "SECTION", whereas in later
		// versions the first code is two bytes (0, 0).
		if len(data) > len(binarySentinel)+1 && data[len(binarySentinel)+1] != 0 {
			s.r12Binary = true
		}
	}
	s.reset()
	return s
}

func (s *stream) reset() {
	s.pos, s.line, s.eof, s.err = 0, 0, false, nil
	if s.binary {
		s.pos = len(binarySentinel)
	}
	s.code, s.kind, s.str = -1, kindNone, ""
}

// position describes the current place for messages.
func (s *stream) position() string {
	if s.binary {
		return fmt.Sprintf("offset %d", s.pos)
	}
	return fmt.Sprintf("line %d", s.line)
}

func (s *stream) fail(format string, args ...any) bool {
	if s.err == nil {
		s.err = fmt.Errorf("dxf: "+format+" at %s", append(args, s.position())...)
	}
	s.eof = true
	s.code, s.str = 0, tokEOF
	return false
}

// next reads the following group, skipping 999 comments. At the end of the
// data (or after an error) it reports false and the group is (0, EOF).
func (s *stream) next() bool {
	for {
		if !s.read() {
			return false
		}
		if s.code != 999 {
			return true
		}
	}
}

func (s *stream) read() bool {
	if s.eof {
		return false
	}
	if s.binary {
		return s.readBinary()
	}
	return s.readASCII()
}

func (s *stream) readLine() ([]byte, bool) {
	if s.pos >= len(s.data) {
		return nil, false
	}
	end := bytes.IndexByte(s.data[s.pos:], '\n')
	var line []byte
	if end < 0 {
		line = s.data[s.pos:]
		s.pos = len(s.data)
	} else {
		line = s.data[s.pos : s.pos+end]
		s.pos += end + 1
	}
	s.line++
	if n := len(line); n > 0 && line[n-1] == '\r' {
		line = line[:n-1]
	}
	return line, true
}

func (s *stream) readASCII() bool {
	codeLine, ok := s.readLine()
	if !ok {
		s.eof = true
		s.code, s.str, s.kind = 0, tokEOF, kindString
		return false
	}
	t := strings.TrimSpace(string(codeLine))
	if t == "" && s.pos >= len(s.data) {
		s.eof = true
		s.code, s.str, s.kind = 0, tokEOF, kindString
		return false
	}
	code, err := strconv.Atoi(t)
	if err != nil {
		return s.fail("invalid group code %q", truncate(t, 40))
	}
	value, ok := s.readLine()
	if !ok {
		value = nil
	}
	s.setASCII(code, value)
	return true
}

func (s *stream) setASCII(code int, value []byte) {
	s.code = code
	s.kind = kindOf(code)
	s.raw = value
	s.str, s.f, s.i, s.b, s.h, s.chunk = "", 0, 0, false, 0, nil
	switch s.kind {
	case kindString, kindNone:
		s.str = unescapeCaret(s.codec.decode(value))
	case kindDouble:
		s.f = parseFloat(value)
	case kindInt16, kindInt32, kindInt64:
		s.i = parseInt(value)
	case kindHandle:
		s.h = parseHandle(value)
		s.str = strings.TrimSpace(string(value))
	case kindBool:
		s.i = parseInt(value)
		s.b = s.i != 0
	case kindChunk:
		b, err := hex.DecodeString(strings.TrimSpace(string(value)))
		if err != nil {
			b = nil
		}
		s.chunk = b
	}
}

func (s *stream) readBinary() bool {
	if s.pos >= len(s.data) {
		s.eof = true
		s.code, s.str, s.kind = 0, tokEOF, kindString
		return false
	}
	var code int
	if s.r12Binary {
		c := int(s.data[s.pos])
		s.pos++
		if c == 255 {
			if s.pos+2 > len(s.data) {
				return s.fail("truncated binary group code")
			}
			code = int(int16(binary.LittleEndian.Uint16(s.data[s.pos:])))
			s.pos += 2
		} else {
			code = c
		}
	} else {
		if s.pos+2 > len(s.data) {
			return s.fail("truncated binary group code")
		}
		code = int(int16(binary.LittleEndian.Uint16(s.data[s.pos:])))
		s.pos += 2
	}
	s.code = code
	s.kind = kindOf(code)
	s.str, s.f, s.i, s.b, s.h, s.chunk, s.raw = "", 0, 0, false, 0, nil, nil
	need := func(n int) bool {
		if s.pos+n > len(s.data) {
			return s.fail("truncated binary value for code %d", code)
		}
		return true
	}
	switch s.kind {
	case kindString, kindNone, kindHandle:
		end := bytes.IndexByte(s.data[s.pos:], 0)
		if end < 0 {
			return s.fail("unterminated binary string")
		}
		v := s.data[s.pos : s.pos+end]
		s.pos += end + 1
		s.raw = v
		if s.kind == kindHandle {
			s.h = parseHandle(v)
			s.str = string(v)
		} else {
			s.str = unescapeCaret(s.codec.decode(v))
		}
	case kindDouble:
		if !need(8) {
			return false
		}
		s.f = math.Float64frombits(binary.LittleEndian.Uint64(s.data[s.pos:]))
		s.pos += 8
	case kindInt16:
		if !need(2) {
			return false
		}
		s.i = int64(int16(binary.LittleEndian.Uint16(s.data[s.pos:])))
		s.pos += 2
	case kindInt32:
		if !need(4) {
			return false
		}
		s.i = int64(int32(binary.LittleEndian.Uint32(s.data[s.pos:])))
		s.pos += 4
	case kindInt64:
		if !need(8) {
			return false
		}
		s.i = int64(binary.LittleEndian.Uint64(s.data[s.pos:]))
		s.pos += 8
	case kindBool:
		if !need(1) {
			return false
		}
		s.b = s.data[s.pos] != 0
		s.i = int64(s.data[s.pos])
		s.pos++
	case kindChunk:
		if !need(1) {
			return false
		}
		n := int(s.data[s.pos])
		s.pos++
		if !need(n) {
			return false
		}
		s.chunk = append([]byte(nil), s.data[s.pos:s.pos+n]...)
		s.pos += n
	}
	return true
}

// value returns the current value as a Go value: string, float64, int16,
// int32, int64, bool, []byte or uint64 for handles.
func (s *stream) value() any {
	switch s.kind {
	case kindDouble:
		return s.f
	case kindInt16:
		return int16(s.i)
	case kindInt32:
		return int32(s.i)
	case kindInt64:
		return s.i
	case kindBool:
		return s.b
	case kindChunk:
		return s.chunk
	case kindHandle:
		return s.h
	}
	return s.str
}

// Typed accessors convert like ACadSharp's ValueAs* (Convert.ToXxx).
func (s *stream) float() float64 {
	switch s.kind {
	case kindDouble:
		return s.f
	case kindInt16, kindInt32, kindInt64, kindBool:
		return float64(s.i)
	case kindString, kindNone:
		return parseFloat([]byte(s.str))
	}
	return 0
}

func (s *stream) int() int64 {
	switch s.kind {
	case kindInt16, kindInt32, kindInt64, kindBool:
		return s.i
	case kindDouble:
		if math.IsNaN(s.f) || math.IsInf(s.f, 0) {
			return 0
		}
		return int64(math.Round(s.f))
	case kindHandle:
		return int64(s.h)
	case kindString, kindNone:
		return parseInt([]byte(s.str))
	}
	return 0
}

func (s *stream) i16() int16  { return int16(s.int()) }
func (s *stream) i32() int32  { return int32(s.int()) }
func (s *stream) u16() uint16 { return uint16(s.int()) }
func (s *stream) bool() bool  { return s.int() != 0 || (s.kind == kindBool && s.b) }
func (s *stream) handle() uint64 {
	switch s.kind {
	case kindHandle:
		return s.h
	case kindString, kindNone:
		return parseHandle([]byte(s.str))
	}
	return uint64(s.int())
}

// angle reads a value in degrees as radians.
func (s *stream) angle() float64 { return s.float() * math.Pi / 180 }

func (s *stream) is(code int, value string) bool { return s.code == code && s.str == value }

func parseFloat(b []byte) float64 {
	t := strings.TrimSpace(string(b))
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		// Some writers use a decimal comma.
		f, err = strconv.ParseFloat(strings.Replace(t, ",", ".", 1), 64)
		if err != nil {
			return 0
		}
	}
	return f
}

func parseInt(b []byte) int64 {
	t := strings.TrimSpace(string(b))
	i, err := strconv.ParseInt(t, 10, 64)
	if err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(t, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
		return int64(f)
	}
	return 0
}

func parseHandle(b []byte) uint64 {
	t := strings.TrimSpace(string(b))
	h, err := strconv.ParseUint(t, 16, 64)
	if err != nil {
		return 0
	}
	return h
}

func unescapeCaret(s string) string {
	if !strings.Contains(s, "^") {
		return s
	}
	r := strings.NewReplacer("^J", "\n", "^M", "\r", "^I", "\t", "^ ", "^")
	return r.Replace(s)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
