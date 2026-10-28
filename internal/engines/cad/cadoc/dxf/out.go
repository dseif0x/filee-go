package dxf

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// out writes DXF groups in ASCII or binary (DxfAsciiWriter / DxfBinaryWriter).
type out struct {
	w      *bufio.Writer
	binary bool
	codec  *textCodec
	prec   int
	err    error
	buf    [8]byte
}

func (o *out) put(b []byte) {
	if o.err == nil {
		_, o.err = o.w.Write(b)
	}
}

func (o *out) putString(s string) {
	if o.err == nil {
		_, o.err = o.w.WriteString(s)
	}
}

func (o *out) newline() { o.putString("\r\n") }

func (o *out) code(c int) {
	if o.binary {
		binary.LittleEndian.PutUint16(o.buf[:2], uint16(int16(c)))
		o.put(o.buf[:2])
		return
	}
	s := strconv.Itoa(c)
	switch {
	case c < 10 && c >= 0:
		o.putString("  ")
	case c < 100 && c >= 0:
		o.putString(" ")
	}
	o.putString(s)
	o.newline()
}

var caretEscaper = strings.NewReplacer("^", "^ ", "\n", "^J", "\r", "^M", "\t", "^I")

// str writes a string group; the value is escaped like ACadSharp does
// (^J, ^M, ^I, "^ ") and encoded in the file's code page.
func (o *out) str(c int, s string) {
	o.code(c)
	if strings.ContainsAny(s, "^\n\r\t") {
		s = caretEscaper.Replace(s)
	}
	b := o.codec.encode(s)
	if o.binary {
		o.put(b)
		o.put([]byte{0})
		return
	}
	o.put(b)
	o.newline()
}

// strIf writes a string group unless it is empty.
func (o *out) strIf(c int, s string) {
	if s != "" {
		o.str(c, s)
	}
}

func (o *out) formatFloat(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "0"
	}
	if o.prec > 0 {
		s := strconv.FormatFloat(v, 'f', o.prec, 64)
		if strings.Contains(s, ".") {
			s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
		}
		if s == "-0" {
			s = "0"
		}
		return s
	}
	// Like .NET's "R": plain notation, exponent only for very large or
	// small magnitudes.
	if a := math.Abs(v); a == 0 || (a >= 1e-5 && a < 1e15) {
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	s := strconv.FormatFloat(v, 'E', -1, 64)
	mant, exp, _ := strings.Cut(s, "E")
	sign, digits := exp[:1], exp[1:]
	if len(digits) < 2 {
		digits = "0" + digits
	}
	return mant + "E" + sign + digits
}

func (o *out) num(c int, v float64) {
	switch kindOf(c) {
	case kindDouble:
		o.dbl(c, v)
	case kindInt16, kindInt32, kindInt64, kindBool:
		o.integer(c, int64(math.Round(v)))
	case kindHandle:
		o.handle(c, uint64(v))
	default:
		o.str(c, o.formatFloat(v))
	}
}

func (o *out) dbl(c int, v float64) {
	if kindOf(c) != kindDouble {
		o.num(c, v)
		return
	}
	o.code(c)
	if o.binary {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			v = 0
		}
		binary.LittleEndian.PutUint64(o.buf[:8], math.Float64bits(v))
		o.put(o.buf[:8])
		return
	}
	o.putString(o.formatFloat(v))
	o.newline()
}

// angle writes radians as degrees.
func (o *out) angle(c int, rad float64) { o.dbl(c, rad*180/math.Pi) }

// integer writes an integer group in the width its code requires.
func (o *out) integer(c int, v int64) {
	k := kindOf(c)
	switch k {
	case kindDouble:
		o.dbl(c, float64(v))
		return
	case kindHandle:
		o.handle(c, uint64(v))
		return
	case kindString, kindNone:
		o.str(c, strconv.FormatInt(v, 10))
		return
	}
	o.code(c)
	if !o.binary {
		if k == kindInt16 {
			v = int64(int16(v))
		} else if k == kindInt32 {
			v = int64(int32(v))
		}
		o.putString(strconv.FormatInt(v, 10))
		o.newline()
		return
	}
	switch k {
	case kindInt16:
		binary.LittleEndian.PutUint16(o.buf[:2], uint16(int16(v)))
		o.put(o.buf[:2])
	case kindInt32:
		binary.LittleEndian.PutUint32(o.buf[:4], uint32(int32(v)))
		o.put(o.buf[:4])
	case kindInt64:
		binary.LittleEndian.PutUint64(o.buf[:8], uint64(v))
		o.put(o.buf[:8])
	case kindBool:
		if v != 0 {
			o.put([]byte{1})
		} else {
			o.put([]byte{0})
		}
	}
}

func (o *out) boolean(c int, b bool) {
	if b {
		o.integer(c, 1)
	} else {
		o.integer(c, 0)
	}
}

func (o *out) handle(c int, h uint64) {
	if kindOf(c) != kindHandle {
		o.integer(c, int64(h))
		return
	}
	o.code(c)
	s := strings.ToUpper(strconv.FormatUint(h, 16))
	if o.binary {
		o.putString(s)
		o.put([]byte{0})
		return
	}
	o.putString(s)
	o.newline()
}

// ref writes the handle of an object (0 for nil).
func (o *out) ref(c int, obj cadoc.Object) {
	if isNilObject(obj) {
		o.handle(c, 0)
		return
	}
	o.handle(c, obj.Base().Handle)
}

// refIf writes the handle of an object unless it is nil.
func (o *out) refIf(c int, obj cadoc.Object) {
	if !isNilObject(obj) {
		o.handle(c, obj.Base().Handle)
	}
}

// chunk writes binary data, split into groups of at most 127 bytes.
func (o *out) chunk(c int, b []byte) {
	const max = 127
	if len(b) == 0 {
		o.code(c)
		if o.binary {
			o.put([]byte{0})
		} else {
			o.newline()
		}
		return
	}
	for i := 0; i < len(b); i += max {
		end := min(i+max, len(b))
		o.code(c)
		if o.binary {
			o.put([]byte{byte(end - i)})
			o.put(b[i:end])
		} else {
			o.putString(strings.ToUpper(hex.EncodeToString(b[i:end])))
			o.newline()
		}
	}
}

func (o *out) vec3(c int, v cadoc.Vec3) {
	o.dbl(c, v.X)
	o.dbl(c+10, v.Y)
	o.dbl(c+20, v.Z)
}

func (o *out) vec2(c int, v cadoc.Vec2) {
	o.dbl(c, v.X)
	o.dbl(c+10, v.Y)
}

// any writes a value read from a file (GroupCode, XRecord entries, header
// variables) in the representation its code needs.
func (o *out) any(c int, v any) {
	switch x := v.(type) {
	case string:
		switch kindOf(c) {
		case kindString, kindNone:
			o.str(c, x)
		case kindHandle:
			o.handle(c, parseHandle([]byte(x)))
		case kindDouble:
			o.dbl(c, parseFloat([]byte(x)))
		default:
			o.integer(c, parseInt([]byte(x)))
		}
	case float64:
		o.num(c, x)
	case float32:
		o.num(c, float64(x))
	case int16:
		o.integer(c, int64(x))
	case int32:
		o.integer(c, int64(x))
	case int64:
		o.integer(c, x)
	case int:
		o.integer(c, int64(x))
	case uint8:
		o.integer(c, int64(x))
	case uint16:
		o.integer(c, int64(x))
	case uint64:
		o.handle(c, x)
	case bool:
		o.boolean(c, x)
	case []byte:
		o.chunk(c, x)
	case cadoc.Vec3:
		o.vec3(c, x)
	case cadoc.Vec2:
		o.vec2(c, x)
	case cadoc.Object:
		o.ref(c, x)
	case nil:
		switch kindOf(c) {
		case kindString, kindNone:
			o.str(c, "")
		case kindHandle:
			o.handle(c, 0)
		default:
			o.integer(c, 0)
		}
	}
}

func isNilObject(o cadoc.Object) bool {
	if o == nil {
		return true
	}
	v := reflect.ValueOf(o)
	return v.Kind() == reflect.Pointer && v.IsNil()
}
