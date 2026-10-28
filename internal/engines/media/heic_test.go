package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

type bitWriter struct {
	b    []byte
	bits int
}

func (w *bitWriter) put(v, n int) {
	for i := n - 1; i >= 0; i-- {
		if w.bits%8 == 0 {
			w.b = append(w.b, 0)
		}
		if v>>i&1 == 1 {
			w.b[len(w.b)-1] |= 1 << (7 - w.bits%8)
		}
		w.bits++
	}
}

func (w *bitWriter) ue(v int) {
	v++
	n := 0
	for x := v; x > 1; x >>= 1 {
		n++
	}
	w.put(0, n)
	w.put(v, n+1)
}

// testSps is an HEVC SPS: Main profile, 4:2:0, 8-bit, w × h.
func testSps(w, h int) []byte {
	bw := &bitWriter{}
	bw.put(0, 4) // vps id
	bw.put(0, 3) // no sub-layers
	bw.put(1, 1) // temporal id nesting
	ptl := []byte{0x01, 0x60, 0, 0, 0, 0x90, 0, 0, 0, 0, 0, 0x5d}
	for _, b := range ptl {
		bw.put(int(b), 8)
	}
	bw.ue(0) // sps id
	bw.ue(1) // 4:2:0
	bw.ue(w)
	bw.ue(h)
	bw.put(0, 1) // no conformance window
	bw.ue(0)     // luma 8-bit
	bw.ue(0)     // chroma 8-bit
	bw.put(1, 1) // rest: stop bit
	return append([]byte{nalSps << 1, 1}, bw.b...)
}

func annexB(units ...[]byte) []byte {
	var b []byte
	for _, u := range units {
		b = append(b, 0, 0, 0, 1)
		b = append(b, u...)
	}
	return b
}

func slice(first bool) []byte {
	f := byte(0)
	if first {
		f = 0x80
	}
	return []byte{nalIdrNoLeading << 1, 1, f, 0xaa, 0xbb}
}

func TestHevcStreamIsSplitPerTile(t *testing.T) {
	vps := []byte{nalVps << 1, 1, 0x0c, 0x01}
	pps := []byte{nalPps << 1, 1, 0xc1, 0x62}
	sps := testSps(512, 512)
	sei := []byte{39 << 1, 1, 5, 0}
	data := annexB(vps, sps, pps, sei, slice(true), slice(false), vps, sps, pps, slice(true))
	s, err := ParseHevc(data)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, 2, len(s.Pictures))
	eq(t, 8, s.BitDepth)
	// Two NAL units with 4-byte lengths in the first picture.
	eq(t, uint32(5), binary.BigEndian.Uint32(s.Pictures[0]))
	eq(t, 18, len(s.Pictures[0]))
	c := s.Configuration
	eq(t, byte(1), c[0])
	if !bytes.Equal(c[1:13], []byte{0x01, 0x60, 0, 0, 0, 0x90, 0, 0, 0, 0, 0, 0x5d}) {
		t.Error("profile tier level")
	}
	eq(t, byte(0xfd), c[16]) // 4:2:0
	eq(t, byte(0xf8), c[17]) // 8-bit
	eq(t, byte(0x0f), c[21]) // one layer, nested, 4-byte lengths
	eq(t, byte(3), c[22])

	changed := annexB(vps, sps, pps, slice(true), vps, testSps(256, 256), pps, slice(true))
	if _, err := ParseHevc(changed); !errors.Is(err, ErrInvalidData) {
		t.Error("changed parameter sets accepted")
	}
	p := []byte{1 << 1, 1, 0x80, 0}
	if _, err := ParseHevc(annexB(vps, sps, pps, p)); !errors.Is(err, ErrInvalidData) {
		t.Error("P picture accepted")
	}
	if _, err := ParseHevc([]byte{1, 2, 3}); err == nil {
		t.Error("garbage accepted")
	}
	// Emulation prevention bytes are removed before the SPS is read.
	eq(t, "000001", strings.TrimSpace(string(hexOf(unescapeRbsp([]byte{0, 0, 3, 1})))))
}

func hexOf(b []byte) []byte {
	const d = "0123456789abcdef"
	var o []byte
	for _, c := range b {
		o = append(o, d[c>>4], d[c&15])
	}
	return o
}

func TestHeifGridWithExif(t *testing.T) {
	cols, rows, tw, th := HeicLayout(1000, 600)
	eq(t, [4]int{2, 2, 512, 512}, [4]int{cols, rows, tw, th})
	c1, r1, w1, h1 := HeicLayout(301, 199)
	eq(t, [4]int{1, 1, 302, 200}, [4]int{c1, r1, w1, h1})
	eq(t, 14, HeicCrf(90))
	eq(t, 30, HeicCrf(50))
	stream := &HevcStream{Configuration: []byte{1, 2, 3}, BitDepth: 8,
		Pictures: [][]byte{{0, 0, 0, 1, 0xa}, {0, 0, 0, 1, 0xb}, {0, 0, 0, 1, 0xc}, {0, 0, 0, 1, 0xd}}}
	exif := MakerNoteExif(MakerNote(liveID))
	file, err := WriteHeif(&HeifImage{Width: 1000, Height: 600, Stream: stream, TileWidth: 512, TileHeight: 512, Columns: 2, Rows: 2, Exif: append([]byte("Exif\x00\x00"), exif...)})
	if err != nil {
		t.Fatal(err)
	}
	if string(file[4:12]) != "ftypheic" {
		t.Error("brand")
	}
	// The Exif item is found again by the reader, and the tiles are where iloc says.
	got := HeifExif(bytes.NewReader(file))
	eq(t, strings.ToLower(liveID), ReadMakerNote(TiffMakerNote(got)))
	for _, b := range []byte{0xa, 0xb, 0xc, 0xd} {
		if !bytes.Contains(file, []byte{0, 0, 0, 1, b}) {
			t.Error("tile missing")
		}
	}
	if !bytes.Contains(file, []byte("grid")) || !bytes.Contains(file, []byte("dimg")) || !bytes.Contains(file, []byte("nclx")) {
		t.Error("grid boxes")
	}
	if _, err := WriteHeif(&HeifImage{Width: 10, Height: 10, Stream: stream, TileWidth: 10, TileHeight: 10, Columns: 1, Rows: 1}); err == nil {
		t.Error("tile count mismatch accepted")
	}
	single, _ := WriteHeif(&HeifImage{Width: 10, Height: 10, Stream: &HevcStream{Configuration: []byte{1}, Pictures: [][]byte{{1}}, BitDepth: 8},
		TileWidth: 10, TileHeight: 10, Columns: 1, Rows: 1, IccProfile: []byte("icc!")})
	if bytes.Contains(single, []byte("grid")) || !bytes.Contains(single, []byte("profi")) {
		t.Error("single picture")
	}
	if ExifPayload([]byte("no tiff header here")) != nil {
		t.Error("exif without tiff header")
	}
	a := HeicArgs("in.png", "out.hevc", 1000, 600, 90)
	eq(t, "pad=1024:1024:0:0,fillborders=right=24:bottom=424:mode=smear,scale=out_color_matrix=bt601:out_range=pc,format=yuv420p,untile=2x2", after(a, "-vf"))
	eq(t, "14", after(a, "-crf"))
	eq(t, "hevc", after(a, "-f"))
}
