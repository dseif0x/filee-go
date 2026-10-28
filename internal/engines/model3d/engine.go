// Package model3d converts 3D models in Go only: GLB, glTF, STL, OBJ, 3MF and
// PLY into each other through one in-memory model (Model), FBX (read only)
// into any of them, and an isometric preview picture from a small software
// rasteriser.
package model3d

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"
	"unicode"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/images"
	"github.com/dseif0x/filee-go/internal/presets"
)

// Formats are the 3D formats, each readable and writable.
var Formats = []string{"glb", "gltf", "stl", "obj", "3mf", "ply"}

// InputFormats are every 3D format that can be read: Formats and FBX.
var InputFormats = append(slices.Clone(Formats), "fbx")

// PreviewSize is the long edge of the preview picture before the preset's resize.
const PreviewSize = 1024

// Converter is the 3D model engine.
type Converter struct{}

func New() *Converter { return &Converter{} }

func (*Converter) ID() string   { return "model3d" }
func (*Converter) Name() string { return "3D models (built-in)" }

// MaxParallelism: a large mesh takes a few hundred MB while it is converted, so only a few run at once.
func (*Converter) MaxParallelism() int { return max(1, min(runtime.NumCPU()/2, 4)) }

// Transforms: the rendered picture goes through the image options, rotation and flip included.
func (*Converter) Transforms(from, to string) bool { return slices.Contains(PictureFormats, to) }

func (*Converter) Edges() []convert.Edge {
	var e []convert.Edge
	for _, from := range InputFormats {
		for _, to := range Formats {
			if from != to {
				e = append(e, convert.E(from, to))
			}
		}
	}
	for _, from := range InputFormats {
		for _, to := range PictureFormats {
			e = append(e, images.Edge(from, to))
		}
	}
	return e
}

func (*Converter) Status() convert.Status {
	return convert.Available("GLB, glTF, STL, OBJ, 3MF, PLY ↔ each other; FBX → each of them", "built-in · qmuntal/gltf")
}

func (*Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	reading := func(f float64) { progress.Report(0.6 * f) }
	model, err := Read(ctx, step.InputPath, step.From, reading)
	if err != nil {
		return nil, err
	}
	progress.Report(0.6)
	var image *presets.ImageOptions
	if step.Preset != nil {
		image = &step.Preset.Image
	}
	var outputs []string
	if err := Write(ctx, model, step.To, step.Output, image, &outputs); err != nil {
		for _, o := range outputs {
			os.Remove(o)
		}
		return nil, err
	}
	progress.Report(1)
	return outputs, nil
}

// Read reads a model of format and checks it.
func Read(ctx context.Context, path, format string, progress func(float64)) (*Model, error) {
	var m *Model
	var err error
	switch format {
	case "stl":
		m, err = readStl(ctx, path, progress)
	case "obj":
		m, err = readObj(ctx, path, progress)
	case "ply":
		m, err = readPly(ctx, path, progress)
	case "3mf":
		m, err = readThreeMf(ctx, path, progress)
	case "glb", "gltf":
		m, err = readGltf(ctx, path, progress)
	case "fbx":
		m, err = readFbx(ctx, path, progress)
	default:
		return nil, fmt.Errorf("Unknown 3D format %q.", format)
	}
	if err != nil {
		return nil, err
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return m, nil
}

// Write writes m as format; every file written is added to outputs.
func Write(ctx context.Context, m *Model, format string, out convert.OutputAllocator, image *presets.ImageOptions, outputs *[]string) error {
	if format == "obj" {
		return writeObjSet(ctx, m, out, outputs)
	}
	if !slices.Contains(Formats, format) {
		img, err := renderModel(ctx, m, PreviewSize)
		if err != nil {
			return err
		}
		written, err := writePicture(img, format, out, image)
		*outputs = append(*outputs, written...)
		return err
	}
	path := out.Allocate(format, "")
	if path == "" {
		return nil
	}
	*outputs = append(*outputs, path)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	switch format {
	case "stl":
		err = writeStl(ctx, m, f)
	case "ply":
		err = writePly(ctx, m, f)
	case "3mf":
		err = writeThreeMf(ctx, m, f)
	case "glb", "gltf":
		err = writeGltf(ctx, m, f, format == "glb")
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// writeObjSet writes the .obj, the .mtl next to it (when the model has materials) and the textures next to that.
func writeObjSet(ctx context.Context, m *Model, out convert.OutputAllocator, outputs *[]string) error {
	obj := out.Allocate("obj", "")
	if obj == "" {
		return nil
	}
	*outputs = append(*outputs, obj)
	mtl := ""
	textures := map[*Texture]string{}
	if len(m.Materials) > 0 {
		mtl = out.Allocate("mtl", "")
		if mtl != "" {
			*outputs = append(*outputs, mtl)
			names := map[string]bool{}
			var seen []*Texture
			for _, mat := range m.Materials {
				t := mat.BaseColorTexture
				if t == nil || slices.Contains(seen, t) {
					continue
				}
				seen = append(seen, t)
				name := safeName(t.Name)
				for i := 2; names[strings.ToLower(name)]; i++ {
					name = fmt.Sprintf("%s_%d", safeName(t.Name), i)
				}
				names[strings.ToLower(name)] = true
				ext := t.Extension
				if ext == "" {
					ext = "png"
				}
				file := out.Allocate(ext, "_"+name)
				if file == "" {
					continue
				}
				*outputs = append(*outputs, file)
				if err := os.WriteFile(file, t.Data, 0o644); err != nil {
					return err
				}
				textures[t] = file
			}
		}
	}
	return writeObj(ctx, m, obj, mtl, textures)
}

// safeName is a texture name for a file name: no characters file systems refuse, no white space, 40 at most.
func safeName(name string) string {
	var b []rune
	for _, r := range name {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 || unicode.IsSpace(r) {
			continue
		}
		b = append(b, r)
	}
	if len(b) == 0 {
		return "texture"
	}
	if len(b) > 40 {
		b = b[:40]
	}
	return string(b)
}
