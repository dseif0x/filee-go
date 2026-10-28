package ofd

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dseif0x/filee-go/internal/xdom"
)

// The ZIP container of an OFD file, read defensively: parts are looked up by
// name and never written out under their own names, references are resolved
// against the package root and cannot leave it, XML is read without DTDs and
// every part with a size limit (also while decompressing).

const (
	maxEntries      = 100_000
	maxXMLBytes     = 64 << 20
	maxPictureBytes = 32 << 20
	maxTotalBytes   = 1 << 30
)

type pkg struct {
	f        *os.File
	entries  map[string]*zip.File
	names    []string
	maxTotal int64
	read     int64
}

func openPackage(path string) (*pkg, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	z, err := zip.NewReader(f, st.Size())
	if err != nil {
		f.Close()
		return nil, errors.New("the file is not an OFD document (it is not a ZIP package); it may be damaged")
	}
	p, err := newPackage(z, maxTotalBytes)
	if err != nil {
		f.Close()
		return nil, err
	}
	p.f = f
	return p, nil
}

func memoryPackage(b []byte, maxTotal int64) (*pkg, error) {
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, err
	}
	return newPackage(z, maxTotal)
}

func newPackage(z *zip.Reader, maxTotal int64) (*pkg, error) {
	if len(z.File) > maxEntries {
		return nil, fmt.Errorf("the OFD package has more than %d parts; Filee does not read it", maxEntries)
	}
	p := &pkg{entries: map[string]*zip.File{}, maxTotal: min(maxTotal, maxTotalBytes)}
	for _, e := range z.File {
		// Names that climb out of the package are unreachable; of two parts with one name the first wins.
		if strings.HasSuffix(e.Name, "/") {
			continue
		}
		if name, ok := resolve("", e.Name); ok {
			k := strings.ToLower(name)
			if _, dup := p.entries[k]; !dup {
				p.entries[k] = e
				p.names = append(p.names, name)
			}
		}
	}
	return p, nil
}

func (p *pkg) Close() {
	if p.f != nil {
		p.f.Close()
	}
}

func (p *pkg) has(name string) bool {
	_, ok := p.entries[strings.ToLower(name)]
	return name != "" && ok
}

// resolve turns a reference into a part name: relative to baseDir, or to the
// root when it starts with "/"; false when it is empty, leaves the package
// or is a URL or drive path.
func resolve(baseDir, ref string) (string, bool) {
	ref = strings.ReplaceAll(strings.TrimSpace(ref), `\`, "/")
	if ref == "" || strings.Contains(ref, ":") {
		return "", false
	}
	var parts []string
	if !strings.HasPrefix(ref, "/") {
		for _, s := range strings.Split(baseDir, "/") {
			if s != "" {
				parts = append(parts, s)
			}
		}
	}
	for _, s := range strings.Split(ref, "/") {
		switch s {
		case "", ".":
		case "..":
			if len(parts) == 0 {
				return "", false
			}
			parts = parts[:len(parts)-1]
		default:
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "/"), true
}

func dirName(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[:i]
	}
	return ""
}

// loadXML returns an XML part, nil when there is no such part; an error for parts that are not XML.
func (p *pkg) loadXML(name string) (*xdom.Node, error) {
	if !p.has(name) {
		return nil, nil
	}
	b, err := p.readPart(name, maxXMLBytes)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, fmt.Errorf("the OFD part %s is larger than %d MB; Filee does not read it", name, maxXMLBytes>>20)
	}
	n, err := xdom.ParseBytes(b)
	if err != nil {
		return nil, fmt.Errorf("the OFD part %s is not valid XML; the file may be encrypted or damaged: %v", name, err)
	}
	return n, nil
}

// readPart returns the bytes of a part; nil when missing or larger than limit.
func (p *pkg) readPart(name string, limit int64) ([]byte, error) {
	e := p.entries[strings.ToLower(name)]
	if e == nil || int64(e.UncompressedSize64) > limit {
		return nil, nil
	}
	rc, err := e.Open()
	if err != nil {
		return nil, nil
	}
	defer rc.Close()
	var buf bytes.Buffer
	chunk := make([]byte, 81920)
	for {
		n, err := rc.Read(chunk)
		if n > 0 {
			if int64(buf.Len()+n) > limit {
				return nil, nil
			}
			p.read += int64(n)
			if p.read > p.maxTotal {
				return nil, fmt.Errorf("the OFD package unpacks to more than %d MB; Filee does not read it", p.maxTotal>>20)
			}
			buf.Write(chunk[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil
		}
	}
	return buf.Bytes(), nil
}
