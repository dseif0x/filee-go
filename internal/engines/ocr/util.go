package ocr

import (
	"crypto/rand"
	"encoding/hex"
	"os"
)

func randomName() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func moveFile(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	if err := copyFile(from, to); err != nil {
		return err
	}
	return os.Remove(from)
}
