package libreoffice

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"hash/crc32"
	"io"
	"os"
	"sort"
	"strings"
)

// LibreOffice's package reader refuses archives that other readers open
// (Tencent Docs exports): entries stored with a data descriptor, bytes
// between entries, a name twice. The parts are fine, so a copy with the same
// parts written plainly loads.

// IsPackage: the file is a ZIP holding [Content_Types].xml (Office Open XML) or mimetype (OpenDocument).
func IsPackage(path string) bool {
	z, err := zip.OpenReader(path)
	if err != nil {
		return false
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name == "[Content_Types].xml" || f.Name == "mimetype" {
			return true
		}
	}
	return false
}

// Repack writes every part of input to output: [Content_Types].xml and an
// OpenDocument mimetype first (mimetype stored), the rest deflated, without
// directory entries; a name that appears twice keeps its first part.
func Repack(input, output string) error {
	z, err := zip.OpenReader(input)
	if err != nil {
		return err
	}
	defer z.Close()
	var parts []*zip.File
	for _, f := range z.File {
		if !strings.HasSuffix(f.Name, "/") && !strings.HasSuffix(f.Name, `\`) {
			parts = append(parts, f)
		}
	}
	rank := func(f *zip.File) int {
		if f.Name == "[Content_Types].xml" || f.Name == "mimetype" {
			return 0
		}
		return 1
	}
	sort.SliceStable(parts, func(i, j int) bool { return rank(parts[i]) < rank(parts[j]) })
	out, err := os.Create(output)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	written := map[string]bool{}
	for _, p := range parts {
		k := strings.ToLower(p.Name)
		if written[k] {
			continue
		}
		written[k] = true
		h := &zip.FileHeader{Name: p.Name, Method: zip.Deflate}
		if y := p.Modified.Year(); y >= 1980 && y <= 2107 {
			h.Modified = p.Modified
		}
		var data []byte
		rc, err := p.Open()
		if err == nil {
			data, err = io.ReadAll(rc)
			rc.Close()
		}
		if err == nil {
			// Sizes go into the local headers, never data descriptors (which LibreOffice refuses on stored entries).
			if p.Name == "mimetype" {
				h.Method = zip.Store
			} else {
				var buf bytes.Buffer
				fw, _ := flate.NewWriter(&buf, flate.BestCompression)
				fw.Write(data)
				fw.Close()
				h.CompressedSize64 = uint64(buf.Len())
				h.CRC32, h.UncompressedSize64 = crc32.ChecksumIEEE(data), uint64(len(data))
				var w io.Writer
				if w, err = zw.CreateRaw(h); err == nil {
					_, err = w.Write(buf.Bytes())
				}
				data = nil
			}
			if data != nil {
				h.CRC32, h.CompressedSize64, h.UncompressedSize64 = crc32.ChecksumIEEE(data), uint64(len(data)), uint64(len(data))
				var w io.Writer
				if w, err = zw.CreateRaw(h); err == nil {
					_, err = w.Write(data)
				}
			}
		}
		if err != nil {
			zw.Close()
			out.Close()
			os.Remove(output)
			return err
		}
	}
	if err := zw.Close(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
