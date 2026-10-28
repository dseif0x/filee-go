package heic

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// Splitting the raw HEVC stream FFmpeg writes (Annex B, NAL units behind start
// codes) into its parameter sets and one coded picture per tile, and building
// the decoder configuration record (hvcC) HEIF stores them in.

// Stream is an all-intra HEVC stream, split the way HEIF stores it.
type Stream struct {
	// Configuration is the payload of the hvcC box: profile, chroma format, bit depth and VPS/SPS/PPS.
	Configuration []byte
	// Pictures are each coded picture as length-prefixed (4 bytes) NAL units, ready for mdat.
	Pictures [][]byte
	// BitDepth is the luma bit depth (8 for Main, 10 for Main 10).
	BitDepth int
}

const (
	nalIdrWithLeading = 19
	nalIdrNoLeading   = 20
	nalVPS            = 32
	nalSPS            = 33
	nalPPS            = 34
)

// ParseStream splits an Annex B stream. Every picture must be an IDR picture,
// and the parameter sets x265 repeats before each one must not change.
func ParseStream(annexB []byte) (*Stream, error) {
	var vps, sps, pps []byte
	var pictures [][][]byte
	same := func(known, unit []byte) ([]byte, error) {
		if known == nil || bytes.Equal(known, unit) {
			return unit, nil
		}
		return nil, errors.New("the HEVC stream changes its parameter sets between pictures")
	}
	for _, unit := range Units(annexB) {
		typ := int(unit[0]>>1) & 0x3F
		var err error
		switch {
		case typ == nalVPS:
			vps, err = same(vps, unit)
		case typ == nalSPS:
			sps, err = same(sps, unit)
		case typ == nalPPS:
			pps, err = same(pps, unit)
		case typ < nalIdrWithLeading || (typ > nalIdrNoLeading && typ < 32):
			return nil, fmt.Errorf("the HEVC stream holds a picture that is not an IDR picture (NAL unit type %d)", typ)
		case typ < 32:
			// a slice with first_slice_segment_in_pic_flag starts the next picture
			if unit[2]&0x80 != 0 || len(pictures) == 0 {
				pictures = append(pictures, nil)
			}
			pictures[len(pictures)-1] = append(pictures[len(pictures)-1], unit)
		}
		if err != nil {
			return nil, err
		}
	}
	if vps == nil || sps == nil || pps == nil || len(pictures) == 0 {
		return nil, errors.New("the HEVC stream has no parameter sets or no picture")
	}
	config, depth, err := configuration(vps, sps, pps)
	if err != nil {
		return nil, err
	}
	s := &Stream{Configuration: config, BitDepth: depth}
	for _, units := range pictures {
		var b []byte
		for _, u := range units {
			b = binary.BigEndian.AppendUint32(b, uint32(len(u)))
			b = append(b, u...)
		}
		s.Pictures = append(s.Pictures, b)
	}
	return s, nil
}

// Units are the NAL units of an Annex B stream, without their start codes.
func Units(data []byte) [][]byte {
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
		// a four-byte start code leaves one zero byte before the next unit; NAL units never end in zero bytes
		for end > s && data[end-1] == 0 {
			end--
		}
		if end-s >= 3 {
			units = append(units, data[s:end])
		}
	}
	return units
}

// configuration is the HEVCDecoderConfigurationRecord (ISO/IEC 14496-15, 8.3.3) with one VPS, SPS and PPS.
func configuration(vps, sps, pps []byte) ([]byte, int, error) {
	info, err := readSPS(sps)
	if err != nil {
		return nil, 0, err
	}
	var b bytes.Buffer
	b.WriteByte(1)
	b.Write(info.ptl)
	b.Write([]byte{0xF0, 0x00}) // min_spatial_segmentation_idc 0
	b.WriteByte(0xFC)           // parallelismType unknown
	b.WriteByte(byte(0xFC | info.chroma))
	b.WriteByte(byte(0xF8 | (info.luma - 8)))
	b.WriteByte(byte(0xF8 | (info.chromaDepth - 8)))
	b.Write([]byte{0, 0}) // avgFrameRate unknown
	nested := byte(0)
	if info.nested {
		nested = 0x04
	}
	b.WriteByte(byte(info.layers<<3) | nested | 0x03)
	b.WriteByte(3)
	for _, u := range []struct {
		typ  int
		unit []byte
	}{{nalVPS, vps}, {nalSPS, sps}, {nalPPS, pps}} {
		b.WriteByte(byte(0x80 | u.typ)) // array_completeness
		b.Write([]byte{0, 1})
		b.Write(binary.BigEndian.AppendUint16(nil, uint16(len(u.unit))))
		b.Write(u.unit)
	}
	return b.Bytes(), info.luma, nil
}

type spsInfo struct {
	ptl                       []byte
	chroma, luma, chromaDepth int
	layers                    int
	nested                    bool
}

// readSPS reads the fields of a sequence parameter set (H.265, 7.3.2.2) that hvcC repeats.
func readSPS(sps []byte) (*spsInfo, error) {
	if len(sps) < 3 {
		return nil, errors.New("the HEVC sequence parameter set is truncated")
	}
	r := &bitReader{data: unescape(sps[2:])}
	r.read(4) // sps_video_parameter_set_id
	sub := r.read(3)
	nested := r.read(1) == 1
	if sub != 0 {
		return nil, errors.New("HEVC streams with temporal sub-layers are not supported")
	}
	ptl := make([]byte, 12)
	for i := range ptl {
		ptl[i] = byte(r.read(8))
	}
	r.golomb() // sps_seq_parameter_set_id
	chroma := r.golomb()
	if chroma == 3 {
		r.read(1)
	}
	r.golomb()
	r.golomb()
	if r.read(1) == 1 { // conformance window
		for i := 0; i < 4; i++ {
			r.golomb()
		}
	}
	luma := r.golomb() + 8
	chromaDepth := r.golomb() + 8
	if r.err != nil {
		return nil, r.err
	}
	return &spsInfo{ptl, chroma, luma, chromaDepth, sub + 1, nested}, nil
}

// unescape removes emulation prevention bytes (00 00 03 → 00 00).
func unescape(data []byte) []byte {
	out := make([]byte, 0, len(data))
	zeros := 0
	for _, b := range data {
		if zeros >= 2 && b == 3 {
			zeros = 0
			continue
		}
		out = append(out, b)
		if b == 0 {
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

func (r *bitReader) read(n int) int {
	v := 0
	for i := 0; i < n; i++ {
		if r.pos>>3 >= len(r.data) {
			r.err = errors.New("the HEVC sequence parameter set is truncated")
			return 0
		}
		v = v<<1 | int(r.data[r.pos>>3]>>(7-r.pos&7))&1
		r.pos++
	}
	return v
}

func (r *bitReader) golomb() int {
	zeros := 0
	for r.read(1) == 0 {
		if r.err != nil {
			return 0
		}
		if zeros++; zeros > 30 {
			r.err = errors.New("the HEVC sequence parameter set is malformed")
			return 0
		}
	}
	return 1<<zeros - 1 + r.read(zeros)
}
