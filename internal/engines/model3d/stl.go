package model3d

// STL (stereolithography): binary and ASCII. Triangles only, no units (read as
// millimetres), Z up, no colours, UVs or materials. Binary files are streamed
// in blocks and vertices merged by position.

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
)

const (
	stlHeaderSize = 84
	stlRecordSize = 50
)

func isStlText(b byte) bool { return b == 9 || b == 10 || b == 13 || (b >= 32 && b < 127) }

func readStl(ctx context.Context, path string, progress func(float64)) (*Model, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	length := info.Size()
	header := make([]byte, stlHeaderSize)
	headerRead, _ := io.ReadFull(f, header)
	header = header[:headerRead]
	var count uint32
	if headerRead == stlHeaderSize {
		count = binary.LittleEndian.Uint32(header[80:])
	}
	exactBinary := headerRead == stlHeaderSize && stlHeaderSize+int64(stlRecordSize)*int64(count) == length
	startsWithSolid := strings.HasPrefix(strings.ToLower(strings.TrimLeft(string(header), " \t\r\n\x00")), "solid")
	fitsBinary := headerRead == stlHeaderSize && count > 0 && stlHeaderSize+int64(stlRecordSize)*int64(count) <= length
	if startsWithSolid && fitsBinary && !exactBinary {
		// Some exporters start a binary header with "solid" and add bytes at the end: text has no control bytes.
		sample := make([]byte, 512)
		n, _ := io.ReadFull(f, sample)
		startsWithSolid = true
		for _, b := range sample[:n] {
			if !isStlText(b) {
				startsWithSolid = false
				break
			}
		}
		if _, err := f.Seek(stlHeaderSize, io.SeekStart); err != nil {
			return nil, err
		}
	}

	var model *Model
	switch {
	case exactBinary || (!startsWithSolid && fitsBinary):
		model, err = readStlBinary(ctx, f, int(min(count, math.MaxInt32/3)), progress)
	case startsWithSolid:
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		model, err = readStlASCII(ctx, f, length, progress)
	case headerRead < stlHeaderSize:
		err = errors.New("The file is too short to be an STL file.")
	default:
		err = fmt.Errorf("The binary STL file is cut short: it announces %s triangles but holds only %s.",
			groupDigits(int64(count)), groupDigits((length-stlHeaderSize)/stlRecordSize))
	}
	if err != nil {
		return nil, err
	}
	for _, p := range model.Parts {
		p.Transform(zUpToYUp)
	}
	return model, nil
}

// groupDigits writes n with thousands separators ("1,000,000"), as .NET's N0 does.
func groupDigits(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func readStlBinary(ctx context.Context, r io.Reader, count int, progress func(float64)) (*Model, error) {
	part := newPart("STL")
	part.Indices = make([]int, 0, count*3)
	merger := newPositionMerger(part, count/2)
	buffer := make([]byte, stlRecordSize*4096)
	done := 0
	vec := func(b []byte) vec3 {
		return vec3{
			math.Float32frombits(binary.LittleEndian.Uint32(b)),
			math.Float32frombits(binary.LittleEndian.Uint32(b[4:])),
			math.Float32frombits(binary.LittleEndian.Uint32(b[8:])),
		}
	}
	for done < count {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		batch := min(4096, count-done)
		if _, err := io.ReadFull(r, buffer[:batch*stlRecordSize]); err != nil {
			return nil, fmt.Errorf("The binary STL file is cut short: %w", err)
		}
		for i := 0; i < batch; i++ {
			rec := buffer[i*stlRecordSize : (i+1)*stlRecordSize]
			a, b, c := vec(rec[12:]), vec(rec[24:]), vec(rec[36:])
			if !a.finite() || !b.finite() || !c.finite() {
				continue
			}
			part.AddTriangle(merger.add(a), merger.add(b), merger.add(c))
		}
		done += batch
		if progress != nil {
			progress(float64(done) / float64(count))
		}
	}
	return &Model{Parts: []*Part{part}}, nil
}

func readStlASCII(ctx context.Context, f *os.File, length int64, progress func(float64)) (*Model, error) {
	model := &Model{}
	var part *Part
	var merger *positionMerger
	var corners [3]int
	cornerCount := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<16), 1<<24)
	line := 0
	var read int64
	for sc.Scan() {
		line++
		raw := sc.Text()
		read += int64(len(raw)) + 1
		text := strings.TrimSpace(strings.TrimPrefix(raw, "\uFEFF"))
		lower := strings.ToLower(text)
		switch {
		case strings.HasPrefix(lower, "vertex"):
			if part == nil {
				part = newPart("STL")
				merger = newPositionMerger(part, 1024)
				model.Parts = append(model.Parts, part)
			}
			v, err := parseStlVector(text[6:], line)
			if err != nil {
				return nil, err
			}
			if cornerCount < 3 {
				corners[cornerCount] = merger.add(v)
				cornerCount++
			}
		case strings.HasPrefix(lower, "endloop"):
			if cornerCount == 3 {
				part.AddTriangle(corners[0], corners[1], corners[2])
			}
			cornerCount = 0
		case strings.HasPrefix(lower, "solid"):
			name := strings.TrimSpace(text[5:])
			if name == "" {
				name = "STL"
			}
			part = newPart(name)
			merger = newPositionMerger(part, 1024)
			model.Parts = append(model.Parts, part)
		}
		if line&0xFFFF == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if progress != nil {
				progress(float64(read) / float64(max(1, length)))
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return model, nil
}

func parseStlVector(text string, line int) (vec3, error) {
	var values [3]float32
	found := 0
	for _, token := range strings.FieldsFunc(text, func(r rune) bool { return r == ' ' || r == '\t' }) {
		if found == 3 {
			return vec3{}, fmt.Errorf("Line %d of the STL file has an invalid vertex.", line)
		}
		v, ok := parseFloat32(token)
		if !ok {
			return vec3{}, fmt.Errorf("Line %d of the STL file has an invalid vertex.", line)
		}
		values[found] = v
		found++
	}
	if found != 3 {
		return vec3{}, fmt.Errorf("Line %d of the STL file has an invalid vertex.", line)
	}
	return vec3{values[0], values[1], values[2]}, nil
}

// parseFloat32 reads a number as .NET's float.TryParse with the invariant
// culture does: "1e3", "-.5", "Infinity", "NaN".
func parseFloat32(s string) (float32, bool) {
	v, err := strconv.ParseFloat(s, 32)
	if err != nil {
		var ne *strconv.NumError
		if errors.As(err, &ne) && ne.Err == strconv.ErrRange {
			return float32(v), true
		}
		switch s {
		case "Infinity", "∞":
			return float32(math.Inf(1)), true
		case "-Infinity", "-∞":
			return float32(math.Inf(-1)), true
		}
		return 0, false
	}
	if strings.ContainsAny(s, "xXpP_") || strings.EqualFold(strings.TrimLeft(s, "+-"), "inf") {
		return 0, false
	}
	return float32(v), true
}

// writeStl writes a binary STL (Z up, millimetres) with every part in one
// solid and facet normals from the winding.
func writeStl(ctx context.Context, m *Model, w io.Writer) error {
	bw := bufio.NewWriterSize(w, 1<<20)
	header := make([]byte, stlHeaderSize)
	copy(header, "Binary STL written by Filee")
	binary.LittleEndian.PutUint32(header[80:], uint32(m.TriangleCount()))
	bw.Write(header)
	rec := make([]byte, stlRecordSize)
	put := func(b []byte, v vec3) {
		binary.LittleEndian.PutUint32(b, math.Float32bits(v.X))
		binary.LittleEndian.PutUint32(b[4:], math.Float32bits(v.Y))
		binary.LittleEndian.PutUint32(b[8:], math.Float32bits(v.Z))
	}
	for _, p := range m.Parts {
		for t := 0; t+2 < len(p.Indices); t += 3 {
			if t&0xFFFF == 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			a := toZUp(p.Position(p.Indices[t]))
			b := toZUp(p.Position(p.Indices[t+1]))
			c := toZUp(p.Position(p.Indices[t+2]))
			n := b.sub(a).cross(c.sub(a))
			if n.lengthSquared() > 0 {
				n = n.normalize()
			} else {
				n = vec3{}
			}
			if !finite(n.X) {
				n = vec3{}
			}
			put(rec, n)
			put(rec[12:], a)
			put(rec[24:], b)
			put(rec[36:], c)
			rec[48], rec[49] = 0, 0
			bw.Write(rec)
		}
	}
	return bw.Flush()
}

// positionMerger adds positions to a part, reusing the vertex of an identical position.
type positionMerger struct {
	part *Part
	seen map[vec3]int
}

func newPositionMerger(p *Part, capacity int) *positionMerger {
	return &positionMerger{part: p, seen: make(map[vec3]int, max(16, min(capacity, 1<<24)))}
}

func (m *positionMerger) add(pos vec3) int {
	// -0 and 0 are the same point.
	pos = pos.noNegativeZero()
	if i, ok := m.seen[pos]; ok {
		return i
	}
	i := m.part.AddVertex(pos, nil, nil, nil)
	m.seen[pos] = i
	return i
}
