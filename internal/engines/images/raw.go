package images

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/jpeg"
	"slices"
	"sync"
	"testing/fstest"

	"github.com/ncruces/rethinkraw/pkg/dcraw"
	_ "github.com/ncruces/rethinkraw/pkg/dcraw/embed" // dcraw 9.28 compiled to WebAssembly
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// Camera RAW. There is no camera RAW decoder in pure Go, so dcraw (Dave
// Coffin's decoder, compiled to WebAssembly and run by wazero) develops the
// picture with the settings the original used through LibRaw: the camera's
// white balance, sRGB output, AHD interpolation, automatic brightness; it
// turns the picture upright itself. Formats dcraw doesn't know (Canon CR3,
// cameras newer than 2018) fall back to the largest JPEG preview the camera
// embedded in the file, which is usually full size or close to it.

var (
	dcrawOnce    sync.Once
	dcrawRuntime wazero.Runtime
	dcrawModule  wazero.CompiledModule
	dcrawErr     error
	// dcrawSlots limits concurrent developments (each holds a full picture in WebAssembly memory).
	dcrawSlots = make(chan struct{}, 2)
)

func dcrawCompile() {
	ctx := context.Background()
	dcrawRuntime = wazero.NewRuntime(ctx)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, dcrawRuntime); err != nil {
		dcrawErr = err
		return
	}
	dcrawModule, dcrawErr = dcrawRuntime.CompileModule(ctx, dcraw.Binary)
}

// developRaw runs dcraw on the file and returns the PPM it writes.
func developRaw(ctx context.Context, data []byte) ([]byte, error) {
	dcrawOnce.Do(dcrawCompile)
	if dcrawErr != nil {
		return nil, dcrawErr
	}
	dcrawSlots <- struct{}{}
	defer func() { <-dcrawSlots }()
	var out, errs bytes.Buffer
	fsys := fstest.MapFS{"input": &fstest.MapFile{Data: data, Mode: 0o400}}
	cfg := wazero.NewModuleConfig().
		WithArgs("dcraw", "-c", "-w", "-o", "1", "-q", "3", "input").
		WithStdout(&out).WithStderr(&errs).WithFS(fsys).WithName("")
	mod, err := dcrawRuntime.InstantiateModule(ctx, dcrawModule, cfg)
	if mod != nil {
		mod.Close(ctx)
	}
	if out.Len() == 0 {
		if err == nil {
			err = errors.New(string(bytes.TrimSpace(errs.Bytes())))
		}
		return nil, fmt.Errorf("dcraw: %w", err)
	}
	return out.Bytes(), nil
}

func decodeRaw(data []byte) (*Picture, error) {
	ppm, err := developRaw(context.Background(), data)
	if err == nil {
		if p, err2 := decodeNetpbm(ppm); err2 == nil {
			for _, f := range p.Frames {
				f.Gray = false
			}
			return p, nil
		}
	}
	if p := rawPreview(data); p != nil {
		return p, nil
	}
	if err == nil {
		err = errors.New("no picture found")
	}
	return nil, fmt.Errorf("this camera RAW file can't be read: %w", err)
}

// rawPreview is the largest JPEG embedded in a RAW file that decodes, turned
// as the RAW file (or the JPEG itself) says; nil when there is none.
func rawPreview(data []byte) *Picture {
	type candidate struct {
		start, end, pixels int
	}
	var cands []candidate
	for i := 0; i+4 <= len(data); {
		j := bytes.Index(data[i:], []byte{0xFF, 0xD8, 0xFF})
		if j < 0 {
			break
		}
		start := i + j
		end := jpegEnd(data, start)
		if end > start {
			if cfg, err := jpeg.DecodeConfig(bytes.NewReader(data[start:end])); err == nil && cfg.Width >= 64 && cfg.Height >= 64 {
				cands = append(cands, candidate{start, end, cfg.Width * cfg.Height})
			}
			i = end
		} else {
			i = start + 3
		}
	}
	slices.SortFunc(cands, func(a, b candidate) int { return b.pixels - a.pixels })
	orientation := rawOrientation(data)
	for _, c := range cands {
		p, err := decodeJPEG(data[c.start:c.end])
		if err != nil {
			continue
		}
		f := p.Frames[0]
		if orientation > 0 {
			f.Meta.Orientation = orientation
			f.Meta.Exif = nil // the preview's own Exif may disagree
		}
		return p
	}
	return nil
}

// jpegEnd finds where the JPEG starting at start ends (after its EOI), or -1.
func jpegEnd(data []byte, start int) int {
	p := start + 2
	for p+4 <= len(data) {
		if data[p] != 0xFF {
			return -1
		}
		m := data[p+1]
		switch {
		case m == 0xFF:
			p++
			continue
		case m == 0xD9:
			return p + 2
		case m >= 0xD0 && m <= 0xD7 || m == 0x01:
			p += 2
			continue
		}
		n := int(data[p+2])<<8 | int(data[p+3])
		p += 2 + n
		if m != 0xDA {
			continue
		}
		// entropy-coded data: up to a marker that is not a stuffed zero or a restart
		for p+1 < len(data) {
			if data[p] == 0xFF && data[p+1] != 0 && !(data[p+1] >= 0xD0 && data[p+1] <= 0xD7) {
				break
			}
			p++
		}
	}
	return -1
}

// rawOrientation reads the orientation of a TIFF-based RAW file's first directory.
func rawOrientation(data []byte) int {
	if len(data) < 8 {
		return 0
	}
	head := bytes.Clone(data[:min(len(data), 1<<20)])
	// ORF ("IIRO"/"IIRS") and RW2 ("IIU\0") are TIFF with another magic number.
	switch string(head[:2]) {
	case "II":
		head[2], head[3] = '*', 0
	case "MM":
		head[2], head[3] = 0, '*'
	default:
		return 0
	}
	return exifOrientation(head)
}
