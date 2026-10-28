package media

// Splits the raw HEVC stream FFmpeg writes (Annex B) into its parameter sets
// and one coded picture per tile, and builds the hvcC record HEIF stores.

import (
	"bytes"
	"encoding/binary"
)

// HevcStream is an all-intra HEVC stream split the way HEIF stores it.
type HevcStream struct {
	// Configuration is the hvcC payload.
	Configuration []byte
	// Pictures are the coded pictures as length-prefixed NAL units.
	Pictures [][]byte
	BitDepth int
}

const (
	nalIdrLeading   = 19
	nalIdrNoLeading = 20
	nalVps          = 32
	nalSps          = 33
	nalPps          = 34
)

// ParseHevc splits an Annex B stream. Every picture must be IDR, and the
// parameter sets repeated before each must not change (all tiles share one hvcC).
func ParseHevc(annexB []byte) (*HevcStream, error) {
	var vps, sps, pps []byte
	var pictures [][][]byte
	same := func(known, unit []byte) ([]byte, error) {
		if known == nil || bytes.Equal(known, unit) {
			return unit, nil
		}
		return nil, invalid("the HEVC stream changes its parameter sets between pictures")
	}
	for _, u := range nalUnits(annexB) {
		t := int(u[0]>>1) & 0x3f
		var err error
		switch {
		case t == nalVps:
			vps, err = same(vps, u)
		case t == nalSps:
			sps, err = same(sps, u)
		case t == nalPps:
			pps, err = same(pps, u)
		case t < nalIdrLeading || (t > nalIdrNoLeading && t < 32):
			return nil, invalid("the HEVC stream holds a picture that is not an IDR picture (NAL unit type %d)", t)
		case t < 32:
			// A slice with first_slice_segment_in_pic_flag starts the next picture.
			if u[2]&0x80 != 0 || len(pictures) == 0 {
				pictures = append(pictures, nil)
			}
			pictures[len(pictures)-1] = append(pictures[len(pictures)-1], u)
		}
		if err != nil {
			return nil, err
		}
	}
	if vps == nil || sps == nil || pps == nil || len(pictures) == 0 {
		return nil, invalid("the HEVC stream has no parameter sets or no picture")
	}
	cfg, depth, err := hvcConfiguration(vps, sps, pps)
	if err != nil {
		return nil, err
	}
	s := &HevcStream{Configuration: cfg, BitDepth: depth}
	for _, p := range pictures {
		var b []byte
		for _, u := range p {
			b = binary.BigEndian.AppendUint32(b, uint32(len(u)))
			b = append(b, u...)
		}
		s.Pictures = append(s.Pictures, b)
	}
	return s, nil
}

// nalUnits are the NAL units of an Annex B stream, without start codes.
func nalUnits(data []byte) [][]byte {
	var starts []int
	for i := 0; i+2 < len(data); i++ {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			starts = append(starts, i+3)
			i += 2
		}
	}
	var units [][]byte
	for k, s := range starts {
		end := len(data)
		if k+1 < len(starts) {
			end = starts[k+1] - 3
		}
		// A four-byte start code leaves a zero before the next unit; NAL units never end in zeros.
		for end > s && data[end-1] == 0 {
			end--
		}
		if end-s >= 3 {
			units = append(units, data[s:end])
		}
	}
	return units
}

func hvcConfiguration(vps, sps, pps []byte) ([]byte, int, error) {
	info, err := readSps(sps)
	if err != nil {
		return nil, 0, err
	}
	var o bytes.Buffer
	o.WriteByte(1)
	o.Write(info.ptl)
	o.Write([]byte{0xf0, 0x00}) // reserved + min_spatial_segmentation_idc 0
	o.WriteByte(0xfc)           // parallelismType unknown
	o.WriteByte(byte(0xfc | info.chroma))
	o.WriteByte(byte(0xf8 | (info.luma - 8)))
	o.WriteByte(byte(0xf8 | (info.chromaDepth - 8)))
	o.Write([]byte{0, 0}) // avgFrameRate unknown
	nested := byte(0)
	if info.nested {
		nested = 0x04
	}
	o.WriteByte(byte(info.layers<<3) | nested | 0x03) // 4-byte lengths
	o.WriteByte(3)
	for _, u := range []struct {
		t    int
		unit []byte
	}{{nalVps, vps}, {nalSps, sps}, {nalPps, pps}} {
		o.WriteByte(byte(0x80 | u.t)) // array_completeness
		o.Write(u16(1))
		o.Write(u16(len(u.unit)))
		o.Write(u.unit)
	}
	return o.Bytes(), info.luma, nil
}

type spsInfo struct {
	ptl                       []byte
	chroma, luma, chromaDepth int
	layers                    int
	nested                    bool
}

func readSps(sps []byte) (*spsInfo, error) {
	if len(sps) < 3 {
		return nil, invalid("the HEVC sequence parameter set is truncated")
	}
	b := &bitReader{data: unescapeRbsp(sps[2:])}
	b.read(4)
	sub := b.read(3)
	nested := b.read(1) == 1
	if sub != 0 && b.err == nil {
		return nil, invalid("HEVC streams with temporal sub-layers are not supported")
	}
	ptl := make([]byte, 12)
	for i := range ptl {
		ptl[i] = byte(b.read(8))
	}
	b.golomb()
	chroma := b.golomb()
	if chroma == 3 {
		b.read(1)
	}
	b.golomb()
	b.golomb()
	if b.read(1) == 1 {
		for i := 0; i < 4; i++ {
			b.golomb()
		}
	}
	luma := b.golomb() + 8
	cd := b.golomb() + 8
	if b.err != nil {
		return nil, b.err
	}
	return &spsInfo{ptl, chroma, luma, cd, sub + 1, nested}, nil
}

// unescapeRbsp removes emulation prevention bytes (00 00 03 → 00 00).
func unescapeRbsp(d []byte) []byte {
	out := make([]byte, 0, len(d))
	zeros := 0
	for _, c := range d {
		if zeros >= 2 && c == 3 {
			zeros = 0
			continue
		}
		out = append(out, c)
		if c == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return out
}

type bitReader struct {
	data []byte
	pos  int
	err  error
}

func (b *bitReader) read(n int) int {
	v := 0
	for i := 0; i < n; i++ {
		if b.pos>>3 >= len(b.data) {
			if b.err == nil {
				b.err = invalid("the HEVC sequence parameter set is truncated")
			}
			return 0
		}
		v = v<<1 | int(b.data[b.pos>>3]>>(7-b.pos&7))&1
		b.pos++
	}
	return v
}

func (b *bitReader) golomb() int {
	zeros := 0
	for b.read(1) == 0 {
		if b.err != nil {
			return 0
		}
		if zeros++; zeros > 30 {
			b.err = invalid("the HEVC sequence parameter set is malformed")
			return 0
		}
	}
	return (1 << zeros) - 1 + b.read(zeros)
}
