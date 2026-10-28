package cad

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc/dxf"
	"github.com/dseif0x/filee-go/internal/presets"
)

// Reading DWG / DXF and writing DXF / DWG. Output is AutoCAD 2018 (AC1032)
// unless the preset asks for an older version (#489): 2018 is the current
// DWG format (AutoCAD 2018–2026, DWG TrueView, BricsCAD, LibreCAD …), and its
// text is UTF-8. Before 2007 text is in a code page, picked from the text.

// OutputVersion is written unless the preset says otherwise.
const OutputVersion = cadoc.AC1032

// VersionFor is the version to write for the preset's choice. "Same as the
// source" keeps the drawing's version within what can be written (older than
// 2000 becomes 2000). A 2007 DWG cannot be written: chosen, it is written as
// 2004, which AutoCAD 2007 opens; kept from a 2007 source, as 2010, which
// keeps its text in UTF-8.
func VersionFor(choice presets.CadVersion, source cadoc.Version, format string) cadoc.Version {
	var v cadoc.Version
	switch choice {
	case presets.AutoCad2013:
		v = cadoc.AC1027
	case presets.AutoCad2010:
		v = cadoc.AC1024
	case presets.AutoCad2007:
		v = cadoc.AC1021
	case presets.AutoCad2004:
		v = cadoc.AC1018
	case presets.AutoCad2000:
		v = cadoc.AC1015
	case presets.SameAsSourceVersion:
		switch {
		case source < cadoc.AC1015:
			v = cadoc.AC1015
		case source > OutputVersion:
			v = OutputVersion
		default:
			v = source
		}
	default:
		v = OutputVersion
	}
	if format == "dwg" && v == cadoc.AC1021 {
		if choice == presets.SameAsSourceVersion {
			return cadoc.AC1024
		}
		return cadoc.AC1018
	}
	return v
}

// ReadDocument reads a drawing (binary and ASCII DXF are detected). Errors for
// files that are not drawings, are damaged or use an unsupported version have
// readable messages.
func ReadDocument(path, format string, warn func(string)) (doc *cadoc.Document, err error) {
	kind := "DXF"
	if format == "dwg" {
		kind = "DWG"
	}
	defer func() {
		if r := recover(); r != nil {
			doc, err = nil, fmt.Errorf("the file is not a valid %s drawing or is damaged: %v", kind, r)
		}
	}()
	if format == "dwg" {
		if err := requireDwgSignature(path); err != nil {
			return nil, err
		}
		doc, err = readDwg(path, warn)
	} else {
		// CreateDefaults adds missing tables (layer 0, standard styles, model
		// space) so files from simple DXF writers can be rendered and saved as DWG.
		doc, err = dxf.ReadFile(path, dxf.ReadConfig{Failsafe: true, CreateDefaults: true}, func(level dxf.Level, msg string) {
			if level >= dxf.LevelWarning && warn != nil {
				warn(msg)
			}
		})
	}
	if err != nil {
		var nf *os.PathError
		if errors.As(err, &nf) {
			return nil, err
		}
		if strings.Contains(strings.ToLower(err.Error()), "not supported") {
			return nil, fmt.Errorf("this %s version is not supported (%v)", kind, err)
		}
		return nil, fmt.Errorf("the file is not a valid %s drawing or is damaged: %w", kind, err)
	}
	return doc, nil
}

// requireDwgSignature: DWG files start with "AC10xx"; anything else fails early.
func requireDwgSignature(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	head := make([]byte, 6)
	if n, _ := io.ReadFull(f, head); n < 6 || head[0] != 'A' || head[1] != 'C' || head[2] < '0' || head[2] > '9' {
		return errors.New("the file is not a DWG drawing (it does not start with an AutoCAD version tag)")
	}
	return nil
}

// WriteDocument writes the drawing as ASCII DXF or DWG in the preset's
// version and returns how many objects that version doesn't have were left out.
func WriteDocument(doc *cadoc.Document, path, format string, version presets.CadVersion, warn func(string)) (leftOut int, err error) {
	doc.Header.Version = VersionFor(version, doc.Header.Version, format)
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("the drawing could not be written as %s: %v", strings.ToUpper(format), r)
		}
		if err != nil {
			os.Remove(path)
		}
	}()
	if doc.Header.Version < cadoc.AC1021 {
		PrepareCodePage(doc)
	}
	leftOut = dropNewerEntities(doc, warn)
	dropStraySequenceEnds(doc)
	addMissingDimensionBlocks(doc)
	if format == "dwg" {
		dropUnwritableDimensions(doc, warn)
		err = writeDwg(doc, path, warn)
	} else {
		var b bytes.Buffer
		err = dxf.Write(&b, doc, dxf.WriteConfig{}, func(level dxf.Level, msg string) {
			if level >= dxf.LevelWarning && warn != nil {
				warn(msg)
			}
		})
		if err == nil {
			err = os.WriteFile(path, b.Bytes(), 0o644)
		}
	}
	if err != nil {
		return 0, fmt.Errorf("the drawing could not be written as %s: %w", strings.ToUpper(format), err)
	}
	return leftOut, nil
}

// dropNewerEntities leaves out what the version written doesn't have
// (AutoCAD before it would not open the file): tables and arc dimensions
// before 2004, multileaders and PDF underlays before 2007, meshes before 2010.
func dropNewerEntities(doc *cadoc.Document, warn func(string)) int {
	v := doc.Header.Version
	tooNew := func(e cadoc.Entity) bool {
		switch e.(type) {
		case *cadoc.TableEntity, *cadoc.DimensionArc:
			return v < cadoc.AC1018
		case *cadoc.MultiLeader, *cadoc.PdfUnderlay:
			return v < cadoc.AC1021
		case *cadoc.Mesh:
			return v < cadoc.AC1024
		}
		return false
	}
	count := 0
	for r := range doc.BlockRecords.All() {
		count += r.RemoveEntities(tooNew)
	}
	if count > 0 && warn != nil {
		warn(fmt.Sprintf("%d object(s) newer than %s were left out.", count, v))
	}
	return count
}

// dropStraySequenceEnds: R12 files can leave SEQEND markers as loose entities.
func dropStraySequenceEnds(doc *cadoc.Document) {
	for r := range doc.BlockRecords.All() {
		r.RemoveEntities(func(e cadoc.Entity) bool { _, ok := e.(*cadoc.Seqend); return ok })
	}
}

// dropUnwritableDimensions: DWG stores each dimension's measured value, which
// cannot be computed for degenerate ones; those are left out instead of
// failing the whole file.
func dropUnwritableDimensions(doc *cadoc.Document, warn func(string)) {
	for r := range doc.BlockRecords.All() {
		n := r.RemoveEntities(func(e cadoc.Entity) bool {
			d, ok := e.(cadoc.Dimension)
			if !ok {
				return false
			}
			_, err := d.Measurement()
			return err != nil
		})
		if n > 0 && warn != nil {
			warn(fmt.Sprintf("%d degenerate dimension(s) in %s were left out of the DWG.", n, r.Name))
		}
	}
}

// addMissingDimensionBlocks draws the picture of dimensions that come without
// one (some writers omit it); the writers would otherwise leave them out.
func addMissingDimensionBlocks(doc *cadoc.Document) {
	var dims []cadoc.Dimension
	for r := range doc.BlockRecords.All() {
		for _, e := range r.Entities {
			if d, ok := e.(cadoc.Dimension); ok && d.Dimension().Block == nil {
				dims = append(dims, d)
			}
		}
	}
	for _, d := range dims {
		_ = cadoc.UpdateDimensionBlock(doc, d)
	}
}
