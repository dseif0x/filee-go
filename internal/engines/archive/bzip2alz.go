package archive

import (
	"context"
	"errors"
	"io"
)

// bzip2 decoding of ALZip's variant: ALZ stores bzip2 blocks without the
// "BZh" stream header, marks each block with "DLZ\x01" (instead of the 48-bit
// block magic, the block CRC and the "randomised" bit) and ends with
// "DLZ\x02". The block format itself (Huffman coded MTF/RLE2 symbols,
// Burrows-Wheeler transform, RLE1) is plain bzip2, so compress/bzip2, which
// insists on the standard framing, can't read it.

const (
	alzBlockMarker = 0x444C5A01 // "DLZ\x01"
	alzEndMarker   = 0x444C5A02 // "DLZ\x02"
	bzMaxCodeLen   = 20
	bzGroupSize    = 50
	bzMaxSelectors = 18002
)

type bzDecoder struct {
	in      io.Reader
	out     io.Writer
	max     int64
	buf     [64 << 10]byte
	pos, n  int
	bits    uint64
	nbits   int
	written int64
	tt      []int32
}

func bzErr(s string) error { return errors.New(s) }

// decompressAlzBzip2 decodes ALZ-framed bzip2 from in to out, failing past maxOutput bytes.
func decompressAlzBzip2(ctx context.Context, in io.Reader, out io.Writer, maxOutput int64) (int64, error) {
	d := &bzDecoder{in: in, out: out, max: maxOutput}
	for {
		marker, ok := d.tryBits(32)
		if !ok {
			return d.written, nil
		}
		if err := ctx.Err(); err != nil {
			return d.written, err
		}
		if marker == alzEndMarker {
			return d.written, nil
		}
		if marker != alzBlockMarker {
			return d.written, bzErr("bzip2 block marker missing")
		}
		// ALZip always writes 900k blocks.
		if err := d.block(9); err != nil {
			return d.written, err
		}
	}
}

var errBzEnd = errors.New("bzip2 data ends unexpectedly")

func (d *bzDecoder) tryBits(count int) (uint32, bool) {
	for d.nbits < count {
		if d.pos == d.n {
			n, _ := io.ReadFull(d.in, d.buf[:])
			if n == 0 {
				return 0, false
			}
			d.n, d.pos = n, 0
		}
		d.bits = d.bits<<8 | uint64(d.buf[d.pos])
		d.pos++
		d.nbits += 8
	}
	d.nbits -= count
	return uint32(d.bits>>d.nbits) & uint32(1<<count-1), true
}

// readBits panics with errBzEnd at the end of the input; block recovers it.
func (d *bzDecoder) readBits(count int) uint32 {
	v, ok := d.tryBits(count)
	if !ok {
		panic(errBzEnd)
	}
	return v
}

type bzPanic struct{ err error }

func (d *bzDecoder) block(level int) (err error) {
	defer func() {
		if r := recover(); r != nil {
			switch e := r.(type) {
			case error:
				if e == errBzEnd {
					err = e
					return
				}
				panic(r)
			case bzPanic:
				err = e.err
			default:
				panic(r)
			}
		}
	}()
	fail := func(s string) { panic(bzPanic{bzErr(s)}) }

	blockMax := level * 100_000
	if len(d.tt) < blockMax {
		d.tt = make([]int32, blockMax)
	}
	tt := d.tt
	origPtr := int(d.readBits(24))

	var seqToUnseq [256]byte
	inUse := 0
	inUse16 := d.readBits(16)
	for i := 0; i < 16; i++ {
		if inUse16&(0x8000>>i) == 0 {
			continue
		}
		bits := d.readBits(16)
		for j := 0; j < 16; j++ {
			if bits&(0x8000>>j) != 0 {
				seqToUnseq[inUse] = byte(i*16 + j)
				inUse++
			}
		}
	}
	if inUse == 0 {
		fail("bzip2 block uses no symbols")
	}
	alphaSize := inUse + 2
	groups := int(d.readBits(3))
	selectorCount := int(d.readBits(15))
	if groups < 2 || groups > 6 || selectorCount < 1 {
		fail("invalid bzip2 block header")
	}
	selectors := make([]byte, min(selectorCount, bzMaxSelectors))
	var mtfGroups [6]byte
	for i := 0; i < groups; i++ {
		mtfGroups[i] = byte(i)
	}
	for i := 0; i < selectorCount; i++ {
		j := 0
		for d.readBits(1) == 1 {
			j++
			if j >= groups {
				fail("invalid bzip2 selector")
			}
		}
		v := mtfGroups[j]
		for ; j > 0; j-- {
			mtfGroups[j] = mtfGroups[j-1]
		}
		mtfGroups[0] = v
		if i < len(selectors) {
			selectors[i] = v // bzip2 1.0.8 reads but ignores selectors beyond the limit
		}
	}

	limit := make([][bzMaxCodeLen + 2]int, groups)
	base := make([][bzMaxCodeLen + 2]int, groups)
	perm := make([][258]int, groups)
	minLens := make([]int, groups)
	lengths := make([]int, alphaSize)
	for t := 0; t < groups; t++ {
		cur := int(d.readBits(5))
		for i := 0; i < alphaSize; i++ {
			for {
				if cur < 1 || cur > bzMaxCodeLen {
					fail("invalid bzip2 code length")
				}
				if d.readBits(1) == 0 {
					break
				}
				if d.readBits(1) == 0 {
					cur++
				} else {
					cur--
				}
			}
			lengths[i] = cur
		}
		minLens[t] = bzDecodeTable(lengths, &limit[t], &base[t], &perm[t])
	}

	var mtf [256]byte
	for i := range mtf {
		mtf[i] = byte(i)
	}
	var counts [256]int
	eob := inUse + 1
	length := 0
	groupIndex, groupLeft, table := -1, 0, 0
	next := func() int {
		if groupLeft == 0 {
			groupIndex++
			if groupIndex >= len(selectors) {
				fail("bzip2 selectors exhausted")
			}
			table = int(selectors[groupIndex])
			groupLeft = bzGroupSize
		}
		groupLeft--
		n := minLens[table]
		code := int(d.readBits(n))
		for n <= bzMaxCodeLen && code > limit[table][n] {
			n++
			code = code<<1 | int(d.readBits(1))
		}
		if n > bzMaxCodeLen {
			fail("invalid bzip2 Huffman code")
		}
		slot := code - base[table][n]
		if slot < 0 || slot >= 258 {
			fail("invalid bzip2 Huffman code")
		}
		return perm[table][slot]
	}

	sym := next()
	for sym != eob {
		if sym <= 1 {
			// RUNA / RUNB: a bijective base-2 run length of the symbol at the MTF front.
			run, weight := 0, 1
			for sym <= 1 {
				if weight > blockMax {
					fail("bzip2 run too long")
				}
				run += (sym + 1) * weight
				weight <<= 1
				sym = next()
			}
			v := seqToUnseq[mtf[0]]
			if length+run > blockMax {
				fail("bzip2 block too long")
			}
			counts[v] += run
			for k := 0; k < run; k++ {
				tt[length+k] = int32(v)
			}
			length += run
			continue
		}
		if length >= blockMax {
			fail("bzip2 block too long")
		}
		index := sym - 1
		if index >= inUse {
			fail("invalid bzip2 symbol")
		}
		front := mtf[index]
		copy(mtf[1:index+1], mtf[:index])
		mtf[0] = front
		b := seqToUnseq[front]
		counts[b]++
		tt[length] = int32(b)
		length++
		sym = next()
	}
	if origPtr < 0 || origPtr >= max(length, 1) {
		fail("invalid bzip2 origin pointer")
	}

	// Inverse Burrows-Wheeler transform: link every position to its successor.
	var starts [256]int
	sum := 0
	for i := range starts {
		starts[i] = sum
		sum += counts[i]
	}
	for i := 0; i < length; i++ {
		b := tt[i] & 0xFF
		tt[starts[b]] |= int32(i << 8)
		starts[b]++
	}

	// Walk the chain and undo the initial run-length coding (4 equal bytes + a count byte).
	out := make([]byte, 0, 64<<10)
	flush := func() {
		if len(out) == 0 {
			return
		}
		d.written += int64(len(out))
		if d.written > d.max {
			panic(bzPanic{errTooMuchData})
		}
		if _, err := d.out.Write(out); err != nil {
			panic(bzPanic{err})
		}
		out = out[:0]
	}
	emit := func(v byte) {
		out = append(out, v)
		if len(out) == cap(out) {
			flush()
		}
	}
	position := 0
	if length > 0 {
		position = int(tt[origPtr] >> 8)
	}
	previous, repeat := -1, 0
	for i := 0; i < length; i++ {
		e := tt[position]
		b := int(e & 0xFF)
		position = int(e >> 8)
		if repeat == 4 {
			for k := 0; k < b; k++ {
				emit(byte(previous))
			}
			repeat, previous = 0, -1
			continue
		}
		if b == previous {
			repeat++
		} else {
			repeat = 1
		}
		previous = b
		emit(byte(b))
	}
	flush()
	return nil
}

// bzDecodeTable builds canonical Huffman decode tables as in the reference
// bzip2 (limit / base / perm per code length) and returns the shortest length.
func bzDecodeTable(lengths []int, limit, base *[bzMaxCodeLen + 2]int, perm *[258]int) int {
	minLen, maxLen := bzMaxCodeLen, 0
	for _, l := range lengths {
		minLen = min(minLen, l)
		maxLen = max(maxLen, l)
	}
	p := 0
	for l := minLen; l <= maxLen; l++ {
		for s, sl := range lengths {
			if sl == l {
				perm[p] = s
				p++
			}
		}
	}
	var count [bzMaxCodeLen + 2]int
	for _, l := range lengths {
		count[l+1]++
	}
	for i := 1; i < len(count); i++ {
		count[i] += count[i-1]
	}
	for i := range limit {
		limit[i] = -1 // lengths outside min..max never match, so decoding goes on to longer codes
		base[i] = count[i]
	}
	code := 0
	for l := minLen; l <= maxLen; l++ {
		code += count[l+1] - count[l]
		limit[l] = code - 1
		code <<= 1
	}
	for l := minLen + 1; l <= maxLen; l++ {
		base[l] = (limit[l-1]+1)<<1 - count[l]
	}
	return minLen
}
