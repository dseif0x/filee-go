package bundle

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"fmt"
)

// Universal merges thin Mach-O executables (darwin/arm64 and darwin/amd64)
// into one universal ("fat") file, as lipo -create does. Each slice keeps its
// own code signature (the Go linker signs arm64 ad hoc), aligned to 16 KiB.
func Universal(thin ...[]byte) ([]byte, error) {
	const align = 14 // 2^14, the arm64 page size; lipo uses it for both
	type arch struct {
		cpu, sub uint32
		data     []byte
	}
	var archs []arch
	for i, b := range thin {
		f, err := macho.NewFile(bytes.NewReader(b))
		if err != nil {
			return nil, fmt.Errorf("slice %d: %w", i, err)
		}
		archs = append(archs, arch{uint32(f.Cpu), f.SubCpu, b})
		_ = f.Close()
	}
	var out bytes.Buffer
	be := binary.BigEndian
	_ = binary.Write(&out, be, uint32(macho.MagicFat))
	_ = binary.Write(&out, be, uint32(len(archs)))
	offset := uint32(8 + 20*len(archs))
	offsets := make([]uint32, len(archs))
	for i, a := range archs {
		offset = (offset + (1<<align - 1)) &^ (1<<align - 1)
		offsets[i] = offset
		for _, v := range []uint32{a.cpu, a.sub, offset, uint32(len(a.data)), align} {
			_ = binary.Write(&out, be, v)
		}
		offset += uint32(len(a.data))
	}
	for i, a := range archs {
		out.Write(make([]byte, int(offsets[i])-out.Len()))
		out.Write(a.data)
	}
	return out.Bytes(), nil
}
