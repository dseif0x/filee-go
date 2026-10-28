package dwg

import (
	"time"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// mergedReader is DwgMergedReader: R2007+ objects keep their strings in a
// separate string stream and their handles in a handle stream after the
// data; this routes each kind of read to its stream. For older versions the
// three streams are one reader.
type mergedReader struct {
	main, text, handle *BitReader
}

func newMerged(main, text, handle *BitReader) *mergedReader {
	return &mergedReader{main: main, text: text, handle: handle}
}

func (m *mergedReader) PositionInBits() int64        { return m.main.PositionInBits() }
func (m *mergedReader) SetPositionInBits(p int64)    { m.main.SetPositionInBits(p) }
func (m *mergedReader) Read2Bits() byte              { return m.main.Read2Bits() }
func (m *mergedReader) ReadBit() bool                { return m.main.ReadBit() }
func (m *mergedReader) ReadBitAsShort() int16        { return m.main.ReadBitAsShort() }
func (m *mergedReader) ReadBitShort() int16          { return m.main.ReadBitShort() }
func (m *mergedReader) ReadBitShortAsBool() bool     { return m.main.ReadBitShortAsBool() }
func (m *mergedReader) ReadBitLong() int32           { return m.main.ReadBitLong() }
func (m *mergedReader) ReadBitLongLong() int64       { return m.main.ReadBitLongLong() }
func (m *mergedReader) ReadBitDouble() float64       { return m.main.ReadBitDouble() }
func (m *mergedReader) Read2BitDouble() cadoc.Vec2   { return m.main.Read2BitDouble() }
func (m *mergedReader) Read3BitDouble() cadoc.Vec3   { return m.main.Read3BitDouble() }
func (m *mergedReader) Read2RawDouble() cadoc.Vec2   { return m.main.Read2RawDouble() }
func (m *mergedReader) Read3RawDouble() cadoc.Vec3   { return m.main.Read3RawDouble() }
func (m *mergedReader) ReadRC() byte                 { return m.main.ReadRC() }
func (m *mergedReader) ReadRawChar() byte            { return m.main.ReadRawChar() }
func (m *mergedReader) ReadBytes(n int) []byte       { return m.main.ReadBytes(n) }
func (m *mergedReader) ReadShort() int16             { return m.main.ReadShort() }
func (m *mergedReader) ReadRawLong() int64           { return m.main.ReadRawLong() }
func (m *mergedReader) ReadRawULong() uint64         { return m.main.ReadRawULong() }
func (m *mergedReader) ReadDateTime() time.Time      { return m.main.ReadDateTime() }
func (m *mergedReader) ReadTimeSpan() time.Duration  { return m.main.ReadTimeSpan() }
func (m *mergedReader) ReadBitExtrusion() cadoc.Vec3 { return m.main.ReadBitExtrusion() }
func (m *mergedReader) ReadBitThickness() float64    { return m.main.ReadBitThickness() }
func (m *mergedReader) ReadBitDoubleWithDefault(d float64) float64 {
	return m.main.ReadBitDoubleWithDefault(d)
}
func (m *mergedReader) ReadEnColor() (cadoc.Color, cadoc.Transparency, bool) {
	return m.main.ReadEnColor()
}
func (m *mergedReader) ReadSentinel() [16]byte { return m.main.ReadSentinel() }
func (m *mergedReader) ResetShift() uint16     { return m.main.ResetShift() }

// HandleReference reads from the handle stream.
func (m *mergedReader) HandleReference(ref uint64) uint64 { return m.handle.HandleReference(ref) }

// ReadTextUnicode reads from the string stream ("" when it is empty).
func (m *mergedReader) ReadTextUnicode() string {
	if m.text.Empty {
		return ""
	}
	return m.text.ReadTextUnicode()
}

// ReadVariableText reads from the string stream ("" when it is empty).
func (m *mergedReader) ReadVariableText() string {
	if m.text.Empty {
		return ""
	}
	return m.text.ReadVariableText()
}

// ReadColorByIndex reads a BS index cast to a byte, as DwgMergedReader does.
func (m *mergedReader) ReadColorByIndex() cadoc.Color {
	return colorIndex(int16(byte(m.ReadBitShort())))
}

// ReadCmColor reads a CMC; from R2004 on the color and book names come from
// the string stream. useTextStream forces that for older versions (the C#
// parameter, used by table styles).
func (m *mergedReader) ReadCmColor(useTextStream bool) cadoc.Color {
	if m.main.Version < cadoc.AC1018 && !useTextStream {
		return m.main.ReadCmColor()
	}
	m.ReadBitShort()
	rgb := uint32(m.ReadBitLong())
	c := cmColorFromRGB(rgb)
	id := m.ReadRC()
	if id&1 == 1 {
		m.ReadVariableText()
	}
	if id&2 == 2 {
		m.ReadVariableText()
	}
	return c
}
