package gfx

import (
	"encoding/binary"
	"math"
	"sync"
)

var (
	srgbOnce sync.Once
	srgbICC  []byte
)

// SRGBProfile returns a small ICC v2 sRGB profile (PDF/A output intents).
func SRGBProfile() []byte {
	srgbOnce.Do(func() { srgbICC = buildSRGB() })
	return srgbICC
}

func buildSRGB() []byte {
	be := binary.BigEndian
	s15 := func(v float64) []byte {
		b := make([]byte, 4)
		be.PutUint32(b, uint32(int32(math.Round(v*65536))))
		return b
	}
	xyz := func(x, y, z float64) []byte {
		b := []byte("XYZ \x00\x00\x00\x00")
		b = append(b, s15(x)...)
		b = append(b, s15(y)...)
		return append(b, s15(z)...)
	}
	desc := func(text string) []byte {
		b := []byte("desc\x00\x00\x00\x00")
		n := make([]byte, 4)
		be.PutUint32(n, uint32(len(text)+1))
		b = append(b, n...)
		b = append(b, text...)
		b = append(b, 0)
		b = append(b, make([]byte, 4+4)...)    // Unicode language and count
		b = append(b, make([]byte, 2+1+67)...) // ScriptCode
		return b
	}
	text := func(t string) []byte { return append(append([]byte("text\x00\x00\x00\x00"), t...), 0) }
	curve := func() []byte {
		const n = 1024
		b := []byte("curv\x00\x00\x00\x00")
		c := make([]byte, 4)
		be.PutUint32(c, n)
		b = append(b, c...)
		for i := 0; i < n; i++ {
			v := float64(i) / (n - 1)
			var l float64
			if v <= 0.04045 {
				l = v / 12.92
			} else {
				l = math.Pow((v+0.055)/1.055, 2.4)
			}
			b = binary.BigEndian.AppendUint16(b, uint16(math.Round(l*65535)))
		}
		return b
	}
	trc := curve()
	tags := []struct {
		sig  string
		data []byte
	}{
		{"desc", desc("sRGB IEC61966-2.1")},
		{"cprt", text("No copyright, use freely")},
		{"wtpt", xyz(0.9642, 1.0, 0.8249)},
		{"rXYZ", xyz(0.4360747, 0.2225045, 0.0139322)},
		{"gXYZ", xyz(0.3850649, 0.7168786, 0.0971045)},
		{"bXYZ", xyz(0.1430804, 0.0606169, 0.7141733)},
		{"rTRC", trc},
		{"gTRC", trc},
		{"bTRC", trc},
	}
	header := make([]byte, 128)
	table := make([]byte, 4+12*len(tags))
	be.PutUint32(table, uint32(len(tags)))
	var data []byte
	offset := 128 + len(table)
	shared := map[string]uint32{}
	for i, t := range tags {
		pos, ok := shared[string(t.data)]
		if !ok {
			for len(data)%4 != 0 {
				data = append(data, 0)
			}
			pos = uint32(offset + len(data))
			shared[string(t.data)] = pos
			data = append(data, t.data...)
		}
		e := table[4+12*i:]
		copy(e, t.sig)
		be.PutUint32(e[4:], pos)
		be.PutUint32(e[8:], uint32(len(t.data)))
	}
	out := append(append(header, table...), data...)
	for len(out)%4 != 0 {
		out = append(out, 0)
	}
	be.PutUint32(out[0:], uint32(len(out)))
	be.PutUint32(out[8:], 0x02100000)
	copy(out[12:], "mntr")
	copy(out[16:], "RGB ")
	copy(out[20:], "XYZ ")
	copy(out[36:], "acsp")
	copy(out[68:], s15(0.9642))
	copy(out[72:], s15(1.0))
	copy(out[76:], s15(0.8249))
	return out
}
