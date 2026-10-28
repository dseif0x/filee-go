package dwg

// wLZ77AC18 is DwgLZ77AC18Compressor, the LZ77 variant of R2004+ section
// pages. The output must match ACadSharp byte for byte, so the hash, the
// match search and the opcode layout are kept as they are.
type wLZ77AC18 struct {
	source        []byte
	dest          *wMemStream
	block         [0x8000]int
	initialOffset int
	currPosition  int
	currOffset    int
	totalOffset   int
}

// wCompressAC18 compresses source[offset:offset+totalSize] into dest.
func wCompressAC18(source []byte, offset, totalSize int, dest *wMemStream) {
	c := &wLZ77AC18{}
	c.compress(source, offset, totalSize, dest)
}

func (c *wLZ77AC18) compress(source []byte, offset, totalSize int, dest *wMemStream) {
	for i := range c.block {
		c.block[i] = -1
	}
	c.source = source
	c.dest = dest
	c.initialOffset = offset
	c.totalOffset = offset + totalSize
	c.currOffset = offset
	c.currPosition = offset + 4

	compressionOffset, matchPos := 0, 0
	currOffset, lastMatchPos := 0, 0
	for c.currPosition < c.totalOffset-0x13 {
		if !c.compressChunk(&currOffset, &lastMatchPos) {
			c.currPosition++
			continue
		}
		mask := c.currPosition - c.currOffset
		if compressionOffset != 0 {
			c.applyMask(matchPos, compressionOffset, mask)
		}
		c.writeLiteralLength(mask)
		c.currPosition += currOffset
		c.currOffset = c.currPosition
		compressionOffset = currOffset
		matchPos = lastMatchPos
	}
	literalLength := c.totalOffset - c.currOffset
	if compressionOffset != 0 {
		c.applyMask(matchPos, compressionOffset, literalLength)
	}
	c.writeLiteralLength(literalLength)
	// 0x11 terminates the stream.
	dest.writeByte(0x11)
	dest.writeByte(0)
	dest.writeByte(0)
}

func (c *wLZ77AC18) writeLen(n int) {
	if n <= 0 {
		panic("dwg: lz77 length")
	}
	for n > 0xFF {
		n -= 0xFF
		c.dest.writeByte(0)
	}
	c.dest.writeByte(byte(n))
}

func (c *wLZ77AC18) writeOpCode(opCode, compressionOffset, value int) {
	if compressionOffset <= 0 || value <= 0 {
		panic("dwg: lz77 opcode")
	}
	if compressionOffset <= value {
		opCode |= compressionOffset - 2
		c.dest.writeByte(byte(opCode))
		return
	}
	c.dest.writeByte(byte(opCode))
	c.writeLen(compressionOffset - value)
}

func (c *wLZ77AC18) writeLiteralLength(length int) {
	if length <= 0 {
		return
	}
	if length > 3 {
		c.writeOpCode(0, length-1, 0x11)
	}
	c.dest.write(c.source[c.currOffset : c.currOffset+length])
}

func (c *wLZ77AC18) applyMask(matchPosition, compressionOffset, mask int) {
	var curr, next int
	if compressionOffset >= 0x0F || matchPosition > 0x400 {
		if matchPosition <= 0x4000 {
			matchPosition--
			c.writeOpCode(0x20, compressionOffset, 0x21)
		} else {
			matchPosition -= 0x4000
			c.writeOpCode(0x10|((matchPosition>>11)&8), compressionOffset, 0x09)
		}
		curr = (matchPosition & 0xFF) << 2
		next = matchPosition >> 6
	} else {
		matchPosition--
		curr = (compressionOffset+1)<<4 | (matchPosition&0b11)<<2
		next = matchPosition >> 2
	}
	if mask < 4 {
		curr |= mask
	}
	c.dest.writeByte(byte(curr))
	c.dest.writeByte(byte(next))
}

func (c *wLZ77AC18) compressChunk(offset, matchPos *int) bool {
	*offset = 0
	src := c.source
	p := c.currPosition
	v1 := int(src[p+3]) << 6
	v2 := v1 ^ int(src[p+2])
	v3 := v2<<5 ^ int(src[p+1])
	v4 := v3<<5 ^ int(src[p])
	valueIndex := (v4 + v4>>5) & 0x7FFF

	value := c.block[valueIndex]
	*matchPos = p - value
	if value >= c.initialOffset && *matchPos <= 0xBFFF {
		if *matchPos > 0x400 && src[p+3] != src[value+3] {
			valueIndex = (valueIndex & 0x7FF) ^ 0b100000000011111
			value = c.block[valueIndex]
			*matchPos = p - value
			if value < c.initialOffset || *matchPos > 0xBFFF || (*matchPos > 0x400 && src[p+3] != src[value+3]) {
				c.block[valueIndex] = p
				return false
			}
		}
		if src[p] == src[value] && src[p+1] == src[value+1] && src[p+2] == src[value+2] {
			*offset = 3
			index := value + 3
			cur := p + 3
			for cur < c.totalOffset {
				eq := src[index] == src[cur]
				index++
				cur++
				if !eq {
					break
				}
				*offset++
			}
		}
	}
	c.block[valueIndex] = p
	return *offset >= 3
}
