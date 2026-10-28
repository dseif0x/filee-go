package ofd

import (
	"image"
	"os"
)

func readFile(p string) ([]byte, error) { return os.ReadFile(p) }

func decodeFile(p string) (image.Image, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}
