package model3d

import (
	"context"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// Cut, flipped and random bytes in every 3D format are read or refused: never a panic.
func TestDamagedModelsAreReadOrRefusedNeverACrash(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	for _, format := range Formats {
		source := save(t, cubeModel(), t.TempDir(), "cube", format)
		whole, _ := os.ReadFile(source)
		path := filepath.Join(dir, "d."+format)
		for n := 0; n < len(whole); n += max(1, len(whole)/211) {
			os.WriteFile(path, whole[:n], 0o644)
			Read(ctx, path, format, nil)
		}
		step := max(1, len(whole)/499)
		for i := 0; i < len(whole); i += step {
			c := append([]byte(nil), whole...)
			c[i] ^= 0xFF
			os.WriteFile(path, c, 0o644)
			Read(ctx, path, format, nil)
		}
	}
	r := rand.New(rand.NewSource(2))
	for _, format := range InputFormats {
		for i := 0; i < 100; i++ {
			b := make([]byte, r.Intn(400))
			r.Read(b)
			path := filepath.Join(dir, "r."+format)
			os.WriteFile(path, b, 0o644)
			Read(ctx, path, format, nil)
		}
	}
}
