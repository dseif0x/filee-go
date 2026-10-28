package images

import (
	"bytes"
	"encoding/binary"
	"testing"
)

var ddsColours = bytes.Repeat([]byte{255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255, 255, 255, 0, 128}, 4)

// ddsFile is an uncompressed 4 × 4 DDS without a DX10 header.
func ddsFile(flags, red, green, blue, alpha uint32, pixels []byte) []byte {
	data := make([]byte, 128+len(pixels))
	copy(data, "DDS ")
	for _, kv := range [][2]uint32{{4, 124}, {8, 0x100F}, {12, 4}, {16, 4}, {20, 16}, {76, 32}, {80, flags}, {88, 32},
		{92, red}, {96, green}, {100, blue}, {104, alpha}, {108, 0x1000}} {
		binary.LittleEndian.PutUint32(data[kv[0]:], kv[1])
	}
	copy(data[128:], pixels)
	return data
}

func TestDDSUncompressedLayouts(t *testing.T) {
	for _, alpha := range []bool{true, false} {
		flags := uint32(0x40)
		if alpha {
			flags |= 1
		}
		pic, err := DecodeBytes(ddsFile(flags, 0xFF, 0xFF00, 0xFF0000, 0xFF000000, ddsColours), "dds")
		if err != nil {
			t.Fatal(err)
		}
		img := pic.Frames[0].Img
		for i := 0; i < 16; i++ {
			want := ddsColours[i*4 : i*4+4]
			got := img.Pix[i*4 : i*4+4]
			if !bytes.Equal(got[:3], want[:3]) || (alpha && got[3] != want[3]) || (!alpha && got[3] != 255) {
				t.Fatalf("alpha=%v pixel %d: %v, want %v", alpha, i, got, want)
			}
		}
	}
	var bgra []byte
	for i := 0; i < len(ddsColours); i += 4 {
		bgra = append(bgra, ddsColours[i+2], ddsColours[i+1], ddsColours[i], ddsColours[i+3])
	}
	pic, err := DecodeBytes(ddsFile(0x41, 0xFF0000, 0xFF00, 0xFF, 0xFF000000, bgra), "dds")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pic.Frames[0].Img.Pix, ddsColours) {
		t.Errorf("BGRA pixels %v", pic.Frames[0].Img.Pix[:16])
	}
}
