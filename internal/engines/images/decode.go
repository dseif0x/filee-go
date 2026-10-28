package images

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gen2brain/avif"
	"github.com/gen2brain/heic"
	"github.com/gen2brain/jpegxl"
	"github.com/gen2brain/webp"
	jpeg2000 "github.com/mrjoshuak/go-jpeg2000"
	"golang.org/x/image/bmp"

	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/presets"
)

var (
	errNotPNG     = errors.New("the file is not a PNG image")
	errDamagedPNG = errors.New("the PNG file is damaged (a chunk runs past the end)")
)

// maxFileBytes is the largest picture file read into memory.
const maxFileBytes = 2 << 30

// Decode reads the picture at path as format (a format id; "" detects it by
// the extension). Layered formats come back as one flattened picture; other
// multi-frame formats (TIFF pages, animation frames, icon sizes) keep all
// frames, animations as whole (coalesced) frames. The frames are as stored:
// the Exif orientation is applied by ApplyOptions (or AutoOrient). preset
// gives the reading options (FITS stretch and debayering) and may be nil.
func Decode(path, format string, preset *presets.Preset) (*Picture, error) {
	if format == "" {
		if f := formats.Detect(path); f != nil {
			format = f.ID
		}
	}
	if format == "fits" {
		debayer := true
		if preset != nil {
			debayer = preset.Image.FitsDebayer
		}
		img, err := readFits(path)
		if err != nil {
			return nil, err
		}
		return &Picture{Frames: []*Frame{fitsToFrame(img, debayer)}}, nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.Size() > maxFileBytes {
		return nil, fmt.Errorf("the file is too large to read (%d MB)", st.Size()>>20)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if format == "livp" {
		still, stillFormat, err := livePhotoStill(path)
		if err != nil {
			return nil, err
		}
		return DecodeBytes(still, stillFormat)
	}
	if format == "ppm" || format == "pam" || format == "pfm" {
		// The Netpbm family shares one reader; the magic number tells them apart.
		return decodeNetpbm(data)
	}
	return DecodeBytes(data, format)
}

// DecodeBytes decodes an in-memory file of format (see Decode).
func DecodeBytes(data []byte, format string) (*Picture, error) {
	switch format {
	case "png":
		if isAnimatedPNG(data) {
			return decodeAPNG(data)
		}
		return decodePNG(data)
	case "apng":
		return decodeAPNG(data)
	case "jpg":
		return decodeJPEG(data)
	case "gif":
		return decodeGIF(data)
	case "webp":
		return decodeWebP(data)
	case "tiff":
		return decodeTIFF(data)
	case "bmp":
		return decodeBMP(data)
	case "ico", "cur":
		return decodeICO(data)
	case "avif":
		img, err := avif.Decode(bytes.NewReader(data), avif.Options{AutoRotate: true})
		if err != nil {
			return nil, fmt.Errorf("AVIF: %w", err)
		}
		f := NewFrame(img)
		f.Meta = heifMeta(data)
		return one(f), nil
	case "heic":
		img, err := heic.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("HEIC: %w", err)
		}
		f := NewFrame(img)
		f.Meta = heifMeta(data)
		return one(f), nil
	case "jxl":
		anim, err := jpegxl.DecodeAll(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("JPEG XL: %w", err)
		}
		p := &Picture{}
		for i, img := range anim.Image {
			f := NewFrame(img)
			if i < len(anim.Delay) {
				f.Delay, f.Ticks = anim.Delay[i], 1000
			}
			p.Frames = append(p.Frames, f)
		}
		if len(p.Frames) == 0 {
			return nil, errors.New("the JPEG XL file holds no picture")
		}
		return p, nil
	case "jp2":
		img, err := jpeg2000.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("JPEG 2000: %w", err)
		}
		return one(NewFrame(img)), nil
	case "psd", "psb":
		return decodePSD(data)
	case "tga":
		return decodeTGA(data)
	case "ppm", "pam", "pfm":
		return decodeNetpbm(data)
	case "xcf":
		return decodeXCF(data)
	case "raw":
		return decodeRaw(data)
	case "mng":
		return decodeMNG(data)
	case "qoi":
		return decodeQOI(data)
	case "dds":
		return decodeDDS(data)
	case "pcx":
		return decodePCX(data)
	case "xpm":
		return decodeXPM(data)
	case "xbm":
		return decodeXBM(data)
	case "wbmp":
		return decodeWBMP(data)
	case "sgi":
		return decodeSGI(data)
	case "ras":
		return decodeSunRaster(data)
	case "miff":
		return decodeMIFF(data)
	case "dcm":
		return decodeDICOM(data)
	case "pcd":
		return decodePCD(data)
	case "exr":
		return decodeEXR(data)
	case "hdr":
		return decodeHDR(data)
	case "fits":
		img, err := parseFits(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err
		}
		return one(fitsToFrame(img, true)), nil
	}
	return nil, fmt.Errorf("the image engine cannot read %q", format)
}

func one(f *Frame) *Picture { return &Picture{Frames: []*Frame{f}} }

func decodePNG(data []byte) (*Picture, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("PNG: %w", err)
	}
	f := NewFrame(img)
	if chunks, err := pngChunks(data); err == nil {
		f.Meta = pngMeta(chunks)
		for _, c := range chunks {
			// A colour type with alpha, or a tRNS chunk, makes it an image with alpha (as ImageMagick reads it).
			if c.Type == "IHDR" && len(c.Data) == 13 && (c.Data[9] == 4 || c.Data[9] == 6) {
				f.Alpha = true
			}
			if c.Type == "IHDR" && len(c.Data) == 13 && (c.Data[9] == 0 || c.Data[9] == 4) {
				f.Gray = true
			}
			if c.Type == "tRNS" {
				f.Alpha = true
			}
		}
	}
	return one(f), nil
}

func decodeJPEG(data []byte) (*Picture, error) {
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("JPEG: %w", err)
	}
	f := NewFrame(img)
	if y, ok := img.(*image.YCbCr); ok && y.SubsampleRatio == image.YCbCrSubsampleRatio444 && isGray(f.Img) {
		f.Gray = false // a colour JPEG that happens to be grey stays colour
	}
	f.Meta = jpegMeta(data)
	return one(f), nil
}

func decodeBMP(data []byte) (*Picture, error) {
	// Filee's reader handles more kinds (RLE, bit fields, alpha); x/image/bmp is the fallback.
	p, err := decodeBMPFallback(data)
	if err != nil {
		img, err2 := bmp.Decode(bytes.NewReader(data))
		if err2 != nil {
			return nil, fmt.Errorf("BMP: %w", err)
		}
		p = one(NewFrame(img))
	}
	if len(data) >= 46 {
		ppmX := int32(binary.LittleEndian.Uint32(data[38:]))
		ppmY := int32(binary.LittleEndian.Uint32(data[42:]))
		if ppmX > 0 && ppmY > 0 {
			p.Frames[0].Meta.DpiX, p.Frames[0].Meta.DpiY = float64(ppmX)*0.0254, float64(ppmY)*0.0254
		}
	}
	return p, nil
}

func decodeWebP(data []byte) (*Picture, error) {
	anim, err := webp.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("WebP: %w", err)
	}
	meta := webpMeta(data)
	p := &Picture{Loops: anim.LoopCount}
	for i, img := range anim.Image {
		f := NewFrame(img)
		f.Meta = meta
		if len(anim.Image) > 1 && i < len(anim.Delay) {
			f.Delay, f.Ticks = anim.Delay[i], 1000
		}
		p.Frames = append(p.Frames, f)
	}
	if len(p.Frames) == 0 {
		return nil, errors.New("the WebP file holds no picture")
	}
	return p, nil
}

func decodeGIF(data []byte) (*Picture, error) {
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("GIF: %w", err)
	}
	return coalesceGIF(g), nil
}

// livePhotoStill reads the photo (JPEG or HEIC) of an iPhone LIVP without unpacking it.
func livePhotoStill(path string) ([]byte, string, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, "", fmt.Errorf("the Live Photo can't be opened: %w", err)
	}
	defer z.Close()
	var found *zip.File
	for _, f := range z.File {
		if f.FileInfo().IsDir() || strings.HasPrefix(f.Name, "__MACOSX/") || strings.HasPrefix(filepath.Base(f.Name), "._") {
			continue
		}
		switch strings.ToLower(filepath.Ext(f.Name)) {
		case ".jpg", ".jpeg", ".heic", ".heif":
			if found != nil {
				return nil, "", errors.New("the Live Photo holds more than one photo")
			}
			found = f
		}
	}
	if found == nil {
		return nil, "", errors.New("the Live Photo holds no photo")
	}
	const maxImage = 64 << 20
	if found.UncompressedSize64 == 0 || found.UncompressedSize64 > maxImage {
		return nil, "", errors.New("the Live Photo's picture is empty or too large")
	}
	r, err := found.Open()
	if err != nil {
		return nil, "", err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, maxImage+1))
	if err != nil {
		return nil, "", err
	}
	format := "jpg"
	if ext := strings.ToLower(filepath.Ext(found.Name)); ext == ".heic" || ext == ".heif" {
		format = "heic"
	}
	return data, format, nil
}

// DecodeImage reads the first frame of the file (the largest of an icon, the
// composite of a layered file) turned upright, for engines that need pixels.
func DecodeImage(path, format string) (*image.NRGBA, error) {
	if format == "" {
		if f := formats.Detect(path); f != nil {
			format = f.ID
		}
	}
	pic, err := Decode(path, format, nil)
	if err != nil {
		return nil, err
	}
	f := SelectFrames(pic, format, "png")[0]
	f.AutoOrient()
	return f.Img, nil
}
