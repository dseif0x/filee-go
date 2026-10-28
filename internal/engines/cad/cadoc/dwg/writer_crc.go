package dwg

import "hash/crc32"

// writerCRC8Table is CRC.CrcTable: ACadSharp's "CRC8" is the reflected
// CRC-16 with polynomial 0xA001 (seeded with 0xC0C1 in DWG files).
var writerCRC8Table = func() (t [256]uint16) {
	for i := range t {
		c := uint16(i)
		for j := 0; j < 8; j++ {
			if c&1 != 0 {
				c = c>>1 ^ 0xA001
			} else {
				c >>= 1
			}
		}
		t[i] = c
	}
	return t
}()

// wCRC8 is CRC8StreamHandler.GetCRCValue.
func wCRC8(seed uint16, p []byte) uint16 {
	for _, b := range p {
		seed = seed>>8 ^ writerCRC8Table[b^byte(seed)]
	}
	return seed
}

// wCRC32 is CRC32StreamHandler's seed after writing p: the IEEE CRC-32.
func wCRC32(seed uint32, p []byte) uint32 { return crc32.Update(seed, crc32.IEEETable, p) }

// writerMagicSequence is DwgCheckSumCalculator.MagicSequence: 256 bytes of
// the MSVC rand() sequence seeded with 1.
var writerMagicSequence = func() (m [256]byte) {
	seed := int32(1)
	for i := range m {
		seed = seed*0x343FD + 0x269EC3
		m[i] = byte(seed >> 0x10)
	}
	return m
}()

// wCompressionPadding is DwgCheckSumCalculator.CompressionCalculator: the
// padding that aligns a page to 32 bytes.
func wCompressionPadding(length int) int { return 0x1F - (length+0x20-1)%0x20 }

// wChecksum is DwgCheckSumCalculator.Calculate (Adler-32 like page checksum).
func wChecksum(seed uint32, p []byte) uint32 {
	sum1 := seed & 0xFFFF
	sum2 := seed >> 16
	for len(p) > 0 {
		n := min(0x15B0, len(p))
		for _, b := range p[:n] {
			sum1 += uint32(b)
			sum2 += sum1
		}
		p = p[n:]
		sum1 %= 0xFFF1
		sum2 %= 0xFFF1
	}
	return sum2<<16 | sum1&0xFFFF
}
